package mobileui

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/licensing"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/scale"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/tok"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/typo"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

var settingsLinks = []struct{ id, title, icon string }{
	{"general", "播放与下载", "settings"},
	{"appearance", "外观", "palette"},
	{"menu", "我的页面", "menu"},
	{"proxy", "网络代理", "globe"},
	{"about", "关于布丁音乐", "info"},
}

var mobileMenuEntries = []struct{ key, title string }{
	{"/follow", "我的关注"},
	{"/later", "稍后再看"},
	{"/history", "历史记录"},
	{"/local-music", "本地音乐"},
	{"/download-list", "下载记录"},
	{"/stats", "听歌统计"},
}

type settingsPageData struct {
	message string
	failed  bool
	proxy   proxyDraft
}

type proxyDraft struct {
	custom, testing      bool
	kind                 app.ProxyType
	host, port, user     string
	secret               string
	hostError, portError string
	result               string
	failed               bool
}

func (s *shell) settings(c *ui.Context, page *route) {
	if page.kind == pageSettings {
		s.settingsIndex(c, "")
		return
	}
	if page.title != "" {
		settingsPageHeading(c, page.title)
	}
	if page.id == "license-text" {
		text, _ := page.data.(string)
		for _, line := range strings.Split(strings.TrimSpace(text), "\n\n") {
			settingsText(c, line, 13, tok.Of(c.Theme()).Text2)
		}
		return
	}
	state, _ := page.data.(*settingsPageData)
	if state == nil {
		state = &settingsPageData{}
		page.data = state
	}
	st := s.app.SettingsSnapshot()
	readOnly := s.app.SettingsReadOnly()
	if err := s.app.TakeSettingsError(); err != nil {
		state.message, state.failed = "无法读取或保存设置，请检查应用存储。", true
	}
	if readOnly {
		settingsText(c, "此设置文件由较新的版本创建，当前只能查看。", 13, tok.Of(c.Theme()).Warn)
	}
	if state.message != "" {
		palette := tok.Of(c.Theme())
		color := palette.Text2
		if state.failed {
			color = palette.Err
		}
		settingsText(c, state.message, 13, color).Role(ui.RoleStatus)
	}
	switch page.id {
	case "general":
		s.generalSettings(c, state, st, readOnly)
	case "appearance":
		s.appearanceSettings(c, state, st, readOnly)
	case "menu":
		s.menuSettings(c, state, st, readOnly)
	case "proxy":
		s.proxySettings(c, state, st, readOnly)
	case "about":
		s.aboutSettings(c, state, st)
	case "licenses":
		s.licenseSettings(c, state)
	}
}

func (s *shell) settingsIndex(c *ui.Context, selected string) {
	settingsPageHeading(c, "设置")
	settingsPanel(c, "settings-index", func() {
		for index, link := range settingsLinks {
			if index > 0 {
				settingsDivider(c)
			}
			if !settingsLink(c, link.id, link.title, link.icon) || link.id == selected {
				continue
			}
			stack := s.stacks[s.tab]
			for position := len(stack) - 1; position >= 0; position-- {
				if stack[position].kind == pageSettings {
					s.stacks[s.tab] = stack[:position+1]
					break
				}
			}
			s.push(route{kind: pageSetting, title: link.title, id: link.id})
		}
	})
}

func (s *shell) saveSetting(state *settingsPageData, change func(*app.Settings)) bool {
	if _, err := s.app.UpdateSettings(change); err != nil {
		state.message, state.failed = "设置保存失败，请重试。", true
		return false
	}
	state.message, state.failed = "", false
	return true
}

func settingsPanel(c *ui.Context, key string, content func()) {
	ui.Column(c).Key(key).FillWidth().PaddingX(14).Radius(16).
		Background(tok.Of(c.Theme()).Surface).Children(content)
}

func settingsDivider(c *ui.Context) {
	ui.Box(c).FillWidth().Height(1).Shrink(0).Background(tok.Of(c.Theme()).Line)
}

func settingsLink(c *ui.Context, key, title, icon string) bool {
	palette := tok.Of(c.Theme())
	button := ui.ButtonBase(c).Key(key).Label(title).FillWidth().MinHeight(58).
		Padding(6, 0).Gap(12).AlignItems(ui.Center)
	button.Children(func() {
		if icon != "" {
			glyph(c, icon, 18, palette.AccentText)
		}
		settingsText(c, title, 14, palette.Text).Grow(1)
		glyph(c, "right", 14, palette.Text3)
	})
	return button.Clicked()
}

func settingsText(c *ui.Context, text string, size float32, color ui.Color) ui.Element {
	return ui.Text(c, text).FontSize(scale.Font(c, size)).TextColor(color).Wrap().MinWidth(0)
}

func settingsPageHeading(c *ui.Context, title string) {
	settingsText(c, title, 20, tok.Of(c.Theme()).Text).FontWeight(700).
		Role(ui.RoleHeading).Margin(4, 0, 10, 0)
}

func settingsGroup(c *ui.Context, title, note string, content func()) {
	palette := tok.Of(c.Theme())
	ui.Column(c).Key(title).FillWidth().Gap(10).Margin(12, 0, 0, 0).Children(func() {
		settingsText(c, title, 14, palette.Text2).FontWeight(600)
		if note != "" {
			settingsText(c, note, 12, palette.Text2)
		}
		content()
	})
}

func settingRow(c *ui.Context, key, title, note string, control func()) {
	palette := tok.Of(c.Theme())
	ui.Row(c).Key(key).FillWidth().MinHeight(56).PaddingY(6).Gap(12).AlignItems(ui.Center).Children(func() {
		ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(3).Children(func() {
			settingsText(c, title, 14, palette.Text)
			if note != "" {
				settingsText(c, note, 12, palette.Text2)
			}
		})
		if control != nil {
			control()
		}
	})
}

func settingSwitch(c *ui.Context, label string, on, disabled bool) bool {
	palette := tok.Of(c.Theme())
	button := ui.ButtonBase(c).Key(label).Label(label).Role(ui.RoleSwitch).Checked(on).
		Size(touchSize(), touchSize()).Shrink(0).Disabled(disabled)
	button.Draw(func(painter *ui.Painter, rect ui.Rect) {
		left, top := rect.X+(rect.W-44)/2, rect.Y+(rect.H-24)/2
		track, knobX := palette.LineStrong, left+3
		if on {
			track, knobX = palette.Accent, left+23
		}
		if disabled {
			track = track.Mix(palette.Surface, 0.45)
		}
		painter.Fill(ui.Rect{X: left, Y: top, W: 44, H: 24}, track, 12)
		painter.Fill(ui.Rect{X: knobX, Y: top + 3, W: 18, H: 18}, ui.RGB(255, 255, 255), 9)
	})
	return button.Clicked()
}

type settingChoice[T comparable] struct {
	value T
	label string
}

func settingSelect[T comparable](c *ui.Context, title string, value T, options []settingChoice[T], disabled bool, change func(T)) {
	labels := make([]string, 0, len(options))
	var selected string
	for _, choice := range options {
		labels = append(labels, choice.label)
		if choice.value == value {
			selected = choice.label
		}
	}
	c.Key(title)
	if ui.Select(c, &selected, labels).Label("选择" + title).MinHeight(touchSize()).MaxWidth(148).Shrink(0).Disabled(disabled).Changed() {
		for _, choice := range options {
			if choice.label == selected && choice.value != value {
				change(choice.value)
				return
			}
		}
	}
}

func settingsAction(c *ui.Context, label string, primary, wide bool) ui.Element {
	palette := tok.Of(c.Theme())
	text, fill := palette.Text2, palette.Surface2
	if primary {
		text, fill = palette.AccentInk, palette.Accent
	} else if !wide {
		fill = ui.Color{}
	}
	button := ui.ButtonBase(c).Key(label).Label(label).MinHeight(touchSize()).PaddingX(14).Shrink(0).
		Radius(22).Background(fill).AlignItems(ui.Center).Justify(ui.Center)
	if wide {
		button = button.FillWidth()
	}
	button.Children(func() { settingsText(c, label, 14, text).FontWeight(600) })
	return button
}

func (s *shell) generalSettings(c *ui.Context, state *settingsPageData, st app.Settings, readOnly bool) {
	qualities := []settingChoice[app.AudioQuality]{
		{app.AudioQualityLossless, "Hi-Res 无损"}, {app.AudioQualityDolby, "杜比全景声"},
		{app.AudioQualityHigh, "高品质"}, {app.AudioQualityMedium, "标准"}, {app.AudioQualityLow, "流畅"},
	}
	if !s.app.LoggedIn() {
		qualities = qualities[2:]
		if st.AudioQuality == app.AudioQualityLossless || st.AudioQuality == app.AudioQualityDolby {
			qualities = append([]settingChoice[app.AudioQuality]{{st.AudioQuality, "登录后可用"}}, qualities...)
		}
	}
	settingsGroup(c, "播放", "", func() {
		settingsPanel(c, "playback-settings", func() {
			settingRow(c, "audio-quality", "默认音质", "切换后会重新取流，并从当前位置继续播放", func() {
				settingSelect(c, "默认音质", st.AudioQuality, qualities, readOnly, func(value app.AudioQuality) {
					s.saveSetting(state, func(settings *app.Settings) { settings.AudioQuality = value })
				})
			})
			settingsDivider(c)
			settingRow(c, "video-quality", "视频画质", "满屏播放器里看视频时用的清晰度", func() {
				quality := st.VideoQuality
				if quality == 0 {
					quality = 64
				}
				choices := []settingChoice[int]{{120, "4K"}, {80, "1080P 高清"}, {64, "720P 准高清"}, {32, "480P 标清"}, {16, "360P 流畅"}}
				settingSelect(c, "视频画质", quality, choices, readOnly, func(value int) {
					s.saveSetting(state, func(settings *app.Settings) { settings.VideoQuality = value })
				})
			})
			settingsDivider(c)
			syncNote := "把播放进度同步到 B 站账号，换设备可以接着看"
			if !s.app.LoggedIn() {
				syncNote = "登录后可把播放进度同步到 B 站账号"
			}
			settingRow(c, "report-history", "上报本机播放记录", syncNote, func() {
				loggedIn := s.app.LoggedIn()
				if settingSwitch(c, "上报本机播放记录", loggedIn && st.ReportPlayHistory, readOnly || !loggedIn) {
					s.saveSetting(state, func(settings *app.Settings) { settings.ReportPlayHistory = !settings.ReportPlayHistory })
				}
			})
		})
	})
	settingsGroup(c, "下载", "", func() {
		settingsPanel(c, "download-settings", func() {
			settingRow(c, "download-location", "下载目录", "布丁音乐 · 应用存储", func() {
				if settingsAction(c, "管理存储", false, false).Disabled(!s.app.DownloadMounted()).Clicked() {
					s.push(route{kind: pageDownloads, title: "下载记录", id: "storage"})
				}
			})
		})
	})
}

func (s *shell) appearanceSettings(c *ui.Context, state *settingsPageData, st app.Settings, readOnly bool) {
	palette := tok.Of(c.Theme())
	settingsGroup(c, "深浅模式", "", func() {
		ui.Row(c).FillWidth().Gap(8).Children(func() {
			for _, theme := range []struct {
				value app.ThemeMode
				label string
				color ui.Color
			}{
				{app.ThemeModeLight, "浅色", ui.RGB(233, 234, 235)},
				{app.ThemeModeDark, "深色", ui.RGB(23, 24, 27)},
				{app.ThemeModeSystem, "跟随系统", ui.RGB(153, 155, 160)},
			} {
				selected := theme.value == st.ThemeMode
				background, border := palette.Surface, ui.Color{}
				if selected {
					background, border = palette.AccentSoft, palette.Accent
				}
				button := ui.ButtonBase(c).Key(string(theme.value)).Label(theme.label).Selected(selected).
					Grow(1).Width(0).MinWidth(0).Column().AlignItems(ui.Center).Gap(5).Padding(8, 4).
					Radius(12).Background(background).Border(1, border).Disabled(readOnly)
				button.Children(func() {
					ui.Box(c).FillWidth().Height(34).Radius(8).Background(theme.color)
					settingsText(c, theme.label, 13, palette.Text)
				})
				if button.Clicked() {
					s.saveSetting(state, func(settings *app.Settings) { settings.ThemeMode = theme.value })
				}
			}
		})
	})
	settingsGroup(c, "颜色与字体", "", func() {
		settingsPanel(c, "appearance-colors", func() {
			settingRow(c, "accent", "强调色", "用在正在播放、选中和主要按钮上", func() {
				ui.Row(c).Shrink(0).Children(func() {
					choices := []settingChoice[app.Accent]{{app.AccentCinnabar, "朱砂"}, {app.AccentIndigo, "靛青"}, {app.AccentPine, "松绿"}, {app.AccentAmber, "琥珀"}}
					for _, choice := range choices {
						selected := choice.value == st.Accent
						button := ui.ButtonBase(c).Key(string(choice.value)).Label(choice.label).Selected(selected).Column().
							Width(38).MinHeight(46).AlignItems(ui.Center).Justify(ui.Center).Gap(3).Disabled(readOnly)
						button.Children(func() {
							ring := ui.Color{}
							if selected {
								ring = palette.Focus
							}
							ui.Box(c).Size(26, 26).Radius(13).Border(1, ring).AlignItems(ui.Center).Justify(ui.Center).Children(func() {
								ui.Box(c).Size(22, 22).Radius(11).Background(tok.AccentColor(string(choice.value), c.Theme().Dark))
							})
							settingsText(c, choice.label, 11, palette.Text2)
						})
						if button.Clicked() {
							s.saveSetting(state, func(settings *app.Settings) { settings.Accent = choice.value })
						}
					}
				})
			})
			settingsDivider(c)
			settingRow(c, "cover-accent", "封面取色", "强调色跟着当前歌曲的封面变", func() {
				if settingSwitch(c, "封面取色", st.CoverAccent, readOnly) {
					s.saveSetting(state, func(settings *app.Settings) { settings.CoverAccent = !settings.CoverAccent })
				}
			})
			settingsDivider(c)
			settingRow(c, "font-scale", "界面字号", "", func() {
				choices := []settingChoice[app.FontScale]{{app.FontScaleSmall, "紧凑"}, {app.FontScaleStd, "标准"}, {app.FontScaleLarge, "大"}}
				settingSelect(c, "界面字号", st.FontScale, choices, readOnly, func(value app.FontScale) {
					s.saveSetting(state, func(settings *app.Settings) { settings.FontScale = value })
				})
			})
		})
	})
	settingsGroup(c, "字体", "", func() {
		settingsPanel(c, "mobile-font-families", func() {
			families := []settingChoice[string]{{app.DefaultFontFamily, "系统默认"}, {"PingFang SC", "苹方"}, {"Hiragino Sans GB", "冬青黑体"}, {"Songti SC", "宋体"}, {"Kaiti SC", "楷体"}, {"Microsoft YaHei", "微软雅黑"}}
			for index, family := range families {
				if index > 0 {
					settingsDivider(c)
				}
				if settingOption(c, family.value, family.label, "", family.value, family.value == st.FontFamily, readOnly) {
					s.saveSetting(state, func(settings *app.Settings) { settings.FontFamily = family.value })
				}
			}
		})
	})
	settingsGroup(c, "数据显示样式", "", func() {
		settingsPanel(c, "display-settings", func() {
			settingRow(c, "video-display", "视频列表的显示方式", "搜索结果的「视频」、UP 主投稿都会跟着变", func() {
				choices := []settingChoice[app.DisplayMode]{{app.DisplayModeCard, "卡片"}, {app.DisplayModeList, "列表"}, {app.DisplayModeCompact, "紧凑"}}
				settingSelect(c, "视频列表的显示方式", st.DisplayMode, choices, readOnly, func(value app.DisplayMode) {
					s.saveSetting(state, func(settings *app.Settings) { settings.DisplayMode = value })
				})
			})
		})
	})
}

func settingOption(c *ui.Context, key, title, note, font string, selected, disabled bool) bool {
	palette := tok.Of(c.Theme())
	button := ui.ButtonBase(c).Key(key).Label(title).Selected(selected).FillWidth().MinHeight(50).
		Padding(6, 4).Gap(10).AlignItems(ui.Center).Disabled(disabled)
	if selected {
		button = button.Radius(8).Background(palette.AccentSoft)
	}
	button.Children(func() {
		ui.Column(c).Grow(1).Shrink(1).Gap(3).Children(func() {
			text := settingsText(c, title, 14, palette.Text)
			if font != "" {
				text.Font(font)
				settingsText(c, "晴天 夜航电台 0123", 12, palette.Text2).Font(font)
			}
			if note != "" {
				settingsText(c, note, 12, palette.Text2)
			}
		})
		if selected {
			glyph(c, "check", 16, palette.AccentText)
		}
	})
	return button.Clicked()
}

func (s *shell) menuSettings(c *ui.Context, state *settingsPageData, st app.Settings, readOnly bool) {
	settingsGroup(c, "入口显示", "关掉的入口会从「我的」隐藏，数据仍保留。", func() {
		settingsPanel(c, "menu-entries", func() {
			for index, entry := range mobileMenuEntries {
				if index > 0 {
					settingsDivider(c)
				}
				s.visibilitySetting(c, state, st, entry.key, entry.title, "", readOnly)
			}
		})
	})
	settingsGroup(c, "歌单显示", "", func() {
		settingsPanel(c, "playlist-entries", func() {
			seen := make(map[int64]bool)
			for _, group := range []app.FavoriteGroup{app.FavoritesCreated, app.FavoritesCollected} {
				for _, item := range s.app.SideFavorites(group) {
					if seen[item.ID] {
						continue
					}
					if len(seen) > 0 {
						settingsDivider(c)
					}
					seen[item.ID] = true
					key := strconv.FormatInt(item.ID, 10)
					note := fmt.Sprintf("%s · %d 首", group, item.MediaCount)
					s.visibilitySetting(c, state, st, key, item.Title, note, readOnly)
				}
			}
			if len(seen) == 0 {
				message := "暂无可显示的歌单"
				if !s.app.LoggedIn() {
					message = "登录后可设置收藏夹显示"
				}
				settingRow(c, "playlist-empty", message, "", nil)
			}
		})
	})
}

func (s *shell) visibilitySetting(c *ui.Context, state *settingsPageData, st app.Settings, key, title, note string, readOnly bool) {
	settingRow(c, key, title, note, func() {
		shown := !slices.Contains(st.HiddenMenuKeys, key)
		if settingSwitch(c, "显示"+title, shown, readOnly) {
			s.saveSetting(state, func(settings *app.Settings) {
				if shown {
					if !slices.Contains(settings.HiddenMenuKeys, key) {
						settings.HiddenMenuKeys = append(slices.Clone(settings.HiddenMenuKeys), key)
					}
				} else {
					settings.HiddenMenuKeys = slices.DeleteFunc(slices.Clone(settings.HiddenMenuKeys), func(hidden string) bool { return hidden == key })
				}
			})
		}
	})
}

func (s *shell) proxySettings(c *ui.Context, state *settingsPageData, st app.Settings, readOnly bool) {
	draft := &state.proxy
	stored := st.ProxySettings
	custom := draft.custom || stored.Type != app.ProxyNone && stored.Type != app.ProxyDirect
	settingsGroup(c, "代理设置", "", func() {
		settingsPanel(c, "proxy-modes", func() {
			options := []struct {
				mode        app.ProxyType
				label, note string
				selected    bool
			}{
				{app.ProxyDirect, "不使用代理", "直接连接", !custom && stored.Type == app.ProxyDirect},
				{app.ProxyNone, "使用系统代理", "跟随系统网络设置", !custom && stored.Type == app.ProxyNone},
				{app.ProxyHTTP, "自定义代理", "手动填写类型、地址和端口", custom},
			}
			for index, option := range options {
				if index > 0 {
					settingsDivider(c)
				}
				if !settingOption(c, string(option.mode), option.label, option.note, "", option.selected, readOnly || draft.testing) {
					continue
				}
				if option.mode == app.ProxyHTTP {
					draft.custom = true
					continue
				}
				if s.saveSetting(state, func(settings *app.Settings) { settings.ProxySettings.Type = option.mode }) {
					draft.custom, draft.result = false, ""
				}
			}
		})
	})
	if !custom {
		return
	}
	if draft.kind == "" {
		draft.kind = stored.Type
		if draft.kind == app.ProxyNone || draft.kind == app.ProxyDirect {
			draft.kind = app.ProxyHTTP
		}
		draft.host, draft.user = stored.Host, stored.Username
		if stored.Port != nil {
			draft.port = strconv.Itoa(*stored.Port)
		}
	}
	palette := tok.Of(c.Theme())
	ui.Column(c).Key("custom-proxy-form").FillWidth().Gap(10).Margin(12, 0, 0, 0).Children(func() {
		disabled := readOnly || draft.testing
		settingRow(c, "proxy-protocol", "代理类型", "", func() {
			choices := []settingChoice[app.ProxyType]{{app.ProxyHTTP, "HTTP 代理"}, {app.ProxyHTTPS, "HTTPS 代理"}, {app.ProxySOCKS4, "SOCKS4 代理"}, {app.ProxySOCKS5, "SOCKS5 代理"}}
			settingSelect(c, "代理类型", draft.kind, choices, disabled, func(value app.ProxyType) { draft.kind = value })
		})
		proxyField(c, "代理主机", "127.0.0.1", draft.hostError, &draft.host, ui.KeyboardURL, false, disabled)
		proxyField(c, "代理端口", "7890", draft.portError, &draft.port, ui.KeyboardNumber, false, disabled)
		proxyField(c, "代理用户名", "没有就留空", "", &draft.user, ui.KeyboardDefault, false, disabled)
		secretHint, savedSecret := "", ""
		if stored.Password != "" && draft.user == stored.Username &&
			strings.Trim(strings.TrimSpace(draft.host), "[]") == stored.Host && stored.Port != nil &&
			strings.TrimSpace(draft.port) == strconv.Itoa(*stored.Port) && draft.kind == stored.Type {
			secretHint = "留空保留已保存的密码"
			savedSecret = stored.Password
		}
		proxyField(c, "代理密码", secretHint, "", &draft.secret, ui.KeyboardDefault, true, disabled)
		if settingsAction(c, "保存并测试", true, true).Disabled(disabled).Clicked() {
			s.saveMobileProxy(state, savedSecret)
		}
		if draft.testing {
			settingsText(c, "正在测试…", 13, palette.Text2).Role(ui.RoleStatus)
		} else if draft.result != "" {
			color := palette.Text2
			if draft.failed {
				color = palette.Err
			}
			settingsText(c, draft.result, 13, color).Role(ui.RoleStatus)
		}
	})
}

func proxyField(c *ui.Context, label, placeholder, message string, value *string, keyboard ui.KeyboardType, masked, disabled bool) {
	palette := tok.Of(c.Theme())
	ui.Column(c).Key(label).FillWidth().Gap(5).Children(func() {
		settingsText(c, label, 13, palette.Text)
		input := ui.TextInputBase(c, value).Label(label).Placeholder(placeholder).FillWidth().MinHeight(touchSize()).
			PaddingX(12).Radius(8).Background(palette.Surface2).TextColor(palette.Text).Disabled(disabled).
			Keyboard(keyboard).Capitalize(ui.CapitalizeNone).Autocorrect(false).ReturnKey(ui.ReturnNext)
		if masked {
			input.Password().ReturnKey(ui.ReturnDone)
		}
		if message != "" {
			settingsText(c, message, 12, palette.Err)
		}
	})
}

func (s *shell) saveMobileProxy(state *settingsPageData, storedSecret string) {
	draft := &state.proxy
	draft.hostError, draft.portError, draft.result = "", "", ""
	host := strings.TrimSpace(draft.host)
	if strings.HasPrefix(host, "[") && strings.HasSuffix(host, "]") {
		host = host[1 : len(host)-1]
	}
	port, err := strconv.Atoi(strings.TrimSpace(draft.port))
	if host == "" {
		draft.hostError = "请填写代理主机"
	} else if strings.ContainsAny(host, "/?#@[] \t\r\n") || strings.Contains(host, ":") && net.ParseIP(host) == nil {
		draft.hostError = "请填写主机名或 IP 地址"
	}
	if port < 1 || port > 65535 || err != nil {
		draft.portError = "端口必须为 1–65535"
	}
	if draft.hostError != "" || draft.portError != "" {
		return
	}
	secret := draft.secret
	if secret == "" {
		secret = storedSecret
	}
	if !s.saveSetting(state, func(settings *app.Settings) {
		settings.ProxySettings = app.ProxySettings{Type: draft.kind, Host: host, Port: &port, Username: draft.user, Password: secret}
	}) {
		return
	}
	draft.testing, draft.secret = true, ""
	go func() {
		ctx, cancel := context.WithTimeout(s.ctx, 5*time.Second)
		defer cancel()
		var dialer net.Dialer
		conn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(host, strconv.Itoa(port)))
		if err == nil {
			_ = conn.Close()
		}
		s.update(func() {
			draft.testing = false
			draft.result, draft.failed = "已保存，代理端口可连接。", err != nil
			if err != nil {
				draft.result = "已保存，但无法连接代理端口。请检查地址及网络后重试。"
			}
		})
	}()
}

func (s *shell) aboutSettings(c *ui.Context, state *settingsPageData, st app.Settings) {
	palette := tok.Of(c.Theme())
	ui.Column(c).Key("about-brand").FillWidth().AlignItems(ui.Center).Gap(8).PaddingY(24).Children(func() {
		mobileRecordMark(c, 60)
		typo.Display(settingsText(c, "布丁音乐 PudMusic", 20, palette.Text), scale.Font(c, 20))
		settingsText(c, "原生音乐播放器 · Go + MyGo", 13, palette.Text2)
	})
	settingsPanel(c, "about-actions", func() {
		updateNote := "通过安装来源的应用商店更新"
		if runtime.GOOS == "ios" {
			updateNote = "通过 App Store 更新"
		}
		settingRow(c, "app-update", "应用更新", updateNote, nil)
		settingsDivider(c)
		settingRow(c, "about-license", "开源许可", "基于 Biu 重新实现与扩展", func() {
			if settingsAction(c, "查看", false, false).Clicked() {
				s.push(route{kind: pageSetting, title: "开源许可", id: "licenses"})
			}
		})
		settingsDivider(c)
		settingRow(c, "diagnostic", "诊断信息", "遇到问题时附在反馈里", func() {
			if settingsAction(c, "复制", false, false).Disabled(s.win == nil).Clicked() {
				mygo.Clipboard.WriteText(fmt.Sprintf("PudMusic %s\n系统：%s/%s，Go %s\n音质偏好：%s，视频画质：%d\n主题：%s / %s，封面取色：%v，字号：%s\n代理：%s",
					mygo.App.Version(), runtime.GOOS, runtime.GOARCH, runtime.Version(), st.AudioQuality, st.VideoQuality,
					st.ThemeMode, st.Accent, st.CoverAccent, st.FontScale, st.ProxySettings.Type))
				state.message, state.failed = "已复制诊断信息", false
			}
		})
	})
}

func (s *shell) licenseSettings(c *ui.Context, state *settingsPageData) {
	settingsPanel(c, "license-list", func() {
		for index, document := range []struct{ name, path string }{
			{"Biu", licensing.BiuLicenseRelPath},
			{"Remix Icon", licensing.IconLicenseRelPath},
			{"思源宋体", licensing.FontLicenseRelPath},
			{"第三方组件许可", licensing.NoticesRelPath},
		} {
			if index > 0 {
				settingsDivider(c)
			}
			if !settingsLink(c, document.path, document.name, "") {
				continue
			}
			resources, err := mygo.App.Path(mygo.PathResources)
			if err == nil {
				var text []byte
				text, err = os.ReadFile(filepath.Join(resources, licensing.NoticesDirName, filepath.Base(document.path)))
				if err == nil {
					s.push(route{kind: pageSetting, title: document.name, id: "license-text", data: string(text)})
				}
			}
			if err != nil {
				state.message, state.failed = "无法读取随包提供的许可文件。", true
			}
		}
	})
}
