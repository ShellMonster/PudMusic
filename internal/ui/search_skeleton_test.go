package ui

import (
	"context"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/ShellMonster/PudMusic/internal/ui/widgets/list"
	mygoui "github.com/egoist/mygo/ui"
)

func TestSearchUserLoadingUsesUserCardSkeleton(t *testing.T) {
	searchUserSkeletonCase(t, false)
}

func TestRenderSearchUserSkeletonShots(t *testing.T) {
	dir := os.Getenv("BIU_SHOT_DIR")
	if dir == "" {
		t.Skip("设 BIU_SHOT_DIR 才会存图")
	}
	for _, dark := range []bool{false, true} {
		t.Run(map[bool]string{false: "light", true: "dark"}[dark], func(t *testing.T) {
			_, tt := searchUserSkeletonCase(t, dark)
			save(t, dir, map[bool]string{false: "light", true: "dark"}[dark]+"-search-user-skeleton.png", tt)
		})
	}
}

func searchUserSkeletonCase(t *testing.T, dark bool) (*Shell, *mygoui.Tester) {
	s, tt := newShell(t, systemIsDark(dark))
	release := make(chan struct{})
	s.search.fetch = func(_ int, _ context.Context, _ int) (list.Page, error) {
		<-release
		return list.Page{}, nil
	}
	t.Cleanup(func() { close(release) })

	s.search.tab = searchTabUser
	s.navigate(searchPath + "?" + searchQuery + "=" + url.QueryEscape("陈奕迅"))
	for i := 0; i < 8; i++ {
		tt.Advance(50 * time.Millisecond)
		tt.Frame()
	}
	if _, ok := tt.Find("正在加载用户…"); !ok {
		t.Fatalf("用户搜索首屏未显示用户卡片骨架，文本为 %q", tt.Texts())
	}
	if _, ok := tt.Find("搜索用户占位:0:0:头像"); !ok {
		t.Fatalf("用户卡片骨架没有头像占位，文本为 %q", tt.Texts())
	}
	return s, tt
}
