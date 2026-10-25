package ui

// 播放条上那几个**状态**的出图：深浅各一套。
//
//	BIU_SHOT_DIR=/some/dir go test ./internal/ui/ -run TestRenderPlayBarStateShots
//
// 这一族是票 #222 的「深浅两套出图」那一项：没歌、没封面、加载中、超一小时、
// 歌名可点——每一档都要看得出「这一档和上一档不一样」。

import (
	"os"
	"testing"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/app"
)

func TestRenderPlayBarStateShots(t *testing.T) {
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
			// 01 **没歌**：左边那一块**没有封面占位**（上游整块不画）。
			empty := playBarTester(t, app.State{}, 1200, dark)
			save(t, dir, "01-播放条-没歌-"+name+".png", empty)

			// 02 **本地文件**：封面那一格是一枚**音符**（上游的
			// `emptyPlaceholder={<RiMusic2Line />}`），而右边那一组**没有
			// 「下载」**（上游的 `source !== "local"`）。
			local := playBarTester(t, app.State{
				HasTrack: true, Title: "本地的一首歌", Source: app.SourceLocal,
				Duration: 3 * time.Minute, Position: 42 * time.Second,
				Rate: 1, Volume: 0.8, PlayMode: app.PlayModeLoop,
			}, 1200, dark)
			save(t, dir, "02-播放条-本地文件-"+name+".png", local)

			// 03 **封面加载中**：那一格压着一枚**更淡的音符**（不是一块空白）。
			//
			// ⚠️ **服务器卡着不回**：那才是「正在加载」那一档（请求发出去了，
			// 图还没到）。真去拉一个真 B 站的地址是**不许的**（钥匙串里是用户
			// 真实的登录态）。
			g := newGatedCover(t)
			loading := playBarTester(t, app.State{
				HasTrack: true, Title: "在线的一首歌", Source: app.SourceOnline,
				CoverURL: g.url(),
				Rate:     1, Volume: 0.8, PlayMode: app.PlayModeLoop,
			}, 1200, dark)
			save(t, dir, "03-播放条-封面加载中-"+name+".png", loading)

			// 04 **超一小时**：读数写成 `hh:mm:ss`（上游的 `formatDuration`）。
			long := playBarTester(t, app.State{
				HasTrack: true, Title: "一小时以上的一首", Source: app.SourceOnline,
				BiliURL:  "https://www.bilibili.com/video/BV1xx411c7mD",
				Duration: 75 * time.Minute, Position: 90 * time.Minute,
				Rate: 1, Volume: 0.8, PlayMode: app.PlayModeLoop,
			}, 1200, dark)
			save(t, dir, "04-播放条-超一小时-"+name+".png", long)

			// 05 **歌名悬停**：那一句底下长出一道**下划线**（上游的
			// `hover:underline`）。
			//
			// ⚠️ **用的是 `playBarTester`**：它建壳的时候就把深浅设好，而
			// 「建好之后再 `SetDark`」那一手在 Tester 里**不重画**（画面上还是
			// 浅色那一版）。
			hovered := playBarTester(t, app.State{
				HasTrack: true, Title: "一首歌", Source: app.SourceOnline,
				BiliURL:  "https://www.bilibili.com/video/BV1xx411c7mD",
				Duration: 3 * time.Minute, Position: 42 * time.Second,
				Rate: 1, Volume: 0.8, PlayMode: app.PlayModeLoop,
			}, 1200, dark)
			if title, ok := hovered.Find("一首歌"); ok {
				hovered.Move(title.X+title.W/2, title.Y+title.H/2)
				hovered.Frame()
			}
			save(t, dir, "05-播放条-歌名悬停-"+name+".png", hovered)

			// 06 **风控**：那一行**没有「重试」**，只有「在 B 站打开」。
			//
			// 🔴 **这一档是票 #316 的样子**：风控判成**不可重试**（立刻再问一次
			// 还是撞同一面墙，而那枚「重试」没有退避），所以播放条上不许给
			// 它——给了用户就是点一次撞一次。
			risk := playBarTester(t, app.State{
				HasTrack: true, Title: "一首歌", Source: app.SourceOnline,
				BiliURL:   "https://www.bilibili.com/video/BV1xx411c7mD",
				Err:       "被 B 站的风控拦下了，等一会儿再试",
				ErrDetail: "被 B 站的风控拦下了，等一会儿再试",
				CanRetry:  false,
				Duration:  3 * time.Minute, Position: 42 * time.Second,
				Rate: 1, Volume: 0.8, PlayMode: app.PlayModeLoop,
			}, 1200, dark)
			save(t, dir, "06-播放条-风控-不给重试-"+name+".png", risk)
		})
	}
}
