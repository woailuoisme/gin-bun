package auth

import (
	"context"
	"database/sql"
	"time"

	"github.com/uptrace/bun"

	"bgin/internal/database/model"
	shareduser "bgin/internal/shared/user"
)

// Repository 是认证域的数据访问入口。
type Repository struct {
	db    *bun.DB
	users *shareduser.Repository
}

// NewRepository 构造 Repository。
func NewRepository(db *bun.DB) *Repository {
	return &Repository{
		db:    db,
		users: shareduser.NewRepository(db),
	}
}

// FindUserByEmail 按邮箱大小写不敏感查找用户。
func (r *Repository) FindUserByEmail(ctx context.Context, email string) (*model.User, error) {
	return r.users.FindByEmail(ctx, email)
}

// FindUserByUsername 按用户名大小写不敏感查找用户。
func (r *Repository) FindUserByUsername(ctx context.Context, username string) (*model.User, error) {
	return r.users.FindByUsername(ctx, username)
}

// FindUserByID 按主键查找用户。
func (r *Repository) FindUserByID(ctx context.Context, id string) (*model.User, error) {
	return r.users.FindByID(ctx, id)
}

// FindCredentialAccount 取某用户的本地密码凭据；不存在时返回 sql.ErrNoRows。
func (r *Repository) FindCredentialAccount(ctx context.Context, userID string) (*model.Account, error) {
	account := new(model.Account)
	err := r.db.NewSelect().
		Model((*model.Account)(nil)).
		Where("a.user_id = ?", userID).
		Where("a.provider_id = ?", model.ProviderCredential).
		Scan(ctx, account)
	if err != nil {
		return nil, err
	}
	return account, nil
}

// CreateUserWithCredential 在同一事务内创建用户与其本地密码凭据。
func (r *Repository) CreateUserWithCredential(ctx context.Context, user *model.User, passwordHash string) error {
	return r.users.CreateWithCredential(ctx, user, passwordHash)
}

// UpdatePassword 改写本地密码凭据的哈希；凭据不存在时返回 sql.ErrNoRows。
func (r *Repository) UpdatePassword(ctx context.Context, userID, passwordHash string) error {
	result, err := r.db.NewUpdate().
		Model((*model.Account)(nil)).
		Set("password = ?", passwordHash).
		Set("updated_at = ?", time.Now().UTC()).
		Where("user_id = ?", userID).
		Where("provider_id = ?", model.ProviderCredential).
		Exec(ctx)
	if err != nil {
		return err
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// RecordLogin 刷新最近登录时间与登录方式。
func (r *Repository) RecordLogin(ctx context.Context, userID, method string) error {
	_, err := r.db.NewUpdate().
		Model((*model.User)(nil)).
		Set("last_login_at = ?", time.Now().UTC()).
		Set("last_login_method = ?", method).
		Where("id = ?", userID).
		Exec(ctx)
	return err
}

// MarkEmailVerified 置位邮箱验证状态。
func (r *Repository) MarkEmailVerified(ctx context.Context, userID string) error {
	_, err := r.db.NewUpdate().
		Model((*model.User)(nil)).
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
			Model((*model.Verification)(nil)).
			Where("identifier = ?", identifier).
			Exec(ctx); err != nil {
			return err
		}

		record := &model.Verification{Identifier: identifier, Value: value, ExpiresAt: expiresAt}
		_, err := tx.NewInsert().Model(record).Exec(ctx)
		return err
	})
}

// FindVerification 取出某 identifier 的唯一一条凭据；不存在时返回 sql.ErrNoRows。
func (r *Repository) FindVerification(ctx context.Context, identifier string) (*model.Verification, error) {
	record := new(model.Verification)
	err := r.db.NewSelect().
		Model((*model.Verification)(nil)).
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
		Model((*model.Verification)(nil)).
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
		Model((*model.Verification)(nil)).
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
	return r.db.NewSelect().Model((*model.User)(nil)).Where("u.deleted_at IS NULL").Count(ctx)
}
