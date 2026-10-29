package ui

import (
	"strconv"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/service/bili"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/list"
)

func searchVideoMusic(hit bili.VideoHit) list.Music {
	return searchItemsOf([]app.SearchItem{{
		ID: hit.Bvid, Title: hit.Title, Cover: hit.Cover, Owner: hit.Owner,
		OwnerMid: hit.Mid, Duration: hit.Duration, View: hit.Play, PubTime: hit.PubDate,
		Item: app.Item{Source: app.SourceOnline, Type: "mv", Bvid: hit.Bvid, Cid: hit.Cid,
			Title: hit.Title, Cover: hit.Cover, Artist: hit.Owner, ArtistMid: hit.Mid},
	}})[0]
}

func searchVideoMusics(hits []bili.VideoHit) []list.Music {
	out := make([]list.Music, 0, len(hits))
	for _, hit := range hits {
		if hit.Bvid != "" {
			out = append(out, searchVideoMusic(hit))
		}
	}
	return out
}

func searchUserMusic(hit bili.UserHit) list.Music {
	mid := formatSearchMid(hit.Mid)
	return searchItemsOf([]app.SearchItem{{
		ID: searchUserIDPrefix + mid, Title: hit.Name, Cover: hit.Avatar, Owner: hit.Sign,
		Item: app.Item{Source: app.SourceOnline, Type: app.ItemTypeUser, Bvid: mid, Title: hit.Name, Cover: hit.Avatar},
	}})[0]
}

func searchUserMusics(hits []bili.UserHit) []list.Music {
	out := make([]list.Music, 0, len(hits))
	for _, hit := range hits {
		if hit.Mid != 0 {
			out = append(out, searchUserMusic(hit))
		}
	}
	return out
}

func formatSearchMid(mid int64) string { return strconv.FormatInt(mid, 10) }

func searchVideoQueryOf(tab int, q searchQuerySpec) bili.VideoQuery {
	return bili.VideoQuery{Keyword: q.keyword, MusicOnly: tab == searchTabSong, Order: []bili.VideoOrder{
		bili.VideoOrderTotalRank, bili.VideoOrderClick, bili.VideoOrderPubdate, bili.VideoOrderDanmaku, bili.VideoOrderStow,
	}[sortOf(tab, q.sort)]}
}

func searchUserQueryOf(q searchQuerySpec) bili.UserQuery {
	sorts := []bili.UserSort{bili.UserSortDefault, bili.UserSortFansDesc, bili.UserSortFansAsc, bili.UserSortLevelDesc, bili.UserSortLevelAsc}
	return bili.UserQuery{Keyword: q.keyword, Sort: sorts[sortOf(searchTabUser, q.sort)]}
}

func searchOnRealClient(s *Shell, c *bili.Client) {
	s.app.MountSearch(c)
}
