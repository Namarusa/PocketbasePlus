package pages

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	validation "github.com/pocketbase/ozzo-validation/v4"
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

// Basecoat UI (shadcn/ui in plain HTML/CSS/JS, MIT) is the component kit of
// the pages: its style bundles, script and the Geist fonts are embedded (the
// pages never load anything from the internet).
const (
	// SettingsCollection holds the site theme (one record).
	SettingsCollection  = "pages_settings"
	kitBasecoat         = "basecoat"
	basecoatVersion     = "1.0.2"
	kitMigrationFile    = "1760900000_pages_kit.go"
	modeMigrationFile   = "1761000000_pages_mode.go"
	chartsMigrationFile = "1761500000_pages_chart_colors.go"
)

// Modes of the site theme: auto follows the visitor's system setting.
var modes = []string{"light", "dark", "auto"}

// Styles are the Basecoat style packs (vega is the default).
var Styles = []string{"vega", "nova", "maia", "lyra", "mira", "luma", "sera", "rhea"}

func init() {
	m.Register(func(app core.App) error {
		pages, err := app.FindCollectionByNameOrId(Collection)
		if err != nil {
			return err
		}
		// existing pages keep no kit (Basecoat's base styles would change them)
		pages.Fields.Add(&core.SelectField{Name: "kit", MaxSelect: 1, Values: []string{kitBasecoat}})
		if err := app.Save(pages); err != nil {
			return err
		}

		settings := core.NewBaseCollection(SettingsCollection)
		settings.Fields.Add(
			&core.SelectField{Name: "style", MaxSelect: 1, Values: Styles},
			&core.SelectField{Name: "mode", MaxSelect: 1, Values: []string{"light", "dark"}},
			&core.SelectField{Name: "font", MaxSelect: 1, Values: []string{"geist", "system"}},
			&core.TextField{Name: "primary", Max: 60},
			&core.TextField{Name: "radius", Max: 20},
			&core.TextField{Name: "theme_css", Max: 100_000}, // pasted shadcn theme (:root{...} .dark{...})
			&core.AutodateField{Name: "updated", OnCreate: true, OnUpdate: true},
		)
		if err := app.Save(settings); err != nil {
			return err
		}
		rec := core.NewRecord(settings)
		rec.Load(map[string]any{"style": "vega", "mode": "light", "font": "geist"})
		if err := app.Save(rec); err != nil {
			return err
		}
		return markSystem(app, true, SettingsCollection)
	}, func(app core.App) error {
		if err := markSystem(app, false, SettingsCollection); err != nil {
			return err
		}
		if c, err := app.FindCollectionByNameOrId(SettingsCollection); err == nil {
			if err := app.Delete(c); err != nil {
				return err
			}
		}
		pages, err := app.FindCollectionByNameOrId(Collection)
		if err != nil {
			return nil
		}
		pages.Fields.RemoveByName("kit")
		return app.Save(pages)
	}, kitMigrationFile)

	// the auto mode
	m.Register(func(app core.App) error {
		return setModes(app, modes)
	}, func(app core.App) error {
		return setModes(app, modes[:2])
	}, modeMigrationFile)

	// the chart colors of the site
	m.Register(func(app core.App) error {
		settings, err := app.FindCollectionByNameOrId(SettingsCollection)
		if err != nil {
			return err
		}
		settings.Fields.Add(&core.TextField{Name: "chart_colors", Max: 200})
		return app.Save(settings)
	}, func(app core.App) error {
		settings, err := app.FindCollectionByNameOrId(SettingsCollection)
		if err != nil {
			return err
		}
		settings.Fields.RemoveByName("chart_colors")
		return app.Save(settings)
	}, chartsMigrationFile)
}

func setModes(app core.App, values []string) error {
	settings, err := app.FindCollectionByNameOrId(SettingsCollection)
	if err != nil {
		return err
	}
	f, ok := settings.Fields.GetByName("mode").(*core.SelectField)
	if !ok {
		return fmt.Errorf("pages_settings.mode not found")
	}
	f.Values = values
	if !slices.Contains(values, "auto") {
		if _, err := app.DB().NewQuery("UPDATE {{" + SettingsCollection + "}} SET [[mode]] = 'light' WHERE [[mode]] = 'auto'").Execute(); err != nil {
			return err
		}
	}
	return app.Save(settings)
}

// --- site theme -------------------------------------------------------------------------

type theme struct {
	Style    string
	Mode     string // light, dark or auto
	Font     string
	Primary  string
	Radius   string
	Charts   string // chart colors (chartcolors.go)
	CSS      string
	Modified time.Time
}

func loadTheme(app core.App) theme {
	t := theme{Style: "vega", Mode: "light", Font: "geist"}
	rec, err := app.FindFirstRecordByFilter(SettingsCollection, "id != ''")
	if err != nil {
		return t
	}
	if s := rec.GetString("style"); slices.Contains(Styles, s) {
		t.Style = s
	}
	if md := rec.GetString("mode"); slices.Contains(modes, md) {
		t.Mode = md
	}
	if f := rec.GetString("font"); f != "" {
		t.Font = f
	}
	t.Primary = rec.GetString("primary")
	t.Radius = rec.GetString("radius")
	t.Charts = rec.GetString("chart_colors")
	t.CSS = rec.GetString("theme_css")
	t.Modified = rec.GetDateTime("updated").Time()
	return t
}

var (
	colorRegex  = regexp.MustCompile(`^[#a-zA-Z0-9(),.%\s/-]{1,60}$`)
	radiusRegex = regexp.MustCompile(`^[0-9]*\.?[0-9]+(rem|px|em)$`)
	hexRegex    = regexp.MustCompile(`^#([0-9a-fA-F]{3}|[0-9a-fA-F]{6})$`)
)

func validateSettings(rec *core.Record) error {
	errs := validation.Errors{}
	if v := strings.TrimSpace(rec.GetString("primary")); v != "" && !colorRegex.MatchString(v) {
		errs["primary"] = validation.NewError("validation_invalid_color", "Use a CSS color, e.g. #2563eb or oklch(0.55 0.2 260).")
	}
	if v := strings.TrimSpace(rec.GetString("radius")); v != "" && !radiusRegex.MatchString(v) {
		errs["radius"] = validation.NewError("validation_invalid_radius", "Use a length, e.g. 0.625rem or 8px.")
	}
	if v := rec.GetString("chart_colors"); v != "" {
		if _, _, ok := chartColors(v); !ok {
			errs["chart_colors"] = validation.NewError("validation_invalid_chart_colors", "Choose a palette, or 5 colors like #2563eb.")
		}
	}
	if v := rec.GetString("login_page"); v != "" && !slugRegex.MatchString(v) {
		errs["login_page"] = validation.NewError("validation_invalid_slug", "Choose a page.")
	}
	if len(errs) > 0 {
		return errs
	}
	return nil
}

var slugRegex = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

// themeCSS is the stylesheet of the site theme: the embedded fonts and the
// overrides of the theme variables (after the Basecoat bundle).
func (t theme) css() string {
	var b strings.Builder
	b.WriteString(`@font-face{font-family:"Geist Sans";src:url(/p/_static/geist-sans.woff2) format("woff2");font-weight:100 900;font-style:normal;font-display:swap}` + "\n")
	b.WriteString(`@font-face{font-family:"Geist Mono";src:url(/p/_static/geist-mono.woff2) format("woff2");font-weight:100 900;font-style:normal;font-display:swap}` + "\n")
	if t.Font == "system" {
		b.WriteString(`:root{--font-sans:ui-sans-serif,system-ui,-apple-system,"Segoe UI",Roboto,sans-serif}` + "\n")
	}
	if r := strings.TrimSpace(t.Radius); radiusRegex.MatchString(r) {
		b.WriteString(":root{--radius:" + r + "}\n")
	}
	if p := strings.TrimSpace(t.Primary); colorRegex.MatchString(p) {
		b.WriteString(":root,.dark{--primary:" + p + ";--primary-foreground:" + foregroundFor(p) + "}\n")
	}
	// theme toggles show the icon of the mode they switch to
	b.WriteString(`html:not(.dark) [data-theme-toggle] .icon-sun,.dark [data-theme-toggle] .icon-moon{display:none}` + "\n")
	if t.CSS != "" {
		b.WriteString("/* theme */\n" + t.CSS + "\n")
	}
	// the chart colors picked in the theme settings win over a pasted theme's
	b.WriteString(chartColorsCSS(t.Charts))
	return b.String()
}

// foregroundFor returns a readable text color on a background color (white,
// or black on light hex colors).
func foregroundFor(color string) string {
	if !hexRegex.MatchString(color) {
		return "#fff"
	}
	h := color[1:]
	if len(h) == 3 {
		h = string([]byte{h[0], h[0], h[1], h[1], h[2], h[2]})
	}
	v, _ := strconv.ParseUint(h, 16, 32)
	r, g, b := float64(v>>16&255), float64(v>>8&255), float64(v&255)
	if 0.299*r+0.587*g+0.114*b > 160 {
		return "#111"
	}
	return "#fff"
}

func serveTheme(e *core.RequestEvent) error {
	t := loadTheme(e.App)
	h := e.Response.Header()
	h.Set("Content-Type", "text/css; charset=utf-8")
	h.Set("Cache-Control", "no-cache")
	h.Set("X-Content-Type-Options", "nosniff")
	http.ServeContent(e.Response, e.Request, "theme.css", t.Modified, strings.NewReader(t.css()))
	return nil
}

// --- embedded static files ---------------------------------------------------------------

type staticFile struct {
	path string // in extFS
	ct   string
	gz   bool // stored gzipped
}

var staticFiles = func() map[string]staticFile {
	files := map[string]staticFile{
		"basecoat-" + basecoatVersion + ".js": {"ext/vendor/basecoat-" + basecoatVersion + "/basecoat.js.gz", "text/javascript; charset=utf-8", true},
		"geist-sans.woff2":                    {"ext/vendor/geist-1.7.2/Geist-Variable.woff2", "font/woff2", false},
		"geist-mono.woff2":                    {"ext/vendor/geist-1.7.2/GeistMono-Variable.woff2", "font/woff2", false},
	}
	for _, s := range Styles {
		files[styleFile(s)] = staticFile{"ext/vendor/basecoat-" + basecoatVersion + "/" + s + ".css.gz", "text/css; charset=utf-8", true}
	}
	files[uiJS] = staticFile{"ext/ui/gp-ui.js", "text/javascript; charset=utf-8", false}
	files[uiCSS] = staticFile{"ext/ui/gp-ui.css", "text/css; charset=utf-8", false}
	return files
}()

// uiJS and uiCSS are the pages script and styles of the charts and calendars
// (named by their content: cached for a year, new names after an upgrade).
var uiJS, uiCSS = versioned("ext/ui/gp-ui.js", "gp-ui-%s.js"), versioned("ext/ui/gp-ui.css", "gp-ui-%s.css")

func versioned(path, pattern string) string {
	data, _ := extFS.ReadFile(path)
	sum := sha256.Sum256(data)
	return fmt.Sprintf(pattern, hex.EncodeToString(sum[:4]))
}

func styleFile(style string) string {
	return "basecoat-" + basecoatVersion + "-" + style + ".css"
}

// serveStatic serves the embedded kit files (versioned names: cached for a year).
func serveStatic(e *core.RequestEvent) error {
	f, ok := staticFiles[e.Request.PathValue("file")]
	if !ok {
		return e.NotFoundError("", nil)
	}
	data, err := extFS.ReadFile(f.path)
	if err != nil {
		return e.NotFoundError("", err)
	}
	h := e.Response.Header()
	h.Set("Content-Type", f.ct)
	h.Set("Cache-Control", "public, max-age=31536000, immutable")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Vary", "Accept-Encoding")
	if f.gz {
		if strings.Contains(e.Request.Header.Get("Accept-Encoding"), "gzip") {
			h.Set("Content-Encoding", "gzip")
		} else {
			zr, err := gzip.NewReader(bytes.NewReader(data))
			if err != nil {
				return e.InternalServerError("", err)
			}
			defer zr.Close()
			if data, err = io.ReadAll(zr); err != nil {
				return e.InternalServerError("", err)
			}
		}
	}
	h.Set("Content-Length", strconv.Itoa(len(data)))
	e.Response.WriteHeader(http.StatusOK)
	if e.Request.Method != http.MethodHead {
		_, err = e.Response.Write(data)
	}
	return err
}

// kitScript runs first in the head of the pages with the kit (allowed by its
// hash in the CSP, so the pages need no other script): it applies the light or
// dark mode before the page shows (the visitor's choice made with a theme
// toggle, else the site mode, "auto" following the system setting), handles
// the theme and sidebar toggles, and marks the sidebar link of the page.
const kitScript = `(()=>{const d=document.documentElement,q=matchMedia("(prefers-color-scheme: dark)"),k="themeMode",` +
	`get=()=>{try{return localStorage.getItem(k)}catch(e){}},` +
	`apply=()=>{const s=get(),m=d.dataset.themeMode;d.classList.toggle("dark",s?s=="dark":m=="auto"?q.matches:m=="dark")};` +
	`apply();q.addEventListener("change",apply);` +
	`document.addEventListener("click",e=>{const t=e.target.closest&&e.target.closest("[data-theme-toggle],[data-sidebar-toggle]");if(!t)return;` +
	`if(t.hasAttribute("data-theme-toggle")){const v=d.classList.toggle("dark")?"dark":"light";try{localStorage.setItem(k,v)}catch(e){}return}` +
	`const id=t.getAttribute("data-sidebar-toggle"),s=id?document.getElementById(id):document.querySelector(".sidebar");if(s&&s.toggle)s.toggle()});` +
	`document.addEventListener("DOMContentLoaded",()=>{if(document.querySelector(".sidebar [aria-current=page]"))return;` +
	`for(const a of document.querySelectorAll(".sidebar nav a[href]:not([href^='#'])"))if(a.origin==location.origin&&a.pathname==location.pathname){a.setAttribute("aria-current","page");break}})})()`

var kitScriptHash = func() string {
	sum := sha256.Sum256([]byte(kitScript))
	return "'sha256-" + base64.StdEncoding.EncodeToString(sum[:]) + "'"
}()

// kitHead returns the mode script and the stylesheets of a page with the kit.
func kitHead(t theme) string {
	return "<script>" + kitScript + "</script>\n" + fmt.Sprintf(`<link rel="stylesheet" href="/p/_static/%s">`+"\n"+`<link rel="stylesheet" href="/p/_theme.css?v=%d">`+"\n",
		styleFile(t.Style), t.Modified.Unix())
}
