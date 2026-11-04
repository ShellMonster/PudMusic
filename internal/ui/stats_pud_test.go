package ui

// 听歌统计页的原型样式（T7）：`stat-strip` 四格、每天收听柱状图、最常听的歌手、本周单曲循环。

import "testing"

func TestTheStatsPageShowsTheFourStripCellsAndThreeSections(t *testing.T) {
	tt := statsShotShell(t, false, seedShotStats)
	for _, want := range []string{
		"听歌统计", "最近 7 天", "收听总时长", "播放过的歌曲", "新认识的歌手", "连续听歌",
		"每天收听", "单位：分钟", "最常听的歌手", "本周单曲循环", "今天",
	} {
		if !tt.HasText(want) {
			t.Errorf("统计页上没有 %q，文字有 %q", want, tt.Texts())
		}
	}
}

func TestTheStatsLoopRowsSayHowManyTimesTheyPlayed(t *testing.T) {
	tt := statsShotShell(t, false, seedShotStats)
	if !tt.HasText("本机播放 ") {
		t.Errorf("单曲循环那一列没有「本机播放 N 次」，文字有 %q", tt.Texts())
	}
	if !tt.HasText("播放 / 弹幕") { // 曲目表头
		t.Errorf("单曲循环上面没有曲目表头，文字有 %q", tt.Texts())
	}
}

func TestTheStatsPageHasNoTabsAnymore(t *testing.T) {
	tt := statsShotShell(t, false, seedShotStats)
	for _, gone := range []string{"日", "周", "月"} {
		if _, ok := tt.Find(gone); ok {
			t.Errorf("统计页上还有「%s」那一档 Tab（原型只有最近 7 天）", gone)
		}
	}
}
