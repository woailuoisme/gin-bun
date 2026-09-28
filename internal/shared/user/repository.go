// Package user 是用户域的内核：user 表的读写入口、档案视图与档案列的更新规则。
//
// 它不面向任何具体交付面，由 internal/features/client/profile（前端自助）与
// internal/features/admin/user（后台管理）两个模块共用，见 ADR-0005。
package user

import (
	"context"
	"database/sql"
	"strings"
	"time"

	govalidator "github.com/go-playground/validator/v10"
	"github.com/samber/lo"
	"github.com/uptrace/bun"

	"bgin/internal/models"
	"bgin/internal/platform/errs"
	"bgin/internal/platform/validator"
	"bgin/internal/shared/enums"
)

func init() {
	if err := validator.RegisterRule("usersort", ValidateSort, "oneof"); err != nil {
		panic(err)
	}
}

// ListFilter 是列表查询的过滤条件。
type ListFilter struct {
	Search string
	Status enums.Status
	Role   enums.Role
	Sort   []string
}

// sortColumns 是允许排序的字段白名单，未列出的字段一律忽略以杜绝注入。
var sortColumns = map[string]string{
	"createdAt":   "u.created_at",
	"updatedAt":   "u.updated_at",
	"email":       "u.email",
	"name":        "u.name",
	"lastLoginAt": "u.last_login_at",
}

// ValidateSort 校验用户列表的排序字段，支持以逗号分隔的复合排序以及 - 降序前缀。
func ValidateSort(field govalidator.FieldLevel) bool {
	raw := field.Field().String()
	if raw == "" {
		return true
	}
	for part := range strings.SplitSeq(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			return false
		}
		part = strings.TrimPrefix(part, "-")
		if _, ok := sortColumns[part]; !ok {
			return false
		}
	}
	return true
}

// defaultSort 保证分页遍历的确定性顺序，对应 api.md 6.2。
const defaultSort = "-createdAt"

// updatableColumns 是允许经 UpdateFields 写入的列白名单。
//
// 列名会被拼进 SQL（值仍走占位符），因此这道检查必须落在函数内部，
// 而不是靠"调用方只传本包构造的字面量"这条纪律：一旦有人把请求体里的键透传进来，
// 没有白名单就是注入面。新增可写列时同时改这里与调用点。
var updatableColumns = map[string]struct{}{
	"name":             {},
	"image":            {},
	"username":         {},
	"display_username": {},
	"phone_number":     {},
	"role":             {},
	"status":           {},
	"banned":           {},
	"ban_reason":       {},
	"ban_expires":      {},
	"email_verified":   {},
}

// Repository 是 user 表的数据访问入口。
type Repository struct {
	db *bun.DB
}

// NewRepository 构造 Repository。
func NewRepository(db *bun.DB) *Repository { return &Repository{db: db} }

// activeUsers 返回已排除软删除记录的查询，所有业务查询都必须经过它。
func (r *Repository) activeUsers() *bun.SelectQuery {
	return r.db.NewSelect().Model((*models.User)(nil)).Where("u.deleted_at IS NULL")
}

// FindByID 按主键查找用户。
func (r *Repository) FindByID(ctx context.Context, id string) (*models.User, error) {
	user := new(models.User)
	if err := r.activeUsers().Where("u.id = ?", id).Scan(ctx, user); err != nil {
		return nil, err
	}
	return user, nil
}

// FindByUsername 按用户名大小写不敏感查找用户。
func (r *Repository) FindByUsername(ctx context.Context, username string) (*models.User, error) {
	user := new(models.User)
	err := r.activeUsers().
		Where("u.username IS NOT NULL").
		Where("LOWER(u.username) = ?", strings.ToLower(strings.TrimSpace(username))).
		Scan(ctx, user)
	if err != nil {
		return nil, err
	}
	return user, nil
}

// FindByEmail 按邮箱大小写不敏感查找用户。
func (r *Repository) FindByEmail(ctx context.Context, email string) (*models.User, error) {
	user := new(models.User)
	err := r.activeUsers().Where("LOWER(u.email) = ?", strings.ToLower(strings.TrimSpace(email))).Scan(ctx, user)
	if err != nil {
		return nil, err
	}
	return user, nil
}

// List 按条件分页查询用户。
func (r *Repository) List(ctx context.Context, filter ListFilter, offset, limit int) ([]models.User, error) {
	users := make([]models.User, 0, limit)

	query := r.applyFilter(r.activeUsers(), filter)
	for _, order := range filter.Sort {
		query = query.Order(order)
	}
	query = query.Offset(offset).Limit(limit)

	if err := query.Scan(ctx, &users); err != nil {
		return nil, err
	}
	return users, nil
}

// Count 统计满足条件的用户数。
func (r *Repository) Count(ctx context.Context, filter ListFilter) (int, error) {
	return r.applyFilter(r.activeUsers(), filter).Count(ctx)
}

// CreateWithCredential 在同一事务内创建用户与其本地密码凭据。
func (r *Repository) CreateWithCredential(ctx context.Context, user *models.User, passwordHash string) error {
	return r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if _, err := tx.NewInsert().Model(user).Exec(ctx); err != nil {
			return err
		}

		account := &models.Account{
			UserID:     user.ID,
			AccountID:  user.ID,
			ProviderID: models.ProviderCredential,
			Password:   &passwordHash,
		}
		_, err := tx.NewInsert().Model(account).Exec(ctx)
		return err
	})
}

// FindCredentialAccount 取某用户的本地密码凭据；不存在时返回 sql.ErrNoRows。
func (r *Repository) FindCredentialAccount(ctx context.Context, userID string) (*models.Account, error) {
	account := new(models.Account)
	err := r.db.NewSelect().
		Model((*models.Account)(nil)).
		Where("a.user_id = ?", userID).
		Where("a.provider_id = ?", models.ProviderCredential).
		Scan(ctx, account)
	if err != nil {
		return nil, err
	}
	return account, nil
}

// UpdatePassword 改写本地密码凭据的哈希；凭据不存在时返回 sql.ErrNoRows。
func (r *Repository) UpdatePassword(ctx context.Context, userID, passwordHash string) error {
	result, err := r.db.NewUpdate().
		Model((*models.Account)(nil)).
		Set("password = ?", passwordHash).
		Set("updated_at = ?", time.Now().UTC()).
		Where("user_id = ?", userID).
		Where("provider_id = ?", models.ProviderCredential).
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
		Model((*models.User)(nil)).
		Set("last_login_at = ?", time.Now().UTC()).
		Set("last_login_method = ?", method).
		Where("id = ?", userID).
		Exec(ctx)
	return err
}

// UpdateFields 按列名更新用户。列名必须落在 updatableColumns 白名单内，否则直接报错。
func (r *Repository) UpdateFields(ctx context.Context, id string, fields map[string]any) error {
	if len(fields) == 0 {
		return nil
	}

	query := r.db.NewUpdate().
		Model((*models.User)(nil)).
		Where("id = ?", id).
		Where("deleted_at IS NULL")
	for column, value := range fields {
		if _, ok := updatableColumns[column]; !ok {
			return errs.BadRequest("Disallowed user column for update: " + column)
		}
		query = query.Set(column+" = ?", value)
	}
	query = query.Set("updated_at = ?", time.Now().UTC())

	result, err := query.Exec(ctx)
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

// SoftDelete 写 deleted_at 完成软删除，保留 idx_user_email 等部分唯一索引的语义。
func (r *Repository) SoftDelete(ctx context.Context, id string) error {
	now := time.Now().UTC()
	result, err := r.db.NewUpdate().
		Model((*models.User)(nil)).
		Set("deleted_at = ?", now).
		Set("updated_at = ?", now).
		Where("id = ?", id).
		Where("deleted_at IS NULL").
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

// applyFilter 把列表条件翻译成 SQL，所有取值都通过占位符传递。
func (r *Repository) applyFilter(query *bun.SelectQuery, filter ListFilter) *bun.SelectQuery {
	if filter.Status != "" {
		query = query.Where("u.status = ?", filter.Status)
	}
	if filter.Role != "" {
		query = query.Where("u.role = ?", filter.Role)
	}
	if filter.Search != "" {
		// ILIKE 需要显式转义，避免用户输入的通配符改变检索语义。
		pattern := "%" + escapeLike(filter.Search) + "%"
		query = query.Where(
			"(u.name ILIKE ? OR u.email ILIKE ? OR u.username ILIKE ?)",
			pattern, pattern, pattern,
		)
	}
	return query
}

// escapeLike 转义 LIKE 的通配符，使用户输入只做字面匹配。
func escapeLike(value string) string {
	replacer := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return replacer.Replace(value)
}

// NormaliseSort 把 api.md 6.2 的排序表达式翻译成白名单内的列与方向。
// 缺省或全部非法时退回默认排序，保证分页顺序稳定。
func NormaliseSort(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		raw = defaultSort
	}

	orders := make([]string, 0, 4)
	for part := range strings.SplitSeq(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		descending := strings.HasPrefix(part, "-")
		column, ok := sortColumns[strings.TrimPrefix(part, "-")]
		if !ok {
			continue
		}
		orders = append(orders, column+lo.Ternary(descending, " DESC", " ASC"))
	}

	if len(orders) == 0 {
		orders = append(orders, sortColumns["createdAt"]+" DESC")
	}
	return orders
}
