#!/usr/bin/env bash
# Isolated Herdr + Claude Code + development Bridge for mobile checks.
#
#   scripts/mobile-e2e/herdr-e2e.sh up      build, start, seed one prompt, print state
#   scripts/mobile-e2e/herdr-e2e.sh status  print state and whether each part answers
#   scripts/mobile-e2e/herdr-e2e.sh down    stop everything and remove what up created
#
# Environment (all optional):
#   HERDR_E2E_DIR      state directory (default: $TMPDIR/herdr-e2e). `down` removes it
#   HERDR_E2E_SESSION  Herdr session name (default: e2e-mobile)
#   HERDR_E2E_PORT     Bridge port on 127.0.0.1 (default: 8797)
#   HERDR_E2E_MODEL    model passed to `claude --model` (default: claude-haiku-4-5-20251001)
#   HERDR_E2E_SEED     "0" skips the seed prompt that creates the native transcript
#
# The default Herdr server and the user's agents are never touched: every Herdr
# call goes through the named session, and `down` compares the default server's
# pane list and the `claude` PIDs with what `up` recorded.
set -euo pipefail

repo=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
dir=${HERDR_E2E_DIR:-${TMPDIR:-/tmp}/herdr-e2e}
dir=${dir%/}
session=${HERDR_E2E_SESSION:-e2e-mobile}
port=${HERDR_E2E_PORT:-8797}
model=${HERDR_E2E_MODEL:-claude-haiku-4-5-20251001}
seed=${HERDR_E2E_SEED:-1}
sock=$HOME/.config/herdr/sessions/$session/herdr.sock

die() { echo "herdr-e2e: $*" >&2; exit 1; }
t() { perl -e 'alarm shift; exec @ARGV' "$@"; }
h() { "$dir/h" "$@"; }

need() { for c in "$@"; do command -v "$c" >/dev/null || die "$c not on PATH"; done; }

pane_list() { herdr pane list | jq -c '[.result.panes[] | {pane_id, terminal_id}]'; }
# Claude's process name is its version string, so match argv0 instead of the name.
claude_pids() { ps -axo pid=,args= | awk '$2 == "claude" || $2 ~ /\/claude$/ { print $1 }' | sort || true; }

up() {
	need herdr jq perl curl git mise
	[[ -S $sock ]] && die "session '$session' already has a socket at $sock; run down first"
	[[ -d $dir ]] && die "$dir exists; run down first"
	mkdir -p "$dir/proj" "$dir/receipts"
	echo "herdr-e2e: building Bridge and client into $dir"
	(cd "$repo" && mise exec -- go build -o "$dir/bridge" ./cmd/bridge) || die "go build"
	(cd "$repo" && mise exec -- pnpm --dir web --silent exec vite build --outDir "$dir/dist" --emptyOutDir >/dev/null) || die "vite build"

	pane_list >"$dir/default-panes-before.json"
	claude_pids >"$dir/claude-pids-before.txt"
	cp -p "$HOME/.claude.json" "$dir/claude.json.before-up" 2>/dev/null || true

	git -C "$dir/proj" init -q
	printf 'export function add(a: number, b: number) {\n  return a + b\n}\n' >"$dir/proj/math.ts"
	git -C "$dir/proj" add -A
	git -C "$dir/proj" -c user.email=e2e@example.invalid -c user.name=e2e commit -qm init

	# Coding-agent variables (HERDR_*, CLAUDE_CODE_*) must not leak into the
	# isolated server, or the Claude it starts does not save its transcript.
	cat >"$dir/h" <<WRAP
#!/bin/sh
exec env -i HOME="$HOME" USER="$USER" SHELL="$SHELL" TERM=xterm-256color LANG="${LANG:-en_US.UTF-8}" PATH="$PATH" herdr --session $session "\$@"
WRAP
	chmod +x "$dir/h"
	nohup "$dir/h" server >"$dir/server.log" 2>&1 &
	for _ in $(seq 1 20); do [[ -S $sock ]] && break; sleep 1; done
	[[ -S $sock ]] || die "isolated server did not create $sock (see $dir/server.log)"

	local pane
	pane=$(h workspace create --cwd "$dir/proj" --no-focus | jq -r '.result.root_pane.pane_id')
	[[ -n $pane && $pane != null ]] || die "workspace create"
	echo "$pane" >"$dir/pane-id"
	h agent start a --kind claude --pane "$pane" -- --model "$model" >/dev/null 2>&1 || true
	sleep 5
	# First open of a folder shows the trust screen; accept it in the test pane only.
	h pane send-keys "$pane" down enter >/dev/null
	local st=""
	for _ in $(seq 1 30); do
		st=$(h agent list | jq -r '.result.agents[] | select(.name=="a") | .agent_status')
		[[ $st == idle ]] && break
		sleep 2
	done
	[[ $st == idle ]] || die "claude not idle ($st); see the test pane"
	if [[ $seed != 0 ]]; then
		h agent prompt a "Run the Bash command 'ls -la' once, then reply with a 6-line TypeScript code block and the word SEED-OK." --wait --until idle >/dev/null || die "seed prompt"
	fi

	nohup "$dir/bridge" -herdr-socket "$sock" -receipts-dir "$dir/receipts" -static "$dir/dist" \
		-listen "127.0.0.1:$port" -origins "http://127.0.0.1:$port,http://localhost:$port" -tailnet-host off \
		-project-root "$dir" >"$dir/bridge.log" 2>&1 &
	echo $! >"$dir/bridge.pid"
	local sid=""
	for _ in $(seq 1 15); do
		sid=$(curl -fs --max-time 2 "http://127.0.0.1:$port/api/sessions" | jq -r '.sessions[] | select(.id | startswith("claude:")) | .id' | head -1) || true
		[[ -n $sid ]] && break
		sleep 2
	done
	[[ -n $sid ]] || die "no claude session in the development Bridge (see $dir/bridge.log)"
	local enc
	enc=$(jq -rn --arg s "$sid" '$s | @uri')
	{
		echo "E2E_DIR=$dir"
		echo "E2E_PANE=$pane"
		echo "E2E_SESSION=$sid"
		echo "E2E_URL=http://127.0.0.1:$port/#session=$enc"
		echo "E2E_PORT=$port"
	} | tee "$dir/env"
}

status() {
	[[ -f $dir/env ]] || die "no state at $dir; run up first"
	cat "$dir/env"
	[[ -S $sock ]] && echo "server: socket present" || echo "server: no socket"
	curl -fs --max-time 2 "http://127.0.0.1:$port/api/sessions" >/dev/null && echo "bridge: answering" || echo "bridge: not answering"
}

down() {
	need herdr jq
	[[ -d $dir ]] || die "nothing at $dir"
	local base
	base=$(basename "$dir")
	if [[ -f $dir/bridge.pid ]]; then kill "$(cat "$dir/bridge.pid")" 2>/dev/null || true; fi
	[[ -x $dir/h ]] && h server stop >/dev/null 2>&1 || true
	sleep 2
	herdr session delete "$session" >/dev/null 2>&1 || true
	# Claude's transcript root keys project directories by path with / and . as -.
	for d in "$HOME"/.claude/projects/*"${base//./-}"*; do [[ -d $d ]] && rm -rf "$d"; done
	# The trust acceptance leaves a projects entry in the user's Claude settings.
	# A running Claude writes this file too, so keep a backup and re-check the result.
	local key backups
	key=$(cd "$dir/proj" 2>/dev/null && pwd -P) || key="$dir/proj"
	backups=${dir}.backups
	mkdir -p "$backups"
	if [[ -f $HOME/.claude.json ]]; then
		cp -p "$HOME/.claude.json" "$backups/claude.json.$(date +%Y%m%dT%H%M%S)"
		if jq -e --arg k "$key" '.projects[$k]' "$HOME/.claude.json" >/dev/null 2>&1; then
			jq --arg k "$key" 'del(.projects[$k])' "$HOME/.claude.json" >"$backups/claude.json.new" &&
				jq -e . "$backups/claude.json.new" >/dev/null &&
				cat "$backups/claude.json.new" >"$HOME/.claude.json"
		fi
		echo "claude.json entries left for $base: $(jq -r --arg b "$base" '.projects | keys[] | select(contains($b))' "$HOME/.claude.json" | wc -l | tr -d ' ')"
	fi
	echo "default panes diff (before -> after):"
	diff <(jq -c '.[]' "$dir/default-panes-before.json") <(pane_list | jq -c '.[]') && echo "  none"
	echo "claude pids lost: $(comm -23 "$dir/claude-pids-before.txt" <(claude_pids) | wc -l | tr -d ' ')"
	rm -rf "$dir"
	echo "herdr-e2e: removed $dir (backups in $backups)"
}

case ${1:-} in
up) up ;;
status) status ;;
down) down ;;
*) sed -n '2,20p' "$0"; exit 2 ;;
esac
