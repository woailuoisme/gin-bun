// Package authz 提供基于 Casbin 的静态权限矩阵。
package authz

import (
	"embed"
	"fmt"
	"io/fs"

	"github.com/casbin/casbin/v3"
	"github.com/casbin/casbin/v3/model"
	stringadapter "github.com/casbin/casbin/v3/persist/string-adapter"
)

// 常用资源与动作常量。
const (
	ResourceUsers = "users"

	ActionCreate = "create"
	ActionList   = "list"
	ActionRead   = "read"
	ActionUpdate = "update"
	ActionDelete = "delete"
)

//go:embed rbac/model.conf rbac/policy.csv
var rbacFS embed.FS

// Enforcer 判断角色对资源的操作权限。
type Enforcer struct {
	enforcer *casbin.Enforcer
}

// New 从内嵌策略文件构造 Enforcer。
func New() (*Enforcer, error) {
	modelText, err := fs.ReadFile(rbacFS, "rbac/model.conf")
	if err != nil {
		return nil, fmt.Errorf("authz: read model failed: %w", err)
	}
	policyText, err := fs.ReadFile(rbacFS, "rbac/policy.csv")
	if err != nil {
		return nil, fmt.Errorf("authz: read policy failed: %w", err)
	}

	parsedModel, err := model.NewModelFromString(string(modelText))
	if err != nil {
		return nil, fmt.Errorf("authz: parse model failed: %w", err)
	}

	enforcer, err := casbin.NewEnforcer(parsedModel, stringadapter.NewAdapter(string(policyText)))
	if err != nil {
		return nil, fmt.Errorf("authz: new enforcer failed: %w", err)
	}
	// 策略全部来自内嵌文件，运行期没有任何变更来源。
	enforcer.EnableAutoSave(false)
	return &Enforcer{enforcer: enforcer}, nil
}

// Can 判定角色是否被授予该权限。
func (e *Enforcer) Can(role, resource, action string) (bool, error) {
	allowed, err := e.enforcer.Enforce(role, resource, action)
	if err != nil {
		return false, fmt.Errorf("authz: enforce failed: %w", err)
	}
	return allowed, nil
}
