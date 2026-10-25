// 播放条左边第三段那三枚键（分集 / 收藏 / 赞）的**身份**。
//
// 🔴 **为什么要有**：这一段是三枚并排，而「分集」是**逐帧问出来**的（单 P 的稿子
// 不摆，见 `viewPartButton`）——它一出现，后面几枚的**兄弟序号**就都变了，而框架
// 给元素的默认身份就是「父 ID + 兄弟序号」。于是那一枚新来的键会**继承**上一枚
// 留下的动画通道（画面上是「另一枚键莫名其妙缩着」——按下缩放和赞键那两档动效
// 都存在通道里，见 `widgets/flat/press.go`）。
//
// 所以这三枚都用 `Context.Key` 钉住身份。
//
// ⚠️ **三枚分住在三个文件里**（`partdrawer.go` / `playbar.go` / `like.go`），
// 而身份是**一件事**——所以这三个 key 收在这里，别各写各的。
package ui

const (
	// partButtonKey 是「分集」那一枚。
	partButtonKey = "playbar:part"
	// favButtonKey 是「收藏」那一枚。
	favButtonKey = "playbar:fav"
	// likeButtonKey 是「赞」那一枚。
	likeButtonKey = "playbar:like"
)
