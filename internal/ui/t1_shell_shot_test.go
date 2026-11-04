// T1 外壳（侧栏 / 顶栏 / 播放条 / 弹层 / Toast / 命令面板）的画面验收。
//
//	BIU_SHOT_DIR=/some/dir go test ./internal/ui/ -run TestRenderT1ShellShots
//
// 深浅两套都出，逐张和 ref/ 里的原型截图对照。数据全是合成的，没有真实封面。
package ui

import (
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/notice"
	mygoui "github.com/egoist/mygo/ui"
)

// t1BarState 是外壳截图里播放条上那一首（合成数据，不带封面）。
func t1BarState() app.State {
	return app.State{
		HasTrack: true, Title: "渺小", Artist: "田馥甄 · 夜航电台", ArtistMid: 12345,
		Source: app.SourceOnline, BiliURL: "https://www.bilibili.com/video/BV1xx411c7mD",
		Quality: "Hi-Res", Rate: 1, PlayMode: app.PlayModeLoop, Volume: 0.8,
		Duration: 4*time.Minute + 24*time.Second, Position: 37 * time.Second,
		Playing: true,
	}
}

// t1Shell 造一个整窗外壳：和 `Shell.viewMain` 同一条画法，但播放状态用合成的 st。
// withoutLogin 让 t1Shell 不装登录态（占位选项，t1Guest 认它）。
func withoutLogin() shellOpt { return func(*shellSpec) {} }

// t1Guest 判断这次是不是要游客外壳：选项里带了 withoutLogin 的那个函数。
func t1Guest(opts []shellOpt) bool {
	want := reflect.ValueOf(withoutLogin()).Pointer()
	for _, o := range opts {
		if reflect.ValueOf(o).Pointer() == want {
			return true
		}
	}
	return false
}

func t1Shell(t *testing.T, dark bool, st app.State, opts ...shellOpt) *testShell {
	t.Helper()
	all := append([]shellOpt{
		systemIsDark(dark),
		withPrefs(mygoui.Preferences{ReduceMotion: true, TextScale: 1}),
		withView(func(s *Shell) func(*mygoui.Context) {
			return func(c *mygoui.Context) {
				s.wake.set(c)
				s.applyFrameTheme(c)
				s.syncUnderlay(c)
				s.drawUnderlay(c, st)
				s.guardAt("命令面板", s.viewCommandPalette)(c)
				s.viewToasts(c)
			}
		}),
	}, opts...)
	if !t1Guest(opts) {
		all = append(all, loggedInWith("42"))
	}
	b := newTestShell(t, all...)
	homeFrame(b.tt)
	return b
}

// t1Favorites 给侧栏塞几条合成的收藏夹（没有封面地址，画默认封面）。
func t1Favorites(b *testShell) {
	b.a.SetFavorites(app.FavoritesCreated, []app.Favorite{
		{ID: 1, Title: "我喜欢", Mid: 42, Type: 2, MediaCount: 5},
		{ID: 2, Title: "深夜循环", Mid: 42, Type: 2, MediaCount: 9},
		{ID: 3, Title: "千禧年华语", Mid: 42, Type: 2, MediaCount: 10},
		{ID: 4, Title: "独立与摇滚", Mid: 42, Type: 2, MediaCount: 6},
	})
}

func TestRenderT1ShellShots(t *testing.T) {
	dir := os.Getenv("BIU_SHOT_DIR")
	if dir == "" {
		t.Skip("设 BIU_SHOT_DIR 才会存图")
	}
	for _, dark := range []bool{true, false} {
		name := "light"
		if dark {
			name = "dark"
		}
		t.Run(name, func(t *testing.T) {
			// 外壳：展开的侧栏 + 收藏夹 + 播放条。
			{
				b := t1Shell(t, dark, t1BarState())
				t1Favorites(b)
				homeFrame(b.tt)
				save(t, dir, name+"-home-rec.png", b.tt)
			}
			// 收起的侧栏。
			{
				st := app.DefaultSettings("")
				st.SideMenuCollapsed = true
				b := t1Shell(t, dark, t1BarState(), withSettings(st))
				t1Favorites(b)
				homeFrame(b.tt)
				save(t, dir, name+"-collapsed-home.png", b.tt)
			}
			// 命令面板。
			{
				b := t1Shell(t, dark, t1BarState())
				b.s.paletteOpen = true
				homeFrame(b.tt)
				b.tt.Advance(400 * time.Millisecond)
				homeFrame(b.tt)
				save(t, dir, name+"-palette.png", b.tt)
			}
			// 音质弹层、睡眠定时弹层。
			for _, c := range []struct{ click, file string }{
				{"音质：" + t1BarState().Quality, "pop-quality"},
				{"睡眠定时", "pop-sleep"},
			} {
				b := t1Shell(t, dark, t1BarState())
				if err := b.tt.Click(c.click); err != nil {
					t.Fatalf("点不到 %q：%v，文字有 %q", c.click, err, b.tt.Texts())
				}
				homeFrame(b.tt)
				b.tt.Advance(400 * time.Millisecond)
				homeFrame(b.tt)
				save(t, dir, name+"-"+c.file+".png", b.tt)
			}
			// Toast。
			{
				b := t1Shell(t, dark, t1BarState())
				b.s.addToast("已加入下一首播放", notice.Success)
				homeFrame(b.tt)
				b.tt.Advance(400 * time.Millisecond)
				homeFrame(b.tt)
				save(t, dir, name+"-toast.png", b.tt)
			}
			// 未登录：侧栏少三项、顶栏是「登录」键。
			{
				b := t1Shell(t, dark, t1BarState(), withoutLogin())
				b.s.navigate(followPath)
				homeFrame(b.tt)
				save(t, dir, name+"-loggedout-follow.png", b.tt)
			}
			// 搜索联想（空输入：历史 + 热门）。
			{
				b := t1Shell(t, dark, t1BarState())
				b.s.hist.add("陈奕迅")
				b.s.hist.add("万青 采石")
				if err := b.tt.Click(searchLabel); err != nil {
					t.Fatalf("点不到搜索框：%v", err)
				}
				homeFrame(b.tt)
				b.tt.Advance(400 * time.Millisecond)
				homeFrame(b.tt)
				save(t, dir, name+"-suggest.png", b.tt)
			}
			// 头像菜单。
			{
				b := t1Shell(t, dark, t1BarState())
				if err := b.tt.Click(avatarLabel); err != nil {
					t.Fatalf("点不到头像：%v", err)
				}
				homeFrame(b.tt)
				b.tt.Advance(400 * time.Millisecond)
				homeFrame(b.tt)
				save(t, dir, name+"-avatar-menu.png", b.tt)
			}
		})
	}
}
