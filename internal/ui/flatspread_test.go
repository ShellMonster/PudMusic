package ui

// 「铺开扁平按钮」那一票（#134 的第七单元）的**画面**验收。
//
// 六处（满屏播放器 / 迷你播放器 / 下载记录 / 本地音乐 / 收藏夹 / 动态卡片）里的
// 动作键不再是 `ui.Button` 那枚「一圈 1px 描边 + 一块常驻底色」的药丸。这件事在
// 代码里看不出来——换成 `ui.Button` 一样编译得过、点得动、名字也还叫得出来，只有
// 画面上多了那两样。所以下面全部读像素，深浅两套各跑一遍（深色下那圈
// `t.Border` 压在 `#18181b` 上是一道亮线，比浅色更显眼）。
//
// 两条判据，按有没有字分开：
//
//   - **图标键**：`assertFlat`（`flatbutton_test.go`）——框内那一块和框外那一块
//     必须同一个颜色，框线那一行也不许有变化。`Find` 给回来的就是按钮的框
//     （`Label` 挂在按钮上）。
//   - **文字键**：`Find` 给回来的是**那串字自己的框**（配方里那条「不许补
//     Label」的规矩），所以量的是字上下各 3 像素——`ui.Button` 的上下内边距是 6，
//     那一条落在它的常驻底里；扁平配方那里就是页面底色。
//
// ⚠️ **反面也要断**：对话框里的确定/取消仍然是真按钮（`TestTheDialogButtonsStay
// Outlined`）。把那一批也拍平的话，「哪一个是主要动作」就没有了——而那是票面
// 点名要保的东西。
//
// ⚠️ 量之前**把指针挪开**：`flat` 的悬停反馈是一层压在前景色上的淡底，指针停在
// 按钮上时框内框外本来就不同色，这一组量的就不是「有没有常驻底」了。

import (
	"image"
	"image/color"
	"os"
	"testing"

	"github.com/ShellMonster/Biu_Go/internal/service/download"
	mygoui "github.com/egoist/mygo/ui"
)

// assertFlatText 断一枚**文字**扁平键：字上下那几像素就是页面底色。
//
// 为什么不能直接对 `Find` 给的框用 `assertFlat`（理由见文件头）：文字键上没有
// `Label`，`Find` 给回来的是字自己的框，两种实现下它一模一样。
//
// 差别的窗口是字上下各 3 像素：`ui.Button` 的上下内边距是 6（`styleButton` 的
// `t.Space(1.5)`），那 3 像素落在它的常驻底里；再往外两种实现都出了按钮，量不出
// 区别。所以 3 这个数不是随手挑的——它必须小于 6，也必须大于 0。
func assertFlatText(t *testing.T, img *image.RGBA, r mygoui.Rect, bg color.RGBA, what string) {
	t.Helper()
	x := round32(r.X + r.W/2)
	for _, y := range []int{round32(r.Y) - 3, round32(r.Y+r.H) + 3} {
		got := img.RGBAAt(clampX(img, x), clampY(img, y))
		if d := maxDiff(got, bg); d > probeTolerance {
			t.Errorf("%s 那枚文字键上下还有底：%v，页面底色 %v，差 %d（> %d）——"+
				"它还裹在 `ui.Button` 里", what, got, bg, d, probeTolerance)
		}
	}
}

// assertTextKeyHasAShell 断一枚**文字按钮仍然有壳**（常驻底 + 那圈描边）——
// 和上面那一组反着来。
//
// 文字按钮的 `Find` 给回来的是字自己的框（没有 `Label`），所以判据换个方向：
// **字正上方那几像素里有东西不是背景色的**。`ui.Button` 的上下内边距是 6
// （`styleButton` 的 `t.Space(1.5)`），从字顶往上一行到六行是它的常驻底，最外
// 一行是那圈描边——实测弹窗里那颗「取消」：字顶 y=421，描边在 y=414（217,217,222），
// 底是 415~420（244,244,245）。扁平配方那里从字顶一直空到外面。
//
// `bg` 取字顶上方 12 像素那一格（比按钮的 6 像素内边距更外面）。
func assertTextKeyHasAShell(t *testing.T, img *image.RGBA, r mygoui.Rect, what string) {
	t.Helper()
	x := round32(r.X + r.W/2)
	top := round32(r.Y)
	bg := img.RGBAAt(clampX(img, x), clampY(img, top-12))
	for y := top - 7; y <= top-1; y++ {
		if got := img.RGBAAt(clampX(img, x), clampY(img, y)); maxDiff(got, bg) > probeTolerance {
			return
		}
	}
	t.Errorf("%s 字上面那几行和背景是同一个颜色（%v）——它被拍平了：那是主次分明"+
		"的地方，不能去掉那层底和那圈描边", what, bg)
}

// flatPageBG 是这一帧页面的底色（内容区的根用的就是它）。
//
// ⚠️ 从**设置 + 深浅**现算，不从画面某一个像素上取：取像素就要挑一个「那儿一定
// 是底色」的点，而那种点在每一页都不一样——挑错一个，这一组量到的是那个点上的
// 别的东西。（`pagerhythm_test.go` 那个同名的 `pageBackground` 是另一种量法：它在
// 一整块区域里取众数，用在「不知道底色是哪一档」的地方。）
func flatPageBG(s *Shell, dark bool) color.RGBA {
	return flatColor(BuildTheme(s.app.SettingsSnapshot(), dark).Background)
}

// flatColor 把框架的 `ui.Color` 换成 `color.RGBA`（`ui.Color` 不实现
// `color.Color`，转换得逐字段写）。
func flatColor(c mygoui.Color) color.RGBA {
	return color.RGBA{R: c.R, G: c.G, B: c.B, A: c.A}
}

// noPointer 把指针挪到一片空地上，再画一帧。
//
// 悬停那一层是 `flat` 的反馈（压在按钮上的淡底），指针停在被量的按钮上时框内
// 框外本来就不一样——那样量出来的红与「有没有常驻底」无关。
//
// 那一点是播放条上那条空地（各页都一样）；迷你窗口没有播放条，它自己那一档见
// `noPointerMini`。
func noPointer(tt *mygoui.Tester) {
	tt.Move(600, 795)
	tt.Frame()
}

// noPointerMini 是 `noPointer` 的 320×100 那一档：左上角封面那一块，离那四个
// 键最远。
func noPointerMini(tt *mygoui.Tester) {
	tt.Move(5, 5)
	tt.Frame()
}

// assertKeysFlat 把一批**图标键**逐个断「框内框外同色」。
func assertKeysFlat(t *testing.T, tt *mygoui.Tester, what string, labels ...string) {
	t.Helper()
	img := tt.Image()
	for _, label := range labels {
		r, ok := tt.Find(label)
		if !ok {
			t.Fatalf("%s上找不到 %q，文字有 %q", what, label, tt.Texts())
		}
		assertFlat(t, img, r, what+" "+label)
	}
}

// assertNoRing 断一枚图标键**边上没有那圈描边**，背景色由调用方给。
//
// 迷你播放器那一档要用它，不用 `assertFlat`，有两个原因：
//
//   - 那一格窗口的底色就是 `t.Surface`，而 `ui.Button` 的常驻底也是 `t.Surface`
//     ——「框内框外同色」在那里恒成立，量不出常驻底（这也是**看得见的**那部分：
//     同色的一层底本来就看不见）。能看见的是那圈 `t.Border`。
//   - 那一列只有 88 高，四行挤在一起：进度条的滑钮就压在上一首那个键上头，
//     `probeEdge` 的参照行会取到滑钮上。
//
// 所以这里不找参照行，直接和**已知的底色**比：每条边的四分之一、四分之三处各取
// 一个最外圈的像素。扁平配方那里是底色，`ui.Button` 那里是描边。取样点避开边中点
// 是给播放键留的：28 的实心圆画在 32 的框里，圆的上下左右正好顶到边。
func assertNoRing(t *testing.T, img *image.RGBA, r mygoui.Rect, bg color.RGBA, what string) {
	t.Helper()
	// 容差比别处宽：进度条的滑钮带一层软影子，而那颗滑钮就在这一行的正上方
	// ——上边那几个取样点压在影子拖出来的渐变上（实测差 5，离描边的 27 差得远）。
	const tol = 12
	x0, y0 := round32(r.X), round32(r.Y)
	x1, y1 := round32(r.X+r.W)-1, round32(r.Y+r.H)-1
	xs := []int{x0 + round32(r.W*0.25), x0 + round32(r.W*0.75)}
	ys := []int{y0 + round32(r.H*0.25), y0 + round32(r.H*0.75)}
	var pts [][2]int
	for _, x := range xs {
		pts = append(pts, [2]int{x, y0}, [2]int{x, y1})
	}
	for _, y := range ys {
		pts = append(pts, [2]int{x0, y}, [2]int{x1, y})
	}
	for _, p := range pts {
		got := img.RGBAAt(clampX(img, p[0]), clampY(img, p[1]))
		if d := maxDiff(got, bg); d > tol {
			t.Errorf("%s 那一圈还有描边：(%d,%d) 是 %v，底色 %v，差 %d（> %d）",
				what, p[0], p[1], got, bg, d, tol)
		}
	}
}

// assertTextKeysFlat 把一批**文字键**逐个断「上下那几像素是页面底色」。
func assertTextKeysFlat(t *testing.T, tt *mygoui.Tester, bg color.RGBA, what string, labels ...string) {
	t.Helper()
	img := tt.Image()
	for _, label := range labels {
		r, ok := tt.Find(label)
		if !ok {
			t.Fatalf("%s上找不到 %q，文字有 %q", what, label, tt.Texts())
		}
		assertFlatText(t, img, r, bg, what+" "+label)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 一、满屏播放器
// ─────────────────────────────────────────────────────────────────────────────

// TestTheFullPlayerKeysAreFlat 满屏顶栏那两枚和分集那两枚。
//
// 上游是 28 / 22 / 24 / 16 的图标键（`full-screen-player/index.tsx:294,313,406`、
// `page-list.tsx:47`）。字色是白的（`fullOnColor`）——按钮不再自己刷 `t.Text`，
// 图标从祖先继承，所以这里顺带钉住「图形画出来了」（白色的图标压在压暗过的封面
// 上，`scanGlyph` 认得出它就说明它没被底吃掉）。
func TestTheFullPlayerKeysAreFlat(t *testing.T) {
	for _, dark := range []bool{false, true} {
		name := "浅色"
		if dark {
			name = "深色"
		}
		t.Run(name, func(t *testing.T) {
			_, tt, _ := fullTester(t)
			tt.SetDark(dark)
			openFull(t, tt)
			noPointer(tt)

			assertKeysFlat(t, tt, "满屏播放器", "关闭全屏", "全屏设置")
			// 「框里没描边、没底色」还不够：**图形本身要被画出来**。这两枚是白的
			// （`fullOnColor`），压在一个压暗过的封面上——底要是把它吃掉，框内
			// 框外照样同色（那是一枚透明的空键），所以得单独断那枚图形。
			assertGlyphPainted(t, tt, "关闭全屏")
			assertGlyphPainted(t, tt, "全屏设置")
		})
	}
}

// TestTheFullPlayerPartsKeysAreFlat 分集把手和「收起分集」。
//
// 单独一条：那两枚要真的有多 P 的稿子才会摆出来（`multiPages`），而
// `fullTester` 用的是本地文件（只有一「集」）。
func TestTheFullPlayerPartsKeysAreFlat(t *testing.T) {
	for _, dark := range []bool{false, true} {
		name := "浅色"
		if dark {
			name = "深色"
		}
		t.Run(name, func(t *testing.T) {
			s, tt := onlineTester(t, multiPages(), false)
			tt.SetDark(dark)
			s.openFullPlayer()
			waitFrames(t, tt, "分集问回来了", func() bool { return len(s.app.Parts()) == 3 })
			noPointer(tt)

			assertKeysFlat(t, tt, "满屏播放器", "显示分集列表")
			assertGlyphPainted(t, tt, "显示分集列表")

			if err := tt.Click("显示分集列表"); err != nil {
				t.Fatalf("点不了分集把手：%v（文字：%q）", err, tt.Texts())
			}
			tt.Frame()
			noPointer(tt)
			assertKeysFlat(t, tt, "分集面板", "收起分集")
			assertGlyphPainted(t, tt, "收起分集")
		})
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 二、迷你播放器
// ─────────────────────────────────────────────────────────────────────────────

// TestTheMiniPlayerKeysAreFlat 迷你窗口上那四个图标键。
//
// ⚠️ 音量**不在这里**：它是用户定下来要留的（上游迷你播放器没有），这一票只把
// 按钮换成图标，一个字都没动它。
func TestTheMiniPlayerKeysAreFlat(t *testing.T) {
	for _, dark := range []bool{false, true} {
		name := "浅色"
		if dark {
			name = "深色"
		}
		t.Run(name, func(t *testing.T) {
			s, a := newMiniShell(t, nil)
			if err := enqueueTwoWithCover(a, t); err != nil {
				t.Fatal(err)
			}
			a.Toggle() // 暂停：播放键上写的是「播放」
			tt := miniTester(s)
			tt.SetDark(dark)
			tt.Frame()
			noPointerMini(tt)

			bg := flatColor(BuildTheme(s.app.SettingsSnapshot(), dark).Surface)
			img := tt.Image()
			for _, label := range []string{"上一首", "播放", "下一首", "退出"} {
				r, ok := tt.Find(label)
				if !ok {
					t.Fatalf("迷你播放器上找不到 %q，文字有 %q", label, tt.Texts())
				}
				assertNoRing(t, img, r, bg, "迷你播放器 "+label)
			}
		})
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 三、下载记录
// ─────────────────────────────────────────────────────────────────────────────

// TestTheDownloadListKeysAreFlat 页头那两枚和行里那几枚操作。
func TestTheDownloadListKeysAreFlat(t *testing.T) {
	for _, dark := range []bool{false, true} {
		name := "浅色"
		if dark {
			name = "深色"
		}
		t.Run(name, func(t *testing.T) {
			// 一行下完的（「打开」+「删除」）和一行出错的（「重试」+「删除」）：
			// 五种操作里亮得最多的那两档都量到。
			//
			// ⚠️ **走 `newDownloads`（真引擎）而不是 `newRows`（快照）**：
			// 页头那颗「打开文件夹」只在真有下载目录的时候才摆，而快照那一套
			// 外壳没挂下载（`DownloadDir()` 是空的），量不到它。
			s, tt := newDownloads(t,
				newTask("歌 甲", download.StatusDone, dlChunk, 100),
				newTask("歌 乙", download.StatusError, dlProgress, 50),
			)
			tt.SetDark(dark)
			tt.Frame()
			noPointer(tt)

			assertKeysFlat(t, tt, "下载记录", "清空记录", "打开", "重试", "删除")
			assertTextKeysFlat(t, tt, flatPageBG(s, dark), "下载记录", "打开文件夹")
		})
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 四、本地音乐
// ─────────────────────────────────────────────────────────────────────────────

// TestTheLocalMusicKeysAreFlat 页头、操作行、每一行末尾那几枚。
func TestTheLocalMusicKeysAreFlat(t *testing.T) {
	for _, dark := range []bool{false, true} {
		name := "浅色"
		if dark {
			name = "深色"
		}
		t.Run(name, func(t *testing.T) {
			library := t.TempDir()
			other := t.TempDir()
			libSong(t, library, "别删我.mp3")
			libSong(t, other, "另一个目录.mp3")

			tt, s, _, _ := shellOnLocalMusic(t, library, other)
			tt.SetDark(dark)
			tt.Frame()
			noPointer(tt)

			assertKeysFlat(t, tt, "本地音乐", "全部加入队列", "重新扫描", "打开文件夹", deleteRowLabel,
				searchLocalLabel)
			assertTextKeysFlat(t, tt, flatPageBG(s, dark), "本地音乐", "添加目录")

			// 「移除此目录」只在一个**具体的**目录被选中时摆（「全部目录」那一档
			// 没有「当前目录」可移除）。直接拨那个字段而不是走下拉：下拉的选项
			// 是原生菜单，无头 Tester 里展开不了（`localmusic_delete_test.go`
			// 那一条为此 `t.Skipf` 了）。
			s.local.dirLabel = dirLabels([]string{library, other})[1]
			tt.Frame()
			noPointer(tt)
			assertKeysFlat(t, tt, "本地音乐", "移除此目录")
		})
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 五、收藏夹
// ─────────────────────────────────────────────────────────────────────────────

// TestTheCollectionKeysAreFlat 操作条那一排。
func TestTheCollectionKeysAreFlat(t *testing.T) {
	for _, dark := range []bool{false, true} {
		name := "浅色"
		if dark {
			name = "深色"
		}
		t.Run(name, func(t *testing.T) {
			_, s, tt := collectionShell(t, "12345",
				mediaOf(1, "歌 一"), mediaOf(2, "歌 二"), mediaOf(3, "歌 三"))
			tt.SetDark(dark)
			tt.Frame()
			noPointer(tt)

			assertKeysFlat(t, tt, "收藏夹", "添加到播放列表", "收藏这个收藏夹", "更多操作",
				collectionSearchLabel)
			assertTextKeysFlat(t, tt, flatPageBG(s, dark), "收藏夹", "多选")
		})
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 六、动态卡片
// ─────────────────────────────────────────────────────────────────────────────

// TestTheDynamicCardKeysAreFlat 卡片上那一排动作和那个作者名。
func TestTheDynamicCardKeysAreFlat(t *testing.T) {
	for _, dark := range []bool{false, true} {
		name := "浅色"
		if dark {
			name = "深色"
		}
		t.Run(name, func(t *testing.T) {
			item := uiDynamicArchive("v1", "BV1play")
			setLike(item, 12, false)
			s, tt, _ := dynamicCardShell(t, newUIExplore(t), item)
			tt.SetDark(dark)
			tt.Frame()
			noPointer(tt)

			// 作者名：`uiDynamic` 里叫「作者v1」。
			assertTextKeysFlat(t, tt, flatPageBG(s, dark), "动态卡片",
				"作者v1", "赞 12", "下一首播放", "下载音频", "下载视频")
		})
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 反面：真按钮那一批**仍然是真按钮**
// ─────────────────────────────────────────────────────────────────────────────

// TestTheDialogButtonsStayOutlined 对话框里的确定/取消仍然带描边和常驻底。
//
// 🔴 **这一条是和上面那一组对着断的**：票面点名「这些是主次分明的地方，去掉描边
// 就没了主次了」。只断「该扁的扁了」的话，一个把所有按钮都拍平的实现照样全绿——
// 而那个实现把「哪一个是主要动作」一起删掉了。
//
// 判据反过来用：`assertFlat` 要的两个「同一个颜色」在这里**必须不成立**——
// 框内和框外不同色（常驻底），框线那一行也和外面不同色（描边）。
func TestTheDialogButtonsStayOutlined(t *testing.T) {
	for _, dark := range []bool{false, true} {
		name := "浅色"
		if dark {
			name = "深色"
		}
		t.Run(name, func(t *testing.T) {
			_, tt := newDownloads(t, newTask("歌 甲", download.StatusPaused, dlProgress, 50))
			tt.SetDark(dark)
			tt.Frame()

			if err := tt.Click(opDelete); err != nil {
				t.Fatal(err)
			}
			dlFrame(tt)
			cancel, ok := tt.Find("取消")
			if !ok {
				t.Fatalf("弹窗上没有「取消」，文字有 %q", tt.Texts())
			}
			assertTextKeyHasAShell(t, tt.Image(), cancel, "弹窗里的「取消」")
		})
	}
}

// TestTheEmptyStateRetryStaysOutlined 空状态/错误页那颗重试也仍然是真按钮。
//
// 另一处「主次分明的地方」：`viewLoadError` 上那颗重试是这一整块里唯一的出路，
// 它得比旁边那几句说明看起来更像一个「按得动的东西」。
func TestTheEmptyStateRetryStaysOutlined(t *testing.T) {
	for _, dark := range []bool{false, true} {
		name := "浅色"
		if dark {
			name = "深色"
		}
		t.Run(name, func(t *testing.T) {
			s, _ := newShell(t, systemIsDark(dark))
			// 直接画那个错误块：这一条量的是那一颗按钮，走页面反而要造一台
			// 拉失败的假 B 站。
			tt := mygoui.NewTester(func(c *mygoui.Context) {
				s.viewLoadError(c, "flat:probe", "那一趟", "重试", errFlatProbe, func() {})
			}, 1200, 800)
			tt.SetDark(dark)
			tt.Frame()

			r, ok := tt.Find("重试")
			if !ok {
				t.Fatalf("错误块上没有「重试」，文字有 %q", tt.Texts())
			}
			assertTextKeyHasAShell(t, tt.Image(), r, "错误块里的「重试」")
		})
	}
}

// errFlatProbe 是上面那一条造出来的「一个错误」。
var errFlatProbe = &flatProbeError{}

type flatProbeError struct{}

func (*flatProbeError) Error() string { return "这一块没拉出来（测试造出来的）" }

// ─────────────────────────────────────────────────────────────────────────────
// 两处手搓的分割线换成了框架的 `ui.Divider`
// ─────────────────────────────────────────────────────────────────────────────

// TestTheTwoRulesAreDrawnAsOnePixelLines 队列抽屉和分集抽屉顶上那条线还在，
// 而且**还是那一像素**。
//
// 它们原来各是一句手搓的 `ui.Row/Box + Height(1) + Background(t.Border)`，这一票
// 换成框架的 `ui.Divider`——两处画出来逐字一样（1 DIP、无圆角、无内边距、
// `t.Border`，方向看自己在 `Row` 还是 `Column` 里）。所以判据不是「调了哪个
// 函数」（那只能证明换了写法，证明不了画面上还是一条线），而是**画面**。
//
// 🔴 **只在「头那一格底下」那一条几像素高的带子里找**，不在整窗里找。第一版
// 就是整窗找「最长的一条横线」，于是**删掉 `ui.Divider` 照样绿**：分集面板自己
// 的顶边就是一条 348 像素的 `t.Border`（评审用 `-overlay` 验的，报出来 348px /
// y=572），比 300 的门槛还长。队列抽屉那一半没这个问题（它删掉之后只剩 264），
// 但两半用同一把尺子才说得上「验过」。
//
// 那一行在哪儿是**布局常量**说了算的，不是猜的：
//
//	队列抽屉  面板贴着窗口右边、宽 `queueDrawerWidth`，头高 `queueHeaderHeight`
//	          → 线在面板顶往下第 `queueHeaderHeight` 行（实测 y=52，run=384）
//	分集抽屉  面板自己的框由 `Label("分集列表")` 给，头高 `partHeaderHeight`
//	          → 线在 `panel.Y + partHeaderHeight`（run 跟着面板宽度 `panel.W-8` 走）
//
// 面板**自己**的那一圈边（分集那个 popover 的顶边就在 `panel.Y`）落在带子外面。
func TestTheTwoRulesAreDrawnAsOnePixelLines(t *testing.T) {
	for _, dark := range []bool{false, true} {
		name := "浅色"
		if dark {
			name = "深色"
		}
		t.Run(name, func(t *testing.T) {
			s, tt := newShell(t)
			tt.SetDark(dark)
			// 队列抽屉。
			s.drawerOpen = true
			for range 3 {
				tt.Frame()
			}
			img := tt.Image()
			border := flatColor(BuildTheme(s.app.SettingsSnapshot(), dark).Border)
			x0 := img.Rect.Max.X - queueDrawerWidth
			assertRuleAt(t, img, border, "队列抽屉", x0, img.Rect.Max.X-1,
				queueHeaderHeight-2, queueHeaderHeight+2, queueDrawerWidth-8)

			// 分集抽屉：要一首有多 P 的，否则那一列摆不出来（`partsDrawer`
			// 只在 `PartsWorthAsking` 的时候画）。
			s.drawerOpen = false
			sp, stt := onlineTester(t, multiPages(), false)
			stt.SetDark(dark)
			waitFrames(t, stt, "分集问回来了", func() bool { return len(sp.app.Parts()) == 3 })
			sp.partOpen = true
			for range 3 {
				stt.Frame()
			}
			panel, ok := stt.Find("分集列表")
			if !ok {
				t.Fatalf("画面上找不到分集面板，文字有 %q", stt.Texts())
			}
			py := round32(panel.Y)
			assertRuleAt(t, stt.Image(), border, "分集抽屉",
				round32(panel.X), round32(panel.X+panel.W)-1,
				py+partHeaderHeight-2, py+partHeaderHeight+2, int(panel.W)-8)
		})
	}
}

// assertRuleAt 断给定那一条横带子里有**恰好一行**横贯的 `t.Border` 线。
//
// 「恰好一行」= 1 DIP：多一行就是 2 像素，那已经不是 `ui.Divider` 画出来的东西了。
// `minRun` 是「横贯」的下限（按抽屉宽减几个像素），所以带子里别的东西（某一行
// 的一半、一个图标的边）不会顶替它。
func assertRuleAt(t *testing.T, img *image.RGBA, border color.RGBA, what string, x0, x1, y0, y1, minRun int) {
	t.Helper()
	if x0 < 0 {
		x0 = 0
	}
	lines, longest := 0, 0
	for y := y0; y <= y1; y++ {
		run := 0
		best := 0
		for x := x0; x <= x1; x++ {
			if maxDiff(img.RGBAAt(clampX(img, x), clampY(img, y)), border) <= probeTolerance {
				run++
				best = max(best, run)
			} else {
				run = 0
			}
		}
		longest = max(longest, best)
		if best >= minRun {
			lines++
		}
	}
	if lines != 1 {
		t.Errorf("%s：y=%d..%d 那一条带子里横贯的描边线有 %d 行（want 恰好 1），"+
			"最长的一行 %d 像素（门槛 %d）——头顶那条分割线没画出来（或者不只一像素）",
			what, y0, y1, lines, longest, minRun)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 顶栏那枚「迷你」
// ─────────────────────────────────────────────────────────────────────────────

// TestTheNavBarMiniKeyIsAnIcon 顶栏末尾那枚「迷你」是图标键了。
//
// 它原来是**文字键**（「迷你」两个字），和左边三枚图标键并排，样子不齐。这次接
// 上了 `iconPiP`（上游 `components/window-action/index.tsx:46` 就是
// `PictureInPicture`）。
//
// 三样一起断：框是图标键的框（不是那两个字的宽度）、框里画了图形、名字还叫得出
// 来（`Label`——文字消失之后它是读屏软件和测试唯一的凭据）。
func TestTheNavBarMiniKeyIsAnIcon(t *testing.T) {
	_, tt := flatSideShell(t, false)

	r, ok := tt.Find("迷你")
	if !ok {
		t.Fatalf("顶栏上找不到「迷你」，文字有 %q", tt.Texts())
	}
	// 两个汉字 13 号 + 两边 14 的内边距 = 54 上下；图标键是 28。用 40 当分界，
	// 两边都离得开。
	if r.W > 40 {
		t.Errorf("顶栏的「迷你」框有 %.1f 宽——它还是那两个字的文字键，不是图标键", r.W)
	}
	// ⚠️ **高度也要断**：文字键上 `Find` 给回来的是**那两个字自己的框**（文字键
	// 没有 `Label`），它宽 26、高 15 ——宽度那一条正好放它过去（实测：只断宽度时
	// 把这个键改回 `flatButton(c, "迷你")` 照样绿）。图标键给回来的是**按钮的框**
	// （`Label` 挂在按钮上），28 高。
	if r.H < 24 {
		t.Errorf("顶栏的「迷你」框只有 %.1f 高——它是那两个字的文字框，不是一枚图标键", r.H)
	}
	if s := scanGlyph(tt, "迷你"); s.ink == "" {
		t.Errorf("「迷你」那一格里没画图形（只有底色 %v）——图标没接上", s.bg)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 出图
// ─────────────────────────────────────────────────────────────────────────────

// TestRenderFlatButtonShots 把这六处存成 PNG，留给人眼和上游并排看。
//
//	BIU_SHOT_DIR=/some/dir go test ./internal/ui/ -run TestRenderFlatButtonShots
//
// 深浅各一套（理由同 `flatbutton_test.go` 文件头：深色下那圈描边更显眼，只出浅色
// 等于没验）。每一张挑的都是**这一票动过的那几枚**所在的地方：
//
//	80 满屏播放器（顶栏 + 分集面板）	81 迷你播放器（320×100）
//	82 下载记录（页头 + 操作列）		83 本地音乐（页头 + 操作行 + 行尾）
//	84 收藏夹（操作条那一排）		85 动态卡片（作者名 + 动作那一排）
//
// ⚠️ **一个字都不许打真 B 站接口**：全部是内存夹具（`multiPages` / `dlSnapshot` /
// `libSong` / `collectionShell` / `uiDynamicArchive`），和 `chromebg` 那一族同一个
// 约定。
func TestRenderFlatButtonShots(t *testing.T) {
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
			// 80 满屏播放器（多 P 的稿子才摆得出分集把手）。
			s, tt := onlineTester(t, multiPages(), false)
			tt.SetDark(dark)
			s.openFullPlayer()
			waitFrames(t, tt, "分集问回来了", func() bool { return len(s.app.Parts()) == 3 })
			s.full.partsOpen = true
			tt.Frame()
			noPointer(tt)
			save(t, dir, "80-满屏播放器-"+name+".png", tt)

			// 81 迷你播放器（320×100）。
			ms, a := newMiniShell(t, nil)
			if err := enqueueTwoWithCover(a, t); err != nil {
				t.Fatal(err)
			}
			mt := miniTester(ms)
			mt.SetDark(dark)
			mt.Frame()
			noPointerMini(mt)
			save(t, dir, "81-迷你播放器-"+name+".png", mt)

			// 82 下载记录。
			_, dt := newDownloads(t,
				newTask("歌 甲", download.StatusDone, dlChunk, 100),
				newTask("歌 乙", download.StatusError, dlProgress, 50),
			)
			dt.SetDark(dark)
			dt.Frame()
			noPointer(dt)
			save(t, dir, "82-下载记录-"+name+".png", dt)

			// 83 本地音乐（表格那一档，行尾那两枚在里面）。
			library := t.TempDir()
			libSong(t, library, "第一首.mp3")
			libSong(t, library, "第二首.mp3")
			lt, _, _, _ := shellOnLocalMusic(t, library)
			lt.SetDark(dark)
			lt.Frame()
			noPointer(lt)
			save(t, dir, "83-本地音乐-"+name+".png", lt)

			// 84 收藏夹（操作条那一排）。
			_, _, ct := collectionShell(t, "12345",
				mediaOf(1, "歌 一"), mediaOf(2, "歌 二"), mediaOf(3, "歌 三"))
			ct.SetDark(dark)
			ct.Frame()
			noPointer(ct)
			save(t, dir, "84-收藏夹-"+name+".png", ct)

			// 85 动态卡片。
			item := uiDynamicArchive("v1", "BV1play")
			setLike(item, 12, false)
			_, yt, _ := dynamicCardShell(t, newUIExplore(t), item)
			yt.SetDark(dark)
			yt.Frame()
			noPointer(yt)
			save(t, dir, "85-动态卡片-"+name+".png", yt)
		})
	}
}
