// 多 P 投稿的判重（票 #683）。
//
// # 用户报的那句话
//
// 「有些歌 B 站是一个视频下多个子集，点有些按钮时点里面的歌单好像播放不了」。
// 三个独立的现象都指向同一件事：**同一个 bvid 的两个分 P 被当成了同一首**。
//
//	队列抽屉	按 `Identity()` 去重，两集只画一行
//	EnqueueAndPlayNow	`indexByIdentity` 找到第一集，放的是第一集
//	PlayAt	`sameSong` 判真 → 只挪指针、不重新取流，播放没切换
//
// # 为什么保留「没有 cid 时退回按 bvid」
//
// 搜索结果只给 bvid，那时**还不知道是哪一集**——两首都没有 cid 的项本来就是
// 同一个「未知的那一集」，按同一首处理才对（不然同一首歌在抽屉里会出现两行）。
//
// 🔴 **「一个有一个没有」判不同**：有 cid 的那一项是**确定的某一集**，没 cid 的
// 是**未知**。判同一的话，定位（`indexByIdentity`）会落到未知的那一项上，于是
// 用户点的「第二集」被一个还不知道是哪一集的项顶掉——放出来的是第一集。
package app

import "testing"

// TestMvIdentityCarriesTheCid 钉住 mv 的判重标识带上 cid。
func TestMvIdentityCarriesTheCid(t *testing.T) {
	cases := []struct {
		name string
		item Item
		want string
	}{
		{"没有 cid 的 mv", Item{Source: "online", Type: "mv", Bvid: "BV1"}, "mv:BV1"},
		{"有 cid 的 mv", Item{Source: "online", Type: "mv", Bvid: "BV1", Cid: "777"}, "mv:BV1|777"},
	}
	for _, c := range cases {
		if got := c.item.Identity(); got != c.want {
			t.Errorf("%s：Identity() = %q，want %q", c.name, got, c.want)
		}
	}

	// 同一个投稿的两分集是**两首**（这一票要治的就是那件事）。
	one := Item{Source: "online", Type: "mv", Bvid: "BV1", Cid: "777"}
	two := Item{Source: "online", Type: "mv", Bvid: "BV1", Cid: "778"}
	if one.Identity() == two.Identity() {
		t.Errorf("同一个投稿的两分集还是同一个标识：%q", one.Identity())
	}
	if SameTrack(one, two) {
		t.Error("同一个投稿的两分集被判成了同一首")
	}

	// 都没有 cid：还是同一首（搜索结果那一档，两首都还不知道是哪一集）。
	unknown1 := Item{Source: "online", Type: "mv", Bvid: "BV1"}
	unknown2 := Item{Source: "online", Type: "mv", Bvid: "BV1"}
	if unknown1.Identity() != unknown2.Identity() {
		t.Errorf("两首都缺 cid 却给了不同的标识：%q / %q", unknown1.Identity(), unknown2.Identity())
	}
	if !SameTrack(unknown1, unknown2) {
		t.Error("两首都缺 cid 的同一个投稿被判成了两首——抽屉里会出现两行同一个视频")
	}

	// 🔴 一个有一个没有 → **不同**（理由见文件头）。
	if unknown1.Identity() == one.Identity() {
		t.Errorf("「不知道是哪一集」和「确定的第 777 集」是同一个标识：%q", one.Identity())
	}
	if SameTrack(unknown1, one) {
		t.Error("「不知道是哪一集」被当成了「确定的第 777 集」——" +
			"定位会落到未知的那一项上，用户点的第二集被顶掉")
	}
}

// TestTrackKeyIsTheOnlySourceOfTheIdentity 钉住 `Identity` / `SameTrack` 同一份口径。
//
// 两处各写一份的话，改了「Identity」忘了「SameTrack」，症状是「列表上标着正在
// 放的那一首，和抽屉里认的不是同一首」，而界面上看不出任何异样。
func TestTrackKeyIsTheOnlySourceOfTheIdentity(t *testing.T) {
	items := []Item{
		{Source: "online", Type: "mv", Bvid: "BV1"},
		{Source: "online", Type: "mv", Bvid: "BV1", Cid: "777"},
		{Source: "online", Type: "mv", Bvid: "BV2", Cid: "777"},
		{Source: "online", Type: "audio", Sid: "1"},
		LocalItem("/music/a.flac"),
		{},
	}
	for i, x := range items {
		for j, y := range items {
			sameIdentity := x.Identity() != "" && x.Identity() == y.Identity()
			if got := SameTrack(x, y); got != sameIdentity {
				t.Errorf("第 %d 项和第 %d 项：SameTrack=%v，而 Identity 相等=%v（%q / %q）",
					i, j, got, sameIdentity, x.Identity(), y.Identity())
			}
		}
	}
}

// TestPlayAtSwitchesBetweenTwoEpisodesOfOneVideo 和
// `TestEnqueueAndPlayNowPlaysTheEpisodeAskedFor` 是这条判据的两条**整链**用例
// ——它们要真的取流、真的出声，而这一份文件是 `package app`（没有那台假 B 站和
// 播放器夹具），所以它们住在外面的 `multipart_play_test.go`。

// TestIsPlayingMatchesARowThatDoesNotKnowTheEpisode 钉住**列表行和正在放的那一项
// 的「同一首」不是同一个问题**（票 #683）。
//
// 收藏夹 / 搜索结果的行只有 bvid（接口不给 cid），而队列里那一项起播之后
// **cid 被补上了**（缺陷 D）。按 `Identity` 严格比的话两者是「两首」
// （一个未知、一个确定），于是**用户在听的那首歌在列表上不高亮**——症状和
// 票 #279 那一类一样：有一行在跳，而它不是正在放的那一首（这一次是反过来：
// 正在放的那一行不跳）。
//
// 🔴 所以这一条判据是「同一个视频的同一集（两边的 cid 都知道时必须相等）」，
// 不是 `Identity` 的字符串相等。判据在 `sameStreamTarget`（一处）。
func TestIsPlayingMatchesARowThatDoesNotKnowTheEpisode(t *testing.T) {
	a := New()
	t.Cleanup(a.Close)

	playing := onlineItemOf("BV1", "正在放的那一集")
	playing.Cid = "777"
	playingNow(a, playing)

	// 行那一份只有 bvid（收藏夹 / 搜索结果的形状）。
	row := Item{Source: "online", Type: "mv", Bvid: "BV1", Title: "列表上那一行"}
	if !a.IsPlaying(row) {
		t.Error("正在放 BV1 的第 777 集，而只写了 bvid 的那一行没有被认成正在放")
	}

	// 另一集（cid 明确不同）**不该**亮。
	other := Item{Source: "online", Type: "mv", Bvid: "BV1", Cid: "888", Title: "另一集"}
	if a.IsPlaying(other) {
		t.Error("在放第 777 集，第 888 集那一条却被标成正在播放")
	}
}

// TestEnqueueAndPlayNowLocatesARowThatDoesNotKnowTheEpisode 钉住「点列表里那一行
// 不会在队列里多出一份」的另一半（票 #683）。
//
// 场景：队列里那一项起播之后 cid 被补上了（缺陷 D），而用户回到**列表**（收藏夹 /
// 搜索结果，接口只给 bvid）再点同一首——那一行的身份是「未知是哪一集」，队列里
// 那一项是「确定的第 777 集」。按 `Identity` 严格比是两首，于是 `EnqueueAndPlayNow`
// 会**追加一份**，队列里同一集变成两行。
//
// 🔴 **判据故意分成两个函数**：
//
//	`Identity` / `SameTrack`	队列身份（去重、删除、落盘）—— 一边有一边没有 = 不同
//	`sameStreamTarget`	「用户说的是不是这一段流」—— 不知道的那一边不比 cid
//
// 两者各自都对：前者判同会让「确定的那一集」被「未知」顶掉；后者判不同会让点同一首
// 歌多出一份。所以 `EnqueueAndPlayNow` / `IsPlaying` 用后者，队列内部用前者。
func TestEnqueueAndPlayNowLocatesARowThatDoesNotKnowTheEpisode(t *testing.T) {
	a := New()
	t.Cleanup(a.Close)

	queued := Item{Source: "online", Type: "mv", Bvid: "BV1", Cid: "777", Title: "队列里那一集"}
	a.Queue().Reset(queued)
	if _, ok := a.Queue().Move(1); !ok {
		t.Fatal("队列没有当前项")
	}

	// 列表那一行：只有 bvid（搜结果的形状），标题和队列里那一份一样。
	row := Item{Source: "online", Type: "mv", Bvid: "BV1", Title: "列表上那一行"}
	// 起播会失败（没挂取流），但**队列形状**是这一条要断的。
	_ = a.EnqueueAndPlayNow(row)

	items := a.Queue().Items()
	if len(items) != 1 {
		t.Fatalf("队列里有 %d 项，want 1（点同一首不该多出一份）：%+v", len(items), items)
	}
	if items[0].Cid != "777" {
		t.Errorf("队列那一项的 cid 变成了 %q，want 777（放的是队列里那份完整数据）", items[0].Cid)
	}
}
