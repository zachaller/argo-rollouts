#!/usr/bin/env bash
# Prints the job matrix of the e2e job in .github/workflows/testing.yaml as one
# line of JSON: a list of {version, k3s_version, latest, parallel, job, bug, label}.
#
# Environment:
#   VERSIONS    JSON list of {version, k3s_version, latest}                (required)
#   KUBERNETES  "all" or "latest": which of those versions to run          (default all)
#   PARALLEL    go test -parallel value; a comma-separated list runs one
#               configuration per value                                    (default 1)
#   JOBS        identical jobs per configuration, to collect more samples  (default 1)
#   BUGS        "none", "all", or a comma-separated list of patch names
#               (without .patch) under test/e2e/seeded-bugs                (default none)
#
# label is empty for the default configuration, so job names only change when
# the workflow is dispatched with other settings.
set -euo pipefail

: "${VERSIONS:?VERSIONS is required}"
KUBERNETES="${KUBERNETES:-all}"
PARALLEL="${PARALLEL:-1}"
JOBS="${JOBS:-1}"
BUGS="${BUGS:-none}"
BUGS_DIR="${BUGS_DIR:-$(dirname "$0")/../test/e2e/seeded-bugs}"

case "$BUGS" in
none | "")
	bugs='[""]'
	;;
all)
	bugs=$(find "$BUGS_DIR" -maxdepth 1 -name '*.patch' -exec basename {} .patch \; | sort | jq -Rcn '[inputs]')
	if [ "$bugs" = "[]" ]; then
		echo "no patches found in $BUGS_DIR" >&2
		exit 1
	fi
	;;
*)
	bugs=$(jq -cn --arg list "$BUGS" '$list | split(",") | map(gsub("\\s"; "") | select(length > 0))')
	for bug in $(jq -r '.[]' <<<"$bugs"); do
		if [ ! -f "$BUGS_DIR/$bug.patch" ]; then
			echo "no such seeded bug: $BUGS_DIR/$bug.patch" >&2
			exit 1
		fi
	done
	;;
esac

jq -cn \
	--argjson versions "$VERSIONS" \
	--arg kubernetes "$KUBERNETES" \
	--arg parallel "$PARALLEL" \
	--argjson jobs "$JOBS" \
	--argjson bugs "$bugs" '
	[$versions[] | select($kubernetes == "all" or .latest)] as $vs
	| ($parallel | split(",") | map(gsub("\\s"; "") | select(length > 0))) as $ps
	| [
		$vs[] as $v | $ps[] as $p | range(1; $jobs + 1) as $j | $bugs[] as $b
		| $v + {
			parallel: $p,
			job: $j,
			bug: $b,
			label: (
				(if ($ps | length) > 1 or $p != "1" then ", parallel \($p)" else "" end)
				+ (if $jobs > 1 then ", job \($j)" else "" end)
				+ (if $b != "" then ", bug \($b)" else "" end)
			)
		}
	]'
