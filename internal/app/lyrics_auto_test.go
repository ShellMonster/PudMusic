package app_test

import (
	"context"
	"testing"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/service/lyrics"
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
