# spectra/nonmem — the minimal NONMEM spectrum (MH 11.11.)

One normaliser and one reference model. Not a spectrum framework.

- `normalize-nonmem.sh <in> <out>`: takes out of one NONMEM output what changes from run to run and
  nothing else. `.lst`: the date lines (also `Current Date:`), the license lines, `Elapsed … time in seconds`, `#CPUT`.
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

## The tool package `nonmem-7.5` (TQ P5)

`tool/` is the source of the tool package that `scope tool` builds: the image by digest
(`psn5.4.0-nonmem751`, TQ D4), the frame `nmfe75 <ctl> <output>`, the licence as hidden configuration
(mounted read-only at `/opt/nm/license/nonmem.lic`, never in the package), one member `theo-run1` (THEO-101
run1 with `$DATA` and `$TABLE` flat, its listing as the output), this normaliser in its own busybox image
(same ref as `Ref()`, tested) and provisional requirements NM-IQ-1, NM-IQ-2, NM-OQ-1, NM-OQ-2.
NM-OQ-2 (vendor test models) is untested: the image carries no NONMEM examples.

```
measure.sh tool <work> <licence> <key>            # the reference run, signed; prints --run theo-run1=<id>
scope tool <out> v1 --src spectra/nonmem/tool --registry <work>/registry --root <work>/root \
  --pub <work>/runner.pub --run theo-run1=<id> --key <key>
measure.sh tool <work2> <licence> <key>           # a candidate run
go run ./spectra/nonmem/apply <work2>/registry <key> <work>/root/theo-run1/run1.lst <work2>/root/theo-run1/run1.lst
scope spectrum-check <out> --registry <work2>/registry --candidate theo-run1=<hash of the candidate's run1.lst>
```

On 25.11.2026 on ai1: minimisation successful, OFV 133.202254503871, the candidate fulfilled the spectrum
via the normaliser (L1), and NM-IQ-1, NM-IQ-2, NM-OQ-1 passed. The raw outputs equal robert1's runs of
11.11. (image nonmem751) byte for byte, apart from dates and timings.
