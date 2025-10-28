package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// supported extensions
var supported = map[string]bool{
	".srt":  true,
	".json": true,
	".xml":  true,
	".ttml": true,
	".ass":  true,
}

func main() {
	// CLI flags (minimal)
	tolerance := flag.Float64("tolerance", 0.1, "time tolerance in seconds for merging continuous dialogs")
	outdir := flag.String("outdir", "", "override output directory (optional)")
	keepTemp := flag.Bool("keep-temp", false, "keep temporary SRT output (for debugging, not used in in-memory mode)")
	flag.Parse()

	if len(flag.Args()) == 0 {
		// no args: if on Windows show MessageBox; otherwise print to stdout
		msg := "No subtitle file provided.\n\nPlease drag and drop subtitle file(s) onto this program or run it from the command line."
		if runtime.GOOS == "windows" {
			showMessageBox("Limesub v3", msg, "info")
			os.Exit(0)
		}
		fmt.Println(msg)
		os.Exit(0)
	}

	// Process all provided args (supports drag & drop multiple files)
	for _, in := range flag.Args() {
		if err := processOne(in, *outdir, *tolerance, *keepTemp); err != nil {
			// show MessageBox on Windows for errors (professional message)
			errMsg := fmt.Sprintf("Failed to process '%s': %v", in, err)
			if runtime.GOOS == "windows" {
				// More elegant message
				showMessageBox("Limesub v3 — Processing Error", fmt.Sprintf("An error occurred while processing the file:\n\n%s\n\nPlease verify the file is valid.", filepath.Base(in)), "error")
			} else {
				fmt.Fprintln(os.Stderr, errMsg)
			}
		}
	}
}
