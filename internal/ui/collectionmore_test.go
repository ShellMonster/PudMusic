// 歌单内容页 ⋯ 菜单里那三件（清除失效内容 / 删除收藏夹 / 从侧栏移除）的测试。
//
// ⚠️ 这一组全部走 HTTP fixture（`favBili`）——这三个都是**写操作**，而它们写
// 的是用户真实的收藏夹。真网联调单列在任务报告的「需要真网验」那一节。
//
// ⚠️ **每一个「等」都用结果当判据**：后台那一趟跑在 goroutine 里，而 Tester
// 一帧只要几十微秒，按帧数等就是按运气等，满载的机器上会偶发地红。

package ui

import (
	"strings"
	"testing"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/ShellMonster/Biu_Go/internal/service/bili"
	"github.com/egoist/mygo/ui"
)

// favRef 是 fixture 里那个收藏夹的容器引用（id 和 `favBili` 的回答一致）。
var favRef = app.CollectionRef{ID: 12345, Type: bili.CollectionFavorite}

// openMore 打开 ⋯ 菜单。
func openMore(t *testing.T, tt *ui.Tester) {
	t.Helper()
	if err := tt.Click("更多操作"); err != nil {
		t.Fatalf("打开 ⋯ 菜单：%v", err)
	}
	settleFrame(tt)
}

// chooseMore 从 ⋯ 菜单里点一项。点不到会报错（菜单是这一趟全部建出来的）。
func chooseMore(t *testing.T, tt *ui.Tester, label string) error {
	t.Helper()
	openMore(t, tt)
	err := tt.ChooseMenuItem(label)
	settleFrame(tt)
	return err
}

// screenHasText 说画面上有没有一句话的**一部分**。
//
// ⚠️ 不用 `wantText`：那一处是 `tt.HasText` 的整句匹配，而确认框的正文是一整段
// 话（要同时说清收藏夹名、里面几首、撤不回来），拆成整句去匹配会把断言绑死在
// 措辞的每一个字上。这里要的是「这几件事它说了没有」。
func screenHasText(tt *ui.Tester, part string) bool {
	return strings.Contains(strings.Join(tt.Texts(), "\n"), part)
}

// wantSaid 说画面上要有这句话的一部分，说清楚没有的是哪一段。
func wantSaid(t *testing.T, tt *ui.Tester, part string) {
	t.Helper()
	if !screenHasText(tt, part) {
		t.Errorf("画面上没出现 %q 这一句，文字有 %q", part, tt.Texts())
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 哪些项摆得出来
// ─────────────────────────────────────────────────────────────────────────────

// TestOnlyTheOwnerGetsTheOwnerActions 「清除失效内容」「删除收藏夹」只有自己
// 创建的收藏夹才摆得出来，「从侧栏移除」哪一页都有。
//
// 🔴 三个格子各有各的理由，少一个都会让用户按出一个**注定失败**的按钮，而 B 站
// 回的「权限不足」在界面上和账号坏了长得一模一样：
//
//   - 别人建的：B 站没有删别人收藏夹的接口。
//   - 默认收藏夹（B 站发下来的那一个）：同样删不掉。
//   - 没登录：`OwnerMid` 为 0 时 `0 == 0` 会让判断通过，而真按下去什么也不会发生。
//
// 「从侧栏移除」对别人建的也摆得出来——它一个字节都不往 B 站发，对别人的收藏夹
// 它恰恰是唯一能整掉它的办法。
func TestOnlyTheOwnerGetsTheOwnerActions(t *testing.T) {
	for _, tc := range []struct {
		name     string
		ownerMid int64
		attr     int
		loggedIn string
		want     [2]bool // 清除失效内容 / 删除收藏夹
	}{
		{"自己建的非默认", 1, 2, "1", [2]bool{true, true}},
		{"别人建的", 900, 2, "1", [2]bool{false, false}},
		{"默认收藏夹", 1, 0, "1", [2]bool{false, false}},
		{"没登录", 0, 2, "", [2]bool{false, false}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b, s, tt := collectionShellAs(t, tc.loggedIn, "12345", mediaOf(1, "歌 一"))
			// ⚠️ 元信息是缓存着的：改了 fixture 之后要重新拉一次，否则判的还是
			// 刚进来那一份（也就是默认那一份：upper.mid=1、attr=2）。
			b.setOwnerMid(tc.ownerMid)
			b.setAttr(tc.attr)
			s.app.LoadCollection(favRef)
			waitFrames(t, tt, "新的元信息缓存进来", func() bool {
				info := s.app.Collection(favRef)
				return info.Loaded && info.OwnerMid == tc.ownerMid && info.Default == (tc.attr == 0)
			})

			for i, label := range []string{"清除失效内容", "删除收藏夹"} {
				offered := chooseMore(t, tt, label) == nil
				if tc.want[i] && !offered {
					t.Errorf("菜单里摆不出 %q", label)
				}
				if !tc.want[i] && offered {
					t.Errorf("菜单里居然摆出了 %q（按下去注定失败）", label)
				}
				// ⚠️ 选中了会**弹出系统确认框**，而它盖住整个界面（点外面不关是
				// 它该有的脾气）。不收掉的话下面那一次点「更多操作」根本按不到，
				// 于是量到的会是「点不开菜单」——一个和被测逻辑毫无关系的红。
				_ = tt.Click("取消")
				settleFrame(tt)
			}
			// ⚠️ 「从侧栏移除」每一页都得在：它是唯一那个「谁都能做」的。
			if err := chooseMore(t, tt, "从侧栏移除"); err != nil {
				t.Errorf("菜单里摆不出「从侧栏移除」：%v", err)
			}
		})
	}
}

// TestNoTwoMenuItemsShareAKeyOrALabel ⋯ 菜单按 label 建项（`m.Item`），按 key
// 分发。
//
// 两个项同名的话用户点第二个会走第一个那一支，而键重名的话后一个分支永远走不到
// ——两种症状都是「按了没反应 / 按了另一件事」，而且从画面上完全看不出来。
func TestNoTwoMenuItemsShareAKeyOrALabel(t *testing.T) {
	all := make([]collectionMenuItem, 0, 5)
	all = append(all, collectionMoreBaseItems...)
	all = append(all, collectionMoreOwnerItems...)
	all = append(all, collectionMoreForgetItem)

	keys := map[string]string{}
	labels := map[string]string{}
	for _, it := range all {
		if prev, dup := keys[it.key]; dup {
			t.Errorf("键 %q 被 %q 和 %q 用了", it.key, prev, it.label)
		}
		if prev, dup := labels[it.label]; dup {
			t.Errorf("名字 %q 被键 %q 和 %q 用了——用户点第二个会走第一个那一支", it.label, prev, it.key)
		}
		keys[it.key], labels[it.label] = it.label, it.key
	}
	if len(all) != 5 {
		t.Errorf("⋯ 菜单一共有 %d 项，want 5（两个下载 + 两个 owner + 一个从侧栏移除）", len(all))
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 清除失效内容
// ─────────────────────────────────────────────────────────────────────────────

// TestClearingTheInvalidContentAsksFirst 点「清除失效内容」不会直接发请求。
func TestClearingTheInvalidContentAsksFirst(t *testing.T) {
	b, s, tt := collectionShellAs(t, "1", "12345", mediaOf(1, "歌 一"), mediaOf(2, "歌 二"))
	waitSongs(t, tt, s, 2)

	if err := chooseMore(t, tt, "清除失效内容"); err != nil {
		t.Fatal(err)
	}
	wantSaid(t, tt, "清掉「通勤路上」里的失效内容吗？")
	if n := len(b.cleanedFolders()); n != 0 {
		t.Fatalf("确认框还没点确定就发了 %d 次 clean", n)
	}

	if err := tt.Click("清除"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "清除那一趟到了 B 站", func() bool { return len(b.cleanedFolders()) == 1 })
	if got := b.cleanedFolders(); got[0] != "12345" {
		t.Errorf("clean 的 media_id = %v，want [12345]", got)
	}
	wantSaid(t, tt, "失效内容已清除")
}

// TestCancellingAnyOfTheThreeSendsNothing 三个确认框的「取消」一个都不许发东西。
//
// 规格点名「取消则不删」。这三个里有两个删的是**撤不回来**的东西，所以这里把
// 三个各自都走一遍——`runConfirmed` 那张 switch 一旦写错成「取消也走做那一条」，
// 三个会一起出事，而只测一个看不出来。
func TestCancellingAnyOfTheThreeSendsNothing(t *testing.T) {
	for _, item := range []string{"清除失效内容", "删除收藏夹", "从侧栏移除"} {
		t.Run(item, func(t *testing.T) {
			b, s, tt := collectionShellAs(t, "1", "12345", mediaOf(1, "歌 一"), mediaOf(2, "歌 二"))
			waitSongs(t, tt, s, 2)

			if err := chooseMore(t, tt, item); err != nil {
				t.Fatalf("点 %q：%v", item, err)
			}
			if err := tt.Click("取消"); err != nil {
				t.Fatalf("点取消：%v", err)
			}
			settleFrame(tt)

			// ⚠️ 判据是「一个写请求都没有」，不是「这一项没发」：取消之后如果哪
			// 一条兜底的路径还在跑，这里立刻就抓到。
			if n := b.hitsOf("/x/v3/fav/resource/clean"); n != 0 {
				t.Errorf("取消了还发了 %d 次 clean", n)
			}
			if n := b.hitsOf("/x/v3/fav/folder/del"); n != 0 {
				t.Errorf("取消了还删了 %d 次收藏夹", n)
			}
			// 确认框也收了（Esc / 点外面不关是它该有的脾气）。
			if screenHasText(tt, "吗？") {
				t.Errorf("取消了确认框还在，文字有 %q", tt.Texts())
			}
		})
	}
}

// TestACleanedFolderRedrawsItsList 清完之后列表要真的重来。
//
// 🔴 这里量的是「**清完之后**又发了一次内容列表请求」，不是「点完立刻发」。
// 立刻发的那一趟拉回来的还是**还带着失效条目**的列表——刷新确实发生了，只是
// 发生得太早，症状是「按完什么都没变」。
//
// ⚠️ 判据里还带一句 Toast：notice 和 reload 是同一个出口，而「请求次数变大」
// 这一半也可能是界面线程自己触发的那一趟——两个都在，才说明是做完那一趟
// 推着走的。
func TestACleanedFolderRedrawsItsList(t *testing.T) {
	b, s, tt := collectionShellAs(t, "1", "12345", mediaOf(1, "歌 一"), mediaOf(2, "歌 二"))
	waitSongs(t, tt, s, 2)
	before := b.hitsOf("/x/v3/fav/resource/list")

	if err := chooseMore(t, tt, "清除失效内容"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("清除"); err != nil {
		t.Fatal(err)
	}
	waitFrames(t, tt, "清完之后列表重来", func() bool {
		return b.hitsOf("/x/v3/fav/resource/list") > before &&
			screenHasText(tt, "失效内容已清除")
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// 删除收藏夹
// ─────────────────────────────────────────────────────────────────────────────

// TestDeletingAFolderSaysWhichOneAndLeavesThePage 删收藏夹：确认框说清是哪一
// 个、里面有几首、撤不回来，删完这一页得走开。
func TestDeletingAFolderSaysWhichOneAndLeavesThePage(t *testing.T) {
	b, s, tt := collectionShellAs(t, "1", "12345", mediaOf(1, "歌 一"), mediaOf(2, "歌 二"))
	waitSongs(t, tt, s, 2)

	if err := chooseMore(t, tt, "删除收藏夹"); err != nil {
		t.Fatal(err)
	}

	// ⚠️ 三样都要在：收藏夹的名字、里面有几首、撤不回来。少一样用户就可能在
	// 一个读不出后果的框上按确定——而这一下撤不回来。
	wantSaid(t, tt, "删掉「通勤路上」吗？")
	wantSaid(t, tt, "B 站上的这个收藏夹和里面的 2 首都会没")
	wantSaid(t, tt, "撤不回来")

	if err := tt.Click("删除"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "删除那一趟到了 B 站", func() bool { return len(b.deletedFolders()) == 1 })
	if got := b.deletedFolders(); got[0] != "12345" {
		t.Errorf("folder/del 的 media_ids = %v，want [12345]", got)
	}
	// 删完之后当前就停在这个没有了的容器上——再点一次刷新只会得到一句
	// 「打不开这个收藏夹」。
	waitFrames(t, tt, "离开被删掉的那一页", func() bool {
		return s.router.Path() == "/empty"
	})
}

// TestDeletingFromThePageIsTheSameCallTheSidebarMakes 页面上删和侧栏删发出去
// 的是同一个接口。
//
// 两个入口共用 `app.deleteCreatedFolder`。这一条钉的就是这件事——各写一遍的
// 话「侧栏删得掉、页面上删不掉」是最难查的那一类：两边测试各自都绿。
func TestDeletingFromThePageIsTheSameCallTheSidebarMakes(t *testing.T) {
	b, s, tt := collectionShellAs(t, "1", "12345", mediaOf(1, "歌 一"))
	waitSongs(t, tt, s, 1)

	// 先走侧栏那一遍（它要那一行在侧栏里）。
	s.app.SetFavorites(app.FavoritesCreated, []app.Favorite{{ID: 12345, Title: "通勤路上"}})
	var said string
	s.app.DeleteFavoriteFolder(app.Favorite{ID: 12345, Title: "通勤路上"}, func(msg string) { said = msg })
	waitFor(t, "侧栏那一遍删完", func() bool { return len(b.deletedFolders()) == 1 })
	if said == "" {
		t.Error("侧栏那一遍删完一句话都没有——用户按下去像是什么也没发生")
	}

	// 再走页面上那一遍。
	s.navigate("/collection/12345")
	waitFrames(t, tt, "页面重新进来", func() bool {
		return s.collection != nil && s.app.Collection(s.collection.ref).Loaded
	})
	if err := chooseMore(t, tt, "删除收藏夹"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("删除"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "页面上那一遍删完", func() bool { return len(b.deletedFolders()) == 2 })

	if got := b.deletedFolders(); got[0] != got[1] {
		t.Errorf("两次发出去的 media_ids = %v，两个入口发的必须是同一个", got)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 两个删除不该长得一样
// ─────────────────────────────────────────────────────────────────────────────

// TestCancellingLeavesNothingBehind 不：两个「不」的来源各走一遍，而「删 B 站
// 上那个」**一个字节都不发出去**。
//
// ⚠️ **两个来源都要**：点「取消」按钮（`AlertDialog` 返回它的下标 **0**）和按
// Esc（返回 `AlertDismissed`）。它们是两个不同的返回值，只清其中一个的话，
// 另一个就是「按了取消还是删了」——而这里测的是**不可逆**的那一下。
//
// 🔴 **判据是「没发请求」，不是「`p.confirm.kind` 清了没」**：Esc 那条路上一旦
// 真的做了，`startCollectionDeleteFolder` 会把 `s.collection` 整个换掉，于是
// `s.collection.confirm.kind` 读回来的是**新页面**那个恒为 0 的值——那条断言
// 会永远绿，而它看着像在守着什么。
func TestCancellingLeavesNothingBehind(t *testing.T) {
	for _, how := range []string{"取消按钮", "Esc"} {
		t.Run(how, func(t *testing.T) {
			b, s, tt := collectionShellAs(t, "1", "12345", mediaOf(1, "歌 一"))
			waitSongs(t, tt, s, 1)
			if err := chooseMore(t, tt, "删除收藏夹"); err != nil {
				t.Fatal(err)
			}
			// 前提得摆好：框真的弹出来了（没弹出来的话「取消不发请求」是废话）。
			if !screenHasText(tt, "删掉「通勤路上」吗？") {
				t.Fatalf("确认框根本没画出来，文字有 %q", tt.Texts())
			}

			if how == "Esc" {
				tt.Key(0, ui.KeyEscape)
			} else if err := tt.Click("取消"); err != nil {
				t.Fatal(err)
			}
			settleFrame(tt)

			if n := b.hitsOf("/x/v3/fav/folder/del"); n != 0 {
				t.Errorf("%s 之后还是删了 %d 次收藏夹", how, n)
			}
			if screenHasText(tt, "删掉「通勤路上」吗？") {
				t.Errorf("%s 之后确认框还在，文字有 %q", how, tt.Texts())
			}
		})
	}
}

// TestTheTwoDeletionsDoNotReadTheSame 「删 B 站上那个」和「只删本地记录」
// 必须让用户一眼分得出来。
//
// 🔴 规格点名了这一条（issue #100：「前者不可逆，措辞和确认弹窗都要更重」）。
// 判据刻意写成**两边都要说 / 另一边不许说**，而不是「两个字符串不相等」——
// 措辞改一版就不相等了，而真正要挡住的是「两个框长得一样」这件事本身。
func TestTheTwoDeletionsDoNotReadTheSame(t *testing.T) {
	_, s, tt := collectionShellAs(t, "1", "12345", mediaOf(1, "歌 一"), mediaOf(2, "歌 二"))
	waitSongs(t, tt, s, 2)

	if err := chooseMore(t, tt, "删除收藏夹"); err != nil {
		t.Fatal(err)
	}
	heavyTitle, heavyWhat := confirmScreen(t, tt)
	if !strings.Contains(heavyWhat, "撤不回来") {
		t.Errorf("删 B 站上那个的正文没说要撤不回来：%q", heavyWhat)
	}
	if strings.Contains(heavyWhat, "不会动") {
		t.Errorf("不可逆的那一个说了「不会动」：%q", heavyWhat)
	}
	if err := tt.Click("取消"); err != nil {
		t.Fatal(err)
	}
	settleFrame(tt)

	if err := chooseMore(t, tt, "从侧栏移除"); err != nil {
		t.Fatal(err)
	}
	lightTitle, lightWhat := confirmScreen(t, tt)
	if !strings.Contains(lightWhat, "不会动") {
		t.Errorf("只删本地记录的正文没说明白 B 站上的还在：%q", lightWhat)
	}
	if strings.Contains(lightWhat, "撤不回来") {
		t.Errorf("一个字节都不发的那一个说了「撤不回来」：%q", lightWhat)
	}
	if heavyTitle == lightTitle {
		t.Errorf("两个确认框的标题一样（%q），用户分不出按的是哪一个", heavyTitle)
	}
}

// confirmScreen 是画面上那个系统确认框的标题和正文。
//
// ⚠️ **认的是「确认框」那两行**：整个画面上别的字（头图区的标题那一串）会混进
// `Texts()`，而不挑出那两行的话「标题一样吗」会永远答案是「一样」——那条断言
// 就成了恒真，挡不住任何东西。
func confirmScreen(t *testing.T, tt *ui.Tester) (title, what string) {
	t.Helper()
	for _, s := range tt.Texts() {
		switch {
		case strings.Contains(s, "吗？"):
			title = s
		case strings.Contains(s, "撤不回来") || strings.Contains(s, "不会动"):
			what = s
		}
	}
	if title == "" || what == "" {
		t.Fatalf("画面上找不到确认框（title=%q what=%q），文字有 %q", title, what, tt.Texts())
	}
	return title, what
}

// ─────────────────────────────────────────────────────────────────────────────
// 从侧栏移除
// ─────────────────────────────────────────────────────────────────────────────

// TestForgettingTheFolderNeverTouchesBilibili 「从侧栏移除」一个请求都不发，
// 那一行确实从侧栏里没了，而这一页还留着。
//
// 🔴 它和「删除收藏夹」并排摆出来，靠的就是这一条：用户按下它的理由是「我不想
// 删 B 站上那个，只是这儿别列了」。而它一旦发出任何一个写请求——哪怕 B 站回
// -101——用户就已经拿到了他明确不要的那个结果。
func TestForgettingTheFolderNeverTouchesBilibili(t *testing.T) {
	b, s, tt := collectionShellAs(t, "1", "12345", mediaOf(1, "歌 一"), mediaOf(2, "歌 二"))
	waitSongs(t, tt, s, 2)
	s.app.SetFavorites(app.FavoritesCreated, []app.Favorite{{ID: 12345, Title: "通勤路上"}})
	settleFrame(tt)

	if err := chooseMore(t, tt, "从侧栏移除"); err != nil {
		t.Fatal(err)
	}
	wantSaid(t, tt, "B 站上的这个收藏夹不会动")
	if err := tt.Click("移除"); err != nil {
		t.Fatal(err)
	}

	waitFrames(t, tt, "移除的那句话说出来", func() bool {
		return screenHasText(tt, "B 站上的那个还在")
	})
	if n := b.hitsOf("/x/v3/fav/folder/del"); n != 0 {
		t.Errorf("「从侧栏移除」删了 %d 次收藏夹——它一个字节都不该往 B 站发", n)
	}
	if n := len(s.app.SideFavorites(app.FavoritesCreated)); n != 0 {
		t.Errorf("侧栏里还剩 %d 行，want 0", n)
	}
	// ⚠️ **这一页还留着**：收藏夹在 B 站上还在、刚才那两首也还在。走开的话用户
	// 正在看的东西就没了，而他要的只是侧栏别列它。
	if got := s.router.Path(); got != "/collection/12345" {
		t.Errorf("从侧栏移除之后跑到了 %q，want 这一页还停着", got)
	}
}

// TestAFolderNobodyOwnsCanStillBeForgotten 别人的收藏夹摆不出删除，但「从侧栏
// 移除」摆得出来——它是唯一能整掉它的办法。
func TestAFolderNobodyOwnsCanStillBeForgotten(t *testing.T) {
	b, s, tt := collectionShellAs(t, "1", "12345", mediaOf(1, "歌 一"))
	b.setOwnerMid(900)
	s.app.LoadCollection(favRef)
	waitFrames(t, tt, "新的元信息缓存进来", func() bool {
		return s.app.Collection(favRef).OwnerMid == 900
	})
	s.app.SetFavorites(app.FavoritesCollected, []app.Favorite{{ID: 12345, Title: "通勤路上", Type: 11}})

	if err := chooseMore(t, tt, "删除收藏夹"); err == nil {
		t.Error("别人的收藏夹摆出了「删除收藏夹」——B 站没这个接口")
	}
	if err := chooseMore(t, tt, "从侧栏移除"); err != nil {
		t.Fatalf("别人的收藏夹摆不出「从侧栏移除」：%v", err)
	}
	if err := tt.Click("移除"); err != nil {
		t.Fatal(err)
	}
	waitFrames(t, tt, "移除的那句话说出来", func() bool {
		return screenHasText(tt, "B 站上的那个还在")
	})
	if n := b.hitsOf("/x/v3/fav/folder/del"); n != 0 {
		t.Errorf("往 B 站发了 %d 次删除", n)
	}
}
