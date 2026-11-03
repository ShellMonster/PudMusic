#!/usr/bin/env bash
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
    windows/amd64) echo "Windows_x86_64" ;;
    windows/arm64) echo "Windows_ARM64" ;;
    *) echo "" ;;
  esac
}

for platform in "${targets[@]}"; do
  case "$platform" in
    darwin/arm64|darwin/amd64|windows/amd64|windows/arm64) ;;
    *) printf '不支持的发布目标：%s\n' "$platform" >&2; exit 1 ;;
  esac

  target="${platform//\//-}"
  label="$(platform_label "$platform")"
  dir="$out/$target"
  # ⚠️ **归档/安装包用品牌名 PudMusic**，不是 mygo.json 的 slug（slug 只能小写）。
  archive="$dir/PudMusic-$version-$label.tar.gz"
  manifest="$dir/update-$target.json"
  [ -d "$dir" ] || { printf '缺少产物目录：%s\n' "$dir" >&2; exit 1; }
  [ -f "$archive" ] || { printf '%s 缺少更新归档：%s\n' "$target" "$archive" >&2; exit 1; }
  [ -f "$manifest" ] || { printf '%s 缺少更新清单：%s\n' "$target" "$manifest" >&2; exit 1; }

  case "$platform" in
    darwin/*)
      package="$dir/PudMusic-$version-$label.dmg"
      [ -f "$package" ] || { printf '%s 缺少 macOS 安装包：%s\n' "$target" "$package" >&2; exit 1; }
      ;;
    windows/*)
      package="$dir/PudMusic-setup-$version-$label.exe"
      [ -f "$package" ] || { printf '%s 缺少 Windows 安装包：%s\n' "$target" "$package" >&2; exit 1; }
      ;;
  esac

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

done

printf '已核对 %s 个发布目标：%s\n' "${#targets[@]}" "$(IFS=,; echo "${targets[*]}")"
