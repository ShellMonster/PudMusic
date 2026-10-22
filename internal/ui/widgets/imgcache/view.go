package imgcache

import (
	"github.com/ShellMonster/Biu_Go/internal/ui/iconset"
	"github.com/egoist/mygo/ui"
)

// View 画一张网络图：好了画图，没好画占位，失败画失败态。
//
// 这是缓存唯一的界面出口，所有要显示网络图的地方都走它——三种状态怎么画、
// 要占多大地方，只在这里定一次。
//
// ⚠️ 调用方必须先用 Cache.AddWake 注册一路唤醒，否则图下完之后界面会永远
// 停在占位（实测：只投递不唤醒 = 0 帧刷新）。Shell.New 里已经注册了。
func View(c *ui.Context, cache *Cache, url string, w, h float32) ui.Element {
	// 圆角跟着主题走，不写死数字：主题的 Radius 来自设置里的
	// app.Settings.BorderRadius（ui.BuildTheme 落到 t.Radius），用户在设置页调
	// 圆角时封面跟着变。
	//
	// ⚠️ **上游那个 `radius="md"` 是固定的 6，不是跟着主题的**（Tailwind 的
	// `md` 就是 6）——我们这一档跟主题，是为了「用户在设置里调圆角，封面跟着变」
	// 这件事**我们自己的**行为。上游那一个固定 6 的例子在下面（`ViewRadius`）。
	return ViewRadius(c, cache, url, w, h, c.Theme().Radius)
}

// ViewRadius 是 View 加上「圆角」。
//
// ⚠️ **为什么要有这一档**：上游播放条上那张封面是 `radius="md"`——**Tailwind 的
// `md` 是固定的 6**（`layout/playbar/left/index.tsx:31-41` 的
// `<Image radius="md" …>`），而**我们全局那个圆角令牌是 8**（`radius.default`
// 是**令牌**，用户在设置里能调它，改了它整个界面跟着变）。
//
// 所以只有**播放条上那一张封面**用 6，其余的封面照旧跟主题走（见
// `internal/ui/playbar.go` 的 `coverRadius`）。
func ViewRadius(c *ui.Context, cache *Cache, url string, w, h, radius float32) ui.Element {
	return ViewPlaceholder(c, cache, url, w, h, radius, placeholderIcon)
}

// ViewPlaceholder 是 View 加上「**压根没有地址**那一格摆哪一枚图形」。
//
// ⚠️ **上游那是 `emptyPlaceholder` 一个 prop**（`components/image/index.tsx:31`：
// `{emptyPlaceholder || <RiFileImageLine size="40%" …/>}`）：**播放条那一张传的是
// `<RiMusic2Line />`**（`layout/playbar/left/index.tsx:40` 的
// `emptyPlaceholder={<RiMusic2Line />}`），别处传的是默认的那张图片图标。
//
// 所以我们照它做成一个参数，而**不是**把全局那一格都换成音乐符号——那样
// 列表行和网格里的空封面就都变成音符了，而上游那边它们是图片图标。
//
// ⚠️ **它只管「压根没有地址」那一档**：「拉失败」那一档画的是**图片图标**
// （`drawCover` 的 `default` 那一支），不跟着这个参数走——那是我们比上游多
// 分出来的一档（上游失败和空地址是同一个分支）。
func ViewPlaceholder(c *ui.Context, cache *Cache, url string, w, h, radius float32, empty *ui.SVG) ui.Element {
	return ui.Box(c).Size(w, h).Radius(radius).Clip().Children(func() {
		drawCover(c, cache, url, w, h, radius, empty)
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// 占位那一格
//
// ⚠️ **三种状态必须分得开**，而这一条是这一票的核心（issue 126：「我分得清
// 『还没加载』和『加载失败』」）。改之前是一块纯色 + 一行 11px 的字：正在加载
// 写「载入中」、失败写「封面加载失败」——一屏里坏三张封面就是三个写着字的灰块，
// 而「还没加载」和「加载失败」在界面上只差三个字，扫一眼分不出来。
//
// 现在按上游 `components/image/index.tsx:22-32` 分成三样：
//
//	正在加载    一块浅色 + **一枚淡的音符**，**没有边框、没有字**
//	拉失败了    描边框 + 40% 大小的图片图标，底色是 Surface 原色（较深）
//	没有地址    描边框 + 一枚音符（**图形由调用方给**，上游的 emptyPlaceholder），
//	            底色浅一档（更靠背景色）
//
// 那一档底色的深浅是这么定的：**「本该有而没拿到」比「本来就没有」更该显眼**
// ——后者压根没人期待，描边框和那枚图标够了，底色再抢眼就是白抢。
//
// 描边框是上游那个 `border-content2`，图标是上游那个 40%：
//
//   - **边框是「这一格最后会是一张图，但现在拿不到」的标记**，所以骨架屏
//     连它都不画——先给一圈线再撤掉会让那一格闪一下。
//   - **图标大小跟着封面走**（40% 边长），不是写死的像素：最小的一档封面
//     只有 32 像素，写死 14 在那儿就占了将近一半；放大到满屏那张又小得看不见。
//
// 🔴 **「正在加载」那一档要画得出东西**（票 #222 / 报告 §5.3）：它原来是一块
// **纯色**，在浅色主题下和背景几乎一样——一屏里几十张封面正在加载，用户看到的是
// 一排空白，**分不出「还在加载」和「压根没有图」**。现在那一档压一枚**更淡的
// 音符**：淡到「这是占位」而不是「这是一首歌」，但看得见。
//
// ⚠️ **它不闪**（上游那个骨架屏会闪）：框架没有现成的骨架屏动画，而闪起来在
// 一屏几十张封面时是灾难（报告 §5.3 的建议②：保留静态）。
//
// ⚠️ **一个字都不写**：上游那一格也没有文字。写字的代价是它在 32 像素那一档
// 会换行，而换行之后那行字压到封面上，看起来像叠了一层说明。要说的话在它
// 外面那一块（列表行 / 卡片）说。
// ─────────────────────────────────────────────────────────────────────────────

// placeholderIconRatio 是图片图标占封面**较短那条边**的比例，抄上游的 `40%`。
//
// 取短边而不是长边：封面是方的（列表 48、网格那格接近正方），而取长边的话在
// 一张被压扁的封面上（收藏夹头图那种宽扁的）那枚图标会大到出格。
const placeholderIconRatio = 0.4

// skeletonMix 是骨架屏那一块的底色：从 `Surface` 往 `Background` 那头挪的
// 比例（0 = 原色，1 = 全背景色）。
//
// 往背景色挪是为了让它在**浅色底下也看得出是一块占位**——`Surface` 和
// `Background` 在浅色主题里差得很小，原样画出来那一格和一块空白没区别。
// 深色主题里两个色本来就分得开，往背景色挪反而更淡——但「更深/更浅」在一块
// 没有边框也没有图形的方块上是看不出来的，骨架屏靠的是「不是白的」这一层。
const skeletonMix = 0.5

// loadingIconFade 是「正在加载」那一格里那枚音符往背景色那边挪的比例。
//
// ⚠️ **它比「没有地址」那一档的图形更淡，但看得见**：两档画的是同一枚音符，
// 靠深浅分开（「还在加载」是临时状态，「压根没有」是长期状态——后者更该显眼
// 一点）。
//
// ⚠️ **别调得太淡**：0.45 那一档在浅色主题下几乎看不见（实测出图：那枚音符是
// 一团比背景亮不了多少的灰），于是那一格又变回「一块空白」——那正是这一档要
// 改的那件事。0.25 是「看得见、但明显比旁边那枚淡」的那一档。
const loadingIconFade = 0.25

// emptyMix 是「压根没有地址」那一格的底色：比失败那一档**浅**一点（往背景色
// 那边挪）。
//
// 两个都是「拿不到」，可一个是**本来就没有**（这一页压根不给封面）、一个是
// **本该有**（B 站给了地址，没拉到）。分得开的**主要**是边框和图标，底色这一
// 档只是补一层意思，所以它只挪一点点。
const emptyMix = 0.25

// placeholderIcon 是封面那一格里的图片图标。
//
// ⚠️ 它从 `iconset` 取而**不是**从界面层拿常量：这一格住在 `widgets/imgcache`，
// 而界面层 import 这个包——反过来 import 是个环（见 `iconset` 文件头）。界面层
// 那个常量 `iconCoverPlaceholder` 指的就是同一个文件。
var placeholderIcon = iconset.Must("Media/image-line.svg")

// loadingIcon 是「正在加载」那一格压着的那枚音符。
//
// ⚠️ **它是 `Media/music-2-line.svg`**（上游的 `RiMusic2Line`）：「正在加载一
// 首歌的封面」这句话，用一枚音符说正好；而图片图标（`placeholderIcon`）是
// 「拉失败了」那一档的标记——同一个图形出现在两档里，用户就分不出这两档了。
//
// ⚠️ 它同样从 `iconset` 取，理由和 `placeholderIcon` 一样（见上面那段）。
var loadingIcon = iconset.Must("Media/music-2-line.svg")

// drawCover 是 View 里面的那一支，分成四种情形。
func drawCover(c *ui.Context, cache *Cache, url string, w, h, radius float32, empty *ui.SVG) {
	// 没有地址：压根没有东西可显示，不是「加载失败」——两者画同一张（上游也是
	// 同一个分支），但底色浅一档（`emptyMix` 往背景色那边挪），扫一眼就知道
	// 这一格是空的而不是坏的。
	//
	// ⚠️ **图形由调用方给**（上游的 `emptyPlaceholder`）：播放条那一张是音符，
	// 别处是图片图标（见 `ViewPlaceholder`）。
	if url == "" {
		drawPlaceholder(c, w, h, emptyMix, radius, empty)
		return
	}

	switch st := cache.Get(url); {
	case st.Ready():
		ui.Image(c, st.Bitmap).Fit(ui.Cover).Size(w, h)
	case st.Loading:
		drawSkeleton(c, w, h)
	default:
		drawPlaceholder(c, w, h, 0, radius, placeholderIcon)
	}
}

// drawSkeleton 画「正在加载」那一格：一块浅色 + **一枚更淡的音符**，**没有边框**。
//
// 🔴 **它要画得出东西**（票 #222 / 报告 §5.3）：原来是一块纯色，在浅色主题下
// 和背景几乎一样——一屏里几十张封面正在加载，用户看到的是一排空白，**分不出
// 「还在加载」和「压根没有图」**。
//
// ⚠️ **那枚音符比「没有地址」那一档的更淡**：淡到「这是占位」而不是「这是一
// 首歌」，但看得见。两档靠**深浅**分开（边框是「拿不到」的标记，骨架屏连它都
// 不画——见上面那段三态的说明）。
//
// ⚠️ **它不闪**：上游那个骨架屏会闪，而框架没有现成的骨架屏动画（见上面那段）。
func drawSkeleton(c *ui.Context, w, h float32) {
	t := c.Theme()
	size := placeholderIconRatio * min32(w, h)
	ui.Column(c).Size(w, h).Background(t.Surface.Mix(t.Background, skeletonMix)).
		Center().Children(func() {
		// 再往背景色那边挪一档：它说的是「还没到」，不是「在这儿」。
		ui.Icon(c, loadingIcon).FontSize(size).
			TextColor(t.TextMuted.Mix(t.Background, loadingIconFade))
	})
}

// drawPlaceholder 画「拿不到 / 没有」那一格：描边框 + 一枚按封面缩放的图形。
//
// mix 是底色从 `Surface` 往 `Background` 挪的比例（0 = 原色）。
//
// icon 是那一格中间那枚图形（上游的 `emptyPlaceholder`，或者默认的图片图标）。
//
// ⚠️ **圆角跟着外面那一层走**（`ViewRadius` 给的那一档）：外层已经裁过一次，
// 这里再来一圈描边，那一圈描边才落在圆角上而不是先被裁成直角。
func drawPlaceholder(c *ui.Context, w, h, mix, radius float32, icon *ui.SVG) {
	t := c.Theme()
	size := placeholderIconRatio * min32(w, h)
	ui.Column(c).Size(w, h).Background(t.Surface.Mix(t.Background, mix)).
		// 边框跟着主题的 `Border`：上游那一圈是 `border-content2`，说的是同一件
		// 事——「这一格是控件的边」。用户换主题色时它跟着变。
		Border(1, t.Border).
		// 圆角和外面那个 Clip 对上：外层已经裁过一次，这里再来一圈，
		// 那一圈描边才落在圆角上而不是先被裁成直角。
		Radius(radius).Center().Children(func() {
		// 次要色：它在说「这儿没有图」，不是「这里有话要说」——和空状态
		// 那一格里的插画同一档。
		ui.Icon(c, icon).FontSize(size).TextColor(t.TextMuted)
	})
}

// min32 取两个里小的那个。
//
// ⚠️ 自己写而不用 `math.Min` 是因为这里是 float32 而 math.Min 是 float64，
// 每次封面都多一对装箱分配——而这一格是列表 / 网格里**每一行**都要画的。
func min32(a, b float32) float32 {
	if a < b {
		return a
	}
	return b
}
