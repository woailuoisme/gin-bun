package excel

import (
	"cmp"
	"fmt"
	"math"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/golang-module/carbon/v2"
	"github.com/samber/lo"
	"github.com/spf13/cast"
	"github.com/xuri/excelize/v2"
)

// zhTrueToken 中文「是」（U+662F）的转义表示，用于兼容布尔真值解析。
const zhTrueToken = "\u662f"

type structFieldInfo struct {
	index int
	tag   string
	name  string
	kind  reflect.Kind
}

var structCache sync.Map

func getStructFields(typ reflect.Type) []structFieldInfo {
	if typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}

	if typ.Kind() != reflect.Struct {
		return nil
	}

	if val, ok := structCache.Load(typ); ok {
		if cached, valid := val.([]structFieldInfo); valid {
			return cached
		}
	}

	fields := make([]structFieldInfo, 0, typ.NumField())
	for i := range typ.NumField() {
		field := typ.Field(i)
		if field.PkgPath != "" || field.Anonymous {
			continue
		}

		tag := cmp.Or(field.Tag.Get("excel"), field.Tag.Get("json"))
		if tag == "" || tag == "-" {
			tag = field.Name
		}

		fields = append(fields, structFieldInfo{
			index: i,
			tag:   tag,
			name:  field.Name,
			kind:  field.Type.Kind(),
		})
	}

	structCache.Store(typ, fields)

	return fields
}

// adjustColumnWidths 调整工作表中各列的显示宽度。
func adjustColumnWidths(export any, f *excelize.File, sheetName string) error {
	if w, ok := export.(WithColumnWidths); ok {
		for col, width := range w.ColumnWidths() {
			_ = f.SetColWidth(sheetName, col, col, width)
		}

		return nil
	}

	if autosize, ok := export.(ShouldAutoSize); ok && autosize.ShouldAutoSize() {
		cols, err := f.GetCols(sheetName)
		if err != nil {
			return err
		}

		for idx, col := range cols {
			maxLen := 0
			for _, val := range col {
				maxLen = max(maxLen, len([]rune(val)))
			}

			colName, err := excelize.ColumnNumberToName(idx + 1)
			if err == nil {
				width := math.Max(float64(maxLen)+3, 10)
				_ = f.SetColWidth(sheetName, colName, colName, width)
			}
		}
	}

	return nil
}

// getRowValuesFromStruct 基于缓存的结构体元数据提取字段值切片。
func getRowValuesFromStruct(item any) []any {
	if item == nil {
		return nil
	}

	val := reflect.ValueOf(item)
	if val.Kind() == reflect.Pointer {
		val = val.Elem()
	}

	fields := getStructFields(val.Type())
	if len(fields) == 0 {
		return []any{item}
	}

	vals := make([]any, len(fields))
	for i, f := range fields {
		vals[i] = formatValue(val.Field(f.index).Interface())
	}

	return vals
}

// formatValue 将任意 Go 数据类型规格化为适合写入 Excel 单元格的值。
func formatValue(val any) any {
	if val == nil {
		return ""
	}

	switch v := val.(type) {
	case time.Time:
		if v.IsZero() {
			return ""
		}

		return carbon.CreateFromStdTime(v).ToDateTimeString()
	case *time.Time:
		if v == nil || v.IsZero() {
			return ""
		}

		return carbon.CreateFromStdTime(*v).ToDateTimeString()
	case bool:
		if v {
			return "Yes"
		}

		return "No"
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		return v
	case float32, float64:
		return v
	default:
		return fmt.Sprintf("%v", v)
	}
}

// mapRowToStruct 将单行文本单元格反序列化填充到对应的目标结构体字段中。
func mapRowToStruct(cols, headers []string, dest reflect.Value) error {
	typ := dest.Type()
	fields := getStructFields(typ)

	headerMap := make(map[string]int, len(headers))
	for i, h := range headers {
		headerMap[strings.ToLower(strings.TrimSpace(h))] = i
	}

	for _, f := range fields {
		colIdx := resolveColumnIndex(f, headers, headerMap, len(cols))
		if colIdx < 0 || colIdx >= len(cols) {
			continue
		}

		cellVal := strings.TrimSpace(cols[colIdx])
		if cellVal == "" {
			continue
		}

		structField := dest.Field(f.index)
		if err := setFieldValue(structField, cellVal); err != nil {
			return fmt.Errorf("failed to map field %s (val: %s): %w", f.name, cellVal, err)
		}
	}

	return nil
}

func resolveColumnIndex(
	f structFieldInfo,
	headers []string,
	headerMap map[string]int,
	columnCount int,
) int {
	if len(headers) > 0 && f.tag != "-" {
		if idx, ok := headerMap[strings.ToLower(f.tag)]; ok {
			return idx
		}
		if idx, ok := headerMap[strings.ToLower(f.name)]; ok {
			return idx
		}
	}

	if len(headers) == 0 && f.index < columnCount {
		return f.index
	}

	return -1
}

// setFieldValue 安全转换并填充字符串单元格内容至目标反射字段。
func setFieldValue(field reflect.Value, val string) error {
	if !field.CanSet() {
		return nil
	}

	switch field.Kind() {
	case reflect.String:
		field.SetString(val)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		intVal, err := cast.ToInt64E(val)
		if err != nil {
			return err
		}

		field.SetInt(intVal)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		uintVal, err := cast.ToUint64E(val)
		if err != nil {
			return err
		}

		field.SetUint(uintVal)
	case reflect.Float32, reflect.Float64:
		floatVal, err := cast.ToFloat64E(val)
		if err != nil {
			return err
		}

		field.SetFloat(floatVal)
	case reflect.Bool:
		boolVal, err := cast.ToBoolE(val)
		if err != nil {
			lowerVal := strings.ToLower(val)
			boolVal = lowerVal == "yes" || val == zhTrueToken
		}

		field.SetBool(boolVal)
	case reflect.Struct:
		if field.Type() == reflect.TypeFor[time.Time]() {
			parsedTime, err := parseCellTime(val)
			if err != nil {
				return err
			}

			field.Set(reflect.ValueOf(parsedTime))
		}
	case reflect.Pointer:
		elem := reflect.New(field.Type().Elem())
		if err := setFieldValue(elem.Elem(), val); err != nil {
			return err
		}

		field.Set(elem)
	default:
		return nil
	}

	return nil
}

func parseCellTime(val string) (time.Time, error) {
	c := carbon.Parse(val)
	if c.Error == nil && c.IsValid() {
		return c.StdTime(), nil
	}

	if t, err := cast.ToTimeE(val); err == nil {
		return t, nil
	}

	return time.Time{}, fmt.Errorf("invalid time value %s", val)
}

func isRowEmpty(cols []string) bool {
	return lo.EveryBy(cols, func(col string) bool {
		return strings.TrimSpace(col) == ""
	})
}
