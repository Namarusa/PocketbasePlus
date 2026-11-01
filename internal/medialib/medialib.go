// Package medialib is what the other modules know of the Assets library
// (the optional internal/media module): the address of a file of the library
// and how to read it. Like internal/pdfview, it couples the modules by a
// convention, not an import: a page, a document or an email links to
// /media/{id}/{name}, and works again as soon as the file is replaced.
package medialib

import (
	"errors"
	"fmt"
	"io"
	"net/url"
	"regexp"
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

const (
	// Collection holds the files of the library.
	Collection = "media_assets"
	// Prefix is the public address of the files: /media/{id}/{name}.
	Prefix = "/media/"
)

// ErrNotInstalled is returned when the Assets module isn't there.
var ErrNotInstalled = errors.New("the Assets module isn't installed")

var idRegex = regexp.MustCompile(`^[a-z0-9]{15}$`)

// Link returns the address of a file of the library (the name is only
// there to read well: renaming the file keeps the old links working).
func Link(id, name string) string {
	if name == "" {
		name = "file"
	}
	return Prefix + id + "/" + url.PathEscape(name)
}

// ID returns the id of a library address: /media/{id}/..., or the same on
// the address of this app (https://app.example.com/media/{id}/...).
func ID(app core.App, link string) (string, bool) {
	link = strings.TrimSpace(link)
	if !strings.HasPrefix(link, Prefix) {
		u, err := url.Parse(link)
		if err != nil || u.Host == "" {
			return "", false
		}
		own, err := url.Parse(app.Settings().Meta.AppURL)
		if err != nil || !strings.EqualFold(own.Host, u.Host) {
			return "", false
		}
		link = u.Path
	}
	rest := strings.TrimPrefix(link, Prefix)
	id, _, _ := strings.Cut(rest, "/")
	id, _, _ = strings.Cut(id, "?")
	if !idRegex.MatchString(id) {
		return "", false
	}
	return id, true
}

// Find returns the record of a file of the library.
func Find(app core.App, id string) (*core.Record, error) {
	col, err := app.FindCachedCollectionByNameOrId(Collection)
	if err != nil {
		return nil, ErrNotInstalled
	}
	rec, err := app.FindRecordById(col, id)
	if err != nil {
		return nil, fmt.Errorf("the file %s isn't in the Assets library anymore", id)
	}
	return rec, nil
}

// Read returns the content of a file of the library (at most limit bytes).
func Read(app core.App, id string, limit int64) ([]byte, *core.Record, error) {
	rec, err := Find(app, id)
	if err != nil {
		return nil, nil, err
	}
	fsys, err := app.NewFilesystem()
	if err != nil {
		return nil, nil, err
	}
	defer fsys.Close()
	r, err := fsys.GetReader(rec.BaseFilesPath() + "/" + rec.GetString("file"))
	if err != nil {
		return nil, nil, fmt.Errorf("the file of %q can't be read: %w", rec.GetString("name"), err)
	}
	defer r.Close()
	raw, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, nil, err
	}
	if int64(len(raw)) > limit {
		return nil, nil, fmt.Errorf("%q is larger than %d MB", rec.GetString("name"), limit>>20)
	}
	return raw, rec, nil
}
