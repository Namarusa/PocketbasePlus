// Pages: formulas, computed texts of the elements (data-pb-formula, see
// internal/pages/formula.go): {{formatDate "DD/MM/YYYY" record.created}},
// {{currency "EUR" (multiply record.price record.qty)}}... The "Formula"
// setting opens an editor with the record's fields, the functions and a live
// preview on the sample record; the canvas shows the computed sample.
// Dynamic values are formulas of an attribute or a style of any element
// (data-pb-attr-src="{{record.image}}/high.webp", data-pb-style-color=...):
// the "Dynamic values" setting lists them and opens the same editor.

import { api, select } from "../pocketbaseplus/lib.js";
import { actionAt, functionList, functionSnippet, insertAt } from "../pocketbaseplus/functions.js";
import { lookup, valueSummary } from "../pocketbaseplus/bindings.js";

/**
 * The properties a formula can set (internal/pages/formula.go: dynamicAttrs
 * and dynamicStyles); tags: only for these elements.
 */
export const dynamicProps = [
    { key: "data-pb-attr-src", label: "Image address", tags: ["img", "video", "audio", "source", "iframe"], kind: "url", example: "{{if record.image}}{{record.image}}/high.webp{{end}}" },
    { key: "data-pb-attr-href", label: "Link address", tags: ["a"], kind: "url", example: "/search?q={{urlquery record.name}}" },
    { key: "data-pb-attr-alt", label: "Alternative text", tags: ["img"], example: "Photo of {{record.name}}" },
    { key: "data-pb-attr-title", label: "Tooltip", example: "Updated {{timeAgo record.updated}}" },
    { key: "data-pb-attr-class", label: "Extra CSS classes", example: "status-{{lower record.status}}" },
    { key: "data-pb-attr-placeholder", label: "Placeholder", tags: ["input", "textarea"], example: "{{record.name}}" },
    { key: "data-pb-attr-value", label: "Value", tags: ["input", "textarea", "button", "option"], example: "{{record.id}}" },
    { key: "data-pb-attr-aria-label", label: "Label for screen readers", example: "{{record.name}}" },
    { key: "data-pb-style-color", label: "Text color", kind: "color", example: `{{if gt record.stock 0}}green{{else}}red{{end}}` },
    { key: "data-pb-style-background-color", label: "Background color", kind: "color", example: "{{record.color}}" },
    { key: "data-pb-style-border-color", label: "Border color", kind: "color", example: "{{record.color}}" },
    { key: "data-pb-style-width", label: "Width", example: "{{record.progress}}%" },
    { key: "data-pb-style-height", label: "Height", example: "{{record.size}}px" },
    { key: "data-pb-style-max-width", label: "Max width", example: "{{record.size}}px" },
    { key: "data-pb-style-min-height", label: "Min height", example: "{{record.size}}px" },
    { key: "data-pb-style-font-size", label: "Text size", example: "{{record.size}}px" },
    { key: "data-pb-style-font-weight", label: "Text weight", example: `{{if record.featured}}700{{else}}400{{end}}` },
    { key: "data-pb-style-opacity", label: "Opacity", example: `{{if record.archived}}0.5{{else}}1{{end}}` },
    { key: "data-pb-style-display", label: "Display (none: hidden)", example: `{{if not record.published}}none{{end}}` },
    { key: "data-pb-style-text-align", label: "Text alignment", example: "{{record.align}}" },
    { key: "data-pb-style-padding", label: "Padding", example: "{{record.space}}px" },
    { key: "data-pb-style-margin", label: "Margin", example: "{{record.space}}px" },
    { key: "data-pb-style-border-radius", label: "Corner radius", example: "{{record.radius}}px" },
    { key: "data-pb-style-border-width", label: "Border width", example: "{{record.border}}px" },
    { key: "data-pb-style-gap", label: "Gap", example: "{{record.space}}px" },
    { key: "data-pb-style-transform", label: "Transform", example: "rotate({{record.angle}}deg)" },
];

export const propOf = (key) => dynamicProps.find((p) => p.key == key);

/** The dynamic values of a component: [{ key, label, formula }]. */
export function dynamicValues(comp) {
    const attrs = comp.getAttributes();
    return Object.keys(attrs)
        .filter((k) => propOf(k))
        .map((k) => ({ key: k, label: propOf(k).label, formula: attrs[k] }));
}

/** The properties a component can have (its tag's ones first). */
export function propsFor(comp) {
    const tag = String(comp.get("tagName") || "div").toLowerCase();
    return dynamicProps.filter((p) => !p.tags || p.tags.includes(tag));
}

/**
 * Applies the computed dynamic values to the canvas element of a component
 * (the server sets the real ones; the saved HTML keeps its own values).
 */
export async function applyDynamic(pageId, comp, col, recId, as) {
    const el = comp.view?.el;
    if (!el) {
        return;
    }
    for (const c of el._gppDynClasses || []) {
        el.classList.remove(c);
    }
    el._gppDynClasses = [];
    for (const { key, formula } of dynamicValues(comp)) {
        if (!formula.trim()) {
            continue;
        }
        const r = await previewFormula(pageId, formula, col, recId, as);
        const v = (r.text ?? "").trim();
        if (r.error || !v) {
            continue;
        }
        el._gppDynClasses.push(...canvasValue(el, key, v));
    }
}

/**
 * Shows a computed value on a canvas element: a style, an attribute, or
 * classes (returned, to remove them on the next update). The text isn't
 * changed: the text editor would save it.
 */
export function canvasValue(el, key, v) {
    if (key.startsWith("data-pb-style-")) {
        el.style.setProperty(key.slice("data-pb-style-".length), v);
    } else if (key == "data-pb-attr-class") {
        const classes = v.split(/\s+/).filter((c) => /^[\w:-]+$/.test(c));
        el.classList.add(...classes);
        return classes;
    } else if (key.startsWith("data-pb-attr-")) {
        el.setAttribute(key.slice("data-pb-attr-".length), v);
    }
    return [];
}

const previews = new Map();

/** The result of a formula on a record: { text } or { error } (cached). */
export function previewFormula(pageId, formula, col, recId, as = "") {
    const key = [formula, col?.name || "", recId || "", as].join("\u0000");
    if (!previews.has(key)) {
        previews.set(
            key,
            api(`/api/pages/${encodeURIComponent(pageId)}/formula`, {
                method: "POST",
                body: { formula, collection: col?.name || "", record: recId || "", as },
                requestKey: null,
            }).catch((err) => {
                previews.delete(key);
                return { error: err?.message || "Failed to compute the formula." };
            }),
        );
        if (previews.size > 300) {
            previews.delete(previews.keys().next().value);
        }
    }
    return previews.get(key);
}

/** The text a formula element shows in the canvas. */
export async function formulaSample(pageId, formula, col, recId, as) {
    if (!formula) {
        return "ƒ formula";
    }
    const r = await previewFormula(pageId, formula, col, recId, as);
    return r.error ? "⚠ " + r.error : r.text;
}

/**
 * The component type of the formula elements and the "Formula" setting of
 * the texts. ctx: { pageId, collection(comp), sampleId(comp) (a promise),
 * fields(col) [{ path, label }], userFields() [{ path, label }], viewAs(), onChange(comp) }.
 */
export function defineFormula(ed, ctx) {
    ed.DomComponents.addType("pb-formula", {
        isComponent: (el) => el?.nodeType == 1 && el.hasAttribute("data-pb-formula"),
        model: {
            defaults: {
                name: "Formula",
                icon: '<i class="ri-functions"></i>',
                droppable: false,
                editable: false,
                traits: [{ type: "gpp-formula", name: "gpp-formula", label: "Formula" }],
            },
        },
    });

    ed.Traits.addType("gpp-formula", {
        createInput({ component }) {
            const el = document.createElement("div");
            el.className = "gpp-formula-trait";
            el.addEventListener("click", (e) => {
                if (e.target.closest("button")) {
                    formulaModal(ed, component, ctx);
                }
            });
            render(el, component);
            return el;
        },
        onUpdate({ elInput, component }) {
            render(elInput, component);
        },
    });

    ed.Traits.addType("gpp-dynamic", {
        createInput({ component }) {
            const el = document.createElement("div");
            el.className = "gpp-formula-trait gpp-dynamic-trait";
            el.addEventListener("click", (e) => {
                const btn = e.target.closest("button");
                if (!btn) {
                    return;
                }
                if (btn.dataset.remove) {
                    component.removeAttributes([btn.dataset.remove]);
                    ctx.onChange(component);
                    renderDynamic(el, component);
                } else {
                    formulaModal(ed, component, ctx, btn.dataset.key || "new", () => renderDynamic(el, component));
                }
            });
            renderDynamic(el, component);
            return el;
        },
        onUpdate({ elInput, component }) {
            renderDynamic(elInput, component);
        },
    });

    function renderDynamic(el, comp) {
        const rows = dynamicValues(comp).map((v) => {
            const row = document.createElement("div");
            row.className = "gpp-dynamic-row";
            const open = Object.assign(document.createElement("button"), { type: "button", className: "gpp-dynamic-open", title: "Edit the formula" });
            open.dataset.key = v.key;
            open.append(
                Object.assign(document.createElement("span"), { className: "gpp-dynamic-label", textContent: v.label }),
                Object.assign(document.createElement("code"), { className: "gpp-formula-code", textContent: v.formula }),
            );
            const remove = Object.assign(document.createElement("button"), { type: "button", className: "gpp-dynamic-remove", title: "Remove", innerHTML: '<i class="ri-close-line"></i>' });
            remove.dataset.remove = v.key;
            row.append(open, remove);
            return row;
        });
        el.replaceChildren(
            ...rows,
            Object.assign(document.createElement("button"), {
                type: "button",
                className: "gpp-formula-btn",
                innerHTML: '<i class="ri-functions"></i> Add a dynamic value',
                title: "Compute an attribute or a style from the data: image address, link, color, size...",
            }),
        );
    }

    function render(el, comp) {
        const f = comp.getAttributes()["data-pb-formula"] || "";
        el.replaceChildren(
            ...(f ? [Object.assign(document.createElement("code"), { className: "gpp-formula-code", textContent: f })] : []),
            Object.assign(document.createElement("button"), {
                type: "button",
                className: "gpp-formula-btn",
                innerHTML: `<i class="ri-functions"></i> ${f ? "Edit the formula" : "Add a formula"}`,
            }),
        );
    }
}

/**
 * The formula editor of a component: its text (key "data-pb-formula"), a
 * dynamic value (key "data-pb-attr-src"...) or a new dynamic value ("new":
 * the property is chosen in the editor). onSaved: called after a change.
 */
export function formulaModal(ed, comp, ctx, key = "data-pb-formula", onSaved = null) {
    const col = ctx.collection(comp);
    const isNew = key == "new";
    const isText = key == "data-pb-formula";
    const choices = isNew ? propsFor(comp).filter((p) => comp.getAttributes()[p.key] === undefined) : [];
    const data = store({
        key: isNew ? choices[0]?.key || "" : key,
        formula: isNew ? "" : comp.getAttributes()[key] || "",
        sample: null,
        tab: "data",
        search: "",
        category: "",
        result: null,
        loading: false,
        recId: "",
    });
    const existing = !!data.formula;
    let input;

    (ctx.sample ? ctx.sample(comp) : ctx.sampleId(comp).then((id) => (id ? { id } : null))).then((rec) => {
        data.recId = rec?.id || "";
        data.sample = rec || null;
        refresh();
    });
    const prop = () => propOf(data.key);

    let timer;
    function refresh() {
        clearTimeout(timer);
        timer = setTimeout(async () => {
            if (!data.formula.trim()) {
                data.result = null;
                return;
            }
            data.loading = true;
            data.result = await previewFormula(ctx.pageId, data.formula, col, data.recId, ctx.viewAs());
            data.loading = false;
        }, 300);
    }

    function insertData(path) {
        const inAction = actionAt(input.value, input.selectionStart ?? input.value.length) != null;
        insertAt(input, inAction ? path : `{{${path}}}`);
    }

    function insertFn(f) {
        const { text, sel } = functionSnippet(f, input.value, input.selectionStart ?? input.value.length);
        insertAt(input, text, sel);
        // next: a field for the selected argument
        data.tab = "data";
        data.search = "";
    }

    function saveDynamic(remove) {
        const formula = data.formula.trim();
        if (remove || !formula) {
            comp.removeAttributes([data.key]);
        } else {
            comp.addAttributes({ [data.key]: formula });
            // a computed image address replaces the image field
            if (data.key == "data-pb-attr-src" && comp.getAttributes()["data-pb-field"] !== undefined) {
                comp.removeAttributes(["data-pb-field"]);
            }
        }
        app.modals.close(modal);
        ctx.onChange(comp);
        onSaved?.();
    }

    function save(remove = false) {
        if (!isText) {
            saveDynamic(remove);
            return;
        }
        const formula = remove ? "" : data.formula.trim();
        let target = comp;
        if (formula) {
            if (!comp.is("pb-formula")) {
                // a formula element (no inline text editing, no field binding)
                const json = comp.toJSON();
                delete json.components;
                delete json.content;
                json.type = "pb-formula";
                json.attributes = { ...(json.attributes || {}), "data-pb-formula": formula };
                delete json.attributes["data-pb-field"];
                [target] = comp.replaceWith(json);
            } else {
                target.addAttributes({ "data-pb-formula": formula });
            }
        } else if (comp.is("pb-formula")) {
            const json = comp.toJSON();
            json.type = "text";
            json.attributes = { ...(json.attributes || {}) };
            delete json.attributes["data-pb-formula"];
            json.components = [{ type: "textnode", content: "Text" }];
            [target] = comp.replaceWith(json);
        }
        app.modals.close(modal);
        if (target) {
            ctx.onChange(target);
            ed.select(target);
        }
        onSaved?.();
    }

    // the value of a field in the sample record ("record.name"), for the data list
    function sampleValue(path) {
        if (!path.startsWith("record.") || !data.sample) {
            return "";
        }
        const v = lookup(data.sample, path.slice("record.".length));
        return Array.isArray(v) && path.split(".").length > 2 ? "" : valueSummary(v);
    }

    // the result as it shows: an image, a color...
    function resultExtra(text) {
        const p = prop();
        const v = String(text || "").trim();
        if (!v || !p) {
            return null;
        }
        if (p.kind == "url" && data.key == "data-pb-attr-src" && /^(https?:|\/)/i.test(v)) {
            return t.img({ rid: "img_" + v, className: "gpp-formula-thumb", src: v, alt: "", onerror: (e) => (e.target.replaceWith(Object.assign(document.createElement("span"), { className: "txt-hint txt-sm", textContent: "The image doesn't load: check the address." }))) });
        }
        if (p.kind == "color") {
            return t.span({ rid: "color_" + v, className: "gpp-formula-swatch", style: "background:" + v.replace(/[;"<>]/g, "") });
        }
        return null;
    }

    const dataGroups = () => {
        const groups = [];
        if (col) {
            groups.push({ name: `Record (${col.name})`, icon: "ri-database-2-line", items: [{ path: "record.id", label: "id" }, ...ctx.fields(col).filter((f) => f.path != "id").map((f) => ({ path: "record." + f.path, label: f.label }))] });
        }
        groups.push({ name: "Signed-in visitor (empty for guests)", icon: "ri-user-line", items: ctx.userFields().map((f) => ({ path: "auth." + f.path, label: f.label })) });
        groups.push({ name: "Page address", icon: "ri-link", items: [{ path: "query.q", label: "a parameter of the address (?q=...): rename q" }] });
        if (ctx.isErrorPage?.()) {
            groups.unshift({ name: "The error", icon: "ri-error-warning-line", items: [
                { path: "error.code", label: "code (404, 401, 403, 500)" },
                { path: "error.title", label: "title (Page not found…)" },
                { path: "error.message", label: "message" },
                { path: "error.path", label: "the address asked" },
                { path: "error.signin", label: "the sign in page, back to that address" },
            ] });
        }
        return groups;
    };

    const tabButton = (key, label, icon) =>
        t.button({ type: "button", className: () => `gp-tab-btn ${data.tab == key ? "active" : ""}`, onmousedown: (e) => e.preventDefault(), onclick: () => (data.tab = key) }, t.i({ className: icon, ariaHidden: true }), " " + label);

    const modal = t.div(
        { className: "modal popup lg gpp-formula-modal" },
        t.header({ className: "modal-header" }, t.h5({ className: "m-auto" }, isText ? "Formula" : isNew ? "Add a dynamic value" : prop()?.label || "Dynamic value")),
        t.div(
            { className: "modal-content" },
            isNew
                ? t.div(
                      { className: "field gpp-dynamic-prop" },
                      t.label(null, "Property"),
                      select({ options: () => choices.map((p) => ({ value: p.key, label: p.label })), value: () => data.key, onchange: (v) => (data.key = v) }),
                  )
                : null,
            isText
                ? null
                : t.p(
                      { className: "txt-hint m-t-0" },
                      () => `Computed for each visit${col ? " and each record" : ""}: text with `,
                      t.code(null, "{{ }}"),
                      " parts, e.g. ",
                      t.code(null, () => prop()?.example || ""),
                      () => (prop()?.key.startsWith("data-pb-style-") ? ". An empty result keeps the element's style." : ". An empty result removes it."),
                  ),
            !isText
                ? null
                : t.p(
                { className: "txt-hint m-t-0" },
                "The text of the element, computed for each visit: text with ",
                t.code(null, "{{ }}"),
                " parts, e.g. ",
                t.code(null, `Added {{timeAgo record.created}}`),
                ". Pick fields and functions below; they're inserted at the cursor.",
            ),
            t.div(
                { className: "field gpp-formula-field" },
                t.textarea({
                    className: "txt-code",
                    rows: 4,
                    spellcheck: false,
                    placeholder: () => (isText ? `{{currency "EUR" (multiply record.price record.qty)}}` : prop()?.example || ""),
                    value: () => data.formula,
                    oninput: (e) => {
                        data.formula = e.target.value;
                        refresh();
                    },
                    onmount: (el) => {
                        input = el;
                        setTimeout(() => el.focus(), 50);
                    },
                }),
            ),
            t.div(
                { className: () => `gpp-formula-preview ${data.result?.error ? "error" : ""}` },
                t.span({ className: "gpp-formula-preview-label" }, () => (col ? (data.recId ? "Preview (sample record)" : "Preview (no record yet)") : "Preview")),
                t.div({ className: "gpp-formula-preview-text" }, () => {
                    if (!data.formula.trim()) {
                        return t.span({ className: "txt-hint" }, "Write a formula to see its result.");
                    }
                    if (data.loading && !data.result) {
                        return t.span({ className: "txt-hint" }, "Computing...");
                    }
                    if (data.result?.error) {
                        return data.result.error;
                    }
                    if (data.result?.text === "") {
                        return t.span({ className: "txt-hint" }, "(empty)");
                    }
                    return t.div({ rid: "res_" + data.key + data.result?.text, className: "gpp-formula-result" }, t.span(null, data.result?.text ?? ""), resultExtra(data.result?.text));
                }),
            ),
            t.div({ className: "gp-picker-tabs m-t-sm" }, tabButton("data", "Data", "ri-database-2-line"), tabButton("functions", "Functions", "ri-functions")),
            t.div(
                { className: "field gp-bare" },
                t.input({
                    type: "search",
                    placeholder: () => (data.tab == "functions" ? "Search functions..." : "Search fields..."),
                    value: () => data.search,
                    oninput: (e) => (data.search = e.target.value),
                }),
            ),
            t.div({ className: "gpp-formula-picker" }, () =>
                data.tab == "functions"
                    ? functionList(data, insertFn)
                    : t.div({ className: "gp-picker-list" }, () => {
                          const q = data.search.toLowerCase();
                          const out = [];
                          for (const g of dataGroups()) {
                              const items = g.items.filter((i) => !q || i.path.toLowerCase().includes(q) || i.label.toLowerCase().includes(q));
                              if (!items.length) {
                                  continue;
                              }
                              out.push(
                                  t.div({ className: "gp-picker-title" }, t.i({ className: g.icon, ariaHidden: true }), t.span(null, g.name)),
                                  ...items.map((i) =>
                                      t.button(
                                          { rid: "f_" + i.path + "=" + sampleValue(i.path), type: "button", className: "gp-picker-item", onmousedown: (e) => e.preventDefault(), onclick: () => insertData(i.path) },
                                          t.span({ className: "txt" }, i.label),
                                          sampleValue(i.path) ? t.span({ className: "gp-data-value" }, sampleValue(i.path)) : t.code(null, i.path),
                                      )
                                  ),
                              );
                          }
                          return out.length ? out : t.div({ className: "txt-hint txt-sm p-10" }, "No matching field.");
                      }),
            ),
        ),
        t.footer(
            { className: "modal-footer" },
            existing
                ? t.button({ type: "button", className: "btn transparent danger", onclick: () => save(true) }, t.span({ className: "txt" }, isText ? "Remove the formula" : "Remove"))
                : null,
            t.button({ type: "button", className: "btn transparent secondary m-l-auto", onclick: () => app.modals.close(modal) }, t.span({ className: "txt" }, "Cancel")),
            t.button({ type: "button", className: "btn expanded", disabled: () => !data.formula.trim() || !data.key, onclick: () => save() }, t.span({ className: "txt" }, "Save")),
        ),
    );
    modal.onafterclose = () => {
        clearTimeout(timer);
        modal.remove();
    };
    document.body.appendChild(modal);
    app.modals.open(modal);
}

