package app_test

// 「墓碑」：刷新时详情接口查不到的那一条**还在列表里**，但标着失效。
//
// UP 主把收藏夹删了、或者设成仅自己可见之后，B 站的列表接口照样回它
// （「我收藏的」那边 `state != 0`），而详情接口（`folder/info`）回非 0——
// 这时候把那一条从侧栏上拿掉，用户会以为是应用少拉了。所以那一行留下，
// 画成一条点不动的灰行（见 `internal/ui` 那一层）。

import (
	"net/http"
	"testing"

	"github.com/ShellMonster/PudMusic/internal/app"
)

// favInfoPath 是「这个收藏夹还在不在」那个详情接口。
const favInfoPath = "/x/v3/fav/folder/info"

// TestACollectedEntryThatIsGoneStaysAsATombstone 钉住「我收藏的」那组：
// `state != 0` 的那一条**不滤掉**，标成失效留下。
//
// 这是墓碑的另一半验收：主人删了它、而收藏关系还留着——B 站那边恢复可见
// 之后，下一次刷新它能自己回来。
func TestACollectedEntryThatIsGoneStaysAsATombstone(t *testing.T) {
	f := newBiliFixture(t)
	onFavList(t, f, nil, nil, false)
	onFavCollected(t, f, nil, []map[string]any{
		{"id": 900, "title": "还在的那个", "mid": 77, "type": 11, "state": 0},
		{"id": 901, "title": "失效的那个", "mid": 78, "type": 11, "state": 1},
	}, false)

	a := favApp(t, f, favStore(t, "7"))
	a.RefreshFavorites()

	waitFor(t, waitTimeout, "两组都拉回来了", func() bool {
		return len(a.SideFavorites(app.FavoritesCollected)) == 2
	})
	got := a.SideFavorites(app.FavoritesCollected)
	var dead, alive *app.Favorite
	for i := range got {
		switch got[i].ID {
		case 900:
			alive = &got[i]
		case 901:
			dead = &got[i]
		}
	}
	if dead == nil {
		t.Fatalf("失效的那一条被从列表里拿掉了（剩下 %v）", got)
	}
	if !dead.Invalid {
		t.Errorf("失效的那一条没标 Invalid：%+v", *dead)
	}
	if alive == nil || alive.Invalid {
		t.Errorf("还在的那一条被标成失效了：%+v", alive)
	}
}

// TestACreatedEntryWhoseInfoIsGoneStaysAsATombstone 钉住「我创建的」那组：
// 详情接口查不到的那一条标成失效留下，不从列表里消失。
//
// 「我创建的」的列表接口**不**给 state，所以这一组只能挨个问一遍
// `folder/info`——问不到（返回非 0 业务码）就是失效。
func TestACreatedEntryWhoseInfoIsGoneStaysAsATombstone(t *testing.T) {
	f := newBiliFixture(t)
	onFavList(t, f, &favHits{}, []map[string]any{
		{"id": 11, "title": "还在的那个", "attr": 0, "media_count": 2},
		{"id": 12, "title": "失效的那个", "attr": 0, "media_count": 3},
	}, false)
	onFavCollected(t, f, nil, nil, false)
	// ⚠️ **只有失效的那一条回业务错误**：还在的那一条照常回 info——
	// 都回错误的话，分不清是「这一条没了」还是「整个接口坏了」。
	f.on(favInfoPath, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("media_id") == "12" {
			writeFixtureJSON(t, w, -400, "请求错误", nil)
			return
		}
		writeFixtureJSON(t, w, 0, "0", map[string]any{
			"id": 11, "title": "还在的那个", "attr": 0, "media_count": 2,
		})
	})

	a := favApp(t, f, favStore(t, "7"))
	a.RefreshFavorites()

	waitFor(t, waitTimeout, "「我创建的」拉回来了", func() bool {
		return len(a.SideFavorites(app.FavoritesCreated)) == 2 &&
			!a.FavoritesRefreshing()
	})
	got := a.SideFavorites(app.FavoritesCreated)
	var dead *app.Favorite
	for i := range got {
		if got[i].ID == 12 {
			dead = &got[i]
		}
	}
	if dead == nil {
		t.Fatalf("详情接口查不到的那一条被从列表里拿掉了（剩下 %v）", got)
	}
	if !dead.Invalid {
		t.Errorf("详情接口查不到的那一条没标 Invalid：%+v", *dead)
	}
}

// TestAGroupOfOnlyTombstonesIsNotEmpty 钉住「一组全是失效」也算有内容：
// 空状态那句话**不**摆出来（那一组画得出墓碑，不是「还没有收藏夹」）。
func TestAGroupOfOnlyTombstonesIsNotEmpty(t *testing.T) {
	f := newBiliFixture(t)
	onFavList(t, f, nil, nil, false)
	onFavCollected(t, f, nil, []map[string]any{
		{"id": 900, "title": "全是失效的那个", "mid": 77, "type": 11, "state": 1},
	}, false)

	a := favApp(t, f, favStore(t, "7"))
	a.RefreshFavorites()

	waitFor(t, waitTimeout, "「我收藏的」拉回来了", func() bool {
		return len(a.SideFavorites(app.FavoritesCollected)) == 1
	})
	got := a.SideFavorites(app.FavoritesCollected)
	if !got[0].Invalid {
		t.Fatalf("那一条没标失效：%+v", got[0])
	}
	if st := a.FavoriteState(app.FavoritesCollected); !st.Loaded {
		t.Errorf("一组全是失效时状态是 %+v，want Loaded", st)
	}
}

// TestATombstoneComesBackWhenBiliRestoresIt 钉住「B 站恢复了还能回来」：
// 上一次标了失效的那一条，下一次刷新接口说它在，失效标记跟着消掉。
//
// 这正是墓碑**不整条删**的理由：删掉了的话恢复回来是「凭空多出一条」，
// 而用户只会以为是上一次的顺序丢了。
func TestATombstoneComesBackWhenBiliRestoresIt(t *testing.T) {
	f := newBiliFixture(t)
	onFavList(t, f, nil, nil, false)
	onFavCollected(t, f, nil, []map[string]any{
		{"id": 900, "title": "那个收藏夹", "mid": 77, "type": 11, "state": 1},
	}, false)

	a := favApp(t, f, favStore(t, "7"))
	a.RefreshFavorites()
	waitFor(t, waitTimeout, "第一趟回来了（失效）", func() bool {
		items := a.SideFavorites(app.FavoritesCollected)
		return len(items) == 1 && items[0].Invalid
	})

	// B 站那边恢复可见（state 归 0），再刷一次。
	onFavCollected(t, f, nil, []map[string]any{
		{"id": 900, "title": "那个收藏夹", "mid": 77, "type": 11, "state": 0},
	}, false)
	a.RefreshFavorites()
	waitFor(t, waitTimeout, "第二趟回来了（恢复）", func() bool {
		items := a.SideFavorites(app.FavoritesCollected)
		return len(items) == 1 && !items[0].Invalid && !a.FavoritesRefreshing()
	})
}
