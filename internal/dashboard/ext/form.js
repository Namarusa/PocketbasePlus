// Public form page (/f/{slug}) and iframe embed (/f/{slug}/embed).
// Shablon only: the page runs outside of the superuser dashboard.

const body = document.body;
const slug = body.dataset.slug;
const embed = body.dataset.mode == "embed";
const AUTH_KEY = "gpFormAuth";
// the dashboard preview (?preview=1) uses the superuser session of the same site
const preview = new URLSearchParams(window.location.search).get("preview") == "1";

function superuserToken() {
    try {
        return JSON.parse(localStorage.getItem("__pb_superusers__/_") || "null")?.token || "";
    } catch {
        return "";
    }
}

function apiURL(path) {
    return preview ? path + "?preview=1" : path;
}

function apiHeaders() {
    if (state.auth) {
        return { Authorization: state.auth.token };
    }
    if (preview && superuserToken()) {
        return { Authorization: superuserToken() };
    }
    return {};
}

// --- color scheme -------------------------------------------------------------------------

const media = window.matchMedia("(prefers-color-scheme: dark)");
function applyScheme(theme) {
    let scheme = theme == "light" || theme == "dark" ? theme : "";
    if (!scheme) {
        try {
            scheme = localStorage.getItem("pbColorScheme") || "";
        } catch {}
    }
    document.documentElement.setAttribute("data-color-scheme", scheme || (media.matches ? "dark" : "light"));
}
applyScheme("auto");

// --- embed height (the parent page resizes the iframe) ------------------------------------------

if (embed && window.parent !== window) {
    let last = 0;
    const report = () => {
        const height = Math.ceil(document.documentElement.scrollHeight);
        if (height != last) {
            last = height;
            window.parent.postMessage({ type: "pocketbaseplus-form:height", slug, height }, "*");
        }
    };
    new ResizeObserver(report).observe(document.documentElement);
    window.addEventListener("load", report);
}

// --- auth -----------------------------------------------------------------------------------

function readAuth() {
    try {
        const raw = JSON.parse(localStorage.getItem(AUTH_KEY) || "null");
        if (raw?.token) {
            const payload = JSON.parse(atob(raw.token.split(".")[1].replace(/-/g, "+").replace(/_/g, "/")));
            if (payload.exp * 1000 > Date.now()) {
                return raw;
            }
        }
    } catch {}
    return null;
}

function saveAuth(auth) {
    try {
        if (auth) {
            localStorage.setItem(AUTH_KEY, JSON.stringify(auth));
        } else {
            localStorage.removeItem(AUTH_KEY);
        }
    } catch {}
}

// --- state ----------------------------------------------------------------------------------

const state = store({
    form: null,
    loading: true,
    notFound: false,
    auth: readAuth(),
    values: {},
    files: {},
    errors: {},
    message: "",
    sending: false,
    done: false,
    draftError: "", // editor live preview
    version: 0, // re-renders the form when its definition changes
});

// new fields get an empty value (existing values are kept while editing)
function initValues(form) {
    const values = { ...state.values };
    for (const f of form.fields) {
        if (!(f.name in values)) {
            values[f.name] = f.type == "bool" ? false : f.multiple ? [] : "";
        }
    }
    state.values = values;
}

function applyForm(form) {
    state.form = form;
    state.version++;
    applyScheme(form.theme);
    document.documentElement.style.setProperty("--gpFormAccent", form.accent || "var(--primaryColor)");
    document.title = form.title || "Form";
    initValues(form);
}

async function loadForm() {
    try {
        const res = await fetch(apiURL(`/api/forms/${encodeURIComponent(slug)}`), { headers: apiHeaders() });
        if (!res.ok) {
            state.notFound = true;
            return;
        }
        applyForm(await res.json());
    } catch {
        state.notFound = true;
    } finally {
        state.loading = false;
    }
}

// --- submit ---------------------------------------------------------------------------------

function pageQuery() {
    const out = {};
    for (const [k, v] of new URLSearchParams(window.location.search)) {
        out[k] = v;
    }
    return out;
}

async function send(retried = false) {
    const form = state.form;
    state.sending = true;
    state.errors = {};
    state.message = "";
    const data = new FormData();
    data.append("_token", form.token);
    data.append("_query", JSON.stringify(pageQuery()));
    data.append(form.honeypot, honeypotEl?.value || "");
    for (const f of form.fields) {
        const v = state.values[f.name];
        if (f.type == "file") {
            for (const file of state.files[f.name] || []) {
                data.append(f.name, file);
            }
        } else if (Array.isArray(v)) {
            v.forEach((item) => data.append(f.name, item));
        } else if (f.type == "bool") {
            data.append(f.name, v ? "true" : "false");
        } else if (v !== "" && v !== null && v !== undefined) {
            data.append(f.name, v);
        }
    }
    try {
        const res = await fetch(apiURL(`/api/forms/${encodeURIComponent(slug)}/submit`), { method: "POST", body: data, headers: apiHeaders() });
        const out = await res.json().catch(() => ({}));
        if (res.ok) {
            if (out.redirectUrl) {
                redirect(out.redirectUrl);
                return;
            }
            state.done = true;
            state.message = out.message || "Thank you, your answer was sent.";
            window.scrollTo({ top: 0 });
            return;
        }
        if (res.status == 401) {
            saveAuth(null);
            state.auth = null;
        }
        // the token expired (page open for hours): get a new one and retry once
        if (res.status == 400 && /expired/i.test(out.message || "") && !retried) {
            await loadForm();
            return send(true);
        }
        const errors = {};
        for (const [k, v] of Object.entries(out.data || {})) {
            errors[k] = v?.message || "Invalid value.";
        }
        state.errors = errors;
        state.message = out.message || "The form couldn't be sent.";
    } catch {
        state.message = "The form couldn't be sent. Check your connection and try again.";
    } finally {
        state.sending = false;
    }
}

function redirect(url) {
    try {
        if (embed && window.top !== window) {
            window.top.location.href = url;
            return;
        }
    } catch {}
    window.location.href = url;
}

function reset() {
    const values = {};
    for (const f of state.form.fields) {
        values[f.name] = f.type == "bool" ? false : f.multiple ? [] : "";
    }
    state.values = values;
    state.files = {};
    state.done = false;
    state.message = "";
    loadForm(); // fresh token
}

// --- inputs ---------------------------------------------------------------------------------

let seq = 0;

function formatSize(bytes) {
    if (!bytes) {
        return "";
    }
    return bytes >= 1 << 20 ? `${Math.round((bytes / (1 << 20)) * 10) / 10} MB` : `${Math.round(bytes / 1024)} KB`;
}

/** Searchable list of options (long selects, relations). */
function searchSelect(f, id) {
    const ui = store({ search: "", open: false });
    const selected = () => [].concat(state.values[f.name] || []).filter(Boolean);
    const labelOf = (v) => f.options.find((o) => o.value == v)?.label || v;
    const toggle = (value) => {
        if (f.multiple) {
            const list = selected();
            state.values[f.name] = list.includes(value) ? list.filter((x) => x != value) : [...list, value].slice(0, f.maxSelect || 99);
        } else {
            state.values[f.name] = value;
            document.getElementById(id + "_list")?.hidePopover();
        }
    };
    return t.div(
        { className: "gp-fsel" },
        t.button(
            { type: "button", id, className: "gp-fsel-btn", "html-popovertarget": id + "_list" },
            t.span({ className: "txt" }, () => (selected().length ? selected().map(labelOf).join(", ") : f.placeholder || "Select…")),
            t.i({ className: "ri-arrow-down-s-line", ariaHidden: true }),
        ),
        t.div(
            { id: id + "_list", popover: "auto", className: "dropdown gp-fsel-list" },
            f.options.length > 8
                ? t.div({ className: "field gp-fsel-search" }, t.input({ type: "search", placeholder: "Search…", value: () => ui.search, oninput: (e) => (ui.search = e.target.value) }))
                : null,
            () => {
                const q = ui.search.trim().toLowerCase();
                const items = f.options.filter((o) => !q || o.label.toLowerCase().includes(q)).slice(0, 200);
                return t.div(
                    { rid: "items-" + q + "-" + selected().join("|") },
                    items.length
                        ? items.map((o) =>
                            t.button(
                                { type: "button", className: `dropdown-item ${selected().includes(o.value) ? "active" : ""}`, onclick: () => toggle(o.value) },
                                f.multiple ? t.i({ className: selected().includes(o.value) ? "ri-checkbox-line" : "ri-checkbox-blank-line", ariaHidden: true }) : null,
                                t.span({ className: "txt" }, o.label),
                            )
                        )
                        : t.div({ className: "txt-hint p-sm" }, "No results."),
                );
            },
        ),
    );
}

function input(f) {
    const id = "gpf_" + f.name + "_" + seq++;
    const common = {
        id,
        name: f.name,
        required: !!f.required,
        placeholder: f.placeholder || "",
        oninput: (e) => (state.values[f.name] = e.target.value),
        value: () => state.values[f.name] ?? "",
    };
    switch (f.type) {
        case "email":
            return { id, el: t.input({ ...common, type: "email", autocomplete: "email" }) };
        case "url":
            return { id, el: t.input({ ...common, type: "url" }) };
        case "number":
            return { id, el: t.input({ ...common, type: "number", step: f.onlyInt ? "1" : "any", min: f.min ?? undefined, max: f.max ?? undefined }) };
        case "date":
            return { id, el: t.input({ ...common, type: "date" }) };
        case "editor":
            return { id, el: t.textarea({ ...common, rows: 5, maxLength: f.max || undefined }) };
        case "bool":
            return {
                id,
                inline: true,
                el: t.input({ id, type: "checkbox", className: "switch", name: f.name, checked: () => !!state.values[f.name], onchange: (e) => (state.values[f.name] = e.target.checked) }),
            };
        case "file": {
            const accept = (f.mimeTypes || []).join(",");
            return {
                id,
                el: t.input({
                    id,
                    type: "file",
                    name: f.name,
                    required: !!f.required,
                    multiple: !!f.multiple,
                    accept: accept || undefined,
                    onchange: (e) => {
                        const files = [...e.target.files];
                        const tooBig = f.maxSize && files.find((x) => x.size > f.maxSize);
                        if (tooBig) {
                            state.errors = { ...state.errors, [f.name]: `${tooBig.name} is too big (max ${formatSize(f.maxSize)}).` };
                            e.target.value = "";
                            state.files[f.name] = [];
                            return;
                        }
                        const errors = { ...state.errors };
                        delete errors[f.name];
                        state.errors = errors;
                        state.files[f.name] = files.slice(0, f.maxSelect || 99);
                    },
                }),
                hint: [f.maxSize ? `Max ${formatSize(f.maxSize)}` : "", f.multiple ? `up to ${f.maxSelect} files` : ""].filter(Boolean).join(", "),
            };
        }
        case "select":
        case "relation": {
            const few = f.options.length <= (f.multiple ? 8 : 6) && f.type == "select";
            if (!few) {
                return { id, el: searchSelect(f, id), custom: true };
            }
            const selected = () => [].concat(state.values[f.name] || []);
            return {
                id,
                custom: true,
                el: t.div(
                    { className: "gp-choices", role: f.multiple ? "group" : "radiogroup" },
                    f.options.map((o, i) =>
                        t.label(
                            { className: "gp-choice" },
                            t.input({
                                type: f.multiple ? "checkbox" : "radio",
                                name: f.name,
                                value: o.value,
                                required: !f.multiple && !!f.required && i == 0,
                                checked: () => selected().includes(o.value),
                                onchange: (e) => {
                                    if (f.multiple) {
                                        const list = selected().filter((x) => x != o.value);
                                        state.values[f.name] = e.target.checked ? [...list, o.value] : list;
                                    } else {
                                        state.values[f.name] = o.value;
                                    }
                                },
                            }),
                            t.span(null, o.label),
                        )
                    ),
                ),
            };
        }
    }
    return { id, el: t.input({ ...common, type: "text", minLength: f.min || undefined, maxLength: f.max || undefined }) };
}

function fieldEl(f) {
    const { id, el, inline, custom, hint } = input(f);
    const help = [f.help, hint].filter(Boolean).join(" · ");
    return t.div(
        { className: `gp-form-field ${f.width == "half" ? "half" : ""}` },
        inline
            ? t.div({ className: "field gp-form-switch" }, el, t.label({ htmlFor: id }, t.span({ className: "txt" }, f.label, f.required ? t.span({ className: "gp-req" }, " *") : null)))
            : custom
                ? t.div(
                    { className: "gp-form-custom" },
                    t.label({ className: "gp-form-label", htmlFor: id }, f.label, f.required ? t.span({ className: "gp-req" }, " *") : null),
                    el,
                )
                : t.div({ className: "field" }, t.label({ htmlFor: id }, f.label), el),
        help ? t.div({ className: "field-help" }, help) : null,
        t.div({ className: "gp-form-error", hidden: () => !state.errors[f.name] }, () => state.errors[f.name] || ""),
    );
}

// --- views ----------------------------------------------------------------------------------

/** Simple text with paragraphs and links (no HTML). */
function richText(text) {
    return String(text || "")
        .split(/\n{2,}/)
        .map((p) =>
            t.p(
                null,
                p.split(/(https?:\/\/[^\s]+)/g).map((part) => (/^https?:\/\//.test(part) ? t.a({ href: part, target: "_blank", rel: "noopener" }, part) : part)),
            )
        );
}

function loginView() {
    const form = store({ email: "", password: "", error: "", loading: false });
    async function submit(e) {
        e.preventDefault();
        form.loading = true;
        form.error = "";
        try {
            const res = await fetch("/api/collections/users/auth-with-password", {
                method: "POST",
                headers: { "Content-Type": "application/json" },
                body: JSON.stringify({ identity: form.email, password: form.password }),
            });
            const out = await res.json();
            if (!res.ok) {
                throw new Error(res.status == 400 ? "Invalid email or password." : out.message);
            }
            const auth = { token: out.token, email: out.record?.email };
            saveAuth(auth);
            state.auth = auth;
        } catch (err) {
            form.error = err.message;
        } finally {
            form.loading = false;
        }
    }
    return t.form(
        { className: "gp-form-login", onsubmit: submit },
        t.p({ className: "txt-hint" }, "Sign in to fill this form."),
        t.div({ className: "field" }, t.label({ htmlFor: "gpfEmail" }, "Email"), t.input({ id: "gpfEmail", type: "email", required: true, autocomplete: "username", value: () => form.email, oninput: (e) => (form.email = e.target.value) })),
        t.div({ className: "field" }, t.label({ htmlFor: "gpfPass" }, "Password"), t.input({ id: "gpfPass", type: "password", required: true, autocomplete: "current-password", value: () => form.password, oninput: (e) => (form.password = e.target.value) })),
        t.div({ className: "alert danger", hidden: () => !form.error }, () => form.error),
        t.button({ type: "submit", className: () => `btn block gp-form-submit ${form.loading ? "loading" : ""}`, disabled: () => form.loading }, t.span({ className: "txt" }, "Sign in")),
    );
}

let honeypotEl;

function formView(form) {
    return t.form(
        {
            className: "gp-form",
            noValidate: false,
            onsubmit: (e) => {
                e.preventDefault();
                send();
            },
        },
        // hidden from people (bots fill it)
        t.div(
            { className: "gp-form-hp", ariaHidden: true },
            t.label(null, "Website", (honeypotEl = t.input({ type: "text", name: form.honeypot, tabIndex: -1, autocomplete: "off" }))),
        ),
        t.div({ className: "gp-form-grid" }, form.fields.map(fieldEl)),
        t.div({ className: "alert danger m-t-sm", hidden: () => !state.message || state.done }, () => state.message),
        t.button(
            { type: "submit", className: () => `btn lg block gp-form-submit ${state.sending ? "loading" : ""}`, disabled: () => state.sending || !!state.form?.draft },
            t.span({ className: "txt" }, form.submitLabel || "Submit"),
        ),
        () => (state.form?.draft ? t.div({ rid: "dnote", className: "txt-hint txt-sm txt-center m-t-5" }, "Preview of unsaved changes: save the form to test answers.") : null),
    );
}

const root = t.div(
    { className: "gp-form-wrap" },
    () => {
        if (state.loading) {
            return t.div({ rid: "loading", className: "txt-center p-base" }, t.span({ className: "loader" }));
        }
        if (state.notFound || !state.form) {
            return t.div({ rid: "missing", className: "gp-form-card txt-center" }, t.h5(null, "This form is not available."));
        }
        const form = state.form;
        return t.div(
            // keyed by version: an element kept by rid wouldn't show the new definition
            { rid: "form-" + form.id + "-" + state.version, className: "gp-form-card" },
            () => (state.draftError ? t.div({ rid: "derr", className: "alert danger m-b-sm" }, state.draftError) : null),
            form.title ? t.h3({ className: "gp-form-title" }, form.title) : null,
            form.description ? t.div({ className: "gp-form-description" }, richText(form.description)) : null,
            () => {
                if (form.requireAuth && !state.auth) {
                    return t.div({ rid: "login" }, loginView());
                }
                if (state.done) {
                    return t.div(
                        { rid: "done", className: "gp-form-done" },
                        t.i({ className: "ri-checkbox-circle-line", ariaHidden: true }),
                        t.div(null, richText(state.message)),
                        t.button({ type: "button", className: "btn secondary", onclick: reset }, t.span({ className: "txt" }, "Send another answer")),
                    );
                }
                return t.div(
                    { rid: "form" },
                    form.requireAuth && state.auth
                        ? t.div(
                            { className: "gp-form-user txt-hint txt-sm" },
                            `Signed in as ${state.auth.email || "user"} · `,
                            t.a({ href: "#", onclick: (e) => (e.preventDefault(), saveAuth(null), (state.auth = null)) }, "Sign out"),
                        )
                        : null,
                    formView(form),
                );
            },
        );
    },
);

body.appendChild(root);
loadForm();

// editor live preview: the dashboard (same site) sends the unsaved settings
if (preview && window.parent !== window) {
    let seq = 0;
    window.addEventListener("message", async (e) => {
        if (e.origin !== window.location.origin || e.data?.type != "pocketbaseplus-form:draft" || e.data.slug != slug) {
            return;
        }
        const current = ++seq;
        try {
            const res = await fetch(`/api/forms/${encodeURIComponent(slug)}/draft`, {
                method: "POST",
                headers: { "Content-Type": "application/json", ...apiHeaders() },
                body: JSON.stringify(e.data.draft || {}),
            });
            const out = await res.json().catch(() => ({}));
            if (current != seq) {
                return; // a newer draft is on its way
            }
            if (!res.ok) {
                state.draftError = out.message || "Invalid settings.";
                return;
            }
            state.draftError = "";
            state.done = false;
            state.notFound = false;
            state.loading = false;
            applyForm(out);
        } catch {}
    });
    window.parent.postMessage({ type: "pocketbaseplus-form:ready", slug }, window.location.origin);
}
