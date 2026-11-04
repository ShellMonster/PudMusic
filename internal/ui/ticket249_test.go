// 票 #249 留下的两个测试夹具（顶栏外壳、开着队列抽屉的外壳）。
//
// 「图标键统一成一档」那一批断言已经随旧设计删掉：顶栏断言在 T1（顶栏改版）里删，
// 队列抽屉和满屏播放器的断言在 T4 里删；两边删的是同一文件里的不同测试，合并后只剩
// 夹具，给 `ticket249_shot_test.go` 用。
package ui

import (
	"testing"

	"github.com/ShellMonster/PudMusic/internal/app"
	mygoui "github.com/egoist/mygo/ui"
)

// navBarShell 建一个**只画顶栏**的外壳，返回外壳和 Tester。
//
// 和 `navBarTester` 同一套画法，只是把外壳也返回来——这一票要拿主题算主色
// （`ink.AccentOn` 是**从主题现算**的，不是写死的一个绿）。
//
// ⚠️ **要装主题**（`s.applyFrameTheme(c)`）：只画顶栏的那一支**不装**的话，画出来
// 的是**框架默认那份主题**（主色是蓝的 #2563eb），而设置里的主色是棕的
// （#a8552e）——按棕的去找主色像素，一个都找不到（同 `playbar_layout_test.go` 的
// `playbarTester` 顶上那一段）。
//
// ⚠️ **只画顶栏**：整窗里「登录」这两个字在别处也有（侧栏那一行），而
// `Tester.Find` 认的是**先画的那个**——画整窗的话量到的是别处那一枚。
func navBarShell(t *testing.T, preset app.Accent, dark bool) (*Shell, *mygoui.Tester) {
	t.Helper()
	st := app.DefaultSettings("")
	st.Accent = preset
	b := newTestShell(t, withSettings(st), systemIsDark(dark), withView(func(s *Shell) func(*mygoui.Context) {
		return func(c *mygoui.Context) {
			s.wake.set(c)
			s.applyFrameTheme(c)
			s.viewNav(c)
		}
	}))
	tt := b.tt
	tt.SetFocused(true)
	tt.Frame()
	return b.s, tt
}

// drawerShell 建一个**开着队列抽屉**的外壳（两行歌）。
//
// 深浅两套由 `systemIsDark` 决定——主色当字色那一档（`ink.AccentOn`）深浅两个
// 不同的数，所以这一族必须两套都跑。
func drawerShell(t *testing.T, preset app.Accent, dark bool) *harness {
	t.Helper()
	st := app.DefaultSettings("")
	st.Accent = preset
	b := newTestShell(t, withSettings(st), at(winW, winH), systemIsDark(dark))
	h := &harness{app: b.a, tt: b.tt, s: b.s}
	second := app.Item{
		Source: "online", Type: "audio", Sid: "999", Bvid: "BV1probe", Cid: "1",
		Title: "队列里的第二首",
	}
	enqueue(t, h, append(copiesOf(t, 1), second)...)
	openDrawer(t, h)
	return h
}
