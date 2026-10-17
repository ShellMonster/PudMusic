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
	"slices"
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

// FindBiuDirs 找本机上 Biu 可能的数据目录，**正式版排在最前面**。
//
// 三条来源，按优先级：
//
//  1. 正式版：`app.getPath("userData")`（`biu/electron/utils.ts:13`），
//     macOS 上是 `~/Library/Application Support/Biu`，Windows 是 `%APPDATA%\Biu`，
//     Linux 是 `~/.config/Biu`。目录名来自 package.json 的 `productName`（"Biu"）。
//  2. 同名的小写目录（`…/biu`）：**macOS 上它和 `Biu` 是同一个目录**（默认的
//     APFS 大小写不敏感），Windows 和 Linux 上是两个不同的路径。
//  3. 开发版：同一个函数在 `isDev` 时拼的是 `app.getPath("appData")/biu-dev`
//     （`utils.ts:15`，`main.ts:30` 也会 `setPath` 到那儿）。装了开发版的人机器上
//     常有它，而那里面往往是**最新的数据**——但它也是**开发调试时留下的**，
//     未必是用户想要的那一份，所以排在正式版之后。
//
// ⚠️ **按「同一个文件」去重，不是按路径字符串**。真机上验出来过：`Biu` 和 `biu`
// 两个路径在 macOS 上 `os.Stat` 拿到的是同一个 inode，按字符串比会列两遍，而用户
// 在设置页上会看到两条一模一样的「从 … 导入」，导入报告里也会列两遍。
// `os.SameFile` 比的是设备号 + inode，那是文件系统给的唯一身份。
//
// 不存在的目录直接不出现。返回结果里的每一项都是真的存在的目录。
func FindBiuDirs() []string { return findBiuDirsUnder(biuConfigRoot()) }

// biuConfigRoot 是 Biu 那几个候选目录的共同父目录，也就是 Electron 的
// `app.getPath("appData")` 在各平台上的位置（`biu/electron/utils.ts:15`）。
func biuConfigRoot() string {
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
	if root == "" {
		return ""
	}
	return root
}

// findBiuDirsUnder 在一个已知的父目录下面找那几个候选。测试走这一条，
// 因为拼 `root` 那部分（`os.UserHomeDir` / 环境变量）在无头环境里没法替换，
// 而这里才是「同一个文件去重」「优先级排序」真正发生的地方。
func findBiuDirsUnder(root string) []string {
	// 顺序就是优先级：正式版先，同名的小写目录次之，dev 版最后。
	candidates := []string{
		filepath.Join(root, "Biu"),
		filepath.Join(root, "biu"),
		filepath.Join(root, "biu-dev"),
	}

	var out []string
	var infos []os.FileInfo
	for _, p := range candidates {
		st, err := os.Stat(p)
		if err != nil || !st.IsDir() {
			continue
		}
		// 同一个文件已经收过了。
		if slices.ContainsFunc(infos, func(seen os.FileInfo) bool { return os.SameFile(seen, st) }) {
			continue
		}
		infos = append(infos, st)
		out = append(out, p)
	}
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
	// 续传带不过来这件事**必须说出来**。不说的话用户在记录页上看到一堆从 Biu 来的
	// 「暂停」，按「继续」发现从头开始，而界面上没有任何一句话解释过为什么——
	// 而他刚刚亲手导入了这些记录。
	if len(out) > 0 {
		notes = append(notes,
			"下载记录带过来了，但**续传带不过来**：Biu 的半成品是分开的若干块文件，"+
				"都在它自己的临时目录里，这边盘上一个字节都没有。"+
				"所以没下完的那几首进度从 0% 开始；已经下完的那些文件是真的在那儿的。")
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
		// ⚠️ **不带 Chunks**，理由见 `biuTask.Chunks` 的注释。
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
