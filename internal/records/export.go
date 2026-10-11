package records

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/types"
	"github.com/xuri/excelize/v2"
)

const (
	exportPageSize   = 500
	maxExportRecords = 200000
)

// exportFields returns the exported fields: the requested ones, or all the
// fields except passwords and token keys.
func exportFields(c *core.Collection, requested string) ([]core.Field, error) {
	var fields []core.Field
	if strings.TrimSpace(requested) != "" {
		for _, name := range strings.Split(requested, ",") {
			name = strings.TrimSpace(name)
			f := c.Fields.GetByName(name)
			if f == nil || f.Type() == core.FieldTypePassword || name == core.FieldNameTokenKey {
				return nil, fmt.Errorf("unknown field %q", name)
			}
			fields = append(fields, f)
		}
		return fields, nil
	}
	for _, f := range c.Fields {
		if f.Type() == core.FieldTypePassword || f.GetName() == core.FieldNameTokenKey {
			continue
		}
		fields = append(fields, f)
	}
	return fields, nil
}

// cellValue converts a record value for the CSV/XLSX cells: multiple values
// are joined with ";", objects as JSON, numbers and bools keep their type.
func cellValue(f core.Field, v any) any {
	switch val := v.(type) {
	case nil:
		return ""
	case []string:
		return strings.Join(val, ";")
	case string, float64, int, int64, bool:
		return val
	case types.DateTime:
		if val.IsZero() {
			return ""
		}
		return val.String()
	case types.JSONRaw:
		if len(val) == 0 || string(val) == "null" {
			return ""
		}
		return string(val)
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprint(v)
	}
	return string(raw)
}

func cellString(v any) string {
	switch val := v.(type) {
	case string:
		return val
	case bool:
		if val {
			return "true"
		}
		return "false"
	}
	return fmt.Sprint(v)
}

// eachRecord calls fn for every record matching the filter, page by page.
func eachRecord(app core.App, c *core.Collection, filter, sort string, fn func(*core.Record) error) error {
	for offset := 0; offset < maxExportRecords; offset += exportPageSize {
		records, err := app.FindRecordsByFilter(c, filter, sort, exportPageSize, offset)
		if err != nil {
			return err
		}
		for _, r := range records {
			if err := fn(r); err != nil {
				return err
			}
		}
		if len(records) < exportPageSize {
			return nil
		}
	}
	return nil
}

func exportRecords(e *core.RequestEvent) error {
	c, err := findCollection(e)
	if err != nil {
		return err
	}
	q := e.Request.URL.Query()
	format := q.Get("format")
	if format == "" {
		format = "csv"
	}
	fields, err := exportFields(c, q.Get("fields"))
	if err != nil {
		return e.BadRequestError(err.Error(), nil)
	}
	filter, sort := q.Get("filter"), q.Get("sort")
	// validate the filter and sort before starting the response
	if _, err := e.App.FindRecordsByFilter(c, filter, sort, 1, 0); err != nil {
		return e.BadRequestError("Invalid filter or sort: "+err.Error(), nil)
	}

	filename := fmt.Sprintf("%s-%s.%s", c.Name, time.Now().Format("20060102-1504"), format)
	h := e.Response.Header()
	h.Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	h.Set("Cache-Control", "no-store")

	switch format {
	case "csv":
		h.Set("Content-Type", "text/csv; charset=utf-8")
		e.Response.WriteHeader(http.StatusOK)
		return exportCSV(e.App, c, fields, filter, sort, e.Response)
	case "json":
		h.Set("Content-Type", "application/json")
		e.Response.WriteHeader(http.StatusOK)
		return exportJSON(e.App, c, fields, filter, sort, e.Response)
	case "xlsx":
		h.Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
		return exportXLSX(e.App, c, fields, filter, sort, e.Response)
	}
	return e.BadRequestError("Unknown format (csv, json or xlsx).", nil)
}

func exportCSV(app core.App, c *core.Collection, fields []core.Field, filter, sort string, out io.Writer) error {
	// BOM: Excel then reads the file as UTF-8
	if _, err := out.Write([]byte("\ufeff")); err != nil {
		return err
	}
	w := csv.NewWriter(out)
	header := make([]string, len(fields))
	for i, f := range fields {
		header[i] = f.GetName()
	}
	if err := w.Write(header); err != nil {
		return err
	}
	err := eachRecord(app, c, filter, sort, func(r *core.Record) error {
		row := make([]string, len(fields))
		for i, f := range fields {
			row[i] = cellString(cellValue(f, r.Get(f.GetName())))
		}
		return w.Write(row)
	})
	w.Flush()
	if err != nil {
		return err
	}
	return w.Error()
}

func exportJSON(app core.App, c *core.Collection, fields []core.Field, filter, sort string, out io.Writer) error {
	if _, err := io.WriteString(out, "[\n"); err != nil {
		return err
	}
	first := true
	err := eachRecord(app, c, filter, sort, func(r *core.Record) error {
		item := make(map[string]any, len(fields))
		for _, f := range fields {
			item[f.GetName()] = r.Get(f.GetName())
		}
		raw, err := json.Marshal(item)
		if err != nil {
			return err
		}
		if !first {
			if _, err := io.WriteString(out, ",\n"); err != nil {
				return err
			}
		}
		first = false
		_, err = out.Write(raw)
		return err
	})
	if err != nil {
		return err
	}
	_, err = io.WriteString(out, "\n]\n")
	return err
}

func exportXLSX(app core.App, c *core.Collection, fields []core.Field, filter, sort string, out http.ResponseWriter) error {
	f := excelize.NewFile()
	defer f.Close()
	sheet := c.Name
	if len(sheet) > 31 {
		sheet = sheet[:31] // Excel limit
	}
	if err := f.SetSheetName("Sheet1", sheet); err != nil {
		return err
	}
	sw, err := f.NewStreamWriter(sheet)
	if err != nil {
		return err
	}
	bold, _ := f.NewStyle(&excelize.Style{Font: &excelize.Font{Bold: true}})
	header := make([]any, len(fields))
	for i, fl := range fields {
		header[i] = excelize.Cell{Value: fl.GetName(), StyleID: bold}
	}
	if err := sw.SetRow("A1", header); err != nil {
		return err
	}
	rowNum := 2
	err = eachRecord(app, c, filter, sort, func(r *core.Record) error {
		row := make([]any, len(fields))
		for i, fl := range fields {
			row[i] = cellValue(fl, r.Get(fl.GetName()))
		}
		cell, _ := excelize.CoordinatesToCellName(1, rowNum)
		rowNum++
		return sw.SetRow(cell, row)
	})
	if err != nil {
		return err
	}
	if err := sw.Flush(); err != nil {
		return err
	}
	out.WriteHeader(http.StatusOK)
	return f.Write(out)
}
