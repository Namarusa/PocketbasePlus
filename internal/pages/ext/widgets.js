// Pages: the chart, calendar and date picker blocks of the builder. The
// elements are placeholders (data-pb-* settings): the server draws the chart
// with the data, or writes the date fields, and the canvas shows what the
// published page shows (rendered by /api/pages/{id}/snippet, with the
// pages script of the calendars running in the canvas).

import { api } from "../pocketbaseplus/lib.js";
import { colorsOf, loadPalettes, paletteLabel } from "./palettes.js";

const groupable = ["text", "email", "url", "select", "bool", "number", "relation", "date", "autodate"];

const fnOptions = [
    { id: "", label: "Count of records" },
    { id: "count_distinct", label: "Count of distinct values" },
    { id: "sum", label: "Sum" },
    { id: "avg", label: "Average" },
    { id: "min", label: "Minimum" },
    { id: "max", label: "Maximum" },
];

/**
 * Defines the component types. ctx: { pageId, byName(name), collectionOptions(), viewAs() }.
 */
export function defineWidgetTypes(ed, ctx) {
    const dc = ed.DomComponents;
    defineColorsTrait(ed);
    const has = (el, attr) => el?.nodeType == 1 && el.hasAttribute(attr);
    const view = {
        onRender({ el, model }) {
            renderSnippet(ed, ctx, model, el);
        },
    };
    const rerender = function () {
        this.view?.render();
    };

    dc.addType("pb-chart", {
        isComponent: (el) => has(el, "data-pb-chart"),
        model: {
            defaults: {
                name: "Chart",
                icon: '<i class="ri-bar-chart-2-line"></i>',
                droppable: false,
                components: [],
                traits: [
                    {
                        type: "select",
                        name: "data-pb-chart",
                        label: "Chart",
                        options: [
                            { id: "bar", label: "Bars" },
                            { id: "line", label: "Line" },
                            { id: "area", label: "Area" },
                            { id: "pie", label: "Pie" },
                            { id: "donut", label: "Donut" },
                        ],
                    },
                    { type: "select", name: "data-pb-collection", label: "Collection", options: ctx.collectionOptions() },
                    { type: "select", name: "data-pb-fn", label: "Value", options: fnOptions },
                    { type: "select", name: "data-pb-value", label: "Field", options: [] },
                    { type: "select", name: "data-pb-group", label: "Group by", options: [] },
                    { type: "select", name: "data-pb-date", label: "Over time", options: [] },
                    {
                        type: "select",
                        name: "data-pb-precision",
                        label: "Interval",
                        options: [
                            { id: "", label: "Day" },
                            { id: "hour", label: "Hour" },
                            { id: "week", label: "Week" },
                            { id: "month", label: "Month" },
                            { id: "year", label: "Year" },
                        ],
                    },
                    {
                        type: "select",
                        name: "data-pb-range",
                        label: "Period",
                        options: [
                            { id: "", label: "All the records" },
                            { id: "24h", label: "Last 24 hours" },
                            { id: "7d", label: "Last 7 days" },
                            { id: "30d", label: "Last 30 days" },
                            { id: "90d", label: "Last 90 days" },
                            { id: "365d", label: "Last 365 days" },
                        ],
                    },
                    { type: "select", name: "data-pb-split", label: "Series by", options: [] },
                    { type: "checkbox", name: "data-pb-stacked", label: "Stacked", valueTrue: "true", valueFalse: "" },
                    { type: "gpp-chart-filter", name: "data-pb-filter", label: "Filter" },
                    { type: "number", name: "data-pb-limit", label: "Max items", min: 1, max: 100, placeholder: "10" },
                    {
                        type: "select",
                        name: "data-pb-order",
                        label: "Order",
                        options: [
                            { id: "", label: "Biggest first" },
                            { id: "label", label: "By name" },
                        ],
                    },
                    { type: "gpp-colors", name: "data-pb-colors", label: "Colors" },
                    { type: "text", name: "data-pb-label", label: "Values", placeholder: "e.g. Orders" },
                    { type: "number", name: "data-pb-height", label: "Height", min: 80, max: 1200, placeholder: "260 (px)" },
                    {
                        type: "select",
                        name: "data-pb-access",
                        label: "Data access",
                        options: [
                            { id: "rules", label: "Visitor's API rules" },
                            { id: "", label: "Everyone (page gate)" },
                        ],
                    },
                ],
            },
            init() {
                this.on("change:attributes", rerender);
                this.on("change:attributes:data-pb-collection", () => refreshChartTraits(this, ctx));
            },
        },
        view,
    });

    dc.addType("pb-calendar", {
        isComponent: (el) => has(el, "data-pb-calendar"),
        model: {
            defaults: {
                name: "Calendar",
                icon: '<i class="ri-calendar-line"></i>',
                droppable: false,
                components: [],
                traits: [
                    {
                        type: "select",
                        name: "data-pb-calendar",
                        label: "Select",
                        options: [
                            { id: "single", label: "One date" },
                            { id: "range", label: "A date range" },
                            { id: "multiple", label: "Several dates" },
                        ],
                    },
                    { type: "checkbox", name: "data-pb-picker", label: "Picker", valueTrue: "true", valueFalse: "false" },
                    { type: "text", name: "data-pb-name", label: "Name", placeholder: "date (range: date_start, date_end)" },
                    { type: "checkbox", name: "data-pb-required", label: "Required", valueTrue: "true", valueFalse: "" },
                    { type: "checkbox", name: "data-pb-time", label: "With time", valueTrue: "true", valueFalse: "" },
                    {
                        type: "select",
                        name: "data-pb-months",
                        label: "Months",
                        options: [
                            { id: "", label: "One" },
                            { id: "2", label: "Two" },
                        ],
                    },
                    {
                        type: "select",
                        name: "data-pb-captions",
                        label: "Title",
                        options: [
                            { id: "", label: "Month year" },
                            { id: "dropdown", label: "Month / year lists" },
                        ],
                    },
                    { type: "text", name: "data-pb-min", label: "Earliest", placeholder: "YYYY-MM-DD or today" },
                    { type: "text", name: "data-pb-max", label: "Latest", placeholder: "YYYY-MM-DD or today" },
                    {
                        type: "select",
                        name: "data-pb-disabled-days",
                        label: "Unavailable",
                        options: [
                            { id: "", label: "None" },
                            { id: "0,6", label: "Weekends" },
                            { id: "0", label: "Sundays" },
                            { id: "1,2,3,4,5", label: "Weekdays" },
                        ],
                    },
                    {
                        type: "select",
                        name: "data-pb-week-start",
                        label: "Week starts",
                        options: [
                            { id: "", label: "Visitor's locale" },
                            { id: "0", label: "Sunday" },
                            { id: "1", label: "Monday" },
                            { id: "6", label: "Saturday" },
                        ],
                    },
                    { type: "text", name: "data-pb-placeholder", label: "Placeholder", placeholder: "Pick a date" },
                ],
            },
            init() {
                this.on("change:attributes", rerender);
            },
        },
        view,
    });
}

/**
 * The "Colors" setting of a chart: the theme's chart colors, a palette, the
 * primary color's shades or custom colors (data-pb-colors).
 */
function defineColorsTrait(ed) {
    ed.Traits.addType("gpp-colors", {
        createInput({ component }) {
            const el = document.createElement("div");
            el.className = "gpp-chart-colors";
            el.innerHTML = `<select></select><span class="gpp-swatches"></span><div class="gpp-custom-colors" hidden></div>`;
            const sel = el.querySelector("select");
            const set = (v) => (v ? component.addAttributes({ "data-pb-colors": v }) : component.removeAttributes("data-pb-colors"));
            sel.addEventListener("change", () => {
                const v = sel.value;
                if (v == "custom") {
                    const cur = colorsOf(component.getAttributes()["data-pb-colors"], el.palettes || [])?.light || [];
                    set("custom:" + [0, 1, 2, 3, 4].map((i) => (/^#[0-9a-f]{6}$/i.test(cur[i] || "") ? cur[i] : ["#2563eb", "#16a34a", "#f59e0b", "#dc2626", "#7c3aed"][i])).join(","));
                } else {
                    set(v);
                }
                this.onUpdate({ elInput: el, component });
            });
            el.querySelector(".gpp-custom-colors").addEventListener("input", (e) => {
                const inputs = [...el.querySelectorAll(".gpp-custom-colors input")];
                set("custom:" + inputs.map((i) => i.value).join(","));
                el.querySelector(".gpp-swatches").innerHTML = inputs.map((i) => `<i style="background:${i.value}"></i>`).join("");
            });
            loadPalettes().then((list) => {
                el.palettes = list;
                this.onUpdate({ elInput: el, component });
            });
            return el;
        },
        onUpdate({ elInput, component }) {
            const v = component.getAttributes()["data-pb-colors"] || "";
            const palettes = elInput.palettes || [];
            const sel = elInput.querySelector("select");
            const options = [["", "Theme's chart colors"], ...palettes.map((p) => [p.name, paletteLabel(p.name)]), ["primary", "Primary color shades"], ["custom", "Custom…"]];
            sel.innerHTML = options.map(([id, label]) => `<option value="${id}">${label}</option>`).join("");
            sel.value = v.startsWith("custom:") ? "custom" : v;
            const colors = colorsOf(v, palettes)?.light;
            elInput.querySelector(".gpp-swatches").innerHTML = colors ? colors.map((c) => `<i style="background:${c.replace(/[<>"]/g, "")}"></i>`).join("") : "";
            const custom = elInput.querySelector(".gpp-custom-colors");
            custom.hidden = !v.startsWith("custom:");
            if (!custom.hidden && document.activeElement?.parentElement != custom) {
                custom.innerHTML = (colors || []).map((c, i) => `<input type="color" value="${/^#[0-9a-f]{6}$/i.test(c) ? c : "#888888"}" aria-label="Color ${i + 1}">`).join("");
            }
        },
    });
}

/** The field options of a chart's settings (its collection's fields). */
export function refreshChartTraits(comp, ctx) {
    if (!comp?.is?.("pb-chart")) {
        return;
    }
    const col = ctx.byName(comp.getAttributes()["data-pb-collection"]);
    const fields = (col?.fields || []).filter((f) => !f.hidden && f.type != "password" && f.name != "tokenKey");
    const opts = (empty, types) => [{ id: "", label: empty }, ...fields.filter((f) => types.includes(f.type)).map((f) => ({ id: f.name, label: `${f.name} (${f.type})` }))];
    const set = (name, options) => comp.getTrait(name)?.set("options", options);
    set("data-pb-collection", ctx.collectionOptions());
    set("data-pb-value", opts(col ? "- None (count) -" : "- Select a collection -", ["number", "date", "autodate", ...groupable]));
    set("data-pb-group", opts("- Field -", groupable));
    set("data-pb-date", opts("- No -", ["date", "autodate"]));
    set("data-pb-split", opts("- One series -", groupable.filter((t) => t != "date" && t != "autodate")));
}

// ---- canvas preview ----------------------------------------------------------------------------

const cache = new Map();

const markup = (model) => model.toHTML().replace(/\sid="[^"]*"/, "");

function renderSnippet(ed, ctx, model, el) {
    const attrs = model.getAttributes();
    if (model.is("pb-chart") && !attrs["data-pb-collection"]) {
        el.innerHTML =
            '<div style="padding:28px 16px;border:1px dashed #a1a1aa;border-radius:8px;text-align:center;font:13px system-ui,sans-serif;color:#71717a">' +
            "Chart: choose a collection in its settings.</div>";
        return;
    }
    // the element's settings (not its id, added by the editor when it's styled)
    const html = markup(model);
    const key = (ctx.viewAs() || "") + "|" + html;
    const show = (out) => {
        if (el.dataset.gppKey != key) {
            el.innerHTML = out;
            el.dataset.gppKey = key;
            el.ownerDocument.defaultView.gpUI?.init(el);
        }
    };
    if (cache.has(key)) {
        const v = cache.get(key);
        if (typeof v == "string") {
            show(v);
            return;
        }
    } else {
        cache.set(
            key,
            api(`/api/pages/${ctx.pageId}/snippet`, { method: "POST", body: { html, as: ctx.viewAs() || "" }, requestKey: null })
                .then((r) => {
                    cache.set(key, r.html);
                    return r.html;
                })
                .catch((err) => {
                    cache.delete(key);
                    return `<p style="padding:8px;color:#b91c1c;font:13px system-ui,sans-serif">${(err?.message || "Couldn't show the element.").replace(/[<>&]/g, "")}</p>`;
                }),
        );
    }
    if (!el.innerHTML.trim()) {
        el.innerHTML = '<div style="height:120px"></div>'; // its place while it loads
    }
    Promise.resolve(cache.get(key)).then((out) => {
        // the latest settings only (the attributes may have changed meanwhile)
        if ((ctx.viewAs() || "") + "|" + markup(model) == key) {
            show(out);
        } else {
            renderSnippet(ed, ctx, model, el); // changed meanwhile
        }
    });
}

/** Renders the charts again (the "View as" visitor changed). */
export function refreshWidgets(ed) {
    ed?.getWrapper()
        ?.find("[data-pb-chart], [data-pb-calendar]")
        .forEach((c) => c.view?.render());
}

// ---- blocks ------------------------------------------------------------------------------------

export function setWidgetBlocks(ed) {
    const bm = ed.BlockManager;
    const media = (name) => `<i class="${name} gpp-block-icon"></i>`;
    const charts = { id: "Charts", label: "Charts", open: false };
    const dates = { id: "Dates", label: "Dates", open: false };
    const chart = (kind) => `<div data-pb-chart="${kind}" data-pb-access="rules"></div>`;
    bm.add("gpp-chart-bar", { category: charts, label: "Bar chart", media: media("ri-bar-chart-2-line"), content: chart("bar") });
    bm.add("gpp-chart-line", { category: charts, label: "Line chart", media: media("ri-line-chart-line"), content: chart("line") });
    bm.add("gpp-chart-area", { category: charts, label: "Area chart", media: media("ri-landscape-line"), content: chart("area") });
    bm.add("gpp-chart-pie", { category: charts, label: "Pie chart", media: media("ri-pie-chart-2-line"), content: chart("pie") });
    bm.add("gpp-chart-donut", { category: charts, label: "Donut chart", media: media("ri-donut-chart-line"), content: chart("donut") });

    bm.add("gpp-date-picker", { category: dates, label: "Date picker", media: media("ri-calendar-event-line"), content: `<div data-pb-calendar="single" data-pb-picker="true" data-pb-name="date"></div>` });
    bm.add("gpp-date-range", { category: dates, label: "Date range picker", media: media("ri-calendar-2-line"), content: `<div data-pb-calendar="range" data-pb-picker="true" data-pb-months="2" data-pb-name="dates"></div>` });
    bm.add("gpp-date-time", { category: dates, label: "Date and time picker", media: media("ri-calendar-schedule-line"), content: `<div data-pb-calendar="single" data-pb-picker="true" data-pb-time="true" data-pb-name="date"></div>` });
    bm.add("gpp-calendar", { category: dates, label: "Calendar", media: media("ri-calendar-line"), content: `<div data-pb-calendar="single" data-pb-name="date"></div>` });
    bm.add("gpp-calendar-multi", { category: dates, label: "Calendar: several dates", media: media("ri-calendar-check-line"), content: `<div data-pb-calendar="multiple" data-pb-name="dates"></div>` });
}
