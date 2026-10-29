package app

import (
	"context"
	"time"

	"github.com/ShellMonster/PudMusic/internal/service/bili"
)

// SpaceQuery identifies one UP space.
type SpaceQuery struct {
	Mid int64
}

// FollowQuery identifies a page of the current account's follows.
type FollowQuery struct {
	SelfMid int64
	Page    int
	Keyword string
	TagID   *int
}

// DynamicQuery identifies one page of a dynamic feed or UP-space dynamic tab.
type DynamicQuery struct {
	Mid    int64
	Offset string
}

// FollowAction is an application-level follow/unfollow command.
type FollowAction struct {
	Mid    int64
	Follow bool
}

// DynamicLikeAction is an application-level dynamic like command.
type DynamicLikeAction struct {
	ID   string
	Like bool
}

// SpaceUser is the presentation-safe UP-space identity.
type SpaceUser struct {
	Mid      int64
	Name     string
	Sign     string
	Face     string
	Banner   string
	Level    int
	Official string
}

// SpaceRelation describes the current account's relation to a UP.
type SpaceRelation int

const (
	RelationNone     SpaceRelation = SpaceRelation(bili.RelationNone)
	RelationFollowed SpaceRelation = SpaceRelation(bili.RelationFollowed)
	RelationMutual   SpaceRelation = SpaceRelation(bili.RelationMutual)
	RelationBlocked  SpaceRelation = SpaceRelation(bili.RelationBlocked)
)

func (r SpaceRelation) Followed() bool { return r == RelationFollowed || r == RelationMutual }
func (r SpaceRelation) Blocked() bool  { return r == RelationBlocked }

// RelationStats are public follow and follower counts.
type RelationStats struct {
	Following int64
	Follower  int64
}

// SpaceHeaderSnapshot is the complete UP-space header snapshot consumed by UI.
type SpaceHeaderSnapshot struct {
	Loaded   bool
	Loading  bool
	User     SpaceUser
	Relation SpaceRelation
	Stat     RelationStats
	Err      string

	statKnown     bool
	statErr       string
	relationFor   int64
	relationKnown bool
	relationErr   string
}

// FollowState is the application decision for the follow control.
type FollowState int

const (
	FollowHidden FollowState = iota
	FollowAskable
	FollowDone
	FollowFailed
)

func (h SpaceHeaderSnapshot) Follow(selfMid int64, loggedIn bool) FollowState {
	if !loggedIn || h.Loading && !h.Loaded {
		return FollowHidden
	}
	if !h.relationKnown {
		if h.relationErr != "" {
			return FollowFailed
		}
		return FollowHidden
	}
	if h.relationFor != selfMid {
		return FollowHidden
	}
	if h.Relation.Followed() {
		return FollowDone
	}
	return FollowAskable
}

func (h SpaceHeaderSnapshot) StatOK() bool         { return h.statKnown }
func (h SpaceHeaderSnapshot) StatFailed() bool     { return h.statErr != "" }
func (h SpaceHeaderSnapshot) RelationKnown() bool  { return h.relationKnown }
func (h SpaceHeaderSnapshot) RelationFailed() bool { return h.relationErr != "" }
func (h SpaceHeaderSnapshot) RelationErr() string  { return h.relationErr }

func (h SpaceHeaderSnapshot) NeedStat(selfMid int64) bool {
	if selfMid == 0 || !h.Loaded || h.Loading || h.statErr != "" {
		return false
	}
	return !h.StatOK()
}

func (h SpaceHeaderSnapshot) NeedRelation(selfMid int64) bool {
	if selfMid == 0 || !h.Loaded || h.Loading || h.relationErr != "" {
		return false
	}
	return !(h.relationKnown && h.relationFor == selfMid)
}

func (h SpaceHeaderSnapshot) AnyPartFailed() bool { return h.relationErr != "" || h.statErr != "" }

// SpaceVideo is one UP-space投稿 row.
type SpaceVideo struct {
	Bvid        string
	Title       string
	Cover       string
	Duration    string
	Play        int64
	Description string
	Author      string
	Cid         string
	Created     time.Time
}

type SpaceVideoQuery struct {
	Mid     int64
	Page    int
	Size    int
	Keyword string
	Order   string
}

type SpaceVideoPage struct {
	Videos  []SpaceVideo
	Total   int
	HasMore bool
}

// SpaceCollection is a 收藏夹/合集 row.
type SpaceCollection struct {
	ID    int64
	Title string
	Cover string
	Count int
	Type  int
}

type SpaceCollectionPage struct {
	Items   []SpaceCollection
	Count   int
	HasMore bool
}

// DynamicKind identifies the supported dynamic shapes.
type DynamicKind uint8

const (
	DynamicText DynamicKind = iota
	DynamicOpus
	DynamicVideo
)

type DynamicAuthor struct {
	Mid  int64
	Name string
	Face string
}

type DynamicVideoItem struct {
	Bvid     string
	Title    string
	Cover    string
	Duration string
	Play     string
	Desc     string
}

type Dynamic struct {
	ID        string
	Kind      DynamicKind
	Author    DynamicAuthor
	Time      string
	Text      string
	Pics      []string
	Video     *DynamicVideoItem
	Liked     bool
	LikeCount int
}

type DropReason uint8

const (
	DropShapeUnknown DropReason = iota
	DropUnreadable
	DropNoIdentity
	DropEmpty
	DropNoArchive
)

type DroppedDynamic struct {
	Reason DropReason
	Key    string
	Why    string
}

type DropCounts struct {
	ShapeUnknown int
	Unreadable   int
	NoIdentity   int
	Empty        int
	NoArchive    int
	Other        int
}

func (c *DropCounts) Add(r DropReason) {
	switch r {
	case DropShapeUnknown:
		c.ShapeUnknown++
	case DropUnreadable:
		c.Unreadable++
	case DropNoIdentity:
		c.NoIdentity++
	case DropEmpty:
		c.Empty++
	case DropNoArchive:
		c.NoArchive++
	default:
		c.Other++
	}
}
func (c DropCounts) Total() int {
	return c.ShapeUnknown + c.Unreadable + c.NoIdentity + c.Empty + c.NoArchive + c.Other
}

type DynamicPage struct {
	Items    []Dynamic
	Offset   string
	HasMore  bool
	Raw      int
	Drops    []DroppedDynamic
	DropWhy  []string
	Baseline string
}

type FollowTag struct {
	ID    int
	Name  string
	Count int
	Tip   string
}

type FollowUser struct {
	Mid      int64
	Name     string
	Face     string
	Sign     string
	Official string
	Special  bool
}

type FollowUsersPage struct {
	Users []FollowUser
	Total int
}

type FollowAuthor struct {
	Mid  int64
	Name string
	Face string
}

type FollowAuthorsPage struct {
	Authors []FollowAuthor
	Total   int
	HasMore bool
}

func mapSpaceUser(v *bili.User) SpaceUser {
	if v == nil {
		return SpaceUser{}
	}
	return SpaceUser{Mid: v.Mid, Name: v.Name, Sign: v.Sign, Face: v.Face, Banner: v.Banner, Level: v.Level, Official: v.Official}
}
func mapRelation(v bili.Relation) SpaceRelation { return SpaceRelation(v) }
func mapStats(v *bili.RelationStat) RelationStats {
	if v == nil {
		return RelationStats{}
	}
	return RelationStats{Following: v.Following, Follower: v.Follower}
}
func mapSpaceVideo(v bili.UpVideo) SpaceVideo {
	return SpaceVideo{Bvid: v.Bvid, Title: v.Title, Cover: v.Cover, Duration: v.Duration, Play: v.Play, Description: v.Description, Author: v.Author, Cid: v.Cid, Created: v.Created}
}
func mapCollection(v bili.UpCollection) SpaceCollection {
	return SpaceCollection{ID: v.ID, Title: v.Title, Cover: v.Cover, Count: v.Count, Type: v.Type}
}
func mapDynamic(v bili.Dynamic) Dynamic {
	d := Dynamic{ID: v.ID, Kind: DynamicKind(v.Kind), Author: DynamicAuthor{Mid: v.Author.Mid, Name: v.Author.Name, Face: v.Author.Face}, Time: v.Time, Text: v.Text, Pics: append([]string(nil), v.Pics...), Liked: v.Liked, LikeCount: v.LikeCount}
	if v.Video != nil {
		d.Video = &DynamicVideoItem{Bvid: v.Video.Bvid, Title: v.Video.Title, Cover: v.Video.Cover, Duration: v.Video.Duration, Play: v.Video.Play, Desc: v.Video.Desc}
	}
	return d
}
func mapDynamicPage(v bili.DynamicPage) DynamicPage {
	p := DynamicPage{Offset: v.Offset, HasMore: v.HasMore, Raw: v.Raw, DropWhy: append([]string(nil), v.DropWhy...), Baseline: v.Baseline}
	p.Items = make([]Dynamic, 0, len(v.Items))
	for _, item := range v.Items {
		p.Items = append(p.Items, mapDynamic(item))
	}
	p.Drops = make([]DroppedDynamic, 0, len(v.Drops))
	for _, drop := range v.Drops {
		p.Drops = append(p.Drops, DroppedDynamic{Reason: DropReason(drop.Reason), Key: drop.Key, Why: drop.Why})
	}
	return p
}
func mapFollowTag(v bili.FollowTag) FollowTag {
	return FollowTag{ID: v.ID, Name: v.Name, Count: v.Count, Tip: v.Tip}
}
func mapFollowUser(v bili.FollowUser) FollowUser {
	return FollowUser{Mid: v.Mid, Name: v.Name, Face: v.Face, Sign: v.Sign, Official: v.Official, Special: v.Special}
}

func (a *App) SpaceVideoPage(ctx context.Context, q SpaceVideoQuery) (SpaceVideoPage, error) {
	pg, err := a.UpVideos(ctx, q)
	return pg, err
}
