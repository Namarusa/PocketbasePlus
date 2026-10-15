// Pages: "Which records" editor of the lists, charts and single item pages.
// The filter and the sort are picked (field, comparison, value; field and
// direction) instead of typed, and stored as the PocketBase filter and sort
// the server already reads (data-pb-filter, data-pb-sort, record_filter):
//
//	published = true && price >= 10 && owner = {{auth.id}}
//	-created,name
//
// A filter the editor can't show as rules (parentheses, "and" mixed with
// "or"...) stays editable as text.

import { select } from "../pocketbaseplus/lib.js";

// --- the rules <-> the filter text ------------------------------------------------------------

const DATE_MACROS = [
    ["@now", "now"],
    ["@todayStart", "today (start)"],
    ["@todayEnd", "today (end)"],
    ["@yesterday", "yesterday"],
    ["@tomorrow", "tomorrow"],
    ["@monthStart", "the start of this month"],
    ["@monthEnd", "the end of this month"],
    ["@yearStart", "the start of this year"],
    ["@yearEnd", "the end of this year"],
];

// the comparisons by kind of field: [value, label, filter operator]
const OPS = {
    text: [["eq", "is", "="], ["ne", "is not", "!="], ["contains", "contains", "~"], ["notContains", "doesn't contain", "!~"]],
    number: [["eq", "=", "="], ["ne", "≠", "!="], ["gt", ">", ">"], ["gte", "≥", ">="], ["lt", "<", "<"], ["lte", "≤", "<="]],
    date: [["gte", "is on or after", ">="], ["gt", "is after", ">"], ["lte", "is on or before", "<="], ["lt", "is before", "<"], ["eq", "is", "="]],
    bool: [["eq", "is", "="]],
    choice: [["eq", "is", "="], ["ne", "is not", "!="]],
    list: [["has", "includes", "?="], ["hasNot", "doesn't include", "!~"]],
    relation: [["eq", "is", "="], ["ne", "is not", "!="]],
};
const EMPTY_OPS = [["empty", "is empty"], ["notEmpty", "is not empty"]];

/** The kind of a field for its comparisons and value input. */
function kindOf(f) {
    if (!f) {
        return "text";
    }
    const multiple = f.maxSelect > 1;
    switch (f.type) {
        case "number":
            return "number";
        case "bool":
            return "bool";
        case "date":
        case "autodate":
            return "date";
        case "select":
            return multiple ? "list" : "choice";
        case "relation":
            return multiple ? "list" : "relation";
        case "file":
            return multiple ? "list" : "text";
    }
    return "text";
}

/** The comparisons of a field: [{ value, label }]. */
export function opsFor(f) {
    const kind = kindOf(f);
    const list = OPS[kind].map(([value, label]) => ({ value, label }));
    return kind == "bool" ? list : [...list, ...EMPTY_OPS.map(([value, label]) => ({ value, label }))];
}

const opSymbol = (f, op) => OPS[kindOf(f)].find((o) => o[0] == op)?.[2];

/** A value of the filter text: a quoted text, a number, true/false, a date macro or a {{placeholder}}. */
function literal(v, f) {
    v = String(v ?? "");
    if (/^\{\{\s*(record|auth|query)\.[A-Za-z0-9_.]+\s*\}\}$/.test(v) || /^@[a-zA-Z]+$/.test(v)) {
        return v;
    }
    const kind = kindOf(f);
    if (kind == "bool") {
        return v == "true" ? "true" : "false";
    }
    if (kind == "number" && v.trim() !== "" && !isNaN(Number(v))) {
        return String(Number(v));
    }
    if (kind == "date" && /^\d{4}-\d{2}-\d{2}$/.test(v)) {
        v += " 00:00:00.000Z";
    }
    return "'" + v.replace(/\\/g, "\\\\").replace(/'/g, "\\'") + "'";
}

/** The filter text of the rules. */
export function buildFilter({ match, rules }, fieldOf) {
    const parts = rules
        .filter((r) => r.field)
        .map((r) => {
            const f = fieldOf(r.field);
            if (r.op == "empty" || r.op == "notEmpty") {
                const none = kindOf(f) == "number" ? "0" : kindOf(f) == "bool" ? "false" : "''";
                return `${r.field} ${r.op == "empty" ? "=" : "!="} ${none}`;
            }
            return `${r.field} ${opSymbol(f, r.op) || "="} ${literal(r.value, f)}`;
        });
    return parts.join(match == "any" ? " || " : " && ");
}

// splits on a separator outside the quotes, the parentheses and the {{ }}
function splitTop(src, sep) {
    const out = [];
    let depth = 0;
    let quote = "";
    let start = 0;
    for (let i = 0; i < src.length; i++) {
        const c = src[i];
        if (quote) {
            if (c == "\\") {
                i++;
            } else if (c == quote) {
                quote = "";
            }
        } else if (c == "'" || c == '"') {
            quote = c;
        } else if (c == "(" || (c == "{" && src[i + 1] == "{")) {
            depth++;
        } else if (c == ")" || (c == "}" && src[i + 1] == "}")) {
            depth--;
        } else if (depth == 0 && src.startsWith(sep, i)) {
            out.push(src.slice(start, i));
            start = i + sep.length;
            i += sep.length - 1;
        }
    }
    out.push(src.slice(start));
    return out;
}

const condRegex = /^([A-Za-z0-9_]+(?:\.[A-Za-z0-9_]+)?)\s*(\?=|\?!=|!=|>=|<=|!~|=|>|<|~)\s*(.+)$/;

function parseValue(raw) {
    raw = raw.trim();
    const q = raw.match(/^'((?:[^'\\]|\\.)*)'$/) || raw.match(/^"((?:[^"\\]|\\.)*)"$/);
    if (q) {
        return q[1].replace(/\\(.)/g, "$1");
    }
    if (/^-?\d+(\.\d+)?$/.test(raw) || raw == "true" || raw == "false" || /^@[a-zA-Z]+$/.test(raw) || /^\{\{\s*(record|auth|query)\.[A-Za-z0-9_.]+\s*\}\}$/.test(raw)) {
        return raw.replace(/\s+/g, "");
    }
    return null;
}

/**
 * The rules of a filter text, or null when it can't be shown as rules (it
 * stays editable as text).
 */
export function parseFilter(src, fieldOf) {
    src = String(src || "").trim();
    if (!src) {
        return { match: "all", rules: [] };
    }
    const ands = splitTop(src, "&&");
    const ors = splitTop(src, "||");
    if (ands.length > 1 && ors.length > 1) {
        return null; // "and" mixed with "or"
    }
    const match = ors.length > 1 ? "any" : "all";
    const rules = [];
    for (const part of match == "any" ? ors : ands) {
        const m = part.trim().match(condRegex);
        if (!m || m[1].startsWith("@")) {
            return null;
        }
        const [, field, symbol, rawValue] = m;
        const value = parseValue(rawValue);
        if (value === null) {
            return null;
        }
        const f = fieldOf(field);
        const kind = kindOf(f);
        if ((value === "" || (value === "0" && kind == "number")) && (symbol == "=" || symbol == "!=") && kind != "bool") {
            rules.push({ field, op: symbol == "=" ? "empty" : "notEmpty", value: "" });
            continue;
        }
        const op = OPS[kind].find((o) => o[2] == symbol)?.[0];
        if (!op) {
            return null;
        }
        rules.push({ field, op, value: kind == "date" ? value.replace(/ 00:00:00\.000Z$/, "") : value });
    }
    return { match, rules };
}

/** The sort of the list: [{ field, desc }] ("@random": random order). */
export function parseSort(src) {
    return String(src || "")
        .split(",")
        .map((s) => s.trim())
        .filter(Boolean)
        .map((s) => (s.startsWith("-") ? { field: s.slice(1), desc: true } : s.startsWith("+") ? { field: s.slice(1), desc: false } : { field: s, desc: false }));
}

export const buildSort = (list) => list.filter((s) => s.field).map((s) => (s.field == "@random" ? "@random" : (s.desc ? "-" : "") + s.field)).join(",");

// --- the fields offered ------------------------------------------------------------------------

/**
 * The fields a filter can use: the collection's own (not hidden), and the
 * fields of its related records ("brand.name"). [{ value, label, field }]
 */
export function filterFields(col, byName) {
    if (!col) {
        return [];
    }
    const out = [];
    for (const f of col.fields || []) {
        if (f.hidden || f.type == "password" || f.type == "json" || f.type == "geoPoint") {
            continue;
        }
        out.push({ value: f.name, label: f.name, field: f });
        if (f.type == "relation" && f.maxSelect <= 1) {
            const rel = byName(f.collectionId);
            for (const rf of rel?.fields || []) {
                if (!rf.hidden && rf.name != "id" && !["password", "json", "geoPoint", "relation", "file"].includes(rf.type)) {
                    out.push({ value: `${f.name}.${rf.name}`, label: `${f.name} → ${rf.name}`, field: rf });
                }
            }
        }
    }
    return out;
}

/** The fields a list can be sorted by. */
export function sortFields(col) {
    return (col?.fields || []).filter((f) => !f.hidden && !["password", "json", "geoPoint", "file", "editor"].includes(f.type) && !(f.maxSelect > 1));
}

const label = (fields, path) => fields.find((x) => x.value == path)?.label || path;

/** A filter in words: "published is yes and price ≥ 10". */
export function filterSentence(src, col, byName) {
    const fields = filterFields(col, byName);
    const fieldOf = (p) => fields.find((x) => x.value == p)?.field;
    const parsed = parseFilter(src, fieldOf);
    if (!parsed) {
        return src;
    }
    return parsed.rules
        .map((r) => {
            const f = fieldOf(r.field);
            const op = opsFor(f).find((o) => o.value == r.op)?.label || r.op;
            if (r.op == "empty" || r.op == "notEmpty") {
                return `${label(fields, r.field)} ${op}`;
            }
            return `${label(fields, r.field)} ${op} ${valueWords(r.value, f)}`;
        })
        .join(parsed.match == "any" ? " or " : " and ");
}

function valueWords(v, f) {
    v = String(v ?? "");
    const m = v.match(/^\{\{\s*(record|auth|query)\.([A-Za-z0-9_.]+)\s*\}\}$/);
    if (m) {
        return { record: `the page record's ${m[2]}`, auth: m[2] == "id" ? "the signed-in visitor" : `the visitor's ${m[2]}`, query: `?${m[2]} of the address` }[m[1]];
    }
    const macro = DATE_MACROS.find(([x]) => x == v);
    if (macro) {
        return macro[1];
    }
    if (f?.type == "bool") {
        return v == "true" ? "yes" : "no";
    }
    return `“${v}”`;
}

/** A sort in words: "price (highest first), then name (A–Z)". */
export function sortSentence(src, col) {
    const list = parseSort(src);
    return list
        .map((s) => {
            if (s.field == "@random") {
                return "random order";
            }
            const f = col?.fields.find((x) => x.name == s.field);
            return `${s.field} (${directionLabel(f, s.desc)})`;
        })
        .join(", then ");
}

function directionLabel(f, desc) {
    switch (f?.type) {
        case "number":
            return desc ? "highest first" : "lowest first";
        case "date":
        case "autodate":
            return desc ? "newest first" : "oldest first";
        case "bool":
            return desc ? "yes first" : "no first";
    }
    return desc ? "Z–A" : "A–Z";
}

// --- the editor ----------------------------------------------------------------------------------

let uid = 0;

/**
 * Opens the "Which records" editor. opts:
 *   collection: the collection object; byName(id or name): a collection
 *   filter, sort: the current texts (sort undefined: no sort section)
 *   record: the collection of the page record ({{record.x}}), or null
 *   auth: the users collection ({{auth.x}})
 *   onSave({ filter, sort })
 */
export function recordsModal({ title = "Which records", collection, byName, filter = "", sort, record = null, auth = null, onSave }) {
    const fields = filterFields(collection, byName);
    const fieldOf = (p) => fields.find((x) => x.value == p)?.field;
    const parsed = parseFilter(filter, fieldOf);
    const data = store({
        version: 0,
        text: parsed ? null : filter, // a filter that can't be shown as rules
        match: parsed?.match || "all",
        count: null,
        names: [],
        countNote: "",
    });
    const rules = (parsed?.rules || []).map((r) => ({ ...r, _id: ++uid }));
    const sorts = sort === undefined ? null : parseSort(sort).map((s) => ({ ...s, _id: ++uid }));
    const redraw = () => {
        data.version++;
        preview();
    };

    const currentFilter = () => (data.text !== null ? data.text.trim() : buildFilter({ match: data.match, rules }, fieldOf));

    // how many records match (before the API rules and the visitor's values)
    let timer;
    function preview() {
        clearTimeout(timer);
        timer = setTimeout(async () => {
            const f = currentFilter();
            if (f.includes("{{")) {
                data.count = null;
                data.countNote = "The records depend on the visitor or the address: they show on the published page.";
                return;
            }
            try {
                const res = await app.pb.collection(collection.name).getList(1, 3, { filter: f, sort: sorts ? buildSort(sorts) || "" : "", requestKey: null, skipTotal: false });
                const labelField = collection.fields.find((x) => x.presentable) || collection.fields.find((x) => ["name", "title", "label"].includes(x.name)) || collection.fields.find((x) => x.type == "text" && x.name != "id");
                data.count = res.totalItems;
                data.names = res.items.map((r) => String(r[labelField?.name || "id"] ?? r.id));
                data.countNote = "";
            } catch (err) {
                data.count = null;
                data.countNote = err?.response?.message || err?.message || "Invalid filter.";
            }
        }, 300);
    }

    function addRule() {
        const first = fields.find((x) => x.field.type == "bool") || fields.find((x) => x.value != "id") || fields[0];
        rules.push({ _id: ++uid, field: first?.value || "", op: opsFor(first?.field)[0].value, value: first?.field.type == "bool" ? "true" : "" });
        redraw();
    }

    // the value of a rule: typed for its field, or a value of the visitor, the page record, the address, a date
    function valueInput(r) {
        const f = fieldOf(r.field);
        if (r.op == "empty" || r.op == "notEmpty") {
            return t.span({ className: "gpp-rec-novalue" });
        }
        const sources = [
            { value: "fixed", label: "a value" },
            ...(kindOf(f) == "date" ? [{ value: "date", label: "a date" }] : []),
            ...(auth ? [{ value: "auth", label: "the visitor" }] : []),
            ...(record ? [{ value: "record", label: "the page record" }] : []),
            { value: "query", label: "the address" },
        ];
        const m = String(r.value).match(/^\{\{\s*(record|auth|query)\.([A-Za-z0-9_.]+)\s*\}\}$/);
        const source = m ? m[1] : DATE_MACROS.some(([x]) => x == r.value) ? "date" : "fixed";
        const setSource = (s) => {
            if (s == source) {
                return;
            }
            r.value = { fixed: "", date: "@now", auth: f?.type == "relation" ? "{{auth.id}}" : "{{auth.email}}", record: f?.type == "relation" ? "{{record.id}}" : `{{record.${record?.fields.find((x) => x.name == r.field.split(".").pop())?.name || "id"}}}`, query: "{{query.q}}" }[s];
            redraw();
        };
        let input;
        if (source == "fixed") {
            if (f?.type == "bool") {
                input = select({ required: true, options: [{ value: "true", label: "Yes" }, { value: "false", label: "No" }], value: () => r.value || "true", onchange: (v) => ((r.value = v || "true"), redraw()) });
            } else if (f?.type == "select") {
                input = select({ required: true, placeholder: "- Choose -", options: (f.values || []).map((v) => ({ value: v, label: v })), value: () => r.value, onchange: (v) => ((r.value = v), redraw()) });
            } else if (f?.type == "relation") {
                input = relationInput(r, f);
            } else {
                input = t.input({
                    type: kindOf(f) == "date" ? "date" : kindOf(f) == "number" ? "number" : "text",
                    step: kindOf(f) == "number" ? "any" : undefined,
                    value: r.value,
                    placeholder: kindOf(f) == "number" ? "0" : "a value",
                    oninput: (e) => ((r.value = e.target.value), preview()),
                });
            }
        } else if (source == "date") {
            input = select({ required: true, options: DATE_MACROS.map(([value, label]) => ({ value, label })), value: () => r.value, onchange: (v) => ((r.value = v || "@now"), redraw()) });
        } else if (source == "auth") {
            const opts = [{ value: "id", label: "the visitor (their id)" }, ...refFields(auth).filter((x) => x.value != "id").map((x) => ({ value: x.value, label: `their ${x.label}` }))];
            input = select({ required: true, options: opts, value: () => m?.[2] || "id", onchange: (v) => ((r.value = `{{auth.${v || "id"}}}`), redraw()) });
        } else if (source == "record") {
            const opts = refFields(record).map((x) => ({ value: x.value, label: `its ${x.label}` }));
            input = select({ required: true, options: opts, value: () => m?.[2] || "id", onchange: (v) => ((r.value = `{{record.${v || "id"}}}`), redraw()) });
        } else {
            input = t.input({
                type: "text",
                className: "txt-code",
                value: m?.[2] || "",
                placeholder: "param",
                title: "The parameter of the address: ?category=… is category",
                oninput: (e) => ((r.value = `{{query.${e.target.value.replace(/[^A-Za-z0-9_]/g, "") || "q"}}}`), preview()),
            });
        }
        return t.div(
            { className: "gpp-rec-value" },
            sources.length > 1 ? t.div({ className: "gpp-rec-source" }, select({ required: true, options: sources, value: () => source, onchange: (v) => v && setSource(v) })) : null,
            t.div({ className: "gpp-rec-input" }, input),
        );
    }

    // the values of a record (the visitor, the page record) a rule can use:
    // its fields and its related record's ("brand → country")
    function refFields(col) {
        return filterFields(col, byName).filter((x) => !["password", "tokenKey"].includes(x.field.name) && x.field.type != "file");
    }

    // a related record: a search among its records
    function relationInput(r, f) {
        const rel = byName(f.collectionId);
        const opts = store({ list: [] });
        if (rel) {
            const labelField = rel.fields.find((x) => x.presentable) || rel.fields.find((x) => ["name", "title", "label", "email"].includes(x.name));
            app.pb.collection(rel.name).getList(1, 200, { requestKey: null, sort: labelField ? labelField.name : "" })
                .then((res) => (opts.list = res.items.map((x) => ({ value: x.id, label: labelField ? `${x[labelField.name]}` : x.id }))))
                .catch(() => {});
        }
        return t.div({ rid: "rel" }, () => t.div({ rid: "rel" + opts.list.length + r.value }, select({ required: true,
            placeholder: "- Choose a record -",
            options: () => opts.list,
            value: () => r.value,
            onchange: (v) => ((r.value = v), redraw()),
        })));
    }

    function ruleRow(r, i) {
        const f = fieldOf(r.field);
        return t.div(
            { rid: "r" + r._id + "_" + data.version, className: "gpp-rec-row" },
            t.span({ className: "gpp-rec-join" }, i == 0 ? "where" : data.match == "any" ? "or" : "and"),
            t.div(
                { className: "gpp-rec-field" },
                select({ required: true,
                    options: () => fields.map((x) => ({ value: x.value, label: x.label })),
                    value: () => r.field,
                    onchange: (v) => {
                        if (!v || v == r.field) {
                            return;
                        }
                        r.field = v;
                        const nf = fieldOf(v);
                        if (!opsFor(nf).some((o) => o.value == r.op)) {
                            r.op = opsFor(nf)[0].value;
                        }
                        r.value = nf?.type == "bool" ? "true" : "";
                        redraw();
                    },
                }),
            ),
            t.div({ className: "gpp-rec-op" }, select({ required: true, options: () => opsFor(f), value: () => r.op, onchange: (v) => v && ((r.op = v), redraw()) })),
            valueInput(r),
            t.button(
                { type: "button", className: "btn sm circle transparent secondary", ariaLabel: app.attrs.tooltip("Remove"), onclick: () => (rules.splice(i, 1), redraw()) },
                t.i({ className: "ri-close-line", ariaHidden: true }),
            ),
        );
    }

    function sortRow(s, i) {
        const f = collection.fields.find((x) => x.name == s.field);
        return t.div(
            { rid: "s" + s._id + "_" + data.version, className: "gpp-rec-row sort" },
            t.span({ className: "gpp-rec-join" }, i == 0 ? "by" : "then"),
            t.div(
                { className: "gpp-rec-field" },
                select({ required: true,
                    options: () => [...sortFields(collection).map((x) => ({ value: x.name, label: x.name })), { value: "@random", label: "Random order" }],
                    value: () => s.field,
                    onchange: (v) => v && ((s.field = v), redraw()),
                }),
            ),
            s.field == "@random"
                ? t.span({ className: "gpp-rec-novalue" })
                : t.div({ className: "gpp-rec-op" }, select({ required: true,
                    options: [{ value: "asc", label: directionLabel(f, false) }, { value: "desc", label: directionLabel(f, true) }],
                    value: () => (s.desc ? "desc" : "asc"),
                    onchange: (v) => v && ((s.desc = v == "desc"), redraw()),
                })),
            t.button(
                { type: "button", className: "btn sm circle transparent secondary", ariaLabel: app.attrs.tooltip("Remove"), onclick: () => (sorts.splice(i, 1), redraw()) },
                t.i({ className: "ri-close-line", ariaHidden: true }),
            ),
        );
    }

    function save() {
        const out = { filter: currentFilter() };
        if (sorts) {
            out.sort = buildSort(sorts);
        }
        onSave(out);
        app.modals.close(modal);
    }

    const modal = t.div(
        { className: "modal popup gpp-records-modal" },
        t.header({ className: "modal-header" }, t.h5({ className: "m-auto" }, `${title} of ${collection.name}`)),
        t.div(
            { className: "modal-content" },
            t.div(
                { className: "gpp-rec-head" },
                t.label({ className: "gp-label" }, "Show the records"),
                () => (data.text === null && rules.length > 1
                    ? t.div(
                        { rid: "match" + data.match, className: "gpp-rec-match" },
                        select({ required: true,
                            options: [{ value: "all", label: "matching all the rules" }, { value: "any", label: "matching any rule" }],
                            value: () => data.match,
                            onchange: (v) => v && ((data.match = v), redraw()),
                        }),
                    )
                    : t.span({ rid: "nomatch" })),
            ),
            () => {
                data.version; // redraw
                if (data.text !== null) {
                    return t.div(
                        { rid: "text" },
                        t.textarea({ className: "txt-code", rows: 3, spellcheck: false, value: data.text, oninput: (e) => ((data.text = e.target.value), preview()) }),
                        t.div({ className: "field-help" }, "A filter written by hand (PocketBase syntax). ", parseFilter(data.text, fieldOf)
                            ? t.button({ type: "button", className: "btn xs secondary transparent", onclick: () => {
                                const p = parseFilter(data.text, fieldOf);
                                rules.splice(0, rules.length, ...p.rules.map((r) => ({ ...r, _id: ++uid })));
                                data.match = p.match;
                                data.text = null;
                                redraw();
                            } }, "Edit as rules")
                            : "It can't be shown as rules (parentheses, “and” mixed with “or”…)."),
                    );
                }
                return t.div(
                    { rid: "rules" + data.version, className: "gpp-rec-rules" },
                    rules.length ? rules.map(ruleRow) : t.p({ className: "txt-hint m-0" }, "All the records."),
                    t.div(
                        { className: "gpp-rec-actions" },
                        t.button({ type: "button", className: "btn sm secondary transparent", onclick: addRule }, t.i({ className: "ri-add-line", ariaHidden: true }), t.span({ className: "txt" }, "Add a rule")),
                        t.button({ type: "button", className: "btn sm secondary transparent", onclick: () => ((data.text = currentFilter()), redraw()) }, t.i({ className: "ri-code-line", ariaHidden: true }), t.span({ className: "txt" }, "Edit as text")),
                    ),
                );
            },
            sorts
                ? t.div(
                    { className: "gpp-rec-sorts" },
                    t.label({ className: "gp-label" }, "Order"),
                    () => {
                        data.version;
                        return t.div(
                            { rid: "sorts" + data.version },
                            sorts.length ? sorts.map(sortRow) : t.p({ className: "txt-hint m-0" }, "The order they were created in."),
                            t.button(
                                { type: "button", className: "btn sm secondary transparent", onclick: () => (sorts.push({ _id: ++uid, field: sortFields(collection)[0]?.name || "created", desc: false }), redraw()) },
                                t.i({ className: "ri-add-line", ariaHidden: true }),
                                t.span({ className: "txt" }, sorts.length ? "Then by…" : "Sort by…"),
                            ),
                        );
                    },
                )
                : null,
            () =>
                t.div(
                    { rid: "count" + data.count + data.countNote + data.names.join(), className: "gpp-rec-count" },
                    data.countNote
                        ? t.span({ className: "txt-hint" }, data.countNote)
                        : data.count === null
                            ? t.span({ className: "txt-hint" }, "…")
                            : t.span(
                                null,
                                t.strong(null, `${data.count} record${data.count == 1 ? "" : "s"}`),
                                data.names.length ? t.span({ className: "txt-hint" }, ` (${data.names.join(", ")}${data.count > data.names.length ? "…" : ""})`) : null,
                                t.span({ className: "txt-hint" }, " before the API rules"),
                            ),
                ),
        ),
        t.footer(
            { className: "modal-footer" },
            t.button({ type: "button", className: "btn transparent secondary m-r-auto", onclick: () => app.modals.close(modal) }, t.span({ className: "txt" }, "Cancel")),
            t.button({ type: "button", className: "btn expanded", onclick: save }, t.span({ className: "txt" }, "Apply")),
        ),
    );
    modal.onafterclose = () => modal.remove();
    document.body.appendChild(modal);
    app.modals.open(modal);
    preview();
}

/**
 * A GrapesJS trait showing the records of a list (or chart) in words, opening
 * the editor. ctx: { collectionOf(comp), byName, record(comp), auth(), onChange(comp) }.
 */
export function defineRecordsTraits(ed, ctx) {
    for (const [type, attr, withSort] of [["gpp-records-filter", "data-pb-filter", true], ["gpp-records-sort", "data-pb-sort", true], ["gpp-chart-filter", "data-pb-filter", false]]) {
        ed.Traits.addType(type, {
            createInput({ component }) {
                const el = document.createElement("div");
                el.className = "gpp-formula-trait gpp-records-trait";
                el.addEventListener("click", (e) => {
                    if (!e.target.closest("button")) {
                        return;
                    }
                    const col = ctx.collectionOf(component);
                    if (!col) {
                        app.toasts.info("Choose the collection first.");
                        return;
                    }
                    const a = component.getAttributes();
                    recordsModal({
                        collection: col,
                        byName: ctx.byName,
                        filter: a["data-pb-filter"] || "",
                        sort: withSort ? a["data-pb-sort"] || "" : undefined,
                        record: ctx.record(component),
                        auth: ctx.auth(),
                        onSave: ({ filter, sort }) => {
                            const set = {};
                            const unset = [];
                            (filter ? (set["data-pb-filter"] = filter) : unset.push("data-pb-filter"));
                            if (withSort) {
                                sort ? (set["data-pb-sort"] = sort) : unset.push("data-pb-sort");
                            }
                            component.addAttributes(set);
                            if (unset.length) {
                                component.removeAttributes(unset);
                            }
                            ctx.onChange(component);
                            draw(el, component);
                        },
                    });
                });
                draw(el, component);
                return el;
            },
            onUpdate({ elInput, component }) {
                draw(elInput, component);
            },
        });
        function draw(el, comp) {
            const col = ctx.collectionOf(comp);
            const v = comp.getAttributes()[attr] || "";
            const words = !v ? "" : attr == "data-pb-sort" ? sortSentence(v, col) : filterSentence(v, col, ctx.byName);
            const b = Object.assign(document.createElement("button"), { type: "button", className: "gpp-dynamic-open gpp-cond-sentence", title: v });
            b.textContent = words || (attr == "data-pb-sort" ? "Default order (oldest first)" : "All the records");
            const edit = Object.assign(document.createElement("button"), {
                type: "button",
                className: "gpp-formula-btn",
                innerHTML: `<i class="${attr == "data-pb-sort" ? "ri-sort-asc" : "ri-filter-3-line"}"></i> ${attr == "data-pb-sort" ? "Change the order" : v ? "Edit the filter" : "Filter the records"}`,
            });
            el.replaceChildren(b, edit);
        }
    }
}
