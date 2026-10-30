package list

import (
	"path/filepath"
	"testing"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/notice"
)

// 这一组验的是**接线**：`AppActions` 接在真的 App 上之后，每一项做什么。
// 菜单那一侧（右键开菜单、点哪一项）见 menu_test.go；队列本身的行为
// （换队列、从第一首开始播、打不开就换回去）在 internal/app/queue_test.go 里，
// 不在这里重复一遍。

// TestBiliLink 钉住 B 站地址的拼法，照 Biu 的
// `src/utils/link.ts` 的 getBiliVideoLink：mv 走 video/{bvid}，音频走
// audio/au{sid}，都没有就空串（空串不该去开浏览器）。
func TestBiliLink(t *testing.T) {
	cases := []struct {
		name string
		item app.Item
		want string
	}{
		{"mv", app.Item{Type: "mv", Bvid: "BV1xx411c7mD"}, "https://www.bilibili.com/video/BV1xx411c7mD"},
		{"音频", app.Item{Type: "audio", Sid: "1293494"}, "https://www.bilibili.com/audio/au1293494"},
		{"本地文件", app.Item{Source: "local", Path: "/music/a.mp3"}, ""},
		{"什么都没有", app.Item{}, ""},
	}
	for _, c := range cases {
		if got := BiliLink(c.item); got != c.want {
			t.Errorf("%s：BiliLink() = %q，want %q", c.name, got, c.want)
		}
	}
}

// TestAppActionsSaysWhatWentWrong 验操作失败时有一句话说。
//
// 播放条上确实也会显示错误，但那是**在别的路由上**才看得见；用户正在这个列表
// 页，说一句他才知道刚才那一下为什么没反应。
func TestAppActionsSaysWhatWentWrong(t *testing.T) {
	a := app.New()
	t.Cleanup(a.Close)

	var notices []string
	acts := AppActions(a, func(s string, _ notice.Tone) { notices = append(notices, s) })

	missing := app.LocalItem(filepath.Join(t.TempDir(), "没有这个文件.mp3"))
	acts.Play([]app.Item{missing})

	if len(notices) != 1 {
		t.Fatalf("放不了一首歌，说了 %d 句话，want 1 句", len(notices))
	}
}

// TestAppActionsOnAnUnwiredItemSaysSo 验没接上来源的项在访达里开不了，
// 而且要说出来。
func TestAppActionsOnAnUnwiredItemSaysSo(t *testing.T) {
	a := app.New()
	t.Cleanup(a.Close)

	var notices []string
	acts := AppActions(a, func(s string, _ notice.Tone) { notices = append(notices, s) })
	acts.Open(app.Item{Source: "online", Type: "mv"}) // 没有 bvid

	if len(notices) != 1 {
		t.Fatalf("打开一项没有来处的东西，说了 %d 句话，want 1 句", len(notices))
	}
}

// TestBiliLinkGivesCollectionRowsNoAudioPage 钉住「合集那一行不会被拼成一个不存在的
// au 页」（票 #683 的复审）。
//
// `app.ItemOfMedia` 对**所有**类型都填 `Sid = Media.ID`，而合集那一行的 `ID` 是
// 合集 id（season_id）。`BiliLink` 在没有 bvid 时退回 `audio/au<sid>`——于是合集行
// 的「在 B 站打开」会打开 `https://www.bilibili.com/audio/au1227671`：一个**不存在**
// 的页面，而用户会以为稿子被删了。
//
// ⚠️ 那一行真正的入口是菜单里的「打开这个合集」（走 `Item.CollectionID`）。
func TestBiliLinkGivesCollectionRowsNoAudioPage(t *testing.T) {
	collection := app.Item{Source: "online", Type: "collection", Sid: "1227671", CollectionID: 1227671}
	if got := BiliLink(collection); got != "" {
		t.Errorf("合集行的 BiliLink = %q，want 空串（那一个 sid 是合集 id，拼成 au 页会打开一个不存在的页面）", got)
	}
	// 音乐库那一档照旧。
	if got := BiliLink(app.Item{Source: "online", Type: "audio", Sid: "1293494"}); got != "https://www.bilibili.com/audio/au1293494" {
		t.Errorf("音乐库条目的 BiliLink = %q，want au 页", got)
	}
}
