package app_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/app"
)

// 本地文件坏掉的那些路径。
//
// # 这一条为什么单立一组
//
// SPEC「播放状态机」里最要紧的一句是「**解码失败不能被当成播放结束**」。
// 它的对立面是一个具体的、界面上看得出来的状态：
//
//	位置停在开头 · Playing=false · 界面上什么话都没有
//
// 这一组就盯这个状态。它长得和「歌放完了」**一模一样**，而用户看到它会
// 以为歌放完了——于是去换歌、去怪网络、去以为播放器坏了，实际上是文件坏了。
//
// 所以断言写在**界面读到的那份快照**上（`App.Snapshot()`），不是去问解码器
// 「你报没报错」：解码器报没报错和用户看得见什么，本来就是两件事。
//
// # 坏的造法有三种，因为坏的方式不一样
//
//	拦腰截断   头是好的、尾没了 → 解码器多半在读到某一帧时才发现
//	头部抹掉   容器认不出来   → 有的在 Open 那一层就报，有的要读到数据才发现
//	中间填垃圾 长度不变、内容变 → FLAC 靠帧 CRC 才看得出来
//
// 只造一种就等于只验了一条路：这三种坏法走的报错时机根本不同。

// corruptCase 是一份「造坏的素材」。
type corruptCase struct {
	name string // 给人看的名字
	// src 是好素材在 flacdec/testdata 里的文件名。
	src string
	// corrupt 把它改成坏的那一份，返回写出来的路径。
	corrupt func(t *testing.T, dir, good string) string
}

// 三种坏法，三种时机。素材是现成的那几份真文件（真解码器才测得出真行为）。
var corruptCases = []corruptCase{
	{
		name: "FLAC 拦腰截断", src: "stereo16.flac",
		corrupt: func(t *testing.T, dir, good string) string {
			// 留在 60%：一定落在某一帧中间，不是帧边界上。
			return writeBytes(t, dir, "cut.flac", readFile(t, good)[:sizeOf(t, good)*6/10])
		},
	},
	{
		name: "FLAC 中间填垃圾（长度不变）", src: "stereo16.flac",
		corrupt: func(t *testing.T, dir, good string) string {
			b := readFile(t, good)
			// 从头 1/4 之后铺一段垃圾：FLAC 的帧 CRC 会当场发现。
			for i := len(b) / 4; i < len(b)/4+64 && i < len(b); i++ {
				b[i] ^= 0xA5
			}
			return writeBytes(t, dir, "dirty.flac", b)
		},
	},
	{
		name: "FLAC 头部抹掉", src: "stereo16.flac",
		corrupt: func(t *testing.T, dir, good string) string {
			b := readFile(t, good)
			for i := range min(64, len(b)) {
				b[i] = 0
			}
			return writeBytes(t, dir, "nohead.flac", b)
		},
	},
	{
		name: "MP4 容器头抹掉", src: "stereo16.mp4",
		corrupt: func(t *testing.T, dir, good string) string {
			b := readFile(t, good)
			for i := range min(48, len(b)) {
				b[i] = 0xFF
			}
			return writeBytes(t, dir, "nohead.mp4", b)
		},
	},
	{
		name: "M4A 拦腰截断", src: "reference-24bit-mp4.m4a",
		corrupt: func(t *testing.T, dir, good string) string {
			return writeBytes(t, dir, "cut.m4a", readFile(t, good)[:sizeOf(t, good)/2])
		},
	},
	{
		name: "WAV 数据块抹掉（短）", src: "stereo16.wav",
		corrupt: func(t *testing.T, dir, good string) string {
			// 先把样本砍到 0.2 秒——这一条要等它「放完」才判得成（它解出来
			// 是静音，不报错），素材是四秒的话光这一条就要等四秒，而它对整个
			// 套件的要求是「别给别的测试添压」（全量并行跑的时候这里是一段
			// 真实解码 + 真实输出）。
			b := shortenWAV(t, readFile(t, good), 0.2)
			// 头 44 字节是 RIFF/WAVE/fmt/data 那几段；从 data 之后全是样本。
			for i := 44; i < len(b); i++ {
				b[i] = 0x5A
			}
			return writeBytes(t, dir, "dirty.wav", b)
		},
	},
	{
		name: "MP3 头部抹掉", src: "tone.mp3",
		corrupt: func(t *testing.T, dir, good string) string {
			b := readFile(t, good)
			for i := range min(32, len(b)) {
				b[i] = 0x00
			}
			return writeBytes(t, dir, "nohead.mp3", b)
		},
	},
}

// TestACorruptFileNeverLooksLikeItFinished 是这一组的主线。
//
// 坏文件只有两种下场，**两种都得有话说**：
//
//	Open 就失败       播放条上留着原因，用户按了播放就知道放不了
//	Open 成功、播到一半坏  界面上出现一句错，`Playing` 变 false
//
// 真正不许出现的是第三种：**界面上什么都没有**——不报错、也没在放、
// 位置停在开头。那和「这首歌放完了」在屏幕上是一模一样的，而实际原因
// 是文件坏了。这是 SPEC 点名要消掉的那一类「静默」。
//
// ⚠️ 有些坏法确实**可以**干干净净地读完（MP3 截断在帧边界上和完整文件
// 没有区别）。那种情况不算失败——断言因此不要求「一定报错」，只要求
// 「要么真在放，要么有话说」，把「静默停住」这一种排除掉。
func TestACorruptFileNeverLooksLikeItFinished(t *testing.T) {
	for _, c := range corruptCases {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			path := c.corrupt(t, dir, fixturePath(t, c.src))

			a := newApp(t)
			err := a.Open(path)

			if err != nil {
				// 开口就拒了：原因得留在状态里，不然界面上是「什么也没发生」。
				if got := a.Snapshot().Err; got == "" {
					t.Errorf("Open 报了错（%v），但状态里没有话，界面上看不出放不了", err)
				}
				t.Logf("开口就拒了：%v", err)
				return
			}

			// 开口收下了，那就等它**自己出事**或者**自己放到头**。
			//
			// ⚠️ 不能一开口就断言：此刻 Playing 还是 true、Err 还是空的，
			// 那一刻什么都证明不了。坏 FLAC 要读到坏的那一帧才炸，等到那一刻
			// 才是真的在验「解码失败有没有被说出口」。
			waitFor(t, 40*time.Second, "它要么说出一句为什么停了、要么真的放到头", func() bool {
				st := a.Snapshot()
				return st.Err != "" || reachedEnd(st)
			})
			st := a.Snapshot()
			if st.Err == "" && !reachedEnd(st) {
				t.Errorf("文件是坏的，界面却一声不吭地停住了"+
					"（Playing=%v、Err 为空、位置 %v/%v）——那看起来就是「歌放完了」",
					st.Playing, st.Position, st.Duration)
			}
			t.Logf("开口收下了：最后 Playing=%v Err=%q 位置 %v/%v",
				st.Playing, st.Err, st.Position, st.Duration)
		})
	}
}

// TestAFileThatCannotBeOpenedSaysWhy 盯的是 Open 失败时的**措辞**，
// 和上面那条分开是因为它要看的不是「有没有错」。
//
// 失败原因直接是从解码器冒出来的话，冒出来什么用户看什么：一句
// 「unknown format」对用户没有任何意义，而这一条是唯一能盯住「有没有
// 翻译成人话」的时机——Open 失败之后状态里那一句话就再也不会变了。
func TestAFileThatCannotBeOpenedSaysWhy(t *testing.T) {
	dir := t.TempDir()
	junk := writeBytes(t, dir, "broken.mp3", []byte("这不是音频，这只是一段文字。"))

	a := newApp(t)
	err := a.Open(junk)
	if err == nil {
		t.Fatal("一段纯文本改名叫 .mp3 居然打开成功了")
	}
	if got := a.Snapshot().Err; got == "" {
		t.Fatal("打开失败但状态里没有话")
	}
}

// TestTheErrorDoesNotStickToTheNextSong 盯「错误可见」的反面：
// 错误**不该粘住**。
//
// 一次坏文件之后放一首好的，如果播放条上那句话一直挂着，用户会以为
// 现在这首选不了——而它明明在放。这是和「解码失败不能被当成结束」
// 同一根线上的另一头：那边是别把失败说成结束，这边是别把过去的失败
// 说成现在。
func TestTheErrorDoesNotStickToTheNextSong(t *testing.T) {
	dir := t.TempDir()
	b := readFile(t, flac(t))
	for i := range min(64, len(b)) {
		b[i] = 0 // 抹掉 fLaC 头：一个 FLAC 一眼就认不出来
	}
	bad := writeBytes(t, dir, "broken.flac", b)

	a := newApp(t)
	if err := a.Open(bad); err == nil {
		t.Skip("这一份素材没被拒（截断处正好在能读的范围内），这一条测不到")
	}
	if got := a.Snapshot().Err; got == "" {
		t.Fatal("坏文件打开失败，状态里却没有话")
	}

	if err := a.Open(flac(t)); err != nil {
		t.Fatalf("坏文件之后再放好文件失败了：%v", err)
	}
	waitFor(t, 5*time.Second, "好文件开始放、错误被清掉", func() bool {
		st := a.Snapshot()
		return st.Playing && st.Err == ""
	})
}

// ---- 小工具 ----

// reachedEnd 报这一首是不是真的放到头了。
//
// 「放完了」和「坏在那儿停了」在界面上只差一句话，所以这里用**位置**当裁判：
// 真放完的位置会走到时长附近（留 10% 是因为最后一帧之后位置还会再走一点，
// 而有的解码器报出来的时长比能解出来的最后一帧短一点）。
//
// `st` 是值不是指针：Snapshot 每次都返回一份拷贝，这里读的是同一帧里的
// 三个字段，不会出现「位置是这一帧的、时长是那一帧的」。
func reachedEnd(st app.State) bool {
	if st.Duration <= 0 {
		return false
	}
	return st.Position >= st.Duration*9/10
}

// fixturePath 找那份素材。两个 testdata 目录都要看：FLAC/MP4/WAV 的素材在
// `flacdec/testdata`，MP3 的在上一层的 `testdata`。
func fixturePath(t *testing.T, name string) string {
	t.Helper()
	for _, dir := range []string{
		filepath.Join("..", "service", "audio", "flacdec", "testdata"),
		filepath.Join("..", "service", "audio", "testdata"),
	} {
		p := filepath.Join(dir, name)
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	t.Skipf("找不到测试素材 %s", name)
	return ""
}

func readFile(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func sizeOf(t *testing.T, path string) int {
	t.Helper()
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return int(st.Size())
}

func writeBytes(t *testing.T, dir, name string, b []byte) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, b, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// shortenWAV 把一份 WAV 砍到 seconds 秒，**顺手把两处长度改对**。
//
// 44 字节的 WAV 头：0-3 RIFF、4-7 总长、8-11 WAVE、12-15 fmt 、
// 16-35 fmt 段、36-39 data、40-43 data 长度、44 起是样本。
// 长度不改的话文件会被判成「声明 4 秒、实际 0.2 秒」，于是读到末尾时是一次
// 截断——那验的是另一件事（截断必须报错），不是这一条要验的「脏样本当成静音放完」。
func shortenWAV(t *testing.T, wav []byte, seconds float64) []byte {
	t.Helper()
	if len(wav) <= 44 {
		t.Fatalf("这份 WAV 只有 %d 字节，没有样本可砍", len(wav))
	}
	byteRate := int(wav[28]) | int(wav[29])<<8 | int(wav[30])<<16 | int(wav[31])<<24
	if byteRate <= 0 {
		t.Fatalf("这份 WAV 的字节率是 %d，读不懂", byteRate)
	}
	n := byteRate * int(seconds*1000) / 1000
	n = min(n, len(wav)-44)

	out := append([]byte(nil), wav[:44+n]...)
	put32 := func(off, v int) {
		out[off] = byte(v)
		out[off+1] = byte(v >> 8)
		out[off+2] = byte(v >> 16)
		out[off+3] = byte(v >> 24)
	}
	put32(40, n)         // data 段长度
	put32(4, len(out)-8) // RIFF 总长（从第 8 字节算到文件尾）
	return out
}
