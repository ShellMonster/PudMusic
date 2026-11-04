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

// TestTheUserRowMenuIsNotGreyedOut 盯用户那一行（现在是一张卡）不再带歌的右键菜单。
func TestTheUserRowMenuIsNotGreyedOut(t *testing.T) {
	s, tt := newSearchPage(t, "周杰伦", newSearchStub(24, 24))
	settleText(t, tt, "第 1 页 第 1 首")

	if err := tt.Click(searchTabLabels[searchTabUser]); err != nil {
		t.Fatalf("点不到「用户」那个 Tab：%v", err)
	}
	waitFrames(t, tt, "用户那一页进来", loadedTo(s, searchTabUser, 1))

	// UP 主结果现在是原型的两列卡片（`pud.UCard`），没有「播放 / 不是投稿」那一份歌的右键菜单；
	// 右键点它也不能弹出歌的菜单（点一下卡 = 进他的空间，见 `TestClickingAUserCardGoesToTheirSpace`）。
	_ = tt.RightClick("打开 第 1 页 第 2 首 的空间")
	for _, item := range tt.Menu() {
		if strings.Contains(item, "不是投稿") || item == "播放" {
			t.Errorf("用户卡的右键菜单里有一句歌的菜单项：%q", item)
		}
	}
}
