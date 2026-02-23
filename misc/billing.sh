#!/usr/bin/env sh
#
# Run the monthly billing workflow, log all output, and email results.
# Logs are kept in /tmp for 6 months.
#
# Usage: billing.sh [args...]
#
# All arguments are forwarded to `orders billing start`. Examples:
#   billing.sh                              # current month, all steps
#   billing.sh --month 1 --year 2026        # specific month/year
#   billing.sh --dry-run                    # simulate payment terminal calls
#   billing.sh --charge=false               # skip charging, flag + muhlafim only
#   billing.sh --max-workers 10             # increase concurrency
#
# Required env: SENDGRID_API_KEY

set +e
set -x

BASE_DIR="/"
TIMESTAMP="$(date '+%Y%m%d%H%M%S')"
LOG_FILE="/tmp/billing_$TIMESTAMP.log"

cleanup() {
  find /tmp -name "billing_*.log" -type f -mtime +180 -exec rm -f {} \;
}

cd ${BASE_DIR} &&
  ./orders billing start "$@" >>"${LOG_FILE}" 2>&1

EXIT_CODE=$?

if [ "$EXIT_CODE" = "0" ]; then
  SUBJECT="OK: VH orders - billing workflow"
else
  SUBJECT="ERROR: VH orders - billing workflow (exit $EXIT_CODE)"
fi

curl --request POST \
  --url https://api.sendgrid.com/v3/mail/send \
  --header "authorization: Bearer ${SENDGRID_API_KEY}" \
  --header 'Content-Type: application/json' \
  --data '{"personalizations": [{"to": [{"email": "edoshor@gmail.com"}]}],"from": {"email": "vh-srv-orders@kli.one"},"subject":"'"${SUBJECT}"'","content": [{"type": "text/html","value": "Hey,<br>Please see attached log file."}], "attachments": [{"content": "'$(base64 -w 0 ${LOG_FILE})'", "type": "text/plain", "filename": "billing.log"}]}'

cleanup
exit $EXIT_CODE
