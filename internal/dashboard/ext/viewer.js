// Read-only dashboards viewer for role users, served at /dashboards.
// Users sign in with their account; the API only returns the dashboards
// shared with their roles and the data their collection rules allow.

import { dashboardGrid, toggleFullscreen } from "./grid.js";
import { initialValues, storeValues, variablesBar } from "./variables.js";

const AUTH_KEY = "gpDashboardsAuth";
const SUPERUSER_KEY = "__pb_superusers__/_"; // the superuser dashboard session (same origin)
const USERS_COLLECTION = "users";

// --- color scheme (shared with the superuser dashboard preference) --------------------

const media = window.matchMedia("(prefers-color-scheme: dark)");
function applyColorScheme() {
    let scheme = "";
    try {
        scheme = localStorage.getItem("pbColorScheme") || "";
    } catch {}
    document.documentElement.setAttribute("data-color-scheme", scheme || (media.matches ? "dark" : "light"));
}
applyColorScheme();
media.addEventListener("change", applyColorScheme);
window.addEventListener("storage", (e) => e.key == "pbColorScheme" && applyColorScheme());

function toggleColorScheme() {
    const next = document.documentElement.getAttribute("data-color-scheme") == "dark" ? "light" : "dark";
    try {
        localStorage.setItem("pbColorScheme", next);
    } catch {}
    applyColorScheme();
}

// --- auth ---------------------------------------------------------------------------

function readAuth(key) {
    try {
        const raw = JSON.parse(localStorage.getItem(key) || "null");
        if (raw?.token && !isExpired(raw.token)) {
            return { token: raw.token, record: raw.record || raw.model || null };
        }
    } catch {}
    return null;
}

function isExpired(token) {
    try {
        const payload = JSON.parse(atob(token.split(".")[1].replace(/-/g, "+").replace(/_/g, "/")));
        return !payload.exp || payload.exp * 1000 < Date.now();
    } catch {
        return true;
    }
}

const state = store({
    auth: readAuth(AUTH_KEY),
    route: "",
    dashboards: [],
    listLoaded: false,
    dashboard: null,
    results: {},
    loadingDashboard: false,
    refreshedAt: null,
    error: "",
    vars: {}, // dashboard variable values
});

// the variables bar reads and writes the current values (replaced per dashboard)
const varsProxy = new Proxy({}, {
    get: (_, key) => state.vars[key],
    set: (_, key, value) => {
        state.vars[key] = value;
        return true;
    },
    has: (_, key) => key in state.vars,
});

function saveAuth(auth) {
    state.auth = auth;
    try {
        if (auth && !auth.superuser) {
            localStorage.setItem(AUTH_KEY, JSON.stringify(auth));
        } else if (!auth) {
            localStorage.removeItem(AUTH_KEY);
        }
    } catch {}
}

function logout() {
    saveAuth(null);
    state.dashboards = [];
    state.listLoaded = false;
    state.dashboard = null;
    window.location.hash = "#/";
}

async function request(path, options = {}) {
    const headers = { "Content-Type": "application/json", ...(options.headers || {}) };
    if (state.auth?.token) {
        headers.Authorization = state.auth.token;
    }
    const res = await fetch(path, { ...options, headers });
    const body = await res.json().catch(() => ({}));
    if (res.status == 401 && state.auth) {
        logout();
    }
    if (!res.ok) {
        const err = new Error(body?.message || `Request failed (${res.status}).`);
        err.status = res.status;
        throw err;
    }
    return body;
}

// --- data ---------------------------------------------------------------------------

async function loadList() {
    try {
        state.dashboards = await request("/api/dashboards/shared");
    } catch (err) {
        state.error = err.message;
    } finally {
        state.listLoaded = true;
    }
}

let refreshTimer;

async function openDashboard(id) {
    clearInterval(refreshTimer);
    state.loadingDashboard = true;
    state.results = {};
    try {
        state.dashboard = await request(`/api/dashboards/${encodeURIComponent(id)}`);
        state.vars = initialValues(id, state.dashboard.variables);
        document.title = `${state.dashboard.name} - Dashboards`;
        await refresh();
        if (state.dashboard.refresh > 0) {
            refreshTimer = setInterval(() => !document.hidden && refresh(), state.dashboard.refresh * 1000);
        }
    } catch (err) {
        state.dashboard = null;
        state.error = err.status == 404 ? "This dashboard doesn't exist or isn't shared with you." : err.message;
    } finally {
        state.loadingDashboard = false;
    }
}

async function refresh() {
    const d = state.dashboard;
    if (!d) {
        return;
    }
    for (const p of d.panels) {
        state.results[p.id] = { ...(state.results[p.id] || {}), loading: true };
    }
    try {
        const res = await request(`/api/dashboards/${encodeURIComponent(d.id)}/data`, {
            method: "POST",
            body: JSON.stringify({ vars: JSON.parse(JSON.stringify(state.vars)) }),
        });
        const results = {};
        for (const p of d.panels) {
            const r = res.panels?.[p.id] || {};
            results[p.id] = { data: r.data ?? null, error: r.error || "", loading: false };
        }
        state.results = results;
        state.refreshedAt = new Date();
    } catch (err) {
        for (const p of d.panels) {
            state.results[p.id] = { ...(state.results[p.id] || {}), loading: false, error: err.message };
        }
    }
}

function route() {
    state.error = "";
    const id = window.location.hash.replace(/^#\/?/, "").split(/[/?]/)[0];
    state.route = id;
    if (!state.auth) {
        return;
    }
    if (id) {
        openDashboard(id);
    } else {
        clearInterval(refreshTimer);
        state.dashboard = null;
        document.title = "Dashboards";
        loadList();
    }
}
window.addEventListener("hashchange", route);

// --- views --------------------------------------------------------------------------

function loginView() {
    const form = store({ email: "", password: "", isLoading: false, error: "" });
    const superuser = readAuth(SUPERUSER_KEY);

    async function submit(e) {
        e.preventDefault();
        form.isLoading = true;
        form.error = "";
        try {
            const res = await request(`/api/collections/${USERS_COLLECTION}/auth-with-password`, {
                method: "POST",
                body: JSON.stringify({ identity: form.email, password: form.password }),
            });
            saveAuth({ token: res.token, record: res.record });
            route();
        } catch (err) {
            form.error = err.status == 400 ? "Invalid email or password." : err.message;
        } finally {
            form.isLoading = false;
        }
    }

    return t.div(
        { className: "gp-viewer-login" },
        t.form(
            { className: "gp-viewer-login-box", onsubmit: submit },
            t.div({ className: "gp-viewer-logo" }, t.i({ className: "ri-dashboard-line", ariaHidden: true }), t.span(null, "Dashboards")),
            t.p({ className: "txt-hint txt-center m-t-0" }, "Sign in to view the dashboards shared with you."),
            t.div(
                { className: "field" },
                t.label({ htmlFor: "gpEmail" }, "Email"),
                t.input({ id: "gpEmail", type: "email", required: true, autocomplete: "username", autofocus: true, value: () => form.email, oninput: (e) => (form.email = e.target.value) }),
            ),
            t.div(
                { className: "field" },
                t.label({ htmlFor: "gpPassword" }, "Password"),
                t.input({ id: "gpPassword", type: "password", required: true, autocomplete: "current-password", value: () => form.password, oninput: (e) => (form.password = e.target.value) }),
            ),
            t.div({ className: "alert danger", hidden: () => !form.error }, () => form.error),
            t.button({ type: "submit", className: () => `btn lg block ${form.isLoading ? "loading" : ""}`, disabled: () => form.isLoading }, t.span({ className: "txt" }, "Sign in")),
            superuser
                ? t.button(
                    { type: "button", className: "btn transparent secondary block", onclick: () => (saveAuth({ ...superuser, superuser: true }), route()) },
                    t.i({ className: "ri-shield-user-line", ariaHidden: true }),
                    t.span({ className: "txt" }, `Continue as superuser (${superuser.record?.email || ""})`),
                )
                : null,
        ),
    );
}

function header() {
    return t.header(
        { className: "gp-viewer-header" },
        t.a({ href: "#/", className: "gp-viewer-brand" }, t.i({ className: "ri-dashboard-line", ariaHidden: true }), t.span(null, "Dashboards")),
        () => (state.dashboard ? t.span({ rid: "crumb", className: "gp-viewer-crumb" }, t.i({ className: "ri-arrow-right-s-line", ariaHidden: true }), state.dashboard.name) : null),
        t.div(
            { className: "gp-viewer-header-actions" },
            t.button({ type: "button", className: "btn sm circle transparent secondary", title: "Toggle dark mode", onclick: toggleColorScheme }, t.i({ className: "ri-contrast-2-line", ariaHidden: true })),
            t.span({ className: "txt-hint txt-sm" }, () => state.auth?.record?.email || ""),
            t.button({ type: "button", className: "btn sm transparent secondary", onclick: logout }, t.i({ className: "ri-logout-box-r-line", ariaHidden: true }), t.span({ className: "txt" }, "Sign out")),
        ),
    );
}

function listView() {
    return t.div(
        { className: "gp-viewer-content" },
        t.h4({ className: "m-b-sm" }, "Your dashboards"),
        () => {
            if (!state.listLoaded) {
                return t.span({ rid: "loading", className: "loader" });
            }
            if (!state.dashboards.length) {
                return t.p({ rid: "empty", className: "txt-hint" }, "No dashboards are shared with you yet.");
            }
            return t.div(
                { rid: "cards", className: "gp-dashboard-cards" },
                state.dashboards.map((d) =>
                    t.a(
                        { href: "#/" + d.id, className: "gp-dashboard-card" },
                        t.i({ className: `${d.icon || "ri-dashboard-line"} gp-dashboard-card-icon`, ariaHidden: true }),
                        t.div({ className: "gp-dashboard-card-content" }, t.strong(null, d.name), t.span({ className: "txt-hint txt-sm" }, d.note || "")),
                    )
                ),
            );
        },
    );
}

function dashboardView() {
    let wrap;
    const grid = dashboardGrid({
        panels: () => state.dashboard?.panels || [],
        state: (p) => ({
            get data() {
                return state.results[p.id]?.data ?? null;
            },
            get error() {
                return state.results[p.id]?.error || "";
            },
            get loading() {
                return !!state.results[p.id]?.loading;
            },
        }),
        columns: state.dashboard?.columns || 24,
        emptyMessage: t.p({ className: "txt-hint" }, "This dashboard has no panels yet."),
    });
    return t.div(
        { className: "gp-viewer-content" },
        t.div(
            { className: "gp-viewer-toolbar" },
            t.div(
                null,
                t.h4({ className: "m-0 flex gap-10" }, () => (state.dashboard?.icon ? t.i({ rid: state.dashboard.icon, className: state.dashboard.icon, ariaHidden: true }) : null), () => state.dashboard?.name || ""),
                t.p({ className: "txt-hint m-0", hidden: () => !state.dashboard?.note }, () => state.dashboard?.note || ""),
            ),
            t.div(
                { className: "flex gap-5" },
                t.span({ className: "txt-hint txt-sm" }, () => (state.refreshedAt ? "Updated " + state.refreshedAt.toLocaleTimeString() : "")),
                t.button({ type: "button", className: "btn sm circle transparent secondary", title: "Refresh", onclick: refresh }, t.i({ className: "ri-refresh-line", ariaHidden: true })),
                t.button({ type: "button", className: "btn sm circle transparent secondary", title: "Fullscreen", onclick: () => toggleFullscreen(wrap) }, t.i({ className: "ri-fullscreen-line", ariaHidden: true })),
            ),
        ),
        variablesBar(() => state.dashboard?.variables || [], varsProxy, () => {
            storeValues(state.dashboard.id, state.vars);
            refresh();
        }),
        t.div({ className: "gp-grid-wrap", onmount: (el) => (wrap = el) }, grid),
    );
}

const root = t.div(
    { className: "gp-viewer" },
    () => {
        if (!state.auth) {
            return t.div({ rid: "login" }, loginView());
        }
        return t.div(
            { rid: "app" },
            header(),
            () => {
                if (state.error) {
                    return t.div({ rid: "error", className: "gp-viewer-content" }, t.div({ className: "alert danger" }, state.error), t.a({ href: "#/" }, "Back to your dashboards"));
                }
                if (state.route) {
                    if (state.loadingDashboard && !state.dashboard) {
                        return t.div({ rid: "loading", className: "gp-viewer-content" }, t.span({ className: "loader" }));
                    }
                    return state.dashboard ? t.div({ rid: "d" + state.dashboard.id }, dashboardView()) : null;
                }
                return t.div({ rid: "list" }, listView());
            },
        );
    },
);

document.body.appendChild(root);
route();
