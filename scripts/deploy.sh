#!/usr/bin/env bash
# Build, install, and restart a local Bridge, drive its LaunchAgent, or report
# which code HEAD, the installed binary, web/dist and the running Bridge carry.
# Run through mise so the pinned toolchain is on PATH:
# `mise run build|install|redeploy|status|stop|start|restart`.
#
#   PREFIX                      install prefix (default: $HOME/.local)
#   HERDR_REMOTE_LAUNCHD_LABEL  LaunchAgent label (default: herdr-remote)
#   HERDR_REMOTE_LAUNCHD_PLIST  plist for start/restart when the job is not
#                               loaded (default: ~/Library/LaunchAgents/<label>.plist)
#   HERDR_REMOTE_HEALTH_URL     URL that must answer 200 after restart
set -euo pipefail

cd "$(dirname "$0")/.."

name=herdr-remote-bridge
target=${PREFIX:-$HOME/.local}/bin/$name
label=${HERDR_REMOTE_LAUNCHD_LABEL:-herdr-remote}
service=gui/$(id -u)/$label
health_url=${HERDR_REMOTE_HEALTH_URL:-http://127.0.0.1:8787/api/sessions}

die() {
	echo "deploy: $*" >&2
	exit 1
}

deps() {
	pnpm --dir web install --frozen-lockfile
}

compile() {
	pnpm --dir web build
	go build -o "bin/$name" ./cmd/bridge
}

install_bin() {
	mkdir -p "$(dirname "$target")"
	if [[ -f $target ]]; then
		cp -p "$target" "$target.prev"
	fi
	# Writing over a running binary in place invalidates its macOS code
	# signature and gets the process killed; rename a new file over it instead.
	cp "bin/$name" "$target.new"
	chmod 0755 "$target.new"
	mv -f "$target.new" "$target"
	local revision modified
	read -r revision modified < <(binary_build "$target")
	echo "deploy: installed $target ($(label "$revision" "$modified"))"
}

short() {
	printf '%.7s' "$1"
}

# label <revision> <modified> -> 65f5c6b, 65f5c6b-dirty, or none
label() {
	if [[ -z $1 || $1 == none ]]; then
		echo none
	elif [[ $2 == true ]]; then
		echo "$(short "$1")-dirty"
	else
		short "$1"
	fi
}

# Go stamps vcs.modified from `git status --porcelain`, so untracked files count.
head_build() {
	local modified=false
	[[ -n $(git status --porcelain) ]] && modified=true
	echo "$(git rev-parse HEAD) $modified"
}

# binary_build <path> -> "<revision> <modified>"; `go run` builds carry none.
binary_build() {
	local info
	info=$(go version -m "$1" 2>/dev/null) || return 1
	local revision modified
	revision=$(sed -n 's/^[[:space:]]*build[[:space:]]*vcs.revision=//p' <<<"$info")
	modified=$(sed -n 's/^[[:space:]]*build[[:space:]]*vcs.modified=//p' <<<"$info")
	echo "${revision:-none} ${modified:-false}"
}

# The Vite build stamps its bundle hash into index.html (web/plugins/buildId.ts).
dist_build() {
	sed -n 's/.*<meta name="herdr-build" content="\([0-9a-f]\{16\}\)".*/\1/p' web/dist/index.html 2>/dev/null | head -n 1
}

# running_build -> "<revision> <modified> <client_build>" from the Bridge that
# answers the health URL. The file at $target may already be a newer binary
# than the process, so only the API says what runs.
running_build() {
	local body bridge
	body=$(curl -fs --max-time 2 "$health_url") || return 1
	bridge=$(grep -oE '"bridge":\{[^}]*\}' <<<"$body" | head -n 1)
	local revision modified client
	revision=$(grep -oE '"revision":"[0-9a-f]*"' <<<"$bridge" | cut -d'"' -f4)
	modified=$(grep -oE '"modified":(true|false)' <<<"$bridge" | cut -d: -f2)
	client=$(grep -oE '"client_build":"[0-9a-f]*"' <<<"$bridge" | cut -d'"' -f4)
	echo "${revision:-none} ${modified:-false} ${client:-none}"
}

# verify_running <head revision> <dist client build>: the Bridge that answers
# must run HEAD and serve the client in web/dist. A modified tree is a note,
# not a failure, when its revision matches.
verify_running() {
	local revision modified client
	read -r revision modified client < <(running_build) || die "$health_url did not answer"
	local ok=0
	if [[ $revision != "$1" ]]; then
		echo "deploy: running Bridge is $(short "$revision"), HEAD is $(short "$1")" >&2
		ok=1
	fi
	if [[ -n $2 && $client != "$2" ]]; then
		echo "deploy: running Bridge serves client $client, web/dist has $2" >&2
		ok=1
	fi
	if [[ $modified == true ]]; then
		echo "deploy: running Bridge was built from a modified tree"
	fi
	return $ok
}

status() {
	local head_revision head_modified
	read -r head_revision head_modified < <(head_build)
	echo "deploy: HEAD       $(label "$head_revision" "$head_modified")"
	local revision modified
	if read -r revision modified < <(binary_build "$target"); then
		echo "deploy: installed  $(label "$revision" "$modified")  $target"
	else
		echo "deploy: installed  none  ($target missing)"
	fi
	local dist
	dist=$(dist_build)
	echo "deploy: web/dist   client ${dist:-none}"
	local client
	if read -r revision modified client < <(running_build); then
		echo "deploy: running    $(label "$revision" "$modified") client $client  $health_url"
	else
		echo "deploy: running    none  ($health_url did not answer)"
		return 1
	fi
	if verify_running "$head_revision" "$dist"; then
		echo "deploy: running Bridge matches HEAD and web/dist"
	else
		return 1
	fi
}

healthy() {
	local _
	for _ in $(seq 20); do
		if curl -fs -o /dev/null --max-time 2 "$health_url"; then
			return 0
		fi
		sleep 0.5
	done
	return 1
}

require_launchd() {
	[[ $(uname -s) == Darwin ]] || die "$1 supports launchd only; use your process manager"
}

loaded() {
	launchctl print "$service" >/dev/null 2>&1
}

# plist_path: HERDR_REMOTE_LAUNCHD_PLIST, else the loaded job's own plist, else
# ~/Library/LaunchAgents/<label>.plist. Resolve it before a bootout, which
# forgets the loaded path.
plist_path() {
	local path=${HERDR_REMOTE_LAUNCHD_PLIST:-}
	if [[ -z $path ]]; then
		local info
		if info=$(launchctl print "$service" 2>/dev/null); then
			path=$(sed -n 's/^[[:space:]]*path = //p' <<<"$info" | head -n 1)
		fi
	fi
	[[ -n $path ]] || path=$HOME/Library/LaunchAgents/$label.plist
	[[ -f $path ]] || die "no plist for '$label' at $path; set HERDR_REMOTE_LAUNCHD_PLIST or see contrib/launchd/herdr-remote.plist.example"
	echo "$path"
}

# run_loaded: RunAtLoad may be false, so a loaded job still needs a kickstart.
# A running job is left alone.
run_loaded() {
	launchctl kickstart "$service"
	healthy || die "$health_url did not answer; check the Bridge log"
}

stop() {
	require_launchd stop
	if ! loaded; then
		echo "deploy: '$label' is not loaded"
		return
	fi
	launchctl bootout "$service"
	local _
	for _ in $(seq 20); do
		if ! loaded; then
			echo "deploy: $label stopped"
			return
		fi
		sleep 0.5
	done
	die "'$label' did not stop within 10s"
}

start() {
	require_launchd start
	if loaded; then
		echo "deploy: '$label' is already loaded"
	else
		local plist
		plist=$(plist_path)
		launchctl bootstrap "gui/$(id -u)" "$plist"
		echo "deploy: loaded $plist"
	fi
	run_loaded
	echo "deploy: $label is running and $health_url answered"
}

# restart reloads the plist, so changed ProgramArguments take effect; redeploy's
# kickstart -k only swaps the process.
restart() {
	require_launchd restart
	local plist
	plist=$(plist_path)
	stop
	launchctl bootstrap "gui/$(id -u)" "$plist"
	run_loaded
	echo "deploy: $label restarted from $plist and $health_url answered"
}

redeploy() {
	require_launchd redeploy
	local info
	info=$(launchctl print "$service" 2>/dev/null) ||
		die "LaunchAgent '$label' is not loaded; run '$0 start' (see contrib/launchd/herdr-remote.plist.example) or set HERDR_REMOTE_LAUNCHD_LABEL"
	# launchctl print is not a stable format, so a missing line is not an error.
	local program
	program=$(sed -n 's/^[[:space:]]*program = //p' <<<"$info" | head -n 1)
	if [[ -n $program && $program != "$target" ]]; then
		die "'$label' runs $program, not $target; set PREFIX to match the LaunchAgent"
	fi

	local head_revision head_modified
	read -r head_revision head_modified < <(head_build)
	deps
	go vet ./...
	go test -race ./...
	pnpm --dir web test
	compile
	install_bin

	launchctl kickstart -k "$service"
	if healthy; then
		echo "deploy: $label restarted and $health_url answered"
		verify_running "$head_revision" "$(dist_build)" || die "the running Bridge does not match this checkout; see above"
		echo "deploy: running Bridge is $(label "$head_revision" "$head_modified")"
		return
	fi
	[[ -f $target.prev ]] || die "$health_url did not answer and there is no previous binary to restore"
	mv -f "$target.prev" "$target"
	launchctl kickstart -k "$service"
	if healthy; then
		die "$health_url did not answer; restored the previous binary (web/dist keeps the new build)"
	fi
	die "$health_url did not answer even after restoring the previous binary; check the Bridge log"
}

case ${1:-} in
build)
	deps
	compile
	;;
install)
	deps
	compile
	install_bin
	;;
redeploy) redeploy ;;
status) status ;;
stop) stop ;;
start) start ;;
restart) restart ;;
*) die "usage: $0 build|install|redeploy|status|stop|start|restart" ;;
esac
