// 下载记录那一排筛选（原型 `PAGES.downloads` 的 `.filter-rail`）。
package ui

import (
	"strconv"

	"github.com/ShellMonster/PudMusic/internal/service/download"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/pud"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/space"
	"github.com/egoist/mygo/ui"
)

// downloadFilter 是那一排的档。
type downloadFilter int

const (
	// dlFilterAll 是「全部」：不筛。
	dlFilterAll downloadFilter = iota
	// dlFilterRunning 是「下载中」：排队、下载、暂停、交织中（原型 `run`：run / wait / paused）。
	dlFilterRunning
	// dlFilterDone 是「已完成」。
	dlFilterDone
	// dlFilterFailed 是「失败」。
	dlFilterFailed
)

// dlFilterCount 是有几档。
const dlFilterCount = 4

// dlFilterLabels 是那几档的名字，从左到右（原型：全部、下载中、已完成、失败）。
//
// ⚠️ **默认是「全部」**：升级上来的用户第一次打开这一页，看到的是他原来看到的整个队列。
var dlFilterLabels = [dlFilterCount]string{"全部", "下载中", "已完成", "失败"}

// label 是这一档的名字。认不出的档位说「全部」——和筛选的行为一样（认不出就不筛）。
func (f downloadFilter) label() string {
	if f < 0 || int(f) >= len(dlFilterLabels) {
		return dlFilterLabels[dlFilterAll]
	}
	return dlFilterLabels[f]
}

// matches 报这条记录算不算在这一档里。
func (f downloadFilter) matches(r download.Snapshot) bool {
	switch f {
	case dlFilterRunning:
		return r.Status != download.StatusDone && r.Status != download.StatusError
	case dlFilterDone:
		return r.Status == download.StatusDone
	case dlFilterFailed:
		return r.Status == download.StatusError
	}
	return true
}

// dlEmptyAfterFilter 是「筛完了，这一档什么都没有」时说的那两句话（原型）。
//
// ⚠️ **它和 `noDownloads` 必须是两句不同的话**：前者回答「我的歌去哪了」，后者回答
// 「怎么才能有」。
func dlEmptyAfterFilter(f downloadFilter) emptyState {
	title := "没有失败的下载"
	switch f {
	case dlFilterRunning:
		title = "没有进行中的下载"
	case dlFilterDone:
		title = "没有已完成的下载"
	}
	return emptyState{Title: title, Hint: "所有任务都处理好了。", Icon: iconEmptyDownload}
}

// viewFilter 画那一排筛选 Chip（原型 `.filter-rail`：上 24、缝 8，每枚 Chip 带等宽计数）。
func (d *downloadsPage) viewFilter(c *ui.Context) {
	ui.Row(c).Key("downloads:filter").FillWidth().Gap(space.Px8).Margin(space.Px24, space.None, space.None, space.None).
		Children(func() {
			for i, label := range dlFilterLabels {
				f := downloadFilter(i)
				n := 0
				for _, r := range d.all {
					if f.matches(r) {
						n++
					}
				}
				if pud.ChipCount(c, "downloads:chip:"+strconv.Itoa(i), label, strconv.Itoa(n), d.filter == f).Clicked() {
					d.filter = f
				}
			}
		})
}
