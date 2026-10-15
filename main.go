// Biu MyGo 是用 MyGo 框架重写的 Biu 音乐播放器。
//
// 界面是 MyGo 自己画的原生 UI（即时模式，没有 webview），音频在 Go 里解码。
// 当前阶段只有最小的一步：打开一个本地文件，放出声音，进度条能拖。
package main

import (
	"log"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/ShellMonster/Biu_Go/internal/ui"
	"github.com/egoist/mygo"
	// 本包的 ui 是我们自己的界面层，这里要用 MyGo 的 ui 包，所以换个名字。
	mygoui "github.com/egoist/mygo/ui"
)

func main() {
	mygo.App.WhenReady(func() {
		a := app.New()
		s := ui.New(a)
		w := mygo.NewWindow(mygo.WindowOptions{
			Title:     "Biu MyGo",
			Width:     760,
			Height:    460,
			MinWidth:  560,
			MinHeight: 320,
			// 窗口停在用户上次关掉它的地方。
			StateKey: "main",
			// 窗口里是 MyGo 自己画的界面，不是网页。
			Content: mygoui.View(s.View),
		})
		// 装唤醒回调、接窗口关闭。必须在建窗口之后：外壳要拿得到窗口句柄。
		s.Attach(w)
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
