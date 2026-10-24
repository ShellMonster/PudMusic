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
		{"周杰伦 2023 演唱会 稻香", "周杰伦 2023 演唱会 稻香", "", true},
		{"一路向北 - 周杰伦 | 官方MV", "一路向北 - 周杰伦 | 官方MV", "", false},
		{"【4K】", "【4K】", "", false},
		{"", "", "", false},
		{"《千里之外》（feat. 费玉清）", "《千里之外》（feat. 费玉清）", "", false},
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
