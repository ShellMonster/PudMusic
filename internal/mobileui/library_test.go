package mobileui

import (
	"context"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/service/bili"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/mobile"
)

// TestMobileGuestFollowingOffersLogin keeps the original private page when a guest backs out of login.
func TestMobileGuestFollowingOffersLogin(t *testing.T) {
	a := app.New()
	t.Cleanup(a.Close)
	s := newShell(t.Context(), a)
	s.switchTab(2)
	s.push(route{kind: pageFollowing, title: "关注"})
	tester := ui.NewTester(s.view, 390, 844)
	if !tester.HasText("登录后查看关注") || tester.HasText("暂时没加载出来") {
		t.Fatalf("unexpected guest following state: %q", tester.Texts())
	}
	mobileLibraryShot(t, tester, "guest-following-login.png")
	if err := tester.Click("去登录"); err != nil {
		t.Fatal(err)
	}
	if s.current().kind != pageLogin {
		t.Fatal("login action did not open the login page")
	}
	if err := tester.Click("暂不登录"); err != nil {
		t.Fatal(err)
	}
	if s.current().kind != pageFollowing {
		t.Fatal("cancelling login did not return to following")
	}
}

// TestMobileMyRespectsSavedEntriesAndOpensGuestLibrary exercises the My tab's guest navigation and saved menu visibility.
func TestMobileMyRespectsSavedEntriesAndOpensGuestLibrary(t *testing.T) {
	a := app.New()
	t.Cleanup(a.Close)
	if err := a.MountSettings(filepath.Join(t.TempDir(), app.SettingsFileName)); err != nil {
		t.Fatal(err)
	}
	if err := a.SetThemeMode(app.ThemeModeLight); err != nil {
		t.Fatal(err)
	}
	a.MountLocalMusic(app.LocalMusic{})
	localUpdates := make(chan func(), 1)
	t.Cleanup(a.AddLocalWake(func() {
		select {
		case localUpdates <- func() {}:
		default:
		}
	}))
	s := newShell(t.Context(), a)
	s.switchTab(2)
	tester := ui.NewTester(s.view, 390, 844)
	mobileLibraryShot(t, tester, "mobile-light-my-guest.png")
	if err := tester.Click("最近播放"); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(tester.Texts(), "暂无历史记录") || !slices.Contains(tester.Texts(), "登录查看 B 站历史") {
		t.Fatalf("guest history is missing its offline state or account entry: %q", tester.Texts())
	}
	if err := tester.Click("返回"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.UpdateSettings(func(st *app.Settings) { st.HiddenMenuKeys = []string{"/history"} }); err != nil {
		t.Fatal(err)
	}
	tester.Frame()
	if _, visible := tester.Find("最近播放"); visible {
		t.Fatal("My still shows the history shortcut hidden in settings")
	}
	if err := tester.Click("本地音乐"); err != nil {
		t.Fatal(err)
	}
	waitLibrary(t, tester, localUpdates, func() bool { return slices.Contains(tester.Texts(), "还没有打开文件") })
}

// TestMobileCollectionUsesServerRowsAndSearchQuery checks that a guest can open a real collection and submit a server-backed search.
func TestMobileCollectionUsesServerRowsAndSearchQuery(t *testing.T) {
	queries := make(chan string, 8)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var response string
		switch r.URL.Path {
		case "/x/v3/fav/folder/info":
			response = `{"id":42,"title":"深夜循环","intro":"睡前的小曲。","upper":{"mid":100,"name":"拍档"},"media_count":2}`
		case "/x/v3/fav/resource/list":
			keyword := r.URL.Query().Get("keyword")
			queries <- keyword
			switch keyword {
			case "":
				response = `{"medias":[{"id":6,"type":2,"title":"返航","bvid":"BVfixture6"},{"id":7,"type":2,"title":"夜曲","bvid":"BVfixture7"}],"has_more":false}`
			case "夜曲":
				response = `{"medias":[{"id":7,"type":2,"title":"夜曲","bvid":"BVfixture7"}],"has_more":false}`
			default:
				t.Errorf("unexpected query %q", keyword)
				response = `{"medias":[],"has_more":false}`
			}
		default:
			t.Errorf("unexpected request %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"code":0,"data":%s}`, response)
	}))
	t.Cleanup(server.Close)
	a := app.New()
	t.Cleanup(a.Close)
	if err := a.MountSettings(filepath.Join(t.TempDir(), app.SettingsFileName)); err != nil {
		t.Fatal(err)
	}
	if err := a.SetThemeMode(app.ThemeModeLight); err != nil {
		t.Fatal(err)
	}
	client := bili.NewClient(bili.Options{Transport: server.Client().Transport})
	client.TestOnlyPointHostsAtFixture(server.URL)
	a.MountCollections(app.CollectionRefs{Client: client})
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	s := newShell(ctx, a)
	updates := make(chan func(), 12)
	s.dispatch = func(update func()) {
		select {
		case updates <- update:
		case <-ctx.Done():
		}
	}
	a.SetWake(func() { s.dispatch(func() {}) })
	s.push(route{kind: pageCollection, ref: app.CollectionRef{ID: 42, Type: bili.CollectionFavorite}})
	tester := ui.NewTester(s.view, 390, 844)
	waitLibrary(t, tester, updates, func() bool {
		return slices.Contains(tester.Texts(), "返航") && slices.Contains(tester.Texts(), "深夜循环")
	})
	mobileLibraryShot(t, tester, "mobile-light-collection.png")
	if err := tester.Click("歌单更多"); err != nil {
		t.Fatal(err)
	}
	clickLibraryControl(t, tester, "搜索歌单")
	clickLibraryControl(t, tester, "在这张歌单里找歌")
	tester.Type("夜曲")
	tester.Key(0, ui.KeyEnter)
	waitLibrary(t, tester, updates, func() bool {
		return slices.Contains(tester.Texts(), "夜曲") && !slices.Contains(tester.Texts(), "返航")
	})
	if first, second := <-queries, <-queries; first != "" || second != "夜曲" {
		t.Fatalf("collection requested queries %q, %q", first, second)
	}
}

func waitLibrary(t *testing.T, tester *ui.Tester, updates <-chan func(), ready func() bool) {
	t.Helper()
	deadline := time.NewTimer(3 * time.Second)
	defer deadline.Stop()
	for {
		tester.Frame()
		if ready() {
			return
		}
		select {
		case update := <-updates:
			update()
		case <-deadline.C:
			t.Fatalf("library did not reach expected state: %q", tester.Texts())
		}
	}
}

func clickLibraryControl(t *testing.T, tester *ui.Tester, label string) {
	t.Helper()
	deadline := tester.Now().Add(2 * time.Second)
	for {
		err := tester.Click(label)
		if err == nil {
			return
		}
		if !tester.Now().Before(deadline) {
			t.Fatal(err)
		}
		tester.Advance(time.Second / 60)
	}
}

func mobileLibraryShot(t *testing.T, tester *ui.Tester, name string) {
	t.Helper()
	dir := os.Getenv("BIU_SHOT_DIR")
	if dir == "" {
		return
	}
	file, err := os.Create(filepath.Join(dir, name))
	if err != nil {
		t.Fatal(err)
	}
	err = png.Encode(file, tester.Image())
	if closeErr := file.Close(); err != nil || closeErr != nil {
		t.Fatalf("screenshot %s: encode = %v, close = %v", name, err, closeErr)
	}
}

// TestMobileImportCopiesMatchingNamesSeparately verifies that importing two files with the same name preserves both originals and both app copies.
func TestMobileImportCopiesMatchingNamesSeparately(t *testing.T) {
	base := filepath.Join(t.TempDir(), "app")
	var roots []string
	for _, content := range []string{"first file", "second file"} {
		source := filepath.Join(t.TempDir(), "song.flac")
		if err := os.WriteFile(source, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
		root, err := copyMobileImport(base, source)
		if err != nil {
			t.Fatal(err)
		}
		roots = append(roots, root)
		for _, name := range []string{source, filepath.Join(root, "song.flac")} {
			bytes, err := os.ReadFile(name)
			if err != nil || string(bytes) != content {
				t.Fatalf("music copy %q = %q, err = %v", name, bytes, err)
			}
		}
	}
	if roots[0] == roots[1] {
		t.Fatal("the second import overwrote the first")
	}
}

// TestMobileDocumentSelectionKeepsPlayableCopies verifies that a mixed import keeps the good file and reports the failed file.
func TestMobileDocumentSelectionKeepsPlayableCopies(t *testing.T) {
	application := app.New()
	t.Cleanup(application.Close)
	settings := filepath.Join(t.TempDir(), app.SettingsFileName)
	if err := application.MountSettings(settings); err != nil {
		t.Fatal(err)
	}
	application.MountLocalMusic(app.LocalMusic{})
	s := newShell(t.Context(), application)
	s.dispatch = func(apply func()) { apply() }
	state := &localLibraryState{base: filepath.Join(t.TempDir(), "imports"), working: true}
	s.finishLocalImport(state, []string{
		filepath.Join("..", "service", "audio", "testdata", "tone.mp3"),
		filepath.Join(t.TempDir(), "missing.mp3"),
	}, 0, nil)
	if state.working || state.status != "已导入 1 个文件，1 个文件未能导入" {
		t.Fatalf("import status = %q, working = %t", state.status, state.working)
	}
	if err := application.FlushSettings(); err != nil {
		t.Fatal(err)
	}
	saved, err := app.LoadSettings(settings)
	if err != nil || len(saved.LocalMusicDirs) != 1 {
		t.Fatalf("saved import roots = %v, err = %v", saved.LocalMusicDirs, err)
	}
	if err := application.Open(filepath.Join(saved.LocalMusicDirs[0], "tone.mp3")); err != nil {
		t.Fatalf("open imported file: %v", err)
	}
}

// TestMobileCoverCropUsesPanAndZoom verifies that the exported square reflects the crop preview's translation and scale.
func TestMobileCoverCropUsesPanAndZoom(t *testing.T) {
	source := image.NewRGBA(image.Rect(0, 0, 120, 80))
	red := color.RGBA{R: 255, A: 255}
	blue := color.RGBA{B: 255, A: 255}
	for y := range 80 {
		for x := range 120 {
			ink := blue
			if x < 60 {
				ink = red
			}
			source.SetRGBA(x, y, ink)
		}
	}
	left := cropFolderImage(source, mobile.ZoomState{Scale: 2, X: 40}, 80)
	right := cropFolderImage(source, mobile.ZoomState{Scale: 2, X: -40}, 80)
	if left.Bounds() != image.Rect(0, 0, 40, 40) || right.Bounds() != left.Bounds() {
		t.Fatalf("cropped bounds: left %v, right %v", left.Bounds(), right.Bounds())
	}
	if left.At(20, 20) != red || right.At(20, 20) != blue {
		t.Fatalf("crop ignored pan: left %v, right %v", left.At(20, 20), right.At(20, 20))
	}
}
