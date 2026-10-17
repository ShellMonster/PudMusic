package audio

import (
	"errors"
	"io"
	"testing"
)

// scriptDecoder 是「按一份剧本读」的假解码器：读完给剧本上的下一个错，
// 剧本用完了就一直给最后一个。
type scriptDecoder struct {
	errs []error
	n    int
}

func (d *scriptDecoder) SampleRate() int         { return 44100 }
func (d *scriptDecoder) Channels() int           { return 2 }
func (d *scriptDecoder) SetPosition(int64) error { return nil }
func (d *scriptDecoder) Len() int64              { return 1024 }

func (d *scriptDecoder) Read([]float32) (int, error) {
	if d.n >= len(d.errs) {
		d.n++
		return 0, io.EOF
	}
	err := d.errs[d.n]
	d.n++
	return 8, err
}

// TestEndedAndErrAreComplementary 盯「读完了」和「读坏了」这两件事分得清。
//
// 上层靠这一对判断「该自动下一首」还是「该报错停下来」（`App.onTrackFinished`）。
// 分不清的症状是两种反过来：
//
//   - 读完了当成坏了：放完一首歌播放条上冒一句红字，然后什么都不放。
//   - 读坏了当成读完了：断网一下就自己换歌，用户听着半首歌凭空消失。
//
// 一条源上**先出的那个算数**，所以剧本上两种都排一遍。
func TestEndedAndErrAreComplementary(t *testing.T) {
	// 读到头：Ended 为真、没有错。
	d := &errorDecoder{Decoder: &scriptDecoder{errs: []error{nil, nil, io.EOF}}}
	if d.Ended() {
		t.Error("还没读就已经说读到头了")
	}
	for range 3 {
		d.Read(nil)
	}
	if !d.Ended() {
		t.Error("读到 io.EOF 之后 Ended 还是 false——放完了一首歌不会往下走")
	}
	if err := d.Err(); err != nil {
		t.Errorf("正常读完却记了错 %v（EOF 不该算错）", err)
	}

	// 读到一半坏了：错在，没读到头。
	boom := errors.New("这一帧解不了")
	d = &errorDecoder{Decoder: &scriptDecoder{errs: []error{nil, boom}}}
	d.Read(nil)
	d.Read(nil)
	if err := d.Err(); !errors.Is(err, boom) {
		t.Errorf("读坏了记的是 %v，want %v", err, boom)
	}
	if d.Ended() {
		t.Error("读坏了还说读到头了——断网会被当成放完，然后自己换歌")
	}
}

// TestSeekClearsTheEndedLatch 盯 seek 把那个闩清掉。
//
// 闩着不清的后果很具体：一首放完（闩上了）之后用户按「播放」从头再听，上层
// 看到的还是「读到头了」——**第二个缓冲周期里它就会以为自己又放完了一次**，
// 于是一首 0.25 秒的歌在单曲模式下能一秒钟连跳几十下。
//
// 所以闩必须跟着走：MyGo 的 seek 一定会过一次 `SetPosition`（`stream.seek` →
// `decode` 里的 `s.d.SetPosition`），我们就在那儿清。
func TestSeekClearsTheEndedLatch(t *testing.T) {
	d := &errorDecoder{Decoder: &scriptDecoder{errs: []error{io.EOF}}}
	d.Read(nil)
	if !d.Ended() {
		t.Fatal("剧本第一下就是 EOF，闩却没有上")
	}

	d.SetPosition(0)
	if d.Ended() {
		t.Error("seek 之后那个闩还在——从头再听会被当成又放完一次")
	}
}
