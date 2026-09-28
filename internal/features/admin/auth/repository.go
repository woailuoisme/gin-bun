package adminauth

import (
	"context"

	"github.com/uptrace/bun"

	"bgin/internal/models"
	shareduser "bgin/internal/shared/user"
)

// Repository 定义管理员认证域的数据访问接口。
type Repository interface {
	FindByEmail(ctx context.Context, email string) (*models.User, error)
	FindByID(ctx context.Context, id string) (*models.User, error)
	FindCredentialAccount(ctx context.Context, userID string) (*models.Account, error)
	UpdatePassword(ctx context.Context, userID, passwordHash string) error
	RecordLogin(ctx context.Context, userID, method string) error
}

// repository 是 Repository 的具体实现，内嵌 shareduser.Repository 复用共享内核的数据访问。
type repository struct {
	*shareduser.Repository
}

// NewRepository 构造管理员认证数据仓库。
func NewRepository(db *bun.DB) Repository {
	return &repository{
		Repository: shareduser.NewRepository(db),
	}
}
