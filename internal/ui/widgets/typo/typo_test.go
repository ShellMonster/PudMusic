package typo

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

// TestTheSerifIsRegistered：随包宋体注册得上，注册之后框架的字体表里有它。
func TestTheSerifIsRegistered(t *testing.T) {
	if err := Register(); err != nil {
		t.Fatalf("注册随包宋体失败：%v", err)
	}
	found := false
	for _, f := range ui.FontFamilies() {
		if f.Name == DisplayFamily {
			found = true
			if !f.Registered {
				t.Errorf("%q 在字体表里，但没标成应用注册的", DisplayFamily)
			}
		}
	}
	if !found {
		t.Fatalf("注册之后字体表里没有 %q", DisplayFamily)
	}
	if monoFamily == "" {
		t.Error("等宽字体那一族没定下来")
	}
}

// TestTheSubsetIsTheAgreedSize：子集在 3–5MB（票面目标），不是整套 25MB 的可变字体。
func TestTheSubsetIsTheAgreedSize(t *testing.T) {
	if n := len(serifBold); n < 2_500_000 || n > 5_500_000 {
		t.Errorf("随包宋体 %d 字节，want 3–5MB 的子集", n)
	}
}

// TestDisplayDrawsInTheSerif：同一句话，Display 画出来和系统字体画出来的墨不一样
// （宋体真的用上了），而且宋体比系统粗体的字面更宽或一样宽——起码不是同一份像素。
func TestDisplayDrawsInTheSerif(t *testing.T) {
	render := func(display bool) []byte {
		img := ui.Render(func(c *ui.Context) {
			e := ui.Text(c, "推荐音乐 晚上好")
			if display {
				Display(e, 48)
			} else {
				e.FontSize(48).FontWeight(700)
			}
		}, 400, 80, 1)
		return img.Pix
	}
	a, b := render(true), render(false)
	same := len(a) == len(b)
	if same {
		for i := range a {
			if a[i] != b[i] {
				same = false
				break
			}
		}
	}
	if same {
		t.Fatal("Display 画出来和系统粗体一个像素都不差——随包宋体没用上")
	}
}

// TestEyebrowIsUppercaseMonoAndTracked：Eyebrow 全大写、比不加字距宽。
func TestEyebrowIsUppercaseMonoAndTracked(t *testing.T) {
	tt := ui.NewTester(func(c *ui.Context) {
		// 放进 Row：根上的 Text 默认占满整行宽，量不出字本身多宽。
		ui.Column(c).Children(func() {
			ui.Row(c).Children(func() { Eyebrow(c, "album of the day") })
			ui.Row(c).Children(func() {
				Mono(ui.Text(c, "ALBUM OF THE DAY! ")).FontSize(EyebrowSize).FontWeight(600).SingleLine()
			})
		})
	}, 600, 100)
	tt.Frame()
	tracked, ok := tt.Find("ALBUM OF THE DAY")
	if !ok {
		t.Fatalf("没有全大写的那一行，文字有 %q", tt.Texts())
	}
	plain, ok := tt.Find("ALBUM OF THE DAY! ")
	if !ok {
		t.Fatalf("对照行没画出来")
	}
	// 对照行多两个字符；加了 0.2em 字距的那一行仍然更宽，说明字距真的加上了。
	if tracked.W <= plain.W {
		t.Errorf("Eyebrow 宽 %.1f，不加字距的对照行宽 %.1f——字距没加上", tracked.W, plain.W)
	}
}
