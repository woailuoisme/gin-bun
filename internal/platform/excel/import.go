package excel

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"reflect"
	"slices"

	"github.com/samber/oops"
	"github.com/xuri/excelize/v2"

	"bgin/internal/platform/errs"
)

type importPipeline struct {
	service   *Service
	importObj any
	cfg       *importConfig

	headers    []string
	currentRow int

	toSlice         ToSlice
	isToSlice       bool
	elementType     reflect.Type
	sliceRows       [][]string
	sliceRowNumbers []int

	toChunk   ToChunk
	isToChunk bool
	chunkSize int
	chunkRows [][]string

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
	p.toChunk, p.isToChunk = importObj.(ToChunk)
	p.onRow, p.isOnRow = importObj.(OnRow)

	if p.isToChunk {
		p.chunkSize = defaultChunkSize
		if cr, ok := importObj.(WithChunkReading); ok && cr.ChunkSize() > 0 {
			p.chunkSize = cr.ChunkSize()
		}
		if cfg != nil && cfg.batchSize > 0 {
			p.chunkSize = cfg.batchSize
		}
		p.chunkRows = make([][]string, 0, p.chunkSize)
	}

	if p.isToModel {
		p.batchSize = 1
		if b, ok := importObj.(WithBatchInserts); ok && b.BatchSize() > 0 {
			p.batchSize = b.BatchSize()
		} else if cr, ok := importObj.(WithChunkReading); ok && cr.ChunkSize() > 0 {
			p.batchSize = cr.ChunkSize()
		}
		if cfg != nil && cfg.batchSize > 0 {
			p.batchSize = cfg.batchSize
		}
	}

	return p
}

// Run 执行导入管线，流式解析数据并按契约派发，返回汇总结果与详细失败报告。
func (p *importPipeline) Run(ctx context.Context, r io.Reader) (*ImportResult, error) {
	if ms, ok := p.importObj.(WithMultipleSheetsImport); ok {
		return p.runMultipleSheets(ctx, ms, r)
	}

	if err := p.prepareSliceTarget(); err != nil {
		return nil, err
	}

	f, cleanup, err := openExcelSafely(r, p.cfg, p.service.tempPath)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = f.Close()
		cleanup()
	}()

	sheetName, err := resolveSheetName(f, p.cfg.sheetName)
	if err != nil {
		return nil, err
	}

	return p.runWithFile(ctx, f, sheetName)
}

func (p *importPipeline) runMultipleSheets(ctx context.Context, ms WithMultipleSheetsImport, r io.Reader) (*ImportResult, error) {
	f, cleanup, err := openExcelSafely(r, p.cfg, p.service.tempPath)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = f.Close()
		cleanup()
	}()

	sheetsMap := ms.Sheets()
	result := &ImportResult{}

	for sheetName, handlerObj := range sheetsMap {
		subCfg := *p.cfg
		subCfg.sheetName = sheetName

		subPipeline := newImportPipeline(p.service, handlerObj, &subCfg)
		if subPipeline.isToSlice {
			if err := subPipeline.prepareSliceTarget(); err != nil {
				return result, err
			}
		}

		subRes, err := subPipeline.runWithFile(ctx, f, sheetName)
		if err != nil {
			return result, err
		}

		if subRes != nil {
			result.TotalRows += subRes.TotalRows
			result.ImportedCount += subRes.ImportedCount
			result.SkippedCount += subRes.SkippedCount
			result.Failures = append(result.Failures, subRes.Failures...)
		}
	}

	return result, nil
}

func (p *importPipeline) runWithFile(ctx context.Context, f *excelize.File, sheetName string) (*ImportResult, error) {
	rows, err := f.Rows(sheetName)
	if err != nil {
		return nil, fmt.Errorf("failed to read sheet %s: %w", sheetName, err)
	}
	defer func() { _ = rows.Close() }()

	result := &ImportResult{}
	if err = p.scanRows(ctx, rows, result); err != nil {
		return result, err
	}

	if err = p.flushChunk(ctx, result); err != nil {
		return result, err
	}

	if err = p.flushModels(ctx); err != nil {
		return result, err
	}

	if err = p.writeSliceTarget(result); err != nil {
		return result, err
	}

	return result, nil
}

func openExcelSafely(r io.Reader, cfg *importConfig, tempDir string) (*excelize.File, func(), error) {
	opts := excelize.Options{
		UnzipSizeLimit:    cfg.unzipSizeLimit,
		UnzipXMLSizeLimit: cfg.unzipXMLSizeLimit,
	}

	maxLimit := cfg.maxFileSize
	if maxLimit <= 0 {
		maxLimit = defaultMaxFileSize
	}

	if file, ok := r.(*os.File); ok {
		fi, statErr := file.Stat()
		if statErr == nil && fi.Size() > maxLimit {
			return nil, nil, oops.Code(errs.CodeExcelFileTooLarge).
				Errorf("excel file size %d bytes exceeds limit %d bytes", fi.Size(), maxLimit)
		}

		f, err := excelize.OpenFile(file.Name(), opts)
		if err != nil {
			return nil, nil, oops.Code(errs.CodeExcelOpenError).Wrap(err)
		}
		return f, func() {}, nil
	}

	threshold := cfg.memorySpoolThreshold
	if threshold <= 0 {
		threshold = defaultMemorySpoolThreshold
	}

	limitedReader := io.LimitReader(r, maxLimit+1)
	buf := make([]byte, threshold+1)
	n, readErr := io.ReadFull(limitedReader, buf)
	if readErr == io.EOF || readErr == io.ErrUnexpectedEOF {
		if int64(n) > maxLimit {
			return nil, nil, oops.Code(errs.CodeExcelFileTooLarge).
				Errorf("excel stream size %d bytes exceeds limit of %d bytes", n, maxLimit)
		}

		f, openErr := excelize.OpenReader(bytes.NewReader(buf[:n]), opts)
		if openErr != nil {
			return nil, nil, oops.Code(errs.CodeExcelOpenError).Wrap(openErr)
		}
		return f, func() {}, nil
	}
	if readErr != nil {
		return nil, nil, oops.Code(errs.CodeExcelReadError).Wrap(readErr)
	}

	return spoolToDiskAndOpen(buf[:n], limitedReader, maxLimit, opts, tempDir)
}

func spoolToDiskAndOpen(initialBuf []byte, remaining io.Reader, maxLimit int64, opts excelize.Options, tempDir string) (*excelize.File, func(), error) {
	if err := os.MkdirAll(tempDir, 0o750); err != nil {
		tempDir = os.TempDir()
	}

	tmpFile, err := os.CreateTemp(tempDir, "bgin-excel-spool-*.xlsx")
	if err != nil {
		return nil, nil, oops.Code(errs.CodeExcelTempFileError).Wrap(err)
	}
	cleanup := func() {
		_ = os.Remove(tmpFile.Name())
	}

	if _, err = tmpFile.Write(initialBuf); err != nil {
		_ = tmpFile.Close()
		cleanup()
		return nil, nil, oops.Code(errs.CodeExcelSpoolWriteError).Wrap(err)
	}

	written, copyErr := io.Copy(tmpFile, remaining)
	if copyErr != nil {
		_ = tmpFile.Close()
		cleanup()
		return nil, nil, oops.Code(errs.CodeExcelSpoolCopyError).Wrap(copyErr)
	}

	totalBytes := int64(len(initialBuf)) + written
	if totalBytes > maxLimit {
		_ = tmpFile.Close()
		cleanup()
		return nil, nil, oops.Code(errs.CodeExcelFileTooLarge).
			Errorf("excel stream size exceeds max file size limit of %d bytes", maxLimit)
	}

	if err = tmpFile.Close(); err != nil {
		cleanup()
		return nil, nil, oops.Code(errs.CodeExcelSpoolCloseError).Wrap(err)
	}

	f, openErr := excelize.OpenFile(tmpFile.Name(), opts)
	if openErr != nil {
		cleanup()
		return nil, nil, oops.Code(errs.CodeExcelOpenError).Wrap(openErr)
	}

	return f, cleanup, nil
}

func (p *importPipeline) prepareSliceTarget() error {
	if !p.isToSlice {
		return nil
	}

	destVal := reflect.ValueOf(p.toSlice.ToSlice())
	if destVal.Kind() != reflect.Pointer || destVal.Elem().Kind() != reflect.Slice {
		return fmt.Errorf("excel: ToSlice() must return a pointer to a slice, got %T", p.toSlice.ToSlice())
	}

	p.elementType = destVal.Elem().Type().Elem()
	if p.elementType.Kind() == reflect.Pointer {
		p.elementType = p.elementType.Elem()
	}

	return nil
}

func (p *importPipeline) scanRows(ctx context.Context, rows *excelize.Rows, result *ImportResult) error {
	headingRowIdx := p.cfg.headerRowIndex
	if h, ok := p.importObj.(WithHeadingRow); ok {
		headingRowIdx = h.HeadingRow()
	}

	skipsOnFailure, hasSkips := p.importObj.(SkipsOnFailure)
	canSkip := hasSkips || p.cfg.skipInvalidRows

	for rows.Next() {
		p.currentRow++

		cols, err := rows.Columns()
		if err != nil {
			if canSkip {
				failure := RowFailure{
					Row:   p.currentRow,
					Error: fmt.Errorf("failed to scan row columns: %w", err),
				}
				result.Failures = append(result.Failures, failure)
				result.SkippedCount++
				if hasSkips {
					skipsOnFailure.OnFailure(failure)
				}
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
			p.headers = slices.Clone(cols)
			continue
		}

		if p.cfg.maxRows > 0 && result.TotalRows >= p.cfg.maxRows {
			break
		}

		result.TotalRows++

		if err = p.processRow(ctx, cols, result); err != nil {
			return err
		}
	}

	return nil
}

func (p *importPipeline) processRow(ctx context.Context, cols []string, result *ImportResult) error {
	skipsOnFailure, hasSkips := p.importObj.(SkipsOnFailure)
	canSkip := hasSkips || p.cfg.skipInvalidRows

	switch {
	case p.isOnRow:
		if err := p.onRow.OnRow(cols); err != nil {
			if canSkip {
				failure := RowFailure{
					Row:    p.currentRow,
					Values: cols,
					Error:  err,
				}
				result.Failures = append(result.Failures, failure)
				result.SkippedCount++
				if hasSkips {
					skipsOnFailure.OnFailure(failure)
				}
				return nil
			}
			return fmt.Errorf("failed to process row %d in OnRow: %w", p.currentRow, err)
		}
		result.ImportedCount++

	case p.isToChunk:
		p.chunkRows = append(p.chunkRows, cols)
		if len(p.chunkRows) >= p.chunkSize {
			if err := p.flushChunk(ctx, result); err != nil {
				return err
			}
		}

	case p.isToModel:
		return p.processModelRow(ctx, cols, result)

	case p.isToSlice:
		p.sliceRows = append(p.sliceRows, cols)
		p.sliceRowNumbers = append(p.sliceRowNumbers, p.currentRow)
	}

	if p.cfg.onProgress != nil {
		p.cfg.onProgress(result.ImportedCount, result.TotalRows)
	}
	if ev, ok := p.importObj.(WithEvents); ok {
		ev.OnProgress(result.ImportedCount, result.TotalRows)
	}

	return nil
}

func (p *importPipeline) flushChunk(ctx context.Context, result *ImportResult) error {
	if !p.isToChunk || len(p.chunkRows) == 0 {
		return nil
	}

	chunkData := p.chunkRows
	p.chunkRows = make([][]string, 0, p.chunkSize)

	skipsOnFailure, hasSkips := p.importObj.(SkipsOnFailure)
	canSkip := hasSkips || p.cfg.skipInvalidRows

	if err := p.toChunk.Chunk(ctx, chunkData); err != nil {
		if canSkip {
			failure := RowFailure{
				Row:   p.currentRow,
				Error: fmt.Errorf("chunk processing failed: %w", err),
			}
			result.Failures = append(result.Failures, failure)
			result.SkippedCount += len(chunkData)
			if hasSkips {
				skipsOnFailure.OnFailure(failure)
			}
			return nil
		}
		return fmt.Errorf("failed to process chunk at row %d: %w", p.currentRow, err)
	}

	result.ImportedCount += len(chunkData)
	return nil
}

func (p *importPipeline) processModelRow(ctx context.Context, cols []string, result *ImportResult) error {
	skipsOnFailure, hasSkips := p.importObj.(SkipsOnFailure)
	canSkip := hasSkips || p.cfg.skipInvalidRows

	model, err := p.toModel.ToModel(cols)
	if err != nil {
		if canSkip {
			failure := RowFailure{
				Row:    p.currentRow,
				Values: cols,
				Error:  err,
			}
			result.Failures = append(result.Failures, failure)
			result.SkippedCount++
			if hasSkips {
				skipsOnFailure.OnFailure(failure)
			}
			return nil
		}
		return fmt.Errorf("failed to convert row %d to model: %w", p.currentRow, err)
	}

	valid, valErr := p.validateTarget(model, p.currentRow, cols, result, canSkip, skipsOnFailure)
	if valErr != nil {
		return valErr
	}
	if !valid {
		return nil
	}

	p.batch = append(p.batch, model)
	result.ImportedCount++

	if inserter, ok := p.importObj.(BatchInserter); ok {
		if len(p.batch) >= p.batchSize {
			if err = inserter.InsertBatch(ctx, p.batch); err != nil {
				return fmt.Errorf("failed to insert batch of models: %w", err)
			}
			p.batch = nil
		}
	}

	return nil
}

func (p *importPipeline) flushModels(ctx context.Context) error {
	if !p.isToModel || len(p.batch) == 0 {
		return nil
	}

	if inserter, ok := p.importObj.(BatchInserter); ok {
		if err := inserter.InsertBatch(ctx, p.batch); err != nil {
			return fmt.Errorf("failed to flush remaining batch of models: %w", err)
		}
	}

	return nil
}

func (p *importPipeline) validateTarget(
	target any,
	rowNum int,
	cols []string,
	result *ImportResult,
	canSkip bool,
	skipsOnFailure SkipsOnFailure,
) (bool, error) {
	v, ok := p.importObj.(WithValidation)
	if !ok {
		return true, nil
	}

	if err := v.Validate(target); err != nil {
		if canSkip {
			failure := RowFailure{
				Row:    rowNum,
				Values: cols,
				Error:  err,
			}
			result.Failures = append(result.Failures, failure)
			result.SkippedCount++
			if skipsOnFailure != nil {
				skipsOnFailure.OnFailure(failure)
			}
			return false, nil
		}
		return false, fmt.Errorf("row %d validation failed: %w", rowNum, err)
	}

	return true, nil
}

func (p *importPipeline) writeSliceTarget(result *ImportResult) error {
	if !p.isToSlice || len(p.sliceRows) == 0 {
		return nil
	}

	destVal := reflect.ValueOf(p.toSlice.ToSlice()).Elem()
	isPtrSlice := destVal.Type().Elem().Kind() == reflect.Pointer
	sliceElements := make([]reflect.Value, 0, len(p.sliceRows))

	skipsOnFailure, hasSkips := p.importObj.(SkipsOnFailure)
	canSkip := hasSkips || p.cfg.skipInvalidRows

	for idx, row := range p.sliceRows {
		rowNum := p.sliceRowNumbers[idx]
		newElemPtr := reflect.New(p.elementType)
		newElem := newElemPtr.Elem()

		if err := mapRowToStruct(row, p.headers, newElem); err != nil {
			if canSkip {
				failure := RowFailure{
					Row:    rowNum,
					Values: row,
					Error:  err,
				}
				result.Failures = append(result.Failures, failure)
				result.SkippedCount++
				if hasSkips {
					skipsOnFailure.OnFailure(failure)
				}
				continue
			}
			return err
		}

		valid, valErr := p.validateTarget(newElemPtr.Interface(), rowNum, row, result, canSkip, skipsOnFailure)
		if valErr != nil {
			return valErr
		}
		if !valid {
			continue
		}

		result.ImportedCount++
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
