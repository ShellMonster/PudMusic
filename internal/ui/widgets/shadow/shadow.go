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

// TailwindLG 是上游的 `shadow-lg`：
// `0 10px 15px -3px rgb(0 0 0/0.1), 0 4px 6px -4px rgb(0 0 0/0.1)`。
//
// ⚠️ 同样是**两层**。UP 空间那个 140 的头像带的就是这一档（上游
// `user-profile/space-info.tsx:89`）。
func TailwindLG(e ui.Element) ui.Element {
	return e.Shadow(0, 10, 15, -3, ui.RGBA(0, 0, 0, tailwindBlack10)).
		Shadow(0, 4, 6, -4, ui.RGBA(0, 0, 0, tailwindBlack10))
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

// Popover 是**浮层面板**那一圈影：下拉、联想、日期面板、旋钮浮层。
//
// 浅色 18% 黑、深色 35% 黑（`ui.Color{A: 46}` / `{A: 90}`）。
//
// 浅色那一档是框架 `stylePanel` 的数（`mygo/ui/widgets.go:626`：18% 黑，
// 模糊 20）——我们自拼的那些浮层照它来，两边挨着的时候才一样重。
//
// ⚠️ **深色那一档要更浓**：同一个 18% 压在 `#18181b` 上看不出边界，面板和底下
// 的内容糊成一片。
func Popover(e ui.Element, t *ui.Theme) ui.Element {
	if t.Dark {
		return e.Shadow(0, 6, 20, 0, ui.Color{A: 90})
	}
	return e.Shadow(0, 6, 20, 0, ui.Color{A: 46})
}

// Tooltip 是**提示**那一圈影：2 像素下移、8 像素模糊、20% 黑。
//
// 深浅同值——提示本来就小，一圈淡影够了；深色下它压的是自己的深底（提示的底
// 是前景色），边界靠底色自己那一边。
func Tooltip(e ui.Element, t *ui.Theme) ui.Element {
	return e.Shadow(0, 2, 8, 0, ui.RGBA(0, 0, 0, 0.2))
}

// Modal 是**确认框**那一圈影：10 像素下移、30 像素模糊、30% 黑。
//
// 深浅同值：这一块底下压着 40% 的黑幕（`confirmmodal.go` 的 backdrop），幕本身
// 已经把周围压暗了，面板这一圈影只需要把面板从幕上「抬起来」。
func Modal(e ui.Element, t *ui.Theme) ui.Element {
	return e.Shadow(0, 10, 30, 0, ui.RGBA(0, 0, 0, 0.3))
}

// Sheet 是**贴在背景上的那一块面板**（满屏播放器里那个分集面板）那一圈影。
//
// 8 像素下移、24 像素模糊、12% 黑。
//
// ⚠️ **偏移和模糊跟着主题的间距走**（`t.Space(2)` / `t.Space(6)`）：那一块是
// 毛玻璃（底下还有一层模糊的封面影），影再重就糊成一片了。
//
// ⚠️ **这一档最贵**：模糊 24 的一圈影是**逐像素画**的，一块 280 宽的面板要
// 多画约 280×24×2 ≈ 13 000 像素。软件渲染回退下这一笔是实打实的，所以
// **别为了「更浮起来」把它调重**——要调重是另一票的事（而且要一起量帧耗时）。
func Sheet(e ui.Element, t *ui.Theme) ui.Element {
	return e.Shadow(0, t.Space(2), t.Space(6), 0, ui.RGBA(0, 0, 0, 0.12))
}

// ─────────────────────────────────────────────────────────────────────────────
// 已删的档
//
// 票 #239 删掉的三档（它们**一个调用点都没有**，而「定义了没人用的档比没有档
// 更坏」——下一个人会以为「这一档可以用」）：
//
//   - `TailwindSM`（`shadow-sm`）：上游没有一处用它。
//   - `TailwindXL`（`shadow-xl`）：上游只有回顶按钮那一枚用它，而那一枚是
//     `shadow-black/30`（`TailwindXLBlack30`，还在）。
//   - `HeroLarge`（`shadow="lg"`）：上游的分集那个 Popover 是这一档，但我们
//     那一块用的是自己挑的一档更淡的影（`Sheet`）——毛玻璃底下还有一层模糊的
//     封面影，再压一圈 30 像素的影就糊了。
//
// ⚠️ **上游那几个档的数在 DESIGN.md §四·五 的表里还留着**——那是**文档**（记着
// 上游是哪些值），不是代码里有几档可调。
// ─────────────────────────────────────────────────────────────────────────────

// FullCover 是满屏播放器那张封面底下的影：一层大而深的落影（偏移 24、模糊 48、
// 收 8、50% 黑），让封面从糊过的背景里「浮」起来。
//
// 设计稿 docs/design-mockups/fullplayer.png。背景是压暗过的封面，所以影要比
// 页内卡片重得多才看得见；只有一层，不叠内影。
func FullCover(e ui.Element) ui.Element {
	return e.Shadow(0, 24, 48, -8, ui.RGBA(0, 0, 0, 0.5))
}
