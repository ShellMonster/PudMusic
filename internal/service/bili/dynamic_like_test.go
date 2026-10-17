// 给动态点赞 / 取消点赞那条接口的测试。
//
// ⚠️ 和这一包里其它 fixture 测试一样：这里没有一个断言在证明「B 站真的这么
// 返回」。它证明的是**我们发出去的形状**——打哪条路径、请求体是不是 JSON、
// up 取的是 1 还是 2。真接口的兼容性只能靠真机回归（而这条要登录态）。

package bili

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
)

// thumbBody 收下点赞那一次的请求体。
type thumbBody struct {
	DynID string `json:"dyn_id_str"`
	Up    int    `json:"up"`
}

// serveThumb 让假 B 站答得上来点赞，并记下收到的那个体。
func serveThumb(t *testing.T, f *fakeBili) *thumbBody {
	t.Helper()
	var got thumbBody
	f.on(dynamicThumbPath, func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Errorf("请求体不是 JSON：%v", err)
		}
		writeJSON(w, jsonData(t, 0, "0", map[string]any{}))
	})
	return &got
}

// TestLikeDynamicPostsThatBodyAndThatPath 点赞：路径、JSON 体、up=1。
//
// ⚠️ **两个都不是能猜的**：
//
//   - 打的是 `/x/dynamic/feed/dyn/thumb`（给**这一条动态**点赞），不是
//     `/x/v2/dynamic/feed/like/thumb`（那个给**稿件**点赞）。两个长得像，
//     评的不是一回事，而后者在动态流里会让多个 UP 转发同一支片子时赞数自己往上加。
//   - 请求体是 **JSON**（`{"dyn_id_str":"…","up":1}`）。写成表单的话 B 站回的是
//     「参数错误」（4100001）而不是 csrf 校验失败——排障方向从第一步就错了。
func TestLikeDynamicPostsThatBodyAndThatPath(t *testing.T) {
	f := newFakeBili(t)
	got := serveThumb(t, f)

	err := f.client(t, Options{CSRF: func() string { return "jct" }}).
		LikeDynamic(context.Background(), "1057955152016703512", true)
	if err != nil {
		t.Fatal(err)
	}

	if got.DynID != "1057955152016703512" {
		t.Errorf("发出去的动态 id 是 %q", got.DynID)
	}
	if got.Up != 1 {
		t.Errorf("up = %d，want 1（点赞）——0 是「切换」，我们不用它", got.Up)
	}

	reqs := f.requests()
	if len(reqs) != 1 {
		t.Fatalf("发了 %d 次请求，want 1", len(reqs))
	}
	if reqs[0].Path != dynamicThumbPath {
		t.Errorf("打的是 %q，want %q", reqs[0].Path, dynamicThumbPath)
	}
	// csrf 走查询串和 `bili-jct` 头（体里没有）。
	if reqs[0].Query.Get("csrf") != "jct" {
		t.Errorf("csrf = %q，want jct", reqs[0].Query.Get("csrf"))
	}
	if reqs[0].Header.Get(CSRFHeaderName) != "jct" {
		t.Errorf("%s 头是 %q，want jct", CSRFHeaderName, reqs[0].Header.Get(CSRFHeaderName))
	}
}

// TestUnlikingSendsTwo 「切换」（up=0）会在界面重画之后点成反的，所以取消收藏
// 要显式发 2。
func TestUnlikingSendsTwo(t *testing.T) {
	f := newFakeBili(t)
	got := serveThumb(t, f)

	err := f.client(t, Options{CSRF: func() string { return "jct" }}).
		LikeDynamic(context.Background(), "v1", false)
	if err != nil {
		t.Fatal(err)
	}
	if got.Up != 2 {
		t.Errorf("up = %d，want 2（取消点赞）", got.Up)
	}
}

// TestLikeDynamicNeedsAnID 空 id 不该发请求。
func TestLikeDynamicNeedsAnID(t *testing.T) {
	f := newFakeBili(t)
	serveThumb(t, f)

	err := f.client(t, Options{}).LikeDynamic(context.Background(), "  ", true)
	if err == nil {
		t.Fatal("空 id 也当成功")
	}
	if n := len(f.requests()); n != 0 {
		t.Errorf("空 id 还是发了 %d 次请求", n)
	}
}
