// 把 `BiuScan` 读到的写进本应用的数据目录。
//
// ## 写在启动之前
//
// 四份文件全是**预迁移**：读一份、按我们自己的 schema 写回去。挂载（
// `OpenSettingsStore` / `newShortcutKeys` / `download.New`）随后读到的已经是
// 我们自己的形状，它们各自的 `read` 一行都不用改。
//
// 写成「先读再写」而不是「让挂载时顺手迁」，是因为挂载是四段互不相识的代码，
// 而一次导入要保证的是**「这四份是一次搬过来的」**——哪一份都没写的用户不该拿到
// 一个只搬了一半的目录。
//
// ## 覆盖还是并
//
//   - **设置**：覆盖，但**先备份**。用户已经用了一阵子 MyGo 再点「从 Biu 导入」，
//     现在的设置里有他自己调过的东西——不留一份 `.bak` 就直接抹掉，是这一票里
//     最坏的一种丢数据。
//   - **下载记录**：**并**（`download.MergeIntoQueue`），按 `<bvid>|<cid>` 去重。
//     两边都有的以这边为准：这是他在这个应用里刚点的状态。
//   - **歌词缓存**：逐个文件写，**不覆盖**已经有内容的（见 `writeLyricsCache`）。
//   - **快捷键**：覆盖，但先备份；实际写由 `internal/ui` 那边做（那份的存储在那
//     边，见 `biuimport.go` 的 `BiuScan.Shortcuts` 注释）。
package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ShellMonster/Biu_Go/internal/service/download"
)

// Imported 是一次导入的结果，逐项说清「哪样带过来了、带过来几条」。
//
// 界面上原样摆给用户。`Notes` 里那些是**没带过来的**部分（读不出来的、
// 故意不迁的）——只报成功不报跳过，用户会以为导入是全的。
type Imported struct {
	// Dir 是从哪儿导的。跟 `BiuScan.Dir` 一样，界面上要原样告诉用户。
	Dir string

	Settings  bool
	Shortcuts bool
	// Downloads 是并进去的条数（去重之后）。
	Downloads int
	// Lyrics 是写下去的条数。
	Lyrics int
	// Notes 是没带过来的部分，逐条一句人话。
	Notes []string
}

// Any 报有没有任何一样带过来。
//
// 全是零的话，用户看到的应该是「这台机器上没有可导入的 Biu 数据」而不是一份
// 四项全 0 的报告——后者看着像「导入跑了但什么都没进来」。
func (r Imported) Any() bool {
	return r.Settings || r.Shortcuts || r.Downloads > 0 || r.Lyrics > 0
}

// Import 把这次扫到的东西写进 dst（用户数据目录）。
//
// **不整体回滚**：一份读不出来的文件不该把已经搬过来的几十条下载记录也撤回去
// （见 `biuimport.go` 文件头）。每项各自成败，各自记一句话。
//
// **但每一项都要先问「目标那一份是不是来自更新的版本」**。用户把应用换回旧版
// 时整个数据目录一个字节都不该被写——这个承诺是升级那一半给出的（
// `ErrUpgradeFromNewer`、各 store 自己的只读标志），而导入是**另一条**写这个目录
// 的路：不在这里也问一遍，它就会在用户换回旧版之后往那个目录里塞一份 v1 的设置，
// 等用户升级回来时看到的是「我的设置怎么变了」。
func (s *BiuScan) Import(dst string) (Imported, error) {
	r := Imported{Dir: s.Dir, Notes: append([]string(nil), s.Notes...)}
	var firstErr error
	fail := func(format string, a ...any) {
		if firstErr == nil {
			firstErr = fmt.Errorf(format, a...)
		}
	}

	if s.HasSettings {
		switch err := importSettings(dst, s.Settings); {
		case errors.Is(err, ErrSettingsFromNewer):
			r.Notes = append(r.Notes, "你现在的设置来自更新的版本，这一版一个字都不会写回去，所以设置没有带过来。")
		case err != nil:
			fail("导入设置：%v", err)
			r.Notes = append(r.Notes, fmt.Sprintf("设置没能带过来：%v", err))
		default:
			r.Settings = true
		}
	}

	if n, err := importDownloads(dst, s.Downloads); err != nil {
		if !errors.Is(err, download.ErrQueueFromNewer) {
			fail("导入下载记录：%v", err)
			r.Notes = append(r.Notes, fmt.Sprintf("下载记录没能带过来：%v", err))
		} else {
			// 版本对不上：队列自己已经说了「没有动它」，这里补一句人话给用户。
			r.Notes = append(r.Notes, "你现在的下载记录来自更新的版本，这一版一个字都不会写回去，所以下载记录没有带过来。")
		}
	} else {
		r.Downloads = n
	}

	if n, err := importLyrics(dst, s.Lyrics); err != nil {
		fail("导入歌词缓存：%v", err)
		r.Notes = append(r.Notes, fmt.Sprintf("歌词缓存没能带过来：%v", err))
	} else {
		r.Lyrics = n
	}

	return r, firstErr
}

// importSettings 把 Biu 的设置写进我们自己的设置文件。
//
// **不是**把原始字节抄过去：那一份带着 Biu 的信封（`{"appSettings": {...}}`），
// 抄过去的话我们自己的写盘路径下次就会把它按 v0 读（`splitEnvelope` 的判据是
// 「有没有 appSettings 键」，两份都能认，但一份是我们写的、一份不是，下次改
// 任何一格都会重新写一遍——多一次没必要的往返）。
//
// 读用 `parseSettingsFile`（认 Biu 的两种形状）、写用 `writeSettingsFile`
// （按我们自己的 schema），中间隔着一次 `normalize`。
//
// **目标那一份来自更新的版本时返回 `ErrSettingsFromNewer` 并且不碰它**——哪怕
// 这次导入是用户在设置页上自己按下去的。理由同上：那个目录是「用户换回了旧版」
// 的证据，往里面写一份 v1 的东西会毁掉他升级之后的设置。
//
// 目标文件存在且是当前版本时先挪成 `.bak`（`backupFile`）。用户已经用了一阵子
// MyGo 再点导入，现在的设置里有他自己调过的东西。
func importSettings(dst string, raw []byte) error {
	path := SettingsPath(dst)
	if err := guardNewerTarget(path); err != nil {
		return err
	}

	s, from, err := parseSettingsFile(raw)
	if err != nil {
		return err
	}
	if err := applyMigrations(&s, from); err != nil {
		return err
	}
	s.normalize()

	if _, err := os.Stat(path); err == nil {
		backupFile(path)
	}
	return writeSettingsFile(path, s)
}

// guardNewerTarget 拦住「目标那一份来自更新的版本」。
//
// **只在真的读得出版本号时才拦**。目标文件存在但读不懂，那是**用户自己的**数据
// 坏了——那一份要走正常路径（先备份再覆盖），而不是拿「只读」当借口把它留在
// 一个用户看不懂的状态里。
func guardNewerTarget(path string) error {
	_, from, err := readSettingsForImport(path)
	if err != nil {
		return nil // 不存在，或者读不出版本号：都不构成「来自更新版本」
	}
	if from > SchemaVersion {
		return fmt.Errorf("%w：%s 是 v%d 的，我们只到 v%d",
			ErrSettingsFromNewer, filepath.Base(path), from, SchemaVersion)
	}
	return nil
}

// readSettingsForImport 读**目标**设置文件现在是什么版本。文件不存在时返回
// `os.ErrNotExist`。
func readSettingsForImport(path string) (Settings, int, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Settings{}, 0, err
	}
	return parseSettingsFile(raw)
}

// writeSettingsFile 把一份设置按我们自己的 schema 写下去（临时文件 + rename）。
//
// 设置落盘的原子写在 `writeFileAtomic`（`settings.go`），这里只管**信封**：
// `{"schemaVersion": N, "appSettings": {...}}`。
func writeSettingsFile(path string, s Settings) error {
	b, err := marshalSettingsFile(s)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return writeFileAtomic(path, b)
}

// importDownloads 把 Biu 的下载记录并进队列文件，返回**实际并进去的**条数。
//
// 返回值不是 `len(s.Downloads)`：队列里已经有同一首时不并（`MergeIntoQueue`
// 按 ID 去重），而用户看到的报告应该说的是「新增了几条」——「带了 30 条」而
// 界面上多了 28 行，用户会去队列里找那两条，找不到。
func importDownloads(dst string, tasks []*download.Task) (int, error) {
	if len(tasks) == 0 {
		return 0, nil
	}
	path := filepath.Join(dst, download.QueueFileName)
	before, err := queueTaskCount(path)
	if err != nil {
		return 0, err
	}
	if err := download.MergeIntoQueue(path, tasks); err != nil {
		return 0, err
	}
	after, err := queueTaskCount(path)
	if err != nil {
		return 0, err
	}
	return after - before, nil
}

// queueTaskCount 数一份队列文件里有几条。数不出来就当 0——它只用来算差值，
// 差值算错了只会让报告里的数字不好看，不会弄丢任何东西。
func queueTaskCount(path string) (int, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return 0, nil
		}
		return 0, err
	}
	var probe struct {
		Tasks []json.RawMessage `json:"tasks"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		return 0, nil
	}
	return len(probe.Tasks), nil
}

// importLyrics 把歌词写进 `dst/lyrics-cache/<bvid>-<cid>.json`。
//
// **已有内容的文件不覆盖**。理由是歌词缓存在这一版还没有别的来源
// （#21 才做），所以「已经有一份」要么是从上一次导入来的（内容一样，覆不覆
// 都一样），要么是用户手改过的偏移/字号（那是他调出来的，导入不该抹掉）。
func importLyrics(dst string, entries []LyricsEntry) (int, error) {
	if len(entries) == 0 {
		return 0, nil
	}
	dir := filepath.Join(dst, LyricsCacheDirName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return 0, err
	}
	n := 0
	var firstErr error
	for _, e := range entries {
		path := filepath.Join(dir, LyricsKey(e.Bvid, e.Cid)+".json")
		if st, err := os.Stat(path); err == nil && st.Size() > 0 {
			continue
		}
		b, err := json.MarshalIndent(e, "", "  ")
		if err != nil {
			firstErr = firstErrElse(firstErr, err)
			continue
		}
		if err := writeFileAtomic(path, b); err != nil {
			firstErr = firstErrElse(firstErr, err)
			continue
		}
		n++
	}
	return n, firstErr
}

// firstErrElse 记下第一个错，后面的不覆盖它。
func firstErrElse(first, next error) error {
	if first != nil {
		return first
	}
	return next
}
