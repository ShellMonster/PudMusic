package ui

import (
	"fmt"
	"strconv"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/ShellMonster/Biu_Go/internal/notice"
	"github.com/ShellMonster/Biu_Go/internal/platform"
	"github.com/ShellMonster/Biu_Go/internal/service/bili"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/flat"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/imgcache"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/ink"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/list"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/scale"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/scrim"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/shadow"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/space"
	tclean "github.com/ShellMonster/Biu_Go/internal/ui/widgets/title"
	"github.com/egoist/mygo/ui"
)

// 播放条的尺寸：
//
//	总高 76px        `playbarHeight`（2026-10-24 那一批界面重设计，PR #357）
//	左右内边距 16px  playbar/index.tsx:23-31 的 px-4
//	封面 52×52       同上（重设计之后从 56 改成 52）
//	封面与歌名 8px   同一处的 space-x-2
//
// ⚠️ **上游 Biu 那条是 88 高**（`_research/biu/src/layout/index.tsx:48` 的
// `h-[88px]`），而**我们这一条是 76**（重设计把它改小了，票 #357）。
// **88 那个数是上游的，别拿它算我们的**——2026-10-24 之前我们才是 88。
//
// ⚠️ Biu 那条**只有 px-4，没有上下内边距**：一行内容垂直居中，56 的封面就正好
// 上下各留 16（88-56=32）。所以这里是 Height(76) + PaddingX(16)
// + 一行 AlignItems(Center)，不是四边都 Padding(16) 再往上堆第二行——那样算
// 出来是 98 起（Biu 那条提示的 toastOffset=90 也会被压穿）。
//
// 🔴 **左边那一块是三段**（票 #219，上游 `playbar/left/index.tsx:29`）：
//
//	封面 │ 歌名·UP名（文字列）│ 分集·收藏·赞（横着并排的一行）
//
// 段与段之间 8（上游那一行的 `space-x-2`）。改之前我们把三枚键摆在**歌名下面
// 那一行**（和 UP 名并排），于是它们和「歌名/UP 名」是**上下**的关系；上游是
// **左右**的关系（见 `viewTrackActions`）。
const (
	playbarHeight = 76
	playbarPadX   = space.Px16
	coverSize     = 52
	coverGap      = space.Px8

	// coverRadius 是播放条上那张封面（和它悬停时那层黑罩）的圆角：**6**。
	//
	// 上游是 `radius="md"`（`layout/playbar/left/index.tsx:31-41` 的
	// `<Image radius="md" …>`）和浮层那一层的 `rounded-md`（`:42-44`）——Tailwind
	// 的 `md` 是 6。
	//
	// ⚠️ **它不是全局那个 `radius.default: 8`**：那是个**令牌**，用户在设置页
	// 能调它（`app.Settings.BorderRadius` → `t.Radius`），改了它整个界面跟着变。
	// 所以**只有这一处封面**用 6（票 #220 / 报告 §3.3、§3.4）。
	coverRadius = 6

	// trackRowGap 是歌名下面那几行之间的缝（UP 名 / 出错那一句）。
	//
	// 上游是 `space-y-1`（`playbar/left/index.tsx:46`）= **4**。
	//
	// ⚠️ **别拿「框到框」的距离去比上游那个 4**：我们的文本盒是字体的
	// ascent+descent（**没有额外行距**），上游那一行的行盒却是 24 / 20
	// （Tailwind 的 `text-base` / `text-sm`），字画在行盒里居中——所以上游
	// 「行盒到行盒」是 4，而**墨迹到墨迹**才是眼睛看见的那一档。
	//
	// 实测（2026-10，票 #219）：上游那张 README 截图里，歌名的墨迹底到 UP 名的
	// 墨迹顶是 17 个**设备像素**（那张图缩过 1.21 倍，换成 CSS 是 **14**）；
	// 我们不设缝的时候是 **10**，加上这 4 就是 **14**——对上了。
	trackRowGap = space.Px4

	// playbarRightGap 是右边那一组里键与键之间的缝。Biu 那一行是
	// `space-x-2`（`playbar/right/index.tsx:13`），不是外面那一行的 12。
	playbarRightGap = space.Px8

	// transportGap 是上一首 / 播放 / 下一首三枚键之间的空隙。
	//
	// 上游是 `space-x-6`（`components/music-play-control/index.tsx:19`）= **24**。
	// 原来是 12：那一排挤成一堆，中间那枚（用户真正要按的那枚）左右只各留 12。
	transportGap = space.Px24

	// playbarCenterPadX 是中间那一列左右各留的内边距。
	//
	// 上游是 `px-6`（`layout/playbar/center/index.tsx:12`）= **24**。
	// ⚠️ **它不是装饰**：右边那一组是**定宽**的一排键（比 1 份轨道宽），在窄
	// 窗口下会从轨道里溢出来——这 24 是进度行和那排键之间的缓冲。
	playbarCenterPadX = space.Px24

	// artistMinW 是 UP 名那一段最少留多宽。
	//
	// ⚠️ **没有它，UP 名会被挤成 0 宽**：`Grow(1)` 的 UP 名在小到只剩一点的时候
	// 一个像素都分不到，而**界面上看不出坏了**（按钮和那句话都还在，只有名字没了）。
	// 留 60 是「还能认出是哪个 UP」的下限（**14 号字**四个多字），再窄就只剩两个
	// 偏旁了。
	//
	// 本应用的窗口最小宽是 900（`WindowOptions` 的 `MinWidth`），那儿它是完整
	// 的；760 是测试里的窄窗口，这一档是给它的。
	artistMinW = 60

	// playbarActionsW 是第三段那三枚键在**摆满**时有多宽：三枚 × 32 = **96**。
	//
	// 每一枚的盒子恒是 32（上游 HeroUI 的 `isIconOnly` 是 `w-[2rem] h-[2rem]`），
	// 图形是 18（上游播放条上全部图标键都是 18，票 #219 / 报告 §3.6）。
	playbarActionsW = 3 * iconHitMd

	// trackTextMinW 是**文字列**那一段（歌名 + UP 名）最少有多宽。
	//
	// ⚠️ **它不是「歌名有多宽」，是「这一段最少保底多少」**：歌名那一行还要
	// 让出音质徽章（28 + 一道 6 的缝 = 34），所以这一段保底 76 的时候，歌名
	// 自己只剩 42——**两个多字**（徽章是短版「无损」/「杜比」的时候是 48.1，
	// 三个多字——见 `viewQualityBadge`）。
	//
	// 这不是我们挑的，是上游那个结构的账：左边那一列只分到整条播放条的 1/5
	// （上游 `grid-cols-[minmax(0,1fr)_minmax(0,3fr)_minmax(0,1fr)]`），而三枚
	// 键又占掉 96 + 8。**上游在 1200 宽（它自己的默认窗口）上歌名那一列只有
	// 65.6**，比我们还窄（报告 §2.8 的表）。
	//
	// ⚠️ **真嫌窄就动 `playbarLeftMinW`**（那个数是左列的保底宽），或者动
	// 「歌名 + UP 名」这一列的分配——**别动这一段的保底**：UP 名要 60
	// （`artistMinW`），歌名要多少是另一件事。
	trackTextMinW = 76

	// playbarLeftMinW 是左边那一列的最小宽度：**244**。
	//
	//	封面 56 │ 缝 8 │ 文字列 76 │ 缝 8 │ 三枚键 96
	//
	// 🔴 **它钉的不是「好看」，是「多一 P 那一档和单 P 那一档一样宽」**。
	//
	// 左列在 Grid 的一格（1 份）里，而框架给格子里那一块的宽度是它**自己的最小
	// 内容宽**——那个数随着第三段摆几枚键而变：多 P 时多一枚分集键，那一段就
	// 宽 32，于是**左列宽 32、歌名窄 32**。而歌名窄了是**界面上看不出坏了**的
	// 那种（字还在那儿，只是截断得更早）。
	//
	// 钉住之后，多 P 那一档左列还是 244、单 P 那一档也是 244（单 P 时第三段
	// 只有两枚键，那 32 就让给了**文字列**那一段）。
	//
	// ⚠️ **244 和改之前一个像素都不差**（改之前是 56 + 8 + 180，第二行里摆着
	// UP 名和三枚键）。所以窄窗口下它撑出轨道的那一截（900 宽下 70.4）也一个
	// 像素都不差，进度行照样碰不到它。
	playbarLeftMinW = coverSize + coverGap + trackTextMinW + coverGap + playbarActionsW
)

// viewPlayBar 是底部那条播放条：封面、歌名、上一首/播放/下一首、可拖的进度条、
// 右边那一组控件。
//
// 一行摆完，横着分三块，和 Biu 的 playbar/left|center|right 一一对应：
// 左边封面加歌名，中间控制在上进度在下，右边是那一组。
//
// 🔴 **底色取 `Background`（和内容区同一个来源），靠一条阴影和侧栏分开**。
//
// Biu 的播放条是 `bg-background`（`layout/playbar/index.tsx:26` 的 `Card`），
// 底色和内容区一样，分界靠的是那**两道**影：外层 div 的 `shadow-2xl`
// （`layout/index.tsx:48`）+ 里面那张 Card 的 `shadow="sm"`
// （`layout/playbar/index.tsx:24`）——**深色下真正把那条线画出来的是后者**
// （1 像素内白边）。我们原来取 `Surface`：**浅色下只差一档，
// 深色下差一整级**（`#27272a` / `#18181b`），于是深色下界面裂成三条带
// （票 #127 的头一条）。三处统一之后层次没丢：上面那条线在。
//
// ⚠️ **卡片和弹窗现在也取 `Background` 了**（票 #172）：上游那张 `Card` 走
// `bg-content1`、那一枚 `Drawer` 走 `bg-background`，两处都等于页面底色，
// **浮起来靠的是那圈影**（`widgets/shadow` 里 HeroUI 那几档）。所以「靠底色
// 亮一档来分层」这一句已经作废——别再照着它去找「亮一级的板」。
//
// 🔴 **宽度账**（2026-10 票 #216 之后量的，深浅两套一样）：
//
//	窗口   播放条可用   轨道 1/3/1（左边/中间/右边）   中列内容区   进度行   滑块   左边那一列
//	 900       868        173.6 / 520.8 / 173.6            472.8     354.6  238.6    244（撑出 70.4）
//	1000       968        193.6 / 580.8 / 193.6            532.8     399.6  283.6    244（撑出 50.4）
//	1200      1168        233.6 / 700.8 / 233.6            652.8     489.6  373.6    244（撑出 10.4）
//	1600      1568        313.6 / 940.8 / 313.6            892.8     669.6  553.6    313.6
//
//	（可用 = 窗口 - 左右各 16 的内边距；**三列按 1:3:1 分**，这是上游那张
//	 `grid-cols-[minmax(0,1fr)_minmax(0,3fr)_minmax(0,1fr)]` 的数——轨道宽度
//	 只按比例走，内容更宽也不把轨道撑开。中列内容区 = 中列 - 左右各 24 的
//	 内边距（上游 center 的 `px-6`）；进度行 = 内容区的 **3/4**（上游的 `w-3/4`）；
//	 滑块 = 进度行 - 两个 48 的读数 - 两道 10 的缝。）
//
//	🔴 **三键落在窗口的竖直中轴上**（实测 760/900/1200/1600 四档，偏差 0.0）：
//	 它在中间那一列里居中，而**左右两列等宽**（1:3:1 的头一份和尾一份一样）
//	 ——那才是那一列对称于窗口的原因。
//
//	⚠️ **左边那一列是 244，不是轨道宽**：它的内容最少要 244（封面 56 + 缝 8 +
//	 文字列 76 + 缝 8 + 三枚键 96，见 `playbarLeftMinW`），窄窗口下
//	**撑出轨道**，伸进中间那一列（上游也是这么溢的：它那一列的内容最少 184，
//	 而 1 份在 900 宽下只有 173.6）。**它撑出来的那一截离进度行还有 70**
//	（900 宽下），不碰。
//
//	⚠️ **右边那一组是 243.2**（不是票 #127 那时的 362）：音量收成一枚图标之后，
//	 常驻的那根 120 像素横滑块没有了（见 `viewPlayBarRight`）；图标键的点击区
//	 从 28 长到 32（上游那一档，G39）之后又加了 19.7——**五枚键各多 4**；
//	 票 #173 又把倍速那一枚的字从 13/14 放到 16（上游的 `text-[16px]`），
//	 它自己又宽了 2.5。
//
//	⚠️ **这张表的前提是「有歌」**，而有两样东西会改它：
//
//	 1. **「下载」那一枚只在有歌、且不是本地文件时摆**（`viewPlayBarRight` 的
//		`st.HasTrack && st.Source != app.SourceLocal`）。没有歌时右边那一组是
//		203.2（少一枚 32 的键加一道 8 的缝）——**表里是「有歌」的那一档**。
//		⚠️ **它不再影响中间那一列**：右边那一组是绝对定位
//		贴住右沿的（见 `viewPlayBarRight`），不占那 1 份轨道。
//	 2. **「倍速」那一枚贴字宽**（上游的 `min-w-fit`），所以它随当前倍率变：
//		「1x」是 43.2，「1.25x」是 65.8——**表里是 1x 的那一档**。同样地，它
//		只改右边那一组的宽，**不再改中间那一列**。
//
//	🔴 **左边那一块里面怎么分（票 #219 之后）**：左边那一列的**宽一个像素都
//	没变**（还是 244 / 313.6），变的是它里面那三段：
//
//		封面 56 │ 缝 8 │ 文字列（歌名 + UP 名）│ 缝 8 │ 三枚键 96
//
//	 ⚠️ **票 #135 说过「不改左侧那一块的布局」，这一票改的就是它**——报告 §2.8
//	 说上游那边是三段（封面 │ 歌名·UP名 │ 三枚键），而我们那三枚键挤在歌名
//	 下面那一行。
//
//	 **歌名因此窄了很多**：改之前文字列那一段是 180（第二行摆着 UP 名和三枚
//	 键），改之后是 76——**三枚键从歌名下面那一行搬到了同一段**（上游就是这
//	 么摆的，报告 §2.8）。900~1200 宽上歌名从 146（九个多字）掉到 **42**
//	 （两个多字，还要让出音质徽章的 28 + 6；徽章是短版「无损」/「杜比」的时候
//	 是 48.1）；1280 上是 47.6；1600 上是 111.6。
//
//	 **和上游比的是「歌名的宽」，不是「那一段的宽」**：上游那一段（1200 宽下
//	 65.6）里**也有 chip**——它的「无损」/「杜比」是 10 号字（≈ 20 宽），
//	 所以上游的歌名是 **≈ 39.6**，我们的是 **42**（192K 那一档）/ 48.1
//	 （无损 / 杜比）——**同一量级**。
//
//	 **上游名和它对齐**（上游 `items-start` + 一个普通 `span`）：改之前我们
//	 那是 `flatButton(...).Grow(1)`，而框架的按钮内容**默认居中**——那句话
//	 飘到了中间（实测 x=219，而歌名在 x=80）。
//
//	 ⚠️ **歌名会被截掉**是设计好的（`MaxLines(1)` + 省略号），不是坏了。
func (s *Shell) viewPlayBar(c *ui.Context, st app.State) {
	// 记一笔「播放条建了一遍」——给「一帧建了几遍」那条测试数（见 builds.go）。
	s.builds.playbar++
	t := c.Theme()

	// ⚠️ **那道影是上游的两层**（外层 div 的 `shadow-2xl` + Card 自己的
	// `shadow="sm"`），值都在 `internal/ui/widgets/shadow` 里（DESIGN.md §四·五：
	// 调用处不许现调 `Shadow(...)`）。
	//
	// **底色和内容区是同一个**（上游 `playbar/index.tsx:26` 的 `bg-background`），
	// 所以影是这一条唯一的分界——去掉它播放条会和内容区糊成一片（浅色下尤其
	// 明显：两边都是 `#ffffff`）。
	bar := shadow.PlayBar(ui.Column(c).Key("playbar").Height(playbarHeight).
		PaddingX(playbarPadX).Background(s.coverWash(t, t.Background)), t)
	bar.Children(func() {
		// 🔴 **三列按 1:3:1 分**（上游 `playbar/index.tsx:26` 的
		// `grid-cols-[minmax(0,1fr)_minmax(0,3fr)_minmax(0,1fr)]`）。
		//
		// 框架的 `ui.Fr` 就是 CSS 的 `minmax(0, <f>fr)`：**轨道宽度只按比例
		// 走，内容更宽也不把它撑开**（`mygo/ui/grid.go` 的 `sizeTracks`）——
		// 和上游那个 `minmax(0,…)` 是一个意思。
		//
		// ⚠️ **左右两列等宽，是「三键落在窗口竖直中轴上」的前提**：三键在
		// 中间那一列里居中，而那一列要对称于窗口，左右两列就得等宽（外层
		// 左右各 16 的内边距也是对称的）。**这一条不是排版偏好，是算出来的**：
		// 两列不等宽的时候，中间那列的中心就偏了，三键跟着偏。
		//
		// ⚠️ **这里为什么是 Grid 而不是 Row**：Row 里三块各 `Grow(1)` 分的是
		// 「剩下的空间」，而**剩下多少取决于左右两块自己的内容有多宽**——左边
		// 那一块（封面 + 歌名 + UP 名 + 三枚键）最少要 244，右边那一组是
		// 定宽的 243.2，于是中间那列分到的永远不是三份里的一份。Grid 的
		// `Fr` 轨道不看内容（见上面那段），这才是上游那个比例。
		ui.Grid(c).Fill().ColumnTracks(ui.Fr(1), ui.Fr(3), ui.Fr(1)).
			RowTracks(ui.Fixed(playbarHeight)).Children(func() {
			// 左：**三段**——封面 │ 歌名·UP名 │ 分集·收藏·赞（上游
			// `playbar/left/index.tsx:29-93`）。段与段之间 8（coverGap，
			// 那一行的 `space-x-2`）。
			//
			// ⚠️ **三枚键在第三段，不在歌名下面**（改之前在那儿）：上游那一段
			// 是 `<div className="flex items-center">`——flex 默认方向是 row，
			// 所以它们是**横着并排的一行**，不是竖排（报告 §2.8 点名了这一条，
			// 而报告的第一版把它写成了竖排）。
			ui.Row(c).Gap(coverGap).AlignItems(ui.Center).
				MinWidth(playbarLeftMinW).Children(func() {
				s.viewCover(c, st)
				s.viewTrackText(c, st, t)
				s.viewTrackActions(c, st)
			})

			// 中：控制在上、进度在下。Biu 的 playbar/center 就是一个 flex-col。
			//
			// 🔴 **`AlignItems(ui.Center)` 是这一票的主体**：三键和进度行
			// 都在这一列里横向居中，于是三键落在窗口的竖直中轴上（上游
			// `center/index.tsx:12` 的 `items-center`）。原来这一列没有它，
			// 三键靠左，而进度条铺满整列——**两者的中轴互相对不上**。
			//
			// `Justify(ui.Center)` 是竖直居中（上游的 `justify-center`）。
			//
			// 🔴 **`PassThrough()`：这一列不接指针**——它那一个框会让
			// **左列撑出来的那一截点不动**。
			//
			// 原因是命中测试取**最上面**那个含点的元素
			// （`mygo/ui/input.go:145` 从 hits 列表末尾往前找），而这一列是
			// 左列的**后一个兄弟**、先画后画，于是它那整个框（900 宽下从
			// x=189.6 起）把左列溢出来的那一截盖住了：**收藏、赞、音质徽章
			// 这些在左列右端的键点了没反应**，而**界面上看不出坏了**（它们
			// 好好地画在那儿）。窗口宽小于约 1175 时都这样（那是「这一列的
			// 左沿越过左列右端」的那个临界宽度）。
			//
			// `flagPassThrough` 只跳过**它自己**的命中框，**子节点照旧登记**
			//（`mygo/ui/runtime.go:1046`）——所以三键和进度条照旧能点、能拖，
			// 而这一列那一大片空白不再挡着左列。
			ui.Column(c).Gap(space.Px2).AlignItems(ui.Center).Justify(ui.Center).
				PaddingX(playbarCenterPadX).PassThrough().Children(func() {
				s.viewTransport(c, st)
				s.viewProgress(c, st)
			})

			s.viewPlayBarRight(c, st)
		})
	})
}

// viewPlayBarRight 是播放条右边那一组。**次序照 Biu 的 `playbar/right`**：
// 模式 / 下载 / 队列 / 音量 / 倍速（`layout/playbar/right/index.tsx:14-18`）。
//
// ⚠️ **那一行还多一个「加歌」，是本应用自己的**：Biu 的右边没有它——上游把
// 「选一个本地音乐」放在侧栏菜单的「本地音乐」那一项里，而我们的
// `pickAndEnqueue` 是一条独立的路（播放条和顶栏「打开文件」共用它）。
// 摆在**最后一格**：前五格的次序是肌肉记忆，加东西不该插在中间。
//
// ⚠️ **「下载」是从左边第二行搬过来的，不是新加的一枚**：Biu 的播放条左边
// 只有分集 / 收藏 / 赞三枚（`playbar/left/index.tsx:86-90`），下载在右边
// （`playbar/right/index.tsx:15`）。两处各摆一枚就是「同一个动作两个入口」，
// 而它们会不一致——左边那枚走 `downloadLater` 补 cid，右边这枚也一样，
// 但用户看到的是两枚一样的图标，那是在问「哪一个是真的」。
//
// 键与键之间的缝是 8（`space-x-2`，`playbar/right/index.tsx:13`），不是外面
// 那一行的 12：这一行最挤，8 是上游量出来摆得下五枚的那一档。
//
// 🔴 **这一组现在是 243.2 宽**（六个键 + 五道 8 的缝）：模式 32、下载 32、队列 32、
// 音量 32、倍速 43.2（贴字宽）、加歌 32。票 #127 那时它是 362——多出来的是那根
// **常驻的 120 像素横滑块**加一道缝，票 #135 把它收成了图标；票 #173 又把倍速
// 那一枚的字放到 16（上游的 `text-[16px]`），它自己宽了 2.5。
//
// ⚠️ **「下载」那一枚是有歌、且不是本地文件时才摆的**（下面那个
// `if st.HasTrack && st.Source != app.SourceLocal`）——所以「243.2」是**有歌且
// 是 B 站那一首**的那一档；没有歌（或者放的是本地文件）时这一组是 203.2。
func (s *Shell) viewPlayBarRight(c *ui.Context, st app.State) {
	// 🔴 **这一组是绝对定位，贴着播放条内容区的右沿**（上游
	// `playbar/right/index.tsx:13` 的 `justify-end`）。
	//
	// ⚠️ **为什么不能就让它待在第三列里**：那一列是 **1 份**（三列里的），
	// 而这一组是**定宽**的 243.2——900 宽下 1 份只有 173.6。框架给格子里
	// 那一块的宽度是它**自己的内容宽**（于是比轨道宽），不靠右的话它从轨道
	// 的左沿开始排，**最后那枚「加歌」键会跑到窗口外面去**（实测它在
	// x=921.6，而窗口只有 900）。而靠右也不行：格子里那一块一旦比轨道宽，
	// 框架的横向排布就没有余量可分（溢出时一律靠左排），内容还是往右溢。
	//
	// 所以这一组干脆**脱出那一列**，直接贴住内容区的右沿（`Right(0)`）——
	// 窗口多宽它都在最右边，和上游一样；它比第三列宽出来的那一截**伸进
	// 中间那一列**（上游那边也是这么溢的：它那一组是 203.2，1 份在 900 宽
	// 下是 173.6），而中间那列左右各 24 的内边距（上游的 `px-6`）就是留给
	// 它的缓冲。
	ui.Row(c).Key("playbar-right").Gap(playbarRightGap).AlignItems(ui.Center).
		Absolute().Right(0).Top(0).Bottom(0).Children(func() {
		// 播放模式。**原来它摆在进度那一行的最右边**，而上游摆在右边这一组
		// 的头一个（`playbar/right/index.tsx:14`）。搬回来之后中间那一列还
		// 腾出 42（一个 32 的键加一道 10 的缝），而右边这一组多花同样的数。
		s.viewPlayMode(c, st)

		// 下载。**没有歌时不摆，本地文件也不摆**——Biu 那一支的条件是
		// `{Boolean(playId) && getPlayItem()?.source !== "local" && …}`
		// （`playbar/right/index.tsx:15`）。
		//
		// 🔴 **本地文件不摆**（票 #222 / 报告 §3.12）：本地文件在 B 站上压根
		// 没有 cid 可下，按下去只会得到一句「这一首下不了」——给一个按了只会
		// 报错的键不如不摆。**这一条以前判不出来**：`app.State` 里没有来源
		// 那一栏（`Item.Source` 在队列那一层），现在有了（见 `State.Source`）。
		if st.HasTrack && st.Source != app.SourceLocal {
			s.viewDownloadButton(c, st)
		}

		// 播放列表抽屉的开关（原来在顶栏，见 navbar.go）。上游它就在右边这一组
		// 的第三个（`playbar/right/index.tsx:16`），而顶栏那一枚是当初右边摆不下
		// 时挪过去的——现在右边腾得开，两处只能留一个，留两个就会不一致。
		if s.withTip(c, iconKey(c, iconQueue, "播放列表", iconMd, false).
			Size(iconHitMd, iconHitMd).Padding(space.None, space.Px7), drawerTip(s.drawerOpen)).Clicked() {
			s.drawerOpen = !s.drawerOpen
		}

		// 音量收成**一枚图标 + 悬停浮出的竖滑块**（上游 `music-volume` 那个做法），
		// 见 playbar_volume.go。
		//
		// ⚠️ **常驻的那根 120 像素横滑块没有了**：它抢的是进度条该有的空间——
		// 那一档右边这一组是 362 宽，而窗口下限是 900。收成图标之后右边这组窄了
		// 118.8（362 → 243.2）。
		//
		// ⚠️ **它现在不再影响中间那一列**（票 #216 之后）：右边这一组是绝对
		// 定位贴右沿的，不占那 1 份轨道，中间那一列的宽度只由窗口宽度决定。
		s.viewVolumeButton(c, st)

		// 倍速紧挨着音量：两个都是「听感」旋钮（上游也是挨着的）。
		s.viewRate(c, st)

		s.viewSleep(c, st)

		if s.withTip(c, iconKey(c, iconEnqueue, "加歌", iconMd, false).
			Size(iconHitMd, iconHitMd).Padding(space.None, space.Px7), "把本地文件加到队列末尾").Clicked() {
			s.pickAndEnqueue()
		}
	})
}

// drawerTip 是播放列表开关悬停时那句话：这个键是**开关**，所以说的话得说清
// 按下去会开还是会关——不然用户得先按一下才知道自己按的是什么。
//
// 它原来在 `navbar.go`：那一票之前这个开关在顶栏上（票 #127 把它搬到了这里）。
func drawerTip(open bool) string {
	if open {
		return "收起播放列表"
	}
	return "打开播放列表"
}

// viewTrackText 是封面右边那一列字：歌名（右边跟着音质徽章）、UP 名（可点），
// 出错时再补一句原因。
//
// 🔴 **这一列只有字**（上游 `playbar/left/index.tsx:46` 的
// `<div className="flex min-w-0 flex-col items-start space-y-1">`）：两行，左
// 对齐，**三枚键不在这一列**——它们是**平级的第三段**（见 `viewTrackActions`）。
// 改之前三枚键和 UP 名挤在歌名下面那一行，于是「歌名/UP 名」和「三枚键」是
// **上下**的关系，而上游是**左右**的关系（报告 §2.8）。
//
// 三行竖着堆，行数由**有没有那一句**决定：平时两行（歌名 / UP 名），出错时
// 三行。三行实测占 57.5（歌名 18.8 + UP 名 16.5 + 错误 14.1 + 两道 4 的缝），
// 在 76 高的播放条里上下各留 15.3——而横向仍然是 `Grow(1)` 那一段的整条宽：
// **下面两行的固定宽度不吃歌名的宽度**（这一条是「为什么第二行不挤歌名」的
// 答案，`TestTheTextColumnDoesNotSquashTheTitle` 盯着它）。
//
// ⚠️ **外面必须是 Column**。这里曾经为了塞下音质徽章把 Column 拆成了一个裸
// Row（歌名和徽章并排），结果错误文字变成了这一层的**兄弟**——它落到了
// 歌名右边的外层行上，把歌名挤成 20px 宽、76px 高的竖条并被裁掉。
// 而「出错时播放条上有一句人话」正是这一票要保证的东西，结构不能为它让路。
//
// ⚠️ **UP 名那一行是后来才有的**（A23），错误那一行是从第二行掉到第三行的。
// 掉下来之后 `TestTheErrorSitsUnderTheTitle` 仍然成立——它断的是「错误在歌名
// 的下面」，不是「错误紧贴着歌名」。
//
// 徽章是**在 Column 里面**跟歌名并排的：外层永远是竖着堆的行，徽章只占第一行
// 里的右边一小块。
func (s *Shell) viewTrackText(c *ui.Context, st app.State, t *ui.Theme) {
	// 显示的是清洗过的歌名（去视频宣传标签，见 `widgets/title`）；st.Title 本身不动。
	title := tclean.Clean(st.Title).Title
	if !st.HasTrack {
		title = "还没有打开文件"
		// 🔴 **冷启动那一句**（票 #478）：窗口开了、界面在那儿、就是没声音，
		// 而「还没有打开文件」是**一句假话**——应用不是没有文件，它是在
		// **等上一首**（冷启动恢复上次那一首时，那一趟还在取流）。
		//
		// 用户看到的是「我什么都没干，它怎么不动」——而他不知道自己在
		// 等什么。这一句说的就是「还在准备」。
		//
		// ⚠️ **它只在启动那一段说**：应用准备好了之后，这一句收掉，
		// 回到「还没有打开文件」——那时这句话是真的（用户确实没开过文件）。
		if st.Starting {
			title = "正在启动应用…"
		}
	}

	// 🔴 **缝是 4**（上游那一列的 `space-y-1`）——理由和量法见 `trackRowGap`。
	ui.Column(c).Grow(1).Gap(trackRowGap).Children(func() {
		ui.Row(c).Gap(space.Px6).AlignItems(ui.Center).Children(func() {
			// ⚠️ **一行，截断**——照 Biu：`left/index.tsx` 的歌名是
			// `min-w-0 flex-1 truncate`。以前这里不设 MaxLines，窗口一窄它就换行、
			// 换到三行，而**下面两行一格都分不到**：76 高的播放条塞不下
			// 「三行歌名 + UP 名 + 错误」，多出来的那两行被裁掉，而**界面上看不出坏了**
			//（歌名还在，只是下面的话看不见了）。一行 + 省略号之后超长的那部分看不全，
			// 但下面每一行都还在。
			//
			// 🔴 **16 / 400**（票 #173 / G26）：原来是 15 / 700——字号小一档、
			// 字重重一档。**层次靠大小，不靠重量**：上游那一句是 `text-base`
			// （16 / 400），而加粗的那一档在整条播放条上会跟满屏播放器的标题
			// （20 / 400）抢注意力——它是全条最大的字，不该是全条最重的字。
			//
			// 🔴 **歌名可点**（票 #222 / 报告 §6.5）：上游那一句点了会跳 B 站
			// 视频页（`layout/playbar/left/index.tsx:54-57` 的
			// `openBiliVideoLink`），而且**悬停时有下划线**（`:56` 的
			// `"hover:underline"`）。那是用户在这首歌上能走的**第二条路**：
			// 听不下去就去 B 站看它。
			//
			// ⚠️ **本地文件不点**（上游的 `isClickable` 就是
			// `playItem && playItem.source !== "local"`）：本地文件在 B 站上
			// 压根没有页面可跳，给一个点了没用的入口比不给更糟。
			//
			// ⚠️ **判据是「有 B 站地址」**（`st.BiliURL`），不只是「不是本地
			// 文件」：在线条目也可能没有稿件身份（既没有 bvid 也没有 sid），
			// 那时候没有页面可跳——上游那边拼出来的是
			// `bilibili.com/video/undefined`，那是个 404。
			name := ui.Text(c, title).FontSize(scale.Font(c, scale.Size16)).
				TextColor(textColor(t, st.HasTrack)).
				MaxLines(1).Ellipsis("…")
			if titleClickable(st) {
				url := st.BiliURL
				name = name.OnClick(func() { s.openExternalURL(url) })
				// ⚠️ **悬停才画下划线**（上游的 `hover:underline`）：平时不画，
				// 指针停上去才长出来。
				//
				// ⚠️ **问的是 `name` 自己**（元素建好之后）：`Hovered` 要元素在
				// 命中列表里，而那要它已经建好（`OnClick` 之后才算）。
				if name.Hovered() {
					name = name.Underline()
				}
			}
			s.viewQualityBadge(c, st, t)
		})
		// UP 名（点它进他的空间）。**没有歌不摆**（票 #219 之前是
		// `viewTrackMeta` 第一行就 return）。
		if st.HasTrack && st.Artist != "" {
			s.viewArtist(c, st, t)
		}
		// 🔴 **出错、「正在重连」、「正在取流」和「停住了」占同一个位置**
		// （票 #282、#315、#319）：四者都是「这一句说明现在怎么了」，而那一行
		// 只有一行宽。
		//
		// 🔴 **错一收，「详情」面板也得收**（票 #320）：**在 switch 之前**判——
		// 错收掉之后紧接着来的可能是「正在重连」「正在取流」「停住了」那几档
		// （票 #315 / #282 / #319），而那块面板说的是上一句错的话，它自己不会走。
		if st.Err == "" {
			s.errDetailOpen = false
		}
		// 它们的优先级是**错 > 重连 > 停住**：
		//
		//	出错		一句错比一枚转圈信息量大，而转圈还在转说明还有希望
		//	正在重连	这一首断了，我在救它——比「停住了」具体
		//	停住了	播放器在、只是不响了
		//
		// 🔴 **取流不在这一排里**（票 #581）：取流的反馈只有播放键上那一圈光
		// （`loadhalo.go`），这一行不说话——出错时播放器压根没有、重连时上一首
		// 停着，而停住了说明播放器在、只是不响了；取流的时候上一首还在放，
		// 这一行为空正好不打扰。
		switch {
		case st.Err != "":
			s.viewTrackError(c, st, t)
		case st.Reconnecting:
			s.viewTrackReconnecting(c, st, t)
		case st.Stopped:
			s.viewTrackStopped(c, st, t)
		}
	})
}

// viewTrackReconnecting 是播放条上「正在重连」那一句话（前面带一枚转圈）。
//
// # 为什么要有这一句
//
// 断流之后应用要做的事有两条：字节源换地址 / 换流（`internal/service/audio`
// 的 `recover`），以及从播放位置重开一首（`refreshOnlineNow`）。两条都要跑
// 网络，而这一档以前界面上**一个字都没有**——用户看到的是「歌停着、界面
// 不动、没有字」，等多久都像应用死了（票 #315）。
//
// 加上退避之后更糟：那 7 秒的等待原本是**完全静默**的。
//
// # 它和「正在取流」不是同一件事
//
//	正在取流	这一首**还没放起来**（第一次点它）
//	正在重连	这一首**响过一半**，流断了，正在救
//
// 用户看到的差别很大：前者歌压根没响过，后者是听着听着停住了。
//
// ⚠️ **它是灰字，不是红字**：红字是「出错了」，而这一句是「还在救」。两者的
// 语气不一样，用户读到红色会以为已经完了。
//
// 🔴 **屏幕上的那句话要短**：播放条上那一行只有 140 像素宽（1200 宽的窗口，
// 实测），12 号字放得下十来个字——「网络不稳，正在重连（第 2 次）」这样的
// 一句话上屏只剩「网络不稳，正在…」，那不是「一句能看懂的话」。完整那一句
// 进悬停提示（和错误那一栏一个做法，见 `viewTrackError`）。
func (s *Shell) viewTrackReconnecting(c *ui.Context, st app.State, t *ui.Theme) {
	msg := "正在重连"
	tip := "网络不稳，正在重连"
	if st.ReconnectAttempt > 0 {
		tip += fmt.Sprintf("（第 %d 次）", st.ReconnectAttempt)
	}
	tip += "…"
	ui.Row(c).Gap(space.Px4).AlignItems(ui.Center).Children(func() {
		// ⚠️ **转圈的无障碍名字就是「正在重连」**：转圈没有可见的字，而它是
		// 唯一能认出「这里在等」的凭据（无头 Tester 也只认字和名字）。
		ui.Spinner(c).Size(statusLineSpinnerSize, statusLineSpinnerSize).Label("正在重连")
		// ⚠️ **一行 + 省略号，和「正在取流」那一句同一个理由**。
		s.withTip(c, ui.Text(c, msg).FontSize(scale.Font(c, scale.Size12)).
			TextColor(t.TextMuted).MaxLines(1).Ellipsis("…").Grow(1), tip)
	})
}

// viewTrackStopped 是播放条上「停住了」那一句话（票 #319）。
//
// # 它说的是哪一件
//
// 「停住了」有两种，而用户要分得开：
//
//	用户按了停止 —— **「已停止」**：这一首到此为止，**队列里还有歌**
//	顺序播放走到队尾 —— **「这一串放完了」**：**没有下一首了**
//
// ⚠️ **判据是 `State.QueueFinished`**（见 `app.State` 的说明）：那才是「队列
// 自己走完了」的那一种，而 `State.Stopped` 是两种都有。只看 `Stopped` 的话，
// 用户按了停止也会被告知「这一串放完了」——而队列里明明还有十几首。
//
// # 为什么它占的是「出错 / 正在取流」那一格
//
// 那一格本来就是「**此刻怎么了**」那一行（见 `viewTrackText` 的注释），而
// 「停住了」正是这样一句话。
//
// ⚠️ **它是灰字**：它是「现在停着」，不是「出了事」——红字那一档是留给出错的
// （`viewTrackError`）。而「正在取流」也是灰字，那一句前面有一枚转圈、这一句
// 没有，两者不会看混。
//
// ⚠️ **别给它挂悬停提示**：那一句只有六个字，而那一格只有 140 像素宽
// （1200 宽的窗口，实测），放得下。
func (s *Shell) viewTrackStopped(c *ui.Context, st app.State, t *ui.Theme) {
	msg := "已停止"
	if st.QueueFinished {
		msg = "这一串放完了"
	}
	ui.Text(c, msg).FontSize(scale.Font(c, scale.Size12)).
		TextColor(t.TextMuted).MaxLines(1).Ellipsis("…")
}

// viewTrackActions 是左边那一块的**第三段**：分集 / 收藏 / 赞，
// **横着并排的一行**。
//
// 🔴 **横排，不是竖排**（票 #219，报告 §2.8）：上游
// `playbar/left/index.tsx:85` 是 `<div className="flex items-center">`——
// **flex 的默认方向是 row**，所以那三枚是横着并排的一行，和「歌名/UP 名」
// 那一列**平级**。（报告的第一版把这儿写成了「封面右边一列竖排的按钮」，
// 那是照着一张**旧截图**猜的——旧版上游还没有这三枚。）
//
// **次序照上游**：分集（`:86`）/ 收藏（`:89`，`MusicFavButton`）/ 赞（`:90`，
// `MusicThumb`）。改之前我们是 分集 → 赞 → 收藏。
//
// ⚠️ **键与键之间没有缝**：上游那一段没有 `space-x`，而每一枚的盒子恒是 32
// （HeroUI 的 `isIconOnly` 是 `w-[2rem] h-[2rem]`），图形 18 → 每边 7 的留白，
// 于是两枚图形之间天然隔着 14。
//
// ⚠️ **这一段会随「有没有分集」而宽窄**（96 / 64）：分集那一枚是逐帧问出来
// 的（`viewPartButton`），单 P 的稿子不摆。于是**文字列**那一段（歌名 + UP 名）
// 跟着宽窄 32——**这一条是上游的账**（上游那一段也是 `flex`，宽度跟着内容
// 走）。左列的**总宽**被 `playbarLeftMinW` 钉住了，所以变的是歌名的宽度，
// 不是左列的宽度（见 `viewPlayBar` 那张宽度账）。
//
// # 🔴 「让位」表（2026-10 票 #219 之后重量的）
//
// 同一首歌、同一块屏幕，**单 P**（不摆分集键）和**多 P**（摆）各量一次：
//
//	窗口    歌名（单 P → 多 P）    UP 名（单 P → 多 P）   让位   进度条    右边那一组
//	 900     74.0 → 42.0          108.0 → 76.0           32    238.6    32 / 32 / 32
//	1000     74.0 → 42.0          108.0 → 76.0           32    283.6    32 / 32 / 32
//	1200     74.0 → 42.0          108.0 → 76.0           32    373.6    32 / 32 / 32
//	1280     79.6 → 47.6          113.6 → 81.6          32    409.6    32 / 32 / 32
//	1600    143.6 → 111.6         177.6 → 145.6          32    553.6    32 / 32 / 32
//
// **让位的是 32 —— 就是这个键自己那一格**（`iconHitMd`），而**进度条和右边
// 那一组一个像素都不让**（它们压根不在左列里：右边那一组是绝对定位贴右沿
// 的，进度行在中间那一列里）。
//
// ⚠️ **和改之前的那一版不是同一件事**：改之前这个键在**歌名下面那一行**，
// 让位的是 **40**（`iconHit` 32 + 一道 8 的缝），而让位的也只有那一行里的
// 歌名和 UP 名。现在它和「歌名/UP 名」是**两段**（左右），所以它让的是
// **左边那一列里的 32**，歌名和 UP 名**一起**让（它们在同一个 `Grow(1)` 的
// 段里）。
//
// ⚠️ **左列的**总宽**没变**（还是 244 / 313.6，`playbarLeftMinW` 钉着的），
// 所以窄窗口下它撑出轨道的那一截也一个像素都不差——**进度行照样碰不到它**。
//
// 没有歌时这一段一格都不画：给一个空的播放条摆三个点不动的键，比不摆更让人
// 以为它们坏了。
func (s *Shell) viewTrackActions(c *ui.Context, st app.State) {
	if !st.HasTrack {
		// ⚠️ **开着的分集浮层要收掉**：这一段的其余部分一格都不画，而
		// `viewPartButton`（连同它开头那句「没有歌就收掉」）在这一段里面——不
		// 在这儿也收一遍的话，「歌放完了 / 用户按了停止」之后 `partOpen` 一直
		// 留着 true，等下一首多 P 的歌放起来，浮层**自己弹出来**（用户什么都
		// 没按）。用户动作和 A12 那条不是一回事：换歌时 `viewPartButton` 每一帧
		// 都在跑，收得住；**没有歌**的那一段它压根没被调到。
		s.partOpen = false
		return
	}
	ui.Row(c).AlignItems(ui.Center).Children(func() {
		s.viewPartButton(c, st)
		s.viewFavButton(c, st)
		s.viewLikeStar(c, st)
	})
}

// viewTrackError 是播放条上那一句错，和它旁边那一枚**能做的事**。
//
// # 为什么是「一枚键」而不是「一句更长的说明」
//
// 这一行只有一行宽（还带省略号），而「为什么 + 还能怎么办」两半加起来
// 有一二十个字——写在这里必然被截掉一半，而截掉的那一半恰恰是**用户下一步
// 要做的事**。所以那句话留在悬停提示里（全文），旁边给一枚**动作**：
//
//	能重试	「重试」——网络断了、地址过期了，再试一次就有可能好
//	不能重试但有 B 站地址	「在 B 站打开」——B 站说这一条没有音频流，
//	                    用户能做的就是去 B 站里听它
//
// 两样都没有（比如本地文件打不开）就只留那句话。
//
// ⚠️ **不能重试的时候不给「重试」**：给一个按了也没用的重试比不给更糟——
// 用户按两次发现还是那样，就不再信这个应用了。
//
// ⚠️ **风控和限流也不算「能重试」**（票 #316 / #346）：那枚「重试」是**立刻**
// 再取一次流（没有退避），所以这两类按下去就是再撞一次墙。它们落进「不能
// 重试」那一边，界面上给的是「在 B 站打开」。
//
// ⚠️ **判据只有一处**（`bili.Recoverable`，见 `service/bili/retry.go`）：
// 下载记录页上那枚「重试」用的是同一张表。
//
// ⚠️ 这一行**只在出错的时候才有这一枚键**，所以平时播放条的布局一个像素
// 都不动（那一列的宽度、进度条的长度都不受影响）。
//
// # 「详情」层（票 #320）
//
// 这一行是**一行 + 省略号**（理由见下面那段注释），而 `bili.Friendly` 那张表
// 里的话信息量很大——「B 站没有给这一条单独的音频流（歌单、活动那一种多半是
// 这样）。在 B 站里打开它听，或者换一首放」四十来个字，压成一行之后用户常常
// 只读到半句话。
//
// 所以完整那一句有**两个去处**：
//
//	悬停	提示框（这一层早就有，票 #282 的评审加的）
//	点它	播放条**上方**那块小面板（`playbar_err.go`）——完整那一句、多行
//	        排开、**能选中复制**（用户要拿它去提 issue）
//
// ⚠️ **面板是浮层**：76 高那一行一个字都不多（票 #320 的验收盯的就是这个）。
//
// ⚠️ **每帧多一次分配**：那个 `OnClick` 的闭包（和歌名那一处一个道理——它也
// 是每帧一个闭包）。**平时（没有错）一个字都没多**：`framebudget_test.go`
// 量的是「一首 B 站的歌」那一副（**14 次 / 385 字节**，2026-10-25 在 HEAD 上
// 实测；出错的那一副本来就多花钱——那句话的字要排版——这一条不改变量级）。
//
// ⚠️ **别拿旧的数当基线**：票 #320 的报告里写过「12 次 / 578 字节」，那是
// 2026-10-24 那次界面重设计**之前**的数（重设计把播放条从 88 高改成 76、
// 封面从 56 改成 52）；现在的数是 14 次 / 385 字节（票 #320 的评审查出来的）。
func (s *Shell) viewTrackError(c *ui.Context, st app.State, t *ui.Theme) {
	msg := "打不开：" + st.Err
	// 🔴 **提示框里给的是完整那一句**：这一行只有 140 像素宽（1200 宽的窗口，
	// 实测），十二三个字就是它的全部——一句 50 个字的话上屏只剩「打不开：取流…」
	// 这样的碎片（票 #282 的评审）。短的那句在画面上，完整的那句在提示里，
	// 两边合起来才是一句能看懂的话。
	tip := "打不开：" + errFullBody(st)
	// ⚠️ **面板开着的时候不挂提示框**：提示框和那块面板**摆在同一个位置**
	//（都在这一行的上面），而它们说的是同一句话——指针移开又移回来的时候，
	// 两块叠在一起，屏幕上就是一块小提示盖着一块面板。
	if s.errDetailOpen {
		tip = ""
	}
	ui.Row(c).Gap(space.Px6).AlignItems(ui.Center).Children(func() {
		// 🔴 **一行 + 省略号，和歌名同一个理由**：这一列最窄的时候（760 宽
		// 的测试窗口，比应用自己的下限 900 还窄）只剩 70，而这一句话有 14 个
		// 字——换成两行就是四行，**76 高的播放条装不下，最后一行被裁掉**，
		// 而症状是「说了有话却看不见」（文本节点还在，测试也还找得到它）。
		//
		// 缩掉的那几个字在提示框和「详情」面板里补回来（见上面那段说明）。
		//
		// 🔴 **这一句是「详情层」的入口**（票 #320）：它点得开。
		line := ui.Text(c, msg).FontSize(scale.Font(c, scale.Size12)).
			TextColor(dangerText(t)).MaxLines(1).Ellipsis("…").Grow(1).
			OnClick(func() { s.errDetailOpen = !s.errDetailOpen })
		line = s.withTip(c, line, tip)
		// ⚠️ **悬停才显出「点得动」**：一道下划线 + 一只指针手
		//（侧栏那个登录提示是一个做法，`sidebar.go`）。
		//
		// ⚠️ **问的是 `line` 自己**（元素建好之后）：`Hovered` 要元素在命中列表
		// 里，而那要它已经建好（`OnClick` 之后才算）。
		if line.Hovered() {
			line = line.Underline().Cursor(ui.CursorPointer)
		}
		// 「点外面关掉」是框架那个浮层自己管的（`mygo/ui/base.go` 的 `popover`：
		// `panel.PressedOutside()`），所以这里只管开关本身（和 `viewRate` /
		// `viewPartButton` 同一个写法）。
		//
		// ⚠️ **Escape 那一条有条件**：框架把键**先给焦点链**（焦点上的那个控件
		// 和它周围那一圈），**它们都不处理，才轮到浮层**（`mygo/ui/input.go` 的
		// `routeKeys`）。所以「焦点在一个自己吃 Esc 的控件里」的时候**这一块
		// 关不掉**（`ui.SearchField` 就是——它的清空按钮在 `afterInput` 里判
		// `in.Shortcut(0, KeyEscape)`，见 `navbar_search.go` 顶上那一节）；
		// 焦点在别处（点开之后焦点在锚点那一行上）时，Esc 是关得掉的。
		//
		// ⚠️ 浮层那一层外面包一层 Guard（铁律三）：它是在**播放条这一层的布局
		// 之外**单独建的，它一崩坏掉的是整块浮层，而报错的地方在播放条上——
		// 两者对不上，极难查。
		ui.Popover(c, line, &s.errDetailOpen, func() {
			s.guardAt("错误详情", func(c *ui.Context) { s.viewErrorDetail(c, st) })(c)
		})
		switch {
		case st.CanRetry:
			if s.withTip(c, flatButton(c, "重试").FontSize(scale.Font(c, scale.Size12)),
				"再试一次这一首").Clicked() {
				s.retryPlay()
			}
		case st.BiliURL != "":
			if s.withTip(c, flatButton(c, "在 B 站打开").FontSize(scale.Font(c, scale.Size12)),
				"在 B 站里听这一首").Clicked() {
				s.openExternalURL(st.BiliURL)
			}
		}
	})
}

// retryPlay 重试上一次失败的那一首。
//
// 界面上给「重试」这一枚键只在**能重试**的时候（`State.CanRetry`）——
// 给一个按了也没用的重试比不给更糟。
func (s *Shell) retryPlay() {
	if err := s.app.Retry(); err != nil {
		s.addToast(err.Error(), notice.Danger)
	}
}

// openExternalURL 把一个网址交给默认浏览器。
//
// ⚠️ **走接缝**（`Shell.openExternal`）：无头 Tester 里 `systemOpenLink` 会 panic
// （`mygo.Shell` 的 `needsApp` 见到 `App.Run` 没跑过就直接 panic）——而这一栏是
// 「点歌名跳 B 站视频页」那条路的终点，**没有它就断不了「点了跳的是哪一页」**。
//
// ⚠️ **打不开要说一句**：静悄悄地失败的话，用户点了歌名什么都没发生，而
// 「点了没反应」在界面上看不出坏在哪。
func (s *Shell) openExternalURL(url string) {
	open := s.openExternal
	if open == nil {
		open = systemOpenLink
	}
	if err := open(url); err != nil {
		s.addToast("打不开："+err.Error(), notice.Danger)
	}
}

// viewArtist 是那一列里的 UP 名。**点它进 `/user/{id}`**（Biu 的播放条上就是
// 这么摆的：`playbar/left/index.tsx` 的 ownerName 是个链接）。
//
// 🔴 **左对齐**（票 #219，报告 §2.9）：上游那一句是**普通 `span`**，而它那一列
// 是 `items-start`——所以那句话紧跟在歌名下面、**和歌名同一个左沿**。
//
// 我们这里是**一枚「名字型链接」**（点它进那个 UP 的空间，票 #417）：一句话字，
// 指针落上去时底下长出一道下划线，**没有按钮壳**——上游那一处也是
// `hover:underline`。
//
// ⚠️ **左对齐 + 内边距清零**是这一档配方（`widgets/flat` 的 `Link`）里定死的：
// 框架的按钮内容默认居中、内边距默认 6/14——不改的话那句话会**飘到那一列的
// 中间**（实测歌名在 x=80，而 UP 名在 x=219）或者比歌名**右移 14 像素**。
//
// ⚠️ **有名字、没有编号时照样显示，只是点不动**：B 站有的接口只给名字不给
// mid（见 `app.Item.ArtistMid`）。把名字藏起来是另一种撒谎——用户看到的是
// 「这一首没有 UP 主」。
//
// ⚠️ 走的是 `upPathPrefix` 而不是写死 `"/user/"`：路由表里那一条改了的话
// 这里跟着错，而且错得很安静（空间页退回占位页，没人看得出来）。
//
// 🔴 **14**（票 #173 / G26）：原来是 12——那是「次要信息」那一档（列表里的
// 播放量 / 投稿时间），而 UP 名是**列表里那一行同样的字**（上游两处都是
// `text-sm`）。两处差一档的话，歌名从列表走到播放条会**缩回去一格**。
func (s *Shell) viewArtist(c *ui.Context, st app.State, t *ui.Theme) {
	if st.ArtistMid <= 0 {
		// 没有编号时是**普通文字**：它在那一列里本来就是左对齐的（列是
		// `AlignItems` 默认的 Stretch，文字从左沿起）。
		ui.Text(c, st.Artist).Grow(1).MinWidth(artistMinW).FontSize(scale.Font(c, scale.Size14)).
			TextColor(t.TextMuted).MaxLines(1).Ellipsis("…")
		return
	}
	// **名字型链接**：UP 名这一格是**一句话字**，指针落上去时底下长出一道下划线。
	//
	// ⚠️ **它不是一枚按钮**（票 #417）：改之前这里画的是扁平按钮——左右各留一段
	// 内边距 + 圆角，于是那个名字被一个**胶囊**包着，而指针落上去时胶囊才显
	// 出来（像一块底牌）。上游那一处是 `hover:underline`，**没有按钮壳**。
	//
	// ⚠️ **左对齐 + 内边距清零**（理由见 `flat.Link`）：名字要跟上面那句歌名
	// 同一个左沿，而框架的按钮内容默认居中、内边距默认 6/14。
	//
	// ⚠ **那句话是 `list.OwnerTipText` 给的**（票 #360）：列表里的 UP 名是同一
	// 件事的另一个地方，两处各写一遍的话，改了一处另一处就安静地变成另一句
	// 话——而那种错在画面上只是「提示里那句话怪怪的」。
	if s.withTip(c, flatLink(c, st.Artist).Grow(1).MinWidth(artistMinW).
		FontSize(scale.Font(c, scale.Size14)).TextColor(t.TextMuted).
		MaxLines(1).Ellipsis("…"), list.OwnerTipText(st.Artist)).Clicked() {
		s.navigate(upPathPrefix + strconv.FormatInt(st.ArtistMid, 10))
	}
}

// viewFavButton 是「收藏」那个键：开「把这一首放进哪些收藏夹」那个弹窗。
//
// ⚠️ **身份是按下去那一刻才去取的**（`App.PlayingItem`），而不是建按钮之前。
// `State` 那一组里没有 B 站的稿件 id，而收藏夹弹窗要 avid（`favTarget`）——
// 真拿到按钮却拿不到身份的话，用户点的是一个画在那儿的东西。
// 取不到就**说一句**，不静悄悄地走人。
//
// 🔴 **已经收藏过的那一首，这个键是亮的**（主色，和赞过的星标一样，见 like.go）。
// 它认的是 `App.LikeState().Favorited`，而那一栏有两个来源：换歌时问一次
// `archive/relation`，以及**长按三连那一次的回包**（B 站顺手收藏了这一首，
// 那是 `bili.TripleResult.Favorited`）。
//
// 为什么必须有这一栏：三连是一次「用户没按收藏键、却被收藏了」的操作。不显示它，
// 用户在三连之后完全看不出收藏夹里多了一首——而他去一看真的有。
func (s *Shell) viewFavButton(c *ui.Context, st app.State) {
	faved := s.app.LikeState().Favorited
	// 空心 = 还没收藏，实心 = 已经收藏。图标认的是「有没有」，和原来那个
	// 靠主色染出来的「收藏」两个字说的是同一件事。
	// 哪一档配哪一张只有 `favIcon` 说了算（理由见 icons.go 那几个函数的头一段）。
	//
	// 🔴 **图标 18、点击区 32**（票 #219，报告 §3.6）：上游那一枚是
	// `RiStarFill/Line size={18}`（`components/music-fav-button/index.tsx:55-56`），
	// 而**播放条上全部图标键都是 18**。改之前是 14（`iconSm`）——比上游小 4 像素，
	// 在 32 的盒子里每边留 9，看起来像「画糊了」。
	// 🔴 **身份钉死**：这一段的兄弟是「逐帧问出来」的「分集」键（单 P 的稿子不
	// 摆），它一出现，这一枚的兄弟序号就变了——而动画通道是存在身份里的（理由
	// 见 `playbar_keys.go`）。
	c.Key(favButtonKey)
	btn := s.withTip(c, iconKey(c, favIcon(faved), "收藏", iconMd, false).
		Size(iconHitMd, iconHitMd).Padding(space.None, space.Px7), favTooltip(faved))
	if faved {
		// ⚠️ **只在这一支设颜色**：没收藏的那一支一个字都不改，好让「默认那个颜色」
		// 继续由按钮自己决定（图标从祖先的文字色继承，按钮给的就是它）。
		//
		// 和「正在播放」同一个规矩：主色当前景色时浅色下要一个与底色
		// ≥4.5:1 的变体（理由见 `widgets/ink`）——图标也是前景，1.9:1 的绿
		// 压在白底上同样看不出是「已收藏」。
		btn = btn.TextColor(ink.AccentOn(c.Theme()))
	}
	if btn.Clicked() {
		it, ok := s.app.PlayingItem()
		if !ok {
			s.addToast("这一首还没有取回来，收藏不了", notice.Warning)
			return
		}
		s.openFolderSelect(it)
	}
}

// favTooltip 是收藏键悬停时那句话。
func favTooltip(faved bool) string {
	if faved {
		return "这一首已经收藏过了（点一下看收在哪一格）"
	}
	return "把这一首放进收藏夹"
}

// viewDownloadButton 是「下载」那个键，摆在右边那一组（`viewPlayBarRight`）。
//
// ⚠️ **走 `downloadLater`（后台补 cid），不走 `app.Download`**：队列里那一份
// 是从列表翻出来的，而 B 站的几个列表接口都不给 cid（搜索结果、历史、动态都
// 是），下载按分 P 存文件。缺 cid 时 `Download` 会**如实拒绝**而不是下错一集
// ——可播放条上这一格是「把正在放的这首存下来」，用户要的是那首，补出来的
// 第一集不是。理由和 `dynamiccard.go` 的「下载音频」是同一条。
//
// ⚠️ **尺寸跟右边那一组其余几枚一样（`iconMd` / `iconHitMd`）**：这一格原来
// 摆在左边第二行，是一排 `iconSm` 小键里的一枚；搬过来之后它要是小一号，
// 五枚键在一条线上会看出参差——而参差正是「这一枚是从别处搬来的」在画面上
// 唯一的痕迹。
func (s *Shell) viewDownloadButton(c *ui.Context, st app.State) {
	if s.withTip(c, iconKey(c, iconDownload, "下载", iconMd, false).
		Size(iconHitMd, iconHitMd).Padding(space.None, space.Px7), "把正在放的这首下载到本地").Clicked() {
		it, ok := s.app.PlayingItem()
		if !ok {
			s.addToast("这一首还没有取回来，下载不了", notice.Warning)
			return
		}
		s.downloadLater(it, false, "下载音频")
	}
}

// viewQualityBadge 是歌名右边那个音质徽章（「Hi-Res 无损」/「192K」）。
//
// 它同时是**换音质的入口**：点一下开菜单，几档里选一档。
//
// 为什么是徽章而不是另摆一个控件：播放条只有 76 高，左右两边（进度条、音量、
// 加歌）都已经占满了，而「我在放的是哪一档」这件事用户本来就在看这行字。
// 另摆一个下拉框就是把播放条挤坏——上一票为了塞下徽章把歌名挤成竖条，
// 那次结构性的破坏就是从这个方向来的。
//
// 本地文件不画：那是文件格式（FLAC / AAC），不是「音质」——
// 拿格式凑一个徽章会让「无损」这两个字在两个意思之间飘。
//
// ⚠️ **换流之后它会变**，所以它必须每帧从 `State` 现读，不能缓存。
// B 站换流拿回来的可能是降级过的那一档：无声地降级会让人以为自己还在听无损。
func (s *Shell) viewQualityBadge(c *ui.Context, st app.State, t *ui.Theme) {
	if st.Quality == "" {
		return
	}
	// 🔴 **显示的是**短版**（`bili.ChipLabel`），全称留在悬停提示里**（票 #219）。
	//
	// 这一列（歌名 + UP 名）只有 76 宽（左边那一列的 244 减去封面 56、两道 8 的
	// 缝和三枚键 96），而全称（「无损 FLAC」52.7、「杜比全景声」54.7、
	// 「Hi-Res 无损」60.6）**和歌名挤在一行放不下**：框架在装不下时按**基宽的
	// 比例**分摊那份 shortfall，于是**徽章自己被摊扁**——实测「杜比全景声」
	// 只剩 13.8（读出来**一个字**），而它同时是「换音质」的入口，**点击区只剩
	// 14 像素**。
	//
	// 短版（无损 / 杜比 / 192K）就是**上游那一枚 chip 的形状**
	// （`playbar/left/index.tsx:50-56` 的「无损」/「杜比」），而全称在提示里
	// （「音质：杜比全景声（点一下换）」）——那一行才是「B 站给了什么」的完整
	// 说法。
	//
	// 🔴 **`Shrink(0)`：这一枚不许被挤窄**（框架的 `flex-shrink: 0`）。它是
	// **控件**（点它换音质），挤窄它就是「点不准」和「读不出来」；而歌名是
	// **可截断的标签**（`MaxLines(1)` + 省略号），挤窄它只是截得更早。
	//
	// ⚠️ **今天这几档 chip 的最小内容宽就够保住它**（它们的字不换行）——这一行
	// 是把那个优先级**写死**：将来某一档的 chip 变长了，挤的应该是歌名。
	//
	// 收短之后各是几宽（1200 宽、多 P、实测）：192K → 歌名 42.0 / 徽章 28.0；
	// 无损 → 歌名 48.1 / 徽章 21.9；杜比 → 歌名 48.1 / 徽章 21.9。
	s.withTip(c, ui.Text(c, bili.ChipLabel(st.Quality)).
		FontSize(scale.Font(c, scale.Size11)).TextColor(t.TextMuted).Shrink(0),
		"音质："+st.Quality+"（点一下换）").
		Menu(func(m *ui.Menu) {
			for _, q := range qualityChoices {
				// `Checked` 是「当前选的是这一档」。选的和给的要能分开看：
				// 降档发生的时候，用户要能看见自己选的和系统给的不是同一档。
				if m.Item(q.Label()).
					Checked(app.BiliToPref(q) == st.QualityPref).
					Chosen() {
					// 🔴 **点的就是当前这一档时什么都不做**：换档要重新取流，
					// 为一个听不出区别的点击打一次网络往返是白费；下面的降档
					// 提示也只在真的换了档的时候说。
					if app.BiliToPref(q) != st.QualityPref {
						s.app.SetAudioQuality(q)
						qualitySwitchToast(s, q)
					}
				}
			}
		})
}

// qualitySwitchToast 在用户**切到无损 / 杜比**的那一下说一句降档规矩（票 #581）。
//
// 🔴 **只在切换那一下说，不是每放一首歌都说**：点名的档这一首没有时，取流那层
// 自动退到最高有损（`bili.pickAudio`），照样放出来——有没有退档，看播放条上的
// 音质徽章就知道（它显示的就是实际拿到的那一档）。每放一首歌都念一句「可能会
// 降档」是噪音。
//
// 🔴 **只有无损 / 杜比要说**：这两档不是每首都有；有损三档（高/中/低）B 站
// 基本每首都有，为它们说「可能降档」没有信息量。中/低档偶尔真的没有，那种
// 情况徽章自己会变。
func qualitySwitchToast(s *Shell, q bili.AudioQuality) {
	switch q {
	case bili.AudioLossless:
		s.addToast("已选无损：有些歌没有无损，会自动降档播放", notice.Success)
	case bili.AudioDolby:
		s.addToast("已选杜比全景声：有些歌没有杜比，会自动降档播放", notice.Success)
	}
}

// qualityChoices 是徽章菜单里的几档（票 #581 起：无损/杜比/高品质/中等/
// 低品质，**没有「自动」**——默认就是高品质，点名的档拿不到时自动退到
// 最高有损）。
//
// 顺序从「最费流量」到「省流量」，和设置里那份一致。标签取自
// `bili.AudioQuality.Label`——那是「用户要什么」的说法，不是「B 站给了什么」
// （后者是徽章），两者混起来就看不出有没有降档。
var qualityChoices = []bili.AudioQuality{
	bili.AudioLossless,
	bili.AudioDolby,
	bili.AudioHigh,
	bili.AudioMedium,
	bili.AudioLow,
}

// viewTransport 是上一首、播放/暂停、下一首。
//
// 🔴 **次序是「上一首 · 播放 · 下一首」**：上游的 `MusicPlayControl` 就是
// `RiSkipBackFill` / `RiPlayCircleFill` / `RiSkipForwardFill`
// （`components/music-play-control/index.tsx:16-18`），而**我们自己的**满屏播放器
// （`fullplayer.go`）和迷你播放器（`miniplayer.go`）也是这个次序。
//
// 改之前这里是「上一首 · 下一首 · 播放」——唯独播放条这处没照做。后果是**用户从
// 全屏退回播放条时，肌肉记忆里「中间是播放」的位置按下去是下一首**。
//
// ⚠️ **「上一首」「下一首」两个按钮的本体是共用的**（`viewPrevButton` /
// `viewNextButton`，满屏播放器那边也在用）——所以它们长到 22 的时候，满屏底下
// 那一排也跟着长（上游那两处是同一个 `MusicPlayControl` 组件，同一个数）。
//
// ⚠️ **三个都是只有图标的按钮**（Biu 的 `MusicPlayControl` 就是上面那三个）。
// 文字版是三个汉字各占 88 宽——中间那一列 276 宽全被这三个键占满，而进度条
// 那一行是从它这儿借的富余（见 viewProgress）。换成图标之后那一行只有
// **160** 宽（上一首 32 + 24 + 播放 48 + 24 + 下一首 32），歌名那边宽出来的
// 就是「加得下东西」的那一段。
//
// 🔴 **三键之间的空隙是 24**（上游 `music-play-control/index.tsx:19` 的
// `space-x-6`），原来只有 12：那时候三枚键挤成一堆，中间那枚——**用户真正
// 要按的那一枚**——左右只各留 12 的空。
//
// 无障碍名字一个字没少（「上一首」「播放」「暂停」「下一首」），所以读屏软件
// 念出来和原来一字一样——变的是**画出来的**，不是**念出来的**。
func (s *Shell) viewTransport(c *ui.Context, st app.State) {
	ui.Row(c).Gap(transportGap).AlignItems(ui.Center).Children(func() {
		s.viewPrevButton(c)

		// 🔴 **取流期间这一枚仍然是播放/暂停**（票 #282 的评审）：正在取流的
		// 那一首**还没放起来**，而**上一首还在放**——把它换成一枚转圈，用户就
		// 按不了暂停了（迷你窗和满屏播放器里还有，播放条上没有了）。
		//
		// 「正在取流」的那枚转圈在**第三行**（`viewTrackLoading`），不在这里。
		//
		// 🔴 **取流的时候它外面多绕一圈光**（票 #271）：那是**加**上去的，
		// 不是替掉这一枚——按钮照旧是播放 / 暂停、照样按得下、提示照旧。
		if st.Playing {
			b := iconButtonPlay(c, iconPauseFill, "暂停", iconHitPlay, iconPlayBtn, false)
			// ⚠️ **光圈只在这一枚不是灰的时候画**：框架对禁用的控件整个乘
			// 0.5 不透明度（理由见 loadhalo.go）。
			b = s.withLoadHalo(c, b, st, iconHitPlay, true)
			if b.Clicked() {
				s.app.Toggle()
			}
		} else {
			// ⚠️ **禁不启用由这里给**（`iconButtonPlay` 的 `disabled`）：那一枚的
			// 颜色是**建元素时**定的，「悬停变主色」要在**禁用之后**才问——问早了
			// 一枚禁用的键会用主色告诉用户「这里点得动」（理由见那个函数）。
			b := iconButtonPlay(c, iconPlayFill, "播放", iconHitPlay, iconPlayBtn, !st.HasTrack)
			b = s.withLoadHalo(c, b, st, iconHitPlay, st.HasTrack)
			if b.Clicked() {
				s.app.Toggle()
			}
		}

		s.viewNextButton(c)
	})
}

// viewCover 是播放条左边那张封面。**点它开全屏播放器**。
//
// 四种状态（没有地址 / 载入中 / 就绪 / 加载失败）都由 imgcache 画，
// 这里不重复它的判断，也不替它决定长什么样——以后缓存换画法只改那一处。
//
// 🔴 **没歌时这一整块不画**（票 #222 / 报告 §5.2）：上游在没有歌的时候左边那一块
// **整块不画**（`layout/playbar/index.tsx:28` 的 `{Boolean(playId) && <Left />}`），
// 所以画面上**没有那个方块**——我们原来摆的是一块「封面占位」（描边框 + 图片
// 图标），那一块在用户眼里是「有一首歌，只是封面还没拿到」。
//
// ⚠️ **连那 56×56 的位置一起不摆**：留着位置的话，没歌时那句「还没有打开文件」
// 会比有歌时的歌名**更靠左**（x=16 对 x=80）——同一个位置在两档之间跳来跳去。
//
// ⚠️ **也不要点得动**：一个点下去会开全屏播放器的空方块，比不摆更让人以为坏了
// （上游那一块压根不存在）。
//
// 🔴 它带着 `coverTransition(identity)`，而满屏上那一张带的是同一个——这就是
// 「封面从列表行放大成全屏背景」那一段，等价于 SwiftUI 的 `matchedGeometryEffect`
// （理由见 fullplayer.go 的 coverMatch）。**这一层是转场的另一半**：少了它，
// 满屏的封面找不到「上一个」，就只是「啪」地一下就位。
//
// ⚠️ 它在**每一帧**都要重新装一遍 Match（`ElementTransition` 是每帧重新登记的，
// 框架只认这一帧装的那个）。而框架只留一帧的记录（`rec.frame+1 < rt.frame` 就
// 删），所以漏一帧就等于转场没了。
func (s *Shell) viewCover(c *ui.Context, st app.State) {
	if !st.HasTrack {
		return
	}
	// 🔴 外面这一层 `Size(56,56)` 不是多余的：**`Absolute()` 是相对父容器摆的**，
	// 而这一行的父容器是上面那个「封面 + 歌名」的 `Grow(1)` Row。不套这一层的话
	// 悬停浮层盖住的是**整块左半边播放条**——实测那块黑从 x≈8 铺到 x≈1048，歌名
	// `stereo16` 被压在底下，⤢ 落在整块的中心 x≈540 而不是落在 56×56 的封面上。
	//
	// `queue_drawer.go` 的 `viewQueueCover` 就是这个套法（它外面套的是
	// `Width(queueCoverSize)` 那一层），照抄。
	//
	// 🔴 **可点的是外面这一层 wrapper，不是里面那张图。**
	//
	// 命中链是「最上面那个含点的框 + 它**自己的父链**」，**兄弟不在链上**
	// （`mygo/ui/input.go:140-154` 的 `appendHitChain`）；而 `flagPassThrough` 只
	// 跳过那一个自己的命中框、子节点照登记（`runtime.go:1046`）。于是只要可点的
	// 是浮层的**兄弟**，浮层里任何一个字都会把它挡住：⤢ 正好落在封面正中，而
	// 「把鼠标移过去、点一下」点的就是那个正中。
	//
	// 症状很有意思：点封面**边角**能开（那儿没有文字），点**正中**什么也不会发生。
	//
	// 两条改法都验过（改回去都红）：
	//
	//	① 可点的挪到 wrapper 上（本票）。wrapper 是浮层的**父级**，于是浮层里不管
	//	   画什么，链都走得到它。往浮层里再加东西也吃不掉点击。
	//	② 给 ⤢ 那个字加 `PassThrough()`（评审给的最小改法）。字不登记了，链落到
	//	   里面那张图上——也通，但那是**逐个子元素**修的：下一个往浮层里加的东西
	//	   还得再记着加一次。
	//
	// 选了 ①，顺手也给字加了 `PassThrough()`（`toast.go` 那边也是这么写的）——
	// 那个字自己不该接指针：指针停在一个字上却报成「停在封面旁边的空气上」，
	// 悬停反馈的位置就不对了。
	//
	// `queue_drawer.go` 的 `viewQueueRow` 本来就是这个形状——可点的行是封面的
	// **祖先**，所以那边浮层里画 ▶ 还是 ⤢ 都吃不掉点击。
	wrap := ui.Row(c).Key("playbar-cover").Size(coverSize, coverSize)
	wrap.Children(func() {
		// 共享元素转场留在里面那张图上（它才是转场的那一半）。
		//
		// ⚠️ **圆角是 6，不是主题那一个 8**（票 #220 / 报告 §3.4）：上游那张封面
		// 是 `radius="md"`（`layout/playbar/left/index.tsx:31-41`），而全局那个
		// `radius.default: 8` 是**令牌**——用户在设置里能调它，动它整个界面跟着
		// 变，所以只有这一处封面用 6。
		//
		// ⚠️ **「压根没有封面」那一格是**一枚音符**（票 #222 / 报告 §5.3）：上游
		// 那个 `Image` 传的是 `emptyPlaceholder={<RiMusic2Line />}`
		// （`layout/playbar/left/index.tsx:40`），而我们原来摆的是一枚图片图标。
		// 本地文件**永远**没有封面（见 `State.CoverURL`），所以这一档在播放条上
		// 是常客——摆成音符才说得清「这是一首歌，只是它没有封面」。
		imgcache.ViewPlaceholderTitled(c, s.images, st.CoverURL, st.Title, coverSize, coverSize, coverRadius,
			icon(iconEmptyMusic)).
			Label("当前封面").
			Transition(coverTransition(coverIdentity(s, st)))

		if wrap.Hovered() {
			// Biu 那边是 hover 时浮一层 `bg-[rgba(0,0,0,0.5)]` 加一个向上的箭头
			// （`layout/playbar/left/index.tsx:38-41`，图标是 `RiArrowUpSLine`）。
			// 没有这一层的话，用户不知道那张封面是能点的。
			//
			// 🔴 **箭头是主色绿**（票 #220 / 报告 §3.3）：上游那一层是
			// `text-primary`（`layout/playbar/left/index.tsx:42-44`），而我们原来
			// 给的是白 92%——**明暗正好和上游反过来**。
			//
			// ⚠️ **取的是主色本身，不是 `ink.AccentOn` 那一档**：那一档是按**应用
			// 底色**算的（浅色下是一个深绿 rgb(18,114,52)），而这里压的是**半黑
			// 罩**——罩子底下是封面那张图，深绿压在上面读不出来。罩子永远是暗的，
			// 所以这里要的是**亮的那一档主色**。
			//
			// 🔴 **箭头是 32**（票 #173 / G26）：原来是 20——在 56×56 的封面
			// 正中画一枚 20 的箭头，它小得像是「画糊了」，而不是「这里可以点开」。
			// 上游那一枚是 `size={32}`，而它要装进的就是这个 56 的方框。
			//
			// ⚠️ **圆角是 6**（票 #220 / 报告 §3.3）：上游那一层是 `rounded-md`，
			// 而它要**和封面那一块对齐**（两边都是 6 才看得出来是一张）。
			//
			// PassThrough：浮层这一层和里面那个图标都不接点击。
			ui.Row(c).Absolute().Left(0).Top(0).Right(0).Bottom(0).Radius(coverRadius).
				PassThrough().Center().Background(ui.RGBA(0, 0, 0, scrim.Cover)).Children(func() {
				ui.Icon(c, icon(iconFullscreen)).FontSize(32).
					TextColor(c.Theme().Accent).
					PassThrough()
			})
		}

		// 长按 2.5 秒 = 一键三连（SPEC 需求 69），500ms 起进度环。
		//
		// ⚠️ **它在 `Children` 里面**：环画的是「这一帧按了多久」，而那要靠
		// `wrap.Pressed()` 问出来（见 like.go 的 viewLikeRing）。
		s.viewLikeRing(c, wrap.Pressed())
	})

	if wrap.Clicked() {
		// 🔴 **长按三连之后松手补发的那一次 click 必须吞掉**，否则凭空弹出全屏
		// 播放器（原因见 like.go 文件头：Biu 那边漏了它是一次多余的赞）。
		if s.like.swallowClick() {
			return
		}
		s.openFullPlayer()
	}
}

// viewPrevButton / viewNextButton 是那两首键。分成两个是为了全屏播放器能把它们
// 摆到播放键**两边**（Biu 的 `MusicPlayControl` 就是 上一首 · 播放 · 下一首）。
//
// 能不能按看的是队列：没放过歌、或者只有刚打开的那一首（没进队列）时是灰的。
// 队列里只有一首歌时按钮仍然是亮的——按下去听到的是同一首从头来，
// 表现成按了没反应更让人困惑。
//
// ⚠️ 原来是 88 宽的三个汉字按钮，而三个汉字在按钮里要 54——**44 就已经放不下**，
// 按 64 画出来是「上...」「下...」，看着像坏了。图标化之后这件事不再存在：
// 22 的图标 + 每边 5 的内边距，点击区 32（`iconHitMd`），比原来那个 88 窄近三倍
// 还点得准（角上也点得到，见 `TestTheHitAreaIsBiggerThanTheGlyph`）。
//
// ⚠️ **图形是 22，不是 18**（票 #173 / G26）：上游那一排三枚是三个数——跳过那两枚
// `size={22}`、中间那枚 48。照 `iconMd`（18）画的话，这一排里**最小的两枚紧挨着
// 最大的那一枚**，中间空着一圈，而用户要找的恰恰是中间那一枚。
//
// ⚠️ **内边距跟着图形走**：命中区恒是 32，而 22 的图标要每边 5 才放得下；
// 还留着 7 的话内容框只剩 18，那枚图形自己就被裁掉 4 像素（而**界面上看不出坏了**
// ——它只是小了一圈）。
//
// # 满屏播放器那一行用的是**另一个入口**
//
// `viewPrevButtonOn` / `viewNextButtonOn` / `viewPlayModeOn` 是同一枚键，**按给定
// 的底**算悬停反馈：满屏那一行坐着的是压暗过的封面，而按页面白底算出来的那一
// 档主色压在那上面等于看不见（票 #248）。
func (s *Shell) viewPrevButton(c *ui.Context) { s.viewPrevButtonOn(c, flat.ThemeBase) }

func (s *Shell) viewPrevButtonOn(c *ui.Context, base flat.Base) {
	if iconKeyOnBase(c, iconPrev, "上一首", iconSkip, !s.app.CanPrev(), base).
		Size(iconHitMd, iconHitMd).Padding(space.None, space.Px5).Clicked() {
		s.app.Prev()
	}
}

func (s *Shell) viewNextButton(c *ui.Context) { s.viewNextButtonOn(c, flat.ThemeBase) }

func (s *Shell) viewNextButtonOn(c *ui.Context, base flat.Base) {
	if iconKeyOnBase(c, iconNext, "下一首", iconSkip, !s.app.CanNext(), base).
		Size(iconHitMd, iconHitMd).Padding(space.None, space.Px5).Clicked() {
		s.app.Next()
	}
}

// viewPlayMode 是播放条上那个切播放模式的按钮，外加它右键出来的那一列。
//
// 照 Biu 的 `src/components/music-play-mode/index.tsx`：按一下在四档里转一格
// （`togglePlayMode`），档位和文案照 `common/constants/audio.tsx:27-48`。
//
// **界面不算下一首**：这一层只调 `App.CyclePlayMode` / `App.SetPlayMode`，
// 「放完下一首是哪首」是 `app.Queue.Advance` 独家的事（见 queue.go 那一节）。
// 这里要是自己按 cur+1 算一遍，随机和单曲两档立刻就错。
//
// 摆在哪儿：右边那一组的**头一个**（`viewPlayBarRight`，照
// `playbar/right/index.tsx:14`）。原来它摆在进度那一行的最右边，那是右边摆不下
// 时借中间那一列的富余（票 #104）；上游本来就在右边这一组。
//
// # 满屏播放器那一行用的是**另一个入口**
//
// `viewPlayModeOn` 是同一枚键，**按给定的底**算悬停反馈：满屏那一行坐着的是压暗
// 过的封面，而按页面白底算出来的那一档主色压在那上面等于看不见（票 #248）。
func (s *Shell) viewPlayMode(c *ui.Context, st app.State) {
	s.viewPlayModeOn(c, st, flat.ThemeBase)
}

func (s *Shell) viewPlayModeOn(c *ui.Context, st app.State, base flat.Base) {
	// 四档各配一个图标（Biu 的 `getPlayModeList`，逐个照它的 `icon` 那一项），
	// 悬停那句话仍然报的是**当前这一档**。
	btn := s.withTip(c, iconKeyOnBase(c, playModeIcon(st.PlayMode), "播放模式", iconMd, false, base).
		Size(iconHitMd, iconHitMd).Padding(space.None, space.Px7),
		st.PlayMode.Label()+"："+st.PlayMode.Desc()+"（点一下换，右键看全部）")

	// 按一下换到下一档。档序（顺序 → 循环 → 随机 → 单曲 → 回顺序）是 App 那边
	// 的 `PlayMode.Next`，照 Biu 的 `getPlayModeList` 次序，两边按几下能转回
	// 同一档才是同一件事。
	if btn.Clicked() {
		s.app.CyclePlayMode()
	}

	// 右键出来的是「全部四档 + 那个开关」。四档摆在一列里，不是让人一格一格按
	// 过去：「我要随机」和「我要单曲」是两种不同的意图，按三下才到得了，而转
	// 错了还得再按几下。
	btn.ContextMenu(func(m *ui.Menu) {
		// 现读一次 Snapshot 而不是接着用 st：这一列是**弹出那一刻**才建的，
		// 而外面那一帧的 st 是主帧读的那一份（和 viewRateMenu 同一个理由）。
		now := s.app.Snapshot()
		for _, mode := range app.PlayModes {
			if m.Item(mode.Desc()).Checked(mode == now.PlayMode).Chosen() {
				s.app.SetPlayMode(mode)
			}
		}
		// 「保持分集顺序」只在随机那一档里摆出来，照 Biu 的那一支
		// （`music-play-mode/index.tsx:37` 那个 `if (playMode === Random)`）：
		// 别的档位里它什么都不改变，摆出来只会让人以为它在别的档里也有用。
		if now.PlayMode == app.PlayModeRandom {
			m.Separator()
			if m.Item("保持分集顺序").Checked(now.KeepPagesOrderInRandom).Chosen() {
				s.app.SetKeepPagesOrderInRandom(!now.KeepPagesOrderInRandom)
			}
		}
	})
}

// pickAndEnqueue 弹文件框，把选中的歌加到队列尾部。
//
// 队列空的时候加歌会直接开始播，所以点一下就能听见声音；之后再加就只是排着。
//
// 这是**唯一**一个弹文件框的入口：播放条的「加歌」和导航栏的「打开文件」
// 都调它。两个入口曾经各写一份，一个只取 paths[0]、还绕过队列直接 app.Open，
// 结果用它打开的歌上一首/下一首永远是灰的。现在只剩这一条路。
//
// ⚠️ 文件框是多选的，选回来的每一条都得进队列——只取 paths[0] 会把用户其余选
// 好的歌静默丢掉，用户在界面上看不出自己选漏了。
//
// ⚠️ 必须放在另一个 goroutine 里：MyGo 把弹框请求 post 到主线程再等系统回调，
// 在主线程里等就是死等，事件循环会卡死。
func (s *Shell) pickAndEnqueue() {
	win := s.win
	go func() {
		paths, err := platform.PickAudioFile(win)
		if err != nil || len(paths) == 0 {
			return
		}
		s.enqueuePicked(paths)
	}()
}

// viewProgress 是那一行进度。
//
// 进度条拖起来是这样分工的：按住的时候值归用户，松手才 seek，seek 完交还给
// 播放器。不这么分的话，后台时钟每 50ms 写回一次位置，会和拖动打架——
// 表现就是拖不动，或者松手又弹回去。
//
// 🔴 **它不是 `ui.Slider`**，是 `playbarProgressSlider`（见 knobslider.go）：
// 框架那一颗的旋钮全程亮着、轨道有 6 像素粗，而按住的时候旋钮会褪成一圈黑边
// （深色底上等于消失）。上游那边是 4 像素的轨道加一颗**只在悬停时才出现**的
// 圆点（`music-play-progress` 的 `h-[4px]` 和 `hideThumb={!showThumb}`）——
// 一颗一直亮着的圆点在整条进度上全程抢注意力，而它是用户真正要抓的东西。
//
// 🔴 **两个时间是 14**（票 #173 / G26）：原来是 12。它们不是「列表里的次要信息」
// （那一档是播放量 / 投稿时间），而是**控件旁边的一行读数**——上游那两个读数
// 是 `text-sm`，和 UP 名同一档。12 在这一行里小得几乎看不见，而这一行恰恰是
// 用户盯着看的一行。
//
// 🔴 **这一行只占中间那一列的 3/4，且居中**（上游
// `components/music-play-progress/index.tsx:25` 的 `w-3/4` + `items-center`）：
// 原来它是 `Grow(1)`，**铺满**中间那一列，于是它的中轴和上面那三键的中轴
// **互相对不上**（实测差 152 像素），而这一条进度是用户盯着看的一行。
// 居中是靠外面那个 Column 的 `AlignItems(ui.Center)`（见 `viewPlayBar`），
// `WidthPercent(75)` 只管宽度。
//
// ⚠️ **两个读数各钉 48 宽**（上游那边是自然宽，`text-sm` 的「0:00」约 37）：
// 钉住是为了读数从「0:00」变成「10:00」时**进度条不左右跳**。代价是进度条
// 比上游短 26（上游 1200 宽下 399.6，我们 373.6）——那是**故意**的：让进度条
// 抖一下比让它短一点更招人烦。
//
// ⚠️ **是「最少 48」，不是「正好 48」**（票 #222）：超一小时的歌读数是
// `01:30:00`（八个字符，比 48 宽），钉死 48 的话它**换行**——画面上那一格变成
// 「01:30:」加「00」两行，而 76 高的播放条里那一行本来就窄，换行之后它是
// **整个播放条上唯一一处错版**。
//
// `MinWidth(48)` + `MaxLines(1)`：平时还是 48（进度条不跳），超一小时时那一格
// 自己撑开、**不换行**（进度条短一点，那一档的例外）。
func (s *Shell) viewProgress(c *ui.Context, st app.State) {
	known := st.Duration > 0
	chs := st.Chapters

	ui.Row(c).Gap(space.Px10).AlignItems(ui.Center).WidthPercent(75).Children(func() {
		ui.Text(c, clockOrDash(st.Position)).FontSize(scale.Font(c, scale.Size14)).TextColor(c.Theme().TextMuted).
			MinWidth(48).MaxLines(1)

		sl := playbarProgressSlider(c, &s.scrub, 0, 1).Grow(1).Label("播放进度").Disabled(!known)
		// 章节点画在滑块自己的画法之上（`DrawOver`；pad 是旋钮留在轨道里的
		// 那半个宽 8，点比 4 像素的轨道粗 3——票 #585）。
		sl = withChapterDots(sl, chs, st.Duration, 8, 7, c.Theme().Background)
		// ⚠️ 这里必须用 Pressed()，不能用 Dragging()。
		//
		// Dragging() 报的是「这个控件正在拖一个值 / 正在被拖动」（Drag 与
		// Draggable 两套机制），普通 Slider 只用 flagDraggable 做拖动**源**，
		// 它自己的 dragRec 是 nil，所以 Dragging() 对滑块**永远是 false**
		//（实测：按下滑块移动 200 DIP，Dragging 仍为 false，Pressed 为 true）。
		// MyGo 自带的 examples/timeline 用的就是 Dragging，那一处的判断是失效的。
		switch held := sl.Pressed(); {
		case held && !s.held:
			s.held = true
			// 记下按下那一帧的指针和位置：松手时拿它们分「点 / 拖」。
			s.scrubPressX, _, _ = sl.PointerPosition()
			s.scrubPressPos = st.Position
			s.app.ScrubStart()
		case !held && s.held:
			s.held = false
			s.app.ScrubEnd(snappedScrub(sl, s.scrub, s.scrubPressX, s.scrubPressPos, st.Duration, chs))
		case !s.held && known:
			// 松手之后进度条跟着播放器走。
			s.scrub = float64(st.Position) / float64(st.Duration)
		}

		total := clockOrDash(st.Duration)
		ui.Text(c, total).FontSize(scale.Font(c, scale.Size14)).TextColor(c.Theme().TextMuted).
			MinWidth(48).MaxLines(1)
	})
}

// textColor 是一条曲目名该用的颜色：没歌的时候用弱化的，别让人以为能点。
func textColor(t *ui.Theme, has bool) ui.Color {
	if has {
		return t.Text
	}
	return t.TextMuted
}

// titleClickable 报**歌名该不该可点**（点了跳 B 站视频页）。
//
// 🔴 **三个条件每一个都要单测**（票 #222 的评审提出来的）：只断「本地文件的歌名
// 点了没反应」是**钉不住的**——真实状态里本地文件**压根没有 B 站地址**，于是
// 「点不动」可以是「没有地址」造成的，而**删掉本地那一半照样全绿**。
// 那一半判据只有靠**人工的组合**（本地文件 + 一个地址）才验得到，所以判据本身
// 抽成一个函数，用表驱动把四种组合各喂一遍（`TestTheTitleClickRule`）。
//
//	有歌 + 不是本地文件 + 有地址 → 可点
//	有歌 + 不是本地文件 + 没地址 → 不可点（在线条目也可能没有稿件身份）
//	有歌 + 本地文件     + 有地址 → 不可点（人工组合）
//	没有歌               + 有地址 → 不可点（人工组合）
//
// ⚠️ **两个「人工组合」在真实状态里不会出现**：本地文件在 B 站上没有页面可跳
// （所以它也没有地址），没有歌的时候更没有地址。它们在这儿是为了**把判据的
// 每一半分开验**——真实状态里它们「顺带」也被前一个条件挡住了。
func titleClickable(st app.State) bool {
	return st.HasTrack && st.Source != app.SourceLocal && st.BiliURL != ""
}

// clock 把时长写成 m:ss；超过一小时写成 hh:mm:ss。
//
// 🔴 **超过一小时是 hh:mm:ss**（票 #222 / 报告 §4.2）：上游那个 `formatDuration`
// （`src/common/utils/time.ts:6-16`）是
//
//	if seconds >= 3600 { return dur.format("hh:mm:ss", {trim: false}) }
//	else               { return dur.format("mm:ss",     {trim: false}) }
//
// 我们原来只有 `m:ss`，于是**一小时以上的歌显示成「75:00」**——那不是时长，
// 那是分钟数（而用户读不出它是分钟数）。真实场景：B 站的歌很少超一小时，但
// 有声书和直播回放会。
//
// ⚠️ **小时是两位**（上游的 `trim: false`）：`01:15:00`，不是 `1:15:00`。
//
// ⚠️ **分钟那一位还是一位**（上游那边是两位，`00:05`）：这一条票面没要求，
// 改它会让所有读数宽一格，而那两个读数是**各钉 48 宽**的（见 `viewProgress`）。
// 要改就两处一起改（`history.go` 的 `playedText` 也在用同一套写法）。
func clock(d time.Duration) string {
	if d <= 0 {
		return "0:00"
	}
	s := int(d.Seconds())
	if s >= 3600 {
		return fmt.Sprintf("%02d:%02d:%02d", s/3600, (s%3600)/60, s%60)
	}
	return fmt.Sprintf("%d:%02d", s/60, s%60)
}

// clockOrDash 是**读数那一格**：读不到就写 `-:--`。
//
// 🔴 **一个减号**（票 #222 / 报告 §4.2）：上游那两个读数在拿不到的时候都是
// `-:--`（`components/music-play-progress/index.tsx:27,48`：
// `currentTime ? formatDuration(currentTime) : "-:--"`），而我们原来总长写的是
// `--:--`（两个减号），和上游差一个字。
//
// ⚠️ **位置那一格在 0 秒时也是 `-:--`**（上游的 `currentTime ?` 把 0 当成
// 「还没有」）：刚开始放的那一秒，读数写「-:--」而不是「0:00」——它说的是
// 「还没读到一个位置」，不是「这一秒零」。
func clockOrDash(d time.Duration) string {
	if d <= 0 {
		return "-:--"
	}
	return clock(d)
}
