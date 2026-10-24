// 封面选择器 + 封面裁剪弹窗（Biu 的 `components/image-upload` 和它下面的
// `crop-modal`）。
//
// # 这一层做什么、不做什么
//
// 它做的是**界面**：选文件、画图、画框、拖框、把「上传」按下去。
// 它不做的是裁剪本身——那在 `covercrop.go`，全是能直接调的纯函数。
// 分开是因为裁剪算错了在画面上看不出来（框在那儿、上传也成功、只是传上去的
// 图不对），那种错只能靠单测钉，而单测要能钉就必须是纯函数。
//
// # 🔴 拖动状态用 `Dragged()` 判，不用 `Pressed()`
//
// 三个是三件事（理由照抄 `sidebar.go` 的 sideTrackDrag）：
//
//   - `Dragging()` 报的是 Drag / Draggable 两套机制，普通控件对它永远是 false。
//     用它的症状是「能按住、就是拖不动」。
//   - `Pressed()` 多一条**指针必须在元素框内**的条件。裁剪框常常比裁剪台还大
//     （图宽的时候），指针按住左下角往右下拖，十来像素就出框了——于是框只能
//     挪十几像素就卡住，而拖框是这个弹窗**唯一**的功能。
//   - `Dragged()` 没有框内那一条，它给的是「按着没有」。
//
// ⚠️ 位置仍然用 `PointerPosition` 而不是 `Dragged()` 的位移：一帧里 view 要跑
// 好几遍，每一遍读到的都是同一个位移，累加起来会快好几倍（同一个坑，
// `sidebar.go` 的注释写得更细）。
//
// # DPR 是从绘制回调里拿的
//
// 框架只有 `Painter.Scale()` 能回答「这台机器几个物理像素算一个点」，而它
// 只在绘制阶段拿得到（`ui.Element.DrawOver`）。所以这里在裁剪台的 `DrawOver`
// 里记一次，**下一帧**用它。一帧的滞后在这里没有后果：DPR 只在窗口被拖到
// 另一块不同密度的屏幕时才会变，而那时候用户也不会正好在按「上传」。

package ui

import (
	"image"

	"github.com/ShellMonster/Biu_Go/internal/notice"
	"github.com/ShellMonster/Biu_Go/internal/service/bili"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/imgcache"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/leading"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/scale"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/scrim"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/space"
	"github.com/egoist/mygo/ui"
)

// cropState 是裁剪弹窗此刻的全部状态。
//
// **只由界面线程读写**（铁律一）。它挂在 `folderEdit` 里面而不是挂窗口上：
// 裁剪只能从「新建 / 修改收藏夹」那个弹窗里进来，单独放一个入口的话用户
// 裁完了不知道裁给谁。
type cropState struct {
	open bool
	// box 是裁剪框（百分比，见 covercrop.go）。
	box cropBox
	// drag 是这一次拖拽的起点，见 cropDrag。
	drag cropDrag
	// dpr 是这台机器的像素密度。见包注释那一段。
	dpr float32
	// uploading 是「上传中」。
	uploading bool

	// src 是选中那张图的像素（`decodeCover` 解出来的）。
	src *image.RGBA
	// bitmap 是同一张图当预览用的位图。
	//
	// ⚠️ **只在换图的时候重建**：它是 GPU 纹理，每帧新建一个的话显存会一直涨
	// （理由同 `login.go` 的 qrBitmap）。而它**只能写不能读**，所以所有要碰
	// 像素的事都已经在 `src` 上做完了（见 covercrop.go 的包注释）。
	bitmap *ui.Bitmap
	// srcFor 是 bitmap 对应的是哪个文件——同一个文件不重复解码。
	srcFor string
}

// cropDrag 记着这一次拖拽的起点。
type cropDrag struct {
	// held 是「上一帧按着」。它和 held 一起用来认出「这一帧刚开始拖」。
	held bool
	// corner 是按在哪儿（框里面 / 哪个角）。
	corner cropCorner
	// fromX / fromY 是按下去那一刻指针在裁剪台上的位置。
	fromX, fromY float32
	// start 是按下去那一刻的框。
	start cropBox
}

// size 是这张图多大（像素）。没有图时是 0。
func (c *cropState) size() (int32, int32) {
	if c.src == nil {
		return 0, 0
	}
	return int32(c.src.Bounds().Dx()), int32(c.src.Bounds().Dy())
}

// ── 选文件 ─────────────────────────────────────────────────────────────────

// pickCover 弹文件框选一张封面图。
//
// ⚠️ **文件框在另一个 goroutine 里弹**：MyGo 把请求 post 到主线程再等系统
// 回调，主线程自己进去等就是死等，事件循环再也转不起来（理由同
// `playbar.go` 的 pickAndEnqueue）。
//
// ⚠️ `pick` 是**接缝**：真实现要去系统里弹面板，而那一条在无头 Tester 里会
// panic。测试建完外壳换成「记下路径然后给一张图」的桩，于是「选中的图有没有
// 被解码、有没有被裁成 16:9」能自动验。
//
// 🔴 **这一趟只往 `s.coverPick` 那个带锁的小槽里放，绝不碰 `cropState`**。
// 读盘 + 解一张几千像素的照片是几十到几百毫秒，在界面线程上做就是每点一次
// 冻一下窗口，所以它**必须**在后台；而 `cropState` 是界面状态，**只由界面
// 线程读写**（铁律一）。
//
// 早一版是在这个 goroutine 里直接 `crop.setSource(...)` + `crop.open = true`
// ——那是一处真的数据竞争：`-race` 报「goroutine N 写 crop.open，主线程读
// crop.open」，症状是裁剪弹窗**有时候开不起来**（`-race` 下跑十次红三四次），
// 而单线程跑十次全绿。两边都在同一份内存上，只是时序不同。
func (s *Shell) pickCover() {
	win := s.win
	pick := s.pickImage
	go func() {
		paths, err := pick(win)
		if err != nil || len(paths) == 0 {
			// 用户取消：什么都不用说，也不用叫醒界面。
			return
		}
		img, derr := readAndDecodeCover(paths[0])
		s.coverPick.put(img, paths[0], derr)
		s.wake.invalidate()
	}()
}

// flushCoverPick 取走后台解码好的那张图，**在界面线程上**开裁剪弹窗。
//
// 每帧调一次（由 `viewFolderDialogs`）。**必须**在界面线程上：开弹窗、
// 建 `ui.Bitmap`、改裁剪框，全是界面状态。
//
// 解码失败要说给用户听——「点了没反应」比「选错了格式」难解释得多。
func (s *Shell) flushCoverPick() {
	img, path, errMsg, ok := s.coverPick.take()
	if !ok {
		return
	}
	if errMsg != "" {
		s.addToast(errMsg, notice.Danger)
		return
	}
	e := s.folderEdit
	if e == nil || !e.open {
		// 表单已经关了（比如用户在这几百毫秒里按了 Esc）：那张图没人要了，
		// 直接丢掉——一张 8000×8000 的照片解码完是 256MB。
		return
	}
	// ⚠️ **同一个文件不重来一遍**：换图要重新解码（几十到几百毫秒）和重建
	// GPU 纹理，而用户连点两下「选择照片」选的是同一个文件是常事。
	if e.crop.srcFor == path && e.crop.src != nil {
		e.crop.open = true
		return
	}
	e.crop.setSource(img, path)
	e.crop.open = true
}

// setSource 换掉正在裁的那张图。
func (c *cropState) setSource(img *image.RGBA, path string) {
	c.src, c.srcFor = img, path
	c.bitmap = ui.NewBitmap(img)
	c.box = fitCrop(DefaultCrop, float32(img.Bounds().Dx()), float32(img.Bounds().Dy()))
	c.drag = cropDrag{}
}

// closeCrop 关掉裁剪弹窗，顺手把那张图放掉。
//
// ⚠️ **`src` 和 `bitmap` 一起放掉**：一张 8000×8000 的照片解码完是 256MB，
// 不放的话用户连开十个弹窗就把内存吃光了（而界面上看不出任何异样）。
func (s *Shell) closeCrop(crop *cropState) {
	crop.open = false
	crop.src = nil
	crop.bitmap = nil
	crop.srcFor = ""
	crop.drag = cropDrag{}
	crop.uploading = false
}

// ── 裁剪弹窗 ───────────────────────────────────────────────────────────────

// viewCoverCrop 是「裁剪封面」那个弹窗。
//
// 🔴 **提交中（上传中）不能被误关**：点外面、Esc、点「取消」都不生效，
// 理由见 viewFolderEdit 的注释。
func (s *Shell) viewCoverCrop(c *ui.Context, crop *cropState) {
	if !crop.open || crop.src == nil {
		return
	}
	modal := ui.DialogBase(c, &crop.open, func(backdrop, panel ui.Element) {
		dialogPanel(c, backdrop, panel, "裁剪封面")
		// ⚠️ **字重不写**（400）：弹窗标题是 400（DESIGN.md §二）。
		ui.Text(c, "裁剪封面").FontSize(scale.Font(c, scale.Size15))
		s.viewCropStage(c, crop)
		ui.Row(c).Gap(space.Px8).Justify(ui.End).Children(func() {
			if ui.Button(c, "取消").Disabled(crop.uploading).Clicked() {
				s.closeCrop(crop)
			}
			// ⚠️ **上传中那一档是「转起来 + 灰下去」**（票 #178）：原来只是把字
			// 换成「上传中…」，而 Biu 那个 `<AsyncButton isLoading>` 画的是一枚
			// **转圈**的按钮——「在忙」这件事只靠字说，没有一个能一眼看出来的
			// 样子。共用的那一档见 `loading.go` 的 `busyPrimaryButton`。
			if crop.uploading {
				busyPrimaryButton(c, uploadLabel(true))
			} else if ui.PrimaryButton(c, uploadLabel(false)).Clicked() {
				s.uploadCrop(crop)
			}
		})
	})
	// ⚠️ 只有**没在上传**的时候才让这次关闭算数。上传在后台进行，而用户在
	// 那几百毫秒里点一下「取消」的后果是：那一趟回来之后往一份已经被扔掉的
	// 表单上写封面地址——而那份表单可能已经是另一个收藏夹的了。
	if modal.Dismissed() && crop.uploading {
		crop.open = true
	}
}

// uploadLabel 是上传按钮上的字。
func uploadLabel(busy bool) string {
	if busy {
		return "上传中…"
	}
	return "上传"
}

// viewCropStage 是裁剪台：图 + 框 + 四角把手。
//
// 框和把手是**画上去的**（`DrawOver`）而不是摆上去的：它们要压在图上面，而
// 这个框架没有堆叠布局（`ui.Overlay` 是画一层的全屏浮层，位置不受控）。
// 摆控件的话框只能是一个绝对定位的兄弟元素，而那样它一换图就得重算四个
// 控件的边距——拖框时每帧重算，代码比画出来多一倍还不准。
func (s *Shell) viewCropStage(c *ui.Context, crop *cropState) {
	t := c.Theme()
	iw, ih := crop.size()

	stage := ui.Column(c).Key("crop-stage").Width(cropStageW).Height(cropStageH).
		Radius(t.Radius).Clip().Background(t.Surface)
	stage.DrawOver(func(p *ui.Painter, r ui.Rect) {
		// DPR 只在绘制阶段拿得到（见包注释）。记的是上一帧那一趟，
		// 一帧滞后在这里没有后果。
		if scale := p.Scale(); scale > 0 {
			crop.dpr = scale
		}
		drawCropStage(p, r, crop, iw, ih, t)
	})

	// 拖拽读的是 stage 自己：它的框**不动**（固定 320×180），所以指针的
	// 绝对坐标在拖动的全程都是一个意思。拿框自己的坐标会得到一个常数。
	_, _, held := stage.Dragged()
	x, y, _ := stage.PointerPosition()
	s.trackCropDrag(crop, x, y, held, iw, ih)
}

// drawCropStage 画台子上的东西。图和框是分开画的：框压在图上面。
func drawCropStage(p *ui.Painter, r ui.Rect, crop *cropState, iw, ih int32, t *ui.Theme) {
	if crop.bitmap == nil || iw <= 0 {
		return
	}
	dim := ui.RGBA(0, 0, 0, scrim.Media)

	// 图：按 fitImage 的结果等比缩放居中，和框用的是同一个函数——
	// 两处各算一遍的话，框和图会差半个像素，而那个差值在拖到边上时看得见。
	fx, fy, fw, fh := fitImage(float32(iw), float32(ih))
	p.Image(crop.bitmap, ui.Rect{X: r.X + fx, Y: r.Y + fy, W: fw, H: fh}, ui.FillBox)

	// 框外的部分压暗：四块，围着选框。
	// ⚠️ `stageRect` 给的是台子内的坐标，要加上台子自己的位置才是窗口坐标
	// （绘制回调给的 `r` 是**这个元素在窗口里的框**，不是从 0 开始的）。
	sx, sy, bw, bh := stageRect(crop.box, float32(iw), float32(ih))
	bx, by := r.X+sx, r.Y+sy
	p.Fill(ui.Rect{X: r.X, Y: r.Y, W: r.W, H: by - r.Y}, dim, 0)
	p.Fill(ui.Rect{X: r.X, Y: by + bh, W: r.W, H: r.Y + r.H - by - bh}, dim, 0)
	p.Fill(ui.Rect{X: r.X, Y: by, W: bx - r.X, H: bh}, dim, 0)
	p.Fill(ui.Rect{X: bx + bw, Y: by, W: r.X + r.W - bx - bw, H: bh}, dim, 0)

	p.Stroke(ui.Rect{X: bx, Y: by, W: bw, H: bh}, t.Accent, 2, 2)
	handle := float32(cropHandle)
	for _, hx := range []float32{bx, bx + bw} {
		for _, hy := range []float32{by, by + bh} {
			p.Fill(ui.Rect{X: hx - handle/2, Y: hy - handle/2, W: handle, H: handle}, t.Accent, 2)
		}
	}
}

// trackCropDrag 是拖框。
//
// 按下那一刻记起点，往后每一帧都**从起点重算**（不是「这一帧的框 + 这一帧的
// 位移」）：后者在指针被夹到台子边缘再回来时会突然跳一大截。
func (s *Shell) trackCropDrag(crop *cropState, x, y float32, held bool, iw, ih int32) {
	if !held {
		crop.drag = cropDrag{}
		return
	}
	if !crop.drag.held {
		bx, by, bw, bh := stageRect(crop.box, float32(iw), float32(ih))
		crop.drag = cropDrag{
			held:   true,
			corner: cornerAt(x, y, bx, by, bw, bh),
			fromX:  x, fromY: y,
			start: crop.box,
		}
	}
	switch crop.drag.corner {
	case cornerMove:
		crop.box = moveCrop(crop.drag.start, x-crop.drag.fromX, y-crop.drag.fromY, float32(iw), float32(ih))
	case cornerNone:
		// 按在框外面：这一帧什么都不做，但**记住起点**——用户常常是先点在
		// 框外一点、然后拖进来。起点照记，这一帧的框不动。
	default:
		crop.box = resizeCrop(crop.drag.start, crop.drag.corner, x, float32(iw), float32(ih))
	}
}

// ── 上传 ───────────────────────────────────────────────────────────────────

// uploadCrop 裁完上传。
//
// ⚠️ **裁剪在这一个函数里做，不在后台**：它是一次缩放 + 一次 JPEG 编码，
// 一张 960×600 的图是十几毫秒（DPR=2 的话是几十毫秒）。放后台的话上传按钮
// 转的那一圈就短得看不见，而多一个 goroutine 就多一处要唤醒界面的地方。
func (s *Shell) uploadCrop(crop *cropState) {
	if iw, _ := crop.size(); iw <= 0 {
		return
	}
	url, err := cropToDataURL(crop.src, crop.box, crop.dpr)
	if err != nil {
		s.addToast("裁不出来："+err.Error(), notice.Danger)
		return
	}
	crop.uploading = true
	// 后台那一趟会把地址送到 `app.TakeCoverURL`，把成败送到
	// `app.TakeFolderResult`——**逐窗口通知**靠 `a.wakeUI()`（见 app 层）。
	s.app.UploadCover(url)
}

// readAndDecodeCover 读一个文件并解码。
//
// ⚠️ 先 `Stat` 再读：**光靠 `len(data)` 判不出「太大」**——一张 5MB 的 PNG
// 解开是 400MB（一张纯色的 PNG 压缩率极高），而 `decodeCover` 那个上限挡的是
// 字节数不是解开之后的面积。这里再加一道尺寸上限。
func readAndDecodeCover(path string) (*image.RGBA, error) {
	raw, err := readFileLimited(path, maxCoverBytes+1)
	if err != nil {
		return nil, err
	}
	return decodeCover(raw)
}

// coverHint 是封面那一格下面那句提示。照抄 Biu（`image-upload/index.tsx:159`）。
const coverHint = "建议上传高清封面 ≥960×600，jpeg 或 png 格式，图片 ≤5MB"

// coverPreviewW / coverPreviewH 是编辑弹窗里封面那一格的大小。
//
// ⚠️ **240×150 照抄 Biu**（`favorites-edit-modal/index.tsx:196-197` 的
// `width={240} height={150}`）。它是 16:10 而不是 16:9——Biu 那边就是 16:10
// （画的时候 `object-cover` 把图裁成 16:10 摆着），照抄才对得上。
const (
	coverPreviewW = 240
	coverPreviewH = 150
)

// viewCoverPicker 是编辑弹窗里那一格：现在的封面（或者「点击上传图片」），
// 加上底下的提示。
//
// 点它开文件框。**外面这一格是 Button 而不是 Image**：Image 只是一张图，
// 它上面的点击要自己接，而 Button 免费给到悬停、按下和键盘焦点。
func (s *Shell) viewCoverPicker(c *ui.Context, e *folderEdit) {
	t := c.Theme()

	btn := ui.Button(c, "").Width(coverPreviewW).Height(coverPreviewH).
		Radius(t.Radius).Background(t.SurfaceHover).Border(1, t.Border).
		Disabled(e.submitting || e.crop.uploading)
	btn.Children(func() {
		if e.cover != "" {
			// 已经传上去的封面走 imgcache：它就是一张网络图片，和头图区
			// 那张走的是同一条路（同一个缓存、同一套唤醒）。
			imgcache.View(c, s.images, e.cover, coverPreviewW, coverPreviewH)
			return
		}
		ui.Text(c, "点击上传图片").FontSize(scale.Font(c, scale.Size13)).TextColor(t.TextMuted)
	})
	if btn.Clicked() {
		s.pickCover()
	}

	ui.Text(c, coverHint).FontSize(scale.Font(c, scale.Size11)).LineHeight(leading.Hint).
		TextColor(t.TextMuted).MaxLines(1)
}

// folderEditing 报此刻有没有收藏夹编辑弹窗开着。
func (s *Shell) folderEditing() bool { return s.folderEdit != nil && s.folderEdit.open }

// folderDraftToBili 把表单上的那份翻成服务层要的那一份。
//
// ⚠️ **私密在这里翻一次**（界面说「公开 / 私密」，B 站要 0 / 1）。翻两次的
// 后果是「界面上选了公开、传过去的是 1」，而 B 站那边不报错——用户建出来
// 的是私密收藏夹。
func folderDraftToBili(e *folderEdit) bili.FolderDraft {
	d := bili.FolderDraft{Title: e.title, Intro: e.intro, Cover: e.cover}
	if !e.public {
		d.Privacy = 1
	}
	return d
}
