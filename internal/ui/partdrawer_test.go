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
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/service/bili"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/tok"
	"github.com/egoist/mygo/ui"
)

// partsReady 等分集真的问回来、而且歌已经在放。
//
// want 是**这一份 fixture 回几集**：多 P 那几条是 3，单 P 那一条是 1。写死 3 的话
// 单 P 那条永远等不到——而它等的恰恰是「只有一集」这个状态。
//
// ⚠️ **`HasTrack` 和「问到几集」两个条件都要**：只等集数的话会量到「这一瞬」——
// 换歌和 `App.PlayPart` 都要先 `ClearTrack()`（`online.go`），而那几百毫秒里
// `HasTrack` 恒为 false，播放条上什么都不画（`viewTrackActions` 第一行就 return）。
// 那时候量到的是一个空的播放条，不是坏了。
func partsReady(t *testing.T, s *Shell, tt *ui.Tester, want int) {
	t.Helper()
	// ⚠️ **判据里要带上画面**：App 的状态是后台那一趟写回来的，而播放条上那个
	// 「分集」键是**逐帧问 App** 才摆的（`viewPartButton`）——而
	// `partsWorthEntry` 在「还没问到（`len(parts)==0`）而这一首问得出」时是
	// **摆**这个键的（键上的提示是「正在问这一首有哪几集」）。
	//
	// `waitFrames` 是「画一帧 → 再问判据」，所以判据一旦为真就立刻返回，读到的却是
	// **上一帧**画出来的东西：单 P 那一条于是读到还没收掉的那个键，红的时候症状是
	// 「只有一个分 P 却摆了分集入口」，而功能是好的（和
	// `collectionbatch_test.go` 的 `TestTheStarToggles` 是同一件事）。
	//
	// 画面该是什么样不是新定的规矩，就是产品自己那一句：问到多于一条才摆。
	waitFrames(t, tt, fmt.Sprintf("这一首放起来而且分集问回来了（要 %d 集）", want), func() bool {
		if !s.app.Snapshot().HasTrack || len(s.app.Parts()) != want {
			return false
		}
		_, onScreen := tt.Find("分集")
		return onScreen == (want > 1)
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
	for _, want := range []string{"P1 · 第一集", "P2 · 第二集", "P3 · 第 3 集"} {
		if !tt.HasText(want) {
			t.Fatalf("浮层里没有 %q，文字有 %q", want, tt.Texts())
		}
	}
	if !tt.HasText("分集 · 3 集") {
		t.Errorf("浮层头上没有说一共几集，文字有 %q", tt.Texts())
	}

	// 点第三集：立刻切过去，**不用先退出再进**。
	if err := tt.Click("P3 · 第 3 集"); err != nil {
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
	// 队列里**还是那一项**（不是插了一首新的）：换集是同一项换了 cid（`Item.Identity()`
	// 里 mv 带上 cid），插一项新的会让队列里多出一份同一集、抽屉画成两行。
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
	if err := tt.Click("P1 · 第一集"); err != nil {
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

	// 原型 `popHtml('parts')`：每一项写成 `P{n} · {标题}`，编号和标题在同一行文字里。
	texts := tt.Texts()
	for _, want := range []string{"P1 · 第一集", "P2 · 第二集", "P3 · 第 3 集"} {
		if !slices.Contains(texts, want) {
			t.Errorf("浮层里没有 %q（每一项该是 `P{n} · {标题}`），画面上的文字有 %q", want, texts)
		}
	}
}

// TestTheDrawerMarksTheEpisodeThatIsPlaying 断「标出正在播的那一集」。
//
// Biu 那一支是 `variant="flat" color="primary"`（整行**淡主色底** + 主色字，
// 上游展开成 `bg-primary/20 text-primary-600`），所以这里断的是**那一行的底
// 是不是主色 20% 那一层淡底**——而只有正在放的那一行是。
//
// ⚠️ **判据是「那一行底上的众数」**：那一块里既有底也有字，而底占绝大多数——
// 众数才是底。取的是**整行**的框（`Tester.Find` 给的是整行，500×64）：四角那点
// 圆角的抗锯齿像素是面板底色，只有几十个，不影响众数。
//
// ⚠️ **改之前那一行是实心主色底**（票 #323）：那一版的判据是「框里主色像素占
// 多少」，而淡底**不算**主色像素（浅色下 #d2f7df 的绿 − 红 = 37，门槛是 60）——
// 换成淡底之后那条判据不适用了，钉的是「底就是那一层淡主色」。
//
// ⚠️ **深浅两套都量**：淡底在深色下是 #193e29，压在 #18181b 上只差十几个通道，
// 只量浅色的话深色下那一层根本看不出来。
//
// ⚠️ **判据有「两半」，少一半等于没断**：
//
//   - **正在放的那一行要的是那一层淡主色底**；
//   - **没在放的那一行不许是实心主色底**——把兄弟行也刷成实心主色底的话，
//     「哪一集在放」就说不清了，而只断前一半的话那一条照样绿。
//
// 改之前那一版（票 #323 之前）断的是**两半都有**：它逐行量「框里主色像素占
// 多少」，没在放的那些行有上限。换成淡底之后判据换了，可**后一半不能跟着
// 一起丢**——它就是从那一版搬过来的。
func TestTheDrawerMarksTheEpisodeThatIsPlaying(t *testing.T) {
	for _, dark := range []bool{false, true} {
		name := "浅色"
		if dark {
			name = "深色"
		}
		t.Run(name, func(t *testing.T) {
			s, tt := onlineTester(t, multiPages(), false)
			tt.SetDark(dark)
			partsReady(t, s, tt, 3)
			openPartsDrawer(t, tt)
			tt.Frame()

			accent := rgbaOf(tok.Of(BuildTheme(app.DefaultSettings(""), dark)).AccentText)

			// 队列里 cid 是 777，也就是「第一集」——正在放的就是它。
			cur, ok := tt.Find("P1 · 第一集")
			if !ok {
				t.Fatalf("浮层里没有「P1 · 第一集」，文字有 %q", tt.Texts())
			}
			sib, ok := tt.Find("P2 · 第二集")
			if !ok {
				t.Fatalf("浮层里没有「P2 · 第二集」，文字有 %q", tt.Texts())
			}
			img := tt.Image()
			count := func(r ui.Rect) (n int) {
				for _, px := range bandPixels(img, r) {
					if nearRGBA(px, accent, 4) {
						n++
					}
				}
				return n
			}
			curInk, sibInk := count(cur), count(sib)
			t.Logf("%s：accent-text 像素 正在放 %d，旁边 %d", name, curInk, sibInk)
			// 原型 `.pop-opt[aria-checked="true"]`：accent-text 的字 600 加右边一枚对勾，没有淡底。
			// 判据：正在放那一行里有 accent-text 的墨，旁边那一行一点没有。
			if curInk == 0 {
				t.Errorf("%s：正在放那一行里一个 accent-text 像素都没有（字和对勾该是 accent-text）", name)
			}
			if sibInk != 0 {
				t.Errorf("%s：没在放的那一行里有 %d 个 accent-text 像素——「哪一集在放」说不清了", name, sibInk)
			}
		})
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

// TestThePartsKeySaysTheSameThingAsTheDrawer 断键上的悬停提示和抽屉里那一行字
// **说的是同一件事**。
//
// ⚠️ **这条钉的是评审抓到的那一处**：问失败之后悬停提示还在说「正在问这一首有哪几集」
// 而抽屉里说的是「问不出分集：HTTP 403」——同一时刻两句话打架。而它更难受的地方是
// `App` 问失败之后 **15 秒内不会再问**（`partsRetryAfter`），那句「正在问」于是挂在
// 键上 15 秒，长到用户以为它在慢慢加载。
//
// 两处现在走 `partsHowItIs` 这**一个出口**，所以这里把两边的输出摆在一起逐档比。
// 额外断一句「问不出的时候不许出现『暂时问不到』」——那是 `likeStarTooltip` 对赞用的
// 那句话（网络抖动的常态），拿它来说分集会把常态说成故障。
//
// ⚠️ 断的是**这个纯函数**而不是悬停那一下的像素：悬停要指针停够 0.6 秒才出
// （`TooltipBase` 的规矩），把它和「那句话对不对」搅在一起，飘的是指针时机而症状报在
// 错的原因上。悬停本身画得出来由 `TestThePartsKeySaysWhyItIsThere` 断。
func TestThePartsKeySaysTheSameThingAsTheDrawer(t *testing.T) {
	cur := app.Item{Cid: "777"}
	for _, c := range []struct {
		name         string
		parts        []bili.VideoPart
		worthAsking  bool
		err          error
		wantTip      string // 提示里必须有的那一段
		sameAsDrawer bool   // 提示和抽屉是不是同一句话（同一个出口 → 一样）
	}{
		{
			name: "问到了：说几集、第几集", parts: multiParts(), worthAsking: true,
			wantTip: "共 3 集，正在放第 1 集", sameAsDrawer: false,
		},
		{
			name: "还在问", worthAsking: true,
			wantTip: "正在问这一首有哪几集", sameAsDrawer: true,
		},
		{
			// 🔴 评审抓到的那一档。
			name: "问失败：不能还在说「正在问」", worthAsking: true,
			err:          errors.New("bili: 网络没通: HTTP 403"),
			wantTip:      "问不出分集：bili: 网络没通: HTTP 403",
			sameAsDrawer: true,
		},
		{
			name: "这一首压根没有分集（本地文件 / 音乐库）", worthAsking: false,
			wantTip: "这一首没有分集", sameAsDrawer: true,
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			tip := partEntryTooltip(c.parts, cur, c.worthAsking, c.err)
			if !strings.Contains(tip, c.wantTip) {
				t.Errorf("悬停提示是 %q，里面没有 %q", tip, c.wantTip)
			}
			if c.sameAsDrawer {
				want := partsHowItIs(len(c.parts), c.worthAsking, c.err)
				if !strings.Contains(tip, want) {
					t.Errorf("悬停提示是 %q，抽屉里那一行说的是 %q——两处说的是同一件事，"+
						"说法不一样就等于在界面上撒两个矛盾的谎", tip, want)
				}
			}
			if c.err != nil && strings.Contains(tip, "正在问") {
				t.Errorf("问失败了，提示里却还有「正在问」：%q", tip)
			}
			if c.err != nil && strings.Contains(tip, "暂时问不到") {
				t.Errorf("问失败了，提示却说的是「暂时问不到」——那是赞那一键的说法，"+
					"用在分集上会把网络抖动的常态说成故障：%q", tip)
			}
		})
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
	// partsTitleMinW 是歌名最少要留多宽。
	//
	// ⚠️ **它按「一行放得下几个字」算，不按某个像素数算**：歌名是 **16** 号字
	// （票 #173 从 15 号粗体改过来的），一个汉字 16，这里留**两个半字** = 40。
	// 比它更窄的话，标题就开始一个字一行地竖着排，而**界面上看不出坏了**
	// （字还在那儿）。
	//
	// 🔴 **它从 128 降到 40 是票 #219 逼出来的**（改之前是**八个字**）：三枚键
	// 从歌名下面那一行搬到了**封面右边第三段**（上游 `playbar/left/index.tsx:85`
	// 的 `flex items-center`——横着并排），于是它们吃的是**歌名那一列**的宽度：
	// 左边那一列是 281（`playbarLeftMinW`，本票角标之后；评论数收成挂在图形
	// 右上角外侧的角标、不占布局），而现在分给歌名那一列的只有 **76**
	//（281 − 封面 52 − 两道 8 的缝 − 那一排键 137），歌名自己还要让出音质
	// 徽章（28 + 一道 6 的缝）——**42**（两个多字）。
	//
	// ⚠️ **本票之后那一列不再分两档**（票 #682 时「评论数还没回来 → 111」那
	// 一档没有了）：角标不占布局，那一排键带不带数字都是 137（四枚 32 + 三道
	// 3 的键间缝）——所以这个下限（40）在**哪一档**上都量得出来。
	//
	// ⚠️ **上游那边更狠**：它那一列里歌名右边也横着三枚键，而 1 份在 1200 宽
	// （上游自己的默认窗口）上只有 233.6，歌名那一列是 **65.6**——比我们的 76
	// 还窄（报告 §2.8 的表）。所以这条门槛量的是「有没有被挤成竖条」，不是
	// 「跟上游一样」。
	partsTitleMinW = 40
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
			assertIconKey(t, tt, "分集")
		})
	}
}

// TestTheDrawerDoesNotComeBackOnItsOwn 断「浮层状态不会在没人的时候自己回来」。
//
// 开着的浮层跨过「没有歌」那一段（歌放完了、用户按了停止、`App.ClearTrack`）：
// 这一段里 `viewTrackActions` 一格都不画，连带 `viewPartButton` 也没被调到，于是
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
// 决定这个键摆哪儿的时候，**摆错了量得出来的后果**不止一种：歌名被压窄、
// 进度条被压窄、UP 名被挤没、两个控件叠在一起。所以判据不能是某一个阈值——
// 阈值定高了定低了都是在猜「多窄算坏」。
//
// 这里的判据是**自比**：同一个 fixture、同一个窗口，**单 P**（不摆这个键）和
// **多 P**（摆）各量一次。
//
// # 🔴 票 #219 之后：让位的换人了
//
// 改之前这个键在**歌名下面那一行**（和 UP 名并排），所以它让位的是**那一行
// 里的歌名和 UP 名**（那一行是 `Grow(1)` 的，谁都可伸缩）。改之后它在**第三段**
// ——封面右边**横着并排**的那一行（上游 `playbar/left/index.tsx:85` 的
// `flex items-center`），而**歌名/UP 名那一列在它左边**。
//
// 于是现在的事实是（实测，900 / 1000 / 1200 三档都一样）：
//
//	让位的：歌名和 UP 名，各让 **32**（这个键自己那一格，`iconHitMd`）
//	不让的：进度条、播放模式、下载、加歌——**一个像素都不让**
//
// ⚠️ **32 是这个键自己的宽度，不是「额外花掉的」**：第三段的宽度跟着内容走
// （上游那一段也是 `flex`），所以它多出来的那一格是从**左边那一列里**出的。
//
// ⚠️ **进度条和右边那一组一个像素都不许少**：进度条被压窄就是「拖一个位置要
// 点半天」，那不是不好用，是用不了；而右边那一组是**绝对定位**贴右沿的，
// 它压根不在左列里。
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

			// 不许被花掉的那几样。
			for _, what := range []string{
				"播放进度", // 进度条：拖一个位置要点半天，那不是不好用，是用不了
				"播放模式", "下载当前歌曲",
			} {
				if without[what] == 0 || with[what] == 0 {
					t.Fatalf("宽度 %d：%q 有一档量不到（单 P %.0f / 多 P %.0f）——"+
						"量的是「它有多宽」，量不到就没法比", width, what, without[what], with[what])
				}
				if widthGap(without[what]-with[what]) > partsWidthSlack {
					t.Errorf("宽度 %d：「%s」单 P %.1f / 多 P %.1f（差 %.1f）——"+
						"摆这个键花掉了别的东西的宽度", width, what, without[what], with[what],
						widthGap(without[what]-with[what]))
				}
			}

			// 让位的那两样（歌名 / UP 名）：让的**只能是这个键自己那一格**。
			for _, what := range []string{
				"一首歌的名字很长很长很长很长",          // 歌名
				"一个非常非常非常非常非常非常长的 UP 主名字", // UP 名
			} {
				if without[what] == 0 || with[what] == 0 {
					t.Fatalf("宽度 %d：%q 有一档量不到（单 P %.0f / 多 P %.0f）——"+
						"量的是「它有多宽」，量不到就没法比", width, what, without[what], with[what])
				}
				// 原型把分集键放进了右列（绝对定位），左列的歌名 / UP 名一个像素都不让。
				if d := widthGap(without[what] - with[what]); d > partsWidthSlack {
					t.Errorf("宽度 %d：「%s」单 P %.1f / 多 P %.1f（让位 %.1f）——"+
						"分集键在右列，不该挤左列", width, what, without[what], with[what], d)
				}
			}

			// UP 名让位之后仍要认得出是哪个 UP。
			if got := with["一个非常非常非常非常非常非常长的 UP 主名字"]; got < ownerNameMinW {
				t.Errorf("宽度 %d：UP 名让位之后只剩 %.1f 宽（下限 %d）——"+
					"它让位是因为它是那一列里唯一该让的，但让没了就是「这一首没有 UP 主」",
					width, got, ownerNameMinW)
			}
		})
	}
}

// TestThePartsKeySharesTheRowWithTheOtherKeys 钉「这个键和右列那一组在同一行」。
//
// ⚠️ **喜欢歌单原型之后**：赞 / 收藏两枚键去掉了，「分集」挪在右列
// （`viewPlayBarRight`）——它和右列那几枚（下载 / 歌词 / 队列）在同一条
// 水平线上，而不是左边那一列。
func TestThePartsKeySharesTheRowWithTheOtherKeys(t *testing.T) {
	s, tt := onlineTester(t, multiPages(), false, at(900, 460))
	partsReady(t, s, tt, 3)

	part, ok := tt.Find("分集")
	if !ok {
		t.Fatalf("画面上找不到分集入口，文字有 %q", tt.Texts())
	}
	const sameLine = 2
	for _, other := range []string{"下载当前歌曲", "歌词", "播放列表"} {
		r, ok := tt.Find(other)
		if !ok {
			t.Fatalf("画面上找不到 %q，文字有 %q", other, tt.Texts())
		}
		mid := part.Y + part.H/2
		otherMid := r.Y + r.H/2
		if widthGap(mid-otherMid) > sameLine {
			t.Errorf("分集那一格的垂直中心在 y=%.1f，而 %q 那一格在 y=%.1f（差 %.1f）——"+
				"它们不在同一条水平线上：分集挪在右列那一组",
				mid, other, otherMid, widthGap(mid-otherMid))
		}
	}
}

// TestThePartsPopoverIsOnlyAsTallAsItsContent 断分集弹层按内容收高（原型 `.pop`：内边距 8，
// `h5` 加每集一个 36 高的 `pop-opt`），不是一块固定高的死面板。
func TestThePartsPopoverIsOnlyAsTallAsItsContent(t *testing.T) {
	s, tt := onlineTester(t, multiPages(), false, at(900, 460))
	partsReady(t, s, tt, 3)
	openPartsDrawer(t, tt)

	panel, ok := tt.Find("分集列表")
	if !ok {
		t.Fatalf("画面上找不到「分集列表」这一块，文字有 %q", tt.Texts())
	}
	// 三集：上下内边距 16 + 标题 ≤ 35 + 3 × 36 = 159；留 12 的余量。
	if panel.H > 171 {
		t.Errorf("三集的时候这块是 %.0f 高，want ≤ 171——面板在最后一行下面空出了一片", panel.H)
	}
	if panel.H < 3*36 {
		t.Errorf("三集的时候这块只有 %.0f 高，连三行 36 的选项都放不下", panel.H)
	}
}

// TestThePartsPopoverMatchesThePrototype 钉住分集弹层的行高和宽度：每一项 36 高
// （`.pop-opt` 内边距 8 + 13 号字行高 1.55），面板最小宽 200（`.pop { min-width: 200px }`）。
func TestThePartsPopoverMatchesThePrototype(t *testing.T) {
	s, tt := onlineTester(t, multiPages(), false, at(900, 560))
	partsReady(t, s, tt, 3)
	openPartsDrawer(t, tt)

	panel, ok := tt.Find("分集列表")
	if !ok {
		t.Fatalf("画面上找不到「分集列表」这一块，文字有 %q", tt.Texts())
	}
	if panel.W < 200 {
		t.Errorf("分集面板只有 %.1f 宽，want ≥ 200（原型 .pop 的 min-width）", panel.W)
	}
	first, ok := tt.Find("P1 · 第一集")
	if !ok {
		t.Fatalf("画面上找不到第一集，文字有 %q", tt.Texts())
	}
	second, ok := tt.Find("P2 · 第二集")
	if !ok {
		t.Fatalf("画面上找不到第二集，文字有 %q", tt.Texts())
	}
	if got := second.Y - first.Y; widthGap(got-36) > 1 {
		t.Errorf("两集之间隔了 %.2f，want 36（原型 .pop-opt）", got)
	}
}

// assertIconKey 断 label 那一格是**一个图标键**（`iconButton` + `Size(iconHitMd,
// iconHitMd)`），而不是一个文字键。
//
// # 判据一：那一格的**点击区是正方的**
//
// 旁边赞 / 收藏那两个键量的都是 **32.00×32.00**（`iconHitMd`），而一个文字键的
// 框是**照着字长出来**的——实测 `ui.Button(c, "分集")` 是 **25.80×15.31**，宽高
// 差 10.49。所以「宽和高差不超过 2」这一条，图标键过、文字键不过。
//
// ⚠️ **它们走的是 `iconHitMd` 而不是 `iconHit`**（票 #219 把图标从 14 改成 18
// 之后）：那两个数**都是 32**（上游那一档：图标键的盒子恒是 32×32），差的是
// 内边距——`iconHit` 是每边 9（配 14 的图标），`iconHitMd` 是每边 7（配 18）。
//
// ⚠️ **这一段原来写的是「赞 / 收藏 / 下载三个键」**：下载跟着上游搬到了播放条
// 右边那一组（票 #127），它不再是这一行的邻居——而它用的 `iconHitMd` 恰好也是
// 32（上游那一档：图标键的盒子恒是 32×32），所以**量出来的数碰巧一样**，正是
// 这类注释最坏的地方：数字对得上，没人会去查它说的是哪几枚。
//
// 这条判据量的还是**用户看得见的东西**：三个正方的键中间夹一个长条，界面上就是
// 「这个键没跟上图标化」。
//
// # 判据二：框里真的画着图形
//
// `assertGlyphPainted`（icons_test.go 的那个）只判「那一块不是空的」——所以它单用
// 抓不住文字键，只能配着上面那一条。
//
// # ⚠️ 一条走过的弯路：**不要用「那个字在 Texts() 里出现几次」**
//
// 想法是「图标键只有无障碍名那一处、文字键有 Label 和字两处」，量出来图标键 1 次、
// 带 `.Label()` 的文字键 2 次，看着分得开。**评审用单参数文字键打穿过一次**：
// `ui.Button(c, "分集")`（也就是 #118 之前赞/收藏/下载三个键的写法）的**文字本身
// 就是它的无障碍名**，于是它也只出现 **1** 次，和图标键一模一样。根子在这儿：
// `Texts()` 分不出「哪一处是 Label、哪一处是字」，所以计数在这个形状上根本不成立。
func assertIconKey(t *testing.T, tt *ui.Tester, label string) {
	t.Helper()
	r, ok := tt.Find(label)
	if !ok {
		t.Fatalf("画面上找不到 %q：%q", label, tt.Texts())
	}
	assertGlyphPainted(t, tt, label)

	const squareTolerance = 2
	if widthGap(r.W-r.H) > squareTolerance {
		t.Errorf("%q 那一格是 %.2f×%.2f（宽高差 %.2f），不是方的——旁边赞/收藏那两枚"+
			"量的都是 32.00×32.00。一个文字键的框是照着字长出来的，所以这一格画的是"+
			"「分集」两个字而不是一枚图标（图标化之后这一格再没有字，而字是用户一眼"+
			"认出它是什么的唯一东西）",
			label, r.W, r.H, widthGap(r.W-r.H))
	}
}

// widthGap 是两个宽度之差的绝对值。
func widthGap(f float32) float32 {
	if f < 0 {
		return -f
	}
	return f
}

// partsWidthSlack 是上面那条比宽度的容差。
//
// 4 是量出来的**噪声上限**：900 上同一首歌的两档，标题量出来是 206 与 208（多 P 那档
// 反而宽 2.3）——那是文字换行到某一行的宽度的小数抖动，不是布局让了位。而真正「摆错
// 了」的代价是 14 到 26（见上面那段），差一个数量级，4 分得开这两者。
const partsWidthSlack = 4

// partsKeyWidth 是分集那一枚**自己占的那一格**的宽度：32。
//
// ⚠️ **写的是字面的 32，不是 `iconHitMd`**：阈值取自被测对象的话，把它改小
// 这一条就照样绿——而「它只让出自己那一格」正是这一条要断的东西。
//
// ⚠️ **它不是「额外花掉的钱」**：第三段的宽度跟着内容走（上游那一段也是
// `flex`），所以它多出来的那一格是从**左边那一列里**出的——歌名和 UP 名让出
// 的就是这 32（实测 900 / 1000 / 1200 三档都一样）。
const partsKeyWidth = 32

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
		"播放进度", "播放模式", "下载当前歌曲",
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

// accentGreenGap 是「一个像素算不算主色」的门槛：绿通道要比红通道高出这么多。
//
// 主题里那个 Accent 是 {30 215 96}（绿 - 红 = 185），面板底色是 {24 24 27}（差 0），
// 正文是浅灰（差也是 0）——所以这一个条件既认得出主色，又不会把字和底算进去。
// 60 是从「主题色差 185」倒推的一半：认得出，又不会因为抗锯齿的边缘像素误判。
//
// ⚠️ **它现在只被别处用着**（历史记录那个开关、本地音乐那一处）：分集抽屉
// 「正在放」那一行改成了**淡主色底**（票 #323），而淡底**不算**主色像素
// （浅色下 #d2f7df 的绿 − 红 = 37）——那一处的判据换成了「底是不是那一层淡底」。
const accentGreenGap = 60

// TestThePartsKeyMatchesItsRowNeighbours 锁住：播放条上「分集」那一枚和同排的下载键是同一种
// 图标键（原型 app.js:929 `.icon-btn`）——36 见方、同一条中线，悬停是 hover 底 + 正文色，不变主色。
// 曾经出错的行为：它是旧的 `iconKey`（32 + 每边 7 内边距、悬停图形变主色），和同排对不齐。
func TestThePartsKeyMatchesItsRowNeighbours(t *testing.T) {
	s, tt := onlineTester(t, multiPages(), false)
	partsReady(t, s, tt, 3)

	part, ok := tt.Find("分集")
	if !ok {
		t.Fatalf("播放条上没有分集入口，文字有 %q", tt.Texts())
	}
	dl, ok := tt.Find("下载当前歌曲")
	if !ok {
		t.Fatalf("播放条上没有下载键，文字有 %q", tt.Texts())
	}
	if widthGap(part.W-36) > 0.5 || widthGap(part.H-36) > 0.5 {
		t.Errorf("分集键 %.1f×%.1f，want 36×36（同排 .icon-btn）", part.W, part.H)
	}
	if widthGap((part.Y+part.H/2)-(dl.Y+dl.H/2)) > 0.5 {
		t.Errorf("分集键中线 y=%.1f，下载键 y=%.1f：不在一条线上", part.Y+part.H/2, dl.Y+dl.H/2)
	}

	tt.Move(part.X+part.W/2, part.Y+part.H/2)
	tt.Frame()
	img := tt.Image()
	for y := int(part.Y); y < int(part.Y+part.H); y++ {
		for x := int(part.X); x < int(part.X+part.W); x++ {
			if p := img.RGBAAt(x, y); int(p.R)-int(p.B) > 60 {
				t.Fatalf("悬停时分集键 (%d,%d) 是主色像素 %v：悬停该换正文色，不是主色", x, y, p)
			}
		}
	}
}
