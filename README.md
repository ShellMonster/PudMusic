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

| 格式 | 谁在解 | 备注 |
|---|---|---|
| FLAC（裸流 `.flac`） | `internal/service/audio/flacdec` | 自带解码器，已逐样本验证 |
| FLAC（MP4 容器里的） | 同上 | B 站的 FLAC 音轨是这种 |
| MP3 | `mygo/audio/mp3` | MyGo 自带 |
| AIFF / AIFC | `internal/service/audio/aiff.go` | 本项目写的，只认未压缩的 PCM |
| WAV | `mygo/audio` | MyGo 自带 |

**AAC 留到下一阶段**（走 macOS AudioToolbox / Windows MediaFoundation）。

FLAC 解码器是 t-0017 验证过的实现，本仓库里带着它的 64 项测试和测试素材
（`internal/service/audio/flacdec/`）。搬进来时只改了 import 路径，代码逐字未动。

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

还有一条是 MyGo 文档里没写的：**滑块的「正在被拖」要用 `Pressed()`，不能用 `Dragging()`。**
`Dragging()` 报的是 `Drag`/`Draggable` 两套机制，普通滑块对它永远是 `false`
（MyGo 自带的 `examples/timeline` 用的就是 `Dragging()`，那一处的判断是失效的）。

## 版本

MyGo 记在 `go.mod` 里，基线 **v0.5.1**。**不锁死**：MyGo 是 pre-1.0、发布很快、
没有兼容性承诺。升级前请重跑这里的探针（本仓库的测试、`library/t-0018/` 的探针、
音频实验），确认计划依赖的 API 还在。
