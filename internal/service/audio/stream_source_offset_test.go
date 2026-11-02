package audio

// 断点续传的字节源那一半（2026-11-02）。
//
// # 这条治的是什么
//
// 「切歌收养补完」管的是**一个进程之内**：被切走的源活着下完。进程退出时
// 下到一半的那一份，原来只能整个扔掉——带 CachePath 的源每次打开都
// `O_TRUNC` 从零下。于是用户听合集听到一半退出，下次冷启动还是从零等。
//
// 修法：源把**顺序下载前沿**记在旁挂的偏移账本里（`OffsetPath`，应用层
// 从缓存那层拿到路径），下一次建源时把账本上的数交给 `ResumeFrom`——
// 泵本来就支持「从 Filled() 起发 Range」（会话内换流续传走的就是它），
// 这里只是把那个数**种**进去。
//
// # 判据
//
//	关源那一刻	账本上就是已经下到的字节数（一个字节不差——Close 时补写）
//	按账本续传	第二条源发的 Range 起点 == 账本上的数
//	下完		账本退役（完整记录取代它）
//	账本说谎	（起点比文件还大）老实从零来，不许拿洞当字节
import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestTheFrontierIsLedgeredOnCloseAndResumed 盯「关源记账 → 按账续传 → 下完销账」。
func TestTheFrontierIsLedgeredOnCloseAndResumed(t *testing.T) {
	data := payload(64 << 10)
	var rangeStarts atomic.Int64 // 续传那一条源的 Range 起点（记最后一次）
	// 🔴 **服务器要比读得慢**：64KB 一口气倒完的话，Close 之前泵就已经
	// 到 EOF，走的是「下完退役」分支——测的就是正常路径，不是续传。
	// 闸门控住后半段，Close 时前沿必然停在半路。
	gate := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if n := rangeStart(r); n > 0 {
			// 🔴 **只数主请求的起点**：续传前那一次「头部抽查」也是 0 起的
			// Range（它要的就是头部），把它算进来就看不出续传的起点对不对。
			rangeStarts.Store(n)
		}
		half := len(data) / 2
		w.Header().Set("Content-Length", fmt.Sprint(len(data)))
		w.Write(data[:half])
		w.(http.Flusher).Flush()
		select {
		case <-gate:
		case <-r.Context().Done():
			return
		}
		w.Write(data[half:])
	}))
	t.Cleanup(func() {
		select {
		case <-gate:
		default:
			close(gate)
		}
		srv.Close()
	})

	dir := t.TempDir()
	cachePath := filepath.Join(dir, "song.bin")
	offsetPath := filepath.Join(dir, "song.bin.off")

	// 第一条源：读至少 4 KB（把下载前沿推过去），然后关——这一步模拟
	// 「退出时只下到一半」。
	//
	// ⚠️ **短读是规格**：Read 只给「连续」的那一段，一次未必凑满。而账本记
	// 的是**下载前沿**（filled），它可能比读到的多——所以断言是
	// 「账本 ≥ 已读量、≤ 整首」，把账本上的数原样交给第二条源去验。
	s1, err := NewStreamSource(StreamSourceOptions{
		URLs: []string{srv.URL}, Client: srv.Client(),
		CachePath: cachePath, OffsetPath: offsetPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	consumed := 0
	chunk := make([]byte, 4096)
	deadline := time.Now().Add(10 * time.Second)
	for consumed < 4096 {
		n, err := s1.Read(chunk)
		consumed += n
		if err != nil {
			t.Fatalf("第一次读（已读 %d）：%v", consumed, err)
		}
		if time.Now().After(deadline) {
			t.Fatalf("下载没推进（只读到 %d）", consumed)
		}
	}
	if err := s1.Close(); err != nil {
		t.Fatal(err)
	}

	// 🔴 **关源那一刻，账本上就是前沿**：停在半路（前一半已发出、闸门未放），
	// 所以它落在 [已读量, 前一半] 之间——一个字节不差是不可能的（泵比读
	// 跑得快），落不进这个区间才是错的。
	raw, err := os.ReadFile(offsetPath)
	if err != nil {
		t.Fatalf("关源之后账本没写上：%v", err)
	}
	ledger, perr := strconv.ParseInt(string(raw), 10, 64)
	if perr != nil {
		t.Fatalf("账本撕了：%q（%v）", string(raw), perr)
	}
	half := int64(len(data) / 2)
	if ledger < int64(consumed) || ledger > half {
		t.Fatalf("账本 = %d，want [%d, %d]（半路）", ledger, consumed, half)
	}
	if st, err := os.Stat(cachePath); err != nil || st.Size() < ledger {
		t.Fatalf("字节文件不够续传：size=%v ledger=%d err=%v", st, ledger, err)
	}

	// 放行闸门，第二条源：拿账本续传。Range 起点必须是账本上的数——从零
	// 来的话这一条就白搭。
	close(gate)
	s2, err := NewStreamSource(StreamSourceOptions{
		URLs: []string{srv.URL}, Client: srv.Client(),
		CachePath: cachePath, OffsetPath: offsetPath,
		ResumeFrom: ledger,
	})
	if err != nil {
		t.Fatal(err)
	}
	// 读完剩下的部分（从游标 0 开始读整首——盘上前 4096 个字节是上一条源
	// 留下的，内容一样）。
	buf := make([]byte, 0, len(data))
	chunk = make([]byte, 8192)
	deadline = time.Now().Add(10 * time.Second)
	for len(buf) < len(data) {
		n, err := s2.Read(chunk)
		buf = append(buf, chunk[:n]...)
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			t.Fatalf("续传读到 %d：%v", len(buf), err)
		}
		if time.Now().After(deadline) {
			t.Fatalf("续传没推进到头（%d / %d）", len(buf), len(data))
		}
	}
	if len(buf) != len(data) {
		t.Fatalf("读出来的整首 = %d 字节，want %d", len(buf), len(data))
	}
	if got := rangeStarts.Load(); got != ledger {
		t.Fatalf("续传的 Range 起点 = %d，want 账本上的 %d——半截字节没有接上", got, ledger)
	}

	// 🔴 **下完了账本退役**：Close 之后的完整记录（测试直接关源，EOF 已记）。
	if err := s2.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(offsetPath); !os.IsNotExist(err) {
		t.Errorf("下完了账本还在：%v", err)
	}
}

// TestABogusResumeStartsFresh 盯「账本说谎就老实从零」。
//
// 起点比盘上文件还大，硬信它的话 Range 会从「不存在的字节」续起——
// 文件先截到那个数、前面全是洞。判据：Range 从 0 起，整首照样完整。
func TestABogusResumeStartsFresh(t *testing.T) {
	data := payload(32 << 10)
	var firstStart atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if n := rangeStart(r); n > 0 {
			firstStart.Store(n)
		}
		serveBytes(w, r, data, len(data))
	}))
	t.Cleanup(srv.Close)

	dir := t.TempDir()
	s, err := NewStreamSource(StreamSourceOptions{
		URLs: []string{srv.URL}, Client: srv.Client(),
		CachePath:  filepath.Join(dir, "song.bin"),
		OffsetPath: filepath.Join(dir, "song.bin.off"),
		ResumeFrom: 1 << 20, // 比整首还大
	})
	if err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 0, len(data))
	chunk := make([]byte, 8192)
	for len(buf) < len(data) {
		n, err := s.Read(chunk)
		buf = append(buf, chunk[:n]...)
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			t.Fatalf("读：%v", err)
		}
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if got := firstStart.Load(); got != 0 {
		t.Fatalf("谎报的起点被采信了（Range 从 %d 起）——前面全是洞", got)
	}
	if len(buf) != len(data) {
		t.Fatalf("整首 = %d 字节，want %d", len(buf), len(data))
	}
}

// TestAResumeOnDifferentBytesStartsFresh 盯「**这段内容变了就不许续传**」
// （2026-11-02 审查②的 P0-1）。
//
// # 为什么这一条是 P0
//
// B 站同一首（同一个 bvid+cid）返回的字节**不是契约稳定的**：稿件被替换、
// CDN 换对象、会员降级后拿到另一段字节——而缓存键和长度都没变。
// 于是上一次退出时留下的半截 + 这一次拿回来的新内容 = **一个两段拼接的
// 文件**，大小与整首一致 → 记成 `complete:true` → **永久坏缓存**：用户
// 此后每次切回这一首都播那段拼接货，而且因为「大小对得上」，它**永远
// 不会被重下**。
//
// # 判据
//
// 盘上放一份「旧内容」的半截，服务器这一次给**另一份**内容：续传必须
// 被放弃（盘上那份被清掉，Range 从 0 起，整首完整）。
func TestAResumeOnDifferentBytesStartsFresh(t *testing.T) {
	old := payload(64 << 10)
	fresh := payload(64 << 10)
	// 🔴 **两份内容必须真的不同**（第一版这个用例把 `old` 和 `fresh` 都来自
	// 同一个 `payload()`——它们一模一样，断言自然全绿，测了个寂寞）。
	//
	// ⚠️ **差异必须落在「盘上留着的」那一段**（前半段）：那是续传**不会重下**
	// 的部分——而它正是用户会听到坏东西的地方。差异只放在后半段的话，
	// 拼出来的文件跟新内容一模一样，测不出任何东西。
	for i := 0; i < 32768; i++ {
		fresh[i] = fresh[i] ^ 0xFF
	}

	var rangeStarts []int64
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := rangeStart(r)
		mu.Lock()
		rangeStarts = append(rangeStarts, start)
		mu.Unlock()
		// 这次给的是**新内容**。
		serveBytes(w, r, fresh, len(fresh))
	}))
	t.Cleanup(srv.Close)

	dir := t.TempDir()
	cachePath := filepath.Join(dir, "song.bin")
	offsetPath := filepath.Join(dir, "song.bin.off")
	// 盘上留下「旧内容」的前 32 KB + 一个说 32 KB 的账本。
	if err := os.WriteFile(cachePath, old[:32<<10], 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(offsetPath, []byte("32768"), 0o644); err != nil {
		t.Fatal(err)
	}

	s, err := NewStreamSource(StreamSourceOptions{
		URLs: []string{srv.URL}, Client: srv.Client(),
		CachePath: cachePath, OffsetPath: offsetPath,
		ResumeFrom: 32768,
	})
	if err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 0, len(fresh))
	chunk := make([]byte, 8192)
	deadline := time.Now().Add(10 * time.Second)
	for len(buf) < len(fresh) {
		n, rerr := s.Read(chunk)
		buf = append(buf, chunk[:n]...)
		if rerr != nil {
			if errors.Is(rerr, io.EOF) {
				break
			}
			t.Fatalf("读：%v", rerr)
		}
		if time.Now().After(deadline) {
			t.Fatalf("没读够（%d / %d）", len(buf), len(fresh))
		}
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	// 🔴 **盘上那份必须是完整的新内容**（不是两段拼接）。
	got, err := os.ReadFile(cachePath)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(fresh) {
		t.Fatalf("盘上 %d 字节，want %d", len(got), len(fresh))
	}
	fmt.Printf("[probe] rangeStarts=%v gotLen=%d\n", rangeStarts, len(got))
	fmt.Printf("[probe] got[0:8]=%v fresh[0:8]=%v\n", got[:8], fresh[:8])
	fmt.Printf("[probe] got[32768:32776]=%v fresh[32768:32776]=%v old[32768:32776]=%v\n", got[32768:32776], fresh[32768:32776], old[32768:32776])
	// 🔴 **比整个文件**：拼接的证据在**前半段**（上一次留下的旧内容）。
	for i := range fresh {
		if got[i] != fresh[i] {
			t.Fatalf("盘上第 %d 字节 = %d，want %d——续传把两段内容拼起来了"+
				"（前半段是上一次留下的旧内容）", i, got[i], fresh[i])
		}
	}
}
