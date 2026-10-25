package hover

import (
	"reflect"
	"runtime"
	"testing"

	"github.com/egoist/mygo/ui"
)

// 这一票（issue #314）钉住波形那一格跨帧要用的那一份状态**不是每帧重建的**。
//
// # 原来每帧付什么
//
// 波形每一帧都要问一次「此刻的相位是多少」，而相位要跨帧记住两样东西（起点和
// 上一次要帧的时刻）。原来那两样是**两个 `ui.Local`**，键是
// `歌名+":start"` 和 `歌名+":seen"`——于是每一帧都要：
//
//   - 拼两次字符串（两次分配）；
//   - 把两个字符串**装箱进 `any`**（`ui.Local` 的键是 `any`，而
//     `string → any` 的装箱每次都要在堆上分配一次）。
//
// 一共四次分配，而这一格是**每一帧**都要问一次的。
//
// # 现在付什么
//
// **零次**：键换成一个**零大小的结构体**（装箱走 `runtime.zerobase`，一次都
// 不分配），两样东西合成一个结构体（一次查表而不是两次），而那个 `init`
// 闭包**不捕获任何东西**（`ui.Local` 的 `init` 参数只被调用一次，编译器
// 能把它内联掉——**捕获了变量本身不是分配的原因**，见 `wavePhase` 那段）。
//
// ⚠️ 「起始时刻」没有靠 init 闭包带进来：它靠「零点还没定过」这一句定下来
// （见 hover.go 的 wavePhase）。

// TestTheWaveStateKeyDoesNotAllocate 钉住「波形的本地存储键不分配」。
//
// 判据是**装箱进 `any` 时不分配**——而决定这一点的**不是「零不零大小」**，
// 是**这个键是不是编译期常量**（实测）：
//
//   - 零大小的结构体 → 装箱走 `runtime.zerobase`，0 次分配；
//   - 非零大小、但**编译期常量**（比如 `big40{1,2,3,4,5}`）→ 0 次分配
//     （编译器给常量分配一份静态副本，装箱指向它）；
//   - 非零大小、**运行期才算出来**的值 → 每帧 1 次分配。
//
// 所以「非零大小就一定分配」是错的（票面原先那么写），而「零大小一定不分配」
// 是对的——**零大小是本键实际用的那一档**，这里量它是为了守住「别把它换成一个
// 运行期才算出来的键」。
//
// ⚠️ **判据为什么用「零大小」而不是「编译期常量」**：后者在 Go 里没法直接测
// （没有反射能说「这个值是不是编译期常量」），而零大小是它的**充分条件**——
// 零大小必然不分配。所以这一条测的是「零大小」，而上面那段实测说明的是
// 「零大小为什么够」。
func TestTheWaveStateKeyDoesNotAllocate(t *testing.T) {
	if got := reflect.TypeOf(waveStateKey{}).Size(); got != 0 {
		t.Errorf("波形的本地存储键是 %d 字节，want 0——\n"+
			"零大小的键装箱进 `any`（`ui.Local` 的键是 `any`）走 `runtime.zerobase`，"+
			"每帧 0 次分配；换成非零大小的键就要看它是不是编译期常量"+
			"（编译期常量仍然是 0 次，运行期才算出来的值才是每帧 1 次）", got)
	}
}

// TestTheWaveDoesNotRebuildItsStateEveryFrame 钉住「波形那一帧的分配数」。
//
// ## 判据是**这一格的每帧分配次数**，不是「全部界面的」
//
// 那一格里每帧新建的东西只有三样：
//
//   - **那个标签**（`PlayingLabel`）：`歌名 + " " + "正在播放"`，一次分配。
//     它是给读屏软件和 Tester 认这一行用的（见 hover.go 的注释），不能省。
//   - **框架画那四根竖条**时的两笔（实测 2 次）。
//   - **本地存储的查表**：改完之后是 **0 次**。
//
// 所以基线是 **3 次**。上限取 **5**：给框架那两笔留一点余量，而原来那种写法
// （两次字符串拼接 ＋ 两次装箱）是 **7 次**——超了。
func TestTheWaveDoesNotRebuildItsStateEveryFrame(t *testing.T) {
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Box(c).Size(60, 30).Padding(8).Children(func() {
			Wave(c, "wave", 40, ui.RGB(0, 0, 255), "甲")
		})
	}, testerW, testerH)
	tt.Frame()

	const n = 100
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	for range n {
		tt.Frame()
	}
	runtime.ReadMemStats(&after)

	got := float64(after.Mallocs-before.Mallocs) / float64(n)
	const limit = 5
	if got > limit {
		t.Errorf("波形那一帧分配了 %.1f 次（上限 %d 次）。\n"+
			"这一格每帧都要问一次相位，而相位要跨帧记住两样东西——"+
			"那一份状态**不是每帧重建的**：\n"+
			"  · 本地存储的键要是零大小的（装箱进 `any` 不分配）；\n"+
			"  · 两样东西合成一个结构体（一次查表而不是两次）；\n"+
			"  · init 闭包不捕获任何东西（**捕获了变量本身不是分配的原因**——\n"+
			"    决定分配的是闭包**逃不逃逸**，而 `ui.Local` 的 `init` 参数\n"+
			"    只被调用一次、不逃逸，于是编译器能把它内联掉）。\n"+
			"（改之前是 7 次：两次字符串拼接 ＋ 两次装箱 ＋ 那个标签 ＋ 画竖条。）",
			got, limit)
	}
	t.Logf("波形那一帧：每帧 %.2f 次分配", got)
}

// TestTheCoverIdentitiesAreNotRecomputedEveryFrame 钉住「那一圈反馈的**身份**
// 不是每帧重算的」（票 #407）。
//
// ## 它挡的是什么
//
// 那一圈反馈（压暗 / 三角 / 渐变条 / 播放钮）每一帧每一行都要建，而它建出来的
// 那几样东西**带着身份**：节点键（`Key`）和动画键（`AnimateWith`）——淡入要
// 跨帧，节点的身份不变才有那份状态（见 hover.go 的文件头）。
//
// 现算的话，每一帧每一行要：
//
//   - 拼四个字符串（标签、`:dim`、`:fade`、`:tri`）：**四次分配**；
//   - 把其中三个**装箱进 `any`**（`Key` 和 `AnimateWith` 的参数是 `any`，而
//     `string → any` 的装箱每次都要在堆上分配一次）：**又三次**。
//
// 一屏十三行就是**九十一次**——实测那是首页那一屏每帧 137 次分配里的 91 次
// （见 `internal/ui/framebudget_test.go` 里「首页列表」那一档）。
//
// ## 判据是**零分配**，不是「少一点」
//
// 那几个身份**只跟着歌名变**：歌名不变它们就不变，于是算一次、记下来，之后
// 每帧拿现成的（**零分配**）。
//
// ⚠️ **记下来的是「已经装箱好的那一份」**：只把字符串记下来、每帧再装箱一次，
// 等于只省了七次里的四次——装箱那三次还在。
func TestTheCoverIdentitiesAreNotRecomputedEveryFrame(t *testing.T) {
	// 第一遍要算（要分配），所以先算一遍再量。
	const title = "甲乙丙丁"
	idsOf(title, idsRow)
	idsOf(title, idsCard)
	idsOf(title, idsCardOne)

	for _, c := range []struct {
		name string
		kind idsKind
	}{
		{"列表行", idsRow},
		{"网格卡片", idsCard},
		{"网格卡片（只放这一首）", idsCardOne},
	} {
		t.Run(c.name, func(t *testing.T) {
			got := testing.AllocsPerRun(100, func() {
				_ = idsOf(title, c.kind)
			})
			if got != 0 {
				t.Errorf("那一圈反馈的身份每帧重算了一遍（%.0f 次分配）。\n"+
					"那套身份**只跟着歌名变**，算一次记下来就够：\n"+
					"  · 查表不分配（键是结构体，不装箱）；\n"+
					"  · 记下来的是**已经装箱进 `any` 的那一份**（`Key` 和\n"+
					"    `AnimateWith` 的参数都是 `any`，而 `string → any` 的装箱\n"+
					"    每次都要在堆上分配一次）。\n"+
					"（现算的话是每帧每行七次：一屏十三行就是九十一次。）",
					got)
			}
		})
	}
}

// TestTheCoverIdentitiesAreDistinctPerKind 钉住「那几种反馈的身份互不重」。
//
// ⚠️ **同一个盒子里的节点键不能重**（框架按「父 + 键」认节点，重了就是两个
// 元素抢一个身份）。所以「从这一首往后听」和「只放这一首」那两种卡片反馈
// 必须各有一套自己的键。
func TestTheCoverIdentitiesAreDistinctPerKind(t *testing.T) {
	const title = "甲乙丙丁"
	row := idsOf(title, idsRow)
	card := idsOf(title, idsCard)
	one := idsOf(title, idsCardOne)

	if card.label == one.label {
		t.Errorf("两种卡片反馈的无障碍名字一样（%q）——"+
			"「从这一首往后听」和「只放这一首」读出来的话得不一样", card.label)
	}
	if card.bar == one.bar || card.btn == one.btn {
		t.Errorf("两种卡片反馈的节点键重了（%v / %v）——"+
			"同一个盒子里的两个节点抢一个身份，框架会当成重复键", card.bar, one.bar)
	}
	if row.dim == row.tri || row.dim == row.fade || row.tri == row.fade {
		t.Errorf("列表行那一圈的身份重了（%v / %v / %v）——"+
			"压暗、三角和淡入动画各自要一个", row.dim, row.tri, row.fade)
	}
	// ⚠️ **同一个节点上的两个动画键不能重**：那一枚三角上叠着两档缩放
	//（「弹出来」和「指针停在上面时长大」），键重了就是它们共用一份动画状态，
	// 于是「弹出来」的时候它就长到 110% 了。
	if row.pop == row.grow {
		t.Errorf("列表行那枚三角上的两档缩放用了同一个键（%v）——"+
			"「弹出来」和「停在上面长大」得各走各的动画", row.pop)
	}
}

// TestTheCoverIdentitiesAreTheOnesTheOldCodeBuilt 钉住「身份的值**本身**」
// （票 #407）。
//
// ## 为什么「零分配」那条不够
//
// 「算一次就记下来」这个改法只改「**算几次**」，**不改「算什么**」——键和值
// 都和改之前逐位相同，所以节点的 id（`keyedID` 对字符串走 `maphash.String`）
// 和改之前一样：淡入的状态、跨帧的身份一样都不差。
//
// 而这件事**光靠「零分配」那条测不出来**：把 `packIDs` 的后缀改掉
// （`:dim` 改成 `:dim2`），零分配那条照样绿——而**屏幕阅读器读到的那个标签**
// 和节点的跨帧身份就悄悄变了（框架按「父 + 键」认节点，键变了就是另一个
// 节点，淡入的动画状态跟着重来）。
//
// 所以这一条把**改之前的算法原样再算一遍**，逐项比：行那一圈三样、卡片那一
// 圈三样、两种卡片的无障碍名字、以及空歌名和重复歌名。
func TestTheCoverIdentitiesAreTheOnesTheOldCodeBuilt(t *testing.T) {
	// 改之前的算法（原样照抄，`RowCover` / `cardOverlay` 那一版）：
	// 标签是「歌名 + 空格 + 那句话」，节点键和动画键是「标签 + 后缀」。
	oldRow := func(title string) (label, dim, fade, tri string) {
		label = title + " " + PlayFrom
		return label, label + ":dim", label + ":fade", label + ":tri"
	}
	oldCard := func(label string) (bar, fade, btn string) {
		return label + ":bar", label + ":fade", label + ":btn"
	}
	// ⚠️ **后面加的那几样（缩放那一族）也写死在这里**：它们的键一样是
	// 「标签 + 后缀」，而键一变就是另一个节点——动画的状态跟着重来（放大变成
	// 「啪」地一下出现）。
	oldRowZoom := func(label string) (pop, grow string) {
		return label + ":pop", label + ":grow"
	}
	oldCardZoom := func(label string) (zoom, pop string) {
		return label + ":zoom", label + ":pop"
	}

	// ⚠️ **歌名取样含空串和重复歌名**：空串是「这一首歌没有名字」（标签就只剩
	// 那半句），重复歌名是两张列表里同名的两首（它们要拿到**同一套**身份，
	// 而那是对的——键一样，节点身份也一样）。
	titles := []string{"", "甲", "晴天", "晴天 甲", "甲", "【周杰伦】晴天"}

	for _, title := range titles {
		wantLabel, wantDim, wantFade, wantTri := oldRow(title)

		row := idsOf(title, idsRow)
		assertSame(t, "列表行的标签", row.label, wantLabel)
		assertSame(t, "列表行压暗那一层的键", row.dim, wantDim)
		assertSame(t, "列表行淡入动画的键", row.fade, wantFade)
		assertSame(t, "列表行三角那一层的键", row.tri, wantTri)
		wantPop, wantGrow := oldRowZoom(wantLabel)
		assertSame(t, "列表行「弹出来」那一层的键", row.pop, wantPop)
		assertSame(t, "列表行「停在上面长大」那一层的键", row.grow, wantGrow)

		card := idsOf(title, idsCard)
		assertSame(t, "卡片的无障碍名字", card.label, wantLabel)
		wantBar, wantCardFade, wantBtn := oldCard(wantLabel)
		assertSame(t, "卡片渐变条的键", card.bar, wantBar)
		assertSame(t, "卡片淡入动画的键", card.fade, wantCardFade)
		assertSame(t, "卡片播放钮的键", card.btn, wantBtn)
		wantZoom, wantCardPop := oldCardZoom(wantLabel)
		assertSame(t, "卡片封面放大那一层的键", card.zoom, wantZoom)
		assertSame(t, "卡片「弹出来」那一层的键", card.pop, wantCardPop)

		one := idsOf(title, idsCardOne)
		wantOneLabel := title + " " + PlayNow
		assertSame(t, "「只放这一首」那张卡的无障碍名字", one.label, wantOneLabel)
		wantBar, wantCardFade, wantBtn = oldCard(wantOneLabel)
		assertSame(t, "「只放这一首」那张卡的渐变条的键", one.bar, wantBar)
		assertSame(t, "「只放这一首」那张卡的淡入动画的键", one.fade, wantCardFade)
		assertSame(t, "「只放这一首」那张卡的播放钮的键", one.btn, wantBtn)
		wantZoom, wantCardPop = oldCardZoom(wantOneLabel)
		assertSame(t, "「只放这一首」那张卡封面放大那一层的键", one.zoom, wantZoom)
		assertSame(t, "「只放这一首」那张卡「弹出来」那一层的键", one.pop, wantCardPop)
	}
}

// assertSame 比一个身份和期望值（身份是 `any`，期望是字符串）。
func assertSame(t *testing.T, what string, got any, want string) {
	t.Helper()
	s, ok := got.(string)
	if !ok {
		t.Errorf("%s 是 %T（want 字符串）——身份必须是装好字符串的那一份 `any`", what, got)
		return
	}
	if s != want {
		t.Errorf("%s 是 %q，want %q——\n"+
			"「算一次就记下来」只改「算几次」，**不改「算什么」**：身份的值和改之前\n"+
			"逐位相同，节点的 id（`keyedID` 对字符串走 `maphash.String`）才一样——\n"+
			"而淡入的动画状态、屏幕阅读器读到的标签，都跟着它。",
			what, s, want)
	}
}
