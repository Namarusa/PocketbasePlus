// Collection page tools: saved views, export / import, bulk edit and the
// alternative layouts (cards, kanban, calendar, map) of the native records list.

import { api, field, handleError, select } from "./lib.js";

const extBase = app.pb.buildURL("/_/extensions/pocketbaseplus/");
const LAYOUT_KEY = "gpLayout_"; // + collection id

// --- url state ------------------------------------------------------------------------

// The collection page keeps its filter and sort in the url hash, updated with
// history.replaceState (no event): emit one so the layouts can follow it.
if (!history.__gpPatched) {
    history.__gpPatched = true;
    for (const method of ["pushState", "replaceState"]) {
        const original = history[method];
        history[method] = function(...args) {
            const result = original.apply(this, args);
            window.dispatchEvent(new Event("gp:urlchange"));
            return result;
        };
    }
}

const url = store({ href: window.location.href });
const syncUrl = () => (url.href = window.location.href);
window.addEventListener("gp:urlchange", syncUrl);
window.addEventListener("hashchange", syncUrl);

function hashParams() {
    url.href; // reactive dependency
    const hash = window.location.hash;
    const i = hash.indexOf("?");
    return new URLSearchParams(i >= 0 ? hash.slice(i + 1) : "");
}

const activeCollection = () => app.store.activeCollection;

// --- shared helpers ---------------------------------------------------------------------

/** Readable value of a record field (lists, dates, booleans). */
function display(record, f) {
    const v = record?.[f.name];
    if (v === null || v === undefined || v === "") {
        return "";
    }
    if (f.type == "relation") {
        const exp = record.expand?.[f.name];
        const list = Array.isArray(exp) ? exp : exp ? [exp] : [];
        if (list.length) {
            return list.map((r) => r.name || r.title || r.email || r.id).join(", ");
        }
    }
    if (Array.isArray(v)) {
        return v.join(", ");
    }
    if (typeof v == "boolean") {
        return v ? "Yes" : "No";
    }
    if (typeof v == "object") {
        return JSON.stringify(v);
    }
    if (f.type == "date" || f.type == "autodate") {
        const d = new Date(String(v).replace(" ", "T"));
        return isNaN(d) ? v : d.toLocaleString(undefined, { dateStyle: "medium", timeStyle: "short" });
    }
    if (f.type == "editor") {
        const div = document.createElement("div");
        div.innerHTML = v; // only used to extract the text (never inserted in the page)
        return div.textContent || "";
    }
    return String(v);
}

function titleField(c) {
    const fields = c?.fields || [];
    return (
        fields.find((f) => f.presentable)
        || fields.find((f) => ["name", "title", "label", "reference", "email", "username"].includes(f.name))
        || fields.find((f) => f.type == "text" && f.name != "id")
        || fields.find((f) => f.name == "id")
    );
}

function openRecord(c, record, onchange) {
    app.modals.openRecordUpsert(c, record, {
        onsave: () => onchange?.(),
        ondelete: () => onchange?.(),
    });
}

let fileToken = null;
async function getFileToken() {
    if (!fileToken) {
        fileToken = app.pb.files.getToken().catch(() => "");
        setTimeout(() => (fileToken = null), 60_000);
    }
    return fileToken;
}

// --- toolbar (views, layout, export, import) ------------------------------------------------

/** Adds the tools next to the native collection header buttons. */
export function mountToolbar(secondaryBtns) {
    if (secondaryBtns._gpTools) {
        return;
    }
    secondaryBtns._gpTools = true;
    secondaryBtns.appendChild(toolbar());
}

const views = store({ list: [], loadedFor: "" });

async function loadViews(force = false) {
    const c = activeCollection();
    if (!c?.id || (!force && views.loadedFor == c.id)) {
        return;
    }
    views.loadedFor = c.id;
    try {
        const me = app.store.superuser?.id;
        views.list = await app.pb.collection("saved_views").getFullList({
            filter: app.pb.filter("collection = {:c} && (owner = {:me} || shared = true)", { c: c.id, me }),
            sort: "name",
            requestKey: "gpSavedViews",
        });
    } catch (err) {
        handleError(err);
    }
}

function applyView(v) {
    const c = activeCollection();
    const q = v.query || {};
    if (q.columns && typeof q.columns == "object") {
        app.utils.saveLocalHistory(app.consts.COLUMNS_STORAGE_PREFIX + c.id, q.columns);
    }
    if (q.layout) {
        saveLayout(c.id, q.layout);
    }
    const params = new URLSearchParams({ collection: c.name });
    if (q.filter) params.set("filter", q.filter);
    if (q.sort) params.set("sort", q.sort);
    // a navigation re-creates the records list with the view settings
    window.location.hash = "/collections?" + params.toString();
}

function saveViewModal(existing = null) {
    const c = activeCollection();
    const params = hashParams();
    const form = store({ name: existing?.name || "", shared: !!existing?.shared, isSaving: false });

    async function save() {
        form.isSaving = true;
        const query = {
            filter: params.get("filter") || "",
            sort: params.get("sort") || "",
            columns: app.utils.getLocalHistory(app.consts.COLUMNS_STORAGE_PREFIX + c.id, {}),
            layout: getLayout(c.id),
        };
        try {
            const body = { name: form.name.trim(), shared: form.shared, collection: c.id };
            if (!existing) {
                body.query = query;
            }
            if (existing) {
                await app.pb.collection("saved_views").update(existing.id, body);
            } else {
                await app.pb.collection("saved_views").create(body);
            }
            app.toasts.success(existing ? "Successfully saved the view." : "Successfully saved the current view.");
            loadViews(true);
            app.modals.close(modal);
        } catch (err) {
            handleError(err);
        } finally {
            form.isSaving = false;
        }
    }

    const modal = t.div(
        { className: "modal popup sm gp-view-modal" },
        t.header({ className: "modal-header" }, t.h5({ className: "m-auto" }, existing ? "Edit view" : "Save the current view")),
        t.form(
            {
                id: "gpViewForm",
                className: "modal-content",
                onsubmit: (e) => {
                    e.preventDefault();
                    save();
                },
            },
            field({ label: "Name" }, t.input({ type: "text", required: true, autofocus: true, placeholder: "e.g. Unpaid orders", value: () => form.name, oninput: (e) => (form.name = e.target.value) })),
            existing
                ? null
                : t.div(
                    { className: "gp-view-summary txt-hint txt-sm" },
                    t.div(null, "Filter: ", t.code(null, params.get("filter") || "none")),
                    t.div(null, "Sort: ", t.code(null, params.get("sort") || "default")),
                    t.div(null, `Layout: ${getLayout(c.id).type}, with the visible columns.`),
                ),
            t.div(
                { className: "field m-t-sm" },
                t.input({ id: "gpViewShared", type: "checkbox", className: "switch", checked: () => form.shared, onchange: (e) => (form.shared = e.target.checked) }),
                t.label({ htmlFor: "gpViewShared" }, t.span({ className: "txt" }, "Shared with the other superusers")),
            ),
        ),
        t.footer(
            { className: "modal-footer" },
            t.button({ type: "button", className: "btn transparent secondary m-r-auto", onclick: () => app.modals.close(modal) }, t.span({ className: "txt" }, "Cancel")),
            t.button({ type: "submit", "html-form": "gpViewForm", className: () => `btn expanded ${form.isSaving ? "loading" : ""}`, disabled: () => form.isSaving }, t.span({ className: "txt" }, "Save")),
        ),
    );
    showModal(modal);
}

function removeView(v) {
    app.modals.confirm(`Delete the view "${v.name}"?`, async () => {
        try {
            await app.pb.collection("saved_views").delete(v.id);
            loadViews(true);
        } catch (err) {
            handleError(err);
        }
    });
}

function showModal(modal) {
    modal.onafterclose = () => modal.remove();
    document.body.appendChild(modal);
    app.modals.open(modal);
}

async function download(format) {
    const c = activeCollection();
    const params = hashParams();
    const q = new URLSearchParams({ format });
    if (params.get("filter")) q.set("filter", params.get("filter"));
    if (params.get("sort")) q.set("sort", params.get("sort"));
    // the visible columns (all when none are hidden)
    const prefs = app.utils.getLocalHistory(app.consts.COLUMNS_STORAGE_PREFIX + c.id, {});
    const visible = c.fields.filter((f) => f.type != "password" && f.name != "tokenKey" && prefs[f.id] !== false && !(f.hidden && prefs[f.id] === undefined)).map((f) => f.name);
    if (Object.keys(prefs).length) {
        q.set("fields", visible.join(","));
    }
    try {
        const res = await fetch(app.pb.buildURL(`/api/gp/export/${encodeURIComponent(c.name)}?${q}`), {
            headers: { Authorization: app.pb.authStore.token },
        });
        if (!res.ok) {
            const body = await res.json().catch(() => ({}));
            throw new Error(body.message || `Export failed (${res.status}).`);
        }
        const blob = await res.blob();
        const name = (res.headers.get("Content-Disposition") || "").match(/filename="([^"]+)"/)?.[1] || `${c.name}.${format}`;
        const a = document.createElement("a");
        a.href = URL.createObjectURL(blob);
        a.download = name;
        a.click();
        setTimeout(() => URL.revokeObjectURL(a.href), 10_000);
    } catch (err) {
        app.toasts.error(err.message);
    }
}

function toolbar() {
    const viewsId = "gpViewsDropdown";
    const exportId = "gpExportDropdown";
    const layoutId = "gpLayoutDropdown";
    const closeDropdown = (e) => e.currentTarget.closest(".dropdown")?.hidePopover();

    return t.div(
        {
            className: "gp-records-tools",
            hidden: () => !activeCollection()?.id,
            onmount: () => {
                // reload the views when the collection changes
                toolbar._w = watch(() => activeCollection()?.id, () => loadViews());
            },
            onunmount: () => toolbar._w?.unwatch(),
        },
        // views
        t.button(
            { type: "button", className: "btn sm transparent secondary gp-tool-btn", "html-popovertarget": viewsId, title: "Saved views" },
            t.i({ className: "ri-bookmark-line", ariaHidden: true }),
            t.span({ className: "txt" }, "Views"),
        ),
        t.div(
            { id: viewsId, popover: "auto", className: "dropdown gp-views-dropdown" },
            () => {
                const me = app.store.superuser?.id;
                const list = views.list;
                return t.div(
                    { rid: "views-" + list.map((v) => v.id + v.updated).join() },
                    list.length
                        ? list.map((v) =>
                            t.div(
                                { className: "gp-view-item" },
                                t.button(
                                    { type: "button", className: "dropdown-item", onclick: (e) => (closeDropdown(e), applyView(v)) },
                                    t.i({ className: v.shared ? "ri-share-line" : "ri-bookmark-line", ariaHidden: true }),
                                    t.span({ className: "txt" }, v.name),
                                ),
                                v.owner == me
                                    ? t.div(
                                        { className: "gp-view-actions" },
                                        t.button({ type: "button", className: "btn sm circle transparent secondary", title: "Edit", onclick: (e) => (closeDropdown(e), saveViewModal(v)) }, t.i({ className: "ri-pencil-line", ariaHidden: true })),
                                        t.button({ type: "button", className: "btn sm circle transparent danger", title: "Delete", onclick: (e) => (closeDropdown(e), removeView(v)) }, t.i({ className: "ri-delete-bin-7-line", ariaHidden: true })),
                                    )
                                    : null,
                            )
                        )
                        : t.div({ className: "txt-hint txt-sm p-sm" }, "No saved views yet."),
                    t.hr(),
                    t.button(
                        { type: "button", className: "dropdown-item", onclick: (e) => (closeDropdown(e), saveViewModal()) },
                        t.i({ className: "ri-add-line", ariaHidden: true }),
                        t.span({ className: "txt" }, "Save the current view…"),
                    ),
                );
            },
        ),
        // layout
        t.button(
            { type: "button", className: "btn sm transparent secondary gp-tool-btn", "html-popovertarget": layoutId, title: "Layout" },
            t.i({ className: () => layoutTypes[layoutOf()]?.icon || "ri-table-line", ariaHidden: true }),
            t.span({ className: "txt" }, () => layoutTypes[layoutOf()]?.label || "Table"),
        ),
        t.div(
            { id: layoutId, popover: "auto", className: "dropdown gp-layout-dropdown" },
            () => {
                const c = activeCollection();
                return t.div(
                    { rid: "layouts-" + (c?.id || "") + layoutOf() },
                    Object.entries(layoutTypes).map(([type, def]) => {
                        const unavailable = def.requires && !c?.fields?.some(def.requires);
                        return t.button(
                            {
                                type: "button",
                                className: `dropdown-item ${layoutOf() == type ? "active" : ""}`,
                                disabled: !!unavailable,
                                title: unavailable ? def.missing : "",
                                onclick: (e) => {
                                    closeDropdown(e);
                                    setLayoutType(type);
                                },
                            },
                            t.i({ className: def.icon, ariaHidden: true }),
                            t.span({ className: "txt" }, def.label),
                            unavailable ? t.span({ className: "txt-hint txt-sm m-l-auto" }, def.missing) : null,
                        );
                    }),
                    t.hr(),
                    t.button(
                        { type: "button", className: "dropdown-item", disabled: layoutOf() == "table", onclick: (e) => (closeDropdown(e), layoutSettings()) },
                        t.i({ className: "ri-settings-3-line", ariaHidden: true }),
                        t.span({ className: "txt" }, "Layout settings…"),
                    ),
                );
            },
        ),
        // export / import
        t.button(
            { type: "button", className: "btn sm transparent secondary gp-tool-btn", "html-popovertarget": exportId, title: "Export the filtered records" },
            t.i({ className: "ri-download-2-line", ariaHidden: true }),
            t.span({ className: "txt" }, "Export"),
        ),
        t.div(
            { id: exportId, popover: "auto", className: "dropdown nowrap" },
            [["csv", "CSV", "ri-file-text-line"], ["xlsx", "Excel (XLSX)", "ri-file-excel-2-line"], ["json", "JSON", "ri-braces-line"]].map(([format, label, icon]) =>
                t.button({ type: "button", className: "dropdown-item", onclick: (e) => (closeDropdown(e), download(format)) }, t.i({ className: icon, ariaHidden: true }), t.span({ className: "txt" }, label))
            ),
        ),
        t.button(
            {
                type: "button",
                className: "btn sm transparent secondary gp-tool-btn",
                hidden: () => activeCollection()?.type == "view",
                ariaDescription: app.attrs.tooltip("Import records from a file", "bottom"),
                onclick: () => importModal(),
            },
            t.i({ className: "ri-upload-2-line", ariaHidden: true }),
            t.span({ className: "txt" }, "Import"),
        ),
    );
}

// --- import ------------------------------------------------------------------------------

function importModal() {
    const c = activeCollection();
    const data = store({
        file: null,
        preview: null, // { columns, sample, total, mapping }
        mapping: {},
        mode: "create",
        matchField: "id",
        result: null,
        isLoading: false,
    });
    const targets = c.fields.filter((f) => !["autodate", "file"].includes(f.type) && f.name != "tokenKey");

    function form(dryRun) {
        const fd = new FormData();
        fd.append("file", data.file);
        fd.append("options", JSON.stringify({ mapping: data.mapping, mode: data.mode, matchField: data.matchField, dryRun }));
        return fd;
    }

    async function loadPreview(file) {
        data.file = file;
        data.result = null;
        data.isLoading = true;
        try {
            const fd = new FormData();
            fd.append("file", file);
            data.preview = await api(`/api/gp/import/${encodeURIComponent(c.name)}/preview`, { method: "POST", body: fd });
            data.mapping = { ...data.preview.mapping };
        } catch (err) {
            data.preview = null;
            handleError(err);
        } finally {
            data.isLoading = false;
        }
    }

    async function run(dryRun) {
        data.isLoading = true;
        try {
            data.result = await api(`/api/gp/import/${encodeURIComponent(c.name)}`, { method: "POST", body: form(dryRun) });
            if (data.result.saved) {
                app.toasts.success(`Imported ${data.result.created} new and ${data.result.updated} updated records.`);
                app.modals.close(modal);
                window.dispatchEvent(new Event("gp:recordschanged"));
                document.querySelector(".page-header-secondary-btns .rotate-btn")?.click(); // refresh the list
            }
        } catch (err) {
            handleError(err);
        } finally {
            data.isLoading = false;
        }
    }

    const modal = t.div(
        { className: "modal popup lg gp-import-modal" },
        t.header({ className: "modal-header" }, t.h5({ className: "m-auto" }, `Import into ${c.name}`)),
        t.div(
            { className: "modal-content" },
            t.div(
                { className: "field" },
                t.label({ htmlFor: "gpImportFile" }, "File (CSV, Excel XLSX or JSON)"),
                t.input({ id: "gpImportFile", type: "file", accept: ".csv,.tsv,.txt,.xlsx,.json", onchange: (e) => e.target.files[0] && loadPreview(e.target.files[0]) }),
            ),
            t.div({ className: "field-help" }, "First row = column names. Multiple values (select, relation) are separated by \";\". Files can't be imported; nothing is saved if a row is invalid."),
            () => {
                const p = data.preview;
                if (!p) {
                    return data.isLoading ? t.div({ rid: "loading", className: "txt-center p-sm" }, t.span({ className: "loader" })) : null;
                }
                return t.div(
                    { rid: "preview-" + p.columns.join("|") + p.total },
                    t.h6({ className: "gp-section-title" }, `Columns (${p.total} rows)`),
                    t.table(
                        { className: "gp-import-mapping" },
                        t.thead(null, t.tr(null, t.th(null, "Column"), t.th(null, "Example"), t.th(null, "Field"))),
                        t.tbody(
                            null,
                            p.columns.map((col) =>
                                t.tr(
                                    null,
                                    t.td({ className: "txt-bold" }, col),
                                    t.td({ className: "txt-hint gp-import-sample" }, String(p.sample?.[0]?.[col] ?? "")),
                                    t.td(null, t.div({ className: "field gp-bare" }, select({
                                        placeholder: "Skip",
                                        options: targets.map((f) => ({ value: f.name, label: `${f.name} · ${f.type}` })),
                                        value: () => data.mapping[col] || "",
                                        onchange: (v) => (data.mapping[col] = v),
                                    }))),
                                )
                            ),
                        ),
                    ),
                    t.div(
                        { className: "grid sm m-t-sm" },
                        t.div({ className: "col-lg-6" }, field({ label: "Mode" }, select({
                            required: true,
                            options: [{ value: "create", label: "Create new records" }, { value: "upsert", label: "Update existing, create the others" }],
                            value: () => data.mode,
                            onchange: (v) => (data.mode = v),
                        }))),
                        t.div({ className: "col-lg-6", hidden: () => data.mode != "upsert" }, field({ label: "Match records by" }, select({
                            required: true,
                            options: [{ value: "id", label: "id" }, ...c.fields.filter((f) => ["text", "email", "number", "url"].includes(f.type) && f.name != "id").map((f) => ({ value: f.name, label: f.name }))],
                            value: () => data.matchField,
                            onchange: (v) => (data.matchField = v),
                        }))),
                    ),
                );
            },
            () => {
                const r = data.result;
                if (!r) {
                    return null;
                }
                return t.div(
                    { rid: "result-" + JSON.stringify(r).length, className: `alert ${r.ok ? "success" : "danger"} m-t-sm gp-import-result` },
                    r.ok
                        ? t.div(null, `Dry run OK: ${r.created} records would be created and ${r.updated} updated.`)
                        : t.div(
                            null,
                            t.strong(null, `${r.errors.length} invalid row${r.errors.length > 1 ? "s" : ""}${r.stoppedAt ? " (stopped after 100 errors)" : ""}. Nothing was saved.`),
                            t.ul(
                                null,
                                r.errors.slice(0, 20).map((e) =>
                                    t.li(null, `Row ${e.row}: ${e.message}`, e.fields ? " " + Object.entries(e.fields).map(([k, v]) => `${k}: ${v}`).join("; ") : "")
                                ),
                            ),
                        ),
                );
            },
        ),
        t.footer(
            { className: "modal-footer" },
            t.button({ type: "button", className: "btn transparent secondary m-r-auto", onclick: () => app.modals.close(modal) }, t.span({ className: "txt" }, "Cancel")),
            t.button({ type: "button", className: "btn secondary", disabled: () => !data.preview || data.isLoading, onclick: () => run(true) }, t.span({ className: "txt" }, "Dry run")),
            t.button(
                { type: "button", className: () => `btn expanded ${data.isLoading ? "loading" : ""}`, disabled: () => !data.preview || data.isLoading, onclick: () => run(false) },
                t.i({ className: "ri-upload-2-line", ariaHidden: true }),
                t.span({ className: "txt" }, "Import"),
            ),
        ),
    );
    showModal(modal);
}

// --- bulk edit ------------------------------------------------------------------------------

/** Adds an "Edit" button to the bulk bar of the native records list. */
export function mountBulkEdit(listEl) {
    if (listEl._gpBulk) {
        return;
    }
    listEl._gpBulk = true;
    const btn = t.button(
        {
            type: "button",
            className: "btn sm pill gp-bulk-edit",
            onclick: () => bulkEditModal(selectedIds(listEl)),
        },
        t.i({ className: "ri-edit-2-line", ariaHidden: true }),
        t.span({ className: "txt" }, "Edit"),
    );
    const place = () => {
        const bar = listEl.querySelector(".records-bulkbar");
        if (bar && btn.parentNode !== bar && activeCollection()?.type != "view") {
            bar.insertBefore(btn, bar.lastElementChild);
        }
    };
    place();
    new MutationObserver(place).observe(listEl, { childList: true, subtree: true });
}

function selectedIds(listEl) {
    const all = listEl.querySelector('input[id^="all_records_list_"]');
    const prefix = all ? all.id.slice(4) : "";
    return [...listEl.querySelectorAll("tbody .col-bulk-select input:checked")]
        .map((i) => (prefix && i.id.startsWith(prefix) ? i.id.slice(prefix.length) : ""))
        .filter(Boolean);
}

function fieldInput(f, obj) {
    switch (f.type) {
        case "bool":
            return t.div(
                { className: "field" },
                t.input({ id: "gpBulk_" + f.name, type: "checkbox", className: "switch", checked: () => !!obj.value, onchange: (e) => (obj.value = e.target.checked) }),
                t.label({ htmlFor: "gpBulk_" + f.name }, t.span({ className: "txt" }, () => (obj.value ? "Yes" : "No"))),
            );
        case "number":
            return t.div({ className: "field gp-bare" }, t.input({ type: "number", step: "any", value: () => obj.value ?? "", oninput: (e) => (obj.value = e.target.value === "" ? null : Number(e.target.value)) }));
        case "select":
            return t.div({ className: "field gp-bare" }, app.components.select({
                max: f.maxSelect || 1,
                placeholder: "Empty",
                options: (f.values || []).map((v) => ({ value: v, label: v })),
                value: () => obj.value ?? (f.maxSelect > 1 ? [] : ""),
                onchange: (opts) => (obj.value = f.maxSelect > 1 ? opts.map((o) => o.value) : opts[0]?.value || ""),
            }));
        case "date":
            return t.div({ className: "field gp-bare" }, t.input({
                type: "datetime-local",
                value: () => (obj.value ? String(obj.value).replace(" ", "T").slice(0, 16) : ""),
                oninput: (e) => (obj.value = e.target.value ? new Date(e.target.value).toISOString().replace("T", " ") : ""),
            }));
        case "relation":
            return t.div(
                { className: "field gp-bare" },
                t.input({ type: "text", placeholder: f.maxSelect > 1 ? "Record ids separated by ;" : "Record id", value: () => (Array.isArray(obj.value) ? obj.value.join(";") : obj.value || ""), oninput: (e) => (obj.value = f.maxSelect > 1 ? e.target.value.split(";").map((s) => s.trim()).filter(Boolean) : e.target.value.trim()) }),
            );
        case "json":
        case "geoPoint":
            return t.div({ className: "field gp-bare" }, t.textarea({ rows: 2, className: "txt-code", placeholder: "JSON", value: () => (obj.value === undefined ? "" : JSON.stringify(obj.value)), oninput: (e) => {
                try {
                    obj.value = JSON.parse(e.target.value);
                } catch {
                    obj.value = e.target.value;
                }
            } }));
        default:
            return t.div({ className: "field gp-bare" }, f.type == "editor"
                ? t.textarea({ rows: 3, value: () => obj.value ?? "", oninput: (e) => (obj.value = e.target.value) })
                : t.input({ type: f.type == "email" ? "email" : f.type == "url" ? "url" : "text", value: () => obj.value ?? "", oninput: (e) => (obj.value = e.target.value) }));
    }
}

function bulkEditModal(ids) {
    const c = activeCollection();
    if (!ids.length) {
        return;
    }
    const editable = c.fields.filter((f) => !["autodate", "file", "password"].includes(f.type) && f.name != "id" && f.name != "tokenKey");
    const data = store({ rows: [], isSaving: false });

    function addRow(name) {
        if (!name || data.rows.some((r) => r.name == name)) {
            return;
        }
        const f = editable.find((x) => x.name == name);
        data.rows = [...data.rows, { name, value: f.type == "bool" ? false : f.type == "select" && f.maxSelect > 1 ? [] : f.type == "number" ? null : "" }];
    }

    async function save() {
        if (!data.rows.length) {
            return;
        }
        data.isSaving = true;
        const values = {};
        for (const r of data.rows) {
            values[r.name] = r.value;
        }
        try {
            const res = await api(`/api/gp/bulk/${encodeURIComponent(c.name)}`, { method: "PATCH", body: { ids, data: values } });
            app.toasts.success(`Successfully updated ${res.updated} records.`);
            app.modals.close(modal);
            window.dispatchEvent(new Event("gp:recordschanged"));
            document.querySelector(".page-header-secondary-btns .rotate-btn")?.click();
        } catch (err) {
            handleError(err);
        } finally {
            data.isSaving = false;
        }
    }

    const modal = t.div(
        { className: "modal popup gp-bulk-modal" },
        t.header({ className: "modal-header" }, t.h5({ className: "m-auto" }, `Edit ${ids.length} record${ids.length > 1 ? "s" : ""}`)),
        t.div(
            { className: "modal-content" },
            t.p({ className: "txt-hint m-t-0" }, "The chosen fields get the same value on every selected record (in one transaction: nothing changes if a record is invalid)."),
            () =>
                t.div(
                    { rid: "rows-" + data.rows.map((r) => r.name).join(), className: "gp-bulk-rows" },
                    data.rows.map((r) => {
                        const f = editable.find((x) => x.name == r.name);
                        return t.div(
                            { className: "gp-bulk-row" },
                            t.strong({ className: "gp-bulk-name" }, r.name),
                            fieldInput(f, r),
                            t.button({ type: "button", className: "btn sm circle transparent danger", title: "Remove", onclick: () => (data.rows = data.rows.filter((x) => x !== r)) }, t.i({ className: "ri-close-line", ariaHidden: true })),
                        );
                    }),
                ),
            t.div({ className: "field gp-bare gp-bulk-add" }, select({
                placeholder: "+ Add a field to change",
                options: () => editable.filter((f) => !data.rows.some((r) => r.name == f.name)).map((f) => ({ value: f.name, label: `${f.name} · ${f.type}` })),
                value: () => "",
                onchange: addRow,
            })),
        ),
        t.footer(
            { className: "modal-footer" },
            t.button({ type: "button", className: "btn transparent secondary m-r-auto", onclick: () => app.modals.close(modal) }, t.span({ className: "txt" }, "Cancel")),
            t.button(
                { type: "button", className: () => `btn expanded ${data.isSaving ? "loading" : ""}`, disabled: () => data.isSaving || !data.rows.length, onclick: save },
                t.span({ className: "txt" }, "Apply"),
            ),
        ),
    );
    showModal(modal);
}

// --- layouts ----------------------------------------------------------------------------------

const layoutTypes = {
    table: { label: "Table", icon: "ri-table-line" },
    cards: { label: "Cards", icon: "ri-layout-grid-line" },
    kanban: { label: "Kanban", icon: "ri-layout-column-line", requires: (f) => f.type == "select" && (f.maxSelect || 1) <= 1, missing: "needs a select field" },
    calendar: { label: "Calendar", icon: "ri-calendar-2-line", requires: (f) => f.type == "date" || f.type == "autodate", missing: "needs a date field" },
    map: { label: "Map", icon: "ri-map-pin-line", requires: (f) => f.type == "geoPoint", missing: "needs a geo point field" },
};

const layouts = store({ byCollection: {} });

function getLayout(collectionId) {
    layouts.byCollection[collectionId] ||= app.utils.getLocalHistory(LAYOUT_KEY + collectionId, { type: "table" });
    return layouts.byCollection[collectionId];
}

function saveLayout(collectionId, layout) {
    layouts.byCollection[collectionId] = { ...layout };
    app.utils.saveLocalHistory(LAYOUT_KEY + collectionId, JSON.stringify(layout));
}

function layoutOf() {
    const c = activeCollection();
    return c?.id ? getLayout(c.id).type || "table" : "table";
}

function setLayoutType(type) {
    const c = activeCollection();
    const current = getLayout(c.id);
    const next = { ...current, type };
    const fields = c.fields;
    // sensible defaults for the fields of the layout
    next.titleField ||= titleField(c)?.name;
    if (type == "kanban" && !fields.some((f) => f.name == next.groupField)) {
        next.groupField = fields.find(layoutTypes.kanban.requires)?.name;
    }
    if (type == "calendar" && !fields.some((f) => f.name == next.dateField)) {
        next.dateField = (fields.find((f) => f.type == "date") || fields.find(layoutTypes.calendar.requires))?.name;
    }
    if (type == "map" && !fields.some((f) => f.name == next.geoField)) {
        next.geoField = fields.find(layoutTypes.map.requires)?.name;
    }
    if (type == "cards" && next.imageField === undefined) {
        next.imageField = fields.find((f) => f.type == "file")?.name || "";
    }
    saveLayout(c.id, next);
}

function layoutSettings() {
    const c = activeCollection();
    const form = store({ ...getLayout(c.id) });
    const opts = (filterFn, empty) => [
        ...(empty ? [{ value: "", label: empty }] : []),
        ...c.fields.filter(filterFn).map((f) => ({ value: f.name, label: `${f.name} · ${f.type}` })),
    ];
    const modal = t.div(
        { className: "modal popup sm" },
        t.header({ className: "modal-header" }, t.h5({ className: "m-auto" }, `${layoutTypes[form.type]?.label} settings`)),
        t.div(
            { className: "modal-content" },
            field({ label: "Title field" }, select({ required: true, options: opts((f) => f.type != "password" && f.name != "tokenKey"), value: () => form.titleField, onchange: (v) => (form.titleField = v) })),
            form.type == "cards" ? field({ label: "Image field" }, select({ options: opts((f) => f.type == "file", "No image"), value: () => form.imageField || "", onchange: (v) => (form.imageField = v) })) : null,
            form.type == "kanban" ? field({ label: "Columns from" }, select({ required: true, options: opts(layoutTypes.kanban.requires), value: () => form.groupField, onchange: (v) => (form.groupField = v) })) : null,
            form.type == "calendar" ? field({ label: "Date field" }, select({ required: true, options: opts(layoutTypes.calendar.requires), value: () => form.dateField, onchange: (v) => (form.dateField = v) })) : null,
            form.type == "map" ? field({ label: "Location field" }, select({ required: true, options: opts(layoutTypes.map.requires), value: () => form.geoField, onchange: (v) => (form.geoField = v) })) : null,
        ),
        t.footer(
            { className: "modal-footer" },
            t.button({ type: "button", className: "btn transparent secondary m-r-auto", onclick: () => app.modals.close(modal) }, t.span({ className: "txt" }, "Cancel")),
            t.button({ type: "button", className: "btn expanded", onclick: () => (saveLayout(c.id, { ...form }), app.modals.close(modal)) }, t.span({ className: "txt" }, "Apply")),
        ),
    );
    showModal(modal);
}

/** Adds the alternative layouts container after the native records list. */
export function mountLayouts(listEl) {
    if (listEl._gpLayouts) {
        return;
    }
    listEl._gpLayouts = true;

    const data = store({ records: [], loading: false, error: "", month: monthStart(new Date()), version: 0 });
    let reloadTimer;
    const reload = () => {
        clearTimeout(reloadTimer);
        reloadTimer = setTimeout(() => data.version++, 50);
    };
    window.addEventListener("gp:recordschanged", reload);

    async function load(c, layout, params) {
        data.loading = true;
        data.error = "";
        const filters = [];
        if (params.get("filter")) {
            filters.push(`(${params.get("filter")})`);
        }
        if (layout.type == "calendar" && layout.dateField) {
            const from = data.month;
            const to = new Date(Date.UTC(from.getUTCFullYear(), from.getUTCMonth() + 1, 1));
            filters.push(`${layout.dateField} >= "${from.toISOString().replace("T", " ")}" && ${layout.dateField} < "${to.toISOString().replace("T", " ")}"`);
        }
        const expand = c.fields.filter((f) => f.type == "relation").map((f) => f.name).slice(0, 5).join(",");
        try {
            const res = await app.pb.collection(c.name).getList(1, layout.type == "map" ? 1000 : 300, {
                filter: filters.join(" && "),
                sort: params.get("sort") || (c.fields.some((f) => f.name == "created") ? "-created" : ""),
                expand,
                requestKey: "gpLayoutRecords",
            });
            data.records = res.items;
            data.total = res.totalItems;
        } catch (err) {
            if (!err?.isAbort) {
                data.error = err?.message || "Failed to load the records.";
            }
        } finally {
            data.loading = false;
        }
    }

    const container = t.div(
        {
            className: "gp-layout-container",
            onmount: () => {
                container._w = watch(() => {
                    const c = activeCollection();
                    const layout = c?.id ? getLayout(c.id) : { type: "table" };
                    const params = hashParams();
                    data.version; // reload trigger
                    data.month;
                    // the native table is hidden (not removed: it keeps its state)
                    listEl.classList.toggle("gp-hidden", layout.type != "table");
                    if (c?.id && layout.type != "table") {
                        load(c, layout, params);
                    }
                });
            },
            onunmount: () => {
                container._w?.unwatch();
                window.removeEventListener("gp:recordschanged", reload);
            },
        },
        () => {
            const c = activeCollection();
            const layout = c?.id ? getLayout(c.id) : { type: "table" };
            if (!c?.id || layout.type == "table") {
                return null;
            }
            const key = [c.id, layout.type, layout.titleField, layout.groupField, layout.dateField, layout.geoField, layout.imageField].join("|");
            return t.div(
                { rid: key, className: `gp-layout gp-layout-${layout.type}` },
                () => (data.error ? t.div({ rid: "err", className: "alert danger" }, data.error) : null),
                () => (data.total > data.records.length ? t.div({ rid: "more", className: "txt-hint txt-sm m-b-sm" }, `Showing the first ${data.records.length} of ${data.total} records. Use the filter to narrow them.`) : null),
                renderLayout(c, layout, data, reload),
            );
        },
    );
    // one container per records list: the list is re-created when the collection
    // or the view changes, and the container must go with it
    listEl.parentNode?.querySelectorAll(":scope > .gp-layout-container").forEach((el) => el.remove());
    listEl.after(container);
    const cleanup = (e) => {
        if (e.detail === listEl) {
            document.removeEventListener("unmount:recordsList", cleanup);
            container.remove();
        }
    };
    document.addEventListener("unmount:recordsList", cleanup);
}

function renderLayout(c, layout, data, reload) {
    switch (layout.type) {
        case "cards":
            return cardsLayout(c, layout, data, reload);
        case "kanban":
            return kanbanLayout(c, layout, data, reload);
        case "calendar":
            return calendarLayout(c, layout, data, reload);
        case "map":
            return mapLayout(c, layout, data, reload);
    }
    return null;
}

function cardFields(c, layout) {
    return c.fields
        .filter((f) => f.name != layout.titleField && f.name != layout.imageField && !["id", "tokenKey", "password", "json", "editor", "file", "autodate", "geoPoint"].includes(f.type) && f.name != "id" && !f.hidden)
        .slice(0, 3);
}

function cardsLayout(c, layout, data, reload) {
    const title = c.fields.find((f) => f.name == layout.titleField) || titleField(c);
    const image = c.fields.find((f) => f.name == layout.imageField && f.type == "file");
    const extra = cardFields(c, layout);
    return t.div(
        { className: "gp-cards" },
        () =>
            t.div(
                { rid: "cards-" + data.records.map((r) => r.id + r.updated).join(), className: "gp-cards-grid" },
                data.records.length || data.loading
                    ? data.records.map((r) =>
                        t.button(
                            { type: "button", className: "gp-card", onclick: () => openRecord(c, r, reload) },
                            image ? cardImage(r, image) : null,
                            t.strong({ className: "gp-card-title" }, display(r, title) || r.id),
                            extra.map((f) =>
                                t.div({ className: "gp-card-field" }, t.span({ className: "txt-hint" }, f.name), t.span(null, display(r, f) || "—"))
                            ),
                        )
                    )
                    : t.div({ className: "txt-hint p-base" }, "No records."),
            ),
    );
}

function cardImage(record, f) {
    const files = [].concat(record[f.name] || []);
    const img = t.div({ className: "gp-card-image" });
    if (files[0] && /\.(png|jpe?g|gif|webp|avif|svg)$/i.test(files[0])) {
        getFileToken().then((token) => {
            const el = document.createElement("img");
            el.loading = "lazy";
            el.alt = "";
            el.src = app.pb.files.getURL(record, files[0], { thumb: "400x250", token });
            img.appendChild(el);
        });
    } else {
        img.appendChild(t.i({ className: "ri-image-line", ariaHidden: true }));
    }
    return img;
}

function kanbanLayout(c, layout, data, reload) {
    const group = c.fields.find((f) => f.name == layout.groupField);
    const title = c.fields.find((f) => f.name == layout.titleField) || titleField(c);
    const extra = cardFields(c, { ...layout, titleField: title?.name }).filter((f) => f.name != group?.name).slice(0, 2);
    if (!group) {
        return t.div({ className: "alert info" }, "Pick the field of the columns in the layout settings.");
    }
    const columns = [...(group.values || []), ""];
    const state = store({ over: "" });

    async function move(id, value) {
        const r = data.records.find((x) => x.id == id);
        if (!r || r[group.name] == value) {
            return;
        }
        const previous = r[group.name];
        r[group.name] = value; // optimistic
        data.records = [...data.records];
        try {
            await app.pb.collection(c.name).update(id, { [group.name]: value }, { requestKey: null });
        } catch (err) {
            r[group.name] = previous;
            data.records = [...data.records];
            handleError(err);
        }
    }

    return t.div(
        { className: "gp-kanban" },
        () =>
            t.div(
                { rid: "kanban-" + data.records.map((r) => r.id + r[group.name] + r.updated).join(), className: "gp-kanban-board" },
                columns.map((value) => {
                    const items = data.records.filter((r) => (r[group.name] || "") == value);
                    return t.div(
                        {
                            className: () => `gp-kanban-column ${state.over == value ? "over" : ""}`,
                            ondragover: (e) => {
                                e.preventDefault();
                                state.over = value;
                            },
                            ondragleave: () => state.over == value && (state.over = null),
                            ondrop: (e) => {
                                e.preventDefault();
                                state.over = null;
                                move(e.dataTransfer.getData("text/plain"), value);
                            },
                        },
                        t.div({ className: "gp-kanban-head" }, t.strong(null, value || "(empty)"), t.span({ className: "label sm" }, String(items.length))),
                        t.div(
                            { className: "gp-kanban-items" },
                            items.map((r) =>
                                // a div: buttons can't reliably start a drag in every browser
                                t.div(
                                    {
                                        role: "button",
                                        tabIndex: 0,
                                        className: "gp-card gp-kanban-card",
                                        draggable: true,
                                        ondragstart: (e) => {
                                            e.dataTransfer.setData("text/plain", r.id);
                                            e.dataTransfer.effectAllowed = "move";
                                        },
                                        onclick: () => openRecord(c, r, reload),
                                        onkeydown: (e) => {
                                            if (e.key == "Enter" || e.key == " ") {
                                                e.preventDefault();
                                                openRecord(c, r, reload);
                                            }
                                        },
                                    },
                                    t.strong({ className: "gp-card-title" }, display(r, title) || r.id),
                                    extra.map((f) => t.div({ className: "gp-card-field" }, t.span({ className: "txt-hint" }, f.name), t.span(null, display(r, f) || "—"))),
                                )
                            ),
                        ),
                    );
                }),
            ),
    );
}

function monthStart(d) {
    return new Date(Date.UTC(d.getFullYear(), d.getMonth(), 1));
}

function calendarLayout(c, layout, data, reload) {
    const dateField = c.fields.find((f) => f.name == layout.dateField);
    const title = c.fields.find((f) => f.name == layout.titleField) || titleField(c);
    if (!dateField) {
        return t.div({ className: "alert info" }, "Pick the date field in the layout settings.");
    }
    const shift = (n) => (data.month = new Date(Date.UTC(data.month.getUTCFullYear(), data.month.getUTCMonth() + n, 1)));
    const weekdays = [...Array(7)].map((_, i) => new Date(Date.UTC(2024, 0, 1 + i)).toLocaleDateString(undefined, { weekday: "short", timeZone: "UTC" }));
    return t.div(
        { className: "gp-calendar" },
        t.div(
            { className: "gp-calendar-head" },
            t.button({ type: "button", className: "btn sm circle transparent secondary", title: "Previous month", onclick: () => shift(-1) }, t.i({ className: "ri-arrow-left-s-line", ariaHidden: true })),
            t.strong(null, () => data.month.toLocaleDateString(undefined, { month: "long", year: "numeric", timeZone: "UTC" })),
            t.button({ type: "button", className: "btn sm circle transparent secondary", title: "Next month", onclick: () => shift(1) }, t.i({ className: "ri-arrow-right-s-line", ariaHidden: true })),
            t.button({ type: "button", className: "btn sm transparent secondary", onclick: () => (data.month = monthStart(new Date())) }, t.span({ className: "txt" }, "Today")),
            t.span({ className: "txt-hint txt-sm m-l-auto" }, `by ${dateField.name} (UTC)`),
        ),
        t.div({ className: "gp-calendar-grid gp-calendar-weekdays" }, weekdays.map((d) => t.div(null, d))),
        () => {
            const first = data.month;
            const offset = (first.getUTCDay() + 6) % 7; // weeks start on Monday
            const days = new Date(Date.UTC(first.getUTCFullYear(), first.getUTCMonth() + 1, 0)).getUTCDate();
            const today = new Date().toISOString().slice(0, 10);
            const byDay = {};
            for (const r of data.records) {
                const day = String(r[dateField.name] || "").slice(0, 10);
                (byDay[day] ||= []).push(r);
            }
            const cells = [];
            for (let i = 0; i < offset; i++) {
                cells.push(t.div({ className: "gp-calendar-day empty" }));
            }
            for (let d = 1; d <= days; d++) {
                const key = `${first.getUTCFullYear()}-${String(first.getUTCMonth() + 1).padStart(2, "0")}-${String(d).padStart(2, "0")}`;
                const items = byDay[key] || [];
                cells.push(
                    t.div(
                        { className: `gp-calendar-day ${key == today ? "today" : ""}` },
                        t.span({ className: "gp-calendar-num" }, String(d)),
                        items.slice(0, 4).map((r) => t.button({ type: "button", className: "gp-calendar-item", title: display(r, title), onclick: () => openRecord(c, r, reload) }, display(r, title) || r.id)),
                        items.length > 4 ? t.span({ className: "txt-hint txt-sm" }, `+${items.length - 4} more`) : null,
                    ),
                );
            }
            return t.div({ rid: "days-" + first.toISOString() + data.records.map((r) => r.id + r.updated).join(), className: "gp-calendar-grid" }, cells);
        },
    );
}

let leafletLoading;
function loadLeaflet() {
    if (window.L) {
        return Promise.resolve();
    }
    leafletLoading ||= new Promise((resolve, reject) => {
        const css = document.createElement("link");
        css.rel = "stylesheet";
        css.href = extBase + "vendor/leaflet/leaflet.css";
        document.head.appendChild(css);
        const s = document.createElement("script");
        s.src = extBase + "vendor/leaflet/leaflet.js";
        s.onload = resolve;
        s.onerror = () => {
            leafletLoading = null;
            reject(new Error("Failed to load the map library."));
        };
        document.head.appendChild(s);
    });
    return leafletLoading;
}

let tilesLoading;
/** The tile URL of the maps ("" offline without a tile server). */
function mapTiles() {
    tilesLoading ||= api("/api/gp/config")
        .then((c) => c.mapTiles || "")
        .catch(() => "https://tile.openstreetmap.org/{z}/{x}/{y}.png");
    return tilesLoading;
}

function mapLayout(c, layout, data, reload) {
    const geo = c.fields.find((f) => f.name == layout.geoField);
    const title = c.fields.find((f) => f.name == layout.titleField) || titleField(c);
    if (!geo) {
        return t.div({ className: "alert info" }, "Pick the location field in the layout settings.");
    }
    let map, markers, watcher, resizeObserver;
    return t.div({
        className: "gp-map",
        onmount: async (el) => {
            try {
                await loadLeaflet();
            } catch (err) {
                el.textContent = err.message;
                return;
            }
            const L = window.L;
            L.Icon.Default.imagePath = extBase + "vendor/leaflet/images/";
            map = L.map(el, { scrollWheelZoom: true });
            // tiles: OpenStreetMap, a tile server set with POCKETBASEPLUS_MAP_TILES, or none offline
            const tiles = await mapTiles();
            if (tiles) {
                L.tileLayer(tiles, {
                    maxZoom: 19,
                    attribution: tiles.includes("openstreetmap.org") ? "&copy; OpenStreetMap contributors" : "",
                }).addTo(map);
            } else {
                el.classList.add("gp-map-offline");
            }
            markers = L.featureGroup().addTo(map);
            map.setView([20, 0], 2);
            watcher = watch(() => {
                const list = data.records;
                markers.clearLayers();
                for (const r of list) {
                    const p = r[geo.name];
                    if (!p || (!p.lat && !p.lon)) {
                        continue;
                    }
                    const popup = document.createElement("div");
                    popup.className = "gp-map-popup";
                    const strong = document.createElement("strong");
                    strong.textContent = display(r, title) || r.id;
                    const btn = document.createElement("button");
                    btn.type = "button";
                    btn.className = "btn sm";
                    btn.textContent = "Open";
                    btn.onclick = () => openRecord(c, r, reload);
                    popup.append(strong, btn);
                    L.marker([p.lat, p.lon], { title: display(r, title) }).bindPopup(popup).addTo(markers);
                }
                if (markers.getLayers().length) {
                    map.fitBounds(markers.getBounds(), { padding: [30, 30], maxZoom: 13, animate: false });
                }
            });
            resizeObserver = new ResizeObserver(() => map?.invalidateSize({ animate: false }));
            resizeObserver.observe(el);
        },
        onunmount: () => {
            watcher?.unwatch();
            resizeObserver?.disconnect();
            // stop the running animations before removing the map (Leaflet errors otherwise)
            map?.stop();
            map?.off();
            map?.remove();
            map = null;
        },
    });
}
