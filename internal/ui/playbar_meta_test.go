package ui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/ShellMonster/Biu_Go/internal/app"
	mygoui "github.com/egoist/mygo/ui"
)

// 播放条上的 UP 名、收藏、下载（对照审计的 A23）。
//
// 这一组盯三件人眼能看见的事：
//
//	UP 名摆出来了，而且**点得动**（进的是那个 UP 主的空间，不是首页）
//	收藏 / 下载两个键摆出来了，字没被截
//	加了这些之后**歌名没有被挤没**——挤没了之后界面上看不出坏了（字还在那儿，
//	只是一个字一行）

// metaBarTester 只画播放条，State 从外面传进来（和 `qualityBarTester` 一样）。
//
// 喂 State 而不是真开一首文件，是为了让「有没有 UP 名」「有没有编号」这些
// 组合都能摆出来——真放的话本地文件压根没有 UP 名。
func metaBarTester(t *testing.T, st app.State, width int) (*mygoui.Tester, *Shell) {
	t.Helper()
	a := app.New()
	t.Cleanup(a.Close)
	s := New(a)
	t.Cleanup(s.images.Close)

	tt := mygoui.NewTester(func(c *mygoui.Context) {
		s.wake.set(c)
		s.viewPlayBar(c, st)
	}, width, coverWinH)
	tt.Frame()
	return tt, s
}

// TestThePlayBarShowsTheOwner 盯 UP 名摆出来了。
//
// 没有这一句的话播放条上根本没有「这是谁的歌」——用户听着一首不认识的歌，
// 界面上找不到任何能问一句的地方。
func TestThePlayBarShowsTheOwner(t *testing.T) {
	tt, _ := metaBarTester(t, app.State{
		HasTrack: true, Title: "一首歌",
		Artist: "某个 UP 主", ArtistMid: 12345,
	}, 1000)
	if !tt.HasText("某个 UP 主") {
		t.Errorf("播放条上没有 UP 名：%q", tt.Texts())
	}
}

// TestClickingTheOwnerGoesToThatSpace 盯**点得动**，而且去的是对的地方。
//
// ⚠️ 这里断的是路由真的换了（`/user/12345`）。只断「按下去不报错」的话，
// 跳到首页也算过——而那正是「摆了个按钮但没接上」最常见的症状。
func TestClickingTheOwnerGoesToThatSpace(t *testing.T) {
	tt, s := metaBarTester(t, app.State{
		HasTrack: true, Title: "一首歌",
		Artist: "某个 UP 主", ArtistMid: 12345,
	}, 1000)

	if err := tt.Click("某个 UP 主"); err != nil {
		t.Fatalf("点不动 UP 名：%v（画面：%q）", err, tt.Texts())
	}
	tt.Frame()
	if got := s.router.Path(); !strings.HasSuffix(got, "/user/12345") {
		t.Errorf("点 UP 名之后路由是 %q，want 以 /user/12345 结尾", got)
	}
}

// TestAnOwnerWithoutAnIDIsStillShownButGoesNowhere 钉住「只有名字」的那一支。
//
// 有些来源只给 UP 名不给编号（动态流那一批就是）。这时候**照旧显示**：
// 把名字藏起来是另一种撒谎——用户看到的是「这一首没有 UP 主」。
// 只是点不动，所以也不能去一个编出来的地址。
func TestAnOwnerWithoutAnIDIsStillShownButGoesNowhere(t *testing.T) {
	tt, s := metaBarTester(t, app.State{
		HasTrack: true, Title: "一首歌",
		Artist: "只有名字的 UP 主", ArtistMid: 0,
	}, 1000)

	if !tt.HasText("只有名字的 UP 主") {
		t.Fatalf("没有编号的 UP 名不显示了：%q", tt.Texts())
	}
	before := s.router.Path()
	if err := tt.Click("只有名字的 UP 主"); err != nil {
		t.Fatalf("点不动那个 UP 名：%v", err)
	}
	tt.Frame()
	if got := s.router.Path(); got != before {
		t.Errorf("没有编号的 UP 名点下去却去了 %q（原来是 %q）——"+
			"那是把 mid 拼成了一个编出来的地址", got, before)
	}
}

// TestThePlayBarHasFavoriteAndDownload 盯那两个键摆出来了。
//
// 收藏和下载的接线是 #88 接好的（`s.listActions()` 那一份），这里只管 UI：
// 键不在，用户就永远不知道有这两件事。
func TestThePlayBarHasFavoriteAndDownload(t *testing.T) {
	tt, _ := metaBarTester(t, app.State{
		HasTrack: true, Title: "一首歌", Artist: "某个 UP 主", ArtistMid: 1,
	}, 1000)
	for _, want := range []string{"收藏", "下载"} {
		if !tt.HasText(want) {
			t.Errorf("播放条上没有「%s」：%q", want, tt.Texts())
		}
	}
}

// TestNoFavoriteOrDownloadWithoutATrack 盯没歌时那两格一格都不画。
//
// 给一个空的播放条摆两个点不动的键，用户会以为它们坏了。
func TestNoFavoriteOrDownloadWithoutATrack(t *testing.T) {
	tt, _ := metaBarTester(t, app.State{}, 1000)
	for _, unwanted := range []string{"收藏", "下载"} {
		if tt.HasText(unwanted) {
			t.Errorf("还没有歌，播放条上却有「%s」：%q", unwanted, tt.Texts())
		}
	}
}

// TestTheMetaLabelsAreNotTruncated 量「画出来」的那几个字，而不是控件的文本。
//
// ⚠️ `HasText` 读的是控件的**文本**，被截的只是画出来的那几个字——所以这一条
// 断言会一直绿，哪怕屏幕上写着「收…」。断法是量宽度：和同一串字在一根不设限的
// 行里的自然宽度比（`textWidths`）。这一条教训在
// `playbar_rate_test.go` 的 TestRateMenuLabelsAreNotTruncated 那里已经吃过一次。
func TestTheMetaLabelsAreNotTruncated(t *testing.T) {
	labels := []string{"收藏", "下载"}
	natural := naturalButtonTextWidths(t, labels)

	// 两个键都在，同时还有 UP 名（最长的一档）：挤得最狠的情形。
	tt, _ := metaBarTester(t, app.State{
		HasTrack: true, Title: "一首歌的名字很长很长很长很长",
		Artist: "一个名字很长的 UP 主名字", ArtistMid: 12345,
	}, 900)

	for _, label := range labels {
		rect, ok := tt.Find(label)
		if !ok {
			t.Fatalf("播放条上没有 %q：%q", label, tt.Texts())
		}
		if rect.W+0.5 < natural[label] {
			t.Errorf("%q 只画出了 %.1f 宽，它本来要 %.1f——被截了"+
				"（用户在这两个键上认的是「收藏」「下载」，缺字就只能靠猜）",
				label, rect.W, natural[label])
		}
	}
}

// ownerNameMinW 是这一条断言的阈值。
//
// ⚠️ **它是一个独立的绝对数，不是生产那边的那个下限（`playbar.artistMinW`）**：
// 阈值取自被测对象的话，把那个下限删成 0 这条断言照样全绿——而它恰恰是为
// 「UP 名被挤成 0 宽」写的那一条（第一版就是这么写的，`artistMinW` 一改它
// 跟着改，等于没断言）。
//
// 40 是从「一个 UP 名要认得出来」倒推的：12 号字一个汉字 12 宽，40 能放下
// 三个字多一个偏旁；比它更窄的话用户读到的是「听不…」，那和没有是一回事。
// 这一条只管「宽到能认出」，不管「完整」——名字长到放不下是可以缩的
// （`Ellipsis` 那一档），没有宽度才是坏的。
const ownerNameMinW = 40

// TestTheOwnerIsNeverSqueezedOutOfExistence 盯 UP 名不会被挤成 0 宽。
//
// 窗口窄的时候左边那一块只剩 110，而「收藏」「下载」两个键加两道缝要 112——
// 不给 UP 名留一个下限的话它一个像素都分不到，而**界面上看不出坏了**：按钮和
// 那两行字都还在，只有名字没了，用户看到的是「这一首没有 UP 主」。
func TestTheOwnerIsNeverSqueezedOutOfExistence(t *testing.T) {
	const owner = "一个非常非常非常非常非常非常长的 UP 主名字"
	for _, width := range []int{760, 900, 1000, 1200, 1280} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			tt, _ := metaBarTester(t, app.State{
				HasTrack: true, Title: "一首歌", Artist: owner, ArtistMid: 12345,
			}, width)

			r, ok := tt.Find(owner)
			if !ok {
				t.Fatalf("宽度 %d：画面上没有那个 UP 名：%q", width, tt.Texts())
			}
			if r.W < ownerNameMinW {
				t.Errorf("宽度 %d：UP 名只剩 %.1f 宽（下限 %d）——它被两个键挤没了，"+
					"而界面上看不出坏了（按钮和字都还在）", width, r.W, ownerNameMinW)
			}
		})
	}
}

// TestTheMetaRowDoesNotSquashTheTitle 盯加了第二行之后歌名还是一整行。
//
// 这一票之前踩过同一个坑：为了塞下一个控件把歌名挤成 20px 宽、88px 高的竖条，
// 而**界面上看不出坏了**——字还在那儿，只是一个字一行。
//
// 断的是两件事：歌名横向要占得开（不能被压成竖排），三行都要留在 88 高的
// 播放条里（不能被裁）。
func TestTheMetaRowDoesNotSquashTheTitle(t *testing.T) {
	for _, width := range []int{760, 900, 1000, 1200, 1280} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			tt, _ := metaBarTester(t, app.State{
				HasTrack: true, Title: "一首歌的名字很长很长很长很长",
				Artist: "一个名字很长的 UP 主名字", ArtistMid: 12345,
				Err: "连不上 B 站这条音频流了", Rate: 1,
			}, width)

			title, ok := tt.Find("一首歌的名字很长很长很长很长")
			if !ok {
				t.Fatalf("宽度 %d：歌名不在画面上：%q", width, tt.Texts())
			}
			if title.W < 40 || title.W < title.H {
				t.Errorf("宽度 %d：歌名只剩 %.1f×%.1f，已经被挤成竖条了",
					width, title.W, title.H)
			}
			if title.H > playbarHeight || title.Y+title.H > playbarHeight {
				t.Errorf("宽度 %d：歌名（y=%.1f 高 %.1f）被 %d 高的播放条裁了",
					width, title.Y, title.H, playbarHeight)
			}
			// 三行都得在播放条里：UP 名那一行和错误那一行是这一票新加的，
			// 它们被 88 高裁掉的话用户看到的是「有话说了却看不见」。
			for _, text := range []string{"一个名字很长的 UP 主名字", "打不开：连不上 B 站这条音频流了"} {
				r, ok := tt.Find(text)
				if !ok {
					t.Fatalf("宽度 %d：画面上找不到 %q：%q", width, text, tt.Texts())
				}
				if r.Y < 0 || r.Y+r.H > playbarHeight {
					t.Errorf("宽度 %d：%q（y=%.1f 高 %.1f）被 %d 高的播放条裁了",
						width, text, r.Y, r.H, playbarHeight)
				}
			}
		})
	}
}

// TestTheErrorSitsUnderTheOwnerLine 钉住三行的**顺序**：歌名 / UP 名 / 错误。
//
// 顺序反了的话用户读到的是「先看到一句打不开，再看到是谁的歌」——而他最先要
// 知道的是「出了什么事」。
func TestTheErrorSitsUnderTheOwnerLine(t *testing.T) {
	tt, _ := metaBarTester(t, app.State{
		HasTrack: true, Title: "这一首歌的名字",
		Artist: "某个 UP 主", ArtistMid: 12345,
		Err: "连不上 B 站这条音频流了",
	}, 1000)

	title, okT := tt.Find("这一首歌的名字")
	owner, okO := tt.Find("某个 UP 主")
	errBox, okE := tt.Find("打不开：连不上 B 站这条音频流了")
	if !okT || !okO || !okE {
		t.Fatalf("歌名在不在 %v，UP 名在不在 %v，错误在不在 %v，画面的字：%q",
			okT, okO, okE, tt.Texts())
	}
	if owner.Y < title.Y+title.H {
		t.Errorf("UP 名 {%.1f,%.1f} 没有排在歌名 {%.1f,%.1f %.1fx%.1f} 的下面",
			owner.X, owner.Y, title.X, title.Y, title.W, title.H)
	}
	if errBox.Y < owner.Y+owner.H {
		t.Errorf("错误 {%.1f,%.1f} 没有排在 UP 名 {%.1f,%.1f %.1fx%.1f} 的下面",
			errBox.X, errBox.Y, owner.X, owner.Y, owner.W, owner.H)
	}
}

// TestThePlayBarButtonsSaySoWhenNothingIsPlaying 盯「按下去」有一句话。
//
// 没歌时两个键一格都不画（见上一条），而**有歌、但那一首还没进队列**时它们是
// 摆着的——那就是「画着一个按了没反应的东西」。说一句比按了没反应好解释。
func TestThePlayBarButtonsSaySoWhenNothingIsPlaying(t *testing.T) {
	tt, s := metaBarTester(t, app.State{HasTrack: true, Title: "一首歌"}, 1000)
	for _, label := range []string{"收藏", "下载"} {
		if err := tt.Click(label); err != nil {
			t.Fatalf("点不动 %q：%v（画面：%q）", label, err, tt.Texts())
		}
	}
	// 按了之后要有一句话（Toast）。断的是 `Shell.toasts` 而不是画面上的字：
	// 这一趟只画了播放条，而 Toast 是 `viewMain` 最后那一层——真去量画面的话
	// 这一条会因为「没画到」而红，而它要验的不是绘制。
	//
	// 这一条不点破是哪一句：拿不到身份时两句都是对的答案（见两个按钮各自的
	// 注释），而这一票要钉的是「有没有一句话」。
	if got := s.visibleToasts(); len(got) < 2 {
		t.Errorf("两个键按下去只说了 %d 句，want 2 句（%q）——"+
			"一个画在那儿、按下去一声不吭的键比不摆更坏", len(got), toastTexts(got))
	}
}

// naturalButtonTextWidths 量几串字在**按钮里**各占多宽，也就是它们不设限时的
// 自然宽度。
//
// ⚠️ 为什么不用 `textWidths`（那把量的是裸 `ui.Text`）：MyGo 的按钮标签**不吃
// 元素上的 `FontSize`**，它是另一个字号。第一版拿裸文字当基准，于是「收藏」
// 在按钮里 23.8、在裸文字里 25.8，差的那 2 像素被当成「被截了」——而它压根
// 没被截（按钮给它的格子是 32）。基准必须是**同一种控件**，否则量的是字号
// 差异，不是截断。
func naturalButtonTextWidths(t *testing.T, ss []string) map[string]float32 {
	t.Helper()
	tt := mygoui.NewTester(func(c *mygoui.Context) {
		mygoui.Row(c).Width(1200).Children(func() {
			for _, s := range ss {
				mygoui.Button(c, s)
			}
		})
	}, 1200, 100)
	tt.Frame()

	out := make(map[string]float32, len(ss))
	for _, s := range ss {
		r, ok := tt.Find(s)
		if !ok {
			t.Fatalf("连不设限的一行都量不出 %q 的宽度", s)
		}
		out[s] = r.W
	}
	return out
}
