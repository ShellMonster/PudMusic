package app_test

import (
	"context"
	"testing"
	"time"

	"github.com/ShellMonster/PudMusic/internal/service/lyrics"
)

type fakeLyricSource struct{ cands []lyrics.Candidate }

func (f fakeLyricSource) ID() string   { return "fake" }
func (f fakeLyricSource) Name() string { return "假的" }
func (f fakeLyricSource) Search(context.Context, string) ([]lyrics.Candidate, error) {
	return f.cands, nil
}
func (f fakeLyricSource) Preview(context.Context, lyrics.Candidate) (lyrics.Doc, error) {
	return lyrics.Doc{}, lyrics.ErrNoLyrics
}

func TestALocalSongWithoutLyricsFindsThemByItself(t *testing.T) {
	a := newApp(t)
	a.MountLyrics(lyrics.NewCache(t.TempDir()))
	a.MountLyricSources(fakeLyricSource{cands: []lyrics.Candidate{
		{ID: "9", Title: "自动找词", DurationMS: 35000, LRC: "[00:01.00]第一句\n[00:05.00]第二句\n"},
	}})
	if err := a.Open(longWav(t, "自动找词.wav")); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for !a.Lyrics().HasLyrics {
		if time.Now().After(deadline) {
			t.Fatal("等了 5 秒，没有自动装上歌词")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if got := a.Lyrics().Rows[0].Text; got != "第一句" {
		t.Errorf("第一行是 %q", got)
	}
}

// blockingLyricSource 卡住 Search，直到测试放行。它用来验「自动搜的时候
// 界面不该一直转圈」：搜索慢，不代表应用还在加载。
type blockingLyricSource struct {
	started chan struct{}
	release chan struct{}
	cands   []lyrics.Candidate
}

func (b *blockingLyricSource) ID() string   { return "blocking" }
func (b *blockingLyricSource) Name() string { return "慢的" }
func (b *blockingLyricSource) Search(context.Context, string) ([]lyrics.Candidate, error) {
	close(b.started)
	<-b.release
	return b.cands, nil
}
func (b *blockingLyricSource) Preview(context.Context, lyrics.Candidate) (lyrics.Doc, error) {
	return lyrics.Doc{}, lyrics.ErrNoLyrics
}

// TestAutoSearchDoesNotHoldTheLoadingState 钉住「字幕没结果就先说没有」。
//
// 自动找歌词最长 25 秒；一直转圈的话用户读到的是「应用卡住了」。搜索继续在
// 后台跑，找到再换上去。
func TestAutoSearchDoesNotHoldTheLoadingState(t *testing.T) {
	a := newApp(t)
	a.MountLyrics(lyrics.NewCache(t.TempDir()))
	src := &blockingLyricSource{
		started: make(chan struct{}),
		release: make(chan struct{}),
		cands: []lyrics.Candidate{
			{ID: "9", Title: "自动找词3", DurationMS: 35000, LRC: "[00:01.00]终于找到\n"},
		},
	}
	a.MountLyricSources(src)
	if err := a.Open(longWav(t, "自动找词3.wav")); err != nil {
		t.Fatal(err)
	}

	select {
	case <-src.started:
	case <-time.After(5 * time.Second):
		t.Fatal("自动找歌词没有开始")
	}
	waitFor(t, 2*time.Second, "搜索期间加载态收起", func() bool {
		return !a.Lyrics().Loading
	})
	if a.Lyrics().HasLyrics {
		t.Fatal("搜索还卡着，歌词却已经有了")
	}

	close(src.release)
	waitFor(t, 5*time.Second, "搜到之后歌词上屏", func() bool {
		return a.Lyrics().HasLyrics
	})
}

func TestAnAutoSearchNeverInstallsASongThatDoesNotMatch(t *testing.T) {
	a := newApp(t)
	a.MountLyrics(lyrics.NewCache(t.TempDir()))
	a.MountLyricSources(fakeLyricSource{cands: []lyrics.Candidate{
		{ID: "1", Title: "另一首歌", DurationMS: 35000, LRC: "[00:01.00]错的词\n"},
	}})
	if err := a.Open(longWav(t, "自动找词2.wav")); err != nil {
		t.Fatal(err)
	}
	time.Sleep(500 * time.Millisecond)
	if a.Lyrics().HasLyrics {
		t.Error("装上了对不上的歌词")
	}
}
