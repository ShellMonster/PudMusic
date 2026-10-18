package ui

import (
	"fmt"
	"strconv"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/ShellMonster/Biu_Go/internal/platform"
	"github.com/ShellMonster/Biu_Go/internal/service/bili"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/imgcache"
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
	// metaGap 是第二行那几样（UP 名 / 赞 / 收藏 / 下载）之间的间距。
	metaGap = 8

	// artistMinW 是 UP 名那一段最少留多宽。
	//
	// ⚠️ **没有它，UP 名会被挤成 0 宽**：`Grow(1)` 的 UP 名在小到只剩一点的时候
	// 一个像素都分不到，而**界面上看不出坏了**（按钮和那句话都还在，只有名字没了）。
	// 留 60 是「还能认出是哪个 UP」的下限，再窄就只剩两个偏旁。
	//
	// 第二行现在是**三个**图标键（赞 / 收藏 / 下载），每个 `iconHit` = 28、三道
	// `metaGap` = 8，所以它们要 28×3 + 8×3 = **108**。
	// 图标化之前它们是三个 44 宽的文字键加三道缝，要 44×3 + 8×3 = **156**——
	// 也就是说第二行比原来**窄了 48**，这一档的上限松了，而这个下限没动。
	//
	// 本应用的窗口最小宽是 900（`WindowOptions` 的 `MinWidth`），那儿它是完整
	// 的；760 是测试里的窄窗口，这一档是给它的。
	artistMinW = 60
)

// viewPlayBar 是底部那条播放条：封面、歌名、上一首/下一首/播放、可拖的进度条、音量。
//
// 一行摆完，横着分三块，和 Biu 的 playbar/left|center|right 一一对应：
// 左边封面加歌名，中间控制在上进度在下，右边音量和加歌。
func (s *Shell) viewPlayBar(c *ui.Context, st app.State) {
	t := c.Theme()

	ui.Column(c).Key("playbar").Height(playbarHeight).PaddingX(playbarPadX).
		Background(t.Surface).Radius(12).Children(func() {
		ui.Row(c).Fill().Gap(12).AlignItems(ui.Center).Children(func() {
			// 左：封面 + 歌名。封面与歌名之间是 8（coverGap），歌名与右边那两块
			// 之间仍是 12。
			ui.Row(c).Gap(coverGap).AlignItems(ui.Center).Grow(1).Children(func() {
				s.viewCover(c, st)
				s.viewTrackText(c, st, t)
			})

			// 中：控制在上、进度在下。Biu 的 playbar/center 就是一个 flex-col。
			ui.Column(c).Gap(2).Children(func() {
				s.viewTransport(c, st)
				s.viewProgress(c, st)
			})

			// 右：音量 + 倍速 + 加歌。
			//
			// ⚠️ **这一行现在还剩 40 像素的富余，第四个控件摆得下了**
			// （实测：760 宽时左边那一块还有 208，而中间 246 + 右边 266 两块都
			// 是写死的；一个 28 宽的键加一道 12 的缝只要 40）。
			//
			// 这一票之前不是这样：那时候「加歌」是个 56 宽的三个字按钮，中间那一列
			// 的上一首/播放/下一首又是 88+76+88，760 宽时歌名只剩 14.9px（一个字
			// 一行），而**挤没了之后界面上看不出坏了**。图标化把 transport 那一行
			// 从 276 压到 120，右边这一组从 264 变成 266（多了音量图标那 18，
			// 加歌从 56 缩到 28 抵掉 28）。
			//
			// **所以播放列表的开关搬回右边那一组是下一次的事**，不是现在做不了的：
			// issue 93 的边界是「换图标，不重排界面」，所以它继续摆在顶栏
			// （见 navbar.go 的 viewNav）。而 `TestTheErrorSitsUnderTheTitle` 和
			// `TestTheRateButtonDoesNotSquashTheTitle` 还在盯「歌名不能被挤没」。
			//
			// 收藏 / 下载 / 赞**不在右边这一行**：它们在左边那一块的第二行（见
			// viewTrackMeta），而那一行的固定宽度不吃歌名的宽度——它只从第二行
			// 自己那份里面分。
			ui.Row(c).Gap(12).AlignItems(ui.Center).Children(func() {
				// 音量图标是**装饰**：它不可点，旁边那条滑块才是控件，而滑块自己
				// 带着「音量」这个无障碍名字。所以这里刻意不加 Label——加了就是
				// 两个同名的东西，而 `Tester.Find` 只会认到先画的那个（图标），
				// 那些要量滑块宽度的测试就会量到图标上去。
				//
				// 画的是哪一档看音量本身（Biu 的 music-volume 三档同一个做法）。
				ui.Icon(c, icon(volumeIcon(st.Volume))).
					FontSize(iconMd).TextColor(c.Theme().TextMuted)

				// 音量条自己带无障碍标签，不用旁边再摆一个「音量」字。
				vol := st.Volume
				if ui.Slider(c, &vol, 0, 1).Width(120).Label("音量").Changed() {
					s.app.SetVolume(vol)
				}

				// 倍速紧挨着音量：两个都是「听感」旋钮，加歌是另一类事。
				s.viewRate(c, st)

				if iconButton(c, iconEnqueue, "加歌", iconMd).
					Size(iconHitMd, iconHitMd).Padding(0, 5).
					Tooltip("把本地文件加到队列末尾").Clicked() {
					s.pickAndEnqueue()
				}
			})
		})
	})
}

// viewTrackText 是封面右边那几行字：歌名、UP 名（可点）+ 收藏 + 下载，
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
			ui.Text(c, title).FontSize(15).Bold().
				TextColor(textColor(t, st.HasTrack)).
				MaxLines(1).Ellipsis("…")
			s.viewQualityBadge(c, st, t)
		})
		s.viewTrackMeta(c, st, t)
		if st.Err != "" {
			ui.Text(c, "打不开："+st.Err).FontSize(12).TextColor(t.Danger)
		}
	})
}

// viewTrackMeta 是歌名下面那一行：UP 名（点它进他的空间）、分集、赞、收藏、下载。
//
// **摆在这里而不是右边那一排**：右边已经放不下第四个控件了（见 `viewPlayBar`
// 里那段注释——760 宽时歌名只剩一个字一行，而**挤没了之后界面看不出坏了**）。
// 第二行是空的，歌名那一行的宽度也不被它吃（上面那段）。
//
// 「分集」摆在赞/收藏/下载**前面**，照的是 Biu 播放条上的次序
// （`playbar/left/index.tsx` 的 `{hasMultiPart && <PageListDrawer/>}` 紧跟着
// `MusicFavButton` / `MusicThumb`）。
//
// ⚠️ **它摆在这儿最贵的一次是花掉 UP 名 36 个像素（`iconHit` 28 + 一道 `metaGap` 8），
// 别的什么都不花；而窗口一宽就连那 36 也不用花**——这是量的，不是推的。同一首歌、
// 同一块屏幕，**单 P（不摆这个键）和多 P（摆）两档各量一次**（UP 名的自然宽是 249.7）：
//
//	窗口    UP 名 166→130   216→180   249.7→230   249.7→249.7   249.7→249.7
//	        900      950        1000        1100            1200/1280
//	    （让位 36） （让位 36） （让位 19.7）  （让位 0）      （让位 0）
//
// **每一档的歌名（208）、进度条（80）、加歌（900 上 856..884、1200 上 1156..1184）
// 两档都一模一样**——这个键不花它们一分钱。
//
// 1100 以上让位是 0，原因是那一行**够宽到装得下 UP 名的自然宽再加这个键**，`Grow(1)`
// 于是没东西可挤。这不是「宽窗口下碰巧没事」，而是 `Grow` 的正常行为：挤的永远是
// 那个可伸缩的东西，而它在这一行里只有 UP 名一个（`Grow(1)` + `MinWidth` +
// `Ellipsis("…")`，见 `viewArtist`）。在应用自己的窗口下限 900 上它让出 36，而 130
// 仍远在 `ownerNameMinW`（40）之上。
//
// ⚠️ **另一处也量过：中间那一列的第二行**（挨着播放模式，`viewProgress`）——摆在那儿
// 在 900 上让 UP 名让出 38（166 → 128），歌名和进度条同样一分不丢。两处几乎等价，
// 所以这一条**不是算出来的，是照 Biu 挑的**（Biu 的分集入口就摆在收藏/收藏夹旁边）。
//
// 这段注释的前一版写的是「摆在中间列第二行要拿歌名和进度条的钱」（208→182、
// 94→80），**那是 #118 把这一行图标化之前量的**：那时赞/收藏/下载是三个 44 宽的文字
// 键，这一行挤到了边缘；图标化之后它腾出 48（见 `artistMinW` 那段），两处就平了。
// 再往前一版还写过「这一行已经溢出 9、会压到位置时间」——那一条是**按算式推的，实测
// 不重叠**（这一行变宽时中间列跟着一起右移同样多的宽度）。**推出来的账要量一遍才写
// 进注释**，量出来不对就删。
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
		s.viewDownloadButton(c, st)
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
		ui.Text(c, st.Artist).Grow(1).MinWidth(artistMinW).FontSize(12).
			TextColor(t.TextMuted).MaxLines(1).Ellipsis("…")
		return
	}
	if ui.Button(c, st.Artist).Grow(1).MinWidth(artistMinW).FontSize(12).
		Padding(0, 2).TextColor(t.TextMuted).MaxLines(1).Ellipsis("…").
		Tooltip("打开 " + st.Artist + " 的空间").Clicked() {
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
	btn := iconButton(c, favIcon(faved), "收藏", iconSm).
		Size(iconHit, iconHit).Padding(0, 7).
		Tooltip(favTooltip(faved))
	if faved {
		// ⚠️ **只在这一支设颜色**：没收藏的那一支一个字都不改，好让「默认那个颜色」
		// 继续由按钮自己决定（图标从祖先的文字色继承，按钮给的就是它）。
		btn = btn.TextColor(c.Theme().Accent)
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

// viewDownloadButton 是「下载」那个键。
//
// ⚠️ **走 `downloadLater`（后台补 cid），不走 `app.Download`**：队列里那一份
// 是从列表翻出来的，而 B 站的几个列表接口都不给 cid（搜索结果、历史、动态都
// 是），下载按分 P 存文件。缺 cid 时 `Download` 会**如实拒绝**而不是下错一集
// ——可播放条上这一格是「把正在放的这首存下来」，用户要的是那首，补出来的
// 第一集不是。理由和 `dynamiccard.go` 的「下载音频」是同一条。
func (s *Shell) viewDownloadButton(c *ui.Context, st app.State) {
	if iconButton(c, iconDownload, "下载", iconSm).
		Size(iconHit, iconHit).Padding(0, 7).
		Tooltip("把正在放的这首下载到本地").Clicked() {
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
	ui.Text(c, st.Quality).FontSize(11).TextColor(t.TextMuted).
		Tooltip("音质：" + st.Quality + "（点一下换）").
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

// viewTransport 是上一首、下一首、播放/暂停。
//
// ⚠️ **三个都是只有图标的按钮**（Biu 的 `MusicPlayControl` 就是
// `RiSkipBackFill` / `RiPlayCircleFill` / `RiSkipForwardFill`）。
// 文字版是三个汉字各占 88 宽——中间那一列 276 宽全被这三个键占满，而进度条
// 那一行是从它这儿借的富余（见 viewProgress）。换成图标之后那一列只剩 132，
// 歌名那边宽出来的 144 就是「加得下东西」的那一段。
//
// 无障碍名字一个字没少（「上一首」「播放」「暂停」「下一首」），所以读屏软件
// 念出来和原来一字一样——变的是**画出来的**，不是**念出来的**。
func (s *Shell) viewTransport(c *ui.Context, st app.State) {
	ui.Row(c).Gap(12).AlignItems(ui.Center).Children(func() {
		s.viewStepButtons(c)

		if st.Playing {
			if iconButtonPrimary(c, transportIcon(true), "暂停", iconPlayBtn).
				Size(iconHitPlay, iconHitPlay).Padding(0, 5).
				Tooltip("暂停").Clicked() {
				s.app.Toggle()
			}
		} else {
			if iconButtonPrimary(c, transportIcon(false), "播放", iconPlayBtn).
				Size(iconHitPlay, iconHitPlay).Padding(0, 5).
				Tooltip("播放").Disabled(!st.HasTrack).Clicked() {
				s.app.Toggle()
			}
		}
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
	// （`mygo/ui/input.go:131-145` 的 `appendHitChain`）；而 `flagPassThrough` 只
	// 跳过那一个自己的命中框、子节点照登记（`runtime.go:953`）。于是只要可点的
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

// viewStepButtons 是上一首和下一首。
//
// 能不能按看的是队列：没放过歌、或者只有刚打开的那一首（没进队列）时是灰的。
// 队列里只有一首歌时按钮仍然是亮的——按下去听到的是同一首从头来，
// 表现成按了没反应更让人困惑。
func (s *Shell) viewStepButtons(c *ui.Context) {
	s.viewPrevButton(c)
	s.viewNextButton(c)
}

// viewPrevButton / viewNextButton 是那两首键。分成两个是为了全屏播放器能把它们
// 摆到播放键**两边**（Biu 的 `MusicPlayControl` 就是 上一首 · 播放 · 下一首）。
//
// ⚠️ 原来是 88 宽的三个汉字按钮，而三个汉字在按钮里要 54——**44 就已经放不下**，
// 按 64 画出来是「上...」「下...」，看着像坏了。图标化之后这件事不再存在：
// 18 的图标 + 每边 5 的内边距，点击区 28（`iconHitMd`），比原来那个 88 窄三倍
// 还点得准（角上也点得到，见 `TestTheHitAreaIsBiggerThanTheGlyph`）。
func (s *Shell) viewPrevButton(c *ui.Context) {
	if iconButton(c, iconPrev, "上一首", iconMd).
		Size(iconHitMd, iconHitMd).Padding(0, 5).
		Tooltip("上一首").Disabled(!s.app.CanPrev()).Clicked() {
		s.app.Prev()
	}
}

func (s *Shell) viewNextButton(c *ui.Context) {
	if iconButton(c, iconNext, "下一首", iconMd).
		Size(iconHitMd, iconHitMd).Padding(0, 5).
		Tooltip("下一首").Disabled(!s.app.CanNext()).Clicked() {
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
// 摆在哪儿：进度那一行的最右边。理由（量出来的账）见 viewProgress 里那段。
func (s *Shell) viewPlayMode(c *ui.Context, st app.State) {
	// 四档各配一个图标（Biu 的 `getPlayModeList`，逐个照它的 `icon` 那一项），
	// 悬停那句话仍然报的是**当前这一档**。
	btn := iconButton(c, playModeIcon(st.PlayMode), "播放模式", iconMd).
		Size(iconHitMd, iconHitMd).Padding(0, 5).
		Tooltip(st.PlayMode.Label() + "：" + st.PlayMode.Desc() + "（点一下换，右键看全部）")

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
func (s *Shell) viewProgress(c *ui.Context, st app.State) {
	known := st.Duration > 0

	ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
		ui.Text(c, clock(st.Position)).FontSize(12).TextColor(c.Theme().TextMuted).Width(48)

		sl := ui.Slider(c, &s.scrub, 0, 1).Grow(1).Label("播放进度").Disabled(!known)
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
		ui.Text(c, total).FontSize(12).TextColor(c.Theme().TextMuted).Width(48)

		// 播放模式按钮在这一行的**最右边**，而不是右边那一组里。
		//
		// 这一行是另一个账：它左边的进度条是 `Grow(1)` 的，而中间那一列的宽
		// 是这一行和上面那行（上一首/播放/下一首）里**更宽的那一个**定下来的。
		// 摆在这里是从那一行的富余里出的，不从歌名里出。
		//
		// ⚠️ 它原来是 56 宽的（上面写的是两个汉字），现在换成了 28 宽的图标键
		// ——省下来的 28 又全部回到这一行：这一行 246 宽，而中间那一列整列是
		// 246，所以进度条在 760 宽下只剩 112。`TestThePlayModeButtonDoesNotSquashTheTitle`
		// 和 `TestTheErrorSitsUnderTheTitle` 都在盯「歌名没被挤成竖排」。
		s.viewPlayMode(c, st)
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
