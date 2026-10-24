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

				// 点「鬼畜」：**第一帧**（旧的那一块还在满亮，新的那一块**还没
				// 建**——票 #238：旧的先淡出、新的后进，中间不同时画两层）。
				if err := tt.Click(homeTabs[1]); err != nil {
					t.Fatalf("点不到 %q 那一档：%v", homeTabs[1], err)
				}
				tt.Frame()
				save(t, dir, "101-切Tab-切换后第一帧-"+name+".png", tt)

				// 退场中：旧的那一块在淡出（还亮着，只是不那么亮了）。
				tt.Advance(90 * time.Millisecond)
				tt.Frame()
				save(t, dir, "102-切Tab-退场中-"+name+".png", tt)

				// 进场中：旧的那一块已经没了，新的那一块在淡入。
				tt.Advance(50 * time.Millisecond)
				tt.Frame()
				save(t, dir, "103-切Tab-进场中-"+name+".png", tt)

				// 走完。
				tt.Advance(400 * time.Millisecond)
				tt.Frame()
				save(t, dir, "104-切Tab-切换后-"+name+".png", tt)
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
			save(t, dir, "110-切流行-切换前-"+name+".png", tt)

			// 点「流行」：那一趟还在路上，屏幕上留着的应该是**旧的内容**。
			if err := tt.Click(homeTabs[homePopTab]); err != nil {
				t.Fatalf("点不到 %q 那一档：%v", homeTabs[homePopTab], err)
			}
			homeFrame(tt)
			save(t, dir, "111-切流行-那一档还在拉-"+name+".png", tt)

			// 放它过去：这一屏自己换掉（有过渡）。
			close(gate)
			// ⚠️ **推着钟等**：切 Tab 那一段过渡是按钟走的（旧的先淡出、新的后
			// 淡进），而 Tester 的钟站着不动。
			waitFramesClock(t, tt, "「流行」那一档拉回来了", func() bool {
				return showsText(tt, "流行 榜上的歌")
			})
			tt.Advance(400 * time.Millisecond)
			tt.Frame()
			save(t, dir, "112-切流行-切换后-"+name+".png", tt)
		})
	}
}

// TestRenderHomePopTabWhileNewMusicLoadsShots 存「切到「流行」而**新歌速递**还
// 在拉」那几帧（票 #238 的 F3）。
//
// # 这一组是这一票的正题
//
// 「流行」自己的歌（音乐榜）到了，而**新歌速递**那一趟还在路上——此时那一档
// **照样出现**（此前它整档都不出现，屏幕上一直是上一档的内容，而用户全程没有
// 任何反馈），而**新歌速递那一块自己说它在拉**。
//
// ⚠️ **闸按在「新歌速递」那一趟上**，不是音乐榜那一趟：按在音乐榜上的话，
// 那一档自己的歌还没到，整档不出现是应该的（票 #368 的那一条仍然成立）。
func TestRenderHomePopTabWhileNewMusicLoadsShots(t *testing.T) {
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
			// 把「新歌速递」那一趟按住在门口。
			gate := make(chan struct{})
			b.mu.Lock()
			newMusic := b.handlers[uiHomeNewMusic]
			b.mu.Unlock()
			b.on(uiHomeNewMusic, func(w http.ResponseWriter, r *http.Request) {
				<-gate
				newMusic(w, r)
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

			// 点「流行」：它自己的歌到了（音乐榜那一趟没被按），新歌速递还在拉。
			if err := tt.Click(homeTabs[homePopTab]); err != nil {
				t.Fatalf("点不到 %q 那一档：%v", homeTabs[homePopTab], err)
			}
			// ⚠️ **推着钟等**：切 Tab 那一段过渡是按钟走的。
			waitFramesClock(t, tt, "「流行」那一档出现了", func() bool {
				return showsText(tt, "流行 榜上的歌")
			})
			save(t, dir, "120-切流行-新歌速递还在拉-"+name+".png", tt)

			// 放它过去：新歌速递那一块自己换掉。
			close(gate)
			waitFramesClock(t, tt, "新歌速递到了", func() bool {
				return showsText(tt, "正在加载新歌速递") == false &&
					showsText(tt, "新歌速递里那首")
			})
			save(t, dir, "121-切流行-新歌速递到了-"+name+".png", tt)
		})
	}
}
