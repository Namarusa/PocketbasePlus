// Documents: the list of the templates (by folder), the "New template"
// window (starters) and the generated documents.

import { field, formatDate, handleError, page, select, sidebar, userCollections, api } from "../pocketbaseplus/lib.js";

const base = app.pb.buildURL("/_/extensions/pocketbaseplus-documents/");
if (!document.querySelector(`link[href="${base}documents.css"]`)) {
    document.head.appendChild(t.link({ rel: "stylesheet", href: base + "documents.css" }));
}

const OPEN_KEY = "gpDocumentFoldersOpen";

export const formatLabels = { a4: "A4", a5: "A5", a3: "A3", letter: "Letter", legal: "Legal", custom: "Custom" };

export function docsSidebar() {
    return sidebar("gpDocumentsSidebar", [
        {
            name: "Documents",
            links: [
                { href: "#/documents", label: "Templates", icon: "ri-file-pdf-2-line", isActive: () => window.location.hash == "#/documents" || /^#\/documents\/(?!generated)/.test(window.location.hash) },
                { href: "#/documents/generated", label: "Generated", icon: "ri-history-line" },
            ],
        },
    ]);
}

export function slugify(s) {
    return String(s || "")
        .toLowerCase()
        .normalize("NFD")
        .replace(/[̀-ͯ]/g, "")
        .replace(/[^a-z0-9]+/g, "-")
        .replace(/^-|-$/g, "")
        .slice(0, 80) || "document";
}

export function pageDocuments() {
    app.store.title = "Documents";
    const data = store({ templates: [], isLoading: true, search: "", closed: app.utils.getLocalHistory(OPEN_KEY, {}) });

    async function load() {
        data.isLoading = true;
        try {
            data.templates = await app.pb.collection("document_templates").getFullList({
                sort: "name",
                fields: "id,name,slug,folder,format,orientation,collection,updated",
                requestKey: "gpDocTemplates",
            });
        } catch (err) {
            handleError(err);
        }
        data.isLoading = false;
    }

    const folders = () => [...new Set(data.templates.map((x) => x.folder).filter(Boolean))].sort((a, b) => a.localeCompare(b));
    const matches = (x) => {
        const q = data.search.trim().toLowerCase();
        return !q || [x.name, x.slug, x.folder, x.collection].some((v) => (v || "").toLowerCase().includes(q));
    };
    function toggle(key) {
        data.closed[key] = !data.closed[key];
        app.utils.saveLocalHistory(OPEN_KEY, JSON.stringify(data.closed));
    }

    async function duplicate(x) {
        try {
            const full = await app.pb.collection("document_templates").getOne(x.id);
            const copy = { ...full, name: full.name + " (copy)", slug: `${full.slug}-copy-${Date.now().toString(36)}` };
            for (const k of ["id", "created", "updated", "assets", "collectionId", "collectionName"]) {
                delete copy[k];
            }
            await app.pb.collection("document_templates").create(copy);
            app.toasts.success("Duplicated the template (without its images).");
            load();
        } catch (err) {
            handleError(err);
        }
    }

    function remove(x) {
        app.modals.confirm(`Do you really want to delete "${x.name}"? The documents already made are kept.`, async () => {
            try {
                await app.pb.collection("document_templates").delete(x.id);
                app.toasts.success("Deleted the template.");
                load();
            } catch (err) {
                handleError(err);
            }
        });
    }

    const row = (x) =>
        t.tr(
            { rid: x.id + x.updated },
            t.td(null, t.a({ href: "#/documents/" + x.id, className: "txt-bold" }, x.name)),
            t.td(null, t.span({ className: "label sm" }, (formatLabels[x.format] || x.format) + (x.orientation == "landscape" ? " · landscape" : ""))),
            t.td({ className: "txt-hint" }, x.collection || "—"),
            t.td({ className: "txt-hint txt-sm txt-nowrap" }, formatDate(x.updated)),
            t.td(
                { className: "txt-right txt-nowrap" },
                t.button({ type: "button", className: "btn sm circle transparent secondary", ariaLabel: app.attrs.tooltip("Duplicate"), onclick: () => duplicate(x) }, t.i({ className: "ri-file-copy-line", ariaHidden: true })),
                t.button({ type: "button", className: "btn sm circle transparent danger", ariaLabel: app.attrs.tooltip("Delete"), onclick: () => remove(x) }, t.i({ className: "ri-delete-bin-7-line", ariaHidden: true })),
            ),
        );

    const header = (key, label, count) =>
        t.tr(
            { rid: "h" + key + count + !!data.closed[key], className: "gp-doc-folder" },
            t.td(
                { colSpan: 5 },
                t.button(
                    { type: "button", className: "gp-doc-folder-toggle", ariaExpanded: !data.closed[key], onclick: () => toggle(key) },
                    t.i({ className: data.closed[key] ? "ri-arrow-right-s-line" : "ri-arrow-down-s-line", ariaHidden: true }),
                    t.i({ className: key ? "ri-folder-line" : "ri-file-list-line", ariaHidden: true }),
                    t.span({ className: "txt-bold" }, label),
                    t.span({ className: "label sm" }, count),
                ),
            ),
        );

    function body() {
        if (data.isLoading && !data.templates.length) {
            return t.tr({ rid: "loading" }, t.td({ colSpan: 5, className: "txt-center" }, t.span({ className: "loader" })));
        }
        const list = data.templates.filter(matches);
        if (!list.length) {
            return t.tr(
                { rid: "empty" },
                t.td(
                    { colSpan: 5, className: "txt-center txt-hint" },
                    data.search ? "No template matches." : ["No templates yet. ", t.a({ href: "#", onclick: (e) => (e.preventDefault(), newTemplateModal()) }, "Create one"), " from an invoice, a quote or a certificate."],
                ),
            );
        }
        const searching = !!data.search.trim();
        const rows = [];
        for (const f of folders()) {
            const items = list.filter((x) => x.folder == f);
            if (!items.length) {
                continue;
            }
            rows.push(header(f, f, items.length));
            if (!data.closed[f] || searching) {
                rows.push(...items.map(row));
            }
        }
        const loose = list.filter((x) => !x.folder);
        if (loose.length && folders().length) {
            rows.push(header("", "Other templates", loose.length));
        }
        if (!data.closed[""] || searching || !folders().length) {
            rows.push(...loose.map(row));
        }
        return rows;
    }

    load();

    return page(
        {
            pbEvent: "gpPageDocuments",
            sidebar: docsSidebar(),
            breadcrumbs: ["Documents", "Templates"],
            buttons: t.button(
                { type: "button", className: "btn expanded-lg", onclick: () => newTemplateModal() },
                t.i({ className: "ri-add-line", ariaHidden: true }),
                t.span({ className: "txt" }, "New template"),
            ),
        },
        t.p(
            { className: "txt-hint m-t-0" },
            "PDF documents (invoices, quotes, certificates…) built from blocks and filled with the data of a record and formulas like ",
            t.code(null, "{{record.name}}"),
            ". The flows make them with the Generate document step, and attach them to emails or store them in records.",
        ),
        t.div(
            { className: "field gp-bare gp-doc-search m-b-sm" },
            t.input({ type: "search", placeholder: "Search templates...", value: () => data.search, oninput: (e) => (data.search = e.target.value) }),
        ),
        t.div(
            { className: "gp-table-wrap" },
            t.table(
                { className: "gp-doc-table" },
                t.thead(null, t.tr(null, t.th(null, "Name"), t.th(null, "Page"), t.th(null, "About"), t.th(null, "Updated"), t.th(null, ""))),
                t.tbody(null, body),
            ),
        ),
    );
}

// the "New template" window: a name, a starter, the collection
export function newTemplateModal() {
    const state = store({ name: "", starter: "invoice", collection: "", starters: [], collections: [], isSaving: false });
    api("/api/documents/starters", { requestKey: null }).then((list) => (state.starters = list)).catch(handleError);
    userCollections().then((list) => (state.collections = list.map((c) => c.name)));

    async function create() {
        const starter = state.starters.find((s) => s.key == state.starter);
        if (!starter) {
            return;
        }
        state.isSaving = true;
        const name = state.name.trim() || starter.name;
        const spec = starter.spec;
        try {
            const rec = await app.pb.collection("document_templates").create({
                name,
                slug: slugify(name) + "-" + Math.random().toString(36).slice(2, 6),
                format: spec.format,
                orientation: spec.orientation,
                margins: spec.margins,
                font_size: spec.font_size,
                accent_color: spec.accent_color,
                collection: state.collection,
                header: spec.header || [],
                body: spec.body || [],
                footer: spec.footer || [],
                lists: spec.lists || [],
                // the starter's name (invoice-…) becomes the template's
                filename: (spec.filename || "").replace(/^[a-z]+-/, slugify(name) + "-"),
            });
            app.modals.close(modal);
            window.location.hash = "#/documents/" + rec.id;
        } catch (err) {
            handleError(err);
        }
        state.isSaving = false;
    }

    const modal = t.div(
        { className: "modal popup gp-doc-new" },
        t.header({ className: "modal-header" }, t.h5({ className: "m-auto" }, "New document template")),
        t.form(
            { id: "gpNewDocument", className: "modal-content", onsubmit: (e) => (e.preventDefault(), create()) },
            field({ label: "Name", name: "doc_name" }, t.input({ id: "doc_name", type: "text", autofocus: true, placeholder: () => state.starters.find((s) => s.key == state.starter)?.name || "Invoice", value: () => state.name, oninput: (e) => (state.name = e.target.value) })),
            t.label({ className: "gp-label m-t-sm" }, "Start from"),
            () =>
                t.div(
                    { rid: "starters" + state.starters.length, className: "gp-doc-starters" },
                    state.starters.map((s) =>
                        t.button(
                            {
                                type: "button",
                                className: () => `gp-doc-starter ${state.starter == s.key ? "active" : ""}`,
                                ariaPressed: () => state.starter == s.key,
                                onclick: () => (state.starter = s.key),
                            },
                            t.i({ className: s.icon, ariaHidden: true }),
                            t.strong(null, s.name),
                            t.small({ className: "txt-hint" }, s.description),
                        )
                    ),
                ),
            field(
                { label: "About the records of", name: "doc_collection", hint: "Each document is made for one of its records ({{record.…}}). Optional: you can change it later." },
                select({
                    id: "doc_collection",
                    placeholder: "- No collection -",
                    options: () => state.collections.map((c) => ({ value: c, label: c })),
                    value: () => state.collection,
                    onchange: (v) => (state.collection = v),
                }),
            ),
        ),
        t.footer(
            { className: "modal-footer" },
            t.button({ type: "button", className: "btn transparent secondary m-r-auto", onclick: () => app.modals.close(modal) }, t.span({ className: "txt" }, "Cancel")),
            t.button({ type: "submit", "html-form": "gpNewDocument", className: () => `btn expanded ${state.isSaving ? "loading" : ""}`, disabled: () => state.isSaving || !state.starters.length }, t.span({ className: "txt" }, "Create")),
        ),
    );
    modal.onafterclose = () => modal.remove();
    document.body.appendChild(modal);
    app.modals.open(modal);
}

// --- generated documents ------------------------------------------------------------------------

function fileSize(n) {
    if (n < 1024) {
        return n + " B";
    }
    return n < 1024 * 1024 ? (n / 1024).toFixed(0) + " KB" : (n / 1024 / 1024).toFixed(1) + " MB";
}

/** Downloads a generated document (a protected file: a short-lived token). */
export async function downloadDocument(doc) {
    try {
        const token = await app.pb.files.getToken();
        // fetched, then saved with its name (the stored file has a random suffix)
        const res = await fetch(app.pb.files.getURL(doc, doc.file, { token }));
        if (!res.ok) {
            throw new Error("The file can't be downloaded (" + res.status + ").");
        }
        const url = URL.createObjectURL(await res.blob());
        const a = t.a({ href: url, download: doc.filename || "document.pdf" });
        document.body.appendChild(a);
        a.click();
        a.remove();
        setTimeout(() => URL.revokeObjectURL(url), 10000);
    } catch (err) {
        handleError(err);
    }
}

export function pageGenerated() {
    app.store.title = "Generated documents";
    const data = store({ items: [], page: 1, total: 0, isLoading: true });

    async function load() {
        data.isLoading = true;
        try {
            const res = await app.pb.collection("documents").getList(data.page, 50, { sort: "-created", expand: "template", requestKey: "gpDocs" });
            data.items = res.items;
            data.total = res.totalItems;
        } catch (err) {
            handleError(err);
        }
        data.isLoading = false;
    }

    function remove(d) {
        app.modals.confirm(`Delete "${d.filename}"?`, async () => {
            try {
                await app.pb.collection("documents").delete(d.id);
                load();
            } catch (err) {
                handleError(err);
            }
        });
    }

    load();
    return page(
        { pbEvent: "gpPageGenerated", sidebar: docsSidebar(), breadcrumbs: ["Documents", "Generated"] },
        t.p({ className: "txt-hint m-t-0" }, "The documents made by the flows and from the editor. Only superusers can download them."),
        t.div(
            { className: "gp-table-wrap" },
            t.table(
                { className: "gp-doc-table" },
                t.thead(null, t.tr(null, t.th(null, "File"), t.th(null, "Template"), t.th(null, "Record"), t.th(null, "Pages"), t.th(null, "Made by"), t.th(null, "Created"), t.th(null, ""))),
                t.tbody(null, () => {
                    if (data.isLoading && !data.items.length) {
                        return t.tr({ rid: "loading" }, t.td({ colSpan: 7, className: "txt-center" }, t.span({ className: "loader" })));
                    }
                    if (!data.items.length) {
                        return t.tr({ rid: "empty" }, t.td({ colSpan: 7, className: "txt-center txt-hint" }, "No documents yet: generate one from a template, or with a flow."));
                    }
                    return data.items.map((d) =>
                        t.tr(
                            { rid: d.id },
                            t.td(null, t.button({ type: "button", className: "gp-doc-link", onclick: () => downloadDocument(d) }, t.i({ className: "ri-file-pdf-2-line", ariaHidden: true }), " ", d.filename)),
                            t.td(null, d.expand?.template ? t.a({ href: "#/documents/" + d.template }, d.expand.template.name) : t.span({ className: "txt-hint" }, "—")),
                            t.td(
                                { className: "txt-hint txt-sm" },
                                d.record_id ? t.a({ href: `#/collections?collection=${encodeURIComponent(d.collection)}&record=${d.record_id}` }, `${d.collection} · ${d.record_id}`) : "—",
                            ),
                            t.td({ className: "txt-hint" }, `${d.pages} · ${fileSize(d.size || 0)}`),
                            t.td({ className: "txt-hint txt-sm" }, d.source),
                            t.td({ className: "txt-hint txt-sm txt-nowrap" }, formatDate(d.created)),
                            t.td(
                                { className: "txt-right txt-nowrap" },
                                t.button({ type: "button", className: "btn sm circle transparent secondary", ariaLabel: app.attrs.tooltip("Download"), onclick: () => downloadDocument(d) }, t.i({ className: "ri-download-2-line", ariaHidden: true })),
                                t.button({ type: "button", className: "btn sm circle transparent danger", ariaLabel: app.attrs.tooltip("Delete"), onclick: () => remove(d) }, t.i({ className: "ri-delete-bin-7-line", ariaHidden: true })),
                            ),
                        )
                    );
                }),
            ),
        ),
        () =>
            data.total > 50
                ? t.div(
                    { rid: "pager" + data.page, className: "flex gap-5 m-t-sm" },
                    t.button({ type: "button", className: "btn sm secondary", disabled: data.page <= 1, onclick: () => (data.page--, load()) }, "Previous"),
                    t.span({ className: "txt-hint txt-sm" }, `Page ${data.page} / ${Math.ceil(data.total / 50)}`),
                    t.button({ type: "button", className: "btn sm secondary", disabled: data.page * 50 >= data.total, onclick: () => (data.page++, load()) }, "Next"),
                )
                : null,
    );
}
