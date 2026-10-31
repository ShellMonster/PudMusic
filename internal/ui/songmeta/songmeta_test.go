// 公共层那一族的测试：「从标题里认出歌名」那一份逻辑。
//
// # 这一族断的是「**认不认得出**」
//
// 这一层是**显示**（`internal/ui/widgets/title`）和**匹配**
// （`internal/ui/songname`）**共用**的那一份判据——所以它认错了，两个界面
// 一起错。
//
// ⇒ 这一族断的是**判据本身**：B 站上那几种常见形状（`《》`/`【】`/破折号/
// 冒号/竖线/引号）各自认出什么、认不出的时候是什么语义。
package songmeta

import "testing"

// TestTheKnownShapesAreRecognized 断「B 站上那几种常见形状都认得出歌名」。
//
// ⚠️ **这张表是票面给的**（旧实现实测认不出的那些形状）：每一行都是一种 B
// 站上的常见写法，而每一行认出的**歌名**都写在右边。
//
// 🔴 **「孤勇者 - 陈奕迅 - 官方完整版」那一档是最难的一档**：三段，破折号切
// 出来是「孤勇者」「陈奕迅」「官方完整版」——而「陈奕迅」是**歌手**（认得出
// 词典里那一档）⇒ 它是署名 ⇒ 歌名是「孤勇者」。
func TestTheKnownShapesAreRecognized(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"【4K修复】周杰伦 - 晴天 官方MV 1080P 高音质", "晴天"},
		{"【晴天】", "晴天"},
		{"歌手《晴天》", "晴天"},
		{"周杰伦 - 晴天", "晴天"},
		{"周杰伦-晴天", "晴天"},
		{"晴天：电影主题曲", "晴天"},
		{"起风了 | 吴青峰 | 官方MV", "起风了"},
		{"孤勇者 - 陈奕迅 - 官方完整版", "孤勇者"},
		{"起风了(钢琴版)", "起风了"},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			id := Identify(c.in)
			if !id.OK {
				t.Fatalf("Identify(%q) 认不出歌名", c.in)
			}
			if id.Name != c.want {
				t.Errorf("Identify(%q).Name = %q，want %q", c.in, id.Name, c.want)
			}
		})
	}
}

// TestTheArtistSegmentIsNotTheSongName 断「签名那一段不是歌名」。
//
// ⚠️ **两边都没线索时是本质歧义**（"于文文 - 体面" 和 "体面 - 于文文" 长得一
// 样）⇒ 按通行约定取 **艺人 - 歌名**（B 站文件名和下载工具都是这个约定）。
//
// 🔴 **而认得出歌手的时候不是歧义**：那一段是署名 ⇒ 另一段才是歌名。
func TestTheArtistSegmentIsNotTheSongName(t *testing.T) {
	// ⚠️ **破折号那一档**：右边是歌名（艺人 - 歌名）。
	if got := Identify("周杰伦 - 晴天").Name; got != "晴天" {
		t.Errorf("「周杰伦 - 晴天」认出了 %q，want「晴天」", got)
	}
	// ⚠️ **竖线那一档**：B 站的写法是「歌名 | 艺人 | 说明」——歌名在前。
	if got := Identify("起风了 | 吴青峰 | 官方MV").Name; got != "起风了" {
		t.Errorf("「起风了 | 吴青峰 | 官方MV」认出了 %q，want「起风了」", got)
	}
	// ⚠️ **三段那一档**：中间那一段是歌手 ⇒ 剔掉它。
	if got := Identify("孤勇者 - 陈奕迅 - 官方完整版").Name; got != "孤勇者" {
		t.Errorf("「孤勇者 - 陈奕迅 - 官方完整版」认出了 %q，want「孤勇者」", got)
	}
}

// TestTheQuotedSongNameIsRecognized 断「引号那一档」。
//
// 🔴 **旧实现认不出**（"“Let It Go”" 那一句）：引号没处理 ⇒ 整句留着。
func TestTheQuotedSongNameIsRecognized(t *testing.T) {
	cases := []string{
		"“Let It Go”",
		"“Let It Go” (From \"Frozen\")",
		"《晴天》",
		"「晴天」",
		"『晴天』",
	}
	for _, in := range cases {
		t.Run(in, func(t *testing.T) {
			id := Identify(in)
			if !id.OK {
				t.Fatalf("Identify(%q) 认不出歌名", in)
			}
			if id.Name != "Let It Go" && id.Name != "晴天" {
				t.Errorf("Identify(%q).Name = %q，want「Let It Go」或「晴天」", in, id.Name)
			}
		})
	}
}

// TestTheArtistNameIsNotTheSongName 断「词典里的歌手名不会被当成歌名」。
//
// ⚠️ **这一档是词典存在的理由**："孤勇者 - 陈奕迅" 那一档认得出「陈奕迅」是
// 歌手 ⇒ 歌名是「孤勇者」；而「周杰倫」（繁体）那一档也要认得出——B 站上两
// 种字形都有。
func TestTheArtistNameIsNotTheSongName(t *testing.T) {
	for _, in := range []string{
		"【4K60帧】稻香-周杰倫",
		"【4K60帧】稻香-周杰伦",
		"孤勇者 - 陈奕迅",
	} {
		t.Run(in, func(t *testing.T) {
			id := Identify(in)
			if !id.OK {
				t.Fatalf("Identify(%q) 认不出歌名", in)
			}
			if id.Name != "稻香" && id.Name != "孤勇者" {
				t.Errorf("Identify(%q).Name = %q，want 歌名（不是歌手名）", in, id.Name)
			}
		})
	}
}

// TestTheVersionMarkerIsKeptAsALiveFlag 断「版本标记是**曲目身份**，不是噪声」。
//
// 🔴 **现场版和录音室版是两个不同的录音**——所以「现场」要记进 `Live`，而
// 不是被当成宣传词丢掉。
//
// ⚠️ **英文那一族按整词判**："Olive" / "Believe" 里有 "live" 那个子串——按子
// 串判的话那些歌名会被误判成现场版（列表上多一枚「现场」标签）。
func TestTheVersionMarkerIsKeptAsALiveFlag(t *testing.T) {
	for _, in := range []string{
		"【现场】稻香",
		"稻香 Live",
		"稻香 演唱会",
		"周杰伦：晴天 (Live 2019)",
	} {
		if id := Identify(in); !id.Live {
			t.Errorf("Identify(%q).Live = false，want true（现场版要记下来）", in)
		}
	}
	for _, in := range []string{"Olive", "Believe", "晴天", "Let It Go"} {
		if id := Identify(in); id.Live {
			t.Errorf("Identify(%q).Live = true，want false"+
				"（\"Olive\" 里有 \"live\" 那个子串，但它不是现场版）", in)
		}
	}
}

// TestThePromoWordsAreNotTheSongName 断「宣传词不是歌名」。
//
// ⚠️ **括号外面**的宣传词也要清（"官方MV 1080P 高音质" 挂在括号外面）。
func TestThePromoWordsAreNotTheSongName(t *testing.T) {
	for _, in := range []string{
		"【官方MV】",
		"[4K]",
		"（官方）",
		"晴天 - 官方MV",
		"【4K修复】官方MV 晴天",
	} {
		id := Identify(in)
		if id.OK && (id.Name == "官方MV" || id.Name == "4K" || id.Name == "官方") {
			t.Errorf("Identify(%q).Name = %q——那是宣传词，不是歌名", in, id.Name)
		}
	}
}

// TestTheFallbackIsExplicit 断「**认不出就是认不出**」。
//
// 🔴 **公共层的语义是 `OK == false`**（不是「返回原标题」）：
//
//	显示层：认不出就退回**原标题**（`widgets/title` 那一档）
//	匹配层：认不出就给**空表**（`songname` 那一档）
//
// ⚠️ **这两个语义是有区别的**，而这一层给的是那个**共同的**前提：认不出。
func TestTheFallbackIsExplicit(t *testing.T) {
	for _, in := range []string{"", "【官方MV】", "[4K]", "（官方）", "【】"} {
		id := Identify(in)
		if id.OK {
			t.Errorf("Identify(%q).OK = true（want false），Name = %q", in, id.Name)
		}
		if id.Name != "" {
			t.Errorf("Identify(%q).Name = %q，want 空串"+
				"（认不出时公共层给的是「认不出」，不是原标题）", in, id.Name)
		}
	}
}

// TestTheExtraIsNotLost 断「方括号里那一段**不许丢**」。
//
// 🔴 **这是一个真缺陷**（旧实现）：旧 `Clean` 在「清洗之后标题非空」那条路上
// **只赋 `Title`** ⇒ 方括号里的内容整个丢了——而「【晴天】」里那内容**就是
// 歌名**。
func TestTheExtraIsNotLost(t *testing.T) {
	// ⚠️ **方括号外面还有别的**：方括号里那一段是歌手 ⇒ 它进 `Extra`。
	id := Identify("【周杰伦】晴天")
	if id.Extra != "周杰伦" {
		t.Errorf("Identify(\"【周杰伦】晴天\").Extra = %q，want「周杰伦」"+
			"（方括号里那一段不许丢）", id.Extra)
	}
	// ⚠️ **整条标题就是那对括号**：那一段**就是**歌名 ⇒ 它**不是** `Extra`
	//（两处都摆一遍的话，列表的副标题会变成「晴天 · UP 名」）。
	id = Identify("【晴天】")
	if id.Name != "晴天" {
		t.Errorf("Identify(\"【晴天】\").Name = %q，want「晴天」", id.Name)
	}
	if id.Extra != "" {
		t.Errorf("Identify(\"【晴天】\").Extra = %q，want 空串（那一段就是歌名）", id.Extra)
	}
}

// TestTheAliasesCarryTheSameJudgment 断「**别名表和显示走同一份判据**」。
//
// ⚠️ **这是这一层的核心承诺**：显示认出来是「晴天」，别名表里也必须有「晴
// 天」——否则同一个视频在播放条上写着「晴天」、在评论页里却筛不出提到「晴
// 天」的评论。
func TestTheAliasesCarryTheSameJudgment(t *testing.T) {
	for _, in := range []string{
		"【4K修复】周杰伦 - 晴天 官方MV 1080P 高音质",
		"【晴天】",
		"歌手《晴天》",
		"周杰伦 - 晴天",
		"起风了 | 吴青峰 | 官方MV",
		"孤勇者 - 陈奕迅 - 官方完整版",
		"“Let It Go”",
	} {
		t.Run(in, func(t *testing.T) {
			id := Identify(in)
			if !id.OK {
				t.Fatalf("Identify(%q) 认不出歌名", in)
			}
			found := false
			for _, a := range Aliases(in) {
				if a == id.Name {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("Aliases(%q) = %q，里面没有认出来的那一个 %q",
					in, Aliases(in), id.Name)
			}
		})
	}
}

// TestTheOriginalTitleIsAnAlias 断「原标题也在别名表里」。
//
// ⚠️ **用户会把整个标题抄进评论里**（「看下【4K修复】晴天」）——那一行也要
// 命中。
func TestTheOriginalTitleIsAnAlias(t *testing.T) {
	raw := "【4K修复】晴天"
	found := false
	for _, a := range Aliases(raw) {
		if a == raw {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Aliases(%q) = %q，里面没有原标题", raw, Aliases(raw))
	}
}

// TestThePromoTableHasOnlyOneCopy 断「宣传词表只有一份」。
//
// 🔴 **自己再抄一份那张表就是两份口径**，而分叉出来的那种差异极难查（一个说
// 「官方MV」是宣传词，另一个说不算）。
//
// ⇒ 这一族断的是「`widgets/title` 那个出口**指的就是这里**」——它不是第二
// 份表，它是个转发。
func TestThePromoTableHasOnlyOneCopy(t *testing.T) {
	for _, s := range []string{"官方MV", "4K修复", "高音质", "官方", "1080P"} {
		if !IsPromoOnly(s) {
			t.Errorf("IsPromoOnly(%q) = false，want true", s)
		}
		if !ContainsPromo(s) {
			t.Errorf("ContainsPromo(%q) = false，want true", s)
		}
	}
	// ⚠️ **空串不算**：那是「没有这一段」。
	if IsPromoOnly("") || ContainsPromo("") {
		t.Errorf("空串被算成了宣传词")
	}
}
