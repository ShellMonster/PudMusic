// 票 #367 的画面验收：「我的关注」那一页左边那列分组按当前风格重做。
//
//	BIU_SHOT_DIR=/some/dir go test ./internal/ui/ -run TestRenderFollowPaneShots
//
// # 深浅两套都要出
//
// 这一列的选中项是**淡主色底 + 按那层底现算的字**——浅色下那层底和页面底色差得
// 很远，深色下只差十几个通道。**只看浅色的话深色下那一层根本看不出来**。
//
// # 主题方案也要各看一眼
//
// 方案压过底色 / 主色 / 圆角三项，而这一列的底色、淡底、圆角全是从主题现算的
// ——**经典那一套过了不等于方案那一套也过了**（暖纸的底是暖的、暮光是琥珀）。
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

// followPaneShotFixture 装一份假 B 站：三个分组（其中一个名字很长）+ 三个关注对象。
//
// ⚠️ **名字里带一个很长的**：分组名过长时怎么办是这一票要定的事之一，出图里
// 得看得见它是怎么处理的。
func followPaneShotFixture(t *testing.T) *uiExplore {
	t.Helper()
	b := newUIExplore(t)
	uiFollowTags(b, map[int]any{
		-10: "特别关注",
		0:   "默认分组",
		777: "常听的那些不太熟但是每首都想循环到会唱完整首的 UP 主",
	})
	uiFollowList(b, 3, []any{
		uiFollowUser(11, "第一个 UP"), uiFollowUser(12, "第二个 UP"),
		uiFollowUser(13, "第三个 UP"),
	})
	uiFollowSearch(b, "", nil)
	uiFollowTag(b, 777, []any{uiFollowUser(77, "常听的那个人")})
	return b
}

// followPaneShotShell 造一个停在「我的关注」那一页的壳。
func followPaneShotShell(t *testing.T, dark bool, preset app.ThemePreset) (*Shell, *mygoui.Tester) {
	t.Helper()
	st := app.DefaultSettings("")
	st.ThemePreset = preset
	b := followPaneShotFixture(t)
	s, tt, _ := newSpaceShell(t, b, true, "77",
		systemIsDark(dark), withSettings(st),
		withPrefs(mygoui.Preferences{ReduceMotion: true, TextScale: 1}))
	s.navigate(followPath)
	tt.Frame()
	f := s.followPageOf()
	waitFrames(t, tt, "关注的人和分组到了", func() bool {
		return len(f.users) == 3 && f.tagStarted
	})
	// 指针停在内容区：停在分组那一列上会叠一层悬停底，而那一格的颜色就不是
	// 「平时的样子」了。
	tt.Move(900, 500)
	homeFrame(tt)
	return s, tt
}

// TestRenderFollowPaneShots 把这一页左边那列存成 PNG。
func TestRenderFollowPaneShots(t *testing.T) {
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
			// 01 经典（Biu 绿）：这一列的「底子」。
			{
				_, tt := followPaneShotShell(t, dark, app.ThemePresetClassic)
				save(t, dir, "01-分组栏-经典-"+name+".png", tt)
			}
			// 02 暖纸（默认方案）：底色、淡底、圆角都是从方案来的。
			{
				_, tt := followPaneShotShell(t, dark, app.ThemePresetWarmPaper)
				save(t, dir, "02-分组栏-暖纸-"+name+".png", tt)
			}
			// 03 暮光·琥珀：另一套色相（暖纸和暮光的底色差得最远）。
			{
				_, tt := followPaneShotShell(t, dark, app.ThemePresetDuskAmber)
				save(t, dir, "03-分组栏-暮光-"+name+".png", tt)
			}
			// 07 悬停：指针停在**没选中**的那一格上。
			//
			// ⚠️ **悬停反馈要和侧栏菜单项是同一档**（压一层半透明的前景色），
			// 所以这一张要看得见那一层。
			{
				_, tt := followPaneShotShell(t, dark, app.ThemePresetWarmPaper)
				r, ok := tt.Find("特别关注(1)")
				if !ok {
					t.Fatalf("分组那一列上没有「特别关注(1)」那一格，文字有 %q", tt.Texts())
				}
				tt.Move(r.X+r.W/2, r.Y+r.H/2)
				tt.Frame()
				save(t, dir, "07-悬停-"+name+".png", tt)
			}
			// 04 三态：加载中 / 拉失败了 / 一个分组都没有。
			//
			// ⚠️ **加载中那一档要停在一半**：分组那一拉是后台发的，而假 B 站
			// 回得太快——存图的时候它已经回来了，画面上就成了「加载好了」那一档。
			// 所以这一份夹具的分组那一支**卡住**（`gate`），等图存完才放行。
			{
				b := newUIExplore(t)
				gate := make(chan struct{})
				defer close(gate)
				b.on("/x/relation/tags", func(w http.ResponseWriter, r *http.Request) {
					<-gate
					uiExploreJSON(w, 0, []any{})
				})
				uiFollowList(b, 1, []any{uiFollowUser(11, "第一个 UP")})
				uiFollowSearch(b, "", nil)
				st := app.DefaultSettings("")
				st.ThemePreset = app.ThemePresetWarmPaper
				s, tt, _ := newSpaceShell(t, b, true, "77",
					systemIsDark(dark), withSettings(st),
					withPrefs(mygoui.Preferences{ReduceMotion: true, TextScale: 1}))
				s.navigate(followPath)
				tt.Frame()
				homeFrame(tt)
				save(t, dir, "04-加载中-"+name+".png", tt)
			}
			{
				b := newUIExplore(t)
				b.on("/x/relation/tags", func(w http.ResponseWriter, r *http.Request) {
					uiExploreJSON(w, -412, nil)
				})
				uiFollowList(b, 1, []any{uiFollowUser(11, "第一个 UP")})
				uiFollowSearch(b, "", nil)
				st := app.DefaultSettings("")
				st.ThemePreset = app.ThemePresetWarmPaper
				s, tt, _ := newSpaceShell(t, b, true, "77",
					systemIsDark(dark), withSettings(st),
					withPrefs(mygoui.Preferences{ReduceMotion: true, TextScale: 1}))
				s.navigate(followPath)
				tt.Frame()
				f := s.followPageOf()
				waitFrames(t, tt, "分组拉失败了", func() bool { return f.tagErr != nil })
				homeFrame(tt)
				save(t, dir, "05-拉失败-"+name+".png", tt)
			}
			{
				b := newUIExplore(t)
				uiFollowTags(b, map[int]any{})
				uiFollowList(b, 0, []any{})
				uiFollowSearch(b, "", nil)
				st := app.DefaultSettings("")
				st.ThemePreset = app.ThemePresetWarmPaper
				s, tt, _ := newSpaceShell(t, b, true, "77",
					systemIsDark(dark), withSettings(st),
					withPrefs(mygoui.Preferences{ReduceMotion: true, TextScale: 1}))
				s.navigate(followPath)
				tt.Frame()
				f := s.followPageOf()
				waitFrames(t, tt, "分组到了（空的）", func() bool { return f.tagStarted })
				homeFrame(tt)
				save(t, dir, "06-没有分组-"+name+".png", tt)
			}
		})
	}
}
