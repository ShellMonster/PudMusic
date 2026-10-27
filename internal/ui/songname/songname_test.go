// 「只看正文里提到这首歌」那一档的别名表（票 #574）。
//
// # 这一族断的是「哪些写法算提到了这首歌」
//
// 别名表是**一张表，一处算**（`internal/ui/songname`）：界面层只调它，不自己
// 再拼一份歌名或别名——两份口径迟早分叉，而分叉出来的那种差异极难查（一个
// 界面上筛出三条，另一个筛出五条，而两边看着都「对」）。
//
// ⇒ 这一族断的是那张表的**内容**：常见标题形状（【晴天】/「歌手 - 歌名」/
// 紧凑连字符/冒号）里歌名认不认得出来、歌手那一段在不在里面、宣传词那一段
// 在不在里面，以及「一个字的歌名」收不收。
package songname

import (
	"testing"
)

// has 报表里有没有这一条写法（比的是**归一化之后**的形式）。
func has(t *testing.T, tab Table, want string) bool {
	t.Helper()
	for _, f := range tab.Forms() {
		if normalize(f) == normalize(want) {
			return true
		}
	}
	return false
}

// TestTheCleanedSongNameIsInTheTable 断「歌名在表里」。
//
// 🔴 **歌名那一栏是清洗过的那一份**（`widgets/title` 那一套）：B 站的标题里
// 全是视频的痕迹（`【4K修复】`、`官方MV`），而用户在评论里写的是「晴天」。
func TestTheCleanedSongNameIsInTheTable(t *testing.T) {
	tab := New("【4K修复】官方MV 晴天", "某个 UP 主")
	if !has(t, tab, "晴天") {
		t.Errorf("表里没有「晴天」：%q", tab.Forms())
	}
	// ⚠️ **视频痕迹不进表**：那些是「这个视频」的痕迹，不是歌名。
	if has(t, tab, "【4K修复】") {
		t.Errorf("表里有「【4K修复】」——那是视频的痕迹，不是歌名：%q", tab.Forms())
	}
}

// TestThePlainTitleIsAlsoInTheTable 断「原标题也在表里」。
//
// ⚠️ **用户会把整个标题抄进评论里**（「看下【4K修复】晴天」）——那一行也要
// 命中。
func TestThePlainTitleIsAlsoInTheTable(t *testing.T) {
	tab := New("【4K修复】晴天", "某个 UP 主")
	if !has(t, tab, "【4K修复】晴天") {
		t.Errorf("表里没有原标题：%q", tab.Forms())
	}
}

// TestTheBookTitleMarksAreStripped 断「《晴天》也算提到」。
func TestTheBookTitleMarksAreStripped(t *testing.T) {
	tab := New("《晴天》", "某个歌手")
	if !has(t, tab, "晴天") {
		t.Errorf("表里没有去掉书名号的「晴天」：%q", tab.Forms())
	}
}

// TestTheShortNameOfAPlainTitleIsInTheTable 断「简称在表里」。
//
// ⚠️ **「歌手 - 歌名」那种标题**是 B 站上最常见的形状，而用户在评论里写的是
// 「晴天」，不是「周杰伦 - 晴天」。
func TestTheShortNameOfAPlainTitleIsInTheTable(t *testing.T) {
	tab := New("周杰伦 - 晴天", "某个 UP 主")
	if !has(t, tab, "晴天") {
		t.Errorf("表里没有简称「晴天」：%q", tab.Forms())
	}
}

// TestTheArtistSegmentIsNotAnAlias 断「破折号前那一段是署名，不进表」。
//
// 🔴 **提到歌手不等于提到这首歌**（票 #574 的评审）：`mv` 那一档的艺人栏是
// **UP 主**，跟标题里的歌手没关系——所以认不出歌手的时候也不能把署名当歌名。
func TestTheArtistSegmentIsNotAnAlias(t *testing.T) {
	tab := New("周杰伦 - 晴天", "某个 UP 主")
	if has(t, tab, "周杰伦") {
		t.Errorf("表里有「周杰伦」——那是署名，不是歌名：%q", tab.Forms())
	}
	if tab.Mentions("周杰伦") {
		t.Errorf("正文里只有「周杰伦」却命中了")
	}
	if !tab.Mentions("晴天真好听") {
		t.Errorf("正文里明明有「晴天」，却没命中")
	}
}

// TestTheArtistSegmentIsNotAnAliasWhenTheArtistIsKnown 断「认得出歌手的时候，
// 歌手那一段不进表」。
//
// 🔴 **评论里写「周杰伦」不等于提到「晴天」**：那是提到了歌手。
func TestTheArtistSegmentIsNotAnAliasWhenTheArtistIsKnown(t *testing.T) {
	tab := New("周杰伦 - 晴天", "周杰伦")
	if has(t, tab, "周杰伦") {
		t.Errorf("表里有「周杰伦」——歌手那一段不是歌名：%q", tab.Forms())
	}
	if !has(t, tab, "晴天") {
		t.Errorf("表里没有「晴天」：%q", tab.Forms())
	}
}

// TestAPromoSegmentIsNotAnAlias 断「宣传词那一段不进表」。
//
// ⚠️ **宣传词那张表只有一处**（`widgets/title` 里那张），别处不抄第二份——
// 而「晴天 - 官方MV」那种标题里，「官方MV」那一段不是歌名。
func TestAPromoSegmentIsNotAnAlias(t *testing.T) {
	tab := New("晴天 - 官方MV", "某个 UP 主")
	if has(t, tab, "官方MV") {
		t.Errorf("表里有「官方MV」——那是宣传词，不是歌名：%q", tab.Forms())
	}
	if !has(t, tab, "晴天") {
		t.Errorf("表里没有「晴天」：%q", tab.Forms())
	}
}

// TestTheBracketedSongNameIsInTheTable 断「【晴天】」那一档。
//
// 🔴 **方括号里那一段有时候就是歌名**（票 #574 的评审：「New("【晴天】") 只
// 认出了「【晴天】」，正文里提到「晴天」的那一条被判成没提到」）。
func TestTheBracketedSongNameIsInTheTable(t *testing.T) {
	tab := New("【晴天】", "某个 UP 主")
	if !has(t, tab, "晴天") {
		t.Errorf("表里没有「晴天」：%q", tab.Forms())
	}
	if !tab.Mentions("晴天太好听了") {
		t.Errorf("正文里明明有「晴天」，却没命中")
	}
}

// TestTheBracketHoldsTheArtistWhenThereIsMoreThanIt 断「【周杰伦】晴天」那一档
// 里方括号装的是歌手。
//
// 🔴 **方括号里是歌手的时候不能把它当歌名**：那会让「评论里写「周杰伦」」
// 被算成「提到了这首歌」。
func TestTheBracketHoldsTheArtistWhenThereIsMoreThanIt(t *testing.T) {
	tab := New("【周杰伦】晴天", "某个 UP 主")
	if has(t, tab, "周杰伦") {
		t.Errorf("表里有「周杰伦」——那一段是歌手，不是歌名：%q", tab.Forms())
	}
	if !has(t, tab, "晴天") {
		t.Errorf("表里没有「晴天」：%q", tab.Forms())
	}
}

// TestTheCompactDashTitleGivesTheSongName 断「周杰伦-晴天」那一档。
//
// 🔴 **紧凑的连字符也是分隔符**（票 #574 的评审：「New("周杰伦-晴天") 只认出
// 了整个标题」）。
func TestTheCompactDashTitleGivesTheSongName(t *testing.T) {
	tab := New("周杰伦-晴天", "某个 UP 主")
	if !has(t, tab, "晴天") {
		t.Errorf("表里没有「晴天」：%q", tab.Forms())
	}
	if has(t, tab, "周杰伦") {
		t.Errorf("表里有「周杰伦」——那是署名，不是歌名：%q", tab.Forms())
	}
	if !tab.Mentions("晴天真好听") {
		t.Errorf("正文里明明有「晴天」，却没命中")
	}
}

// TestTheColonTitleGivesTheSongName 断「晴天：电影主题曲」那一档。
//
// ⚠️ **全角和半角两种冒号都算**（B 站上两种都有）。
//
// 🔴 **这一档右边是说明**（"电影主题曲"）⇒ 歌名在**左边**那一段。
func TestTheColonTitleGivesTheSongName(t *testing.T) {
	for _, title := range []string{"晴天：电影主题曲", "晴天:电影主题曲"} {
		tab := New(title, "某个 UP 主")
		if !has(t, tab, "晴天") {
			t.Errorf("标题 %q 的表里没有「晴天」：%q", title, tab.Forms())
		}
		if !tab.Mentions("晴天的前奏一响就泪目") {
			t.Errorf("标题 %q：正文里明明有「晴天」，却没命中", title)
		}
	}
}

// TestTheColonTitleDoesNotTakeTheArtistSegment 断「歌手：歌名」那一档。
//
// 🔴 **歌手那一段不进表**（票 #574 的第二轮评审：「New("周杰伦：晴天") 的表里
// 有「周杰伦」，于是正文只提到歌手的评论也被算成提到了这首歌」）。
//
// ⇒ 这一档右边**不是**说明 ⇒ 歌名在**右边**那一段，左边那一段（署名）丢掉。
func TestTheColonTitleDoesNotTakeTheArtistSegment(t *testing.T) {
	tab := New("周杰伦：晴天", "某个 UP 主")
	if has(t, tab, "周杰伦") {
		t.Errorf("表里有「周杰伦」——那是署名，不是歌名：%q", tab.Forms())
	}
	if !has(t, tab, "晴天") {
		t.Errorf("表里没有「晴天」：%q", tab.Forms())
	}
	if tab.Mentions("「周杰伦」这一段我循环了一整天") {
		t.Errorf("正文里只有「周杰伦」却命中了——提到歌手不等于提到这首歌")
	}
	if !tab.Mentions("「晴天」这一段我循环了一整天") {
		t.Errorf("正文里明明有「晴天」，却没命中")
	}
}

// TestTheCompactDashAndColonTitleGivesTheSongName 断「歌手-歌名：说明」那一
// 档。
//
// 🔴 **两种分隔符挨在一起的时候要先切外层的**（票 #574 的第二轮评审：
// 「周杰伦-晴天：电影主题曲」那一档只认出了「晴天：电影主题曲」，于是正文里
// 提到「晴天」的那一条被判成没提到，界面显示零命中）。
func TestTheCompactDashAndColonTitleGivesTheSongName(t *testing.T) {
	tab := New("周杰伦-晴天：电影主题曲", "某个 UP 主")
	if !has(t, tab, "晴天") {
		t.Errorf("表里没有「晴天」：%q", tab.Forms())
	}
	if has(t, tab, "周杰伦") {
		t.Errorf("表里有「周杰伦」——那是署名，不是歌名：%q", tab.Forms())
	}
	if has(t, tab, "晴天：电影主题曲") {
		t.Errorf("表里有「晴天：电影主题曲」——那一段是歌名加说明，不是歌名：%q",
			tab.Forms())
	}
	if !tab.Mentions("「晴天」这一段我循环了一整天") {
		t.Errorf("正文里明明有「晴天」，却没命中")
	}
	if tab.Mentions("「周杰伦」这一段我循环了一整天") {
		t.Errorf("正文里只有「周杰伦」却命中了——提到歌手不等于提到这首歌")
	}
}

// TestTheSongNameIsNotSwallowedByAVersionNote 断「歌名：说明」那一档不把歌名
// 一起吞掉。
//
// ⚠️ **说明那一族收得紧**：「告白气球新版」「钢琴家的告白」那样的歌名**含
// 着**「版」「钢琴」，而它们是歌名的一部分——所以「说明」那一族只收「说明自
// 己」的那些词，不收可能出现在歌名里的那些。
func TestTheSongNameIsNotSwallowedByAVersionNote(t *testing.T) {
	for _, title := range []string{"周杰伦：告白气球新版", "歌手：钢琴家的告白"} {
		tab := New(title, "某个 UP 主")
		if tab.Empty() {
			t.Errorf("标题 %q 给出了空表", title)
		}
		if has(t, tab, "周杰伦") || has(t, tab, "歌手") {
			t.Errorf("标题 %q 的表里有署名那一段：%q", title, tab.Forms())
		}
	}
}

// TestTheEnglishTitleIsNotSplit 断「英文标题的空格是词的一部分」。
//
// 🔴 **"Let It Go" 拆成 "Go" 之后，「good」那种评论也会命中**——所以英文标题
// 不拆。
func TestTheEnglishTitleIsNotSplit(t *testing.T) {
	tab := New("Let It Go", "某个歌手")
	if has(t, tab, "Go") {
		t.Errorf("表里有「Go」——英文标题的空格是词的一部分：%q", tab.Forms())
	}
	if !has(t, tab, "Let It Go") {
		t.Errorf("表里没有整个「Let It Go」：%q", tab.Forms())
	}
}

// TestOneCharacterAliasesAreNotUsed 断「一个字的**拉丁字母**别名不收」。
//
// 🔴 **一个字母的标题会命中满屏的评论**：正文里到处是那个字母，而筛出来的那些
// 跟「提到这首歌」没关系。
func TestOneCharacterAliasesAreNotUsed(t *testing.T) {
	tab := New("A", "某个 UP 主")
	if !tab.Empty() {
		t.Errorf("一个字母的标题进了表：%q", tab.Forms())
	}
	if tab.Mentions("这是一个 A") {
		t.Errorf("一个字母的别名命中了正文")
	}
}

// TestOneCharacterChineseSongNamesAreUsable 断「一个字的**汉字**歌名收」。
//
// 🔴 **《鱼》《光》那样的歌名是真的**（票 #574 的评审：「New("鱼", "周深") 给出
// 了空表，整个开关被按灰了」）。
func TestOneCharacterChineseSongNamesAreUsable(t *testing.T) {
	tab := New("鱼", "周深")
	if tab.Empty() {
		t.Fatalf("一个字的汉字歌名给出了空表")
	}
	if !has(t, tab, "鱼") {
		t.Errorf("表里没有「鱼」：%q", tab.Forms())
	}
	if !tab.Mentions("这条鱼真好吃") {
		t.Errorf("正文里明明有「鱼」，却没命中")
	}
}

// TestAnEmptyTitleGivesAnEmptyTable 断「没有名字就是空表」。
//
// ⚠️ **空表不能拿来筛**：那样会把评论全筛掉，而界面上一句话都说不清为什么。
func TestAnEmptyTitleGivesAnEmptyTable(t *testing.T) {
	tab := New("", "")
	if !tab.Empty() {
		t.Errorf("空标题给出了表：%q", tab.Forms())
	}
	if tab.Mentions("随便什么") {
		t.Errorf("空表命中了正文")
	}
}

// TestTheBodyIsMatched 断「正文里提到就是命中」。
func TestTheBodyIsMatched(t *testing.T) {
	tab := New("晴天", "周杰伦")
	if !tab.Mentions("晴天的前奏一响就泪目") {
		t.Errorf("正文里明明有「晴天」，却没命中")
	}
	if tab.Mentions("这届年轻人真是身在福中不知福") {
		t.Errorf("正文里没有「晴天」，却命中了")
	}
}

// TestTheMatchIgnoresCaseAndSpaces 断「大小写和空格不算」。
//
// ⚠️ **英文歌名常被写成连着的**（「letitgo」），而中文标题里的空格也常是可有
// 无的（「大 鱼」）。
func TestTheMatchIgnoresCaseAndSpaces(t *testing.T) {
	tab := New("Let It Go", "某个歌手")
	if !tab.Mentions("letitgo!!!") {
		t.Errorf("「letitgo」没命中「Let It Go」")
	}
	if !tab.Mentions("LET IT GO") {
		t.Errorf("「LET IT GO」没命中「Let It Go」")
	}
	tab2 := New("大 鱼", "某个歌手")
	if !tab2.Mentions("大鱼真好听") {
		t.Errorf("「大鱼」没命中「大 鱼」")
	}
}
