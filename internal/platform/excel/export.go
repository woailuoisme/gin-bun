package excel

import (
	"context"
	"database/sql"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"math"
	"reflect"
	"strings"

	"github.com/samber/oops"
	"github.com/xuri/excelize/v2"
)

const (
	defaultSheetName       = "Sheet1"
	defaultMaxRowsPerSheet = 1000000 // Excel 规范单工作表最大行数上限为 1,048,576
	widthSampleLimit       = 500     // 自动列宽抽样前 500 行以平衡性能与排版
)

type exportPipeline struct {
	service *Service
	export  any
	cfg     *exportConfig
}

func newExportPipeline(service *Service, export any, cfg *exportConfig) *exportPipeline {
	return &exportPipeline{
		service: service,
		export:  export,
		cfg:     cfg,
	}
}

// Write 执行导出管线，将数据流式写入输出端。
func (p *exportPipeline) Write(ctx context.Context, w io.Writer) error {
	if _, isQuery := p.export.(FromQuery); isQuery {
		if isCSV(p.cfg.sheetName) {
			return p.writeStreamCSV(ctx, w)
		}
		return p.writeStream(ctx, w)
	}

	if _, isSlice := p.export.(FromSlice); !isSlice {
		return errors.New("export object must implement either FromSlice or FromQuery")
	}

	f := excelize.NewFile()
	defer func() { _ = f.Close() }()

	ensureSheet(f, p.cfg.sheetName)

	if err := p.writeMemory(f); err != nil {
		return fmt.Errorf("failed to write excel data: %w", err)
	}

	if err := p.applyWorkbookWidths(f, p.cfg.sheetName); err != nil {
		return fmt.Errorf("failed to adjust column widths: %w", err)
	}

	if err := f.Write(w); err != nil {
		return fmt.Errorf("failed to write file to destination: %w", err)
	}

	return nil
}

func (p *exportPipeline) writeMemory(f *excelize.File) error {
	fromSlice, ok := p.export.(FromSlice)
	if !ok {
		return errors.New("export contract does not implement FromSlice")
	}

	rawSlice := fromSlice.FromSlice()
	val := reflect.ValueOf(rawSlice)

	if val.Kind() == reflect.Pointer {
		val = val.Elem()
	}

	if val.Kind() != reflect.Slice {
		return errors.New("FromSlice must return a slice or slice pointer")
	}

	rowOffset := 1

	if h, ok := p.export.(WithHeadings); ok {
		headers := h.Headings()
		if len(headers) > 0 {
			cell, _ := excelize.CoordinatesToCellName(1, rowOffset)
			if err := f.SetSheetRow(p.cfg.sheetName, cell, &headers); err != nil {
				return err
			}
			rowOffset++
		}
	}

	mapper, hasMapper := p.export.(WithMapping)
	length := val.Len()

	for i := range length {
		item := val.Index(i).Interface()

		var cols []any
		if hasMapper {
			cols = mapper.Mapping(item)
		} else {
			cols = getRowValuesFromStruct(item)
		}

		cell, _ := excelize.CoordinatesToCellName(1, rowOffset)
		if err := f.SetSheetRow(p.cfg.sheetName, cell, &cols); err != nil {
			return err
		}

		rowOffset++
	}

	return nil
}

func (p *exportPipeline) writeStream(ctx context.Context, w io.Writer) error {
	rows, err := p.fetchQueryRows(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()

	return p.writeStreamWorkbook(ctx, rows, w)
}

func (p *exportPipeline) writeStreamCSV(ctx context.Context, w io.Writer) error {
	rows, err := p.fetchQueryRows(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()

	cw := csv.NewWriter(w)

	if h, ok := p.export.(WithHeadings); ok {
		headers := h.Headings()
		if len(headers) > 0 {
			if err := cw.Write(headers); err != nil {
				return err
			}
		}
	}

	mapper, hasMapper := p.export.(WithMapping)

	for rows.Next() {
		if err := ctx.Err(); err != nil {
			return err
		}

		values, err := scanSQLRowValues(rows)
		if err != nil {
			return err
		}

		var cols []any
		if hasMapper {
			cols = mapper.Mapping(values)
		} else {
			cols = values
		}

		record := make([]string, len(cols))
		for idx, col := range cols {
			record[idx] = fmt.Sprintf("%v", formatValue(col))
		}

		if err := cw.Write(record); err != nil {
			return err
		}
	}

	// csv.Writer 把写失败延迟到 Flush 才暴露，因此必须在返回前显式 Flush 并检查；
	// 两个错误都保留，避免落盘失败被 rows.Err() 掩盖。
	cw.Flush()

	return errors.Join(rows.Err(), cw.Error())
}

var errDBUnavailable = errors.New("database connection is not available in excel service")

func resolveSQLDB(cfg *exportConfig, svc *Service) (*sql.DB, error) {
	if cfg != nil && cfg.db != nil {
		return cfg.db, nil
	}
	if svc != nil && svc.db != nil {
		return svc.db, nil
	}
	return nil, errDBUnavailable
}

func (p *exportPipeline) fetchQueryRows(ctx context.Context) (*sql.Rows, error) {
	fromQuery, ok := p.export.(FromQuery)
	if !ok {
		return nil, errors.New("export contract does not implement FromQuery")
	}

	sqlDB, err := resolveSQLDB(p.cfg, p.service)
	if err != nil {
		return nil, err
	}

	return fromQuery.QueryRows(ctx, sqlDB)
}

func (p *exportPipeline) getSheetChunkSize() int {
	if p.cfg != nil && p.cfg.sheetChunkSize > 0 {
		return p.cfg.sheetChunkSize
	}
	if c, ok := p.export.(WithSheetChunkSize); ok && c.SheetChunkSize() > 0 {
		return c.SheetChunkSize()
	}
	return defaultMaxRowsPerSheet
}

func scanSQLRowValues(rows *sql.Rows) ([]any, error) {
	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}

	values := make([]any, len(cols))
	scanArgs := make([]any, len(cols))
	for i := range values {
		scanArgs[i] = &values[i]
	}

	if err := rows.Scan(scanArgs...); err != nil {
		return nil, err
	}

	for i, v := range values {
		if b, ok := v.([]byte); ok {
			values[i] = string(b)
		}
	}

	return values, nil
}

// writeStreamWorkbook 使用 StreamWriter 直接向 io.Writer 流式输出，完全消除磁盘临时文件。
func (p *exportPipeline) writeStreamWorkbook(ctx context.Context, rows *sql.Rows, w io.Writer) error {
	file := excelize.NewFile()
	defer func() { _ = file.Close() }()

	currentSheet := p.cfg.sheetName
	ensureSheet(file, currentSheet)

	sw, err := file.NewStreamWriter(currentSheet)
	if err != nil {
		return oops.Code("excel_stream_error").Wrap(err)
	}

	widths := map[int]int{}
	rowOffset, err := p.writeStreamHeadings(sw, widths)
	if err != nil {
		return err
	}

	mapper, hasMapper := p.export.(WithMapping)
	maxRowsPerSheet := p.getSheetChunkSize()
	sheetIndex := 1
	totalRowsWritten := 0

	for rows.Next() {
		if err := ctx.Err(); err != nil {
			return err
		}

		if rowOffset > maxRowsPerSheet {
			sheetIndex++
			sw, currentSheet, rowOffset, widths, err = p.switchStreamSheet(file, currentSheet, sw, sheetIndex, widths)
			if err != nil {
				return err
			}
		}

		values, err := scanSQLRowValues(rows)
		if err != nil {
			return err
		}

		var cols []any
		if hasMapper {
			cols = mapper.Mapping(values)
		} else {
			cols = values
		}

		cell, _ := excelize.CoordinatesToCellName(1, rowOffset)
		vals := formatStreamRow(cols, widths, totalRowsWritten < widthSampleLimit)

		if err := sw.SetRow(cell, vals); err != nil {
			return err
		}

		rowOffset++
		totalRowsWritten++
	}

	if err := rows.Err(); err != nil {
		return err
	}

	if err := sw.Flush(); err != nil {
		return oops.Code("excel_flush_error").Wrap(err)
	}

	p.applyStreamColumnWidths(file, currentSheet, widths)

	return file.Write(w)
}

func (p *exportPipeline) switchStreamSheet(
	file *excelize.File,
	currentSheet string,
	oldSW *excelize.StreamWriter,
	sheetIndex int,
	widths map[int]int,
) (*excelize.StreamWriter, string, int, map[int]int, error) {
	if err := oldSW.Flush(); err != nil {
		return nil, "", 0, nil, err
	}
	p.applyStreamColumnWidths(file, currentSheet, widths)

	newSheet := fmt.Sprintf("%s_%d", p.cfg.sheetName, sheetIndex)
	_, _ = file.NewSheet(newSheet)

	sw, err := file.NewStreamWriter(newSheet)
	if err != nil {
		return nil, "", 0, nil, err
	}

	newWidths := map[int]int{}
	rowOffset, err := p.writeStreamHeadings(sw, newWidths)
	if err != nil {
		return nil, "", 0, nil, err
	}

	return sw, newSheet, rowOffset, newWidths, nil
}

func formatStreamRow(cols []any, widths map[int]int, sampleWidth bool) []any {
	vals := make([]any, len(cols))
	for idx, col := range cols {
		formatted := formatValue(col)
		vals[idx] = formatted

		if sampleWidth {
			cellLen := len([]rune(fmt.Sprintf("%v", formatted)))
			if current, exists := widths[idx]; !exists || cellLen > current {
				widths[idx] = cellLen
			}
		}
	}

	return vals
}

func (p *exportPipeline) writeStreamHeadings(
	sw *excelize.StreamWriter,
	widths map[int]int,
) (int, error) {
	rowOffset := 1

	h, ok := p.export.(WithHeadings)
	if !ok {
		return rowOffset, nil
	}

	headers := h.Headings()
	if len(headers) == 0 {
		return rowOffset, nil
	}

	cell, _ := excelize.CoordinatesToCellName(1, rowOffset)
	vals := make([]any, len(headers))

	for idx, header := range headers {
		vals[idx] = header
		widths[idx] = len([]rune(header))
	}

	if err := sw.SetRow(cell, vals); err != nil {
		return rowOffset, err
	}

	return rowOffset + 1, nil
}

func (p *exportPipeline) applyStreamColumnWidths(
	f *excelize.File,
	sheetName string,
	widths map[int]int,
) {
	if p.cfg != nil && p.cfg.columnWidths != nil {
		for colStr, width := range p.cfg.columnWidths {
			_ = f.SetColWidth(sheetName, colStr, colStr, width)
		}
		return
	}

	if customWidths, ok := p.export.(WithColumnWidths); ok {
		for colStr, width := range customWidths.ColumnWidths() {
			_ = f.SetColWidth(sheetName, colStr, colStr, width)
		}
		return
	}

	shouldAuto := false
	if p.cfg != nil && p.cfg.autoWidth != nil {
		shouldAuto = *p.cfg.autoWidth
	} else if autoSize, ok := p.export.(ShouldAutoSize); ok {
		shouldAuto = autoSize.ShouldAutoSize()
	}

	if shouldAuto {
		for colIdx, width := range widths {
			colName, err := excelize.ColumnNumberToName(colIdx + 1)
			if err != nil {
				continue
			}

			computedWidth := math.Max(float64(width+3), 10.0)
			_ = f.SetColWidth(sheetName, colName, colName, computedWidth)
		}
	}
}

func (p *exportPipeline) applyWorkbookWidths(f *excelize.File, sheetName string) error {
	if p.cfg != nil && p.cfg.columnWidths != nil {
		for col, width := range p.cfg.columnWidths {
			_ = f.SetColWidth(sheetName, col, col, width)
		}
		return nil
	}

	return adjustColumnWidths(p.export, f, sheetName)
}

func ensureSheet(f *excelize.File, sheetName string) {
	if sheetName == defaultSheetName {
		return
	}

	_, _ = f.NewSheet(sheetName)
	_ = f.DeleteSheet(defaultSheetName)
}

func isCSV(name string) bool {
	lower := strings.ToLower(name)
	return strings.HasSuffix(lower, ".csv")
}
