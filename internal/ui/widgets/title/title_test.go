package title

import "testing"

func TestClean(t *testing.T) {
	cases := []struct {
		in    string
		title string
		extra string
		live  bool
	}{
		{"晴天", "晴天", "", false},
		{"【4K修复】晴天", "晴天", "", false},
		{"晴天【官方MV】", "晴天", "", false},
		{"【周杰伦】晴天", "晴天", "周杰伦", false},
		{"【周杰伦】【4K】晴天 (官方MV)", "晴天", "周杰伦", false},
		{"[Hi-Res]夜曲", "夜曲", "", false},
		{"七里香（4K修复）", "七里香", "", false},
		{"七里香（Live）", "七里香", "", true},
		{"【现场】稻香", "稻香", "", true},
		// ⚠️ **这一档是「认得出歌名」**：旧实现只处理括号里的东西，括号**外面**
		// 的「2023 演唱会」没人清 ⇒ 整句留在标题里 ⇒ 界面上写着「周杰伦 2023
		// 演唱会 稻香」而不是「稻香」。
		//
		// 🔴 **「演唱会」是版本标记**（现场版），它不是歌名的一部分 ⇒ 歌名是
		// 「稻香」，而「这是现场版」记在 `Live` 那一档上。
		{"周杰伦 2023 演唱会 稻香", "稻香", "", true},
		// ⚠️ **这一档是「破折号 / 竖线」**：旧实现没处理 ⇒ 整句留着。
		// 现在歌名是「一路向北」（B 站文件名和下载工具都是**艺人 - 歌名**那个
		// 约定，所以破折号**右边**那一段是歌名；竖线那一档是**歌名 | 艺人 | 说
		// 明**，歌名在前）。
		{"一路向北 - 周杰伦 | 官方MV", "一路向北", "", false},
		{"【4K】", "【4K】", "", false},
		{"", "", "", false},
		// ⚠️ **书名号那一档**：里面那一段**就是歌名**（而方括号里装的是歌手）。
		// 旧实现不认书名号 ⇒ 整句留着。
		{"《千里之外》（feat. 费玉清）", "千里之外", "", false},
	}
	for _, c := range cases {
		got := Clean(c.in)
		if got.Title != c.title || got.Extra != c.extra || got.Live != c.live {
			t.Errorf("Clean(%q) = %+v，want {%q %q live=%v}", c.in, got, c.title, c.extra, c.live)
		}
	}
}

// 清洗只管显示：去不掉的就原样留着，绝不返回空串。
func TestCleanNeverReturnsEmptyForANonEmptyTitle(t *testing.T) {
	for _, in := range []string{"【官方MV】", "[4K]", "（官方）", "【】"} {
		if got := Clean(in); got.Title == "" {
			t.Errorf("Clean(%q) 返回了空标题", in)
		}
	}
}

// TestCleanFallsBackToTheRawTitle 断「**认不出就退回原标题**」。
//
// 🔴 **这是显示层的语义**（公共层给的是 `OK == false`，而这一层要的是一个**永
// 远非空**的显示串）——宁可显示脏标题，也不显示空白。
//
// ⚠️ **而匹配层是另一个语义**（`songname`：认不出给**空表**）——两个语义不
// 一样，但代码和注释里都写清楚了（见 `songmeta` 的文件头）。
func TestCleanFallsBackToTheRawTitle(t *testing.T) {
	for _, in := range []string{
		"【官方MV】", "[4K]", "（官方）", "【】", "【4K修复】",
	} {
		if got := Clean(in); got.Title != in {
			t.Errorf("Clean(%q).Title = %q，want 原标题 %q（认不出就退回原标题）",
				in, got.Title, in)
		}
	}
}

// TestTheBracketedSongNameIsNotLost 断「【晴天】那一档**内容不许丢**」。
//
// 🔴 **这是一个真缺陷**（旧实现）：旧 `Clean` 在「清洗之后标题非空」那条路上
// **只赋 `Title`**（`Extra` 从没被赋值）⇒ **方括号里的内容整个丢了**——而
// 「【晴天】」里那内容**就是歌名**。
//
// 现在：歌名是「晴天」，而「【周杰伦】晴天」那一档方括号里的「周杰伦」**照旧**
// 进 `Extra`（副标题上写着谁唱的）。
func TestTheBracketedSongNameIsNotLost(t *testing.T) {
	// ⚠️ **整条标题就是那对括号**：那一段**就是**歌名。
	if got := Clean("【晴天】"); got.Title != "晴天" {
		t.Errorf("Clean(\"【晴天】\").Title = %q，want「晴天」", got.Title)
	}
	// ⚠️ **方括号外面还有别的**：方括号里那一段是歌手 ⇒ 它进 `Extra`（**不许
	// 丢**）。
	got := Clean("【周杰伦】晴天")
	if got.Title != "晴天" {
		t.Errorf("Clean(\"【周杰伦】晴天\").Title = %q，want「晴天」", got.Title)
	}
	if got.Extra != "周杰伦" {
		t.Errorf("Clean(\"【周杰伦】晴天\").Extra = %q，want「周杰伦」"+
			"（方括号里那一段不许丢）", got.Extra)
	}
}

// TestThePromoWordsOutsideBracketsAreRemoved 断「括号**外面**的宣传词也要清」。
//
// 🔴 **这是一个真缺陷**（旧实现）：旧 `Clean` 只处理括号**里面**的，而
// 「官方MV 1080P 高音质」挂在括号**外面** ⇒ 那一档全留着 ⇒ 界面上写着
// 「周杰伦 - 晴天 官方MV 1080P 高音质」。
func TestThePromoWordsOutsideBracketsAreRemoved(t *testing.T) {
	got := Clean("【4K修复】周杰伦 - 晴天 官方MV 1080P 高音质")
	if got.Title != "晴天" {
		t.Errorf("Clean(...).Title = %q，want「晴天」"+
			"（括号外的宣传词也要清）", got.Title)
	}
}

// TestTheKnownShapesAreRecognized 断「B 站上那几种常见形状都认得出歌名」。
//
// ⚠️ **这张表是票面给的**（实测过旧实现认不出的那些形状）：每一行都是一种
// B 站上的常见写法。
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
		if got := Clean(c.in).Title; got != c.want {
			t.Errorf("Clean(%q).Title = %q，want %q", c.in, got, c.want)
		}
	}
}
