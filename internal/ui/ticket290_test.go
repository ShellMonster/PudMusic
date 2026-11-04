// 票 #290：列表页与队列抽屉一批（L-01 / L-04 / L-06 / S-10）。
//
// 四条都是「**位置**」上的事，而位置这件事在代码里看不出来——所以这一族量的是
// **画面上的坐标**：
//
//	L-01  列表页的日期筛选排贴内容区**左缘**（原来整排居中）
//	L-04  稍后再看页头右上角有一枚「清除已看完」，按它要先问一句
//	L-06  下载记录的筛选排在**卡片里面**（原来在卡片外面）
//	S-10  队列抽屉表头的条数紧跟「播放列表」（原来被推到面板最右）
//
// 出处是那份对照报告（`~/.herdr-projects/reports/Biu_MyGo/161/report.md`）的
// L-01 / L-04 / L-06 / S-10 四条，上游那一侧的坐标也在里面。
package ui

import (
	"strings"
	"testing"
)

// ─────────────────────────────────────────────────────────────────────────────
// L-01：日期筛选排贴内容区左缘
// ─────────────────────────────────────────────────────────────────────────────

// ─────────────────────────────────────────────────────────────────────────────
// L-04：稍后再看页头右上角的「清除已看完」
// ─────────────────────────────────────────────────────────────────────────────

// TestTheLaterHeaderHasAClearViewedButton 钉住「页头右上角有一枚「清除已看完」」。
//
// 上游是 `flex items-center justify-between`（`later/index.tsx:218-224`）：左边一个
// `h1`，右边一枚 110×32 的按钮。我们原来右边**整块空白**（实测 x=1020..1184）。
//
// 判据是**它在右端**：内容区的右缘是窗口宽减去页内边距（`pagePadX` = 16），
// 按钮的右沿就贴着那一条（按钮自己还有 8 的右内边距）。
func TestTheLaterHeaderHasAClearViewedButton(t *testing.T) {
	f := newLibFake(t)
	f.later[1] = shotLaterBody()
	tt, _ := libraryShell(t, f, laterPath)
	const label = "删除已看完的 1" // 原型：「删除已看完的 N」，N 是已看完的条数
	waitFrames(t, tt, "页头画出来了", func() bool { return tt.HasText(label) })

	btn, ok := tt.Find(label)
	if !ok {
		t.Fatalf("稍后再看页头右侧没有「%s」，文字有 %q", label, tt.Texts())
	}
	right := float32(tt.Image().Rect.Max.X) - 32 // 内容区右页边距（窗口 1200、内容区 968 → 32）
	if d := right - (btn.X + btn.W); d > 30 {
		t.Errorf("「清除已看完」的右沿离内容区右缘 %.0f 像素（want ≤30）——"+
			"它没有落在页头的右端", d)
	}
}

// TestTheClearViewedButtonAsksBeforeItDeletes 钉住「按它先问一句，确认了才发请求」。
//
// ⚠️ **这一下是不可恢复的批量删除**：B 站那边没有撤销（上游也是弹一道确认，
// `later/index.tsx:196-211`）。而我们这边是**共用**的那套确认框
// （`internal/ui/confirmmodal.go`）。
//
// 判据三段：按下去弹框、**一个请求都不发**、确认之后才发那一次——而且那一次
// 带的是 `viewed=true`（上游 `service/history-toview-del.ts`：删一条是
// `aid=…`，清已看完是 `viewed=true`，两个都不发的话 B 站什么都不删）。
func TestTheClearViewedButtonAsksBeforeItDeletes(t *testing.T) {
	const delPath = "/x/v2/history/toview/del"

	f := newLibFake(t)
	f.later[1] = shotLaterBody()
	tt, _ := libraryShell(t, f, laterPath)
	waitFrames(t, tt, "页头画出来了", func() bool { return tt.HasText("删除已看完的 1") })

	if err := tt.Click("删除已看完的 1"); err != nil {
		t.Fatalf("点不了「清除已看完」：%v，文字有 %q", err, tt.Texts())
	}
	tt.Frame()
	if !tt.HasText("删除已观看完的视频？") {
		t.Fatalf("按了「清除已看完」没有确认框，文字有 %q", tt.Texts())
	}
	if got := f.callsOf(delPath); got != 0 {
		t.Fatalf("还没点确认就发了 %d 次删除请求", got)
	}

	if err := tt.Click("确认删除"); err != nil {
		t.Fatalf("点不了那枚「确认删除」：%v，文字有 %q", err, tt.Texts())
	}
	waitFor(t, "删除请求发出去了", func() bool {
		tt.Frame()
		return f.callsOf(delPath) == 1
	})
	if body := f.bodyOf(delPath); !strings.Contains(body, "viewed=true") {
		t.Errorf("删除请求的体是 %q，want 里有 viewed=true", body)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// L-06：下载记录的筛选排在卡片里面
// ─────────────────────────────────────────────────────────────────────────────

// ─────────────────────────────────────────────────────────────────────────────
// S-10：队列抽屉表头的条数紧跟「播放列表」
// ─────────────────────────────────────────────────────────────────────────────
