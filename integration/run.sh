#!/bin/sh
set -eu
: "${AII_OS_DIR:?Set AII_OS_DIR to the host source checkout}"
: "${JEV_STAGE:?Set JEV_STAGE to the packaged staging directory}"
root=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)
AII_OS_DIR=$(CDPATH= cd -- "$AII_OS_DIR" && pwd)
JEV_STAGE=$(CDPATH= cd -- "$JEV_STAGE" && pwd)
export AII_OS_DIR JEV_STAGE
python3 "$root/scripts/check_package_settings.py" "$JEV_STAGE"
TMPDIR=$(CDPATH= cd -- "${TMPDIR:-/tmp}" && pwd)
export TMPDIR
run=$(mktemp -d "$TMPDIR/jev-integration.XXXXXX")
trap 'rm -f "$run/overlay.json"; rmdir "$run"' 0
trap 'exit 129' HUP
trap 'exit 130' INT
trap 'exit 143' TERM
python3 - "$AII_OS_DIR" "$root" "$run/overlay.json" <<'PY'
import json
from pathlib import Path
import sys

host, plugin, out = map(Path, sys.argv[1:])
replacements = {
    str(host / 'internal/pluginhost/generic_jev_external_test.go'):
        str(plugin / 'integration/broker_test.go.txt'),
    str(host / 'internal/conversation/generic_jev_delivery_test.go'):
        str(plugin / 'integration/delivery_test.go.txt'),
}
out.write_text(json.dumps({'Replace': replacements}), encoding='utf-8')
PY
cd "$AII_OS_DIR"
go test -race -overlay "$run/overlay.json" ./internal/pluginhost ./internal/conversation -run '^TestGenericJev' -count=1 -v
