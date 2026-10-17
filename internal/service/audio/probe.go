package audio

import (
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	mygoaudio "github.com/egoist/mygo/audio"
)

// ProbeDuration 从**文件自己的容器**里读出时长，一个样本都不解。
//
// ## 这是什么，不是什么
//
// 它问的是解码器「你一共多少帧」（`Decoder.Len`），而帧数是写在容器头里的：
// MP4 的 `mvhd`、FLAC 的 `STREAMINFO`、MP3 的帧数、Ogg 的 granule position。
// 答完就把解码器关掉，一个 `Read` 都没发出去，音频设备也没开过。
//
// ⚠️ **「解码音频」和「读元信息」是两件事，别混**：
//   - 它**不是**「解一遍看看有多长」——那要对几万个样本跑一遍解码，几千个
//     文件扫一遍就是几十分钟，而且要开音频设备。
//   - 它**也不是**标签里的时长。ID3 的 `TLEN`、MP4 的 `©too` 那类字段我们
//     没有读（见 `localmusic.Title`），这一栏给的是**容器说的**时长。
//     两者在变长码率的 MP3 上会差几秒——所以界面上这一栏不叫「标签时长」。
//
// 答不出来的（解码器说不知道、采样率是 0、文件打不开）一律给 `(0, nil)`：
// 「这个文件里读不出时长」不是一个错误状态，界面上画「-」就对了，而返回一个
// 错误会让整次扫描把它当成坏文件记一笔。
//
// ⚠️ **必须在后台 goroutine 里调**：它有真实的磁盘 IO（要读文件头，MP4 的
// `moov` 在文件尾的话还要 seek 过去）。
func ProbeDuration(path string) (time.Duration, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	// 和 Open 同一条认容器的路（openDecoder），所以「扫出来说能放」和
	// 「扫出来有时长」说的是同一个判断，不会出现一个有一个没有。
	d, _, err := openDecoder(f)
	if err != nil {
		f.Close()
		if errors.Is(err, mygoaudio.ErrFormat) {
			return 0, fmt.Errorf("%s: unsupported audio format", path)
		}
		return 0, err
	}
	// owned 把解码器自己（实现了 io.Closer 的话）和文件绑在一起关掉，
	// 顺序和 Player 用的是同一套（见 player.go 的 owned）。
	if c, ok := owned(d, f).(io.Closer); ok {
		defer c.Close()
	}

	frames := d.Len()
	if frames <= 0 {
		return 0, nil // 解码器说不知道
	}
	rate := d.SampleRate()
	if rate <= 0 {
		return 0, nil
	}
	// 先转成 float64 再算：frames × 秒 / rate 用整数算会在几小时的音频上
	// 差出一截（纳秒 × 帧数早就溢出 int64 了）。
	secs := float64(frames) / float64(rate)
	if secs <= 0 || secs > 1e6 { // 一年多：那不是时长，那是坏数据
		return 0, nil
	}
	return time.Duration(secs * float64(time.Second)), nil
}
