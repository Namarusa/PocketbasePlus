import { api, field, handleError, multiSelect, page, select, sidebar } from "./lib.js";

const actions = ["list", "view", "create", "update", "delete"];

// shared between the sidebar and the page (refreshed on save)
const shared = store({ collections: [], loaded: false });

let loading = null;

// concurrent callers share the same in-flight request
// (separate requests with the same key would auto-cancel each other)
function loadCollections() {
    if (!loading) {
        loading = api("/api/rbac/collections", { requestKey: null })
            .then((list) => (shared.collections = list))
            .catch(handleError)
            .finally(() => {
                shared.loaded = true;
                loading = null;
            });
    }
    return loading;
}

export function permissionsSidebar() {
    if (!shared.loaded) {
        loadCollections();
    }

    return sidebar("gpPermissionsSidebar", () => [
        {
            name: "Access",
            links: [{ href: "#/permissions/roles", label: "Roles & users", icon: "ri-group-line" }],
        },
        {
            name: "Collections",
            empty: "No collections yet.",
            links: shared.collections.map((c) => ({
                href: "#/permissions/collections/" + encodeURIComponent(c.name),
                label: c.name,
                icon: c.type == "auth" ? "ri-user-line" : "ri-folder-2-line",
                exact: true,
                after: c.managed ? t.span({ className: "label sm success m-l-auto" }, "managed") : null,
            })),
        },
    ]);
}

/**
 * Client-side mirror of internal/rbac/compiler.go used for the live preview
 * (the server compiles the authoritative rules on save).
 */
function compile(ownerField, grants) {
    const rules = {};
    for (const action of actions) {
        const parts = grants
            .filter((g) => g.action == action)
            .sort((a, b) => (a.roleId < b.roleId ? -1 : 1))
            .map((g) => {
                const hasRole = `@request.auth.roles.id ?= "${g.roleId}"`;
                if (g.scope == "all") {
                    return hasRole;
                }
                if (g.scope == "custom") {
                    return `(${hasRole} && (${(g.filter || "").trim()}))`;
                }
                if (action == "create") {
                    return `(${hasRole} && @request.body.${ownerField} = @request.auth.id)`;
                }
                if (action == "update" && ownerField != "id") {
                    return `(${hasRole} && ${ownerField} = @request.auth.id && (@request.body.${ownerField}:isset = false || @request.body.${ownerField} = @request.auth.id))`;
                }
                return `(${hasRole} && ${ownerField} = @request.auth.id)`;
            });
        rules[action] = parts.length ? parts.join(" || ") : null;
    }
    return rules;
}

export function pagePermissions(route) {
    const name = route.params.collection ? decodeURIComponent(route.params.collection) : "";

    // landing: jump to the first managed (or first) collection
    if (!name) {
        (shared.loaded ? Promise.resolve() : loadCollections()).then(() => {
            const first = shared.collections.find((c) => c.managed) || shared.collections[0];
            window.location.replace(first ? "#/permissions/collections/" + encodeURIComponent(first.name) : "#/permissions/roles");
        });
        return page({ pbEvent: "gpPagePermissions", sidebar: permissionsSidebar(), breadcrumbs: ["Permissions"] });
    }

    app.store.title = `Permissions - ${name}`;

    const data = store({
        isLoading: true,
        isSaving: false,
        matrix: null,
        managed: false,
        ownerField: "",
        cells: {}, // roleId -> action -> none|own|all|custom
        extras: {}, // roleId -> action -> { filter, fields, presets }
        initSerialized: "",
        showRules: false,
        get grants() {
            const result = [];
            for (const roleId in data.cells) {
                for (const action of actions) {
                    const scope = data.cells[roleId][action];
                    if (scope != "none") {
                        const x = data.extras[roleId]?.[action] || {};
                        const g = { roleId, action, scope };
                        if (scope == "custom") {
                            g.filter = x.filter || "";
                        }
                        if (x.fields?.length && action != "delete") {
                            g.fields = [...x.fields];
                        }
                        if (action == "create" && x.presets && Object.keys(x.presets).length) {
                            g.presets = { ...x.presets };
                        }
                        result.push(g);
                    }
                }
            }
            return result;
        },
        get serialized() {
            return JSON.stringify([data.managed, data.ownerField, data.grants]);
        },
        get hasChanges() {
            return !data.isLoading && data.serialized != data.initSerialized;
        },
        get customError() {
            return data.grants.some((g) => g.scope == "custom" && !g.filter.trim()) ? `Set the filter of the "Custom" cells.` : "";
        },
        get ownError() {
            const usesOwn = data.grants.some((g) => g.scope == "own");
            if (!usesOwn) {
                return "";
            }
            if (!data.ownerField) {
                return `Pick an owner field to use the "Own" scope.`;
            }
            if (data.ownerField == "id" && data.grants.some((g) => g.scope == "own" && g.action == "create")) {
                return `"Own" can't be used for create when the owner field is the record id.`;
            }
            return "";
        },
    });

    function init(m) {
        data.matrix = m;
        data.managed = m.collection.managed;
        data.ownerField = m.collection.ownerField;
        const cells = {};
        const extras = {};
        for (const r of m.roles) {
            cells[r.id] = { list: "none", view: "none", create: "none", update: "none", delete: "none" };
            extras[r.id] = {};
        }
        for (const g of m.grants) {
            if (cells[g.roleId]) {
                cells[g.roleId][g.action] = g.scope;
                extras[g.roleId][g.action] = { filter: g.filter || "", fields: g.fields || [], presets: g.presets || {} };
            }
        }
        data.cells = cells;
        data.extras = extras;
        data.initSerialized = data.serialized;
    }

    async function load() {
        data.isLoading = true;
        try {
            init(await api(`/api/rbac/matrix/${encodeURIComponent(name)}`, { requestKey: "gpRbacMatrix" }));
            data.isLoading = false;
        } catch (err) {
            handleError(err);
        }
    }

    async function save() {
        if (data.isSaving || !data.hasChanges || data.ownError || data.customError) {
            return;
        }
        data.isSaving = true;
        try {
            const m = await api(`/api/rbac/matrix/${data.matrix.collection.id}`, {
                method: "PUT",
                body: { managed: data.managed, ownerField: data.ownerField, grants: data.grants },
            });
            init(m);
            loadCollections();
            app.toasts.success(
                data.managed
                    ? "Successfully saved the permissions and applied the API rules."
                    : "Successfully saved the permissions (not applied: the collection is not managed).",
            );
        } catch (err) {
            handleError(err);
        }
        data.isSaving = false;
    }

    function setRow(roleId, scope) {
        for (const a of actions) {
            data.cells[roleId][a] = scope;
        }
    }

    load();

    const ownerOptions = () => {
        const c = data.matrix?.collection;
        if (!c) {
            return [];
        }
        const list = [{ value: "", label: "None" }];
        if (c.type == "auth") {
            list.push({ value: "id", label: "id (the user record itself)" });
        }
        for (const f of c.fields) {
            if (f.type == "relation" || f.type == "text") {
                list.push({ value: f.name, label: `${f.name} (${f.type})` });
            }
        }
        return list;
    };

    const extrasOf = (roleId, action) => {
        data.extras[roleId] ||= {};
        return (data.extras[roleId][action] ||= { filter: "", fields: [], presets: {} });
    };

    /** Short description of the options of a cell (filter, fields, presets). */
    function cellSummary(roleId, action) {
        const scope = data.cells[roleId]?.[action];
        if (!scope || scope == "none") {
            return "";
        }
        const x = data.extras[roleId]?.[action] || {};
        const parts = [];
        if (scope == "custom") {
            parts.push(x.filter?.trim() ? "filter" : "no filter!");
        }
        if (x.fields?.length && action != "delete") {
            parts.push(`${x.fields.length} field${x.fields.length > 1 ? "s" : ""}`);
        }
        const presets = Object.keys(x.presets || {}).length;
        if (action == "create" && presets) {
            parts.push(`${presets} preset${presets > 1 ? "s" : ""}`);
        }
        return parts.join(" · ");
    }

    function scopeSelect(roleId, action) {
        return t.div(
            { className: "gp-scope-cell" },
            select({
                className: () => `gp-scope gp-scope-${data.cells[roleId]?.[action]}`,
                required: true,
                // "Own" needs an owner field (kept while selected so it can be changed)
                options: () =>
                    [
                        { value: "none", label: "None" },
                        { value: "own", label: "Own" },
                        { value: "all", label: "All" },
                        { value: "custom", label: "Custom" },
                    ].filter((o) => o.value != "own" || data.ownerField || data.cells[roleId]?.[action] == "own"),
                value: () => data.cells[roleId]?.[action],
                onchange: (v) => {
                    data.cells[roleId][action] = v;
                    if (v == "custom" && !extrasOf(roleId, action).filter) {
                        openCellOptions(roleId, action);
                    }
                },
            }),
            t.button(
                {
                    type: "button",
                    className: () => `btn sm circle transparent secondary gp-scope-options ${cellSummary(roleId, action) ? "active" : ""}`,
                    hidden: () => (data.cells[roleId]?.[action] || "none") == "none",
                    ariaDescription: app.attrs.tooltip(action == "delete" ? "Filter" : "Filter, fields and presets"),
                    onclick: () => openCellOptions(roleId, action),
                },
                t.i({ className: "ri-equalizer-line", ariaHidden: true }),
            ),
            t.div({ className: () => `gp-scope-summary ${cellSummary(roleId, action).includes("!") ? "txt-danger" : ""}` }, () => cellSummary(roleId, action)),
        );
    }

    /** Options of a matrix cell: custom filter, allowed fields and create presets. */
    function openCellOptions(roleId, action) {
        const role = data.matrix.roles.find((r) => r.id == roleId);
        const collection = data.matrix.collection;
        const current = extrasOf(roleId, action);
        const form = store({
            filter: current.filter || "",
            fields: [...(current.fields || [])],
            presets: Object.entries(current.presets || {}).map(([key, value]) => ({ key, value: typeof value == "string" ? value : JSON.stringify(value) })),
        });
        const isRead = action == "list" || action == "view";
        const fieldOptions = collection.fields
            .filter((f) => f.type != "password" && f.name != "tokenKey" && f.name != "id")
            .map((f) => ({ value: f.name, label: `${f.name} · ${f.type}` }));
        const presetFields = collection.fields.filter((f) => !["password", "autodate"].includes(f.type) && f.name != "id" && f.name != "tokenKey");

        function apply() {
            const presets = {};
            for (const p of form.presets) {
                if (!p.key) {
                    continue;
                }
                let value = p.value;
                // numbers, booleans and JSON values keep their type
                if (/^(true|false|null|-?\d+(\.\d+)?|[\[{].*)$/s.test(String(value).trim())) {
                    try {
                        value = JSON.parse(value);
                    } catch {}
                }
                presets[p.key] = value;
            }
            data.extras[roleId][action] = { filter: form.filter.trim(), fields: [...form.fields], presets };
            app.modals.close(modal);
        }

        const modal = t.div(
            { className: "modal popup gp-cell-modal" },
            t.header({ className: "modal-header" }, t.h5({ className: "m-auto" }, `${role?.name || "Role"} · ${action}`)),
            t.form(
                {
                    id: "gpCellForm",
                    className: "modal-content",
                    onsubmit: (e) => {
                        e.preventDefault();
                        apply();
                    },
                },
                () => data.cells[roleId][action] == "custom"
                    ? t.div(
                        { rid: "filter", className: "m-b-sm" },
                        field(
                            {
                                label: "Filter (Custom scope)",
                                hint: `The records this role can ${action}, e.g. status = "published" || owner = @request.auth.id. PocketBase filter syntax, @request.auth.* and @collection.* are available.`,
                            },
                            t.input({ type: "text", className: "txt-code", required: true, value: () => form.filter, oninput: (e) => (form.filter = e.target.value) }),
                        ),
                    )
                    : null,
                action != "delete"
                    ? t.div(
                        { className: "m-b-sm" },
                        field(
                            {
                                label: isRead ? "Readable fields" : "Writable fields",
                                hint: isRead
                                    ? "Leave empty for all fields. The other fields are hidden from the responses and can't be used in filters, sorts or expands."
                                    : "Leave empty for all fields. Changing other fields is refused (sending their unchanged value back is accepted).",
                            },
                            multiSelect({
                                placeholder: "All fields",
                                options: fieldOptions,
                                value: () => form.fields,
                                onchange: (v) => (form.fields = v),
                            }),
                        ),
                    )
                    : null,
                action == "create"
                    ? t.div(
                        null,
                        t.label({ className: "gp-label" }, "Presets (default values when the field isn't sent)"),
                        () =>
                            t.div(
                                { rid: "presets-" + form.presets.length, className: "gp-presets-list" },
                                form.presets.map((p, i) =>
                                    t.div(
                                        { className: "gp-preset-row" },
                                        t.div({ className: "field gp-bare" }, select({
                                            placeholder: "Field",
                                            required: true,
                                            options: presetFields.map((f) => ({ value: f.name, label: f.name })),
                                            value: () => p.key,
                                            onchange: (v) => (p.key = v),
                                        })),
                                        t.div({ className: "field gp-bare" }, t.input({ type: "text", placeholder: "Value, @request.auth.id or @now", value: () => p.value, oninput: (e) => (p.value = e.target.value) })),
                                        t.button({ type: "button", className: "btn sm circle transparent danger", title: "Remove", onclick: () => (form.presets = form.presets.filter((_, j) => j != i)) }, t.i({ className: "ri-close-line", ariaHidden: true })),
                                    )
                                ),
                            ),
                        t.button(
                            { type: "button", className: "btn sm transparent secondary", onclick: () => (form.presets = [...form.presets, { key: "", value: "" }]) },
                            t.i({ className: "ri-add-line", ariaHidden: true }),
                            t.span({ className: "txt" }, "Add preset"),
                        ),
                        t.div({ className: "field-help" }, "Presets are applied after the create rule check: with an \"Own\" create scope, clients still send the owner."),
                    )
                    : null,
            ),
            t.footer(
                { className: "modal-footer" },
                t.button({ type: "button", className: "btn transparent secondary m-r-auto", onclick: () => app.modals.close(modal) }, t.span({ className: "txt" }, "Cancel")),
                t.button({ type: "submit", "html-form": "gpCellForm", className: "btn expanded" }, t.span({ className: "txt" }, "Apply")),
            ),
        );
        modal.onafterclose = () => modal.remove();
        document.body.appendChild(modal);
        app.modals.open(modal);
    }

    return page(
        {
            pbEvent: "gpPagePermissions",
            className: "gp-page-permissions",
            sidebar: permissionsSidebar(),
            breadcrumbs: ["Permissions", name],
        },
        () => {
            if (data.isLoading) {
                return t.div({ rid: "loading", className: "block txt-center" }, t.span({ className: "loader lg" }));
            }

            const m = data.matrix;

            return t.form(
                {
                    rid: "form",
                    className: "grid",
                    inert: () => data.isSaving,
                    onsubmit: (e) => {
                        e.preventDefault();
                        save();
                    },
                },
                t.div(
                    { className: "col-lg-12" },
                    t.p(
                        { className: "txt-hint m-0" },
                        "Pick what each role can do. Saving compiles this matrix into the collection's API rules, so PocketBase enforces it natively (including list filtering and realtime subscriptions).",
                    ),
                ),
                t.div(
                    { className: "col-lg-12" },
                    t.div(
                        { className: "field" },
                        t.input({
                            id: "managed",
                            type: "checkbox",
                            className: "switch",
                            checked: () => data.managed,
                            onchange: (e) => (data.managed = e.target.checked),
                        }),
                        t.label(
                            { htmlFor: "managed" },
                            t.span({ className: "txt" }, "Manage the API rules of this collection with roles"),
                        ),
                    ),
                    t.div(
                        { className: "alert warning m-t-sm", hidden: () => !data.managed },
                        "The API rules of this collection are generated from this matrix: changes made in the collection's ",
                        t.strong(null, "API Rules"),
                        " tab will be overwritten.",
                    ),
                    t.div(
                        { className: "alert info m-t-sm", hidden: () => data.managed },
                        "While unmanaged, the matrix is saved but the collection's current API rules are left untouched.",
                    ),
                ),
                t.div(
                    { className: "col-lg-6" },
                    t.div(
                        { className: "field" },
                        t.label({ htmlFor: "ownerField" }, `Owner field (used by the "Own" scope)`),
                        select({
                            id: "ownerField",
                            name: "ownerField",
                            placeholder: "None",
                            options: () => ownerOptions().filter((o) => o.value),
                            value: () => data.ownerField,
                            onchange: (v) => (data.ownerField = v),
                        }),
                    ),
                ),
                t.div(
                    { className: "col-lg-12" },
                    m.roles.length
                        ? t.div(
                            { className: "gp-table-wrap" },
                            t.table(
                                { className: "gp-matrix" },
                                t.thead(
                                    null,
                                    t.tr(
                                        null,
                                        t.th(null, "Role"),
                                        actions.map((a) => t.th({ className: "txt-capitalize" }, a)),
                                        t.th(null, ""),
                                    ),
                                ),
                                t.tbody(
                                    null,
                                    m.roles.map((role) =>
                                        t.tr(
                                            null,
                                            t.td(null, t.strong(null, role.name)),
                                            actions.map((a) => t.td(null, scopeSelect(role.id, a))),
                                            t.td(
                                                { className: "txt-right txt-nowrap" },
                                                t.button(
                                                    {
                                                        type: "button",
                                                        className: "btn sm secondary transparent",
                                                        onclick: () => setRow(role.id, "all"),
                                                    },
                                                    "All",
                                                ),
                                                t.button(
                                                    {
                                                        type: "button",
                                                        className: "btn sm secondary transparent",
                                                        onclick: () => setRow(role.id, "none"),
                                                    },
                                                    "None",
                                                ),
                                            ),
                                        )
                                    ),
                                ),
                            ),
                        )
                        : t.div(
                            { className: "alert info" },
                            "No roles yet. ",
                            t.a({ href: "#/permissions/roles" }, "Create roles"),
                            " first.",
                        ),
                    t.div(
                        { className: "field-help m-t-sm" },
                        () =>
                            `Users without a granted role get nothing; superusers always have full access. "Own" means records whose "${
                                data.ownerField || "owner field"
                            }" is the signed-in user.`,
                    ),
                    t.div({ className: "alert danger m-t-sm", hidden: () => !data.ownError }, () => data.ownError),
                    t.div({ className: "alert danger m-t-sm", hidden: () => !data.customError }, () => data.customError),
                ),
                t.div(
                    { className: "col-lg-12" },
                    t.button(
                        {
                            type: "button",
                            className: "btn sm secondary transparent",
                            onclick: () => (data.showRules = !data.showRules),
                        },
                        t.i({ className: () => (data.showRules ? "ri-eye-off-line" : "ri-eye-line"), ariaHidden: true }),
                        t.span({ className: "txt" }, () => (data.showRules ? "Hide generated rules" : "Show generated rules")),
                    ),
                    app.components.slide(
                        () => data.showRules,
                        t.div(
                            { className: "gp-rules m-t-sm" },
                            () => {
                                const preview = data.managed && !data.ownError
                                    ? compile(data.ownerField, data.grants)
                                    : data.matrix.rules;
                                return actions.map((a) =>
                                    t.div(
                                        { className: "gp-rule" },
                                        t.span({ className: "txt-hint txt-capitalize" }, a),
                                        t.code(null, preview[a] ?? "null (superusers only)"),
                                    )
                                );
                            },
                            t.div(
                                { className: "field-help" },
                                () =>
                                    data.managed
                                        ? "Preview of the rules applied on save."
                                        : "Current API rules (unmanaged collection).",
                            ),
                        ),
                    ),
                ),
                t.div({ className: "col-lg-12" }, t.hr()),
                t.div(
                    { className: "col-lg-12" },
                    t.div(
                        { className: "flex" },
                        t.div({ className: "m-r-auto" }),
                        t.button(
                            {
                                type: "button",
                                className: "btn transparent secondary",
                                hidden: () => !data.hasChanges,
                                onclick: () => init(data.matrix),
                            },
                            t.span({ className: "txt" }, "Cancel"),
                        ),
                        t.button(
                            {
                                className: () => `btn expanded-lg ${data.isSaving ? "loading" : ""}`,
                                disabled: () => !data.hasChanges || data.isSaving || !!data.ownError || !!data.customError,
                            },
                            t.span({ className: "txt" }, "Save changes"),
                        ),
                    ),
                ),
            );
        },
    );
}
