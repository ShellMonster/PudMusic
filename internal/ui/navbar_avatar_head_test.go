package ui

import (
	"testing"
	"time"

	"github.com/ShellMonster/PudMusic/internal/ui/widgets/tok"
	mygoui "github.com/egoist/mygo/ui"
)

// TestTheAvatarMenuHeadShowsTheNicknameAndUID 锁定原型 app.js:1007 的菜单头部：40 的头像 +
// 名字（B 站昵称，和首页问候语同一份）+ 一行 UID 小字（DESIGN §0′.T1 不用原型假数据）。
// 曾经只有「已登录 / UID」两行字、没有头像。
func TestTheAvatarMenuHeadShowsTheNicknameAndUID(t *testing.T) {
	f := newAvatarFixture(t)
	f.withALoginIn()
	_, tt := newAvatarShell(t, f, "2")
	waitFrames(t, tt, "头像图到了", func() bool { return avatarShowsThePicture(tt) })
	openAvatarMenu(t, tt)
	tt.Advance(400 * time.Millisecond)
	homeFrame(tt)

	name, ok := tt.Find("我")
	if !ok {
		t.Fatalf("菜单头部没有昵称「我」，文字有 %q", tt.Texts())
	}
	if !tt.HasText("UID 2") {
		t.Errorf("菜单头部没有 UID 那行小字，文字有 %q", tt.Texts())
	}
	if tt.HasText("已登录") {
		t.Errorf("有昵称时菜单头部还写着「已登录」")
	}
	// 头像在名字左边，缝 12：名字左边 12..52 那一格里该是那张假头像的颜色。
	face := name
	face.X, face.W = name.X-12-avatarMenuHeadSide, avatarMenuHeadSide
	face.Y, face.H = name.Y+name.H/2-avatarMenuHeadSide/2+8, avatarMenuHeadSide-16
	if n := countColorIn(tt.Image(), face, avatarImageColor); n < int(face.W*face.H)/3 {
		t.Errorf("名字左边那 40 见方里假头像的颜色只有 %d 个像素——头部没画头像", n)
	}
}

// TestTheAvatarRingSitsOutsideTheButton 锁定原型 index.html:256-258：外环是 36 按钮外面的
// 2 像素（直径 36 → 40），和 32 的头像之间空 2。曾经环画在 36 以内、紧贴头像。
func TestTheAvatarRingSitsOutsideTheButton(t *testing.T) {
	f := newAvatarFixture(t)
	f.withALoginIn()
	s, tt := newAvatarShell(t, f, "2")
	waitFrames(t, tt, "头像图到了", func() bool { return avatarShowsThePicture(tt) })
	openAvatarMenu(t, tt) // 展开态：accent 环
	homeFrame(tt)

	hit, _ := avatarBox(tt)
	cx, cy := hit.X+hit.W/2, hit.Y+hit.H/2
	accent := tok.Of(BuildTheme(s.app.SettingsSnapshot(), false)).Accent
	img := tt.Image()
	// 环在半径 18..20：正上方 y = cy-19 那一点是 accent；半径 17（空的那一圈）不是。
	ring := countNear(img, rectAt(cx, cy-19), accent)
	gap := countNear(img, rectAt(cx, cy-17), accent)
	if ring == 0 {
		t.Errorf("半径 19 那一圈没有 accent 环（环该在 36 按钮外面 2 像素）")
	}
	if gap != 0 {
		t.Errorf("半径 17 那一圈也是 accent——环贴着头像了，原型中间空 2")
	}
}

// rectAt 是以 (x, y) 为中心的 1×1 一格。
func rectAt(x, y float32) mygoui.Rect { return mygoui.Rect{X: x - 0.5, Y: y - 0.5, W: 1, H: 1} }
