# 发版与版本

> 原文在 `README.md`《版本》，以及 `scripts/release.sh` / `CHANGELOG.md` 顶上那几段
> （它们是「为什么这么写」的部分）。

**什么时候用得上**：你要发一个版本、升版本号、升 MyGo；或者你在改 `CHANGELOG.md` /
`mygo.json` 的 `version`。

---

## 版本号三处对上

**出处**：`CHANGELOG.md` 顶上两段（+ `scripts/release.sh` 的第 1.3 步）

发一版要改**三个地方**，缺一个都会在某一步停住：

| 地方 | 内容 | 不对会怎样 |
|---|---|---|
| `mygo.json` 的 `version` | `0.1.0` 这样的版本号 | 标签与清单都对不上 |
| `CHANGELOG.md` 的 `## <版本>` 段落 | 这一版的更新日志 | `mygo build` **直接失败**，报 `<文件> has no notes for <版本>` |
| git 标签 | `v<版本>`（前缀来自 `mygo.json` 的 `updates.tagPrefix`，默认 `v`） | 应用查不到更新 |

⚠️ **段落标题必须正好是 `## <版本>`**：MyGo 按那个切段落，把那一段原样显示在「有新
版本」弹窗里。**空段落和没有段落对应用是一回事**——弹窗里一片空白。

### CHANGELOG 的两个读者

- **装上应用的人**：看到的是「有新版本」弹窗里那段 Markdown。
- **改这份代码的人**：发下一个版本时，照这一版的写法加一段。

所以 CHANGELOG 里**不要写「还没做」**（见
[git-and-workflow.md](git-and-workflow.md)《注释的纪律》），也不要重复 README「已知
边界」那一节——那边写得比这里细，重复会让两边各说各话。

**什么时候用得上**：你要发版，或者在改更新日志的写法。

---

## 发版五步

**出处**：`scripts/release.sh` 的五步（+ 它顶上那段「真正干活的还是 MyGo 自带的 CLI」）

```sh
scripts/release.sh                 # 四个首发目标
scripts/release.sh darwin/arm64    # 只发一个
```

脚本做的是 MyGo 的 CLI **不做、而漏了不会报错**的那几件。五个步骤：

| 步 | 做什么 | 拦住了什么 |
|---|---|---|
| 1 | 前置检查 | 工作区脏、`gh` 没登录、CHANGELOG 缺段落、标签打错提交、**私钥读不到**、Windows 缺 NSIS、仓库是私有的 |
| 2 | 跑验收 | `go build` / `go vet` / `go test`（**不带 `-race`**） |
| 3 | 重新生成图标与第三方许可清单 | 「改了图案 / 依赖却忘了重新生成」——检查 `git diff` |
| 4 | 打带签名的更新包 | **清单缺失**（多半是没私钥）、**签名是空的**、清单版本对不上 |
| 5 | 推标签并挂**草稿** Release | 标签打错、release 已经发过 |

### ⚠️ 第 5 步挂的是草稿，不是发布

草稿里的资产**匿名取不到**，而 MyGo 的更新源是从**正式发布**的 release 里读清单的。所以
这个脚本跑完**还不算发完**——人看过之后要把草稿转正式：

```sh
gh release edit "$tag" --draft=false
```

**那一步才是应用开始看得到新版本的那一刻。**

### ⚠️ 私钥丢了就再也发不了更新

签名私钥在脚本算出来的默认路径下（`~/.config/biu-mygo/` 里那个 `mygo-update.key`，
能用 `MYGO_UPDATE_KEY_FILE` 改路径），**它不在仓库里**。

**丢了它就再也发不了更新，而且已经装出去的老版本会验不过之后的每一个版本**——那时只能
让所有人重新下载安装。所以**备份在哪要写在发版说明里**，别只放在本机。

还没有就生成一对（私钥别进仓库）：

```sh
go tool mygo keygen -o ~/.config/biu-mygo/
```

公钥（同一目录的 `mygo-update.pub`）要写进 `mygo.json` 的 `updates.publicKey`。

### ⚠️ 仓库是私有的 ⇒ 发出去的版本别人拿不到更新

MyGo 的更新源走**匿名** HTTPS 请求
（`https://github.com/<owner>/<repo>/releases/latest/download/update-<平台>.json`），
**私有仓库的资产匿名取不到**，而应用和日志**都不会报错**——它只是一直查不到更新。

脚本在这里是**警告不是拦住**（源码仓库现在还是私有，用户已定「就用它本身，以后转
公开」）。确实要发就设 `MYGO_RELEASE_ALLOW_PRIVATE=1`。**仓库转公开之后什么都不用改**，
同一条配置自己就通了。

### ⚠️ 标签要打在 main 上

本仓库是 **squash 合并**（PR 的合并提交就是压过的那一个），所以在功能分支上打的标签，
合并之后**永远不在 main 上**——标签指向一份 main 里根本不存在的代码。症状是发布出去的
产物和 main 对不上，而哪一处都不报错。

要发布的是 main 上那一份，就切到 main 再跑一次。已经打过标签的话：

```sh
git tag -f "$tag" origin/main && git push -f origin "$tag"
gh release edit "$tag" --repo <owner>/<repo> --target main
```

### ⚠️ bash 3.2 的解析坑

`${变量}` 后面紧跟一个非 ASCII 字符，会让 bash 3.2（本机唯一的 bash）把那个字符的头一个
字节并进变量名——`$变量` + 全角逗号会解析成一个不存在的 `变量…`，`set -u` 下当场崩在
那一行。

**危险的不是崩，是崩在哪**：第 5 步崩之前已经把标签推出去了，于是留下一个
「**有标签、没有 release**」的半截发版，而 `bash -n` 查不出来（语法是对的）。

脚本自己在动手之前先扫一遍（用 `LC_ALL=C grep -n '\$[A-Za-z_][A-Za-z0-9_]*[^ -~]'`），
**不用 `grep -P`**（BSD grep 未必有）。

**什么时候用得上**：你要发版，或者你在改 `scripts/release.sh`。

---

## 打包与签名

**出处**：`README.md`《从源码跑 / 自己打包》《安装》两节里的公证与签名那几段

### 打包

```sh
scripts/build.sh              # 打 macOS dmg + Windows NSIS 安装包
scripts/build.sh darwin/arm64 # 只打一个目标
```

打包走 MyGo 自带的 CLI（`go tool mygo build`，`go.mod` 里的 `tool` 指令）。打 Windows 安装包
需要 NSIS：macOS 上 `brew install nsis`，Debian 上 `apt install nsis`。

`scripts/build.sh` 在缺 NSIS 时会**直接停下并说明**，而不是像框架那样只打一行提示就跳过
——否则脚本「成功」了而产物里没有安装包，要等下一次签收才发现。

### ⚠️ 两个产物都没有签名 / 公证

| 产物 | 现状 | 用户第一次打开会看到 |
|---|---|---|
| macOS `.dmg` | **没有公证**（notarization） | Gatekeeper 说「无法验证开发者」 |
| Windows 安装包 | **没有代码签名** | SmartScreen 说「Windows 已保护你的电脑」 |

**这两条都是预期的，不是应用坏了**：

- macOS：右键点图标 → **打开** → 在弹窗里再点一次「打开」；或者命令行
  `xattr -dr com.apple.quarantine "/Applications/BiuGo.app"`。之后这台机器不会再拦。
- Windows：点 **更多信息** → **仍要运行**。

要彻底消除它们需要 Apple 开发者计划的 Developer ID 并走公证流程，以及一张代码签名证书
——本项目**明确不做 / 没有买**（用户已定）。

⚠️ **注意：更新包的签名是另一回事，它有**——见上面《发版五步》里那个私钥。安装包本身
不签名，不影响自动更新。

**什么时候用得上**：你在打包、或者用户第一次装上说「打不开 / 被拦了」。

---

## MyGo 版本基线

**出处**：`README.md`《版本》

MyGo 记在 `go.mod` 里，基线 **v0.6.4**（2026-10-26 从 v0.6.3 升上来；v0.6.3 是
2026-10-24 从 v0.6.2 升上来的）。

**不锁死**：MyGo 是 pre-1.0、发布很快、**没有兼容性承诺**。升级前请重跑这里的探针
（本仓库的测试、维护者本机的 `library/t-0018/` 探针、音频实验），确认计划依赖的 API 还在。

⚠️ `library/` 那一份**不在版本库里**（和 `docs/research/` 一样是维护者本机的东西）——找不到
它不等于「没有探针」，是**没在仓库里**。

### v0.5.5 起必读：`ui.Tester` 的钟站着不动

动画、`Context.After`、双击判定全走那只钟，只有测试自己调 `tt.Advance(d)` 它才走。所以
「循环 `tt.Frame()` 等动画停下来」这类辅助函数会**静默失效**。

完整的说明在 [interface-pitfalls.md](interface-pitfalls.md)《测试里的钟》——那是**测试**
侧的；这一条记在这里是因为**升 MyGo 时最容易撞上它**。

**什么时候用得上**：你在升 MyGo，或者你在写依赖那只钟的测试。
