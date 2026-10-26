package localmusic

import (
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// Trash 是「把一个路径移到系统废纸篓」这件事。
//
// # 为什么是接口
//
// 「怎么移到废纸篓」只有系统知道（macOS 是 NSFileManager 的
// `trashItemAtURL:`，Windows 是 IFileOperation 的 `fofxRecycleOnDelete`），
// 而这一层（service）不认识界面框架，也不该认识——所以**系统那一份实现
// 住在界面层**（`internal/ui/localmusic_trash.go`），这里只认「能把一个
// 路径移进废纸篓」这件事本身。测试用假的那一份（`internal/app` 里的
// `stubTrash` 与界面层测试里的那一枚）。
//
// 这个分工和文件监听那一份是同一个理由（`app.DirWatch` ↔
// `ui/mygoDirWatch`）：**框架那侧的 API 必须在应用的事件循环起来之后才能
// 调**（`mygo.WatchFiles` / `mygo.Shell.TrashItem` 都走 `needsApp`），
// 而 `service` 这一层不 import 框架。
//
// # 契约（三条，实现必须都守住）
//
//  1. **成功就意味着那个路径上已经没有那个东西了**——它现在在废纸篓里，
//     用户能从系统里找回来。
//
//  2. **失败必须把原文件留在原处**，而且**原样**留下。
//     框架那一侧的语义正是这样（`mygo.Shell.TrashItem` 的文档原话：无法进
//     回收站时它会失败并**把东西留在原地**）。
//
//  3. **不许有第二个出口**：这一层**不提供**「回收站不可用就永久删」这条
//     路。产品上那是从「不可恢复」改成「可恢复」的那一票（票 #558）——
//     悄悄回退到 `os.Remove` 等于把用户刚确认过的语义又换回去，而**界面上
//     一句话都不会变**（症状是静默的）。
type Trash interface {
	// TrashItem 把 path 移到系统废纸篓（Windows 那边叫回收站）。
	//
	// 失败时它**必须**把文件留在原处。返回的错误会被这一层认成
	// `ErrTrashFailed` / `ErrNotTrashable` / `ErrTrashNameConflict` /
	// `ErrDenied` 之一（见 `ClassifyTrashError`）。
	//
	// ⚠️ **失败的话里没有「文件已经不在了」这一档**：这一层分不清「系统
	// 说找不到它」和「它真的没了」，而**猜错的方向是往成功那边猜**——
	// 界面会说「已移到系统废纸篓」，而文件好好地躺在原处。所以「文件没了」
	// 由 `Delete` **自己再 stat 一次**来判（见 delete.go），不靠这句话。
	//
	// 实现给的是**系统原话**（归类在这一层做，只有**一处**会漏判）。
	TrashItem(path string) error
}

// 移到废纸篓失败时的两种错，界面据此说不同的话。
var (
	// ErrTrashFailed 是「没能移到废纸篓」。
	//
	// ⚠️ **它只说「没能」**：为什么没成由实现补在后面（系统的原话），
	// 而界面上那句「文件未删除」是**界面自己加的**
	// （`internal/ui/localmusic_actions.go`）——放在这一层的话，界面上
	// 会出现「文件未删除：文件未删除：…」这种两遍的话。
	ErrTrashFailed = errors.New("没能移到系统废纸篓")

	// ErrNotTrashable 是「那个位置根本没有废纸篓可以进」。
	//
	// 典型的那几种：网络盘、Windows 上没有回收站的盘（框架的
	// `internal/windows/trash.go` 明确把「本来会被永久删除」的那一次**拒掉**，
	// 宁可失败也不让文件被悄悄删掉）、系统压根不提供回收站。
	//
	// ⚠️ **这一档和 `ErrTrashFailed` 分开是有用的**：用户看到「那个位置没有
	// 回收站」能自己判断「那我就去文件管理器里删」；而看到「没能移到废纸篓」
	// 会以为是应用坏了。
	ErrNotTrashable = errors.New("系统不提供回收站（网络盘、跨卷或没有废纸篓）")

	// ErrTrashNameConflict 是「废纸篓里已经有同名的东西」。
	//
	// ⚠️ **多数情况下系统自己会处理**（macOS 会在废纸篓里改成「歌 2.mp3」，
	// Windows 也会自己加后缀），所以这条**很少**出现——但它一出现就是
	// 用户自己造成的（他在废纸篓里已经放过一个同名的东西），而系统的原话
	// 用户读不懂（`The file … already exists`）。
	ErrTrashNameConflict = errors.New("废纸篓里已经有同名的东西")

	// ErrDenied 是「系统不让我动它」。
	//
	// 典型的那几种：权限不够（Windows 0x80070005 `ERROR_ACCESS_DENIED`）、
	// 文件被别的程序锁着（0x80070005 也是它）、只读卷。
	//
	// ⚠️ **它和 `ErrTrashFailed` 分开是有用的**：用户看到「系统不让你动
	// 它」能自己去改权限或者关掉那个占着它的程序；看到「没能移到系统
	// 废纸篓」他会以为是应用坏了。
	ErrDenied = errors.New("系统不让你动这个文件（权限不够，或者它正被别的程序占用）")

	// ErrNoTrash 是「这个应用没装上回收站」。
	//
	// 它是**配置**上的错，不是系统给的：本地音乐那一摊没挂上回收站就
	// 有人来删文件了（正常生产路径上不会——`ui.MountLocalMusic` 一定装）。
	// 单独一档是因为它和「系统不提供回收站」要说的不是一件事：那个是
	// 「这个位置不行」，这个是「应用自己没装好」。
	ErrNoTrash = errors.New("这个应用没有装上系统回收站")
)

// ClassifyTrashError 把系统那一份原话翻成这一层的几种错。
//
// # 为什么需要它
//
// 三个平台的原话完全不一样（macOS 是 `localizedDescription`，Windows 是
// HRESULT 翻出来的一句，Linux 是 GIO 的 `GError`），而**用户读不懂它们**。
// 这一层把它们归成几档，界面才能说人话：
//
//	那个位置没有回收站  → 「文件未删除：这个位置没有回收站」
//	废纸篓里已经有同名的 → 「文件未删除：废纸篓里已经有同名的东西」
//	系统不让你动它      → 「文件未删除：系统不让你动这个文件」
//	别的                → 「文件未删除：没能移到系统废纸篓：<原因>」
//
// # 判据是**结构化的片段**，不是整句里的关键词
//
// ⚠️ **这一条是评审打回来的那一版血的教训。** 原来这里对**整段**系统原话
// 做子串匹配，于是：
//
//   - 一个叫「Not Found.mp3」的文件，回收站失败时整句里带着「not found」
//     → 被判成「文件已经不在了」→ 界面上说「已移到系统废纸篓」，而**文件
//     好好地躺在原处**（用户以为删掉了，下一次重扫它又回来了）；
//   - Windows 那边**所有**回收站失败的句子都带同一截前缀
//     「to the recycle bin」，于是 0x80070050（已存在）、0x80070005（权限
//     失败）、0x80070002（文件不存在）**全被归成「系统不提供回收站」**。
//
// 所以现在这一层只认三样**结构化的**东西：
//
//  1. **HRESULT 码**（Windows 那边是 `mygo: … failed (HRESULT 0x80070005)`）
//     ——一个码对一个意思，这是最靠得住的一档。
//  2. **框架自己写死的那一句**（`it would be deleted permanently`）——那是
//     「放进去就得永久删掉」，而这一层宁可不删也不做那件事。
//  3. **别的平台那几句**（`already exists` / `no trash` …），而且**先把路径
//     和引号里的内容摘掉**再找——文件名里带「not found」或者「already
//     exists」不许影响分类。
//
// ⚠️ **「文件已经不在了」不在这一层**：这一层分不清「系统说找不到它」和
// 「它真的没了」，而**猜错的方向是往成功那边猜**（界面会说「已移到系统
// 废纸篓」，而文件还在原处）。「它真的没了」由 `Delete` **自己再 stat
// 一次**来判（见 delete.go）。
//
// path 是那一条**被搬的路径**（`Delete` 传的是词法上的那一条）：摘掉它，
// 用户文件名的内容就不会参与分类。
//
// 漏判的后果是「用户看到一句英文」，不是「文件被删了」——安全线在别处
// （失败时文件一定还在原处）。关键词放在这里而不是散在界面层，是为了
// **只有一处**会漏判。
func ClassifyTrashError(err error, path string) error {
	if err == nil {
		return nil
	}
	// 已经归过档的：再归一次会把话说两遍。
	if errors.Is(err, ErrTrashFailed) || errors.Is(err, ErrNotTrashable) ||
		errors.Is(err, ErrTrashNameConflict) || errors.Is(err, ErrDenied) {
		return err
	}
	// ⚠️ **先把排版引号换成 ASCII 的**：macOS 的原话是
	// `The file “歌.mp3” already exists.`——那对引号是 U+201C/U+201D。
	msg := stripQuoted(normalizeQuotes(err.Error()))
	msg = stripPath(msg, path)
	low := strings.ToLower(msg)

	// 1. HRESULT 码：一个码对一个意思。
	if code, ok := hresultCode(low); ok {
		switch code {
		case hresultAccessDenied:
			return fmt.Errorf("%w：%v", ErrDenied, err)
		case hresultAlreadyExists:
			return fmt.Errorf("%w：%v", ErrTrashNameConflict, err)
		default:
			// ⚠️ **0x80070002（文件不存在）也归在这一档**，而**不是**
			// 「文件已经不在了」：这一层分不清「系统找不到它」和「它真的
			// 没了」，而猜错的方向是往成功那边猜。真相由 `Delete` 再 stat
			// 一次来定。
			return fmt.Errorf("%w：%v", ErrTrashFailed, err)
		}
	}

	// 2. 框架自己写死的那一句（`mygo/internal/windows/trash.go`）：
	// 「进不了回收站，放进去就得永久删掉」——那正是这一层宁可不删也不做
	// 的事。
	//
	// ⚠️ **判的是整句，不是「recycle bin」那一截**——Windows 那边**每一句**
	// 回收站失败的原话都带「to the recycle bin」，拿它当判据会把权限失败
	// 和已存在也一起吞进这一档。
	if strings.Contains(low, "would be deleted permanently") {
		return fmt.Errorf("%w：%v", ErrNotTrashable, err)
	}

	// 3. 别的平台那几句。
	switch {
	case hasAny(low, "already exists", "file exists", "同名", "重名"):
		return fmt.Errorf("%w：%v", ErrTrashNameConflict, err)
	case hasAny(low, "no trash", "trash is not available", "trash not available",
		"no trash directory", "operation not supported",
		"没有回收站", "没有废纸篓", "回收站不可用", "废纸篓不可用"):
		return fmt.Errorf("%w：%v", ErrNotTrashable, err)
	}
	return fmt.Errorf("%w：%v", ErrTrashFailed, err)
}

// Windows 那边几个 HRESULT 码（`mygo/internal/windows/com.go` 的
// `hresultError` 把它们翻成 `mygo: … failed (HRESULT 0x80070005)`）。
const (
	// hresultAccessDenied 是 `ERROR_ACCESS_DENIED`：权限不够，或者文件正被
	// 别的程序占着。
	hresultAccessDenied = 0x80070005
	// hresultAlreadyExists 是 `ERROR_ALREADY_EXISTS`：目标位置上已经有同名
	// 的东西（回收站里那个）。
	hresultAlreadyExists = 0x80070050
)

// hresultRe 认 HRESULT 那一句。
//
// ⚠️ **只认「HRESULT」后面跟着的那个码**：这一段是**结构化的**（框架那边
// 固定这么写），而整句里的其它字（路径、文件名）一个字都不参与分类。
var hresultRe = regexp.MustCompile(`hresult\s+(0x[0-9a-f]{1,8})`)

// hresultCode 从那句原话里取出 HRESULT 码。
func hresultCode(low string) (uint32, bool) {
	m := hresultRe.FindStringSubmatch(low)
	if m == nil {
		return 0, false
	}
	v, err := strconv.ParseUint(m[1][2:], 16, 32)
	if err != nil {
		return 0, false
	}
	return uint32(v), true
}

// stripQuoted 把引号里的内容摘掉。
//
// ⚠️ **摘的是内容，不是引号**：macOS 那句 `The file “Not Found.mp3” doesn't
// exist.` 摘完是 `The file "" doesn't exist.`——文件名里那串字一个都留不下
// 来，于是它**不许**影响分类。
var quotedRe = regexp.MustCompile(`"[^"]*"`)

func stripQuoted(s string) string {
	return quotedRe.ReplaceAllString(s, `""`)
}

// stripPath 把那一条被搬的路径（和它的文件名）从原话里摘掉。
//
// ⚠️ **摘文件名那一份也要摘**：macOS 原话里出现的是**只有文件名**的
// 形式（`The file “歌.mp3” …`），不是整条路径。
func stripPath(msg, path string) string {
	if path == "" {
		return msg
	}
	msg = strings.ReplaceAll(msg, path, "")
	if base := filepath.Base(path); base != "" && base != "." && base != string(filepath.Separator) {
		msg = strings.ReplaceAll(msg, base, "")
	}
	return msg
}

// normalizeQuotes 把排版引号换成 ASCII 的那几种。
//
// ⚠️ **只换引号**，别的（重音、连字）不换——我们要匹配的关键词里没有那些。
func normalizeQuotes(s string) string {
	return strings.NewReplacer(
		"’", "'", // 弯撇号 ’
		"‘", "'", // 弯撇号 ‘
		"“", `"`, // 左双引号 “
		"”", `"`, // 右双引号 ”
	).Replace(s)
}

// hasAny 报 text 里有没有这几句中的任何一句。
func hasAny(text string, subs ...string) bool {
	for _, s := range subs {
		if strings.Contains(text, s) {
			return true
		}
	}
	return false
}
