package adminauth

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"bgin/internal/models"
	"bgin/internal/platform/errs"
	"bgin/internal/platform/hasher"
	"bgin/internal/platform/identity"
	"bgin/internal/platform/logger"
	"bgin/internal/platform/token"
	"bgin/internal/platform/validator"
	"bgin/internal/shared/enums"
	shareduser "bgin/internal/shared/user"
)

// Deps 声明管理员认证服务的外部依赖。
type Deps struct {
	Repo   Repository
	Tokens *token.Issuer
	Hasher *hasher.Hasher
}

// Service 实现后台管理员认证的业务逻辑。
type Service struct {
	repo   Repository
	tokens *token.Issuer
	hasher *hasher.Hasher
}

// NewService 构造 Service。
func NewService(deps Deps) *Service {
	return &Service{
		repo:   deps.Repo,
		tokens: deps.Tokens,
		hasher: deps.Hasher,
	}
}

// SignInEmail 管理员邮箱密码登录。
func (s *Service) SignInEmail(ctx context.Context, req SignInEmailRequest) (SessionResponse, error) {
	user, err := s.repo.FindByEmail(ctx, req.Email)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			logger.FromContext(ctx).Info().Str("email", req.Email).Msg("admin login failed: account not found")
			return SessionResponse{}, errs.InvalidCredentials()
		}
		return SessionResponse{}, errs.Internal(err)
	}

	// 权限角色门禁：非系统管理员一律返回凭据错误，防止账号角色与存在性枚举。
	if user.Role != string(enums.RoleAdmin) {
		logger.FromContext(ctx).Warn().Str("email", req.Email).Str("role", user.Role).Msg("non-admin account attempted admin login")
		return SessionResponse{}, errs.InvalidCredentials()
	}

	// 账号封禁与活跃状态拦截。
	if err := s.guardAccountState(user); err != nil {
		return SessionResponse{}, err
	}

	account, err := s.repo.FindCredentialAccount(ctx, user.ID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return SessionResponse{}, errs.InvalidCredentials()
		}
		return SessionResponse{}, errs.Internal(err)
	}
	if account.Password == nil {
		return SessionResponse{}, errs.InvalidCredentials()
	}

	matched, err := s.hasher.Check(*account.Password, req.Password)
	if err != nil {
		return SessionResponse{}, errs.Internal(err)
	}
	if !matched {
		return SessionResponse{}, errs.InvalidCredentials()
	}

	pair, err := s.tokens.Issue(user.ID, user.Role)
	if err != nil {
		return SessionResponse{}, errs.Internal(err)
	}

	s.recordLogin(ctx, user.ID, string(enums.ProviderCredential))

	return SessionResponse{
		Token:        pair.AccessToken,
		RefreshToken: pair.RefreshToken,
		ExpiresIn:    int(time.Until(pair.AccessExpiresAt).Seconds()),
		User:         shareduser.ProjectProfile(user),
	}, nil
}

// GetSession 获取当前有效会话的管理员资料。
func (s *Service) GetSession(ctx context.Context, userID string) (GetSessionResponse, error) {
	user, err := s.repo.FindByID(ctx, userID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return GetSessionResponse{}, errs.Unauthorized(errs.CodeAuthTokenInvalid, "The token is invalid")
		}
		return GetSessionResponse{}, errs.Internal(err)
	}

	if user.Role != string(enums.RoleAdmin) {
		return GetSessionResponse{}, errs.Forbidden("Access restricted to administrators")
	}

	if err := s.guardAccountState(user); err != nil {
		return GetSessionResponse{}, err
	}

	return GetSessionResponse{User: shareduser.ProjectProfile(user)}, nil
}

// Refresh 刷新管理员双令牌，实现令牌轮换。
func (s *Service) Refresh(ctx context.Context, req RefreshTokenRequest) (SessionResponse, error) {
	claims, err := s.tokens.Parse(strings.TrimSpace(req.RefreshToken), token.TypeRefresh)
	if err != nil {
		return SessionResponse{}, err
	}

	revoked, err := s.tokens.IsRevoked(ctx, claims.ID)
	if err != nil {
		return SessionResponse{}, errs.Internal(err)
	}
	if revoked {
		return SessionResponse{}, errs.Unauthorized(errs.CodeAuthTokenRevoked, "The token has been revoked")
	}

	user, err := s.repo.FindByID(ctx, claims.Subject)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return SessionResponse{}, errs.Unauthorized(errs.CodeAuthTokenInvalid, "The token is invalid")
		}
		return SessionResponse{}, errs.Internal(err)
	}

	if user.Role != string(enums.RoleAdmin) {
		return SessionResponse{}, errs.Forbidden("Access restricted to administrators")
	}

	if err := s.guardAccountState(user); err != nil {
		return SessionResponse{}, err
	}

	if err := s.tokens.Revoke(ctx, claims); err != nil {
		return SessionResponse{}, errs.Internal(err)
	}

	pair, err := s.tokens.Issue(user.ID, user.Role)
	if err != nil {
		return SessionResponse{}, errs.Internal(err)
	}

	return SessionResponse{
		Token:        pair.AccessToken,
		RefreshToken: pair.RefreshToken,
		ExpiresIn:    int(time.Until(pair.AccessExpiresAt).Seconds()),
		User:         shareduser.ProjectProfile(user),
	}, nil
}

// SignOut 登出并作废当前管理员访问令牌与可选的刷新令牌。
func (s *Service) SignOut(ctx context.Context, id identity.Identity, req SignOutRequest) error {
	if err := s.tokens.RevokeByID(ctx, id.TokenID, id.TokenExpiresAt); err != nil {
		return errs.Internal(err)
	}

	if req.RefreshToken == nil || strings.TrimSpace(*req.RefreshToken) == "" {
		return nil
	}

	claims := s.parseRefreshToken(strings.TrimSpace(*req.RefreshToken))
	if claims == nil || claims.Subject != id.UserID {
		logger.FromContext(ctx).Info().Msg("refresh token invalid or mismatched on logout, skipped")
		return nil
	}

	if err := s.tokens.Revoke(ctx, claims); err != nil {
		return errs.Internal(err)
	}
	return nil
}

// parseRefreshToken 解析刷新令牌，失败时返回 nil。
func (s *Service) parseRefreshToken(raw string) *token.Claims {
	claims, err := s.tokens.Parse(raw, token.TypeRefresh)
	if err != nil {
		return nil
	}
	return claims
}

// ChangePassword 修改当前管理员登录密码。
func (s *Service) ChangePassword(ctx context.Context, userID string, req ChangePasswordRequest) error {
	if err := validator.PasswordStrength("newPassword", req.NewPassword); err != nil {
		return err
	}

	user, err := s.repo.FindByID(ctx, userID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return errs.Unauthorized(errs.CodeAuthTokenInvalid, "The token is invalid")
		}
		return errs.Internal(err)
	}

	if user.Role != string(enums.RoleAdmin) {
		return errs.Forbidden("Access restricted to administrators")
	}

	if err := s.guardAccountState(user); err != nil {
		return err
	}

	account, err := s.repo.FindCredentialAccount(ctx, userID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return errs.New(errs.CodeAuthPasswordMismatch, "The current password is incorrect")
		}
		return errs.Internal(err)
	}
	if account.Password == nil {
		return errs.New(errs.CodeAuthPasswordMismatch, "The current password is incorrect")
	}

	matched, err := s.hasher.Check(*account.Password, req.CurrentPassword)
	if err != nil {
		return errs.Internal(err)
	}
	if !matched {
		return errs.New(errs.CodeAuthPasswordMismatch, "The current password is incorrect")
	}

	hash, err := s.hasher.Make(req.NewPassword)
	if err != nil {
		return errs.Internal(err)
	}

	if err := s.repo.UpdatePassword(ctx, userID, hash); err != nil {
		return errs.Internal(err)
	}
	return nil
}

// guardAccountState 检查账号封禁与活跃状态。
func (s *Service) guardAccountState(user *models.User) error {
	if user.IsBanned(time.Now().UTC()) || user.Status == string(enums.StatusBanned) {
		return errs.New(errs.CodeAuthAccountBanned, "The account has been banned")
	}
	if user.Status != string(enums.StatusActive) {
		return errs.Forbidden("The account is not active")
	}
	return nil
}

// recordLogin 记录最近登录信息；失败不影响登录本身。
func (s *Service) recordLogin(ctx context.Context, userID, method string) {
	if err := s.repo.RecordLogin(ctx, userID, method); err != nil {
		logger.FromContext(ctx).Warn().Err(err).Str("user_id", userID).Msg("failed to record admin login metadata")
	}
}
