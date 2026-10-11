// Settings → AI: providers (TypeSafe, OpenRouter) and the models used by
// the flows "AI decision" step.

import { api, field, handleError, select } from "./lib.js";

export const providerKinds = {
    typesafe: {
        label: "TypeSafe",
        icon: "ri-flashlight-line",
        hint: "System One models (Jev): fast typed decisions with calibrated probabilities.",
        modelType: "system_one",
        defaultURL: "https://api.typesafe.ai",
        keyURL: "https://typesafe.ai",
    },
    openrouter: {
        label: "OpenRouter",
        icon: "ri-route-line",
        hint: "Standard LLMs of many vendors (OpenAI, Anthropic, Google...) through one API.",
        modelType: "standard",
        defaultURL: "https://openrouter.ai/api/v1",
        keyURL: "https://openrouter.ai/settings/keys",
        example: "openai/gpt-5-mini",
    },
    anthropic: {
        label: "Anthropic",
        icon: "ri-claude-line",
        hint: "Claude models (Opus, Sonnet, Haiku), direct API.",
        modelType: "standard",
        defaultURL: "https://api.anthropic.com",
        keyURL: "https://platform.claude.com/settings/keys",
        example: "claude-opus-5-5",
    },
    openai: {
        label: "OpenAI",
        icon: "ri-openai-line",
        hint: "GPT models, direct API.",
        modelType: "standard",
        defaultURL: "https://api.openai.com/v1",
        keyURL: "https://platform.openai.com/api-keys",
        example: "gpt-5-mini",
    },
    gemini: {
        label: "Google Gemini",
        icon: "ri-gemini-line",
        hint: "Gemini models (Google AI Studio key), OpenAI compatible API.",
        modelType: "standard",
        defaultURL: "https://generativelanguage.googleapis.com/v1beta/openai",
        keyURL: "https://aistudio.google.com/apikey",
        example: "gemini-3.8-flash",
    },
    deepseek: {
        label: "DeepSeek",
        icon: "ri-water-flash-line",
        hint: "DeepSeek models. JSON mode only: the answer schemas go in the instructions.",
        modelType: "standard",
        defaultURL: "https://api.deepseek.com",
        keyURL: "https://platform.deepseek.com/api_keys",
        example: "deepseek-v4-pro",
    },
    kimi: {
        label: "Kimi (Moonshot)",
        icon: "ri-moon-clear-line",
        hint: "Kimi models, direct API.",
        endpointHint: "China accounts: https://api.moonshot.cn/v1",
        modelType: "standard",
        defaultURL: "https://api.moonshot.ai/v1",
        keyURL: "https://platform.kimi.ai/console/api-keys",
        example: "kimi-k3",
    },
    glm: {
        label: "GLM (Z.ai)",
        icon: "ri-bubble-chart-line",
        hint: "GLM models. JSON mode only: the answer schemas go in the instructions.",
        endpointHint: "China accounts: https://open.bigmodel.cn/api/paas/v4",
        modelType: "standard",
        defaultURL: "https://api.z.ai/api/paas/v4",
        keyURL: "https://z.ai/manage-apikey/apikey-list",
        example: "glm-5",
    },
};

export const modelTypes = {
    system_one: {
        label: "System One",
        icon: "ri-flashlight-line",
        hint: "Decision models answering typed questions natively, with calibrated probabilities. Fast (under a second) and cheap.",
        kind: "typesafe",
        example: { name: "Jev", model: "jev-latest" },
    },
    standard: {
        label: "Standard",
        icon: "ri-chat-3-line",
        hint: "General LLMs (Claude, GPT, Gemini, DeepSeek, Kimi, GLM...) answering through a JSON schema. Their probabilities are self-estimated (not calibrated). They also write texts and run the assistant.",
        kind: "openrouter",
        example: { name: "GPT-5 mini", model: "openai/gpt-5-mini" },
    },
};

/** Same as the native settings sidebar (not exported by the dashboard). */
function settingsSidebar() {
    return app.components.pageSidebar(
        { pbEvent: "settingsSidebar", className: "settings-sidebar" },
        t.nav({ className: "sidebar-content scrollable" }, () => {
            const result = [];
            for (const groupName in app.store.settingsNavGroups) {
                const children = app.store.settingsNavGroups[groupName];
                result.push(
                    t.details(
                        { className: "nav-group", "html-data-group": groupName, open: true },
                        t.summary({ tabIndex: -1, onfocusout: () => false, onclick: () => false, onkeyup: () => false }, groupName),
                        children.map((link) =>
                            t.a(
                                {
                                    href: link.href,
                                    className: (el) => `nav-item ${(link.isActive?.(el) || app.utils.isActivePath(link.href, false)) ? "active" : ""}`,
                                },
                                link.icon ? t.i({ className: link.icon, ariaHidden: true }) : null,
                                t.span({ className: "txt" }, link.label),
                            )
                        ),
                    ),
                );
            }
            return result;
        }),
    );
}

function settingsPage({ pbEvent, title, buttons }, ...content) {
    return t.div(
        { pbEvent, className: "page gp-page" },
        settingsSidebar(),
        t.div(
            { className: "page-content full-height" },
            t.header(
                { className: "page-header" },
                t.nav({ className: "breadcrumbs" }, t.div({ className: "breadcrumb-item" }, "Settings"), t.div({ className: "breadcrumb-item" }, title)),
                buttons ? t.div({ className: "page-header-primary-btns" }, buttons) : null,
            ),
            t.div({ className: "wrapper m-b-base" }, ...content),
            t.footer({ className: "page-footer" }, app.components.credits()),
        ),
    );
}

// shared lists (the pages and the flow editor model picker)
const shared = store({ providers: [], models: [], loaded: false });

export async function loadAI() {
    try {
        const [providers, models] = await Promise.all([
            app.pb.collection("ai_providers").getFullList({ sort: "name", requestKey: null }),
            app.pb.collection("ai_models").getFullList({ sort: "name", requestKey: null }),
        ]);
        shared.providers = providers;
        shared.models = models;
    } catch (err) {
        handleError(err);
    }
    shared.loaded = true;
    return shared;
}

export function aiModels() {
    return shared.models;
}

function providerOf(model) {
    return shared.providers.find((p) => p.id == model.provider);
}

function typeBadge(type) {
    const def = modelTypes[type];
    return t.span({ className: `label sm gp-ai-type ${type == "system_one" ? "info" : ""}` }, t.i({ className: def?.icon, ariaHidden: true }), def?.label || type);
}

function emptyRow(cols, text, rid) {
    return t.tr({ rid }, t.td({ colSpan: cols, className: "txt-hint txt-center p-sm" }, text));
}

function loadingRow(cols) {
    return t.tr({ rid: "loading" }, t.td({ colSpan: cols, className: "txt-center" }, t.span({ className: "loader" })));
}

// =======================================================================================
// providers

export function pageAIProviders() {
    app.store.title = "AI providers";
    loadAI();

    const busy = store({ testing: "" });

    async function test(p) {
        busy.testing = p.id;
        try {
            const res = await api(`/api/ai/providers/${p.id}/models`, { method: "POST", requestKey: null });
            app.toasts.success(`Connected to ${p.name}: ${res.models.length} model(s) available.`);
        } catch (err) {
            handleError(err);
        }
        busy.testing = "";
    }

    function remove(p) {
        app.modals.confirm(`Do you really want to delete the provider "${p.name}"?`, async () => {
            try {
                await app.pb.collection("ai_providers").delete(p.id);
                app.toasts.success("Successfully deleted the provider.");
                loadAI();
            } catch (err) {
                handleError(err);
            }
        });
    }

    return settingsPage(
        {
            pbEvent: "gpPageAIProviders",
            title: "AI providers",
            buttons: t.button(
                { type: "button", className: "btn", onclick: () => openProviderModal() },
                t.i({ className: "ri-add-line", ariaHidden: true }),
                t.span({ className: "txt" }, "New provider"),
            ),
        },
        t.p(
            { className: "txt-hint m-t-0" },
            "The accounts used by the AI features. Add a provider with its API key, then its models in ",
            t.a({ href: "#/settings/ai/models" }, "AI models"),
            ". API keys are stored in the database and never sent back to the browser.",
        ),
        t.div(
            { className: "gp-table-wrap" },
            t.table(
                null,
                t.thead(null, t.tr(null, t.th(null, "Name"), t.th(null, "Provider"), t.th(null, "API key"), t.th(null, "Endpoint"), t.th(null, ""))),
                t.tbody(null, () => {
                    if (!shared.loaded) {
                        return loadingRow(5);
                    }
                    if (!shared.providers.length) {
                        return emptyRow(5, "No providers yet: add TypeSafe for Jev, or OpenRouter for standard LLMs.", "empty");
                    }
                    return shared.providers.map((p) => {
                        const kind = providerKinds[p.kind] || {};
                        return t.tr(
                            { rid: p.id + p.updated },
                            t.td(null, t.strong(null, p.name)),
                            t.td(null, t.span({ className: "gp-ai-kind" }, t.i({ className: kind.icon, ariaHidden: true }), kind.label || p.kind)),
                            t.td(null, p.key_hint ? t.code(null, p.key_hint) : t.span({ className: "label sm danger" }, "Not set")),
                            t.td({ className: "txt-hint txt-sm" }, p.base_url || "Default"),
                            t.td(
                                { className: "txt-right txt-nowrap" },
                                t.button(
                                    {
                                        type: "button",
                                        className: () => `btn sm transparent secondary ${busy.testing == p.id ? "loading" : ""}`,
                                        disabled: () => busy.testing == p.id,
                                        onclick: () => test(p),
                                    },
                                    t.i({ className: "ri-pulse-line", ariaHidden: true }),
                                    t.span({ className: "txt" }, "Test"),
                                ),
                                t.button(
                                    { type: "button", className: "btn sm circle transparent secondary", ariaLabel: app.attrs.tooltip("Edit"), onclick: () => openProviderModal({ existing: p }) },
                                    t.i({ className: "ri-pencil-line", ariaHidden: true }),
                                ),
                                t.button(
                                    { type: "button", className: "btn sm circle transparent danger", ariaLabel: app.attrs.tooltip("Delete"), onclick: () => remove(p) },
                                    t.i({ className: "ri-delete-bin-7-line", ariaHidden: true }),
                                ),
                            ),
                        );
                    });
                }),
            ),
        ),
    );
}

function kindCards(obj, key, defs, disabled = () => false) {
    return t.div(
        { className: "gp-ai-cards" },
        Object.entries(defs).map(([value, def]) =>
            t.label(
                { className: () => `gp-ai-card ${obj[key] == value ? "active" : ""} ${disabled() ? "disabled" : ""}` },
                t.input({
                    type: "radio",
                    name: key,
                    value,
                    checked: () => obj[key] == value,
                    disabled,
                    onchange: () => (obj[key] = value),
                }),
                t.span({ className: "gp-ai-card-title" }, t.i({ className: def.icon, ariaHidden: true }), def.label),
                t.small({ className: "txt-hint" }, def.hint),
            )
        ),
    );
}

export function openProviderModal({ existing = null, kind = "typesafe", onsaved = null } = {}) {
    const data = store({
        name: existing?.name || "",
        kind: existing?.kind || kind,
        api_key: "",
        base_url: existing?.base_url || "",
        advanced: !!existing?.base_url || !!providerKinds[existing?.kind || kind]?.endpointHint,
        isSaving: false,
    });
    const hasModels = existing && shared.models.some((m) => m.provider == existing.id);

    async function save() {
        data.isSaving = true;
        try {
            const body = { name: data.name || providerKinds[data.kind].label, kind: data.kind, base_url: data.base_url };
            // an empty key field keeps the current key
            if (data.api_key || !existing) {
                body.api_key = data.api_key;
            }
            const saved = existing
                ? await app.pb.collection("ai_providers").update(existing.id, body)
                : await app.pb.collection("ai_providers").create(body);
            app.toasts.success("Successfully saved the provider.");
            app.modals.close(modal);
            await loadAI();
            onsaved?.(saved);
        } catch (err) {
            handleError(err);
        }
        data.isSaving = false;
    }

    const modal = t.div(
        { className: "modal popup gp-ai-modal" },
        t.header({ className: "modal-header" }, t.h5({ className: "m-auto" }, existing ? "Edit provider" : "New AI provider")),
        t.form(
            {
                id: "gpAIProviderForm",
                className: "modal-content",
                inert: () => data.isSaving,
                onsubmit: (e) => {
                    e.preventDefault();
                    save();
                },
            },
            t.label({ className: "gp-label" }, "Provider"),
            kindCards(data, "kind", providerKinds, () => hasModels),
            () => (hasModels ? t.div({ rid: "inuse", className: "field-help m-b-sm" }, "The provider has models, so its kind can't change.") : null),
            t.div(
                { className: "grid sm m-t-sm" },
                t.div(
                    { className: "col-lg-12" },
                    field(
                        { label: "Name", name: "name" },
                        t.input({
                            id: "name",
                            name: "name",
                            type: "text",
                            placeholder: () => providerKinds[data.kind].label,
                            value: () => data.name,
                            oninput: (e) => (data.name = e.target.value),
                        }),
                    ),
                ),
                t.div(
                    { className: "col-lg-12" },
                    field(
                        {
                            label: "API key",
                            name: "api_key",
                            hint: t.span(
                                null,
                                "Create one at ",
                                () => t.a({ href: providerKinds[data.kind].keyURL, target: "_blank", rel: "noopener noreferrer" }, providerKinds[data.kind].keyURL),
                                existing?.key_hint ? ` · leave empty to keep the current key (${existing.key_hint})` : "",
                            ),
                        },
                        t.input({
                            id: "api_key",
                            name: "api_key",
                            type: "password",
                            autocomplete: "new-password",
                            required: !existing?.key_hint,
                            placeholder: existing?.key_hint ? "Unchanged" : "",
                            value: () => data.api_key,
                            oninput: (e) => (data.api_key = e.target.value),
                        }),
                    ),
                ),
                t.div(
                    { className: "col-lg-12" },
                    () =>
                        data.advanced
                            ? field(
                                { label: "Endpoint (optional)", name: "base_url", hint: providerKinds[data.kind].endpointHint || "Only for proxies, regional endpoints or compatible gateways." },
                                t.input({
                                    id: "base_url",
                                    name: "base_url",
                                    type: "url",
                                    placeholder: () => providerKinds[data.kind].defaultURL,
                                    value: () => data.base_url,
                                    oninput: (e) => (data.base_url = e.target.value),
                                }),
                            )
                            : t.button(
                                { rid: "adv", type: "button", className: "btn sm transparent secondary", onclick: () => (data.advanced = true) },
                                t.i({ className: "ri-settings-3-line", ariaHidden: true }),
                                t.span({ className: "txt" }, "Custom endpoint"),
                            ),
                ),
            ),
        ),
        t.footer(
            { className: "modal-footer" },
            t.button({ type: "button", className: "btn transparent secondary m-r-auto", onclick: () => app.modals.close(modal) }, t.span({ className: "txt" }, "Cancel")),
            t.button(
                { type: "submit", "html-form": "gpAIProviderForm", className: () => `btn expanded ${data.isSaving ? "loading" : ""}`, disabled: () => data.isSaving },
                t.span({ className: "txt" }, existing ? "Save" : "Create"),
            ),
        ),
    );
    modal.onafterclose = () => modal.remove();
    document.body.appendChild(modal);
    app.modals.open(modal);
}

// =======================================================================================
// models

export function pageAIModels() {
    app.store.title = "AI models";
    loadAI();

    const busy = store({ testing: "" });

    async function test(m) {
        busy.testing = m.id;
        try {
            const res = await api(`/api/ai/models/${m.id}/test`, { method: "POST", requestKey: null });
            openTestResult(m, res);
        } catch (err) {
            handleError(err);
        }
        busy.testing = "";
    }

    function remove(m) {
        app.modals.confirm(`Do you really want to delete the model "${m.name}"?`, async () => {
            try {
                await app.pb.collection("ai_models").delete(m.id);
                app.toasts.success("Successfully deleted the model.");
                loadAI();
            } catch (err) {
                handleError(err);
            }
        });
    }

    return settingsPage(
        {
            pbEvent: "gpPageAIModels",
            title: "AI models",
            buttons: t.button(
                { type: "button", className: "btn", onclick: () => openModelModal() },
                t.i({ className: "ri-add-line", ariaHidden: true }),
                t.span({ className: "txt" }, "New model"),
            ),
        },
        t.p(
            { className: "txt-hint m-t-0" },
            "The models available in the flows \"AI decision\" step. ",
            t.strong(null, "System One"),
            " models (TypeSafe Jev) are built for typed decisions; ",
            t.strong(null, "Standard"),
            " models (OpenRouter) are general LLMs answering the same questions.",
        ),
        t.div(
            { className: "gp-table-wrap" },
            t.table(
                null,
                t.thead(null, t.tr(null, t.th(null, "Name"), t.th(null, "Type"), t.th(null, "Provider"), t.th(null, "Model id"), t.th(null, ""))),
                t.tbody(null, () => {
                    if (!shared.loaded) {
                        return loadingRow(5);
                    }
                    if (!shared.models.length) {
                        return emptyRow(5, shared.providers.length ? "No models yet." : "Add a provider first, then its models.", "empty" + shared.providers.length);
                    }
                    return shared.models.map((m) =>
                        t.tr(
                            { rid: m.id + m.updated },
                            t.td(null, t.strong(null, m.name), m.description ? t.div({ className: "txt-hint txt-sm" }, m.description) : null),
                            t.td(null, typeBadge(m.type)),
                            t.td(null, providerOf(m)?.name || "?"),
                            t.td(null, t.code(null, m.model)),
                            t.td(
                                { className: "txt-right txt-nowrap" },
                                t.button(
                                    {
                                        type: "button",
                                        className: () => `btn sm transparent secondary ${busy.testing == m.id ? "loading" : ""}`,
                                        disabled: () => busy.testing == m.id,
                                        onclick: () => test(m),
                                    },
                                    t.i({ className: "ri-play-line", ariaHidden: true }),
                                    t.span({ className: "txt" }, "Test"),
                                ),
                                t.button(
                                    { type: "button", className: "btn sm circle transparent secondary", ariaLabel: app.attrs.tooltip("Edit"), onclick: () => openModelModal({ existing: m }) },
                                    t.i({ className: "ri-pencil-line", ariaHidden: true }),
                                ),
                                t.button(
                                    { type: "button", className: "btn sm circle transparent danger", ariaLabel: app.attrs.tooltip("Delete"), onclick: () => remove(m) },
                                    t.i({ className: "ri-delete-bin-7-line", ariaHidden: true }),
                                ),
                            ),
                        )
                    );
                }),
            ),
        ),
    );
}

export function openModelModal({ existing = null, onsaved = null } = {}) {
    const data = store({
        type: existing?.type || "system_one",
        provider: existing?.provider || "",
        model: existing?.model || "",
        name: existing?.name || "",
        description: existing?.description || "",
        models: [], // models offered by the selected provider
        modelsFor: "",
        modelsError: "",
        loadingModels: false,
        search: "",
        isSaving: false,
    });

    function compatibleProviders() {
        return shared.providers.filter((p) => providerKinds[p.kind]?.modelType == data.type);
    }

    // keep a compatible provider selected when the type changes
    const providerWatcher = watch(() => {
        const list = compatibleProviders();
        if (!list.some((p) => p.id == data.provider)) {
            data.provider = list[0]?.id || "";
        }
    });

    // the models of the provider, loaded as soon as it's selected
    async function loadModels(force = false) {
        const id = data.provider;
        if (!id || (!force && data.modelsFor == id)) {
            return;
        }
        data.modelsFor = id;
        data.models = [];
        data.modelsError = "";
        data.search = "";
        data.loadingModels = true;
        try {
            if (force || !modelsCache[id]) {
                modelsCache[id] = (await api(`/api/ai/providers/${id}/models`, { method: "POST", requestKey: null })).models || [];
            }
            if (data.modelsFor == id) {
                data.models = modelsCache[id];
            }
        } catch (err) {
            if (data.modelsFor == id) {
                data.modelsError = err?.response?.message || err?.message || "Failed to load the models.";
            }
        }
        if (data.modelsFor == id) {
            data.loadingModels = false;
        }
    }
    const modelsWatcher = watch(() => data.provider, () => loadModels());
    loadModels();

    function pickModel(m) {
        const autoName = !data.name || data.models.some((x) => x.id == data.model && (x.name || x.id) == data.name);
        data.model = m.id;
        if (autoName) {
            data.name = m.name || m.id;
        }
    }

    // example model id of the selected provider
    function exampleModel() {
        const p = shared.providers.find((x) => x.id == data.provider);
        return providerKinds[p?.kind]?.example || modelTypes[data.type].example.model;
    }

    async function save() {
        data.isSaving = true;
        try {
            const model = data.model || exampleModel();
            const body = {
                type: data.type,
                provider: data.provider,
                model,
                name: data.name || model,
                description: data.description,
            };
            const saved = existing
                ? await app.pb.collection("ai_models").update(existing.id, body)
                : await app.pb.collection("ai_models").create(body);
            app.toasts.success("Successfully saved the model.");
            app.modals.close(modal);
            await loadAI();
            onsaved?.(saved);
        } catch (err) {
            handleError(err);
        }
        data.isSaving = false;
    }

    function filteredModels() {
        const terms = data.search.toLowerCase().split(/\s+/).filter(Boolean);
        return data.models.filter((m) => {
            const text = (m.id + " " + (m.name || "")).toLowerCase();
            return terms.every((t) => text.includes(t));
        });
    }

    function modelPicker() {
        return t.div(
            { className: "gp-ai-picker" },
            t.div(
                { className: "gp-ai-picker-head" },
                () => {
                    const provider = shared.providers.find((p) => p.id == data.provider);
                    if (data.loadingModels) {
                        return t.span({ rid: "loading:" + data.provider, className: "txt-hint" }, t.span({ className: "loader sm" }), ` Loading the models of ${provider?.name || "the provider"}…`);
                    }
                    if (data.modelsError) {
                        return t.span({ rid: "error:" + data.modelsError, className: "txt-danger" }, "Couldn't load the models: " + data.modelsError);
                    }
                    const n = filteredModels().length;
                    const text = data.search
                        ? `${n} of ${data.models.length} models`
                        : `${data.models.length} models available from ${provider?.name || "the provider"}${provider?.kind == "openrouter" ? " (supporting structured outputs)" : ""}`;
                    // keyed by the text: an element with the same rid is reused as is
                    return t.span({ rid: "count:" + text, className: "txt-hint" }, text);
                },
                t.div(
                    { className: "field gp-bare gp-ai-picker-search" },
                    t.input({
                        type: "search",
                        placeholder: "Search models (e.g. claude, gpt, free)…",
                        ariaLabel: "Search models",
                        value: () => data.search,
                        oninput: (e) => (data.search = e.target.value),
                    }),
                ),
                t.button(
                    {
                        type: "button",
                        className: () => `btn sm circle transparent secondary ${data.loadingModels ? "loading" : ""}`,
                        ariaLabel: app.attrs.tooltip("Reload the models"),
                        onclick: () => loadModels(true),
                    },
                    t.i({ className: "ri-refresh-line", ariaHidden: true }),
                ),
            ),
            () => {
                if (data.loadingModels || data.modelsError || !data.models.length) {
                    return null;
                }
                const list = filteredModels();
                const shown = list.slice(0, 100);
                return t.div(
                    // keyed by content (rows are recreated when the list changes)
                    { rid: `models-${data.modelsFor}-${data.search}-${list.length}-${data.model}`, className: "gp-ai-picker-list", role: "listbox" },
                    shown.length
                        ? shown.map((m) =>
                            t.button(
                                {
                                    type: "button",
                                    role: "option",
                                    ariaSelected: m.id == data.model ? "true" : "false",
                                    className: `gp-ai-picker-item ${m.id == data.model ? "active" : ""}`,
                                    onclick: () => pickModel(m),
                                },
                                t.span({ className: "gp-ai-picker-name" }, m.name || m.id),
                                t.code({ className: "gp-ai-picker-id" }, m.id),
                                modelMeta(m) ? t.span({ className: "gp-ai-picker-meta" }, modelMeta(m)) : null,
                            )
                        )
                        : t.div({ className: "txt-hint p-sm" }, "No matching model. You can still type a model id above."),
                    list.length > shown.length ? t.div({ className: "txt-hint txt-sm p-sm" }, `${list.length - shown.length} more: refine the search.`) : null,
                );
            },
        );
    }

    const modal = t.div(
        { className: "modal popup gp-ai-modal" },
        t.header({ className: "modal-header" }, t.h5({ className: "m-auto" }, existing ? "Edit model" : "New AI model")),
        t.form(
            {
                id: "gpAIModelForm",
                className: "modal-content",
                inert: () => data.isSaving,
                onsubmit: (e) => {
                    e.preventDefault();
                    save();
                },
            },
            t.label({ className: "gp-label" }, "Model type"),
            kindCards(data, "type", modelTypes),
            () => {
                const list = compatibleProviders();
                const def = modelTypes[data.type];
                if (!list.length) {
                    return t.div(
                        { rid: "noprovider_" + data.type, className: "alert warning m-t-sm" },
                        t.span(
                            { className: "m-r-auto" },
                            data.type == "system_one"
                                ? "System One models run on TypeSafe: add it as a provider first."
                                : "Standard models run on OpenRouter, Anthropic, OpenAI, Gemini, DeepSeek, Kimi or GLM: add one as a provider first.",
                        ),
                        t.button(
                            {
                                type: "button",
                                className: "btn sm",
                                onclick: () => openProviderModal({ kind: def.kind, onsaved: (saved) => (data.provider = saved.id) }),
                            },
                            t.span({ className: "txt" }, "Add a provider"),
                        ),
                    );
                }
                return t.div(
                    { rid: "fields_" + data.type, className: "grid sm m-t-sm" },
                    t.div(
                        { className: "col-lg-6" },
                        field(
                            { label: "Provider", name: "provider" },
                            select({
                                name: "provider",
                                required: true,
                                options: () => compatibleProviders().map((p) => ({ value: p.id, label: p.name })),
                                value: () => data.provider,
                                                onchange: (v) => (data.provider = v),
                            }),
                        ),
                    ),
                    t.div(
                        { className: "col-lg-6" },
                        field(
                            { label: "Model id", name: "model" },
                            t.input({
                                id: "model",
                                name: "model",
                                type: "text",
                                className: "txt-code",
                                autocomplete: "off",
                                placeholder: () => exampleModel(),
                                value: () => data.model,
                                oninput: (e) => (data.model = e.target.value),
                            }),
                        ),
                    ),
                    t.div({ className: "col-lg-12" }, modelPicker()),
                    t.div(
                        { className: "col-lg-12" },
                        field(
                            { label: "Name", name: "name", hint: "Shown in the flow steps." },
                            t.input({
                                id: "name",
                                name: "name",
                                type: "text",
                                placeholder: () => data.model || exampleModel(),
                                value: () => data.name,
                                oninput: (e) => (data.name = e.target.value),
                            }),
                        ),
                    ),
                    t.div(
                        { className: "col-lg-12" },
                        field(
                            { label: "Description (optional)", name: "description" },
                            t.input({
                                id: "description",
                                name: "description",
                                type: "text",
                                placeholder: data.type == "system_one" ? "Fast triage and routing" : "For questions needing more reasoning",
                                value: () => data.description,
                                oninput: (e) => (data.description = e.target.value),
                            }),
                        ),
                    ),
                );
            },
        ),
        t.footer(
            { className: "modal-footer" },
            t.button({ type: "button", className: "btn transparent secondary m-r-auto", onclick: () => app.modals.close(modal) }, t.span({ className: "txt" }, "Cancel")),
            t.button(
                {
                    type: "submit",
                    "html-form": "gpAIModelForm",
                    className: () => `btn expanded ${data.isSaving ? "loading" : ""}`,
                    disabled: () => data.isSaving || !data.provider,
                },
                t.span({ className: "txt" }, existing ? "Save" : "Create"),
            ),
        ),
    );
    modal.onafterclose = () => {
        providerWatcher?.unwatch();
        modelsWatcher?.unwatch();
        modal.remove();
    };
    document.body.appendChild(modal);
    app.modals.open(modal);
}

// models of the providers, per provider id (for the page session)
const modelsCache = {};

function compactNumber(n) {
    if (n >= 1e6) {
        return (n / 1e6).toLocaleString(undefined, { maximumFractionDigits: 1 }) + "M";
    }
    if (n >= 1e3) {
        return Math.round(n / 1e3) + "K";
    }
    return String(n);
}

function price(v) {
    return "$" + (v >= 1 ? v.toLocaleString(undefined, { maximumFractionDigits: 2 }) : v.toLocaleString(undefined, { maximumSignificantDigits: 2 }));
}

/** "1M context · $2 / $10 per M tokens · free" */
function modelMeta(m) {
    const parts = [];
    if (m.context) {
        parts.push(compactNumber(m.context) + " context");
    }
    if (m.inputPrice || m.outputPrice) {
        parts.push(`${price(m.inputPrice || 0)} / ${price(m.outputPrice || 0)} per M tokens (in / out)`);
    } else if (m.id.endsWith(":free")) {
        parts.push("free");
    }
    return parts.join(" · ");
}

/** Probability bars of one answer. */
export function probabilityBars(entries) {
    const top = Math.max(...entries.map(([, p]) => p));
    return t.div(
        { className: "gp-ai-bars" },
        entries.map(([label, p]) =>
            t.div(
                { className: `gp-ai-bar ${p == top ? "top" : ""}` },
                t.span({ className: "gp-ai-bar-label", title: label }, label),
                t.span({ className: "gp-ai-bar-track" }, t.span({ className: "gp-ai-bar-fill", style: `width: ${Math.round(p * 100)}%` })),
                t.span({ className: "gp-ai-bar-value" }, Math.round(p * 100) + "%"),
            )
        ),
    );
}

function openTestResult(m, res) {
    const answers = res.response.answers;
    const modal = t.div(
        { className: "modal popup gp-ai-modal" },
        t.header({ className: "modal-header" }, t.h5({ className: "m-auto" }, `Test of ${m.name}`)),
        t.div(
            { className: "modal-content" },
            t.div(
                { className: "flex gap-10 m-b-sm" },
                typeBadge(m.type),
                t.code(null, res.response.model),
                t.span({ className: "txt-hint" }, `${res.durationMs} ms`),
                res.response.calibrated ? null : t.span({ className: "label sm warning", title: "Probabilities estimated by the model itself" }, "uncalibrated"),
            ),
            t.div({ className: "gp-panel m-b-sm" }, t.div({ className: "gp-label" }, "Message"), t.div(null, res.request.state)),
            res.request.questions.map((q) => {
                const a = answers[q.key] || {};
                let body;
                if (q.type == "noul") {
                    body = probabilityBars([["yes", a.noul || 0], ["no", 1 - (a.noul || 0)]]);
                } else if (q.type == "choice") {
                    body = probabilityBars(q.options.map((o) => [o.key, a.probabilities?.[o.key] || 0]));
                } else {
                    body = probabilityBars(q.levels.map((l, i) => [l, a.probabilities?.[i] || 0]));
                }
                return t.div(
                    { className: "m-b-sm" },
                    t.div(
                        { className: "flex gap-10" },
                        t.strong({ className: "m-r-auto" }, q.instructions),
                        t.span({ className: "txt-hint txt-sm" }, `confidence ${Math.round((a.confidence || 0) * 100)}%`),
                    ),
                    body,
                );
            }),
        ),
        t.footer(
            { className: "modal-footer" },
            t.button({ type: "button", className: "btn transparent secondary m-l-auto", onclick: () => app.modals.close(modal) }, t.span({ className: "txt" }, "Close")),
        ),
    );
    modal.onafterclose = () => modal.remove();
    document.body.appendChild(modal);
    app.modals.open(modal);
}

// =======================================================================================
// assistant

export function pageAIAssistant() {
    app.store.title = "AI assistant";
    loadAI();

    const data = store({ id: "", enabled: false, model: "", instructions: "", loaded: false, isSaving: false, initial: "" });
    const serialized = () => JSON.stringify([data.enabled, data.model, data.instructions]);

    async function load() {
        try {
            const res = await app.pb.collection("ai_settings").getList(1, 1, { sort: "created", requestKey: null });
            const s = res.items[0] || (await app.pb.collection("ai_settings").create({}));
            Object.assign(data, { id: s.id, enabled: !!s.assistant_enabled, model: s.assistant_model || "", instructions: s.instructions || "" });
        } catch (err) {
            handleError(err);
        }
        data.initial = serialized();
        data.loaded = true;
    }

    async function save() {
        data.isSaving = true;
        try {
            await app.pb.collection("ai_settings").update(data.id, {
                assistant_enabled: data.enabled,
                assistant_model: data.model,
                instructions: data.instructions,
            });
            data.initial = serialized();
            app.toasts.success("Successfully saved the assistant settings.");
        } catch (err) {
            handleError(err);
        }
        data.isSaving = false;
    }

    load();

    const standardModels = () => shared.models.filter((m) => m.type == "standard");

    return settingsPage(
        { pbEvent: "gpPageAIAssistant", title: "AI assistant" },
        t.p(
            { className: "txt-hint m-t-0" },
            "The ",
            t.strong(null, "Assistant"),
            " button of the header opens a chat that reads your data (schema, records, totals) and prepares dashboards, email templates, campaigns and records. ",
            "It never changes anything by itself: every change is a card that you apply.",
        ),
        () => {
            if (!data.loaded) {
                return t.div({ rid: "loading", className: "txt-center" }, t.span({ className: "loader" }));
            }
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
                    t.div(
                        { className: "field" },
                        t.input({ id: "assistant_enabled", type: "checkbox", className: "switch", checked: () => data.enabled, onchange: (e) => (data.enabled = e.target.checked) }),
                        t.label({ htmlFor: "assistant_enabled" }, t.span({ className: "txt" }, "Enable the assistant")),
                    ),
                ),
                t.div(
                    { className: "col-lg-12" },
                    () => {
                        if (shared.loaded && !standardModels().length) {
                            return t.div(
                                { rid: "nomodel", className: "alert warning" },
                                t.span({ className: "m-r-auto" }, "The assistant needs a Standard model supporting tool calling, e.g. claude-opus-5-5 (Anthropic), gpt-5-mini (OpenAI) or any of them through OpenRouter."),
                                t.button({ type: "button", className: "btn sm", onclick: () => openModelModal({ onsaved: (m) => m.type == "standard" && (data.model = m.id) }) }, t.span({ className: "txt" }, "Add a model")),
                            );
                        }
                        return field(
                            { label: "Model", name: "assistant_model", hint: "A Standard model that supports tool calling. Bigger models plan better; smaller ones are faster and cheaper." },
                            select({
                                name: "assistant_model",
                                placeholder: "- Select model -",
                                options: () => standardModels().map((m) => ({ value: m.id, label: `${m.name} (${m.model})` })),
                                value: () => data.model,
                                onchange: (v) => (data.model = v),
                            }),
                        );
                    },
                ),
                t.div(
                    { className: "col-lg-12" },
                    field(
                        {
                            label: "Business notes",
                            name: "instructions",
                            hint: "What the schema doesn't say: definitions, conventions, tone. The assistant reads them before every answer.",
                        },
                        t.textarea({
                            id: "instructions",
                            name: "instructions",
                            rows: 10,
                            placeholder: "- A paid order is status = \"paid\"; revenue is the sum of orders.total (EUR, VAT included).\n- Customers are in the customers collection; their address is email.\n- Promo codes live in promo_codes (code, percent, expires).\n- Emails: friendly, short, signed \"The Acme team\". Answer in French.",
                            value: () => data.instructions,
                            oninput: (e) => (data.instructions = e.target.value),
                        }),
                    ),
                ),
                t.div(
                    { className: "col-lg-12 flex" },
                    t.span({ className: "txt-hint m-r-auto" }, "Conversations are private to each superuser and kept in ai_conversations."),
                    t.button(
                        { type: "submit", className: () => `btn expanded ${data.isSaving ? "loading" : ""}`, disabled: () => data.isSaving || serialized() == data.initial },
                        t.span({ className: "txt" }, "Save changes"),
                    ),
                ),
            );
        },
    );
}
