package app_test

// 反复切歌时，**已经取过的流地址要能复用**，不能每次都重新问 B 站。
//
// # 这条盯的是真问题（2026-10-30 用户提出来的）
//
// 原来的缓存只留**一条**：`a.streamCache = map[string]*bili.StreamRef{key: ref}`
// ——每取到一条新的就把整张表重建成那一条。于是：
//
//	A → B → A → B → A
//
// 每一次切回去都要**重新问 B 站要一次地址**（实测 7 次切歌 = 7 次取流请求）。
// 用户快速来回切歌时，每切一下都等一趟带签名的网络往返——而那条地址明明
// 十几秒前才拿到过、有效期还有几小时。
//
// # 判据
//
// 数取流接口被调了几次：切 N 次只该调**第一次**（每首各一次），不该切一次调一次。
//
// ⚠️ 「每一首各一次」是底线：同一首**同时**被切两次不该发两趟（那是另一个
// 毛病，在途的那一趟还在路上），而**先后**切两次才该复用。

import (
	"testing"
	"time"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/service/bili"
)

// TestSwitchingBackAnd forthReusesTheStreamAddress 盯「来回切歌不重复取流」。
func TestSwitchingBackAndForthReusesTheStreamAddress(t *testing.T) {
	f := newStreamFixture(t)
	f.media = readFlac(t)
	f.onPlayURL(dashFor(cdnURL(f.URL, "a.flac", 2*time.Hour), int(bili.QualityHigh192), 110836))

	a := app.New()
	t.Cleanup(a.Close)
	cli := bili.NewClient(bili.Options{})
	cli.TestOnlyPointHostsAtFixture(f.URL)
	a.MountStreaming(app.Streaming{Client: cli, Media: f.Client(), TempDir: t.TempDir()})

	// A → B → A → B → A：两首来回切五遍。
	seq := []string{"A", "B", "A", "B", "A"}
	for i, who := range seq {
		if err := a.PlayFrom(onlineItem("BV1", who, "第 "+who+" 首")); err != nil {
			t.Fatalf("第 %d 次切到 %s：%v", i+1, who, err)
		}
		// 等这一首真的放起来
		deadline := time.Now().Add(10 * time.Second)
		for time.Now().Before(deadline) {
			if a.Snapshot().Title == "第 "+who+" 首" {
				break
			}
			time.Sleep(2 * time.Millisecond)
		}
	}

	// 两首各取一次就够；第三遍 A 不该再问 B 站。
	if got := f.playCalls.Load(); got > 2 {
		t.Errorf("来回切了 %d 次，取流接口被调了 %d 次，want ≤2（两首各一次）——"+
			"切回去的那一趟没有复用已经拿到的地址，用户每切一下都白等一趟网络往返", len(seq), got)
	}
}
