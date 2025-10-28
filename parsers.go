package main

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// SRTBlock is the in-memory SRT representation
type SRTBlock struct {
	Index   int
	StartMs int64
	EndMs   int64
	Text    string // may contain '\n'
}

// ConvertAnyToSRT parses input file and returns []SRTBlock in memory.
func ConvertAnyToSRT(path string) ([]SRTBlock, error) {
	ext := strings.ToLower(filepathExt(path))
	switch ext {
	case ".srt":
		return parseSRTFile(path)
	case ".json":
		return parseJSONToSRT(path)
	case ".xml":
		return parseXMLToSRT(path)
	case ".ttml":
		return parseTTMLToSRT(path)
	default:
		return nil, fmt.Errorf("unsupported input format: %s", ext)
	}
}

// helper to get extension
func filepathExt(p string) string {
	return strings.ToLower(filepath.Ext(p))
}

// --- SRT parser (simple)
func parseSRTFile(path string) ([]SRTBlock, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	text := string(data)
	blocks := parseSRTString(text)
	return blocks, nil
}

func parseSRTString(content string) []SRTBlock {
	content = strings.ReplaceAll(content, "\r\n", "\n")
	content = strings.ReplaceAll(content, "\r", "\n")
	parts := regexp.MustCompile(`\n{2,}`).Split(strings.TrimSpace(content), -1)
	var out []SRTBlock
	idx := 1
	for _, p := range parts {
		lines := strings.Split(p, "\n")
		if len(lines) < 2 {
			continue
		}
		// find timeline
		var timeLine string
		for i := 0; i < len(lines); i++ {
			if strings.Contains(lines[i], "-->") {
				timeLine = lines[i]
				textLines := lines[i+1:]
				startMs, endMs := parseSRTTimeLine(timeLine)
				text := strings.Join(textLines, "\n")
				out = append(out, SRTBlock{
					Index:   idx,
					StartMs: startMs,
					EndMs:   endMs,
					Text:    text,
				})
				idx++
				break
			}
		}
	}
	return out
}

func parseSRTTimeLine(line string) (int64, int64) {
	parts := strings.Split(line, "-->")
	if len(parts) < 2 {
		return 0, 0
	}
	start := strings.TrimSpace(parts[0])
	end := strings.TrimSpace(parts[1])
	sMs := parseTimeStringToMs(start)
	eMs := parseTimeStringToMs(end)
	return sMs, eMs
}

// --- JSON Parser (expected events[] with tStartMs, dDurationMs, segs[].utf8)
func parseJSONToSRT(path string) ([]SRTBlock, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	// Try to decode into generic
	var root map[string]interface{}
	if err := json.Unmarshal(data, &root); err != nil {
		return nil, err
	}
	eventsRaw, ok := root["events"]
	if !ok {
		// try "body" or top-level array
		var arr []interface{}
		if err := json.Unmarshal(data, &arr); err == nil {
			return jsonEventsToSRT(arr)
		}
		return nil, fmt.Errorf("no events array found in JSON")
	}
	events, ok := eventsRaw.([]interface{})
	if !ok {
		return nil, fmt.Errorf("events is not array")
	}
	return jsonEventsToSRT(events)
}

func jsonEventsToSRT(events []interface{}) ([]SRTBlock, error) {
	var out []SRTBlock
	for i, e := range events {
		ev, ok := e.(map[string]interface{})
		if !ok {
			continue
		}
		var startMs int64
		var durMs int64
		// tStartMs
		if v, exists := ev["tStartMs"]; exists {
			startMs = asInt64(v)
		} else if v, exists := ev["start"]; exists {
			startMs = asInt64(v)
		}
		if v, ok := ev["dDurationMs"]; ok {
			durMs = asInt64(v)
		} else if v, ok := ev["duration"]; ok {
			durMs = asInt64(v)
		}
		if durMs == 0 {
			// fallback small duration
			durMs = 2000
		}
		endMs := startMs + durMs
		// build text
		var text string
		if segs, ok := ev["segs"].([]interface{}); ok {
			var sb strings.Builder
			for _, s := range segs {
				if m, ok := s.(map[string]interface{}); ok {
					if ut, ok := m["utf8"]; ok {
						sb.WriteString(fmt.Sprintf("%v", ut))
					} else if txt, ok := m["text"]; ok {
						sb.WriteString(fmt.Sprintf("%v", txt))
					}
				} else {
					sb.WriteString(fmt.Sprintf("%v", s))
				}
			}
			text = sb.String()
		} else if v, ok := ev["text"]; ok {
			text = fmt.Sprintf("%v", v)
		}
		out = append(out, SRTBlock{Index: i + 1, StartMs: startMs, EndMs: endMs, Text: strings.TrimSpace(text)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].StartMs < out[j].StartMs })
	return out, nil
}

func asInt64(v interface{}) int64 {
	switch t := v.(type) {
	case float64:
		return int64(t)
	case int64:
		return t
	case int:
		return int64(t)
	case string:
		i, _ := strconv.ParseInt(strings.TrimSpace(t), 10, 64)
		return i
	default:
		return 0
	}
}

// --- XML parser (generic) - will find nodes like <dia> or other <entry>
func parseXMLToSRT(path string) ([]SRTBlock, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	decoder := xml.NewDecoder(bytes.NewReader(data))
	decoder.Strict = false
	// naive approach: collect <dia> blocks or elements with <st> <et> <sub>
	type entry struct {
		Start int64
		End   int64
		Text  string
	}
	var entries []entry
	var inElement string
	for {
		tok, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			// fallback: try simpler parsing below
			break
		}
		switch se := tok.(type) {
		case xml.StartElement:
			inElement = strings.ToLower(se.Name.Local)
			if inElement == "dia" || inElement == "entry" || inElement == "p" {
				// decode into generic map
				var inner struct {
					XMLName xml.Name `xml:"-"`
					St      string   `xml:"st"`
					Et      string   `xml:"et"`
					Sub     string   `xml:"sub"`
				}
				_ = decoder.DecodeElement(&inner, &se)
				if inner.Sub != "" {
					startMs := parseTimeStringToMs(inner.St)
					endMs := parseTimeStringToMs(inner.Et)
					if endMs == 0 {
						endMs = startMs + 2000
					}
					entries = append(entries, entry{Start: startMs, End: endMs, Text: inner.Sub})
				}
			}
		default:
		}
	}
	// if entries empty, attempt regex fallback: look for <st>nnn</st>, <et>nnn</et>, <sub><![CDATA[...]]>
	if len(entries) == 0 {
		// naive fallback
		re := regexp.MustCompile(`(?s)<dia>.*?</dia>`)
		all := re.FindAll(data, -1)
		for _, block := range all {
			st := regexp.MustCompile(`(?s)<st>(.*?)</st>`).FindSubmatch(block)
			et := regexp.MustCompile(`(?s)<et>(.*?)</et>`).FindSubmatch(block)
			sub := regexp.MustCompile(`(?s)<sub><!\[CDATA\[(.*?)\]\]></sub>`).FindSubmatch(block)
			if len(sub) > 1 {
				start := int64(0)
				end := int64(0)
				if len(st) > 1 {
					start = asInt64(stringToInt64(trimString(st[1])))
				}
				if len(et) > 1 {
					end = asInt64(stringToInt64(trimString(et[1])))
				}
				if end == 0 {
					end = start + 2000
				}
				entries = append(entries, entry{Start: start, End: end, Text: string(sub[1])})
			}
		}
	}
	// convert entries -> SRTBlock
	var out []SRTBlock
	for i, e := range entries {
		out = append(out, SRTBlock{Index: i + 1, StartMs: e.Start, EndMs: e.End, Text: safeTrimAndNormalizeSpaces(e.Text)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].StartMs < out[j].StartMs })
	return out, nil
}

func trimString(b []byte) string {
	return strings.TrimSpace(string(b))
}

func stringToInt64(s string) int64 {
	i, _ := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	return i
}

// --- TTML parser (careful with <br> variants)
func parseTTMLToSRT(path string) ([]SRTBlock, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	// Normalize br tags BEFORE XML parse
	text := string(data)
	text = normalizeBrTags(text)
	decoder := xml.NewDecoder(strings.NewReader(text))
	decoder.Strict = false
	var blocks []SRTBlock
	idx := 1
	for {
		tok, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		switch se := tok.(type) {
		case xml.StartElement:
			if strings.ToLower(se.Name.Local) == "p" {
				var p struct {
					Begin string `xml:"begin,attr"`
					End   string `xml:"end,attr"`
					Dur   string `xml:"dur,attr"`
					Inner string `xml:",innerxml"`
				}
				if err := decoder.DecodeElement(&p, &se); err == nil {
					start := parseTimeStringToMs(p.Begin)
					end := parseTimeStringToMs(p.End)
					if end == 0 {
						if p.Dur != "" {
							dur := parseTimeStringToMs(p.Dur)
							end = start + dur
						} else {
							end = start + 2000
						}
					}
					txt := stripTagsButPreserveNewlines(p.Inner)
					txt = safeTrimAndNormalizeSpaces(txt)
					blocks = append(blocks, SRTBlock{Index: idx, StartMs: start, EndMs: end, Text: txt})
					idx++
				}
			}
		default:
		}
	}
	sort.Slice(blocks, func(i, j int) bool { return blocks[i].StartMs < blocks[j].StartMs })
	return blocks, nil
}

// helper: replace <br /> variants with \n (case-insensitive)
func normalizeBrTags(s string) string {
	re := regexp.MustCompile(`(?i)<br\s*/?>`)
	out := re.ReplaceAllString(s, "\n")
	out = strings.ReplaceAll(out, "\r\n", "\n")
	out = strings.ReplaceAll(out, "\r", "\n")
	return out
}

// strip tags but keep text and already inserted newlines
func stripTagsButPreserveNewlines(s string) string {
	// Remove xml tags but leave inner text; we already normalized <br> -> \n
	// Remove any remaining tags like <span ...> </span>
	re := regexp.MustCompile(`(?s)<[^>]+>`)
	out := re.ReplaceAllString(s, "")
	return out
}
