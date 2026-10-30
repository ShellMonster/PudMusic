package streamaudio

// 字节缓存的判据钉死。
//
// # 为什么这个包必须有自己的测试
//
// 这一层每一个判据都是「**用户听到什么**」的直接决定因素：
//
//	Complete 那一栏	这一份能不能播（判错了 = 放出噪声或者永远放不出来）
//	档位标识		用户点 A 放出来的是不是 B
//	淘汰		正在听的歌会不会被删掉
//
// 而它们在应用层那些用例里全是**间接**的——中间隔着解码器、网络、播放器。
// 判据本身出错的时候，应用层的用例给出的信息是「歌没放出来」，
// 而「为什么」要读好几层代码才知道。

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// writeBytesFile 造一份「盘上的字节」。
func writeBytesFile(t *testing.T, c *Cache, track, tier string, size int) string {
	t.Helper()
	path := c.BytesPath(track, tier)
	if path == "" {
		t.Fatal("缓存路径是空串")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, make([]byte, size), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestALookupNeedsBothTheMappingAndTheBytes 盯「两张表都要有」。
func TestALookupNeedsBothTheMappingAndTheBytes(t *testing.T) {
	c := NewCache(t.TempDir())
	track, pref, tier := "online|mv|BV1|1|", "0", "q30280"

	// 什么都没有。
	if _, ok := c.Lookup(track, pref); ok {
		t.Error("什么都没有却查到了")
	}

	// 有映射，没有字节。
	if err := c.SavePref(track, pref, tier); err != nil {
		t.Fatal(err)
	}
	if _, ok := c.Lookup(track, pref); ok {
		t.Error("只有映射表就查到了——那张表指向的字节根本不存在")
	}

	// 有字节，没有记录（那一栏说「下完了」）。
	writeBytesFile(t, c, track, tier, 100)
	if _, ok := c.Lookup(track, pref); ok {
		t.Error("只有字节文件就查到了——没下完的不能当缓存用")
	}

	// 两样都有。
	if err := c.SaveBytes(track, tier, 100, "192K", Meta{}); err != nil {
		t.Fatal(err)
	}
	if _, ok := c.Lookup(track, pref); !ok {
		t.Error("两样都有却查不到")
	}
}

// TestTheSizeOnDiskHasToMatch 盯「盘上的字节大小要对得上」。
//
// 🔴 **这是最要紧的一道**：一个**下到一半**的文件，大小可能和整首**一样**
// （末尾那一段是并行的第二条 Range 请求取回来的），而它的中间是**洞**——
// 洞里的字节是零，解出来是噪声。
func TestTheSizeOnDiskHasToMatch(t *testing.T) {
	c := NewCache(t.TempDir())
	track, pref, tier := "online|mv|BV1|1|", "0", "q30280"
	writeBytesFile(t, c, track, tier, 100)
	if err := c.SavePref(track, pref, tier); err != nil {
		t.Fatal(err)
	}
	// 记录说 100 字节，盘上也是 100 → 命中。
	if err := c.SaveBytes(track, tier, 100, "192K", Meta{}); err != nil {
		t.Fatal(err)
	}
	if _, ok := c.Lookup(track, pref); !ok {
		t.Fatal("大小对得上却查不到")
	}

	// 盘上的字节被截断了 → 不该命中。
	if err := os.Truncate(c.BytesPath(track, tier), 50); err != nil {
		t.Fatal(err)
	}
	if _, ok := c.Lookup(track, pref); ok {
		t.Error("盘上的字节被截断了却查到了——用户会听到放了一半的歌")
	}
}

// TestAnUnknownLengthIsNotComplete 盯「不知道有多长就不算完整」。
//
// 🔴 **服务器不给 `Content-Length` 时会走到这里**：连接中途结束，而客户端
// 分不出「结束」和「被掐了」——那两种长得一模一样。
func TestAnUnknownLengthIsNotComplete(t *testing.T) {
	c := NewCache(t.TempDir())
	track, pref, tier := "online|mv|BV1|1|", "0", "q30280"
	writeBytesFile(t, c, track, tier, 100)
	if err := c.SavePref(track, pref, tier); err != nil {
		t.Fatal(err)
	}
	if err := c.SaveBytes(track, tier, -1, "192K", Meta{}); err != nil {
		t.Fatal(err)
	}
	if _, ok := c.Lookup(track, pref); ok {
		t.Error("长度不知道却查到了——那一份可能是截断的")
	}
}

// TestTheExpiryIsSevenDays 盯「过期了就不算」。
func TestTheExpiryIsSevenDays(t *testing.T) {
	now := time.Now()
	c := NewCache(t.TempDir()).WithClock(func() time.Time { return now })
	track, pref, tier := "online|mv|BV1|1|", "0", "q30280"
	writeBytesFile(t, c, track, tier, 100)
	if err := c.SavePref(track, pref, tier); err != nil {
		t.Fatal(err)
	}
	if err := c.SaveBytes(track, tier, 100, "192K", Meta{}); err != nil {
		t.Fatal(err)
	}
	if _, ok := c.Lookup(track, pref); !ok {
		t.Fatal("刚下完就查不到")
	}

	// 往前推 8 天。
	c.WithClock(func() time.Time { return now.Add(8 * 24 * time.Hour) })
	if _, ok := c.Lookup(track, pref); ok {
		t.Error("过期 8 天了还查得到")
	}
}

// TestEvictKeepsTheOneInUse 盯「正在用的那一份不许被淘汰」。
//
// 🔴 **这是最不能接受的一种后果**：用户正在听的歌被删掉 → 歌就断了。
func TestEvictKeepsTheOneInUse(t *testing.T) {
	c := NewCache(t.TempDir()).WithCapacity(1000)
	// 三份，每份 400 字节（总共 1200 > 1000）。
	for _, tier := range []string{"q30216", "q30232", "q30280"} {
		writeBytesFile(t, c, "online|mv|BV1|1|", tier, 400)
		if err := c.SaveBytes("online|mv|BV1|1|", tier, 400, "x", Meta{}); err != nil {
			t.Fatal(err)
		}
	}
	// 标住「正在用的那一份」。
	c.Protect("online|mv|BV1|1|", "q30280")

	c.Evict()

	if _, err := os.Stat(c.BytesPath("online|mv|BV1|1|", "q30280")); err != nil {
		t.Errorf("正在用的那一份被淘汰了：%v——用户正在听的歌会断掉", err)
	}
}

// TestEvictDropsTheOldestFirst 盯「淘汰是最久没动过的先删」。
func TestEvictDropsTheOldestFirst(t *testing.T) {
	base := time.Now()
	clock := base
	c := NewCache(t.TempDir()).WithCapacity(1000).WithClock(func() time.Time { return clock })
	track := "online|mv|BV1|1|"
	// 三份，每份 400 字节。依次写，TouchedAt 一份比一份新。
	for i, tier := range []string{"q30216", "q30232", "q30280"} {
		clock = base.Add(time.Duration(i) * time.Hour)
		writeBytesFile(t, c, track, tier, 400)
		if err := c.SaveBytes(track, tier, 400, "x", Meta{}); err != nil {
			t.Fatal(err)
		}
	}
	c.Evict()
	// 最老的那一份没了，最新的还在。
	if _, err := os.Stat(c.BytesPath(track, "q30216")); !os.IsNotExist(err) {
		t.Error("最老的那一份没被淘汰")
	}
	if _, err := os.Stat(c.BytesPath(track, "q30280")); err != nil {
		t.Errorf("最新的一份被淘汰了：%v", err)
	}
}

// TestTheOrphanBytesAreCounted 盯「没有记录的字节也算占地方」。
//
// 🔴 **一次「没下完就切歌」就留下一个孤儿**——而它在容量账本之外的话，
// 缓存目录会无上限地长，2 GB 上限形同虚设。
func TestTheOrphanBytesAreCounted(t *testing.T) {
	c := NewCache(t.TempDir()).WithCapacity(1000)
	track := "online|mv|BV1|1|"
	// 一份有记录的（100 字节）+ 一个孤儿（5000 字节）。
	writeBytesFile(t, c, track, "q30280", 100)
	if err := c.SaveBytes(track, "q30280", 100, "x", Meta{}); err != nil {
		t.Fatal(err)
	}
	// 孤儿：**只有字节、没有记录**——那是「没下完就切歌」留下的。
	orphan := filepath.Join(c.Dir(), bytesPrefix+encode(track)+"-"+encode("q30216")+".bin")
	if err := os.WriteFile(orphan, make([]byte, 5000), 0o644); err != nil {
		t.Fatal(err)
	}

	c.Evict()

	if _, err := os.Stat(orphan); !os.IsNotExist(err) {
		t.Error("孤儿没被淘汰——它占着地方却不在账本里，缓存目录会无上限地长")
	}
}

// TestCleanOrphansRemovesTheLeftovers 盯「启动时清残留」。
//
// 🔴 **上一个进程被杀 / 崩掉会留下这些**：只有字节、没有记录（半截下载），
// 或者只有记录、没有字节（用户手工删过）。
func TestCleanOrphansRemovesTheLeftovers(t *testing.T) {
	dir := t.TempDir()
	c := NewCache(dir)
	track, tier := "online|mv|BV1|1|", "q30280"

	// 只有字节、没有记录。
	lonely := filepath.Join(dir, bytesPrefix+encode(track)+"-"+encode("q30216")+".bin")
	if err := os.WriteFile(lonely, make([]byte, 100), 0o644); err != nil {
		t.Fatal(err)
	}
	// 只有记录、没有字节。
	if err := c.SaveBytes(track, tier, 100, "x", Meta{}); err != nil {
		t.Fatal(err)
	}
	// 一份正常的（两个文件都在）。
	writeBytesFile(t, c, track, "q30232", 100)
	if err := c.SaveBytes(track, "q30232", 100, "x", Meta{}); err != nil {
		t.Fatal(err)
	}

	if n := c.CleanOrphans(); n != 2 {
		t.Errorf("清掉了 %d 个残留，want 2", n)
	}
	if _, err := os.Stat(lonely); !os.IsNotExist(err) {
		t.Error("只有字节没有记录的那份还在")
	}
	if _, err := os.Stat(c.bytesMetaPath(track, tier)); !os.IsNotExist(err) {
		t.Error("只有记录没有字节的那份还在")
	}
	// 正常的那一份还在。
	if _, err := os.Stat(c.BytesPath(track, "q30232")); err != nil {
		t.Errorf("正常的那一份被清掉了：%v", err)
	}
}

// TestThePrefMappingCanPointAtAnotherPref 盯「两个偏好可以指向同一份字节」。
//
// 🔴 **这是这一票的核心**：用户点了「无损」而这一首没有无损，B 站给 192K。
// 于是「无损」和「高音质」在**这一首**上解析到同一档——它们必须**共用**
// 一份字节，而不是各下一遍。
func TestThePrefMappingCanPointAtAnotherPref(t *testing.T) {
	c := NewCache(t.TempDir())
	track, tier := "online|mv|BV1|1|", "q30280"
	writeBytesFile(t, c, track, tier, 100)
	if err := c.SaveBytes(track, tier, 100, "192K", Meta{}); err != nil {
		t.Fatal(err)
	}
	// 「无损」（偏好 4）和「高音质」（偏好 0）都指向 192K 那一档。
	if err := c.SavePref(track, "4", tier); err != nil {
		t.Fatal(err)
	}
	if err := c.SavePref(track, "0", tier); err != nil {
		t.Fatal(err)
	}
	for _, pref := range []string{"0", "4"} {
		if _, ok := c.Lookup(track, pref); !ok {
			t.Errorf("偏好 %s 查不到——两个偏好指向同一档时必须共用一份字节", pref)
		}
	}
	// 盘上只有**一份**字节。
	entries, err := os.ReadDir(c.Dir())
	if err != nil {
		t.Fatal(err)
	}
	bins := 0
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".bin" {
			bins++
		}
	}
	if bins != 1 {
		t.Errorf("盘上有 %d 个字节文件，want 1——两个偏好不该各存一份", bins)
	}
}

// TestAnEmptyCacheDoesNothing 盯「没目录就什么都不做」。
func TestAnEmptyCacheDoesNothing(t *testing.T) {
	c := NewCache("")
	if _, ok := c.Lookup("online|mv|BV1|1|", "0"); ok {
		t.Error("空缓存查到了东西")
	}
	if err := c.SaveBytes("online|mv|BV1|1|", "q30280", 100, "x", Meta{}); err != nil {
		t.Errorf("空缓存写失败了：%v", err)
	}
	if err := c.SavePref("online|mv|BV1|1|", "0", "q30280"); err != nil {
		t.Errorf("空缓存写失败了：%v", err)
	}
	if n := c.CleanOrphans(); n != 0 {
		t.Errorf("空缓存清了 %d 个东西", n)
	}
	c.Evict()
}

// TestTheRecordIsFoundByItsFileName 盯「记录靠文件名认，不靠里面那一栏」。
//
// 🔴 **拿记录里那一栏去算路径**的话，用户手改过 `.json` 之后，
// 淘汰会**删掉另一个文件**，而真正那个 `.bin` 永远删不掉。
func TestTheRecordIsFoundByItsFileName(t *testing.T) {
	dir := t.TempDir()
	c := NewCache(dir)
	track, tier := "online|mv|BV1|1|", "q30280"
	writeBytesFile(t, c, track, tier, 100)
	if err := c.SaveBytes(track, tier, 100, "x", Meta{}); err != nil {
		t.Fatal(err)
	}
	// 把记录里的身份改成别的（模拟用户手改 / 旧版本写的）。
	metaPath := c.bytesMetaPath(track, tier)
	raw, err := os.ReadFile(metaPath)
	if err != nil {
		t.Fatal(err)
	}
	tampered := strings.Replace(string(raw),
		`"track":"`+track+`"`, `"track":"online|mv|BV9|9|"`, 1)
	if tampered == string(raw) {
		t.Fatalf("没改成（原始内容：%s）", raw)
	}
	if err := os.WriteFile(metaPath, []byte(tampered), 0o644); err != nil {
		t.Fatal(err)
	}

	// 淘汰要删掉**真的那一个** `.bin`。
	c.WithCapacity(1)
	c.Evict()
	if _, err := os.Stat(c.BytesPath(track, tier)); !os.IsNotExist(err) {
		t.Error("真正的那个字节文件没被删——淘汰靠记录里那一栏算路径就找错了文件")
	}
}
