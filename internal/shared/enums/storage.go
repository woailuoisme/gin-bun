// Package enums 定义系统跨领域通用枚举与常量。
package enums

// StorageDisk 代表对象存储驱动与磁盘枚举类型。
type StorageDisk string

// 存储磁盘驱动常量定义。
const (
	// DiskDefault 默认存储磁盘。
	DiskDefault StorageDisk = "default"
	// DiskLocal 本地文件系统存储磁盘。
	DiskLocal StorageDisk = "local"
	// DiskS3 S3 兼容对象存储磁盘。
	DiskS3 StorageDisk = "s3"
)

// String 返回存储驱动字符串。
func (d StorageDisk) String() string {
	return string(d)
}

// MediaCollection 代表媒体文件所属集合枚举类型。
type MediaCollection string

// 媒体集合常量定义。
const (
	// CollectionAvatar 用户头像集合。
	CollectionAvatar MediaCollection = "avatar"
	// CollectionDocument 业务文档集合。
	CollectionDocument MediaCollection = "document"
	// CollectionAttachment 邮件或消息附件集合。
	CollectionAttachment MediaCollection = "attachment"
)

// String 返回媒体集合名称字符串。
func (c MediaCollection) String() string {
	return string(c)
}

// ModelType 代表媒体多态关联的主体模型类型枚举。
type ModelType string

// 媒体多态关联主体模型类型常量定义。
const (
	// ModelTypeUser 普通用户实体模型。
	ModelTypeUser ModelType = "User"
	// ModelTypeAdmin 管理员用户实体模型。
	ModelTypeAdmin ModelType = "Admin"
)

// String 返回模型类型名称字符串。
func (m ModelType) String() string {
	return string(m)
}
