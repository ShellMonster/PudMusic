#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/.."

ALL_TARGETS=(
  darwin/amd64
  darwin/arm64
  linux/amd64
  linux/arm64
  windows/amd64
  windows/arm64
)
if [ "$#" -gt 0 ]; then
  targets=("$@")
else
  targets=("${ALL_TARGETS[@]}")
fi

read -r slug version out <<EOF
$(python3 - <<'PY'
import json
from pathlib import Path

config = json.loads(Path("mygo.json").read_text())
print(config.get("slug", ""), config.get("version", ""), config.get("out", "build"))
PY
)
EOF

[ -n "$slug" ] || { printf 'mygo.json 没有 slug\n' >&2; exit 1; }
[ -n "$version" ] || { printf 'mygo.json 没有 version\n' >&2; exit 1; }

# platform_label 把 GOOS/GOARCH 翻成资产名里那一段（和
# `rename-release-assets.sh` 里那份同一个约定）。
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

for platform in "${targets[@]}"; do
  case "$platform" in
    darwin/arm64|darwin/amd64|linux/amd64|linux/arm64|windows/amd64|windows/arm64) ;;
    *) printf '不支持的发布目标：%s\n' "$platform" >&2; exit 1 ;;
  esac

  target="${platform//\//-}"
  label="$(platform_label "$platform")"
  dir="$out/$target"
  # ⚠️ 更新归档的名字**分平台**：macOS / Windows 的 tar.gz 被
  # `rename-release-assets.sh` 归成 `update-<target>.tar.gz`（排资产列表
  # 后面），而 **Linux 的 tar.gz 不改名**——它是 `install.sh` 在同目录找的
  # `$slug-$version-$target.tar.gz` 和 `PKGBUILD` 的 `source`（见
  # rename 脚本里那段注释）。清单 `update-<target>.json` 两边同名。
  case "$platform" in
    linux/*) archive="$dir/$slug-$version-$target.tar.gz" ;;
    *)       archive="$dir/update-$target.tar.gz" ;;
  esac
  manifest="$dir/update-$target.json"
  [ -d "$dir" ] || { printf '缺少产物目录：%s\n' "$dir" >&2; exit 1; }
  [ -f "$archive" ] || { printf '%s 缺少更新归档：%s\n' "$target" "$archive" >&2; exit 1; }
  # ⚠️ **未签名的构建没有清单**：`MYGO_UPDATER_PRIVATE_KEY` 没设时 mygo
  # 不打 update-*.json。CI 那一轮永远有；本地 probe 那一档跳过 manifest
  # 断言，但**包本身**（dmg/exe/deb）照样要验——那是用户拿走的东西。
  if [ -f "$manifest" ]; then
    python3 - "$manifest" "$version" "$archive" <<'PY'
import json
import sys
from pathlib import Path

manifest_path, expected_version, expected_archive = sys.argv[1:]
manifest = json.loads(Path(manifest_path).read_text())
if manifest.get("version") != expected_version:
    raise SystemExit(f"{manifest_path} 版本为 {manifest.get('version')!r}，不是 {expected_version!r}")
if not manifest.get("signature"):
    raise SystemExit(f"{manifest_path} 没有更新签名")
if Path(manifest.get("url", "")).name != Path(expected_archive).name:
    raise SystemExit(f"{manifest_path} 指向 {manifest.get('url')!r}，不是 {Path(expected_archive).name!r}")
PY
  fi

  case "$platform" in
    darwin/*)
      package="$dir/PudMusic-$version-$label.dmg"
      [ -f "$package" ] || { printf '%s 缺少 macOS 安装包：%s\n' "$target" "$package" >&2; exit 1; }
      ;;
    windows/*)
      package="$dir/PudMusic-setup-$version-$label.exe"
      [ -f "$package" ] || { printf '%s 缺少 Windows 安装包：%s\n' "$target" "$package" >&2; exit 1; }
      ;;
    linux/*)
      # ⚠️ **deb 和原名 tar.gz 是两份，都要在**：`.deb` 在上面验，
      # tar.gz（`$slug-$version-$target.tar.gz`）在前面 archive 那一档
      # 已经验过——它同时是更新归档、install.sh 的 ARCHIVE 和 PKGBUILD
      # 的 source，所以**不能**改名（见 rename 脚本里那段注释）。
      package="$dir/PudMusic-$version-$label.deb"
      [ -f "$package" ] || { printf '%s 缺少 Debian 安装包：%s\n' "$target" "$package" >&2; exit 1; }
      ;;
  esac

done

printf '已核对 %s 个发布目标：%s\n' "${#targets[@]}" "$(IFS=,; echo "${targets[*]}")"
