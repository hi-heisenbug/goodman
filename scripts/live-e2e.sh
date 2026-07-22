#!/usr/bin/env bash
# Run Goodman's real kernel proofs from a root-capable host or privileged
# container. The e2e scripts retain ownership of their own cleanup.
set -euo pipefail
cd "$(dirname "$0")/.."

MODE="${1:-all}"
case "$MODE" in
	openclaw)
		GOODMAN_E2E_SKIP_BUILD=1 bash test/e2e/openclaw_test.sh
		;;
	drift)
		GOODMAN_E2E_SKIP_BUILD=1 bash test/e2e/drift_test.sh
		;;
	all)
		GOODMAN_E2E_SKIP_BUILD=1 bash test/e2e/openclaw_test.sh
		GOODMAN_E2E_SKIP_BUILD=1 bash test/e2e/drift_test.sh
		;;
	*)
		echo "Usage: scripts/live-e2e.sh [openclaw|drift|all]" >&2
		exit 2
		;;
esac
