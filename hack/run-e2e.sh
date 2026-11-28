#!/usr/bin/env bash
# Runs the e2e tests against the current kubeconfig, E2E_REPEAT times one after
# another, and records how long each run took in e2e-timings.txt. The JUnit
# file of run N is kept as junit-e2e-run-N.xml.
#
# Environment (all optional):
#   E2E_REPEAT       number of runs                                   (default 1)
#   E2E_CONTROLLER   "inprocess": the test binary runs the controller  (default)
#                    "external": a controller was started separately
#                    "off": there is no controller, on purpose
#   plus everything `make test-e2e` reads: E2E_PARALLEL, E2E_TEST_OPTIONS,
#   E2E_RERUN_FAILS, E2E_WAIT_TIMEOUT, E2E_COVERAGE.
set -uo pipefail

repeat="${E2E_REPEAT:-1}"
controller_pattern='exe/main --instance-id'
controller_died=/tmp/e2e-controller-died
rm -f "$controller_died"
: >e2e-timings.txt

# Build the tools and the test binary first, so that neither is part of the
# time measured for the first run.
make install-devtools-local || exit 1
make build-e2e || exit 1

# Without a controller every test only waits for its timeout, so stop as soon
# as a separately started controller is gone. A controller inside the test
# binary cannot go without the tests.
watchdog=
if [ "${E2E_CONTROLLER:-inprocess}" = "external" ]; then
	(
		while pgrep -f "$controller_pattern" >/dev/null; do sleep 5; done
		touch "$controller_died"
		echo "::error::The controller exited during the e2e run"
		pkill -f gotestsum
		pkill -f e2e.test
	) &
	watchdog=$!
fi

status=0
for i in $(seq 1 "$repeat"); do
	start=$(date +%s)
	# A run that is cut short writes no JUnit file. Remove the previous one so
	# that it is not kept as this run's result.
	rm -f junit-e2e-test.xml
	if make test-e2e; then result=pass; else
		result=fail
		status=1
	fi
	echo "run $i of $repeat: $result in $(($(date +%s) - start))s (parallel ${E2E_PARALLEL:-8})" | tee -a e2e-timings.txt
	[ -f junit-e2e-test.xml ] && cp junit-e2e-test.xml "junit-e2e-run-$i.xml"
	if [ -e "$controller_died" ]; then
		echo "controller exited; skipping the remaining runs" | tee -a e2e-timings.txt
		break
	fi
done

[ -n "$watchdog" ] && kill "$watchdog" 2>/dev/null
if [ -n "${GITHUB_STEP_SUMMARY:-}" ]; then
	{
		echo '```'
		cat e2e-timings.txt
		echo '```'
	} >>"$GITHUB_STEP_SUMMARY"
fi
exit "$status"
