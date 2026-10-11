package records

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"

	validation "github.com/pocketbase/ozzo-validation/v4"
	"github.com/pocketbase/pocketbase/core"
	"github.com/xuri/excelize/v2"
)

const (
	maxImportBytes = 20 << 20
	maxImportRows  = 10000
	maxRowErrors   = 100
)

// table is a parsed import file: the columns and the rows (column -> value).
type table struct {
	Columns []string
	Rows    []map[string]any
	// first data row number in the file (for error messages)
	FirstRow int
}

func readUpload(e *core.RequestEvent) (*multipart.FileHeader, error) {
	if err := e.Request.ParseMultipartForm(maxImportBytes); err != nil {
		return nil, err
	}
	files := e.Request.MultipartForm.File["file"]
	if len(files) == 0 {
		return nil, errors.New("missing file")
	}
	return files[0], nil
}

// parseTable reads a CSV, JSON or XLSX file.
func parseTable(fh *multipart.FileHeader) (*table, error) {
	f, err := fh.Open()
	if err != nil {
		return nil, err
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, maxImportBytes+1))
	if err != nil {
		return nil, err
	}
	if len(raw) > maxImportBytes {
		return nil, fmt.Errorf("the file is too big (max %d MB)", maxImportBytes>>20)
	}

	var t *table
	switch strings.ToLower(filepath.Ext(fh.Filename)) {
	case ".csv", ".txt", ".tsv":
		t, err = parseCSV(raw)
	case ".json":
		t, err = parseJSON(raw)
	case ".xlsx":
		t, err = parseXLSX(raw)
	default:
		return nil, errors.New("unsupported file (CSV, JSON or XLSX)")
	}
	if err != nil {
		return nil, err
	}
	if len(t.Rows) > maxImportRows {
		return nil, fmt.Errorf("too many rows (max %d)", maxImportRows)
	}
	return t, nil
}

func fromRows(rows [][]string) *table {
	t := &table{FirstRow: 2}
	if len(rows) == 0 {
		return t
	}
	for i, h := range rows[0] {
		h = strings.TrimSpace(h)
		if h == "" {
			h = fmt.Sprintf("column_%d", i+1)
		}
		t.Columns = append(t.Columns, h)
	}
	for _, r := range rows[1:] {
		row := map[string]any{}
		empty := true
		for i, col := range t.Columns {
			if i < len(r) {
				row[col] = r[i]
				if strings.TrimSpace(r[i]) != "" {
					empty = false
				}
			}
		}
		if !empty {
			t.Rows = append(t.Rows, row)
		}
	}
	return t
}

func parseCSV(raw []byte) (*table, error) {
	raw = bytes.TrimPrefix(raw, []byte("\ufeff"))
	// the delimiter used most in the header line (, ; or tab)
	firstLine, _, _ := bytes.Cut(raw, []byte("\n"))
	delim := ','
	best := bytes.Count(firstLine, []byte(","))
	for _, d := range []rune{';', '\t'} {
		if n := bytes.Count(firstLine, []byte(string(d))); n > best {
			delim, best = d, n
		}
	}
	r := csv.NewReader(bytes.NewReader(raw))
	r.Comma = delim
	r.FieldsPerRecord = -1
	r.LazyQuotes = true
	rows, err := r.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("invalid CSV: %w", err)
	}
	return fromRows(rows), nil
}

func parseXLSX(raw []byte) (*table, error) {
	f, err := excelize.OpenReader(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("invalid XLSX: %w", err)
	}
	defer f.Close()
	sheets := f.GetSheetList()
	if len(sheets) == 0 {
		return &table{}, nil
	}
	rows, err := f.GetRows(sheets[0])
	if err != nil {
		return nil, err
	}
	return fromRows(rows), nil
}

func parseJSON(raw []byte) (*table, error) {
	var items []map[string]any
	if err := json.Unmarshal(raw, &items); err != nil {
		// also accept {"items": [...]} (the records API list format)
		var wrapped struct {
			Items []map[string]any `json:"items"`
		}
		if err2 := json.Unmarshal(raw, &wrapped); err2 != nil || wrapped.Items == nil {
			return nil, fmt.Errorf("invalid JSON (expected an array of objects): %w", err)
		}
		items = wrapped.Items
	}
	t := &table{Rows: items, FirstRow: 1}
	seen := map[string]bool{}
	for _, item := range items {
		for k := range item {
			if !seen[k] {
				seen[k] = true
				t.Columns = append(t.Columns, k)
			}
		}
	}
	return t, nil
}

// suggestMapping maps the columns named like a field (case insensitive).
func suggestMapping(c *core.Collection, columns []string) map[string]string {
	mapping := map[string]string{}
	for _, col := range columns {
		for _, f := range c.Fields {
			if strings.EqualFold(strings.TrimSpace(col), f.GetName()) && importable(f) {
				mapping[col] = f.GetName()
			}
		}
	}
	return mapping
}

func importable(f core.Field) bool {
	switch f.Type() {
	case core.FieldTypeAutodate, core.FieldTypeFile:
		return false
	}
	return f.GetName() != core.FieldNameTokenKey
}

// importValue converts a cell to the field value (multiple values separated
// by ";", JSON for json fields, yes/no for booleans...).
func importValue(f core.Field, v any) (any, error) {
	s, isString := v.(string)
	if !isString {
		return v, nil // JSON imports keep their types
	}
	s = strings.TrimSpace(s)
	switch field := f.(type) {
	case *core.BoolField:
		switch strings.ToLower(s) {
		case "true", "1", "yes", "y", "x", "on", "oui", "vrai":
			return true, nil
		case "", "false", "0", "no", "n", "off", "non", "faux":
			return false, nil
		}
		return nil, fmt.Errorf("invalid boolean %q", s)
	case *core.NumberField:
		if s == "" {
			return nil, nil
		}
		n, err := strconv.ParseFloat(strings.ReplaceAll(s, ",", "."), 64)
		if err != nil {
			return nil, fmt.Errorf("invalid number %q", s)
		}
		return n, nil
	case *core.JSONField:
		if s == "" {
			return nil, nil
		}
		var parsed any
		if err := json.Unmarshal([]byte(s), &parsed); err != nil {
			return s, nil // plain text value
		}
		return parsed, nil
	case *core.GeoPointField:
		if s == "" {
			return nil, nil
		}
		var parsed map[string]any
		if json.Unmarshal([]byte(s), &parsed) == nil {
			return parsed, nil
		}
		// "lat, lon"
		parts := strings.Split(s, ",")
		if len(parts) == 2 {
			lat, err1 := strconv.ParseFloat(strings.TrimSpace(parts[0]), 64)
			lon, err2 := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
			if err1 == nil && err2 == nil {
				return map[string]any{"lat": lat, "lon": lon}, nil
			}
		}
		return nil, fmt.Errorf("invalid geo point %q (use \"lat, lon\")", s)
	case *core.SelectField:
		if field.IsMultiple() {
			return splitList(s), nil
		}
	case *core.RelationField:
		if field.IsMultiple() {
			return splitList(s), nil
		}
	}
	return s, nil
}

func splitList(s string) []string {
	list := []string{}
	for _, part := range strings.FieldsFunc(s, func(r rune) bool { return r == ';' || r == '|' }) {
		if p := strings.TrimSpace(part); p != "" {
			list = append(list, p)
		}
	}
	return list
}

// --- routes -------------------------------------------------------------------------

func importPreview(e *core.RequestEvent) error {
	c, err := findCollection(e)
	if err != nil {
		return err
	}
	fh, err := readUpload(e)
	if err != nil {
		return e.BadRequestError("Upload a CSV, JSON or XLSX file.", err)
	}
	t, err := parseTable(fh)
	if err != nil {
		return e.BadRequestError(err.Error(), nil)
	}
	sample := t.Rows
	if len(sample) > 5 {
		sample = sample[:5]
	}
	return e.JSON(http.StatusOK, map[string]any{
		"columns": t.Columns,
		"sample":  sample,
		"total":   len(t.Rows),
		"mapping": suggestMapping(c, t.Columns),
	})
}

type importOptions struct {
	Mapping    map[string]string `json:"mapping"`    // column -> field
	Mode       string            `json:"mode"`       // create | upsert
	MatchField string            `json:"matchField"` // upsert: id or a field
	DryRun     bool              `json:"dryRun"`
}

type rowError struct {
	Row     int               `json:"row"`
	Message string            `json:"message"`
	Fields  map[string]string `json:"fields,omitempty"`
}

var errRollback = errors.New("rollback")

// importRecords creates (or updates) the records of the file in one
// transaction: nothing is saved when a row fails, or with dryRun.
func importRecords(e *core.RequestEvent) error {
	c, err := findCollection(e)
	if err != nil {
		return err
	}
	if c.IsView() {
		return e.BadRequestError("View collections are read-only.", nil)
	}
	fh, err := readUpload(e)
	if err != nil {
		return e.BadRequestError("Upload a CSV, JSON or XLSX file.", err)
	}
	var opts importOptions
	if raw := e.Request.FormValue("options"); raw != "" {
		if err := json.Unmarshal([]byte(raw), &opts); err != nil {
			return e.BadRequestError("Invalid options.", err)
		}
	}
	t, err := parseTable(fh)
	if err != nil {
		return e.BadRequestError(err.Error(), nil)
	}
	if len(opts.Mapping) == 0 {
		opts.Mapping = suggestMapping(c, t.Columns)
	}
	fields := map[string]core.Field{}
	for col, name := range opts.Mapping {
		if name == "" {
			continue
		}
		f := c.Fields.GetByName(name)
		if f == nil || !importable(f) {
			return e.BadRequestError(fmt.Sprintf("The column %q can't be imported into %q.", col, name), nil)
		}
		fields[col] = f
	}
	if len(fields) == 0 {
		return e.BadRequestError("Map at least one column to a field.", nil)
	}
	if opts.Mode == "upsert" {
		if opts.MatchField == "" {
			opts.MatchField = core.FieldNameId
		}
		if c.Fields.GetByName(opts.MatchField) == nil {
			return e.BadRequestError("Unknown match field.", nil)
		}
	}

	ctx := actorContext(e)
	errs := []rowError{}
	created, updated := 0, 0
	txErr := e.App.RunInTransaction(func(txApp core.App) error {
		for i, row := range t.Rows {
			if len(errs) >= maxRowErrors {
				break
			}
			rowNum := t.FirstRow + i
			rec, isNew, err := importTarget(txApp, c, opts, fields, row)
			if err != nil {
				errs = append(errs, rowError{Row: rowNum, Message: err.Error()})
				continue
			}
			var cellErrs map[string]string
			hasPassword := false
			for col, f := range fields {
				v, ok := row[col]
				if !ok {
					continue
				}
				value, err := importValue(f, v)
				if err != nil {
					cellErrs = setErr(cellErrs, f.GetName(), err.Error())
					continue
				}
				if f.Type() == core.FieldTypePassword {
					if s, _ := value.(string); s != "" {
						rec.SetPassword(s)
						hasPassword = true
					}
					continue
				}
				rec.Set(f.GetName(), value)
			}
			if cellErrs != nil {
				errs = append(errs, rowError{Row: rowNum, Message: "Invalid values.", Fields: cellErrs})
				continue
			}
			if isNew && c.IsAuth() && !hasPassword {
				rec.SetRandomPassword()
			}
			if err := txApp.SaveWithContext(ctx, rec); err != nil {
				errs = append(errs, toRowError(rowNum, err))
				continue
			}
			if isNew {
				created++
			} else {
				updated++
			}
		}
		if len(errs) > 0 || opts.DryRun {
			return errRollback
		}
		return nil
	})
	if txErr != nil && !errors.Is(txErr, errRollback) {
		return e.BadRequestError("Import failed: "+txErr.Error(), txErr)
	}
	return e.JSON(http.StatusOK, map[string]any{
		"ok":      len(errs) == 0,
		"dryRun":  opts.DryRun,
		"saved":   len(errs) == 0 && !opts.DryRun,
		"total":   len(t.Rows),
		"created": created,
		"updated": updated,
		"errors":  errs,
		"stoppedAt": func() int {
			if len(errs) >= maxRowErrors {
				return errs[len(errs)-1].Row
			}
			return 0
		}(),
	})
}

// importTarget returns the record a row creates or (upsert) updates.
func importTarget(app core.App, c *core.Collection, opts importOptions, fields map[string]core.Field, row map[string]any) (*core.Record, bool, error) {
	if opts.Mode != "upsert" {
		return core.NewRecord(c), true, nil
	}
	var key any
	for col, f := range fields {
		if f.GetName() == opts.MatchField {
			key = row[col]
		}
	}
	keyStr := strings.TrimSpace(fmt.Sprint(key))
	if key == nil || keyStr == "" {
		return core.NewRecord(c), true, nil
	}
	var existing *core.Record
	var err error
	if opts.MatchField == core.FieldNameId {
		existing, err = app.FindRecordById(c, keyStr)
	} else {
		existing, err = app.FindFirstRecordByData(c, opts.MatchField, keyStr)
	}
	if err != nil {
		rec := core.NewRecord(c)
		return rec, true, nil
	}
	return existing, false, nil
}

func setErr(m map[string]string, k, v string) map[string]string {
	if m == nil {
		m = map[string]string{}
	}
	m[k] = v
	return m
}

func toRowError(row int, err error) rowError {
	var verrs validation.Errors
	if errors.As(err, &verrs) {
		fields := map[string]string{}
		for k, v := range verrs {
			fields[k] = v.Error()
		}
		return rowError{Row: row, Message: "Invalid values.", Fields: fields}
	}
	return rowError{Row: row, Message: err.Error()}
}
