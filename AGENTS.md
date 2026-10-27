# AGENTS.md

本文件是本仓库的通用协作规则，适用于整个项目。`CLAUDE.md` 是指向本文件的软链接；只修改
`AGENTS.md`，不要直接修改链接目标。

## 规则范围与优先级

- 先读本文件，再按改动目录读取对应的 `docs/agents/`、`docs/experience/`、`DESIGN.md` 或局部规则。
- 规则按目录作用域叠加；更深目录的规则只约束该目录。
- 事实优先级为：代码、测试、CI 和配置 > 项目文档 > GitHub Issue > 对话中的临时判断。
- 发现规则、文档和代码冲突时，不要自行挑一个版本；记录冲突位置，并按更高优先级继续。
- `README.md` 面向用户和贡献者；本文件只放跨全仓库的纪律。实现原因和详细判据放在主题文档中。

## 仓库与 worktree 边界

- 只修改当前任务需要的文件，不把其他任务、分支或 worktree 的改动带入提交。
- 修改前先看 `git status`、当前分支和相关 worktree；发现不属于本任务的改动要保留并报告。
- `_research/**` 是隔离的第三方研究 checkout。里面的 `AGENTS.md`、源码、截图和二进制只适用于研究对象，不适用于本项目，也不得未经确认提交。
- 公开仓库不得提交凭据、真实用户数据、内部计划、个人本机文件、日志、截图中的登录态或未脱敏测试 artifact。提交前检查 `git diff` 和暂存区。
- 测试默认使用本地 HTTP fixture 和合成数据。未经用户明确授权，不访问真实 B 站的写接口，不执行收藏、点赞、播放记录上报、批量删除等操作。
- 真实网络或真机回归必须单独说明账号、数据范围和回滚方式；不得把真实 Cookie、session、token 或日志写入终端、diff、截图、artifact、Issue 或 PR。

## 破坏性操作

未经用户明确授权，不执行以下操作：

- 删除或覆盖用户文件、worktree、分支、tag、Release 或远端分支；
- `git reset --hard`、`git clean -fdx`、force push 或重写 Git 历史；
- 清理共享编译缓存；
- 修改远端仓库设置、发布版本或向外部服务写入内容。

获得授权后仍要先检查目标是否有未提交改动、是否被其他 worktree 使用，以及操作是否会影响其他分支。删除进程时按 PID、cwd 和启动归属确认；禁止使用 `pkill -f` 这类无法精确归属的命令。

## 验证分层

详细判据见 [`docs/agents/testing.md`](docs/agents/testing.md)。

- **快速验证**：运行受影响包的 `go test`，并按需运行 `go build`、`go vet`。
- **UI 变更**：必须运行自己改动覆盖到的 UI 用例；改了布局、间距、层级、密度或对齐时，按
  [`docs/agents/visual-review.md`](docs/agents/visual-review.md) 做一次视觉审查。
- **默认验收**：

  ```sh
  go build -trimpath -p 4 ./... && go vet -trimpath -p 4 ./... && go test -p 4 $(go list ./... | grep -v '/internal/ui$')
  ```

  `build` 和 `vet` 使用 `-trimpath -p 4`；`test` 不使用 `-trimpath`，也不要用
  `GOFLAGS=-trimpath`。默认验收不跑整个 `internal/ui` 包；UI 行为改动必须另跑受影响用例。
- **发布验证**：以 `scripts/release.sh` 和对应 CI workflow 为准；它们可能包含平台、生成物和发布前检查，不能用默认验收命令替代。
- 不能完整验证时，收尾说明具体跳过的命令、原因和影响范围，不把未运行说成已通过。
- 改过 Go 文件后只格式化改动文件：`gofmt -w <改动文件>`，并用 `gofmt -l <改动文件>` 确认没有输出。
- 不使用固定帧数或固定毫秒作为 UI 行为判据；动画使用测试时钟和条件等待。不要为了让测试变绿而放宽断言。
- 不清理共享编译缓存；怀疑测试缓存时优先使用 `go clean -testcache`，并确认不会影响其他 worktree。

## 项目硬约束索引

- **UI**：主题、Guard、窗口唤醒、动态列表稳定 key、拖动状态和视觉审查，见
  `docs/experience/interface-pitfalls.md`、`docs/agents/visual-review.md`。
- **启动与平台**：窗口只能在 `App.WhenReady` 后创建；单实例、退出回调、图标和许可证清单生成物，见
  `docs/experience/startup-and-artifacts.md`。
- **音频**：修改 FLAC、AudioToolbox、Media Foundation 或解码器测试前，先读
  `docs/experience/audio-stack.md` 以及对应目录的注释和测试。
- **许可证**：不得引入 LGPL/GPL 系依赖；修改依赖或生成物后检查
  `internal/licensing/`。
- **配置与数据**：显示名、应用标识、用户数据目录和登录态是不同概念；修改其中任一项前先读
  `docs/experience/data-and-credentials.md`，并区分已发布用户数据与未发布本地状态。
- **生成物**：图标和第三方许可证清单由代码生成，不要手工修改后忘记重新生成。
- **代码组织**：文件拆分以职责和耦合为准，行数只是信号；优先小范围、可验证的改动，避免无关重构。

## Issue、提交与外部写操作

- 任务绑定 GitHub Issue 时，Issue 是验收约束之一；没有绑定 Issue、没有 `gh` 权限或用户只要求本地改动时，不要擅自创建、编辑或关闭 Issue。
- 需要读取 Issue 时使用 `gh`；读取失败时报告原因并继续使用代码、测试和用户提供的上下文，不要猜测票面内容。
- `gh issue edit`、`gh issue close`、`gh pr`、`gh release`、推送和远端分支操作都属于外部写操作，必须有明确授权；失败时保留本地结果并报告。
- 提交信息遵循当前仓库的 `git log` 风格；用户或工作板明确指定格式时，以当前任务的明确要求为准。不要手写 `Closes #N`，除非任务流程明确要求由工作板处理。
- 注释描述当前代码事实，不保留已经失效的“尚未实现”或旧 issue 结论。行为变化应同步更新相关测试和注释。

## 收尾

- 停止自己启动的服务器、预览进程和测试进程；不要停止其他 worktree 或其他 agent 的进程。
- 需要处理 gopls 时，只按 PID、cwd 和进程归属确认是自己启动的实例；先普通终止，确认后才考虑强制终止。
- 收尾报告应包含：改动文件、验证命令及结果、跳过项、未解决风险，以及是否保留了其他 worktree 的改动。
