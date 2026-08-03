#!/bin/bash
echo "Building ForgeGrid..."
GOOS=windows GOARCH=amd64 go build -o dist/ForgeGrid-USB/Windows/ForgeGrid.exe main.go
GOOS=linux GOARCH=amd64 go build -o dist/ForgeGrid-USB/Linux/forgegrid main.go
cd dist/ForgeGrid-USB && sha256sum Windows/ForgeGrid.exe Linux/forgegrid Windows/START-FORGEGRID.bat Linux/start-forgegrid.sh README.html Examples/forgegrid.yaml > CHECKSUMS.txt
echo "Build complete."
