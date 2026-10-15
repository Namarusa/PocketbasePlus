import { allCollections, api, emailsSidebar, field, formatDate, handleError, page, select } from "./lib.js";

const OPEN_KEY = "gpTemplateFoldersOpen";
const ACCOUNTS = "\u0000accounts"; // the key of the account emails group

const collectionsLabel = (c) => (c == "*" ? "all, superusers included" : c || "all auth collections");

export function pageTemplates() {
    app.store.title = "Email templates";

    const data = store({
        templates: [],
        accounts: [], // the account emails (kind, label, description)
        authCollections: [],
        isLoading: true,
        search: "",
        closed: app.utils.getLocalHistory(OPEN_KEY, {}), // folder → collapsed
    });

    async function load() {
        data.isLoading = true;
        try {
            const [templates, accounts] = await Promise.all([
                app.pb.collection("email_templates").getFullList({
                    sort: "name",
                    fields: "id,name,slug,subject,folder,account_email,account_collection,marketing,updated",
                    requestKey: "gpTemplates",
                }),
                data.accounts.length ? data.accounts : api("/api/emails/accounts", { requestKey: "gpAccountEmails" }),
            ]);
            data.templates = templates;
            data.accounts = accounts;
        } catch (err) {
            handleError(err);
        }
        data.isLoading = false;
    }
    allCollections().then((list) => (data.authCollections = list.filter((c) => c.type == "auth").map((c) => c.name)));

    const folders = () => [...new Set(data.templates.map((t) => t.folder).filter(Boolean))].sort((a, b) => a.localeCompare(b));

    function matches(tpl) {
        const q = data.search.trim().toLowerCase();
        return !q || [tpl.name, tpl.subject, tpl.slug, tpl.folder].some((v) => (v || "").toLowerCase().includes(q));
    }

    function toggle(key) {
        data.closed[key] = !data.closed[key];
        app.utils.saveLocalHistory(OPEN_KEY, JSON.stringify(data.closed));
    }

    async function duplicate(tpl) {
        try {
            const full = await app.pb.collection("email_templates").getOne(tpl.id);
            delete full.id;
            await app.pb.collection("email_templates").create({
                ...full,
                name: full.name + " (copy)",
                slug: `${full.slug}-copy-${Date.now().toString(36)}`,
            });
            app.toasts.success("Successfully duplicated the template.");
            load();
        } catch (err) {
            handleError(err);
        }
    }

    function remove(tpl) {
        const what = tpl.account_email
            ? `Do you really want to delete "${tpl.name}"? PocketBase's own email is sent again.`
            : `Do you really want to delete "${tpl.name}"? The automations using it will be deleted too.`;
        app.modals.confirm(what, async () => {
            try {
                await app.pb.collection("email_templates").delete(tpl.id);
                app.toasts.success("Successfully deleted the template.");
                load();
            } catch (err) {
                handleError(err);
            }
        });
    }

    // --- folders ----------------------------------------------------------------------------

    function moveModal(list) {
        const state = store({ folder: list.length == 1 ? list[0].folder || "" : "", isSaving: false });
        async function move() {
            state.isSaving = true;
            try {
                for (const tpl of list) {
                    await app.pb.collection("email_templates").update(tpl.id, { folder: state.folder.trim() });
                }
                app.modals.close(modal);
                load();
            } catch (err) {
                handleError(err);
            }
            state.isSaving = false;
        }
        const modal = t.div(
            { className: "modal popup sm" },
            t.header({ className: "modal-header" }, t.h5({ className: "m-auto" }, list.length == 1 ? `Move “${list[0].name}”` : `Move ${list.length} templates`)),
            t.form(
                { id: "gpMoveTemplate", className: "modal-content", onsubmit: (e) => (e.preventDefault(), move()) },
                field(
                    { label: "Folder", name: "folder", hint: "A new name creates the folder. Empty: no folder." },
                    t.input({
                        type: "text",
                        id: "folder",
                        maxLength: 100,
                        autofocus: true,
                        placeholder: "e.g. Orders",
                        value: () => state.folder,
                        oninput: (e) => (state.folder = e.target.value),
                    }),
                ),
                () =>
                    folders().length
                        ? t.div(
                              { className: "flex flex-wrap gap-5" },
                              folders().map((f) =>
                                  t.button(
                                      {
                                          type: "button",
                                          rid: "f" + f,
                                          className: () => `label gp-folder-chip ${state.folder == f ? "active" : ""}`,
                                          onclick: () => (state.folder = f),
                                      },
                                      t.i({ className: "ri-folder-line", ariaHidden: true }),
                                      " " + f,
                                  )
                              ),
                          )
                        : null,
            ),
            t.footer(
                { className: "modal-footer" },
                t.button({ type: "button", className: "btn transparent secondary m-r-auto", onclick: () => app.modals.close(modal) }, t.span({ className: "txt" }, "Cancel")),
                t.button({ type: "submit", "html-form": "gpMoveTemplate", className: () => `btn expanded ${state.isSaving ? "loading" : ""}`, disabled: () => state.isSaving }, t.span({ className: "txt" }, "Move")),
            ),
        );
        modal.onafterclose = () => modal.remove();
        document.body.appendChild(modal);
        app.modals.open(modal);
    }

    function renameFolder(name) {
        const state = store({ name, isSaving: false });
        async function rename() {
            state.isSaving = true;
            try {
                for (const tpl of data.templates.filter((x) => x.folder == name && !x.account_email)) {
                    await app.pb.collection("email_templates").update(tpl.id, { folder: state.name.trim() });
                }
                if (data.closed[name]) {
                    toggle(name);
                }
                app.modals.close(modal);
                load();
            } catch (err) {
                handleError(err);
            }
            state.isSaving = false;
        }
        const modal = t.div(
            { className: "modal popup sm" },
            t.header({ className: "modal-header" }, t.h5({ className: "m-auto" }, "Rename the folder")),
            t.form(
                { id: "gpRenameFolder", className: "modal-content", onsubmit: (e) => (e.preventDefault(), rename()) },
                field(
                    { label: "Name", name: "folder_name", hint: "Empty: its templates leave the folder (they aren't deleted)." },
                    t.input({ type: "text", id: "folder_name", maxLength: 100, autofocus: true, value: () => state.name, oninput: (e) => (state.name = e.target.value) }),
                ),
            ),
            t.footer(
                { className: "modal-footer" },
                t.button({ type: "button", className: "btn transparent secondary m-r-auto", onclick: () => app.modals.close(modal) }, t.span({ className: "txt" }, "Cancel")),
                t.button({ type: "submit", "html-form": "gpRenameFolder", className: () => `btn expanded ${state.isSaving ? "loading" : ""}`, disabled: () => state.isSaving }, t.span({ className: "txt" }, "Save")),
            ),
        );
        modal.onafterclose = () => modal.remove();
        document.body.appendChild(modal);
        app.modals.open(modal);
    }

    // --- account emails -----------------------------------------------------------------------

    function customizeModal(account) {
        const taken = new Set(data.templates.filter((x) => x.account_email == account.kind).map((x) => x.account_collection || ""));
        const options = () => [
            { value: "", label: "All auth collections (but the superusers)" },
            { value: "*", label: "All auth collections, superusers included" },
            ...data.authCollections.map((c) => ({ value: c, label: c })),
        ].filter((o) => !taken.has(o.value) && !(o.value == "" && taken.has("*")) && !(o.value == "*" && taken.has("")));
        const state = store({ collection: options()[0]?.value ?? "", isSaving: false });
        async function create() {
            state.isSaving = true;
            try {
                const res = await api("/api/emails/accounts/customize", { method: "POST", body: { kind: account.kind, collection: state.collection } });
                app.modals.close(modal);
                window.location.hash = "#/emails/templates/" + res.id;
            } catch (err) {
                handleError(err);
            }
            state.isSaving = false;
        }
        const modal = t.div(
            { className: "modal popup sm" },
            t.header({ className: "modal-header" }, t.h5({ className: "m-auto" }, `Design the ${account.label.toLowerCase()} email`)),
            t.form(
                { id: "gpCustomizeAccount", className: "modal-content", onsubmit: (e) => (e.preventDefault(), create()) },
                t.p({ className: "txt-hint m-t-0" }, account.description, " The template starts from the email PocketBase sends now."),
                field(
                    { label: "Used for the accounts of", name: "account_collection" },
                    select({
                        id: "account_collection",
                        required: true,
                        options,
                        value: () => state.collection,
                        onchange: (v) => (state.collection = v),
                    }),
                ),
            ),
            t.footer(
                { className: "modal-footer" },
                t.button({ type: "button", className: "btn transparent secondary m-r-auto", onclick: () => app.modals.close(modal) }, t.span({ className: "txt" }, "Cancel")),
                t.button({ type: "submit", "html-form": "gpCustomizeAccount", className: () => `btn expanded ${state.isSaving ? "loading" : ""}`, disabled: () => state.isSaving || !options().length }, t.span({ className: "txt" }, "Design it")),
            ),
        );
        modal.onafterclose = () => modal.remove();
        document.body.appendChild(modal);
        app.modals.open(modal);
    }

    // --- rows -----------------------------------------------------------------------------------

    function actions(tpl) {
        return t.td(
            { className: "txt-right txt-nowrap" },
            tpl.account_email
                ? null
                : [
                      t.a(
                          { className: "btn sm circle transparent secondary", href: "#/emails/send?template=" + tpl.id, ariaLabel: app.attrs.tooltip("Send") },
                          t.i({ className: "ri-send-plane-line", ariaHidden: true }),
                      ),
                      t.button(
                          { type: "button", className: "btn sm circle transparent secondary", ariaLabel: app.attrs.tooltip("Move to a folder"), onclick: () => moveModal([tpl]) },
                          t.i({ className: "ri-folder-transfer-line", ariaHidden: true }),
                      ),
                      t.button(
                          { type: "button", className: "btn sm circle transparent secondary", ariaLabel: app.attrs.tooltip("Duplicate"), onclick: () => duplicate(tpl) },
                          t.i({ className: "ri-file-copy-line", ariaHidden: true }),
                      ),
                  ],
            t.button(
                { type: "button", className: "btn sm circle transparent danger", ariaLabel: app.attrs.tooltip(tpl.account_email ? "Delete (back to PocketBase's email)" : "Delete"), onclick: () => remove(tpl) },
                t.i({ className: "ri-delete-bin-7-line", ariaHidden: true }),
            ),
        );
    }

    function templateRow(tpl) {
        return t.tr(
            { rid: tpl.id + tpl.updated + tpl.folder },
            t.td(
                null,
                t.a({ href: "#/emails/templates/" + tpl.id, className: "txt-bold" }, tpl.name),
                tpl.marketing ? t.span({ className: "label sm m-l-5" }, "marketing") : null,
            ),
            t.td(null, t.span({ className: "label sm" }, tpl.slug)),
            t.td({ className: "txt-hint gp-ellipsis" }, tpl.subject),
            t.td({ className: "txt-hint txt-sm txt-nowrap" }, formatDate(tpl.updated)),
            actions(tpl),
        );
    }

    function accountRows(account) {
        const list = data.templates.filter((x) => x.account_email == account.kind);
        const free = list.length < data.authCollections.length + 1; // a collection (or all of them: "" or "*") is left
        const rows = [
            t.tr(
                { rid: "a" + account.kind + list.length + "/" + data.authCollections.length },
                t.td(
                    { colSpan: 4 },
                    t.div({ className: "txt-bold" }, account.label),
                    t.div({ className: "txt-hint txt-sm" }, account.description, list.length ? "" : " PocketBase's own email is sent."),
                ),
                t.td(
                    { className: "txt-right txt-nowrap" },
                    free
                        ? t.button(
                              { type: "button", className: "btn sm outline", onclick: () => customizeModal(account) },
                              t.i({ className: "ri-palette-line", ariaHidden: true }),
                              t.span({ className: "txt" }, list.length ? "Another collection" : "Design it"),
                          )
                        : null,
                ),
            ),
        ];
        for (const tpl of list) {
            rows.push(
                t.tr(
                    { rid: tpl.id + tpl.updated, className: "gp-sub-row" },
                    t.td(
                        null,
                        t.i({ className: "ri-corner-down-right-line txt-hint m-r-5", ariaHidden: true }),
                        t.a({ href: "#/emails/templates/" + tpl.id, className: "txt-bold" }, tpl.name),
                    ),
                    t.td(null, t.span({ className: "label sm" }, collectionsLabel(tpl.account_collection))),
                    t.td({ className: "txt-hint gp-ellipsis" }, tpl.subject),
                    t.td({ className: "txt-hint txt-sm txt-nowrap" }, formatDate(tpl.updated)),
                    actions(tpl),
                ),
            );
        }
        return rows;
    }

    function groupHeader(key, icon, label, count, extra = null) {
        return t.tr(
            { rid: "h" + key + count + !!data.closed[key], className: "gp-folder-row" },
            t.td(
                { colSpan: 5 },
                t.div(
                    { className: "flex gap-5" },
                    t.button(
                        { type: "button", className: "gp-folder-toggle", ariaExpanded: !data.closed[key], onclick: () => toggle(key) },
                        t.i({ className: data.closed[key] ? "ri-arrow-right-s-line" : "ri-arrow-down-s-line", ariaHidden: true }),
                        t.i({ className: icon, ariaHidden: true }),
                        t.span({ className: "txt-bold" }, label),
                        t.span({ className: "label sm" }, count),
                    ),
                    extra,
                ),
            ),
        );
    }

    function body() {
        if (data.isLoading && !data.templates.length && !data.accounts.length) {
            return t.tr({ rid: "loading" }, t.td({ colSpan: 5, className: "txt-center" }, t.span({ className: "loader" })));
        }
        const rows = [];
        const searching = !!data.search.trim();

        // the account emails
        const accounts = data.accounts.filter((a) => !searching || data.templates.some((x) => x.account_email == a.kind && matches(x)) || a.label.toLowerCase().includes(data.search.trim().toLowerCase()));
        if (accounts.length) {
            const customized = data.templates.filter((x) => x.account_email).length;
            rows.push(groupHeader(ACCOUNTS, "ri-user-settings-line", "Account emails", `${customized} / ${data.accounts.length}`));
            if (!data.closed[ACCOUNTS] || searching) {
                accounts.forEach((a) => rows.push(...accountRows(a)));
            }
        }

        // the folders, then the templates without one
        const own = data.templates.filter((x) => !x.account_email && matches(x));
        for (const f of folders()) {
            const list = own.filter((x) => x.folder == f);
            if (!list.length) {
                continue;
            }
            rows.push(
                groupHeader(f, "ri-folder-line", f, list.length,
                    t.button(
                        { type: "button", className: "btn xs circle transparent secondary gp-folder-edit", ariaLabel: app.attrs.tooltip("Rename the folder"), onclick: () => renameFolder(f) },
                        t.i({ className: "ri-pencil-line", ariaHidden: true }),
                    ),
                ),
            );
            if (!data.closed[f] || searching) {
                rows.push(...list.map(templateRow));
            }
        }
        const loose = own.filter((x) => !x.folder);
        if (loose.length && folders().length) {
            rows.push(groupHeader("", "ri-file-list-line", "Other templates", loose.length));
        }
        if (!data.closed[""] || searching || !folders().length) {
            rows.push(...loose.map(templateRow));
        }
        if (!own.length) {
            rows.push(
                t.tr(
                    { rid: "empty" + searching },
                    t.td(
                        { colSpan: 5, className: "txt-center txt-hint" },
                        searching ? "No template matches." : ["No templates yet. ", t.a({ href: "#/emails/templates/new" }, "Create one"), "."],
                    ),
                ),
            );
        }
        return rows;
    }

    load();

    return page(
        {
            pbEvent: "gpPageTemplates",
            sidebar: emailsSidebar(),
            breadcrumbs: ["Emails", "Templates"],
            buttons: t.a(
                { className: "btn expanded-lg", href: "#/emails/templates/new" },
                t.i({ className: "ri-add-line", ariaHidden: true }),
                t.span({ className: "txt" }, "New template"),
            ),
        },
        t.p(
            { className: "txt-hint m-t-0" },
            "HTML email templates with variables like ",
            t.code(null, "{{.record.name}}"),
            ". Use them for manual sends and automations, or to design the account emails (verification, password reset...).",
        ),
        t.div(
            { className: "field gp-bare gp-templates-search m-b-sm" },
            t.input({ type: "search", placeholder: "Search templates...", value: () => data.search, oninput: (e) => (data.search = e.target.value) }),
        ),
        t.div(
            { className: "gp-table-wrap" },
            t.table(
                { className: "gp-templates-table" },
                t.thead(
                    null,
                    t.tr(null, t.th(null, "Name"), t.th(null, "Slug"), t.th(null, "Subject"), t.th(null, "Updated"), t.th(null, "")),
                ),
                t.tbody(null, body),
            ),
        ),
    );
}
