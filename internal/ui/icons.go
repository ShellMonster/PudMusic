// 图标资源：播放条和顶栏上那些「只有图标」的按钮从哪儿来。
//
// # 图标集是 RemixIcon，目录结构照它上游
//
// Biu 的界面图标全来自 `@remixicon/react`，所以这一批取的是同一套（Remix Design
// 的 Remix Icon，Apache 之外的自有许可 Remix Icon License v1.0，见 NOTICE）。
// 目录**照上游那样按分类分开**（`Media/`、`Arrows/`、`System/`、`Editor/`、
// `Document/`、`User & Faces/`），而不是拍平了扔进一个目录：上游那 2800 个图标
// 就是这么分的，拍平之后既认不出某个图标属于哪一类，加图标时也没法照着上游
// 的目录找。**加新图标就照着这个办法往对应的分类目录里放**，名字逐字照上游
// （`gh api repos/Remix-Design/RemixIcon/contents/icons/<分类>/<文件名>`）。
//
// # 图标名 = 上游的「分类/文件名」，不是随手起的别名
//
// 这样任何一个图标都能反查回上游哪一个：拿到名字，去上游那个路径取，
// 得到的是同一个文件。同一个界面同一个意思用同一个图标，也不用另外记。
//
// # 资源**不画透明边**：点击区靠控件的 padding 撑
//
// 若在 SVG 里画一圈透明边把图形缩小，点击区就变成「资源里写死的一个数」：
// 换图标、换尺寸、换主题时它跟着错位，而且**界面上看不出坏了**（点得到，
// 只是热区歪了）。所以这里的 SVG 是上游原样，圈是控件的内边距（见 iconButton）。
//
// # 颜色一个字都不定：图标跟主题走
//
// `ui.Icon` 按**祖先的文字色**画（`mygo/ui/svg.go` 的 drawIcon 收的是文字色），
// 而 MyGo 的按钮给的是 `t.Text`（深色模式下是亮色）。于是深浅两套主题下
// 图标各自是「在这套背景上看得清」的那个色，**不给它定死颜色**是既定行为：
// 逐个定死的话，换主题色那一处就得再改一遍这里。
package ui

import (
	"embed"
	"fmt"
	"path"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/egoist/mygo/ui"
)

//go:embed icons
var iconFiles embed.FS

// 图标名。逐字照上游的「分类/文件名」（理由见文件头）。
//
// 命名成 Go 常量而不是到处写字符串：拼错一个分类在 map 里查不到时只会画出
// 一格空白，而 `icons_test.go` 那一条会把每个名字都过一次 embed。
const (
	// ── 播放条 ──
	iconPlay       = "Media/play-circle-fill.svg"   // 播放
	iconPause      = "Media/pause-circle-fill.svg"  // 暂停
	iconPrev       = "Media/skip-back-fill.svg"     // 上一首
	iconNext       = "Media/skip-forward-fill.svg"  // 下一首
	iconStar       = "System/star-line.svg"         // 收藏（还没收藏）
	iconStarFilled = "System/star-fill.svg"         // 收藏（已经收藏）
	iconDownload   = "System/download-2-fill.svg"   // 下载
	iconVolumeUp   = "Media/volume-up-line.svg"     // 音量：响
	iconVolumeMid  = "Media/volume-down-line.svg"   // 音量：小
	iconVolumeMute = "Media/volume-mute-line.svg"   // 音量：静音
	iconEnqueue    = "Media/play-list-add-line.svg" // 加歌
	iconFullscreen = "Arrows/arrow-up-s-line.svg"   // 封面上的「放大」
	iconLike       = "System/thumb-up-line.svg"     // 赞（还没赞）
	iconLikeFilled = "System/thumb-up-fill.svg"     // 赞（已经赞了）

	// iconPageList 是播放条上那个分集抽屉的入口。
	//
	// ⚠️ **它和 `iconQueue` 是同一张图，这不是笔误**：Biu 那边播放列表的开关
	// （`layout/playbar/right/index.tsx` 的 `OpenPlaylistDrawerButton`）和分集的
	// 入口（`layout/playbar/left/page-list.tsx:25`）用的都是 `<RiListRadio size={18}/>`。
	// 两个按钮不是一回事（一个开队列、一个开分集），所以这里给**两个名字**——
	// 名字得跟着那个按钮的用处走，`iconQueue` 的注释才不至于替分集按钮说它不是队列。
	iconPageList = "Editor/list-radio.svg"

	// ── 播放模式四档 ──
	// 逐个照 Biu 的 `getPlayModeList`（`src/common/constants/audio.tsx:27-48`）：
	// 顺序 order-play、列表循环 repeat-2、随机 shuffle、单曲 repeat-one。
	iconModeOrder  = "Media/order-play-line.svg"
	iconModeLoop   = "Media/repeat-2-line.svg"
	iconModeRandom = "Media/shuffle-line.svg"
	iconModeOne    = "Media/repeat-one-line.svg"

	// ── 顶栏 ──
	// ⚠️ 后退 / 前进 **故意不在这一列**：那两个是框架的 `ui.BackButton` /
	// `ui.ForwardButton`，它带着右键的「去过那些页」列表，而要读历史得用
	// `Router` 的私有字段（见 navbar.go 的 viewNavBack）。
	iconSettings = "System/settings-3-line.svg"    // 设置
	iconFolder   = "Document/folder-open-line.svg" // 打开文件
	iconQueue    = "Editor/list-radio.svg"         // 播放列表
	iconSearch   = "System/search-line.svg"        // 搜索
	iconClose    = "System/close-line.svg"         // 清空
	iconFeed     = "User & Faces/team-line.svg"    // 动态
	iconLogin    = "System/login-circle-line.svg"  // 登录
	iconLogout   = "System/logout-circle-line.svg" // 退出登录

	// ⚠️ **头像那一格故意不在这一列。** #115（真实头像与未登录形态）把它整个
	// 重做成了三档——真图 / 出问题的人形 / 等着的人形——而那个人形是
	// `navbar_avatar.go` 的 `avatarPerson` 按 `avatarSize` 的分数倍画出来的
	// （圆脑袋 + 一块肩膀，两块都要跟着尺寸等比缩放）。
	//
	// 图标化那一票原本在这一格摆一个 `User & Faces/user-line.svg`（就是
	// 「未登录时别显示首字母 B，显示一个人」），**被 #115 取代了**，那个资源
	// 随之删掉。取 #115 的理由：它给每一档都配了不同的无障碍名和悬停说明
	// （「这儿没有人」/「正在取」/「这个账号没有头像」），而那一档一档都要分得
	// 开是那张票的验收；一个图标只给得出一个样子。理由见 `avatarPerson`。
)

// iconNames 是这一份要备齐的图标。一个常量一个名字，漏一个立刻红
// （`TestEveryIconWeNamedIsThereAndItParses`）。
var iconNames = []string{
	iconPlay, iconPause, iconPrev, iconNext,
	iconStar, iconStarFilled, iconDownload,
	iconVolumeUp, iconVolumeMid, iconVolumeMute,
	iconEnqueue, iconFullscreen, iconLike, iconLikeFilled, iconPageList,
	iconModeOrder, iconModeLoop, iconModeRandom, iconModeOne,
	iconSettings, iconFolder, iconQueue,
	iconSearch, iconClose, iconFeed, iconLogin, iconLogout,
}

// icons 是上面那批图标**启动时一次解析好**的结果。
//
// 为什么不是 view 里现读现解析：解析一个 SVG 要走一遍 XML 和路径，而
// `ui.Icon` 拿的是一个 `*SVG`（`mygo/ui/svg.go` 的 `coreIcon` 把它存进元素）——
// 每一帧重新解析不但白烧 CPU，还会让 `Icon` 那个元素每帧换一个 doc。
//
// 为什么解析不了要 panic 而不是画空白：这些是**随程序编进去的文件**，不是用户
// 装进来的一份资源——它在开发机上就坏掉的唯一原因是「有人在重构里改了名字」，
// 而那种坏在第一次启动就该炸出来，而不是让用户看到一片空白的按钮。
var icons = mustParseIcons(iconNames)

// mustParseIcons 把一份图标名解析成 `*ui.SVG`，成一个也解析不了就当场 panic。
func mustParseIcons(names []string) map[string]*ui.SVG {
	m := make(map[string]*ui.SVG, len(names))
	for _, name := range names {
		b, err := iconFiles.ReadFile(path.Join("icons", name))
		if err != nil {
			panic(fmt.Sprintf("ui: 图标 %q 读不到（icons/%s）：%v", name, name, err))
		}
		s, err := ui.ParseSVG(b)
		if err != nil {
			panic(fmt.Sprintf("ui: 图标 %q 解析不了（icons/%s）：%v", name, name, err))
		}
		// 解析成功但没有尺寸的话画出来是一格空白，而那和「没画出来」在画面上
		// 长得一模一样——趁还在启动期就拒了。
		if w, h := s.Size(); w <= 0 || h <= 0 {
			panic(fmt.Sprintf("ui: 图标 %q 没有尺寸（icons/%s 是不是空的）", name, name))
		}
		m[name] = s
	}
	return m
}

// icon 按名字取一个图标。名字写错返回 nil，而 `ui.Icon(nil)` 画不出东西——
// 用 `icons_test.go` 那一条把所有名字钉住，别靠运行时的空白发现。
func icon(name string) *ui.SVG { return icons[name] }

// 图标边长。这些是**图标本身**的大小，点击区另算（见 iconButton 的内边距）。
//
// ⚠️ **不许在资源里画透明边来撑点击区**（issue 93 的约束）：换一个尺寸就错位，
// 而且界面上看不出坏了。所以这三个数是「画出来多大」，不是「点得到多大」。
const (
	// iconSm 是播放条第二行那三枚小键（赞 / 收藏 / 下载）的图标边长。
	// Biu 的 `music-thumb` 用的是 18，我们那三枚挤在 88 高播放条的第二行，
	// 留给它们 14 才是完整的一圈（28 的点击区里每边 7 的空白）。
	iconSm = 14
	// iconMd 是上一首 / 下一首 / 播放模式 / 加歌这四枚，加上顶栏上多数按钮。
	// Biu 那边是 18~22。
	iconMd = 18
	// iconPlayBtn 是播放/暂停那一枚。Biu 是 48，而这里的播放条只有 88 高，
	// 中间那一列还压着进度条；48 会把那一列撑到别处借宽度（见 playbar.go 的账）。
	iconPlayBtn = 30
)

// 点击区比图标大出来的那一圈。**靠内边距，不靠资源**（见文件头）。
//
// ⚠️ 这三个数不是「好看不好看」，是「点得到点不到」：
// `TestTheHitAreaIsBiggerThanTheGlyph` 断的正是这三件事——图形的外接框严格小于
// 按钮框、每边留白至少 3 像素、以及**图形之外的那一圈按得中**（它按的是播放键
// 四条边内两像素处；点得中说明那一圈是真的能按，而不是画着好看）。
const (
	// iconHit 是小图标键（iconSm）的点击区边长：14 的图标 + 每边 7。
	iconHit = 28
	// iconHitMd 是中等图标键（iconMd）的点击区边长：18 + 每边 5。
	iconHitMd = 28
	// iconHitPlay 是播放/暂停那一枚：30 的图标 + 每边 5。
	iconHitPlay = 40
)

// iconButton 造一个**只画一个图标**的普通按钮。
//
// name 是图标名，label 是无障碍名字，font 是图标边长。点击区由调用方的
// `Padding` / `Size` 撑——这里给的是「一个画着图标的按钮」，不是「多大一个」。
//
// 🔴 **label 不许省**。图标对辅助技术是不可见的（`mygo/ui/svg.go` 的 Icon 文档：
// "Icons are decorations that assistive technology does not see, unless Label
// names them"），而这个按钮**再没有字可念**了——用户从一个只有图标的按钮面前
// 什么也听不到。同时它也是无头 Tester 认得这个按钮的唯一凭据。
func iconButton(c *ui.Context, name, label string, font float32) ui.Element {
	btn := ui.Button(c, "").Label(label)
	btn.Children(func() { ui.Icon(c, icon(name)).FontSize(font) })
	return btn
}

// iconButtonPrimary 是播放/暂停那一枚：主色按钮。
//
// 和 iconButton 只差一个「底色是主色」——两者的形状、图标、点击区完全一样，
// 写成两个是为了不去改 MyGo 建出来的按钮样式（`ui.PrimaryButton` 是建的时候
// 选的，建完再改底色要连 hover / pressed 两个色一起改）。
func iconButtonPrimary(c *ui.Context, name, label string, font float32) ui.Element {
	btn := ui.PrimaryButton(c, "").Label(label)
	btn.Children(func() { ui.Icon(c, icon(name)).FontSize(font) })
	return btn
}

// 下面这四个是「哪个状态配哪一张」的唯一出口。
//
// ## 为什么不让调用点自己 `if`
//
// 评审踩到过一个真盲区：这些图标最初的测试断的是「几张图形两两不同」，于是
// **把空心和实心对调**之后全部照样绿——它们确实还是两两不同，而用户看到的是
// 画到了反的那一张（未赞时显示成灰色实心拇指，和 Biu 相反）。
//
// 「两两不同」断的是**忘了换**，「对调之后照样不同」断不了的是**换反了**。所以
// 现在每一档都收敛到一个函数：调用点不再有第二个决定它画哪张图的地方，而
// `icons_test.go` 用一张**抄自 Biu 源码**的表逐项钉住这几个函数（那张表里写的是
// 上游的字面文件名，不是这里的常量——改常量而不改那张表，立刻红）。

// likeIcon 是「赞没赞」该配哪一张。
//
// Biu 的 `components/music-thumb/index.tsx`：`isThumb ? <RiThumbUpFill …/> :
// <RiThumbUpLine …/>` —— 空心是还没赞，实心是赞过了。
func likeIcon(liked bool) string {
	if liked {
		return iconLikeFilled
	}
	return iconLike
}

// favIcon 是「收藏没收藏」该配哪一张。Biu 的 `music-fav-button` 同一种做法。
func favIcon(faved bool) string {
	if faved {
		return iconStarFilled
	}
	return iconStar
}

// transportIcon 是「在放不在放」该配哪一张。Biu 的 `music-play-control/index.tsx`：
// `isPlaying ? <RiPauseCircleFill/> : <RiPlayCircleFill/>`。
func transportIcon(playing bool) string {
	if playing {
		return iconPause
	}
	return iconPlay
}

// transportLabel 是播放/暂停那一枚的无障碍名字。
func transportLabel(playing bool) string {
	if playing {
		return "暂停"
	}
	return "播放"
}

// playModeIcon 是某一档播放模式该画哪个图标。
//
// 逐个照 Biu 的 `getPlayModeList`（`src/common/constants/audio.tsx:27-48`）：
// 顺序 order-play、列表循环 repeat-2、随机 shuffle、单曲 repeat-one。
//
// 认不出来的档位返回空串——那是 `app` 加了新档而这里没跟上，摆在按钮上的会
// 是一格空白而不是别的意思。
func playModeIcon(m app.PlayMode) string {
	switch m {
	case app.PlayModeSequence:
		return iconModeOrder
	case app.PlayModeLoop:
		return iconModeLoop
	case app.PlayModeRandom:
		return iconModeRandom
	case app.PlayModeSingle:
		return iconModeOne
	}
	return ""
}

// volumeIcon 是音量到哪一档该画哪个图标。
//
// 三档和阈值逐字照 Biu 的 `music-volume/index.tsx:159-164`：`volume > 0.5`
// 是「响」，以下是「小」，0 是「静音」。抄它的阈值而不是自己分档，是因为
// 用户在 Biu 那边习惯的是那道分界。
func volumeIcon(v float64) string {
	switch {
	case v <= 0:
		return iconVolumeMute
	case v > 0.5:
		return iconVolumeUp
	default:
		return iconVolumeMid
	}
}
