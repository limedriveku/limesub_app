package main

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// parseTimeStringToMs supports HH:MM:SS.mmm, MM:SS.mmm, "1234ms", "1.234s", SRT format with comma
func parseTimeStringToMs(s string) int64 {
	if s == "" {
		return 0
	}
	s = strings.TrimSpace(s)
	// handle SRT style with comma
	s = strings.ReplaceAll(s, ",", ".")
	// ms suffix
	if strings.HasSuffix(strings.ToLower(s), "ms") {
		v := strings.TrimSuffix(strings.ToLower(s), "ms")
		f, _ := strconv.ParseFloat(strings.TrimSpace(v), 64)
		return int64(f)
	}
	if strings.HasSuffix(strings.ToLower(s), "s") {
		v := strings.TrimSuffix(strings.ToLower(s), "s")
		f, _ := strconv.ParseFloat(strings.TrimSpace(v), 64)
		return int64(f * 1000)
	}
	// colon format
	if strings.Count(s, ":") >= 1 {
		parts := strings.Split(s, ":")
		// handle HH:MM:SS.mmm or MM:SS.mmm etc.
		var hh, mm, ss float64
		if len(parts) == 3 {
			hh, _ = strconv.ParseFloat(parts[0], 64)
			mm, _ = strconv.ParseFloat(parts[1], 64)
			ss, _ = strconv.ParseFloat(parts[2], 64)
		} else if len(parts) == 2 {
			mm, _ = strconv.ParseFloat(parts[0], 64)
			ss, _ = strconv.ParseFloat(parts[1], 64)
		} else {
			// fallback
			f, _ := strconv.ParseFloat(s, 64)
			return int64(f * 1000)
		}
		total := hh*3600 + mm*60 + ss
		return int64(total * 1000)
	}
	// fallback numeric
	f, err := strconv.ParseFloat(s, 64)
	if err == nil {
		// interpret as seconds if small? To be safe: if >1000 treat as ms, else seconds
		if f > 1000 {
			return int64(f)
		}
		return int64(f * 1000)
	}
	return 0
}

func formatMsToSRTTime(ms int64) string {
	h := ms / 3600000
	m := (ms % 3600000) / 60000
	s := (ms % 60000) / 1000
	msr := ms % 1000
	return fmt.Sprintf("%02d:%02d:%02d,%03d", h, m, s, msr)
}

// safeTrimAndNormalizeSpaces: preserve internal newlines, trim each line
func safeTrimAndNormalizeSpaces(text string) string {
	lines := strings.Split(text, "\n")
	for i := range lines {
		lines[i] = strings.TrimSpace(lines[i])
	}
	return strings.Join(lines, "\n")
}
