#!/usr/bin/env bash

# Shared runtime checks for setup and preflight scripts. Callers provide the
# command lookup environment; these helpers do not install or mutate anything.

goodman_go_is_supported() {
	command -v go >/dev/null 2>&1 || return 1
	local version major minor
	version="$(go env GOVERSION 2>/dev/null || true)"
	version="${version#go}"
	major="${version%%.*}"
	minor="${version#*.}"
	minor="${minor%%.*}"
	[[ "$major" =~ ^[0-9]+$ && "$minor" =~ ^[0-9]+$ ]] || return 1
	(( major > 1 || (major == 1 && minor >= 25) ))
}

goodman_node_supports_openclaw() {
	command -v node >/dev/null 2>&1 || return 1
	node -e '
const [major, minor, patch] = process.versions.node.split(".").map(Number);
const atLeast = (wantMajor, wantMinor, wantPatch) =>
  major > wantMajor || (major === wantMajor && (minor > wantMinor || (minor === wantMinor && patch >= wantPatch)));
process.exit(
  (major === 22 && atLeast(22, 22, 3)) ||
  (major === 24 && atLeast(24, 15, 0)) ||
  (major === 25 && atLeast(25, 9, 0)) || major > 25 ? 0 : 1
);'
}
