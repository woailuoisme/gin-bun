// Package authz 是基于 Casbin 的静态权限矩阵。
// 策略随二进制发布、运行期不可修改，且不写 g 规则——角色归属以 user.role 为权威（见 ADR-0003）。
package authz

import (
	"embed"
	"fmt"
	"io/fs"

	"github.com/casbin/casbin/v3"
	"github.com/casbin/casbin/v3/model"
	stringadapter "github.com/casbin/casbin/v3/persist/string-adapter"
)

// 权限码使用的资源名与动作名，供中间件与策略文件共用。
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

// Enforcer 判断某个角色能否对某个资源执行某个动作。
type Enforcer struct {
	enforcer *casbin.Enforcer
}

// New 从内嵌的策略文件构造 Enforcer。资源已编译进二进制，装载失败属于打包错误。
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
