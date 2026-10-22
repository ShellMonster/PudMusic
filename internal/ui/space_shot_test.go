// UP 空间 / 动态 / 关注这三页的出图。
//
//	BIU_SHOT_DIR=/some/dir go test ./internal/ui/ -run TestRenderSpaceDynamicFollowShots
//
// ⚠️ **深浅两套都要出**，理由和 `hover_shot_test.go` 开头那一段一样：深色下
// 头图、分隔线、卡片边框的对比和浅色下是两个问题。
//
// 假 B 站、壳、等状态的工具在 `upspace_test.go` / `follow_test.go` 里；
// 这里只负责把三页各摆一份、各存一张。
package ui

import (
	"net/http"
	"os"
	"testing"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/ShellMonster/Biu_Go/internal/service/bili"
	mygoui "github.com/egoist/mygo/ui"
)

// TestRenderSpaceDynamicFollowShots 把这三页存成 PNG，深浅各一套。
func TestRenderSpaceDynamicFollowShots(t *testing.T) {
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
			shotUpSpace(t, dir, name, dark)
			shotDynamicFeed(t, dir, name, dark)
			shotFollow(t, dir, name, dark)
		})
	}
}

// shotSpaceShell 是这三张图共用的一台假 B 站 + 壳。
//
// ⚠️ **`systemIsDark` 不能省**：不带它的时候「深色」那一档和浅色一模一样
// （`hover_shot_test.go` 记过这个坑）。
func shotSpaceShell(t *testing.T, b *uiExplore, dark bool, loggedIn bool, mid string) (*Shell, *mygoui.Tester) {
	t.Helper()
	sh := newTestShell(t,
		systemIsDark(dark),
		mounted(func(_ *testing.T, a *app.App) {
			transport := &app.CredentialTransport{Base: b.Server.Client().Transport, Policy: app.DefaultInjectPolicy()}
			cli := bili.NewClient(bili.Options{Transport: transport})
			cli.TestOnlyPointHostsAtFixture(b.URL)
			a.MountLogin(app.NewMemoryStore("test"), cli)
			a.MountExplore(cli)
			a.MountStreaming(app.Streaming{Client: cli, Media: b.Server.Client()})
			if loggedIn {
				a.TestOnlyAdoptLogin(mid)
			}
		}))
	return sh.s, sh.tt
}

// shotUpSpace 存 UP 空间：头像 140、头图区左右 32、统计那一段。
func shotUpSpace(t *testing.T, dir, name string, dark bool) {
	t.Helper()
	b := newUIExplore(t)
	uiSpaceData(b, "Biu 的用户", 0, 123456)
	s, tt := shotSpaceShell(t, b, dark, true, "77")
	openSpace(t, s, tt, 42)
	save(t, dir, "40-"+name+"-UP空间.png", tt)
}

// shotDynamicFeed 存动态流：左栏 256、卡片下边框、投稿封面。
func shotDynamicFeed(t *testing.T, dir, name string, dark bool) {
	t.Helper()
	b := newUIExplore(t)
	uiSpaceData(b, "我自己", 0, 1)
	b.on("/x/polymer/web-dynamic/v1/feed/all", func(w http.ResponseWriter, r *http.Request) {
		uiExploreJSON(w, 0, map[string]any{"items": []any{
			uiDynamic("1", "这一条是纯文字动态，用来看卡片的下边框和正文的左右留白。"),
			uiDynamicArchive("2", "BV1xx"),
		}, "has_more": false})
	})
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
	waitFrames(t, tt, "动态到了", func() bool { return len(f.items) == 2 && len(f.authors) == 2 })
	tt.Frame()
	save(t, dir, "41-"+name+"-动态流.png", tt)
}

// shotFollow 存关注页：名字 18 / 签名 14，以及 hover 之后升起来的操作条。
func shotFollow(t *testing.T, dir, name string, dark bool) {
	t.Helper()
	b := newUIExplore(t)
	uiFollowTags(b, map[int]any{-10: "特别关注", 0: "默认分组"})
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
	save(t, dir, "42-"+name+"-关注.png", tt)

	// hover 操作条：指针移上去，推 300ms 的钟让它升到终点。
	av, ok := tt.Find(f.users[1].Face)
	if !ok {
		t.Fatalf("第二张卡的头像没画出来，文字有 %q", tt.Texts())
	}
	tt.Move(av.X+av.W/2, av.Y+av.H/2)
	tt.Frame()
	tt.Advance(followActionBarTime)
	tt.Frame()
	save(t, dir, "43-"+name+"-关注-操作条.png", tt)
}
