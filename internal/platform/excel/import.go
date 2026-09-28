package excel

import (
	"context"
	"fmt"
	"io"
	"reflect"

	"github.com/xuri/excelize/v2"
)

type importPipeline struct {
	service   *Service
	importObj any
	cfg       *importConfig

	headers    []string
	currentRow int

	toSlice     ToSlice
	isToSlice   bool
	elementType reflect.Type
	sliceRows   [][]string

	toModel   ToModel
	isToModel bool
	batchSize int
	batch     []any

	onRow   OnRow
	isOnRow bool
}

func newImportPipeline(service *Service, importObj any, cfg *importConfig) *importPipeline {
	p := &importPipeline{
		service:   service,
		importObj: importObj,
		cfg:       cfg,
	}

	p.toSlice, p.isToSlice = importObj.(ToSlice)
	p.toModel, p.isToModel = importObj.(ToModel)
	p.onRow, p.isOnRow = importObj.(OnRow)

	if p.isToModel {
		p.batchSize = 1
		if b, ok := importObj.(WithBatchInserts); ok && b.BatchSize() > 0 {
			p.batchSize = b.BatchSize()
		}
		if cfg != nil && cfg.batchSize > 0 {
			p.batchSize = cfg.batchSize
		}
	}

	return p
}

// Run 执行导入管线，流式解析数据并按契约派发。
func (p *importPipeline) Run(ctx context.Context, r io.Reader) error {
	if err := p.prepareSliceTarget(); err != nil {
		return err
	}

	f, err := excelize.OpenReader(r)
	if err != nil {
		return fmt.Errorf("failed to open excel reader: %w", err)
	}
	defer func() { _ = f.Close() }()

	sheetName, err := resolveSheetName(f, p.cfg.sheetName)
	if err != nil {
		return err
	}

	rows, err := f.Rows(sheetName)
	if err != nil {
		return fmt.Errorf("failed to read sheet %s: %w", sheetName, err)
	}
	defer func() { _ = rows.Close() }()

	if err = p.scanRows(ctx, rows); err != nil {
		return err
	}

	if err = p.flushModels(ctx); err != nil {
		return err
	}

	return p.writeSliceTarget()
}

func (p *importPipeline) prepareSliceTarget() error {
	if !p.isToSlice {
		return nil
	}

	destVal := reflect.ValueOf(p.toSlice.ToSlice())
	if destVal.Kind() != reflect.Pointer || destVal.Elem().Kind() != reflect.Slice {
		return fmt.Errorf("ToSlice() must return a pointer to a slice, got %T", p.toSlice.ToSlice())
	}

	p.elementType = destVal.Elem().Type().Elem()
	if p.elementType.Kind() == reflect.Pointer {
		p.elementType = p.elementType.Elem()
	}

	return nil
}

func (p *importPipeline) scanRows(ctx context.Context, rows *excelize.Rows) error {
	headingRowIdx := p.cfg.headerRowIndex
	if h, ok := p.importObj.(WithHeadingRow); ok {
		headingRowIdx = h.HeadingRow()
	}

	for rows.Next() {
		p.currentRow++

		cols, err := rows.Columns()
		if err != nil {
			if p.cfg != nil && p.cfg.skipInvalidRows {
				continue
			}
			return fmt.Errorf("failed to scan row %d: %w", p.currentRow, err)
		}

		if len(cols) == 0 || isRowEmpty(cols) {
			continue
		}

		if headingRowIdx > 0 && p.currentRow < headingRowIdx {
			continue
		}

		if headingRowIdx > 0 && p.currentRow == headingRowIdx {
			p.headers = cols
			continue
		}

		if err = p.processRow(ctx, cols); err != nil {
			return err
		}
	}

	return nil
}

func (p *importPipeline) processRow(ctx context.Context, cols []string) error {
	switch {
	case p.isOnRow:
		if err := p.onRow.OnRow(cols); err != nil {
			return fmt.Errorf("failed to process row %d in OnRow: %w", p.currentRow, err)
		}
	case p.isToModel:
		return p.processModelRow(ctx, cols)
	case p.isToSlice:
		p.sliceRows = append(p.sliceRows, cols)
	}

	return nil
}

func (p *importPipeline) processModelRow(ctx context.Context, cols []string) error {
	model, err := p.toModel.ToModel(cols)
	if err != nil {
		if p.cfg != nil && p.cfg.skipInvalidRows {
			return nil
		}
		return fmt.Errorf("failed to convert row %d to model: %w", p.currentRow, err)
	}

	if v, ok := p.importObj.(WithValidation); ok {
		if err = v.Validate(model); err != nil {
			if p.cfg != nil && p.cfg.skipInvalidRows {
				return nil
			}
			return fmt.Errorf("row %d validation failed: %w", p.currentRow, err)
		}
	}

	p.batch = append(p.batch, model)
	if len(p.batch) >= p.batchSize {
		if err = p.service.insertBatch(ctx, p.importObj, p.batch); err != nil {
			return fmt.Errorf("failed to insert batch of models: %w", err)
		}

		p.batch = nil
	}

	return nil
}

func (p *importPipeline) flushModels(ctx context.Context) error {
	if !p.isToModel || len(p.batch) == 0 {
		return nil
	}

	if err := p.service.insertBatch(ctx, p.importObj, p.batch); err != nil {
		return fmt.Errorf("failed to flush remaining batch of models: %w", err)
	}

	return nil
}

func (p *importPipeline) writeSliceTarget() error {
	if !p.isToSlice || len(p.sliceRows) == 0 {
		return nil
	}

	destVal := reflect.ValueOf(p.toSlice.ToSlice()).Elem()
	isPtrSlice := destVal.Type().Elem().Kind() == reflect.Pointer
	sliceElements := make([]reflect.Value, 0, len(p.sliceRows))

	for _, row := range p.sliceRows {
		newElemPtr := reflect.New(p.elementType)
		newElem := newElemPtr.Elem()

		if err := mapRowToStruct(row, p.headers, newElem); err != nil {
			return err
		}

		if v, ok := p.importObj.(WithValidation); ok {
			if err := v.Validate(newElemPtr.Interface()); err != nil {
				return fmt.Errorf("data validation failed: %w", err)
			}
		}

		if isPtrSlice {
			sliceElements = append(sliceElements, newElemPtr)
		} else {
			sliceElements = append(sliceElements, newElem)
		}
	}

	resultSlice := reflect.MakeSlice(destVal.Type(), len(sliceElements), len(sliceElements))
	for i, el := range sliceElements {
		resultSlice.Index(i).Set(el)
	}

	destVal.Set(resultSlice)

	return nil
}
