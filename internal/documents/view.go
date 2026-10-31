package documents

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/pocketbase/pocketbase/core"

	"github.com/Namarusa/PocketbasePlus/internal/pdfview"
)

// The documents shown in the pages: a PDF viewer of a page shows a template
// drawn for the record of the page (or of a list item). The page signs the
// link (pdfview.ViewLink): the visitor sees the document of the records the
// page showed them, nothing else. The PDF isn't stored; the last ones are
// kept a few minutes (a page opened again, several viewers).

const (
	viewCacheTTL  = 5 * time.Minute
	maxViewCached = 40
)

var viewCache = struct {
	sync.Mutex
	items map[string]viewItem
}{items: map[string]viewItem{}}

type viewItem struct {
	pdf      []byte
	filename string
	expires  time.Time
}

func viewRoute(e *core.RequestEvent) error {
	q := e.Request.URL.Query()
	tplID, target := e.Request.PathValue("template"), e.Request.PathValue("target")
	colName, recID, ok := pdfview.CheckView(e.App, tplID, target, q.Get("exp"), q.Get("sig"))
	if !ok {
		return e.ForbiddenError("This link expired: open the page again.", nil)
	}
	tpl, err := findTemplate(e.App, tplID)
	if err != nil {
		return e.NotFoundError("The document isn't available.", nil)
	}
	var rec *core.Record
	if tc := tpl.GetString("collection"); tc != "" && recID != "" {
		col, err := e.App.FindCachedCollectionByNameOrId(colName)
		if err != nil || col.Name != tc {
			return e.BadRequestError(fmt.Sprintf("The template is about the records of %s, not %s.", tc, colName), nil)
		}
		if rec, err = e.App.FindRecordById(col, recID); err != nil {
			return e.NotFoundError("The document isn't available.", nil)
		}
	}
	key := tpl.Id + "|" + tpl.GetString("updated") + "|" + recID
	if rec != nil {
		key += "|" + rec.GetString("updated")
	}
	item, err := cachedView(key, func() (viewItem, error) {
		spec, err := specOf(tpl)
		if err != nil {
			return viewItem{}, err
		}
		res, err := Make(e.Request.Context(), e.App, Input{Template: tpl, Spec: spec, Name: tpl.GetString("slug"), Record: rec, Compress: true})
		if err != nil {
			return viewItem{}, err
		}
		for _, be := range res.Errors {
			e.App.Logger().Warn("Document shown in a page: a block failed", "template", tpl.GetString("slug"), "block", be.Block, "error", be.Error)
		}
		return viewItem{pdf: res.PDF, filename: res.Filename}, nil
	})
	if err != nil {
		e.App.Logger().Error("Document shown in a page failed", "template", tpl.GetString("slug"), "error", err)
		return e.InternalServerError("The document couldn't be made.", nil)
	}
	h := e.Response.Header()
	h.Set("Content-Disposition", "inline; filename*=UTF-8''"+url.PathEscape(item.filename))
	h.Set("Cache-Control", "private, max-age=300")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'")
	return e.Blob(http.StatusOK, "application/pdf", item.pdf)
}

// cachedView returns the cached document of key, or makes it.
func cachedView(key string, make func() (viewItem, error)) (viewItem, error) {
	now := time.Now()
	viewCache.Lock()
	it, ok := viewCache.items[key]
	viewCache.Unlock()
	if ok && now.Before(it.expires) {
		return it, nil
	}
	it, err := make()
	if err != nil {
		return it, err
	}
	it.expires = now.Add(viewCacheTTL)
	viewCache.Lock()
	defer viewCache.Unlock()
	for k, v := range viewCache.items {
		if now.After(v.expires) {
			delete(viewCache.items, k)
		}
	}
	if len(viewCache.items) >= maxViewCached {
		clear(viewCache.items)
	}
	viewCache.items[key] = it
	return it, nil
}

// findTemplate finds a template by its id or slug.
func findTemplate(app core.App, idOrSlug string) (*core.Record, error) {
	if idOrSlug == "" {
		return nil, errors.New("no template")
	}
	tpl, err := app.FindRecordById(TemplatesCollection, idOrSlug)
	if err != nil {
		tpl, err = app.FindFirstRecordByData(TemplatesCollection, "slug", idOrSlug)
	}
	return tpl, err
}
