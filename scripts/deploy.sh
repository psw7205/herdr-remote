#!/usr/bin/env bash
# Build, install, and restart a local Bridge. Run through mise so the pinned
# toolchain is on PATH: `mise run build|install|redeploy`.
#
#   PREFIX                      install prefix (default: $HOME/.local)
#   HERDR_REMOTE_LAUNCHD_LABEL  LaunchAgent label for redeploy (default: herdr-remote)
#   HERDR_REMOTE_HEALTH_URL     URL that must answer 200 after restart
set -euo pipefail

cd "$(dirname "$0")/.."

name=herdr-remote-bridge
target=${PREFIX:-$HOME/.local}/bin/$name
label=${HERDR_REMOTE_LAUNCHD_LABEL:-herdr-remote}
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
	echo "deploy: installed $target"
}

healthy() {
	local _
	for _ in $(seq 20); do
		if curl -fsS -o /dev/null --max-time 2 "$health_url"; then
			return 0
		fi
		sleep 0.5
	done
	return 1
}

redeploy() {
	[[ $(uname -s) == Darwin ]] || die "redeploy supports launchd only; run install and restart the Bridge with your process manager"
	local service
	service=gui/$(id -u)/$label
	local info
	info=$(launchctl print "$service" 2>/dev/null) ||
		die "LaunchAgent '$label' is not loaded; see contrib/launchd/herdr-remote.plist.example or set HERDR_REMOTE_LAUNCHD_LABEL"
	# launchctl print is not a stable format, so a missing line is not an error.
	local program
	program=$(sed -n 's/^[[:space:]]*program = //p' <<<"$info" | head -n 1)
	if [[ -n $program && $program != "$target" ]]; then
		die "'$label' runs $program, not $target; set PREFIX to match the LaunchAgent"
	fi

	deps
	go vet ./...
	go test -race ./...
	pnpm --dir web test
	compile
	install_bin

	launchctl kickstart -k "$service"
	if healthy; then
		echo "deploy: $label restarted and $health_url answered"
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
*) die "usage: $0 build|install|redeploy" ;;
esac
