package list

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// 这一组验的是**卡片画出来的东西**：封面、标题、UP 名。
//
// 封面的四态（空地址 / 载入中 / 就绪 / 失败）本身是 imgcache 的事，那边已经
// 验过了（widgets/imgcache/view_test.go）。这里验的是**接进来之后对不对**：
// 封面画在对的地方、有歌名、有 UP 名，而且封面加载中不会把整行顶没。

// pngBytes 造一张纯色 PNG 当封面。
func pngBytes(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.SetRGBA(x, y, color.RGBA{R: 200, G: 40, B: 120, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// TestTheCardShowsTheTitleAndTheOwner 是验收「音乐卡片显示封面、标题、UP 名」
// 的后两样。断言的是画面上的字，不是「调了哪个函数」。
func TestTheCardShowsTheTitleAndTheOwner(t *testing.T) {
	_, tt := newList(t, Actions{}, []Music{
		song("a", "夜空中最亮的星", "逃跑计划"),
	})

	if !tt.HasText("夜空中最亮的星") {
		t.Errorf("歌名没画出来，文字有 %q", tt.Texts())
	}
	if !tt.HasText("逃跑计划") {
		t.Errorf("UP 名没画出来，文字有 %q", tt.Texts())
	}
}

// TestASongWithoutAnOwnerDoesNotDrawOne 验没有 UP 名时不画空位。
//
// Biu 的 music-card 是 `{ownerName && (...)}`（`music-card/index.tsx`）：没有
// 就不画。这里跟着来——画一行空的出来，列表里看起来是「这一首没有 UP 名」，
// 而真实原因是这一项本来就没填。
func TestASongWithoutAnOwnerDoesNotDrawOne(t *testing.T) {
	_, tt := newList(t, Actions{}, []Music{
		song("a", "没有 UP 名的歌", ""),
	})
	if !tt.HasText("没有 UP 名的歌") {
		t.Errorf("歌名没画出来，文字有 %q", tt.Texts())
	}
	for _, s := range tt.Texts() {
		if s == "" {
			t.Error("画面上有一个空的文字节点")
		}
	}
}

// TestTheCoverGoesThroughImgcache 验封面走的是已经建好的那套缓存：图还没来时
// 显示「载入中」，来了自己换上去，中间这一行始终摆在那儿。
func TestTheCoverGoesThroughImgcache(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		<-release
		w.Write(pngBytes(t, coverSize, coverSize))
	}))
	defer srv.Close()

	_, tt := newList(t, Actions{}, []Music{
		{ID: "a", Title: "有封面的歌", Owner: "UP 甲", Cover: srv.URL + "/cover.png"},
	})

	if !tt.HasText("载入中") {
		t.Errorf("第一帧没有占位，文字有 %q", tt.Texts())
	}
	if tt.HasText("封面加载失败") {
		t.Errorf("还没下完就画了失败态，文字有 %q", tt.Texts())
	}
	close(release)

	waitFor(t, 5*time.Second, "封面换成了图", func() bool {
		tt.Frame()
		return !tt.HasText("载入中")
	})
	if tt.HasText("封面加载失败") {
		t.Errorf("下完了却画了失败态，文字有 %q", tt.Texts())
	}
	// 换封面不能把这一行弄没。
	if !tt.HasText("有封面的歌") || !tt.HasText("UP 甲") {
		t.Errorf("封面换完之后这一行不见了，文字有 %q", tt.Texts())
	}
}

// TestALocalSongHasAnEmptyCoverNotAFailedOne 验本地文件那一行画的是空封面。
//
// 本地文件的封面永远是空的，而且是照抄 Biu 的行为（上游也不读音频里内嵌的图）。
// 空和「加载失败」在 imgcache 的 drawCover 里是分开的两支：画错了，用户看到
// 一行「封面加载失败」，会以为歌坏了。
func TestALocalSongHasAnEmptyCoverNotAFailedOne(t *testing.T) {
	_, tt := newList(t, Actions{}, []Music{
		{ID: "a", Title: "本地的一首", Owner: ""},
	})
	if tt.HasText("封面加载失败") || tt.HasText("载入中") {
		t.Errorf("空封面不该画成失败或载入中，文字有 %q", tt.Texts())
	}
}

// TestTheCoverSitsNextToTheTitle 验封面和文字是并排的一行，不是上下堆着。
//
// 判据是**画面上的位置**：UP 名那一块的左边正好落在「内边距 + 序号列 + 间距 +
// 封面 + 间距」那一处（8+40+8+48+8=112）。写死这个数是有意的——只判「比封面
// 靠右」的话，封面挪到 x=0 也照样过。
func TestTheCoverSitsNextToTheTitle(t *testing.T) {
	_, tt := newList(t, Actions{}, []Music{
		song("a", "夜空中最亮的星", "逃跑计划"),
	})

	r, ok := tt.Find("逃跑计划")
	if !ok {
		t.Fatalf("UP 名没画出来，文字有 %q", tt.Texts())
	}
	want := cardPadX + indexWidth + cardGap + coverSize + cardGap
	if d := r.X - float32(want); d > 1 || d < -1 {
		t.Errorf("UP 名那一块在 x=%v，want %v（左边就是封面），排版不是「序号 封面 文字」",
			r.X, want)
	}
}

// waitFor 轮询到 cond 成立，最多等 d。异步的东西（图下载完）只能这样等。
func waitFor(t *testing.T, d time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("等了 %v，%s 还没发生", d, what)
}

// TestTheDurationColumn 验时长那一列。
//
// 带小时的那种也要对：一小时的歌显示成「61:30」的话，用户得自己换算一次。
func TestTheDurationColumn(t *testing.T) {
	cases := []struct {
		d    time.Duration
		want string
	}{
		{3*time.Minute + 5*time.Second, "3:05"},
		{45 * time.Second, "0:45"},
		{time.Hour + 2*time.Minute, "1:02:00"},
		{0, ""}, // 没有时长就不画那一列
	}
	for _, c := range cases {
		_, tt := newList(t, Actions{}, []Music{
			{ID: "a", Title: "有时长的歌", Duration: c.d},
		})
		if got := formatDuration(c.d); got != c.want {
			t.Errorf("formatDuration(%v) = %q，want %q", c.d, got, c.want)
		}
		if c.want != "" && !tt.HasText(c.want) {
			t.Errorf("%v 的时长没画成 %q，文字有 %q", c.d, c.want, tt.Texts())
		}
	}
}
