//go:build e2e

package main

// 播放条那三枚键（赞 / 收藏 / 评论）的**数字角标**真窗口 fixture。
//
// # 为什么单独有这一台
//
// 这一票断的是**角标的 bounds 相对那一枚键的 bounds**（它在图形右上角）——
// 而无头 Tester 的 `Find` 量的是同一棵树，但**那一棵树上「角标挂在图形盒子
// 右上角」是布局事实**，真窗口上同样成立；E2E 这一层要验的是「无障碍树里
// 角标确实是那枚键的子节点，且落在右上象限」——那是**读屏软件读得到的那一份**
// （无头窗口里没人问，那一帧压根不描述，见 `internal/e2e/progress_test.go`
// 的文件头）。
//
// ⚠️ **它跑的是真的那一枚壳**（`ui.New` + `s.View`），不是手搓的一排盒子——
// 要验的就是「真播放条上角标在哪儿」，而不是「角标这个函数在测试里放得对不对」。
//
// # 数据从哪儿来
//
// 内置一台假 B 站（`httptest`）：`/x/web-interface/view` 回 `stat`（reply /
// favorite / like），`/x/player/wbi/playurl` 回一条指向 `/cdn/a.flac` 的 dash
// 流，`/x/player/pagelist` 回一集，`/x/web-interface/nav` 回「已登录」——
// 和 `internal/ui/searchbili_test.go` 的 `biliFixture` 是同一份形状（这里只留
// 播放条要的那几路）。

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
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

func main() {
	// 内置的假 B 站：播放条要的三路（view 的 stat / pagelist / playurl→cdn）。
	srv := httptest.NewServer(http.HandlerFunc(serveFixture))
	defer srv.Close()

	a := app.New()
	cli := bili.NewClient(bili.Options{Transport: srv.Client().Transport})
	cli.TestOnlyPointHostsAtFixture(srv.URL)
	a.MountLogin(app.NewMemoryStore("counts-fixture"), cli)
	tmp, err := os.MkdirTemp("", "counts-fixture-")
	if err != nil {
		log.Fatalf("造临时目录：%v", err)
	}
	defer os.RemoveAll(tmp)
	a.MountStreaming(app.Streaming{
		Client: cli, Media: srv.Client(), TempDir: tmp,
	})

	s := ui2.New(a)
	// 队列里放进**在线那一首**：`CommentsWorthAsking` 判的是当前项是 B 站的
	// 对象，而三个数在 `/x/web-interface/view` 的 `stat` 里——同一趟回包。
	if err := a.Enqueue(app.Item{
		Source: "online", Type: "mv", Bvid: "BVcountsBadge", Cid: "777",
		Title: "有三枚角标的那一首", Artist: "某个 UP 主",
	}); err != nil {
		log.Fatalf("把在线那首放进队列：%v", err)
	}

	// ⚠️ **和用户数据目录分开**（理由同 `automation_e2e.go` 顶上那段）：登录态
	// 和封面缓存落在哪由它说了算——E2E 起的这一份有自己的一份，不碰用户那份。
	if dir := os.Getenv("BIU_E2E_DATA_DIR"); dir != "" {
		mygo.App.SetPath(mygo.PathUserData, dir)
	}

	mygo.App.WhenReady(func() {
		mygo.NewWindow(ui2.WindowOptions(s.View))
	})
	if err := mygo.App.Run(); err != nil {
		log.Println(err)
		os.Exit(1)
	}
}

// serveFixture 是那一台假 B 站的全部回包（`internal/ui/searchbili_test.go` 的
// `biliFixture` 里「播放条要的那几路」的子集）。
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
		// 三枚键的三个数（票 #701：同一趟回包）。
		writeJSON(w, map[string]any{"code": 0, "message": "0", "ttl": 1,
			"data": map[string]any{
				"aid": 4242, "bvid": r.URL.Query().Get("bvid"),
				"stat": map[string]any{"reply": 1286, "favorite": 2468, "like": 9999},
			}})

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
//
// ⚠️ **用 `r.Host` 而不是记一份 srv.URL**：handler 拿到的是进来的那一份请求，
// 主机头就是它自己——不用把 httptest 的地址穿进来。
func mediaURL(r *http.Request) string {
	return "http://" + r.Host + "/cdn/a.flac?deadline=" +
		strconv.FormatInt(time.Now().Add(2*time.Hour).Unix(), 10) + "&upsig=deadbeef"
}

// serveMedia 回音频字节（`internal/service/audio/flacdec/testdata/stereo16.flac`
// 那份真 FLAC——解码器要真字节才放得起来）。
//
// ⚠️ **Range 也要答**：取流是按段拉的（`serveMedia` 那份的同一套）。
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
	// `go build` 的工作目录是仓库根（`internal/e2e` 的测试把 `build.Dir` 钉在
	// `repoRoot`）——而**运行**时 cwd 也是仓库根（`exec.Command` 没设 Dir 就
	// 继承测试进程的 cwd，测试在 `internal/e2e` 下跑、往上两级是根）。
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
