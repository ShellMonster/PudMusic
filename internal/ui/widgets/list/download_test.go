package list

// 「下载音频」那一项的接线（`Actions.Download`）。
//
// 这一组盯的是两件只有接线层知道的事：
//
//  1. 菜单里那一下**做的是下载**（而不是又一句「还没接 B 站接口」）；
//  2. `AppActions` 给的那一份**真的是 App.Download**——于是缺 cid 的条目得到
//     一句说清原因的话，而不是一个假装成功的提示。

import (
	"strings"
	"testing"

	"github.com/ShellMonster/Biu_Go/internal/app"
)

// downloadRecorder 是记「下载」这一下的桩。
type downloadRecorder struct {
	downloaded []app.Item
	notices    []string
}

func (r *downloadRecorder) actions() Actions {
	acts := Actions{
		Play:     func([]app.Item) {},
		Add:      func([]app.Item) {},
		Open:     func(app.Item) {},
		Download: func(it app.Item) { r.downloaded = append(r.downloaded, it) },
		Notify:   func(text string) { r.notices = append(r.notices, text) },
	}
	return acts
}

// TestTheDownloadItemReallyDownloads 「下载音频」按下去要真的是下载。
func TestTheDownloadItemReallyDownloads(t *testing.T) {
	rec := &downloadRecorder{}
	_, tt := newList(t, rec.actions(), onlineSongs(3))

	if err := tt.RightClick("在线的歌 A"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("下载音频"); err != nil {
		t.Fatal(err)
	}

	if len(rec.downloaded) != 1 {
		t.Fatalf("按了「下载音频」，收到 %d 次下载，want 1 次", len(rec.downloaded))
	}
	// 收到的必须是**用户按下去那一首**（菜单是在新的一帧里重建的，见 menu.go
	// 的 buildSongMenu 注释）。
	if got := rec.downloaded[0]; got.Bvid != "BV1" {
		t.Errorf("下的是 %q，want BV1（第一首）", got.Bvid)
	}
	if len(rec.notices) != 0 {
		t.Errorf("成功时不该多说话，却说了 %q", rec.notices)
	}
}

// TestDownloadingSomethingWithoutAPartSaysSo AppActions 给的那一份：缺 cid 的
// 条目**如实拒绝**。
//
// ⚠️ **不许偷偷去问一次分 P**：搜索结果里的条目只有 bvid，而问回来的会是那支
// 稿件的**第一集**——多 P 的第三集被下成第一集还不报错。见 `AppActions` 的
// 注释。
func TestDownloadingSomethingWithoutAPartSaysSo(t *testing.T) {
	a := app.New()
	t.Cleanup(a.Close)
	// 下载那一摊**故意不挂**：这一条要验的是缺 cid 时说不说得出原因，而
	// `App.download` 是先判 cid 再判挂没挂的（见 downloads.go）。

	var notices []string
	acts := AppActions(a, func(s string) { notices = append(notices, s) })
	acts.Download(app.Item{Source: "online", Type: "mv", Bvid: "BV1"})

	if len(notices) != 1 {
		t.Fatalf("说了 %d 句话，want 1", len(notices))
	}
	if !strings.Contains(notices[0], "分 P") {
		t.Errorf("说的是 %q，没有说清是缺分 P", notices[0])
	}
}

// TestDownloadingALocalFileSaysItIsAlreadyOnTheDisk 本地文件那一项按下去要说
// 对的那一句。
//
// ⚠️ **不是**「这一首还不能放：缺稿件 id」——那是放的说法，会让用户以为这首
// 本地歌坏了。
func TestDownloadingALocalFileSaysItIsAlreadyOnTheDisk(t *testing.T) {
	a := app.New()
	t.Cleanup(a.Close)

	var notices []string
	acts := AppActions(a, func(s string) { notices = append(notices, s) })
	acts.Download(app.LocalItem("/tmp/一首歌.m4a"))

	if len(notices) != 1 {
		t.Fatalf("说了 %d 句话，want 1", len(notices))
	}
	if !strings.Contains(notices[0], "磁盘") {
		t.Errorf("说的是 %q，没有说清它已经在磁盘上了", notices[0])
	}
}
