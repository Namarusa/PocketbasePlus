// Dynamic content ("bindings") shared by the editors: the data browser of the
// pickers with the values of the test data, and the live result shown under
// the inputs (like Budibase: every binding shows its value, every input
// shows what it gives).

/** Value at a dotted path ("steps.api.body.cards.0.name") of plain data. */
export function lookup(data, path) {
    let cur = data;
    for (const part of String(path).split(".")) {
        if (cur == null || typeof cur != "object") {
            return undefined;
        }
        cur = cur[Array.isArray(cur) ? Number(part) : part];
    }
    return cur;
}

/** Short text of a value: "Pikachu", 12, list · 25 items, {name, hp…}. */
export function valueSummary(v, count) {
    if (v === undefined) {
        return "";
    }
    if (v === null || v === "") {
        return "empty";
    }
    if (Array.isArray(v)) {
        const n = count ?? v.length;
        return n == 1 ? "list · 1 item" : `list · ${n} items`;
    }
    if (typeof v == "object") {
        const keys = Object.keys(v);
        return keys.length ? `{${keys.slice(0, 3).join(", ")}${keys.length > 3 ? "…" : ""}}` : "{}";
    }
    if (typeof v == "string") {
        const s = v.replace(/\s+/g, " ");
        return `"${s.length > 48 ? s.slice(0, 48) + "…" : s}"`;
    }
    return String(v);
}

const isTree = (v) => v != null && typeof v == "object" && (Array.isArray(v) ? v.length > 0 : Object.keys(v).length > 0);

// the children of a value: the keys of an object, the fields of the first
// item of a list (or its items when they aren't objects)
function childrenOf(item, value) {
    if (Array.isArray(value)) {
        const first = value[0];
        if (first != null && typeof first == "object" && !Array.isArray(first)) {
            return Object.keys(first).sort().map((k) => ({ path: `${item.path}.0.${k}`, label: `first item › ${k}`, key: k }));
        }
        return value.slice(0, 3).map((_, i) => ({ path: `${item.path}.${i}`, label: `item ${i + 1}`, key: String(i) }));
    }
    return Object.keys(value).sort().map((k) => ({ path: `${item.path}.${k}`, label: k, key: k }));
}

/**
 * The data list of a picker.
 *
 * groups: [{name, icon, items: [{path, label}]}]; sample(path) → {value, count}
 * (value undefined when unknown); onPick(path); onLoop(path) (optional: the
 * "For each" button of the lists); state: store with `search` and `open`
 * (expanded paths).
 */
export function dataList({ groups, sample, onPick, onLoop, state }) {
    state.expanded ||= {};

    function row(item, depth) {
        const { value, count } = sample(item.path) || {};
        const tree = isTree(value);
        const open = () => !!state.expanded[item.path];
        return t.div(
            // keyed by the value too: a new test run changes it
            { rid: "r_" + item.path + "=" + valueSummary(value, count) + (item.label || ""), className: "gp-data-row" },
            t.div(
                { className: "gp-data-line", style: `padding-left: ${depth * 14}px` },
                tree
                    ? t.button(
                        {
                            type: "button",
                            className: "gp-data-toggle",
                            ariaLabel: app.attrs.tooltip("Show the fields"),
                            onmousedown: (e) => e.preventDefault(),
                            onclick: () => (state.expanded[item.path] = !open()),
                        },
                        t.i({ className: () => (open() ? "ri-arrow-down-s-line" : "ri-arrow-right-s-line"), ariaHidden: true }),
                    )
                    : t.span({ className: "gp-data-toggle-space" }),
                t.button(
                    {
                        type: "button",
                        className: "gp-picker-item",
                        title: item.path,
                        onmousedown: (e) => e.preventDefault(),
                        onclick: () => onPick(item.path),
                    },
                    t.span({ className: "txt" }, item.label || item.path),
                    value !== undefined ? t.span({ className: "gp-data-value" }, valueSummary(value, count)) : t.code(null, item.path),
                ),
                Array.isArray(value) && onLoop
                    ? t.button(
                        {
                            type: "button",
                            className: "btn xs secondary transparent gp-data-loop",
                            title: "Do the next steps for each item of this list",
                            onmousedown: (e) => e.preventDefault(),
                            onclick: () => onLoop(item.path),
                        },
                        t.i({ className: "ri-repeat-line", ariaHidden: true }),
                        t.span({ className: "txt" }, "For each"),
                    )
                    : null,
            ),
            () => (tree && open() ? t.div({ rid: "kids" }, ...childrenOf(item, value).map((c) => row(c, depth + 1))) : null),
        );
    }

    // a search also looks inside the test data (4 levels)
    function searchItems(q) {
        const out = [];
        const seen = new Set();
        const visit = (item, depth) => {
            if (out.length >= 80 || seen.has(item.path)) {
                return;
            }
            seen.add(item.path);
            if (item.path.toLowerCase().includes(q) || (item.label || "").toLowerCase().includes(q)) {
                out.push(item);
            }
            const { value } = sample(item.path) || {};
            if (depth < 4 && isTree(value)) {
                for (const c of childrenOf(item, value)) {
                    visit({ ...c, label: `${item.label || item.path} › ${c.key}` }, depth + 1);
                }
            }
        };
        return (g) => {
            out.length = 0;
            seen.clear();
            g.items.forEach((i) => visit(i, 0));
            return [...out];
        };
    }

    return t.div({ className: "gp-picker-list" }, () => {
        const q = (state.search || "").trim().toLowerCase();
        const find = q ? searchItems(q) : null;
        const result = [];
        for (const g of groups()) {
            const items = find ? find(g) : g.items;
            if (!items.length) {
                continue;
            }
            result.push(
                t.div(
                    { rid: "g_" + g.name + (q ? "_s" + q : ""), className: "gp-picker-group" },
                    t.div({ className: "gp-picker-title" }, g.icon ? t.i({ className: g.icon, ariaHidden: true }) : null, t.span(null, g.name)),
                    ...items.map((i) => row(i, 0)),
                ),
            );
        }
        return result.length ? result : t.div({ rid: "none", className: "txt-hint txt-sm p-10" }, "No matching content.");
    });
}

/**
 * Batches the previews of many inputs in one request: request(item) resolves
 * with the result of one {id, expr, loops}; send(items) returns {results}.
 */
export function previewQueue(send) {
    let pending = [];
    let timer = null;
    return {
        request(item) {
            return new Promise((resolve) => {
                pending.push({ item, resolve });
                clearTimeout(timer);
                timer = setTimeout(async () => {
                    const batch = pending;
                    pending = [];
                    let res = null;
                    try {
                        res = await send(batch.map((b) => b.item));
                    } catch (err) {
                        if (!err?.isAbort) {
                            console.warn("previews:", err);
                        }
                    }
                    for (const b of batch) {
                        b.resolve(res ? { hasSample: res.hasSample, ...(res.results?.[b.item.id] || {}) } : null);
                    }
                }, 60);
            });
        },
    };
}

let previewIds = 0;

/**
 * The live result under an input: "= Pikachu", "= list · 25 items", an
 * error, or a hint about an unknown variable (check(expr) returns a message
 * or "", computed in the browser). expr() and loops() are reactive getters
 * (item(): the loop item when the test run didn't reach the loop); version()
 * changes when the test data changes.
 */
export function previewLine({ expr, loops, item, version, queue, check }) {
    const id = "p" + ++previewIds;
    const state = store({ result: null, hint: "" });
    let timer = null;
    let seq = 0;
    let watcher = null;

    function refresh(value) {
        clearTimeout(timer);
        const src = value ?? "";
        if (!src.includes("{{")) {
            state.result = null;
            state.hint = "";
            return;
        }
        timer = setTimeout(async () => {
            const n = ++seq;
            state.hint = check?.(src) || "";
            const r = await queue.request({ id, expr: src, loops: loops?.() || [], item: item?.() });
            if (n == seq) {
                state.result = r;
            }
        }, 300);
    }

    return t.div(
        {
            className: "gp-preview-line",
            onmount: () => {
                watcher = watch(() => [expr(), version?.(), JSON.stringify(loops?.() || [])], ([v]) => refresh(v));
            },
            onunmount: () => {
                clearTimeout(timer);
                watcher?.unwatch();
            },
        },
        () => {
            const r = state.result;
            if (state.hint) {
                return t.span({ rid: "hint_" + state.hint, className: "gp-preview-warn" }, t.i({ className: "ri-error-warning-line", ariaHidden: true }), state.hint);
            }
            if (!r) {
                return null;
            }
            if (r.error) {
                return t.span({ rid: "err_" + r.error, className: "gp-preview-error" }, t.i({ className: "ri-close-circle-line", ariaHidden: true }), r.error);
            }
            if (r.missing) {
                return t.span(
                    { rid: "missing_" + r.missing + r.hasSample, className: "gp-preview-hint" },
                    r.hasSample ? `${r.missing}: not in the test data` : "Run a test to see the values",
                );
            }
            const text = r.type == "text" || r.type == "number" || r.type == "bool" ? (r.text ?? String(r.value)) : valueSummary(r.value, r.count);
            const note = r.shortened ? t.span({ className: "gp-preview-hint", title: "The test data keeps the first items of the long lists: a run uses all of them." }, "(first items of a long list)") : null;
            return t.span(
                { rid: "val_" + (r.shortened ? "s" : "") + r.type + text, className: "gp-preview-value", title: typeof r.value == "object" ? JSON.stringify(r.value, null, 2).slice(0, 2000) : String(r.text ?? r.value ?? "") },
                t.span({ className: "gp-preview-eq" }, "="),
                r.type == "empty" ? t.em(null, "empty") : text.length > 160 ? text.slice(0, 160) + "…" : text,
                note,
            );
        },
    );
}

/** "did you mean" helper: the closest name (edit distance ≤ 2). */
export function closest(word, names) {
    let best = "";
    let bestD = 3;
    for (const n of names) {
        const d = distance(word, n);
        if (d < bestD) {
            best = n;
            bestD = d;
        }
    }
    return best;
}

function distance(a, b) {
    const dp = Array.from({ length: a.length + 1 }, (_, i) => [i, ...Array(b.length).fill(0)]);
    for (let j = 1; j <= b.length; j++) {
        dp[0][j] = j;
    }
    for (let i = 1; i <= a.length; i++) {
        for (let j = 1; j <= b.length; j++) {
            dp[i][j] = Math.min(dp[i - 1][j] + 1, dp[i][j - 1] + 1, dp[i - 1][j - 1] + (a[i - 1] == b[j - 1] ? 0 : 1));
        }
    }
    return dp[a.length][b.length];
}
