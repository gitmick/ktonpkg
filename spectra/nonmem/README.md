# spectra/nonmem — the minimal NONMEM spectrum (MH 11.11.)

One normaliser and one reference model. Not a spectrum framework.

- `normalize-nonmem.sh <in> <out>`: takes out of one NONMEM output what changes from run to run and
  nothing else. `.lst`: the date lines, the license lines, `Elapsed … time in seconds`, `#CPUT`.
  `.xml`: `nm:start_datetime`, `nm:stop_datetime`, `nm:*elapsed_time`, `nm:total_cputime`, the
  license information. `.cpu`: emptied. The type comes from the content, so every application is the
  same potential. Numbers, the NONMEM version and the model stay.
- `nonmem.go`: the script embedded (no second implementation), `Ray()`, `Ref()` (the potential:
  script hash + args + pinned busybox image; pinned in the test) and `Application(output, normalized)`,
  the foton of one application for a caller to sign and add.
- `build.sh`: the tool package from two runs of one model. It runs the normaliser in its image on
  both runs, refuses if they differ afterwards, and builds `nonmem-spectrum` (profile tool) with the
  normalised reference outputs as members (not `.cpu`: emptied, it qualifies nothing).

## L1 in plankton

plankton's `Reproduces` reaches L1 for two outputs when each has an application foton with this ref
taking it as input, and their first outputs are equal. Re-execute the normaliser (from this package,
in its image) rather than trust a recorded application: kton `security/attacks/normalizer-forge.sh`.
