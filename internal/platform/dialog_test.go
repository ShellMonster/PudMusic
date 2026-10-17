package platform

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	// 空导入：这些 init 才是真正往解码器注册表里写东西的地方，白名单要对着它们核。
	_ "github.com/ShellMonster/Biu_Go/internal/service/audio"

	"github.com/egoist/mygo"
	mygoaudio "github.com/egoist/mygo/audio"
)

// 这组测试盯的是一条不变量：**文件框里能选的格式，和应用真能解的格式，必须一样。**
//
// 注册表按文件头分派，白名单按扩展名，两边各记各的。注册表那边加了新解码器而
// 白名单这边忘了跟上，用户看到的现象是「能播，却选不进来」——阶段 1 做完 AAC
// 解码器忘了开入口就是这个问题。下面每条断言都对着活的注册表跑，不是对着这份
// 表自己抄一遍。

// registryRow 是解码器注册表里的一条，外加它对应的文件扩展名。
type registryRow struct {
	name string // 注册到 MyGo 的格式名，也就是 audio.NewDecoder 返回的那个名字
	// magic 是 RegisterFormat 的第二个参数。注册表里没有「扩展名」的概念，
	// exts 是另记的一层，必须能从这里指回它的源头。
	magic string
	// 只有在 magic 为空时才用：说明这一条不走注册表，是靠什么被认出来的。
	via  string
	exts []string
}

// knownDecoders 是解码器注册表的手抄本，一行对应一次 RegisterFormat。
//
// ⚠️ 往注册表里加解码器就在这里加一行，而且补上它该有的 exts——
// TestKnownDecodersCoversEveryRegistration 会发现漏了的那一行，
// TestAudioFiltersCoverEveryDecodableExtension 会挡住补了行却忘了开白名单的那一半。
var knownDecoders = []registryRow{
	// MyGo 自带的（audio/wav.go、audio/mp3/mp3.go）。
	{name: "wav", magic: "RIFF????WAVE", exts: []string{"wav"}},
	{name: "mp3", magic: "ID3", exts: []string{"mp3"}},

	// 本仓库注册的（internal/service/audio/aiff.go、containers.go）。
	// .aif 不是第三种格式，只是 IFF 容器的另一个叫法：注册表认的是文件头，
	// .aif 和 .aiff 落到同一条注册上，所以它们写在同一行。
	{name: "aiff", magic: "FORM????AIFF", exts: []string{"aiff", "aif"}},
	{name: "aifc", magic: "FORM????AIFC", exts: []string{"aifc"}},
	// ftyp 是正常封装的 MP4 家族，styp 是分片流（Biu 下载的 .m4s）。
	// ⚠️ .mp4 不在白名单里：同一个容器也装着视频，文件框按音频筛的时候不该
	// 把电影摆到用户面前。真要开，靠末位的「所有文件」。
	{name: "mp4", magic: "????ftyp", exts: []string{"m4a"}},
	{name: "mp4", magic: "????styp", exts: []string{"m4s"}},
	// 裸 ADTS：没有容器，头两字节就是同步字。
	{name: "aac", magic: "\xff??", exts: []string{"aac"}},

	// FLAC 是唯一的例外：它压根不进注册表（flacdec/mygoflac 那个注册只有它
	// 自己的测试在用），注册表认不出头几个字节时由 openDecoder 落到 flacdec.Open。
	{name: "flac", via: "注册表认不出来时 audio.openDecoder 落到 flacdec.Open", exts: []string{"flac"}},
}

// TestKnownDecodersCoversEveryRegistration 是让上面这张表不腐烂的那道关：
// 把 audio 包里 init() 真的写进注册表的注册逐条扫出来（这正是应用运行时生效的
// 那一组），看 knownDecoders 是不是都有对应的一行。
//
// 没有这一条的话，上面那张表只是一份会过期的备忘录：加了新解码器又没想起来改表，
// 测试照样全绿，白名单照样漏掉那个格式——正是要防的那件事。
func TestKnownDecodersCoversEveryRegistration(t *testing.T) {
	live := registrationsInAppCode(t)
	if len(live) == 0 {
		t.Fatal("在 ../service/audio 里一条 RegisterFormat 都没扫到，多半是路径或解析方式错了")
	}
	for _, l := range live {
		if !knownDecoderHas(l.name, l.magic) {
			t.Errorf("应用按文件头 %q 注册了格式 %q，但 knownDecoders 里没有这一行——"+
				"补一行（连同它该有的 exts），否则文件框会悄悄漏掉这个格式", l.magic, l.name)
		}
	}
}

func knownDecoderHas(name, magic string) bool {
	for _, row := range knownDecoders {
		if row.name == name && row.magic == magic {
			return true
		}
	}
	return false
}

// TestRegistryRowsStillMatchTheRegistry 拿一段能命中 magic 的假文件头去问活的
// 注册表，看它报的格式名还是不是这里写的那一个。解码器被改名或删掉时红。
func TestRegistryRowsStillMatchTheRegistry(t *testing.T) {
	for _, row := range knownDecoders {
		if row.magic == "" {
			t.Logf("%s 不走注册表：%s", row.name, row.via)
			continue
		}
		got, err := probeFormatName(row.magic)
		if err != nil {
			// 这里要的不是「解得开」，只是「认得出」：喂的是 16 字节的假文件头，
			// open 失败是应该的，NewDecoder 失败时也会把名字一起带回来。
			t.Errorf("注册表不认识文件头 %q（%s）: %v", row.magic, row.name, err)
			continue
		}
		if got != row.name {
			t.Errorf("文件头 %q 认成了 %q，want %q", row.magic, got, row.name)
		}
	}
}

// TestAudioFiltersCoverEveryDecodableExtension 是本组测试的主心骨：注册表能解的
// 扩展名，文件框里必须选得到。
func TestAudioFiltersCoverEveryDecodableExtension(t *testing.T) {
	allowed := filterExtensions(audioFilters)
	for _, row := range knownDecoders {
		for _, ext := range row.exts {
			if !allowed[ext] {
				t.Errorf("%s（%s）能解 .%s，但文件框里选不到它", row.name, source(row), ext)
			}
		}
	}
}

// TestAudioFiltersHaveNothingWeCannotDecode 反过来钉一道：白名单里不该出现注册表
// 解不了的扩展名。写错一个字母、当年不支持的格式忘了删，都会在这里现形。
func TestAudioFiltersHaveNothingWeCannotDecode(t *testing.T) {
	decodable := map[string]bool{}
	for _, row := range knownDecoders {
		for _, ext := range row.exts {
			decodable[ext] = true
		}
	}
	for _, ext := range sortedKeys(filterExtensions(audioFilters)) {
		if !decodable[ext] {
			t.Errorf("文件框放行 .%s，但解码器注册表里没有它", ext)
		}
	}
}

// TestTheFirstFilterListsEverythingElse 核「音频文件」这一项确实是后面各项的并集。
// 常见的手滑是给某个格式单独加了一组，忘了它上面那个汇总项。
func TestTheFirstFilterListsEverythingElse(t *testing.T) {
	if len(audioFilters) == 0 {
		t.Fatal("一个分组都没有")
	}
	union := map[string]bool{}
	for _, f := range audioFilters[1:] {
		for _, e := range f.Extensions {
			if e != "*" {
				union[e] = true
			}
		}
	}
	got := sortedKeys(filterExtensions(audioFilters[:1]))
	want := sortedKeys(union)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("「%s」列的是 %v，单项分组的并集是 %v", audioFilters[0].Name, got, want)
	}
}

// TestTheCatchAllStaysLast 盯住「所有文件」：白名单只是把该看见的摆到前面，
// 兜底那一项没了就是把用户挡在门外，所以它必须在，而且必须在末位。
func TestTheCatchAllStaysLast(t *testing.T) {
	last := audioFilters[len(audioFilters)-1]
	if len(last.Extensions) != 1 || last.Extensions[0] != "*" {
		t.Errorf("末位的分组是 %v，不是「所有文件」", last)
	}
	for _, f := range audioFilters[:len(audioFilters)-1] {
		for _, e := range f.Extensions {
			if e == "*" {
				t.Errorf("分组「%s」里混进了 *，兜底项只该在末位出现一次", f.Name)
			}
		}
	}
}

// TestTheDialogLetsYouPickSeveralFiles 钉住多选。一次只能选一首的话，本地音乐
// 那种「把整个文件夹加进队列」就没法做。
func TestTheDialogLetsYouPickSeveralFiles(t *testing.T) {
	opts := audioOpenOptions(nil)
	if !opts.Multiple {
		t.Error("Multiple 是 false，文件框只能选一个")
	}
	if len(opts.Filters) != len(audioFilters) {
		t.Errorf("带出去 %d 个分组，白名单有 %d 个", len(opts.Filters), len(audioFilters))
	}
	if opts.Title == "" {
		t.Error("对话框没有标题")
	}
}

// ---- 辅助 ----

// filterExtensions 摊开一组分组的扩展名。
func filterExtensions(fs []mygo.FileFilter) map[string]bool {
	out := map[string]bool{}
	for _, f := range fs {
		for _, e := range f.Extensions {
			if e != "*" {
				out[strings.ToLower(e)] = true
			}
		}
	}
	return out
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// source 说清一个扩展名是从哪来的，好让失败信息能直接指到注册表那一行。
func source(row registryRow) string {
	if row.magic == "" {
		return row.via
	}
	return "文件头 " + row.magic
}

// registrationsInAppCode 扫出 audio 包（非递归，不含子包）里那些
// RegisterFormat 调用——这就是应用运行时真正生效的那一组注册。
//
// 扫源码而不是问注册表，是因为 MyGo 把注册表关在包内，没有任何枚举接口
// （audio.go 里只有 RegisterFormat 和 NewDecoder 两个入口）。子包不在范围内：
// extaf 不注册任何格式，flacdec/mygoflac 那个注册只有它自己的测试在用。
// MyGo 自带的 wav / mp3 同样不在扫出来的范围内，它们跟着 go.mod 里钉死的版本走。
func registrationsInAppCode(t *testing.T) []registryRow {
	t.Helper()
	files, err := filepath.Glob(filepath.Join("..", "service", "audio", "*.go"))
	if err != nil {
		t.Fatalf("找不到 audio 包: %v", err)
	}
	var rows []registryRow
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		parsed, err := parser.ParseFile(token.NewFileSet(), file, nil, 0)
		if err != nil {
			t.Fatalf("解析 %s: %v", file, err)
		}
		ast.Inspect(parsed, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "RegisterFormat" {
				return true
			}
			if len(call.Args) < 2 {
				t.Errorf("%s: RegisterFormat 的参数少于两个，knownDecoders 认不出来", file)
				return true
			}
			name, ok1 := stringLit(call.Args[0])
			magic, ok2 := stringLit(call.Args[1])
			if !ok1 || !ok2 {
				// 认不出来就大声说，别装作扫过了——静悄悄地漏掉才是这个测试要防的事。
				t.Errorf("%s: 这条 RegisterFormat 的名字或文件头不是字面量，"+
					"knownDecoders 认不出来，请改成字面量或手工核对一次", file)
				return true
			}
			rows = append(rows, registryRow{name: name, magic: magic})
			return true
		})
	}
	return rows
}

// stringLit 读一个字符串字面量（含 "…" 和 `…` 两种写法）。
func stringLit(e ast.Expr) (string, bool) {
	lit, ok := e.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	s, err := strconv.Unquote(lit.Value)
	return s, err == nil
}

// probeFormatName 拿一段能命中 magic 的假文件头去问注册表，返回它报的格式名。
//
// 只喂文件头、不给正文：'?' 在 match 里就是通配符，其余字节照抄，所以这段头一定
// 命中——命中的那条 open 必然失败，但我们只要 NewDecoder 在失败时也会一起返回的
// 名字（audio.go 里 return 的是 `nil, f.name, err`）。这样既不用造素材，也不碰
// 音频设备。
func probeFormatName(magic string) (string, error) {
	head := make([]byte, 16)
	copy(head, magic)

	d, name, err := mygoaudio.NewDecoder(bytes.NewReader(head))
	if d != nil {
		if c, ok := d.(io.Closer); ok {
			c.Close()
		}
	}
	if name == "" {
		return "", err
	}
	return name, nil
}

// ── 选封面的那个文件框 ─────────────────────────────────────────────────────

// TestImageFiltersMatchWhatTheCropperAccepts 选封面白名单和裁剪那一层能解的格式
// 必须对得上。
//
// ⚠️ 和上面音频那一对是同一个道理，只是那边有解码器注册表当活的标准，这边
// 没有——`internal/ui/covercrop.go` 的 `decodeCover` 用 `image.Decode`，而它的
// 注册器是 `image/jpeg` 和 `image/png` 两个包。白名单写了别的（用户选得到、
// 选完被拒），或者少了（用户根本选不到）。
func TestImageFiltersMatchWhatTheCropperAccepts(t *testing.T) {
	// 那边注册的是哪几个。改 `covercrop.go` 的 import 时记得改这里。
	decodable := map[string]bool{"jpg": true, "jpeg": true, "png": true}
	allowed := filterExtensions(imageFilters)

	for ext := range allowed {
		if !decodable[ext] {
			t.Errorf("文件框里能选 .%s，可裁剪那一层解不了它", ext)
		}
	}
	for ext := range decodable {
		if !allowed[ext] {
			t.Errorf("裁剪那一层能解 .%s，文件框里却选不到它", ext)
		}
	}
}

// TestImageFiltersKeepTheEscapeHatch 白名单里必须留着「所有文件」。
//
// ⚠️ 白名单只是把该看见的摆到前面，**不是拦路虎**。真有一张 `.webp` 的封面时
// （B 站自己的封面全是 `.webp`），拒掉它不如让用户选——选错了上层会说
// 「只支持 jpeg 或 png」，那句话指得准；而不给选，用户只会看到「怎么不能选
// 这张」。
func TestImageFiltersKeepTheEscapeHatch(t *testing.T) {
	if len(imageFilters) == 0 {
		t.Fatal("选封面的文件框一项都没有")
	}
	last := imageFilters[len(imageFilters)-1]
	if len(last.Extensions) != 1 || last.Extensions[0] != "*" {
		t.Errorf("最后一项是 %v，want 一项「所有文件」（Extensions 只有一个 *）", last.Extensions)
	}
}
