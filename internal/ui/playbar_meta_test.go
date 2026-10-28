package ui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/ShellMonster/PudMusic/internal/app"
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
// ⚠️ **它背后是一个空的 App**，所以第三段那三枚键里只有「收藏」在（分集要看
// 这一首有几集，赞要看这一首能不能赞——两样都得问 App）。要量三枚都在的那一
// 档，用 `playbar_left_test.go` 里的 `leftBarTester`。
//
// 喂 State 而不是真开一首文件，是为了让「有没有 UP 名」「有没有编号」这些
// 组合都能摆出来——真放的话本地文件压根没有 UP 名。
func metaBarTester(t *testing.T, st app.State, width int) (*mygoui.Tester, *Shell) {
	t.Helper()
	var b *testShell
	b = newTestShell(t, at(width, coverWinH), withView(func(s *Shell) func(*mygoui.Context) {
		return func(c *mygoui.Context) {
			s.wake.set(c)
			s.viewPlayBar(c, st)
		}
	}))
	tt := b.tt
	tt.Frame()
	return tt, b.s
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

// TestTheMetaKeysDrawTheirOwnGlyphs 盯那两个键上画的是各自的图形。
//
// ⚠️ **「那两个键」是收藏和下载**：下载在播放条**右边**那一组（票 #127），
// 收藏在左边**第三段**（票 #219）。
//
// ⚠️ **这一条是被图标化顶掉的，原来叫 `TestTheMetaLabelsAreNotTruncated`**：它量
// 「收藏」「下载」那两个字有没有被截短，而**图标没有字可截**——图标版的对应
// 问题是「两枚图标认不认得出」，所以判据换成了「两枚的图形两两不同」。
//
// 为什么原来那条在图标化之后**变成恒真**、必须换掉而不是留着：它拿按钮框的
// 宽（28）去比那两个词在**另一个按钮**里的自然宽度（约 26），而 28 > 26 恒成立
// ——它还在绿，可它量的已经不是「有没有被截」这件事了。留着一条恒真的测试，
// 比没有这一条更坏（`TestTheMetaLabelsAreNotTruncated` 那段注释里写的正是这个：
// 无头 Tester 读的是控件的文本，被截的只是画出来的那几个字，所有断言照样绿）。
//
// 另一半（「图形真的画出来了」）在 `icons_test.go` 的
// `TestThePlayBarDrawsAnIconForEveryAction` 里——那边深浅两套主题都量，而这里
// 只管「两枚不一样」。
func TestTheMetaKeysDrawTheirOwnGlyphs(t *testing.T) {
	tt, _ := metaBarTester(t, app.State{
		HasTrack: true, Title: "一首歌的名字很长很长很长很长",
		Artist: "一个名字很长的 UP 主名字", ArtistMid: 12345,
	}, 900)

	seen := map[string]string{}
	for _, label := range []string{"收藏", "下载"} {
		ink := glyphIn(tt, label)
		if ink == "" {
			t.Fatalf("播放条上 %q 那一块没画东西：%q", label, tt.Texts())
		}
		if prev, dup := seen[ink]; dup {
			t.Errorf("%q 和 %q 画出来是同一个图形——它们是一枚图标，不是一对文字", label, prev)
		}
		seen[ink] = label
	}
}

// ownerNameMinW 是这一条断言的阈值。
//
// ⚠️ **它是一个独立的绝对数，不是生产那边的那个下限（`playbar.artistMinW`）**：
// 阈值取自被测对象的话，把那个下限删成 0 这条断言照样全绿——而它恰恰是为
// 「UP 名被挤成 0 宽」写的那一条（第一版就是这么写的，`artistMinW` 一改它
// 跟着改，等于没断言）。
//
// 40 是从「一个 UP 名要认得出来」倒推的：**14 号字**一个汉字 14 宽，40 能放下
// 两个多字；比它更窄的话用户读到的是「听不…」，那和没有是一回事。
// 这一条只管「宽到能认出」，不管「完整」——名字长到放不下是可以缩的
// （`Ellipsis` 那一档），没有宽度才是坏的。
//
// ⚠️ **这个门槛是「12 号字时倒推的」那 40**（票 #173 把 UP 名从 12 放到 14 之后，
// 同一个 40 在字面上少显示一个字多）。**它没有跟着长**——因为它离实际值远得很：
// 多 P 那一档最窄的时候（测试里的 760 宽窗口）UP 名还有 56，而在应用自己的下限
// 900 上是 112。**这条断言是保险丝，不是刻度**：它防的是「被挤成 0 宽」那种坏，
// 而那种坏在 40 这一档早就该响了。真要跟着字号长，得先量「多少个字还算认得出」。
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
			// ⚠️ `PlayMode` 要给：那个键（#104）现在摆在**播放条右边那一组**的头
			// 一个（票 #127 把它从进度那一行搬过去的）。
			// ⚠️ **这一段原来写的是「它摆在中间那一列的第二行，而中间那一列是从
			// 左边那块借宽度的」**——那句话连机制带位置一起过期了：它现在在右边
			// 那一组，而那一组是**写死的**（不 `Grow`），所以它不借谁的宽度。
			// **但「要给它」这件事本身还得留着**：右边那一组多了三枚键之后，
			// 左边那一块就窄了，UP 名被挤没正是从这里来的——不摆它（也就是
			// `State` 里不给 `PlayMode`），这一条量的是「没有别的控件抢地方」的
			// 那种播放条，而真窗口上它一直摆着。
			tt, _ := metaBarTester(t, app.State{
				HasTrack: true, Title: "一首歌", Artist: owner, ArtistMid: 12345,
				PlayMode: app.PlayModeSequence,
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

// TestTheTextColumnDoesNotSquashTheTitle 盯加了第二行之后歌名还是一整行。
//
// ⚠️ **它原来叫 `TestTheMetaRowDoesNotSquashTheTitle`**：「歌名下面那一行」在
// 票 #219 之后**没有了**——三枚键搬到了**封面右边第三段**，而 UP 名是那一列
// 自己的第二行。
//
// 这一票之前踩过同一个坑：为了塞下一个控件把歌名挤成 20px 宽、76px 高的竖条，
// 而**界面上看不出坏了**——字还在那儿，只是一个字一行。
//
// 断的是两件事：歌名横向要占得开（不能被压成竖排），三行都要留在 76 高的
// 播放条里（不能被裁）。
func TestTheTextColumnDoesNotSquashTheTitle(t *testing.T) {
	for _, width := range []int{760, 900, 1000, 1200, 1280} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			// ⚠️ `PlayMode` 同样要给：见上面那条的注释（#104 那个键在右边那一组，
			// 而右边多了三枚键之后左边那一块就窄了）。
			tt, _ := metaBarTester(t, app.State{
				HasTrack: true, Title: "一首歌的名字很长很长很长很长",
				Artist: "一个名字很长的 UP 主名字", ArtistMid: 12345,
				Err: "连不上 B 站这条音频流了", Rate: 1,
				PlayMode: app.PlayModeSequence,
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
			// 🔴 **出错时那一列还是两行**（歌名 + UP 名）（票：左下角出错那三行
			// 太碎）：错误文案搬到了**播放键**上，而那一列写的是**失败的那一首**
			// （理由见 `State.FailedTitle`）。
			//
			// ⚠️ **它们都得在播放条里**：被 76 高裁掉的话用户看到的是「有话说了
			// 却看不见」。
			for _, text := range []string{"一个名字很长的 UP 主名字"} {
				r, ok := tt.Find(text)
				if !ok {
					t.Fatalf("宽度 %d：画面上找不到 %q：%q", width, text, tt.Texts())
				}
				if r.Y < 0 || r.Y+r.H > playbarHeight {
					t.Errorf("宽度 %d：%q（y=%.1f 高 %.1f）被 %d 高的播放条裁了",
						width, text, r.Y, r.H, playbarHeight)
				}
			}
			// 那一列上**不该**有第三行（错误文案搬走了）。
			for _, text := range tt.Texts() {
				if strings.HasPrefix(text, "打不开") {
					t.Errorf("宽度 %d：那一列上还有那句被截碎的错误文案 %q——"+
						"它该搬到播放键上去", width, text)
				}
			}
		})
	}
}

// TestTheOwnerSitsUnderTheTitle 钉住两行的**顺序**：歌名 / UP 名。
//
// 顺序反了的话用户读到的是「先看到是谁的歌，再看到歌名」——而他最先要知道的
// 是「这是哪一首」。
//
// 🔴 **出错时那一列还是两行**（票：左下角出错那三行太碎）：错误文案搬到了
// **播放键**上，而那一列写的是**失败的那一首**（理由见 `State.FailedTitle`）。
// 那一列仍然只有两行——三行的话，76 高的播放条塞不下（票 #219 的教训）。
func TestTheOwnerSitsUnderTheTitle(t *testing.T) {
	tt, _ := metaBarTester(t, app.State{
		HasTrack: true, Title: "这一首歌的名字",
		Artist: "某个 UP 主", ArtistMid: 12345,
		Err: "连不上 B 站这条音频流了",
	}, 1000)

	title, okT := tt.Find("这一首歌的名字")
	owner, okO := tt.Find("某个 UP 主")
	if !okT || !okO {
		t.Fatalf("歌名在不在 %v，UP 名在不在 %v，画面的字：%q",
			okT, okO, tt.Texts())
	}
	if owner.Y < title.Y+title.H {
		t.Errorf("UP 名 {%.1f,%.1f} 没有排在歌名 {%.1f,%.1f %.1fx%.1f} 的下面",
			owner.X, owner.Y, title.X, title.Y, title.W, title.H)
	}
	// 🔴 **那一列上不该有第三行**（错误文案搬走了）：它是三行里最挤的一行，
	// 而 76 高的播放条放不下三行（票 #219 的教训）。
	for _, s := range tt.Texts() {
		if strings.HasPrefix(s, "打不开") {
			t.Errorf("那一列上还有那句被截碎的错误文案 %q——它该搬到播放键上去", s)
		}
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
