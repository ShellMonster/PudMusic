package bili

import (
	"strings"
	"testing"
)

// 这一组钉的是**音质 id ↔ 标签**那张表。
//
// # 为什么要有这一组
//
// B 站的音质 id 会变，而这张表是**硬编码**在 `quality.go` 里的——所以它必须
// 被钉住：哪天有人改错一个 id，症状是**界面上一个安静的谎**（票 #255 就是
// 这么来的：Hi-Res 无损流挂着「杜比 Hi-Res」，`IsLossless()` 还判它不是无损，
// 而界面上一行错都没有）。
//
// # 表的出处
//
// 公开资料 + 2026-10-23 的只读实测，见 `quality.go` 的文件头。**实测是准的**：
// `dash.flac.audio.id` 是 30251（老资料里的 30257 在实测里找不到）。
//
// ⚠️ B 站加档、改档的时候这张表会过期。**过期不是事故**——认不出来的 id 有
// 兜底（按码率说话），所以这里钉的是「**已知这几档**」而不是「**只有这几档**」。

// knownQualities 是「B 站给得出来的投稿侧音质」那张表。
//
// 顺序就是从低到高：和 `qualityRank` 一致。
var knownQualities = []struct {
	q         Quality
	label     string
	lossless  bool
	bandwidth int // 实测的那一档的码率（用来验证「标签不吃码率」）
}{
	{QualityLow64, "64K", false, 43962},
	{QualityMid128, "128K", false, 102931},
	{QualityHigh192, "192K", false, 203786},
	{QualityDolby, "杜比全景声", false, 1024203},
	{QualityHiResLossless, "Hi-Res 无损", true, 1609860},
}

// TestKnownQualityLabelsArePinned 钉住「这几档分别是什么标签」。
//
// 传进去的 bitrate 是**故意给错的**：认得出的档位必须用**档位自己的名字**，
// 不能拿码率去算（B 站给的 bandwidth 是这一次编码的实际码率，和标称对不上）。
// 所以哪怕给 0，标签也得是那张表里的名字。
func TestKnownQualityLabelsArePinned(t *testing.T) {
	for _, tc := range knownQualities {
		t.Run(tc.label, func(t *testing.T) {
			if got := tc.q.Label(0); got != tc.label {
				t.Errorf("Label(0) = %q，want %q", got, tc.label)
			}
			if got := tc.q.Label(tc.bandwidth); got != tc.label {
				t.Errorf("Label(%d) = %q，want %q（认得出的档位不吃码率）",
					tc.bandwidth, got, tc.label)
			}
		})
	}
}

// TestTheThreeViewsOfAQualityAgree 钉住「标签 / IsLossless / qualityRank」三者
// 一致。
//
// 这三个是**同一件事的三种说法**：
//
//	标签说什么（给人看）、IsLossless() 说什么（给机器判）、qualityRank 排第几
//	（给挑流排序）。
//
// 它们一旦各说各的，症状全是**静默**的：徽章写着「Hi-Res 无损」而
// `IsLossless()` 说不是（于是容器判错、下载判错），或者标签说「杜比 Hi-Res」
// 而 rank 把它排在有损下面（于是自动档挑错）。
func TestTheThreeViewsOfAQualityAgree(t *testing.T) {
	// 「标签里带『无损』」 ⟺ 「IsLossless() 为真」。
	for _, tc := range knownQualities {
		saysLossless := strings.Contains(tc.label, "无损")
		if saysLossless != tc.lossless {
			t.Errorf("%d（标签 %q）的无损判定是 %v，want %v",
				tc.q, tc.label, tc.lossless, saysLossless)
		}
		if tc.q.IsLossless() != saysLossless {
			t.Errorf("%d（标签 %q）的 IsLossless() = %v，want %v",
				tc.q, tc.label, tc.q.IsLossless(), saysLossless)
		}
	}

	// rank 必须**严格递增**（从低到高），而且每一档都在表里。
	prev := 0
	for _, tc := range knownQualities {
		got, ok := qualityRank[tc.q]
		if !ok {
			t.Errorf("%d（%s）不在 qualityRank 里", tc.q, tc.label)
			continue
		}
		if got <= prev {
			t.Errorf("%d（%s）的 rank = %d，不比上一档（%d）高",
				tc.q, tc.label, got, prev)
		}
		prev = got
	}

	// 认不出来的 id 排在**所有已知档之下**（不是中间）。
	// 不知道它有多好，宁可当成最低，也不要凭 id 的大小猜。
	if r, ok := qualityRank[QualityUnknown]; ok {
		t.Errorf("QualityUnknown 在 qualityRank 里，rank = %d（它该是「不在表里」）", r)
	}
	if r, ok := qualityRank[Quality(99999)]; ok {
		t.Errorf("未知的 id 99999 竟然在 qualityRank 里，rank = %d", r)
	}
}

// TestHiResLosslessIsNotDolbyHiRes 盯票 #255 的那一条。
//
// 30251 以前被叫「杜比 Hi-Res」。它是**投稿侧的 Hi-Res 无损**（`dash.flac`
// 里的 FLAC），和杜比没有关系——挂错了标签，`IsLossless()` 也会跟着判错。
func TestHiResLosslessIsNotDolbyHiRes(t *testing.T) {
	const q = Quality(30251)
	if !q.IsLossless() {
		t.Errorf("30251 的 IsLossless() = false，want true")
	}
	if got := q.Label(1609860); got != "Hi-Res 无损" {
		t.Errorf("30251 的标签 = %q，want Hi-Res 无损", got)
	}
	if strings.Contains(q.Label(0), "杜比") {
		t.Errorf("30251 的标签里带「杜比」：%q", q.Label(0))
	}
	// 杜比是另一档（30250），它**不是**无损。
	if QualityDolby.IsLossless() {
		t.Errorf("30250（杜比全景声）的 IsLossless() = true，want false")
	}
	if got := QualityDolby.Label(1024203); got != "杜比全景声" {
		t.Errorf("30250 的标签 = %q，want 杜比全景声", got)
	}
}

// TestAnUnknownQualityFallsBackToTheBitrate 盯「B 站加新档」时的兜底。
//
// 认不出来的 id **不是错误**：按码率说话总比「未知音质」有用，而且**不能**
// 报错、不能崩、不能挂成一个不相干的标签。
func TestAnUnknownQualityFallsBackToTheBitrate(t *testing.T) {
	const unknown = Quality(99999)
	if got := unknown.Label(203786); got != "204K" {
		t.Errorf("未知档的标签 = %q，want 204K（按码率说话）", got)
	}
	if got := unknown.Label(0); got != "未知音质" {
		t.Errorf("没有码率时标签 = %q，want 未知音质", got)
	}
	if unknown.IsLossless() {
		t.Errorf("未知档的 IsLossless() = true，want false")
	}
}

// TestChipLabelShortensTheQualityNames 盯播放条上那枚 chip 的短版。
//
// 那一行只有 76 宽，全称放不下（框架会把徽章摊扁，点击区只剩 14 像素）。
// 短版只负责「一眼认得出是哪一类」，全称留在悬停提示里。
func TestChipLabelShortensTheQualityNames(t *testing.T) {
	for _, tc := range []struct{ full, want string }{
		{"Hi-Res 无损", "无损"},
		{"无损 FLAC", "无损"},
		{"杜比全景声", "杜比"},
		{"192K", "192K"},
		{"128K", "128K"},
		{"64K", "64K"},
		{"未知音质", "未知"},
		{"204K", "204K"},
	} {
		if got := ChipLabel(tc.full); got != tc.want {
			t.Errorf("ChipLabel(%q) = %q，want %q", tc.full, got, tc.want)
		}
	}
}
