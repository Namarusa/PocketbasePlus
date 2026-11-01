package documents

import (
	"context"
	"errors"
	"fmt"
	"io"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/filesystem"

	"github.com/Namarusa/PocketbasePlus/internal/formula"
	"github.com/Namarusa/PocketbasePlus/internal/httpcall"
	"github.com/Namarusa/PocketbasePlus/internal/labels"
	"github.com/Namarusa/PocketbasePlus/internal/medialib"
)

const (
	generateTimeout = 30 * time.Second
	maxListRecords  = 5000
	maxAssetBytes   = 10 << 20
)

// Input is what a document is made from.
type Input struct {
	Template *core.Record // the template (its assets), or nil for a preview of an unsaved one
	Spec     *Spec
	Name     string         // the template name (default file name)
	Record   *core.Record   // the record the document is about (optional)
	Values   map[string]any // given by the flow step
	Compress bool
}

// Result is a made document.
type Result struct {
	PDF      []byte
	Filename string
	Pages    int
	Errors   []BlockError
	Boxes    []Box   // where the blocks are (the editor)
	PageW    float64 // mm
	PageH    float64
}

// Make lays out the document.
func Make(ctx context.Context, app core.App, in Input) (*Result, error) {
	ctx, cancel := context.WithTimeout(ctx, generateTimeout)
	defer cancel()

	data := map[string]any{
		"record":   map[string]any{},
		"values":   in.Values,
		"lists":    map[string]any{},
		"app":      map[string]any{"name": app.Settings().Meta.AppName, "url": app.Settings().Meta.AppURL},
		"document": map[string]any{"name": in.Name, "format": in.Spec.Format},
	}
	if in.Values == nil {
		data["values"] = map[string]any{}
	}
	if in.Record != nil {
		data["record"] = related(recordMap(app, in.Record, 2))
	}
	lists := map[string]any{}
	var listErrs []BlockError
	for _, l := range in.Spec.Lists {
		items, err := loadList(app, l, in.Record, in.Values)
		if err != nil {
			listErrs = append(listErrs, BlockError{Block: "list:" + l.Name, Error: err.Error()})
		}
		lists[l.Name] = items
	}
	data["lists"] = lists

	load := func(b *Block, src string) ([]byte, error) { return loadImage(ctx, app, in.Template, in.Record, b, src) }
	r, err := render(ctx, in.Spec, data, load, in.Compress)
	if err != nil {
		return nil, err
	}
	return &Result{PDF: r.pdf, Pages: r.pages, Errors: append(listErrs, r.errs...), Filename: filename(in, data), Boxes: r.boxes, PageW: r.pageW, PageH: r.pageH}, nil
}

var unsafeFileChars = regexp.MustCompile(`[^A-Za-z0-9._ -]+`)

// filename renders the file name of a document (its formula, or the
// template name and the record).
func filename(in Input, data map[string]any) string {
	name := ""
	if v, ok := in.Values["filename"].(string); ok && strings.TrimSpace(v) != "" {
		name = v // given by the flow step
	} else if strings.TrimSpace(in.Spec.Filename) != "" {
		name, _ = formula.Render(in.Spec.Filename, data, roots...)
	}
	if strings.TrimSpace(name) == "" {
		name = in.Name
		if in.Record != nil {
			name += "-" + in.Record.Id
		}
	}
	name = strings.TrimSuffix(strings.TrimSpace(name), ".pdf")
	name = strings.Trim(unsafeFileChars.ReplaceAllString(name, "-"), "-. ")
	if name == "" {
		name = "document"
	}
	if len(name) > 120 {
		name = name[:120]
	}
	return name + ".pdf"
}

// --- data -------------------------------------------------------------------------------------

// related is a record: printed, it shows its name ({{record.customer}}, a
// relation); its fields stay reachable ({{record.customer.email}}).
type related map[string]any

func (r related) String() string {
	if l, ok := r["@label"].(string); ok {
		return l
	}
	return fmt.Sprint(r["id"])
}

// recordMap returns the fields of a record, its relations followed depth
// times (the passwords and tokens left out).
func recordMap(app core.App, rec *core.Record, depth int) map[string]any {
	out := map[string]any{
		"id":             rec.Id,
		"collectionName": rec.Collection().Name,
		"@label":         recordLabel(rec),
	}
	for _, f := range rec.Collection().Fields {
		name := f.GetName()
		switch f.Type() {
		case core.FieldTypePassword:
			continue
		case core.FieldTypeRelation:
			if depth <= 0 {
				out[name] = rec.Get(name)
				continue
			}
			ids := rec.GetStringSlice(name)
			rf := f.(*core.RelationField)
			list, _ := app.FindRecordsByIds(rf.CollectionId, ids)
			byID := map[string]*core.Record{}
			for _, r := range list {
				byID[r.Id] = r
			}
			var items []any
			for _, id := range ids {
				if r := byID[id]; r != nil {
					items = append(items, related(recordMap(app, r, depth-1)))
				}
			}
			if rf.MaxSelect <= 1 {
				if len(items) > 0 {
					out[name] = items[0]
				} else {
					out[name] = ""
				}
			} else {
				if items == nil {
					items = []any{}
				}
				out[name] = items
			}
			continue
		}
		if name == core.FieldNameTokenKey {
			continue
		}
		out[name] = rec.Get(name)
	}
	return out
}

// recordLabel is the name of a record: its presentable fields, a usual
// name field, or a reference (number, code…) before its id.
func recordLabel(rec *core.Record) string {
	if l := labels.Of(rec, labels.Fields(rec.Collection(), false)); l != rec.Id {
		return l
	}
	for _, name := range []string{"number", "reference", "ref", "code", "subject"} {
		if f := rec.Collection().Fields.GetByName(name); f != nil && !f.GetHidden() {
			if v := rec.GetString(name); v != "" {
				return v
			}
		}
	}
	return rec.Id
}

var listPlaceholder = regexp.MustCompile(`\{\{\s*\.?(record|values)\.([A-Za-z0-9_.]+)\s*\}\}`)

// loadList returns the records of a list of the document, its filter
// placeholders ({{record.id}}) given as parameters.
func loadList(app core.App, l List, rec *core.Record, values map[string]any) ([]any, error) {
	col, err := app.FindCachedCollectionByNameOrId(l.Collection)
	if err != nil {
		return []any{}, fmt.Errorf("list %s: unknown collection %q", l.Name, l.Collection)
	}
	params := dbx.Params{}
	var recData map[string]any
	if rec != nil {
		recData = recordMap(app, rec, 0)
	}
	filter := listPlaceholder.ReplaceAllStringFunc(l.Filter, func(m string) string {
		parts := listPlaceholder.FindStringSubmatch(m)
		var v any
		if parts[1] == "record" {
			v = formula.Lookup(recData, parts[2])
		} else {
			v = formula.Lookup(values, parts[2])
		}
		key := fmt.Sprintf("p%d", len(params))
		params[key] = fmt.Sprint(formula.Plain(v))
		if v == nil {
			params[key] = ""
		}
		return "{:" + key + "}"
	})
	limit := l.Limit
	if limit <= 0 || limit > maxListRecords {
		limit = maxListRecords
	}
	records, err := app.FindRecordsByFilter(col, filter, l.Sort, limit, 0, params)
	if err != nil {
		return []any{}, fmt.Errorf("list %s: %w", l.Name, err)
	}
	out := make([]any, 0, len(records))
	for _, r := range records {
		out = append(out, related(recordMap(app, r, 1)))
	}
	return out, nil
}

// loadImage returns the bytes of an image block: a file of the template,
// the first file of a field of the record, or the image at an address (src).
func loadImage(ctx context.Context, app core.App, tpl *core.Record, rec *core.Record, b *Block, src string) ([]byte, error) {
	var owner *core.Record
	var name string
	switch {
	case src != "":
		return loadImageURL(ctx, app, src)
	case b.Asset != "":
		if tpl == nil {
			return nil, errors.New("save the template to use its images")
		}
		for _, f := range tpl.GetStringSlice("assets") {
			if f == b.Asset {
				owner, name = tpl, f
			}
		}
		if owner == nil {
			return nil, fmt.Errorf("the image %q isn't a file of the template anymore", b.Asset)
		}
	case b.Field != "":
		if rec == nil {
			return nil, nil // no record (e.g. the preview without a sample)
		}
		files := rec.GetStringSlice(b.Field)
		if len(files) == 0 {
			return nil, nil
		}
		owner, name = rec, files[0]
	default:
		return nil, nil
	}
	return readFile(app, owner.BaseFilesPath()+"/"+name)
}

// readFile reads an image from the storage.
func readFile(app core.App, key string) ([]byte, error) {
	fsys, err := app.NewFilesystem()
	if err != nil {
		return nil, err
	}
	defer fsys.Close()
	r, err := fsys.GetReader(key)
	if err != nil {
		return nil, fmt.Errorf("image %s: %w", key[strings.LastIndex(key, "/")+1:], err)
	}
	defer r.Close()
	raw, err := io.ReadAll(io.LimitReader(r, maxAssetBytes+1))
	if err != nil {
		return nil, err
	}
	if len(raw) > maxAssetBytes {
		return nil, errors.New("the image is larger than 10 MB")
	}
	return raw, nil
}

// --- generated documents ----------------------------------------------------------------------

// Generate makes a document from a template for a record and stores it in
// the documents collection.
func Generate(ctx context.Context, app core.App, templateID, recordID string, values map[string]any, source string) (*core.Record, error) {
	tpl, err := app.FindRecordById(TemplatesCollection, templateID)
	if err != nil {
		tpl, err = app.FindFirstRecordByData(TemplatesCollection, "slug", templateID)
		if err != nil {
			return nil, fmt.Errorf("document template %q not found", templateID)
		}
	}
	spec, err := specOf(tpl)
	if err != nil {
		return nil, err
	}
	var rec *core.Record
	if col := tpl.GetString("collection"); recordID != "" {
		if col == "" {
			return nil, errors.New("the template isn't about a collection: it takes no record")
		}
		rec, err = app.FindRecordById(col, recordID)
		if err != nil {
			return nil, fmt.Errorf("record %q not found in %s", recordID, col)
		}
	}
	res, err := Make(ctx, app, Input{Template: tpl, Spec: spec, Name: tpl.GetString("slug"), Record: rec, Values: values, Compress: true})
	if err != nil {
		return nil, err
	}
	if len(res.Errors) > 0 {
		return nil, fmt.Errorf("the template has errors: %s", res.Errors[0].Error)
	}
	col, err := app.FindCachedCollectionByNameOrId(DocumentsCollection)
	if err != nil {
		return nil, err
	}
	file, err := filesystem.NewFileFromBytes(res.PDF, res.Filename)
	if err != nil {
		return nil, err
	}
	doc := core.NewRecord(col)
	doc.Set("template", tpl.Id)
	doc.Set("file", file)
	doc.Set("filename", res.Filename)
	doc.Set("pages", res.Pages)
	doc.Set("size", len(res.PDF))
	doc.Set("source", source)
	if rec != nil {
		doc.Set("collection", rec.Collection().Name)
		doc.Set("record_id", rec.Id)
	}
	if err := app.Save(doc); err != nil {
		return nil, err
	}
	return doc, nil
}

// Output is the value of a generated document in the flows.
func Output(doc *core.Record) map[string]any {
	return map[string]any{
		"document": doc.Id,
		"id":       doc.Id,
		"filename": doc.GetString("filename"),
		"pages":    doc.GetInt("pages"),
		"size":     doc.GetInt("size"),
	}
}

// File returns a generated document, to store in a record or attach.
func File(app core.App, id string) (*filesystem.File, error) {
	raw, name, err := Bytes(app, id)
	if err != nil {
		return nil, err
	}
	return filesystem.NewFileFromBytes(raw, name)
}

// Bytes returns the PDF of a generated document and its file name.
func Bytes(app core.App, id string) ([]byte, string, error) {
	doc, err := app.FindRecordById(DocumentsCollection, id)
	if err != nil {
		return nil, "", fmt.Errorf("document %q not found", id)
	}
	fsys, err := app.NewFilesystem()
	if err != nil {
		return nil, "", err
	}
	defer fsys.Close()
	r, err := fsys.GetReader(doc.BaseFilesPath() + "/" + doc.GetString("file"))
	if err != nil {
		return nil, "", err
	}
	defer r.Close()
	raw, err := io.ReadAll(r)
	return raw, doc.GetString("filename"), err
}

// loadImageURL reads the image at an address: a file of this application
// (/api/files/…, read from the storage) or an http(s) address, downloaded
// with the checks of the flows' requests (no private addresses).
func loadImageURL(ctx context.Context, app core.App, src string) ([]byte, error) {
	// a file of the Assets library: read from the storage
	if id, ok := medialib.ID(app, src); ok {
		raw, _, err := medialib.Read(app, id, maxAssetBytes)
		return raw, err
	}
	path := src
	if base := strings.TrimRight(app.Settings().Meta.AppURL, "/"); base != "" && strings.HasPrefix(path, base+"/") {
		path = strings.TrimPrefix(path, base)
	}
	if strings.HasPrefix(path, "/api/files/") {
		return loadLocalFile(app, path)
	}
	if !strings.HasPrefix(src, "http://") && !strings.HasPrefix(src, "https://") {
		return nil, fmt.Errorf("the image address %q must start with https:// (or /api/files/)", shorten(src, 80))
	}
	raw, err := httpcall.Download(ctx, src, maxAssetBytes, 15*time.Second)
	if errors.Is(err, httpcall.ErrTooLarge) {
		return nil, errors.New("the image is larger than 10 MB")
	}
	if err != nil {
		return nil, fmt.Errorf("image %s: %w", shorten(src, 80), err)
	}
	return raw, nil
}

// loadLocalFile reads /api/files/{collection}/{record}/{file} from the
// storage (the query, e.g. ?thumb=, is ignored).
func loadLocalFile(app core.App, path string) ([]byte, error) {
	path, _, _ = strings.Cut(path, "?")
	parts := strings.Split(strings.TrimPrefix(path, "/api/files/"), "/")
	if len(parts) != 3 {
		return nil, fmt.Errorf("the image address %q isn't a file of a record", shorten(path, 80))
	}
	col, err := app.FindCachedCollectionByNameOrId(parts[0])
	if err != nil {
		return nil, fmt.Errorf("image: no collection %q", parts[0])
	}
	rec, err := app.FindRecordById(col, parts[1])
	if err != nil {
		return nil, fmt.Errorf("image: no record %q in %s", parts[1], col.Name)
	}
	name := parts[2]
	found := false
	for _, f := range col.Fields {
		if f.Type() == core.FieldTypeFile && slices.Contains(rec.GetStringSlice(f.GetName()), name) {
			found = true
			break
		}
	}
	if !found {
		return nil, fmt.Errorf("image: %s isn't a file of the record", name)
	}
	return readFile(app, rec.BaseFilesPath()+"/"+name)
}

func shorten(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}
