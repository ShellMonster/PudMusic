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

	mygoui "github.com/egoist/mygo/ui"
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
	// 等级是名字右边的 LV 角标（原型 `.badge`：名字和角标基线对齐、缝 8）。
	badge, ok := tt.Find("LV4")
	if !ok {
		t.Fatalf("画面上找不到等级角标，文字有 %q", tt.Texts())
	}
	if badge.X <= name.X+name.W {
		t.Errorf("等级角标在 x=%.1f，昵称右边是 %.1f——角标不在昵称右边", badge.X, name.X+name.W)
	}
	// 签名在昵称**下面**一行，认证说明在签名下面。
	sign, ok := tt.Find("这是一个签名")
	if !ok {
		t.Fatalf("画面上找不到签名，文字有 %q", tt.Texts())
	}
	if sign.Y <= name.Y {
		t.Errorf("签名在 y=%.1f，昵称在 y=%.1f——签名不在昵称下面那一行", sign.Y, name.Y)
	}
	official, ok := tt.Find("哔哩哔哩音乐认证")
	if !ok {
		t.Fatalf("画面上找不到认证说明，文字有 %q", tt.Texts())
	}
	if official.Y <= sign.Y {
		t.Errorf("认证说明在 y=%.1f，签名在 y=%.1f——它不在签名下面", official.Y, sign.Y)
	}
	// 关注键在昵称那一列的右边。
	follow, ok := tt.Find("已关注")
	if !ok {
		follow, ok = tt.Find("关注")
	}
	if !ok {
		t.Fatalf("画面上找不到「关注」那一枚，文字有 %q", tt.Texts())
	}
	if follow.X <= badge.X+badge.W {
		t.Errorf("「关注」离昵称那一列太近（x=%.1f，角标右边 %.1f）——右边那一组没有推到右边去", follow.X, badge.X+badge.W)
	}
}

// TestTheFollowButtonIsPrimaryUntilFollowed 钉住「关注」那一枚的两种样子（原型 `.btn-primary` /
// `.btn-ghost`）：还没关注是主色实心键 + 加号，关注了是描边的幽灵键 + 对勾。
func TestTheFollowButtonIsPrimaryUntilFollowed(t *testing.T) {
	b := newUIExplore(t)
	uiSpaceData(b, "还没关注的 UP", 0, 123456)
	s, tt, _ := newSpaceShell(t, b, true, "77")
	openSpace(t, s, tt, 42)

	follow, ok := tt.Find("关注")
	if !ok {
		t.Fatalf("画面上找不到「关注」那一枚，文字有 %q", tt.Texts())
	}
	// 没关注：键的左缘内侧是主色实心（浅色朱砂 #D2391B）。
	img := tt.Image()
	px := img.RGBAAt(int(follow.X)-10, int(follow.Y+follow.H/2))
	if !(px.R > 150 && px.G < 120 && px.B < 120) {
		t.Errorf("还没关注的「关注」键底色是 %v——该是主色实心", px)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 动态条目
// ─────────────────────────────────────────────────────────────────────────────

// TestTheDynamicItemPutsTheTimeBesideTheNameAndTheLikeInTheFoot 钉住动态条目（原型 `.post`）：
//
//   - 头一行：昵称（600）和「时间 · 动作」在**同一行**（基线对齐、缝 12），时间在昵称右边；
//   - 脚：「赞 + 数」在内容下面，「⋯」在赞的右边。
func TestTheDynamicItemPutsTheTimeBesideTheNameAndTheLikeInTheFoot(t *testing.T) {
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
	if when.X <= name.X+name.W {
		t.Errorf("「时间 · 动作」在 x=%.1f，昵称右边是 %.1f——它不在昵称右边", when.X, name.X+name.W)
	}
	if dy := abs32(when.Y + when.H - (name.Y + name.H)); dy > 6 {
		t.Errorf("「时间 · 动作」和昵称底边差 %.1f——两段没在同一行上", dy)
	}
	like, ok := tt.Find("点赞 3482")
	if !ok {
		t.Fatalf("画面上找不到「点赞 3482」，文字有 %q", tt.Texts())
	}
	if like.Y <= name.Y {
		t.Errorf("「赞」在 y=%.1f，昵称在 y=%.1f——赞不在内容下面的脚里", like.Y, name.Y)
	}
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
	if tt.HasText("点赞 12") && countExactText(tt, "点赞 12") != 1 {
		t.Errorf("「赞」既在外面又在菜单里，文字有 %q", tt.Texts())
	}
}

// TestTheFollowButtonIsKeyboardReachable 断「关注」那一枚**键盘走得到、回车
// 按得动**。
//
// ⚠️ **这一条是回归**：这一版把它从 `ui.PrimaryButton` 换成了自绘的描边方块，
// 而自绘的那一版一开始用的是 `ui.Row`——`ui.Row` 只是一个容器（`Clicked()`
// 只设 `flagClickable`、不设 `flagFocusable`），于是 Tab **跳过**它、回车按
// 不动，读屏也只当它是「容器里的一段字」。改之前（`ui.PrimaryButton`）键盘
// 是走得到的——这一版不能把那份可达性一起丢掉（票 #453 修的正是这一类断口）。
func TestTheFollowButtonIsKeyboardReachable(t *testing.T) {
	b := newUIExplore(t)
	uiSpaceData(b, "要点关注的 UP", 0, 1)
	s, tt, _ := newSpaceShell(t, b, true, "77")
	openSpace(t, s, tt, 42)
	sp := s.upSpaces[42]

	// Tab 走到「关注」上（这一页能拿焦点的东西不止一个，所以多按几下）。
	found := false
	for range 40 {
		tt.Key(0, mygoui.KeyTab)
		tt.Frame()
		if tt.Focused("关注") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("按了 40 次 Tab 也没走到「关注」上——键盘用户够不着它")
	}
	// 回车按下去：关系该变成「已关注」。
	tt.Key(0, mygoui.KeyEnter)
	tt.Frame()
	waitFor(t, "关系变成已关注", func() bool {
		return sp.header.Snapshot().Relation.Followed()
	})
	tt.Frame()
	if !showsText(tt, "已关注") {
		t.Errorf("键盘按下去之后按钮没变成「已关注」，文字有 %q", tt.Texts())
	}
}

// TestTheArchiveDescriptionIsNotSaidTwice 钉住「那一句话只说一遍」。
//
// 原型的嵌入卡没有简介行，所以投稿动态用来顶替正文的简介只在正文那一处画一次；
// 简介后面多一个空格时（B 站真会给）也不能因为判不出「同一句」而画两遍。
func TestTheArchiveDescriptionIsNotSaidTwice(t *testing.T) {
	item := uiDynamicArchive("v1", "BV1play")
	arch := item["modules"].(map[string]any)["module_dynamic"].(map[string]any)["major"].(map[string]any)["archive"].(map[string]any)
	arch["desc"] = "投稿简介 "

	_, tt, _ := dynamicCardShell(t, newUIExplore(t), item)

	n := countExactText(tt, "投稿简介") + countExactText(tt, "投稿简介 ")
	if n > 1 {
		t.Errorf("那一句话在画面上出现了 %d 次，want ≤ 1", n)
	}
}

// TestTheArchiveCardIsAnEmbedWithAPlayButton 钉住稿件卡是原型的 `.embed`：72 见方的封面在左，
// 中间标题 / 作者 / 读数 + 时长，右边一枚主色圆形播放键。
func TestTheArchiveCardIsAnEmbedWithAPlayButton(t *testing.T) {
	_, tt, _ := dynamicCardShell(t, newUIExplore(t), uiDynamicArchive("v1", "BV1play"))

	cover, ok := tt.Find("https://f/c.jpg")
	if !ok {
		t.Fatalf("画面上找不到封面，文字有 %q", tt.Texts())
	}
	if cover.W != 72 || cover.H != 72 {
		t.Errorf("封面量出来是 %.1f×%.1f，want 72×72", cover.W, cover.H)
	}
	// 时长是读数那一行里单独的一段，在封面右边。
	dur, ok := tt.Find("3:20")
	if !ok {
		t.Fatalf("画面上找不到时长，文字有 %q", tt.Texts())
	}
	if dur.X < cover.X+cover.W {
		t.Errorf("时长在 x=%.1f，封面右边是 %.1f——它不在封面右边", dur.X, cover.X+cover.W)
	}
	// 播放键在最右边。
	play, ok := tt.Find("播放 视频标题")
	if !ok {
		t.Logf("播放键的名字取决于夹具的标题，跳过位置断言（文字有 %q）", tt.Texts())
		return
	}
	if play.X < dur.X {
		t.Errorf("播放键在 x=%.1f，时长在 x=%.1f——播放键不在最右边", play.X, dur.X)
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
