package ui

// 播放条上那几样**状态**：没歌、加载中、时间读数、没封面、下载键、歌名可点。
//
// 这一组是票 #222 的验收（报告 §5.2 §5.3 §4.2 §3.12 §6.5）。
//
// # 判据都是**画面**
//
// SPEC 的 Testing Decisions 说 UI 测试断渲染结果，不断言「调了哪个函数」。
// 这一族里有一处例外（歌名要的是哪一枚图形），那一处断的是源码里的那一句——
// 理由写在那一条自己的注释里。

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"testing"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/app"
	mygoui "github.com/egoist/mygo/ui"
)

// ---- §5.2 没歌时不摆封面占位 ----

// TestNoTrackMeansNoCoverPlaceholder 断**没歌时播放条上没有封面那一块**。
//
// 🔴 **上游在没有歌的时候左边那一块整块不画**（`layout/playbar/index.tsx:28` 的
// `{Boolean(playId) && <Left />}`）——画面上**没有那个方块**。我们原来摆的是
// 一块「封面占位」（描边框 + 一枚图形），那一块在用户眼里是「有一首歌，只是
// 封面还没拿到」。
//
// ⚠️ **判据是「那一块是干净的底色」**：没歌时那一格上**什么都没有**，于是画面上
// 那一块就是播放条的底色——**只有一种颜色**。而有歌（本地文件）时那一格是
// 「描边框 + 一枚音符」，颜色不止一种、顶上还有那一圈线。
//
// ⚠️ **只扫上面那一条**（`coverTopColors`）：没歌时「还没有打开文件」那句字落在
// 原来封面那一格的位置上（x 从 16 起，见 `viewCover`），整格扫会把那行字算进来
// ——而那一格本来该是**什么都没有**。
func TestNoTrackMeansNoCoverPlaceholder(t *testing.T) {
	_, tt := coverBarTester(t, app.State{})

	if got := coverTopColors(t, tt); len(got) != 1 {
		t.Errorf("没歌时封面那一块画了东西（%v）——上游那一块整块不画，"+
			"我们原来摆的是一个「封面占位」，在用户眼里是「有一首歌，封面还没拿到」", got)
	}
	if hasCoverBorder(tt) {
		t.Error("没歌时封面那一块还有一圈描边框——那一圈是「这一格最后会是一张图」的标记，" +
			"而没歌的时候压根没有图")
	}

	// 有歌（本地文件，没有封面）时那一格**是有的**：它摆的是一枚音符
	// （上游的 `emptyPlaceholder={<RiMusic2Line />}`）。
	_, withTrack := coverBarTester(t, app.State{
		HasTrack: true, Title: "本地的一首歌", Source: app.SourceLocal,
	})
	if got := coverTopColors(t, withTrack); len(got) < 2 {
		t.Errorf("有歌但没有封面时那一块是一块纯色（%v）——那一格该摆一枚音符", got)
	}
	if !hasCoverBorder(withTrack) {
		t.Error("有歌但没有封面时那一块没有描边框——「压根没有地址」和「拉失败了」" +
			"分得开靠的就是这一圈线加上不同的图形")
	}
}

// coverTopColors 报封面那一格**上面一条**出现了哪几种颜色（去重、不计次序）。
//
// ⚠️ **只取上面一条**（y 从 16 到 30）：没歌时「还没有打开文件」那句字落在原来
// 封面那一格的位置上（x 从 16 起），整格扫会把那行字算进来——而没歌时那一格
// 本来该是**什么都没有**，判据要问的正是「那儿有没有东西」。
func coverTopColors(t *testing.T, tt *mygoui.Tester) []string {
	t.Helper()
	img := tt.Image()
	seen := map[string]bool{}
	var out []string
	for y := biuPlaybarPadX; y < biuPlaybarPadX+14; y++ {
		for x := biuPlaybarPadX; x < biuPlaybarPadX+biuCoverSize; x++ {
			k := hexOf(img.At(x, y))
			if seen[k] {
				continue
			}
			seen[k] = true
			out = append(out, k)
		}
	}
	return out
}

// ---- §4.2 时间读数 ----

// TestTheTrackTimeTellsTheHourWhenItIsPastAnHour 断**超一小时写成 hh:mm:ss**。
//
// 🔴 我们原来只有 `m:ss`，于是 75 分钟的歌显示成「75:00」——那不是时长，那是
// 分钟数（而用户读不出它是分钟数）。上游那个 `formatDuration`
// （`src/common/utils/time.ts:6-16`）是 `seconds >= 3600 ? hh:mm:ss : mm:ss`。
//
// ⚠️ **小时是两位**（上游的 `trim: false`）：`01:15:00`，不是 `1:15:00`。
//
// ⚠️ **断的是画面上那个读数**（不是去问那个格式化函数）：读数钉在 48 宽的格子里，
// 画面上找得到它，用户才看得见它。
func TestTheTrackTimeTellsTheHourWhenItIsPastAnHour(t *testing.T) {
	tt, _ := metaBarTester(t, app.State{
		HasTrack: true, Title: "一小时以上的一首",
		Duration: 75 * time.Minute, Position: 90 * time.Minute,
	}, 1200)

	for _, want := range []string{"01:30:00", "01:15:00"} {
		if _, ok := tt.Find(want); !ok {
			t.Errorf("画面上找不到读数 %q（位置 %d 分 / 总长 %d 分），文字有 %q",
				want, 90, 75, tt.Texts())
		}
	}
	// 一小时以内还是 m:ss（上游那一支）。
	short, _ := metaBarTester(t, app.State{
		HasTrack: true, Title: "短的一首", Duration: 100 * time.Second,
	}, 1200)
	for _, want := range []string{"1:40"} {
		if _, ok := short.Find(want); !ok {
			t.Errorf("画面上找不到读数 %q，文字有 %q", want, short.Texts())
		}
	}
}

// TestTheTrackTimeReadsDashWhenThereIsNone 断**读不到的时候写 `-:--`**。
//
// 🔴 **一个减号**（上游 `components/music-play-progress/index.tsx:27,48`：
// `currentTime ? formatDuration(currentTime) : "-:--"`），而我们原来总长写的是
// `--:--`（两个减号），位置写的是 `0:00`（那是在说「这一秒零」，不是「还没读到
// 一个位置」）。
//
// ⚠️ **两个读数都要断**：上游那两个是同一个写法，而我们原来是**两个不同的**
// （一个 `0:00` 一个 `--:--`）——只断一个的话，改了另一个没人知道。
func TestTheTrackTimeReadsDashWhenThereIsNone(t *testing.T) {
	tt, _ := metaBarTester(t, app.State{HasTrack: true, Title: "一首歌"}, 1200)

	dashes := 0
	for _, r := range tt.Texts() {
		if r == "-:--" {
			dashes++
		}
	}
	if dashes != 2 {
		t.Errorf("画面上有 %d 个 `-:--`，want 2——两个读数在读不到的时候都要是它，文字有 %q",
			dashes, tt.Texts())
	}
}

// ---- §5.3 没封面时是音乐符号 ----

// TestThePlayBarAsksForTheMusicNote 断播放条上「压根没有封面」那一格要的是
// **一枚音符**，不是那枚图片图标。
//
// ⚠️ **断的是源码里的那一句**（AST），不是画面：音符和图片图标在画面上都是
// 「一枚灰色的图形」，像素上分不出是哪一枚——而这一条要断的恰恰是「是哪一枚」。
// 画面那一半在下面那条（`TestThePlayBarCoverWithoutOneShowsSomething`）。
//
// 🔴 **上游那个 `Image` 传的是 `emptyPlaceholder={<RiMusic2Line />}`**
// （`layout/playbar/left/index.tsx:40`），而我们原来摆的是那枚图片图标。
// 本地文件**永远**没有封面（见 `State.CoverURL`），所以这一档在播放条上是常客。
func TestThePlayBarAsksForTheMusicNote(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "playbar.go", nil, 0)
	if err != nil {
		t.Fatalf("解析 playbar.go 失败：%v", err)
	}
	found := false
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || len(call.Args) == 0 {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "ViewPlaceholder" {
			return true
		}
		// 最后一个实参是那一格摆的图形（上游的 `emptyPlaceholder`）。
		//
		// ⚠️ **比的是那一段源码**：图形是 `icon(iconEmptyMusic)` 这样包着的，
		// 而 `chainCalls` 只看得见链上的**调用**（这里是 `icon`），看不见它的
		// 实参（`iconEmptyMusic`）。
		if strings.Contains(goformat(fset, call.Args[len(call.Args)-1]), "iconEmptyMusic") {
			found = true
		}
		return true
	})
	if !found {
		t.Error("playbar.go 里画封面那一格要的不是 `iconEmptyMusic`（音乐符号）——" +
			"上游那个 Image 传的是 `emptyPlaceholder={<RiMusic2Line />}`，" +
			"而我们原来摆的是一枚图片图标")
	}
}

// TestThePlayBarCoverWithoutOneShowsSomething 断**没封面时那一格画得出东西**。
//
// 🔴 **一个空框加一枚图形**（不是一块空白）：本地文件永远没有封面，而那一格是
// 播放条上常驻的一块——空着的话用户看到的是「这里缺一块」。
//
// ⚠️ **判据是画面**：那一格里有图形（颜色不止一种）且顶上有一圈描边框。
func TestThePlayBarCoverWithoutOneShowsSomething(t *testing.T) {
	_, tt := coverBarTester(t, app.State{
		HasTrack: true, Title: "本地的一首歌", Source: app.SourceLocal,
	})
	if got := coverColors(t, tt, 4); len(got) < 2 {
		t.Errorf("没封面时那一格是一块纯色（%v）——它该摆一枚音符，"+
			"空着的话用户看到的是「这里缺一块」", got)
	}
	if !hasCoverBorder(tt) {
		t.Error("没封面时那一格没有描边框——那一圈线是「这一格最后会是一张图，"+
			"但现在没有」的标记")
	}
}

// ---- §3.12 下载键 ----

// TestTheDownloadKeyIsNotThereForALocalFile 断**本地文件不摆「下载」那一枚**。
//
// 🔴 上游那一支的条件是 `{Boolean(playId) && getPlayItem()?.source !== "local"
// && <MusicDownloadButton />}`（`playbar/right/index.tsx:15`）：本地文件在 B 站
// 上压根没有 cid 可下，按下去只会得到一句「这一首下不了」——**给一个按了只会
// 报错的键不如不摆**。
//
// ⚠️ **这一条以前判不出来**：`app.State` 里没有来源那一栏（`Item.Source` 在队列
// 那一层，不在快照里），现在有了（见 `State.Source`）。
func TestTheDownloadKeyIsNotThereForALocalFile(t *testing.T) {
	local, _ := metaBarTester(t, app.State{
		HasTrack: true, Title: "本地的一首歌", Source: app.SourceLocal,
		Rate: 1, Volume: 0.8,
	}, 1200)
	if local.HasText("下载") {
		t.Errorf("本地文件时播放条上画出了「下载」，文字有 %q", local.Texts())
	}

	online, _ := metaBarTester(t, app.State{
		HasTrack: true, Title: "B 站的一首歌", Source: app.SourceOnline,
		Rate: 1, Volume: 0.8,
	}, 1200)
	if !online.HasText("下载") {
		t.Errorf("B 站那一首的播放条上没有「下载」，文字有 %q", online.Texts())
	}
}

// ---- §6.5 歌名可点 ----

// TestClickingTheTitleOpensTheBiliPage 断**点歌名会跳 B 站视频页**。
//
// 🔴 上游那一句点了会跳（`layout/playbar/left/index.tsx:54-57` 的
// `openBiliVideoLink`）：那是用户在这首歌上能走的**第二条路**——听不下去就去
// B 站看它。
//
// ⚠️ **判据是「跳的是哪一个地址」**，不是「按下去没报错」：按下去没报错也可能
// 是跳到了首页，而那正是「摆了个能点的东西但没接上」最常见的症状。
//
// ⚠️ **真窗口里不能真的开浏览器**：所以这一条走接缝（`Shell.openExternal`），
// 记下地址就够了。
func TestClickingTheTitleOpensTheBiliPage(t *testing.T) {
	const want = "https://www.bilibili.com/video/BV1xx411c7mD"
	tt, s := metaBarTester(t, app.State{
		HasTrack: true, Title: "一首歌", Source: app.SourceOnline, BiliURL: want,
	}, 1200)

	var got string
	s.openExternal = func(url string) error { got = url; return nil }

	if err := tt.Click("一首歌"); err != nil {
		t.Fatalf("点不动歌名：%v（画面：%q）", err, tt.Texts())
	}
	tt.Frame()
	if got != want {
		t.Errorf("点歌名跳的是 %q，want %q", got, want)
	}
}

// TestTheTitleIsNotClickableForALocalFile 断**本地文件的歌名点不动**。
//
// 🔴 上游的 `isClickable` 就是 `playItem && playItem.source !== "local"`
// （`layout/playbar/left/index.tsx:31`）：本地文件在 B 站上压根没有页面可跳，
// 给一个点了没用的入口比不给更糟。
//
// ⚠️ **判据是「按下去什么也没发生」**：这一条断的不是「跳错了地方」，而是
// **压根没有那条路**。
func TestTheTitleIsNotClickableForALocalFile(t *testing.T) {
	tt, s := metaBarTester(t, app.State{
		HasTrack: true, Title: "本地的一首歌", Source: app.SourceLocal,
	}, 1200)

	var got string
	s.openExternal = func(url string) error { got = url; return nil }

	if err := tt.Click("本地的一首歌"); err != nil {
		t.Fatalf("点歌名报错了：%v（画面：%q）", err, tt.Texts())
	}
	tt.Frame()
	if got != "" {
		t.Errorf("本地文件的歌名点下去跳了 %q——本地文件在 B 站上没有页面可跳", got)
	}
}

// TestHoveringTheTitleUnderlinesIt 断**悬停时歌名有下划线**。
//
// 🔴 上游是 `hover:underline`（`layout/playbar/left/index.tsx:56`）：那是「这
// 一句点得动」的提示——没有它，用户不知道歌名能点。
//
// ⚠️ **判据是「墨迹变多了」**：下划线是**额外**的那一道，文字本身一个像素都不
// 变。量的是歌名那个盒子里的非底色像素（`inkPixels`）——指针不在上面时是 N，
// 指上去之后是 M > N。
//
// ⚠️ **不许断「某一行的颜色是某个数」**：那要写死一个平台相关的值。
func TestHoveringTheTitleUnderlinesIt(t *testing.T) {
	tt, s := metaBarTester(t, app.State{
		HasTrack: true, Title: "一首歌", Source: app.SourceOnline,
		BiliURL: "https://www.bilibili.com/video/BV1xx411c7mD",
	}, 1200)

	title, ok := tt.Find("一首歌")
	if !ok {
		t.Fatalf("画面上找不到歌名，文字有 %q", tt.Texts())
	}
	bg := themeColor(BuildTheme(s.app.SettingsSnapshot(), false).Background)
	before := inkPixels(tt.Image(), title, bg)

	tt.Move(title.X+title.W/2, title.Y+title.H/2)
	tt.Frame()

	after := inkPixels(tt.Image(), title, bg)
	if after <= before {
		t.Errorf("指针停在歌名上之后墨迹没有变多（%d → %d）——"+
			"悬停时那句字底下要长出一道下划线（上游的 `hover:underline`）", before, after)
	}
}
