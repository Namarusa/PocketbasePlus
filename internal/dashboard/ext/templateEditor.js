import {
    allCollections,
    api,
    debounce,
    emailFrame,
    emailsSidebar,
    field,
    handleError,
    page,
    recordLabel,
    recordVariables,
    select,
    switchInput,
    textInput,
    userCollections,
} from "./lib.js";
import { functionList, functionSnippet } from "./functions.js";

const starterHTML = `<div style="font-family: sans-serif; max-width: 560px; margin: auto">
  <h2>Hello {{default "there" .record.name}},</h2>
  <p>This is an email from {{.app.name}}.</p>
  <p><a href="{{.app.url}}">Open the app</a></p>
</div>`;

const globalVariables = [".app.name", ".app.url", ".recipient.email"];

export function pageTemplateEditor(route) {
    const id = route.params.id;
    const isNew = id == "new";

    app.store.title = isNew ? "New email template" : "Email template";

    const data = store({
        isLoading: !isNew,
        isSaving: false,
        isSendingTest: false,
        form: {
            name: "",
            slug: "",
            subject: "",
            html: starterHTML,
            text: "",
            sample_collection: "",
            marketing: false,
            folder: "",
            account_email: "",
            account_collection: "",
        },
        accounts: [], // the account emails (kind, label, variables)
        folders: [],
        authCollections: [],
        sampleDataText: `{\n  "name": "Ada"\n}`,
        sampleRecordId: "",
        slugTouched: false,
        collections: [],
        sampleRecords: [],
        variables: [],
        previewTab: "html",
        preview: { subject: "", html: "", text: "", error: "" },
        initSerialized: "",
        get serialized() {
            return JSON.stringify([data.form, data.form.sample_collection ? "" : data.sampleDataText]);
        },
        get hasChanges() {
            return data.serialized != data.initSerialized;
        },
    });

    let htmlEditorContent; // the codeEditor contenteditable element

    function parsedSampleData() {
        try {
            return JSON.parse(data.sampleDataText);
        } catch {
            return null;
        }
    }

    function renderBody() {
        return {
            subject: data.form.subject,
            html: data.form.html,
            text: data.form.text,
            collection: data.form.sample_collection,
            recordId: data.sampleRecordId,
            data: data.form.sample_collection ? undefined : parsedSampleData(),
            account: data.form.account_email,
        };
    }

    const refreshPreview = debounce(async () => {
        try {
            const res = await api("/api/emails/preview", { method: "POST", body: renderBody(), requestKey: "gpPreview" });
            data.preview = {
                subject: res.subject || "",
                html: res.html || "",
                text: res.text || "",
                error: res.error || "",
            };
        } catch (err) {
            if (!err.isAbort) {
                data.preview.error = err.message;
            }
        }
    }, 300);

    const account = () => data.accounts.find((a) => a.kind == data.form.account_email);

    async function load() {
        userCollections().then((list) => (data.collections = list));
        allCollections().then((list) => (data.authCollections = list.filter((c) => c.type == "auth").map((c) => c.name)));
        api("/api/emails/accounts", { requestKey: null }).then((list) => (data.accounts = list)).catch(() => {});
        app.pb
            .collection("email_templates")
            .getFullList({ fields: "folder", filter: "folder != ''", requestKey: null })
            .then((list) => (data.folders = [...new Set(list.map((x) => x.folder))].sort((a, b) => a.localeCompare(b))))
            .catch(() => {});
        if (isNew) {
            data.initSerialized = data.serialized;
            return;
        }
        try {
            const tpl = await app.pb.collection("email_templates").getOne(id, { requestKey: "gpTemplate" });
            // update in place: the inputs are bound to this object
            Object.assign(data.form, {
                name: tpl.name,
                slug: tpl.slug,
                subject: tpl.subject,
                html: tpl.html,
                text: tpl.text,
                sample_collection: tpl.sample_collection,
                marketing: !!tpl.marketing,
                folder: tpl.folder || "",
                account_email: tpl.account_email || "",
                account_collection: tpl.account_collection || "",
            });
            if (tpl.sample_data) {
                data.sampleDataText = JSON.stringify(tpl.sample_data, null, 2);
            }
            data.slugTouched = true;
            data.initSerialized = data.serialized;
            app.store.title = tpl.name;
            data.isLoading = false;
        } catch (err) {
            handleError(err);
        }
    }

    async function loadSample(name) {
        data.sampleRecordId = "";
        data.sampleRecords = [];
        data.variables = [];
        if (!name) {
            return;
        }
        data.variables = await recordVariables(name);
        try {
            const res = await app.pb.collection(name).getList(1, 20, { requestKey: "gpSampleRecords" });
            data.sampleRecords = res.items;
        } catch (err) {
            handleError(err);
        }
    }

    async function save() {
        if (data.isSaving) {
            return;
        }
        data.isSaving = true;
        const body = {
            ...data.form,
            sample_data: data.form.sample_collection ? null : parsedSampleData(),
        };
        try {
            if (isNew) {
                const created = await app.pb.collection("email_templates").create(body);
                app.toasts.success("Successfully created the template.");
                data.initSerialized = data.serialized;
                window.location.replace("#/emails/templates/" + created.id);
            } else {
                await app.pb.collection("email_templates").update(id, body);
                data.initSerialized = data.serialized;
                app.store.title = data.form.name;
                app.toasts.success("Successfully saved the template.");
            }
        } catch (err) {
            handleError(err);
        }
        data.isSaving = false;
    }

    async function sendTest() {
        data.isSendingTest = true;
        try {
            const res = await api("/api/emails/test", { method: "POST", body: renderBody() });
            app.toasts.success(`Test email sent to ${res.to}.`);
        } catch (err) {
            handleError(err);
        }
        data.isSendingTest = false;
    }

    const fnState = store({ open: false, search: "", category: "" });

    // inserts text at the html editor caret (or appends it)
    function insert(text) {
        if (htmlEditorContent && document.activeElement == htmlEditorContent) {
            document.execCommand("insertText", false, text);
            return;
        }
        data.form.html += text;
    }

    function chip(label, text) {
        return t.button(
            {
                type: "button",
                className: "label gp-chip",
                // keep the editor focus/caret so the text is inserted where the user was typing
                onmousedown: (e) => e.preventDefault(),
                onclick: () => insert(text),
            },
            label,
        );
    }

    function onKeydown(e) {
        if ((e.ctrlKey || e.metaKey) && e.key == "s") {
            e.preventDefault();
            save();
        }
    }

    const watchers = [];

    load();

    return page(
        {
            pbEvent: "gpPageTemplateEditor",
            className: "gp-page-template-editor",
            sidebar: emailsSidebar(),
            wide: true,
            breadcrumbs: [
                t.a({ href: "#/emails/templates" }, "Templates"),
                t.span(null, () => (isNew ? "New" : data.form.name || "...")),
            ],
            buttons: [
                t.button(
                    {
                        type: "button",
                        className: () => `btn outline ${data.isSendingTest ? "loading" : ""}`,
                        disabled: () => data.isSendingTest,
                        onclick: sendTest,
                        ariaDescription: app.attrs.tooltip("Sends the current (unsaved) version to your superuser email"),
                    },
                    t.i({ className: "ri-mail-send-line", ariaHidden: true }),
                    t.span({ className: "txt" }, "Send test to me"),
                ),
                t.button(
                    {
                        type: "button",
                        className: () => `btn expanded-lg ${data.isSaving ? "loading" : ""}`,
                        disabled: () => data.isSaving || (!isNew && !data.hasChanges),
                        onclick: save,
                    },
                    t.span({ className: "txt" }, () => (isNew ? "Create" : "Save changes")),
                ),
            ],
        },
        t.div(
            {
                className: "grid",
                onmount: (el) => {
                    window.addEventListener("keydown", onKeydown);
                    // live preview on any change of the rendered inputs
                    watchers.push(watch(() => JSON.stringify(renderBody()), () => refreshPreview()));
                    // sample collection change
                    watchers.push(watch(() => data.form.sample_collection, (name) => loadSample(name)));
                    // auto slug from the name for new templates
                    watchers.push(watch(() => data.form.name, (name) => {
                        if (!data.slugTouched) {
                            data.form.slug = name.toLowerCase().replace(/[^a-z0-9]+/g, "-").replace(/^-|-$/g, "");
                        }
                    }));
                    htmlEditorContent = el.querySelector(".gp-html-editor [contenteditable]");
                },
                onunmount: () => {
                    window.removeEventListener("keydown", onKeydown);
                    refreshPreview.cancel();
                    watchers.forEach((w) => w.unwatch());
                },
            },
            t.div(
                { className: "col-lg-6" },
                field({ label: "Name", name: "name" }, textInput(data.form, "name", { required: true })),
            ),
            t.div(
                { className: "col-lg-6" },
                field(
                    { label: "Slug", name: "slug" },
                    textInput(data.form, "slug", {
                        className: "txt-code",
                        oninput: (e) => {
                            data.slugTouched = true;
                            data.form.slug = e.target.value;
                        },
                    }),
                ),
            ),
            t.div(
                { className: "col-lg-12" },
                field(
                    { label: "Subject", name: "subject" },
                    textInput(data.form, "subject", { placeholder: "Welcome {{.record.name}}" }),
                ),
            ),
            // editor column
            t.div(
                { className: "col-lg-6" },
                field(
                    { label: "HTML body", name: "html", className: "gp-html-editor" },
                    app.components.codeEditor({
                        id: "html",
                        name: "html",
                        language: "html",
                        value: () => data.form.html,
                        oninput: (val) => (data.form.html = val),
                        // called with the word being typed
                        autocomplete: (word) => {
                            const w = (word || "").toLowerCase();
                            return [...data.variables.map((v) => `.record.${v}`), ...(account()?.variables || []), ...globalVariables]
                                .filter((v) => v.toLowerCase().includes(w));
                        },
                    }),
                ),
                t.details(
                    { className: "gp-details m-b-sm", open: () => !!data.form.text },
                    t.summary(null, "Plain text body ", t.span({ className: "txt-hint" }, "(optional, improves deliverability)")),
                    field(
                        { name: "text" },
                        t.textarea({
                            id: "text",
                            name: "text",
                            className: "txt-code",
                            rows: 6,
                            value: () => data.form.text,
                            oninput: (e) => (data.form.text = e.target.value),
                        }),
                    ),
                ),
                () =>
                    data.form.account_email
                        ? t.div(
                              { rid: "account", className: "gp-panel m-b-sm" },
                              t.div(
                                  { className: "txt-bold m-b-5" },
                                  t.i({ className: "ri-user-settings-line", ariaHidden: true }),
                                  " Account email: ",
                                  () => account()?.label || data.form.account_email,
                              ),
                              t.div(
                                  { className: "field-help m-b-sm" },
                                  "PocketBase sends this email itself; this template replaces its own (if it fails, PocketBase's is sent). ",
                                  () => (account()?.variables || []).includes(".link")
                                      ? ["Put ", t.code(null, "{{.link}}"), " in a button: it opens the confirmation page."]
                                      : "",
                              ),
                              field(
                                  { label: "Used for the accounts of", name: "account_collection" },
                                  select({
                                      id: "account_collection",
                                      required: true,
                                      options: () => [
                                          { value: "", label: "All auth collections (but the superusers)" },
                                          { value: "*", label: "All auth collections, superusers included" },
                                          ...data.authCollections.map((c) => ({ value: c, label: c })),
                                      ],
                                      value: () => data.form.account_collection,
                                      onchange: (v) => (data.form.account_collection = v),
                                  }),
                              ),
                          )
                        : null,
                t.div(
                    { className: "gp-panel m-b-sm", hidden: () => !!data.form.account_email },
                    field(
                        { label: "Folder", name: "folder", hint: "Groups the templates in the list. A new name creates the folder." },
                        t.input({
                            type: "text",
                            id: "folder",
                            maxLength: 100,
                            placeholder: "No folder",
                            value: () => data.form.folder,
                            oninput: (e) => (data.form.folder = e.target.value),
                        }),
                    ),
                    () =>
                        data.folders.length
                            ? t.div(
                                  { className: "flex flex-wrap gap-5 m-b-sm" },
                                  data.folders.map((f) =>
                                      t.button(
                                          {
                                              type: "button",
                                              rid: "f" + f,
                                              className: () => `label gp-folder-chip ${data.form.folder == f ? "active" : ""}`,
                                              onclick: () => (data.form.folder = data.form.folder == f ? "" : f),
                                          },
                                          t.i({ className: "ri-folder-line", ariaHidden: true }),
                                          " " + f,
                                      )
                                  ),
                              )
                            : null,
                ),
                t.div(
                    { className: "gp-panel m-b-sm", hidden: () => !!data.form.account_email },
                    switchInput(data.form, "marketing", "Marketing email"),
                    t.div(
                        { className: "field-help" },
                        "Promotions and newsletters: an unsubscribe link is added (or use {{.unsubscribe_url}}), and the addresses that unsubscribed are skipped. Leave it off for transactional emails (orders, accounts...).",
                    ),
                ),
                t.div(
                    { className: "gp-panel" },
                    t.div({ className: "txt-bold m-b-sm" }, "Variables"),
                    t.div(
                        { className: "grid sm" },
                        t.div(
                            { className: "col-lg-6" },
                            field(
                                { label: "Preview with records from", name: "sample_collection" },
                                select({
                                    id: "sample_collection",
                                    placeholder: "Sample JSON data",
                                    options: () => data.collections.map((c) => ({ value: c.name, label: c.name })),
                                    value: () => data.form.sample_collection,
                                    onchange: (v) => (data.form.sample_collection = v),
                                }),
                            ),
                        ),
                        t.div(
                            { className: "col-lg-6", hidden: () => !data.form.sample_collection },
                            field(
                                { label: "Record", name: "sample_record" },
                                select({
                                    id: "sample_record",
                                    placeholder: "First record",
                                    options: () => data.sampleRecords.map((r) => ({ value: r.id, label: recordLabel(r) })),
                                    value: () => data.sampleRecordId,
                                    onchange: (v) => (data.sampleRecordId = v),
                                }),
                            ),
                        ),
                        t.div(
                            { className: "col-lg-12", hidden: () => !!data.form.sample_collection },
                            field(
                                { label: "Sample .record data (JSON)", name: "sample_data" },
                                t.textarea({
                                    id: "sample_data",
                                    className: "txt-code",
                                    rows: 4,
                                    value: () => data.sampleDataText,
                                    oninput: (e) => (data.sampleDataText = e.target.value),
                                }),
                            ),
                        ),
                    ),
                    t.div({ className: "field-help m-b-5" }, "Click to insert at the cursor (also suggested while typing):"),
                    t.div(
                        { className: "flex flex-wrap gap-5" },
                        () => data.variables.map((v) => chip(`.record.${v}`, `{{.record.${v}}}`)),
                        () => (account()?.variables || []).map((v) => chip(v, `{{${v}}}`)),
                        globalVariables.map((v) => chip(v, `{{${v}}}`)),
                        () => (data.form.account_email ? null : chip("range .records", "{{range .records}}\n  {{.id}}\n{{end}}")),
                        t.button(
                            {
                                type: "button",
                                className: () => `label gp-chip ${fnState.open ? "active" : ""}`,
                                onmousedown: (e) => e.preventDefault(),
                                onclick: () => (fnState.open = !fnState.open),
                            },
                            t.i({ className: "ri-functions", ariaHidden: true }),
                            " Functions",
                        ),
                    ),
                    () =>
                        fnState.open
                            ? t.div(
                                  { className: "gp-fn-panel m-t-sm" },
                                  t.div(
                                      { className: "field gp-bare" },
                                      t.input({ type: "search", placeholder: "Search functions...", value: () => fnState.search, oninput: (e) => (fnState.search = e.target.value) }),
                                  ),
                                  functionList(fnState, (f) => insert(functionSnippet(f).text)),
                              )
                            : null,
                    t.div(
                        { className: "field-help m-t-sm" },
                        "Functions: ",
                        t.code(null, `{{upper .record.name}}`), ", ",
                        t.code(null, `{{default "n/a" .record.x}}`), ", ",
                        t.code(null, `{{formatDate "DD/MM/YYYY" .record.created}}`), ", ",
                        t.code(null, `{{currency "EUR" .record.total}}`), ", ",
                        t.code(null, `{{if .record.paid}}...{{end}}`),
                        " (all of them in Functions). ",
                        t.code(null, ".records"),
                        " holds the matching records in scheduled digests. Values are HTML-escaped automatically.",
                    ),
                ),
            ),
            // preview column
            t.div(
                { className: "col-lg-6" },
                t.div(
                    { className: "gp-panel gp-preview" },
                    t.div(
                        { className: "flex gap-5 m-b-sm" },
                        t.div({ className: "txt-bold m-r-auto" }, "Preview"),
                        ["html", "text"].map((tab) =>
                            t.button(
                                {
                                    type: "button",
                                    className: () => `btn xs ${data.previewTab == tab ? "" : "secondary transparent"}`,
                                    onclick: () => (data.previewTab = tab),
                                },
                                tab.toUpperCase(),
                            )
                        ),
                    ),
                    t.div({ className: "alert danger m-b-sm gp-pre", hidden: () => !data.preview.error }, () => data.preview.error),
                    t.div(
                        { className: "gp-preview-subject" },
                        t.span({ className: "txt-hint" }, "Subject: "),
                        () => data.preview.subject,
                    ),
                    () => {
                        if (data.previewTab == "text") {
                            return t.pre({ rid: "text", className: "gp-text-preview" }, () => data.preview.text || "(no plain text body)");
                        }
                        return t.div({ rid: "html" }, emailFrame(() => data.preview.html));
                    },
                ),
            ),
        ),
    );
}
