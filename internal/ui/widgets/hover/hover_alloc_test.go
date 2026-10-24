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
