package auth

import (
	"context"
	"time"

	"github.com/uptrace/bun"

	"bgin/internal/models"
	shareduser "bgin/internal/shared/user"
)

// Repository 是认证域的数据访问入口，内嵌 shareduser.Repository 以复用用户基础查询。
type Repository struct {
	db *bun.DB
	*shareduser.Repository
}

// NewRepository 构造 Repository。
func NewRepository(db *bun.DB) *Repository {
	return &Repository{
		db:         db,
		Repository: shareduser.NewRepository(db),
	}
}

// MarkEmailVerified 置位邮箱验证状态。
func (r *Repository) MarkEmailVerified(ctx context.Context, userID string) error {
	_, err := r.db.NewUpdate().
		Model((*models.User)(nil)).
		Set("email_verified = ?", true).
		Set("updated_at = ?", time.Now().UTC()).
		Where("id = ?", userID).
		Exec(ctx)
	return err
}

// SaveVerification 覆盖式写入一次性凭据：同一 identifier 只保留最新一条。
func (r *Repository) SaveVerification(ctx context.Context, identifier, value string, expiresAt time.Time) error {
	return r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if _, err := tx.NewDelete().
			Model((*models.Verification)(nil)).
			Where("identifier = ?", identifier).
			Exec(ctx); err != nil {
			return err
		}

		record := &models.Verification{Identifier: identifier, Value: value, ExpiresAt: expiresAt}
		_, err := tx.NewInsert().Model(record).Exec(ctx)
		return err
	})
}

// FindVerification 取出某 identifier 的唯一一条凭据；不存在时返回 sql.ErrNoRows。
func (r *Repository) FindVerification(ctx context.Context, identifier string) (*models.Verification, error) {
	record := new(models.Verification)
	err := r.db.NewSelect().
		Model((*models.Verification)(nil)).
		Where("v.identifier = ?", identifier).
		Scan(ctx, record)
	if err != nil {
		return nil, err
	}
	return record, nil
}

// DeleteVerification 按主键删除已核销或已过期的凭据。
func (r *Repository) DeleteVerification(ctx context.Context, id string) error {
	_, err := r.db.NewDelete().
		Model((*models.Verification)(nil)).
		Where("id = ?", id).
		Exec(ctx)
	return err
}

// ConsumeVerification 原子核销一枚凭据：仅当 identifier 与凭据摘要都匹配时删除，
// 并报告是否真的删掉了那一行。返回 false 表示这枚凭据已不存在、已过期，或刚被别处消费。
//
// 把"取出—比对—删除"拆成两个事务是不安全的：两个携带同一验证码的并发请求会都读到
// 同一条记录、都通过比对，随后都执行删除（第二次影响 0 行却同样返回成功），
// 一次性凭据因此被消费两次。这里用 compare-and-delete 把判定收进一条语句，
// 由受影响行数给出唯一裁决。
func (r *Repository) ConsumeVerification(ctx context.Context, identifier, value string) (bool, error) {
	result, err := r.db.NewDelete().
		Model((*models.Verification)(nil)).
		Where("identifier = ?", identifier).
		Where("value = ?", value).
		Exec(ctx)
	if err != nil {
		return false, err
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	return affected == 1, nil
}

// CountActiveUsers 统计未删除用户数，供健康检查与 db:seed 使用。
func (r *Repository) CountActiveUsers(ctx context.Context) (int, error) {
	return r.db.NewSelect().Model((*models.User)(nil)).Where("u.deleted_at IS NULL").Count(ctx)
}
