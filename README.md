# BiuGo

用 [MyGo](https://github.com/egoist/mygo) 框架重写的 [Biu](https://github.com/wood3n/biu)
音乐播放器。界面是 MyGo 自己画的原生 UI（即时模式，**没有 webview**），音频在 Go 里解码。

这是一个**正在重写中的项目**。已经能跑的：扫码登录、首页三个 Tab 的推荐与新歌速递、
稍后再看与历史记录、收藏夹/合集/系列、UP 空间与动态流、搜索、下载与下载记录、
本地音乐（把音频文件拖进窗口即可导入）、虚拟列表与音乐卡片、分页自动续拉、播放条与队列抽屉、
本地解码播放（FLAC / MP3 / WAV / AIFF / AAC / M4A / OGG）、B 站在线取流播放与在线音质覆盖、
自动更新、托盘与单实例与开机自启、系统媒体会话、网络封面缓存（落在应用自己的缓存
目录里，重启之后不用重下；目录有字节上限，超了按「上次写下来的时间」淘汰最老的那几张）。

**页面不是空壳**：登记表里的 14 条路由中 13 条有自己那一页的内容，剩下那一条走的是
这一页自己的空状态——`/empty`（空状态落地页，按设计就该是空的）。哪一页有没有接上
真数据由 `internal/ui/realdata_test.go` 钉着，不是靠谁记得；而本文这一句里的那个
13 也由 `TestTheCountsInTheProseMatchTheCountsInTheCode` 回头核着——加了一页却忘了
改这三处说明，`go test` 会当场红。

排期与「明确不做」的完整清单在 `docs/research/PLAN.md`。⚠️ 那个目录被 `.gitignore`
排除在外，所以在本仓库里打不开——它是维护者本地的规划文档，不是随代码发的东西。

---

## 安装

### macOS：拖进「应用程序」

下载 `BiuGo <版本>.dmg`，把 BiuGo 拖到 Applications。

> **这个 dmg 没有公证（notarization）。**
> 首次打开时 Gatekeeper 会说「无法验证开发者」。**这是预期的，不是应用坏了**，两条路都行：
>
> - 右键点图标 → **打开** → 在弹窗里再点一次「打开」；或者
> - 命令行：`xattr -dr com.apple.quarantine "/Applications/BiuGo.app"`
>
> 之后这台机器不会再拦。要彻底消除它需要 Apple 开发者计划的 Developer ID 并走公证流程，
> 本项目**明确不做**（用户已定，见 `PLAN.md` §5.1 第 4 条）。

### Windows：跑安装包

下载 `BiuGo Setup <版本> amd64.exe`（或 `arm64`）并运行。它把应用装进
`%LOCALAPPDATA%\Programs\BiuGo`——**不需要管理员权限**，并加上开始菜单快捷方式、
「设置 → 应用」里能看到的卸载项，以及 `biu://` 链接的注册。

> **这个安装包没有代码签名。** SmartScreen 会说「Windows 已保护你的电脑」。
> 同样是预期的：点 **更多信息** → **仍要运行**。
> 要消除它需要一张代码签名证书，本项目**没有买**。

### 从源码跑 / 自己打包

```sh
go run .                      # 起窗口
go test ./...                 # 全部测试（不需要窗口）
go test -race ./...           # 带竞态检测
go vet ./...

scripts/build.sh              # 打 macOS dmg + Windows NSIS 安装包
scripts/build.sh darwin/arm64 # 只打一个目标
```

打包走 MyGo 自带的 CLI（`go tool mygo build`，`go.mod` 里的 `tool` 指令）。
打 Windows 安装包需要 NSIS：macOS 上 `brew install nsis`，Debian 上 `apt install nsis`。
`scripts/build.sh` 在缺 NSIS 时会**直接停下并说明**，而不是像框架那样只打一行提示
就跳过——否则脚本「成功」了而产物里没有安装包，要等下一次签收才发现。

---

## 已知边界

这一节**逐条**列出这个应用做不到的事。明确的不做 ≠ 静默降级：用户有权知道装上之后
会撞到什么。每一项都注明了为什么，以及哪些是「已决定不做」、哪些是「还没决定」。

### 音频格式

| 边界 | 说明 |
|---|---|
| **本地音乐不支持 WMA** | **明确的功能缺口。** Biu 的本地扫描白名单里有 `.wma`，本项目解不了它（还没有能用的纯 Go WMA 解码器）。扫到 `.wma` 会列出来但**播不了**——页面顶上会明写「这个目录里有我们还放不出的格式（.wma），它们会列出来但放不了。」，行里的作者那一格也写着「这个格式我们放不了」，不会让人以为文件坏了。这条是「决定还没做，但当前边界清楚」，不是「没排期」。将来要不要做还没拍板。 |
| **Windows 上有损那一档还没在真机上验过** | B 站的有损音质（`dash.audio`，最高 192K）那一档**接上了** Windows 自带的 Media Foundation（本仓库的 `internal/service/audio/mfadec`，`IMFTransform` + `CLSID_CMSAACDecMFT`，issue #253）——**但这条还没在真 Windows 上跑过一行**：验收它的那条 CI（`windows-aac-spike.yml`）的 runner 一直没接上（账号付款的问题），所以「能不能放、放在哪、多长」**都还没有真机上的数**。⚠️ 已知的两个限制：① **ALAC 解不了**（`.m4a` / `.m4s` / `.mp4` 里装的 ALAC 音轨），而 B 站不发 ALAC；② Windows 上**在线**的有损流要**等整个文件下完才开口**——拆分片 MP4 得先把每一个 `moof` 走一遍，而 `moof` 在文件末尾；macOS 上是边下边播。 |
| **杜比音轨不能手动选** | 杜比全景声（实测 id `30250`，`codecs` 是 `ec-3`）没有放进设置里的音质档位（只有 自动 / 无损 / 192K / 128K / 64K）。自动档在 B 站只给杜比时会取它，能不能解出取决于平台：macOS 的 AudioToolbox **解得了**，**Windows 上解不了**——Media Foundation 不带 E-AC-3，而且 Win11 24H2 移除了系统自带的 AC-3 解码器（再加上 Dolby 那边的 field-of-use 授权限制），所以这一档在 Windows 上是**划掉**的。⚠️ 「杜比 Hi-Res = `30251`」是**二手映射表**里的说法，不是公开资料的说法——公开资料（「视频伴音音质代码」一节）写的是 `30251` = **Hi-Res 无损**，实测它就是 `dash.flac` 那一档（编码 `fLaC`，见下一节）。 |
| **AIFF 只认未压缩的 PCM** | `internal/service/audio/aiff.go` 是本项目自己写的，只解未压缩的 PCM。AIFF 容器里的压缩编码（MAC3/IMA4 等）解不了。 |
| **FLAC 解码器在本仓库里** | 裸 FLAC 走 `internal/service/audio/flacdec`（纯 Go，已逐样本验证）；**MP4 容器里的 FLAC 在 macOS 上走系统解码器**（那条路是按容器认的），**在 Windows 上落回 `flacdec`**（那里没有系统解码器）。实测 B 站那条无损流是 `48000` / 2 声道 / **24 bit**，`flacdec` 解得动。 |

### 登录

| 边界 | 说明 |
|---|---|
| **只有扫码登录** | 密码登录和短信登录都要在 DOM 里跑极验验证码的第三方 JS，原生 UI 做不出来。**不提议为验证码嵌 webview**——那会推翻「整个应用不用 webview」的决定。 |

### 平台

| 边界 | 说明 |
|---|---|
| **首发只有 macOS 和 Windows** | Linux 不首发。 |
| **Windows 媒体浮窗没有封面** | Windows 的 SMTC 浮窗有歌名、艺人、进度条和播放键，但**没有专辑封面**——构造封面要走 `Windows.Storage.Streams` 的四个抄来的 IID，在 macOS 上一个都验不了，而抄错的后果是「静默地没封面，其他一切正常」。macOS 的浮窗有封面。 |
| **页面切换动画没有** | Biu 源码里已经自己注释掉了（它会导致切页重复渲染、数据请求 double）。现在是死设置，照抄「没有」。导入 Biu 配置时那个键仍被识别。 |
| **不上架 Mac App Store** | MyGo 不实现 App Sandbox，也不管理文件选择的 powerbox。要上架得自己补 sandbox + 文件选择授权。 |

### 音频质量之外的行为

| 边界 | 说明 |
|---|---|
| **B 站「音乐 MV」播不了** | 本质是视频。已批准延后，恢复条件见 `PLAN.md` §5.2。 |
| **变速倍率不落盘** | 退出应用就没了。**音量是落盘的**（用户设的那一档重启之后还在，静音是它自己的一格）——倍率没跟上。Biu 也把倍率和音量存在 `play-list-store` 里，不在 `app-settings.json`；我们把音量放进了设置，是为了不落盘的话每次启动都是 100%。 |
| **改倍率之后约 1 秒才听得出来** | MyGo 预解约一秒，改倍率之前那一秒的输出已经铺好了。这是框架缓冲的固有延迟。 |

### ⚠️ 这一条是**差异说明**，不是「不支持」

> **全屏播放器的毛玻璃效果与 Biu 原版不同。**
>
> Biu 用自研的 `glassmorphism.ts`：封面取主色 → 32×32 均值 → 按
> `deviceMemory`/`hardwareConcurrency` 降模糊 → 三层 `radial-gradient`。
>
> **这一版还没有毛玻璃。** 满屏播放器的背景现在是「封面铺满整窗 + 一层 55%
> 的纯黑压暗」（`internal/ui/fullplayer.go` 的 `fullScrim`）——那层压暗是必需的，
> 因为满屏的控件和字全是白的，Biu 那边靠三层渐变挡着，我们这边靠它。
>
> 毛玻璃本身（MyGo 的 `plugins/glass`，Liquid Glass，三端 shader + CPU 等价实现）
> 是用户 2026-10-15 拍板的取舍，**还没落地**；落地时同样**不自绘等效效果**。
>
> 所以**构图与质感必然不同**：MyGo 只有线性渐变，没有 `mix-blend-mode`。
> 按低端机分级降模糊那一套也不做（MyGo 没有对应的 API），用固定效果。
>
> 验收口径是「毛玻璃在、内容看得清、不影响读歌词、不影响操作」，
> **不是**「和 Biu 的截图逐像素一致」。

（上面四项取舍——下载视频、AIFF、Windows 任务栏缩略图播放键、毛玻璃——是用户
2026-10-15 逐项拍板的。前三项**要交付**，不在「不支持」里；第四项是这一条差异说明。）

---

## 许可

本项目是 Biu 的重写，沿用 Biu 的许可发布。

- **Biu 的著作权属于原作者 wood3n。** PolyForm Noncommercial 1.0.0 要求随软件分发时
  **逐字**保留 `Required Notice: Copyright (c) 2022–2025 wood3n`——它在 [`LICENSE`](LICENSE)
  的第 5 行，也随安装包发出去（`licenses/BIU-LICENSE.txt`）。
  要商业化必须先单独向原作者取得授权。
- **第三方依赖的许可证原文随包发出去**，完整清单见
  `resources/licenses/THIRD-PARTY-NOTICES.txt`（macOS 上在 app bundle 的
  `Contents/Resources/licenses/`，Windows 上在可执行文件旁边的 `licenses\`）。
- **本项目不含任何 LGPL 代码。** 清单不是照 `go.mod` 抄的，是照 `go list -deps` 的实际
  结果核出来的，四个构建目标都核；`go test ./internal/licensing/` 会在对不上时失败。

这些依赖的许可证（其中一处曾经被写错过、现在由测试钉住）：

| 依赖 | 许可证 |
|---|---|
| `github.com/ebitengine/purego` | **Apache-2.0**（不是 MIT） |
| `github.com/tphakala/go-m4a` | MIT |
| `github.com/mewkiz/flac` | Unlicense |
| `github.com/jfreymuth/oggvorbis` / `github.com/jfreymuth/vorbis` | MIT（Ogg Vorbis 解码器） |

另外还有一份**不在 `go list -deps` 里**的第三方代码：MP3 解码器
`tphakala/go-mp3`（**MIT**，不是 BSD-3）是 **vendor 进仓库**的
（`internal/service/audio/mp3dec/`），来路、版本和「为什么 vendor 而不是进
`go.mod`」见那一带的 `PROVENANCE.md`；它的许可证原文同样随包发出去
（`resources/licenses/github.com-tphakala-go-mp3-LICENSE.txt`）。

---

## 支持哪些格式

认容器的是**头几个字节**，不是扩展名——本地文件和 B 站在线流走的是同一个
`openDecoder`（`internal/service/audio/player.go`）。

| 容器 | 谁在解 | 备注 |
|---|---|---|
| 裸 FLAC（`.flac`） | `internal/service/audio/flacdec` | 自带解码器，已逐样本验证 |
| MP4 家族（`.m4a` / `.m4s` / `.mp4`） | `internal/service/audio/extaf`（macOS）/ `internal/service/audio/mfadec`（Windows，⚠️ **还没在真 Windows 上验过**） | macOS 走 AudioToolbox；**Windows 上 AAC 接的是 Media Foundation**（`mfadec`）。⚠️ **MP4 里的 FLAC 在 macOS 上也走 AudioToolbox，在 Windows 上落回 `flacdec`** |
| 裸 ADTS（`.aac`） | 同上 | 没有容器，头两字节就是同步字。⚠️ **Windows 上接的是 Media Foundation**（同样**还没在真 Windows 上验过**） |
| MP3 | `internal/service/audio/mp3dec` | 本仓库自己解（vendor 进来的 `tphakala/go-mp3`，MIT），两个平台都能解。补了 **MPEG-2.5**、**LAME gapless**（编码器延迟被裁掉，歌和下一首接得上），打开一个文件也不再全文件扫帧头 |
| Ogg Vorbis（`.ogg`） | `mygo/audio/vorbis` | MyGo 自带，纯 Go，两个平台都能解。⚠️ 那一行 blank import 在 `internal/service/audio/player.go`——少了它 `.ogg` 在本地音乐里**列得出来、点进去报「不支持这个音频格式」** |
| AIFF / AIFC | `internal/service/audio/aiff.go` | 本项目写的，只认未压缩的 PCM |
| WAV | `mygo/audio` | MyGo 自带 |

注册表认不出来、而头几个字节是 `fLaC` 的，落回 `flacdec`——那是**本地裸
`.flac` 文件**的路子（MyGo 的表是对字面量做匹配的，给变长的 magic 注册不了）。

⚠️ **B 站在线的无损流不是裸 FLAC。** 实测（2026-10-23，issue #252）它的头
16 字节是 `00 00 00 20 66 74 79 70 69 73 6f 35 00 00 00 01 69 73 6f 35`
——`ftyp` 盒，也就是 **MP4 里的 FLAC**（素材与形状说明见
`internal/service/bili/testdata/lossless/`）。所以在线的无损流走的是上表里
「MP4 家族」那一行（macOS 上是系统解码器；**Windows 上 AAC 接 Media Foundation，
而 MP4 里的 FLAC 落回 `flacdec`**），**不是** `flacdec` 的裸 FLAC 那一条。

**这个平台没有 AudioToolbox 时**（Windows），先问 **Media Foundation**
（`internal/service/audio/mfadec`）——它解 AAC（MP4 家族和裸 ADTS 两种容器）；
它认不出来（比如 MP4 里装的是 FLAC）才落回 `flacdec`——`flacdec` 能解 MP4
里的 FLAC，于是 B 站无损流在 Windows 上照样放得出来。

**Windows 上 MP3 / WAV / FLAC / OGG / AIFF 和 MP4 里的 FLAC 能用**（不依赖 macOS
的系统解码器）。**AAC（MP4 家族和裸 ADTS）接的是 Media Foundation**——⚠️ **那一档
还没在真 Windows 上验过**（验收它的那条 CI 的 runner 一直没接上，见「已知边界」）。

⚠️ **Windows 上还有一个代价**：Media Foundation 那条路靠 `go-m4a` 拆分片 MP4，
而 `go-m4a` 要先把每一个 `moof` 走一遍——`moof` 在文件末尾，所以**在线**的流
要等整个文件下完才开口（本地文件不用）。macOS 上那条路走 AudioToolbox，是
按需读的，边下边播。

FLAC 解码器是 t-0017 验证过的实现，本仓库里带着它的 64 项测试和测试素材
（`internal/service/audio/flacdec/`）。搬进来时只改了 import 路径，代码逐字未动。

## 图标

图案是 `internal/platform/icon-source.png`（1024×1024，一张挑定的满幅方图），
`resources/icon.png` 是它经 `platform.AppIcon` 缩放并切形之后的产物
（`go generate ./internal/platform/`）。`mygo build` 从后者渲出完整尺寸集：

| 平台 | 尺寸 | 档数 |
|---|---|---|
| macOS `.icns` | 32 / 64 / 128 / 256 / 256 / 512 / 512 / 1024 | 8 |
| Windows `.ico` | 16 / 24 / 32 / 48 / 64 / 128 / 256 | 7 |

⚠️ **图源不能缩到 512。** 下采样是从图源做的，图源 512 的话 1024 那档就是插值出来的
模糊图——Biu 自己的 `electron/icons/logo.png` 就只有 512×512，这是它的缺陷，不要照抄。
`internal/platform/icon_test.go` 的 `TestTheArtworkIsBigEnoughForEverySize` 守着下限。

⚠️ **macOS 不会替你切圆角。** Big Sur 改的是 Apple 自家那一批图标，而那道圆角是
**画进 .icns 里**的；第三方应用的图标形状完全由素材决定，MyGo 的 `pngToICNS` 也只
缩放、一个像素都不动。所以留白和圆角是 `fillIcon` 切的——macOS 图标网格那一格
（图案占 0.824，圆角半径 0.2237×图案边长）。图源因此必须是一张**铺到四个角**的
方图，`TestTheArtworkIsFullBleed` 钉着这一条。

⚠️ **托盘和应用是同一张图**（`TrayIconPNG` 就是 `AppIconPNG(32)`）。两份各画各的
必然漂移，而症状是「Dock 上是新图标、通知区域还是旧图案」——那种错只有用户看得见，
而没有任何一处会报错。

## 在线音质

B 站在线流分两档路：**Hi-Res 无损**（`dash.flac`，要大会员）和**有损 AAC**
（`dash.audio`，最高 192K）。用户选的那一档在 `internal/service/bili/quality.go`
里翻译成 B 站的档位 id；播放条上歌名右边的徽章点一下就能换档，换完立刻按新档
重新取流、从原来的播放位置接着播（不需要切歌，也不需要重启）。
⚠️ 无损那一路在两个平台都能播；有损那一路**在 Windows 上接上了**（Media
Foundation），但**还没在真 Windows 上验过**——见「已知边界」里那条。

## 怎么测

测试分两层，都不需要开窗口：

```sh
# 界面层：用 MyGo 的 Tester，view 不需要真窗口就能点、能拖
go test -v ./internal/ui/

# 业务层与解码：真解码器、真播放器，只是没有声卡时不出声
go test -v ./internal/app/ ./internal/service/audio/

# 打包层：图标尺寸集、第三方许可清单与实际构建依赖对不对得上
go test -v ./internal/platform/ ./internal/licensing/
```

音频相关的断言都只看**时间和位置**，不依赖「听得见」，所以在没有声卡的机器上也跑得过。

## 代码怎么分层

依赖单向，反向一律禁止：

```
internal/ui/        → internal/app/ → internal/service/ → internal/platform/
   画界面              全部状态         解码、播放         平台差异
（只有这一层 import mygo/ui）

internal/licensing/  → 不 import 任何 internal 包（只读 go.mod 和模块缓存）
internal/platform/cmd/, internal/licensing/cmd/   → 生成用的命令行，不进运行时
```

`main.go` 只做一件事：建状态、建窗口、把两者接上。

窗口有**两扇**：主窗口（1200×800）和迷你播放器（320×100 的透明置顶小窗，
`internal/ui/miniplayer.go`）。它们各有各的 `Shell`（一个外壳对一个窗口），
但**播放状态是同一份 `app.App`**——所以界面上不会显示不同，前提是改完状态之后
挨着两扇窗都唤醒（见下面第 2 条）。迷你播放器开着的时候主窗口是收起来的，两个
窗口互斥（Biu 也是这么干的）。

## 踩过坑的规矩

写在代码里，也写在这里，因为它们都不是从代码本身能看出来的：

1. **`Window.Update(nil)` 会 SIGSEGV，进程直接死。** 改状态用 `Update(func)`，
   只刷画面用 `Invalidate()`。
2. **后台投递完状态必须叫醒界面。** 只投递不唤醒，界面永远停在旧帧。
   走 `ui.Services.Invalidate()`（见 `internal/ui/wake.go`）。
   **而且要挨着每一扇窗叫醒**：现在有主窗口和迷你播放器两扇窗（都共用同一个
   `app.App`），所以走的是 `App.AddWake` 那一份挨个叫醒的列表，而每扇窗建外壳
   时登记自己那一条。只叫醒主窗口的话，迷你窗口停在旧内容（实测）。
3. **每个 view 外面包一层 `Guard`（recover）。** MyGo 的 `Dispatch`/`drain` 都没有
   recover，view 里 panic 会穿透 Cocoa 事件循环杀掉整个进程。
4. **单实例锁和退出回调都在 `mygo.App.Run()` 之前装。** 锁晚一步就是两个实例
   同时启动的竞态；回调晚一步，启动命令行里的 `biu://…` 会在它装好之前就被交付掉，
   点链接唤起应用什么都不会发生。
5. **建窗口只能走 `App.WhenReady`，不能直接在回调里 `NewWindow`。** `App.Run()`
   先把事件循环转起来，`applicationDidFinishLaunching` 才把 `ready` 置上；在这
   两件事之间主线程上 `NewWindow` 是**直接 panic** 的。第二个实例正好落在这一段
   里（它的回调走 `postMain`），症状是「图标连点两下，应用当场崩」。
6. **`mygo.App.RequestSingleInstanceLock()` 挡不住同时启动的两个实例**（实测
   20 轮 17 轮两个都拿到锁：两个都 Dial 失败 → 都删掉对方的 socket 文件 → 都
   Listen 成功）。所以 `internal/platform` 在它之上还压了一把 flock，见
   `single_instance.go` 的注释。
7. **提交进仓库的图标和许可清单都是生成物。** 图标图案在 `platform.AppIcon` 里
   （图源是它 `go:embed` 的 `internal/platform/icon-source.png`），清单在
   `go list -deps` 的结果里。换了图案或依赖忘了重新生成，仓库里躺着的是旧文件，
   而**没有任何一处会报错**。`go generate ./internal/platform/ ./internal/licensing/`，
   `scripts/build.sh` 每次打包前都会跑一遍并检查 `git diff`。
8. **应用数据目录的名字不跟显示名走。** 应用 2026-10 从 `Biu MyGo` 改名成
   `BiuGo`，而 `mygo.App.Path(mygo.PathUserData)` 默认是 `os.UserConfigDir()`
   拼**应用名**——跟着改名走的话，老用户升级之后设置、搜索历史、快捷键、歌词缓存、
   下载记录和界面状态会一起「消失」（数据没坏，只是不再有人去那个目录找，而症状
   是静默的）。所以 `internal/platform/datadir.go` 把目录**钉死**在旧名字上，
   `main()` 的第一句就调它。取舍与「什么时候该真搬」见那个文件顶上那一节。
   连带的后果：macOS 上用户数据仍然住在
   `~/Library/Application Support/Biu MyGo`，而应用叫 BiuGo。

还有两条是 MyGo 文档里没写的，都跟「正在被拖」有关，用错了都不报错、只是拖不动：

1. **不要用 `Dragging()` 判拖动。** 它报的是 `Drag`/`Draggable` 两套机制，
   普通滑块和普通控件对它永远是 `false`（MyGo 自带的 `examples/timeline`
   用的就是 `Dragging()`，那一处的判断是失效的）。
2. **`Pressed()` 还要求「指针仍在控件框内」。** 进度条那种轨道横跨整个行程、
   指针一直留在框里的控件用它没问题；**一条 8 像素宽的拖拽把手用它只能拖动
   16 像素就卡住**——指针往左挪 9 个像素就出框了，它当场变 `false`
   （`ui/input.go:1157`）。薄把手要用 `Dragged()` 判按着没有，再用
   `PointerPosition()` 取指针位置（侧栏那条把手就是这么做的，见
   `internal/ui/sidebar.go`）。

   顺带：**别拿 `Dragged()` 的位移累加**。`dragX` 要到一帧结束才清掉，而一帧里
   view 要跑好几遍（`ui/runtime.go:524`），每一遍读到的都是同一个位移，
   累加起来就是拖快好几倍。取绝对位置就没有这个问题。

## 版本

MyGo 记在 `go.mod` 里，基线 **v0.6.2**（2026-10-22 从 v0.5.5 升上来）。**不锁死**：
MyGo 是 pre-1.0、发布很快、没有兼容性承诺。升级前请重跑这里的探针（本仓库的
测试、`library/t-0018/` 的探针、音频实验），确认计划依赖的 API 还在。

**v0.5.5 起必读：`ui.Tester` 的钟站着不动了。** 动画、`Context.After`、
双击判定全走那只钟，只有测试自己调 `tt.Advance(d)` 它才走。所以「循环
`tt.Frame()` 等动画停下来」这类辅助函数会**静默失效**——钟不走，动画永远停在第一
帧，而「连着几帧不动」读到的恰恰是「不动」，于是它判定「停了」放行；接下来
`tt.Click` 照着窗口外的坐标点下去**照样返回 nil，什么也没发生**。症状是
「点了没反应」，不是「等不到」，很容易误判成控件坏了。

要等动画就走两步：**推钟**（`tt.Advance`）+ **问 `tt.FramePending()`**（view 还在
要帧吗），**不要数帧数**。照这么写的例子见 `internal/ui/settings_test.go` 的
`settleSettingsPage`。
