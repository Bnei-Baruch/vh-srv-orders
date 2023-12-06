#!/usr/bin/env sh

set +e
set -x

BASE_DIR="/"
TIMESTAMP="$(date '+%Y%m%d%H%M%S')"
LOG_FILE="/tmp/robokasa_$TIMESTAMP.log"

cleanup() {
  find /tmp -name "robokasa_*.log" -type f -mmin +60 -exec rm -f {} \;
}

cd ${BASE_DIR} &&
  ./orders robokasa >>"${LOG_FILE}" 2>&1

WARNINGS="$(grep -Eic "warning|error:" ${LOG_FILE})"
if [ "$WARNINGS" = "0" ]; then
  echo "No warnings"
  cleanup
  exit 0
fi

curl --request POST \
  --url https://api.sendgrid.com/v3/mail/send \
  --header "authorization: Bearer ${SENDGRID_API_KEY}" \
  --header 'Content-Type: application/json' \
  --data '{"personalizations": [{"to": [{"email": "edoshor@gmail.com"}]}],"from": {"email": "vh-srv-orders@kli.one"},"subject":"ERROR: VH orders - robokasa import","content": [{"type": "text/html","value": "Hey,<br>Please see attached log file."}], "attachments": [{"content": "'$(base64 -w 0 ${LOG_FILE})'", "type": "text/plain", "filename": "robokasa.log"}]}'

cleanup
exit 1
