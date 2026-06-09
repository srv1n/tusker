#!/usr/bin/env bash
set -euo pipefail

fail=0
for path in research/legacy site tusker/_config/docs-map.yaml tusker/_config/knowledge-policy.yaml .agents/skills/tusker; do
  if [ -e "$path" ]; then
    echo "legacy/stale surface present: $path" >&2
    fail=1
  fi
done

if find tusker.yaml .tusker skill docs -type f \( -name '*.yaml' -o -name '*.yml' -o -name '*.md' \) -print0 2>/dev/null | xargs -0 grep -In 'orchestration:' >/tmp/tusker-v7-surface.$$ 2>/dev/null; then
  cat /tmp/tusker-v7-surface.$$ >&2
  rm -f /tmp/tusker-v7-surface.$$
  echo "use automation:, not orchestration:" >&2
  fail=1
fi
rm -f /tmp/tusker-v7-surface.$$

exit "$fail"
