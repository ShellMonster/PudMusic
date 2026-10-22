package app

// `State.Source` 是这一票（票 #222）新加的那一栏：界面要靠它分两件事——
// 本地文件不摆「下载」那一枚（上游 `playbar/right/index.tsx:15` 的
// `source !== "local"`），而歌名点不点得动也看它（上游
// `playbar/left/index.tsx:31` 的 `isClickable`）。
//
// 以前判不出来：`Item.Source` 在队列那一层，不在快照里。

import (
	"os"
	"path/filepath"
	"testing"
)

// TestTheSnapshotTellsTheSource 断**快照里的来源和正在放的那一首对得上**。
//
// ⚠️ **判据是「和那首歌是同一首」**，不是「是不是本地文件」：这一栏是界面
// 用来做决定的（摆不摆下载键、歌名点不点），判错了就会对着**另一首歌**做决定。
//
// ⚠️ **没歌时是空串**：那一栏和 `HasTrack` 是一对——没有歌的时候谈来源没有意义。
func TestTheSnapshotTellsTheSource(t *testing.T) {
	a := New()
	t.Cleanup(a.Close)

	if got := a.Snapshot().Source; got != "" {
		t.Errorf("还没放任何东西时来源是 %q，应该是空串", got)
	}

	path := localAudioForSource(t)
	if err := a.Open(path); err != nil {
		t.Fatalf("打开本地文件：%v", err)
	}
	st := a.Snapshot()
	if !st.HasTrack {
		t.Fatal("本地文件没放起来")
	}
	if st.Source != SourceLocal {
		t.Errorf("本地文件的来源是 %q，want %q", st.Source, SourceLocal)
	}
	if st.Path != path {
		t.Errorf("来源说 %q，而路径是 %q——这一栏和那一首对不上", st.Source, st.Path)
	}
}

// localAudioForSource 造一个能打开的本地音频。
//
// ⚠️ **真的造一个**：这一条要验的是「本地文件那一路会写路径」——用假的路径
// 造不出「打不开」和「打开了」的区别。
func localAudioForSource(t *testing.T) string {
	t.Helper()
	// 复用仓库里现成的测试素材（FLAC 解码器那一份）。
	p := filepath.Join("..", "service", "audio", "flacdec", "testdata", "stereo16.flac")
	if _, err := os.Stat(p); err != nil {
		t.Skipf("找不到 FLAC 测试素材：%v", err)
	}
	return p
}
