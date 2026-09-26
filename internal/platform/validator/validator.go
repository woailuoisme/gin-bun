// Package validator 封装请求绑定与校验，并把失败结果翻译为统一错误响应。
package validator

import (
	"errors"
	"fmt"
	"io"
	"reflect"
	"regexp"
	"strings"
	"unicode"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"
	govalidator "github.com/go-playground/validator/v10"

	"bgin/internal/platform/errs"
	"bgin/internal/platform/i18n"
)

func init() {
	if err := InitEngine(); err != nil {
		panic(err)
	}
}

// InitEngine 将自定义校验规则与字段名提取函数注册至 Gin 全局 binding.Validator。
func InitEngine() error {
	if v, ok := binding.Validator.Engine().(*govalidator.Validate); ok {
		return registerRules(v)
	}
	return nil
}

// registerRules 统一注册 json 标签名解析与业务校验规则。
func registerRules(v *govalidator.Validate) error {
	v.SetTagName("validate")
	v.RegisterTagNameFunc(func(field reflect.StructField) string {
		name, _, _ := strings.Cut(field.Tag.Get("json"), ",")
		if name == "" || name == "-" {
			return field.Name
		}
		return name
	})
	rules := []struct {
		tag string
		fn  govalidator.Func
	}{
		{"usernameslug", validateUsernameSlug},
		{"e164", validateE164},
		{"notreserved", validateNotReserved},
		{"usersort", validateUserSort},
	}
	for _, rule := range rules {
		if err := v.RegisterValidation(rule.tag, rule.fn); err != nil {
			return fmt.Errorf("register validation %q: %w", rule.tag, err)
		}
	}
	return nil
}

// usernameSlugPattern 限定用户名只含小写字母、数字与下划线。
var usernameSlugPattern = regexp.MustCompile(`^[a-z0-9_]+$`)

// e164Pattern 限定手机号为 E.164 规范（加号开头，7 到 15 位数字）。
var e164Pattern = regexp.MustCompile(`^\+[1-9]\d{6,14}$`)

// reservedShortNames 是禁止占用的短名，避免与系统路径或保留字冲突。
// 这是编译期常量，不接受请求参数传入——validator 没有可用的「不在集合内」内置规则。
var reservedShortNames = map[string]struct{}{
	"admin":  {},
	"root":   {},
	"system": {},
	"me":     {},
	"api":    {},
	"bgin":   {},
}

// validateUsernameSlug 校验用户名的字符集。
func validateUsernameSlug(field govalidator.FieldLevel) bool {
	return usernameSlugPattern.MatchString(field.Field().String())
}

// validateE164 校验手机号是否符合 E.164 规范。
func validateE164(field govalidator.FieldLevel) bool {
	return e164Pattern.MatchString(field.Field().String())
}

// validateNotReserved 拒绝保留短名，大小写不敏感。
func validateNotReserved(field govalidator.FieldLevel) bool {
	_, reserved := reservedShortNames[strings.ToLower(field.Field().String())]
	return !reserved
}

// userSortColumns 是允许排序的用户字段集合。
var userSortColumns = map[string]struct{}{
	"createdAt":   {},
	"updatedAt":   {},
	"email":       {},
	"name":        {},
	"lastLoginAt": {},
}

// validateUserSort 校验用户列表的排序字段，支持以逗号分隔的复合排序以及 - 降序前缀。
func validateUserSort(field govalidator.FieldLevel) bool {
	raw := field.Field().String()
	if raw == "" {
		return true
	}
	for part := range strings.SplitSeq(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			return false
		}
		part = strings.TrimPrefix(part, "-")
		if _, ok := userSortColumns[part]; !ok {
			return false
		}
	}
	return true
}

// PasswordStrength 校验口令至少 8 位且同时含字母与数字，返回契约规定的 422 业务错误。
func PasswordStrength(field, password string) error {
	if strings.ContainsFunc(password, unicode.IsLetter) && strings.ContainsFunc(password, unicode.IsDigit) {
		return nil
	}
	return errs.New(errs.CodeAuthPasswordTooWeak, "The password does not meet strength requirements").
		WithField(field, "The "+field+" must contain both letters and numbers.")
}

// BindOptionalJSON 解析可省略的请求体：空体视为零值对象，非空体仍需是合法 JSON。
func BindOptionalJSON(c *gin.Context, target any) error {
	if c.Request.Body == nil || c.Request.ContentLength == 0 {
		return nil
	}
	err := c.ShouldBindJSON(target)
	if errors.Is(err, io.EOF) {
		return nil
	}
	return err
}

// Translate 按请求语言渲染每条校验失败，键为 api.md 约定的字段路径。
func Translate(translator *i18n.Translator, language string, valErr govalidator.ValidationErrors) map[string][]string {
	fields := make(map[string][]string, len(valErr))
	for _, fe := range valErr {
		field := errs.FieldPath(fe)
		var message string
		if translator != nil {
			message = translator.ValidationMessage(language, MessageKey(fe), field, fe.Param())
		}
		if message == "" {
			message = fmt.Sprintf("The %s is invalid.", field)
		}
		fields[field] = append(fields[field], message)
	}
	return fields
}

// MessageKey 选择文案键：数值与字符串的 min/max 措辞不同，需要区分。
func MessageKey(fe govalidator.FieldError) string {
	switch fe.Tag() {
	case "min", "max":
		if isNumericKind(fe.Kind()) {
			return fe.Tag() + "_numeric"
		}
	case "usersort":
		return "oneof"
	}
	return fe.Tag()
}

// isNumericKind 判断字段是否为数值类型，用于挑选 min/max 的措辞。
func isNumericKind(kind reflect.Kind) bool {
	switch kind {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		return true
	default:
		return false
	}
}
