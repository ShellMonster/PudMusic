#!/usr/bin/env bash
# 发版入口：本地只做**前置检查**，然后打 tag 推上去——真正的构建、签名、
# 挂草稿 Release 都在 CI（.github/workflows/release.yml）里做。
#
#   scripts/release.sh
#
# 为什么这么拆：本地和 CI 同时构建上传会让 release 上躺两套文件名（mygo 原名 +
# 品牌名），而 CI 末尾一步 `gh release edit --draft` 还会把已发布的 release
# 误拖回草稿。链路只留 CI 一条，本地只保证「tag 打在对的代码上、该核的都核过」。
#
# ⚠️ 这个脚本跑完**还不算发完**：tag 推上去会触发 CI 构建并挂**草稿**，人看过
# 草稿里的资产和说明之后，把它转正式（`gh release edit <tag> --draft=false`）
# 才是应用开始看得到新版本的那一刻——草稿资产匿名取不到。
#
# ⚠️ 签名私钥在 repo secret `MYGO_UPDATER_PRIVATE_KEY`，不在本机。
set -euo pipefail

cd "$(dirname "$0")/.."

say() { printf '\n\033[1m==> %s\033[0m\n' "$*"; }
die() { printf '\n\033[31m错误：%s\033[0m\n' "$*" >&2; exit 1; }

need() {
  command -v "$1" >/dev/null 2>&1 || die "$2"
}

need go "装 Go 1.27 或更新版本。"
need git "装 git。"
need gh "装 GitHub CLI（brew install gh）并登录：gh auth login"

# ------------------------------------------------- 自检：别带着已知的解析坑去发版
#
# **`${变量}` 后面紧跟一个非 ASCII 字符，会让 bash 3.2（本机唯一的 bash，/usr/bin/env
# bash 就是它）把那个字符的头一个字节并进变量名**——`$变量` + 全角逗号会解析成
# 一个不存在的 `变量…`，`set -u` 下当场崩在那一行。
#
# 危险的不是崩，是**崩在哪**：万一崩在 tag 已经推出去之后，就留下一个
# 「有标签、CI 已在跑」的半截发版，而 `bash -n` 查不出来（语法是对的）。
#
# 所以动手之前先扫一遍自己。不用 `grep -P`（BSD grep 未必有），用 LC_ALL=C 下的
# `[^ -~]`：它只看字节，落在可打印 ASCII 之外就算，正好是「非 ASCII」。
if bad_lines="$(LC_ALL=C grep -n '\$[A-Za-z_][A-Za-z_0-9_]*[^ -~]' "$0")" && [ -n "$bad_lines" ]; then
  printf '\033[31m错误：脚本自己有一处 bash 3.2 解析不了的变量：\033[0m\n%s\n' "$bad_lines" >&2
  die '把上面几行的变量加上花括号：变量名后面紧跟中文时，bash 3.2 会把那个字的第一个
  字节并进变量名，set -u 下当场崩——而且可能崩在标签已经推出去之后。
  最小复现：bash -c '"'"'set -u; tag=v; echo "$tag 的"'"'"' 正常；把 tag 后面那个空格
  换成全角逗号，就变成 bash: tag…: unbound variable。'
fi

# ---------------------------------------------------------------- 读 mygo.json
# jq 有就用 jq，没有就退到 python3。两个都是现成的，没有第三个选择——而 JSON 里
# 有密码学材料和仓库名，用正则去抠字符串就是给自己埋一个静默的坑。
if command -v jq >/dev/null 2>&1; then
  json_tool=jq
elif command -v python3 >/dev/null 2>&1; then
  json_tool=python3
else
  die "读 mygo.json 要 jq 或 python3，两个都没有。装一个：brew install jq"
fi

# json_get <点号路径>：取不到就打印空，不报错。
json_get() {
  if [ "$json_tool" = jq ]; then
    jq -r "$1 // empty" mygo.json
  else
    python3 - "$1" <<'PY'
import json, sys
node = json.load(open("mygo.json"))
for part in sys.argv[1].lstrip(".").split("."):
    if isinstance(node, dict) and part in node:
        node = node[part]
    else:
        sys.exit(0)
if isinstance(node, (str, int, float)):
    print(node)
PY
  fi
}

# json_file_get <文件> <顶层键>：取那个键的值，取不到或不是字符串就打印空。
json_file_get() {
  if [ "$json_tool" = jq ]; then
    jq -r ".$2 // empty" "$1"
  else
    python3 - "$1" "$2" <<'PY'
import json, sys
try:
    node = json.load(open(sys.argv[1]))
except Exception:
    sys.exit(0)
if isinstance(node, dict) and isinstance(node.get(sys.argv[2]), str):
    print(node[sys.argv[2]])
PY
  fi
}

# md_section <版本> <文件>：打印那份 md 里 `## <版本>` 那一段。
#
# ⚠️ 这段 awk 是 `mygo/internal/update.ReleaseNotes` 的翻版，一个字一处都对上：
# 标题认 `## 0.1.0` 与 Keep a Changelog 的 `## [0.1.0] - 2026-10-19` 两种写法，
# 到下一个 `## ` 停。**两份实现必须一起对**——这里放宽了而 MyGo 没放宽，就会出现
# 「脚本说没问题、build 说没有这一段」。
#
# CHANGELOG.md 和 RELEASE-NOTES.md 用同一条规则取段——两份都是 `## <版本>` 起头。
md_section() {
  awk -v want="$1" '
    /^## / {
      if (in_) exit
      f = $2
      if (f ~ /^\[/) { sub(/^\[/, "", f); sub(/\].*$/, "", f) }
      sub(/^v/, "", f)
      in_ = (f == want)
      next
    }
    in_ { print }
  ' "$2"
}

# ================================================================ 1 前置检查
say "第 1 步 / 共 2 步：前置检查"

# 1.1 工作区干净。发版打的是 HEAD 上那一份代码，脏工作区打出来的产物和标签对不上。
if [ -n "$(git status --porcelain)" ]; then
  git status --short >&2
  die "工作区不干净，先提交或 stash。产物要能对应到标签指的那一个提交。"
fi

version="$(json_get .version)"
[ -n "$version" ] || die "mygo.json 里没有 version。"
tag_prefix="$(json_get .updates.tagPrefix)"
[ -n "$tag_prefix" ] || tag_prefix=v
tag="$tag_prefix$version"

# 1.2 更新源与公钥。**两者都缺时 MyGo 自己也会报错**，但它报的是英文，而这里是
# 发版那一刻最该看到的一句中文。
public_key="$(json_get .updates.publicKey)"
publish_repo="$(json_get .updates.github)"
update_url="$(json_get .updates.url)"

[ -n "$public_key" ] || die "mygo.json 里没有 updates.publicKey。
  没有它打出来的应用永远停在「这个版本不能自动更新」，而**不会报任何错**。
  公钥来自 mygo keygen 生成的 mygo-update.pub。"

if [ -z "$publish_repo" ] && [ -z "$update_url" ]; then
  die "mygo.json 里 updates.github 和 updates.url 都是空的——没有更新源。
  两个字段二选一：updates.github 是放更新的**公开**仓库（owner/name），
  updates.url 是放更新的 HTTPS 目录。"
fi
[ -n "$publish_repo" ] || die "mygo.json 用的是 updates.url，而这条链路只走 GitHub
（CI 里是 gh release create）。要发到自己的服务器或 S3，得另写一条上传链路。"

# 公开性：MyGo 的更新源要求**公开**仓库——release 资产是给所有人匿名下载的，私有
# 仓库的资产匿名取不到，而**哪一处都不会报错**：应用只是一直查不到更新。
#
# 当前更新源是公开的 ShellMonster/PudMusic。这里仍保留检查，是为了防止将来把更新
# 源切回私有仓库后无意间发布一个用户无法自动更新的版本。
#
# 设 MYGO_RELEASE_ALLOW_PRIVATE=1 才走这一步——不是怕误操作，是让「我知道这条路
# 还没通，仍然要发」这个决定落在纸上，而不是落在某次心照不宣的放行里。
visibility="$(gh repo view "$publish_repo" --json visibility -q .visibility 2>/dev/null || true)"
case "$visibility" in
  PUBLIC) ;;
  "") die "取不到仓库 $publish_repo 的可见性——仓库名写错了，或者 gh 没权限。" ;;
  PRIVATE|INTERNAL)
    if [ "${MYGO_RELEASE_ALLOW_PRIVATE:-}" != "1" ]; then
      die "$publish_repo 是私有仓库，release 资产匿名取不到——应用查更新走的是匿名
  HTTPS 请求（https://github.com/$publish_repo/releases/latest/download/…），
  私有仓库拿不到，而不会报任何错。

  确实要发（源码仓库反正会转公开，先把包打出来放着）就加一句：
    MYGO_RELEASE_ALLOW_PRIVATE=1 scripts/release.sh

  仓库转公开之后什么都不用改，同一条配置自己就通了。"
    fi
    printf '\n\033[33m⚠️  %s 还是私有仓库，这次发出去的版本**别人拿不到更新**。\033[0m\n' "$publish_repo"
    cat <<EOF
      release 资产是匿名下载的，私有仓库的取不到：应用查更新走的是
      https://github.com/$publish_repo/releases/latest/download/update-<平台>.json
      这条匿名请求，它会一直拿到 404——而应用和日志都不会报错。

      仓库转公开之后同一条配置自动就通了，**不用改任何配置**，也不用重发。

      这也是为什么 CI 挂的是草稿：先别发布。
EOF
    ;;
  *) die "$publish_repo 的可见性是 ${visibility}，认不出来。GitHub 改了取值？" ;;
esac

gh auth status >/dev/null 2>&1 || die "gh 没登录，跑一次 gh auth login。"

# 1.3 两份说明都要齐：CHANGELOG 是给开发者的账（GitHub Release 说明从它取，
# 见 generate-release-notes.sh），RELEASE-NOTES 是给用户的信（应用内更新弹窗
# 从它取——mygo.json 的 updates.changelog 指着它，build 时写进 update 清单）。
# 三处版本号要对上：mygo.json 的 version、两份 md 的 `## <版本>` 段、待推的 git 标签。
[ -f CHANGELOG.md ] || die "没有 CHANGELOG.md。那是给开发者的版本账，发版节奏靠它核版本号。"
[ -f RELEASE-NOTES.md ] || die "没有 RELEASE-NOTES.md。应用内更新弹窗的说明取它的 \`## <版本>\` 段。"
[ -n "$(md_section "$version" CHANGELOG.md)" ] \
  || die "CHANGELOG.md 里没有 \`## $version\` 那一段。先记一笔开发者账再发版。"
notes="$(md_section "$version" RELEASE-NOTES.md)"
[ -n "$notes" ] || die "RELEASE-NOTES.md 里没有 \`## $version\` 那一段。应用内更新弹窗
  展示的就是它，发出去之前先写一段给用户的更新说明。"

# 1.4 标签。**已经发过的版本不要重发**：装出去的老版本会以为有更新，清单里的
# 签名和文件却还是上一次那套。已经推到远端且**指向 HEAD** 的标签放行——那是上
# 一次跑到一半留下的。
#
# 「接着走完」这一半在 CI：tag 推上去会触发构建并挂草稿，草稿存在时 CI 会清掉
# 旧资产重传，是幂等的。
# 这里只管住一件事——**别把标签打在和已有标签不同的提交上**，那是重发。
head_sha="$(git rev-parse HEAD)"
if tag_sha="$(git rev-list -n1 "$tag" 2>/dev/null)"; then
  [ "$tag_sha" = "$head_sha" ] ||
    die "本地标签 $tag 指向 ${tag_sha}，不是 HEAD（${head_sha}）。
  发版标签要打在发出去的那一份代码上。git tag -f $tag $head_sha 之后再跑。"
else
  if remote_sha="$(git ls-remote --tags origin "refs/tags/$tag" 2>/dev/null | head -1 | cut -f1)"; then
    [ -n "$remote_sha" ] || remote_sha="$(git ls-remote --tags origin "refs/tags/$tag^{}" 2>/dev/null | head -1 | cut -f1)"
  fi
  if [ -n "$remote_sha" ]; then
    [ "$remote_sha" = "$head_sha" ] ||
      die "远端已经有标签 ${tag}，它指向 ${remote_sha}，不是 HEAD（${head_sha}）。
  这一版看起来已经发过了，而代码又动了。要么回到标签那一个提交，要么把
  mygo.json 的 version 升到下一个版本。"
    git fetch --tags --quiet
  fi
fi

# 1.4b HEAD 在默认分支上吗。**不在就提醒，不拦**——从发布分支发版是有人会做的事。
#
# 之所以值得提醒：这个仓库是 **squash 合并**（PR 的合并提交就是压过的那一个），
# 所以在功能分支上打的标签，合并之后**永远不在 main 上**——标签指向一份 main 里
# 根本不存在的代码。症状是发布出去的产物和 main 对不上，而哪一处都不报错。
if ! git merge-base --is-ancestor "$head_sha" origin/main 2>/dev/null; then
  printf '\n\033[33m⚠️  HEAD 不在 origin/main 上，标签会指向 main 里没有的提交。\033[0m\n'
  cat <<EOF
    标签 $tag 会打在 ${head_sha}，而它不是 origin/main 的祖先。
    本仓库是 squash 合并，所以合完之后 main 会有另一个提交 SHA，而标签还指着这里。

    要发布的是 main 上那一份，就切到 main 再跑一次 scripts/release.sh；
    已经打过标签的话，先把它挪走：
      git tag -f $tag origin/main && git push -f origin $tag
      gh release edit $tag --repo $publish_repo --target main
EOF
fi

# ============================================================ 2 打 tag 并推送
say "第 2 步 / 共 2 步：打标签并推送（CI 接手构建与发布）"

# 本地打标签再推，而不是让 gh 顺手建一个：gh 默认按**远端默认分支**的 HEAD 建标签，
# 而我们要的是本机 HEAD——两者不一定同一个提交。标签冲突要在推之前就撞出来。
if git rev-parse -q --verify "refs/tags/$tag" >/dev/null; then
  say "标签 $tag 已经在本地（指向 HEAD），直接推"
else
  git tag "$tag" HEAD
fi
git push origin "$tag"

say "标签已推送，CI 会接管剩下的：构建 → 重命名 → 校验 → 挂草稿 Release"
cat <<EOF
  看 CI 进度：  gh run list --repo $publish_repo --workflow release.yml --limit 3
  看草稿：      gh release view "$tag" --repo $publish_repo

  CI 跑完会留下一份**草稿** Release。人看过资产和说明之后转正式：
    gh release edit "$tag" --repo $publish_repo --draft=false

  ⚠️ 正式发出去之前，它只是一份草稿：资产匿名取不到，应用查不到新版本。
EOF
