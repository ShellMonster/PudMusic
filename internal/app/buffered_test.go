package app

import (
	"testing"
	"time"
)

// TestBufferedTimeNeedsBothNumbers 钉住「不知道总长度就不画」。
//
// 取流接口不保证给 Content-Length；拿 0 当分母会折出一个假位置，而进度条上
// 多画一截会让用户以为「已经缓冲到这儿了」。
func TestBufferedTimeNeedsBothNumbers(t *testing.T) {
	for _, c := range []struct {
		name         string
		dur          time.Duration
		filled, size int64
		want         time.Duration
	}{
		{"没有时长", 0, 100, 200, 0},
		{"不知道总长", 10 * time.Second, 100, 0, 0},
		{"还没下到", 10 * time.Second, 0, 200, 0},
		{"一半", 10 * time.Second, 100, 200, 5 * time.Second},
		{"下满了", 10 * time.Second, 200, 200, 10 * time.Second},
		{"多报了也不超过时长", 10 * time.Second, 300, 200, 10 * time.Second},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := bufferedTime(c.dur, c.filled, c.size); got != c.want {
				t.Errorf("bufferedTime(%v, %d, %d) = %v，want %v", c.dur, c.filled, c.size, got, c.want)
			}
		})
	}
}
