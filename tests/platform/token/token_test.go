// Package token_test 验证 JWT 双令牌的签发与校验契约（ADR-0001）。
package token_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"bgin/internal/config"
	"bgin/internal/platform/errs"
	"bgin/internal/platform/token"
)

// testSecret 是测试用的固定密钥，长度满足启动校验。
const testSecret = "0123456789abcdef0123456789abcdef"

// newIssuer 构造不依赖 Redis 的签发器，仅用于签发与校验路径。
func newIssuer(secret, issuer string, accessTTL time.Duration) *token.Issuer {
	return token.New(config.JWTConfig{
		Secret:     secret,
		Issuer:     issuer,
		AccessTTL:  accessTTL,
		RefreshTTL: 24 * time.Hour,
	}, nil)
}

// TestIssueAndParse 校验双令牌形态不同，且访问令牌载荷完整。
func TestIssueAndParse(t *testing.T) {
	issuer := newIssuer(testSecret, "bgin-test", 15*time.Minute)

	pair, err := issuer.Issue("u_1", "admin")
	require.NoError(t, err)
	assert.NotEqual(t, pair.AccessToken, pair.RefreshToken, "两类令牌必须不同")

	claims, err := issuer.Parse(pair.AccessToken, token.TypeAccess)
	require.NoError(t, err)
	assert.Equal(t, "u_1", claims.Subject)
	assert.Equal(t, "admin", claims.Role)
	assert.Equal(t, token.TypeAccess, claims.Type)
	assert.NotEmpty(t, claims.ID, "每枚令牌都要有唯一 jti 才能独立撤销")
	assert.True(t, pair.AccessExpiresAt.Before(pair.RefreshExpiresAt), "访问令牌寿命必须短于刷新令牌")
}

// TestParseRejectsCrossTypeUsage 校验刷新令牌不能当访问令牌用，反之亦然。
func TestParseRejectsCrossTypeUsage(t *testing.T) {
	issuer := newIssuer(testSecret, "bgin-test", 15*time.Minute)
	pair, err := issuer.Issue("u_1", "user")
	require.NoError(t, err)

	_, err = issuer.Parse(pair.RefreshToken, token.TypeAccess)
	require.Error(t, err)

	_, err = issuer.Parse(pair.AccessToken, token.TypeRefresh)
	require.Error(t, err)
}

// TestParseRejectsForeignSecret 校验换密钥后旧令牌立即失效。
func TestParseRejectsForeignSecret(t *testing.T) {
	pair, err := newIssuer(testSecret, "bgin-test", 15*time.Minute).Issue("u_1", "user")
	require.NoError(t, err)

	other := newIssuer("ffffffffffffffffffffffffffffffff", "bgin-test", 15*time.Minute)
	_, err = other.Parse(pair.AccessToken, token.TypeAccess)
	assert.Error(t, err)
}

// TestParseRejectsForeignIssuer 校验签发方不匹配被拒绝。
func TestParseRejectsForeignIssuer(t *testing.T) {
	pair, err := newIssuer(testSecret, "bgin-test", 15*time.Minute).Issue("u_1", "user")
	require.NoError(t, err)

	other := newIssuer(testSecret, "someone-else", 15*time.Minute)
	_, err = other.Parse(pair.AccessToken, token.TypeAccess)
	assert.Error(t, err)
}

// TestParseRejectsGarbage 校验畸形令牌被拒绝。
func TestParseRejectsGarbage(t *testing.T) {
	issuer := newIssuer(testSecret, "bgin-test", 15*time.Minute)

	for _, raw := range []string{"", "not-a-jwt", "a.b.c"} {
		_, err := issuer.Parse(raw, token.TypeAccess)
		assert.Error(t, err, "令牌 %q 应被拒绝", raw)
	}
}

// TestExpiredTokenUsesDedicatedCode 校验过期令牌返回专门的错误码而非泛化的无效错误。
func TestExpiredTokenUsesDedicatedCode(t *testing.T) {
	issuer := newIssuer(testSecret, "bgin-test", -time.Minute)

	pair, err := issuer.Issue("u_1", "user")
	require.NoError(t, err)

	_, err = issuer.Parse(pair.AccessToken, token.TypeAccess)
	require.Error(t, err)

	bizErr, ok := errs.As(err)
	require.True(t, ok, "应返回可映射为 HTTP 响应的业务错误")
	assert.Equal(t, errs.CodeAuthTokenExpired, bizErr.Code())
	assert.Equal(t, 401, errs.Status(bizErr.Code()))
}

// TestRoleClaimIsCarried 校验角色声明写入令牌，供鉴权中间件免查库使用（ADR-0003）。
func TestRoleClaimIsCarried(t *testing.T) {
	issuer := newIssuer(testSecret, "bgin-test", 15*time.Minute)

	pair, err := issuer.Issue("u_1", "admin")
	require.NoError(t, err)

	claims, err := issuer.Parse(pair.AccessToken, token.TypeAccess)
	require.NoError(t, err)
	assert.Equal(t, "admin", claims.Role)
}

// TestEachIssueHasUniqueJTI 校验每次签发的 jti 都不同，否则撤销会误伤其他令牌。
func TestEachIssueHasUniqueJTI(t *testing.T) {
	issuer := newIssuer(testSecret, "bgin-test", 15*time.Minute)

	first, err := issuer.Issue("u_1", "user")
	require.NoError(t, err)
	second, err := issuer.Issue("u_1", "user")
	require.NoError(t, err)

	assert.NotEqual(t, first.AccessToken, second.AccessToken)
}
