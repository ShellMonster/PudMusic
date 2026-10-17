package app

// 「换回来的还是不是同一首」那道闸门。
//
// # 为什么这道闸门要单独一个文件
//
// 它是 `refetch` 里两行判断，而那两行**曾经整段是死的**：音乐库那一路
// （type=12 / sid）的 `StreamRef.Sid` 当时是拿**请求里**那个 sid 填的，于是
// `sameItem(ref, it)` 比的是「我请求的 == 我请求的」，`SameTrack(prev, ref)`
// 同理——**恒为真**。而当时钉它的那条用例是端到端的（CDN 同时被作废，所以歌
// 无论如何都会停），**闸门在不在它都 PASS**。
//
// 🔴 端到端验不了这一层：闸门拒绝之后歌确实停了，闸门失效之后源判成「换了另一
// 条流」重开、也还是那几段同样的字节——**两边在界面上长得一样**。所以这一层只能
// **直接测那个决定**：闸门在位就返回 nil，删掉就返回一条 reply。
//
// 三条各钉各的一句：
//
//	TestSameItemTellsThisItemFromAnother   判据本身（`sameItem` 的函数体）
//	TestRefetchRefusesAStreamFromAnotherSong  refetch 里那一句 `sameItem`
//	TestRefetchRefusesASongChangeEvenWithAPreviousRef  有 prev 时也不许放行
//	bili.TestSameTrackTellsMusicLibraryEntriesApart   `SameTrack`（恒真就红）
//
// 所以这个文件是 `package app`（不是 `app_test`）：`refetch` 和 `sameItem` 都是
// 未导出的，而把它们导出去只为让测试够得着，是把实现的形状让给测试。

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ShellMonster/Biu_Go/internal/service/bili"
)

// TestSameItemTellsThisItemFromAnother 盯身份判据本身。
//
// 🔴 **音乐库那一路 Bvid / Cid 都是空的**：只按它们判的话这一路的任何两条流都会
// 被认成同一首，而这一项的空 Sid 也不能放行——空着不是「谁都跟它一样」，
// 那是「谁都查不出来」。
func TestSameItemTellsThisItemFromAnother(t *testing.T) {
	for _, c := range []struct {
		what string
		it   Item
		ref  *bili.StreamRef
		want bool
	}{
		{"同一个 sid", Item{Type: "audio", Sid: "1"}, &bili.StreamRef{Sid: "1"}, true},
		{"另一个 sid", Item{Type: "audio", Sid: "1"}, &bili.StreamRef{Sid: "2"}, false},
		{"ref 没有 sid", Item{Type: "audio", Sid: "1"}, &bili.StreamRef{}, false},
		{"item 没有 sid", Item{Type: "audio"}, &bili.StreamRef{Sid: "1"}, false},
		{"同一首投稿", Item{Type: "mv", Bvid: "B", Cid: "7"}, &bili.StreamRef{Bvid: "B", Cid: "7"}, true},
		{"另一首投稿", Item{Type: "mv", Bvid: "B", Cid: "7"}, &bili.StreamRef{Bvid: "B", Cid: "8"}, false},
		{"ref 是 nil", Item{Type: "audio", Sid: "1"}, nil, false},
	} {
		if got := sameItem(c.ref, c.it); got != c.want {
			t.Errorf("%s：sameItem = %v，want %v", c.what, got, c.want)
		}
	}
}

// refetchFixture 是一台只回音乐库取流的假 B 站，它给回来的 sid 是 fixedSid。
//
// ⚠️ **sid 是从返回里给的，不是从请求里抄的** —— B 站真的这么回，而那道闸门
// 读的就是它。fixture 要是「回请求里的那个 sid」，整条链会绿得很好看，
// 而闸门在真机上是死的（这正是这个文件顶上写的那件事）。
func refetchFixture(t *testing.T, fixedSid int) *Streaming {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 0, "msg": "success", "ttl": 1,
			"data": map[string]any{
				"sid": fixedSid, "type": int(bili.MusicTier192), "timeout": 10800,
				"cdns":      []string{"https://cdn.example/song.m4a?deadline=1"},
				"qualities": []any{map[string]any{"type": 1, "require": 0}},
			},
		})
	}))
	t.Cleanup(srv.Close)

	cli := bili.NewClient(bili.Options{})
	cli.TestOnlyPointHostsAtFixture(srv.URL)
	return &Streaming{Client: cli}
}

// TestRefetchRefusesAStreamFromAnotherSong 换流拿回来的是**另一首**就得停。
//
// 这一条盯 `sameItem` 那一半，而且**特意没有 prev**：没有手上那条流可比的时候，
// 只有「取回来的这条是不是这一项的」拦得住。放行的话源会拿它续，用户会在毫无
// 提示的情况下听到另一首歌——而界面上什么都看不出来。
//
// 🔴 把 `refetch` 里那一句 `if !sameItem(ref, it)` 删掉，这条立刻红。
func TestRefetchRefusesAStreamFromAnotherSong(t *testing.T) {
	s := refetchFixture(t, 9999999) // B 站给的是另一首的流
	a := New()
	t.Cleanup(a.Close)

	it := Item{Source: "online", Type: "audio", Sid: "1293494", Title: "这一首"}
	if got := a.refetch(context.Background(), s, it); got != nil {
		t.Errorf("取回来的流属于 sid=9999999，refetch 却放行了：%+v", got)
	}
}

// TestRefetchRefusesASongChangeEvenWithAPreviousRef 手上已有流时也要拦。
//
// 这一条盯的是**换了流以后和手上那条比**那条路（`SameTrack` 比的是两条流，
// 不是流和条目）。它和上一条**互相覆盖**：上一条没有 prev，只有 `sameItem`
// 拦得住；这一条有 prev，而两者对「sid 变了」都会拦——所以**单独删掉其中
// 任何一句，这一层都还是绿的**，那不是这两句的错，是它们在这里重叠。
//
// 各钉各的那一句在别处：`sameItem` 由上一条钉（删掉就红），`SameTrack` 由
// `bili.TestSameTrackTellsMusicLibraryEntriesApart` 钉（把它改成恒真就红）。
// 这一条留着是为了钉「**有 prev 时也不许放行**」——那是另一条路径（换流重试
// 时手上那条还在），而它一旦退化，症状是换流之后静默换成另一首歌。
func TestRefetchRefusesASongChangeEvenWithAPreviousRef(t *testing.T) {
	s := refetchFixture(t, 9999999)
	a := New()
	t.Cleanup(a.Close)

	it := Item{Source: "online", Type: "audio", Sid: "1293494", Title: "这一首"}
	a.online = &onlineTrack{item: it, ref: &bili.StreamRef{Sid: "1293494", MusicTier: bili.MusicTier192}}

	if got := a.refetch(context.Background(), s, it); got != nil {
		t.Errorf("换流换成了 sid=9999999，却当成同一首放行了：%+v", got)
	}
}

// TestRefetchStillServesTheSameSong 闸门在位的时候**别把正常的换流也拦了**。
//
// 前两条只钉「拦得住」，这一条钉「拦得准」：同一首再问一次是要接着用的
// （源要从同一个字节偏移往下接），把它也拦掉的话换流这条路一次都跑不了，
// 而症状是「播到一半断了再也接不上」。
func TestRefetchStillServesTheSameSong(t *testing.T) {
	s := refetchFixture(t, 1293494)
	a := New()
	t.Cleanup(a.Close)

	it := Item{Source: "online", Type: "audio", Sid: "1293494", Title: "这一首"}
	a.online = &onlineTrack{item: it, ref: &bili.StreamRef{Sid: "1293494", MusicTier: bili.MusicTier192}}

	got := a.refetch(context.Background(), s, it)
	if got == nil {
		t.Fatal("同一首的换流被当成别的歌拦掉了：换流这条路一次都跑不了")
	}
	if len(got.URLs) == 0 {
		t.Error("放行了但一条地址都没有")
	}
}
