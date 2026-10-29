# 启动与生成物

> 原文在 `AGENTS.md` §4《静默出错的几条铁律》§「启动与生成物」、`README.md`
> 《踩过坑的规矩》4–7 与《图标》。

**什么时候用得上**：你要动 `main.go`、`internal/platform/`、打包脚本；或者你在换应用
图标、改第三方依赖、加单实例 / 托盘 / 开机自启。

---

## 建窗口的时机

**出处**：`AGENTS.md` §4 §「启动与生成物」第 5 条（+ `README.md`《踩过坑的规矩》5）

**建窗口只能走 `App.WhenReady`，不能在回调里直接 `NewWindow`。**

`App.Run()` 先把事件循环转起来，`applicationDidFinishLaunching` 才把 ready 置上；这两件事
之间主线程上 `NewWindow` 是**直接 panic**。

第二个实例正好落在这一段里（它的回调走 `postMain`），症状是「**图标连点两下，应用
当场崩**」。

**什么时候用得上**：你在加新的窗口（迷你播放器已经在里面了）、或者在写任何
「收到系统事件就开窗」的逻辑。

---

## 单实例与退出回调

**出处**：`AGENTS.md` §4 §「启动与生成物」第 6、7 条（+ `README.md`《踩过坑的规矩》4、6）

**单实例锁和退出回调都必须在 `mygo.App.Run()` 之前装。**

- **锁晚一步**就是两个实例同时起来的竞态；
- **回调晚一步**，启动命令行里的 `biu://…` 会在它装好之前被交付掉——点链接唤起应用
  **什么都不会发生**。

### `RequestSingleInstanceLock()` 挡不住同时启动的两个实例

实测 20 轮有 17 轮**两个都拿到锁**：都 Dial 失败 → 都删掉对方的 socket 文件 → 都 Listen
成功。

所以 `internal/platform` 在它之上还压了一把**内核独占锁**（`single_instance.go`，flock /
Windows 上等价的那把）。两个机制**缺一不可**：框架那把挡的是「正常情况下别开第二个」，
内核那把挡的是「同时开」。

**什么时候用得上**：你在动启动流程、或者现象是「能同时开两个实例」「点了链接唤起不来」。

---

## 生成物

**出处**：`AGENTS.md` §4 §「启动与生成物」第 8 条（+ `README.md`《踩过坑的规矩》7）

**图标和第三方许可清单是生成物。**

- **图标图案**在 `platform.AppIcon` 里——图源是它 `go:embed` 的
  `internal/platform/icon-source.png`（人工挑定）。
- **第三方许可清单**从 `go list -deps` 生成（`internal/licensing`，不是照 `go.mod` 抄的）。

**换了图案或依赖却忘了重新生成，仓库里躺着的是旧文件，而没有任何一处会报错。**

重新生成：

```sh
go generate ./internal/platform/ ./internal/licensing/
```

`scripts/build.sh` 每次打包前都会跑一遍并**检查 `git diff`**——所以打包机上会拦住
「改了图案没重新生成」。

**什么时候用得上**：你换了图标源图、加/删了一个依赖、或者在打包时看到脚本因为
`git diff` 非空而停。

---

## 许可清单是三道会红的断言

**出处**：`README.md`《许可》（+ `internal/licensing/licensing.go` 的包注释）

`internal/licensing` 不是「维护一份文档」，它是**三道会红的断言**：

1. 清单里的模块集合 == `go list -deps` 数出来的**实际**依赖集合（不多不少）；
2. 每条声明的 SPDX 标识 == 那个模块 LICENSE 文件里真正写着的东西；
3. 随包发出去的许可证原文 == 模块缓存里那份，**一个字节不差**。

### ⚠️ 为什么不读 `go.mod`

`go.mod` 列的是**要求**，不是**编进去了什么**：indirect 的依赖可能没被引用，而某个
`//go:build windows` 文件里的 import 在 macOS 上根本不会被看到。所以这里跑的是
`go list -deps`，而且**darwin 和 windows 各跑一遍**——两个平台的构建图不一样。

### 三份**不在** `go list -deps` 里的东西

| 谁 | 为什么不在 | 谁盯着它 |
|---|---|---|
| **Biu 的许可**（PolyForm Noncommercial 1.0.0） | Biu 不是构建依赖，它是**上游**。但 PolyForm 的 Notices 一节要求「任何拿到软件一部分副本的人也拿到这份条款」 | `BiuLicenseRelPath` |
| **vendor 进仓库的 MP3 解码器**（`tphakala/go-mp3`，MIT） | vendor 进来的代码是**本仓库的一部分**，不是链接进来的模块 | `VendoredGoMP3LicenseRelPaths` |
| **图标资源那一份**（Remix Icon） | 图标是编进二进制的**资源**，不是链接进来的代码模块。后果是 `TestTheInventoryListsExactlyWhatGetsCompiled` 看不见它少了（那张表只会因为「少一个依赖」红），而 `gen-notices` 的 prune 会把它当「过期文件」删掉 | `AssetLicenseRelPaths`，由 `TestTheShippedAssetLicensesExist` 盯着 |

### ⚠️ 这两处的许可证曾经被写错过

- `github.com/ebitengine/purego` 是 **Apache-2.0**，**不是 MIT**；
- `tphakala/go-mp3` 是 **MIT**，**不是 BSD-3**。

两处都被测试钉住了。

**什么时候用得上**：你在加 / 删一个依赖、或者在查「清单和实际构建对不上」。
（用户要读的许可声明在 `README.md`《许可》一节，那一份留在原处。）

---

## 图标素材的规矩

**出处**：`README.md`《图标》三条 ⚠️（+ `internal/platform/icon.go`）

三条，每一条踩过。

### ⚠️ 图源不能缩到 512

下采样是从图源做的，图源 512 的话 1024 那档就是**插值出来的模糊图**。

Biu 自己的 `electron/icons/logo.png` 就只有 512×512——**这是它的缺陷，不要照抄**。

`internal/platform/icon_test.go` 的 `TestTheArtworkIsBigEnoughForEverySize` 守着下限。

### ⚠️ macOS 不会替你切圆角

Big Sur 改的是 **Apple 自家那一批**图标，而那道圆角是**画进 .icns 里**的；第三方应用的
图标形状完全由素材决定，MyGo 的 `pngToICNS` 也只缩放、**一个像素都不动**。

所以留白和圆角是 `fillIcon` 切的——macOS 图标网格那一格（图案占 **0.824**，圆角半径
**0.2237 × 图案边长**，见 `internal/platform/icon.go` 的 `iconShapeRatio` /
`iconCornerRatio`）。

图源因此必须是一张**铺到四个角**的方图，`TestTheArtworkIsFullBleed` 钉着这一条。

### ⚠️ 托盘和应用是同一张图

`TrayIconPNG` 就是 `AppIconPNG(32)`。

两份各画各的必然漂移，而症状是「**Dock 上是新图标、通知区域还是旧图案**」——那种错
只有用户看得见，而没有任何一处会报错。

### 图源有两种，代码自己判

- **满幅方图**（四个角不透明）→ 留白与圆角由代码切；
- **自带形状**（四个角透明）→ 形状是图案的一部分，代码原样用。

走哪一条由**代码看源图自己判**，所以**换图不用改代码**。

### 产物的尺寸集

| 平台 | 尺寸 | 档数 |
|---|---|---|
| macOS `.icns` | 32 / 64 / 128 / 256 / 256 / 512 / 512 / 1024 | 8 |
| Windows `.ico` | 16 / 24 / 32 / 48 / 64 / 128 / 256 | 7 |

**什么时候用得上**：你在换图标、改图标生成逻辑、或者在排查「Dock 和托盘不一样」。

---

## 包体归因：分层体积与链接依赖

**出处**：`internal/perf` 的 `layers.go` / `golist.go` / `size_report.go`，
入口是 `internal/perf/cmd/artifact-report`（票 #674）

「包 18 MB」不是一个能拿来做决定的数：**主可执行文件**是依赖改动会动的那一块，
**Resources** 是换一张图会动的那一块，**安装包/压缩包**是用户真正下载的那一块。
混在一起就分不清「这次变重是谁变重」。

所以报告固定分四层：

| 层 | 判定 | 意义 |
|---|---|---|
| 主可执行文件 | `Contents/MacOS/` 下那一支；Windows 的 `*.exe`（非 `-setup-`）；Linux 无扩展名的顶层可执行文件 | 链接依赖的结果 |
| Resources | `Contents/Resources/` 下的全部内容；非 bundle 目标里 `resources/` 复制过去的那棵树 | 随包资源的来源 |
| 其他 | `Info.plist`、`PkgInfo`、`_CodeSignature/`、`install.sh`、`update-*.json` | 安装后要占盘、但不属于上面两类 |
| 安装包/压缩包 | `.dmg`、`*-setup-*.exe`、`.tar.gz`、`.delta`、`.deb` | 下载体积，**不计入安装后总计** |

规则在 `ClassifyArtifactFile` 里，**用测试钉住**（`internal/perf/layers_test.go`）：
少判一条不会让构建失败，只会让报告里的百分比对不上 100。

### 链接依赖为什么按模块、不按字节

`go list -deps` 能回答「**哪些模块真的编进去了**」，回答不了「每个模块占多少字节」
——那需要 linker map 或符号级尺寸分析。报告只报前者，**不编造后者的数字**。

依赖按 `build/<goos>-<goarch>` 每个目标各跑一次再合并：`//go:build windows` 的文件让
四个目标的依赖图不完全一样，只跑本机那一个会漏。这和许可清单是同一个口径。

### 怎么跑

```sh
scripts/build.sh              # 打包后自动跑
go run ./internal/perf/cmd/artifact-report            # 量 build/
go run ./internal/perf/cmd/artifact-report -no-deps   # 只量体积
```

`build/` 不存在或没产物时它输出「**未构建**」并成功返回——那是一个正常状态。
**量的是产物，不是模块缓存**：模块缓存多大跟实际编进去多少没有关系。

### 大资源与格式候选

- 资源来源按顶层条目聚合；`>= 1 MiB` 的标成「大资源」，需要复核是否该改成运行时
  按需下载或独立资源包。`TestNoOptionalLargeResourceIsStaticallyShipped` 直接盯着
  `resources/` —— 往那里丢一个大文件会让测试红。
- 「格式与能力来源」那张表按**实际链接结果**过滤：裁掉一个格式会删掉哪些模块，
  表上直接写着。MP3 解码器是 vendor 进仓库的，不在 `go list -deps` 里，报告会说明。

**什么时候用得上**：你在加/删依赖、换随包资源、或者在判断「这次包变大是不是我改的」。
