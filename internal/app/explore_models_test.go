package app

import (
	"context"
	"net/http"
	"testing"
)

func TestExploreQueriesExposeApplicationModels(t *testing.T) {
	b := newExploreBili(t)
	b.on("/x/space/wbi/arc/search", func(w http.ResponseWriter, r *http.Request) {
		writeExploreJSON(w, 0, map[string]any{"list": map[string]any{
			"vlist": []map[string]any{{"bvid": "BV1", "title": "投稿", "pic": "//img/cover", "length": "1:02", "play": 3, "created": 1}},
		}, "page": map[string]any{"count": 1}})
	})
	b.on("/x/polymer/web-dynamic/v1/feed/all", func(w http.ResponseWriter, r *http.Request) {
		writeExploreJSON(w, 0, map[string]any{"items": []any{}, "offset": "next", "has_more": false})
	})
	b.on("/x/relation/tags", func(w http.ResponseWriter, r *http.Request) {
		writeExploreJSON(w, 0, []map[string]any{{"tagid": 2, "name": "常看", "count": 1}})
	})
	b.on("/x/relation/followings", func(w http.ResponseWriter, r *http.Request) {
		writeExploreJSON(w, 0, map[string]any{"total": 1, "list": []map[string]any{{"mid": 7, "uname": "关注的人", "face": "//img/face"}}})
	})
	a := newExploreApp(t, b)
	ctx := context.Background()

	video, err := a.UpVideos(ctx, SpaceVideoQuery{Mid: 42, Page: 1})
	if err != nil || len(video.Videos) != 1 || video.Videos[0].Bvid != "BV1" {
		t.Fatalf("app 投稿模型不对：%+v, %v", video, err)
	}
	if _, ok := any(video).(SpaceVideoPage); !ok {
		t.Fatal("投稿查询没有返回 app.SpaceVideoPage")
	}

	dyn, err := a.DynamicFeed(ctx, "")
	if err != nil || dyn.Offset != "next" {
		t.Fatalf("app 动态模型不对：%+v, %v", dyn, err)
	}
	tags, err := a.FollowTags(ctx)
	if err != nil || len(tags) != 1 || tags[0].ID != 2 {
		t.Fatalf("app 关注分组模型不对：%+v, %v", tags, err)
	}
	users, err := a.FollowList(ctx, 77, 1)
	if err != nil || len(users.Users) != 1 || users.Users[0].Mid != 7 {
		t.Fatalf("app 关注用户模型不对：%+v, %v", users, err)
	}
	authors, err := a.FollowedAuthors(ctx, 77, 1)
	if err != nil || len(authors.Authors) != 1 || authors.Authors[0].Mid != 7 {
		t.Fatalf("app 动态作者模型不对：%+v, %v", authors, err)
	}
}
