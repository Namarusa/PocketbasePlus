// Dashboards (insights): pages of panels showing metrics, charts and lists
// computed from the collections, in the spirit of Directus Insights.

import { dashboardGrid, freePosition, toggleFullscreen } from "./grid.js";
import { api, debounce, field, handleError, multiSelect, page, select, sidebar, userCollections } from "./lib.js";
import { aggregateFunctions, colorChoices, loadECharts, panelBody, panelTypes } from "./panels.js";
import { panelSuggestions } from "./suggestions.js";
import { initialValues, rangePresets, storeValues, variablesBar, variableTypes } from "./variables.js";

const extBase = app.pb.buildURL("/_/extensions/pocketbaseplus/");
export const COLUMNS = 24;

export const dashboardIcons = [
    "ri-dashboard-line", "ri-line-chart-line", "ri-bar-chart-2-line", "ri-pie-chart-2-line", "ri-hashtag",
    "ri-money-dollar-circle-line", "ri-shopping-cart-2-line", "ri-user-3-line", "ri-team-line", "ri-mail-line",
    "ri-calendar-line", "ri-time-line", "ri-stack-line", "ri-database-2-line", "ri-flashlight-line", "ri-star-line",
    "ri-heart-line", "ri-alert-line", "ri-checkbox-circle-line", "ri-global-line", "ri-file-list-3-line", "ri-lightbulb-flash-line",
];

const refreshChoices = [
    { value: 0, label: "Off" },
    { value: 10, label: "10 seconds" },
    { value: 30, label: "30 seconds" },
    { value: 60, label: "1 minute" },
    { value: 300, label: "5 minutes" },
    { value: 900, label: "15 minutes" },
    { value: 3600, label: "1 hour" },
];

// --- shared list (sidebar + pages) -------------------------------------------------

const shared = store({ dashboards: [], loaded: false });
let loading = null;

export function loadDashboards() {
    if (!loading) {
        loading = app.pb
            .collection("dashboards")
            .getFullList({ sort: "name", requestKey: null })
            .then((list) => (shared.dashboards = list))
            .catch(handleError)
            .finally(() => {
                shared.loaded = true;
                loading = null;
            });
    }
    return loading;
}

function dashboardsSidebar() {
    if (!shared.loaded) {
        loadDashboards();
    }
    return sidebar("gpDashboardsSidebar", () => [
        {
            name: "Dashboards",
            empty: "No dashboards yet.",
            links: [
                { href: "#/dashboards", label: "All dashboards", icon: "ri-layout-grid-line", exact: true },
                ...shared.dashboards.map((d) => ({ href: "#/dashboards/" + d.id, label: d.name, icon: d.icon || "ri-dashboard-line" })),
            ],
        },
    ]);
}

function viewerUrl(id = "") {
    return window.location.origin + "/dashboards" + (id ? "#/" + id : "");
}

// --- dashboard settings modal --------------------------------------------------------

/** Opens the create/edit modal of a dashboard; resolves with the saved record. */
function openDashboardSettings(dashboard = null, { onDeleted } = {}) {
    const data = store({
        form: {
            id: dashboard?.id || "",
            name: dashboard?.name || "",
            icon: dashboard?.icon || "ri-dashboard-line",
            note: dashboard?.note || "",
            roles: [...(dashboard?.roles || [])],
            refresh: dashboard?.refresh || 0,
            variables: clone(dashboard?.variables || []).map((v) => ({ ...v, options: (v.options || []).join(", ") })),
        },
        roles: [],
        collections: [],
        isSaving: false,
    });
    userCollections().then((list) => (data.collections = list)).catch(handleError);

    app.pb.collection("roles").getFullList({ sort: "name", requestKey: null })
        .then((list) => (data.roles = list))
        .catch(handleError);

    let resolveSaved;
    const saved = new Promise((r) => (resolveSaved = r));

    async function save() {
        data.isSaving = true;
        try {
            const body = { ...data.form };
            delete body.id;
            body.variables = data.form.variables.map((v) => {
                const out = { key: v.key.trim(), label: v.label.trim(), type: v.type };
                if (v.type == "select") {
                    out.options = String(v.options || "").split(",").map((o) => o.trim()).filter(Boolean);
                }
                if (v.default !== "" && v.default != null) {
                    out.default = v.type == "number" ? Number(v.default) : v.default;
                }
                return out;
            });
            const rec = data.form.id
                ? await app.pb.collection("dashboards").update(data.form.id, body)
                : await app.pb.collection("dashboards").create(body);
            app.toasts.success(data.form.id ? "Successfully saved the dashboard." : "Successfully created the dashboard.");
            loadDashboards();
            app.modals.close(modal);
            resolveSaved(rec);
        } catch (err) {
            handleError(err);
        } finally {
            data.isSaving = false;
        }
    }

    function remove() {
        app.modals.confirm(`Do you really want to delete the dashboard "${dashboard.name}" and its panels?`, async () => {
            try {
                await app.pb.collection("dashboards").delete(dashboard.id);
                app.toasts.success("Successfully deleted the dashboard.");
                loadDashboards();
                app.modals.close(modal);
                onDeleted?.();
            } catch (err) {
                handleError(err);
            }
        });
    }

    const modal = t.div(
        { className: "modal popup gp-dashboard-modal" },
        t.header({ className: "modal-header" }, t.h5({ className: "m-auto" }, data.form.id ? "Dashboard settings" : "New dashboard")),
        t.form(
            {
                id: "gpDashboardForm",
                className: "modal-content",
                inert: () => data.isSaving,
                onsubmit: (e) => {
                    e.preventDefault();
                    save();
                },
            },
            t.div(
                { className: "grid sm" },
                t.div({ className: "col-lg-12" }, field({ label: "Name", name: "name" }, t.input({
                    id: "name",
                    type: "text",
                    required: true,
                    value: () => data.form.name,
                    oninput: (e) => (data.form.name = e.target.value),
                }))),
                t.div(
                    { className: "col-lg-12" },
                    t.label({ className: "gp-label" }, "Icon"),
                    t.div(
                        { className: "gp-icon-picker" },
                        dashboardIcons.map((icon) =>
                            t.button(
                                {
                                    type: "button",
                                    className: () => `btn sm circle ${data.form.icon == icon ? "" : "transparent secondary"}`,
                                    title: icon.replace(/^ri-|-line$/g, ""),
                                    onclick: () => (data.form.icon = icon),
                                },
                                t.i({ className: icon, ariaHidden: true }),
                            )
                        ),
                    ),
                ),
                t.div({ className: "col-lg-12" }, field({ label: "Note", name: "note" }, t.input({
                    id: "note",
                    type: "text",
                    placeholder: "Optional description",
                    value: () => data.form.note,
                    oninput: (e) => (data.form.note = e.target.value),
                }))),
                t.div({ className: "col-lg-6" }, field(
                    { label: "Shared with roles", name: "roles", hint: "Users with these roles can view it (read-only) at /dashboards, with their own data permissions." },
                    multiSelect({
                        id: "roles",
                        placeholder: "Superusers only",
                        options: () => data.roles.map((r) => ({ value: r.id, label: r.name })),
                        value: () => data.form.roles,
                        onchange: (v) => (data.form.roles = v),
                    }),
                )),
                t.div({ className: "col-lg-12" }, variablesEditor(data)),
                t.div({ className: "col-lg-6" }, field(
                    { label: "Auto refresh", name: "refresh" },
                    select({
                        id: "refresh",
                        required: true,
                        options: refreshChoices.map((c) => ({ value: String(c.value), label: c.label })),
                        value: () => String(data.form.refresh || 0),
                        onchange: (v) => (data.form.refresh = Number(v)),
                    }),
                )),
            ),
        ),
        t.footer(
            { className: "modal-footer" },
            dashboard
                ? t.button(
                    { type: "button", className: "btn transparent danger m-r-auto", onclick: remove },
                    t.i({ className: "ri-delete-bin-7-line", ariaHidden: true }),
                    t.span({ className: "txt" }, "Delete"),
                )
                : t.button({ type: "button", className: "btn transparent secondary m-r-auto", onclick: () => app.modals.close(modal) }, t.span({ className: "txt" }, "Close")),
            t.button(
                { type: "submit", "html-form": "gpDashboardForm", className: () => `btn expanded ${data.isSaving ? "loading" : ""}`, disabled: () => data.isSaving },
                t.span({ className: "txt" }, data.form.id ? "Save changes" : "Create"),
            ),
        ),
    );
    // the modal is created on demand: remove it from the page once closed
    modal.onafterclose = () => {
        resolveSaved(null);
        modal.remove();
    };
    document.body.appendChild(modal);
    app.modals.open(modal);
    return saved;
}

/** Editor of the dashboard variables (settings modal). */
function variablesEditor(data) {
    const typeOptions = Object.entries(variableTypes).map(([value, d]) => ({ value, label: d.label }));
    // select options from the values of a select field
    const selectFields = () =>
        data.collections.flatMap((c) => c.fields.filter((f) => f.type == "select").map((f) => ({ value: `${c.name}.${f.name}`, label: `${c.name} · ${f.name}`, values: f.values || [] })));
    return t.div(
        { className: "gp-vars-editor" },
        t.label({ className: "gp-label" }, "Variables"),
        t.div({ className: "field-help m-b-5" }, "Inputs shown above the panels. Use them in the panel filters as {{$key}}, e.g. ({{$status}} = \"\" || status = {{$status}}), or for a date range: created >= {{$period.from}} && created < {{$period.to}}."),
        () =>
            t.div(
                { rid: "vars-" + data.form.variables.length, className: "gp-vars-rows" },
                data.form.variables.map((v, i) =>
                    t.div(
                        { className: "gp-vars-row" },
                        t.div({ className: "field gp-bare" }, t.input({ type: "text", placeholder: "key", className: "txt-code", required: true, pattern: "[A-Za-z_][A-Za-z0-9_]*", value: () => v.key, oninput: (e) => (v.key = e.target.value) })),
                        t.div({ className: "field gp-bare" }, t.input({ type: "text", placeholder: "Label", value: () => v.label, oninput: (e) => (v.label = e.target.value) })),
                        t.div({ className: "field gp-bare" }, select({ required: true, options: typeOptions, value: () => v.type, onchange: (val) => ((v.type = val), (v.default = val == "date_range" ? "30d" : "")) })),
                        () =>
                            v.type == "select"
                                ? t.div(
                                    { rid: "opts", className: "gp-vars-options" },
                                    t.div({ className: "field gp-bare" }, t.input({ type: "text", placeholder: "Options: paid, shipped...", value: () => v.options, oninput: (e) => (v.options = e.target.value) })),
                                    t.div({ className: "field gp-bare" }, select({
                                        placeholder: "Use a select field",
                                        options: () => selectFields(),
                                        value: () => "",
                                        onchange: (val) => {
                                            const f = selectFields().find((x) => x.value == val);
                                            if (f) {
                                                v.options = f.values.join(", ");
                                            }
                                        },
                                    })),
                                )
                                : v.type == "date_range"
                                    ? t.div({ rid: "range", className: "field gp-bare" }, select({
                                        required: true,
                                        options: rangePresets.filter((p) => p.value != "custom").map((p) => ({ value: p.value, label: "Default: " + p.label })),
                                        value: () => v.default || "30d",
                                        onchange: (val) => (v.default = val),
                                    }))
                                    : t.div({ rid: "def", className: "field gp-bare" }, t.input({ type: v.type == "number" ? "number" : "text", placeholder: "Default value", value: () => v.default ?? "", oninput: (e) => (v.default = e.target.value) })),
                        t.button({ type: "button", className: "btn sm circle transparent danger", title: "Remove", onclick: () => (data.form.variables = data.form.variables.filter((_, j) => j != i)) }, t.i({ className: "ri-close-line", ariaHidden: true })),
                    )
                ),
            ),
        t.button(
            {
                type: "button",
                className: "btn sm transparent secondary",
                onclick: () => (data.form.variables = [...data.form.variables, { key: data.form.variables.length ? "" : "period", label: data.form.variables.length ? "" : "Period", type: data.form.variables.length ? "select" : "date_range", options: "", default: data.form.variables.length ? "" : "30d" }]),
            },
            t.i({ className: "ri-add-line", ariaHidden: true }),
            t.span({ className: "txt" }, "Add variable"),
        ),
    );
}

// --- list page ---------------------------------------------------------------------

export function pageDashboards() {
    app.store.title = "Dashboards";
    loadDashboards();

    async function create() {
        const rec = await openDashboardSettings();
        if (rec) {
            window.location.hash = `#/dashboards/${rec.id}?edit=1`;
        }
    }

    return page(
        {
            pbEvent: "gpPageDashboards",
            className: "gp-page-dashboards",
            sidebar: dashboardsSidebar(),
            breadcrumbs: ["Dashboards"],
            buttons: t.button(
                { type: "button", className: "btn expanded-lg", onclick: create },
                t.i({ className: "ri-add-line", ariaHidden: true }),
                t.span({ className: "txt" }, "New dashboard"),
            ),
        },
        t.p(
            { className: "txt-hint m-t-0" },
            "Build dashboards of metrics, charts and lists from your collections. Share them with roles: their users open them at ",
            t.a({ href: viewerUrl(), target: "_blank", rel: "noopener" }, "/dashboards"),
            " and only see the records they're allowed to list.",
        ),
        () => {
            if (!shared.loaded) {
                return t.div({ rid: "loading", className: "txt-center p-base" }, t.span({ className: "loader" }));
            }
            if (!shared.dashboards.length) {
                return t.div(
                    { rid: "empty", className: "gp-dashboards-empty" },
                    t.i({ className: "ri-dashboard-line", ariaHidden: true }),
                    t.h6(null, "No dashboards yet"),
                    t.p({ className: "txt-hint" }, "Create one and add metrics, charts and lists."),
                    t.button({ type: "button", className: "btn", onclick: create }, t.i({ className: "ri-add-line", ariaHidden: true }), t.span({ className: "txt" }, "New dashboard")),
                );
            }
            return t.div(
                // keyed by content: an element kept by rid doesn't refresh its children
                { rid: "cards-" + shared.dashboards.map((d) => d.id + d.updated).join(), className: "gp-dashboard-cards" },
                shared.dashboards.map((d) =>
                    t.a(
                        { rid: d.id + d.updated, href: "#/dashboards/" + d.id, className: "gp-dashboard-card" },
                        t.i({ className: `${d.icon || "ri-dashboard-line"} gp-dashboard-card-icon`, ariaHidden: true }),
                        t.div(
                            { className: "gp-dashboard-card-content" },
                            t.strong(null, d.name),
                            t.span({ className: "txt-hint txt-sm" }, d.note || ""),
                        ),
                        d.roles?.length ? t.span({ className: "label sm", title: "Shared with roles" }, t.i({ className: "ri-share-line", ariaHidden: true }), `${d.roles.length}`) : null,
                    )
                ),
            );
        },
    );
}

// --- dashboard page ------------------------------------------------------------------

function clone(v) {
    return JSON.parse(JSON.stringify(v));
}

let keySeq = 0;
function newKey() {
    return `new_${Date.now()}_${keySeq++}`;
}

export function pageDashboard(route) {
    const id = route.params.id;
    const startEditing = /[?&]edit=1/.test(window.location.hash);

    const data = store({
        dashboard: null, // saved state
        panels: [], // displayed panels (a draft copy while editing)
        results: {}, // panel key -> { data, error, loading }
        editing: false,
        dirty: false,
        isLoading: true,
        isSaving: false,
        refreshedAt: null,
        vars: {}, // dashboard variable values
        chartsReady: !!window.echarts,
    });

    const keyOf = (p) => p.id || p._key;
    const stateOf = (p) => ({
        get data() {
            return data.results[keyOf(p)]?.data ?? null;
        },
        get error() {
            return data.results[keyOf(p)]?.error || "";
        },
        get loading() {
            return !!data.results[keyOf(p)]?.loading;
        },
    });

    loadECharts(extBase).then(() => (data.chartsReady = true)).catch((err) => app.toasts.error(err.message));

    async function load() {
        data.isLoading = true;
        try {
            const d = await api(`/api/dashboards/${encodeURIComponent(id)}`, { requestKey: "gpDashboard" + id });
            data.dashboard = d;
            data.panels = clone(d.panels);
            data.vars = initialValues(id, d.variables);
            app.store.title = d.name;
            if (startEditing && !data.editing) {
                startEdit();
            }
            await refresh();
        } catch (err) {
            if (err?.status == 404) {
                app.toasts.error("Dashboard not found.");
                window.location.hash = "#/dashboards";
                return;
            }
            handleError(err);
        } finally {
            data.isLoading = false;
        }
    }

    /** Loads the data of the saved panels. */
    async function refresh() {
        if (!data.dashboard || data.editing) {
            return;
        }
        for (const p of data.panels) {
            data.results[keyOf(p)] = { ...(data.results[keyOf(p)] || {}), loading: true };
        }
        try {
            const res = await api(`/api/dashboards/${encodeURIComponent(id)}/data`, {
                method: "POST",
                body: { vars: clone(data.vars) },
                requestKey: "gpDashboardData" + id,
            });
            const results = {};
            for (const p of data.panels) {
                const r = res.panels?.[p.id] || {};
                results[p.id] = { data: r.data ?? null, error: r.error || "", loading: false };
            }
            data.results = results;
            data.refreshedAt = new Date();
        } catch (err) {
            for (const p of data.panels) {
                data.results[keyOf(p)] = { ...(data.results[keyOf(p)] || {}), loading: false };
            }
            handleError(err);
        }
    }

    /** Loads the data of one (unsaved) panel. */
    async function previewPanel(p) {
        const key = keyOf(p);
        if (p.type == "label") {
            data.results[key] = { data: null, error: "", loading: false };
            return;
        }
        data.results[key] = { ...(data.results[key] || {}), loading: true };
        try {
            const res = await api("/api/dashboards/preview", {
                method: "POST",
                body: { type: p.type, options: p.options, variables: data.dashboard?.variables || [], vars: clone(data.vars) },
                requestKey: "gpPanelPreview" + key,
            });
            data.results[key] = { data: res.data ?? null, error: res.error || "", loading: false };
        } catch (err) {
            if (!err?.isAbort) {
                data.results[key] = { data: null, error: err?.message || "Failed to load the data.", loading: false };
            }
        }
    }

    // auto refresh (paused while editing or when the tab is hidden)
    let timer;
    const autoRefresh = watch(() => [data.dashboard?.refresh, data.editing], ([seconds, editing]) => {
        clearInterval(timer);
        if (seconds > 0 && !editing) {
            timer = setInterval(() => {
                if (!document.hidden) {
                    refresh();
                }
            }, seconds * 1000);
        }
    });

    function startEdit() {
        data.panels = clone(data.dashboard.panels);
        data.editing = true;
        data.dirty = false;
    }

    function cancelEdit() {
        const discard = () => {
            data.editing = false;
            data.dirty = false;
            data.panels = clone(data.dashboard.panels);
            refresh();
        };
        if (data.dirty) {
            app.modals.confirm("Discard the unsaved changes?", discard);
        } else {
            discard();
        }
    }

    async function saveEdit() {
        data.isSaving = true;
        try {
            const body = data.panels.map((p) => {
                const copy = clone(p);
                delete copy._key;
                return copy;
            });
            const d = await api(`/api/dashboards/${encodeURIComponent(id)}/panels`, { method: "PUT", body: { panels: body } });
            data.dashboard = d;
            data.panels = clone(d.panels);
            data.editing = false;
            data.dirty = false;
            app.toasts.success("Successfully saved the dashboard.");
            refresh();
        } catch (err) {
            handleError(err);
        } finally {
            data.isSaving = false;
        }
    }

    async function addPanel() {
        const panel = await openPanelEditor(null, editorContext());
        if (!panel) {
            return;
        }
        const type = panelTypes[panel.type];
        Object.assign(panel, { _key: newKey(), w: type.w, h: type.h, ...freePosition(data.panels, type.w, type.h, COLUMNS) });
        data.panels.push(panel);
        data.dirty = true;
        previewPanel(data.panels[data.panels.length - 1]);
    }

    async function editPanel(p) {
        const updated = await openPanelEditor(p, editorContext());
        if (!updated) {
            return;
        }
        const type = panelTypes[updated.type];
        Object.assign(p, updated, {
            w: Math.max(p.w, type.minW),
            h: Math.max(p.h, type.minH),
        });
        p.x = Math.min(p.x, COLUMNS - p.w);
        data.dirty = true;
        previewPanel(p);
    }

    function duplicatePanel(p) {
        const copy = clone(p);
        delete copy.id;
        copy._key = newKey();
        Object.assign(copy, freePosition(data.panels, copy.w, copy.h, COLUMNS));
        data.panels.push(copy);
        data.results[copy._key] = clone(data.results[keyOf(p)] || {});
        data.dirty = true;
    }

    function removePanel(p) {
        data.panels = data.panels.filter((x) => x !== p && keyOf(x) != keyOf(p));
        data.dirty = true;
    }

    async function openSettings() {
        const rec = await openDashboardSettings(data.dashboard && { ...data.dashboard, roles: (await app.pb.collection("dashboards").getOne(id)).roles }, {
            onDeleted: () => (window.location.hash = "#/dashboards"),
        });
        if (rec) {
            Object.assign(data.dashboard, { name: rec.name, icon: rec.icon, note: rec.note, refresh: rec.refresh, variables: rec.variables || [] });
            data.vars = { ...initialValues(id, rec.variables), ...pick(data.vars, (rec.variables || []).map((v) => v.key)) };
            app.store.title = rec.name;
            if (data.editing) {
                data.panels.forEach(previewPanel);
            } else {
                refresh();
            }
        }
    }

    function pick(obj, keys) {
        const out = {};
        for (const k of keys) {
            if (k in obj) {
                out[k] = obj[k];
            }
        }
        return out;
    }

    /** The variables of the panel editor (preview and filter helpers). */
    function editorContext() {
        return { variables: data.dashboard?.variables || [], vars: clone(data.vars) };
    }

    // the bar reads and writes the current values (data.vars is replaced on load)
    const varsProxy = new Proxy({}, {
        get: (_, key) => data.vars[key],
        set: (_, key, value) => {
            data.vars[key] = value;
            return true;
        },
        has: (_, key) => key in data.vars,
    });

    function varsChanged() {
        storeValues(id, data.vars);
        if (data.editing) {
            data.panels.forEach(previewPanel);
        } else {
            refresh();
        }
    }

    /** Opens the records behind a chart element (bar, slice, time bucket). */
    async function drill(panel, target) {
        const o = panel.options || {};
        const c = (await userCollections()).find((x) => x.name == o.collection);
        if (!c) {
            return;
        }
        const conds = [];
        if (o.filter?.trim()) {
            conds.push(`(${literalFilter(o.filter, data.dashboard?.variables || [], data.vars)})`);
        }
        if (panel.type == "time_series") {
            conds.push(bucketFilter(o.dateField, target.bucket, target.precision));
        } else if (o.groupField) {
            conds.push(fieldFilter(c, o.groupField, target.key));
        }
        if (o.splitField && target.seriesKey && target.seriesKey != "__other__") {
            conds.push(fieldFilter(c, o.splitField, target.seriesKey));
        }
        const params = new URLSearchParams({ collection: c.name, filter: conds.filter(Boolean).join(" && ") });
        window.open("#/collections?" + params.toString(), "_blank");
    }

    load();

    let gridWrap;
    const grid = dashboardGrid({
        panels: () => data.panels,
        state: stateOf,
        editing: () => data.editing,
        columns: COLUMNS,
        actions: {
            edit: editPanel,
            duplicate: duplicatePanel,
            remove: removePanel,
            changed: () => (data.dirty = true),
            drill,
        },
        emptyMessage: t.div(
            { className: "gp-grid-empty-content" },
            t.i({ className: "ri-layout-grid-line", ariaHidden: true }),
            t.p(null, "This dashboard has no panels yet."),
            t.button(
                { type: "button", className: "btn", onclick: () => (data.editing ? addPanel() : (startEdit(), addPanel())) },
                t.i({ className: "ri-add-line", ariaHidden: true }),
                t.span({ className: "txt" }, "Add a panel"),
            ),
        ),
    });

    const iconBtn = (icon, title, onclick, extra = {}) =>
        t.button({ type: "button", className: "btn circle transparent secondary", ariaLabel: app.attrs.tooltip(title), onclick, ...extra }, t.i({ className: icon, ariaHidden: true }));

    return page(
        {
            pbEvent: "gpPageDashboard",
            className: "gp-page-dashboard",
            sidebar: dashboardsSidebar(),
            wide: true,
            breadcrumbs: [
                t.a({ href: "#/dashboards" }, "Dashboards"),
                t.span({ className: "flex gap-5" }, () => (data.dashboard?.icon ? t.i({ rid: data.dashboard.icon, className: data.dashboard.icon, ariaHidden: true }) : null), () => data.dashboard?.name || "..."),
            ],
            buttons: () => {
                if (!data.dashboard) {
                    return null;
                }
                if (data.editing) {
                    return t.div(
                        { rid: "edit", className: "flex gap-10" },
                        t.button(
                            { type: "button", className: "btn transparent secondary", onclick: cancelEdit, disabled: () => data.isSaving },
                            t.span({ className: "txt" }, "Cancel"),
                        ),
                        t.button(
                            { type: "button", className: "btn secondary", onclick: addPanel, disabled: () => data.isSaving },
                            t.i({ className: "ri-add-line", ariaHidden: true }),
                            t.span({ className: "txt" }, "Add panel"),
                        ),
                        t.button(
                            { type: "button", className: () => `btn expanded ${data.isSaving ? "loading" : ""}`, onclick: saveEdit, disabled: () => data.isSaving },
                            t.span({ className: "txt" }, "Save"),
                        ),
                    );
                }
                return t.div(
                    { rid: "view", className: "flex gap-5" },
                    t.span({ className: "txt-hint txt-sm gp-refreshed", title: "Last refresh" }, () => (data.refreshedAt ? data.refreshedAt.toLocaleTimeString() : "")),
                    iconBtn("ri-refresh-line", "Refresh", refresh),
                    iconBtn("ri-fullscreen-line", "Fullscreen", () => toggleFullscreen(gridWrap)),
                    iconBtn("ri-external-link-line", "Open the role viewer", () => window.open(viewerUrl(id), "_blank", "noopener")),
                    iconBtn("ri-settings-3-line", "Settings", openSettings),
                    t.button(
                        { type: "button", className: "btn expanded-lg", onclick: startEdit },
                        t.i({ className: "ri-pencil-line", ariaHidden: true }),
                        t.span({ className: "txt" }, "Edit panels"),
                    ),
                );
            },
        },
        t.p({ className: "txt-hint m-t-0", hidden: () => !data.dashboard?.note }, () => data.dashboard?.note || ""),
        () => (data.dashboard ? t.div({ rid: "vars-" + data.dashboard.id }, variablesBar(() => data.dashboard?.variables || [], varsProxy, varsChanged)) : null),
        t.div(
            { className: "alert info gp-edit-hint", hidden: () => !data.editing },
            t.i({ className: "ri-information-line", ariaHidden: true }),
            t.span(null, "Drag a panel by its header to move it, and by its bottom right corner to resize it."),
        ),
        t.div(
            {
                className: "gp-grid-wrap",
                onmount: (el) => (gridWrap = el),
                onunmount: () => {
                    clearInterval(timer);
                    autoRefresh.unwatch();
                },
            },
            () => {
                if (data.isLoading && !data.dashboard) {
                    return t.div({ rid: "loading", className: "txt-center p-base" }, t.span({ className: "loader" }));
                }
                if (!data.chartsReady) {
                    return t.div({ rid: "charts", className: "txt-center p-base" }, t.span({ className: "loader" }));
                }
                return t.div({ rid: "grid" }, grid);
            },
        ),
    );
}

// --- drill-down filters -------------------------------------------------------------------

const quote = (v) => JSON.stringify(String(v ?? ""));

/** Client-side mirror of the server date range presets (drill-down links). */
function rangeBounds(value) {
    const v = value && typeof value == "object" ? value : { preset: value || "all" };
    const now = new Date();
    const iso = (d) => d.toISOString().replace("T", " ");
    const day = new Date(Date.UTC(now.getUTCFullYear(), now.getUTCMonth(), now.getUTCDate()));
    const days = { "24h": 1, "7d": 7, "30d": 30, "90d": 90, "365d": 365 };
    switch (v.preset) {
        case "today":
            return [iso(day), iso(new Date(day.getTime() + 86400000))];
        case "this_month":
            return [iso(new Date(Date.UTC(now.getUTCFullYear(), now.getUTCMonth(), 1))), iso(now)];
        case "this_year":
            return [iso(new Date(Date.UTC(now.getUTCFullYear(), 0, 1))), iso(now)];
        case "custom": {
            const from = v.from ? v.from + " 00:00:00.000Z" : "0001-01-01 00:00:00.000Z";
            const to = v.to ? iso(new Date(Date.parse(v.to + "T00:00:00Z") + 86400000)) : "9999-12-31 23:59:59.999Z";
            return [from, to];
        }
    }
    if (days[v.preset]) {
        return [iso(new Date(now.getTime() - days[v.preset] * 86400000)), iso(now)];
    }
    return ["0001-01-01 00:00:00.000Z", "9999-12-31 23:59:59.999Z"];
}

/** A panel filter with its variables replaced by literal values (for a link). */
function literalFilter(filter, defs, values) {
    return filter.replace(/\{\{\s*\$([A-Za-z_][A-Za-z0-9_]*)(?:\.(from|to))?\s*\}\}/g, (_, key, part) => {
        const def = defs.find((d) => d.key == key);
        if (!def) {
            return '""';
        }
        if (def.type == "date_range") {
            const [from, to] = rangeBounds(values[key] ?? def.default);
            return quote(part == "to" ? to : from);
        }
        const v = values[key] ?? def.default ?? "";
        return def.type == "number" && v !== "" ? String(Number(v)) : quote(v);
    });
}

function fieldFilter(c, name, key) {
    const f = c.fields.find((x) => x.name == name);
    if (!f) {
        return "";
    }
    if (key === null || key === undefined || key === "") {
        return f.type == "bool" ? `${name} = false` : `${name} = ""`;
    }
    switch (f.type) {
        case "bool":
            return `${name} = ${key == "1" || key === true}`;
        case "number":
            return `${name} = ${Number(key)}`;
        case "date":
        case "autodate":
            return bucketFilter(name, key, "day");
    }
    if ((f.type == "select" || f.type == "relation") && (f.maxSelect || 1) > 1) {
        return `${name} ?= ${quote(key)}`;
    }
    return `${name} = ${quote(key)}`;
}

/** Filter of a time bucket ("2026-10-04", "2026-10", "2026-10-04 08:00"...). */
function bucketFilter(field, bucket, precision) {
    const parts = String(bucket).split(/[- :]/).map(Number);
    const start = new Date(Date.UTC(parts[0], (parts[1] || 1) - 1, parts[2] || 1, parts[3] || 0));
    const end = new Date(start);
    switch (precision) {
        case "hour":
            end.setUTCHours(end.getUTCHours() + 1);
            break;
        case "week":
            end.setUTCDate(end.getUTCDate() + 7);
            break;
        case "month":
            end.setUTCMonth(end.getUTCMonth() + 1);
            break;
        case "year":
            end.setUTCFullYear(end.getUTCFullYear() + 1);
            break;
        default:
            end.setUTCDate(end.getUTCDate() + 1);
    }
    const iso = (d) => d.toISOString().replace("T", " ");
    return `${field} >= ${quote(iso(start))} && ${field} < ${quote(iso(end))}`;
}

// --- panel editor ----------------------------------------------------------------------

const defaultOptions = {
    metric: { fn: "count" },
    meter: { fn: "count" },
    time_series: { fn: "count", precision: "day", range: "30d", style: "line" },
    bar: { fn: "count", limit: 10 },
    pie: { fn: "count", limit: 8, donut: true },
    list: { limit: 10 },
    label: { text: "", size: "lg" },
};

const fieldTypeLabel = (f) => `${f.name} · ${f.type}`;

/** Fields usable by an aggregate function. */
function fieldsForFn(collection, fn) {
    const fields = (collection?.fields || []).filter((f) => f.type != "password" && f.name != "tokenKey");
    switch (fn) {
        case "sum":
        case "avg":
            return fields.filter((f) => f.type == "number");
        case "min":
        case "max":
            return fields.filter((f) => ["number", "date", "autodate"].includes(f.type));
        case "count_distinct":
            return fields.filter((f) => !["json", "editor", "file"].includes(f.type));
    }
    return [];
}

const groupableTypes = ["text", "email", "url", "select", "bool", "number", "relation", "date", "autodate"];

/**
 * Opens the panel editor: a type picker for new panels, then the type options
 * with data type aware field pickers, suggestions and a live preview.
 * Resolves with the edited panel (without position) or null.
 */
function openPanelEditor(panel, ctx = { variables: [], vars: {} }) {
    const isNew = !panel;
    const data = store({
        step: isNew ? "type" : "options",
        form: panel
            ? clone({ type: panel.type, name: panel.name, icon: panel.icon, color: panel.color, show_header: panel.show_header, note: panel.note, options: panel.options || {} })
            : { type: "", name: "", icon: "", color: "", show_header: true, note: "", options: {} },
        collections: [],
        preview: { data: null, error: "", loading: false },
    });

    userCollections().then((list) => (data.collections = list)).catch(handleError);

    const collection = () => data.collections.find((c) => c.name == data.form.options.collection);
    const o = () => data.form.options;
    const set = (key, value) => (data.form.options[key] = value);

    let resolve;
    const result = new Promise((r) => (resolve = r));
    let done = false;
    const finish = (value) => {
        if (!done) {
            done = true;
            resolve(value);
        }
    };

    function chooseType(type) {
        data.form.type = type;
        data.form.options = { ...clone(defaultOptions[type] || {}), collection: data.form.options.collection };
        data.form.show_header = type != "label";
        data.step = "options";
    }

    function applySuggestion(s) {
        data.form.options = { ...clone(defaultOptions[data.form.type] || {}), collection: o().collection, ...clone(s.options) };
        if (s.name && (!data.form.name || data.form.name == data.lastSuggestedName)) {
            data.form.name = s.name;
            data.lastSuggestedName = s.name;
        }
    }

    // smart defaults when the collection changes
    function onCollectionChange(name) {
        set("collection", name);
        const c = data.collections.find((x) => x.name == name);
        const fields = c?.fields || [];
        const has = (n) => fields.some((f) => f.name == n);
        if (data.form.type == "time_series") {
            const dateField = has("created") ? "created" : fields.find((f) => f.type == "date" || f.type == "autodate")?.name || "";
            set("dateField", dateField);
        }
        if (data.form.type == "list") {
            set("fields", []);
            set("sort", has("created") ? "-created" : "");
        }
        if (["bar", "pie"].includes(data.form.type)) {
            set("groupField", fields.find((f) => f.type == "select" || f.type == "bool")?.name || "");
        }
        if (o().field && !fields.some((f) => f.name == o().field)) {
            set("field", "");
        }
    }

    const runPreview = debounce(async () => {
        const type = data.form.type;
        if (!type || type == "label") {
            data.preview = { data: null, error: "", loading: false };
            return;
        }
        data.preview.loading = true;
        try {
            const res = await api("/api/dashboards/preview", {
                method: "POST",
                body: { type, options: data.form.options, variables: ctx.variables, vars: ctx.vars },
                requestKey: "gpPanelEditorPreview",
            });
            data.preview = { data: res.data ?? null, error: res.error || "", loading: false };
        } catch (err) {
            if (!err?.isAbort) {
                data.preview = { data: null, error: err?.message || "Failed to load the data.", loading: false };
            }
        }
    }, 350);

    // ---- form pieces

    const textOpt = (label, key, attrs = {}) =>
        field({ label, hint: attrs.hint }, t.input({ type: attrs.type || "text", placeholder: attrs.placeholder || "", value: () => o()[key] ?? "", oninput: (e) => set(key, attrs.type == "number" ? (e.target.value === "" ? "" : Number(e.target.value)) : e.target.value), className: attrs.className || "" }));

    const switchOpt = (label, key, def = false) => {
        const elId = "gpOpt_" + key;
        return t.div(
            { className: "field gp-switch-field" },
            t.input({ id: elId, type: "checkbox", className: "switch", checked: () => o()[key] ?? def, onchange: (e) => set(key, e.target.checked) }),
            t.label({ htmlFor: elId }, t.span({ className: "txt" }, label)),
        );
    };

    const choiceOpt = (label, key, choices, attrs = {}) =>
        field({ label, hint: attrs.hint }, select({ required: true, options: choices, value: () => String(o()[key] ?? choices[0].value), onchange: (v) => set(key, v) }));

    const collectionField = () =>
        field({ label: "Collection" }, select({
            required: true,
            placeholder: "- Select collection -",
            options: () => data.collections.map((c) => ({ value: c.name, label: c.name })),
            value: () => o().collection,
            onchange: onCollectionChange,
        }));

    const fnField = () =>
        field({ label: "Function" }, select({
            required: true,
            options: Object.entries(aggregateFunctions).map(([value, label]) => ({ value, label })),
            value: () => o().fn || "count",
            onchange: (v) => {
                set("fn", v);
                const allowed = fieldsForFn(collection(), v).map((f) => f.name);
                if (!allowed.includes(o().field)) {
                    set("field", allowed[0] || "");
                }
            },
        }));

    const valueField = () =>
        t.div(
            { hidden: () => !o().fn || o().fn == "count" },
            field({ label: "Field" }, select({
                required: true,
                placeholder: () => (fieldsForFn(collection(), o().fn).length ? "- Select field -" : "No compatible field"),
                options: () => fieldsForFn(collection(), o().fn).map((f) => ({ value: f.name, label: fieldTypeLabel(f) })),
                value: () => o().field,
                onchange: (v) => set("field", v),
            })),
        );

    // snippet of a dashboard variable for the filter (the field is guessed, editable)
    function variableSnippet(v) {
        const fields = collection()?.fields || [];
        if (v.type == "date_range") {
            const df = fields.find((f) => f.name == o().dateField) || fields.find((f) => f.name == "created") || fields.find((f) => f.type == "date" || f.type == "autodate");
            const name = df?.name || "created";
            return `${name} >= {{$${v.key}.from}} && ${name} < {{$${v.key}.to}}`;
        }
        const target = fields.find((f) => f.name == v.key)?.name || fields.find((f) => f.type == "select")?.name || "field";
        return `({{$${v.key}}} = "" || ${target} = {{$${v.key}}})`;
    }

    const filterField = () =>
        t.div(
            { className: "gp-filter-field" },
            field(
                { label: "Filter", hint: "PocketBase filter syntax, e.g. status = \"paid\" && created >= @todayStart" },
                t.input({ type: "text", className: "txt-code", placeholder: "Optional", value: () => o().filter || "", oninput: (e) => set("filter", e.target.value) }),
            ),
            ctx.variables.length
                ? t.div({ className: "field gp-bare gp-insert-var" }, select({
                    placeholder: "{{ }} Insert a dashboard variable",
                    options: ctx.variables.map((v) => ({ value: v.key, label: `${v.label || v.key} ($${v.key}, ${variableTypes[v.type]?.label || v.type})` })),
                    value: () => "",
                    onchange: (key) => {
                        const v = ctx.variables.find((x) => x.key == key);
                        if (v) {
                            const current = (o().filter || "").trim();
                            set("filter", current ? `${current} && ${variableSnippet(v)}` : variableSnippet(v));
                        }
                    },
                }))
                : null,
        );

    // split a chart into one series per value of a field
    const splitFields = () =>
        (collection()?.fields || [])
            .filter((f) => groupableTypes.includes(f.type) && f.name != "id" && f.type != "date" && f.type != "autodate" && !((f.type == "select" || f.type == "relation") && (f.maxSelect || 1) > 1))
            .map((f) => ({ value: f.name, label: fieldTypeLabel(f) }));
    const splitOptions = () => [
        t.div({ className: "col-lg-6" }, field({ label: "Split by", hint: "One series per value (the biggest ones, the others in \"Other\")." }, select({
            placeholder: "No split",
            options: splitFields,
            value: () => o().splitField || "",
            onchange: (v) => set("splitField", v),
        }))),
        t.div({ className: "col-lg-6", hidden: () => !o().splitField }, textOpt("Max series", "maxSeries", { type: "number", placeholder: "5" })),
    ];

    const numberFormat = () => [
        t.div({ className: "col-lg-4" }, textOpt("Decimals", "decimals", { type: "number", placeholder: "Auto" })),
        t.div({ className: "col-lg-4" }, textOpt("Prefix", "prefix", { placeholder: "e.g. $" })),
        t.div({ className: "col-lg-4" }, textOpt("Suffix", "suffix", { placeholder: "e.g. %" })),
    ];

    const precisionChoices = [
        { value: "hour", label: "Hour" },
        { value: "day", label: "Day" },
        { value: "week", label: "Week" },
        { value: "month", label: "Month" },
        { value: "year", label: "Year" },
    ];
    const rangeChoices = [
        { value: "24h", label: "Last 24 hours" },
        { value: "7d", label: "Last 7 days" },
        { value: "30d", label: "Last 30 days" },
        { value: "90d", label: "Last 90 days" },
        { value: "365d", label: "Last 365 days" },
        { value: "", label: "All time" },
        // the dashboard date range variables
        ...ctx.variables.filter((v) => v.type == "date_range").map((v) => ({ value: "$" + v.key, label: `Dashboard: ${v.label || v.key}` })),
    ];

    function conditionsEditor() {
        const ops = [">", ">=", "<", "<=", "=", "!="].map((op) => ({ value: op, label: op }));
        return t.div(
            { className: "gp-conditions" },
            t.label({ className: "gp-label" }, "Conditional colors"),
            () =>
                (o().conditions || []).map((c, i) =>
                    t.div(
                        { rid: "c" + i, className: "gp-condition-row" },
                        t.span({ className: "txt-hint txt-sm" }, "When value"),
                        t.div({ className: "field gp-bare" }, select({ required: true, options: ops, value: () => c.op || ">", onchange: (v) => (c.op = v) })),
                        t.div({ className: "field gp-bare" }, t.input({ type: "number", value: () => c.value ?? "", oninput: (e) => (c.value = e.target.value) })),
                        t.div({ className: "field gp-bare" }, select({ required: true, options: colorChoices.filter((x) => x.value), value: () => c.color || "danger", onchange: (v) => (c.color = v) })),
                        t.button({ type: "button", className: "btn sm circle transparent danger", title: "Remove", onclick: () => set("conditions", o().conditions.filter((_, j) => j != i)) }, t.i({ className: "ri-close-line", ariaHidden: true })),
                    )
                ),
            t.button(
                { type: "button", className: "btn sm transparent secondary", onclick: () => set("conditions", [...(o().conditions || []), { op: ">", value: "", color: "danger" }]) },
                t.i({ className: "ri-add-line", ariaHidden: true }),
                t.span({ className: "txt" }, "Add condition"),
            ),
        );
    }

    function suggestionsBlock() {
        return t.div(
            { className: "gp-suggestions", hidden: () => !collection() || !panelSuggestions(data.form.type, collection(), data.collections).length },
            t.label({ className: "gp-label" }, t.i({ className: "ri-magic-line", ariaHidden: true }), " Suggestions for ", () => o().collection || ""),
            t.div(
                { className: "gp-suggestion-list" },
                () =>
                    panelSuggestions(data.form.type, collection(), data.collections).map((s) =>
                        t.button(
                            { type: "button", className: "gp-suggestion", title: s.hint || "", onclick: () => applySuggestion(s) },
                            t.i({ className: s.icon || panelTypes[data.form.type]?.icon, ariaHidden: true }),
                            t.span(null, s.label),
                        )
                    ),
            ),
        );
    }

    function typeOptions() {
        const type = data.form.type;
        const dataSection = (...rows) => [
            t.h6({ className: "gp-section-title" }, "Data"),
            t.div({ className: "grid sm" }, t.div({ className: "col-lg-12" }, collectionField()), t.div({ className: "col-lg-12" }, suggestionsBlock()), ...rows),
        ];
        switch (type) {
            case "metric":
                return [
                    ...dataSection(t.div({ className: "col-lg-6" }, fnField()), t.div({ className: "col-lg-6" }, valueField()), t.div({ className: "col-lg-12" }, filterField())),
                    t.h6({ className: "gp-section-title" }, "Display"),
                    t.div(
                        { className: "grid sm" },
                        ...numberFormat(),
                        t.div({ className: "col-lg-6" }, choiceOpt("Alignment", "align", [{ value: "center", label: "Center" }, { value: "left", label: "Left" }, { value: "right", label: "Right" }])),
                        t.div({ className: "col-lg-6" }, switchOpt("Abbreviate large numbers (1.2K)", "abbreviate")),
                        t.div({ className: "col-lg-12" }, conditionsEditor()),
                    ),
                ];
            case "meter":
                return [
                    ...dataSection(t.div({ className: "col-lg-6" }, fnField()), t.div({ className: "col-lg-6" }, valueField()), t.div({ className: "col-lg-12" }, filterField())),
                    t.h6({ className: "gp-section-title" }, "Display"),
                    t.div({ className: "grid sm" }, t.div({ className: "col-lg-12" }, textOpt("Target (max)", "max", { type: "number", placeholder: "Automatic", hint: "The value of a full gauge. Empty: a round number above the value." })), ...numberFormat()),
                ];
            case "time_series":
                return [
                    ...dataSection(
                        t.div({ className: "col-lg-6" }, field({ label: "Date field" }, select({
                            required: true,
                            placeholder: () => ((collection()?.fields || []).some((f) => f.type == "date" || f.type == "autodate") ? "- Select field -" : "No date field"),
                            options: () => (collection()?.fields || []).filter((f) => f.type == "date" || f.type == "autodate").map((f) => ({ value: f.name, label: fieldTypeLabel(f) })),
                            value: () => o().dateField,
                            onchange: (v) => set("dateField", v),
                        }))),
                        t.div({ className: "col-lg-3" }, choiceOpt("Group by", "precision", precisionChoices)),
                        t.div({ className: "col-lg-3" }, choiceOpt("Range", "range", rangeChoices)),
                        t.div({ className: "col-lg-6" }, fnField()),
                        t.div({ className: "col-lg-6" }, valueField()),
                        ...splitOptions(),
                        t.div({ className: "col-lg-12" }, filterField()),
                    ),
                    t.h6({ className: "gp-section-title" }, "Display"),
                    t.div(
                        { className: "grid sm" },
                        t.div({ className: "col-lg-6" }, choiceOpt("Style", "style", [{ value: "line", label: "Line" }, { value: "area", label: "Area" }, { value: "bar", label: "Bars" }])),
                        t.div({ className: "col-lg-6" }, choiceOpt("Curve", "curve", [{ value: "straight", label: "Straight" }, { value: "smooth", label: "Smooth" }, { value: "step", label: "Step" }])),
                        t.div({ className: "col-lg-12", hidden: () => !o().splitField }, switchOpt("Stacked series", "stacked")),
                        ...numberFormat(),
                    ),
                ];
            case "bar":
            case "pie":
                return [
                    ...dataSection(
                        t.div({ className: "col-lg-12" }, field({ label: "Group by", hint: "Relations show the related record names; multiple values count once per value." }, select({
                            required: true,
                            placeholder: "- Select field -",
                            options: () => (collection()?.fields || []).filter((f) => groupableTypes.includes(f.type) && f.name != "id").map((f) => ({ value: f.name, label: fieldTypeLabel(f) })),
                            value: () => o().groupField,
                            onchange: (v) => set("groupField", v),
                        }))),
                        t.div({ className: "col-lg-6" }, fnField()),
                        t.div({ className: "col-lg-6" }, valueField()),
                        t.div({ className: "col-lg-6" }, textOpt("Max groups", "limit", { type: "number", placeholder: "10" })),
                        t.div({ className: "col-lg-6" }, choiceOpt("Order", "order", [{ value: "value", label: "Biggest first" }, { value: "label", label: "Alphabetical" }])),
                        ...(type == "bar" ? splitOptions() : []),
                        t.div({ className: "col-lg-12" }, filterField()),
                    ),
                    t.h6({ className: "gp-section-title" }, "Display"),
                    type == "bar"
                        ? t.div(
                            { className: "grid sm" },
                            t.div({ className: "col-lg-6" }, switchOpt("Horizontal bars", "horizontal")),
                            t.div({ className: "col-lg-6" }, switchOpt("Show values", "showValues")),
                            t.div({ className: "col-lg-12", hidden: () => !o().splitField }, switchOpt("Stacked series", "stacked")),
                            ...numberFormat(),
                        )
                        : t.div({ className: "grid sm" }, t.div({ className: "col-lg-4" }, switchOpt("Donut", "donut")), t.div({ className: "col-lg-4" }, switchOpt("Legend", "legend", true)), t.div({ className: "col-lg-4" }, switchOpt("Percent labels", "labels")), ...numberFormat()),
                ];
            case "list":
                return [
                    ...dataSection(
                        t.div({ className: "col-lg-12" }, field({ label: "Columns", hint: "Leave empty for the first fields." }, multiSelect({
                            max: 10,
                            placeholder: "Default columns",
                            options: () => (collection()?.fields || []).filter((f) => f.type != "password" && f.name != "tokenKey").map((f) => ({ value: f.name, label: fieldTypeLabel(f) })),
                            value: () => o().fields || [],
                            onchange: (v) => set("fields", v),
                        }))),
                        t.div({ className: "col-lg-6" }, field({ label: "Sort by" }, select({
                            placeholder: "Newest first",
                            options: () => (collection()?.fields || []).filter((f) => !["json", "editor", "file", "password"].includes(f.type)).flatMap((f) => [
                                { value: "-" + f.name, label: `${f.name} ↓ (descending)` },
                                { value: f.name, label: `${f.name} ↑ (ascending)` },
                            ]),
                            value: () => o().sort || "",
                            onchange: (v) => set("sort", v),
                        }))),
                        t.div({ className: "col-lg-6" }, textOpt("Limit", "limit", { type: "number", placeholder: "10" })),
                        t.div({ className: "col-lg-12" }, filterField()),
                    ),
                    t.h6({ className: "gp-section-title" }, "Display"),
                    t.div({ className: "grid sm" }, t.div({ className: "col-lg-12" }, switchOpt("Column headers", "headers", true))),
                ];
            case "label":
                return [
                    t.h6({ className: "gp-section-title" }, "Text"),
                    t.div(
                        { className: "grid sm" },
                        t.div({ className: "col-lg-12" }, field({ label: "Text" }, t.textarea({ rows: 3, value: () => o().text || "", oninput: (e) => set("text", e.target.value) }))),
                        t.div({ className: "col-lg-6" }, choiceOpt("Size", "size", [{ value: "sm", label: "Small" }, { value: "md", label: "Medium" }, { value: "lg", label: "Large" }, { value: "xl", label: "Extra large" }])),
                        t.div({ className: "col-lg-6" }, choiceOpt("Alignment", "align", [{ value: "left", label: "Left" }, { value: "center", label: "Center" }, { value: "right", label: "Right" }])),
                    ),
                ];
        }
        return [];
    }

    // live preview panel (same rendering as the dashboard)
    const previewPanel = {
        get type() {
            return data.form.type;
        },
        get name() {
            return data.form.name;
        },
        get color() {
            return data.form.color;
        },
        get options() {
            return data.form.options;
        },
    };

    // read through getters: data.preview is replaced on every result
    const previewState = {
        get data() {
            return data.preview.data;
        },
        get error() {
            return data.preview.error;
        },
        get loading() {
            return data.preview.loading;
        },
    };

    const modal = t.div(
        {
            className: () => `modal popup gp-panel-modal ${data.step == "type" ? "step-type" : ""}`,
            onmount: () => {
                modal._w = watch(() => JSON.stringify([data.form.type, data.form.options, data.step]), () => {
                    if (data.step == "options") {
                        runPreview();
                    }
                });
            },
            onunmount: () => {
                modal._w?.unwatch();
                runPreview.cancel();
                finish(null);
            },
        },
        t.header(
            { className: "modal-header" },
            t.h5({ className: "m-auto" }, () => (data.step == "type" ? "Add a panel" : `${isNew ? "New" : "Edit"} ${panelTypes[data.form.type]?.label || "panel"}`)),
        ),
        t.div(
            { className: "modal-content" },
            () => {
                if (data.step == "type") {
                    return t.div(
                        { rid: "types", className: "gp-type-picker" },
                        Object.entries(panelTypes).map(([type, def]) =>
                            t.button(
                                { type: "button", className: "gp-type-card", onclick: () => chooseType(type) },
                                t.i({ className: def.icon, ariaHidden: true }),
                                t.strong(null, def.label),
                                t.span({ className: "txt-hint txt-sm" }, def.description),
                            )
                        ),
                    );
                }
                return t.div(
                    { rid: "options-" + data.form.type, className: "gp-panel-editor" },
                    t.div(
                        { className: "gp-panel-editor-form" },
                        t.div(
                            { className: "grid sm" },
                            t.div({ className: "col-lg-6" }, field({ label: "Type" }, select({
                                required: true,
                                options: Object.entries(panelTypes).map(([value, def]) => ({ value, label: def.label })),
                                value: () => data.form.type,
                                onchange: (v) => {
                                    const prev = data.form.options;
                                    data.form.type = v;
                                    data.form.options = { ...clone(defaultOptions[v] || {}), collection: prev.collection, filter: prev.filter, fn: prev.fn, field: prev.field };
                                },
                            }))),
                            t.div({ className: "col-lg-6" }, field({ label: "Name" }, t.input({ type: "text", placeholder: "Panel title", value: () => data.form.name, oninput: (e) => (data.form.name = e.target.value) }))),
                            t.div({ className: "col-lg-6" }, field({ label: "Icon" }, select({
                                placeholder: "No icon",
                                options: dashboardIcons.map((icon) => ({ value: icon, label: () => t.span({ className: "flex gap-5" }, t.i({ className: icon, ariaHidden: true }), icon.replace(/^ri-|-line$/g, "")) })),
                                value: () => data.form.icon,
                                onchange: (v) => (data.form.icon = v),
                            }))),
                            t.div({ className: "col-lg-6" }, field({ label: "Color" }, select({
                                required: true,
                                options: colorChoices,
                                value: () => data.form.color || "",
                                onchange: (v) => (data.form.color = v),
                            }))),
                            t.div({ className: "col-lg-8" }, field({ label: "Note" }, t.input({ type: "text", placeholder: "Shown on hover", value: () => data.form.note || "", oninput: (e) => (data.form.note = e.target.value) }))),
                            t.div({ className: "col-lg-4" }, t.div(
                                { className: "field gp-switch-field" },
                                t.input({ id: "gpShowHeader", type: "checkbox", className: "switch", checked: () => data.form.show_header, onchange: (e) => (data.form.show_header = e.target.checked) }),
                                t.label({ htmlFor: "gpShowHeader" }, t.span({ className: "txt" }, "Show header")),
                            )),
                        ),
                        typeOptions(),
                    ),
                    t.div(
                        { className: "gp-panel-editor-preview" },
                        t.label({ className: "gp-label" }, "Preview"),
                        t.div(
                            { className: () => `gp-panel gp-panel-${data.form.type} gp-preview-panel` },
                            t.header(
                                { className: "gp-panel-header", hidden: () => !data.form.show_header },
                                () => (data.form.icon ? t.i({ rid: data.form.icon, className: `${data.form.icon} gp-panel-icon`, ariaHidden: true }) : null),
                                t.span({ className: "gp-panel-name" }, () => data.form.name || panelTypes[data.form.type]?.label || ""),
                            ),
                            () => t.div({ rid: data.form.type, className: "gp-panel-body" }, panelBody(previewPanel, previewState)),
                        ),
                    ),
                );
            },
        ),
        t.footer(
            { className: "modal-footer" },
            t.button(
                {
                    type: "button",
                    className: "btn transparent secondary m-r-auto",
                    onclick: () => {
                        if (data.step == "options" && isNew) {
                            data.step = "type";
                            return;
                        }
                        app.modals.close(modal);
                    },
                },
                t.span({ className: "txt" }, () => (data.step == "options" && isNew ? "Back" : "Cancel")),
            ),
            t.button(
                {
                    type: "button",
                    className: "btn expanded",
                    hidden: () => data.step != "options",
                    onclick: () => {
                        finish(clone(data.form));
                        app.modals.close(modal);
                    },
                },
                t.span({ className: "txt" }, isNew ? "Add panel" : "Apply"),
            ),
        ),
    );

    // the modal is created on demand: remove it from the page once closed
    modal.onafterclose = () => {
        finish(null);
        modal.remove();
    };
    document.body.appendChild(modal);
    app.modals.open(modal);
    return result;
}
