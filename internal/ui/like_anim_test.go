// 赞键那一下的两层动效：图标层先弹，**背景层晚 0.1s 才长出来**。
//
// # 为什么量的是「这一帧的倍数」而不是画面上的像素
//
// 背景层是主色很淡的一盘（18%），而它「长出来」的那一段只有半秒——量像素量的是
// 「有没有一丁点颜色变了」，机器一忙就飘。所以这里量的是**那一层这一帧缩到多少**
// （`s.like` 里那三个数，见 `like.go`）：它们是画面上那一帧真正用的倍数。
//
// # 0.1s 这个数从哪来
//
// 《制作点赞与投票动画》榜单 v3：「爱心与圆形背景分别做 0.7s 的 scale 动画，
// **背景晚 0.1s 开始**」。
package ui

import (
	"testing"
	"time"
)

// TestTheLikeIconPopsBeforeTheBackgroundGrows 钉住「背景层比图标层晚 0.1s」。
//
// 这条用例量的就是那句话：**图标层已经在弹了，背景层还没动**。
func TestTheLikeIconPopsBeforeTheBackgroundGrows(t *testing.T) {
	e := newLikeEnv(t, false, false, false)
	if _, ok := e.tt.Find("赞"); !ok {
		t.Fatalf("播放条上没有赞键，文字有 %q", e.tt.Texts())
	}
	if err := e.tt.Click("赞"); err != nil {
		t.Fatalf("点不了赞键：%v（文字：%q）", err, e.tt.Texts())
	}
	// ⚠️ **点击是建树的时候才被消费的**（`Clicked()` 问的是这一帧的状态），
	// 所以要点完再画一帧，那一帧才是「赞出去了」的那一帧。
	e.tt.Frame()

	// 图标层：从这一帧起就在弹（往 1.25 走）。
	//
	// ⚠️ **别急着断它已经动了**：目标变了的那一帧，倍数还是 1（动画从那一刻才
	// 开始走）——所以要看的是**推一点钟之后**。
	e.tt.Advance(50 * time.Millisecond)
	e.tt.Frame()
	if e.s.like.iconScale <= 1 {
		t.Errorf("赞出去 50ms 之后图标层是 %v，want > 1（它该先弹起来）", e.s.like.iconScale)
	}

	// 背景层：这时候**还没动**——它晚 0.1s 才开始。
	if e.s.like.bgScale != likeBgRest {
		t.Errorf("赞出去 50ms 之后背景层缩到 %v，want %v（它还没开始长）",
			e.s.like.bgScale, likeBgRest)
	}
	if e.s.like.bgAlpha != 0 {
		t.Errorf("赞出去 50ms 之后背景层的不透明度是 %v，want 0（它还没开始长）", e.s.like.bgAlpha)
	}

	// 推过那 0.1s：背景层开始长了。
	e.tt.Advance(150 * time.Millisecond)
	e.tt.Frame()
	if e.s.like.bgScale <= likeBgRest {
		t.Errorf("赞出去 200ms 之后背景层缩到 %v，want > %v（它该开始长了）",
			e.s.like.bgScale, likeBgRest)
	}
	if e.s.like.bgAlpha <= 0 {
		t.Errorf("赞出去 200ms 之后背景层的不透明度是 %v，want > 0（它该开始长了）", e.s.like.bgAlpha)
	}
}

// TestTheLikeBackgroundNeverScalesBelowAHalf 钉住手册 ⓪ ② 那一档。
//
// 「伴随不透明度的缩放动画…不要以 0 作为开始和结束的值，一般保持在 0.5 以上」
// ——用户很容易注意到元素从一个点出现。所以背景层在「没赞」那一档是**半大**
// （`likeBgRest`），不是 0。
//
// ⚠️ **这一条量的是「没赞」那一档**：赞了之后它长到 1（满大），而缩放动画的
// **起点**是那个 0.5——长出来的那一段是从 0.5 走到 1 的。
func TestTheLikeBackgroundNeverScalesBelowAHalf(t *testing.T) {
	e := newLikeEnv(t, false, false, false)

	// 没赞的时候，背景层就在那儿（半大、不透明）。
	e.tt.Frame()
	if e.s.like.bgScale != likeBgRest {
		t.Errorf("没赞的时候背景层缩到 %v，want %v", e.s.like.bgScale, likeBgRest)
	}
	if likeBgRest < 0.5 {
		t.Errorf("背景层那一档是 %v，want >= 0.5（手册 ⓪ ②）", likeBgRest)
	}
}

// TestTheLikeBackgroundGrowsToFullSize 钉住「赞了之后背景层是满大的」。
func TestTheLikeBackgroundGrowsToFullSize(t *testing.T) {
	e := newLikeEnv(t, false, false, false)
	if err := e.tt.Click("赞"); err != nil {
		t.Fatalf("点不了赞键：%v（文字：%q）", err, e.tt.Texts())
	}
	e.tt.Frame()

	// 推够久：延迟 0.1s + 长出来 0.26s + 一点余量。
	e.tt.Advance(600 * time.Millisecond)
	e.tt.Frame()
	if e.s.like.bgScale < 0.99 {
		t.Errorf("赞出去 600ms 之后背景层缩到 %v，want 1（长满了）", e.s.like.bgScale)
	}
	if e.s.like.bgAlpha < 0.99 {
		t.Errorf("赞出去 600ms 之后背景层的不透明度是 %v，want 1（长满了）", e.s.like.bgAlpha)
	}
	// 图标层弹完要回到 1。
	if e.s.like.iconScale > 1.01 {
		t.Errorf("赞出去 600ms 之后图标层是 %v，want 1（弹完该回去了）", e.s.like.iconScale)
	}
}
