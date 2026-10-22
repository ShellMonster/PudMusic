// #179 的出图：八处阴影 + 三处 hover 放大，深浅两套。
//
//	BIU_SHOT_DIR=/some/dir go test ./internal/ui/ -run TestRenderShadowAndZoomShots
//
// ⚠️ **深浅两套都要出**：HeroUI 的 `shadow="sm|md|lg"` 深浅两套值不一样
// （深色那套多一层 1 像素的白色**内**阴影），而 Tailwind 那一档是**同一个黑**
// 压在两个不同的底上——深色下 10% 的黑几乎看不见，浅色下清清楚楚。
//
// 夹具在 `upspace_test.go` / `follow_test.go` / `downloadlist_test.go` 里；
// 这里只负责把各处摆一份、各存一张。
package ui

import (
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/ShellMonster/Biu_Go/internal/service/download"
	mygoui "github.com/egoist/mygo/ui"
)

// TestRenderShadowAndZoomShots 把这一票动过的地方各存一张 PNG，深浅各一套。
func TestRenderShadowAndZoomShots(t *testing.T) {
	dir := os.Getenv("BIU_SHOT_DIR")
	if dir == "" {
		t.Skip("设 BIU_SHOT_DIR 才会存图")
	}
	for _, dark := range []bool{false, true} {
		name := "浅色"
		if dark {
			name = "深色"
		}
		t.Run(name, func(t *testing.T) {
			shotQueueDrawer(t, dir, name, dark)
			shotBackToTop(t, dir, name, dark)
			shotGridPlayButton(t, dir, name, dark)
			shotNewMusicCover(t, dir, name, dark)
			shotDownloadTable(t, dir, name, dark)
			shotColorSwatch(t, dir, name, dark)
			shotFollowCard(t, dir, name, dark)
			shotSpaceAvatar(t, dir, name, dark)
			shotCollectionZoom(t, dir, name, dark)
			shotAuthorZoom(t, dir, name, dark)
			shotNavbarZoom(t, dir, name, dark)
		})
	}
}

// shotQueueDrawer 存队列抽屉：面板外面那一圈影。
func shotQueueDrawer(t *testing.T, dir, name string, dark bool) {
	t.Helper()
	s, tt := shotSpaceShell(t, newUIExplore(t), dark, true, "77")
	s.drawerOpen = true
	tt.Frame()
	save(t, dir, "50-"+name+"-队列抽屉.png", tt)
}

// shotBackToTop 存回顶那一枚：它底下那一圈影。
func shotBackToTop(t *testing.T, dir, name string, dark bool) {
	t.Helper()
	b := newUIExplore(t)
	uiSpaceData(b, "我自己", 0, 1)
	b.on("/x/relation/followings", func(w http.ResponseWriter, r *http.Request) {
		uiExploreJSON(w, 0, map[string]any{"total": 1, "list": []any{
			map[string]any{"mid": 11, "uname": "第一个 UP", "face": "https://f/author-11.jpg"},
		}})
	})
	s, tt := shotSpaceShell(t, b, dark, true, "77")
	s.navigate(dynamicFeedPath)
	tt.Frame()
	f := s.dynamicFeedOf()
	waitFrames(t, tt, "人到了", func() bool { return len(f.authors) == 1 })
	tt.Frame()
	// 滚到底，那一枚才出来。
	for range 40 {
		tt.Scroll(600, 400, 0, 900)
		tt.Frame()
		if tt.HasText("返回顶部") {
			break
		}
	}
	save(t, dir, "51-"+name+"-回顶.png", tt)
}

// shotGridPlayButton 存网格卡片右下角那个主色播放钮：它左边那一圈影。
func shotGridPlayButton(t *testing.T, dir, name string, dark bool) {
	t.Helper()
	_, tt := shotHomeShell(t, dark)
	tt.Frame()
	// 指针停在第一张网格卡的封面上。
	card, ok := tt.Find("音乐 1")
	if !ok {
		t.Fatalf("画面上找不到第一张卡，文字有 %q", tt.Texts())
	}
	tt.Move(card.X+card.W/2, card.Y+card.H/2)
	tt.Frame()
	tt.Advance(300 * time.Millisecond)
	tt.Frame()
	save(t, dir, "52-"+name+"-网格播放钮.png", tt)
}

// shotNewMusicCover 存「新歌速递」那块：封面外面那一圈 `shadow-md`。
//
// ⚠️ **新歌速递只画在「流行」那一屏的顶上**（`homepage.go:225`），所以要先切到
// 那一档——停在「音乐」那一档的话，屏幕上只有一张列表。
func shotNewMusicCover(t *testing.T, dir, name string, dark bool) {
	t.Helper()
	s, tt := shotHomeShell(t, dark)
	s.home.tab = homePopTab
	tt.Frame()
	save(t, dir, "53-"+name+"-新歌速递封面.png", tt)
}

// shotDownloadTable 存下载记录那张卡：卡片外面那一圈 `shadow-sm`。
func shotDownloadTable(t *testing.T, dir, name string, dark bool) {
	t.Helper()
	rows := []download.Snapshot{
		dlSnapshot("第一首", download.StatusDone, 100, 100),
		dlSnapshot("第二首", download.StatusDone, 200, 200),
	}
	_, tt := shotDownloadShell(t, dark, rows)
	save(t, dir, "54-"+name+"-下载表.png", tt)
}

// shotColorSwatch 存设置里那两块色卡：色卡外面那一圈 `shadow-sm`。
func shotColorSwatch(t *testing.T, dir, name string, dark bool) {
	t.Helper()
	mode, systemDark := app.ThemeModeLight, false
	if dark {
		mode, systemDark = app.ThemeModeDark, true
	}
	tt, s, _, _ := shellWithSettings(t, mode, systemDark)
	s.navigate(settingsPath)
	tt.Frame()
	tt.Frame()
	save(t, dir, "55-"+name+"-设置色卡.png", tt)
}

// shotFollowCard 存关注那一页：卡片外面那一圈影，以及 hover 之后升起来的那条药丸。
func shotFollowCard(t *testing.T, dir, name string, dark bool) {
	t.Helper()
	b := newUIExplore(t)
	uiFollowTags(b, map[int]any{0: "默认分组"})
	uiFollowList(b, 4, []any{
		uiFollowUser(11, "第一个 UP"), uiFollowUser(12, "第二个 UP"),
		uiFollowUser(13, "第三个 UP"), uiFollowUser(14, "第四个 UP"),
	})
	uiFollowSearch(b, "", nil)
	uiFollowTag(b, 0, []any{})

	s, tt := shotSpaceShell(t, b, dark, true, "77")
	s.navigate(followPath)
	tt.Frame()
	f := s.followPageOf()
	waitFrames(t, tt, "人到了", func() bool { return len(f.users) == 4 && f.tagStarted })
	tt.Frame()
	save(t, dir, "56-"+name+"-关注卡.png", tt)

	// hover 之后升起来的那条药丸（它自己那一圈 `shadow-lg`）。
	av, ok := tt.Find(f.users[1].Face)
	if !ok {
		t.Fatalf("第二张卡的头像没画出来，文字有 %q", tt.Texts())
	}
	tt.Move(av.X+av.W/2, av.Y+av.H/2)
	tt.Frame()
	tt.Advance(followActionBarTime)
	tt.Frame()
	save(t, dir, "57-"+name+"-关注卡-操作条.png", tt)
}

// shotSpaceAvatar 存 UP 空间的头图区：头像外面那一圈 `shadow-lg`。
func shotSpaceAvatar(t *testing.T, dir, name string, dark bool) {
	t.Helper()
	b := newUIExplore(t)
	uiSpaceData(b, "Biu 的用户", 0, 123456)
	s, tt := shotSpaceShell(t, b, dark, true, "77")
	openSpace(t, s, tt, 42)
	save(t, dir, "58-"+name+"-UP空间头像.png", tt)
}

// shotCollectionZoom 存 UP 空间「收藏夹」那一格：指针移上去之后封面在框里涨大。
func shotCollectionZoom(t *testing.T, dir, name string, dark bool) {
	t.Helper()
	b := newUIExplore(t)
	uiSpaceData(b, "有收藏夹的 UP", 0, 1)
	cover := uiServePNG(b, "cover.png", uiCoverPNG(t, 48, 64))
	b.on("/x/v3/fav/folder/created/list", func(w http.ResponseWriter, r *http.Request) {
		uiExploreJSON(w, 0, map[string]any{"list": []any{
			map[string]any{"id": 100, "title": "点我", "cover": cover, "media_count": 8},
		}})
	})
	s, tt := shotSpaceShell(t, b, dark, true, "77")
	openSpace(t, s, tt, 42)
	sp := s.upSpaces[42]
	sp.tab = upTabCollection
	tt.Frame()
	waitFrames(t, tt, "收藏夹加载完", func() bool { return sp.fav.started })
	waitFrames(t, tt, "封面下来了", func() bool { return s.images.Get(cover).Ready() })
	tt.Frame()
	before, ok := tt.Find(cover)
	if !ok {
		t.Fatalf("画面上找不到那张封面，文字有 %q", tt.Texts())
	}
	tt.Move(before.X+before.W/2, before.Y+before.H/2)
	tt.Frame()
	tt.Advance(hoverZoomTime)
	tt.Frame()
	save(t, dir, "59-"+name+"-收藏夹封面放大.png", tt)
}

// shotAuthorZoom 存动态流作者栏：指针移上去之后那个圆头像涨大。
func shotAuthorZoom(t *testing.T, dir, name string, dark bool) {
	t.Helper()
	b := newUIExplore(t)
	uiSpaceData(b, "我自己", 0, 1)
	b.on("/x/relation/followings", func(w http.ResponseWriter, r *http.Request) {
		uiExploreJSON(w, 0, map[string]any{"total": 2, "list": []any{
			map[string]any{"mid": 11, "uname": "第一个 UP", "face": "https://f/author-11.jpg"},
			map[string]any{"mid": 12, "uname": "第二个 UP", "face": "https://f/author-12.jpg"},
		}})
	})
	s, tt := shotSpaceShell(t, b, dark, true, "77")
	s.navigate(dynamicFeedPath)
	tt.Frame()
	f := s.dynamicFeedOf()
	waitFrames(t, tt, "人到了", func() bool { return len(f.authors) == 2 })
	tt.Frame()
	av, ok := tt.Find("https://f/author-11.jpg")
	if !ok {
		t.Fatalf("作者栏那个头像没画出来，文字有 %q", tt.Texts())
	}
	tt.Move(av.X+av.W/2, av.Y+av.H/2)
	tt.Frame()
	tt.Advance(hoverZoomTime)
	tt.Frame()
	save(t, dir, "60-"+name+"-作者头像放大.png", tt)
}

// shotNavbarZoom 存顶栏：指针移上去之后那个头像涨大。
func shotNavbarZoom(t *testing.T, dir, name string, dark bool) {
	t.Helper()
	b := newTestShell(t, systemIsDark(dark))
	tt := b.tt
	tt.Frame()
	av, ok := tt.Find(avatarLabel)
	if !ok {
		t.Fatalf("画面上找不到顶栏那个头像，文字有 %q", tt.Texts())
	}
	tt.Move(av.X+av.W/2, av.Y+av.H/2)
	tt.Frame()
	tt.Advance(hoverZoomTime)
	tt.Frame()
	save(t, dir, "61-"+name+"-顶栏头像放大.png", tt)
}

// shotDownloadShell 造一个装着下载记录的外壳。
//
// ⚠️ **不能直接用 `newRows`**：那一条走的是 `ui.NewTester`（**没有深浅那一档**），
// 于是深色那一套存出来的是浅色的图——两套图一模一样，而这一票要的恰恰是
// 「深浅两套的影不一样」。
func shotDownloadShell(t *testing.T, dark bool, rows []download.Snapshot) (*Shell, *mygoui.Tester) {
	t.Helper()
	b := newTestShell(t, systemIsDark(dark))
	b.s.downloads.rowsFn = func() []download.Snapshot { return rows }
	b.tt.Frame()
	b.s.navigate(downloadListPath)
	dlFrame(b.tt)
	return b.s, b.tt
}

// shotHomeShell 造一个带着首页的外壳，**显示样式是网格那一档**。
//
// ⚠️ **网格那一档是这一票要的**：网格卡片右下角那个主色播放钮是 `shadow-2xl`
// 的那一处，而列表档根本没有那一枚。
func shotHomeShell(t *testing.T, dark bool) (*Shell, *mygoui.Tester) {
	t.Helper()
	st := app.DefaultSettings("")
	st.DisplayMode = app.DisplayModeCard
	b := newTestShell(t, systemIsDark(dark), withSettings(st))
	s := b.s
	s.home.SetSongs(fullSongs())
	homeFrame(b.tt)
	return s, b.tt
}
