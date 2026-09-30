package user

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/aarondl/opt/omitnull"
	"github.com/jackc/pgx/v5/pgconn"

	"bgin/internal/platform/errs"
)

// uniqueViolation 是 PostgreSQL 的唯一约束冲突错误码。
const uniqueViolation = "23505"

// 唯一索引名，用于把冲突翻译成具体错误码。
const (
	indexUserEmail       = "idx_user_email"
	indexUserUsername    = "idx_user_username"
	indexUserPhoneNumber = "idx_user_phone_number"
)

// NormaliseEmail 统一邮箱大小写与首尾空白，与库中 idx_user_email 的口径一致。
func NormaliseEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

// NormaliseUsername 统一用户名大小写与首尾空白，与库中 idx_user_username 的口径一致。
func NormaliseUsername(username string) string {
	return strings.ToLower(strings.TrimSpace(username))
}

// ProfilePatch 是档案字段局部更新的校验投影。
// 前端自助与后台管理对这组基础档案字段使用完全一致的校验规则。
type ProfilePatch struct {
	Name            *string `json:"name" validate:"omitempty,min=1,max=120"`
	Image           *string `json:"image" validate:"omitempty,max=2048"`
	Username        *string `json:"username" validate:"omitempty,min=3,max=30,usernameslug,notreserved"`
	DisplayUsername *string `json:"display_username" validate:"omitempty,min=1,max=120"`
	PhoneNumber     *string `json:"phone_number" validate:"omitempty,e164"`
}

// FieldSet 是客户端与后台管理共用的可更新档案字段集合。
type FieldSet struct {
	Name            omitnull.Val[string]
	Image           omitnull.Val[string]
	Username        omitnull.Val[string]
	DisplayUsername omitnull.Val[string]
	PhoneNumber     omitnull.Val[string]
}

// Patch 将字段集转换为用于校验的指针投影结构体。
func (f FieldSet) Patch() ProfilePatch {
	return ProfilePatch{
		Name:            ToPtr(f.Name),
		Image:           ToPtr(f.Image),
		Username:        ToPtr(f.Username),
		DisplayUsername: ToPtr(f.DisplayUsername),
		PhoneNumber:     ToPtr(f.PhoneNumber),
	}
}

// ToPtr 将三态值在拥有具体值时转换为指针，缺省或置空时返回 nil。
func ToPtr[T any](v omitnull.Val[T]) *T {
	if val, ok := v.Get(); ok {
		return &val
	}
	return nil
}

// CollectFields 把档案三态字段翻译成待更新列。repo 只用于确认用户名未被他人占用。
func CollectFields(ctx context.Context, repo *Repository, userID string, patch FieldSet) (map[string]any, error) {
	fields := make(map[string]any, 5)

	if !patch.Name.IsUnset() {
		if patch.Name.IsNull() {
			return nil, FieldCannotBeNull("name")
		}
		fields["name"] = strings.TrimSpace(patch.Name.MustGet())
	}
	if !patch.Image.IsUnset() {
		SetNullable(fields, "image", patch.Image)
	}
	if !patch.PhoneNumber.IsUnset() {
		SetNullable(fields, "phone_number", patch.PhoneNumber)
	}
	if !patch.Username.IsUnset() {
		if err := setUsername(ctx, repo, userID, fields, patch.Username); err != nil {
			return nil, err
		}
	}
	if !patch.DisplayUsername.IsUnset() {
		SetNullable(fields, "display_username", patch.DisplayUsername)
	}
	return fields, nil
}

// ApplyUpdate 执行更新，并把"记录不存在"翻译为 404、唯一冲突翻译为具体错误码。
func ApplyUpdate(ctx context.Context, repo *Repository, id string, fields map[string]any) error {
	if len(fields) == 0 {
		return nil
	}

	if err := repo.UpdateFields(ctx, id, fields); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return errs.NotFound("The requested user does not exist")
		}
		if _, ok := errs.As(err); ok {
			return err
		}
		return MapUniqueConflict(err)
	}
	return nil
}

// SetNullable 把三态字段写入列；显式 null 或空白串都落为 NULL。
func SetNullable(fields map[string]any, column string, value omitnull.Val[string]) {
	if value.IsNull() {
		fields[column] = nil
		return
	}

	trimmed := strings.TrimSpace(value.MustGet())
	if trimmed == "" {
		fields[column] = nil
		return
	}
	fields[column] = trimmed
}

// FieldCannotBeNull 用于库层 NOT NULL、因而不能接受显式 null 的字段。
// 这里只声明"违反 not_null 规则"，文案由渲染层按请求语言从 lang/*/validation.toml 取。
func FieldCannotBeNull(field string) error {
	return errs.ValidationKeys(map[string][]errs.MessageKey{
		field: {{Key: "not_null"}},
	})
}

// MapUniqueConflict 把唯一索引冲突翻译成具体的业务错误码。
func MapUniqueConflict(err error) error {
	pgErr, ok := errors.AsType[*pgconn.PgError](err)
	if !ok || pgErr.Code != uniqueViolation {
		return errs.Internal(err)
	}

	switch pgErr.ConstraintName {
	case indexUserEmail:
		return errs.New(errs.CodeAuthEmailAlreadyExists, "An account with this email address already exists")
	case indexUserUsername:
		return errs.New(errs.CodeAuthUsernameTaken, "The username has already been taken")
	case indexUserPhoneNumber:
		// 手机号同样是可写列，撞索引时不该退化成通用 409——调用方需要知道是哪个字段。
		return errs.New(errs.CodeAuthPhoneNumberTaken, "The phone number has already been taken")
	default:
		return errs.Conflict("The user conflicts with an existing record")
	}
}

// setUsername 处理用户名的三态：显式 null 表示解绑，赋新值前需确认未被他人占用。
func setUsername(
	ctx context.Context,
	repo *Repository,
	userID string,
	fields map[string]any,
	value omitnull.Val[string],
) error {
	if value.IsNull() {
		fields["username"] = nil
		fields["display_username"] = nil
		return nil
	}

	normalised := NormaliseUsername(value.MustGet())
	// 这次查重只为给出友好错误，不构成并发保证：两个用户同时改名可能双双通过这里。
	// 真正的仲裁是唯一索引 idx_user_username，冲突由 MapUniqueConflict 兜底成同一个错误码。
	existing, err := repo.FindByUsername(ctx, normalised)
	switch {
	case err == nil && existing.ID != userID:
		return errs.New(errs.CodeAuthUsernameTaken, "The username has already been taken")
	case err != nil && !errors.Is(err, sql.ErrNoRows):
		return errs.Internal(err)
	}

	fields["username"] = normalised
	if _, ok := fields["display_username"]; !ok {
		fields["display_username"] = normalised
	}
	return nil
}
