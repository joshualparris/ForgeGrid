#!/bin/bash
INBOX=$(./forgegrid agent-bridge inbox 2>/dev/null)
if [[ "$INBOX" == "" || "$INBOX" == "[]" || "$INBOX" == "null" ]]; then
  echo "No work pending."
else
  COUNT=$(echo "$INBOX" | grep -c '"id":')
  echo "$COUNT pending message(s) in inbox."
fi
