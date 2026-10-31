// 「这一段**像不像**艺人名」那一族测试（`nameshape.go`）。
//
// # 这一族断的是「**说谎的那一半**」
//
// 词典（`artists.go`）是**一张名单**，而名单**收不全**——漏掉一个歌手的后果是
// 那一首歌名认成歌手名，而界面上**把歌手的名字当歌名显示**。
//
// ⇒ 所以有**两层**：词典（准确但不全）+ 形状判据（这一族）。
//
// ⚠️ **这一族断的是「形状判据会不会把不该否决的否决掉」**：判据**宁可多命中**
// （误判的代价是那一条标题退回原样，**不说谎**），但也不能把**所有**歌名都
// 判成人名（那样整份标题都退回原样，识别率就废了）。
package songmeta

import "testing"

// TestTheShapeCatchesTheNamesTheDictionaryMisses 钉住「词典漏掉的那几个，形状
// 判据要接住」。
//
// 🔴 **这一族是票 #706 那个 bug 的正题**：那一张表上的每一个，词典里**都**没
// 收——而按约定取后一段的话，界面上显示的就是**歌手的名字**。
func TestTheShapeCatchesTheNamesTheDictionaryMisses(t *testing.T) {
	for _, in := range []string{
		"吴青峰", "隔壁老樊", "张远", "队长", "阿冗", "Beyond",
	} {
		if IsArtistName(in) {
			// 词典收了的（补漏那一批）就不该由这一族管——但**也不该红**。
			continue
		}
		if !looksLikeArtistName(in, "起风了") {
			t.Errorf("looksLikeArtistName(%q) = false——词典没收它，而形状判据"+
				"也没接住 ⇒ 那一条标题会把歌手的名字当歌名显示", in)
		}
	}
}

// TestTheShapeDoesNotSwallowTheSongNames 钉住「形状判据**不把歌名当人名**」。
//
// ⚠️ **这一族是「宁可多命中」的那一半的反面**：歌名被当成人名 ⇒ 那一条标题
// 退回原样（脏，但不说谎）——而**到处都是**的话，整份标题都退回原样，识别率
// 就废了。
func TestTheShapeDoesNotSwallowTheSongNames(t *testing.T) {
	for _, in := range []string{
		"起风了", "晴天", "稻香", "海阔天空", "体面", "消愁", "孤勇者",
		"一路向北", "七里香", "夜曲", "夜空中最亮的星", "匆匆那年",
	} {
		if looksLikeArtistName(in, "某个 UP 主") {
			t.Errorf("looksLikeArtistName(%q) = true——那是个歌名，"+
				"形状判据把它当成人名 ⇒ 那一档会退回原标题", in)
		}
	}
}

// TestTheLatinNameNeedsAHanNeighbour 钉住「拉丁字母那一档**旁边要有汉字**」。
//
// ⚠️ **这是这一份判据的骨干**：纯英文的标题里**歌名和艺人名长得一样**
// （"Adele - Hello"），而 B 站上**通行约定就是「艺人 - 歌名」**——所以纯英文那
// 一档**按约定走**，这份判据不插手。
//
// ⇒ 而「一段汉字 + 一段拉丁字母」是**中英混排**：那种写法里拉丁字母那一段几乎
// 总是团名（"海阔天空 - Beyond"）。
func TestTheLatinNameNeedsAHanNeighbour(t *testing.T) {
	// ⚠️ **旁边是汉字** ⇒ 那是团名。
	if !looksLikeArtistName("Beyond", "海阔天空") {
		t.Error("looksLikeArtistName(\"Beyond\", \"海阔天空\") = false——" +
			"中英混排那一档里拉丁字母那一段是团名")
	}
	// ⚠️ **旁边也是拉丁字母** ⇒ 不由这一族管（按通行约定走）。
	if looksLikeArtistName("Beyond", "Yesterday") {
		t.Error("looksLikeArtistName(\"Beyond\", \"Yesterday\") = true——" +
			"两边都是拉丁字母时按「艺人 - 歌名」那个约定走，这一族不插手")
	}
}

// TestTheShapeNeedsTheWholeSegment 钉住「判据看的是**整段**」。
//
// ⚠️ **一段里带别的字就不是「名字」**："吴青峰 feat. X"那一段不是人名——
// 而它是**合作歌手**那一档（`IsFeatureLead`）管的事。
func TestTheShapeNeedsTheWholeSegment(t *testing.T) {
	for _, in := range []string{
		"吴青峰 feat. 某人",
		"吴青峰 官方MV",
		"吴青峰（官方）",
	} {
		if looksLikeArtistName(in, "起风了") {
			t.Errorf("looksLikeArtistName(%q) = true——那不是「一个人名」", in)
		}
	}
}
