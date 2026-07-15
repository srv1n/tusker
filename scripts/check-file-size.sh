#!/usr/bin/env bash
#
# Advisory code file-size check.
#
# Lists hand-written code files over the ~1,000-line guideline (see
# skills/tusker/references/ENGINEERING_DISCIPLINE.md, Operating Posture, and
# docs/ai-contribution-policy.md, Code file size). This check is ADVISORY: it
# reports offenders but never fails the build. Files already known to exceed the
# limit live in scripts/file-size-allowlist.txt and are burned down by
# CLN-T-0007.
set -euo pipefail

LIMIT="${FILE_SIZE_LIMIT:-1000}"
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
ALLOWLIST="$ROOT/scripts/file-size-allowlist.txt"

cd "$ROOT"

# Load allowlist entries, skipping blank lines and '#' comments.
allow=""
if [ -f "$ALLOWLIST" ]; then
	allow="$(grep -vE '^[[:space:]]*(#|$)' "$ALLOWLIST" || true)"
fi

is_allowed() {
	[ -n "$allow" ] || return 1
	printf '%s\n' "$allow" | grep -Fxq "$1"
}

offenders=""
while IFS= read -r f; do
	[ -f "$f" ] || continue
	n=$(wc -l <"$f" | tr -d ' ')
	[ "$n" -gt "$LIMIT" ] || continue
	is_allowed "$f" && continue
	offenders="${offenders}${n}	${f}
"
done < <(git ls-files -- '*.go' '*.ts' '*.tsx' '*.js' '*.jsx' \
	| grep -vE '(^|/)(vendor|node_modules|dist|_generated)/' || true)

echo "Advisory: code file size check (limit ${LIMIT} lines)"
if [ -n "$offenders" ]; then
	echo "  Code files over ${LIMIT} lines that are not allowlisted:"
	printf '%s' "$offenders" | sort -rn | awk '{printf "    %6d  %s\n", $1, $2}'
	echo "  Split them into cohesive modules, or record a deliberate exception in"
	echo "  scripts/file-size-allowlist.txt (burn-down tracked by CLN-T-0007)."
else
	echo "  OK: no non-allowlisted code files exceed ${LIMIT} lines."
fi

# Advisory only: always succeed so the check never fails the build.
exit 0
