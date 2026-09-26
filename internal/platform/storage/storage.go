// Package storage 提供对象存储的统一入口：一个 Disk 抽象与两个驱动——本地文件系统与
// S3 兼容对象存储。
//
// 设计上刻意保持薄：不做多磁盘路由、不引入依赖注入框架，也没有内存与假实现——测试需要
// 替身时用 local 驱动指向 t.TempDir() 即可。驱动由配置单选（config.StorageConfig.Driver），
// 不会出现"配了 s3 却因为本地目录存在而静默改走本地"这类歧义。
//
// S3 客户端只用 github.com/minio/minio-go/v7：同一份实现可对接 AWS S3、MinIO、Garage、
// Cloudflare R2 与阿里云 OSS，无需再引入 aws-sdk-go。
package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"path"
	"strings"
	"time"

	"bgin/internal/config"
	"bgin/internal/platform/errs"
)

// msgFileNotFound 是对象不存在时对外暴露的固定文案。对象键不进响应体，只留在因由里进日志。
const msgFileNotFound = "The requested file does not exist"

// 驱动能力的边界用哨兵错误表达，便于调用方 errors.Is 判断，而不是靠匹配字符串。
var (
	// ErrNoPublicURL 表示没有配置公开访问地址，因此给不出永久链接。
	// 这是配置缺口而非运行故障：调用方应改用 TemporaryURL（仅 s3 支持），
	// 或由部署方在配置里补上公开地址前缀。
	ErrNoPublicURL = errors.New("storage: no public URL configured")

	// ErrPresignUnsupported 表示该驱动没有预签名机制，目前只有 local 会返回它。
	//
	// 本地文件没有服务端签名可言：与其返回一个带 expires 参数、看似受控实则谁都能访问的
	// 链接，不如明确拒绝，逼调用方改用 s3 或自行实现路由与访问校验。
	ErrPresignUnsupported = errors.New("storage: driver does not support presigned URLs")
)

// Disk 是对外暴露的存储契约。
//
// path 是相对键（例如 "users/42/avatar.webp"），两个驱动按同一条键定位对象；前导斜杠会被
// 规整掉。除 URL 外的每个方法都会尊重 ctx 的取消与超时。
//
// 对象不存在时统一返回 NOT_FOUND；其余底层失败收敛为 INTERNAL_SERVER_ERROR，原始原因只进
// 日志。用 errs.As 可从错误链里取出错误码。
type Disk interface {
	// Put 覆盖写入整个对象。
	Put(ctx context.Context, path string, contents []byte) error
	// PutStream 从流写入对象；读取长度未知时由驱动自行决定分片策略。
	PutStream(ctx context.Context, path string, r io.Reader) error
	// PutMultipart 从 HTTP multipart 表单头写入对象，自动管理流生命周期与资源释放。
	PutMultipart(ctx context.Context, path string, file *multipart.FileHeader) error
	// Get 读出对象的全部字节。
	Get(ctx context.Context, path string) ([]byte, error)
	// GetStream 返回对象的只读流，由调用方负责关闭。
	GetStream(ctx context.Context, path string) (io.ReadCloser, error)
	// Delete 删除对象；对象本就不存在时视为成功。
	Delete(ctx context.Context, path string) error
	// Exists 报告对象是否存在。
	Exists(ctx context.Context, path string) (bool, error)
	// Size 返回对象的字节数。
	Size(ctx context.Context, path string) (int64, error)
	// LastModified 返回对象的最后修改时间。
	LastModified(ctx context.Context, path string) (time.Time, error)
	// MimeType 返回对象的内容类型，先按扩展名推断，再回退到内容嗅探。
	MimeType(ctx context.Context, path string) (string, error)
	// URL 返回对象的公开访问地址；未配置公开地址时返回 ErrNoPublicURL。
	URL(path string) (string, error)
	// TemporaryURL 返回带签名与时效的下载地址；驱动不支持时返回 ErrPresignUnsupported。
	TemporaryURL(ctx context.Context, path string, ttl time.Duration) (string, error)
	// PresignPut 返回带签名与时效的上传地址；驱动不支持时返回 ErrPresignUnsupported。
	PresignPut(ctx context.Context, path string, ttl time.Duration) (string, error)
	// HealthCheck 探测存储是否可用，供启动期与就绪探针使用。
	HealthCheck(ctx context.Context) error
}

// Open 按配置构造驱动并立即探测一次可用性：存储不可用时启动就失败，
// 而不是等到第一次真正读写才发现（与 database.Open、cache.Open 的处理一致）。
func Open(ctx context.Context, cfg config.StorageConfig) (Disk, error) {
	var (
		disk Disk
		err  error
	)

	switch cfg.Driver {
	case config.StorageDriverLocal:
		disk, err = NewLocalDisk(cfg.Local.Root, cfg.Local.URL)
	case config.StorageDriverS3:
		disk, err = NewS3Disk(cfg.S3)
	default:
		return nil, fmt.Errorf("unknown storage driver %q, options: %s / %s",
			cfg.Driver, config.StorageDriverLocal, config.StorageDriverS3)
	}
	if err != nil {
		return nil, err
	}

	if err := disk.HealthCheck(ctx); err != nil {
		return nil, fmt.Errorf("storage driver %s is unavailable: %w", cfg.Driver, err)
	}
	return disk, nil
}

// keyOf 把对象键规整成两个驱动共用的形态：以 "/" 开头、去掉前导斜杠、消解 "." 与 ".."。
// 它只做规范化，不承担越界判定——本地驱动的越界由 resolvePath 用相对路径校验拦截。
func keyOf(key string) string {
	return strings.TrimPrefix(path.Clean("/"+key), "/")
}

// notFound 构造对象不存在的 404：对外文案固定，对象键只进日志。
func notFound(path string) error {
	return errs.Wrap(errs.CodeNotFound, msgFileNotFound, fmt.Errorf("storage object %s does not exist", path))
}

// opFailed 把底层失败连同操作名与对象键收敛为 500。对外文案固定为通用提示，
// 操作名与键只进日志（errs.Internal 的语义）。
func opFailed(op, path string, err error) error {
	return errs.Internal(fmt.Errorf("%s %q failed: %w", op, path, err))
}
