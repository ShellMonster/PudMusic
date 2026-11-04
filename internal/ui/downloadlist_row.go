// 下载记录的一行（原型 `.drow`）：
//
//	封面 48 | 文件名 2fr | 大小或进度条 1.4fr | 状态 110 | 操作 120
//
// 列与列隔 16、内边距 12、底下一条 1 像素 line；窗口宽 ≤ 760 时去掉大小 / 进度那一列。
package ui

import (
	"github.com/ShellMonster/PudMusic/internal/service/download"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/imgcache"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/pud"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/scale"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/space"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/tok"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/typo"
	"github.com/egoist/mygo/ui"
	"strconv"
)

const (
	dlCover    = 48  // `.drow .t-cover`
	dlStatusW  = 110 // 状态列
	dlOpsW     = 120 // 操作列
	dlHideBar  = 760 // 窗口宽 ≤ 760（原型 `@media (max-width: 760px)`）不画大小 / 进度那一列
	dlProgH    = 4   // `.prog` 高
	dlStatusPt = 6   // `.status::before` 圆点直径
)

// viewRow 画一行。w 是列表宽。
func (d *downloadsPage) viewRow(c *ui.Context, s *Shell, r download.Snapshot, w, winW float32) {
	p := tok.Of(c.Theme())
	narrow := winW <= dlHideBar
	ui.Row(c).Key("dl:" + r.ID).Width(w).Padding(space.Px12).Gap(space.Px16).AlignItems(ui.Center).
		Draw(func(pp *ui.Painter, rc ui.Rect) {
			pp.Fill(ui.Rect{X: rc.X, Y: rc.Y + rc.H - 1, W: rc.W, H: 1}, p.Line, 0)
		}).
		Children(func() {
			ui.Box(c).Size(dlCover, dlCover).Radius(tok.R4).Background(p.Surface2).Clip().Shrink(0).
				Children(func() { imgcache.ViewTitled(c, s.images, r.Cover, r.Title, dlCover, dlCover) })

			ui.Column(c).Grow(2).Shrink(1).Children(func() {
				ui.Text(c, r.Title).FontSize(scale.Font(c, scale.Size14)).FontWeight(500).TextColor(p.Text).
					SingleLine().Ellipsis("…")
				if r.Label != "" {
					ui.Text(c, r.Label).FontSize(scale.Font(c, scale.Size12)).TextColor(p.Text2).
						SingleLine().Ellipsis("…")
				}
			})

			if !narrow {
				ui.Column(c).Grow(1.4).Shrink(1).Justify(ui.Center).Children(func() {
					if r.Status == download.StatusDone {
						typo.Mono(ui.Text(c, fileSize(r.Total))).FontSize(scale.Font(c, scale.Size12)).
							TextColor(p.Text2).SingleLine()
						return
					}
					frac := float32(0)
					if r.Percent > 0 {
						frac = float32(r.Percent) / 100
					}
					ui.Box(c).FillWidth().Height(dlProgH).Label(r.Title + " 下载进度").
						Draw(func(pp *ui.Painter, rc ui.Rect) {
							pp.Fill(rc, p.Surface2, dlProgH/2)
							if frac > 0 {
								pp.Fill(ui.Rect{X: rc.X, Y: rc.Y, W: rc.W * frac, H: rc.H}, p.Accent, dlProgH/2)
							}
						})
				})
			}

			d.viewStatusPud(c, r)

			ui.Row(c).Width(dlOpsW).Shrink(0).Justify(ui.End).Gap(space.Px4).AlignItems(ui.Center).
				Children(func() { d.viewOpsPud(c, s, r) })
		})
}

// viewStatusPud 是状态列（原型 `.status`：12 号、前面一枚 6 的圆点，圆点和字同色、缝 6）：
// 完成 ok、下载中 accent-text 加等宽百分比、排队 / 暂停 text-3、失败 err。
func (d *downloadsPage) viewStatusPud(c *ui.Context, r download.Snapshot) {
	p := tok.Of(c.Theme())
	var col ui.Color
	var label string
	mono := ""
	switch r.Status {
	case download.StatusDone:
		col, label = p.Ok, "下载完成"
	case download.StatusRunning:
		col = p.AccentText
		if r.Percent >= 0 {
			mono = strconv.Itoa(r.Percent) + "%"
		} else {
			label = "准备中"
		}
	case download.StatusQueued:
		col, label = p.Text3, "排队中"
	case download.StatusPaused:
		col, label = p.Text3, "下载暂停"
		if r.Percent >= 0 {
			mono = strconv.Itoa(r.Percent) + "%"
		}
	case download.StatusMuxing:
		col, label = p.Text3, "交织中"
	default:
		col = p.Err
		label = "失败"
		if r.Err != "" {
			label += " · " + r.Err
		}
	}
	ui.Row(c).Width(dlStatusW).Shrink(0).Gap(space.Px6).AlignItems(ui.Center).Children(func() {
		ui.Box(c).Size(dlStatusPt, dlStatusPt).Radius(dlStatusPt / 2).Background(col).Shrink(0)
		if mono != "" && r.Status == download.StatusRunning {
			typo.Mono(ui.Text(c, mono)).FontSize(scale.Font(c, scale.Size12)).TextColor(col).SingleLine()
			return
		}
		ui.Row(c).Shrink(1).Gap(space.Px4).Children(func() {
			t := ui.Text(c, label).FontSize(scale.Font(c, scale.Size12)).TextColor(col).Shrink(1)
			if r.Status == download.StatusError && r.Err != "" {
				// 失败那一格带着原因：整句要看得见（票 #241 F-19），110 宽的状态列里折行、不截成省略号。
				t.MaxLines(3)
			} else {
				t.SingleLine().Ellipsis("…")
			}
			if mono != "" {
				typo.Mono(ui.Text(c, "· "+mono)).FontSize(scale.Font(c, scale.Size12)).TextColor(col).SingleLine()
			}
		})
	})
}

// viewOpsPud 是操作列（原型 `ops`）：失败 = 小幽灵键「重试」，完成 = 打开键，暂停 = 继续键，
// 其余 = 暂停键；最后一枚是删除记录。
func (d *downloadsPage) viewOpsPud(c *ui.Context, s *Shell, r download.Snapshot) {
	key := "dl:" + r.ID + ":"
	switch {
	case r.Status == download.StatusError && r.CanRetry:
		if pud.Button(c, key+"retry", pud.Ghost, pud.Sm, opRetry, "refresh").Clicked() {
			d.resume(r, s)
		}
	case r.Status == download.StatusDone:
		if d.tipWith(c, pud.IconBtn(c, key+"open", "open", opOpen, pud.IconOpts{}), opOpen).Clicked() {
			d.revealFile(r, s)
		}
	case r.Status == download.StatusPaused:
		if d.tipWith(c, pud.IconBtn(c, key+"resume", "play", opResume, pud.IconOpts{}), opResume).Clicked() {
			d.resume(r, s)
		}
	case r.Status.CanPause():
		if d.tipWith(c, pud.IconBtn(c, key+"pause", "pause", opPause, pud.IconOpts{}), opPause).Clicked() {
			d.pause(r, s)
		}
	}
	if d.tipWith(c, pud.IconBtn(c, key+"del", "trash", opDelete, pud.IconOpts{}), opDelete).Clicked() {
		d.askDeleteOne(r, s)
	}
}
