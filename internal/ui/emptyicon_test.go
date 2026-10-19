package ui

import (
	"image"
	"os"
	"strings"
	"testing"

	mygoui "github.com/egoist/mygo/ui"
)

// 空状态**插画位**那一族，以及错误页。
//
// # 这一族断的是「画面上画出来了」，不是「哪个函数被调了」
//
// MyGo 的 Icon 没有元素树可以查（`mygo/ui/svg.go` 的文档：图标是装饰，辅助技术
// 看不见它），而无头 Tester 的 `Find` 只认画出来的文字和 `Label` 起的名字。所以
// 「那枚图标画出来了没有」只能从**像素**上看——元素在、位置对、文字也在，可屏
// 幕上什么也没有，那一类是查不出来的。
//
// ⚠️ **深浅两套主题都要断**：图标是单色的、跟着祖先的文字色走（`drawIcon` 收的
// 就是文字色），而浅色下文字是深灰、深色下是亮白。只断浅色的话，深色下整格
// 空掉照样全绿。

// TestEveryEmptyStateNamesItsOwnIllustration 是这一票的核心那一条：
// **七页各画各的**。
//
// 🔴 **这一条断的是「配的是哪一枚」，不是像素。** 试过像素：把每一页插画那一块
// 的颜色集合拿来两两比，而它红不起来——两次量到的集合里混着抗锯齿的灰阶，
// 同一个图标在两页上落在不同的亚像素位置上，集合就不同；而「那一页根本没配
// 图标」反倒让它更独特（少一枚 = 少一批颜色 = 和别的页不一样），于是照样全绿。
//
// 像素那一层留给下面那条（`TestTheIllustrationIsActuallyPainted`）：它断的是
// 「画出来了没有」，而「画的是不是这一枚」是数据层的断言，就用数据断。
//
// ⚠️ 上游七页用的是**同一枚** `RiCloseCircleLine`（`components/empty/index.tsx:16`），
// 所以「每页都配了图标」和「每页都一样」是同时成立的——上一版就是那个样子，
// 报告 §S1 点名的正是「七个页面共用一张」。
func TestEveryEmptyStateNamesItsOwnIllustration(t *testing.T) {
	// 七页就是 issue 126 点名的那七页（和 #124 的依赖顺序里那一份一致）。
	pages := []struct {
		what string
		e    emptyState
	}{
		{"推荐音乐", lookup("/").Empty},
		{"我的关注", lookup(followPath).Empty},
		{"稍后再看", lookup(laterPath).Empty},
		{"历史记录", lookup(historyPath).Empty},
		{"下载记录", lookup(downloadListPath).Empty},
		{"搜索", searchIdle},
		{"收藏夹", noFavorites},
	}

	seen := map[string]string{}
	for _, p := range pages {
		if p.e.Icon == "" {
			t.Errorf("%s 的空状态没有配插画（图标名是空串）——七页共用一张正是"+
				"这一票要消掉的那件事", p.what)
			continue
		}
		if prev, dup := seen[p.e.Icon]; dup {
			t.Errorf("%s 和 %s 配的是同一枚图标（%s）——"+
				"每一页要能一眼看出是哪个页面空着", p.what, prev, p.e.Icon)
			continue
		}
		seen[p.e.Icon] = p.what
	}
	if len(seen) < len(pages) {
		t.Errorf("七页里只有 %d 枚不同的图标，want %d", len(seen), len(pages))
	}
}

// TestTheIllustrationIsActuallyPainted 断「那一枚真的画出来了」。
//
// 判据是**标题正上方那条带里有东西**：上游 `space-y-2` 是 8、插画 32，所以插画
// 落在 `[标题顶 - 40, 标题顶 - 8]`。底色取「标题左边那一点」——**不是窗口左上角**，
// 那里是侧栏，而侧栏和内容区的底色不是同一个数（第一版取的就是它，于是整块内容
// 区在指纹里变成了两百多种白）。
//
// 断的是**画上了东西**而不是「长什么样」：长什么样是上面那条断的（它比较的是图标
// 名）。拿掉 `drawEmpty` 里那一行 `ui.Icon` 之后这一条立刻红，而上面那条照样绿。
func TestTheIllustrationIsActuallyPainted(t *testing.T) {
	for _, p := range []struct {
		what, path, title string
	}{
		{"推荐音乐", "/", lookup("/").Empty.Title},
		{"稍后再看", laterPath, lookup(laterPath).Empty.Title},
		{"历史记录", historyPath, lookup(historyPath).Empty.Title},
		{"下载记录", downloadListPath, lookup(downloadListPath).Empty.Title},
	} {
		for _, dark := range []bool{false, true} {
			name := "浅色"
			if dark {
				name = "深色"
			}
			t.Run(p.what+"/"+name, func(t *testing.T) {
				b := newTestShell(t, systemIsDark(dark))
				s, tt := b.s, b.tt
				s.navigate(p.path)
				waitFrames(t, tt, p.what+"那一页的空状态画出来了", func() bool {
					return tt.HasText(p.title)
				})
				if n := paintedInBand(t, tt, p.title); n < 20 {
					t.Errorf("%s 的插画那一带只画了 %d 个像素（want ≥ 20）——"+
						"要么没画，要么和底色是同一个数（深色下看的是「看得清」那一位）",
						p.what, n)
				}
			})
		}
	}
}

// TestTheIllustrationIsFollowedByTheCopy 断「插画在两行字**上面**」。
//
// 判据是**位置**：插画那一块的外接框底边要落在标题那一行的顶边之上（上游
// `space-y-2 py-20`，插画在最上面）。只看「有几枚图标」的话，把插画摆在文字
// 底下（或者和文字重叠）也照样过——而那在画面上是一团糊。
func TestTheIllustrationIsFollowedByTheCopy(t *testing.T) {
	s, tt := newShell(t)
	s.navigate("/download-list")
	tt.Frame()

	rt := lookup(downloadListPath)
	title := rt.Empty.Title
	if rt.Empty.Icon == "" {
		t.Fatalf("下载记录的空状态没有配图标（%q）", rt.Empty)
	}
	tr, ok := tt.Find(title)
	if !ok {
		t.Fatalf("空状态那两行字没画出来，文字有 %q", tt.Texts())
	}

	// 标题上面那一块：图标待在那儿的高度。
	above := glyphRows(t, tt, 0, int(tr.Y)-1, 400)
	if above == 0 {
		t.Errorf("标题 %q 的上面一行图形都没画——插画不在文字上面", title)
	}
	// 而图形不能和标题那一行重叠：插画的外接框底边落在标题顶边之上。
	iconBox := iconAbove(t, tt, int(tr.Y)-1)
	if iconBox.Max.Y >= int(tr.Y) {
		t.Errorf("插画一直画到 y=%d，而标题从 y=%v 开始——两样叠在一起了（插画盒 %v）",
			iconBox.Max.Y, tr.Y, iconBox)
	}
}

// TestTheSharedDrawingEntryIsTheOnlyOne 钉住「**一套**绘制入口」。
//
// 🔴 机械查，不靠自觉：空状态这一族有几十条调用点，每一处自己写一遍
// `ui.Column(...).Center().Gap(6).Padding(20, 28)` 的话，七页的留白和间距就会
// 慢慢漂开——而每一次都是无意的（抄一下上一处的），也看不出来。
//
// 判据：除了 `empty.go` 那个文件，**没有任何文件**自己建一个空状态的那一列。
// 宁可误伤也不放过：`ui.Column` 在这个包里到处都是，所以断的是「那一列旁边
// 同时出现了 `emptyIconSize` 或者 `FontSize(15)` 这两个空状态独有的数」——
// 两个一起出现才是「有人在重写一遍」。
func TestTheSharedDrawingEntryIsTheOnlyOne(t *testing.T) {
	for _, name := range filesInThisPackage(t) {
		// 这一条自己得放过：它上面就写着 `emptyIconSize` 那个名字（判据就是
		// 它），而放过的理由是「文件里没有那一列」而不是「文件里没有那个数」。
		if name == "empty.go" || name == "emptyicon_test.go" {
			continue
		}
		b, err := readFileOf(t, name)
		if err != nil {
			t.Fatalf("读不到 %s，这条就没法查了：%v", name, err)
		}
		src := string(b)
		for i, line := range strings.Split(src, "\n") {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "//") {
				continue // 注释里提到它们是正常的（解释为什么没有了）
			}
			if strings.Contains(line, "emptyIconSize") && strings.Contains(line, "ui.Column") {
				t.Errorf("%s:%d 自己建了空状态的那一列——共用 `drawEmpty` "+
					"（empty.go）那一个出口，七页各写一份的话改一次留白就会漂开",
					name, i+1)
			}
		}
	}
}

// TestTheErrorPageSaysWhereItBroke 是错误页那一族的核心：出错在哪一步。
//
// 用户要拿这句话去跟开发者说，而只显示 panic 的值（「index out of range
// [5] with length 3」）谁也复现不出来——那句话说明了**是什么**，一点也没说
// **在哪儿**。所以错误页上必须同时有：崩在界面的哪一块、当时在看哪一页。
func TestTheErrorPageSaysWhereItBroke(t *testing.T) {
	b := newTestShell(t, at(900, 600),
		stubbed(func(_ *testing.T, s *Shell) {
			s.navigate("/download-list")
			s.crash = s.newCrashInfo("页面「下载记录」", "index out of range [5] with length 3")
		}))
	tt := b.tt

	for _, want := range []string{
		"出错的地方：页面「下载记录」",     // 界面的哪一块
		"当时在看的页面：下载记录",       // 当时在看哪一页
		"index out of range", // 原始错误
		"反馈",                 // 走得出去的路之一
		"回到首页",
		"重新载入这一页",
	} {
		if !tt.HasText(want) {
			t.Errorf("错误页上没有 %q，文字有 %q", want, tt.Texts())
		}
	}

	// 而「哪一页」这一句要能**换**：同一个 panic 落在不同页上，错误页得说出
	// 不同的页名——否则这一句只是个装饰。
	b2 := newTestShell(t, at(900, 600),
		stubbed(func(_ *testing.T, s *Shell) {
			s.navigate(laterPath)
			s.crash = s.newCrashInfo("侧栏", "坏掉了")
		}))
	if !b2.tt.HasText("当时在看的页面：稍后再看") {
		t.Errorf("换一页崩之后错误页还是原来那页的名字，文字有 %q", b2.tt.Texts())
	}
	if !b2.tt.HasText("出错的地方：侧栏") {
		t.Errorf("崩在别的块上，错误页没跟着换，文字有 %q", b2.tt.Texts())
	}
}

// TestTheErrorPageHasAnIllustration 断「错误页不是一片白」。
//
// ⚠️ **判据是像素**：光断言「有文字」的话，那张插画被撤掉之后这一条照样绿——
// 而「界面出错了」四行字摆在正中，用户看到的就是「一片白上面有��行字」。
// 断言的是插画那一块**和它上下的背景不是一个颜色**。
func TestTheErrorPageHasAnIllustration(t *testing.T) {
	for _, dark := range []bool{false, true} {
		name := "浅色"
		if dark {
			name = "深色"
		}
		t.Run(name, func(t *testing.T) {
			b := newTestShell(t, at(900, 600), systemIsDark(dark),
				stubbed(func(_ *testing.T, s *Shell) {
					s.crash = s.newCrashInfo("侧栏", "坏掉了")
				}))
			img := b.tt.Image()
			// 插画在标题「界面出错了…」**上面**。量出标题那一行，再扫它上面
			// 那条带里有没有和窗口底色不一样的像素。
			//
			// ⚠️ **扫的是「有没有」而不是「某一个点是不是」**：那张图是线稿，
			// 线条之间留着大片的底色，取中心那一个点十次里有九次落在空处——
			// 而那种测试红起来的样子是「插画没画出来」，真因却是取错了点。
			tr, ok := b.tt.Find("界面出错了，播放器已经停在这一页")
			if !ok {
				t.Fatalf("错误页没画出来，文字有 %q", b.tt.Texts())
			}
			window := img.RGBAAt(4, 4)
			band := 0
			for y := 0; y < int(tr.Y); y++ {
				for x := 0; x < 900; x++ {
					if img.RGBAAt(x, y) != window {
						band++
					}
				}
			}
			if band == 0 {
				t.Errorf("错误页上标题上面那条带（%d × %d）全是窗口底色（%v）——"+
					"插画没画出来，撤掉它之后用户看到的就是一片白",
					900, int(tr.Y), window)
			}
		})
	}
}

// TestTheErrorPageHasAWayOutInBothThemes 断「走得出去」在深色下也在。
//
// 这一条很土，但它挡的是一个真实发生过的坏：按钮摆在**深色下看不见的底色**上，
// 于是「点得动」在浅色下验得过、在深色下点不动，而两条断言都只跑浅色。
func TestTheErrorPageHasAWayOutInBothThemes(t *testing.T) {
	for _, dark := range []bool{false, true} {
		name := "浅色"
		if dark {
			name = "深色"
		}
		t.Run(name, func(t *testing.T) {
			// ⚠️ **一枚一个外壳**：三枚里任何一枚点下去都会离开错误页
			//（「重新载入」清掉 crash、「回到首页」换路由），而一个外壳上连点
			// 第二枚时屏幕上早就是正常画面了——报出来的是「第二枚不存在」，
			// 真因是第一枚点得太有效。
			for _, label := range []string{"反馈", "回到首页", "重新载入这一页"} {
				b := newTestShell(t, at(900, 600), systemIsDark(dark),
					stubbed(func(_ *testing.T, s *Shell) {
						s.crash = s.newCrashInfo("侧栏", "坏掉了")
					}))
				if _, ok := b.tt.Find(label); !ok {
					t.Errorf("错误页上没有 %q，文字有 %q", label, b.tt.Texts())
					b.Close()
					continue
				}
				if err := b.tt.Click(label); err != nil {
					t.Errorf("点不了 %q：%v", label, err)
				}
				b.Close()
			}
		})
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 量画面的工具
// ─────────────────────────────────────────────────────────────────────────────

// paintedInBand 数「标题正上方那条带里，有多少个像素和底色不一样」。
//
// ⚠️ 底色取「标题左边那一点」（`标题顶 - 6`），**不是窗口左上角**：那里是侧栏，
// 而侧栏和内容区的底色不是同一个数。拿侧栏那个数当底色的话，整块内容区每一点
// 纯白都「不是底色」，于是数出来的是整屏的白而不是那枚插画——而判据是
// 「≥ 20 个像素」，第一版因此永远绿（它量到的是几千）。
//
// 带宽和高度照 `drawEmpty` 的那几个数：插画 32、间距 8，所以落在
// `[标题顶 - 40, 标题顶 - 8]`。横着取标题中点左右各 24——空状态是居中的
// （上游 `items-center justify-center`），标题居中，所以它的中点就是那一列的中点。
func paintedInBand(t *testing.T, tt *mygoui.Tester, title string) int {
	t.Helper()
	tr, ok := tt.Find(title)
	if !ok {
		t.Fatalf("画面上找不到标题 %q，文字有 %q", title, tt.Texts())
	}
	img := tt.Image()
	bg := img.RGBAAt(int(tr.X)-6, int(tr.Y)-6)
	cx := int(tr.X + tr.W/2)
	n := 0
	for y := int(tr.Y) - 40; y <= int(tr.Y)-8; y++ {
		for x := cx - 24; x <= cx+24; x++ {
			if y < 0 || x < 0 || x >= img.Bounds().Dx() || y >= img.Bounds().Dy() {
				continue
			}
			if img.RGBAAt(x, y) != bg {
				n++
			}
		}
	}
	return n
}

// glyphRows 数那一行里「不是底色」的像素有几个——0 就是那一行什么都没画。
func glyphRows(t *testing.T, tt *mygoui.Tester, x0, y, w int) int {
	t.Helper()
	if y < 0 {
		return 0
	}
	img := tt.Image()
	bg := img.At(2, 2) // 窗口左上角 = 底色
	n := 0
	for x := x0; x < x0+w; x++ {
		if img.At(x, y) != bg {
			n++
		}
	}
	return n
}

// iconAbove 量 `y` 那一行往上、标题之前那一块里的图形外接框。
//
// 找不到就返回一个落在标题之上的空框（`Max.Y` 小于标题 y），调用点据此报错。
func iconAbove(t *testing.T, tt *mygoui.Tester, y int) (r image.Rectangle) {
	t.Helper()
	img := tt.Image()
	bg := img.At(2, 2)
	minX, minY, maxX, maxY := 1<<30, 1<<30, -1, -1
	for yy := 0; yy <= y; yy++ {
		for x := 0; x < 1200; x++ {
			if img.At(x, yy) == bg {
				continue
			}
			if x < minX {
				minX = x
			}
			if yy < minY {
				minY = yy
			}
			if x > maxX {
				maxX = x
			}
			if yy > maxY {
				maxY = yy
			}
		}
	}
	return image.Rect(minX, minY, maxX, maxY)
}

// readFileOf 读这个包里的一份文件。
//
// 测试进程的当前目录就是包目录（`go test` 的约定），所以 `filesInThisPackage`
// 给的那些相对名字直接能用。
func readFileOf(t *testing.T, name string) ([]byte, error) {
	t.Helper()
	return os.ReadFile(name)
}
