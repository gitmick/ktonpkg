#!/usr/bin/env bash
# build.sh <runs-dir> <out-scope-package> <key> [label]: the nonmem-spectrum package from two runs of
# one model (<runs-dir>/run-a, run-b with one .lst .xml .cpu each; <runs-dir>/model.ctl, data.csv).
# 1. every output of both runs through the normaliser, in its pinned image (as anyone re-executes it)
# 2. run-a and run-b must normalise to the same bytes, or nothing is built
# 3. the kton-package/1 bundle (build/main.go), then the sealed scope package (scope from-bundle)
set -euo pipefail
HERE="$(cd "$(dirname "$0")" && pwd)"; ROOT="$HERE/../.."
RUNS=$(cd "$1" && pwd); OUT=$2; KEY=$3; LABEL=${4:-v1}
IMG=$(sed -n 's/^\tImage *= *"oci:\/\/docker.io\/library\/\(.*\)"/\1/p' "$HERE/nonmem.go")
W=$(mktemp -d); trap 'rm -rf "$W"' EXIT
mkdir -p "$W/ref/normalized" "$W/ref/raw"
for r in run-a run-b; do
  for t in lst xml cpu; do
    set -- "$RUNS/$r"/*."$t"; [ $# = 1 ] && [ -f "$1" ] || { echo "$r: want exactly one .$t"; exit 1; }
    mkdir -p "$W/$r/$t"; cp "$1" "$W/$r/$t/in"; cp "$HERE/normalize-nonmem.sh" "$W/$r/$t/"
    docker run --rm --network none -v "$W/$r/$t:/w" -w /w "$IMG" sh normalize-nonmem.sh in normalized
  done
done
for t in lst xml cpu; do
  cmp -s "$W/run-a/$t/normalized" "$W/run-b/$t/normalized" || {
    echo "run-a and run-b differ after normalising: .$t"; diff "$W/run-a/$t/normalized" "$W/run-b/$t/normalized" | head -20; exit 1; }
  cp "$W/run-a/$t/normalized" "$W/ref/normalized/run1.$t"
done
cp "$RUNS"/run-a/*.lst "$W/ref/raw/run1.lst"; cp "$RUNS/model.ctl" "$RUNS/data.csv" "$W/ref/"
( cd "$ROOT" && go run ./spectra/nonmem/build "$W/bundle" "$W/ref" )
( cd "$ROOT" && go run ./cmd/scope from-bundle "$W/bundle" "$OUT" "$LABEL" --key "$KEY" ) | grep -E '"(package|revision|problems)"'
