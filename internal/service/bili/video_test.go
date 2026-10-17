package bili

// 这一组测的是「补分 P id」那条路：搜出来的歌只有 bvid，放它之前要问一次该放哪一集
// （见 video.go 的文件头）。

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"
)

// videoFixture 建一台会答分 P 列表的假 B 站。
func videoFixture(t *testing.T, data map[string]any, code int) (*Client, *fakeBili) {
	t.Helper()
	f := newFakeBili(t)
	f.on(videoPagesPath, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, jsonData(t, code, "0", data))
	})
	return f.client(t, Options{Now: func() time.Time { return time.Unix(1702204169, 0) }}), f
}

// TestVideoPagesReadsThePages 是「列表读对了」那一条。
func TestVideoPagesReadsThePages(t *testing.T) {
	c, f := videoFixture(t, map[string]any{
		"pages": []any{
			map[string]any{"cid": 777, "page": 1, "part": "第一集"},
			map[string]any{"cid": 778, "page": 2, "part": "第二集"},
			map[string]any{"cid": 0, "page": 3, "part": "没给 cid 的那一项"},
		},
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
	if got := f.requests()[0].Query.Get("bvid"); got != "BV1MbznYoEk1" {
		t.Errorf("请求带的 bvid 是 %q", got)
	}
}

// TestVideoPagesReadsTheFlatShape 盯「只有一个分 P 时 B 站把信息平铺在 data 上」那一支。
//
// 不认它的话，单 P 的稿件（大多数）会被当成「没有分 P」，于是每一首单 P 的歌都要
// 多问一次、还要么报错要么退到别处去。
func TestVideoPagesReadsTheFlatShape(t *testing.T) {
	c, _ := videoFixture(t, map[string]any{"cid": 999, "page": 1, "part": "整首"}, 0)

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
	// 列表是空的也算同一类。
	c2, _ := videoFixture(t, map[string]any{"pages": []any{}}, 0)
	if _, err := c2.VideoPages(context.Background(), "BV1"); !errors.Is(err, ErrNoVideo) {
		t.Errorf("空列表 err = %v，want ErrNoVideo", err)
	}
	// bvid 是空的：调用方给错了参数，同样不是网络错。
	c3, _ := videoFixture(t, nil, 0)
	if _, err := c3.VideoPages(context.Background(), ""); !errors.Is(err, ErrNoVideo) {
		t.Errorf("空 bvid err = %v，want ErrNoVideo", err)
	}
}

// TestFirstVideoCid 是「压根没给 cid 时该用的那一集」。
func TestFirstVideoCid(t *testing.T) {
	c, _ := videoFixture(t, map[string]any{
		"pages": []any{
			map[string]any{"cid": 777, "page": 1, "part": "第一集"},
			map[string]any{"cid": 778, "page": 2, "part": "第二集"},
		},
	}, 0)

	got, err := c.FirstVideoCid(context.Background(), "BV1")
	if err != nil {
		t.Fatalf("FirstVideoCid: %v", err)
	}
	if got != "777" {
		t.Errorf("拿到的是 %q，want 777（第一集）", got)
	}
}

// TestPlayURLWithoutCidOmitsTheParameter 盯「cid 为空时**不发** cid 参数」。
//
// 发空串和不发是两件事：B 站看到 `cid=` 可能当 0 处理，那取回来的是**别的稿件**的
// 音频——放出来是一首能出声、但完全不是用户点的歌。宁可让它按第一集给（那是这个稿件
// 自己的内容），也不要一个指到别处的 id。
func TestPlayURLWithoutCidOmitsTheParameter(t *testing.T) {
	f := newFakeBili(t)
	f.on(pathNav, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, jsonData(t, 0, "0", map[string]any{
			"wbi_img": map[string]string{
				"img_url": "https://i0.hdslb.com/bfs/wbi/" + sampleImgKey + ".png",
				"sub_url": "https://i0.hdslb.com/bfs/wbi/" + sampleSubKey + ".png",
			},
		}))
	})
	f.on(playurlPath, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, `{"code":0,"message":"0","ttl":1,"data":{"timelength":1000,
			"dash":{"duration":1,"audio":[{"id":30280,"bandwidth":110836,"codecs":"mp4a.40.2",
			"mimeType":"audio/mp4","baseUrl":"https://cdn.example/a.flac"}]}}}`)
	})
	c := f.client(t, Options{Now: func() time.Time { return time.Unix(1702204169, 0) }})

	ref, err := c.PlayURL(context.Background(), "BV1", "", AudioAuto)
	if err != nil {
		t.Fatalf("cid 为空时取流失败了：%v", err)
	}
	if ref.Bvid != "BV1" {
		t.Errorf("ref.Bvid = %q", ref.Bvid)
	}
	for _, r := range f.requests() {
		if r.Path != playurlPath {
			continue
		}
		if _, has := r.Query["cid"]; has {
			t.Errorf("请求里带着 cid=%q，want 整个参数都不发", r.Query.Get("cid"))
		}
	}
}

// TestPlayURLStillRefusesAnEmptyBvid 盯「连稿件都没有」仍然是当场拒。
//
// cid 放宽了，bvid 没有跟着放宽：没有 bvid 就不知道去问哪个稿件，那是真给错了参数，
// 换成「退到第一集」只会放出一首不相干的歌。
func TestPlayURLStillRefusesAnEmptyBvid(t *testing.T) {
	f := newFakeBili(t)
	c := f.client(t, Options{})
	if _, err := c.PlayURL(context.Background(), "", "777", AudioAuto); err == nil {
		t.Errorf("没有 bvid 也放行了")
	}
}
