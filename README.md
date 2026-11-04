# PudMusic · 布丁音乐

一款面向 macOS 和 Windows 的桌面音乐播放器。

PudMusic 使用 Go 和 [MyGo](https://github.com/egoist/mygo) 原生 UI 构建，在 [Biu](https://github.com/wood3n/biu) 的基础上重新实现并扩展。界面不依赖 WebView，音频播放链路使用 Go 解码器和操作系统音频能力。

> 项目仍在持续开发中。功能、平台支持和安装包状态会随版本变化。

**页面不是空壳**：应用里的 14 条路由有 13 条接上了真实内容，剩下的那一条是
`/empty`（空状态落地页，按设计就该是空的）。哪一页有没有接上真数据由
`internal/ui/realdata_test.go` 钉着，不是靠谁记得；而本文这一句里的那个 13 也由
同一条测试回头核着——加了一页却忘了改这几处说明，`go test` 会当场红。

[下载 Releases](https://github.com/ShellMonster/PudMusic/releases) · [报告问题](https://github.com/ShellMonster/PudMusic/issues)

## 界面预览

### 搜索、内容列表与播放条

<p align="center">
  <img src="docs/screenshots/readme/search-and-player.png" alt="布丁音乐搜索结果、侧栏与播放条" width="900">
</p>

### 迷你播放器

<p align="center">
  <img src="docs/screenshots/readme/mini-player.png" alt="布丁音乐迷你播放器" width="640">
</p>

截图来自当前版本的实际应用界面。展示内容仅用于说明界面，不代表推荐或授权任何具体音乐内容。

## 功能

### 播放与音频

- 播放 B 站音乐内容，支持登录后获取可用的在线音质档位。
- 支持 Hi-Res 无损和 AAC 等在线音质；切换音质后会重新取流并从原播放位置继续。
- 支持本地 FLAC、MP3、WAV、AIFF/AIFC、AAC、M4A 和 OGG Vorbis。
- 支持播放速度调整、音量均衡和睡眠定时。
- 重启后恢复播放队列、当前歌曲和播放位置，恢复后保持暂停。
- 缓存网络封面，减少重复下载。

### 音乐库与内容发现

- 推荐音乐首页、榜单和新歌速递。
- 搜索音乐内容。
- 浏览 UP 主空间、用户动态和我的关注。
- 管理收藏夹、合集和系列。
- 使用稍后再看和历史记录。
- 下载内容并查看下载记录。
- 将本地音频文件拖入窗口导入音乐。

### 队列与播放器

- 播放条和播放队列抽屉。
- 拖动调整队列顺序。
- 支持立即播放、下一首播放和添加到队列。
- 全屏播放器和迷你播放器。
- 歌词显示。

### 桌面集成

- 系统托盘或菜单栏入口。
- 单实例运行和开机自启。
- 系统媒体控制。
- 双击音频文件，或将文件拖到程序坞图标上打开。
- 支持 `biu://` 链接和常见音频文件关联。
- 自动更新。
- 常规、菜单、快捷键和代理设置。

## 支持平台

| 平台 | 架构 | 状态 |
|---|---|---|
| macOS | arm64 / amd64 | 支持 |
| Windows | amd64 / arm64 | 支持 |
| Linux | — | 当前不提供发行版本 |

macOS 当前提供 DMG 安装包，首次打开未公证的应用时可能需要在系统提示中确认打开。Windows 当前提供 amd64 和 arm64 安装包，安装包未进行代码签名，系统可能显示 SmartScreen 提示。

## 当前边界

- 当前只支持 B 站扫码登录。
- 本地暂不支持 WMA。
- AIFF/AIFC 目前只支持未压缩 PCM，压缩编码无法播放。
- MP4、M4A 或 MP4 相关容器中的 ALAC 音轨暂不支持。
- B 站音乐 MV 以视频为主，当前播放器面向音频播放。
- 杜比音轨不能手动选择；不同平台的系统解码能力也不同。
- Windows 的 AAC/有损在线音频路径依赖 Media Foundation，部分场景的实际播放表现可能与 macOS 不同。
- macOS 和 Windows 的系统媒体控件能力不同，例如 Windows 媒体浮窗当前不显示封面。
- 当前不提供 Mac App Store 版本。

## 技术栈

| 领域 | 技术 |
|---|---|
| 主要语言 | Go |
| 桌面 UI | [MyGo](https://github.com/egoist/mygo) 原生即时模式 UI |
| 应用打包 | MyGo CLI、macOS DMG、Windows NSIS 安装包 |
| 音频链路 | Go 解码器、macOS AudioToolbox、Windows Media Foundation |
| 平台集成 | 文件关联、URL Scheme、单实例、托盘/菜单栏、开机自启、系统媒体会话 |
| 在线内容 | B 站公开内容与扫码登录后的可用接口 |

主要开源依赖包括：

- `github.com/egoist/mygo`：原生 UI、应用生命周期和打包。
- `github.com/ebitengine/purego`：调用部分 macOS 系统 API。
- `github.com/mewkiz/flac`：FLAC 处理。
- `github.com/tphakala/go-m4a`：M4A/MP4 容器处理。
- `github.com/Eyevinn/mp4ff`：MP4/DASH 相关处理。
- Ogg Vorbis 相关库：OGG Vorbis 音频处理。
- 仓库内 vendor 的 `tphakala/go-mp3`：MP3 解码。

完整依赖和许可证信息见 [`NOTICE`](NOTICE)、[`LICENSE`](LICENSE) 和 [`resources/licenses/THIRD-PARTY-NOTICES.txt`](resources/licenses/THIRD-PARTY-NOTICES.txt)。

## 安装与运行

### 下载发行版

从 [Releases](https://github.com/ShellMonster/PudMusic/releases) 下载对应平台的安装包：

- macOS：DMG
- Windows：amd64 或 arm64 安装包

### 从源码运行

需要 Go 1.27 或更新版本：

```sh
go run .
```

### 构建与测试

```sh
go build ./...
go vet ./...
go test ./...
```

构建安装包：

```sh
scripts/build.sh
```

构建 Windows 安装包需要安装 NSIS。应用使用 MyGo CLI 进行跨平台打包。

## 项目来源与致谢

PudMusic 是对 [Biu](https://github.com/wood3n/biu) 的 Go 原生 UI 重构与扩展。项目保留了 Biu 作为音乐播放器的产品方向，并根据 MyGo 的原生桌面能力重新实现界面、播放链路和平台集成。

感谢以下项目和社区提供的基础能力与参考：

- [MyGo](https://github.com/egoist/mygo)：Go 原生桌面 UI 框架与应用打包工具。
- [Biu](https://github.com/wood3n/biu)：项目参考和重构来源。
- [mewkiz/flac](https://github.com/mewkiz/flac)：FLAC 处理。
- [tphakala/go-m4a](https://github.com/tphakala/go-m4a)：M4A/MP4 容器处理。
- [Eyevinn/mp4ff](https://github.com/Eyevinn/mp4ff)：MP4 相关处理。
- [ebitengine/purego](https://github.com/ebitengine/purego)：Go 与系统 API 的互操作支持。

使用、修改和分发本项目及其依赖时，请遵守各自的许可证要求。

## 许可证

本项目使用 PolyForm Noncommercial 1.0.0 发布。项目基于 Biu 的公开项目进行重构，Biu 的版权和许可证要求见 [`LICENSE`](LICENSE)。商业化使用需要另行取得原作者授权。

第三方依赖的许可证原文随项目和安装包提供，详见 [`NOTICE`](NOTICE) 与 [`resources/licenses/THIRD-PARTY-NOTICES.txt`](resources/licenses/THIRD-PARTY-NOTICES.txt)。
