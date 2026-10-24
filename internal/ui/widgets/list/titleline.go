package list

import (
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/ink"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/scale"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/space"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/title"
	"github.com/egoist/mygo/ui"
)

// Shown 是一首歌在列表 / 网格里**真正显示**的几样字：清洗过的歌名、副标题（从标题方括号里
// 挪出来的歌手 / 专辑名拼在 UP 名前面）、是不是现场版。清洗规则见 `widgets/title`。
// 它只管显示：`Music.Title` 不变，勾选框 / 菜单 / 无障碍名字仍用原标题。
//
// ⚠️ **导出的**：界面层里有几处自己画行 / 画卡片（首页的新歌速递那一块，见
// `internal/ui/homepage_newmusic.go`），它们要显示的字**必须和列表里的一样**——
// 两处各写一份清洗算法的话，同一首歌在两个地方会显示成两个名字。
type Shown struct {
	// Title 是清洗过的歌名。
	Title string
	// Owner 是副标题那一行：UP 名（前面可能缀着从标题方括号里挪出来的歌手 / 专辑名）。
	Owner string
	// Live 是「这是现场版」。
	Live bool
}

// ShowOf 清洗一首歌的显示字。
func ShowOf(m Music) Shown {
	c := title.Clean(m.Title)
	owner := m.Owner
	switch {
	case c.Extra != "" && owner != "":
		owner = c.Extra + " · " + owner
	case c.Extra != "":
		owner = c.Extra
	}
	return Shown{Title: c.Title, Owner: owner, Live: c.Live}
}

// liveTagRadius 是「现场」小标签的圆角：刻意不跟主题圆角走（标签是个小胶囊，
// 用户把全局圆角拖到 24 它也不该变成圆球）。
const liveTagRadius = 4

// LiveTag 画那枚「现场」小标签：caption 号、强调色字、强调色 14% 的淡底（规范 §7.3）。
//
// ⚠️ **导出的**：界面层里自己画行 / 画卡片的那几处要画同一枚标签（见 `Shown` 的
// 注释）——各画各的的话，同一首歌在一处有「现场」标签、在另一处没有。
func LiveTag(c *ui.Context) {
	t := c.Theme()
	ui.Text(c, "现场").FontSize(scale.Font(c, scale.Size11)).TextColor(liveTagText(t)).
		Background(t.Background.Mix(t.Accent, 0.14)).Radius(liveTagRadius).
		Padding(space.Px2, space.Px6).SingleLine()
}

// OwnerLine 画副标题那一行：现场标签（有就画）+ 副标题文字。maxW > 0 时给上限，grow 时吃满。
//
// ⚠️ **导出的**：理由同 `LiveTag`——界面层里自己画行 / 画卡片的那几处要画同一行
// 副标题（见 `Shown` 的注释）。
func OwnerLine(c *ui.Context, s Shown, size, lh float32, color ui.Color, maxW float32, grow bool) {
	row := ui.Row(c).Gap(space.Px6).AlignItems(ui.Center)
	if maxW > 0 {
		row = row.MaxWidth(maxW)
	}
	if grow {
		row = row.Grow(1)
	}
	row.Children(func() {
		if s.Live {
			LiveTag(c)
		}
		if s.Owner != "" {
			ui.Text(c, s.Owner).FontSize(size).LineHeight(lh).TextColor(color).MaxLines(1).Ellipsis("…")
		}
	})
}

// liveTagText 是标签字色：强调色压在它自己的 14% 淡底上，浅色下不够 4.5:1 就压深。
func liveTagText(t *ui.Theme) ui.Color {
	return ink.Ensure(t.Accent, t.Background.Mix(t.Accent, 0.14), 4.5)
}
