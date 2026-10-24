package ui

// 切 Tab 那一段过渡的**出图**（票 #368）。
//
//	BIU_SHOT_DIR=/some/dir go test ./internal/ui/ -run TestRenderHomeTabTransitionShots
//
// # 为什么出图
//
// 这一票动的是「画面上是什么」，而逐像素的断言断不出「好不好看」——尤其断
// 不出**新旧两块叠在一起的那一帧**长什么样（那是这一票新造出来的一帧）。
// 所以存图给人眼。
//
// # 深浅两套 × 主题方案各一套
//
// 深浅两套都要出：这一段过渡是**不透明度**上的事，而深浅两套的底色差得开
// （浅色下内容是深字、深色下是亮字），淡入的那一帧在两套里差得更多。
//
// 主题方案也各出一套：三套配色（暖纸 / 暮光 / 柔雾）连着「页面底色」都
// 不一样，而这一段过渡是**在底色上淡入淡出**——底色变了，那一段的观感也
// 跟着变。
//
// # ⚠️ **推钟**：Tester 的钟站着不动（AGENTS.md §2），不推钟的话存出来的
// 全是过渡的**第一帧**（新内容还在淡入，等于一张空白的图）。

import (
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/app"
)

// TestRenderHomeTabTransitionShots 存下切 Tab 那一段过渡的几帧。
func TestRenderHomeTabTransitionShots(t *testing.T) {
	dir := os.Getenv("BIU_SHOT_DIR")
	if dir == "" {
		t.Skip("设 BIU_SHOT_DIR 才会存图")
	}

	for _, preset := range []app.ThemePreset{app.ThemePresetWarmPaper, app.ThemePresetClassic} {
		for _, dark := range []bool{false, true} {
			name := string(preset)
			if dark {
				name += "-深色"
			} else {
				name += "-浅色"
			}
			t.Run(name, func(t *testing.T) {
				st := app.DefaultSettings("")
				st.ThemePreset = preset
				s, tt := newShell(t, withSettings(st), systemIsDark(dark))
				s.home.SetSongs(fullSongs())
				homeFrame(tt)

				// 切换前。
				save(t, dir, "100-切Tab-切换前-"+name+".png", tt)

				// 点「鬼畜」：**第一帧**（旧的内容还在，新内容还没淡进来）。
				if err := tt.Click(homeTabs[1]); err != nil {
					t.Fatalf("点不到 %q 那一档：%v", homeTabs[1], err)
				}
				tt.Frame()
				save(t, dir, "101-切Tab-切换后第一帧-"+name+".png", tt)

				// 过渡中：旧的那一块淡出、新的那一块淡入，两块叠着。
				tt.Advance(60 * time.Millisecond)
				tt.Frame()
				save(t, dir, "102-切Tab-过渡中-"+name+".png", tt)

				// 走完。
				tt.Advance(300 * time.Millisecond)
				tt.Frame()
				save(t, dir, "103-切Tab-切换后-"+name+".png", tt)
			})
		}
	}
}

// TestRenderHomePopTabShots 存「切到「流行」而它还在拉」那几帧。
//
// ⚠️ **这一组是这一票的正题**：切过去之后屏幕上留着的应该是**旧的内容**，
// 而不是「正在加载」那一格——存出来给人眼看的正是「旧内容留在画面上」这件事。
//
// ⚠️ **走真路子**（假 B 站把「流行」那一档要的那一趟按在门口）：那一档
// 「在拉」这件事不是能硬塞的字段——`loading` 是取数那一层的状态，而它在
// 真的有一趟请求在路上的时候才为真。
func TestRenderHomePopTabShots(t *testing.T) {
	dir := os.Getenv("BIU_SHOT_DIR")
	if dir == "" {
		t.Skip("设 BIU_SHOT_DIR 才会存图")
	}
	b := newUIExplore(t)
	homeArchives(b)

	for _, dark := range []bool{false, true} {
		name := "浅色"
		if dark {
			name = "深色"
		}
		t.Run(name, func(t *testing.T) {
			// 把「流行」那一档（音乐榜）要的那一趟按住在门口。
			//
			// ⚠️ **每一档一套闸**：闸是「放行那一趟」的信号，而它只能放一次。
			gate := make(chan struct{})
			b.mu.Lock()
			rank := b.handlers[uiHomeMusicRank]
			b.mu.Unlock()
			b.on(uiHomeMusicRank, func(w http.ResponseWriter, r *http.Request) {
				<-gate
				rank(w, r)
			})
			t.Cleanup(func() {
				select {
				case <-gate:
				default:
					close(gate)
				}
			})

			_, tt, _ := newHomeShell(t, b, systemIsDark(dark))
			waitFrames(t, tt, "第一档拉回来了", func() bool { return showsText(tt, "音乐 推的第一首") })
			save(t, dir, "104-切流行-切换前-"+name+".png", tt)

			// 点「流行」：那一趟还在路上，屏幕上留着的应该是**旧的内容**。
			if err := tt.Click(homeTabs[homePopTab]); err != nil {
				t.Fatalf("点不到 %q 那一档：%v", homeTabs[homePopTab], err)
			}
			homeFrame(tt)
			save(t, dir, "105-切流行-那一档还在拉-"+name+".png", tt)

			// 放它过去：这一屏自己换掉（有过渡）。
			close(gate)
			waitFrames(t, tt, "「流行」那一档拉回来了", func() bool {
				return showsText(tt, "流行 榜上的歌")
			})
			tt.Advance(60 * time.Millisecond)
			tt.Frame()
			save(t, dir, "106-切流行-过渡中-"+name+".png", tt)
			tt.Advance(300 * time.Millisecond)
			tt.Frame()
			save(t, dir, "107-切流行-切换后-"+name+".png", tt)
		})
	}
}
