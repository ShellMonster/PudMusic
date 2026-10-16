package list

import (
	"bytes"
	"log"
	"os"
	"strings"
	"testing"
)

// 这一条钉的是 `guardFailed` 和它的调用方之间的那份约定：**崩过的记进 failed，
// 调用方下一帧靠它短路**。
//
// ⚠️ 为什么单拿它出来钉：`List.viewRow` 和 `Grid.viewRow` 都是「先看 failed、
// 没崩过才画」的写法（网格还有第二层 `Grid.viewCellGuarded`）。`List` 那一层是
// 能从头跑通的（`TestARowThatCannotBeDrawnDoesNotTakeTheRestDown` 连画七帧数
// 日志条数），网格的行级兜底今天还没有能从测试里走到的崩法（格子之外没有会崩
// 的东西），所以那份「不再重试」在这里钉住：把调用方的写法原样摆一遍，
// 记的东西对不对、会不会重试就都验到了。
//
// 顺带钉住 failed 是**指针**：nil 时要就地建出来。值传进去的话新表建在局部变量
// 上，调用方那份还是 nil，下一帧照样重试——上面那半个承诺就白写了。
func TestGuardFailedMarksItSoTheCallerSkipsItNextFrame(t *testing.T) {
	var buf bytes.Buffer
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })

	var failed map[string]bool // 故意是 nil：兜底那一层要能自己建出来
	draws := 0

	// 照 List.viewRow / Grid.viewRow 的写法连画七帧。
	for range 7 {
		if failed["song-1"] {
			continue
		}
		draws++
		guardFailed(&failed, "测试行", "song-1", func() { panic("这一行画崩了") })
	}

	if draws != 1 {
		t.Errorf("画了 %d 次，want 1 次（崩过的行不该每帧重试：每帧崩一次就是每帧一份"+
			"完整栈，日志被刷爆、帧率拖垮）", draws)
	}
	if failed["song-1"] != true {
		t.Errorf("崩了没有记进 failed：%v", failed)
	}
	if got := strings.Count(buf.String(), "测试行画崩了"); got != 1 {
		t.Errorf("日志里有 %d 条「测试行画崩了」，want 1 条，日志是 %q", got, buf.String())
	}
	if !strings.Contains(buf.String(), "song-1") {
		t.Errorf("日志里没有崩的是哪一首（%q），查起来没头", buf.String())
	}
}
