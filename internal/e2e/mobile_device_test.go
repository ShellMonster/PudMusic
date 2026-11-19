package e2e

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestMobileDeviceNavigation drives the installed app and keeps a screenshot of each checked page when requested.
func TestMobileDeviceNavigation(t *testing.T) {
	device := os.Getenv("PUDMUSIC_DEVICE_CLI")
	if device == "" {
		t.Skip("set PUDMUSIC_DEVICE_CLI to the agent-device command returned by device_open")
	}
	if !filepath.IsAbs(device) {
		t.Fatal("PUDMUSIC_DEVICE_CLI must be an absolute path")
	}
	shots := os.Getenv("PUDMUSIC_DEVICE_SHOTS")
	if shots != "" {
		if !filepath.IsAbs(shots) {
			t.Fatal("PUDMUSIC_DEVICE_SHOTS must be an absolute path")
		}
		if err := os.MkdirAll(shots, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	run := func(args ...string) {
		t.Helper()
		ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
		defer cancel()
		output, err := exec.CommandContext(ctx, device, args...).CombinedOutput()
		if err != nil {
			t.Fatalf("agent-device %s: %v\n%s", strings.Join(args, " "), err, output)
		}
	}
	press := func(label string) { run("press", "label=\""+label+"\"", "--settle") }
	check := func(page, label string) {
		run("wait", "label=\""+label+"\"")
		if shots != "" {
			run("screenshot", filepath.Join(shots, page+".png"))
		}
	}

	run("open", "io.github.shellmonster.biu", "--relaunch")
	check("discover", "每日推荐")
	press("搜索")
	check("search", "搜索歌曲、歌手、歌单或 UP 主")
	press("我的")
	check("my", "本地音乐")
	press("设置")
	check("settings", "播放与下载")
	press("外观")
	check("appearance", "深浅模式")
	press("返回")
	check("settings-back", "播放与下载")
	press("返回")
	press("本地音乐")
	check("library", "添加文件")
}
