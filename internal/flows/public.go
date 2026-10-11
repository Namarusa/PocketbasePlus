package flows

// The template helpers of the flows, for the modules adding steps (e.g. the
// saved API calls of internal/apisources): the same syntax and functions as
// the step settings, with the values escaped for their place.

// RenderString renders a template (a string with {{...}}) with the data.
func RenderString(src string, data map[string]any) (string, error) {
	return resolveString(src, data)
}

// RenderURL renders a URL template: values are escaped as path segments, or
// as query values after the "?" (a value at the start is the base URL).
func RenderURL(src string, data map[string]any) (string, error) {
	return renderURL(src, data)
}

// RenderJSONBody renders a JSON template: values inside strings are escaped,
// values in place of a JSON value are encoded as JSON.
func RenderJSONBody(src string, data map[string]any) (string, error) {
	return renderJSONBody(src, data)
}

// RenderXMLBody renders an XML template: values are escaped as XML text.
func RenderXMLBody(src string, data map[string]any) (string, error) {
	return renderXMLBody(src, data)
}

// ValidateTemplate checks that a template parses.
func ValidateTemplate(src string) error {
	return validateTemplates(src)
}

// LookupPath returns the value at a dotted path ("items.0.title") of plain
// data (maps and lists), or nil.
func LookupPath(data any, path string) any {
	return lookupPath(data, path)
}

// ToPlain converts a value to plain JSON data (maps, lists, strings, numbers, bools).
func ToPlain(v any) any {
	return toPlain(v)
}
