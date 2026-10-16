package ui

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// searchValues 把一份历史压成几个词，断言和报错的时候好读。
func searchValues(items []searchItem) []string {
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = it.Value
	}
	return out
}

// TestSearchHistoryKeepsTheNewestFirst 验记一条的规矩：Biu 的 add 那一支
// （`store/search-history.ts:20-27`）——去重、把新的顶到最前。
func TestSearchHistoryKeepsTheNewestFirst(t *testing.T) {
	h := newSearchHistory("")
	t.Cleanup(func() { _ = h.Close() })

	h.add("周杰伦")
	h.add("林俊杰")
	h.add("周杰伦")

	got := searchValues(h.list())
	want := []string{"周杰伦", "林俊杰"}
	if len(got) != len(want) {
		t.Fatalf("记了三次之后是 %q，want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("记了三次之后是 %q，want %q", got, want)
		}
	}
}

// TestSearchHistorySkipsBlank 验空词和纯空格不记。
//
// Biu 的 submitSearch 第一句就是 `if (!keyword?.trim()) return`
// （`layout/navbar/search/index.tsx:54`）——不然浮层上会出现一条搜不出任何
// 东西的历史。
func TestSearchHistorySkipsBlank(t *testing.T) {
	h := newSearchHistory("")
	t.Cleanup(func() { _ = h.Close() })

	h.add("")
	h.add("   ")
	h.add("\t\n")
	if got := h.list(); len(got) != 0 {
		t.Errorf("空词也记进去了：%q", searchValues(got))
	}
}

// TestSearchHistoryShowsTheNewestFew 验浮层上最多摆 searchHistoryShown 条，
// 存的比这多。Biu 是 `items.slice(0, 10)`（`search/index.tsx:141`）。
func TestSearchHistoryShowsTheNewestFew(t *testing.T) {
	h := newSearchHistory("")
	t.Cleanup(func() { _ = h.Close() })

	for i := range searchHistoryStored + 5 {
		h.add(fmt.Sprintf("搜过的 %d", i))
	}

	if got := len(h.list()); got != searchHistoryShown {
		t.Errorf("浮层上摆 %d 条，want %d", got, searchHistoryShown)
	}
	if got := len(h.items); got != searchHistoryStored {
		t.Errorf("存了 %d 条，want %d（再多就该从最旧的一头丢）", got, searchHistoryStored)
	}
	// 存的要是**最近**的那些，不是随便截一段：加了 n+5 条，留下的最旧一条是
	// 「搜过的 5」——从头数第 6 条，末尾那 n-1 条已经被丢掉了。
	if last := h.items[len(h.items)-1].Value; last != "搜过的 5" {
		t.Errorf("存的最旧一条是 %q，want「搜过的 5」", last)
	}
}

// TestSearchHistorySurvivesRestart 验「搜过 → 关掉 → 再打开还记得」。
//
// 这是这份存储存在的全部理由，也是「异步写盘 + 收尾同步落盘」那一套的全部理由。
func TestSearchHistorySurvivesRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), searchHistoryFileName)

	h := newSearchHistory(path)
	h.add("周杰伦")
	h.add("林俊杰")
	if err := h.Close(); err != nil {
		t.Fatalf("收尾落盘失败：%v", err)
	}
	// Close 可以重复调：窗口关了之后进程再退、再调一次都不出错。
	if err := h.Close(); err != nil {
		t.Fatalf("第二次 Close 出错：%v", err)
	}

	again := newSearchHistory(path)
	t.Cleanup(func() { _ = again.Close() })
	// 新的在前：最后搜的「林俊杰」排在「周杰伦」前面。
	got := searchValues(again.list())
	if len(got) != 2 || got[0] != "林俊杰" || got[1] != "周杰伦" {
		t.Errorf("重开之后是 %q，want [林俊杰 周杰伦]", got)
	}
}

// TestSearchHistoryWritesItsSchemaVersion 验落盘带 schema 号。
//
// 没有这一格就分不清这份文件是哪个版本写的，升级到一半崩溃时留下的到底是
// 新的还是旧的，只能靠猜。
func TestSearchHistoryWritesItsSchemaVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), searchHistoryFileName)
	h := newSearchHistory(path)
	h.add("周杰伦")
	if err := h.Close(); err != nil {
		t.Fatal(err)
	}

	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var f searchHistoryFile
	if err := json.Unmarshal(b, &f); err != nil {
		t.Fatalf("写出来的不是 JSON：%v（%q）", err, b)
	}
	if f.SchemaVersion != searchHistorySchema {
		t.Errorf("schema 号是 %d，want %d", f.SchemaVersion, searchHistorySchema)
	}
	if len(f.Items) != 1 || f.Items[0].Value != "周杰伦" {
		t.Errorf("写出来的是 %+v", f.Items)
	}
	if f.Items[0].Time <= 0 {
		t.Errorf("没记下时刻：%+v", f.Items[0])
	}
}

// TestSearchHistoryLeavesNoTempFiles 验原子写不留半个文件在目录里。
//
// 临时文件 + rename 是最低要求：写到一半被断电不能留下一个「看着像设置、
// 读出来是空的」文件。
func TestSearchHistoryLeavesNoTempFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, searchHistoryFileName)

	h := newSearchHistory(path)
	h.add("周杰伦")
	if err := h.Close(); err != nil {
		t.Fatal(err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Errorf("目录里留下了临时文件 %q", e.Name())
		}
	}
	if len(entries) != 1 {
		names := make([]string, len(entries))
		for i, e := range entries {
			names[i] = e.Name()
		}
		t.Errorf("目录里有 %q，want 只有 %q", names, searchHistoryFileName)
	}
}

// TestSearchHistoryBacksUpACorruptFile 验坏文件不一声不响地清掉。
//
// 处理：备份走（`<path>.bak`），接着用空的。少了这一步，用户丢了历史都不知道
// 丢了什么。
func TestSearchHistoryBacksUpACorruptFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), searchHistoryFileName)
	if err := os.WriteFile(path, []byte("这不是 JSON"), 0o644); err != nil {
		t.Fatal(err)
	}

	h := newSearchHistory(path)
	t.Cleanup(func() { _ = h.Close() })
	if got := h.list(); len(got) != 0 {
		t.Errorf("坏文件读出了 %q", searchValues(got))
	}
	if _, err := os.Stat(path + ".bak"); err != nil {
		t.Errorf("坏文件没有备份：%v", err)
	}
}

// TestSearchHistoryFromANewerVersionIsNotRewritten 验来自更新版本的文件只读。
//
// 这份文件是别的版本写的，按旧语义改一遍就毁了——和设置那边的
// ErrSettingsFromNewer 同一个道理（`internal/app/settings.go`）。
func TestSearchHistoryFromANewerVersionIsNotRewritten(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, searchHistoryFileName)
	body := []byte(`{"schemaVersion": 99, "items": [{"value": "周杰伦", "time": 1}]}`)
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatal(err)
	}
	h := newSearchHistory(path)
	t.Cleanup(func() { _ = h.Close() })
	h.add("林俊杰")
	h.clear()
	if err := h.Close(); err != nil {
		t.Fatal(err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(body) {
		t.Errorf("只读的那份被改写了：%q", got)
	}
}

// TestSearchHistoryNoFileIsNotAnError 验第一次运行（没有那个文件）是正常状态。
func TestSearchHistoryNoFileIsNotAnError(t *testing.T) {
	h := newSearchHistory(filepath.Join(t.TempDir(), "还不存在的.json"))
	t.Cleanup(func() { _ = h.Close() })
	if got := h.list(); len(got) != 0 {
		t.Errorf("没有文件却读出了 %q", searchValues(got))
	}
}

// TestSearchHistoryMemoryOnlyKeepsWorking 验挂不上盘时搜索照常能用。
//
// 拿不到应用数据目录不该挡住开窗口——搜索还能用，只是关掉之后不记得搜过什么。
func TestSearchHistoryMemoryOnlyKeepsWorking(t *testing.T) {
	h := newSearchHistory("")
	h.add("周杰伦")
	h.add("林俊杰")
	h.remove("周杰伦")
	if got := searchValues(h.list()); len(got) != 1 || got[0] != "林俊杰" {
		t.Errorf("内存里那份是 %q，want [林俊杰]", got)
	}
	h.clear()
	if got := h.list(); len(got) != 0 {
		t.Errorf("清空之后还剩 %q", searchValues(got))
	}
	if err := h.Close(); err != nil {
		t.Errorf("内存里那份收尾出错：%v", err)
	}
}

// TestSearchHistorySummaryTruncatesLongValues 验浮层上那条长关键词被截断。
//
// 限制写在浮层那一层：存下来的还是完整的一条，只是摆出来的那一小片不把浮层
// 顶变宽。
func TestSearchHistorySummaryTruncatesLongValues(t *testing.T) {
	long := strings.Repeat("很长的关键词", 20)
	got := searchHistorySummary(long)
	if len([]rune(got)) > 25 {
		t.Errorf("截断之后还有 %d 个字", len([]rune(got)))
	}
	if !strings.HasPrefix(long, strings.TrimSuffix(got, "…")) {
		t.Errorf("截断之后的前缀对不上原文：%q", got)
	}
	if short := "短"; searchHistorySummary(short) != short {
		t.Errorf("短的那条被动了：%q", searchHistorySummary(short))
	}
}
