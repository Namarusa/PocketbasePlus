// Settings → Secrets: the keys, tokens and passwords of the flows HTTP
// requests, the saved APIs and the AI providers, used as {{secret.NAME}}.
// The values are never sent back to the browser.

import { api, field, formatDate, handleError } from "./lib.js";
import { emptyRow, loadingRow, settingsPage } from "./aiSettings.js";

const nameRegex = /^[A-Z][A-Z0-9_]{0,63}$/;

// shared list (the page and the pickers of the other editors)
const shared = store({ list: [], uses: {}, loaded: false, status: null });

export async function loadSecrets() {
    try {
        const [list, uses, status] = await Promise.all([
            app.pb.collection("secrets").getFullList({ sort: "name", requestKey: null }),
            api("/api/secrets/uses", { requestKey: null }),
            api("/api/secrets/status", { requestKey: null }),
        ]);
        shared.list = list;
        shared.uses = uses || {};
        shared.status = status;
    } catch (err) {
        handleError(err);
    }
    shared.loaded = true;
    return shared;
}

/** The secrets ({name, hint, description}), loaded once. */
export function secretsList() {
    if (!shared.loaded) {
        loadSecrets();
    }
    return shared.list;
}

/** The reference of a secret in a template. */
export function secretRef(name) {
    return `{{secret.${name}}}`;
}

/** A secret name from a label: "X-API-Key" → "X_API_KEY". */
export function secretName(label) {
    let name = String(label || "").toUpperCase().replace(/[^A-Z0-9]+/g, "_").replace(/^_+|_+$/g, "");
    if (!/^[A-Z]/.test(name)) {
        name = "KEY_" + name;
    }
    name = name.slice(0, 60).replace(/_+$/, "");
    const taken = new Set(shared.list.map((s) => s.name));
    let out = name;
    for (let n = 2; taken.has(out); n++) {
        out = `${name}_${n}`;
    }
    return out;
}

// names of the values that look secret (as the run logs of the flows)
const secretKeyRegex = /(pass(word|wd|code|phrase)?$|^pwd$|secret|token|api[_-]?key|authorization|cookie|^key$|access[_-]?key)/i;

/** Whether a typed value should rather be a secret (a key name that looks secret, a plain value). */
export function looksSecret(key, value) {
    value = String(value ?? "").trim();
    return secretKeyRegex.test(key || "") && value.length >= 6 && !value.includes("{{");
}

// =======================================================================================
// page

export function pageSecrets() {
    app.store.title = "Secrets";
    loadSecrets();

    function remove(s) {
        const uses = shared.uses[s.name] || [];
        if (uses.length) {
            app.toasts.error(`The secret is used by ${uses.join(", ")}: change them first.`);
            return;
        }
        app.modals.confirm(`Do you really want to delete the secret "${s.name}"?`, async () => {
            try {
                await app.pb.collection("secrets").delete(s.id);
                app.toasts.success("Successfully deleted the secret.");
                loadSecrets();
            } catch (err) {
                handleError(err);
            }
        });
    }

    async function copy(s) {
        try {
            await navigator.clipboard.writeText(secretRef(s.name));
            app.toasts.success(`Copied ${secretRef(s.name)}`);
        } catch {
            app.toasts.info(secretRef(s.name));
        }
    }

    return settingsPage(
        {
            pbEvent: "gpPageSecrets",
            title: "Secrets",
            buttons: t.button(
                { type: "button", className: "btn", onclick: () => openSecretModal() },
                t.i({ className: "ri-add-line", ariaHidden: true }),
                t.span({ className: "txt" }, "New secret"),
            ),
        },
        t.p(
            { className: "txt-hint m-t-0" },
            "Keys, tokens and passwords entered once and used as ",
            t.code(null, "{{secret.NAME}}"),
            " in the HTTP requests of the flows, the sign-in of the APIs and the AI providers. A value is never shown again, and it's hidden in the run logs.",
        ),
        () =>
            shared.status && !shared.status.encrypted
                ? t.div(
                    { rid: "plain", className: "alert warning m-b-sm" },
                    t.div(
                        { className: "content" },
                        t.p(null, t.strong(null, "The secrets are stored unencrypted. ")),
                        t.p(null, "Set ", t.code(null, shared.status.env), " to 32 random characters (e.g. the output of openssl rand -hex 16) and restart: they're encrypted on start. Keep that key: without it the secrets can't be read."),
                    ),
                )
                : null,
        t.div(
            { className: "gp-table-wrap" },
            t.table(
                { className: "gp-secrets-table" },
                t.thead(null, t.tr(null, t.th(null, "Name"), t.th(null, "Value"), t.th(null, "Used by"), t.th(null, "Updated"), t.th(null, ""))),
                t.tbody(null, () => {
                    if (!shared.loaded) {
                        return loadingRow(5);
                    }
                    if (!shared.list.length) {
                        return emptyRow(5, "No secrets yet.", "empty");
                    }
                    return shared.list.map((s) => {
                        const uses = shared.uses[s.name] || [];
                        return t.tr(
                            { rid: s.id + s.updated + uses.join() },
                            t.td(
                                null,
                                t.button(
                                    { type: "button", className: "btn sm transparent secondary gp-secret-name", ariaLabel: app.attrs.tooltip("Copy " + secretRef(s.name)), onclick: () => copy(s) },
                                    t.span({ className: "txt txt-code" }, s.name),
                                    t.i({ className: "ri-file-copy-line", ariaHidden: true }),
                                ),
                                s.description ? t.div({ className: "txt-hint txt-sm" }, s.description) : null,
                            ),
                            t.td(
                                { className: "txt-nowrap" },
                                t.code(null, s.hint || "…"),
                                // stored before the key was set (encrypted at the next start)
                                s.encrypted || !shared.status?.encrypted ? null : t.span({ className: "label sm warning m-l-5" }, "Unencrypted"),
                            ),
                            t.td(
                                { className: "txt-sm" },
                                uses.length ? uses.map((u) => t.div(null, u)) : t.span({ className: "txt-hint" }, "Not used"),
                            ),
                            t.td({ className: "txt-hint txt-sm txt-nowrap" }, formatDate(s.updated)),
                            t.td(
                                { className: "txt-right txt-nowrap" },
                                t.button(
                                    { type: "button", className: "btn sm circle transparent secondary", ariaLabel: app.attrs.tooltip("Edit"), onclick: () => openSecretModal({ existing: s }) },
                                    t.i({ className: "ri-pencil-line", ariaHidden: true }),
                                ),
                                t.button(
                                    { type: "button", className: "btn sm circle transparent danger", ariaLabel: app.attrs.tooltip("Delete"), onclick: () => remove(s) },
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

// =======================================================================================
// modal

/**
 * New secret (or a new value / description of an existing one). onsaved
 * receives the saved secret.
 */
export function openSecretModal({ existing = null, name = "", value = "", description = "", onsaved = null } = {}) {
    const data = store({
        name: existing?.name || name,
        value,
        description: existing?.description ?? description,
        show: false,
        isSaving: false,
    });
    const nameError = () => (data.name && !nameRegex.test(data.name) ? "Capital letters, digits and _, starting with a letter." : "");

    async function save() {
        if (nameError()) {
            return;
        }
        data.isSaving = true;
        try {
            const body = { description: data.description };
            if (!existing) {
                body.name = data.name;
            }
            // an empty value keeps the current one
            if (data.value || !existing) {
                body.value = data.value;
            }
            const saved = existing
                ? await app.pb.collection("secrets").update(existing.id, body)
                : await app.pb.collection("secrets").create(body);
            app.toasts.success("Successfully saved the secret.");
            app.modals.close(modal);
            await loadSecrets();
            onsaved?.(saved);
        } catch (err) {
            handleError(err);
        }
        data.isSaving = false;
    }

    const modal = t.div(
        { className: "modal popup gp-secret-modal" },
        t.header({ className: "modal-header" }, t.h5({ className: "m-auto" }, existing ? `Secret ${existing.name}` : "New secret")),
        t.form(
            {
                id: "gpSecretForm",
                className: "modal-content",
                inert: () => data.isSaving,
                onsubmit: (e) => {
                    e.preventDefault();
                    save();
                },
            },
            existing
                ? null
                : field(
                    { label: "Name", name: "name", hint: () => nameError() || t.span(null, "Used as ", t.code(null, secretRef(data.name || "NAME")), ". It can't change later.") },
                    t.input({
                        id: "name",
                        name: "name",
                        type: "text",
                        className: "txt-code",
                        required: true,
                        autocomplete: "off",
                        placeholder: "OPENAI_KEY",
                        value: () => data.name,
                        oninput: (e) => {
                            // typed in capitals, spaces and dashes as _
                            const pos = e.target.selectionStart;
                            data.name = e.target.value.toUpperCase().replace(/[\s-]/g, "_");
                            e.target.value = data.name;
                            e.target.setSelectionRange(pos, pos);
                        },
                    }),
                ),
            field(
                {
                    label: "Value",
                    name: "value",
                    hint: existing ? `Stored (${existing.hint || "…"}): leave empty to keep it.` : "Never shown again after saving.",
                },
                t.div(
                    { className: "gp-api-secret" },
                    t.input({
                        id: "value",
                        name: "value",
                        type: () => (data.show ? "text" : "password"),
                        autocomplete: "new-password",
                        required: !existing,
                        placeholder: existing ? "Unchanged" : "",
                        value: () => data.value,
                        oninput: (e) => (data.value = e.target.value),
                        onmount: (el) => setTimeout(() => (existing || name) && el.focus(), 0),
                    }),
                    t.button(
                        { type: "button", className: "btn xs circle transparent secondary", ariaLabel: app.attrs.tooltip("Show / hide what you type"), onclick: () => (data.show = !data.show) },
                        t.i({ className: () => (data.show ? "ri-eye-off-line" : "ri-eye-line"), ariaHidden: true }),
                    ),
                ),
            ),
            field(
                { label: "Description (optional)", name: "description" },
                t.input({
                    id: "description",
                    name: "description",
                    type: "text",
                    placeholder: "e.g. Production key of the shop API",
                    value: () => data.description,
                    oninput: (e) => (data.description = e.target.value),
                }),
            ),
        ),
        t.footer(
            { className: "modal-footer" },
            t.button({ type: "button", className: "btn transparent secondary m-r-auto", onclick: () => app.modals.close(modal) }, t.span({ className: "txt" }, "Cancel")),
            t.button(
                { type: "submit", "html-form": "gpSecretForm", className: () => `btn expanded ${data.isSaving ? "loading" : ""}`, disabled: () => data.isSaving || !!nameError() },
                t.span({ className: "txt" }, existing ? "Save" : "Create"),
            ),
        ),
    );
    modal.onafterclose = () => modal.remove();
    document.body.appendChild(modal);
    app.modals.open(modal);
}

// =======================================================================================
// picker

let pickerCount = 0;

/**
 * A 🔑 button listing the secrets: onpick receives the name of the chosen
 * one ("New secret…" creates one first; suggest gives its name and value).
 */
export function secretPicker({ onpick, suggest = () => ({}), label = "" }) {
    const id = "gpSecretPicker" + ++pickerCount;
    const close = (e) => e.currentTarget.closest(".dropdown")?.hidePopover();
    return t.div(
        { className: "gp-secret-picker" },
        t.button(
            {
                type: "button",
                className: `btn ${label ? "sm" : "xs circle"} transparent secondary`,
                "html-popovertarget": id,
                ariaLabel: label ? undefined : app.attrs.tooltip("Use a secret"),
                onclick: () => loadSecrets(),
            },
            t.i({ className: "ri-key-2-line", ariaHidden: true }),
            label ? t.span({ className: "txt" }, label) : null,
        ),
        t.div(
            { id, popover: "auto", className: "dropdown gp-secret-dropdown" },
            () =>
                t.div(
                    { rid: "s-" + shared.list.map((s) => s.name).join() },
                    shared.list.length
                        ? shared.list.map((s) =>
                            t.button(
                                { type: "button", className: "dropdown-item", onclick: (e) => (close(e), onpick(s.name)) },
                                t.i({ className: "ri-key-2-line", ariaHidden: true }),
                                t.span({ className: "txt txt-code" }, s.name),
                                s.description ? t.small({ className: "txt-hint txt-ellipsis" }, s.description) : null,
                            )
                        )
                        : t.div({ className: "txt-hint txt-sm p-sm" }, shared.loaded ? "No secrets yet." : "Loading…"),
                    t.hr(),
                    t.button(
                        {
                            type: "button",
                            className: "dropdown-item",
                            onclick: (e) => {
                                close(e);
                                openSecretModal({ ...suggest(), onsaved: (s) => onpick(s.name) });
                            },
                        },
                        t.i({ className: "ri-add-line", ariaHidden: true }),
                        t.span({ className: "txt" }, "New secret…"),
                    ),
                ),
        ),
    );
}
