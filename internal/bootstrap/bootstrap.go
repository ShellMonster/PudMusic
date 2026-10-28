// Package bootstrap coordinates the pre-window application assembly.
//
// The coordinator owns ordering and failure policy. Platform and UI details stay
// behind Dependencies so the ordering can be tested without a native window or
// a real network client.
package bootstrap

import (
	"log"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/media"
	"github.com/ShellMonster/PudMusic/internal/platform"
	"github.com/ShellMonster/PudMusic/internal/service/bili"
	"github.com/ShellMonster/PudMusic/internal/ui"
)

// Dependencies are the operations involved in assembling the application before
// the event loop and the first native window. Mounts that return an error are
// optional unless Assemble explicitly treats them as required.
type Dependencies struct {
	PinUserDataDir func() error
	NewApp         func() *app.App

	MountMigration       func() error
	MountSettings        func(*app.App) error
	MountFullPlayerPrefs func(*app.App)
	MountLogin           func(*app.App) *bili.Client
	MountStreaming       func(*app.App, *bili.Client)
	StartWarmUp          func(*app.App)
	MountCollections     func(*app.App, *bili.Client)
	MountExplore         func(*app.App, *bili.Client)
	MountLibrary         func(*app.App, *bili.Client)
	MountRecommend       func(*app.App, *bili.Client)
	MountLocalMusic      func(*app.App)
	MountLyrics          func(*app.App)
	MountComments        func(*app.App)
	MountDanmaku         func(*app.App)
	MountUpdates         func(*app.App) error
	MountDownloads       func(*app.App) error
	MountNotifications   func(*app.App)
	MountMedia           func(*app.App) *media.Session
	NoteMounted          func(*app.App)
	Run                  func(*app.App, *media.Session) error

	Logf func(string, ...any)
}

// DefaultDependencies wires the production platform, application and UI
// adapters. It does not execute any operation.
func DefaultDependencies() Dependencies {
	return Dependencies{
		PinUserDataDir:       platform.PinUserDataDir,
		NewApp:               app.New,
		MountMigration:       ui.MountMigration,
		MountSettings:        ui.MountSettings,
		MountFullPlayerPrefs: ui.MountFullPlayerPrefs,
		MountLogin:           ui.MountLogin,
		MountStreaming:       ui.MountStreaming,
		StartWarmUp:          func(a *app.App) { a.StartWarmUp() },
		MountCollections:     ui.MountCollections,
		MountExplore:         func(a *app.App, cli *bili.Client) { a.MountExplore(cli) },
		MountLibrary:         func(a *app.App, cli *bili.Client) { a.MountLibrary(cli) },
		MountRecommend:       func(a *app.App, cli *bili.Client) { a.MountRecommend(cli) },
		MountLocalMusic:      ui.MountLocalMusic,
		MountLyrics:          ui.MountLyrics,
		MountComments:        ui.MountComments,
		MountDanmaku:         ui.MountDanmaku,
		MountUpdates:         ui.MountUpdates,
		MountDownloads:       ui.MountDownloads,
		MountNotifications:   ui.MountNotifications,
		MountMedia:           ui.MountMedia,
		NoteMounted:          func(a *app.App) { a.NoteMounted() },
		Run:                  ui.Run,
		Logf:                 log.Printf,
	}
}

// Assemble performs all pre-window mounting and then hands control to the UI
// runtime. Pinning the data directory is attempted before App creation; if the
// platform cannot pin it, startup keeps the existing fallback behavior and
// continues with the framework's default directory. Migration, settings,
// updates and downloads retain their existing optional failure policy and are
// logged while startup continues.
func Assemble(d Dependencies) (*app.App, error) {
	if d.Logf == nil {
		d.Logf = log.Printf
	}
	if d.PinUserDataDir == nil {
		return nil, errMissingDependency("PinUserDataDir")
	}
	if err := d.PinUserDataDir(); err != nil {
		d.Logf("biu: 钉不住应用数据目录，这次的设置可能存不住：%v", err)
	}
	if d.NewApp == nil {
		return nil, errMissingDependency("NewApp")
	}
	a := d.NewApp()
	if a == nil {
		return nil, errMissingDependency("NewApp returned nil")
	}

	if d.MountMigration != nil {
		if err := d.MountMigration(); err != nil {
			d.Logf("biu: 升级数据失败，原文件都留着了：%v", err)
		}
	}
	if d.MountSettings != nil {
		if err := d.MountSettings(a); err != nil {
			d.Logf("biu: 拿不到应用数据目录，这次的设置存不住：%v", err)
		}
	}
	if d.MountFullPlayerPrefs != nil {
		d.MountFullPlayerPrefs(a)
	}

	var cli *bili.Client
	if d.MountLogin != nil {
		cli = d.MountLogin(a)
	}
	if d.MountStreaming != nil {
		d.MountStreaming(a, cli)
	}
	if d.StartWarmUp != nil {
		d.StartWarmUp(a)
	}
	if d.MountCollections != nil {
		d.MountCollections(a, cli)
	}
	if d.MountExplore != nil {
		d.MountExplore(a, cli)
	}
	if d.MountLibrary != nil {
		d.MountLibrary(a, cli)
	}
	if d.MountRecommend != nil {
		d.MountRecommend(a, cli)
	}
	if d.MountLocalMusic != nil {
		d.MountLocalMusic(a)
	}
	if d.MountLyrics != nil {
		d.MountLyrics(a)
	}
	if d.MountComments != nil {
		d.MountComments(a)
	}
	if d.MountDanmaku != nil {
		d.MountDanmaku(a)
	}
	if d.MountUpdates != nil {
		if err := d.MountUpdates(a); err != nil {
			d.Logf("biu: 挂自动更新失败：%v", err)
		}
	}
	if d.MountDownloads != nil {
		if err := d.MountDownloads(a); err != nil {
			d.Logf("biu: 挂下载失败，这次的下载功能用不了：%v", err)
		}
	}
	if d.MountNotifications != nil {
		d.MountNotifications(a)
	}
	var sess *media.Session
	if d.MountMedia != nil {
		sess = d.MountMedia(a)
	}
	if d.NoteMounted != nil {
		d.NoteMounted(a)
	}
	if d.Run != nil {
		if err := d.Run(a, sess); err != nil {
			return a, err
		}
	}
	return a, nil
}

// missingDependency is kept private so production callers see the original
// startup error while tests can still exercise invalid wiring safely.
type missingDependency string

func (e missingDependency) Error() string { return "bootstrap: missing dependency " + string(e) }

func errMissingDependency(name string) error { return missingDependency(name) }
