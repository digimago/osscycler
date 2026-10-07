#!/usr/bin/env bash
# Run the core, or the full stack (core in the background + TUI in front).
#
#   scripts/run.sh core  [core flags...]   core in the foreground, logs to the terminal
#   scripts/run.sh stack [core flags...]   core logs to $LOG_DIR/core.log; quitting
#                                          the TUI stops the core
#
# Environment (all optional; the Makefile sets them):
#   BIN          binaries directory              (default _bin)
#   LOG_DIR      where core.log goes             (default _logs)
#   ADDR         API address                     (default 127.0.0.1:7420)
#   TOKEN_FILE   API token file                  (default _secrets/api-token)
#   ANT_KEY_FILE ANT+ network key, used if ANT_PLUS_NETWORK_KEY is unset
#                                                (default _secrets/ant-network-key)
#   PORT         ANT stick tty                   (default /dev/ttyANT)
#   COURSES      directory of .gpx courses       (default _gpx, if it exists)
#   WORKOUTS     directory of .zwo workouts      (default _workouts)
#   RIDES        where rides are recorded (FIT)  (default _rides; empty: don't record)
#   FAKE=1       synthetic ride, no stick needed
#   TUI_FLAGS    extra flags for the TUI, e.g. -tour
set -euo pipefail

mode=${1:-}
case $mode in
core | stack) shift ;;
*) echo "usage: $0 core|stack [core flags...]" >&2; exit 2 ;;
esac

BIN=${BIN:-_bin}
LOG_DIR=${LOG_DIR:-_logs}
ADDR=${ADDR:-127.0.0.1:7420}
TOKEN_FILE=${TOKEN_FILE:-_secrets/api-token}
ANT_KEY_FILE=${ANT_KEY_FILE:-_secrets/ant-network-key}
PORT=${PORT:-/dev/ttyANT}
COURSES=${COURSES:-_gpx}
WORKOUTS=${WORKOUTS:-_workouts}
RIDES=${RIDES-_rides}

die() { echo "run: $*" >&2; exit 1; }

[[ -f $TOKEN_FILE ]] || die "no API token at $TOKEN_FILE (run: make token)"

core_cmd=("$BIN/osscycler-core" -addr "$ADDR" -token-file "$TOKEN_FILE")
if [[ -n ${COURSES:-} && -d $COURSES ]]; then
	core_cmd+=(-courses "$COURSES")
fi
if [[ -n ${WORKOUTS:-} ]]; then
	core_cmd+=(-workouts "$WORKOUTS") # created on the first save
fi
if [[ -n $RIDES ]]; then
	core_cmd+=(-record "$RIDES")
else
	core_cmd+=(-no-record) # the core records by default
fi
group=""
if [[ ${FAKE:-0} == 1 ]]; then
	core_cmd+=(-fake)
else
	# Without either, the core uses the key built into a release binary,
	# or says where to get one.
	if [[ -z ${ANT_PLUS_NETWORK_KEY:-} && -f $ANT_KEY_FILE ]]; then
		ANT_PLUS_NETWORK_KEY=$(<"$ANT_KEY_FILE")
		export ANT_PLUS_NETWORK_KEY
	fi
	[[ -e $PORT ]] || echo "run: $PORT not found yet (stick plugged in? udev rule installed? see: make udev); the core keeps looking" >&2
	core_cmd+=(-port "$PORT")
	# A group added since login isn't active in this session yet; borrow it via sg.
	if [[ ! -w $PORT ]]; then
		group=$(stat -c %G "$(readlink -f "$PORT")")
		id -nG "$USER" | tr ' ' '\n' | grep -qx "$group" ||
			die "$PORT is not writable and $USER is not in group $group"
	fi
fi
core_cmd+=("$@")

# start_core runs the core with exec, so the PID of the background job (or
# this process, in core mode) is the core itself, even through sg.
start_core() {
	if [[ -n $group ]]; then
		exec sg "$group" -c "exec $(printf '%q ' "${core_cmd[@]}")"
	fi
	exec "${core_cmd[@]}"
}

if [[ $mode == core ]]; then
	start_core
fi

host=${ADDR%:*} port=${ADDR##*:}
up() { (exec 3<>"/dev/tcp/$host/$port") 2>/dev/null; }
# Another core on the same address would answer our readiness check and
# the TUI would drive it instead of ours.
up && die "something already listens on $ADDR (another core?); stop it or set ADDR"

mkdir -p "$LOG_DIR"
log="$LOG_DIR/core.log"
start_core >>"$log" 2>&1 &
core=$!
trap 'kill -INT "$core" 2>/dev/null; wait "$core" 2>/dev/null || true' EXIT
trap 'exit 130' INT TERM HUP # make sure the EXIT trap runs on signals too

# Wait for the API port, failing fast if the core dies.
for _ in $(seq 100); do
	kill -0 "$core" 2>/dev/null || { tail -n 20 "$log" >&2; die "core exited during startup (log: $log)"; }
	up && break
	sleep 0.1
done
up || die "core did not open $ADDR within 10 s (log: $log)"

# TUI_FLAGS (e.g. -tour) is split on spaces on purpose.
# shellcheck disable=SC2086
"$BIN/osscycler-tui" -addr "$ADDR" -token-file "$TOKEN_FILE" ${TUI_FLAGS:-}
