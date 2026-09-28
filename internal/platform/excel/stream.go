package excel

import (
	"cmp"
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"reflect"

	"github.com/samber/oops"
	"github.com/xuri/excelize/v2"
)

// StreamImportOption 定义流式导入配置选项别名，与 ImportOption 保持统一。
type StreamImportOption = ImportOption

// WithStreamSheetName 指定读取的工作表名称。
func WithStreamSheetName(name string) StreamImportOption {
	return WithImportSheetName(name)
}

// WithHeaderRowIndex 配置跳过的表头行数（从 1 开始计数）。
func WithHeaderRowIndex(idx int) StreamImportOption {
	return WithHeadingRowIndex(idx)
}

// sliceGenerator 构造基于泛型切片的流式迭代器生成器。
func sliceGenerator[T any](items []T) func(yield func(item T) bool) error {
	return func(yield func(item T) bool) error {
		for _, item := range items {
			if !yield(item) {
				break
			}
		}
		return nil
	}
}

// ExportSlice 使用流式写入器将内存中的泛型切片导出为 Excel 格式。
func ExportSlice[T any](
	ctx context.Context,
	w io.Writer,
	items []T,
	headings []string,
	mapper func(item T) []any,
	opts ...ExportOption,
) error {
	return StreamExport(ctx, w, headings, sliceGenerator(items), mapper, opts...)
}

// ExportSliceCSV 以 O(1) 内存将内存中的泛型切片导出为 CSV 格式。
func ExportSliceCSV[T any](
	ctx context.Context,
	w io.Writer,
	items []T,
	headings []string,
	mapper func(item T) []any,
) error {
	return StreamExportCSV(ctx, w, headings, sliceGenerator(items), mapper)
}

func initStreamWriter(file *excelize.File, sheetName string, headings []string) (*excelize.StreamWriter, int, error) {
	sheetName = cmp.Or(sheetName, defaultSheetName)
	ensureSheet(file, sheetName)

	sw, err := file.NewStreamWriter(sheetName)
	if err != nil {
		return nil, 0, oops.Code("excel_stream_error").Wrap(err)
	}

	rowIdx := 1
	if len(headings) > 0 {
		cells := make([]any, len(headings))
		for i, h := range headings {
			cells[i] = excelize.Cell{Value: h}
		}
		cellName, cellErr := excelize.CoordinatesToCellName(1, rowIdx)
		if cellErr != nil {
			return nil, 0, oops.Code("excel_stream_error").Wrap(cellErr)
		}
		if err := sw.SetRow(cellName, cells); err != nil {
			return nil, 0, oops.Code("excel_stream_error").Wrap(err)
		}
		rowIdx++
	}

	return sw, rowIdx, nil
}

func writeStreamRow[T any](sw *excelize.StreamWriter, item T, rowIdx int, mapper func(item T) []any) error {
	var rowValues []any
	if mapper != nil {
		rowValues = mapper(item)
	} else {
		rowValues = []any{item}
	}

	cells := make([]any, len(rowValues))
	for i, v := range rowValues {
		cells[i] = excelize.Cell{Value: formatValue(v)}
	}

	cellName, err := excelize.CoordinatesToCellName(1, rowIdx)
	if err != nil {
		return err
	}

	return sw.SetRow(cellName, cells)
}

// StreamExport 使用生成器迭代函数将元素流式导出到 Excel 写入器，内存占用保持 O(1)。
func StreamExport[T any](
	ctx context.Context,
	w io.Writer,
	headings []string,
	generator func(yield func(item T) bool) error,
	mapper func(item T) []any,
	opts ...ExportOption,
) error {
	if w == nil {
		return oops.Code("nil_writer").Errorf("writer cannot be nil")
	}
	if generator == nil {
		return oops.Code("nil_generator").Errorf("generator cannot be nil")
	}

	cfg := newDefaultExportConfig()
	for _, opt := range opts {
		opt(cfg)
	}

	file := excelize.NewFile()
	defer func() { _ = file.Close() }()

	sw, rowIdx, err := initStreamWriter(file, cfg.sheetName, headings)
	if err != nil {
		return err
	}

	// yieldErr 记录被 yield 协议吞掉的失败原因：生成器只能以返回 false 中断迭代，
	// 无法回传错误；若不在此捕获，写失败与上下文取消都会产出「残缺却成功」的导出文件。
	var yieldErr error

	genErr := generator(func(item T) bool {
		select {
		case <-ctx.Done():
			yieldErr = ctx.Err()
			return false
		default:
		}

		if err := writeStreamRow(sw, item, rowIdx, mapper); err != nil {
			yieldErr = err
			return false
		}
		rowIdx++
		return true
	})
	if genErr != nil {
		return oops.Code("excel_generator_error").Wrap(genErr)
	}
	if yieldErr != nil {
		// 上下文取消不是写失败：原样上抛，调用方可用 errors.Is(err, context.Canceled)
		// 把「客户端断开」与「真实写错误」分开，只有后者需要告警。
		if errors.Is(yieldErr, context.Canceled) || errors.Is(yieldErr, context.DeadlineExceeded) {
			return yieldErr
		}
		return oops.Code("excel_stream_write_error").Wrap(yieldErr)
	}

	if err := sw.Flush(); err != nil {
		return oops.Code("excel_flush_error").Wrap(err)
	}

	return file.Write(w)
}

// StreamExportCSV 使用生成器迭代函数将元素直接导出为 CSV 格式，支持百万级数据高效导出并保持 O(1) 内存。
func StreamExportCSV[T any](
	ctx context.Context,
	w io.Writer,
	headings []string,
	generator func(yield func(item T) bool) error,
	mapper func(item T) []any,
) error {
	if w == nil {
		return oops.Code("nil_writer").Errorf("writer cannot be nil")
	}
	if generator == nil {
		return oops.Code("nil_generator").Errorf("generator cannot be nil")
	}

	cw := csv.NewWriter(w)

	if len(headings) > 0 {
		if err := cw.Write(headings); err != nil {
			return oops.Code("csv_write_error").Wrap(err)
		}
	}

	// yieldErr 同 StreamExport：生成器无法回传错误，必须在闭包外捕获首个失败原因。
	var yieldErr error

	genErr := generator(func(item T) bool {
		select {
		case <-ctx.Done():
			yieldErr = ctx.Err()
			return false
		default:
		}

		var rowValues []any
		if mapper != nil {
			rowValues = mapper(item)
		} else {
			rowValues = []any{item}
		}

		record := make([]string, len(rowValues))
		for i, v := range rowValues {
			record[i] = fmt.Sprintf("%v", formatValue(v))
		}

		if err := cw.Write(record); err != nil {
			yieldErr = err
			return false
		}
		return true
	})
	if genErr != nil {
		return oops.Code("csv_generator_error").Wrap(genErr)
	}
	if yieldErr != nil {
		// 同 StreamExport：取消与写失败必须对调用方可见地分开。
		if errors.Is(yieldErr, context.Canceled) || errors.Is(yieldErr, context.DeadlineExceeded) {
			return yieldErr
		}
		return oops.Code("csv_write_error").Wrap(yieldErr)
	}

	// csv.Writer 会把写失败延迟到 Flush 才暴露，因此必须显式 Flush 并检查 Error()。
	cw.Flush()
	if err := cw.Error(); err != nil {
		return oops.Code("csv_flush_error").Wrap(err)
	}

	return nil
}

func resolveSheetName(file *excelize.File, requested string) (string, error) {
	if requested != "" {
		return requested, nil
	}
	sheets := file.GetSheetList()
	if len(sheets) == 0 {
		return "", oops.Code("empty_sheets").Errorf("no sheets found in excel workbook")
	}
	return sheets[0], nil
}

func processImportRow[T any](
	rowColumns []string,
	rowIdx int,
	mapper func(row []string, rowIdx int) (T, error),
	consumer func(item T, rowIdx int) error,
	skipInvalid bool,
) (bool, error) {
	if len(rowColumns) == 0 {
		return false, nil
	}

	item, mapErr := mapper(rowColumns, rowIdx)
	if mapErr != nil {
		if skipInvalid {
			return false, nil
		}
		return false, oops.Code("excel_map_error").With("row_idx", rowIdx).Wrap(mapErr)
	}

	if err := consumer(item, rowIdx); err != nil {
		return false, oops.Code("excel_consume_error").With("row_idx", rowIdx).Wrap(err)
	}

	return true, nil
}

func iterateRows[T any](
	ctx context.Context,
	rows *excelize.Rows,
	headerRowIndex int,
	skipInvalid bool,
	mapper func(row []string, rowIdx int) (T, error),
	consumer func(item T, rowIdx int) error,
) (int, error) {
	currentRowIdx, processedCount := 0, 0
	for rows.Next() {
		currentRowIdx++
		select {
		case <-ctx.Done():
			return processedCount, oops.Code("context_canceled").With("row_idx", currentRowIdx).Wrap(ctx.Err())
		default:
		}

		if currentRowIdx <= headerRowIndex {
			continue
		}

		rowCols, colErr := rows.Columns()
		if colErr != nil {
			if skipInvalid {
				continue
			}
			return processedCount, oops.Code("excel_read_row_error").With("row_idx", currentRowIdx).Wrap(colErr)
		}

		processed, procErr := processImportRow(rowCols, currentRowIdx, mapper, consumer, skipInvalid)
		if procErr != nil {
			return processedCount, procErr
		}
		if processed {
			processedCount++
		}
	}
	return processedCount, nil
}

// StreamImport 从 Excel 读取器按行顺序流式读取，保持低内存开销。
func StreamImport[T any](
	ctx context.Context,
	r io.Reader,
	mapper func(row []string, rowIdx int) (T, error),
	consumer func(item T, rowIdx int) error,
	opts ...StreamImportOption,
) (int, error) {
	if r == nil {
		return 0, oops.Code("nil_reader").Errorf("reader cannot be nil")
	}
	if mapper == nil || consumer == nil {
		return 0, oops.Code("nil_callback").Errorf("mapper and consumer cannot be nil")
	}

	cfg := newDefaultImportConfig()
	for _, opt := range opts {
		opt(cfg)
	}

	file, err := excelize.OpenReader(r)
	if err != nil {
		return 0, oops.Code("excel_open_error").Wrap(err)
	}
	defer func() { _ = file.Close() }()

	sheetName, err := resolveSheetName(file, cfg.sheetName)
	if err != nil {
		return 0, err
	}

	rows, err := file.Rows(sheetName)
	if err != nil {
		return 0, oops.Code("excel_rows_error").Wrap(err)
	}
	defer func() { _ = rows.Close() }()

	return iterateRows(ctx, rows, cfg.headerRowIndex, cfg.skipInvalidRows, mapper, consumer)
}

// StreamImportCSV 从 CSV 读取器按行顺序流式读取，具备真正的 O(1) 内存特性。
func StreamImportCSV[T any](
	ctx context.Context,
	r io.Reader,
	mapper func(row []string, rowIdx int) (T, error),
	consumer func(item T, rowIdx int) error,
	opts ...StreamImportOption,
) (int, error) {
	if r == nil {
		return 0, oops.Code("nil_reader").Errorf("reader cannot be nil")
	}
	if mapper == nil || consumer == nil {
		return 0, oops.Code("nil_callback").Errorf("mapper and consumer cannot be nil")
	}

	cfg := newDefaultImportConfig()
	for _, opt := range opts {
		opt(cfg)
	}

	cr := csv.NewReader(r)
	currentRowIdx, processedCount := 0, 0

	for {
		select {
		case <-ctx.Done():
			return processedCount, oops.Code("context_canceled").Wrap(ctx.Err())
		default:
		}

		record, err := cr.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			if cfg.skipInvalidRows {
				continue
			}
			return processedCount, oops.Code("csv_read_error").With("row_idx", currentRowIdx).Wrap(err)
		}

		currentRowIdx++
		if currentRowIdx <= cfg.headerRowIndex {
			continue
		}

		processed, procErr := processImportRow(record, currentRowIdx, mapper, consumer, cfg.skipInvalidRows)
		if procErr != nil {
			return processedCount, procErr
		}
		if processed {
			processedCount++
		}
	}

	return processedCount, nil
}

func checkRowHeaderOrSkip(cols []string, headerRowIdx, currentRowIdx int) (skip bool, isHeader bool) {
	if len(cols) == 0 || isRowEmpty(cols) {
		return true, false
	}
	if headerRowIdx > 0 && currentRowIdx < headerRowIdx {
		return true, false
	}
	if headerRowIdx > 0 && currentRowIdx == headerRowIdx {
		return false, true
	}
	return false, false
}

func decodeElement[T any](cols, headers []string, isPtr bool, baseType reflect.Type) (T, error) {
	newElem := reflect.New(baseType)
	if err := mapRowToStruct(cols, headers, newElem.Elem()); err != nil {
		var zero T
		return zero, err
	}

	var item T
	if isPtr {
		val, ok := reflect.TypeAssert[T](newElem)
		if ok {
			item = val
		}
	} else {
		val, ok := reflect.TypeAssert[T](newElem.Elem())
		if ok {
			item = val
		}
	}

	return item, nil
}

// ImportSlice 将 Excel 单元格流直接反序列化为泛型结构体切片，避免外部构造繁重的 Concern 结构体。
func ImportSlice[T any](
	ctx context.Context,
	r io.Reader,
	dest *[]T,
	opts ...ImportOption,
) error {
	if r == nil {
		return oops.Code("excel_nil_pointer").In("excel.ImportSlice").Errorf("reader cannot be nil")
	}
	if dest == nil {
		return oops.Code("excel_nil_pointer").In("excel.ImportSlice").Errorf("destination slice pointer cannot be nil")
	}

	cfg := newDefaultImportConfig()
	for _, opt := range opts {
		opt(cfg)
	}

	file, err := excelize.OpenReader(r)
	if err != nil {
		return oops.Code("excel_open_error").Wrap(err)
	}
	defer func() { _ = file.Close() }()

	sheetName, err := resolveSheetName(file, cfg.sheetName)
	if err != nil {
		return err
	}

	rows, err := file.Rows(sheetName)
	if err != nil {
		return oops.Code("excel_rows_error").Wrap(err)
	}
	defer func() { _ = rows.Close() }()

	elemType := reflect.TypeFor[T]()
	isPtr := elemType.Kind() == reflect.Pointer
	baseType := elemType
	if isPtr {
		baseType = elemType.Elem()
	}

	var headers []string
	var items []T
	currentRowIdx := 0

	for rows.Next() {
		currentRowIdx++
		cols, colErr := rows.Columns()
		if colErr != nil {
			if cfg.skipInvalidRows {
				continue
			}
			return oops.Code("excel_read_row_error").With("row_idx", currentRowIdx).Wrap(colErr)
		}

		skip, isHeader := checkRowHeaderOrSkip(cols, cfg.headerRowIndex, currentRowIdx)
		if isHeader {
			headers = cols
			continue
		}
		if skip {
			continue
		}

		item, mapErr := decodeElement[T](cols, headers, isPtr, baseType)
		if mapErr != nil {
			if cfg.skipInvalidRows {
				continue
			}
			return oops.Code("excel_map_error").With("row_idx", currentRowIdx).Wrap(mapErr)
		}

		items = append(items, item)
	}

	*dest = items
	return nil
}
