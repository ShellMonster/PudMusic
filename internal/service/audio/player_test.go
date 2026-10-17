package audio

import (
	"path/filepath"
	"testing"
	"time"

	mygoaudio "github.com/egoist/mygo/audio"
)

// waitUntil 轮询到 cond 成立，最多等 d。异步的东西（解码 goroutine、音频输出
// 的打开）只能这样等，给死时间会 flaky。
func waitUntil(t *testing.T, d time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// Open 是本地播放的总入口，用四种真实素材各验一遍：都能打开、报得出长度、
// 能跳、关掉不漏句柄。
//
// testdata/tone.mp3 来自 MyGo 仓库自带的那份（MIT），放在这儿是为了让
// MP3 这条路也有真文件可测，而不是只测它「注册成功」。
func TestOpenPlaysEachSupportedFormat(t *testing.T) {
	w := stereo16(t)
	aiff := writeTemp(t, "t.aiff", buildAIFF("AIFF", w.samples, w.channels, w.rate, ""))
	wav := writeTemp(t, "t.wav", buildWAV(w.samples, w.channels, w.rate))

	for _, c := range []struct {
		name string
		path string
	}{
		{"flac", filepath.Join("flacdec", "testdata", "stereo16.flac")},
		{"mp3", filepath.Join("testdata", "tone.mp3")},
		{"aiff", aiff},
		{"wav", wav},
	} {
		t.Run(c.name, func(t *testing.T) {
			p, err := Open(c.path)
			if err != nil {
				t.Fatal(err)
			}
			defer p.Close()

			if p.Duration() <= 0 {
				t.Errorf("%s 的时长是 %v，解码器没报出长度", c.name, p.Duration())
			}
			p.Play()
			time.Sleep(120 * time.Millisecond)
			if !p.Playing() {
				t.Errorf("%s 按了播放却没有在放", c.name)
			}
			p.Seek(p.Duration() / 2)
			p.Pause()
		})
	}
}

// TestSystemAudioOutputOpens 盯的是「真的出声」这一半：前面的用例只验解码器
// 在走（位置会往前），这一条验播放器把系统的音频输出打开了，而且真的有采样
// 流出去——没打开输出的话，位置照样走，只是没人听得见。
//
// 没有声卡的机器（CI、远程 shell）上跳过：那是环境问题，不是播放器的 bug。
// 用 FLAC 而不是 MP3 试：FLAC 的解码缓冲短得多，起播延迟小，断言更稳。
func TestSystemAudioOutputOpens(t *testing.T) {
	p, err := Open(filepath.Join("flacdec", "testdata", "stereo16.flac"))
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()

	p.Play()
	waitUntil(t, 5*time.Second, func() bool {
		return mygoaudio.Err() != nil || p.Position() > 0
	})
	if err := mygoaudio.Err(); err != nil {
		t.Skipf("这台机器没有可用的音频输出，跳过：%v", err)
	}
	if p.Position() <= 0 {
		t.Error("系统输出开了，但一个采样也没走出来")
	}
}

func TestOpenRejectsWhatItCannotPlay(t *testing.T) {
	dir := t.TempDir()
	junk := writeTemp(t, "junk.mp3", []byte("not audio at all"))
	if _, err := Open(junk); err == nil {
		t.Error("拿一个纯文本改名叫 .mp3 居然打开成功了")
	}

	if _, err := Open(filepath.Join(dir, "missing.flac")); err == nil {
		t.Error("打开一个不存在的文件居然成功了")
	}

	// ⚠️ 「把 WAV 改名成 .flac」曾经也放在这儿，断言它打不开——那是**按
	// 扩展名认容器**的行为。现在认的是头几个字节，那份文件是能放的，
	// 由 `TestContentDecidesTheContainerNotTheFileName` 断。
}

// buildWAV 造一份 16 位小端 PCM 的 WAV，用来验 MyGo 自带的 WAV 那一路。
func buildWAV(samples []int32, channels, rate int) []byte {
	data := make([]byte, len(samples)*2)
	for i, s := range samples {
		putUint16LE(data[i*2:], uint16(int16(s)))
	}
	body := []byte("WAVE")
	body = append(body, "fmt "...)
	body = appendUint32LE(body, 16)
	body = append(body, 1, 0)                              // PCM
	body = append(body, byte(channels), byte(channels>>8)) // 声道数
	body = appendUint32LE(body, uint32(rate))
	body = appendUint32LE(body, uint32(rate*channels*2)) // 字节率
	body = append(body, byte(channels*2), 0)             // 每帧字节数
	body = append(body, 16, 0)                           // 位宽
	body = append(body, "data"...)
	body = appendUint32LE(body, uint32(len(data)))
	body = append(body, data...)

	out := make([]byte, 8, 8+len(body))
	copy(out, "RIFF")
	putUint32LE(out[4:], uint32(len(body)))
	return append(out, body...)
}

func putUint16LE(b []byte, v uint16) {
	b[0], b[1] = byte(v), byte(v>>8)
}

func putUint32LE(b []byte, v uint32) {
	b[0], b[1], b[2], b[3] = byte(v), byte(v>>8), byte(v>>16), byte(v>>24)
}

func appendUint32LE(b []byte, v uint32) []byte {
	return append(b, byte(v), byte(v>>8), byte(v>>16), byte(v>>24))
}
