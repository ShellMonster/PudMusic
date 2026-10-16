//go:build windows && (amd64 || arm64)

// Windows 那一半的曲目信息：歌名、艺人、类型，写在浮窗和锁屏上显示的那一行。
//
// ⚠️ **这一份没有封面**，而 darwin 那一半有（`MPMediaItemArtwork`）。是刻意的：
//
// SMTC 的封面要走 `SystemMediaTransportControlsDisplayUpdater::put_Thumbnail`，
// 参数是一个 `IRandomAccessStreamReference`；从一段字节造出那个引用，Windows
// 只有一条路——`Windows.Storage.Streams.DataWriter`（`WriteBytes` 再
// `DetachStream`）或者 `Buffer` + `IBufferByteAccess`。那条路上的四个 IID
// （`IDataWriter`、`IRandomAccessStreamReferenceStatics`、`IActivationFactory`、
// `IRandomAccessStream`）在这台机器上一个都验不了——
//
//	抄错一个 → `QueryInterface` 返回 `E_NOINTERFACE` → 静默地没有封面，
//	而歌名、进度、按键全都正常。**没有症状可循的那种错。**
//
// 那一票（BMG-26）在 macOS 上能真机验，所以它敢写。现在写 Windows 的这一半，
// 值不值得为四个抄来的 GUID 冒「界面上完全看不出坏了」的风险，由真机那一轮
// 决定，不在这里赌。`Options.Fetch` 在 Windows 上因此用不上（见 media.go 里
// 那个字段的注释）。

package media

import "unsafe"

// applyMetadata 把歌名、艺人、类型写给系统。
//
// ⚠️ **每次都整份重写，不做「变了才写」**：这一段失败的表现是「浮窗上的
// 歌名停在上一首」，而缓存「上一次写了什么」就多一处会写错的状态——
// 缓存错了的症状是「这一次换歌系统没反应」，那比写得频繁糟得多。
// 写频繁的代价只是几十微秒的跨进程调用，而 `Session.Publish` 已经把频率
// 压住了（语义变化才发，见 media.go 的 shouldPublish）。
func (p *windowsPlatform) applyMetadata(smtc uintptr, n NowPlaying) {
	var raw uintptr
	if hresultFailed(comCall(smtc, smtcGetDisplayUpdater, uintptr(unsafe.Pointer(&raw)))) || raw == 0 {
		logOnce("系统媒体浮窗的曲目信息拿不到，浮窗上只有歌名之外的东西")
		return
	}
	updater := queryInterface(raw, iidDisplayUpdater)
	release(raw)
	if updater == 0 {
		logOnce("系统媒体浮窗的曲目信息接口不对（IID 抄错了？）")
		return
	}
	defer release(updater)

	// 没有歌就把浮窗上那一栏整个清掉。⚠️ 不清的话，关掉一首歌之后锁屏上会
	// 留着一首已经不存在的歌——而用户点它不会发生任何事。
	if n.State == StateNone {
		if hr := comCall(updater, updaterClearAll); hresultFailed(hr) {
			logOnce("清空浮窗失败", hresultError("ClearAll", hr))
			return
		}
		if hr := comCall(updater, updaterUpdate); hresultFailed(hr) {
			logOnce("浮窗清空后没生效", hresultError("Update", hr))
		}
		return
	}

	// 类型必设：系统按 Unknown 走会用另一套版式，症状是「标题在那儿但
	// 排版不对」（见 smtc_windows.go 里 playbackTypeMusic 那段）。
	comCall(updater, updaterPutType, uintptr(playbackTypeMusic))

	var rawMusic uintptr
	if hresultFailed(comCall(updater, updaterGetMusic, uintptr(unsafe.Pointer(&rawMusic)))) || rawMusic == 0 {
		logOnce("浮窗的音乐属性接口拿不到，浮窗上没有歌名和艺人")
		return
	}
	music := queryInterface(rawMusic, iidMusicProps)
	release(rawMusic)
	if music == 0 {
		logOnce("浮窗的音乐属性接口不对（IID 抄错了？）")
		return
	}
	defer release(music)

	putStr(music, musicPutTitle, n.Title)
	// 艺人**空着就不写**：本地文件永远没有艺人（见 app/media.go 里
	// setArtist 那段注释）。写一个空串的话浮窗上会多出「未知艺人」这类
	// 占位，而那是我们在替系统编内容。
	if n.Artist != "" {
		putStr(music, musicPutArtist, n.Artist)
		// AlbumArtist 和 Artist 一起写：SMTC 的歌信息是分开两格的，
		// 只写一格的话另一格显示上一首的。
		putStr(music, musicPutAlbumArtist, n.Artist)
	}

	// ⚠️ **这一句是这一整段的理由**：光改本地那几个属性系统看不见，
	// 必须显式推一次。漏了它的症状是「歌名和艺人永远停在第一首」，
	// 而进度条和播放状态都正常——最难归因的一种。
	if hr := comCall(updater, updaterUpdate); hresultFailed(hr) {
		logOnce("往浮窗推歌名失败", hresultError("DisplayUpdater.Update", hr))
	}
}

// putStr 往一个 `put_Xxx(HSTRING)` 上写一个字符串。
//
// ⚠️ **空串直接跳过**：HSTRING 可以是「空的」（句柄为 0），
// 而 `put_Title(空的)` 在 SMTC 上会把标题清成一个占位而不是真的清空，
// 浮窗上于是出现一行空白或者「未知」。不写的话系统保留上一次的——
// 那也是不对，但至少是「显示旧的」而不是「显示奇怪的东西」，而这一份
// 每次换歌都会被重新写一遍，旧的不会被留着。
func putStr(obj uintptr, slot int, s string) {
	if s == "" {
		return
	}
	h, err := winString(s)
	if err != nil {
		logOnce("造 HSTRING 失败", err)
		return
	}
	defer h.Free()
	comCall(obj, slot, h.Handle())
}
