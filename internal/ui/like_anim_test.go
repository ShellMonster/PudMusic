// 赞键那一下的两层动效：图标层先弹，**背景层晚 0.1s 才长出来**。
//
// # 为什么量的是**画面上的像素**，而不是「这一帧的倍数」
//
// 原来那一族量的是「背景层这一帧缩到多少」（`s.like` 里那几个数）：它们是画面上
// 那一帧真正用的倍数。而背景层现在走 `AnimatePose`（缩放和不透明度合成一个
// pose，见 `like.go` 的 `likeButton`），**pose 的当前值没有公开的读法**
// （`mygo/ui/pose.go`：`AnimatePose` 返回的是元素本身，不是值）——所以那一族
// 改成从**画面上**量。
//
// # 量的是「图标那一块之外变了几个像素」
//
// 赞键一按下去，**图标自己也在变**（颜色从灰变主色、还弹一下），而它就压在
// 背景层的正中间——所以「变了的那一片」分不出是图标还是背景层。把图标那一块
// 排除之后，剩下的就是背景层。
//
// ⚠️ **基线要取「指针已经停在赞键上」的那一帧**：赞键有悬停底色，而它一出现，
// 整格都变了——基线里没有它的话，那一格的变化全是悬停底色。
//
// # 0.1s 这个数从哪来
//
// 《制作点赞与投票动画》榜单 v3：「爱心与圆形背景分别做 0.7s 的 scale 动画，
// **背景晚 0.1s 开始**」。
package ui

import (
	"image"
	"math"
	"testing"
	"time"

	mygoui "github.com/egoist/mygo/ui"
)

// likeIconRadius 是「图标那一块」有多大（像素）。
//
// ⚠️ **13 是「图标那一块」的半径**：图标是 18 像素（`icons.go` 的 `iconMd`），
// 弹起来是 1.25 倍 ⇒ 半径约 11.25，加上抗锯齿那一圈到 12~14。
// 背景层比它大的那一圈（满大时半径 16）就数得出来了。
const likeIconRadius = 13

// likeDiscGrowth 是「赞键那一圈背景」长出来多少。
type likeDiscGrowth struct {
	// count 是**图标那一块之外**变了几个像素。
	//
	// 背景层还没开始的时候，这个数是 0（那一块之外什么都没有动）；
	// 它长起来之后是几百（那一圈是一大盘）。
	count int
	// radius 是变了的最远那一个离赞键中心多远（像素）。
	//
	// 背景层满大时是 `iconHitMd/2`（16）。
	radius int
}

// likeDiscMeasure 量「赞键那一圈背景」相对基线长了多少。
//
// base 是「指针已经停在赞键上、还没赞」的那一帧。
func likeDiscMeasure(base, img *image.RGBA, box mygoui.Rect) likeDiscGrowth {
	var g likeDiscGrowth
	cx := float64(box.X) + float64(box.W)/2
	cy := float64(box.Y) + float64(box.H)/2
	x0, y0 := int(box.X)-2, int(box.Y)-2
	x1, y1 := int(box.X+box.W)+2, int(box.Y+box.H)+2
	if x1 > img.Bounds().Dx() {
		x1 = img.Bounds().Dx()
	}
	if y1 > img.Bounds().Dy() {
		y1 = img.Bounds().Dy()
	}
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			if base.RGBAAt(x, y) == img.RGBAAt(x, y) {
				continue
			}
			dx, dy := float64(x)+0.5-cx, float64(y)+0.5-cy
			r := int(math.Sqrt(dx*dx+dy*dy) + 0.5)
			if r > g.radius {
				g.radius = r
			}
			if r > likeIconRadius {
				g.count++
			}
		}
	}
	return g
}

// likeDiscEnv 造一个「指针停在赞键上、还没赞」的现场，返回基线那一帧。
func likeDiscEnv(t *testing.T) (*likeEnv, mygoui.Rect, *image.RGBA) {
	t.Helper()
	e := newLikeEnv(t, false, false, false)
	e.tt.Frame()
	box, ok := e.tt.Find("赞")
	if !ok {
		t.Fatalf("播放条上没有赞键，文字有 %q", e.tt.Texts())
	}
	// ⚠️ **指针要先停在赞键上**：赞键有悬停底色，基线里没有它的话，那一格
	// 的变化全是悬停底色（见文件头）。
	e.tt.Move(box.X+box.W/2, box.Y+box.H/2)
	e.tt.Frame()
	return e, box, e.tt.Image()
}

// TestTheLikeIconPopsBeforeTheBackgroundGrows 钉住「背景层比图标层晚 0.1s」。
//
// 这条用例量的就是那句话：**图标层已经在弹了，背景层还没动**。
func TestTheLikeIconPopsBeforeTheBackgroundGrows(t *testing.T) {
	e, box, base := likeDiscEnv(t)

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
	if e.s.likeBtn.iconScale <= 1 {
		t.Errorf("赞出去 50ms 之后图标层是 %v，want > 1（它该先弹起来）", e.s.likeBtn.iconScale)
	}

	// 背景层：这时候**还没动**——它晚 0.1s 才开始，所以图标那一块之外一个
	// 像素都还没变（实测：这时候只有图标自己的抗锯齿边，几十个；背景层长起来
	// 之后是几百个）。
	if g := likeDiscMeasure(base, e.tt.Image(), box); g.count > 80 {
		t.Errorf("赞出去 50ms 之后图标那一块之外已经变了 %d 个像素——"+
			"背景层该晚 0.1s 才开始", g.count)
	}

	// 推过那 0.1s：背景层开始长了。
	e.tt.Advance(150 * time.Millisecond)
	e.tt.Frame()
	g := likeDiscMeasure(base, e.tt.Image(), box)
	if g.count < 150 {
		t.Errorf("赞出去 200ms 之后图标那一块之外只变了 %d 个像素——"+
			"背景层该开始长了", g.count)
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
//
// ⚠️ **它现在量的是 pose 的**目标**：缩放走 `AnimatePose` 之后，那条通道的**当前
// 值没有公开的读法**，而「动画从 0.5 起步」这件事**正是目标值**（`Scale` 的
// 目标就是 0.5，动画从当前值走向它）。
func TestTheLikeBackgroundNeverScalesBelowAHalf(t *testing.T) {
	// 没赞的时候，背景层就在那儿（半大）。
	if got := likeBgScaleTarget(false); got != likeBgRest {
		t.Errorf("没赞的时候背景层缩到 %v，want %v", got, likeBgRest)
	}
	if likeBgRest < 0.5 {
		t.Errorf("背景层那一档是 %v，want >= 0.5（手册 ⓪ ②）", likeBgRest)
	}
}

// TestTheLikeBackgroundGrowsToFullSize 钉住「赞了之后背景层是满大的」。
func TestTheLikeBackgroundGrowsToFullSize(t *testing.T) {
	e, box, base := likeDiscEnv(t)

	if err := e.tt.Click("赞"); err != nil {
		t.Fatalf("点不了赞键：%v（文字：%q）", err, e.tt.Texts())
	}
	e.tt.Frame()

	// 推够久：延迟 0.1s + 长出来 0.26s + 一点余量。
	e.tt.Advance(600 * time.Millisecond)
	e.tt.Frame()
	g := likeDiscMeasure(base, e.tt.Image(), box)
	// 满大就是 `iconHitMd` 那一格（半径一半）；边缘那一像素是抗锯齿，允许差一。
	want := int(iconHitMd / 2)
	if g.radius < want-1 || g.radius > want+1 {
		t.Errorf("赞出去 600ms 之后背景层长到半径 %d 像素，want %d（长满了）", g.radius, want)
	}
	// 图标层弹完要回到 1。
	if e.s.likeBtn.iconScale > 1.01 {
		t.Errorf("赞出去 600ms 之后图标层是 %v，want 1（弹完该回去了）", e.s.likeBtn.iconScale)
	}
}
