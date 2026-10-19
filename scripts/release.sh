#!/usr/bin/env bash
# 本机一键发版：核版本 → 跑测试 → 重生成 → 打**带签名**的更新包 → 挂一个草稿 Release。
#
#   scripts/release.sh                 # 四个首发目标
#   scripts/release.sh darwin/arm64    # 只发一个
#
# 真正干活的还是 MyGo 自带的 CLI（`go tool mygo build`）。这个脚本做的是它不做、
# 而**漏了不会报错**的那几件：核三处版本号一致、跑验收、重新生成图标与许可清单、
# 检查更新包真的带签名、用 `gh` 把标签推上去并挂成**草稿** Release。
#
# ⚠️ 为什么挂草稿而不是直接发布：草稿里的资产**匿名取不到**，而 MyGo 的更新源是
# 从正式发布的 release 里读清单的。所以这个脚本跑完**还不算发完**，人看过之后要
# 把草稿转正式（`gh release edit v0.1.0 --draft=false`），那一步才是应用开始
# 看得到新版本的那一刻。
#
# ⚠️ 这条链路的私钥在 `~/.config/biu-mygo/mygo-update.key`（能用
# `MYGO_UPDATE_KEY_FILE` 改路径）。**丢了它就再也发不了更新，而且已经装出去的
# 老版本会验不过之后的每一个版本**——备份在哪要写在发版说明里，别只放在本机。
set -euo pipefail

cd "$(dirname "$0")/.."

ALL_TARGETS=(
  darwin/arm64
  darwin/amd64
  windows/amd64
  windows/arm64
)

if [ "$#" -gt 0 ]; then
  targets=("$@")
else
  targets=("${ALL_TARGETS[@]}")
fi

say() { printf '\n\033[1m==> %s\033[0m\n' "$*"; }
die() { printf '\n\033[31m错误：%s\033[0m\n' "$*" >&2; exit 1; }

need() {
  command -v "$1" >/dev/null 2>&1 || die "$2"
}

need go "装 Go 1.27 或更新版本。"
need git "装 git。"
need gh "装 GitHub CLI（brew install gh）并登录：gh auth login"

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

# changelog_section <版本>：打印 CHANGELOG.md 里 `## <版本>` 那一段。
#
# ⚠️ 这段 awk 是 `mygo/internal/update.ReleaseNotes` 的翻版，一个字一处都对上：
# 标题认 `## 0.1.0` 与 Keep a Changelog 的 `## [0.1.0] - 2026-10-19` 两种写法，
# 到下一个 `## ` 停。**两份实现必须一起对**——这里放宽了而 MyGo 没放宽，就会出现
# 「脚本说没问题、build 说没有这一段」。
changelog_section() {
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
  ' CHANGELOG.md
}

# ================================================================ 1 前置检查
say "第 1 步 / 共 5 步：前置检查"

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
[ -n "$publish_repo" ] || die "mygo.json 用的是 updates.url，而这个脚本只走 GitHub 那一路
（第 5 步是 gh release create）。要发到自己的服务器或 S3，得另写一条上传链路。"

# 公开性：MyGo 的更新源要求**公开**仓库——release 资产是给所有人匿名下载的，私有
# 仓库的资产匿名取不到，而**哪一处都不会报错**：应用只是一直查不到更新。
#
# ⚠️ 所以这里是**警告不是拦住**：源码仓库 Biu_Go 现在还是私有，用户已定「就用它
# 本身，以后转公开」。在那之前发出去的版本拿不到更新（见警告里那几行）；转公开
# 之后同一条配置自动就通了，**不用改任何配置**。所以拦下来只会让人以为这条路断了，
# 而它只是还没到通的时候。
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

      这也是为什么第 5 步挂的是草稿：先别发布。
EOF
    ;;
  *) die "$publish_repo 的可见性是 $visibility，认不出来。GitHub 改了取值？" ;;
esac

gh auth status >/dev/null 2>&1 || die "gh 没登录，跑一次 gh auth login。"

# 1.3 CHANGELOG 里有这一版。**三处版本号要对上**：mygo.json 的 version、
# CHANGELOG.md 的段落、待推的 git 标签。
[ -f CHANGELOG.md ] || die "没有 CHANGELOG.md。MyGo 从它的 \`## <版本>\` 段落取更新日志，
  文件不在时应用弹窗里的更新日志是空的。"
notes="$(changelog_section "$version")"
[ -n "$notes" ] || die "CHANGELOG.md 里没有 \`## $version\` 那一段（MyGo 取更新日志的格式）。
  空段落和没有段落对应用是一回事：弹窗里一片空白。"

# 1.4 标签。**已经发过的版本不要重发**：装出去的老版本会以为有更新，清单里的
# 签名和文件却还是上一次那套。已经推到远端且**指向 HEAD** 的标签放行——那是上
# 一次跑到一半留下的。
#
# 「接着走完」这一半在第 5 步：那里的标签推得出去、也有删草稿重挂的续跑路径。
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

# 1.5 签名私钥。**它不在仓库里**，所以每次发版都要现读；读不到就停，不要打出
# 一个没有签名的更新包——那种包用户装不上，而且要等下一次有人报「更新失败」。
key_file="${MYGO_UPDATE_KEY_FILE:-$HOME/.config/biu-mygo/mygo-update.key}"
if [ -z "${MYGO_UPDATER_PRIVATE_KEY:-}" ]; then
  [ -f "$key_file" ] || die "读不到更新签名私钥：$key_file

  **丢了私钥就再也发不了更新**，而且已经装出去的老版本会验不过之后的每一个版本。
  还没有就生成一对（私钥别进仓库）：
    go tool mygo keygen -o \"$(dirname "$key_file")\"
  公钥（同一目录的 mygo-update.pub）要写进 mygo.json 的 updates.publicKey。
  别的地方存：MYGO_UPDATE_KEY_FILE=/path/to/mygo-update.key scripts/release.sh"
  MYGO_UPDATER_PRIVATE_KEY="$(cat "$key_file")"
  export MYGO_UPDATER_PRIVATE_KEY
fi

# Windows 的 NSIS 安装包要 makensis，理由和 scripts/build.sh 一样：MyGo 遇到没有它
# 会跳过安装包只打一行提示，脚本「成功」了而产物里没有 .exe。
for t in "${targets[@]}"; do
  case "$t" in
    windows/*)
      command -v makensis >/dev/null 2>&1 || die "要打 Windows 安装包就需要 NSIS（makensis），这台机器上没有。
  macOS:  brew install nsis
  Debian: sudo apt install nsis
  不想装的话，只发一个目标：scripts/release.sh darwin/arm64"
      ;;
  esac
done

printf '  版本 %s（标签 %s）→ %s\n' "$version" "$tag" "$publish_repo"
printf '  目标 %s\n' "$(IFS=,; echo "${targets[*]}")"
printf '  私钥 %s\n' "$key_file"

# ================================================================== 2 验收
say "第 2 步 / 共 5 步：跑验收"
# ⚠️ **不带 -race**：全量 race 在这台 10 核机器上要十几到二十分钟（长板是
# internal/service/audio、.../flacdec、internal/platform），放进发版链路会让每一次
# 发版都等到超时。真要查并发单独挑那几个包跑。
go build ./... && go vet ./... && go test ./...

# ============================================================== 3 重新生成
say "第 3 步 / 共 5 步：重新生成图标与第三方许可清单"
# 图案和清单都是算出来的生成物。**改了图案或依赖却忘了重新生成，仓库里躺着的是
# 旧文件，而没有任何一处会报错**——所以这里跑一遍并检查 git diff。
go generate ./internal/platform/ ./internal/licensing/
if ! git diff --quiet; then
  git --no-pager diff --stat >&2
  die "生成出来的东西和提交的不一致，把上面这些改动提交了再跑一次。"
fi

# ============================================================== 4 打更新包
say "第 4 步 / 共 5 步：打带签名的更新包"
platforms="$(IFS=,; echo "${targets[*]}")"
# ⚠️ 不公证、不签名 Developer ID：那一票还没定（mygo.json 里没有
# macos.signingIdentity，是 ad hoc）。这一条链只管自动更新。
go tool mygo build -platform "$platforms"

# 收集产物，并且**核每一个目标都真的产出了带签名的清单**。
#
# 这是整个脚本最要紧的一道检查：没有私钥时 MyGo 不报错，它只是不打更新文件，
# 于是产物里躺着一个永远不会更新的安装包，而签收的人要等用户报障才发现。
assets=()
for t in "${targets[@]}"; do
  target="${t/\//-}"
  dir="build/$target"
  [ -d "$dir" ] || die "没有产物目录 ${dir}——第 4 步说它打完了，这个目录却没有。"

  manifest="$dir/update-$target.json"
  [ -f "$manifest" ] || die "$target 没有 update-$target.json——更新包没打出来。

  最常见的原因是没有私钥：那时 MyGo 照打安装包、不报任何错，只是不产出更新文件。
  确认 MYGO_UPDATER_PRIVATE_KEY 有值（它必须是 base64 的私钥本身，不是路径）。"

  # 清单里的签名必须是空的这一条：清单在而签名空，等于发出去一个谁都能改的更新源。
  sig="$(json_file_get "$manifest" signature)"
  [ -n "$sig" ] || die "$target 的清单里 signature 是空的——它没被签名。
  别把这种包发出去：应用会装一个校验不过的更新。"
  manifest_version="$(json_file_get "$manifest" version)"
  [ "$manifest_version" = "$version" ] ||
    die "$target 的清单里版本是 ${manifest_version}，不是 ${version}。"

  while IFS= read -r f; do
    [ -n "$f" ] && assets+=("$f")
  done < <(find "$dir" -maxdepth 1 -type f \
    \( -name '*.tar.gz' -o -name '*.delta' -o -name '*.dmg' -o -name '*Setup*.exe' \) -print | sort)
  assets+=("$manifest")
done

say "产物"
for f in "${assets[@]}"; do
  printf '  %-56s %6s\n' "$f" "$(du -h "$f" | cut -f1)"
done

# ========================================================== 5 草稿 Release
say "第 5 步 / 共 5 步：推标签并挂草稿 Release"

notes_file="$(mktemp)"
trap 'rm -f "$notes_file"' EXIT
printf '%s\n' "$notes" >"$notes_file"

# 本地打标签再推，而不是让 gh 顺手建一个：gh 默认按**远端默认分支**的 HEAD 建标签，
# 而我们要的是本机 HEAD——两者不一定同一个提交。标签冲突要在推之前就撞出来。
if git rev-parse -q --verify "refs/tags/$tag" >/dev/null; then
  say "标签 $tag 已经在本地（指向 HEAD），直接推"
else
  git tag "$tag" HEAD
fi
git push origin "$tag"

# 重跑要先清掉上一次留下的草稿：`gh release create` 在同一个 tag 上已有 release 时
# 直接失败（没有 --clobber，那是 gh release upload 的选项），而标签在上面已经推上去了
# ——于是第 1.4 步说的「重跑要能接着走完」在 release 这一半原本是假的。
#
# 只删**草稿**：已发布的 release 是发出去过的东西，删它等于撤一次发版，那不是
# 「重跑」而是另一个决定，脚本没有资格替人做。
existing_is_draft() { gh release view "$tag" --repo "$publish_repo" --json isDraft -q .isDraft 2>/dev/null; }

if state="$(existing_is_draft)"; then
  case "$state" in
    true)
      say "删掉上一次留下的草稿 Release $tag，重挂一遍"
      gh release delete "$tag" --repo "$publish_repo" --yes
      ;;
    false)
      die "$tag 的 Release 已经**发布**了，不再重挂。
  这一版已经发出去过，重挂会让装着的用户拿到一份对不上的更新。
  要改这一版就把 mygo.json 的 version 升到下一个版本。"
      ;;
    *)
      die "查不到 $tag 的 Release 状态（gh 报的是 $state）。"
      ;;
  esac
fi

# --verify-tag：标签已经推上去了，别让 gh 再建一个。
# --draft：**不正式发布**。草稿里的资产匿名取不到，应用看不到新版本——这是有意的，
# 先让人看过再发。
gh release create "$tag" \
  --repo "$publish_repo" \
  --verify-tag \
  --draft \
  --title "$tag" \
  --notes-file "$notes_file" \
  "${assets[@]}"

say "草稿 Release 已挂上（还没发布）"
cat <<EOF
  gh release view "$tag" --repo $publish_repo
  gh release edit "$tag" --repo $publish_repo --draft=false   # 确认之后才发出去

  ⚠️ 正式发出去之前，它只是一份草稿：资产匿名取不到，应用查不到新版本。
  ⚠️ 私钥 $key_file —— 备份在哪请写进发版说明，别只留在本机。
     丢了它就再也发不了更新，而且已经装出去的老版本会验不过之后的每一个版本。
EOF
