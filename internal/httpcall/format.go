package httpcall

import (
	"bytes"
	"encoding/json"
	"fmt"
	"mime"
	"strings"
)

// The response formats:
//   - "auto": from the Content-Type, else from the content (JSON, XML, text)
//   - "json", "xml", "text": forced (a response that doesn't parse is an error)
//   - "": the format of the flows saved before the formats existed (JSON when
//     the content is JSON, else text)
const (
	FormatAuto = "auto"
	FormatJSON = "json"
	FormatXML  = "xml"
	FormatText = "text"
)

// Formats are the formats a step or a call can choose.
var Formats = []string{FormatAuto, FormatJSON, FormatXML, FormatText}

func normalizeFormat(f string) (string, error) {
	f = strings.ToLower(strings.TrimSpace(f))
	switch f {
	case "", FormatAuto, FormatJSON, FormatXML, FormatText:
		return f, nil
	}
	return "", fmt.Errorf("unknown response format %q (auto, json, xml or text)", f)
}

// ValidFormat reports whether f is a response format ("" included).
func ValidFormat(f string) bool {
	_, err := normalizeFormat(f)
	return err == nil
}

// Parse reads a response body in a format and returns the format used and the
// value: JSON data, XML as a map (see XMLToMap), or the text.
func Parse(raw []byte, contentType, format string) (string, any, error) {
	format, err := normalizeFormat(format)
	if err != nil {
		return "", nil, err
	}
	switch format {
	case "":
		if len(raw) > 0 && json.Valid(raw) {
			var v any
			if json.Unmarshal(raw, &v) == nil {
				return FormatJSON, v, nil
			}
		}
		return FormatText, string(raw), nil
	case FormatText:
		return FormatText, string(raw), nil
	case FormatJSON:
		if len(bytes.TrimSpace(raw)) == 0 {
			return FormatJSON, nil, nil // e.g. 204 No Content
		}
		var v any
		if err := json.Unmarshal(raw, &v); err != nil {
			return FormatText, string(raw), fmt.Errorf("the response isn't valid JSON: %w", err)
		}
		return FormatJSON, v, nil
	case FormatXML:
		if len(bytes.TrimSpace(raw)) == 0 {
			return FormatXML, nil, nil
		}
		v, err := XMLToMap(raw)
		if err != nil {
			return FormatText, string(raw), fmt.Errorf("the response isn't valid XML: %w", err)
		}
		return FormatXML, v, nil
	}

	// auto: the declared type first, then the content
	mt, _, _ := mime.ParseMediaType(contentType)
	switch {
	case mt == "application/json" || strings.HasSuffix(mt, "+json"):
		if f, v, err := Parse(raw, "", FormatJSON); err == nil {
			return f, v, nil
		}
	case mt == "application/xml" || mt == "text/xml" || strings.HasSuffix(mt, "+xml"):
		if f, v, err := Parse(raw, "", FormatXML); err == nil {
			return f, v, nil
		}
	}
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) > 0 && json.Valid(trimmed) {
		return Parse(raw, "", FormatJSON)
	}
	if len(trimmed) > 0 && trimmed[0] == '<' && mt != "text/html" {
		if v, err := XMLToMap(raw); err == nil {
			return FormatXML, v, nil
		}
	}
	return FormatText, string(raw), nil
}
