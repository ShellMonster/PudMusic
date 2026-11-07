//go:build e2e

package main

// 喜欢歌单绑定那一票的真窗口 fixture：一台**绑定已就位**的壳，正在放一首
// 可赞的 B 站 mv。
//
// # 为什么单独有这一台
//
// 这一票在真窗口上要断的是**无障碍树**里的事（`internal/e2e` 的那一族）：
//
//   - 播放条 / 满屏上「赞」和「收藏」那两枚键**不在树里**（连同它们的角标
//     数字）——拿掉的是社交键；
//   - 「喜欢」入口**在**——留下的那个键。
//
// 「键在不在无障碍树里」这件事只有真窗口验得了：框架只在**辅助技术问起**
// 时才描述那一帧（`mygo/ui/access.go` 的 `accessibilityOn`），无头窗口里
// 没人问，那一帧压根不描述。所以界面层那一族（`likedplaylist_test.go`）断
// 的是同一棵树的**布局事实**，而这里断的是**读屏软件读得到的那一份**。
//
// ⚠️ **它跑的是真的那一枚壳**（`ui.New` + `s.View`），不是手搓的一排键——
// 要验的就是「真播放条 / 真满屏上那几枚键在不在」，而不是「测试里摆的对不对」。
//
// # 数据从哪儿来
//
// 内置一台假 B 站（`httptest`），和 `internal/ui/searchbili_test.go` 的
// `biliFixture` 是同一份形状，加上喜欢歌单要的那几路：
//
//   - `archive/relation` 回「没赞没藏」——播放条上的「喜欢」要这个答案才亮；
//   - `created/list` 回两格「我创建的收藏夹」——绑定那一格在里面；
//   - `folder/add` / `resource/deal` 应答写接口——「绑定并加入」走的那一趟。
//
// **绑定是启动前种下的**：fixture 往 `BIU_E2E_DATA_DIR` 写一份
// `settings.json`，`likedPlaylist` 指着 `created/list` 里真有的一格
// （id 100）——这是「已绑定」那一档，喜欢不再问。

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo"
	_ "github.com/egoist/mygo/plugins/automation"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/service/bili"
	ui2 "github.com/ShellMonster/PudMusic/internal/ui"
)

// boundFolderID 是「已绑定」那一格（`created/list` 里真有的一格）。
const boundFolderID = 100

func main() {
	srv := httptest.NewServer(http.HandlerFunc(serveFixture))
	defer srv.Close()

	// 先把「已绑定」那份设置种进数据目录（`MountSettings` 读的是启动那一刻
	// 的目录——进程起来之后再写就来不及了）。
	if dir := os.Getenv("BIU_E2E_DATA_DIR"); dir != "" {
		mygo.App.SetPath(mygo.PathUserData, dir)
		seedLikedSettings(dir)
	}

	a := app.New()
	cli := bili.NewClient(bili.Options{Transport: srv.Client().Transport})
	cli.TestOnlyPointHostsAtFixture(srv.URL)
	a.MountLogin(app.NewMemoryStore("liked-fixture"), cli)
	tmp, err := os.MkdirTemp("", "liked-fixture-")
	if err != nil {
		log.Fatalf("造临时目录：%v", err)
	}
	defer os.RemoveAll(tmp)
	a.MountStreaming(app.Streaming{
		Client: cli, Media: srv.Client(), TempDir: tmp,
	})
	a.MountCollections(app.CollectionRefs{Client: cli})

	s := ui2.New(a)
	if err := a.Enqueue(app.Item{
		Source: "online", Type: "mv", Bvid: "BVlikedE2E", Cid: "777",
		Title: "有绑定喜欢的那一首", Artist: "某个 UP 主",
	}); err != nil {
		log.Fatalf("把在线那首放进队列：%v", err)
	}

	mygo.App.WhenReady(func() {
		mygo.NewWindow(ui2.WindowOptions(s.View))
	})
	if err := mygo.App.Run(); err != nil {
		log.Println(err)
		os.Exit(1)
	}
}

// seedLikedSettings 往数据目录写一份「已绑定喜欢歌单」的 settings.json。
//
// ⚠️ **信封是设置文件的真形状**（`schemaVersion` + `appSettings`），只多写
// `likedPlaylist` 那一格——其余键让 `DefaultSettings` 补上。
func seedLikedSettings(dataDir string) {
	settings := map[string]any{
		"schemaVersion": 1,
		"appSettings": map[string]any{
			"isMuted": true,
			"likedPlaylist": map[string]any{
				"folderId": boundFolderID,
				"title":    "绑定的那一格",
			},
		},
	}
	blob, err := json.Marshal(settings)
	if err != nil {
		log.Fatalf("序列化绑定设置：%v", err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, "settings.json"), blob, 0o600); err != nil {
		log.Fatalf("写绑定设置：%v", err)
	}
}

// serveFixture 是那一台假 B 站的全部回包（`internal/ui/searchbili_test.go`
// 的 `biliFixture` 里「播放条 + 喜欢歌单要的那几路」的子集）。
func serveFixture(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.URL.Path == "/x/web-interface/nav":
		writeJSON(w, map[string]any{"code": 0, "message": "0", "ttl": 1,
			"data": map[string]any{"isLogin": true, "mid": 9527, "uname": "测试账号",
				"wbi_img": map[string]string{
					"img_url": "https://i0.hdslb.com/bfs/wbi/7cd084941338484aae1ad9425b84077c.png",
					"sub_url": "https://i0.hdslb.com/bfs/wbi/4932caff0ff746eab6f01bf08b70ac45.png",
				}}})

	case r.URL.Path == "/x/web-interface/view":
		// bvid → aid（收藏接口要的是 avid），外加 `stat`——赞 / 收藏 / 评论
		// 那三枚键的角标数（票 #701：同一趟回包）。
		writeJSON(w, map[string]any{"code": 0, "message": "0", "ttl": 1,
			"data": map[string]any{
				"aid": 4242, "bvid": r.URL.Query().Get("bvid"),
				"stat": map[string]any{"reply": 1286, "favorite": 2468, "like": 9999},
			}})

	case r.URL.Path == "/x/web-interface/archive/relation":
		// 「赞没赞、收藏没收藏」那一问——播放条上的「喜欢」要它才亮。
		writeJSON(w, map[string]any{"code": 0, "message": "0", "ttl": 1,
			"data": map[string]any{"like": false, "favorite": false}})

	case r.URL.Path == "/x/v3/fav/folder/created/list":
		// 「我创建的收藏夹」分页（绑定那一格在里面，id 100）。
		writeJSON(w, map[string]any{"code": 0, "message": "0", "ttl": 1,
			"data": map[string]any{
				"count": 2, "has_more": false,
				"list": []any{
					map[string]any{"id": boundFolderID, "title": "绑定的那一格",
						"media_count": 1, "attr": 0, "fav_state": 0},
					map[string]any{"id": 200, "title": "第二格",
						"media_count": 0, "attr": 0, "fav_state": 0},
				}}})

	case r.URL.Path == "/x/v3/fav/folder/created/list-all":
		// 收藏那一趟挑默认格走的另一路（`FavoriteSong` 没指定 FolderID 时）。
		writeJSON(w, map[string]any{"code": 0, "message": "0", "ttl": 1,
			"data": map[string]any{"count": 2, "list": []any{
				map[string]any{"id": boundFolderID, "title": "绑定的那一格", "media_count": 1, "attr": 0},
				map[string]any{"id": 200, "title": "第二格", "media_count": 0, "attr": 0},
			}}})

	case r.URL.Path == "/x/v3/fav/folder/add":
		// 新建收藏夹（「创建并加入」那一趟）。
		_ = r.ParseForm()
		writeJSON(w, map[string]any{"code": 0, "message": "0", "ttl": 1,
			"data": map[string]any{
				"id": 300, "title": r.PostForm.Get("title"), "attr": 0,
				"media_count": 0, "fav_state": 0,
			}})

	case r.URL.Path == "/x/v3/fav/resource/ids":
		// 「这一格里现在有哪些内容」——收藏前的去重那一问（这里回空 = 没在）。
		writeJSON(w, map[string]any{"code": 0, "message": "0", "ttl": 1,
			"data": map[string]any{"ids": []any{}}})

	case r.URL.Path == "/x/v3/fav/resource/deal":
		// 写收藏那一趟。记下 `add_media_ids` 就是「收进了哪一格」。
		_ = r.ParseForm()
		writeJSON(w, map[string]any{"code": 0, "message": "0", "ttl": 1})

	case r.URL.Path == "/x/player/pagelist":
		writeJSON(w, map[string]any{"code": 0, "message": "0", "ttl": 1,
			"data": []any{map[string]any{
				"cid": 777, "page": 1, "part": "第一集", "duration": 260,
				"from": "vupload", "vid": "", "weblink": "",
				"dimension": map[string]any{"width": 1920, "height": 1080, "rotate": 0},
			}}})

	case r.URL.Path == "/x/player/wbi/playurl":
		writeJSON(w, map[string]any{"code": 0, "message": "0", "ttl": 1,
			"data": map[string]any{
				"timelength": 1000,
				"dash": map[string]any{
					"duration": 1,
					"audio": []any{map[string]any{
						"id": 30280, "bandwidth": 110836, "codecs": "mp4a.40.2",
						"mimeType": "audio/mp4",
						"baseUrl":  mediaURL(r),
					}},
				},
			}})

	case strings.HasPrefix(r.URL.Path, "/cdn/"):
		serveMedia(w, r)

	default:
		http.NotFound(w, r)
	}
}

// mediaURL 是 playurl 回给客户端的那条流地址：指向这台服务器的 `/cdn/a.flac`。
func mediaURL(r *http.Request) string {
	return "http://" + r.Host + "/cdn/a.flac?deadline=" +
		strconv.FormatInt(time.Now().Add(2*time.Hour).Unix(), 10) + "&upsig=deadbeef"
}

// serveMedia 回音频字节（`internal/service/audio/flacdec/testdata/stereo16.flac`
// 那份真 FLAC——解码器要真字节才放得起来）。
//
// ⚠️ **Range 也要答**：取流是按段拉的。
func serveMedia(w http.ResponseWriter, r *http.Request) {
	data, err := os.ReadFile(filepath.Join(fixtureRoot(), "internal", "service",
		"audio", "flacdec", "testdata", "stereo16.flac"))
	if err != nil {
		http.Error(w, "读不到 FLAC 素材: "+err.Error(), http.StatusInternalServerError)
		return
	}
	start := 0
	if h := r.Header.Get("Range"); strings.HasPrefix(h, "bytes=") {
		start, _ = strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(h, "bytes="), "-"))
	}
	if start > len(data) {
		http.Error(w, "越界", http.StatusRequestedRangeNotSatisfiable)
		return
	}
	w.Header().Set("Content-Length", fmt.Sprint(len(data)-start))
	if start > 0 {
		w.Header().Set("Content-Range",
			fmt.Sprintf("bytes %d-%d/%d", start, len(data)-1, len(data)))
		w.WriteHeader(http.StatusPartialContent)
	}
	_, _ = w.Write(data[start:])
}

// fixtureRoot 是仓库根（E2E 测试在仓库根下 `go build` 这台 fixture）。
func fixtureRoot() string {
	if _, err := os.Stat("internal/service/audio/flacdec/testdata/stereo16.flac"); err == nil {
		return "."
	}
	if _, err := os.Stat("../../internal/service/audio/flacdec/testdata/stereo16.flac"); err == nil {
		return "../.."
	}
	return "."
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// _ = url 引着它防未使用报错：r.PostForm 的形状靠它。
var _ = url.Values{}
