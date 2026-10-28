package ui

// 「复制当前歌曲信息和封面」这一族的**出图**（票 #556）。
//
//	BIU_SHOT_DIR=/some/dir go test ./internal/ui/ -run TestRenderClipboardShots
//
// 出三张：
//
//	01 播放条（有歌，封面在）—— **焦点不在**封面上（日常那一档）
//	02 播放条 —— **焦点在**封面上（Tab 走过去那一档：焦点环画在封面外面 3 像素）
//	03 满屏播放器 —— 焦点在封面上
//
// # 为什么出图
//
// 这一票给封面那一层加了 `.Focusable()`，于是**焦点环会画在封面外面 3 像素**
//（框架的 `mygo/ui/paint.go` 的 `FocusRing`）。那是**画面上的变化**——而画面上的
// 变化要人眼看过（AGENTS.md §1「出图是材料，审查才是那道门」）。
//
// ⚠️ **深浅两套都要出**：焦点环是半透明主色，压在浅底和深底上深浅不一——
// 只出浅色的话，这一档在深色下没人看过。
//
// ⚠️ **不打真 B 站**：封面是本地的 `httptest` 夹具。

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/ShellMonster/PudMusic/internal/app"
	mygoui "github.com/egoist/mygo/ui"
)

// TestRenderClipboardShots 出这一族的图。
func TestRenderClipboardShots(t *testing.T) {
	dir := os.Getenv("BIU_SHOT_DIR")
	if dir == "" {
		t.Skip("设 BIU_SHOT_DIR 才会存图")
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write(coverPNG(t))
	}))
	defer srv.Close()

	for _, dark := range []bool{false, true} {
		name := "浅色"
		if dark {
			name = "深色"
		}
		t.Run(name, func(t *testing.T) {
			st := app.State{
				HasTrack: true, Title: "一首歌", Artist: "某个 UP 主",
				Duration: 3 * time.Minute, Position: 42 * time.Second,
				Rate: 1, Volume: 0.8, PlayMode: app.PlayModeLoop,
				Source: app.SourceOnline, CoverURL: srv.URL + "/cover.png",
				BiliURL: "https://www.bilibili.com/video/BV1xx411c7mD",
			}

			// 01 焦点**不在**封面上（日常那一档）。
			{
				sh, tt := copyShotShell(t, st, dark, false)
				waitCoverReady(t, sh, tt, st.CoverURL)
				save(t, dir, "01-播放条-封面-焦点不在-"+name+".png", tt)
			}

			// 02 焦点**在**封面上：焦点环画在封面外面 3 像素。
			{
				sh, tt := copyShotShell(t, st, dark, false)
				tt.SetFocused(true)
				focusCover(t, tt)
				waitCoverReady(t, sh, tt, st.CoverURL)
				save(t, dir, "02-播放条-封面-焦点在-"+name+".png", tt)
			}

			// 03 满屏播放器：焦点在封面上。
			{
				sh, tt := copyShotShell(t, st, dark, true)
				tt.SetFocused(true)
				focusCover(t, tt)
				waitCoverReady(t, sh, tt, st.CoverURL)
				save(t, dir, "03-满屏-封面-焦点在-"+name+".png", tt)
			}
		})
	}
}

// waitCoverReady 等封面真的下回来，并且画面上已经是它了。
//
// # 为什么必须等
//
// 封面是**异步**下来的（`imgcache` 后台下载 + 解码），而落点那一帧它多半
// **还没到**——存下来的就是「音符占位图」那一档，而不是这一票要审的那一张
// 真封面。实测：出图用例一开始没等封面，6 张里有 1 张存成了占位图（那一档
// 还成了「深色」那张——焦点链长短会改落点，而落点那一帧封面恰好没到）。
//
// ⚠️ **等的是条件，不是固定帧数**（AGENTS.md §2「不要用固定帧数 / 固定毫秒
// 当判据」）：机器慢一点就飘，而飘的时候和被测的逻辑一点关系都没有。
// 这里等的是缓存里那一张**真的 ready 了**（`State.Ready()`）。
//
// ⚠️ **落了条件之后还要再画一帧**：状态是上一帧进的信箱，**同一帧里画出来的
// 还是上一帧的样子**——不等这一帧，存下去的还是占位图。
func waitCoverReady(t *testing.T, sh *Shell, tt *mygoui.Tester, url string) {
	t.Helper()
	waitFrames(t, tt, "封面下回来了", func() bool {
		return sh.images.Get(url).Ready()
	})
	tt.Frame()
}

// copyShotShell 造一个只画播放条（或满屏）那一块的外壳。
func copyShotShell(t *testing.T, st app.State, dark, full bool) (*Shell, *mygoui.Tester) {
	t.Helper()
	sh := newTestShell(t, at(1200, 800), systemIsDark(dark),
		withPrefs(mygoui.Preferences{ReduceMotion: true, TextScale: 1}),
		withView(func(s *Shell) func(*mygoui.Context) {
			return func(c *mygoui.Context) {
				s.wake.set(c)
				s.applyFrameTheme(c)
				if full {
					s.viewFullCover(c, st, false, fullWindowRect(c), false)
					return
				}
				s.viewPlayBar(c, st)
			}
		}))
	return sh.s, sh.tt
}

// focusCover 用 Tab 把焦点走到封面那一层。
func focusCover(t *testing.T, tt *mygoui.Tester) {
	t.Helper()
	for range 20 {
		tt.Key(0, mygoui.KeyTab)
		tt.Frame()
		if tt.Focused(copyCoverActionsLabel) {
			return
		}
	}
	t.Fatalf("Tab 二十下都没走到封面那一层")
}
