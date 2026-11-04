package ui

// 搜索页「用户」那一行的菜单：**不是歌的行不能被当成放不了的歌**。
//
// # 为什么这条要单独钉
//
// 那一行的「播放」是**进 UP 主空间**（上游 `user-card.tsx:51` 的
// `navigate("/user/" + mid)`），不是放这一首——它压根没有音频。
//
// 而「放不了」的菜单判据是照着「一首歌」写的：判成放不了的话，这一行会被
// 按成灰的（用户想进 UP 主空间进不去），菜单末尾还多一句对他毫无意义的
// 话（「这一条不是投稿，放不了。搜同名的歌再放」）。
//
// 那一行判成灰这件事**在仓库自带的用例里看不见**——它落在 `internal/ui`
// 那个包里，而验收命令默认不跑它。

import (
	"strings"
	"testing"
)

// TestTheUserRowMenuIsNotGreyedOut 盯用户那一行的菜单项还是能按的。
func TestTheUserRowMenuIsNotGreyedOut(t *testing.T) {
	s, tt := newSearchPage(t, "周杰伦", newSearchStub(24, 24))
	settleText(t, tt, "第 1 页 第 1 首")

	if err := tt.Click("用户"); err != nil {
		t.Fatalf("点不到「用户」那个 Tab：%v", err)
	}
	waitFrames(t, tt, "用户那一页进来", loadedTo(s, searchTabUser, 1))

	if err := tt.RightClick("第 1 页 第 2 首"); err != nil {
		t.Fatalf("右键点不到那一行：%v", err)
	}
	// ⚠️ **菜单项要在点之前数**：点完菜单就关了，而 `tt.Menu()` 读的是
	// **当前开着的那一份**。
	for _, item := range tt.Menu() {
		if strings.Contains(item, "不是投稿") {
			t.Errorf("用户那一行的菜单末尾多了一句对他毫无意义的话：%q", item)
		}
	}
	if err := tt.ChooseMenuItem("播放"); err != nil {
		t.Fatalf("用户那一行的「播放」被按成灰的了：%v", err)
	}
}
