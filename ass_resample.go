package main

import (
	"bufio"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
)

// Resample an existing ASS file to 1920x1080, change font name to Basic Comical NC, rescale pos/fs/bord/shad/blur, margins, etc.
func ResampleASSFileTo1080(inputPath string, outputPath string) error {
	data, err := os.ReadFile(inputPath)
	if err != nil {
		return err
	}
	content := string(data)
	// detect PlayResX and PlayResY
	oldX, oldY := detectPlayRes(content)
	if oldX == 0 || oldY == 0 {
		// assume 1280x720
		oldX = 1280
		oldY = 720
	}
	fx := float64(1920) / float64(oldX)
	fy := float64(1080) / float64(oldY)
	f := (fx + fy) / 2.0

	// 1. Update PlayResX and PlayResY (replace or insert)
	content = updateOrInsertPlayRes(content, 1920, 1080)

	// 2. Insert comment about resample under [Script Info]
	content = insertResampleComment(content)

	// 3. Replace font names in Styles and in {\fn...} inline tags
	// Styles: replace the fontname in style lines (Style: Name,Fontname,Fontsize,...)
	reStyle := regexp.MustCompile(`(?m)^Style:\s*([^,]+),([^,]+),([0-9.]+),(.*)$`)
	content = reStyle.ReplaceAllStringFunc(content, func(line string) string {
		// split by commas but only first few
		parts := strings.SplitN(line, ",", 4)
		if len(parts) < 4 {
			return line
		}
		name := strings.TrimSpace(strings.TrimPrefix(parts[0], "Style:"))
		// parts[1] is fontname
		fontsizePart := parts[2]
		rest := parts[3]
		return fmt.Sprintf("Style: %s,Basic Comical NC,%s,%s", strings.TrimSpace(name), strings.TrimSpace(fontsizePart), rest)
	})
	// inline {\fn...}
	reFnInline := regexp.MustCompile(`(?i)\\fn[^\\\}]+`)
	content = reFnInline.ReplaceAllString(content, `\fnBasic Comical NC`)

	// 4. Rescale numeric tags in Events and Styles
	// change margins in Styles (MarginL, MarginR, MarginV are typically the last numeric fields before encoding)
	content = rescaleStyleMargins(content, fx, fy, f)

	// 5. Rescale tags in Dialogue lines: \pos, \move, \org, \clip, \iclip, \fs, \fsp, \bord, \shad, \blur
	content = rescaleDialogueTags(content, fx, fy, f)

	// write out
	return os.WriteFile(outputPath, []byte(content), 0644)
}

func detectPlayRes(content string) (int, int) {
	reX := regexp.MustCompile(`(?m)^PlayResX\s*:\s*([0-9]+)`)
	reY := regexp.MustCompile(`(?m)^PlayResY\s*:\s*([0-9]+)`)
	x := 0
	y := 0
	if m := reX.FindStringSubmatch(content); len(m) > 1 {
		x, _ = strconv.Atoi(m[1])
	}
	if m := reY.FindStringSubmatch(content); len(m) > 1 {
		y, _ = strconv.Atoi(m[1])
	}
	return x, y
}

func updateOrInsertPlayRes(content string, x, y int) string {
	reX := regexp.MustCompile(`(?m)^PlayResX\s*:\s*[0-9]+`)
	reY := regexp.MustCompile(`(?m)^PlayResY\s*:\s*[0-9]+`)
	if reX.MatchString(content) {
		content = reX.ReplaceAllString(content, fmt.Sprintf("PlayResX: %d", x))
	} else {
		content = strings.Replace(content, "[V4+ Styles]", fmt.Sprintf("PlayResX: %d\nPlayResY: %d\n\n[V4+ Styles]", x, y), 1)
	}
	if reY.MatchString(content) {
		content = reY.ReplaceAllString(content, fmt.Sprintf("PlayResY: %d", y))
	}
	return content
}

func insertResampleComment(content string) string {
	re := regexp.MustCompile(`(?m)^\[V4\+ Styles\]`)
	return re.ReplaceAllString(content, "; Resampled to 1920x1080 and normalized by Limesub v3\n\n[V4+ Styles]")
}

func rescaleStyleMargins(content string, fx, fy, f float64) string {
	// For each Style: line format has many fields. We will parse margins by splitting.
	scanner := bufio.NewScanner(strings.NewReader(content))
	var out []string
	reStyle := regexp.MustCompile(`(?m)^Style:\s*(.*)$`)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "Style:") {
			// split by commas
			parts := splitCSVPreserve(line[len("Style:"):])
			// ensure length
			if len(parts) >= 22 {
				// fields near end: Outline index ~ 16, Shadow ~17, Alignment ~?, MarginL ~? but depends on Format ordering.
				// Simpler approach: assume format ordering as in spec and positions:
				// Format: Name, Fontname, Fontsize, PrimaryColour, SecondaryColour, OutlineColour, BackColour, Bold, Italic, Underline, StrikeOut, ScaleX, ScaleY, Spacing, Angle, BorderStyle, Outline, Shadow, Alignment, MarginL, MarginR, MarginV, Encoding
				// So index: 0..21
				// Outline at 16 (0-based), Shadow at 17, MarginL at 19, MarginR at 20, MarginV at 21
				// fontsize at 2
				// cautious parsing:
				for i := range parts {
					parts[i] = strings.TrimSpace(parts[i])
				}
				// fontsize
				if fs, err := strconv.ParseFloat(parts[2], 64); err == nil {
					parts[2] = fmt.Sprintf("%.0f", fs*f)
				}
				// Outline
				if len(parts) > 16 {
					if v, err := strconv.ParseFloat(parts[16], 64); err == nil {
						parts[16] = fmt.Sprintf("%g", v*f)
					}
				}
				// Shadow
				if len(parts) > 17 {
					if v, err := strconv.ParseFloat(parts[17], 64); err == nil {
						parts[17] = fmt.Sprintf("%g", v*f)
					}
				}
				// Margins
				if len(parts) > 19 {
					if v, err := strconv.ParseFloat(parts[19], 64); err == nil {
						parts[19] = fmt.Sprintf("%.0f", v*fx)
					}
				}
				if len(parts) > 20 {
					if v, err := strconv.ParseFloat(parts[20], 64); err == nil {
						parts[20] = fmt.Sprintf("%.0f", v*fx)
					}
				}
				if len(parts) > 21 {
					if v, err := strconv.ParseFloat(parts[21], 64); err == nil {
						parts[21] = fmt.Sprintf("%.0f", v*fy)
					}
				}
				// reconstruct
				line = "Style: " + strings.Join(parts, ",")
			}
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

// naive CSV split on commas but respecting no quotes
func splitCSVPreserve(s string) []string {
	parts := strings.Split(s, ",")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return parts
}

func rescaleDialogueTags(content string, fx, fy, f float64) string {
	// patterns to handle: \pos(x,y), \move(x1,y1,x2,y2), \org(x,y), \clip(x1,y1,x2,y2) (simple numeric)
	// \fsN, \fspN, \bordN, \shadN, \blurN
	rePos := regexp.MustCompile(`(?i)\\pos\(\s*([0-9.+-]+)\s*,\s*([0-9.+-]+)\s*\)`)
	content = rePos.ReplaceAllStringFunc(content, func(s string) string {
		m := rePos.FindStringSubmatch(s)
		if len(m) < 3 {
			return s
		}
		x, _ := strconv.ParseFloat(m[1], 64)
		y, _ := strconv.ParseFloat(m[2], 64)
		return fmt.Sprintf("\\pos(%.2f,%.2f)", x*fx, y*fy)
	})
	reMove := regexp.MustCompile(`(?i)\\move\(\s*([0-9.+-]+)\s*,\s*([0-9.+-]+)\s*,\s*([0-9.+-]+)\s*,\s*([0-9.+-]+)(,.*?)?\)`)
	content = reMove.ReplaceAllStringFunc(content, func(s string) string {
		m := reMove.FindStringSubmatch(s)
		if len(m) < 5 {
			return s
		}
		x1, _ := strconv.ParseFloat(m[1], 64)
		y1, _ := strconv.ParseFloat(m[2], 64)
		x2, _ := strconv.ParseFloat(m[3], 64)
		y2, _ := strconv.ParseFloat(m[4], 64)
		rest := ""
		if len(m) > 5 {
			rest = m[5]
		}
		return fmt.Sprintf("\\move(%.2f,%.2f,%.2f,%.2f%s)", x1*fx, y1*fy, x2*fx, y2*fy, rest)
	})
	reOrg := regexp.MustCompile(`(?i)\\org\(\s*([0-9.+-]+)\s*,\s*([0-9.+-]+)\s*\)`)
	content = reOrg.ReplaceAllStringFunc(content, func(s string) string {
		m := reOrg.FindStringSubmatch(s)
		x, _ := strconv.ParseFloat(m[1], 64)
		y, _ := strconv.ParseFloat(m[2], 64)
		return fmt.Sprintf("\\org(%.2f,%.2f)", x*fx, y*fy)
	})
	// clip numeric simple
	reClip := regexp.MustCompile(`(?i)\\iclip\(\s*([0-9.+-]+)\s*,\s*([0-9.+-]+)\s*,\s*([0-9.+-]+)\s*,\s*([0-9.+-]+)\s*\)`)
	content = reClip.ReplaceAllStringFunc(content, func(s string) string {
		m := reClip.FindStringSubmatch(s)
		a, _ := strconv.ParseFloat(m[1], 64)
		b, _ := strconv.ParseFloat(m[2], 64)
		c, _ := strconv.ParseFloat(m[3], 64)
		d, _ := strconv.ParseFloat(m[4], 64)
		return fmt.Sprintf("\\iclip(%.2f,%.2f,%.2f,%.2f)", a*fx, b*fy, c*fx, d*fy)
	})
	// fs, fsp, bord, shad, blur
	reFs := regexp.MustCompile(`(?i)\\fs([0-9.]+)`)
	content = reFs.ReplaceAllStringFunc(content, func(s string) string {
		m := reFs.FindStringSubmatch(s)
		v, _ := strconv.ParseFloat(m[1], 64)
		return fmt.Sprintf("\\fs%.2f", v*f)
	})
	reFsp := regexp.MustCompile(`(?i)\\fsp([0-9.]+)`)
	content = reFsp.ReplaceAllStringFunc(content, func(s string) string {
		m := reFsp.FindStringSubmatch(s)
		v, _ := strconv.ParseFloat(m[1], 64)
		return fmt.Sprintf("\\fsp%.2f", v*f)
	})
	reBord := regexp.MustCompile(`(?i)\\bord([0-9.]+)`)
	content = reBord.ReplaceAllStringFunc(content, func(s string) string {
		m := reBord.FindStringSubmatch(s)
		v, _ := strconv.ParseFloat(m[1], 64)
		return fmt.Sprintf("\\bord%.2f", v*f)
	})
	reShad := regexp.MustCompile(`(?i)\\shad([0-9.]+)`)
	content = reShad.ReplaceAllStringFunc(content, func(s string) string {
		m := reShad.FindStringSubmatch(s)
		v, _ := strconv.ParseFloat(m[1], 64)
		return fmt.Sprintf("\\shad%.2f", v*f)
	})
	reBlur := regexp.MustCompile(`(?i)\\blur([0-9.]+)`)
	content = reBlur.ReplaceAllStringFunc(content, func(s string) string {
		m := reBlur.FindStringSubmatch(s)
		v, _ := strconv.ParseFloat(m[1], 64)
		return fmt.Sprintf("\\blur%.2f", v*f)
	})
	return content
}
