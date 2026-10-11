// The functions of the templates and formulas (internal/formula), for the
// pickers of the flow editor, the email templates and the page builder:
// a searchable list by category, and the insertion of a function at the
// caret of an input with its first argument selected.

import { api } from "./lib.js";

let loading = null;

/** The catalog: { categories: [...], functions: [{ name, category, syntax, description, example, result, snippet }] }. */
export function loadFunctions() {
    loading ||= api("/api/gp/functions").catch((err) => {
        loading = null;
        throw err;
    });
    return loading;
}

/** Whether the caret of a value is inside an action ("{{ ... |"), and the text of the action before it. */
export function actionAt(value, caret) {
    const before = value.slice(0, caret);
    const open = before.lastIndexOf("{{");
    if (open < 0 || before.lastIndexOf("}}") > open) {
        return null;
    }
    return before.slice(open + 2);
}

/**
 * The text to insert for a function at the caret: {{name arg1 arg2}}
 * outside an action, the call alone (or in parentheses as an argument)
 * inside one. sel is the range of the first argument, to select it.
 */
export function functionSnippet(f, value = "", caret = 0) {
    const inAction = actionAt(value, caret);
    if (f.snippet) {
        const text = inAction == null ? f.snippet : f.snippet.replace(/^\{\{|\}\}$/g, "");
        const m = /[A-Z]{3,}/.exec(text);
        return { text, sel: m ? [m.index, m.index + m[0].length] : null };
    }
    // the arguments of the syntax: "round [decimals] number" → decimals number
    const args = f.syntax
        .replace(/\(.*?\)/g, "")
        .replace(/[[\]]/g, "")
        .split(/\s+/)
        .slice(1)
        .filter((a) => a && a != "...");
    let call = [f.name, ...args].join(" ");
    let open = "";
    if (inAction == null) {
        open = "{{";
        call = open + call + "}}";
    } else if (args.length && !/(^|\||\(|\{\{-?)\s*$/.test(inAction)) {
        open = "(";
        call = "(" + call + ")";
    }
    if (!args.length) {
        return { text: call, sel: null };
    }
    const start = open.length + f.name.length + 1;
    return { text: call, sel: [start, start + args[0].length] };
}

/** Inserts a text at the caret of an input or textarea, then selects sel (relative to the text). */
export function insertAt(input, text, sel = null) {
    const start = input.selectionStart ?? input.value.length;
    const end = input.selectionEnd ?? start;
    input.setRangeText(text, start, end, "end");
    input.focus();
    if (sel) {
        input.setSelectionRange(start + sel[0], start + sel[1]);
    }
    input.dispatchEvent(new Event("input", { bubbles: true }));
}

/**
 * The searchable list of the functions. state: store({ search, category });
 * onPick(f) is called with the clicked function.
 */
export function functionList(state, onPick) {
    const data = store({ catalog: null, error: "" });
    loadFunctions()
        .then((c) => (data.catalog = c))
        .catch(() => (data.error = "Failed to load the functions."));

    return t.div(
        { className: "gp-fn-list" },
        t.div(
            { className: "gp-fn-cats" },
            () =>
                data.catalog
                    ? ["", ...data.catalog.categories].map((c) =>
                          t.button(
                              {
                                  type: "button",
                                  className: () => `gp-fn-cat ${state.category == c ? "active" : ""}`,
                                  onmousedown: (e) => e.preventDefault(),
                                  onclick: () => (state.category = c),
                              },
                              c || "All",
                          )
                      )
                    : null,
        ),
        t.div({ className: "gp-fn-items" }, () => {
            if (data.error) {
                return t.div({ className: "txt-hint txt-sm p-10" }, data.error);
            }
            if (!data.catalog) {
                return t.div({ className: "txt-hint txt-sm p-10" }, "Loading...");
            }
            const q = (state.search || "").toLowerCase().trim();
            const items = data.catalog.functions.filter(
                (f) => (!state.category || f.category == state.category) && (!q || f.name.toLowerCase().includes(q) || f.description.toLowerCase().includes(q))
            );
            if (!items.length) {
                return t.div({ className: "txt-hint txt-sm p-10" }, "No matching function.");
            }
            return items.map((f) =>
                t.button(
                    {
                        type: "button",
                        className: "gp-fn-item",
                        title: "Insert " + f.name,
                        onmousedown: (e) => e.preventDefault(),
                        onclick: () => onPick(f),
                    },
                    t.div({ className: "gp-fn-head" }, t.code({ className: "gp-fn-syntax" }, f.syntax), state.category ? null : t.span({ className: "gp-fn-tag" }, f.category)),
                    t.div({ className: "gp-fn-desc" }, f.description),
                    t.div({ className: "gp-fn-example" }, t.code(null, f.example), t.i({ className: "ri-arrow-right-line", ariaHidden: true }), t.code(null, f.result === "" ? '""' : f.result)),
                )
            );
        }),
    );
}
