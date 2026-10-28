#!/usr/bin/env bash
set -euo pipefail

# 根据 tag 对应的提交范围，从 CHANGELOG 生成 GitHub Release 说明。
# 用法：scripts/generate-release-notes.sh <tag> [output]

cd "$(dirname "$0")/.."

tag="${1:-${GITHUB_REF_NAME:-}}"
out="${2:-release-notes.md}"

if [ -z "$tag" ]; then
  printf '用法：%s <tag> [output]\n' "$0" >&2
  exit 2
fi

version="${tag#v}"
slug="$(python3 - <<'PY'
import json
print(json.load(open('mygo.json'))['slug'])
PY
)"

[ -n "$slug" ] || { printf 'mygo.json 中没有 slug\n' >&2; exit 1; }

commit="$(git rev-parse "$tag^{commit}")"
previous=""
if previous_commit="$(git rev-parse -q --verify "$tag^{}")"; then
  previous="$(git describe --tags --abbrev=0 "$previous_commit^" 2>/dev/null || true)"
fi

range="$commit"
if [ -n "$previous" ]; then
  range="$previous..$commit"
fi

mkdir -p "$(dirname "$out")"

changelog_section() {
  awk -v want="$version" '
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

section="$(changelog_section)"
if [ -z "${section//[[:space:]]/}" ]; then
  printf 'CHANGELOG.md 中没有 ## %s 段落\n' "$version" >&2
  exit 1
fi

count_all="$(git rev-list --count --no-merges "$range")"
count_kind() {
  case "$1" in
    feat) git log --format='%s' --no-merges "$range" | awk '/^feat(:|\(| )/ || /^feature(:|\(| )/ { n++ } END { print n + 0 }' ;;
    fix) git log --format='%s' --no-merges "$range" | awk '/^fix(:|\(| )/ { n++ } END { print n + 0 }' ;;
    docs) git log --format='%s' --no-merges "$range" | awk '/^docs?(:|\(| )/ { n++ } END { print n + 0 }' ;;
    test) git log --format='%s' --no-merges "$range" | awk '/^test(:|\(| )/ { n++ } END { print n + 0 }' ;;
    ci) git log --format='%s' --no-merges "$range" | awk '/^(ci|build)(:|\(| )/ { n++ } END { print n + 0 }' ;;
  esac
}

feat="$(count_kind feat)"
fix="$(count_kind fix)"
docs="$(count_kind docs)"
test="$(count_kind test)"
ci="$(count_kind ci)"
short_sha="$(git rev-parse --short "$commit")"
date="$(TZ=Asia/Shanghai date '+%Y-%m-%d')"
compare=""
if [ -n "$previous" ]; then
  compare="https://github.com/ShellMonster/PudMusic/compare/${previous}...${tag}"
fi

{
  printf '# PudMusic %s\n\n' "$tag"
  printf '> 布丁音乐（PudMusic）桌面音乐播放器版本发布。\n\n'
  printf -- '- 发布日期：%s\n' "$date"
  printf -- '- 发布提交：`%s`\n' "$short_sha"
  if [ -n "$previous" ]; then
    printf -- '- 变更范围：`%s..%s`\n' "$previous" "$tag"
  fi
  printf '\n## 版本说明\n\n%s\n' "$section"
  printf '\n## 本次变更\n\n'
  printf -- '- 代码变更提交：%s 个\n' "$count_all"
  printf -- '- 功能（feat）：%s 个\n' "$feat"
  printf -- '- 修复（fix）：%s 个\n' "$fix"
  printf -- '- 文档（docs）：%s 个\n' "$docs"
  printf -- '- 测试（test）：%s 个\n' "$test"
  printf -- '- CI/构建（ci/build）：%s 个\n' "$ci"
  if [ -n "$compare" ]; then
    printf '\n[查看完整变更记录](%s)\n' "$compare"
  fi
  printf '\n## 下载\n\n'
  printf '请按系统和架构选择下方资产；Release 页面中的资产标签会标明对应平台。自动更新清单由构建流程生成并随 Release 上传。\n\n'
  printf '| 系统 | 架构 | 安装包 | 更新归档 | 自动更新清单 |\n'
  printf '| --- | --- | --- | --- | --- |\n'
  printf '| macOS | Apple Silicon（arm64） | `%s-%s-arm64.dmg` | `%s-%s-darwin-arm64.tar.gz` | `update-darwin-arm64.json` |\n' "$slug" "$version" "$slug" "$version"
  printf '| macOS | Intel（amd64） | `%s-%s-amd64.dmg` | `%s-%s-darwin-amd64.tar.gz` | `update-darwin-amd64.json` |\n' "$slug" "$version" "$slug" "$version"
  printf '| Windows | x64（amd64） | `%s-setup-%s-amd64.exe` | `%s-%s-windows-amd64.tar.gz` | `update-windows-amd64.json` |\n' "$slug" "$version" "$slug" "$version"
  printf '| Windows | ARM64 | `%s-setup-%s-arm64.exe` | `%s-%s-windows-arm64.tar.gz` | `update-windows-arm64.json` |\n' "$slug" "$version" "$slug" "$version"
  printf '\n'
  printf '安装包未进行 Apple 公证或 Windows 代码签名。\n'
  printf '\n## 校验\n\n'
  printf '下载后请通过文件大小、GitHub Assets 和项目发布信息核对来源。\n'
} > "$out"

printf '已生成 %s\n' "$out"
