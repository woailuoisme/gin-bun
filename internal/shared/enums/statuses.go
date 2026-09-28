// Package enums 定义系统跨领域通用枚举与常量。
package enums

import (
	"fmt"
	"slices"
)

// UserStatus 代表用户账号状态枚举。
type UserStatus string

// 用户状态枚举常量定义。
const (
	// UserStatusActive 正常活跃状态。
	UserStatusActive UserStatus = "active"
	// UserStatusInactive 未激活状态。
	UserStatusInactive UserStatus = "inactive"
	// UserStatusPending 等待审核状态。
	UserStatusPending UserStatus = "pending"
	// UserStatusSuspended 暂停使用状态。
	UserStatusSuspended UserStatus = "suspended"
	// UserStatusBanned 封禁冻结状态。
	UserStatusBanned UserStatus = "banned"
)

// Status 定义用户账号的生命周期状态枚举别名。
type Status = UserStatus

// 账号状态通用常量定义。
const (
	// StatusActive 正常活跃状态。
	StatusActive Status = UserStatusActive
	// StatusInactive 未激活状态。
	StatusInactive Status = UserStatusInactive
	// StatusPending 等待审核状态。
	StatusPending Status = UserStatusPending
	// StatusSuspended 暂停使用状态。
	StatusSuspended Status = UserStatusSuspended
	// StatusBanned 封禁冻结状态。
	StatusBanned Status = UserStatusBanned
)

// userStatusMap 存储用户状态元数据映射。
var userStatusMap = map[UserStatus]EnumInfo[UserStatus]{
	UserStatusActive:    {Value: UserStatusActive, Label: "Active", Color: ColorSuccess, Priority: 1},
	UserStatusInactive:  {Value: UserStatusInactive, Label: "Inactive", Color: ColorSecondary, Priority: 2},
	UserStatusPending:   {Value: UserStatusPending, Label: LabelPending, Color: ColorWarning, Priority: 3},
	UserStatusSuspended: {Value: UserStatusSuspended, Label: "Suspended", Color: ColorWarning, Priority: 4},
	UserStatusBanned:    {Value: UserStatusBanned, Label: "Banned", Color: ColorDanger, Priority: 5},
}

// userStatusLegacyMap 存储兼容历史别名的用户状态映射。
var userStatusLegacyMap = map[string]UserStatus{
	"Active":     UserStatusActive,
	"Inactive":   UserStatusInactive,
	LabelPending: UserStatusPending,
	"Suspended":  UserStatusSuspended,
	"Banned":     UserStatusBanned,
}

// AllUserStatuses 保存按默认优先级排序的所有用户状态只读切片。
var AllUserStatuses = []UserStatus{
	UserStatusActive,
	UserStatusInactive,
	UserStatusPending,
	UserStatusSuspended,
	UserStatusBanned,
}

// userStatusInfo 查找指定用户状态的元数据信息。
func userStatusInfo(status UserStatus) (EnumInfo[UserStatus], bool) {
	info, exists := userStatusMap[status]
	return info, exists
}

// String 返回状态的字符串表现形式。
func (s UserStatus) String() string {
	if info, exists := userStatusInfo(s); exists {
		return info.Label
	}

	return string(s)
}

// Label 获取状态的可读文本标签。
func (s UserStatus) Label() string { return s.String() }

// Color 获取状态对应的徽标颜色。
func (s UserStatus) Color() string {
	if info, exists := userStatusInfo(s); exists {
		return info.Color
	}

	return ColorGray
}

// Priority 获取状态排序优先级。
func (s UserStatus) Priority() int {
	if info, exists := userStatusInfo(s); exists {
		return info.Priority
	}

	return 999
}

// IsValid 检查状态是否在预设合法状态集中。
func (s UserStatus) IsValid() bool { _, exists := userStatusInfo(s); return exists }

// IsActive 判断用户是否处于正常活跃状态。
func (s UserStatus) IsActive() bool { return s == UserStatusActive }

// IsBlocked 判断用户是否已被冻结或封禁。
func (s UserStatus) IsBlocked() bool { return s == UserStatusSuspended || s == UserStatusBanned }

// CanLogin 判断用户是否允许登录认证。
func (s UserStatus) CanLogin() bool { return s == UserStatusActive || s == UserStatusPending }

// IsBanned 判定账号是否已被封禁冻结。
func (s UserStatus) IsBanned() bool { return s == UserStatusBanned }

// AllStatuses 返回常用候选账号状态切片。
func AllStatuses() []Status {
	return []Status{StatusActive, StatusInactive, StatusBanned}
}

// UserStatusFromString 解析字符串为用户状态枚举。
func UserStatusFromString(value string) (UserStatus, error) {
	status := UserStatus(value)
	if status.IsValid() {
		return status, nil
	}

	if status, exists := FindByLabel(value, userStatusMap); exists {
		return status, nil
	}

	if status, exists := FindByLegacyValue(value, userStatusLegacyMap); exists {
		return status, nil
	}

	return "", fmt.Errorf("invalid user status: %s", value)
}

// UserStatusMustFromString 解析字符串为用户状态枚举，失败时触发恐慌。
func UserStatusMustFromString(value string) UserStatus {
	status, err := UserStatusFromString(value)
	if err != nil {
		panic(err)
	}

	return status
}

// UserStatusValues 获取全部用户状态枚举值列表。
func UserStatusValues() []UserStatus { return slices.Clone(AllUserStatuses) }

// UserStatusSortedValues 获取按优先级排序后的用户状态枚举值列表。
func UserStatusSortedValues() []UserStatus { return slices.Clone(AllUserStatuses) }

// UserStatusGetOptions 获取前端选项键值对映射。
func UserStatusGetOptions() map[string]string { return CreateOptionsMap(userStatusMap) }

// UserStatusGetOptionsWithColor 获取带颜色配置的前端选项映射。
func UserStatusGetOptionsWithColor() map[string]map[string]string {
	return CreateOptionsWithColorMap(userStatusMap)
}

// UserStatusGetAllInfo 获取全部用户状态元数据列表。
func UserStatusGetAllInfo() []EnumInfo[UserStatus] {
	infos := make([]EnumInfo[UserStatus], len(AllUserStatuses))
	for i, s := range AllUserStatuses {
		infos[i] = userStatusMap[s]
	}
	return infos
}

// UserStatusGetSortedInfo 获取按优先级排序的用户状态元数据列表。
func UserStatusGetSortedInfo() []EnumInfo[UserStatus] {
	return UserStatusGetAllInfo()
}
