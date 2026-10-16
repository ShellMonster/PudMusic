package download

import (
	"net/url"
	"strings"
	"testing"
	"unicode/utf8"
)

// 净化的三条要求来自 `sanitize.go` 的文件头：剥标签、解实体、换非法字符、
// 截断到 **255 字节**。每一条各断一遍，最后再断一遍「三步的顺序」。
//
// B 站歌名里这三种脏东西是**同时**出现的（高亮标签 + 实体 + 冒号），
// 所以第一个用例就是混着给的。

func TestSanitizeStripsTagsEntitiesAndIllegalChars(t *testing.T) {
	cases := []struct {
		name  string
		title string
		want  string
	}{
		{
			// B 站搜索结果里最常见的一种：<em> 高亮 + 数字实体 + 冒号。
			// 冒号是**必须**换掉的——它在 Finder 里显示成 `/`，
			// 在 Windows 上根本建不出文件。
			name:  "标签实体冒号一起来",
			title: `<em class="keyword">残酷</em>物语: &#21487;`,
			want:  "残酷物语_ 可",
		},
		{
			name:  "命名实体",
			title: "Rock &amp; Roll",
			want:  "Rock & Roll",
		},
		{
			// 七个 Windows 保留字符全在。注意 `<h>` **被当成标签剥掉了**
			//（`gi` 之间什么都没有）——这正是「先剥标签」那个顺序的代价，
			// 钉在这里是为了别有人哪天悄悄把顺序换回去。
			// 裸的尖括号（`a<b`）归下一条管：它剥不掉标签，就被当非法字符。
			name:  "保留字符",
			title: `a/b\c:d*e?f"g<h>i|j`,
			want:  "a_b_c_d_e_f_gi_j",
		},
		{
			name:  "裸尖括号",
			title: "3<5",
			want:  "3_5",
		},
		{
			// 连着几个非法字符只出一个下划线，不然 `AC//DC` 变成 `AC___DC`。
			name:  "连着的非法字符并成一个",
			title: "AC//DC",
			want:  "AC_DC",
		},
		{
			// 控制字符（B 站歌名里偶尔带 \x00 之类）。
			name:  "控制字符",
			title: "a\x00b\x1fc",
			want:  "a_b_c",
		},
		{
			// 首尾空白和中间连着的空白。
			// ⚠️ U+3000（全角空格）**不动**：它在中文排版里是有意义的字符，
			// 不是脏东西。B 站上游的正则 `\s+` 会把它换成半角，那是它们的
			// 选择，不是我们的。
			name:  "空白收干净",
			title: "  歌名 \n\n 里  有  空格 ",
			want:  "歌名 里 有 空格",
		},
		{
			// 解实体**再**换非法字符：`3&lt;5` 解出来是 `3<5`，那不是标签，
			// 是一个该被换掉的非法字符。顺序反了它会被当标签删掉，
			// 歌名变成 `35`。
			// `&` 不是非法字符（三个平台都允许），所以 `&amp;&amp;` 留下 `&&`。
			name:  "转义过的尖括号不是标签",
			title: "3&lt;5 &amp;&amp; 4&gt;2",
			want:  "3_5 && 4_2",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := sanitize(c.title); got != c.want {
				t.Fatalf("sanitize(%q)\n 得到 %q\n 想要 %q", c.title, got, c.want)
			}
		})
	}
}

func TestSanitizeDecodesNumericEntitiesThatBiuMisses(t *testing.T) {
	// B 站上游（`electron/utils.ts:83`）的正则**匹配**了 `&#…;` / `&#x…;`
	// 却只查一张六个键的命名表，查不到就把原样 match 返回——数字实体
	// 根本没被解，歌名里会留下六个字面字符。这里用标准库，钉住这一点。
	const want = "可"
	for _, in := range []string{"&#21487;", "&#x53EF;", "&#X53ef;"} {
		if got := sanitize(in); got != want {
			t.Fatalf("sanitize(%q) = %q，期望 %q", in, got, want)
		}
	}
}

// 🔴 截断按**字节**，不是按字符。
//
// 用字符截的话一个中文歌名是 765 字节，`os.Create` 直接报
// `file name too long`——而且是在下到一半的时候才报。
func TestFileNameTruncatesToBytesNotRunes(t *testing.T) {
	// 300 个汉字 = 900 字节 > 255。
	got := FileName(strings.Repeat("歌", 300), ".flac")
	if len(got) > maxNameBytes {
		t.Fatalf("截断之后还有 %d 字节，上限是 %d：%q", len(got), maxNameBytes, got)
	}
	// 一个汉字 3 字节，扩展名 `.flac` 占掉 5 字节，剩下的 250 装得下 83 个。
	if n := utf8.RuneCountInString(strings.TrimSuffix(got, ".flac")); n != 83 {
		t.Fatalf("截断之后是 %d 个汉字，250 字节应该装得下 83 个", n)
	}
}

// 🔴 不能从中间把一个 UTF-8 字符劈开。
//
// 劈开的那半个让文件名变成非法 UTF-8，HFS+ 上表现为「打不开」，
// 而且**不会报错**，只是 Finder 里那个名字显示成乱码。
func TestFileNameNeverSplitsARune(t *testing.T) {
	// 「𝄞」是四字节的增补平面字符，放前面正好让截断点落在它中间。
	for n := 240; n < 270; n++ {
		got := FileName(strings.Repeat("歌", 60)+"𝄞"+strings.Repeat("a", n), ".m4a")
		if !utf8.ValidString(got) {
			t.Fatalf("截断到 %d 字节时把字符劈开了：%q", n, got)
		}
	}
}

// 🔴 255 的预算**要含扩展名**。
//
// B 站上游是先截到 255 再拼扩展名（`download-core.ts:277`），最长出来
// 260 个字符——正好超出它本来想守住的那个上限。
func TestFileNameBudgetIncludesTheExtension(t *testing.T) {
	got := FileName(strings.Repeat("歌", 300), ".flac")
	if len(got) > maxNameBytes {
		t.Fatalf("含扩展名一共 %d 字节，超了上限 %d", len(got), maxNameBytes)
	}
	if !strings.HasSuffix(got, ".flac") {
		t.Fatalf("扩展名被截掉了：%q", got)
	}
}

func TestFileNameOfNothingIsEmpty(t *testing.T) {
	// 净化不出来时**返回空串**，不编一个假名字。调用方拿内容 id 顶上
	//（`DestFor`）。编一个「未命名」在记录页上一眼看得出是名字丢了。
	for _, in := range []string{"", "   ", "..."} {
		if got := FileName(in, ".m4a"); got != "" {
			t.Fatalf("FileName(%q) 净化不出来却给了 %q", in, got)
		}
	}
	// 全是非法字符时**不是**空串：`///` 并成一个 `_`。
	if got := FileName(`///`, ".m4a"); got != "_.m4a" {
		t.Fatalf("连着的非法字符应该并成一个下划线，给了 %q", got)
	}
	// 结尾是点/空格：macOS 不允许，Windows 连空格也不行。
	if got := FileName("歌名...", ".m4a"); got != "歌名.m4a" {
		t.Fatalf("结尾的点没去掉：%q", got)
	}
}

func TestExtOfIgnoresTheQueryString(t *testing.T) {
	// B 站的地址后面挂着一长串查询参数，直接对整条取后缀会取到
	// `&upsig=xxx` 那种东西。
	u, err := url.Parse("https://cn.bilivideo.com/audio/x.m4s?upsig=abc&deadline=12345")
	if err != nil {
		t.Fatal(err)
	}
	if got := ExtOf(u.String()); got != ".m4s" {
		t.Fatalf("ExtOf = %q，期望 .m4s", got)
	}
	for _, raw := range []string{
		"https://x/y.flac?a=1",
		"https://x/y.M4A",
		"https://x/y.mp3?a=1#f",
	} {
		if got := ExtOf(raw); got == "" {
			t.Fatalf("ExtOf(%q) 认不出来", raw)
		}
	}
	// 认不出来的容器宁可当没有，也不要落一个 `.php` 之类的后缀。
	if got := ExtOf("https://x/y.php?a=1"); got != "" {
		t.Fatalf("ExtOf 认了一个不是音频的后缀：%q", got)
	}
	if got := ExtOf("://不是地址"); got != "" {
		t.Fatalf("ExtOf 对着垃圾输入给了 %q", got)
	}
}
