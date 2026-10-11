// Pages: list of the pages built with the visual builder, published at
// /{slug} (and /{slug}/{id} for record pages), the home page at /.

import { field, handleError, page, select, sidebar, switchInput, userCollections } from "../pocketbaseplus/lib.js";
import { templates } from "./kit.js";

export const shared = store({ pages: [], loaded: false });
let loading = null;

export function loadPages() {
    if (!loading) {
        loading = app.pb
            .collection("pages")
            .getFullList({ sort: "title", fields: "id,title,slug,published,home,record_collection,kind,access,roles,updated", requestKey: null })
            .then((list) => (shared.pages = list))
            .catch(handleError)
            .finally(() => {
                shared.loaded = true;
                loading = null;
            });
    }
    return loading;
}

export function pagesSidebar() {
    if (!shared.loaded) {
        loadPages();
    }
    return sidebar("gpPagesSidebar", () => [
        {
            name: "Pages",
            empty: "No pages yet.",
            links: [
                { href: "#/pages", label: "All pages", icon: "ri-layout-grid-line", exact: true },
                { href: "#/pages/theme", label: "Theme", icon: "ri-palette-line", exact: true },
                { href: "#/pages/members", label: "Members & sign in", icon: "ri-shield-user-line", exact: true },
                ...shared.pages.filter((p) => !isPart(p)).map((p) => ({
                    href: "#/pages/" + p.id,
                    label: p.title,
                    icon: p.home ? "ri-home-4-line" : p.record_collection ? "ri-file-list-3-line" : "ri-pages-line",
                    after: t.span(
                        { className: "flex gap-5" },
                        isProtected(p) ? t.i({ className: "ri-lock-line txt-hint", title: "Members only" }) : null,
                        t.span({ className: `gp-dot ${p.published ? "on" : ""}`, title: p.published ? "Published" : "Draft" }),
                    ),
                })),
            ],
        },
        {
            name: "Site parts",
            empty: "Header, footer, sidebar... placed on several pages.",
            links: shared.pages.filter(isPart).map((p) => ({ href: "#/pages/" + p.id, label: p.title, icon: "ri-layout-top-2-line" })),
        },
    ]);
}

export const isPart = (p) => p.kind == "part";
export const isProtected = (p) => p.access == "users" || p.access == "roles";

/** The site parts a template can use instead of new copies: { header: slug, footer: slug, sidebar: slug }. */
export function siteParts() {
    const found = {};
    const roles = { header: /header|navbar|nav\b|menu bar/i, footer: /footer/i, sidebar: /sidebar|side menu/i };
    for (const p of shared.pages.filter(isPart)) {
        for (const [role, re] of Object.entries(roles)) {
            if (!found[role] && (re.test(p.slug) || re.test(p.title))) {
                found[role] = { slug: p.slug, title: p.title };
            }
        }
    }
    return found;
}

// the first path segments of the app's routes: those pages stay under /p/ (as on the server)
const reservedSlugs = new Set(["api", "p", "f", "dashboards", "email"]);

/** The address of the page with this slug (and record id). */
export const pageURL = (slug, id = "") => (reservedSlugs.has(slug) ? "/p/" : "/") + slug + (id ? "/" + id : "");

export const pagePath = (p) => (p.home ? "/" : pageURL(p.slug));

export function slugify(s) {
    return String(s || "")
        .normalize("NFD")
        .replace(/[̀-ͯ]/g, "")
        .toLowerCase()
        .replace(/[^a-z0-9]+/g, "-")
        .replace(/^-+|-+$/g, "")
        .slice(0, 60) || "page";
}

const starter = (title) => `<section style="padding:64px 24px;text-align:center;font-family:system-ui,sans-serif">
<h1 style="font-size:40px;margin:0 0 12px">${title.replace(/[<>&"]/g, "")}</h1>
<p style="font-size:18px;color:#555;margin:0">Drag blocks from the right panel to build the page.</p></section>`;

const recordStarter = () => `<section style="padding:48px 24px;max-width:960px;margin:0 auto;font-family:system-ui,sans-serif">
<h1 data-pb-field="" style="font-size:36px;margin:0 0 16px">{field}</h1>
<p data-pb-field="" style="font-size:18px;color:#555">{field}</p></section>`;

function newPageModal() {
    const data = store({ title: "", kind: "page", template: "blank", collection: "", collections: [], useParts: true, isSaving: false });
    const available = siteParts();
    // the site parts the chosen template can use
    const usable = () => (choices().find((x) => x.id == data.template)?.parts || []).filter((r) => available[r]);
    const choices = () => templates.filter((x) => x.kind == "any" || x.kind == data.kind);
    userCollections().then((list) => (data.collections = list.filter((c) => c.type != "view"))).catch(handleError);

    async function create() {
        data.isSaving = true;
        try {
            const recordPage = data.kind == "record";
            const tpl = choices().find((x) => x.id == data.template) || choices()[0];
            const o = {};
            if (data.useParts) {
                usable().forEach((r) => (o[r] = available[r].slug));
            }
            const rec = await app.pb.collection("pages").create({
                title: data.title,
                slug: slugify(data.title),
                published: false,
                record_collection: recordPage ? data.collection : "",
                kind: data.kind == "part" ? "part" : "",
                kit: tpl.kit ? "basecoat" : "",
                // the lists of new pages show what the visitor may read (API rules)
                html: (tpl.html ? tpl.html(o) : recordPage ? recordStarter() : starter(data.title)).replace(/data-pb-list=/g, 'data-pb-access="rules" data-pb-list='),
                record_rules: recordPage,
            });
            app.modals.close(modal);
            loadPages();
            window.location.hash = "#/pages/" + rec.id;
        } catch (err) {
            handleError(err);
        } finally {
            data.isSaving = false;
        }
    }

    const modal = t.div(
        { className: "modal popup sm" },
        t.header({ className: "modal-header" }, t.h5({ className: "m-auto" }, "New page")),
        t.form(
            { id: "gpNewPage", className: "modal-content", onsubmit: (e) => (e.preventDefault(), create()) },
            field({ label: "Title" }, t.input({ type: "text", required: true, autofocus: true, placeholder: "e.g. Our products", value: () => data.title, oninput: (e) => (data.title = e.target.value) })),
            field({ label: "Type" }, select({
                options: [
                    { value: "page", label: "Page (/slug)" },
                    { value: "record", label: "Record page: one record per URL (/slug/id)" },
                    { value: "part", label: "Site part: header, footer, sidebar... placed on several pages" },
                ],
                value: () => data.kind,
                onchange: (v) => {
                    data.kind = v;
                    data.template = choices()[0].id;
                },
            })),
            field({ label: "Template", hint: "Basecoat UI templates use the components and the site theme (Pages → Theme)." }, select({
                options: () => choices().map((x) => ({ value: x.id, label: x.label })),
                value: () => data.template,
                onchange: (v) => (data.template = v),
            })),
            () => usable().length
                ? t.div(
                    { rid: "parts" + usable().join(), className: "m-b-sm" },
                    switchInput(data, "useParts", "Use the site parts: " + usable().map((r) => available[r].title).join(", ")),
                )
                : null,
            () => data.kind == "record"
                ? field({ label: "Collection", hint: "The fields of the page show the record of the URL. Link to it from a collection list." }, select({
                    required: true,
                    placeholder: "- Select collection -",
                    options: () => data.collections.map((c) => ({ value: c.id, label: c.name })),
                    value: () => data.collection,
                    onchange: (v) => (data.collection = v),
                }))
                : null,
        ),
        t.footer(
            { className: "modal-footer" },
            t.button({ type: "button", className: "btn transparent secondary m-r-auto", onclick: () => app.modals.close(modal) }, t.span({ className: "txt" }, "Cancel")),
            t.button({ type: "submit", "html-form": "gpNewPage", className: () => `btn expanded ${data.isSaving ? "loading" : ""}`, disabled: () => data.isSaving || !data.title || (data.kind == "record" && !data.collection) }, t.span({ className: "txt" }, "Create")),
        ),
    );
    modal.onafterclose = () => modal.remove();
    document.body.appendChild(modal);
    app.modals.open(modal);
}

export function pagePages() {
    app.store.title = "Pages";
    const data = store({ collections: [] });
    loadPages();
    userCollections().then((list) => (data.collections = list)).catch(handleError);

    async function toggle(p) {
        try {
            await app.pb.collection("pages").update(p.id, { published: !p.published });
            loadPages();
        } catch (err) {
            handleError(err);
        }
    }

    return page(
        {
            pbEvent: "gpPagePages",
            sidebar: pagesSidebar(),
            breadcrumbs: ["Pages"],
            buttons: t.button({ type: "button", className: "btn expanded-lg", onclick: newPageModal }, t.i({ className: "ri-add-line", ariaHidden: true }), t.span({ className: "txt" }, "New page")),
        },
        t.p({ className: "txt-hint m-t-0" }, "Pages designed with a drag and drop builder and published on your site. Collection lists, fields and record pages show your data, rendered by the server (visitors don't need access to the collections: only the fields you place are shown)."),
        t.div(
            { className: "gp-table-wrap" },
            t.table(
                null,
                t.thead(null, t.tr(null, t.th(null, "Title"), t.th(null, "Type"), t.th(null, "Link"), t.th(null, "Status"))),
                t.tbody(null, () => {
                    if (!shared.loaded) {
                        return t.tr({ rid: "loading" }, t.td({ colSpan: 4, className: "txt-center" }, t.span({ className: "loader" })));
                    }
                    if (!shared.pages.length) {
                        return t.tr({ rid: "empty" }, t.td({ colSpan: 4, className: "txt-center txt-hint" }, "No pages yet."));
                    }
                    return shared.pages.map((p) =>
                        t.tr(
                            { rid: p.id + p.updated + data.collections.length },
                            t.td(null, t.a({ href: "#/pages/" + p.id, className: "txt-bold flex gap-5" }, t.i({ className: isPart(p) ? "ri-layout-top-2-line" : p.home ? "ri-home-4-line" : "ri-pages-line", ariaHidden: true }), t.span(null, p.title), isProtected(p) ? t.i({ className: "ri-lock-line txt-hint", ariaHidden: true, title: "Members only" }) : null)),
                            t.td({ className: "txt-hint" }, isPart(p) ? "Site part" : p.record_collection
                                ? `Record page of ${data.collections.find((c) => c.id == p.record_collection)?.name || "?"}`
                                : p.home ? "Home page" : "Page"),
                            t.td(null, isPart(p)
                                ? t.span({ className: "txt-hint txt-sm" }, "Placed on pages")
                                : p.published && !p.record_collection
                                ? t.a({ href: pagePath(p), target: "_blank", rel: "noopener", className: "txt-sm" }, pagePath(p))
                                : t.span({ className: "txt-hint txt-sm" }, p.record_collection ? pageURL(p.slug, "{id}") : "Draft")),
                            t.td(
                                null,
                                isPart(p) ? null : t.button(
                                    { type: "button", className: `label sm gp-status-toggle ${p.published ? "success" : ""}`, ariaDescription: app.attrs.tooltip("Click to toggle"), onclick: () => toggle(p) },
                                    p.published ? "published" : "draft",
                                ),
                            ),
                        )
                    );
                }),
            ),
        ),
    );
}
