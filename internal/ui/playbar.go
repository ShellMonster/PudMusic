package ui

import (
	"fmt"
	"strconv"
	"time"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/notice"
	"github.com/ShellMonster/PudMusic/internal/platform"
	"github.com/ShellMonster/PudMusic/internal/service/bili"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/flat"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/imgcache"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/list"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/pud"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/scale"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/scrim"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/space"
	tclean "github.com/ShellMonster/PudMusic/internal/ui/widgets/title"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/tok"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/typo"
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
//	封面 │ 歌名·UP名（文字列）│ 评论（横着并排的一行）
//
// 段与段之间 8（上游那一行的 `space-x-2`）。改之前我们把三枚键摆在**歌名下面
// 那一行**（和 UP 名并排），于是它们和「歌名/UP 名」是**上下**的关系；上游是
// **左右**的关系（见 `viewTrackActions`）。
const (
	playbarHeight = 84
	playbarPadX   = space.Px20
	coverSize     = 56
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
	coverRadius = 4

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

	// playbarActionsW 是第三段那几枚键在**摆满**时有多宽：**64**。
	//
	// 每一枚的盒子恒是 32（上游 HeroUI 的 `isIconOnly` 是 `w-[2rem] h-[2rem]`），
	// 图形是 18（上游播放条上全部图标键都是 18，票 #219 / 报告 §3.6）。
	//
	// 🔴 **喜欢歌单原型之后是「喜欢 + 评论」两枚**（都条件摆：本地文件不摆，
	// 见 `viewTrackActions`）：「赞」「收藏」两枚键连同它们的数量角标一起
	// 去掉了——喜欢/收藏走「喜欢」心形那一路，不是 B 站点赞。心形是**喜欢
	// 歌单绑定入口**（`likeCurrent` → 绑定流程），和迷你窗、刘海、满屏、
	// 列表行上那颗心是同一枚。「分集」那枚挪在右列（`viewPlayBarRight`），
	// 不在这一段。
	//
	// 这个数取的是**摆满**那一档——它是左列的**保底宽**用的
	// （见 `playbarLeftMinW`），而保底宽钉的是**最大**那一档，于是条件摆的
	// 那两枚只让**歌名那一列**多出它们自己那两格（理由同「有没有分集」那一条）。
	playbarActionsW = 2 * iconHitMd

	// playbarActionsGap 是第三段那几枚键**两两之间**的缝（3）。
	playbarActionsGap = space.Px3

	// trackTextMinW 是**文字列**那一段（歌名 + UP 名）最少有多宽。
	//
	// ⚠️ **它不是「歌名有多宽」，是「这一段最少保底多少」**：歌名那一行还要
	// 让出音质徽章（28 + 一道 6 的缝 = 34），所以这一段保底 76 的时候，歌名
	// 自己只剩 42——**两个多字**（徽章是短版「无损」/「杜比」的时候是 48.1，
	// 三个多字——见 `viewQualityBadge`）。
	//
	// 这不是我们挑的，是上游那个结构的账：左边那一列只分到整条播放条的 1/5
	// （上游 `grid-cols-[minmax(0,1fr)_minmax(0,3fr)_minmax(0,1fr)]`），而上游
	// 那三枚键（分集 / 赞 / 收藏）又占掉 96 + 8。**上游在 1200 宽（它自己的
	// 默认窗口）上歌名那一列只有 65.6**，比我们还窄（报告 §2.8 的表）。
	//
	// ⚠️ **真嫌窄就动 `playbarLeftMinW`**（那个数是左列的保底宽），或者动
	// 「歌名 + UP 名」这一列的分配——**别动这一段的保底**：UP 名要 60
	// （`artistMinW`），歌名要多少是另一件事。
	trackTextMinW = 76

	// playbarLeftMinW 是左边那一列的最小宽度。
	//
	//	封面 52 │ 缝 8 │ 文字列 76 │ 缝 8 │ 两枚键 64
	//
	// 🔴 **它钉的不是「好看」，是「多一 P 那一档和单 P 那一档一样宽」**。
	//
	// 左列在 Grid 的一格（1 份）里，而框架给格子里那一块的宽度是它**自己的最小
	// 内容宽**——那个数随着第三段摆几枚键而变：多 P 时多一枚分集键，那一段就
	// 宽 32，于是**左列宽 32、歌名窄 32**。而歌名窄了是**界面上看不出坏了**的
	// 那种（字还在那儿，只是截断得更早）。
	//
	// 钉住之后，评论键摆不摆左列都是这个宽——条件摆的那一枚只让**歌名那一
	// 列**多出它自己那一格（理由同原来「有没有分集」那一条）。
	//
	// ⚠️ **喜欢歌单原型之后它是 208**（心 + 评论两枚）：赞 / 收藏两枚键连同
	// 角标一起去掉了，心形摆回来之后这一段保底宽按**两枚都摆**算。
	playbarLeftMinW = coverSize + coverGap + trackTextMinW + coverGap + playbarActionsW
)

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
// 对齐，**那一排键不在这一列**——它们是**平级的第三段**（见 `viewTrackActions`）。
// 改之前那三枚键和 UP 名挤在歌名下面那一行，于是「歌名/UP 名」和「三枚键」是
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
	artist, artistMid := st.Artist, st.ArtistMid

	// 🔴 **出错时这一列写的是「失败的那一首」**（票：左下角出错那三行）。
	//
	// 出错时播放器压根没装上，而 `st.Title` 是**正在放的那一首**——于是那一列
	// 显示的是**上一首**：用户读到的是「这一首正在放，但它打不开」，而打不开
	// 的其实是刚点的**另一首**。两首对不上号，是这一列最难懂的一种形态。
	//
	// ⚠️ **这一支要排在「没有歌」那一支前面**：冷启动就失败的时候
	// （`HasTrack=false`、`FailedTitle` 有值），用户要知道的正是**失败那一首**
	// 的名字——「还没有打开文件」那句占位话会把它整个盖掉（评审抓到的就是
	// 这个顺序问题）。
	//
	// ⚠️ **作者编号不带**（`artistMid = 0`）：那一栏记的是失败那一首的作者，
	// 但跳转要的编号没有跟着记（它在那一场失败里没有用——用户下一步是重试或
	// 去 B 站，不是进作者空间）。
	if st.Err != "" && st.FailedTitle != "" {
		title = tclean.Clean(st.FailedTitle).Title
		artist, artistMid = st.FailedArtist, 0
	} else if !st.HasTrack {
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
	ui.Column(c).Grow(1).MinWidth(0).Gap(space.None).Children(func() {
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
			name := ui.Text(c, title).FontSize(scale.Font(c, scale.Size14)).FontWeight(600).FixedLineHeight(21.7).
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
		})
		// UP 名（点它进他的空间）。**没有歌不摆**（票 #219 之前是
		// `viewTrackMeta` 第一行就 return）。
		//
		// 🔴 **出错时这一行写的是失败那一首的作者**（和上面那一行同一个道理）。
		if st.HasTrack && artist != "" {
			s.viewArtist(c, artist, artistMid, t)
		}
		// 🔴 **出错、「正在重连」、「正在取流」和「停住了」占同一个位置**
		// （票 #282、#315、#319）：四者都是「这一句说明现在怎么了」，而那一行
		// 只有一行宽。
		//
		// 🔴 **错一收，「详情」面板也得收**（票 #320）：**在 switch 之前**判——
		// 错收掉之后紧接着来的可能是「正在重连」「正在取流」「停住了」那几档
		// （票 #315 / #282 / #319），而那块面板说的是上一句错的话，它自己不会走。
		//
		// ⚠️ **复位点必须排在浮层建立之前**：那块面板现在锚在**播放键**上
		// （`viewErrorPlayKey`，中间那一列），而这一列是 Grid 的**第一个**孩子
		// ——本列先建，复位才赶在浮层之前。要是哪天把左列挪到中列之后，
		// 这里就得跟着挪（`viewErrorPlayKey` 里那段注释写着同一件事）。
		// 它们的优先级是**错 > 重连 > 停住**：
		//
		//	出错		一句错比一枚转圈信息量大，而转圈还在转说明还有希望
		//	正在重连	这一首断了，我在救它——比「停住了」具体
		//	停住了	播放器在、只是不响了
		//
		// 🔴 **出错时这一行不说话**（票：左下角出错那三行太碎）：错误文案搬到了
		// **播放键**上（那一枚变成红色的），而这一行宽度只有 75.7 像素（实测）
		// ——一句 26 个字的话上屏只剩十来个。所以出错的**信号**是那一枚键，不是
		// 这一行。
		//
		// ⚠️ **「错 > 重连」这条优先级还在**：出错了就不该还说「正在重连」——
		// 用户要看到的是「为什么不行、还能怎么办」，不是「还在重连」。
		switch {
		case st.Err != "":
			// 出错时这一行**什么都不说**（信号在播放键上）。
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

// viewTrackActions 是左边那一块的**第三段**：评论那一枚键，横着排在
// 「歌名/UP 名」那一列的右边（上游 `playbar/left/index.tsx:85` 的
// `<div className="flex items-center">`）。
//
// 🔴 **喜欢歌单原型之后是「喜欢 + 评论」两枚**（都条件摆：本地文件不摆）：
// 原型把「赞」「收藏」两枚键连同数量角标一起去掉了——喜欢/收藏走「喜欢」
// 心形那一路（这里、满屏左栏、列表行、迷你窗、刘海），不是 B 站点赞。
// 「分集」那枚挪在右列（`viewPlayBarRight`）。
//
// 没有歌时这一段一格都不画：给一个空的播放条摆几枚点不动的键，比不摆更让人
// 以为它们坏了。
func (s *Shell) viewTrackActions(c *ui.Context, st app.State) {
	// ⚠️ **一枚键都不摆的时候这一段整个不占宽**：评论键是条件摆的（本地文件
	// 不摆），心形也是（`Likeable`：本地文件和没有 B 站身份的都不摆）——
	// 留着那一格（连它前面那道缝）的话，歌名那一列会白白窄掉。
	//
	// ⚠️ **外层不拿 `st.HasTrack` 当门**：`HasTrack` 是「播放器装上没有」
	// （`a.player != nil`），在线那首的 Open 是异步的——壳建好的那一帧它
	// 还是 false，而 `Likeable` / `CommentsWorthAsking` 判的是「队列里那一
	// 项能不能赞 / 问评论」，那一个帧早就是 true。拿 `HasTrack` 当门，歌
	// 还在取流的那一段这两枚就都不摆——那是**闪烁**，不是「还没好」。
	if !s.app.Likeable() && !s.app.CommentsWorthAsking() {
		return
	}
	ui.Row(c).Gap(space.Px2).AlignItems(ui.Center).Shrink(0).Children(func() {
		// 心形是**喜欢歌单绑定入口**（`likeCurrent` → 绑定流程），不是 B 站
		// 点赞，也不是收藏夹多选框；实心 / 空心认「进没进收藏夹」。和迷你窗、
		// 刘海、满屏、列表行上那颗心是同一枚（`IconOpts{Row: true}`：命中区
		// 32，和这一段其他键一样大）。
		if s.app.Likeable() {
			faved := s.app.LikeState().Favorited
			name, label := "heart", "喜欢"
			if faved {
				name, label = "heart-on", "已喜欢"
			}
			if s.withTip(c, pud.IconBtn(c, "pb-like", name, label, pud.IconOpts{Row: true, On: faved}),
				favTooltip(faved)).Clicked() {
				s.likeCurrent()
			}
		}
		// ⚠️ **本地文件不摆**（`viewCommentsButton`）：一个点不动的键比不摆
		// 更糟。
		s.viewCommentsButton(c, st)
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
//
// 🔴 **名字和编号是参数**（票：左下角出错那三行）：出错时这一行写的是**失败
// 那一首**的作者，而 `State.Artist` / `State.ArtistMid` 是**正在放的那一首**
// 的（理由见 `State.FailedTitle`）。
func (s *Shell) viewArtist(c *ui.Context, name string, mid int64, t *ui.Theme) {
	if mid <= 0 {
		// 没有编号时是**普通文字**：它在那一列里本来就是左对齐的（列是
		// `AlignItems` 默认的 Stretch，文字从左沿起）。
		ui.Text(c, name).MinWidth(artistMinW).FontSize(scale.Font(c, scale.Size12)).FixedLineHeight(18.6).
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
	//
	// ⚠️ **命中区只到名字那么宽**（`Shrink(1)` 收缩、不 `Grow`）：链接铺满
	// 整段时，指针从右边评论键往左挪、一进那一段的右沿就亮下划线——离名字
	// 墨迹还有几十像素就说「可点」，是在说谎。列表行上同一枚链接（
	// `widgets/pud/track.go` 的 owner 那一格）就是这么写的：按钮收缩成文字
	// 宽，外层靠 `AlignItems(Start)` 保左沿。
	ui.Column(c).MinWidth(0).AlignItems(ui.Start).Children(func() {
		if s.withTip(c, flatLink(c, name).Shrink(1).MinWidth(artistMinW).
			FontSize(scale.Font(c, scale.Size12)).FixedLineHeight(18.6).TextColor(t.TextMuted).
			MaxLines(1).Ellipsis("…"), list.OwnerTipText(name)).Clicked() {
			s.navigate(upPathPrefix + strconv.FormatInt(mid, 10))
		}
	})
}

// favTooltip 是「喜欢」心形悬停时那句话（满屏 / 收藏入口共用）。
//
// ⚠️ **它说的是「我」收藏了没有**（那一枚键自己的状态），不是「整个稿件
// 被收藏了多少次」——那个数是 B 站稿件维度的读数，跟这枚键无关。
func favTooltip(faved bool) string {
	if faved {
		return "已喜欢"
	}
	return "喜欢"
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

// errPlayKeyLabel 是**出错那一档的播放键**在**不可重试**那一档的无障碍名字
// （读屏软件念的是它，而它在画面上是一个图标）。
//
// ⚠️ **它是一个出口**：好几处测试要认出「出错时的那一枚」，而它们认的是
// `Label`——`Tester.Find` 认的是文字和 `Label`，不认 `Key`。这个名字散着写
// 的话，改了一处另一处就安静地对不上。
//
// ⚠️ **可重试那一档的名字是「重试这一首」**（见 `viewErrorPlayKey`）：那一档
// 点下去就是重试，名字说的就是它做的事。
const errPlayKeyLabel = "这一首放不出来，出错了"

// viewErrorPlayKey 是**出错那一档的播放键**。
//
// # 形态
//
//	可重试	危险色底 + **回转箭头**，点它**直接重试**这一首
//	不可重试	危险色底 + **感叹号**，点它**打开「详情」面板**
//	        （完整那一句 + 「在 B 站打开」在里面）
//
// # 为什么两档点下去做的事不一样
//
// 能做的事有**两件**（重试 / 在 B 站里听它），而**两个都摆在播放条上放不下**
// ——那一列只有 75.7 像素宽（实测），一句 26 个字的话上屏只剩十来个。所以：
//
//   - **可重试的那一档，重试就是全部**——网络抖了一下，用户要的就是「再试
//     一次」，一步到位；那一枚的图形（回转箭头）说的正是这件事。
//   - **不可重试的那一档，重试不存在**——重试一万次还是这一句。用户能做的
//     是「看清楚为什么」和「去 B 站里听它」，两样都在面板里。那一枚的图形
//     （感叹号）说的是「这一首放不出来」，不是「按下去能重试」。
//
// 🔴 **两档都是危险色底**：错误文案搬走之后，这一枚就是「出错了」唯一的信号。
//
// 🔴 **字色用 `readableOn`**：危险色在浅色方案下是深红（`#B4492E`），白字压
// 上去够；而经典那套的危险红 `#f31260` 上白字 4.15:1 —— `readableOn` 按
// 对比度挑（判据 ≥3:1，理由见 `semantic.go`）。
//
// ⚠️ **提示里给完整那一句**：播放条上那一行的宽度只有 75.7 像素（实测），
// 一句 26 个字的话上屏只剩十来个——完整那一句在悬停提示和「详情」面板里
// （`errFullBody`）。
func (s *Shell) viewErrorPlayKey(c *ui.Context, st app.State) ui.Element {
	t := c.Theme()

	tip := "打不开：" + errFullBody(st)
	// ⚠️ **面板开着的时候不挂提示框**：提示框和那块面板**摆在同一个位置**，
	// 而它们说的是同一句话——指针移开又移回来的时候，两块叠在一起，屏幕上就
	// 是一块小提示盖着一块面板。
	if s.errDetailOpen {
		tip = ""
	}

	var b ui.Element
	if st.CanRetry {
		b = ui.ButtonBase(c).Label("重试这一首").
			Size(iconHitPlay, iconHitPlay).Padding(space.None, space.None).Radius(iconHitPlay / 2).
			Background(t.Danger).
			OnClick(func() { s.retryPlay() })
		b.Children(func() {
			ic := ui.Icon(c, icon(iconRefresh)).FontSize(iconPlayBtn).
				TextColor(readableOn(t.Danger, ui.RGB(255, 255, 255)))
			ic = flat.PressScale(ic, b.Pressed(), false)
		})
		b = s.withTip(c, b, tip)
		return b
	}

	// 不可重试：那一枚点开是「详情」面板（完整那一句 + 「在 B 站打开」）。
	b = ui.ButtonBase(c).Label(errPlayKeyLabel).
		Size(iconHitPlay, iconHitPlay).Padding(space.None, space.None).Radius(iconHitPlay / 2).
		Background(t.Danger).
		OnClick(func() { s.errDetailOpen = !s.errDetailOpen })
	b.Children(func() {
		ic := ui.Icon(c, icon(iconWarning)).FontSize(iconPlayBtn).
			TextColor(readableOn(t.Danger, ui.RGB(255, 255, 255)))
		ic = flat.PressScale(ic, b.Pressed(), false)
	})
	b = s.withTip(c, b, tip)
	// 🔴 **这一枚是「详情层」的入口**（票 #320）：它点得开——完整那一句在那块
	// 面板里，而「在 B 站打开」也在里面（见 `viewErrorDetail`）。
	//
	// ⚠️ 浮层那一层外面包一层 Guard（铁律三）：它是在**播放条这一层的布局
	// 之外**单独建的，它一崩坏掉的是整块浮层，而报错的地方在播放条上——
	// 两者对不上，极难查。
	s.pbPopover(c, b, &s.errDetailOpen, "错误详情", func() {
		s.guardAt("错误详情", func(c *ui.Context) { s.viewErrorDetail(c, st) })(c)
	})
	return b
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
	wrap := ui.Row(c).Key("playbar-cover").Size(coverSize, coverSize).
		// 🔴 **可聚焦**：没有它，键盘（菜单键 / Shift+F10）**够不着**这一份
		// 菜单——框架的 `menuKey` 找的是「焦点链上最里层那个带菜单的元素」
		// （`mygo/ui/menu.go` 的 `menuTarget`），而焦点链上得先有这一层。
		//
		// ⚠️ **焦点环画在元素外面 3 像素**（`mygo/ui/paint.go` 的 `FocusRing`），
		// 所以它会盖到封面旁边那一圈留白上——**不改布局**（留白是 8，见
		// `coverGap`），只是焦点在的时候多一圈线。
		Focusable().
		// ⚠️ **名字不能叫「当前封面」**：里面那张图已经有一个了
		// （`imgcache.View…(...).Label("当前封面")`），读屏会把两个都念一遍。
		Label(copyCoverActionsLabel)
	s.coverMenuShortcut(wrap)
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
				PassThrough().Center().Background(ui.RGBA(0, 0, 0, scrim.Media)).Children(func() {
				// 原型 `.np-cover .expand`：rgba(0,0,0,.55) 底、白色 20 的展开图标。
				ui.Icon(c, pud.Icon("expand")).FontSize(20).
					TextColor(ui.RGB(255, 255, 255)).
					PassThrough()
			})
		}

	})

	// 右键开原型的「当前封面的操作」弹层（`pud.PopPanel` / `PopOpt`），不再走系统原生菜单。
	if wrap.RightClicked() {
		s.coverMenuOpen = !s.coverMenuOpen
	}
	s.pbPopover(c, wrap, &s.coverMenuOpen, "封面菜单", func() {
		s.guardAt("封面菜单", func(c *ui.Context) { s.viewCoverMenu(c, st) })(c)
	})

	if wrap.Clicked() {
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
func (s *Shell) viewPlayMode(c *ui.Context, st app.State) {
	// 原型 `MODES`：order / repeat / repeat1 / shuffle 四个图标，36 见方的 `.icon-btn`，
	// 非顺序档 `aria-pressed`（accent-text）。
	name := map[app.PlayMode]string{
		app.PlayModeSequence: "order", app.PlayModeLoop: "repeat",
		app.PlayModeSingle: "repeat1", app.PlayModeRandom: "shuffle",
	}[st.PlayMode]
	if name == "" { // 状态里还没有播放模式（刚启动 / 没歌）：按顺序播放画，别拿空名字去找图标
		name = "order"
	}
	btn := s.withTip(c, pud.IconBtn(c, "pb-mode", name, "播放模式",
		pud.IconOpts{On: st.PlayMode != app.PlayModeSequence}), st.PlayMode.Desc())

	// 按一下换到下一档。档序（顺序 → 循环 → 随机 → 单曲 → 回顺序）是 App 那边
	// 的 `PlayMode.Next`，照 Biu 的 `getPlayModeList` 次序，两边按几下能转回
	// 同一档才是同一件事。
	if btn.Clicked() {
		s.app.CyclePlayMode()
		// ⚠️ **播放模式变了，菜单上那个勾选态要跟着变**（菜单是系统画的，不会
		// 自己变，见 refreshMenuChecked）。
		s.refreshMenuChecked(s.app.Snapshot())
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

	ui.Row(c).Gap(space.Px12).AlignItems(ui.Center).FillWidth().MaxWidth(560).Children(func() {
		// 原型 `.seek .mono`：44 宽、等宽 11 号、text-3、居中。
		typo.Mono(ui.Text(c, clockOrDash(st.Position))).FontSize(scale.Size11).TextColor(tok.Of(c.Theme()).Text3).
			Width(44).TextAlign(ui.Center).MaxLines(1).Shrink(0)

		// ⚠️ **`live` 只在「在放」时给**：暂停时位置不动，每帧重画同一个位置
		// 是纯浪费——而它会一直要下一帧，窗口就停不下来（`progress_live_test.go`
		// 的 `TestAPausedBarDoesNotAskForFrames` 钉着这一条）。
		// ⚠️ **`visible` 那道闸不能省**：失焦（但没最小化）时框架自己不管，
		// 链子会在绘制路径里自己续下去（理由见 `selfSliderLive` 那段注释）。
		var live sliderLive
		if known && st.Playing {
			dur := float64(st.Duration)
			live = sliderLive{
				frac: func() float32 {
					return float32(float64(s.app.PlayPosition()) / dur)
				},
				visible: s.presence.seen,
			}
		}
		sl := playbarProgressSliderLive(c, &s.scrub, 0, 1, live).Grow(1).Label("播放进度").Disabled(!known)
		// 已缓冲和弹幕热度画在轨道之外（见 timeline.go）。只对「知道时长」
		// 的那一档画：没有时长时比例是 0/0，画出来是一条不确定位置的线。
		if known {
			buffered := float32(0)
			if st.Buffered > 0 {
				buffered = min(float32(float64(st.Buffered)/float64(st.Duration)), 1)
			}
			sl = withTimelineOverlays(sl, timelineOverlay{
				buffered:    buffered,
				heat:        s.heat.of(s.app.Danmaku().Items, st.Duration),
				pad:         8,
				trackW:      knobHoverLook.trackW,
				bufferedCol: tok.Of(c.Theme()).Text3,
				heatCol:     c.Theme().Accent.Alpha(0.45),
			})
		}
		// 章节标记画在滑块自己的画法之上（`DrawOver`；pad 是旋钮留在轨道里的
		// 那半个宽 8）。原型 index.html:854 `.chap`：宽 4、高 10、圆角 1、`side` 色的竖条
		// （在 4 高的轨道上切出一道 4 宽的口）。
		sl = withChapterBars(sl, chs, st.Duration, 8, 4, 10, 1, tok.Of(c.Theme()).Side)
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
		typo.Mono(ui.Text(c, total)).FontSize(scale.Size11).TextColor(tok.Of(c.Theme()).Text3).
			Width(44).TextAlign(ui.Center).MaxLines(1).Shrink(0)
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
