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
