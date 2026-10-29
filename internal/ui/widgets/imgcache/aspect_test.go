package imgcache

import (
	"fmt"
	"testing"
)

// 这一组钉住票 #664 的核心：**向图床要的那一档，宽高比要和那一格一致**。
//
// # 为什么
//
// 980 DIP 宽的横幅 @2x 要 1960×264 物理像素。长边吸到 768 之后短边按比例是 103，
// 而「量化到 1/32」把它变成 **128** ⇒ 比例偏 19~24%（横幅被拉宽，画面上看得见）。
//
// ⚠️ **判据要量一批宽度，不能只量 980 那一档**——264 能被 8 整除是**巧合**。
func TestTheRequestedTierKeepsTheAspectRatio(t *testing.T) {
	const base = "https://i0.hdslb.com/bfs/archive/y.jpg"
	// ⚠️ **要量一大段，不只是几个点**：只量 900~1100 会漏掉「长边撞到档位
	// 边界」那一段——实测那一段（1537 DIP 往上的超宽窗口）偏差会到 3.09%，
	// 而 980 那一档只有 1.5%。
	//
	// 这一段从 600 扫到 1900（DIP）——覆盖「最小窗口」到「比 Retina 屏还宽」。
	for _, cw := range rangeAll(600, 1900) {
		const slotH = 132 // DIP
		pw, ph := coverPixelSize(float32(cw), slotH, 2)
		if pw <= 0 || ph <= 0 {
			t.Fatalf("宽 %d：量不出尺寸", cw)
		}
		// 那一格真正显示的比例（DIP）和要的那一档的比例（物理像素）。
		slotRatio := float64(cw) / float64(slotH)
		gotRatio := float64(pw) / float64(ph)
		dev := (gotRatio/slotRatio - 1) * 100
		if dev < 0 {
			dev = -dev
		}
		if dev > 3 {
			t.Errorf("宽 %d：要的那一档 %dx%d 的宽高比 %.3f，而那一格是 %.3f——偏 %.1f%%（要 ≤3%%）",
				cw, pw, ph, gotRatio, slotRatio, dev)
		}
		_ = base
		// 顺带确认那一档确实是要的尺寸（不是被最小档顶替了）。
		// ⚠️ **允许比「正好」大**（档位是向上取的），但不许比它小——小了就是糊。
		if pw < cw*2 {
			t.Errorf("宽 %d：要的那一档只有 %d 像素宽，而那一格 @2x 要 %d 像素——糊了",
				cw, pw, cw*2)
		}
		_ = sizedURL(base, pw, ph)
	}
}

// 横幅在 Retina 上**不糊**：要的那一档的像素宽 ≥ 那一格的 DIP 宽 × 屏幕倍率。
func TestTheBannerIsNotBlurryOnRetina(t *testing.T) {
	const (
		slotW = 980 // DIP（1200 窗口 − 220 侧栏）
		slotH = 132
		scale = 2
	)
	pw, ph := coverPixelSize(slotW, slotH, scale)
	if pw < slotW*scale {
		t.Errorf("要的那一档只有 %d 像素宽，而那一格 @%dx 要 %d 像素——Retina 上糊 %0.1f 倍",
			pw, scale, slotW*scale, float64(slotW*scale)/float64(pw))
	}
	if ph < slotH*scale {
		t.Errorf("要的那一档只有 %d 像素高，而那一格 @%dx 要 %d 像素", ph, scale, slotH*scale)
	}
}

// 短边那一步的量化粒度是 8（不是 32）——1/32 那档的比例偏差太大。
//
// ⚠️ **长边是「吸到档位」的**：980 DIP @2x 要 1960 物理像素，而档位表里没有
// 1960 ⇒ 吸到 **2048**（下一档）。短边按比例是 264×2048/1960 = 276，
// 量化到 8 的倍数是 **280** ⇒ 比例偏 1.5%（要 ≤3%）。
func TestTheShortEdgeIsQuantizedToEight(t *testing.T) {
	pw, ph := coverPixelSize(980, 132, 2)
	if pw != 2048 || ph != 280 {
		t.Fatalf("横幅要的那一档是 %dx%d，want 2048x280", pw, ph)
	}
	// 量化粒度确实是 8（不是 32）：32 那档短边会是 288，比例偏 9%。
	if ph%8 != 0 {
		t.Errorf("短边 %d 不是 8 的倍数——量化粒度不对", ph)
	}
}

// 「要图的那一档」**不再被「存的那一份」的上限夹住**——那两条线是两件事。
//
// ⚠️ **而内存那一头一个字都没变**：`maxStoredSide` 仍然是「存进内存缓存的
// 那一份」的上限（`scale.go` / `jpegstream.go` 自己判它），32MiB 那个预算算的
// 是 `bytesOf(bm)`（**解码后** bitmap 的字节）。
func TestTheRequestedTierIsNotClampedByTheStoredCap(t *testing.T) {
	// 一个远超 768 的显示尺寸：扩了档位表之后不该再被夹到 768。
	pw, _ := coverPixelSize(980, 132, 2)
	if pw <= maxStoredSide {
		t.Errorf("要的那一档是 %d 像素宽——它又被「存的那一份」的上限（%d）夹住了",
			pw, maxStoredSide)
	}
}

// ⚠️ **内存那一头没被放松**：这一条钉住「`maxStoredSide` 仍然是 768」，
// 而且**它只管「存的那一份」**（解码之后缩到多大），与「向图床要多大」无关。
func TestTheStoredCapIsUnchanged(t *testing.T) {
	if maxStoredSide != 768 {
		t.Errorf("`maxStoredSide` 变成了 %d——它管的是「存进内存缓存的那一份」，"+
			"放开它要重算 32MiB 的预算（那是另一票的事）", maxStoredSide)
	}
	// 存进去的那一份：解码一张 1960×264 的图，存下来的长边仍然是 768。
	b := storedBounds(1960, 264)
	if b.Dx() != 768 {
		t.Errorf("1960×264 的图存下来的长边是 %d，want 768", b.Dx())
	}
}

// 满屏播放器的背景**不会**跟着涨到 2400×1600。
//
// ⚠️ 那一格**故意**要大的那一档，但它真正需要的源图只有 600×400（画布
// `winW/4 × winH/4` = 300×200 @2x）——它现在要 2400×1600，**多要了 16 倍
// 像素**，而它是**每次开满屏都要的**（网络字节 ×10）。
//
// ⇒ 那一处**自己**知道上限是多少，所以上限由它传（`GetSizedCapped`）。
func TestTheBackdropDoesNotGrowUnbounded(t *testing.T) {
	const (
		winW = 1200
		winH = 800
		// 画布是窗口的 1/4（fullBackdropScale = 4），源图按画布尺寸 × 倍率要。
		canvasW = winW / 4
		canvasH = winH / 4
	)
	// 不加上限：会要整窗那么大。
	pw, _ := coverPixelSize(winW, winH, 2)
	if pw < 2048 {
		t.Fatalf("这一档的夹子没放开（%d 像素宽）——本票没生效", pw)
	}
	// 按画布尺寸要（那一处现在就是这么做的）。
	cw, ch := coverPixelSize(float32(canvasW), float32(canvasH), 2)
	if cw > 768 {
		t.Errorf("按画布尺寸要的是 %dx%d——比它真正需要的 600×400 大，"+
			"网络字节白花了", cw, ch)
	}
	fmt.Printf("画布 %d×%d @2x ⇒ 要 %dx%d\n", canvasW, canvasH, cw, ch)
}

// rangeAll 给出 [from, to] 之间的每一个整数。
func rangeAll(from, to int) []int {
	out := make([]int, 0, to-from+1)
	for i := from; i <= to; i++ {
		out = append(out, i)
	}
	return out
}
