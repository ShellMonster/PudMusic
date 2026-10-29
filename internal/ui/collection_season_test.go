// 合集 / 系列那一页能真的列出内容（票 #683，缺陷 C 的界面那一半）。
//
// 业务层那一条在 `internal/app/season_dispatch_test.go`（分派到哪条路径）。这一
// 份走**整条链**：路由上带 `?type=21` → 容器页 → `app.LoadCollectionPage` →
// 假 B 站的合集接口 → 列表画出来。断的是「用户点进一个合集，里面看得见东西」。
package ui

import (
	"strconv"
	"testing"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/service/bili"
	"github.com/egoist/mygo/ui"
)

// collectionShellTyped 停在 `/collection/{id}?type={typ}` 上。
//
// ⚠️ **`collectionShell` 那一族只造收藏夹**（`collectionShellAs` 把 type 写死
// 成空串 = 11）。这一票要验的正是「type 不是 11 时走哪条接口」，所以得有这一份。
func collectionShellTyped(t *testing.T, typ bili.CollectionType, id string, medias ...map[string]any) (*favBili, *Shell, *ui.Tester) {
	t.Helper()
	b := newCollectionBili(t)
	b.setMedias(medias...)
	s, tt := newShell(t, withSettings(app.DefaultSettings("")))
	s.app.SetWake(s.wake.invalidate)

	transport := &app.CredentialTransport{
		Base:   b.Server.Client().Transport,
		Policy: app.DefaultInjectPolicy().AllowBiliHost(hostOfFixture(b.URL)),
	}
	cli := bili.NewClient(bili.Options{Transport: transport, CSRF: func() string { return "jct" }})
	cli.TestOnlyPointHostsAtFixture(b.URL)
	s.app.MountCollections(app.CollectionRefs{Client: cli, Settle: 0})

	s.navigate("/collection/" + id + "?type=" + strconv.Itoa(int(typ)))
	waitFrames(t, tt, "容器元信息加载完", func() bool {
		return s.collection != nil && s.app.Collection(s.collection.ref).Loaded
	})
	return b, s, tt
}

// TestTheSeasonPageListsItsArchives 是本缺陷的主干那一条。
//
// 走的是**假 B 站上那两条新路径**（`favBili` 已经会答它们）：列表里看得见合集里
// 的那几首。走错接口（收藏夹那条）的话这两行**一首都不会出现**，而页面上是一句
// 「暂无内容」——正是用户报的那个样子。
func TestTheSeasonPageListsItsArchives(t *testing.T) {
	for _, c := range []struct {
		name string
		typ  bili.CollectionType
		id   string
	}{
		{"合集", bili.CollectionAlbum, "1227671"},
		{"系列", bili.CollectionSeries, "534501"},
	} {
		t.Run(c.name, func(t *testing.T) {
			b, s, tt := collectionShellTyped(t, c.typ, c.id,
				mediaOf(1, "第一集"), mediaOf(2, "第二集"))
			waitSongs(t, tt, s, 2)

			for _, want := range []string{"第一集", "第二集"} {
				if !tt.HasText(want) {
					t.Errorf("%s 里没有 %q，文字有 %q", c.name, want, tt.Texts())
				}
			}
			// 头图区那一行写的是这一类的名字（不是「收藏夹」）。
			kind := "视频合集"
			if c.typ == bili.CollectionSeries {
				kind = "视频系列"
			}
			if !tt.HasText(kind) {
				t.Errorf("头图区没写 %q，文字有 %q", kind, tt.Texts())
			}
			// 🔴 **打的是哪条路径**（这一条才是本缺陷的判据）：`favBili` 对收藏夹
			// 那条路径也答得出两行，所以「列表画出来了」在分派没生效时**照样过**。
			// 只有数请求才分得清。
			wantPath := "/x/polymer/web-space/seasons_archives_list"
			if c.typ == bili.CollectionSeries {
				wantPath = "/x/series/archives"
			}
			if got := b.hitsOf(wantPath); got != 1 {
				t.Errorf("%s 打了 %q %d 次，want 1 次", c.name, wantPath, got)
			}
			if got := b.hitsOf("/x/v3/fav/resource/list"); got != 0 {
				t.Errorf("%s 打了收藏夹的内容接口 %d 次——那个接口对它只会回空列表", c.name, got)
			}
		})
	}
}

// TestTheSeasonPageShowsTheFetchFailureThatIsNotADeadEnd 走错接口那一档的**反面**：
// 接口真的挂掉时，界面上要出现「加载失败，点此重试」，而不是一句「暂无内容」。
//
// 这一条和上面那条是同一件事的两半：分派对之后，合集那边的失败才有机会走
// 列表自己的失败态。原来打的是收藏夹那个接口，B 站回 -400 → 列表失败 → 那句
// 重试**其实画得出来**；但用户看到的是「这个合集打不开」而不是「里面是空的」。
// 现在两条路径都通了，失败态照旧要能画出来。
func TestTheSeasonPageShowsTheFetchFailureThatIsNotADeadEnd(t *testing.T) {
	b := newCollectionBili(t)
	b.setMedias(mediaOf(1, "第一集"))
	b.failList = -400
	_, s, tt := collectionShellTypedWith(t, b, bili.CollectionAlbum, "1227671")
	waitFrames(t, tt, "合集内容拉失败", func() bool {
		return s.collection != nil && s.collection.list.Paged().Err != nil
	})
	tt.Frame()

	if !tt.HasText("加载失败") {
		t.Errorf("合集拉失败时画面上没有重试入口，文字有 %q", tt.Texts())
	}
	dontWantText(t, tt, "暂无内容")
}

// collectionShellTypedWith 是上面那一个，用**已经备好的**夹具（要改 failList
// 之类的时候用）。
func collectionShellTypedWith(t *testing.T, b *favBili, typ bili.CollectionType, id string) (*favBili, *Shell, *ui.Tester) {
	t.Helper()
	s, tt := newShell(t, withSettings(app.DefaultSettings("")))
	s.app.SetWake(s.wake.invalidate)
	transport := &app.CredentialTransport{
		Base:   b.Server.Client().Transport,
		Policy: app.DefaultInjectPolicy().AllowBiliHost(hostOfFixture(b.URL)),
	}
	cli := bili.NewClient(bili.Options{Transport: transport, CSRF: func() string { return "jct" }})
	cli.TestOnlyPointHostsAtFixture(b.URL)
	s.app.MountCollections(app.CollectionRefs{Client: cli, Settle: 0})
	s.navigate("/collection/" + id + "?type=" + strconv.Itoa(int(typ)))
	waitFrames(t, tt, "容器元信息加载完", func() bool {
		return s.collection != nil && s.app.Collection(s.collection.ref).Loaded
	})
	return b, s, tt
}
