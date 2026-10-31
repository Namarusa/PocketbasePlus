// The variable menu of the fields of the APIs and their calls (like the
// bindings of Budibase and the variables of FlutterFlow's API calls): a { }
// button in the field, or "{{" typed in it, lists the variables of the call,
// those of its API and the secrets, and inserts the one picked at the caret
// ({{name}}, or {{.vars.name}} when the name is also a function).

import { secretRef, secretsList } from "../pocketbaseplus/secrets.js";
import { isShortName } from "./assist.js";

const nameRegex = /^[A-Za-z_][A-Za-z0-9_]*$/;

/** The reference of a variable in a template. */
export function refFor(name) {
    return isShortName(name) ? `{{${name}}}` : `{{.vars.${name}}}`;
}

// the "{{partial" typed just before the caret (not closed yet)
const typedRegex = /\{\{\s*(?:\.vars\.)?([A-Za-z0-9_]*)$/;

/**
 * Wraps a template input (or textarea) with the variable menu.
 * groups() → [{ label, icon, items: [{ name, hint }] }] (the variables);
 * onNew(name), optional: declares a new variable (then inserted).
 */
export function varField(input, { groups, onNew = null, secrets = true }) {
    const state = store({ open: false, filter: "", active: 0, typed: false });
    let typedStart = -1; // where "{{" starts when the menu was opened by typing

    function items() {
        const q = state.filter.toLowerCase();
        const out = [];
        const known = new Set();
        for (const g of groups()) {
            for (const it of g.items) {
                known.add(it.name);
                if (!q || it.name.toLowerCase().includes(q)) {
                    out.push({ ...it, group: g.label, icon: g.icon, ref: refFor(it.name) });
                }
            }
        }
        if (secrets) {
            for (const s of secretsList()) {
                if (!q || s.name.toLowerCase().includes(q) || "secret".includes(q)) {
                    out.push({ name: s.name, hint: s.description || s.hint || "", group: "Secrets", icon: "ri-key-2-line", ref: secretRef(s.name) });
                }
            }
        }
        if (onNew && nameRegex.test(state.filter) && !known.has(state.filter)) {
            out.push({ name: state.filter, hint: "", group: "", icon: "ri-add-line", ref: refFor(state.filter), isNew: true });
        }
        return out;
    }

    function open(typed) {
        state.typed = typed;
        state.active = 0;
        state.open = true;
    }
    function close() {
        state.open = false;
        state.filter = "";
        typedStart = -1;
    }

    function pick(it) {
        if (it.isNew && !onNew(it.name)) {
            return;
        }
        const caret = input.selectionStart ?? input.value.length;
        let start = caret;
        let end = input.selectionEnd ?? caret;
        if (state.typed && typedStart >= 0) {
            start = typedStart;
            // "{{na|}}": the closing braces are already there
            if (input.value.slice(end, end + 2) == "}}") {
                end += 2;
            }
        }
        input.setRangeText(it.ref, start, end, "end");
        input.dispatchEvent(new Event("input", { bubbles: true }));
        close();
        input.focus();
    }

    // "{{" typed: the menu opens, filtered by the name being typed
    input.addEventListener("input", (e) => {
        if (!e.isTrusted) {
            return;
        }
        const caret = input.selectionStart ?? input.value.length;
        const m = typedRegex.exec(input.value.slice(0, caret));
        if (m) {
            typedStart = caret - m[0].length;
            state.filter = m[1];
            if (!state.open || !state.typed) {
                open(true);
            }
            state.active = 0;
        } else if (state.open && state.typed) {
            close();
        }
    });
    input.addEventListener("keydown", (e) => {
        if (!state.open) {
            return;
        }
        const list = items();
        if (e.key == "ArrowDown" || e.key == "ArrowUp") {
            e.preventDefault();
            const n = list.length || 1;
            state.active = (state.active + (e.key == "ArrowDown" ? 1 : n - 1)) % n;
        } else if ((e.key == "Enter" || e.key == "Tab") && list.length) {
            e.preventDefault();
            pick(list[Math.min(state.active, list.length - 1)]);
        } else if (e.key == "Escape") {
            e.preventDefault();
            e.stopPropagation(); // not the modal
            close();
        }
    });
    input.addEventListener("blur", () => setTimeout(() => document.activeElement != input && !wrap.contains(document.activeElement) && close(), 150));

    const wrap = t.div(
        { className: () => `gp-varfield ${input.tagName == "TEXTAREA" ? "multiline" : ""} ${state.open ? "open" : ""}` },
        input,
        t.button(
            {
                type: "button",
                className: "btn xs circle transparent secondary gp-varfield-btn",
                ariaLabel: app.attrs.tooltip("Insert a variable"),
                ariaExpanded: () => state.open,
                onmousedown: (e) => e.preventDefault(), // keep the caret of the field
                onclick: () => {
                    if (state.open) {
                        close();
                        return;
                    }
                    state.filter = "";
                    open(false);
                    input.focus();
                },
            },
            t.i({ className: "ri-braces-line", ariaHidden: true }),
        ),
        () => {
            if (!state.open) {
                return null;
            }
            const list = items();
            let group = null;
            const rows = [];
            list.forEach((it, i) => {
                if (it.group && it.group != group) {
                    group = it.group;
                    rows.push(t.div({ className: "gp-varmenu-group" }, group));
                }
                rows.push(
                    t.button(
                        {
                            type: "button",
                            className: () => `gp-varmenu-item ${state.active == i ? "active" : ""}`,
                            onmousedown: (e) => e.preventDefault(),
                            onmouseenter: () => (state.active = i),
                            onclick: () => pick(it),
                        },
                        t.i({ className: it.icon, ariaHidden: true }),
                        it.isNew
                            ? t.span(null, "New variable ", t.code(null, it.name))
                            : t.code(null, it.name),
                        it.hint ? t.small({ className: "txt-hint txt-ellipsis" }, it.hint) : null,
                    ),
                );
            });
            return t.div(
                { rid: "menu" + state.filter + list.length, className: "gp-varmenu", role: "listbox" },
                rows.length
                    ? rows
                    : t.div({ className: "txt-hint txt-sm gp-varmenu-empty" }, state.filter ? `No variable "${state.filter}".` : "No variables yet: type {{name}} to declare one."),
                t.div({ className: "gp-varmenu-help" }, "↑↓ Enter to insert · Esc to close · type ", t.code(null, "{{"), " in a field to open this list"),
            );
        },
    );
    return wrap;
}
