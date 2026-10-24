// 票 #234 的画面验收：「当前在哪」统一成一种画法、搜索页少一个框、满屏齿轮归位。
//
//	BIU_SHOT_DIR=/some/dir go test ./internal/ui/ -run TestRenderCurrentStyleShots
//
// # 深浅两套都要出
//
// 这一票改的每一处都是**底色**：淡主色那一层（浅色）和中性灰那一层（深色）。
// 深色下那一层和页面底色只差十几个通道，浅色下差得很远——**只出浅色的话深色下
// 那一层根本看不出来**（审计 F-02 的截图就是这么比的）。
//
// ⚠️ **一个字都不许打真 B 站接口**：全部是内存夹具 + 无头渲染。
package ui

import (
	"net/http"
	"os"
	"testing"
)

// TestRenderCurrentStyleShots 把这一票改过的那几处存成 PNG。
func TestRenderCurrentStyleShots(t *testing.T) {
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
			// 70 搜索页：**没有页内输入框，也没有「搜索」那一行标题**——
			// 顶上第一行就是那句结果标题（24/400）。
			st := newSearchStub(2, 4)
			b := newTestShell(t, systemIsDark(dark))
			s, tt := b.s, b.tt
			st.install(s)
			s.navigate(searchPath + "?" + searchQuery + "=" + "周杰伦")
			waitFrames(t, tt, "搜索结果出来了", func() bool {
				return tt.HasText("搜索【周杰伦】的结果")
			})
			tt.Move(1180, 300)
			homeFrame(tt)
			save(t, dir, "70-搜索页-"+name+".png", tt)

			// 71 我的关注：左栏「全部关注」那一格是**淡主色底**（原来是一整块
			// 实心主色），和右边那一屏的侧栏菜单当前项是同一种画法。
			ex := newUIExplore(t)
			uiFollowTags(ex, map[int]any{-10: "特别关注", 777: "常听的"})
			uiFollowList(ex, 3, []any{
				uiFollowUser(11, "第一个 UP"), uiFollowUser(12, "第二个 UP"),
				uiFollowUser(13, "第三个 UP"),
			})
			uiFollowSearch(ex, "", nil)
			uiFollowTag(ex, 777, []any{uiFollowUser(77, "常听的那个人")})
			s2, tt2, _ := newSpaceShell(t, ex, true, "77", systemIsDark(dark))
			s2.navigate(followPath)
			tt2.Frame()
			f := s2.followPageOf()
			waitFrames(t, tt2, "关注的人到了", func() bool { return len(f.users) == 3 && f.tagStarted })
			tt2.Move(1180, 300)
			homeFrame(tt2)
			save(t, dir, "71-我的关注-"+name+".png", tt2)

			// 72 用户动态：左栏「全部」那一行是**淡主色底**（原来是一块 248×50
			// 的实心主色大块）。
			ex2 := newUIExplore(t)
			uiSpaceData(ex2, "我自己", 0, 1)
			ex2.on("/x/polymer/web-dynamic/v1/feed/all", func(w http.ResponseWriter, r *http.Request) {
				uiExploreJSON(w, 0, map[string]any{"items": []any{}, "has_more": false})
			})
			ex2.on("/x/relation/followings", func(w http.ResponseWriter, r *http.Request) {
				uiExploreJSON(w, 0, map[string]any{"total": 2, "list": []any{
					map[string]any{"mid": 11, "uname": "第一个 UP", "face": "https://f/1.jpg"},
					map[string]any{"mid": 12, "uname": "第二个 UP", "face": "https://f/2.jpg"},
				}})
			})
			s3, tt3, _ := newSpaceShell(t, ex2, true, "77", systemIsDark(dark))
			s3.navigate(dynamicFeedPath)
			tt3.Frame()
			f3 := s3.dynamicFeedOf()
			waitFrames(t, tt3, "关注的人到了", func() bool { return len(f3.authors) >= 2 })
			tt3.Move(1180, 300)
			homeFrame(tt3)
			save(t, dir, "72-用户动态-"+name+".png", tt3)

			// 73 队列抽屉：正在放的那一行是**主色当字 + 一层很淡的主色底**
			// （原来是一整行 383×64 的实心主色）。
			b4 := newTestShell(t, at(winW, winH), systemIsDark(dark))
			h := &harness{app: b4.a, tt: b4.tt, s: b4.s}
			enqueue(t, h, copiesOf(t, 3)...)
			openDrawer(t, h)
			h.tt.Move(1180, 300)
			homeFrame(h.tt)
			save(t, dir, "73-队列抽屉-"+name+".png", h.tt)

			// 74 满屏播放器：顶栏是「⌄ 歌名 ⚙」——齿轮**紧跟在歌名后面**
			// （原来它浮在窗口 47% 处，离歌名 421 像素）。
			_, tt5, _ := fullTester(t)
			openFull(t, tt5)
			settleFull(t, tt5)
			tt5.Move(1180, 300)
			homeFrame(tt5)
			save(t, dir, "74-满屏播放器-"+name+".png", tt5)
		})
	}
}
