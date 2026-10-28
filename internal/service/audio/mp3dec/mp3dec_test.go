package mp3dec

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	gomp3 "github.com/ShellMonster/PudMusic/internal/service/audio/mp3dec/gomp3"
)

// 素材：`internal/service/audio/testdata/gapless.mp3`（0.625 秒的 440 Hz
// 正弦，44100 Hz 单声道，带 LAME gapless 标签）。
//
// 来路和那一份的规格写在 `internal/service/audio/mp3_test.go` 顶上——
// 这里是同一份文件，从上一级目录拿。
const fixture = "../testdata/gapless.mp3"

// TestTheEndIsNotProbedOnASourceThatCannotAnswerIt 钉住「问不到末尾的源，
// 解码器不会去问」。
//
// # 为什么这件事要钉
//
// MP3 的长度不在文件头里：没有 Xing/Info 标签时，唯一能算出长度的办法是量
// 一遍音频字节的范围，而那需要知道文件末尾在哪。对**本地文件**那是一次
// lseek，代价可以忽略；对**网络流**那是「先把剩下的下完」——用户盯着喇叭
// 等半天，一个样本都没出来。
//
// 所以解码器问末尾之前先看「这个源问得起来吗」（`canProbeEnd`）。这一条钉
// 的是**那个「不问」真的发生了**：用一个会记数的源，被问了就红。
//
// 素材得是**没有标签**的那一份，否则长度从标签里就出来了，根本不会走到
// 「问末尾」那一步——那样的测试是空转。
func TestTheEndIsNotProbedOnASourceThatCannotAnswerIt(t *testing.T) {
	data, err := os.ReadFile(filepath.Clean(fixture))
	if err != nil {
		t.Fatal(err)
	}
	src := &countingSource{r: bytes.NewReader(tagless(t, data))}

	d, err := Open(src)
	if err != nil {
		t.Fatalf("解不了：%v", err)
	}
	if src.endAsked != 0 {
		t.Errorf("解码器问了 %d 次「末尾在哪」——这个源问不到，问了就是"+
			"「先把整个流下完」", src.endAsked)
	}
	// 长度报不出来是**对的**：没有标签，又不许问末尾。
	if d.Len() != -1 {
		t.Errorf("Len() = %d，want -1（长度不知道）", d.Len())
	}
	// 但照样解得出声音。
	buf := make([]float32, 4096)
	n, err := d.Read(buf)
	if err != nil && err != io.EOF {
		t.Fatalf("读出错：%v", err)
	}
	if n == 0 {
		t.Error("一个样本都没解出来")
	}
}

// TestALocalFileIsAskedForItsEnd 钉住「本地文件问得到末尾，长度就报得出来」。
//
// 这是上一条的另一头：同一个解码器，源换成本地文件（能 stat 自己），
// 「问末尾」就允许了，于是**没有标签**的 MP3 也报得出长度——
// 本地音乐列表里那一栏「时长」靠的就是它。
func TestALocalFileIsAskedForItsEnd(t *testing.T) {
	data, err := os.ReadFile(filepath.Clean(fixture))
	if err != nil {
		t.Fatal(err)
	}
	// 先落成一个真的文件：判据是「这个源能 stat 自己」。
	path := filepath.Join(t.TempDir(), "tagless.mp3")
	if err := os.WriteFile(path, tagless(t, data), 0o644); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	d, err := Open(f)
	if err != nil {
		t.Fatalf("解不了：%v", err)
	}
	if d.Len() <= 0 {
		t.Errorf("Len() = %d，want 一个正数（本地文件的末尾问得到，长度算得出来）", d.Len())
	}
}

// countingSource 是个会记「有没有被问末尾」的字节源。
//
// 它**不是**本地文件（没有 Stat 方法），所以解码器会拒绝问它的末尾——
// 这正是被测的那件事。
type countingSource struct {
	r        io.ReadSeeker
	endAsked int
}

func (c *countingSource) Read(p []byte) (int, error) { return c.r.Read(p) }

func (c *countingSource) Seek(off int64, whence int) (int64, error) {
	if whence == io.SeekEnd {
		c.endAsked++
		return 0, errors.New("countingSource: 问不到末尾")
	}
	return c.r.Seek(off, whence)
}

// tagless 把素材的**头一帧**（带 Info/LAME 标签的那一帧，连同前面的
// ID3v2 标签）去掉，得到一份「没有标签」的 MP3。
//
// 为什么需要它：带标签的 MP3，长度从标签里就出来了（Xing 帧数 × 每帧样本
// 数），解码器根本不会去问末尾——那样的测试是空转，钉不住任何东西。
// 没有标签的 MP3 才会走「量一遍音频字节范围」那条路。
func tagless(t *testing.T, data []byte) []byte {
	t.Helper()
	dec := gomp3.NewDecoder()
	buf := make([]float32, 1152*2)
	_, info, err := dec.DecodeFrame(data, buf)
	if err != nil && !errors.Is(err, gomp3.ErrUnsupported) {
		t.Fatalf("拆头一帧：%v", err)
	}
	if info.FrameBytes <= 0 || info.FrameBytes >= len(data) {
		t.Fatalf("头一帧的长度是 %d，素材一共 %d 字节", info.FrameBytes, len(data))
	}
	return data[info.FrameBytes:]
}
