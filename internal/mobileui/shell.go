package mobileui

import (
	"context"
	"log"
	"runtime"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/media"
	desktop "github.com/ShellMonster/PudMusic/internal/ui"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

type pageKind uint8

const (
	pageDiscover pageKind = iota
	pageSearch
	pageMy
	pageDaily
	pageChart
	pageNews
	pageMixes
	pageAlbum
	pageCollection
	pageHistory
	pageLocal
	pageDownloads
	pageLater
	pageFollowing
	pageFeed
	pageCreator
	pageStats
	pageSettings
	pageSetting
	pageLogin
)

type route struct {
	kind  pageKind
	title string
	id    string
	ref   app.CollectionRef
	query string
	focus bool
	data  any
	key   uint64
}

type shell struct {
	app      *app.App
	win      *mygo.Window
	ctx      context.Context
	tab      int
	stacks   [3][]*route
	serial   uint64
	player   playerView
	notice   string
	dispatch func(func())
}

func newShell(ctx context.Context, application *app.App) *shell {
	return &shell{
		app: application, ctx: ctx, serial: 3,
		stacks: [3][]*route{
			{{kind: pageDiscover, title: "发现", key: 1}},
			{{kind: pageSearch, title: "搜索", key: 2}},
			{{kind: pageMy, title: "我的", key: 3}},
		},
	}
}

func Run(application *app.App, session *media.Session) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	defer application.Close()
	if session != nil {
		defer session.Close()
	}
	s := newShell(ctx, application)
	if err := application.RestorePlayback(); err != nil {
		log.Printf("pudmusic: restore mobile playback: %v", err)
	}
	mygo.App.WhenReady(func() {
		application.NoteReady()
		s.win = mygo.NewWindow(mygo.WindowOptions{
			Title: "布丁音乐", Width: 390, Height: 844, Content: ui.View(s.view),
		})
		remove := application.AddWake(s.win.Invalidate)
		s.win.OnClosed(remove)
	})
	defer s.watchDocumentShares()()
	return mygo.App.Run()
}

func (s *shell) current() *route {
	stack := s.stacks[s.tab]
	return stack[len(stack)-1]
}

func (s *shell) push(page route) {
	if previous := s.current(); previous.kind == pageLogin {
		s.leaveLogin(previous)
	}
	s.serial++
	page.key = s.serial
	s.stacks[s.tab] = append(s.stacks[s.tab], &page)
}

func (s *shell) switchTab(tab int) {
	if previous := s.current(); previous.kind == pageLogin {
		s.leaveLogin(previous)
		s.stacks[s.tab] = s.stacks[s.tab][:len(s.stacks[s.tab])-1]
	}
	if s.tab == tab {
		s.stacks[tab] = s.stacks[tab][:1]
	}
	s.tab = tab
}

func (s *shell) openSearch() {
	s.switchTab(1)
	s.stacks[1] = s.stacks[1][:1]
	s.current().focus = true
}

func (s *shell) back() bool {
	if s.player.sheet == sheetTrackFolders && s.player.folders != nil {
		view := s.player.folders
		if view.saving || view.editor.saving || view.editor.crop.uploading {
			return true
		}
		if view.editor.open {
			view.editor.open = false
			return true
		}
	}
	if (s.player.sheet == sheetComments || s.player.sheet == sheetReplies) && s.player.comments.focused {
		s.player.comments.focused = false
		return true
	}
	if s.dismissSearchKeyboard() {
		return true
	}
	if s.player.back() {
		if !s.player.visible {
			s.app.StopVideo()
		}
		return true
	}
	stack := s.stacks[s.tab]
	if len(stack) > 1 {
		if page := s.current(); page.kind == pageLogin {
			s.leaveLogin(page)
		}
		s.stacks[s.tab] = stack[:len(stack)-1]
		return true
	}
	if s.tab != 0 {
		s.switchTab(0)
		return true
	}
	return false
}

func (s *shell) update(fn func()) {
	if s.ctx.Err() != nil {
		return
	}
	if s.dispatch != nil {
		s.dispatch(fn)
	} else if s.win != nil {
		s.win.Update(fn)
	}
}

func (s *shell) view(c *ui.Context) {
	desktop.ApplyTheme(c, s.app.SettingsSnapshot(), c.Theme().Dark)
	desktop.Guard(s.draw, func(value any) { log.Printf("pudmusic: mobile view: %v", value) })(c)
}

func (s *shell) draw(c *ui.Context) {
	if (s.player.visible || s.player.sheet != sheetNone || s.tab != 0 || len(s.stacks[s.tab]) > 1) && c.Shortcut(0, ui.KeyBack) {
		s.back()
	}
	if s.notice != "" {
		c.Toast(s.notice)
		s.notice = ""
	}
	state := s.app.Snapshot()
	if s.player.visible && !state.HasTrack && !state.Loading && state.Err == "" {
		s.player.visible = false
	}
	s.app.SetSpectrumWantedFor(app.SpectrumFull, s.player.visible && !s.player.lyrics && state.Playing && s.app.FullPlayerPrefs().ShowSpectrum)
	palette := phonePalette(c)
	background := palette.surface
	if page := s.current(); !s.player.visible && (page.kind == pageSettings || page.kind == pageSetting || page.kind == pageLogin) {
		background = palette.fill
	}
	ui.Column(c).Fill().Background(background).AccessHidden(s.player.sheet != sheetNone).Children(func() {
		if s.player.visible {
			s.fullPlayer(c, state)
			return
		}
		page := s.current()
		s.topBar(c, page)
		scroll := ui.Scroll(c.Key(page.key)).Grow(1).Basis(0).FillWidth()
		if page.kind == pageDiscover || page.kind == pageDaily || page.kind == pageChart || page.kind == pageNews || page.kind == pageMixes || page.kind == pageAlbum {
			s.discoverRefresh(page, scroll)
		} else {
			s.libraryRefresh(page, scroll)
		}
		scroll.Children(func() {
			ui.Column(c).FillWidth().Padding(8, 16, 24, 16).Gap(8).Children(func() {
				s.drawPage(c, page)
			})
		})
		if state.HasTrack || state.Loading || state.Err != "" {
			s.miniPlayer(c, state)
		}
		s.tabs(c)
	})
	s.playerSheet(c, state)
}

func (s *shell) drawPage(c *ui.Context, page *route) {
	switch page.kind {
	case pageDiscover, pageDaily, pageChart, pageNews, pageMixes, pageAlbum:
		s.discover(c, page)
	case pageSearch:
		s.search(c, page)
	case pageCollection:
		s.collection(c, page)
	case pageLogin:
		s.login(c, page)
	case pageSettings, pageSetting:
		s.settings(c, page)
	default:
		s.library(c, page)
	}
}

func (s *shell) topBar(c *ui.Context, page *route) {
	if page.kind == pageMy || page.kind == pageSearch {
		return
	}
	if page.kind == pageDiscover {
		ui.Row(c).Height(touchSize()).FillWidth().PaddingX(16).Gap(8).AlignItems(ui.Center).Children(func() {
			brand(c)
			ui.Text(c, "布丁音乐").FontSize(16).FontWeight(600)
		})
		return
	}
	ui.Row(c).Height(touchSize()).FillWidth().PaddingX(8).AlignItems(ui.Center).Children(func() {
		iconButton(c, "left", "返回").OnClick(func() { s.back() })
		if page.kind == pageSettings || page.kind == pageSetting || page.kind == pageLogin {
			return
		}
		label := page.title
		if page.kind == pageCollection {
			label = "歌单"
		}
		ui.Box(c).Grow(1).Basis(0).Center().Children(func() {
			ui.Text(c, label).FontSize(13).FontWeight(600).MaxLines(1)
		})
		ui.Box(c).Size(touchSize(), touchSize())
	})
}

func (s *shell) tabs(c *ui.Context) {
	palette := phonePalette(c)
	height := float32(56)
	if runtime.GOOS == "android" {
		height = 60
	}
	ui.Row(c).FillWidth().Height(height).Children(func() {
		for index, tab := range [...]struct{ label, icon string }{{"发现", "disc"}, {"搜索", "search"}, {"我的", "user"}} {
			selected := s.tab == index
			ink := palette.muted
			if selected {
				ink = palette.accent
			}
			ui.Column(c.Key(index)).Role(ui.RoleButton).Label(tab.label).AccessSelected(selected).Grow(1).Basis(0).Height(height).Center().Gap(2).Children(func() {
				if runtime.GOOS == "android" && selected {
					ui.Box(c).Size(52, 29).Radius(15).Center().Background(palette.soft).Children(func() { glyph(c, tab.icon, 22, ink) })
				} else {
					glyph(c, tab.icon, 22, ink)
				}
				ui.Text(c, tab.label).FontSize(12).TextColor(ink).FontWeight(500)
			}).OnClick(func() { s.switchTab(index) })
		}
	})
}
