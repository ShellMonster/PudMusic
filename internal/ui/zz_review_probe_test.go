// 临时探针（评审用，跑完删掉）。
package ui

import (
	"testing"

	"github.com/ShellMonster/Biu_Go/internal/app"
	mygoui "github.com/egoist/mygo/ui"
)

// 收藏夹：最小窗口 + 最宽侧栏 + 字号 1.4，搜索展开之后各控件的位置。
func TestZZProbeCollectionScale14(t *testing.T) {
	for _, side := range []float64{160, 240, 360, 480} {
		st := app.DefaultSettings("")
		st.SideMenuWidth = side
		_, s, tt := collectionShellOpts(t,
			[]shellOpt{withSettings(st), at(minWindowW, minWindowH),
				withPrefs(mygoui.Preferences{ReduceMotion: true, TextScale: 1.4})},
			"", "12345", mediaOf(1, "歌 一"), mediaOf(2, "歌 二"))
		waitSongs(t, tt, s, 2)

		sort, ok := tt.Find("排序：最近收藏")
		if !ok {
			t.Fatalf("侧栏 %.0f：找不到排序下拉，文字 %q", side, tt.Texts())
		}
		if err := tt.Click(collectionSearchLabel); err != nil {
			t.Fatalf("侧栏 %.0f：点不开搜索 %v", side, err)
		}
		tt.Frame()
		tt.Frame()
		box, ok := tt.Find(collectionSearchLabel)
		if !ok {
			t.Fatalf("侧栏 %.0f：展开后找不到搜索框，文字 %q", side, tt.Texts())
		}
		sort, _ = tt.Find("排序：最近收藏")
		t.Logf("侧栏 %.0f：搜索框 x=%.1f w=%.1f 右缘=%.1f | 排序 x=%.1f w=%.1f 右缘=%.1f | 窗口 %d",
			side, box.X, box.W, box.X+box.W, sort.X, sort.W, sort.X+sort.W, minWindowW)
	}
}

// 本地音乐：最小窗口，工具条右边那几个键还在窗口里吗。
func TestZZProbeLocalMinWindow(t *testing.T) {
	root := t.TempDir()
	libSong(t, root, "第一首.mp3")
	libSong(t, root, "第二首.mp3")
	st := app.DefaultSettings("")
	st.LocalMusicDirs = []string{root}
	b := newTestShell(t,
		withSettings(st), at(minWindowW, minWindowH),
		withPrefs(mygoui.Preferences{ReduceMotion: true, TextScale: 1}),
		mounted(func(_ *testing.T, a *app.App) { a.MountLocalMusic(app.LocalMusic{}) }))
	tt := b.tt
	openLocal(t, tt)
	settleLocal(t, b.a, tt)
	tt.Frame()
	for _, name := range []string{"重新扫描", "搜索标题", "全部目录", "播放全部", "全部加入队列"} {
		r, ok := tt.Find(name)
		if !ok {
			t.Logf("本地音乐 %q：找不到（文字 %q）", name, tt.Texts())
			continue
		}
		t.Logf("本地音乐 %q：x=%.1f w=%.1f 右缘=%.1f | 窗口 %d", name, r.X, r.W, r.X+r.W, minWindowW)
	}
	// 展开搜索之后呢？
	if err := tt.Click(searchLocalLabel); err != nil {
		t.Fatalf("点不开本地搜索：%v", err)
	}
	tt.Frame()
	tt.Frame()
	for _, name := range []string{"重新扫描", "搜索标题", "全部目录"} {
		r, ok := tt.Find(name)
		if !ok {
			t.Logf("展开后 本地音乐 %q：找不到（文字 %q）", name, tt.Texts())
			continue
		}
		t.Logf("展开后 本地音乐 %q：x=%.1f w=%.1f 右缘=%.1f | 窗口 %d", name, r.X, r.W, r.X+r.W, minWindowW)
	}
}

// 收藏夹：点开搜索、打字，搜索框会不会自己收回去？列表有没有跟着筛？
func TestZZProbeCollectionTyping(t *testing.T) {
	_, s, tt := collectionShell(t, "12345",
		mediaOf(1, "歌 一"), mediaOf(2, "歌 二"), mediaOf(3, "歌 三"))
	waitSongs(t, tt, s, 3)

	tt.SetFocused(true)
	if err := tt.Click(collectionSearchLabel); err != nil {
		t.Fatalf("点不开搜索：%v", err)
	}
	tt.Frame()
	tt.Type("歌")
	tt.Frame()
	tt.Frame()
	r, ok := tt.Find(collectionSearchLabel)
	if !ok {
		t.Fatalf("打字之后搜索不见了，文字 %q", tt.Texts())
	}
	t.Logf("打字之后：搜索框 w=%.1f（收起是 28）", r.W)
	t.Logf("打字之后画面上的字：%q", tt.Texts())
}

// 本地音乐：点开搜索、打字，框还在不在？列表筛没筛？
func TestZZProbeLocalTyping(t *testing.T) {
	root := t.TempDir()
	libSong(t, root, "第一首.mp3")
	libSong(t, root, "第二首.mp3")
	tt, _, _, _ := shellOnLocalMusic(t, root)
	tt.SetFocused(true)
	if err := tt.Click(searchLocalLabel); err != nil {
		t.Fatalf("点不开本地搜索：%v", err)
	}
	tt.Frame()
	tt.Type("第一")
	tt.Frame()
	tt.Frame()
	r, ok := tt.Find(searchLocalLabel)
	if !ok {
		t.Fatalf("打字之后搜索不见了，文字 %q", tt.Texts())
	}
	t.Logf("打字之后：搜索框 w=%.1f（收起是 28）", r.W)
	t.Logf("打字之后画面上的字：%q", tt.Texts())
}

// 收藏夹：搜索开着、列表已经筛了，点别的控件之后搜索收不收？列表还筛着吗？
func TestZZProbeCollapseWithFilter(t *testing.T) {
	_, s, tt := collectionShell(t, "12345",
		mediaOf(1, "歌 一"), mediaOf(2, "歌 二"), mediaOf(3, "歌 三"))
	waitSongs(t, tt, s, 3)
	tt.SetFocused(true)
	if err := tt.Click(collectionSearchLabel); err != nil {
		t.Fatalf("点不开搜索：%v", err)
	}
	tt.Frame()
	tt.Type("歌 二")
	tt.Frame()
	// 推钟过防抖（500ms）
	tt.Advance(600 * 1000000)
	tt.Frame()
	tt.Frame()
	t.Logf("筛完之后的字：%q", tt.Texts())
	// 点「多选」——一个拿得走焦点的控件
	if err := tt.Click("多选"); err != nil {
		t.Fatalf("点多选：%v", err)
	}
	tt.Frame()
	tt.Frame()
	r, ok := tt.Find(collectionSearchLabel)
	if !ok {
		t.Fatalf("搜索不见了")
	}
	t.Logf("点别处之后搜索框 w=%.1f（收起 28 / 展开 240）", r.W)
	t.Logf("点别处之后的字：%q", tt.Texts())
}

// 收藏夹：打字之后 p.draft 到底有没有那个词？
func TestZZProbeCollectionDraft(t *testing.T) {
	_, s, tt := collectionShell(t, "12345",
		mediaOf(1, "歌 一"), mediaOf(2, "歌 二"), mediaOf(3, "歌 三"))
	waitSongs(t, tt, s, 3)
	tt.SetFocused(true)
	if err := tt.Click(collectionSearchLabel); err != nil {
		t.Fatalf("点不开搜索：%v", err)
	}
	tt.Frame()
	tt.Type("歌 二")
	tt.Frame()
	tt.Frame()
	t.Logf("draft=%q seenDraft=%q keyword=%q", s.collection.draft, s.collection.seenDraft, s.collection.keyword)
	r, _ := tt.Find(collectionSearchLabel)
	t.Logf("搜索框 w=%.1f", r.W)
	// 再敲一个字，看还在不在框里
	tt.Type("三")
	tt.Frame()
	tt.Frame()
	t.Logf("再敲之后 draft=%q seenDraft=%q", s.collection.draft, s.collection.seenDraft)
}

// 本地音乐：搜索开着、列表已经筛了，点一行歌之后搜索收不收？列表还筛着吗？
func TestZZProbeLocalRowClick(t *testing.T) {
	root := t.TempDir()
	libSong(t, root, "第一首.mp3")
	libSong(t, root, "第二首.mp3")
	tt, _, _, _ := shellOnLocalMusic(t, root)
	tt.SetFocused(true)
	if err := tt.Click(searchLocalLabel); err != nil {
		t.Fatalf("点不开本地搜索：%v", err)
	}
	tt.Frame()
	tt.Type("第一")
	tt.Frame()
	tt.Frame()
	r, _ := tt.Find(searchLocalLabel)
	t.Logf("打字之后：搜索框 w=%.1f；画面上的歌：%q", r.W, tt.Texts())
	// 点第二行（筛选之后它已经不在画面上了，所以点第一行）
	if _, ok := tt.Find("第一首"); !ok {
		t.Fatalf("找不到第一首")
	}
	if err := tt.Click("第一首"); err != nil {
		t.Fatalf("点歌名：%v", err)
	}
	tt.Frame()
	tt.Frame()
	r2, ok := tt.Find(searchLocalLabel)
	t.Logf("点歌之后：搜索框 w=%.1f（收起 28 / 展开 180）找得到=%v", r2.W, ok)
	t.Logf("点歌之后的字：%q", tt.Texts())
}

// 收藏夹：开 → 收 → 再开，第二次还有焦点吗？（AutoFocus 只在节点「出生」那一帧给焦点）
func TestZZProbeReopenFocus(t *testing.T) {
	_, s, tt := collectionShell(t, "12345", mediaOf(1, "歌 一"), mediaOf(2, "歌 二"))
	waitSongs(t, tt, s, 2)
	tt.SetFocused(true)

	open := func(tag string) {
		if err := tt.Click(collectionSearchLabel); err != nil {
			t.Fatalf("%s 点不开搜索：%v", tag, err)
		}
		tt.Frame()
		tt.Frame()
		r, ok := tt.Find(collectionSearchLabel)
		if !ok {
			t.Fatalf("%s 搜索不见了", tag)
		}
		t.Logf("%s：w=%.1f 焦点在框里=%v", tag, r.W, tt.Focused(collectionSearchLabel))
	}
	open("第一次开")
	// 点「多选」把它收回去
	if err := tt.Click("多选"); err != nil {
		t.Fatalf("点多选：%v", err)
	}
	tt.Frame()
	tt.Frame()
	r, _ := tt.Find(collectionSearchLabel)
	t.Logf("收回去之后：w=%.1f", r.W)
	// 再点开
	open("第二次开")
	// 能不能直接打字
	tt.Type("歌")
	tt.Frame()
	t.Logf("第二次打字之后 draft=%q", s.collection.draft)
}
