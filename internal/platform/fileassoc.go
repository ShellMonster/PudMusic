// 文件关联：系统把一个音频文件交给本应用时，本应用认不认它。
//
// # 这一层只做「扩展名 → 认 / 不认」的判断
//
// 「系统把文件交给应用」这个入口（`mygo.App.OnOpenFile`）在三个地方都要用
// 到同一份口径：
//
//   - `mygo.json` 的 `fileAssociations`：告诉系统本应用开哪些扩展名；
//   - 命令行参数那一条路：框架按 `fileAssociations` 过滤启动参数
//     （`mygo/deeplink.go` 的 `fileArgs`），扩展名不在里面的参数根本不会
//     投给 `OnOpenFile`；
//   - 界面上收到文件之后：认得就放，认不得就说一句人话。
//
// 三处各记一份的话，症状是「双击一个 .mp3 应用起来了但什么也没发生」——
// 因为系统按第一份登记、框架按第二份过滤、界面按第三份判断，三份对不上。
// 所以**只有这一份**，另外两处都从它派生。
//
// # 判据是「我们真放得出声」，不是「扫描白名单里有」
//
// ⚠️ **扫描白名单（`internal/service/localmusic.Extensions`）比这一份大**：
// 那里面有 `.wma`，而 WMA 我们压根没有解码器（PLAN §5.4 点名的那个缺口）。
// 拿扫描白名单当关联口径的话，系统会把 `.wma` 也交给本应用，而界面上
// 收到之后只能对它说「这个格式我们放不了」——用户双击一个歌却得到一句
// 「放不了」，那是比「双击没反应」更坏的一种体验。
//
// 所以这一份只收**当前真能解码**的扩展名。`.wma` 要进来，得先做完 WMA
// 解码支持（那是另一张票的事）。
//
// # 扩展名一律**不带点**
//
// MyGo 的 `FileAssociation.Ext` 明确要求不带点（`mygo@v0.6.4`
// `cmd/mygo/config.go:109-110`），配置校验器用 `extRe` 拒绝 `.mp3` 这类写法
// （同文件 `:242-249`）。所以这一份里存的是**不带点**的。
//
// ⚠️ **两边各写一份**，而 `mygo.json` 那一边是**手写的 JSON**（框架读它、
// 不读 Go 代码）：加了一个格式只改一边的话，症状是「格式框里能选、文件框
// 里选不到」或者反过来。所以「两边是同一份」这件事只能靠**一条测试钉住**
// （`TestMygoJSONDeclaresTheSameExtensions`）。
package platform

import (
	"path/filepath"
	"runtime"
	"slices"
	"strings"
)

// runtimeGOOS 是构建目标平台。**可替换**：测试要把「另一台上那份长什么样」
// 钉住——比如 macOS 上跑测试却要断「Linux 那份里没有 m4a」。
var runtimeGOOS = runtime.GOOS

// associatedAudioExtsAll 是**有解码器的全部**扩展名（**不带点**）。
//
// ⚠️ **这一份是「全部平台合起来放得起的」**，而**不是某一平台上登记的**：
// 登记的那一份在它之上还要按 GOOS 裁（见 `AssociatedAudioExts`）——Linux
// 上没有 AAC 解码器（`internal/service/audio/mfadec` 在非 Windows 上
// 返回 `ErrUnsupported`，而 `extaf` 只在 darwin 上有），所以 Linux 那份
// 里没有 m4a / aac。
//
// 每一项都对着一个**真的解码器**：
//
//   - WAV：MyGo 自带（`mygo/audio`）
//   - MP3：`internal/service/audio/mp3dec`
//   - 裸 FLAC：`internal/service/audio/flacdec`
//   - MP4 / AAC：系统解码器（macOS 的 AudioToolbox、Windows 的 Media Foundation）
//   - AIFF / AIFC：`internal/service/audio/aiff.go`
//   - Ogg Vorbis：`mygo/audio/vorbis`
//
// ⚠️ **Windows 上 AAC / M4A 接的是 Media Foundation，而那一档还没在真
// Windows 上验过**（README「已知边界」一节写着这件事）。这里仍然登记它们，
// 因为登记的是**格式**而验的是**平台**——真验出来不行的话，是要把这一项
// 拿掉还是要把 Media Foundation 那条路修好，那是另一张票的裁决。
var associatedAudioExtsAll = []string{
	"mp3", "flac", "wav", "m4a", "aac", "ogg", "aiff", "aif", "aifc",
}

// aacExts 是 **Linux 上没有解码器**的那一档（MP4 / AAC）。
//
// 判据和 `associatedAudioExtsAll` 里那一行对得上：MP4 / AAC 靠系统解码器
// （macOS 的 AudioToolbox、Windows 的 Media Foundation），而 Linux 上
// 两边都没有——`extaf` 只在 darwin 上有，`mfadec` 只在 windows 上有。
var aacExts = map[string]bool{"m4a": true, "aac": true}

// AssociatedAudioExts 是本应用向系统登记的音频扩展名（**不带点**）。
//
// ⚠️ **这一份是「系统交给我们的文件」的入口白名单**，
// 和 `internal/service/localmusic.Extensions`（扫描白名单）**不是同一份**：
// 那一份里有 `.wma`，而我们放不了它（见包头那一节）。
//
// ⚠️ **它按 GOOS 裁过**：Linux 上没有 AAC 解码器，于是 Linux 这一份里
// 没有 m4a / aac——登记了的话，桌面环境把 `.m4a` 列入本应用的「打开方式」，
// 用户双击得到的只有一句「这个格式我们放不了」。
var AssociatedAudioExts = associatedAudioExtsForGOOS(runtimeGOOS)

// associatedAudioExtsForGOOS 报某个目标平台上登记的那一份。
//
// ⚠️ **它和 `AssociatedAudioExts` 必须同源**：linux 上它就是裁掉 aac/m4a
// 之后的同一份，而**不是另写一份**——两份各记一份的代价是「mygo.json
// 加了一个格式，linux 那份忘了加」，而那个症状是「Linux 上双击新格式没
// 反应」。
func associatedAudioExtsForGOOS(goos string) []string {
	if goos == "linux" {
		out := make([]string, 0, len(associatedAudioExtsAll)-len(aacExts))
		for _, ext := range associatedAudioExtsAll {
			if aacExts[ext] {
				continue
			}
			out = append(out, ext)
		}
		return out
	}
	return associatedAudioExtsAll
}

// FrameworkAudioExts 是**框架按它过滤命令行**的那一份——`mygo.json` 的
// `fileAssociations`（mygo build 用 `-ldflags -X` 把它注进
// `packageFileExtensions`，`mygo/deeplink.go` 的 `fileArgs` 按它过滤
// 启动参数）。
//
// ⚠️ **它和 `AssociatedAudioExts` 不是同一份**：那一份按 GOOS 裁过
// （darwin 上有 m4a/aac），而这一份是 `mygo.json` 那份**交集**——所有
// 平台共用的那份（也就是 `associatedAudioExtsForGOOS("linux")`，最窄
// 的那份）。macOS / Windows 的 AAC 关联由 `macos.infoPlist` 和
// `windows.installerInclude` 补，**不经过** `fileArgs`——所以框架那一半
// 在 Windows 上也不会把 `.m4a` 投给 `OnOpenFile`（由界面那半边接住，
// 见 `internal/ui/openfile.go` 的 `handleLaunchArgs`）。
//
// ⚠️ **它只给「照抄框架判据」的那一处用**（`frameworkWouldDeliver`）：
// 界面判收不收用的是 `AssociatedAudioExts`——`IsAssociatedAudioExt` 别换。
var FrameworkAudioExts = associatedAudioExtsForGOOS("linux")

// IsFrameworkAudioExt 报这个扩展名是不是**框架会投给 `OnOpenFile`**的那一档。
//
// ⚠️ **判据是 `FrameworkAudioExts`，不是 `AssociatedAudioExts`**：Windows
// 上 `.m4a` 在后者里而不在前者里——框架不会投它（它由 `installerInclude`
// 的注册表项唤起、在 `handleLaunchArgs` 那一遍被我们自己认下来）。
func IsFrameworkAudioExt(ext string) bool {
	return slices.Contains(FrameworkAudioExts, normalizeExt(ext))
}

// IsAssociatedAudioExt 报这个扩展名（**可以带点、可以不带**）是不是本应用
// 登记的那一档。
//
// ⚠️ **判据是登记的那一份，不是扫描白名单**（理由见包头）。
//
// 收下带点的写法是因为调用方拿到的路径是「文件名」，扩展名那一截天然带着点
// （`filepath.Ext` 返回 `.mp3`）。而登记的那一份是不带点的（MyGo 的要求），
// 于是比较之前先把两边都归一化成**不带点**。
func IsAssociatedAudioExt(ext string) bool {
	return slices.Contains(AssociatedAudioExts, normalizeExt(ext))
}

// AudioExtOf 从一个路径（或文件名）里取出扩展名，归一化成**不带点**的小写。
//
// 没有扩展名时给空串（那不是一个关联格式，`IsAssociatedAudioExt` 会说 false）。
func AudioExtOf(path string) string {
	return normalizeExt(filepath.Ext(path))
}

// normalizeExt 把扩展名归一化成**不带点的小写**。
//
// 两头都要归一：调用方给的可能带点（`filepath.Ext` 的结果），登记的那一份
// 一定不带点（MyGo 的要求）。只归一头的话，比的就是「`.mp3`」和「`mp3`」——
// 而它们是同一个格式。
func normalizeExt(ext string) string {
	return strings.ToLower(strings.TrimPrefix(strings.TrimSpace(ext), "."))
}
