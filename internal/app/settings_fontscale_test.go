package app

import "testing"

// 「界面字号」三档：认不出的值落到标准，比例是 0.94 / 1 / 1.08。
func TestFontScaleNormalizesAndHasThreeFactors(t *testing.T) {
	if got := DefaultSettings("").FontScale; got != FontScaleStd {
		t.Errorf("默认字号档 = %q，want std", got)
	}
	for in, want := range map[FontScale]float32{FontScaleSmall: 0.94, FontScaleStd: 1, FontScaleLarge: 1.08, "": 1, "huge": 1} {
		if got := in.Factor(); got != want {
			t.Errorf("%q.Factor() = %v, want %v", in, got, want)
		}
	}
	s := DefaultSettings("")
	s.FontScale = "huge"
	s.ProxySettings.Type = "bogus"
	s.normalize()
	if s.FontScale != FontScaleStd || s.ProxySettings.Type != ProxyNone {
		t.Errorf("归一后 fontScale=%q proxy=%q", s.FontScale, s.ProxySettings.Type)
	}
	s.ProxySettings.Type = ProxyDirect
	s.normalize()
	if s.ProxySettings.Type != ProxyDirect {
		t.Errorf("direct 被归一掉了：%q", s.ProxySettings.Type)
	}
}
