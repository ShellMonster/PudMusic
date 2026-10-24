package list

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/egoist/mygo/ui"
)

// 这一组验的是「点列表行里的 UP 名进他的空间」那条路（票 #360）。
//
// 断言面是**行为**：点了 UP 名，`OpenOwner` 收到的是**那个 UP 的编号**。
// 而「名字还在不在」「点了会不会崩」是另外两件事，各有各的判据。

// ownerMid 是这一组里那些歌的 UP 编号。挑一个不像「顺序」的数：真的按 1、2、3
// 排的话，「拿到的是另一个编号」这种错可能被当成「拿到的是同一个人的」。
const ownerMid = 4242

// ownerActs 是一份接线：它把「进那个 UP 的空间」记下来。
//
// ⚠ **它记下的是编号和名字两样**：提示里说的是名字（`OwnerTipText`），而导航
// 用的是编号——两样都要对得上，才是真的进了**那个人**的空间。
func ownerActs(got *[]string) Actions {
	return Actions{OpenOwner: func(mid int64, name string) {
		*got = append(*got, strconv.FormatInt(mid, 10)+"|"+name)
	}}
}

// ownerSong 是一行「有 UP 名也有编号」的歌。
func ownerSong(id, title, owner string) Music {
	return Music{ID: id, Title: title, Owner: owner, Item: app.Item{Artist: owner, ArtistMid: ownerMid}}
}

// TestClickingTheOwnerNameOpensThatUpSpace 钉住「点列表行里的 UP 名进他的空间」。
func TestClickingTheOwnerNameOpensThatUpSpace(t *testing.T) {
	var got []string
	_, tt := newList(t, ownerActs(&got), []Music{
		ownerSong("a", "夜空中最亮的星", "逃跑计划"),
	})

	if err := tt.Click("逃跑计划"); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("点了一下 UP 名，却收到 %d 次「进空间」，want 1 次：%v", len(got), got)
	}
	if want := "4242|逃跑计划"; got[0] != want {
		t.Errorf("进的是 %q，want %q", got[0], want)
	}
}

// TestClickingTheOwnerNameInACompactRowOpensThatUpSpace 钉住紧凑那一档也一样。
//
// ⚠ **紧凑那一档是另一段画法**（`card.go` 的 `viewCompactCard`），不是上面那一段
// 少画几样——所以「上面那条绿了就当两档都对」是不成立的。
func TestClickingTheOwnerNameInACompactRowOpensThatUpSpace(t *testing.T) {
	var got []string
	l := New(newCache(t), ownerActs(&got))
	l.SetView(ViewCompact)
	l.SetSongs([]Music{ownerSong("a", "夜空中最亮的星", "逃跑计划")})
	tt := ui.NewTester(l.View, testerW, testerH)
	tt.Frame()

	if err := tt.Click("逃跑计划"); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("点了一下 UP 名，却收到 %d 次「进空间」，want 1 次：%v", len(got), got)
	}
	if want := "4242|逃跑计划"; got[0] != want {
		t.Errorf("进的是 %q，want %q", got[0], want)
	}
}

// TestClickingTheOwnerNameInAGridCellOpensThatUpSpace 钉住网格那一格也一样。
//
// ⚠ **网格那一格自己带一个 `OnClick`**（点一下选中那一首，`grid.go` 的
// `viewCell`）：所以这里验的是「点 UP 名进的是**空间**，而不是把那一格选中」。
func TestClickingTheOwnerNameInAGridCellOpensThatUpSpace(t *testing.T) {
	var got []string
	_, tt := newGrid(t, ownerActs(&got), []Music{
		ownerSong("a", "夜空中最亮的星", "逃跑计划"),
	}, 800, gridH)

	if err := tt.Click("逃跑计划"); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("点了一下 UP 名，却收到 %d 次「进空间」，want 1 次：%v", len(got), got)
	}
	if want := "4242|逃跑计划"; got[0] != want {
		t.Errorf("进的是 %q，want %q", got[0], want)
	}
}

// TestAnOwnerWithoutAMidIsStillShown 钉住「没有编号时名字照旧显示」。
//
// ⚠ **不许因为「点不动」就把名字藏起来**（AGENTS.md §4）：B 站有的接口只给名字
// 不给编号，那种情况下用户看到的是「这一行没有 UP 名」——而真实原因是这一页
// 没给编号。
func TestAnOwnerWithoutAMidIsStillShown(t *testing.T) {
	var got []string
	_, tt := newList(t, ownerActs(&got), []Music{
		{ID: "a", Title: "夜空中最亮的星", Owner: "逃跑计划"},
	})

	if !tt.HasText("逃跑计划") {
		t.Errorf("没有编号时 UP 名没画出来，文字有 %q", tt.Texts())
	}
}

// TestAnOwnerWithoutAMidDoesNothing 钉住「没有编号时点了没反应」。
//
// ⚠ **「没反应」是这一条要的**：不崩、不弹窗、不导航。名字还在（上一条钉着），
// 而那一行别的部分照旧能用。
func TestAnOwnerWithoutAMidDoesNothing(t *testing.T) {
	var got []string
	_, tt := newList(t, ownerActs(&got), []Music{
		{ID: "a", Title: "夜空中最亮的星", Owner: "逃跑计划"},
	})

	if err := tt.Click("逃跑计划"); err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("没有编号的那一行点了却进了空间：%v", got)
	}
	if !tt.HasText("逃跑计划") {
		t.Errorf("点完之后名字不见了，文字有 %q", tt.Texts())
	}
}

// TestAnUnwiredListKeepsItsOwnerNames 钉住「没接上导航时名字照旧显示」。
//
// ⚠ **接线是可选的**（`Actions.OpenOwner` 为 nil 时是「按了没反应」，不是
// 「报错」——见 `actions.go` 的 `Actions` 注释）。而**那一行不许因此少一个字**。
func TestAnUnwiredListKeepsItsOwnerNames(t *testing.T) {
	_, tt := newList(t, Actions{}, []Music{
		ownerSong("a", "夜空中最亮的星", "逃跑计划"),
	})

	if !tt.HasText("逃跑计划") {
		t.Errorf("没接上导航时 UP 名没画出来，文字有 %q", tt.Texts())
	}
	if err := tt.Click("逃跑计划"); err != nil {
		t.Fatal(err)
	}
	if !tt.HasText("逃跑计划") {
		t.Errorf("点了一下名字就没了，文字有 %q", tt.Texts())
	}
}

// TestALongOwnerNameStaysInItsColumn 钉住「那一列的字没变」。
//
// ⚠ **UP 名那一列有一个宽度上限**（`listTitleMaxW`）：一个很长的 UP 名要**收成
// 省略号**，而不是一路顶到右边那几列跟前（而那几列是逐行对齐的）。可点的那一枚
// 比普通字**多了一层按钮**，而按钮默认**不收缩**——所以这一条盯的是「加了按钮
// 之后上限还管不管用」。
func TestALongOwnerNameStaysInItsColumn(t *testing.T) {
	long := strings.Repeat("很长的UP主名字", 12)
	_, tt := newList(t, Actions{}, []Music{
		ownerSong("a", "夜空中最亮的星", long),
	})

	r, ok := tt.Find(long)
	if !ok {
		t.Fatalf("画面上找不到那个 UP 名，文字有 %q", tt.Texts())
	}
	if r.W > listTitleMaxW+1 {
		t.Errorf("那个 UP 名占了 %.1f 宽，want ≤ %d——它该收成省略号的",
			r.W, listTitleMaxW)
	}
}

// TestTheOwnerNameSitsWhereItSat 钉住「那一列的字没变」。
//
// ⚠ **可点的那一枚比普通字多了一层按钮**，而扁平按钮那份配方默认带内边距
// （6/14）：留着的话那句话会往右挪一截——于是「那一列的字」就不是原来那一列
// 的字了（而那种错在画面上只是「字歪了一点点」）。
//
// 判据是**两个盒子一模一样**：一个画成可点的那一枚、一个画成普通字（没有编号
// 走的就是那一支），位置和大小都得一样。
func TestTheOwnerNameSitsWhereItSat(t *testing.T) {
	const owner = "逃跑计划"

	var got []string
	_, tt := newList(t, ownerActs(&got), []Music{ownerSong("a", "夜空中最亮的星", owner)})
	btn, ok := tt.Find(owner)
	if !ok {
		t.Fatalf("画面上找不到那个 UP 名，文字有 %q", tt.Texts())
	}

	// 普通字：同样的歌，只是没有编号（那一支走的是 `ownerLine` 里的另一句）。
	_, tt2 := newList(t, Actions{}, []Music{{ID: "b", Title: "夜空中最亮的星", Owner: owner}})
	plain, ok := tt2.Find(owner)
	if !ok {
		t.Fatalf("画面上找不到那个 UP 名，文字有 %q", tt2.Texts())
	}

	if btn.X != plain.X {
		t.Errorf("可点的那一枚在 x=%.1f，普通字在 x=%.1f——多出来的那一层按钮把字挤走了",
			btn.X, plain.X)
	}
	if btn.W != plain.W {
		t.Errorf("可点的那一枚占了 %.1f 宽，普通字占了 %.1f 宽", btn.W, plain.W)
	}
}

// TestAnOwnerNameThatIsOnlyTheTitlesArtistIsNotALink 钉住「只有标题里挪过来的
// 那个歌手名时**不是**链接」。
//
// ⚠ **这一条是票 #360 的评审查出来的**：UP 空间「投稿」那一档每一行画的是
// 「周杰倫」这种（标题里【】挪过来的），而那一页**压根没有 UP 名**（名字在
// 页头，每一行都是同一个 UP）。
//
// 把它画成可点的，提示就成了「打开 的空间」（名字空着），点它还会跳到
// **编号指的那个人**——而那一页就是那个人的空间：**标签和目的地对不上**。
func TestAnOwnerNameThatIsOnlyTheTitlesArtistIsNotALink(t *testing.T) {
	var got []string
	_, tt := newList(t, ownerActs(&got), []Music{
		// ⚠ **Owner 是空的，而编号有**（投稿那一档就是这样）。
		{ID: "a", Title: "【周杰倫】某首歌", Item: app.Item{ArtistMid: 42}},
	})

	if !tt.HasText("周杰倫") {
		t.Fatalf("那个歌手名没画出来，文字有 %q", tt.Texts())
	}
	if err := tt.Click("周杰倫"); err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("点了那个歌手名却进了空间：%v——它不是 UP 名", got)
	}

	// 悬停上去也**不该有那句提示**（「打开 的空间」——名字空着）。
	r, ok := tt.Find("周杰倫")
	if !ok {
		t.Fatalf("画面上找不到那个歌手名，文字有 %q", tt.Texts())
	}
	tt.Move(r.X+r.W/2, r.Y+r.H/2)
	tt.Advance(700 * time.Millisecond)
	tt.Frame()
	if tt.HasText("的空间") {
		t.Errorf("悬停出了那句提示，文字有 %q——它不是 UP 名", tt.Texts())
	}
}

// TestTheOwnerNameSaysWhichSpaceItOpens 钉住「悬停上去那句话说的是**那个人**」。
//
// ⚠ **提示里是那个人的名字**，不是画出来的那一句（画出来的那一句可能是清洗过
// 的「歌手 · UP 名」，见 `showOf`）。
func TestTheOwnerNameSaysWhichSpaceItOpens(t *testing.T) {
	var got []string
	_, tt := newList(t, ownerActs(&got), []Music{
		ownerSong("a", "夜空中最亮的星", "逃跑计划"),
	})

	r, ok := tt.Find("逃跑计划")
	if !ok {
		t.Fatalf("画面上找不到那个 UP 名，文字有 %q", tt.Texts())
	}
	tt.Move(r.X+r.W/2, r.Y+r.H/2)
	tt.Advance(700 * time.Millisecond)
	tt.Frame()
	if !tt.HasText("打开 逃跑计划 的空间") {
		t.Errorf("悬停上去没有那句话，文字有 %q", tt.Texts())
	}
}

// TestTheOwnerTipSaysTheSameThingEverywhere 钉住那句提示的写法只有一处。
//
// ⚠ **播放条上那一枚用的是同一个函数**（`internal/ui/playbar.go` 的
// `viewArtist`）：两处各写一遍的话，改了一处另一处就安静地变成另一句话。
func TestTheOwnerTipSaysTheSameThingEverywhere(t *testing.T) {
	if got, want := OwnerTipText("逃跑计划"), "打开 逃跑计划 的空间"; got != want {
		t.Errorf("提示是 %q，want %q", got, want)
	}
}
