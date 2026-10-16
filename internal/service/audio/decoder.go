// 解码错误的单独上报。
//
// # 为什么必须单独做一层
//
// MyGo 的流接口把解码失败**折成「读到文件尾」**（`mygo/audio/source.go`
// 的 `stream.decode`：任何 `err != nil` 都只置 `atEOF = true`，具体错误
// 不往外传）。它这么设计是对的——播放器只关心「还有没有样本」。
//
// 但对我们不行：`Playing()` 在暂停、停止、关闭、播完时**都是 false**
// （`mygo/audio/audio.go` 的 `Playing`），而「解码失败」在这条链路上
// 也表现为 `Playing() == false`。于是：
//
//	歌播完了        → Playing() == false → 该自动下一首
//	网络断了        → Playing() == false → 该报错并提示换流
//	容器坏了        → Playing() == false → 该说「这个文件解不了」
//
// 三件事在 `Playing()` 眼里**一模一样**。`spec.md`「播放状态机」一节说的
// 「解码失败不能被当成播放结束」就是这件事，这里是它的实现：**保留第一条
// 非 EOF 的错误**，让上层分得清上面那三种。
//
// # 一条错误只有一个赢家
//
// 同一个源上可能先后报好几个错（下载断了、换成流、又断了）。只记**第一条**：
// 第一条才是用户最先撞上的那个，后面的多半是它的余波。

package audio

import (
	"errors"
	"io"
	"sync"

	mygoaudio "github.com/egoist/mygo/audio"
)

// errorDecoder 在解码器和 MyGo 之间插一脚，把错误截下来。
//
// ⚠️ 它**必须转发 Close**：MyGo 靠 `s.d.(io.Closer)` 决定要不要关解码器
// （`mygo/audio/source.go` 的 `stream.decode` 退出时）。而这里 `Decoder`
// 是一个**接口**字段，方法不会被提升——不显式写 `Close`，`closingDecoder`
// 就再也没人调，AudioToolbox 的句柄会一直留着（`handles_test.go` 记着
// 这个坑：播放照常、fd 数也不涨，切到几百首才崩）。
type errorDecoder struct {
	mygoaudio.Decoder

	mu  sync.Mutex
	err error
}

// readError 截一次错误。EOF 不算错——那是正常读完。
func (d *errorDecoder) record(err error) {
	if err == nil || errors.Is(err, io.EOF) {
		return
	}
	d.mu.Lock()
	if d.err == nil {
		d.err = err
	}
	d.mu.Unlock()
}

// Err 返回第一条非 EOF 的错误。正常播完或者只是暂停过的话是 nil。
func (d *errorDecoder) Err() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.err
}

func (d *errorDecoder) Read(buf []float32) (int, error) {
	n, err := d.Decoder.Read(buf)
	d.record(err)
	return n, err
}

// SetPosition 也截。
//
// seek 失败同样会被 MyGo 折成「流到头了」（`source.go` 里
// `s.atEOF = err != nil`），表现和「播完了」完全一样。拖进度条拖到一个
// 坏位置然后界面安静地停住，就是漏了这里。
func (d *errorDecoder) SetPosition(frame int64) error {
	err := d.Decoder.SetPosition(frame)
	d.record(err)
	return err
}

// Close 转发给内层解码器，没有就什么都不做。
func (d *errorDecoder) Close() error {
	if c, ok := d.Decoder.(io.Closer); ok {
		return c.Close()
	}
	return nil
}

var _ mygoaudio.Decoder = (*errorDecoder)(nil)
var _ io.Closer = (*errorDecoder)(nil)
