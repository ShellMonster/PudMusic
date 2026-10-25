package ui

// 这一族盯的是**侧栏里那两组收藏夹的样式**（票 #371）。
//
// 这一片是 2026-10-24 那次界面重设计（PR #357）之后**没跟上的一处**：重设计改
// 了侧栏本身（展开 220 / 折叠 64、底色用方案的 `sidebar`、选中项底是
// `accentSoft` 淡底），而这一片还是老样子——它跟同一根侧栏里的菜单项放在一起
// 是两套风格。
//
// # 判据是「和同一根侧栏里的参照物对齐」，不是「某个具体的数」
//
// 这一片要跟的是侧栏那几样东西：分组标题（「发现」「我的音乐」）、它们的左沿、
// 菜单项的选中态。拿它们当参照物，而不是写死像素值——方案换了、主题的间距变
// 了，那些数会跟着变，而「是不是一套」这件事不会变。
//
// ⚠️ **一个字都不许打真 B 站接口**：全部是内存夹具 + 无头渲染。

import (
	"image"
	"image/color"
	"math"
	"testing"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/ink"
	"github.com/egoist/mygo/ui"
	mygoui "github.com/egoist/mygo/ui"
)

// TestTheFavoritesAreaLinesUpWithTheSidebar 断这一片**和侧栏是同一块**。
//
// 三件事，缺一件这一片就散架：
//
//  1. **左沿一样**：这一片的折叠开关起在 x=16，和侧栏的分组标题（「发现」）、
//     品牌 logo 同一列——它缩进到别的地方，就像是另外贴上去的一件东西。
//  2. **封面和分组标题的字同列**：分组标题的字、下面那些**封面**都在 x=36
//     （标题左边那枚箭头图标占了 20）。参考图上「创建的歌单」那几个字和下面
//     那些封面就是左对齐的。
//  3. **收藏夹名的字让开了封面**：它起在 `favNamePad`（36 + 32 + 10 = 78），
//     不再和分组标题的字同列——那一行左边多了一枚方形封面（票 #270）。
//
// ⚠️ **第 2 条和第 3 条是一对**：封面和标题字同列、名字在封面的右边，两处
// 都对的时候这一片才是「标题 + 它下面缩进一层的东西」；只对一处的话，
// 要么是封面飘在中间，要么是名字被封面压住。
func TestTheFavoritesAreaLinesUpWithTheSidebar(t *testing.T) {
	_, _, tt, _ := favShell(t, 2, 0)

	// 侧栏自己的分组标题——这一片要和它同一列。
	side, ok := tt.Find("发现")
	if !ok {
		t.Fatalf("侧栏上没有「发现」那一组，文字有 %q", tt.Texts())
	}
	// 这一片的折叠开关。⚠️ 找的是**按钮**（它的无障碍名字是「折叠 我创建的」），
	// 所以量到的是这一行块的左沿——也就是那枚箭头图标的位置。
	fold, ok := tt.Find("折叠 我创建的")
	if !ok {
		t.Fatalf("侧栏上没有「我创建的」那个折叠开关，文字有 %q", tt.Texts())
	}
	title, ok := tt.Find("我创建的")
	if !ok {
		t.Fatalf("侧栏上没有「我创建的」那一组，文字有 %q", tt.Texts())
	}
	row, ok := tt.Find("我建的 1")
	if !ok {
		t.Fatalf("侧栏上没有「我建的 1」那一行，文字有 %q", tt.Texts())
	}

	if fold.X != side.X {
		t.Errorf("这一片的左沿在 x=%.1f，而侧栏的分组标题在 x=%.1f——它们不在一条竖线上",
			fold.X, side.X)
	}
	// ⚠️ **收藏夹名的位置要用「画面上的墨」量**，不能用 `Find` 给的 rect：
	// MyGo 把列表行里的文字按 inline 段落排版，而那个段落节点的 rect 是**行盒**
	// 的左沿（不是文字的）——量出来会差 20 个像素，而那 20 个正是这一片要钉的
	// 东西（`inkLeft` 的注释写着这件事）。
	img := tt.Image()
	titleLeft := inkLeft(img, title, int(title.X))
	// 名字那一行的墨从**它该在的那一列**开始找：起点太靠左会先撞上封面里
	// 那枚占位图标（那是封面，不是名字）。
	want := titleLeft + int(favCoverSize) + int(favCoverGap)
	nameLeft := inkLeft(img, row, want-2)
	if nameLeft < want-2 || nameLeft > want+2 {
		t.Errorf("分组标题的字起在 x=%d，收藏夹名的字却起在 x=%d（want %d 上下）——"+
			"名字没有让开那枚封面", titleLeft, nameLeft, want)
	}
}

// TestEachFavoriteRowHasACoverAndACount 断**每一行是「图 + 名称 + N 首」**。
//
// ⚠️ **这是票 #270 的正题**：用户 2026-10-25 给的参考图（Biu 的设计稿）上，
// 侧栏那两组收藏夹的每一行是一枚方形封面 + 名称 + 数量，不是纯文字。
//
// 三件事：
//
//  1. **左边有一枚方块**（封面那一格）；
//  2. **名字右边有「N 首」**（数量用的是 B 站那个 `media_count`）；
//  3. **那枚方块和名字各自在該在的列上**（上面那条已经钉了名字，这一钉方块）。
func TestEachFavoriteRowHasACoverAndACount(t *testing.T) {
	_, a, tt, _ := sideShell(t)
	a.SetFavorites(app.FavoritesCreated, []app.Favorite{{
		ID: 1, Title: "夏日午后", Mid: 42, Type: 11,
		// 封面地址留空：没有封面是真的有（B 站那边用户没设过），画占位图就是
		// 对的——而这一格**照样得在**。
		Cover:      "",
		MediaCount: 32,
	}})
	tt.Frame()

	row, ok := tt.Find("夏日午后")
	if !ok {
		t.Fatalf("侧栏上没有「夏日午后」那一行，文字有 %q", tt.Texts())
	}

	// ② 数量那一行。
	if !tt.HasText("32 首") {
		t.Errorf("那一行没有写数量，文字有 %q", tt.Texts())
	}

	// ① 左边那枚方块：从封面那一列（`favCoverX`）往右找第一笔墨，它得落在
	// 封面那一格里面（`favCoverSize` 见方）。
	//
	// ⚠️ **用的是画面上的墨，不是元素**：封面那一格是 imgcache 画的，界面上
	// 没有一个能按名字找着的文字节点——而它没地址时画的那枚占位图标是有墨的
	// （`widgets/imgcache` 的「没有地址」那一档），量得到。
	img := tt.Image()
	ink := inkLeft(img, row, int(favCoverX))
	if ink < 0 || ink >= int(favCoverX)+int(favCoverSize) {
		t.Errorf("那一行左边没有一枚方块（第一笔墨在 x=%d，而封面那一格是 %d..%d）",
			ink, int(favCoverX), int(favCoverX)+int(favCoverSize))
	}
}

// TestTheFavoritesEmptyLineLinesUpWithTheNames 断**空状态那句**和收藏夹名同列。
//
// ⚠️ 这一条和上面那一条是**两件事**：上面断的是「有内容的时候」，这一条断的
// 是「一个都没有的时候」——而空状态那一档平时根本不出现在画面上（要真去拉过
// B 站、而且那边确实空着才有），所以它得单独钉住。
func TestTheFavoritesEmptyLineLinesUpWithTheNames(t *testing.T) {
	_, tt := favEmptyShell(t)

	title, ok := tt.Find("我创建的")
	if !ok {
		t.Fatalf("侧栏上没有「我创建的」那一组，文字有 %q", tt.Texts())
	}
	line, ok := tt.Find("还没有收藏夹")
	if !ok {
		t.Fatalf("侧栏上没有「还没有收藏夹」那一句，文字有 %q", tt.Texts())
	}
	if line.X != title.X {
		t.Errorf("那一句起头在 x=%.1f，而分组标题在 x=%.1f——它不属于任何一组",
			line.X, title.X)
	}
}

// TestTheFavoritesAreaHasRoomAboveIt 断这一片**上面留了一截**。
//
// 这一片是侧栏的**第三段**（前两段是「发现」「我的音乐」），而侧栏的分组标题上方
// 留的是 12——不留这一截，第一组的标题就**紧贴着上面那一条**，这一片看着像上
// 一段的延续，而不是自己的一段（改造前那一条缝只有 16，而组内是 8 上下）。
//
// ⚠️ **量的是登录态**：未登录时侧栏里有一句「还有 3 个入口，登录后可见」挡在
// 「设置」和这一片之间，那一句自己就占了一截——量它就量不到这一片头上的缝了。
func TestTheFavoritesAreaHasRoomAboveIt(t *testing.T) {
	f := newFolderBili(t, nil)
	_, tt := refreshShell(t, f, "7")

	above, ok := tt.Find("设置")
	if !ok {
		t.Fatalf("侧栏上没有「设置」那一项，文字有 %q", tt.Texts())
	}
	fold, ok := tt.Find("折叠 我创建的")
	if !ok {
		t.Fatalf("侧栏上没有「我创建的」那个折叠开关，文字有 %q", tt.Texts())
	}
	// ⚠️ **从「设置」那行字的底算起**：那一行是 40 高（上游 HeroUI 的 Button md），
	// 而它的字在行里居中——所以「字的底到行的底」还有 12 上下，那是**上一段自
	// 己的**留白。这一片头上的缝是从**行底**算的，而行底拿不到，只能从字底算，
	// 于是阈值要算上那 12（实测：改之前 16、改之后 28）。
	if gap := fold.Y - (above.Y + above.H); gap < 20 {
		t.Errorf("「我创建的」那个折叠开关离上面那一条只有 %.1f 的缝——这一片头上没留白",
			gap)
	}
}

// TestTheFavoritesTextIsReadableOnTheSidebar 断这一片的字在侧栏底上
// **≥ 4.5:1**。
//
// 这一片有三种字，各压在不同的一层底上：
//
//   - **分组标题 / 空状态 / 在飞状态**：次要色，压侧栏底；
//   - **收藏夹名**：正文色，压侧栏底；
//   - **当前那一行**：按那层 `accentSoft` 淡底现算的字（`flatCurrentText`）。
//
// ⚠️ **四个方案 × 深浅两套都要过**：方案换了，字色和底色一起换——而它们的对
// 比度不是同一个数（实测最低的是经典浅色 4.83:1，暖纸浅色 5.00:1）。
func TestTheFavoritesTextIsReadableOnTheSidebar(t *testing.T) {
	for _, preset := range []app.ThemePreset{
		app.ThemePresetClassic, app.ThemePresetWarmPaper,
		app.ThemePresetDuskAmber, app.ThemePresetSoftPeach,
	} {
		for _, dark := range []bool{false, true} {
			shade := "浅色"
			if dark {
				shade = "深色"
			}
			st := app.DefaultSettings("")
			st.ThemePreset = preset
			th := BuildTheme(st, dark)
			sidebar := th.Background
			if sk, _, ok := presetSkinFor(preset, dark); ok {
				sidebar = ui.Hex(sk.sidebar)
			}
			for _, c := range []struct {
				what string
				from ui.Color
				onto ui.Color
			}{
				{"分组标题 / 空状态 / 在飞状态（次要色）", th.TextMuted, sidebar},
				{"收藏夹名（正文色）", th.Text, sidebar},
				{"当前那一行（按淡底现算）", flatCurrentText(th, true), accentSoft(th)},
			} {
				if r := ink.Ratio(c.from, c.onto); r < 4.5 {
					t.Errorf("%s / %s：%v 压 %v 只有 %.2f:1（< 4.5）",
						preset, shade, c.from, c.onto, r)
				}
			}
		}
	}
}

// TestTheFavoritesGroupTitleIsTheSameKindOfTitleAsTheSidebarOne 断这一片的
// **分组标题**和侧栏的分组标题是同一档。
//
// 「发现」「我的音乐」是 12 / 500 / 次要色（`viewSideGroupTitle`），而这一片的
// 「我创建的」「我收藏的」也是同一档——两处挨着，用户一眼就该看出它们是同一层
// 东西（上游那边分组标题是弱化的次级文字，而菜单项和收藏夹名才是正文色）。
//
// ⚠️ **字号用盒高量**：一行字画出来多高是字号 × 行高（`textBoxPerSize`），
// 而字重在盒子里量不到（400 和 500 的行高一样）。字重那一档由**画面上的
// 墨量**比：两处都是 12 号的次要色，而墨量相近就说明它们是同一档字。
func TestTheFavoritesGroupTitleIsTheSameKindOfTitleAsTheSidebarOne(t *testing.T) {
	_, _, tt, _ := favShell(t, 2, 0)

	side, ok := tt.Find("发现")
	if !ok {
		t.Fatalf("侧栏上没有「发现」那一组，文字有 %q", tt.Texts())
	}
	title, ok := tt.Find("我创建的")
	if !ok {
		t.Fatalf("侧栏上没有「我创建的」那一组，文字有 %q", tt.Texts())
	}

	// 判据一：**字号一样**（盒高一样）。
	if math.Abs(float64(side.H-title.H)) > 0.6 {
		t.Errorf("「我创建的」那一行字高 %.2f，而侧栏的分组标题「发现」是 %.2f——"+
			"它们不是同一档字号", title.H, side.H)
	}

	// 判据二：**字色一样**（两处都是次要色）。
	img := tt.Image()
	sideInk := inkOfRect(img, side)
	titleInk := inkOfRect(img, title)
	if !nearRGBA(sideInk, titleInk, 24) {
		t.Errorf("「我创建的」的字是 %v，而侧栏的分组标题「发现」是 %v——两处不是同一档字",
			titleInk, sideInk)
	}
}

// TestTheCollapsedFavoritesPanelIsACard 断折叠态那层浮层是**卡片**那一档。
//
// ⚠️ **底色是 `surface1`，不是页面底**：这一层是一张浮在侧栏旁边的卡片（头像
// 下拉是同一块料），而「贴着一层底的小浮层」那一档（`popupPanel`：底 = 页面底、
// 圆角写死的 8）是另一种东西——同一个侧栏里两种浮层挨着，一块浮在内容上、
// 一块像贴上去的。
//
// 经典那一档下这两者也差得开：`cardBackground` 是控件层（`t.Surface`），
// `popupPanel` 那一档是页面底（`t.Background`）。
func TestTheCollapsedFavoritesPanelIsACard(t *testing.T) {
	s, tt := collapsedSidebar(t)

	if err := tt.Click(favCollapsedName); err != nil {
		t.Fatalf("点那枚「收藏夹」图标：%v", err)
	}
	tt.Frame()

	// 浮层里的组名。折叠态时侧栏不画组标题，所以这一个只可能来自浮层。
	group, ok := tt.Find("我创建的")
	if !ok {
		t.Fatalf("浮层里没有「我创建的」那一组，文字有 %q", tt.Texts())
	}
	// 取浮层底上一点：组名那行字的**左边**（浮层有 4 的内边距 + 行自己的 8）。
	//
	// ⚠️ **别贴着浮层的左沿取**：那儿是 1 像素的描边，量到的是描边和底色的
	// 混合值。
	img := tt.Image()
	px := samplePixel(img, int(group.X)-2, int(group.Y)+int(group.H)/2)
	theme := BuildTheme(app.DefaultSettings(""), false)
	want := rgbaOf(s.cardBackground(theme))
	if !nearRGBA(px, want, 3) {
		t.Errorf("浮层那一块的底色是 %v，want %v（卡片那一档的 surface1）", px, want)
	}
	_ = s
}

// TestTheCollapsedFavoritesIconLightsUpOnACollectionPage 断折叠态那枚图标在
// **收藏夹页上**要亮起来。
//
// 折叠起来之后侧栏里只剩这一枚图标，它就是「当前在哪」唯一的指示——不亮的话
// 用户在收藏夹那一页上，却看不出来自己在哪一页。
//
// 判据是**那一格里的底色**：和侧栏菜单项在折叠态里的画法一样（`flatCurrent`——
// 强调色淡底 + 按那层底现算的字）。
func TestTheCollapsedFavoritesIconLightsUpOnACollectionPage(t *testing.T) {
	s, tt := collapsedSidebar(t)

	s.navigate(collectionPath(1))
	tt.Frame()

	icon, ok := tt.Find(favCollapsedName)
	if !ok {
		t.Fatalf("折叠态的侧栏上没有「收藏夹」那一枚，文字有 %q", tt.Texts())
	}
	// 取那一格里的**底**色：图形在格子中间，所以取左上角那一点。
	img := tt.Image()
	px := samplePixel(img, int(icon.X)+3, int(icon.Y)+3)
	theme := BuildTheme(app.DefaultSettings(""), false)
	if !nearRGBA(px, rgbaOf(accentSoft(theme)), 3) {
		t.Errorf("收藏夹那一枚的底色是 %v，want %v（accentSoft 淡底）",
			px, rgbaOf(accentSoft(theme)))
	}
}

// inkOfRect 是那一块里的**字**色：底色取众数，字取离底最远的那一个像素
// （字是抗锯齿画的，最远的那一头就是它本身）。
func inkOfRect(img *image.RGBA, r mygoui.Rect) color.RGBA {
	bg := modeOf(bandPixels(img, r))
	return farthestFrom(img, r, bg)
}

// inkLeft 是那一条带里第一个「有墨」的 x —— **文字的左边缘**。
//
// 从 x0 开始往右扫，撞到第一个和底色差得够远的像素就停。
//
// ⚠️ **为什么不用 Tester 给的 rect**：MyGo 把**列表行**里的文字按 inline 段落
// 排版，而那个段落节点的 rect 是**行盒**的左沿（不是文字的）——量出来会差 20
// 个像素，而那 20 个正是这一片要钉的东西（收藏夹名的字该和分组标题的字同列）。
//
// ⚠️ **底色取那一带里的众数**：字只占一条带的一小部分，众数才是底。
func inkLeft(img *image.RGBA, r mygoui.Rect, x0 int) int {
	bg := modeOf(bandPixels(img, r))
	for x := x0; x < int(r.X+r.W); x++ {
		for y := int(r.Y); y < int(r.Y+r.H); y++ {
			if maxDiff(img.RGBAAt(x, y), bg) > 40 {
				return x
			}
		}
	}
	return -1
}

// samplePixel 取那一个像素。越界返回全透明——调用方拿它和期望值比，
// 不一样就是「那儿没有那个底色」。
func samplePixel(img *image.RGBA, x, y int) color.RGBA {
	if x < 0 || y < 0 || x >= img.Bounds().Dx() || y >= img.Bounds().Dy() {
		return color.RGBA{}
	}
	return img.RGBAAt(x, y)
}
