# B 站评论接口的实测形状

**2026-10-26 只读探针**（研究票 #537 的后续实测，任务 BMG-299）+
**2026-10-27 真机回归**（票 #575，任务 BMG-303）。只发过
`GET /x/v2/reply/main`、`GET /x/v2/reply/wbi/main` 与 `GET /x/v2/reply/reply`
——**没有碰任何写接口**（没发评论、没点赞评论）。

## 这些文件是什么

**真接口返回的形状**，不是照着 `bili.Comment` 那个结构体造的。这两件事的区别
很要紧：照着自己写的结构体造 fixture，验的是「我们读得懂我们造的东西」；
而真返回里有大量我们**用不上但存在**的字段（`vip` / `pendant` / `nameplate` /
`folder` / `up_action` / `assist` / `blacklist` / `vote` / `callbacks` …），
照着自己造的话，B 站改字段名时这边不会红。

每一份里挑了**六条**评论，凑齐了下面这几档：

| 文件 | 里面有什么 |
|---|---|
| `reply-main.json` | 最热档（`mode=3`）。含**一条 UP 主自己发的评论**（`大圆镜科普`，mid = `upper.mid`）、一条有认证的（`official_verify.type=0`）、带 `emote` 的、带 `pictures` 的、带着 `up_like` 的 |
| `reply-main-latest.json` | 最新档（`mode=2`）。含带 `emote` 和 `pictures` 的 |
| `reply-main-upreply.json` | 最热档。含**带 `reply_control.up_reply=true`** 的那几条——**它们不是 UP 主发的**（见下） |
| `reply-main-empty.json` | **一条评论都没有**的那一档（音乐库音频 `type=14` / oid=563526）：`code 0` + 空 `replies` + `cursor.is_end=true` |
| `reply-reply.json` | 楼中楼（`/x/v2/reply/reply`）。含 `page.count`（那一层的总回复数） |

## ⚠️ 两条从真返回里看出来的、容易搞错的事

### 1. `reply_control.up_reply` **不是**「这条是 UP 主发的」

实测（oid=114256632618805）：`data.upper.mid` = `654552`，而带
`up_reply=true` 的那几条评论 mid 是 `294067289` / `594810944` / …——
**没有一条等于 UP 主**。

`up_reply` 的意思是「**UP 主回复过这一条**」。判「这条是 UP 主发的」要靠
**`mid == data.upper.mid`**（见 `bili.Comment.IsUpper`）。

### 2. `content.pictures[].img_size` **是小数**，而且有时是整数

实测同一个字段：`283.87` / `109.92` / `236.8` / `57.02`（小数）与 `57`（整数）。

按 `int` 解的话，`encoding/json` 撞上类型对不上会让**整个反序列化失败**——
于是「一条评论带一张图」变成「**整页 30 条评论一条都读不出来**」，而界面上只是
一个空列表（没有报错、没有提示、什么都没有）。

⇒ 用 `bili.flexInt` 解它（`flexjson.go` 里那个「读不出来就当零值、**绝不**
报错」的类型）。这跟 `flexjson.go` 文件头记的是同一类事故（票 #300：
`pub_ts` 从数字变成字符串，于是 UP 空间那一页永远转圈）。

## 其余几个实测结论（代码里也写着）

- **头像在 `member.avatar`，不在 `face`**——`face` 这个键**压根不存在**。
- **`member.official_verify.type`**：`-1` = 无认证（**不是 0**），`0` = 个人认证，
  `1` = 机构认证。
- **`reply_control.time_desc`** 是现成的中文相对时间（「7小时前发布」）——
  **不用自己算**。
- **`reply_control.sub_reply_entry_text`** 是现成的回复数文案（「共52条回复」）。
- **`cursor.next` 是「不透明游标」，含义随排序变**：最热档是**页码**
  （1 → 2 → 3 …），最新档是**楼层号**（160595 → 160592 → …，递减）。
  ⇒ 原样带回去，**别自己加一**。
- **`cursor.mode` 跟请求里的 `mode` 未必一样**：实测请求 `mode=0` 时 B 站回的
  是 `cursor.mode=3`（热门评论）。界面上写「最新评论」要认 `cursor.mode`。
- **`ps` 上限 30**（主列表）/ **20**（楼中楼）：超了 B 站**不报错**，只是默默
  截断——翻页从第二页开始会漏掉一批，而界面上看不出任何异样。
- **`buvid3` 会把列表砍到 3 条**并宣称「到底了」（`cursor.is_end=true`）。
  实测同一 URL、同一客户端，只改 Cookie 头：

  | 请求头带的 Cookie | 拿到的条数 | `cursor.is_end` |
  |---|---|---|
  | 不带 | **20** | false |
  | `buvid3=<真值>` | **3** | **true** |

  ⇒ 评论这一处**绝对不能开** `bili.Call.Buvid`。

  ⚠️ **「3 条」是「没有登录态」时的数**（2026-10-27 真机回归实测，同一个
  URL、同一枚真 buvid3）：

  | 客户端 | 拿到的条数 | `cursor.is_end` |
  |---|---|---|
  | 匿名 + `buvid3` | **3** | **true** |
  | 假 `SESSDATA` + `buvid3` | **3** | **true** |
  | **真登录态** + `buvid3` | **20** | false |

  ⇒ 判据是**登录态成不成立**，不是「Cookie 头里有没有 `SESSDATA` 这个名字」。
  ⇒ **`NoBuvid: true` 仍然是对的**（往严的那边退），但别改成「要带」。

  ⚠️ **光「不开」还不够**：jar 里的 cookie 是在 `http.Client.send` 里加的，
  而那**在运输层之前**（`net/http/client.go`：`c.Jar.Cookies(url)` →
  `req.AddCookie` → `RoundTrip`）——所以代码里没写 `Buvid: true` 也**挡不住**
  jar 里已经有一枚 buvid3。而 jar 里**确实**会有：登录成功时 B 站下发的
  **全部** cookie 都被存下来（`app.Account.finish` 收下 `IssuedCookies()` 的
  每一条），B 站的 cookie 域是 `.bilibili.com`，`api.bilibili.com` 收得到。

  ⇒ 评论请求显式写 **`bili.Call.NoBuvid: true`**：它在**运输层**把 buvid
  那一族从 Cookie 头里摘掉（那里 jar 已经加好了），而**登录态照带**
  （SESSDATA / bili_jct 一个都不动）。
- **`/x/v2/reply/wbi/main` 少了 WBI 签名就 -403** ⇒ 用不带 WBI 的
  `/x/v2/reply/main`。
- **匿名可用**：全程没有 SESSDATA，三个接口都正常返回。

## 🔴 2026-10-27 真机回归新发现的三条（**都不是这批 fixture 能证明的**）

### 1. 「评论」那一档（`mode=1`）翻不了页，且与「最新评论」是同一份内容

- 带 `cursor.next` 翻第二页 ⇒ **`-400 min_score(1.000000) must be <= max_score(0.000000)`**
  （`mode=3` / `mode=2` 正常）。**通病**：视频（`type=1`）和音乐库音频
  （`type=14`）都一样。
- `cursor.support_mode` **恒为 `[2, 3]`，不含 1** ⇒ B 站已经不支持 mode=1
  这一档的游标了。
- 改用 `cursor.pagination_reply.next_offset` + `pagination_str={"offset":"…"}`
  ⇒ **三档都能翻**（实测）。
- `mode=1` 与 `mode=2` 的内容**逐条相同**（第 1、2 页都比过 rpid）。
  ⇒ 票 #591。

### 2. 评论区被关掉的码现在是 **12002**，不是 12061

实测（视频 oid=455017341、音乐库音频 oid=1187010）**所有**评论接口都回
**12002「当前页面评论功能已关闭」**（`main` / `wbi/main` / 楼中楼 / 老接口
都试了）。12061 一次都没出现。⇒ 票 #592。

### 3. 字段：条件出现的那些键，实测形状是「**压根没有那个键**」

2026-10-27 真机回归一共采到 **1064 条**评论（89 份真响应）：

| 键 | 在场 | 缺席 |
|---|---|---|
| `replies[].rpid/mid/root/parent/count/rcount/like/ctime` | 1064 | 0 |
| `replies[].member.{mid,uname,avatar,level_info.current_level,official_verify.type,official_verify.desc}` | 1064 | 0 |
| `replies[].member.face` | **0** | 1064（这个键压根不存在） |
| `replies[].content.message` | 1064 | 0 |
| `replies[].reply_control.time_desc` | 1064 | 0 |
| `replies[].content.emote` | 253 | 811（**条件出现**） |
| `replies[].content.pictures` | 47 | 1017（**条件出现**） |
| `replies[].reply_control.location` | 254 | 810（**条件出现**，不是空串） |
| `replies[].reply_control.sub_reply_entry_text` | 608 | 456（**条件出现**） |
| `replies[].reply_control.up_reply` | 4 | 1060（**条件出现**） |
| `cursor.{next,is_end,mode,name,pagination_reply,support_mode}` | 85 | 4 |
| `cursor.all_count` | 66 | 19（**全都是空列表那一档**） |

- ⚠️ **`cursor.all_count` 在「一条评论都没有」的那一档**（`code 0` + 空
  `replies` + `is_end=true`）**压根没有这个键**——不是 0。
- `content.pictures[].img_size` **仍然是 float**（实测 30 个 float / 17 个 int）
  ⇒ 继续用 `bili.flexInt`，**别改回 int**。
- `content.emote` 每一项的形状（实测 259 项）：
  `{id, package_id, state, type, attr, text, url, meta:{size,suggest}, mtime, jump_title}`
  ⇒ `bili.CommentEmote` 的字段名都对。
- `reply_control.up_reply` **还在**（2026-10-26 那批 fixture 里也有），
  但**按内容出现**：oid=114256632618805 的 mode=3 有 4 条带这个键，而
  oid=170001 的 1064 条一条都没有。
- ⚠️ **`mid == data.upper.mid` 这一条这轮没被真数据触发**：1064 条里
  **0 条**的 `mid` 等于 `upper.mid`（没有 UP 主自己发评论的样本）。
  ⇒ `IsUpper` 的判据仍然只有 fixture 证明，**没有真机证明**。

## 这些 fixture 证明不了什么

它们证明的是**协议形状对不对**（字段名、类型、层级）。票 #575 的真机回归
（2026-10-27）证明了「B 站现在还这么返回」——但**它也有没覆盖到的地方**：

- 真实用户的多设备 / 地域差异没测（只在本机一处网络跑）。
- `mid == upper.mid`（「这条是 UP 主发的」）**没被真数据触发**。
- 没找到「评论多到要翻好几页的 `type=14` 音乐库音频」——只找到 4 条的那种。
- 风控：并发打同一个 URL 会回 **HTTP 412**（B 站按 IP 限速），串行隔
  6~8 秒就 0 次。**412 是探针自己打太快，不是接口行为**。
