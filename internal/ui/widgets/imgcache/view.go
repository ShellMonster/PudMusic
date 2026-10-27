package imgcache

import (
	"hash/fnv"
	"unicode/utf8"

	"github.com/ShellMonster/Biu_Go/internal/ui/iconset"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/ink"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/shimmer"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/title"
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
	return ViewPlaceholderTitled(c, cache, url, "", w, h, radius, empty)
}

// ViewTitled 是 View 加上歌名：**没有封面地址**时画「歌名首字 + 主题色渐变」，而不是一枚
// 灰色图标（设计规范 §7.3）。有地址的三档（载入中 / 就绪 / 失败）和 View 完全一样。
func ViewTitled(c *ui.Context, cache *Cache, url, label string, w, h float32) ui.Element {
	return ViewPlaceholderTitled(c, cache, url, label, w, h, c.Theme().Radius, placeholderIcon)
}

// ViewPlaceholderTitled 是 ViewPlaceholder 加上歌名。label 为空就和 ViewPlaceholder 一样
// （画 empty 那枚图形）；不空且没有地址，就画首字渐变。
func ViewPlaceholderTitled(c *ui.Context, cache *Cache, url, label string, w, h, radius float32, empty *ui.SVG) ui.Element {
	return ui.Box(c).Size(w, h).Radius(radius).Clip().Children(func() {
		if url == "" && label != "" {
			drawGlyphCover(c, label, w, h, radius)
			return
		}
		drawCover(c, cache, url, w, h, radius, empty, Shape{})
	})
}

// States 画一张网络图的**有地址那三态**（正在加载 / 就绪 / 拉失败）。
//
// ⚠️ **它和 `View*` 那一族的分工**：那一族连「空地址」也管（画一枚图片图标或
// 歌名首字），而**头像、头图、缩略图**的「空地址」有自己的口径——头像没有图时
// 要一块和所在底面差得开的底（卡片上的头像尤其：占位圆和卡片同色就整个看不见，
// 见 `spaceAvatarWith`），头图没有图时要铺页面底色。所以这一档交回调用方，
// 这里只画有地址的那三态。
//
// 于是「四种状态怎么画」仍然只有一份（`drawCover`），而调用方只管自己那一种
// 「本来就没有」。
func States(c *ui.Context, cache *Cache, url string, w, h, radius float32, shape Shape) {
	drawCover(c, cache, url, w, h, radius, shape.emptyIcon(), shape)
}

// Loading 画「这一格还在等」那一档，**不经过缓存**。
//
// ⚠️ **它给的是「元信息还没回来」那种等**：调用方手上有一个**将来会是地址**
// 的字段，而那一刻它是空串（或者压根还没有）。传空串给 `States` 会被画成
// 「本来就没有」——两档的含义是相反的（一个是「等一等就有」，一个是「永远没有」）。
//
// ⚠️ **别拿一个假地址去骗缓存**：那种做法会让缓存里多一条永远失败的记录、
// 每开一次页面多发一次注定失败的请求，而且失败之后它落到**失败态**上
// （`isPermanent` 判它永久失败）——界面从「在等」变成「拉失败了」。
func Loading(c *ui.Context, w, h float32, shape Shape) ui.Element {
	return ui.Column(c).Size(w, h).Children(func() {
		drawSkeleton(c, w, h, shape.loadingGlyph())
	})
}

// shapeKind 是 `States` 那三态要用的形状参数。
type Shape struct {
	// Icon 是失败那一格中间那枚图形（nil = 图片图标）。
	//
	// ⚠️ **头像传人形、头图传图片图标**：同一屏里「一个人没有头像」和「一张
	// 头图拉不到」是两件事，图形一样的话用户分不出哪一格在说什么。
	Icon *ui.SVG
	// Loading 是加载中那一格的图形（nil = 音符）。
	//
	// ⚠️ **它是「正在加载」的记号，不是「这是什么」的记号**：一张封面正在加载 =
	// 一首歌，所以是音符；一张头像正在加载不等于一首歌，调这个字段换成合适的。
	Loading *ui.SVG
}

// emptyIcon 是失败那一档用的图形（nil 就是默认的图片图标）。
func (s Shape) emptyIcon() *ui.SVG {
	if s.Icon != nil {
		return s.Icon
	}
	return placeholderIcon
}

// loadingGlyph 是加载中那一档用的图形（nil 就是音符）。
func (s Shape) loadingGlyph() *ui.SVG {
	if s.Loading != nil {
		return s.Loading
	}
	return loadingIcon
}

// AvatarShape 是「头像」那一套形状：失败是**人形**（这一位没有头像，不是
// 「一张图坏了」），加载中是**人形**（还在拉他的脸）。
func AvatarShape() Shape {
	return Shape{Icon: avatarIcon, Loading: avatarIcon}
}

// avatarIcon 是头像那一套的图形（「一个人」）。
//
// ⚠️ 从 `iconset` 取而**不是**界面层的 `iconEmptyUser`：这一格住在
// `widgets/imgcache`，而界面层 import 这个包——反过来 import 是个环。
var avatarIcon = iconset.Must("User & Faces/user-line.svg")

// glyphRatio 是首字占封面较短边的比例。
const glyphRatio = 0.44

// glyphOf 取歌名的首字：先清洗（`【4K】晴天` 的首字是「晴」，不是「【」），再取第一个
// 字符；英文字母转大写；清洗后空了就用音符。
func glyphOf(label string) string {
	r, n := utf8.DecodeRuneInString(title.Clean(label).Title)
	if n == 0 || r == utf8.RuneError {
		return "♪"
	}
	if r >= 'a' && r <= 'z' {
		r -= 'a' - 'A'
	}
	return string(r)
}

// drawGlyphCover 画「首字 + 渐变」：渐变从 Background 往强调色推两档（每首歌按标题的哈希
// 在 22%~38% 之间取一档、角度在 120°~150° 之间取一个，于是一屏占位封面不是同一块色，
// 又都在主题的强调色相里）；字取强调色压深到 ≥ 4.5:1 的那一档。
func drawGlyphCover(c *ui.Context, label string, w, h, radius float32) {
	t := c.Theme()
	hs := fnv.New32a()
	_, _ = hs.Write([]byte(label))
	v := hs.Sum32()
	lo := 0.22 + float32(v%17)/100    // 0.22 ~ 0.38
	angle := 120 + float32((v/17)%31) // 120° ~ 150°
	from, to := t.Background.Mix(t.Accent, lo), t.Background.Mix(t.Accent, lo+0.18)
	text := ink.Ensure(t.Accent, from.Mix(to, 0.5), 4.5)
	ui.Column(c).Size(w, h).Gradient(from, to, angle).Border(1, t.Border).Radius(radius).Center().Children(func() {
		ui.Text(c, glyphOf(label)).FontSize(glyphRatio * min32(w, h)).FontWeight(600).
			TextColor(text).SingleLine()
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
// 🔴 **底色那一档在浅色下不够**（2026-10-27）：`Surface`（#f4f4f5）往 `Background`
// 混一半得到的近似 #fafafa，压在 #ffffff 的页面上**每通道只差 4**——48 像素的
// 小封面在浅色下依旧是一块看不出边界的白。所以那一档改成**前景色 8% 叠出来的
// 灰**（`shimmer.BaseAlpha`，约 #ebebeb），和骨架屏那一档**同一个配方**：同一屏
// 上下两块占位是同一个灰，用户不会以为它们是两种状态。
//
// ⚠️ **它扫光**（2026-10-27 改）：原先这里写着「框架没有现成的骨架屏动画」，
// 而 `widgets/shimmer` 就是那条动画。一屏封面全在等的时候，扫光让「在等」这件事
// 一眼可见，和列表首屏的骨架屏也是同一个节奏（同一个钟、同一条高光）。
// ⚠️ **别按「一屏几十张各自闪」去担心**：相位只由钟决定，于是它们**同步**扫过，
// 看起来是一道光，不是一堆各自闪的块（理由见 `shimmer` 的文件头）。
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

// skeletonMix 是「拉失败 / 没有地址」那两档的底色：从 `Surface` 往 `Background`
// 那头挪的比例（0 = 原色，1 = 全背景色）。
//
// ⚠️ **它只管那两档**（2026-10-27）：加载中那一档不再用它——那个混出来的色在
// 浅色下几乎就是白的（见上面那段）。加载中走 `shimmer` 的「前景色 8%」。
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
//
// shape 决定「加载中」和「拉失败」那两档中间摆哪枚图形（默认：音符 / 图片图标）。
// 「没有地址」那一档用的是 empty（调用方给的那一枚）。
func drawCover(c *ui.Context, cache *Cache, url string, w, h, radius float32, empty *ui.SVG, shape Shape) {
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
		// 🔴 **有图这一档也描一圈边**（票 #244 / UX 报告 §2 的 E2）。
		//
		// 原来四档里只有三档有边界：「正在加载」没有，「拉失败」「没有地址」有
		// （`drawPlaceholder` 的 `Border(1, t.Border)`）——**「图取到了、图本身是
		// 深色」那一档没有**。于是一张近黑的封面（`#12141a` 那一档）在深色底上
		// 和行的底色几乎一样，用户分不清那是「封面是黑的」还是「封面没加载出来」。
		//
		// ⚠️ **描边加在图片自己身上，不另建一个盒子**：列表里每一行都有一张封面，
		// 而多建一个元素就是每帧多一次分配（`internal/ui/framebudget_test.go`
		// 那一组量的是**每帧建了多少个对象**——队列抽屉那一档的上限只比基线多
		// 6 次，而一屏十来行封面就是十来次）。
		//
		// ⚠️ **描边是画在图片盒子里面那一圈**（框架的 border 在内容区之外、
		// 内容区之内那一圈），所以图片自己小 2 像素——那 2 像素就是这一圈边。
		// 圆角跟着外面那一层走（`radius`），于是这一圈边和占位那一档的边是同一档。
		ui.Image(c, st.Bitmap).Fit(ui.Cover).Size(w, h).
			Border(1, c.Theme().Border).Radius(radius)
	case st.Loading:
		drawSkeleton(c, w, h, shape.loadingGlyph())
	default:
		drawPlaceholder(c, w, h, 0, radius, shape.emptyIcon())
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
// ⚠️ **它扫光**（`widgets/shimmer`）：一屏封面全在等的时候，「在等」这件事
// 靠那道往前走的高光一眼可见；相位只由钟决定，所以一屏是**同一道光**扫过。
func drawSkeleton(c *ui.Context, w, h float32, glyph *ui.SVG) {
	t := c.Theme()
	size := placeholderIconRatio * min32(w, h)
	// ⚠️ **底色不能再是 Surface 往 Background 混**：浅色下那个混出来的色和页面
	// 白几乎一样（见上面 `skeletonMix` 那一段）。用前景色 8% 是骨架屏那一档的
	// 配方，两处于是同一个灰。
	base := t.Text.Alpha(shimmer.BaseAlpha)
	glow := t.Background.Alpha(shimmer.GlowAlpha)
	visible := shimmer.VisibleFlag()
	ui.Column(c).Size(w, h).
		Draw(func(p *ui.Painter, r ui.Rect) {
			// 不描边、不裁圆角：外层那个 `ViewPlaceholderTitled` 的 Box 已经
			// `Radius(radius).Clip()` 过一次，这里画到边就行（多画一圈圆角会把
			// 那一道边描在裁剪之后的直角上）。
			shimmer.Paint(p, r, 0, base, glow, visible)
		}).
		Center().Children(func() {
		// 再往背景色那边挪一档：它说的是「还没到」，不是「在这儿」。
		ui.Icon(c, glyph).FontSize(size).
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
