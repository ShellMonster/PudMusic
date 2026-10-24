// 票 #416 的画面验收：UP 空间那一页的几张 Tab 的版式。
//
//	BIU_SHOT_DIR=/some/dir go test ./internal/ui/ -run TestRenderUpSpaceTabShots
//
// # 深浅两套都要出
//
// 这一族看的是**底色上的一道线**（动态条目那条下边框）和**底色之间的一档**
// （悬停那一层）——深色下那两样和浅色下差得远，只出浅色的话深色下糊成一片。
//
// # 主题方案也要各看一眼
//
// 这一族里没有写死的颜色：下边框是 `t.Border`，悬停那一层是 `t.SurfaceHover`，
// 底色是 `t.Background`——而方案压过底色、描边（`t.Border = text1 @ borderA`）
// 和 `SurfaceHover`（`surface2` 往字色推 4%）。**经典那一套过了不等于方案那一套
// 也过了**（暖纸的底是暖的、悬停那一层是淡棕——那条棕带就是在那一档上冒出来的）。
//
// ⚠️ **一个字都不许打真 B 站接口**：全是内存夹具 + 无头渲染。
package ui

import (
	"net/http"
	"os"
	"testing"

	"github.com/ShellMonster/Biu_Go/internal/app"
	mygoui "github.com/egoist/mygo/ui"
)

// upTabShotFixture 装一份假 B 站：一条纯文字动态 + 一条投稿型动态。
//
// ⚠️ **两条**：只有一条的时候看不出「条目之间那 8」（上游的 `mb-2`），而这一族
// 要看的就是条与条之间怎么排。
func upTabShotFixture(t *testing.T) *uiExplore {
	t.Helper()
	b := newUIExplore(t)
	uiSpaceData(b, "Biu 的用户", 0, 123456)
	b.on("/x/polymer/web-dynamic/v1/feed/space", func(w http.ResponseWriter, r *http.Request) {
		uiExploreJSON(w, 0, map[string]any{"items": []any{
			uiDynamicWithFace("1", "这一条是纯文字动态，用来看通栏、下边框和正文的左右留白。",
				"https://f/author-11.jpg"),
			uiDynamicArchive("2", "BV1xx"),
		}, "offset": "", "has_more": false})
	})
	b.on("/x/polymer/web-dynamic/v1/feed/all", func(w http.ResponseWriter, r *http.Request) {
		uiExploreJSON(w, 0, map[string]any{"items": []any{
			uiDynamicWithFace("1", "这一条是纯文字动态，用来看通栏、下边框和正文的左右留白。",
				"https://f/author-11.jpg"),
			uiDynamicArchive("2", "BV1xx"),
		}, "has_more": false})
	})
	b.on("/x/relation/followings", func(w http.ResponseWriter, r *http.Request) {
		uiExploreJSON(w, 0, map[string]any{"total": 2, "list": []any{
			map[string]any{"mid": 11, "uname": "第一个 UP", "face": "https://f/author-11.jpg"},
			map[string]any{"mid": 12, "uname": "第二个 UP", "face": "https://f/author-12.jpg"},
		}})
	})
	return b
}

// upTabShotShell 造一个停在 UP 空间「动态」那一页的壳。
func upTabShotShell(t *testing.T, dark bool, preset app.ThemePreset) (*Shell, *mygoui.Tester) {
	t.Helper()
	st := app.DefaultSettings("")
	st.ThemePreset = preset
	b := upTabShotFixture(t)
	s, tt, _ := newSpaceShell(t, b, true, "77",
		systemIsDark(dark), withSettings(st),
		withPrefs(mygoui.Preferences{ReduceMotion: true, TextScale: 1}))
	openSpace(t, s, tt, 42)
	sp := s.upSpaces[42]
	waitFrames(t, tt, "动态到了", func() bool { return sp.dyn.started })
	tt.Frame()
	// 指针停在离内容区远远的地方（停在一条动态上会叠一层悬停底，而这一组要看的
	// 是「平时的样子」）。
	tt.Move(1150, 700)
	homeFrame(tt)
	return s, tt
}

// TestRenderUpSpaceTabShots 把 UP 空间那几页存成 PNG。
func TestRenderUpSpaceTabShots(t *testing.T) {
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
			// 01 经典（Biu 绿）：这一族的那一档底色。
			{
				_, tt := upTabShotShell(t, dark, app.ThemePresetClassic)
				save(t, dir, "70-UP空间-动态-经典-"+name+".png", tt)
			}
			// 02 暖纸（默认方案）：**那条棕带就是在这一档上冒出来的**
			// （`t.SurfaceHover` = `surface2` 往字色推 4%，是淡棕）。
			{
				_, tt := upTabShotShell(t, dark, app.ThemePresetWarmPaper)
				save(t, dir, "71-UP空间-动态-暖纸-"+name+".png", tt)
			}
			// 03 暮光·琥珀：另一套色相。
			{
				_, tt := upTabShotShell(t, dark, app.ThemePresetDuskAmber)
				save(t, dir, "72-UP空间-动态-暮光-"+name+".png", tt)
			}
			// 04 悬停：指针停在一条动态上——**底下不许再有一条棕色的带**。
			{
				_, tt := upTabShotShell(t, dark, app.ThemePresetWarmPaper)
				r, ok := tt.Find("https://f/author-11.jpg")
				if !ok {
					t.Fatalf("画面上找不到那个头像，文字有 %q", tt.Texts())
				}
				tt.Move(r.X+r.W/2, r.Y+r.H/2)
				homeFrame(tt)
				save(t, dir, "73-UP空间-动态-悬停-"+name+".png", tt)
			}
			// 05 动态流那一页：同一个组件在另一页上的样子（左右 32 而不是 16）。
			{
				st := app.DefaultSettings("")
				st.ThemePreset = app.ThemePresetWarmPaper
				b := upTabShotFixture(t)
				s, tt, _ := newSpaceShell(t, b, true, "77",
					systemIsDark(dark), withSettings(st),
					withPrefs(mygoui.Preferences{ReduceMotion: true, TextScale: 1}))
				s.navigate(dynamicFeedPath)
				tt.Frame()
				f := s.dynamicFeedOf()
				waitFrames(t, tt, "动态到了", func() bool { return len(f.items) == 2 })
				tt.Frame()
				tt.Move(1150, 700)
				homeFrame(tt)
				save(t, dir, "74-动态流-暖纸-"+name+".png", tt)
			}
		})
	}
}
