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
	// 圆角跟着主题走，不写死数字：Biu 那边的 radius="md" 也是跟着主题的，
	// 不是常量。主题的 Radius 来自设置里的 app.Settings.BorderRadius
	// （ui.BuildTheme 落到 t.Radius），用户在设置页调圆角时封面跟着变。
	return ui.Box(c).Size(w, h).Radius(c.Theme().Radius).Clip().Children(func() {
		drawCover(c, cache, url, w, h)
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
//	正在加载    骨架屏：一块浅色，**没有边框、没有图形、没有字**
//	没有地址    描边框 + 40% 大小的图片图标
//	拉失败了    同上，底色深一档
//
// 描边框是上游那个 `border-content2`，图标是上游那个 40%：
//
//   - **边框是「这一格最后会是一张图，但现在拿不到」的标记**，所以骨架屏
//     连它都不画——先给一圈线再撤掉会让那一格闪一下。
//   - **图标大小跟着封面走**（40% 边长），不是写死的像素：最小的一档封面
//     只有 32 像素，写死 14 在那儿就占了将近一半；放大到满屏那张又小得看不见。
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

// emptyMix 是「压根没有地址」那一格的底色：比失败那一档再深一点。
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

// drawCover 是 View 里面的那一支，分成四种情形。
func drawCover(c *ui.Context, cache *Cache, url string, w, h float32) {
	// 没有地址：压根没有东西可显示，不是「加载失败」——两者画同一张（上游也是
	// 同一个分支），但底色深一档，扫一眼就知道这一格是空的而不是坏的。
	if url == "" {
		drawPlaceholder(c, w, h, emptyMix)
		return
	}

	switch st := cache.Get(url); {
	case st.Ready():
		ui.Image(c, st.Bitmap).Fit(ui.Cover).Size(w, h)
	case st.Loading:
		drawSkeleton(c, w, h)
	default:
		drawPlaceholder(c, w, h, 0)
	}
}

// drawSkeleton 画「正在加载」那一格：一块浅色，**没有边框**。
//
// 边框和图标都留给「拿不到」那一支——见上面那段三态的说明。
func drawSkeleton(c *ui.Context, w, h float32) {
	t := c.Theme()
	ui.Column(c).Size(w, h).Background(t.Surface.Mix(t.Background, skeletonMix))
}

// drawPlaceholder 画「拿不到 / 没有」那一格：描边框 + 一枚按封面缩放的图片图标。
//
// mix 是底色从 `Surface` 往 `Background` 挪的比例（0 = 原色）。
func drawPlaceholder(c *ui.Context, w, h, mix float32) {
	t := c.Theme()
	size := placeholderIconRatio * min32(w, h)
	ui.Column(c).Size(w, h).Background(t.Surface.Mix(t.Background, mix)).
		// 边框跟着主题的 `Border`：上游那一圈是 `border-content2`，说的是同一件
		// 事——「这一格是控件的边」。用户换主题色时它跟着变。
		Border(1, t.Border).
		// 圆角和外面那个 Clip 对上：外层已经按主题圆角裁过一次，这里再来一圈，
		// 那一圈描边才落在圆角上而不是先被裁成直角。
		Radius(t.Radius).Center().Children(func() {
			// 次要色：它在说「这儿没有图」，不是「这里有话要说」——和空状态
			// 那一格里的插画同一档。
			ui.Icon(c, placeholderIcon).FontSize(size).TextColor(t.TextMuted)
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
