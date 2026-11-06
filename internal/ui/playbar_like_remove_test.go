// 播放器 UI「点赞 / 收藏」移除这一票的测试（喜欢歌单原型）。
//
// 原型把「赞（thumb）」「收藏（star）」两枚键从播放器上拿掉了，只留下
// 「喜欢」心形——那是用户自己的**喜欢歌单绑定入口**（点开「把这一首放进
// 哪些收藏夹」那个弹窗），不是 B 站的点赞，也不是三连。
//
// 这一族断的是**用户可观察的行为**：
//
//	播放条左列：没有「赞」「收藏」两枚键，也没有它们的数量角标；
//	            「分集」「评论」还在
//	播放条封面：短按照旧开满屏；**长按不再触发三连**（B 站一趟都收不到）
//	满屏：没有赞键（连带「长按三连」那句提示和赞数角标）；
//	      「喜欢」心形还在，点了开收藏夹弹窗
//	菜单「歌曲」栏：没有「赞」那一项，「添加到收藏夹…」还在
//
// ⚠️ 长按那几条用**测试时钟**推进（`fakeClock`），不靠墙上毫秒。

package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/ShellMonster/PudMusic/internal/app"
)

// favEnv 是一台会答赞 / 收藏夹的假 B 站加上一个正在放在线歌的界面，
// **歌单那一摊也挂着**（「喜欢」心形点了要开收藏夹弹窗，没挂只会说一句
// 「收藏夹现在打不开」）。
func favEnv(t *testing.T) *likeEnv {
	t.Helper()
	env := newLikeEnv(t, false, false, false)
	b := newCollectionBili(t)
	env.s.app.MountCollections(app.CollectionRefs{Client: collectionClient(t, b), Settle: 0})
	env.tt.Frame()
	return env
}

// TestThePlaybarHasNoLikeOrFavoriteButton 断「赞」「收藏」两枚键不在播放条上，
// 而「分集」「评论」还在。
//
// ⚠️ **连着数量一起断**（无障碍名字里带过计数）：票 #701 给那两枚键带过
// 角标——原型里没有它们。
func TestThePlaybarHasNoLikeOrFavoriteButton(t *testing.T) {
	tt, _ := leftBarTester(t, multiPages(), 1200)

	for _, label := range []string{"分集", "评论"} {
		if _, ok := tt.Find(label); !ok {
			t.Fatalf("画面上找不到「%s」：它不该跟着赞 / 收藏一起走掉，文字有 %q", label, tt.Texts())
		}
	}
	for _, label := range []string{"赞", "已赞", "收藏", "已收藏"} {
		if _, ok := tt.Find(label); ok {
			t.Errorf("播放条上还摆着「%s」：原型里这一枚已经去掉了，文字有 %q", label, tt.Texts())
		}
	}
}

// TestLongPressingTheCoverDoesNotTriple 断「长按封面 2.5 秒」不再发一键三连。
//
// 三连入口跟着赞键一起走了（喜欢歌单原型里没有三连）——而长按**之后的
// 松手也不许再吞掉那一次 click**：现在按住封面再松手，和短按一样是开满屏。
func TestLongPressingTheCoverDoesNotTriple(t *testing.T) {
	env := newLikeEnv(t, false, false, false)
	s, tt, f := env.s, env.tt, env.f

	x, y := coverPoint(t, tt)
	tt.Press(x, y)
	advanceLike(env, 3*time.Second)
	if got := f.triplePostCount(); got != 0 {
		t.Fatalf("长按封面发了 %d 趟三连：三连入口已经去掉，want 0", got)
	}
	tt.Release(x, y)
	tt.Frame()
	if !s.full.open {
		t.Error("长按封面松手之后满屏没开：三连没了，这一按就是普通的一按")
	}
}

// TestHalfLongPressingTheCoverStillOpensFullPlayer 是上面那一条的另一面：
// 以前按住 600ms（进度环出来、三连还没发）松手，那一次 click 被吞掉、
// 满屏不开（票 #681）。没有三连之后这条吞掉的规矩整个不存在。
func TestHalfLongPressingTheCoverStillOpensFullPlayer(t *testing.T) {
	env := newLikeEnv(t, false, false, false)
	s, tt := env.s, env.tt

	x, y := coverPoint(t, tt)
	tt.Press(x, y)
	advanceLike(env, 600*time.Millisecond)
	tt.Release(x, y)
	tt.Frame()
	if !s.full.open {
		t.Error("按住 600ms 松手之后满屏没开：没有三连了，这一按不许再被吞")
	}
}

// TestTheFullPlayerHasNoThumbButKeepsTheHeart 断满屏左栏：没有「赞」键
// （连带「长按三连」提示和赞数角标），「喜欢」心形还在。
func TestTheFullPlayerHasNoThumbButKeepsTheHeart(t *testing.T) {
	env := favEnv(t)
	s, tt := env.s, env.tt
	s.openFullPlayer()
	tt.Frame()

	if _, ok := tt.Find("喜欢"); !ok {
		t.Fatalf("满屏里找不到「喜欢」心形：它是喜欢歌单的绑定入口，文字有 %q", tt.Texts())
	}
	for _, label := range []string{"赞", "已赞", "收藏", "已收藏"} {
		if _, ok := tt.Find(label); ok {
			t.Errorf("满屏里还摆着「%s」：原型里赞 / 收藏入口已经去掉了，文字有 %q", label, tt.Texts())
		}
	}
	for _, text := range tt.Texts() {
		if strings.Contains(text, "三连") {
			t.Errorf("满屏里还留着「三连」的提示 %q：长按三连已经没有了", text)
		}
	}
}

// TestTheFullPlayerHeartOpensTheFolderSelect 断「喜欢」心形点下去开的是
// 「把这一首放进哪些收藏夹」那个弹窗（喜欢歌单的绑定入口），而不是发一趟
// B 站点赞。
func TestTheFullPlayerHeartOpensTheFolderSelect(t *testing.T) {
	env := favEnv(t)
	s, tt := env.s, env.tt
	s.openFullPlayer()
	tt.Frame()

	if err := tt.Click("喜欢"); err != nil {
		t.Fatalf("点「喜欢」：%v（文字有 %q）", err, tt.Texts())
	}
	waitFrames(t, tt, "收藏夹选择弹窗开出来", func() bool { return tt.HasText("添加到收藏夹") })
	if got := env.f.likePostsCount(); got != 0 {
		t.Errorf("点「喜欢」发了 %d 趟点赞请求：心形是喜欢歌单入口，不是 B 站点赞", got)
	}
}

// TestTheMenuHasNoLikeItem 断菜单「歌曲」栏没有「赞」那一项，
// 「添加到收藏夹…」还在（喜欢歌单的入口）。
func TestTheMenuHasNoLikeItem(t *testing.T) {
	b := newTestShell(t)
	s := b.s
	m := appMenu(s)
	s.menu = m

	// 「歌曲」那一栏是第 5 栏（应用 / 文件 / 编辑 / 播放 / **歌曲** / …）。
	col := m.Items()[4]
	for _, it := range col.Submenu {
		if it.Label == "赞" {
			t.Error("菜单里还有「赞」那一项：播放器上的赞键已经去掉了，菜单上留一个能点的是骗人")
		}
	}
	if it := m.ItemByID(menuFavoriteID); it == nil {
		t.Error("菜单里没有「添加到收藏夹…」那一项：它是喜欢歌单的入口，不该走")
	}
}
