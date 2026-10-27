package ui

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/covertint"
	"github.com/egoist/mygo/ui"
)

// coverTint 记着当前封面取出来的色相，换封面时在后台重新取。
type coverTint struct {
	mu       sync.Mutex
	url      string
	hue, sat float64

	// glowKey / glowCol 是满屏光晕那一色的缓存：同一对 (色相, 饱和度) 只算一次，
	// 每帧再格式化一遍十六进制串、再解析回来是白花的分配。
	glowKey [2]float64
	glowCol ui.Color
	glowOK  bool
}

// tintClient 拉封面字节用的客户端：走全应用唯一的网络层（代理设置、
// 凭据边界都归它管），超时保留自己那一档——取色是锦上添花，等不起。
var tintClient = func() *http.Client {
	c := newCredentialTransport().Client(nil)
	c.Timeout = 10 * time.Second
	return c
}()

// sync 报当前封面的色相和饱和度；封面换了就起一次后台下载，取到后叫醒界面重画。
// 还没取到（或封面没有颜色）时饱和度是 0，主题沿用原来的主色。
func (t *coverTint) sync(url string, wake func()) (hue, sat float64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if url != t.url {
		t.url, t.hue, t.sat = url, 0, 0
		if url != "" {
			go t.fetch(url, wake)
		}
	}
	return t.hue, t.sat
}

// glow 是封面主色在满屏背景上的光晕色（和 sync 同一套取色）。取不到色时 ok 为 false。
//
// wake 由调用方预先备好传进来：现场写 `s.wake.invalidate` 每帧会建一个闭包。
func (t *coverTint) glow(url string, wake func()) (ui.Color, bool) {
	hue, sat := t.sync(url, wake)
	if sat <= 0 {
		return ui.Color{}, false
	}
	key := [2]float64{hue, sat}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.glowKey != key {
		t.glowKey = key
		t.glowCol, t.glowOK = parseHexColor(covertint.Hue{H: hue, S: sat}.Color(true))
	}
	return t.glowCol, t.glowOK
}

func (t *coverTint) fetch(url string, wake func()) {
	h, ok, err := covertint.Fetch(context.Background(), tintClient, url)
	if err != nil || !ok {
		return
	}
	t.mu.Lock()
	if t.url != url {
		t.mu.Unlock()
		return
	}
	t.hue, t.sat = h.H, h.S
	t.mu.Unlock()
	wake()
}
