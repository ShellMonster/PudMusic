# 上游 Biu

> 原文在 `AGENTS.md` §1《UI 票的验收必须有一次「视觉审查」》①，以及 `README.md`
> 《已知边界 → 平台》《⚠️ 这一条是差异说明，不是「不支持」》。

上游是 [`wood3n/biu`](https://github.com/wood3n/biu)（Electron + React）。**它是这个项目的
参照物**：数值以上游源码为准（`DESIGN.md` 就是这么来的）。

**什么时候用得上**：你在实现或对齐一个上游页面（**UI 票尤其**）；或者你要查「上游这个
数值到底是多少」「上游这个页面长什么样」。

---

## 上游源码与截图怎么取

**出处**：`AGENTS.md` §1《UI 票的验收必须有一次「视觉审查」》①

### 源码

```sh
gh api repos/wood3n/biu/contents/src/pages/<页面>/index.tsx --jq .content | base64 -d
```

⚠️ **一个页面常常不止一个文件**：比如「关注页」是 `src/pages/follow-list/` 下的
`index.tsx` + `user-card.tsx` + `group-modal.tsx` + `set-group-modal.tsx`，**数值常常在
子组件里**（`ROW_GAP` 就在 `src/components/virtual-grid-page-list/index.tsx`）。

列目录：

```sh
gh api repos/wood3n/biu/contents/src/pages --jq '.[].name'
gh api repos/wood3n/biu/contents/src/components --jq '.[].name'
```

### 截图

上游 README 里的实图在 `screenshots/`：

```sh
gh api repos/wood3n/biu/contents/screenshots --jq '.[].name'
```

只有 `home` / `main` / `download` / `fullscreen` / `playlist` / `mini` 六张，⚠️ **而且是
旧版的**——**没有对应图就比源码，别去找不存在的截图**。

### ⚠️ 上游的 `disableAnimation` 有 43 处

（2026-10-26 在上游 `master` 上数过：`src/` 下 43 处，分布在 33 个文件里。）

这是上游自己关掉的动画。**看到上游某个组件没有动画，先确认是不是 `disableAnimation`
带来的**——那可能是上游的临时状态，不是它的设计。

**什么时候用得上**：你在对齐一个上游页面的行为，而两边看起来不一样。

---

## 数值怎么对齐

**出处**：`AGENTS.md` §1 同上（+ `internal/ui/follow.go` 的 `followRowGap` 注释）

**数值以上游源码为准**，不是以上游截图为准（截图是旧版的，而且尺寸会被缩放）。

一个典型的例子是**关注页卡片**：

上游 `src/components/virtual-grid-page-list/index.tsx` 里有

```ts
const ROW_GAP = 16;
```

它同时当**两个角色**用：

- **行间距**：`estimateSize: () => rowHeight + ROW_GAP` 加上
  `height: virtualRow.size - ROW_GAP`（行之间留 16，画的时候把那一份减掉）；
- **列间距**：`gap: ${ROW_GAP}px`（CSS grid 的 gap 同时管行和列）。

我们这边**列间距**走共用的 `list.GridGap`，**行间距**单独是 `internal/ui/follow.go` 的
`followRowGap`（= `space.Px16`）。

⚠️ **票 #418 的教训**：那张票出图了、票也过了，用户看一眼就发现**行与行之间贴死了**
——列与列有 16 的缝，纵向一格没有。票面里已经写着「间隙对着上游核过」，可**没人按
「间距」这一项去量**，只对着图扫了一眼，于是上游的 `ROW_GAP = 16`（**那是行间距**）
被当成了格子之间的缝。

⇒ **间距要量，不要目测**。测量办法见 `docs/agents/visual-review.md`。

**什么时候用得上**：你在对齐一个上游页面的数值；或者你在查「上游这个数到底是 16 还是别的」。

---

## 我们和上游刻意不同的地方

**出处**：`README.md`《已知边界 → 平台》与《⚠️ 这一条是差异说明，不是「不支持」》

这些是**拍过板的差异**，不是「还没做」。⚠️ **别把它们改回去**。

### 页面切换动画没有

Biu 源码里已经自己注释掉了（`src/pages/settings/system-settings.tsx` 里那段
`pageTransition` 的 UI，连带一句 FIXME：「暂时移除，该功能会导致页面切换时重复渲染，
数据请求 double」）。

现在是**死设置**，照抄「没有」。当前版本只识别自己的设置文件格式。

### 全屏播放器的毛玻璃效果与 Biu 原版不同

> **这一条是「差异说明」，不是「不支持」。**

Biu 用自研的 `glassmorphism.ts`（`src/components/full-screen-player/glassmorphism.ts`）：
封面取主色 → 32×32 均值 → 按 `deviceMemory` / `hardwareConcurrency` 降模糊 → 三层
`radial-gradient`。

**这一版还没有毛玻璃。** 满屏播放器的背景现在是「封面铺满整窗 + 一层 55% 的纯黑压暗」
（`internal/ui/fullplayer.go` 的 `fullScrim`）——那层压暗是**必需的**，因为满屏的控件和
字全是白的，Biu 那边靠三层渐变挡着，我们这边靠它。

毛玻璃本身（MyGo 的 `plugins/glass`，Liquid Glass，三端 shader + CPU 等价实现）是用户
2026-10-15 拍板的取舍，**还没落地**；落地时同样**不自绘等效效果**。

所以**构图与质感必然不同**：MyGo 只有线性渐变，没有 `mix-blend-mode`。按低端机分级降模糊
那一套也不做（MyGo 没有对应的 API），用固定效果。

**验收口径是「毛玻璃在、内容看得清、不影响读歌词、不影响操作」，**不是**「和 Biu 的截图
逐像素一致」。

### Windows 媒体浮窗没有封面

Windows 的 SMTC 浮窗有歌名、艺人、进度条和播放键，但**没有专辑封面**——构造封面要走
`Windows.Storage.Streams` 的四个抄来的 IID，在 macOS 上**一个都验不了**，而抄错的后果是
「**静默地没封面，其他一切正常**」。macOS 的浮窗有封面。

### 不上架 Mac App Store

MyGo 不实现 App Sandbox，也不管理文件选择的 powerbox。要上架得自己补 sandbox + 文件
选择授权。

### 首发只有 macOS 和 Windows

Linux 不首发。

### 只有扫码登录

密码登录和短信登录都要在 DOM 里跑极验验证码的第三方 JS，原生 UI 做不出来。
**不提议为验证码嵌 webview**——那会推翻「整个应用不用 webview」的决定。

**什么时候用得上**：你在实现一个上游有的功能，先来这里确认「这个我们是不是刻意不做」。
