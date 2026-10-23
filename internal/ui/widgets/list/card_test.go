package list

import (
	"bytes"
	"fmt"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/scale"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/hover"
	"github.com/egoist/mygo/ui"
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
// 是那一格占位，来了自己换上去，中间这一行始终摆在那儿。
//
// ⚠️ **断的是画面不是那一格上的字**：改之前占位上写着「载入中」，而现在按上游
// `components/image/index.tsx:22-32` **一个字都不写**（骨架屏 / 描边框 + 图片
// 图标）。所以唯一能断「画出来的是哪一档」的只有那一块 48×48 的像素。
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

	bx, by, bw, bh := coverBox(t, tt, "有封面的歌")
	// 第一帧是骨架屏：一块浅底 + **一枚更淡的音符**（没有描边框、没有字）。
	//
	// 🔴 **判据是「画得出东西」，不是「一块纯色」**（票 #222 / 报告 §5.3）：那一格
	// 原来是一块**纯色**——浅色主题下它和背景几乎一样，一屏里几十张封面正在加载
	// 时用户看到的是一排空白，**分不出「还在加载」和「压根没有图」**。
	loading := distinctIn(tt, bx, by, bw, bh)
	if len(loading) < 2 {
		t.Errorf("正在加载那一格是一块纯色（%v）——加载中这一档**画不出东西**，"+
			"用户分不出「还在加载」和「压根没有图」", loading)
	}

	close(release)
	// 判据是**那一块变成了夹具那张图的颜色**，而不是「和之前不一样」：
	// 这一族的 `pngBytes` 造的是**一整块纯色**（200,40,120），所以「有几种颜色」
	// 换成 1 —— 断「有几种颜色」的话，等来的那一天是换上了图、而那一天照样红。
	waitFor(t, 5*time.Second, "封面换成了图", func() bool {
		tt.Frame()
		return equalStrings(distinctIn(tt, bx, by, bw, bh), []string{"#c82878"})
	})
	// 换封面不能把这一行弄没。
	if !tt.HasText("有封面的歌") || !tt.HasText("UP 甲") {
		t.Errorf("封面换完之后这一行不见了，文字有 %q", tt.Texts())
	}
}

// TestALocalSongHasAnEmptyCoverNotAFailedOne 验本地文件那一行画的是空封面。
//
// 本地文件的封面永远是空的，而且是照抄 Biu 的行为（上游也不读音频里内嵌的图）。
// 空和「加载失败」在 imgcache 的 drawCover 里是分开的两支：画错了，用户会在那一
// 格上看到一圈描边框 + 一枚图片图标，而那说的是「没拉下来」，不是「本来就没有」。
func TestALocalSongHasAnEmptyCoverNotAFailedOne(t *testing.T) {
	_, tt := newList(t, Actions{}, []Music{
		{ID: "a", Title: "本地的一首", Owner: ""},
	})
	bx, by, bw, bh := coverBox(t, tt, "本地的一首")
	// 「本来都没有」那一格画的是**描边框 + 一枚图标**，所以有不止一种颜色；
	// 而骨架屏（正在加载）是一块纯色。只断「非空」的话，那一格画成骨架屏也过。
	if got := distinctIn(tt, bx, by, bw, bh); len(got) < 2 {
		t.Errorf("空封面那一格只有一种颜色（%v）——画的是「正在加载」那一档，"+
			"而本地文件是**本来就没有**，该是描边框加一枚图片图标", got)
	}
}

// coverBox 报封面那一块在画面上的位置。
//
// ⚠️ **必须用歌名去 `Find`，不能用 UP 名**：`Find` 认的是**画着这句话的那一块**
// ——用歌名查它给的是**整行**（`{0, 0, 520, 64}`，歌名在行里是撑满的那一块），
// 用 UP 名查它给的是 UP 名那 14 像素高的文字块。第一版拿 UP 名算，于是
// `y = 文字顶 + (文字高 − 封面) / 2` 算出一个负数，量到窗口外面全是白，
// 「有几种颜色」于是恒为三，于是每一条断言都变成恒真。
//
// 横坐标是**算出来的**（`cardPadX + indexWidth + rowGap`）：行是变高的
// （`card.go` 顶上明确不许钉死行高），所以纵坐标只能从整行那一块推。
func coverBox(t *testing.T, tt *ui.Tester, title string) (x, y, w, h int) {
	t.Helper()
	row, ok := tt.Find(title)
	if !ok {
		t.Fatalf("歌名 %q 没画出来，文字有 %q", title, tt.Texts())
	}
	if row.H < coverSize {
		t.Fatalf("查到的那一块只有 %v 高，不是整行（封面是 %d）——"+
			"拿它推纵向位置会推到窗口外面去", row.H, coverSize)
	}
	x = cardPadX + indexWidth + rowGap
	y = int(row.Y) + (int(row.H)-coverSize)/2
	// 往里收 6 像素：边框那一圈和它的抗锯齿边不算「这一格画了什么」，而
	// 「正在加载」那一档是**纯色**——不收边的话边缘的抗锯齿会把它变成六种白。
	return x + 6, y + 6, coverSize - 12, coverSize - 12
}

// distinctIn 报那一块里出现了哪几种颜色（去重、不计次序）。
func distinctIn(tt *ui.Tester, x, y, w, h int) []string {
	img := tt.Image()
	seen := map[string]bool{}
	var out []string
	for dy := range h {
		for dx := range w {
			c := img.At(x+dx, y+dy)
			r, g, b, _ := c.RGBA()
			k := fmt.Sprintf("#%02x%02x%02x", r>>8, g>>8, b>>8)
			if seen[k] {
				continue
			}
			seen[k] = true
			out = append(out, k)
		}
	}
	return out
}

// equalStrings 比两个字符串切片（次序有关）。这里比的是「就那一种颜色」。
func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestTheCoverSitsNextToTheTitle 验封面和文字是并排的一行，不是上下堆着。
//
// 判据是**画面上的位置**：UP 名那一块的左边正好落在「内边距 + 序号列 + 间距 +
// 封面 + 间距」那一处（8+32+16+48+16=120）。写死这个数是有意的——只判「比封面
// 靠右」的话，封面挪到 x=0 也照样过。
func TestTheCoverSitsNextToTheTitle(t *testing.T) {
	_, tt := newList(t, Actions{}, []Music{
		song("a", "夜空中最亮的星", "逃跑计划"),
	})

	r, ok := tt.Find("逃跑计划")
	if !ok {
		t.Fatalf("UP 名没画出来，文字有 %q", tt.Texts())
	}
	want := cardPadX + indexWidth + rowGap + coverSize + rowGap
	if d := r.X - float32(want); d > 1 || d < -1 {
		t.Errorf("UP 名那一块在 x=%v，want %v（左边就是封面），排版不是「序号 封面 文字」",
			r.X, want)
	}
}

// TestTheRowScaleFollowsTheDesign 钉住列表那一行的四个数（DESIGN.md「列表与网格」）：
// 歌名 16、UP 名 14、列间距 16、序号 32。
//
// ⚠️ **歌名那一块量不出来，所以它断的是常量**：`ui.List` 把歌名挂成了**整行**的
// 无障碍标签（`List.View` 的 `state.Label`），无头 Tester 的 `Find` 命中的是整行
// （见 `coverBox` 的注释）——歌名自己那一块的盒子在测试里取不到。别的数都有量出来
// 的断言：序号 / 间距 / 封面落在哪见 `TestTheCoverSitsNextToTheTitle`，UP 名和时长
// 谁大见 `TestTheRowTextIsBiggerThanTheNumbers`，序号居中见
// `TestTheRowIndexIsCentredInItsColumn`。
func TestTheRowScaleFollowsTheDesign(t *testing.T) {
	if scale.ListTitle != 16 {
		t.Errorf("列表歌名字号 %v，want 16（DESIGN.md「列表歌名 16」）", scale.ListTitle)
	}
	if scale.ListOwner != 14 {
		t.Errorf("列表 UP 名字号 %v，want 14（DESIGN.md「列表 UP 名 14」）", scale.ListOwner)
	}
	if rowGap != 16 {
		t.Errorf("列表列间距 %v，want 16（DESIGN.md「列表列间距 16」）", rowGap)
	}
	if indexWidth != 32 {
		t.Errorf("序号那一列 %v 宽，want 32（上游 `min-w-8`）", indexWidth)
	}
}

// TestTheRowTextIsBiggerThanTheNumbers 钉住 UP 名（14）比时长 / 播放量（12）高一档。
//
// 判据是**画出来的两块文字谁高**，不是调了哪个函数：UP 名 12 → 14 这件事要是没做，
// 两块一样高，正是改之前的样子（票面「UP 名 12 vs 上游 14」）。
func TestTheRowTextIsBiggerThanTheNumbers(t *testing.T) {
	_, tt := newList(t, Actions{}, []Music{
		{ID: "a", Title: "一首歌", Owner: "甲 UP", Duration: time.Minute + 5*time.Second},
	})

	owner, ok := tt.Find("甲 UP")
	if !ok {
		t.Fatalf("画面上找不到 UP 名，文字有 %q", tt.Texts())
	}
	clock, ok := tt.Find(formatDuration(time.Minute + 5*time.Second))
	if !ok {
		t.Fatalf("画面上找不到时长，文字有 %q", tt.Texts())
	}
	if owner.H <= clock.H {
		t.Errorf("UP 名那一块高 %v，时长高 %v——UP 名没有比次要数字大一档",
			owner.H, clock.H)
	}
}

// TestTheRowIndexIsCentredInItsColumn 钉住序号**居中**（上游 `min-w-8 text-center`）。
//
// 判据是**逐像素采样**：把序号那一列的墨迹扫出来，看它的中心是不是落在这一列的中点
// 上。右对齐是改之前的样子——数字的右边缘贴着列尾，中心偏右一大截。
//
// ⚠️ 用一位数的序号（「1」）：两位数的墨迹本来就更宽，居中和右对齐在那上面只差几个
// 像素，判不出来。
func TestTheRowIndexIsCentredInItsColumn(t *testing.T) {
	_, tt := newList(t, Actions{}, []Music{song("a", "一首歌", "")})

	img := tt.Image()
	bg := img.RGBAAt(testerW-1, testerH-1) // 右下角是空的：那就是底色
	left, right := -1, -1
	for y := range int(listRowHeight) {
		for x := cardPadX; x < cardPadX+indexWidth; x++ {
			if img.RGBAAt(x, y) == bg {
				continue
			}
			if left < 0 {
				left = x
			}
			right = x
		}
	}
	if left < 0 {
		t.Fatalf("序号那一列里一个非底色的像素都没有（序号没画出来）")
	}
	mid := float32(left+right+1) / 2
	want := float32(cardPadX) + indexWidth/2
	if d := mid - want; d > 2 || d < -2 {
		t.Errorf("序号那一块的中心在 x=%.1f，这一列的中点在 %.1f——差 %.1f（右对齐了？）",
			mid, want, d)
	}
}

// TestTheCompactRowHasNoCover 钉住「紧凑那一档不画封面」。
//
// 上游紧凑那一支只画歌名（`music-list-item/index.tsx:85-88`），封面整块不在；我们
// 改之前缩到 32 还在画。两条判据：
//
//  1. **位置**：UP 名的左边正好是「内边距 + 序号列 + 一个列间距」——中间没有封面。
//  2. **那圈 hover 播放反馈也没有**：它只有封面才画得出，而它正是紧凑档原来唯一的
//     鼠标播放入口。
func TestTheCompactRowHasNoCover(t *testing.T) {
	const title, owner = "紧凑的一首", "紧凑 UP"
	l := New(newCache(t), Actions{})
	l.SetView(ViewCompact)
	l.SetSongs([]Music{song("a", title, owner)})
	tt := ui.NewTester(l.View, testerW, testerH)
	tt.Frame()

	r, ok := tt.Find(owner)
	if !ok {
		t.Fatalf("画面上找不到 UP 名，文字有 %q", tt.Texts())
	}
	want := float32(compactPadX + compactIndexWidth + rowGap)
	if d := r.X - want; d > 1 || d < -1 {
		t.Errorf("UP 名在 x=%.1f，want %.1f（内边距 + 序号列 + 列间距）——中间还画着封面？",
			r.X, want)
	}

	// 指针移到这一行上：普通那一档此时会露出封面上的播放三角，紧凑这一档不该有。
	tt.Move(testerW/2, listRowHeight/2)
	tt.Frame()
	tt.Frame()
	tt.Advance(hover.Fade)
	tt.Frame()
	if _, ok := tt.Find(hover.FromLabel(title)); ok {
		t.Errorf("紧凑档的封面上冒出了播放三角——这一档不该有封面：%q", tt.Texts())
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
