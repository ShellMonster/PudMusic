# 项目经验

这个目录装的是**「为什么是这样」**，不是「怎么办」。

- **规矩**（不许做什么、必须做什么）留在 `AGENTS.md`——那份是每次开工都要读的。
- **数值**（间距多大、圆角多少、列宽几格）在 `DESIGN.md`——那份是量东西时用的尺子。
- **细则**（派单怎么写、测试怎么判、收尾怎么清）在 `docs/agents/`。
- **知识**（当初为什么这么选、哪些地方静默出错、每个子系统怎么分层）在这里。

为什么单独开一个目录：这些坑的共同特征是**只有在你改到那块时才会咬你**。混在
`AGENTS.md` 里，每次都要读一遍规矩的人也被迫读一遍历史；合成一个大文件，则整份
被忽略 ⇒ 等于丢了。所以按主题拆开，**改到哪块读哪块**。

⚠️ 本目录里的每一段都带一行「**什么时候用得上**」——知识不进场景就等于没写。

---

## 目录里有什么

| 文档 | 一句话 | 什么时候读它 |
|---|---|---|
| [interface-pitfalls.md](interface-pitfalls.md) | 界面层那些「不报错、只是悄悄不对」的地方：主题、panic、唤醒、行 key、拖动、测试的钟 | 你要动 `internal/ui/` 的绘制、输入或 view 生命周期 |
| [startup-and-artifacts.md](startup-and-artifacts.md) | 启动顺序、单实例、生成物（图标 / 许可清单）、图标素材的规矩 | 你要动 `main.go`、`internal/platform/`、打包或换图标 |
| [audio-stack.md](audio-stack.md) | 解码器各自负责什么、在线音质怎么换档、哪些格式解不了、哪些还没在真机验过 | 你要动 `internal/service/audio/`、`internal/service/bili/` 的音质或容器 |
| [data-and-credentials.md](data-and-credentials.md) | 四个名字互不相干、用户数据目录为什么钉死、登录态为什么不放钥匙串、什么时候不许写兼容逻辑 | 你要动设置、登录态、用户数据目录、迁移 |
| [architecture.md](architecture.md) | 分层与依赖方向、两扇窗怎么共用一份状态、封面缓存的内存预算 | 你要加一个子系统，或者不清楚某段代码该放哪一层 |
| [upstream-biu.md](upstream-biu.md) | 上游 Biu 的源码和截图怎么取、哪些数值以上游为准、我们和上游刻意不同的地方 | 你在实现或对齐一个上游页面（UI 票尤其） |
| [release-and-versioning.md](release-and-versioning.md) | 版本号三处对上、发版五步、更新签名私钥、MyGo 版本基线 | 你要发版、升版本、升 MyGo |
| [git-and-workflow.md](git-and-workflow.md) | 提交信息、文件怎么拆、注释为什么不许写「还没做」、工作板与 worktree 怎么配合 | 你要提交、拆文件、或第一次在这个仓库里干活 |

---

## 为什么是 8 份而不是 7 份

早一版的设计稿（BMG-287 报告 §2.6）给的是 7 份。本目录把**发版与版本**从
「工程与协作」里单独拆出来，成了 8 份——理由是这两件事的**读者和时机都不同**：

- 「提交信息、文件怎么拆」是**每次提交前**要想到的；
- 「发版五步、私钥在哪」是**一年可能碰一次**、而碰错代价极大（私钥丢了就再也发不了
  更新）的东西。

混在一份里，那份就得一年里被反复打开，而里面最要紧的那几行会被淹没。

---

## 搬迁对照表

这一节是给**下一个人**看的：这些内容原来在哪儿、现在在哪儿、为什么搬。
它同时是「有没有漏」的核对表——搬一处留一处出处，漏了能看出来。

> 「留在原处」= 这条**纪律本体**留在源文件里（`AGENTS.md` / `README.md`），
> 而**细节**搬到了下表右边那一份文档里。
>
> ⚠️ **原票（BMG-289）只搬不删**：搬完之后源文件里那份**还在**，两处并存。
> **删掉源文件里那份是后续那票（BMG-290）的活**——那一票把 `AGENTS.md` 与 `README.md`
> 瘦身成「只剩每次都要读的东西」，知识层的内容全部只留在这份目录里。
>
> ✅ **BMG-290 已经做完了**，源文件里那份已删：
>
> - `AGENTS.md`：**11 章编号一个没少**，每章压成「每次都要读的纪律」，
>   知识与细则下沉到本目录与 `docs/agents/`，文件从 39 KB 降到 12 KB。
> - `README.md`：只留「是什么 / 怎么跑 / 怎么构建 / 支持什么 / 已知边界 / 许可 /
>   文档索引」，下面那几节**整节删掉**（知识只留在本目录）：
>   「封面内存缓存」「支持哪些格式」「图标」「在线音质」「怎么测」「代码怎么分层」
>   「踩过坑的规矩」「版本」。安装步骤、已知边界的表格本体、许可与著作权声明**留在
>   README**（那是给用户看的）。
> - `DESIGN.md`：**一个字没动**（它被 12 处按章节号引用）。

### 从 `AGENTS.md` 搬来的

> **38 条**，与收尾说明里的落位表一一对应。**§1~§11 每一章都有条目**——
> 一章都没有落掉，才叫「一条不落」。

| # | 原位置 | 内容 | 新位置（文档 § 小节） | 纪律留在源文件？ |
|---|---|---|---|---|
| 1 | §1 | `-trimpath` 为什么（缓存跨 worktree 命中、实测 38 包 / 41 MB） | `docs/agents/testing.md` §《编译缓存与 `-trimpath`》 | — |
| 2 | §1 | **三个硬条件**（① `test` 不带 `-trimpath` ② 不许 `GOFLAGS=-trimpath` ③ 全板一致，含「同一个提交里改完 `AGENTS.md` + `DESIGN.md` + 全部 open 票面 + 板子的 check 命令」） | **纪律留在 `AGENTS.md` §1**；理由在 `docs/agents/testing.md` §《编译缓存与 `-trimpath`》里的《三个硬条件，踩了更糟》 | ✅ |
| 3 | §1 | `-p 4` 为什么限并发（机器主人自己也要用） | `docs/agents/testing.md` §《`-p 4` 是限并发用的》 | ✅ |
| 4 | §1 | 默认不跑 `internal/ui` 的理由（一轮 62~298 秒、绿只证明画面没坏） | `docs/agents/testing.md` §《默认不跑 `internal/ui`》 | ✅ |
| 5 | §1 | 为什么不带 `-race`（十几到二十分钟，评审会超时） | `docs/agents/testing.md` §《不带 `-race`》 | ✅ |
| 6 | §1 视觉审查 ① | 上游源码怎么取、上游截图只有六张且是旧版 | `docs/experience/upstream-biu.md` §《上游源码与截图怎么取》 | — |
| 7 | §1 视觉审查 ①③④ | 跟什么比 / 比哪五项 / 留下什么 / 不合格就继续改 | `docs/agents/visual-review.md` §《① 跟什么比》…§《④ 不合格就继续改》 | ✅ |
| 8 | §1 视觉审查 ⚠️ | 不要要求逐像素比（出图本身不确定、要比得先跑两遍 HEAD） | `docs/agents/visual-review.md` §《逐像素比这件事》 | ✅ |
| 9 | §1 视觉审查 | 票 #418 的翻车（行距当列距，票 #479 修的） | `docs/agents/visual-review.md` §《一次真实的翻车（票 #418）》 | — |
| 10 | §1 视觉审查 | 上游 `ROW_GAP` 的两个角色（行距 vs 列距） | `docs/experience/upstream-biu.md` §《数值怎么对齐》 | — |
| 11 | §2 | `waitFor` / `waitFrames` / 推钟 + `FramePending` / load < 8 / 磁盘 5 GB / `diskguard` / 不许清缓存 / 加压 3 次 / F12 / 每帧分配 / `gofmt` | `docs/agents/testing.md` §《跑测试的几条口径》 | ✅ |
| 12 | §2 | 已知会飘的名单（3 条 + 1 条待复核 + 2 条已划掉）、怎么自己核这份名单 | `docs/agents/testing.md` §《已知会飘的那几条》 | ✅ |
| 13 | §3 | 改了对一件事的裁决必须同步改 GitHub issue 的验收标准（`gh issue edit`）；为什么（对话里判的事只有你知道，评审不知道；栽过两次） | **纪律留在 `AGENTS.md` §3**；操作细则 `docs/agents/issue-tracker.md` §《Conventions》《Ticket template》 | ✅ |
| 14 | §3 | 自动关票不归你管：板子按 `merge.closes` 在开 PR 之前往正文加一行 `Closes #<票号>`；没 PR 就合并后自己 `gh issue close` | **纪律留在 `AGENTS.md` §3**；背景 `docs/experience/git-and-workflow.md` §《工作板与 worktree》 | ✅ |
| 15 | §3 | 不要手写 `Closes #N`（连反引号里也不行；GitHub 不认代码块里的关闭关键字；2026-10-24 漏过一张 #244）；**关不掉的最常见原因是时序**（`merge.closes` 必须在 PR 开出来之前配好）；票号写错也关不掉（板子只关 `merge.closes` 里列的那几张） | **纪律留在 `AGENTS.md` §3**；背景 `docs/experience/git-and-workflow.md` §《提交信息》 | ✅ |
| 16 | §4 界面层 1 | `SetTheme` 每帧在 view 最开头调 | **纪律留在 `AGENTS.md` §4**；背景 `docs/experience/interface-pitfalls.md` §《主题色》 | ✅ |
| 17 | §4 界面层 2 | view 里 panic 会杀进程 ⇒ `ui.Guard` | **纪律留在 `AGENTS.md` §4**；背景 `docs/experience/interface-pitfalls.md` §《panic》 | ✅ |
| 18 | §4 界面层 3 | 投递完状态要挨着每扇窗叫醒；`Update(nil)` SIGSEGV；看不见的窗丢帧是设计 | **纪律留在 `AGENTS.md` §4**；背景 `docs/experience/interface-pitfalls.md` §《唤醒》 | ✅ |
| 19 | §4 界面层 4 | 动态列表行要有稳定且唯一的 key | **纪律留在 `AGENTS.md` §4**；背景 `docs/experience/interface-pitfalls.md` §《行 key》 | ✅ |
| 20 | §4 拖动两条 | `Pressed()` / `Dragged()` / `Dragging()` 各自的适用范围 | **纪律留在 `AGENTS.md` §4**；背景 `docs/experience/interface-pitfalls.md` §《拖动》 | ✅ |
| 21 | §4 启动 5 | `App.WhenReady` 之前不能 `NewWindow` | **纪律留在 `AGENTS.md` §4**；背景 `docs/experience/startup-and-artifacts.md` §《建窗口的时机》 | ✅ |
| 22 | §4 启动 6 | 单实例锁与退出回调要在 `Run()` 之前 | **纪律留在 `AGENTS.md` §4**；背景 `docs/experience/startup-and-artifacts.md` §《单实例与退出回调》 | ✅ |
| 23 | §4 启动 7 | `RequestSingleInstanceLock()` 挡不住（20 轮 17 轮）⇒ 内核独占锁 | **纪律留在 `AGENTS.md` §4**；背景 `docs/experience/startup-and-artifacts.md` §《单实例与退出回调》 | ✅ |
| 24 | §4 启动 8 | 图标与第三方许可清单是生成物 | **纪律留在 `AGENTS.md` §4**；背景 `docs/experience/startup-and-artifacts.md` §《生成物》 | ✅ |
| 25 | §4 启动 9 | 用户数据目录的名字不跟显示名走 | **纪律留在 `AGENTS.md` §4**；背景 `docs/experience/data-and-credentials.md` §《用户数据目录》 | ✅ |
| 26 | §5 | 四个名字互不相干（显示名 / bundle id / 钥匙串服务名 / 用户数据目录名） | **纪律留在 `AGENTS.md` §5**；背景 `docs/experience/data-and-credentials.md` §《四个名字》 | ✅ |
| 27 | §5 | FLAC 解码器是逐样本验证过的资产 | **纪律留在 `AGENTS.md` §5**；背景 `docs/experience/audio-stack.md` §《FLAC 解码器》 | ✅ |
| 28 | §5 | 动 macOS 音频路径前先读 `extaf/` 的注释 | **纪律留在 `AGENTS.md` §5**；背景 `docs/experience/audio-stack.md` §《macOS 那条路：AudioToolbox》 | ✅ |
| 29 | §5 | 不为「我们自己的老状态」写兼容逻辑 + 三条例外 | **纪律留在 `AGENTS.md` §5**；背景 `docs/experience/data-and-credentials.md` §《什么时候该写兼容逻辑》 | ✅ |
| 30 | §6 | 一律走 fixture、真机回归单独立项 | **纪律留在 `AGENTS.md` §6**；背景 `docs/experience/data-and-credentials.md` §《别打真 B 站》 | ✅ |
| 31 | §7 | 注释说谎比没有注释更坏（审计查出过至少五条） | **纪律留在 `AGENTS.md` §7**；背景 `docs/experience/git-and-workflow.md` §《注释的纪律》 | ✅ |
| 32 | §8 | 提交信息：中文一句话 + 票号；不写 `Closes #N` | **纪律留在 `AGENTS.md` §8**；背景 `docs/experience/git-and-workflow.md` §《提交信息》 | ✅ |
| 33 | §9 | `GLOSSARY.md` / `docs/adr/` 不存在、`docs/research/` 不进版本库 | **纪律留在 `AGENTS.md` §9**；背景 `docs/experience/git-and-workflow.md` §《仓库里哪些东西不是文档》 | ✅ |
| 34 | §9 | 回归证据是代码，不是文档 | **纪律留在 `AGENTS.md` §9**；背景 `docs/experience/architecture.md` §《回归证据是代码，不是文档》 | ✅ |
| 35 | §10 | gopls 收尾：为什么、怎么做（按 cwd 认领）、清理的顺序 | **纪律留在 `AGENTS.md` §10**；细则 `docs/agents/cleanup.md` §《为什么》《怎么做》《清理的顺序》 | ✅ |
| 36 | §11 | 文件怎么组织：什么时候该拆 / 怎么拆 / 什么时候别拆 | **纪律留在 `AGENTS.md` §11**；背景 `docs/experience/git-and-workflow.md` §《文件怎么组织》 | ✅ |
| 37 | §1 里 `-trimpath` 的理由 | 为什么全板必须一致（半开半开 ⇒ 缓存翻倍到 518 MB） | `docs/experience/git-and-workflow.md` §《为什么 `-trimpath` 是全板一致的》 | — |
| 38 | 开头 | 「只写从代码里看不出来、踩过才知道的东西」 | `docs/agents/README.md` §《规矩 vs 知识 vs 数值》 | — |

### 从 `README.md` 搬来的

> **21 条**，与收尾说明里的落位表一一对应。

| # | 原位置 | 内容 | 新位置（文档 § 小节） | 留在源文件？ |
|---|---|---|---|---|
| 1 | 「安装」（macOS / Windows） | dmg 没有公证、安装包没有代码签名、用户第一次打开会看到什么 | **保留在 `README.md`《安装》**（用户看的）；背景 `docs/experience/release-and-versioning.md` §《打包与签名》 | ✅ |
| 2 | 「从源码跑 / 自己打包」 | NSIS 缺失时脚本要停下；打包走 `go tool mygo build` | **保留在 `README.md`《怎么跑 / 怎么构建》**；背景 `docs/experience/release-and-versioning.md` §《打包与签名》 | ✅ |
| 3 | 「封面内存缓存」 | `BIU_IMAGE_CACHE_MB` 的档位与取舍、96 → 32 MiB 堆峰值实测、解码路径 | `docs/experience/architecture.md` §《封面缓存与内存预算》（README 只留一行指向） | — |
| 4 | 「已知边界 → 音频格式」 | WMA / AIFF / ALAC / Windows AAC 未验 / 杜比 id / FLAC 在哪个平台走哪条路 | **表格本体保留在 `README.md`**；背景 `docs/experience/audio-stack.md` §《解不了 / 没验过的几处》《支持哪些格式》 | ✅ |
| 5 | 「已知边界 → 登录」 | 登录态为什么不在钥匙串、权限被放宽会自愈、换机要重新扫码 | **表格本体保留在 `README.md`**；背景 `docs/experience/data-and-credentials.md` §《登录态》 | ✅ |
| 6 | 「已知边界 → 平台」 | 首发只有 macOS/Windows、Windows 媒体浮窗没封面、页面切换动画照抄「没有」、不上架 MAS | **表格本体保留在 `README.md`**；背景 `docs/experience/upstream-biu.md` §《我们和上游刻意不同的地方》 | ✅ |
| 7 | 「已知边界 → 音频质量之外的行为」 | 音乐 MV 播不了、变速倍率落盘、改倍率约 1 秒才听得出来 | **表格本体保留在 `README.md`**；背景 `docs/experience/audio-stack.md` §《音频之外的几条已知边界》 | ✅ |
| 8 | 「已知边界 → 差异说明」 | 为什么这一版没有毛玻璃、验收口径不是逐像素 | **保留在 `README.md`**；背景 `docs/experience/upstream-biu.md` §《我们和上游刻意不同的地方》 | ✅ |
| 9 | 「许可」 | 许可清单是三道会红的断言、为什么不读 `go.mod`、三份不在 `go list -deps` 里的东西、purego 是 Apache-2.0 / go-mp3 是 MIT | **声明保留在 `README.md`《许可》**；背景 `docs/experience/startup-and-artifacts.md` §《许可清单是三道会红的断言》 | ✅ |
| 10 | 「支持哪些格式」 | 容器 → 解码器对照表、B 站无损是 MP4 里的 FLAC | `docs/experience/audio-stack.md` §《支持哪些格式》（README 只留一行指向） | — |
| 11 | 「图标」 | 图源不能缩到 512、macOS 不替你切圆角、托盘与应用同一张图、图源两种由代码自己判 | `docs/experience/startup-and-artifacts.md` §《图标素材的规矩》 | — |
| 12 | 「在线音质」 | 两档路、档位 id 在哪翻译、徽章换档的行为、编号上错过两次 | `docs/experience/audio-stack.md` §《在线音质怎么换档》（README 只留一行指向） | — |
| 13 | 「怎么测」 | 测试分哪三层、分别跑哪几个包 | `docs/agents/testing.md` §《测试分三层》 | — |
| 14 | 「代码怎么分层」 | 依赖单向图、`main.go` 只做一件事、两扇窗共用一份状态 | `docs/experience/architecture.md` §《依赖单向》《`main.go` 只做一件事》《两扇窗》 | — |
| 15 | 「踩过坑的规矩」1–3 | `Update(nil)` SIGSEGV、投递后要唤醒、view 包 `Guard` | `docs/experience/interface-pitfalls.md` §《唤醒》《panic》 | — |
| 16 | 「踩过坑的规矩」4–7 | 单实例锁与退出回调、`WhenReady`、`RequestSingleInstanceLock`、生成物 | `docs/experience/startup-and-artifacts.md` §《单实例与退出回调》《建窗口的时机》《生成物》 | — |
| 17 | 「踩过坑的规矩」8 | 应用数据目录的名字不跟显示名走 | `docs/experience/data-and-credentials.md` §《用户数据目录》 | — |
| 18 | 「踩过坑的规矩」拖动两条 + 位移累加 | `Dragging()` / `Pressed()` / `Dragged()`、别拿位移累加 | `docs/experience/interface-pitfalls.md` §《拖动》 | — |
| 19 | 「版本」 | MyGo 基线、pre-1.0 不锁死、升级前重跑探针 | `docs/experience/release-and-versioning.md` §《MyGo 版本基线》 | — |
| 20 | 「版本」v0.5.5 起 | `ui.Tester` 的钟站着不动 ⇒ 推钟 + `FramePending()` | `docs/experience/interface-pitfalls.md` §《测试里的钟》 | — |
| 21 | 「代码怎么分层」里两扇窗的尺寸 | 迷你播放器 320×100（**旧数字**）→ 代码里是 `miniWidth = 320` / `miniHeight = 84` | **以代码为准**（`internal/ui/miniplayer.go`）；README 里那句随 BMG-290 删掉了，`docs/experience/architecture.md` §《两扇窗》现在只留代码里的数 | — |

### 明确**没有**搬的（它们不是「项目经验」）

- `AGENTS.md` §1、§2、§3、§6、§7、§8、§10 的**纪律本体**——「不许做什么、必须做什么」留在
  `AGENTS.md`，那是每次都要读的。搬走等于把纪律降级成知识。
- `AGENTS.md` §5 的**不变量清单**——「不可协商的项目不变量」是纪律，不是知识。
- `README.md` 的**安装步骤、已知边界的表格本体、许可与著作权声明**——那是给用户看的，
  不是给改代码的人看的。它留在 `README.md`。（许可那一节里**「清单怎么生成、怎么核」
  那一半**搬到了 `docs/experience/startup-and-artifacts.md`，因为那是改代码的人要看的东西。）
- `DESIGN.md`——不动（它被 12 处按章节号引用，挪动会大面积指错）。
