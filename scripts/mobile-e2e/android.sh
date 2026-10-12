#!/usr/bin/env bash
# Android emulator driver for mobile checks against the development Bridge.
#
#   scripts/mobile-e2e/android.sh up [url]    boot the AVD, reverse the Bridge port, open Chrome, forward DevTools
#   scripts/mobile-e2e/android.sh down        remove forwards and stop the emulator
#   scripts/mobile-e2e/android.sh open <url>  open a URL in Chrome
#   scripts/mobile-e2e/android.sh tap <x> <y>
#   scripts/mobile-e2e/android.sh hold <x> <y> <ms>      press without moving
#   scripts/mobile-e2e/android.sh swipe <x1> <y1> <x2> <y2> <ms>
#   scripts/mobile-e2e/android.sh key <keycode>          e.g. KEYCODE_POWER, KEYCODE_BACK
#   scripts/mobile-e2e/android.sh text <string>          ASCII only; use Gboard taps for IME
#   scripts/mobile-e2e/android.sh shot <file.png>
#   scripts/mobile-e2e/android.sh net off|on             wifi and data; adb reverse keeps working
#   scripts/mobile-e2e/android.sh bridge off|on          cut the Bridge path (adb reconnect) or restore it
#   scripts/mobile-e2e/android.sh stop-chrome|start-chrome [url]   force-stop (installed PWA/tab state lost) and relaunch
#
# Environment (all optional):
#   ANDROID_SDK_ROOT  (default: $HOME/Library/Android/sdk)
#   HERDR_E2E_AVD     AVD name (default: Pixel_4_API_31)
#   HERDR_E2E_PORT    Bridge port to reverse (default: 8797)
#   HERDR_E2E_CDP     host port forwarded to Chrome DevTools (default: 9222)
#   HERDR_E2E_DIR     state directory of herdr-e2e.sh, used for the default URL
#
# Coordinates are device pixels: CSS px times the device pixel ratio plus the
# top of Chrome's content area (`cdp.mjs center` does this; CONTENT_TOP overrides
# the default 240 of the Pixel 4 AVD). A tap below the content area lands on the
# system navigation bar and sends Chrome to the background.
set -euo pipefail

sdk=${ANDROID_SDK_ROOT:-$HOME/Library/Android/sdk}
avd=${HERDR_E2E_AVD:-Pixel_4_API_31}
port=${HERDR_E2E_PORT:-8797}
cdp=${HERDR_E2E_CDP:-9222}
dir=${HERDR_E2E_DIR:-${TMPDIR:-/tmp}/herdr-e2e}
adb=$sdk/platform-tools/adb
emulator=$sdk/emulator/emulator
chrome=com.android.chrome/com.google.android.apps.chrome.Main

die() { echo "android: $*" >&2; exit 1; }
t() { perl -e 'alarm shift; exec @ARGV' "$@"; }
a() { t 20 "$adb" -e "$@"; }

default_url() {
	[[ -f $dir/env ]] || die "no URL given and no state at $dir/env"
	sed -n 's/^E2E_URL=//p' "$dir/env"
}

open_url() {
	local url=$1
	t 60 "$adb" -e shell "am start -W -n $chrome -a android.intent.action.VIEW -d '$url'" >/dev/null
}

forward_cdp() {
	a forward "tcp:$cdp" localabstract:chrome_devtools_remote >/dev/null
	for _ in $(seq 1 20); do curl -fs --max-time 3 "http://127.0.0.1:$cdp/json/version" >/dev/null && return; sleep 3; done
	die "Chrome DevTools did not answer on 127.0.0.1:$cdp"
}

up() {
	[[ -x $adb && -x $emulator ]] || die "Android SDK not at $sdk (set ANDROID_SDK_ROOT)"
	local url=${1:-$(default_url)}
	if ! a get-state >/dev/null 2>&1; then
		# 1.5 GB RAM or software GPU made the framework stall on this AVD.
		nohup "$emulator" -avd "$avd" -no-snapshot -no-boot-anim -no-audio -gpu auto -memory 3072 -cores 4 >"${dir}.emulator.log" 2>&1 &
		sleep 10
	fi
	local booted=""
	for _ in $(seq 1 60); do
		booted=$(t 5 "$adb" -e shell getprop sys.boot_completed 2>/dev/null | tr -d '\r') || true
		[[ $booted == 1 ]] && break
		sleep 3
	done
	[[ $booted == 1 ]] || die "emulator did not boot (see ${dir}.emulator.log)"
	sleep 15
	a reverse "tcp:$port" "tcp:$port" >/dev/null
	open_url "$url"
	sleep 10
	forward_cdp
	echo "android: $avd up, Bridge reversed on $port, DevTools on 127.0.0.1:$cdp"
	echo "URL=$url"
}

down() {
	a reverse --remove-all >/dev/null 2>&1 || true
	a forward --remove-all >/dev/null 2>&1 || true
	t 15 "$adb" -e emu kill >/dev/null 2>&1 || true
	sleep 5
	echo "android: qemu processes left: $(pgrep -f qemu-system | wc -l | tr -d ' ')"
}

case ${1:-} in
up) up "${2:-}" ;;
down) down ;;
open) open_url "${2:?url}" ;;
tap) a shell input tap "${2:?x}" "${3:?y}" ;;
hold) a shell input swipe "${2:?x}" "${3:?y}" "$2" "$3" "${4:?ms}" ;;
swipe) a shell input swipe "${2:?x1}" "${3:?y1}" "${4:?x2}" "${5:?y2}" "${6:?ms}" ;;
key) a shell input keyevent "${2:?keycode}" ;;
text) a shell input text "${2:?string}" ;;
shot) t 30 "$adb" -e emu screenrecord screenshot "${2:?file}" >/dev/null && echo "$2" ;;
net)
	case ${2:-} in
	off) a shell svc wifi disable; a shell svc data disable ;;
	on) a shell svc wifi enable; a shell svc data enable ;;
	*) die "net off|on" ;;
	esac
	;;
bridge)
	# Removing the reverse alone leaves established sockets flowing through adbd;
	# only a transport reset closes them, and it drops every forward too.
	case ${2:-} in
	off) a reverse --remove-all >/dev/null 2>&1 || true; "$adb" reconnect >/dev/null 2>&1 || true; t 30 "$adb" -e wait-for-device; forward_cdp ;;
	on) a reverse "tcp:$port" "tcp:$port" >/dev/null ;;
	*) die "bridge off|on" ;;
	esac
	;;
stop-chrome) a shell am force-stop com.android.chrome ;;
start-chrome) open_url "${2:-$(default_url)}"; sleep 8; forward_cdp ;;
*) sed -n '2,24p' "$0"; exit 2 ;;
esac
