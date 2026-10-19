package iconset

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 这一族盯的是**资源本身**，不是「界面上画了哪一枚」。
//
// # 为什么数量要钉住
//
// 视觉调研记的是「图标资源 26 : 105」——上游用了约 105 个 remixicon，我们那时
// 只有 26 个。这个数是这一票的一半验收（「数量按上游 105 个对」），而**数量是
// 最容易悄悄退回去的一样东西**：删一个图标、合并两个常量，测试全绿，而界面上
// 就是少了一枚。
//
// ⚠️ 钉的是**文件数**而不是 `iconNames` 的长度：两个常量可以指同一个文件
// （`iconPageList` 和 `iconQueue` 就是同一个 `Editor/list-radio.svg`），而
// `icons.go` 那张表能不能覆盖住盘上每一个文件，是另一条要断的事（见下）。

// buiIconCount 是上游那一批的大小，105。
//
// ⚠️ **「约 105」是个估数**（调研那一行写的是「用了约 105 个 remixicon」），
// 所以这一条断的是「不小于 105」而不是「正好 105」：我们这一票备到 107 个，
// 多出来的那两个是界面上确实有落点而上游那张图里数不清的（「反馈」和「回顶」
// 那一枚向上箭头）。断成「等于」的话，往后加一枚正确的图标就得改这个数。
const buiIconCount = 105

// TestTheIconSetIsNotSlimmerThanBiu 钉住「量」：文件数不小于上游那 105 个。
//
// 🔴 这条断的是**目录里的 SVG 文件数**，不是 `iconNames` 的长度、也不是资源能
// 不能解析——那两样坏了各有别的测试管（`TestEveryIconWeNamedIsThereAndItParses`
// 和界面层那个）。这一条只管数量：删掉三个文件而把 `iconNames` 里的名字留下，
// 那一条会红（读不到），而**把三个名字一并删掉**的话只有这一条会红。
func TestTheIconSetIsNotSlimmerThanBiu(t *testing.T) {
	got := countIconFiles(t)
	if got < buiIconCount {
		t.Errorf("图标资源只有 %d 个 SVG，上游那一批是 %d 个——"+
			"这一票做的就是补齐这件事，别让它悄悄退回去", got, buiIconCount)
	}
	t.Logf("图标资源 %d 个（上游约 %d）", got, buiIconCount)
}

// TestTheIconDirectoryIsOrganizedLikeUpstream 钉住「目录结构照上游组织」。
//
// 上游 RemixIcon 是按分类分的（`Media/`、`Arrows/`、`System/` …），不是一个
// 拍平的目录。拍平之后既认不出某个图标属于哪一类，加图标时也没法照着上游的
// 目录找——而「照着上游找」正是这一批图标唯一的来源。
//
// ⚠️ 判据是「每个文件都在某个分类目录下」而不是「有几个分类」：分类会随图标
// 增加而变多，而「有没有躺在分类目录之外」是那条真正要防的事。
func TestTheIconDirectoryIsOrganizedLikeUpstream(t *testing.T) {
	root := filepath.Join("icons")
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("读不到 %s，这条就没法查了：%v", root, err)
	}
	if len(entries) == 0 {
		t.Fatalf("%s 下面一个分类都没有", root)
	}

	flat := 0
	for _, e := range entries {
		if !e.IsDir() {
			flat++
		}
	}
	if flat > 0 {
		t.Errorf("%s 底下有 %d 个文件直接躺着——图标按上游那样放分类目录里"+
			"（上游那两千多个就是那么分的），别拍平", root, flat)
	}

	for _, cat := range entries {
		if !cat.IsDir() {
			continue
		}
		n := 0
		_ = filepath.WalkDir(filepath.Join(root, cat.Name()), func(_ string, d os.DirEntry, _ error) error {
			if !d.IsDir() && strings.HasSuffix(d.Name(), ".svg") {
				n++
			}
			return nil
		})
		if n == 0 {
			t.Errorf("分类目录 %s 下面是空的——要么删掉它，要么把图标放进去", cat.Name())
		}
		t.Logf("  %-16s %d 个", cat.Name(), n)
	}
}

// TestEveryIconIsNamedByItsCategoryAndFile 钉住「图标名 = 上游的『分类/文件名』」。
//
// 那个约定是「任何一个图标都能反查回上游哪一个」的凭据（见 `internal/ui/icons.go`
// 的文件头）。违反它的写法在画面上**看不出任何异样**——`Media/play.svg` 和
// `Media/Play.svg` 都能画出来——所以只能靠机器查。
func TestEveryIconIsNamedByItsCategoryAndFile(t *testing.T) {
	root := filepath.Join("icons")
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, cat := range entries {
		if !cat.IsDir() {
			continue
		}
		files, err := os.ReadDir(filepath.Join(root, cat.Name()))
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range files {
			name := f.Name()
			if !strings.HasSuffix(name, ".svg") {
				t.Errorf("%s/%s 不是 .svg", cat.Name(), name)
				continue
			}
			// 上游的文件名全是小写加连字符：`close-circle-line.svg`、
			// `arrow-up-s-line.svg`、`account-circle-line.svg`。
			if name != strings.ToLower(name) {
				t.Errorf("%s/%s 里有大写字母——上游的文件名全是小写的，"+
					"照抄才反查得回去", cat.Name(), name)
			}
			if _, err := Load(cat.Name() + "/" + name); err != nil {
				t.Errorf("%s/%s 在盘上却取不出来：%v", cat.Name(), name, err)
			}
		}
	}
}

// TestLoadCachesSoTheSameNameIsTheSamePointer 钉住「同一个名字永远是指针同一个」。
//
// 为什么这一条要紧：`ui.Icon` 把 `*ui.SVG` 存进元素，而元素每帧重建——同一个
// 名字每帧换一个 doc 的话，光栅化的遮罩每帧都进不了缓存（`mygo/ui/svg.go` 的
// `drawIcon` 的注释：once there, the GPU draws the icon in any color without
// drawing its shapes again）。而「没进缓存」在界面上只是慢一点，**看不出坏了**。
func TestLoadCachesSoTheSameNameIsTheSamePointer(t *testing.T) {
	first, err := Load("Media/image-line.svg")
	if err != nil {
		t.Fatal(err)
	}
	second, err := Load("Media/image-line.svg")
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Error("同一个名字两次取出来不是同一个指针——光栅化结果进不了缓存，" +
			"每一帧都要把那张 SVG 再画一遍")
	}
}

// TestLoadRemembersTheFailure 钉住「失败也只试一次」。
//
// 一个读不到的名字每次 `Load` 都去读一次 embed，而 embed 的读失败要走一遍
// 文件系统查找——界面层是启动期整批过一遍（那里失败当场 panic），而控件层是
// 每帧可能问一次。缓存失败省掉的是那一遍，而且更重要的是**同一个坏名字拿到
// 的是同一个错**，两次调用给出两句不同的话没法排查。
func TestLoadRemembersTheFailure(t *testing.T) {
	const bad = "Media/没有这一个.svg"
	_, first := Load(bad)
	if first == nil {
		t.Fatal("读不到的名字居然没有报错")
	}
	_, second := Load(bad)
	if first.Error() != second.Error() {
		t.Errorf("同一个坏名字两次给出的错不一样：\n%v\n%v", first, second)
	}
	// 而它**不能**被当成一张空图标交出去：画出来是一格空白，而那和
	// 「根本没画」在画面上长得一模一样。
	if svg, err := Load(bad); err == nil && svg != nil {
		t.Errorf("读不到的名字返回了一张图：%v", svg)
	}
}

// TestEveryResourceIsMonochrome 钉住「图标是单色的，跟着主题走」。
//
// MyGo 的 `ui.Icon` 画的是**形状本身**，颜色从祖先的文字色继承（`mygo/ui/
// svg.go` 的 drawIcon 收的就是文字色）。资源里写死 `fill="#ff0000"` 的后果不是
// 显示错，而是**多了一份永远不生效的东西**，而且下一个人会以为「这个图标是红色
// 的所以它有含义」。
//
// 合法的那一份是 `fill="currentColor"`：那正是「跟着文字色」的意思。
//
// ⚠️ 这一条扫的是**目录里的每一个文件**，不是 `iconNames`——新加一枚而忘了改
// 那张表，界面层那条（`icons_test.go`）管不到，而漏进来的图标照样会被画出去。
func TestEveryResourceIsMonochrome(t *testing.T) {
	for _, name := range allIconNames(t) {
		b, err := Source(name)
		if err != nil {
			t.Errorf("图标 %q 读不到：%v", name, err)
			continue
		}
		src := string(b)
		for _, bad := range []string{`fill="#`, `stroke="#`, `fill="rgb`, `stop-color`} {
			if strings.Contains(src, bad) {
				t.Errorf("图标 %q 里写死了颜色（%q）——Icon 画的是文字色，"+
					"写死的那一份会被忽略，只留下一份永远不生效的说明", name, bad)
			}
		}
	}
}

// TestEveryResourceHasASize 钉住「每一张都有尺寸」。
//
// `ui.ParseSVG` 对一份既没有 viewBox 也没有 width/height 的文件返回 0×0，而
// 那画出来是**一格空白**——和「根本没画出来」在画面上长得一模一样（`icons_test.go`
// 顶上那一段记着同一个坑，那边断的是命名过的那一批，这边断的是盘上的每一张）。
func TestEveryResourceHasASize(t *testing.T) {
	for _, name := range allIconNames(t) {
		if _, err := Load(name); err != nil {
			t.Errorf("图标 %q 取不出来：%v", name, err)
		}
	}
}

// allIconNames 列出 icons/ 底下的每一个 SVG（相对 icons/ 的「分类/文件名」）。
func allIconNames(t *testing.T) []string {
	t.Helper()
	var out []string
	cats, err := os.ReadDir("icons")
	if err != nil {
		t.Fatalf("读不到 icons 目录：%v", err)
	}
	for _, cat := range cats {
		if !cat.IsDir() {
			continue
		}
		files, err := os.ReadDir(filepath.Join("icons", cat.Name()))
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range files {
			if strings.HasSuffix(f.Name(), ".svg") {
				out = append(out, cat.Name()+"/"+f.Name())
			}
		}
	}
	if len(out) == 0 {
		t.Fatal("icons/ 底下一个 SVG 都没有——这一条要么是路径错了，要么是资源真被删了")
	}
	return out
}

// countIconFiles 数 icons/ 底下的 SVG 一共有多少张。
func countIconFiles(t *testing.T) int {
	t.Helper()
	return len(allIconNames(t))
}
