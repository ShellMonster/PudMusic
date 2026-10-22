package app

// 「合集/剧集那一类」与「没挂稿件的推荐条目」：**必须说成两句话**。
//
// # 为什么这两类要分开
//
// 它们在 B 站那边是两件事，而用户照着做的下一步也不一样：
//
//	合集 / 电影 / 番剧	这一条**本来就是合集**，里面才有单集
//	                    → 下一步是「打开里面的单集」
//	没挂稿件的推荐条目	B 站没给它挂可播的稿件
//	                    → 下一步是「搜同名的歌」
//
// 混成一句「这一条不是投稿」的话，两个界面上（收藏夹的合集、首页的推荐条目）
// 说的还是同一句，而**其中一句给的「怎么办」对那一类根本不贴切**。
//
// 这一组是 app 包内的（不是 `app_test`）：它要比的是**两处内部写法**
// （`pgcCannotPlayReason` 与 `UnplayableReason`），而那两处得是同一句。

import (
	"strings"
	"testing"

	"github.com/ShellMonster/Biu_Go/internal/service/bili"
)

// TestTheCollectionKindAndTheArchiveKindSayDifferentThings 两类不许说同一句。
func TestTheCollectionKindAndTheArchiveKindSayDifferentThings(t *testing.T) {
	collection := ItemOfMedia(bili.Media{ID: 7, Type: bili.MediaCollection, Title: "一个合集"})
	if collection.Type != "collection" {
		t.Fatalf("合集翻成 Item 之后的 Type 是 %q，want \"collection\"——"+
			"空 Type 在判据里和「没挂稿件的推荐条目」是同一种", collection.Type)
	}
	movie := ItemOfMedia(bili.Media{ID: 8, Type: bili.MediaMovie, Title: "一部电影"})
	if movie.Type != "collection" {
		t.Fatalf("电影翻成 Item 之后的 Type 是 %q，want \"collection\"", movie.Type)
	}

	collectionNote := UnplayableMenuNote(collection)
	if !strings.Contains(collectionNote, "合集/剧集") {
		t.Errorf("合集那一类的说明是 %q，没说清是哪一种条目", collectionNote)
	}
	if !strings.Contains(collectionNote, "打开里面的单集") {
		t.Errorf("合集那一类的说明是 %q，「还能怎么办」对合集不贴切", collectionNote)
	}

	// 没挂稿件的推荐条目：那是**另一句**。
	archiveNote := UnplayableMenuNote(Item{Source: "online", Type: "mv"})
	if !strings.Contains(archiveNote, "不是投稿") {
		t.Errorf("没挂稿件那一类的说明是 %q，没说清是哪一种条目", archiveNote)
	}
	if archiveNote == collectionNote {
		t.Errorf("两类说的是同一句：%q", archiveNote)
	}
}

// TestTheCollectionKindAndThePGCOneSayTheSameThing 同一个类不许在两个界面
// 说两句不同的话。
//
// 收藏夹里的合集/电影和稍后再看里的 PGC，在 B 站那边是同一件事（不是投稿、
// 没有单独的音频流），用户照着做的下一步也一样（打开里面的单集）。
func TestTheCollectionKindAndThePGCOneSayTheSameThing(t *testing.T) {
	collection := ItemOfMedia(bili.Media{ID: 7, Type: bili.MediaCollection, Title: "一个合集"})
	fromCollection := UnplayableReason(collection)
	fromPGC := pgcCannotPlayReason(false)
	if fromCollection != fromPGC {
		t.Errorf("同一个类说了两句：合集 %q / PGC %q", fromCollection, fromPGC)
	}
	if fromPGC == "" {
		t.Error("放得了的也给了理由")
	}
}
