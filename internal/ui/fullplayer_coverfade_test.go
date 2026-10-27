package ui

import "testing"

// 切歌时满屏封面交叉淡入淡出靠两样东西：封面格子的键跟着封面地址走（换歌 = 新格子进场、
// 旧格子带影子退场），以及进场起点和退场终点都是全透明。
func TestTheFullCoverIsKeyedByItsURLAndFadesInAndOut(t *testing.T) {
	s := &Shell{}
	a, a2 := s.fullCoverKeyFor("https://c/a.jpg"), s.fullCoverKeyFor("https://c/a.jpg")
	if a != a2 {
		t.Errorf("同一个封面地址的键应当不变：%v / %v", a, a2)
	}
	if b := s.fullCoverKeyFor("https://c/b.jpg"); b == a {
		t.Errorf("换了封面地址，键却没变：%v", b)
	}
	for name, m := range map[string]*struct{ opacity float32 }{
		"进场": {fullCoverFade.Enter.Opacity}, "退场": {fullCoverFade.Exit.Opacity},
	} {
		if m.opacity != 0 {
			t.Errorf("%s的透明度 = %v，want 0", name, m.opacity)
		}
	}
	tr := s.fullCoverMatchFor("song-1")
	if tr.Match == nil || tr.Enter == nil || tr.Exit == nil {
		t.Errorf("共享元素转场缺东西：%+v", tr)
	}
	if again := s.fullCoverMatchFor("song-1"); again.Match != tr.Match {
		t.Error("同一个身份的转场应当缓存，而不是每帧重造")
	}
}
