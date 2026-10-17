// Package localmusic 扫本地的音乐目录：从文件里读出元信息，按一条明确的
// 规则处理路径冲突与符号链接，并且能真的把文件从磁盘上删掉。
//
// # 扫一层，不递归
//
// 只读每个根目录**直接**里面的文件。Biu 上游的 `walk` 也是这样
// （`biu/electron/ipc/local-music.ts:17-27`，`if (entry.isDirectory()) continue`），
// 而 PLAN 里那句「要可控的目录深度（只扫一层）」说的就是它。
//
// 不递归还有一个副作用是好的：**目录里的符号链接永远不会被走进去**，
// 所以扫描不可能跑到用户没指给我们的地方去。要走进去得用户自己把那个
// 目录加进设置，那是他明确指的。
//
// # 元信息从文件里读，不是从名字猜的
//
// 时长和标题都来自文件本身（见 ProbeDuration / Title），读不出来就是零值，
// 界面上画成「-」。⚠️ **解码音频不等于读标签**：时长是从容器头里读出来的
// （moov/mvhd、FLAC 的 STREAMINFO、MP3 的帧头……），一个样本都没有解；标题
// 是从 ID3/MP4/FLAC/WAV 的标签块里读的。两件事都必须真的读文件——拿文件名
// 去掉扩展名当标题、或者拿播放器的时长冒充标签，都是在说一件假事。
//
// 格式（`Format`）取的是**扩展名**，因为「格式」这一栏在 Biu 那边说的就是
// 扩展名（`local-music.ts:63`，`path.extname(file).slice(1)`）；大小和创建
// 时间取的是 `stat`。这三样不花钱，每次重扫都重新取是对的。
//
// # 路径冲突与符号链接：三条明确的规矩
//
// 这一层是**唯一**做删除决策的地方，规矩写死在这里，界面层改不动它：
//
//  1. **身份按 realpath 算，展示和删除按列表里的原路径。**
//     身份（`Track.ID`）是 `md5(canonicalPath)`，canonical 是
//     `filepath.EvalSymlinks` 解出来的绝对路径——它让「同一个文件的两条
//     路径」算出同一个身份，于是去重之后界面上的行身份不会撞车。
//     但 `Track.Path` 是**目录里原本的那个路径**，删文件只删它。
//     两者混用就是把「删这个链接」变成「删链接指向的那个文件」，也就是删到
//     用户音乐库外面去了。
//
//  2. **同一个文件只列一行。** 根目录先按 realpath 去重（同一个文件夹被加了
//     两次、或者加了一个指向它的符号链接，都只算一个），条目再按 realpath
//     去重（`a.mp3` 和 `a.mp3 -> /别处/a.mp3` 只留先扫到的那个）。不去重的
//     后果不是「多一行」那么轻：两行共用一个身份，而框架会拿这个身份认行
//     的状态——选中一行两行都亮（见 widgets/list 的铁律四）。
//
//  3. **符号链接按链接本身对待。** 扫的时候只收「普通文件」和「指向普通
//     文件的符号链接」，指向目录的链接不算音乐（它是个目录）。删的时候用
//     `os.Remove`，POSIX 的 `unlink` 语义——**删链接，不删它指向的东西**。
//
// 这三条各自都有一条回归测试钉着（见 localmusic_test.go / delete_test.go）。

package localmusic

import (
	"crypto/md5"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"
)

// Extensions 是扫描白名单：只有这些扩展名的文件会进列表。
//
// ⚠️ **白名单不等于能播**。它照抄 Biu 上游那一份
// （`local-music.ts:10`：`.mp3 .flac .wav .m4a .aac .ogg .wma .aiff`），
// 所以里面有两个我们**播不了**的：`.wma` 是 PLAN §5.4 点名的那个真实缺口
// （待定，不计入排期），`.aif/.aifc` 是 PLAN 拍板保留的 AIFF（能播）。
//
// 少一个扩展名的症状是「能播却选不进来」，多一个的症状是「列表里躺着一堆
// 根本不是音乐的东西」。要改这里的时候对着 `platform.audioFilters` 核一遍
// （`internal/app/settings_test.go` 的 `biuKeys` 是同一份清单的另一半）。
var Extensions = []string{
	".mp3", ".flac", ".wav", ".m4a", ".aac", ".ogg", ".wma", ".aiff", ".aif", ".aifc",
}

// playableExts 是「列进来了、而且真的放得出声」的扩展名。
//
// 判据是解码器注册表（`internal/service/audio` 里那些 `RegisterFormat`），
// 不是这份白名单：WAV 和 MP3 由框架注册、FLAC 走 `flacdec`、MP4/AAC/AIFF
// 各自注册、Ogg Vorbis 由 `audio/vorbis` 注册，WMA 什么也没有。
//
// ⚠️ **列表里出现一个播不了的扩展名时，界面上必须说清楚**，不能让人以为
// 文件坏了（PLAN §5.4 的原话：「不要含糊过去」）。
var playableExts = map[string]bool{
	".mp3": true, ".flac": true, ".wav": true, ".m4a": true,
	".aac": true, ".ogg": true, ".aiff": true, ".aif": true, ".aifc": true,
}

// Playable 报这个格式能不能播。界面上给每一行加不加「不支持这个格式」就看它。
func Playable(ext string) bool { return playableExts[strings.ToLower(ext)] }

// Track 是一首被扫出来的本地音乐。
type Track struct {
	// ID 是这一项的身份，**必须稳定且唯一**（界面层的铁律四）。
	//
	// 它是 canonical 路径的 md5。这一条要的两件事它都有：
	//
	//   - **重扫之间不变。** 文件没动，重扫多少次身份都一样——而这才是界面
	//     上要紧的那一条：文件监听每有一个变动就重扫一遍，身份要是跟着变，
	//     用户选中��那一行每次刷新都跳走。（`TestIdentityIsStableAcrossRescans`）
	//   - **两个路径指向同一个文件时是同一个 ID。** 那种情况由 Scan 去重，
	//     列表里只会剩一行，所以不会出现两行共用一个身份。
	//
	// ⚠️ **改名（含把整个文件夹挪走）会换身份**，这是有意的：那一行记的
	// 是「这个路径上的这个文件」，用户改完名之后它就是另一项了，界面上
	// 让它重新排一次队是对的。Biu 上游也是这么算的（`local-music.ts:57`
	// 的 `md5(canonicalPath.toLowerCase())`）。
	ID string

	// Path 是**目录里原本的那个路径**，也是**唯一**会被删掉的路径。
	//
	// ⚠️ 它**不是** canonical：canonical 只用来算身份。删 canonical 会把
	// 「删掉这个链接」变成「删掉链接指向的那个文件」，也就是删到用户的音乐库
	// 外面去了（见包头那三条规矩）。
	Path string

	// Canonical 是**扫描那一刻** `Track.Path` 解析出来的真实路径。
	//
	// ⚠️ 它是删除前的**第二道**身份检查（见 `Delete`）：列表上那一行记的是
	// 「当时那个位置上的那个文件」，而 `Path` 只是一个位置。有人（或者某个
	// 脚本）在扫完之后把根目录换成指向别处的符号链接，再点删除时，
	// `Path` 解析出来已经是**库外**的另一个文件了——只查「在不在某个根下面」
	// 拦不住（根自己也变了），比对 Canonical 拦得住。
	//
	// 它同时也是 `ID` 的来源（`identityPath(Canonical)`），所以不用另算。
	Canonical string

	// Root 是它来自哪个根目录。界面上按目录筛选用它。
	Root string

	// Title 是**文件里的标题标签**。标签读不出来时是文件名（去掉扩展名），
	// 和 Biu 的 `toSafeTitle` 一样（`local-music.ts:31-34`）。
	Title string

	// Size 是字节数，`stat` 来的。
	Size int64

	// Format 是扩展名（小写，不含点）。它说的是**扩展名**，不是容器——
	// 一个装着 FLAC 的 `.m4a` 在这一栏写 `.m4a`，Biu 那边也一样。
	Format string

	// Duration 是**从文件头里读出来**的时长，一个样本都没解。
	//
	// 零值 = 文件里读不出来（不支持的容器、坏文件、变长码率的 MP3）。界面上
	// 画「-」，不拿 0 当「0 秒」。
	Duration time.Duration

	// Created 是文件的创建时间。系统给不出创建时间时退回修改时间，
	// 和 Biu 的 `stat.birthtimeMs || stat.mtimeMs` 一样。
	Created time.Time
}

// Playable 报这一首放不放得出声。见包级 `Playable`。
func (t Track) Playable() bool { return Playable(t.Format) }

// Meta 是「要花一次 IO 才拿得到」的那几项，重扫时靠它省掉重复读。
//
// 键是 canonical 路径（也就是 `Track.ID` 那一份）。判「这个文件是不是原来
// 那个」用的是 **大小 + 修改时间**：这两个变了就重读标签，不变就直接复用。
// 只看大小不够（改标签不改长度），只看时间也不够（有些盘上时间精度只有秒），
// 两个一起看才既省 IO 又不把旧的标签显示成新的。
type Meta struct {
	Size     int64
	ModTime  time.Time
	Title    string
	Duration time.Duration
}

// Result 是一次扫描的结果。
type Result struct {
	// Tracks 是扫出来的全部条目，按（根目录，路径）排好序。
	//
	// 排序是给用户看的，也给测试看的：文件系统返回的目录顺序在不同机器上
	// 不一样，不排的话「列表里第几首是 XXX」这种断言会时好时坏。
	Tracks []Track

	// Cache 是下一遍扫描要用的元信息缓存，直接传回 Scan 就行。
	Cache map[string]Meta

	// Err 是有目录读不到时的错。
	//
	// 一个目录读不到（没权限、外接盘没插、被删了）不该让整次扫描白跑，所以
	// 这里**收拢**成一个错、其余目录照常。Err 为 nil 表示全部读到了。
	Err error
}

// Scan 读一遍这些根目录，**每个只读一层**。
//
// prev 是上一遍的缓存（第一次传 nil）。返回值里的 `Cache` 留着传给下一遍，
// 于是只有真正变过的文件才会重新读标签。
//
// ⚠️ 这里是**阻塞 IO**（ReadDir + 每个文件的 stat + 读标签和容器头），
// 只能在后台 goroutine 里调。界面线程上一次几千个文件的 stat 就是界面卡住。
func Scan(roots []string, prev map[string]Meta) Result {
	res := Result{Cache: make(map[string]Meta, len(prev))}

	seenFile := map[string]bool{} // canonical 路径 -> 见过
	for _, root := range uniqueRoots(roots) {
		dir, err := readDir(root)
		if err != nil {
			// 收拢而不是立刻返回：另一个目录可能好好地��着。
			//
			// 用 errors.Join 是因为它保得住每一个：`errors.Is(err, fs.ErrNotExist)`
			// 对合并后的错照样答得上来，界面上就能分「外接盘没插」和「没权限」。
			res.Err = errors.Join(res.Err, fmt.Errorf("读不了目录 %s：%w", root, err))
			continue
		}
		for _, entry := range dir {
			track, ok := trackOf(root, entry, prev, res.Cache, seenFile)
			if !ok {
				continue
			}
			res.Tracks = append(res.Tracks, track)
		}
	}

	// (根目录, 路径)：同一个根里按文件名排，不同根之间按用户加的顺序排。
	slices.SortFunc(res.Tracks, func(a, b Track) int {
		if a.Root != b.Root {
			return strings.Compare(a.Root, b.Root)
		}
		return strings.Compare(a.Path, b.Path)
	})
	return res
}

// readDir 列一个目录的第一层。它单独一个函数是为了让「根不存在」和
// 「根里面某个文件 stat 不到」这两种错在处理上分得开。
func readDir(root string) ([]fs.DirEntry, error) {
	return os.ReadDir(root)
}

// uniqueRoots 把根目录按 realpath 去重，保持用户加的先后顺序。
//
// 同一个文件夹被加了两次（很常见：加了一次 `~/Music`，后来又加了一次
// `/Users/我/Music`），或者加了一个指向已加目录的符号链接——不去重的话
// 同一首歌会以两行出现，而两行共用一个身份。
func uniqueRoots(roots []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, r := range roots {
		// ⚠️ **只跳空串，绝不 TrimSpace**：目录名末尾带空格是完全合法的
		// （macOS/Linux 都许），而 `strings.TrimSpace("/Music ")` 变出来的
		// `/Music` 是**另一个文件夹**。裁一次的后果不是「读不到」：
		// 设置里那个带空格的目录被换成了隔壁那个，而删除那一侧的根检查
		// （`underAnyRoot`）也认了它——用户选的是 `Music `，文件却从
		// `Music` 里没的。
		if r == "" {
			continue
		}
		abs, err := filepath.Abs(r)
		if err != nil {
			abs = filepath.Clean(r)
		}
		key := identityPath(abs)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, abs)
	}
	return out
}

// trackOf 判断这一个目录项要不要收进列表，要的话把它的元信息读出来。
//
// prev 是上一遍的缓存（可以 nil），next 是这一遍要留下的缓存。
// ⚠️ 写的是 **next 不是 prev**：扫描期间用户看到的缓存不能被改，否则一次
// 扫到一半崩了会把上一遍好好的缓存弄成半份。顺带也把「这一遍已经不存在的
// 文件」从缓存里清掉了——`next` 是从零建的。
func trackOf(root string, entry fs.DirEntry, prev, next map[string]Meta, seen map[string]bool) (Track, bool) {
	name := entry.Name()
	ext := strings.ToLower(filepath.Ext(name))
	if !slices.Contains(Extensions, ext) {
		return Track{}, false
	}

	path := filepath.Join(root, name)
	info, err := os.Lstat(path)
	if err != nil {
		// 列表读到之后、stat 之前它没了（正在被拷进来 / 正在被删）。
		return Track{}, false
	}
	// 规矩 3：**只收普通文件和指向普通文件的符号链接**。
	//
	// 指向目录的链接不进（那是个目录，不是音乐）；设备、管道、套接字也不进。
	// 别的类型（块设备之类）在音乐目录里出现就是放错了东西。
	if info.Mode()&os.ModeSymlink != 0 {
		// 链接本身可能已经断了：指向不存在的路径时 Stat 报错。
		// 断掉的链接不进列表——它指着一首已经不存在的歌。
		target, err := os.Stat(path)
		if err != nil || !target.Mode().IsRegular() {
			return Track{}, false
		}
	} else if !info.Mode().IsRegular() {
		return Track{}, false
	}

	// 规矩 2：canonical 见过就跳过。
	canon, err := filepath.EvalSymlinks(path)
	if err != nil {
		// 解不出（比如中途被删了）就用清理过的绝对路径凑一个身份：宁可这一行
		// 的身份在极端情况下不稳，也不要因为解析失败把文件从列表里漏掉。
		canon = filepath.Clean(path)
	}
	id := identityPath(canon)
	if seen[id] {
		return Track{}, false
	}
	seen[id] = true

	// stat 的跟随链接那一份：大小和创建时间要的是**文件本身**的，不是链接的。
	target := info
	if info.Mode()&os.ModeSymlink != 0 {
		if st, err := os.Stat(path); err == nil {
			target = st
		}
	}

	track := Track{
		ID:        id,
		Path:      path,
		Canonical: canon,
		Root:      root,
		Size:      target.Size(),
		Format:    ext,
		Created:   Created(target),
	}

	// 大小 + 修改时间都没变就复用上一次的标签和时长：这两样才是要花 IO 的。
	cached, ok := prev[id]
	if ok && cached.Size == target.Size() && cached.ModTime.Equal(target.ModTime()) {
		track.Title, track.Duration = cached.Title, cached.Duration
		if track.Title == "" {
			track.Title = titleFromName(name)
		}
		next[id] = cached
		return track, true
	}

	track.Title = Title(path)
	if track.Title == "" {
		track.Title = titleFromName(name)
	}
	track.Duration = ProbeDuration(path)
	next[id] = Meta{
		Size:     target.Size(),
		ModTime:  target.ModTime(),
		Title:    track.Title,
		Duration: track.Duration,
	}
	return track, true
}

// titleFromName 是标签读不出来时的标题：文件名去掉扩展名。
//
// 和 Biu 的 `toSafeTitle`（`local-music.ts:31-34`）一致——它也是
// `metaTitle || path.parse(filePath).name`。
func titleFromName(name string) string {
	return strings.TrimSuffix(name, filepath.Ext(name))
}

// identityPath 是算 `Track.ID` 的那个键。
//
// ⚠️ 大小写不敏感的文件系统（macOS 的 APFS 默认、Windows 的 NTFS）上，
// `Song.mp3` 和 `song.mp3` 是同一个文件，身份必须一样——否则它们会以两个
// 身份出现，而列表里只该有一行。Linux 上文件系统本身区分大小写，就不能折，
// 折了会把两个真的不同的文件算成一个。
func identityPath(abs string) string {
	if caseInsensitiveFS {
		abs = strings.ToLower(abs)
	}
	sum := md5.Sum([]byte(abs))
	return hex.EncodeToString(sum[:])
}

// caseInsensitiveFS 报路径的大小写算不算同一个。
var caseInsensitiveFS = runtime.GOOS == "darwin" || runtime.GOOS == "windows"
