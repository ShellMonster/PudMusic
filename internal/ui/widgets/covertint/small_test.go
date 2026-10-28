package covertint

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"net/http"
	"strings"
	"testing"
)

// 取色那一趟**不下原图**。
//
// # 为什么这是这一票的全部
//
// 歌单页头图区那层「封面主色光晕」要的是**一个颜色**，而从前为了算它把封面
// 原图整张解码了一遍：实测 3456×2160 的原图解码 **1375ms**，而取色算法本身
// 只要 4.5ms——**解码占了 99.9%**。用户看到的「顶部颜色渲染慢」，实际是
// 「顶部颜色算得慢」。
//
// 这一条钉住的是「请求的是小图」：**下载 + 解码一起降两个数量级**。

func TestTheTintAsksForASmallCoverAndNotTheOriginal(t *testing.T) {
	got, cli := fakeBili(t, 120, 68, color.RGBA{30, 80, 220, 255})

	if _, ok, err := Fetch(context.Background(), cli, biliCover); err != nil || !ok {
		t.Fatalf("取色失败：ok=%v err=%v", ok, err)
	}

	// 必须带「缩到 N 像素」的参数。
	if !strings.Contains(*got, "@") {
		t.Fatalf("取色请求的是原图（%q）——它会把几 MB 的原图整张解码一遍", *got)
	}
	// ⚠️ **不能带 `_1c`**：那是**中心裁剪**，会切掉画面两边，而主色常常在边上。
	//
	// 实测 60 张真封面：中心裁正方（`@96w_96h_1c`）的色相偏差
	// **max 172.9° / avg 10.88°**——那是取到了另一个颜色；等比缩放
	// （`@96w_96h`）只有 **max 8.27° / avg 0.90°**。
	if strings.Contains(*got, "_1c") {
		t.Fatalf("取色请求带了 `_1c`（%q）——那是中心裁剪，会严重偏色", *got)
	}
}

// 要的那一档**够小**：小到解码不花钱，又够大到色相稳。
//
// 96 那一档是量出来的：等比缩放到长边 96 时，60 张真封面的色相偏差
// max 8.27° / avg 0.90°；缩到 48 就开始飘（max 31.52°）。
func TestTheTintAsksForASmallEnoughCover(t *testing.T) {
	got, cli := fakeBili(t, 120, 68, color.RGBA{30, 80, 220, 255})

	if _, _, err := Fetch(context.Background(), cli, biliCover); err != nil {
		t.Fatal(err)
	}
	side := askedSide(*got)
	if side <= 0 {
		t.Fatalf("请求里看不出要了多大的图：%q", *got)
	}
	if side > 128 {
		t.Fatalf("要了 %d 像素的图（%q）——那还是接近原图，解码要几百毫秒", side, *got)
	}
}

// 网易云那一档**不变**：`?param=WxH` 是它自己的缩图语法，不该被 B 站那一套
// 盖掉。
func TestTheNeteaseCoverKeepsItsOwnScaling(t *testing.T) {
	if got := scalingFor("https://p1.music.126.net/abc.jpg"); got != "param=48y48" {
		t.Fatalf("网易云的缩图参数变成了 %q", got)
	}
}

// 已经带过参数的地址**原样返回**：图床自己裁好的那一档，再拼一次就成了
// 两个 `@`，那是坏地址。
func TestAnAlreadyScaledCoverIsLeftAlone(t *testing.T) {
	if got := scalingFor("https://i0.hdslb.com/bfs/archive/x.jpg@672w_378h_1c.webp"); got != "" {
		t.Fatalf("已经带过参数的地址又被拼了一次：%q", got)
	}
}

// 不是 B 站图床的地址**不拼**：别的图床不认这一套参数，拼了就是坏地址。
func TestAForeignImageHostIsLeftAlone(t *testing.T) {
	if got := scalingFor("https://example.com/cover.jpg"); got != "" {
		t.Fatalf("给非图床的地址拼了参数：%q", got)
	}
}

// 取出来的色**和原图取的一样**（这一档夹具是纯色封面，两种取法必然一致）。
//
// ⚠️ 这一条量的是「换了小图之后，颜色没变」——不是「小图够不够准」。准不准
// 由 `TestTheSmallCoverKeepsTheColour` 那一条拿真图量。
func TestTheTintIsTheSameAsTheOriginalsColour(t *testing.T) {
	_, cli := fakeBili(t, 320, 180, color.RGBA{220, 40, 30, 255})

	h, ok, err := Fetch(context.Background(), cli, biliCover)
	if err != nil || !ok {
		t.Fatalf("取色失败：ok=%v err=%v", ok, err)
	}
	// 红色的色相在 355~5 之间。
	if h.H > 10 && h.H < 350 {
		t.Fatalf("红色封面取出 %+v，色相应在 355~5", h)
	}
}

// writeCover 画一张纯色图。
func writeCover(w http.ResponseWriter, width, height int, c color.RGBA) {
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := range height {
		for x := range width {
			img.Set(x, y, c)
		}
	}
	// 写成 JPEG（真实封面就是这个格式）。
	w.Header().Set("Content-Type", "image/jpeg")
	_ = encodeJPEG(w, img)
}

// askedSide 从请求串里读出要的长边（`@96w_96h` → 96）。
func askedSide(uri string) int {
	at := strings.Index(uri, "@")
	if at < 0 {
		return 0
	}
	rest := uri[at+1:]
	end := strings.IndexAny(rest, "._?")
	if end < 0 {
		end = len(rest)
	}
	head := rest[:end]
	parts := strings.SplitN(head, "w_", 2)
	if len(parts) == 0 {
		return 0
	}
	var n int
	if _, err := fmt.Sscanf(parts[0], "%d", &n); err != nil {
		return 0
	}
	return n
}

// encodeJPEG 把图写成 JPEG 字节。
func encodeJPEG(w interface{ Write([]byte) (int, error) }, img image.Image) error {
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, nil); err != nil {
		return err
	}
	_, err := w.Write(buf.Bytes())
	return err
}

// biliCover 是一个**真实的 B 站图床地址**。
//
// ⚠️ **不能用 httptest 的 127.0.0.1**：那一趟要判「是不是 B 站图床」才决定
// 拼不拼参数，而 127.0.0.1 不是图床——那样测的是「假地址不拼参数」，不是
// 「真封面拼了参数」。所以这里配一个**假 transport**：地址是真的（判 host 的
// 那段代码真的跑到了），发出去的那一趟被拦下来换成夹具。
const biliCover = "https://i0.hdslb.com/bfs/archive/cover.jpg"

// fakeBili 造一个「什么地址都收、但把请求记下来」的客户端。
func fakeBili(t *testing.T, width, height int, c color.RGBA) (*string, *http.Client) {
	t.Helper()
	got := new(string)
	cli := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		*got = r.URL.RequestURI()
		body := new(bytes.Buffer)
		img := image.NewRGBA(image.Rect(0, 0, width, height))
		for y := range height {
			for x := range width {
				img.Set(x, y, c)
			}
		}
		if err := jpeg.Encode(body, img, nil); err != nil {
			return nil, err
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(body),
			Header:     make(http.Header),
		}, nil
	})}
	return got, cli
}

// roundTripFunc 把一个函数当成 RoundTripper。
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// 这个包**自己能解 WebP**——不靠 `internal/ui` 顺带注册。
//
// # 为什么
//
// B 站图床给的封面有一档就是 `.webp`（`imgcache` 一直用 `@…webp`），
// 而 `image.Decode` 认不认 WebP 取决于**这个二进制里有没有那个解码器**。
//
// 从前这个包只注册了 jpeg/png，它是被 `internal/ui` 用的——那边导入了
// `mygo/ui`，WebP 解码器**顺带**注册上了，于是**碰巧**能解。那是依赖了
// 别人的副作用：这个包单独用就解不了，而症状是「取不到色」——**静默的**。
func TestThisPackageDecodesWebPOnItsOwn(t *testing.T) {
	// 一枚 4×4 的蓝色 WebP（VP8L 无损）。
	img, _, err := image.Decode(bytes.NewReader(blueWebP))
	if err != nil {
		t.Fatalf("这个包解不了 webp：%v（它是靠 `internal/ui` 顺带注册的）", err)
	}
	if b := img.Bounds(); b.Dx() != 4 || b.Dy() != 4 {
		t.Fatalf("解出来的尺寸不对：%+v", b)
	}
	// 顺手确认**颜色也对**（不是解出来一片空白）。
	r, g, bl, _ := img.At(2, 2).RGBA()
	if r>>8 < 20 || g>>8 < 60 || bl>>8 < 180 {
		t.Fatalf("解出来的颜色不对：%d,%d,%d（该是蓝的）", r>>8, g>>8, bl>>8)
	}
}

// blueWebP 是一枚 4×4 的**蓝色** WebP（VP8L 无损）。
//
// 它是**内嵌的字节**，不是从网上下的——测试不许依赖网络。
//
// ⚠️ VP8L 的负载是「位打包」的，所以这个串**不能**随手改：改一个字节就
// 不是合法的 WebP 了。要换尺寸或换颜色就重新编一枚。
var blueWebP = []byte{
	0x52, 0x49, 0x46, 0x46, // "RIFF"
	0x1e, 0x00, 0x00, 0x00, // 文件大小（不含头 8 字节）
	0x57, 0x45, 0x42, 0x50, // "WEBP"
	0x56, 0x50, 0x38, 0x4c, // "VP8L"
	0x11, 0x00, 0x00, 0x00, // 负载长度
	0x2f, 0x03, 0xc0, 0x00,
	0x00, 0x07, 0x50, 0xa8,
	0x7a, 0x94, 0xbb, 0xff,
	0x81, 0x88, 0xe8, 0x7f,
	0x00, 0x00,
}
