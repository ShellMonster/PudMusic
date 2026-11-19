package mobileui

import (
	"net/url"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/service/bili"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/scale"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/tok"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/typo"
	"github.com/egoist/mygo/ui"
)

type loginPageData struct {
	started    bool
	code       *bili.Code
	bitmap     *ui.Bitmap
	openFailed bool
}

func (s *shell) leaveLogin(page *route) {
	if page.kind != pageLogin || page.data == nil {
		return
	}
	data, _ := page.data.(*loginPageData)
	if data != nil && data.started {
		s.app.CancelLogin()
	}
	page.data = nil
}

func (s *shell) login(c *ui.Context, page *route) {
	data, _ := page.data.(*loginPageData)
	if data == nil {
		data = &loginPageData{}
		page.data = data
	}
	palette := tok.Of(c.Theme())
	_, height := c.Size()
	ui.Column(c).Key("mobile-auth").FillWidth().MinHeight(min(585, max(0, height-145))).PaddingY(24).
		Gap(12).AlignItems(ui.Start).Justify(ui.Center).Children(func() {
		if !data.started {
			mobileRecordMark(c, 60)
			loginHeading(c, "登录", "继续你的音乐旅程：登录后同步收藏夹、关注和历史记录，并解锁 Hi-Res 无损音质。")
			if s.app.Account() == nil {
				settingsText(c, "当前无法申请二维码，请稍后重试。", 13, palette.Err).Role(ui.RoleStatus)
			}
			if settingsAction(c, "继续登录", true, true).Disabled(s.app.Account() == nil).Clicked() {
				data.started = true
				s.app.BeginLogin()
			}
			if loginQuietAction(c, "暂不登录").Clicked() {
				s.back()
			}
			return
		}
		snapshot := s.app.LoginState()
		if snapshot.Phase == app.LoginDone {
			mobileRecordMark(c, 60)
			created := s.app.FavoriteState(app.FavoritesCreated)
			collected := s.app.FavoriteState(app.FavoritesCollected)
			switch {
			case s.app.FavoritesRefreshing():
				loginHeading(c, "登录成功，正在同步…", "")
			case created.Err != "" || collected.Err != "":
				loginHeading(c, "同步失败", "登录已完成。请检查网络后重试。")
				if settingsAction(c, "重试", true, true).Clicked() {
					s.app.RefreshFavorites()
				}
			default:
				loginHeading(c, "登录成功", "")
			}
			if snapshot.Err != "" {
				settingsText(c, "登录状态未能保存。关闭应用后需要重新登录。", 13, palette.Warn).Role(ui.RoleStatus)
			}
			if settingsAction(c, "返回", true, true).Clicked() {
				s.back()
			}
			return
		}
		link := snapshot.ConfirmationURL()
		description := "请用另一台已登录 B 站的手机扫码。"
		if link != "" {
			description = "在本机打开 B 站确认，或用另一台设备扫码。"
		}
		loginHeading(c, "扫码登录", description)
		data.drawCode(c, snapshot.QR, snapshot.Phase == app.LoginExpired)
		message, color := snapshot.Hint, palette.Text2
		switch snapshot.Phase {
		case app.LoginWaiting:
			if snapshot.Hint == bili.ScanWaiting.String() {
				message = "正在等待扫码…"
			}
		case app.LoginScanned:
			if snapshot.Hint == bili.ScanScanned.String() {
				message = "已扫码，等待确认…"
			}
		case app.LoginExpired:
			message, color = "二维码已过期，请刷新二维码", palette.Warn
		case app.LoginFailed:
			message, color = "未能完成登录，请检查网络后刷新二维码。", palette.Err
		}
		settingsText(c, message, 14, color).Role(ui.RoleStatus)
		if link != "" {
			if data.openFailed {
				settingsText(c, "未能打开链接，请重试或使用另一台设备扫码。", 13, palette.Warn).Role(ui.RoleStatus)
			}
			if settingsAction(c, "在 B 站 App 中确认", true, true).Clicked() {
				data.openFailed = false
				c.OpenURLThen("bilibili://browser/?url="+url.QueryEscape(link), func(err error) {
					if page.data == data && s.app.LoginState().ConfirmationURL() == link {
						data.openFailed = err != nil
					}
				})
			}
		}
		if settingsAction(c, "刷新二维码", false, true).
			Disabled(snapshot.Phase == app.LoginWaiting && snapshot.QR == nil).Clicked() {
			data.openFailed = false
			s.app.RefreshLoginCode()
		}
		if loginQuietAction(c, "暂不登录").Clicked() {
			s.back()
		}
	})
}

func (data *loginPageData) drawCode(c *ui.Context, code *bili.Code, expired bool) {
	if code != data.code {
		data.code, data.bitmap = code, nil
		if code != nil && !expired {
			const quietModules = 8
			moduleSize := max(1, 136/(code.Size()+quietModules))
			data.bitmap = ui.NewBitmap(code.Image(moduleSize)).Pixelated()
		}
	}
	ui.Box(c).Key("login-qr-card").Size(160, 160).Radius(8).
		Background(ui.RGB(255, 255, 255)).AlignItems(ui.Center).Justify(ui.Center).Children(func() {
		if data.bitmap != nil && !expired {
			width, height := data.bitmap.Size()
			ui.Image(c, data.bitmap).Size(float32(width), float32(height)).Label("B 站登录二维码")
		} else if expired {
			glyph(c, "refresh", 34, ui.RGB(111, 114, 121))
		}
	})
}

func loginHeading(c *ui.Context, title, description string) {
	palette := tok.Of(c.Theme())
	ui.Column(c).Key(title).FillWidth().Gap(6).Children(func() {
		typo.Display(settingsText(c, title, 26, palette.Text), scale.Font(c, 26)).Role(ui.RoleHeading)
		if description != "" {
			settingsText(c, description, 14, palette.Text2)
		}
	})
}

func loginQuietAction(c *ui.Context, label string) ui.Element {
	return settingsAction(c, label, false, true).Background(ui.Color{})
}

func mobileRecordMark(c *ui.Context, size float32) {
	palette := tok.Of(c.Theme())
	ui.Box(c).Key("mobile-record-mark").Size(size, size).Shrink(0).Draw(func(painter *ui.Painter, rect ui.Rect) {
		centerX, centerY := rect.X+rect.W/2, rect.Y+rect.H/2
		scale := rect.W / 32
		circle := func(radius float32, color ui.Color) {
			radius *= scale
			painter.Fill(ui.Rect{X: centerX - radius, Y: centerY - radius, W: radius * 2, H: radius * 2}, color, radius)
		}
		ring := palette.Text.Mix(palette.Bg, 0.35)
		circle(16, palette.Text)
		circle(12, ring)
		circle(11.4, palette.Text)
		circle(9.5, ring)
		circle(8.9, palette.Text)
		circle(6, palette.Accent)
		circle(1.6, palette.Text)
	})
}
