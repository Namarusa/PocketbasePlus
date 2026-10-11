// Pages: members and page actions. Pages can be public, for signed-in users
// or for some roles (the roles of the app); elements can be shown to guests,
// signed-in users or roles only (data-pb-visible). Buttons and forms run
// "Page action" flows (sign in, actions); the server answers with the next
// page. The builder previews the page "as" a visitor.

import { field, handleError, page, select } from "../pocketbaseplus/lib.js";
import { loadPages, pageURL, pagesSidebar, shared, slugify } from "./pages.js";

export const members = store({ roles: [], flows: [], userFields: [], settings: null, loaded: false });

export async function loadMembers() {
    const [roles, flows, users, settings] = await Promise.all([
        app.pb.collection("roles").getFullList({ sort: "name", fields: "id,name", requestKey: null }).catch(() => []),
        app.pb.collection("flows").getFullList({ filter: 'trigger_type = "action"', sort: "name", fields: "id,name,active,trigger,steps", requestKey: null }).catch(() => []),
        app.pb.collections.getOne("users", { requestKey: null }).catch(() => null),
        app.pb.collection("pages_settings").getFirstListItem("", { requestKey: null }).catch(() => null),
    ]);
    members.roles = roles;
    members.flows = flows;
    members.userFields = (users?.fields || []).filter((f) => !f.hidden && f.type != "password" && f.name != "tokenKey").map((f) => f.name);
    members.settings = settings;
    members.loaded = true;
    return members;
}

const hasStep = (flow, type) => JSON.stringify(flow.steps || []).includes(`"type":"${type}"`);
export const signInFlow = () => members.flows.find((f) => f.active && hasStep(f, "sign_in"));
export const loginSlug = () => members.settings?.login_page || "";
const roleName = (id) => members.roles.find((r) => r.id == id)?.name || "?";

// ---- access -------------------------------------------------------------------------------

/** A short description of who can open a page. */
export function accessLabel(p) {
    switch (p.access) {
        case "users":
            return "Signed-in users";
        case "roles":
            return "Roles: " + (p.roles || []).map(roleName).join(", ");
    }
    return "Public";
}

/** The visitors of the "View as" switch: "" (you: everything), "public", "users", "role:<id>". */
export function viewerOptions() {
    return [
        { value: "", label: "Admin (everything)" },
        { value: "public", label: "Public visitor (not signed in)" },
        { value: "users", label: "Signed-in user (no role)" },
        ...members.roles.map((r) => ({ value: "role:" + r.id, label: "Role: " + r.name })),
    ];
}

/** Whether a visitor of the switch can open the page. */
export function viewerCanOpen(p, viewer) {
    if (!viewer) {
        return true;
    }
    switch (p.access) {
        case "users":
            return viewer != "public";
        case "roles":
            return viewer.startsWith("role:") && (p.roles || []).includes(viewer.slice(5));
    }
    return true;
}

/** Hides the elements the visitor doesn't see, in the canvas. */
export function applyViewAs(ed, viewer) {
    const doc = ed?.Canvas.getDocument();
    if (!doc) {
        return;
    }
    let style = doc.getElementById("gpp-viewas");
    if (!style) {
        style = doc.createElement("style");
        style.id = "gpp-viewas";
        doc.head.appendChild(style);
    }
    let css = "";
    if (viewer) {
        const tokens = ["everyone", ...(viewer == "public" ? ["guests"] : ["users"]), ...(viewer.startsWith("role:") ? [viewer] : [])];
        css = `[data-pb-visible]:not([data-pb-visible=""])${tokens.map((t) => `:not([data-pb-visible~="${t}"])`).join("")}{display:none!important}`;
    }
    style.textContent = css;
    doc.body.classList.toggle("gpp-viewing-as", !!viewer);
}

// ---- component types --------------------------------------------------------------------

const visibleOptions = [
    { id: "", label: "Everyone" },
    { id: "guests", label: "Guests only (not signed in)" },
    { id: "users", label: "Signed-in users" },
    { id: "roles", label: "Some roles…" },
];

/** The "Visible to" setting of every element (data-pb-visible). */
function visibleTrait(ed) {
    ed.Traits.addType("gpp-visible", {
        createInput() {
            const el = document.createElement("div");
            el.className = "gpp-visible";
            el.innerHTML = `<select class="gpp-visible-mode">${visibleOptions.map((o) => `<option value="${o.id}">${o.label}</option>`).join("")}</select><div class="gpp-visible-roles"></div>`;
            return el;
        },
        onEvent({ elInput, component }) {
            const mode = elInput.querySelector(".gpp-visible-mode").value;
            let value = mode;
            if (mode == "roles") {
                value = [...elInput.querySelectorAll(".gpp-visible-roles input:checked")].map((i) => "role:" + i.value).join(" ") || "roles";
            }
            if (value) {
                component.addAttributes({ "data-pb-visible": value });
            } else {
                component.removeAttributes("data-pb-visible");
            }
            this.onUpdate({ elInput, component });
        },
        onUpdate({ elInput, component }) {
            const value = component.getAttributes()["data-pb-visible"] || "";
            const tokens = value.split(/\s+/).filter(Boolean);
            const roles = tokens.filter((t) => t.startsWith("role:")).map((t) => t.slice(5));
            const mode = roles.length || value == "roles" ? "roles" : tokens.includes("guests") ? "guests" : tokens.includes("users") ? "users" : "";
            elInput.querySelector(".gpp-visible-mode").value = mode;
            const box = elInput.querySelector(".gpp-visible-roles");
            box.hidden = mode != "roles";
            box.innerHTML = members.roles.length
                ? members.roles.map((r) => `<label><input type="checkbox" value="${r.id}" ${roles.includes(r.id) ? "checked" : ""}> ${r.name.replace(/[<>&"]/g, "")}</label>`).join("")
                : `<small>No roles yet (Permissions → Roles & users).</small>`;
        },
    });
}

const flowOptions = () => [
    { id: "", label: members.flows.length ? "- Select a flow -" : "- No \"Page action\" flow yet -" },
    ...members.flows.map((f) => ({ id: f.id, label: f.name + (f.active ? "" : " (inactive)") })),
];
const userFieldOptions = () => [{ id: "", label: "- Select a field -" }, ...members.userFields.map((n) => ({ id: n, label: n }))];

/** Adds the "Visible to" setting to a selected component. */
export function addVisibleTrait(comp) {
    if (comp && comp.get("type") != "wrapper" && !comp.getTrait("gpp-visible")) {
        comp.addTrait({ type: "gpp-visible", name: "gpp-visible", label: "Visible to", changeProp: true }, { at: 0 });
    }
}

/** Refreshes the options of the member settings of a selected component. */
export function refreshMemberTraits(comp) {
    comp?.getTrait?.("data-pb-action")?.set("options", flowOptions());
    comp?.getTrait?.("data-pb-user")?.set("options", userFieldOptions());
}

export function defineMemberTypes(ed) {
    visibleTrait(ed);
    const dc = ed.DomComponents;
    const has = (el, attr) => el?.nodeType == 1 && el.hasAttribute(attr);
    dc.addType("pb-action", {
        isComponent: (el) => has(el, "data-pb-action"),
        model: {
            defaults: {
                name: "Page action",
                icon: '<i class="ri-flashlight-line"></i>',
                traits: [{ type: "select", name: "data-pb-action", label: "Flow", options: [] }],
            },
        },
    });
    dc.addType("pb-logout", {
        isComponent: (el) => has(el, "data-pb-logout"),
        model: {
            defaults: {
                name: "Sign out",
                icon: '<i class="ri-logout-box-r-line"></i>',
                traits: [{ type: "text", name: "data-pb-logout", label: "Then open", placeholder: "/" }],
            },
        },
    });
    dc.addType("pb-user", {
        isComponent: (el) => has(el, "data-pb-user"),
        model: {
            defaults: {
                name: "Signed-in user field",
                icon: '<i class="ri-user-line"></i>',
                droppable: false,
                traits: [{ type: "select", name: "data-pb-user", label: "Field", options: [] }],
            },
            init() {
                this.on("change:attributes:data-pb-user", () => this.components(`{${this.getAttributes()["data-pb-user"] || "field"}}`));
            },
        },
    });
    dc.addType("pb-message", {
        isComponent: (el) => has(el, "data-pb-message"),
        model: { defaults: { name: "Action messages", icon: '<i class="ri-chat-check-line"></i>', droppable: false } },
    });
    // form inputs: their name is the name of the value in the flow (trigger.form.<name>)
    dc.addType("gpp-input", {
        isComponent: (el) => el?.nodeType == 1 && ["INPUT", "TEXTAREA", "SELECT"].includes(el.tagName),
        model: {
            defaults: {
                name: "Input",
                droppable: false,
                traits: [
                    { type: "text", name: "name", label: "Name (trigger.form.<name>)" },
                    { type: "select", name: "type", label: "Type", options: ["text", "email", "password", "number", "date", "tel", "url", "checkbox", "hidden"].map((v) => ({ id: v, label: v })) },
                    { type: "text", name: "placeholder", label: "Placeholder" },
                    { type: "text", name: "value", label: "Value" },
                    { type: "checkbox", name: "required", label: "Required" },
                ],
            },
        },
    });
}

// ---- blocks ----------------------------------------------------------------------------------

export function setMemberBlocks(ed, kit) {
    const bm = ed.BlockManager;
    bm.getAll()
        .filter((b) => b.get("id").startsWith("gpp-member-"))
        .forEach((b) => bm.remove(b.get("id")));
    const category = { id: "Members & actions", label: "Members & actions", open: false };
    const media = (name) => `<i class="${name} gpp-block-icon"></i>`;
    const flow = signInFlow()?.id || "";
    const login = loginSlug();

    const btn = kit ? `class="btn"` : `style="display:inline-block;padding:10px 20px;background:#2563eb;color:#fff;border:0;border-radius:6px;font:inherit;font-weight:600;cursor:pointer"`;
    const outline = kit ? `class="btn" data-variant="outline"` : `style="display:inline-block;padding:8px 16px;background:transparent;color:inherit;border:1px solid #d4d4d8;border-radius:6px;font:inherit;cursor:pointer"`;
    const input = (type, name, label, extra = "") => kit
        ? `<div style="display:grid;gap:6px"><label class="label" data-kit-ref="for:${name}">${label}</label><input class="input" type="${type}" data-kit-id="${name}" name="${name}" ${extra}></div>`
        : `<label style="display:block;margin:0 0 12px"><span style="display:block;margin:0 0 4px;font-weight:600">${label}</span><input type="${type}" name="${name}" ${extra} style="width:100%;box-sizing:border-box;padding:10px;border:1px solid #d4d4d8;border-radius:6px;font:inherit"></label>`;
    const formStyle = kit ? `class="form" style="display:grid;gap:16px"` : `style="display:block"`;
    const card = (inner) => kit
        ? `<section style="padding:48px 16px"><div class="card" style="max-width:400px;margin:0 auto">${inner}</div></section>`
        : `<section style="padding:48px 16px;font-family:system-ui,sans-serif"><div style="max-width:400px;margin:0 auto;padding:24px;border:1px solid #e4e4e7;border-radius:12px">${inner}</div></section>`;

    const blocks = {
        "login-form": {
            label: "Login form",
            media: media("ri-login-box-line"),
            content: card(
                (kit ? `<header><h2>Sign in</h2><p>Enter your email and password.</p></header><section>` : `<h2 style="margin:0 0 4px">Sign in</h2><p style="margin:0 0 16px;color:#71717a">Enter your email and password.</p>`) +
                `<div data-pb-message></div>` +
                `<form data-pb-action="${flow}" ${formStyle}>${input("email", "email", "Email", 'required autocomplete="username"')}${input("password", "password", "Password", 'required autocomplete="current-password"')}<button type="submit" ${btn}>Sign in</button></form>` +
                (kit ? `</section>` : ""),
            ),
        },
        "action-button": {
            label: "Action button",
            media: media("ri-flashlight-line"),
            content: `<button type="submit" data-pb-action="" ${btn}>Run action</button>`,
        },
        "action-form": {
            label: "Action form",
            media: media("ri-file-edit-line"),
            content: `<form data-pb-action="" ${formStyle}>${input("text", "message", "Message")}<button type="submit" ${btn}>Send</button></form>`,
        },
        "logout": {
            label: "Sign out button",
            media: media("ri-logout-box-r-line"),
            content: `<button type="submit" data-pb-logout="/" ${outline}>Sign out</button>`,
        },
        "account": {
            label: "Account (signed in / sign in)",
            media: media("ri-account-circle-line"),
            content: `<div style="display:flex;gap:12px;align-items:center;flex-wrap:wrap"><span data-pb-visible="users">Signed in as <strong data-pb-user="email">{email}</strong></span>` +
                `<button type="submit" data-pb-logout="/" data-pb-visible="users" ${outline}>Sign out</button>` +
                `<a href="${pageURL(login || "login")}" data-pb-visible="guests" ${kit ? `class="btn"` : `style="font-weight:600"`}>Sign in</a></div>`,
        },
        "user-field": {
            label: "Signed-in user field",
            media: media("ri-user-line"),
            content: `<span data-pb-user="email">{email}</span>`,
        },
        "message": {
            label: "Action messages",
            media: media("ri-chat-check-line"),
            content: `<div data-pb-message></div>`,
        },
        "for-guests": {
            label: "Box for guests",
            media: media("ri-user-unfollow-line"),
            content: `<div data-pb-visible="guests" style="padding:16px"><p style="margin:0">Only visitors who aren't signed in see this.</p></div>`,
        },
        "for-users": {
            label: "Box for signed-in users",
            media: media("ri-user-follow-line"),
            content: `<div data-pb-visible="users" style="padding:16px"><p style="margin:0">Only signed-in users see this.</p></div>`,
        },
    };
    for (const [id, b] of Object.entries(blocks)) {
        bm.add("gpp-member-" + id, { category, ...b });
    }
}

// ---- sign in page ------------------------------------------------------------------------------

/** The HTML of the sign in page: the form for guests, the account for users. */
function loginHTML(flowId, kit) {
    const btn = kit ? `class="btn"` : `style="display:inline-block;padding:10px 20px;background:#2563eb;color:#fff;border:0;border-radius:6px;font:inherit;font-weight:600;cursor:pointer"`;
    const outline = kit ? `class="btn" data-variant="outline"` : `style="padding:8px 16px;background:transparent;border:1px solid #d4d4d8;border-radius:6px;font:inherit;cursor:pointer"`;
    const field = (type, name, label, auto) => kit
        ? `<div style="display:grid;gap:6px"><label class="label" for="login-${name}">${label}</label><input class="input" type="${type}" id="login-${name}" name="${name}" required autocomplete="${auto}"></div>`
        : `<label style="display:block;margin:0 0 12px"><span style="display:block;margin:0 0 4px;font-weight:600">${label}</span><input type="${type}" name="${name}" required autocomplete="${auto}" style="width:100%;box-sizing:border-box;padding:10px;border:1px solid #d4d4d8;border-radius:6px;font:inherit"></label>`;
    const box = kit ? `class="card" style="max-width:400px;margin:0 auto"` : `style="max-width:400px;margin:0 auto;padding:24px;border:1px solid #e4e4e7;border-radius:12px"`;
    const head = (title, text) => kit ? `<header><h2>${title}</h2><p>${text}</p></header>` : `<h2 style="margin:0 0 4px">${title}</h2><p style="margin:0 0 16px;color:#71717a">${text}</p>`;
    const body = (inner) => (kit ? `<section>${inner}</section>` : inner);
    return `<body><section style="padding:64px 16px;${kit ? "" : "font-family:system-ui,sans-serif"}">` +
        `<div data-pb-visible="guests" ${box}>${head("Sign in", "Enter your email and password.")}${body(`<div data-pb-message></div><form data-pb-action="${flowId}" ${kit ? `class="form" style="display:grid;gap:16px"` : ""}>${field("email", "email", "Email", "username")}${field("password", "password", "Password", "current-password")}<button type="submit" ${btn}>Sign in</button></form>`)}</div>` +
        `<div data-pb-visible="users" ${box}>${head("You're signed in", "")}${body(`<div data-pb-message></div><p>Signed in as <strong data-pb-user="email">{email}</strong>.</p><div style="display:flex;gap:8px;flex-wrap:wrap"><a href="/" ${btn}>Home</a><button type="submit" data-pb-logout="/login" ${outline}>Sign out</button></div>`)}</div>` +
        `</section></body>`;
}

/** Creates the "Sign in" flow (when missing) and the sign in page, set as the site's. */
export async function createLoginPage() {
    await loadMembers();
    let flow = signInFlow();
    if (!flow) {
        flow = await app.pb.collection("flows").create({
            name: "Sign in",
            description: "Signs the visitors in from the sign in page of the site (Pages).",
            active: true,
            trigger_type: "action",
            trigger: { access: "public" },
            steps: [
                { key: "login", name: "Sign in", type: "sign_in", config: { collection: "users", identity: "{{.trigger.form.email}}", password: "{{.trigger.form.password}}" } },
                { key: "redirect", name: "Redirect to page", type: "redirect", config: { url: "{{if .trigger.next}}{{.trigger.next}}{{else}}/{{end}}", message: "Welcome back!" } },
            ],
        });
    }
    await loadPages();
    const taken = new Set(shared.pages.map((p) => p.slug));
    let slug = "login";
    for (let n = 2; taken.has(slug); n++) {
        slug = `login-${n}`;
    }
    const kit = !!members.settings; // the site theme exists: Basecoat page
    const html = loginHTML(flow.id, kit).replace(`data-pb-logout="/login"`, `data-pb-logout="${pageURL(slugify(slug))}"`);
    const pageRec = await app.pb.collection("pages").create({ title: "Sign in", slug: slugify(slug), published: true, kit: kit ? "basecoat" : "", access: "public", html, css: "" });
    if (members.settings) {
        members.settings = await app.pb.collection("pages_settings").update(members.settings.id, { login_page: pageRec.slug });
    }
    await Promise.all([loadPages(), loadMembers()]);
    return pageRec;
}

// ---- Members page (#/pages/members) ---------------------------------------------------------

export function pageMembers() {
    app.store.title = "Members & sign in";
    const data = store({ isSaving: false, isCreating: false, pages: [] });
    const refresh = () =>
        Promise.all([
            loadMembers(),
            app.pb.collection("pages").getFullList({ filter: "kind != 'part'", sort: "title", fields: "id,title,slug,published,access,roles", requestKey: null }).then((l) => (data.pages = l)),
        ]).catch(handleError);
    refresh();

    async function setLogin(slug) {
        data.isSaving = true;
        try {
            members.settings = await app.pb.collection("pages_settings").update(members.settings.id, { login_page: slug });
            app.toasts.success("Saved.");
        } catch (err) {
            handleError(err);
        } finally {
            data.isSaving = false;
        }
    }

    async function create() {
        data.isCreating = true;
        try {
            const p = await createLoginPage();
            app.toasts.success(`Created the page "${pageURL(p.slug)}" and the "Sign in" flow.`);
            await refresh();
        } catch (err) {
            handleError(err);
        } finally {
            data.isCreating = false;
        }
    }

    return page(
        {
            pbEvent: "gpPageMembers",
            sidebar: pagesSidebar(),
            breadcrumbs: [t.a({ href: "#/pages" }, "Pages"), "Members & sign in"],
        },
        t.p(
            { className: "txt-hint m-t-0" },
            "Pages are public, or for signed-in users, or for some roles (Permissions → Roles & users). Visitors sign in on the sign in page with their user account (users collection); ",
            "the sign in, like every button of the pages, runs a \"Page action\" flow. Guests opening a protected page are sent to the sign in page, then back.",
        ),
        () => {
            if (!members.loaded) {
                return t.div({ rid: "loading", className: "txt-center" }, t.span({ className: "loader" }));
            }
            const login = data.pages.some((p) => p.slug == loginSlug()) ? loginSlug() : "";
            const flow = signInFlow();
            return t.div(
                { rid: "members" + login + members.flows.length + data.pages.length },
                t.div({ className: "txt-lg m-b-sm" }, "Sign in page"),
                t.div(
                    { className: "grid sm m-b-base" },
                    t.div(
                        { className: "col-lg-6" },
                        field(
                            { label: "Page", hint: "Where guests are sent when a page needs a sign in." },
                            select({
                                placeholder: "None: guests see \"Sign in required\"",
                                options: () => [...data.pages.map((p) => ({ value: p.slug, label: `${p.title} (${pageURL(p.slug)})` }))],
                                value: () => loginSlug(),
                                onchange: (v) => setLogin(v),
                            }),
                        ),
                    ),
                    t.div(
                        { className: "col-lg-6 gp-field-action" },
                        t.button(
                            { type: "button", className: () => `btn secondary ${data.isCreating ? "loading" : ""}`, disabled: () => data.isCreating, onclick: create },
                            t.i({ className: "ri-magic-line", ariaHidden: true }),
                            t.span({ className: "txt" }, login ? "Create a new sign in page" : "Create the sign in page"),
                        ),
                    ),
                ),
                t.div(
                    { className: "alert info m-b-base" },
                    flow
                        ? t.span(null, "Sign in flow: ", t.a({ href: "#/flows/" + flow.id }, flow.name), ". Edit it to add checks (e.g. only verified users), notifications or another redirection.")
                        : t.span(null, "No active flow with a \"Sign in\" step yet: \"Create the sign in page\" makes one."),
                ),
                t.div({ className: "txt-lg m-b-sm" }, "Who can open the pages"),
                t.div(
                    { className: "gp-table-wrap" },
                    t.table(
                        null,
                        t.thead(null, t.tr(null, t.th(null, "Page"), t.th(null, "Access"), t.th(null, "Status"))),
                        t.tbody(
                            null,
                            data.pages.length
                                ? data.pages.map((p) =>
                                    t.tr(
                                        null,
                                        t.td(null, t.a({ href: "#/pages/" + p.id, className: "txt-bold" }, p.title), p.slug == login ? t.span({ className: "label sm m-l-10" }, "sign in page") : null),
                                        t.td(null, t.span({ className: "flex gap-5" }, t.i({ className: p.access == "users" || p.access == "roles" ? "ri-lock-line" : "ri-global-line", ariaHidden: true }), accessLabel(p))),
                                        t.td({ className: "txt-hint" }, p.published ? "published" : "draft"),
                                    )
                                )
                                : t.tr(null, t.td({ colSpan: 3, className: "txt-hint txt-center" }, "No pages yet.")),
                        ),
                    ),
                ),
                t.p({ className: "txt-hint txt-sm" }, "Change the access of a page in its Settings (page builder). Use \"View as\" in the builder to see a page as a role."),
            );
        },
    );
}
