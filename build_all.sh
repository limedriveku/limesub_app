#!/usr/bin/env bash
set -e
echo "Building for linux amd64..."
GOOS=linux GOARCH=amd64 go build -o dist/limesubv3-linux main.go parsers.go pipeline.go ass_resample.go utils.go
echo "Building for windows amd64 (GUI exe)..."
GOOS=windows GOARCH=amd64 go build -ldflags="-H=windowsgui -s -w" -o dist/limesubv3-windows.exe main.go parsers.go pipeline.go ass_resample.go utils.go messagebox_windows.go
echo "Done. Check dist/"
