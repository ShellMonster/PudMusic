package ui

// 这组测试盯的是「打开文件」入口的一条不变量：**用户选了几首，队列里就得有几首。**
//
// 出过的问题：文件框开了多选，可这个入口只把 paths[0] 交给 Open——用户选 5 首
// 只听见 1 首，其余 4 首静默消失，界面上还看不出任何异样，歌单也还是原来那份。
// 同一个文件框在播放条的「加歌」上是遍历全部 paths 的，只有这里漏了。

import (
	"path/filepath"
	"testing"

	"github.com/ShellMonster/Biu_Go/internal/app"
)

func TestEveryPickedPathLandsInTheQueue(t *testing.T) {
	a := app.New()
	t.Cleanup(a.Close)
	s := &Shell{app: a}

	dir := t.TempDir()
	picked := []string{
		filepath.Join(dir, "one.m4a"),
		filepath.Join(dir, "two.flac"),
		filepath.Join(dir, "three.m4s"),
	}

	s.enqueuePicked(picked)

	// 故意不给真实音频：这几条打不打得开是播放的事，这里要盯的只是
	// 「有没有被丢掉」。Enqueue 无论如何都先把它们全排上。
	got := a.Queue().Items()
	if len(got) != len(picked) {
		t.Fatalf("选了 %d 首，队列里只有 %d 项：%v", len(picked), len(got), got)
	}
	for i, item := range got {
		if item.Path != picked[i] {
			t.Errorf("队列第 %d 项是 %q，want %q（顺序也要和用户选的一致）", i, item.Path, picked[i])
		}
	}
}

// TestPickingNothingQueuesNothing 是取消选择的边界：空选择不该凭空多出一项。
func TestPickingNothingQueuesNothing(t *testing.T) {
	a := app.New()
	t.Cleanup(a.Close)
	s := &Shell{app: a}

	s.enqueuePicked(nil)

	if n := a.Queue().Len(); n != 0 {
		t.Errorf("没选文件却排了 %d 项进队列", n)
	}
	if st := a.Snapshot(); st.HasTrack {
		t.Error("没选文件却有歌在放")
	}
}

// TestPickedFilesBecomeQueueItems 盯住包成队列项的那一步：路径原样带上，标题是
// 去掉了扩展名的文件名（播放条上显示的就是它）。
func TestPickedFilesBecomeQueueItems(t *testing.T) {
	a := app.New()
	t.Cleanup(a.Close)
	s := &Shell{app: a}

	dir := t.TempDir()
	s.enqueuePicked([]string{filepath.Join(dir, "夜空中最亮的星.m4a")})

	item, ok := a.Queue().Current()
	if !ok {
		t.Fatal("队列里没有当前项")
	}
	if item.Source != "local" || item.Type != "local" {
		t.Errorf("本地文件的 Source/Type = %q/%q，want local/local", item.Source, item.Type)
	}
	if want := "夜空中最亮的星"; item.Title != want {
		t.Errorf("标题 = %q，want %q", item.Title, want)
	}
}
