package app

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ShellMonster/PudMusic/internal/service/bili"
)

// 全屏播放器那份偏好：落盘、读回、以及几个「用户可以直接编辑这个文件」的情形。
//
// 这一份**不进** `app.Settings`（理由见 fullplayer_prefs.go 的文件头），所以它
// 自己的坏法得自己兜：认不出来的颜色不能一路穿到画界面的那一行。

// TestTheDefaultsMatchBiusStore 是这一份最重要的一条。
//
// Biu 的 `defaultSettings`（`store/full-screen-player-settings.ts:21-29`）：
// 歌词 / 封面 / 虚化背景开着，频谱关着，三个颜色都是空串（走各自的默认）。
// 「默认」这句话的另一半是「**没挂上盘**的时候也是这份」——那才是用户第一次
// 打开这个应用时的样子。
func TestTheDefaultsMatchBiusStore(t *testing.T) {
	a := New()
	t.Cleanup(a.Close)

	p := a.FullPlayerPrefs()
	if !p.ShowLyrics {
		t.Error("默认歌词是关的，Biu 是开着的")
	}
	if !p.ShowSpectrum {
		t.Error("默认频谱是关的，产品要求默认开着")
	}
	if !p.ShowCover {
		t.Error("默认封面是关的，Biu 是开着的")
	}
	if !p.ShowBlurredBackground {
		t.Error("默认虚化背景是关的，Biu 是开着的")
	}
	// ⚠️ **弹幕那一格不是 Biu 的**：上游 Biu 没有弹幕（研究票 #538 报告 §6
	// 第 6 条），所以它是我们自己加的开关——而产品要求它**默认开着**
	// （用户 2026-10-26 明确要的）。
	if !p.ShowDanmaku {
		t.Error("默认弹幕是关的，产品要求默认开着")
	}
	// 颜色空串 = 走默认（频谱落到 #666，歌词落到白，背景落到主题色）。
	if p.LyricsColor != "" || p.SpectrumColor != "" || p.BackgroundColor != "" {
		t.Errorf("默认颜色不是空串：歌词 %q、频谱 %q、背景 %q",
			p.LyricsColor, p.SpectrumColor, p.BackgroundColor)
	}
}

// TestAnOldPrefsFileKeepsTheNewDefaults 断「升级读老文件」。
//
// 1 版那份文件里只有 `showSpectrum` / `spectrumColor`。新加的三个开关**不在文件
// 里**，读盘之后必须是默认（开着），不能因为 bool 零值是 false 就把用户的歌词、
// 封面和毛玻璃一起关掉——他在文件里一个字节都没改。
func TestAnOldPrefsFileKeepsTheNewDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, fullPlayerPrefsFileName)
	body := `{"schemaVersion":1,"fullPlayer":{"showSpectrum":true,"spectrumColor":"#1ed760"}}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	a := New()
	t.Cleanup(a.Close)
	a.MountFullPlayerPrefs(path)

	p := a.FullPlayerPrefs()
	if !p.ShowSpectrum || p.SpectrumColor != "#1ed760" {
		t.Errorf("老的频谱那两样没读回来：开关 %v、颜色 %q", p.ShowSpectrum, p.SpectrumColor)
	}
	if !p.ShowLyrics || !p.ShowCover || !p.ShowBlurredBackground {
		t.Errorf("老文件里没有的三个开关没有落回默认：歌词 %v、封面 %v、虚化 %v",
			p.ShowLyrics, p.ShowCover, p.ShowBlurredBackground)
	}
	// ⚠️ **老文件里也没有弹幕那一格**：它同样要落回默认（开着）——用户在那份
	// 文件里一个字节都没改过。
	if !p.ShowDanmaku {
		t.Error("老文件里没有弹幕那一格，它没有落回默认（开着）")
	}
}

// TestTheSpectrumSwitchSurvivesARestart 断「改了关掉应用还在」。
func TestTheSpectrumSwitchSurvivesARestart(t *testing.T) {
	dir := t.TempDir()
	path := FullPlayerPrefsPath(dir)
	if path == "" {
		t.Fatal("给了目录却算出空路径")
	}

	a := New()
	t.Cleanup(a.Close)
	a.MountFullPlayerPrefs(path)
	p := DefaultFullPlayerPrefs()
	p.ShowSpectrum, p.SpectrumColor = true, "#1ed760"
	a.SetFullPlayerPrefs(p)

	// 重开一个 App 读同一份文件。
	b := New()
	t.Cleanup(b.Close)
	b.MountFullPlayerPrefs(path)
	got := b.FullPlayerPrefs()
	if !got.ShowSpectrum {
		t.Error("重开之后频谱又变回关的了")
	}
	if got.SpectrumColor != "#1ed760" {
		t.Errorf("重开之后的颜色是 %q，want #1ed760", got.SpectrumColor)
	}
}

// TestThePrefsFileUsesStableKeyNames 验证偏好文件使用稳定的字段名。
func TestThePrefsFileUsesStableKeyNames(t *testing.T) {
	dir := t.TempDir()
	a := New()
	t.Cleanup(a.Close)
	a.MountFullPlayerPrefs(FullPlayerPrefsPath(dir))
	p := DefaultFullPlayerPrefs()
	p.ShowSpectrum = true
	p.LyricsColor, p.SpectrumColor, p.BackgroundColor = "#ffffff", "#ff0000", "#101014"
	a.SetFullPlayerPrefs(p)

	b, err := os.ReadFile(filepath.Join(dir, fullPlayerPrefsFileName))
	if err != nil {
		t.Fatalf("读回自己写的那份：%v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatalf("写出去的不是 JSON：%v", err)
	}
	if raw["schemaVersion"] != float64(fullPlayerPrefsSchema) {
		t.Errorf("schemaVersion 是 %v，want %d", raw["schemaVersion"], fullPlayerPrefsSchema)
	}
	inner, ok := raw["fullPlayer"].(map[string]any)
	if !ok {
		t.Fatalf("文件里没有 fullPlayer 那一段：%s", b)
	}
	// ⚠️ 三个默认开着的开关**一定**在文件里（true 不能靠 omitempty 省）；两个
	// 颜色和频谱开关这次也写了值，所以 Biu 那七个键一个不少。
	for _, k := range []string{
		"showLyrics", "showSpectrum", "showCover", "showBlurredBackground",
		"lyricsColor", "spectrumColor", "backgroundColor",
	} {
		if _, ok := inner[k]; !ok {
			t.Errorf("Biu 那一个 store 里的键 %q 不见了：%s", k, b)
		}
	}
	// ⚠️ **弹幕那一格不是 Biu 的键**（上游没有弹幕），所以它**不在上面那张
	// 表里**——但它得在文件里（否则用户关掉弹幕这件事存不下来）。
	if _, ok := inner["showDanmaku"]; !ok {
		t.Errorf("弹幕那一格不在文件里：%s", b)
	}
}

// TestAColorThatIsNotAColorFallsBackToTheDefault 断「用户直接编辑这个文件」。
//
// 设置文件是用户能手改的。`"spectrumColor": "红"` 一路穿到画界面的那一行的话，
// 界面上是一条纯黑的频谱摆在深色背景上——看着像「没画出来」，而他是在用着一个
// 自己挑过的颜色。三个颜色同一把尺子。
func TestAColorThatIsNotAColorFallsBackToTheDefault(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, fullPlayerPrefsFileName)
	write := func(lyrics, spectrum, bg string) {
		t.Helper()
		body := `{"schemaVersion":2,"fullPlayer":{"showSpectrum":true,` +
			`"lyricsColor":` + quoteJSON(lyrics) + `,` +
			`"spectrumColor":` + quoteJSON(spectrum) + `,` +
			`"backgroundColor":` + quoteJSON(bg) + `}}`
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	for _, bad := range []string{"红", "#12345", "#gggggg", "rgb(1,2,3)"} {
		write(bad, bad, bad)
		a := New()
		a.MountFullPlayerPrefs(path)
		p := a.FullPlayerPrefs()
		if p.LyricsColor != "" || p.SpectrumColor != "" || p.BackgroundColor != "" {
			t.Errorf("颜色 %q 被原样留下了（歌词 %q、频谱 %q、背景 %q），want 空串=默认",
				bad, p.LyricsColor, p.SpectrumColor, p.BackgroundColor)
		}
		if !p.ShowSpectrum {
			t.Errorf("颜色 %q 不对就把开关也带坏了", bad)
		}
		a.Close()
	}

	write("#ffffff", "#1ed760", "#101014")
	a := New()
	t.Cleanup(a.Close)
	a.MountFullPlayerPrefs(path)
	p := a.FullPlayerPrefs()
	if p.LyricsColor != "#ffffff" || p.SpectrumColor != "#1ed760" || p.BackgroundColor != "#101014" {
		t.Errorf("合法颜色被扔了：歌词 %q、频谱 %q、背景 %q",
			p.LyricsColor, p.SpectrumColor, p.BackgroundColor)
	}
}

// quoteJSON 把一个字面量写成 JSON 串（测试里手搓，用不着 encoding/json 那一套）。
func quoteJSON(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// TestABrokenPrefsFileIsIgnored 断「坏文件当没有」。
//
// 这一份丢了最坏的结果是**回到默认**——而默认是什么由设置说了算（频谱在
// 票 #525 之后默认开着），不值得为它拒绝启动（`LoadSettings` 那边要备份、要报错、
// 要只读保护，是另一回事）。
//
// ⚠️ **拿默认来比，而不是写死哪几个开关开着**：这一条要量的是「坏文件当没有」，
// 而「没有」长什么样是 `DefaultFullPlayerPrefs` 说了算（写死的话，默认一改，
// 这条就红——而它红的不是「坏文件被当成了好文件」，是「默认变了」）。
func TestABrokenPrefsFileIsIgnored(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, fullPlayerPrefsFileName),
		[]byte("{这不是 JSON"), 0o600); err != nil {
		t.Fatal(err)
	}
	a := New()
	t.Cleanup(a.Close)
	a.MountFullPlayerPrefs(FullPlayerPrefsPath(dir))
	p := a.FullPlayerPrefs()
	if want := DefaultFullPlayerPrefs(); p != want {
		t.Errorf("坏文件没有落回默认：拿到 %+v，want %+v", p, want)
	}
}

// TestASettingThatIsOnlyInMemoryStillSwitches 断「拿不到目录时开关照样能用」。
func TestASettingThatIsOnlyInMemoryStillSwitches(t *testing.T) {
	a := New()
	t.Cleanup(a.Close)
	// path 是空串 = 只在内存里（`FullPlayerPrefsPath("")`）。
	a.MountFullPlayerPrefs(FullPlayerPrefsPath(""))
	p := DefaultFullPlayerPrefs()
	p.ShowSpectrum = true
	a.SetFullPlayerPrefs(p)

	if !a.FullPlayerPrefs().ShowSpectrum {
		t.Fatal("只改内存的那一次没生效")
	}
	// 再改一次还是同一个实例：挂回 App 上是为了不让它自己跳回去。
	p.ShowSpectrum, p.SpectrumColor = false, "#123456"
	a.SetFullPlayerPrefs(p)
	if a.FullPlayerPrefs().ShowSpectrum {
		t.Error("第二次改没生效")
	}
	if got := a.FullPlayerPrefs().SpectrumColor; got != "#123456" {
		t.Errorf("第二次改把颜色丢了：%q", got)
	}
}

// ---- 频谱条 ----

// TestTheSpectrumBarsAreZeroWithoutATrack 断「没歌时给的是一排 0」。
//
// 返回一排 0 而不是空切片：调用方要的是**格数**（`barCount` 根），空切片会让
// 那一块连格子都没有。而一排 0 在画面上是**什么都没有**——`paintSpectrum` 里
// 高度为 0 的那几格不画（票 #374 改成柱状图之后，旧画法那条「基线」没了）。
func TestTheSpectrumBarsAreZeroWithoutATrack(t *testing.T) {
	a := New()
	t.Cleanup(a.Close)

	bars := a.SpectrumBars(48, 40)
	if len(bars) != 48 {
		t.Fatalf("给了 48 根，出来 %d 根", len(bars))
	}
	for i, v := range bars {
		if v != 0 {
			t.Fatalf("没歌的时候第 %d 根是 %v", i, v)
		}
	}
	if a.SpectrumBars(0, 40) != nil {
		t.Error("要 0 根的时候没有回 nil")
	}
}

func TestTheSpectrumPreferenceControlsTheCurrentPlayer(t *testing.T) {
	a := New()
	t.Cleanup(a.Close)

	p := DefaultFullPlayerPrefs()
	p.ShowSpectrum = false
	a.SetFullPlayerPrefs(p)
	if err := a.Open(filepath.Join("..", "service", "audio", "flacdec", "testdata", "stereo16.flac")); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return a.Snapshot().Position > 0 }, "播放位置前进")
	for i, v := range a.SpectrumBars(48, 40) {
		if v != 0 {
			t.Fatalf("频谱关闭时第 %d 根是 %v", i, v)
		}
	}

	p.ShowSpectrum = true
	a.SetFullPlayerPrefs(p)
	waitFor(t, func() bool {
		for _, v := range a.SpectrumBars(48, 40) {
			if v > 0 {
				return true
			}
		}
		return false
	}, "频谱历史恢复")
}

// 频谱是**每帧**画的，每帧 `make` 一次是白烧（48 根 × 4 字节 × 60 帧/秒）。
// 所以返回的是同一块切片：第二次拿到的和第一次是同一块内存。
func TestTheSpectrumBarsReuseOneBuffer(t *testing.T) {
	a := New()
	t.Cleanup(a.Close)

	first := a.SpectrumBars(48, 40)
	save := append([]float32(nil), first...)
	second := a.SpectrumBars(48, 40)
	if len(first) == 0 || &first[0] != &second[0] {
		t.Fatal("两次拿到的不是同一块缓冲")
	}
	for i := range second {
		second[i] = 1 // 改坏它
	}
	for i, v := range a.SpectrumBars(48, 40) {
		if v != save[i] {
			t.Fatalf("上一次改的那一块漏进了这一次：第 %d 根是 %v，want %v", i, v, save[i])
		}
	}

	// 要更多根的时候要能长大，而不是拿旧的截断。
	bigger := a.SpectrumBars(128, 40)
	if len(bigger) != 128 {
		t.Fatalf("要 128 根，出来 %d 根", len(bigger))
	}
}

// ---- 分集 ----

// TestPlayPartRefusesWhatItCannotDo 断「说清楚为什么」。
//
// 分集是 B 站的东西：本地音乐没有、本地音乐之外还没开始的队列也没有。
// 每一支都要有一句人话，不能返回一个空错误让界面上「点了没反应」。
func TestPlayPartRefusesWhatItCannotDo(t *testing.T) {
	a := New()
	t.Cleanup(a.Close)

	if err := a.PlayPart(""); err == nil {
		t.Error("空 cid 没有被拒")
	}
	if err := a.PlayPart("777"); err == nil {
		t.Error("队列空着却接受了换集")
	}

	a.Queue().Append(LocalItem("/tmp/whatever.mp3"))
	a.queue.selectAt(0)
	if err := a.PlayPart("777"); err == nil {
		t.Error("本地文件却接受了换集")
	}
}

// TestPlayPartIgnoresTheEpisodeAlreadyPlaying 断「在放的就别重头再放一次」。
//
// 点正在放的那一行不该把歌从头播一遍：Biu 的 `playListItem` 开头就是一句
// `if (playId === id) return`。
func TestPlayPartIgnoresTheEpisodeAlreadyPlaying(t *testing.T) {
	a := New()
	t.Cleanup(a.Close)

	a.Queue().Append(Item{Source: "online", Type: "mv", Bvid: "BV1", Cid: "777"})
	a.queue.selectAt(0)
	if err := a.PlayPart("777"); err != nil {
		t.Errorf("在放的那一集又被点了一次：%v", err)
	}
	if a.player != nil {
		t.Error("为了「点同一个」开了一个播放器出来")
	}
}

// TestPlayPartReplacesTheQueueItemInPlace 断「换集不插新的一项」。
//
// 多 P 的两集在 `Item.Identity()` 里是同一首（都按 bvid 判重）。插一项新的
// 的话抽屉和队列里会同时出现同一首，而「上一首 / 下一首」走的还是队列——
// 用户换完集按「上一首」，期望回到刚才那一集去。
func TestPlayPartReplacesTheQueueItemInPlace(t *testing.T) {
	a := New()
	t.Cleanup(a.Close)
	// 换集会真的去取流，而这里没挂取流零件——所以这一趟必然失败。
	// **要断的是队列**，不是取流成没成功。
	a.Queue().Append(Item{Source: "online", Type: "mv", Bvid: "BV1", Cid: "777", Title: "多 P"})
	a.queue.selectAt(0)

	_ = a.PlayPart("778")

	items := a.Queue().Items()
	if len(items) != 1 {
		t.Fatalf("队列里有 %d 项，换集之后应该还是 1 项", len(items))
	}
	if items[0].Cid != "778" {
		t.Errorf("队列里那一项的 cid 是 %q，want 778", items[0].Cid)
	}
	if items[0].Title != "多 P" {
		t.Errorf("换集把别的字段也改了：%q", items[0].Title)
	}
}

// TestPartsAreNilForSomethingWithoutAny 断「没有的东西不摆」。
//
// Biu 的把手条件是 `playItem.hasMultiPart`，本地音乐压根不该去问 B 站。
func TestPartsAreNilForSomethingWithoutAny(t *testing.T) {
	a := New()
	t.Cleanup(a.Close)

	if got := a.Parts(); got != nil {
		t.Errorf("队列空着却问出了 %d 集", len(got))
	}
	a.Queue().Append(LocalItem("/tmp/x.mp3"))
	a.queue.selectAt(0)
	if got := a.Parts(); got != nil {
		t.Errorf("本地音乐却问出了 %d 集", len(got))
	}
}

// TestPartsSurvivesNotBeingMounted 断「没挂取流零件时分集是问不到的，不是崩」。
func TestPartsSurvivesNotBeingMounted(t *testing.T) {
	a := New()
	t.Cleanup(a.Close)
	a.Queue().Append(Item{Source: "online", Type: "mv", Bvid: "BV1", Cid: "777"})
	a.queue.selectAt(0)

	if got := a.Parts(); got != nil {
		t.Errorf("没挂取流零件却问出了 %d 集", len(got))
	}
	if a.PartsErr() == nil {
		t.Error("问不到却没有记下原因，界面上会说不出「为什么」")
	}
	// 再问一次不许再发一趟（`partsWanted` 那个闸）。
	_ = a.Parts()
	if a.PartsErr() == nil {
		t.Error("第二次问把第一次记下的原因冲掉了")
	}
}

// TestPartsStartOverOnAnotherVideo 断「换一首歌就从头问」。
//
// `partsBvid` 是这份缓存属于谁；换了稿还拿老的来，用户在听 A 的歌、面板列的是
// B 的集。
func TestPartsStartOverOnAnotherVideo(t *testing.T) {
	a := New()
	t.Cleanup(a.Close)

	a.Queue().Append(Item{Source: "online", Type: "mv", Bvid: "BV1", Cid: "1"})
	a.queue.selectAt(0)
	// 手动塞一份进去，模拟「问回来了」。
	a.partsMu.Lock()
	a.partsBvid, a.parts, a.partsLoaded, a.partsWanted = "BV1", []bili.VideoPart{{Cid: "1"}}, true, false
	a.partsMu.Unlock()
	if got := a.Parts(); len(got) != 1 {
		t.Fatalf("预置的一份分集没被认出来，拿到 %d 条", len(got))
	}

	a.Queue().Reset(Item{Source: "online", Type: "mv", Bvid: "BV2", Cid: "2"})
	a.queue.selectAt(0)
	if got := a.Parts(); got != nil {
		t.Errorf("换了一个稿却还拿着上一个的分集（%d 条）", len(got))
	}
}

// fetchCalls 数那个替身被叫了几趟。用 atomic 而不是普通 int：它是后台那个
// goroutine 写的、测试这一侧读的，`-race` 会报。
type fetchCalls struct{ n atomic.Int32 }

// wait 轮询到至少 n 趟。
func (c *fetchCalls) wait(t *testing.T, n int32, what string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if c.n.Load() >= n {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("等了 %d 趟，want ≥ %d（%s）", c.n.Load(), n, what)
}

func (c *fetchCalls) got() int32 { return c.n.Load() }

// TestPartsAreAskedAgainAfterAFailure 是第一轮评审的 major。
//
// 原来失败和成功一样把 `partsLoaded` 置成 true，于是一次 403 风控 / 一次网络抖动
// 之后，这一首在本次播放期间**再也不会出现分集入口**——而界面上还没有任何地方
// 能说出「问不出分集」（那个把手只在 `len(parts) > 1` 时才摆，面板进不去）。
//
// 这一条把「上一次发起」的时刻拨到退避窗口之前，然后确认**真的又发了一趟**。
func TestPartsAreAskedAgainAfterAFailure(t *testing.T) {
	a := New()
	t.Cleanup(a.Close)

	calls := &fetchCalls{}
	a.partsFetch = func(_ context.Context, _ string) ([]bili.VideoPart, error) {
		calls.n.Add(1)
		return nil, errors.New("风控")
	}
	a.Queue().Append(Item{Source: "online", Type: "mv", Bvid: "BV1", Cid: "1"})
	a.queue.selectAt(0)

	// 第一趟：失败。
	if got := a.Parts(); got != nil {
		t.Fatalf("第一次就问出了 %d 集", len(got))
	}
	calls.wait(t, 1, "第一次那一趟")
	if a.PartsErr() == nil {
		t.Fatal("问失败了却没有记下原因")
	}

	// 退避之内再问：不发。
	for range 5 {
		a.Parts()
	}
	if got := calls.got(); got != 1 {
		t.Fatalf("退避之内又发了 %d 趟，want 还是 1", got)
	}

	// 把「上一次发起」的时刻拨到退避窗口之前：下一趟之后界面会再问一次。
	a.partsMu.Lock()
	a.partsTriedAt = a.partsTriedAt.Add(-2 * partsRetryAfter)
	a.partsMu.Unlock()

	if got := a.Parts(); got != nil {
		t.Fatalf("第二趟还是问出了 %d 集", len(got))
	}
	calls.wait(t, 2, "退避之后那一趟")
	if got := calls.got(); got != 2 {
		t.Fatalf("过了退避窗口发了 %d 趟，want 2 —— 失败之后就再也不问了", got)
	}

	// 第三趟换成成功：这次就记住，不再问第四回。
	ok := &fetchCalls{}
	a.partsFetch = func(_ context.Context, _ string) ([]bili.VideoPart, error) {
		ok.n.Add(1)
		return []bili.VideoPart{{Cid: "1", Page: 1}, {Cid: "2", Page: 2}}, nil
	}
	a.partsMu.Lock()
	a.partsTriedAt = a.partsTriedAt.Add(-2 * partsRetryAfter)
	a.partsMu.Unlock()

	_ = a.Parts() // 触发第三趟（退避窗口已经被拨过去了）
	ok.wait(t, 1, "成功那一趟")
	waitParts(t, a, 2, "成功那一趟的结果")
	for range 10 {
		a.Parts()
	}
	if got := ok.got(); got != 1 {
		t.Errorf("成功之后又问了 %d 趟，want 1", got)
	}
	if a.PartsErr() != nil {
		t.Errorf("成功之后还留着上一次失败的原因：%v", a.PartsErr())
	}
}

// waitParts 轮询到问到了 want 条分集。
func waitParts(t *testing.T, a *App, want int, what string) []bili.VideoPart {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if got := a.Parts(); len(got) == want {
			return got
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("等了 %d 集，want %d（%s）", len(a.Parts()), want, what)
	return nil
}

// TestPartsAreNotAskedAgainAfterANewerFailure 断「重试本身也有退避」。
//
// 界面每一帧都调 `Parts()`，没有退避就是失败一次发一趟、每秒一趟。
func TestPartsAreNotAskedAgainAfterANewerFailure(t *testing.T) {
	a := New()
	t.Cleanup(a.Close)

	calls := &fetchCalls{}
	a.partsFetch = func(_ context.Context, _ string) ([]bili.VideoPart, error) {
		calls.n.Add(1)
		return nil, errors.New("风控")
	}
	a.Queue().Append(Item{Source: "online", Type: "mv", Bvid: "BV1", Cid: "1"})
	a.queue.selectAt(0)

	for range 50 {
		a.Parts()
	}
	calls.wait(t, 1, "第一趟")
	for range 50 {
		a.Parts()
	}
	if got := calls.got(); got != 1 {
		t.Errorf("连着问 50 次发了 %d 趟，want 1", got)
	}
}

// TestPartsWorthAsking 只对 B 站的 mv 为真。
func TestPartsWorthAsking(t *testing.T) {
	a := New()
	t.Cleanup(a.Close)

	if a.PartsWorthAsking() {
		t.Error("队列空着却说有分集可问")
	}
	a.Queue().Append(LocalItem("/tmp/x.mp3"))
	a.queue.selectAt(0)
	if a.PartsWorthAsking() {
		t.Error("本地音乐却有分集可问")
	}
	a.Queue().Reset(Item{Source: "online", Type: "mv", Bvid: "BV1", Cid: "1"})
	a.queue.selectAt(0)
	if !a.PartsWorthAsking() {
		t.Error("B 站的 mv 却没有分集可问")
	}
}
