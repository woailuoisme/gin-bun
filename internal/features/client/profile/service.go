package profile

import (
	"context"
	"database/sql"
	"errors"

	"bgin/internal/platform/errs"
	"bgin/internal/shared/user"
)

// Deps 是档案域所需的依赖，由 main 装配后注入。
type Deps struct {
	Repo *user.Repository
}

// Service 实现前端用户的本人档案用例。
// 档案列的翻译与写入规则不在本包，而在共享内核 internal/shared/user。
type Service struct {
	repo *user.Repository
}

// NewService 构造档案服务。
func NewService(deps Deps) *Service { return &Service{repo: deps.Repo} }

// GetProfile 返回当前用户的档案。
func (s *Service) GetProfile(ctx context.Context, userID string) (user.Profile, error) {
	entity, err := s.repo.FindByID(ctx, userID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return user.Profile{}, errs.NotFound("The requested user does not exist")
		}
		return user.Profile{}, errs.Internal(err)
	}
	return user.ProjectProfile(entity), nil
}

// UpdateProfile 按 api.md 2.3 的三态语义局部更新当前用户档案。
func (s *Service) UpdateProfile(ctx context.Context, userID string, req UpdateProfileRequest) (user.Profile, error) {
	fields, err := user.CollectFields(ctx, s.repo, userID, req.fieldSet())
	if err != nil {
		return user.Profile{}, err
	}
	if err := user.ApplyUpdate(ctx, s.repo, userID, fields); err != nil {
		return user.Profile{}, err
	}
	return s.GetProfile(ctx, userID)
}
