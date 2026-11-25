; model:       run1
; based_on:    -
; description: Base Model
; tag:         one-compartment + absorption
; tag:         IIV CL, V, KA
; tag:         proportional RUV
; note:        EDA suggests one compartment with first-order absorption; weight range (54.6-86.4 kg) supports testing WT on CL/V
$PROBLEM run1 - base: 1-cmt oral, IIV CL/V/KA, proportional error
$INPUT ID TIME AMT DV MDV EVID CMT WT PREDOSE EXCL
$DATA nm_data.csv IGNORE=@ IGNORE=(EXCL.EQ.1)
$SUBROUTINES ADVAN2 TRANS2
$PK
  TVCL = THETA(1)
  TVV  = THETA(2)
  TVKA = THETA(3)
  CL = TVCL*EXP(ETA(1))
  V  = TVV *EXP(ETA(2))
  KA = TVKA*EXP(ETA(3))
  S2 = V
$ERROR
  IPRED = F
  W = SQRT(SIGMA(1,1))*IPRED
  IF (W.LE.0.001) W = 0.001
  IRES = DV-IPRED
  IWRES = IRES/W
  Y = IPRED*(1+EPS(1))
$THETA (0.1, 3) (1, 30) (0.1, 1.5)
$OMEGA 0.1 0.1 0.3
$SIGMA 0.02
$ESTIMATION METHOD=1 INTERACTION MAXEVAL=9999 PRINT=10
$COVARIANCE PRINT=E
$TABLE ID TIME DV PRED IPRED CWRES IWRES CL V KA ETA1 ETA2 ETA3 WT NOPRINT ONEHEADER FILE=sdtab1
