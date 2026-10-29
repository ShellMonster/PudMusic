package ui

import (
	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/imgcache"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/list"
)

func (s *Shell) pageInput() pageInput {
	if s.framePage.Input.Images != nil {
		return s.framePage.Input
	}
	return s.pageSeam().Input
}

func (s *Shell) pagePrefs() app.FullPlayerPrefs {
	return s.pageInput().FullPlayerPrefs
}

func (s *Shell) pageAccount() app.AccountSnapshot {
	return s.pageInput().Account
}

func (s *Shell) pageImages() *imgcache.Cache {
	return s.pageInput().Images
}

func (s *Shell) pageDisplayView() list.View {
	return s.pageInput().DisplayView
}
