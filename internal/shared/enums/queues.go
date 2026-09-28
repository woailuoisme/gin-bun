// Package enums 定义系统跨领域通用枚举与常量。
package enums

// QueueName 代表 Asynq 队列名称枚举类型。
type QueueName string

// 队列名称常量定义。
const (
	// QueueCritical 高优先级任务队列。
	QueueCritical QueueName = "critical"
	// QueueDefault 默认优先级任务队列。
	QueueDefault QueueName = "default"
	// QueueLow 低优先级/批量任务队列。
	QueueLow QueueName = "low"
)

// String 返回队列名称字符串。
func (q QueueName) String() string {
	return string(q)
}

// IsValid 检查队列名是否合法。
func (q QueueName) IsValid() bool {
	return q == QueueCritical || q == QueueDefault || q == QueueLow
}

// JobName 代表异步任务名称枚举类型。
type JobName string

// 任务名称常量定义。
const (
	// JobSendWelcomeEmail 发送用户欢迎邮件任务。
	JobSendWelcomeEmail JobName = "send_welcome_email"
	// JobReminderNotification 发送提醒通知任务。
	JobReminderNotification JobName = "reminder_notification"
	// JobUserImport 批量导入用户数据任务。
	JobUserImport JobName = "user_import"
	// JobUserExport 批量导出用户数据任务。
	JobUserExport JobName = "user_export"
	// JobCleanupTempFiles 清理临时文件任务。
	JobCleanupTempFiles JobName = "cleanup_temp_files"
	// JobSyncSearchIndex 同步搜索索引任务。
	JobSyncSearchIndex JobName = "search:sync_index"
	// JobDeleteSearchIndex 删除搜索索引任务。
	JobDeleteSearchIndex JobName = "search:delete_index"
	// JobRealtimeBroadcast 实时消息异步广播任务。
	JobRealtimeBroadcast JobName = "realtime:broadcast"
	// JobMqttPublish MQTT 消息异步削峰发布任务。
	JobMqttPublish JobName = "mqtt:publish"
)

// String 返回任务名称字符串。
func (j JobName) String() string {
	return string(j)
}
