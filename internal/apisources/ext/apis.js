// APIs module: the outside APIs (base URL, sign-in, shared headers, secret)
// and their saved calls (variables, body, response format, named outputs),
// tested here and run by the "API call" step of the flows.

import { api, bare, field, handleError, page, select, sidebar } from "../pocketbaseplus/lib.js";
import { namesReady, parseCurl, splitURL, templateNames, varNameFor } from "./assist.js";

const base = app.pb.buildURL("/_/extensions/pocketbaseplus-apis/");
if (!document.querySelector(`link[href="${base}apis.css"]`)) {
    document.head.appendChild(t.link({ rel: "stylesheet", href: base + "apis.css" }));
}

const authTypes = [
    { value: "none", label: "None" },
    { value: "bearer", label: "Bearer token" },
    { value: "basic", label: "Basic auth (username + password)" },
    { value: "header", label: "API key in a header" },
    { value: "query", label: "API key in the query string" },
];
const methods = ["GET", "POST", "PUT", "PATCH", "DELETE", "HEAD"].map((m) => ({ value: m, label: m }));
const bodyTypes = [
    { value: "none", label: "No body" },
    { value: "json", label: "JSON" },
    { value: "form", label: "Form (URL encoded)" },
    { value: "xml", label: "XML" },
    { value: "text", label: "Text" },
];
const formats = [
    { value: "auto", label: "Automatic (from the response)" },
    { value: "json", label: "JSON" },
    { value: "xml", label: "XML" },
    { value: "text", label: "Raw text" },
];
const nameRegex = /^[A-Za-z_][A-Za-z0-9_]*$/;

const shared = store({ sources: [], calls: [], loaded: false });
let loading = null;

export function loadAPIs() {
    loading ||= Promise.all([
        app.pb.collection("api_sources").getFullList({ sort: "name", requestKey: null }),
        app.pb.collection("api_calls").getFullList({ sort: "name", fields: "id,name,source,method,path,description", requestKey: null }),
    ])
        .then(([sources, calls]) => {
            shared.sources = sources;
            shared.calls = calls;
        })
        .catch(handleError)
        .finally(() => {
            shared.loaded = true;
            loading = null;
        });
    return loading;
}

const callsOf = (sourceId) => shared.calls.filter((c) => c.source == sourceId);
const sourceOf = (id) => shared.sources.find((s) => s.id == id);

function apisSidebar() {
    if (!shared.loaded) {
        loadAPIs();
    }
    return sidebar("gpAPIsSidebar", () => [
        {
            name: "Flows",
            links: [
                { href: "#/flows", label: "All flows", icon: "ri-flow-chart", exact: true },
                { href: "#/apis", label: "APIs", icon: "ri-plug-line", exact: true },
            ],
        },
        ...shared.sources.map((s) => ({
            name: s.name,
            links: [
                ...callsOf(s.id).map((c) => ({
                    href: "#/apis/" + c.id,
                    label: c.name,
                    icon: "ri-arrow-left-right-line",
                    after: t.span({ className: "gp-api-method" }, c.method || "GET"),
                })),
                { href: `#/apis/new?api=${s.id}`, label: "New call", icon: "ri-add-line", isActive: () => window.location.hash == `#/apis/new?api=${s.id}` },
            ],
        })),
    ]);
}

// --- overview -----------------------------------------------------------------------------

export function pageAPIs() {
    app.store.title = "APIs";
    loadAPIs();

    function removeSource(s) {
        app.modals.confirm(`Do you really want to delete the API "${s.name}"?`, async () => {
            try {
                await app.pb.collection("api_sources").delete(s.id);
                app.toasts.success("Deleted the API.");
                loadAPIs();
            } catch (err) {
                handleError(err);
            }
        });
    }

    function removeCall(c) {
        app.modals.confirm(`Do you really want to delete the call "${c.name}"?`, async () => {
            try {
                await app.pb.collection("api_calls").delete(c.id);
                app.toasts.success("Deleted the call.");
                loadAPIs();
            } catch (err) {
                handleError(err);
            }
        });
    }

    return page(
        {
            pbEvent: "gpPageAPIs",
            sidebar: apisSidebar(),
            breadcrumbs: ["Flows", "APIs"],
            buttons: t.button(
                { type: "button", className: "btn expanded-lg", onclick: () => openSourceModal() },
                t.i({ className: "ri-add-line", ariaHidden: true }),
                t.span({ className: "txt" }, "New API"),
            ),
        },
        t.p(
            { className: "txt-hint m-b-sm" },
            "Set up an outside API once (address, sign-in, shared headers), then save its calls with their variables. Flows run them with the ",
            t.strong(null, "API call"),
            " step and use the response fields in the next steps.",
        ),
        () => {
            if (!shared.loaded) {
                return t.div({ rid: "loading", className: "block txt-center" }, t.span({ className: "loader lg" }));
            }
            if (!shared.sources.length) {
                return t.div(
                    { rid: "empty", className: "gp-api-empty" },
                    t.i({ className: "ri-plug-line", ariaHidden: true }),
                    t.p(null, "No APIs yet."),
                    t.button({ type: "button", className: "btn", onclick: () => openSourceModal() }, t.span({ className: "txt" }, "Add an API")),
                );
            }
            return shared.sources.map((s) =>
                t.div(
                    { rid: "src_" + s.id + s.updated, className: "gp-api-card" },
                    t.div(
                        { className: "gp-api-card-head" },
                        t.div(
                            { className: "gp-api-card-title" },
                            t.strong(null, s.name),
                            t.code({ className: "txt-hint" }, s.base_url || "no base address"),
                        ),
                        t.span({ className: "label sm" }, authTypes.find((a) => a.value == (s.auth || "none"))?.label || s.auth),
                        s.auth && s.auth != "none" && !s.secret_hint ? t.span({ className: "label sm danger" }, "No secret") : null,
                        t.button({ type: "button", className: "btn sm transparent secondary", onclick: () => openSourceModal(s) }, t.i({ className: "ri-settings-3-line", ariaHidden: true }), t.span({ className: "txt" }, "Settings")),
                        t.a({ className: "btn sm", href: `#/apis/new?api=${s.id}` }, t.i({ className: "ri-add-line", ariaHidden: true }), t.span({ className: "txt" }, "New call")),
                        t.button({ type: "button", className: "btn sm circle transparent danger", ariaLabel: app.attrs.tooltip("Delete the API"), onclick: () => removeSource(s) }, t.i({ className: "ri-delete-bin-7-line", ariaHidden: true })),
                    ),
                    callsOf(s.id).length
                        ? t.div(
                            { className: "gp-api-calls" },
                            ...callsOf(s.id).map((c) =>
                                t.div(
                                    { className: "gp-api-call-row" },
                                    t.span({ className: "gp-api-method" }, c.method || "GET"),
                                    t.a({ href: "#/apis/" + c.id, className: "txt-bold" }, c.name),
                                    t.code({ className: "txt-hint gp-api-path" }, c.path || "/"),
                                    t.button({ type: "button", className: "btn xs circle transparent danger", ariaLabel: app.attrs.tooltip("Delete the call"), onclick: () => removeCall(c) }, t.i({ className: "ri-close-line", ariaHidden: true })),
                                )
                            ),
                        )
                        : t.div({ className: "txt-hint txt-sm p-10" }, "No calls yet."),
                )
            );
        },
    );
}

// --- API settings ---------------------------------------------------------------------------

function openSourceModal(existing = null) {
    const data = store({
        name: existing?.name || "",
        base_url: existing?.base_url || "",
        auth: existing?.auth || "none",
        auth_name: existing?.auth_name || "",
        username: existing?.username || "",
        secret: "",
        headers: pairs(existing?.headers),
        timeout_seconds: existing?.timeout_seconds || "",
        variables: toVariables(existing?.variables),
        showSecret: false,
        isSaving: false,
    });
    const vars = varInserter();
    // the variables of the API's fields, added as they're typed
    const templates = () => [data.base_url, data.auth_name, data.username, data.secret, ...data.headers.map((h) => h.v)];
    const usedNames = () => {
        const used = new Set();
        templates().forEach((src) => templateNames(src).forEach((n) => used.add(n)));
        // a secret stored with variables: those are used
        for (const m of String(existing?.secret_hint || "").matchAll(/\{\{\.vars\.([A-Za-z0-9_]+)\}\}/g)) {
            used.add(m[1]);
        }
        return used;
    };
    const watcher = autoVariables(data, templates, () => [], () => [data.base_url]);

    async function save() {
        data.isSaving = true;
        try {
            const body = {
                name: data.name,
                base_url: data.base_url,
                auth: data.auth,
                auth_name: data.auth_name,
                username: data.username,
                headers: fromPairs(data.headers),
                timeout_seconds: data.timeout_seconds || null,
                variables: fromVariables(data.variables),
            };
            // an empty secret field keeps the current secret
            if (data.secret || !existing) {
                body.secret = data.secret;
            }
            existing ? await app.pb.collection("api_sources").update(existing.id, body) : await app.pb.collection("api_sources").create(body);
            app.toasts.success("Successfully saved the API.");
            app.modals.close(modal);
            loadAPIs();
        } catch (err) {
            handleError(err);
        }
        data.isSaving = false;
    }

    const secretLabel = () => ({ bearer: "Token", basic: "Password", header: "API key", query: "API key" })[data.auth] || "Secret";

    const modal = t.div(
        { className: "modal popup gp-api-modal" },
        t.header({ className: "modal-header" }, t.h5({ className: "m-auto" }, existing ? "API settings" : "New API")),
        t.form(
            {
                id: "gpAPISourceForm",
                className: "modal-content",
                inert: () => data.isSaving,
                onmount: (el) => el.addEventListener("focusin", vars.track),
                onsubmit: (e) => {
                    e.preventDefault();
                    save();
                },
            },
            t.div(
                { className: "grid sm" },
                t.div({ className: "col-lg-12" }, field({ label: "Name", name: "name" }, t.input({ id: "name", name: "name", type: "text", required: true, placeholder: "e.g. Stripe", value: () => data.name, oninput: (e) => (data.name = e.target.value) }))),
                t.div(
                    { className: "col-lg-12" },
                    field(
                        { label: "Base address", name: "base_url", hint: "Each call adds its path, e.g. /customers/{{.vars.id}}. Variables work anywhere: https://{{.vars.host}}/v1. Leave empty to type full addresses in the calls." },
                        t.input({ id: "base_url", name: "base_url", type: "text", className: "txt-code gp-api-tpl", placeholder: "https://api.example.com/v1", value: () => data.base_url, oninput: (e) => (data.base_url = e.target.value) }),
                    ),
                ),
                t.div({ className: "col-lg-12" }, field({ label: "Sign-in", name: "auth" }, select({ name: "auth", required: true, options: authTypes, value: () => data.auth, onchange: (v) => (data.auth = v || "none") }))),
                t.div(
                    { className: "col-lg-6", hidden: () => data.auth != "header" && data.auth != "query" },
                    field({ label: () => (data.auth == "query" ? "Parameter name" : "Header name"), name: "auth_name" }, t.input({ id: "auth_name", name: "auth_name", type: "text", className: "gp-api-tpl", placeholder: () => (data.auth == "query" ? "api_key" : "X-API-Key"), value: () => data.auth_name, oninput: (e) => (data.auth_name = e.target.value) })),
                ),
                t.div(
                    { className: "col-lg-6", hidden: () => data.auth != "basic" },
                    field({ label: "Username", name: "username" }, t.input({ id: "username", name: "username", type: "text", className: "gp-api-tpl", autocomplete: "off", value: () => data.username, oninput: (e) => (data.username = e.target.value) })),
                ),
                t.div(
                    { className: "col-lg-6", hidden: () => data.auth == "none" },
                    field(
                        {
                            label: secretLabel,
                            name: "secret",
                            hint: (existing?.secret_hint ? `Stored (${existing.secret_hint}): leave empty to keep it. ` : "") +
                                "Never shown again, nor in the run logs. Or a variable given by the flow, e.g. {{.vars.token}} (its value is hidden in the logs too).",
                        },
                        t.div(
                            { className: "gp-api-secret" },
                            t.input({
                                id: "secret",
                                name: "secret",
                                type: () => (data.showSecret ? "text" : "password"),
                                className: "gp-api-tpl",
                                autocomplete: "new-password",
                                placeholder: existing?.secret_hint ? "Unchanged" : "",
                                value: () => data.secret,
                                oninput: (e) => (data.secret = e.target.value),
                            }),
                            t.button(
                                { type: "button", className: "btn xs circle transparent secondary", ariaLabel: app.attrs.tooltip("Show / hide"), onclick: () => (data.showSecret = !data.showSecret) },
                                t.i({ className: () => (data.showSecret ? "ri-eye-off-line" : "ri-eye-line"), ariaHidden: true }),
                            ),
                        ),
                    ),
                ),
                t.div(
                    { className: "col-lg-12" },
                    t.label({ className: "gp-label" }, "Variables"),
                    t.div(
                        { className: "field-help m-b-5" },
                        "Values given by the flow step to every call of the API: type {{name}} in the base address, the headers or the sign-in and the variable is added here (the { } button inserts one in the last field you clicked).",
                    ),
                    variablesEditor(data, vars.insert, "No variables: type {{name}} in a field to add one.", () => [], usedNames),
                ),
                t.div(
                    { className: "col-lg-12" },
                    t.label({ className: "gp-label" }, "Shared headers"),
                    t.div({ className: "field-help m-b-5" }, "Sent with every call (not secret: use the sign-in for keys)."),
                    pairsEditor(data, "headers", { keyPlaceholder: "Header-Name", addLabel: "Add header", tpl: true }),
                ),
                t.div(
                    { className: "col-lg-6" },
                    field(
                        { label: "Timeout (seconds)", name: "timeout_seconds", hint: "15 by default, 120 at most (retries included)." },
                        t.input({ id: "timeout_seconds", name: "timeout_seconds", type: "number", min: 1, max: 120, value: () => data.timeout_seconds, oninput: (e) => (data.timeout_seconds = e.target.value === "" ? "" : Number(e.target.value)) }),
                    ),
                ),
            ),
        ),
        t.footer(
            { className: "modal-footer" },
            t.button({ type: "button", className: "btn transparent secondary m-r-auto", onclick: () => app.modals.close(modal) }, t.span({ className: "txt" }, "Cancel")),
            t.button({ type: "submit", "html-form": "gpAPISourceForm", className: () => `btn expanded ${data.isSaving ? "loading" : ""}`, disabled: () => data.isSaving }, t.span({ className: "txt" }, existing ? "Save" : "Create")),
        ),
    );
    modal.onafterclose = () => {
        watcher.unwatch();
        modal.remove();
    };
    document.body.appendChild(modal);
    app.modals.open(modal);
}

// name/value pairs edited as a list (an object keeps its keys unique, but
// renaming a key would move it to the end)
function pairs(obj) {
    return Object.entries(obj && typeof obj == "object" ? obj : {}).map(([k, v]) => ({ id: randomId(), k, v: String(v ?? "") }));
}

function fromPairs(list) {
    const out = {};
    for (const p of list) {
        if (p.k.trim()) {
            out[p.k.trim()] = p.v;
        }
    }
    return out;
}

function randomId() {
    return Math.random().toString(36).slice(2, 10);
}

// toVariable(p): the "make it a variable" button of the rows with a fixed value
function pairsEditor(obj, prop, { keyPlaceholder = "name", valuePlaceholder = "value", addLabel = "Add", tpl = false, toVariable = null } = {}) {
    return t.div(
        { className: "gp-kv" },
        () =>
            obj[prop].map((p) =>
                t.div(
                    { rid: "kv_" + p.id, className: `gp-kv-row ${toVariable ? "gp-kv-row-var" : ""}` },
                    bare(t.input({ type: "text", className: "txt-code", placeholder: keyPlaceholder, value: () => p.k, oninput: (e) => (p.k = e.target.value) })),
                    bare(t.input({ type: "text", className: `${tpl ? "gp-api-tpl" : ""}`, placeholder: valuePlaceholder, value: () => p.v, oninput: (e) => (p.v = e.target.value) })),
                    toVariable
                        ? t.button(
                            {
                                type: "button",
                                className: "btn xs secondary transparent",
                                hidden: () => !p.v || p.v.includes("{{") || !p.k.trim(),
                                title: () => `Make it a variable: {{${varNameFor(p.k)}}}, with "${p.v}" as default value (the flows can then give another value)`,
                                onclick: () => toVariable(p),
                            },
                            t.i({ className: "ri-braces-line", ariaHidden: true }),
                            t.span({ className: "txt" }, "Variable"),
                        )
                        : null,
                    t.button(
                        { type: "button", className: "btn sm circle transparent danger", ariaLabel: app.attrs.tooltip("Remove"), onclick: () => (obj[prop] = obj[prop].filter((x) => x.id != p.id)) },
                        t.i({ className: "ri-close-line", ariaHidden: true }),
                    ),
                )
            ),
        t.button(
            { type: "button", className: "btn sm secondary transparent", onclick: () => (obj[prop] = [...obj[prop], { id: randomId(), k: "", v: "" }]) },
            t.i({ className: "ri-add-line", ariaHidden: true }),
            t.span({ className: "txt" }, addLabel),
        ),
    );
}

// --- call editor ------------------------------------------------------------------------------

export function pageCall(route) {
    const id = route.params.id;
    const isNew = id == "new";
    app.store.title = isNew ? "New API call" : "API call";
    loadAPIs();

    const data = store({
        isLoading: !isNew,
        isSaving: false,
        savedId: isNew ? "" : id,
        call: emptyCall(route.query?.api?.[0] || route.query?.api || ""),
        original: "",
        test: { running: false, result: null, tab: "fields", count: 0 },
        preview: null, // the request as it will be sent (dry run with the default values)
    });

    function emptyCall(source) {
        return {
            name: "",
            source,
            description: "",
            method: "GET",
            path: "",
            query: [],
            headers: [],
            body_type: "none",
            body: "",
            form: [],
            variables: [],
            response_format: "auto",
            outputs: [],
            retries: 0,
            retry_unsafe: false,
        };
    }

    function payload() {
        const c = data.call;
        return {
            name: c.name,
            source: c.source,
            description: c.description,
            method: c.method,
            path: c.path,
            query: fromPairs(c.query),
            headers: fromPairs(c.headers),
            body_type: c.body_type,
            body: c.body,
            form: fromPairs(c.form),
            variables: fromVariables(c.variables),
            response_format: c.response_format,
            outputs: c.outputs.filter((o) => o.name.trim()).map((o) => ({ name: o.name.trim(), path: o.path.trim() })),
            retries: Number(c.retries) || 0,
            retry_unsafe: !!c.retry_unsafe,
        };
    }

    const dirty = () => JSON.stringify(payload()) != data.original;

    async function load() {
        try {
            const r = await app.pb.collection("api_calls").getOne(id, { requestKey: "gpAPICall" });
            data.call = {
                ...emptyCall(r.source),
                ...r,
                query: pairs(r.query),
                headers: pairs(r.headers),
                form: pairs(r.form),
                variables: toVariables(r.variables),
                outputs: (r.outputs || []).map((o) => ({ id: randomId(), name: o.name, path: o.path })),
            };
            app.store.title = r.name;
        } catch (err) {
            handleError(err);
        }
        data.original = JSON.stringify(payload());
        data.isLoading = false;
    }

    async function save() {
        data.isSaving = true;
        try {
            const body = payload();
            // the last test response feeds the field picker of the flow editor
            const r = data.test.result;
            if (r && !r.error && r.status) {
                body.sample = { format: r.format, body: prune(r.body, 20, 300) };
            }
            if (!body.name) {
                body.name = `${body.method} ${body.path || "/"}`.slice(0, 100);
                data.call.name = body.name;
            }
            const saved = data.savedId ? await app.pb.collection("api_calls").update(data.savedId, body) : await app.pb.collection("api_calls").create(body);
            data.original = JSON.stringify(payload());
            app.toasts.success("Successfully saved the call.");
            loadAPIs();
            if (!data.savedId) {
                data.savedId = saved.id;
                window.location.hash = "#/apis/" + saved.id;
            }
        } catch (err) {
            handleError(err);
        }
        data.isSaving = false;
    }

    async function remove() {
        app.modals.confirm(`Do you really want to delete the call "${data.call.name}"?`, async () => {
            try {
                await app.pb.collection("api_calls").delete(data.savedId);
                app.toasts.success("Deleted the call.");
                await loadAPIs();
                window.location.hash = "#/apis";
            } catch (err) {
                handleError(err);
            }
        });
    }

    async function runTest() {
        if (!data.call.source) {
            app.toasts.error("Select the API of the call.");
            return;
        }
        data.test.running = true;
        try {
            data.test.result = await api("/api/apisources/test", {
                method: "POST",
                body: { id: data.savedId, source: data.call.source, call: payload() },
                requestKey: null,
            });
            data.test.count++;
            if (data.test.result.error && !data.test.result.status) {
                data.test.tab = "request";
            } else if (data.test.tab == "request") {
                data.test.tab = "fields";
            }
        } catch (err) {
            handleError(err);
        }
        data.test.running = false;
    }

    const source = () => sourceOf(data.call.source);
    const vars = varInserter();
    const insertVar = vars.insert;

    // --- less typing: variables found in the templates, values made variables,
    // pasted addresses and cURL commands, the request previewed as it's edited

    const apiVarNames = () => (source()?.variables || []).map((v) => v.name);
    const callTemplates = () => {
        const c = data.call;
        return [c.path, ...c.query.map((p) => p.v), ...c.headers.map((p) => p.v), c.body_type == "form" ? "" : c.body, ...c.form.map((p) => p.v)];
    };
    const usedNames = () => {
        const used = new Set();
        callTemplates().forEach((src) => templateNames(src).forEach((n) => used.add(n)));
        return used;
    };
    const autoWatcher = autoVariables(data.call, callTemplates, apiVarNames, () => [data.call.path]);

    // a fixed value of a parameter, header or form field becomes a variable
    // (its value is the default): the flows can then give another one
    function toVariable(p) {
        let name = varNameFor(p.k);
        const taken = new Set([...data.call.variables.map((v) => v.name), ...apiVarNames()]);
        for (let n = 2; taken.has(name); n++) {
            name = varNameFor(p.k) + "_" + n;
        }
        data.call.variables = [...data.call.variables, { id: randomId(), name, default: p.v, required: false }];
        p.v = `{{${name}}}`;
        app.toasts.success(`Added the variable "${name}" (default value: the previous one).`);
    }

    // a full address pasted in the path: split into the path and the parameters
    function applyAddress(text) {
        const parts = splitURL(text, source()?.base_url);
        if (!parts) {
            return false;
        }
        const c = data.call;
        c.path = parts.path;
        if (parts.query.length) {
            const rows = c.query.filter((r) => r.k.trim() && !parts.query.some(([k]) => k == r.k));
            c.query = [...rows, ...parts.query.map(([k, v]) => ({ id: randomId(), k, v }))];
        }
        const notes = [];
        if (parts.query.length) {
            notes.push(`${parts.query.length} query parameter${parts.query.length > 1 ? "s" : ""} added below (the "Variable" button makes a value change with each run)`);
        }
        if (!parts.inBase) {
            notes.push("the address isn't under the base address of the API: it's kept whole");
        }
        app.toasts.info(notes.length ? "Split the address: " + notes.join("; ") + "." : "Split the address.");
        return true;
    }

    function openCurlModal() {
        const st = store({ text: "" });
        const apply = () => {
            const r = parseCurl(st.text);
            if (!r) {
                app.toasts.error("Paste a command starting with curl and an address.");
                return;
            }
            const c = data.call;
            c.method = r.method;
            applyAddress(r.url) || (c.path = r.url);
            c.headers = [...c.headers.filter((h) => h.k.trim()), ...r.headers.map(([k, v]) => ({ id: randomId(), k, v }))];
            if (r.bodyType != "none") {
                c.body_type = r.bodyType;
                c.body = r.body;
                c.form = r.form.map(([k, v]) => ({ id: randomId(), k, v }));
            }
            app.modals.close(modal);
            if (r.authorization || r.user) {
                app.toasts.info("The command signs in: set the token or password in the API settings (Sign-in), not in the call.");
            }
        };
        const modal = t.div(
            { className: "modal popup gp-api-modal" },
            t.header({ className: "modal-header" }, t.h5({ className: "m-auto" }, "Import a cURL command")),
            t.div(
                { className: "modal-content" },
                t.p({ className: "txt-hint m-b-sm" }, "Paste a command copied from the documentation of the API or from the browser (Network tab → Copy as cURL): its method, address, headers and body fill the call."),
                field({ name: "curl" }, t.textarea({ name: "curl", rows: 8, className: "txt-code", placeholder: "curl -X POST https://api.example.com/v1/items -H 'Content-Type: application/json' -d '{\"name\":\"Pen\"}'", value: () => st.text, oninput: (e) => (st.text = e.target.value), onmount: (el) => setTimeout(() => el.focus(), 50) })),
            ),
            t.footer(
                { className: "modal-footer" },
                t.button({ type: "button", className: "btn transparent secondary m-r-auto", onclick: () => app.modals.close(modal) }, t.span({ className: "txt" }, "Cancel")),
                t.button({ type: "button", className: "btn expanded", onclick: apply }, t.span({ className: "txt" }, "Import")),
            ),
        );
        modal.onafterclose = () => modal.remove();
        document.body.appendChild(modal);
        app.modals.open(modal);
    }

    // the request as it will be sent, rendered by the server (same escaping)
    let previewTimer;
    let previewSeq = 0;
    const previewWatcher = watch(() => JSON.stringify([payload(), data.call.source, source()?.updated]), () => {
        clearTimeout(previewTimer);
        previewTimer = setTimeout(async () => {
            if (!data.call.source || (!data.call.path && !source()?.base_url)) {
                data.preview = null;
                return;
            }
            const n = ++previewSeq;
            try {
                const res = await api("/api/apisources/test", { method: "POST", body: { source: data.call.source, call: payload(), dryRun: true }, requestKey: null });
                if (n == previewSeq) {
                    data.preview = res;
                }
            } catch (err) {
                if (!err?.isAbort && n == previewSeq) {
                    data.preview = { error: err?.response?.message || String(err) };
                }
            }
        }, 500);
    });

    function addOutput(path) {
        const parts = path.split(".").filter((p) => !/^\d+$/.test(p));
        let name = (parts[parts.length - 1] || "value").replace(/^[@#]/, "").replace(/[^A-Za-z0-9_]/g, "_");
        if (!nameRegex.test(name)) {
            name = "v_" + name;
        }
        let unique = name;
        for (let n = 2; data.call.outputs.some((o) => o.name == unique); n++) {
            unique = name + "_" + n;
        }
        if (data.call.outputs.some((o) => o.path == path)) {
            app.toasts.info("This field is already an output.");
            return;
        }
        data.call.outputs = [...data.call.outputs, { id: randomId(), name: unique, path }];
    }

    if (!isNew) {
        load();
    } else {
        data.original = JSON.stringify(payload());
    }


    return page(
        {
            pbEvent: "gpPageAPICall",
            sidebar: apisSidebar(),
            wide: true,
            breadcrumbs: [t.a({ href: "#/apis" }, "APIs"), () => source()?.name || "…", () => data.call.name || (isNew ? "New call" : "…")],
            buttons: t.div(
                { className: "flex gap-10" },
                () =>
                    data.savedId
                        ? t.button({ rid: "del", type: "button", className: "btn transparent danger", onclick: remove }, t.i({ className: "ri-delete-bin-7-line", ariaHidden: true }), t.span({ className: "txt" }, "Delete"))
                        : null,
                t.button(
                    { type: "button", className: () => `btn expanded-lg ${data.isSaving ? "loading" : ""}`, disabled: () => data.isSaving || data.isLoading || (!dirty() && !!data.savedId), onclick: save },
                    t.span({ className: "txt" }, "Save"),
                ),
            ),
        },
        () => {
            if (data.isLoading) {
                return t.div({ rid: "loading", className: "block txt-center" }, t.span({ className: "loader lg" }));
            }
            return t.div(
                {
                    rid: "editor",
                    className: "gp-api-editor",
                    onmount: (el) => el.addEventListener("focusin", vars.track),
                    onunmount: () => {
                        autoWatcher.unwatch();
                        previewWatcher.unwatch();
                        clearTimeout(previewTimer);
                    },
                },
                t.div({ className: "gp-api-form" }, callForm()),
                t.div({ className: "gp-api-test" }, testPanel()),
            );
        },
    );

    function callForm() {
        const c = data.call;
        return [
            t.div(
                { className: "grid sm" },
                t.div({ className: "col-lg-6" }, field({ label: "Name", name: "name" }, t.input({ id: "name", name: "name", type: "text", placeholder: "e.g. Get customer", value: () => c.name, oninput: (e) => (c.name = e.target.value) }))),
                t.div(
                    { className: "col-lg-6" },
                    field(
                        { label: "API", name: "source" },
                        select({ name: "source", required: true, options: () => shared.sources.map((s) => ({ value: s.id, label: s.name })), value: () => c.source, onchange: (v) => (c.source = v) }),
                    ),
                ),
                t.div({ className: "col-lg-12" }, field({ label: "Description", name: "description" }, t.input({ id: "description", name: "description", type: "text", placeholder: "What the call does (shown in the flow editor)", value: () => c.description, oninput: (e) => (c.description = e.target.value) }))),
            ),

            t.div(
                { className: "gp-api-section gp-api-section-row" },
                t.h6(null, "Request"),
                t.button({ type: "button", className: "btn xs secondary transparent", onclick: openCurlModal }, t.i({ className: "ri-terminal-box-line", ariaHidden: true }), t.span({ className: "txt" }, "Import cURL")),
            ),
            t.div(
                { className: "gp-api-url" },
                // rebuilt with its value: an import (cURL) changes it from the code
                () => t.div({ rid: "method_" + c.method, className: "gp-api-url-method" }, field({ name: "method" }, select({ name: "method", required: true, options: methods, value: () => c.method, onchange: (v) => (c.method = v || "GET") }))),
                t.code({ className: "gp-api-url-base", title: "Base address of the API", hidden: () => !source()?.base_url }, () => (source()?.base_url || "") + "/"),
                t.div(
                    { className: "gp-api-url-path" },
                    field(
                        { name: "path" },
                        t.input({
                            name: "path",
                            type: "text",
                            className: "txt-code gp-api-tpl",
                            placeholder: () => (source()?.base_url ? "customers/{{id}}  (or paste a full address)" : "https://api.example.com/customers/{{id}}"),
                            value: () => c.path,
                            oninput: (e) => (c.path = e.target.value),
                            onpaste: (e) => {
                                const text = e.clipboardData?.getData("text") || "";
                                if (e.target.value.trim() == "" || e.target.selectionStart == 0) {
                                    if (applyAddress(text)) {
                                        e.preventDefault();
                                    }
                                }
                            },
                        }),
                    ),
                ),
            ),
            () => {
                const p = data.preview;
                if (!p) {
                    return null;
                }
                return t.div(
                    { rid: "preview_" + (p.url || "") + (p.error || ""), className: `gp-api-preview ${p.error ? "error" : ""}` },
                    ...(p.error
                        ? [t.i({ className: "ri-error-warning-line", ariaHidden: true }), t.span(null, p.error)]
                        : [
                            t.span({ className: "label sm" }, c.method || "GET"),
                            t.code({ title: "The address sent with the default values of the variables" }, p.url),
                            p.request?.body ? t.span({ className: "txt-hint txt-sm" }, "+ body") : null,
                        ]),
                );
            },

            section("Variables", "Values given by the flow step, with those of the API. Type {{name}} in the path, parameters, headers or body and the variable is added here; its default value is used by Test and when the flow gives none. Values are escaped for their place."),
            () =>
                source()?.variables?.length
                    ? t.div(
                        { rid: "apivars_" + source().id + source().updated, className: "gp-api-apivars" },
                        t.span({ className: "txt-hint txt-sm" }, "From the API:"),
                        ...source().variables.map((v) =>
                            t.button(
                                { type: "button", className: "label sm gp-api-chip", title: `Insert {{.vars.${v.name}}} (test value in the API settings)`, onmousedown: (e) => e.preventDefault(), onclick: () => insertVar(v.name) },
                                t.i({ className: "ri-braces-line", ariaHidden: true }),
                                v.name,
                            )
                        ),
                    )
                    : null,
            variablesEditor(c, insertVar, "No variables of its own: type {{name}} in a field (e.g. customers/{{id}}), or use the \"Variable\" button of a parameter.", () => (source()?.variables || []).map((v) => v.name), usedNames),

            section("Query parameters", "Added to the address. A parameter whose value is a variable is left out when the variable is empty."),
            pairsEditor(c, "query", { keyPlaceholder: "name", addLabel: "Add parameter", tpl: true, toVariable }),
            section("Headers", "In addition to the shared headers of the API and its sign-in."),
            pairsEditor(c, "headers", { keyPlaceholder: "Header-Name", addLabel: "Add header", tpl: true, toVariable }),

            t.div(
                { hidden: () => c.method == "GET" || c.method == "HEAD" },
                section("Body"),
                () => t.div({ rid: "bt_" + c.body_type, className: "gp-api-narrow" }, field({ name: "body_type" }, select({ name: "body_type", required: true, options: bodyTypes, value: () => c.body_type, onchange: (v) => (c.body_type = v || "none") }))),
                t.div(
                    { hidden: () => !["json", "xml", "text"].includes(c.body_type) },
                    field(
                        { name: "body", hint: () => ({ json: "Values in strings are escaped; a variable alone in place of a value is inserted as JSON (lists, numbers...).", xml: "Values are escaped as XML text.", text: "Sent as written." })[c.body_type] || "" },
                        t.textarea({
                            name: "body",
                            rows: 8,
                            className: "txt-code gp-api-tpl",
                            placeholder: () => ({ json: '{\n  "email": "{{.vars.email}}",\n  "tags": {{.vars.tags}}\n}', xml: "<order>\n  <id>{{.vars.id}}</id>\n</order>" })[c.body_type] || "",
                            value: () => c.body,
                            oninput: (e) => (c.body = e.target.value),
                        }),
                    ),
                ),
                t.div({ hidden: () => c.body_type != "form" }, pairsEditor(c, "form", { keyPlaceholder: "field", addLabel: "Add field", tpl: true, toVariable })),
            ),

            section("Response"),
            t.div(
                { className: "grid sm" },
                t.div({ className: "col-lg-6" }, field({ label: "Format", name: "response_format" }, select({ name: "response_format", required: true, options: formats, value: () => c.response_format, onchange: (v) => (c.response_format = v || "auto") }))),
                t.div(
                    { className: "col-lg-6" },
                    field(
                        { label: "Retries", name: "retries", hint: "On network errors, 429 and 5xx answers." },
                        t.input({ id: "retries", name: "retries", type: "number", min: 0, max: 3, value: () => c.retries, oninput: (e) => (c.retries = e.target.value === "" ? 0 : Number(e.target.value)) }),
                    ),
                ),
                t.div(
                    { className: "col-lg-12", hidden: () => !c.retries || !["POST", "PATCH"].includes(c.method) },
                    t.div(
                        { className: "field" },
                        t.input({ id: "retry_unsafe", type: "checkbox", className: "switch", checked: () => !!c.retry_unsafe, onchange: (e) => (c.retry_unsafe = e.target.checked) }),
                        t.label({ htmlFor: "retry_unsafe" }, t.span({ className: "txt" }, `Also retry ${c.method} (only if the API ignores repeated requests)`)),
                    ),
                ),
            ),

            section("Outputs", "Named values of the response, used by the flows as steps.<key>.data.<name>. Click a field of the test response to add it."),
            outputsEditor(),
        ];
    }

    function section(title, hint) {
        return t.div({ className: "gp-api-section" }, t.h6(null, title), hint ? t.div({ className: "field-help" }, hint) : null);
    }

    function outputsEditor() {
        const c = data.call;
        return t.div(
            { className: "gp-kv" },
            () =>
                c.outputs.length
                    ? c.outputs.map((o) =>
                        t.div(
                            { rid: "out_" + o.id, className: "gp-api-out-row" },
                            bare(t.input({ type: "text", className: () => `txt-code ${o.name && !nameRegex.test(o.name) ? "gp-invalid" : ""}`, placeholder: "name", value: () => o.name, oninput: (e) => (o.name = e.target.value) })),
                            bare(t.input({ type: "text", className: "txt-code", placeholder: "path, e.g. items.0.id", value: () => o.path, oninput: (e) => (o.path = e.target.value) })),
                            t.code({ className: "gp-api-out-value", title: "Value in the last test" }, () => preview(data.test.result?.data?.[o.name])),
                            t.button(
                                { type: "button", className: "btn sm circle transparent danger", ariaLabel: app.attrs.tooltip("Remove"), onclick: () => (c.outputs = c.outputs.filter((x) => x.id != o.id)) },
                                t.i({ className: "ri-close-line", ariaHidden: true }),
                            ),
                        )
                    )
                    : t.div({ rid: "none", className: "txt-hint txt-sm" }, "No outputs: the flows can still use the whole response (body, status, headers)."),
            t.button(
                { type: "button", className: "btn sm secondary transparent", onclick: () => (c.outputs = [...c.outputs, { id: randomId(), name: "", path: "" }]) },
                t.i({ className: "ri-add-line", ariaHidden: true }),
                t.span({ className: "txt" }, "Add output"),
            ),
        );
    }

    function testPanel() {
        return [
            t.div(
                { className: "gp-api-test-head" },
                t.h6(null, "Test"),
                t.button(
                    { type: "button", className: () => `btn sm ${data.test.running ? "loading" : ""}`, disabled: () => data.test.running, onclick: runTest },
                    t.i({ className: "ri-play-line", ariaHidden: true }),
                    t.span({ className: "txt" }, "Test the call"),
                ),
            ),
            t.div(
                { className: "field-help m-b-sm" },
                () => (data.call.variables.length ? "Uses the default values of the variables and the call as edited (saving isn't needed)." : "Sends the call as edited (saving isn't needed)."),
            ),
            () => {
                const r = data.test.result;
                if (!r) {
                    return t.div({ rid: "nores", className: "gp-api-noresult" }, t.i({ className: "ri-send-plane-line", ariaHidden: true }), t.span(null, "Send a test to see the response and pick its fields."));
                }
                return t.div(
                    { rid: "res" + data.test.count },
                    t.div(
                        { className: "gp-api-status" },
                        r.status ? t.span({ className: `label ${r.ok ? "success" : "danger"}` }, String(r.status)) : t.span({ className: "label danger" }, "No response"),
                        r.format ? t.span({ className: "label sm" }, String(r.format).toUpperCase()) : null,
                        r.duration_ms != null ? t.span({ className: "txt-hint txt-sm" }, `${r.duration_ms} ms`) : null,
                        r.attempts > 1 ? t.span({ className: "txt-hint txt-sm" }, `${r.attempts} attempts`) : null,
                    ),
                    r.error ? t.div({ className: "alert danger m-b-sm" }, r.error) : null,
                    r.hint ? t.div({ className: "alert info m-b-sm" }, t.i({ className: "ri-lightbulb-line", ariaHidden: true }), " ", r.hint) : null,
                    t.div(
                        { className: "gp-tabs m-b-sm" },
                        ...[
                            ["fields", "Fields", "ri-node-tree"],
                            ["raw", "Raw", "ri-file-text-line"],
                            ["headers", "Headers", "ri-list-check"],
                            ["request", "Request", "ri-upload-2-line"],
                        ].map(([key, label, icon]) =>
                            t.button({ type: "button", className: () => `gp-tab ${data.test.tab == key ? "active" : ""}`, onclick: () => (data.test.tab = key) }, t.i({ className: icon, ariaHidden: true }), label)
                        ),
                    ),
                    t.div({ hidden: () => data.test.tab != "fields", className: "gp-api-tree" }, r.body == null || typeof r.body != "object" ? t.div({ className: "txt-hint txt-sm" }, r.format == "text" ? "A text response: use the raw text (steps.<key>.body)." : "No fields.") : tree(r.body, "")),
                    t.pre({ hidden: () => data.test.tab != "raw", className: "gp-api-pre" }, (r.raw || "") + (r.raw_truncated ? "\n…" : "")),
                    t.div({ hidden: () => data.test.tab != "headers" }, t.pre({ className: "gp-api-pre" }, Object.entries(r.headers || {}).map(([k, v]) => `${k}: ${v}`).join("\n"))),
                    t.div({ hidden: () => data.test.tab != "request" }, t.pre({ className: "gp-api-pre" }, JSON.stringify(r.request || {}, null, 2))),
                );
            },
        ];
    }

    // the parsed response as a tree: a click on a field adds it to the outputs
    function tree(value, path) {
        if (Array.isArray(value)) {
            return t.div(
                { className: "gp-api-tree-list" },
                ...value.map((item, i) => node(String(i), item, path ? `${path}.${i}` : String(i))),
                value.length ? null : t.div({ className: "txt-hint txt-sm" }, "empty list"),
            );
        }
        return t.div({ className: "gp-api-tree-list" }, ...Object.keys(value).sort().map((k) => node(k, value[k], path ? `${path}.${k}` : k)));
    }

    function node(key, value, path) {
        // a toggle: the field is an output (named value of the flows) or not
        const isOutput = () => data.call.outputs.some((o) => o.path == path);
        const add = t.button(
            {
                type: "button",
                className: () => `btn xs transparent gp-api-add ${isOutput() ? "active" : "secondary"}`,
                title: () => (isOutput() ? `Remove ${path} from the outputs` : `Add ${path} to the outputs`),
                onclick: (e) => {
                    e.preventDefault();
                    if (isOutput()) {
                        data.call.outputs = data.call.outputs.filter((o) => o.path != path);
                    } else {
                        addOutput(path);
                    }
                },
            },
            t.i({ className: () => (isOutput() ? "ri-checkbox-line" : "ri-checkbox-blank-line"), ariaHidden: true }),
        );
        if (value && typeof value == "object") {
            const count = Array.isArray(value) ? `[${value.length}]` : `{${Object.keys(value).length}}`;
            return t.details(
                { className: "gp-api-node", open: path.split(".").length < 3 },
                t.summary(null, t.span({ className: "gp-api-key" }, key), t.span({ className: "txt-hint txt-sm" }, count), add),
                tree(value, path),
            );
        }
        return t.div({ className: "gp-api-leaf" }, t.span({ className: "gp-api-key" }, key), t.code({ className: "gp-api-val", title: String(value) }, preview(value)), add);
    }
}

// the {{.vars.x}} buttons insert into the last focused template input
function varInserter() {
    let lastInput = null;
    return {
        track(e) {
            if (e.target.matches?.(".gp-api-tpl")) {
                lastInput = e.target;
            }
        },
        insert(name) {
            const text = `{{.vars.${name}}}`;
            const el = lastInput && document.body.contains(lastInput) ? lastInput : null;
            if (!el) {
                navigator.clipboard?.writeText(text).catch(() => {});
                app.toasts.info(`Copied ${text}: click a field first to insert it there.`);
                return;
            }
            const start = el.selectionStart ?? el.value.length;
            el.setRangeText(text, start, el.selectionEnd ?? start, "end");
            el.dispatchEvent(new Event("input", { bubbles: true }));
            el.focus();
        },
    };
}

/**
 * Variables (name, default value, required) of an API or a call:
 * obj.variables. used() → the names its templates use (the others are shown
 * as unused).
 */
function variablesEditor(obj, insertVar, emptyText, taken = () => [], used = null) {
    return t.div(
        { className: "gp-kv" },
        () =>
            obj.variables.length
                ? [
                    t.div({ rid: "vhead", className: "gp-api-var-row gp-api-head" }, t.span(null, "Name"), t.span({ title: "Used by the Test button, and when the flow gives no value" }, "Default value"), t.span(null, "Required"), t.span()),
                    ...obj.variables.map((v) =>
                        t.div(
                            { rid: "var_" + v.id, className: () => `gp-api-var-row ${used && v.name && namesReady() && !used().has(v.name) ? "unused" : ""}`, title: () => (used && v.name && namesReady() && !used().has(v.name) ? "Not used in the call: remove it, or insert it with the { } button" : "") },
                            bare(
                                t.input({
                                    type: "text",
                                    className: () => `txt-code ${v.name && (!nameRegex.test(v.name) || taken().includes(v.name)) ? "gp-invalid" : ""}`,
                                    title: () => (taken().includes(v.name) ? "The API already has this variable" : ""),
                                    placeholder: "name",
                                    value: () => v.name,
                                    oninput: (e) => (v.name = e.target.value),
                                }),
                            ),
                            bare(t.input({ type: "text", placeholder: "e.g. pikachu", value: () => v.default, oninput: (e) => (v.default = e.target.value) })),
                            t.div({ className: "field gp-checkbox" }, t.input({ type: "checkbox", id: "req_" + v.id, checked: () => !!v.required, onchange: (e) => (v.required = e.target.checked) }), t.label({ htmlFor: "req_" + v.id })),
                            t.div(
                                { className: "gp-api-var-actions" },
                                t.button(
                                    { type: "button", className: "btn xs secondary transparent", disabled: () => !nameRegex.test(v.name), title: "Insert in the last field", onmousedown: (e) => e.preventDefault(), onclick: () => insertVar(v.name) },
                                    t.i({ className: "ri-braces-line", ariaHidden: true }),
                                ),
                                t.button(
                                    { type: "button", className: "btn sm circle transparent danger", ariaLabel: app.attrs.tooltip("Remove"), onclick: () => (obj.variables = obj.variables.filter((x) => x.id != v.id)) },
                                    t.i({ className: "ri-close-line", ariaHidden: true }),
                                ),
                            ),
                        )
                    ),
                ]
                : t.div({ rid: "none", className: "txt-hint txt-sm" }, emptyText),
        t.button(
            {
                type: "button",
                className: "btn sm secondary transparent",
                onclick: () => {
                    let n = obj.variables.length + 1;
                    while (obj.variables.some((v) => v.name == "var" + n) || taken().includes("var" + n)) {
                        n++;
                    }
                    obj.variables = [...obj.variables, { id: randomId(), name: "var" + n, default: "", required: false }];
                },
            },
            t.i({ className: "ri-add-line", ariaHidden: true }),
            t.span({ className: "txt" }, "Add variable"),
        ),
    );
}

// the "test" values of the calls saved before the defaults become their default
const toVariables = (list) => (list || []).map((v) => ({ id: randomId(), name: v.name, default: v.default ?? v.test ?? "", required: !!v.required }));
const fromVariables = (list) => list.filter((v) => v.name.trim()).map((v) => ({ name: v.name.trim(), default: v.default || "", required: !!v.required }));

/**
 * Adds the variables a template uses to obj.variables as they're typed
 * ({{customer_id}} anywhere: a new row), except the names of other() (the
 * API's). A variable of the path is required. Returns the watcher.
 */
function autoVariables(obj, sources, other = () => [], pathSources = () => []) {
    let timer;
    return watch(() => JSON.stringify(sources()), () => {
        clearTimeout(timer);
        timer = setTimeout(() => {
            if (!namesReady()) {
                return;
            }
            const used = new Set();
            sources().forEach((src) => templateNames(src).forEach((n) => used.add(n)));
            const inPath = new Set();
            pathSources().forEach((src) => templateNames(src).forEach((n) => inPath.add(n)));
            const have = new Set([...obj.variables.map((v) => v.name), ...other()]);
            const added = [...used].filter((n) => !have.has(n) && /^[A-Za-z_][A-Za-z0-9_]*$/.test(n));
            if (added.length) {
                obj.variables = [...obj.variables, ...added.map((n) => ({ id: randomId(), name: n, default: "", required: inPath.has(n) }))];
            }
        }, 600);
    });
}

// shortens a response like the server does for the samples
function prune(v, maxItems, maxText) {
    if (Array.isArray(v)) {
        return v.slice(0, maxItems).map((x) => prune(x, maxItems, maxText));
    }
    if (v && typeof v == "object") {
        return Object.fromEntries(Object.entries(v).map(([k, x]) => [k, prune(x, maxItems, maxText)]));
    }
    return typeof v == "string" && v.length > maxText ? v.slice(0, maxText) + "…" : v;
}

function preview(v) {
    if (v === undefined) {
        return "";
    }
    const s = typeof v == "string" ? JSON.stringify(v) : JSON.stringify(v) ?? String(v);
    return s.length > 80 ? s.slice(0, 80) + "…" : s;
}
