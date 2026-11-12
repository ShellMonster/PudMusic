// 网易云的逐字歌词优先：开一首歌时顺手问一次网易云，找到带 YRC 的那一份就
// 拿它换掉 B 站字幕（或者补上本来没有的那一份）。
//
// # 为什么单独一条路
//
// B 站字幕只有整句时间，界面按整句高亮；逐字（YRC）只有网易云给得了。自动找歌词
// 那一路（lyrics_auto.go）是「B 站没字幕时」的兜底，找到什么装什么，不挑逐字。
// 这一条和它并行，只挑带逐字的候选，装上了就按「搜索来的」记。
//
// # 它和自动找歌词互不消费
//
// 这里有自己的去重表（`preferYRCTried`），不碰 `autoLyricTried`：这一条没找到
// 逐字，不代表那一条该放弃——B 站没有字幕时，那份整句歌词仍然要装。
//
// # 什么都找不到就什么都不改
//
// 没有够像的候选、候选拉不到、拉回来没有 YRC——三种都直接走人，当前那份歌词
// （B 站字幕、缓存里的词）原样留着。
package app

import (
	"context"
	"log"
	"time"

	"github.com/ShellMonster/PudMusic/internal/service/lyrics"
)

// preferNeteaseYRC 在后台按歌名去网易云找一份带逐字的歌词。
//
// key 是这一首的缓存键，title 是**原始的**歌名（搜索词和排序都先过
// `cleanLyricTitle`，见 lyrics_auto.go）。同一把键每运行只问一次——搜索本身
// 失败（网络那一层）的那一次不算问过。
func (a *App) preferNeteaseYRC(key, title string) {
	query := cleanLyricTitle(title)
	if key == "" || query == "" {
		return
	}

	a.mu.Lock()
	if a.closed || a.noAutoLyrics || a.preferYRCTried[key] {
		a.mu.Unlock()
		return
	}
	src := neteaseLyricSource(a.lyricSearch.sources)
	if src == nil {
		a.mu.Unlock()
		return
	}
	if a.preferYRCTried == nil {
		a.preferYRCTried = map[string]bool{}
	}
	a.preferYRCTried[key] = true
	gen := a.lyricGen
	durMS := int(a.duration / time.Millisecond)
	a.mu.Unlock()

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
		defer cancel()
		cands, err := src.Search(ctx, query)
		if err != nil {
			log.Printf("pudmusic: 网易云逐字歌词（%s）搜不到：%v", query, err)
			// 网络失败不该把这一首永远钉成「问过了」：下一次重报同一首
			// 还该再试。真正的「问过但没有逐字」才留在表里。
			a.mu.Lock()
			delete(a.preferYRCTried, key)
			a.mu.Unlock()
			return
		}
		ranked := rankAutoCandidates(title, durMS, cands)
		for _, c := range ranked[:min(len(ranked), autoMaxTries)] {
			doc, err := src.Preview(ctx, c)
			if err != nil {
				continue
			}
			if len(lyrics.ParseYRC(doc.YRC)) == 0 {
				continue
			}
			a.installNeteaseYRC(key, gen, title, doc)
			return
		}
	}()
}

// neteaseLyricSource 从挂上的源里找出网易云那一个。**必须拿着 a.mu 调**。
//
// 逐字只有网易云给得了，所以这一条路不去问 LRCLib。
func neteaseLyricSource(srcs []lyrics.Source) lyrics.Source {
	for _, s := range srcs {
		if s.ID() == lyrics.SourceNetease {
			return s
		}
	}
	return nil
}

// installNeteaseYRC 把找到的逐字那一份装上并落盘。
//
// 🔴 **只允许顶掉自动来的那两份**（`OriginBili` / `OriginAuto`）：用户自己挑的
// （`OriginSearch`）和来路不明的老记录都不动。B 站那份先到也照样换——这正是
// 这一条路的目的；兜底自动搜先装上的整句歌词也照样换，否则「优先逐字」会输给
// 一场它本来就该赢的竞速。反过来，这一份先到的话 `installBiliLyrics` 的守卫会
// 挡住 B 站那份。
func (a *App) installNeteaseYRC(key string, gen int, title string, doc lyrics.Doc) {
	// 用户挑的那份可能还在后台读（`loadLyrics` 比这一趟网络先发，但不保证先
	// 落）。它一回来就会把词装上去；这一份要是抢在前面，读盘那趟会因为代次
	// 变了被丢掉——界面上就变成「用户挑的词被搜索结果顶掉」。所以先直接看
	// 一眼盘上那份的来路（读盘在后台，不挡界面）。
	a.mu.Lock()
	cache := a.lyricCache
	artist := a.lyricArtist
	a.mu.Unlock()
	if cache != nil {
		if e, ok := cache.Load(key, title, artist); ok &&
			(e.Lyrics != "" || e.YRC != "") &&
			e.Origin != lyrics.OriginBili && e.Origin != lyrics.OriginAuto {
			return
		}
	}

	a.mu.Lock()
	if a.lyricKey != key || a.closed {
		a.mu.Unlock()
		return
	}
	// 代次变了 = 这一首的歌词已经被别的东西换过。B 站那份和兜底搜来的那份
	// 都值得被逐字顶掉；用户的词、以及换歌留下的新代次都不碰。
	if a.lyricGen != gen && a.lyricOrigin != lyrics.OriginBili && a.lyricOrigin != lyrics.OriginAuto {
		a.mu.Unlock()
		return
	}
	// 同一把键上换过曲名的话，那份词就是照着旧名字搜的，不能再装。
	if a.lyricTitle != title {
		a.mu.Unlock()
		return
	}
	hasLyrics := a.lyricEntry.Lyrics != "" || a.lyricEntry.YRC != ""
	if hasLyrics && a.lyricOrigin != lyrics.OriginBili && a.lyricOrigin != lyrics.OriginAuto {
		a.mu.Unlock()
		return
	}
	entry, c := a.putLyricsLocked(key, doc.LRC, doc.Translated, doc.YRC, lyrics.OriginAuto)
	wake := a.wake
	a.mu.Unlock()
	a.saveLyrics(c, key, entry, wake)
}
