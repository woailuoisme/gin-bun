// Package auth 实现注册、登录、登出、令牌续签与密码重置等认证用例。
package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"bgin/internal/config"
	"bgin/internal/models"
	"bgin/internal/platform/cache"
	"bgin/internal/platform/errs"
	"bgin/internal/platform/hasher"
	"bgin/internal/platform/i18n"
	"bgin/internal/platform/identity"
	"bgin/internal/platform/logger"
	"bgin/internal/platform/mailer"
	"bgin/internal/platform/token"
	"bgin/internal/platform/validator"
	shareduser "bgin/internal/shared/user"
)

// Deps 是认证服务所需的依赖，由 main 装配后注入。
type Deps struct {
	Repo       *Repository
	Tokens     *token.Issuer
	Hasher     *hasher.Hasher
	Cache      *cache.Client
	Mailer     mailer.Mailer
	Translator *i18n.Translator
	Config     *config.Config
}

// Service 实现认证用例。
type Service struct {
	repo       *Repository
	tokens     *token.Issuer
	hasher     *hasher.Hasher
	mailer     mailer.Mailer
	translator *i18n.Translator
	cfg        *config.Config
	verifier   *verifier
}

// NewService 构造认证服务。
func NewService(deps Deps) *Service {
	return &Service{
		repo:       deps.Repo,
		tokens:     deps.Tokens,
		hasher:     deps.Hasher,
		mailer:     deps.Mailer,
		translator: deps.Translator,
		cfg:        deps.Config,
		verifier:   newVerifier(deps.Repo, deps.Cache, deps.Config),
	}
}

// SignUpEmail 注册邮箱密码账号，并按 Q15 的约定直接完成登录。
func (s *Service) SignUpEmail(ctx context.Context, req SignUpEmailRequest) (SessionResponse, error) {
	if err := validator.PasswordStrength("password", req.Password); err != nil {
		return SessionResponse{}, err
	}

	user := &models.User{
		Name:   strings.TrimSpace(req.Name),
		Email:  shareduser.NormaliseEmail(req.Email),
		Image:  req.Image,
		Role:   models.RoleUser,
		Status: models.StatusActive,
	}
	assignUsername(user, req.Username)

	hash, err := s.hasher.Make(req.Password)
	if err != nil {
		return SessionResponse{}, errs.Internal(err)
	}
	if err := s.repo.CreateWithCredential(ctx, user, hash); err != nil {
		return SessionResponse{}, shareduser.MapUniqueConflict(err)
	}

	session, err := s.issueSession(user)
	if err != nil {
		return SessionResponse{}, err
	}
	s.recordLogin(ctx, user.ID, models.ProviderCredential)
	return session, nil
}

// SignInEmail 邮箱密码登录。
func (s *Service) SignInEmail(ctx context.Context, req SignInEmailRequest) (SessionResponse, error) {
	user, err := s.repo.FindByEmail(ctx, req.Email)
	if err != nil {
		return SessionResponse{}, s.translateLookupFailure(ctx, err, req.Email)
	}
	return s.signInWithCredentials(ctx, user, req.Password)
}

// SignInUsername 用户名密码登录。
func (s *Service) SignInUsername(ctx context.Context, req SignInUsernameRequest) (SessionResponse, error) {
	user, err := s.repo.FindByUsername(ctx, req.Username)
	if err != nil {
		return SessionResponse{}, s.translateLookupFailure(ctx, err, req.Username)
	}
	return s.signInWithCredentials(ctx, user, req.Password)
}

// SignOut 作废当前访问令牌；同时传入刷新令牌时一并作废，否则续签通道仍然敞开。
func (s *Service) SignOut(ctx context.Context, id identity.Identity, req SignOutRequest) error {
	if err := s.tokens.RevokeByID(ctx, id.TokenID, id.TokenExpiresAt); err != nil {
		return errs.Internal(err)
	}

	if req.RefreshToken == nil || strings.TrimSpace(*req.RefreshToken) == "" {
		return nil
	}

	claims := s.parseRefreshToken(strings.TrimSpace(*req.RefreshToken))
	if claims == nil || claims.Subject != id.UserID {
		// 刷新令牌已失效或不属于当前用户时静默跳过：访问令牌已经作废，登出目标已达成。
		logger.FromContext(ctx).Info().Msg("refresh token invalid on logout, skipped")
		return nil
	}
	if err := s.tokens.Revoke(ctx, claims); err != nil {
		return errs.Internal(err)
	}
	return nil
}

// parseRefreshToken 尽力解析刷新令牌。解析失败返回 nil 而不返回 error，
// 因为在登出场景下"令牌本就无效"不算失败，调用方据此静默跳过即可。
func (s *Service) parseRefreshToken(raw string) *token.Claims {
	claims, err := s.tokens.Parse(raw, token.TypeRefresh)
	if err != nil {
		return nil
	}
	return claims
}

// GetSession 返回当前用户。纯只读、不续签，令牌过期即 401（Q13）。
func (s *Service) GetSession(ctx context.Context, userID string) (GetSessionResponse, error) {
	user, err := s.repo.FindByID(ctx, userID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return GetSessionResponse{}, errs.Unauthorized(errs.CodeAuthTokenInvalid, "The token is invalid")
		}
		return GetSessionResponse{}, errs.Internal(err)
	}
	if err := s.guardAccountState(user); err != nil {
		return GetSessionResponse{}, err
	}
	return GetSessionResponse{User: projectUser(user)}, nil
}

// Refresh 用刷新令牌换取新的一对令牌，旧的刷新令牌立即失效（轮换）。
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

	if err := s.tokens.Revoke(ctx, claims); err != nil {
		return SessionResponse{}, errs.Internal(err)
	}
	// 角色取库里最新值而非旧令牌声明，因此改角色在续签时自动生效。
	return s.issueSession(user)
}

// ChangePassword 校验当前口令后写入新口令。
// 已签发的令牌不会因此失效——这是 ADR-0001 明确记录的取舍。
func (s *Service) ChangePassword(ctx context.Context, userID string, req ChangePasswordRequest) error {
	if err := validator.PasswordStrength("newPassword", req.NewPassword); err != nil {
		return err
	}

	account, err := s.repo.FindCredentialAccount(ctx, userID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return noCredentialError()
		}
		return errs.Internal(err)
	}
	if account.Password == nil {
		return noCredentialError()
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

// ForgetPassword 发送携带一次性令牌的重置链接。
func (s *Service) ForgetPassword(ctx context.Context, language string, req ForgetPasswordRequest) (StatusResponse, error) {
	return s.sendResetCredential(ctx, language, purposeResetLink, req.Email)
}

// RequestPasswordReset 发送 6 位重置验证码。它与 ForgetPassword 是两条不同的路径：
// 前者产出可点击的链接，后者产出需用户抄回的短码（见 CONTEXT.md）。
func (s *Service) RequestPasswordReset(ctx context.Context, language string, req RequestPasswordResetRequest) (StatusResponse, error) {
	return s.sendResetCredential(ctx, language, purposeResetOTP, req.Email)
}

// ResetPassword 处理链接令牌与验证码两条互斥的重置路径。
func (s *Service) ResetPassword(ctx context.Context, req ResetPasswordRequest) (StatusResponse, error) {
	if err := validator.PasswordStrength("newPassword", req.NewPassword); err != nil {
		return StatusResponse{}, err
	}

	user, err := s.resolveResetTarget(ctx, req)
	if err != nil {
		return StatusResponse{}, err
	}

	hash, err := s.hasher.Make(req.NewPassword)
	if err != nil {
		return StatusResponse{}, errs.Internal(err)
	}
	if err := s.repo.UpdatePassword(ctx, user.ID, hash); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return StatusResponse{}, noCredentialError()
		}
		return StatusResponse{}, errs.Internal(err)
	}
	return StatusResponse{Status: true}, nil
}

// SendVerificationOTP 发送邮箱验证码。
func (s *Service) SendVerificationOTP(ctx context.Context, language string, req SendVerificationOTPRequest) (StatusResponse, error) {
	user, err := s.repo.FindByEmail(ctx, req.Email)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// 静默成功：不向未认证调用方暴露邮箱是否已注册。
			logger.FromContext(ctx).Info().Msg("verification requested for non-existent email, ignored")
			return StatusResponse{Status: true}, nil
		}
		return StatusResponse{}, errs.Internal(err)
	}
	if user.EmailVerified {
		return StatusResponse{Status: true}, nil
	}

	ttl := s.cfg.Auth.OTPTTL
	code, err := s.verifier.issueCode(ctx, purposeEmailVerify, user.Email, ttl)
	if err != nil {
		return StatusResponse{}, errs.Internal(err)
	}
	if err := s.sendMail(ctx, language, user, mailKeyVerifyEmail, code, ttl); err != nil {
		return StatusResponse{}, errs.Internal(err)
	}
	return StatusResponse{Status: true}, nil
}

// VerifyEmail 核销验证码并置位邮箱验证状态。
func (s *Service) VerifyEmail(ctx context.Context, req VerifyEmailRequest) (StatusResponse, error) {
	email := shareduser.NormaliseEmail(req.Email)
	if err := s.verifier.consume(ctx, purposeEmailVerify, email, strings.TrimSpace(req.OTP)); err != nil {
		return StatusResponse{}, err
	}

	user, err := s.repo.FindByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return StatusResponse{}, errs.NotFound("No account is associated with this email address")
		}
		return StatusResponse{}, errs.Internal(err)
	}
	if err := s.repo.MarkEmailVerified(ctx, user.ID); err != nil {
		return StatusResponse{}, errs.Internal(err)
	}
	return StatusResponse{Status: true}, nil
}

// 邮件模板键，与 lang/<语言>/mail.toml 中的键前缀一一对应。
const (
	mailKeyVerifyEmail = "verify_email"
	mailKeyResetLink   = "reset_link"
	mailKeyResetOTP    = "reset_otp"
)

// signInWithCredentials 校验本地密码凭据并签发令牌。
func (s *Service) signInWithCredentials(ctx context.Context, user *models.User, password string) (SessionResponse, error) {
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

	matched, err := s.hasher.Check(*account.Password, password)
	if err != nil {
		return SessionResponse{}, errs.Internal(err)
	}
	if !matched {
		return SessionResponse{}, errs.InvalidCredentials()
	}

	session, err := s.issueSession(user)
	if err != nil {
		return SessionResponse{}, err
	}
	s.recordLogin(ctx, user.ID, models.ProviderCredential)
	return session, nil
}

// issueSession 校验账号状态后签发双令牌并组装响应体，不写库。
func (s *Service) issueSession(user *models.User) (SessionResponse, error) {
	if err := s.guardAccountState(user); err != nil {
		return SessionResponse{}, err
	}

	pair, err := s.tokens.Issue(user.ID, user.Role)
	if err != nil {
		return SessionResponse{}, errs.Internal(err)
	}

	return SessionResponse{
		Token:        pair.AccessToken,
		RefreshToken: pair.RefreshToken,
		ExpiresIn:    int(time.Until(pair.AccessExpiresAt).Seconds()),
		User:         projectUser(user),
	}, nil
}

// guardAccountState 拒绝封禁、未激活与（开启开关时）未验证邮箱的账号。
func (s *Service) guardAccountState(user *models.User) error {
	if user.IsBanned(time.Now().UTC()) || user.Status == models.StatusBanned {
		return errs.New(errs.CodeAuthAccountBanned, "The account has been banned")
	}
	if user.Status != models.StatusActive {
		return errs.Forbidden("The account is not active")
	}
	if s.cfg.Auth.RequireEmailVerification && !user.EmailVerified {
		return errs.New(errs.CodeAuthEmailNotVerified, "The email address has not been verified")
	}
	return nil
}

// translateLookupFailure 把"账号不存在"统一翻译为凭证错误，避免泄漏账号是否存在。
func (s *Service) translateLookupFailure(ctx context.Context, err error, identifier string) error {
	if errors.Is(err, sql.ErrNoRows) {
		logger.FromContext(ctx).Info().Str("identifier", identifier).Msg("login failed: account not found")
		return errs.InvalidCredentials()
	}
	return errs.Internal(err)
}

// recordLogin 记录最近登录信息；失败不影响登录本身。
func (s *Service) recordLogin(ctx context.Context, userID, method string) {
	if err := s.repo.RecordLogin(ctx, userID, method); err != nil {
		logger.FromContext(ctx).Warn().Err(err).Str("user_id", userID).Msg("failed to record last login")
	}
}

// sendResetCredential 签发重置凭据并投递邮件。邮箱不存在时静默返回成功。
func (s *Service) sendResetCredential(ctx context.Context, language, purpose, email string) (StatusResponse, error) {
	user, err := s.repo.FindByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			logger.FromContext(ctx).Info().Msg("password reset requested for non-existent email, ignored")
			return StatusResponse{Status: true}, nil
		}
		return StatusResponse{}, errs.Internal(err)
	}

	code, mailKey, ttl, err := s.issueResetCredential(ctx, purpose, user.Email)
	if err != nil {
		return StatusResponse{}, errs.Internal(err)
	}
	if err := s.sendMail(ctx, language, user, mailKey, code, ttl); err != nil {
		return StatusResponse{}, errs.Internal(err)
	}
	return StatusResponse{Status: true}, nil
}

// issueResetCredential 按用途签发重置凭据，返回明文、邮件模板键与寿命。
func (s *Service) issueResetCredential(ctx context.Context, purpose, email string) (code, mailKey string, ttl time.Duration, err error) {
	switch purpose {
	case purposeResetLink:
		ttl = s.cfg.Auth.PasswordResetTTL
		code, err = s.verifier.issueToken(ctx, purpose, email, ttl)
		mailKey = mailKeyResetLink
	case purposeResetOTP:
		ttl = s.cfg.Auth.OTPTTL
		code, err = s.verifier.issueCode(ctx, purpose, email, ttl)
		mailKey = mailKeyResetOTP
	default:
		err = errs.Internal(fmt.Errorf("unknown reset purpose %q", purpose))
	}
	return code, mailKey, ttl, err
}

// resolveResetTarget 核销凭据并定位待重置的账号。
func (s *Service) resolveResetTarget(ctx context.Context, req ResetPasswordRequest) (*models.User, error) {
	email := shareduser.NormaliseEmail(req.Email)

	purpose, credential := purposeResetOTP, ""
	switch {
	case req.Token != nil && strings.TrimSpace(*req.Token) != "":
		purpose, credential = purposeResetLink, strings.TrimSpace(*req.Token)
	case req.OTP != nil && strings.TrimSpace(*req.OTP) != "":
		credential = strings.TrimSpace(*req.OTP)
	default:
		return nil, errs.ValidationKeys(map[string][]errs.MessageKey{
			"token": {{Key: "token_or_code_required"}},
		})
	}

	if err := s.verifier.consume(ctx, purpose, email, credential); err != nil {
		return nil, err
	}

	user, err := s.repo.FindByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errs.NotFound("No account is associated with this email address")
		}
		return nil, errs.Internal(err)
	}
	return user, nil
}

// sendMail 渲染并投递邮件；重置链接由前端站点根地址拼接而成。
func (s *Service) sendMail(ctx context.Context, language string, user *models.User, mailKey, code string, ttl time.Duration) error {
	lang := s.translator.Language(language)
	vars := map[string]string{
		"Name": user.Name,
		"Code": code,
		"TTL":  strconv.Itoa(int(ttl.Minutes())),
		"Link": s.resetLink(user.Email, code),
	}

	subject, body := s.translator.Mail(lang, mailKey, vars)
	return s.mailer.Send(ctx, mailer.Message{To: user.Email, Subject: subject, Body: body})
}

// resetLink 拼接前端重置页面地址；邮箱必须一并带上，否则无法定位凭据。
func (s *Service) resetLink(email, token string) string {
	query := url.Values{"token": {token}, "email": {email}}
	return strings.TrimRight(s.cfg.App.URL, "/") + "/reset-password?" + query.Encode()
}

// assignUsername 写入规范化后的用户名。字符集与保留字已在校验层把关，此处只做大小写归一。
func assignUsername(user *models.User, username *string) {
	if username == nil {
		return
	}
	normalised := shareduser.NormaliseUsername(*username)
	user.Username = &normalised
	user.DisplayUsername = &normalised
}

// noCredentialError 用于本地密码凭据缺失的场景。
func noCredentialError() error {
	return errs.NotFound("No password credential is associated with this account")
}
