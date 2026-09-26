package middleware

import (
	"errors"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"
	govalidator "github.com/go-playground/validator/v10"

	"bgin/internal/platform/errs"
	"bgin/internal/platform/validator"
)

// ErrorHandler 拦截请求链路中的全部错误，集中解析业务码、执行多语言翻译并写回统一 JSON 响应。
func (m *Middleware) ErrorHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()

		if len(c.Errors) == 0 || c.Writer.Written() {
			return
		}

		err := c.Errors.Last().Err
		if fields, ok := m.extractValidationFields(c, err); ok {
			err = errs.Validation(fields)
		}

		errs.Render(c, err)
	}
}

// extractValidationFields 提取并翻译入参校验错误字典。
func (m *Middleware) extractValidationFields(c *gin.Context, err error) (map[string][]string, bool) {
	lang := c.GetHeader("Accept-Language")
	if m.translator != nil {
		lang = m.translator.Language(lang)
	}

	if valErr, ok := errors.AsType[govalidator.ValidationErrors](err); ok {
		return validator.Translate(m.translator, lang, valErr), true
	}

	sliceErr, ok := errors.AsType[binding.SliceValidationError](err)
	if !ok {
		return nil, false
	}

	fields := make(map[string][]string)
	for _, subErr := range sliceErr {
		subValErr, subOk := errors.AsType[govalidator.ValidationErrors](subErr)
		if !subOk {
			continue
		}
		for k, v := range validator.Translate(m.translator, lang, subValErr) {
			fields[k] = append(fields[k], v...)
		}
	}
	return fields, len(fields) > 0
}
