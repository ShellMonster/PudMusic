// 下载记录页那一排「全部 / 音频 / 视频」。
//
// # 为什么它得在这一页，而不是每一行上多一列
//
// 音频和视频**本来就是同一张表**（它们是两个下载任务，`Task.Video` 区别，
// 见 `download.Snapshot.Video`），而下了一堆 MV 之后，用户要在一列表的歌里翻
// 才找得到那一个视频。这一排是按**整份队列**筛的，筛完表格里剩下的每一行都是
// 那一类——用户看的是「我现在在看视频」，不是「这一行是视频」。
//
// # 纯前端，一句请求都不发
//
// 数据早就在内存里了（`app.DownloadSnapshots` 就是队列本身的一份拷贝，见
// `downloadsPage.rowsFn`）。这一档换一次是切一个切片，而切片是页面上的东西、
// 队列里的东西一个都不动——所以切档既不花网络，也不改用户已经排好的那一批。
//
// `TestChangingTheFilterSendsNoRequest` 钉的就是这一条，判据是**这一次点击之后
// 那台记请求的 http.Client 上仍然是零**。

package ui

import (
	"github.com/egoist/mygo/ui"
)

// downloadFilter 是那一排的档。
//
// 用整数而不是字符串，因为 `ui.Radio` 收的就是一个可比较的值（这一排是三档
// 互斥的单选）；而 `label()` 把下标说成人话，免得界面上直接写「你在看第 2 档」。
type downloadFilter int

const (
	// dlFilterAll 是「全部」：不筛。
	dlFilterAll downloadFilter = iota
	// dlFilterAudio 是「音频」：只留歌。
	dlFilterAudio
	// dlFilterVideo 是「视频」：只留 MV。
	dlFilterVideo
)

// dlFilterCount 是有几档。标签表和「下标越界」的兜底都用它，不是两处各写一个数。
const dlFilterCount = 3

// dlFilterLabels 是那几档的名字，从左到右。
//
// ⚠️ **默认是「全部」而不是「音频」**：升级上来的用户第一次打开这一页，
// 看到的是他原来看到的那一列表（队列里有什么就显示什么）。默认成「音频」的
// 话，他下过的那些 MV 会**凭空消失**，而界面上没有任何地方说它们去了哪。
var dlFilterLabels = [dlFilterCount]string{"全部", "音频", "视频"}

// label 是这一档的名字。认不出的档位说「全部」——和筛选的行为一样（认不出就
// 不筛），所以一个坏值不会在界面上留下一句「你在看第 7 档」。
func (f downloadFilter) label() string {
	if f < 0 || int(f) >= len(dlFilterLabels) {
		return dlFilterLabels[dlFilterAll]
	}
	return dlFilterLabels[f]
}

// dlEmptyAfterFilter 是「筛完了，这一档什么都没有」时说的那两句话。
//
// ⚠️ **它和 `noDownloads` 必须是两句不同的话**，而判据是「用户分得清这两种空」：
// 前者回答「我的歌去哪了」（所以要点名他在看哪一档），后者回答「怎么才能有」
// （所以要说点下载）。混成一句的话，筛到空的用户会以为自己白下了几十首。
//
// 这是一个函数而不是 `emptyState` 里的一段，是为了让断言能问**它**
// （`TestTheEmptyCopyAfterFilteringIsNotTheCopyForNoDownloads`）——
// 抄一份文案进测试的话，改了文案那条就红，而文案不是这一条要验的东西。
func dlEmptyAfterFilter(f downloadFilter) emptyState {
	return emptyState{
		Title: "这一档没有记录",
		Hint:  "你在看「" + f.label() + "」这一档，换一档看看",
		Icon:  iconEmptyDownload,
	}
}

// viewFilter 画那一排单选。
//
// 上游是 HeroUI 的 `RadioGroup orientation="horizontal"`（`download-list/index.tsx:110-120`），
// 三个 `Radio` 之间 `gap-4`（16）。**不是 `Segmented`**：那个控件的形状是
// 「一条灰底轨道 + 一块白色滑块」，说的是「切视图」，而这一排是「留哪一类」。
//
// ⚠️ **它在卡片里面**（上游那一排是 `Table` 的 `topContent`，而那张 `Table` 在
// `Card` 里）：原来它摆在卡片**外面**，卡片的边线和它那一排是分开的两样东西。
// 摆法见 `downloadlist.go` 的 `viewTable`。
//
// ⚠️ **不塞进页头那一行**：那一行在窗口最窄那一档（900 减掉侧栏和
// 页内边距约 660）已经满了——标题 + 路径 + 「打开文件夹」+「清空记录」固定占
// 掉约 302，剩下的全给路径（见 `viewToolbar` 的注释）。再挤进一个三档的控件，
// 就只能去吃路径，而路径是那一行里唯一在回答「歌下到哪儿了」的那一段。
// 拆两行是唯一在**每一个**宽度上都对的排法，代价是竖着多一行的位置——和
// `libraryPage.viewFilters` 把搜索框拆成第二行是同一个取舍、同一个理由。
//
// ⚠️ 控件写的是 `sel`，画完之后**无条件**抄回来：MyGo 是即时模式，点中一个
// 单选时框架在处理输入那一刻就把新值写进 `sel` 了，而下一帧再画一遍时读的
// 就是这个值。判 `Changed()` 也行，无条件抄更耐得住框架改这两者的先后。
func (d *downloadsPage) viewFilter(c *ui.Context) {
	sel := int(d.filter)
	// `RadioGroup` 默认是一列（`coreRadioGroup` 建的是 Column），`.Row()` 把它
	// 改成横排；`Gap(16)` 是上游的 `wrapper: "gap-4"`。
	//
	// ⚠️ `Label("筛选")` 给的是**无障碍名字**，不是画在屏幕上的一行字：它让读屏
	// 软件念出「筛选」而不是三个孤零零的词，也顺手让 `Tester.Find("筛选")`
	// 能认出这是那一个控件。它不占位置。
	//
	// ⚠️ **Key 挂在外面那个 Row 上，不能挂在 RadioGroup 上**：后者建的时候已经
	// 装过状态（焦点组），再挂 Key 框架会直接 panic（「whose state was
	// initialized: use Context.Key before construction」）。
	ui.Row(c).Key("downloads:filter").FillWidth().AlignItems(ui.Center).Children(func() {
		ui.RadioGroup(c, func() {
			for i, label := range dlFilterLabels {
				ui.Radio(c, &sel, i, label)
			}
		}).Row().Gap(c.Theme().Space(4)).Label("筛选")
	})
	d.filter = downloadFilter(sel)
}
