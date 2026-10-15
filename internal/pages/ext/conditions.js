// Pages: conditions of the elements (data-pb-conditions, see
// internal/pages/conditions.go), like Budibase's: "Hide the element if image
// is empty", "Set the text color to gold if rarity equals Rare". Each rule is
// built with dropdowns (field, comparison, a value input of the field's
// type), checked live on the sample record; a condition can also be a
// formula, and values can use {{ }} parts, for the advanced cases.

import { api, select } from "../pocketbaseplus/lib.js";
import { lookup, valueSummary } from "../pocketbaseplus/bindings.js";
import { canvasValue, previewFormula, propOf, propsFor } from "./formula.js";

const ATTR = "data-pb-conditions";

// comparisons (formula.Check), by kind of value
const OPS = {
    empty: "is empty",
    notEmpty: "is not empty",
    eq: "equals",
    ne: "doesn't equal",
    gt: "is greater than",
    gte: "is at least",
    lt: "is less than",
    lte: "is at most",
    contains: "contains",
    notContains: "doesn't contain",
    startsWith: "starts with",
    endsWith: "ends with",
    isTrue: "is yes",
    isFalse: "is no",
};
const DATE_OPS = { gt: "is after", gte: "is on or after", lt: "is before", lte: "is on or before" };
const LIST_OPS = { contains: "includes", notContains: "doesn't include" };
const NO_VALUE = new Set(["empty", "notEmpty", "isTrue", "isFalse"]);

/** The kind of a field for the comparisons: text, number, date, bool, list, choice. */
function kindOf(f) {
    if (!f) {
        return "any";
    }
    const multiple = (f.maxSelect || 0) > 1;
    switch (f.type) {
        case "number":
            return "number";
        case "date":
        case "autodate":
            return "date";
        case "bool":
            return "bool";
        case "select":
            return multiple ? "list" : "choice";
        case "relation":
        case "file":
            return multiple ? "list" : "text";
        case "json":
            return "list";
    }
    return "text";
}

function opsFor(kind) {
    const pick = (keys, labels = {}) => keys.map((k) => ({ value: k, label: labels[k] || OPS[k] }));
    switch (kind) {
        case "number":
            return pick(["eq", "ne", "gt", "gte", "lt", "lte", "empty", "notEmpty"]);
        case "date":
            return pick(["lt", "gt", "lte", "gte", "eq", "empty", "notEmpty"], DATE_OPS);
        case "bool":
            return pick(["isTrue", "isFalse"]);
        case "list":
            return pick(["contains", "notContains", "empty", "notEmpty"], LIST_OPS);
        case "choice":
            return pick(["eq", "ne", "empty", "notEmpty"]);
        case "text":
            return pick(["eq", "ne", "contains", "notContains", "startsWith", "endsWith", "empty", "notEmpty"]);
    }
    return pick(["eq", "ne", "contains", "notContains", "gt", "lt", "empty", "notEmpty", "isTrue", "isFalse"]);
}

/** The conditions of a component (the saved JSON, [] when none or invalid). */
export function conditionsOf(comp) {
    try {
        const v = JSON.parse(comp.getAttributes()[ATTR] || "[]");
        return Array.isArray(v) ? v : [];
    } catch {
        return [];
    }
}

// the data a condition can test: the record's fields, the visitor's, an
// address parameter or a formula
function fieldChoices(comp, ctx) {
    const col = ctx.collection(comp);
    const out = [];
    if (col) {
        out.push({ value: "record.id", label: "id", kind: "text" });
        for (const f of ctx.typedFields(col)) {
            if (f.path != "id") {
                out.push({ value: "record." + f.path, label: f.label, kind: kindOf(f.field), field: f.field });
            }
        }
    }
    for (const f of ctx.typedUserFields()) {
        out.push({ value: "auth." + f.path, label: "Signed-in visitor › " + f.label, kind: f.path == "id" ? "text" : kindOf(f.field), field: f.field });
    }
    out.push({ value: "query", label: "Page address › a parameter (?name=…)", kind: "any" });
    out.push({ value: "formula", label: "Custom formula…", kind: "formula" });
    return out;
}

const fieldValue = (c) => (c.formula !== undefined ? "formula" : c.field?.startsWith("query.") ? "query" : c.field || "");

/** One line of a rule: "Hide if image is empty". */
export function ruleSentence(rule, comp, ctx) {
    const choices = fieldChoices(comp, ctx);
    const action = rule.action == "show" ? "Show only if" : rule.action == "hide" ? "Hide if" : `Set ${(rule.prop == "data-pb-formula" ? "the text" : propOf(rule.prop)?.label || rule.prop).toLowerCase()} to "${rule.value || ""}" if`;
    const when = (rule.when || []).map((c) => {
        if (c.formula !== undefined) {
            return c.formula || "a formula";
        }
        const ch = choices.find((x) => x.value == c.field);
        const name = ch ? ch.label.replace(/ \(.*\)$/, "") : String(c.field || "?").replace(/^record\./, "");
        const kind = ch?.kind || "any";
        const op = opsFor(kind).find((o) => o.value == c.op)?.label || OPS[c.op] || c.op;
        return `${name} ${op}${NO_VALUE.has(c.op) ? "" : ` ${c.value ?? ""}`}`;
    });
    return when.length ? `${action} ${when.join(rule.match == "any" ? " or " : " and ")}` : action.replace(/ (only )?if$/, "") + " always";
}

/** The "Conditions" setting: the rules in words, and the editor. */
export function defineConditions(ed, ctx) {
    ed.Traits.addType("gpp-conditions", {
        createInput({ component }) {
            const el = document.createElement("div");
            el.className = "gpp-formula-trait gpp-dynamic-trait";
            el.addEventListener("click", (e) => {
                if (e.target.closest("button")) {
                    conditionsModal(component, ctx, () => render(el, component));
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
        const rules = conditionsOf(comp);
        el.replaceChildren(
            ...rules.map((r) => {
                const b = Object.assign(document.createElement("button"), { type: "button", className: "gpp-dynamic-open gpp-cond-sentence", title: "Edit the conditions" });
                b.textContent = ruleSentence(r, comp, ctx);
                return b;
            }),
            Object.assign(document.createElement("button"), {
                type: "button",
                className: "gpp-formula-btn",
                innerHTML: `<i class="ri-git-branch-line"></i> ${rules.length ? "Edit the conditions" : "Add a condition"}`,
                title: "Show, hide or change the element depending on the data",
            }),
        );
    }
}

let uid = 0;

// a CSS color as #rrggbb for the color input ("gold" → #ffd700), "" if not one
function hexColor(v) {
    const probe = document.createElement("span");
    probe.style.color = String(v || "").trim();
    if (!probe.style.color) {
        return "";
    }
    document.body.appendChild(probe);
    const m = getComputedStyle(probe).color.match(/\d+(\.\d+)?/g) || [];
    probe.remove();
    return m.length >= 3 ? "#" + m.slice(0, 3).map((n) => Math.round(+n).toString(16).padStart(2, "0")).join("") : "";
}

/** The conditions editor of a component. */
export function conditionsModal(comp, ctx, onSaved) {
    const col = ctx.collection(comp);
    const tag = String(comp.get("tagName") || "").toLowerCase();
    const textual = (comp.is("text") || comp.is("pb-field") || comp.is("pb-formula")) && tag != "img";
    const choices = fieldChoices(comp, ctx);
    const actions = [
        { value: "show", label: "Show the element only if" },
        { value: "hide", label: "Hide the element if" },
        ...(textual ? [{ value: "set:data-pb-formula", label: "Replace the text if" }] : []),
        ...propsFor(comp).map((p) => ({ value: "set:" + p.key, label: `Set the ${p.label.toLowerCase()} if` })),
    ];

    // plain objects edited in place; a structural change (action, field,
    // comparison, rows) bumps the version to redraw
    const rules = conditionsOf(comp).map((r) => ({ ...r, _id: ++uid, when: (r.when || []).map((c) => ({ ...c, _id: ++uid })) }));
    const data = store({ version: 0, results: null, error: "", sample: null, recId: "" });
    const redraw = () => {
        data.version++;
        check();
    };

    (ctx.sample(comp) || Promise.resolve(null)).then((rec) => {
        data.sample = rec || null;
        data.recId = rec?.id || "";
        check();
    });

    const clean = () =>
        rules.map(({ _id, when, ...r }) => {
            const out = { action: r.action };
            if (r.action == "set") {
                out.prop = r.prop;
                out.value = r.value || "";
            }
            if (r.match == "any" && when.length > 1) {
                out.match = "any";
            }
            out.when = when.map(({ _id, ...c }) => {
                if (c.formula !== undefined) {
                    return { formula: c.formula };
                }
                const cc = { field: c.field || "", op: c.op || "" };
                if (!NO_VALUE.has(c.op) && c.value) {
                    cc.value = c.value;
                }
                return cc;
            });
            return out;
        });

    let timer;
    function check() {
        clearTimeout(timer);
        timer = setTimeout(async () => {
            if (!rules.length) {
                data.results = null;
                return;
            }
            try {
                data.results = await api(`/api/pages/${encodeURIComponent(ctx.pageId)}/conditions`, {
                    method: "POST",
                    body: { conditions: clean(), collection: col?.name || "", record: data.recId, as: ctx.viewAs() },
                    requestKey: null,
                });
                data.error = data.results?.error || "";
            } catch (err) {
                data.error = err?.message || "Failed to check the conditions.";
            }
        }, 300);
    }

    function newCondition() {
        const first = choices.find((c) => c.value.startsWith("record.") && c.value != "record.id") || choices[0];
        return { _id: ++uid, field: first.value, op: opsFor(first.kind)[0].value, value: "" };
    }

    function addRule() {
        rules.push({ _id: ++uid, action: "hide", when: [newCondition()] });
        redraw();
    }

    function save() {
        const out = clean();
        if (out.length) {
            comp.addAttributes({ [ATTR]: JSON.stringify(out) });
        } else {
            comp.removeAttributes([ATTR]);
        }
        app.modals.close(modal);
        ctx.onChange(comp);
        onSaved?.();
    }

    // the sample value of a field: "Pikachu", list · 3 items
    function sampleOf(path) {
        if (!data.sample || !path?.startsWith("record.")) {
            return "";
        }
        const parts = path.split(".");
        if (parts.length > 2) {
            return ""; // a related record's field: not in the sample
        }
        return valueSummary(lookup(data.sample, parts[1])) || "empty";
    }

    // the value input of a condition, for the kind of its field
    function valueInput(c, ch) {
        const kind = ch?.kind || "any";
        if (NO_VALUE.has(c.op)) {
            return t.span({ className: "gpp-cond-novalue" });
        }
        const values = ch?.field?.values;
        if (values?.length && (kind == "choice" || kind == "list") && !String(c.value || "").includes("{{")) {
            return t.div(
                { rid: "v_" + c._id + "_" + c.value, className: "gpp-cond-value" },
                select({
                    options: () => values.map((v) => ({ value: v, label: v })),
                    value: () => c.value || "",
                    onchange: (v) => {
                        c.value = v;
                        redraw();
                    },
                }),
            );
        }
        const input = t.input({
            type: "text",
            value: c.value || "",
            inputMode: kind == "number" ? "decimal" : undefined,
            placeholder: kind == "date" ? "2026-12-31 or {{now}}" : kind == "number" ? "100" : "a value or {{ formula }}",
            oninput: (e) => {
                c.value = e.target.value;
                check();
            },
        });
        if (kind != "date") {
            return t.div({ className: "gpp-cond-value" }, input);
        }
        const quick = (label, v) =>
            t.button(
                {
                    type: "button",
                    className: "btn xs secondary transparent",
                    onclick: () => {
                        c.value = v;
                        redraw();
                    },
                },
                t.span({ className: "txt" }, label),
            );
        return t.div({ className: "gpp-cond-value" }, input, t.div({ className: "gpp-cond-quick" }, quick("Now", "{{now}}"), quick("Today", "{{today}}"), quick("In 7 days", "{{addDays 7 now}}"), quick("7 days ago", "{{addDays -7 now}}")));
    }

    function conditionRow(rule, c, i) {
        const which = fieldValue(c);
        const ch = choices.find((x) => x.value == (which == "query" ? "query" : c.field));
        const removeBtn = t.button(
            {
                type: "button",
                className: "btn sm circle transparent secondary",
                ariaLabel: app.attrs.tooltip("Remove this condition"),
                onclick: () => {
                    rule.when.splice(i, 1);
                    redraw();
                },
            },
            t.i({ className: "ri-close-line", ariaHidden: true }),
        );
        const fieldSelect = t.div(
            { className: "gpp-cond-field" },
            select({
                options: () => choices.map((x) => ({ value: x.value, label: x.label })),
                value: () => which,
                onchange: (v) => {
                    if (!v) {
                        return redraw(); // re-picked: keep it
                    }
                    const next = choices.find((x) => x.value == v);
                    if (v == "formula") {
                        delete c.field;
                        delete c.op;
                        delete c.value;
                        c.formula = "";
                    } else {
                        delete c.formula;
                        c.field = v == "query" ? "query.q" : v;
                        const ops = opsFor(next?.kind || "any");
                        if (!ops.some((o) => o.value == c.op)) {
                            c.op = ops[0].value;
                        }
                    }
                    redraw();
                },
            }),
        );
        const join = i == 0 ? "" : rule.match == "any" ? "or" : "and";
        const label = t.span({ className: "gpp-cond-join" }, join);
        if (c.formula !== undefined) {
            return t.div(
                { rid: "c_" + c._id + "_" + data.version, className: "gpp-cond-row formula" },
                label,
                fieldSelect,
                t.textarea({
                    className: "txt-code",
                    rows: 2,
                    spellcheck: false,
                    value: c.formula,
                    placeholder: "{{and (gt record.hp 100) (eq record.type \"Fire\")}}",
                    oninput: (e) => {
                        c.formula = e.target.value;
                        check();
                    },
                }),
                removeBtn,
            );
        }
        const sample = sampleOf(c.field);
        return t.div(
            { rid: "c_" + c._id + "_" + data.version, className: "gpp-cond-row" },
            label,
            fieldSelect,
            which == "query"
                ? t.input({
                      type: "text",
                      className: "gpp-cond-param",
                      value: c.field.slice("query.".length),
                      placeholder: "parameter",
                      title: "The name of the parameter: ?q=… is q",
                      oninput: (e) => {
                          c.field = "query." + (e.target.value.replace(/[^A-Za-z0-9_]/g, "") || "q");
                          check();
                      },
                  })
                : null,
            t.div(
                { className: "gpp-cond-op" },
                select({
                    options: () => opsFor(ch?.kind || "any"),
                    value: () => c.op,
                    onchange: (v) => {
                        c.op = v || c.op;
                        redraw();
                    },
                }),
            ),
            valueInput(c, ch),
            removeBtn,
            sample ? t.div({ className: "gpp-cond-sample" }, "Sample: ", sample) : null,
        );
    }

    function setValueInput(rule) {
        const p = rule.prop == "data-pb-formula" ? { kind: "text", example: "Sold out" } : propOf(rule.prop) || {};
        const input = t.input({
            type: "text",
            value: rule.value || "",
            placeholder: p.kind == "color" ? "gold, #f59e0b" : (p.example || "").replace(/\{\{if .*?\}\}(.*?)\{\{else\}\}.*?\{\{end\}\}/, "$1") || "a value",
            oninput: (e) => {
                rule.value = e.target.value;
                const hex = p.kind == "color" && hexColor(rule.value);
                const swatch = input.parentNode?.querySelector(".gpp-cond-color");
                if (hex && swatch) {
                    swatch.value = hex;
                }
                check();
            },
        });
        return t.div(
            { className: "gpp-cond-set" },
            t.span({ className: "gpp-cond-join" }, "to"),
            p.kind == "color"
                ? t.input({
                      type: "color",
                      className: "gpp-cond-color",
                      value: hexColor(rule.value) || "#f59e0b",
                      title: "Pick a color",
                      oninput: (e) => {
                          rule.value = e.target.value;
                          input.value = e.target.value;
                          check();
                      },
                  })
                : null,
            input,
        );
    }

    function ruleCard(rule, i) {
        const result = () => data.results?.rules?.[i];
        const move = (d) => {
            const [r] = rules.splice(i, 1);
            rules.splice(i + d, 0, r);
            redraw();
        };
        return t.div(
            { rid: "r_" + rule._id + "_" + data.version, className: "gpp-cond-rule" },
            t.div(
                { className: "gpp-cond-head" },
                t.div(
                    { className: "gpp-cond-action" },
                    select({
                        options: () => actions,
                        value: () => (rule.action == "set" ? "set:" + rule.prop : rule.action),
                        onchange: (v) => {
                            if (!v) {
                                return redraw();
                            }
                            if (v.startsWith("set:")) {
                                rule.action = "set";
                                rule.prop = v.slice(4);
                            } else {
                                rule.action = v;
                                delete rule.prop;
                                delete rule.value;
                            }
                            redraw();
                        },
                    }),
                ),
                rule.when.length > 1
                    ? t.div(
                          { className: "gpp-cond-match" },
                          select({
                              options: () => [
                                  { value: "all", label: "all of these" },
                                  { value: "any", label: "any of these" },
                              ],
                              value: () => (rule.match == "any" ? "any" : "all"),
                              onchange: (v) => {
                                  rule.match = v || rule.match;
                                  redraw();
                              },
                          }),
                      )
                    : null,
                t.span({ className: "gpp-cond-status" }, () => {
                    const r = result();
                    if (!r) {
                        return null;
                    }
                    if (r.error) {
                        return t.span({ rid: "e" + r.error, className: "txt-danger", title: r.error }, t.i({ className: "ri-error-warning-line", ariaHidden: true }), " " + r.error);
                    }
                    return r.matched
                        ? t.span({ rid: "y", className: "gpp-cond-yes", title: "On the sample record" }, t.i({ className: "ri-checkbox-circle-line", ariaHidden: true }), " true")
                        : t.span({ rid: "n", className: "gpp-cond-no", title: "On the sample record" }, t.i({ className: "ri-close-circle-line", ariaHidden: true }), " false");
                }),
                t.div(
                    { className: "gpp-cond-tools" },
                    i > 0 ? t.button({ type: "button", className: "btn sm circle transparent secondary", ariaLabel: app.attrs.tooltip("Move up"), onclick: () => move(-1) }, t.i({ className: "ri-arrow-up-line", ariaHidden: true })) : null,
                    i < rules.length - 1 ? t.button({ type: "button", className: "btn sm circle transparent secondary", ariaLabel: app.attrs.tooltip("Move down"), onclick: () => move(1) }, t.i({ className: "ri-arrow-down-line", ariaHidden: true })) : null,
                    t.button(
                        {
                            type: "button",
                            className: "btn sm circle transparent danger",
                            ariaLabel: app.attrs.tooltip("Remove the rule"),
                            onclick: () => {
                                rules.splice(i, 1);
                                redraw();
                            },
                        },
                        t.i({ className: "ri-delete-bin-line", ariaHidden: true }),
                    ),
                ),
            ),
            rule.action == "set" ? setValueInput(rule) : null,
            ...rule.when.map((c, j) => conditionRow(rule, c, j)),
            t.button(
                {
                    type: "button",
                    className: "btn xs secondary transparent gpp-cond-add",
                    onclick: () => {
                        rule.when.push(newCondition());
                        redraw();
                    },
                },
                t.i({ className: "ri-add-line", ariaHidden: true }),
                t.span({ className: "txt" }, rule.when.length ? "And / or another condition" : "Add a condition (else: always)"),
            ),
        );
    }

    const modal = t.div(
        { className: "modal popup lg gpp-formula-modal gpp-cond-modal" },
        t.header({ className: "modal-header" }, t.h5({ className: "m-auto" }, "Conditions")),
        t.div(
            { className: "modal-content" },
            t.p(
                { className: "txt-hint m-t-0" },
                "Show, hide or change this element depending on the data, checked for each visit",
                col ? " and each record" : "",
                ". A hidden element isn't sent to the visitor. The rules apply in order (the last matching value wins); ",
                t.em(null, "true / false"),
                " is the result on the sample record.",
            ),
            () => (data.error ? t.div({ rid: "err" + data.error, className: "alert alert-danger m-b-sm" }, data.error) : null),
            () => {
                const r = data.results;
                if (!r || r.error || !rules.some((x) => x.action != "set")) {
                    return null;
                }
                return t.div(
                    { rid: "vis" + r.visible, className: "gpp-cond-summary" },
                    t.i({ className: r.visible ? "ri-eye-line" : "ri-eye-off-line", ariaHidden: true }),
                    r.visible ? " Shown on the sample record" : " Hidden on the sample record (dimmed in the canvas)",
                );
            },
            t.div({ className: "gpp-cond-rules" }, () => {
                data.version;
                return rules.length
                    ? rules.map(ruleCard)
                    : t.div({ rid: "none_" + data.version, className: "gpp-cond-empty txt-hint" }, "No condition yet: the element is always shown as designed.");
            }),
            t.div(
                { className: "gpp-cond-presets" },
                t.button({ type: "button", className: "btn sm secondary", onclick: addRule }, t.i({ className: "ri-add-line", ariaHidden: true }), t.span({ className: "txt" }, "Add a rule")),
            ),
        ),
        t.footer(
            { className: "modal-footer" },
            t.button({ type: "button", className: "btn transparent secondary m-l-auto", onclick: () => app.modals.close(modal) }, t.span({ className: "txt" }, "Cancel")),
            t.button({ type: "button", className: "btn expanded", onclick: save }, t.span({ className: "txt" }, "Save")),
        ),
    );
    modal.onafterclose = () => {
        clearTimeout(timer);
        modal.remove();
    };
    document.body.appendChild(modal);
    app.modals.open(modal);
    if (!rules.length) {
        addRule();
    } else {
        check();
    }
}

/**
 * Shows the conditions on the canvas element of a component: dimmed when
 * hidden on the sample record, the "set" values applied (not the text).
 */
export async function applyConditions(pageId, comp, col, recId, as) {
    const el = comp.view?.el;
    if (!el) {
        return;
    }
    for (const c of el._gppCondClasses || []) {
        el.classList.remove(c);
    }
    el._gppCondClasses = [];
    el.classList.remove("gpp-cond-hidden");
    const rules = conditionsOf(comp);
    if (!rules.length) {
        return;
    }
    let r;
    try {
        r = await api(`/api/pages/${encodeURIComponent(pageId)}/conditions`, { method: "POST", body: { conditions: rules, collection: col?.name || "", record: recId || "", as }, requestKey: null });
    } catch {
        return;
    }
    if (r.error) {
        return;
    }
    el.classList.toggle("gpp-cond-hidden", !r.visible);
    for (const { prop, value } of r.sets || []) {
        const v = value.includes("{{") ? (await previewFormula(pageId, value, col, recId, as)).text : value;
        if (v?.trim()) {
            el._gppCondClasses.push(...canvasValue(el, prop, v.trim()));
        }
    }
}
