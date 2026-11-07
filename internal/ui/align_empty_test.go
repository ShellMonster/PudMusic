package ui

import (
	"errors"
	"testing"
	"time"

	"github.com/ShellMonster/PudMusic/internal/app"
	mygoui "github.com/egoist/mygo/ui"
)

// TestAnEmptyFolderInvitesYouToDiscoverMusic 锁住空收藏夹的文案和出路（原型 app.js:638）：
// 「还没有收藏在这个收藏夹里」+「去发现音乐」，点了回首页。
//
// 曾经出错的行为：用通用的「暂无内容」，没有按钮，空收藏夹是死胡同。
func TestAnEmptyFolderInvitesYouToDiscoverMusic(t *testing.T) {
	_, s, tt := collectionShell(t, "12345")
	waitSongs(t, tt, s, 0)
	wantText(t, tt, "还没有收藏在这个收藏夹里")
	clickAfterScrollingIntoView(t, tt, "去发现音乐")
	waitFrames(t, tt, "回到首页", func() bool { return s.router.Path() == homePath })
}

// clickAfterScrollingIntoView 把页面往下滚到 label 露在播放条上面，再点它。
//
// 1200×800 下空收藏夹那枚键原本在 y≈780——播放条（84 高）底下，点不到；空状态和头图区放进
// 滚动区之后滚一下就能点到。这一步本身就是在验「点得到」：滚不动的话这里直接失败。
func clickAfterScrollingIntoView(t *testing.T, tt *mygoui.Tester, label string) {
	t.Helper()
	r, ok := tt.Find(label)
	if !ok {
		t.Fatalf("没有「%s」，文字有 %q", label, tt.Texts())
	}
	h := tt.Image().Bounds().Dy()
	limit := float32(h) - 84 - 8
	if r.Y+r.H > limit {
		scrollToEnd(t, tt, 600, 400, "「"+label+"」滚到播放条上面", func() bool {
			r, ok = tt.Find(label)
			return ok && r.Y+r.H <= limit
		})
	}
	tt.ClickAt(r.X+r.W/2, r.Y+r.H/2)
	tt.Frame()
}

// TestTheDefaultFolderSaysItIsStillEmpty 锁住默认收藏夹（原型的「我喜欢」）空着时说「这里还是空的」
// （原型 app.js:638 的 `isLiked` 那一支）。
func TestTheDefaultFolderSaysItIsStillEmpty(t *testing.T) {
	b := newCollectionBili(t)
	b.setMedias()
	b.setAttr(0) // 0 = B 站发下来的默认收藏夹
	_, s, tt := collectionShellFrom(t, b, nil, "", "12345")
	waitSongs(t, tt, s, 0)
	waitFrames(t, tt, "默认收藏夹的空状态", func() bool { return showsText(tt, noLikedYet.Title) })
	if !showsText(tt, "去发现音乐") {
		t.Errorf("默认收藏夹空着时也该有「去发现音乐」，文字有 %q", tt.Texts())
	}
}

// TestClearingTheCollectionSearchBringsTheSongsBack 锁住搜索无结果时的文案和「清空搜索」（原型 app.js:639）：
// 说「列表里没有匹配的歌曲」，点「清空搜索」后框清空、列表回到全部。
func TestClearingTheCollectionSearchBringsTheSongsBack(t *testing.T) {
	clock := newFakeClock(time.Unix(2026, 0))
	_, s, tt := collectionShellOpts(t, []shellOpt{withClock(clock)}, "", "12345",
		mediaOf(1, "歌 一"), mediaOf(2, "歌 二"))
	waitSongs(t, tt, s, 2)

	typeInto(t, tt, collectionSearchLabel, "没有这首")
	tt.Frame()
	tt.Frame()
	clock.Advance(searchDebounce + time.Millisecond)
	tt.Frame()
	waitFrames(t, tt, "搜索无结果的空状态", func() bool { return showsText(tt, noSearchResults.Title) })
	if !showsText(tt, "没有和「没有这首」匹配的歌曲。") {
		t.Errorf("无结果的说明里该带上搜的词，文字有 %q", tt.Texts())
	}

	clickAfterScrollingIntoView(t, tt, "清空搜索")
	waitFrames(t, tt, "清空之后列表回到两首", func() bool {
		return s.collection != nil && s.collection.keyword == "" && s.collection.list.Paged().Loaded == 2
	})
	if s.collection.draft != "" {
		t.Errorf("清空搜索之后框里还留着 %q", s.collection.draft)
	}
}

// TestLoggedOutLaterAndHistoryKeepTheirPageHead 锁住稍后再看 / 历史记录未登录时先画页头、再画登录提示
// （原型 app.js:699 / 709 `head() + loginPrompt(...)`）。
//
// 曾经出错的行为：整页只有一个登录提示框，没有页头，看不出自己在哪一页。
func TestLoggedOutLaterAndHistoryKeepTheirPageHead(t *testing.T) {
	for _, c := range []struct{ path, title, prompt string }{
		{laterPath, "稍后再看", "登录后查看稍后再看"},
		{historyPath, "历史记录", "登录后查看历史记录"},
	} {
		t.Run(c.title, func(t *testing.T) {
			f := newLibFake(t)
			b := newTestShell(t, mounted(func(t *testing.T, a *app.App) {
				a.MountLibrary(biliForTest(t, f)) // 挂上客户端，但不登录
			}))
			s, tt := b.s, b.tt
			tt.Frame()
			s.navigate(c.path)
			tt.Frame()

			head, ok := tt.Find(c.title)
			if !ok {
				t.Fatalf("未登录的%s没有页头，文字有 %q", c.title, tt.Texts())
			}
			prompt, ok := tt.Find(c.prompt)
			if !ok {
				t.Fatalf("未登录的%s没有登录提示，文字有 %q", c.title, tt.Texts())
			}
			if head.Y >= prompt.Y {
				t.Errorf("页头（y=%.0f）该在登录提示（y=%.0f）上面", head.Y, prompt.Y)
			}
		})
	}
}

// TestALoadErrorIsAnEmptyStateWithARetry 锁住「拉失败」那一格走 `pud.Empty` 体系（DESIGN §0′.T2）：
// 24 号宋体标题「这一块没拉出来」、说明里写清哪一趟、带名字的重试键，点了真的重试。
func TestALoadErrorIsAnEmptyStateWithARetry(t *testing.T) {
	s, _ := newShell(t)
	retried := 0
	tt := mygoui.NewTester(func(c *mygoui.Context) {
		s.viewLoadError(c, "probe:err", "关注的 UP 主", "重试加载关注的人", errors.New("网络超时"), func() { retried++ })
	}, 1200, 800)
	tt.Frame()
	for _, want := range []string{"这一块没拉出来", "没拉出来的是关注的 UP 主：网络超时", "重试加载关注的人"} {
		if !tt.HasText(want) {
			t.Errorf("失败块上没有「%s」，文字有 %q", want, tt.Texts())
		}
	}
	if err := tt.Click("重试加载关注的人"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if retried != 1 {
		t.Errorf("点了重试，重试函数跑了 %d 次，want 1", retried)
	}
}

// TestTheUnmountedPageHeadSitsWhereTheLoggedOutOneDoes 锁住稍后再看 / 历史记录在「B 站客户端没挂上」
// 时画的是那一页的标准页头（Eyebrow + 标题 + 导语），而且和未登录时的页头落在同一个位置。
//
// 曾经出错的行为：没挂上那一支只在空状态上面加了一行 20 号页面名，没有 Eyebrow 和导语，
// 位置也和未登录那一支（`pud.PageHead`）不一样，两种状态切换时页名会跳。
func TestTheUnmountedPageHeadSitsWhereTheLoggedOutOneDoes(t *testing.T) {
	for _, pg := range []struct{ path, eyebrow, lede string }{
		{laterPath, "WATCH LATER", "先存下来，等有空的时候再听。"},
		{historyPath, "HISTORY", ""},
	} {
		t.Run(pg.path, func(t *testing.T) {
			heads := map[string]mygoui.Rect{}
			for _, mount := range []bool{false, true} {
				opts := []shellOpt{}
				state := "没挂上"
				if mount {
					state = "未登录"
					f := newLibFake(t)
					opts = append(opts, mounted(func(t *testing.T, a *app.App) { a.MountLibrary(biliForTest(t, f)) }))
				}
				b := newTestShell(t, opts...)
				b.s.navigate(pg.path)
				waitFrames(t, b.tt, state+"的页头", func() bool { return b.tt.HasText(pg.eyebrow) })
				r, _ := b.tt.Find(pg.eyebrow)
				heads[state] = r
				if pg.lede != "" && !b.tt.HasText(pg.lede) {
					t.Errorf("%s时没有页头导语「%s」，文字有 %q", state, pg.lede, b.tt.Texts())
				}
				if !mount && !b.tt.HasText(biliNotMounted) {
					t.Errorf("没挂上时没有说原因，文字有 %q", b.tt.Texts())
				}
			}
			if a, b := heads["没挂上"], heads["未登录"]; a.X != b.X || a.Y != b.Y {
				t.Errorf("页头 Eyebrow 没挂上时在 %v，未登录时在 %v，应该同一个位置", a, b)
			}
		})
	}
}
