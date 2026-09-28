// Package media 提供媒体文件上传、验证、处理、命名策略、Imgproxy 防篡改签名与多态实体附件管理。
package media

import (
	"cmp"
	"context"
	"database/sql"
	"errors"
	"mime/multipart"
	"strings"

	"github.com/samber/oops"
	"github.com/uptrace/bun"

	"bgin/internal/config"
	"bgin/internal/models"
	"bgin/internal/platform/errs"
	"bgin/internal/platform/logger"
	"bgin/internal/platform/storage"
)

// ErrMediaNotFound 表示未查询到目标媒体记录的哨兵错误。
var ErrMediaNotFound = errors.New("media: record not found")

// Attachable 描述可绑定多态媒体附件的业务模型契约。
type Attachable interface {
	GetModelType() string
	GetModelID() string
}

// GenericAttachable 简单通用的 Attachable 实现。
type GenericAttachable struct {
	ModelType string
	ModelID   string
}

// GetModelType 返回业务模型类型标识。
func (g GenericAttachable) GetModelType() string { return g.ModelType }

// GetModelID 返回业务模型主键 ID。
func (g GenericAttachable) GetModelID() string { return g.ModelID }

// Model 快捷创建通用多态载体对象。
func Model(modelType, modelID string) Attachable {
	return GenericAttachable{
		ModelType: strings.TrimSpace(modelType),
		ModelID:   strings.TrimSpace(modelID),
	}
}

// ToAttachable 智能解析输入对象为 Attachable 接口实例。
func ToAttachable(v any) Attachable {
	if v == nil {
		return nil
	}
	if a, ok := v.(Attachable); ok {
		return a
	}
	return nil
}

// AttachConfig 保存媒体附加的选项参数。
type AttachConfig struct {
	Name             string         // 原始友好名称（若为空则默认取上传文件名）
	SingleFile       bool           // 是否为单文件集合（若为 true，自动清空旧同名集合下的物理文件与数据库记录）
	CustomProperties map[string]any // 自定义扩展业务属性（JSONB）
	Manipulations    map[string]any // 图片处理指令与参数元数据（JSONB）
	OrderColumn      *int           // 多附件排序号
	DiskName         string         // 显式指定的存储磁盘名称
}

// AttachOption 定义附加选项函数。
type AttachOption func(*AttachConfig)

// WithName 设置媒体附件的展示名称。
func WithName(name string) AttachOption {
	return func(c *AttachConfig) {
		c.Name = strings.TrimSpace(name)
	}
}

// WithSingleFile 设置该附件集合为单文件模式（自动覆盖已有旧附件）。
func WithSingleFile() AttachOption {
	return func(c *AttachConfig) {
		c.SingleFile = true
	}
}

// WithCustomProperties 设置自定义业务元数据。
func WithCustomProperties(props map[string]any) AttachOption {
	return func(c *AttachConfig) {
		c.CustomProperties = props
	}
}

// WithManipulations 设置图片衍生与操作指令。
func WithManipulations(manipulations map[string]any) AttachOption {
	return func(c *AttachConfig) {
		c.Manipulations = manipulations
	}
}

// WithOrderColumn 设置多附件排序号。
func WithOrderColumn(order int) AttachOption {
	return func(c *AttachConfig) {
		c.OrderColumn = &order
	}
}

// Manager 为业务层提供多态媒体附件聚合管理门面。
type Manager struct {
	db       *bun.DB
	disk     storage.Disk
	pipeline Pipeline
	image    Client
	cfg      config.MediaConfig
}

// NewManager 构建多态媒体管理门面实例。
func NewManager(
	mediaCfg config.MediaConfig,
	disk storage.Disk,
	db *bun.DB,
	pipeline Pipeline,
	image Client,
) *Manager {
	return &Manager{
		db:       db,
		disk:     disk,
		pipeline: pipeline,
		image:    image,
		cfg:      mediaCfg,
	}
}

// Pipeline 返回底层的无状态媒体处理与上传管道。
func (m *Manager) Pipeline() Pipeline {
	return m.pipeline
}

// Image 返回底层 Imgproxy 图像处理器。
func (m *Manager) Image() Client {
	return m.image
}

// Disk 返回底层的存储磁盘驱动句柄。
func (m *Manager) Disk() storage.Disk {
	return m.disk
}

// DB 返回底层数据库连接池。
func (m *Manager) DB() *bun.DB {
	return m.db
}

// Attach 将文件或流上传并关联至指定业务模型的多态集合中。
func (m *Manager) Attach(
	ctx context.Context,
	target any,
	collection string,
	source any,
	opts ...AttachOption,
) (*models.Media, error) {
	attachable := ToAttachable(target)
	if attachable == nil || strings.TrimSpace(attachable.GetModelID()) == "" {
		return nil, oops.Code(errs.CodeBadRequest).Errorf("media: invalid attachable target or empty model id")
	}

	modelType := strings.TrimSpace(attachable.GetModelType())
	modelID := strings.TrimSpace(attachable.GetModelID())
	collection = cmp.Or(strings.TrimSpace(collection), m.cfg.DefaultCollection, "default")

	var conf AttachConfig
	for _, opt := range opts {
		if opt != nil {
			opt(&conf)
		}
	}

	// 1. 上传文件到底层存储并获取元数据
	fileInfo, err := m.uploadSource(ctx, source, collection, conf.DiskName)
	if err != nil {
		return nil, err
	}

	// 2. 单文件模式下先清理旧的物理文件与记录；失败不阻断新文件绑定
	if conf.SingleFile && m.db != nil {
		if err := m.clearOldFiles(ctx, modelType, modelID, collection); err != nil {
			logger.FromContext(ctx).Debug().
				Err(err).
				Str("model_type", modelType).
				Str("model_id", modelID).
				Msg("failed to clear old media files, proceeding with new upload")
		}
	}

	// 3. 将新媒体记录持久化至数据库
	friendlyName := cmp.Or(conf.Name, fileInfo.OriginalName, fileInfo.FileName)
	mediaRecord := &models.Media{
		ModelType:        modelType,
		ModelID:          modelID,
		CollectionName:   collection,
		Name:             friendlyName,
		FileName:         fileInfo.Path,
		MimeType:         fileInfo.MimeType,
		Disk:             cmp.Or(conf.DiskName, "default"),
		Size:             fileInfo.Size,
		ConversionStatus: "completed",
		OrderColumn:      conf.OrderColumn,
		CustomProperties: conf.CustomProperties,
		Manipulations:    conf.Manipulations,
	}

	if m.db != nil {
		_, err = m.db.NewInsert().Model(mediaRecord).Exec(ctx)
		if err != nil {
			// 入库失败时尝试回滚删除刚上传的物理文件
			if delErr := m.pipeline.Delete(ctx, fileInfo.Path); delErr != nil {
				logger.FromContext(ctx).Warn().
					Err(delErr).
					Str("path", fileInfo.Path).
					Msg("failed to clean up orphaned media file after DB insert failure")
			}
			return nil, oops.Code(errs.CodeInternal).In("media.Manager.Attach").Wrap(err)
		}
	}

	return mediaRecord, nil
}

// uploadSource 内部按源类型调度底层管道上传。
func (m *Manager) uploadSource(ctx context.Context, source any, category, diskName string) (*FileInfo, error) {
	switch s := source.(type) {
	case *multipart.FileHeader:
		return m.pipeline.Upload(ctx, s, category, diskName)
	case []byte:
		return m.pipeline.UploadBytes(ctx, s, "upload.bin", category, diskName)
	case *FileInfo:
		return s, nil
	case FileInfo:
		return &s, nil
	default:
		return nil, oops.Code(errs.CodeBadRequest).Errorf("media: unsupported upload source type: %T", source)
	}
}

// clearOldFiles 物理删除并移出指定模型集合下的所有旧附件。
func (m *Manager) clearOldFiles(ctx context.Context, modelType, modelID, collection string) error {
	var oldMedia []models.Media
	err := m.db.NewSelect().
		Model(&oldMedia).
		Where("model_type = ? AND model_id = ? AND collection_name = ?", modelType, modelID, collection).
		Scan(ctx)
	if err != nil {
		return err
	}

	for _, item := range oldMedia {
		_ = m.disk.Delete(ctx, item.FileName)
	}

	_, err = m.db.NewDelete().
		Model((*models.Media)(nil)).
		Where("model_type = ? AND model_id = ? AND collection_name = ?", modelType, modelID, collection).
		Exec(ctx)
	return err
}

// Get 查询指定业务模型下某个集合（或全部集合）的所有媒体附件。
func (m *Manager) Get(ctx context.Context, target any, collection ...string) ([]models.Media, error) {
	attachable := ToAttachable(target)
	if attachable == nil {
		return nil, oops.Code(errs.CodeBadRequest).Errorf("media: invalid attachable target")
	}

	if m.db == nil {
		return nil, oops.Code("database_unavailable").Errorf("media: database connection is not available")
	}

	q := m.db.NewSelect().
		Model((*models.Media)(nil)).
		Where("model_type = ? AND model_id = ?", attachable.GetModelType(), attachable.GetModelID())

	if len(collection) > 0 && strings.TrimSpace(collection[0]) != "" {
		q = q.Where("collection_name = ?", strings.TrimSpace(collection[0]))
	}

	var items []models.Media
	err := q.Order("order_column ASC", "id ASC").Scan(ctx, &items)
	if err != nil {
		return nil, oops.Code(errs.CodeInternal).In("media.Manager.Get").Wrap(err)
	}

	return items, nil
}

// GetFirst 查询指定业务模型特定集合下的首个媒体附件（如头像）。
func (m *Manager) GetFirst(ctx context.Context, target any, collection string) (*models.Media, error) {
	items, err := m.Get(ctx, target, collection)
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, oops.Code(errs.CodeNotFound).Wrap(ErrMediaNotFound)
	}
	return &items[0], nil
}

// FindByUUID 根据对外暴露的安全唯一 UUID 查询媒体附件记录。
func (m *Manager) FindByUUID(ctx context.Context, uuid string) (*models.Media, error) {
	if strings.TrimSpace(uuid) == "" {
		return nil, oops.Code(errs.CodeBadRequest).Errorf("media: uuid cannot be empty")
	}

	if m.db == nil {
		return nil, oops.Code("database_unavailable").Errorf("media: database connection is not available")
	}

	var item models.Media
	err := m.db.NewSelect().
		Model(&item).
		Where("uuid = ?", strings.TrimSpace(uuid)).
		Scan(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, oops.Code(errs.CodeNotFound).Wrap(ErrMediaNotFound)
		}
		return nil, oops.Code(errs.CodeNotFound).In("media.Manager.FindByUUID").Wrap(err)
	}

	return &item, nil
}

// FindByID 根据自增物理主键 ID 查询媒体附件记录。
func (m *Manager) FindByID(ctx context.Context, id int64) (*models.Media, error) {
	if m.db == nil {
		return nil, oops.Code("database_unavailable").Errorf("media: database connection is not available")
	}

	var item models.Media
	err := m.db.NewSelect().
		Model(&item).
		Where("id = ?", id).
		Scan(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, oops.Code(errs.CodeNotFound).Wrap(ErrMediaNotFound)
		}
		return nil, oops.Code(errs.CodeNotFound).In("media.Manager.FindByID").Wrap(err)
	}

	return &item, nil
}

// Delete 删除指定 UUID 的媒体附件，同时删除底层物理文件与数据库记录。
func (m *Manager) Delete(ctx context.Context, uuid string) error {
	mediaItem, err := m.FindByUUID(ctx, uuid)
	if err != nil {
		return err
	}

	// 1. 删除底层存储文件
	_ = m.disk.Delete(ctx, mediaItem.FileName)

	// 2. 删除数据库记录
	_, err = m.db.NewDelete().
		Model((*models.Media)(nil)).
		Where("uuid = ?", uuid).
		Exec(ctx)
	if err != nil {
		return oops.Code(errs.CodeInternal).In("media.Manager.Delete").Wrap(err)
	}

	return nil
}

// ClearCollection 清空指定业务模型特定集合下的所有媒体附件（物理文件与数据库记录全部清除）。
func (m *Manager) ClearCollection(ctx context.Context, target any, collection string) error {
	attachable := ToAttachable(target)
	if attachable == nil {
		return oops.Code(errs.CodeBadRequest).Errorf("media: invalid attachable target")
	}
	if m.db == nil {
		return oops.Code("database_unavailable").Errorf("media: database connection is not available")
	}

	return m.clearOldFiles(ctx, attachable.GetModelType(), attachable.GetModelID(), strings.TrimSpace(collection))
}

// URL 智能生成媒体文件的访问 URL。
// 如果指定了图像处理选项且配置了 Imgproxy，则生成 Imgproxy 防篡改签名 URL；否则返回 Storage Disk 原始/预签名 URL。
func (m *Manager) URL(mediaItem *models.Media, opts ...ProcessOption) string {
	if mediaItem == nil || mediaItem.FileName == "" {
		return ""
	}

	isImage := strings.HasPrefix(mediaItem.MimeType, "image/") && mediaItem.MimeType != "image/svg+xml"
	if len(opts) > 0 && isImage && m.image != nil {
		imgURL, err := m.image.URL(mediaItem.FileName, opts...)
		if err == nil && imgURL != "" {
			return imgURL
		}
	}

	url, _ := m.disk.URL(mediaItem.FileName)
	return url
}

// Thumbnail 快捷获取多态媒体文件的缩略图 URL。
func (m *Manager) Thumbnail(mediaItem *models.Media, width, height int) string {
	return m.URL(mediaItem,
		WithResizingType(ResizeFill),
		WithResize(width, height),
	)
}

// Avatar 快捷获取多态媒体文件的头像裁切 URL。
func (m *Manager) Avatar(mediaItem *models.Media, size int) string {
	return m.URL(mediaItem,
		WithResizingType(ResizeFill),
		WithResize(size, size),
		WithGravity(GravityFocusPoint),
	)
}
