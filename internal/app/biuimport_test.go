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

// **分块记账一个都不搬**，因为 Biu 的分块文件不在我们盘上。
//
// 这是被评审验出来的：这一票原先把 Biu 的 `done: true` 换算成我们的分块下标，
// 而两边的「分块」根本不是同一个东西——Biu 把每一块写成独立文件
// （`join(tempDir, "audio.part1")`，`download-core.ts:387`），我们是并发 3 写
// **同一个** `.part`。带上一份「盘上并不存在的分块记账」，等于声称前 N 块已经下好了：
// 按「继续」时续传跳过那些下标，拼出来是一个坏文件。
//
// 而不跳过的那条路也不通：我们不知道 Biu 那边下的是哪一档（`Identity.Quality`
// 是零值），`SameResource` 会把记账清空从头重下——那一道拦住的是坏文件，代价是
// Biu 里下到 80% 的歌从头再来。
//
// 所以诚实地不带：记录和路径带过来，进度从 0% 开始。
func TestNoChunkBookkeepingComesOverBecauseThoseBytesAreNotOnOurDisk(t *testing.T) {
	f := newBiuFixture(t).put(t, biuDownloadsFile, biuDownloadsJSON)
	dst := t.TempDir()
	r, err := mustScan(t, f).Import(dst)
	if err != nil {
		t.Fatal(err)
	}
	tasks := readQueue(t, filepath.Join(dst, download.QueueFileName))
	if len(tasks) != 2 {
		t.Fatalf("队列里有 %d 条", len(tasks))
	}
	for _, task := range tasks {
		if len(task.Chunks) != 0 {
			t.Errorf("「%s」带了一���分块记账 %v，而盘上那些字节并不存在", task.Title, task.Chunks)
		}
	}
	// 记录本身带过来了——路径、标题、状态、身份全在。
	// 「已下完」的那些文件是真的在盘上的（`savePath` 指着它），那条记录有意义。
	var done, half *download.Task
	for i := range tasks {
		if tasks[i].Status == download.StatusDone {
			done = &tasks[i]
		} else {
			half = &tasks[i]
		}
	}
	if done == nil || half == nil {
		t.Fatalf("状态没翻过来：%v", tasks)
	}
	if done.Dest == "" || half.Dest == "" {
		t.Errorf("保存路径没带过来：%+v", tasks)
	}
	// 而这件事要告诉用户，不能让他以为「从 Biu 带过来的下载都能续传」。
	if !saysSomethingAbout(r.Notes, "续传") {
		t.Errorf("没有说「续传带不过来」：%v", r.Notes)
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

// 「已经有一份来自更新版本的队列」时不并，也不碰它，而且**把这件事说给用户听**。
//
// 往一份我们读不懂的文件上动手，比什么都不做糟得多。而用户看到的是一句
// 「你现在的下载记录来自更新的版本」而不是笼统的「没带过来」——前者告诉他
// 「你换回旧版了」，后者让他以为出了别的毛病。
func TestAQueueFromANewerVersionIsLeftAloneAndSaidOutLoud(t *testing.T) {
	dst := t.TempDir()
	path := filepath.Join(dst, download.QueueFileName)
	mustWrite(t, path, `{"version":99,"tasks":[]}`)

	f := newBiuFixture(t).put(t, biuDownloadsFile, biuDownloadsJSON)
	r, _ := mustScan(t, f).Import(dst)
	if got := mustRead(t, path); !strings.Contains(got, `"version":99`) {
		t.Fatalf("那份文件被改了：%s", got)
	}
	if r.Downloads != 0 {
		t.Fatalf("报告说并进去了 %d 条", r.Downloads)
	}
	if !saysSomethingAbout(r.Notes, "来自更新的版本") {
		t.Fatalf("没有把「来自更新版本」说出来：%v", r.Notes)
	}
}

// 「已经有一份来自更新版本的设置」时：不换、**不备份**、不碰，并把话说清楚；
// 别的照样带（它们各有各的判据）。
//
// 覆盖 + 留一份 `.bak` 看起来像「我们很小心」，实际是把那份文件换成了 v1——
// 用户升级回来时看到的是他的设置被一个旧版本改写过。每一项各自判，是因为四份
// 文件的「只读」是四份独立的承诺，导入要一条一条兑现。
func TestSettingsFromANewerVersionAreNotSwappedOutButTheRestStillComesOver(t *testing.T) {
	dst := t.TempDir()
	mine := `{"schemaVersion":` + itoa(SchemaVersion+5) + `,"appSettings":{"primaryColor":"#123456"}}`
	mustWrite(t, SettingsPath(dst), mine)

	f := newBiuFixture(t).
		put(t, biuSettingsFile, biuAppSettings).
		put(t, biuDownloadsFile, biuDownloadsJSON)
	r, _ := mustScan(t, f).Import(dst)

	if got := mustRead(t, SettingsPath(dst)); got != mine {
		t.Fatalf("那份设置被换掉了：\n改前 %s\n改后 %s", mine, got)
	}
	if _, err := os.Stat(SettingsPath(dst) + ".bak"); err == nil {
		t.Error("连备份都留了一份：那是把「用户换回旧版」当成了一次正常导入")
	}
	if r.Settings {
		t.Error("报告说设置带过来了")
	}
	if !saysSomethingAbout(r.Notes, "来自更新的版本") {
		t.Errorf("没有把「来自更新版本」说出来：%v", r.Notes)
	}
	// 别的照样带：它们各有各的判据，不该被设置的只读连坐。
	if r.Downloads != 2 {
		t.Errorf("下载记录也没带过来（%d 条）：%v", r.Downloads, r.Notes)
	}
	if r.Lyrics != 0 && r.Lyrics != 2 {
		t.Errorf("歌词条数不对：%d", r.Lyrics)
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
