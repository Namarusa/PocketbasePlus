// Pages: formulas, computed texts of the elements (data-pb-formula, see
// internal/pages/formula.go): {{formatDate "DD/MM/YYYY" record.created}},
// {{currency "EUR" (multiply record.price record.qty)}}... The "Formula"
// setting opens an editor with the record's fields, the functions and a live
// preview on the sample record; the canvas shows the computed sample.

import { api } from "../pocketbaseplus/lib.js";
import { actionAt, functionList, functionSnippet, insertAt } from "../pocketbaseplus/functions.js";

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

/** The formula editor of a component. */
export function formulaModal(ed, comp, ctx) {
    const col = ctx.collection(comp);
    const data = store({
        formula: comp.getAttributes()["data-pb-formula"] || "",
        tab: "data",
        search: "",
        category: "",
        result: null,
        loading: false,
        recId: "",
    });
    const existing = !!data.formula;
    let input;

    ctx.sampleId(comp).then((id) => {
        data.recId = id || "";
        refresh();
    });

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

    function save(remove = false) {
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
    }

    const dataGroups = () => {
        const groups = [];
        if (col) {
            groups.push({ name: `Record (${col.name})`, icon: "ri-database-2-line", items: [{ path: "record.id", label: "id" }, ...ctx.fields(col).map((f) => ({ path: "record." + f.path, label: f.label }))] });
        }
        groups.push({ name: "Signed-in visitor (empty for guests)", icon: "ri-user-line", items: ctx.userFields().map((f) => ({ path: "auth." + f.path, label: f.label })) });
        groups.push({ name: "Page address", icon: "ri-link", items: [{ path: "query.q", label: "a parameter of the address (?q=...): rename q" }] });
        return groups;
    };

    const tabButton = (key, label, icon) =>
        t.button({ type: "button", className: () => `gp-tab-btn ${data.tab == key ? "active" : ""}`, onmousedown: (e) => e.preventDefault(), onclick: () => (data.tab = key) }, t.i({ className: icon, ariaHidden: true }), " " + label);

    const modal = t.div(
        { className: "modal popup lg gpp-formula-modal" },
        t.header({ className: "modal-header" }, t.h5({ className: "m-auto" }, "Formula")),
        t.div(
            { className: "modal-content" },
            t.p(
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
                    placeholder: `{{currency "EUR" (multiply record.price record.qty)}}`,
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
                    return data.result?.text === "" ? t.span({ className: "txt-hint" }, "(empty)") : data.result?.text ?? "";
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
                                          { type: "button", className: "gp-picker-item", onmousedown: (e) => e.preventDefault(), onclick: () => insertData(i.path) },
                                          t.span({ className: "txt" }, i.label),
                                          t.code(null, i.path),
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
                ? t.button({ type: "button", className: "btn transparent danger", onclick: () => save(true) }, t.span({ className: "txt" }, "Remove the formula"))
                : null,
            t.button({ type: "button", className: "btn transparent secondary m-l-auto", onclick: () => app.modals.close(modal) }, t.span({ className: "txt" }, "Cancel")),
            t.button({ type: "button", className: "btn expanded", disabled: () => !data.formula.trim(), onclick: () => save() }, t.span({ className: "txt" }, "Save")),
        ),
    );
    modal.onafterclose = () => {
        clearTimeout(timer);
        modal.remove();
    };
    document.body.appendChild(modal);
    app.modals.open(modal);
}

