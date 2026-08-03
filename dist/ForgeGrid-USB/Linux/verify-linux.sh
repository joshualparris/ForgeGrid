#!/bin/bash
echo "Verifying ForgeGrid Linux Runtime..."
PORT=48192
./dist/ForgeGrid-USB/Linux/forgegrid -mode coordinator -port $PORT -insecure > /dev/null 2>&1 &
PID=$!

sleep 2
HTTP_STATUS=$(curl -s -o /dev/null -w "%{http_code}" http://127.0.0.1:$PORT/api/coordinator/status)

kill -9 $PID

if [ "$HTTP_STATUS" == "200" ]; then
    echo "Linux runtime verified successfully."
    exit 0
else
    echo "Linux runtime verification failed (HTTP $HTTP_STATUS)"
    exit 1
fi
