package ui

// 票 #337 覆盖 #299：全部播放替换这一批；立即播放追加单首并起播。
// 本地音频夹具走真实播放器，不访问真 B 站。

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/list"
)

// localSongs 造 n 首**本地**的歌：歌名是「本地 1」到「本地 n」，文件是同一个
// FLAC 素材的 n 份拷贝（路径不同——路径就是本地歌的身份，两首一样的路径在队列
// 里是同一首）。
func localSongs(t *testing.T, n int) []list.Music {
	t.Helper()
	b, err := os.ReadFile(flac(t))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	out := make([]list.Music, n)
	for i := range out {
		name := filepath.Join(dir, string(rune('a'+i))+".flac")
		if err := os.WriteFile(name, b, 0o644); err != nil {
			t.Fatal(err)
		}
		out[i] = list.Music{
			ID:    "local-" + string(rune('a'+i)),
			Title: "本地 " + string(rune('1'+i)),
			Owner: "本地的 UP 主",
			Item:  app.LocalItem(name),
		}
	}
	return out
}
