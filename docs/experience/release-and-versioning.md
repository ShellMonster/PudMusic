# 发版与版本

> 原文在 `scripts/release.sh` 与 `.github/workflows/release.yml` 的注释、`CHANGELOG.md`
> 顶上那几段（它们是「为什么这么写」的部分）。

**什么时候用得上**：你要发一个版本、升版本号、升 MyGo；或者你在改 `CHANGELOG.md` /
`RELEASE-NOTES.md` / `mygo.json` 的 `version`。

---

## 版本号、两份说明和标签对上

**出处**：`CHANGELOG.md` 顶上两段（+ `scripts/release.sh` 的前置检查、`release.yml` 的
`Resolve release tag`）

发一版要让**下面四处**对上，缺一个都会在某一步停住：

| 地方 | 内容 | 不对会怎样 |
|---|---|---|
| `mygo.json` 的 `version` | `0.1.0` 这样的版本号 | 标签与清单都对不上；`release.yml` 的 `Resolve release tag` 直接失败 |
| `CHANGELOG.md` 的 `## <版本>` 段落 | 开发者账，也是 GitHub Release 说明的来源 | `generate-release-notes.sh` 报「CHANGELOG.md 中没有 ## <版本> 段落」 |
| `RELEASE-NOTES.md` 的 `## <版本>` 段落 | 应用内「有新版本」弹窗的说明 | `mygo build` **直接失败**（`mygo.json` 的 `updates.changelog` 指着它） |
| git 标签 | `v<版本>`（前缀来自 `mygo.json` 的 `updates.tagPrefix`，默认 `v`） | 应用查不到更新；`release.sh` 也会核标签与版本 |

⚠️ **两份说明的段落标题都必须正好是 `## <版本>`**：`mygo build` 按这个切
`RELEASE-NOTES.md`，把那一段原样显示在「有新版本」弹窗里；`generate-release-notes.sh`
按同一条规则切 `CHANGELOG.md`。**空段落和没有段落对应用是一回事**——弹窗里只剩一句
「暂无更新日志」。

### 两份说明的分工

- `CHANGELOG.md`：给改代码的人看的版本账，也是 GitHub Release 页面的说明来源
  （`scripts/generate-release-notes.sh`）。
- `RELEASE-NOTES.md`：给装应用的人看的更新说明，`mygo build` 把它写进更新清单，
  应用内弹窗渲染的就是它。怎么写见那份文件开头的「怎么写」。
- 两边**不互相复制**：用户感知得到的变化写给 `RELEASE-NOTES.md`，内部重构、测试、
  依赖升级写给 `CHANGELOG.md`。

两份里都**不要写「还没做」**（见
[git-and-workflow.md](git-and-workflow.md)《注释的纪律》），也不要重复 README「已知
边界」那一节——那边写得比这里细，重复会让两边各说各话。

**什么时候用得上**：你要发版，或者在改更新日志的写法。

---

## 发版：本地两步 + CI 构建

**出处**：`scripts/release.sh` 顶上那段注释与前置检查、`.github/workflows/release.yml`

```sh
scripts/release.sh
```

脚本**不收平台参数**：六个目标（macOS ×2、Windows ×2、Linux ×2）由 `release.yml`
的 matrix 构建。本地只负责「标签打在对的代码上、该核的都核过」；构建、签名、
挂草稿全在 CI。

| 阶段 | 在哪 | 做什么 | 拦住了什么 |
|---|---|---|---|
| 1 | 本地 | 前置检查 | 工作区脏、`gh` 没登录、版本号与说明对不上、标签打错提交、HEAD 不在 `origin/main` 的祖先里、没有更新源或公钥、更新源仓库不可见 |
| 2 | 本地 | 打 `v<版本>` 标签并推送 | 标签冲突在推之前撞出来 |
| 3 | CI | 按 matrix 构建六套目标 | 版本号与 tag 不一致、`MYGO_UPDATER_PRIVATE_KEY` 缺失 |
| 4 | CI | 发布专属检查：`internal/licensing` 测试、重新生成图标与许可清单并核 `git diff` | 改了图案或依赖却忘了重新生成 |
| 5 | CI | 重命名、校验资产、算 `SHA256SUMS`、从 `CHANGELOG.md` 生成 GitHub Release 说明 | 缺资产、文件名不对 |
| 6 | CI | 挂**草稿** Release | 已发布的版本被重传 |

⚠️ **`release.yml` 不跑 `go build` / `go vet` / `go test` 全量**：它假定标签指向的
提交已经过了 PR CI。所以标签必须打在 `origin/main` 上的提交（见下）。

### ⚠️ 脚本跑完还不算发完：草稿要人转正式

草稿里的资产**匿名取不到**，而 MyGo 的更新源是从**正式发布**的 release 里读清单的。所以
这个脚本跑完**还不算发完**——人看过之后要把草稿转正式：

```sh
gh release edit "$tag" --draft=false
```

**那一步才是应用开始看得到新版本的那一刻。**

### ⚠️ 私钥在 CI secret 里，丢了就再也发不了更新

CI 从仓库 secret **`MYGO_UPDATER_PRIVATE_KEY`** 读私钥（内容是 `mygo-update.key`
本身）。缺了它 `release.yml` 的 `Verify update signing key` 会直接失败，不会打出
没有更新清单的包。

本机只在打本地包时需要私钥：`mygo.json` 的 `updates.privateKey` 指向文件路径，
环境变量 `MYGO_UPDATER_PRIVATE_KEY` 的内容优先于路径。还没有就生成一对
（默认写到 `os.UserConfigDir()/mygo/update-keys`，`-o` 可改；私钥别进仓库）：

```sh
go tool mygo keygen -o ~/.config/pudmusic/update-keys
```

公钥（同一目录的 `mygo-update.pub`）要写进 `mygo.json` 的 `updates.publicKey`。

**丢了它就再也发不了更新，而且已经装出去的老版本会验不过之后的每一个版本**——那时只能
让所有人重新下载安装。所以**备份在哪要写在发版说明里**，别只放在本机。

### ⚠️ 更新源仓库必须是公开的

MyGo 的更新源走**匿名** HTTPS 请求
（`https://github.com/<owner>/<repo>/releases/latest/download/update-<平台>.json`），
**私有仓库的资产匿名取不到**，而应用和日志**都不会报错**——它只是一直查不到更新。

当前更新源 `ShellMonster/PudMusic` 是公开的。脚本仍保留这项检查，防止将来把更新源
切回私有仓库后发出版本。两种情况分开处理：`gh repo view` **取不到可见性**（仓库名
写错或没有权限）时脚本直接停下，没有放行开关；仓库存在但可见性是 PRIVATE / INTERNAL
时，设 `MYGO_RELEASE_ALLOW_PRIVATE=1` 才能继续。仓库转公开之后同一条配置自己就通了。

### ⚠️ 标签要打在 main 上

本仓库合并 PR 时如果用了 **squash 合并**（合并提交压过分支上的提交），功能分支上
那个 SHA 合并之后**不在 main 里**——标签会指向一份 main 里根本不存在的代码。症状是
发布出去的产物和 main 对不上，而哪一处都不报错。用 merge commit 合并时这个 SHA 仍在
main 的祖先链里，脚本的检查会通过，但那不代表可以继续在分支上打标签：发版标签始终
打在 main 上。

更要紧的是 `release.yml` **不跑 build/vet/test**，它假定标签指向的提交已经过了 PR CI。
squash 合并后，只有 main 上那个压过的提交是跑过 PR CI 的；功能分支的 SHA 不在 main
里，所以 `scripts/release.sh` 发现 HEAD 不是 `origin/main` 的祖先时**直接停下**，不推
标签。merge commit 合并时分支 head 仍在 main 的祖先链里，脚本不会拦，但发版标签仍应
打在 main 上。

确实要从别的提交发（你自己保证它测过），设 `PUDMUSIC_RELEASE_ALLOW_OFF_MAIN=1`，脚本
只打一段警告就放行。

要发布的是 main 上那一份，就切到 main 再跑一次。已经打过标签的话：

```sh
git tag -f "$tag" origin/main && git push -f origin "$tag"
gh release edit "$tag" --repo <owner>/<repo> --target main
```

### ⚠️ bash 3.2 的解析坑

`${变量}` 后面紧跟一个非 ASCII 字符，会让 bash 3.2（本机唯一的 bash）把那个字符的头一个
字节并进变量名——`$变量` + 全角逗号会解析成一个不存在的 `变量…`，`set -u` 下当场崩在
那一行。

**危险的不是崩，是崩在哪**：如果崩在标签已经推出去之后，就留下一个
「**有标签、没有 release**」的半截发版，而 `bash -n` 查不出来（语法是对的）。

脚本自己在动手之前先扫一遍（用 `LC_ALL=C grep -n '\$[A-Za-z_][A-Za-z0-9_]*[^ -~]'`），
**不用 `grep -P`**（BSD grep 未必有）。

**什么时候用得上**：你要发版，或者你在改 `scripts/release.sh`。

---

## 打包与签名

**出处**：`README.md`《从源码跑 / 自己打包》《安装》两节里的公证与签名那几段

### 打包

```sh
scripts/build.sh              # 六个目标（macOS dmg、Windows NSIS、Linux tar.gz/deb）
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
  `xattr -dr com.apple.quarantine "/Applications/PudMusic.app"`。之后这台机器不会再拦。
- Windows：点 **更多信息** → **仍要运行**。

要彻底消除它们需要 Apple 开发者计划的 Developer ID 并走公证流程，以及一张代码签名证书
——本项目**明确不做 / 没有买**（用户已定）。

⚠️ **注意：更新包的签名是另一回事，它有**——见上面《发版：本地两步 + CI 构建》里
那个私钥。安装包本身不签名，不影响自动更新。

**什么时候用得上**：你在打包、或者用户第一次装上说「打不开 / 被拦了」。

---

## MyGo 版本基线

**出处**：`go.mod` 的 `require` 与 `tool` 指令

MyGo 记在 `go.mod` 里，当前基线 **v0.9.0**（2026-11-11 升上来）。升级时以 `go.mod`
为准，不要照抄这里的数字。

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
