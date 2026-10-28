// 票 #298（板子任务 Biu_MyGo/177）的验收：队列抽屉的进出场。
//
// # 这一票在做什么
//
// **大面板（队列抽屉）加进出场**：从右侧滑入，**200ms** +
// `CubicBezier(0.16, 0.9, 0.21, 0.98)`，**不做回弹**。
// **菜单 / 小浮层 / 确认框 / Toast 不加**（高频操作，加了变噪音）。
//
// # 这里断的是四件事
//
//	① 滑入的方向和终点	面板从窗口右边滑进来，停在右沿往里 384 的地方
//	② 曲线和时长		滑到一半时面板已经走完 95%（那条曲线在 0.5 处的值）
//	③ 不做回弹		整条轨迹单调、不越过终点
//	④ 关的时候动完再拆	退场那 200ms 里面板还在树上，滑完了才拆
//
// # 判据为什么是「画面」而不是「代码」
//
// AGENTS.md §2：查布局问题先对着画面量。面板的**位置**在画面上是能量出来的
// （面板底色和页面底色是同一个颜色，分开它的是左边那道 1 像素的边线），
// 而「它是从哪边滑进来的」「它停在哪」都是画面的事。
//
// ⚠️ **别改成读代码里的常量**：那样断的是「代码写的是 200ms」，不是「它真的
// 滑了 200ms」。
package ui

import (
	"go/ast"
	"go/parser"
	"go/token"
	"image"
	"image/color"
	"math"
	"os"
	"runtime"
	"testing"
	"time"

	"github.com/ShellMonster/PudMusic/internal/app"
	mygoui "github.com/egoist/mygo/ui"
)

// drawerEdgeRow 是量抽屉左缘那一行（面板左边那道 1 像素边线所在的那一行）。
//
// ⚠️ **它在内容区里，不在顶栏也不在播放条上**：顶栏那一行有别的横线，
// 播放条那一行有播放条自己的影——而这一行除了面板的边线什么都没有。
const drawerEdgeRow = 120

// drawerEdgeX 是画面上抽屉面板左缘那一列的 x。
//
// ⚠️ **它是从画面上找出来的**：面板底色和页面底色**是同一个颜色**（上游的
// `bg-background`，见 queue_drawer.go 那段注释），所以画面上唯一能认出面板
// 左缘的就是那道 1 像素的边线。
//
// ⚠️ **那道边线的颜色是独一份的**：面板左边那圈影压在底色上是「更暗一点」
// （浅色下 ≥ 229，而边线是 219；深色下 ≤ 27，而边线是 59）——所以从右往左扫，
// 第一个撞上边线颜色的那一列就是面板的左缘，不会撞上影。
//
// ⚠️ **面板整个在窗口外面的时候扫不到**（返回 ok=false）——那正是「还没滑进来」。
func drawerEdgeX(img *image.RGBA, row int, border color.RGBA) (int, bool) {
	for x := img.Rect.Max.X - 1; x >= img.Rect.Min.X; x-- {
		if img.RGBAAt(x, row) == border {
			return x, true
		}
	}
	return 0, false
}

// drawerTester 造一个开着抽屉的外壳：队列里摆两首，抽屉开着。
//
// ⚠️ **它不推钟**：动画从**第一帧**开始（那一帧面板还在窗口外面），
// 推钟是各个测试自己按需要做的事。
func drawerTester(t *testing.T, dark bool) (*Shell, *mygoui.Tester) {
	t.Helper()
	b := newTestShell(t, systemIsDark(dark), at(shotWinW, shotWinH))
	s, tt := b.s, b.tt
	s.app.Queue().Reset(
		app.Item{Source: "local", Type: "audio", Title: "第一首"},
		app.Item{Source: "local", Type: "audio", Title: "第二首"},
	)
	s.drawerOpen = true
	return s, tt
}

// drawerBorder 是这一档主题里抽屉面板左边那道 1 像素边线的颜色。
func drawerBorder(s *Shell, dark bool) color.RGBA {
	return flatColor(BuildTheme(s.app.SettingsSnapshot(), dark).Border)
}

// TestTheQueueDrawerSlidesInFromTheRight 断①：抽屉是**从右边**滑进来的。
//
// 判据是画面上那道 1 像素的边线（面板的左缘）落在哪儿：
//
//	刚打开	窗口外面（扫不到那道边线——面板整个还在屏幕右边之外）
//	到位了	shotWinW - queueDrawerWidth（面板贴住窗口右沿）
func TestTheQueueDrawerSlidesInFromTheRight(t *testing.T) {
	for _, dark := range []bool{false, true} {
		name := "浅色"
		if dark {
			name = "深色"
		}
		t.Run(name, func(t *testing.T) {
			s, tt := drawerTester(t, dark)
			border := drawerBorder(s, dark)

			// 第一帧：动画从这一帧开始，面板还在窗口外面。
			tt.Frame()
			if x, ok := drawerEdgeX(tt.Image(), drawerEdgeRow, border); ok {
				t.Errorf("抽屉刚打开时面板左缘就在 x=%d——它应该整个在窗口外面，从右边滑进来", x)
			}

			// 推钟到动画走完。
			settleDrawer(t, s, tt)
			x, ok := drawerEdgeX(tt.Image(), drawerEdgeRow, border)
			if !ok {
				t.Fatalf("抽屉到位了，画面上却找不到面板左缘那道边线")
			}
			if want := shotWinW - queueDrawerWidth; x != want {
				t.Errorf("面板左缘在 x=%d，want %d（面板 %d 宽，贴住窗口右沿）",
					x, want, queueDrawerWidth)
			}
		})
	}
}

// TestTheQueueDrawerSlidesInOnThatCurveInThatTime 断②：曲线和时长。
//
// ⚠️ **判据是「半程时面板走完了多少」**：那条曲线
// `CubicBezier(0.16, 0.9, 0.21, 0.98)` 在 t=0.5 处的值是 **0.954460680**
// （自己算的，不是抄来的），所以滑到一半时面板应该已经走完 **95.4%**——
// 也就是离终点只剩 **17.5 个像素**。
//
// 这一条同时把两样东西钉住了：
//
//	曲线	框架默认的 EaseOut 在 0.5 处是 0.875（差 30 个像素），
//	      而「用弹簧」在 0.5 处根本没到——所以这条红了就说明用错了曲线
//	时长	滑了 100ms 还没到，说明它比 200ms 长得多（框架的弹簧兜底是 500ms）
//
// ⚠️ **推 100ms 正好落在半程**：框架的钟按 60Hz 走，一帧 16.666666ms，
// 第 6 帧落在 99.999996ms（差 4 纳秒）——所以量到的就是 t=0.5。
func TestTheQueueDrawerSlidesInOnThatCurveInThatTime(t *testing.T) {
	for _, dark := range []bool{false, true} {
		name := "浅色"
		if dark {
			name = "深色"
		}
		t.Run(name, func(t *testing.T) {
			s, tt := drawerTester(t, dark)
			border := drawerBorder(s, dark)

			tt.Frame()
			tt.Advance(100 * time.Millisecond)

			x, ok := drawerEdgeX(tt.Image(), drawerEdgeRow, border)
			if !ok {
				t.Fatalf("滑了 100ms，面板左缘那道边线还不在画面上——它没在滑")
			}
			// 期望位置：1200 - 384 × 0.954460680 = 833.487。
			//
			// ⚠️ **容差是 1.5 个像素**：面板的位置是浮点数，而画面上那道 1 像素的
			// 边线画在整数列上——量出来的 x 只能是 833 或 834。
			want := shotWinW - queueDrawerWidth*0.954460680
			if math.Abs(float64(x)-want) > 1.5 {
				t.Errorf("滑到一半时面板左缘在 x=%d，want %.2f（那条曲线在半程处是 95.4%%）。\n"+
					"差 %.1f 个像素：多半是曲线用错了（框架默认的 EaseOut 在半程处是 87.5%%）",
					x, want, math.Abs(float64(x)-want))
			}

			// 再推 150ms：总共 250ms > 200ms，它该到了。
			// ⚠️ **这一条断的是「不是弹簧」**：弹簧那一支的兜底是 500ms
			//（`mygo/ui/motion.go` 的 defaultSpringDuration），250ms 走不到。
			tt.Advance(150 * time.Millisecond)
			x, ok = drawerEdgeX(tt.Image(), drawerEdgeRow, border)
			if !ok || x != shotWinW-queueDrawerWidth {
				t.Errorf("滑了 250ms，面板左缘在 %d（画面上找得到：%v），want %d——"+
					"它该在 200ms 走完", x, ok, shotWinW-queueDrawerWidth)
			}
		})
	}
}

// TestTheQueueDrawerDoesNotBounce 断③：**不做回弹**。
//
// 回弹只留给「奖励动量」（甩动落位那种「给了一下奖励」的动作），
// **点击触发的过渡不弹**——所以面板只往终点靠，不许越过它再弹回来。
//
// 判据是整条轨迹：
//
//	单调	每一帧都更靠近终点（不许来回）
//	不越过	任何一帧都不许比终点更靠左（那是弹过去的）
func TestTheQueueDrawerDoesNotBounce(t *testing.T) {
	s, tt := drawerTester(t, false)
	border := drawerBorder(s, false)

	tt.Frame()
	last := shotWinW
	for i := range 13 {
		// 每一趟正好画一帧（帧间隔 16.666666ms < 17ms，而时间只会越推越靠后）。
		tt.Advance(17 * time.Millisecond)
		x, ok := drawerEdgeX(tt.Image(), drawerEdgeRow, border)
		if !ok {
			// 面板整个还在窗口外面：那是开头那几帧（它从右边滑进来）。
			continue
		}
		if x > last {
			t.Errorf("第 %d 帧面板左缘在 x=%d，比上一帧的 %d 更靠右——它在往回走", i, x, last)
		}
		final := shotWinW - queueDrawerWidth
		if x < final {
			t.Errorf("第 %d 帧面板左缘在 x=%d，越过了终点 %d——那是回弹", i, x, final)
		}
		last = x
	}
	if last != shotWinW-queueDrawerWidth {
		t.Errorf("滑完了，面板左缘在 x=%d，want %d", last, shotWinW-queueDrawerWidth)
	}
}

// TestTheQueueDrawerIsTornDownOnlyAfterItSlidOut 断④：**关的时候动完再拆**。
//
// 退场那 200ms 里面板**还在树上**（它滑到窗口外面去了，但还没拆）——
// 所以「关掉」不是「不建」，是「建着，滑出去，滑完了才拆」。
//
// 拆早了会怎样：面板在窗口里**啪地一下消失**，而用户刚点的「关」，
// 那个动作看起来像「闪了一下」。
func TestTheQueueDrawerIsTornDownOnlyAfterItSlidOut(t *testing.T) {
	s, tt := drawerTester(t, false)
	border := drawerBorder(s, false)

	tt.Frame()
	settleDrawer(t, s, tt)
	if s.drawerPhase != drawerShown {
		t.Fatalf("滑到位之后阶段是 %d，want %d", s.drawerPhase, drawerShown)
	}

	// 关掉。
	s.drawerOpen = false
	tt.Frame()
	if s.drawerPhase != drawerOut {
		t.Errorf("关掉之后阶段是 %d，want %d（它该开始往窗口外面滑）",
			s.drawerPhase, drawerOut)
	}
	// 还没滑完：面板还在树上（阶段不是 gone）。
	if s.drawerPhase == drawerGone {
		t.Fatal("抽屉刚关就没了——它该先滑出去，滑完了再拆")
	}

	// 推钟到滑完：这一帧面板还在画面上（它在窗口外面），下一帧才真的没了。
	tt.Advance(250 * time.Millisecond)
	tt.Frame()
	if s.drawerPhase != drawerGone {
		t.Errorf("滑完了阶段是 %d，want %d（面板该拆了）", s.drawerPhase, drawerGone)
	}
	if _, ok := drawerEdgeX(tt.Image(), drawerEdgeRow, border); ok {
		t.Error("抽屉拆了，画面上还有面板左缘那道边线")
	}
	// ⚠️ **拆掉的那一帧里面板还在树上**（阶段是在建它的过程里改的），所以
	// 「抽屉没了」要再画一帧才成立——而量画面的读的是上一帧。
	tt.Frame()
	if tt.HasText("第一首") {
		t.Error("抽屉拆了，画面上还画着它那一行")
	}
}

// TestTheSmallOverlaysHaveNoEnterOrExitAnimation 断「小浮层不加进出场」。
//
// 断的是**代码里有没有动画调用**：确认框、菜单、弹窗、浮层这一批，一个都不许
// 有进出场动画。
//
// ⚠️ **画面上断不了**：一个「立刻出现」和一个「淡入一帧」在截图上是同一张图
// （Tester 的钟站着不动，淡入那一帧停在起点上——而起点就是「全不透明」）。
// 所以这里读源码。
//
// ⚠️ **Toast 不在这个名单里，而且是有理由的**：它那 250ms 的入场是 #207 自己
// 定的（票 #180「Toast 语义色与入场」），而 #292 还要把它的曲线统一成
// `CubicBezier(0.16, 0.9, 0.21, 0.98)`。这一票按「不加 = **这一票**不加」
// 处理：**没动它**（下面那一条把它钉住，免得它被顺手改掉）。
//
// ⚠️ **顶栏那个联想面板也不在这个名单里，而且是刻意的**（票 #259，2026-10-25
// 拍板）：**它只在用户输入时出现**，而用户此刻正盯着它——它不属于「高频操作」
// 那一档，所以给它开了一个例外：进场 200ms（淡入 + 升上来 10 DIP）。
// ⚠️ **例外只开这一处**：`navbar_search.go` **不许**加进这个名单（那是把
// 例外变成规矩）；理由与范围见 DESIGN.md §五《联想面板的进场》那一节。
func TestTheSmallOverlaysHaveNoEnterOrExitAnimation(t *testing.T) {
	// 名单是「小浮层」那一类：菜单、确认框、弹窗、浮层面板。
	//
	// ⚠️ **「浮层面板（联想、旋钮面板、下拉）」那一行说的是 `popup.go`**——那是
	// **共用的那个外观出口**（`popupPanel`），不是联想面板本身。联想面板的动画
	// 在 `navbar_search.go`（它自己那一层），所以这里断不到它。
	files := []string{
		"confirmmodal.go",    // 确认框
		"popup.go",           // 浮层面板的**外观**出口（旋钮面板、下拉也走它）
		"menu.go",            // 应用菜单
		"covermodal.go",      // 封面大图
		"folderedit.go",      // 新建 / 改收藏夹
		"folderselect.go",    // 选收藏夹
		"folderpick.go",      // 选文件夹
		"daterangepicker.go", // 日期区间
		"loginprompt.go",     // 登录提示
		"partdrawer.go",      // 分集抽屉（满屏之外那个向上的浮层）
	}
	for _, name := range files {
		for _, call := range animationCallsInFile(t, name) {
			t.Errorf("%s 里有 %q——小浮层**不加进出场**（高频操作，加了变噪音）",
				name, call)
		}
	}
}

// animationCallsInFile 列出那个文件里所有**真的调了**动画的名字。
//
// 找法是**语法树**，不是文本搜：文本搜会撞上注释里提到的名字（这一票的注释里
// 就写着 `AnimateTo`），而语法树只看得见调用的那一下。
//
// ⚠️ **它只认框架那几个入口**（`AnimateTo` / `Animate` / `Spring` / `Keyframes` /
// `Timeline` / `AnimatePose`）。手写的那种（像 Toast 那样自己按时间算一个比例
// 再喂给 `Opacity`）**认不出来**——那一处是 #207 定的，本票没动它。
func animationCallsInFile(t *testing.T, name string) []string {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, name, nil, 0)
	if err != nil {
		t.Fatalf("解析 %s 失败，这一就没法查了：%v", name, err)
	}
	var found []string
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		switch sel.Sel.Name {
		case "AnimateTo", "AnimateWith", "AnimateColor", "Animate", "Spring",
			"Keyframes", "Timeline", "AnimatePose":
			found = append(found, sel.Sel.Name)
		}
		return true
	})
	return found
}

// TestTheToastKeepsItsOwnEntryAnimation 钉住 Toast 那一段入场**没被这一票动过**。
//
// ⚠️ **它不是「小浮层不许有动画」那一条的反例，是另一件事**：Toast 那 250ms
// 的入场（淡入 + 从右滑 16）是 #207 自己定的（票 #180「Toast 语义色与入场」），
// 而 #292 要把它的曲线统一成 `CubicBezier(0.16, 0.9, 0.21, 0.98)`。
// 这一票（#298）**没动它**——所以这里钉住它的时长，免得它被顺手改掉。
//
// ⚠️ **它是入场，不是「进出场」**：Toast 到点**直接收走**，没有出场动画。
func TestTheToastKeepsItsOwnEntryAnimation(t *testing.T) {
	if toastEnterTime != 250*time.Millisecond {
		t.Errorf("Toast 的入场时长是 %v，want 250ms（#207 定的，#292 要改的是曲线不是时长）",
			toastEnterTime)
	}
	// 出场：Toast 到点直接收走——没有「淡出」那一段。
	// 断法是**源码里没有出场的动画调用**（入场那几个名字都在，见上面那一条的名单
	// 之外单独列出来）。
	if calls := animationCallsInFile(t, "toast.go"); len(calls) > 0 {
		t.Errorf("toast.go 里有动画调用 %v——Toast 到点是直接收走的，没有出场动画", calls)
	}
}

// TestTheQueueDrawerSlidesOutOnThatCurveInThatTime 断②的另一半：**退场**那条曲线。
//
// ⚠️ **为什么单起一条**：进场那一侧逐帧量了曲线和半程位置，退场只断了「滑完了
// 才拆」——那样的话**退场变成瞬移**（目标值写错、时长改成 0）测试照样绿。
//
// 判据是**画面上那道 1 像素的边线**（面板的左缘）退到哪儿了：
//
//	刚关	还在右沿（x = 宽度 - 384）
//	滑了 100ms	退到 宽度 - 384 + 384×0.954460680（那条曲线在 0.5 处的值）
//	滑完	整个在窗口外面（x = 宽度）
//
// ⚠️ **它和进场那一侧是镜像的**：进场的半程在 x=833.5，退场的半程在 x=1182.5
// （两者关于窗口中点 x=1008 对称）。
func TestTheQueueDrawerSlidesOutOnThatCurveInThatTime(t *testing.T) {
	for _, dark := range []bool{false, true} {
		name := "浅色"
		if dark {
			name = "深色"
		}
		t.Run(name, func(t *testing.T) {
			s, tt := drawerTester(t, dark)
			border := drawerBorder(s, dark)

			// 先让它滑到位（开着的时候量不到退场）。
			tt.Frame()
			settleDrawer(t, s, tt)

			// 关掉：这一帧是退场的第 0 毫秒，面板还在右沿。
			s.drawerOpen = false
			tt.Frame()
			if s.drawerPhase != drawerOut {
				t.Fatalf("关掉之后阶段是 %d，want %d（它该开始往窗口外面滑）",
					s.drawerPhase, drawerOut)
			}
			x, ok := drawerEdgeX(tt.Image(), drawerEdgeRow, border)
			if !ok || x != shotWinW-queueDrawerWidth {
				t.Fatalf("刚关时面板左缘在 %d（画面上找得到：%v），want %d",
					x, ok, shotWinW-queueDrawerWidth)
			}

			// 滑了 100ms：退到那条曲线的半程处。
			tt.Advance(100 * time.Millisecond)
			x, ok = drawerEdgeX(tt.Image(), drawerEdgeRow, border)
			if !ok {
				t.Fatalf("滑了 100ms，面板左缘那道边线还不在画面上——它没在滑")
			}
			// 期望：1200 - 384 + 384 × 0.954460680 = 1182.487。
			//
			// ⚠️ **容差是 1.5 个像素**：面板的位置是浮点数，而画面上那道 1 像素的
			// 边线画在整数列上。
			want := shotWinW - queueDrawerWidth + queueDrawerWidth*0.954460680
			if math.Abs(float64(x)-want) > 1.5 {
				t.Errorf("退场滑到一半时面板左缘在 x=%d，want %.2f（那条曲线在半程处是 95.4%%）",
					x, want)
			}

			// 滑完：整个面板在窗口外面（左边那道边线也跟着出去了，画面上找不到）。
			tt.Advance(150 * time.Millisecond)
			if _, ok := drawerEdgeX(tt.Image(), drawerEdgeRow, border); ok {
				t.Error("退场滑完了，面板左缘那道边线还在画面上——它该整个滑出去了")
			}
		})
	}
}

// TestTheQueueDrawerDoesNotBlockClicksWhileItSlides 断「动画**不能挡交互**」。
//
// 抽屉滑进去的那 200ms 里面板还在窗口外面，而底下那片蒙版**照常吃点击**——
// 点它一下就是「关抽屉」。如果动画把点击挡了，那一下就什么都不发生，
// 而抽屉会继续滑进去：用户按了「关」，它反而开了。
//
// 顺带断「**重入**」：退场途中再点一次外面，**不许**把抽屉弹回来
// （关是幂等的——`drawerOpen = false`，不是 `drawerOpen = !drawerOpen`）。
func TestTheQueueDrawerDoesNotBlockClicksWhileItSlides(t *testing.T) {
	s, tt := drawerTester(t, false)

	tt.Frame()
	tt.Advance(100 * time.Millisecond)
	if s.drawerPhase != drawerIn {
		t.Fatalf("滑到一半时阶段是 %d，want %d（它该还在往里滑）",
			s.drawerPhase, drawerIn)
	}

	// 点底下那片蒙版（窗口左边）：那就是「关抽屉」。
	tt.ClickAt(10, 400)
	tt.Frame()
	if s.drawerOpen {
		t.Error("滑到一半时点外面，抽屉没有关——动画把点击挡了")
	}

	// 再点一次：不许把抽屉弹回来。
	tt.ClickAt(10, 400)
	tt.Frame()
	if s.drawerOpen {
		t.Error("退场途中又点了一下外面，抽屉被弹回来了——关应该是幂等的")
	}
}

// TestTheQueueDrawerSlideDoesNotAllocatePerFrame 断「进出场不许每帧新建对象」。
//
// **每帧分配是我们唯一的性能判据**（`internal/ui/framebudget_test.go`）：
// 分配次数是「这一帧建了多少个对象」，**不受机器快慢影响**。
//
// 断法是**同一块界面、同一个钟**的两段比：抽屉滑进去那 200ms 里的每一帧，
// 和它滑到位之后稳住的每一帧——两者的每帧分配次数**不许差出一截**。
// （面板在动，所以它每一帧都要重新排一遍版；那笔钱是「动布局位置」的固有代价，
// 但**不许**再多出「动画自己每帧新建对象」那一笔。）
//
// ⚠️ **头几帧不算**：抽屉**第一次**出现时那几个字要进字形图集，而字形图集一变，
// 框架就把**整棵树**重新排一遍（`mygo/ui/layoutcache.go` 的 `rt.gen`）。
// 那是「这个抽屉第一次出现」的一次性开销，不是进出场的开销——所以先开一次、
// 关掉，**再**开一次，量的是第二次。
func TestTheQueueDrawerSlideDoesNotAllocatePerFrame(t *testing.T) {
	s, tt := newShell(t, withView(func(s *Shell) func(*mygoui.Context) {
		return func(c *mygoui.Context) {
			// ⚠️ **主题要自己装**：只画这一块的那一支不走 `Shell.View`，
			// 而框架每帧开头会把主题重置成跟随系统那一份（铁律二）。
			s.applyFrameTheme(c)
			s.viewQueueDrawer(c)
		}
	}))
	s.app.Queue().Reset(
		app.Item{Source: "local", Type: "audio", Title: "第一首"},
		app.Item{Source: "local", Type: "audio", Title: "第二首"},
	)

	// 第一次开：那几个字进字形图集（一次性开销）。
	s.drawerOpen = true
	tt.Frame()
	settleDrawer(t, s, tt)

	// 稳态：抽屉开着、不动的时候每一帧分配多少。
	const steadyFrames = 20
	steadyAllocs, steadyBytes := measureAllocs(func() {
		for range steadyFrames {
			tt.Frame()
		}
	})
	steadyPerFrame := float64(steadyAllocs) / steadyFrames

	// 关掉，滑完（面板拆了），再开一次——这一次量的是**进场**。
	s.drawerOpen = false
	settleDrawer(t, s, tt)
	s.drawerOpen = true
	tt.Frame()

	// 12 帧 × 16.667ms = 200ms，正好是那一段动画。
	const slideFrames = 12
	slideAllocs, slideBytes := measureAllocs(func() {
		for range slideFrames {
			tt.Advance(17 * time.Millisecond)
		}
	})
	slidePerFrame := float64(slideAllocs) / slideFrames

	t.Logf("稳态每帧 %.1f 次分配 / %.0f 字节；进场那 200ms 每帧 %.1f 次 / %.0f 字节",
		steadyPerFrame, float64(steadyBytes)/steadyFrames,
		slidePerFrame, float64(slideBytes)/slideFrames)

	// ⚠️ **余量是 20%**：抽屉在动，所以它每一帧都要重新排一遍版，那笔钱是
	// 「动布局位置」的固有代价（DESIGN.md §五：「要动就动布局位置」）。
	// 而「动画自己每帧新建对象」那一笔**不许**出现——那会是一倍以上。
	if slidePerFrame > steadyPerFrame*1.2 {
		t.Errorf("抽屉滑进去那 200ms 里每帧分配 %.1f 次，稳态是 %.1f 次——"+
			"多出来的是「动画每帧新建对象」", slidePerFrame, steadyPerFrame)
	}
}

// measureAllocs 量那一段里一共分配了多少次、多少字节。
//
// ⚠️ **量之前先强制 GC**：不然量到的是上一轮留下的垃圾（那是上一帧的账）。
func measureAllocs(fn func()) (allocs, bytes uint64) {
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	fn()
	runtime.ReadMemStats(&after)
	return after.Mallocs - before.Mallocs, after.TotalAlloc - before.TotalAlloc
}

// TestTheQueueDrawerBackdropStopsEatingClicksWhileItSlidesOut 断退场时
// **底下那片蒙版不吃点击**。
//
// # 为什么单起一条
//
// 蒙版是**透明**的：用户看不见它，而它盖住整页。看不见的东西吃点击，
// 就是「点不动」——而它吃掉的那些点击只会把 `drawerOpen` 又设成 false。
//
// 改动之前关掉抽屉是**整个浮层都不建**，蒙版立刻消失，下一次点击直接落在页面上。
// 现在面板还要滑 200ms 才拆，所以蒙版得**手动**让开指针（`PassThrough`）。
//
// # 判据
//
// 同一个坐标（侧栏的「本地音乐」那一项）：
//
//	抽屉开着	点不动（蒙版吃掉了——Biu 的透明蒙版就是这样）
//	正在退场	点得动，路由真的变了
func TestTheQueueDrawerBackdropStopsEatingClicksWhileItSlidesOut(t *testing.T) {
	s, tt := drawerTester(t, false)

	tt.Frame()
	settleDrawer(t, s, tt)

	// 开着的时候：底下那一页点不动（蒙版吃掉了）。
	if err := tt.Click("本地音乐"); err != nil {
		t.Fatalf("点不了侧栏的「本地音乐」：%v（文字：%q）", err, tt.Texts())
	}
	tt.Frame()
	if got := s.router.Location(); got == "/local-music" {
		t.Fatal("抽屉开着的时候底下那一页点得动——蒙版没盖住")
	}

	// 关掉：这一帧是退场的第 0 毫秒，面板还在右沿，蒙版还在树上。
	s.drawerOpen = false
	tt.Frame()
	if s.drawerPhase != drawerOut {
		t.Fatalf("关掉之后阶段是 %d，want %d", s.drawerPhase, drawerOut)
	}

	// 退场中：底下那一页要点得动。
	if err := tt.Click("本地音乐"); err != nil {
		t.Fatalf("退场时点不了侧栏的「本地音乐」：%v（文字：%q）", err, tt.Texts())
	}
	tt.Frame()
	if got := s.router.Location(); got != "/local-music" {
		t.Errorf("退场时点侧栏的「本地音乐」，路由是 %q，want %q——"+
			"底下那片蒙版还在吃点击", got, "/local-music")
	}
}

// ---- 出图 ----

// TestRenderQueueDrawerSlideShots 出这一票的图：抽屉**滑到一半**的那一张，
// 深浅各一套。
//
// ⚠️ **BIU_SHOT_DIR 不设就不存图**（那一族测试都是这样）。
//
// ⚠️ **存的是「半程」那一张**：全到位的那张和改动前一模一样（那证明不了
// 「它在滑」），而半程那一张能看出面板还在窗口右沿里面一点点。
func TestRenderQueueDrawerSlideShots(t *testing.T) {
	dir := os.Getenv("BIU_SHOT_DIR")
	if dir == "" {
		t.Skip("设 BIU_SHOT_DIR 才会存图")
	}
	for _, dark := range []bool{false, true} {
		name := "浅色"
		if dark {
			name = "深色"
		}
		t.Run(name, func(t *testing.T) {
			s, tt := drawerTester(t, dark)
			tt.Frame()
			tt.Advance(100 * time.Millisecond)
			save(t, dir, "91-"+name+"-队列抽屉-滑到一半.png", tt)
			_ = s
		})
	}
}
