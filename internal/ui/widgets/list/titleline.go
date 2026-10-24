package list

import (
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/ink"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/scale"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/space"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/title"
	"github.com/egoist/mygo/ui"
)

// shown 是一首歌在列表 / 网格里**真正显示**的几样字：清洗过的歌名、副标题（从标题方括号里
// 挪出来的歌手 / 专辑名拼在 UP 名前面）、是不是现场版。清洗规则见 `widgets/title`。
// 它只管显示：`Music.Title` 不变，勾选框 / 菜单 / 无障碍名字仍用原标题。
type shown struct {
	title string
	owner string
	live  bool
}

// showOf 清洗一首歌的显示字。
func showOf(m Music) shown {
	c := title.Clean(m.Title)
	owner := m.Owner
	switch {
	case c.Extra != "" && owner != "":
		owner = c.Extra + " · " + owner
	case c.Extra != "":
		owner = c.Extra
	}
	return shown{title: c.Title, owner: owner, live: c.Live}
}

// liveTagRadius 是「现场」小标签的圆角：刻意不跟主题圆角走（标签是个小胶囊，
// 用户把全局圆角拖到 24 它也不该变成圆球）。
const liveTagRadius = 4

// liveTag 画那枚「现场」小标签：caption 号、强调色字、强调色 14% 的淡底（规范 §7.3）。
func liveTag(c *ui.Context) {
	t := c.Theme()
	ui.Text(c, "现场").FontSize(scale.Font(c, scale.Size11)).TextColor(liveTagText(t)).
		Background(t.Background.Mix(t.Accent, 0.14)).Radius(liveTagRadius).
		Padding(space.Px2, space.Px6).SingleLine()
}

// ownerLine 画副标题那一行：现场标签（有就画）+ 副标题文字。maxW > 0 时给上限，grow 时吃满。
func ownerLine(c *ui.Context, s shown, size, lh float32, color ui.Color, maxW float32, grow bool) {
	row := ui.Row(c).Gap(space.Px6).AlignItems(ui.Center)
	if maxW > 0 {
		row = row.MaxWidth(maxW)
	}
	if grow {
		row = row.Grow(1)
	}
	row.Children(func() {
		if s.live {
			liveTag(c)
		}
		if s.owner != "" {
			ui.Text(c, s.owner).FontSize(size).LineHeight(lh).TextColor(color).MaxLines(1).Ellipsis("…")
		}
	})
}

// liveTagText 是标签字色：强调色压在它自己的 14% 淡底上，浅色下不够 4.5:1 就压深。
func liveTagText(t *ui.Theme) ui.Color {
	return ink.Ensure(t.Accent, t.Background.Mix(t.Accent, 0.14), 4.5)
}
