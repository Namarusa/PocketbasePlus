// Pages → Theme: the site theme of the pages built with the Basecoat UI kit
// (style pack, light/dark, font, primary color, radius, chart colors, a pasted shadcn theme),
// with a live preview of the components.

import { field, handleError, page, select, textInput } from "../pocketbaseplus/lib.js";
import { icon, sections, styleURL, styles } from "./kit.js";
import { pagesSidebar } from "./pages.js";
import { colorsOf, loadPalettes, paletteCSS, paletteLabel } from "./palettes.js";

// the chart colors of the Basecoat styles (all the same)
const styleChartColors = ["#93c5fd", "#3b82f6", "#2563eb", "#1d4ed8", "#1e40af"];

const radii = ["0rem", "0.3rem", "0.5rem", "0.625rem", "0.75rem", "1rem"];

/** Text color on a background color (mirrors foregroundFor in kit.go). */
function foregroundFor(color) {
    const m = /^#([0-9a-f]{3}|[0-9a-f]{6})$/i.exec(color || "");
    if (!m) {
        return "#fff";
    }
    const h = m[1].length == 3 ? m[1].replace(/./g, "$&$&") : m[1];
    const v = parseInt(h, 16);
    return 0.299 * ((v >> 16) & 255) + 0.587 * ((v >> 8) & 255) + 0.114 * (v & 255) > 160 ? "#111" : "#fff";
}

/** The theme stylesheet (mirrors theme.css in kit.go). */
function themeCSS(t, palettes = []) {
    let css = `@font-face{font-family:"Geist Sans";src:url(/p/_static/geist-sans.woff2) format("woff2");font-weight:100 900;font-display:swap}`;
    if (t.font == "system") {
        css += `:root{--font-sans:ui-sans-serif,system-ui,-apple-system,"Segoe UI",Roboto,sans-serif}`;
    }
    if (/^[0-9]*\.?[0-9]+(rem|px|em)$/.test(t.radius || "")) {
        css += `:root{--radius:${t.radius}}`;
    }
    if (/^[#a-zA-Z0-9(),.%\s/-]{1,60}$/.test(t.primary || "")) {
        css += `:root,.dark{--primary:${t.primary};--primary-foreground:${foregroundFor(t.primary)}}`;
    }
    return css + (t.theme_css || "") + paletteCSS(t.chart_colors, palettes);
}

/** A sample chart for the preview: grouped bars of the 5 chart colors and a donut. */
function sampleChart() {
    const months = ["Jan", "Feb", "Mar", "Apr", "May", "Jun"];
    const series = ["Desktop", "Mobile", "Tablet", "TV", "Other"];
    const value = (m, s) => 40 + ((m * 37 + s * 53) % 60) - s * 6;
    const col = 100 / months.length;
    const bw = (col * 0.8) / series.length;
    let bars = "";
    months.forEach((_, m) =>
        series.forEach((_, s) => {
            bars += `<div class="gp-chart-bar" style="left:${m * col + col * 0.1 + s * bw}%;width:${bw}%;bottom:0;height:${value(m, s)}%;background:var(--gp-c${s + 1})"></div>`;
        }),
    );
    const legend = series
        .map((name, s) => `<span class="chart-legend-item"><span class="chart-legend-indicator" style="--chart-indicator-color:var(--gp-c${s + 1})"></span>${name}</span>`)
        .join("");
    let a = -Math.PI / 2;
    const slices = [30, 25, 20, 15, 10]
        .map((v, i) => {
            const b = a + (v / 100) * 2 * Math.PI;
            const p = (r, x) => `${(r * Math.cos(x)).toFixed(3)} ${(r * Math.sin(x)).toFixed(3)}`;
            const d = `M${p(1, a)}A1 1 0 0 1 ${p(1, b)}L${p(0.6, b)}A0.6 0.6 0 0 0 ${p(0.6, a)}Z`;
            a = b;
            return `<path class="gp-chart-slice" d="${d}" style="fill:var(--gp-c${i + 1})"/>`;
        })
        .join("");
    return `<div class="card" style="grid-column:1/-1"><header><h2>Charts</h2><p>The chart colors.</p></header><section style="display:grid;grid-template-columns:2fr 1fr;gap:24px;align-items:center">
<figure class="gp-chart" style="--gp-chart-h:180px"><div class="gp-chart-body"><div class="gp-chart-y"><span style="bottom:0%">0</span><span style="bottom:50%">50</span><span style="bottom:100%">100</span></div>
<div class="gp-chart-plot"><div class="gp-chart-grid"><i style="bottom:0%"></i><i style="bottom:50%"></i><i style="bottom:100%"></i></div><div class="gp-chart-bars">${bars}</div></div>
<div class="gp-chart-x">${months.map((m, i) => `<span style="left:${(i + 0.5) * col}%">${m}</span>`).join("")}</div></div><div class="chart-legend">${legend}</div></figure>
<figure class="gp-chart"><div class="gp-chart-pie-wrap" style="height:180px"><svg class="gp-chart-pie-svg" viewBox="-1 -1 2 2">${slices}</svg><div class="gp-chart-center"><strong>1.2k</strong><span>Visitors</span></div></div></figure></section></div>`;
}

const sample = `<div style="max-width:1000px;margin:0 auto;padding:28px;display:grid;gap:20px;grid-template-columns:repeat(auto-fit,minmax(280px,1fr))">
<div class="card"><header><h2>Create project</h2><p>Deploy your new project in one click.</p></header>
<section style="display:flex;flex-direction:column;gap:10px"><div style="display:flex;gap:8px;flex-wrap:wrap"><span class="badge">Badge</span><span class="badge" data-variant="secondary">Secondary</span><span class="badge" data-variant="outline">Outline</span><span class="badge" data-variant="destructive">Destructive</span></div>
<div class="progress" style="height:8px;border-radius:999px;background:var(--muted)"><span style="width:60%;background:var(--primary)"></span></div></section>
<footer style="gap:8px;flex-wrap:wrap"><button class="btn">Primary</button><button class="btn" data-variant="secondary">Secondary</button><button class="btn" data-variant="outline">Outline</button><button class="btn" data-variant="ghost">Ghost</button><button class="btn" data-variant="link">Link</button><button class="btn" data-variant="destructive">${icon("trash-2")} Delete</button></footer></div>
<div style="display:flex;flex-direction:column;gap:14px"><div style="display:flex;gap:8px;flex-wrap:wrap;align-items:center"><button class="btn" data-size="sm">${icon("plus")} New</button><button class="btn" data-variant="outline" data-size="sm">Next ${icon("arrow-right")}</button><button class="btn" data-variant="outline" data-size="icon-sm" aria-label="Settings">${icon("settings")}</button><button class="btn" data-variant="ghost" data-size="icon-sm" aria-label="Search">${icon("search")}</button><div class="button-group" role="group"><button class="btn" data-variant="outline" data-size="sm">${icon("arrow-left")}</button><button class="btn" data-variant="outline" data-size="sm">Today</button><button class="btn" data-variant="outline" data-size="sm">${icon("arrow-right")}</button></div></div>
<div class="alert">${icon("info")}<h2>Heads up!</h2><section>This is how alerts look.</section></div>
<div class="alert" data-variant="destructive">${icon("alert")}<h2>Error</h2><section>Something went wrong.</section></div>
<section class="accordion"><details open><summary>Is it accessible?${icon("chevron")}</summary><section><p>Yes, it follows the WAI-ARIA patterns.</p></section></details><details><summary>Is it styled?${icon("chevron")}</summary><section><p>Yes.</p></section></details></section></div>
<div class="table-container" style="grid-column:1/-1"><table class="table"><thead><tr><th>Invoice</th><th>Status</th><th style="text-align:right">Amount</th></tr></thead><tbody><tr><td>#001</td><td><span class="badge" data-variant="secondary">Paid</span></td><td style="text-align:right">$250.00</td></tr><tr><td>#002</td><td><span class="badge" data-variant="outline">Pending</span></td><td style="text-align:right">$150.00</td></tr></tbody></table></div></div>`;

const base = app.pb.buildURL("/_/extensions/pocketbaseplus-pages/");

export function pageTheme() {
    app.store.title = "Theme";
    if (!document.querySelector(`link[href="${base}builder.css"]`)) {
        document.head.appendChild(t.link({ rel: "stylesheet", href: base + "builder.css" }));
    }
    const data = store({ theme: null, saved: "", isSaving: false, preview: "", palettes: [] }); // preview: "", "light" or "dark"
    loadPalettes().then((list) => (data.palettes = list));
    const systemDark = () => matchMedia("(prefers-color-scheme: dark)").matches;
    const previewDark = (th) => (data.preview ? data.preview == "dark" : th.mode == "dark" || (th.mode == "auto" && systemDark()));
    const serialized = () => JSON.stringify(data.theme);

    app.pb
        .collection("pages_settings")
        .getFirstListItem("", { requestKey: null })
        .then((t) => {
            data.theme = {
                id: t.id,
                style: t.style || "vega",
                mode: t.mode || "light",
                font: t.font || "geist",
                primary: t.primary || "",
                radius: t.radius || "",
                theme_css: t.theme_css || "",
                chart_colors: t.chart_colors || "",
            };
            data.saved = serialized();
        })
        .catch(handleError);

    async function save() {
        data.isSaving = true;
        try {
            const { id, ...body } = data.theme;
            await app.pb.collection("pages_settings").update(id, body);
            data.saved = serialized();
            app.toasts.success("Successfully saved the theme.");
        } catch (err) {
            handleError(err);
        } finally {
            data.isSaving = false;
        }
    }

    function previewDoc(t) {
        return `<!doctype html><html class="${previewDark(t) ? "dark" : ""}"><head><meta charset="utf-8"><link rel="stylesheet" href="${styleURL(t.style)}"><link rel="stylesheet" href="${base}ui/gp-ui.css"><style>${themeCSS(t, data.palettes).replace(/<\//g, "<\\/")}</style></head><body>${sections.hero}${sample.replace(/<\/div>$/, sampleChart() + "</div>")}</body></html>`;
    }

    /** The chart colors picker: the palettes as swatches, the primary color's shades, custom colors. */
    // the first custom colors: the current ones when they're hex colors, else a varied set
    const customStart = (colors) => (colors.every((c) => /^#[0-9a-f]{6}$/i.test(c)) ? colors : ["#2563eb", "#16a34a", "#f59e0b", "#dc2626", "#7c3aed"]);

    function chartColorsField(th) {
        const custom = () => (th.chart_colors.startsWith("custom:") ? th.chart_colors.slice(7).split(",") : null);
        const isActive = (value) => (value == "custom" ? !!custom() : th.chart_colors == value);
        // colors: a list, or a function (the custom colors change)
        const choice = (value, label, colors) =>
            t.button(
                {
                    rid: "pal-" + value,
                    type: "button",
                    className: () => `gpp-palette ${isActive(value) ? "active" : ""}`,
                    ariaPressed: () => String(isActive(value)),
                    onclick: () =>
                        (th.chart_colors =
                            value == "custom"
                                ? "custom:" + customStart(colorsOf(th.chart_colors, data.palettes)?.light || styleChartColors).join(",")
                                : value),
                },
                t.span({ className: "gpp-swatches" }, ...[0, 1, 2, 3, 4].map((k) => t.i({ style: () => "background:" + (typeof colors == "function" ? colors() : colors)[k] }))),
                t.span({ className: "gpp-palette-name" }, label),
            );
        const primaryShades = () => colorsOf("primary", []).light.map((c) => c.replace(/var\(--primary\)/g, th.primary || "#171717").replace(/var\(--background\)/g, "#fff"));
        return field(
            { label: "Chart colors", hint: "The colors of the charts, from the shadcn/ui chart themes. A chart can also have its own in its settings." },
            t.div(
                null,
                t.div({ className: "gpp-palettes" }, () => [
                    choice("", "Style default", styleChartColors),
                    // keyed by content: the list comes once the palettes are loaded
                    ...data.palettes.map((p) => choice(p.name, paletteLabel(p.name), p.light)),
                    choice("primary", "Primary color shades", primaryShades),
                    choice("custom", "Custom", () => custom() || ["#e5e7eb", "#d1d5db", "#9ca3af", "#6b7280", "#374151"]),
                ]),
                t.div(
                    { className: "gpp-custom-colors", hidden: () => !custom() },
                    ...[0, 1, 2, 3, 4].map((i) =>
                        t.input({
                            type: "color",
                            className: "gpp-color",
                            ariaLabel: "Chart color " + (i + 1),
                            value: () => (/^#[0-9a-f]{6}$/i.test(custom()?.[i] || "") ? custom()[i] : "#888888"),
                            oninput: (e) => {
                                const list = [0, 1, 2, 3, 4].map((j) => custom()?.[j] || "#888888");
                                list[i] = e.target.value;
                                th.chart_colors = "custom:" + list.join(",");
                            },
                        }),
                    ),
                ),
            ),
        );
    }


    return page(
        {
            pbEvent: "gpPageTheme",
            sidebar: pagesSidebar(),
            breadcrumbs: [t.a({ href: "#/pages" }, "Pages"), "Theme"],
            wide: true,
            buttons: t.button(
                {
                    type: "button",
                    className: () => `btn expanded-lg ${data.isSaving ? "loading" : ""}`,
                    disabled: () => !data.theme || data.isSaving || serialized() == data.saved,
                    onclick: save,
                },
                t.span({ className: "txt" }, "Save changes"),
            ),
        },
        t.p(
            { className: "txt-hint m-t-0" },
            "The look of the pages built with the Basecoat UI kit (shadcn/ui components). Everything is embedded in the app: the pages load nothing from the internet.",
        ),
        () => {
            if (!data.theme) {
                return t.div({ rid: "loading", className: "txt-center" }, t.span({ className: "loader" }));
            }
            const th = data.theme;
            return t.div(
                { rid: "theme", className: "gpp-theme" },
                t.div(
                    { className: "gpp-theme-form" },
                    field(
                        { label: "Style" },
                        select({
                            options: styles.map((s) => ({ value: s, label: s[0].toUpperCase() + s.slice(1) + (s == "vega" ? " (default)" : "") })),
                            value: () => th.style,
                            onchange: (v) => (th.style = v),
                        }),
                    ),
                    field(
                        { label: "Mode", hint: 'Add a "Light / dark toggle" block to let visitors switch: their choice is remembered on every page.' },
                        select({
                            options: [
                                { value: "light", label: "Light" },
                                { value: "dark", label: "Dark" },
                                { value: "auto", label: "Auto (the visitor's system setting)" },
                            ],
                            value: () => th.mode,
                            onchange: (v) => {
                                th.mode = v;
                                data.preview = "";
                            },
                        }),
                    ),
                    field(
                        { label: "Font" },
                        select({
                            options: [
                                { value: "geist", label: "Geist (embedded)" },
                                { value: "system", label: "System font" },
                            ],
                            value: () => th.font,
                            onchange: (v) => (th.font = v),
                        }),
                    ),
                    field(
                        { label: "Primary color", hint: "Buttons, badges, links. Empty: the style's color." },
                        t.div(
                            { className: "flex gap-5" },
                            t.input({
                                type: "color",
                                className: "gpp-color",
                                value: () => (/^#[0-9a-f]{6}$/i.test(th.primary) ? th.primary : "#171717"),
                                oninput: (e) => (th.primary = e.target.value),
                            }),
                            textInput(th, "primary", { placeholder: "e.g. #2563eb or oklch(0.55 0.2 260)" }),
                        ),
                    ),
                    chartColorsField(th),
                    field(
                        { label: "Corner radius" },
                        select({
                            options: [{ value: "", label: "Style default" }, ...radii.map((r) => ({ value: r, label: r }))],
                            value: () => th.radius,
                            onchange: (v) => (th.radius = v),
                        }),
                    ),
                    field(
                        { label: "Theme CSS", hint: "Optional: paste a shadcn/ui theme (CSS variables :root { … } and .dark { … }), e.g. exported from a theme editor." },
                        t.textarea({
                            rows: 8,
                            className: "txt-code",
                            spellcheck: false,
                            placeholder: ":root {\n  --primary: oklch(0.55 0.2 260);\n}",
                            value: () => th.theme_css,
                            oninput: (e) => (th.theme_css = e.target.value),
                        }),
                    ),
                ),
                t.div(
                    { className: "gpp-theme-right" },
                    t.div(
                        { className: "gpp-theme-switch" },
                        ...["light", "dark"].map((m) =>
                            t.button(
                                {
                                    type: "button",
                                    className: () => `btn sm ${previewDark(th) == (m == "dark") ? "secondary" : "transparent secondary"}`,
                                    onclick: () => (data.preview = m),
                                },
                                t.i({ className: m == "dark" ? "ri-moon-line" : "ri-sun-line" }),
                                t.span({ className: "txt" }, m == "dark" ? "Dark preview" : "Light preview"),
                            ),
                        ),
                    ),
                    t.iframe({
                        className: "gpp-theme-preview",
                        title: "Theme preview",
                        sandbox: "allow-same-origin", // no scripts; same origin for the embedded font
                        srcdoc: () => previewDoc(th),
                    }),
                ),
            );
        },
    );
}
