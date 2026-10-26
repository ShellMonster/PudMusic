package localmusic

import (
	"errors"
	"fmt"
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
	// `ErrTrashFailed` / `ErrNotTrashable` / `ErrMissing` 之一
	// （见 `ClassifyTrashError`）。
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
//	文件已经不在了      → 界面上不报错，把那一行从列表里拿掉
//	那个位置没有回收站  → 「文件未删除：这个位置没有回收站」
//	废纸篓里已经有同名的 → 「文件未删除：废纸篓里已经有同名的东西」
//	别的                → 「文件未删除：没能移到系统废纸篓：<原因>」
//
// ⚠️ **判据是那一句原话里的关键词**：系统那边改文案的话这一层就会漏判
// （漏判的后果是「用户看到一句英文」，不是「文件被删了」——安全线还在）。
// 关键词放在这里而不是散在界面层，是为了**只有一处**会漏判。
func ClassifyTrashError(err error) error {
	if err == nil {
		return nil
	}
	// 已经归过档的：再归一次会把话说两遍。
	if errors.Is(err, ErrTrashFailed) || errors.Is(err, ErrNotTrashable) ||
		errors.Is(err, ErrTrashNameConflict) || errors.Is(err, ErrMissing) {
		return err
	}
	// ⚠️ **先把排版引号换成 ASCII 的**：macOS 的原话是
	// `The file “歌.mp3” doesn’t exist.`——那个撇号是 **U+2019**（弯引号），
	// 而关键词写的是 ASCII 的 `doesn't`。不换的话 macOS 上「文件已经不在了」
	// 那一档**一条都命中不了**（实测：2026-10-26 的原生冒烟跑出来的正是
	// `doesn’t exist`，于是它掉进「别的」那一档，被说成「没能移到废纸篓」——
	// 而真相是文件已经不在了，该做的是把那一行从列表里拿掉）。
	msg := strings.ToLower(normalizeQuotes(err.Error()))
	switch {
	case hasAny(msg, "no such file", "doesn't exist", "does not exist",
		"cannot find", "not found", "找不到", "不存在"):
		// 文件在检查和搬走之间没了（多半是用户在别的窗口里刚删的）。
		// 那不是失败：界面上该做的是把那一行从列表里拿掉。
		return fmt.Errorf("%w：%v", ErrMissing, err)
	case hasAny(msg, "would be deleted permanently", "recycle bin",
		"no trash", "回收站", "废纸篓"):
		// 系统说「这里没有回收站，放进去就得永久删掉」——那正是这一层
		// 宁可不删也不做的事。
		return fmt.Errorf("%w：%v", ErrNotTrashable, err)
	case hasAny(msg, "already exists", "file exists", "同名", "重名"):
		return fmt.Errorf("%w：%v", ErrTrashNameConflict, err)
	default:
		return fmt.Errorf("%w：%v", ErrTrashFailed, err)
	}
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
