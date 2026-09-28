package core

import (
	"regexp"
	"strings"
	"unicode"
)

var matchFirstCap = regexp.MustCompile("(.)([A-Z][a-z]+)")
var matchAllCap = regexp.MustCompile("([a-z0-9])([A-Z])")

// ToSnakeCase converts a string (CamelCase, PascalCase, kebab-case, or spaces) to snake_case.
func ToSnakeCase(str string) string {
	s := strings.TrimSpace(str)
	s = strings.ReplaceAll(s, "-", "_")
	s = strings.ReplaceAll(s, " ", "_")
	snake := matchFirstCap.ReplaceAllString(s, "${1}_${2}")
	snake = matchAllCap.ReplaceAllString(snake, "${1}_${2}")
	return strings.ToLower(snake)
}

// ToKebabCase converts a string to kebab-case.
func ToKebabCase(str string) string {
	return strings.ReplaceAll(ToSnakeCase(str), "_", "-")
}

// ToPascalCase converts a string (snake_case, kebab-case, space separated) to PascalCase.
func ToPascalCase(str string) string {
	words := splitWords(str)
	var sb strings.Builder
	for _, word := range words {
		if len(word) == 0 {
			continue
		}
		sb.WriteString(strings.ToUpper(word[:1]))
		sb.WriteString(strings.ToLower(word[1:]))
	}
	return sb.String()
}

// ToCamelCase converts a string to camelCase.
func ToCamelCase(str string) string {
	pascal := ToPascalCase(str)
	if len(pascal) == 0 {
		return ""
	}
	return strings.ToLower(pascal[:1]) + pascal[1:]
}

// splitWords splits an arbitrary string into lowercase words based on separators or camelCase boundaries.
func splitWords(s string) []string {
	snake := ToSnakeCase(s)
	raw := strings.Split(snake, "_")
	var result []string
	for _, part := range raw {
		p := strings.TrimSpace(part)
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}

// Pluralize produces a pragmatic English plural form for standard model/resource nouns.
func Pluralize(noun string) string {
	s := strings.TrimSpace(noun)
	if len(s) == 0 {
		return ""
	}
	lower := strings.ToLower(s)
	lastChar := lower[len(lower)-1]

	// Irregular / special cases
	switch lower {
	case "person":
		return matchCase(s, "people")
	case "child":
		return matchCase(s, "children")
	case "man":
		return matchCase(s, "men")
	case "woman":
		return matchCase(s, "women")
	}

	// -y preceded by a consonant: category -> categories
	if lastChar == 'y' && len(lower) > 1 && !isVowel(lower[len(lower)-2]) {
		return s[:len(s)-1] + "ies"
	}

	// -s, -sh, -ch, -x, -z: bus -> buses, box -> boxes
	if strings.HasSuffix(lower, "s") || strings.HasSuffix(lower, "sh") ||
		strings.HasSuffix(lower, "ch") || strings.HasSuffix(lower, "x") ||
		strings.HasSuffix(lower, "z") {
		return s + "es"
	}

	// Default append 's'
	return s + "s"
}

// Singularize produces a pragmatic English singular form for standard model/resource nouns.
func Singularize(noun string) string {
	s := strings.TrimSpace(noun)
	if len(s) == 0 {
		return ""
	}
	lower := strings.ToLower(s)

	switch lower {
	case "people":
		return matchCase(s, "person")
	case "children":
		return matchCase(s, "child")
	case "men":
		return matchCase(s, "man")
	case "women":
		return matchCase(s, "woman")
	}

	if strings.HasSuffix(lower, "ies") && len(lower) > 3 {
		return s[:len(s)-3] + "y"
	}
	if strings.HasSuffix(lower, "ses") || strings.HasSuffix(lower, "shes") ||
		strings.HasSuffix(lower, "ches") || strings.HasSuffix(lower, "xes") ||
		strings.HasSuffix(lower, "zes") {
		return s[:len(s)-2]
	}
	if strings.HasSuffix(lower, "s") && !strings.HasSuffix(lower, "ss") && len(lower) > 1 {
		return s[:len(s)-1]
	}

	return s
}

func isVowel(c byte) bool {
	return c == 'a' || c == 'e' || c == 'i' || c == 'o' || c == 'u'
}

func matchCase(source, target string) string {
	if len(source) == 0 || len(target) == 0 {
		return target
	}
	if unicode.IsUpper(rune(source[0])) {
		return strings.ToUpper(target[:1]) + target[1:]
	}
	return strings.ToLower(target)
}
