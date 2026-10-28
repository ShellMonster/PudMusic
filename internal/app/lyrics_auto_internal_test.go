package app

import (
	"testing"

	"github.com/ShellMonster/PudMusic/internal/service/lyrics"
)

func TestCleanLyricTitleDropsBracketsAndNoise(t *testing.T) {
	for in, want := range map[string]string{
		"【洛天依原创】人是猫":              "人是猫",
		"《夜曲》官方MV":                "夜曲",
		"Shape of You (Official)": "Shape of You",
		"普通的歌":                    "普通的歌",
	} {
		if got := cleanLyricTitle(in); got != want {
			t.Errorf("cleanLyricTitle(%q)=%q want %q", in, got, want)
		}
	}
}

func TestPickAutoCandidate(t *testing.T) {
	cands := []lyrics.Candidate{
		{ID: "1", Title: "人是猫 (Live)", DurationMS: 300000},
		{ID: "2", Title: "人是猫", DurationMS: 134000},
		{ID: "3", Title: "完全不相干", DurationMS: 133000},
	}
	c, ok := pickAutoCandidate("【洛天依原创】人是猫", 133000, cands)
	if !ok || c.ID != "2" {
		t.Fatalf("挑到 %+v ok=%v，want ID 2", c, ok)
	}
	if _, ok := pickAutoCandidate("人是猫", 60000, cands); ok {
		t.Error("时长差一分多钟也挑了")
	}
	if _, ok := pickAutoCandidate("没有的歌", 0, cands); ok {
		t.Error("歌名对不上也挑了")
	}
	if c, ok := pickAutoCandidate("人是猫", 0, cands[:1]); !ok || c.ID != "1" {
		t.Errorf("不知道时长时应按歌名挑：%+v %v", c, ok)
	}
}

// 回归：榜首候选（同名翻唱）没有歌词时，要接着试下一条，而不是整个源放弃；
// 歌名里带着歌手名的候选排在前面。
func TestAutoRankingPrefersTheArtistAndKeepsTheRest(t *testing.T) {
	cands := []lyrics.Candidate{
		{ID: "1", Title: "稻香", Artist: "路人甲"},
		{ID: "2", Title: "稻香", Artist: "周杰伦"},
		{ID: "3", Title: "稻香(翻唱)", Artist: "路人乙"},
	}
	r := rankAutoCandidates("周杰伦 - 稻香 高音质", 0, cands)
	if len(r) != 2 || r[0].ID != "2" || r[1].ID != "1" {
		t.Fatalf("排序 %+v", r)
	}
}
