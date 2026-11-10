// 进度条下面那一层时间轴：已缓冲区间和弹幕热度。
//
// 两者都画在滑块自己的 `DrawOver` 里，和章节刻度共用「值 → 像素」的映射
// （`x0 = r.X + pad`，`dx = r.W - 2*pad`，pad 是旋钮留在轨道里的半个宽）。
// 分开画而不是各算一套，是因为差半个像素就会「点在缝上、歌在点上」。
//
// # 弹幕热度为什么要缓存
//
// 界面每帧问一次，而分桶要遍历整份弹幕（几千条）。缓存按「条数 + 首末 id +
// 时长」认身份：换集、换歌、弹幕增量回来都会让指纹变，其余帧直接用上一次
// 的桶。
package ui

import (
	"math"
	"time"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/egoist/mygo/ui"
)

const (
	// timelineHeatPoints 是热度条最多的点数。一首歌再长也压到这么多根，
	// 否则一分钟的宽度里画不出可分辨的条。
	timelineHeatPoints = 480
	// timelineHeatBucket 是最小分桶宽度。
	timelineHeatBucket = 5 * time.Second
)

// timelineHeat 是弹幕热度条的跨帧缓存。
type timelineHeat struct {
	count       int
	first, last string
	dur         time.Duration
	bars        []float32
}

// of 返回这一批弹幕的热度（0~1，已按峰值归一）。返回的切片只读。
func (h *timelineHeat) of(items []app.DanmakuItem, dur time.Duration) []float32 {
	if dur <= 0 || len(items) == 0 {
		h.bars = nil
		return nil
	}
	first, last := items[0].ID, items[len(items)-1].ID
	if h.bars != nil && h.count == len(items) && h.first == first && h.last == last && h.dur == dur {
		return h.bars
	}
	bars := heatBuckets(items, dur)
	h.count, h.first, h.last, h.dur, h.bars = len(items), first, last, dur, bars
	return bars
}

// heatBuckets 把弹幕按时间分桶，用平方根压峰值。
//
// 平方根是给「满屏弹幕」留余地：不做的话一个高潮段能把其余部分全压成一条
// 平线；做了之后 1 条和 100 条之间仍然分得开，但不会只剩一根柱子。
func heatBuckets(items []app.DanmakuItem, dur time.Duration) []float32 {
	if dur <= 0 || len(items) == 0 {
		return nil
	}
	secs := dur.Seconds()
	bucket := math.Max(timelineHeatBucket.Seconds(), secs/float64(timelineHeatPoints))
	n := min(int(math.Ceil(secs/bucket)), timelineHeatPoints)
	if n <= 0 {
		return nil
	}
	counts := make([]int, n)
	for _, it := range items {
		if it.TimeMS < 0 {
			continue
		}
		i := int(float64(it.TimeMS) / 1000 / bucket)
		if i < 0 {
			i = 0
		}
		if i >= n {
			i = n - 1
		}
		counts[i]++
	}
	peak := 0
	for _, c := range counts {
		peak = max(peak, c)
	}
	if peak == 0 {
		return nil
	}
	out := make([]float32, n)
	for i, c := range counts {
		out[i] = float32(math.Sqrt(float64(c) / float64(peak)))
	}
	return out
}

// timelineOverlay 是画在滑块上那两层需要的全部输入。
type timelineOverlay struct {
	// buffered 是已经下到本地的比例（0~1）。
	buffered float32
	// heat 是热度条，空表示不画。
	heat []float32
	// pad 是滑块内容框两边的留白（旋钮的一半）；trackW 是轨道粗。
	pad, trackW float32
	// bufferedCol / heatCol 是两层各自的颜色。
	bufferedCol, heatCol ui.Color
}

// paintTimelineOverlays 把已缓冲和热度画到滑块上。
//
// 两层都在轨道之外：已缓冲是轨道下面一条 2 像素的细线，热度是轨道上面
// 5 像素的柱子。压在轨道上的话，进度那一截的颜色会被它们搅浑。
//
// ⚠️ **它是纯绘制**：调用方（`playbar.go`）把它和章节标记放进**同一个**
// `DrawOver`，因为框架那个 API 只存一个回调。
func paintTimelineOverlays(p *ui.Painter, r ui.Rect, o timelineOverlay) {
	x0, dx := r.X+o.pad, r.W-2*o.pad
	if dx <= 0 {
		return
	}
	cy := r.Y + r.H/2

	if o.buffered > 0 && o.buffered < 1 {
		p.Fill(ui.Rect{
			X: x0,
			Y: cy + o.trackW/2 + 2,
			W: dx * o.buffered,
			H: 2,
		}, o.bufferedCol, 1)
	}

	if len(o.heat) > 0 {
		const barH = 5
		step := dx / float32(len(o.heat))
		w := min(step*0.7, step)
		y := cy - o.trackW/2 - 2 - barH
		for i, v := range o.heat {
			if v <= 0 {
				continue
			}
			h := barH * min(max(v, 0), 1)
			p.Fill(ui.Rect{
				X: x0 + step*float32(i),
				Y: y + barH - h,
				W: w,
				H: h,
			}, o.heatCol, 0.5)
		}
	}
}
