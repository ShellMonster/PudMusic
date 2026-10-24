// 图标资源：界面上那些「只有图标」的按钮、空状态里的插画位从哪儿来。
//
// # 图标集是 RemixIcon，目录结构照它上游
//
// Biu 的界面图标全来自 `@remixicon/react`，所以这一批取的是同一套（Remix Design
// 的 Remix Icon，Apache 之外的自有许可 Remix Icon License v1.0，见 NOTICE）。
// 目录**照上游那样按分类分开**（`icons/Media/`、`Arrows/`、`System/`、`Editor/`、
// `Document/`、`Design/`、`Business/`、`Device/`、`Others/`、`Communication/`、
// `User & Faces/`、`Weather/`），而不是拍平了扔进一个目录：上游那 2892 个图标
// 就是这么分的，拍平之后既认不出某个图标属于哪一类，加图标时也没法照着上游
// 的目录找。**加新图标就照着这个办法往对应的分类目录里放**，名字逐字照上游
// （`gh api repos/Remix-Design/RemixIcon/contents/icons/<分类>/<文件名>`）。
//
// embed 在叶子包 `internal/ui/iconset`（为了给 `widgets/imgcache` 也共用这一份，
// 理由见那边文件头）；这里只管**名字**和**哪个意思配哪一张**。
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
//
// # 「量」这件事的现状，别信注释里的旧数
//
// 视觉调研记的是「图标资源 26 : 105」——上游用了约 105 个 remixicon，我们当时
// 只有 26 个。这一票把资源补到 `iconNames` 那张表（**数表，别数注释**）的规模。
// 表里每一组都标着它是给哪一处用的；界面上还没用上的那一部分留着，是因为
// 「备齐」和「接上去」是两件事，而接上去归各自那张票（播放条控件排列是 #127，
// 顶栏与设置页也是）。
package ui

import (
	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/ShellMonster/Biu_Go/internal/ui/iconset"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/list"
	"github.com/egoist/mygo/ui"
)

// 图标名。逐字照上游的「分类/文件名」（理由见文件头）。
//
// 命名成 Go 常量而不是到处写字符串：拼错一个分类在 map 里查不到时只会画出
// 一格空白，而 `icons_test.go` 那一条会把每个名字都过一次 embed。
const (
	// ── 侧栏菜单 ──
	//
	// 每一项两枚：线框的那枚是平时画的，**选中那一项换成实心的那一枚**
	// （上游 `biu/src/common/constants/menus.tsx` 的 `icon` / `activeIcon`）。
	//
	// ⚠️ **名字逐字照上游的 RemixIcon 文件名**，而上游那几枚是从
	// `@remixicon/react` 引进来的：`RiDiscLine` → `Media/disc-line.svg`、
	// `RiUserFollowLine` → `User & Faces/user-follow-line.svg`、
	// `RiCalendarScheduleLine` → `Business/calendar-schedule-line.svg`、
	// `RiHistoryLine` → `System/history-line.svg`、
	// `RiFolderMusicLine` → `Document/folder-music-line.svg`、
	// `RiFileDownloadLine` → `Document/file-download-line.svg`。
	// 「设置」是我们自己多摆的那一项（上游设置在头像下拉里），配的是设置那一枚。
	iconMenuRecommend   = "Media/disc-line.svg"                 // 推荐音乐
	iconMenuRecommendOn = "Media/disc-fill.svg"                 // 推荐音乐（选中）
	iconMenuFollow      = "User & Faces/user-follow-line.svg"   // 我的关注
	iconMenuFollowOn    = "User & Faces/user-follow-fill.svg"   // 我的关注（选中）
	iconMenuLater       = "Business/calendar-schedule-line.svg" // 稍后再看
	iconMenuLaterOn     = "Business/calendar-schedule-fill.svg" // 稍后再看（选中）
	iconMenuHistory     = "System/history-line.svg"             // 历史记录
	iconMenuHistoryOn   = "System/history-fill.svg"             // 历史记录（选中）
	iconMenuLocal       = "Document/folder-music-line.svg"      // 本地音乐
	iconMenuLocalOn     = "Document/folder-music-fill.svg"      // 本地音乐（选中）
	iconMenuDownload    = "Document/file-download-line.svg"     // 下载记录
	iconMenuDownloadOn  = "Document/file-download-fill.svg"     // 下载记录（选中）
	iconMenuSettings    = "System/settings-3-line.svg"          // 设置
	iconMenuSettingsOn  = "System/settings-3-fill.svg"          // 设置（选中）

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

	// iconPageList 是播放条上那个**分集抽屉**的入口。
	//
	// 逐字照上游：`RiListRadio`（`layout/playbar/left/page-list.tsx:25` 的
	// `<RiListRadio size={18}/>`）。
	//
	// ⚠️ **它和 `iconQueue` 是两张不同的图**（票 #220 / 报告 §3.7）：上游那边
	// 「播放列表」用的是 `RiPlayListLine`（`components/open-playlist-drawer-button/
	// index.tsx:10-13`），和分集这一枚**不是同一个图形**——两个按钮一个开队列、
	// 一个开分集，用同一张图的话用户分不出哪个是哪个。改之前我们两个都用了这张
	// `Editor/list-radio.svg`，那是一个**看得见的错**（不是「静默」的那一种）。
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
	iconSearch   = "System/search-line.svg"        // 搜索
	iconClose    = "System/close-line.svg"         // 清空
	iconFeed     = "User & Faces/team-line.svg"    // 动态

	// iconLogin / iconLogout 是顶栏右端那两枚（登录 / 退出登录）。
	//
	// ⚠️ **这一对换过图形**：原来是 `System/login-circle-line.svg` 和
	// `System/logout-circle-line.svg`——**同一枚圆圈差一个箭头方向**（一个向右、
	// 一个向左），在 18px 上肉眼极难分清（UX 报告 §3-A 的 A1，票 #242）。
	// 顶栏那一枚**没有文字**，于是「我按的是登录还是退出」全靠图形认。
	//
	// 现在这一对是：
	//
	//   - 登录 = `Device/qr-code-line.svg`（二维码）
	//   - 退出 = `System/logout-circle-r-line.svg`（圆圈 + 一支**朝外**的箭头）
	//
	// 为什么不沿用上游那一对「进门 / 出门」：它们都是「门框 + 箭头」，而门框是
	// 同一个矩形，差只在箭头进还是出——18px 上仍然是「一个框里有个箭头」。
	// 二维码是**另一种东西**（密的方块），和圆圈箭头一眼就是两样。
	//
	// 为什么登录用二维码：这一票的登录就是**扫码登录**（`login.go` 那一页），
	// 而中文音乐客户端的登录入口历来是二维码（QQ 音乐 / 网易云 PC 端右上角都是）。
	// 二维码还顺带和顶栏那枚**头像**分开——头像那一格画的是一个人形，而顶栏
	// 右边再摆一枚人形会看着像重复。
	//
	// ⚠️ **这两张图是这一票引进来的**（之前 `iconset` 里没有它们）：
	// `gh api repos/Remix-Design/RemixIcon/contents/icons/Device/qr-code-line.svg`、
	// `gh api repos/Remix-Design/RemixIcon/contents/icons/System/logout-circle-r-line.svg`。
	iconLogin  = "Device/qr-code-line.svg"         // 登录（扫码）
	iconLogout = "System/logout-circle-r-line.svg" // 退出登录

	// iconLoginCircle / iconLogoutCircle 是上游原来那**一对**（圆圈里一支箭头，
	// 一个朝右一个朝左）。界面现在不用它们——理由在上面那一段——但资源留着。
	iconLoginCircle  = "System/login-circle-line.svg"
	iconLogoutCircle = "System/logout-circle-line.svg"

	// ── 播放条右边那一组 ──
	// ⚠️ 「播放列表」原来在顶栏那一列里，票 #127 跟着上游搬到了播放条
	//（`layout/playbar/right/index.tsx:16`），所以它归在这一列而不是顶栏那一列。
	//
	// 图形逐字照上游：`RiPlayListLine`（`components/open-playlist-drawer-button/
	// index.tsx:10-13` 的 `<RiPlayListLine size={18}/>`）——
	// **和分集那一枚（`iconPageList` 的 `RiListRadio`）不是同一张图**（票 #220 /
	// 报告 §3.7）。改之前我们两枚都用 `Editor/list-radio.svg`，于是用户看见的是
	// **两个一模一样的图形**，而它们一个开队列、一个开分集。
	//
	// ⚠️ **这张图是这一票引进来的**（之前 `iconset` 里没有它）：
	// `gh api repos/Remix-Design/RemixIcon/contents/icons/Media/play-list-line.svg`。
	iconQueue = "Media/play-list-line.svg" // 播放列表（抽屉的开关）

	// ── 队列抽屉头 ──
	// 逐字照 Biu 的 `components/music-playlist-drawer/index.tsx:137-141`：
	// `RiFocus3Line`（定位当前播放）/ `RiDeleteBinLine`（清空播放列表）。
	iconLocate = "Design/focus-3-line.svg"    // 定位当前播放
	iconDelete = "System/delete-bin-line.svg" // 清空队列

	// iconPlayFill 是队列行悬停时封面上的那一枚播放三角，逐字照上游
	// `music-playlist-drawer/list-item.tsx` 的 `<RiPlayFill size={20}/>`。
	//
	// ⚠️ **不是播放条那一枚** `play-circle-fill`：那一枚带着一圈圆底，缩到 20
	// 像素再压在 35% 的黑色蒙层上会糊成一团；上游这里用的就是一枚光三角。
	iconPlayFill = "Media/play-fill.svg"

	// ⚠️ **头像那一格故意不在这一列。** #115（真实头像与未登录形态）把它整个
	// 重做成了三档——真图 / 出问题的人形 / 等着的人形——而那个人形是
	// `navbar_avatar.go` 的 `avatarPerson` 按 `avatarSize` 的分数倍画出来的
	// （圆脑袋 + 一块肩膀，两块都要跟着尺寸等比缩放）。
	//
	// 图标化那一票原本在这一格摆一个 `User & Faces/user-line.svg`（就是
	// 「未登录时别显示首字母 B，显示一个人」），**被 #115 取代了**。取 #115 的
	// 理由：它给每一档都配了不同的无障碍名和悬停说明（「这儿没有人」/「正在取」/
	// 「这个账号没有头像」），而那一档一档都要分得开是那张票的验收；一个图标
	// 只给得出一个样子。理由见 `avatarPerson`。
	//
	// ⚠️ 那一票删掉的是**头像那一格用的那一张**，`User & Faces/user-line.svg`
	// 这个**资源**这一票又补回来了——它现在给「UP 主空间」的空状态当插画。
	// 资源在不在和用在哪一处是两件事。

	// ── 空状态那一格插画位 ──────────────────────────────────────────────
	//
	// ⚠️ **上游所有页面的空状态共用同一枚 `RiCloseCircleLine`**（`components/
	// empty/index.tsx:16`，32px）。我们这里给每一页各配一枚，是这一票要的
	// （issue 126：「每个页面的空状态有自己的样子而不是共用一张」）——一屏里同时
	// 只看得到一个空状态，而那一格要能一眼说清「是哪个页面空的」。
	//
	// `iconEmptyDefault` 保留上游那一枚：没有更贴切那一枚的页（`/empty` 落地页
	// 本身）用它，**不是**自己编一枚。
	iconEmptyDefault  = "System/close-circle-line.svg"         // 空状态（上游那张）
	iconEmptyMusic    = "Media/music-2-line.svg"               // 首页推荐：一格一首歌
	iconEmptyGroup    = "User & Faces/group-line.svg"          // 我的关注：一组人
	iconEmptyLater    = "System/time-line.svg"                 // 稍后再看：一个钟
	iconEmptyHistory  = "System/history-line.svg"              // 历史记录：一圈回头
	iconEmptyDisk     = "Device/hard-drive-2-line.svg"         // 本地音乐：硬盘上的
	iconEmptyFolder   = "Document/folder-line.svg"             // 收藏夹：一个空夹子
	iconEmptyUser     = "User & Faces/user-line.svg"           // UP 主空间：一个人
	iconEmptyLyrics   = "Media/mic-line.svg"                   // 歌词：一只麦克风
	iconEmptyLogin    = "User & Faces/account-circle-line.svg" // 没登录：一个头像位
	iconEmptySearch   = "System/search-line.svg"               // 搜索：一只放大镜
	iconEmptyFeed     = "User & Faces/team-line.svg"           // 动态：一群人在说话
	iconEmptyDownload = "System/download-2-fill.svg"           // 下载记录：一只下箭头

	// iconFailed 是「这一块拉失败了」那一支的插画位。
	//
	// 它和上面那一批**分开**：失败不是空（理由写在 `viewLoadError` 顶上），
	// 界面上必须一眼分得出「本来就没有」和「没拉出来」。
	iconFailed = "System/error-warning-line.svg"

	// iconCoverPlaceholder 是封面加载不出来时那一格里的图片图标（40% 大小，
	// 理由见 `widgets/imgcache/view.go`）。
	//
	// ⚠️ 它住在 `widgets/imgcache`——封面的唯一画法在那边，而那个包拿不到这里
	// 的常量（反过来 import 是个环，见 `iconset` 文件头）。所以**这里的名字和
	// 那边引的是同一个文件**，不是两张图。
	iconCoverPlaceholder = "Media/image-line.svg"

	// ── 调研点名「常缺」的那一批（报告 §C12）─────────────────────────────
	//
	// 上游点名的缺口逐条对上：更多、网格 / 列表视图、文件夹+、编辑、删除、日历、
	// 展开 / 全屏、画中画、刷新、外链、眼睛、感叹号、问号、导入 / 导出、用户组、
	// 关注 / 取关、月亮 / 太阳、磁盘。
	iconMore        = "System/more-2-line.svg"              // 每一行的操作（⋮）
	iconMoreFilled  = "System/more-fill.svg"                // 操作（实心那一档）
	iconMoreFilled2 = "System/more-2-fill.svg"              // 操作（实心，另一版）
	iconViewGrid    = "Design/layout-grid-line.svg"         // 网格档
	iconViewList    = "Editor/list-check-2.svg"             // 列表档
	iconGrid        = "Design/grid-line.svg"                // 网格
	iconFolderAdd   = "Document/folder-add-line.svg"        // 新建收藏夹
	iconEdit        = "Design/edit-line.svg"                // 编辑
	iconEditBox     = "Design/edit-box-line.svg"            // 编辑（框起来那一版）
	iconCalendar    = "Business/calendar-line.svg"          // 日期 / 日期范围
	iconExpand      = "Arrows/expand-diagonal-line.svg"     // 放大
	iconShrinkBack  = "Arrows/expand-diagonal-2-line.svg"   // 还原
	iconPiP         = "Media/picture-in-picture-2-line.svg" // 画中画
	iconRefresh     = "System/refresh-line.svg"             // 刷新
	iconExternal    = "Arrows/arrow-right-up-line.svg"      // 外链（在 B 站打开）
	iconEye         = "System/eye-line.svg"                 // 显示
	iconEyeOff      = "System/eye-off-line.svg"             // 隐藏
	iconWarning     = "System/error-warning-line.svg"       // 感叹号 / 警告
	iconQuestion    = "System/question-line.svg"            // 问号（帮助）
	iconImport      = "System/import-line.svg"              // 导入
	iconExport      = "System/export-line.svg"              // 导出
	// iconAdd 既是「关注」也是「新建收藏夹」那枚加号——上游两处用的是同一个
	// `RiAddLine`（`music-list-item` 和 `side/collection`），而 `icons.go` 一个
	// 文件一个名字，所以只留一个。
	iconAdd        = "System/add-line.svg"           // 加（关注 / 新建收藏夹）
	iconFollowDone = "System/check-line.svg"         // 已关注
	iconMoon       = "Weather/moon-line.svg"         // 深色
	iconSun        = "Weather/sun-line.svg"          // 浅色
	iconDisk       = "Device/hard-drive-2-line.svg"  // 磁盘
	iconDeleteBin5 = "System/delete-bin-5-line.svg"  // 删除（另一版）
	iconBookmark   = "Business/bookmark-line.svg"    // 书签
	iconInbox      = "Business/inbox-line.svg"       // 收件箱
	iconChart      = "Business/bar-chart-2-line.svg" // 统计

	// ── 导航、方向、展开收起 ────────────────────────────────────────────
	iconPrevEpisode   = "Arrows/arrow-left-line.svg"       // 上一集
	iconNextEpisode   = "Arrows/arrow-right-line.svg"      // 下一集
	iconGoBack        = "Arrows/arrow-go-back-line.svg"    // 后退
	iconGoForward     = "Arrows/arrow-go-forward-line.svg" // 前进
	iconSideCollapse  = "Arrows/arrow-left-s-line.svg"     // 侧栏收起
	iconSideExpand    = "Arrows/arrow-right-s-line.svg"    // 侧栏展开
	iconChevronDown   = "Arrows/arrow-down-s-line.svg"     // 向下展开
	iconChevronUp     = "Arrows/arrow-up-s-line.svg"       // 向上收起
	iconDropDown      = "Arrows/arrow-drop-down-line.svg"  // 下拉
	iconDropUp        = "Arrows/arrow-drop-up-line.svg"    // 上拉
	iconDownloadArrow = "Arrows/arrow-right-down-line.svg" // 下载（带箭头那一版）
	// ⚠️ 「回到顶部」那一枚的**画法**在 `widgets/list`（那个包在界面层下面），
	// 所以文件名只有一个出处：`list.BackToTopIcon`。这里取它，不是另写一遍。
	iconToTop          = list.BackToTopIcon                // 回到顶部
	iconToTopLeft      = "Arrows/arrow-left-up-line.svg"   // 左上
	iconToBottomLeft   = "Arrows/arrow-left-down-line.svg" // 左下
	iconDrag           = "Arrows/drag-move-2-line.svg"     // 拖动
	iconFullscreenLine = "Media/fullscreen-line.svg"       // 全屏

	// ── 文件、媒体、其余 ────────────────────────────────────────────────
	iconImageAdd      = "Media/image-add-line.svg"              // 添加封面
	iconVideo         = "Media/video-line.svg"                  // 视频
	iconHeadphone     = "Media/headphone-line.svg"              // 耳机
	iconMovie         = "Media/movie-line.svg"                  // 影片
	iconEqualizer     = "Media/equalizer-line.svg"              // 正在放（跳动的波形）
	iconStop          = "Media/stop-circle-line.svg"            // 停止
	iconBell          = "Media/notification-line.svg"           // 通知
	iconFileMusic     = "Document/file-music-line.svg"          // 音乐文件
	iconFileList      = "Document/file-list-2-line.svg"         // 文件清单
	iconFileAdd       = "Document/file-add-line.svg"            // 添加文件
	iconFileSheet     = "Document/file-excel-2-line.svg"        // 清单表
	iconDraft         = "Document/draft-line.svg"               // 草稿
	iconDownloadLine  = "System/download-line.svg"              // 下载（线）
	iconDownloadCloud = "System/download-cloud-line.svg"        // 云端目录
	iconShield        = "System/shield-check-line.svg"          // 校验
	iconChecked       = "System/checkbox-circle-line.svg"       // 已选
	iconUnchecked     = "System/checkbox-blank-circle-line.svg" // 未选
	iconZoomIn        = "System/zoom-in-line.svg"               // 放大
	iconZoomOut       = "System/zoom-out-line.svg"              // 缩小
	iconInfo          = "System/information-line.svg"           // 信息
	iconCloseFilled   = "System/close-circle-fill.svg"          // 关闭（实心）
	iconTip           = "Others/lightbulb-line.svg"             // 提示
	iconUser          = "User & Faces/user-line.svg"            // 用户
	iconUserAdd       = "User & Faces/user-add-line.svg"        // 加关注的人
	iconFeedback      = "Communication/feedback-line.svg"       // 反馈
)

// iconNames 是这一份要备齐的图标。一个常量一个名字，漏一个立刻红
// （`TestEveryIconWeNamedIsThereAndItParses`）。
//
// ⚠️ **数表，别数注释**：视觉调研记的是上游用了「约 105 个 remixicon」，而我们
// 这一票补到与它相当（`TestTheIconSetIsNotSlimmerThanBiu` 钉的就是「不小于 105
// 个**文件**」这一位）。注释里曾经写着 26，那是补之前的数。
var iconNames = []string{
	// 侧栏菜单
	iconMenuRecommend, iconMenuRecommendOn,
	iconMenuFollow, iconMenuFollowOn,
	iconMenuLater, iconMenuLaterOn,
	iconMenuHistory, iconMenuHistoryOn,
	iconMenuLocal, iconMenuLocalOn,
	iconMenuDownload, iconMenuDownloadOn,
	iconMenuSettings, iconMenuSettingsOn,

	// 播放条
	iconPlay, iconPause, iconPrev, iconNext,
	iconStar, iconStarFilled, iconDownload,
	iconVolumeUp, iconVolumeMid, iconVolumeMute,
	iconEnqueue, iconFullscreen, iconLike, iconLikeFilled, iconPageList,
	iconModeOrder, iconModeLoop, iconModeRandom, iconModeOne,
	// 顶栏
	iconSettings, iconFolder, iconQueue,
	iconSearch, iconClose, iconFeed, iconLogin, iconLogout,
	// 上游原来的那一对登录 / 退出（圆圈 + 一个向左、一个向右的箭头）。界面
	// 现在不用它们了（18px 上分不清，票 #242），但**资源留着**：这一份图标
	// 是照上游备齐的，「备齐」和「接上去」是两件事（见文件头）。
	iconLoginCircle, iconLogoutCircle,
	// 队列抽屉头与行
	iconLocate, iconDelete, iconPlayFill,

	// 空状态插画位
	iconEmptyDefault, iconEmptyMusic, iconEmptyGroup, iconEmptyLater,
	iconEmptyHistory, iconEmptyDisk, iconEmptyFolder, iconEmptyUser,
	iconEmptyLyrics, iconEmptyLogin, iconEmptySearch, iconEmptyFeed,
	iconEmptyDownload, iconFailed, iconCoverPlaceholder,

	// 调研点名的缺口
	iconMore, iconMoreFilled, iconMoreFilled2,
	iconViewGrid, iconViewList, iconGrid,
	iconFolderAdd, iconEdit, iconEditBox, iconCalendar,
	iconExpand, iconShrinkBack, iconPiP, iconRefresh, iconExternal,
	iconEye, iconEyeOff, iconWarning, iconQuestion,
	iconImport, iconExport, iconAdd, iconFollowDone,
	iconMoon, iconSun, iconDisk, iconDeleteBin5,
	iconBookmark, iconInbox, iconChart,

	// 导航、方向、展开收起
	iconPrevEpisode, iconNextEpisode, iconGoBack, iconGoForward,
	iconSideCollapse, iconSideExpand, iconChevronDown, iconChevronUp,
	iconDropDown, iconDropUp, iconDownloadArrow,
	iconToTop, iconToTopLeft, iconToBottomLeft, iconDrag, iconFullscreenLine,

	// 文件、媒体、其余
	iconImageAdd, iconVideo, iconHeadphone, iconMovie, iconEqualizer,
	iconStop, iconBell, iconFileMusic, iconFileList, iconFileAdd,
	iconFileSheet, iconDraft, iconDownloadLine, iconDownloadCloud,
	iconShield, iconChecked, iconUnchecked, iconZoomIn, iconZoomOut,
	iconInfo, iconCloseFilled, iconTip, iconUser, iconUserAdd, iconFeedback,
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
//
// 解析和缓存都在 `iconset` 里（那边有 embed，也有给 `widgets/imgcache` 用的一
// 份缓存），这里只是**启动期**把它整批过一遍：解析不了当场炸，而不用等第一次
// 画到那一枚。
func mustParseIcons(names []string) map[string]*ui.SVG {
	m := make(map[string]*ui.SVG, len(names))
	for _, name := range names {
		m[name] = iconset.Must(name)
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
	// iconSm 是**那些不在播放条上**的小图标键的图标边长（下载表的操作列、
	// 本地音乐那两枚、队列抽屉头那两枚、侧栏的「新建收藏夹」）。
	//
	// ⚠️ **播放条上那几枚已经不用这一档了**（票 #219，报告 §3.6）：分集 / 赞 /
	// 收藏搬到了左边第三段，而上游播放条上**全部图标键都是 18**——它们跟着
	// 右边那一组走 `iconMd`（票 #127 把「下载」搬过去的时候就是这一档）。
	iconSm = 14
	// iconMd 是播放条上那些大一点的键（播放模式 / 下载 / 播放列表 / 加歌），
	// 加上顶栏上多数按钮。Biu 那边是 18~22。
	iconMd = 18
	// iconLoginSize 是顶栏那枚**登录**键的图标边长。**16**，不是 iconMd。
	//
	// ⚠️ **那一枚的图形是二维码**（票 #242：这一票的登录是扫码登录，而「圆圈里
	// 一支箭头」那一对在 18px 上分不清进出）。二维码**天生比线框图标密**：
	// 18px 下它那几块方块挨在一起糊成一块（墨迹 191，是顶栏那四枚线框图标
	// 136~141 的 **1.36 倍**——整排最重的一枚）；**16px 下那几块还是分得开的**
	// （墨迹 137，和那四枚**同一档**），而且仍然看得出是个二维码。
	//
	// 票 #241 / 报告 F-16：顶栏末两枚的墨迹要和左边那四枚线框图标**同一档**。
	iconLoginSize = 16

	// iconSkip 是「上一首 / 下一首」那两枚的图标边长。
	//
	// ⚠️ **它不跟 iconMd 走**：上游的 `MusicPlayControl`（`components/music-play-control/
	// index.tsx:16-18`）把跳过那两枚画成 `size={22}`，而中间那枚播放键是 48——
	// 同一排里三枚是三个数。我们原来照 `iconMd`（18）画，于是**全条上最小的两枚
	// 紧挨着最大的那一枚**，中间空着一圈，而用户要找的是中间那一枚。
	//
	// 命中区仍然是 32（`iconHitMd`）——所以是「每边 5 的留白」，不是 7。
	iconSkip = 22
	// iconPlayBtn 是播放/暂停那一枚。Biu 是 48（`music-play-control` 的
	// `<RiPlayCircleFill size={48} />`），我们照抄。
	//
	// ⚠️ **它曾经是 30**，理由写的是「播放条只有 88 高，中间那一列还压着进度条」——
	// 而那个理由是假的：88 高装得下 48（48 + 2 的缝 + 20 的进度行 = 70）。
	// 30 的真正后果是**它成了全条上最小的一枚**，而 18 的上下首和它挤成一小堆，
	// 一眼看过去中间是空的——用户要找的那枚键反而不显眼。
	iconPlayBtn = 48
)

// 点击区比图标大出来的那一圈。**靠内边距，不靠资源**（见文件头）。
//
// ⚠️ 这三个数不是「好看不好看」，是「点得到点不到」：
// `TestTheHitAreaIsBiggerThanTheGlyph` 断的正是这三件事——图形的外接框严格小于
// 按钮框、每边留白至少 3 像素、以及**图形之外的那一圈按得中**（它按的是播放键
// 四条边内两像素处；点得中说明那一圈是真的能按，而不是画着好看）。
const (
	// iconHit 是小图标键（iconSm）的点击区边长：14 的图标 + 每边 9。
	//
	// ⚠️ **32 是上游那一档**（G39）：HeroUI 的图标键（`isIconOnly`）不管
	// `size` 是哪一档，盒子都是 `w-[2rem] h-[2rem]`——**32×32**。
	// 我们原来是 28（图标 + 每边 7），比上游小了一档。
	//
	// ⚠️ **它是全局默认**：播放条、顶栏、队列抽屉头、收藏夹行尾、下载表那一列、
	// 本地音乐工具条、满屏播放器的顶栏……全是从这两个数长出来的。改了它，
	// **每一处图标键都跟着长 4 像素**——所以改之前量过「相邻控件有没有被挤重」
	// （见 `playbar.go` 顶上那段宽度账：那一条还剩 100 多像素的余量）。
	//
	// ⚠️ **播放条上那三枚（分集 / 赞 / 收藏）已经不用这一档**（票 #219）：
	// 它们的图标是 18（`iconMd`），而点击区**仍然是 32**——所以它们走的是
	// `iconHitMd`（那个数也是 32，只是内边距从 9 变成 7）。
	iconHit = 32
	// iconHitMd 是中等图标键（iconMd）的点击区边长：18 + 每边 7。
	//
	// ⚠️ **它和 iconHit 是同一个数**（上游那一档不分大小：图标键的盒子恒是
	// 32×32），而图标本身是 18——所以那圈留白是每边 7。
	iconHitMd = 32
	// iconHitPlay 是播放/暂停那一枚的**盒子**：48×48。
	//
	// ⚠️ **它跟图标一样大**（`iconPlayBtn` 也是 48）：上游那一枚是
	// `className="size-12 min-w-12"` 的空按钮，里面画一枚 `size={48}` 的图形——
	// **图形正好填满盒子**（`music-play-control/index.tsx:23-25`）。上游**没有**
	// 那 5 像素留白，我们原来有（图标 48 + 每边 5 = 58），于是那一枚比上游大了一圈。
	//
	// ⚠️ **别又给它加回留白**：留白不是「好按」，是「图形没填满盒子」——
	// 上游那一枚的图形自己就带着 4 像素（圆占画布 20/24），那 4 像素才是留白。
	// 而 48×48 这一格本来就是全条上最大的一枚命中区（别的是 32）。
	iconHitPlay = 48
	// loadingLineSpinnerSize 是播放条上「正在取流」那一行里那枚转圈的直径。
	//
	// ⚠️ **它是 12，不是页面级那一枚的 32**（`loading.go` 的
	// `loadingSpinnerSize`）：那一行是 12 号字、只有 14 高，一枚 32 的转圈
	// 会比那一行还高，把上下两行挤开。
	loadingLineSpinnerSize = 12
)

// iconButton 造一个**只画一个图标**的扁平按钮。
//
// name 是图标名，label 是无障碍名字，font 是图标边长。点击区由调用方的
// `Padding` / `Size` 撑——这里给的是「一个画着图标的按钮」，不是「多大一个」。
//
// ⚠️ **它是扁平的**（无描边、无常驻底色，见 flatbutton.go）：顶栏那几枚、播放条
// 上那一排、队列抽屉头那两枚都是它，而调研点名的那圈 1px 描边正是 `ui.Button`
// 的默认样式给的（报告 §1.1 的 F1）。这一改之后**所有图标键一处都不漏**——
// 它们全从这里出。
//
// 🔴 **label 不许省**。图标对辅助技术是不可见的（`mygo/ui/svg.go` 的 Icon 文档：
// "Icons are decorations that assistive technology does not see, unless Label
// names them"），而这个按钮**再没有字可念**了——用户从一个只有图标的按钮面前
// 什么也听不到。同时它也是无头 Tester 认得这个按钮的唯一凭据。
func iconButton(c *ui.Context, name, label string, font float32) ui.Element {
	return flatIconButton(c, name, label, font)
}

// iconKey 造一枚**图标键**：只画一个图标的扁平按钮，**悬停时图形变主色**。
//
// 上游 `src/components/icon-button/index.tsx:16`：
// `className={twMerge("hover:text-primary text-inherit", className)}` —— 悬停时
// 图形**从前景色变成主色**，不只是底下多一层淡底（报告 §3.5）。
//
// ⚠️ **它和 `iconButton` 是两件事**：上游那个 `IconButton` 是**共享组件**，而我们
// 把「底是应用底色」和「底是压暗过的封面」分成两种情况（见下面那段），所以
// **按调用点挑**，不是全局的。播放条上那些图标键走它，满屏播放器那一行不走。
//
// ⚠️ **禁用的状态必须从这里给**（`disabled` 那一栏），不能靠调用方链
// `.Disabled(...)`：悬停是**建元素的时候**问的（`Hovered()` 在禁用的键上恒为
// false），而链式调用是在返回**之后**才把禁用设上去——问早了，一枚**禁用**的键
// 会用主色告诉用户「这里点得动」（票 #217 的评审查出来过同款的一条）。
//
// 理由、颜色那一档怎么取，都在 `widgets/flat` 的 `IconButtonAccent` 上面写着。
func iconKey(c *ui.Context, name, label string, font float32, disabled bool) ui.Element {
	return flatIconButtonAccent(c, name, label, font, disabled)
}

// iconButtonPlay 是播放/暂停那一枚：**上游那一枚**。
//
// 上游（`components/music-play-control/index.tsx:23-25`）：
//
//	<IconButton isDisabled={…} radius="full" onPress={togglePlay} className="size-12 min-w-12">
//	  {isPlaying ? <RiPauseCircleFill size={48}/> : <RiPlayCircleFill size={48}/>}
//	</IconButton>
//
// 而那个 `IconButton`（`src/components/icon-button/index.tsx:11-19`）是
// `Button isIconOnly radius="md" size="sm" variant="light" className="hover:text-primary text-inherit"`
// ——**无底色、无描边，图形用前景色画**，悬停时变主色。
//
// 所以这一枚是四件事：
//
//  1. **没有实心底色**。原来是 `ui.PrimaryButton`：一块 58×58 的**主色圆角方块**
//     （主题圆角 8，不是药丸），里面一枚**深色**的圆——**明暗正好和上游反过来**，
//     一眼就看不出是同一个东西。
//  2. **图形是前景色**（深色下白、浅色下近黑），不是主色。
//  3. **盒子 48×48，图形正好填满**（`iconHitPlay`）。
//  4. **悬停时图形变主色**（上游的 `hover:text-primary`）。
//
// ⚠️ **主色当字色那一档取 `ink.AccentOn`**：浅色下主色是 #1ed760，压在白底上
// 只有 1.9:1（`widgets/ink` 的文件头写着这件事），所以取那个「读得出来」的变体；
// 深色下就是主色本身。理由和 `flatbutton.go` 的 `flatCurrent` 是同一条：
// **主色当底和主色当字是两件事**。
//
// 🔴 **这一档只给「底是应用底色」的地方用**：满屏播放器那一枚的底是**压暗过的
// 封面**，不是应用底色——主色当字色那一档压在那上面只有 1.0:1（实测），而且
// 那一行其余四枚图标都是满屏自己的前景色，一枚变绿另外四枚不变，一眼就不成套。
// 所以满屏那一枚走 `iconButton`（见 `fullplayer.go` 的 `viewFullControls`）。
//
// ⚠️ **别把它改回实心主色**：那一版（「实心主色是强调色，主操作的重量必须
// 留着」）是照着**旧形状**写的（我们是一块圆底，上游是一枚圆图形），而上游
// 那一枚改过来之后，这条理由自己就不成立了（上游那一枚同样是强调色的播放键，
// 只是它靠的是 `RiPlayCircleFill` 那张实心图形，而不是一块底）。
//
// ⚠️ 顶栏那个**登录**键原来也有一份实心主色的按钮（`iconButtonPrimary`），
// 票 #241 把它去掉了——**那个函数已经删了**，别再加回来：顶栏那一排的视觉
// 重量由图标说话，理由见 `login.go` 的 `viewLoginEntry`。
// **现在整条顶栏没有一枚实心底色的方块**——这一枚也不能例外。
//
// 判据在 `playbar_layout_test.go` 的 `TestThePlayKeyIsBigEnoughToHit`：
// 那一枚现在量的是「播放条底色上那一片墨迹」，不再是「主色底上那一片墨迹」。
func iconButtonPlay(c *ui.Context, name, label string, font float32, disabled bool) ui.Element {
	// ⚠️ **它就是 `iconKey`**：悬停变主色、禁用的状态由这里给，两条都在那边写着。
	// 写成两个是因为这一枚的**形状**（48×48、图形正好填满、底下什么都没有）是
	// 它自己的事，而「悬停变主色」是所有图标键共有的那一条。
	//
	// ⚠️ **禁不启用由调用方给**（`disabled` 那一栏）：那是**状态**决定的（没歌的
	// 时候那一枚是禁用的），而它必须在**问悬停之前**就定下来——问早了，一枚禁用
	// 的键会用主色告诉用户「这里点得动」（见 `iconKey` 那段）。
	return iconKey(c, name, label, font, disabled)
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
