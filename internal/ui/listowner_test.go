package ui

import (
	"strconv"
	"testing"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/list"
)

// 这一组钉住「点列表行里的 UP 名进他的空间」那条路（票 #360）。
//
// 断言的是**路径**：点完之后在 `/user/{mid}` 那儿。而「那一列的字没变」
// （布局没被按钮挤坏）由 `widgets/list` 那边的那几条钉着。

// ownerMidOf 是这一组里那些 UP 的编号。挑几个不像「顺序」的数：真按 1、2、3
// 排的话，「拿到的是另一个编号」这种错可能被当成「拿到的是同一个人的」。
var ownerMidOf = []int64{4242, 777, 31337}

// homeSongsWithOwnerMids 造几行「有 UP 名也有编号」的歌。
//
// ⚠ **编号是行对行给的**：三行的 UP 名和编号都不一样，所以「点第二行进的是
// 第二个人」这种错不会被「点到了第一个」蒙过去。
func homeSongsWithOwnerMids(prefix string, n int) []list.Music {
	out := make([]list.Music, n)
	for i := range out {
		mid := ownerMidOf[i%len(ownerMidOf)]
		name := prefix + " 的 UP 主 " + strconv.Itoa(i+1)
		out[i] = list.Music{
			ID:    prefix + "-" + strconv.Itoa(i),
			Title: songName(prefix, i+1),
			Owner: name,
			Item: app.Item{
				Title:     songName(prefix, i+1),
				Artist:    name,
				ArtistMid: mid,
			},
		}
	}
	return out
}

// TestClickingTheOwnerNameInAListRowGoesIntoThatSpace 钉住「首页推荐那一行的
// UP 名点了进他的空间」。
func TestClickingTheOwnerNameInAListRowGoesIntoThatSpace(t *testing.T) {
	songs := homeSongs{
		tabs: [homeTabCount][]list.Music{0: homeSongsWithOwnerMids("a", 3)},
	}
	s, tt := newHome(t, songs)

	if err := tt.Click("a 的 UP 主 2"); err != nil {
		t.Fatal(err)
	}
	want := upPathOf(ownerMidOf[1])
	if got := s.router.Path(); got != want {
		t.Errorf("点了第二行的 UP 名跳到了 %q，want %q", got, want)
	}
}

// TestClickingTheOwnerNameInALibraryRowGoesIntoThatSpace 钉住「历史记录 / 稍后再看
// 那一行的 UP 名点了进他的空间」。
//
// ⚠ **这两页自己画行**（`viewLibraryRowHead`，见 `internal/ui/librarypage.go`），
// **不走 widgets/list** ——所以只改 widgets/list 那一层的话它们走不到（票 #360
// 的评审查出来的：点完之后路径还是那一页，而选中的是那一行——点击确实落到了
// 那一行上，只是那个名字不是链接）。
func TestClickingTheOwnerNameInALibraryRowGoesIntoThatSpace(t *testing.T) {
	for _, c := range []struct{ path, title string }{
		{laterPath, "稍后再看"},
		{historyPath, "历史记录"},
	} {
		t.Run(c.title, func(t *testing.T) {
			f := newLibFake(t).
				withLater(1, 3, "甲", "乙", "丙").
				withHistory(1, 754, 2700, "甲", "乙", "丙")
			tt, s := libraryShell(t, f, c.path)
			waitFrames(t, tt, "把内容拉回来", func() bool {
				return rowsShown(s.pageOf(c.path)) == 3
			})

			if err := tt.Click("UP 乙"); err != nil {
				t.Fatalf("点不到那一行的 UP 名：%v，文字有 %q", err, tt.Texts())
			}
			// 假 B 站给的那几行 mid 都是 11（`laterBody` / `historyBody`）。
			if got, want := s.router.Path(), upPathOf(11); got != want {
				t.Errorf("点了 UP 名跳到了 %q，want %q", got, want)
			}
		})
	}
}

// TestClickingTheOwnerNameInANewMusicCardGoesIntoThatSpace 钉住「首页「新歌速递」
// 那一块的 UP 名点了进他的空间」。
//
// ⚠ **那一块自己画卡片**（`internal/ui/homepage_newmusic.go` 的 `viewCard`），
// 不走 widgets/list ——同一页里两处 UP 名一处能点一处不能，是票 #360 的评审
// 点出来的那处不一致。
func TestClickingTheOwnerNameInANewMusicCardGoesIntoThatSpace(t *testing.T) {
	var gotMid int64
	var gotName string
	acts := list.Actions{OpenOwner: func(mid int64, name string) {
		gotMid, gotName = mid, name
	}}
	_, tt := newGridTester(t, acts, []list.Music{{
		ID:    "n1",
		Title: "夜空中最亮的星",
		Owner: "逃跑计划",
		Item:  app.Item{ArtistMid: 4242},
	}}, 800, 900)

	if err := tt.Click("逃跑计划"); err != nil {
		t.Fatal(err)
	}
	if gotMid != 4242 || gotName != "逃跑计划" {
		t.Errorf("点 UP 名进的是 %d/%q，want 4242/逃跑计划", gotMid, gotName)
	}
}

// TestClickingTheOwnerNameInASearchResultGoesIntoThatSpace 钉住「搜索结果那一行的
// UP 名点了进他的空间」。
//
// ⚠ **搜索页那一列画的是 UP 名**（不是「用户」那一档的个性签名）——那一档的接线
// 没装 `OpenOwner`，签名点了跳空间是错的。
func TestClickingTheOwnerNameInASearchResultGoesIntoThatSpace(t *testing.T) {
	st := newSearchStub(2, 4)
	s, tt := newSearchPage(t, "周杰伦", st)
	waitFrames(t, tt, "结果出来了", func() bool {
		return tt.HasText("搜索【周杰伦】的结果")
	})
	waitFrames(t, tt, "第一页进来了", func() bool { return loadedTo(s, searchTabSong, 1)() })

	if err := tt.Click("某UP"); err != nil {
		t.Fatalf("点不到结果那一行的 UP 名：%v，文字有 %q", err, tt.Texts())
	}
	if got, want := s.router.Path(), upPathOf(stubMid(1, 0)); got != want {
		t.Errorf("点了 UP 名跳到了 %q，want %q", got, want)
	}
}

// TestClickingTheOwnerNameInACollectionRowGoesIntoThatSpace 钉住「收藏夹 / 合集
// 那一行的 UP 名点了进他的空间」。
func TestClickingTheOwnerNameInACollectionRowGoesIntoThatSpace(t *testing.T) {
	_, s, tt := collectionShell(t, "12345", mediaOf(1, "歌 一"), mediaOf(2, "歌 二"))
	waitSongs(t, tt, s, 2)

	if err := tt.Click("UP 2"); err != nil {
		t.Fatalf("点不到那一行的 UP 名：%v，文字有 %q", err, tt.Texts())
	}
	// 夹具里那条的 upper.mid 是 2（`mediaOf`）。
	if got, want := s.router.Path(), upPathOf(2); got != want {
		t.Errorf("点了 UP 名跳到了 %q，want %q", got, want)
	}
}
