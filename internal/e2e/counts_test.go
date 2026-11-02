//go:build e2e

package e2e

// 播放条那三枚键（赞 / 收藏 / 评论）的**数字角标**真窗口回归（本票）。
//
// # 为什么这一族要开真窗口
//
// 界面层那条（`internal/ui/playbar_counts_test.go` 的
// `TestTheThreeCountsSitAsBadgesOnTheIconsTopRight`）在无头 Tester 里量的是
// **同一棵树**——但那一层断的是「角标建在了图形的右上角」这个**布局事实**。
// 而这里断的是**无障碍树里看得到的那一份**：
//
//  1. **角标文本在无障碍树里是那一枚键的子节点**（它是 `Attach` 上去的，
//     无障碍树的父子关系跟着元素树走，不跟「它悬在哪儿」走）。
//  2. **角标的 bounds 落在那一枚键的右上象限**（`Attach(AnchorTopRight,
//     AnchorTopLeft)`：角标左缘贴图形右缘、上缘贴图形上沿，数字整个在图形
//     右上外侧）。
//
// 这两层在无头 Tester 里问不到——框架只在**辅助技术问起**时才描述那一帧
// （`mygo/ui/access.go` 的 `accessibilityOn`：真窗口里是系统的读屏软件问起，
// 无头窗口里没人问，那一帧就压根不描述）。所以这一族走 automation 插件的
// `tree` 方法：它读的是**同一份无障碍树**（`mygo/ui/automation.go` 的
// `AutomationTree`）。
//
// # 怎么跑
//
//	BIU_RUN_MYGO_E2E=1 go test -tags e2e ./internal/e2e/ -run TestThePlayBarCountsSitAsBadges
//
// ⚠️ **要一个真窗口**：它开一个窗口并驱动它，所以本机得能开窗口。

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// TestThePlayBarCountsSitAsBadges 钉住**三枚键上的数字是挂在图形右上角的角标**。
//
// 🔴 **三枚都要断**：只断一枚的话，「收藏 / 评论走了角标、赞还停在横排」那一档
// 照样绿——而赞键的图形那一块是绝对定位的两层（`like.go`），和另外两枚不是同
// 一条路径。
//
// ⚠️ **判据是 bounds + 像素**：无障碍树上按钮是**叶子**（`leafRole`——按钮把
// 里面的字收成自己的名字，孩子不单独上树），所以角标文本在树里**没有自己的
// bounds**。这一层于是断两样：
//
//  1. 键的无障碍名字里带着那个数（「赞 9k+」）——读屏用户听得到。
//  2. `capture` 回来的画面上，**键的右上象限有墨迹、而图形正右方那一带没有**
//     ——横排那一版（数字在图形右边那一格、中心 y ≈ 键中心 y）立刻红。
func TestThePlayBarCountsSitAsBadges(t *testing.T) {
	if os.Getenv("BIU_RUN_MYGO_E2E") != "1" {
		t.Skip("设置 BIU_RUN_MYGO_E2E=1 才运行真窗口 E2E")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	socketDir, socket, err := makeAutomationSocket()
	if err != nil {
		t.Fatalf("创建自动化 socket：%v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(socketDir) })

	binary := filepath.Join(socketDir, "counts-fixture")
	var buildOut, buildErr bytes.Buffer
	build := exec.Command("go", "build", "-tags", "e2e", "-o", binary, "./internal/e2e/fixture_counts")
	build.Dir = repoRoot(t)
	build.Stdout = &buildOut
	build.Stderr = &buildErr
	if err := build.Run(); err != nil {
		t.Fatalf("构建角标 fixture：%v\nstdout:\n%s\nstderr:\n%s", err, buildOut.String(), buildErr.String())
	}

	cmd := exec.CommandContext(ctx, binary)
	cmd.Dir = repoRoot(t) // fixture 读 FLAC 素材要用仓库根当 cwd
	cmd.Env = append(os.Environ(),
		"MYGO_AUTOMATION="+socket,
		"BIU_E2E_DATA_DIR="+filepath.Join(socketDir, "data"))
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("启动角标 fixture：%v", err)
	}
	defer func() {
		cancel()
		waitForFixtureExit(t, cmd)
	}()

	conn := waitForAutomationSocket(t, socket, cmd, &stdout, &stderr)
	defer conn.Close()
	client := &automationClient{conn: conn, enc: json.NewEncoder(conn), dec: bufio.NewReader(conn)}

	window := firstContentWindow(t, client)

	// ⚠️ **等数画出来**：那一趟（`/x/web-interface/view`）在后台 goroutine 上
	// 跑——树是每帧重算的，等「三个无障碍名字都在」比按帧数等稳。
	for _, label := range []string{"赞 9k+", "收藏 2k+", "评论 1k+"} {
		if !waitForTree(t, client, window, label, 20*time.Second) {
			t.Fatalf("等不到「%s」那一枚键：\n%s", label, treeDump(t, client, window))
		}
	}

	tree := countsTree(t, client, window)

	// capture 回来一张 PNG：像素那半的判据在它上面量。
	//
	// ⚠️ **PNG 是像素、bounds 是 DIP**：Retina 上一格 DIP 是两个像素——坐标要
	// 乘 `scale`（`windows` 的 `contentBounds` 给 DIP、PNG 给像素，两数相除）。
	img, geom := captureWindow(t, client, window)
	if os.Getenv("BIU_E2E_DUMP") != "" {
		p := filepath.Join(os.TempDir(), "counts-capture.png")
		f, _ := os.Create(p)
		defer f.Close()
		_ = png.Encode(f, img)
		t.Logf("capture 落在 %s（geom=%+v）", p, geom)
		for _, n := range tree {
			if n.Label == "赞 9k+" || n.Label == "收藏 2k+" || n.Label == "评论 1k+" {
				t.Logf("键「%s」 bounds=%+v role=%s", n.Label, n.Bounds, n.Role)
			}
		}
	}

	for _, c := range []struct {
		name  string
		label string // 键的无障碍名字
	}{
		{"赞", "赞 9k+"},
		{"收藏", "收藏 2k+"},
		{"评论", "评论 1k+"},
	} {
		key, ok := findNode(tree, c.label)
		if !ok {
			t.Fatalf("无障碍树里找不到「%s」那一枚键：\n%s", c.label, treeDump(t, client, window))
		}
		// ⚠️ **树的 bounds 只有原点**（每一项的 W/H 都是 0——automation 只把
		// 元素的左上角发出来）。键是 32×32（`iconHitMd`），所以图形盒子的中心
		// 是「原点 + 16」——**不是 `X+W/2`**（那是原点自己）。
		icx, icy := key.Bounds.X+16, key.Bounds.Y+16
		// 底色取样点：键的左上角（键没有底、是播放条那一行的底色）。
		bg := pixelAt(img, geom, key.Bounds.X+2, key.Bounds.Y+2)

		// 🔴 **两条判据把「贴在右上外侧」钉死**（角标的左上角钉在图形盒子
		// 右上角 = icx+9, icy−9，不收不压，字左对齐从图形右缘起排）：
		//
		//	R1 [icx+13, icx+27] × [icy−9, icy+1]——角标的**本体那一带**：实测
		//	   数字笔画落在图形右缘往右 ≈ icx+9 起、贴着图形上沿的横带里。
		//	   横排版（数字中线 ≈ icy、左缘起在图形右沿 +2）的左半截会撞进
		//	   这里的下半截——所以这条还要带上「上沿那一半」：横排版的数字
		//	   上不到 icy−9 那一带。
		//	R2 [icx+9, icx+11.5]——角标的**左端**：`AnchorTopLeft` 把它钉在图形
		//	   右缘（icx+9），左端就落在 icx+9——**一点不往图形上收**。而横排
		//	   那一版的数字从图形右沿 +2 起排（icx+11 才有墨），icx+9 那一带
		//	   是空的；往回收 1.5 那版左端在 icx+7.5，这里反而没墨。这条有墨
		//	   ⇒ 数字的左缘就贴着图形右缘，不多不少。
		if ink := darkerIn(img, geom, icx+13, icy-9, 14, 10, bg); ink == 0 {
			t.Errorf("「%s」键图形右上外侧（%.0f,%.0f 起 14×10 DIP）一点墨迹都没有——"+
				"数字没有收成角标（角标本体在图形右缘往右、贴着上沿那一带）",
				c.name, icx+13, icy-9)
		}
		if ink := darkerIn(img, geom, icx+9, icy-9, 2.5, 10, bg); ink == 0 {
			t.Errorf("「%s」键图形右缘贴边（%.0f,%.0f 起 2.5×10 DIP）一点墨迹都没有——"+
				"数字的左缘不在图形右缘上（横排版左缘在缝后、内收版左缘在图形上，这里都该是空的）",
				c.name, icx+9, icy-9)
		}
	}
}

// countsNode 是无障碍树里的一项（只留这一族用的字段）。
//
// ⚠️ **id / parent 是字符串**（`nodeID` 按十进制数字串发——JavaScript 的 number
// 会把它截掉），不是 JSON number。
type countsNode struct {
	ID     string `json:"id"`
	Parent string `json:"parent"`
	Role   string `json:"role"`
	Label  string `json:"label"`
	Bounds struct {
		X, Y, W, H float64
	} `json:"bounds"`
}

// countsTree 读窗口的无障碍树。
func countsTree(t *testing.T, client *automationClient, window int) []countsNode {
	t.Helper()
	var tree struct {
		Nodes []countsNode `json:"nodes"`
	}
	if err := automationCall(client, 310, "tree", map[string]any{"window": window}, &tree); err != nil {
		t.Fatalf("读元素树：%v", err)
	}
	return tree.Nodes
}

// findNode 在无障碍树里找某个无障碍名字的那一项。
func findNode(tree []countsNode, label string) (countsNode, bool) {
	for _, n := range tree {
		if n.Label == label {
			return n, true
		}
	}
	return countsNode{}, false
}

// captureGeom 是「树的 DIP 坐标 → PNG 像素坐标」的换算：直接乘像素倍率。
type captureGeom struct {
	scale float64 // DIP → 像素
}

// px / py 把一个 DIP 点换算成 PNG 像素坐标。
func (g captureGeom) px(x float64) int { return int(x * g.scale) }
func (g captureGeom) py(y float64) int { return int(y * g.scale) }

// captureWindow 把窗口的内容区拍下来，返回 PNG 解码出来的图和坐标换算。
//
// ⚠️ **坐标是两个口径**：
//
//   - 无障碍树的 `bounds` 是**内容区 DIP**——它的原点就是 PNG 的左上角
//     （`captureContent` 只拍内容面，`windows` 的 `contentBounds.x/y` 是窗口
//     在**屏幕**上的位置，和内容坐标无关——**不许**拿它去减）。
//   - PNG 是**像素**——Retina 上 1 DIP = 2 像素，倍率从 `contentBounds` 的宽
//     （DIP）和 PNG 的宽（像素）算出来。
func captureWindow(t *testing.T, client *automationClient, window int) (*image.RGBA, captureGeom) {
	t.Helper()
	var wins struct {
		Windows []struct {
			ID            int `json:"id"`
			ContentBounds struct {
				X, Y, Width, Height float64
			} `json:"contentBounds"`
		} `json:"windows"`
	}
	if err := automationCall(client, 311, "windows", nil, &wins); err != nil {
		t.Fatalf("问窗口的 contentBounds：%v", err)
	}
	var geom captureGeom
	for _, w := range wins.Windows {
		if w.ID == window {
			// 倍率先记成 DIP 宽——等 PNG 到手再除。
			geom.scale = w.ContentBounds.Width
		}
	}
	if geom.scale <= 0 {
		t.Fatalf("窗口 %d 的 contentBounds 问不到：%+v", window, wins.Windows)
	}

	var shot struct {
		PNG string `json:"png"`
	}
	if err := automationCall(client, 312, "capture", map[string]any{"window": window}, &shot); err != nil {
		t.Fatalf("拍窗口：%v", err)
	}
	raw, err := base64.StdEncoding.DecodeString(shot.PNG)
	if err != nil {
		t.Fatalf("capture 的 PNG 不是 base64：%v", err)
	}
	img, err := png.Decode(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("capture 的 PNG 解不开：%v", err)
	}
	b := img.Bounds()
	rgba, ok := img.(*image.RGBA)
	if !ok {
		rgba = image.NewRGBA(b)
		draw.Draw(rgba, b, img, b.Min, draw.Src)
	}
	geom.scale = float64(b.Dx()) / geom.scale
	return rgba, geom
}

// pixelAt 取 DIP 坐标上那一个像素。
func pixelAt(img *image.RGBA, g captureGeom, x, y float64) color.RGBA {
	px, py := g.px(x), g.py(y)
	if px < 0 || py < 0 || px >= img.Bounds().Dx() || py >= img.Bounds().Dy() {
		return color.RGBA{}
	}
	return img.RGBAAt(px, py)
}

// darkerIn 数 [x, x+w) × [y, y+h)（DIP）那一块里「比底色深」的像素有几个。
//
// ⚠️ **量「深」不量「不一样」**：角标和横排版的数字都是 `TextMuted`（比播放
// 条底色深），而那一带还会混进键的图形——图形是主题色，是**亮**的。数「比底
// 色深」的像素，认得的是那串小字、不是图标笔画；数「和底色不一样」会把邻居
// 键的图标也算进来，横排版和角标版就分不出来了。
func darkerIn(img *image.RGBA, g captureGeom, x, y, w, h float64, bg color.RGBA) int {
	n := 0
	for py := g.py(y); py < g.py(y+h); py++ {
		for px := g.px(x); px < g.px(x+w); px++ {
			if px < 0 || py < 0 || px >= img.Bounds().Dx() || py >= img.Bounds().Dy() {
				continue
			}
			p := img.RGBAAt(px, py)
			if int(bg.R)+int(bg.G)+int(bg.B)-int(p.R)-int(p.G)-int(p.B) > 30 {
				n++
			}
		}
	}
	return n
}
