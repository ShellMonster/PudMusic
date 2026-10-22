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

// Tailwind 的几档。**颜色是 Tailwind 默认的那档黑**（10%，`2xl` 是 25%）——
// 上游那些 `shadow-*` 后面没有跟 `shadow-black/…`，用的就是默认色。
//
// ⚠️ 每个函数都往 e 上**加**影子（`Element.Shadow` 是累加的），所以拿到的还是
// 同一个元素，可以继续链下去。
const (
	// tailwindBlack10 是 Tailwind 默认的那档影子的颜色：10% 黑。
	tailwindBlack10 = 0.1
	// tailwindBlack25 是 `shadow-2xl` 的颜色：25% 黑。
	tailwindBlack25 = 0.25
)

// TailwindSM 是上游的 `shadow-sm`：`0 1px 2px 0 rgb(0 0 0/0.05)`。
func TailwindSM(e ui.Element) ui.Element {
	return e.Shadow(0, 1, 2, 0, ui.RGBA(0, 0, 0, tailwindBlack10))
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
