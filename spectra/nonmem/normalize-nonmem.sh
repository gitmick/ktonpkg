#!/bin/sh
# normalize-nonmem.sh <in> <out>: one NONMEM output with what changes from run to run taken out.
# Only time, date, license and CPU; numbers, the NONMEM version and the model stay, so two runs agree
# here exactly when NONMEM computed the same. The type comes from the content, not the name, so every
# application is the same potential (one protocol ref) whatever the file is called:
#   .xml  (starts with <?xml or <nm:)  start/stop datetime, elapsed and CPU times, license information
#   .cpu  (one number, nothing else)   the whole file is a CPU time: emptied
#   else  (.lst and any other text)    the date lines, the license block, elapsed and CPU times
set -eu
in=$1; out=$2
if head -c 512 "$in" | grep -q '<?xml\|<nm:'; then
  sed -E \
    -e 's#(<nm:(start|stop)_datetime>)[^<]*(</nm:(start|stop)_datetime>)#\1<date>\3#g' \
    -e 's#(<nm:[a-z_]*elapsed_time>)[^<]*(</nm:[a-z_]*elapsed_time>)#\1<time>\2#g' \
    -e 's#(<nm:total_cputime>)[^<]*(</nm:total_cputime>)#\1<time>\2#g' \
    -e '/<nm:license_information>/,/<\/nm:license_information>/{/<nm:license_information>/!{/<\/nm:license_information>/!d;};}' \
    "$in" > "$out"
elif ! grep -qvE '^[[:space:]]*[-+0-9.Ee]*[[:space:]]*$' "$in"; then
  : > "$out"
else
  sed -E \
    -e 's/^(Mon|Tue|Wed|Thu|Fri|Sat|Sun) (Jan|Feb|Mar|Apr|May|Jun|Jul|Aug|Sep|Oct|Nov|Dec) [ 0-9][0-9] [0-9:]{8} [A-Z]+ [0-9]{4} *$/<date>/' \
    -e 's/^(License Registered to:).*/\1 <license>/' \
    -e 's/^(Expiration Date:).*/\1 <license>/' \
    -e 's/^(Days until program expires *:).*/\1 <license>/' \
    -e 's/^( *Elapsed [a-z]+ +time in seconds:).*/\1 <time>/' \
    -e 's/^( *#CPUT: Total CPU Time in Seconds,).*/\1 <time>/' \
    "$in" > "$out"
fi
