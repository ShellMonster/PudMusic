package ui

// 「我收藏的」那一组（issue #362）。
//
// 这一组盯的是**类型**：B 站「我收藏了别人的哪些收藏夹」那个接口
// （`collected/list`）给的条目里有收藏夹（11）、合集（21）和系列（31）
// 三种——而它们是三个**不同的容器**。
//
// ⚠️ **类型不在地址上就完了**：`collectionPath` 只给 id，于是点一个合集会
// **按收藏夹打开**——打不开，或者打开的是另一个容器，而界面上看不出异样。
//
// ⚠️ **今天侧栏上只有收藏夹那一种行**（合集 / 系列被 `app.collectedFavorites`
// 滤掉了——它们的内容列表是另一套接口，本项目没接）。这一组钉的是**路由
// 本身**认得出类型：等歌单页接上合集内容，那一行放开时地址已经是对的。

import (
	"testing"

	"github.com/ShellMonster/PudMusic/internal/app"
)

// TestClickingACollectedAlbumGoesToThatAlbum 断点一个**合集**走的是合集
// 那个地址。
func TestClickingACollectedAlbumGoesToThatAlbum(t *testing.T) {
	s, a, tt, _ := favShell(t, 0, 0)
	a.SetFavorites(app.FavoritesCollected, []app.Favorite{
		{ID: 901, Title: "合集 甲", Mid: 78, Type: 21},
	})
	tt.Frame()

	if err := tt.Click("合集 甲"); err != nil {
		t.Fatalf("点「合集 甲」：%v（文字有 %q）", err, tt.Texts())
	}
	tt.Frame()
	if got := s.router.Location(); got != "/collection/901?type=21" {
		t.Errorf("点完停在 %q，want /collection/901?type=21", got)
	}
}

// TestClickingACollectedFavoriteGoesWithoutTheType 断点一个**收藏夹**的
// 地址上**不带** `?type=`。
//
// 收藏夹（11）是默认值，而 Biu 的地址也是不带的那一种（`/collection/{id}`）——
// 两边的地址长得一样，导来导去才对得上。
func TestClickingACollectedFavoriteGoesWithoutTheType(t *testing.T) {
	s, a, tt, _ := favShell(t, 0, 0)
	a.SetFavorites(app.FavoritesCollected, []app.Favorite{
		{ID: 902, Title: "收藏夹 乙", Mid: 79, Type: 11},
	})
	tt.Frame()

	if err := tt.Click("收藏夹 乙"); err != nil {
		t.Fatalf("点「收藏夹 乙」：%v（文字有 %q）", err, tt.Texts())
	}
	tt.Frame()
	if got := s.router.Location(); got != "/collection/902" {
		t.Errorf("点完停在 %q，want /collection/902（收藏夹是默认值，地址上不写 type）", got)
	}
}

// TestAnUnknownTypeIsTreatedAsAFavorite 断**认不出来的类型**按收藏夹处理。
//
// 侧栏那一行的 Type 是别的页面填的，而「按收藏夹处理」最多是打不开，
// 「整条失败」会让用户以为自己收藏的不是收藏夹。
func TestAnUnknownTypeIsTreatedAsAFavorite(t *testing.T) {
	s, a, tt, _ := favShell(t, 0, 0)
	a.SetFavorites(app.FavoritesCollected, []app.Favorite{
		{ID: 903, Title: "认不出的那一条", Mid: 80, Type: 99},
	})
	tt.Frame()

	if err := tt.Click("认不出的那一条"); err != nil {
		t.Fatalf("点「认不出的那一条」：%v（文字有 %q）", err, tt.Texts())
	}
	tt.Frame()
	if got := s.router.Location(); got != "/collection/903" {
		t.Errorf("点完停在 %q，want /collection/903（认不出的类型按收藏夹处理）", got)
	}
}
