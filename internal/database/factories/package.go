package factories

import (
	"github.com/uptrace/bun"
)

// User 快捷构造 UserFactory 实例。
func User(db bun.IDB) *UserFactory {
	return NewUserFactory(db)
}

// Account 快捷构造 AccountFactory 实例。
func Account(db bun.IDB) *AccountFactory {
	return NewAccountFactory(db)
}
