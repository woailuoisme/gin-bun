// Package errs 定义统一的错误码、错误类型与 HTTP 映射。
package errs

import (
	"net/http"
	"strings"
)

// Code 是写入响应体 code 字段的机器可读错误码。
type Code string

// 通用错误码。这些值必须与 api.md 5.2 的描述保持一致。
const (
	CodeBadRequest       Code = "BAD_REQUEST"
	CodeValidationFailed Code = "VALIDATION_FAILED"
	CodeNotFound         Code = "NOT_FOUND"
	CodeMethodNotAllowed Code = "METHOD_NOT_ALLOWED"
	CodeForbidden        Code = "FORBIDDEN"
	CodeConflict         Code = "CONFLICT"
	CodeRateLimited      Code = "RATE_LIMITED"
	// CodeRequestTooLarge 对应请求体超过服务端上限（见 middleware.BodyLimit）。
	// 单独成码而不是并入 BAD_REQUEST：调用方需要据此判断"重试无用、必须改小载荷"。
	CodeRequestTooLarge Code = "REQUEST_TOO_LARGE"
	CodeInternal        Code = "INTERNAL_SERVER_ERROR"
)

// 认证域错误码。前缀 AUTH_ 用于把认证失败与业务失败区分开。
const (
	CodeAuthInvalidCredentials Code = "AUTH_INVALID_CREDENTIALS"
	CodeAuthEmailAlreadyExists Code = "AUTH_EMAIL_ALREADY_EXISTS"
	CodeAuthUsernameTaken      Code = "AUTH_USERNAME_TAKEN"
	CodeAuthPhoneNumberTaken   Code = "AUTH_PHONE_NUMBER_TAKEN"
	CodeAuthAccountBanned      Code = "AUTH_ACCOUNT_BANNED"
	CodeAuthEmailNotVerified   Code = "AUTH_EMAIL_NOT_VERIFIED"
	CodeAuthOTPInvalid         Code = "AUTH_OTP_INVALID"
	CodeAuthOTPExpired         Code = "AUTH_OTP_EXPIRED"
	CodeAuthOTPTooManyAttempts Code = "AUTH_OTP_TOO_MANY_ATTEMPTS"
	CodeAuthOTPRateLimited     Code = "AUTH_OTP_RATE_LIMITED"
	CodeAuthTokenInvalid       Code = "AUTH_TOKEN_INVALID"
	CodeAuthTokenExpired       Code = "AUTH_TOKEN_EXPIRED"
	CodeAuthTokenRevoked       Code = "AUTH_TOKEN_REVOKED"
	CodeAuthPasswordMismatch   Code = "AUTH_PASSWORD_MISMATCH"
	CodeAuthPasswordTooWeak    Code = "AUTH_PASSWORD_TOO_WEAK"
)

// statuses 是错误码到 HTTP 状态码的唯一映射表，新增错误码必须在此登记。
var statuses = map[Code]int{
	CodeBadRequest:       http.StatusBadRequest,
	CodeValidationFailed: http.StatusUnprocessableEntity,
	CodeNotFound:         http.StatusNotFound,
	CodeMethodNotAllowed: http.StatusMethodNotAllowed,
	CodeForbidden:        http.StatusForbidden,
	CodeConflict:         http.StatusConflict,
	CodeRateLimited:      http.StatusTooManyRequests,
	CodeRequestTooLarge:  http.StatusRequestEntityTooLarge,
	CodeInternal:         http.StatusInternalServerError,

	CodeAuthInvalidCredentials: http.StatusUnauthorized,
	CodeAuthEmailAlreadyExists: http.StatusConflict,
	CodeAuthUsernameTaken:      http.StatusConflict,
	CodeAuthPhoneNumberTaken:   http.StatusConflict,
	CodeAuthAccountBanned:      http.StatusForbidden,
	CodeAuthEmailNotVerified:   http.StatusForbidden,
	CodeAuthOTPInvalid:         http.StatusUnprocessableEntity,
	CodeAuthOTPExpired:         http.StatusUnprocessableEntity,
	CodeAuthOTPTooManyAttempts: http.StatusTooManyRequests,
	CodeAuthOTPRateLimited:     http.StatusTooManyRequests,
	CodeAuthTokenInvalid:       http.StatusUnauthorized,
	CodeAuthTokenExpired:       http.StatusUnauthorized,
	CodeAuthTokenRevoked:       http.StatusUnauthorized,
	CodeAuthPasswordMismatch:   http.StatusUnprocessableEntity,
	CodeAuthPasswordTooWeak:    http.StatusUnprocessableEntity,
}

// Status 返回错误码对应的 HTTP 状态码；未登记的错误码一律按 500 处理。
func Status(c Code) int {
	if s, ok := statuses[c]; ok {
		return s
	}
	return http.StatusInternalServerError
}

// domain 用于给 oops 标注错误来源，便于日志按子系统筛选。
func domain(c Code) string {
	if strings.HasPrefix(string(c), "AUTH_") {
		return "auth"
	}
	return "app"
}
