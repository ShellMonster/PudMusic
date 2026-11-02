package streamaudio

// 偏移文件：断点续传的账本（2026-11-02）。
//
// 一个下到一半的缓存文件，盘上大小**不可信**（末尾那一段是并行取回来的，
// 中间可能有洞——见 `service/audio` 的 `availAt`）。「顺序前沿到了第几字节」
// 记在旁挂的 `.off` 文件里，下一次播同一首拿它当续传起点。
//
// # 这一层管的是文件的位置和生命周期
//
//	OffsetPath	它落在哪儿（字节文件名 + ".off"）
//	Remove	删字节时连它一起删（别留孤儿）
//	SaveBytes	下完了就删（完整记录取代偏移，账本作废）
//	CleanOrphans	字节没了偏移还在 → 清（跟别的孤儿一个规矩）
//
// 真正**写**偏移的是字节源自己（`service/audio`），读和校验在应用层
// （`internal/app` 的 buildFromRef）——这一层不发请求也不算偏移。
import (
	"os"
	"testing"
)

func TestTheOffsetPathSitsNextToTheBytes(t *testing.T) {
	c := NewCache(t.TempDir())
	track, tier := "online|mv|BV1|1|", "q30280"
	p := c.OffsetPath(track, tier)
	if p == "" {
		t.Fatal("偏移路径是空串")
	}
	want := c.BytesPath(track, tier) + ".off"
	if p != want {
		t.Errorf("偏移路径 = %q，want %q（贴着字节文件）", p, want)
	}
	if c := NewCache(""); c.OffsetPath(track, tier) != "" {
		t.Error("空目录的缓存不该给偏移路径")
	}
}

func TestRemoveTakesTheOffsetWithIt(t *testing.T) {
	c := NewCache(t.TempDir())
	track, tier := "online|mv|BV1|1|", "q30280"
	writeBytesFile(t, c, track, tier, 100)
	if err := os.WriteFile(c.OffsetPath(track, tier), []byte("64"), 0o644); err != nil {
		t.Fatal(err)
	}
	c.Remove(track, tier)
	if _, err := os.Stat(c.OffsetPath(track, tier)); !os.IsNotExist(err) {
		t.Error("字节删了偏移还在——下次会拿它当一份不存在字节的续传起点")
	}
}

func TestCompletingTheDownloadRetiresTheOffset(t *testing.T) {
	c := NewCache(t.TempDir())
	track, tier := "online|mv|BV1|1|", "q30280"
	if err := os.WriteFile(c.OffsetPath(track, tier), []byte("64"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := c.SaveBytes(track, tier, 100, "x", Meta{}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(c.OffsetPath(track, tier)); !os.IsNotExist(err) {
		t.Error("下完了偏移还在——账本该被完整记录取代")
	}
}

func TestCleanOrphansSweepsTheLoneOffsets(t *testing.T) {
	c := NewCache(t.TempDir())
	// 有字节的偏移：留着（这一首还在续传账上）。
	//
	// ⚠️ **必须连记录一起造**（`SaveBytes`）：一份只有字节、没有记录的 `.bin`
	// 是**孤儿**（上一次进程被杀留下的半截），CleanOrphans 会把它连同账本
	// 一起清掉——那正是「正在续传的那一份」在崩溃后的归宿。
	writeBytesFile(t, c, "online|mv|BV1|1|", "q30280", 100)
	if err := c.SaveBytes("online|mv|BV1|1|", "q30280", 100, "x", Meta{}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(c.OffsetPath("online|mv|BV1|1|", "q30280"), []byte("64"), 0o644); err != nil {
		t.Fatal(err)
	}
	// 字节没了的偏移：孤儿，清。
	if err := os.WriteFile(c.OffsetPath("online|mv|BV1|2|", "q30280"), []byte("64"), 0o644); err != nil {
		t.Fatal(err)
	}

	if n := c.CleanOrphans(); n == 0 {
		t.Fatal("孤儿偏移没被清")
	}
	if _, err := os.Stat(c.OffsetPath("online|mv|BV1|1|", "q30280")); err != nil {
		t.Error("有字节的偏移被误清了")
	}
	if _, err := os.Stat(c.OffsetPath("online|mv|BV1|2|", "q30280")); !os.IsNotExist(err) {
		t.Error("孤儿偏移还在")
	}
}
