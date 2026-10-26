package bili

import (
	"bytes"
	"compress/flate"
	"compress/zlib"
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 这一组测的是「B 站那一份弹幕」是怎么拿下来、怎么变成一份结构化列表的。
//
// # 素材是**真的字节**
//
// `testdata/danmaku/` 里有两个文件：解压后的 XML，和**原始 deflate**
// （就是 B 站直接给的那一档）。形状说明在那个目录的 `README.md` 里——
// 里面记着 2026-10-27 的实测：响应头是 `Content-Type: text/xml` +
// `Content-Encoding: deflate`，而 deflate 是**裸的**（头两字节 `c5 55`，
// 不是 zlib 的 `78 xx`）。
//
// ⚠️ **这里没有一个断言是在证明「B 站真的这么返回」**——那要靠真机回归。
// 这里证明的是：形状对不对、我们在各种失败形态下行为对不对。

// danmakuFixtureDir 是实测素材的目录。
const danmakuFixtureDir = "testdata/danmaku"

// readDanmakuFixture 读一份素材。
func readDanmakuFixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(danmakuFixtureDir, name))
	if err != nil {
		t.Fatalf("读素材 %s：%v", name, err)
	}
	return b
}

// danmakuFixtureClient 建一台会回那份弹幕的假 B 站。
//
// ⚠️ **buvid 那一条也备上**：`Danmaku` 会带 `Buvid: true`，而 buvid 是
// 从 `x/frontend/finger/spi` 白拿的——那一趟没备的话请求照样发出去
// （`buvidCookie` 拿不到就跳过），但多一次 404。
func danmakuFixtureClient(t *testing.T) (*fakeBili, *Client) {
	t.Helper()
	f := newFakeBili(t)
	comp := readDanmakuFixture(t, "danmaku.deflate")
	f.on("/x/frontend/finger/spi", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, `{"code":0,"message":"ok","data":{"b_3":"B3DM","b_4":"B4DM"}}`)
	})
	f.on(pathDanmaku, func(w http.ResponseWriter, r *http.Request) {
		// ⚠️ **响应头要照实写**：B 站给的就是这两个。
		w.Header().Set("Content-Type", "text/xml")
		w.Header().Set("Content-Encoding", "deflate")
		_, _ = w.Write(comp)
	})
	return f, f.client(t, Options{})
}

// writeDanmakuXML 回一份**压着的**弹幕 XML。
//
// ⚠️ **要压**——B 站给的就是压着的那一档（`Content-Encoding: deflate`，
// 裸 deflate）。回一份没压的 XML 看着像那么回事，但测的就不是那条路了
// （真接口从来不给没压的）。
func writeDanmakuXML(w http.ResponseWriter, xmlText string) {
	var buf bytes.Buffer
	fw, err := flate.NewWriter(&buf, flate.DefaultCompression)
	if err != nil {
		panic(err)
	}
	if _, err := fw.Write([]byte(xmlText)); err != nil {
		panic(err)
	}
	if err := fw.Close(); err != nil {
		panic(err)
	}
	w.Header().Set("Content-Type", "text/xml")
	w.Header().Set("Content-Encoding", "deflate")
	_, _ = w.Write(buf.Bytes())
}

// wantDanmaku 是 fixture 里那几条弹幕**逐条**的期望值。
//
// ⚠️ **逐条对，不是只对条数**：条数对了而时间/颜色错了，症状是
// 「弹幕都在，但滚的时机和颜色全不对」——界面上看着有东西，看不出坏了。
var wantDanmaku = []Danmaku{
	{TimeMS: 28096, Mode: 1, FontSize: 25, Color: 16777215, Text: "？", ID: "2230710913068061952"},
	{TimeMS: 3694, Mode: 1, FontSize: 25, Color: 16777215, Text: "许愿0+1挽昼不歪，出了玩到关服，出了必来还愿", ID: "2230660890598903552"},
	{TimeMS: 41954, Mode: 1, FontSize: 25, Color: 16777215, Text: "太帅了啊啊啊啊", ID: "2230850003000190720"},
	{TimeMS: 70120, Mode: 5, FontSize: 25, Color: 16777215, Text: "意思是大招直接重启游戏吗？！！！", ID: "2229049562097370368"},
	{TimeMS: 16205, Mode: 5, FontSize: 25, Color: 16777215, Text: "站在妈妈后面！", ID: "2229053856468673024"},
	{TimeMS: 60, Mode: 4, FontSize: 25, Color: 6830715, Text: "佩恩六道，参上！", ID: "2230822513213287680"},
	{TimeMS: 27198, Mode: 4, FontSize: 25, Color: 16777215, Text: "索尼1000xm6报到", ID: "1955381659302397952"},
	{TimeMS: 75560, Mode: 1, FontSize: 25, Color: 16777215, Text: "B&O Level", ID: "2205875756755285504"},
	{TimeMS: 55285, Mode: 1, FontSize: 25, Color: 16777215, Text: "B&W pi8前来报道", ID: "2199531086035099648"},
	{TimeMS: 158606, Mode: 5, FontSize: 25, Color: 15138834, Text: "Ciallo～(∠・ω< )⌒☆", ID: "2228558683221498368"},
	{
		TimeMS: 1000, Mode: 1, FontSize: 25, Color: 16777215,
		Text: `"><img src=x id=bili_xss_probe onerror=1>`,
		ID:   "2183724117876872960",
	},
	{TimeMS: 40885, Mode: 1, FontSize: 18, Color: 15138834, Text: "哔哩哔哩(゜-゜)つロ干杯~-bilibili", ID: "2187722498287407872"},
	{TimeMS: 0, Mode: 1, FontSize: 45, Color: 16777215, Text: "text", ID: "3371230543"},
	{TimeMS: 3006, Mode: 6, FontSize: 64, Color: 9033215, Text: "第五回蓝白弹幕合战开始辣", ID: "53765158432521728"},
	{TimeMS: 3006, Mode: 6, FontSize: 64, Color: 9033215, Text: "第五回蓝白弹幕合战开始辣", ID: "53765158432521728"},
	{
		TimeMS: 53914, Mode: 7, FontSize: 70, Color: 16777215,
		Text: `[0.32,0.11,"1-1",1.5,"真/n是/n毫/n无/n道/n理",0,0,0.32,0.11,500,0,true,"黑体",1]`,
		ID:   "4141681672",
	},
	{TimeMS: 62635, Mode: 1, FontSize: 25, Color: 11890, Text: "你也时间纠缠（）", ID: "2230820584286073344"},
	{TimeMS: 68115, Mode: 1, FontSize: 25, Color: 41194, Text: "吓哭了", ID: "2230794181377636864"},
}

// TestDanmakuReadsEveryFieldOfEveryLine 钉「解析出来的那几条和素材逐条一致」。
//
// ⚠️ **逐条对五项**（时间 / 模式 / 字号 / 颜色 / 正文）：条数对了而时间错了，
// 症状是「弹幕都在，但滚的时机不对」——界面上看着有东西，看不出坏了。
func TestDanmakuReadsEveryFieldOfEveryLine(t *testing.T) {
	_, c := danmakuFixtureClient(t)

	got, err := c.Danmaku(context.Background(), "42988143830")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(wantDanmaku) {
		t.Fatalf("拿到 %d 条，want %d 条", len(got), len(wantDanmaku))
	}
	for i, want := range wantDanmaku {
		if got[i] != want {
			t.Errorf("第 %d 条 = %+v，want %+v", i, got[i], want)
		}
	}
}

// TestDanmakuCutsTheNineFieldsOfThePAttribute 钉「`p` 那 9 段是怎么切的」。
//
// 字段表（实测 2026-10-27）：
//
//	1 时间（秒，5 位小数）  2 模式  3 字号  4 颜色（十进制 RGB）
//	5 ctime  6 pool  7 用户哈希  8 rowid  9 额外位
//
// ⚠️ **第 8 段是 19 位的大整数**——走过 `float64` 会掉精度（弹幕 id 变成
// 另一个数），所以它只能留在字符串里。
func TestDanmakuCutsTheNineFieldsOfThePAttribute(t *testing.T) {
	for _, c := range []struct {
		what  string
		p     string
		want  danmakuP
		valid bool
	}{
		{
			what:  "实测的一条",
			p:     "70.12000,5,25,16777215,1792814614,0,ce48882b,2229049562097370368,10",
			want:  danmakuP{TimeSec: 70.12, Mode: 5, FontSize: 25, Color: 16777215, RowID: "2229049562097370368"},
			valid: true,
		},
		{
			what:  "白字",
			p:     "0.06000,4,25,16777215,1793025966,0,c889a439,2230822513213287680,9",
			want:  danmakuP{TimeSec: 0.06, Mode: 4, FontSize: 25, Color: 16777215, RowID: "2230822513213287680"},
			valid: true,
		},
		{
			what:  "小字号 + 彩色",
			p:     "40.88500,1,18,15138834,1787888044,0,8a5c7223,2187722498287407872,10",
			want:  danmakuP{TimeSec: 40.885, Mode: 1, FontSize: 18, Color: 15138834, RowID: "2187722498287407872"},
			valid: true,
		},
		{
			// ⚠️ **19 位的 id 一个字都不能少**：走 float64 的话它会变成
			// 2229049562097370368 之外的另一个数。
			what:  "rowid 不掉精度",
			p:     "3.00600,6,64,9033215,1629640101,1,75599940,53765158432521728,10",
			want:  danmakuP{TimeSec: 3.006, Mode: 6, FontSize: 64, Color: 9033215, RowID: "53765158432521728"},
			valid: true,
		},
		{
			what:  "第 8 段是 19 位",
			p:     "1.00000,1,25,16777215,1787411400,0,a9af1af7,2183724117876872960,10",
			want:  danmakuP{TimeSec: 1, Mode: 1, FontSize: 25, Color: 16777215, RowID: "2183724117876872960"},
			valid: true,
		},
		{
			what:  "段数不够",
			p:     "1.00000,1,25,16777215",
			valid: false,
		},
		{
			what:  "时间是乱的",
			p:     "abc,1,25,16777215,1,0,x,1,10",
			valid: false,
		},
		{
			what:  "颜色是乱的",
			p:     "1.00000,1,25,xyzzy,1,0,x,1,10",
			valid: false,
		},
	} {
		got, ok := parseDanmakuP(c.p)
		if ok != c.valid {
			t.Errorf("%s：ok = %v，want %v", c.what, ok, c.valid)
			continue
		}
		if !c.valid {
			continue
		}
		if got != c.want {
			t.Errorf("%s：%+v，want %+v", c.what, got, c.want)
		}
	}
}

// TestDanmakuUnwrapsTheXMLEntities 钉「正文里的实体被解开了」。
//
// ⚠️ **B 站的正文是转义过的**（实测有 `&amp;` / `&lt;` / `&gt;`）。
// 解不开的症状是界面上出现「B&amp;O Level」这一串符号——而弹幕一多，
// 一眼扫过去根本看不出哪几条不对。
//
// ⚠️ **里面那一条是 B 站自己给的一条 XSS 探针**（别人发的弹幕）：
// `"><img src=x id=bili_xss_probe onerror=1>`。它是**纯文字**——
// 我们不解释 HTML，所以它就该原样显示成字。
func TestDanmakuUnwrapsTheXMLEntities(t *testing.T) {
	_, c := danmakuFixtureClient(t)

	got, err := c.Danmaku(context.Background(), "42988143830")
	if err != nil {
		t.Fatal(err)
	}
	var texts []string
	for _, d := range got {
		texts = append(texts, d.Text)
	}
	joined := strings.Join(texts, "\n")
	if strings.Contains(joined, "&amp;") || strings.Contains(joined, "&lt;") {
		t.Errorf("正文里还留着没解开的实体：%q", joined)
	}
	if !strings.Contains(joined, "B&O Level") {
		t.Errorf("`&amp;` 没被解开：%q", joined)
	}
	if !strings.Contains(joined, "Ciallo～(∠・ω< )⌒☆") {
		t.Errorf("`&lt;`/`&gt;` 没被解开：%q", joined)
	}
	// XSS 探针那一条：**原样当文字**（我们不解释 HTML，所以它就是几个
	// 符号，不是一个会执行的标签）。
	if !strings.Contains(joined, `"><img src=x id=bili_xss_probe onerror=1>`) {
		t.Errorf("那条 XSS 探针的正文不对：%q", joined)
	}
}

// TestDanmakuUnwrapsIsDeflateNotZlib 钉「B 站给的是原始 deflate，不是 zlib」。
//
// 🔴 **这个坑是静默的**：B 站的响应头写着 `Content-Encoding: deflate`，
// 而那个名字**本身就有歧义**（RFC 里 `deflate` 指 zlib 包装的那种）。
// B 站给的却是**裸 deflate**（RFC 1951，头两字节 `c5 55`）——
// 拿 `compress/zlib` 去解会报一句看不懂的错（`flate: corrupt input
// before offset 5`），而症状是「一条弹幕都没有」。
func TestDanmakuUnwrapsIsDeflateNotZlib(t *testing.T) {
	xmlBytes := readDanmakuFixture(t, "danmaku.xml")

	// ⚠️ **把同一份 XML 压成 zlib**（`78 xx` 那种包装）。
	var zbuf bytes.Buffer
	zw := zlib.NewWriter(&zbuf)
	if _, err := zw.Write(xmlBytes); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	zbytes := zbuf.Bytes()
	if zbytes[0] != 0x78 {
		t.Fatalf("zlib 的头不是 78：% x", zbytes[:2])
	}

	// 🔴 **zlib 压的那份，原始 deflate 解不开**——而这一条是**静默**的：
	// 它报错，但报的是一句跟「压缩格式不对」毫无关系的话。
	if _, err := inflateDanmaku(zbytes); err == nil {
		t.Fatal("zlib 压的那份被解开了——那就是把 zlib 当 deflate 了")
	} else if !strings.Contains(err.Error(), "deflate") {
		t.Errorf("错里没提 deflate：%v", err)
	}

	// 而**原始 deflate 那份解得开**，解出来和 XML 原文一模一样。
	out, err := inflateDanmaku(readDanmakuFixture(t, "danmaku.deflate"))
	if err != nil {
		t.Fatalf("原始 deflate 解不开：%v", err)
	}
	if !bytes.Equal(out, xmlBytes) {
		t.Errorf("解出来的和 XML 原文不一样（%d vs %d 字节）", len(out), len(xmlBytes))
	}
}

// TestDoRawHandsBackTheBytesUndecoded 钉「`DoRaw` 给的是原文，不解 JSON」。
//
// ⚠️ **这一条是「为什么要有 `DoRaw`」那一句的证据**：弹幕那一条回的是
// XML，`Do` 会把它当 JSON 解——而症状是「一条弹幕都没有」，**不报错**。
func TestDoRawHandsBackTheBytesUndecoded(t *testing.T) {
	f := newFakeBili(t)
	f.on("/x/thing", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/xml")
		_, _ = w.Write([]byte(`<?xml version="1.0"?><i><d p="1">x</d></i>`))
	})
	c := f.client(t, Options{})

	got, err := c.DoRaw(context.Background(), Call{Method: "GET", URL: c.hostAPI + "/x/thing"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(got), "<?xml") {
		t.Errorf("DoRaw 给的不是原文：%q", got)
	}

	// 而 `Do` 那条路**解不了**它——这正是要另开一路的原因。
	if _, err := c.Do(context.Background(), Call{Method: "GET", URL: c.hostAPI + "/x/thing"}); err == nil {
		t.Error("Do 解开了 XML——那说明它变了")
	}
}

// TestDoRawReportsTheSameErrorsAsDo 钉「`DoRaw` 的错误分类和 `Do` 一样」。
//
// ⚠️ **错误分类只有一份**（`retry.go` 那张表是全项目唯一的一处）：而
// 「能不能重试」是按**哨兵**判的。`DoRaw` 自己造一套的话，弹幕那条路上
// 的「重试」会给错——界面上那枚「重试」是立刻再问一次，给一个按了也没用
// 的重试比不给更糟。
func TestDoRawReportsTheSameErrorsAsDo(t *testing.T) {
	for _, c := range []struct {
		what  string
		code  int
		body  string
		isErr error
	}{
		{"网关错误", http.StatusBadGateway, "", ErrNetwork},
		{"风控挡的一页 HTML", http.StatusPreconditionFailed, "<html>412</html>", ErrNetwork},
	} {
		f := newFakeBili(t)
		f.on("/x/thing", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(c.code)
			_, _ = w.Write([]byte(c.body))
		})
		cli := f.client(t, Options{})

		_, err := cli.DoRaw(context.Background(), Call{Method: "GET", URL: cli.hostAPI + "/x/thing"})
		if !errors.Is(err, c.isErr) {
			t.Errorf("%s：DoRaw 返回 %v，want %v", c.what, err, c.isErr)
		}
		if !Recoverable(err) {
			t.Errorf("%s：被判成不可重试了", c.what)
		}
	}
}

// TestDanmakuAsksTheDanmakuEndpoint 钉「问的是弹幕那个接口，参数是 oid」。
func TestDanmakuAsksTheDanmakuEndpoint(t *testing.T) {
	f, c := danmakuFixtureClient(t)

	if _, err := c.Danmaku(context.Background(), "42988143830"); err != nil {
		t.Fatal(err)
	}

	var asked bool
	for _, rec := range f.requests() {
		if rec.Path != pathDanmaku {
			continue
		}
		asked = true
		if rec.Method != http.MethodGet {
			t.Errorf("弹幕那一趟是 %s，want GET", rec.Method)
		}
		if got := rec.Query.Get("oid"); got != "42988143830" {
			t.Errorf("oid = %q，want %q", got, "42988143830")
		}
	}
	if !asked {
		t.Error("没打弹幕那个接口")
	}
}

// TestDanmakuWithoutACidDoesNotAsk 盯「cid 是空的时候不发请求」。
func TestDanmakuWithoutACidDoesNotAsk(t *testing.T) {
	f, c := danmakuFixtureClient(t)

	got, err := c.Danmaku(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("没有 cid 时回了 %d 条，want 空", len(got))
	}
	for _, rec := range f.requests() {
		if rec.Path == pathDanmaku {
			t.Error("没有 cid 时还是打了弹幕那个接口")
		}
	}
}

// TestDanmakuWithNoDanmakuIsNotAnError 盯「这一集没有弹幕」那一支。
//
// ⚠️ **空列表不是错误**：很多视频压根没有弹幕，而那是**正常**的一种。
func TestDanmakuWithNoDanmakuIsNotAnError(t *testing.T) {
	f := newFakeBili(t)
	f.on(pathDanmaku, func(w http.ResponseWriter, r *http.Request) {
		// B 站对「没有弹幕」回的是一个**只有外壳**的 XML。
		writeDanmakuXML(w, `<?xml version="1.0" encoding="UTF-8"?><i></i>`)
	})
	c := f.client(t, Options{})

	got, err := c.Danmaku(context.Background(), "12345")
	if err != nil {
		t.Fatalf("没有弹幕不该是错误：%v", err)
	}
	if len(got) != 0 {
		t.Errorf("没有弹幕时回了 %d 条，want 空", len(got))
	}
}

// TestDanmakuKeepsWhatItCouldReadWhenOneLineIsBroken 盯「一条读不懂的不把整份丢掉」。
//
// 🔴 **弹幕的正文是用户自己打的字**，什么字符都可能有。实测里有 `&amp;`
// 那些规矩实体，但**没定义**的实体或者一个**裸** `&` 在严格模式下是
// **报错**的——而报错的位置在**那一条**上。
//
// ⇒ 症状是「只显示了几条弹幕」（后面几百条全丢），而界面上看着是
// 「这个视频弹幕很少」——看不出坏了。
func TestDanmakuKeepsWhatItCouldReadWhenOneLineIsBroken(t *testing.T) {
	f := newFakeBili(t)
	f.on(pathDanmaku, func(w http.ResponseWriter, r *http.Request) {
		writeDanmakuXML(w,
			`<?xml version="1.0" encoding="UTF-8"?><i>`+
				`<d p="1.00000,1,25,16777215,1,0,x,1,10">第一条</d>`+
				`<d p="2.00000,1,25,16777215,1,0,x,2,10">坏的 &foo; 一条</d>`+
				`<d p="3.00000,1,25,16777215,1,0,x,3,10">第三条</d>`+
				`</i>`)
	})
	c := f.client(t, Options{})

	got, err := c.Danmaku(context.Background(), "12345")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) == 0 {
		t.Fatal("一条都没读到")
	}
	if got[0].Text != "第一条" {
		t.Errorf("第一条 = %q，want %q", got[0].Text, "第一条")
	}
	// 坏的那一条**照样读得出来**（非严格模式把它原样留着）。
	if len(got) < 2 || !strings.Contains(got[1].Text, "坏的") {
		t.Errorf("坏的那一条被丢了：%+v", got)
	}
	// ⚠️ **而且后面那一条不能跟着丢**——那才是「只显示了几条」那个症状。
	var sawThird bool
	for _, d := range got {
		if d.Text == "第三条" {
			sawThird = true
		}
	}
	if !sawThird {
		t.Errorf("坏的那一条把后面的也带走了：%+v", got)
	}
}

// TestDanmakuIgnoresLinesItCannotUnderstand 盯「`p` 读不出来的那一条被丢掉」。
//
// ⚠️ **丢的是那一条，不是整份**：段数不够的时候，后面的几段是空的——
// 那时候颜色会变成 0（全黑），而**一条黑字弹幕在界面上看着完全正常**。
func TestDanmakuIgnoresLinesItCannotUnderstand(t *testing.T) {
	f := newFakeBili(t)
	f.on(pathDanmaku, func(w http.ResponseWriter, r *http.Request) {
		writeDanmakuXML(w,
			`<?xml version="1.0" encoding="UTF-8"?><i>`+
				`<d p="1.00000,1,25,16777215,1,0,x,1,10">好的那条</d>`+
				`<d p="2.00000,1,25">段数不够</d>`+
				`<d p="3.00000,1,25,16777215,1,0,x,3,10">另一条好的</d>`+
				`</i>`)
	})
	c := f.client(t, Options{})

	got, err := c.Danmaku(context.Background(), "12345")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("拿到 %d 条，want 2 条：%+v", len(got), got)
	}
	for _, d := range got {
		if d.Text == "段数不够" {
			t.Errorf("段数不够的那一条没被丢掉：%+v", got)
		}
	}
}

// TestDanmakuUnwrapsAWholeResponseThatIsDeflated 盯「整条响应能解开」。
//
// 这一条走的是**真的那份压缩字节**（`testdata/danmaku/danmaku.deflate`），
// 不是在测试里现压的——所以它证明的是「B 站给的那一档字节解得开」。
func TestDanmakuUnwrapsAWholeResponseThatIsDeflated(t *testing.T) {
	comp := readDanmakuFixture(t, "danmaku.deflate")

	// ⚠️ **头两个字节不是 zlib 的 `78 xx`**——这是「deflate 不是 zlib」
	// 那一档的字节证据。
	if comp[0] == 0x78 {
		t.Fatalf("素材的头是 % x——那是 zlib，B 站给的不是那个", comp[:2])
	}

	r := flate.NewReader(bytes.NewReader(comp))
	raw, err := io.ReadAll(r)
	_ = r.Close()
	if err != nil {
		t.Fatalf("原始 deflate 解不开：%v", err)
	}
	if !bytes.HasPrefix(raw, []byte("<?xml")) {
		t.Errorf("解出来的不是 XML：%q", raw[:40])
	}
}

// TestDanmakuKeepsTheRowIDAsAString 钉「弹幕 id 是字符串，不掉精度」。
//
// 🔴 **那个 id 是 19 位的大整数**（实测 `2229049562097370368`）——走过
// `float64` 会变成 `2229049562097370400` 那一类的另一个数。
//
// ⚠️ **它为什么必须在**：下一张票（#578，满屏播放器上滚弹幕）要给**每一条**
// 弹幕一个**稳定且唯一**的 key——而报告 §3.1 实测过：同一个 key 换一批
// 弹幕会继承上一批的位移，元素直接跑到屏幕外、**一个字都画不出来**。
func TestDanmakuKeepsTheRowIDAsAString(t *testing.T) {
	_, c := danmakuFixtureClient(t)

	got, err := c.Danmaku(context.Background(), "42988143830")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) == 0 {
		t.Fatal("一条都没拿到")
	}
	for _, d := range got {
		if d.ID == "" {
			t.Errorf("有一条没有 id：%+v", d)
		}
	}
	// ⚠️ **一个字都不能少**：那个数是 19 位。
	if got[3].ID != "2229049562097370368" {
		t.Errorf("id = %q，want %q", got[3].ID, "2229049562097370368")
	}
}

// TestDanmakuGivesEveryLineItsOwnID 盯「id 不重复」。
//
// ⚠️ **重复的 id 就是下一张票那个静默失败**：两个元素共用一个 key，
// 动态列表会把它们的状态**整体错配**——而界面上看不出任何异样。
func TestDanmakuGivesEveryLineItsOwnID(t *testing.T) {
	f := newFakeBili(t)
	f.on(pathDanmaku, func(w http.ResponseWriter, r *http.Request) {
		writeDanmakuXML(w,
			`<?xml version="1.0" encoding="UTF-8"?><i>`+
				`<d p="1.00000,1,25,16777215,1,0,x,111,10">甲</d>`+
				`<d p="2.00000,1,25,16777215,1,0,x,222,10">乙</d>`+
				`</i>`)
	})
	c := f.client(t, Options{})

	got, err := c.Danmaku(context.Background(), "12345")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("拿到 %d 条，want 2 条", len(got))
	}
	if got[0].ID == got[1].ID {
		t.Errorf("两条的 id 一样：%q", got[0].ID)
	}
	if got[0].ID != "111" || got[1].ID != "222" {
		t.Errorf("id 不对：%q %q", got[0].ID, got[1].ID)
	}
}

// TestDanmakuKeepsTheOrderBiliGave 盯「顺序照 B 站给的，不自己排」。
//
// ⚠️ **这一层不排**：它返回的是**B 站给的那份列表**。而 B 站给的顺序
// **不是按时间**的（实测第一条 70.12 秒、第二条 16.205 秒）。
//
// ⇒ 要「按当前位置取现在该在屏幕上的那几条」得**调用方自己排**——
// 而排在这里是不对的：这一层是「B 站说了什么」，那一层是「现在该显示什么」。
func TestDanmakuKeepsTheOrderBiliGave(t *testing.T) {
	_, c := danmakuFixtureClient(t)

	got, err := c.Danmaku(context.Background(), "42988143830")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) < 2 {
		t.Fatalf("拿到 %d 条，太少了", len(got))
	}
	// ⚠️ **第一条是 70.12 秒、第二条是 16.205 秒**——真的不是按时间的。
	if got[0].TimeMS != 28096 || got[1].TimeMS != 3694 {
		t.Errorf("顺序变了：%d %d", got[0].TimeMS, got[1].TimeMS)
	}
}

// TestDanmakuSaysSoWhenTheAnswerIsNotDanmaku 盯「回的不是弹幕」那一档。
//
// 🔴 **这一档是静默的**：B 站被风控挡的时候回的是**一页 HTML**，而那一页
// **也是合法的 XML**（`<html><body>412</body></html>` 能解）——
// 于是「拿 XML 去解」只会得出「**零条弹幕**」。
//
// ⇒ 症状是「这个视频没有弹幕」，而真相是「现在问不出来」。
// 而这两种在界面上要走的路**不一样**（一个是不画，一个是可以重试）。
func TestDanmakuSaysSoWhenTheAnswerIsNotDanmaku(t *testing.T) {
	f := newFakeBili(t)
	f.on(pathDanmaku, func(w http.ResponseWriter, r *http.Request) {
		// ⚠️ **B 站风控挡下来的那一页**：HTTP 200 + 一页 HTML，
		// 而且**它是压着的**（真接口给的每一份响应都是压着的）。
		writeDanmakuXML(w, "<html><body>412 Precondition Failed</body></html>")
	})
	c := f.client(t, Options{})

	got, err := c.Danmaku(context.Background(), "12345")
	if err == nil {
		t.Fatalf("回了一页 HTML 却当成没弹幕（%d 条）", len(got))
	}
	if !errors.Is(err, ErrBadResponse) {
		t.Errorf("err = %v，want ErrBadResponse", err)
	}
	// ⚠️ **「回的不是预期的内容」是不可重试的**：立刻再问还是撞同一面墙
	// （`retry.go` 那张表）。
	if Recoverable(err) {
		t.Error("被判成可重试了——立刻再问还是撞同一面墙")
	}
	// 而它**不是**真风控（-352/-412 那个业务码）——那一档在 JSON 里面。
	if errors.Is(err, ErrRiskControl) {
		t.Error("被归到了真风控")
	}
}

// TestDanmakuWithTheFeatureTurnedOffIsNotAnError 盯「弹幕被关闭」那一支。
//
// ⚠️ **实测（2026-10-27）**：B 站对「这个视频弹幕功能已被关闭」回的是
// **一个压着的 XML**，里面**没有 `<d>`**，但有几段说明：
//
//	<?xml version="1.0" encoding="UTF-8"?><i><chatserver>chat.bilibili.com</chatserver>
//	<chatid>1</chatid><mission>0</mission><maxlimit>1500</maxlimit><state>2</state>
//	<text>本视频弹幕功能已被关闭</text><text_side>本视频弹幕功能已被关闭</text_side></i>
//
// ⇒ **零条弹幕，不是错误**：那是**正常**的一种（很多视频压根没开弹幕）。
func TestDanmakuWithTheFeatureTurnedOffIsNotAnError(t *testing.T) {
	f := newFakeBili(t)
	f.on(pathDanmaku, func(w http.ResponseWriter, r *http.Request) {
		writeDanmakuXML(w, `<?xml version="1.0" encoding="UTF-8"?><i>`+
			`<chatserver>chat.bilibili.com</chatserver><chatid>1</chatid>`+
			`<mission>0</mission><maxlimit>1500</maxlimit><state>2</state>`+
			`<text>本视频弹幕功能已被关闭</text>`+
			`<text_side>本视频弹幕功能已被关闭</text_side></i>`)
	})
	c := f.client(t, Options{})

	got, err := c.Danmaku(context.Background(), "1")
	if err != nil {
		t.Fatalf("弹幕被关闭不该是错误：%v", err)
	}
	if len(got) != 0 {
		t.Errorf("弹幕被关闭时回了 %d 条，want 空", len(got))
	}
}
