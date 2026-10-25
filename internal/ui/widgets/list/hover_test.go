package list

import (
	"image/color"
	"slices"
	"testing"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/hover"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/motion"
	"github.com/egoist/mygo/ui"
)

// 这一组验封面上的 hover 反馈（票面「一、列表行 hover」和「二、网格卡片 hover」）。
//
// 判据一律是**画面上画出来的东西**：控件的盒子（位置和大小）和画出来的像素。量「调了
// 哪个函数」的东西在这儿一个也没有——那是改代码的人自己能看见的。
//
// # ⚠️ 等动画要**推钟**
//
// MyGo 的 Tester 的钟站着不动（`mygo/ui/headless.go`：「Its clock stands still
// unless the test moves it with Advance」），所以只 `tt.Frame()` 的话淡入一步也不走，
// 而那一帧看起来是「淡入已经完成了」——症状是「测出来的透明度永远是 1」。要推钟：
// `tt.Advance(d)` 配 `tt.FramePending()`，见下面那两条。
//
// # ⚠️ hover 要**连着几帧**
//
// hover 是**上一帧**的布局决定的（`rt.hover`），所以移过去之后第一帧往往还拿着旧答
// 案。移完之后画几帧再问。

// hoverOn 把指针移到 title 那一行的中间，让 hover 换过来，再推满一次淡入。
//
// ⚠️ **三步一个都不能少**：
//
//  1. 画一帧——hover 是**上一帧**的布局决定的（`rt.hover`），移过去的第一帧还拿着旧
//     答案；
//  2. 再画一帧——这一帧 hover 才换过来；
//  3. **推钟**——Tester 的钟站着不动，不推的话淡入一步也不走，而那一帧看起来是
//     「淡入已经完成了」。
//
// 想要的不是「淡完之后」那一帧的用例请**别用这个**，自己按帧走（见
// TestTheHoverFeedbackFadesInOverTwoHundredMilliseconds）。
func hoverOn(t *testing.T, tt *ui.Tester, title string) {
	t.Helper()
	r, ok := tt.Find(title)
	if !ok {
		t.Fatalf("画面上找不到「%s」，文字有 %q", title, tt.Texts())
	}
	tt.Move(r.X+r.W/2, r.Y+r.H/2)
	tt.Frame()
	tt.Frame()
	settleFade(t, tt)
}

// hoverOff 把指针挪到画面外（列表上方那块空白），然后让淡出走完。
func hoverOff(t *testing.T, tt *ui.Tester) {
	t.Helper()
	tt.Move(testerW+100, -100)
	tt.Frame()
	tt.Frame()
	settleFade(t, tt)
}

// pixelAt 取画面上那一个点的颜色。
func pixelAt(tt *ui.Tester, x, y float32) color.RGBA {
	return tt.Image().RGBAAt(int(x), int(y))
}

// brightness 是那个点有多亮（0~1，三通道平均）。
//
// ⚠️ **三个通道要先转成 int 再加**：`c.R+c.G+c.B` 是 uint8 的加法，白色（255×3 =
// 765）在那里绕回 203——量出来的「亮度」是 0.27 而不是 0.93，于是「淡入有没有渐进」
// 那一条永远判不出来（第一版就是在这儿栽的）。
func brightness(c color.RGBA) float32 {
	return float32(int(c.R)+int(c.G)+int(c.B)) / 3 / 255
}

// ownersAre 是 n 行歌，**每行的 UP 名都不一样**（"UP A" 到 "UP E"）。
//
// 为什么非要唯一：这一组要量的是**行里某一块东西的横坐标**，而量它就得先找到
// 它——`Tester.Find` 认的是控件的文本，两行画着同一个字的时候它只给回第一块。
func ownersAre(n int) []Music {
	songs := onlineSongs(n)
	for i := range songs {
		songs[i].Owner = "UP " + string(rune('A'+i))
	}
	return songs
}

// ─────────────────────────────────────────────────────────────────────────────
// 列表行
// ─────────────────────────────────────────────────────────────────────────────

// TestHoveringARowPutsAPlayTriangleOnItsCover 钉住「指针移上去，封面正中浮出一个
// 20px 的白色播放三角」。
//
// 钉的是三样：**在**（Find 得到）、**多大**（20 见方，不是别处的数）、**在哪儿**
// （封面的正中间）。
//
// ⚠️ **三角形是一个 20 见方的盒子**，盒子里画一个三角——所以量到的是那个盒子而不是
// 画出来的多边形。三角画满了整个盒子（`hover.drawTri` 从盒子的 15% 画到 88%），
// 少了 20 这个数用户看到的就是一个小点。
func TestHoveringARowPutsAPlayTriangleOnItsCover(t *testing.T) {
	rec := &recorded{}
	songs := ownersAre(5)
	_, tt := newList(t, rec.actions(), songs)

	if _, ok := tt.Find(hover.FromLabel(songs[2].Title)); ok {
		t.Fatalf("指针还没移上去封面就有播放三角了：%q", tt.Texts())
	}

	hoverOn(t, tt, songs[2].Title)

	got, ok := tt.Find(hover.FromLabel(songs[2].Title))
	if !ok {
		t.Fatalf("指针移上去之后封面上没有播放三角：%q", tt.Texts())
	}
	if got.W < 19 || got.H < 19 {
		t.Errorf("那个播放三角只有 %.1f×%.1f，want 20×20（上游 `music-list-item/index.tsx"+
			":100-107` 的 `w-5 h-5`）——一个几像素的三角用户根本认不出那是「播放」",
			got.W, got.H)
	}
	// 别的行**没有**：那一格三角只跟着指针走。
	if _, ok := tt.Find(hover.FromLabel(songs[0].Title)); ok {
		t.Errorf("指针在第三行，第一行的封面上却有播放三角：%q", tt.Texts())
	}
}

// TestTheHoverTriangleIsGoneWhenThePointerLeaves 是上面那一条的另一半：反馈是**跟
// 着指针**的，不是出现一次就留在那儿。
func TestTheHoverTriangleIsGoneWhenThePointerLeaves(t *testing.T) {
	rec := &recorded{}
	songs := ownersAre(5)
	_, tt := newList(t, rec.actions(), songs)

	hoverOn(t, tt, songs[2].Title)
	if _, ok := tt.Find(hover.FromLabel(songs[2].Title)); !ok {
		t.Fatalf("指针移上去之后封面上没有播放三角：%q", tt.Texts())
	}

	hoverOff(t, tt)
	// ⚠️ 淡出要走满 200ms，所以这里推钟等它走完再问——问「指针移走了的那一帧」量到
	// 的是「刚开始淡出」，那是它应该在的样子，不是缺陷。
	settleFade(t, tt)
	if _, ok := tt.Find(hover.FromLabel(songs[2].Title)); ok {
		t.Errorf("指针移走之后那个播放三角还留着：%q", tt.Texts())
	}
}

// TestThereIsNoTextPlayButtonOnTheIndexAnyMore 钉住「序号换成一个写着『播放』的按钮」
// 那一套真的拆干净了。
//
// 上游那一行里没有任何文字按钮，播放三角是一个图形。留着那个按钮的话用户会在同一行
// 上看到两个「能播放」的东西，而它们点下去还是同一件事。
func TestThereIsNoTextPlayButtonOnTheIndexAnyMore(t *testing.T) {
	rec := &recorded{}
	songs := ownersAre(5)
	_, tt := newList(t, rec.actions(), songs)

	hoverOn(t, tt, songs[2].Title)
	if shows(tt, "播放") {
		t.Errorf("hover 之后序号那一列又出现了一个写着「播放」的按钮：%q", tt.Texts())
	}
}

// TestTheHoverTrianglePaintsWhiteOnTheDimmedCover 量**画出来的像素**：三角那个点比
// 同一张封面上别的地方亮一截。
//
// ⚠️ 为什么非量像素不可：`Tester.Find` 认的是控件的**标签**，标签在而绘制没画出来
// （或者反过来）它一点都看不出来。而这一条的失败症状正是「框在那儿、三角没画出来」
// ——一个只断言元素存在的话永远发现不了。
//
// 封面空地址时 imgcache 画一块 `t.Surface`，上面压 35% 黑就暗下去了，白色的三角于是
// 比它亮得多。判据只比**同一张封面上的两个点**，所以换主题、换深浅都照样成立。
func TestTheHoverTrianglePaintsWhiteOnTheDimmedCover(t *testing.T) {
	rec := &recorded{}
	songs := ownersAre(5)
	_, tt := newList(t, rec.actions(), songs)

	hoverOn(t, tt, songs[2].Title)
	tri, ok := tt.Find(hover.FromLabel(songs[2].Title))
	if !ok {
		t.Fatalf("封面上没有播放三角：%q", tt.Texts())
	}

	// 三角的形心：三角形在右边是尖的，形心落在白里。
	inside := brightness(pixelAt(tt, tri.X+tri.W*0.3, tri.Y+tri.H/2))
	// 封面**左边那一块**：那个播放三角是居中贴在封面上的，所以封面左边那一列上没有
	// 三角，只有压暗。
	//
	// ⚠️ **取样点必须在封面里面**：往右多取一个三角的边长就取到封面外面去了，而外面
	// 是列表的底色（浅色），比压暗过的封面还亮——于是「三角比封面亮」这条永远不成立，
	// 而画面上明明有一个白三角（第一版就栽在这儿）。
	dark := brightness(pixelAt(tt, tri.X-coverSizeLeftEdge, tri.Y+tri.H/2))
	if inside <= dark+0.15 {
		t.Errorf("三角那个点只比封面左边亮 %.2f（%.2f → %.2f）——那个白色三角多半根本没"+
			"画出来，而 Find 照样能找到它的标签", inside-dark, dark, inside)
	}
}

// coverSizeLeftEdge 是「从那个居中的播放三角往左，到封面左边沿」的距离：封面边长减
// 三角边长，两边对半分。
const coverSizeLeftEdge = (coverSize - 20) / 2

// TestTheHoverFeedbackFadesInOverTwoHundredMilliseconds 钉住「播放钮和三角形都
// 200ms 淡入，不是突然冒出来」。
//
// 判据是**同一张封面上那一点随时间变暗**：指针移过去的那一帧它还没压暗（那个三角连
// 标签都还找不到——`Invisible()` 的原话之一是「has no text to find」），推到四分之一
// 压了一半，再推满就压满了。
//
// ⚠️ **量压暗那一层，不量三角**：三角是白的，它一出来那个点就接近纯白（0.94），后面
// 再怎么渐变也只动 0.03——拿它当判据，这一版的第一条断言写的是「差 0.05 以上」，于是
// 一条**真的在渐变**的淡入被判成「啪地一下」。压暗那一层是从「没有压」到「压满」的
// 全程，量程大得多。
//
// ⚠️ **必须 `tt.Advance` 推钟**：Tester 的钟站着不动，只画帧的话这一步一步也不走。
func TestTheHoverFeedbackFadesInOverTwoHundredMilliseconds(t *testing.T) {
	rec := &recorded{}
	songs := ownersAre(5)
	_, tt := newList(t, rec.actions(), songs)

	// 那个播放三角是**居中**贴在封面上的（见 RowCover），所以封面左边那一列上没有三角，
	// 只有压暗——拿它当探针，两种情形都量得到。
	probe := func() float32 {
		t.Helper()
		tri, ok := tt.Find(hover.FromLabel(songs[2].Title))
		x := tri.X - coverSizeLeftEdge
		if ok {
			// 三角在的时候用三角的盒子定位（淡入之后 `Find` 才找得到它）。
			x = tri.X - coverSizeLeftEdge
		} else {
			// 还没出现时按行的位置推：封面左边 = 行左边 + 内边距 + 序号宽 + 间距。
			x = cardPadX + indexWidth + rowGap
		}
		return brightness(pixelAt(tt, x, triY(tt, songs[2].Title)))
	}

	// 先把指针移过去但不推钟。
	r, ok := tt.Find(songs[2].Title)
	if !ok {
		t.Fatalf("画面上找不到「%s」，文字有 %q", songs[2].Title, tt.Texts())
	}
	tt.Move(r.X+r.W/2, r.Y+r.H/2)
	tt.Frame()
	tt.Frame()

	if _, ok := tt.Find(hover.FromLabel(songs[2].Title)); ok {
		t.Fatalf("指针移上去的第一帧封面上就有播放三角了——那不是淡入，是「啪」地一下"+
			"出现：%q", tt.Texts())
	}
	start := probe()

	advance(t, tt, hover.Fade/4)
	if _, ok := tt.Find(hover.FromLabel(songs[2].Title)); !ok {
		t.Fatalf("推到四分之一还找不到播放三角：%q", tt.Texts())
	}
	quarter := probe()

	advance(t, tt, hover.Fade)
	full := probe()

	if quarter >= start-0.04 {
		t.Errorf("推到 %v 时封面和刚移过去时一样亮（%.3f → %.3f）——压暗是「啪」地一下"+
			"盖上去的", hover.Fade/4, start, quarter)
	}
	if full >= quarter-0.04 {
		t.Errorf("推到 %v 时封面和推到 %v 时一样亮（%.3f → %.3f）——淡入没有走完就停了"+
			"，或者压根没有渐进", hover.Fade/4, hover.Fade, quarter, full)
	}
}

// triY 是 title 那一行的竖直中点。探针要落在那一行里，所以用行来定位竖直方向。
func triY(tt *ui.Tester, title string) float32 {
	r, ok := tt.Find(title)
	if !ok {
		return 0
	}
	return r.Y + r.H/2
}

// TestThePlayTriangleGrowsWhenThePointerIsOnIt 钉住「那个三角在 hover 时放大一点」。
//
// 上游是 `hover:scale-110`（`music-list-item/index.tsx:100-107`），110% 就是 20 变
// 22。
//
// ⚠️ **量得到是因为 `Tester.Find` 给的是变换后的盒子**（`mygo/ui/runtime.go` 的
// `visualRect`），不是布局那个。这是它值钱的地方：`Scale` 不改布局，而断言量的是
// 画出来的大小。
//
// ⚠️ **必须推钟**：那一档放大**现在有动画了**（以前是瞬时的，每帧直接算 `Scale`），
// 而 Tester 的钟站着不动（`mygo/ui/headless.go`）——不推的话量到的是「放大还没
// 开始」，那一帧量出来还是 20。
func TestThePlayTriangleGrowsWhenThePointerIsOnIt(t *testing.T) {
	rec := &recorded{}
	songs := ownersAre(5)
	_, tt := newList(t, rec.actions(), songs)

	hoverOn(t, tt, songs[2].Title)
	settleFade(t, tt)
	rest, ok := tt.Find(hover.FromLabel(songs[2].Title))
	if !ok {
		t.Fatalf("封面上没有播放三角：%q", tt.Texts())
	}

	// 把指针挪到三角的正中间。
	tt.Move(rest.X+rest.W/2, rest.Y+rest.H/2)
	tt.Frame()
	tt.Advance(motion.GrowTime)
	tt.Frame()
	grown, ok := tt.Find(hover.FromLabel(songs[2].Title))
	if !ok {
		t.Fatalf("指针停在三角上时它不见了：%q", tt.Texts())
	}

	want := rest.W * 1.1
	if grown.W < want-1 {
		t.Errorf("指针停在三角上时它只有 %.1f 宽，want %.1f（上游的 110%%）——"+
			"放大这一条没落地", grown.W, want)
	}
	// 挪到别处它跟着收回去：放大是**跟着指针**的，淡出走完这一层就整个没了。
	hoverOff(t, tt)
	if _, ok := tt.Find(hover.FromLabel(songs[2].Title)); ok {
		t.Errorf("指针移走、淡出走完之后播放三角还留着：%q", tt.Texts())
	}
}

// TestThePlayTrianglePlaysThatSongAndTheRestAfterIt 钉住播放三角的**语义**和双击
// 同一行一样：从这一首往后接。
//
// 不一样的话用户第一次按它就会拿到一个自己没点过的动作（比如只加一首、或者只跳到
// 这一首）。
func TestThePlayTrianglePlaysThatSongAndTheRestAfterIt(t *testing.T) {
	rec := &recorded{}
	songs := onlineSongs(5)
	_, tt := newList(t, rec.actions(), songs)

	hoverOn(t, tt, songs[1].Title)
	settleFade(t, tt)
	if err := tt.Click(hover.FromLabel(songs[1].Title)); err != nil {
		t.Fatalf("点不了封面上那个播放三角：%v（画面：%q）", err, tt.Texts())
	}

	if len(rec.played) != 1 {
		t.Fatalf("收到 %d 次播放，want 1 次", len(rec.played))
	}
	got := rec.played[0]
	if len(got) != 4 {
		t.Fatalf("从第二首开始，队列里进了 %d 首，want 4 首（它和它后面那三首）", len(got))
	}
	for i, want := range []string{"BV2", "BV3", "BV4", "BV5"} {
		if got[i].Bvid != want {
			t.Errorf("队列第 %d 首是 %q，want %q", i+1, got[i].Bvid, want)
		}
	}
}

// TestHoveringARowDoesNotMoveIt 盯指针移上去的时候那一行**不横着跳一下**。
//
// 播放三角是 `Attach` 在封面上的（不在布局里），所以它在不在都不改封面的盒子。
// 要是哪天改成把它塞进行内布局，那一行右边的每一块都会被推走——而指针正在往那一行上
// 移，行在指针底下滑一下是很难受的。
//
// ⚠️ **量的是行里的 UP 名那一块**，理由和 `ownersAre` 上方那一段一样。
func TestHoveringARowDoesNotMoveIt(t *testing.T) {
	_, tt := newList(t, (&recorded{}).actions(), ownersAre(5))

	before, ok := tt.Find("UP C")
	if !ok {
		t.Fatalf("找不到第三行的 UP 名，文字有 %q", tt.Texts())
	}
	if before.X <= indexWidth+coverSize {
		t.Fatalf("第三行的 UP 名只在 x=%.1f，连序号（%d）加封面（%d）都没让开——"+
			"量错了东西，这条用例什么也证明不了", before.X, indexWidth, coverSize)
	}

	hoverOn(t, tt, "在线的歌 C")
	settleFade(t, tt)

	after, ok := tt.Find("UP C")
	if !ok {
		t.Fatalf("hover 之后找不到第三行的 UP 名，文字有 %q", tt.Texts())
	}
	if after.X != before.X {
		t.Errorf("hover 之后第三行横着挪了 %.1f（%.1f → %.1f）——播放三角占了布局里"+
			"的位置，它右边的每一块都被推走了，而指针就在那一行上",
			after.X-before.X, before.X, after.X)
	}
}

// TestThePlayingRowShowsAWaveformWhereTheNumberWas 钉住「正在播放的那一行，序号位置
// 换成跳动的波形」。
//
// 钉的是**位置**：波形摆的那一格是序号那一列（x 在 `indexWidth` 之内），而行尾那句
// 11px 的「正在播放」没有了。
//
// ⚠️ **数标签而不是数字**：波形是一个没有文字的盒子，它能被 `Find` 找到靠的是它的
// 无障碍标签（`歌名 正在播放`，见 `hover.PlayingLabel`）。找歌名会命中 `ui.List` 挂
// 在**整行**上的盒子（`List.View` 里那句 `l.state.Label`），那个盒子的 X 永远是 0。
func TestThePlayingRowShowsAWaveformWhereTheNumberWas(t *testing.T) {
	rec := &recorded{}
	songs := ownersAre(5)
	_, tt := newList(t, playingActions(rec, songs[1].Item.Identity()), songs)

	row, ok := tt.Find(songs[1].Title)
	if !ok {
		t.Fatalf("画面上找不到「%s」，文字有 %q", songs[1].Title, tt.Texts())
	}
	wave, ok := tt.Find(hover.PlayingLabel(songs[1].Title))
	if !ok {
		t.Fatalf("正在放的那一行没有那个跳动波形：%q", tt.Texts())
	}
	if wave.X >= indexWidth {
		t.Errorf("波形摆在 x=%.1f，可序号那一列只到 %d——它没有占在序号的位置上"+
			"（行是 x=%.1f 起的那一块）", wave.X, indexWidth, row.X)
	}
	if wave.W > indexWidth {
		t.Errorf("波形占了 %.1f 宽，比序号那一列（%d）还宽——它把封面顶走了",
			wave.W, indexWidth)
	}
	// 没在放的那几行照旧是序号。
	if _, ok := tt.Find(hover.PlayingLabel(songs[2].Title)); ok {
		t.Errorf("没在放的那一行上也有波形：%q", tt.Texts())
	}
}

// TestTheWaveformMoves 钉住波形是**跳动的**，不是一段画上去就不动的竖条。
//
// 判据是**同样四根条子的高度变了**。四根条子各错开四分之一轮，所以它们的高度互不相
// 同——全都不动的话那是一段竖条，会动的话至少有一根变了。
//
// ⚠️ 必须推钟：Tester 的钟站着不动。
func TestTheWaveformMoves(t *testing.T) {
	rec := &recorded{}
	songs := ownersAre(5)
	_, tt := newList(t, playingActions(rec, songs[1].Item.Identity()), songs)

	wave, ok := tt.Find(hover.PlayingLabel(songs[1].Title))
	if !ok {
		t.Fatalf("正在放的那一行没有那个跳动波形：%q", tt.Texts())
	}
	before := barHeights(t, tt, wave)
	if len(before) < 2 {
		t.Fatalf("那个波形里只量到 %d 根竖条（%v）——它多半根本没画出来", len(before), before)
	}

	// 推半个周期：四根条子各换了一次相位。
	advance(t, tt, hover.Fade*2)
	wave, ok = tt.Find(hover.PlayingLabel(songs[1].Title))
	if !ok {
		t.Fatalf("推钟之后那个波形不见了：%q", tt.Texts())
	}
	after := barHeights(t, tt, wave)

	if slices.Equal(before, after) {
		t.Errorf("推了 %v 之后那几根竖条一根都没动过（%v → %v）——"+
			"那是一段画上去就不动的竖条，不是跳动的波形", hover.Fade*2, before, after)
	}
}

// barHeights 是那个波形盒子里那几根竖条此刻各有多高。
//
// ⚠️ **量的是条子自己，不是那个盒子**：盒子高是写死的 14，跳的是**里面那几根**。
// 量法是逐列扫：和背景差得远的那一格算「条子上的一点」，一段连续的算一根条子。
//
// ⚠️ **背景取盒子里第一列、而不是盒外**：条子是主色、和列表底色差得开；拿盒外当基准的
// 话，封面那一块也会被当成条子，量出来的是一个恒定的数（第一版就是这么量的，两次都
// 是 12）。
func barHeights(t *testing.T, tt *ui.Tester, wave ui.Rect) []int {
	t.Helper()
	img := tt.Image()
	x0, x1 := int(wave.X), int(wave.X+wave.W)
	y0, y1 := int(wave.Y), int(wave.Y+wave.H)
	// 背景：盒子里最左边那一列（条子居中摆着，最左边那一列上什么都没有）。
	bg := pixelAt(tt, wave.X, wave.Y+wave.H/2)

	var out []int
	for x := x0; x < x1; x++ {
		top, bottom := -1, -1
		for y := y0; y < y1; y++ {
			c := img.RGBAAt(x, y)
			if colorDiff(c, bg) < 40 {
				continue
			}
			if top < 0 {
				top = y
			}
			bottom = y
		}
		if top >= 0 {
			out = append(out, bottom-top+1)
		}
	}
	return out
}

// colorDiff 是两个颜色差多远（三通道绝对差之和）。
func colorDiff(a, b color.RGBA) int {
	return absI(int(a.R)-int(b.R)) + absI(int(a.G)-int(b.G)) + absI(int(a.B)-int(b.B))
}

func absI(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// ─────────────────────────────────────────────────────────────────────────────
// 网格卡片
// ─────────────────────────────────────────────────────────────────────────────

// TestHoveringACardFloatsAPlayButtonOnItsCover 钉住网格卡片 hover 时右下角浮出那个
// 40px 的主色圆播放钮。
//
// ⚠️ **量的是它离封面右边和下边各多少**：8（`hover.btnInset`）。差几像素的话用户看到
// 的是播放钮「悬在封面外面」，而网格之间只有 16 的缝，它会盖到隔壁那一格上。
func TestHoveringACardFloatsAPlayButtonOnItsCover(t *testing.T) {
	rec := &recorded{}
	songs := onlineSongs(8)
	_, tt := newGrid(t, rec.actions(), songs, 1100, gridH) // 4 列

	if _, ok := tt.Find(hover.FromLabel(songs[2].Title)); ok {
		t.Fatalf("指针还没移上去就有播放钮了：%q", tt.Texts())
	}
	hoverOn(t, tt, songs[2].Title)
	settleFade(t, tt)

	btn, ok := tt.Find(hover.FromLabel(songs[2].Title))
	if !ok {
		t.Fatalf("指针移到那一格之后封面右下角没有播放钮：%q", tt.Texts())
	}
	if btn.W < 38 {
		t.Errorf("那个播放钮只有 %.1f 宽，want 40（上游 `music-card/index.tsx:39-47`）",
			btn.W)
	}
	// 别的格**没有**：那一格跟着指针走。
	if _, ok := tt.Find(hover.FromLabel(songs[5].Title)); ok {
		t.Errorf("指针在第三格，第六格上却有播放钮：%q", tt.Texts())
	}
}

// TestTheCardGradientBarSaysTheDuration 钉住封面底部压的那一条渐变：它上面有这一首
// 的时长，而且右边给那个播放钮让出了位置。
//
// ⚠️ **右边要留出 `btn + btnInset`**：不留的话时长那一半压在播放钮底下，用户读到的是
// 一个「没画完的 3:0」。
func TestTheCardGradientBarSaysTheDuration(t *testing.T) {
	songs := onlineSongs(8)
	for i := range songs {
		songs[i].Duration = time.Duration(185+i) * time.Second // 3:05 起
	}
	_, tt := newGrid(t, (&recorded{}).actions(), songs, 1100, gridH)

	hoverOn(t, tt, songs[2].Title)
	settleFade(t, tt)

	dur, ok := tt.Find(formatDuration(songs[2].Duration))
	if !ok {
		t.Fatalf("封面底下那一行没有这一首的时长（%q）：%q",
			formatDuration(songs[2].Duration), tt.Texts())
	}
	btn, ok := tt.Find(hover.FromLabel(songs[2].Title))
	if !ok {
		t.Fatalf("封面上没有播放钮：%q", tt.Texts())
	}
	if dur.X+dur.W > btn.X {
		t.Errorf("时长那一块的右边缘 %.1f 已经压在播放钮的左边缘 %.1f 上"+
			"——用户读到的是一个被盖掉一半的时长", dur.X+dur.W, btn.X)
	}
	// ⚠️ **它还必须贴着右边那一半**：这一条问的是「右半边那半截是不是在右边」——
	// Row 默认从左边挨着摆，不右对齐的话时长浮在封面左下角，看着像另一个控件，而上面
	// 那条「没被盖住」是绿的（贴不贴边它都验不出来）。
	mid := (dur.X + btn.X + btn.W) / 2
	if dur.X+dur.W < mid {
		t.Errorf("时长落在左边（%.1f~%.1f），那个播放钮在 %.1f~%.1f——它该贴着播放钮"+
			"左边那一半", dur.X, dur.X+dur.W, btn.X, btn.X+btn.W)
	}
}

// TestTheCardGradientBarSaysThePlayCount 钉住封面底部那一条渐变**左半边是播放量**。
//
// 上游 `music-card/index.tsx:50-55` 的那一条上是「左播放量、右时长」。播放量来自
// `Music.View`（见它那几段注释：有它的是首页 / 搜索歌曲视频 / UP 空间投稿 /
// 稍后再看 / 收藏夹·合集·系列）。
//
// ⚠️ **它必须在左边**：那一条只有 22 高，播放量和时长挤在两头，而这一条量的是播放量
// 的右边缘紧跟着时长的左边——放反了就是两串字贴在一起。
func TestTheCardGradientBarSaysThePlayCount(t *testing.T) {
	songs := onlineSongs(8)
	songs[2].Duration = 185 * time.Second
	songs[2].View = 123_400 // →「12.3万」
	_, tt := newGrid(t, (&recorded{}).actions(), songs, 1100, gridH)

	hoverOn(t, tt, songs[2].Title)
	settleFade(t, tt)

	count, ok := tt.Find(PlayCountText(songs[2].View))
	if !ok {
		t.Fatalf("封面底下那一条上没有播放量 %q，画面上有 %q",
			PlayCountText(songs[2].View), tt.Texts())
	}
	clock, ok := tt.Find(formatDuration(songs[2].Duration))
	if !ok {
		t.Fatalf("封面底下那一条上没有时长，文字有 %q", tt.Texts())
	}
	if count.X >= clock.X {
		t.Errorf("播放量在 x=%.1f、时长在 x=%.1f——上游那一条是**左播放量、右时长**",
			count.X, clock.X)
	}
	// 别的格没有播放量（夹具里就是没有），所以那一半不画。
	if _, ok := tt.Find(PlayCountText(songs[3].View)); ok {
		t.Errorf("没有播放量的那一格上也有字：%q", tt.Texts())
	}
}

// TestTheCardPlayButtonPlaysThatCardAndTheRestAfterIt 钉住网格那个圆播放钮的**语义**
// 和双击那一格一样：从这一首往后接。
func TestTheCardPlayButtonPlaysThatCardAndTheRestAfterIt(t *testing.T) {
	rec := &recorded{}
	songs := onlineSongs(8)
	_, tt := newGrid(t, rec.actions(), songs, 1100, gridH)

	hoverOn(t, tt, songs[1].Title)
	settleFade(t, tt)
	if err := tt.Click(hover.FromLabel(songs[1].Title)); err != nil {
		t.Fatalf("点不了封面上那个播放钮：%v（画面：%q）", err, tt.Texts())
	}

	if len(rec.played) != 1 {
		t.Fatalf("收到 %d 次播放，want 1 次", len(rec.played))
	}
	if got := rec.played[0]; len(got) != 7 || got[0].Bvid != "BV2" {
		t.Errorf("从第二格开始接进去的是 %d 首（第一首 %q），want 7 首（它和它后面那六首）",
			len(got), got[0].Bvid)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 等动画
// ─────────────────────────────────────────────────────────────────────────────

// advance 推钟 d，并把那一刻的画面画出来。
//
// ⚠️ **推钟之后必须补一帧**：MyGo 的 `Advance` 只画 d 这段时间里**到点**的那些帧，刻意的
// 免得在同一个时刻上白画（`mygo/ui/headless.go`）。所以推到的那一刻上画面还是推进去的
// 那一帧——那正是「有没有动」这类判据量到旧画面的原因。
//
// ⚠️ **不要改成「循环 `tt.Frame()` 数帧」**：钟站着不动时那种等法**静默失效**——不报错、
// 不超时，直接放行，于是每次量到的都是同一帧（AGENTS.md 第二节记的就是这件事）。
//
// ⚠️ **也别拿 `FramePending()` 当「画完了」的判据**：那个跳动波形只在钟走过的时候才要
// 下一帧（`hover.wavePhase`），而 `Advance` 本身也会问一次，两者的时序对不上就是死等。
func advance(t *testing.T, tt *ui.Tester, d time.Duration) {
	t.Helper()
	tt.Advance(d)
	tt.Frame()
}

// settleFade 推满一次淡入 / 淡出（hover.Fade）。
func settleFade(t *testing.T, tt *ui.Tester) {
	t.Helper()
	advance(t, tt, hover.Fade)
}
