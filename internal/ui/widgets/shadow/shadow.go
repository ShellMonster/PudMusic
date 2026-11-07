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
//
// # 这个包里只有**元素级**的影
//
// `Element.Shadow(...)` / `Element.InsetShadow(...)` 那一套（值在这里，调用处
// 只说「要哪一档」）。
//
// ⚠️ **画笔级的影（`p.Shadow(rect, …)`，七个参数）不走这里**，而且**不是漏了**：
// 那一套画的是**一枚控件自己的影**——旋钮的影跟着旋钮的半径、跟着它按下去几分
// 变浓（`internal/ui/knobslider.go`）、开关那颗旋钮的影跟着开关的直径
// （`internal/ui/static_switch.go`）。那些值不是「哪一档」，是**跟着那个控件
// 算出来的一个数**，放进这里就得把控件的参数也搬进来，那这个包就变成第二个
// `internal/ui` 了。值仍写在**画它的那一处**，理由和上面这一段一样，写在这里。
//
// # 死档不许留着
//
// ⚠️ **这个包里每一档都必须有调用点**。定义了没人用的档比没有档更坏：下一个人
// 会以为「这一档可以用」，照着它抄一遍，而它画出来的东西上游从来没有过。
// 没有调用点的档**删掉**（票 #239 把 `TailwindSM` / `TailwindXL` / `HeroLarge`
// 三档删了）。
package shadow

import "github.com/egoist/mygo/ui"

// Tailwind 的几档。**颜色是 Tailwind 默认的那档黑**（其余 10%、`2xl` 25%）——
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

// TailwindMD 是上游的 `shadow-md`：
// `0 4px 6px -1px rgb(0 0 0/0.1), 0 2px 4px -2px rgb(0 0 0/0.1)`。
//
// ⚠️ **是两层**：Tailwind 的 `shadow-md` 从 v3 起就是两层（外扩 + 内收各一层），
// 只抄一层的话那一块看着比上游「薄」一档。
func TailwindMD(e ui.Element) ui.Element {
	return e.Shadow(0, 4, 6, -1, ui.RGBA(0, 0, 0, tailwindBlack10)).
		Shadow(0, 2, 4, -2, ui.RGBA(0, 0, 0, tailwindBlack10))
}

// Tailwind2XL 是上游的 `shadow-2xl`：`0 25px 50px -12px rgb(0 0 0/0.25)`。
//
// ⚠️ **只有一层，而且颜色是 25% 黑**（不是 10%）。网格卡片右下角那个主色圆播放钮
// 带的就是这一档（上游 `components/music-card/index.tsx:51`）。
func Tailwind2XL(e ui.Element) ui.Element {
	return e.Shadow(0, 25, 50, -12, ui.RGBA(0, 0, 0, tailwindBlack25))
}

// ─────────────────────────────────────────────────────────────────────────────
// 我们自己挑的那几档面板影
//
// ⚠️ **下面四档不是上游的数**：上游有对应物的是浮层（`shadow-2xl`）、提示和
// 对话框（HeroUI 的 `<Tooltip>` / `<Modal>`），但**我们没照抄**——DESIGN.md
// §四·五 写着「我们那几处的数是**自己挑的，不是上游那一档**，所以浮层
// 「浮起来」的分界**和上游还不一样**」。
//
// 这一票（#239）只做**归档**：把那几个写在调用处的数搬进这个包，**数值一个
// 不改**（画面必须和改之前逐像素相同）。「和上游对齐」是另一票的事。
// ─────────────────────────────────────────────────────────────────────────────

// Pop 是「深夜唱片行」的 `--shadow-pop`（原型 index.html 89 / 111 行）：弹层、
// 抽屉、对话框、Toast、批量操作条、回到顶部、搜索联想——**所有浮起来的面板**
// 都是这一档。
//
//	深色  0 24px 64px rgba(0,0,0,.55), 0 2px 8px rgba(0,0,0,.4)
//	浅色  0 24px 64px rgba(20,22,26,.18), 0 2px 8px rgba(20,22,26,.08)
//
// 用法：`panel = shadow.Pop(panel, t)`，放在底色、圆角之后。
func Pop(e ui.Element, t *ui.Theme) ui.Element {
	if t.Dark {
		return e.Shadow(0, 24, 64, 0, ui.RGBA(0, 0, 0, 0.55)).
			Shadow(0, 2, 8, 0, ui.RGBA(0, 0, 0, 0.4))
	}
	return e.Shadow(0, 24, 64, 0, ui.RGBA(20, 22, 26, 0.18)).
		Shadow(0, 2, 8, 0, ui.RGBA(20, 22, 26, 0.08))
}

// LargeCover 是「深夜唱片行」的 `--shadow-cover`（原型 index.html 89 / 112 行，
// `.cover.lg`）：「今日一张」那张大封面。
//
//	深色  0 24px 48px rgba(0,0,0,.5)
//	浅色  0 20px 40px rgba(20,22,26,.22)
func LargeCover(e ui.Element, t *ui.Theme) ui.Element {
	if t.Dark {
		return e.Shadow(0, 24, 48, 0, ui.RGBA(0, 0, 0, 0.5))
	}
	return e.Shadow(0, 20, 40, 0, ui.RGBA(20, 22, 26, 0.22))
}

// CoverPlay 是卡片封面上那枚悬停播放键（`.cover-play`）的影：0 8px 24px 35% 黑。
func CoverPlay(e ui.Element) ui.Element {
	return e.Shadow(0, 8, 24, 0, ui.RGBA(0, 0, 0, 0.35))
}

// Popover 是**浮层面板**那一圈影：下拉、联想、日期面板、旋钮浮层。
// 2026-11 起就是 `Pop`（§0′：原型的弹层都是 `--shadow-pop`）。
func Popover(e ui.Element, t *ui.Theme) ui.Element { return Pop(e, t) }

// Tooltip 是**提示**那一圈影：2 像素下移、8 像素模糊、20% 黑。
//
// 深浅同值——提示本来就小，一圈淡影够了；深色下它压的是自己的深底（提示的底
// 是前景色），边界靠底色自己那一边。
func Tooltip(e ui.Element, t *ui.Theme) ui.Element {
	return e.Shadow(0, 2, 8, 0, ui.RGBA(0, 0, 0, 0.2))
}

// Modal 是**确认框 / 卡片浮层**那一圈影。2026-11 起就是 `Pop`（原型 `dialog.dlg`
// 用的是 `--shadow-pop`）。
func Modal(e ui.Element, t *ui.Theme) ui.Element { return Pop(e, t) }

// ─────────────────────────────────────────────────────────────────────────────
// 已删的档
//
// 票 #239 删掉的三档 + 这一票删掉的 `CurtainAxis`（它们**一个调用点都没有**，而
// 「定义了没人用的档比没有档更坏」——下一个人会以为「这一档可以用」）：
//
//   - `TailwindSM`（`shadow-sm`）：上游没有一处用它。
//   - `TailwindXL`（`shadow-xl`）：上游只有回顶按钮那一枚用它，而那一枚是
//     `shadow-black/30`（回顶按钮现在走 `shadow.Pop`，这一档也删了）。
//   - `HeroLarge`（`shadow="lg"`）：上游的分集那个 Popover 是这一档，但我们
//     那一块用的是自己挑的一档更淡的影（`Sheet`）——毛玻璃底下还有一层模糊的
//     封面影，再压一圈 30 像素的影就糊了。
//   - `Sheet`：满屏播放器里那个分集面板（毛玻璃）的影；「深夜唱片行」§0′.T4 删掉了那个面板，这一档
//     随之没有调用点。
//   - `CurtainAxis`：歌词卷帘下轴（地轴）投在纸上那道影。歌词板改成独立的
//     圆角板之后，轴这个元素就没了，这一档没人调。
//
// ⚠️ **上游那几个档的数在 DESIGN.md §四·五 的表里还留着**——那是**文档**（记着
// 上游是哪些值），不是代码里有几档可调。
// ─────────────────────────────────────────────────────────────────────────────

// FullCover 是满屏播放器那张封面底下的影：原型 `.full-cover { box-shadow: 0 32px 80px
// rgba(0,0,0,.6) }`（偏移 32、模糊 80、不收、60% 黑），让封面从糊过的背景里「浮」起来。
//
// 设计稿 docs/design-mockups/fullplayer.png。背景是压暗过的封面，所以影要比
// 页内卡片重得多才看得见；只有一层，不叠内影。
func FullCover(e ui.Element) ui.Element {
	return e.Shadow(0, 32, 80, 0, ui.RGBA(0, 0, 0, 0.6))
}
