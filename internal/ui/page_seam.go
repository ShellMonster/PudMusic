package ui

import (
	"time"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/imgcache"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/list"
)

// pageInput is the immutable shared input a page receives for one build pass.
//
// Page state stays owned by the Shell, while cross-page state is sampled here so
// a page does not need to reach through the Shell for every shared read.
type pageInput struct {
	Playback        app.State
	Current         app.Item
	HasCurrent      bool
	Account         app.AccountSnapshot
	FullPlayerPrefs app.FullPlayerPrefs
	Images          *imgcache.Cache
	Now             func() time.Time
	DisplayView     list.View
}

// pageActions are the small actions a page can request from its Shell/App owner.
type pageActions struct {
	Navigate           func(string)
	SetFullPlayerPrefs func(app.FullPlayerPrefs)
	TogglePlayback     func()
	Wake               func()
}

type pageSeam struct {
	Input   pageInput
	Actions pageActions
}

// pageSeam captures shared state once for a page build and exposes only scoped
// actions. It is deliberately concrete and local to the UI package: each page
// keeps its own state model instead of implementing a generic page interface.
func (s *Shell) pageSeam() pageSeam {
	st := s.app.Snapshot()
	current, hasCurrent := s.app.CurrentItem()
	return pageSeam{
		Input: pageInput{
			Playback:        st,
			Current:         current,
			HasCurrent:      hasCurrent,
			Account:         s.app.AccountSnapshot(),
			FullPlayerPrefs: s.app.FullPlayerPrefs(),
			Images:          s.images,
			Now:             s.now,
			DisplayView:     s.displayView(),
		},
		Actions: pageActions{
			Navigate:           s.navigate,
			SetFullPlayerPrefs: s.app.SetFullPlayerPrefs,
			TogglePlayback:     s.app.Toggle,
			Wake:               s.wake.invalidate,
		},
	}
}
