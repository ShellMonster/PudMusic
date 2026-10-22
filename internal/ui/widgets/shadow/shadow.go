// Package shadow 把上游那几档阴影的数值固定在一个地方。
//
// # 为什么单开一个包
//
// 上游那几档阴影来自**两个互不相干的地方**，而它们各自的值都散在好几处：
//
//   - **Tailwind 的 `shadow-*`**（上游 `src/` 里那些类）：`shadow-sm` / `md` /
//     `lg` / `xl` / `2xl`，颜色是 Tailwind 默认的那档黑（10%，`2xl` 是 25%）。
//   - **HeroUI 的 `shadow="sm|md|lg"`**（`--heroui-box-shadow-*`）：**另一套
//     数值**，而且**深浅两套不一样**——深色那套多一层 1 像素的白色**内**阴影，
//     浅色那套那一层是外阴影。
//
// 抄的时候最容易出的错是「同一档在两个文件里抄成两个数」，而那种错在画面上
// 只是「这一块看着比那一块平一点」。所以值只写在这里，调用处只说「要哪一档」。
//
// # 数值从哪来
//
//   - Tailwind v4 的那几档：`tailwindcss` 的 `theme.boxShadow`（报告 95 的
//     「03 · 颜色、状态与阴影对照」表已经换算过一遍，这里照它）。
//   - HeroUI 的那三档：`@heroui/theme@2.4.25` 的 `dist/plugin.js` 里的
//     `boxShadow`（浅色那一份和 `darkLayout` 里深色那一份，逐值抄的）。
//
// ⚠️ **深浅两套不要自己合并成一套**：上游深色下卡片和背景几乎同色（`content1`
// 与背景差 1~2 个色阶），**靠阴影分层**——深浅用同一个浓度的话，深色下那一圈
// 影压在深底上根本看不出来（实测 18% 黑压 `#18181b` 上量不出差别）。
package shadow

import "github.com/egoist/mygo/ui"

// Tailwind 的几档。**颜色是 Tailwind 默认的那档黑**（`sm` 是 5%、其余 10%、
// `2xl` 25%）——上游那些 `shadow-*` 后面没有跟 `shadow-black/…`，用的就是默认色。
//
// ⚠️ 每个函数都往 e 上**加**影子（`Element.Shadow` 是累加的），所以拿到的还是
// 同一个元素，可以继续链下去。
const (
	// tailwindBlack05 是 `shadow-sm` 的颜色：5% 黑。
	//
	// ⚠️ **它比其余几档淡一半**：Tailwind 的 `shadow-sm` 就是 5%，
	// 拿 10% 去填会得到**两倍浓度**的影（而这一档画出来的是最淡的一圈，
	// 差一倍一眼就看得出来）。
	tailwindBlack05 = 0.05
	// tailwindBlack10 是 Tailwind 默认的那档影子的颜色：10% 黑。
	tailwindBlack10 = 0.1
	// tailwindBlack25 是 `shadow-2xl` 的颜色：25% 黑。
	tailwindBlack25 = 0.25
)

// TailwindSM 是上游的 `shadow-sm`：`0 1px 2px 0 rgb(0 0 0/0.05)`。
func TailwindSM(e ui.Element) ui.Element {
	return e.Shadow(0, 1, 2, 0, ui.RGBA(0, 0, 0, tailwindBlack05))
}

// TailwindMD 是上游的 `shadow-md`：
// `0 4px 6px -1px rgb(0 0 0/0.1), 0 2px 4px -2px rgb(0 0 0/0.1)`。
//
// ⚠️ **是两层**：Tailwind 的 `shadow-md` 从 v3 起就是两层（外扩 + 内收各一层），
// 只抄一层的话那一块看着比上游「薄」一档。
func TailwindMD(e ui.Element) ui.Element {
	return e.Shadow(0, 4, 6, -1, ui.RGBA(0, 0, 0, tailwindBlack10)).
		Shadow(0, 2, 4, -2, ui.RGBA(0, 0, 0, tailwindBlack10))
}

// TailwindLG 是上游的 `shadow-lg`：
// `0 10px 15px -3px rgb(0 0 0/0.1), 0 4px 6px -4px rgb(0 0 0/0.1)`。
//
// ⚠️ 同样是**两层**。UP 空间那个 140 的头像带的就是这一档（上游
// `user-profile/space-info.tsx:89`）。
func TailwindLG(e ui.Element) ui.Element {
	return e.Shadow(0, 10, 15, -3, ui.RGBA(0, 0, 0, tailwindBlack10)).
		Shadow(0, 4, 6, -4, ui.RGBA(0, 0, 0, tailwindBlack10))
}

// TailwindXL 是上游的 `shadow-xl`：
// `0 20px 25px -5px rgb(0 0 0/0.1), 0 8px 10px -6px rgb(0 0 0/0.1)`。
func TailwindXL(e ui.Element) ui.Element {
	return e.Shadow(0, 20, 25, -5, ui.RGBA(0, 0, 0, tailwindBlack10)).
		Shadow(0, 8, 10, -6, ui.RGBA(0, 0, 0, tailwindBlack10))
}

// TailwindXLBlack30 是上游的 `shadow-xl shadow-black/30`——**回顶按钮那一枚**
// （上游 `components/scroll-container/index.tsx:82`）。
//
// ⚠️ **`shadow-black/30` 把两层的颜色都换成 30% 黑**，不是只换一层，也不是
// 「用默认色再叠一层 30% 黑」。上游那一枚之所以比别处浓，是因为它是一枚**浮在
// 内容上面**的按钮，底下那一层影就是它和内容之间的分界。
func TailwindXLBlack30(e ui.Element) ui.Element {
	return e.Shadow(0, 20, 25, -5, ui.RGBA(0, 0, 0, 0.3)).
		Shadow(0, 8, 10, -6, ui.RGBA(0, 0, 0, 0.3))
}

// Tailwind2XL 是上游的 `shadow-2xl`：`0 25px 50px -12px rgb(0 0 0/0.25)`。
//
// ⚠️ **只有一层，而且颜色是 25% 黑**（不是 10%）。网格卡片右下角那个主色圆播放钮
// 带的就是这一档（上游 `components/music-card/index.tsx:51`）。
func Tailwind2XL(e ui.Element) ui.Element {
	return e.Shadow(0, 25, 50, -12, ui.RGBA(0, 0, 0, tailwindBlack25))
}

// PlayBar 是播放条那一整套影：**外层那个 div 的 Tailwind `shadow-2xl` +
// 里面那张 Card 的 HeroUI `shadow="sm"`**。
//
// 上游那两层落在**同一个矩形**上（`layout/index.tsx:48` 那个 div 是
// `h-[88px] w-full`，里面 `playbar/index.tsx:24` 那张 Card 是 `h-full` 铺满它），
// 所以这里一层元素上就够。
//
// ⚠️ **加进去的先后看不出差别**（别拿它当保证）：MyGo 是「先画所有**外**影 →
// 再画底 → 最后画**内**影」（`mygo/ui/paint.go:246-262`），所以那道 1 像素的
// **内白边永远画在 `shadow-2xl` 之上**——两层反过来加，逐行像素**一个都不差**
// （实测过）。这里按上游的叠放次序写（Card 压着外层 div），只是为了看着像上游
// 那个结构，**不是**它对不对的原因。
//
// ⚠️ **两层是上游那个结构，不是两套随便挑的**：外层那档朝下洒 25、模糊 50，
// 于是它**在播放条下方**（窗口外，看不见），真正落在内容区那一侧的是 Card
// 自己那档 `shadow="sm"`（深色下 1 像素内白边，浅色下 1 像素外黑边）。
func PlayBar(e ui.Element, t *ui.Theme) ui.Element {
	return HeroSmall(Tailwind2XL(e), t)
}

// HeroSmall 是 HeroUI 的 `shadow="sm"`（`--heroui-box-shadow-small`）。
//
// 浅色：`0 0 5px 0 rgb(0 0 0/0.02), 0 2px 10px 0 rgb(0 0 0/0.06),
// 0 0 1px 0 rgb(0 0 0/0.3)`。
// 深色：`0 0 5px 0 rgb(0 0 0/0.05), 0 2px 10px 0 rgb(0 0 0/0.2),
// inset 0 0 1px 0 rgb(255 255 255/0.15)`。
//
// ⚠️ **深浅两套的第三层不一样**：浅色那层是 1 像素的**外**黑边（把卡片和底色
// 分开），深色那层是 1 像素的**内**白边（深底上黑边看不见，白边才看得出边界）。
// 抄成一样的话深色下那张卡片就是「糊在底上的一块」。
func HeroSmall(e ui.Element, t *ui.Theme) ui.Element {
	if t.Dark {
		return e.Shadow(0, 0, 5, 0, ui.RGBA(0, 0, 0, 0.05)).
			Shadow(0, 2, 10, 0, ui.RGBA(0, 0, 0, 0.2)).
			InsetShadow(0, 0, 1, 0, ui.RGBA(255, 255, 255, 0.15))
	}
	return e.Shadow(0, 0, 5, 0, ui.RGBA(0, 0, 0, 0.02)).
		Shadow(0, 2, 10, 0, ui.RGBA(0, 0, 0, 0.06)).
		Shadow(0, 0, 1, 0, ui.RGBA(0, 0, 0, 0.3))
}

// HeroMedium 是 HeroUI 的 `shadow="md"`（`--heroui-box-shadow-medium`）。
//
// 浅色：`0 0 15px 0 rgb(0 0 0/0.03), 0 2px 30px 0 rgb(0 0 0/0.08),
// 0 0 1px 0 rgb(0 0 0/0.3)`。
// 深色：`0 0 15px 0 rgb(0 0 0/0.06), 0 2px 30px 0 rgb(0 0 0/0.22),
// inset 0 0 1px 0 rgb(255 255 255/0.15)`。
//
// ⚠️ HeroUI 的 `Card` **默认就是这一档**（`@heroui/theme` 的 card 主题
// `defaultVariants.shadow = "md"`），所以上游那些没写 `shadow=` 的卡片
// （关注卡、队列抽屉）带的是它。
func HeroMedium(e ui.Element, t *ui.Theme) ui.Element {
	if t.Dark {
		return e.Shadow(0, 0, 15, 0, ui.RGBA(0, 0, 0, 0.06)).
			Shadow(0, 2, 30, 0, ui.RGBA(0, 0, 0, 0.22)).
			InsetShadow(0, 0, 1, 0, ui.RGBA(255, 255, 255, 0.15))
	}
	return e.Shadow(0, 0, 15, 0, ui.RGBA(0, 0, 0, 0.03)).
		Shadow(0, 2, 30, 0, ui.RGBA(0, 0, 0, 0.08)).
		Shadow(0, 0, 1, 0, ui.RGBA(0, 0, 0, 0.3))
}

// HeroInnerRim 是 HeroUI 那一档的**内**那一层：深色下是 1 像素的**白边**
// （`inset 0 0 1px 0 rgb(255 255 255/0.15)`）。
//
// ⚠️ **浅色那一档没有这一层可加**：它的第三层是 1 像素的**外**黑边
// （`0 0 1px 0 rgb(0 0 0/0.3)`）——落在盒子**外面**，不会被谁盖住，所以浅色下
// 这个函数什么也不做。
//
// ⚠️ **什么时候用这个**：那一档的影要落在**一个被别的控件铺满的盒子**上时——
// 外面那一圈看得见，而**内**那一层会被铺在上面的那个控件整个盖住，于是深色下
// 那道白边（深底上唯一看得见的那一层）就没了。设置里那块色卡就是这一种：那个
// 取色板控件（`ui.ColorWell`）铺满了整格。
func HeroInnerRim(e ui.Element, t *ui.Theme) ui.Element {
	if !t.Dark {
		return e
	}
	return e.InsetShadow(0, 0, 1, 0, ui.RGBA(255, 255, 255, 0.15))
}

// HeroLarge 是 HeroUI 的 `shadow="lg"`（`--heroui-box-shadow-large`）。
//
// 浅色：`0 0 30px 0 rgb(0 0 0/0.04), 0 30px 60px 0 rgb(0 0 0/0.12),
// 0 0 1px 0 rgb(0 0 0/0.3)`。
// 深色：`0 0 30px 0 rgb(0 0 0/0.07), 0 30px 60px 0 rgb(0 0 0/0.26),
// inset 0 0 1px 0 rgb(255 255 255/0.15)`。
func HeroLarge(e ui.Element, t *ui.Theme) ui.Element {
	if t.Dark {
		return e.Shadow(0, 0, 30, 0, ui.RGBA(0, 0, 0, 0.07)).
			Shadow(0, 30, 60, 0, ui.RGBA(0, 0, 0, 0.26)).
			InsetShadow(0, 0, 1, 0, ui.RGBA(255, 255, 255, 0.15))
	}
	return e.Shadow(0, 0, 30, 0, ui.RGBA(0, 0, 0, 0.04)).
		Shadow(0, 30, 60, 0, ui.RGBA(0, 0, 0, 0.12)).
		Shadow(0, 0, 1, 0, ui.RGBA(0, 0, 0, 0.3))
}
