package ui

import (
	"testing"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/ShellMonster/Biu_Go/internal/service/bili"
	mygoui "github.com/egoist/mygo/ui"
)

// max32 是两个数里大的那个。
func max32(a, b float32) float32 {
	if a > b {
		return a
	}
	return b
}

// min32 是两个数里小的那个。
func min32(a, b float32) float32 {
	if a < b {
		return a
	}
	return b
}

// 这一族盯播放条左边那一块的**结构**（票 #219，报告 §2.8 / §2.9 / §2.10 / §3.6）。
//
// 上游是**三段**：
//
//	封面 │ 歌名·UP名（文字列）│ 分集·收藏·赞（横着并排的一行）
//
// 改之前我们把三枚键摆在**歌名下面那一行**（和 UP 名并排），于是「歌名/UP 名」
// 和「三枚键」是**上下**的关系，而上游是**左右**的关系。
//
// 这一族盯四件事：
//
//	三段真的横着摆（三枚键在歌名**右边**，不是下面）
//	UP 名**左对齐**（和歌名同一个左沿）
//	第二行和歌名之间的空隙是上游那个（`space-y-1` = 4）
//	三枚键的图标是 **18**（上游播放条上全部图标键都是 18）

// leftBarTester 造一个**真的挂上假 B 站**的播放条外壳（多 P 的那一档）。
//
// ⚠️ **必须挂真的 App**：三枚键里有两枚是**逐帧问 App** 才摆的——分集那一枚
// 看「这一首有几集」（`viewPartButton`），赞那一枚看「这一首能不能赞」
// （`viewLikeStar`）。只喂一份 State 的那个 tester（`playbarTester`）背后是
// 一个空的 App，那三枚键摆不出来。
func leftBarTester(t *testing.T, pages []any, width int) (*mygoui.Tester, *Shell) {
	t.Helper()
	s, tt := onlineTester(t, pages, false, at(width, 560))
	partsReady(t, s, tt, lenOf(pages))
	tt.Frame()
	return tt, s
}

// TestTheLeftBlockIsThreeSegments 盯「左边那一块是三段」。
//
// # 为什么这条重要
//
// 改之前三枚键在**歌名下面那一行**。那也是一个「三枚键」的样子，而**界面上
// 看不出坏了**——键还是那三枚、也还点得动，只是它们和「歌名/UP 名」是上下
// 的关系，而上游是左右的。
//
// # 四条判据
//
//	① 封面在歌名**左边**（上游那一行的第一个孩子）
//	② 三枚键在文字列**右边**——不是下面
//	③ 三枚键在**同一条水平线上**（横排；竖排的话垂直中心会差 32）
//	④ 次序是 分集 → 收藏 → 赞（上游 `left/index.tsx:86/89/90`）
func TestTheLeftBlockIsThreeSegments(t *testing.T) {
	tt, _ := leftBarTester(t, multiPages(), 1200)

	cover, ok := tt.Find("当前封面")
	if !ok {
		t.Fatalf("画面上找不到封面，文字有 %q", tt.Texts())
	}
	title, ok := tt.Find("一首歌的名字很长很长很长很长")
	if !ok {
		t.Fatalf("画面上找不到歌名，文字有 %q", tt.Texts())
	}
	// 三枚键。⚠️ **「赞」那一枚要真的挂上 App 才摆得出来**（`viewLikeStar`）。
	keys := []string{"分集", "收藏", "赞"}
	boxes := make([]mygoui.Rect, 0, len(keys))
	for _, k := range keys {
		r, ok := tt.Find(k)
		if !ok {
			t.Fatalf("画面上找不到 %q，文字有 %q", k, tt.Texts())
		}
		boxes = append(boxes, r)
	}

	// ① 封面在歌名左边。
	if cover.X+cover.W > title.X {
		t.Errorf("封面（x=%.1f..%.1f）和歌名（x=%.1f）重叠了——"+
			"左边那一块的第一段是封面", cover.X, cover.X+cover.W, title.X)
	}

	// ② 三枚键在文字列右边。
	//
	// ⚠️ **量的是「文字列的右沿」，不是歌名那一行的右沿**：歌名右边还有音质
	// 徽章（28 宽 + 一道 6 的缝），而徽章是**跟着歌名**的。文字列的右沿要用
	// UP 名那一行量——它是 `Grow(1)` 的，铺满整段（名字长了会被截断，但框还是
	// 那一段那么宽）。
	owner, ok := tt.Find("一个非常非常非常非常非常非常长的 UP 主名字")
	if !ok {
		t.Fatalf("画面上找不到 UP 名，文字有 %q", tt.Texts())
	}
	for i, k := range keys {
		if boxes[i].X < owner.X+owner.W {
			t.Errorf("%q 在 x=%.1f，而文字列的右沿在 %.1f——"+
				"它在文字列**下面**（改之前那一行），而上游是**右边**",
				k, boxes[i].X, owner.X+owner.W)
		}
	}

	// ③ 三枚键在同一条水平线上（横排）。
	for i := 1; i < len(boxes); i++ {
		mid := boxes[i].Y + boxes[i].H/2
		prev := boxes[i-1].Y + boxes[i-1].H/2
		if widthGap(mid-prev) > 1 {
			t.Errorf("%q 的垂直中心在 y=%.1f，而 %q 那一枚在 y=%.1f——"+
				"它们不在同一条水平线上：那是**竖排**，而上游那一段是 "+
				"`flex items-center`（横排）",
				keys[i], mid, keys[i-1], prev)
		}
	}

	// ④ 次序：分集 → 收藏 → 赞。
	for i := 1; i < len(boxes); i++ {
		if boxes[i].X <= boxes[i-1].X {
			t.Errorf("%q 在 x=%.1f，而 %q 那一枚在 x=%.1f——"+
				"次序不对（上游是 分集 → 收藏 → 赞）",
				keys[i], boxes[i].X, keys[i-1], boxes[i-1].X)
		}
	}
}

// TestTheOwnerNameIsLeftAlignedWithTheTitle 盯 UP 名**左对齐**。
//
// 🔴 **上游是左对齐**：那一列是 `items-start`，而 UP 名是**普通 `span`**
// （`playbar/left/index.tsx:46` 与 `:72-83`）——所以那句话紧跟在歌名下面，
// **和歌名同一个左沿**。
//
// 改之前我们那儿是 `flatButton(...).Grow(1)`，而**框架的按钮内容默认居中**
// ——那句话飘到了那一段的中间（实测歌名在 x=80、UP 名在 x=219）。
//
// ⚠️ **判据是「两个左沿差不超过 1 像素」**，不是「差不多」：差 2 像素就已经是
// 「没对齐」了（上游那一列里两句话的左沿分毫不差）。
//
// # 🔴 **名字必须短，否则这一条是恒真的**
//
// 那一段（歌名 + UP 名）只有 76~140 宽，而**名字比它还长的话，文字盒被拉满**
// ——于是**左对齐和居中算出来的左沿一模一样**（都是那一段的左沿），这一条就
// 什么也钉不住。
//
// 第一版就是踩了这个：用的是「一个非常非常非常非常非常非常长的 UP 主名字」
// （140 宽，比那一段还宽），去掉 `Justify(ui.Start)` 它**照样绿**——整包也没有
// 任何一条测试能抓住那个变异。**实测**（去掉 `Justify(ui.Start)` 之后）：
// 短名「某个 UP 主」→ UP 名在 x=115.9（差 35.9）；「UP」→ x=140.5（差 60.5）；
// 而那个长名 → 仍然是 x=80.0（差 0）。
//
// 所以这里量的是**短名**；长名那一档只保证「它还在、没被挤没」。
func TestTheOwnerNameIsLeftAlignedWithTheTitle(t *testing.T) {
	for _, c := range []struct {
		name      string
		artist    string
		artistMid int64
		// pinned 说这一档量的是「左沿」（名字短，Start 和 Center 算得不一样）
		// 还是「它还在」（名字比那一段还长，两种对齐算出来一样）。
		pinned bool
	}{
		{"有编号 · 短名", "某个 UP 主", 12345, true},
		{"没有编号 · 短名", "某个 UP 主", 0, true},
		{"有编号 · 名字比那一段还长", "一个非常非常非常非常非常非常长的 UP 主名字", 12345, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			tt, _ := metaBarTester(t, app.State{
				HasTrack: true, Title: "一首歌的名字很长很长很长很长",
				Artist: c.artist, ArtistMid: c.artistMid,
			}, 1000)

			title, ok := tt.Find("一首歌的名字很长很长很长很长")
			if !ok {
				t.Fatalf("画面上找不到歌名，文字有 %q", tt.Texts())
			}
			owner, ok := tt.Find(c.artist)
			if !ok {
				t.Fatalf("画面上找不到 UP 名 %q，文字有 %q", c.artist, tt.Texts())
			}
			if !c.pinned {
				// 这一档钉不住「左沿」（名字把那一段填满了），只钉「它还在、
				// 没被挤没」——那是 `TestTheOwnerIsNeverSqueezedOutOfExistence`
				// 的活。
				if owner.W < ownerNameMinW {
					t.Errorf("UP 名只剩 %.1f 宽（下限 %d）", owner.W, ownerNameMinW)
				}
				return
			}
			if d := widthGap(owner.X - title.X); d > 1 {
				t.Errorf("UP 名在 x=%.1f，而歌名在 x=%.1f（差 %.1f）——"+
					"它没有和歌名左对齐（上游那一列是 items-start，"+
					"而框架的按钮内容默认居中）", owner.X, title.X, d)
			}
		})
	}
}

// TestTheQualityChipIsShortEnoughToSitNextToTheTitle 盯音质徽章**够短**。
//
// 那一列（歌名 + UP 名）在多 P 那一档只有 76 宽，而框架在装不下时按**基宽的
// 比例**分摊那份 shortfall——**徽章自己被摊扁**就是从这儿来的：全称
// 「杜比全景声」（11px 下 54.7 宽）被摊到 **13.8**（读出来一个字），而它同时
// 是「换音质」的入口，**点击区只剩 14 像素**。
//
// 所以徽章显示的是**短版**（上游那一枚 chip 的形状：「无损」/「杜比」，
// `playbar/left/index.tsx:50-56`），全称留在悬停提示里。
//
// ⚠️ **上限是 30**：76 − 6（那道缝）− 30 = **40**，而 40 是歌名那一档的下限
// （`partsTitleMinW`）。徽章再宽，歌名就被挤到那个下限以下——那就不是「截短」
// 而是「压成竖条」了。
//
// ⚠️ **量的是画出来的那一枚**：这一条靠 `metaBarTester`（那一段有 140 宽，
// **装得下**徽章的自然宽），所以量到的是它**本来的**宽——徽章要是退回全称，
// 这一条立刻红（54.7 > 30）。
func TestTheQualityChipIsShortEnoughToSitNextToTheTitle(t *testing.T) {
	for _, full := range []string{
		"192K", "128K", "64K", "无损 FLAC", "Hi-Res 无损", "杜比全景声", "未知音质",
	} {
		t.Run(full, func(t *testing.T) {
			tt, _ := metaBarTester(t, app.State{
				HasTrack: true, Title: "一首歌的名字很长很长很长很长",
				Artist: "某个 UP 主", ArtistMid: 12345,
				Quality: full,
			}, 1000)

			chip, ok := tt.Find(bili.ChipLabel(full))
			if !ok {
				t.Fatalf("画面上找不到徽章 %q，文字有 %q", bili.ChipLabel(full), tt.Texts())
			}
			const maxChipW = 30
			if chip.W > maxChipW {
				t.Errorf("徽章 %q 是 %.1f 宽（上限 %d）——"+
					"它和歌名挤在 76 宽的那一行里会被摊扁"+
					"（实测全称那一档被摊到 13.8：一个字，点击区只剩 14 像素）",
					bili.ChipLabel(full), chip.W, maxChipW)
			}
		})
	}
}

// TestTheGapBetweenTheTitleAndTheOwnerName 盯第二行和歌名之间的空隙。
//
// 上游是 `space-y-1`（`playbar/left/index.tsx:46`）= **4**。
//
// ⚠️ **量的是「框到框」**：歌名那一行的框底到 UP 名那一行的框顶。
// 我们的文本盒是字体的 ascent+descent（**框里没有额外的行距**），所以框到框
// 就是眼睛看见的那一档；上游那边行盒里还留着一截 leading（它的行盒是 24 / 20），
// 所以**同样的 4 在上游那边看着比这边松一点**——那不是我们这一档的差别。
func TestTheGapBetweenTheTitleAndTheOwnerName(t *testing.T) {
	tt, _ := leftBarTester(t, multiPages(), 1200)

	title, ok := tt.Find("一首歌的名字很长很长很长很长")
	if !ok {
		t.Fatalf("画面上找不到歌名，文字有 %q", tt.Texts())
	}
	owner, ok := tt.Find("一个非常非常非常非常非常非常长的 UP 主名字")
	if !ok {
		t.Fatalf("画面上找不到 UP 名，文字有 %q", tt.Texts())
	}
	got := owner.Y - (title.Y + title.H)
	const want = 4 // 上游的 space-y-1
	if widthGap(got-want) > 1 {
		t.Errorf("歌名那一行的框底到 UP 名那一行的框顶是 %.1f，want %d——"+
			"上游那一列是 `space-y-1`（4）", got, want)
	}
}

// TestTheThreeLeftKeysUseTheUpstreamIconSize 盯三枚键的图标是 **18**。
//
// 🔴 **上游播放条上全部图标键都是 18**（票 #219，报告 §3.6）：赞是
// `RiThumbUpFill/Line size={18}`、收藏是 `RiStarFill/Line size={18}`、分集是
// `RiListRadio size={18}`。改之前我们那三枚是 **14**（`iconSm`）——小 4 像素。
//
// # 判据是画面上的墨迹
//
// ⚠️ **不能拿按钮框当判据**：那三枚的盒子恒是 32（`iconHitMd`），14 和 18
// 装在同一个盒子里，框一点没变——变的是**图形**。
//
// 实测（深浅两套一样）：18 那一档的墨迹是 分集 19×18、收藏 22×21、赞 22×20；
// 14 那一档是 16×16、18×17、18×17。门槛定在 19：两头都离得开。
func TestTheThreeLeftKeysUseTheUpstreamIconSize(t *testing.T) {
	tt, _ := leftBarTester(t, multiPages(), 1200)

	for _, label := range []string{"分集", "收藏", "赞"} {
		g := scanGlyph(tt, label)
		if g.ink == "" {
			t.Fatalf("%q 那一块没画东西：%q", label, tt.Texts())
		}
		if big := max32(g.box.W, g.box.H); big < 19 {
			t.Errorf("%q 的图形是 %.0fx%.0f——上游那一枚是 18"+
				"（实测 18 那一档是 19~22，14 那一档是 16~18）",
				label, g.box.W, g.box.H)
		}
	}
}

// TestTheLeftKeysStayPutWhenThePartsKeyAppears 盯「分集那一枚不挪别的东西」。
//
// 分集那一枚是**逐帧问 App** 才摆的（单 P 的稿子不摆），而它在第三段里
// **占着自己那一格**——所以它出现/消失的时候，收藏和赞**不许挪位置**。
//
// ⚠️ **这一条量的是位置，不是宽度**：宽度变了不算坏（那一段本来就跟着内容
// 宽窄），**位置变了**才是坏——键在用户眼前跳一下，而用户什么都没点。
func TestTheLeftKeysStayPutWhenThePartsKeyAppears(t *testing.T) {
	single, _ := leftBarTester(t, nil, 1200)
	multi, _ := leftBarTester(t, multiPages(), 1200)

	for _, label := range []string{"收藏", "赞"} {
		a, okA := single.Find(label)
		b, okB := multi.Find(label)
		if !okA || !okB {
			t.Fatalf("%q 在两档里有一档量不到（单 P %v / 多 P %v）", label, okA, okB)
		}
		if d := widthGap(a.X - b.X); d > 1 {
			t.Errorf("%q 在单 P 那一档的 x=%.1f，多 P 那一档的 x=%.1f（差 %.1f）——"+
				"分集那一枚出现/消失的时候它挪了位置（它该占自己那一格）",
				label, a.X, b.X, d)
		}
	}
}
