package ui

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/covertint"
)

// coverTint 记着当前封面取出来的色相，换封面时在后台重新取。
type coverTint struct {
	mu       sync.Mutex
	url      string
	hue, sat float64
}

var tintClient = &http.Client{Timeout: 10 * time.Second}

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
