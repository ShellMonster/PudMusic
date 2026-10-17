// 侧栏的「新建」「修改」、提交中不能被误关、以及封面那一串（选文件 → 裁剪 →
// 上传）的测试。
//
// 宿主和那台假 B 站在 foldertest_test.go 里。

package ui

import (
	"bytes"
	"image"
	"image/jpeg"
	"sync"
	"testing"

	mygoui "github.com/egoist/mygo/ui"
)

// ─────────────────────────────────────────────────────────────────────────────
// 侧栏的「新建」「修改」
// ─────────────────────────────────────────────────────────────────────────────

// TestTheSidebarsNewButtonOpensTheEditDialog 侧栏那个 + 能开出「新建收藏夹」。
//
// 这是这一票的第一条验收。
func TestTheSidebarsNewButtonOpensTheEditDialog(t *testing.T) {
	f := newFolderBili(t, nil)
	s, tt := folderShell(t, f)
	seedAndSettle(s, tt, 42)

	if err := tt.Click("新建收藏夹"); err != nil {
		t.Fatalf("侧栏上没有「新建收藏夹」那个按钮：%v（画面上 %q）", err, tt.Texts())
	}
	settleFrame(tt)

	wantText(t, tt, "新建收藏夹")
	wantText(t, tt, "点击上传图片")
	wantText(t, tt, coverHint)
	// ⚠️ 断言的是 `Label`（名称 / 简介）而不是 `Placeholder`
	// （「请输入收藏夹名称」）：Placeholder 不是无障碍标签，Tester 找不到它
	// ——而 Label 才是读屏软件读到的那个，也是用户 Tab 过去时听到的那个。
	wantText(t, tt, "名称")
	wantText(t, tt, "简介")
	wantText(t, tt, "公开")
	wantText(t, tt, "提交")
	wantText(t, tt, "取消")
	wantText(t, tt, "名称为必填项")
	if s.folderEdit == nil || !s.folderEdit.open {
		t.Fatal("弹窗没有开")
	}
	if s.folderEdit.editID != 0 {
		t.Errorf("「新建」的 editID = %d，want 0", s.folderEdit.editID)
	}
	// 「我收藏的」那一组**不该**有那个 +（Biu 的 `titleExtra` 只给 created，
	// `collection/index.tsx:486`）。数 + 而不是数「新建收藏夹」：后者在弹窗
	// 打开之后标题上也是同一句话，会把这条断言数成两次。
	if got := countLabels(tt.Texts(), "+"); got != 1 {
		t.Errorf("那个 + 出现了 %d 次，want 1（只有「我创建的」那组该有）", got)
	}
}

// TestTheSidebarsEditMenuOpensTheSameDialog 「修改」开出的是同一个弹窗，
// 而且**先把它原来的样子拉回来**。
//
// ⚠️ 拉回来这一条不是锦上添花：侧栏那一行只有标题，名称之外的三个字段
// （简介、封面、公开私密）一个都推不出来。照着标题填进去，其余三项会
// **存成空**——用户以为他只是改了名字，结果把简介和封面都删了。
func TestTheSidebarsEditMenuOpensTheSameDialog(t *testing.T) {
	f := newFolderBili(t, nil)
	s, tt := folderShell(t, f)
	seedAndSettle(s, tt, 42)

	if err := tt.RightClick("侧栏收藏夹 42"); err != nil {
		t.Fatalf("%v；画面上 %q", err, tt.Texts())
	}
	if err := tt.ChooseMenuItem("修改"); err != nil {
		t.Fatalf("右键菜单里没有「修改」：%v（菜单是 %v）", err, tt.Menu())
	}
	settleFrame(tt)
	wantText(t, tt, "修改收藏夹")

	// 后台那一趟回来之后表单被填上。
	waitFrameFor(t, tt, "收藏夹信息拉回来", func() bool {
		return s.folderEdit != nil && !s.folderEdit.loading && s.folderEdit.title != ""
	})
	// ⚠️ 读的是状态而不是画面：Tester 能找到输入框（它有 Label），但读不到
	// 输入框里的字。
	if got := s.folderEdit.title; got != "通勤路上" {
		t.Errorf("名称 = %q，want 通勤路上", got)
	}
	if got := s.folderEdit.intro; got != "早上听" {
		t.Errorf("简介 = %q，want 早上听", got)
	}
	if !s.folderEdit.public {
		t.Error("attr 第 0 位是 0（公开），表单里那个开关却关着")
	}
	// 问的是**那一个**收藏夹（42），不是侧栏那一行的序号。
	if got := f.asked(); len(got) != 1 || got[0] != "42" {
		t.Errorf("folder/info 问的是 %v，want [42]", got)
	}
}

// countLabels 数一帧里某个文字出现了几次。
func countLabels(texts []string, want string) int {
	n := 0
	for _, s := range texts {
		if s == want {
			n++
		}
	}
	return n
}

// ─────────────────────────────────────────────────────────────────────────────
// 提交中不能被误关
// ─────────────────────────────────────────────────────────────────────────────

// TestSubmittingCannotBeDismissed 提交中的弹窗点外面不关。
//
// 🔴 这一条是这一票的验收。少了它 symptoms 很具体：用户点「提交」之后手滑点
// 一下弹窗外面，弹窗关了，后台那一趟还在跑并且**成功了**——收藏夹建出来了，
// 而用户不知道它叫什么、也没法接着改。
//
// 对照的那一支（没提交时点外面**要**关）也在这一条里：两个都钉住，才不会
// 有人后来为了修这条把「点外面关」一起关掉。
func TestSubmittingCannotBeDismissed(t *testing.T) {
	// 闸门：写接口那一趟卡在这里，直到测试看完中间态才放它走。
	gate := make(chan struct{})
	release := sync.OnceFunc(func() { close(gate) })
	defer release()
	f := newFolderBili(t, gate)
	s, tt := folderShell(t, f)
	seedAndSettle(s, tt, 42)

	if err := tt.Click("新建收藏夹"); err != nil {
		t.Fatal(err)
	}
	settleFrame(tt)

	// 没提交的时候点外面**要**关。
	tt.ClickAt(8, 8)
	settleFrame(tt)
	if s.folderEdit != nil && s.folderEdit.open {
		t.Fatal("没提交的时候点外面竟然没关掉")
	}

	// 再打开，填名字，提交。
	if err := tt.Click("新建收藏夹"); err != nil {
		t.Fatal(err)
	}
	settleFrame(tt)
	openNewFolder(s, "通勤路上")
	settleFrame(tt)
	if err := tt.Click("提交"); err != nil {
		t.Fatal(err)
	}
	settleFrame(tt)

	// ⚠️ 判据是**按钮转圈那一句**，不是「弹窗还在」：提交那一趟在 Tester 里
	// 只要几毫秒，等到「弹窗关了」才算不出「误关之后有没有立刻又开」。
	if !tt.HasText("提交中…") {
		t.Fatalf("点提交之后按钮没有转圈，文字有 %q", tt.Texts())
	}
	// 点外面、Esc 各来一次。
	tt.ClickAt(8, 8)
	settleFrame(tt)
	tt.Key(0, mygoui.KeyEscape)
	settleFrame(tt)
	if s.folderEdit == nil || !s.folderEdit.open {
		t.Fatal("提交中的弹窗被点外面 / Esc 关掉了")
	}

	// 后台回来之后才关。
	release()
	waitFrameFor(t, tt, "提交结果回来之后弹窗关掉", func() bool {
		return s.folderEdit == nil || !s.folderEdit.open
	})
	writes := f.writes()
	if len(writes) != 1 {
		t.Fatalf("发了 %d 次写请求，want 1", len(writes))
	}
	if got := writes[0].get("title"); got != "通勤路上" {
		t.Errorf("发出去的标题 = %q", got)
	}
	// 🔴 **「新建」不能顺手带上 media_id**：带上之后 B 站当成修改，
	// 用户建出来的东西没有出现在「我创建的」里（它被改到别人那儿去了）。
	if writes[0].get("media_id") != "" {
		t.Errorf("新建时不该带 media_id，发出去的是 %q", writes[0].get("media_id"))
	}
}

// openNewFolder 直接往表单里填一个名字（不走打字）。
//
// ⚠️ Tester 的 `Type` 走的是输入法那一套，而无头环境里它不驱动 TextInput；
// 所以这里直接写状态。**这不算作弊**：这一票要验的是「提交出去的那份对不对」
// 和「提交中不能被误关」，而输入通道本身是框架的事。
func openNewFolder(s *Shell, title string) {
	s.folderEdit.title = title
}

// ─────────────────────────────────────────────────────────────────────────────
// 封面：选文件 → 裁剪 → 上传
// ─────────────────────────────────────────────────────────────────────────────

// openCropDialog 开「新建收藏夹」，然后点封面那一格走完选文件 + 打开裁剪弹窗。
func openCropDialog(t *testing.T, s *Shell, tt *mygoui.Tester) {
	t.Helper()
	seedAndSettle(s, tt, 42)
	if err := tt.Click("新建收藏夹"); err != nil {
		t.Fatal(err)
	}
	settleFrame(tt)
	if err := tt.Click("点击上传图片"); err != nil {
		t.Fatal(err)
	}
	waitFrameFor(t, tt, "裁剪弹窗打开", func() bool {
		return s.folderEdit != nil && s.folderEdit.crop.open
	})
}

// TestPickingACoverOpensTheCropDialog 选一张图会开出裁剪弹窗。
func TestPickingACoverOpensTheCropDialog(t *testing.T) {
	f := newFolderBili(t, nil)
	s, tt := folderShell(t, f)
	s.pickImage = stubPicker(t, pickerPath(t, 800, 600))
	openCropDialog(t, s, tt)

	wantText(t, tt, "裁剪封面")
	wantText(t, tt, "上传")
	wantText(t, tt, "取消")

	// ⚠️ 初始框是 10/10/80/80，而图是 4:3 —— H 会被按 16:9 掰小
	// （见 fitCrop）。这里钉的是「宽还是 80、左上还是 10」。
	b := s.folderEdit.crop.box
	if b.X != 10 || b.W != 80 {
		t.Errorf("初始框 %+v，want X=10 W=80", b)
	}
	// 800×600 的图上，80% 宽 = 640，16:9 的高 = 360，占全图 60%。
	if want := float32(60); absF32(b.H-want) > 0.5 {
		t.Errorf("初始框的高 = %v%%，want %v%%（80 宽的 16:9）", b.H, want)
	}
}

// TestTheUploadedCoverIsSixteenByNine 按 DPR 上传的那张真的是 16:9。
//
// 🔴 这一条钉的是整条链：选文件 → 解码 → 裁 → 编 JPEG → 发给 B 站。
// 断言的是**服务器收到的那张**的尺寸，不是界面上那个框的位置——框对了而
// 发出去的图不对，用户看到的是「封面裁得好好的，上传之后比例变了」。
func TestTheUploadedCoverIsSixteenByNine(t *testing.T) {
	for _, tc := range []struct {
		name         string
		srcW, srcH   int
		scale        float32
		wantW, wantH int
	}{
		{"1 倍屏 16:9 的图", 960, 540, 1, 768, 432},
		{"2 倍屏 16:9 的图", 960, 540, 2, 1536, 864},
		{"2 倍屏 4:3 的图", 800, 600, 2, 1280, 720},
		{"1 倍屏 竖着的图", 600, 1200, 1, 480, 270},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFolderBili(t, nil)
			s, tt := folderShell(t, f)
			tt.SetScale(tc.scale)
			s.pickImage = stubPicker(t, pickerPath(t, tc.srcW, tc.srcH))
			openCropDialog(t, s, tt)

			// ⚠️ **先跑一帧**：DPR 是在绘制回调里记的（见 covermodal.go 的包
			// 注释），所以它是上一帧的值。第一帧记下的默认是 Tester 的 1。
			waitFrameFor(t, tt, "记下这台机器的像素密度", func() bool {
				return s.folderEdit.crop.dpr == tc.scale
			})

			if err := tt.Click("上传"); err != nil {
				t.Fatal(err)
			}
			waitFrameFor(t, tt, "封面传上去", func() bool { return len(f.coversSent()) == 1 })

			w, h := decodeSentCover(t, f.coversSent()[0])
			if w != tc.wantW || h != tc.wantH {
				t.Errorf("传上去的是 %dx%d，want %dx%d", w, h, tc.wantW, tc.wantH)
			}
			if ratio := float32(w) / float32(h); absF32(ratio-cropAspect) > 0.01 {
				t.Errorf("传上去那张的比例是 %v，want %v", ratio, cropAspect)
			}

			// 传完之后裁剪弹窗关掉、地址落到表单上。
			waitFrameFor(t, tt, "封面落到表单上", func() bool {
				return s.folderEdit.crop.uploading == false && s.folderEdit.cover != ""
			})
			if s.folderEdit.cover != "https://i0.hdslb.com/c.jpg" {
				t.Errorf("表单上的封面 = %q", s.folderEdit.cover)
			}
			if s.folderEdit.crop.src != nil {
				t.Error("传完之后那张 256MB 的原图还攥在手上没放掉")
			}
		})
	}
}

// decodeSentCover 读出服务器收到的那张 data 地址里的尺寸。
func decodeSentCover(t *testing.T, dataURL string) (int, int) {
	t.Helper()
	const prefix = "data:image/jpeg;base64,"
	if len(dataURL) < len(prefix) || dataURL[:len(prefix)] != prefix {
		t.Fatalf("发出去的不是 data:image/jpeg 地址：%.40q", dataURL)
	}
	raw := decodeDataURL(t, dataURL)
	cfg, _, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("发出去的那张解不开：%v", err)
	}
	// 顺带钉住「真的是 JPEG」：B 站从前缀认格式，而前缀对着内容是骗不过去的
	// （它看的就是前缀）——所以这里验的是内容本身。
	if _, err := jpeg.Decode(bytes.NewReader(raw)); err != nil {
		t.Errorf("内容不是 JPEG：%v", err)
	}
	return cfg.Width, cfg.Height
}

// TestTheRowKeysFollowTheFolderNotItsPosition 行身份跟着收藏夹，不跟着位置。
//
// 🔴 铁律四，而且这一条有具体的症状：用户勾了几个收藏夹之后回到这一列，列表
// 前面多了一行（他刚在别处收藏了个新的），**后面每一行的勾都串到了别的收藏夹
// 上**。他点确认，B 站那边收到的 `del_media_ids` 里全是他没取消的那些。
//
// 默认的元素身份是「父 ID + 兄弟序号」，而序号在前面插一行之后整体错一位。
// 这里的 `Key` 给的是收藏夹的 id（见 `favSelectRowKey`）。
func TestTheRowKeysFollowTheFolderNotItsPosition(t *testing.T) {
	f := newFolderBili(t, nil)
	f.pages = [][]map[string]any{folderPage(1, 3)}
	s, tt := selectPageShell(t, f)
	openSelectFromMenu(t, tt)
	settleFrame(tt)
	waitFolders(t, tt, s, 3)

	if err := tt.Click("全选"); err != nil {
		t.Fatal(err)
	}
	settleFrame(tt)
	if len(s.folderSelect.picked) != 3 {
		t.Fatalf("全选之后勾了 %d 个，want 3", len(s.folderSelect.picked))
	}

	// 前面多了一个收藏夹（用户刚在别处新建了一个）。
	f.lock()
	f.pages = [][]map[string]any{append(folderPage(100, 1), folderPage(1, 3)...)}
	f.unlock()
	s.app.LoadCreatedFolders(s.folderSelect.rid)

	waitFrameFor(t, tt, "新列表换上去", func() bool {
		return s.folderSelect != nil && len(s.folderSelect.list) == 4 &&
			s.folderSelect.list[0].ID == 100
	})

	// ⚠️ **勾选跟着收藏夹走**：那三个还在，原来那个新的没被勾上。
	want := map[int64]bool{1: true, 2: true, 3: true}
	if len(s.folderSelect.picked) != len(want) {
		t.Fatalf("勾了 %v，want %v", s.folderSelect.picked, want)
	}
	for id := range want {
		if !s.folderSelect.picked[id] {
			t.Errorf("收藏夹 %d 的勾丢了（列表前面插了一行之后）", id)
		}
	}
	if s.folderSelect.picked[100] {
		t.Error("新冒出来的那个收藏夹被勾上了")
	}
	// 而「全选」那一格要跟着新的总数重新算：四个里勾了三个，不再是全选。
	if s.folderSelect.allSelected() {
		t.Error("四个里勾了三个，allSelected 却说是全选")
	}
}
