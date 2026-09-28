// Package errs_test 以黑盒方式验证错误映射契约（api.md 5.2 与 4.2）。
package errs_test

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"reflect"
	"testing"

	"github.com/go-playground/validator/v10"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"bgin/internal/platform/errs"
)

// api.md 5.2 明文规定的两条文案，不得随意改动。
const (
	msgValidationFailed = "Validation failed"
	msgInternal         = "An unexpected error occurred"
)

// payload 覆盖嵌套结构与切片字段，用于验证字段路径的规范化。
type payload struct {
	Email string `json:"email" validate:"required,email"`
	Tags  []tag  `json:"tags" validate:"dive"`
}

type tag struct {
	Name string `json:"name" validate:"required"`
}

// TestMapBusinessError 校验业务错误按登记的状态码与错误码输出。
func TestMapBusinessError(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   errs.Code
	}{
		{"登录失败", errs.InvalidCredentials(), http.StatusUnauthorized, errs.CodeAuthInvalidCredentials},
		{"账号封禁", errs.New(errs.CodeAuthAccountBanned, "banned"), http.StatusForbidden, errs.CodeAuthAccountBanned},
		{"邮箱重复", errs.New(errs.CodeAuthEmailAlreadyExists, "dup"), http.StatusConflict, errs.CodeAuthEmailAlreadyExists},
		{"用户名占用", errs.New(errs.CodeAuthUsernameTaken, "taken"), http.StatusConflict, errs.CodeAuthUsernameTaken},
		{"令牌过期", errs.Unauthorized(errs.CodeAuthTokenExpired, "expired"), http.StatusUnauthorized, errs.CodeAuthTokenExpired},
		{"令牌已撤销", errs.Unauthorized(errs.CodeAuthTokenRevoked, "revoked"), http.StatusUnauthorized, errs.CodeAuthTokenRevoked},
		{"验证码错误", errs.New(errs.CodeAuthOTPInvalid, "bad code"), http.StatusUnprocessableEntity, errs.CodeAuthOTPInvalid},
		{"验证码过期", errs.New(errs.CodeAuthOTPExpired, "expired code"), http.StatusUnprocessableEntity, errs.CodeAuthOTPExpired},
		{"口令不匹配", errs.New(errs.CodeAuthPasswordMismatch, "mismatch"), http.StatusUnprocessableEntity, errs.CodeAuthPasswordMismatch},
		{"请求体格式", errs.BadRequest("bad"), http.StatusBadRequest, errs.CodeBadRequest},
		{"资源不存在", errs.NotFound("missing"), http.StatusNotFound, errs.CodeNotFound},
		{"无权访问", errs.Forbidden("nope"), http.StatusForbidden, errs.CodeForbidden},
		{"状态冲突", errs.Conflict("conflict"), http.StatusConflict, errs.CodeConflict},
		{"限流", errs.RateLimited("slow down"), http.StatusTooManyRequests, errs.CodeRateLimited},
		{"内部错误", errs.Internal(errors.New("boom")), http.StatusInternalServerError, errs.CodeInternal},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status, resp := errs.Map(tt.err)
			assert.Equal(t, tt.wantStatus, status)
			assert.Equal(t, string(tt.wantCode), resp.Code)
			assert.NotEmpty(t, resp.Message)
		})
	}
}

// TestMapValidationErrorNormalisesFieldPaths 校验未包装的 validator 错误也能给出
// 干净的字段路径：无根类型名、索引写成点号（api.md 5.2 的 users.0.email 形态）。
func TestMapValidationErrorNormalisesFieldPaths(t *testing.T) {
	instance := validator.New()
	instance.RegisterTagNameFunc(func(field reflect.StructField) string {
		if name := field.Tag.Get("json"); name != "" {
			return name
		}
		return field.Name
	})

	status, resp := errs.Map(instance.Struct(payload{Tags: []tag{{}}}))

	require.Equal(t, http.StatusUnprocessableEntity, status)
	assert.Equal(t, string(errs.CodeValidationFailed), resp.Code)
	assert.Equal(t, msgValidationFailed, resp.Message)
	assert.Contains(t, resp.Errors, "email")
	assert.Contains(t, resp.Errors, "tags.0.name", "实际字段键: %v", resp.Errors)
}

// TestMapPgUniqueViolation 校验唯一约束冲突被翻译成 409 且不泄漏数据库细节。
func TestMapPgUniqueViolation(t *testing.T) {
	status, resp := errs.Map(&pgconn.PgError{
		Code:           "23505",
		ConstraintName: "idx_user_email",
		Detail:         "Key (lower(email))=(a@b.c) already exists.",
	})

	assert.Equal(t, http.StatusConflict, status)
	assert.Equal(t, string(errs.CodeConflict), resp.Code)
	assert.NotContains(t, resp.Message, "already exists", "不应把数据库细节透给调用方")
}

// TestMapPgForeignKeyAndNotNull 校验外键与非空约束归为 409。
func TestMapPgForeignKeyAndNotNull(t *testing.T) {
	for _, code := range []string{"23503", "23502"} {
		status, _ := errs.Map(&pgconn.PgError{Code: code})
		assert.Equal(t, http.StatusConflict, status, "PgError %s", code)
	}
}

// TestMapNoRows 校验 sql.ErrNoRows 与 pgx.ErrNoRows 都归为 404。
func TestMapNoRows(t *testing.T) {
	for _, err := range []error{sql.ErrNoRows, pgx.ErrNoRows} {
		status, resp := errs.Map(err)
		assert.Equal(t, http.StatusNotFound, status)
		assert.Equal(t, string(errs.CodeNotFound), resp.Code)
	}
}

// TestMapUnexpectedError 校验未识别错误一律收敛为 500，且不泄漏原始信息。
func TestMapUnexpectedError(t *testing.T) {
	status, resp := errs.Map(errors.New("some internal detail leaked via driver"))

	assert.Equal(t, http.StatusInternalServerError, status)
	assert.Equal(t, string(errs.CodeInternal), resp.Code)
	assert.Equal(t, msgInternal, resp.Message)
	assert.NotContains(t, resp.Message, "driver")
}

// TestMapOOpsOnlyError 校验平台层直接抛出的、已登记错误码的 oops 错误。
func TestMapOOpsOnlyError(t *testing.T) {
	err := oops.Code(string(errs.CodeAuthOTPInvalid)).Public("The verification code is incorrect").New("otp")

	status, resp := errs.Map(err)
	assert.Equal(t, http.StatusUnprocessableEntity, status)
	assert.Equal(t, string(errs.CodeAuthOTPInvalid), resp.Code)
	assert.Equal(t, "The verification code is incorrect", resp.Message)
}

// TestMapUnregisteredOOpsCodeFallsBackTo500 校验未登记的错误码不会漏出成 2xx。
func TestMapUnregisteredOOpsCodeFallsBackTo500(t *testing.T) {
	status, resp := errs.Map(oops.Code("TOTALLY_UNKNOWN").New("x"))

	assert.Equal(t, http.StatusInternalServerError, status)
	assert.Equal(t, string(errs.CodeInternal), resp.Code)
}

// TestValidationCarriesFieldDetails 校验 422 响应体携带按字段路径归集的明细。
func TestValidationCarriesFieldDetails(t *testing.T) {
	status, resp := errs.Map(errs.Validation(map[string][]string{
		"authorization.role": {"The selected authorization.role is invalid."},
		"users.0.email":      {"The users.0.email field is required."},
	}))

	assert.Equal(t, http.StatusUnprocessableEntity, status)
	assert.Len(t, resp.Errors, 2)
	assert.Len(t, resp.Errors["users.0.email"], 1)
}

// TestWrapKeepsCause 校验错误包裹后仍可用 errors.Is 命中原始错误。
func TestWrapKeepsCause(t *testing.T) {
	err := errs.Wrap(errs.CodeNotFound, "missing", sql.ErrNoRows)
	require.ErrorIs(t, err, sql.ErrNoRows)

	extracted, ok := errs.As(err)
	require.True(t, ok)
	assert.Equal(t, errs.CodeNotFound, extracted.Code())
	assert.Equal(t, "missing", extracted.Message())
}

// TestWrapNilCauseDegradesToNew 校验空 cause 不会 panic 也不会丢失错误码。
func TestWrapNilCauseDegradesToNew(t *testing.T) {
	err := errs.Wrap(errs.CodeNotFound, "missing", nil)

	extracted, ok := errs.As(err)
	require.True(t, ok)
	assert.Equal(t, errs.CodeNotFound, extracted.Code())
}

// TestWithFieldAccumulates 校验同一字段的多条明细会累积而非覆盖。
func TestWithFieldAccumulates(t *testing.T) {
	err := errs.New(errs.CodeValidationFailed, msgValidationFailed).
		WithField("name", "first").
		WithField("name", "second")

	assert.Equal(t, []string{"first", "second"}, err.Fields()["name"])
}

// TestNormalizePath 校验索引路径改写为点号分隔。
func TestNormalizePath(t *testing.T) {
	assert.Equal(t, "users.0.email", errs.NormalizePath("users[0].email"))
	assert.Equal(t, "a.0.1.b", errs.NormalizePath("a[0][1].b"))
	assert.Equal(t, "plain", errs.NormalizePath("plain"))
}

// TestStatusCoversEveryDeclaredCode 校验每个公开错误码都有明确的状态码，
// 不会因为漏登记而悄悄退化成 500。
func TestStatusCoversEveryDeclaredCode(t *testing.T) {
	tests := []struct {
		code       errs.Code
		wantStatus int
	}{
		{errs.CodeBadRequest, http.StatusBadRequest},
		{errs.CodeValidationFailed, http.StatusUnprocessableEntity},
		{errs.CodeNotFound, http.StatusNotFound},
		{errs.CodeMethodNotAllowed, http.StatusMethodNotAllowed},
		{errs.CodeForbidden, http.StatusForbidden},
		{errs.CodeConflict, http.StatusConflict},
		{errs.CodeRateLimited, http.StatusTooManyRequests},
		{errs.CodeRequestTooLarge, http.StatusRequestEntityTooLarge},
		{errs.CodeInternal, http.StatusInternalServerError},
		{errs.CodeAuthInvalidCredentials, http.StatusUnauthorized},
		{errs.CodeAuthEmailAlreadyExists, http.StatusConflict},
		{errs.CodeAuthUsernameTaken, http.StatusConflict},
		{errs.CodeAuthAccountBanned, http.StatusForbidden},
		{errs.CodeAuthEmailNotVerified, http.StatusForbidden},
		{errs.CodeAuthOTPInvalid, http.StatusUnprocessableEntity},
		{errs.CodeAuthOTPExpired, http.StatusUnprocessableEntity},
		{errs.CodeAuthOTPTooManyAttempts, http.StatusTooManyRequests},
		{errs.CodeAuthOTPRateLimited, http.StatusTooManyRequests},
		{errs.CodeAuthTokenInvalid, http.StatusUnauthorized},
		{errs.CodeAuthTokenExpired, http.StatusUnauthorized},
		{errs.CodeAuthTokenRevoked, http.StatusUnauthorized},
		{errs.CodeAuthPasswordMismatch, http.StatusUnprocessableEntity},
		{errs.CodeAuthPasswordTooWeak, http.StatusUnprocessableEntity},
		{errs.CodeGatewayTimeout, http.StatusGatewayTimeout},
	}

	for _, tt := range tests {
		assert.Equal(t, tt.wantStatus, errs.Status(tt.code), "错误码 %s 的状态码映射不对", tt.code)
	}

	// 未登记的错误码必须兜底为 500，而不是漏成 2xx。
	assert.Equal(t, http.StatusInternalServerError, errs.Status(errs.Code("NOT_REGISTERED")))
}

// TestGatewayTimeoutMapping 校验 context.DeadlineExceeded 与 GatewayTimeout 映射为 504。
func TestGatewayTimeoutMapping(t *testing.T) {
	status, resp := errs.Map(context.DeadlineExceeded)
	assert.Equal(t, http.StatusGatewayTimeout, status)
	assert.Equal(t, string(errs.CodeGatewayTimeout), resp.Code)

	status, resp = errs.Map(errs.GatewayTimeout(context.DeadlineExceeded))
	assert.Equal(t, http.StatusGatewayTimeout, status)
	assert.Equal(t, string(errs.CodeGatewayTimeout), resp.Code)
}

// TestStackIsAvailableForLogging 校验 oops 堆栈可被提取，便于 500 日志定位。
func TestStackIsAvailableForLogging(t *testing.T) {
	err := errs.Internal(errors.New("boom"))
	assert.NotEmpty(t, errs.Stack(err))
}
