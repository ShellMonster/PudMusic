package comments

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ShellMonster/PudMusic/internal/service/bili"
)

// TestTheKeyCarriesTheType 钉住「键里带 type」。
//
// 🔴 **只按 oid 认会撞**：avid 是 12345、auid 也是 12345 是两回事，而它们各自
// 有一整个评论区。混在一个文件里的症状是「视频的评论页里摆着音频的评论」——
// 而两个页面上都看不出任何异常。
func TestTheKeyCarriesTheType(t *testing.T) {
	if got, want := Key(1, 12345), "1-12345"; got != want {
		t.Errorf("Key(1, 12345)=%q want %q", got, want)
	}
	if got, want := Key(14, 130191), "14-130191"; got != want {
		t.Errorf("Key(14, 130191)=%q want %q", got, want)
	}
	if Key(1, 12345) == Key(14, 12345) {
		t.Error("视频和音频的同一个 oid 撞到了同一个键")
	}
}

// TestTheTwoObjectsGetTwoFiles 钉住「键带 type ⇒ 两个文件」。
func TestTheTwoObjectsGetTwoFiles(t *testing.T) {
	dir := t.TempDir()
	c := NewCache(dir)
	if err := c.Save(1, 12345, Entry{Comments: []bili.Comment{{Rpid: 1, Message: "视频的"}}}); err != nil {
		t.Fatal(err)
	}
	if err := c.Save(14, 12345, Entry{Comments: []bili.Comment{{Rpid: 2, Message: "音频的"}}}); err != nil {
		t.Fatal(err)
	}
	files, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, f := range files {
		names = append(names, f.Name())
	}
	if len(names) != 2 {
		t.Fatalf("目录里有 %d 个文件，want 2：%v", len(names), names)
	}
	// 读回来也得分清。
	e, ok := c.Load(1, 12345)
	if !ok || len(e.Comments) != 1 || e.Comments[0].Message != "视频的" {
		t.Errorf("读回来的不是视频那一份：%+v（ok=%v）", e, ok)
	}
	e, ok = c.Load(14, 12345)
	if !ok || len(e.Comments) != 1 || e.Comments[0].Message != "音频的" {
		t.Errorf("读回来的不是音频那一份：%+v（ok=%v）", e, ok)
	}
}

// TestAnExpiredEntryIsAsGoodAsMissing 钉住「TTL 一小时」。
//
// ⚠️ **判据是文件的修改时刻**（见 `diskcache` 的文件头），而这一条要把它
// 推到 2 小时前——`Save` 写下去的时刻就是「刚刚」。
func TestAnExpiredEntryIsAsGoodAsMissing(t *testing.T) {
	dir := t.TempDir()
	c := NewCache(dir)
	if err := c.Save(1, 12345, Entry{Comments: []bili.Comment{{Rpid: 1}}}); err != nil {
		t.Fatal(err)
	}
	// 把那份文件的修改时刻推到 2 小时前。
	old := time.Now().Add(-2 * TTL)
	if err := os.Chtimes(c.Path(Key(1, 12345)), old, old); err != nil {
		t.Fatal(err)
	}
	if _, ok := c.Load(1, 12345); ok {
		t.Error("过期的那一份还当得上有——TTL 没生效")
	}
	// 还没过期的那一份读得回来。
	if err := c.Save(1, 999, Entry{Comments: []bili.Comment{{Rpid: 1}}}); err != nil {
		t.Fatal(err)
	}
	if _, ok := c.Load(1, 999); !ok {
		t.Error("刚写的那一份读不回来")
	}
}

// TestTheExpiryRollsWithEveryUse 钉住「过期是滚动的」。
//
// 🔴 **这是这一层存在的全部理由**（2026-10-30 用户提出来的）：
//
//	「过期7天应该会滚动刷新吧，比如第一天用户听的，应该第8天删；
//	  第三天用户又听了，这时候应该是第11天清的」
//
// ⇒ 判据是「**最后一次用到**」，不是「取回来的时刻」。
//
// ⚠️ **用假时钟**：真等一小时没法测。
func TestTheExpiryRollsWithEveryUse(t *testing.T) {
	now := time.Now()
	clock := now
	c := NewCache(t.TempDir()).WithClock(func() time.Time { return clock })
	if err := c.Save(1, 12345, Entry{
		FetchedAt: now.Unix(),
		Comments:  []bili.Comment{{Rpid: 1}},
	}); err != nil {
		t.Fatal(err)
	}
	// 第 30 分钟：用户又看了一遍（离边界还有半小时）。
	clock = now.Add(30 * time.Minute)
	if _, ok := c.Load(1, 12345); !ok {
		t.Fatal("第 30 分钟读不到")
	}
	// 🔴 **第 80 分钟**：按「写下来」算早该删了（离写下来 80 分钟 > 1 小时），
	// 但用户 30 分钟时看过 ⇒ 还该在（离最后一次用到只有 50 分钟）。
	//
	// ⚠️ **这一条断言是「滚动」的全部要害**：判据若是不滚的（按写下来的
	// 时刻算），它在这里会红。
	clock = now.Add(80 * time.Minute)
	if _, ok := c.Load(1, 12345); !ok {
		t.Error("第 30 分钟看过，第 80 分钟就过期了——过期没有跟着使用滚动")
	}
	// 🔴 **第 4 小时**：离最后一次用到（80 分钟那次）已经 3.3 小时 ⇒ 该删了。
	clock = now.Add(4 * time.Hour)
	if _, ok := c.Load(1, 12345); ok {
		t.Error("离最后一次用到满 1 小时了还不过期——用户不看了它就该被清掉")
	}
}

// TestACorruptFileIsAsGoodAsMissing 钉住「读不出来一律当成没有，绝不报错」。
func TestACorruptFileIsAsGoodAsMissing(t *testing.T) {
	dir := t.TempDir()
	c := NewCache(dir)
	path := filepath.Join(dir, Key(1, 12345)+".json")
	if err := os.WriteFile(path, []byte("{不是 JSON"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, ok := c.Load(1, 12345); ok {
		t.Error("坏掉的那一份还当得上有")
	}
}

// TestTheEmptyCacheDoesNotTouchTheDisk 钉住「没挂目录就不落盘」。
func TestTheEmptyCacheDoesNotTouchTheDisk(t *testing.T) {
	c := NewCache("")
	if c.Path(Key(1, 12345)) != "" {
		t.Error("空目录的缓存给出了路径")
	}
	if _, ok := c.Load(1, 12345); ok {
		t.Error("空目录的缓存读出了东西")
	}
	if err := c.Save(1, 12345, Entry{}); err != nil {
		t.Errorf("空目录的缓存写盘报错：%v", err)
	}
}

// TestTheAVIDIndexRemembersTheSwap 钉住「bvid → avid 那一趟换过之后要记住」。
func TestTheAVIDIndexRemembersTheSwap(t *testing.T) {
	dir := t.TempDir()
	c := NewCache(dir)
	if _, ok := c.LoadAVID("BV1"); ok {
		t.Error("没换过就当记得了")
	}
	if err := c.SaveAVID("BV1", 12345); err != nil {
		t.Fatal(err)
	}
	got, ok := c.LoadAVID("BV1")
	if !ok || got != 12345 {
		t.Errorf("LoadAVID=%d ok=%v want 12345", got, ok)
	}
}
