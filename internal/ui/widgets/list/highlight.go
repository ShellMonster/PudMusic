package list

import (
	"strings"

	"github.com/ShellMonster/PudMusic/internal/ui/widgets/ink"
	"github.com/egoist/mygo/ui"
)

// SetHighlight 设歌名里要标出的搜索词，传空串去掉。只有搜索结果用它。
func (l *List) SetHighlight(q string) { l.hl = strings.TrimSpace(q) }

// titleText 画歌名；hl 非空且在歌名里出现时，把命中的那几段染成主色并加粗，
// 其余照旧。大小写不敏感，只标第一处（歌名一行放不下更多）。没命中就是普通的 ui.Text。
func titleText(c *ui.Context, text, hl string, color ui.Color) ui.Element {
	if hl == "" {
		return ui.Text(c, text).TextColor(color)
	}
	i := indexFold(text, hl)
	if i < 0 {
		return ui.Text(c, text).TextColor(color)
	}
	j := i + len(hl)
	mark := ink.AccentOn(c.Theme())
	return ui.RichText(c,
		ui.Span{Text: text[:i], Color: color},
		ui.Span{Text: text[i:j], Color: mark, Weight: 700},
		ui.Span{Text: text[j:], Color: color},
	)
}

// indexFold 是大小写不敏感的 strings.Index，返回的是 s 里的字节下标。
// 只有大小写折叠后字节数不变的文本才能直接用下标切（ASCII 和中文都满足）；
// 遇到会变长度的字符就当没命中，不去冒切坏 UTF-8 的险。
func indexFold(s, sub string) int {
	ls, lsub := strings.ToLower(s), strings.ToLower(sub)
	if len(ls) != len(s) || len(lsub) != len(sub) {
		return -1
	}
	return strings.Index(ls, lsub)
}

const (
	progressBarW = 120
	progressBarH = 3
)

// progressBar 在标题下面画「看到哪儿」：灰轨 + 主色填充。没有进度或时长时一个元素都不建。
func progressBar(c *ui.Context, m Music) {
	if m.Progress <= 0 || m.Duration <= 0 {
		return
	}
	f := float32(m.Progress) / float32(m.Duration)
	if f > 1 {
		f = 1
	}
	t := c.Theme()
	ui.Box(c).Key("progress:"+m.ID).Size(progressBarW, progressBarH).Radius(progressBarH / 2).
		Background(t.Text.Alpha(0.1)).Clip().Children(func() {
		ui.Box(c).Size(progressBarW*f, progressBarH).Background(t.Accent)
	})
}
