package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/ShellMonster/Biu_Go/internal/service/bili"
)

// 全屏播放器那份偏好：落盘、读回、以及几个「用户可以直接编辑这个文件」的情形。
//
// 这一份**不进** `app.Settings`（理由见 fullplayer_prefs.go 的文件头），所以它
// 自己的坏法得自己兜：认不出来的颜色不能一路穿到画界面的那一行。

// TestTheSpectrumIsOffByDefault 是这一份最重要的一条。
//
// Biu 的 `defaultSettings.showSpectrum: false`（`store/full-screen-player-settings.ts:22`）。
// 「默认是关的」这句话的另一半是「**没挂上盘**的时候也是关的」——那才是用户第一次
// 打开这个应用时的样子。
func TestTheSpectrumIsOffByDefault(t *testing.T) {
	a := New()
	t.Cleanup(a.Close)

	if p := a.FullPlayerPrefs(); p.ShowSpectrum {
		t.Error("没挂上盘的时候频谱是开着的")
	}
	// 颜色空串 = 走默认（界面上那个 #666，见 ui/fullplayer_spectrum.go）。
	if got := a.FullPlayerPrefs().SpectrumColor; got != "" {
		t.Errorf("默认颜色是 %q，want 空串（界面上落到 #666）", got)
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
	a.SetFullPlayerPrefs(FullPlayerPrefs{ShowSpectrum: true, SpectrumColor: "#1ed760"})

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

// TestThePrefsFileUsesBiusKeyNames 断「键名和 Biu 那个 store 一致」。
//
// 用户从 Biu 迁过来的时候是**按内容**认的（整份导出/导入），键名不一样就等于
// 他在 Biu 里调好的频谱开关到这边变成「没设过」。
func TestThePrefsFileUsesBiusKeyNames(t *testing.T) {
	dir := t.TempDir()
	a := New()
	t.Cleanup(a.Close)
	a.MountFullPlayerPrefs(FullPlayerPrefsPath(dir))
	a.SetFullPlayerPrefs(FullPlayerPrefs{ShowSpectrum: true, SpectrumColor: "#ff0000"})

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
	for _, k := range []string{"showSpectrum", "spectrumColor"} {
		if _, ok := inner[k]; !ok {
			t.Errorf("Biu 那一个 store 里的键 %q 不见了：%s", k, b)
		}
	}
}

// TestAColorThatIsNotAColorFallsBackToTheDefault 断「用户直接编辑这个文件」。
//
// 设置文件是用户能手改的。`"spectrumColor": "红"` 一路穿到画界面的那一行的话，
// 界面上是一条纯黑的频谱摆在深色背景上——看着像「没画出来」，而他是在用着一个
// 自己挑过的颜色。
func TestAColorThatIsNotAColorFallsBackToTheDefault(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, fullPlayerPrefsFileName)
	write := func(v string) {
		t.Helper()
		body := `{"schemaVersion":1,"fullPlayer":{"showSpectrum":true,"spectrumColor":` +
			quoteJSON(v) + `}}`
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	for _, bad := range []string{"红", "#12345", "#gggggg", "rgb(1,2,3)"} {
		write(bad)
		a := New()
		a.MountFullPlayerPrefs(path)
		if got := a.FullPlayerPrefs().SpectrumColor; got != "" {
			t.Errorf("颜色 %q 被原样留下了（got %q），want 空串=默认", bad, got)
		}
		if !a.FullPlayerPrefs().ShowSpectrum {
			t.Errorf("颜色 %q 不对就把开关也带坏了", bad)
		}
		a.Close()
	}

	write("#1ed760")
	a := New()
	t.Cleanup(a.Close)
	a.MountFullPlayerPrefs(path)
	if got := a.FullPlayerPrefs().SpectrumColor; got != "#1ed760" {
		t.Errorf("一个合法的颜色被扔了：%q", got)
	}
}

// quoteJSON 把一个字面量写成 JSON 串（测试里手搓，用不着 encoding/json 那一套）。
func quoteJSON(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// TestABrokenPrefsFileIsIgnored 断「坏文件当没有」。
//
// 这一份丢了最坏的结果是频谱回到默认（关着），而频谱本来就该默认关着——
// 不值得为它拒绝启动（`LoadSettings` 那边要备份、要报错、要只读保护，是另一回事）。
func TestABrokenPrefsFileIsIgnored(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, fullPlayerPrefsFileName),
		[]byte("{这不是 JSON"), 0o600); err != nil {
		t.Fatal(err)
	}
	a := New()
	t.Cleanup(a.Close)
	a.MountFullPlayerPrefs(FullPlayerPrefsPath(dir))
	if a.FullPlayerPrefs().ShowSpectrum {
		t.Error("坏文件被读成了「频谱开着」")
	}
}

// TestASettingThatIsOnlyInMemoryStillSwitches 断「拿不到目录时开关照样能用」。
func TestASettingThatIsOnlyInMemoryStillSwitches(t *testing.T) {
	a := New()
	t.Cleanup(a.Close)
	// path 是空串 = 只在内存里（`FullPlayerPrefsPath("")`）。
	a.MountFullPlayerPrefs(FullPlayerPrefsPath(""))
	a.SetFullPlayerPrefs(FullPlayerPrefs{ShowSpectrum: true})

	if !a.FullPlayerPrefs().ShowSpectrum {
		t.Fatal("只改内存的那一次没生效")
	}
	// 再改一次还是同一个实例：挂回 App 上是为了不让它自己跳回去。
	a.SetFullPlayerPrefs(FullPlayerPrefs{ShowSpectrum: false, SpectrumColor: "#123456"})
	if a.FullPlayerPrefs().ShowSpectrum {
		t.Error("第二次改没生效")
	}
	if got := a.FullPlayerPrefs().SpectrumColor; got != "#123456" {
		t.Errorf("第二次改把颜色丢了：%q", got)
	}
}

// ---- 频谱条 ----

// TestTheSpectrumBarsAreZeroWithoutATrack 断「没歌时画出来是一条基线」。
//
// 返回一排 0 而不是空切片：空切片在界面上是「这一条压根没有数据」，
// 而一排 0 是「有数据，就是静」。
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

// TestTheSpectrumBarsReuseOneBuffer 断「每帧不新分配」。
//
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
