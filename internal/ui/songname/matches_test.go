// 「命中在正文的哪几段」那一族（票 #684）。
//
// # 这一族断的是「区间」，不是「有没有」
//
// `Mentions` 只答「这条评论在不在聊这首歌」——筛选用它就够了。而**高亮**要知道
// 「命中的那几个字在正文的第几个字上」：判据就是评论页要把**那一段**染上底色，
// 多一个字少一个字，画面上都看得出来。
//
// ⚠️ **区间按 rune 算，左闭右开**（票面口径）：正文里中日英混排还有 emoji，
// 按字节切的话 `[]rune(body)[r.Start:r.End]` 会切出半个字。
//
// 🔴 **归一化之后的下标不能直接用**：`normalize` 去空白、转小写（「Let It Go」
// ⇒ "letitgo"）。直接拿归一化串上的下标去切原串，切出来的是别的一段字——
// 见 `TestTheRangeCoversTheSpacesInsideAnEnglishSongName`。
package songname

import (
	"strings"
	"testing"
)

// rangesOf 把命中区间翻成人看得懂的写法（切成原文那几段），报错时用。
func rangesOf(body string, rs []Range) []string {
	runes := []rune(body)
	out := make([]string, 0, len(rs))
	for _, r := range rs {
		if r.Start < 0 || r.End > len(runes) || r.Start > r.End {
			out = append(out, "<越界>")
			continue
		}
		out = append(out, string(runes[r.Start:r.End]))
	}
	return out
}

// TestTheSongNameIsFoundInTheBody 断「一处命中的字符区间」。
func TestTheSongNameIsFoundInTheBody(t *testing.T) {
	tab := New("晴天", "周杰伦")
	got := tab.Matches("晴天真的绝了")
	want := []Range{{Start: 0, End: 2}}
	if len(got) != 1 || got[0] != want[0] {
		t.Fatalf("命中区间是 %v，want %v（切出来是 %v）", got, want,
			rangesOf("晴天真的绝了", got))
	}
	if sub := string([]rune("晴天真的绝了")[got[0].Start:got[0].End]); sub != "晴天" {
		t.Errorf("区间切出来的子串是 %q，want「晴天」", sub)
	}
}

// TestTheBookTitleMarksAreNotPartOfTheRange 断「书名号不在命中区间里」。
//
// ⚠️ **要染的是歌名那五个字**，不是整段「《吹梦到西洲》」：书名号是用户自己
// 加的标点，高亮它会让底色宽出一截。
func TestTheBookTitleMarksAreNotPartOfTheRange(t *testing.T) {
	tab := New("黄诗扶&妖扬《吹梦到西洲》", "黄诗扶")
	body := "《吹梦到西洲》循环一天"
	got := tab.Matches(body)
	want := []Range{{Start: 1, End: 6}}
	if len(got) != 1 || got[0] != want[0] {
		t.Fatalf("命中区间是 %v，want %v（切出来是 %v）", got, want, rangesOf(body, got))
	}
	if sub := string([]rune(body)[got[0].Start:got[0].End]); sub != "吹梦到西洲" {
		t.Errorf("区间切出来的子串是 %q，want「吹梦到西洲」", sub)
	}
}

// TestEveryOccurrenceIsFound 断「一句话里出现两次就是两个区间」。
//
// 🔴 **不是只找第一处**：用户要看全——只染第一处的话，第二处看着像「没命中」，
// 用户会以为筛选结果里混进了不相关的评论。
func TestEveryOccurrenceIsFound(t *testing.T) {
	tab := New("晴天", "周杰伦")
	body := "晴天好听，晴天真棒"
	got := tab.Matches(body)
	want := []Range{{Start: 0, End: 2}, {Start: 5, End: 7}}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("命中区间是 %v，want %v（切出来是 %v）", got, want, rangesOf(body, got))
	}
}

// TestTheRangeCoversTheSpacesInsideAnEnglishSongName 断「英文歌名里的空格算在
// 区间里」。
//
// 🔴 **这是归一化带来那个坑**：标题「Let It Go」归一化之后是 "letitgo"，正文
// 里写的是 "let it go"——按归一化串上的下标去切原串，切出来的会短几个字（或者
// 切到别处）。正确的那一段**包含中间那两个空格**：高亮该覆盖整段「let it go」。
func TestTheRangeCoversTheSpacesInsideAnEnglishSongName(t *testing.T) {
	tab := New("Let It Go", "某个歌手")
	body := "let it go 真好听"
	got := tab.Matches(body)
	want := []Range{{Start: 0, End: 9}}
	if len(got) != 1 || got[0] != want[0] {
		t.Fatalf("命中区间是 %v，want %v（切出来是 %v）", got, want, rangesOf(body, got))
	}
	if sub := string([]rune(body)[got[0].Start:got[0].End]); sub != "let it go" {
		t.Errorf("区间切出来的子串是 %q，want「let it go」", sub)
	}
}

// TestTheArtistIsNotARange 断「只提到歌手就是没有区间」。
func TestTheArtistIsNotARange(t *testing.T) {
	tab := New("周杰伦：晴天", "某个 UP 主")
	if got := tab.Matches("黄诗扶唱得好"); len(got) != 0 {
		t.Errorf("没提到歌名却给出了区间 %v（切出来是 %v）",
			got, rangesOf("黄诗扶唱得好", got))
	}
	if got := tab.Matches("周杰伦唱得好"); len(got) != 0 {
		t.Errorf("只提到歌手却给出了区间 %v（切出来是 %v）",
			got, rangesOf("周杰伦唱得好", got))
	}
}

// TestOverlappingAliasesMergeIntoOneRange 断「重叠/相邻的区间合并」。
//
// 🔴 **不合并的话同一段会被叠两次底色**：别名表里同时有「晴天」和「晴天真好听」
// 的时候，正文「晴天真好听」会命中两处重叠的区间——画面上看不出来（两层一样的
// 底色），但 span 会白白多切几段。
func TestOverlappingAliasesMergeIntoOneRange(t *testing.T) {
	// ⚠️ **直接摆一张表**（同包的测试）：`New` 认出来的别名表里，短的通常不是
	// 长的前缀，而这一条要验的正是那种重叠。
	tab := Table{
		forms: []string{"晴天", "晴天真好听"},
		keys:  []string{"晴天", "晴天真好听"},
	}
	body := "晴天真好听"
	got := tab.Matches(body)
	want := []Range{{Start: 0, End: 5}}
	if len(got) != 1 || got[0] != want[0] {
		t.Fatalf("命中区间是 %v，want 合并成 %v（切出来是 %v）", got, want,
			rangesOf(body, got))
	}
}

// TestAdjacentRangesMerge 断「紧挨着的两段也合并」。
//
// ⚠️ **相邻（End == Start）也算**：两段挨着的底色画出来就是一块，不合并只会
// 多切一段 span。
func TestAdjacentRangesMerge(t *testing.T) {
	tab := Table{forms: []string{"晴天", "真棒"}, keys: []string{"晴天", "真棒"}}
	body := "晴天真棒"
	got := tab.Matches(body)
	want := []Range{{Start: 0, End: 4}}
	if len(got) != 1 || got[0] != want[0] {
		t.Fatalf("命中区间是 %v，want 合并成 %v（切出来是 %v）", got, want,
			rangesOf(body, got))
	}
}

// TestNothingMatchesGivesNoRanges 断「空表 / 空正文 / 没命中都是 nil」。
func TestNothingMatchesGivesNoRanges(t *testing.T) {
	tab := New("晴天", "周杰伦")
	if got := tab.Matches(""); got != nil {
		t.Errorf("空正文给出了 %v，want nil", got)
	}
	if got := New("", "").Matches("晴天"); got != nil {
		t.Errorf("空表给出了 %v，want nil", got)
	}
	if got := tab.Matches("这届年轻人真是身在福中不知福"); got != nil {
		t.Errorf("没命中给出了 %v，want nil", got)
	}
}

// TestTheRangeIsInRunesAroundEmoji 断「区间按 rune 算，混排 emoji 也切得对」。
//
// 🔴 **emoji 是四个字节**：按字节下标切的话 `[]rune(body)` 那一步就错位了——
// 切出来的**子串**会是一个完全不相干的位置。所以这一条的判据是**切出来的
// 子串真的等于歌名**，而不是「区间看起来差不多」。
func TestTheRangeIsInRunesAroundEmoji(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{"emoji 在前", "今天😀晴天🎵真好听", "晴天"},
		{"emoji 在后", "晴天😀🎵真好听", "晴天"},
		{"韩文日文混排", "오늘は晴天ですね😀", "晴天"},
	}
	tab := New("晴天", "周杰伦")
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := tab.Matches(tc.body)
			if len(got) != 1 {
				t.Fatalf("%q 的命中区间是 %v，want 一处（切出来是 %v）",
					tc.body, got, rangesOf(tc.body, got))
			}
			runes := []rune(tc.body)
			sub := string(runes[got[0].Start:got[0].End])
			if sub != tc.want {
				t.Errorf("%q 的区间 %v 切出来是 %q，want %q", tc.body, got[0],
					sub, tc.want)
			}
		})
	}
}

// TestMentionsAgreesWithMatches 断「口径只有一处」。
//
// 🔴 `Mentions` 是 `len(Matches) > 0` 的薄封装：两份判据迟早分叉，而分叉出来的
// 症状是「筛选筛出来了，却没有一处高亮」——两边看着都「对」。
func TestMentionsAgreesWithMatches(t *testing.T) {
	titles := []string{"晴天", "Let It Go", "周杰伦：晴天 (Live)", "【4K修复】官方MV 晴天"}
	bodies := []string{
		"", "晴天", "《晴天》真好听", "letitgo", "LET IT GO",
		"黄诗扶唱得好", "周杰伦", "这届年轻人真是身在福中不知福",
	}
	for _, title := range titles {
		tab := New(title, "某个 UP 主")
		for _, body := range bodies {
			if got, want := tab.Mentions(body), len(tab.Matches(body)) > 0; got != want {
				t.Errorf("标题 %q、正文 %q：Mentions=%v，Matches 有 %d 处",
					title, body, got, len(tab.Matches(body)))
			}
		}
	}
}

// TestMatchesStaysWithinItsAllocationBudget 断「每条评论跑一次的那个函数不
// 乱分配」。
//
// 🔴 **它是每一条评论跑一次的**（排版缓存里排一次，见评论页的 `commentBodyOf`）：
// 500 字 × 5 个别名的正文上跑一趟，分配次数要有上界——不然「开着筛选翻一屏」
// 就是几十次可避免的分配（`framebudget_test.go` 盯着的那一族）。
func TestMatchesStaysWithinItsAllocationBudget(t *testing.T) {
	tab := Table{
		forms: []string{"晴天", "周杰伦", "Let It Go", "吹梦到西洲", "大鱼"},
		keys:  []string{"晴天", "周杰伦", "letitgo", "吹梦到西洲", "大鱼"},
	}
	body := strings.Repeat("这是一条评论，晴天真好听，let it go 也不错。", 26)
	if n := len([]rune(body)); n < 500 {
		t.Fatalf("正文只有 %d 个 rune，够不上「500 字」这一档", n)
	}
	var sink []Range
	allocs := testing.AllocsPerRun(100, func() {
		sink = tab.Matches(body)
	})
	if len(sink) == 0 {
		t.Fatal("这条正文一处都没命中——分配预算就白量了")
	}
	// 实测基线见 `songname.go` 的 `Matches` 注释；上限留一截余量，
	// 但远低于「每处命中一个区间就多分配一次」那一档。
	const budget = 20
	if allocs > budget {
		t.Errorf("每条评论分配 %.1f 次，预算 %d 次——`Matches` 在稳态路径上多分了", allocs, budget)
	}
	t.Logf("500 字 × 5 个别名：每次调用分配 %.1f 次", allocs)
}
