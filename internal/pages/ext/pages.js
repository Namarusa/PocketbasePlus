// Pages: list of the pages built with the visual builder, published at
// /{slug} (and /{slug}/{id} for single item pages), the home page at /.

import { field, handleError, page, select, sidebar, switchInput, userCollections } from "../pocketbaseplus/lib.js";
import { errorCodes, pageTypes, templates } from "./kit.js";

export const shared = store({ pages: [], loaded: false });
let loading = null;

export function loadPages() {
    if (!loading) {
        loading = app.pb
            .collection("pages")
            .getFullList({ sort: "title", fields: "id,title,slug,published,home,record_collection,list_collection,error_code,kind,access,roles,updated", requestKey: null })
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
                ...shared.pages.filter((p) => !isPart(p) && !isError(p)).map((p) => ({
                    href: "#/pages/" + p.id,
                    label: p.title,
                    icon: pageIcon(p),
                    after: t.span(
                        { className: "flex gap-5" },
                        isProtected(p) ? t.i({ className: "ri-lock-line txt-hint", title: "Members only" }) : null,
                        t.span({ className: `gp-dot ${p.published ? "on" : ""}`, title: p.published ? "Published" : "Draft" }),
                    ),
                })),
            ],
        },
        {
            name: "Error pages",
            empty: "Not set: visitors see the default ones, with the site theme.",
            links: shared.pages.filter(isError).sort((a, b) => a.error_code.localeCompare(b.error_code)).map((p) => ({
                href: "#/pages/" + p.id,
                label: `${p.error_code} · ${p.title}`,
                icon: "ri-error-warning-line",
                after: t.span({ className: `gp-dot ${p.published ? "on" : ""}`, title: p.published ? "In use" : "Draft: the default page is shown" }),
            })),
        },
        {
            name: "Site parts",
            empty: "Header, footer, sidebar... placed on several pages.",
            links: shared.pages.filter(isPart).map((p) => ({ href: "#/pages/" + p.id, label: p.title, icon: "ri-layout-top-2-line" })),
        },
    ]);
}

export const isPart = (p) => p.kind == "part";
export const isError = (p) => p.kind == "error";
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
const reservedSlugs = new Set(["api", "p", "f", "dashboards", "email", "hooks"]);

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

/** The type of a page: single, collection, item (one record per address) or part. */
export function pageType(p) {
    if (p.kind == "part") {
        return "part";
    }
    if (p.kind == "collection") {
        return "collection";
    }
    if (p.kind == "error") {
        return "error";
    }
    return p.record_collection ? "item" : "single";
}

const typeIcons = { single: "ri-pages-line", collection: "ri-gallery-view-2", item: "ri-file-list-2-line", part: "ri-layout-top-2-line", error: "ri-error-warning-line" };
export const pageIcon = (p) => (p.home ? "ri-home-4-line" : typeIcons[pageType(p)]);

const escapeHTML = (s) => String(s).replace(/[<>&"]/g, (c) => ({ "<": "&lt;", ">": "&gt;", "&": "&amp;", '"': "&quot;" })[c]);

/** Radio cards: the types of pages, the layouts. */
function choiceCards(items, value, onchange, className = "") {
    return t.div(
        { className: "gp-ai-cards gpp-choice-cards " + className },
        items.map((x) =>
            t.label(
                { className: () => `gp-ai-card ${value() == x.id ? "active" : ""}` },
                t.input({ type: "radio", name: className || "choice", value: x.id, checked: () => value() == x.id, onchange: () => onchange(x.id) }),
                t.span({ className: "gp-ai-card-title" }, t.i({ className: x.icon, ariaHidden: true }), x.label),
                x.hint || x.address ? t.small({ className: "txt-hint" }, [x.hint, x.address ? `(${x.address})` : ""].filter(Boolean).join(" ")) : null,
            )
        ),
    );
}

const extBase = app.pb.buildURL("/_/extensions/pocketbaseplus-pages/");

function newPageModal() {
    if (!document.querySelector(`link[href="${extBase}builder.css"]`)) {
        document.head.appendChild(t.link({ rel: "stylesheet", href: extBase + "builder.css" }));
    }
    const data = store({ title: "", type: "single", layout: "blank", collection: "", collections: [], useParts: true, withItem: true, errorCode: "", isSaving: false });
    // one page per error: the codes still free
    const freeCodes = () => errorCodes.filter((x) => !shared.pages.some((p) => isError(p) && p.error_code == x.code));
    const available = siteParts();
    const layouts = () => templates.filter((x) => x.type == data.type);
    const layout = () => layouts().find((x) => x.id == data.layout) || layouts()[0];
    // the site parts the chosen layout can use
    const usable = () => (layout()?.parts || []).filter((r) => available[r]);
    const needsCollection = () => data.type == "collection" || data.type == "item";
    const collection = () => data.collections.find((c) => c.id == data.collection);
    // the single item page the cards of a collection page link to
    const itemPage = () => shared.pages.find((p) => p.record_collection && p.record_collection == data.collection);
    userCollections().then((list) => (data.collections = list.filter((c) => c.type != "view"))).catch(handleError);
    if (!shared.loaded) {
        loadPages();
    }

    const parts = () => {
        const o = {};
        if (data.useParts) {
            usable().forEach((r) => (o[r] = available[r].slug));
        }
        return o;
    };
    // the lists of new pages show what the visitor may read (API rules)
    const withRules = (html) => html.replace(/data-pb-list=/g, 'data-pb-access="rules" data-pb-list=');

    async function create() {
        data.isSaving = true;
        try {
            const col = collection();
            const slug = slugify(data.title);
            // a collection page without a single item page: create it too (its cards link to it)
            if (data.type == "collection" && data.withItem && !itemPage()) {
                const detail = templates.find((x) => x.id == "detail");
                await app.pb.collection("pages").create({
                    title: `${data.title} (item)`,
                    slug: slug + "-item",
                    published: false,
                    record_collection: col.id,
                    kit: "basecoat",
                    html: withRules(detail.html(parts())),
                    record_rules: true,
                });
            }
            const isErr = data.type == "error";
            let pageSlug = isErr ? "error-" + data.errorCode : slug;
            for (let n = 2; isErr && shared.pages.some((p) => p.slug == pageSlug); n++) {
                pageSlug = `error-${data.errorCode}-${n}`;
            }
            const rec = await app.pb.collection("pages").create({
                title: data.title,
                slug: pageSlug,
                error_code: isErr ? data.errorCode : "",
                published: false,
                kind: ["part", "collection", "error"].includes(data.type) ? data.type : "",
                record_collection: data.type == "item" ? col.id : "",
                list_collection: data.type == "collection" ? col.id : "",
                // every page uses the site theme (Pages → Theme)
                kit: "basecoat",
                html: withRules(layout().html(parts(), { title: escapeHTML(data.title), collection: col?.name || "" })),
                record_rules: data.type == "item",
            });
            app.modals.close(modal);
            await loadPages();
            window.location.hash = "#/pages/" + rec.id;
        } catch (err) {
            handleError(err);
        } finally {
            data.isSaving = false;
        }
    }

    const modal = t.div(
        { className: "modal popup gpp-new-page" },
        t.header({ className: "modal-header" }, t.h5({ className: "m-auto" }, "New page")),
        t.form(
            { id: "gpNewPage", className: "modal-content", onsubmit: (e) => (e.preventDefault(), create()) },
            field({ label: "Title" }, t.input({ type: "text", required: true, autofocus: true, placeholder: "e.g. Our products", value: () => data.title, oninput: (e) => (data.title = e.target.value) })),
            t.label({ className: "gp-label" }, "What kind of page?"),
            choiceCards(pageTypes, () => data.type, (v) => {
                data.type = v;
                data.layout = layouts()[0].id;
                if (v == "error" && !data.errorCode) {
                    data.errorCode = freeCodes()[0]?.code || "";
                    data.title ||= freeCodes()[0]?.title || "";
                }
            }, "gpp-types"),
            () => data.type == "error"
                ? t.div(
                    { rid: "error" + freeCodes().length },
                    freeCodes().length
                        ? field({
                            label: "Shown for",
                            hint: "One page per error. Visitors see it once it's published; until then (and for the errors without a page), a default page with the site theme.",
                        }, select({
                            required: true,
                            options: () => freeCodes().map((x) => ({ value: x.code, label: `${x.code} · ${x.title}` })),
                            value: () => data.errorCode,
                            onchange: (v) => {
                                const before = errorCodes.find((x) => x.code == data.errorCode)?.title;
                                data.errorCode = v;
                                if (!data.title || data.title == before) {
                                    data.title = errorCodes.find((x) => x.code == v)?.title || data.title;
                                }
                            },
                        }))
                        : t.div({ className: "alert info" }, "Every error already has its page: edit them from the list (Error pages)."),
                )
                : t.div({ rid: "noerror" }),
            () => needsCollection()
                ? t.div(
                    { rid: "collection" },
                    field({
                        label: "Collection",
                        hint: data.type == "item" ? "The page shows the record of its address. Collection pages link to it." : "The records the page lists, with a search box, filters and pages.",
                    }, select({
                        required: true,
                        placeholder: "- Select collection -",
                        options: () => data.collections.map((c) => ({ value: c.id, label: c.name })),
                        value: () => data.collection,
                        onchange: (v) => (data.collection = v),
                    })),
                    () => data.type == "collection" && data.collection
                        ? t.div(
                            { rid: "item" + data.collection, className: "m-b-sm" },
                            itemPage()
                                ? t.div({ className: "field-help" }, `The cards link to its single item page “${itemPage().title}”.`)
                                : switchInput(data, "withItem", `Also create its single item page (${pageURL(slugify(data.title || "page") + "-item", "{id}")}), linked from the cards`),
                        )
                        : t.div({ rid: "noitem" }),
                )
                : t.div({ rid: "nocollection" }),
            t.label({ className: "gp-label" }, "Layout"),
            () => t.div({ rid: "layouts-" + data.type }, choiceCards(layouts(), () => layout()?.id, (v) => (data.layout = v), "gpp-layouts")),
            () => usable().length
                ? t.div(
                    { rid: "parts" + usable().join(), className: "m-t-sm" },
                    switchInput(data, "useParts", "Use the site parts: " + usable().map((r) => available[r].title).join(", ")),
                )
                : null,
            t.div({ className: "field-help m-t-sm" }, "Every page uses the site theme: colors, fonts and light / dark mode are set in Pages → Theme."),
        ),
        t.footer(
            { className: "modal-footer" },
            t.button({ type: "button", className: "btn transparent secondary m-r-auto", onclick: () => app.modals.close(modal) }, t.span({ className: "txt" }, "Cancel")),
            t.button({ type: "submit", "html-form": "gpNewPage", className: () => `btn expanded ${data.isSaving ? "loading" : ""}`, disabled: () => data.isSaving || !data.title || (needsCollection() && !data.collection) || (data.type == "error" && !data.errorCode) }, t.span({ className: "txt" }, "Create")),
        ),
    );
    modal.onafterclose = () => modal.remove();
    document.body.appendChild(modal);
    app.modals.open(modal);
}

/** "Collection page · products", "Single item page · products", "Home page"... */
export function typeLabel(p, collections = []) {
    const name = (id) => collections.find((c) => c.id == id)?.name || "?";
    switch (pageType(p)) {
        case "part":
            return "Site part";
        case "collection":
            return `Collection page · ${name(p.list_collection)}`;
        case "item":
            return `Single item page · ${name(p.record_collection)}`;
        case "error":
            return `Error page · ${p.error_code}`;
    }
    return p.home ? "Home page" : "Single page";
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
        t.p({ className: "txt-hint m-t-0" }, "Pages designed with a drag and drop builder and published on your site. Collection pages, single item pages, lists and fields show your data, rendered by the server (visitors don't need access to the collections: only the fields you place are shown)."),
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
                            t.td(null, t.a({ href: "#/pages/" + p.id, className: "txt-bold flex gap-5" }, t.i({ className: pageIcon(p), ariaHidden: true }), t.span(null, p.title), isProtected(p) ? t.i({ className: "ri-lock-line txt-hint", ariaHidden: true, title: "Members only" }) : null)),
                            t.td({ className: "txt-hint" }, typeLabel(p, data.collections)),
                            t.td(null, isPart(p)
                                ? t.span({ className: "txt-hint txt-sm" }, "Placed on pages")
                                : isError(p)
                                ? t.span({ className: "txt-hint txt-sm" }, p.published ? `Shown for the ${p.error_code} errors` : "Not used yet: the default page is shown")
                                : p.published && !p.record_collection
                                ? t.a({ href: pagePath(p), target: "_blank", rel: "noopener", className: "txt-sm" }, pagePath(p))
                                : t.span({ className: "txt-hint txt-sm" }, p.record_collection ? pageURL(p.slug, "{id}") : "Draft")),
                            t.td(
                                null,
                                isPart(p) ? null : t.button(
                                    { type: "button", className: `label sm gp-status-toggle ${p.published ? "success" : ""}`, ariaDescription: app.attrs.tooltip("Click to toggle"), onclick: () => toggle(p) },
                                    p.published ? (isError(p) ? "in use" : "published") : "draft",
                                ),
                            ),
                        )
                    );
                }),
            ),
        ),
    );
}
