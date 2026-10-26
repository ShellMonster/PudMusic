# B 站评论接口的实测形状

**2026-10-26 只读探针**（研究票 #537 的后续实测，任务 BMG-299）。只发过
`GET /x/v2/reply/main` 与 `GET /x/v2/reply/reply`——**没有碰任何写接口**
（没发评论、没点赞评论）。

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
- **`/x/v2/reply/wbi/main` 少了 WBI 签名就 -403** ⇒ 用不带 WBI 的
  `/x/v2/reply/main`。
- **匿名可用**：全程没有 SESSDATA，三个接口都正常返回。

## 这些 fixture 证明不了什么

它们证明的是**协议形状对不对**（字段名、类型、层级）。它们**证明不了**
「B 站现在还这么返回」——那要真机回归（票 #575）。
