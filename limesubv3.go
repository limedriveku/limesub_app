package main

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io/fs"
	"io/ioutil"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unsafe"
	"golang.org/x/sys/windows"
)

// ====================== BASIC STRUCT ======================

type SRTBlock struct {
	Start time.Duration
	End   time.Duration
	Text  string
	Style string
}

// ====================== MESSAGEBOX (WINDOWS ONLY) ======================

var (
	user32           = windows.NewLazySystemDLL("user32.dll")
	procMessageBoxW  = user32.NewProc("MessageBoxW")
)

func MessageBox(title, text string) {
	titleUTF16, _ := windows.UTF16PtrFromString(title)
	textUTF16, _ := windows.UTF16PtrFromString(text)
	procMessageBoxW.Call(0, uintptr(unsafe.Pointer(textUTF16)), uintptr(unsafe.Pointer(titleUTF16)), 0)
}

// ====================== UTILITIES ======================

func stripFontTags(s string) string {
	re := regexp.MustCompile(`\\fn[^\\}]+|\\fs\d+`)
	return re.ReplaceAllString(s, "")
}

func cleanText(s string) string {
	s = strings.ReplaceAll(s, "\r", "")
	s = strings.TrimSpace(s)
	return s
}

func detectStyle(text string) string {
	t := strings.ToUpper(stripFontTags(text))
	t = strings.TrimSpace(t)
	if len(t) == 0 {
		return "Default"
	}
	noTag := regexp.MustCompile(`\{\\[^}]+\}`).ReplaceAllString(t, "")
	if noTag == strings.ToUpper(noTag) {
		return "tanda"
	}
	if (strings.HasPrefix(noTag, "(") && strings.HasSuffix(noTag, ")")) ||
		(strings.HasPrefix(noTag, "[") && strings.HasSuffix(noTag, "]")) {
		return "tanda"
	}
	return "Default"
}

// ====================== FILE DETECTION ======================

func detectFormat(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".srt":
		return "srt"
	case ".json":
		return "json"
	case ".xml":
		return "xml"
	case ".ttml":
		return "ttml"
	case ".ass":
		return "ass"
	default:
		return "unknown"
	}
}

// ====================== PARSERS ======================

func parseSRT(data string) []SRTBlock {
	re := regexp.MustCompile(`(?m)^\d+\s*\n(\d{2}:\d{2}:\d{2},\d{3}) --> (\d{2}:\d{2}:\d{2},\d{3})\s*\n(.*?)(?=\n\d+\n|\z)`)
	matches := re.FindAllStringSubmatch(data, -1)
	var out []SRTBlock
	for _, m := range matches {
		start, _ := parseTime(m[1])
		end, _ := parseTime(m[2])
		text := cleanText(m[3])
		out = append(out, SRTBlock{Start: start, End: end, Text: text})
	}
	return out
}

func parseTime(s string) (time.Duration, error) {
	parts := strings.Split(strings.ReplaceAll(s, ",", "."), ":")
	if len(parts) != 3 {
		return 0, fmt.Errorf("invalid time")
	}
	sec, _ := strconv.ParseFloat(parts[2], 64)
	min, _ := strconv.Atoi(parts[1])
	hour, _ := strconv.Atoi(parts[0])
	total := time.Duration(float64(time.Hour)*float64(hour) + float64(time.Minute)*float64(min) + float64(time.Second)*sec)
	return total, nil
}

func parseJSONtoSRT(data []byte) []SRTBlock {
	var entries []map[string]interface{}
	json.Unmarshal(data, &entries)
	var out []SRTBlock
	for _, e := range entries {
		start, _ := parseTime(fmt.Sprintf("%v", e["start"]))
		end, _ := parseTime(fmt.Sprintf("%v", e["end"]))
		out = append(out, SRTBlock{Start: start, End: end, Text: fmt.Sprintf("%v", e["text"])})
	}
	return out
}

func parseXMLtoSRT(data []byte) []SRTBlock {
	type Node struct {
		Start string `xml:"start,attr"`
		End   string `xml:"end,attr"`
		Text  string `xml:",chardata"`
	}
	var n struct {
		Body []Node `xml:"body>p"`
	}
	xml.Unmarshal(data, &n)
	var out []SRTBlock
	for _, p := range n.Body {
		start, _ := parseTime(strings.ReplaceAll(p.Start, ".", ","))
		end, _ := parseTime(strings.ReplaceAll(p.End, ".", ","))
		txt := strings.ReplaceAll(p.Text, "\n", " ")
		out = append(out, SRTBlock{Start: start, End: end, Text: txt})
	}
	return out
}

func parseTTMLtoSRT(data []byte) []SRTBlock {
	type Node struct {
		Begin string `xml:"begin,attr"`
		End   string `xml:"end,attr"`
		Text  string `xml:",innerxml"`
	}
	var n struct {
		Body []Node `xml:"body>div>p"`
	}
	xml.Unmarshal(data, &n)
	var out []SRTBlock
	for _, p := range n.Body {
		start, _ := parseTime(strings.ReplaceAll(p.Begin, ".", ","))
		end, _ := parseTime(strings.ReplaceAll(p.End, ".", ","))
		txt := strings.ReplaceAll(p.Text, "<br/>", "\n")
		txt = strings.ReplaceAll(txt, "<br />", "\n")
		out = append(out, SRTBlock{Start: start, End: end, Text: cleanText(txt)})
	}
	return out
}

// ====================== MERGE LOGIC ======================

func mergeSameOrContinuous(blocks []SRTBlock) []SRTBlock {
	sort.Slice(blocks, func(i, j int) bool { return blocks[i].Start < blocks[j].Start })
	var out []SRTBlock
	for _, b := range blocks {
		if len(out) == 0 {
			out = append(out, b)
			continue
		}
		last := &out[len(out)-1]
		if last.Style == b.Style && cleanText(last.Text) == cleanText(b.Text) {
			gap := b.Start - last.End
			if gap < 200*time.Millisecond {
				last.End = b.End
				continue
			}
		}
		out = append(out, b)
	}
	return out
}

func mergeSameTimeAndStyle(blocks []SRTBlock) []SRTBlock {
	var out []SRTBlock
	for _, b := range blocks {
		merged := false
		for i := range out {
			if out[i].Start == b.Start && out[i].End == b.End && out[i].Style == b.Style && out[i].Text != b.Text {
				out[i].Text = out[i].Text + "\\N" + b.Text
				merged = true
				break
			}
		}
		if !merged {
			out = append(out, b)
		}
	}
	return out
}

// ====================== ASS GENERATOR ======================

func generateASS(blocks []SRTBlock) string {
	header := `[Script Info]
; Script generated by Limesub v2
; https://t.me/s/limenime
; https://www.facebook.com/limenime.official
; https://discord.gg/7XS7MCvVwh
; https://x.com/limenime
Title: Default Limenime Subtitle File
ScriptType: v4.00+
WrapStyle: 0
ScaledBorderAndShadow: yes
YCbCr Matrix: None
PlayResX: 1920
PlayResY: 1080
Timer: 100.0000

[V4+ Styles]
Format: Name, Fontname, Fontsize, PrimaryColour, SecondaryColour, OutlineColour, BackColour, Bold, Italic, Underline, StrikeOut, ScaleX, ScaleY, Spacing, Angle, BorderStyle, Outline, Shadow, Alignment, MarginL, MarginR, MarginV, Encoding
Style: Default,Basic Comical NC,70,&H00FFFFFF,&H00FFFFFF,&H00000000,&H80000000,0,0,0,0,100,100,0,0,1,1.5,1,2,64,64,33,1
Style: tanda,Basic Comical NC,75,&H00FFFFFF,&H000000FF,&H00000000,&H00000000,-1,0,0,0,100,100,0,0,1,1,0,8,0,0,0,1

[Events]
Format: Layer, Start, End, Style, Name, MarginL, MarginR, MarginV, Effect, Text
`
	var buf strings.Builder
	buf.WriteString(header)
	for _, b := range blocks {
		start := formatTimeASS(b.Start)
		end := formatTimeASS(b.End)
		text := stripFontTags(b.Text)
		if b.Style != "tanda" {
			text = "{\\blur3}{\\fad(00,40)}" + text
		}
		buf.WriteString(fmt.Sprintf("Dialogue: 0,%s,%s,%s,,0,0,0,,%s\n", start, end, b.Style, text))
	}
	return buf.String()
}

func formatTimeASS(t time.Duration) string {
	h := int(t.Hours())
	m := int(t.Minutes()) % 60
	s := int(t.Seconds()) % 60
	cs := int(t.Milliseconds()/10) % 100
	return fmt.Sprintf("%d:%02d:%02d.%02d", h, m, s, cs)
}

// ====================== OUTPUT HANDLER ======================

func nextOutputPath(input string) string {
	dir := filepath.Dir(input)
	base := strings.TrimSuffix(filepath.Base(input), filepath.Ext(input))
	out := filepath.Join(dir, base+"_Limenime.ass")
	if _, err := os.Stat(out); err == nil {
		for i := 1; ; i++ {
			candidate := filepath.Join(dir, fmt.Sprintf("%s_Limenime(%d).ass", base, i))
			if _, err := os.Stat(candidate); err != nil {
				return candidate
			}
		}
	}
	return out
}

// ====================== MAIN ======================

func main() {
	if len(os.Args) < 2 {
		MessageBox("Limesub v3", "Tidak ada file yang diberikan.\nGunakan drag & drop file subtitle ke aplikasi ini,\natau jalankan melalui Command Prompt.")
		return
	}

	inputPath := os.Args[1]
	format := detectFormat(inputPath)
	data, err := ioutil.ReadFile(inputPath)
	if err != nil {
		MessageBox("Limesub v3", "Gagal membaca file input.")
		return
	}

	var blocks []SRTBlock
	switch format {
	case "srt":
		blocks = parseSRT(string(data))
	case "json":
		blocks = parseJSONtoSRT(data)
	case "xml":
		blocks = parseXMLtoSRT(data)
	case "ttml":
		blocks = parseTTMLtoSRT(data)
	case "ass":
		// Placeholder: normalization/resample bisa ditambahkan di sini
		MessageBox("Limesub v3", "File ASS akan dinormalisasi ke 1080p (fitur ini segera hadir).")
		return
	default:
		MessageBox("Limesub v3", "Format file tidak dikenali.\nAplikasi ini hanya mendukung SRT, JSON, XML, dan TTML.")
		return
	}

	// Style detection
	for i := range blocks {
		blocks[i].Style = detectStyle(blocks[i].Text)
	}

	// Merge dan efek
	blocks = mergeSameOrContinuous(blocks)
	blocks = mergeSameTimeAndStyle(blocks)

	outPath := nextOutputPath(inputPath)
	ioutil.WriteFile(outPath, []byte(generateASS(blocks)), fs.ModePerm)

	fmt.Println("✅ Berhasil mengonversi:", filepath.Base(inputPath), "→", filepath.Base(outPath))
}
