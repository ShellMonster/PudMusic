package app

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/ShellMonster/PudMusic/internal/notice"
	"github.com/ShellMonster/PudMusic/internal/service/bili"
)

// SearchTab identifies one of the search page's result kinds.
type SearchTab int

const (
	SearchTabSong SearchTab = iota
	SearchTabVideo
	SearchTabUser
)

// SearchSort is shared by the video/song and user result tabs. The numeric
// order is the order shown by the search page.
type SearchSort int

const (
	SearchSortDefault SearchSort = iota
	SearchSortSecond
	SearchSortThird
	SearchSortFourth
	SearchSortFifth
)

// SearchQuery is the application search request. The UI does not need to know
// how a tab or sort is encoded for the upstream service.
type SearchQuery struct {
	Tab     SearchTab
	Keyword string
	Sort    SearchSort
	Page    int
}

// SearchItem is one stable application search row. Item contains the action
// data used by playback and navigation, while the remaining fields are safe
// for presentation.
type SearchItem struct {
	ID       string
	Title    string
	Cover    string
	Owner    string
	OwnerMid int64
	Duration time.Duration
	View     int64
	PubTime  time.Time
	Item     Item
}

// SearchPage is one application-level page of search results.
type SearchPage struct {
	Items   []SearchItem
	HasMore bool
	Total   int
}

// SearchErrorCategory identifies the stable meaning of a search failure.
type SearchErrorCategory = notice.SearchErrorCategory

const (
	SearchErrorUnknown           = notice.SearchErrorCategoryUnknown
	SearchErrorMissingClient     = notice.SearchErrorCategoryMissingClient
	SearchErrorInvalidTab        = notice.SearchErrorCategoryInvalidTab
	SearchErrorCanceled          = notice.SearchErrorCategoryCanceled
	SearchErrorNetwork           = notice.SearchErrorCategoryNetwork
	SearchErrorNotLoggedIn       = notice.SearchErrorCategoryNotLoggedIn
	SearchErrorRiskControl       = notice.SearchErrorCategoryRiskControl
	SearchErrorTooFrequent       = notice.SearchErrorCategoryTooFrequent
	SearchErrorCSRF              = notice.SearchErrorCategoryCSRF
	SearchErrorBadResponse       = notice.SearchErrorCategoryBadResponse
	SearchErrorTruncatedResponse = notice.SearchErrorCategoryTruncatedResponse
	SearchErrorAPI               = notice.SearchErrorCategoryAPI
)

// SearchError is the application error model for a search request.
//
// Message and Error keep the legacy presentation contract for existing callers.
// New callers should use Category and Retryable, then choose presentation text in
// the UI layer.
type SearchError struct {
	Message   string
	Category  SearchErrorCategory
	Retryable bool
	cause     error
}

func (e *SearchError) Error() string {
	if e == nil {
		return ""
	}
	return e.Message
}

func (e *SearchError) Unwrap() error { return e.cause }

// Search is the application boundary for the search page. It owns the
// service client so UI code only sees SearchQuery, SearchPage and SearchError.
type Search struct {
	client *bili.Client
}

var errSearchMissing = errors.New("search client is not mounted")

// Page loads and maps one search page.
func (s *Search) Page(ctx context.Context, q SearchQuery) (SearchPage, error) {
	if s == nil || s.client == nil {
		return SearchPage{}, newSearchError(notice.SearchErrorCategoryMissingClient, true, errSearchMissing)
	}
	if ctx == nil {
		ctx = context.Background()
	}

	var (
		page SearchPage
		err  error
	)
	switch q.Tab {
	case SearchTabUser:
		var got bili.UserPage
		got, err = s.client.SearchUsers(ctx, bili.UserQuery{
			Keyword: q.Keyword,
			Page:    q.Page,
			Sort:    userSortOf(q.Sort),
		})
		if err == nil {
			page = SearchPage{Items: userItems(got.Hits), HasMore: got.HasMore, Total: got.Total}
		}
	case SearchTabSong, SearchTabVideo:
		var got bili.VideoPage
		got, err = s.client.SearchVideos(ctx, bili.VideoQuery{
			Keyword:   q.Keyword,
			Page:      q.Page,
			MusicOnly: q.Tab == SearchTabSong,
			Order:     videoOrderOf(q.Sort),
		})
		if err == nil {
			page = SearchPage{Items: videoItems(got.Hits), HasMore: got.HasMore, Total: got.Total}
		}
	default:
		return SearchPage{}, newSearchError(notice.SearchErrorCategoryInvalidTab, false, errors.New("unknown search tab"))
	}
	if err != nil {
		return SearchPage{}, searchError(err)
	}
	return page, nil
}

func videoOrderOf(sort SearchSort) bili.VideoOrder {
	switch sort {
	case SearchSortSecond:
		return bili.VideoOrderClick
	case SearchSortThird:
		return bili.VideoOrderPubdate
	case SearchSortFourth:
		return bili.VideoOrderDanmaku
	case SearchSortFifth:
		return bili.VideoOrderStow
	default:
		return bili.VideoOrderTotalRank
	}
}

func userSortOf(sort SearchSort) bili.UserSort {
	switch sort {
	case SearchSortSecond:
		return bili.UserSortFansDesc
	case SearchSortThird:
		return bili.UserSortFansAsc
	case SearchSortFourth:
		return bili.UserSortLevelDesc
	case SearchSortFifth:
		return bili.UserSortLevelAsc
	default:
		return bili.UserSortDefault
	}
}

func videoItems(hits []bili.VideoHit) []SearchItem {
	items := make([]SearchItem, 0, len(hits))
	for _, hit := range hits {
		if hit.Bvid == "" {
			continue
		}
		items = append(items, SearchItem{
			ID: hit.Bvid, Title: hit.Title, Cover: hit.Cover, Owner: hit.Owner,
			OwnerMid: hit.Mid, Duration: hit.Duration, View: hit.Play, PubTime: hit.PubDate,
			Item: Item{
				Source: SourceOnline, Type: "mv", Bvid: hit.Bvid, Cid: hit.Cid,
				Title: hit.Title, Cover: hit.Cover, Artist: hit.Owner, ArtistMid: hit.Mid,
			},
		})
	}
	return items
}

func userItems(hits []bili.UserHit) []SearchItem {
	items := make([]SearchItem, 0, len(hits))
	for _, hit := range hits {
		if hit.Mid == 0 {
			continue
		}
		mid := strconv.FormatInt(hit.Mid, 10)
		items = append(items, SearchItem{
			ID: searchUserIDPrefix + mid, Title: hit.Name, Cover: hit.Avatar,
			Owner: hit.Sign, Item: Item{
				Source: SourceOnline, Type: ItemTypeUser, Bvid: mid,
				Title: hit.Name, Cover: hit.Avatar,
			},
		})
	}
	return items
}

const searchUserIDPrefix = "u:"

func searchError(err error) error {
	if err == nil {
		return nil
	}
	category := searchErrorCategory(err)
	return newSearchError(category, bili.Recoverable(err), err)
}

func newSearchError(category SearchErrorCategory, retryable bool, cause error) *SearchError {
	return &SearchError{
		Message:   notice.SearchErrorText(category),
		Category:  category,
		Retryable: retryable,
		cause:     cause,
	}
}

func searchErrorCategory(err error) SearchErrorCategory {
	var api *bili.APIError
	switch {
	case errors.Is(err, context.Canceled):
		return SearchErrorCanceled
	case errors.Is(err, bili.ErrNetwork):
		return SearchErrorNetwork
	case errors.Is(err, bili.ErrNotLoggedIn):
		return SearchErrorNotLoggedIn
	case errors.Is(err, bili.ErrRiskControl):
		return SearchErrorRiskControl
	case errors.Is(err, bili.ErrTooFrequent):
		return SearchErrorTooFrequent
	case errors.Is(err, bili.ErrCSRF):
		return SearchErrorCSRF
	case errors.Is(err, bili.ErrBadResponse):
		return SearchErrorBadResponse
	case errors.Is(err, bili.ErrTruncatedResponse):
		return SearchErrorTruncatedResponse
	case errors.As(err, &api):
		return SearchErrorAPI
	default:
		return SearchErrorUnknown
	}
}

// MountSearch attaches the application search boundary to this App.
func (a *App) MountSearch(cli *bili.Client) {
	a.mu.Lock()
	a.search = &Search{client: cli}
	a.mu.Unlock()
}

// SearchOf returns this App's search boundary. An unmounted boundary remains
// usable and reports a user-facing SearchError when queried.
func (a *App) SearchOf() *Search {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.search == nil {
		a.search = &Search{}
	}
	return a.search
}
