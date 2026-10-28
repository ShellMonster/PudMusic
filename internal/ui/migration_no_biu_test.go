package ui

import (
	"os"
	"path/filepath"
	"testing"
)

// 启动迁移只处理本应用自己的数据升级，不会读取机器上的 Biu 目录。
func TestStartupMigrationDoesNotImportFromBiu(t *testing.T) {
	biu := t.TempDir()
	for name, body := range map[string]string{
		"app-settings.json":      `{"appSettings":{"primaryColor":"#17c964"}}`,
		"shortcut-settings.json": `{"shortcuts":[],"globalShortcuts":[]}`,
		"media-downloads.json":   `{"task":{"title":"Biu download"}}`,
		"lyrics-cache.json":      `{"BV1-1":{"lyrics":"Biu lyric"}}`,
	} {
		if err := os.WriteFile(filepath.Join(biu, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	dst := t.TempDir()

	if err := mountMigrationAt(dst); err != nil {
		t.Fatalf("启动迁移失败了：%v", err)
	}
	if rep := LastMigration(); rep.Upgraded == "" {
		t.Fatalf("启动迁移没有报告本应用数据升级：%+v", rep)
	}
	for _, name := range []string{
		"settings.json",
		"shortcuts.json",
		"download-queue.json",
		"lyrics-cache",
	} {
		if _, err := os.Stat(filepath.Join(dst, name)); err == nil {
			t.Fatalf("Biu 目录 %s 的 %s 被写进了本应用数据目录", biu, name)
		}
	}
	if entries, err := os.ReadDir(dst); err != nil {
		t.Fatal(err)
	} else if len(entries) != 1 {
		t.Fatalf("启动迁移除了数据版本标记外还写入了 %d 个文件：%v", len(entries), entries)
	}
}

func mustWriteHere(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
