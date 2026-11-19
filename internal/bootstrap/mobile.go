//go:build ios || android

package bootstrap

import (
	"log"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/media"
	"github.com/egoist/mygo"
)

func MobileDependencies(run func(*app.App, *media.Session) error) Dependencies {
	d := DefaultDependencies()
	d.MountUpdates = nil
	d.MountNotifications = nil
	noteMounted := d.NoteMounted
	d.NoteMounted = func(a *app.App) {
		mygo.App.OnSaveState(func() []byte {
			a.SavePlayback()
			if err := a.FlushSettings(); err != nil {
				log.Printf("pudmusic: 进入后台时保存设置失败：%v", err)
			}
			return nil
		})
		noteMounted(a)
	}
	d.Run = run
	return d
}
