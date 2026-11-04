// Package typo 是「深夜唱片行」的字体入口（DESIGN.md §0′）：宋体标题、等宽小字、
// 等宽数字、Eyebrow 小标。
//
// # 三种字体
//
//	Display  宋体 700（随包的思源宋体 / Noto Serif SC Bold 子集）  20 号及以上的标题
//	Mono     macOS SF Mono（没有就 Menlo）/ Windows Consolas        Eyebrow、kbd、音质角标
//	正文     系统默认（苹方 / 微软雅黑），这里不管                     其余一切
//
// # 用法
//
//	typo.Display(ui.Text(c, "推荐音乐"), scale.Font(c, scale.PageTitle))
//	typo.Mono(ui.Text(c, "⌘K")).FontSize(scale.Font(c, scale.Size11))
//	typo.Num(ui.Text(c, "4:29"))
//	typo.Eyebrow(c, "Album of the day")     // 11 号等宽 600、全大写、字距 0.2em、text-3
//
// # 随包字体
//
// 标题字体是 `fonts/NotoSerifSC-Bold-subset.ttf`：Noto Serif SC 可变字体
// （google/fonts 的 `ofl/notoserifsc`）固定在 wght=700，再用 fonttools 的
// `pyftsubset` 裁到 GB2312 全部字符 + ASCII + 常用标点 / 全角符号（7645 字，
// 约 3.2MB）。OFL 1.1 许可，原文在 `fonts/OFL.txt`，随包那一份在
// `resources/licenses/noto-serif-sc-OFL.txt`（`internal/licensing` 盯着）。
// 子集之外的字（生僻字）由系统按回退规则补字，字形会换成系统宋体 / 黑体。
//
// 注册在 `internal/ui.Run` 里、建窗口之前调 `Register`；`Display` 自己也会在第一次
// 用到时补一次（测试和预览不走 `Run`），所以忘了调也不会画成系统字体。
package typo

import (
	_ "embed"
	"runtime"
	"strings"
	"sync"

	"github.com/ShellMonster/PudMusic/internal/ui/widgets/leading"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/tok"
	"github.com/egoist/mygo/ui"
)

//go:embed fonts/NotoSerifSC-Bold-subset.ttf
var serifBold []byte

// DisplayFamily 是随包宋体注册进框架时用的族名。别处不要写这个字面量，用 `Display`。
const DisplayFamily = "PudMusic Serif"

var (
	registerOnce sync.Once
	registerErr  error
	monoFamily   string
)

// Register 把随包宋体注册进框架，并定下等宽字体那一族。可以重复调，只注册一次。
func Register() error {
	registerOnce.Do(func() {
		registerErr = ui.RegisterFont(serifBold, DisplayFamily)
		monoFamily = pickMono()
	})
	return registerErr
}

// pickMono 按平台挑等宽字体：macOS 先找 SF Mono（系统自带，但不是每台都暴露给
// 应用），找不到用 Menlo；Windows 用 Consolas。
func pickMono() string {
	switch runtime.GOOS {
	case "darwin":
		for _, f := range ui.FontFamilies() {
			if f.Name == "SF Mono" {
				return "SF Mono"
			}
		}
		return "Menlo"
	case "windows":
		return "Consolas"
	}
	return "monospace"
}

// Display 给文字套上宋体 700，字号 size（已经乘过系统缩放的那个数，一般是
// `scale.Font(c, scale.PageTitle)`）。原型里 20 号及以上的标题都走它。
func Display(e ui.Element, size float32) ui.Element {
	_ = Register()
	return e.Font(DisplayFamily).FontSize(size).FontWeight(700)
}

// DisplayNatural 是宋体（Noto Serif SC）一行字的自然高度和字号的比（实测 48 号 = 68.98，
// 约 1.437）。框架的 `LineHeight` / `FixedLineHeight` 只能把行高**加大**，不能压到自然高度
// 以下，而原型的大标题行高是 1.05–1.3（`.hero-title` 1.05、`.h-page` 1.15），所以大标题会比
// 原型高出一截。用 `DisplayTrim` 算出上下各该缩回多少，调用方用负外边距补回去。
const DisplayNatural = 1.437

// DisplayTrim 是 size 号宋体要按行高 lh 排时，上下各该用负外边距缩回的像素。
func DisplayTrim(size, lh float32) float32 {
	if lh >= DisplayNatural {
		return 0
	}
	return (DisplayNatural - lh) * size / 2
}

// Mono 给文字套上等宽字体（只用在 Eyebrow 小标、kbd 键帽、音质角标）。字号、字重
// 由调用方定。
func Mono(e ui.Element) ui.Element {
	_ = Register()
	return e.Font(monoFamily).FontFeatures("tnum")
}

// Num 给数字打开等宽数字（`tnum`）：时长、播放量、统计数字。字体不变（还是正文字体），
// 只是数字一样宽，进度条两端的读数不再跳。
func Num(e ui.Element) ui.Element {
	return e.FontFeatures("tnum")
}

// EyebrowSize 是 Eyebrow 的字号（原型 `--fs-11`）；EyebrowTracking 是字距（0.2em）。
const (
	EyebrowSize     float32 = 11
	EyebrowTracking float32 = EyebrowSize * 0.2
)

// Eyebrow 是页头 / 主视觉上面那一行小标（原型 `.eyebrow`）：11 号等宽、600、
// 全大写、字距 0.2em、第三级文字色。字距走框架的 `LetterSpacing`（每个字后面加
// 2.2 DIP），不用空格近似。
//
// 字号**不**乘系统缩放：它是装饰性小标，和图标一样跟着固定像素走（原型里它在
// 页头那一行的位置是按 11 排的）。
func Eyebrow(c *ui.Context, text string) ui.Element {
	return Mono(ui.Text(c, strings.ToUpper(text))).
		FontSize(EyebrowSize).FontWeight(600).LetterSpacing(EyebrowTracking).LineHeight(leading.Body).
		TextColor(tok.Of(c.Theme()).Text3).SingleLine()
}
