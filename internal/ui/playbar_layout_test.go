// 播放条重做那一票（GitHub #135）的验收。
//
// 这一族断的是**画面上的东西**：进度条的长度随窗口变、三键的左右次序、音量
// 浮层里那根竖滑块、旋钮只在悬停时出现、播放键的尺寸。
//
// ⚠️ **断的都是位置和像素，不是函数名**：这一票改的每一件事在界面上都是
// 「看得见但不出错」的那种——次序反了照样点得动，旋钮不画也照样能拖，
// 宽度不变也照样能 seek。判据只有量得出来的那几样才算数。
package ui

import (
	"fmt"
	"image"
	"image/color"
	"os"
	"testing"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/ink"
	mygoui "github.com/egoist/mygo/ui"
)

// playbarTester 只画播放条，State 从外面传进来。
//
// 和 `metaBarTester`（playbar_meta_test.go）的差别有三个：**能指定深浅**、
// 这一份总是带一个**有时长的**状态（旋钮那条测试要进度条是**能用的**：框架的
// `Hovered()` 对禁用的控件恒为 false，`mygo/ui/input.go:1116`），以及
// **它把应用的主题装上了**（`applyFrameTheme`）——
//
// ⚠️ **最后那一条是这一族要读颜色的前提**：真窗口里每一帧开头都装一次主题
// （`viewMain` 的第一行），而只画播放条的那一支不装——于是画出来的是**框架默认
// 那份主题**（主色是蓝的 #2563eb），而设置里的主色是绿的（#1ed760）。按绿的去找
// 主色像素，一个都找不到。
func playbarTester(t *testing.T, st app.State, width, height int, dark bool) (*mygoui.Tester, *Shell) {
	t.Helper()
	if height == 0 {
		height = coverWinH
	}
	var b *testShell
	b = newTestShell(t, at(width, height), systemIsDark(dark), withView(func(s *Shell) func(*mygoui.Context) {
		return func(c *mygoui.Context) {
			s.wake.set(c)
			s.applyFrameTheme(c)
			s.viewPlayBar(c, st)
		}
	}))
	tt := b.tt
	tt.Frame()
	return tt, b.s
}

// barState 是这一族用的那份状态：一首有歌、有时长、位置在 0 的。
//
// ⚠️ **位置必须是 0**：旋钮在 `frac=0` 那头，而填过的那一段轨道也是主色——
// 位置不在 0 的话，「轨道上有一段主色」和「旋钮画出来了」就分不开了
// （见 `TestTheProgressKnobIsOnlyDrawnWhileHovered`）。
func barState() app.State {
	return app.State{
		HasTrack: true, Title: "一首歌",
		Artist: "某个 UP 主", ArtistMid: 12345,
		Rate: 1, Volume: 0.8, PlayMode: app.PlayModeLoop,
		Duration: 100 * time.Second, Position: 0,
	}
}

// TestTheProgressBarGrowsWithTheWindow 断「窗口越宽，进度条越长」。
//
// ⚠️ **断的是单调，不是某个像素数**：进度条该有多长取决于窗口有多宽、也取决于
// 右边那一组有多宽（音量收成图标之后它窄了 141），所以钉死一个数的话，改动
// 右边任何一枚键都会把它顶红——而那不是回归。
//
// 中间那一档（1200）**必须**在：只断 900 和 1600 的话，「中间那一列不参与剩余
// 空间分配、于是两端一样宽」这种坏法在两端是看不出来的（两端都是 80）。
//
// 🔴 **票 #216 之后这条仍然成立，但机制换了**：进度条不再是「铺满中间那一列」，
// 而是中间那一列的 **3/4**（上游 `w-3/4`），而中间那一列是三列里的**三份**
// （上游的 1:3:1）。新机制下的确切长度由
// `TestTheMiddleColumnIsThreePartsWide` 那条钉着，这一条只管「越宽越长」。
func TestTheProgressBarGrowsWithTheWindow(t *testing.T) {
	var prevW float32
	var prevAt int
	for _, width := range []int{900, 1200, 1600} {
		tt, _ := playbarTester(t, barState(), width, 0, false)

		bar, ok := tt.Find("播放进度")
		if !ok {
			t.Fatalf("宽度 %d：画面上找不到进度条，文字有 %q", width, tt.Texts())
		}
		if width != 900 && bar.W <= prevW {
			t.Errorf("宽度 %d：进度条只有 %.1f 宽，而宽度 %d 那档是 %.1f——"+
				"它没有跟着窗口变长", width, bar.W, prevAt, prevW)
		}
		prevW, prevAt = bar.W, width
	}
}

// TestTheTransportKeysArePrevPlayNext 断三键的**位置顺序**。
//
// ⚠️ **断的是谁在左谁在右，不是「三个键都在」**：三个键都在而次序反了，
// 别的测试全绿——而次序反了的后果是「用户从全屏退回播放条时，肌肉记忆里
// 「中间是播放」的位置按下去是下一首」。
//
// 上游的 `MusicPlayControl` 就是 `RiSkipBackFill` / `RiPlayCircleFill` /
// `RiSkipForwardFill`（`components/music-play-control/index.tsx:16-18`），
// 而**我们自己的**满屏播放器和迷你播放器也是这个次序。
func TestTheTransportKeysArePrevPlayNext(t *testing.T) {
	tt, _ := playbarTester(t, barState(), 1200, 0, false)

	prev, ok := tt.Find("上一首")
	if !ok {
		t.Fatalf("画面上找不到「上一首」，文字有 %q", tt.Texts())
	}
	play, ok := tt.Find("播放")
	if !ok {
		t.Fatalf("画面上找不到「播放」，文字有 %q", tt.Texts())
	}
	next, ok := tt.Find("下一首")
	if !ok {
		t.Fatalf("画面上找不到「下一首」，文字有 %q", tt.Texts())
	}

	if !(prev.X < play.X && play.X < next.X) {
		t.Errorf("三键的次序是「上一首 %.1f · 播放 %.1f · 下一首 %.1f」——"+
			"播放键得在**中间**（上游的 `MusicPlayControl` 就是上一首 · 播放 · 下一首，"+
			"而我们自己的满屏播放器也是那个次序）", prev.X, play.X, next.X)
	}
}

// TestTheTransportKeysSitOnTheVerticalCenterLine 断「三键落在窗口的竖直中轴上」。
//
// 🔴 **这是这一票的头一条**：上游把三键和进度条都摆在窗口的竖直中轴上
// （`layout/playbar/center/index.tsx:12` 的 `items-center`），我们原来三键靠左、
// 进度条铺满整列，**两者的中轴互相对不上**（实测差 152 像素）。
//
// ⚠️ **断的是「三键那一排的中心 == 窗口的中心」，不是「播放键在窗口正中间」**：
// 播放键是三枚里最宽的一枚（48），它在三键那一排里**正好居中**（32+24+24 =
// 80 = 160 的一半），所以这两条是一回事——但写成前者，将来播放键换了尺寸也不会
// 误红。
//
// ⚠️ **每一档都要在**：偏差是「左右两列差多少」的函数，而 760 那一档
// （测试里最窄的窗口）差得最多。
func TestTheTransportKeysSitOnTheVerticalCenterLine(t *testing.T) {
	for _, width := range []int{760, 900, 1200, 1600} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			tt, _ := playbarTester(t, barState(), width, 0, false)

			prev, ok := tt.Find("上一首")
			if !ok {
				t.Fatalf("画面上找不到「上一首」，文字有 %q", tt.Texts())
			}
			next, ok := tt.Find("下一首")
			if !ok {
				t.Fatalf("画面上找不到「下一首」，文字有 %q", tt.Texts())
			}

			got := (prev.X + next.X + next.W) / 2
			want := float32(width) / 2
			if absFloat(got-want) > 1 {
				t.Errorf("三键那一排的中心在 %.1f，而窗口的竖直中轴在 %.1f（差 %.1f）——"+
					"三键没有落在中轴上（上游 `center/index.tsx:12` 的 `items-center`）",
					got, want, got-want)
			}
		})
	}
}

// TestTheTransportKeysAre24Apart 断三键之间的空隙是 24。
//
// 上游是 `space-x-6`（`components/music-play-control/index.tsx:19`）= 24，
// 我们原来只有 12：那时候三枚键挤成一堆，中间那枚——**用户真正要按的那一枚**
// ——左右只各留 12 的空。
//
// ⚠️ **断的是键与键之间的那两道缝**，不是三键的总宽：总宽还含着三枚键自己的
// 宽度（32 / 48 / 32），改了键的尺寸就会把它顶红，而那不是回归。
func TestTheTransportKeysAre24Apart(t *testing.T) {
	tt, _ := playbarTester(t, barState(), 1200, 0, false)

	prev, ok := tt.Find("上一首")
	if !ok {
		t.Fatalf("画面上找不到「上一首」，文字有 %q", tt.Texts())
	}
	play, ok := tt.Find("播放")
	if !ok {
		t.Fatalf("画面上找不到「播放」，文字有 %q", tt.Texts())
	}
	next, ok := tt.Find("下一首")
	if !ok {
		t.Fatalf("画面上找不到「下一首」，文字有 %q", tt.Texts())
	}

	for _, gap := range []struct {
		name string
		got  float32
	}{
		{"上一首和播放之间", play.X - (prev.X + prev.W)},
		{"播放和下一首之间", next.X - (play.X + play.W)},
	} {
		if absFloat(gap.got-transportGap) > 0.5 {
			t.Errorf("%s只有 %.1f，而它该是 %d（上游的 `space-x-6`）",
				gap.name, gap.got, transportGap)
		}
	}
}

// TestTheMiddleColumnIsThreePartsWide 断中间那一列是三列里的三份（1:3:1），
// 而进度那一行是它的 3/4 且居中。
//
// 上游：`playbar/index.tsx:26` 的
// `grid-cols-[minmax(0,1fr)_minmax(0,3fr)_minmax(0,1fr)]`，加上
// `music-play-progress/index.tsx:25` 的 `w-3/4`。
//
// 🔴 **这两条只能一起量**：中间那一列在画面上**没有可见的边界**（它的底色和
// 播放条一样），所以「它有多宽」量不出来——能从画面上量出来的是**它里面的
// 东西**：三键那一行和进度那一行，而两者都在那一列里居中。于是：
//
//	进度行宽 = 0.75 × (3/5 × 可用宽 − 左右各 24 的内边距)
//
// ⚠️ **「3/5」是中间那一列在五份里占的三份**（1:3:1 ⇒ 左边 1 份、中间 3 份、
// 右边 1 份，共 5 份）。任何一处不照上游来（中间列不是三份、进度行不是 3/4、
// 内边距不是 24），这个数就会对不上。
//
// ⚠️ **居中单独断**：宽度对得上而位置偏了，那也是坏法（进度条在窗口里偏左，
// 而上面那三键在正中——那正是这一票要修的那一种坏法）。
func TestTheMiddleColumnIsThreePartsWide(t *testing.T) {
	for _, width := range []int{760, 900, 1200, 1600} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			tt, _ := playbarTester(t, barState(), width, 0, false)

			pos, ok := tt.Find(clock(0))
			if !ok {
				t.Fatalf("画面上找不到左边那个读数，文字有 %q", tt.Texts())
			}
			total, ok := tt.Find(clock(100 * time.Second))
			if !ok {
				t.Fatalf("画面上找不到右边那个读数，文字有 %q", tt.Texts())
			}

			// 进度那一行的宽（两个读数之间，两个读数各钉 48 宽）。
			got := total.X + total.W - pos.X
			// 中间那一列 = 三列里的三份（1:3:1）；内容区 = 那一列减去左右各 24。
			usable := float32(width - playbarPadX*2)
			want := 0.75 * (0.6*usable - 2*playbarCenterPadX)
			if absFloat(got-want) > 1 {
				t.Errorf("进度那一行只有 %.1f 宽，而它该是 %.1f——"+
					"中间那一列是三列里的三份（1:3:1），进度行是它的 3/4",
					got, want)
			}

			// 居中：进度行的中心 == 窗口的中心。
			mid := pos.X + got/2
			if absFloat(mid-float32(width)/2) > 1 {
				t.Errorf("进度那一行的中心在 %.1f，而窗口的竖直中轴在 %.1f——"+
					"它没有居中（上游 `w-3/4` + `items-center`）",
					mid, float32(width)/2)
			}
		})
	}
}

// TestTheLeftColumnIsOneFifthOfTheBar 断左边那一列是五份里的一份。
//
// 上一条量的是「中间那一列和三份的关系」，这一条量的是**左边那一列自己**：
// 歌名的框宽 = 歌名那一列的宽 = 左边那一列的宽 − 封面 − 缝，而**歌名够长时
// 它的框就是整个容器那么宽**（`MaxLines(1)` + 省略号）。
//
// ⚠️ **要歌名够长**：短歌名的框是它自己的自然宽，量的是字不是容器。
// ⚠️ **要有 UP 名**：没有 UP 名的话歌名下面那一行只要 120，左边那一列的最小
// 内容宽就不是 244，量到的就不是「轨道」而是「内容」。
// ⚠️ **窗口要够宽**（≥ 1252）：窄窗口上左边那一列是**内容撑出来的**（244，
// 见 `playbarLeftMinW`），不是轨道那么宽——那正是「溢出轨道」那一档。
func TestTheLeftColumnIsOneFifthOfTheBar(t *testing.T) {
	st := barState()
	st.Title = "一首歌的名字很长很长很长很长很长很长很长"

	for _, width := range []int{1280, 1400} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			tt, _ := playbarTester(t, st, width, 0, false)

			title, ok := tt.Find(st.Title)
			if !ok {
				t.Fatalf("画面上找不到歌名，文字有 %q", tt.Texts())
			}

			got := title.W
			want := float32(width-playbarPadX*2)/5 - coverSize - coverGap
			if absFloat(got-want) > 1 {
				t.Errorf("歌名那一列只有 %.1f 宽，而它该是 %.1f——"+
					"左边那一列是五份里的一份（上游的 1:3:1）", got, want)
			}
		})
	}
}

// absFloat 是两个浮点数相差多少（差一点点算 0）。
func absFloat(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}

// TestTheLeftColumnKeysAreNotCoveredByTheMiddleColumn 断左列**撑出轨道**的那
// 一截上的键点得动。
//
// 🔴 **这一条钉的是票 #216 踩到的一个坑**：中间那一列在 Grid 里是左列的
// **后一个兄弟**，而框架的命中测试取**最上面**那个含点的元素
// （`mygo/ui/input.go:145` 从 hits 列表末尾往前找），于是中间那一列那整个框
// （900 宽下从 x=189.6 起）把左列撑出来的那一截盖住了——**收藏、赞、音质徽章
// 这些在左列右端的键点了没反应**，而**界面上看不出坏了**（它们好好地画在
// 那儿，一根汗毛都没少）。
//
// ⚠️ **临界宽度在 1175 上下**：中间那一列的左沿 = 16 + (窗口 − 32) / 5，左列
// 的右端 = 244（`playbarLeftMinW`），两者一碰，左列右端那些键就全进了中间
// 那一列的框里。所以这里量的是**最窄的那几档**（应用的窗口下限是 900）。
//
// ⚠️ **判据是悬停**：指针停在键的正中心上，那句话要出来。悬停是命中链的第一
// 环——命中链错了，悬停和点击一起错，而**点击**更难断（点下去要弹东西，
// 弹的东西又是另一条路）。
func TestTheLeftColumnKeysAreNotCoveredByTheMiddleColumn(t *testing.T) {
	for _, width := range []int{900, 1000, 1100} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			tt, _ := playbarTester(t, barState(), width, 0, false)

			fav, ok := tt.Find("收藏")
			if !ok {
				t.Fatalf("宽度 %d：画面上找不到「收藏」，文字有 %q", width, tt.Texts())
			}
			tt.Move(fav.X+fav.W/2, fav.Y+fav.H/2)
			tt.Frame()
			if !tt.HasText(favTooltip(false)) {
				t.Errorf("宽度 %d：指针停在「收藏」上（x=%.1f..%.1f）却没有那句话——"+
					"它被中间那一列的命中框盖住了（那一片点了没反应，而界面上看不出坏了）",
					width, fav.X, fav.X+fav.W)
			}
		})
	}
}

// TestTheRightGroupHugsTheRightEdge 断右边那一组贴在播放条内容区的右沿，
// 而且**一枚键都没有跑到窗口外面去**。
//
// 🔴 **这一条钉的是票 #216 落地时踩到的一个坑**：第三列是 1 份（900 宽下
// 只有 173.6），而右边那一组是**定宽**的 243.2——框架给格子里那一块的宽度
// 是它**自己的内容宽**（于是比轨道宽），而横向排布在装不下的时候**一律
// 靠左排**，于是它从轨道的左沿开始排：最右边那枚「加歌」键落在 x=921.6，
// **而窗口只有 900**。那枚键看不见、点不着，**而界面上看不出坏了**（其余
// 几枚都好好地在那儿）。
//
// ⚠️ **断的是「最右边那枚键的右沿 == 内容区的右沿」**：内容区的右沿是窗口宽
// 减去右边那 16 的内边距（`playbarPadX`）。
//
// ⚠️ **竖直方向也断**：那一组在 88 高的播放条里**上下居中**（上游
// `playbar/right/index.tsx:13` 的 `items-center`）——它现在是绝对定位的，
// 高度得自己给，给歪了就会贴着上沿或下沿。
func TestTheRightGroupHugsTheRightEdge(t *testing.T) {
	for _, width := range []int{760, 900, 1200, 1600} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			tt, _ := playbarTester(t, barState(), width, 0, false)

			last, ok := tt.Find("加歌")
			if !ok {
				t.Fatalf("宽度 %d：画面上找不到「加歌」那一枚，文字有 %q", width, tt.Texts())
			}

			wantX := float32(width - playbarPadX)
			if absFloat(last.X+last.W-wantX) > 1 {
				t.Errorf("宽度 %d：最右边那枚键的右沿在 %.1f，而内容区的右沿在 %.1f——"+
					"它跑到窗口外面去了（那一枚看不见也点不着）",
					width, last.X+last.W, wantX)
			}

			wantY := float32(playbarHeight) / 2
			if absFloat(last.Y+last.H/2-wantY) > 1 {
				t.Errorf("宽度 %d：那一枚键的垂直中心在 %.1f，而播放条的正中在 %.1f——"+
					"它没有上下居中", width, last.Y+last.H/2, wantY)
			}
		})
	}
}

// TestThePlayKeyIsBigEnoughToHit 断播放键的尺寸**和它那一枚图形**。
//
// ⚠️ **量的是画面上那一片图形，不是常量**：这一票把播放键换成上游那一枚
// （`icons.go` 的 `iconButtonPlay`）——48×48 的盒子、**底下什么都没有**、
// 图形是前景色。于是量法整个换了一副：原来是「主色底上那一片墨迹」，
// 现在是**「播放条底色上那一片墨迹」**。
//
// 判据是四条：
//
//  1. **盒子是 48×48**（上游的 `size-12 min-w-12`）——它不跟别的图标键的 32 走。
//
//  2. **比上下首大**（上下首是 22 的图标、32 的点击区）。只断第 1 条的话，
//     一枚 48 的**空**盒子也算过。
//
//  3. 🔴 **图形本身够大**（≥ 36 宽）——**这一条是第 1 条抓不到的**：盒子还是 48
//     而图形画成 24 的话，前两条照样绿——而用户看见的那枚**图形**小了一半。
//
//  4. 🔴 **图形落在盒子中间**（左右各留 ≥ 2 像素，且居中）——图形画歪了或者
//     被裁掉（图标比盒子大）的话，前三条都抓不到。
//
// ⚠️ **用「暂停」那一张**：播放那一枚画的是**一个三角形**，而三角形落在
// 24×24 的画板上只占 11 宽——量它的墨迹宽度，48 的图标量出 12，30 的量出 7，
// 门槛定在哪儿都别扭。**暂停是一枚圆**（`RiPauseCircleFill`），圆占画布 20/24：
// 48 的图标量出 40 宽，30 的量出 25——门槛放在 36，两头都离得开。
func TestThePlayKeyIsBigEnoughToHit(t *testing.T) {
	st := barState()
	st.Playing = true
	tt, s := playbarTester(t, st, 1200, 0, false)
	// 指针挪到播放条之外（理由见 `TestThePlayKeyHasNoSolidBackground` 那一条）：
	// 悬停那一层压的是**整格**，量「图形有多宽」的时候不能把它算进来。
	tt.Move(600, 300)
	tt.Frame()

	play, ok := tt.Find("暂停")
	if !ok {
		t.Fatalf("画面上找不到「暂停」，文字有 %q", tt.Texts())
	}
	// ① 盒子是 48×48（上游那一枚的盒子，图形正好填满）。
	//
	// ⚠️ **这里写的是字面的 48，不是 `iconHitPlay`**：写成常量的话，把常量改回
	// 58 这一条就照样绿——而「盒子是 48」是上游源码里的那个数（`size-12
	// min-w-12`），它得由这一条钉住，不能由被测的那个常量钉住。
	const wantBox = 48
	if d := absFloat(play.W-wantBox) + absFloat(play.H-wantBox); d > 2 {
		t.Errorf("播放键的盒子是 %.1fx%.1f，而上游那一枚是 48×48"+
			"（`className=\"size-12 min-w-12\"`）——它不跟别的图标键的 32 走",
			play.W, play.H)
	}
	prev, ok := tt.Find("上一首")
	if !ok {
		t.Fatalf("画面上找不到「上一首」，文字有 %q", tt.Texts())
	}
	// ② 比上下首大。
	if play.W <= prev.W {
		t.Errorf("播放键 %v 比「上一首」%v 还小——它得是全条上最大的一枚",
			play, prev)
	}

	// ③④ 图形本身：播放条底色上那一片不是底色的像素。
	//
	// ⚠️ **底色是播放条自己的**（`t.Background`），不是主色：这一枚现在**底下
	// 什么都没有**，所以「不是底色的那一片」就是图形本身。
	bg := rgb(BuildTheme(s.app.SettingsSnapshot(), false).Background)
	lo, hi, n := inkExtent(tt.Image(), play, bg)
	if n == 0 {
		t.Fatalf("播放键上没画东西：%q", tt.Texts())
	}
	if hi-lo+1 < 36 {
		t.Errorf("播放键上的图形只有 %d 宽（%d..%d）——图标是 48，"+
			"而它原来只有 30（全条上最小的一枚）", hi-lo+1, lo, hi)
	}
	// 图形落在盒子中间：左右各留 ≥ 2 像素（它没被盒子裁掉），中点对得上。
	left := float32(lo) - play.X
	right := play.X + play.W - float32(hi+1)
	if left < 2 || right < 2 {
		t.Errorf("播放键上的图形离盒子边只剩 %.1f / %.1f 像素（图形 %d..%d，"+
			"盒子 %.1f..%.1f）——它被裁掉了一截", left, right, lo, hi,
			play.X, play.X+play.W)
	}
	if d := absFloat(float32(lo+hi)/2 - (play.X + play.W/2)); d > 1.5 {
		t.Errorf("播放键上的图形偏了 %.1f 像素（图形 %d..%d，盒子 %.1f..%.1f）——"+
			"它没有落在盒子中间", d, lo, hi, play.X, play.X+play.W)
	}
}

// TestThePlayKeyHasNoSolidBackground 断播放键**底下什么都没有**，而图形是**前景色**。
//
// ⚠️ **这一条是这一票的正题**：原来那一枚是 `ui.PrimaryButton`——一块 58×58 的
// **实心主色圆角方块**（主题圆角 8，**不是药丸**），里面一枚**深色**的圆。
// 上游那一枚是**无底色**的 `IconButton` + `RiPlayCircleFill size={48}`：
// **底下什么都没有**，圆用**前景色**画（深色下白、浅色下近黑）。
// **明暗正好反过来**，一眼就看不出是同一个东西。
//
// 三条判据：
//
//  1. **盒子的边缘是播放条的底色**——底下没有压着一块实色。
//  2. **图形是前景色**（`t.Text`），不是主色。
//  3. 🔴 **「正在播放」那一档也一样**（`RiPauseCircleFill`）：形状换了，颜色
//     没换——两档都断，「只改了播放那一档」这种坏法就抓得住。
//
// 🔴 **第 1 条要量「四条边的中点」，不能只量「四个角」**（评审指出来的）：
// 旧那一枚是**圆角 8** 的方块，四个角上**本来就是空的**（圆角之外没有填充），
// 于是「四角是底色」对一块实心方块**照样成立**——浅色那一档就抓不住这一票
// 自己的「改之前」那一种。**边的中点**才是实心方块和空按钮分开的地方：
// 圆角只吃掉四个角，边中点上是实心的。
//
// ⚠️ **深浅两套都断**：旧那一枚的图形是 `t.AccentText`（主色底上的字色）——
// **深色下那是白**（主色够亮），和前景色差 11 个通道，所以第 2 条抓得住；
// **浅色下那是 #18181b，和前景色一模一样**，第 2 条抓不住——**靠的是第 1 条**
// （边中点）。
func TestThePlayKeyHasNoSolidBackground(t *testing.T) {
	for _, dark := range []bool{false, true} {
		name := "浅色"
		if dark {
			name = "深色"
		}
		for _, playing := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/正在播放=%v", name, playing), func(t *testing.T) {
				st := barState()
				st.Playing = playing
				tt, s := playbarTester(t, st, 1200, 0, dark)
				// 指针挪到播放条之外：悬停那一层会压一层淡底，量「底下有没有
				// 实色」的时候不能把它算进来。
				//
				// ⚠️ **y=300 是「播放条之外」**：这一族只画播放条（`playbarTester`），
				// 而它摆在窗口的**顶上**（那 88 高），所以窗口的中段是空的。指针要是
				// 停在 (600, 20)，那正好落在播放键上——三键是居中的，x=600 就是
				// 中间那一枚。
				tt.Move(600, 300)
				tt.Frame()

				label := "播放"
				if playing {
					label = "暂停"
				}
				btn, ok := tt.Find(label)
				if !ok {
					t.Fatalf("画面上找不到 %q，文字有 %q", label, tt.Texts())
				}
				th := BuildTheme(s.app.SettingsSnapshot(), dark)
				bg := rgb(th.Background)
				accent := rgb(th.Accent)

				// ① 边缘是底色：底下没有一块实色。
				//
				// ⚠️ **四个角 + 四条边的中点都要量**：角上对**圆角方块**是空的
				// （抓不住旧那一枚），边中点才是实的（见上面那段）。
				//
				// ⚠️ **离边 2 像素**：抗锯齿那一圈是过渡色，贴着边量会读到它。
				x0, y0 := round32(btn.X), round32(btn.Y)
				x1, y1 := x0+int(btn.W)-1, y0+int(btn.H)-1
				probes := [][2]int{
					{x0 + 1, y0 + 1}, {x1 - 1, y0 + 1}, {x0 + 1, y1 - 1}, {x1 - 1, y1 - 1},
					{x0 + 2, y0 + int(btn.H)/2}, {x1 - 2, y0 + int(btn.H)/2},
					{x0 + int(btn.W)/2, y0 + 2}, {x0 + int(btn.W)/2, y1 - 2},
				}
				for _, p := range probes {
					if got := tt.Image().RGBAAt(p[0], p[1]); !sameColor(got, bg) {
						t.Errorf("%q 那一枚的边上 (%d,%d) 是 %v，而播放条底色是 %v——"+
							"它底下压着一块实色（上游那一枚底下什么都没有）",
							label, p[0], p[1], got, bg)
					}
				}

				// ② 图形是前景色，不是主色。
				got := inkColor(tt.Image(), btn, bg)
				if !sameColor(got, rgb(th.Text)) {
					t.Errorf("%q 那一枚的图形是 %v，而前景色是 %v——"+
						"上游那一枚的圆是用**前景色**画的（深色下白、浅色下近黑）",
						label, got, rgb(th.Text))
				}
				if sameColor(got, accent) {
					t.Errorf("%q 那一枚的图形是 %v，和主色一样——"+
						"上游那一枚不是主色的", label, got)
				}
			})
		}
	}
}

// TestThePlayKeyTurnsAccentOnHover 断**悬停时图形变主色**。
//
// 上游那个 `IconButton`（`src/components/icon-button/index.tsx:11-19`）是
// `className="hover:text-primary text-inherit"`：**图形本身从前景色变成主色**，
// 不只是底下多一层淡底。
//
// ⚠️ **浅色下那一档不是主色本身**：主色是 #1ed760，压在白底上只有 1.9:1
// （`widgets/ink` 的文件头写着这件事），所以取「主色当字色时读得出来的那一档」
// （`ink.AccentOn`）——和 `flatbutton.go` 的 `flatCurrent` 是同一条道理：
// **主色当底和主色当字是两件事**。
func TestThePlayKeyTurnsAccentOnHover(t *testing.T) {
	for _, dark := range []bool{false, true} {
		name := "浅色"
		if dark {
			name = "深色"
		}
		t.Run(name, func(t *testing.T) {
			tt, s := playbarTester(t, barState(), 1200, 0, dark)
			// 指针挪到播放条之外（理由见上面那一条）。
			tt.Move(600, 300)
			tt.Frame()

			btn, ok := tt.Find("播放")
			if !ok {
				t.Fatalf("画面上找不到「播放」，文字有 %q", tt.Texts())
			}
			th := BuildTheme(s.app.SettingsSnapshot(), dark)
			bg := rgb(th.Background)
			idle := inkColor(tt.Image(), btn, bg)

			// 指针落到这一枚上。
			tt.Move(btn.X+btn.W/2, btn.Y+btn.H/2)
			tt.Frame()
			lit := inkColor(tt.Image(), btn, bg)

			want := rgb(ink.AccentOn(th))
			if !near(lit, want) {
				t.Errorf("悬停之后播放键的图形是 %v，而那一档该是主色 %v——"+
					"上游是 `hover:text-primary`：图形本身从前景色变成主色",
					lit, want)
			}
			if near(lit, idle) {
				t.Errorf("悬停之后播放键的图形还是 %v（没变）——"+
					"图形要自己变成主色，不只是底下多一层淡底", lit)
			}
		})
	}
}

// TestTheDisabledPlayKeyDoesNotTurnAccent 断**禁用**着的播放键**悬停时不变主色**。
//
// 🔴 **一枚禁用的键用主色告诉用户「这里点得动」**——那是错的（评审查出来的一条）。
//
// 原因在**顺序**：颜色是**建元素时**定的，而「悬停没有」也是**建元素时**问的
// （`Hovered()`）。问的时候如果**还没禁用**，那一刻问出来的是「它悬停着」——于是
// 一枚禁用的键也染上了主色（实测：浅色下悬停后变成 rgb(136,188,154) 的绿）。
//
// 修法是**先禁用、再问悬停**（`icons.go` 的 `iconButtonPlay`：禁不启用由调用方给，
// 因为那是**状态**决定的——没歌的时候那一枚是禁用的）。
//
// 判据：禁用的那一枚**悬停之后一个像素都不变**（框架把禁用键整体乘 0.5 不透明度，
// 所以它本来就该是暗的；而禁用的键**连那层淡底都不该有**——`widgets/flat` 的
// `shade` 是在**画的时候**重新问一遍 `IsDisabled()` 的）。
func TestTheDisabledPlayKeyDoesNotTurnAccent(t *testing.T) {
	for _, dark := range []bool{false, true} {
		name := "浅色"
		if dark {
			name = "深色"
		}
		t.Run(name, func(t *testing.T) {
			st := barState()
			st.HasTrack = false // 没有歌 → 那一枚是禁用的
			tt, s := playbarTester(t, st, 1200, 0, dark)
			bg := rgb(BuildTheme(s.app.SettingsSnapshot(), dark).Background)

			btn, ok := tt.Find("播放")
			if !ok {
				t.Fatalf("画面上找不到「播放」，文字有 %q", tt.Texts())
			}
			// 指针先离开。
			tt.Move(600, 300)
			tt.Frame()
			idle := inkColor(tt.Image(), btn, bg)

			// 指针落到这一枚上。
			tt.Move(btn.X+btn.W/2, btn.Y+btn.H/2)
			tt.Frame()
			lit := inkColor(tt.Image(), btn, bg)

			if !sameColor(lit, idle) {
				t.Errorf("禁用着的播放键悬停之后从 %v 变成了 %v——"+
					"一枚禁用的键不该用主色告诉用户「这里点得动」", idle, lit)
			}
			// 顺带断一句「它不是主色」：主色是绿的，前景色是中性色。
			if absInt(int(lit.G)-int(lit.R)) > 12 {
				t.Errorf("禁用着的播放键悬停之后是 %v——那不是主色该有的样子", lit)
			}
		})
	}
}

// inkColor 是 box 里「不是底色」的那一片像素的众数色——**图形本身的颜色**。
//
// ⚠️ **判据和底色不一样**：这一枚底下什么都没有，所以播放条底色上「不是底色的
// 那一片」就是图形。众数而不是平均：抗锯齿那一圈是过渡色，平均出来谁也不像。
func inkColor(img *image.RGBA, box mygoui.Rect, bg color.RGBA) color.RGBA {
	var samples []color.RGBA
	for y := max(int(box.Y), img.Rect.Min.Y); y < min(int(box.Y+box.H), img.Rect.Max.Y); y++ {
		for x := max(int(box.X), img.Rect.Min.X); x < min(int(box.X+box.W), img.Rect.Max.X); x++ {
			if p := img.RGBAAt(x, y); distinctPixel(p, bg) {
				samples = append(samples, p)
			}
		}
	}
	if len(samples) == 0 {
		return color.RGBA{}
	}
	return modeOf(samples)
}

// inkExtent 是 box 里「不是底色」的那一片像素有多宽（左右沿和个数）。
//
// 判据是**和底色不一样**：播放键底下什么都没有，所以播放条底色上「不是底色的
// 那一片」就是图形（深色下白、浅色下近黑）。
//
// ⚠️ **左右沿是真的最小和最大**，不是「扫描顺序里第一个和最后一个」：扫描是逐行
// 从上往下的，一个**圆**（暂停那一枚就是 `RiPauseCircleFill`）的最后一行最窄，
// 于是「最后一个」落在圆的下沿（12 宽），而真正的左右沿是圆的最大处（40 宽）。
func inkExtent(img *image.RGBA, box mygoui.Rect, bg color.RGBA) (lo, hi, n int) {
	lo, hi = -1, -1
	for y := max(int(box.Y), img.Rect.Min.Y); y < min(int(box.Y+box.H), img.Rect.Max.Y); y++ {
		for x := max(int(box.X), img.Rect.Min.X); x < min(int(box.X+box.W), img.Rect.Max.X); x++ {
			p := img.RGBAAt(x, y)
			if distinctPixel(p, bg) {
				if lo < 0 || x < lo {
					lo = x
				}
				if x > hi {
					hi = x
				}
				n++
			}
		}
	}
	return lo, hi, n
}

// TestTheProgressKnobIsOnlyDrawnWhileHovered 断旋钮只在悬停或按下时画。
//
// 上游是 `hideThumb={!showThumb}`（`music-play-progress`）：那颗圆点一直亮着的话，
// 它在整条进度上全程抢注意力，而它是用户真正要抓的东西。
//
// 判据是**画面上有没有主色的那一团**：旋钮是主色的，而没有悬停的时候那一块
// 只有 4 像素的轨道（`t.Border`）——两者分得开。
//
// ⚠️ **位置必须在 0**（`barState`）：填过的那一段轨道也是主色，位置不在 0 的
// 时候「轨道上有一段主色」和「旋钮画出来了」在像素上是一回事。
func TestTheProgressKnobIsOnlyDrawnWhileHovered(t *testing.T) {
	for _, dark := range []bool{false, true} {
		name := "浅色"
		if dark {
			name = "深色"
		}
		t.Run(name, func(t *testing.T) {
			tt, s := playbarTester(t, barState(), 1200, 0, dark)

			bar, ok := tt.Find("播放进度")
			if !ok {
				t.Fatalf("画面上找不到进度条，文字有 %q", tt.Texts())
			}
			knob := knobSpot(bar)
			a := BuildTheme(s.app.SettingsSnapshot(), dark).Accent
			accent := color.RGBA{R: a.R, G: a.G, B: a.B, A: a.A}

			if n := nearPixels(tt.Image(), knob, accent); n != 0 {
				t.Errorf("没有悬停的时候旋钮却画出来了（%v 那一块有 %d 个接近主色的像素）",
					knob, n)
			}

			// 悬停到进度条上。
			tt.Move(bar.X+bar.W/2, bar.Y+bar.H/2)
			tt.Frame()

			if n := nearPixels(tt.Image(), knob, accent); n == 0 {
				t.Errorf("悬停之后旋钮没有画出来（%v 那一块一个接近主色的像素都没有）", knob)
			}
		})
	}
}

// knobSpot 是旋钮该在的那一块。
//
// ⚠️ **位置是算出来的，不是量出来的**：滑块的左边沿是**边框框**（含内边距），
// 而旋钮的中心在「内容框左边沿 + 半个旋钮」——内边距留的就是那半个旋钮
// （框架的规矩，`coreSlider` 的 `PaddingX(kw / 2)`），于是旋钮的中心正好是
// 「滑块左边沿 + 一个旋钮」。
func knobSpot(bar mygoui.Rect) mygoui.Rect {
	kw := knobHoverLook.knobW
	return mygoui.Rect{
		X: bar.X + kw - kw/2,
		Y: bar.Y + (bar.H-kw)/2,
		W: kw, H: kw,
	}
}

// nearPixels 数 box 里有多少个像素接近 want。
//
// 门槛是**每个通道各差 24 以内**：主色是 #1ed760，而抗锯齿的边上的像素是它和
// 底色混出来的——差得远的那些不算。
func nearPixels(img *image.RGBA, box mygoui.Rect, want color.RGBA) int {
	const slack = 24
	n := 0
	for y := max(int(box.Y), img.Rect.Min.Y); y < min(int(box.Y+box.H), img.Rect.Max.Y); y++ {
		for x := max(int(box.X), img.Rect.Min.X); x < min(int(box.X+box.W), img.Rect.Max.X); x++ {
			p := img.RGBAAt(x, y)
			if absInt(int(p.R)-int(want.R)) <= slack &&
				absInt(int(p.G)-int(want.G)) <= slack &&
				absInt(int(p.B)-int(want.B)) <= slack {
				n++
			}
		}
	}
	return n
}

// TestTheVolumePopoverHasAVerticalSlider 断音量浮层里有一根**竖**滑块，
// 底下是百分比。
//
// 上游 `music-volume` 那个浮层里就是一根 `orientation="vertical"` 的滑块加一个
// `{Math.round(volume * 100)}%`。
//
// ⚠️ **浮层是悬停才出来的**（上游是 300ms 延迟的 Tooltip），所以这一条先悬停
// 再量。
func TestTheVolumePopoverHasAVerticalSlider(t *testing.T) {
	tt, s := playbarTester(t, barState(), 1200, 0, false)

	hoverVolumeKey(t, tt)

	sl, ok := tt.Find("音量")
	if !ok {
		t.Fatalf("悬停之后浮层里没有那根音量滑块，文字有 %q", tt.Texts())
	}
	if sl.H <= sl.W {
		t.Errorf("浮层里那根滑块是 %v——它是横的，而上游那根是竖的"+
			"（`music-volume` 的 `orientation=\"vertical\"`）", sl)
	}
	if !tt.HasText("80%") {
		t.Errorf("浮层底下没有那个百分比，文字有 %q", tt.Texts())
	}

	// 🔴 **值映射也得是竖的**：只量「元素是竖的」抓不住「画的是横的」——
	// 元素的高和宽是摆出来的，画法换了它还是那么高那么宽，而用户看见的是一根
	// 横着的滑块（按在底下不动）。
	//
	// 框架的竖直映射是 `1-(y-cy)/ch`（`mygo/ui/value_input.go:207`）：**底下是 0、
	// 顶上是 1**。横着的话，按在底下（同一个 x）就是中间那一档。
	pressAt := func(y float32) {
		tt.Press(sl.X+sl.W/2, y)
		tt.Release(sl.X+sl.W/2, y)
		tt.Frame()
	}
	pressAt(sl.Y + sl.H - 2)
	if got := s.app.Snapshot().Volume; got > 0.15 {
		t.Errorf("按在滑块**底下**之后音量是 %v，want ≈0——它不是一根竖的滑块", got)
	}
	pressAt(sl.Y + 2)
	if got := s.app.Snapshot().Volume; got < 0.85 {
		t.Errorf("按在滑块**顶上**之后音量是 %v，want ≈1——它不是一根竖的滑块", got)
	}
}

// TestClickingTheVolumeIconMutes 断「点图标静音」真的接上了。
//
// ⚠️ **静音 = 音量 0**：播放器那边压根没有静音这一档，而应用层也只有音量
// （`App.SetVolume`）。所以这一条断的是**应用里的音量真的变成了 0**，而不是
// 「图标换了张」。
//
// 再点一下要**放回去**：放回去的是静音之前那个音量（80%），不是 50%。
func TestClickingTheVolumeIconMutes(t *testing.T) {
	h := newHarness(t)
	h.open(t)

	before := h.app.Snapshot().Volume
	if before <= 0 {
		t.Fatalf("起点就不对：音量是 %v", before)
	}

	if err := h.tt.Click("静音"); err != nil {
		t.Fatalf("点不动音量那一枚：%v（文字：%q）", err, h.tt.Texts())
	}
	h.tt.Frame()
	if got := h.app.Snapshot().Volume; got != 0 {
		t.Errorf("点了音量图标之后音量是 %v，want 0——它没有静音", got)
	}

	// 静音了之后那一枚换了个名字（上游的 `aria-label={isMuted ? "取消静音" : "静音"}`）。
	if err := h.tt.Click("取消静音"); err != nil {
		t.Fatalf("点不动「取消静音」：%v（文字：%q）", err, h.tt.Texts())
	}
	h.tt.Frame()
	if got := h.app.Snapshot().Volume; got != before {
		t.Errorf("取消静音之后音量是 %v，want %v——它没有放回静音之前那个音量",
			got, before)
	}
}

// TestTheWheelOverTheVolumeIconChangesTheVolume 断滚轮 ±0.05 真的接上了。
//
// 上游 `music-volume` 的 `onWheel`：`const delta = event.deltaY > 0 ? -0.05 : 0.05`。
// 0.05 和快捷键那一步（`shortcutVolumeStep`）是同一个数——两条路走一样的步子。
//
// ⚠️ **往上是加**：框架的滚轮事件 `DY > 0` 是往下滚，上游那边往下滚是**减**。
func TestTheWheelOverTheVolumeIconChangesTheVolume(t *testing.T) {
	h := newHarness(t)
	h.open(t)

	// ⚠️ **起点得在中档**：音量默认是 1（顶格），往上滚是 1.05 —— 钳完还是 1，
	// 于是这一条量出来的是「没变」，而它其实接上了。
	h.app.SetVolume(0.5)
	h.tt.Frame()

	before := h.app.Snapshot().Volume
	icon, ok := volumeKeyOnTheBar(h.tt)
	if !ok {
		t.Fatalf("播放条上找不到音量那一枚，文字有 %q", h.tt.Texts())
	}

	// 往上滚一格：音量 +0.05。
	h.tt.Scroll(icon.X+icon.W/2, icon.Y+icon.H/2, 0, -100)
	h.tt.Frame()
	if got := h.app.Snapshot().Volume; got != before+volWheelStep {
		t.Errorf("在音量图标上往上滚了一格之后音量是 %v，want %v（+%.2f）",
			got, before+volWheelStep, volWheelStep)
	}

	// 往下滚一格：音量 -0.05。
	h.tt.Scroll(icon.X+icon.W/2, icon.Y+icon.H/2, 0, 100)
	h.tt.Frame()
	if got := h.app.Snapshot().Volume; got != before {
		t.Errorf("在音量图标上往下滚了一格之后音量是 %v，want %v（滚回去）",
			got, before)
	}
}

// TestRenderPlayBarRedoShots 存图：播放条重做之后的样子，深浅各一套。
//
//	BIU_SHOT_DIR=/some/dir go test ./internal/ui/ -run TestRenderPlayBarRedoShots
//
// ⚠️ **深浅两套都要出**：旋钮是**主色**的，而播放条的底在浅色下就是白的
// （`t.Background` = `#ffffff`）——浅色那一套才看得出「一颗白旋钮落在白底上」
// 那种坏（那正是旋钮不能画成白色的原因）。
//
// ⚠️ **窗口只留 88 高**（播放条那一行）：整窗的截图在 #127 那一族里已经有了
// （`chrome_shot_test.go` 的 01-整窗-），这一族看的是播放条**自己**。
func TestRenderPlayBarRedoShots(t *testing.T) {
	dir := os.Getenv("BIU_SHOT_DIR")
	if dir == "" {
		t.Skip("设 BIU_SHOT_DIR 才会存图")
	}
	for _, dark := range []bool{false, true} {
		name := "浅色"
		if dark {
			name = "深色"
		}
		for _, width := range []int{1200, 1600} {
			tt, _ := playbarTester(t, barState(), width, playbarHeight, dark)
			// 指针停在进度条上：旋钮只在悬停或按下时画。
			bar, ok := tt.Find("播放进度")
			if !ok {
				t.Fatalf("画面上找不到进度条，文字有 %q", tt.Texts())
			}
			tt.Move(bar.X+bar.W*0.35, bar.Y+bar.H/2)
			tt.Frame()
			save(t, dir, fmt.Sprintf("70-播放条-%d-%s.png", width, name), tt)
		}

		// 音量浮层开着：一根竖滑块 + 底下的百分比。
		//
		// ⚠️ **这一张把播放条摆在窗口底下**（真窗口里它就在那儿）：浮层是从
		// 播放条**往上**弹的，摆在顶上就被窗口裁掉半张。
		tt, _ := bottomBarTester(t, barState(), 1200, 320, dark)
		hoverVolumeKey(t, tt)
		save(t, dir, "72-播放条-音量浮层-"+name+".png", tt)

		// 封面悬停：那一层黑罩和它正中那枚箭头（票 #173 把它放到 32）。
		//
		// ⚠️ **必须悬停才有**：那一层是 `wrap.Hovered()` 才画的，而箭头是**白色
		// 92%** 压在 50% 的黑罩上——不悬停就只剩一块空的封面。
		tt, _ = playbarTester(t, barState(), 1200, playbarHeight, dark)
		cover, ok := tt.Find("当前封面")
		if !ok {
			t.Fatalf("画面上找不到封面，文字有 %q", tt.Texts())
		}
		tt.Move(cover.X+cover.W/2, cover.Y+cover.H/2)
		tt.Frame()
		save(t, dir, "71-播放条-封面悬停-"+name+".png", tt)
	}
}

// TestRenderPlayKeyShots 存图：播放键改成上游那一枚之后的样子，深浅各一套。
//
//	BIU_SHOT_DIR=/some/dir go test ./internal/ui/ -run TestRenderPlayKeyShots
//
// ⚠️ **四张都要**：平时（图形是**前景色**）、悬停（图形**变主色**）、正在播放
// （形状换成 `RiPauseCircleFill`）、正在播放时悬停——「悬停变主色」是这一票的
// 一半，而**平时那一张和悬停那一张长得完全不一样**。
//
// 🔴 **每一张的文件名都要带上状态**（评审指出来的）：第一版把「悬停」那一张
// **写了两遍**（两档状态都写同一个文件名），后写的那一遍把先写的盖掉了——于是
// 报告里那张标着「悬停」的图**其实是「正在播放」那一档**，而「悬停 + 播放三角」
// 那一张**根本不存在**。这一条现在每一张的名字里都有状态。
//
// ⚠️ **窗口留 160 高、指针停在中段**：这一族只画播放条（`playbarTester`），而它
// 摆在窗口的**顶上**（那 88 高），所以窗口的中段是空的——指针要停在那儿才算
// 「不在任何一枚键上」（三键是居中的，x=600 就是中间那一枚）。
func TestRenderPlayKeyShots(t *testing.T) {
	dir := os.Getenv("BIU_SHOT_DIR")
	if dir == "" {
		t.Skip("设 BIU_SHOT_DIR 才会存图")
	}
	for _, dark := range []bool{false, true} {
		name := "浅色"
		if dark {
			name = "深色"
		}
		for _, playing := range []bool{false, true} {
			st := barState()
			st.Playing = playing
			label := "播放"
			idle, hover := "73-播放键-平时-"+name, "74-播放键-悬停-"+name
			if playing {
				label = "暂停"
				idle, hover = "75-播放键-正在播放-"+name, "76-播放键-正在播放时悬停-"+name
			}

			tt, _ := playbarTester(t, st, 1200, 160, dark)
			tt.Move(600, 130) // 播放条之外
			tt.Frame()
			save(t, dir, idle+".png", tt)

			// 悬停：图形变主色。
			btn, ok := tt.Find(label)
			if !ok {
				t.Fatalf("画面上找不到 %q，文字有 %q", label, tt.Texts())
			}
			tt.Move(btn.X+btn.W/2, btn.Y+btn.H/2)
			tt.Frame()
			save(t, dir, hover+".png", tt)
		}
	}
}

// bottomBarTester 画一条**摆在窗口底下**的播放条。
//
// ⚠️ **播放条在真窗口里是贴着底的**（`viewMain` 的最后一块），而音量浮层是
// 从它**往上**弹的——只画播放条的那一支把它摆在窗口顶上，于是浮层有一半在
// 窗口外面。这一支是给存图用的。
func bottomBarTester(t *testing.T, st app.State, width, height int, dark bool) (*mygoui.Tester, *Shell) {
	t.Helper()
	var b *testShell
	b = newTestShell(t, at(width, height), systemIsDark(dark), withView(func(s *Shell) func(*mygoui.Context) {
		return func(c *mygoui.Context) {
			s.wake.set(c)
			s.applyFrameTheme(c)
			mygoui.Column(c).Fill().Children(func() {
				mygoui.Row(c).Grow(1)
				s.viewPlayBar(c, st)
			})
		}
	}))
	tt := b.tt
	tt.Frame()
	return tt, b.s
}

// hoverVolumeKey 把指针停在音量图标上，等那 300 毫秒过去（浮层是悬停**一会儿**
// 才出来的，上游那个 Tooltip 的 `delay={300}`）。
//
// ⚠️ **必须推钟**：无头 Tester 的钟站着不动（只有 `Advance` 能推它），而那
// 300 毫秒是框架的**动画**在走——不推钟它永远走不完，于是浮层永远不出来。
// 这一条是「用 `tt.Advance(d)` 配 `tt.FramePending()`」那个写法（CLAUDE.md §2）。
func hoverVolumeKey(t *testing.T, tt *mygoui.Tester) {
	t.Helper()
	icon, ok := volumeKeyOnTheBar(tt)
	if !ok {
		t.Fatalf("播放条上找不到音量那一枚，文字有 %q", tt.Texts())
	}
	tt.Move(icon.X+icon.W/2, icon.Y+icon.H/2)
	tt.Advance(volHoverDelay)
	tt.Frame()
}

// TestTheVolumeTrackIsFilledFromTheBottom 钉「轨道填的是**已经调上去的那一段**」。
//
// ⚠️ **这一条是被评审指出来的**：竖着的那一根原来是从**顶**往下填到旋钮的，
// 填成了**剩下的**那一段——于是 0% 显示满格、100% 显示空格。
//
// 判据：轨道**上半段**（离 0% 时旋钮最远的那一头）在 100% 时是主色、在 0% 时不是。
// 那一段两头都离旋钮远（旋钮 100% 时在顶上、0% 时在底下），所以影子和旋钮都
// 够不着它。
func TestTheVolumeTrackIsFilledFromTheBottom(t *testing.T) {
	for _, c := range []struct {
		vol    float64
		filled bool
	}{{0, false}, {1, true}} {
		t.Run(fmt.Sprint(c.vol), func(t *testing.T) {
			st := barState()
			st.Volume = c.vol
			tt, s := bottomBarTester(t, st, 1200, 320, false)
			hoverVolumeKey(t, tt)

			sl, ok := tt.Find("音量")
			if !ok {
				t.Fatalf("悬停之后浮层里没有那根音量滑块，文字有 %q", tt.Texts())
			}
			box := mygoui.Rect{
				X: sl.X + sl.W/2 - 2,
				Y: sl.Y + 30,
				W: 4, H: 20,
			}
			a := BuildTheme(s.app.SettingsSnapshot(), false).Accent
			accent := color.RGBA{R: a.R, G: a.G, B: a.B, A: a.A}
			n := nearPixels(tt.Image(), box, accent)
			switch {
			case c.filled && n == 0:
				t.Errorf("音量 100%%，轨道上半段 %v 却一个主色像素都没有——它没有填满", box)
			case !c.filled && n != 0:
				t.Errorf("音量 0%%，轨道上半段 %v 却有 %d 个主色像素——"+
					"它填反了（填的是**剩下**的那一段）", box, n)
			}
		})
	}
}

// TestTheVolumePopoverClosesOnItsOwn 钉「浮层会自己收掉」。
//
// ⚠️ **这一条是被评审指出来的**：原来那段只读 `s.clock.Now()`，**没有排任何到点
// 的唤醒**——于是「指针离开了」这件事**只在那一帧**被算一次，而不播放的时候
// **没有任何东西在要帧**：浮层于是永远留在画面上，遮住内容区。
//
// 判据是**画面上那个浮层没了**（那根滑块不在了），不是某个布尔。
func TestTheVolumePopoverClosesOnItsOwn(t *testing.T) {
	tt, s := bottomBarTester(t, barState(), 1200, 320, false)
	// 假时钟：这一条要等 400 毫秒，不真等（`clock.go` 那一层）。
	fc := newFakeClock(time.Unix(1_700_000_000, 0))
	s.clock = fc

	// 悬停 → 浮层出来。
	hoverVolumeKey(t, tt)
	if _, ok := tt.Find("音量"); !ok {
		t.Fatalf("悬停之后浮层里没有那根音量滑块，文字有 %q", tt.Texts())
	}

	// 指针移到空白处：既不在图标上，也不在浮层里。
	tt.Move(200, 60)
	// 🔴 **先让悬停那个动画走完**：走完之后**没有任何东西在要帧**了——
	// 「浮层自己收掉」真正要考的就是这一段（不播放的时候没有人要帧，
	// 原来那一版于是永远停在画面上）。
	tt.Advance(volHoverDelay + 50*time.Millisecond)
	tt.Frame()
	if _, ok := tt.Find("音量"); !ok {
		t.Fatalf("指针刚离开浮层它就没了，文字有 %q——它不该这么急", tt.Texts())
	}

	// 推**应用**那个钟：到点了就该**有一帧被要**（那一段排了到点的唤醒）。
	fc.Advance(volCloseGrace * 2)
	if !tt.FramePending() {
		t.Fatalf("过了宽限没有一帧被要（文字有 %q）——"+
			"那一段必须自己排一个到点的唤醒：不播放的时候没有任何东西在要帧，"+
			"而它只在**那一帧**算过一次「指针离开了」", tt.Texts())
	}
	tt.Frame()

	if _, ok := tt.Find("音量"); ok {
		t.Errorf("过了宽限浮层还开着（文字有 %q）——它得自己收掉", tt.Texts())
	}
}
