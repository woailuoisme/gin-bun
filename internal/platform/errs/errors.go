package errs

import (
	"errors"

	"github.com/samber/oops"
)

// 面向调用方的固定文案。api.md 5.2 已明确规定前两条，不得随意改写。
const (
	msgValidationFailed = "Validation failed"
	msgInternal         = "An unexpected error occurred"
	msgBadRequest       = "The request could not be understood"
	msgNotFound         = "The requested resource was not found"
	msgMethodNotAllowed = "The requested HTTP method is not allowed"
	msgForbidden        = "You are not allowed to perform this action"
	msgConflict         = "The request conflicts with the current state of the resource"
	msgRateLimited      = "Too many requests"
	msgRequestTooLarge  = "The request body is too large"
	msgInvalidLogin     = "Invalid email or password"
)

// Error 是携带机器可读错误码与可选字段级明细的业务错误。
type Error struct {
	code    Code
	message string
	fields  map[string][]string
	cause   error
}

// Error 实现 error 接口，返回面向调用方的文案。
func (e *Error) Error() string { return e.message }

// Unwrap 暴露底层错误，使 errors.Is/As 可以继续向下匹配。
func (e *Error) Unwrap() error { return e.cause }

// Code 返回错误码。
func (e *Error) Code() Code { return e.code }

// Message 返回面向调用方的英文描述。
func (e *Error) Message() string { return e.message }

// Fields 返回按字段路径归集的校验明细，无明细时为 nil。
func (e *Error) Fields() map[string][]string { return e.fields }

// WithField 追加一条字段级明细并返回自身，供构造时链式调用。
func (e *Error) WithField(field, message string) *Error {
	if e.fields == nil {
		e.fields = make(map[string][]string)
	}
	e.fields[field] = append(e.fields[field], message)
	return e
}

// New 按错误码构造业务错误，并用 oops 捕获堆栈与来源域。
func New(code Code, message string) *Error {
	return &Error{
		code:    code,
		message: message,
		cause:   oops.Code(string(code)).In(domain(code)).Public(message).New(message),
	}
}

// Wrap 在既有错误之上附加错误码语义，同时保留原始错误。
func Wrap(code Code, message string, cause error) *Error {
	if cause == nil {
		return New(code, message)
	}
	return &Error{
		code:    code,
		message: message,
		cause:   oops.Code(string(code)).In(domain(code)).Public(message).Wrapf(cause, "%s", message),
	}
}

// BadRequest 构造 400，用于请求体无法解析等结构性错误。
func BadRequest(message string) *Error {
	if message == "" {
		message = msgBadRequest
	}
	return New(CodeBadRequest, message)
}

// NotFound 构造 404。
func NotFound(message string) *Error {
	if message == "" {
		message = msgNotFound
	}
	return New(CodeNotFound, message)
}

// MethodNotAllowed 构造 405。
func MethodNotAllowed(message string) *Error {
	if message == "" {
		message = msgMethodNotAllowed
	}
	return New(CodeMethodNotAllowed, message)
}

// Forbidden 构造 403。
func Forbidden(message string) *Error {
	if message == "" {
		message = msgForbidden
	}
	return New(CodeForbidden, message)
}

// Conflict 构造 409。
func Conflict(message string) *Error {
	if message == "" {
		message = msgConflict
	}
	return New(CodeConflict, message)
}

// RateLimited 构造 429。
func RateLimited(message string) *Error {
	if message == "" {
		message = msgRateLimited
	}
	return New(CodeRateLimited, message)
}

// RequestTooLarge 构造 413，用于请求体超过中间件设定的上限。
func RequestTooLarge(cause error) *Error {
	return Wrap(CodeRequestTooLarge, msgRequestTooLarge, cause)
}

// InvalidCredentials 是登录失败的唯一对外口径，不区分"邮箱不存在"与"密码错误"。
func InvalidCredentials() *Error { return New(CodeAuthInvalidCredentials, msgInvalidLogin) }

// Unauthorized 构造 401，用于凭证缺失、格式错误、过期或已撤销。
func Unauthorized(code Code, message string) *Error { return New(code, message) }

// Internal 把未预期错误收敛为 500；原始错误只进日志，绝不进响应体。
func Internal(cause error) *Error { return Wrap(CodeInternal, msgInternal, cause) }

// Validation 构造 422，fields 为按字段路径归集的校验失败文案。
func Validation(fields map[string][]string) *Error {
	e := New(CodeValidationFailed, msgValidationFailed)
	e.fields = fields
	return e
}

// As 从错误链中提取 *Error，是 errors.AsType 的语义化封装。
func As(err error) (*Error, bool) {
	return errors.AsType[*Error](err)
}

// Stack 提取错误链中的 oops 堆栈文本，非 oops 错误返回空串。
func Stack(err error) string {
	if oopsErr, ok := oops.AsOops(err); ok {
		return oopsErr.Stacktrace()
	}
	return ""
}
