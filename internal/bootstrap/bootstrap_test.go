package bootstrap

import (
	"errors"
	"reflect"
	"testing"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/media"
	"github.com/ShellMonster/PudMusic/internal/service/bili"
)

func TestAssembleRunsHeadlessInDependencyOrder(t *testing.T) {
	var got []string
	mark := func(name string) { got = append(got, name) }
	cli := bili.NewClient(bili.Options{})
	var streamingClient, searchClient, collectionsClient, exploreClient, libraryClient, recommendClient *bili.Client
	d := Dependencies{
		PinUserDataDir: func() error { mark("pin data directory"); return nil },
		NewApp:         func() *app.App { mark("new app"); return app.New() },
		MountMigration: func() error { mark("migration"); return nil },
		MountSettings:  func(*app.App) error { mark("settings"); return nil },
		MountFullPlayerPrefs: func(_ *app.App) {
			mark("full player preferences")
		},
		MountLogin: func(*app.App) *bili.Client { mark("login"); return cli },
		MountStreaming: func(_ *app.App, got *bili.Client) {
			mark("streaming")
			streamingClient = got
		},
		MountSearch: func(_ *app.App, got *bili.Client) {
			mark("search")
			searchClient = got
		},
		StartWarmUp: func(*app.App) { mark("warm up") },
		MountCollections: func(_ *app.App, got *bili.Client) {
			mark("collections")
			collectionsClient = got
		},
		MountExplore: func(_ *app.App, got *bili.Client) {
			mark("explore")
			exploreClient = got
		},
		MountLibrary: func(_ *app.App, got *bili.Client) {
			mark("library")
			libraryClient = got
		},
		MountRecommend: func(_ *app.App, got *bili.Client) {
			mark("recommend")
			recommendClient = got
		},
		MountLocalMusic: func(*app.App) { mark("local music") },
		MountLyrics:     func(*app.App) { mark("lyrics") },
		MountComments:   func(*app.App) { mark("comments") },
		MountDanmaku:    func(*app.App) { mark("danmaku") },
		MountUpdates:    func(*app.App) error { mark("updates"); return nil },
		MountDownloads:  func(*app.App) error { mark("downloads"); return nil },
		MountNotifications: func(*app.App) {
			mark("notifications")
		},
		MountMedia:  func(*app.App) *media.Session { mark("media"); return nil },
		NoteMounted: func(*app.App) { mark("mounted") },
		Run: func(*app.App, *media.Session) error {
			mark("run")
			return nil
		},
		Logf: func(string, ...any) {},
	}
	a, err := Assemble(d)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(a.Close)

	want := []string{
		"pin data directory", "new app", "migration", "settings", "full player preferences",
		"login", "streaming", "search", "warm up", "collections", "explore", "library", "recommend",
		"local music", "lyrics", "comments", "danmaku", "updates", "downloads", "notifications",
		"media", "mounted", "run",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("assembly order = %v, want %v", got, want)
	}
	if streamingClient != cli || searchClient != cli || collectionsClient != cli || exploreClient != cli || libraryClient != cli || recommendClient != cli {
		t.Fatal("client-dependent mounts did not share the login client")
	}
}

func TestAssembleRunsWithOptionalDataDirectoryPinningOmitted(t *testing.T) {
	var run bool
	d := Dependencies{
		NewApp: func() *app.App { return app.New() },
		Run: func(*app.App, *media.Session) error {
			run = true
			return nil
		},
	}

	a, err := Assemble(d)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(a.Close)
	if !run {
		t.Fatal("Run was not entered with optional PinUserDataDir omitted")
	}

	for name, required := range map[string]Dependencies{
		"NewApp": {Run: d.Run},
		"Run":    {NewApp: d.NewApp},
	} {
		t.Run("missing "+name, func(t *testing.T) {
			if _, err := Assemble(required); err == nil {
				t.Fatalf("Assemble succeeded with missing %s", name)
			}
		})
	}
}

func TestAssembleContinuesAfterOptionalMountFailures(t *testing.T) {
	var got []string
	mark := func(name string) { got = append(got, name) }
	var logs []string
	d := minimalDependencies(mark)
	d.MountMigration = func() error { mark("migration"); return errors.New("migration failed") }
	d.MountSettings = func(*app.App) error { mark("settings"); return errors.New("settings failed") }
	d.MountUpdates = func(*app.App) error { mark("updates"); return errors.New("updates failed") }
	d.MountDownloads = func(*app.App) error { mark("downloads"); return errors.New("downloads failed") }
	d.Logf = func(format string, args ...any) { logs = append(logs, format) }

	a, err := Assemble(d)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(a.Close)
	if len(got) != 23 || got[len(got)-1] != "run" {
		t.Fatalf("optional failures stopped assembly: %v", got)
	}
	if len(logs) != 4 {
		t.Fatalf("optional failures logged %d times, want 4: %v", len(logs), logs)
	}
}

func TestAssembleStopsBeforeRunWhenRequiredInitializationFails(t *testing.T) {
	var run bool
	d := minimalDependencies(func(string) {})
	d.NewApp = func() *app.App { return nil }
	d.Run = func(*app.App, *media.Session) error {
		run = true
		return nil
	}

	a, err := Assemble(d)
	if err == nil {
		t.Fatal("Assemble succeeded after required initialization failed")
	}
	if a != nil {
		t.Fatal("Assemble returned an app after NewApp returned nil")
	}
	if run {
		t.Fatal("Run entered after required initialization failed")
	}
}

func TestAssembleContinuesWithFrameworkFallbackWhenDataDirectoryCannotBePinned(t *testing.T) {
	var got []string
	d := minimalDependencies(func(name string) { got = append(got, name) })
	d.PinUserDataDir = func() error { return errors.New("data directory unavailable") }

	a, err := Assemble(d)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(a.Close)
	if len(got) == 0 || got[0] != "new app" {
		t.Fatalf("fallback startup did not continue with app creation: %v", got)
	}
}

func TestAssembleReturnsRunFailureAndAppRemainsClosable(t *testing.T) {
	d := minimalDependencies(func(string) {})
	runErr := errors.New("event loop failed")
	d.Run = func(*app.App, *media.Session) error { return runErr }

	a, err := Assemble(d)
	if !errors.Is(err, runErr) {
		t.Fatalf("Assemble error = %v, want %v", err, runErr)
	}
	if a == nil {
		t.Fatal("Assemble returned no app for cleanup")
	}
	a.Close()
	a.Close()
}

func minimalDependencies(mark func(string)) Dependencies {
	cli := bili.NewClient(bili.Options{})
	return Dependencies{

		PinUserDataDir: func() error { mark("pin"); return nil },
		NewApp:         func() *app.App { mark("new app"); return app.New() },
		MountMigration: func() error { mark("migration"); return nil },
		MountSettings:  func(*app.App) error { mark("settings"); return nil },
		MountFullPlayerPrefs: func(_ *app.App) {
			mark("full player preferences")
		},
		MountLogin:     func(*app.App) *bili.Client { mark("login"); return cli },
		MountStreaming: func(*app.App, *bili.Client) { mark("streaming") },
		MountSearch:    func(*app.App, *bili.Client) { mark("search") },
		StartWarmUp:    func(*app.App) { mark("warm up") },
		MountCollections: func(*app.App, *bili.Client) {
			mark("collections")
		},
		MountExplore: func(*app.App, *bili.Client) { mark("explore") },
		MountLibrary: func(*app.App, *bili.Client) { mark("library") },
		MountRecommend: func(*app.App, *bili.Client) {
			mark("recommend")
		},
		MountLocalMusic: func(*app.App) { mark("local music") },
		MountLyrics:     func(*app.App) { mark("lyrics") },
		MountComments:   func(*app.App) { mark("comments") },
		MountDanmaku:    func(*app.App) { mark("danmaku") },
		MountUpdates:    func(*app.App) error { mark("updates"); return nil },
		MountDownloads:  func(*app.App) error { mark("downloads"); return nil },
		MountNotifications: func(*app.App) {
			mark("notifications")
		},
		MountMedia:  func(*app.App) *media.Session { mark("media"); return nil },
		NoteMounted: func(*app.App) { mark("mounted") },
		Run:         func(*app.App, *media.Session) error { mark("run"); return nil },
		Logf:        func(string, ...any) {},
	}
}
