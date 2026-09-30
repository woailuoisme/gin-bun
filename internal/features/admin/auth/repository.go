package adminauth

import (
	"context"

	"github.com/uptrace/bun"

	"bgin/internal/platform/authentication"
	"bgin/internal/platform/hasher"
	"bgin/internal/shared/models"
	shareduser "bgin/internal/shared/user"
)

// Repository 定义管理员认证域的数据访问接口。
type Repository interface {
	authentication.UserProvider[*models.User]
	authentication.LoginRecorder
	FindByEmail(ctx context.Context, email string) (*models.User, error)
	FindByID(ctx context.Context, id string) (*models.User, error)
	FindCredentialAccount(ctx context.Context, userID string) (*models.Account, error)
	UpdatePassword(ctx context.Context, userID, passwordHash string) error
	RecordLogin(ctx context.Context, userID, method string) error
}

// NewRepository 构造管理员认证数据仓库，直接复用 shareduser.Repository。
func NewRepository(db *bun.DB, h *hasher.Hasher) Repository {
	return shareduser.NewRepository(db, h)
}
