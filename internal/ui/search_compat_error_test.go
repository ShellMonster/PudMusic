package ui

import (
	"errors"
	"testing"

	"github.com/ShellMonster/PudMusic/internal/app"
)

var errSearchNotMounted = errors.New("搜索暂时用不了")

func TestSearchErrorTextUsesApplicationCategory(t *testing.T) {
	err := &app.SearchError{
		Message:  "内部错误",
		Category: app.SearchErrorNetwork,
	}
	if got, want := searchErrorText(err), "连不上 B 站，检查一下网络"; got != want {
		t.Fatalf("searchErrorText = %q, want %q", got, want)
	}
}
