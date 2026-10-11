package httpcall

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"strings"

	"golang.org/x/net/html/charset"
)

const (
	maxXMLDepth    = 64
	maxXMLElements = 100_000
)

// XMLToMap converts an XML document to plain data, so the flows read its
// values with paths like JSON ones ("rss.channel.item.0.title"):
//
//   - the document is a map with the root element: {"rss": {...}}
//   - an attribute is "@name": {"@version": "2.0"}
//   - an element with only text is a string: {"title": "News"}
//   - the text of an element with attributes or children is "#text"
//   - repeated elements are a list: {"item": [{...}, {...}]} (a single one
//     is not: use the "list" template function to always get a list)
//   - namespace prefixes are dropped (local names), CDATA is text
//
// External entities are never loaded.
func XMLToMap(raw []byte) (map[string]any, error) {
	d := xml.NewDecoder(bytes.NewReader(raw))
	d.CharsetReader = charset.NewReaderLabel
	d.Entity = xml.HTMLEntity // &nbsp; and the like, often found in feeds

	type frame struct {
		name  string
		value map[string]any
		text  strings.Builder
	}
	var stack []*frame
	var root map[string]any
	elements := 0
	for {
		tok, err := d.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if root != nil {
				return nil, errors.New("more than one root element")
			}
			elements++
			if elements > maxXMLElements {
				return nil, fmt.Errorf("more than %d elements", maxXMLElements)
			}
			if len(stack) >= maxXMLDepth {
				return nil, fmt.Errorf("more than %d levels", maxXMLDepth)
			}
			f := &frame{name: t.Name.Local, value: map[string]any{}}
			for _, a := range t.Attr {
				if a.Name.Space == "xmlns" || (a.Name.Space == "" && a.Name.Local == "xmlns") {
					continue // namespace declarations
				}
				f.value["@"+a.Name.Local] = a.Value
			}
			stack = append(stack, f)
		case xml.CharData:
			if len(stack) > 0 {
				stack[len(stack)-1].text.Write(t)
			}
		case xml.EndElement:
			f := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			text := strings.TrimSpace(f.text.String())
			var v any
			if len(f.value) == 0 {
				v = text // only text (or empty)
			} else {
				if text != "" {
					f.value["#text"] = text
				}
				v = f.value
			}
			if len(stack) == 0 {
				root = map[string]any{f.name: v}
				continue
			}
			parent := stack[len(stack)-1].value
			switch prev := parent[f.name].(type) {
			case nil:
				parent[f.name] = v
			case []any: // element values are never lists: a repeated element
				parent[f.name] = append(prev, v)
			default:
				parent[f.name] = []any{prev, v}
			}
		}
	}
	if root == nil {
		return nil, errors.New("no root element")
	}
	return root, nil
}
