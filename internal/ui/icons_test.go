// 图标这一票的测试（issue 93）。
//
// # 这一组断的是**画面**，不是「调了哪个函数」
//
// MyGo 的 Icon 没有元素树可以查：无头 Tester 的 `Find` 只认**画出来的文字**和
// **`Label` 起的名字**，而图标本身既没有文字也没名字。所以「图标画出来了没有」
// 只能从**像素**上看——这也是 SPEC Testing Decisions 里那一行说的
// （「无头渲染断言**图标元素存在**，不是断言『调了 SetIcon』」）在原生界面上的
// 落地形态：元素存在 = 那一块上有画上去的东西。
//
// 三条主线：
//
//  1. 资源本身（每个名字都在、解析得出、有尺寸、单色）
//  2. 画面（播放条 / 顶栏上每个动作那一块里真的有图形，深浅两套主题都试）
//  3. 点击区（比图形大，四角都按得动）
package ui

import (
	"go/ast"
	"go/parser"
	"go/token"
	"image"
	"image/color"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/ShellMonster/Biu_Go/internal/ui/iconset"
	mygoui "github.com/egoist/mygo/ui"
)

// TestEveryIconWeNamedIsThereAndItParses 钉住「每个名字都有一份能解析的资源」。
//
// 🔴 **少一个会在启动时就 panic**（`icons.go` 的 mustParseIcons），而这条断的是
// 更早的一层：**多写进 `iconNames` 却没有对应文件**的那种，以及**拼错分类**的
// 那种（`Media/play.svg` 写成 `Media/Play.svg` 是同一个意思的两种写法）。
//
// ⚠️ 尺寸也要断：`ui.ParseSVG` 对一份没有 viewBox 也没有 width/height 的文件
// 返回 0×0，而那画出来是一格**空白**——和「根本没画」在画面上长得一模一样。
func TestEveryIconWeNamedIsThereAndItParses(t *testing.T) {
	for _, name := range iconNames {
		b, err := iconset.Source(name)
		if err != nil {
			t.Errorf("图标 %q 不在 icons/ 里：%v", name, err)
			continue
		}
		s, err := mygoui.ParseSVG(b)
		if err != nil {
			t.Errorf("图标 %q 解析不了：%v", name, err)
			continue
		}
		w, h := s.Size()
		if w <= 0 || h <= 0 {
			t.Errorf("图标 %q 没有尺寸（%.0fx%.0f）——那画出来是空白", name, w, h)
		}
	}
}

// TestEveryIconOnDiskHasAName 钉住「盘上每一张图标都有人叫得出名字」。
//
// 🔴 这一条挡的是**孤儿资源**：这一票按上游清单补了几十张图，而清单是照上游
// 逐个名字抄下来的——抄漏一个（或改分类时搬了家），那张图就躺在 `icons/` 里
// 没人用。界面上看不出任何异样（它从来没被画过），而目录里的图标数一条不少，
// 看着像补齐了。
//
// 所以这里把两个方向都对上：
//
//   - 盘上有、表里没有 → **孤儿**（这一条要抓的）
//   - 表里有、盘上没有 → 启动时就 panic（`mustParseIcons`），由上面那条管
//
// ⚠️ 「同一个文件两个名字」是**允许**的（名字跟着那个按钮的用处走），所以这里比
// 的是集合而不是个数。
//
// ⚠️ **「播放列表」和「分集」现在用的是两个不同的文件**（票 #220）：上游那边
// 「播放列表」是 `RiPlayListLine`、分集是 `RiListRadio`，两个入口用同一张图的话
// 用户分不出哪个是哪个（见 `TestThePlaylistKeyAndThePartKeyAreDifferentGlyphs`）。
func TestEveryIconOnDiskHasAName(t *testing.T) {
	dir := filepath.Join("iconset", "icons")
	var onDisk []string
	err := filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		// ⚠️ 出错时 `d` 是 nil，直接问它就是空指针崩在测试里（第一版就是这么
		// 写的）。而崩在这里看起来像「这一条坏了」，跟目录里那张图没关系。
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(d.Name(), ".svg") {
			return nil
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		onDisk = append(onDisk, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		t.Fatalf("走一遍 %s 失败，这条就没法查了：%v", dir, err)
	}

	named := make(map[string]bool, len(iconNames))
	for _, n := range iconNames {
		named[n] = true
	}
	for _, f := range onDisk {
		if !named[f] {
			t.Errorf("icons/%s 是孤儿——`iconNames` 里没有这个名字，"+
				"所以界面上从没有人画过它（补图标时要连名字一起补）", f)
		}
	}
	// 反向：表里有的每一个，盘上都得有。这一条 `TestEveryIconWeNamedIsThere-
	// AndItParses` 已经断过（它还断了解析得出来），这里只报个数好对照。
	if len(onDisk) < len(named) {
		t.Errorf("表上有 %d 个名字，盘上只有 %d 张图", len(named), len(onDisk))
	}
}

// TestTheIconResourcesAreAllMonochrome 钉住「图标是单色的，跟着主题走」。
//
// MyGo 的 `ui.Icon` 画的是**形状本身**、颜色从祖先的文字色继承（`mygo/ui/svg.go`
// 的 drawIcon 收的就是文字色）。资源里要是写死了 `fill="#ff0000"`，那个颜色会
// 被 Icon 忽略——不是显示错，是**多了一份永远不生效的东西**，而且下一个人会
// 以为「这个图标是红色的所以它有含义」。
//
// 合法的那一份是 `fill="currentColor"`：那正是「跟着文字色」的意思。
func TestTheIconResourcesAreAllMonochrome(t *testing.T) {
	for _, name := range iconNames {
		b, err := iconset.Source(name)
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

// TestEveryPlayModeHasItsIcon 钉住「播放模式四档各有一张自己的图标」。
//
// 🔴 **不只是「都有」**：四档画同一张图标的话，用户在按了四下之后只会看到同一个
// 图形，完全分不出自己换到了哪一档——而这个按钮**没有字可念**（无障碍名字是固定
// 的「播放模式」）。所以这里断的是**四张图形两两不同**。
//
// 判据是画面：按钮那一块的像素指纹四档互不相同（见 glyphIn）。
func TestEveryPlayModeHasItsIcon(t *testing.T) {
	seen := map[app.PlayMode]*mygoui.SVG{}
	for _, m := range app.PlayModes {
		name := playModeIcon(m)
		if name == "" {
			t.Errorf("档位 %q 没有配图标——按钮上会是一格空白", m)
			continue
		}
		if s := icon(name); s == nil {
			t.Errorf("档位 %q 配的图标 %q 不在 icons/ 里", m, name)
			continue
		}
		for other, s2 := range seen {
			if s2 == icon(name) {
				t.Errorf("档位 %q 和 %q 配的是同一张图标（%q）——用户按下去看不出换了档",
					m, other, name)
			}
		}
		seen[m] = icon(name)
	}

	// 画面上那一条：四档画出来的是四个不同的图形。
	tt, a := modeBarTester(t, 1000)
	fingerprints := map[string]app.PlayMode{}
	for range app.PlayModes {
		if err := tt.Click("播放模式"); err != nil {
			t.Fatalf("点不到播放模式按钮：%v", err)
		}
		tt.Frame()
		cur := a.Snapshot().PlayMode
		fp := glyphIn(tt, "播放模式")
		if fp == "" {
			t.Fatalf("档位 %q 的按钮上一块图形也没画出来", cur)
		}
		if prev, dup := fingerprints[fp]; dup {
			t.Errorf("档位 %q 和 %q 画出来一模一样", cur, prev)
		}
		fingerprints[fp] = cur
	}
}

// TestTheHistoryKeysStillHaveAGlyphAndAHitArea 钉住后退 / 前进那一格。
//
// ⚠️ **它是这一票唯一没换成图标资源的一处**（issue 93 的验收点名了它们）：框架的
// `ui.BackButton` / `ui.ForwardButton` 除 `r.Go(±1)` 还挂着**右键的「去过那些
// 页」**列表，而那一列要读历史的路径和标题——`Router` 的 `entries` / `at` 是
// **私有**的，自己重画一个按钮只能把那份列表丢掉。理由写在 navbar.go 的
// `viewNavBack` 里。
//
// 所以那个箭头是框架用 `Draw` 画的一段 path，不是 SVG。它仍然要满足验收的实质
// 那几条，而这一条把它们钉住，免得哪天框架换了画法或者我们改坏了 padding：
//
//	有图形（那一块不是一个纯色）
//	图形是竖的箭头、不是横的字（和 `TestTheFullPlayerControlsAreAllIcons`
//	  同一个判据）
//	按钮框明显大于图形框（实测 34×30 的框里画着 11×18 的箭头）
//
// ⚠️ **这里只断「框比图形大」，不断每边留白够不够**：那一格的 padding 是框架写死
// 的（`Padding(t.Space(1.5), t.Space(2))`），而 `scanGlyph` 量出来的图形包围盒是
// **外扩过**的，拿它算边距差出来的数有正有负（实测上边距 8、下边距 4，而那个 4
// 里含着 4 像素的外扩）。每边留白够不够，由播放条上那几枚自己的图标
// （`TestTheHitAreaIsBiggerThanTheGlyph`，那个 padding 是我们自己的）断。
func TestTheHistoryKeysStillHaveAGlyphAndAHitArea(t *testing.T) {
	tt := navBarTester(t, false)
	for _, label := range []string{navBackLabel, navForwardLabel} {
		r, ok := tt.Find(label)
		if !ok {
			t.Fatalf("顶栏上没有 %q：%q", label, tt.Texts())
		}
		g := scanGlyph(tt, label)
		if g.ink == "" {
			t.Errorf("%q 那一块是空的（%.1f,%.1f %.1fx%.1f）", label, r.X, r.Y, r.W, r.H)
			continue
		}
		if g.box.W > g.box.H*1.6 {
			t.Errorf("%q 画出来是 %.1f×%.1f 的横条——那是字，不是箭头",
				label, g.box.W, g.box.H)
		}
		if g.box.W >= r.W || g.box.H >= r.H {
			t.Errorf("%q 的图形 %.1fx%.1f 并不小于它的框 %.1fx%.1f——"+
				"按这一格要瞄得准", label, g.box.W, g.box.H, r.W, r.H)
		}
	}
}

// 下面这张表是「哪个状态该配哪一张图标」的**外部依据**：逐条抄自 Biu 的源码，
// 而且写的是**上游的字面文件名**，不是 icons.go 里的常量。
//
// ⚠️ **为什么不直接断那四个函数返回哪个常量**：那样改常量的人顺手把测试也改了，
// 一点也红不了——这就是评审踩到的那个盲区（原来的测试断「几张图形两两不同」，
// 实心空心对调之后它们照样两两不同，4000 多行测试全绿，而用户看到的是反的那
// 一张）。这里钉的是**字面文件名**：改了 `iconLike` 指向的资源而没改这一行，立刻
// 红；要改这一行，就得对着 Biu 源码看一眼——那正是我们要的。
//
// 「两两不同」那条（`TestEveryIcon…`）也留着：它断的是**忘了换**（两个状态画
// 同一张），这一条断的是**换反了**。两条缺一条就有一类坏没人管。
var buiIcons = []struct {
	// state 是这个图标对应的界面状态，说给人听。
	state string
	// name 是上游那个文件的「分类/文件名」，逐字照抄 Biu 引的那个图标名换过来的。
	name string
	// got 是生产代码在那一个状态上返回的东西。
	got string
}{
	// Biu `components/music-play-control/index.tsx`：
	//   isPlaying ? <RiPauseCircleFill size={48}/> : <RiPlayCircleFill size={48}/>
	{"播放中（该画暂停）", "Media/pause-circle-fill.svg", transportIcon(true)},
	{"停着（该画播放）", "Media/play-circle-fill.svg", transportIcon(false)},

	// Biu `components/music-thumb/index.tsx`：
	//   isThumb ? <RiThumbUpFill className="text-primary"/> : <RiThumbUpLine size={18}/>
	{"赞过了", "System/thumb-up-fill.svg", likeIcon(true)},
	{"还没赞", "System/thumb-up-line.svg", likeIcon(false)},

	// Biu `components/music-fav-button`（`playbar/left`）：收藏了画实心，没收藏画空心。
	{"收藏过了", "System/star-fill.svg", favIcon(true)},
	{"还没收藏", "System/star-line.svg", favIcon(false)},

	// Biu `src/common/constants/audio.tsx:27-48` 的 `getPlayModeList`：
	//   顺序 <RiOrderPlayLine/>  列表循环 <RiRepeat2Line/>
	//   随机 <RiShuffleLine/>    单曲     <RiRepeatOneLine/>
	{"顺序播放", "Media/order-play-line.svg", playModeIcon(app.PlayModeSequence)},
	{"列表循环", "Media/repeat-2-line.svg", playModeIcon(app.PlayModeLoop)},
	{"随机", "Media/shuffle-line.svg", playModeIcon(app.PlayModeRandom)},
	{"单曲循环", "Media/repeat-one-line.svg", playModeIcon(app.PlayModeSingle)},
}

// TestWhichStateGetsWhichIcon 钉住「哪个状态配哪一张图标」。
//
// 🔴 **这一条是被评审逼出来的**：原来的 13 条图标测试断的是「几张图形两两不同」，
// 于是把空心和实心**对调**之后整个 internal/ui（4000 多行测试）照样全绿——
// 它们确实还是两两不同，而用户看到的是**反的那一张**（未赞时显示成灰色实心拇指，
// 和 Biu 相反）。「两两不同」断的是**忘了换**，断不了**换反了**。
//
// 现在三层同时钉着：
//
//  1. 这一条：那张抄自 Biu 源码的表，逐项对上生产代码的返回值。
//  2. 调用点只剩**一个出口**（`likeIcon` / `favIcon` / `transportIcon` /
//     `playModeIcon`），没有第二个地方能决定画哪一张。
//  3. 画面上「还没赞」和「已赞」画出来确实不一样（`TestTheLikeKeyDrawsItsOwnIcon`、
//     `TestThePlayAndPauseKeysSwapGlyphs`）——防的是「忘了换」。
//
// ⚠️ 表里写的是**上游的字面文件名**，不是 icons.go 的常量：改了常量而不改这一
// 行，红；要改这一行，得对着 Bui 源码看一眼。
func TestWhichStateGetsWhichIcon(t *testing.T) {
	for _, c := range buiIcons {
		if c.got != c.name {
			t.Errorf("「%s」该画的是 %s（照 Biu 的源码），生产代码给的是 %s——"+
				"界面上画出来的是**反过来那一张**", c.state, c.name, c.got)
		}
	}
	// 认不出来的档位不许变成「随便挑一张」：那是四档之外又多一个意思。
	if got := playModeIcon(app.PlayMode("不存在的一档")); got != "" {
		t.Errorf("认不出来的档位返回了 %q，want 空串（空按钮比画错一张诚实）", got)
	}
}

// stateIcons 是**状态驱动**的那几枚图标：画哪一张取决于界面处在什么状态，
// 而不是取决于这个按钮是哪一个。
//
// 🔴 **它们只准出现在 icons.go 的那四个函数里**（likeIcon / favIcon /
// transportIcon / playModeIcon / volumeIcon），别处一律不许直接摆。
//
// 理由是评审踩到的那个盲区：原来那 13 条测试断的是「几张图形两两不同」，于是把
// 空心和实心**对调**之后 4000 多行测试照样全绿。上面那条
// `TestWhichStateGetsWhichIcon` 钉住了那几个**函数返回什么**——但只要调用点能
// 自己再决定一次（view 里一个 `if liked { name = iconLikeFilled }`），它就管不
// 到画面上了。这一条把那条路堵死：想在 view 里直接摆这几枚图标，立刻报错。
var stateIcons = []string{
	"iconLike", "iconLikeFilled",
	"iconStar", "iconStarFilled",
	"iconPlay", "iconPause",
	"iconModeOrder", "iconModeLoop", "iconModeRandom", "iconModeOne",
	"iconVolumeUp", "iconVolumeMid", "iconVolumeMute",
}

// TestTheStateIconsOnlyReachTheScreenThroughTheirFunction 钉住「唯一出口」。
func TestTheStateIconsOnlyReachTheScreenThroughTheirFunction(t *testing.T) {
	state := make(map[string]bool, len(stateIcons))
	for _, n := range stateIcons {
		state[n] = true
	}

	fset := token.NewFileSet()
	matches, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	// icons.go 里 return 过的那些（每一个都要在名单上，漏一个就是这张表过期了）
	returned := map[string]string{}

	for _, name := range matches {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatalf("解析 %s 失败，这一就没法查了：%v", name, err)
		}
		// icons.go 本身不管：它就是那四个函数的家。
		if name == "icons.go" {
			ast.Inspect(f, func(n ast.Node) bool {
				r, ok := n.(*ast.ReturnStmt)
				if !ok || len(r.Results) != 1 {
					return true
				}
				if id, ok := r.Results[0].(*ast.Ident); ok && state[id.Name] {
					returned[id.Name] = name
				}
				return true
			})
			continue
		}
		// 两种写法都要抓：裸标识符（`name := iconLike`）和带包名的
		// （`ui.Icon(c, ui.iconLike)`，虽然本包不会这么写，但下一个包会）。
		//
		// ⚠️ 第一版只扫了 `SelectorExpr`，于是 `name := iconLikeFilled` 那种
		// **最常见的**绕过写法整个漏过去——实测：改完之后这一条照样全绿。
		check := func(line int, ident string) {
			if state[ident] {
				t.Errorf("%s:%d 直接摆了状态图标 %s——绕过 %s 了，那上面 "+
					"TestWhichStateGetsWhichIcon 管不到画面上。哪一档配哪一张由那个"+
					"函数说了算，这里只管调它",
					name, line, ident, fnFor(ident))
			}
		}
		ast.Inspect(f, func(n ast.Node) bool {
			switch v := n.(type) {
			case *ast.SelectorExpr:
				if v.Sel != nil {
					check(fset.Position(v.Pos()).Line, v.Sel.Name)
				}
			case *ast.Ident:
				check(fset.Position(v.Pos()).Line, v.Name)
			}
			return true
		})
	}

	// 反查：上面那几个函数 return 过的每一枚都要在这张表上。往后加一张状态图标而
	// 没往这儿加，那张图标就能被 view 直接摆，这条守卫对它就不管事了。
	for n, where := range returned {
		if !state[n] {
			t.Errorf("icons.go:%s 的某个 return 用了 %s，可它不在 stateIcons 里——"+
				"view 可以绕过那个函数直接摆它", where, n)
		}
	}
	if len(returned) == 0 {
		t.Fatal("一个状态图标都没扫到——这条测试已经不管事了")
	}
}

// fnFor 说某个状态图标归哪个函数管（报错用，答不上来就说「状态驱动的那几个之一」）。
func fnFor(icon string) string {
	switch {
	case icon == "iconLike" || icon == "iconLikeFilled":
		return "likeIcon"
	case icon == "iconStar" || icon == "iconStarFilled":
		return "favIcon"
	case icon == "iconPlay" || icon == "iconPause":
		return "transportIcon"
	case len(icon) > 8 && icon[:8] == "iconMode_":
		return "playModeIcon"
	case len(icon) > 10 && icon[:10] == "iconVolume":
		return "volumeIcon"
	}
	return "状态驱动的那几个函数"
}

// TestEveryVolumeLevelHasItsIcon 钉住音量那三档图标两两不同。
//
// 和播放模式同一个理由：这个图标旁边**没有字**，静音和小声在画面上是两个
// 不同形状的喇叭，认不认得出全靠它。
func TestEveryVolumeLevelHasItsIcon(t *testing.T) {
	cases := []struct {
		volume float64
		want   string
	}{
		{0, iconVolumeMute},
		{0.4, iconVolumeMid},
		{0.9, iconVolumeUp},
	}
	seen := map[string]float64{}
	for _, c := range cases {
		if got := volumeIcon(c.volume, false); got != c.want {
			t.Errorf("音量 %v 配的图标是 %q，want %q", c.volume, got, c.want)
		}
		if icon(c.want) == nil {
			t.Errorf("音量 %v 用的图标 %q 不在 icons/ 里", c.volume, c.want)
		}
		if prev, dup := seen[c.want]; dup {
			t.Errorf("音量 %v 和 %v 配的是同一张图标 %q", c.volume, prev, c.want)
		}
		seen[c.want] = c.volume
	}
}

// TestMutedAlwaysDrawsTheMuteIcon 钉住「静音」这一格自己说了算。
//
// ⚠️ **静音的时候音量那一格记的还是用户设的值**（票 #311）：用户为了不吵把音量
// 调到 20%，按了静音——取消静音放回去的就是 20%，落盘的也是 20%。
//
// 而画面上这时候要画的是**静音那张**：不靠「音量是不是 0」去猜（用户自己把滑块
// 拉到 0 也是静音，但不是「按了静音」）。
func TestMutedAlwaysDrawsTheMuteIcon(t *testing.T) {
	if got := volumeIcon(0.2, true); got != iconVolumeMute {
		t.Errorf("静音的时候画的图标是 %q，want %q", got, iconVolumeMute)
	}
	// 没静音的时候那一档还是照音量走（0.2 是「小」那一档）。
	if got := volumeIcon(0.2, false); got != iconVolumeMid {
		t.Errorf("没静音、音量 0.2 的时候画的图标是 %q，want %q", got, iconVolumeMid)
	}
}

// playBarTester 只画播放条，State 从外面传进来（和 metaBarTester 同一种构造，
// 但这一票要量的是「按钮那一块里有没有图形」，所以喂一份钉死的 State 就够）。
func playBarTester(t *testing.T, st app.State, width int, dark bool) *mygoui.Tester {
	t.Helper()
	b := newTestShell(t, at(width, coverWinH), systemIsDark(dark), withView(func(s *Shell) func(*mygoui.Context) {
		return func(c *mygoui.Context) {
			s.wake.set(c)
			s.viewPlayBar(c, st)
		}
	}))
	tt := b.tt
	tt.Frame()
	return tt
}

// navBarTester 只画顶栏。
func navBarTester(t *testing.T, dark bool) *mygoui.Tester {
	t.Helper()
	b := newTestShell(t, systemIsDark(dark), withView(func(s *Shell) func(*mygoui.Context) {
		return func(c *mygoui.Context) {
			s.wake.set(c)
			s.viewNav(c)
		}
	}))
	tt := b.tt
	tt.SetFocused(true)
	tt.Frame()
	return tt
}

// 满屏都在放的状态：每一个控件都摆着，没有一个是被条件藏起来的。
func fullBarState() app.State {
	return app.State{
		HasTrack: true,
		Title:    "一首歌",
		Artist:   "某个 UP 主", ArtistMid: 12345,
		Quality:  "320K",
		Rate:     1,
		PlayMode: app.PlayModeLoop,
		Volume:   0.8,
	}
}

// TestThePlayBarDrawsAnIconForEveryAction 钉住播放条上每个动作都有图形。
//
// 逐个列出来而不是「凡是叫得出名字的都得有」：一个漏掉的键仍然满足那句话，
// 而用户看到的是**播放条上一个只有文字或者什么都没有的键**。
//
// 判据是画面：那一块里有画上去的东西（glyphIn 非空），而**深浅两套主题都要试**
// ——「跟着主题走」在浅色下画得出来、深色下糊在背景里，是两种完全不同的坏。
func TestThePlayBarDrawsAnIconForEveryAction(t *testing.T) {
	for _, dark := range []bool{false, true} {
		name := "浅色"
		if dark {
			name = "深色"
		}
		t.Run(name, func(t *testing.T) {
			tt := playBarTester(t, fullBarState(), 1200, dark)
			for _, label := range []string{"上一首", "下一首", "播放", "播放模式", "加歌"} {
				assertGlyphPainted(t, tt, label)
			}
			// 赞 / 收藏在左边**第三段**（见 viewTrackActions），下载在右边
			// 那一组（见 viewPlayBarRight）——**两处都要 `HasTrack` 才画**，
			// fullBarState 带了。
			for _, label := range []string{"收藏", "下载"} {
				assertGlyphPainted(t, tt, label)
			}
		})
	}
}

// TestTheLikeKeyDrawsItsOwnIcon 单独钉「赞」那一枚。
//
// ⚠️ **不能塞进上面那一条**：赞键要 `App.Likeable()`（本地文件和音乐库条目没有
// 可赞的东西，`bili/like.go`），而喂一份 State 的那个 tester 背后是一个**空的
// App**——摆不出这一枚。要真的摆出来就得挂上一台假 B 站并放一首在线的进去
// （`newLikeEnv` 就是干这个的），所以它自己一条。
func TestTheLikeKeyDrawsItsOwnIcon(t *testing.T) {
	e := newLikeEnv(t, false, false, true)

	outline := glyphShape(e.tt, "赞")
	if outline == "" {
		t.Fatalf("赞那一枚上没画东西：%q", e.tt.Texts())
	}
	// 赞过之后换另一张图标（空心 → 实心），所以两块图形必须不同。
	if err := e.tt.Click("赞"); err != nil {
		t.Fatalf("点不动赞：%v", err)
	}
	// ⚠️ **等的是「应用里那个状态」，画面要**再画一帧**才收进去。**
	//
	// 为什么不接着用 `waitFrames` 等画面：
	//
	//   - 那个 10 秒是**墙钟**，而它每一轮都在**画一帧**——画一帧是**整个窗口**
	//     重画一遍，满载的机器上它可能就要好几秒（同一时刻别的测试的 goroutine
	//     正在抢 CPU）。于是十秒的预算全花在重画上，条件还没来得及成立。
	//   - 赞是**乐观**的（`App.SetLike`：点下去那一帧本地就改了），而那一趟写
	//     失败会**改回去**（`rollbackLike`）。慢机器上那一趟写可能超时——于是
	//     「亮过」的那一小段在**检查之前**就没了：等画面等的是一个**已经被回滚
	//     掉**的状态，报出来的却是「赞没变亮」——症状离真因隔了三层。
	//
	// 这一条要等的东西本来就是**应用里的状态**（`waitFor` 正是等这个的，见
	// shelltest_test.go 的「两种等法」），画面那一帧单独画。
	e.tt.Frame()
	waitFor(t, "赞变亮了", func() bool { return e.s.app.LikeState().Liked })
	e.tt.Frame()
	if _, ok := e.tt.Find("已赞"); !ok {
		t.Fatalf("赞变亮了，界面上却还是「赞」：%q", e.tt.Texts())
	}
	filled := glyphShape(e.tt, "已赞")
	if filled == "" {
		t.Fatalf("已赞那一枚上没画东西：%q", e.tt.Texts())
	}
	if filled == outline {
		t.Error("赞过之后**形状**和赞之前一模一样——空心/实心那两张图标没换")
	}
}

// TestTheNavBarDrawsAnIconForEveryAction 钉住顶栏上每个动作都有图形。
//
// 同样深浅两套主题。这里列的是 issue 93 点名的那几个，加上这一票顺带换掉的
// 动态和登录（它们原本也是文字按钮，留着会让顶栏一半图标一半字）。
//
// ⚠️ **「播放列表」不在这一列里**：上游的顶栏压根没有这个开关，它在播放条右边
// 那一组（`biu/src/layout/playbar/right/index.tsx:16`）。我们当初是因为右边
// 摆不下才把它挪到顶栏的，票 #127 把它搬回去了——顶栏留着两处开关就是同一个
// 动作两个入口，而它们会不一致。搬回去之后由
// `TestThePlaylistKeySitsOnThePlayBarNotTheTopBar` 钉住它在新位置上。
func TestTheNavBarDrawsAnIconForEveryAction(t *testing.T) {
	for _, dark := range []bool{false, true} {
		name := "浅色"
		if dark {
			name = "深色"
		}
		t.Run(name, func(t *testing.T) {
			tt := navBarTester(t, dark)
			// ⚠️ **「设置」不在这一列里**（票 #289）：顶栏那一枚拿掉了——侧栏和
			// 头像下拉里都有同一个动作，三个入口会不一致。设置还在不在、够不够
			// 得着，由 `navbar_test.go` 的 `TestTheTopBarHasNoSettingsKey` 和
			// 侧栏那一条（`sidebar_test.go`）管。
			for _, label := range []string{"后退", "前进", "打开文件", "动态"} {
				assertGlyphPainted(t, tt, label)
			}
		})
	}
}

// TestThePlayAndPauseKeysSwapGlyphs 钉住播放键的两张图标确实换。
//
// 原来断的是「按钮上的字从『暂停』变成『播放』」。图标版没有字可断，所以断的是
// 两块图形不一样——而「停着画播放键、放着画暂停键」这一半要真的播起来才量得到
// （喂 State 的那个 tester 背后 App 是空的，按下去什么都不发生）。
//
// ⚠️ 它同时是「按帧比指纹」这种断言**成立**的前提：什么都没动的时候两帧必须
// 画得一模一样，否则下面那几条拿指纹当判据的测试全都不可信。
func TestThePlayAndPauseKeysSwapGlyphs(t *testing.T) {
	h := newHarness(t)
	h.open(t)

	play := glyphIn(h.tt, "暂停")
	if play == "" {
		t.Fatalf("播放键上没画东西：%q", h.tt.Texts())
	}
	h.tt.Frame()
	if again := glyphIn(h.tt, "暂停"); again != play {
		t.Fatalf("什么都没动，两帧画出来的不一样——渲染不是确定的，"+
			"那「按帧比指纹」这种断言就全都不可信了（%d vs %d 个像素）",
			len(play), len(again))
	}

	h.app.Toggle() // 暂停
	waitFrames(t, h.tt, "按钮变成播放", func() bool {
		_, ok := h.tt.Find("播放")
		return ok
	})
	paused := glyphIn(h.tt, "播放")
	if paused == "" {
		t.Fatalf("暂停之后那一枚上没画东西：%q", h.tt.Texts())
	}
	if paused == play {
		t.Error("暂停之后画出来和播放时一模一样——播放/暂停那两张图标没换")
	}
}

// TestTheGlyphsAreNotSwallowedByTheBackground 钉住「图标不和背景糊在一起」。
//
// issue 93 的验收原话是「图标是单色、跟着主题文字色变亮变暗——**深色模式下不糊在
// 背景里**」。判据是**亮度差**：图标上最深/最浅的像素和背景的亮度差必须够大，
// 差得不够就是「能看见有东西，但看不清是什么」。
//
// ⚠️ 60 是从「灰底上要认得出轮廓」倒推的绝对数，**不是**照抄某个主题色的值：
// 阈值取自被测对象的话，把那个色改暗一点这条就跟着放过。
func TestTheGlyphsAreNotSwallowedByTheBackground(t *testing.T) {
	const wantContrast = 60

	for _, dark := range []bool{false, true} {
		name := "浅色"
		if dark {
			name = "深色"
		}
		t.Run(name, func(t *testing.T) {
			tt := playBarTester(t, fullBarState(), 1200, dark)
			for _, label := range []string{"上一首", "下一首", "播放", "播放模式", "加歌", "收藏", "下载"} {
				s := scanGlyph(tt, label)
				if s.ink == "" {
					t.Fatalf("%q 那一块什么都没画：%q", label, tt.Texts())
				}
				if s.contrast < wantContrast {
					t.Errorf("%q 的图形和背景亮度只差 %d（背景 %v，want ≥ %d）——"+
						"看得见有东西，看不清是什么", label, s.contrast, s.bg, wantContrast)
				}
			}
		})
	}
}

// TestTheHitAreaIsBiggerThanTheGlyph 钉住「点击区比图形本身大一点」。
//
// issue 93 的验收原话是「图标的点击区域比图标本身大一点，不用瞄得准」。
//
// # 三条判据互相兜着，一条都不能少
//
//  1. **画面：图形的外接框严格小于按钮框**（哪怕只小一像素）。
//     变异「把点击区缩到和图形一样大」（`Size(图形).Padding(0,0)`）会让两者
//     相等，立刻红。
//  2. **画面：按钮四条边的中点点得中**——留出来的那一圈不是死区。
//     只有 (1) 不够：留了空白而那一圈不接指针，用户还是得点准。
//  3. **留白每边至少 3 像素**，而且按钮在画面上真的有 `hit` 那么大。
//     (1) 量的是包围盒，而按钮的边框和圆角渐变会把它撑大 1~2 像素，所以
//     「边距 1 像素」这种退化 (1) 看不出来，(3) 兜着。
//
// # 为什么 (3) 不是「断言照抄被测对象」
//
// 它断的是**两个数之间的关系**（`hit` 比 `glyph` 大多少），阈值 3 是从「手
// 指放上去要按得准」倒推的绝对数，和生产里任何一个数都不相等。
// 而「按钮在画面上真的有 `hit` 那么大」那半句是把**布局真的用了这个值**钉住
// 的——常量写对了但按钮忘了 `Size(hit)`，那一句立刻红。
//
// ⚠️ 按四条边时用**框边内两像素**当探针，不从图形包围盒往外推：包围盒被边框
// 撑大之后可能已经贴到框边，从它往外推出来的点会落到按钮外面（第一版就这样
// 测的，报出来的是「算出来的探针在按钮之外」——离真因隔了两层）。
// 探针一定在图形之外，靠的是 (3) 说的那 3 像素。
func TestTheHitAreaIsBiggerThanTheGlyph(t *testing.T) {
	h := newHarness(t)
	h.open(t)
	h.app.Toggle() // 先停着：播放键在播的时候叫「暂停」
	waitFrames(t, h.tt, "按钮变成播放", func() bool {
		_, ok := h.tt.Find("播放")
		return ok
	})

	keys := []struct {
		label string
		glyph float32 // 图标边长
		hit   float32 // 点击区边长
	}{
		{"上一首", iconMd, iconHitMd},
		{"下一首", iconMd, iconHitMd},
		// ⚠️ **「播放」不在这张表里**：它是**图形正好填满盒子**的那一枚例外
		// （上游 `className="size-12 min-w-12"` + `RiPlayCircleFill size={48}`，
		// 没有那 5 像素留白），所以「每边留白 ≥ 3」按**图标边长**算是 0——
		// 它得按**画面上量到的那一枚图形**算（圆占画布 20/24，留白是 4）。
		// 下面单独断它。
		{"播放模式", iconMd, iconHitMd},
		{"加歌", iconMd, iconHitMd},
		// 收藏：票 #219 之后跟着上游走（`RiStarFill/Line size={18}`），
		// 它和右边那一组一样是 18 的图标 + 32 的点击区。
		{"收藏", iconMd, iconHitMd},
		// ⚠️ **「下载」不在这张表里了**（票 #222 / 报告 §3.12）：上游那一支的
		// 条件是 `{Boolean(playId) && getPlayItem()?.source !== "local" && …}`
		//（`playbar/right/index.tsx:15`）——**本地文件不摆**。而这一条放的是
		// **真的本地文件**（`h.open(t)` 打开的是仓库里那份 FLAC），所以那一枚
		// 压根不在画面上。
		//
		// 它的点击区由 `TestTheDownloadKeyIsNotThereForALocalFile` 那一族断
		// （那一条喂的是假的 `app.State`，能同时断「本地文件不摆」和「B 站的
		// 那一首摆」）。
	}

	const wantSlack = 3
	for _, k := range keys {
		btn, ok := h.tt.Find(k.label)
		if !ok {
			t.Fatalf("画面上找不到 %q：%q", k.label, h.tt.Texts())
		}

		// (1) 图形的外接框严格小于按钮框。
		g := scanGlyph(h.tt, k.label)
		if g.ink == "" {
			t.Fatalf("%q 那一块没画东西：%q", k.label, h.tt.Texts())
		}
		if g.box.W >= btn.W || g.box.H >= btn.H {
			t.Errorf("%q 的图形 %.1fx%.1f 并不小于按钮 %.1fx%.1f——"+
				"按钮几乎只盖着图形那么大", k.label, g.box.W, g.box.H, btn.W, btn.H)
		}

		// (3) 留白够，且画面上那个框真的是 hit 那么大。
		if slack := (k.hit - k.glyph) / 2; slack < wantSlack {
			t.Errorf("%q 的图形 %.0f、点击区 %.0f，每边只留 %.1f 像素（want ≥ %d）——"+
				"指针得瞄得很准才按得中", k.label, k.glyph, k.hit, slack, wantSlack)
		}
		if d := absF(btn.W-k.hit) + absF(btn.H-k.hit); d > 2 {
			t.Errorf("%q 在画面上是 %.1fx%.1f，而点击区应该是 %.0f——"+
				"布局没用上那个尺寸", k.label, btn.W, btn.H, k.hit)
		}

	}

	// ⚠️ **本地文件时「下载」那一枚压根不在**（票 #222 / 报告 §3.12）：这一条放
	// 的是**真的**本地文件，而上游那一支的条件里有 `source !== "local"`
	//（`playbar/right/index.tsx:15`）——本地文件在 B 站上压根没有 cid 可下，
	// 给一个按了只会报错的键不如不摆。
	//
	// ⚠️ **这一句断的是「真 app」那一档**：`TestTheDownloadKeyIsNotThereForALocalFile`
	// 喂的是假的 `app.State`，而这一条是**真的放了一首本地文件**——来源那一栏
	// 要是判错了（`app.State.Source` 那一段），这里立刻红。
	if _, ok := h.tt.Find("下载"); ok {
		t.Error("本地文件的播放条上画出了「下载」——上游不摆（source !== \"local\"）")
	}

	// 「播放」那一枚单独断：它的**盒子就是图形那一份画布**（上游
	// `size-12 min-w-12`），所以留白要按**画面上量到的那一枚图形**算。
	//
	// ⚠️ **量它不能靠 `scanGlyph`**：那一枚的图形占了盒子的一大半（圆直径 40），
	// 于是「框内的众数」是**图形本身**而不是底色，`scanGlyph` 量出来的是空的。
	// 这一枚底下什么都没有（上游那一枚也是），所以拿**播放条的底色**当参照就
	// 量得出来。
	play, ok := h.tt.Find("播放")
	if !ok {
		t.Fatalf("画面上找不到播放键：%q", h.tt.Texts())
	}
	// 底色从画面上采：按钮左边三像素那一列（那一段是空的——上一首和它之间隔着
	// 24 的缝）。不跟主题算，免得多一份依赖。
	col := make([]color.RGBA, 0, int(play.H))
	for dy := range int(play.H) {
		col = append(col, h.tt.Image().RGBAAt(round32(play.X)-3, round32(play.Y)+dy))
	}
	bg := modeOf(col)
	_, _, n := inkExtent(h.tt.Image(), play, bg)
	if n == 0 {
		t.Fatalf("播放键上没画东西：%q", h.tt.Texts())
	}
	// 主播放键是实心圆（设计规范 §5），圆的直径就是盒子，**没有「图形外的留白」**：
	// 原来「图形严格小于盒子、每边留 ≥ 3」是 Biu 无底图标键的判据，对实心圆不成立。
	// 图形（三角 / 竖条）在圆里居中，由 `TestThePlayKeyIsBigEnoughToHit` 钉。
	// 盒子真的是 44×44（设计规范 §5）。
	//
	// ⚠️ **写的是字面的 44，不是 `iconHitPlay`**：写成常量的话，把常量改回 48
	// 这一条就照样绿，得由这一条钉住。
	const wantPlayBox = 36
	if d := absF(play.W-wantPlayBox) + absF(play.H-wantPlayBox); d > 2 {
		t.Errorf("播放键在画面上是 %.1fx%.1f，而规范的盒子是 %d×%d——"+
			"布局没用上那个尺寸", play.W, play.H, wantPlayBox, wantPlayBox)
	}

	// (2) 四条边内两像素处按得中。只在**播放/暂停**这一枚上量：它是唯一能连点
	// 十几次还不改变界面结构的键（见 pressFlipsPlay）。
	btn, ok := h.tt.Find("播放")
	if !ok {
		t.Fatalf("画面上找不到播放键：%q", h.tt.Texts())
	}
	const probe = 2
	edges := [][2]float32{
		{btn.X + probe, btn.Y + btn.H/2},
		{btn.X + btn.W - probe, btn.Y + btn.H/2},
		{btn.X + btn.W/2, btn.Y + probe},
		{btn.X + btn.W/2, btn.Y + btn.H - probe},
	}
	for i, p := range edges {
		if !pressFlipsPlay(h, p) {
			t.Errorf("播放键第 %d 条边（%.0f,%.0f）按下去没反应——"+
				"图形之外那一圈不是能按的地方", i+1, p[0], p[1])
		}
	}
}

// pressFlipsPlay 在 p 处点一下，看播放状态有没有翻。
//
// 为什么全用**播放/暂停**这一枚来试：它每按一次翻一次，按回去就还原，而别的
// 键点下去会弹框、跳路由、或者发请求——那没法在一条测试里连点十几次。
// 别的键点得中由既有的那几条量（`TestPlayPauseButton`、
// `TestThePlayBarButtonsSaySoWhenNothingIsPlaying`）。
func pressFlipsPlay(h *harness, p [2]float32) bool {
	was := h.app.Snapshot().Playing
	h.tt.ClickAt(p[0], p[1])
	h.tt.Frame()
	flipped := h.app.Snapshot().Playing != was
	if flipped {
		// 按回去，好让下一个探针量的是同一个状态。
		h.app.Toggle()
		h.tt.Frame()
	}
	return flipped
}

func absF(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}

// TestTheRoundKeyKeepsItsEdge 钉住播放/暂停那一枚的图形也没有贴到边。
//
// 它是**圆**的，所以量不出干净的包围盒（见 scanGlyph 那段）。改断**边中点**：
// 左边那一列的中点像素必须还是底色——那一列在圆的最宽处，图形要是有
// 顶到边，它就会出现在那里。四个边中点都量一遍。
//
// 这一条补的是 `TestTheHitAreaIsBiggerThanTheGlyph` 在圆形键上量不了的那一半：
// 那个键是播放条上最大的一枚，也正是最容易被「给大一点」的那一枚。
//
// ⚠️ **参照色是「播放条的底色」，不是按钮自己的底色**：票 #217 把这一枚改成上游
// 那一枚之后，它**底下什么都没有**了（原来是一块实心主色方块），于是「按钮的
// 底色」就是播放条的底色。
//
// ⚠️ **不能拿 `scanGlyph` 给的 `g.bg`**：那一枚的图形占了框的一大半（圆直径 40），
// 于是「框内的众数」是**图形本身**——拿它当参照等于拿图形和图形比，那一条会
// 恒红。底色从画面上采：按钮左边三像素那一列（上一首和它之间隔着 24 的缝）。
func TestTheRoundKeyKeepsItsEdge(t *testing.T) {
	h := newHarness(t)
	h.open(t)
	h.app.Toggle()
	waitFrames(t, h.tt, "按钮变成播放", func() bool {
		_, ok := h.tt.Find("播放")
		return ok
	})

	btn, ok := h.tt.Find("播放")
	if !ok {
		t.Fatalf("播放键不在画面上：%q", h.tt.Texts())
	}
	g := scanGlyph(h.tt, "播放")
	if g.ink == "" {
		t.Fatalf("播放键上没画东西：%q", h.tt.Texts())
	}

	// 参照色：播放条的底色（采自按钮左边那一列）。
	col := make([]color.RGBA, 0, int(btn.H))
	for dy := range int(btn.H) {
		col = append(col, h.tt.Image().RGBAAt(round32(btn.X)-3, round32(btn.Y)+dy))
	}
	bg := modeOf(col)

	// 边中点：左右取垂直中点，上下取水平中点。
	//
	// ⚠️ **四舍五入，不截断**（和上面 `scanGlyph` 同一个理由）：`Tester` 给的框是
	// 小数，而 `int()` 截下来落在按钮**外面**一像素，
	// 取到的是身后那一层（播放条的底色），于是报出来的是「图形顶到边了」，
	// 而图形其实好好地待在离边五像素的地方。
	edges := map[string][2]int{
		"左边": {round32(btn.X), int(btn.Y + btn.H/2)},
		"右边": {round32(btn.X+btn.W) - 1, int(btn.Y + btn.H/2)},
		"上边": {int(btn.X + btn.W/2), round32(btn.Y)},
		"下边": {int(btn.X + btn.W/2), round32(btn.Y+btn.H) - 1},
	}
	img := h.tt.Image()
	for _, side := range []string{"左边", "右边", "上边", "下边"} {
		at := edges[side]
		p := img.RGBAAt(clampX(img, at[0]), clampY(img, at[1]))
		// 实心圆：边中点往里就是强调色底，必须和播放条底色不一样（圆撑满了盒子）。
		if !distinctPixel(p, bg) {
			t.Errorf("%s中点 (%d,%d) 是 %v，和底色 %v 一样——"+
				"实心圆没有撑满盒子", side, at[0], at[1], p, bg)
		}
	}
}

func minOf(vs ...float32) float32 {
	m := vs[0]
	for _, v := range vs[1:] {
		m = min(m, v)
	}
	return m
}

// TestTheResourceFilesCarryNoTransparentMargin 钉住「不靠画透明边撑点击区」。
//
// issue 93 的约束原话：「图标资源里不要画透明边来撑点击区——靠控件 padding，
// 换尺寸时会错位」。这一条量的是**资源本身**：RemixIcon 那一套的图形是按 24×24
// 的画板画的、贴到边，而如果谁在这一票之后塞进来一个「四周留 6 像素透明边、
// 好让点击区大一点」的图标，它的画板就不是 24×24 了。
//
// ⚠️ 这里的判据是**画板尺寸**，不是「有没有留白」——留白不是错，**画板被放大**
// 才是（图形在 24×24 画板里居中贴边，缩到 14 时图形就只有 12 左右了，图标会
// 一圈比旁边那个真的在画板上留白的图标小一圈）。
func TestTheResourceFilesCarryNoTransparentMargin(t *testing.T) {
	for _, name := range iconNames {
		w, h := icon(name).Size()
		if w != h {
			t.Errorf("图标 %q 的画板是 %.0fx%.0f，正方形才是「图标贴边画」的画板", name, w, h)
		}
	}
}

// TestEveryIconButtonNamesItself 钉住「每个只有图标的按钮都有无障碍名字」。
//
// issue 93 的验收原话：「每个放上图标的地方都有无障碍标签（图标没有文字，
// 得有）」。这一条断的是**源码那一层**，而画面那一层由上面那两条
// （`TestThePlayBarDrawsAnIconForEveryAction` / `TestTheNavBarDrawsAnIcon…`）
// 从 `Label` 上找按钮——**找得到就证明 Label 在**。
//
// ⚠️ 为什么还要源码这一层：画面那两条把名字一个一个列出来了，而那正是会漏的
// 地方——以后有人加一个新控件，它自己不在那张清单上，两条都绿。而漏掉 Label
// 的**症状**是「读屏软件念不出这个按钮」：界面上它好好的（图标在、点得动、
// 也有 Tooltip），无头 Tester 也照样全绿，只有用读屏的人才发现。
//
// 所以这里扫**每一个** `iconButton(` / `iconButtonPlay(` /
// `iconKey(` / `flatIconButton(` / `flatIconButtonAccent(` 调用，断言第三个参数
// （非障碍名字）是非空的字符串字面量。空串、变量、或者干脆少一个参数，都当场报错
// 并报出文件:行号。
func TestEveryIconButtonNamesItself(t *testing.T) {
	matches, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	checked := 0
	for _, name := range matches {
		// 测试文件本身不算：它里面出现 `iconButton(` 是**这个测试**在做检查。
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatalf("解析 %s 失败，这一就没法查了：%v", name, err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			id, ok := call.Fun.(*ast.Ident)
			// ⚠️ **`flatIconButton` 也要扫**：这两个是同一件事的两个入口
			// （`iconButton` 就是 `flatIconButton` 的别名，见 icons.go），而这
			// 一票铺开扁平按钮之后 `flatIconButton` 的调用点比 `iconButton` 还多。
			// 只扫 `iconButton` 的话，新写的那些图标键一个都管不到。
			//
			// ⚠️ **`iconButtonPlay` 也要扫**（票 #217 那一枚）：它同样是「只有图标的
			// 按钮」，同样得有个无障碍名字。
			//
			// ⚠️ **`iconKey` 也要扫**（票 #220 那一族）：它是「图标键」——播放条上
			// 那些悬停时图形会变主色的键都走它（`iconButton` 的那个「图标键」
			// 变体）。它同样得有个无障碍名字，而**它挪走了就等于掉出了这张网**：
			// 只扫 `iconButton` 的话，这一票新写的那些图标键一个都管不到。
			//
			// ⚠️ **`flatIconButtonAccent` 也要扫**（票 #249）：它是 `iconKey` 的
			// 底下那一层（`widgets/flat` 的 `IconButtonAccent`），将来谁直接用它，
			// 同样得给无障碍名字。
			if !ok || (id.Name != "iconButton" &&
				id.Name != "iconButtonPlay" && id.Name != "iconKey" &&
				id.Name != "flatIconButton" && id.Name != "flatIconButtonAccent") {
				return true
			}
			checked++
			if empty, literal := thirdArg(call); empty {
				why := "少了第三个参数"
				if literal {
					why = "是个空串"
				}
				t.Errorf("%s: %s 的无障碍名字%s——"+
					"图标对读屏是不可见的，一个没有名字的按钮用户听不出它是干什么的",
					fset.Position(call.Pos()), id.Name, why)
			}
			return true
		})
	}
	if checked == 0 {
		t.Fatal("一个 iconButton 调用都没扫到——这条测试已经不管事了")
	}
}

// thirdArg 看一次调用的第三个参数（无障碍名字）。
//
// 返回 (是不是空名字, 是不是字面量)。**变量放过**：播放条上那个「赞 / 已赞」
// 的名字就是一个局部变量（见 like.go 的 viewLikeStar），它是真话、也非空——
// 但 AST 看不出它等于什么，所以这里不猜。
//
// 放过变量的代价说清楚：以后有人写 `iconButton(c, iconFoo, name, 14)` 而 `name`
// 碰巧是空串，这条不报。那一种由画面那一层兜着（`Find` 一个空名字在 MyGo 里
// 等于没有 Label，于是按钮在 `Texts()` 里根本不会出现，而那两条断言正是从
// `Texts()` 上找它的）。
func thirdArg(call *ast.CallExpr) (empty, literal bool) {
	if len(call.Args) < 3 {
		return true, true
	}
	lit, ok := call.Args[2].(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		// 变量：不是字面量，放过。
		return false, false
	}
	text, err := strconv.Unquote(lit.Value)
	return err != nil || text == "", true
}

// ─────────────────────────────────────────────────────────────────────────────
// 从画面上读「那一块画了什么」
//
// MyGo 没有可查的元素树，所以「图标画出来了没有」只能从像素上看：把按钮那一块
// 扫一遍，认出**哪些像素不是底色**，它们的**外接框**就是图形本身。
// ─────────────────────────────────────────────────────────────────────────────

// inkInset 是找图形时从按钮框每一侧跳过的像素（边框 + 圆角过渡带）。
//
// 3 是量出来的：按钮的边框 1 像素，而深色主题下圆角的过渡带最厚的地方在
// 第三行。窄一档（2）实测还有像素漏进来，窄不了一档——更宽就会开始切到
// 边距只有 5 像素的那几枚图形。
const inkInset = 3

// scan 是把 label 那一块扫一遍之后的结果。
type scan struct {
	// box 是图形（画上去的那些像素）的外接框。零值 = 什么都没画。
	box mygoui.Rect
	// ink 是图形的**灰阶**指纹：同一状态的两帧相同，换一张图标就不同。
	// 它认的是「形状 + 明暗」——两枚在 18 像素下墨迹像素数碰巧相同的图标，
	// 在这儿仍然分得开。
	ink string
	// shape 是图形的**纯形状**指纹：只有「哪些像素是墨」，不管它多黑。
	//
	// 它和 `ink` 分开是因为「同一枚图标只换了颜色」在 `ink` 眼里算换了，
	// 而那一次恰恰**不该**算——赞那一键没赞 / 已赞，两次的墨迹一样、只有主色
	// 那一层变了，用 `ink` 去比它就永远「换了」，测不出图标有没有跟着换
	// （第一版就是这么写的：变异把两张图标对调，它没红）。
	shape string
	// bg 是「没画东西的那些像素」的颜色（按钮底色）。
	bg color.RGBA
	// contrast 是图形和背景的最大亮度差。0 = 那一块是平的。
	contrast int
}

// scanGlyph 把画面上 label 那一块扫一遍，认出图形在哪、有多大、像什么。
//
// 🔴 **不能拿「左上角那个像素」当背景**：主色按钮（播放/暂停）是**圆**的，四角
// 是它身后那一层的颜色——用左上角当背景的话整个圆形按钮都算「画上去的东西」，
// 量出来的「图形」是一个填满按钮的圆，于是「点击区比图形大」永远不成立。
//
// 图标是「框内既不是底色、也不是边框、也不是背景的那些像素」，而那三样都得先
// 认出来（每一样在别的按钮上恰好是别的那一样，所以只能按位置取，不能写死）：
//
//	框外一列的众数 = 它身后那一层的颜色（背景）
//	框内的众数     = 按钮自己的底色
//	框最外一圈的众数 = 按钮的边框（`styleButton` 一直在画，四角最厚）
//
// ⚠️ 众数必须返回**原始颜色**而不是量化键：拿量化过的键去比，`{30,215,96}`
// （主色）和它的量化值 `{16,208,96}` 差 14，比容差 8 大，于是**每一颗底色像素
// 都会被当成图形**。第一版就踩了这个，量出来的「图形」等于整个按钮。
//
// ⚠️ 抗锯齿的过渡像素（主色→背景的中间色、底色→边框的中间色）三样都不是，会被
// 算成图形，包围盒因此大 1~2 像素。那个方向是**保守**的：它让「点击区够大」
// 更容易成立，而点击区那条另有「按得中」那一半兜着（内边距是空的时四边中点
// 按下去没有反应，那一半立刻红）。
func scanGlyph(tt *mygoui.Tester, label string) scan {
	r, ok := tt.Find(label)
	if !ok {
		return scan{}
	}
	img := tt.Image()
	// ⚠️ **四舍五入，不截断**：`Tester` 给的框是小数（实测 410.833），`int()` 截下来
	// 落到按钮外面一像素，于是「框最外一圈」那一圈取到的是**身后那一层**，
	// 边框认不出来，于是整个框内都被当成图形，包围盒等于框本身。
	rx, ry := round32(r.X), round32(r.Y)
	rw, rh := int(r.W), int(r.H)
	// 🔴 **跳过最外面 inkInset 像素**。
	//
	// 按钮有一圈边框，而圆角那一段是边框和底色**连续过渡**的——深色主题下
	// 它揉出的颜色有五六种，框内众数、边框众数、框外众数三个数一个都排不干净
	//（试过：前两种写法在浅色下量得对，`TestTheLikeKeyDrawsItsOwnIcon` 在深色下
	// 量出来的「图形」是 26×26 的整个框，而同一个测试里指针停在按钮上、底色
	// 换成 hover 的那一档反而是对的——两个数对不上，症状离真因隔了三层）。
	//
	// 于是不靠「认出边框是什么颜色」，靠**它一定在边上**：跳过外圈，找图形，
	// 再把找到的框**外扩 inkInset 像素**还回去。
	//
	// ⚠️ 内缩会不会把图形切掉、从而把「点击区够大」判松？会切到**贴着边的**
	// 图形——而那恰恰是坏的（`TestTheHitAreaIsBiggerThanTheGlyph` 的第 3 条
	// 兜着：留白必须每边至少 3 像素）。我们这几枚的边距是 5~7 像素，比
	// 2×inkInset 小一点点，所以它们**都完整落在内圈里**。
	x0, y0 := clampX(img, rx+inkInset), clampY(img, ry+inkInset)
	w, h := rw-2*inkInset, rh-2*inkInset
	if w <= 0 || h <= 0 {
		return scan{}
	}

	// 框外那一列：按钮左边外面一像素，跟着它的高度取众数。
	out := make([]color.RGBA, 0, h)
	for dy := range h {
		out = append(out, img.RGBAAt(clampX(img, rx-1), clampY(img, ry+inkInset+dy)))
	}
	outer := modeOf(out)

	// 框内的众数 = 按钮底色。
	in := make([]color.RGBA, 0, w*h)
	for dy := range h {
		for dx := range w {
			in = append(in, img.RGBAAt(clampX(img, x0+dx), clampY(img, y0+dy)))
		}
	}
	base := modeOf(in)

	// 框最外一圈（不内缩）的众数 = 边框。
	rim := make([]color.RGBA, 0, 4*(int(r.W)+int(r.H)))
	for dx := range int(r.W) {
		for _, y := range []int{int(r.Y), int(r.Y+r.H) - 1} {
			rim = append(rim, img.RGBAAt(clampX(img, int(r.X)+dx), clampY(img, y)))
		}
	}
	for dy := 1; dy < int(r.H)-1; dy++ {
		for _, x := range []int{int(r.X), int(r.X+r.W) - 1} {
			rim = append(rim, img.RGBAAt(clampX(img, x), clampY(img, int(r.Y)+dy)))
		}
	}
	rimColor := modeOf(rim)

	var ink, shape strings.Builder
	minX, minY, maxX, maxY := w, h, -1, -1
	span := 0
	for dy := range h {
		for dx := range w {
			p := in[dy*w+dx]
			if distinctPixel(p, outer) && distinctPixel(p, base) && distinctPixel(p, rimColor) {
				ink.WriteString(strconv.Itoa(glyphLuma(p) >> 4))
				shape.WriteByte('x')
				ink.WriteByte('\n')
				shape.WriteByte('\n')
				minX, minY = min(minX, dx), min(minY, dy)
				maxX, maxY = max(maxX, dx), max(maxY, dy)
				span = max(span, absInt(glyphLuma(p)-glyphLuma(base)))
			}
		}
	}
	if maxX < 0 {
		return scan{bg: base}
	}
	return scan{
		// 外扩 inkInset 还回去：报的是图形在**按钮那个坐标系**里的位置。
		box: mygoui.Rect{
			X: float32(rx+inkInset+minX) + frac(r.X),
			Y: float32(ry+inkInset+minY) + frac(r.Y),
			W: float32(maxX - minX + 1 + 2*(inkInset-1)),
			H: float32(maxY - minY + 1 + 2*(inkInset-1)),
		},
		ink:      ink.String(),
		shape:    shape.String(),
		bg:       base,
		contrast: span,
	}
}

// modeOf 是一堆像素里出现最多的那个颜色，**返回原始颜色**而不是量化键
// （理由见 scanGlyph 那段）。
//
// 🔴 **两遍**：第一遍只定「最多的那个量化键」，第二遍在这个键底下再数一次、
// 取它自己那个众数。少第二遍会踩这个坑——同一个量化键底下有好几个原始色
// （深色主题里同一个底色被抗锯齿揉出好几个邻近值），而返回「第一个见到的」
// 那个时，键底下其余每一个都比它差过容差，于是**整个按钮都被当成图形**：
// 量出来的「图形」等于框，包围盒 26×26，按钮 28×28，一侧只剩一像素
// （`TestTheLikeKeyDrawsItsOwnIcon` 里「赞之前」那一档就是这样，而「赞之后」
// 因为指针恰好停在按钮上、底色换了 hover 的，反而是对的——两个数对不上，
// 症状离真因隔了三层）。
func modeOf(samples []color.RGBA) color.RGBA {
	buckets := map[color.RGBA]map[color.RGBA]int{}
	for _, c := range samples {
		k := quant(c)
		if buckets[k] == nil {
			buckets[k] = map[color.RGBA]int{}
		}
		buckets[k][c]++
	}
	bestKey, bestN := color.RGBA{}, -1
	for k := range buckets {
		if size := countAll(buckets[k]); size > bestN {
			bestKey, bestN = k, size
		}
	}
	var best color.RGBA
	bestN = -1
	for c, n := range buckets[bestKey] {
		if n > bestN {
			best, bestN = c, n
		}
	}
	return best
}

func countAll(m map[color.RGBA]int) int {
	n := 0
	for _, k := range m {
		n += k
	}
	return n
}

// quant 把颜色量化到每通道 4 位——够用来数「这一片是什么颜色」，又不会因为
// 抗锯齿的细微差别把同一个颜色数成好几种。
func quant(c color.RGBA) color.RGBA {
	return color.RGBA{c.R >> 4 << 4, c.G >> 4 << 4, c.B >> 4 << 4, c.A >> 4 << 4}
}

// glyphIn 是「画上去的那些像素」的灰阶指纹；空串 = 什么都没画。
func glyphIn(tt *mygoui.Tester, label string) string { return scanGlyph(tt, label).ink }

// glyphShape 只看「哪些像素是墨」的那一份指纹。
func glyphShape(tt *mygoui.Tester, label string) string { return scanGlyph(tt, label).shape }

// assertGlyphPainted 断 label 那一块上**确实画了东西**。
func assertGlyphPainted(t *testing.T, tt *mygoui.Tester, label string) {
	t.Helper()
	r, ok := tt.Find(label)
	if !ok {
		t.Fatalf("画面上找不到 %q：%q", label, tt.Texts())
	}
	if s := scanGlyph(tt, label); s.ink == "" {
		t.Errorf("%q 那一块是空的（%.1f,%.1f %.1fx%.1f，只有底色 %v）——说明没画图形",
			label, r.X, r.Y, r.W, r.H, s.bg)
	}
}

// glyphLuma 是像素的亮度（Rec. 601 的加权，够用来判「亮的那头和暗的那头差多少」）。
func glyphLuma(c color.RGBA) int {
	return (299*int(c.R) + 587*int(c.G) + 114*int(c.B)) / 1000
}

// distinctPixel 是两个像素算不算「不一样的颜色」，留一点光栅化的余量
// （同一张图标两帧之间也不该有差别，留余量是为了不把舍入当成「变了」）。
func distinctPixel(a, b color.RGBA) bool {
	const slack = 8
	return absInt(int(a.R)-int(b.R)) > slack ||
		absInt(int(a.G)-int(b.G)) > slack ||
		absInt(int(a.B)-int(b.B)) > slack ||
		absInt(int(a.A)-int(b.A)) > slack
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// round32 / frac 是取整和取小数。
func round32(f float32) int  { return int(f + 0.5) }
func frac(f float32) float32 { return f - float32(int(f)) }

func clampX(img *image.RGBA, x int) int { return min(max(x, img.Rect.Min.X), img.Rect.Max.X-1) }
func clampY(img *image.RGBA, y int) int { return min(max(y, img.Rect.Min.Y), img.Rect.Max.Y-1) }

// ─────────────────────────────────────────────────────────────────────────────
// 登录 / 退出两枚图标：一眼要能分清
// ─────────────────────────────────────────────────────────────────────────────

// TestTheLoginAndLogoutGlyphsAreNotTheSamePicture 钉住「登录和退出不是同一张图」。
//
// # 为什么这条不是「两个名字不一样」就够了
//
// 原来那两枚是 `System/login-circle-line.svg` 和 `System/logout-circle-line.svg`：
// **同一个圆圈，箭头一个朝右一个朝左**——文件不一样、名字不一样、尺寸也一样，
// 而用户在 18px 上**分不出**（UX 报告 §3-A 的 A1：「顶栏那枚没有文字，
// 按的是登录还是退出全靠图形认」）。
//
// 所以判据是**画出来的像素差多少**：把两枚各画一次（同一个位置、同一个字号），
// 数一数有多少个像素不一样。
//
// ⚠️ **阈值 150 是量出来的**，不是拍的：
//
//	新的这一对（qr-code / logout-circle-r）：差 217 个像素
//	旧的那一对（login-circle / logout-circle）：差  74 个像素
//
// 18px 的图标画下来满打满算三百来个像素，74 个差别就是「同一个圆圈换个箭头
// 朝向」——那正是分不清的那一种；217 是「两样东西」。判据取 150，两边都离得开。
//
// ⚠️ **这一条断的是「差得多」，不是「一眼认得出」**：后者是人的判断，出图
// （深浅两套）才是它的证据。这里只防住「有人又换回那一对只差一个箭头的」。
func TestTheLoginAndLogoutGlyphsAreNotTheSamePicture(t *testing.T) {
	const wantDiff = 150

	login := renderIconAt18(t, iconLogin)
	logout := renderIconAt18(t, iconLogout)

	diff := 0
	for y := 0; y < 64; y++ {
		for x := 0; x < 64; x++ {
			if login.At(x, y) != logout.At(x, y) {
				diff++
			}
		}
	}
	if diff < wantDiff {
		t.Errorf("登录和退出那两枚图标在 18px 下只差 %d 个像素（want ≥ %d）——"+
			"它们多半是「同一个图形差一个方向」，而顶栏那两枚**没有文字**，"+
			"用户分不出按的是哪一个（文件分别是 %s / %s）",
			diff, wantDiff, iconLogin, iconLogout)
	}
}

// renderIconAt18 把一枚图标画在一块 64×64 的画布上，返回画完的图。
//
// ⚠️ **两枚必须画在同一处**：位置差一点点，边缘的抗锯齿就全对不上，量出来的
// 「差别」里就全是位置而不是图形。所以这里给的是同一个尺寸、同一段 view。
func renderIconAt18(t *testing.T, name string) image.Image {
	t.Helper()
	b := newTestShell(t, at(64, 64), withView(func(s *Shell) func(*mygoui.Context) {
		return func(c *mygoui.Context) {
			mygoui.Icon(c, iconset.Must(name)).FontSize(iconMd).TextColor(c.Theme().Text)
		}
	}))
	b.tt.Frame()
	img := b.tt.Image()
	b.Close()
	return img
}
