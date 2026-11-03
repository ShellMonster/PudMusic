package app_test

import (
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/service/audio"
)

// 响度缓存：同一首歌第二次放不再重解整首。断言的是「第二次放拿到的
// Loudness 就是缓存里那份」——不是「缓存文件写没写」（那是实现细节）。
//
// 测试把 MeasureFileForTest 换成计数器：量过一次的歌第二次放，计数器
// 不能再涨。

func TestTheLoudnessCacheSkipsARemeasureOnTheSecondPlay(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, app.SettingsFileName)
	a := volumeApp(t, path)
	a.UpdateSettings(func(s *app.Settings) { s.NormalizeVolume = true })
	song := longWav(t, "歌.wav")

	// 换成计数的假测量：每真量一次 n++。
	var n atomic.Int64
	orig := app.MeasureFileForTest
	app.MeasureFileForTest = func(p string) (audio.Loudness, error) {
		n.Add(1)
		return audio.Loudness{LUFS: -20, Peak: 0.5}, nil
	}
	t.Cleanup(func() { app.MeasureFileForTest = orig })

	if err := a.Open(song); err != nil {
		t.Fatal(err)
	}
	// 第一放后台量完（等它落地）。
	waitFor(t, 2*time.Second, "第一次测量完成", func() bool { return n.Load() == 1 })
	a.Close()

	// 同一个 App 里第二放：应该从内存缓存拿，计数器不许动。
	if err := a.Open(song); err != nil {
		t.Fatal(err)
	}
	time.Sleep(200 * time.Millisecond)
	if n.Load() != 1 {
		t.Errorf("第二放量了 %d 次，want 1（缓存该命中）", n.Load())
	}
}

// 缓存跟着设置文件落盘：换 App、重新 MountSettings 之后第一次放就命中。
func TestTheLoudnessCacheSurvivesARestart(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, app.SettingsFileName)
	song := longWav(t, "歌.wav")

	a := volumeApp(t, path)
	a.UpdateSettings(func(s *app.Settings) { s.NormalizeVolume = true })
	var n atomic.Int64
	orig := app.MeasureFileForTest
	app.MeasureFileForTest = func(p string) (audio.Loudness, error) {
		n.Add(1)
		return audio.Loudness{LUFS: -22, Peak: 0.4}, nil
	}
	t.Cleanup(func() { app.MeasureFileForTest = orig })

	if err := a.Open(song); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 2*time.Second, "第一次测量完成", func() bool { return n.Load() == 1 })
	// CloseSettings 把缓存落盘。
	_ = a.CloseSettings()
	a.Close()

	a2 := volumeApp(t, path)
	a2.UpdateSettings(func(s *app.Settings) { s.NormalizeVolume = true })
	if err := a2.Open(song); err != nil {
		t.Fatal(err)
	}
	time.Sleep(200 * time.Millisecond)
	if n.Load() != 1 {
		t.Errorf("重启后第二放量了 %d 次，want 1（盘上缓存该命中）", n.Load())
	}
}

// ReplayGain 标签优先于缓存和测量：有标签的歌连 MeasureFileForTest 都
// 不该被调到——标签就是「已经量好的」。
//
// 用 FLAC 造这个用例：WAV 走不到标签那条路，FLAC 的 Vorbis 注释是
// 唯一能写 ReplayGain 的测试容器。
func TestTheReplayGainTagShortCircuitsMeasure(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, app.SettingsFileName)
	a := volumeApp(t, path)
	a.UpdateSettings(func(s *app.Settings) { s.NormalizeVolume = true })
	song := flacWithRG(t, filepath.Join(dir, "歌.flac"), "-5.00", "0.9")

	var n atomic.Int64
	orig := app.MeasureFileForTest
	app.MeasureFileForTest = func(p string) (audio.Loudness, error) {
		n.Add(1)
		return audio.Loudness{LUFS: -20, Peak: 0.5}, nil
	}
	t.Cleanup(func() { app.MeasureFileForTest = orig })

	if err := a.Open(song); err != nil {
		t.Fatal(err)
	}
	time.Sleep(200 * time.Millisecond)
	if n.Load() != 0 {
		t.Errorf("有 ReplayGain 的歌量了 %d 次，want 0（标签该直接给）", n.Load())
	}
}

// flacWithRG 把一个真的 FLAC 文件前面插上 ReplayGain Vorbis 注释块。
// 用真文件（flacdec/testdata）是因为 audio.Open 会真解——空壳会被
// 拒成 "unsupported audio format"。标签块插在 fLaC 头之后、原 STREAMINFO
// 块之前，解码头认的是块流，插在中间不碍事。
func flacWithRG(t *testing.T, path, gain, peak string) string {
	t.Helper()
	src := filepath.Join("..", "service", "audio", "flacdec", "testdata", "stereo16.flac")
	raw, err := os.ReadFile(src)
	if err != nil {
		t.Skipf("找不到 FLAC 测试素材：%v", err)
	}
	if string(raw[:4]) != "fLaC" {
		t.Fatal("素材不是 fLaC")
	}
	fields := [][]byte{
		[]byte("REPLAYGAIN_TRACK_GAIN=" + gain),
		[]byte("REPLAYGAIN_TRACK_PEAK=" + peak),
	}
	var comment []byte
	comment = le32Bytes(0)
	comment = append(comment, le32Bytes(uint32(len(fields)))...)
	for _, f := range fields {
		comment = append(comment, le32Bytes(uint32(len(f)))...)
		comment = append(comment, f...)
	}
	// STREAMINFO 必须是第一块（flacdec 的 readMetadata 硬性要求），标签块
	// 插在 STREAMINFO 之后：STREAMINFO 是定长 34 字节正文，头 4 字节 +
	// 34 = 38，从 fLaC 之后跳过。
	if len(raw) < 4+4+34 {
		t.Fatal("素材太短")
	}
	streaminfo := raw[4 : 4+4+34]
	// 类型 4 = VORBIS_COMMENT；不是最后一块（后面还有原文件的元数据块）。
	block := []byte{4}
	block = append(block, byte(len(comment)>>16), byte(len(comment)>>8), byte(len(comment)))
	out := append(append([]byte("fLaC"), streaminfo...), block...)
	out = append(out, comment...)
	out = append(out, raw[4+4+34:]...)
	if err := os.WriteFile(path, out, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func le32Bytes(n uint32) []byte {
	b := make([]byte, 4)
	b[0] = byte(n)
	b[1] = byte(n >> 8)
	b[2] = byte(n >> 16)
	b[3] = byte(n >> 24)
	return b
}
