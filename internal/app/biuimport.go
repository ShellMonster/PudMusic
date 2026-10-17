// 从 Biu 首次导入：设置、下载任务、歌词缓存。
//
// # 为什么不是「只有设置能迁」
//
// 旧版 PLAN 从「Biu 把一部分数据存在 localStorage（LevelDB）里」推出「只有设置能
// 自动迁移」。那是错的：`biu/shared/store/index.ts:1-6` 列出 **5 个 electron-store
// 文件，全都在 `<UserData>/` 下、都是明文 JSON**（只有 `user-login-info` 带
// `encryptionKey`）。其中三个在这一票里迁：
//
//	| Biu 的文件            | 内容     | 这一票                          |
//	|----------------------|----------|---------------------------------|
//	| app-settings.json    | 应用设置 | ✅ 迁（`splitEnvelope` 已经认它）|
//	| shortcut-settings.json | 快捷键 | ✅ 迁（在 `internal/ui` 那边，它本来就认这个文件）|
//	| media-downloads.json | 下载任务 | ✅ 迁（`biuDownloads` 逐条翻译）  |
//	| lyrics-cache.json    | 歌词缓存 | ✅ 迁（`lyrics-cache/<bvid>-<cid>.json`）|
//	| user-login-info.json | 登录态   | ❌ **不迁**，提示重新登录          |
//
// 不迁登录态是**产品决定**，不是技术做不到：那份文件是 electron-store 的
// `encryptionKey` 加密的（conf 的 AES-CBC，密钥由固定字符串 + 机器 id 派生），
// 我们既不该复制那个加密方案，也不该替用户把一份密文搬到自己名下的钥匙串里。
// 而就算搬得过来，Biu 的 cookie 域与 MyGo 的请求头构造也不完全一致——
// 用户重新扫一次码是三十秒的事，比我们猜一套「搬过来可能能用」的语义可靠。
//
// # 失败怎么处理
//
// **逐项跳过，不整体回滚**。设置读不出来不该让下载记录也进不来——它们之间没有
// 任何依赖，一项失败不该连累别的（`Notes` 里逐条记着人话，见 `BiuScan.Notes`）。
//
// 但**已经写进去的不回滚**。理由是这份数据本来就只读别人的文件、只往自己的目录
// 写一份新的，而用户随时可以「清空下载记录」或改回设置——为了一个坏掉的文件把
// 半个导入撤回去，比留下半个导入更难解释。
//
// # 哪些文件是「明文可读」的
//
// 判据只有一条：**能不能 `json.Unmarshal` 成预期形状**。这一层不认 `__internal__`
// 那一段（conf 的内部状态，标准库自动忽略），也不依赖 Biu 的任何一个内部模块。
package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"

	"github.com/ShellMonster/Biu_Go/internal/service/download"
)

// Biu 的四个明文文件名。照 `biu/shared/store/index.ts:1-6` 的 `StoreNameMap`，
// electron-store 会自己在后面加 `.json`。
const (
	biuSettingsFile  = "app-settings.json"
	biuShortcutsFile = "shortcut-settings.json"
	biuDownloadsFile = "media-downloads.json"
	biuLyricsFile    = "lyrics-cache.json"
	// biuLoginFile 是**不解**的那一份。名字列出来是为了让「我们看到了它、决定不搬」
	// 这件事在代码里明着，而不是靠一条注释。
	biuLoginFile = "user-login-info.json"
)

// BiuScan 是从一个 Biu 数据目录里读到了什么。
//
// 零值可用（=「什么都没有」），但**通常该看 nil 还是非 nil**：目录不存在时
// `ScanBiu` 返回 nil，指针。
type BiuScan struct {
	// Dir 是读的哪个目录。界面上要把它原样告诉用户——「从哪儿导的」这件事，
	// 用户出问题时第一句就会问。
	Dir string

	// Settings 是 Biu 的 `app-settings.json` 的**原始字节**。
	//
	// 刻意不解析成 `Settings`：那份文件有两种形状（带 `appSettings` 信封的
	// electron-store 文件、以及设置页「导出配置」下载的裸文件），认它们的那套
	// 判据是 `splitEnvelope`，而那套判据本来就要在**落地的位置**上再走一遍
	// （落地之后要按我们自己的 schema 写回，不能把 Biu 的原始字节抄进去）。
	Settings []byte
	// HasSettings 报 Settings 有没有东西。空 slice 和「文件在但是空的」要分开。
	HasSettings bool

	// Shortcuts 是 Biu 的 `shortcut-settings.json` 的原始字节。同样不解析：
	// 那份的解析在 `internal/ui`（`readShortcuts`），而 `internal/app` 不能 import
	// `internal/ui`（那边 import 这边，反过来就是环）。
	Shortcuts    []byte
	HasShortcuts bool

	// Downloads 是从 `media-downloads.json` 翻出来的下载任务，**已经是**我们自己的
	// `download.Task`——翻译在 `convertBiuTask` 里就地做完，不留中间类型。
	// 翻不出、翻不了的那些在 `Notes` 里。
	Downloads []*download.Task

	// Lyrics 是从 `lyrics-cache.json` 读到的歌词，逐条翻译成了我们自己的形状。
	// 空的（既没有原文也没有翻译）已经在读的时候丢掉了——写一个空文件下去只会
	// 占地方，还会���歌词页以为「缓存过了，别去搜了」。
	Lyrics []LyricsEntry

	// LoggedIn 报 Biu 那边有没有登录态（那份加密文件在不在、有没有内容）。
	//
	// 它只用来决定**要不要提示重新登录**，不携带任何凭据本身：一个字节都不读
	// 进来（见文件头）。
	LoggedIn bool

	// Notes 是「读不进来的」「故意不迁的」，逐条一句人话。
	//
	// 界面上原样摆给用户。空 slice 表示这一份全读进来了。
	Notes []string
}

// FindBiuDirs 找本机上 Biu 可能的数据目录，**从最可能的排在前面**。
//
// 三条来源，按优先级：
//
//  1. 正式版：`app.getPath("userData")`（`biu/electron/utils.ts:13`），
//     macOS 上是 `~/Library/Application Support/Biu`，Windows 是 `%APPDATA%\Biu`，
//     Linux 是 `~/.config/Biu`。目录名来自 package.json 的 `productName`（"Biu"）。
//  2. 开发版：同一个函数在 `isDev` 时拼的是 `app.getPath("appData")/biu-dev`
//     （`utils.ts:15`，`main.ts:30` 也会 `setPath` 到那儿）。装了开发版的人机器上
//     常有它，而那里面往往是**最新**的数据。
//  3. 同名的小写目录（`~/Library/Application Support/biu`）：macOS 的文件系统
//     大小写不敏感，`Biu` 和 `biu` 在同一个卷上是同一个目录，去重时按**路径
//     字符串**比会漏掉。
//
// 不存在的目录直接不出现。返回结果永远**不为空判断而靠 nil**——调用方拿到的
// 每一项都是真的存在的目录。
func FindBiuDirs() []string {
	seen := map[string]bool{}
	add := func(p string) {
		if p == "" || seen[p] {
			return
		}
		if st, err := os.Stat(p); err != nil || !st.IsDir() {
			return
		}
		seen[p] = true
	}

	var root string
	switch runtime.GOOS {
	case "windows":
		root = os.Getenv("APPDATA")
	default:
		// macOS 和 Linux 都是 `~/.config` 那种布局在 `XDG_CONFIG_HOME`，
		// macOS 则是 Application Support；两边分别在下面拼。
		root = os.Getenv("XDG_CONFIG_HOME")
		if runtime.GOOS == "darwin" {
			home, err := os.UserHomeDir()
			if err == nil {
				root = filepath.Join(home, "Library", "Application Support")
			}
		}
	}
	if root == "" {
		if home, err := os.UserHomeDir(); err == nil {
			root = filepath.Join(home, ".config")
		}
	}

	add(filepath.Join(root, "Biu"))
	add(filepath.Join(root, "biu-dev"))
	add(filepath.Join(root, "biu"))

	out := make([]string, 0, len(seen))
	for p := range seen {
		out = append(out, p)
	}
	// map 的遍历顺序是随机的。导入报告里会逐条列目录名，顺序不稳的话同一个问题
	// 每次刷出来的顺序都不一样，看起来像随机行为。
	sort.Strings(out)
	return out
}

// HasBiuData 报一个目录看起来是不是装过 Biu。
//
// 判据是「四个明文文件里至少有一个在」——只看 `app-settings.json` 会漏掉只用
// 歌下过、没改过设置的用户。
func HasBiuData(dir string) bool {
	for _, n := range []string{biuSettingsFile, biuShortcutsFile, biuDownloadsFile, biuLyricsFile} {
		if st, err := os.Stat(filepath.Join(dir, n)); err == nil && st.Mode().IsRegular() {
			return true
		}
	}
	return false
}

// ScanBiu 读一个 Biu 数据目录。
//
// 目录不存在或者里面没有 Biu 的数据时返回 nil——那不是错误，是「这台机器上
// 没有可导的 Biu」。
//
// 读不出来的**单份**文件记进 `Notes` 然后跳过，不让整个导入失败：设置那份坏了
// 不该让下载记录也进不来。
func ScanBiu(dir string) (*BiuScan, error) {
	if dir == "" || !HasBiuData(dir) {
		return nil, nil
	}
	s := &BiuScan{Dir: dir}

	if b, ok := readBiuFile(dir, biuSettingsFile, s); ok {
		s.Settings, s.HasSettings = b, true
	}
	if b, ok := readBiuFile(dir, biuShortcutsFile, s); ok {
		s.Shortcuts, s.HasShortcuts = b, true
	}
	if b, ok := readBiuFile(dir, biuDownloadsFile, s); ok {
		s.Downloads, s.Notes = biuDownloads(b, s.Notes)
	}
	if b, ok := readBiuFile(dir, biuLyricsFile, s); ok {
		s.Lyrics, s.Notes = parseBiuLyrics(b, s.Notes)
	}

	// 登录态只**看文件在不在**，一个字节都不读（它是 conf 的密文，读了也只是
	// 一堆随机数，见文件头）。
	if st, err := os.Stat(filepath.Join(dir, biuLoginFile)); err == nil && st.Size() > 0 {
		s.LoggedIn = true
		s.Notes = append(s.Notes,
			"Biu 那边是登录状态。Biu 的登录信息是加密存的，这一版不搬过来——请在这个应用里重新登录一次。")
	}
	return s, nil
}

// readBiuFile 读一个 Biu 的明文文件，顺带验它是不是 JSON。
//
// 读不出来（不存在 / 打不开 / 不是 JSON）时往 notes 里记一句并返回 false。
// `ok=false` 不带 error：一份坏文件不是整个导入的失败，是这一项跳过。
func readBiuFile(dir, name string, s *BiuScan) (b []byte, ok bool) {
	path := filepath.Join(dir, name)
	raw, err := os.ReadFile(path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			s.Notes = append(s.Notes, fmt.Sprintf("Biu 的 %s 读不出来（%v），这一项没导。", name, err))
		}
		return nil, false
	}
	// 先探是不是 JSON 再交出去：一份内容是乱码的文件**照样能被交出去**，
	// 而丢在下游变成一句「解析错误」的话，用户不知道是哪一份出的问题。
	var probe json.RawMessage
	if err := json.Unmarshal(raw, &probe); err != nil {
		s.Notes = append(s.Notes, fmt.Sprintf("Biu 的 %s 不是 JSON（%v），这一项没导。", name, err))
		return nil, false
	}
	return raw, true
}

// ─────────────────────────────────────────────────────────────────────────────
// 下载任务
// ─────────────────────────────────────────────────────────────────────────────

// biuTask 是 Biu 的 `MediaDownloadTask`（`biu/shared/types/download.d.ts:29`）
// 加上 `FullMediaDownloadTask` 里我们要的那几格（`electron/ipc/download/types.ts`）。
//
// 字段全部是可选的：Biu 自己写出来的记录也是缺这格少那格，而**带问号的那些
// 缺了就是缺了**，不该在这里补默认值把它们伪装成有。
type biuTask struct {
	ID              string `json:"id"`
	Status          string `json:"status"`
	OutputFileType  string `json:"outputFileType"`
	Title           string `json:"title"`
	Cover           string `json:"cover"`
	Bvid            string `json:"bvid"`
	Cid             any    `json:"cid"`
	CreatedTime     int64  `json:"createdTime"`
	SavePath        string `json:"savePath"`
	TotalBytes      int64  `json:"totalBytes"`
	AudioTotalBytes int64  `json:"audioTotalBytes"`
	AudioCodecs     string `json:"audioCodecs"`
	Error           string `json:"error"`
	Chunks          []struct {
		Type string `json:"type"`
		End  int64  `json:"end"`
		Done bool   `json:"done"`
	} `json:"chunks"`
}

// biuChunkSize 是 Biu 一个分块多大。10 MB。
//
// 照抄 `electron/ipc/download/download-core.ts:371` 的 `chunkSize = 10 * 1024 *
// 1024`，和我们这边 `task.go` 的 `chunkSize` 同一个数——**必须一样**，否则换算
// 出来的分块下标指向的是别的字节范围，续传会把两段不同的字节拼在一起。
//
// ⚠️ 它和 `download.chunkSize` 是两个私有常量，两边各一份：那边不导出是为了不让
// 别的包把分块大小当成自己的事去算，而这边要用它做**换算**（外来下标 → 我们的
// 下标），必须知道那个数。真要改分块大小的话，这两处要一起改。
const biuChunkSize = 10 << 20

// biuDownloads 把 Biu 的下载记录翻成我们的任务。
//
// 形状是 `Record<string, FullMediaDownloadTask>`：key 是任务 id，值是任务本身
// （`electron/store.ts:31`）。遍历用 map 而不是数组——Biu 的 `syncMediaDownloadTaskList`
// 那条链上是按 id 存的，读的时候也按 id。
func biuDownloads(raw []byte, notes []string) ([]*download.Task, []string) {
	var byID map[string]biuTask
	if err := json.Unmarshal(raw, &byID); err != nil {
		return nil, append(notes, fmt.Sprintf("Biu 的下载记录读不懂（%v），这一项没导。", err))
	}

	ids := make([]string, 0, len(byID))
	for id := range byID {
		ids = append(ids, id)
	}
	// map 的遍历顺序是随机的，而导入后的队列顺序是**界面上从上到下的顺序**。
	// 不排的话同一份 Biu 数据导两次，队列里的歌顺序不一样，看着像随机行为。
	// 按创建时间排，一样的歌两次导入就是一样的顺序。
	sort.Slice(ids, func(i, j int) bool {
		a, b := byID[ids[i]], byID[ids[j]]
		if a.CreatedTime != b.CreatedTime {
			return a.CreatedTime < b.CreatedTime
		}
		return ids[i] < ids[j]
	})

	out := make([]*download.Task, 0, len(ids))
	skipped := 0
	for _, id := range ids {
		t, ok := convertBiuTask(byID[id])
		if !ok {
			skipped++
			continue
		}
		out = append(out, t)
	}
	if skipped > 0 {
		notes = append(notes, fmt.Sprintf(
			"Biu 的下载记录里有 %d 条没能带过来（视频任务、认不出是哪一首的、或没写保存路径的）。", skipped))
	}
	return out, notes
}

// convertBiuTask 翻一条。ok=false 是「这一条带不过来」。
//
// 三种带不过来的，各有各的理由，都不是「读错了」：
//
//  1. **视频任务**（`outputFileType == "video"`）。这一版只下音频，直接落盘，
//     没有交织（见 `download/store.go` 文件头）。把一条视频任务放进来，界面上
//     会显示一个按「继续」只会从头下一遍音频的行——那是骗用户。
//  2. **认不出是哪一首**（没有 bvid/cid）。`download.Task.ID` 是
//     `<bvid>|<cid>`，没有它就既去不了重也找不到要写的文件。
//  3. **没写保存路径**（`savePath` 空）。Biu 那边正在下的任务还没算出来，
//     而没有路径就写不了盘；带过来是一条按不动的死行。
func convertBiuTask(t biuTask) (*download.Task, bool) {
	if t.OutputFileType != "" && t.OutputFileType != "audio" {
		return nil, false
	}
	bvid, cid := t.Bvid, biuCidString(t.Cid)
	if bvid == "" || cid == "" {
		return nil, false
	}
	if t.SavePath == "" {
		return nil, false
	}

	length := t.TotalBytes
	if length <= 0 {
		length = t.AudioTotalBytes
	}
	return &download.Task{
		ID:      download.TaskID(bvid, cid),
		Title:   t.Title,
		Cover:   t.Cover,
		Label:   biuQualityLabel(t.AudioCodecs),
		Dest:    t.SavePath,
		Status:  biuStatusOf(t.Status),
		Err:     t.Error,
		AddedAt: t.CreatedTime,
		Identity: download.Identity{
			Bvid: bvid, Cid: cid, Length: length,
		},
		Chunks: biuChunks(t.Chunks, length),
	}, true
}

// biuCidString 把 Biu 的 `string | number` 那个 cid 变成我们的字符串形式。
//
// Biu 的类型是 `cid?: string | number`，而我们队列里的 cid 一律是字符串
// （`download.Identity.Cid` 的注释）。数字的按十进制写，字符串的**去掉空白**——
// Biu 那边从接口拿到的字符串有时带着空格，而同一个 cid 带不带空格是两个不同的键，
// 去重就失效了。
func biuCidString(v any) string {
	switch c := v.(type) {
	case string:
		return strings.TrimSpace(c)
	case float64:
		// json 里的数字都解成 float64。Cid 是十几位数（int64 之内），
		// float64 能精确表示到 2^53，够用；但走 strconv 而不是 fmt.Sprintf("%v")
		// 是因为后者对 float64 会写成 "1.234e+11" 这样的科学计数法。
		return strconv.FormatInt(int64(c), 10)
	case json.Number:
		return c.String()
	}
	return ""
}

// biuStatus 把 Biu 的状态翻成我们的。
//
// Biu 有九个状态，我们有六个（`download/task.go`）。「在飞」的那几个
// （`downloading` / `merging` / `converting`）**都落成暂停**，不是 running——
// 迁移完���那个在飞的请求早就不在了，记成 running 会让界面上**永远转圈**
// （`store.go` 的 `Restore` 为这件事专门有一段注释）。
//
// 表里没有的（未来 Biu 加了新的、或者文件里是空串）一律落成暂停：那是一个用户
// 能按「继续」救回来的行，而空状态或没见过的状态在记录页上是什么都不画的空行。
var biuStatus = map[string]download.Status{
	"waiting":        download.StatusQueued,
	"downloading":    download.StatusPaused,
	"downloadPaused": download.StatusPaused,
	"merging":        download.StatusPaused,
	"mergePaused":    download.StatusPaused,
	"converting":     download.StatusPaused,
	"convertPaused":  download.StatusPaused,
	"completed":      download.StatusDone,
	"failed":         download.StatusError,
}

// biuStatusOf 翻一个状态，兜底是暂停。见 `biuStatus` 那张表的注释。
func biuStatusOf(s string) download.Status {
	if v, ok := biuStatus[s]; ok {
		return v
	}
	return download.StatusPaused
}

// biuQualityLabel 把 Biu 的 `audioCodecs` 翻成记录页上的徽章文字。
//
// 认不出就留空：一个空的徽章只是不画那一格，一个猜错的徽章会让用户以为下的是
// 另一档音质。
func biuQualityLabel(codecs string) string {
	switch strings.ToLower(strings.TrimSpace(codecs)) {
	case "flac", "hi-res":
		return "无损 FLAC"
	case "320k", "320kbps", "320":
		return "320K"
	case "128k", "128kbps", "128":
		return "128K"
	}
	return ""
}

// biuChunks 把 Biu 的分块换算成我们的分块下标。
//
// Biu 记的是**字节区间**（`{start, end, done}`，`electron/ipc/download/types.ts`），
// 我们记的是**分块下标**（`download.Task.Chunks`）——见 `task.go` 文件头：按前缀
// 长度记在并发写下是不准的，只有「哪几块齐了」是确定的事实。
//
// 两边的块都是 10 MB（`biuChunkSize` 和 `download.chunkSize`），所以下标 =
// `end / 块大小`。用 `end` 而不是 `start`：Biu 的 `end` 是**闭区间**的最后一个
// 字节，用 `start` 的话第一块会算成 0、下标 -1。
//
// 换算出来的下标要落在范围内（`start/块大小 <= 下标 < 块数`），越界的丢��——
// `Restore` 的 `keepOnRestore` 会因为同样的理由丢掉越界的记账，而在**导入**这一层
// 就丢掉，用户看到的报告里就没有一条「明明在 Biu 里下到 80% 却没带过来」的死行。
func biuChunks(in []struct {
	Type string `json:"type"`
	End  int64  `json:"end"`
	Done bool   `json:"done"`
}, length int64) []int {
	if length <= 0 {
		// 总长都不知道就没法判断下标合不合法，全丢。
		return nil
	}
	total := (length + biuChunkSize - 1) / biuChunkSize
	seen := map[int64]bool{}
	out := make([]int, 0, len(in))
	for _, c := range in {
		if !c.Done || (c.Type != "" && c.Type != "audio") {
			continue
		}
		idx := c.End / biuChunkSize
		if idx < 0 || idx >= total || seen[idx] {
			continue
		}
		seen[idx] = true
		out = append(out, int(idx))
	}
	// 队列里的分块是「完成了哪些」，顺序没有语义，但排一下让落盘的文件两次
	// 导入结果逐字一样（评审和排障时能直接 diff 两份文件）。
	sort.Ints(out)
	return out
}

// ─────────────────────────────────────────────────────────────────────────────
// 歌词缓存
// ─────────────────────────────────────────────────────────────────────────────

// LyricsCacheDirName 是歌词缓存目录。照 `PLAN.md` §3.4 的持久化表。
const LyricsCacheDirName = "lyrics-cache"

// LyricsEntry 是一首歌的歌词。Biu 的 `MusicLyrics`
// （`biu/shared/types/lyrics.d.ts`）+ 身份（见文件头为什么加 bvid/cid）。
//
// 键名和 Biu 逐字一致（`lyrics` / `tLyrics` / `offset` / `fontSize`），这样
// 这份缓存写回 Biu 那边也是认的——用户在两个应用之间来回切，歌词不丢。
type LyricsEntry struct {
	Bvid string `json:"bvid"`
	Cid  int64  `json:"cid"`
	// Lyrics 是歌词原文（时间轴格式的 lrc）。
	Lyrics string `json:"lyrics,omitempty"`
	// TLyrics 是翻译。Biu 那边叫 `tLyrics`，不是 `translationLyrics`。
	TLyrics string `json:"tLyrics,omitempty"`
	// Offset 是用户自己调的偏移（毫秒，可以是负的）。
	Offset int `json:"offset,omitempty"`
	// FontSize 是用户自己调的字号。
	FontSize int `json:"fontSize,omitempty"`
}

// LyricsKey 是这份缓存在 Biu 那边的主键：`<bvid>-<cid>`
// （`biu/src/components/lyrics/index.tsx:194` 的 `${playItem.bvid}-${playItem.cid}`）。
//
// ⚠️ 分隔符是**横线**不是别的：Biu 那边用横线，写回它就要用横线；而 bvid 本身
// 一定含横线（`BV1xx411c7mD` 这种格式里就有），cid 是数字，所以这个键**没有歧义**。
func LyricsKey(bvid string, cid int64) string {
	return bvid + "-" + strconv.FormatInt(cid, 10)
}

// parseBiuLyrics 把 Biu 的歌词缓存读成一条条，顺带把读不出来的记上。
//
// 形状是 `Record<string, MusicLyrics>`（`electron/store.ts:42`）：key 是
// `<bvid>-<cid>`，值是 `{fontSize?, offset?, lyrics?, tLyrics?}`。
//
// 关键在**用 key 而不是值来认身份**：值里根本没有 bvid/cid（那两样在 key 里），
// 靠值拼不出文件名，也就写不出 `lyrics-cache/<bvid>-<cid>.json`。key 认不出来
// （不是 `<bvid>-<cid>` 这个形状）的那条跳过——猜出来的文件名会把歌词写到错误的
// 歌上，而用户看不出来（歌词页翻到别的歌还是能放）。
func parseBiuLyrics(raw []byte, notes []string) ([]LyricsEntry, []string) {
	var byKey map[string]LyricsEntry
	if err := json.Unmarshal(raw, &byKey); err != nil {
		return nil, append(notes, fmt.Sprintf("Biu 的歌词缓存读不懂（%v），这一项没导。", err))
	}
	keys := make([]string, 0, len(byKey))
	for k := range byKey {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	out := make([]LyricsEntry, 0, len(keys))
	bad := 0
	for _, k := range keys {
		bvid, cid, ok := splitLyricsKey(k)
		if !ok {
			bad++
			continue
		}
		v := byKey[k]
		// 既没有原文也没有翻译的等于没有：写一个空文件下去只会占地方，
		// 还会让歌词页以为「缓存过了，别去搜了」。
		if v.Lyrics == "" && v.TLyrics == "" {
			bad++
			continue
		}
		out = append(out, LyricsEntry{
			Bvid: bvid, Cid: cid,
			Lyrics: v.Lyrics, TLyrics: v.TLyrics,
			Offset: v.Offset, FontSize: v.FontSize,
		})
	}
	if bad > 0 {
		notes = append(notes, fmt.Sprintf("Biu 的歌词缓存里有 %d 条读不出来，已经跳过。", bad))
	}
	return out, notes
}

// splitLyricsKey 把 `<bvid>-<cid>` 拆开。cid 是数字，所以从**最后**一个横线切。
func splitLyricsKey(k string) (string, int64, bool) {
	i := strings.LastIndex(k, "-")
	if i <= 0 || i == len(k)-1 {
		return "", 0, false
	}
	cid, err := strconv.ParseInt(k[i+1:], 10, 64)
	if err != nil {
		return "", 0, false
	}
	bvid := k[:i]
	if !strings.HasPrefix(bvid, "BV") && !strings.HasPrefix(bvid, "av") {
		return "", 0, false
	}
	return bvid, cid, true
}
