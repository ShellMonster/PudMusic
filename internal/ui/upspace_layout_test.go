// UP 空间整页布局的钉子（票 #268 / BMG-268）。
//
// 这一票把 UP 空间那一页的骨架按 B 站 UP 主页那张参考图重排了：头图区一行、
// Tab 条在一块浅色圆角容器里、动态条目左边头像右边赞与「⋯」、稿件卡横排。
//
// **判据一律是画面上的位置与尺寸**（AGENTS.md §八：断画面，不断函数调用）——
// 位置变了、尺寸变了、某个东西不见了，这里就红。
package ui

import (
	"net/http"
	"testing"
)

// ─────────────────────────────────────────────────────────────────────────────
// 头图区
// ─────────────────────────────────────────────────────────────────────────────

// TestTheHeaderPutsTheNameTheBadgeAndTheSignInOneRow 钉住头图区那一行的排布：
// **昵称 + 认证徽章**一行，**签名**在下面一行，而「关注 + 统计」在右边。
//
// ⚠️ **认证徽章是一枚圆，不是一行字**：改之前画的是「认证：某某」那一行淡字
// （上游是昵称旁边那一枚 20 的圆，`space-info.tsx:97-101`）。
func TestTheHeaderPutsTheNameTheBadgeAndTheSignInOneRow(t *testing.T) {
	b := newUIExplore(t)
	uiSpaceData(b, "有认证的 UP", 0, 123456)
	// 认证信息：上游的 `official.role` 非 0 就是「有认证」。
	b.on("/x/space/wbi/acc/info", func(w http.ResponseWriter, r *http.Request) {
		uiExploreJSON(w, 0, map[string]any{
			"mid": 42, "name": "有认证的 UP", "sign": "这是一个签名", "level": 4,
			"face":         "https://f/face-42.jpg",
			"top_photo_v2": map[string]string{"l_200h_img": "https://f/banner.jpg"},
			"official":     map[string]any{"role": 1, "title": "哔哩哔哩音乐认证"},
		})
	})
	s, tt, _ := newSpaceShell(t, b, true, "77")
	openSpace(t, s, tt, 42)

	name, ok := tt.Find("有认证的 UP")
	if !ok {
		t.Fatalf("画面上找不到昵称，文字有 %q", tt.Texts())
	}
	badge, ok := tt.Find("哔哩哔哩音乐认证")
	if !ok {
		t.Fatalf("画面上找不到认证徽章（它的无障碍名字是那一句认证说明），文字有 %q", tt.Texts())
	}
	// 徽章在昵称**右边**：上游是 `flex items-center space-x-2`。
	if badge.X <= name.X+name.W {
		t.Errorf("认证徽章在 x=%.1f，昵称右边是 %.1f——徽章不在昵称右边", badge.X, name.X+name.W)
	}
	// 徽章是 20 的圆（上游 `h-5 w-5`）。
	if badge.W != upBadgeS || badge.H != upBadgeS {
		t.Errorf("认证徽章量出来是 %.1f×%.1f，want %d×%d", badge.W, badge.H, upBadgeS, upBadgeS)
	}
	// 签名在昵称**下面**一行。
	sign, ok := tt.Find("这是一个签名")
	if !ok {
		t.Fatalf("画面上找不到签名，文字有 %q", tt.Texts())
	}
	if sign.Y <= name.Y {
		t.Errorf("签名在 y=%.1f，昵称在 y=%.1f——签名不在昵称下面那一行", sign.Y, name.Y)
	}
	// 左边那一列与右边那一组之间的距离（上游 `space-x-8` = 32）。
	follow, ok := tt.Find("关注")
	if !ok {
		t.Fatalf("画面上找不到「关注」那一枚，文字有 %q", tt.Texts())
	}
	if got := follow.X - (badge.X + badge.W); got < 100 {
		t.Errorf("「关注」离昵称那一列只有 %.1f——右边那一组没有推到右边去", got)
	}
}

// TestTheFollowButtonIsAnOutlinedBox 钉住「关注」那一枚是**描边方块 + 加号**。
//
// 参考图（B 站 UP 主页）上就是那么一枚：描边的方块，前面一个「+」。
// 改之前是实心主色底（`ui.PrimaryButton`）。
func TestTheFollowButtonIsAnOutlinedBox(t *testing.T) {
	b := newUIExplore(t)
	uiSpaceData(b, "还没关注的 UP", 0, 123456)
	s, tt, _ := newSpaceShell(t, b, true, "77")
	openSpace(t, s, tt, 42)

	follow, ok := tt.Find("关注")
	if !ok {
		t.Fatalf("画面上找不到「关注」那一枚，文字有 %q", tt.Texts())
	}
	// 描边：那一枚的底色**不是**主色。
	img := tt.Image()
	x := int(follow.X) + 2
	y := int(follow.Y + follow.H/2)
	px := img.RGBAAt(x, y)
	if px.R > 200 && px.G < 120 && px.B < 120 {
		t.Errorf("「关注」那一枚的底色是 %v——那是实心主色，参考图上是描边方块", px)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 动态条目
// ─────────────────────────────────────────────────────────────────────────────

// TestTheDynamicItemPutsTheTimeUnderTheNameAndTheLikeOnTheRight 钉住动态条目
// 头一行的两件事：
//
//   - **左边**：头像 + 昵称，**「时间 · 动作」在昵称下面一行**；
//   - **右边**：「赞 + 数」和「⋯」。
//
// 改之前时间是**跟昵称并排**的，而赞在卡片最下面那一排。
func TestTheDynamicItemPutsTheTimeUnderTheNameAndTheLikeOnTheRight(t *testing.T) {
	b := newUIExplore(t)
	item := uiDynamicArchive("v1", "BV1play")
	setLike(item, 3482, false)
	s, tt, _ := dynamicCardShell(t, b, item)

	name, ok := tt.Find("作者v1")
	if !ok {
		t.Fatalf("画面上找不到昵称，文字有 %q", tt.Texts())
	}
	when, ok := tt.Find("3 分钟前 · 发布了音乐")
	if !ok {
		t.Fatalf("画面上找不到「时间 · 动作」那一行，文字有 %q", tt.Texts())
	}
	// 「时间 · 动作」在昵称**下面**：上沿更靠下，**左沿与昵称齐**。
	if when.Y <= name.Y {
		t.Errorf("「时间 · 动作」在 y=%.1f，昵称在 y=%.1f——它不在昵称下面那一行", when.Y, name.Y)
	}
	if when.X != name.X {
		t.Errorf("「时间 · 动作」的左沿是 %.1f，昵称是 %.1f——两行没对齐", when.X, name.X)
	}
	// 「赞 + 数」在**右边**。
	like, ok := tt.Find("赞 3482")
	if !ok {
		t.Fatalf("画面上找不到「赞 3482」，文字有 %q", tt.Texts())
	}
	if like.X <= name.X {
		t.Errorf("「赞」在 x=%.1f，昵称在 x=%.1f——赞没在右边", like.X, name.X)
	}
	// 「⋯」在赞的右边。
	more, ok := tt.Find(dynMoreLabel)
	if !ok {
		t.Fatalf("画面上找不到「⋯」，文字有 %q", tt.Texts())
	}
	if more.X <= like.X {
		t.Errorf("「⋯」在 x=%.1f，「赞」在 x=%.1f——「⋯」不在赞的右边", more.X, like.X)
	}
	_ = s
}

// TestTheMoreMenuHoldsTheMusicActions 钉住「下一首播放」「下载音频」**收在「⋯」
// 菜单里**，而「赞」留在外面。
//
// ⚠️ 这是票面点名要说明的那种（并排还是收进菜单）：**收进菜单**——上游的
// `MoreMenu` 就是这么放的（`dynamic-feed/item.tsx:145-150`）。
func TestTheMoreMenuHoldsTheMusicActions(t *testing.T) {
	_, tt, _ := dynamicCardShell(t, newUIExplore(t), uiDynamicArchive("v1", "BV1play"))

	// 菜单没开的时候那两项**不在画面上**。
	if tt.HasText("下一首播放") || tt.HasText("下载音频") {
		t.Errorf("菜单还没点开，画面上就有那两项了，文字有 %q", tt.Texts())
	}
	openDynMenu(t, tt)
	for _, want := range []string{"下一首播放", "下载音频", "在 B 站打开"} {
		if !tt.HasText(want) {
			t.Errorf("「⋯」菜单里没有 %q，文字有 %q", want, tt.Texts())
		}
	}
	// 「赞」不在菜单里——它留在外面。
	if tt.HasText("赞 12") && countExactText(tt, "赞 12") != 1 {
		t.Errorf("「赞」既在外面又在菜单里，文字有 %q", tt.Texts())
	}
}

// TestTheArchiveCardIsHorizontalWithADurationBadge 钉住稿件卡是**横排**的：
// 封面 192×128（16:9）、右下角一个时长角标，右边是标题 + 描述 + 播放量。
func TestTheArchiveCardIsHorizontalWithADurationBadge(t *testing.T) {
	_, tt, _ := dynamicCardShell(t, newUIExplore(t), uiDynamicArchive("v1", "BV1play"))

	cover, ok := tt.Find("https://f/c.jpg")
	if !ok {
		t.Fatalf("画面上找不到封面，文字有 %q", tt.Texts())
	}
	if cover.W != dynamicCoverW || cover.H != dynamicCoverH {
		t.Errorf("封面量出来是 %.1f×%.1f，want %d×%d（16:9 的横图）",
			cover.W, cover.H, dynamicCoverW, dynamicCoverH)
	}
	// 时长角标在封面的**右下角**。
	dur, ok := tt.Find("3:20")
	if !ok {
		t.Fatalf("画面上找不到时长，文字有 %q", tt.Texts())
	}
	if dur.X < cover.X || dur.X > cover.X+cover.W {
		t.Errorf("时长角标在 x=%.1f，封面是 %.1f..%.1f——角标不在封面上",
			dur.X, cover.X, cover.X+cover.W)
	}
	if dur.Y < cover.Y+cover.H/2 {
		t.Errorf("时长角标在 y=%.1f，封面是 %.1f..%.1f——角标不在封面的下半",
			dur.Y, cover.Y, cover.Y+cover.H)
	}
	// 描述在封面的**右边**（横排）。
	//
	// ⚠️ **用描述而不是标题**：标题那句同时是**那一行的无障碍名字**
	//（`ListState.Label`），而那一行的盒子是整行——量它量到的是整行的左缘。
	desc, ok := tt.Find("投稿简介")
	if !ok {
		t.Fatalf("画面上找不到描述，文字有 %q", tt.Texts())
	}
	if desc.X < cover.X+cover.W {
		t.Errorf("描述在 x=%.1f，封面右边是 %.1f——内容不在封面右边", desc.X, cover.X+cover.W)
	}
}

// TestTheHeaderShowsTheBadgeOnlyWhenThereIsOne 认证徽章**只在有认证时**画。
func TestTheHeaderShowsTheBadgeOnlyWhenThereIsOne(t *testing.T) {
	b := newUIExplore(t)
	// `official.title` 空 —— 没有认证。
	b.on("/x/space/wbi/acc/info", func(w http.ResponseWriter, r *http.Request) {
		uiExploreJSON(w, 0, map[string]any{
			"mid": 42, "name": "没有认证的 UP", "sign": "这是一个签名", "level": 4,
			"face":         "https://f/face-42.jpg",
			"top_photo_v2": map[string]string{"l_200h_img": "https://f/banner.jpg"},
			"official":     map[string]any{"role": 0, "title": ""},
		})
	})
	s, tt, _ := newSpaceShell(t, b, true, "77")
	openSpace(t, s, tt, 42)

	if _, ok := tt.Find("哔哩哔哩音乐认证"); ok {
		t.Errorf("没认证的 UP 头上画了认证徽章，文字有 %q", tt.Texts())
	}
	// 昵称还在（这一条只是断徽章）。
	if _, ok := tt.Find("没有认证的 UP"); !ok {
		t.Errorf("画面上找不到昵称，文字有 %q", tt.Texts())
	}
}
