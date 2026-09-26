package admin

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/samber/lo"

	"bgin/internal/database/model"
	"bgin/internal/platform/errs"
	"bgin/internal/platform/hasher"
	"bgin/internal/platform/optional"
	"bgin/internal/platform/response"
	"bgin/internal/platform/validator"
	"bgin/internal/shared/user"
)

// Deps 是管理域所需的依赖，由 main 装配后注入。
// Repo 与客户端模块共用同一份仓库实例，见 ADR-0005。
type Deps struct {
	Repo   *user.Repository
	Hasher *hasher.Hasher
}

// Service 实现后台用户管理用例。
type Service struct {
	repo   *user.Repository
	hasher *hasher.Hasher
}

// NewService 构造管理服务。
func NewService(deps Deps) *Service {
	return &Service{repo: deps.Repo, hasher: deps.Hasher}
}

// ListUsers 返回分页用户列表，排序与过滤都走白名单。
func (s *Service) ListUsers(ctx context.Context, query ListUsersQuery) ([]User, response.Meta, error) {
	page, pageSize := normalisePagination(query)
	filter := user.ListFilter{
		Search: strings.TrimSpace(query.Search),
		Status: strings.TrimSpace(query.Status),
		Role:   strings.TrimSpace(query.Role),
		Sort:   user.NormaliseSort(query.Sort),
	}

	total, err := s.repo.Count(ctx, filter)
	if err != nil {
		return nil, response.Meta{}, errs.Internal(err)
	}

	users, err := s.repo.List(ctx, filter, (page-1)*pageSize, pageSize)
	if err != nil {
		return nil, response.Meta{}, errs.Internal(err)
	}

	items := lo.Map(users, func(entity model.User, _ int) User {
		return projectUser(&entity)
	})
	return items, response.NewMeta(page, pageSize, total), nil
}

// GetUser 返回管理视角的单个用户。
func (s *Service) GetUser(ctx context.Context, id string) (User, error) {
	entity, err := s.repo.FindByID(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return User{}, errs.NotFound("The requested user does not exist")
		}
		return User{}, errs.Internal(err)
	}
	return projectUser(entity), nil
}

// CreateUser 由管理员直接创建账号，初始口令由调用方指定。
func (s *Service) CreateUser(ctx context.Context, req CreateUserRequest) (User, error) {
	if err := validator.PasswordStrength("password", req.Password); err != nil {
		return User{}, err
	}

	entity := &model.User{
		Name:          strings.TrimSpace(req.Name),
		Email:         user.NormaliseEmail(req.Email),
		EmailVerified: req.EmailVerified,
		Image:         req.Image,
		Role:          req.Role,
		Status:        model.StatusActive,
	}
	if req.Username != nil {
		normalised := strings.ToLower(strings.TrimSpace(*req.Username))
		entity.Username = &normalised
		entity.DisplayUsername = &normalised
	}

	hash, err := s.hasher.Make(req.Password)
	if err != nil {
		return User{}, errs.Internal(err)
	}
	if err := s.repo.CreateWithCredential(ctx, entity, hash); err != nil {
		return User{}, user.MapUniqueConflict(err)
	}
	return projectUser(entity), nil
}

// UpdateUser 由管理员局部更新用户，含角色、状态与封禁信息。
func (s *Service) UpdateUser(ctx context.Context, id string, req UpdateUserRequest) (User, error) {
	fields, err := s.buildAdminFields(ctx, id, req)
	if err != nil {
		return User{}, err
	}
	if err := user.ApplyUpdate(ctx, s.repo, id, fields); err != nil {
		return User{}, err
	}
	return s.GetUser(ctx, id)
}

// DeleteUser 软删除用户。写 deleted_at 而非物理删除，从而让邮箱可以重新注册，
// 这正是 schema 里那批部分唯一索引的设计意图。
func (s *Service) DeleteUser(ctx context.Context, id string) error {
	if err := s.repo.SoftDelete(ctx, id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return errs.NotFound("The requested user does not exist")
		}
		return errs.Internal(err)
	}
	return nil
}

// buildAdminFields 把管理端更新的三态请求翻译成待更新列，并保证封禁字段自洽。
// 档案字段部分复用共享内核的映射，因此"哪些档案列可改"只有一处定义。
func (s *Service) buildAdminFields(ctx context.Context, id string, req UpdateUserRequest) (map[string]any, error) {
	fields, err := user.CollectFields(ctx, s.repo, id, req.fieldSet())
	if err != nil {
		return nil, err
	}

	if req.EmailVerified.IsSet() {
		fields["email_verified"] = *req.EmailVerified.Value
	}
	if err := setRequiredString(fields, "role", req.Role); err != nil {
		return nil, err
	}
	if err := setRequiredString(fields, "status", req.Status); err != nil {
		return nil, err
	}
	if req.Banned.IsSet() {
		fields["banned"] = *req.Banned.Value
	}
	if req.BanReason.Present {
		user.SetNullable(fields, "ban_reason", req.BanReason)
	}
	if req.BanExpires.Present {
		fields["ban_expires"] = nullableTime(req.BanExpires)
	}

	reconcileBanState(fields)
	return fields, nil
}

// setRequiredString 处理库层 NOT NULL、因而不接受显式 null 的字符串列。
func setRequiredString(fields map[string]any, column string, value optional.Optional[string]) error {
	if !value.Present {
		return nil
	}
	if value.Value == nil {
		return user.FieldCannotBeNull(column)
	}
	fields[column] = *value.Value
	return nil
}

// nullableTime 把三态时间字段转换为可写入列的值，并统一归一到 UTC。
func nullableTime(value optional.Optional[time.Time]) any {
	if value.Value == nil {
		return nil
	}
	return value.Value.UTC()
}

// reconcileBanState 消除 banned 布尔与 status 文本可能出现的自相矛盾组合。
// 置 status=banned 会自动带上 banned=true，反之亦然。
func reconcileBanState(fields map[string]any) {
	if status, ok := fields["status"].(string); ok && status == model.StatusBanned {
		fields["banned"] = true
	}
	if banned, ok := fields["banned"].(bool); ok && banned {
		fields["status"] = model.StatusBanned
	}
}

// normalisePagination 把分页参数收敛到 api.md 6.1 允许的范围。
func normalisePagination(query ListUsersQuery) (page, pageSize int) {
	page = max(query.Page, defaultPage)

	pageSize = query.PageSize
	if pageSize < 1 {
		pageSize = defaultPageSize
	}
	pageSize = min(pageSize, maxPageSize)
	return page, pageSize
}
