package mobileui

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/service/download"
	"github.com/ShellMonster/PudMusic/internal/service/localmusic"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/mobile"
)

const localImportDirectory = "mobile-imports"

type localLibraryState struct {
	working, menu, confirm bool
	base, status, undoRoot string
	target                 localmusic.Track
}

func (s *shell) localMusic(c *ui.Context, page *route) {
	state, _ := page.data.(*localLibraryState)
	if state == nil {
		state = &localLibraryState{base: mobileImportBase()}
		page.data = state
		s.app.SetLocalRoots(s.app.SettingsSnapshot().LocalMusicDirs)
		s.app.RescanLocalMusic()
	}
	snapshot := s.app.LocalMusicState()
	var items []app.Item
	for _, track := range snapshot.Tracks {
		if track.Playable() {
			items = append(items, localTrackItem(track))
		}
	}
	ui.Text(c, fmt.Sprintf("%d 个文件 · FLAC、MP3、M4A、WAV 等", len(snapshot.Tracks))).TextColor(c.Theme().TextMuted).FontSize(13)
	ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
		libraryButton(c, "plus", "添加文件", true, state.working || state.base == "" || !s.app.LocalMusicMounted(), func() { s.importLocalMusic(state, false) })
		if runtime.GOOS == "android" {
			iconButton(c, "folder", "添加文件夹").Disabled(state.working || state.base == "" || !s.app.LocalMusicMounted()).OnClick(func() { s.importLocalMusic(state, true) })
		}
		libraryButton(c, "play", "播放全部", false, len(items) == 0, func() { s.play(items, 0) })
		iconButton(c, "refresh", "重新扫描").Disabled(snapshot.Scanning).OnClick(s.app.RescanLocalMusic)
	})
	if state.status != "" {
		ui.Text(c, state.status).TextColor(c.Theme().TextMuted).FontSize(13)
	}
	if state.undoRoot != "" {
		libraryButton(c, "", "撤销移除", false, state.working, func() {
			root := state.undoRoot
			if s.updateLocalRoots(state, func(roots []string) []string { return append(roots, root) }) {
				state.undoRoot, state.status = "", "已恢复到本地音乐"
			}
		})
	}
	if !s.app.LocalMusicMounted() {
		libraryMessage(c, "本地音乐暂时不可用", "重新打开应用后再试。")
	} else if state.working || snapshot.Scanning {
		ui.Text(c, "正在扫描…").FontSize(13).TextColor(c.Theme().TextMuted)
	} else if snapshot.Err != "" {
		libraryMessage(c, "部分文件无法读取", "确认文件仍可访问后重新扫描。")
	} else if len(snapshot.Tracks) == 0 {
		libraryMessage(c, "还没有打开文件", "从系统文件选择器导入音频后，即可离线播放。")
	}
	for _, track := range snapshot.Tracks {
		meta := strings.ToUpper(strings.TrimPrefix(track.Format, ".")) + " · " + libraryByteSize(track.Size)
		var play func()
		if track.Playable() {
			play = func() { s.play([]app.Item{localTrackItem(track)}, 0) }
		} else {
			meta += " · 暂不支持此格式"
		}
		row := func() {
			ui.Row(c).AlignItems(ui.Center).Children(func() {
				libraryRow(c, "", "disc", filepath.Base(track.Path), meta, play).Grow(1).Basis(0)
				if mobileImportTrack(state.base, track) {
					iconButton(c, "more", "更多操作："+track.Title).OnClick(func() { state.target, state.menu = track, true })
				}
			})
		}
		if mobileImportTrack(state.base, track) {
			mobile.SwipeRow(c.Key(track.ID), nil, mobile.SwipeOptions{Trailing: []mobile.SwipeAction{{Label: "移除", OnTap: func() { s.removeImportedTrack(state, track) }}}}, row)
		} else {
			ui.Box(c.Key(track.ID)).FillWidth().Children(row)
		}
	}
	libraryActionSheet(c, &state.menu, mobile.ActionSheetOptions{Title: state.target.Title, Cancel: "取消"},
		mobile.Action{Label: "从列表移除", Disabled: state.working, OnTap: func() { s.removeImportedTrack(state, state.target) }},
		mobile.Action{Label: "删除应用内副本", Disabled: state.working, Destructive: true, OnTap: func() { state.confirm = true }},
	)
	libraryActionSheet(c, &state.confirm, mobile.ActionSheetOptions{Title: "删除应用内副本？", Message: "来源文件会保留。", Cancel: "取消"},
		mobile.Action{Label: "删除", Destructive: true, OnTap: func() { s.deleteImportedTrack(state, state.target) }},
	)
}

func mobileImportBase() string {
	if dir, err := mygo.App.Path(mygo.PathUserData); err == nil && filepath.IsAbs(dir) {
		return filepath.Join(dir, localImportDirectory)
	}
	return ""
}

func localTrackItem(track localmusic.Track) app.Item {
	item := app.LocalItem(track.Path)
	item.Title = track.Title
	return item
}

func (s *shell) importLocalMusic(state *localLibraryState, directory bool) {
	if state.working || s.win == nil {
		return
	}
	state.working, state.status = true, ""
	go func() {
		var files []string
		var err error
		if directory {
			var selection mobileDocumentSelection
			err = mygo.Native("pudmusic_documents").Call(context.WithoutCancel(s.ctx), "pickDirectory", struct{ Extensions []string }{localmusic.Extensions}, &selection)
			if selection.Directory != "" {
				defer os.RemoveAll(selection.Directory)
			}
			files = selection.Files
			s.finishLocalImport(state, files, selection.Failed, err)
			return
		}
		files, err = mygo.Dialog.Open(mygo.OpenDialogOptions{
			Parent: s.win, Title: "导入音频", Multiple: true,
			Filters: []mygo.FileFilter{{Name: "音频文件", Extensions: localmusic.Extensions, MIMETypes: []string{"audio/*"}}},
		})
		if len(files) == 0 {
			files = nil
		}
		s.finishLocalImport(state, files, 0, err)
	}()
}

func (s *shell) finishLocalImport(state *localLibraryState, files []string, failed int, err error) {
	var imported []string
	if err == nil {
		for _, file := range files {
			root, copyErr := copyMobileImport(state.base, file)
			if copyErr != nil {
				failed++
			} else {
				imported = append(imported, root)
			}
		}
	}
	s.update(func() {
		state.working = false
		switch {
		case err != nil:
			state.status = "无法打开文件选择器，请重试。"
		case len(imported) != 0:
			if s.updateLocalRoots(state, func(roots []string) []string { return append(roots, imported...) }) {
				state.status = fmt.Sprintf("已导入 %d 个文件", len(imported))
				if failed > 0 {
					state.status += fmt.Sprintf("，%d 个文件未能导入", failed)
				}
			} else {
				for _, root := range imported {
					_ = os.RemoveAll(root)
				}
			}
		case failed > 0:
			state.status = "无法读取选中的音频文件。"
		case files != nil:
			state.status = "没有可导入的音频文件。"
		}
		if page := s.current(); page.data != state && state.status != "" {
			s.notice = state.status
		}
	})
}

func copyMobileImport(base, source string) (string, error) {
	if base == "" || !slices.Contains(localmusic.Extensions, strings.ToLower(filepath.Ext(source))) {
		return "", errors.New("unsupported audio file")
	}
	input, err := os.Open(source)
	if err != nil {
		return "", err
	}
	defer input.Close()
	metadata, err := input.Stat()
	if err != nil || !metadata.Mode().IsRegular() {
		return "", errors.New("selected item is not a regular file")
	}
	if err := os.MkdirAll(base, 0700); err != nil {
		return "", err
	}
	root, err := os.MkdirTemp(base, "audio-")
	if err != nil {
		return "", err
	}
	output, err := os.OpenFile(filepath.Join(root, filepath.Base(source)), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err == nil {
		_, copyErr := io.Copy(output, input)
		err = errors.Join(copyErr, output.Close())
	}
	if err != nil {
		_ = os.RemoveAll(root)
		return "", err
	}
	return root, nil
}

func (s *shell) updateLocalRoots(state *localLibraryState, change func([]string) []string) bool {
	updated, err := s.app.UpdateSettings(func(settings *app.Settings) {
		settings.LocalMusicDirs = change(slices.Clone(settings.LocalMusicDirs))
	})
	if err != nil {
		state.status = "无法保存本地音乐列表，请重试。"
		return false
	}
	s.app.SetLocalRoots(updated.LocalMusicDirs)
	return true
}

func mobileImportTrack(base string, track localmusic.Track) bool {
	return base != "" && filepath.Dir(track.Root) == base && strings.HasPrefix(filepath.Base(track.Root), "audio-") &&
		filepath.Dir(track.Path) == track.Root
}

func (s *shell) removeImportedTrack(state *localLibraryState, track localmusic.Track) {
	if !mobileImportTrack(state.base, track) || state.working {
		return
	}
	if s.updateLocalRoots(state, func(roots []string) []string { return withoutLocalRoot(roots, track.Root) }) {
		state.undoRoot, state.status = track.Root, "已从列表移除"
	}
}

func (s *shell) deleteImportedTrack(state *localLibraryState, track localmusic.Track) {
	if !mobileImportTrack(state.base, track) || state.working {
		return
	}
	if item, ok := s.app.PlayingItem(); ok && item.Path == track.Path {
		state.status = "请先切换到其他歌曲。"
		return
	}
	state.working = true
	go func() {
		resolved, err := filepath.EvalSymlinks(track.Path)
		base, baseErr := filepath.EvalSymlinks(state.base)
		if err == nil && (baseErr != nil || resolved != track.Canonical || filepath.Dir(filepath.Dir(resolved)) != base) {
			err = errors.New("file moved outside the app folder")
		}
		if err == nil {
			err = os.Remove(track.Path)
		}
		if err == nil {
			_ = os.Remove(track.Root)
		}
		s.update(func() {
			state.working = false
			if err != nil {
				state.status = "无法删除应用内副本，请重试。"
				return
			}
			if s.updateLocalRoots(state, func(roots []string) []string { return withoutLocalRoot(roots, track.Root) }) {
				state.status = "已删除应用内副本"
			}
		})
	}()
}

func withoutLocalRoot(roots []string, remove string) []string {
	return slices.DeleteFunc(roots, func(root string) bool { return root == remove })
}

type downloadLibraryState struct {
	filter                        int
	menu, confirm, clear, storage bool
	items                         []offlineFile
	target                        download.Snapshot
	file                          offlineFile
	deleteFile, working           bool
	status                        string
}

type offlineFile struct {
	path string
	size int64
}

func (s *shell) downloadLibrary(c *ui.Context, page *route) {
	state, _ := page.data.(*downloadLibraryState)
	if state == nil {
		state = &downloadLibraryState{}
		page.data = state
		if page.id == "storage" && s.app.DownloadMounted() {
			s.scanOfflineFiles(state)
		}
	}
	snapshot := s.app.DownloadSnapshotList()
	ui.Text(c, "保存到布丁音乐 · 下载完成后可离线播放").TextColor(c.Theme().TextMuted).FontSize(13)
	ui.Row(c).Gap(5).Children(func() {
		libraryButton(c, "folder", "管理存储", false, !s.app.DownloadMounted(), func() { s.scanOfflineFiles(state) })
		libraryButton(c, "trash", "清空记录", false, len(snapshot.Rows) == 0 || state.working, func() { state.clear = true })
	})
	labels := []string{"全部", "下载中", "已完成", "失败"}
	counts := make([]int, len(labels))
	for _, task := range snapshot.Rows {
		counts[0]++
		counts[downloadCategory(task.Status)]++
	}
	ui.Row(c).Gap(5).Children(func() {
		for index, title := range labels {
			librarySegment(c, fmt.Sprintf("%s %d", title, counts[index]), state.filter == index, func() { state.filter = index })
		}
	})
	if state.status != "" {
		ui.Text(c, state.status).TextColor(c.Theme().TextMuted).FontSize(13)
	}
	if counts[state.filter] == 0 {
		titles := []string{"还没有可以下载的内容", "没有进行中的下载", "没有已完成的下载", "没有失败的下载"}
		text := ""
		if state.filter == 0 {
			text = "在歌曲或收藏夹的更多操作里选择下载。"
		}
		libraryMessage(c, titles[state.filter], text)
	}
	for _, task := range snapshot.Rows {
		if state.filter == 0 || state.filter == downloadCategory(task.Status) {
			s.downloadRow(c.Key(task.ID), state, task)
		}
	}
	var more []mobile.Action
	if state.target.Status == download.StatusDone {
		more = append(more, mobile.Action{Label: "分享导出", OnTap: func() { s.exportOffline(s.app.DownloadDir(), state.target.Dest) }})
	}
	more = append(more, mobile.Action{Label: "移除记录", Destructive: true, OnTap: func() { state.confirm = true }})
	libraryActionSheet(c, &state.menu, mobile.ActionSheetOptions{Title: state.target.Title, Cancel: "取消"}, more...)
	libraryActionSheet(c, &state.confirm, mobile.ActionSheetOptions{Title: "移除下载记录？", Message: "已下载的文件可以在管理存储查看。", Cancel: "取消"},
		mobile.Action{Label: "移除", Destructive: true, OnTap: func() { s.report(s.app.RemoveDownload(state.target.ID)) }},
	)
	libraryActionSheet(c, &state.clear, mobile.ActionSheetOptions{Title: "清空下载记录？", Message: "未完成的任务将停止。已下载的文件可以在管理存储查看。", Cancel: "取消"},
		mobile.Action{Label: "清空", Destructive: true, OnTap: s.app.ClearDownloads},
	)
	s.downloadStorage(c, state)
}

func downloadCategory(status download.Status) int {
	switch status {
	case download.StatusDone:
		return 2
	case download.StatusError:
		return 3
	default:
		return 1
	}
}

func (s *shell) downloadRow(c *ui.Context, state *downloadLibraryState, task download.Snapshot) {
	ui.Row(c).Gap(5).AlignItems(ui.Center).Children(func() {
		cover(c, task.Cover, 48, 10)
		ui.Column(c).Grow(1).Basis(0).Gap(3).Children(func() {
			ui.Text(c, task.Title).FontSize(14).Bold().MaxLines(1)
			kind := "音频"
			if task.Video {
				kind = "视频"
			}
			status := map[download.Status]string{download.StatusQueued: "排队中", download.StatusRunning: "下载中", download.StatusPaused: "下载暂停", download.StatusMuxing: "正在合成", download.StatusDone: "已完成", download.StatusError: "失败"}[task.Status]
			if task.Status == download.StatusError && task.Err != "" {
				status += " · " + task.Err
			}
			ui.Text(c, kind+" · "+status).FontSize(12).TextColor(c.Theme().TextMuted).MaxLines(2)
			if task.Status != download.StatusDone && task.Percent >= 0 {
				ui.Progress(c, float64(task.Percent)/100).Height(3).Label(task.Title + " 下载进度")
			}
		})
		switch {
		case task.Status == download.StatusDone:
			iconButton(c, "play", "离线播放 "+task.Title).OnClick(func() { s.playOffline(task.Dest, task.Title) })
		case task.Status.CanPause():
			iconButton(c, "pause", "暂停 "+task.Title).OnClick(func() { s.report(s.app.PauseDownload(task.ID)) })
		case task.Status == download.StatusPaused:
			iconButton(c, "play", "继续 "+task.Title).OnClick(func() { s.report(s.app.ResumeDownload(task.ID)) })
		case task.Status == download.StatusError && task.CanRetry:
			iconButton(c, "refresh", "重试 "+task.Title).OnClick(func() { s.report(s.app.ResumeDownload(task.ID)) })
		}
		iconButton(c, "more", "下载操作："+task.Title).OnClick(func() { state.target, state.menu = task, true })
	})
}

func (s *shell) playOffline(path, title string) {
	if !downloadFileInside(s.app.DownloadDir(), path) {
		s.notice = "无法找到离线文件。"
		return
	}
	item := app.LocalItem(path)
	item.Title = title
	s.play([]app.Item{item}, 0)
}

func (s *shell) exportOffline(dir, path string) {
	if !downloadFileInside(dir, path) || s.win == nil {
		return
	}
	go func() {
		_, err := mygo.Dialog.SaveCopy(path, filepath.Base(path))
		if err != nil {
			s.update(func() { s.notice = "导出失败，请重试。" })
		}
	}()
}

func (s *shell) scanOfflineFiles(state *downloadLibraryState) {
	if state.working {
		return
	}
	state.storage, state.working = true, true
	dir := s.app.DownloadDir()
	active := make(map[string]bool)
	for _, task := range s.app.DownloadSnapshotList().Rows {
		if task.Status != download.StatusDone {
			active[task.Dest] = true
		}
	}
	go func() {
		entries, err := os.ReadDir(dir)
		files := make([]offlineFile, 0, len(entries))
		for _, entry := range entries {
			path := filepath.Join(dir, entry.Name())
			if active[path] || !downloadFileInside(dir, path) {
				continue
			}
			info, err := entry.Info()
			if err == nil {
				files = append(files, offlineFile{path, info.Size()})
			}
		}
		s.update(func() {
			state.working, state.items = false, files
			if err != nil && !errors.Is(err, os.ErrNotExist) {
				state.status = "无法读取离线文件。"
			}
		})
	}()
}

func downloadFileInside(root, path string) bool {
	if root == "" || path == "" || filepath.Dir(filepath.Clean(path)) != filepath.Clean(root) {
		return false
	}
	extension := strings.ToLower(filepath.Ext(path))
	if !slices.Contains(localmusic.Extensions, extension) && extension != ".mp4" {
		return false
	}
	stat, err := os.Lstat(path)
	return err == nil && stat.Mode().IsRegular()
}

func (s *shell) downloadStorage(c *ui.Context, state *downloadLibraryState) {
	librarySheet(c, &state.storage, mobile.SheetOptions{Label: "管理存储", Detents: []float32{0.65, 0.9}}, func() {
		ui.Scroll(c).Fill().Padding(16).Gap(7).Children(func() {
			var total int64
			for _, file := range state.items {
				total += file.size
			}
			ui.Text(c, "管理存储").FontSize(20).Bold()
			ui.Text(c, fmt.Sprintf("%d 个文件 · %s", len(state.items), libraryByteSize(total))).FontSize(13).TextColor(c.Theme().TextMuted)
			if state.working {
				libraryMore(c, true, nil)
			}
			for _, file := range state.items {
				name := filepath.Base(file.path)
				ui.Row(c.Key(file.path)).AlignItems(ui.Center).Children(func() {
					libraryRow(c, "", "download", name, libraryByteSize(file.size), func() { s.playOffline(file.path, name) }).Grow(1).Basis(0)
					iconButton(c, "open", "导出 "+name).OnClick(func() { s.exportOffline(s.app.DownloadDir(), file.path) })
					iconButton(c, "trash", "删除 "+name).OnClick(func() { state.file, state.deleteFile = file, true })
				})
			}
		})
	})
	libraryActionSheet(c, &state.deleteFile, mobile.ActionSheetOptions{Title: "删除离线文件？", Message: filepath.Base(state.file.path), Cancel: "取消"},
		mobile.Action{Label: "删除文件", Destructive: true, OnTap: func() { s.deleteOfflineFile(state, state.file.path) }},
	)
}

func (s *shell) deleteOfflineFile(state *downloadLibraryState, path string) {
	if item, ok := s.app.PlayingItem(); ok && item.Path == path {
		s.notice = "请先切换到其他歌曲。"
		return
	}
	for _, task := range s.app.DownloadSnapshotList().Rows {
		if task.Dest == path && task.Status != download.StatusDone {
			s.notice = "请先结束这项下载。"
			return
		}
	}
	if !downloadFileInside(s.app.DownloadDir(), path) || os.Remove(path) != nil {
		s.notice = "无法删除离线文件，请重试。"
		return
	}
	s.scanOfflineFiles(state)
}

func libraryByteSize(size int64) string {
	const kib = 1024
	const mib = 1024 * kib
	const gib = 1024 * mib
	if size >= gib {
		return fmt.Sprintf("%.1f GB", float64(size)/gib)
	}
	if size >= mib {
		return fmt.Sprintf("%.1f MB", float64(size)/mib)
	}
	if size >= kib {
		return fmt.Sprintf("%.1f KB", float64(size)/kib)
	}
	return fmt.Sprintf("%d B", size)
}

type libraryStatsState struct {
	days     [7]libraryStatsDay
	hours    [24]int64
	tracks   map[string]*app.StatsTrackStat
	artists  map[int64]*app.StatsArtistStat
	total    int64
	plays    int64
	newUsers int
	previous int64
}

type libraryStatsDay struct {
	name    string
	minutes int64
}

func (s *shell) libraryStats(c *ui.Context, page *route) {
	state, _ := page.data.(*libraryStatsState)
	if state == nil {
		stats, _ := s.app.ListeningStats()
		state = summarizeLibraryStats(stats, time.Now().Local())
		page.data = state
	}
	ui.Text(c, "最近 7 天 · 只统计在本机的收听").FontSize(13).TextColor(c.Theme().TextMuted)
	if state.total == 0 && state.plays == 0 {
		libraryMessage(c, "暂无历史记录", "播放过的歌曲会按日期记录在这里。")
		return
	}
	minutes := state.total / 60000
	peak := 0
	for hour := range state.hours {
		if state.hours[hour] > state.hours[peak] {
			peak = hour
		}
	}
	peakTime := "—"
	if state.hours[peak] > 0 {
		peakTime = fmt.Sprintf("%02d:00", peak)
	}
	durationLabel := "收听总时长"
	if state.previous > 0 {
		difference := minutes - state.previous/60000
		durationLabel = fmt.Sprintf("比上周 %+d 分钟", difference)
	}
	metrics := [][2]string{{fmt.Sprintf("%d 小时 %d 分", minutes/60, minutes%60), durationLabel}, {fmt.Sprintf("%d 首", len(state.tracks)), "播放过的歌曲"}, {fmt.Sprintf("%d 位", state.newUsers), "新认识的歌手"}, {peakTime, "最常收听的时段"}}
	for index := 0; index < len(metrics); index += 2 {
		ui.Row(c).Gap(8).Children(func() {
			for _, metric := range metrics[index : index+2] {
				ui.Column(c).Grow(1).Basis(0).Padding(12).Radius(12).Background(c.Theme().Surface).Gap(4).Children(func() {
					ui.Text(c, metric[0]).FontSize(19).Bold()
					ui.Text(c, metric[1]).FontSize(12).TextColor(c.Theme().TextMuted)
				})
			}
		})
	}
	sectionHeading(c, "每天收听", "")
	var maxMinutes int64
	for _, day := range state.days {
		maxMinutes = max(maxMinutes, day.minutes)
	}
	ui.Row(c).Gap(4).Padding(8).Background(c.Theme().Surface).Radius(12).Children(func() {
		for index, day := range state.days {
			ink := c.Theme().Border
			if index == len(state.days)-1 {
				ink = c.Theme().Accent
			}
			ui.Column(c).Grow(1).Basis(0).Height(138).Gap(4).Justify(ui.End).AlignItems(ui.Center).
				Label(fmt.Sprintf("%s %d 分钟", day.name, day.minutes)).Children(func() {
				ui.Text(c, fmt.Sprint(day.minutes)).FontSize(11).TextColor(c.Theme().TextMuted)
				ui.Box(c).Width(21).Height(max(2, 91*float32(day.minutes)/float32(max(maxMinutes, 1)))).Radius(4).Background(ink)
				ui.Text(c, day.name).FontSize(11).TextColor(c.Theme().TextMuted)
			})
		}
	})
	stats := app.ListeningStats{Tracks: state.tracks, Artists: state.artists}
	if artists := stats.TopArtists(5); len(artists) > 0 {
		sectionHeading(c, "最常听的歌手", "")
		for _, artist := range artists {
			ui.Row(c.Key(artist.Mid)).AlignItems(ui.Center).Gap(12).Children(func() {
				ui.Text(c, artist.Name).Grow(1).FontSize(13).MaxLines(1)
				ui.Text(c, fmt.Sprintf("%d 分钟", artist.Ms/60000)).FontSize(12).TextColor(c.Theme().TextMuted)
			})
		}
	}
	if maxHour := state.hours[peak]; maxHour > 0 {
		sectionHeading(c, "一天里的收听时段", "")
		palette := phonePalette(c)
		for offset := 0; offset < len(state.hours); offset += 12 {
			ui.Row(c).FillWidth().Gap(3).Children(func() {
				for index, duration := range state.hours[offset : offset+12] {
					hour := index + offset
					color := palette.field.Mix(palette.accent, float32(duration)/float32(maxHour)*.66)
					ui.Box(c.Key(hour)).Grow(1).Basis(0).Height(28).Radius(5).Center().Background(color).
						Role(ui.RoleImage).Label(fmt.Sprintf("%02d 时，收听 %d 分钟", hour, duration/60000)).Children(func() {
						ui.Text(c, fmt.Sprintf("%02d", hour)).FontSize(11)
					})
				}
			})
		}
	}
	sectionHeading(c, "本周单曲循环", "")
	for _, row := range stats.TopTracks(5) {
		item, err := app.ItemOfStatsKey(row.Key, state.tracks[row.Key])
		var play func()
		if err == nil {
			play = func() { s.play([]app.Item{item}, 0) }
		}
		libraryRow(c.Key(row.Key), item.Cover, "disc", row.Title, fmt.Sprintf("本机播放 %d 次", row.Plays), play)
	}
}

func summarizeLibraryStats(stats app.ListeningStats, now time.Time) *libraryStatsState {
	state := &libraryStatsState{tracks: make(map[string]*app.StatsTrackStat), artists: make(map[int64]*app.StatsArtistStat)}
	first := now.AddDate(0, 0, -6).Format("2006-01-02")
	state.previous = stats.DayRangeMs(now.AddDate(0, 0, -13).Format("2006-01-02"), now.AddDate(0, 0, -7).Format("2006-01-02"))
	days := [...]string{"周日", "周一", "周二", "周三", "周四", "周五", "周六"}
	newUsers := make(map[int64]bool)
	knownUsers := make(map[int64]bool)
	for _, track := range stats.Tracks {
		if track != nil && track.ArtistMid != 0 && track.FirstHeard != "" && track.FirstHeard < first {
			knownUsers[track.ArtistMid] = true
		}
	}
	for index := range state.days {
		date := now.AddDate(0, 0, index-6)
		key, name := date.Format("2006-01-02"), days[date.Weekday()]
		if index == 6 {
			name = "今天"
		}
		report := stats.Days[key]
		state.days[index].name = name
		if report == nil {
			continue
		}
		state.days[index].minutes = report.Ms / 60000
		state.total += report.Ms
		state.plays += report.Plays
		for hour, duration := range report.HourlyMs {
			state.hours[hour] += duration
		}
		for id, track := range report.Tracks {
			if track == nil {
				continue
			}
			if state.tracks[id] == nil {
				state.tracks[id] = &app.StatsTrackStat{Title: track.Title, Artist: track.Artist, ArtistMid: track.ArtistMid, Local: track.Local, FirstHeard: track.FirstHeard}
			}
			state.tracks[id].Ms += track.Ms
			state.tracks[id].Plays += track.Plays
			if track.ArtistMid != 0 {
				if state.artists[track.ArtistMid] == nil {
					state.artists[track.ArtistMid] = &app.StatsArtistStat{Name: track.Artist}
				}
				state.artists[track.ArtistMid].Ms += track.Ms
				state.artists[track.ArtistMid].Plays += track.Plays
				if !knownUsers[track.ArtistMid] {
					newUsers[track.ArtistMid] = true
				}
			}
		}
	}
	state.newUsers = len(newUsers)
	return state
}
