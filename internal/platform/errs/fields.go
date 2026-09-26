package errs

import (
	"strings"

	"github.com/go-playground/validator/v10"
)

// FieldPath 把 validator 的字段错误规范化为 api.md 5.2 约定的字段路径。
// 例如 Namespace 为 payload.tags[0].name 时会得到 tags.0.name。
//
// 这里用 Namespace() 而不是 Field()：Field() 只返回叶子字段名，嵌套路径会整段丢失
// （tags[0].name 会变成 name），而 api.md 要求按字段路径归集错误。
// validator 的 Namespace 首段恒定是根结构体类型名，直接剥掉即可。
func FieldPath(fe validator.FieldError) string {
	namespace := fe.Namespace()
	if _, rest, found := strings.Cut(namespace, "."); found {
		namespace = rest
	}
	return NormalizePath(namespace)
}

// NormalizePath 把 a[0].b 形态的字段路径改写为 a.0.b，与 api.md 5.2 的示例一致。
func NormalizePath(namespace string) string {
	var b strings.Builder
	b.Grow(len(namespace))
	for i := range len(namespace) {
		switch namespace[i] {
		case '[':
			b.WriteByte('.')
		case ']':
			// 右括号丢弃：索引数字已在上一次迭代写入
		default:
			b.WriteByte(namespace[i])
		}
	}
	return b.String()
}

// rawValidationFields 是未经翻译的兜底实现，仅在错误绕过了中间件翻译时触发。
// 正常路径下校验失败已由 middleware.ErrorHandler 翻译后构造 *Error。
func rawValidationFields(valErr validator.ValidationErrors) map[string][]string {
	fields := make(map[string][]string, len(valErr))
	for _, fe := range valErr {
		key := FieldPath(fe)
		fields[key] = append(fields[key], fe.Tag())
	}
	return fields
}
