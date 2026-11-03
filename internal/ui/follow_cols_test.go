// 「我的关注」那一页的**列数断点**和**瘦卡**的几个数（票 follow-slim）。
//
// # 为什么和首页用**不同**的断点表
//
// 首页歌卡（`list.Grid`）的封面是 140 高、横长形，Biu 那四档 1536/1280/1024/768
// → 6/5/4/3 是**歌卡**的密度。关注卡是**人像**（头像 + 名字 + 一行签名），
// 窄得多，同一宽度下排得开更多列——用户 2026-11-03 参考网易云关注页拍板，
// 要的是那种清爽瘦卡，于是这一页自己拿一张表（`followColumnsFor`）。
//
// # 表里每个断点上下各钉一个宽度
//
// 写法照 `widgets/list/grid_test.go` 的 `TestColumnsFollowTheBiuBreakpoints`：
// 断点本身和断点上方 1 像素是「刚好够到下一档」和「还没够到」的分界，
// 判成 `>=` 还是 `>` 写错了只有这一条看得出来。
package ui

import "testing"

// TestFollowColumnsFollowThePortraitBreakpoints 钉住关注页那张表：
//
//	>= 1536 → 7 列	>= 1280 → 6 列
//	>= 1024 → 5 列	>=  768 → 4 列	比 768 还窄 → 3 列
//
// 兜底（还没量出宽度）也是 3 列——和「最窄那一档」同一个数，理由同
// `widgets/list/grid.go` 的 `gridMinCols`。
func TestFollowColumnsFollowThePortraitBreakpoints(t *testing.T) {
	for _, c := range []struct {
		w    float32
		cols int
	}{
		{0, 3}, {500, 3}, {767, 3}, // 比 768 窄：3 列
		{768, 4}, {900, 4}, {1023, 4}, // 768~1023：4 列
		{1024, 5}, {1200, 5}, {1279, 5}, // 1024~1279：5 列
		{1280, 6}, {1400, 6}, {1535, 6}, // 1280~1535：6 列
		{1536, 7}, {1900, 7}, // 1536 以上：7 列
	} {
		if got := followColumnsFor(c.w); got != c.cols {
			t.Errorf("容器宽 %v 排 %d 列，want %d", c.w, got, c.cols)
		}
	}
}

// TestTheFollowCardIsTheSlimKind 钉住这张卡的几个数：
//
//   - 头像边长 **96**（上游 HeroUI 是 `h-32` = 128，但用户 2026-11-03 参考
//     网易云关注页拍板要更瘦）；
//   - 卡里那圈留白 **12**（上游是 `p-4` = 16，用户同一条要更紧凑）；
//   - 名字字号 **16**（`scale.Size16`，上游 `text-lg` = 18）；
//   - 签名 **MaxLines(1)**（上游 `line-clamp-2`，用户同一拍板改成一行）。
//
// 头像和留白是常量，直接比；字号和行数断的是**画面上量出来的盒子高**：
// 16 点那行实测约 18.9、一行签名约 19.6，18 点那行实测约 21.2、两行签名约
// 39。阈值取 20 / 25——旧值和新值差得够开。
func TestTheFollowCardIsTheSlimKind(t *testing.T) {
	if followCardSide != 96 {
		t.Errorf("头像边长是 %v，want 96（用户 2026-11-03 参考网易云关注页拍板）", followCardSide)
	}
	if followCardPad != 12 {
		t.Errorf("卡里那圈留白是 %v，want 12（用户 2026-11-03 参考网易云关注页拍板）", followCardPad)
	}

	b := newUIExplore(t)
	uiFollowTags(b, map[int]any{0: "默认分组"})
	uiFollowList(b, 2, []any{
		uiFollowUser(11, "第一个 UP"), uiFollowUser(12, "第二个 UP"),
	})
	uiFollowSearch(b, "", nil)
	uiFollowTag(b, 0, []any{})

	s, tt, _ := newSpaceShell(t, b, true, "77")
	s.navigate(followPath)
	tt.Frame()
	f := s.followPageOf()
	waitFrames(t, tt, "人到了", func() bool { return len(f.users) == 2 && f.tagStarted })
	tt.Frame()

	// 第二张卡的名字不是整行的无障碍名字（那是第一张卡的），所以 `Find`
	// 拿到的就是名字那一段文字本身。
	name, ok := tt.Find("第二个 UP")
	if !ok {
		t.Fatalf("画面上找不到名字，文字有 %q", tt.Texts())
	}
	if name.H > 20 {
		t.Errorf("名字那一段 %.1f 高——18 点那一档是 21.2，16 点该是约 18.9（用户要 Size16）", name.H)
	}
	sign, ok := tt.Find("第二个 UP 的签名")
	if !ok {
		t.Fatalf("画面上找不到签名，文字有 %q", tt.Texts())
	}
	if sign.H > 25 {
		t.Errorf("签名那一段 %.1f 高——MaxLines(2) 下是约 39，MaxLines(1) 该是约 19.6（用户要一行）", sign.H)
	}
}
