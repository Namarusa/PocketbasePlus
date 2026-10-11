// Forms: public forms built from the fields of a collection, published as
// a full page (/f/{slug}) and as an iframe embed limited to allowed domains.

import { field, handleError, page, select, sidebar, userCollections } from "./lib.js";

// field types a form can show
const formTypes = new Set(["text", "email", "url", "number", "bool", "select", "date", "editor", "file", "relation"]);
// fields that can't have a fixed value
const notFixable = new Set(["autodate", "file", "password"]);

const shared = store({ forms: [], loaded: false });
let loading = null;

function loadForms() {
    if (!loading) {
        loading = app.pb
            .collection("forms")
            .getFullList({ sort: "name", requestKey: null })
            .then((list) => (shared.forms = list))
            .catch(handleError)
            .finally(() => {
                shared.loaded = true;
                loading = null;
            });
    }
    return loading;
}

function formsSidebar() {
    if (!shared.loaded) {
        loadForms();
    }
    return sidebar("gpFormsSidebar", () => [
        {
            name: "Forms",
            empty: "No forms yet.",
            links: [
                { href: "#/forms", label: "All forms", icon: "ri-layout-grid-line", exact: true },
                ...shared.forms.map((f) => ({
                    href: "#/forms/" + f.id,
                    label: f.name,
                    icon: "ri-survey-line",
                    after: t.span({ className: `gp-dot ${f.active ? "on" : ""}`, title: f.active ? "Published" : "Draft" }),
                })),
            ],
        },
    ]);
}

const pageURL = (slug) => `${window.location.origin}/f/${slug}`;
const embedURL = (slug) => `${window.location.origin}/f/${slug}/embed`;

function slugify(s) {
    return String(s || "")
        .normalize("NFD")
        .replace(/[\u0300-\u036f]/g, "")
        .toLowerCase()
        .replace(/[^a-z0-9]+/g, "-")
        .replace(/^-+|-+$/g, "")
        .slice(0, 60) || "form";
}

async function copy(text, what) {
    try {
        await navigator.clipboard.writeText(text);
        app.toasts.success(`Copied the ${what}.`);
    } catch {
        app.toasts.error("Copy failed: select the text and copy it manually.");
    }
}

function embedSnippet(slug) {
    const id = `gp-form-${slug}`;
    return [
        `<iframe id="${id}" src="${embedURL(slug)}" style="width:100%;border:0;min-height:300px" title="Form" loading="lazy"></iframe>`,
        `<script>`,
        `addEventListener("message", function (e) {`,
        `  if (e.origin === "${window.location.origin}" && e.data && e.data.type === "pocketbaseplus-form:height" && e.data.slug === "${slug}") {`,
        `    document.getElementById("${id}").style.height = e.data.height + "px";`,
        `  }`,
        `});`,
        `</script>`,
    ].join("\n");
}

// --- list page --------------------------------------------------------------------------------

function newFormModal() {
    const data = store({ name: "", collection: "", collections: [], isSaving: false });
    userCollections().then((list) => (data.collections = list.filter((c) => c.type == "base"))).catch(handleError);

    async function create() {
        data.isSaving = true;
        try {
            const c = data.collections.find((x) => x.name == data.collection);
            // start with the required fields of the collection
            const fields = (c?.fields || [])
                .filter((f) => f.required && formTypes.has(f.type) && f.name != "id" && f.type != "relation")
                .map((f) => ({ field: f.name, width: "full" }));
            const rec = await app.pb.collection("forms").create({
                name: data.name,
                slug: slugify(data.name) + "-" + Math.random().toString(36).slice(2, 6),
                collection: data.collection,
                active: false,
                fields,
                fixed: {},
                title: data.name,
                submit_label: "Submit",
                success_message: "Thank you, your answer was sent.",
                theme: "auto",
                rate_limit: 20,
            });
            app.modals.close(modal);
            loadForms();
            window.location.hash = "#/forms/" + rec.id;
        } catch (err) {
            handleError(err);
        } finally {
            data.isSaving = false;
        }
    }

    const modal = t.div(
        { className: "modal popup sm" },
        t.header({ className: "modal-header" }, t.h5({ className: "m-auto" }, "New form")),
        t.form(
            { id: "gpNewForm", className: "modal-content", onsubmit: (e) => (e.preventDefault(), create()) },
            field({ label: "Name" }, t.input({ type: "text", required: true, autofocus: true, placeholder: "e.g. Contact us", value: () => data.name, oninput: (e) => (data.name = e.target.value) })),
            field({ label: "Collection", hint: "Each answer creates a record in this collection." }, select({
                required: true,
                placeholder: "- Select collection -",
                options: () => data.collections.map((c) => ({ value: c.name, label: c.name })),
                value: () => data.collection,
                onchange: (v) => (data.collection = v),
            })),
        ),
        t.footer(
            { className: "modal-footer" },
            t.button({ type: "button", className: "btn transparent secondary m-r-auto", onclick: () => app.modals.close(modal) }, t.span({ className: "txt" }, "Cancel")),
            t.button({ type: "submit", "html-form": "gpNewForm", className: () => `btn expanded ${data.isSaving ? "loading" : ""}`, disabled: () => data.isSaving || !data.collection }, t.span({ className: "txt" }, "Create")),
        ),
    );
    modal.onafterclose = () => modal.remove();
    document.body.appendChild(modal);
    app.modals.open(modal);
}

export function pageForms() {
    app.store.title = "Forms";
    const data = store({ counts: {}, collections: [] });
    loadForms().then(async () => {
        const counts = {};
        for (const f of shared.forms) {
            try {
                counts[f.id] = (await app.pb.collection("revisions").getList(1, 1, {
                    filter: app.pb.filter("actor_type = 'form' && actor_id = {:id} && action = 'create'", { id: f.id }),
                    fields: "id",
                    requestKey: null,
                })).totalItems;
            } catch {}
        }
        data.counts = counts;
    });
    userCollections().then((list) => (data.collections = list)).catch(handleError);

    async function toggle(f) {
        try {
            await app.pb.collection("forms").update(f.id, { active: !f.active });
            loadForms();
        } catch (err) {
            handleError(err);
        }
    }

    return page(
        {
            pbEvent: "gpPageForms",
            sidebar: formsSidebar(),
            breadcrumbs: ["Forms"],
            buttons: t.button({ type: "button", className: "btn expanded-lg", onclick: newFormModal }, t.i({ className: "ri-add-line", ariaHidden: true }), t.span({ className: "txt" }, "New form")),
        },
        t.p({ className: "txt-hint m-t-0" }, "Public forms built from a collection's fields: pick the fields to show, fix the others, and publish a page or an embeddable form. Each answer creates a record (flows, emails and revisions see it)."),
        t.div(
            { className: "gp-table-wrap" },
            t.table(
                null,
                t.thead(null, t.tr(null, t.th(null, "Name"), t.th(null, "Collection"), t.th(null, "Answers"), t.th(null, "Link"), t.th(null, "Status"))),
                t.tbody(null, () => {
                    if (!shared.loaded) {
                        return t.tr({ rid: "loading" }, t.td({ colSpan: 5, className: "txt-center" }, t.span({ className: "loader" })));
                    }
                    if (!shared.forms.length) {
                        return t.tr({ rid: "empty" }, t.td({ colSpan: 5, className: "txt-center txt-hint" }, "No forms yet."));
                    }
                    return shared.forms.map((f) =>
                        t.tr(
                            { rid: f.id + f.updated + (data.counts[f.id] ?? "") },
                            t.td(null, t.a({ href: "#/forms/" + f.id, className: "txt-bold flex gap-5" }, t.i({ className: "ri-survey-line", ariaHidden: true }), t.span(null, f.name))),
                            t.td({ className: "txt-hint" }, () => data.collections.find((c) => c.id == f.collection)?.name || f.collection),
                            t.td(null, String(data.counts[f.id] ?? "–")),
                            t.td(null, f.active ? t.a({ href: pageURL(f.slug), target: "_blank", rel: "noopener", className: "txt-sm" }, `/f/${f.slug}`) : t.span({ className: "txt-hint txt-sm" }, "Draft")),
                            t.td(
                                null,
                                t.button(
                                    { type: "button", className: `label sm gp-status-toggle ${f.active ? "success" : ""}`, ariaDescription: app.attrs.tooltip("Click to toggle"), onclick: () => toggle(f) },
                                    f.active ? "published" : "draft",
                                ),
                            ),
                        )
                    );
                }),
            ),
        ),
    );
}

// --- editor -----------------------------------------------------------------------------------

export function pageForm(route) {
    const id = route.params.id;
    const data = store({
        form: null,
        saved: "",
        collections: [],
        isSaving: false,
        tab: "fields",
        previewKey: 1,
    });

    const collection = () => data.collections.find((c) => c.id == data.form?.collection);
    const fieldDef = (name) => collection()?.fields.find((f) => f.name == name);
    const serialized = () => JSON.stringify(data.form);
    const dirty = () => data.form && serialized() != data.saved;

    async function load() {
        try {
            const [rec, cols] = await Promise.all([app.pb.collection("forms").getOne(id), userCollections()]);
            data.collections = cols;
            data.form = {
                ...rec,
                fields: rec.fields || [],
                fixedRows: Object.entries(rec.fixed || {}).map(([field, value]) => ({ field, value })),
                domainsText: (rec.allowed_domains || []).join("\n"),
            };
            delete data.form.fixed;
            data.saved = serialized();
            app.store.title = rec.name;
        } catch (err) {
            handleError(err);
            if (err?.status == 404) {
                window.location.hash = "#/forms";
            }
        }
    }

    /** The form settings as saved (also sent to the live preview). */
    function bodyOf(f) {
        const fixed = {};
        for (const row of f.fixedRows) {
            if (row.field) {
                fixed[row.field] = row.value;
            }
        }
        return {
                name: f.name,
                slug: f.slug,
                collection: f.collection,
                active: f.active,
                fields: f.fields,
                fixed,
                title: f.title,
                description: f.description,
                submit_label: f.submit_label,
                success_message: f.success_message,
                redirect_url: f.redirect_url,
                allowed_domains: f.domainsText.split(/[\s,]+/).map((d) => d.trim()).filter(Boolean),
                require_auth: f.require_auth,
                rate_limit: Number(f.rate_limit) || 0,
                theme: f.theme,
                accent: f.accent,
        };
    }

    async function save() {
        data.isSaving = true;
        try {
            const rec = await app.pb.collection("forms").update(id, bodyOf(data.form));
            data.form.allowed_domains = rec.allowed_domains;
            data.form.domainsText = (rec.allowed_domains || []).join("\n");
            data.form.fields = rec.fields || [];
            data.saved = serialized();
            data.previewKey++;
            loadForms();
            app.toasts.success("Successfully saved the form.");
        } catch (err) {
            handleError(err);
        } finally {
            data.isSaving = false;
        }
    }

    function remove() {
        app.modals.confirm(`Delete the form "${data.form.name}"? The records it created are kept.`, async () => {
            try {
                await app.pb.collection("forms").delete(id);
                loadForms();
                window.location.hash = "#/forms";
            } catch (err) {
                handleError(err);
            }
        });
    }

    load();

    // ---- live preview: the unsaved settings are sent to the preview iframe

    const savedSlug = () => {
        try {
            return JSON.parse(data.saved).slug;
        } catch {
            return data.form?.slug;
        }
    };

    function mountPreview(frame) {
        const post = () => {
            if (dirty()) {
                frame.contentWindow?.postMessage({ type: "pocketbaseplus-form:draft", slug: savedSlug(), draft: JSON.parse(JSON.stringify(bodyOf(data.form))) }, window.location.origin); // plain copy: store proxies can't be cloned
            }
        };
        let timer;
        const onMessage = (e) => {
            if (e.origin == window.location.origin && e.source == frame.contentWindow && e.data?.type == "pocketbaseplus-form:ready") {
                post();
            }
        };
        window.addEventListener("message", onMessage);
        const watcher = watch(() => JSON.stringify(bodyOf(data.form)), () => {
            clearTimeout(timer);
            timer = setTimeout(post, 300);
        });
        frame._gpCleanup = () => {
            clearTimeout(timer);
            watcher.unwatch();
            window.removeEventListener("message", onMessage);
        };
    }

    // ---- fields

    const fixedNames = () => new Set(data.form.fixedRows.map((r) => r.field));
    const available = () =>
        (collection()?.fields || []).filter((f) => formTypes.has(f.type) && f.name != "id" && !data.form.fields.some((x) => x.field == f.name) && !fixedNames().has(f.name));
    const missingRequired = () =>
        (collection()?.fields || []).filter((f) => f.required && f.name != "id" && !notFixable.has(f.type) && !data.form.fields.some((x) => x.field == f.name) && !fixedNames().has(f.name));

    function addField(name) {
        const f = fieldDef(name);
        if (!f) {
            return;
        }
        data.form.fields = [...data.form.fields, { field: name, width: "full", ...(f.type == "relation" ? { listRecords: true } : {}) }];
    }

    function move(i, delta) {
        const list = [...data.form.fields];
        const j = i + delta;
        if (j < 0 || j >= list.length) {
            return;
        }
        [list[i], list[j]] = [list[j], list[i]];
        data.form.fields = list;
    }

    function fieldsSection() {
        return t.div(
            { className: "gp-form-builder" },
            () => {
                const missing = missingRequired();
                return missing.length
                    ? t.div(
                        { rid: "missing-" + missing.map((f) => f.name).join(), className: "alert warning" },
                        t.div(null, `Required by the collection but not in the form: ${missing.map((f) => f.name).join(", ")}. Show them or give them a fixed value, otherwise every answer fails.`),
                        t.button({ type: "button", className: "btn sm secondary m-t-5", onclick: () => missing.filter((f) => formTypes.has(f.type)).forEach((f) => addField(f.name)) }, t.span({ className: "txt" }, "Add them to the form")),
                    )
                    : null;
            },
            () =>
                t.div(
                    { rid: "fields-" + data.form.fields.map((f) => f.field).join(), className: "gp-fb-list" },
                    data.form.fields.length
                        ? data.form.fields.map((cfg, i) => {
                            const def = fieldDef(cfg.field);
                            return t.div(
                                { className: "gp-fb-field" },
                                t.div(
                                    { className: "gp-fb-head" },
                                    t.strong(null, cfg.field),
                                    t.span({ className: "label sm" }, def?.type || "?"),
                                    def?.required ? t.span({ className: "label sm warning" }, "required") : null,
                                    t.div(
                                        { className: "gp-fb-actions" },
                                        t.button({ type: "button", className: "btn sm circle transparent secondary", title: "Move up", disabled: i == 0, onclick: () => move(i, -1) }, t.i({ className: "ri-arrow-up-line", ariaHidden: true })),
                                        t.button({ type: "button", className: "btn sm circle transparent secondary", title: "Move down", disabled: i == data.form.fields.length - 1, onclick: () => move(i, 1) }, t.i({ className: "ri-arrow-down-line", ariaHidden: true })),
                                        t.button({ type: "button", className: "btn sm circle transparent danger", title: "Remove", onclick: () => (data.form.fields = data.form.fields.filter((_, j) => j != i)) }, t.i({ className: "ri-close-line", ariaHidden: true })),
                                    ),
                                ),
                                t.div(
                                    { className: "grid sm" },
                                    t.div({ className: "col-lg-6" }, field({ label: "Label" }, t.input({ type: "text", placeholder: cfg.field, value: () => cfg.label || "", oninput: (e) => (cfg.label = e.target.value) }))),
                                    t.div({ className: "col-lg-6" }, field({ label: "Placeholder" }, t.input({ type: "text", value: () => cfg.placeholder || "", oninput: (e) => (cfg.placeholder = e.target.value) }))),
                                    t.div({ className: "col-lg-12" }, field({ label: "Help text" }, t.input({ type: "text", value: () => cfg.help || "", oninput: (e) => (cfg.help = e.target.value) }))),
                                    t.div({ className: "col-lg-4" }, t.div(
                                        { className: "field gp-switch-field" },
                                        t.input({ id: "gpReq_" + cfg.field, type: "checkbox", className: "switch", disabled: !!def?.required, checked: () => !!cfg.required || !!def?.required, onchange: (e) => (cfg.required = e.target.checked) }),
                                        t.label({ htmlFor: "gpReq_" + cfg.field }, t.span({ className: "txt" }, "Required")),
                                    )),
                                    t.div({ className: "col-lg-4" }, field({ label: "Width" }, select({ required: true, options: [{ value: "full", label: "Full" }, { value: "half", label: "Half" }], value: () => cfg.width || "full", onchange: (v) => (cfg.width = v) }))),
                                    def?.type == "relation"
                                        ? t.div({ className: "col-lg-4" }, t.div(
                                            { className: "field gp-switch-field" },
                                            t.input({ id: "gpList_" + cfg.field, type: "checkbox", className: "switch", checked: () => !!cfg.listRecords, onchange: (e) => (cfg.listRecords = e.target.checked) }),
                                            t.label({ htmlFor: "gpList_" + cfg.field }, t.span({ className: "txt" }, "List its records publicly")),
                                        ))
                                        : null,
                                ),
                            );
                        })
                        : t.p({ className: "txt-hint" }, "No fields yet."),
                ),
            t.div({ className: "field gp-bare gp-fb-add" }, select({
                placeholder: "+ Add a field",
                options: () => available().map((f) => ({ value: f.name, label: `${f.name} · ${f.type}` })),
                value: () => "",
                onchange: addField,
            })),

            t.h6({ className: "gp-section-title" }, "Fixed values"),
            t.div({ className: "field-help m-b-5" }, "Set on every answer and never shown. Macros: @now, @query.<param> (from the page URL, e.g. ?src=newsletter), @request.auth.id (forms requiring a login)."),
            () =>
                t.div(
                    { rid: "fixed-" + data.form.fixedRows.length, className: "gp-fb-fixed" },
                    data.form.fixedRows.map((row, i) =>
                        t.div(
                            { className: "gp-fb-fixed-row" },
                            t.div({ className: "field gp-bare" }, select({
                                required: true,
                                placeholder: "Field",
                                options: () => (collection()?.fields || []).filter((f) => !notFixable.has(f.type) && f.name != "id" && !data.form.fields.some((x) => x.field == f.name)).map((f) => ({ value: f.name, label: `${f.name} · ${f.type}` })),
                                value: () => row.field,
                                onchange: (v) => (row.field = v),
                            })),
                            () => {
                                const def = fieldDef(row.field);
                                if (def?.type == "bool") {
                                    return t.div({ rid: "bool", className: "field gp-bare" }, select({ required: true, options: [{ value: "true", label: "Yes" }, { value: "false", label: "No" }], value: () => String(row.value === true || row.value === "true"), onchange: (v) => (row.value = v == "true") }));
                                }
                                if (def?.type == "select" && (def.maxSelect || 1) <= 1) {
                                    return t.div({ rid: "sel-" + row.field, className: "field gp-bare" }, select({ required: true, options: (def.values || []).map((v) => ({ value: v, label: v })), value: () => row.value, onchange: (v) => (row.value = v) }));
                                }
                                return t.div({ rid: "txt", className: "field gp-bare" }, t.input({ type: "text", placeholder: "Value or macro", value: () => row.value ?? "", oninput: (e) => (row.value = e.target.value) }));
                            },
                            t.button({ type: "button", className: "btn sm circle transparent danger", title: "Remove", onclick: () => (data.form.fixedRows = data.form.fixedRows.filter((_, j) => j != i)) }, t.i({ className: "ri-close-line", ariaHidden: true })),
                        )
                    ),
                ),
            t.button({ type: "button", className: "btn sm transparent secondary", onclick: () => (data.form.fixedRows = [...data.form.fixedRows, { field: "", value: "" }]) }, t.i({ className: "ri-add-line", ariaHidden: true }), t.span({ className: "txt" }, "Add fixed value")),
        );
    }

    function textsSection() {
        const f = data.form;
        const input = (label, key, attrs = {}) => field({ label, hint: attrs.hint }, (attrs.rows ? t.textarea : t.input)({ type: attrs.type || "text", rows: attrs.rows, placeholder: attrs.placeholder || "", value: () => f[key] || "", oninput: (e) => (f[key] = e.target.value) }));
        return t.div(
            { className: "grid sm" },
            t.div({ className: "col-lg-12" }, input("Title", "title")),
            t.div({ className: "col-lg-12" }, input("Description", "description", { rows: 3, hint: "Shown above the fields. Blank lines start paragraphs, links are clickable." })),
            t.div({ className: "col-lg-6" }, input("Submit button", "submit_label", { placeholder: "Submit" })),
            t.div({ className: "col-lg-6" }, input("Redirect after submit", "redirect_url", { type: "url", placeholder: "https://… (optional)" })),
            t.div({ className: "col-lg-12" }, input("Success message", "success_message", { rows: 2 })),
            t.div({ className: "col-lg-6" }, field({ label: "Theme" }, select({ required: true, options: [{ value: "auto", label: "Automatic (visitor setting)" }, { value: "light", label: "Light" }, { value: "dark", label: "Dark" }], value: () => f.theme || "auto", onchange: (v) => (f.theme = v) }))),
            t.div({ className: "col-lg-6" }, field({ label: "Accent color", hint: "Submit button color (CSS color)." }, t.input({ type: "text", placeholder: "#1e6bd6", value: () => f.accent || "", oninput: (e) => (f.accent = e.target.value) }))),
        );
    }

    function publishSection() {
        const f = data.form;
        return t.div(
            { className: "grid sm" },
            t.div({ className: "col-lg-12" }, t.div(
                { className: "field" },
                t.input({ id: "gpFormActive", type: "checkbox", className: "switch", checked: () => !!f.active, onchange: (e) => (f.active = e.target.checked) }),
                t.label({ htmlFor: "gpFormActive" }, t.span({ className: "txt" }, () => (f.active ? "Published: anyone with the link can answer" : "Draft: only you can preview it"))),
            )),
            t.div({ className: "col-lg-6" }, field({ label: "Name" }, t.input({ type: "text", required: true, value: () => f.name, oninput: (e) => (f.name = e.target.value) }))),
            t.div({ className: "col-lg-6" }, field({ label: "URL slug" }, t.input({ type: "text", className: "txt-code", required: true, pattern: "[a-z0-9][a-z0-9-]*", value: () => f.slug, oninput: (e) => (f.slug = e.target.value) }))),
            t.div(
                { className: "col-lg-12 gp-fb-link" },
                t.label({ className: "gp-label" }, "Page"),
                t.div(
                    { className: "flex gap-5" },
                    t.code({ className: "gp-fb-url" }, () => pageURL(f.slug)),
                    t.button({ type: "button", className: "btn sm transparent secondary", onclick: () => copy(pageURL(f.slug), "link") }, t.i({ className: "ri-file-copy-line", ariaHidden: true }), t.span({ className: "txt" }, "Copy")),
                    t.a({ className: "btn sm transparent secondary", href: pageURL(f.slug) + (f.active ? "" : "?preview=1"), target: "_blank", rel: "noopener" }, t.i({ className: "ri-external-link-line", ariaHidden: true }), t.span({ className: "txt" }, "Open")),
                ),
            ),
            t.div(
                { className: "col-lg-12" },
                t.label({ className: "gp-label" }, "Embed code"),
                t.textarea({ className: "txt-code gp-fb-snippet", rows: 8, readOnly: true, value: () => embedSnippet(f.slug) }),
                t.button({ type: "button", className: "btn sm transparent secondary m-t-5", onclick: () => copy(embedSnippet(f.slug), "embed code") }, t.i({ className: "ri-file-copy-line", ariaHidden: true }), t.span({ className: "txt" }, "Copy the embed code")),
            ),
            t.div({ className: "col-lg-12" }, field(
                { label: "Sites allowed to embed the form", hint: "One per line, e.g. https://www.example.com. Other sites can't show the form in an iframe nor submit it from their pages." },
                t.textarea({ rows: 3, className: "txt-code", placeholder: "https://www.example.com", value: () => f.domainsText, oninput: (e) => (f.domainsText = e.target.value) }),
            )),
            t.div({ className: "col-lg-6" }, t.div(
                { className: "field gp-switch-field" },
                t.input({ id: "gpFormAuth", type: "checkbox", className: "switch", checked: () => !!f.require_auth, onchange: (e) => (f.require_auth = e.target.checked) }),
                t.label({ htmlFor: "gpFormAuth" }, t.span({ className: "txt" }, "Require a login (users)")),
            )),
            t.div({ className: "col-lg-6" }, field({ label: "Answers per visitor and hour", hint: "0 = unlimited." }, t.input({ type: "number", min: 0, value: () => f.rate_limit ?? 20, oninput: (e) => (f.rate_limit = e.target.value) }))),
        );
    }

    return page(
        {
            pbEvent: "gpPageForm",
            className: "gp-page-form",
            sidebar: formsSidebar(),
            wide: true,
            breadcrumbs: [t.a({ href: "#/forms" }, "Forms"), t.span(null, () => data.form?.name || "...")],
            buttons: () =>
                data.form
                    ? t.div(
                        { rid: "btns", className: "flex gap-10" },
                        t.button({ type: "button", className: "btn circle transparent danger", ariaLabel: app.attrs.tooltip("Delete"), onclick: remove }, t.i({ className: "ri-delete-bin-7-line", ariaHidden: true })),
                        t.button(
                            { type: "button", className: () => `btn expanded ${data.isSaving ? "loading" : ""}`, disabled: () => data.isSaving || !dirty(), onclick: save },
                            t.span({ className: "txt" }, "Save changes"),
                        ),
                    )
                    : null,
        },
        () => {
            if (!data.form) {
                return t.div({ rid: "loading", className: "txt-center p-base" }, t.span({ className: "loader" }));
            }
            return t.div(
                { rid: "editor", className: "gp-fb" },
                t.div(
                    { className: "gp-fb-main" },
                    t.div(
                        { className: "tabs-header" },
                        [["fields", "Fields", "ri-list-check"], ["texts", "Texts & look", "ri-text"], ["publish", "Publish", "ri-share-line"]].map(([tab, label, icon]) =>
                            t.button({ type: "button", className: () => `tab-item ${data.tab == tab ? "active" : ""}`, onclick: () => (data.tab = tab) }, t.i({ className: icon, ariaHidden: true }), t.span({ className: "txt" }, label))
                        ),
                    ),
                    t.div({ className: "gp-fb-tab" }, () => t.div({ rid: data.tab }, data.tab == "fields" ? fieldsSection() : data.tab == "texts" ? textsSection() : publishSection())),
                ),
                t.div(
                    { className: "gp-fb-preview" },
                    t.div({ className: "flex gap-5 m-b-5" }, t.label({ className: "gp-label m-0" }, "Preview"), t.span({ className: "txt-hint txt-sm" }, () => (dirty() ? "showing unsaved changes" : ""))),
                    () =>
                        t.iframe({
                            rid: "p" + data.previewKey + data.saved.length,
                            className: "gp-fb-frame",
                            title: "Form preview",
                            src: embedURL(savedSlug()) + "?preview=1",
                            onmount: (el) => mountPreview(el),
                            onunmount: (el) => el._gpCleanup?.(),
                        }),
                ),
            );
        },
    );
}
