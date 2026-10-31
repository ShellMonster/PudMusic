package imgcache

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// **存的那一份可以比 `maxStoredSide` 大**（票 #666）。
//
// # 为什么
//
// 满屏播放器的背景按**画布尺寸**要图（高清档画布 1200×800），而
// `maxStoredSide = 768` 会把**解下来那一份**钉在 768 —— 于是要到一张大图、
// 解下来还是 768、再被 GPU 拉大，**清晰度白给了**。
//
// ⚠️ **这不是「整张解进内存」**：流式解码只解到目标尺寸就停，内存峰值和源图
// 多大无关（见 `TestStreamingDecodeMemoryIsIndependentOfTheSourceSize`）。
func TestTheStoredCopyCanBeLargerThanTheDefaultCap(t *testing.T) {
	// 一张 2400×1600 的图。
	src := image.NewRGBA(image.Rect(0, 0, 2400, 1600))
	for y := 0; y < 1600; y += 40 {
		for x := 0; x < 2400; x += 40 {
			src.Set(x, y, color.RGBA{uint8(x % 256), uint8(y % 256), 120, 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, src, &jpeg.Options{Quality: 80}); err != nil {
		t.Fatal(err)
	}

	// 默认（0 = maxStoredSide）：长边缩到 768。
	img, handled, err := decodeJPEGStored(buf.Bytes(), 0)
	if err != nil || !handled {
		t.Fatalf("默认那条解失败：handled=%v err=%v", handled, err)
	}
	if got := img.Bounds().Dx(); got != 768 {
		t.Errorf("默认那条的长边是 %d，want 768", got)
	}

	// 传一个更大的目标：解出来就是那么大（**不再被 768 夹**）。
	img, handled, err = decodeJPEGStored(buf.Bytes(), 1600)
	if err != nil || !handled {
		t.Fatalf("大目标那条解失败：handled=%v err=%v", handled, err)
	}
	if got := max(img.Bounds().Dx(), img.Bounds().Dy()); got != 1600 {
		t.Errorf("传目标 1600 时解出来长边是 %d，want 1600——它又被 768 夹住了", got)
	}
}

// **端到端**：`GetSizedStoredAt` 传下去的那个尺寸，真的到了解码器手里。
//
// ⚠️ **这一条和上面那条分工不同**：上面那条直接调 `decodeJPEGStored`（测解码器
// 本身吃不吃这个参数）；这一条走**整条链路**（`GetSizedStoredAt` → entry →
// job → 解码器）——把「job 里那个字段忘了传」这类断链抓住。
func TestTheStoredSideReachesTheDecoderEndToEnd(t *testing.T) {
	// 假图床：给一张 2400×1600 的图（**不管请求的档位**——这一条测的是
	// 「解下来存多大」，不是「要了多大的」）。
	src := image.NewRGBA(image.Rect(0, 0, 2400, 1600))
	for y := 0; y < 1600; y += 40 {
		for x := 0; x < 2400; x += 40 {
			src.Set(x, y, color.RGBA{uint8(x % 256), uint8(y % 256), 120, 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, src, &jpeg.Options{Quality: 80}); err != nil {
		t.Fatal(err)
	}
	body := buf.Bytes()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write(body)
	}))
	defer srv.Close()

	c := New(Options{})
	t.Cleanup(c.Close)
	c.SetScale(1)

	// 要一个 800×600 DIP 的格子，但让它**存 1600 长边**（高清档那种）。
	url := srv.URL + "/backdrop.jpg"
	c.GetSizedStoredAt(url, 800, 600, 1600)
	waitState(t, c, cacheKey(sizedURL(url, 800, 600), 1600), 5*time.Second, loaded)

	// 把那一份拿回来量长边。
	v, ok := c.entries.Load(cacheKey(sizedURL(url, 800, 600), 1600))
	if !ok {
		t.Fatal("缓存里没有那一条")
	}
	e := v.(*entry)
	e.mu.Lock()
	bm := e.bm
	e.mu.Unlock()
	if bm == nil {
		t.Fatal("那一条还没有图")
	}
	w, h := bm.Size()
	if got := max(w, h); got != 1600 {
		t.Errorf("存下来的长边是 %d，want 1600——job 里那个 maxSide 没传下去"+
			"（或者解码器没用它）；实际尺寸 %dx%d", got, w, h)
	}
}
