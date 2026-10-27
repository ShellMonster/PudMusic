// 「只看正文里提到这首歌」那一档的别名表（票 #574）。
//
// # 这一族断的是「哪些写法算提到了这首歌」
//
// 别名表是**一张表，一处算**（`internal/ui/songname`）：界面层只调它，不自己
// 再拼一份歌名或别名——两份口径迟早分叉，而分叉出来的那种差异极难查（一个
// 界面上筛出三条，另一个筛出五条，而两边看着都「对」）。
//
// ⇒ 这一族断的是那张表的**内容**：歌名在不在里面、歌手那一段在不在里面、
// 宣传词那一段在不在里面，以及「一个字的别名」收不收。
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

// TestOneCharacterAliasesAreNotUsed 断「一个字的别名不收」。
//
// 🔴 **一个字的歌名会命中满屏的评论**：正文里到处是那个字，而筛出来的那些跟
// 「提到这首歌」没关系。
func TestOneCharacterAliasesAreNotUsed(t *testing.T) {
	tab := New("A", "某个 UP 主")
	if !tab.Empty() {
		t.Errorf("一个字的歌名进了表：%q", tab.Forms())
	}
	if tab.Mentions("这是一个 A") {
		t.Errorf("一个字的别名命中了正文")
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
// 可无的（「大 鱼」）。
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
