#!/usr/bin/env bash
# 打发行包。
#
#   scripts/build.sh                 # macOS dmg + Windows NSIS 安装包（默认）
#   scripts/build.sh darwin/arm64    # 只打一个目标
#   scripts/build.sh windows/amd64
#
# 真正干活的是 MyGo 自带的 CLI（`go tool mygo build`，`go.mod` 里的 tool 指令）。
# 这个脚本只做三件它不做的事：把目标列全、缺工具时说得清楚、打完把产物列出来。
set -euo pipefail

cd "$(dirname "$0")/.."

# 首发只支持两个平台（Plan §5.1 第 2 条：Linux 首发不做）。
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

platforms="$(IFS=,; echo "${targets[*]}")"

say() { printf '\n\033[1m==> %s\033[0m\n' "$*"; }
die() { printf '\033[31m错误：%s\033[0m\n' "$*" >&2; exit 1; }

need() {
  command -v "$1" >/dev/null 2>&1 || die "$2"
}

need go "装 Go 1.27 或更新版本。"
need git "装 git。"

# Windows 的 NSIS 安装包要 makensis。它在 macOS / Linux 上是**可选**的，MyGo 遇到
# 没有它会**跳过安装包并且只打一行提示**——于是脚本「成功」了，产物里没有 .exe
# 安装包，而签收的人要等下一次才发现。
#
# 所以这里改成硬失败。（这是有意的：任务书要的就是「Windows：NSIS 安装包」。）
want_windows=0
for t in "${targets[@]}"; do
  case "$t" in windows/*) want_windows=1 ;; esac
done
if [ "$want_windows" -eq 1 ]; then
  if ! command -v makensis >/dev/null 2>&1; then
    die "要打 Windows 安装包就需要 NSIS（makensis），这台机器上没有。
  macOS:  brew install nsis
  Debian: sudo apt install nsis
  不想装的话，只打 macOS：scripts/build.sh darwin/arm64"
  fi
fi

say "先核第三方许可清单（清单和实际构建依赖对不上就停）"
go test ./internal/licensing/

say "重新生成图标与许可清单"
# 图案和清单都是代码画/算出来的，提交的那份必须是最新的。跑一次让「忘了重新生成」
# 不可能溜过去。
go generate ./internal/platform/ ./internal/licensing/

if command -v git >/dev/null 2>&1 && git rev-parse --git-dir >/dev/null 2>&1; then
  if ! git diff --quiet -- resources; then
    die "生成出来的东西和提交的不一致，已经改好了——看一眼再重跑一次：
$(git diff --stat -- resources)"
  fi
fi

say "打 $platforms"
# ⚠️ 没有 -skip-notarize：本项目**不公证**（Plan §5.1 第 4 条，用户已定）。
# mygo.json 里没有 macos.notarize，所以这一步本来就不会走；这里不写死那个开关是
# 为了「哪天要公证」时改动只落在一个地方。
go tool mygo build -platform "$platforms"

say "产物"
# mygo.json 的 `out` 是 build/。
for d in build/*/; do
  [ -d "$d" ] || continue
  printf '\n  %s\n' "${d%/}"
  # 只列用户能拿走的东西（镜像、安装包、应用包），不列中间产物。
  #
  # ⚠️ 用 `du -sh` 不用 `du -h`：给目录传 `du -h` 在这台 macOS 上会输出**多行**
  # （目录加它下面的每一项），`$( )` 全收进来之后 printf 只给第一行补空格，
  # 后面的几行就顶到行首去了——产物列表看起来像坏掉了。
  find "$d" -maxdepth 1 \( -name '*.dmg' -o -name '*Setup*.exe' -o -name '*.app' \) -print 2>/dev/null |
    while IFS= read -r f; do
      printf '    %-40s %6s\n' "$(basename "$f")" "$(du -sh "$f" | cut -f1)"
    done
done

say "装之前请读 README 的「已知边界」一节"
cat <<'EOF'
  - macOS 的 dmg **没有公证**：首次打开 Gatekeeper 会拦，右键点图标选「打开」，
    或者 `xattr -dr com.apple.quarantine "/Applications/BiuGo.app"`。
  - Windows 的安装包**没有签名**：SmartScreen 会警告「Windows 已保护你的电脑」。
  - Windows 版**暂时解不了 AAC / M4A / M4S**（B 站的有损音质就是这一档）。
    这是一条待决的路线选择，不是没排期；见 README 与 Plan §6.1。
EOF
