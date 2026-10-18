// 播放条上的分集抽屉（Biu 的 `layout/playbar/left/page-list.tsx`）。
//
// 走的是**整条链**：真的假 B 站（`biliFixture`）回多 P 列表 → `App.Parts` 缓存 →
// 播放条上那个键 → 浮层列出三集 → 点一行 → `App.PlayPart` 换集。断的是**用户真的
// 能从播放条换到另一集**，不是「那个函数被调到了」。
//
// 和满屏那个分集面板（`fullplayer_parts_test.go`）的关系：同一个 `App.Parts`、
// 同一份 `partRowsFor`、同一个 `App.PlayPart`，两个入口。这边量的是「播放条上摆得
// 下、点得动」，那边量的是「满屏里那个把手」——验收要的是播放条这一条。
package ui

import (
	"fmt"
	"image"
	"slices"
	"testing"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/egoist/mygo/ui"
)

// partsReady 等分集真的问回来、而且歌已经在放。
//
// want 是**这一份 fixture 回几集**：多 P 那几条是 3，单 P 那一条是 1。写死 3 的话
// 单 P 那条永远等不到——而它等的恰恰是「只有一集」这个状态。
//
// ⚠️ **`HasTrack` 和「问到几集」两个条件都要**：只等集数的话会量到「这一瞬」——
// 换歌和 `App.PlayPart` 都要先 `ClearTrack()`（`online.go`），而那几百毫秒里
// `HasTrack` 恒为 false，播放条上什么都不画（`viewTrackMeta` 第一行就 return）。
// 那时候量到的是一个空的播放条，不是坏了。
func partsReady(t *testing.T, s *Shell, tt *ui.Tester, want int) {
	t.Helper()
	waitFrames(t, tt, fmt.Sprintf("这一首放起来而且分集问回来了（要 %d 集）", want), func() bool {
		return s.app.Snapshot().HasTrack && len(s.app.Parts()) == want
	})
}

// openPartsDrawer 点开播放条上那个分集浮层。
func openPartsDrawer(t *testing.T, tt *ui.Tester) {
	t.Helper()
	if err := tt.Click("分集"); err != nil {
		t.Fatalf("点不了播放条上的分集入口：%v（文字：%q）", err, tt.Texts())
	}
	tt.Frame()
}

// TestThePlayBarOpensTheEpisodesAndSwitchesToOne 是这一票的主干那一条：
// 播放条上有分集入口 → 打开是浮层列表 → 每一集有编号和标题 → 点一集就切过去。
func TestThePlayBarOpensTheEpisodesAndSwitchesToOne(t *testing.T) {
	s, tt := onlineTester(t, multiPages(), false)
	partsReady(t, s, tt, 3)

	if _, ok := tt.Find("分集"); !ok {
		t.Fatalf("多 P 的歌在放，播放条上却没有分集入口，文字有 %q", tt.Texts())
	}
	openPartsDrawer(t, tt)

	// 三集都在，第三集**没有标题**（B 站有时候不给）——这时要说「第 3 集」，
	// 不是把行留成空白（空标题在列表里和「这一行没画出来」长得一模一样）。
	for _, want := range []string{"第一集", "第二集", "第 3 集"} {
		if !tt.HasText(want) {
			t.Fatalf("浮层里没有 %q，文字有 %q", want, tt.Texts())
		}
	}
	if !tt.HasText("共 3 集") {
		t.Errorf("浮层头上没有说一共几集，文字有 %q", tt.Texts())
	}

	// 点第三集：立刻切过去，**不用先退出再进**。
	if err := tt.Click("第 3 集"); err != nil {
		t.Fatalf("点不了第三集：%v（文字：%q）", err, tt.Texts())
	}
	waitFrames(t, tt, "换到第三集", func() bool {
		it, ok := s.app.CurrentItem()
		return ok && it.Cid == "779"
	})
	// 选完就收：Biu 的 `onPressItem={onClose}`。
	//
	// ⚠️ 这里量的是**用户看得见的结果**，量不到 `viewPartRow` 里那一句关闭本身：
	// `App.PlayPart` 会先 `ClearTrack()`，于是「没有歌就收掉」那句（`viewPartButton`
	// 开头）顺手也把浮层关了，删掉前者这条照样绿。那一句由
	// `TestPickingTheEpisodeAlreadyPlayingStillClosesTheDrawer` 单独钉——那一档
	// `PlayPart` 提前返回、不清轨道，收掉浮层的就只剩它。
	if s.partOpen {
		t.Error("选完一集之后浮层还开着")
	}
	// 队列里**还是那一项**（不是插了一首新的）：多 P 的两集在 `Item.Identity()`
	// 里是同一首，插一项新的会让抽屉和队列里同时出现同一首。
	if n := len(s.app.Queue().Items()); n != 1 {
		t.Errorf("队列里有 %d 项，换集之后应该还是 1 项", n)
	}
}

// TestPickingTheEpisodeAlreadyPlayingStillClosesTheDrawer 断「点正在放的那一集，
// 浮层也收」。
//
// Biu 那边 `playListItem(id)` 在已经是当前项时**提前返回**，而 `onPressItem`
// （`onClose`）照跑——所以点那一行**什么也不会发生**，浮层却要收。用户点一个已经
// 高亮着的行，是为了「看一眼它在那儿」，不是要重播它；浮层留着的话他就得再找
// 一次关它的路。
//
// 🔴 **这一条是为 `viewPartRow` 里那句 `s.partOpen = false` 写的**：换到别的集
// 那条路上，`App.PlayPart` 的 `ClearTrack()` 会让「没有歌就收掉」顺手把浮层收掉，
// 于是**删掉 `viewPartRow` 里那一句，那一条照样绿**（变异验过）。而这一档
// `PlayPart` 在 `cur.Cid == cid` 处提前返回、**不清轨道**，所以收掉浮层的只剩
// `viewPartRow` 里那一句。
func TestPickingTheEpisodeAlreadyPlayingStillClosesTheDrawer(t *testing.T) {
	s, tt := onlineTester(t, multiPages(), false)
	partsReady(t, s, tt, 3)
	openPartsDrawer(t, tt)

	// 队列里 cid 是 777，也就是「第一集」——正在放的就是它。
	if err := tt.Click("第一集"); err != nil {
		t.Fatalf("点不了正在放的那一集：%v（文字：%q）", err, tt.Texts())
	}
	tt.Frame()

	if !s.app.Snapshot().HasTrack {
		t.Fatal("点正在放的那一集把歌停了——`PlayPart` 在已经是当前项时该直接返回")
	}
	if s.partOpen {
		t.Error("点完正在放的那一集，浮层还开着")
	}
}

// TestEveryEpisodeRowCarriesItsNumber 断「每一集有编号」那一条。
//
// 判据是**画面上真的画出了编号这几个字**（`Texts()` 里有一条逐字等于 "1"/"2"/"3"），
// 不是「某一行的字段里有 page」——`partRowsFor` 填对了而 view 没画的话，
// `TestPartRowsAreKeyedByTheirCid` 那条照样绿。
//
// ⚠️ **必须逐字相等，不能用 `HasText`**：它判的是「有没有一段文字**包含**这个词」，
// 而这一屏上包含 "1" 的地方有一堆（倍率的 "1x"、时长、播放位置……）。写成包含的话
// 这一条**一路绿**：第一版就是那么写的，把编号整个删掉它照样绿（变异验过）。
//
// ⚠️ 这一条量的是「画出来的编号」，**不是**「编号落在编号那一列」——像素那一层量过
// 三版，每一版都被布局的细节骗过去：第一版量到了面板左边圆角的抗锯齿，第二版量到了
// 标题自己（`Tester.Find` 给的是**整行**的框，`ListState.Label` 挂在行上）。文字这一层
// 量的是「这个应用到底画没画这个字」，而那才是验收要的东西；「它在标题左边」那一层是
// `viewPartRow` 里的布局，读代码就能看出来，不必用一条会骗人的像素断言去守。
func TestEveryEpisodeRowCarriesItsNumber(t *testing.T) {
	s, tt := onlineTester(t, multiPages(), false)
	partsReady(t, s, tt, 3)
	openPartsDrawer(t, tt)

	texts := tt.Texts()
	for _, want := range []string{"1", "2", "3"} {
		if !slices.Contains(texts, want) {
			t.Errorf("浮层里没有把 %q 画成一个独立的编号（逐字相等的文字里没有它），"+
				"画面上的文字有 %q", want, texts)
		}
	}
}

// TestTheDrawerMarksTheEpisodeThatIsPlaying 断「标出正在播的那一集」。
//
// Biu 那一支是 `variant="flat" color="primary"`（整行浅主色底 + 主色字），
// 所以这里断的是**那一行有没有被主色铺上**——而只有正在放的那一行是。
//
// ⚠️ 判据是**框里主色像素占多少**，不是「某个点上是什么颜色」，也不是「那个点等于
// 主题里那个 Accent」。两个理由，都是量出来的：
//
//  1. `Tester.Find` 给回来的是**标题那三个字自己**的框（第一版按「行的框」用，
//     量出来数字在它左边 8~18 像素，方向整个反了）。而那个框里对正在放的那一行还
//     压着字——字是主色文字（白），按点取样会取到字而不是底。
//  2. 框里离编号那几个像素远一点的地方**不是纯主题底色**（实测 {24 24 27} 往里
//     一步是 {22 22 25}，圆角的抗锯齿），所以「等于 `t.Background`」这种判据在
//     这里不成立。
//
// 「主色像素」判的是**绿调**（绿通道比红通道高出一截）而不是逐字比对：那是这个
// 应用主色的形状（`DefaultPrimaryColor` 一族），而深浅两套主题换的是同一个值。
func TestTheDrawerMarksTheEpisodeThatIsPlaying(t *testing.T) {
	s, tt := onlineTester(t, multiPages(), false)
	partsReady(t, s, tt, 3)
	openPartsDrawer(t, tt)

	for _, c := range []struct {
		title string
		want  bool
	}{
		{"第一集", true}, // 队列里 cid 是 777，也就是第一集
		{"第二集", false},
		{"第 3 集", false},
	} {
		r, ok := tt.Find(c.title)
		if !ok {
			t.Fatalf("浮层里没有 %q，文字有 %q", c.title, tt.Texts())
		}
		share := accentShare(tt.Image(), r)
		if c.want && share < 0.5 {
			t.Errorf("「%s」那一行只有 %.0f%% 是主色（{30 215 96} 那一种），want 一半以上——"+
				"正在放的那一集没标出来，用户分不清自己在哪一集", c.title, share*100)
		}
		if !c.want && share > 0 {
			t.Errorf("「%s」那一行有 %.0f%% 是主色，want 一点都没有——"+
				"没在放的那一行也被染上了主色，标出来的那一集就没有意义了", c.title, share*100)
		}
	}
}

// TestThereIsNoPartsKeyForASinglePartVideo 断验收的反向那一半。
//
// Biu 的条件是 `playItem.hasMultiPart`——只有一个分 P 的稿子没有「选」的意思，
// 摆一个键在那儿只会让人以为点得动什么。
func TestThereIsNoPartsKeyForASinglePartVideo(t *testing.T) {
	s, tt := onlineTester(t, nil, false) // 默认那一份：一集
	partsReady(t, s, tt, 1)

	if n := len(s.app.Parts()); n != 1 {
		t.Fatalf("fixture 回的是 %d 集，want 1", n)
	}
	if _, ok := tt.Find("分集"); ok {
		t.Errorf("只有一个分 P 还摆了分集入口，文字有 %q", tt.Texts())
	}
}

// TestThePartsKeySaysWhyItIsThere 断那个键悬停时说的话。
//
// 它是唯一一处**不点开就知道**自己在一个多长的列表的第几格的地方：分集多的稿子
// （第 100 集那种）光靠点进去数是数不出来的。
func TestThePartsKeySaysWhyItIsThere(t *testing.T) {
	s, tt := onlineTester(t, multiPages(), false)
	partsReady(t, s, tt, 3)

	r, ok := tt.Find("分集")
	if !ok {
		t.Fatalf("播放条上没有分集入口，文字有 %q", tt.Texts())
	}
	// 悬停才出提示，而且要**停够 0.6 秒**才出（`TooltipBase` 的文档），所以是
	// Move + Advance + Frame，不是 Move + Frame。
	tt.Move(r.X+r.W/2, r.Y+r.H/2)
	tt.Advance(700 * time.Millisecond)
	tt.Frame()
	if !tt.HasText("共 3 集") {
		t.Errorf("悬停的时候没有说一共几集，文字有 %q", tt.Texts())
	}
	if !tt.HasText("正在放第 1 集") {
		t.Errorf("悬停的时候没有说正在放第几集，文字有 %q", tt.Texts())
	}
}

// TestTheDrawerSaysWhyWhenTheyCannotBeAsked 是「问不到就说问不到」。
//
// 一片空白在界面上和「这一首没有分集」长得一模一样；而实际上分集是问 B 站问来的，
// 问不到是网络/风控的事，不是这一首没有分集。
//
// 🔴 **这一条是从播放条上的键点进去的**，不是直接写 `s.partOpen = true`：入口要
// 是只在「问到多于一条」时才摆，那句话在真机上是死的（一条都问不到的时候从哪儿也
// 进不去）。这和满屏那条（`TestThePartsPanelSaysWhyWhenTheyCannotBeAsked`）是同一
// 个道理，只是入口换了一个。
func TestTheDrawerSaysWhyWhenTheyCannotBeAsked(t *testing.T) {
	s, tt := onlineTester(t, nil, true)
	waitFrames(t, tt, "问失败", func() bool { return s.app.PartsErr() != nil })
	tt.Frame()

	// 问不到的时候入口也得摆着，否则那句人话没地方显示。
	if _, ok := tt.Find("分集"); !ok {
		t.Fatalf("一条分集都没问到，播放条上却连入口都没有，文字有 %q", tt.Texts())
	}
	openPartsDrawer(t, tt)
	if !tt.HasText("问不出分集") {
		t.Errorf("浮层上没有说为什么，文字有 %q", tt.Texts())
	}
}

// TestOpeningTheFullPlayerClosesThePartsDrawer 断「浮层状态不会漏到下一个界面」。
//
// 满屏开着的时候播放条**根本不画**（`viewMain` 里 `if !s.full.open`），那个键不在、
// 浮层也不建，而 `partOpen` 还留着 true。用户从满屏退回普通窗口的那一刻，这一列会
// 自己弹出来——他什么都没按。
//
// ⚠️ **直接调 `openFullPlayer`，不点封面**：点封面的那一下，`ui.Popover` 自己会
// 「点外面关掉」——于是删掉 `openFullPlayer` 里那一句 `s.partOpen = false`，这条
// **照样绿**（变异验过）。而那一句要守的是**别的入口**（快捷键将来也会开满屏，
// 那一下不走 Popover 的关闭逻辑）。
func TestOpeningTheFullPlayerClosesThePartsDrawer(t *testing.T) {
	s, tt := onlineTester(t, multiPages(), false)
	partsReady(t, s, tt, 3)
	openPartsDrawer(t, tt)

	if !s.partOpen {
		t.Fatalf("浮层没开起来（文字：%q）", tt.Texts())
	}
	s.openFullPlayer()
	if !s.full.open {
		t.Fatalf("满屏没开起来（文字：%q）", tt.Texts())
	}
	if s.partOpen {
		t.Error("开满屏之后播放条那个分集浮层还开着——从满屏退回普通窗口时它会自己弹出来")
	}
}

// 窄窗口下这一票动了播放条，而 900 是这个应用自己的窗口下限
// （`WindowOptions` 的 `MinWidth`）。下面三条量的是「加了这个键之后播放条还是不是
// 原来那个样子」。
const (
	// partsTitleMinW 是歌名最少要留多宽。15 号粗体一个汉字 15，150 是十个字多——
	// 比它更窄的话一行放不下十个字，标题就开始一个字一行地竖着排，而**界面上看不出
	// 坏了**（字还在那儿）。用它自己的自然宽（208）当阈值的话，把播放条整体缩窄
	// 一点点就会红，那测的就不是「有没有被这一票挤坏」了。
	partsTitleMinW = 150
	// partsScrubMinW 是进度条最少要留多宽。它是 `Grow(1)` 的，所以宽度随窗口变；
	// 低于 60 的话拖一个位置要点半天，那不是「不好用」而是「用不了」。
	partsScrubMinW = 60
)

// TestThePartsKeyDoesNotBreakThePlayBar 盯住「加了第四个控件之后播放条还是好的」。
//
// ⚠️ 这一条和 `TestTheOwnerIsNeverSqueezedOutOfExistence`、`TestTheMetaRowDoesNot
// SquashTheTitle` **不是同一条**：那两条量的是 `metaBarTester` 喂进去的一份假
// `State`，而分集入口是**逐帧问 App** 才有的（单 P 不摆，见 `viewPartButton`）——
// 假 State 那两条里它压根不在，于是它们量的是「没有这个键」的播放条。真窗口上放多
// P 的歌时它一直摆着，挤的是哪一样只有这一条量得到。
func TestThePartsKeyDoesNotBreakThePlayBar(t *testing.T) {
	const title = "一首歌的名字很长很长很长很长"
	const owner = "一个非常非常非常非常非常非常长的 UP 主名字"

	for _, width := range []int{900, 1000, 1200, 1280} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			s, tt := onlineTester(t, multiPages(), false, at(width, 460))
			partsReady(t, s, tt, 3)

			if _, ok := tt.Find("分集"); !ok {
				t.Fatalf("宽度 %d：多 P 的歌在放，播放条上却没有分集入口，文字有 %q", width, tt.Texts())
			}
			r, ok := tt.Find(owner)
			if !ok {
				t.Fatalf("宽度 %d：画面上没有那个 UP 名：%q", width, tt.Texts())
			}
			if r.W < ownerNameMinW {
				t.Errorf("宽度 %d：UP 名只剩 %.1f 宽（下限 %d）——它被这个键挤没了，"+
					"而界面上看不出坏了（按钮和字都还在）", width, r.W, ownerNameMinW)
			}
			tb, ok := tt.Find(title)
			if !ok {
				t.Fatalf("宽度 %d：画面上没有歌名：%q", width, tt.Texts())
			}
			if tb.W < partsTitleMinW {
				t.Errorf("宽度 %d：歌名只剩 %.1f 宽（下限 %d）——它开始一个字一行地竖排了，"+
					"而界面上看不出坏了（字还在那儿）", width, tb.W, partsTitleMinW)
			}
			sb, ok := tt.Find("播放进度")
			if !ok {
				t.Fatalf("宽度 %d：画面上没有进度条：%q", width, tt.Texts())
			}
			if sb.W < partsScrubMinW {
				t.Errorf("宽度 %d：进度条只剩 %.1f 宽（下限 %d）——拖一个位置要点半天，"+
					"那不是不好用，是用不了", width, sb.W, partsScrubMinW)
			}
			// 键本身没被挤成「分…」：无头 Tester 读的是**控件的文本**，被截的只是
			// 画出来的那几个字，所以这里量画出来的宽度（和
			// `TestTheMetaLabelsAreNotTruncated` 同一个道理）。
			pb, _ := tt.Find("分集")
			if natural := naturalButtonTextWidths(t, []string{"分集"})["分集"]; pb.W+0.5 < natural {
				t.Errorf("宽度 %d：那个键只画出了 %.1f 宽，它本来要 %.1f——被截了（用户在"+
					"这一个键上认的是「分集」两个字，缺字就只能靠猜）", width, pb.W, natural)
			}
		})
	}
}

// TestTheDrawerDoesNotComeBackOnItsOwn 断「浮层状态不会在没人的时候自己回来」。
//
// 开着的浮层跨过「没有歌」那一段（歌放完了、用户按了停止、`App.ClearTrack`）：
// 这一段里 `viewTrackMeta` 一格都不画，连带 `viewPartButton` 也没被调到，于是
// `partOpen` 留着 true。等下一首多 P 的歌放起来——**浮层自己弹出来**，而用户从
// 头到尾什么都没按。
//
// 换歌那一档是收得住的（`viewPartButton` 每一帧都在跑），所以这一条量的是**停歌**
// 那一档：它和换歌走的是两条不同的路，只盯换歌会漏掉它。
func TestTheDrawerDoesNotComeBackOnItsOwn(t *testing.T) {
	s, tt := onlineTester(t, multiPages(), false)
	partsReady(t, s, tt, 3)
	openPartsDrawer(t, tt)

	// 歌停了：这一段里播放条上一个键都不画。
	s.app.ClearTrack()
	tt.Frame()
	tt.Frame()
	if s.partOpen {
		t.Fatalf("没有歌了，分集浮层还开着（partOpen=true）")
	}

	// 下一首还是多 P 的。
	if err := s.app.Enqueue(app.Item{
		Source: "online", Type: "mv", Bvid: "BVagain", Cid: "1",
		Title: "下一首", Artist: "一个非常非常非常非常非常非常长的 UP 主名字", ArtistMid: 12345,
	}); err != nil {
		t.Fatalf("加下一首：%v", err)
	}
	// `Enqueue` 只在队列空的时候开始播，而这里队列里还留着上一首——所以要点名放它。
	if err := s.app.PlayAt(s.app.Queue().Len() - 1); err != nil {
		t.Fatalf("放下一首：%v", err)
	}
	waitFrames(t, tt, "下一首放起来", func() bool { return s.app.Snapshot().HasTrack })
	tt.Frame()

	if s.partOpen {
		t.Error("停歌之后浮层自己弹回来了——用户什么都没按")
	}
	if _, ok := tt.Find("第一集"); ok {
		t.Errorf("停歌之后画面上还留着上一首的分集列表，文字有 %q", tt.Texts())
	}
}

// TestThePartsKeyCostsNothingElseOnThePlayBar 钉住「摆对地方了」这件事。
//
// # 为什么是「和另一档一样宽」而不是「宽多少」
//
// 这一票最花时间的一件事就是决定这个键摆哪儿，而**摆错了量得出来的后果**不止一
// 种：歌名被压窄、进度条被压窄、UP 名被挤没、两个控件叠在一起。所以判据不能是某
// 一个阈值——阈值定高了定低了都是在猜「多窄算坏」。
//
// 这里的判据是**自比**：同一个 fixture、同一个窗口，**单 P**（不摆这个键）和
// **多 P**（摆）各量一次，两次量出来的那几样**必须一样宽**。「摆这个键不许花掉别人
// 的钱」于是变成一条不用猜的断言：搬错了地方，多出来的那份宽度总得从**某一样**上
// 出，而那一栏立刻对不上。
//
// ⚠️ **UP 名不在这个「不许花」的单子里**，量出来它确实会让出 24（900 上 78 → 54）：
// 那一行里它是**唯一一个该让位的**——`Grow(1)` + `MinWidth` + `Ellipsis("…")`，
// 让位就是它存在的理由（见 `viewArtist`）。它只要还认得出是哪个 UP 就够了，那条由
// `TestThePartsKeyDoesNotBreakThePlayBar` 的 `ownerNameMinW` 守着。
//
// ⚠️ 这张单子**量出来过一次相反的结论**：第一版把键摆在中间那一列的第二行（挨着
// 播放模式），看上去那一行有富余，而一量歌名从 208 掉到 182、进度条从 94 掉到 80。
// 摆在歌名下面这一行两样都不掉——**富余是算出来的，宽度是量出来的**。
//
// ⚠️ **两档必须是同一首歌**：用的是同一份 fixture 的同一首（bvid、标题、UP 名都
// 一样），只有「B 站回几集」不同，差别只有那一个键。
func TestThePartsKeyCostsNothingElseOnThePlayBar(t *testing.T) {
	for _, width := range []int{900, 1000, 1200} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			without := playbarWidths(t, width, nil)
			with := playbarWidths(t, width, multiPages())

			if _, ok := with["分集"]; !ok {
				t.Fatalf("宽度 %d：多 P 的那一档连分集入口都没画出来，量到的是 %v", width, with)
			}
			if _, ok := without["分集"]; ok {
				t.Fatalf("宽度 %d：单 P 的那一档居然也摆了分集入口——这一条量的是宽度，"+
					"入口该不该摆是另一条（TestThereIsNoPartsKeyForASinglePartVideo）", width)
			}
			for _, what := range []string{
				"一首歌的名字很长很长很长很长", // 歌名：被压窄就是竖排，而界面上看不出坏了
				"播放进度", // 进度条：拖一个位置要点半天，那不是不好用，是用不了
				"播放模式", "下载", "加歌",
			} {
				if without[what] == 0 || with[what] == 0 {
					t.Fatalf("宽度 %d：%q 有一档量不到（单 P %.0f / 多 P %.0f）——"+
						"量的是「它有多宽」，量不到就没法比", width, what, without[what], with[what])
				}
				if abs(without[what]-with[what]) > partsWidthSlack {
					t.Errorf("宽度 %d：「%s」单 P %.1f / 多 P %.1f（差 %.1f）——"+
						"摆这个键花掉了别的东西的宽度", width, what, without[what], with[what],
						abs(without[what]-with[what]))
				}
			}
			// UP 名是唯一让位的（见上面那段），但要让得认得出是哪个 UP。
			if got := with["一个非常非常非常非常非常非常长的 UP 主名字"]; got < ownerNameMinW {
				t.Errorf("宽度 %d：UP 名让位之后只剩 %.1f 宽（下限 %d）——"+
					"它让位是因为它是那一行里唯一该让的，但让没了就是「这一首没有 UP 主」",
					width, got, ownerNameMinW)
			}
		})
	}
}

// partsWidthSlack 是上面那条比宽度的容差。
//
// 4 是量出来的**噪声上限**：900 上同一首歌的两档，标题量出来是 206 与 208（多 P 那档
// 反而宽 2.3）——那是文字换行到某一行的宽度的小数抖动，不是布局让了位。而真正「摆错
// 了」的代价是 14 到 26（见上面那段），差一个数量级，4 分得开这两者。
const partsWidthSlack = 4

// playbarWidths 造一个外壳，把播放条上那几样各量多宽。
//
// pages 是给假 B 站的：`nil` 是 fixture 的默认那一份（一集），`multiPages()` 是三集。
func playbarWidths(t *testing.T, width int, pages []any) map[string]float32 {
	t.Helper()
	s, tt := onlineTester(t, pages, false, at(width, 460))
	partsReady(t, s, tt, lenOf(pages))
	tt.Frame()

	out := map[string]float32{}
	for _, what := range []string{
		"分集", "一首歌的名字很长很长很长很长",
		"一个非常非常非常非常非常非常长的 UP 主名字",
		"播放进度", "播放模式", "下载", "加歌",
	} {
		if r, ok := tt.Find(what); ok {
			out[what] = r.W
		}
	}
	return out
}

// lenOf 说这份 fixture 回几集（`nil` 那一份回一集）。
func lenOf(pages []any) int {
	if pages == nil {
		return 1
	}
	return len(pages)
}

func abs(f float32) float32 {
	if f < 0 {
		return -f
	}
	return f
}

// accentGreenGap 是「一个像素算不算主色」的门槛：绿通道要比红通道高出这么多。
//
// 主题里那个 Accent 是 {30 215 96}（绿 - 红 = 185），面板底色是 {24 24 27}（差 0），
// 正文是浅灰（差也是 0）——所以这一个条件既认得出主色，又不会把字和底算进去。
// 60 是从「主题色差 185」倒推的一半：认得出，又不会因为抗锯齿的边缘像素误判。
const accentGreenGap = 60

// accentShare 是一个框里「主色」像素占的比例。
func accentShare(img *image.RGBA, r ui.Rect) float64 {
	x0, y0 := int(r.X), int(r.Y)
	w, h := int(r.W), int(r.H)
	if w <= 0 || h <= 0 {
		return 0
	}
	n := 0
	for y := y0; y < y0+h; y++ {
		for x := x0; x < x0+w; x++ {
			if x < 0 || y < 0 || x >= img.Bounds().Dx() || y >= img.Bounds().Dy() {
				continue
			}
			c := img.RGBAAt(x, y)
			if int(c.G)-int(c.R) >= accentGreenGap {
				n++
			}
		}
	}
	return float64(n) / float64(w*h)
}
