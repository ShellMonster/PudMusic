package video

import (
	"context"
	"os"
	"testing"
	"time"
)

func fixture(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/frag.mp4")
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func fetcher(b []byte) func(context.Context, int64, int64) ([]byte, error) {
	return func(_ context.Context, from, to int64) ([]byte, error) {
		to = min(to, int64(len(b)))
		return b[from:to], nil
	}
}

func TestParseIndexFindsEverySegment(t *testing.T) {
	b := fixture(t)
	if _, err := ParseIndex(b[:40]); err != errNeedMore {
		t.Fatalf("truncated head: %v", err)
	}
	idx, err := ParseIndex(b)
	if err != nil {
		t.Fatal(err)
	}
	if len(idx.Segments) < 3 {
		t.Fatalf("segments=%d", len(idx.Segments))
	}
	var sum int64
	for i, g := range idx.Segments {
		if i > 0 && g.Offset != idx.Segments[i-1].Offset+idx.Segments[i-1].Size {
			t.Fatalf("segment %d not contiguous", i)
		}
		sum += g.Size
	}
	if last := idx.Segments[len(idx.Segments)-1]; last.Offset+last.Size > int64(len(b)) {
		t.Fatalf("segments end at %d, file is %d", last.Offset+last.Size, len(b))
	}
	if d := idx.Duration(); d < 3900*time.Millisecond || d > 4200*time.Millisecond {
		t.Fatalf("duration %v", d)
	}
	if i := idx.At(1500 * time.Millisecond); idx.Segments[i].Start > 1500*time.Millisecond || (i+1 < len(idx.Segments) && idx.Segments[i+1].Start <= 1500*time.Millisecond) {
		t.Fatalf("At picked %d", i)
	}
}

func TestSessionDeliversFramesInOrderAcrossSegments(t *testing.T) {
	s := Start(Config{Fetch: fetcher(fixture(t)), TempDir: t.TempDir()}, 0)
	defer s.Close()
	var last time.Duration = -1
	n := 0
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) && last < 3500*time.Millisecond {
		f, ok := s.Poll(time.Hour)
		if !ok {
			time.Sleep(5 * time.Millisecond)
			continue
		}
		if f.PTS < last {
			t.Fatalf("frame went back: %v after %v", f.PTS, last)
		}
		if f.W != 160 || f.H != 90 || len(f.Pix) != 160*90*4 {
			t.Fatalf("frame %dx%d len %d", f.W, f.H, len(f.Pix))
		}
		last = f.PTS
		n++
	}
	if last < 3500*time.Millisecond {
		t.Fatalf("stopped at %v after %d polls, err=%v", last, n, s.Err())
	}
}

func TestSessionStartsAtTheRequestedTime(t *testing.T) {
	s := Start(Config{Fetch: fetcher(fixture(t)), TempDir: t.TempDir()}, 2*time.Second)
	defer s.Close()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if f, ok := s.Poll(time.Hour); ok {
			if f.PTS < 2*time.Second {
				t.Fatalf("first frame %v is before the start", f.PTS)
			}
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("no frame, err=%v", s.Err())
}

func TestSessionFollowsABackwardSeek(t *testing.T) {
	s := Start(Config{Fetch: fetcher(fixture(t)), TempDir: t.TempDir()}, 0)
	defer s.Close()
	poll := func(pos time.Duration) Frame {
		deadline := time.Now().Add(10 * time.Second)
		for time.Now().Before(deadline) {
			if f, ok := s.Poll(pos); ok {
				return f
			}
			time.Sleep(5 * time.Millisecond)
		}
		t.Fatalf("no frame at %v, err=%v", pos, s.Err())
		return Frame{}
	}
	poll(3 * time.Second)
	time.Sleep(1600 * time.Millisecond)
	if f := poll(500 * time.Millisecond); f.PTS > 600*time.Millisecond {
		t.Fatalf("after seeking back got %v", f.PTS)
	}
}

func TestSessionReportsAFetchFailure(t *testing.T) {
	s := Start(Config{Fetch: func(context.Context, int64, int64) ([]byte, error) { return nil, context.DeadlineExceeded }, TempDir: t.TempDir()}, 0)
	defer s.Close()
	deadline := time.Now().Add(5 * time.Second)
	for s.Err() == nil && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if s.Err() == nil {
		t.Fatal("no error reported")
	}
}
