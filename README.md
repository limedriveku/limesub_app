\# limesubv3 — Subtitle converter \& resampler



Converts SRT / JSON / XML / TTML → ASS (Limenime style), in-memory pipeline.

Also normalizes and resamples input `.ass` to 1920×1080 with `Basic Comical NC` font.



\## Features

\- Drag \& drop support for binaries (Windows/Linux)

\- In-memory conversion pipeline (no temporary SRT files)

\- Support: SRT, JSON, XML, TTML, ASS (resample)

\- Auto-naming: `<name>\_Limenime.ass` with auto-numbering

\- Windows MessageBox on double-click/no-args \& unknown format

\- Cross-build scripts (build\_all.sh) and GitHub Actions example



\## Build (Windows GUI executable)

```bash

\# Build GUI exe (no console) for Windows

go build -ldflags="-H=windowsgui -s -w" -o limesubv3.exe main.go parsers.go pipeline.go ass\_resample.go utils.go messagebox\_windows.go



