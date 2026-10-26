# 干活的细则

这个目录装的是**「怎么做」**——流程、工具、判据。**「不许做什么」**在根目录的
`AGENTS.md`（那份是每次开工都要读的纪律）；**「为什么是这样」**在
[`docs/experience/`](../experience/README.md)；**「数值是多少」**在根目录的 `DESIGN.md`。

**什么时候用得上**：你要写票、跑测试、做视觉审查、或者收尾的时候。

---

## 目录里有什么

| 文档 | 讲什么 | 什么时候读它 |
|---|---|---|
| [issue-tracker.md](issue-tracker.md) | 票在 GitHub Issues 上，`gh` CLI 怎么用；PR 当不做请求面；标签怎么贴 | 你要开票、改票、查票、贴标签 |
| [ticket-template.md](ticket-template.md) | 开票的骨架；UI 票的验收里**必须有**「视觉审查」那一条 | 你在写一张新票的正文 |
| [triage-labels.md](triage-labels.md) | 五个 triage 角色（`needs-triage` / `needs-info` / `ready-for-agent` / `ready-for-human` / `wontfix`）与标签字符串的对应表 | 你在做 triage |
| [domain.md](domain.md) | 领域文档（`GLOSSARY.md` / `docs/adr/`）怎么被消费；**它们现在不存在**，别因为找不到就停下来 | 你在造新的领域概念，或者你在找「术语表」 |
| [testing.md](testing.md) | 验收命令为什么是那一条、`-trimpath` 的三个硬条件、测试的几条口径、**已知会飘的那几条** | 你要跑测试、判一条红是真的回归还是机器忙 |
| [visual-review.md](visual-review.md) | 视觉审查：跟什么比、比哪五项、间距怎么量、留下什么 | 你在一张 UI 票上要交出图并做审查 |
| [cleanup.md](cleanup.md) | 收尾：为什么要把 gopls 停掉、怎么按 cwd 认领、清理的顺序 | 你写完收尾说明、准备交出去之前 |

---

## 规矩 vs 知识 vs 数值

| 文档 | 读者 | 内容 | 什么时候读 |
|---|---|---|---|
| `AGENTS.md` | agent | **每次都要读的硬规矩** | 每票 |
| `docs/agents/` | agent | 流程细则（票面怎么写、视觉审查怎么量、测试怎么判、收尾怎么清） | 按需 |
| `docs/experience/` | 要改代码的人 | **为什么是这样**、哪些地方静默出错 | 改到那块时 |
| `DESIGN.md` | agent / 评审 | **视觉的唯一目标值**（数值） | UI 票 |
| `README.md` | 用户 / 贡献者 | 这是什么、怎么跑、支持什么、已知边界、许可 | 一次 |

**分工的判据**：

- 「**每次都要读**」⇒ `AGENTS.md`（纪律：不许做什么）
- 「**改到才读**」⇒ `docs/experience/`（原因：为什么这么写）
- 「**要看数值**」⇒ `DESIGN.md`（目标值）
- 「**用户要问的**」⇒ `README.md`

⚠️ 三者内容**不许重复**。重复的代价：改一处漏三处，agent 读到两个版本就会挑一个信
——而这个仓库的历史教训恰恰是「**静默出错比报错更坏**」。
