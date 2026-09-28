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

// Response 是 api.md 5.2 定义的错误响应体。所有错误共用这一种结构。
type Response struct {
	Code    string              `json:"code"`
	Message string              `json:"message"`
	Errors  map[string][]string `json:"errors,omitempty"`
}

// Render 把任意 error 映射为统一错误响应并写回，同时按严重程度记日志。
func Render(c *gin.Context, err error) {
	if err == nil {
		return
	}

	status, resp := Map(err)
	c.Set("error_code", resp.Code)
	log := logger.FromContext(c.Request.Context())
	if status >= http.StatusInternalServerError {
		// 记录的是因由而不是包装体：*Error 的 Error() 返回的是那句对外文案
		// （"An unexpected error occurred"），直接 .Err(err) 会把真正的根因
		// ——oops 链里的驱动错误——丢掉，而 Stack() 只有调用帧、不含错误文本。
		// 对外响应走的仍是 resp.Message，所以这里换错误对象不会泄漏内部细节。
		log.Error().
			Str("stack", Stack(err)).
			Err(RootCause(err)).
			Int("status", status).Str("code", resp.Code).Msg("request processing failed")
	} else {
		log.Warn().Err(err).
			Int("status", status).Str("code", resp.Code).Msg("request rejected")
	}

	c.AbortWithStatusJSON(status, resp)
}

// RootCause 取出适合写进日志的错误。
//
// 对 *Error 而言，它包裹的 oops 错误的 Error() 是"对外文案: 根因"，正是排查 500 所需；
// 其余错误原样返回，保留外层的上下文（如"连接数据库失败: ..."）。
func RootCause(err error) error {
	bizErr, ok := As(err)
	if !ok {
		return err
	}
	if inner := errors.Unwrap(bizErr); inner != nil {
		return inner
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

	// 平台层直接抛出的、已在 statuses 登记过的 oops 错误。
	if oopsErr, ok := oops.AsOops(err); ok {
		code := Code(fmt.Sprintf("%v", oopsErr.Code()))
		if _, registered := statuses[code]; registered {
			return Status(code), Response{
				Code:    string(code),
				Message: cmp.Or(oopsErr.Public(), msgInternal),
			}
		}
	}

	// 兜底：未识别的错误一律 500，不向外泄漏任何内部细节。
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

// mapPgError 把数据库约束错误翻译成业务语义，避免把 PgError 细节暴露出去。
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
