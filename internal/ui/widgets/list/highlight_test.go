package list

import "testing"

func TestIndexFoldIgnoresCaseAndNeverSplitsRunes(t *testing.T) {
	for _, c := range []struct {
		s, sub string
		want   int
	}{
		{"Hello World", "world", 6},
		{"晴天 周杰伦", "周杰", 7},
		{"abc", "x", -1},
		{"İstanbul", "stan", -1},
	} {
		if got := indexFold(c.s, c.sub); got != c.want {
			t.Errorf("indexFold(%q,%q)=%d want %d", c.s, c.sub, got, c.want)
		}
	}
}
