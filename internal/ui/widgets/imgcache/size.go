package imgcache

import (
	"strconv"
	"strings"
)

// 「这一格该向图床要多大的图」——按显示尺寸算，不是按固定档位。
//
// # 为什么不能一刀切
//
// 界面上用到封面的地方尺寸差十倍：侧栏收藏 32 DIP、列表行 48、播放条 52、
// 首页推荐 96、歌单页 200×168、满屏 384×288。而缓存里存的一直是**长边 768**
// ——那是按最大的那一处（满屏，384 DIP @2x = 768）定的，于是其余每一处都在
// 「解一张比需要的大十几倍的图」。
//
// 一张 768×432 在内存里是 1.33MB 的 RGBA（加 mipmap 约 1.77MB）；而列表行
// 真的只需要 96×96（0.04MB）——**四十多倍**。一屏十几张、翻几十个页面之后，
// 差的就是那几十 MB。
//
// # 为什么是「显示尺寸 × 屏幕倍率」
//
// 那是**不糊的最小值**（Retina 上一个 DIP 占两个物理像素，给少了要插值放大）。
// 多要的每一像素都直接是 4 字节内存，所以这里不预留「用户可能把窗口拉大」的
// 余量——窗口变大时布局会重建、这一格拿到的新尺寸就变了，缓存按新 key 再去要
// 一张（旧的自然被 LRU 挤掉）。
//
// # 和 `maxStoredSide` 的关系
//
// `maxStoredSide` 从「所有图都缩到它」变成「**上限**」：再大的显示尺寸也不
// 超过它。它仍然是内存和磁盘两边共同的那条线。

// coverPixelSize 把**显示尺寸（DIP）**换成该向图床要的**像素尺寸**。
//
// scale 是屏幕倍率（Retina 是 2，普通屏是 1）。返回的长边已经吸到
// `snapToTier` 的某一档，并且不超过 `maxStoredSide`。
//
// ⚠️ **量不出显示尺寸时返回 0×0**：那意思是「**这一格要多大不知道**」，
// 而 0×0 让调用方**按原图走**（不裁、不缩）——见 `GetSized`。
//
// # 为什么不是「给最小的那一档」
//
// 以前的规则是「量不出来就给 64×64 那一档」，那是**从「量不出来」推出「小」**，
// 而这个推理是错的：
//
//   - 布局还没算完的那些调用点，量不出来是因为**真的还没算出来**，那一次用
//     原图是对的（只是慢一点）；
//   - **通栏**那一格量不出来，是因为**它是通栏**（要的就是整行那么宽），
//     不是「小」——按 64×64 要图等于把一张通栏横幅变成一张正方形缩略图，
//     **横幅的两端直接丢掉**（票 #630 实测：请求的是
//     `top_photo.jpg@64w_64h_1c.webp`，画面上横幅那一带一个像素都没有）。
//
// 「不知道要多大」时**原图是唯一安全的选择**——它至少是全的。
//
// ⚠️ **这条规则落在缓存层，不是每一处各自决定**：将来有人再传 0，会被同一条
// 规则接住。
func coverPixelSize(w, h, scale float32) (int, int) {
	if w <= 0 || h <= 0 {
		// 量不出显示尺寸：**按原图走**（0×0 让 `GetSized` 退回 `Get`）。
		return 0, 0
	}
	if scale <= 0 {
		scale = 1
	}
	pw := int(w*scale + 0.5)
	ph := int(h*scale + 0.5)

	// **长边吸档，短边按比例跟**——不是两条边各吸各的。
	//
	// 两条边各吸各的会把宽高比掰弯：400×336 的两条边分别吸到 400 / 400，
	// 图被拉伸两成（这个仓库吃过这一口，`TestTheProbeKeyMatchesTheDrawnKey`
	// 里量出来的 400w_400h 就是它）。
	//
	// 而只吸长边、短边纯按比例算，又让短边**连续**变化（首页磁贴的宽随窗口
	// 走、高固定）：窗口拖一像素 URL 就换一个，实测拖动 64 个像素产生 48 个
	// 不同的 URL。
	//
	// 所以短边**跟比例、但量化到「1/32」**：宽高比的偏差最多 3%，肉眼看不出来
	// （而且画的时候 `Fit(ui.Cover)` 还会再裁一次），而拖动时短边只会每隔
	// 几十像素才变一档——`TestDraggingTheWindowDoesNotMakeNewURLs` 钉着这一条。
	long, short := pw, ph
	if ph > pw {
		long, short = ph, pw
	}
	tier := min(snapToTier(long), maxStoredSide)
	if long > 0 {
		short = quantize(int(float64(short)*float64(tier)/float64(long)), 32)
	}
	if ph > pw {
		return short, tier
	}
	return tier, short
}

// quantize 把 px 量化到 step 的整数倍（向上取整，至少一个 step）。
//
// 它是「短边跟着长边的比例走，但不要精确到像素」——理由见 `coverPixelSize`
// 里那段注释：精确到像素的话，窗口拖一像素 URL 就换一个。
func quantize(px, step int) int {
	if px <= 0 {
		return step
	}
	return (px + step - 1) / step * step
}

// sizeTiers 是向图床要图的档位，从小到大。
//
// ⚠️ **必须是「向上取整到某一档」，不能直接拼任意数字**：显示尺寸会因为窗口
// 宽度差一两个像素（105 和 106），而那些都会变成不同的 URL——同一张封面被
// 下好几份、在内存里各占一块、画出来一模一样。
//
// 档位之间至少差 ~25%：这样「吸档」造成的浪费最多两成，而相邻两个显示尺寸
// 撞到同一档的机会很大（一屏十几行的高度本来就差不多）。
var sizeTiers = []int{64, 96, 128, 160, 200, 256, 320, 400, 512, 640, 768}

// snapToTier 把要的像素吸到 `sizeTiers` 里**不小于它**的那一档。
//
// 超过最大档就是最大档（调用方还会按 `maxStoredSide` 再夹一次）。
func snapToTier(px int) int {
	for _, t := range sizeTiers {
		if px <= t {
			return t
		}
	}
	return sizeTiers[len(sizeTiers)-1]
}

// sizedURL 给 B 站图床的封面地址拼上**按需裁剪**的参数。
//
// 三种情况原样返回（理由见 `size_test.go` 的 `TestTheSizedURLCarriesTheRequest`）：
// 不是 `/bfs/` 的地址、已经带过 `@` 的、以及尺寸量不出来的。
//
// ⚠️ **这是「同一个封面、多个缓存条目」的来源**：`url@96w_96h_1c.webp` 和
// `url@400w_400h_1c.webp` 是两个 key。这正是要的——列表行和歌单页各拿各的，
// 谁也不挤谁（见 `TestSizedCoversAreTheirOwnCacheEntry`）。
func sizedURL(src string, w, h int) string {
	s := FormatURLProtocol(src)
	if s == "" || w <= 0 || h <= 0 {
		return s
	}
	if !strings.Contains(s, "/bfs/") || strings.Contains(s, "@") {
		return s
	}
	return s + "@" + strconv.Itoa(w) + "w_" + strconv.Itoa(h) + "h_1c.webp"
}
