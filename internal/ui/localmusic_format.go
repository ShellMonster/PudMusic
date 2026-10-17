// 本地音乐页的「把一行变成字」和「筛出要画的那几行」：筛选、目录显示名、
// 大小/时长/日期的写法、逐窗口唤醒句柄。
//
// 单独一个文件是因为它们是**纯函数**（除了唤醒那两条要碰 `p`）：没有控件、
// 没有 `ui.Xxx`，于是能被单测直接调，也不用跟着画法那一段一起读。

package ui

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/service/localmusic"
	"github.com/egoist/mygo/ui"
)

// ─────────────────────────────────────────────────────────────────────────────
// 筛选与格式化
// ─────────────────────────────────────────────────────────────────────────────

// filter 按目录和关键词筛一遍。
//
// 关键词只匹配**标题**（Biu 也是 `item.title.toLowerCase().includes(...)`，
// `local-music.ts:34`）。匹配文件名会让「按标签命名、按文件名找」的用户找不到
// 东西，而这一页的标题本来就是从标签来的。
func (p *localPage) filter(in []localmusic.Track, roots []string) []localmusic.Track {
	kw := strings.ToLower(strings.TrimSpace(p.search))
	dir := p.currentRoot(roots)
	if dir == "" && kw == "" {
		return in
	}
	out := make([]localmusic.Track, 0, len(in))
	for _, tr := range in {
		if dir != "" && tr.Root != dir {
			continue
		}
		if kw != "" && !strings.Contains(strings.ToLower(tr.Title), kw) {
			continue
		}
		out = append(out, tr)
	}
	return out
}

// currentRoot 把当前选中的那个显示名换回路径。没选具体目录时给空串
// （「全部目录」不是一条真实的路径，返回它会让 `tr.Root != dir` 永远成立）。
func (p *localPage) currentRoot(roots []string) string {
	if p.dirLabel == allDirs {
		return ""
	}
	labels := dirLabels(roots)
	// labels[0] 是「全部目录」，所以第 i 个根的标签在 i+1。
	for i, r := range roots {
		if labels[i+1] == p.dirLabel {
			return r
		}
	}
	// 选中的那一档已经不在了（用户把目录移了）：当作「全部」。
	return ""
}

// dirLabels 是目录筛选的选项：先「全部目录」，然后每个根的显示名。
//
// ⚠️ `ui.Select` 拿到的 `selected` 就是选项字符串本身，所以选项得是**显示名**
// 而不是路径——整条 `/Users/我/Music` 摆进下拉框里没人读得下去。Biu 那边是
// `label: getDirName(dir)`（`local-music.ts:226`），同一个道理。
//
// 同名的两个目录（「音乐」和「A/音乐」）显示名撞在一起就分不清了，所以撞了
// 的那个往前多带一段。
func dirLabels(roots []string) []string {
	out := make([]string, 0, len(roots)+1)
	out = append(out, allDirs)
	for _, r := range roots {
		label := dirName(r)
		if slices.Index(out, label) >= 0 {
			label = dirName(filepathDir(r)) + "/" + label
		}
		out = append(out, label)
	}
	return out
}

// dirName 是目录的显示名：路径的最后一段。
//
// 和 Biu 的 `getDirName`（`local-music.ts:47-52`）一样先去掉末尾的斜杠再切
// ——用户在对话框里选到 `/Users/我/Music/` 的话，那一档不该显示成一个空名字。
func dirName(dir string) string {
	trimmed := strings.TrimRight(dir, `/\`)
	if trimmed == "" {
		return dir
	}
	if i := strings.LastIndexAny(trimmed, `/\`); i >= 0 {
		return trimmed[i+1:]
	}
	return trimmed
}

// filepathDir 是 `filepath.Dir` 的薄壳：这一页不想 import path/filepath 只是
// 为了两个字符串操作，而 `dirLabels` 只需要「去掉最后一段」。
func filepathDir(dir string) string {
	trimmed := strings.TrimRight(dir, `/\`)
	if i := strings.LastIndexAny(trimmed, `/\`); i > 0 {
		return trimmed[:i]
	}
	return trimmed
}

// fileSize 把字节数写成 Biu 那一栏的样子（`filesize` 包，默认十进制）。
func fileSize(n int64) string {
	const unit = 1000
	if n < unit {
		return strconv.FormatInt(n, 10) + " B"
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit && exp < 3; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "kMGT"[exp])
}

// durText 把时长写成 `m:ss` / `h:mm:ss`。读不出来给「-」。
//
// ⚠️ 零值给「-」而不是「0:00」：容器里读不出时长是常事（不支持的容器、
// 变长码率的 MP3），画成 0:00 是在说「这首歌一秒都没有」。
func durText(d time.Duration) string {
	if d <= 0 {
		return "-"
	}
	total := int(d.Round(time.Second) / time.Second)
	h, m, sec := total/3600, total/60%60, total%60
	if h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, m, sec)
	}
	return fmt.Sprintf("%d:%02d", m, sec)
}

// createdText 把创建时间写成日期。
//
// ⚠️ 给日期不给时刻：Biu 的 `formatMillisecond` 给完整时间戳，但那一格只有
// 140 宽，时刻会被截掉，而截掉一半的时间戳比只给日期更没用。
func createdText(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return t.Format("2006-01-02")
}

// unplayableFormats 报列表里出现了哪些我们放不了的扩展名（去重、排序）。
func unplayableFormats(tracks []localmusic.Track) []string {
	var out []string
	for _, tr := range tracks {
		if tr.Playable() || slices.Contains(out, tr.Format) {
			continue
		}
		out = append(out, tr.Format)
	}
	slices.Sort(out)
	return out
}

// isPlaying 报这一项是不是正在放。
func (p *localPage) isPlaying(tr localmusic.Track) bool {
	cur, ok := p.app.Queue().Current()
	if !ok {
		return false
	}
	return cur.Source == "local" && cur.Path == tr.Path
}

// ─────────────────────────────────────────────────────────────────────────────
// 唤醒
// ─────────────────────────────────────────────────────────────────────────────

// setWake 登记这一帧的窗口（每个窗口一条）。
func (p *localPage) setWake(svc ui.Services) {
	wake := svc.Invalidate // 方法值：一个窗口一条，所以每帧一个闭包不算多

	p.wakeMu.Lock()
	defer p.wakeMu.Unlock()
	p.frame++
	for i := range p.wakes {
		if p.wakes[i].svc == svc {
			p.wakes[i].wake, p.wakes[i].seen = wake, p.frame
			return
		}
	}
	if len(p.wakes) >= maxLocalWakes {
		// 挤掉最久没画的那一个：那个窗口已经不画了，句柄留着是空转。
		oldest := 0
		for i := range p.wakes {
			if p.wakes[i].seen < p.wakes[oldest].seen {
				oldest = i
			}
		}
		p.wakes = append(p.wakes[:oldest], p.wakes[oldest+1:]...)
	}
	p.wakes = append(p.wakes, localWake{svc: svc, wake: wake, seen: p.frame})
}

// invalidate 叫醒**所有**画过这一页的窗口。任何 goroutine 都能调。
//
// 🔴 铁律三：投递完必须唤醒，不唤醒就是「结果到了、画面停在旧帧」（实测 0 帧
// 刷新）；**只唤醒一个窗口就是另一个窗口停在旧帧**，所以是逐窗口通知。
// 这就是 `app.SetLocalWake` 的注释里说的那个「必须传进来的那个回调」。
func (p *localPage) invalidate() {
	p.wakeMu.Lock()
	wakes := slices.Clone(p.wakes)
	p.wakeMu.Unlock()

	for _, w := range wakes {
		if w.wake != nil {
			w.wake()
		}
	}
}
