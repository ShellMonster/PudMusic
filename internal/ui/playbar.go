package ui

import (
	"fmt"
	"strconv"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/ShellMonster/Biu_Go/internal/platform"
	"github.com/ShellMonster/Biu_Go/internal/service/bili"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/imgcache"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/ink"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/scale"
	"github.com/egoist/mygo/ui"
)

// 播放条的尺寸，数字全部照 Biu 的源码抄，不是自己挑的：
//
//	总高 88px        _research/biu/src/layout/index.tsx:48 的 h-[88px]
//	左右内边距 16px  playbar/index.tsx:23-31 的 px-4
//	封面 56×56       playbar/left/index.tsx:29-45 的 width={56} height={56}
//	封面与歌名 8px   同一处的 space-x-2
//
// ⚠️ 关键是 Biu 那条**只有 px-4，没有上下内边距**：88 高的一行内容垂直居中，
// 56 的封面就正好上下各留 16（88-56=32）。所以这里是 Height(88) + PaddingX(16)
// + 一行 AlignItems(Center)，不是四边都 Padding(16) 再往上堆第二行——那样算
// 出来是 98 起（Biu 那条提示的 toastOffset=90 也会被压穿）。
const (
	playbarHeight = 88
	playbarPadX   = 16
	coverSize     = 56
	coverGap      = 8
	// metaGap 是第二行那几样（UP 名 / 分集 / 赞 / 收藏）之间的间距。
	metaGap = 8

	// playbarRightGap 是右边那一组里键与键之间的缝。Biu 那一行是
	// `space-x-2`（`playbar/right/index.tsx:13`），不是外面那一行的 12。
	playbarRightGap = 8

	// playbarShadowY / playbarShadowBlur / playbarShadowA 是播放条上缘那道阴影。
	//
	// Biu 那一层靠 `shadow-2xl`（`layout/index.tsx:48`）和 Card 上那层 `sm`
	// 浮起来，而**底色和内容区是同一个**（`playbar/index.tsx:26` 的
	// `bg-background`）。统一底色之后那道阴影就是这一条唯一的分界，去掉它
	// 播放条会和内容区糊成一片（浅色下尤其明显：两边都是 `#ffffff`）。
	//
	// 三个数是照 Tailwind 的 `shadow-2xl` 抄的：`0 4px 8px -4px rgb(0 0 0/0.1)`，
	// 往上翻一半（阴影往上洒，所以 y 取负的 2）再把浓度提到 0.24 ——上游那道在
	// 88 高的条上是看得见的，而 0.1 在深色底上等于没有。
	playbarShadowY    = -2
	playbarShadowBlur = 8
	playbarShadowA    = 0.24

	// artistMinW 是 UP 名那一段最少留多宽。
	//
	// ⚠️ **没有它，UP 名会被挤成 0 宽**：`Grow(1)` 的 UP 名在小到只剩一点的时候
	// 一个像素都分不到，而**界面上看不出坏了**（按钮和那句话都还在，只有名字没了）。
	// 留 60 是「还能认出是哪个 UP」的下限，再窄就只剩两个偏旁。
	//
	// 第二行现在是**两个**图标键（赞 / 收藏），每个 `iconHit` = 28、两道
	// `metaGap` = 8，所以它们要 28×2 + 8×2 = **72**。
	// 图标化之前它们是三个 44 宽的文字键（赞 / 收藏 / 下载）加三道缝，要
	// 44×3 + 8×3 = **156**——第二行比原来**窄了 84**（下载跟着上游搬去了右边
	// 那一组，见 `viewPlayBarRight`），这一档的上限松了，而这个下限没动。
	//
	// 本应用的窗口最小宽是 900（`WindowOptions` 的 `MinWidth`），那儿它是完整
	// 的；760 是测试里的窄窗口，这一档是给它的。
	artistMinW = 60
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
// 底色和内容区一样，分界靠的是外层那个 `shadow-2xl`
// （`layout/index.tsx:48`）。我们原来取 `Surface`：**浅色下只差一档，
// 深色下差一整级**（`#27272a` / `#18181b`），于是深色下界面裂成三条带
// （票 #127 的头一条）。三处统一之后层次没丢：上面那条线在。
//
// ⚠️ **卡片和弹窗现在也取 `Background` 了**（票 #172）：上游那张 `Card` 走
// `bg-content1`、那一枚 `Drawer` 走 `bg-background`，两处都等于页面底色，
// **浮起来靠的是那圈影**（`widgets/shadow` 里 HeroUI 那几档）。所以「靠底色
// 亮一档来分层」这一句已经作废——别再照着它去找「亮一级的板」。
//
// 🔴 **宽度账**（2026-10 票 #135 之后量的，深浅两套一样）：
//
//	窗口   播放条可用   右边那一组   左边那一列   中间那一列   进度条
//	 900       868          221         311.5        311.5      195.7
//	1200      1168          221         455.5        455.5      345.7
//	1600      1568          221         655.5        655.5      545.7
//
//	（可用 = 窗口 - 左右各 16 的内边距；左边和中间**各分一半**多出来的那一份，
//	 所以窗口每宽 100，进度条就长 50。）
//
//	**歌名和 UP 名都是自然宽**（歌名 208.4、长的那个 UP 名 249.7），它们不跟着
//	窗口长——左边那一列多出来的那一份是空的。票 #135 明确「不改左侧那一块的
//	布局」，所以那一份空着是有意的。
//
//	⚠️ **右边那一组是 221**（不是票 #127 那时的 362）：音量收成一枚图标之后，
//	 常驻的那根 120 像素横滑块没有了（见 `viewPlayBarRight`）。
func (s *Shell) viewPlayBar(c *ui.Context, st app.State) {
	// 记一笔「播放条建了一遍」——给「一帧建了几遍」那条测试数（见 builds.go）。
	s.builds.playbar++
	t := c.Theme()

	ui.Column(c).Key("playbar").Height(playbarHeight).PaddingX(playbarPadX).
		Background(t.Background).
		Shadow(0, playbarShadowY, playbarShadowBlur, 0, ui.RGBA(0, 0, 0, playbarShadowA)).
		Children(func() {
			ui.Row(c).Fill().Gap(12).AlignItems(ui.Center).Children(func() {
				// 左：封面 + 歌名。封面与歌名之间是 8（coverGap），歌名与右边那两块
				// 之间仍是 12。
				ui.Row(c).Gap(coverGap).AlignItems(ui.Center).Grow(1).Children(func() {
					s.viewCover(c, st)
					s.viewTrackText(c, st, t)
				})

				// 中：控制在上、进度在下。Biu 的 playbar/center 就是一个 flex-col。
				//
				// 🔴 **这一列 `Grow(1)`**：进度条原来永远是 80 像素宽，因为中间这一列
				// **不参与**剩余空间的分配——窗口从 900 拉到 1600，多出来的宽度全被
				// 歌名吃掉。左边那一列和这一列**各分一半**多出来的那一份，于是
				// 进度条随窗口单调变长（实测 900 → 195.7、1200 → 345.7、
				// 1600 → 545.7，每一档正好是窗口宽的一半；整张表见上面那段）。
				//
				// 上游那边中间那一份更大（`grid-cols-[minmax(0,1fr)_minmax(0,3fr)_minmax(0,1fr)]`），
				// 但那一套是**三列都按比例**分的，而右边那一组是**定宽**的（六个键
				// 加五道缝），照抄过来右边那一份会被挤扁——所以这里取「一半」这个
				// 折中：左边那一份仍然够摆封面加歌名，进度条也真的长起来了。
				ui.Column(c).Gap(2).Grow(1).Children(func() {
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
// 🔴 **这一组现在是 221 宽**（六个键 + 五道 8 的缝）：模式 28、下载 28、队列 28、
// 音量 28、倍速 41（贴字宽）、加歌 28。票 #127 那时它是 362——多出来的是那根
// **常驻的 120 像素横滑块**加一道缝，票 #135 把它收成了图标。
func (s *Shell) viewPlayBarRight(c *ui.Context, st app.State) {
	ui.Row(c).Key("playbar-right").Gap(playbarRightGap).AlignItems(ui.Center).Children(func() {
		// 播放模式。**原来它摆在进度那一行的最右边**，而上游摆在右边这一组
		// 的头一个（`playbar/right/index.tsx:14`）。搬回来之后中间那一列还
		// 腾出 38（一个 28 的键加一道 10 的缝），而右边这一组多花同样的数。
		s.viewPlayMode(c, st)

		// 下载。**没有歌时不摆**——Biu 那一支的条件是
		// `{Boolean(playId) && getPlayItem()?.source !== "local" && …}`
		// （`playbar/right/index.tsx:15`）。
		//
		// ⚠️ **我们只实现了前半截（`playId`），后半截（`source !== "local"`）
		// 没实现**：本地文件照样摆着这一枚。**这是改之前就有的行为**——那一枚原来
		// 在左边第二行（`viewTrackMeta`），同样只判「有没有歌」。
		//
		// 为什么没顺手补上：那一支要的是「当前这一首是不是本地文件」，而
		// `app.State`（这一行读的那一份快照）**没有来源这一栏**——`Item.Source`
		// 在队列那一层（`app/queue.go:31`），不在快照里。补它要么给 `State` 加
		// 一栏（动应用层），要么在 view 里每帧去问一次 `App.PlayingItem()`
		//（那会绕过快照，而这一层是**每帧现读快照**的）。两样都超出这一票
		// 「重排控件」的范围，所以这一票只把话说准。
		if st.HasTrack {
			s.viewDownloadButton(c, st)
		}

		// 播放列表抽屉的开关（原来在顶栏，见 navbar.go）。上游它就在右边这一组
		// 的第三个（`playbar/right/index.tsx:16`），而顶栏那一枚是当初右边摆不下
		// 时挪过去的——现在右边腾得开，两处只能留一个，留两个就会不一致。
		if s.withTip(c, iconButton(c, iconQueue, "播放列表", iconMd).
			Size(iconHitMd, iconHitMd).Padding(0, 5), drawerTip(s.drawerOpen)).Clicked() {
			s.drawerOpen = !s.drawerOpen
		}

		// 音量收成**一枚图标 + 悬停浮出的竖滑块**（上游 `music-volume` 那个做法），
		// 见 playbar_volume.go。
		//
		// ⚠️ **常驻的那根 120 像素横滑块没有了**：它抢的是进度条该有的空间——
		// 那一档右边这一组是 362 宽，而窗口下限是 900。收成图标之后右边这组窄了
		// 141（362 → 221），中间那一列（进度条）拿得回来的宽度更多。
		s.viewVolumeButton(c, st)

		// 倍速紧挨着音量：两个都是「听感」旋钮（上游也是挨着的）。
		s.viewRate(c, st)

		if s.withTip(c, iconButton(c, iconEnqueue, "加歌", iconMd).
			Size(iconHitMd, iconHitMd).Padding(0, 5), "把本地文件加到队列末尾").Clicked() {
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

// viewTrackText 是封面右边那几行字：歌名、UP 名（可点）+ 赞 + 收藏，
// 出错时再补一句原因。
//
// 三行竖着堆，行数由**有没有那一句**决定：平时两行（歌名 / UP 名那一行），
// 出错时三行。三行实测占 51（歌名 17.7 + 两行各 14 + 两道 4 的缝），在 88 高的
// 播放条里上下各留 18——而横向仍然是 `Grow(1)` 那一块的整条宽：**下面两行的
// 固定宽度不吃歌名的宽度**（这一条是「为什么第二行不挤歌名」的答案，
// `TestTheMetaRowDoesNotSquashTheTitle` 盯着它）。
//
// ⚠️ **外面必须是 Column**。这里曾经为了塞下音质徽章把 Column 拆成了一个裸
// Row（歌名和徽章并排），结果错误文字变成了这一层的**兄弟**——它落到了
// 歌名右边的外层行上，把歌名挤成 20px 宽、88px 高的竖条并被裁掉。
// 而「出错时播放条上有一句人话」正是这一票要保证的东西，结构不能为它让路。
//
// ⚠️ **UP 名那一行是后来才有的**（A23），错误那一行是从第二行掉到第三行的。
// 掉下来之后 `TestTheErrorSitsUnderTheTitle` 仍然成立——它断的是「错误在歌名
// 的下面」，不是「错误紧贴着歌名」。
//
// 徽章是**在 Column 里面**跟歌名并排的：外层永远是竖着堆的行，徽章只占第一行
// 里的右边一小块。
func (s *Shell) viewTrackText(c *ui.Context, st app.State, t *ui.Theme) {
	title := st.Title
	if !st.HasTrack {
		title = "还没有打开文件"
	}

	ui.Column(c).Grow(1).Children(func() {
		ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
			// ⚠️ **一行，截断**——照 Biu：`left/index.tsx` 的歌名是
			// `min-w-0 flex-1 truncate`。以前这里不设 MaxLines，窗口一窄它就换行、
			// 换到三行，而**下面两行一格都分不到**：88 高的播放条塞不下
			// 「三行歌名 + UP 名 + 错误」，多出来的那两行被裁掉，而**界面上看不出坏了**
			//（歌名还在，只是下面的话看不见了）。一行 + 省略号之后超长的那部分看不全，
			// 但下面每一行都还在。
			ui.Text(c, title).FontSize(scale.Font(c, 15)).Bold().
				TextColor(textColor(t, st.HasTrack)).
				MaxLines(1).Ellipsis("…")
			s.viewQualityBadge(c, st, t)
		})
		s.viewTrackMeta(c, st, t)
		if st.Err != "" {
			// 🔴 **一行 + 省略号，和歌名同一个理由**：这一列最窄的时候（760 宽
			// 的测试窗口，比应用自己的下限 900 还窄）只剩 70，而这一句话有 14 个
			// 字——换成两行就是四行，**88 高的播放条装不下，最后一行被裁掉**，
			// 而症状是「说了有话却看不见」（文本节点还在，测试也还找得到它）。
			//
			// 缩掉的那几个字用悬停补回来，和上面那个音质徽章同一个做法。
			s.withTip(c, ui.Text(c, "打不开："+st.Err).FontSize(scale.Font(c, 12)).
				TextColor(t.Danger).MaxLines(1).Ellipsis("…"), "打不开："+st.Err)
		}
	})
}

// viewTrackMeta 是歌名下面那一行：UP 名（点它进他的空间）、分集、赞、收藏。
//
// **摆在这里而不是右边那一排**：右边那一组的次序是照 Biu 定的（见
// `viewPlayBarRight`），而这一行摆的是**上游左边那一块**里的三枚
// （`playbar/left/index.tsx:86-90`：分集 / 收藏 / 赞）。第二行是空的，
// 歌名那一行的宽度也不被它吃（下面那段）。
//
// 「分集」摆在赞/收藏**前面**，照的是 Biu 播放条上的次序
// （`playbar/left/index.tsx` 的 `{hasMultiPart && <PageListDrawer/>}` 紧跟着
// `MusicFavButton` / `MusicThumb`）。
//
// ⚠️ **它摆在这儿最贵的一次是花掉 UP 名 36 个像素（`iconHit` 28 + 一道 `metaGap` 8），
// 别的什么都不花；而窗口一宽就连那 36 也不用花**——这是量的，不是推的。同一首歌、
// 同一块屏幕，**单 P（不摆这个键）和多 P（摆）两档各量一次**（UP 名的自然宽是 249.7）：
//
//	窗口    UP 名 171.7→135.7   196.7→160.7   221.7→185.7   249.7→235.7   249.7→249.7
//	        900          950          1000          1100           1200/1280
//	    （让位 36）  （让位 36）  （让位 36）  （让位 14）     （让位 0）
//
// **每一档的歌名（208.4）、进度条（195.7 / 220.7 / 245.7 / 295.7 / 345.7 / 385.7）、
// 播放模式（28）、下载（28）、加歌（28）两档都一模一样**——这个键不花它们一分钱。
//
// 1200 以上让位是 0，原因是那一行**够宽到装得下 UP 名的自然宽再加这个键**，`Grow(1)`
// 于是没东西可挤。这不是「宽窗口下碰巧没事」，而是 `Grow` 的正常行为：挤的永远是
// 那个可伸缩的东西，而它在这一行里只有 UP 名一个（`Grow(1)` + `MinWidth` +
// `Ellipsis("…")`，见 `viewArtist`）。在应用自己的窗口下限 900 上它让出 36，而 135
// 仍远在 `ownerNameMinW`（40）之上。
//
// ⚠️ **另一处也量过：中间那一列的第二行**（挨着播放模式）——摆在那儿一样只让 UP 名
// 让出 38 左右，歌名和进度条同样一分不丢。两处几乎等价，所以这一条**不是算出来的，
// 是照 Biu 挑的**（Biu 的分集入口就摆在收藏/收藏夹旁边）。
//
// ⚠️ **上表是 2026-10（票 #135 把音量收成图标、中间那一列改成参与分配之后）
// 重量的**。改之前左边那一列**独占**多出来的宽度（中间那一列不参与分配），
// UP 名因此更宽（900 上是 146），而进度条永远是 80。
// **注释里的数是量出来的，量变了就得改它**——`partdrawer_test.go` 的
// `TestThePartsKeyCostsNothingElseOnThePlayBar` 每一次都在重跑那两档。
//
// ⚠️ **分集那个键是逐帧问出来的**（`viewPartButton`），单 P 的稿子不摆——所以这一行
// 的宽度是变的，而上面那几段注释里量出来的富余是按**摆满了**算的。
//
// 没有歌时这一行一格都不画：给一个空的播放条摆两个点不动的键，比不摆更让人
// 以为它们坏了。
func (s *Shell) viewTrackMeta(c *ui.Context, st app.State, t *ui.Theme) {
	if !st.HasTrack {
		// ⚠️ **开着的分集浮层要收掉**：这一行的其余部分一格都不画，而
		// `viewPartButton`（连同它开头那句「没有歌就收掉」）在这一行里面——不在这儿
		// 也收一遍的话，「歌放完了 / 用户按了停止」之后 `partOpen` 一直留着 true，
		// 等下一首多 P 的歌放起来，浮层**自己弹出来**（用户什么都没按）。
		// 用户动作和 A12 那条不是一回事：换歌时 `viewPartButton` 每一帧都在跑，
		// 收得住；**没有歌**的那一段它压根没被调到。
		s.partOpen = false
		return
	}
	ui.Row(c).Gap(metaGap).AlignItems(ui.Center).Children(func() {
		if st.Artist != "" {
			s.viewArtist(c, st, t)
		}
		s.viewPartButton(c, st)
		s.viewLikeStar(c, st)
		s.viewFavButton(c, st)
	})
}

// viewArtist 是那一行里的 UP 名。**点它进 `/user/{id}`**（Biu 的播放条上就是
// 这么摆的：`playbar/left/index.tsx` 的 ownerName 是个链接）。
//
// ⚠️ **有名字、没有编号时照样显示，只是点不动**：B 站有的接口只给名字不给
// mid（见 `app.Item.ArtistMid`）。把名字藏起来是另一种撒谎——用户看到的是
// 「这一首没有 UP 主」。
//
// ⚠️ 走的是 `upPathPrefix` 而不是写死 `"/user/"`：路由表里那一条改了的话
// 这里跟着错，而且错得很安静（空间页退回占位页，没人看得出来）。
func (s *Shell) viewArtist(c *ui.Context, st app.State, t *ui.Theme) {
	if st.ArtistMid <= 0 {
		ui.Text(c, st.Artist).Grow(1).MinWidth(artistMinW).FontSize(scale.Font(c, 12)).
			TextColor(t.TextMuted).MaxLines(1).Ellipsis("…")
		return
	}
	// 扁平按钮：UP 名这一格是**播放条上一枚按钮**，而调研点名的描边药丸里就有它
	//（报告 §1.1 的 F1 证据：播放条上每一枚带描边的键）。它上面是 12px 的弱化
	// 灰字，本来就不该配一块描边底。
	if s.withTip(c, flatButton(c, st.Artist).Grow(1).MinWidth(artistMinW).
		FontSize(scale.Font(c, 12)).Padding(0, 2).TextColor(t.TextMuted).
		MaxLines(1).Ellipsis("…"), "打开 "+st.Artist+" 的空间").Clicked() {
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
	btn := s.withTip(c, iconButton(c, favIcon(faved), "收藏", iconSm).
		Size(iconHit, iconHit).Padding(0, 7), favTooltip(faved))
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
			s.addToast("这一首还没有取回来，收藏不了")
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
	if s.withTip(c, iconButton(c, iconDownload, "下载", iconMd).
		Size(iconHitMd, iconHitMd).Padding(0, 5), "把正在放的这首下载到本地").Clicked() {
		it, ok := s.app.PlayingItem()
		if !ok {
			s.addToast("这一首还没有取回来，下载不了")
			return
		}
		s.downloadLater(it, false, "下载音频")
	}
}

// viewQualityBadge 是歌名右边那个音质徽章（「无损 FLAC」/「320K」）。
//
// 它同时是**换音质的入口**：点一下开菜单，五档里选一档。
//
// 为什么是徽章而不是另摆一个控件：播放条只有 88 高，左右两边（进度条、音量、
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
	s.withTip(c, ui.Text(c, st.Quality).FontSize(scale.Font(c, 11)).TextColor(t.TextMuted),
		"音质："+st.Quality+"（点一下换）").
		Menu(func(m *ui.Menu) {
			for _, q := range qualityChoices {
				// `Checked` 是「当前选的是这一档」。选的和给的要能分开看：
				// 降级发生的时候，用户要能看见自己选的和系统给的不是同一档。
				if m.Item(q.Label()).
					Checked(app.BiliToPref(q) == st.QualityPref).
					Chosen() {
					s.app.SetAudioQuality(q)
				}
			}
		})
}

// qualityChoices 是徽章菜单里的五档。
//
// 顺序从「交给 B 站决定」到「省流量」，和设置里那份一致。标签取自
// `bili.AudioQuality.Label`——那是「用户要什么」的说法，不是「B 站给了什么」
// （后者是徽章），两者混起来就看不出有没有降级。
var qualityChoices = []bili.AudioQuality{
	bili.AudioAuto,
	bili.AudioLossless,
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
// ⚠️ **「上一首」「下一首」两个按钮的本体一个字没动**（`viewPrevButton` /
// `viewNextButton` 满屏那边也在用），动的只是它们在这一行里的位置。
//
// ⚠️ **三个都是只有图标的按钮**（Biu 的 `MusicPlayControl` 就是上面那三个）。
// 文字版是三个汉字各占 88 宽——中间那一列 276 宽全被这三个键占满，而进度条
// 那一行是从它这儿借的富余（见 viewProgress）。换成图标之后那一行只有
// **138** 宽（上一首 28 + 12 + 播放 58 + 12 + 下一首 28），歌名那边宽出来的
// 就是「加得下东西」的那一段。
//
// 无障碍名字一个字没少（「上一首」「播放」「暂停」「下一首」），所以读屏软件
// 念出来和原来一字一样——变的是**画出来的**，不是**念出来的**。
func (s *Shell) viewTransport(c *ui.Context, st app.State) {
	ui.Row(c).Gap(12).AlignItems(ui.Center).Children(func() {
		s.viewPrevButton(c)

		if st.Playing {
			if s.withTip(c, iconButtonPrimary(c, transportIcon(true), "暂停", iconPlayBtn).
				Size(iconHitPlay, iconHitPlay).Padding(0, 5), "暂停").Clicked() {
				s.app.Toggle()
			}
		} else {
			if s.withTip(c, iconButtonPrimary(c, transportIcon(false), "播放", iconPlayBtn).
				Size(iconHitPlay, iconHitPlay).Padding(0, 5).
				Disabled(!st.HasTrack), "播放").Clicked() {
				s.app.Toggle()
			}
		}

		s.viewNextButton(c)
	})
}

// viewCover 是播放条左边那张封面。**点它开全屏播放器**。
//
// 四种状态（没有地址 / 载入中 / 就绪 / 加载失败）都由 imgcache.View 画，
// 这里不重复它的判断，也不替它决定长什么样——以后缓存换画法只改那一处。
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
		imgcache.View(c, s.images, st.CoverURL, coverSize, coverSize).
			Label("当前封面").
			Transition(coverTransition(coverIdentity(s, st)))

		if wrap.Hovered() {
			// Biu 那边是 hover 时浮一层 `bg-[rgba(0,0,0,0.5)]` 加一个向上的箭头
			// （`layout/playbar/left/index.tsx:38-41`，图标是 `RiArrowUpSLine`）。
			// 没有这一层的话，用户不知道那张封面是能点的。
			//
			// 那一层浮层上是**白**的半透明，而画布是深色遮罩——所以这里给了它
			// 一个具体的颜色，不跟主题走（跟主题走的话深色主题下会是浅灰，
			// 压在黑罩上反而看不清）。这是**唯一**一处图标不该跟主题走的地方。
			//
			// PassThrough：浮层这一层和里面那个图标都不接点击。
			ui.Row(c).Absolute().Left(0).Top(0).Right(0).Bottom(0).Radius(c.Theme().Radius).
				PassThrough().Center().Background(ui.RGBA(0, 0, 0, 0.5)).Children(func() {
				ui.Icon(c, icon(iconFullscreen)).FontSize(20).
					TextColor(ui.RGBA(255, 255, 255, 0.92)).
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
// 18 的图标 + 每边 5 的内边距，点击区 28（`iconHitMd`），比原来那个 88 窄三倍
// 还点得准（角上也点得到，见 `TestTheHitAreaIsBiggerThanTheGlyph`）。
func (s *Shell) viewPrevButton(c *ui.Context) {
	if s.withTip(c, iconButton(c, iconPrev, "上一首", iconMd).
		Size(iconHitMd, iconHitMd).Padding(0, 5).
		Disabled(!s.app.CanPrev()), "上一首").Clicked() {
		s.app.Prev()
	}
}

func (s *Shell) viewNextButton(c *ui.Context) {
	if s.withTip(c, iconButton(c, iconNext, "下一首", iconMd).
		Size(iconHitMd, iconHitMd).Padding(0, 5).
		Disabled(!s.app.CanNext()), "下一首").Clicked() {
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
func (s *Shell) viewPlayMode(c *ui.Context, st app.State) {
	// 四档各配一个图标（Biu 的 `getPlayModeList`，逐个照它的 `icon` 那一项），
	// 悬停那句话仍然报的是**当前这一档**。
	btn := s.withTip(c, iconButton(c, playModeIcon(st.PlayMode), "播放模式", iconMd).
		Size(iconHitMd, iconHitMd).Padding(0, 5),
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
func (s *Shell) viewProgress(c *ui.Context, st app.State) {
	known := st.Duration > 0

	ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
		ui.Text(c, clock(st.Position)).FontSize(scale.Font(c, 12)).TextColor(c.Theme().TextMuted).Width(48)

		sl := playbarProgressSlider(c, &s.scrub, 0, 1).Grow(1).Label("播放进度").Disabled(!known)
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
			s.app.ScrubStart()
		case !held && s.held:
			s.held = false
			s.app.ScrubEnd(s.scrub)
		case !s.held && known:
			// 松手之后进度条跟着播放器走。
			s.scrub = float64(st.Position) / float64(st.Duration)
		}

		total := clock(st.Duration)
		if !known {
			total = "--:--"
		}
		ui.Text(c, total).FontSize(scale.Font(c, 12)).TextColor(c.Theme().TextMuted).Width(48)
	})
}

// textColor 是一条曲目名该用的颜色：没歌的时候用弱化的，别让人以为能点。
func textColor(t *ui.Theme, has bool) ui.Color {
	if has {
		return t.Text
	}
	return t.TextMuted
}

// clock 把时长写成 m:ss。
func clock(d time.Duration) string {
	if d <= 0 {
		return "0:00"
	}
	s := int(d.Seconds())
	return fmt.Sprintf("%d:%02d", s/60, s%60)
}
