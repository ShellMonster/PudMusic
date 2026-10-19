package ui

import (
	"net/http"
	"testing"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/ShellMonster/Biu_Go/internal/service/bili"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/list"
)

// 这一组钉「播放量从数据源一路走到网格卡片封面底部那一条渐变上」。
//
// ## 为什么单独断这一层
//
// 那一格渐变**画出来对不对**是 `widgets/list` 的事（`hover_test.go` 里已经断过）。这一组
// 断的是**上一段路**：数据源明明带着这个数（`RecommendItem.View` / `VideoHit.Play` /
// `UpVideo.Play` / `LibraryItem.View`），而往 `list.Music` 上翻的时候漏了 ——
// **症状是那一半永远空着，而画面上一点异样都看不出来**（字都在、位置也在，只是少了
// 左半边），所以只能从映射函数这一层断。

// TestEachPageThatHasAPlayCountCarriesItIntoTheRow 逐页量「翻过去之后还在不在」。
//
// ⚠️ **逐个页面写一条，不写一个循环**：写循环就得把映射函数收进一张表，而那几个函数的
// 参数类型各不相同（`app.RecommendItem` / `bili.VideoHit` / `bili.UpVideo` /
// `app.LibraryItem`）——收进一张表就得配一个造值的闭包表，那张表会和这几个函数一起漂，
// 而漂了的样子是「表上还写着有，函数里没了」。
func TestEachPageThatHasAPlayCountCarriesItIntoTheRow(t *testing.T) {
	t.Run("首页", func(t *testing.T) {
		got := homeMusicOf([]app.RecommendItem{{ID: "a", View: 123_400}})
		if len(got) != 1 || got[0].View != 123_400 {
			t.Errorf("翻过去之后播放量是 %v，want [123400]（首页那一摊的网格卡片"+
				"上封面底部那一条的左半边会空着）", viewsOf(got))
		}
	})
	t.Run("搜索歌曲与视频", func(t *testing.T) {
		got := searchVideoMusic(bili.VideoHit{Bvid: "BV1", Play: 55})
		if got.View != 55 {
			t.Errorf("翻过去之后播放量是 %d，want 55", got.View)
		}
	})
	t.Run("搜索用户", func(t *testing.T) {
		// 用户不是视频，本来就没有这个数——**零值是那一半不画**，不是「忘了填」。
		got := searchUserMusic(bili.UserHit{Mid: 7, Name: "某人"})
		if got.View != 0 {
			t.Errorf("用户那一档的播放量是 %d，want 0（用户不是视频）", got.View)
		}
	})
	t.Run("稍后再看", func(t *testing.T) {
		got := laterRowOf(app.LibraryItem{ID: "a", View: 990})
		if got.View != 990 {
			t.Errorf("翻过去之后播放量是 %d，want 990", got.View)
		}
	})
	t.Run("历史记录", func(t *testing.T) {
		// 那个接口不给播放量，所以翻过去必须是零值。
		got := historyRowOf(app.LibraryItem{ID: "a"})
		if got.View != 0 {
			t.Errorf("历史记录那一页的播放量是 %d，want 0（那个接口不给）", got.View)
		}
	})
}

// TestThePlayCountTextFoldsLikeUpstream 断那个折法本身。
//
// 抄上游的 `Intl.NumberFormat` compact。七位数那一串字不折的话，网格卡片封面底部
// 那一条只有 22 高，字直接盖过封面去了。
func TestThePlayCountTextFoldsLikeUpstream(t *testing.T) {
	for _, c := range []struct {
		n    int64
		want string
	}{
		{0, ""}, // 没有这个数 → 空串（调用方据此不画那半边）
		{-1, ""},
		{999, "999"},
		{10_000, "1.0万"},
		{123_400, "12.3万"},
		{100_000_000, "1.0亿"},
	} {
		if got := list.PlayCountText(c.n); got != c.want {
			t.Errorf("播放量 %d 折出来是 %q，want %q", c.n, got, c.want)
		}
	}
}

// viewsOf 是几行的播放量拼成一句话，好让报错那一句能一次说清是几行里的哪一行。
func viewsOf(rows []list.Music) []int64 {
	out := make([]int64, len(rows))
	for i, r := range rows {
		out[i] = r.View
	}
	return out
}

// TestTheUpSpaceVideosCarryTheirPlayCount 断「UP 空间投稿」那一页的播放量翻过去了。
//
// 这一页和其余三个不一样：它翻的是 `bili.UpVideo.Play`，而那一层**中途换过一次类型**
// （原本是 `string`，赋值 `strconv.Itoa(接口给的整数)`）。留着那个 string 的话，
// 界面要么再 parse 回来（折法会分叉）、要么显示成没折的七位数——所以这里连着
// **接口解析**一起断：夹具里给 `play`，断言到 `list.Music.View`。
func TestTheUpSpaceVideosCarryTheirPlayCount(t *testing.T) {
	b := newUIExplore(t)
	b.on("/x/space/wbi/arc/search", func(w http.ResponseWriter, r *http.Request) {
		uiExploreJSON(w, 0, map[string]any{
			"list": map[string]any{"vlist": []any{
				map[string]any{
					"bvid": "BV1ok", "title": "点我", "length": "3:20",
					"cid": "9", "play": 123_400,
				},
			}},
			"page": map[string]any{"count": 1},
		})
	})
	s, tt, _ := newSpaceShell(t, b, false, "")
	sp := openSpace(t, s, tt, 42)
	sp.tab = upTabVideo
	tt.Frame()
	waitFrames(t, tt, "投稿加载完", func() bool { return sp.videos.Paged().Loaded > 0 })

	m, ok := sp.videos.List().At(0)
	if !ok {
		t.Fatalf("那一页里一首都没有：%v", sp.videos.Paged())
	}
	if m.View != 123_400 {
		t.Errorf("翻过来之后播放量是 %d，want 123400（网格卡片封面底部那一条的左半边"+
			"会空着，而这一页的接口明明给了 play）", m.View)
	}
}
