#!/bin/bash
echo "Starting ForgeGrid..."
chmod +x ./forgegrid
./forgegrid -mode coordinator || {
    echo "ForgeGrid failed to start. Press Enter to exit."
    read -r
}
