#!/bin/sh

set -u

usage() {
	printf 'usage: %s [--print-lock-path] -- command [args...]\n' "$0" >&2
	exit 64
}

resolve_lock_dir() {
	if [ -n "${TUSKER_VALIDATION_LOCK_DIR:-}" ]; then
		printf '%s\n' "$TUSKER_VALIDATION_LOCK_DIR"
		return
	fi

	common_dir=$(git rev-parse --git-common-dir 2>/dev/null || true)
	if [ -n "$common_dir" ]; then
		case "$common_dir" in
			/*) common_abs=$common_dir ;;
			*) common_abs=$(cd "$(dirname "$common_dir")" && printf '%s/%s\n' "$(pwd -P)" "$(basename "$common_dir")") ;;
		esac
		printf '%s/tusker-validation.lock\n' "$common_abs"
		return
	fi

	physical_pwd=$(pwd -P)
	repo_id=$(printf '%s' "$physical_pwd" | cksum | awk '{print $1}')
	printf '%s/tusker-validation-%s.lock\n' "${TMPDIR:-/tmp}" "$repo_id"
}

lock_dir=$(resolve_lock_dir)
if [ "${1:-}" = "--print-lock-path" ]; then
	printf '%s\n' "$lock_dir"
	exit 0
fi
[ "${1:-}" = "--" ] || usage
shift
[ "$#" -gt 0 ] || usage

poll_seconds=${TUSKER_VALIDATION_LOCK_POLL_SECONDS:-1}
timeout_seconds=${TUSKER_VALIDATION_LOCK_TIMEOUT_SECONDS:-1800}
case "$timeout_seconds" in
	''|*[!0-9]*) printf 'validation gate: timeout must be a non-negative integer\n' >&2; exit 64 ;;
esac

token="$$-$(date +%s)-${PPID:-0}"
acquired=0
child_pid=''
wait_reported=0
wait_started=$(date +%s)

remove_lock_contents() {
	target=$1
	rm -f "$target/pid" "$target/token" "$target/cwd" "$target/started_at"
	rmdir "$target" 2>/dev/null || true
}

release_lock() {
	[ "$acquired" -eq 1 ] || return
	owner_token=$(cat "$lock_dir/token" 2>/dev/null || true)
	if [ -z "$owner_token" ] || [ "$owner_token" = "$token" ]; then
		remove_lock_contents "$lock_dir"
	fi
	acquired=0
}

forward_signal() {
	signal=$1
	status=$2
	if [ -n "$child_pid" ] && kill -0 "$child_pid" 2>/dev/null; then
		kill -"$signal" "$child_pid" 2>/dev/null || true
		wait "$child_pid" 2>/dev/null || true
	fi
	child_pid=''
	exit "$status"
}

trap release_lock EXIT
trap 'forward_signal HUP 129' HUP
trap 'forward_signal INT 130' INT
trap 'forward_signal TERM 143' TERM

while :; do
	if mkdir "$lock_dir" 2>/dev/null; then
		acquired=1
		printf '%s\n' "$token" >"$lock_dir/token"
		printf '%s\n' "$$" >"$lock_dir/pid"
		pwd -P >"$lock_dir/cwd"
		date -u +%Y-%m-%dT%H:%M:%SZ >"$lock_dir/started_at"
		export TUSKER_VALIDATION_LOCK_HELD="$token"
		if [ "$wait_reported" -eq 1 ]; then
			waited=$(( $(date +%s) - wait_started ))
			printf 'validation gate: acquired after %ss\n' "$waited" >&2
		fi
		break
	fi

	owner_pid=$(cat "$lock_dir/pid" 2>/dev/null || true)
	if [ -n "$owner_pid" ] && ! kill -0 "$owner_pid" 2>/dev/null; then
		stale_dir="${lock_dir}.stale.$$.$(date +%s)"
		if mv "$lock_dir" "$stale_dir" 2>/dev/null; then
			printf 'validation gate: recovered stale owner pid %s\n' "$owner_pid" >&2
			remove_lock_contents "$stale_dir"
			continue
		fi
	fi

	if [ "$wait_reported" -eq 0 ]; then
		owner_label=${owner_pid:-initializing}
		printf 'validation gate: busy (owner pid %s); waiting\n' "$owner_label" >&2
		wait_reported=1
	fi
	now=$(date +%s)
	if [ "$timeout_seconds" -gt 0 ] && [ $((now - wait_started)) -ge "$timeout_seconds" ]; then
		printf 'validation gate: timed out after %ss waiting for %s\n' "$timeout_seconds" "$lock_dir" >&2
		exit 75
	fi
	sleep "$poll_seconds"
done

"$@" &
child_pid=$!
wait "$child_pid"
status=$?
child_pid=''
exit "$status"
