import { allCollections, api, field, formatDate, handleError, multiSelect, page, recordLabel, select, userCollections } from "./lib.js";
import { describeTrigger, flowsSidebar, loadFlows, triggerTypes } from "./flows.js";
import { aiModels, loadAI, modelTypes, openModelModal } from "./aiSettings.js";
import { actionAt, functionList, functionSnippet, insertAt } from "./functions.js";
import { closest, dataList, lookup, previewLine, previewQueue, valueSummary } from "./bindings.js";
import { loadSecrets, looksSecret, openSecretModal, secretName, secretPicker, secretRef, secretsList } from "./secrets.js";

// the data names usable without their dot ({{steps.api.body}}); "now", "last"
// and "index" are also function names: they keep it (internal/flows/expr.go)
const shortRoots = new Set(["trigger", "steps", "item", "flow", "secret"]);

// --- step catalog -----------------------------------------------------------------

const stepTypes = {
    condition: { label: "Condition", icon: "ri-git-merge-line", group: "Logic", key: "condition", hint: "Run different steps depending on rules" },
    loop: { label: "For each", icon: "ri-repeat-line", group: "Logic", key: "loop", hint: "Repeat steps for every item of a list" },
    delay: { label: "Delay", icon: "ri-timer-line", group: "Logic", key: "delay", hint: "Wait up to 10 minutes" },
    stop: { label: "Stop / reject", icon: "ri-stop-circle-line", group: "Logic", key: "stop", hint: "End the flow, or reject the record change" },
    run_flow: { label: "Run another flow", icon: "ri-git-branch-line", group: "Logic", key: "subflow", hint: "Call a flow and use its result" },
    get_records: { label: "Get records", icon: "ri-search-line", group: "Records", key: "records", hint: "Query records with a filter" },
    create_record: { label: "Create record", icon: "ri-add-box-line", group: "Records", key: "created", hint: "Create a record in any collection" },
    update_record: { label: "Update record", icon: "ri-edit-box-line", group: "Records", key: "updated", hint: "Change fields of a record" },
    delete_record: { label: "Delete record", icon: "ri-delete-bin-7-line", group: "Records", key: "deleted", hint: "Delete a record" },
    set_trigger: { label: "Update triggering record", icon: "ri-pencil-ruler-2-line", group: "Records", key: "trigger_update", hint: "Change the record before it is saved (before-save flows)" },
    send_email: { label: "Send email", icon: "ri-mail-send-line", group: "Actions", key: "email", hint: "Send one of your email templates" },
    api_call: { label: "API call", icon: "ri-plug-line", group: "Actions", key: "api", hint: "Run a saved call of one of your APIs (Flows → APIs)" },
    http_request: { label: "HTTP request", icon: "ri-global-line", group: "Actions", key: "http", hint: "Call any address (one-off, typed in the step)" },
    notify: { label: "Send notification", icon: "ri-notification-3-line", group: "Actions", key: "notification", hint: "Notify superusers in the dashboard (bell)" },
    log: { label: "Log", icon: "ri-file-list-3-line", group: "Actions", key: "log", hint: "Write a message in the run log" },
    ai_decision: { label: "AI decision", icon: "ri-sparkling-2-line", group: "AI", key: "ai", hint: "Yes/no, choice or score questions answered by an AI model" },
    ai_generate: { label: "AI generate", icon: "ri-quill-pen-line", group: "AI", key: "write", hint: "A standard model writes a text, or named fields" },
    sign_in: { label: "Sign in", icon: "ri-login-box-line", group: "Pages", key: "login", hint: "Check an email and password and sign the visitor in" },
    sign_out: { label: "Sign out", icon: "ri-logout-box-r-line", group: "Pages", key: "logout", hint: "Sign the visitor out" },
    redirect: { label: "Redirect to page", icon: "ri-share-forward-line", group: "Pages", key: "redirect", hint: "Open a page next, with a message" },
    respond: { label: "Respond", icon: "ri-reply-line", group: "Webhook", key: "response", hint: "The response of the webhook: status, JSON, text, HTML or a redirect" },
    set_variables: { label: "Set variables", icon: "ri-braces-line", group: "Advanced", key: "vars", hint: "Compute values to reuse in later steps" },
    script: { label: "Run script", icon: "ri-javascript-line", group: "Advanced", key: "script", hint: "Sandboxed JavaScript returning a value" },
};

function defaultConfig(type) {
    switch (type) {
        case "condition":
            return { match: "all", rules: [{ id: randomId(), left: "", op: "eq", right: "" }] };
        case "loop":
            return { items: "", max: 1000 };
        case "delay":
            return { seconds: 10 };
        case "stop":
            return { outcome: "success", message: "" };
        case "run_flow":
            return { flow: "", payload: {} };
        case "get_records":
            return { collection: "", filter: "", sort: "", limit: 50 };
        case "create_record":
        case "set_trigger":
            return { collection: "", data: {} };
        case "update_record":
            return { collection: "", recordId: "", data: {} };
        case "delete_record":
            return { collection: "", recordId: "" };
        case "send_email":
            return { template: "", to: "", variables: {}, delayMinutes: 0 };
        case "http_request":
            return { method: "POST", url: "", query: {}, headers: {}, body: "", responseFormat: "auto", retries: 0, failOnError: true, timeoutSeconds: 15 };
        case "api_call":
            return { call: "", vars: {}, failOnError: true };
        case "log":
            return { message: "" };
        case "notify":
            return { to: "all", subject: "", message: "", link: "" };
        case "set_variables":
            return { vars: { value: "" } };
        case "sign_in":
            return { collection: "users", identity: "{{.trigger.form.email}}", password: "{{.trigger.form.password}}" };
        case "redirect":
            return { url: "", message: "" };
        case "respond":
            return { format: "json", status: "200", headers: {}, body: '{"ok": true}', redirect: "" };
        case "script":
            return { code: "// data.trigger, data.steps, data.last, data.item...\nreturn { ok: true };" };
        case "ai_decision":
            return {
                model: aiModels()[0]?.id || "",
                state: { mode: "values", values: {} },
                maxChars: 2000,
                explain: false,
                branches: false,
                timeoutSeconds: 30,
                questions: [newAIQuestion("noul", "answer")],
            };
        case "ai_generate":
            return {
                model: aiModels().find((m) => m.type == "standard")?.id || "",
                system: "",
                prompt: "",
                output: "text",
                fields: [{ id: randomId(), name: "subject", type: "text", description: "" }],
                timeoutSeconds: 60,
            };
    }
    return {};
}

const aiQuestionTypes = {
    noul: { label: "Yes / No", icon: "ri-checkbox-circle-line", hint: "Probability that the answer is yes" },
    choice: { label: "Choice", icon: "ri-list-radio", hint: "Pick one option of a list" },
    score: { label: "Score", icon: "ri-bar-chart-horizontal-line", hint: "Rate on ordered levels" },
};

function newAIQuestion(type, key) {
    return {
        id: randomId(),
        key,
        type,
        question: "",
        minConfidence: 0.5,
        yes: "",
        no: "",
        threshold: 0.5,
        options: { source: "static", items: [{ key: "", description: "" }, { key: "", description: "" }] },
        levels: { source: "static", items: ["Low", "Medium", "High"] },
    };
}

const operators = [
    { value: "eq", label: "is equal to" },
    { value: "neq", label: "is not equal to" },
    { value: "contains", label: "contains" },
    { value: "not_contains", label: "does not contain" },
    { value: "starts", label: "starts with" },
    { value: "ends", label: "ends with" },
    { value: "gt", label: "is greater than" },
    { value: "gte", label: "is greater or equal to" },
    { value: "lt", label: "is less than" },
    { value: "lte", label: "is less or equal to" },
    { value: "empty", label: "is empty" },
    { value: "not_empty", label: "is not empty" },
];

const cronPresets = [
    { value: "*/5 * * * *", label: "Every 5 minutes" },
    { value: "0 * * * *", label: "Every hour" },
    { value: "0 9 * * *", label: "Every day at 9:00" },
    { value: "0 9 * * 1", label: "Every Monday at 9:00" },
    { value: "0 9 1 * *", label: "First day of the month" },
];

const runStatusClass = { success: "success", failed: "danger", rejected: "warning", running: "info" };

function randomId() {
    return app.utils.randomString(12);
}

function walkSteps(list, fn) {
    for (const s of list || []) {
        fn(s);
        walkSteps(s.then, fn);
        walkSteps(s.else, fn);
        walkSteps(s.steps, fn);
    }
}

// pages of the site (Redirect step) and roles (Page action access): optional
const sitePages = store({ list: [] });
// address of a page (as the Pages module serves it: the first path segments of the app stay under /p/)
const sitePath = (p) => (p.home ? "/" : (["api", "p", "f", "dashboards", "email"].includes(p.slug) ? "/p/" : "/") + p.slug);
// a redirect saved before the pages moved to the root (/p/slug)
const currentPath = (url) => String(url || "").replace(/^\/p\/(?!_)/, "/");
const roleList = store({ list: [] });

function loadPagesAndRoles() {
    app.pb.collection("pages").getFullList({ filter: "kind != 'part'", sort: "title", fields: "id,title,slug,home", requestKey: null })
        .then((list) => (sitePages.list = list))
        .catch(() => (sitePages.list = [])); // no Pages module
    app.pb.collection("roles").getFullList({ sort: "name", fields: "id,name", requestKey: null })
        .then((list) => (roleList.list = list))
        .catch(handleError);
}

// =======================================================================================

export function pageFlowEditor(route) {
    const id = route.params.id;
    const isNew = id == "new";
    const showRuns = route.path?.endsWith("/runs") || window.location.hash.endsWith("/runs");

    app.store.title = isNew ? "New flow" : "Flow";

    const data = store({
        isLoading: !isNew,
        isSaving: false,
        tab: showRuns ? "runs" : "editor",
        flow: {
            name: "",
            description: "",
            active: true,
            trigger_type: "record",
            trigger: { collection: "", events: ["create"], timing: "after", filter: "" },
            steps: [],
        },
        savedId: isNew ? "" : id,
        savedSlug: "", // the webhook address once saved
        initSerialized: "",
        collections: [],
        allCollections: [],
        templates: [],
        otherFlows: [],
        templateVars: {}, // templateId -> [paths]
        apiCalls: [], // saved API calls (APIs module)
        sample: null, // test data of the last test run: {data: {trigger, steps, loops}, counts, created, status}
        sampleVersion: 0,
        addMenu: "", // list position with an open "add step" menu
        collapsed: {},
        runs: [],
        aiv: {}, // AI step lists render versions (bumped on add/remove/move)
        get serialized() {
            // empty branches are left out: the server drops them when saving
            // and the editor adds them back (the flow must not look changed)
            return JSON.stringify(data.flow, (k, v) => ((k == "then" || k == "else" || k == "steps") && Array.isArray(v) && !v.length ? undefined : v));
        },
        get hasChanges() {
            return data.serialized != data.initSerialized;
        },
    });

    // --- loading ---------------------------------------------------------------------

    // the saved API calls (optional APIs module): variables, outputs and the
    // fields of their last test response
    async function loadAPICalls() {
        if (!globalThis.gpAPIs) {
            return;
        }
        try {
            data.apiCalls = await api("/api/apisources/calls", { requestKey: null });
        } catch (err) {
            handleError(err);
        }
    }

    // the collections, fetched again when coming back to the tab (a collection
    // created meanwhile in the native screens or another tab)
    async function reloadCollections(refresh = true) {
        try {
            const all = await allCollections(refresh);
            data.allCollections = all;
            data.collections = await userCollections();
        } catch (err) {
            handleError(err);
        }
    }

    function onVisible() {
        if (document.visibilityState == "visible") {
            reloadCollections();
        }
    }

    async function load() {
        try {
            loadAI(); // shared AI models list (model picker of the AI steps)
            loadSecrets(); // the secrets of the HTTP requests
            loadPagesAndRoles();
            loadAPICalls();
            const [cols, all, templates, flows] = await Promise.all([
                allCollections(true).then(() => userCollections()),
                allCollections(),
                app.pb.collection("email_templates").getFullList({ sort: "folder,name", fields: "id,name,folder", filter: "account_email = ''", requestKey: null }),
                app.pb.collection("flows").getFullList({ sort: "name", fields: "id,name,trigger_type", requestKey: null }),
            ]);
            data.collections = cols;
            data.allCollections = all;
            data.templates = templates;
            data.otherFlows = flows.filter((f) => f.id != id);

            if (!isNew) {
                const f = await app.pb.collection("flows").getOne(id, { requestKey: null });
                Object.assign(data.flow, {
                    name: f.name,
                    description: f.description,
                    active: f.active,
                    trigger_type: f.trigger_type,
                    trigger: f.trigger || {},
                    steps: f.steps || [],
                });
                data.savedSlug = f.trigger?.slug || "";
                app.store.title = f.name;
                walkSteps(data.flow.steps, (s) => {
                    if (s.type == "send_email" && s.config?.template) {
                        loadTemplateVars(s.config.template);
                    }
                    // stable render ids for the condition rules and AI questions
                    for (const rule of s.config?.rules || []) {
                        rule.id ||= randomId();
                    }
                    for (const q of s.config?.questions || []) {
                        q.id ||= randomId();
                    }
                });
            }
            data.initSerialized = data.serialized;
            data.isLoading = false;
            loadSample();
            if (data.tab == "runs") {
                loadRuns();
            }
        } catch (err) {
            handleError(err);
        }
    }

    // the test data of the last run started from the editor (values of the picker and the previews)
    async function loadSample() {
        if (!data.savedId) {
            return;
        }
        try {
            const s = await api("/api/flows/sample/" + data.savedId, { requestKey: null });
            data.sample = s?.data ? s : null;
            data.sampleVersion++;
        } catch (err) {
            handleError(err);
        }
    }

    async function loadTemplateVars(templateId) {
        if (!templateId || data.templateVars[templateId]) {
            return;
        }
        try {
            const res = await api("/api/flows/template-variables/" + templateId, { requestKey: null });
            data.templateVars[templateId] = res.variables;
        } catch (err) {
            handleError(err);
        }
    }

    async function save() {
        if (data.isSaving) {
            return;
        }
        data.isSaving = true;
        try {
            const body = JSON.parse(data.serialized);
            const saved = data.savedId
                ? await app.pb.collection("flows").update(data.savedId, body)
                : await app.pb.collection("flows").create(body);
            // keep server-side normalization (step ids, webhook token...)
            Object.assign(data.flow, { trigger: saved.trigger || {}, steps: saved.steps || [] });
            data.savedSlug = saved.trigger?.slug || "";
            data.initSerialized = data.serialized;
            app.store.title = saved.name;
            loadFlows();
            app.toasts.success("Successfully saved the flow.");
            if (!data.savedId) {
                data.savedId = saved.id;
                window.location.replace("#/flows/" + saved.id);
            }
        } catch (err) {
            handleError(err);
        }
        data.isSaving = false;
    }

    function remove() {
        app.modals.confirm(`Do you really want to delete the flow "${data.flow.name}" and its run history?`, async () => {
            try {
                await app.pb.collection("flows").delete(data.savedId);
                loadFlows();
                app.toasts.success("Successfully deleted the flow.");
                window.location.hash = "#/flows";
            } catch (err) {
                handleError(err);
            }
        });
    }

    // --- variables (dynamic content) -------------------------------------------------

    function fieldsOf(collectionName) {
        const c = data.allCollections.find((c) => c.name == collectionName || c.id == collectionName);
        return c ? c.fields.filter((f) => f.type != "password" && f.name != "tokenKey").map((f) => f.name) : [];
    }

    function recordItems(prefix, collectionName, labelPrefix = "") {
        return fieldsOf(collectionName).map((name) => ({ path: `${prefix}.${name}`, label: labelPrefix + name }));
    }

    function triggerGroup() {
        const f = data.flow;
        const cfg = f.trigger || {};
        const items = [];
        switch (f.trigger_type) {
            case "record":
                items.push({ path: "trigger.event", label: "event (create/update/delete)" });
                items.push(...recordItems("trigger.record", cfg.collection, "record."));
                if ((cfg.events || []).includes("update")) {
                    items.push(...recordItems("trigger.original", cfg.collection, "previous value: "));
                }
                break;
            case "manual":
                if (cfg.collection) {
                    items.push(...recordItems("trigger.record", cfg.collection, "record."));
                }
                items.push({ path: "trigger.user", label: "superuser email" });
                items.push({ path: "trigger.payload", label: "payload" });
                break;
            case "webhook":
                if (cfg.access && cfg.access != "token") {
                    items.push({ path: "trigger.user.id", label: "signed-in caller id (empty for guests)" });
                    items.push({ path: "trigger.user.email", label: "signed-in caller email" });
                    items.push({ path: "trigger.user.roles", label: "signed-in caller roles" });
                }
                items.push({ path: "trigger.body", label: "request body (JSON or form fields)" });
                items.push({ path: "trigger.query", label: "query parameters" });
                items.push({ path: "trigger.headers", label: "headers" });
                items.push({ path: "trigger.method", label: "HTTP method" });
                break;
            case "schedule":
                items.push({ path: "trigger.time", label: "run time" });
                break;
            case "flow":
                items.push({ path: "trigger.payload", label: "payload from the calling flow" });
                break;
            case "action":
                items.push({ path: "trigger.form", label: "form fields (trigger.form.<name>)" });
                items.push({ path: "trigger.user.id", label: "signed-in user id (empty for guests)" });
                items.push({ path: "trigger.user.email", label: "signed-in user email" });
                items.push({ path: "trigger.user.roles", label: "signed-in user roles" });
                items.push({ path: "trigger.record", label: "record of the button (lists, single item pages)" });
                items.push({ path: "trigger.collection", label: "collection of that record" });
                items.push({ path: "trigger.page", label: "page of the button" });
                items.push({ path: "trigger.next", label: "page to go back to (?next)" });
                break;
        }
        return { name: "Trigger", icon: triggerTypes[f.trigger_type]?.icon, items };
    }

    function stepOutputs(s) {
        const base = `steps.${s.key}`;
        const c = s.config || {};
        switch (s.type) {
            case "get_records":
                return [
                    { path: base + ".items", label: "items (list)" },
                    { path: base + ".count", label: "count" },
                    ...recordItems(base + ".first", c.collection, "first."),
                ];
            case "create_record":
            case "update_record":
            case "delete_record":
                return recordItems(base, c.collection);
            case "set_trigger":
                return recordItems(base, data.flow.trigger?.collection);
            case "send_email":
                return [{ path: base + ".queued", label: "queued count" }];
            case "http_request":
                return [
                    { path: base + ".status", label: "status code" },
                    { path: base + ".body", label: "response body (parsed JSON / XML, or text)" },
                    { path: base + ".ok", label: "ok (true/false)" },
                    { path: base + ".raw", label: "raw response text" },
                    { path: base + ".format", label: "format (json, xml, text)" },
                    { path: base + ".headers", label: "response headers" },
                ];
            case "api_call": {
                const call = data.apiCalls.find((x) => x.id == c.call);
                return [
                    ...(call?.outputs || []).map((o) => ({ path: `${base}.data.${o.name}`, label: `${o.name} (${o.path})` })),
                    ...(call?.fields || []).map((f) => ({ path: `${base}.body.${f}`, label: f })),
                    { path: base + ".ok", label: "ok (true/false)" },
                    { path: base + ".status", label: "status code" },
                    { path: base + ".body", label: "whole response (parsed)" },
                    { path: base + ".raw", label: "raw response text" },
                    { path: base + ".headers", label: "response headers" },
                ];
            }
            case "set_variables":
                return Object.keys(c.vars || {}).map((k) => ({ path: `${base}.${k}`, label: k }));
            case "script":
                return [{ path: base, label: "returned value" }];
            case "condition":
                return [{ path: base + ".result", label: "result (true/false)" }];
            case "loop":
                return [{ path: base + ".count", label: "iterations" }];
            case "run_flow":
                return [{ path: base + ".output", label: "flow output" }, { path: base + ".status", label: "status" }];
            case "log":
                return [{ path: base + ".message", label: "message" }];
            case "sign_in":
                return [{ path: base + ".user.id", label: "user id" }, ...recordItems(base + ".user", c.collection || "users", "user.")];
            case "redirect":
                return [{ path: base + ".redirect", label: "page opened" }];
            case "notify":
                return [{ path: base + ".notified", label: "notified superusers" }];
            case "ai_decision":
                return aiOutputs(base, c);
            case "ai_generate":
                return [
                    ...(c.output == "fields"
                        ? (c.fields || []).filter((f) => f.name).map((f) => ({ path: `${base}.${f.name}`, label: `${f.name} (${f.type || "text"})` }))
                        : [{ path: base + ".text", label: "generated text" }]),
                    { path: base + ".model", label: "model that answered" },
                ];
        }
        return [];
    }

    function aiOutputs(base, c) {
        const items = [];
        for (const q of c.questions || []) {
            const p = `${base}.${q.key}`;
            const name = q.key;
            switch (q.type) {
                case "noul":
                    items.push(
                        { path: p + ".yes", label: `${name} → yes (true/false)` },
                        { path: p + ".value", label: `${name} → probability of yes (0-1)` },
                    );
                    break;
                case "choice":
                    items.push(
                        { path: p + ".choice", label: `${name} → chosen option${q.options?.source == "records" ? " (record id)" : ""}` },
                        { path: p + ".label", label: `${name} → chosen option name` },
                        { path: p + ".probabilities", label: `${name} → probability of each option` },
                    );
                    break;
                case "score":
                    items.push(
                        { path: p + ".label", label: `${name} → level name` },
                        { path: p + ".level", label: `${name} → level index (0 = lowest)` },
                        { path: p + ".score", label: `${name} → score (e.g. 1.4)` },
                    );
                    break;
            }
            items.push(
                { path: p + ".confidence", label: `${name} → confidence (0-1)` },
                { path: p + ".confident", label: `${name} → confident (true/false)` },
            );
            if (c.explain) {
                items.push({ path: p + ".reason", label: `${name} → reason (standard models)` });
            }
        }
        items.push(
            { path: base + ".confident", label: "all answers confident (true/false)" },
            { path: base + ".model", label: "model that answered" },
        );
        return items;
    }

    // steps that run before stepId and the loops containing it
    function scopeOf(stepId) {
        const prev = [];
        const loops = [];
        function walk(list, ancestors) {
            for (const s of list || []) {
                if (s.id == stepId) {
                    loops.push(...ancestors.filter((a) => a.type == "loop"));
                    // the branches of an AI step use its answers
                    prev.push(...ancestors.filter((a) => a.type == "ai_decision"));
                    return true;
                }
                for (const sub of [s.then, s.else, s.steps]) {
                    if (walk(sub, [...ancestors, s])) {
                        return true;
                    }
                }
                prev.push(s);
            }
            return false;
        }
        walk(data.flow.steps, []);
        return { prev, loops };
    }

    function loopItemGroup(loop) {
        const items = [{ path: "item", label: "current item" }, { path: "index", label: "index (0, 1, ...)" }];
        const m = /^\{\{\s*\.?steps\.([A-Za-z0-9_]+)\.items\s*\}\}$/.exec((loop.config?.items || "").trim());
        let source;
        if (m) {
            walkSteps(data.flow.steps, (s) => s.key == m[1] && (source = s));
        }
        if (source?.type == "get_records") {
            items.push(...recordItems("item", source.config?.collection, "item."));
        } else {
            // the fields of the first item of the list in the test data
            const first = loopSample(loop)?.[0];
            if (first && typeof first == "object" && !Array.isArray(first)) {
                items.push(...Object.keys(first).sort().map((k) => ({ path: "item." + k, label: "item." + k })));
            }
        }
        return { name: `For each: ${loop.name || loop.key}`, icon: "ri-repeat-line", items };
    }

    // --- test data -------------------------------------------------------------------------

    // the saved test response of the API call steps (before any test run)
    function apiSample(step) {
        const call = step?.type == "api_call" ? data.apiCalls.find((x) => x.id == step.config?.call) : null;
        if (!call || call.sample === undefined || call.sample === null) {
            return undefined;
        }
        const out = { ok: true, status: 200, body: call.sample, data: {} };
        for (const o of call.outputs || []) {
            out.data[o.name] = lookup(call.sample, o.path);
        }
        return out;
    }

    function stepByKey(key) {
        let found;
        walkSteps(data.flow.steps, (s) => s.key == key && (found = s));
        return found;
    }

    // the list of a loop in the test data (its first items)
    function loopSample(loop) {
        const fromRun = data.sample?.data?.loops?.[loop.key];
        if (fromRun?.length) {
            return fromRun;
        }
        // not run yet: the list its expression gives in the test data
        const m = /^\{\{\s*\.?([A-Za-z0-9_.]+)\s*\}\}$/.exec((loop.config?.items || "").trim());
        const v = m ? sampleValue(m[1], []) : undefined;
        return Array.isArray(v) ? v : undefined;
    }

    // the value of a path in the test data, seen from a step inside the given loops
    function sampleValue(path, loops) {
        const [root, ...rest] = path.split(".");
        let base;
        switch (root) {
            case "trigger":
                base = data.sample?.data?.trigger;
                break;
            case "steps": {
                const key = rest.shift();
                base = data.sample?.data?.steps?.[key];
                if (base === undefined) {
                    base = apiSample(stepByKey(key));
                }
                break;
            }
            case "item": {
                const loop = loops[loops.length - 1];
                base = loop ? loopSample(loop)?.[0] : undefined;
                break;
            }
            case "index":
                return loops.length ? 0 : undefined;
            default:
                return undefined;
        }
        return rest.length ? lookup(base, rest.join(".")) : base;
    }

    // the real length of a shortened list of the test data
    function sampleCount(path, loops) {
        const counts = data.sample?.counts || {};
        if (path.startsWith("item")) {
            const loop = loops[loops.length - 1];
            return loop ? counts[`loops.${loop.key}.0${path.slice(4)}`] : undefined;
        }
        return counts[path];
    }

    function sampleOf(stepId) {
        const loops = scopeOf(stepId).loops;
        return (path) => {
            data.sampleVersion; // refresh with the test data
            const value = sampleValue(path, loops);
            return value === undefined ? {} : { value, count: sampleCount(path, loops) };
        };
    }

    // the test responses of the API steps the last run didn't reach (previews)
    function extraSteps() {
        const out = {};
        walkSteps(data.flow.steps, (s) => {
            if (s.type == "api_call" && data.sample?.data?.steps?.[s.key] === undefined) {
                const v = apiSample(s);
                if (v !== undefined) {
                    out[s.key] = v;
                }
            }
        });
        return out;
    }

    const previews = previewQueue((items) =>
        api("/api/flows/preview", { method: "POST", body: { flow: data.savedId, steps: extraSteps(), items }, requestKey: null }));

    // problems found without the server: a step that doesn't exist (or runs
    // later), "item" outside a For each, a missing }}
    function checkExpr(src, stepId) {
        const opens = (src.match(/\{\{/g) || []).length;
        const closes = (src.match(/\}\}/g) || []).length;
        if (opens > closes) {
            return "Missing }} to close a {{";
        }
        const scope = scopeOf(stepId);
        const before = new Set(scope.prev.map((s) => s.key));
        const all = allKeys();
        for (const action of src.match(/\{\{[\s\S]*?\}\}/g) || []) {
            const code = action.replace(/"(?:[^"\\]|\\.)*"|`[^`]*`/g, '""');
            for (const m of code.matchAll(/(?:^|[^\w.$])\.?steps\.([A-Za-z0-9_]+)/g)) {
                const key = m[1];
                if (before.has(key)) {
                    continue;
                }
                if (all.has(key)) {
                    return `steps.${key} runs after this step: its output isn't available here`;
                }
                const near = closest(key, [...before]);
                return `No step "${key}" before this one` + (near ? ` (did you mean steps.${near}?)` : "");
            }
            if (!scope.loops.length && /(?:^|[^\w.$])\.?item\b/.test(code)) {
                return `"item" is only available inside a For each`;
            }
            for (const m of code.matchAll(/(?:^|[^\w.$])(?:\$?\.)?secret\.([A-Za-z0-9_]+)/g)) {
                if (findStep(stepId)?.step.type != "http_request") {
                    return "Secrets can only be used in HTTP requests (and the saved APIs)";
                }
                const known = secretsList();
                if (known.length && !known.some((s) => s.name == m[1])) {
                    return `No secret ${m[1]}: add it in Settings → Secrets`;
                }
            }
        }
        return "";
    }

    // "For each" from the picker: a loop over the list is added before the
    // step, which moves inside it (its inputs can then use the item)
    function loopOver(path, stepId) {
        const found = findStep(stepId);
        if (!found) {
            app.toasts.info("Add a For each step, then pick this list as its items.");
            return;
        }
        const keys = allKeys();
        let key = "each";
        for (let n = 2; keys.has(key); n++) {
            key = "each" + n;
        }
        const loop = { id: randomId(), key, name: "For each", type: "loop", config: { ...defaultConfig("loop"), items: templateRef(path) }, steps: [] };
        const { list, index, step } = found;
        list.splice(index, 1, loop);
        loop.steps.push(step);
        app.toasts.success(`Added a For each over ${path}: pick the item fields in "For each".`);
    }

    // the list holding a step and its position
    function findStep(stepId) {
        let out = null;
        const walk = (list) => {
            for (let i = 0; i < (list || []).length && !out; i++) {
                const s = list[i];
                if (s.id == stepId) {
                    out = { list, index: i, step: s };
                    return;
                }
                walk(s.then);
                walk(s.else);
                walk(s.steps);
            }
        };
        walk(data.flow.steps);
        return out;
    }

    function variableGroups(stepId) {
        const scope = scopeOf(stepId);
        const groups = [triggerGroup()];
        for (const loop of scope.loops.reverse()) {
            groups.push(loopItemGroup(loop));
        }
        for (const s of scope.prev) {
            const items = stepOutputs(s);
            if (items.length) {
                groups.push({ name: s.name || stepTypes[s.type]?.label || s.key, icon: stepTypes[s.type]?.icon, items });
            }
        }
        // the secrets: only sent by the HTTP requests, never flow data
        if (findStep(stepId)?.step.type == "http_request") {
            const list = secretsList();
            groups.push({
                name: "Secrets",
                icon: "ri-key-2-line",
                items: list.length
                    ? list.map((s) => ({ path: "secret." + s.name, label: s.description ? `${s.name} · ${s.description}` : s.name }))
                    : [{ path: "secret.NAME", label: "no secrets yet: add them in Settings → Secrets" }],
            });
        }
        groups.push({
            name: "Other",
            icon: "ri-more-line",
            items: [
                { path: "last", label: "output of the previous step" },
                { path: "now", label: "current date/time (UTC)" },
                { path: "flow.name", label: "flow name" },
            ],
        });
        return groups;
    }

    // --- dynamic content input (Power Automate like) --------------------------------------

    function pickerPanel(stepId, insert, state, insertFn) {
        const groups = variableGroups(stepId);
        const tab = (key, label, icon) =>
            t.button(
                {
                    type: "button",
                    className: () => `gp-tab-btn ${(state.tab || "data") == key ? "active" : ""}`,
                    onmousedown: (e) => e.preventDefault(),
                    onclick: () => (state.tab = key),
                },
                t.i({ className: icon, ariaHidden: true }),
                " " + label,
            );
        return t.div(
            {
                rid: "picker",
                className: "gp-picker",
                onmount: (el) => {
                    el._outside = (e) => {
                        if (!el.parentElement?.contains(e.target)) {
                            state.open = false;
                        }
                    };
                    setTimeout(() => document.addEventListener("mousedown", el._outside), 0);
                },
                onunmount: (el) => document.removeEventListener("mousedown", el._outside),
            },
            t.div({ className: "gp-picker-tabs" }, tab("data", "Data", "ri-database-2-line"), tab("functions", "Functions", "ri-functions")),
            t.div(
                { className: "field gp-bare" },
                t.input({
                    type: "search",
                    placeholder: () => (state.tab == "functions" ? "Search functions..." : "Search dynamic content..."),
                    value: () => state.search,
                    oninput: (e) => (state.search = e.target.value),
                    onmount: (el) => setTimeout(() => el.focus(), 0),
                }),
            ),
            () =>
                state.tab == "functions"
                    ? functionList(state, insertFn)
                    : dataList({
                        groups: () => groups,
                        sample: sampleOf(stepId),
                        onPick: insert,
                        onLoop: findStep(stepId) ? (path) => {
                            state.open = false;
                            loopOver(path, stepId);
                        } : null,
                        state,
                    }),
        );
    }

    /**
     * Text input (or textarea) with a dynamic content picker.
     * obj[key] holds the value; picked variables are inserted at the caret as {{.path}}.
     */
    function exprInput(obj, key, stepId, { placeholder = "", multiline = false, rows = 3, mono = false, name } = {}) {
        const state = store({ open: false, search: "", tab: "data", category: "" });
        const attrs = {
            name: name || key,
            className: `gp-expr-input ${mono ? "txt-code" : ""}`,
            placeholder,
            value: () => obj[key] ?? "",
            oninput: (e) => (obj[key] = e.target.value),
        };
        let input;
        if (multiline) {
            input = t.textarea({ ...attrs, rows });
        } else {
            input = t.input({ ...attrs, type: "text" });
        }

        function insert(path) {
            // inside an action ({{upper |}}) the value alone
            const inAction = actionAt(input.value, input.selectionStart ?? input.value.length) != null;
            const text = inAction ? templateRef(path).replace(/^\{\{(index (.*))\}\}$/, "($1)").replace(/^\{\{(.*)\}\}$/, "$1") : templateRef(path);
            const start = input.selectionStart ?? input.value.length;
            const end = input.selectionEnd ?? start;
            input.setRangeText(text, start, end, "end");
            obj[key] = input.value;
            state.open = false;
            state.search = "";
            input.focus();
        }

        // a function: {{name args}} with its first argument selected (to pick a value for it)
        function insertFn(f) {
            const { text, sel } = functionSnippet(f, input.value, input.selectionStart ?? input.value.length);
            insertAt(input, text, sel);
            obj[key] = input.value;
            state.open = false;
            state.search = "";
            state.tab = "data"; // next: a value for the selected argument
        }

        const loops = () => scopeOf(stepId).loops;
        return t.div(
            { className: () => `gp-expr ${state.open ? "open" : ""} ${multiline ? "multiline" : ""}` },
            input,
            t.div(
                { className: "gp-expr-btns" },
                t.button(
                    {
                        type: "button",
                        className: "btn xs secondary transparent circle",
                        ariaLabel: app.attrs.tooltip("Open the editor"),
                        onmousedown: (e) => e.preventDefault(),
                        onclick: () => {
                            state.open = false;
                            openBindingEditor(obj, key, stepId, { multiline, mono });
                        },
                    },
                    t.i({ className: "ri-expand-diagonal-line", ariaHidden: true }),
                ),
                t.button(
                    {
                        type: "button",
                        className: "btn xs secondary transparent circle gp-expr-btn",
                        ariaLabel: app.attrs.tooltip("Insert dynamic content"),
                        onmousedown: (e) => e.preventDefault(),
                        onclick: () => (state.open = !state.open),
                    },
                    t.i({ className: "ri-flashlight-line", ariaHidden: true }),
                ),
            ),
            () => (state.open ? pickerPanel(stepId, insert, state, insertFn) : null),
            livePreview(() => obj[key], stepId, loops),
        );
    }

    // the large editor of an input (like Budibase's bindings drawer): the
    // data with their values and the functions on the left, the expression
    // and its live result on the right
    function openBindingEditor(obj, key, stepId, { multiline = false, mono = false } = {}) {
        const state = store({ value: obj[key] ?? "", search: "", tab: "data", category: "", expanded: {} });
        let area;

        function insertText(text, sel) {
            insertAt(area, text, sel);
            state.value = area.value;
        }
        function pick(path) {
            const inAction = actionAt(area.value, area.selectionStart ?? area.value.length) != null;
            const ref = templateRef(path);
            insertText(inAction ? ref.replace(/^\{\{(index (.*))\}\}$/, "($1)").replace(/^\{\{(.*)\}\}$/, "$1") : ref);
            state.search = "";
        }
        function pickFn(f) {
            const { text, sel } = functionSnippet(f, area.value, area.selectionStart ?? area.value.length);
            insertText(text, sel);
            state.search = "";
            state.tab = "data";
        }
        function apply() {
            obj[key] = multiline ? state.value : state.value.replace(/\s*\n\s*/g, " ");
            app.modals.close(modal);
        }
        const tab = (k, label, icon) =>
            t.button(
                { type: "button", className: () => `gp-tab-btn ${state.tab == k ? "active" : ""}`, onclick: () => (state.tab = k) },
                t.i({ className: icon, ariaHidden: true }),
                " " + label,
            );

        const modal = t.div(
            { className: "modal popup lg gp-binding-modal" },
            t.header({ className: "modal-header" }, t.h5({ className: "m-auto" }, "Dynamic value")),
            t.div(
                { className: "modal-content" },
                t.div(
                    { className: "gp-binding-editor" },
                    t.div(
                        { className: "gp-binding-side" },
                        t.div({ className: "gp-picker-tabs" }, tab("data", "Data", "ri-database-2-line"), tab("functions", "Functions", "ri-functions")),
                        t.div(
                            { className: "field gp-bare" },
                            t.input({
                                type: "search",
                                placeholder: () => (state.tab == "functions" ? "Search functions..." : "Search data..."),
                                value: () => state.search,
                                oninput: (e) => (state.search = e.target.value),
                            }),
                        ),
                        () =>
                            state.tab == "functions"
                                ? functionList(state, pickFn)
                                : dataList({ groups: () => variableGroups(stepId), sample: sampleOf(stepId), onPick: pick, state }),
                    ),
                    t.div(
                        { className: "gp-binding-main" },
                        (area = t.textarea({
                            className: `gp-binding-area ${mono ? "txt-code" : ""}`,
                            rows: 10,
                            value: () => state.value,
                            oninput: (e) => (state.value = e.target.value),
                            onmount: (el) => setTimeout(() => el.focus(), 50),
                        })),
                        t.div({ className: "gp-label m-t-sm" }, "Result with the test data"),
                        t.div({ className: "gp-binding-result" }, livePreview(() => state.value, stepId)),
                        t.div(
                            { className: "field-help m-t-sm" },
                            "Click a value to insert it, e.g. {{steps.api.body.name}}. Functions take their values after the name: {{upper item.name}}, {{formatDate \"DD/MM/YYYY\" trigger.record.created}}. ",
                            data.sample ? "" : "Run a test of the flow to see the values.",
                        ),
                    ),
                ),
            ),
            t.footer(
                { className: "modal-footer" },
                t.button({ type: "button", className: "btn transparent secondary m-r-auto", onclick: () => app.modals.close(modal) }, t.span({ className: "txt" }, "Cancel")),
                t.button({ type: "button", className: "btn expanded", onclick: apply }, t.span({ className: "txt" }, "Apply")),
            ),
        );
        modal.onafterclose = () => modal.remove();
        document.body.appendChild(modal);
        app.modals.open(modal);
    }

    // the live result of an input, with the test data
    function livePreview(expr, stepId, loops = () => scopeOf(stepId).loops) {
        return previewLine({
            expr,
            loops: () => loops().map((l) => l.key),
            item: () => {
                const inner = loops()[loops().length - 1];
                return inner && !data.sample?.data?.loops?.[inner.key]?.length ? loopSample(inner)?.[0] : undefined;
            },
            version: () => data.sampleVersion,
            queue: previews,
            check: (src) => checkExpr(src, stepId),
        });
    }

    // a template reading a path: {{.a.b}}, or {{index .a "@id" 0}} when a part
    // isn't a template name (XML attributes, list positions, dashes...)
    function templateRef(path) {
        const parts = path.split(".");
        // the roots without their dot: {{steps.api.body}}
        const dot = shortRoots.has(parts[0]) ? "" : ".";
        const i = parts.findIndex((p) => !/^[A-Za-z_][A-Za-z0-9_]*$/.test(p));
        if (i <= 0) {
            return `{{${dot}${path}}}`;
        }
        const rest = parts.slice(i).map((p) => (/^\d+$/.test(p) ? p : JSON.stringify(p)));
        return `{{index ${dot}${parts.slice(0, i).join(".")} ${rest.join(" ")}}}`;
    }

    // dynamic input without a label, with the native input styling
    function bareExpr(obj, key, stepId, opts) {
        return t.div({ className: "field gp-bare" }, exprInput(obj, key, stepId, opts));
    }

    // list: a function filtering data.allCollections (default: the user collections)
    // --- values picked instead of typed -------------------------------------------------------

    /**
     * A dropdown of suggested values (lists for a For each, record ids...),
     * or a custom dynamic value. options() → [{value, label}].
     */
    function choiceExpr(obj, key, stepId, { options, placeholder = "", mono = true, emptyLabel = "- Choose -" }) {
        const state = store({ custom: false });
        const known = () => !obj[key] || options().some((o) => o.value == obj[key]);
        return t.div({ className: "gp-choice" }, () => {
            if (state.custom || !known()) {
                return t.div(
                    { rid: "custom", className: "gp-choice-custom" },
                    exprInput(obj, key, stepId, { placeholder, mono }),
                    options().length
                        ? t.button(
                            {
                                type: "button",
                                className: "btn xs secondary transparent",
                                onclick: () => {
                                    state.custom = false;
                                    if (!known()) {
                                        obj[key] = "";
                                    }
                                },
                            },
                            t.i({ className: "ri-list-check", ariaHidden: true }),
                            t.span({ className: "txt" }, "Choose from the list"),
                        )
                        : null,
                );
            }
            return t.div(
                { rid: "pick_" + (obj[key] || "") },
                select({
                    placeholder: emptyLabel,
                    options: () => [...options(), { value: "__custom", label: "Custom value…" }],
                    value: () => obj[key] || "",
                    onchange: (v) => (v == "__custom" ? (state.custom = true) : (obj[key] = v)),
                }),
                livePreview(() => obj[key], stepId),
            );
        });
    }

    // the lists available at a step: Get records items, and the lists of the
    // test data (API and HTTP responses, variables, trigger...)
    function listOptions(stepId) {
        data.sampleVersion; // refresh with the test data
        const { prev, loops } = scopeOf(stepId);
        const out = [];
        const add = (path, label) => {
            const value = templateRef(path);
            if (!out.some((o) => o.value == value)) {
                out.push({ value, label });
            }
        };
        const findLists = (v, path, depth, label) => {
            if (Array.isArray(v)) {
                const n = sampleCount(path, loops) ?? v.length;
                add(path, `${label} (${n} item${n == 1 ? "" : "s"})`);
                return;
            }
            if (v && typeof v == "object" && depth < 3) {
                for (const k of Object.keys(v).sort()) {
                    findLists(v[k], `${path}.${k}`, depth + 1, `${label} › ${k}`);
                }
            }
        };
        for (const loop of loops) {
            findLists(sampleValue("item", [loop]), "item", 1, "Current item");
        }
        findLists(sampleValue("trigger", loops), "trigger", 0, "Trigger");
        for (const st of prev) {
            const name = st.name || stepTypes[st.type]?.label || st.key;
            if (st.type == "get_records") {
                add(`steps.${st.key}.items`, `${name} › records`);
                continue;
            }
            findLists(sampleValue(`steps.${st.key}`, loops), `steps.${st.key}`, 0, name);
        }
        return out;
    }

    // the record ids available at a step for a collection
    function idOptions(stepId, collectionName) {
        const { prev, loops } = scopeOf(stepId);
        const col = data.allCollections.find((c) => c.name == collectionName);
        const same = (name) => !collectionName || name == collectionName || (col && name == col.id);
        const out = [];
        const add = (path, label) => {
            const value = templateRef(path);
            if (!out.some((o) => o.value == value)) {
                out.push({ value, label });
            }
        };
        const f = data.flow;
        const tcol = f.trigger?.collection;
        if ((f.trigger_type == "record" || f.trigger_type == "manual") && tcol) {
            if (same(tcol)) {
                add("trigger.record.id", `Trigger record (${tcol})`);
            }
            // relations of the trigger record to the collection
            const tc = data.allCollections.find((c) => c.name == tcol);
            for (const fl of tc?.fields || []) {
                if (fl.type == "relation" && col && fl.collectionId == col.id && fl.maxSelect <= 1) {
                    add(`trigger.record.${fl.name}`, `Trigger record › ${fl.name}`);
                }
            }
        }
        if (f.trigger_type == "action") {
            add("trigger.record.id", "Record of the button");
            if (col?.type == "auth") {
                add("trigger.user.id", "Signed-in visitor");
            }
        }
        for (const loop of loops) {
            const src = /^\{\{\s*\.?steps\.([A-Za-z0-9_]+)\.items\s*\}\}$/.exec((loop.config?.items || "").trim());
            const source = src ? stepByKey(src[1]) : null;
            if (source?.type == "get_records" ? same(source.config?.collection) : sampleValue("item.id", [loop]) !== undefined) {
                add("item.id", `Current item (${loop.name || loop.key})`);
            }
        }
        for (const st of prev) {
            const name = st.name || stepTypes[st.type]?.label || st.key;
            if ((st.type == "create_record" || st.type == "update_record") && same(st.config?.collection)) {
                add(`steps.${st.key}.id`, `${name} (its record)`);
            }
            if (st.type == "get_records" && same(st.config?.collection)) {
                add(`steps.${st.key}.first.id`, `${name} (first record)`);
            }
            if (st.type == "sign_in" && same(st.config?.collection || "users")) {
                add(`steps.${st.key}.user.id`, `${name} (signed-in user)`);
            }
        }
        return out;
    }

    // the collection field a value reads (conditions: values matching its type)
    function fieldOfPath(expr, stepId) {
        const m = /^\{\{\s*\.?([A-Za-z0-9_.]+)\s*\}\}$/.exec(String(expr || "").trim());
        if (!m) {
            return null;
        }
        const parts = m[1].split(".");
        let collection;
        let name;
        if (parts[0] == "trigger" && (parts[1] == "record" || parts[1] == "original") && parts.length == 3) {
            collection = data.flow.trigger?.collection;
            name = parts[2];
        } else if (parts[0] == "steps" && parts.length >= 3) {
            const st = stepByKey(parts[1]);
            if (["create_record", "update_record", "delete_record"].includes(st?.type) && parts.length == 3) {
                collection = st.config?.collection;
                name = parts[2];
            } else if (st?.type == "get_records" && parts[2] == "first" && parts.length == 4) {
                collection = st.config?.collection;
                name = parts[3];
            }
        } else if (parts[0] == "item" && parts.length == 2) {
            const loop = scopeOf(stepId).loops.at(-1);
            const src = /^\{\{\s*\.?steps\.([A-Za-z0-9_]+)\.items\s*\}\}$/.exec((loop?.config?.items || "").trim());
            const st = src ? stepByKey(src[1]) : null;
            if (st?.type == "get_records") {
                collection = st.config?.collection;
                name = parts[1];
            }
        }
        const col = data.allCollections.find((c) => c.name == collection);
        return col?.fields.find((f) => f.name == name) || null;
    }

    // the records of a collection for the relation fields (cached per editor)
    // (a new object on each load: the store doesn't watch added keys)
    const relationRecords = store({ byCollection: {} });
    const loadingRelations = new Set();
    function loadRelationRecords(collectionId) {
        if (loadingRelations.has(collectionId)) {
            return;
        }
        loadingRelations.add(collectionId);
        app.pb.collection(collectionId).getList(1, 200, { sort: "-@rowid", requestKey: null })
            .then((res) => (relationRecords.byCollection = { ...relationRecords.byCollection, [collectionId]: res.items }))
            .catch(handleError);
    }

    // date fields: the stored format (UTC) <-> the local date of the input
    const toLocalInput = (v) => {
        const d = v ? new Date(String(v).replace(" ", "T")) : null;
        if (!d || isNaN(d)) {
            return "";
        }
        const pad = (n) => String(n).padStart(2, "0");
        return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`;
    };
    const fromLocalInput = (v) => (v ? new Date(v).toISOString().replace("T", " ") : "");

    const typedFieldTypes = new Set(["bool", "select", "number", "date", "autodate", "relation"]);

    /**
     * The value of a record field: an input matching its type (options of a
     * select, a switch, a date, a record...), or a dynamic value (⚡).
     */
    function fieldValueInput(obj, f, stepId) {
        if (!typedFieldTypes.has(f.type)) {
            return bareExpr(obj, f.name, stepId, { placeholder: f.type == "json" ? "JSON or dynamic content" : "" });
        }
        const state = store({ dynamic: false });
        const isDynamic = () => state.dynamic || String(obj[f.name] ?? "").includes("{{");
        return t.div(
            { className: "gp-typed" },
            () => {
                if (isDynamic()) {
                    return t.div({ rid: "dyn", className: "gp-typed-input" }, bareExpr(obj, f.name, stepId));
                }
                // the dropdowns are rebuilt with their value (a reused one keeps the old
                // selection); the inputs keep their element (and the focus)
                const keyed = ["select", "bool", "relation"].includes(f.type) ? JSON.stringify(obj[f.name] ?? "") : "";
                return t.div({ rid: "fixed_" + keyed, className: "gp-typed-input" }, fixedInput(obj, f));
            },
            t.button(
                {
                    type: "button",
                    className: () => `btn xs circle transparent ${isDynamic() ? "" : "secondary"}`,
                    ariaLabel: app.attrs.tooltip("Fixed value / dynamic content"),
                    onclick: () => {
                        if (isDynamic()) {
                            state.dynamic = false;
                            delete obj[f.name];
                        } else {
                            state.dynamic = true;
                        }
                    },
                },
                t.i({ className: () => (isDynamic() ? "ri-flashlight-fill" : "ri-flashlight-line"), ariaHidden: true }),
            ),
        );
    }

    const boxed = (input) => t.div({ className: "field gp-bare" }, input);

    function fixedInput(obj, f) {
        const set = (v) => {
            if (v === "" || v === undefined || (Array.isArray(v) && !v.length)) {
                delete obj[f.name];
            } else {
                obj[f.name] = v;
            }
        };
        switch (f.type) {
            case "bool":
                return boxed(select({
                    placeholder: "Not set",
                    options: [{ value: "true", label: "Yes (true)" }, { value: "false", label: "No (false)" }],
                    value: () => (obj[f.name] === undefined || obj[f.name] === "" ? "" : String(obj[f.name])),
                    onchange: (v) => set(v === "" ? "" : v == "true"),
                }));
            case "select": {
                const opts = (f.values || []).map((v) => ({ value: v, label: v }));
                return boxed(f.maxSelect > 1
                    ? multiSelect({ placeholder: "Not set", options: opts, max: f.maxSelect, value: () => [].concat(obj[f.name] ?? []), onchange: set })
                    : select({ placeholder: "Not set", options: opts, value: () => obj[f.name] ?? "", onchange: set }));
            }
            case "number":
                return t.div(
                    { className: "field gp-bare" },
                    t.input({ type: "number", step: "any", placeholder: "Not set", value: () => obj[f.name] ?? "", oninput: (e) => set(e.target.value === "" ? "" : Number(e.target.value)) }),
                );
            case "date":
            case "autodate":
                return t.div(
                    { className: "gp-date-input" },
                    t.div(
                        { className: "field gp-bare" },
                        t.input({ type: "datetime-local", value: () => toLocalInput(obj[f.name]), onchange: (e) => set(fromLocalInput(e.target.value)) }),
                    ),
                    ...[["Now", "{{now}}"], ["+1 day", "{{addDays 1 now}}"], ["+7 days", "{{addDays 7 now}}"], ["+30 days", "{{addDays 30 now}}"]].map(([label, v]) =>
                        t.button({ type: "button", className: "btn xs secondary transparent", title: v, onclick: () => set(v) }, label)
                    ),
                );
            case "relation": {
                loadRelationRecords(f.collectionId);
                const opts = () => (relationRecords.byCollection[f.collectionId] || []).map((r) => ({ value: r.id, label: `${recordLabel(r)}${recordLabel(r) != r.id ? ` (${r.id})` : ""}` }));
                return boxed(f.maxSelect > 1
                    ? multiSelect({ placeholder: "Not set", options: opts, max: f.maxSelect, value: () => [].concat(obj[f.name] ?? []), onchange: set })
                    : select({ placeholder: "Not set", options: opts, value: () => obj[f.name] ?? "", onchange: set }));
            }
        }
        return bareExpr(obj, f.name, null);
    }

    // a filter condition added from fields and values (Get records)
    const filterOps = [
        { value: "=", label: "is" },
        { value: "!=", label: "is not" },
        { value: ">", label: ">" },
        { value: ">=", label: "≥" },
        { value: "<", label: "<" },
        { value: "<=", label: "≤" },
        { value: "~", label: "contains" },
        { value: "!~", label: "doesn't contain" },
    ];
    function filterBuilder(c, stepId) {
        const state = store({ open: false, field: "", op: "=", value: {} });
        const col = () => data.allCollections.find((x) => x.name == c.collection);
        const fieldDef = () => col()?.fields.find((f) => f.name == state.field);
        function literal(v, f) {
            if (typeof v == "string" && v.includes("{{")) {
                return v.trim(); // bound as a parameter when the flow runs
            }
            if (f?.type == "number" || f?.type == "bool") {
                return String(v);
            }
            return "'" + String(v ?? "").replace(/\\/g, "\\\\").replace(/'/g, "\\'") + "'";
        }
        function addCondition() {
            const f = fieldDef();
            if (!f) {
                return;
            }
            const v = state.value[f.name] ?? "";
            const cond = `${f.name} ${state.op} ${literal(v, f)}`;
            c.filter = c.filter?.trim() ? `${c.filter.trim()} && ${cond}` : cond;
            state.open = false;
            state.value = {};
        }
        return () => {
            if (!col()) {
                return null;
            }
            if (!state.open) {
                return t.button(
                    { rid: "add", type: "button", className: "btn xs secondary transparent m-t-5", onclick: () => (state.open = true) },
                    t.i({ className: "ri-add-line", ariaHidden: true }),
                    t.span({ className: "txt" }, "Add a condition"),
                );
            }
            return t.div(
                { rid: "builder", className: "gp-filter-builder" },
                t.div({ className: "field gp-bare" }, select({
                    placeholder: "Field",
                    options: () => col().fields.filter((f) => f.type != "password" && !f.hidden).map((f) => ({ value: f.name, label: f.name })),
                    value: () => state.field,
                    onchange: (v) => {
                        state.field = v;
                        state.value = {};
                    },
                })),
                t.div({ className: "field gp-bare gp-op" }, select({ options: filterOps, value: () => state.op, onchange: (v) => (state.op = v || "=") })),
                () => (fieldDef() ? t.div({ rid: "v_" + state.field, className: "gp-filter-value" }, fieldValueInput(state.value, fieldDef(), stepId)) : t.div({ rid: "nov" })),
                t.button({ type: "button", className: "btn sm", disabled: () => !fieldDef(), onclick: addCondition }, t.span({ className: "txt" }, "Add")),
                t.button({ type: "button", className: "btn sm circle transparent secondary", ariaLabel: app.attrs.tooltip("Cancel"), onclick: () => (state.open = false) }, t.i({ className: "ri-close-line", ariaHidden: true })),
            );
        };
    }

    function collectionSelect(obj, key, { placeholder = "- Select collection -", list, required = true } = {}) {
        return t.div(
            { className: "gp-with-reload" },
            select({
                name: key,
                required,
                placeholder,
                options: () => (list ? list() : data.collections).map((c) => ({ value: c.name, label: c.name })),
                value: () => obj[key],
                onchange: (v) => (obj[key] = v),
            }),
            t.button(
                {
                    type: "button",
                    className: "btn xs circle transparent secondary",
                    ariaLabel: app.attrs.tooltip("Reload the collections"),
                    onclick: () => reloadCollections(),
                },
                t.i({ className: "ri-refresh-line", ariaHidden: true }),
            ),
        );
    }

    // the value of a condition rule: the options of a select or bool field
    // read on the left side, or any value
    function conditionValue(rule, sid) {
        const f = fieldOfPath(rule.left, sid);
        if (f?.type == "select" || f?.type == "bool") {
            const options = () => (f.type == "bool" ? ["true", "false"] : f.values || []).map((v) => ({ value: v, label: v }));
            return choiceExpr(rule, "right", sid, { options, mono: false, emptyLabel: "- Choose a value -" });
        }
        return bareExpr(rule, "right", sid, { placeholder: "value" });
    }

    function plainSelect(obj, key, options) {
        return select({
            name: key,
            required: true,
            options,
            value: () => obj[key] ?? "",
            onchange: (v) => (obj[key] = v),
        });
    }

    function numberInput(obj, key, attrs = {}) {
        return t.input({
            type: "number",
            name: key,
            value: () => obj[key] ?? "",
            oninput: (e) => (obj[key] = e.target.value === "" ? "" : Number(e.target.value)),
            ...attrs,
        });
    }

    function switchField(obj, key, label, id) {
        return t.div(
            { className: "field" },
            t.input({ id, type: "checkbox", className: "switch", checked: () => !!obj[key], onchange: (e) => (obj[key] = e.target.checked) }),
            t.label({ htmlFor: id }, t.span({ className: "txt" }, label)),
        );
    }

    // one dynamic input per collection field (empty = not set)
    function recordFieldsEditor(cfg, collectionName, stepId, hint) {
        if (!cfg.data || typeof cfg.data != "object") {
            cfg.data = {};
        }
        const skip = new Set(["id", "created", "updated", "collectionId", "collectionName", "tokenKey"]);
        return t.div(
            { className: "gp-mapping" },
            () => {
                const c = data.allCollections.find((c) => c.name == collectionName());
                if (!c) {
                    return t.div({ rid: "none", className: "txt-hint txt-sm" }, "Select a collection to map its fields.");
                }
                return [
                    t.div({ rid: "hint_" + c.id, className: "field-help m-b-5" }, hint),
                    ...c.fields
                        .filter((f) => !skip.has(f.name) && f.type != "autodate")
                        .map((f) =>
                            t.div(
                                { rid: c.id + f.name, className: "gp-mapping-row" },
                                t.div({ className: "gp-mapping-label" }, t.span(null, f.name), t.small({ className: "txt-hint" }, f.type)),
                                fieldValueInput(cfg.data, f, stepId),
                            )
                        ),
                ];
            },
        );
    }

    // free key/value editor stored as an object (keys renamed on change)
    // a typed key of an HTTP request: "Move to Secrets" (keeps it out of the
    // flow), or pick a secret
    function kvSecret(obj, k, stepId) {
        const use = (name) => {
            const v = String(obj[k] ?? "");
            // "Bearer xyz" moved: "Bearer {{secret.NAME}}"
            const m = v.match(/^(Bearer|Basic|Token)\s+/i);
            obj[k] = (m && !v.includes("{{") ? m[0] : "") + secretRef(name);
        };
        const suggest = () => {
            const v = String(obj[k] ?? "");
            const plain = v.includes("{{") ? "" : v.replace(/^(Bearer|Basic|Token)\s+/i, "");
            // named after the site and the header: SHOP_EXAMPLE_TOKEN
            const step = findStep(stepId)?.step;
            let site = "";
            try {
                const host = new URL(String(step?.config?.url || "")).hostname;
                if (!/^[\d.]+$|^localhost$|\[/.test(host)) {
                    site = host.split(".").filter((p) => !/^(www|api|app)$/i.test(p)).slice(0, -1).join("_");
                }
            } catch {}
            const what = /^authorization$/i.test(k) ? "TOKEN" : k;
            // a local address: named after the step (CALL_THE_SHOP_TOKEN)
            const prefix = site || step?.name || "";
            return { name: secretName(prefix ? `${prefix}_${what}` : what), value: plain, description: step?.name ? `Used by the step "${step.name}"` : "" };
        };
        return t.div(
            { className: "gp-kv-secret" },
            () =>
                looksSecret(k, obj[k])
                    ? t.button(
                        {
                            rid: "move",
                            type: "button",
                            className: "btn xs warning transparent",
                            ariaLabel: app.attrs.tooltip("This value is saved in the flow: keep it in Secrets instead (encrypted, hidden in the logs)"),
                            onclick: () => openSecretModal({ ...suggest(), onsaved: (s) => use(s.name) }),
                        },
                        t.i({ className: "ri-key-2-line", ariaHidden: true }),
                        t.span({ className: "txt" }, "Move to Secrets"),
                    )
                    : t.div({ rid: "pick" }, secretPicker({ onpick: use, suggest })),
        );
    }

    function kvEditor(parent, prop, stepId, { keyPlaceholder = "name", valuePlaceholder = "value", addLabel = "Add", secrets = false } = {}) {
        if (!parent[prop] || typeof parent[prop] != "object") {
            parent[prop] = {};
        }
        return t.div(
            { className: "gp-kv" },
            () =>
                Object.keys(parent[prop]).map((k, i) =>
                    t.div(
                        { rid: "kv_" + i + "_" + k, className: "gp-kv-row" },
                        t.div(
                            { className: "field gp-bare gp-kv-key" },
                            t.input({
                                type: "text",
                                className: "txt-code",
                                placeholder: keyPlaceholder,
                                value: k,
                                onchange: (e) => {
                                    const newKey = e.target.value.trim();
                                    if (!newKey || newKey == k) {
                                        e.target.value = k;
                                        return;
                                    }
                                    const entries = Object.entries(parent[prop]);
                                    entries[i][0] = newKey;
                                    parent[prop] = Object.fromEntries(entries);
                                },
                            }),
                        ),
                        bareExpr(parent[prop], k, stepId, { placeholder: valuePlaceholder }),
                        secrets ? kvSecret(parent[prop], k, stepId) : null,
                        t.button(
                            {
                                type: "button",
                                className: "btn sm circle transparent danger",
                                ariaLabel: app.attrs.tooltip("Remove"),
                                onclick: () => {
                                    const copy = { ...parent[prop] };
                                    delete copy[k];
                                    parent[prop] = copy;
                                },
                            },
                            t.i({ className: "ri-close-line", ariaHidden: true }),
                        ),
                    )
                ),
            t.button(
                {
                    type: "button",
                    className: "btn sm secondary transparent",
                    onclick: () => {
                        let n = Object.keys(parent[prop]).length + 1;
                        while (parent[prop]["key" + n] !== undefined) {
                            n++;
                        }
                        parent[prop] = { ...parent[prop], ["key" + n]: "" };
                    },
                },
                t.i({ className: "ri-add-line", ariaHidden: true }),
                t.span({ className: "txt" }, addLabel),
            ),
        );
    }

    // --- AI decision step -----------------------------------------------------------------

    const aiStateFieldTypes = new Set(["text", "editor", "email", "url", "number", "bool", "select", "date", "json"]);
    const aiReservedKeys = new Set(["confident", "model", "provider", "calibrated", "usage"]);

    // the readable fields of the trigger record, as named state values
    function triggerStateValues() {
        const cfg = data.flow.trigger || {};
        const c = data.allCollections.find((x) => x.name == cfg.collection);
        const values = {};
        if (!c || !["record", "manual"].includes(data.flow.trigger_type)) {
            return values;
        }
        for (const f of c.fields) {
            if (aiStateFieldTypes.has(f.type) && !f.hidden && f.name != "id") {
                values[f.name] = `{{.trigger.record.${f.name}}}`;
            }
        }
        return values;
    }

    // render versions of the AI lists: a new version re-creates the rows
    // (their closures hold indexes, see the shablon rid reuse note in grid.js)
    function bump(key) {
        data.aiv[key] = (data.aiv[key] || 0) + 1;
    }
    function ver(key) {
        return data.aiv[key] || 0;
    }

    function selectFieldsOf(collectionName) {
        const c = data.allCollections.find((x) => x.name == collectionName || x.id == collectionName);
        return c ? c.fields.filter((f) => f.type == "select") : [];
    }

    function selectedModelType(c) {
        return aiModels().find((m) => m.id == c.model)?.type || "";
    }

    function smallButton(icon, label, onclick, extra = "secondary") {
        return t.button(
            { type: "button", className: `btn xs circle transparent ${extra}`, ariaLabel: app.attrs.tooltip(label), onclick },
            t.i({ className: icon, ariaHidden: true }),
        );
    }

    // up / down / remove of the item i of parent[prop] (a list)
    function rowActions(parent, prop, i, vk) {
        const copy = () => JSON.parse(JSON.stringify(parent[prop] || []));
        const swap = (j) => {
            const l = copy();
            if (j < 0 || j >= l.length) {
                return;
            }
            [l[i], l[j]] = [l[j], l[i]];
            parent[prop] = l;
            bump(vk);
        };
        return t.div(
            { className: "gp-ai-row-actions" },
            smallButton("ri-arrow-up-line", "Move up", () => swap(i - 1)),
            smallButton("ri-arrow-down-line", "Move down", () => swap(i + 1)),
            smallButton("ri-close-line", "Remove", () => {
                const l = copy();
                l.splice(i, 1);
                parent[prop] = l;
                bump(vk);
            }, "danger"),
        );
    }

    function addRowButton(label, onclick, disabled = () => false) {
        return t.button(
            { type: "button", className: "btn sm secondary transparent", disabled, onclick },
            t.i({ className: "ri-add-line", ariaHidden: true }),
            t.span({ className: "txt" }, label),
        );
    }

    function sourceHeader(label, obj, sources) {
        return t.div(
            { className: "flex gap-10 m-b-5" },
            t.span({ className: "gp-label m-r-auto m-b-0" }, label),
            t.div({ className: "field gp-bare gp-inline-select" }, plainSelect(obj, "source", sources)),
        );
    }

    const aiOptionSources = [
        { value: "static", label: "Fixed list" },
        { value: "select", label: "Values of a select field" },
        { value: "records", label: "Records of a collection" },
        { value: "expression", label: "Dynamic list" },
    ];
    const aiLevelSources = [
        { value: "static", label: "Fixed levels" },
        { value: "select", label: "Values of a select field" },
        { value: "expression", label: "Dynamic list" },
    ];

    function aiStaticOptions(o, sid, vk) {
        if (!Array.isArray(o.items)) {
            o.items = [];
        }
        return t.div(
            null,
            () =>
                t.div(
                    { rid: `opts${o.items.length}:${ver(vk)}`, className: "gp-ai-rows" },
                    o.items.map((item, i) =>
                        t.div(
                            { className: "gp-ai-row" },
                            bareExpr(item, "key", sid, { placeholder: "option, e.g. billing", mono: true }),
                            bareExpr(item, "description", sid, { placeholder: "what it means (recommended)" }),
                            rowActions(o, "items", i, vk),
                        )
                    ),
                ),
            addRowButton("Add option", () => (o.items = [...JSON.parse(JSON.stringify(o.items)), { key: "", description: "" }]), () => o.items.length >= 255),
        );
    }

    function aiStaticLevels(l, sid, vk) {
        if (!Array.isArray(l.items)) {
            l.items = [];
        }
        return t.div(
            null,
            () =>
                t.div(
                    { rid: `levels${l.items.length}:${ver(vk)}`, className: "gp-ai-rows" },
                    l.items.map((_, i) =>
                        t.div(
                            { className: "gp-ai-row level" },
                            t.span({ className: "gp-ai-level-index", title: "Level index" }, String(i)),
                            bareExpr(l.items, i, sid, { placeholder: i == 0 ? "lowest level, e.g. Calm" : "next level, e.g. Angry" }),
                            rowActions(l, "items", i, vk),
                        )
                    ),
                ),
            addRowButton("Add level", () => (l.items = [...l.items, ""]), () => l.items.length >= 10),
        );
    }

    function aiSelectSource(o, sid, isLevel) {
        return t.div(
            null,
            t.div(
                { className: "grid sm" },
                t.div({ className: "col-lg-6" }, field({ label: "Collection" }, collectionSelect(o, "collection", {
                    list: () => data.allCollections.filter((x) => !x.name.startsWith("_") && x.fields.some((f) => f.type == "select")),
                }))),
                t.div({ className: "col-lg-6" }, field({ label: "Select field" }, select({
                    required: true,
                    placeholder: "- Select field -",
                    options: () => selectFieldsOf(o.collection).map((f) => ({ value: f.name, label: f.name })),
                    value: () => o.field,
                    onchange: (v) => (o.field = v),
                }))),
            ),
            () => {
                const f = selectFieldsOf(o.collection).find((f) => f.name == o.field);
                if (!f) {
                    return t.div({ rid: "nofield" });
                }
                if (!o.descriptions || typeof o.descriptions != "object") {
                    o.descriptions = {};
                }
                return t.div(
                    { rid: "desc_" + o.collection + "." + o.field + ":" + f.values.join(), className: "gp-mapping" },
                    t.div(
                        { className: "field-help" },
                        isLevel
                            ? "The levels follow the order of the field values (first = lowest). Describe them (optional):"
                            : "Describe the options (optional, improves the answers):",
                    ),
                    f.values.map((v) =>
                        t.div(
                            { className: "gp-mapping-row" },
                            t.div({ className: "gp-mapping-label" }, t.span(null, v)),
                            bareExpr(o.descriptions, v, sid, { placeholder: "what it means" }),
                        )
                    ),
                );
            },
        );
    }

    function aiRecordsSource(o, sid) {
        if (!Array.isArray(o.describe)) {
            o.describe = [];
        }
        return t.div(
            null,
            t.div(
                { className: "grid sm" },
                t.div({ className: "col-lg-6" }, field({ label: "Collection" }, collectionSelect(o, "collection", { list: () => data.allCollections.filter((x) => !x.name.startsWith("_")) }))),
                t.div({ className: "col-lg-3" }, field({ label: "Sort" }, t.input({ type: "text", className: "txt-code", placeholder: "name", value: () => o.sort || "", oninput: (e) => (o.sort = e.target.value) }))),
                t.div({ className: "col-lg-3" }, field({ label: "Max options" }, numberInput(o, "limit", { min: 2, max: 255, placeholder: "100" }))),
            ),
            field(
                { label: "Filter (optional)", hint: "PocketBase filter. Dynamic content is passed as safe parameters, e.g. active = true && region = {{.trigger.record.region}}" },
                exprInput(o, "filter", sid, { mono: true, placeholder: "active = true" }),
            ),
            field(
                { label: "Describe each option with", hint: "Fields the model reads to tell the records apart. The answer is the record id (\"choice\"), its name is in \"label\"." },
                multiSelect({
                    placeholder: "The record name only",
                    options: () => fieldsOf(o.collection).filter((n) => n != "id").map((n) => ({ value: n, label: n })),
                    value: () => o.describe,
                    onchange: (v) => (o.describe = v),
                }),
            ),
        );
    }

    function aiListSource(o, sid, withKey) {
        const col = withKey ? "col-lg-4" : "col-lg-6";
        const path = (key, label, placeholder, hint) =>
            t.div({ className: col }, field({ label, hint }, t.input({ type: "text", className: "txt-code", placeholder, value: () => o[key] || "", oninput: (e) => (o[key] = e.target.value) })));
        return t.div(
            null,
            field(
                { label: "List", hint: "Dynamic content holding a list of texts, or of objects (e.g. the items of a \"Get records\" step)." },
                exprInput(o, "expression", sid, { mono: true, placeholder: "{{.steps.records.items}}" }),
            ),
            t.div(
                { className: "grid sm" },
                withKey ? path("keyPath", "Key path", "id", "Objects: the value answered") : null,
                path("labelPath", "Name path", "name", "Objects: the readable name"),
                path("descriptionPath", "Description path", "description", "Objects: what it means"),
            ),
        );
    }

    function aiQuestionBody(q, sid) {
        switch (q.type) {
            case "choice": {
                if (!q.options || typeof q.options != "object") {
                    q.options = { source: "static", items: [] };
                }
                const o = q.options;
                return t.div(
                    { rid: "choice", className: "gp-ai-sub" },
                    sourceHeader("Options", o, aiOptionSources),
                    () => {
                        switch (o.source) {
                            case "select":
                                return t.div({ rid: "select" }, aiSelectSource(o, sid, false));
                            case "records":
                                return t.div({ rid: "records" }, aiRecordsSource(o, sid));
                            case "expression":
                                return t.div({ rid: "expression" }, aiListSource(o, sid, true));
                            default:
                                return t.div({ rid: "static" }, aiStaticOptions(o, sid, q.id + ":options"));
                        }
                    },
                );
            }
            case "score": {
                if (!q.levels || typeof q.levels != "object") {
                    q.levels = { source: "static", items: [] };
                }
                const l = q.levels;
                return t.div(
                    { rid: "score", className: "gp-ai-sub" },
                    sourceHeader("Levels, from lowest to highest (2 to 10)", l, aiLevelSources),
                    () => {
                        switch (l.source) {
                            case "select":
                                return t.div({ rid: "select" }, aiSelectSource(l, sid, true));
                            case "expression":
                                return t.div({ rid: "expression" }, aiListSource(l, sid, false));
                            default:
                                return t.div({ rid: "static" }, aiStaticLevels(l, sid, q.id + ":levels"));
                        }
                    },
                );
            }
            default:
                return t.div(
                    { rid: "noul", className: "grid sm" },
                    t.div({ className: "col-lg-5" }, field({ label: "Yes means (optional)" }, exprInput(q, "yes", sid, { placeholder: "e.g. Can't work, payments failing" }))),
                    t.div({ className: "col-lg-5" }, field({ label: "No means (optional)" }, exprInput(q, "no", sid, { placeholder: "e.g. A question or a minor issue" }))),
                    t.div({ className: "col-lg-2" }, field({ label: "Yes from", hint: "probability" }, numberInput(q, "threshold", { min: 0, max: 1, step: 0.05 }))),
                );
        }
    }

    function aiQuestionCard(c, q, i, sid) {
        const vk = sid + ":questions";
        const copy = () => JSON.parse(JSON.stringify(c.questions));
        const move = (d) => {
            const l = copy();
            const j = i + d;
            if (j < 0 || j >= l.length) {
                return;
            }
            [l[i], l[j]] = [l[j], l[i]];
            c.questions = l;
            bump(vk);
        };
        const duplicate = () => {
            const l = copy();
            const dup = JSON.parse(JSON.stringify(l[i]));
            dup.id = randomId();
            let key = dup.key + "_copy";
            let n = 2;
            while (l.some((x) => x.key == key)) {
                key = dup.key + "_copy" + n++;
            }
            dup.key = key;
            l.splice(i + 1, 0, dup);
            c.questions = l;
            bump(vk);
        };
        const remove = () => {
            const l = copy();
            l.splice(i, 1);
            c.questions = l;
            bump(vk);
        };

        return t.div(
            { className: "gp-ai-question" },
            t.div(
                { className: "gp-ai-qhead" },
                t.div(
                    { className: "gp-ai-qtypes", role: "radiogroup" },
                    Object.entries(aiQuestionTypes).map(([type, def]) =>
                        t.button(
                            {
                                type: "button",
                                className: () => `gp-ai-qtype ${q.type == type ? "active" : ""}`,
                                ariaLabel: app.attrs.tooltip(def.hint),
                                onclick: () => (q.type = type),
                            },
                            t.i({ className: def.icon, ariaHidden: true }),
                            t.span({ className: "txt" }, def.label),
                        )
                    ),
                ),
                t.div(
                    { className: "field gp-bare gp-ai-qkey" },
                    t.input({
                        type: "text",
                        className: "txt-code",
                        ariaLabel: app.attrs.tooltip("Answer key, used as {{.steps.KEY.ANSWER_KEY...}}"),
                        value: () => q.key,
                        onchange: (e) => {
                            const v = e.target.value.trim().replace(/[^A-Za-z0-9_]/g, "_");
                            if (!/^[A-Za-z_]/.test(v) || aiReservedKeys.has(v) || c.questions.some((x) => x.id != q.id && x.key == v)) {
                                e.target.value = q.key;
                                app.toasts.error("The answer key must be unique, start with a letter and not be confident, model, provider, calibrated or usage.");
                                return;
                            }
                            q.key = v;
                        },
                    }),
                ),
                t.nav(
                    { className: "gp-ai-qactions" },
                    smallButton("ri-arrow-up-line", "Move up", () => move(-1)),
                    smallButton("ri-arrow-down-line", "Move down", () => move(1)),
                    smallButton("ri-file-copy-line", "Duplicate", duplicate),
                    smallButton("ri-delete-bin-7-line", "Delete the question", remove, "danger"),
                ),
            ),
            bareExpr(q, "question", sid, { multiline: true, rows: 2, placeholder: "A literal question, e.g. Is the customer blocked right now?" }),
            () => aiQuestionBody(q, sid),
            t.div(
                { className: "grid sm gp-ai-qfoot" },
                t.div(
                    { className: "col-lg-4" },
                    field({ label: "Confident from", hint: "confidence 0-1 (0 = always)" }, numberInput(q, "minConfidence", { min: 0, max: 1, step: 0.05 })),
                ),
                t.div(
                    { className: "col-lg-8" },
                    t.details(
                        { className: "gp-ai-context", open: Object.keys(q.context || {}).length > 0 },
                        t.summary(null, "Context values (optional)"),
                        t.div(
                            { className: "field-help" },
                            "Data the question refers to by name, e.g. a \"candidate\" record: Is the person of the state the same as `candidate`?",
                        ),
                        kvEditor(q, "context", sid, { keyPlaceholder: "candidate", addLabel: "Add context value" }),
                    ),
                ),
            ),
        );
    }

    const responseFormats = [
        { value: "auto", label: "Automatic (JSON, XML or text)" },
        { value: "json", label: "JSON" },
        { value: "xml", label: "XML" },
        { value: "text", label: "Raw text" },
        { value: "legacy", label: "JSON or text (before XML support)" },
    ];

    // --- API call step (APIs module) ------------------------------------------------------

    function apiCallConfig(c, sid) {
        if (!c.vars || typeof c.vars != "object") {
            c.vars = {};
        }
        const call = () => data.apiCalls.find((x) => x.id == c.call);
        return [
            () => {
                if (!globalThis.gpAPIs) {
                    return t.div({ rid: "nomodule", className: "alert warning m-b-sm" }, "The APIs module isn't installed: this step fails.");
                }
                if (!data.apiCalls.length) {
                    return t.div(
                        { rid: "nocalls", className: "alert info m-b-sm" },
                        t.span({ className: "m-r-auto" }, "No saved API calls yet."),
                        t.a({ className: "btn sm", href: "#/apis", target: "_blank" }, t.i({ className: "ri-add-line", ariaHidden: true }), t.span({ className: "txt" }, "Set up an API")),
                    );
                }
                return t.div(
                    { rid: "calls", className: "grid sm m-b-sm" },
                    t.div(
                        { className: "col-lg-8" },
                        field(
                            { label: "Call" },
                            select({
                                required: true,
                                placeholder: "- Select a call -",
                                options: () => data.apiCalls.map((x) => ({ value: x.id, label: `${x.sourceName} · ${x.name} (${x.method || "GET"})` })),
                                value: () => c.call,
                                onchange: (v) => {
                                    c.call = v;
                                    // keep the values of the variables the new call has
                                    const names = new Set((call()?.variables || []).map((x) => x.name));
                                    c.vars = Object.fromEntries(Object.entries(c.vars).filter(([k]) => names.has(k)));
                                },
                            }),
                        ),
                    ),
                    t.div(
                        { className: "col-lg-4 gp-link-col" },
                        () =>
                            c.call
                                ? t.a({ rid: "edit_" + c.call, href: "#/apis/" + c.call, target: "_blank", className: "txt-sm" }, "Edit the call ", t.i({ className: "ri-external-link-line", ariaHidden: true }))
                                : t.a({ rid: "manage", href: "#/apis", target: "_blank", className: "txt-sm" }, "Manage APIs ", t.i({ className: "ri-external-link-line", ariaHidden: true })),
                        t.button(
                            { type: "button", className: "btn xs circle transparent secondary m-l-5", ariaLabel: app.attrs.tooltip("Reload the calls"), onclick: () => loadAPICalls() },
                            t.i({ className: "ri-refresh-line", ariaHidden: true }),
                        ),
                    ),
                );
            },
            () => {
                const x = call();
                if (!x) {
                    return c.call && data.apiCalls.length ? t.div({ rid: "missing", className: "alert danger m-b-sm" }, "This call doesn't exist anymore.") : null;
                }
                return t.div(
                    { rid: "vars_" + x.id + x.variables.map((v) => v.name).join(",") },
                    x.description ? t.div({ className: "field-help m-b-sm" }, x.description) : null,
                    x.variables.length
                        ? t.div(
                            { className: "gp-mapping" },
                            t.div({ className: "field-help m-b-5" }, "Values of the variables of the call (\"API\": shared by the calls of its API, e.g. the host or a token)."),
                            ...x.variables.map((v) =>
                                t.div(
                                    { className: "gp-mapping-row" },
                                    t.div(
                                        { className: "gp-mapping-label" },
                                        t.span({ className: "txt-code" }, v.name),
                                        v.required || v.api ? t.small({ className: "txt-hint" }, [v.api ? "API" : "", v.required ? "required" : ""].filter(Boolean).join(" · ")) : null,
                                    ),
                                    bareExpr(c.vars, v.name, sid, { placeholder: v.default ? `default: ${v.default}` : v.required ? "required" : "" }),
                                )
                            ),
                        )
                        : t.div({ className: "field-help m-b-sm" }, "The call has no variables."),
                    responseLists(x, sid),
                    !x.fields.length && !x.outputs.length
                        ? t.div({ className: "field-help m-t-5" }, "Test the call once (Edit the call → Test) to pick its response fields in the next steps.")
                        : null,
                );
            },
            t.div({ className: "m-t-sm" }, switchField(c, "failOnError", "Fail the step on error status (4xx/5xx)", "fail_" + sid)),
        ];
    }

    // the lists of the saved test response of a call: a For each in one click
    function responseLists(call, sid) {
        const body = call.sample;
        if (body === undefined || body === null) {
            return null;
        }
        const key = findStep(sid)?.step?.key;
        const lists = [];
        const walk = (v, path, depth) => {
            if (Array.isArray(v)) {
                lists.push(path);
                return;
            }
            if (v && typeof v == "object" && depth < 3) {
                for (const k of Object.keys(v).sort()) {
                    walk(v[k], path ? `${path}.${k}` : k, depth + 1);
                }
            }
        };
        walk(body, "", 0);
        return t.div(
            { className: "gp-api-response m-t-sm" },
            t.span({ className: "txt-hint txt-sm" }, "Response (last test): ", t.code(null, valueSummary(body))),
            ...lists.slice(0, 4).map((p) => {
                const path = `steps.${key}.body${p ? "." + p : ""}`;
                return t.button(
                    { type: "button", className: "btn xs secondary transparent", title: `Add a For each step over ${path} after this step`, onclick: () => addLoopAfter(sid, path) },
                    t.i({ className: "ri-repeat-line", ariaHidden: true }),
                    t.span({ className: "txt" }, `For each ${p || "item"}`),
                );
            }),
        );
    }

    // a For each step over a list, after a step
    function addLoopAfter(stepId, path) {
        const found = findStep(stepId);
        if (!found) {
            return;
        }
        const keys = allKeys();
        let key = "each";
        for (let n = 2; keys.has(key); n++) {
            key = "each" + n;
        }
        found.list.splice(found.index + 1, 0, { id: randomId(), key, name: "For each", type: "loop", config: { ...defaultConfig("loop"), items: templateRef(path) }, steps: [] });
        app.toasts.success(`Added a For each over ${path}: add its steps inside, they can use the fields of "current item".`);
    }

    function aiModelPicker(c, wanted) {
        return () => {
            if (!aiModels().length) {
                return t.div(
                    { rid: "nomodels", className: "alert warning m-b-sm" },
                    t.span({ className: "m-r-auto" }, "No AI model yet."),
                    t.button(
                        { type: "button", className: "btn sm", onclick: () => openModelModal({ onsaved: (m) => (c.model = m.id) }) },
                        t.i({ className: "ri-add-line", ariaHidden: true }),
                        t.span({ className: "txt" }, "Add a model"),
                    ),
                );
            }
            return t.div(
                { rid: "models", className: "grid sm m-b-sm" },
                t.div(
                    { className: "col-lg-8" },
                    field({ label: "AI model" }, select({
                        required: true,
                        placeholder: "- Select model -",
                        options: () => aiModels().map((m) => ({ value: m.id, label: `${m.name} · ${modelTypes[m.type]?.label || m.type}` })),
                        value: () => c.model,
                        onchange: (v) => (c.model = v),
                    })),
                ),
                t.div(
                    { className: "col-lg-4 gp-link-col" },
                    t.a({ href: "#/settings/ai/models", target: "_blank", className: "txt-sm" }, "Manage models ", t.i({ className: "ri-external-link-line", ariaHidden: true })),
                ),
                () => {
                    const type = selectedModelType(c);
                    if (wanted && type && type != wanted) {
                        return t.div({ rid: "wrong_" + type, className: "col-lg-12 alert warning" }, `This step needs a ${modelTypes[wanted].label} model.`);
                    }
                    return null;
                },
            );
        };
    }

    function aiGenerateConfig(s) {
        const c = s.config;
        const sid = s.id;
        if (!Array.isArray(c.fields)) {
            c.fields = [];
        }
        const vk = sid + ":fields";
        const fieldTypes = [
            { value: "text", label: "Text" },
            { value: "number", label: "Number" },
            { value: "boolean", label: "Yes / no" },
            { value: "list", label: "List of texts" },
        ];
        return [
            aiModelPicker(c, "standard"),
            field(
                { label: "Instructions (system)", hint: "Role, tone and rules, e.g. You write short, friendly emails for Acme. Never promise refunds." },
                exprInput(c, "system", sid, { multiline: true, rows: 3 }),
            ),
            field(
                { label: "Prompt", hint: "The request, with dynamic content. Record data is data: it shouldn't change your instructions." },
                exprInput(c, "prompt", sid, { multiline: true, rows: 5, placeholder: "Write a reply to this ticket: {{.trigger.record.body}}" }),
            ),
            t.div(
                { className: "flex gap-10 m-b-5" },
                t.span({ className: "gp-label m-r-auto m-b-0" }, "Output"),
                t.div({ className: "field gp-bare gp-inline-select" }, plainSelect(c, "output", [
                    { value: "text", label: "A text" },
                    { value: "fields", label: "Named fields (JSON)" },
                ])),
            ),
            () => {
                if (c.output != "fields") {
                    return t.div({ rid: "text", className: "field-help" }, `The answer is in {{.steps.${s.key}.text}}.`);
                }
                return t.div(
                    { rid: "fields", className: "gp-ai-sub" },
                    () =>
                        t.div(
                            { rid: `fields${c.fields.length}:${ver(vk)}`, className: "gp-ai-rows" },
                            c.fields.map((f, i) =>
                                t.div(
                                    { className: "gp-ai-row gp-ai-field-row" },
                                    t.div(
                                        { className: "field gp-bare" },
                                        t.input({
                                            type: "text",
                                            className: "txt-code",
                                            placeholder: "name",
                                            value: () => f.name || "",
                                            onchange: (e) => (f.name = e.target.value.trim().replace(/[^A-Za-z0-9_]/g, "_")),
                                        }),
                                    ),
                                    t.div({ className: "field gp-bare" }, plainSelect(f, "type", fieldTypes)),
                                    t.div(
                                        { className: "field gp-bare" },
                                        t.input({ type: "text", placeholder: "what it should contain", value: () => f.description || "", oninput: (e) => (f.description = e.target.value) }),
                                    ),
                                    rowActions(c, "fields", i, vk),
                                )
                            ),
                        ),
                    addRowButton("Add field", () => (c.fields = [...JSON.parse(JSON.stringify(c.fields)), { id: randomId(), name: "", type: "text", description: "" }])),
                );
            },
            t.div(
                { className: "grid sm m-t-sm" },
                t.div({ className: "col-lg-4" }, field({ label: "Temperature", hint: "0 = focused, 1 = creative (empty = model default)" }, numberInput(c, "temperature", { min: 0, max: 2, step: 0.1 }))),
                t.div({ className: "col-lg-4" }, field({ label: "Max tokens", hint: "empty = model default" }, numberInput(c, "maxTokens", { min: 1 }))),
                t.div({ className: "col-lg-4" }, field({ label: "Timeout (seconds)" }, numberInput(c, "timeoutSeconds", { min: 1, max: 300 }))),
            ),
        ];
    }

    function aiStepConfig(s) {
        const c = s.config;
        const sid = s.id;
        if (!c.state || typeof c.state != "object") {
            c.state = { mode: "values", values: {} };
        }
        if (!Array.isArray(c.questions)) {
            c.questions = [];
        }
        const vk = sid + ":questions";

        function addQuestion(type) {
            const keys = new Set(c.questions.map((q) => q.key));
            let n = c.questions.length + 1;
            let key = "q" + n;
            while (keys.has(key)) {
                key = "q" + ++n;
            }
            c.questions = [...JSON.parse(JSON.stringify(c.questions)), newAIQuestion(type, key)];
            bump(vk);
        }

        return [
            // model
            aiModelPicker(c, ""),
            () => {
                const type = selectedModelType(c);
                if (!type) {
                    return null;
                }
                return t.div(
                    { rid: "hint_" + type, className: "field-help gp-ai-model-hint" },
                    type == "system_one"
                        ? "System One: calibrated probabilities, answers in well under a second. Not for math, dates or text generation."
                        : "Standard: the probabilities are estimated by the model itself (not calibrated); slower than System One.",
                );
            },

            // state
            t.div(
                { className: "gp-ai-section" },
                t.div(
                    { className: "flex gap-10 m-b-5" },
                    t.span({ className: "gp-label m-r-auto m-b-0" }, "Data to evaluate"),
                    t.div({ className: "field gp-bare gp-inline-select" }, plainSelect(c.state, "mode", [
                        { value: "values", label: "Named values" },
                        { value: "expression", label: "One expression" },
                    ])),
                ),
                () => {
                    if (c.state.mode == "expression") {
                        return t.div(
                            { rid: "expr" },
                            field(
                                { hint: "Any dynamic content: a record, the items of a \"Get records\" step, a webhook body..." },
                                exprInput(c.state, "expression", sid, { placeholder: "{{.trigger.record}}", mono: true }),
                            ),
                        );
                    }
                    return t.div(
                        { rid: "values" },
                        t.div({ className: "field-help" }, "Send only what the questions need: unrelated data lowers the accuracy. Everything here is data, never instructions."),
                        kvEditor(c.state, "values", sid, { keyPlaceholder: "name", addLabel: "Add value" }),
                        () =>
                            data.flow.trigger?.collection && ["record", "manual"].includes(data.flow.trigger_type)
                                ? t.button(
                                    {
                                        rid: "addfields",
                                        type: "button",
                                        className: "btn sm secondary transparent",
                                        onclick: () => (c.state.values = { ...triggerStateValues(), ...(c.state.values || {}) }),
                                    },
                                    t.i({ className: "ri-database-2-line", ariaHidden: true }),
                                    t.span({ className: "txt" }, "Add the trigger record fields"),
                                )
                                : null,
                    );
                },
                t.div(
                    { className: "grid sm" },
                    t.div({ className: "col-lg-4" }, field({ label: "Max characters per text", hint: "Longer texts are cut (0 = no limit)." }, numberInput(c, "maxChars", { min: 0 }))),
                ),
            ),

            // questions
            t.div(
                { className: "gp-ai-section" },
                t.span({ className: "gp-label" }, "Questions (all answered in one call)"),
                () =>
                    t.div(
                        { rid: `questions${c.questions.length}:${ver(vk)}`, className: "gp-ai-questions" },
                        c.questions.map((q, i) => aiQuestionCard(c, q, i, sid)),
                    ),
                t.div(
                    { className: "flex gap-5 m-t-sm" },
                    t.span({ className: "txt-hint txt-sm" }, "Add a question:"),
                    Object.entries(aiQuestionTypes).map(([type, def]) =>
                        t.button(
                            { type: "button", className: "btn sm secondary transparent", onclick: () => addQuestion(type) },
                            t.i({ className: def.icon, ariaHidden: true }),
                            t.span({ className: "txt" }, def.label),
                        )
                    ),
                ),
                t.details(
                    { className: "gp-ai-tips" },
                    t.summary(null, "Tips for reliable answers"),
                    t.ul(
                        null,
                        t.li(null, "Write literal questions and describe every option: the model answers what is written."),
                        t.li(null, "Split a vague judgment into several precise questions and combine them with a Condition."),
                        t.li(null, "Keep math, counting and date comparisons in Conditions or scripts."),
                        t.li(null, "Public data (forms, webhooks) can try to steer the answers: test edge cases, and use \"Confident from\" with a human review branch."),
                        t.li(null, "System One models can slightly favor the first options: check the answers with the options in another order."),
                    ),
                ),
            ),

            // options and branches
            t.div(
                { className: "grid sm m-t-sm" },
                t.div({ className: "col-lg-6" }, switchField(c, "branches", "Branches: \"If confident\" / \"Otherwise\"", "br_" + sid)),
                t.div(
                    { className: "col-lg-6" },
                    () =>
                        selectedModelType(c) == "standard"
                            ? t.div({ rid: "explain" }, switchField(c, "explain", "Explain the answers (reason)", "ex_" + sid))
                            : t.div({ rid: "noexplain" }),
                ),
                t.div({ className: "col-lg-4" }, field({ label: "Timeout (seconds)" }, numberInput(c, "timeoutSeconds", { min: 1, max: 120 }))),
            ),
            () => {
                if (c.branches) {
                    return t.div(
                        { rid: "branches", className: "gp-branches" },
                        t.div(
                            { className: "gp-branch yes" },
                            t.div({ className: "gp-branch-title" }, t.i({ className: "ri-check-line" }), "If confident"),
                            t.div({ className: "field-help m-b-5" }, "Every answer reached its \"Confident from\" value."),
                            stepList(s, "then"),
                        ),
                        t.div(
                            { className: "gp-branch no" },
                            t.div({ className: "gp-branch-title" }, t.i({ className: "ri-question-line" }), "Otherwise"),
                            t.div({ className: "field-help m-b-5" }, "For example: notify someone to review."),
                            stepList(s, "else"),
                        ),
                    );
                }
                const n = (s.then?.length || 0) + (s.else?.length || 0);
                if (!n) {
                    return null;
                }
                return t.div(
                    { rid: "orphans" + n, className: "alert warning m-t-sm" },
                    t.span({ className: "m-r-auto" }, `${n} step(s) of the branches won't run while the branches are off.`),
                    t.button({ type: "button", className: "btn sm transparent danger", onclick: () => ((s.then = []), (s.else = [])) }, t.span({ className: "txt" }, "Delete them")),
                );
            },
        ];
    }

    // --- step config forms -----------------------------------------------------------------

    /** Page action flows: the record steps check the API rules of the visitor (on unless turned off). */
    function visitorRules(c, sid) {
        return () =>
            data.flow.trigger_type == "action"
                ? t.div(
                    { rid: "rules", className: "m-b-sm" },
                    t.div(
                        { className: "field" },
                        t.input({ id: "rules_" + sid, type: "checkbox", className: "switch", checked: () => c.checkRules !== false, onchange: (e) => (c.checkRules = e.target.checked) }),
                        t.label({ htmlFor: "rules_" + sid }, t.span({ className: "txt" }, "Check the API rules of the visitor (as the REST API would)")),
                    ),
                    t.div(
                        { className: "field-help" },
                        () => c.checkRules !== false
                            ? "The collection's rules and the field permissions of the visitor's roles apply (guests: not signed in). Refused: the visitor sees \"You don't have access to this record.\""
                            : "Full rights: the step ignores the API rules (like other flows). Check the access yourself with a condition.",
                    ),
                )
                : null;
    }

    function stepConfig(s) {
        const c = s.config;
        const sid = s.id;
        switch (s.type) {
            case "condition":
                return [
                    t.div(
                        { className: "flex gap-10 m-b-sm" },
                        t.span(null, "Continue with"),
                        t.strong(null, "If yes"),
                        t.span(null, "when"),
                        t.div({ className: "field gp-bare gp-inline-select" }, plainSelect(c, "match", [{ value: "all", label: "all rules match" }, { value: "any", label: "any rule matches" }])),
                    ),
                    t.div({ className: "gp-rules-list" }, () =>
                        (c.rules || []).map((rule, i) =>
                            t.div(
                                { rid: rule.id || "rule_" + i, className: "gp-rule-row" },
                                bareExpr(rule, "left", sid, { placeholder: "value (pick dynamic content)" }),
                                t.div({ className: "field gp-bare gp-op" }, plainSelect(rule, "op", operators)),
                                () =>
                                    rule.op == "empty" || rule.op == "not_empty"
                                        ? t.div({ rid: "noright", className: "gp-rule-noright" })
                                        : t.div({ rid: "right_" + (fieldOfPath(rule.left, sid)?.type || "") }, conditionValue(rule, sid)),
                                t.button(
                                    { type: "button", className: "btn sm circle transparent danger", ariaLabel: app.attrs.tooltip("Remove rule"), onclick: () => c.rules.splice(i, 1) },
                                    t.i({ className: "ri-close-line", ariaHidden: true }),
                                ),
                            )
                        )),
                    t.button(
                        { type: "button", className: "btn sm secondary transparent", onclick: () => (c.rules = [...(c.rules || []), { id: randomId(), left: "", op: "eq", right: "" }]) },
                        t.i({ className: "ri-add-line", ariaHidden: true }),
                        t.span({ className: "txt" }, "Add rule"),
                    ),
                    t.div(
                        { className: "gp-branches" },
                        t.div({ className: "gp-branch yes" }, t.div({ className: "gp-branch-title" }, t.i({ className: "ri-check-line" }), "If yes"), stepList(s, "then")),
                        t.div({ className: "gp-branch no" }, t.div({ className: "gp-branch-title" }, t.i({ className: "ri-close-line" }), "If no"), stepList(s, "else")),
                    ),
                ];
            case "loop":
                return [
                    field(
                        { label: "Items (a list)", hint: `The records of a "Get records" step, a list of an API response... Inside the loop, pick the fields of "current item". Run a test to list the lists of the responses.` },
                        choiceExpr(c, "items", sid, { options: () => listOptions(sid), placeholder: "{{steps.records.items}}", emptyLabel: "- Choose a list -" }),
                    ),
                    field({ label: "Max iterations" }, numberInput(c, "max", { min: 1, max: 10000 })),
                    t.div({ className: "gp-branch loop" }, t.div({ className: "gp-branch-title" }, t.i({ className: "ri-repeat-line" }), "For each item"), stepList(s, "steps")),
                ];
            case "delay":
                return field({ label: "Seconds (max 600)", hint: "For longer waits use the delay of a \"Send email\" step or a schedule flow." }, numberInput(c, "seconds", { min: 0, max: 600 }));
            case "stop":
                return [
                    field({ label: "Outcome" }, plainSelect(c, "outcome", [
                        { value: "success", label: "Stop the flow (success)" },
                        { value: "reject", label: "Reject (before-save flows: the change is refused)" },
                    ])),
                    () => (c.outcome == "reject" ? field({ label: "Error message shown to the user" }, exprInput(c, "message", sid)) : null),
                ];
            case "run_flow":
                return [
                    field({ label: "Flow", hint: `Usually a flow with the "Another flow" trigger. Its last step output is returned.` }, select({
                        required: true,
                        placeholder: "- Select flow -",
                        options: () => data.otherFlows.map((f) => ({ value: f.id, label: f.name })),
                        value: () => c.flow,
                        onchange: (v) => (c.flow = v),
                    })),
                    t.label({ className: "gp-label" }, "Payload (available as trigger.payload)"),
                    kvEditor(c, "payload", sid, { addLabel: "Add value" }),
                ];
            case "get_records":
                return [
                    visitorRules(c, sid),
                    t.div({ className: "grid sm" },
                        t.div({ className: "col-lg-6" }, field({ label: "Collection" }, collectionSelect(c, "collection", { list: () => data.allCollections.filter((x) => !x.name.startsWith("_")) }))),
                        t.div({ className: "col-lg-3" }, field({ label: "Sort" }, t.input({ type: "text", className: "txt-code", placeholder: "-created", value: () => c.sort || "", oninput: (e) => (c.sort = e.target.value) }))),
                        t.div({ className: "col-lg-3" }, field({ label: "Limit" }, numberInput(c, "limit", { min: 1, max: 1000 }))),
                    ),
                    field({ label: "Filter", hint: "PocketBase filter syntax, or \"Add a condition\". Dynamic content is passed as safe parameters, e.g. owner = {{trigger.record.id}}" },
                        exprInput(c, "filter", sid, { placeholder: "status = 'active' && total > 100", mono: true })),
                    filterBuilder(c, sid),
                ];
            case "create_record":
                return [
                    visitorRules(c, sid),
                    field({ label: "Collection" }, collectionSelect(c, "collection")),
                    recordFieldsEditor(c, () => c.collection, sid, "Fields left empty are not set."),
                ];
            case "update_record":
                return [
                    visitorRules(c, sid),
                    t.div({ className: "grid sm" },
                        t.div({ className: "col-lg-6" }, field({ label: "Collection" }, collectionSelect(c, "collection"))),
                        t.div({ className: "col-lg-6" }, field({ label: "Record" }, choiceExpr(c, "recordId", sid, { options: () => idOptions(sid, c.collection), placeholder: "{{trigger.record.id}}", emptyLabel: "- Choose the record -" }))),
                    ),
                    recordFieldsEditor(c, () => c.collection, sid, "Only the fields with a value are changed."),
                ];
            case "delete_record":
                return [
                    visitorRules(c, sid),
                    t.div({ className: "grid sm" },
                        t.div({ className: "col-lg-6" }, field({ label: "Collection" }, collectionSelect(c, "collection"))),
                        t.div({ className: "col-lg-6" }, field({ label: "Record" }, choiceExpr(c, "recordId", sid, { options: () => idOptions(sid, c.collection), placeholder: "{{item.id}}", emptyLabel: "- Choose the record -" }))),
                    ),
                ];
            case "set_trigger":
                return () =>
                    data.flow.trigger_type == "record" && data.flow.trigger?.timing == "before"
                        ? recordFieldsEditor(c, () => data.flow.trigger?.collection, sid, "The record being saved is changed before it is written.")
                        : t.div({ className: "alert warning" }, `Only available with a "Record event" trigger set to "Before save".`);
            case "send_email":
                return [
                    t.div({ className: "grid sm" },
                        t.div({ className: "col-lg-6" }, field({ label: "Email template" }, select({
                            required: true,
                            placeholder: "- Select template -",
                            options: () => data.templates.map((tpl) => ({ value: tpl.id, label: tpl.folder ? `${tpl.folder} / ${tpl.name}` : tpl.name })),
                            value: () => c.template,
                            onchange: (v) => {
                                c.template = v;
                                loadTemplateVars(c.template);
                            },
                        }))),
                        t.div({ className: "col-lg-3" }, field({ label: "Delay (minutes)" }, numberInput(c, "delayMinutes", { min: 0 }))),
                        t.div({ className: "col-lg-3 gp-link-col" }, () =>
                            c.template ? t.a({ href: "#/emails/templates/" + c.template, target: "_blank", className: "txt-sm" }, "Edit template ", t.i({ className: "ri-external-link-line" })) : null),
                    ),
                    field({ label: "To", hint: "Address(es), comma separated." }, exprInput(c, "to", sid, { placeholder: "{{.trigger.record.email}}" })),
                    () => {
                        if (!c.template) {
                            return null;
                        }
                        const vars = data.templateVars[c.template];
                        if (!vars) {
                            return t.div({ rid: "loading", className: "txt-hint txt-sm" }, "Loading the template variables...");
                        }
                        if (!c.variables || typeof c.variables != "object") {
                            c.variables = {};
                        }
                        if (!vars.length) {
                            return t.div({ rid: "novars_" + c.template, className: "txt-hint txt-sm" }, "This template has no variables.");
                        }
                        return t.div(
                            { rid: "vars_" + c.template, className: "gp-mapping" },
                            t.div({ className: "field-help m-b-5" }, "Template variables: pick a value for each one."),
                            vars.map((v) =>
                                t.div(
                                    { className: "gp-mapping-row" },
                                    t.div({ className: "gp-mapping-label" }, t.span({ className: "txt-code" }, `{{.${v}}}`)),
                                    bareExpr(c.variables, v, sid),
                                )
                            ),
                        );
                    },
                ];
            case "api_call":
                return apiCallConfig(c, sid);
            case "http_request":
                return [
                    t.div({ className: "grid sm" },
                        t.div({ className: "col-lg-3" }, field({ label: "Method" }, plainSelect(c, "method", ["GET", "POST", "PUT", "PATCH", "DELETE"].map((m) => ({ value: m, label: m }))))),
                        t.div({ className: "col-lg-9" }, field({ label: "URL" }, exprInput(c, "url", sid, { placeholder: "https://example.com/api/hook", mono: true }))),
                    ),
                    t.label({ className: "gp-label" }, "Query parameters"),
                    kvEditor(c, "query", sid, { keyPlaceholder: "name", addLabel: "Add parameter", secrets: true }),
                    t.label({ className: "gp-label" }, "Headers"),
                    kvEditor(c, "headers", sid, { keyPlaceholder: "Header-Name", addLabel: "Add header", secrets: true }),
                    () => (c.method == "GET" ? null : field({ label: "Body", hint: `JSON is detected automatically. Use {{json .some.value}} to insert a value as JSON.` }, exprInput(c, "body", sid, { multiline: true, rows: 5, mono: true, placeholder: '{"id": "{{.trigger.record.id}}"}' }))),
                    t.div({ className: "grid sm" },
                        t.div(
                            { className: "col-lg-6" },
                            field(
                                { label: "Response format" },
                                select({
                                    required: true,
                                    options: responseFormats,
                                    // flows saved before the formats: JSON or text
                                    value: () => c.responseFormat || "legacy",
                                    onchange: (v) => (c.responseFormat = v == "legacy" ? "" : v),
                                }),
                            ),
                        ),
                        t.div({ className: "col-lg-3" }, field({ label: "Retries" }, numberInput(c, "retries", { min: 0, max: 3 }))),
                        t.div({ className: "col-lg-3" }, field({ label: "Timeout (seconds)" }, numberInput(c, "timeoutSeconds", { min: 1, max: 120 }))),
                        t.div({ className: "col-lg-12" }, switchField(c, "failOnError", "Fail the step on error status (4xx/5xx)", "fail_" + sid)),
                    ),
                ];
            case "log":
                return field({ label: "Message" }, exprInput(c, "message", sid, { multiline: true, rows: 2 }));
            case "notify":
                return [
                    field({ label: "To", hint: "\"all\" for every superuser, or superuser emails separated by commas." }, exprInput(c, "to", sid)),
                    field({ label: "Subject" }, exprInput(c, "subject", sid)),
                    field({ label: "Message" }, exprInput(c, "message", sid, { multiline: true, rows: 2 })),
                    field({ label: "Link", hint: "Optional dashboard link, e.g. #/collections?collection=orders&record={{.trigger.record.id}}" }, exprInput(c, "link", sid)),
                ];
            case "set_variables":
                return [
                    t.div({ className: "field-help m-b-5" }, "Each variable is available to the next steps."),
                    kvEditor(c, "vars", sid, { keyPlaceholder: "variable_name", addLabel: "Add variable" }),
                ];
            case "sign_in":
                return () =>
                    data.flow.trigger_type != "action"
                        ? t.div({ rid: "noaction", className: "alert warning" }, `Only available with the "Page action" trigger.`)
                        : t.div(
                            { rid: "signin" },
                            field({ label: "Users collection", hint: "An auth collection with password sign in (superusers can't sign in on pages)." }, collectionSelect(c, "collection", {
                                list: () => data.allCollections.filter((x) => x.type == "auth" && x.name != "_superusers"),
                            })),
                            t.div({ className: "grid sm" },
                                t.div({ className: "col-lg-6" }, field({ label: "Email (or username)" }, exprInput(c, "identity", sid, { mono: true }))),
                                t.div({ className: "col-lg-6" }, field({ label: "Password" }, exprInput(c, "password", sid, { mono: true }))),
                            ),
                            t.div({ className: "field-help" }, "Wrong credentials stop the flow: the visitor goes back to the page with \"Invalid email or password.\" (10 failures per 10 minutes max). Once the flow succeeds, the visitor is signed in. The password never appears in the run log."),
                        );
            case "sign_out":
                return t.div({ className: "field-help" }, "Once the flow succeeds, the visitor is signed out. Pages also have a ready-made \"Sign out\" button that needs no flow.");
            case "redirect":
                return [
                    t.div({ className: "grid sm" },
                        t.div({ className: "col-lg-6" }, field({ label: "Page" }, select({
                            placeholder: "- Choose a page -",
                            options: () => sitePages.list.map((p) => ({ value: sitePath(p), label: `${p.title} (${sitePath(p)})` })),
                            value: () => (sitePages.list.some((p) => sitePath(p) == currentPath(c.url)) ? currentPath(c.url) : ""),
                            onchange: (v) => (c.url = v),
                        }))),
                        t.div({ className: "col-lg-6" }, field({ label: "or a path of the site", hint: "Starts with /. Empty: back to ?next or the page of the button." }, exprInput(c, "url", sid, { mono: true, placeholder: "/thanks?order={{.steps.created.id}}" }))),
                    ),
                    field({ label: "Message shown on that page (optional)" }, exprInput(c, "message", sid, { placeholder: "Thanks, your order is saved." })),
                ];
            case "respond": {
                const mode = () => (c.redirect != null && c.redirect !== "" ? "redirect" : c.format || "json");
                const setMode = (m) => {
                    if (m == "redirect") {
                        c.redirect = c.redirect || "/";
                    } else {
                        c.redirect = "";
                        c.format = m;
                    }
                };
                return [
                    () =>
                        data.flow.trigger?.wait
                            ? t.div({ rid: "waiting" })
                            : t.div({ rid: "nowait", className: "alert warning m-b-sm" }, `Turn on "Wait for the flow and respond" in the trigger: the caller gets this response.`),
                    t.div(
                        { className: "gp-segmented m-b-sm" },
                        [["json", "ri-braces-line", "JSON"], ["text", "ri-text", "Text"], ["html", "ri-html5-line", "HTML"], ["redirect", "ri-share-forward-line", "Redirect"]].map(([value, icon, label]) =>
                            t.label(
                                { className: () => `gp-segment ${mode() == value ? "active" : ""}` },
                                t.input({ type: "radio", name: "respond_" + sid, checked: () => mode() == value, onchange: () => setMode(value) }),
                                t.i({ className: icon, ariaHidden: true }),
                                t.span({ className: "txt" }, label),
                            )
                        ),
                    ),
                    () =>
                        mode() == "redirect"
                            ? t.div(
                                { rid: "redirect" },
                                field({ label: "Address", hint: "A path of the site (/thanks) or an http(s) address. Sent with status 303 (or a 30x status below)." }, exprInput(c, "redirect", sid, { mono: true, placeholder: "/thanks?order={{.steps.created.id}}" })),
                            )
                            : t.div(
                                { rid: "body_" + mode() },
                                t.div({ className: "grid sm" },
                                    t.div({ className: "col-lg-3" }, field({ label: "Status" }, exprInput(c, "status", sid, { mono: true, placeholder: "200" }))),
                                    t.div({ className: "col-lg-9" }, field({ label: "Body", hint: {
                                        json: "JSON text, or one value: {{steps.records.items}}. Use {{json .some.value}} inside JSON text.",
                                        text: "Plain text.",
                                        html: "An HTML page. The values are escaped, and the page can't run scripts.",
                                    }[mode()] }, exprInput(c, "body", sid, { multiline: true, rows: 5, mono: true, placeholder: { json: '{"ok": true, "id": "{{.steps.created.id}}"}', text: "Thanks!", html: "<h1>Thanks {{.trigger.body.name}}</h1>" }[mode()] }))),
                                ),
                            ),
                    t.label({ className: "gp-label" }, "Headers"),
                    kvEditor(c, "headers", sid, { keyPlaceholder: "Header-Name", addLabel: "Add header" }),
                    t.div({ className: "field-help" }, "The last Respond step that runs wins. Cookies and security headers can't be set."),
                ];
            }
            case "ai_decision":
                return aiStepConfig(s);
            case "ai_generate":
                return aiGenerateConfig(s);
            case "script":
                return [
                    field(
                        { label: "JavaScript", hint: "Sandboxed: no files, network or database. Read data.trigger, data.steps, data.last, data.item, data.index; return the step output; console.log goes to the run log. 5s max." },
                        app.components.codeEditor({ language: "js", value: () => c.code || "", oninput: (v) => (c.code = v) }),
                    ),
                ];
        }
        return null;
    }

    // --- step tree ---------------------------------------------------------------------------

    function allKeys() {
        const keys = new Set();
        walkSteps(data.flow.steps, (s) => keys.add(s.key));
        return keys;
    }

    function addStep(parent, prop, index, type) {
        const keys = allKeys();
        const base = stepTypes[type].key;
        let key = base;
        let n = 2;
        while (keys.has(key)) {
            key = base + n++;
        }
        const step = { id: randomId(), key, name: stepTypes[type].label, type, config: defaultConfig(type) };
        if (type == "condition" || type == "ai_decision") {
            step.then = [];
            step.else = [];
        }
        if (type == "ai_decision") {
            step.config.state.values = triggerStateValues();
        }
        if (type == "loop") {
            step.steps = [];
        }
        const list = parent[prop] || (parent[prop] = []);
        list.splice(index, 0, step);
        data.addMenu = "";
    }

    function addMenu(parent, prop, index, menuId) {
        return () => {
            if (data.addMenu != menuId) {
                return t.button(
                    {
                        rid: "btn",
                        type: "button",
                        className: "btn sm secondary transparent gp-add-step",
                        onclick: () => (data.addMenu = menuId),
                    },
                    t.i({ className: "ri-add-line", ariaHidden: true }),
                    t.span({ className: "txt" }, "Add step"),
                );
            }
            const groups = {};
            for (const [type, def] of Object.entries(stepTypes)) {
                (groups[def.group] ||= []).push([type, def]);
            }
            return t.div(
                { rid: "menu", className: "gp-add-menu" },
                t.div(
                    { className: "flex m-b-sm" },
                    t.strong({ className: "m-r-auto" }, "Choose a step"),
                    t.button({ type: "button", className: "btn sm circle transparent secondary", onclick: () => (data.addMenu = "") }, t.i({ className: "ri-close-line" })),
                ),
                Object.entries(groups).map(([group, items]) =>
                    t.div(
                        { className: "gp-add-group" },
                        t.div({ className: "gp-picker-title" }, group),
                        t.div(
                            { className: "gp-add-grid" },
                            items.map(([type, def]) =>
                                t.button(
                                    {
                                        type: "button",
                                        className: "gp-add-item",
                                        hidden: (type == "set_trigger" && !(data.flow.trigger_type == "record" && data.flow.trigger?.timing == "before")) ||
                                            (type == "api_call" && !globalThis.gpAPIs) ||
                                            (stepTypes[type].group == "Pages" && data.flow.trigger_type != "action") ||
                                            (stepTypes[type].group == "Webhook" && data.flow.trigger_type != "webhook"),
                                        onclick: () => addStep(parent, prop, index, type),
                                    },
                                    t.i({ className: def.icon, ariaHidden: true }),
                                    t.span({ className: "txt" }, t.strong(null, def.label), t.small({ className: "txt-hint" }, def.hint)),
                                )
                            ),
                        ),
                    )
                ),
            );
        };
    }

    function stepCard(s, parent, prop) {
        const list = () => parent[prop];
        const def = stepTypes[s.type] || { label: s.type, icon: "ri-question-line" };
        if (!s.config) {
            s.config = {};
        }

        function move(delta) {
            const l = list();
            const i = l.indexOf(s);
            const j = i + delta;
            if (i < 0 || j < 0 || j >= l.length) {
                return;
            }
            const [item] = l.splice(i, 1);
            l.splice(j, 0, item);
        }

        function duplicate() {
            const l = list();
            const copy = JSON.parse(JSON.stringify(s));
            const keys = allKeys();
            walkSteps([copy], (x) => {
                x.id = randomId();
                let key = x.key + "_copy";
                let n = 2;
                while (keys.has(key)) {
                    key = x.key + "_copy" + n++;
                }
                keys.add(key);
                x.key = key;
            });
            l.splice(l.indexOf(s) + 1, 0, copy);
        }

        function removeStep() {
            const doRemove = () => {
                const l = list();
                l.splice(l.indexOf(s), 1);
            };
            const nested = (s.then?.length || 0) + (s.else?.length || 0) + (s.steps?.length || 0);
            if (nested) {
                app.modals.confirm(`Delete "${s.name || def.label}" and its ${nested} nested step(s)?`, doRemove);
            } else {
                doRemove();
            }
        }

        return t.div(
            { rid: s.id, className: () => `gp-step gp-step-${s.type} ${s.disabled ? "disabled" : ""}`, "html-data-step": s.key },
            t.div(
                { className: "gp-step-header" },
                t.button(
                    {
                        type: "button",
                        className: "gp-step-toggle",
                        onclick: () => (data.collapsed[s.id] = !data.collapsed[s.id]),
                    },
                    t.i({ className: () => (data.collapsed[s.id] ? "ri-arrow-right-s-line" : "ri-arrow-down-s-line"), ariaHidden: true }),
                    t.span({ className: "gp-step-icon" }, t.i({ className: def.icon, ariaHidden: true })),
                    t.span({ className: "gp-step-title" }, () => s.name || def.label),
                    t.span({ className: "label sm gp-step-key", title: "Use {{.steps." + s.key + "...}} in later steps" }, () => s.key),
                    () => (s.disabled ? t.span({ className: "label sm warning" }, "disabled") : null),
                ),
                t.nav(
                    { className: "gp-step-actions" },
                    t.button({ type: "button", className: "btn xs circle transparent secondary", ariaLabel: app.attrs.tooltip("Move up"), onclick: () => move(-1) }, t.i({ className: "ri-arrow-up-line" })),
                    t.button({ type: "button", className: "btn xs circle transparent secondary", ariaLabel: app.attrs.tooltip("Move down"), onclick: () => move(1) }, t.i({ className: "ri-arrow-down-line" })),
                    t.button({ type: "button", className: "btn xs circle transparent secondary", ariaLabel: app.attrs.tooltip("Duplicate"), onclick: duplicate }, t.i({ className: "ri-file-copy-line" })),
                    t.button(
                        { type: "button", className: "btn xs circle transparent secondary", ariaLabel: app.attrs.tooltip("Enable / disable"), onclick: () => (s.disabled = !s.disabled) },
                        t.i({ className: () => (s.disabled ? "ri-play-line" : "ri-pause-line") }),
                    ),
                    t.button({ type: "button", className: "btn xs circle transparent danger", ariaLabel: app.attrs.tooltip("Delete"), onclick: removeStep }, t.i({ className: "ri-delete-bin-7-line" })),
                ),
            ),
            t.div(
                { className: "gp-step-body", hidden: () => !!data.collapsed[s.id] },
                t.div(
                    { className: "grid sm" },
                    t.div({ className: "col-lg-8" }, field({ label: "Step name" }, t.input({ type: "text", value: () => s.name || "", oninput: (e) => (s.name = e.target.value) }))),
                    t.div(
                        { className: "col-lg-4" },
                        field(
                            { label: "Key", hint: "Used to reference the output." },
                            t.input({
                                type: "text",
                                className: "txt-code",
                                value: () => s.key,
                                onchange: (e) => {
                                    const v = e.target.value.trim().replace(/[^A-Za-z0-9_]/g, "_");
                                    if (!v || (v != s.key && allKeys().has(v))) {
                                        e.target.value = s.key;
                                        app.toasts.error("The key must be unique and non-empty.");
                                        return;
                                    }
                                    s.key = v;
                                },
                            }),
                        ),
                    ),
                ),
                t.div({ className: "gp-step-config" }, ...[].concat(stepConfig(s) ?? [])),
                t.div(
                    { className: "gp-step-footer" },
                    t.div(
                        { className: "field" },
                        t.input({ id: "coe_" + s.id, type: "checkbox", checked: () => !!s.continueOnError, onchange: (e) => (s.continueOnError = e.target.checked) }),
                        t.label({ htmlFor: "coe_" + s.id, className: "txt-sm" }, "Continue the flow if this step fails"),
                    ),
                ),
            ),
        );
    }

    /** Renders parent[prop] (a step list) with add buttons. */
    function stepList(parent, prop) {
        if (!Array.isArray(parent[prop])) {
            parent[prop] = [];
        }
        const listId = (parent.id || "root") + ":" + prop;
        return t.div(
            { className: "gp-steps" },
            () => {
                const list = parent[prop] || [];
                const nodes = [];
                list.forEach((s, i) => {
                    nodes.push(t.div({ rid: "add_" + s.id, className: "gp-insert" }, addMenu(parent, prop, i, listId + ":" + i)));
                    nodes.push(stepCard(s, parent, prop));
                });
                nodes.push(t.div({ rid: "add_end", className: "gp-insert end" }, addMenu(parent, prop, list.length, listId + ":end")));
                return nodes;
            },
        );
    }

    // --- trigger card ------------------------------------------------------------------------

    function webhookURL() {
        const cfg = data.flow.trigger || {};
        const byToken = !cfg.access || cfg.access == "token";
        if (!data.savedId || (byToken && !cfg.token)) {
            return "";
        }
        // the saved address (a new one works once saved)
        const slug = data.savedSlug;
        // buildURL may return a relative ("../api") or absolute URL: normalize it
        const url = new URL(app.pb.buildURL(slug ? "/hooks/" + slug : "/api/flows/webhook/" + data.savedId), window.location.href);
        if (byToken) {
            url.searchParams.set("token", cfg.token);
        }
        return url.href;
    }

    function curlExample(url) {
        const cfg = data.flow.trigger || {};
        const method = !cfg.method || cfg.method == "ANY" ? "POST" : cfg.method;
        const lines = [`curl -X ${method} '${url}'`];
        if (cfg.access == "users" || cfg.access == "roles") {
            lines.push(`  -H 'Authorization: <the token of a signed-in user>'`);
        }
        if (method != "GET") {
            lines.push(`  -H 'Content-Type: application/json' -d '{"example": true}'`);
        }
        return lines.join(" \\\n");
    }

    function triggerCard() {
        return t.div(
            { className: "gp-step gp-trigger" },
            t.div(
                { className: "gp-step-header" },
                t.div(
                    { className: "gp-step-toggle" },
                    t.span({ className: "gp-step-icon trigger" }, t.i({ className: () => triggerTypes[data.flow.trigger_type]?.icon, ariaHidden: true })),
                    t.span({ className: "gp-step-title" }, "Trigger"),
                    t.span({ className: "txt-hint txt-sm" }, () => describeTrigger(data.flow)),
                ),
            ),
            t.div(
                { className: "gp-step-body" },
                t.div(
                    { className: "gp-segmented m-b-sm" },
                    Object.entries(triggerTypes).map(([type, def]) =>
                        t.label(
                            { className: () => `gp-segment ${data.flow.trigger_type == type ? "active" : ""}`, title: def.hint },
                            t.input({
                                type: "radio",
                                name: "trigger_type",
                                checked: () => data.flow.trigger_type == type,
                                onchange: () => {
                                    data.flow.trigger_type = type;
                                    const defaults = {
                                        record: { collection: "", events: ["create"], timing: "after", filter: "" },
                                        manual: { collection: "" },
                                        webhook: { method: "POST", wait: false },
                                        schedule: { cron: "0 9 * * *" },
                                        flow: {},
                                        action: { access: "public" },
                                    };
                                    data.flow.trigger = defaults[type];
                                },
                            }),
                            t.i({ className: def.icon, ariaHidden: true }),
                            t.span({ className: "txt" }, def.label),
                        )
                    ),
                ),
                () => {
                    const cfg = data.flow.trigger;
                    switch (data.flow.trigger_type) {
                        case "record":
                            return t.div(
                                { rid: "record", className: "grid sm" },
                                t.div({ className: "col-lg-6" }, field({ label: "Collection", name: "trigger" }, collectionSelect(cfg, "collection"))),
                                t.div(
                                    { className: "col-lg-6" },
                                    t.label({ className: "gp-label" }, "Events"),
                                    t.div(
                                        { className: "flex gap-15 gp-events" },
                                        ["create", "update", "delete"].map((ev) =>
                                            t.div(
                                                { className: "field" },
                                                t.input({
                                                    id: "tev_" + ev,
                                                    type: "checkbox",
                                                    checked: () => (cfg.events || []).includes(ev),
                                                    onchange: (e) => (cfg.events = e.target.checked ? [...(cfg.events || []), ev] : (cfg.events || []).filter((x) => x != ev)),
                                                }),
                                                t.label({ htmlFor: "tev_" + ev }, ev),
                                            )
                                        ),
                                    ),
                                ),
                                t.div(
                                    { className: "col-lg-12" },
                                    t.label({ className: "gp-label" }, "When"),
                                    t.div(
                                        { className: "gp-segmented" },
                                        [
                                            ["after", "ri-time-line", "After save", "runs in the background"],
                                            ["before", "ri-shield-check-line", "Before save", "can change or reject the record"],
                                        ].map(([value, icon, label, hint]) =>
                                            t.label(
                                                { className: () => `gp-segment ${(cfg.timing || "after") == value ? "active" : ""}` },
                                                t.input({ type: "radio", name: "timing", checked: () => (cfg.timing || "after") == value, onchange: () => (cfg.timing = value) }),
                                                t.i({ className: icon, ariaHidden: true }),
                                                t.span({ className: "txt" }, label, t.small({ className: "txt-hint" }, " - " + hint)),
                                            )
                                        ),
                                    ),
                                ),
                                () =>
                                    (cfg.timing || "after") == "after"
                                        ? t.div(
                                            { rid: "filter", className: "col-lg-12" },
                                            field(
                                                { label: "Filter (optional)", hint: "Only run for records matching this PocketBase filter." },
                                                t.input({ type: "text", className: "txt-code", placeholder: "status = 'paid'", value: () => cfg.filter || "", oninput: (e) => (cfg.filter = e.target.value) }),
                                            ),
                                        )
                                        : t.div(
                                            { rid: "beforehint", className: "col-lg-12" },
                                            t.div(
                                                { className: "alert info" },
                                                `Runs during the save (15s max). Use a Condition step to filter, "Update triggering record" to change values and "Stop / reject" to refuse the change with an error message.`,
                                            ),
                                        ),
                            );
                        case "manual":
                            return t.div(
                                { rid: "manual" },
                                field(
                                    { label: "Show a \"Run flow\" button on the records of", hint: "The selected record is available as trigger.record. Leave empty to run the flow only from here." },
                                    collectionSelect(cfg, "collection", { placeholder: "No collection (run from the Flows page)", required: false }),
                                ),
                            );
                        case "webhook":
                            return t.div(
                                { rid: "webhook" },
                                t.div({ className: "grid sm" },
                                    t.div({ className: "col-lg-4" }, field({ label: "Method" }, plainSelect(cfg, "method", ["POST", "GET", "PUT", "PATCH", "DELETE"].map((m) => ({ value: m, label: m })).concat([{ value: "ANY", label: "Any" }])))),
                                    t.div({ className: "col-lg-8 gp-switch-col" }, switchField(cfg, "wait", "Wait for the flow and respond (a Respond step, else the last step output as JSON)", "wh_wait")),
                                ),
                                t.label({ className: "gp-label" }, "Who can call it"),
                                t.div(
                                    { className: "gp-segmented m-b-sm" },
                                    [
                                        ["token", "ri-key-2-line", "Secret token", "in the address or a header"],
                                        ["public", "ri-global-line", "Public", "anyone"],
                                        ["users", "ri-user-line", "Signed-in users", ""],
                                        ["roles", "ri-shield-user-line", "Roles", ""],
                                    ].map(([value, icon, label, hint]) =>
                                        t.label(
                                            { className: () => `gp-segment ${(cfg.access || "token") == value ? "active" : ""}` },
                                            t.input({ type: "radio", name: "wh_access", checked: () => (cfg.access || "token") == value, onchange: () => (cfg.access = value) }),
                                            t.i({ className: icon, ariaHidden: true }),
                                            t.span({ className: "txt" }, label, hint ? t.small({ className: "txt-hint" }, " - " + hint) : null),
                                        )
                                    ),
                                ),
                                () =>
                                    cfg.access == "roles"
                                        ? t.div({ rid: "roles" }, field({ label: "Roles", name: "trigger" }, multiSelect({
                                            placeholder: "Select roles",
                                            options: () => roleList.list.map((r) => ({ value: r.id, label: r.name })),
                                            value: () => cfg.roles || [],
                                            onchange: (v) => (cfg.roles = v),
                                        })))
                                        : null,
                                () =>
                                    cfg.access && cfg.access != "token"
                                        ? t.div(
                                            { rid: "callerhelp", className: "field-help m-b-sm" },
                                            "The signed-in caller is trigger.user: a PocketBase auth token in the Authorization header, or the session of the visitor for calls from the pages. The record steps check the caller's API rules, and a visitor can call it 60 times a minute.",
                                        )
                                        : t.div({ rid: "callerhelp" }),
                                field(
                                    { label: "Address (optional)", hint: "A readable address: /hooks/new-order instead of /api/flows/webhook/<id>. Lowercase letters, digits and dashes." },
                                    t.div(
                                        { className: "gp-prefixed" },
                                        t.span({ className: "txt-hint txt-code" }, "/hooks/"),
                                        t.input({
                                            type: "text",
                                            className: "txt-code",
                                            placeholder: "new-order",
                                            value: () => cfg.slug || "",
                                            oninput: (e) => (cfg.slug = e.target.value.toLowerCase().replace(/[^a-z0-9-]/g, "-")),
                                        }),
                                    ),
                                ),
                                () => {
                                    const url = webhookURL();
                                    if (!url) {
                                        return t.div({ rid: "nourl", className: "alert info" }, "Save the flow to get its webhook URL.");
                                    }
                                    const byToken = !cfg.access || cfg.access == "token";
                                    return t.div(
                                        { rid: "url" + url, className: "gp-panel" },
                                        t.div({ className: "gp-label" }, byToken ? "Webhook URL (keep it secret: the token authorizes the call)" : "Webhook URL"),
                                        t.div(
                                            { className: "flex gap-5 gp-url-row" },
                                            t.code({ className: "gp-url flex-fill" }, url),
                                            t.button({ type: "button", className: "btn sm secondary", onclick: () => { app.utils.copyToClipboard(url); app.toasts.success("Copied."); } }, "Copy"),
                                            byToken
                                                ? t.button(
                                                    {
                                                        type: "button",
                                                        className: "btn sm transparent danger",
                                                        onclick: () => app.modals.confirm("Generate a new token? The current URL stops working once you save.", () => (cfg.token = "")),
                                                    },
                                                    "New token",
                                                )
                                                : null,
                                        ),
                                        t.details(
                                            { className: "gp-curl" },
                                            t.summary(null, "Example with curl"),
                                            t.pre({ className: "txt-code" }, curlExample(url)),
                                        ),
                                        t.div({ className: "field-help" }, "The body (JSON, form fields or text), query and headers are available as trigger.body, trigger.query and trigger.headers." + (byToken ? " The token can also be sent in an X-Flow-Token header." : "")),
                                    );
                                },
                            );
                        case "schedule":
                            return t.div(
                                { rid: "schedule", className: "grid sm" },
                                t.div({ className: "col-lg-6" }, field({ label: "Cron expression (UTC)" }, t.input({ type: "text", className: "txt-code", value: () => cfg.cron || "", oninput: (e) => (cfg.cron = e.target.value) }))),
                                t.div({ className: "col-lg-6" }, field({ label: "Presets" }, select({
                                    placeholder: "Choose...",
                                    options: cronPresets,
                                    value: () => "", // an action, not a value: always shows the placeholder
                                    onchange: (v) => (cfg.cron = v),
                                }))),
                            );
                        case "action":
                            return t.div(
                                { rid: "action" },
                                t.label({ className: "gp-label" }, "Who can run it"),
                                t.div(
                                    { className: "gp-segmented m-b-sm" },
                                    [
                                        ["public", "ri-global-line", "Public", "everyone, signed in or not"],
                                        ["users", "ri-user-line", "Signed-in users", ""],
                                        ["roles", "ri-shield-user-line", "Roles", ""],
                                    ].map(([value, icon, label, hint]) =>
                                        t.label(
                                            { className: () => `gp-segment ${(cfg.access || "public") == value ? "active" : ""}` },
                                            t.input({ type: "radio", name: "access", checked: () => (cfg.access || "public") == value, onchange: () => (cfg.access = value) }),
                                            t.i({ className: icon, ariaHidden: true }),
                                            t.span({ className: "txt" }, label, hint ? t.small({ className: "txt-hint" }, " - " + hint) : null),
                                        )
                                    ),
                                ),
                                () =>
                                    cfg.access == "roles"
                                        ? t.div({ rid: "roles" }, field({ label: "Roles", name: "trigger" }, multiSelect({
                                            placeholder: "Select roles",
                                            options: () => roleList.list.map((r) => ({ value: r.id, label: r.name })),
                                            value: () => cfg.roles || [],
                                            onchange: (v) => (cfg.roles = v),
                                        })))
                                        : null,
                                t.div(
                                    { className: "alert info m-t-sm" },
                                    `Run from the pages: in the page builder, use the "Action button", "Action form" or "Login form" blocks and choose this flow. `,
                                    `The form fields are available as trigger.form.<name>, the signed-in visitor as trigger.user, the record of the button as trigger.record. `,
                                    `Use "Sign in", "Sign out" and "Redirect to page" steps to decide what happens next; a "Stop / reject" step shows its message on the page.`,
                                ),
                            );
                        default:
                            return t.div({ rid: "flow", className: "alert info" }, `This flow runs when another flow calls it with a "Run another flow" step. The values it passes are available as trigger.payload.`);
                    }
                },
            ),
        );
    }

    // --- test run ------------------------------------------------------------------------------

    const runData = store({ trigger: "{}", records: [], recordId: "", isRunning: false, result: null });

    async function openRun() {
        if (data.hasChanges || !data.savedId) {
            app.toasts.error("Save the flow before running it.");
            return;
        }
        runData.result = null;
        const f = data.flow;
        const cfg = f.trigger || {};
        runData.records = [];
        runData.recordId = "";
        if ((f.trigger_type == "manual" || f.trigger_type == "record") && cfg.collection) {
            try {
                const res = await app.pb.collection(cfg.collection).getList(1, 50, { sort: "-@rowid", requestKey: null });
                runData.records = res.items;
                // the record of the last test first
                const last = data.sample?.data?.trigger?.record?.id;
                runData.recordId = res.items.some((r) => r.id == last) ? last : res.items[0]?.id || "";
            } catch (err) {
                handleError(err);
            }
        }
        const examples = {
            webhook: { method: "POST", query: {}, headers: {}, body: { example: true } },
            schedule: { time: new Date().toISOString() },
            flow: { payload: {} },
            action: { form: { email: "someone@example.com", password: "" }, user: null, page: "/login", next: "" },
        };
        // the trigger data of the last test, or an example
        const lastTrigger = data.sample?.data?.trigger;
        runData.trigger = JSON.stringify(lastTrigger && Object.keys(lastTrigger).length ? lastTrigger : examples[f.trigger_type] || {}, null, 2);
        app.modals.open(runModal);
    }

    async function run() {
        const f = data.flow;
        runData.isRunning = true;
        runData.result = null;
        try {
            let body;
            if (f.trigger_type == "manual") {
                body = { wait: true, collection: f.trigger?.collection || "", recordIds: runData.recordId ? [runData.recordId] : [] };
            } else if (f.trigger_type == "record") {
                const rec = runData.records.find((r) => r.id == runData.recordId);
                body = { wait: true, trigger: { event: (f.trigger.events || ["create"])[0], collection: f.trigger.collection, record: rec || {} } };
            } else {
                body = { wait: true, trigger: JSON.parse(runData.trigger || "{}") };
            }
            // the run keeps its data: the test data of the picker and the previews
            body.sample = true;
            const res = await api("/api/flows/run/" + data.savedId, { method: "POST", body });
            runData.result = res.runs?.[0] || null;
            loadSample();
        } catch (err) {
            handleError(err);
        }
        runData.isRunning = false;
    }

    const runModal = t.div(
        { className: "modal popup gp-run-modal" },
        t.header({ className: "modal-header" }, t.h5({ className: "m-auto" }, "Run the flow")),
        t.div(
            { className: "modal-content" },
            () => {
                const f = data.flow;
                if (f.trigger_type == "record" && f.trigger?.timing == "before") {
                    return t.div({ rid: "before", className: "alert warning m-b-sm" }, "Test run outside of a save: \"Update triggering record\" steps will fail.");
                }
            },
            () => {
                if (runData.records.length) {
                    return field(
                        { label: data.flow.trigger_type == "record" ? "Use this record as the trigger record" : "Run on this record" },
                        select({
                            required: true,
                            options: runData.records.map((r) => ({ value: r.id, label: `${r.name || r.title || r.email || r.number || ""} (${r.id})` })),
                            value: () => runData.recordId,
                            onchange: (v) => (runData.recordId = v),
                        }),
                    );
                }
                if (data.flow.trigger_type == "manual" || data.flow.trigger_type == "record") {
                    return t.p({ rid: "norecords", className: "txt-hint" }, "The flow runs without a record.");
                }
                return field(
                    { label: "Trigger data (JSON)" },
                    t.textarea({ className: "txt-code", rows: 8, value: () => runData.trigger, oninput: (e) => (runData.trigger = e.target.value) }),
                );
            },
            () => {
                const r = runData.result;
                if (!r) {
                    return null;
                }
                return t.div(
                    { rid: "result_" + r.runId, className: `alert ${runStatusClass[r.status] || ""} m-t-sm` },
                    t.div({ className: "flex gap-10" }, t.strong(null, "Run " + r.status), t.a({ href: `#/flows/${data.savedId}/runs`, onclick: () => app.modals.close(runModal, true) }, "See the run details")),
                    r.error ? t.div({ className: "gp-pre m-t-5" }, r.error) : null,
                r.action
                    ? t.div(
                        { className: "m-t-5" },
                        r.action.signIn ? "Signs the visitor in. " : "",
                        r.action.signOut ? "Signs the visitor out. " : "",
                        r.action.redirect ? `Opens ${r.action.redirect}. ` : "",
                        r.action.message ? `Shows "${r.action.message}".` : "",
                    )
                    : null,
                    r.output !== undefined && r.output !== null ? t.div({ className: "gp-pre m-t-5" }, JSON.stringify(r.output, null, 2).slice(0, 2000)) : null,
                );
            },
        ),
        t.footer(
            { className: "modal-footer" },
            t.button({ type: "button", className: "btn transparent secondary m-r-auto", onclick: () => app.modals.close(runModal) }, "Close"),
            t.button(
                { type: "button", className: () => `btn expanded ${runData.isRunning ? "loading" : ""}`, disabled: () => runData.isRunning, onclick: run },
                t.i({ className: "ri-play-line", ariaHidden: true }),
                t.span({ className: "txt" }, "Run now"),
            ),
        ),
    );

    // where the values of the picker and the previews come from
    function testDataBar() {
        return t.div({ className: "gp-testdata" }, () => {
            const smp = data.sample;
            if (!data.savedId) {
                return t.span({ rid: "new", className: "txt-hint" }, t.i({ className: "ri-flask-line", ariaHidden: true }), " Save the flow, then run a test: each value shows its result with the test data.");
            }
            return [
                smp
                    ? t.span(
                        { rid: "has_" + smp.run + smp.status },
                        t.i({ className: "ri-flask-line", ariaHidden: true }),
                        " Test data: run of ",
                        t.strong(null, formatDate(smp.created)),
                        smp.status && smp.status != "success" ? t.span({ className: "label sm warning m-l-5" }, smp.status) : null,
                    )
                    : t.span({ rid: "none", className: "txt-hint" }, t.i({ className: "ri-flask-line", ariaHidden: true }), " No test data yet: run a test to see the values of each step."),
                t.button(
                    { rid: smp ? "again" : "first", type: "button", className: "btn xs outline m-l-auto", onclick: openRun },
                    t.i({ className: "ri-play-line", ariaHidden: true }),
                    t.span({ className: "txt" }, smp ? "Run again" : "Run a test"),
                ),
            ];
        });
    }

    // --- runs history --------------------------------------------------------------------------

    const runView = store({ open: null, expanded: {} });

    async function loadRuns() {
        if (!data.savedId) {
            return;
        }
        try {
            const res = await app.pb.collection("flow_runs").getList(1, 50, {
                filter: app.pb.filter("flow = {:f}", { f: data.savedId }),
                sort: "-created",
                fields: "id,status,trigger_type,error,duration_ms,created",
                requestKey: "gpFlowRuns",
            });
            data.runs = res.items;
        } catch (err) {
            handleError(err);
        }
    }

    async function openRunDetails(r) {
        try {
            runView.open = await app.pb.collection("flow_runs").getOne(r.id, { requestKey: null });
            runView.expanded = {};
            app.modals.open(runDetailsModal);
        } catch (err) {
            handleError(err);
        }
    }

    function jsonBlock(value) {
        return app.components.codeBlock({ language: "json", value: JSON.stringify(value, null, 2) });
    }

    const runDetailsModal = t.div(
        { className: "modal popup lg gp-run-details" },
        t.header({ className: "modal-header" }, t.h5({ className: "m-auto" }, () => (runView.open ? `Run ${runView.open.status}` : "Run"))),
        t.div(
            { className: "modal-content" },
            () => {
                const r = runView.open;
                if (!r) {
                    return null;
                }
                return [
                    t.div(
                        { className: "flex gap-10 m-b-sm" },
                        t.span({ className: `label ${runStatusClass[r.status] || ""}` }, r.status),
                        t.span({ className: "txt-hint" }, formatDate(r.created)),
                        t.span({ className: "txt-hint" }, `${r.duration_ms} ms`),
                    ),
                    r.error ? t.div({ className: "alert danger m-b-sm gp-pre" }, r.error) : null,
                    t.details({ className: "gp-details m-b-sm" }, t.summary(null, "Trigger data"), jsonBlock(r.input)),
                    t.div({ className: "gp-label" }, "Steps"),
                    t.div(
                        { className: "gp-run-steps" },
                        (r.steps || []).length
                            ? r.steps.map((s, i) =>
                                t.div(
                                    { className: `gp-run-step ${s.status}`, style: `margin-left: ${(s.depth || 0) * 20}px` },
                                    t.button(
                                        { type: "button", className: "gp-run-step-head", onclick: () => (runView.expanded[i] = !runView.expanded[i]) },
                                        t.i({ className: s.status == "success" ? "ri-checkbox-circle-fill txt-success" : s.status == "failed" ? "ri-close-circle-fill txt-danger" : "ri-indeterminate-circle-line", ariaHidden: true }),
                                        t.i({ className: stepTypes[s.type]?.icon || "ri-question-line", ariaHidden: true }),
                                        t.strong(null, s.name || s.key),
                                        s.index !== undefined && s.index !== null ? t.span({ className: "label sm" }, `item ${s.index}`) : null,
                                        t.span({ className: "txt-hint txt-sm m-l-auto" }, `${s.durationMs} ms`),
                                    ),
                                    s.error ? t.div({ className: "txt-danger txt-sm gp-pre" }, s.error) : null,
                                    () =>
                                        runView.expanded[i]
                                            ? t.div(
                                                { rid: "detail", className: "grid sm m-t-5" },
                                                t.div({ className: "col-lg-6" }, t.div({ className: "gp-label" }, "Input"), jsonBlock(s.input ?? null)),
                                                t.div({ className: "col-lg-6" }, t.div({ className: "gp-label" }, "Output"), jsonBlock(s.output ?? null)),
                                            )
                                            : null,
                                )
                            )
                            : t.div({ className: "txt-hint" }, "No step was executed."),
                    ),
                    t.div({ className: "gp-label m-t-sm" }, "Output (last step)"),
                    jsonBlock(r.output ?? null),
                ];
            },
        ),
        t.footer({ className: "modal-footer" }, t.button({ type: "button", className: "btn transparent secondary m-r-auto", onclick: () => app.modals.close(runDetailsModal) }, "Close")),
    );

    function runsPanel() {
        return t.div(
            { rid: "runs" },
            t.div(
                { className: "flex m-b-sm" },
                t.span({ className: "txt-hint m-r-auto" }, "The last 50 runs (live)."),
                app.components.refreshButton({ className: "btn sm transparent secondary circle", onclick: loadRuns }),
            ),
            t.div(
                { className: "gp-table-wrap" },
                t.table(
                    null,
                    t.thead(null, t.tr(null, t.th(null, "Status"), t.th(null, "Date"), t.th(null, "Duration"), t.th(null, "Error"))),
                    t.tbody(null, () =>
                        data.runs.length
                            ? data.runs.map((r) =>
                                t.tr(
                                    { rid: r.id + r.status, className: "gp-clickable", onclick: () => openRunDetails(r) },
                                    t.td(null, t.span({ className: `label sm ${runStatusClass[r.status] || ""}` }, r.status)),
                                    t.td({ className: "txt-sm" }, formatDate(r.created)),
                                    t.td({ className: "txt-hint txt-sm" }, `${r.duration_ms} ms`),
                                    t.td({ className: "txt-danger txt-sm" }, (r.error || "").slice(0, 140)),
                                )
                            )
                            : t.tr({ rid: "empty" }, t.td({ colSpan: 4, className: "txt-center txt-hint" }, "No runs yet."))),
                ),
            ),
        );
    }

    // --- page ------------------------------------------------------------------------------------

    let unsubscribeRuns;
    function onKeydown(e) {
        if ((e.ctrlKey || e.metaKey) && e.key == "s" && data.tab == "editor") {
            e.preventDefault();
            save();
        }
    }

    load();

    return page(
        {
            pbEvent: "gpPageFlowEditor",
            className: "gp-page-flow-editor",
            sidebar: flowsSidebar(),
            wide: true,
            breadcrumbs: [t.a({ href: "#/flows" }, "Flows"), t.span(null, () => data.flow.name || (isNew ? "New" : "..."))],
            buttons: [
                t.button(
                    { type: "button", className: "btn transparent secondary", hidden: () => !data.savedId, onclick: remove, ariaLabel: app.attrs.tooltip("Delete flow") },
                    t.i({ className: "ri-delete-bin-7-line", ariaHidden: true }),
                ),
                t.button(
                    { type: "button", className: "btn outline", hidden: () => !data.savedId, onclick: openRun },
                    t.i({ className: "ri-play-line", ariaHidden: true }),
                    t.span({ className: "txt" }, "Test run"),
                ),
                t.button(
                    {
                        type: "button",
                        className: () => `btn expanded-lg ${data.isSaving ? "loading" : ""}`,
                        disabled: () => data.isSaving || (!!data.savedId && !data.hasChanges),
                        onclick: save,
                    },
                    t.span({ className: "txt" }, () => (data.savedId ? "Save changes" : "Create flow")),
                ),
            ],
        },
        t.div(
            {
                className: "gp-flow",
                onmount: async () => {
                    window.addEventListener("keydown", onKeydown);
                    document.addEventListener("visibilitychange", onVisible);
                    if (!isNew) {
                        try {
                            unsubscribeRuns = await app.pb.collection("flow_runs").subscribe("*", (e) => {
                                if (e.record.flow == data.savedId && data.tab == "runs") {
                                    loadRuns();
                                }
                            });
                        } catch (err) {
                            console.warn("flow runs realtime:", err);
                        }
                    }
                },
                onunmount: () => {
                    window.removeEventListener("keydown", onKeydown);
                    document.removeEventListener("visibilitychange", onVisible);
                    unsubscribeRuns?.();
                },
            },
            () => {
                if (data.isLoading) {
                    return t.div({ rid: "loading", className: "block txt-center" }, t.span({ className: "loader lg" }));
                }
                return [
                    t.div(
                        { rid: "head", className: "grid sm m-b-sm" },
                        t.div({ className: "col-lg-6" }, field({ label: "Name", name: "name" }, t.input({ id: "name", name: "name", type: "text", required: true, value: () => data.flow.name, oninput: (e) => (data.flow.name = e.target.value) }))),
                        t.div({ className: "col-lg-4" }, field({ label: "Description", name: "description" }, t.input({ type: "text", name: "description", value: () => data.flow.description || "", oninput: (e) => (data.flow.description = e.target.value) }))),
                        t.div({ className: "col-lg-2 gp-switch-col" }, switchField(data.flow, "active", "Active", "flow_active")),
                    ),
                    t.div(
                        { rid: "tabs", className: "gp-tabs m-b-sm" },
                        t.button({ type: "button", className: () => `gp-tab ${data.tab == "editor" ? "active" : ""}`, onclick: () => (data.tab = "editor") }, t.i({ className: "ri-flow-chart" }), "Editor"),
                        t.button(
                            {
                                type: "button",
                                className: () => `gp-tab ${data.tab == "runs" ? "active" : ""}`,
                                disabled: () => !data.savedId,
                                onclick: () => {
                                    data.tab = "runs";
                                    loadRuns();
                                },
                            },
                            t.i({ className: "ri-history-line" }),
                            "Runs",
                        ),
                    ),
                    t.div(
                        { rid: "body" },
                        () =>
                            data.tab == "runs"
                                ? runsPanel()
                                : t.div(
                                    { rid: "editor", className: "gp-flow-canvas" },
                                    testDataBar(),
                                    triggerCard(),
                                    t.div({ className: "gp-connector" }),
                                    stepList(data.flow, "steps"),
                                ),
                    ),
                ];
            },
        ),
        runModal,
        runDetailsModal,
    );
}
