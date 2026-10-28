package danmaku

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ShellMonster/PudMusic/internal/service/bili"
)

// TestCacheRoundTrip 是缓存最基本的那个约定：写进去什么，读出来还是什么。
func TestCacheRoundTrip(t *testing.T) {
	c := NewCache(t.TempDir())
	want := Entry{
		Items: []bili.Danmaku{
			{TimeMS: 1000, Mode: 1, FontSize: 25, Color: 16777215, Text: "第一条", ID: "1"},
			{TimeMS: 2000, Mode: 5, FontSize: 18, Color: 15138834, Text: "第二条", ID: "2"},
		},
		FetchedAt: time.Now().Truncate(time.Second),
	}
	if err := c.Save("BV1xx411c7mD", "112233", want); err != nil {
		t.Fatal(err)
	}

	got, ok := c.Load("BV1xx411c7mD", "112233")
	if !ok {
		t.Fatal("刚写进去就读不到")
	}
	if len(got.Items) != 2 {
		t.Fatalf("读回来 %d 条，want 2：%+v", len(got.Items), got.Items)
	}
	if got.Items[0] != want.Items[0] || got.Items[1] != want.Items[1] {
		t.Errorf("读回来是 %+v，want %+v", got.Items, want.Items)
	}
	if !got.FetchedAt.Equal(want.FetchedAt) {
		t.Errorf("取回来的时刻是 %v，want %v", got.FetchedAt, want.FetchedAt)
	}
}

// TestTheFileIsNamedAfterTheBvidAndTheCid 钉住文件名。
//
// ⚠️ **不是哈希**：这一层的键本来就是两个 B 站的编号，而用户要能一眼看出那个
// 文件是哪一集的（想删掉一份缓存的时候得找得到）。
func TestTheFileIsNamedAfterTheBvidAndTheCid(t *testing.T) {
	dir := t.TempDir()
	c := NewCache(dir)

	if err := c.Save("BV1xx411c7mD", "112233", Entry{Items: []bili.Danmaku{{Text: "x"}}}); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(dir, "BV1xx411c7mD-112233.json")
	if got := c.Path("BV1xx411c7mD", "112233"); got != want {
		t.Errorf("路径是 %q，want %q", got, want)
	}
	if _, err := os.Stat(want); err != nil {
		t.Errorf("文件没落在那个名字上：%v", err)
	}
}

// TestPartsDoNotShareAFile 盯「同一个投稿的每一集弹幕都不一样」。
func TestPartsDoNotShareAFile(t *testing.T) {
	dir := t.TempDir()
	c := NewCache(dir)

	if err := c.Save("BV1", "1", Entry{Items: []bili.Danmaku{{Text: "第一集"}}}); err != nil {
		t.Fatal(err)
	}
	if err := c.Save("BV1", "2", Entry{Items: []bili.Danmaku{{Text: "第二集"}}}); err != nil {
		t.Fatal(err)
	}

	if got, _ := c.Load("BV1", "1"); len(got.Items) != 1 || got.Items[0].Text != "第一集" {
		t.Errorf("第一集被第二集盖了：%+v", got.Items)
	}
	if got, _ := c.Load("BV1", "2"); len(got.Items) != 1 || got.Items[0].Text != "第二集" {
		t.Errorf("第二集被第一集盖了：%+v", got.Items)
	}
}

// TestAMissingOrBrokenFileIsJustNoDanmaku：读不出来一律当成「没有」，绝不报错。
func TestAMissingOrBrokenFileIsJustNoDanmaku(t *testing.T) {
	dir := t.TempDir()
	c := NewCache(dir)

	if _, ok := c.Load("没写过", "1"); ok {
		t.Error("没写过的读出了东西")
	}
	if _, ok := c.Load("", ""); ok {
		t.Error("空身份读出了东西")
	}

	// 写坏了：半份 JSON。断电或进程被杀会留下这种文件。
	if err := c.Save("坏的那条", "1", Entry{Items: []bili.Danmaku{{Text: "x"}}}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(c.Path("坏的那条", "1"), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, ok := c.Load("坏的那条", "1"); ok {
		t.Error("半份 JSON 读成了一份弹幕")
	}
}

// TestSaveIsAtomicAndLeavesNoRubbish 盯住「断电之后不能留下半份 JSON」：
// 临时文件走完 rename 之后目录里不能多出任何东西。
func TestSaveIsAtomicAndLeavesNoRubbish(t *testing.T) {
	dir := t.TempDir()
	c := NewCache(dir)

	for i := range 5 {
		if err := c.Save("同一集", "1", Entry{
			Items:     []bili.Danmaku{{Text: "x", TimeMS: int64(i)}},
			FetchedAt: time.Now(),
		}); err != nil {
			t.Fatal(err)
		}
	}

	names, err := filepath.Glob(filepath.Join(dir, "*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 1 {
		var got []string
		for _, n := range names {
			got = append(got, filepath.Base(n))
		}
		t.Fatalf("写了 5 次之后目录里应该有 1 个文件，有 %d 个：%v", len(names), got)
	}
	// 写到最后一次的值：rename 真的换掉了，不是每次都写进同一个临时文件。
	if got, _ := c.Load("同一集", "1"); len(got.Items) != 1 || got.Items[0].TimeMS != 4 {
		t.Errorf("最后一次写的是 %+v，want TimeMS=4", got.Items)
	}
}

// TestACacheWithoutADirJustDoesNothing：没挂盘的时候弹幕照样从 B 站取，只是
// 关掉之后不记得。不许因为写不下去就报错。
func TestACacheWithoutADirJustDoesNothing(t *testing.T) {
	c := NewCache("")

	if err := c.Save("BV1", "1", Entry{Items: []bili.Danmaku{{Text: "x"}}}); err != nil {
		t.Errorf("没配盘写东西不该是错误：%v", err)
	}
	if _, ok := c.Load("BV1", "1"); ok {
		t.Error("没配盘却读出了东西")
	}
	if c.Path("BV1", "1") != "" {
		t.Errorf("没配盘时 Path 应该是空的，出了 %q", c.Path("BV1", "1"))
	}
}

// TestSevenDaysLaterItIsGone 钉住 7 天过期。
//
// 过期了之后 `Load` 给的是「没有」——于是应用会重新拉一次（那是「过期后会重新
// 拉」那一条验收的一半）。
func TestSevenDaysLaterItIsGone(t *testing.T) {
	c := NewCache(t.TempDir())
	if err := c.Save("BV1", "1", Entry{
		Items:     []bili.Danmaku{{Text: "x"}},
		FetchedAt: time.Now().Add(-8 * 24 * time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	if _, ok := c.Load("BV1", "1"); ok {
		t.Error("8 天前的那一份还在——它该过期了")
	}
}

// TestFreshAtSixDays 钉住边界：6 天前的那一份**还在**。
func TestFreshAtSixDays(t *testing.T) {
	c := NewCache(t.TempDir())
	if err := c.Save("BV1", "1", Entry{
		Items:     []bili.Danmaku{{Text: "x"}},
		FetchedAt: time.Now().Add(-6 * 24 * time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	if _, ok := c.Load("BV1", "1"); !ok {
		t.Error("6 天前的那一份没了——7 天之内都该还在")
	}
}

// TestAnEntryWithoutAMomentCountsAsExpired 盯「不知道什么时候取的」那一种。
//
// 零值时刻 = 「这一份的来路不明」，而拿一份来路不明的东西当新的，比多拉一次
// 危险得多。
func TestAnEntryWithoutAMomentCountsAsExpired(t *testing.T) {
	e := Entry{Items: []bili.Danmaku{{Text: "x"}}}
	if !e.Expired(time.Now()) {
		t.Error("没有时刻的那一份被当成新鲜的了")
	}
}

// TestThePathRefusesAnIdentityThatIsNotAnID 盯「文件名不能被拼坏」。
//
// 身份里带着路径分隔符的时候，拼出来的名字会跑到别的目录里去——而缓存目录
// 外面的文件，本层管不着。
func TestThePathRefusesAnIdentityThatIsNotAnID(t *testing.T) {
	c := NewCache(t.TempDir())
	bad := []struct {
		bvid string
		cid  string
	}{
		{"", "1"},
		{"BV1", ""},
		{"../逃出去", "1"},
		{"BV1", "../../逃出去"},
		{"BV1", "1/2"},
	}
	for _, b := range bad {
		if got := c.Path(b.bvid, b.cid); got != "" {
			t.Errorf("(%q, %q) 拼出了路径 %q", b.bvid, b.cid, got)
		}
	}
}
