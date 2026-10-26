# BiuGo

用 [MyGo](https://github.com/egoist/mygo) 框架重写的 [Biu](https://github.com/wood3n/biu)
音乐播放器。界面是 MyGo 自己画的原生 UI（即时模式，**没有 webview**），音频在 Go 里解码。

这是一个**正在重写中的项目**。已经能跑的：扫码登录、首页六个 Tab（为你推荐 / 鬼畜 / 流行 / 热榜 / 原创榜 / 最近播放）与新歌速递、
稍后再看与历史记录、收藏夹/合集/系列、UP 空间与动态流、搜索、下载与下载记录、
本地音乐（把音频文件拖进窗口即可导入）、虚拟列表与音乐卡片、分页自动续拉、播放条与队列抽屉
（队列里的歌能拖着重排）、
音量均衡（设置里的开关，默认关：按 EBU R128 积分响度统一到 -16 LUFS；本地文件后台整首量完再定增益，可抬可压但不削波，在线流只按开头 15 秒估、只压不抬）、睡眠定时（播放条上的月亮：15/30/45/60 分钟或放完这一首，一次性、不落盘）、
重启之后回到上次听到哪儿（队列、当前项和播放位置都落盘，恢复出来是**暂停**的）、
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

---

## 怎么跑 / 怎么构建

```sh
go run .                      # 起窗口
go test ./...                 # 全部测试（不需要窗口）
go vet ./...

scripts/build.sh              # 打 macOS dmg + Windows NSIS 安装包
scripts/build.sh darwin/arm64 # 只打一个目标
```

打包走 MyGo 自带的 CLI（`go tool mygo build`，`go.mod` 里的 `tool` 指令）。
打 Windows 安装包需要 NSIS：macOS 上 `brew install nsis`，Debian 上 `apt install nsis`。
`scripts/build.sh` 在缺 NSIS 时会**直接停下并说明**，而不是像框架那样只打一行提示
就跳过——否则脚本「成功」了而产物里没有安装包，要等下一次签收才发现。

验收命令（**默认不跑界面整包**，理由见 `AGENTS.md` §1）：

```sh
go build -trimpath -p 4 ./... && go vet -trimpath -p 4 ./... && go test -p 4 $(go list ./... | grep -v '/internal/ui$')
```

---

## 支持什么

- **本地音乐**：FLAC / MP3 / WAV / AIFF / AAC / M4A / OGG（容器→解码器的对应关系见
  `docs/experience/audio-stack.md`《支持哪些格式》）。
- **B 站在线取流播放**：Hi-Res 无损（`dash.flac`，要大会员）与有损 AAC（`dash.audio`，
  最高 192K）；播放条上歌名右边的徽章点一下就能换档，换完按新档重新取流、从原位置接着播
  （见 `docs/experience/audio-stack.md`《在线音质怎么换档》）。
- **播放**：播放条与队列抽屉（队列里的歌能拖着重排）、音量均衡、睡眠定时、变速倍率、
  自动更新、托盘与单实例与开机自启、系统媒体会话。
- **网络封面缓存**：落在应用自己的缓存目录里，重启之后不用重下；目录有字节上限，
  超了按「上次写下来的时间」淘汰最老的那几张。内存预算用 `BIU_IMAGE_CACHE_MB` 配
  （见 `docs/experience/architecture.md`《封面缓存预算》）。
- **跨平台**：macOS 与 Windows（首发不含 Linux）。

---

## 已知边界

这一节**逐条**列出这个应用做不到的事。明确的不做 ≠ 静默降级：用户有权知道装上之后
会撞到什么。每一项都注明了为什么，以及哪些是「已决定不做」、哪些是「还没决定」。

### 音频格式

| 边界 | 说明 |
|---|---|
| **本地音乐不支持 WMA** | **明确的功能缺口。** Biu 的本地扫描白名单里有 `.wma`，本项目解不了它（还没有能用的纯 Go WMA 解码器）。扫到 `.wma` 会列出来但**播不了**——页面顶上会明写「这个目录里有我们还放不出的格式（.wma），它们会列出来但放不了。」，行里的作者那一格也写着「这个格式我们放不了」，不会让人以为文件坏了。这条是「决定还没做，但当前边界清楚」，不是「没排期」。将来要不要做还没拍板。 |
| **Windows 上有损那一档还没在真机上验过** | B 站的有损音质（`dash.audio`，最高 192K）那一档**接上了** Windows 自带的 Media Foundation（本仓库的 `internal/service/audio/mfadec`，`IMFTransform` + `CLSID_CMSAACDecMFT`，issue #253）——**但这条还没在真 Windows 上跑过一行**：验收它的那条 CI（`windows-aac-spike.yml`）的 runner 一直没接上（账号付款的问题），所以「能不能放、放在哪、多长」**都还没有真机上的数**。⚠️ 已知的两个限制：① **ALAC 解不了**（`.m4a` / `.m4s` / `.mp4` 里装的 ALAC 音轨），而 B 站不发 ALAC；② Windows 上**在线**的有损流要**等整个文件下完才开口**——拆分片 MP4 得先把每一个 `moof` 走一遍，而 `moof` 在文件末尾；macOS 上是边下边播。 |
| **杜比音轨不能手动选** | 杜比全景声（实测 id `30250`，`codecs` 是 `ec-3`）没有放进设置里的音质档位（只有 自动 / 无损 / 192K / 128K / 64K）。自动档在 B 站只给杜比时会取它，能不能解出取决于平台：macOS 的 AudioToolbox **解得了**，**Windows 上解不了**——Media Foundation 不带 E-AC-3，而且 Win11 24H2 移除了系统自带的 AC-3 解码器（再加上 Dolby 那边的 field-of-use 授权限制），所以这一档在 Windows 上是**划掉**的。⚠️ 「杜比 Hi-Res = `30251`」是**二手映射表**里的说法，不是公开资料的说法——公开资料（「视频伴音音质代码」一节）写的是 `30251` = **Hi-Res 无损**，实测它就是 `dash.flac` 那一档（编码 `fLaC`，见 `docs/experience/audio-stack.md`）。 |
| **AIFF 只认未压缩的 PCM** | `internal/service/audio/aiff.go` 是本项目自己写的，只解未压缩的 PCM。AIFF 容器里的压缩编码（MAC3/IMA4 等）解不了。 |
| **FLAC 解码器在本仓库里** | 裸 FLAC 走 `internal/service/audio/flacdec`（纯 Go，已逐样本验证）；**MP4 容器里的 FLAC 在 macOS 上走系统解码器**（那条路是按容器认的），**在 Windows 上落回 `flacdec`**（那里没有系统解码器）。实测 B 站那条无损流是 `48000` / 2 声道 / **24 bit**，`flacdec` 解得动。 |

### 登录

| 边界 | 说明 |
|---|---|
| **只有扫码登录** | 密码登录和短信登录都要在 DOM 里跑极验验证码的第三方 JS，原生 UI 做不出来。**不提议为验证码嵌 webview**——那会推翻「整个应用不用 webview」的决定。 |
| **macOS 的登录态存在一个普通文件里，不是钥匙串** | **这是安全倒退，说在前面。** 凭据落在 `~/Library/Application Support/Biu MyGo/credentials/session.json`（目录 700、文件 600）。换来的好处是**钥匙串不再弹框**——2026-10-25 之前每重新构建一次就要授权一次（根因：ad-hoc 签名的 designated requirement 认死那**一个字节级二进制**，而钥匙串的分区列表只认 `teamid:` 或 `cdhash:`，自签证书也落在后者）。**代价有两条**：① 钥匙串那一层「别的程序读不到」没了——⚠️ 但说实话，**同一用户下的进程本来就能读你权限 600 的文件**（权限位拦的是**别的用户**，不拦同用户的别的进程），所以真正少掉的是「钥匙串会在你授权时提醒你一下」那一层；② **钥匙串里的凭据跟着账号走**（换机、重装系统后登录同一个 Apple ID 就带着），**文件不会**——**换机要重新扫码登录**。Windows 那侧一直是 DPAPI 加密的文件，本来就不弹框，**没动**。 |
| **凭据文件的权限被放宽了会被发现，并且自动修好** | 文件必须只有本人能读写（600），目录 700。**权限位不是保险栓，是提醒**：文件一旦变成 644，macOS **不会报任何错**——它照常读照常写，登录态照常能用，只是别人也能看了。所以每次读它都查一遍，发现组/其他用户有权限就**当场收回 600**，并在日志里记一笔（`~/Library/Logs/BiuGo/main.log`）。⚠️ **收回权限不会弄丢登录态**——内容照常读出来，登录态照常在。自己验：`chmod 644 "$HOME/Library/Application Support/Biu MyGo/credentials/session.json"` 之后启动应用——**登录态不会丢**，而 `ls -l` 会看到权限已经变回 600，那就是「被发现」的证据。 |

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
| **变速倍率落盘** | 用户设的倍率重启之后还在（`playbackRate`，0 或缺省按原速）。 |
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

## 文档索引

**给使用者**：上面几节就够（是什么 / 怎么跑 / 怎么构建 / 支持什么 / 已知边界 / 许可）。

**给改这个仓库的人**：

| 文档 | 里面是什么 | 什么时候看它 |
|---|---|---|
| [`AGENTS.md`](AGENTS.md) | **纪律**：验收命令、静默出错的几条铁律、项目不变量、收尾动作 | 每次 |
| [`DESIGN.md`](DESIGN.md) | **数值**：间距、圆角、列宽、配色等拍板过的设计口径 | 动界面摆布之前 |
| [`docs/agents/README.md`](docs/agents/README.md) | 干活细则的索引与三类文档的分工判据 | 不知道哪份文档该看哪一份时 |
| [`docs/agents/testing.md`](docs/agents/testing.md) | 验收命令、跑测试的口径、已知会飘的那几条 | 跑测试、判一条红是真的回归还是机器忙 |
| [`docs/agents/visual-review.md`](docs/agents/visual-review.md) | 视觉审查：跟什么比、量哪五项、留下什么 | 动界面摆布的票 |
| [`docs/agents/cleanup.md`](docs/agents/cleanup.md) | 收尾：gopls、清理顺序、临时文件放哪 | 收尾 |
| [`docs/agents/issue-tracker.md`](docs/agents/issue-tracker.md) | 票在哪、怎么开票、怎么改票面 | 开票、拆票 |
| [`docs/agents/ticket-template.md`](docs/agents/ticket-template.md) | 票面骨架 | 开票 |
| [`docs/agents/triage-labels.md`](docs/agents/triage-labels.md) | 五个 triage 角色与标签 | 看板分类 |
| [`docs/agents/domain.md`](docs/agents/domain.md) | 领域文档（`GLOSSARY.md` / `docs/adr/`）的现状 | 造新的领域概念时 |
| [`docs/experience/README.md`](docs/experience/README.md) | 项目经验的索引与逐条搬迁对照表 | 想知道某条规矩的来龙去脉 |
| [`docs/experience/interface-pitfalls.md`](docs/experience/interface-pitfalls.md) | 界面层静默出错的地方（主题 / panic / 唤醒 / 行 key / 拖动 / 测试的钟） | 动界面之前 |
| [`docs/experience/startup-and-artifacts.md`](docs/experience/startup-and-artifacts.md) | 启动顺序 / 单实例 / 生成物 / 许可清单 / 图标素材 | 动启动流程或打包时 |
| [`docs/experience/audio-stack.md`](docs/experience/audio-stack.md) | 解码器 / 在线音质换档 / 支持哪些格式 / 解不了的格式 | 动音频时 |
| [`docs/experience/data-and-credentials.md`](docs/experience/data-and-credentials.md) | 四个名字 / 用户数据目录 / 登录态 / 什么时候不许写兼容逻辑 | 动设置、登录态、用户数据目录时 |
| [`docs/experience/architecture.md`](docs/experience/architecture.md) | 分层与依赖方向 / 两扇窗 / 封面缓存预算 | 动包结构时 |
| [`docs/experience/upstream-biu.md`](docs/experience/upstream-biu.md) | 上游源码与截图怎么取 / 数值怎么对齐 / 刻意不同的地方 | 对着上游做东西时 |
| [`docs/experience/release-and-versioning.md`](docs/experience/release-and-versioning.md) | 版本三处对上 / 发版五步 / 打包签名 / MyGo 基线 | 发版时 |
| [`docs/experience/git-and-workflow.md`](docs/experience/git-and-workflow.md) | 提交信息 / 文件怎么拆 / 注释纪律 / worktree | 提交之前 |
