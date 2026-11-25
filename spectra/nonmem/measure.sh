#!/usr/bin/env bash
# measure.sh <tool-src> <work> <licence> <key> [member...]: the reference runs of the NONMEM tool package.
# For each member of <tool-src>/tool.json (or the ones named): stage its control stream and inputs flat
# in <work>/root/<member>/, run nmfe75 <ctl> <output> in the pinned image with the licence mounted
# read-only and no network, then sign the run with plankton author into <work>/registry. Prints
# --run <member>=<foton id> for scope tool, and writes <work>/runner.pub.
# The licence is mounted by path only: it is never staged, recorded, hashed into a foton or printed.
set -euo pipefail
SRC=$(cd "$1" && pwd); WORK=$2; LIC=$3; KEY=$4; shift 4
[ -r "$LIC" ] || { echo "licence $LIC not readable" >&2; exit 1; }
mkdir -p "$WORK/root" "$WORK/registry"; WORK=$(cd "$WORK" && pwd)
plankton pubkey "$KEY" > "$WORK/runner.pub"
# tool <expr>: a value of tool.json, one line per item (python3; no jq on ai1)
tool() { python3 -I -c 'import json,re,sys; t=json.load(open(sys.argv[1])); m={x["name"]:x for x in t["members"]}
v=eval(sys.argv[2],{"t":t,"m":m,"a":sys.argv[3:],"re":re}); print("\n".join(v) if isinstance(v,list) else v)' "$SRC/tool.json" "$@"; }
IMG=$(tool 're.sub(r"^oci://(docker\.io/)?", "", t["image"])')
members=("$@"); [ ${#members[@]} -gt 0 ] || mapfile -t members < <(tool '[x["name"] for x in t["members"]]')
for m in "${members[@]}"; do
  ctl=$(tool 'm[a[0]]["file"] if a[0] in m else ""' "$m")
  out=$(tool 'm[a[0]]["output"] if a[0] in m else ""' "$m")
  [ -n "$ctl" ] || { echo "no member $m" >&2; exit 1; }
  d="$WORK/root/$m"; rm -rf "$d"; mkdir -p "$d"
  cp "$SRC/$ctl" "$d/"
  ins=("$m/$(basename "$ctl")")
  while read -r f; do [ -n "$f" ] && cp "$SRC/$f" "$d/" && ins+=("$m/$(basename "$f")"); done \
    < <(tool 'm[a[0]].get("inputs", [])' "$m")
  chmod -R u+rwX "$d"
  docker run --rm --network none --user "$(id -u):$(id -g)" -e HOME=/tmp -v "$d:/w" -v "$LIC:/opt/nm/license/nonmem.lic:ro" -w /w \
    --entrypoint nmfe75 "$IMG" "$(basename "$ctl")" "$out" > "$WORK/$m.log" 2>&1
  grep -q "LICENSE HAS EXPIRED" "$d/$out" 2>/dev/null && { echo "$m: the licence has expired" >&2; exit 1; }
  [ -s "$d/$out" ] || { echo "$m: no $out (see $WORK/$m.log)" >&2; exit 1; }
  args=(); for i in "${ins[@]}"; do args+=(--in "$i"); done
  # the listing first (the member's output), then NONMEM's result files as evidence: <ctl base>.* and
  # the tables the control stream names (FILE=); not its scratch (FDATA, FCON, temp_dir, …)
  args+=(--out "$m/$out")
  base=$(basename "$ctl"); base=${base%.*}
  tables=$(grep -o 'FILE=[^ ]*' "$d/$(basename "$ctl")" | cut -d= -f2 || true)
  for f in "$d/$base".* $(for t in $tables; do echo "$d/$t"; done); do
    [ -f "$f" ] || continue
    b="$m/$(basename "$f")"; [ "$b" = "$m/$out" ] && continue
    case " ${ins[*]} " in *" $b "*) continue;; esac
    args+=(--out "$b")
  done
  id=$(cd "$WORK/root" && plankton author "${args[@]}" \
    --cmd "docker run --network none <licence ro at /opt/nm/license/nonmem.lic> oci://$IMG nmfe75 $(basename "$ctl") $out" \
    --sign "$KEY" --registry "$WORK/registry" --add --print-id 2>"$WORK/$m.author.log")
  echo "--run $m=$id"
done
