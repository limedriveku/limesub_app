package main

import (
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// ---------- PIPELINE: SRTBlocks -> ASS lines (in memory) ----------

func ConvertSRTBlocksToASS(blocks []SRTBlock, tolerance float64) []string {
	// 1. split multiline -> make one block per line (but keep original timing)
	lines := splitMultiline(blocks)

	// 2. detectStyle (set Style field per block by adding meta in Text prefix: we return new structure)
	type Dlg struct {
		Start int64
		End   int64
		Text  string // text with original tags intact
		Style string
	}
	var dlgs []Dlg
	for _, b := range lines {
		style := detectStyle(b.Text)
		dlgs = append(dlgs, Dlg{Start: b.StartMs, End: b.EndMs, Text: b.Text, Style: style})
	}
	// 3. mergeSameOrContinuous
	dlgs = mergeSameOrContinuous(dlgs, tolerance)
	// 4. mergeSameTimeAndStyle -> combine text with \N
	dlgs = mergeSameTimeAndStyle(dlgs)
	// 5. clean tags: remove \fn and \fs, also remove existing \blur and \fad to prevent duplicates
	for i := range dlgs {
		dlgs[i].Text = cleanFontAndEffects(dlgs[i].Text)
	}
	// 6. apply default effects except style tanda
	var assLines []string
	for _, d := range dlgs {
		text := d.Text
		if d.Style != "tanda" {
			text = `{\blur3}{\fad(00,40)}` + text
		}
		// format time to H:MM:SS.cs (ASS uses centiseconds in many implementations; we'll format as H:MM:SS.ss)
		start := formatMsToASSTime(d.Start)
		end := formatMsToASSTime(d.End)
		line := fmt.Sprintf("Dialogue: 0,%s,%s,%s,,0,0,0,,%s", start, end, d.Style, text)
		assLines = append(assLines, line)
	}
	return assLines
}

// split multiline blocks into per-line blocks with same timing
func splitMultiline(blocks []SRTBlock) []SRTBlock {
	var out []SRTBlock
	for _, b := range blocks {
		lines := strings.Split(b.Text, "\n")
		for _, ln := range lines {
			out = append(out, SRTBlock{
				Index:   b.Index,
				StartMs: b.StartMs,
				EndMs:   b.EndMs,
				Text:    strings.TrimSpace(ln),
			})
		}
	}
	return out
}

// detectStyle: returns "tanda" or "Default"
func detectStyle(text string) string {
	// remove all {..} tags temporarily
	reTag := regexp.MustCompile(`\{.*?\}`)
	clean := reTag.ReplaceAllString(text, "")
	clean = strings.TrimSpace(clean)
	if clean == "" {
		return "Default"
	}
	// check if full caps (no lowercase a-z)
	if regexp.MustCompile(`^[^a-z]+$`).MatchString(clean) {
		return "tanda"
	}
	// check if wrapped in () or []
	if regexp.MustCompile(`^\s*[\(\[].*[\)\]]\s*$`).MatchString(clean) {
		return "tanda"
	}
	return "Default"
}

// mergeSameOrContinuous:
// - remove exact duplicates (same start/end, same style, same text) keeping first
// - merge consecutive blocks with same style/text when end == next.start (within tolerance)
func mergeSameOrContinuous(dlgs []struct {
	Start int64
	End   int64
	Text  string
	Style string
}, tolerance float64) []struct {
	Start int64
	End   int64
	Text  string
	Style string
} {
	// convert to mutable slice
	type D struct {
		Start int64
		End   int64
		Text  string
		Style string
	}
	var arr []D
	for _, d := range dlgs {
		arr = append(arr, D{Start: d.Start, End: d.End, Text: d.Text, Style: d.Style})
	}
	// sort by start
	sort.SliceStable(arr, func(i, j int) bool { return arr[i].Start < arr[j].Start })
	var merged []D
	for i := 0; i < len(arr); i++ {
		cur := arr[i]
		if len(merged) == 0 {
			merged = append(merged, cur)
			continue
		}
		last := &merged[len(merged)-1]
		if last.Style == cur.Style && normalizeSpaces(last.Text) == normalizeSpaces(cur.Text) {
			// exact duplicate (same start and end)
			if last.Start == cur.Start && last.End == cur.End {
				// skip duplicate
				continue
			}
			// continuous?
			if math.Abs(float64(cur.Start-last.End)) <= tolerance*1000 {
				last.End = cur.End
				continue
			}
		}
		merged = append(merged, cur)
	}
	// convert back
	out := make([]struct {
		Start int64
		End   int64
		Text  string
		Style string
	}, len(merged))
	for i, m := range merged {
		out[i] = struct {
			Start int64
			End   int64
			Text  string
			Style string
		}{Start: m.Start, End: m.End, Text: m.Text, Style: m.Style}
	}
	return out
}

// normalizeSpaces helper
func normalizeSpaces(s string) string {
	// collapse spaces
	return strings.TrimSpace(regexp.MustCompile(`\s+`).ReplaceAllString(s, " "))
}

// mergeSameTimeAndStyle: group same start/end/style -> join text with \N
func mergeSameTimeAndStyle(dlgs []struct {
	Start int64
	End   int64
	Text  string
	Style string
}) []struct {
	Start int64
	End   int64
	Text  string
	Style string
} {
	// group by start|end|style
	type key struct {
		S, E int64
		Sty  string
	}
	groups := make(map[key][]string)
	order := []key{}
	for _, d := range dlgs {
		k := key{S: d.Start, E: d.End, Sty: d.Style}
		if _, ok := groups[k]; !ok {
			order = append(order, k)
		}
		groups[k] = append(groups[k], d.Text)
	}
	var out []struct {
		Start int64
		End   int64
		Text  string
		Style string
	}
	for _, k := range order {
		texts := groups[k]
		joined := strings.Join(texts, `\N`)
		out = append(out, struct {
			Start int64
			End   int64
			Text  string
			Style string
		}{Start: k.S, End: k.E, Text: joined, Style: k.Sty})
	}
	return out
}

// cleanFontAndEffects: remove \fn..., \fs..., \blur..., \fad...
func cleanFontAndEffects(s string) string {
	// Remove \fnNAME and \fsNUMBER, and existing {\blur...} and {\fad(...)}
	reFn := regexp.MustCompile(`(?i)\\fn[^\\\}]+`)
	reFs := regexp.MustCompile(`(?i)\\fs\d+`)
	reBlur := regexp.MustCompile(`(?i)\{\\blur[^}]*\}`)
	reFad := regexp.MustCompile(`(?i)\{\\fad\([^}]*\)}`) // crude
	// also remove inline \blur5 etc inside tags
	reBlurInline := regexp.MustCompile(`(?i)\\blur[0-9.]+`)
	reFadInline := regexp.MustCompile(`(?i)\\fad\([^)]*\)`)
	out := reFn.ReplaceAllString(s, "")
	out = reFs.ReplaceAllString(out, "")
	out = reBlur.ReplaceAllString(out, "")
	out = reFad.ReplaceAllString(out, "")
	out = reBlurInline.ReplaceAllString(out, "")
	out = reFadInline.ReplaceAllString(out, "")
	// ensure curly tag wrappers are tidy
	out = strings.ReplaceAll(out, "{}","")
	return out
}

// formatMsToASSTime -> H:MM:SS.xx (centiseconds)
func formatMsToASSTime(ms int64) string {
	totalSec := ms / 1000
	h := totalSec / 3600
	m := (totalSec % 3600) / 60
	s := totalSec % 60
	centi := (ms % 1000) / 10
	return fmt.Sprintf("%d:%02d:%02d.%02d", h, m, s, centi)
}
