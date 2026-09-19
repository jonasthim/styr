#!/usr/bin/env bash
# Unit checks for the installer's version helpers. Run by `make check`.
set -euo pipefail
cd "$(dirname "$0")/.."
# shellcheck disable=SC1090
source <(sed -n '/^normalize_version()/,/^}/p; /^version_lt()/,/^}/p' deploy/install.sh)
fail() { echo "test-install: $*" >&2; exit 1; }
[[ "$(normalize_version 'styr 0.4.0 (5220c85)')" == "0.4.0" ]] || fail "decorated version not normalised"
[[ "$(normalize_version 'v0.5.0')" == "0.5.0" ]] || fail "v prefix not stripped"
[[ "$(normalize_version '(not installed)')" == "(not installed)" ]] || fail "non-version altered"
version_lt 0.4.0 0.5.0 || fail "0.4.0 should sort before 0.5.0"
version_lt 0.9.0 0.10.0 || fail "0.9.0 should sort before 0.10.0"
! version_lt 0.5.0 0.5.0 || fail "equal versions are not lt"
! version_lt "$(normalize_version 'v0.5.0')" "$(normalize_version 'styr 0.4.0 (5220c85)')" || fail "upgrade misread as downgrade"
echo "test-install: ok"
