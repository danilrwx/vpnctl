#!/bin/sh
# capture.sh <каталог состояния> — снимает все экраны TUI в docs/img через tmux и headless chromium
# Нужны: tmux, python3, node + PLAYWRIGHT_CORE и CHROME для shot.js
set -eu
STATE=$1; HERE=$(dirname "$0"); OUT=${OUT:-docs/img}; T=$(mktemp -d)
tmux kill-session -t vpnshots 2>/dev/null || true
tmux new-session -d -s vpnshots -x 118 -y 30 "TERM=xterm-256color ${VPNCTL:-target/release/vpnctl} -C $STATE tui"
sleep 3
shot() { tmux capture-pane -t vpnshots -e -p > "$T/$1.txt"; python3 "$HERE/ansi2html.py" "$T/$1.txt" "$T/$1.html" "$1"; node "$HERE/shot.js" "$T/$1.html" "$OUT/$1.png"; echo "$OUT/$1.png"; }
k() { tmux send-keys -t vpnshots "$@"; sleep "${SLEEP:-1}"; }
shot tui-1-overview
k 2; shot tui-2-users
k 3; k Right; k l; sleep 2; shot tui-3-host
k 4; k p; sleep 4; shot tui-4-plan
k 5; shot tui-5-topology
k 6; shot tui-6-routing
k '?'; shot tui-help; k Escape
k 2; k n; shot tui-dialog-user; k Escape
k q; tmux kill-session -t vpnshots 2>/dev/null || true; rm -rf "$T"
