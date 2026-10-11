// Shared helpers for the PocketBasePlus dashboard pages.

// collections managed by PocketBasePlus itself (never emailed / managed by RBAC)
export const internalCollections = new Set([
    "roles",
    "permissions",
    "rbac_collections",
    "email_templates",
    "email_automations",
    "email_messages",
    "flows",
    "flow_runs",
    "dashboards",
    "dashboard_panels",
    "collection_groups",
    "revisions",
    "comments",
    "notifications",
    "saved_views",
    "forms",
    "ai_providers",
    "ai_models",
    "email_unsubscribes",
    "ai_settings",
    "ai_conversations",
    // collections of optional modules (e.g. Pages), added by their own extension main.js
    ...(globalThis.gpInternalCollections || []),
]);

/** Shows the API error as a toast + field errors (ignores aborted requests). */
export function handleError(err) {
    if (err && !err.isAbort) {
        app.checkApiError(err);
    }
}

/** Calls a PocketBasePlus custom API route. */
export function api(path, options = {}) {
    return app.pb.send(path, options);
}

let collectionsCache = null;

/** All collections, cached for the page session. */
export function allCollections(refresh = false) {
    if (refresh || !collectionsCache) {
        collectionsCache = app.pb.collections.getFullList({ sort: "name", requestKey: null });
        collectionsCache.catch(() => (collectionsCache = null));
    }
    return collectionsCache;
}

/** Collections whose records can be emailed / trigger automations. */
export async function userCollections() {
    return (await allCollections()).filter((c) => !c.name.startsWith("_") && !internalCollections.has(c.name));
}

/**
 * Candidate recipient paths of a collection: its email fields, the email
 * fields of related collections ("owner.email") and its text fields.
 */
export async function recipientPaths(collectionName) {
    const all = await allCollections();
    const col = all.find((c) => c.name == collectionName);
    if (!col) {
        return [];
    }
    const paths = [];
    for (const f of col.fields) {
        if (f.type == "email") {
            paths.push(f.name);
        }
        if (f.type == "relation") {
            const rel = all.find((c) => c.id == f.collectionId);
            for (const rf of rel?.fields || []) {
                if (rf.type == "email") {
                    paths.push(`${f.name}.${rf.name}`);
                }
            }
        }
    }
    for (const f of col.fields) {
        if (f.type == "text" && !f.hidden && f.name != "id") {
            paths.push(f.name);
        }
    }
    return paths;
}

/** Template variable names for the records of a collection. */
export async function recordVariables(collectionName) {
    const col = (await allCollections()).find((c) => c.name == collectionName);
    if (!col) {
        return [];
    }
    return col.fields.filter((f) => f.type != "password" && (!f.hidden || f.name == "email")).map((f) => f.name);
}

/** Short readable label of a record. */
export function recordLabel(r) {
    return r?.name || r?.title || r?.email || r?.number || r?.id || "";
}

export function formatDate(value) {
    if (!value) {
        return "";
    }
    const d = new Date(String(value).replace(" ", "T"));
    return isNaN(d.getTime()) ? value : d.toLocaleString();
}

/** Debounces fn calls. */
export function debounce(fn, ms = 300) {
    let timer;
    const wrapped = (...args) => {
        clearTimeout(timer);
        timer = setTimeout(() => fn(...args), ms);
    };
    wrapped.cancel = () => clearTimeout(timer);
    return wrapped;
}

// --- layout -----------------------------------------------------------------

/**
 * Standard dashboard page: sidebar + header with breadcrumbs (+ optional buttons) + content.
 */
export function page({ pbEvent, className = "", sidebar, breadcrumbs = [], buttons = null, wide = false }, ...content) {
    return t.div(
        { pbEvent, className: `page gp-page ${className}` },
        sidebar,
        t.div(
            { className: "page-content full-height" },
            t.header(
                { className: "page-header" },
                t.nav(
                    { className: "breadcrumbs" },
                    breadcrumbs.map((c) => t.div({ className: "breadcrumb-item" }, c)),
                ),
                buttons ? t.div({ className: "page-header-primary-btns" }, buttons) : null,
            ),
            t.div({ className: `wrapper ${wide ? "lg" : ""} m-b-base` }, ...content),
            t.footer({ className: "page-footer" }, app.components.credits()),
        ),
    );
}

function sidebarLink(link) {
    return t.a(
        {
            href: () => link.href,
            className: () => {
                const active = app.utils.isActivePath(link.href, !link.exact) || link.isActive?.();
                return `nav-item ${active ? "active" : ""}`;
            },
        },
        link.icon ? t.i({ className: link.icon, ariaHidden: true }) : null,
        t.span({ className: "txt" }, link.label),
        link.after || null,
    );
}

/** Sidebar with groups of links; `groups` may be a function for reactive lists. */
export function sidebar(pbEvent, groups) {
    return app.components.pageSidebar(
        { pbEvent, className: "gp-sidebar" },
        t.nav(
            { className: "sidebar-content scrollable" },
            () => {
                const list = typeof groups == "function" ? groups() : groups;
                return list.map((g) =>
                    t.details(
                        { className: "nav-group", open: true },
                        t.summary(
                            { tabIndex: -1, onfocusout: () => false, onclick: () => false, onkeyup: () => false },
                            g.name,
                        ),
                        g.links.length
                            ? g.links.map(sidebarLink)
                            : t.div({ className: "txt-hint txt-sm gp-sidebar-empty" }, g.empty || "Nothing here yet."),
                    )
                );
            },
        ),
    );
}

export function emailsSidebar() {
    return sidebar("gpEmailsSidebar", [
        {
            name: "Emails",
            links: [
                {
                    href: "#/emails/templates",
                    label: "Templates",
                    icon: "ri-file-code-line",
                    isActive: () => window.location.hash == "#/emails",
                },
                { href: "#/emails/automations", label: "Automations", icon: "ri-flashlight-line" },
                { href: "#/emails/send", label: "Send", icon: "ri-send-plane-line" },
                { href: "#/emails/log", label: "Log", icon: "ri-history-line" },
                { href: "#/collections?collection=email_unsubscribes", label: "Unsubscribes", icon: "ri-mail-forbid-line" },
            ],
        },
    ]);
}

// --- form helpers -------------------------------------------------------------

/** A labelled form field. `name` matches API field errors. */
export function field({ label, name, hint, className = "" }, input) {
    const el = t.div(
        { className: `field ${className}` },
        label ? t.label({ htmlFor: name }, label) : null,
        input,
    );
    // like the native forms, help text goes below the field box
    // (single element: Shablon doesn't support arrays nested in arrays)
    return hint ? t.div({ className: "gp-field-wrap" }, el, t.div({ className: "field-help" }, hint)) : el;
}

/** Text input bound to obj[key]. */
export function textInput(obj, key, attrs = {}) {
    return t.input({
        id: key,
        name: key,
        type: "text",
        value: () => obj[key] ?? "",
        oninput: (e) => (obj[key] = e.target.value),
        ...attrs,
    });
}

/**
 * Dashboard select: the component of the native forms (app.components.select).
 * Native <select> popups are drawn by the browser/OS and ignore the dashboard
 * theme (e.g. a light list with light text in dark mode).
 *
 * `options` is a list of {value, label} or a function returning one (reactive),
 * `value` returns the current value and `onchange` receives the new one ("" when unset).
 * Like the native forms, `required` marks the label and hides the "unset" button,
 * and optional selects show their `placeholder` when empty.
 */
export function select({ options, value, onchange, ...props }) {
    const list = typeof options == "function" ? options : () => options;
    return app.components.select({
        searchThreshold: 8,
        ...props,
        options: () => list(),
        // a new array on every change so that the selection is also refreshed
        // when the options load after the value (the component only watches the value)
        value: () => {
            list();
            return [value() ?? ""];
        },
        onchange: (opts) => onchange(opts[0]?.value ?? ""),
    });
}

/** Multiple values variant of select(): `value` returns an array and `onchange` receives one. */
export function multiSelect({ options, value, onchange, max = 99, ...props }) {
    const list = typeof options == "function" ? options : () => options;
    return app.components.select({
        searchThreshold: 8,
        ...props,
        max,
        options: () => list(),
        value: () => {
            list(); // refresh the selection when the options load after the value
            return [...(value() || [])];
        },
        onchange: (opts) => onchange(opts.map((o) => o.value)),
    });
}

/** Checkbox switch bound to obj[key]. */
export function switchInput(obj, key, label, attrs = {}) {
    return t.div(
        { className: "field" },
        t.input({
            id: attrs.id || key,
            name: key,
            type: "checkbox",
            className: "switch",
            checked: () => !!obj[key],
            onchange: (e) => (obj[key] = e.target.checked),
            ...attrs,
        }),
        t.label({ htmlFor: attrs.id || key }, t.span({ className: "txt" }, label)),
    );
}

/** Sandboxed iframe rendering untrusted email HTML. */
export function emailFrame(html, className = "") {
    return t.iframe({
        className: `gp-email-frame ${className}`,
        title: "Email preview",
        "html-sandbox": "",
        srcdoc: html,
    });
}

/** Dashboard styled checkbox (the native styling needs a .field wrapper + label). */
export function checkbox({ id, checked, onchange, ariaLabel, label = "" }) {
    return t.div(
        { className: "field gp-checkbox" },
        t.input({ type: "checkbox", id, checked, onchange, ariaLabel }),
        t.label({ htmlFor: id }, label),
    );
}

/** Wraps a label-less input in a .field so it gets the dashboard input styling. */
export function bare(input, className = "") {
    return t.div({ className: `field gp-bare ${className}` }, input);
}
