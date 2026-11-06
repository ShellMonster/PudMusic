// 播放条上「分集」那一枚键的**身份**。
//
// 🔴 **为什么要有**：「分集」是**逐帧问出来**的（单 P 的稿子不摆，见
// `viewPartButton`）——它一出现，同一段后面几枚的**兄弟序号**就都变了，而框架
// 给元素的默认身份就是「父 ID + 兄弟序号」。于是那一枚新来的键会**继承**上一枚
// 留下的动画通道（画面上是「另一枚键莫名其妙缩着」，按下缩放那档动效存在通道
// 里，见 `widgets/flat/press.go`）。
//
// 所以它用 `Context.Key` 钉住身份。
//
// ⚠️ **喜欢歌单原型之后，原来钉在这一族里的「收藏」「赞」两枚键已经去掉了**
// （`favButtonKey` / `likeButtonKey` 随之删掉）——左列第三段只剩「评论」
// （`commentsButtonKey` 在 `comments_entry.go`），「分集」挪在右列。
package ui

const (
	// partButtonKey 是「分集」那一枚。
	partButtonKey = "playbar:part"
)
