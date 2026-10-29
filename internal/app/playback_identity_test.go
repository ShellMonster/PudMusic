// 「上次听到哪儿」那一格里 mv 的标识带上 cid 之后，恢复要还认得出来（票 #683）。
//
// `Identity()` 会落进设置文件（`PlaybackEntry.ID`），而恢复走的是
// `itemOfPlaybackEntry`——它必须把 `mv:<bvid>|<cid>` 拆回 bvid 和 cid。
// 不拆的话恢复出来的 bvid 是 `BV1|777`：取流拿它当 bvid 发出去，B 站回 -400，
// 而界面上是「上次那一首打不开」。
package app

import "testing"

// TestTheStoredMVIdentitySplitsBackIntoBvidAndCid 钉住 `trackKey` 和它的逆是什么。
func TestTheStoredMVIdentitySplitsBackIntoBvidAndCid(t *testing.T) {
	cases := []struct {
		name     string
		item     Item
		wantBvid string
		wantCid  string
	}{
		{"带 cid", Item{Source: "online", Type: "mv", Bvid: "BV1", Cid: "777"}, "BV1", "777"},
		{"不带 cid", Item{Source: "online", Type: "mv", Bvid: "BV1"}, "BV1", ""},
	}
	for _, c := range cases {
		id := c.item.Identity()
		e := playbackEntryOf(c.item)
		if e.ID != id {
			t.Fatalf("%s：落盘的 ID 是 %q，而 Identity 是 %q（两处拼法不一样）", c.name, e.ID, id)
		}
		back, ok := itemOfPlaybackEntry(e)
		if !ok {
			t.Fatalf("%s：恢复被判成「认不出来」了（ID=%q）", c.name, e.ID)
		}
		if back.Bvid != c.wantBvid || back.Cid != c.wantCid {
			t.Errorf("%s：恢复出来 bvid=%q cid=%q，want %q / %q",
				c.name, back.Bvid, back.Cid, c.wantBvid, c.wantCid)
		}
		// 恢复出来那一项的身份要和当初**一模一样**，否则「上次听到的是这一集」
		// 在恢复之后会指到别的地方去。
		if back.Identity() != id {
			t.Errorf("%s：恢复出来的身份是 %q，want %q", c.name, back.Identity(), id)
		}
	}
}

// TestAnOldStoredEntryWithoutTheSeparatorStillWorks 旧文件里的 `mv:<bvid>` 照样恢复。
//
// 票 #683 之前落盘的标识没有 `|<cid>`，cid 在单独的 `cid` 栏里。升级之后那
// 一份文件还在盘上——**不认它的话用户上次听的那一整列队列会全部消失**。
func TestAnOldStoredEntryWithoutTheSeparatorStillWorks(t *testing.T) {
	it, ok := itemOfPlaybackEntry(PlaybackEntry{
		ID: "mv:BV1old", Cid: "777", Title: "旧文件里的一集",
	})
	if !ok {
		t.Fatal("旧格式的条目被认成「认不出来」了")
	}
	if it.Bvid != "BV1old" || it.Cid != "777" || it.Title != "旧文件里的一集" {
		t.Errorf("旧格式恢复歪了：%+v", it)
	}
}

// TestSplitMVIdentityTakesTheLastSeparator 拆法用**最后一个** `|`。
//
// bvid 里没有 `|`、cid 是十进制数字，所以正常数据上两种拆法一样。用最后一个的
// 理由是「将来 bvid 的字符集放宽」——而这一条把那个选择钉住，免得有人顺手改成
// `strings.Cut`（第一个）。
func TestSplitMVIdentityTakesTheLastSeparator(t *testing.T) {
	if bvid, cid := splitMVIdentity("a|b|777"); bvid != "a|b" || cid != "777" {
		t.Errorf("splitMVIdentity(\"a|b|777\") = %q / %q，want \"a|b\" / \"777\"", bvid, cid)
	}
	if bvid, cid := splitMVIdentity("BV1"); bvid != "BV1" || cid != "" {
		t.Errorf("没有分隔符时 = %q / %q，want \"BV1\" / \"\"", bvid, cid)
	}
}
