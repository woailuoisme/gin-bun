package user

import (
	"context"
	"database/sql"
	"errors"

	"bgin/internal/platform/errs"
)

// Scope 声明档案更新的权限作用域。
type Scope string

const (
	// ScopeClient 声明客户端用户本人档案更新作用域，仅允许修改受限的展示档案字段。
	ScopeClient Scope = "client"
	// ScopeAdmin 声明管理端管理员更新作用域，具备完全字段可写权限。
	ScopeAdmin Scope = "admin"
)

// Service 提供高内聚、高杠杆的用户档案领域服务。
type Service struct {
	repo *Repository
}

// NewService 构造用户档案领域服务。
func NewService(repo *Repository) *Service {
	return &Service{repo: repo}
}

// GetProfile 获取指定用户的档案视图。
func (s *Service) GetProfile(ctx context.Context, userID string) (Profile, error) {
	entity, err := s.repo.FindByID(ctx, userID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Profile{}, errs.NotFound("The requested user does not exist")
		}
		return Profile{}, errs.Internal(err)
	}
	return ProjectProfile(entity), nil
}

// UpdateProfile 根据授权作用域执行档案的三态局部更新与唯一性冲突消解。
func (s *Service) UpdateProfile(ctx context.Context, userID string, patch FieldSet, scope Scope) (Profile, error) {
	fields, err := CollectFields(ctx, s.repo, userID, patch)
	if err != nil {
		return Profile{}, err
	}

	if scope == ScopeClient {
		// 客户端自助更新剔除全部受控列：判定依据就是仓储的可写白名单，
		// 不再另存一份"客户端禁写列"清单（两份清单必然漂移）。
		for column := range fields {
			if !writableColumns[column] {
				delete(fields, column)
			}
		}
	}

	if err := ApplyUpdate(ctx, s.repo, userID, fields); err != nil {
		return Profile{}, err
	}

	return s.GetProfile(ctx, userID)
}
