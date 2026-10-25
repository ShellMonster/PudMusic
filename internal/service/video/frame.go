package video

import "time"

// Frame 是一帧解码后的 RGBA 画面。Pix 归调用方，用完交回 Source.Recycle。
type Frame struct {
	PTS  time.Duration
	W, H int
	Pix  []byte // RGBA，W*H*4，紧排（无行填充）
}
