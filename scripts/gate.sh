#!/bin/sh
set -eu
root=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)
missing=""
[ -n "${AII_OS_DIR:-}" ] && [ -d "${AII_OS_DIR:-}" ] || missing="$missing AII_OS_DIR(host source checkout)"
[ -n "${AII_OS_BIN:-}" ] && [ -d "${AII_OS_BIN:-}" ] || missing="$missing AII_OS_BIN(worker oracle)"
for tool in go tinygo python3; do
  command -v "$tool" >/dev/null 2>&1 || missing="$missing $tool"
done
if [ -n "$missing" ]; then
  echo "gate: INCOMPLETE — missing:$missing; nothing was run" >&2
  exit 3
fi
AII_OS_DIR=$(CDPATH= cd -- "$AII_OS_DIR" && pwd)
AII_OS_BIN=$(CDPATH= cd -- "$AII_OS_BIN" && pwd)
export AII_OS_DIR AII_OS_BIN
cd "$root"
step() { echo "gate: $*" >&2; }
fail() { echo "gate: FAIL — $*" >&2; exit 1; }

step "format"
drift=$(gofmt -l .)
[ -z "$drift" ] || fail "gofmt drift in: $drift"
step "vet"
go vet ./... || fail "go vet"
step "unit tests"
go test ${GATE_GO_TEST_FLAGS:--race -count=1} ./... || fail "go test"
step "script tests"
python3 -B -m unittest discover -s scripts -p 'test_*.py' || fail "script tests"
sh -n integration/run.sh || fail "integration/run.sh does not parse"
step "build and package"
go tool aiisdk build || fail "aiisdk build"
go tool aiisdk package || fail "aiisdk package"
version=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["version"])' plugin.json)
stage="$root/dist/pkg/id.aiii.jev-$version"
python3 scripts/check_package_settings.py "$stage" || fail "packaged settings differ from plugin.json"
step "worker oracle: cases and the confirmation stance"
go tool aiisdk test -skip-build || fail "aiisdk test"
step "integration proof through the host's broker"
JEV_STAGE="$stage" sh integration/run.sh || fail "integration proof"
step "PASS"
