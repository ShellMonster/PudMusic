package bili

// 这一组测的是「补分 P id」那条路：搜出来的歌只有 bvid，放它之前要问一次该放哪一集
// （见 video.go 的文件头）。

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"
)

// videoFixture 建一台会答分 P 列表的假 B 站。
//
// ⚠️ `data` 必须是**数组**（上游 `player-pagelist.ts:18` 的
// `data: PlayerPagelistItem[]`）。第一版这里造的是 `data: {pages: [...]}`，而实现
// 也照着那个错形状写，于是全绿；真接口回数组时补 cid 静默失败，歌照样放不出来。
// 评审抓到的那条就是这个——**fixture 的形状必须照着真接口抄**。
func videoFixture(t *testing.T, data []map[string]any, code int) (*Client, *fakeBili) {
	t.Helper()
	f := newFakeBili(t)
	f.on(videoPagesPath, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, jsonData(t, code, "0", data))
	})
	return f.client(t, Options{Now: func() time.Time { return time.Unix(1702204169, 0) }}), f
}

// partItem 造一个分 P 项，字段照上游的 `PlayerPagelistItem`。
func partItem(cid, page int, part string) map[string]any {
	return map[string]any{
		"cid": cid, "page": page, "part": part, "duration": 260,
		"from": "vupload", "vid": "", "weblink": "",
		"dimension":   map[string]any{"width": 1920, "height": 1080, "rotate": 0},
		"first_frame": "//i0.hdslb.com/bfs/storyff/n10500.jpg",
	}
}

// TestVideoPagesReadsTheArrayContract 是「列表读对了」那一条，**数组形状**。
func TestVideoPagesReadsTheArrayContract(t *testing.T) {
	c, f := videoFixture(t, []map[string]any{
		partItem(777, 1, "第一集"),
		partItem(778, 2, "第二集"),
		partItem(0, 3, "没给 cid 的那一项"),
	}, 0)

	pages, err := c.VideoPages(context.Background(), "BV1MbznYoEk1")
	if err != nil {
		t.Fatalf("VideoPages: %v", err)
	}
	if len(pages) != 2 {
		t.Fatalf("读出 %d 个分 P，want 2（没 cid 的那一项不算）", len(pages))
	}
	if pages[0].Cid != "777" || pages[1].Cid != "778" {
		t.Errorf("cid 读错了：%q / %q", pages[0].Cid, pages[1].Cid)
	}
	if pages[1].Page != 2 || pages[1].Part != "第二集" {
		t.Errorf("第几集 / 标题读错了：%+v", pages[1])
	}
	if pages[0].Duration != 260*time.Second {
		t.Errorf("时长 %v，want 260s", pages[0].Duration)
	}
	if got := f.requests()[0].Query.Get("bvid"); got != "BV1MbznYoEk1" {
		t.Errorf("请求带的 bvid 是 %q", got)
	}
}

// TestVideoPagesRefusesTheWrongShape 钉住「形状不对要报错，不许悄悄当成空」。
//
// 这条正对着第一版的病：真接口回数组、代码按对象读，报的是
// `cannot unmarshal array into Go value of type ...`。要是那一层再把这个错吞掉、
// 当成「没有分 P」往下走，补 cid 就静默失败了——歌放不出来，而日志上什么都看不见。
func TestVideoPagesRefusesTheWrongShape(t *testing.T) {
	f := newFakeBili(t)
	f.on(videoPagesPath, func(w http.ResponseWriter, r *http.Request) {
		// 故意回对象（第一版实现认的那个形状）。
		writeJSON(w, jsonData(t, 0, "0", map[string]any{"pages": []any{partItem(777, 1, "x")}}))
	})
	c := f.client(t, Options{Now: func() time.Time { return time.Unix(1702204169, 0) }})

	if _, err := c.VideoPages(context.Background(), "BV1"); err == nil {
		t.Fatalf("形状不对却读成了，没报错")
	} else if !strings.Contains(err.Error(), "数组") {
		t.Errorf("err = %v，报错里要写明「data 是数组」（那是真接口的形状）", err)
	}
}

// TestVideoPagesOnASinglePartVideo 盯「只有一个分 P 的稿件」（大多数）也是数组。
func TestVideoPagesOnASinglePartVideo(t *testing.T) {
	c, _ := videoFixture(t, []map[string]any{partItem(999, 1, "整首")}, 0)

	pages, err := c.VideoPages(context.Background(), "BV1")
	if err != nil {
		t.Fatalf("VideoPages: %v", err)
	}
	if len(pages) != 1 || pages[0].Cid != "999" {
		t.Fatalf("读出 %+v，want 一个 cid=999 的分 P", pages)
	}
}

// TestVideoPagesSaysNoVideo 盯「B 站说没有这个稿件」和「网络断了」分得开。
//
// 分不开的话，调用方会把「这个稿件没了」当成「网络不好，重试一下」——于是对一个
// 已经删掉的稿件一直重试。
func TestVideoPagesSaysNoVideo(t *testing.T) {
	c, _ := videoFixture(t, nil, -404)
	if _, err := c.VideoPages(context.Background(), "BV1"); !errors.Is(err, ErrNoVideo) {
		t.Errorf("err = %v，want 一个认得出「没有这个稿件」的错", err)
	}
	// 空数组也算同一类。
	c2, _ := videoFixture(t, []map[string]any{}, 0)
	if _, err := c2.VideoPages(context.Background(), "BV1"); !errors.Is(err, ErrNoVideo) {
		t.Errorf("空列表 err = %v，want ErrNoVideo", err)
	}
	// 全都没有 cid 的也算——那种列表取回来用不了。
	c3, _ := videoFixture(t, []map[string]any{partItem(0, 1, "没有 cid")}, 0)
	if _, err := c3.VideoPages(context.Background(), "BV1"); !errors.Is(err, ErrNoVideo) {
		t.Errorf("全是没 cid 的那一项，err = %v，want ErrNoVideo", err)
	}
	// bvid 是空的：调用方给错了参数，同样不是网络错。
	c4, _ := videoFixture(t, nil, 0)
	if _, err := c4.VideoPages(context.Background(), ""); !errors.Is(err, ErrNoVideo) {
		t.Errorf("空 bvid err = %v，want ErrNoVideo", err)
	}
}

// TestFirstVideoCid 是「压根没给 cid 时该用的那一集」。
func TestFirstVideoCid(t *testing.T) {
	c, _ := videoFixture(t, []map[string]any{
		partItem(777, 1, "第一集"),
		partItem(778, 2, "第二集"),
	}, 0)

	got, err := c.FirstVideoCid(context.Background(), "BV1")
	if err != nil {
		t.Fatalf("FirstVideoCid: %v", err)
	}
	if got != "777" {
		t.Errorf("拿到的是 %q，want 777（第一集）", got)
	}
}

// TestPlayURLRefusesAMissingCid 盯 cid 仍然是必填。
//
// 第一版为了「搜出来的歌放得起来」把 cid 改成可选、空的时候不发那个参数——那是拿
// 一个**没验过**的假设换功能：上游 `player-playurl.ts:9` 把 cid 写成必填，而本地
// fixture 放不放假参数，所以「B 站会不会老实按第一集给」根本没人验过。真接口回一
// 段不知道是哪一集的音频时，能出声，但可能不是用户点的那个。宁可问不出来就说清楚。
func TestPlayURLRefusesAMissingCid(t *testing.T) {
	f := newFakeBili(t)
	f.on(pathNav, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, jsonData(t, 0, "0", map[string]any{
			"wbi_img": map[string]string{
				"img_url": "https://i0.hdslb.com/bfs/wbi/" + sampleImgKey + ".png",
				"sub_url": "https://i0.hdslb.com/bfs/wbi/" + sampleSubKey + ".png",
			},
		}))
	})
	c := f.client(t, Options{Now: func() time.Time { return time.Unix(1702204169, 0) }})

	if _, err := c.PlayURL(context.Background(), "BV1", "", AudioHigh); !errors.Is(err, ErrNoStream) {
		t.Errorf("没有 cid 也放行了，err = %v", err)
	}
	// 而且**一个请求都不该发**：参数不全时发出去也是白费一次带签名的往返。
	for _, r := range f.requests() {
		if r.Path == playurlPath {
			t.Errorf("参数不全还发了取流请求（cid=%q）", r.Query.Get("cid"))
		}
	}
}
