package flows

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/filesystem"
)

// The documents of the optional Documents module: the "Generate document"
// step makes a PDF from a template; the other steps take it (as
// {{steps.invoice}}): Send email attaches it, Create / Update record store it
// in a file field, Respond answers with it.

// DocumentRequest asks the Documents module for a document.
type DocumentRequest struct {
	Ctx      context.Context
	App      core.App
	Template string // id or slug
	Record   string // record id (of the template's collection)
	Values   map[string]any
	Source   string
}

// Documents is installed by the Documents module.
type Documents struct {
	// Generate makes and stores a document; it returns the step output
	// ({document, id, filename, pages, size}).
	Generate func(r DocumentRequest) (map[string]any, error)
	// File returns a generated document as a file to store in a record.
	File func(app core.App, id string) (*filesystem.File, error)
	// Bytes returns the PDF of a generated document and its file name.
	Bytes func(app core.App, id string) ([]byte, string, error)
	// Exists reports whether a template exists (the checks of the flows).
	Exists func(app core.App, id string) bool
}

var documents *Documents

var errNoDocuments = errors.New("the Documents module isn't installed")

// SetDocuments installs the documents (the "Generate document" step).
func SetDocuments(d *Documents) { documents = d }

func stepGenerateDocument(r *run, s Step) (any, any, error) {
	if documents == nil {
		return nil, nil, errNoDocuments
	}
	cfg, err := r.resolved(s, "record", "values", "filename")
	if err != nil {
		return cfg, nil, err
	}
	tpl := asString(s.Config, "template")
	if tpl == "" {
		return cfg, nil, errors.New("choose a document template")
	}
	values := asMap(cfg["values"])
	if values == nil {
		values = map[string]any{}
	}
	if name := strings.TrimSpace(toString(cfg["filename"])); name != "" && name != "<nil>" {
		values["filename"] = name
	}
	out, err := documents.Generate(DocumentRequest{
		Ctx:      r.ctx,
		App:      r.app,
		Template: tpl,
		Record:   documentRecordID(cfg["record"]),
		Values:   values,
		Source:   "Flow: " + r.flow.GetString("name"),
	})
	if err != nil {
		return cfg, nil, err
	}
	return cfg, out, nil
}

// documentRecordID is the record of the document: an id, or a record
// ({{trigger.record}}).
func documentRecordID(v any) string {
	if m := asMap(v); m != nil {
		return toString(m["id"])
	}
	s := strings.TrimSpace(toString(v))
	if s == "<nil>" {
		return ""
	}
	return s
}

// DocumentID returns the id of a document given to a step: the output of a
// "Generate document" step ({{steps.invoice}}), or its id.
func DocumentID(v any) string {
	if m := asMap(v); m != nil {
		if id, ok := m["document"].(string); ok {
			return id
		}
		return ""
	}
	return ""
}

// documentFiles replaces the documents given to the file fields of a
// record by their file.
func documentFiles(app core.App, col *core.Collection, fields map[string]any) error {
	for name, v := range fields {
		if _, ok := col.Fields.GetByName(name).(*core.FileField); !ok {
			continue
		}
		id := DocumentID(v)
		if id == "" {
			continue
		}
		if documents == nil {
			return errNoDocuments
		}
		f, err := documents.File(app, id)
		if err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		fields[name] = f
	}
	return nil
}

// documentAttachments returns the ids of the documents of a Send email step.
func documentAttachments(v any) ([]string, error) {
	var ids []string
	add := func(x any) error {
		if id := DocumentID(x); id != "" {
			ids = append(ids, id)
			return nil
		}
		if s := strings.TrimSpace(toString(x)); s != "" && s != "<nil>" {
			return fmt.Errorf("attachments: %q isn't a document (the output of a Generate document step)", s)
		}
		return nil
	}
	switch list := v.(type) {
	case nil:
	case []any:
		for _, x := range list {
			if err := add(x); err != nil {
				return nil, err
			}
		}
	default:
		if err := add(v); err != nil {
			return nil, err
		}
	}
	if len(ids) > 10 {
		return nil, errors.New("attachments: 10 documents at most")
	}
	if len(ids) > 0 && documents == nil {
		return nil, errNoDocuments
	}
	return ids, nil
}
