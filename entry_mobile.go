//go:build ios || android

package main

import (
	"github.com/ShellMonster/PudMusic/internal/bootstrap"
	"github.com/ShellMonster/PudMusic/internal/mobileui"
)

func platformDependencies() bootstrap.Dependencies {
	return bootstrap.MobileDependencies(mobileui.Run)
}
