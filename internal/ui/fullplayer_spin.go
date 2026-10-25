// 满屏播放器那一档：封面转起来。
//
// # 这是 QQ音乐 客户端播放页那个「透明彩胶」的**第一步**
//
// 报告 BMG-254 §3.6（QQ-11）把那一件事拆成了三张票：**先转起来**（这一张）、
// **彩胶造型**（画圆盘 + 同心纹 + 唱针 + 阴影）、**版式档位**（封面和歌词的
// 布局档位）。这一张只做第一件事：**封面在满屏里转**，别的两件各自另开一票。
//
// ## 走的是框架的哪一档：pose（只重绘帧），不是 AnimateTo（整帧重建）
//
// 框架有两档动效成本（`mygo@v0.6.3/ui/pose.go:32` 的注释）：
//
//	AnimateTo   每一帧都要**重建 view + 布局**（整棵满屏播放器）
//	AnimatePose **只重绘帧**：跳过 view 重建与布局
//
// 一个**常驻**的旋转每一帧都要有，于是它必须走 pose 那一档：走 AnimateTo 的话，
// 满屏开着的时候每秒要重建 60 遍整棵满屏播放器（票 #392 治的正是这个毛病）。
//
// ⚠️ **所以这一档不是 `Element.Rotate`**：那一条是**建的时候**写死的变换
// （`mygo@v0.6.3/ui/svg.go:101`），要让角度每帧都变就得每帧重建一次——正是
// 上面那条「每帧重建整棵满屏播放器」。而 `Element.Rotation(...)` **压根不存在**
// （`Rotation` 是包级的 Transform 构造函数，要经 `Element.Transform` 用）。
//
// ## 转的是**图片**，不是外面那个盒子
//
// 外面那层 Box 里除了封面还有长按三连的进度环（`like.go`）：环画在封面**外面**
// 一圈，它是在**画的时候**算的（`likeRingProgress`），跟着封面一起转会让那圈弧
// 乱甩。转的是图片自己（`imgcache.View` 那一格：裁剪和圆角都在它身上，所以
// 整块封面连着裁剪一起转）。
//
// ## 转速是**自定值**
//
// 33⅓ rpm（一圈 1.8 秒）——QQ音乐 的官方图是静态的，**取不到转速**，这是按行业
// 惯例推的（见 `motion.CoverSpinPeriod` 的注释）。

package ui

import (
	"time"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/motion"
	"github.com/egoist/mygo/ui"
)

// fullCoverSpin 是「封面在转」这一轮的状态。
//
// ⚠️ **角度是累加的，不取模**：pose 的那一条通道是**一条曲线**，从当前值走向
// 目标值——所以目标必须**比当前值大**，不然它会倒着转回去。而「比当前值大多少」
// 是固定的一整圈（见 `motion.CoverSpin`），于是这个数就一路加下去。
//
// ⚠️ **精度不会成为问题**：pose 的 Rotate 是个 float32，所以转够多圈之后角度会
// 被量化——但那个量化只是**取整**（误差不超过半个 float32 间距），而且**不累积**：
// 一小时之后（2000 圈）那个间距是 0.086°，在 320 像素那张封面上是**半个像素**。
type fullCoverSpin struct {
	// angle 是那条通道**此刻**的值（度）。
	//
	// ⚠️ **它是 float64**：框架那条通道的值本来就是 float64，而 pose 的 Rotate
	// 是 float32——**收进去的时候才截**（那一处截断只是取整，见上面那段）。
	angle float64
	// target 是上一次给它的目标（通道的**上限**：走到那儿就停在那儿）。
	target float64
	// at 是 angle 是**哪一刻**算出来的。
	at time.Time
	// started 是「这一轮开过没有」（第一帧和后面的帧走的是两支，见下面）。
	started bool
	// id 是这一轮属于**哪一首**（换歌了从头转）。
	id string
}

// fullCoverSpinPose 算这一帧满屏那张封面该摆的 pose。
//
// 第二个返回值是「摆不摆」：false 的时候那一帧封面是**正的**（0 度）。
//
// # 为什么角度要自己记一份
//
// 框架没有「那条通道现在到哪儿了」这个 API，所以这一份得自己记——而且**必须
// 记得和通道分毫不差**：目标永远是「当前角度 + 一整圈」，记错了那一分，转速
// 就跟着错（目标离当前值越远，转得越快）。
//
// 所以下面那份是照着通道的脾气更新的：**匀速走向目标，走到就停**——于是它既
// 不会越过目标（窗口在后台隔了很久也不会「补」那一段），也不会在通道歇着的
// 时候偷偷往前跑。
func (s *Shell) fullCoverSpinPose(c *ui.Context, st app.State, id string) (ui.Pose, ui.Animation, bool) {
	// ⚠️ **「减少动态效果」时不许转**：框架只管它自己算的那一半——ReduceMotion 会
	// 让 pose 的值**直接到终点**（`mygo/ui/pose.go` 的 samplePose），那是「停在
	// 某一个角度」，不是「不转」。所以这一档得自己判（DESIGN.md §五《减少动态
	// 效果》）。
	//
	// 不摆 pose 的后果是那一帧封面回到 **0 度**（正的）——正比歪好。
	if c.Preferences().ReduceMotion {
		return ui.Pose{}, ui.Animation{}, false
	}

	spin := &s.full.spin
	// 换歌了（或者刚打开满屏）：这一轮从头转——新封面出现时是**正的**。
	if spin.id != id {
		*spin = fullCoverSpin{id: id}
	}

	now := c.Now()
	// 第一帧：那条通道还不存在，而框架是**在目标上把它建出来的**
	// （`mygo/ui/pose.go`：「Not posed in the frame before: it shows there at
	// once」）。所以这一帧的目标就是 0：封面出现时是正的，通道也从那儿起步。
	if !spin.started {
		spin.started = true
		spin.at = now
		return ui.Pose{Rotate: 0}, motion.CoverSpin, true
	}

	// 上一帧到这一帧过了多久。通道是**匀速**走向目标、走到就停的，所以它不会
	// 越过那个目标——这一份也得照着来（否则窗口在后台待了半天之后，封面会
	// 「补」那几秒，疯转一通）。
	step := now.Sub(spin.at)
	if step < 0 {
		step = 0
	}
	next := spin.angle + motion.CoverSpinSpeed*step.Seconds()
	if next > spin.target {
		next = spin.target
	}
	spin.angle = next
	spin.at = now

	if !st.Playing {
		// 暂停：**停在它此刻转到的地方**。
		//
		// ⚠️ **不是回到 0 度**：那是一下跳（封面「啪」地转正），而这一格要的是
		// 「停下来」。用 `motion.Snap`（1ns 的线性曲线）：它已经在那儿了，于是
		// 下一帧就到终点，通道歇下来——**不再占帧**（票 #392 治的正是那种
		// 「看不见还在动」）。
		spin.target = spin.angle
		return ui.Pose{Rotate: float32(spin.angle)}, motion.Snap, true
	}

	// 在放：目标定在**一整圈之后**，而曲线也走一整圈——于是它是**匀速**的
	// （200 度/秒 = 33⅓ rpm），而中间那些帧**不用重建界面**（见文件头）。
	spin.target = spin.angle + motion.CoverSpinTurn
	return ui.Pose{Rotate: float32(spin.target)}, motion.CoverSpin, true
}
