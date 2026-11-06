package platform

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

// 这一组盯的是「系统交给我们的文件」这份口径，以及它和 `mygo.json`、
// 和解码器白名单三者的**一致性**。
//
// ⚠️ **三处各记一份的代价是静默的**：系统按 `mygo.json` 那一份登记、框架按
// 同一份过滤启动参数、界面按 `AssociatedAudioExts` 判收不收。三份对不上的
// 症状是「双击一个 .mp3，应用起来了但什么也没发生」——而那和「应用坏了」
// 长得一模一样。所以下面几条钉的是**三份必须一样**。

// mygoFileAssocConfig 是 `mygo.json` 里本项目关心的那几段。
//
// ⚠️ **只解自己关心的字段**：整个 `mygo.json` 还有很多段（更新签名、
// macOS 的 Info.plist……），而那些各有各的测试盯着（见
// `internal/platform/datadir_test.go` 那一族）。
type mygoFileAssocConfig struct {
	Name       string `json:"name"`
	URLSchemes []string
	MacOS      struct {
		InfoPlist map[string]any `json:"infoPlist"`
	} `json:"macos"`
	Windows struct {
		InstallerInclude string `json:"installerInclude"`
	} `json:"windows"`
	FileAssociations []struct {
		Ext  []string `json:"ext"`
		Name string   `json:"name"`
		Role string   `json:"role"`
	} `json:"fileAssociations"`
}

// readMygoRaw 读仓库根上的 `mygo.json` 的原始字节。
//
// ⚠️ **有些字段要自己解**：NSIS include 的路径、InfoPlist 里那份
// CFBundleDocumentTypes——框架生成的东西（desktop entry、注册表脚本、
// Info.plist）是**把这份 JSON 原样翻过去**的，所以钉它就要读这份 JSON
// 本身，而不是再过一遍 `mygoFileAssocConfig` 那个 struct。
func readMygoRaw(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "mygo.json"))
	if err != nil {
		t.Fatalf("读不到 mygo.json：%v", err)
	}
	return b
}

// readMygoFileAssocConfig 读仓库根上的 `mygo.json`。
func readMygoFileAssocConfig(t *testing.T) mygoFileAssocConfig {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "mygo.json"))
	if err != nil {
		t.Fatalf("读不到 mygo.json：%v", err)
	}
	var c mygoFileAssocConfig
	if err := json.Unmarshal(b, &c); err != nil {
		t.Fatalf("解 mygo.json 失败：%v", err)
	}
	return c
}

// TestTheAssociatedExtensionsAreThePlayableOnes 钉住这一份口径的**内容**。
//
// ⚠️ **它必须等于「我们真放得出声」的那一份，而不是「扫描白名单」**：
// 扫描白名单里有 `.wma`，而我们没有 ASF 解码器（PLAN §5.4 点名的缺口）。
// 把 `.wma` 登记成文件关联的话，用户双击一首 WMA 会得到一句「这个格式我们
// 放不了」——比「双击没反应」更坏，因为那一句在说他用的文件有问题。
func TestTheAssociatedExtensionsAreThePlayableOnes(t *testing.T) {
	want := []string{"mp3", "flac", "wav", "m4a", "aac", "ogg", "aiff", "aif", "aifc"}
	if runtime.GOOS == "linux" {
		// Linux 上没有 AAC 解码器（`extaf` 只在 darwin、`mfadec` 只在
		// windows），登记了用户双击 .m4a 只会得到「这个格式我们放不了」。
		want = slices.DeleteFunc(slices.Clone(want), func(ext string) bool {
			return ext == "m4a" || ext == "aac"
		})
	}
	got := slices.Clone(AssociatedAudioExts)
	slices.Sort(got)
	wantSorted := slices.Clone(want)
	slices.Sort(wantSorted)
	if !slices.Equal(got, wantSorted) {
		t.Errorf("关联的扩展名是 %v，want %v", got, wantSorted)
	}
}

// TestLinuxDropsAACFromTheAssociation 钉住「Linux 这一份里没有 m4a / aac」。
//
// ⚠️ **Linux 上没有 AAC 解码器**：`extaf`（AudioToolbox）只在 darwin 上
// 有，`mfadec`（Media Foundation）只在 windows 上有——`open_other.go`
// 在非 Windows 上返回 `ErrUnsupported`。把 `.m4a` / `.aac` 登记给 Linux
// 桌面环境的话，`pudmusic.desktop` 的 `MimeType=` 会带着
// `application/x-pudmusic-m4a`，系统把 `.m4a` 列入本应用的「打开方式」——
// 用户双击得到的只有一句「这个格式我们放不了」。
//
// ⚠️ **这条断的是 `associatedAudioExtsForGOOS("linux")`**，不是当前
// `AssociatedAudioExts`：本测试在 macOS / Windows 上也要能跑——它钉的是
// 「另一台上那份长什么样」，而不是「本机这份对不对」。
func TestLinuxDropsAACFromTheAssociation(t *testing.T) {
	got := associatedAudioExtsForGOOS("linux")
	for _, ext := range []string{"m4a", "aac"} {
		if slices.Contains(got, ext) {
			t.Errorf("Linux 的登记扩展名里还有 %q：那台机器上没有 AAC 解码器，"+
				"登记了用户双击只会得到「放不了」", ext)
		}
	}
	// 其余格式一个都不少——Linux 上它们都放得起。
	for _, ext := range []string{"mp3", "flac", "wav", "ogg", "aiff", "aif", "aifc"} {
		if !slices.Contains(got, ext) {
			t.Errorf("Linux 的登记扩展名里少了 %q：那个格式 Linux 上放得起", ext)
		}
	}
}

// TestTheOtherPlatformsKeepAAC 钉住「macOS / Windows 的登记扩展名里
// 留着 m4a / aac」。
//
// ⚠️ **和上面那一条是同一张表的两半**：只钉「Linux 少了」而不钉「别的
// 平台没跟着少」的话，将来把 `associatedAudioExtsForGOOS` 改成「全平台都
// 裁」时这条不会红——而那个症状是「macOS / Windows 上双击 .m4a 也没反应」。
func TestTheOtherPlatformsKeepAAC(t *testing.T) {
	for _, goos := range []string{"darwin", "windows"} {
		got := associatedAudioExtsForGOOS(goos)
		for _, ext := range []string{"m4a", "aac"} {
			if !slices.Contains(got, ext) {
				t.Errorf("%s 的登记扩展名里少了 %q：那台机器上有 AAC 解码器", goos, ext)
			}
		}
	}
}

// TestWMAIsNotAssociated 单独钉住那一个缺口。
//
// 上一条钉的是「一份不多」，这一条钉的是「一个都不少」——WMA 是唯一一个
// 「扫描白名单里有、我们放不了」的扩展名，所以它值得单独一条：将来有人
// 把它加进关联列表时，红的是这一条（而不是上一条那种「多了一个」的笼统话）。
func TestWMAIsNotAssociated(t *testing.T) {
	if IsAssociatedAudioExt("wma") {
		t.Error("WMA 被登记成文件关联了：我们没有 ASF 解码器（PLAN §5.4），" +
			"用户双击一首 WMA 只会得到一句「这个格式我们放不了」")
	}
	if IsAssociatedAudioExt(".wma") {
		t.Error("带点的 .wma 也被认了：两种写法说的是同一个格式")
	}
}

// TestIsAssociatedAudioExtAcceptsBothSpellings 钉住「带点和不带点是同一个」。
//
// ⚠️ 调用方拿到的路径是文件名，扩展名那一截天然带着点（`filepath.Ext` 返回
// `.mp3`），而登记的那一份按 MyGo 的要求**不带点**（`mygo@v0.6.4`
// `cmd/mygo/config.go:109-110`）。只认一种写法的话，比的就是「`.mp3`」和
// 「`mp3`」——而它们是同一个格式。
func TestIsAssociatedAudioExtAcceptsBothSpellings(t *testing.T) {
	for _, ext := range []string{"mp3", ".mp3", ".MP3", "MP3", " .Mp3 "} {
		if !IsAssociatedAudioExt(ext) {
			t.Errorf("%q 不被认成关联格式", ext)
		}
	}
	for _, ext := range []string{"", ".", "txt", ".txt", "mp4", ".mp4", "m4s", "wav2"} {
		if IsAssociatedAudioExt(ext) {
			t.Errorf("%q 被认成关联格式了", ext)
		}
	}
}

// TestAudioExtOfTakesTheExtensionWithoutTheDot 钉住取扩展名的那一步。
func TestAudioExtOfTakesTheExtensionWithoutTheDot(t *testing.T) {
	cases := []struct {
		path string
		want string
	}{
		{"/Users/x/歌.mp3", "mp3"},
		{"/Users/x/歌.MP3", "mp3"},
		{"歌.flac", "flac"},
		{"/Users/x/没有扩展名", ""},
		// ⚠️ `.隐藏文件` 在 Go 里**是有扩展名的**（`filepath.Ext` 认的是
		// 「最后一段里最后一个点之后的那一截」，而那个点就在开头）——这不是我们
		// 的口径，是标准库的口径，而 `filepath.Ext` 正是我们取扩展名用的那个。
		{"/Users/x/.隐藏文件", "隐藏文件"},
		{"/Users/x/带两个点.a.b", "b"},
	}
	for _, c := range cases {
		if got := AudioExtOf(c.path); got != c.want {
			t.Errorf("AudioExtOf(%q) = %q，want %q", c.path, got, c.want)
		}
	}
}

// TestMygoJSONDeclaresTheSharedExtensions 钉住 `mygo.json` 的
// `fileAssociations` 里只剩**全平台都放得起**的格式。
//
// ⚠️ **这一条是这一组里最要紧的**：mygo.json 的 fileAssociations 是
// **一份三个平台共用**的——macOS 的 CFBundleDocumentTypes、Linux 的
// desktop entry `MimeType=`、Windows 安装包的注册表 `Software\Classes`
// 都从同一份生成。把 m4a / aac 留在里面的话，Linux 的 `.desktop` 会把
// 它们列进 `MimeType=`，系统就把本应用放进 `.m4a` 的「打开方式」——而
// Linux 上**没有 AAC 解码器**，用户双击得到的只有「这个格式我们放不了」。
//
// ⇒ 所以这份 JSON 里的扩展名**必须等于「全平台都放得起」那一份**
// （`associatedAudioExtsForGOOS("linux")`，也就是最窄的那份）；AAC/M4A
// 由 macOS 的 InfoPlist 和 Windows 的 installerInclude **各自补回**
// （见下面两条）。
func TestMygoJSONDeclaresTheSharedExtensions(t *testing.T) {
	c := readMygoFileAssocConfig(t)
	if len(c.FileAssociations) == 0 {
		t.Fatal("mygo.json 里没有 fileAssociations：系统不知道本应用开哪些文件，" +
			"双击一个音频只会用别的应用打开")
	}
	var declared []string
	for _, fa := range c.FileAssociations {
		declared = append(declared, fa.Ext...)
	}
	slices.Sort(declared)
	want := associatedAudioExtsForGOOS("linux")
	slices.Sort(want)
	if !slices.Equal(declared, want) {
		t.Errorf("mygo.json 登记的扩展名是 %v，全平台都放得起的那一份是 %v——"+
			"两份对不上时，Linux 的 desktop entry 会把放不了的格式登记给系统",
			declared, want)
	}
}

// TestMygoJSONKeepsAACOutOfTheLinuxDesktopEntry 单独钉住「Linux 不登记
// m4a / aac」。
//
// ⚠️ 上一条断「一份不多不少」，这一条钉**那两个特定扩展名不在**：Linux
// 的 `.desktop` 由这份 JSON 直接翻出来（mygo `build.go` 的
// `writeLinuxDesktop`），MIME 类型跟着扩展名走——aAc / m4a 在里面的话
// 桌面条目里就有 `application/x-pudmusic-m4a`，系统按它把 `.m4a` 交给
// 本应用，而 Linux 上没有解码器。
func TestMygoJSONKeepsAACOutOfTheLinuxDesktopEntry(t *testing.T) {
	c := readMygoFileAssocConfig(t)
	for _, fa := range c.FileAssociations {
		for _, ext := range fa.Ext {
			if ext == "m4a" || ext == "aac" {
				t.Errorf("mygo.json 的 fileAssociations 里还有 %q：Linux 的"+
					"desktop entry 会按它登记 %q，而 Linux 上没有 AAC 解码器",
					ext, ext)
			}
		}
	}
}

// TestMacOSInfoPlistKeepsTheFullAssociation 钉住 macOS 在
// `macos.infoPlist.CFBundleDocumentTypes` 里补回了**完整的**扩展名
// （含 m4a / aac）。
//
// ⚠️ **机制是「键替换」**：框架的 `bundle.go` 从 `fileAssociations` 生成
// `CFBundleDocumentTypes` 之后，再拿 `macos.infoPlist` 的键**整个换掉**
// 生成的那份（`d[k] = v // the app's own keys win`）——所以这份数组里
// 必须是**全量**（含 m4a / aac），只写那两个的话 macOS 就只剩那两个格式。
func TestMacOSInfoPlistKeepsTheFullAssociation(t *testing.T) {
	c := readMygoFileAssocConfig(t)
	raw, ok := c.MacOS.InfoPlist["CFBundleDocumentTypes"]
	if !ok {
		t.Fatal("macos.infoPlist 里没有 CFBundleDocumentTypes：mygo.json 移走" +
			" m4a/aac 之后，macOS 就没有任何文件关联了")
	}
	docs, ok := raw.([]any)
	if !ok || len(docs) == 0 {
		t.Fatalf("CFBundleDocumentTypes 不是数组（%T）", raw)
	}
	var exts []string
	for _, d := range docs {
		m, ok := d.(map[string]any)
		if !ok {
			continue
		}
		list, ok := m["CFBundleTypeExtensions"].([]any)
		if !ok {
			continue
		}
		for _, e := range list {
			if s, ok := e.(string); ok {
				exts = append(exts, s)
			}
		}
	}
	slices.Sort(exts)
	want := associatedAudioExtsForGOOS("darwin")
	slices.Sort(want)
	if !slices.Equal(exts, want) {
		t.Errorf("macOS InfoPlist 里登记的扩展名是 %v，darwin 那一份是 %v",
			exts, want)
	}
}

// TestWindowsInstallerIncludeRegistersAAC 钉住 Windows 安装包通过
// `windows.installerInclude` 补回 m4a / aac 的注册表关联。
//
// ⚠️ **只钉「这个文件存在、而且里面写着那两个扩展名」**：NSIS 脚本对不对
// 只能在打了安装包的 Windows 上验（见 `scripts/build.sh`），这里钉的是
// 「配置还指着它、它还没被删」。
func TestWindowsInstallerIncludeRegistersAAC(t *testing.T) {
	c := readMygoFileAssocConfig(t)
	if c.Windows.InstallerInclude == "" {
		t.Fatal("mygo.json 里没有 windows.installerInclude：Windows 安装包" +
			"就没有任何 AAC/M4A 关联了")
	}
	path := filepath.Join("..", "..", c.Windows.InstallerInclude)
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读不到 %s：%v", c.Windows.InstallerInclude, err)
	}
	text := string(b)
	for _, ext := range []string{"m4a", "aac"} {
		if !strings.Contains(text, `"`+ext+`"`) && !strings.Contains(text, "."+ext) {
			t.Errorf("%s 里没有 %q：Windows 安装包不会登记那个扩展名",
				c.Windows.InstallerInclude, ext)
		}
	}
}

// TestMygoJSONExtensionsCarryNoDot 钉住「扩展名一律不带点」。
//
// ⚠️ **带点会被 MyGo 的配置校验器直接拒绝**（`mygo@v0.6.4`
// `cmd/mygo/config.go:242-249`，那个 `extRe` 是 `^[a-zA-Z0-9][a-zA-Z0-9_+-]*$`），
// 于是 `mygo build` 在读配置那一步就失败——**而失败发生在打包时，不是在
// 运行时**，症状是「打不出包」或者「打出来的包没有文件关联」。
func TestMygoJSONExtensionsCarryNoDot(t *testing.T) {
	c := readMygoFileAssocConfig(t)
	for i, fa := range c.FileAssociations {
		if len(fa.Ext) == 0 {
			t.Errorf("mygo.json 的 fileAssociations[%d] 一个扩展名都没有", i)
		}
		for _, ext := range fa.Ext {
			if strings.HasPrefix(ext, ".") {
				t.Errorf("mygo.json 的 fileAssociations[%d] 里的 %q 带着点："+
					"MyGo 的配置校验器会拒绝它（extRe），打包在读配置那一步就失败",
					i, ext)
			}
			if ext != strings.ToLower(ext) {
				t.Errorf("mygo.json 的 fileAssociations[%d] 里的 %q 不是小写", i, ext)
			}
		}
	}
}

// TestMygoJSONStillDeclaresTheURLScheme 钉住「加文件关联没有把 URL scheme
// 弄掉」。
//
// ⚠️ 这一条看着像废话，而它防的是一件真会发生的错：`mygo.json` 是一份手改
// 的 JSON，加字段的时候很容易整段重写——而 `urlSchemes` 没了的症状是
// **「点 pudmusic:// 链接唤不起应用」**，那和这一票的文件关联毫无关系，评审时
// 会被算成「别的地方坏了」。
func TestMygoJSONStillDeclaresTheURLScheme(t *testing.T) {
	c := readMygoFileAssocConfig(t)
	if !slices.Contains(c.URLSchemes, URLScheme) {
		t.Errorf("mygo.json 的 urlSchemes 是 %v，没有 %q：点链接唤不起本应用",
			c.URLSchemes, URLScheme)
	}
}

// TestTheFrameworkFiltersCommandLineFilesByTheseExtensions 钉住「框架按什么
// 过滤命令行里的文件」。
//
// ⚠️ **这一条是这一组里最容易被漏掉的一环**：框架把启动参数里的文件交给
// `OnOpenFile` 之前，先按 `packageFileExtensions` 过滤一遍
// （`mygo@v0.6.4` `mygo/deeplink.go` 的 `fileArgs`）——那个值是
// `mygo build` 用 `-ldflags -X` 塞进去的，**来自 `mygo.json` 的
// `fileAssociations`**。
//
// 而「框架过滤的那一份」和「界面判收不收的那一份」**不是同一处代码**：
// 前者在框架里（我们碰不到），后者在这里。所以这一条量的是**我们那份
// `mygo.json` 是不是真的能被框架读成扩展名列表**。
//
// ⚠️ **它量的方式是把框架的那段逻辑照抄一遍**：框架的 `fileArgs` 是包内
// 未导出的，我们没法在测试里调它（`go:linkname` 那条路在这个仓库里不合适）。
// 而照抄的那一段只有三行——**扩展名列表、大小写归一、剥掉点**——照抄错了
// 的风险极低，而漏掉这一环的风险是「所有文件都收不到」。
func TestTheFrameworkFiltersCommandLineFilesByTheseExtensions(t *testing.T) {
	c := readMygoFileAssocConfig(t)

	// 框架那一段：`strings.Split(packageFileExtensions, ",")`，每一项跟
	// `strings.TrimPrefix(strings.ToLower(filepath.Ext(p)), ".")` 比。
	var frameworkExts []string
	for _, fa := range c.FileAssociations {
		for _, ext := range fa.Ext {
			frameworkExts = append(frameworkExts, strings.ToLower(ext))
		}
	}

	// 一个 `.flac` 和一个 `.wma`：前者要收，后者不收。
	dir := t.TempDir()
	flac := filepath.Join(dir, "一首歌.flac")
	wma := filepath.Join(dir, "老歌.wma")
	for _, p := range []string{flac, wma} {
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// 照抄框架那三行。
	wouldBeOpened := func(p string) bool {
		ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(p)), ".")
		return slices.Contains(frameworkExts, ext)
	}

	if !wouldBeOpened(flac) {
		t.Errorf("框架不会把 %q 交给 OnOpenFile：启动命令行里的文件全被丢掉，"+
			"而双击一个文件的表现正是「应用起来了什么也没放」", flac)
	}
	if wouldBeOpened(wma) {
		t.Errorf("框架会把 %q 交给 OnOpenFile：我们放不了那个格式", wma)
	}
	// 框架那一段还要过「是不是普通文件」这一关（`fileArgs` 里 stat 一下）。
	// 所以「一个目录」在启动命令行里也进不来——而 macOS 的 Apple Event
	// 那一条**不过这一关**，那一档由界面层的 `checkOpenFile` 挡。
}
