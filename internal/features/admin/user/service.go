package admin

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/samber/lo"

	"bgin/internal/platform/errs"
	"bgin/internal/platform/hasher"
	"bgin/internal/platform/optional"
	"bgin/internal/platform/response"
	"bgin/internal/platform/validator"
	"bgin/internal/shared/enums"
	"bgin/internal/shared/models"
	"bgin/internal/shared/user"
)

// Deps 声明用户管理服务的外部依赖。
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
	page, pageSize := NormalisePagination(query.Page, query.PageSize)
	filter := user.ListFilter{
		Search: strings.TrimSpace(query.Search),
		Status: enums.Status(strings.TrimSpace(string(query.Status))),
		Role:   enums.Role(strings.TrimSpace(string(query.Role))),
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

	items := lo.Map(users, func(entity models.User, _ int) User {
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

	entity := &models.User{
		Name:          strings.TrimSpace(req.Name),
		Email:         user.NormaliseEmail(req.Email),
		EmailVerified: req.EmailVerified,
		Image:         req.Image,
		Role:          string(req.Role),
		Status:        string(enums.StatusActive),
	}
	if req.Username != nil {
		normalised := user.NormaliseUsername(*req.Username)
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

// DeleteUser 软删除指定用户。
func (s *Service) DeleteUser(ctx context.Context, id string) error {
	if err := s.repo.SoftDelete(ctx, id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return errs.NotFound("The requested user does not exist")
		}
		return errs.Internal(err)
	}
	return nil
}

// buildAdminFields 把管理端三态更新请求转换为待更新字段映射。
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

// setRequiredString 处理库层 NOT NULL、因而不接受显式 null 的字符串或枚举列。
func setRequiredString[T ~string](fields map[string]any, column string, value optional.Optional[T]) error {
	if !value.Present {
		return nil
	}
	if value.Value == nil {
		return user.FieldCannotBeNull(column)
	}
	fields[column] = string(*value.Value)
	return nil
}

// nullableTime 把三态时间字段转换为可写入列的值，并统一归一到 UTC。
func nullableTime(value optional.Optional[time.Time]) any {
	if value.Value == nil {
		return nil
	}
	return value.Value.UTC()
}

// reconcileBanState 保证 banned 布尔值与 status 状态相互协同一致。
func reconcileBanState(fields map[string]any) {
	if status, ok := fields["status"].(string); ok && status == string(enums.StatusBanned) {
		fields["banned"] = true
	}
	if banned, ok := fields["banned"].(bool); ok && banned {
		fields["status"] = string(enums.StatusBanned)
	}
}

// NormalisePagination 将分页参数收敛到允许的安全区间。
func NormalisePagination(page, pageSize int) (int, int) {
	page = min(max(page, defaultPage), maxPage)
	if pageSize < 1 {
		pageSize = defaultPageSize
	}
	return page, min(pageSize, maxPageSize)
}
