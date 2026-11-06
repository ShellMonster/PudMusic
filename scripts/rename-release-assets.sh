#!/usr/bin/env bash
# 把 mygo build 产物重命名成平台友好的名字，并把 update-*.json 清单里的
# url 一并改掉。
#
# 命名（照 tianmu-cli 的约定）：
#
#   darwin/arm64   → Apple-Silicon_macOS
#   darwin/amd64   → Intel_macOS
#   linux/amd64    → Linux_x86_64
#   linux/arm64    → Linux_ARM64
#   windows/amd64  → Windows_x86_64
#   windows/arm64  → Windows_ARM64
#
# 用法：在 `go tool mygo build` 之后、`verify-release-assets.sh` 之前调，
# 传同样的目标列表：
#
#   scripts/rename-release-assets.sh darwin/arm64 windows/amd64 …
#
# ⚠️ **签名不用重算**：manifest 里的 signature 签的是文件**内容**（ed25519
# 对字节流），不是文件名——改文件名不影响验证。改的是 manifest 的 `url`
# 字段里那个**下载地址**（客户端按它去 GitHub release 拉资产），不改的话
# 客户端会按旧名去拉、404。
set -euo pipefail

cd "$(dirname "$0")/.."

# platform_label 把 GOOS/GOARCH 翻成资产名里那一段。
platform_label() {
  case "$1" in
    darwin/arm64)  echo "Apple-Silicon_macOS" ;;
    darwin/amd64)  echo "Intel_macOS" ;;
    linux/amd64)   echo "Linux_x86_64" ;;
    linux/arm64)   echo "Linux_ARM64" ;;
    windows/amd64) echo "Windows_x86_64" ;;
    windows/arm64) echo "Windows_ARM64" ;;
    *) echo "" ;;
  esac
}

# rewrite_manifest 把 update-<target>.json 里所有指到旧文件名的 url 换成新名。
# 只改 url 的**最后一段**（文件名），前面的 releases/download/<tag>/ 不动。
rewrite_manifest() {
  local manifest="$1" old="$2" new="$3"
  python3 - "$manifest" "$old" "$new" <<'PY'
import json, sys
from pathlib import Path

path, old, new = Path(sys.argv[1]), sys.argv[2], sys.argv[3]
m = json.loads(path.read_text())

def fix(u):
    return u[: u.rfind("/") + 1] + new if u.endswith("/" + old) else u

if "url" in m:
    m["url"] = fix(m["url"])
for d in m.get("deltas", []):
    d["url"] = fix(d.get("url", ""))
# previous[] 里的 url 指向**旧版本**的资产（已发布，不能动），别碰。
path.write_text(json.dumps(m, indent=2, ensure_ascii=False) + "\n")
PY
}

for platform in "$@"; do
  label="$(platform_label "$platform")"
  [ -n "$label" ] || { printf '不认得的平台：%s\n' "$platform" >&2; exit 1; }
  target="${platform//\//-}"
  dir="build/$target"
  [ -d "$dir" ] || continue

  # tar.gz 更新归档：清单的 url 指着它。macOS / Windows 把它归到
  # `update-<target>.tar.gz` 一族是为了让安装包（PudMusic-*.dmg /
  # PudMusic-setup-*.exe）在 GitHub 资产列表里排在最前——网页按文件名
  # 字典序排，小写 u 永远在大写 P 之后。
  #
  # ⚠️ **Linux 的 tar.gz 不改名**：它是**同一份文件的两个身份**——更新
  # 归档，也是 `install.sh` 在同目录找的 `$name-$version-$target.tar.gz`
  # 和 `PKGBUILD` 的 `source` 里写的那份。改成 update-* 的话：
  #
  #   - `sh install.sh` 在同目录找不到 `pudmusic-<ver>-linux-<arch>.tar.gz`，
  #     「下载 install.sh + tar.gz」那条安装路径断；
  #   - `makepkg` 按 `source` 找 `pudmusic-<ver>-linux-<arch>.tar.gz`，断。
  #
  # ⇒ Linux 这一支**跳过** tar.gz 改名：清单的 url 本来（mygo 写它时）就
  # 指着 `pudmusic-<ver>-linux-<arch>.tar.gz`，不动它就对。排序让位给
  # 安装路径能走通。
  version="$(python3 -c 'import json;print(json.load(open("mygo.json"))["version"])')"
  slug="$(python3 -c 'import json;print(json.load(open("mygo.json"))["slug"])')"
  archive_old="$dir/$slug-$version-$target.tar.gz"
  archive_new="$dir/update-$target.tar.gz"
  if [[ "$platform" != linux/* ]] && [ -f "$archive_old" ]; then
    mv "$archive_old" "$archive_new"
    # ⚠️ **未签名的构建没有 update manifest**（`MYGO_UPDATER_PRIVATE_KEY`
    # 没设时 mygo 只打 tar.gz、不打 update-*.json）——manifest 在才改 url，
    # 不在就只改名。CI 那一轮永远有 manifest；本地 probe 那一档没有。
    if [ -f "$dir/update-$target.json" ]; then
      rewrite_manifest "$dir/update-$target.json" "$(basename "$archive_old")" "$(basename "$archive_new")"
    fi
  fi

  # delta 差分包：清单 deltas[].url 指着它。同样归 update-* 一族排后。
  for d in "$dir"/*"-to-"*"-$target.delta"; do
    [ -f "$d" ] || continue
    new="$(dirname "$d")/$(basename "$d" | sed -e "s/^${slug}-/update-delta-/" -e "s/-${target}\.delta$/-${label}.delta/")"
    mv "$d" "$new"
    if [ -f "$dir/update-$target.json" ]; then
      rewrite_manifest "$dir/update-$target.json" "$(basename "$d")" "$(basename "$new")"
    fi
  done

  # 安装包 / 压缩包：清单不指它们，纯改名。
  arch="${platform#*/}"
  case "$platform" in
    darwin/*)
      old="$dir/pudmusic-"*"-$arch.dmg"
      for f in $old; do
        [ -f "$f" ] || continue
        mv "$f" "$(dirname "$f")/PudMusic-$version-$label.dmg"
      done
      ;;
    windows/*)
      for f in "$dir"/pudmusic-setup-*-"$arch".exe; do
        [ -f "$f" ] || continue
        mv "$f" "$(dirname "$f")/PudMusic-setup-$version-$label.exe"
      done
      ;;
    linux/*)
      # ⚠️ **只改 .deb**：tar.gz 在上面**故意没动**（见那段注释——mygo 的
      # linux tar.gz 是更新归档和 install.sh/PKGBUILD 入口的同一份，得留着
      # `pudmusic-<ver>-linux-<arch>.tar.gz` 这个原名）。
      for f in "$dir"/"$slug"_"$version"_"$arch".deb; do
        [ -f "$f" ] || continue
        mv "$f" "$(dirname "$f")/PudMusic-$version-$label.deb"
      done
      ;;
  esac
done
