package ui

import (
	"testing"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/ShellMonster/Biu_Go/internal/service/bili"
)

// 「每一个入口造出来的队列项都要带上 UP 主」（A23 的反面）。
//
// # 为什么这一条要单拿一张表
//
// 播放条上那一行 UP 名（显示 + 点它进 `/user/{id}`）认的是队列项上的
// `Artist` / `ArtistMid`。而队列项是**每个页面各造各的**——漏一个的症状是：
//
//	从那一页放的歌，播放条上那一行**整个不画**，只剩收藏和下载两个键
//
// 而**界面上看不出坏了**：歌在放、封面在、歌名在，只是没有「这是谁的歌」。
// 用户看到的是「这一首没有 UP 主」。
//
// 这一票补三个入口（搜索结果 / 动态卡片 / UP 空间投稿）时漏的就是这个，
// 所以这里把**全部入口**摆成一张表：少一个将来就红。
//
// 🔴 **不是「有没有名字」，是「名字和编号都在」**：只有名字的话那句话显示
// 出来却点不动，是另外一种半个功能（见 `app.Item.ArtistMid`）。

// TestEveryEntryCarriesTheUPOwner 是上面那张表。
//
// 每一格都是「页面造队列项的那一行」，判据统一：`Artist` 和 `ArtistMid`
// 都要有值。
func TestEveryEntryCarriesTheUPOwner(t *testing.T) {
	for _, c := range []struct {
		from string
		item app.Item
		want string // 期望的 UP 名
		mid  int64  // 期望的编号
	}{
		{
			from: "搜索结果",
			item: searchVideoMusic(bili.VideoHit{
				Bvid: "BV1search", Title: "搜出来的一首", Owner: "搜索的 UP", Mid: 111,
			}).Item,
			want: "搜索的 UP", mid: 111,
		},
		{
			from: "动态卡片",
			item: dynVideoItem(&bili.DynamicArchive{
				Bvid: "BV1dyn", Title: "动态里的投稿",
			}, bili.DynamicAuthor{Name: "动态的 UP", Mid: 222}),
			want: "动态的 UP", mid: 222,
		},
		{
			from: "UP 空间投稿",
			item: spaceVideoItem(bili.UpVideo{
				Bvid: "BV1space", Cid: "9", Title: "空间里的投稿", Author: "空间的 UP",
			}, 333),
			want: "空间的 UP", mid: 333,
		},
	} {
		t.Run(c.from, func(t *testing.T) {
			if c.item.Artist != c.want {
				t.Errorf("%s 那一项的 UP 名是 %q，want %q——"+
					"播放条上那一行整个不画（viewTrackMeta 只在 Artist 非空时画名字）",
					c.from, c.item.Artist, c.want)
			}
			if c.item.ArtistMid != c.mid {
				t.Errorf("%s 那一项的 UP 编号是 %d，want %d——"+
					"名字显示得出来却点不动", c.from, c.item.ArtistMid, c.mid)
			}
		})
	}
}

// TestTheSearchRowStillShowsTheOwner 盯「补进队列项的同时没把行上那一栏弄没」。
//
// `list.Music.Owner` 和 `app.Item.Artist` 是两栏，都要填：行上那栏是列表里
// 显示的，队列项上那栏是播放条上显示的。
func TestTheSearchRowStillShowsTheOwner(t *testing.T) {
	m := searchVideoMusic(bili.VideoHit{Bvid: "BV1search", Title: "一首歌", Owner: "搜索的 UP", Mid: 111})
	if m.Owner != "搜索的 UP" {
		t.Errorf("行上那栏 UP 名是 %q，want %q", m.Owner, "搜索的 UP")
	}
	if m.Item.Artist != m.Owner {
		t.Errorf("队列项上的 UP 名（%q）和行上那一栏（%q）对不上——"+
			"用户在一个地方看见的名字，在另一个地方不见了", m.Item.Artist, m.Owner)
	}
}
