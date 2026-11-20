#!/usr/bin/env bash
# ailock install self-test: verifies the kill switch end-to-end on a
# Linux/systemd host, including the "general connectivity survives while
# blocked" invariant.
#
# Usage:  sudo ./scripts/selftest.sh [proxy-service] [test-domain]
#   proxy-service : systemd unit to stop/start around the test
#                   (default: proxyctl)
#   test-domain   : a domain from your blocklist (default: youtube.com)
#   CONNECTIVITY_URL env: site that must stay reachable while blocked
#                   (default: https://www.baidu.com)
#
# Needs root: the toggle writes /etc/hosts and the script controls the
# proxy service. The proxy is always restored on exit, even on failure.
set -euo pipefail

PROXY_SVC="${1:-proxyctl}"
DOMAIN="${2:-youtube.com}"
CONNECTIVITY_URL="${CONNECTIVITY_URL:-https://www.baidu.com}"

fail() { echo "FAIL: $*" >&2; exit 1; }

wait_for() { # desc timeout_s cmd...
	local desc=$1 timeout=$2
	shift 2
	local deadline=$((SECONDS + timeout))
	until "$@"; do
		if [ $SECONDS -ge $deadline ]; then
			fail "timeout (${timeout}s) waiting for: $desc"
		fi
		sleep 0.5
	done
}

first_ip() { getent hosts "$DOMAIN" | awk '{print $1}' | head -1; }
sinkholed() { [ "$(first_ip)" = "0.0.0.0" ]; }
resolved() { [ -n "$(getent hosts "$DOMAIN" | grep -v '^0\.0\.0\.0 ')" ]; }

[ "$(id -u)" -eq 0 ] || fail "run as root (sudo)"
command -v ailock >/dev/null || fail "ailock not on PATH"
command -v getent >/dev/null || fail "getent not found (Linux only)"

echo "== ailock $(ailock version), ailock.service: $(systemctl is-active ailock)"
systemctl is-active --quiet ailock || fail "ailock.service not active"
systemctl is-active --quiet "$PROXY_SVC" || fail "$PROXY_SVC not running"

echo "== baseline: $DOMAIN must resolve normally while the proxy is up"
wait_for "initial resolution" 15 resolved
echo "PASS: resolves to $(first_ip)"

STOPPED=0
restore() {
	if [ "$STOPPED" -eq 1 ]; then
		echo "== restoring $PROXY_SVC"
		systemctl start "$PROXY_SVC"
		wait_for "proxy restart + unblock" 30 resolved || true
	fi
}
trap restore EXIT

echo "== stopping $PROXY_SVC — expecting the block within ~interval*2"
systemctl stop "$PROXY_SVC"
STOPPED=1
wait_for "block after proxy stop" 15 sinkholed
echo "PASS: $DOMAIN sinkholed → $(first_ip)"

echo "== invariant: general connectivity must survive while blocked"
code=$(curl -sm5 -o /dev/null -w '%{http_code}' "$CONNECTIVITY_URL") \
	|| fail "$CONNECTIVITY_URL unreachable while blocked — invariant violated"
echo "PASS: $CONNECTIVITY_URL → HTTP $code"

echo "== starting $PROXY_SVC — expecting the block to lift"
systemctl start "$PROXY_SVC"
STOPPED=0
wait_for "unblock after proxy start" 30 resolved
echo "PASS: $DOMAIN resolves again → $(first_ip)"
code=$(curl -sm8 -o /dev/null -w '%{http_code}' "https://$DOMAIN") \
	|| fail "https://$DOMAIN not reachable after unblock"
echo "PASS: https://$DOMAIN → HTTP $code"

echo
echo "ALL PASS — kill switch verified: fail-closed on proxy death, scoped to the blocklist, recovers with the proxy."
