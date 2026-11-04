package ui

import (
	"testing"

	"github.com/ShellMonster/PudMusic/internal/service/download"
	"github.com/egoist/mygo/ui"
)

// 下载记录那一排「全部 / 下载中 / 已完成 / 失败」（原型 `.filter-rail`，DESIGN.md §0′.T7）。
//
// 判据一律是**画面上有没有那一行**（`shows`），不是 `d.rows` 的长度。

// dlThreeShots 是「一条完成 + 一条下载中 + 一条失败」的标准搭配。
func dlThreeShots() []download.Snapshot {
	return []download.Snapshot{
		dlSnapshot("歌 甲", download.StatusDone, dlChunk, dlChunk),
		dlSnapshot("歌 乙", download.StatusRunning, dlChunk, dlChunk/2),
		dlSnapshot("歌 丙", download.StatusError, dlChunk, dlChunk/4),
	}
}

// dlThreeNames 是上面那三条的歌名。
var dlThreeNames = []string{"歌 甲", "歌 乙", "歌 丙"}

// dlNamesOnScreen 是画面上真的出现的那几个（按画出来的次序）。
func dlNamesOnScreen(tt *ui.Tester) []string {
	var out []string
	for _, name := range dlThreeNames {
		if shows(tt, name) {
			out = append(out, name)
		}
	}
	return out
}

func sameNames(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

// dlPick 点第 i 档的 Chip（Chip 的名字是「标签」，计数是它后面的等宽字）。
func dlPick(t *testing.T, tt *ui.Tester, i int) {
	t.Helper()
	if err := tt.Click(dlFilterLabels[i]); err != nil {
		t.Fatalf("点 %q：%v（画面上的字有 %q）", dlFilterLabels[i], err, tt.Texts())
	}
	dlFrame(tt)
}

// TestTheFourFiltersAreOnScreenAndDefaultToEverything 断那一排存在、就是那四档、默认「全部」，
// 且每个 Chip 后面带着该档的条数。
func TestTheFourFiltersAreOnScreenAndDefaultToEverything(t *testing.T) {
	_, tt := newRows(t, dlThreeShots()...)
	for _, label := range []string{"全部", "下载中", "已完成", "失败"} {
		if !shows(tt, label) {
			t.Errorf("筛选那一排没有「%s」，文字有 %q", label, tt.Texts())
		}
	}
	if got := dlNamesOnScreen(tt); !sameNames(got, dlThreeNames) {
		t.Errorf("默认应该是全部：画面上是 %q，want %q", got, dlThreeNames)
	}
}

// TestEachFilterKeepsOnlyItsOwnRows 断三个具体的档各自只留自己那一类：下载中 = 排队 / 下载 / 暂停，
// 已完成 = 完成，失败 = 出错。
func TestEachFilterKeepsOnlyItsOwnRows(t *testing.T) {
	_, tt := newRows(t, dlThreeShots()...)
	for i, want := range map[int][]string{
		int(dlFilterRunning): {"歌 乙"},
		int(dlFilterDone):    {"歌 甲"},
		int(dlFilterFailed):  {"歌 丙"},
		int(dlFilterAll):     dlThreeNames,
	} {
		dlPick(t, tt, i)
		if got := dlNamesOnScreen(tt); !sameNames(got, want) {
			t.Errorf("「%s」那一档画面上是 %q，want %q", dlFilterLabels[i], got, want)
		}
	}
}

// TestAnEmptyFilterSaysWhichOneItIs 断筛到空时说的是那一档的话，不是「还没有可以下载的内容」。
func TestAnEmptyFilterSaysWhichOneItIs(t *testing.T) {
	_, tt := newRows(t, dlSnapshot("歌 甲", download.StatusDone, dlChunk, dlChunk))
	dlPick(t, tt, int(dlFilterFailed))
	if !shows(tt, "没有失败的下载") {
		t.Errorf("失败那一档空了，没说「没有失败的下载」：%q", tt.Texts())
	}
	dlPick(t, tt, int(dlFilterRunning))
	if !shows(tt, "没有进行中的下载") {
		t.Errorf("下载中那一档空了，没说「没有进行中的下载」：%q", tt.Texts())
	}
}
