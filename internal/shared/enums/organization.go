// Package enums 定义系统跨领域通用枚举与常量。
package enums

// OrganizationRole 代表组织成员角色枚举。
type OrganizationRole string

// 组织成员角色枚举常量定义。
const (
	// OrganizationRoleOwner 组织拥有者。
	OrganizationRoleOwner OrganizationRole = "owner"
	// OrganizationRoleAdmin 组织管理员。
	OrganizationRoleAdmin OrganizationRole = "admin"
	// OrganizationRoleMember 普通成员。
	OrganizationRoleMember OrganizationRole = "member"
)

// String 返回组织角色的字符串表现形式。
func (r OrganizationRole) String() string {
	return string(r)
}

// IsValid 检查组织角色是否合法。
func (r OrganizationRole) IsValid() bool {
	switch r {
	case OrganizationRoleOwner, OrganizationRoleAdmin, OrganizationRoleMember:
		return true
	default:
		return false
	}
}

// IsPrivileged 判断该角色是否具备管理权限（Owner 或 Admin）。
func (r OrganizationRole) IsPrivileged() bool {
	return r == OrganizationRoleOwner || r == OrganizationRoleAdmin
}

// InvitationStatus 代表组织邀请状态枚举。
type InvitationStatus string

// 组织邀请状态枚举常量定义。
const (
	// InvitationStatusPending 等待受邀者处理。
	InvitationStatusPending InvitationStatus = "pending"
	// InvitationStatusAccepted 邀请已被接受。
	InvitationStatusAccepted InvitationStatus = "accepted"
	// InvitationStatusRejected 邀请已被拒绝。
	InvitationStatusRejected InvitationStatus = "rejected"
	// InvitationStatusCanceled 邀请已被取消。
	InvitationStatusCanceled InvitationStatus = "canceled"
)

// String 返回邀请状态的字符串表现形式。
func (s InvitationStatus) String() string {
	return string(s)
}

// IsValid 检查邀请状态是否合法。
func (s InvitationStatus) IsValid() bool {
	switch s {
	case InvitationStatusPending, InvitationStatusAccepted, InvitationStatusRejected, InvitationStatusCanceled:
		return true
	default:
		return false
	}
}

// CanRespond 检查邀请是否仍处于可接受或拒绝的待决状态。
func (s InvitationStatus) CanRespond() bool {
	return s == InvitationStatusPending
}
