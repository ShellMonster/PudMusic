package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ShellMonster/Biu_Go/internal/service/download"
)

// ─────────────────────────────────────────────────────────────────────────────
// 脚手架：一份**形状与 Biu 真数据逐字相同**的目录
//
// 数据抄自 `~/Library/Application Support/Biu/` 里的真文件（本票开发时读的），
// 结构抄自 `biu/electron/store.ts:12-47` 与 `biu/shared/types/*.d.ts`。抄真数据
// 是因为这一票最容易犯的错是「按自己写的格式造测试数据，于是读得进自己造的那
// 份、读不进 Biu 的」——而 Biu 的键名、`Record` 形状、`cid` 的
// `string | number` 联合类型，每一条都只有真数据能钉住。
// ─────────────────────────────────────────────────────────────────────────────

// biuFixture 造一份 Biu 数据目录，返回目录路径。
type biuFixture struct {
	dir string
}

func newBiuFixture(t *testing.T) *biuFixture {
	t.Helper()
	return &biuFixture{dir: t.TempDir()}
}

func (f *biuFixture) put(t *testing.T, name, content string) *biuFixture {
	t.Helper()
	mustWrite(t, filepath.Join(f.dir, name), content)
	return f
}

// biuAppSettings 是 Biu 真有的那份 `app-settings.json`（键名与
// `biu/shared/settings/app-settings.ts` 的 `defaultAppSettings` 逐字相同）。
const biuAppSettings = `{
  "appSettings": {
    "downloadPath": "/Users/x/Downloads",
    "closeWindowOption": "hide",
    "autoStart": false,
    "fontFamily": "system-ui",
    "primaryColor": "#17c964",
    "backgroundColor": "",
    "borderRadius": 8,
    "audioQuality": "auto",
    "hiddenMenuKeys": [],
    "displayMode": "list",
    "ffmpegPath": "",
    "themeMode": "system",
    "pageTransition": "none",
    "showSearchHistory": true,
    "proxySettings": { "type": "none", "host": "", "username": "", "password": "" },
    "sideMenuCollapsed": false,
    "sideMenuWidth": 200,
    "sideMenuCollectionFolded": { "created": false, "collected": true },
    "reportPlayHistory": true,
    "localMusicDirs": []
  }
}`

// biuDownloadsJSON 是 `media-downloads.json` 的形状：`Record<string, FullMediaDownloadTask>`
// （`electron/store.ts:31`）。三条：一条下好的、一条下到一半的、一条视频的。
const biuDownloadsJSON = `{
  "task-1": {
    "id": "task-1",
    "status": "completed",
    "outputFileType": "audio",
    "title": "已下完的那首",
    "cover": "http://cover/a.jpg",
    "bvid": "BV1aa411c7mD",
    "cid": 12345,
    "createdTime": 1700000000000,
    "savePath": "/Users/x/Downloads/已下完的那首.flac",
    "totalBytes": 31457280,
    "audioCodecs": "flac",
    "chunks": [
      { "type": "audio", "name": "0", "start": 0, "end": 10485759, "done": true },
      { "type": "audio", "name": "1", "start": 10485760, "end": 20971519, "done": true },
      { "type": "audio", "name": "2", "start": 20971520, "end": 31457279, "done": true }
    ]
  },
  "task-2": {
    "id": "task-2",
    "status": "downloading",
    "outputFileType": "audio",
    "title": "下到一半的那首",
    "bvid": "BV1bb411c7mD",
    "cid": "67890",
    "createdTime": 1700000001000,
    "savePath": "/Users/x/Downloads/下到一半的那首.m4a",
    "totalBytes": 26214400,
    "audioCodecs": "320k",
    "chunks": [
      { "type": "audio", "name": "0", "start": 0, "end": 10485759, "done": true },
      { "type": "audio", "name": "1", "start": 10485760, "end": 20971519, "done": true },
      { "type": "audio", "name": "2", "start": 20971520, "end": 26214399, "done": false }
    ]
  },
  "task-3": {
    "id": "task-3",
    "status": "completed",
    "outputFileType": "video",
    "title": "一个视频",
    "bvid": "BV1cc411c7mD",
    "cid": 999,
    "createdTime": 1700000002000,
    "savePath": "/Users/x/Downloads/一个视频.mp4",
    "totalBytes": 104857600,
    "chunks": []
  }
}`

// biuLyricsCache 是 `lyrics-cache.json` 的形状：`Record<string, MusicLyrics>`
// （`electron/store.ts:42`）。key 是 `${bvid}-${cid}`（`components/lyrics/index.tsx:194`）。
const biuLyricsCache = `{
  "BV1aa411c7mD-12345": {
    "fontSize": 18,
    "offset": -300,
    "lyrics": "[00:00.00] 第一行\n[00:05.00] 第二行",
    "tLyrics": "[00:00.00] first line"
  },
  "BV1bb411c7mD-67890": {
    "fontSize": 20,
    "offset": 0,
    "lyrics": "[00:01.00] 另一首"
  },
  "不是bvid-cid": { "lyrics": "[00:00.00] 这条不该被认出来" },
  "BV1dd411c7mD-1": { "fontSize": 16, "offset": 0 }
}`

// biuShortcutSettings 是真有的那份 `shortcut-settings.json`。
// ⚠️ `isConflict` / `error` 是 Biu 自己**算出来**再存下去的，我们不读
// （见 `shortcuts.go` 的 `shortcutItem` 注释）。
const biuShortcutSettings = `{
  "enableGlobalShortcuts": true,
  "shortcuts": [
    { "id": "togglePlay", "name": "播放/暂停", "shortcut": "CommandOrControl+Shift+P" },
    { "id": "prev", "name": "上一首", "shortcut": "CommandOrControl+Left" },
    { "id": "next", "name": "下一首", "shortcut": "CommandOrControl+Right" },
    { "id": "volumeUp", "name": "音量加", "shortcut": "CommandOrControl+Up" },
    { "id": "volumeDown", "name": "音量减", "shortcut": "CommandOrControl+Down" },
    { "id": "toggleMiniMode", "name": "切换mini/完整模式", "shortcut": "CommandOrControl+M" }
  ],
  "globalShortcuts": [
    { "id": "togglePlay", "name": "播放/暂停", "shortcut": "CommandOrControl+Alt+P", "isConflict": false }
  ]
}`

// full 是「装过 Biu、各项都用了」的那一份。
func full(t *testing.T) *biuFixture {
	return newBiuFixture(t).
		put(t, biuSettingsFile, biuAppSettings).
		put(t, biuShortcutsFile, biuShortcutSettings).
		put(t, biuDownloadsFile, biuDownloadsJSON).
		put(t, biuLyricsFile, biuLyricsCache)
}

// ─────────────────────────────────────────────────────────────────────────────
// 设置
// ─────────────────────────────────────────────────────────────────────────────

// Biu 的 `app-settings.json` 读得进来，而且落到我们自己的文件里。
func TestBiuSettingsComeAcross(t *testing.T) {
	f := newBiuFixture(t).put(t, biuSettingsFile, biuAppSettings)
	dst := t.TempDir()

	r, err := mustScan(t, f).Import(dst)
	if err != nil {
		t.Fatal(err)
	}
	if !r.Settings {
		t.Fatalf("设置没有带过来：%+v", r)
	}

	got, err := LoadSettings(SettingsPath(dst))
	if err != nil {
		t.Fatalf("导出来的设置我们自己的读盘读不懂：%v", err)
	}
	if got.PrimaryColor != "#17c964" {
		t.Fatalf("主色没带过来：%q", got.PrimaryColor)
	}
	if got.DownloadPath != "/Users/x/Downloads" {
		t.Fatalf("下载目录没带过来：%q", got.DownloadPath)
	}
	if !got.SideMenuCollectionFolded.Collected {
		t.Fatalf("收藏夹折叠状态没带过来：%+v", got.SideMenuCollectionFolded)
	}
	// 而写出去的那一份是**我们自己的**形状（带 schemaVersion），不是 Biu 的信封
	// 抄过去——下一次改任何一格都会按旧形状重写一遍。
	raw := mustRead(t, SettingsPath(dst))
	if !strings.Contains(raw, `"schemaVersion"`) {
		t.Fatalf("写出去的不是我们自己的形状：%s", raw)
	}
}

// Biu 自己那个已知的坏字体值在导入时就被修掉了（`settings.go` 的 `migrations[0]`）。
func TestBiuKnownBadFontValueIsFixedOnTheWayIn(t *testing.T) {
	f := newBiuFixture(t).put(t, biuSettingsFile,
		`{"appSettings":{"fontFamily":"system-default","primaryColor":"#17c964"}}`)
	dst := t.TempDir()
	if _, err := mustScan(t, f).Import(dst); err != nil {
		t.Fatal(err)
	}
	got, err := LoadSettings(SettingsPath(dst))
	if err != nil {
		t.Fatal(err)
	}
	if got.FontFamily != DefaultFontFamily {
		t.Fatalf("Biu 那个 \"system-default\" 没被修掉：%q", got.FontFamily)
	}
}

// 用户已经有一份自己的设置了，再导入：旧的留一份 `.bak`。
//
// 这是这一票里最坏的一种丢数据的入口——「我导入了 Biu 的设置，我的设置没了」，
// 而用户连那份文件在哪都说不出来。
func TestImportingOverMyOwnSettingsKeepsAMadeOne(t *testing.T) {
	dst := t.TempDir()
	mine := DefaultSettings("/Users/y/Music")
	mine.ThemeMode = ThemeModeDark
	if err := writeSettingsFile(SettingsPath(dst), mine); err != nil {
		t.Fatal(err)
	}

	f := newBiuFixture(t).put(t, biuSettingsFile, biuAppSettings)
	if _, err := mustScan(t, f).Import(dst); err != nil {
		t.Fatal(err)
	}

	bak := SettingsPath(dst) + ".bak"
	raw := mustRead(t, bak)
	if !strings.Contains(raw, "/Users/y/Music") {
		t.Fatalf("备份里不是我自己那份设置：%s", raw)
	}
	got, err := LoadSettings(SettingsPath(dst))
	if err != nil {
		t.Fatal(err)
	}
	if got.DownloadPath != "/Users/x/Downloads" {
		t.Fatalf("新设置没有生效：%q", got.DownloadPath)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 下载记录
// ─────────────────────────────────────────────────────────────────────────────

// Biu 的下载记录翻译过来之后，是我们自己的队列形状。
func TestBiuDownloadsBecomeOurOwnQueueEntries(t *testing.T) {
	f := newBiuFixture(t).put(t, biuDownloadsFile, biuDownloadsJSON)
	dst := t.TempDir()
	if _, err := mustScan(t, f).Import(dst); err != nil {
		t.Fatal(err)
	}

	tasks := readQueue(t, filepath.Join(dst, download.QueueFileName))
	if len(tasks) != 2 {
		t.Fatalf("队列里 %d 条，期望 2 条（视频那条不该进来）：%+v", len(tasks), tasks)
	}
	byID := map[string]download.Task{}
	for _, task := range tasks {
		byID[task.ID] = task
	}

	done, ok := byID["BV1aa411c7mD|12345"]
	if !ok {
		t.Fatalf("下完的那首不在队列里，ID 是 %q（cid 要转成字符串）", done.ID)
	}
	if done.Title != "已下完的那首" || done.Status != download.StatusDone {
		t.Fatalf("下完的那首翻错了：%+v", done)
	}
	if done.Dest != "/Users/x/Downloads/已下完的那首.flac" {
		t.Fatalf("保存路径没带过来：%q", done.Dest)
	}
	if done.Identity.Length != 31457280 {
		t.Fatalf("总长度没带过来：%d", done.Identity.Length)
	}
	if done.Label != "无损 FLAC" {
		t.Fatalf("音质徽章没翻过来：%q", done.Label)
	}
	if len(done.Chunks) != 3 {
		t.Fatalf("下完的那首应该三块全齐，实际 %v", done.Chunks)
	}

	half, ok := byID["BV1bb411c7mD|67890"]
	if !ok {
		t.Fatalf("下到一半的那首不在队列里")
	}
	// 「在飞」的一律落成暂停：重启之后那个在飞的请求早就不在了，记成 running
	// 会让界面上永远转圈（`download/store.go` 的 `Restore` 为这件事有整段注释）。
	if half.Status != download.StatusPaused {
		t.Fatalf("下到一半的那首记成了 %q，应该落成暂停", half.Status)
	}
	// cid 是字符串的那个也要转成字符串——`"67890"` 和 `67890` 不是同一个键。
	if half.Identity.Cid != "67890" {
		t.Fatalf("字符串形式的 cid 没转成字符串：%q", half.Identity.Cid)
	}
	// 三块里只下完两块：第三块 done=false，不该被算成已完成。
	if len(half.Chunks) != 2 {
		t.Fatalf("下到一半的那首应该两块齐，实际 %v", half.Chunks)
	}
	for _, i := range half.Chunks {
		if i != 0 && i != 1 {
			t.Fatalf("没下完的那块被算成已完成：%v", half.Chunks)
		}
	}
	if half.Label != "320K" {
		t.Fatalf("音质徽章没翻过来：%q", half.Label)
	}
}

// 视频任务不进来。
//
// 这一版只下音频、没交织（`download/store.go` 文件头）。放一条视频任务进来，
// 界面上会出现一个按「继续」只会重新下一遍音频的行。
func TestAVideoDownloadDoesNotBecomeAnAudioRow(t *testing.T) {
	f := newBiuFixture(t).put(t, biuDownloadsFile, biuDownloadsJSON)
	dst := t.TempDir()
	r, err := mustScan(t, f).Import(dst)
	if err != nil {
		t.Fatal(err)
	}
	if r.Downloads != 2 {
		t.Fatalf("并进去 %d 条，期望 2 条", r.Downloads)
	}
	for _, task := range readQueue(t, filepath.Join(dst, download.QueueFileName)) {
		if strings.Contains(task.Dest, ".mp4") || task.ID == "BV1cc411c7mD|999" {
			t.Fatalf("视频任务被当成音频放进来了：%+v", task)
		}
	}
	// 而且这件事要**说出来**：用户下一次找不到那个视频时，得知道它没被迁。
	if !saysSomethingAbout(r.Notes, "没能带过来") {
		t.Fatalf("跳过的条目没有出现在报告里：%v", r.Notes)
	}
}

// 分块下标是从字节区间换算出来的，而且**只算我们格式下合法的**下标。
//
// 换算错了的后果不是「进度显示不准」，是续传把两段不同的字节拼在一起——歌能
// 打开、播到一半就断，而长度对得上所以没人会去怀疑拼接。
func TestBiuChunkBookkeepingTurnsIntoOurIndices(t *testing.T) {
	// 总长 25 MiB = 2.5 块，也就是**三块**。第四条记的 end 落在第四块上，而那一块
	// 按总长根本不存在——Biu 那边换了转码、文件大小对不上时会留下这种记录。
	body := `{"t":{
      "id":"t","status":"completed","outputFileType":"audio","title":"歌",
      "bvid":"BV1xx411c7mD","cid":1,"savePath":"/x/a.m4a","totalBytes":26214400,
      "chunks":[
        {"type":"audio","start":0,"end":10485759,"done":true},
        {"type":"audio","start":10485760,"end":20971519,"done":true},
        {"type":"audio","start":20971520,"end":26214399,"done":true},
        {"type":"audio","start":26214400,"end":31457279,"done":true}
      ]}}`
	f := newBiuFixture(t).put(t, biuDownloadsFile, body)
	dst := t.TempDir()
	if _, err := mustScan(t, f).Import(dst); err != nil {
		t.Fatal(err)
	}
	tasks := readQueue(t, filepath.Join(dst, download.QueueFileName))
	if len(tasks) != 1 {
		t.Fatalf("队列里 %d 条", len(tasks))
	}
	// 合法下标是 0/1/2（第三块只有一半，done=true 说得过去）；
	// 第四条落在第四块上，越界，必须被丢掉——`Restore` 的 `keepOnRestore`
	// 会因为同样的理由丢掉越界的记账，而在导入这一层就丢，用户看到的报告里
	// 就没有一条「明明下到 80% 却没带过来」的死行。
	got := tasks[0].Chunks
	if len(got) != 3 {
		t.Fatalf("换算出 %v，期望三块（下标 0/1/2）", got)
	}
	if got[0] != 0 || got[1] != 1 || got[2] != 2 {
		t.Fatalf("换算出 %v，期望 [0 1 2]", got)
	}
}

// 导入两次不会把同一首歌在队列里放两行。
func TestImportingTwiceDoesNotDoubleTheQueue(t *testing.T) {
	f := newBiuFixture(t).put(t, biuDownloadsFile, biuDownloadsJSON)
	dst := t.TempDir()
	if _, err := mustScan(t, f).Import(dst); err != nil {
		t.Fatal(err)
	}
	if _, err := mustScan(t, f).Import(dst); err != nil {
		t.Fatal(err)
	}
	tasks := readQueue(t, filepath.Join(dst, download.QueueFileName))
	if len(tasks) != 2 {
		t.Fatalf("导了两次之后队列里有 %d 条", len(tasks))
	}
}

// 「已经有一份 v2 的队列」时不并，也不碰它。
//
// 往一份我们读不懂的文件上动手，比什么都不做糟得多。
func TestAQueueWeCannotReadIsLeftAlone(t *testing.T) {
	dst := t.TempDir()
	path := filepath.Join(dst, download.QueueFileName)
	mustWrite(t, path, `{"version":99,"tasks":[]}`)

	f := newBiuFixture(t).put(t, biuDownloadsFile, biuDownloadsJSON)
	r, err := mustScan(t, f).Import(dst)
	if err == nil {
		t.Fatalf("往一份 v99 的队列里并东西没有被拦下：%+v", r)
	}
	if got := mustRead(t, path); !strings.Contains(got, `"version":99`) {
		t.Fatalf("那份文件被改了：%s", got)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 歌词缓存
// ─────────────────────────────────────────────────────────────────────────────

// 歌词落到 `<UserData>/lyrics-cache/<bvid>-<cid>.json`（`PLAN.md` §3.4 的位置）。
func TestLyricsLandInAFileNamedAfterTheSong(t *testing.T) {
	f := newBiuFixture(t).put(t, biuLyricsFile, biuLyricsCache)
	dst := t.TempDir()
	r, err := mustScan(t, f).Import(dst)
	if err != nil {
		t.Fatal(err)
	}
	if r.Lyrics != 2 {
		t.Fatalf("写下去 %d 条，期望 2 条：%v", r.Lyrics, r.Notes)
	}

	path := filepath.Join(dst, LyricsCacheDirName, "BV1aa411c7mD-12345.json")
	var got LyricsEntry
	if err := json.Unmarshal([]byte(mustRead(t, path)), &got); err != nil {
		t.Fatalf("写出来的歌词读不懂：%v", err)
	}
	if got.Bvid != "BV1aa411c7mD" || got.Cid != 12345 {
		t.Fatalf("身份没写对：%+v", got)
	}
	if !strings.Contains(got.Lyrics, "第一行") {
		t.Fatalf("歌词正文没写下去：%+v", got)
	}
	if got.TLyrics != "[00:00.00] first line" {
		t.Fatalf("翻译没写下去：%+v", got)
	}
	// 用户自己调过的偏移/字号也要过去——那是他在歌词页上拖出来的。
	if got.Offset != -300 || got.FontSize != 18 {
		t.Fatalf("偏移/字号没写下去：%+v", got)
	}

	// cid 是字符串的那一条也要落成 `<bvid>-<cid>.json`（键名是数字）。
	if _, err := os.Stat(filepath.Join(dst, LyricsCacheDirName, "BV1bb411c7mD-67890.json")); err != nil {
		t.Fatalf("第二个歌词没落盘：%v", err)
	}
}

// 认不出是哪首歌的、以及一条空歌词，都不写下去，而且**说出来**。
func TestLyricsThatCannotBeAttributedAreSkippedAndSaid(t *testing.T) {
	f := newBiuFixture(t).put(t, biuLyricsFile, biuLyricsCache)
	dst := t.TempDir()
	r, err := mustScan(t, f).Import(dst)
	if err != nil {
		t.Fatal(err)
	}
	// 「不是bvid-cid」（认不出身份）和「BV1dd411c7mD-1」（既没原文也没翻译）
	// 都不该出现——猜出来的文件名会把歌词写到错误的歌上，而用户看不出来。
	entries, err := os.ReadDir(filepath.Join(dst, LyricsCacheDirName))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name() != "BV1aa411c7mD-12345.json" && e.Name() != "BV1bb411c7mD-67890.json" {
			t.Fatalf("不该出现的歌词文件：%s", e.Name())
		}
	}
	if !saysSomethingAbout(r.Notes, "歌词缓存") {
		t.Fatalf("跳过的歌词没有出现在报告里：%v", r.Notes)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 登录态
// ─────────────────────────────────────────────────────────────────────────────

// Biu 的登录态**不迁**，而是要提示重新登录。
//
// 那份文件是 electron-store 的 `encryptionKey` 加密的（conf 的 AES-CBC，密钥
// 由固定字符串 + 机器 id 派生），实测是一堆读不懂的字节。搬它没有意义——
// 而用户看不到「请重新登录」这句提示的话，导入之后他只会发现「怎么还是没登录」。
func TestTheLoginStateIsNotCarriedOverButIsMentioned(t *testing.T) {
	f := full(t).put(t, biuLoginFile,
		"{t}\x00\x01\x02二进制密文\xff不是JSON")

	scan := mustScan(t, f)
	if !scan.LoggedIn {
		t.Fatal("Biu 那边是登录状态，却没被认出来：用户不会被提示重新登录")
	}

	dst := t.TempDir()
	if _, err := scan.Import(dst); err != nil {
		t.Fatal(err)
	}

	// 一个字节的凭据都不许落到我们的目录里。
	var leaked []string
	_ = filepath.Walk(dst, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		body := mustReadFile(t, p)
		for _, secret := range []string{"二进制密文", "SESSDATA", "bili_jct", "refresh_token"} {
			if strings.Contains(body, secret) {
				leaked = append(leaked, p+" 里有 "+secret)
			}
		}
		return nil
	})
	if len(leaked) > 0 {
		t.Fatalf("凭据漏进了我们的目录：%v", leaked)
	}
	// 而「请重新登录」这句话必须在。
	if !saysSomethingAbout(scan.Notes, "重新登录") {
		t.Fatalf("没有提示重新登录：%v", scan.Notes)
	}
}

// Biu 那边没登录时不该凭空说「请重新登录」。
func TestNothingSaysReloginWhenBiuWasNotLoggedIn(t *testing.T) {
	f := newBiuFixture(t).put(t, biuSettingsFile, biuAppSettings)
	scan := mustScan(t, f)
	if scan.LoggedIn {
		t.Fatal("没放登录文件却说有登录态")
	}
	for _, n := range scan.Notes {
		if strings.Contains(n, "重新登录") {
			t.Fatalf("没登录却提示重新登录：%v", scan.Notes)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 目录探测
// ─────────────────────────────────────────────────────────────────────────────

// 一个目录里四个文件一个都没有时不算「装过 Biu」。
//
// 判据只看 `app-settings.json` 会漏掉「只下过歌、没改过设置」的用户。
func TestAnEmptyDirectoryIsNotBiu(t *testing.T) {
	empty := t.TempDir()
	if HasBiuData(empty) {
		t.Fatal("一个空目录被当成了 Biu")
	}
	scan, err := ScanBiu(empty)
	if err != nil || scan != nil {
		t.Fatalf("空目录扫出了东西：%+v %v", scan, err)
	}

	// 只放下载记录也算。
	onlyDownloads := t.TempDir()
	mustWrite(t, filepath.Join(onlyDownloads, biuDownloadsFile), `{}`)
	if !HasBiuData(onlyDownloads) {
		t.Fatal("只有下载记录的用户没有被认出来")
	}
}

// 一份坏掉的文件不拖累别的：设置读不出来，下载记录照样进来。
func TestOneUnreadableFileDoesNotSinkTheWholeImport(t *testing.T) {
	f := newBiuFixture(t).
		put(t, biuSettingsFile, `这不是 JSON`).
		put(t, biuDownloadsFile, biuDownloadsJSON)
	dst := t.TempDir()

	r, err := mustScan(t, f).Import(dst)
	if err != nil {
		t.Fatalf("一份坏文件让整个导入失败了：%v", err)
	}
	if r.Settings {
		t.Fatal("一份读不懂的设置被当成功了")
	}
	if r.Downloads != 2 {
		t.Fatalf("下载记录也没进来：%d", r.Downloads)
	}
	if !saysSomethingAbout(r.Notes, biuSettingsFile) {
		t.Fatalf("坏掉的那一份没有出现在报告里：%v", r.Notes)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 小工具
// ─────────────────────────────────────────────────────────────────────────────

func mustScan(t *testing.T, f *biuFixture) *BiuScan {
	t.Helper()
	scan, err := ScanBiu(f.dir)
	if err != nil {
		t.Fatal(err)
	}
	if scan == nil {
		t.Fatalf("%s 里明明有 Biu 的数据，ScanBiu 却说没有", f.dir)
	}
	return scan
}

// readQueue 读一份队列文件。
func readQueue(t *testing.T, path string) []download.Task {
	t.Helper()
	var f struct {
		Tasks []download.Task `json:"tasks"`
	}
	if err := json.Unmarshal([]byte(mustRead(t, path)), &f); err != nil {
		t.Fatalf("读队列 %s：%v", path, err)
	}
	return f.Tasks
}

// saysSomethingAbout 报一句话里有没有提到某个词。
func saysSomethingAbout(notes []string, word string) bool {
	for _, n := range notes {
		if strings.Contains(n, word) {
			return true
		}
	}
	return false
}

// macOS 上 `Biu` 和 `biu` 是**同一个目录**（默认 APFS 大小写不敏感），只列一个。
//
// 这一条是被真机验证带出来的：在本机上跑 `FindBiuDirs()` 返回了三条，其中
// `…/Biu` 和 `…/biu` 指向同一个 inode。按路径字符串去重会漏掉它们，于是设置页上
// 出现两条一模一样的「从 … 导入」，导入报告里也会列两遍。判据是 `os.SameFile`
// （设备号 + inode），那是文件系统给的唯一身份。
//
// 在大小写**敏感**的卷上（Linux、Windows）那两个路径是两个不同的目录，各留一个
// 才是对的——所以先探一下这个卷的行为，再决定断言什么。
func TestTheTwoSpellingsOfBiuAreOneDirectory(t *testing.T) {
	root := t.TempDir()
	// ⚠️ 用 MkdirAll 而不是 Mkdir：在大小写不敏感的卷上（macOS），
	// 第二个 Mkdir("biu") 会因为 "Biu" 已经在而报 "file exists"。
	for _, name := range []string{"Biu", "biu", "biu-dev"} {
		if err := os.MkdirAll(filepath.Join(root, name), 0o755); err != nil {
			t.Fatal(err)
		}
		// 放一份数据，别让「目录存在但没数据」那条判据把它滤掉。
		mustWrite(t, filepath.Join(root, name, biuSettingsFile), biuAppSettings)
	}

	got := findBiuDirsUnder(root)
	if len(got) < 1 {
		t.Fatalf("一个都没找到：%v", got)
	}

	upper := filepath.Join(root, "Biu")
	lower := filepath.Join(root, "biu")
	same, err := os.Stat(upper)
	if err != nil {
		t.Fatal(err)
	}
	other, err := os.Stat(lower)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(same, other) {
		// 这个卷大小写敏感，两个路径是两个目录——都该留着。
		if len(got) != 3 {
			t.Errorf("大小写敏感的卷上列了 %d 条，want 3（Biu、biu、biu-dev 各一个）：%v", len(got), got)
		}
		return
	}

	seen := map[string]bool{}
	for _, p := range got {
		if seen[p] {
			t.Errorf("同一个目录列了两次：%v", got)
		}
		seen[p] = true
	}
	// 正式版排第一（设置页上按钮旁边写的就是它）。
	if !strings.HasSuffix(got[0], "Biu") {
		t.Errorf("正式版没有排在最前面：%v", got)
	}
}

// dev 版排在正式版**后面**。
//
// 那里面是开发调试时留下的数据，未必是用户想要的那一份；而自动导入取 `dirs[0]`
// ——它排在前面就意味着用户点的是哪一份，决定在他看见的那句话里。
func TestTheDevBuildDirectoryComesLast(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"Biu", "biu-dev"} {
		if err := os.MkdirAll(filepath.Join(root, name), 0o755); err != nil {
			t.Fatal(err)
		}
		mustWrite(t, filepath.Join(root, name, biuSettingsFile), biuAppSettings)
	}
	got := findBiuDirsUnder(root)
	if len(got) != 2 {
		t.Fatalf("列了 %d 条：%v", len(got), got)
	}
	if !strings.HasSuffix(got[0], "Biu") || !strings.HasSuffix(got[1], "biu-dev") {
		t.Errorf("顺序不对，正式版应该在 dev 版前面：%v", got)
	}
}

// 不存在的目录不出现。
func TestMissingDirectoriesDoNotShowUp(t *testing.T) {
	if got := findBiuDirsUnder(t.TempDir()); len(got) != 0 {
		t.Fatalf("一个空目录下列出了东西：%v", got)
	}
}
