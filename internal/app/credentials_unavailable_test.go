package app_test

import (
	"context"
	"errors"
	"testing"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/ShellMonster/Biu_Go/internal/service/bili"
)

// 没有凭据后端的时候，应用必须**降级跑起来**，而不是崩。
//
// 这条路是天天走的：非 macOS / Windows 平台上 `NewSystemStore` **恒**返回
// `(nil, ErrUnavailable)`，而 `MountLogin` 会把那个 nil 原样传下去。
// 之前 `Manager.Load` 第一次 `m.store.Load(ctx)` 就是空接口上的方法调用
// —— SIGSEGV，用户什么都没做错，应用就没了。
//
// 它同时也是 macOS 上的真实分支：purego 符号绑不上时后端一样建不出来。

func TestMountLoginWithoutABackendDoesNotPanic(t *testing.T) {
	a := app.New()
	t.Cleanup(a.Close)
	a.MountLogin(nil, bili.NewClient(bili.Options{}))

	if got := a.CredentialBackendName(); got != "none" {
		t.Errorf("后端名字是 %q，want none（界面上拿这个名字显示「登录态存不住」）", got)
	}
	if a.LoggedIn() {
		t.Error("没有后端却说自己是登录的")
	}
	if p := a.LoginState().Phase; p != app.LoginIdle {
		t.Errorf("没有后端时登录阶段是 %v，want LoginIdle", p)
	}
}

// TestLogoutAlwaysSucceedsWithoutABackend 退登必须永远能成功。
//
// 本来就没有东西存，清不清是一回事——报错的时候用户会被卡在「退不出去」
// 的状态里，而实际上凭据压根不在任何地方。
func TestLogoutAlwaysSucceedsWithoutABackend(t *testing.T) {
	a := app.New()
	t.Cleanup(a.Close)
	a.MountLogin(nil, bili.NewClient(bili.Options{}))
	if err := a.Logout(); err != nil {
		t.Errorf("没有后端时退登返回 %v", err)
	}
}

// TestSavingWithoutABackendIsNotSilentlyIgnored 存不进去要报错。
//
// 假装存成功了的后果是「登录看着成功，下次打开又登出」，而用户完全不知道为什么。
func TestSavingWithoutABackendIsNotSilentlyIgnored(t *testing.T) {
	m := app.NewManager(nil)
	if got := m.BackendName(); got != "none" {
		t.Errorf("后端名字是 %q，want none", got)
	}
	if err := m.Load(context.Background()); !errors.Is(err, app.ErrUnavailable) {
		t.Errorf("Load 返回 %v，want ErrUnavailable", err)
	}
	if err := m.Update(context.Background(), func(*app.Session) error { return nil }); !errors.Is(err, app.ErrUnavailable) {
		t.Errorf("Update 返回 %v，want ErrUnavailable", err)
	}
	if err := m.Logout(context.Background()); err != nil {
		t.Errorf("Logout 返回 %v，本来就没有东西可清", err)
	}
}
