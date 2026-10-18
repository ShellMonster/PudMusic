# Biu MyGo

用 [MyGo](https://github.com/egoist/mygo) 框架重写的 [Biu](https://github.com/wood3n/biu)
音乐播放器。界面是 MyGo 自己画的原生 UI（即时模式，**没有 webview**），音频在 Go 里解码。

这是一个**正在重写中的项目**。已经能跑的：扫码登录、首页三个 Tab 的推荐与新歌速递、
稍后再看与历史记录、收藏夹/合集/系列、UP 空间与动态流、搜索、下载与下载记录、
本地音乐、虚拟列表与音乐卡片、分页自动续拉、播放条与队列抽屉、
本地解码播放（FLAC / MP3 / WAV / AIFF / AAC / M4A）、B 站在线取流播放与在线音质覆盖、
自动更新、托盘与单实例与开机自启、系统媒体会话、网络封面缓存。

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

下载 `Biu MyGo <版本>.dmg`，把 Biu MyGo 拖到 Applications。

> **这个 dmg 没有公证（notarization）。**
> 首次打开时 Gatekeeper 会说「无法验证开发者」。**这是预期的，不是应用坏了**，两条路都行：
>
> - 右键点图标 → **打开** → 在弹窗里再点一次「打开」；或者
> - 命令行：`xattr -dr com.apple.quarantine "/Applications/Biu MyGo.app"`
>
> 之后这台机器不会再拦。要彻底消除它需要 Apple 开发者计划的 Developer ID 并走公证流程，
> 本项目**明确不做**（用户已定，见 `PLAN.md` §5.1 第 4 条）。

### Windows：跑安装包

下载 `Biu MyGo Setup <版本> amd64.exe`（或 `arm64`）并运行。它把应用装进
`%LOCALAPPDATA%\Programs\Biu MyGo`——**不需要管理员权限**，并加上开始菜单快捷方式、
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
| **Windows 上解不了 AAC / ALAC / M4A / M4S** | 也就是 B 站的有损音质（`dash.audio`，未登录最高 320K）那一档，以及所有 `.m4a` / `.m4s` / `.mp4`。这些格式走 macOS 自带的 AudioToolbox，**Windows 上没有对应的解码器**。这是一条**还没决定的路线选择**（走系统 MediaFoundation，还是换一个纯 Go 解码器），不是「来不及做」。⚠️ 后果要说明白：Windows 版目前只能播无损 FLAC、MP3、WAV、AIFF。 |
| **杜比音轨不能手动选** | 杜比全景声（`30250`）和杜比 Hi-Res（`30251`）没有放进设置里的音质档位（只有 自动 / 无损 / 320K / 128K / 64K）。自动档在 B 站只给杜比时会取它，能不能解出取决于系统解码器。 |
| **AIFF 只认未压缩的 PCM** | `internal/service/audio/aiff.go` 是本项目自己写的，只解未压缩的 PCM。AIFF 容器里的压缩编码（MAC3/IMA4 等）解不了。 |
| **FLAC 解码器在本仓库里** | 裸 FLAC 走 `internal/service/audio/flacdec`（纯 Go，已逐样本验证）；**MP4 容器里的 FLAC 走系统解码器**（因为那条路是按容器认的）。 |

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
| **变速倍率不落盘** | 退出应用就没了，和现在的音量一致（Biu 也把倍率和音量存在 `play-list-store` 里，不在 `app-settings.json`）。 |
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

两处曾经被写错过、现在由测试钉住的事实：

| 依赖 | 许可证 |
|---|---|
| `github.com/ebitengine/purego` | **Apache-2.0**（不是 MIT） |
| `github.com/hajimehoshi/go-mp3` | **Apache-2.0**（不是 MIT） |
| `github.com/tphakala/go-m4a` | MIT |
| `github.com/mewkiz/flac` | Unlicense |

---

## 支持哪些格式（macOS）

认容器的是**头几个字节**，不是扩展名——本地文件和 B 站在线流走的是同一个
`openDecoder`（`internal/service/audio/player.go`）。

| 容器 | 谁在解 | 备注 |
|---|---|---|
| 裸 FLAC（`.flac`） | `internal/service/audio/flacdec` | 自带解码器，已逐样本验证 |
| MP4 家族（`.m4a` / `.m4s` / `.mp4`） | `internal/service/audio/extaf` | 走 macOS AudioToolbox；**MP4 里的 FLAC 也走这里**。⚠️ **Windows 上解不了** |
| 裸 ADTS（`.aac`） | 同上 | 没有容器，头两字节就是同步字。⚠️ **Windows 上解不了** |
| MP3 | `mygo/audio/mp3` | MyGo 自带，两个平台都能解 |
| AIFF / AIFC | `internal/service/audio/aiff.go` | 本项目写的，只认未压缩的 PCM |
| WAV | `mygo/audio` | MyGo 自带 |

注册表认不出来、而头几个字节是 `fLaC` 的，落回 `flacdec`——B 站的无损流就是
裸 FLAC，而 MyGo 的表是对字面量做匹配的，给变长的 magic 注册不了。

**Windows 上只有 MP3 / WAV / FLAC / AIFF 这四行能用**（后三行是纯 Go 的，
不依赖系统解码器）。

FLAC 解码器是 t-0017 验证过的实现，本仓库里带着它的 64 项测试和测试素材
（`internal/service/audio/flacdec/`）。搬进来时只改了 import 路径，代码逐字未动。

## 图标

源图是 `resources/icon.png`（1024×1024，**由代码画出来**：`go generate ./internal/platform/`）。
`mygo build` 从它渲出完整尺寸集：

| 平台 | 尺寸 | 档数 |
|---|---|---|
| macOS `.icns` | 32 / 64 / 128 / 256 / 256 / 512 / 512 / 1024 | 8 |
| Windows `.ico` | 16 / 24 / 32 / 48 / 64 / 128 / 256 | 7 |

⚠️ **源图不能缩到 512。** 下采样是从源图做的，源图 512 的话 1024 那档就是插值出来的
模糊图——Biu 自己的 `electron/icons/logo.png` 就只有 512×512，这是它的缺陷，不要照抄。
`internal/platform/icon_test.go` 的 `TestTheSourceIconIsBigEnoughForEverySize` 守着下限。

## 在线音质

B 站在线流分两档路：**无损 FLAC**（`dash.flac`，要大会员）和**有损 AAC**
（`dash.audio`，最高 320K）。用户选的那一档在 `internal/service/bili/quality.go`
里翻译成 B 站的档位 id；播放条上歌名右边的徽章点一下就能换档，换完立刻按新档
重新取流、从原来的播放位置接着播（不需要切歌，也不需要重启）。
⚠️ 无损那一路在两个平台都能播；有损那一路在 Windows 上还解不了（见「已知边界」）。

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
7. **提交进仓库的图标和许可清单都是生成物。** 图案在 `platform.AppIcon` 里、
   清单在 `go list -deps` 的结果里。改了图案或依赖忘了重新生成，仓库里躺着的是
   旧文件，而**没有任何一处会报错**。`go generate ./internal/platform/ ./internal/licensing/`，
   `scripts/build.sh` 每次打包前都会跑一遍并检查 `git diff`。

还有两条是 MyGo 文档里没写的，都跟「正在被拖」有关，用错了都不报错、只是拖不动：

1. **不要用 `Dragging()` 判拖动。** 它报的是 `Drag`/`Draggable` 两套机制，
   普通滑块和普通控件对它永远是 `false`（MyGo 自带的 `examples/timeline`
   用的就是 `Dragging()`，那一处的判断是失效的）。
2. **`Pressed()` 还要求「指针仍在控件框内」。** 进度条那种轨道横跨整个行程、
   指针一直留在框里的控件用它没问题；**一条 8 像素宽的拖拽把手用它只能拖动
   16 像素就卡住**——指针往左挪 9 个像素就出框了，它当场变 `false`
   （`ui/input.go:1028`）。薄把手要用 `Dragged()` 判按着没有，再用
   `PointerPosition()` 取指针位置（侧栏那条把手就是这么做的，见
   `internal/ui/sidebar.go`）。

   顺带：**别拿 `Dragged()` 的位移累加**。`dragX` 要到一帧结束才清掉，而一帧里
   view 要跑好几遍（`ui/runtime.go:657`），每一遍读到的都是同一个位移，
   累加起来就是拖快好几倍。取绝对位置就没有这个问题。

## 版本

MyGo 记在 `go.mod` 里，基线 **v0.5.5**（2026-10-16 从 v0.5.1 升上来）。**不锁死**：
MyGo 是 pre-1.0、发布很快、没有兼容性承诺。升级前请重跑这里的探针（本仓库的
测试、`library/t-0018/` 的探针、音频实验），确认计划依赖的 API 还在。

**升到 v0.5.5 之后必读：`ui.Tester` 的钟站着不动了。** 动画、`Context.After`、
双击判定全走那只钟，只有测试自己调 `tt.Advance(d)` 它才走。所以「循环
`tt.Frame()` 等动画停下来」这类辅助函数会**静默失效**——钟不走，动画永远停在第一
帧，而「连着几帧不动」读到的恰恰是「不动」，于是它判定「停了」放行；接下来
`tt.Click` 照着窗口外的坐标点下去**照样返回 nil，什么也没发生**。症状是
「点了没反应」，不是「等不到」，很容易误判成控件坏了。

要等动画就走两步：**推钟**（`tt.Advance`）+ **问 `tt.FramePending()`**（view 还在
要帧吗），**不要数帧数**。照这么写的例子见 `internal/ui/settings_test.go` 的
`settleSettingsPage`。
