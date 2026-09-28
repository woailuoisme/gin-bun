package excel

import (
	"cmp"
	"context"
	"database/sql"
	"errors"
	"io"
	"sync"

	"github.com/samber/oops"

	"bgin/internal/config"
)

// ExportConcern 标记与导出操作相关的 Concern 接口契约。
type ExportConcern any

// ImportConcern 标记与导入操作相关的 Concern 接口契约。
type ImportConcern any

// FromSlice 声明导出数据来自内存切片。
type FromSlice interface {
	ExportConcern
	FromSlice() any
}

// FromQuery 声明导出数据通过 SQL 数据库游标流式获取，保持恒定低内存。
type FromQuery interface {
	ExportConcern
	QueryRows(ctx context.Context, db *sql.DB) (*sql.Rows, error)
}

// WithHeadings 提供自定义列标题切片。
type WithHeadings interface {
	ExportConcern
	Headings() []string
}

// WithMapping 定义单行数据到 Excel 单元格切片的显式转换逻辑。
type WithMapping interface {
	ExportConcern
	Mapping(row any) []any
}

// ShouldAutoSize 声明是否启用自动列宽计算。
type ShouldAutoSize interface {
	ExportConcern
	ShouldAutoSize() bool
}

// WithColumnWidths 显式定义特定列的像素/字符宽度。
type WithColumnWidths interface {
	ExportConcern
	ColumnWidths() map[string]float64
}

// WithTitle 定义导出的自定义工作表名称。
type WithTitle interface {
	ExportConcern
	Title() string
}

// WithSheetChunkSize 配置每个工作表的最大行数阈值（超限自动新建分表）。
type WithSheetChunkSize interface {
	ExportConcern
	SheetChunkSize() int
}

// ToSlice 声明将解析后的 Excel 数据反序列化到目标切片中。
type ToSlice interface {
	ImportConcern
	ToSlice() any
}

// ToModel 定义将单行单元格字符串转换为领域/数据库实体模型。
type ToModel interface {
	ImportConcern
	ToModel(row []string) (any, error)
}

// OnRow 提供轻量级逐行回调处理函数。
type OnRow interface {
	ImportConcern
	OnRow(row []string) error
}

// WithHeadingRow 指定表头所在的行号（1-based）。
type WithHeadingRow interface {
	ImportConcern
	HeadingRow() int
}

// WithValidation 提供对解析后行数据或模型的业务规则校验。
type WithValidation interface {
	ImportConcern
	Validate(model any) error
}

// WithBatchInserts 配置批量持久化写入时的批次大小。
type WithBatchInserts interface {
	ImportConcern
	BatchSize() int
}

// BatchInserter 提供批量写入数据库实体的持久化接口。
type BatchInserter interface {
	ImportConcern
	InsertBatch(ctx context.Context, batch []any) error
}

// WithConcurrency 配置多协程并发处理参数。
type WithConcurrency interface {
	ImportConcern
	Concurrency() int
}

// WithQueueNotification 定义队列异步任务完成或失败时的回调通知接口。
type WithQueueNotification interface {
	OnQueueSuccess(ctx context.Context, fileURL string) error
	OnQueueFailure(ctx context.Context, err error) error
}

// ExportOption 配置导出操作的函数式选项。
type ExportOption func(*exportConfig)

type exportConfig struct {
	sheetName      string
	db             *sql.DB
	autoWidth      *bool
	columnWidths   map[string]float64
	sheetChunkSize int
}

func newDefaultExportConfig() *exportConfig {
	return &exportConfig{
		sheetName:      defaultSheetName,
		sheetChunkSize: defaultMaxRowsPerSheet,
	}
}

// WithSheetName 覆盖导出时的默认工作表名称。
func WithSheetName(name string) ExportOption {
	return func(c *exportConfig) {
		if name != "" {
			c.sheetName = name
		}
	}
}

// WithDB 为流式数据库查询显式注入 SQL 数据库句柄。
func WithDB(db *sql.DB) ExportOption {
	return func(c *exportConfig) {
		c.db = db
	}
}

// WithAutoWidth 配置是否自动计算列宽。
func WithAutoWidth(auto bool) ExportOption {
	return func(c *exportConfig) {
		c.autoWidth = &auto
	}
}

// WithCustomColumnWidths 显式覆盖列宽映射。
func WithCustomColumnWidths(widths map[string]float64) ExportOption {
	return func(c *exportConfig) {
		c.columnWidths = widths
	}
}

// WithMaxRowsPerSheet 覆盖每个工作表的最大记录行数限制。
func WithMaxRowsPerSheet(size int) ExportOption {
	return func(c *exportConfig) {
		if size > 0 {
			c.sheetChunkSize = size
		}
	}
}

// ImportOption 配置导入操作的函数式选项。
type ImportOption func(*importConfig)

type importConfig struct {
	sheetName       string
	headerRowIndex  int
	skipInvalidRows bool
	batchSize       int
}

func newDefaultImportConfig() *importConfig {
	return &importConfig{
		sheetName:      defaultSheetName,
		headerRowIndex: 1,
	}
}

// WithImportSheetName 指定导入的目标工作表名称。
func WithImportSheetName(name string) ImportOption {
	return func(c *importConfig) {
		if name != "" {
			c.sheetName = name
		}
	}
}

// WithHeadingRowIndex 配置跳过的表头行数或表头所在行（从 1 开始计）。
func WithHeadingRowIndex(idx int) ImportOption {
	return func(c *importConfig) {
		if idx >= 0 {
			c.headerRowIndex = idx
		}
	}
}

// WithSkipInvalidRows 设置是否跳过无效或校验失败的数据行。
func WithSkipInvalidRows(skip bool) ImportOption {
	return func(c *importConfig) {
		c.skipInvalidRows = skip
	}
}

// WithImportBatchSize 配置导入时批量处理的行数大小。
func WithImportBatchSize(size int) ImportOption {
	return func(c *importConfig) {
		if size > 0 {
			c.batchSize = size
		}
	}
}

const defaultTempPath = ".cache/excel"

// Service 管理 Excel 与 CSV 的导入导出管线。
type Service struct {
	tempPath string
	db       *sql.DB
}

var (
	defaultMu      sync.RWMutex
	defaultService = NewService(defaultTempPath, nil)
)

// NewService 创建新的 Excel 服务实例。
func NewService(tempPath string, db *sql.DB) *Service {
	return &Service{
		tempPath: cmp.Or(tempPath, defaultTempPath),
		db:       db,
	}
}

// RegisterExcel 注册并配置 Excel 服务到全局。
func RegisterExcel(cfg *config.Config, db *sql.DB) (*Service, error) {
	tempPath := defaultTempPath
	if cfg != nil {
		tempPath = cmp.Or(cfg.Excel.TempPath, defaultTempPath)
	}

	service := NewService(tempPath, db)
	SetDefaultService(service)

	return service, nil
}

// SetDefaultService 注册全局默认的 Excel 服务实例。
func SetDefaultService(service *Service) {
	defaultMu.Lock()
	defer defaultMu.Unlock()

	if service == nil {
		defaultService = NewService(defaultTempPath, nil)
		return
	}

	defaultService = service
}

// DefaultService 获取全局默认的 Excel 服务实例。
func DefaultService() *Service {
	defaultMu.RLock()
	defer defaultMu.RUnlock()

	return defaultService
}

// Export 是统一的全局导出入口，自动派发到具体的 Concern 实现。
func Export(ctx context.Context, export any, w io.Writer, opts ...ExportOption) error {
	return DefaultService().Export(ctx, export, w, opts...)
}

// Import 是统一的全局导入入口，自动派发到具体的 Concern 实现。
func Import(ctx context.Context, importObj any, r io.Reader, opts ...ImportOption) error {
	return DefaultService().Import(ctx, importObj, r, opts...)
}

// Export 在当前服务实例上执行导出操作。
func (s *Service) Export(ctx context.Context, export any, w io.Writer, opts ...ExportOption) error {
	if export == nil {
		return oops.
			Code("excel_nil_pointer").
			In("excel.Export").
			Errorf("export object cannot be nil")
	}

	if w == nil {
		return oops.
			Code("excel_nil_pointer").
			In("excel.Export").
			Errorf("writer cannot be nil")
	}

	cfg := newDefaultExportConfig()
	for _, opt := range opts {
		opt(cfg)
	}

	if t, ok := export.(WithTitle); ok && t.Title() != "" {
		cfg.sheetName = t.Title()
	}

	return newExportPipeline(s, export, cfg).Write(ctx, w)
}

// Import 在当前服务实例上执行导入操作。
func (s *Service) Import(ctx context.Context, importObj any, r io.Reader, opts ...ImportOption) error {
	if importObj == nil {
		return oops.
			Code("excel_nil_pointer").
			In("excel.Import").
			Errorf("importObj cannot be nil")
	}

	if r == nil {
		return oops.
			Code("excel_nil_pointer").
			In("excel.Import").
			Errorf("reader cannot be nil")
	}

	cfg := newDefaultImportConfig()
	for _, opt := range opts {
		opt(cfg)
	}

	if t, ok := importObj.(WithTitle); ok && t.Title() != "" {
		cfg.sheetName = t.Title()
	}

	return newImportPipeline(s, importObj, cfg).Run(ctx, r)
}

func (s *Service) insertBatch(ctx context.Context, importObj any, batch []any) error {
	if len(batch) == 0 {
		return nil
	}

	if inserter, ok := importObj.(BatchInserter); ok {
		return inserter.InsertBatch(ctx, batch)
	}

	return errors.New("import object must implement BatchInserter to persist models to database")
}
