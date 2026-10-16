# Biu MyGo

用 [MyGo](https://github.com/egoist/mygo) 框架重写的 [Biu](https://github.com/wood3n/biu)
音乐播放器。界面是 MyGo 自己画的原生 UI（即时模式，**没有 webview**），音频在 Go 里解码。

当前是**阶段 1 的第一步**：打开一个本地文件，放出声音，进度条能拖。队列、列表、歌词、
封面、B 站接口都在后面的阶段。

## 跑起来

```sh
go run .                      # 起窗口
go test ./...                 # 全部测试（不需要窗口）
go test -race ./...           # 带竞态检测
go vet ./...
```

打包用 MyGo 自带的 CLI（需要把它加成 tool，见 `go.mod`）：

```sh
go tool mygo build
```

## 支持哪些格式

认容器的是**头几个字节**，不是扩展名——本地文件和 B 站在线流走的是同一个
`openDecoder`（`internal/service/audio/player.go`）。

| 容器 | 谁在解 | 备注 |
|---|---|---|
| 裸 FLAC（`.flac`） | `internal/service/audio/flacdec` | 自带解码器，已逐样本验证 |
| MP4 家族（`.m4a` / `.m4s` / `.mp4`） | `internal/service/audio/extaf` | 走 macOS AudioToolbox；**MP4 里的 FLAC 也走这里** |
| 裸 ADTS（`.aac`） | 同上 | 没有容器，头两字节就是同步字 |
| MP3 | `mygo/audio/mp3` | MyGo 自带 |
| AIFF / AIFC | `internal/service/audio/aiff.go` | 本项目写的，只认未压缩的 PCM |
| WAV | `mygo/audio` | MyGo 自带 |

注册表认不出来、而头几个字节是 `fLaC` 的，落回 `flacdec`——B 站的无损流就是
裸 FLAC，而 MyGo 的表是对字面量做匹配的，给变长的 magic 注册不了。

FLAC 解码器是 t-0017 验证过的实现，本仓库里带着它的 64 项测试和测试素材
（`internal/service/audio/flacdec/`）。搬进来时只改了 import 路径，代码逐字未动。

## 在线音质

B 站在线流分两档路：**无损 FLAC**（`dash.flac`，要大会员）和**有损 AAC**
（`dash.audio`，最高 320K）。用户选的那一档在 `internal/service/bili/quality.go`
里翻译成 B 站的档位 id；播放条上歌名右边的徽章点一下就能换档，换完立刻按新档
重新取流、从原来的播放位置接着播（不需要切歌，也不需要重启）。

## 怎么测

测试分两层，都不需要开窗口：

```sh
# 界面层：用 MyGo 的 Tester，view 不需要真窗口就能点、能拖
go test -v ./internal/ui/

# 业务层与解码：真解码器、真播放器，只是没有声卡时不出声
go test -v ./internal/app/ ./internal/service/audio/
```

音频相关的断言都只看**时间和位置**，不依赖「听得见」，所以在没有声卡的机器上也跑得过。

## 代码怎么分层

依赖单向，反向一律禁止：

```
internal/ui/        → internal/app/ → internal/service/ → internal/platform/
   画界面              全部状态         解码、播放         平台差异
（只有这一层 import mygo/ui）
```

`main.go` 只做一件事：建状态、建窗口、把两者接上。

## 踩过坑的规矩

写在代码里，也写在这里，因为它们都不是从代码本身能看出来的：

1. **`Window.Update(nil)` 会 SIGSEGV，进程直接死。** 改状态用 `Update(func)`，
   只刷画面用 `Invalidate()`。
2. **后台投递完状态必须叫醒界面。** 只投递不唤醒，界面永远停在旧帧。
   走 `ui.Services.Invalidate()`（见 `internal/ui/wake.go`）。
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
