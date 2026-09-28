// Package enums 定义系统跨领域通用枚举与常量。
package enums

import (
	"fmt"
	"slices"
	"strings"
)

// AdminRole 代表后台管理系统的授权角色枚举。
type AdminRole string

// 管理员角色常量定义。
const (
	// SuperAdmin 超级管理员。
	SuperAdmin AdminRole = "super_admin"
	// Admin 普通管理员。
	Admin AdminRole = "admin"
	// Editor 运营编辑。
	Editor AdminRole = "editor"
	// Agent 代理人员。
	Agent AdminRole = "agent"
	// Operator 操作员。
	Operator AdminRole = "operator"
	// Ops 运维人员。
	Ops AdminRole = "ops"
	// RepWorker 客服人员。
	RepWorker AdminRole = "rep_worker"
)

// SuperAdminAlias 表示兼容历史拼写（无下划线）的超级管理员角色输入别名。
// 该值会在 AdminRoleFromString 中归一化为 SuperAdmin，不参与枚举集合与元数据映射。
const SuperAdminAlias AdminRole = "superadmin"

// IsAdminGuardRole 判断给定角色值是否具备进入管理端认证守卫的资格。
// 入参允许大小写与首尾空白差异，会先归一化再与合法管理端角色比对。
func IsAdminGuardRole(role string) bool {
	switch AdminRole(strings.ToLower(strings.TrimSpace(role))) {
	case SuperAdmin, SuperAdminAlias, Admin:
		return true
	default:
		return false
	}
}

// Role 定义用户系统的权限角色枚举。
type Role string

// 普通用户与管理员角色常量定义。
const (
	// RoleUser 普通注册用户。
	RoleUser Role = "user"
	// RoleAdmin 系统管理员。
	RoleAdmin Role = "admin"
)

// UserRole 代表普通用户授权角色枚举别名，保持向前兼容。
type UserRole = Role

// String 返回角色的字符串表现形式。
func (r Role) String() string {
	return string(r)
}

// IsValid 校验角色取值是否在合法白名单内。
func (r Role) IsValid() bool {
	return r == RoleUser || r == RoleAdmin
}

// IsAdmin 判定当前角色是否为系统管理员。
func (r Role) IsAdmin() bool {
	return r == RoleAdmin
}

// AllRoles 返回全部候选角色切片。
func AllRoles() []Role {
	return []Role{RoleUser, RoleAdmin}
}

// adminRoleMap 存储管理员角色元数据映射。
var adminRoleMap = map[AdminRole]EnumInfo[AdminRole]{
	SuperAdmin: {Value: SuperAdmin, Label: "Super Admin", Color: ColorDanger, Priority: 1},
	Admin:      {Value: Admin, Label: "Admin", Color: ColorWarning, Priority: 2},
	Editor:     {Value: Editor, Label: "Editor", Color: ColorInfo, Priority: 3},
	Agent:      {Value: Agent, Label: "Agent", Color: ColorPrimary, Priority: 4},
	Operator:   {Value: Operator, Label: "Operator", Color: ColorSuccess, Priority: 5},
	Ops:        {Value: Ops, Label: "Ops", Color: ColorPurple, Priority: 6},
	RepWorker:  {Value: RepWorker, Label: "Customer Service", Color: ColorOrange, Priority: 7},
}

// legacyRoleMap 存储兼容历史别名的角色映射。
var legacyRoleMap = map[string]AdminRole{
	"Super Admin":           SuperAdmin,
	"Admin":                 Admin,
	"Editor":                Editor,
	"Agent":                 Agent,
	"Ops":                   Ops,
	"Operator":              Operator,
	"Customer Service":      RepWorker,
	string(SuperAdminAlias): SuperAdmin,
}

// AllAdminRoles 保存按优先级排序的所有管理员角色只读切片。
var AllAdminRoles = []AdminRole{
	SuperAdmin,
	Admin,
	Editor,
	Agent,
	Operator,
	Ops,
	RepWorker,
}

// adminRoleInfo 查找指定管理员角色的元数据信息。
func adminRoleInfo(role AdminRole) (EnumInfo[AdminRole], bool) {
	info, exists := adminRoleMap[role]
	return info, exists
}

// String 返回角色的字符串表现形式。
func (r AdminRole) String() string {
	if info, exists := adminRoleInfo(r); exists {
		return info.Label
	}

	return string(r)
}

// Label 获取角色的可读文本标签。
func (r AdminRole) Label() string { return r.String() }

// Color 获取角色的显示徽标颜色。
func (r AdminRole) Color() string {
	if info, exists := adminRoleInfo(r); exists {
		return info.Color
	}

	return ColorGray
}

// Priority 获取角色的排序优先级。
func (r AdminRole) Priority() int {
	if info, exists := adminRoleInfo(r); exists {
		return info.Priority
	}

	return 999
}

// IsValid 检查角色是否在合法预设角色集合中。
func (r AdminRole) IsValid() bool { _, exists := adminRoleInfo(r); return exists }

// AdminRoleFromString 解析字符串为管理员角色枚举。
func AdminRoleFromString(value string) (AdminRole, error) {
	role := AdminRole(strings.ToLower(value))
	if role.IsValid() {
		return role, nil
	}

	if role, exists := FindByLabel(value, adminRoleMap); exists {
		return role, nil
	}

	if role, exists := FindByLegacyValue(value, legacyRoleMap); exists {
		return role, nil
	}

	return "", fmt.Errorf("invalid admin role: %s", value)
}

// AdminRoleMustFromString 解析字符串为角色枚举，失败时触发恐慌。
func AdminRoleMustFromString(value string) AdminRole {
	role, err := AdminRoleFromString(value)
	if err != nil {
		panic(err)
	}

	return role
}

// AdminRoleValues 返回所有管理员角色切片。
func AdminRoleValues() []AdminRole { return slices.Clone(AllAdminRoles) }

// AdminRoleSortedValues 返回按优先级排序后的管理员角色切片。
func AdminRoleSortedValues() []AdminRole { return slices.Clone(AllAdminRoles) }

// AdminRoleGetOptions 获取选项键值对映射。
func AdminRoleGetOptions() map[string]string { return CreateOptionsMap(adminRoleMap) }

// AdminRoleGetOptionsWithColor 获取带颜色配置的选项键值对映射。
func AdminRoleGetOptionsWithColor() map[string]map[string]string {
	return CreateOptionsWithColorMap(adminRoleMap)
}

// AdminRoleGetAllInfo 获取全部角色元数据列表。
func AdminRoleGetAllInfo() []EnumInfo[AdminRole] {
	infos := make([]EnumInfo[AdminRole], len(AllAdminRoles))
	for i, r := range AllAdminRoles {
		infos[i] = adminRoleMap[r]
	}
	return infos
}

// AdminRoleGetSortedInfo 获取按优先级排序的角色元数据列表。
func AdminRoleGetSortedInfo() []EnumInfo[AdminRole] {
	return AdminRoleGetAllInfo()
}

// HasPermission 判定角色是否具备特定操作权限。
func (r AdminRole) HasPermission(permission string) bool {
	switch r {
	case SuperAdmin:
		return true
	case Admin:
		return permission != "system_config"
	case Editor:
		return permission == "content_edit" || permission == "content_view"
	case Operator:
		return permission == "inventory_manage" || permission == "inventory_view"
	case Agent:
		return permission == "agent_manage" || permission == "agent_view"
	case Ops:
		return permission == "system_ops" || permission == "system_monitor"
	case RepWorker:
		return permission == "customer_service" || permission == "customer_view"
	default:
		return false
	}
}

// IsHigherThan 判断当前角色优先级是否高于目标角色。
func (r AdminRole) IsHigherThan(other AdminRole) bool { return r.Priority() < other.Priority() }

// IsLowerThan 判断当前角色优先级是否低于目标角色。
func (r AdminRole) IsLowerThan(other AdminRole) bool { return r.Priority() > other.Priority() }

// IsEqualOrHigherThan 判断当前角色优先级是否等于或高于目标角色。
func (r AdminRole) IsEqualOrHigherThan(other AdminRole) bool { return r.Priority() <= other.Priority() }

// userRolePriority 表示普通用户角色的层级权重，恒低于全部管理端角色。
const userRolePriority = 1000

// RolePriority 返回角色的层级权重，数值越小权限越高；普通用户角色权重最低。
// 第二个返回值为 false 表示角色值不在合法集合内。
func RolePriority(role string) (int, bool) {
	if strings.EqualFold(strings.TrimSpace(role), string(RoleUser)) {
		return userRolePriority, true
	}

	r := AdminRole(strings.ToLower(strings.TrimSpace(role)))
	if !r.IsValid() {
		return 0, false
	}

	return r.Priority(), true
}

// CanManageRole 判断操作者角色是否有权管理目标角色。
// 要求操作者层级严格高于目标角色，从而阻止同级或下级越权管理、以及授予高于自身的角色。
func CanManageRole(actorRole, targetRole string) bool {
	actorPriority, actorOK := RolePriority(actorRole)
	targetPriority, targetOK := RolePriority(targetRole)
	if !actorOK || !targetOK {
		return false
	}

	return actorPriority < targetPriority
}
