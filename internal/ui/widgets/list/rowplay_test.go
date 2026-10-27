package list

// 「一行的主操作」在**两种行**上的分流（票 #608）。
//
// 这一组是补的：`rowPlay` 把双击 / 回车 / 封面钮从 `Actions.Play`（换队列）改到
// `Actions.PlayNow`（放这一首）时，**搜索页「用户」Tab 那一行不能跟着改**——
// 那一行的 `Play` 是「进他的空间」（`searchresults.go` 的 `openSearchUser`），
// 而它的 `PlayNow` 是一句「UP 主不是歌」的说明。分流写错的话，双击一个用户行
// 会弹出那句话，而用户想进他的空间进不去。
//
// ⚠️ **这一条单独立着是因为它改坏了不会报错**：`actions.go` 里两个回调都接得上，
// 类型一样、签名一样，双击照样「有反应」——只是反应是错的。

import (
	"testing"

	"github.com/ShellMonster/Biu_Go/internal/app"
)

// TestDoubleClickingAUserRowOpensTheSpaceNotTheSpeaker 钉住用户行的分流。
func TestDoubleClickingAUserRowOpensTheSpaceNotTheSpeaker(t *testing.T) {
	rec := &recorded{}
	user := Music{
		ID:    "user:42",
		Title: "某个 UP 主",
		Item: app.Item{
			Type: app.ItemTypeUser, Title: "某个 UP 主", ArtistMid: 42,
		},
	}
	_, tt := newList(t, rec.actions(), []Music{
		user,
		onlineSongs(1)[0],
	})

	r, ok := tt.Find("某个 UP 主")
	if !ok {
		t.Fatalf("画面上找不到那一行，文字有 %q", tt.Texts())
	}
	tt.ClickAt(r.X+r.W/2, r.Y+r.H/2)
	tt.ClickAt(r.X+r.W/2, r.Y+r.H/2)
	frame(tt)

	if len(rec.played) != 1 {
		t.Fatalf("双击用户那一行收到 %d 次「进空间」，want 1 次", len(rec.played))
	}
	if len(rec.nowed) != 0 {
		t.Errorf("双击用户那一行走了「立即播放」——它会弹一句「UP 主不是歌」，"+
			"而用户想进的是他的空间（收到 %d 次）", len(rec.nowed))
	}
}

// TestDoubleClickingASongRowDoesNotOpenAnything 是同一枚硬币的另一面：
// 歌那一行**不许**走「进空间」那条路（`Actions.Play`），否则双击一首歌会把
// 整页当成一段队列换掉——那正是票 #608 要消掉的那个 bug。
func TestDoubleClickingASongRowDoesNotOpenAnything(t *testing.T) {
	rec := &recorded{}
	_, tt := newList(t, rec.actions(), onlineSongs(3))

	r, ok := tt.Find("在线的歌 A")
	if !ok {
		t.Fatalf("画面上找不到那一行，文字有 %q", tt.Texts())
	}
	tt.ClickAt(r.X+r.W/2, r.Y+r.H/2)
	tt.ClickAt(r.X+r.W/2, r.Y+r.H/2)
	frame(tt)

	if len(rec.nowed) != 1 {
		t.Fatalf("双击歌那一行收到 %d 次「立即播放」，want 1 次", len(rec.nowed))
	}
	if len(rec.played) != 0 {
		t.Errorf("双击歌那一行走了「进空间 / 换队列」那条路（收到 %d 次）", len(rec.played))
	}
}
