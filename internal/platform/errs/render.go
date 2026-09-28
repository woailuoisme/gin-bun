package errs

import (
	"cmp"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/go-playground/validator/v10"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/samber/oops"

	"bgin/internal/platform/logger"
)

// PostgreSQL 错误码，用于把约束冲突翻译成对调用方有意义的响应。
const (
	pgErrUniqueViolation           = "23505"
	pgErrForeignKeyViolation       = "23503"
	pgErrNotNullViolation          = "23502"
	pgErrInvalidTextRepresentation = "22P02"
)

// Response 是统一错误响应体。
type Response struct {
	Code    string              `json:"code"`
	Message string              `json:"message"`
	Errors  map[string][]string `json:"errors,omitempty"`
}

// Render 映射统一错误响应并写回客户端。
//
// 5xx 记录错误根因与调用堆栈；4xx 由外层 AccessLog 统一输出访问日志，避免重复告警。
func Render(c *gin.Context, err error) {
	if err == nil {
		return
	}

	status, resp := Map(err)
	c.Set("error_code", resp.Code)
	if len(c.Errors) == 0 {
		_ = c.Error(err)
	}

	if status >= http.StatusInternalServerError {
		// 5xx 记录底层根因与调用堆栈，避免对外统一文案遮蔽排错细节。
		log := logger.FromContext(c.Request.Context())
		log.Error().
			Str("stack", Stack(err)).
			Err(RootCause(err)).
			Int("status", status).Str("code", resp.Code).Msg("request processing failed")
	}

	c.AbortWithStatusJSON(status, resp)
}

// RootCause 提取用于日志记录的底层真实错误。
func RootCause(err error) error {
	if bizErr, ok := As(err); ok {
		if inner := errors.Unwrap(bizErr); inner != nil {
			return inner
		}
	}
	return err
}

// Map 把任意 error 归一化为 HTTP 状态码与响应体。映射顺序即优先级。
func Map(err error) (int, Response) {
	if bizErr, ok := As(err); ok {
		return Status(bizErr.code), Response{
			Code:    string(bizErr.code),
			Message: bizErr.message,
			Errors:  bizErr.fields,
		}
	}

	// 请求体结构错误（JSON 语法、类型不匹配、空体）统一按 400 处理。
	if status, resp, ok := mapMalformedBody(err); ok {
		return status, resp
	}

	if valErr, ok := errors.AsType[validator.ValidationErrors](err); ok {
		return http.StatusUnprocessableEntity, Response{
			Code:    string(CodeValidationFailed),
			Message: msgValidationFailed,
			Errors:  rawValidationFields(valErr),
		}
	}

	if pgErr, ok := errors.AsType[*pgconn.PgError](err); ok {
		return mapPgError(pgErr)
	}

	if errors.Is(err, sql.ErrNoRows) || errors.Is(err, pgx.ErrNoRows) {
		return http.StatusNotFound, Response{Code: string(CodeNotFound), Message: msgNotFound}
	}

	if errors.Is(err, context.DeadlineExceeded) {
		return http.StatusGatewayTimeout, Response{Code: string(CodeGatewayTimeout), Message: msgGatewayTimeout}
	}

	// 识别已在 statuses 登记过的 oops 错误。
	if oopsErr, ok := oops.AsOops(err); ok {
		code := Code(fmt.Sprintf("%v", oopsErr.Code()))
		if _, registered := statuses[code]; registered {
			return Status(code), Response{
				Code:    string(code),
				Message: cmp.Or(oopsErr.Public(), msgInternal),
			}
		}
	}

	// 兜底：未识别错误统一返回 500。
	return http.StatusInternalServerError, Response{Code: string(CodeInternal), Message: msgInternal}
}

// mapMalformedBody 识别请求体解析、JSON 解码、查询参数转换与载荷超限的失败。
func mapMalformedBody(err error) (int, Response, bool) {
	if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
		return http.StatusRequestEntityTooLarge, Response{
			Code:    string(CodeRequestTooLarge),
			Message: msgRequestTooLarge,
		}, true
	}
	if isBadRequestError(err) {
		return http.StatusBadRequest, Response{
			Code:    string(CodeBadRequest),
			Message: msgBadRequest,
		}, true
	}

	return 0, Response{}, false
}

func isBadRequestError(err error) bool {
	_, isNum := errors.AsType[*strconv.NumError](err)
	_, isSyntax := errors.AsType[*json.SyntaxError](err)
	_, isType := errors.AsType[*json.UnmarshalTypeError](err)
	return isNum || isSyntax || isType || errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF)
}

// mapPgError 将数据库约束错误映射为业务状态码与文案。
func mapPgError(err *pgconn.PgError) (int, Response) {
	switch err.Code {
	case pgErrUniqueViolation, pgErrForeignKeyViolation, pgErrNotNullViolation:
		return http.StatusConflict, Response{Code: string(CodeConflict), Message: msgConflict}
	case pgErrInvalidTextRepresentation:
		return http.StatusBadRequest, Response{Code: string(CodeBadRequest), Message: msgBadRequest}
	default:
		return http.StatusInternalServerError, Response{Code: string(CodeInternal), Message: msgInternal}
	}
}
