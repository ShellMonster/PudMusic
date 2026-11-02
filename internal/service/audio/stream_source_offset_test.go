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
		if n := rangeStart(r); n >= 0 {
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
