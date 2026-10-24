package list

import "testing"

// TestShowOfCleansTheTitleAndFoldsTheExtra：显示的字是清洗过的；方括号里的歌手名挪到
// 副标题 UP 名前面；现场版记下来（画「现场」小标签）。
func TestShowOfCleansTheTitleAndFoldsTheExtra(t *testing.T) {
	got := showOf(Music{Title: "【周杰伦】【4K修复】晴天（Live）", Owner: "某 UP 主"})
	if got.title != "晴天" {
		t.Errorf("歌名是 %q，want 晴天", got.title)
	}
	if got.owner != "周杰伦 · 某 UP 主" {
		t.Errorf("副标题是 %q，want 「周杰伦 · 某 UP 主」", got.owner)
	}
	if !got.live {
		t.Error("（Live）没被认成现场版")
	}
	plain := showOf(Music{Title: "晴天", Owner: "A"})
	if plain.title != "晴天" || plain.owner != "A" || plain.live {
		t.Errorf("干净的标题被改了：%+v", plain)
	}
	if only := showOf(Music{Title: "【周杰伦】晴天"}); only.owner != "周杰伦" {
		t.Errorf("没有 UP 名时副标题该只有歌手：%q", only.owner)
	}
}
