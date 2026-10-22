package ui

import (
	"os"
	"runtime"
	"runtime/pprof"
	"testing"

	mygoui "github.com/egoist/mygo/ui"
)

// TestZZZReviewProbeWhoAllocates 是探针：在测量窗口里打开全采样的内存剖面，
// 看那 18 KB 一档的分配到底来自哪儿。窗口前后各存一份剖面，用 -base 做差。
func TestZZZReviewProbeWhoAllocates(t *testing.T) {
	dir := "/var/folders/n5/6gq732m93kv595wtphp75zwr0000gn/T/hp-501/0bd8ff/BMG-100/"
	b := newTestShell(t, reduceMotion(), withView(func(s *Shell) func(*mygoui.Context) {
		return func(c *mygoui.Context) {
			s.applyFrameTheme(c)
			s.viewPlayBar(c, s.app.Snapshot())
		}
	}))
	s := b.s
	tt := b.tt
	loadPausedTrack(t, s, tt)

	for range 8 {
		tt.Frame()
	}
	old := runtime.GOMAXPROCS(1)
	defer runtime.GOMAXPROCS(old)

	runtime.MemProfileRate = 1
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	writeProfile(t, dir+"before.pprof")
	for range 3000 {
		tt.Frame()
	}
	runtime.ReadMemStats(&after)
	writeProfile(t, dir+"after.pprof")
	runtime.MemProfileRate = 0

	t.Logf("总：%d 次分配 / %d 字节", after.Mallocs-before.Mallocs, after.TotalAlloc-before.TotalAlloc)
}

func writeProfile(t *testing.T, path string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := pprof.Lookup("allocs").WriteTo(f, 0); err != nil {
		t.Fatal(err)
	}
}
