package ui

import (
	"strings"
	"testing"
)

// TestEmptyIllustrationsFollowTheTheme 钉住「每张空状态插画都用 currentColor」：写死
// 颜色的话深浅两套主题里会有一套看不清（同 art/error.svg 那一条）。
func TestEmptyIllustrationsFollowTheTheme(t *testing.T) {
	for name, file := range emptyIllustrationFiles {
		b, err := artFile.ReadFile("art/" + file)
		if err != nil {
			t.Fatalf("%s 的插画 %s 读不到: %v", name, file, err)
		}
		if !strings.Contains(string(b), "currentColor") || strings.Contains(string(b), "#") {
			t.Errorf("%s 应只用 currentColor、不写死颜色", file)
		}
		if emptyIllustrations[name] == nil {
			t.Errorf("%s 没解析出来", name)
		}
	}
}
