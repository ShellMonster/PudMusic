# 收尾清理

> 原文在 `AGENTS.md` §10《收尾：把自己起的 gopls 停掉》。

**纪律本体**（收尾的最后一步：把自己工作区里那个 gopls 停掉）在 `AGENTS.md`。这份是
**细则**：为什么、怎么做、清理的顺序。

**什么时候用得上**：你做完一张票、写完收尾说明、准备交出去之前。

---

## 为什么

**出处**：`AGENTS.md` §10《收尾：把自己起的 gopls 停掉》§「为什么」

板子清理一张票的时候，要那个 agent 的 herdr 面板「除了前台进程没有别的活进程」。

插件源码（`~/.config/herdr/plugins/github-installments/herdr-projects-*/checkout/`
下，可以自己核实）：

- `crates/core/src/herdr/api.rs:92` 的 `pane_childless(id)`：面板里除了前台进程，不能有
  别的活进程（**前台进程自己也不能有子进程**）；
- `crates/flow/src/land/cleanup.rs:32` 的 `cleanup_with`：清理要 `at_rest`（里面就是
  `pane_childless`）才继续，否则**推迟**（`retry`），并把「空间还在用」写进板子的话里。

gopls 是 Claude Code 自己起的 Go 语言服务器（实测：gopls 的父进程就是那个 claude
进程），**claude 退出之后它还活着**（成了孤儿，被 init 收养）——所以它正好是那个「赖着
不走的进程」。

**插件的规则是对的**（它不想在别人的进程还活着的时候把空间拆了，拆了会把那个进程一起
带走）：**该走的是把那个进程收掉。**

不收的后果：agent 做完了，**herdr-projects 不关那个窗口**（侧边栏里一直留着），板子的话里
一直写着「Kept: … (its space is in use beside its agent: gopls …)」，**工作区也删不掉**。

⚠️ **gopls 只是最常见的一个**：插件拦的是**任何**留在面板里的后台进程（一个没关的
`go test`、一个 `node` 服务器……）。这一条治的是 gopls 这个大头。

**什么时候用得上**：你发现板子收不掉你的工作区（侧边栏里窗口一直留着）。

---

## 怎么做

**出处**：`AGENTS.md` §10 §「怎么做」

**按 cwd 认领，只杀自己那个**：

```sh
# $PWD 就是自己的工作区（自己的 worktree）
for p in $(pgrep -x gopls); do
  cwd=$(lsof -a -p "$p" -d cwd -Fn 2>/dev/null | sed -n 's/^n//p')
  [ "$cwd" = "$PWD" ] && kill "$p"
done
```

- **按 cwd 认领**：`lsof -a -p <pid> -d cwd` 看它的工作区是不是自己那个。**别人的 gopls
  一个别动**（别的 agent 还在用）。
- ⚠️ **别写成 `pkill -f gopls`**——那会杀掉别的 agent 的。
- **放在收尾的最后**：**最后改完文件之后再杀**。在那之前杀，LSP 会再起一个（Claude Code 的
  LSP 一碰到 Go 文件就再拉一个 gopls 起来）。
- 杀完确认一下：`pgrep -fl gopls` 里自己那个没了没有（gopls 还有一个同名的遥测子进程
  `gopls ** telemetry **`，它跟着 gopls 一起死；真还在就 `kill -9`）。

**什么时候用得上**：你写完最后一行代码、准备收尾的时候。

---

## 清理的顺序

**出处**：`AGENTS.md` §10 §「清理的顺序」

省得下一个人重新推（**只有第 1 步是你的**，其余是板子做的）：

1. **杀自己工作区里的残留进程**（上面那条就是它）——**这一步不做，第 3 步就推不动**：你的
   gopls 还在面板里，`pane_childless` 就是 false，清理一直 `retry`；
2. **归档任务**（板子把任务收进归档，`herdr-projects archive <id>`）：它自己的 herdr 空间
   关掉、临时文件清掉；
3. **删工作区**：板子的清理（`cleanup_with`）把 worktree 删掉；
4. **修剪 worktree 登记**：`git worktree prune`——清理里紧跟着 `worktree remove` 做。

**什么时候用得上**：你在推演收尾的步骤，或者你发现清理一直 `retry`。

---

## 顺带：临时文件放哪

**出处**：本目录新增（板子的规矩：`$TMPDIR` 里专属目录、收尾前停掉后台进程）

**自己的临时东西**（测试工程、预览、虚拟环境、冒烟安装、下载）放在**自己那一份
`$TMPDIR` 目录**里，别散在 `/tmp` 或家目录里——板子会在这一票结束时把那个目录连同还活
着的进程一起收掉。

同理，**别留还活着的后台进程**：除了 gopls，还有你起的开发服务器、预览服务。**收尾前
把它们停掉**，除非人明确说留着。

**什么时候用得上**：你在这一票里造了临时东西（测试工程、预览服务、下载）。
