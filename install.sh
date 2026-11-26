#!/usr/bin/env bash
# Installs the handoff skill, the supporting skills it calls and the handoff-watch TUI.
#
#   ./install.sh            install everything that is missing
#   ./install.sh --check    report what is missing, change nothing
#   ./install.sh --no-tui   skip the handoff-watch build
#
# Environment:
#   SKILLS_HOME   where skills are copied      (default ~/.agents/skills)
#   BIN_DIR       where handoff-watch is built (default ~/.local/bin)
#   SKILLS_REPO   GitHub repo of the supporting skills (default trebaud/skills)
#   SKILLS_REF    branch or tag of that repo   (default main)
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SKILLS_HOME="${SKILLS_HOME:-$HOME/.agents/skills}"
BIN_DIR="${BIN_DIR:-$HOME/.local/bin}"
SKILLS_REPO="${SKILLS_REPO:-trebaud/skills}"
SKILLS_REF="${SKILLS_REF:-main}"

# Each agent reads skills from its own folder: the skill is linked into those that exist.
AGENT_DIRS=("$HOME/.claude/skills" "$HOME/.opencode/skills")

# name|GitHub repo|ref|path of the skill folder in the repo
# handoff calls mori and lgtm; lgtm calls the other four.
SUPPORTING=(
  "lgtm|$SKILLS_REPO|$SKILLS_REF|skills/lgtm"
  "security-audit|$SKILLS_REPO|$SKILLS_REF|skills/security-audit"
  "dont-reinvent-the-wheel|$SKILLS_REPO|$SKILLS_REF|skills/dont-reinvent-the-wheel"
  "architecture-audit|$SKILLS_REPO|$SKILLS_REF|skills/architecture-audit"
  "taste|$SKILLS_REPO|$SKILLS_REF|skills/taste"
  "mori|trebaud/mori|main|skill"
)

check_only=false
build_tui=true
for arg in "$@"; do
  case "$arg" in
    --check) check_only=true ;;
    --no-tui) build_tui=false ;;
    -h|--help) sed -n '2,12p' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
    *) echo "unknown option: $arg" >&2; exit 2 ;;
  esac
done

tmp=""
cleanup() { [[ -z "$tmp" ]] || rm -rf "$tmp"; }
trap cleanup EXIT

installed_at() {
  local name="$1" dir
  for dir in "$SKILLS_HOME" "${AGENT_DIRS[@]}"; do
    if [[ -f "$dir/$name/SKILL.md" ]]; then
      echo "$dir/$name"
      return 0
    fi
  done
  return 1
}

# Prints the folder that holds the unpacked repo, downloading it once per run.
fetch_repo() {
  local repo="$1" ref="$2" dest
  dest="$tmp/${repo//\//_}@${ref//\//_}"
  if [[ ! -d "$dest" ]]; then
    mkdir -p "$dest"
    echo "downloading $repo@$ref" >&2
    curl -fsSL "https://codeload.github.com/$repo/tar.gz/$ref" | tar -xz -C "$dest" --strip-components=1
  fi
  echo "$dest"
}

link_skill() {
  local name="$1" dir link
  for dir in "${AGENT_DIRS[@]}"; do
    [[ -d "$dir" ]] || continue
    link="$dir/$name"
    if [[ -e "$link" && ! -L "$link" ]]; then
      echo "  skipped $link: exists and is not a symlink" >&2
      continue
    fi
    ln -sfn "$SKILLS_HOME/$name" "$link"
    echo "  linked $link"
  done
}

copy_skill() {
  local name="$1" source="$2"
  mkdir -p "$SKILLS_HOME"
  rm -rf "${SKILLS_HOME:?}/$name"
  cp -R "$source" "$SKILLS_HOME/$name"
  echo "installed $name -> $SKILLS_HOME/$name"
  link_skill "$name"
}

missing=0

for entry in "${SUPPORTING[@]}"; do
  IFS='|' read -r name repo ref path <<<"$entry"
  if found="$(installed_at "$name")"; then
    echo "ok        $name ($found)"
    continue
  fi
  if $check_only; then
    echo "missing   $name ($repo@$ref:$path)"
    missing=$((missing + 1))
    continue
  fi
  # Set here, not in fetch_repo: that one runs in a subshell and would lose the value.
  [[ -n "$tmp" ]] || tmp="$(mktemp -d)"
  source="$(fetch_repo "$repo" "$ref")/$path"
  if [[ ! -f "$source/SKILL.md" ]]; then
    echo "error: $repo@$ref has no skill at $path" >&2
    exit 1
  fi
  copy_skill "$name" "$source"
done

if $check_only; then
  installed_at handoff >/dev/null && echo "ok        handoff ($(installed_at handoff))" || { echo "missing   handoff"; missing=$((missing + 1)); }
else
  copy_skill handoff "$ROOT/skill"
fi

for tool in go git mori; do
  command -v "$tool" >/dev/null || echo "warning: $tool is not on PATH, the handoff skill needs it" >&2
done

if $check_only; then
  command -v handoff-watch >/dev/null && echo "ok        handoff-watch ($(command -v handoff-watch))" || echo "missing   handoff-watch (optional)"
  [[ $missing -eq 0 ]] || exit 1
  exit 0
fi

if $build_tui; then
  if command -v go >/dev/null; then
    mkdir -p "$BIN_DIR"
    (cd "$ROOT" && go build -o "$BIN_DIR/handoff-watch" ./cmd/handoff-watch)
    echo "built handoff-watch -> $BIN_DIR/handoff-watch"
    case ":$PATH:" in *":$BIN_DIR:"*) ;; *) echo "warning: $BIN_DIR is not on PATH" >&2 ;; esac
  else
    echo "skipped handoff-watch: go is not installed" >&2
  fi
fi
