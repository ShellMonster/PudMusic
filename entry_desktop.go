//go:build !ios && !android

package main

import "github.com/ShellMonster/PudMusic/internal/bootstrap"

func platformDependencies() bootstrap.Dependencies {
	return bootstrap.DefaultDependencies()
}
