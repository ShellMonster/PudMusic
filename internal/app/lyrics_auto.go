package app

import (
	"context"
	"log"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/service/lyrics"
)

// 自动找歌词的最后一道：B 站没有字幕、缓存里也没有时，按歌名去已挂上的歌词源
// （网易云、LRCLib）搜，挑一条对得上的装上，用户不用自己开搜索弹窗。
//
// 「对得上」的判据偏保守，宁可不装也不装错一首：歌名（去掉【】《》括号、MV/原创
// 这类修饰之后）要和候选的歌名互相包含；两边都给了时长的话，差要在 10 秒以内。
// 装上去的歌词记成「搜索来的」，用户在搜索弹窗里换一份照常能换。
//
// 每首歌每次运行只自动找一遍（找不到也不重复发请求）。

var (
	autoBrackets = regexp.MustCompile(`【[^】]*】|\[[^\]]*\]|\([^)]*\)|（[^）]*）`)
	autoNoise    = regexp.MustCompile(`(?i)官方\s*mv|\bmv\b|official|lyrics?\s*video|动态歌词|歌词版|歌词|高音质|无损|完整版|原创曲?|\bpv\b|\b4k\b|hi-?res`)
	autoPunct    = regexp.MustCompile(`[\s\p{P}\p{S}]+`)
)

// autoDurationSlackMS 是自动匹配时两边时长最多差多少。
const autoDurationSlackMS = 10_000

func cleanLyricTitle(title string) string {
	s := autoBrackets.ReplaceAllString(title, " ")
	s = strings.NewReplacer("《", " ", "》", " ", "「", " ", "」", " ").Replace(s)
	s = autoNoise.ReplaceAllString(s, " ")
	return strings.TrimSpace(strings.Join(strings.Fields(s), " "))
}

func normLyricTitle(s string) string {
	return autoPunct.ReplaceAllString(strings.ToLower(s), "")
}

// pickAutoCandidate 挑最像的那一条，没有够像的返回 false。
func pickAutoCandidate(title string, durMS int, cands []lyrics.Candidate) (lyrics.Candidate, bool) {
	want := normLyricTitle(cleanLyricTitle(title))
	if want == "" {
		return lyrics.Candidate{}, false
	}
	best, bestScore := lyrics.Candidate{}, -1
	for _, c := range cands {
		got := normLyricTitle(c.Title)
		if got == "" || !(strings.Contains(got, want) || strings.Contains(want, got)) {
			continue
		}
		score := 1
		if got == want {
			score += 100
		}
		if durMS > 0 && c.DurationMS > 0 {
			diff := durMS - c.DurationMS
			if diff < 0 {
				diff = -diff
			}
			if diff > autoDurationSlackMS {
				continue
			}
			score += (autoDurationSlackMS - diff) / 100
		}
		if score > bestScore {
			best, bestScore = c, score
		}
	}
	return best, bestScore >= 0
}

// autoSearchLyrics 在后台自动找一份歌词装上。key 是这一首的缓存键，title 是歌名。
func (a *App) autoSearchLyrics(key, title string) {
	query := cleanLyricTitle(title)
	if key == "" || query == "" {
		return
	}
	a.mu.Lock()
	srcs := a.lyricSearch.sources
	if len(srcs) == 0 || a.autoLyricTried[key] || a.closed {
		a.mu.Unlock()
		return
	}
	if a.autoLyricTried == nil {
		a.autoLyricTried = map[string]bool{}
	}
	a.autoLyricTried[key] = true
	durMS := int(a.duration / time.Millisecond)
	a.mu.Unlock()

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
		defer cancel()
		for _, src := range srcs {
			cands, err := src.Search(ctx, query)
			if err != nil {
				log.Printf("biu: 自动找歌词（%s）搜不到：%v", src.Name(), err)
				continue
			}
			c, ok := pickAutoCandidate(title, durMS, cands)
			if !ok {
				continue
			}
			doc := lyrics.Doc{LRC: c.LRC, Translated: c.Translated}
			if !c.HasLyrics() {
				if doc, err = src.Preview(ctx, c); err != nil {
					continue
				}
			}
			if len(lyrics.Rows(doc.LRC, "")) == 0 {
				continue
			}
			a.installAutoLyrics(key, doc)
			return
		}
	}()
}

func (a *App) installAutoLyrics(key string, doc lyrics.Doc) {
	a.mu.Lock()
	if a.lyricKey != key || a.closed || a.lyricEntry.Lyrics != "" {
		a.mu.Unlock()
		return
	}
	entry, c := a.putLyricsLocked(key, doc.LRC, doc.Translated, lyrics.OriginSearch)
	wake := a.wake
	a.mu.Unlock()
	a.saveLyrics(c, key, entry, wake)
}

// localLyricTitle 从本地歌词缓存键里还原歌名（文件名去掉扩展名）。
func localLyricTitle(key string) (string, bool) {
	p, ok := strings.CutPrefix(key, "local-")
	if !ok {
		return "", false
	}
	return strings.TrimSuffix(filepath.Base(p), filepath.Ext(p)), true
}
