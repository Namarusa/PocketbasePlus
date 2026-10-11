// Dashboard variables bar, shared by the superuser dashboard and the role
// viewer (Shablon only: the viewer has no dashboard `app` object). Panel
// filters reference the variables as {{$key}} ({{$key.from}} / {{$key.to}}
// for date ranges).

export const rangePresets = [
    { value: "today", label: "Today" },
    { value: "7d", label: "Last 7 days" },
    { value: "30d", label: "Last 30 days" },
    { value: "90d", label: "Last 90 days" },
    { value: "365d", label: "Last 365 days" },
    { value: "this_month", label: "This month" },
    { value: "this_year", label: "This year" },
    { value: "all", label: "All time" },
    { value: "custom", label: "Custom range" },
];

export const variableTypes = {
    date_range: { label: "Date range", icon: "ri-calendar-2-line" },
    select: { label: "Select", icon: "ri-list-check" },
    text: { label: "Text", icon: "ri-text" },
    number: { label: "Number", icon: "ri-hashtag" },
};

const STORAGE_KEY = "gpDashVars_"; // + dashboard id

/** The stored values of a dashboard variables (or their defaults). */
export function initialValues(dashboardId, defs) {
    let stored = {};
    try {
        stored = JSON.parse(localStorage.getItem(STORAGE_KEY + dashboardId) || "{}") || {};
    } catch {}
    const values = {};
    for (const d of defs || []) {
        values[d.key] = d.key in stored ? stored[d.key] : d.default ?? (d.type == "date_range" ? "all" : "");
    }
    return values;
}

export function storeValues(dashboardId, values) {
    try {
        localStorage.setItem(STORAGE_KEY + dashboardId, JSON.stringify(values));
    } catch {}
}

let seq = 0;

/** Small popover list (works without the dashboard select component). */
function pickList({ label, options, value, onchange, placeholder = "All" }) {
    const id = "gpVarPick" + seq++;
    return t.div(
        { className: "gp-var-pick" },
        t.button(
            { type: "button", className: "btn sm secondary gp-var-btn", "html-popovertarget": id },
            t.span({ className: "txt-hint" }, label + ":"),
            t.span({ className: "txt" }, () => options().find((o) => o.value == value())?.label || placeholder),
            t.i({ className: "ri-arrow-down-s-line", ariaHidden: true }),
        ),
        t.div(
            { id, popover: "auto", className: "dropdown nowrap gp-var-dropdown" },
            () =>
                t.div(
                    { rid: "opts-" + options().map((o) => o.value).join("|") + "-" + JSON.stringify(value()) },
                    options().map((o) =>
                        t.button(
                            {
                                type: "button",
                                className: `dropdown-item ${o.value == value() ? "active" : ""}`,
                                onclick: (e) => {
                                    e.currentTarget.closest(".dropdown")?.hidePopover();
                                    onchange(o.value);
                                },
                            },
                            t.span({ className: "txt" }, o.label),
                        )
                    ),
                ),
        ),
    );
}

/** Readable summary of a date range value. */
function rangeValue(v) {
    if (v && typeof v == "object") {
        return v;
    }
    return { preset: v || "all" };
}

/**
 * The variables inputs. `values` is a store (key -> value); `onchange` is
 * called (debounced for typed values) after a change.
 */
export function variablesBar(defs, values, onchange) {
    let timer;
    const changed = (debounced = false) => {
        clearTimeout(timer);
        if (debounced) {
            timer = setTimeout(onchange, 500);
        } else {
            onchange();
        }
    };

    return t.div(
        { className: "gp-vars", hidden: () => !defs().length },
        t.i({ className: "ri-filter-3-line gp-vars-icon", ariaHidden: true }),
        () =>
            t.div(
                { rid: "vars-" + defs().map((d) => d.key + d.type + (d.options || []).join()).join("|"), className: "gp-vars-list" },
                defs().map((d) => {
                    const label = d.label || d.key;
                    switch (d.type) {
                        case "date_range": {
                            const current = () => rangeValue(values[d.key]);
                            return t.div(
                                { className: "gp-var" },
                                pickList({
                                    label,
                                    options: () => rangePresets,
                                    value: () => current().preset || "custom",
                                    onchange: (preset) => {
                                        values[d.key] = preset == "custom" ? { preset: "custom", from: "", to: "" } : preset;
                                        if (preset != "custom") {
                                            changed();
                                        }
                                    },
                                }),
                                () =>
                                    current().preset == "custom"
                                        ? t.div(
                                            { rid: "custom", className: "gp-var-range field gp-bare" },
                                            t.input({
                                                type: "date",
                                                ariaLabel: label + " from",
                                                value: () => current().from || "",
                                                onchange: (e) => ((values[d.key] = { ...current(), preset: "custom", from: e.target.value }), changed()),
                                            }),
                                            t.span({ className: "txt-hint" }, "→"),
                                            t.input({
                                                type: "date",
                                                ariaLabel: label + " to",
                                                value: () => current().to || "",
                                                onchange: (e) => ((values[d.key] = { ...current(), preset: "custom", to: e.target.value }), changed()),
                                            }),
                                        )
                                        : null,
                            );
                        }
                        case "select":
                            return t.div(
                                { className: "gp-var" },
                                pickList({
                                    label,
                                    options: () => [{ value: "", label: "All" }, ...(d.options || []).map((o) => ({ value: o, label: o }))],
                                    value: () => values[d.key] ?? "",
                                    onchange: (v) => ((values[d.key] = v), changed()),
                                }),
                            );
                        default:
                            return t.div(
                                { className: "gp-var field gp-bare" },
                                t.input({
                                    type: d.type == "number" ? "number" : "search",
                                    placeholder: label,
                                    ariaLabel: label,
                                    value: () => values[d.key] ?? "",
                                    oninput: (e) => ((values[d.key] = e.target.value), changed(true)),
                                }),
                            );
                    }
                }),
            ),
    );
}
