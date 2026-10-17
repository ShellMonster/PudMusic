package bili

// 点赞与一键三连那一层的测试。
//
// **形状来自 Biu 上游**：`web-interface-archive-relation.ts`、
// `web-interface-archive-like.ts`、`web-interface-archive-like-triple.ts`。
// fixture 照那几个类型的字段名造。
//
// ⚠️ 没有任何一条断言是在证明「B 站现在真这么返回」。更要紧的是：**点赞和三连
// 都是写操作**，会改用户 B 站账号上的状态（还花掉真金白银的硬币），所以真机
// 验证只能在测试账号上做。

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"
)

// likeAid 是那台假 B 站报的 avid。
const likeAid = 8899

// likeFixture 是一台会点赞的假 B 站。
//
// liked/faved 是它报的「赞没赞、收藏没收藏」；triple 是三连那一趟的回包
// （**它说了算**——B 站可能拒绝了投币，见 TestTheTripleReportsWhatBiliActuallyDid）。
func likeFixture(t *testing.T, liked, faved bool, triple map[string]any) *fakeBili {
	t.Helper()
	f := newFakeBili(t)
	f.on(pathView, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, jsonData(t, 0, "0", map[string]any{"aid": likeAid, "bvid": r.URL.Query().Get("bvid")}))
	})
	f.on(pathArchiveRelation, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, jsonData(t, 0, "0", map[string]any{"like": liked, "favorite": faved}))
	})
	f.on(pathArchiveLike, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, jsonData(t, 0, "0", nil))
	})
	f.on(pathArchiveTriple, func(w http.ResponseWriter, r *http.Request) {
		if triple == nil {
			triple = map[string]any{"like": true, "coin": true, "fav": true, "multiply": 1}
		}
		writeJSON(w, jsonData(t, 0, "0", triple))
	})
	return f
}

// likeClient 是带登录态和 CSRF 的客户端（点赞是写操作，两样都要）。
func likeClient(t *testing.T, f *fakeBili) *Client {
	t.Helper()
	return f.client(t, Options{
		CSRF: func() string { return "the-jct-value" },
		Now:  func() time.Time { return time.Unix(1702204169, 0) },
	})
}

// ---- 关系 ----

// TestArchiveRelationReadsLikeAndFavorite 钉住「赞没赞、收藏没收藏」这两个字段名。
//
// 字段名写错的话结果是**恒为没赞**：Go 的 json 匹配不上就留零值，于是界面上
// 每一首都是「没赞」，而长按三连于是每一首都开着（已经赞过的那几首会被再赞一次）。
func TestArchiveRelationReadsLikeAndFavorite(t *testing.T) {
	for _, c := range []struct {
		name         string
		liked, faved bool
	}{
		{"都没做", false, false},
		{"赞过", true, false},
		{"收过", false, true},
		{"两样都做过", true, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := likeFixture(t, c.liked, c.faved, nil)
			rel, err := likeClient(t, f).ArchiveRelation(context.Background(), "BV1xx")
			if err != nil {
				t.Fatalf("问关系：%v", err)
			}
			if rel.Liked != c.liked || rel.Favorited != c.faved {
				t.Errorf("读到的是 %+v，fixture 给的是 liked=%v faved=%v", rel, c.liked, c.faved)
			}
			req := f.requests()
			if len(req) != 1 {
				t.Fatalf("发了 %d 个请求，want 1", len(req))
			}
			if req[0].Method != http.MethodGet {
				t.Errorf("问关系用的是 %s，want GET", req[0].Method)
			}
			if got := req[0].Query.Get("bvid"); got != "BV1xx" {
				t.Errorf("bvid 传的是 %q", got)
			}
		})
	}
}

// TestArchiveRelationOfNothingAsksNobody 钉住「没有 bvid 就不发请求」。
//
// 本地文件和音乐库条目走到界面上时 bvid 是空的，而这一问**要 Cookie**：发了
// 也没有用（回来的是没有 like/favorite 的关系）。真发出去只是白等一趟。
func TestArchiveRelationOfNothingAsksNobody(t *testing.T) {
	f := likeFixture(t, false, false, nil)
	rel, err := likeClient(t, f).ArchiveRelation(context.Background(), "  ")
	if err != nil {
		t.Fatalf("问关系：%v", err)
	}
	if rel.Liked || rel.Favorited {
		t.Errorf("没有 bvid 却读出了 %+v", rel)
	}
	if n := len(f.requests()); n != 0 {
		t.Errorf("发了 %d 个请求，want 0", n)
	}
}

// ---- 赞 / 取消赞 ----

// TestLikeArchiveSendsTheWriteWithCSRF 是「点一下星标」那一次写。
//
// 断三样：**POST**、**CSRF**（查询串 + `bili-jct` 头）、`up` 的 1/2。
//
// ⚠️ `up` 用 1/2 而不是「切换」：界面重画之后手上那份状态可能是**旧的**，切换
// 会点成反的（用户连点两下等于没点）。和 `LikeDynamic` 是同一条。
func TestLikeArchiveSendsTheWriteWithCSRF(t *testing.T) {
	for _, c := range []struct {
		name string
		like bool
		up   string
	}{
		{"赞", true, "1"},
		{"取消赞", false, "2"},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := likeFixture(t, false, false, nil)
			if err := likeClient(t, f).LikeArchive(context.Background(), "BV1xx", c.like); err != nil {
				t.Fatalf("赞：%v", err)
			}
			req := f.requests()
			if len(req) != 1 {
				t.Fatalf("发了 %d 个请求，want 1", len(req))
			}
			got := req[0]
			if got.Method != http.MethodPost {
				t.Errorf("用的是 %s，want POST", got.Method)
			}
			if got.Form.Get("bvid") != "BV1xx" {
				t.Errorf("bvid 传的是 %q", got.Form.Get("bvid"))
			}
			if got.Form.Get("up") != c.up {
				t.Errorf("up 传的是 %q，want %q", got.Form.Get("up"), c.up)
			}
			if got.Query.Get("csrf") != "the-jct-value" {
				t.Errorf("查询串里没有 csrf")
			}
			if got.Header.Get(CSRFHeaderName) != "the-jct-value" {
				t.Errorf("头里没有 %s", CSRFHeaderName)
			}
		})
	}
}

// TestLikeArchiveNeverAsksTwice 钉住「写操作不重试」。
//
// 这不是「顺便测一下」：PLAN §5 的重试表写死了写操作绝不自动重试，而这一层现在
// 没有重试——**将来有人加一层通用的重试（很常见的优化）时，这条会立刻红**，
// 而那正是要拦的东西。
//
// 造的是「第一次回 -101（没登录）」：真要重试的话 B 站会回同样的东西，而用户的
// 赞会被写两次或者被取消一次。
func TestLikeArchiveNeverAsksTwice(t *testing.T) {
	f := likeFixture(t, false, false, nil)
	f.on(pathArchiveLike, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, jsonData(t, -101, "账号未登录", nil))
	})
	err := likeClient(t, f).LikeArchive(context.Background(), "BV1xx", true)
	if err == nil {
		t.Fatal("没登录却回 nil 了")
	}
	if !errors.Is(err, ErrNotLoggedIn) {
		t.Errorf("没登录给的却是 %v", err)
	}
	if n := len(f.requests()); n != 1 {
		t.Errorf("一次失败发了 %d 个请求，want 1（写操作不重试）", n)
	}
}

// TestLikeArchiveOfNothingSaysSoWithoutAsking 钉住「没有 bvid 当场说」。
//
// 和重试没关系，是「参数给错当场说、别发请求」那条（`audio.go:425`）。
func TestLikeArchiveOfNothingSaysSoWithoutAsking(t *testing.T) {
	f := likeFixture(t, false, false, nil)
	if err := likeClient(t, f).LikeArchive(context.Background(), "", true); !errors.Is(err, ErrNoVideo) {
		t.Errorf("空 bvid 给的是 %v，want ErrNoVideo", err)
	}
	if n := len(f.requests()); n != 0 {
		t.Errorf("发了 %d 个请求，want 0", n)
	}
}

// ---- 三连 ----

// TestLikeTripleAsksForTheAidNotTheBvid 是长按 2.5 秒那一次写。
//
// 🔴 **收的是 avid**：照 bvid 发过去，真接口会回「参数错误」，而界面上是
// 「一键三连没成功」——用户看到的是三连失败了，而真相是我们发错了 id。
// 顺带钉住 bvid → aid 那一次往返**在这一层**（界面上不该知道这个接口要的是
// 另一个 id，理由同 `favorite.go` 文件头）。
func TestLikeTripleAsksForTheAidNotTheBvid(t *testing.T) {
	f := likeFixture(t, false, false, nil)
	if _, err := likeClient(t, f).LikeTriple(context.Background(), "BV1xx"); err != nil {
		t.Fatalf("三连：%v", err)
	}

	req := f.requests()
	var view, triple *recordedRequest
	for i := range req {
		switch req[i].Path {
		case pathView:
			view = &req[i]
		case pathArchiveTriple:
			triple = &req[i]
		}
	}
	if view == nil || triple == nil {
		t.Fatalf("该发的是先 view 后 triple，实际路径 %v", pathsOf(req))
	}
	if got := triple.Form.Get("aid"); got != itoa(likeAid) {
		t.Errorf("aid 传的是 %q，want %d（真接口收的是 avid，不是 bvid）", got, likeAid)
	}
	if triple.Method != http.MethodPost {
		t.Errorf("三连用的是 %s，want POST", triple.Method)
	}
	if triple.Query.Get("csrf") != "the-jct-value" {
		t.Error("三连的查询串里没有 csrf")
	}
}

// TestTheTripleReportsWhatBiliActuallyDid 是这一票最容易做错的一半。
//
// 长按那一把发出去的是「赞 + 投币 + 收藏」，而 B 站**可能只做成一件**：硬币不够、
// 这一首投过了、风控拦了。界面上的三样必须按 B 站的答案写，不能按我们请求的那三个
// true 写——用户看得见有没有投出去，而界面说「投了」而用户手上没有那是撒谎。
//
// 所以这里造一份「赞成功、投币被拒」的回包。
func TestTheTripleReportsWhatBiliActuallyDid(t *testing.T) {
	f := likeFixture(t, false, false, map[string]any{
		"like": true, "coin": false, "fav": false, "multiply": 1,
	})
	res, err := likeClient(t, f).LikeTriple(context.Background(), "BV1xx")
	if err != nil {
		t.Fatalf("三连：%v", err)
	}
	if !res.Liked {
		t.Error("赞没认出来")
	}
	if res.Coined {
		t.Error("B 站说没投成，界面上却说投了——用户手上没有那一枚币")
	}
	if res.Favorited {
		t.Error("B 站说没收藏，界面上却说收藏了")
	}
}

// TestTheTripleCarriesTheFavoriteToo 钉住「收藏那一栏也按三连的答案更新」。
//
// 这是「三连成功后收藏状态同步更新」那一句在服务层的落点：B 站的回包里**有**
// `fav` 那一栏，而长按那一把顺手收藏了这一首。丢掉它的话播放条上的收藏状态会
// 和 B 站对不上（用户点开收藏夹发现没有，而界面上写着收藏了）。
func TestTheTripleCarriesTheFavoriteToo(t *testing.T) {
	f := likeFixture(t, false, false, nil)
	res, err := likeClient(t, f).LikeTriple(context.Background(), "BV1xx")
	if err != nil {
		t.Fatalf("三连：%v", err)
	}
	if !res.Liked || !res.Coined || !res.Favorited {
		t.Errorf("回包是 %+v，want 三样都成了", res)
	}
}

// TestTheTripleOnAFailureIsNotAFavorite 钉住「失败时什么都不改」。
//
// 回一个 -10403（B 站对稿件状态的判断）：三连没成，而界面上的赞和收藏**不许**因为
// 这一次失败就动——用户手上什么都没有，界面上多出一个赞是凭空造出来的。
func TestTheTripleOnAFailureIsNotAFavorite(t *testing.T) {
	f := likeFixture(t, false, false, nil)
	f.on(pathArchiveTriple, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, jsonData(t, -10403, "稿件不可见", nil))
	})
	res, err := likeClient(t, f).LikeTriple(context.Background(), "BV1xx")
	if err == nil {
		t.Fatal("失败了却回 nil")
	}
	if res.Liked || res.Coined || res.Favorited {
		t.Errorf("失败却读出了 %+v：界面会照它把三样都点亮，而用户手上一样都没有", res)
	}
}

// TestTheTripleNeverAsksTwice 是三连那边的「不重试」。
//
// 理由比赞更硬：三连里有**投币**，那是花真金白银的。重试一次就是两枚币。
func TestTheTripleNeverAsksTwice(t *testing.T) {
	f := likeFixture(t, false, false, nil)
	f.on(pathArchiveTriple, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, jsonData(t, -101, "账号未登录", nil))
	})
	if _, err := likeClient(t, f).LikeTriple(context.Background(), "BV1xx"); err == nil {
		t.Fatal("没登录却回 nil 了")
	}
	var triples int
	for _, r := range f.requests() {
		if r.Path == pathArchiveTriple {
			triples++
		}
	}
	if triples != 1 {
		t.Errorf("三连发了 %d 次，want 1（投币重试一次就是两枚币）", triples)
	}
}

// ---- 小工具 ----

// itoa 是这一份测试里的 strconv 缩写（两个用例要它，写全反而绕）。
func itoa(v int64) string {
	const digits = "0123456789"
	if v == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = digits[v%10]
		v /= 10
	}
	return string(buf[i:])
}

// pathsOf 是这几个请求的路径，排错用。
func pathsOf(req []recordedRequest) []string {
	out := make([]string, 0, len(req))
	for _, r := range req {
		out = append(out, r.Path)
	}
	return out
}
