package list

import "testing"

// TestShowOfCleansTheTitleAndFoldsTheExtra：显示的字是清洗过的；方括号里的歌手名挪到
// 副标题 UP 名前面；现场版记下来（画「现场」小标签）。
func TestShowOfCleansTheTitleAndFoldsTheExtra(t *testing.T) {
	got := ShowOf(Music{Title: "【周杰伦】【4K修复】晴天（Live）", Owner: "某 UP 主"})
	if got.Title != "晴天" {
		t.Errorf("歌名是 %q，want 晴天", got.Title)
	}
	if got.Owner != "周杰伦 · 某 UP 主" {
		t.Errorf("副标题是 %q，want 「周杰伦 · 某 UP 主」", got.Owner)
	}
	if !got.Live {
		t.Error("（Live）没被认成现场版")
	}
	plain := ShowOf(Music{Title: "晴天", Owner: "A"})
	if plain.Title != "晴天" || plain.Owner != "A" || plain.Live {
		t.Errorf("干净的标题被改了：%+v", plain)
	}
	if only := ShowOf(Music{Title: "【周杰伦】晴天"}); only.Owner != "周杰伦" {
		t.Errorf("没有 UP 名时副标题该只有歌手：%q", only.Owner)
	}
}
