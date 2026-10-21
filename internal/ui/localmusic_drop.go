// 把从访达 / 资源管理器拖进窗口的文件接住，落进本地音乐。
//
// # 为什么落点是「文件所在的目录」
//
// 本地音乐的模型是**一组根目录、每个只扫一层**（`service/localmusic` 包头
// 那三条规矩），没有「单个文件」这种东西。所以「拖一个文件进来」能复用的
// 最轻一条路，就是把它的父目录当成一个根加进去——和用户点「添加目录」
// 选中的是同一个东西。代价是**同目录下的其它音频也会一起出现**，这一句
// 必须说给用户听（`dropAddedText`），不能让他以为只进来了一首。
//
// 另一条路是「只登记被拖的那几个文件」，那要给扫描换一套输入模型、给删除
// 规则补一种「不在任何根下面的文件」，票面说的「技术上很轻」就不成立了。
// 这一票选了前者（issue #144 的裁决）。
//
// # 为什么只有主窗口挂
//
// 挂载点在 `Shell.Attach`——那是窗口生命周期回调唯一的入口（见它的注释）。
// 迷你播放器那一扇走 `attachMiniWindow`，它不画本地音乐页、也没有画 toast
// 的地方，所以不挂；拖到那一扇上什么都不会发生，和这一票之前一样。
package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/ShellMonster/Biu_Go/internal/service/localmusic"
)

// dropReject 是「拖进来的这样东西没收」和原因。
type dropReject struct {
	name string
	why  string
}

// dropPlan 是「把这几个拖进来的路径变成什么」的结果。
//
// added 是**要加进设置**的目录（已去重、绝对路径）；known 是本来就在库里
// 的目录；rejected 是没收的每一样东西。rescan 是「至少有一个路径是音频或
// 目录」——目录不用加也得重扫一遍，才看得见刚放进那个目录的文件。
type dropPlan struct {
	added    []string
	known    []string
	rejected []dropReject
	rescan   bool
}

// planFileDrop 只看路径，不碰设置、不碰界面，于是能直接在测试里喂临时目录。
//
// roots 是设置里现有的目录。判断「在不在库里」按 **realpath** 比：
// 同一个文件夹用相对路径、绝对路径或者符号链接写出来都是同一个根，而
// `localmusic.Scan` 也是这么去重的（`uniqueRoots`）。
func planFileDrop(paths, roots []string) dropPlan {
	var p dropPlan
	seen := make(map[string]bool, len(roots)+len(paths))
	for _, root := range roots {
		if root == "" {
			continue
		}
		seen[canonicalDir(root)] = true
	}
	// addedNow 是「这一趟里刚加过」的目录。它和 seen 分开记，是为了不在同一次
	// 拖放里把「刚加的那个目录」又说成「已经在库里了」——一次拖进来同一目录下
	// 的两首歌，第二首会走到 seen 那一支。
	addedNow := make(map[string]bool, len(paths))
	known := make(map[string]bool, len(paths))
	rejected := make(map[string]bool, len(paths))

	reject := func(path, why string) {
		if rejected[path] {
			return
		}
		rejected[path] = true
		p.rejected = append(p.rejected, dropReject{name: filepath.Base(path), why: why})
	}

	for _, path := range paths {
		if path == "" {
			continue
		}
		info, err := os.Stat(path)
		if err != nil {
			// 拖进来的那一刻还在、处理的时候没了（拷一半被取消、刚被删），
			// 或者那个位置根本没权限读。两种都说一句——静默跳过的话用户
			// 只会觉得「拖了没反应」。
			if os.IsNotExist(err) {
				reject(path, "找不到了")
			} else {
				reject(path, "读不了")
			}
			continue
		}
		dir := path
		if !info.IsDir() {
			if !info.Mode().IsRegular() {
				// 设备、管道、套接字——不是音乐，也不是目录。
				reject(path, "不是普通文件")
				continue
			}
			ext := strings.ToLower(filepath.Ext(path))
			if !slices.Contains(localmusic.Extensions, ext) {
				reject(path, "不是支持的格式（支持 "+strings.Join(localmusic.Extensions, " ")+"）")
				continue
			}
			dir = filepath.Dir(path)
		}

		key := canonicalDir(dir)
		p.rescan = true
		if seen[key] {
			if !addedNow[key] && !known[key] {
				known[key] = true
				p.known = append(p.known, dir)
			}
			continue
		}
		seen[key] = true
		addedNow[key] = true
		// 存**没用 EvalSymlinks 的绝对路径**：用户拖的是哪条路径，设置里就
		// 记哪条——和「添加目录」存对话框给的那个路径是同一个语义。realpath
		// 只用来比「是不是同一个根」。
		p.added = append(p.added, absPath(dir))
	}
	return p
}

// canonicalDir 是比「是不是同一个目录」用的键。
//
// 解不出符号链接不算错（外接盘没插、目录刚被删）：退到绝对路径，至少
// 相对路径和绝对路径的写法能对上。
func canonicalDir(path string) string {
	abs := absPath(path)
	if real, err := filepath.EvalSymlinks(abs); err == nil {
		return real
	}
	return abs
}

// absPath 是「能绝对就绝对」，失败时退到 Clean 而不是原样返回。
func absPath(path string) string {
	if abs, err := filepath.Abs(path); err == nil {
		return abs
	}
	return filepath.Clean(path)
}

// handleFileDrop 接住一次拖放。**界面线程调**（`OnFileDrop` 在主线程上发）。
func (s *Shell) handleFileDrop(paths []string) {
	if len(paths) == 0 {
		return
	}
	if s.app.SettingsReadOnly() {
		// 只读时 `UpdateSettings` 会失败，但错误码对用户没有意义。这里先说
		// 清楚为什么，免得他拖了半天以为程序坏了。
		s.addToast("设置只读：拖进来的文件加不进本地音乐")
		return
	}

	roots := s.app.SettingsSnapshot().LocalMusicDirs
	plan := planFileDrop(paths, roots)
	if len(plan.added) > 0 {
		next := append(append([]string(nil), roots...), plan.added...)
		if _, err := s.app.UpdateSettings(func(st *app.Settings) {
			st.LocalMusicDirs = next
		}); err != nil {
			s.addToast("存不住这次拖放：" + err.Error())
		} else {
			// 换根会自己重扫（见 `App.SetLocalRoots` 的注释），不用再补一次
			// `RescanLocalMusic`——那会让几千个文件白扫一遍。
			s.app.SetLocalRoots(next)
			s.addToast(dropAddedText(plan.added))
		}
	} else if plan.rescan {
		// 目录已经在库里（或者在这次拖放里刚加过）：文件可能是刚放进去的，
		// 重扫一遍才看得见。`RescanLocalMusic` 是幂等的，界面那一帧也会
		// 再对一次根。
		s.app.RescanLocalMusic()
	}
	if len(plan.known) > 0 {
		s.addToast(dropKnownText(plan.known))
	}
	if len(plan.rejected) > 0 {
		s.addToast(dropRejectedText(plan.rejected))
	}
}

// dropAddedText 说「加进来了」，并且**必须**带上「同目录下的音频也会出现」
// ——用户拖的是一首，列表里多出来的可能是一整张专辑。
func dropAddedText(dirs []string) string {
	return "已加入本地音乐：" + dirNamesText(dirs) +
		"，正在扫描（同目录下的音频都会出现）"
}

// dropKnownText 说「本来就在库里」。不说的话用户会以为拖放没生效。
func dropKnownText(dirs []string) string {
	if len(dirs) == 1 {
		return "「" + dirName(dirs[0]) + "」已经在本地音乐里，正在重新扫描"
	}
	return fmt.Sprintf("%d 个目录已经在本地音乐里，正在重新扫描", len(dirs))
}

// dropRejectedText 说清楚**为什么**没收。只说「不支持」不够——用户手里是
// 一个 .txt，得知道本地音乐到底收什么。
func dropRejectedText(rejects []dropReject) string {
	// 一条拖放里很可能是同一个原因的一堆文件，全列出来会把 toast 撑爆。
	// 列两条，其余说个数；原因本身每一条都带着，所以不会漏掉「为什么」。
	const shown = 2
	parts := make([]string, 0, shown)
	for i, r := range rejects {
		if i == shown {
			break
		}
		parts = append(parts, "「"+r.name+"」"+r.why)
	}
	text := "没放进本地音乐：" + strings.Join(parts, "；")
	if len(rejects) > shown {
		text += fmt.Sprintf("；另有 %d 个", len(rejects)-shown)
	}
	return text
}

// dirNamesText 把目录名串成一句话，超过 max 个只列前 max 个。
func dirNamesText(dirs []string) string {
	const max = 3
	names := make([]string, 0, len(dirs))
	for _, dir := range dirs {
		names = append(names, dirName(dir))
	}
	if len(names) <= max {
		return "「" + strings.Join(names, "」「") + "」"
	}
	return "「" + strings.Join(names[:max], "」「") + "」等 " + fmt.Sprintf("%d 个目录", len(names))
}
