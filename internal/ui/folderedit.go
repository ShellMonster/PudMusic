// 收藏夹的「新建 / 修改」弹窗（Biu 的 `components/favorites-edit-modal`）。
//
// # 一个弹窗两种模式
//
// 新建和修改在 Biu 那边是同一个组件、传不传 `mid` 而已（`favorites-edit-modal
// /index.tsx:39`），表单四个字段（封面 / 名称 / 简介 / 公开私密）也完全一样。
// 这里跟着合并，`editID` 为 0 是新建、不为 0 是修改。
//
// # 🔴 提交中不能被误关
//
// Biu 那一支是 `isDismissable={!isSubmitting}`（`favorites-edit-modal/index.tsx:179`）。
// 少了这一条的症状很具体：用户点「提交」之后手滑点一下弹窗外面，弹窗关了，
// 后台那一趟还在跑并且**成功了**——于是收藏夹建出来了，但用户不知道它叫什么、
// 也没法接着改，而下一次建的时候他会换个名字，于是 B 站那边多出一个。
//
// 同一道闸也管 Esc 和「取消」按钮——三者都是「这一趟还在飞」。
//
// # 🔴 表单状态挂在窗口上，不放在局部变量里
//
// 即时模式每一帧都重画一遍，局部变量下一帧就没了（铁律一）。所以这个弹窗的
// 每一个字段都是 `folderEdit` 上的一个字段，改了要留住。

package ui

import (
	"strings"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/ShellMonster/Biu_Go/internal/notice"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/scale"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/space"
	"github.com/egoist/mygo/ui"
)

// folderEdit 是这个弹窗此刻的全部状态。**只由界面线程读写**。
type folderEdit struct {
	// open 是弹窗开没开。
	open bool
	// editID 是要改哪一个收藏夹。**0 是新建**（B 站的 media_id 不会是 0）。
	editID int64

	title  string
	intro  string
	public bool
	// cover 是**已经传上去**的封面地址。空串表示没有封面——那和「有一张封面
	// 但还没上传」不是一回事，后者是 `crop.open`。
	cover string

	// touched 是「用户碰过名称那一格」。
	//
	// ⚠️ **「名称为必填项」只在 touched 之后才画**，照抄 Biu 的
	// `isInvalid={(touchedFields.title || isSubmitted) && !!fieldState.error}`
	// （`favorites-edit-modal/index.tsx:215`）。Biu 的提交按钮在表单不合法时是
	// 禁用的，所以 `isSubmitted` 那一支实际上到不了——真正起作用的是
	// `touchedFields.title`。
	//
	// 为什么非做不可：一个刚打开、用户一个字都还没敲的表单上摆一句红字「名称为
	// 必填项」，读起来是「**已经**报错了」，而他还没做任何事。第一眼看到的是一句
	// 指责而不是一句提示，那和「提交被拒了」在用户心里是一回事。
	touched bool

	// loading 是「正在拉这个收藏夹原来的样子」（只有修改模式会有）。
	loading bool
	// submitting 是「这一趟还在飞」。
	submitting bool

	// crop 是那个裁剪弹窗的状态，见 covermodal.go。
	crop cropState
}

// newFolderEdit 建一份「新建」的状态。
func newFolderEdit() *folderEdit {
	return &folderEdit{public: true}
}

// heading 是弹窗标题。照抄 Biu（`favorites-edit-modal/index.tsx:184`）。
func (e *folderEdit) heading() string {
	if e.editID > 0 {
		return "修改收藏夹"
	}
	return "新建收藏夹"
}

// busy 是「这一刻不能改表单」。
//
// ⚠️ 三个条件缺一不可：在拉原来的信息时改会覆盖掉用户敲的，在上传封面时改会
// 让那份封面加不到这一份表单上，在提交时改等于把上一次提交的内容悄悄改掉。
func (e *folderEdit) busy() bool { return e.loading || e.submitting || e.crop.uploading }

// valid 报表单能不能提交。
//
// ⚠️ **只有名称是必填**（Biu 的 zod schema：`title: z.string().trim().min(1)`
// —— `favorites-edit-modal/index.tsx:30`），简介可空。B 站那边收藏夹名有长度
// 上限而 Biu 没查，所以不在这儿造一个假的限制。
func (e *folderEdit) valid() bool { return strings.TrimSpace(e.title) != "" }

// viewFolderEdit 画「新建 / 修改收藏夹」这个弹窗。
//
// ⚠️ **外面包着 Guard**（铁律四）：view 里 panic 会穿透 Cocoa 事件循环杀掉整个
// 进程，而这里是新加的一整块界面，出事时坏掉的是整个窗口。
func (s *Shell) viewFolderEdit(c *ui.Context) {
	e := s.folderEdit
	if e == nil || !e.open {
		return
	}
	t := c.Theme()
	busy := e.busy()

	modal := ui.Modal(c, &e.open, func() {
		ui.Text(c, e.heading()).FontSize(scale.Font(c, 15)).Bold()
		s.viewCoverPicker(c, e)

		// 名称。⚠️ `Placeholder` 和 `Label` 不同时给：`Label` 是给读屏软件
		// 读的（那里写「名称」），`Placeholder` 是给眼睛看的（那里写
		// 「请输入收藏夹名称」）。两个都写的话界面上会出现两份同样的字。
		name := ui.TextInput(c, &e.title).Placeholder("请输入收藏夹名称").
			Disabled(busy).Width(coverPreviewW + 60)
		name.Label("名称")
		if name.Changed() {
			e.touched = true
		}

		intro := ui.TextArea(c, &e.intro).Placeholder("可选，简单介绍此收藏夹").
			Disabled(busy).Width(coverPreviewW + 60).Height(84)
		intro.Label("简介")

		// 公开 / 私密。Biu 是一个 Switch（`favorites-edit-modal/index.tsx:243`）。
		pub := ui.Toggle(c, &e.public, publicLabel(e.public)).Disabled(busy)
		pub.Label("公开还是私密")

		// ⚠️ **碰过之后才画**（理由见 touched 那个字段）。
		if !e.valid() && !busy && e.touched {
			ui.Text(c, "名称为必填项").FontSize(scale.Font(c, 11)).TextColor(dangerText(t))
		}

		ui.Row(c).Gap(space.Px8).Justify(ui.End).Children(func() {
			if ui.Button(c, "取消").Disabled(e.submitting).Clicked() {
				s.closeFolderEdit()
			}
			// ⚠️ **提交中那一档是「转起来 + 灰下去」**（票 #178）：原来只是把字
			// 换成「提交中…」，而 Biu 那个 `<AsyncButton isLoading>` 画的是一枚
			// **转圈**的按钮——「在忙」这件事只靠字说，没有一个能一眼看出来的
			// 样子。共用的那一档见 `loading.go` 的 `busyPrimaryButton`。
			//
			// ⚠️ **「提交中」和「还在拉封面」是两件事**：后者那个按钮是灰着的，
			// 但它**不是在忙**——它上面不该转圈（转圈会让用户以为还在提交）。
			if e.submitting {
				busyPrimaryButton(c, submitLabel(true))
				return
			}
			sub := ui.PrimaryButton(c, submitLabel(false)).Disabled(busy || !e.valid())
			if sub.Clicked() {
				s.submitFolderEdit(e)
			}
		})
	})

	// 🔴 **提交中这一道闸**：点外面、Esc 都要把 open 放回去。
	//
	// 只有一个 `&&` 的判据——`busy()` 里另外两个条件（loading / 裁剪上传中）
	// 不该挡「关掉弹窗」：那两种情况下关掉是**用户想做的事**（我不想改了），
	// 而提交中关掉是一次误操作，后果是后台那一趟白跑并且建出一个用户不知道
	// 名字的收藏夹。
	if modal.Dismissed() && e.submitting {
		e.open = true
	}
}

// publicLabel 是那个开关上的字。Biu 是一行字跟着值走（`公开` / `私密`）。
func publicLabel(public bool) string {
	if public {
		return "公开"
	}
	return "私密"
}

// submitLabel 是提交按钮上的字。
func submitLabel(busy bool) string {
	if busy {
		return "提交中…"
	}
	return "提交"
}

// folderResultTone 是后台那一条结果的语气：成了是绿的，没成是红的。
//
// ⚠️ **不看那句话的字**：app 层已经说了成没成（`app.FolderResult.OK`），而
// 那句话是「改好了」还是「没登录，改不了」——按字猜的话，改一次文案就错一次
// 颜色，而颜色错了**没有一处会报错**。
func folderResultTone(ok bool) notice.Tone {
	if ok {
		return notice.Success
	}
	return notice.Danger
}

// openFolderEdit 打开弹窗。
//
// id 为 0 是新建，不为 0 是修改（**修改要在后台拉一份原来的样子**：名称、简介、
// 公开私密、封面一个都不能靠侧栏那一行标题推出来——侧栏只有标题）。
func (s *Shell) openFolderEdit(id int64) {
	// ⚠️ **没挂上就别开弹窗**：弹窗开着而提交那一下什么都不会发生（后台那一层
	// 直接就走了），用户看到的是「点了提交，界面一点反应都没有」。
	if !s.app.CollectionsMounted() {
		// ⚠️ **不许说「还没接接口」**（票 #242，UX 报告 §3-F 的 F2）。
		s.addToast("收藏夹现在打不开", notice.Warning)
		return
	}
	e := newFolderEdit()
	e.editID = id
	e.open = true
	if id > 0 {
		e.loading = true
	}
	s.folderEdit = e
	if id > 0 {
		s.app.LoadFolder(id)
	}
}

// closeFolderEdit 关掉弹窗并把那份状态扔掉。
//
// ⚠️ **提交中的不许走到这里**（`viewFolderEdit` 那一道闸）。真走到的话，
// 后台那一趟回来会往一份已经被扔掉的表单上写——而 `s.folderEdit` 那一刻可能
// 已经是另一个收藏夹的了。
func (s *Shell) closeFolderEdit() {
	if s.folderEdit == nil {
		return
	}
	s.closeCrop(&s.folderEdit.crop)
	s.folderEdit = nil
}

// submitFolderEdit 提交表单。
//
// ⚠️ **这里不做网络**：真正那一趟在后台（`app.CreateFolder` / `EditFolder`），
// 而界面上立刻把「提交中」立起来。少了这一步的话，用户在网络慢的时候会连点
// 三次「提交」，于是建出三个同名收藏夹（B 站不拦这个）。
func (s *Shell) submitFolderEdit(e *folderEdit) {
	if !e.valid() || e.submitting {
		return
	}
	e.submitting = true
	if e.editID > 0 {
		s.app.EditFolder(e.editID, folderDraftToBili(e))
		return
	}
	s.app.CreateFolder(folderDraftToBili(e))
}

// flushFolderResults 取走后台那几条结果，分给此刻在等它们的弹窗。
//
// 每一帧开头调一次（铁律三）。三个弹窗共用两个出口：
//
//	TakeFolderDraft   改收藏夹时拉回来的「它现在长什么样」
//	TakeFolderResult  一条「成了没 + 要说的那句话」
//	TakeCoverURL      封面传上去之后回来的地址
//
// ⚠️ **按「谁在等」分派而不是按顺序**：编辑弹窗里点上传封面会开出裁剪弹窗，
// 那时两个都在等，而它们要的东西完全不同（一个是地址、一个是一句话）。
// 谁先来谁先走会让封面的那句话去解除「提交中」，用户看着是「封面还没传完就
// 说改好了」。
func (s *Shell) flushFolderResults() {
	e := s.folderEdit

	if e != nil && e.loading {
		if draft, ok := s.app.TakeFolderDraft(); ok {
			// ⚠️ **对一下是哪一个收藏夹的**：改 A 的那一趟还在路上时用户改去
			// 改 B 了，A 的那份回来会盖在 B 的表单上——而 B 明明还在「正在
			// 拉」。表现是「打开 B，看到的却是 A 的名字」，用户一提交就把 A
			// 的名字写到了 B 上。
			//
			// 不是这一个就**扔掉**（而不是放回去）：它是「取走而不是读」的那一
			// 槽，留着只会被下一次拉取当成自己的。
			if draft.ID == e.editID {
				e.loading = false
				e.applyDraft(draft)
			}
		}
	}

	// 封面地址是**另一个出口**，见 app 层那个函数的注释。
	if url, ok := s.app.TakeCoverURL(); ok {
		if e != nil && e.crop.uploading {
			e.crop.uploading = false
			s.closeCrop(&e.crop)
			e.cover = url
			s.addToast("封面裁好了", notice.Success)
		}
	}

	res, ok := s.app.TakeFolderResult()
	if !ok {
		return
	}
	sel := s.folderSelect
	switch {
	case sel != nil && sel.submitting:
		// ⚠️ **成功才关弹窗，失败留着**：失败的时候勾还在界面上，重试一下就行；
		// 关掉等于让用户从头勾一遍。
		sel.submitting = false
		s.addToast(res.Text, folderResultTone(res.OK))
		if res.OK {
			s.closeFolderSelect()
		}
	case e != nil && e.crop.uploading:
		// ⚠️ **失败也要解除**：只解除成功的话失败之后按钮永远转圈，弹窗再也
		// 关不掉，而用户看到的是一句「上传失败」加一把转不停的按钮。
		e.crop.uploading = false
		s.addToast(res.Text, folderResultTone(res.OK))
	case e != nil && e.submitting:
		// ⚠️ **成功要关掉弹窗、失败要留着**：失败的时候用户填的东西还在界面上，
		// 关掉等于让他从头敲一遍（而失败的原因——比如「没登录」——在弹窗里
		// 根本看不见）。
		e.submitting = false
		s.addToast(res.Text, folderResultTone(res.OK))
		if res.OK {
			s.closeFolderEdit()
		}
	default:
		s.addToast(res.Text, folderResultTone(res.OK))
	}
}

// applyDraft 把拉回来的那一份填进表单。
//
// ⚠️ **公开 / 私密是翻过来的**：B 站的 `attr` 第 0 位是「私密」，而界面上那个
// 开关问的是「是不是公开」。`app` 那层已经翻过一次，这里拿到的就是界面上
// 那个语义（见 `app.FolderDraftView.Public`）。
func (e *folderEdit) applyDraft(d app.FolderDraftView) {
	e.title, e.intro, e.cover, e.public = d.Title, d.Intro, d.Cover, d.Public
}
