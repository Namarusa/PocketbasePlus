// Pages: the visual builder of a page (GrapesJS), with blocks bound to the
// collections. The page HTML keeps the bindings as data-pb-* attributes and
// the server fills them at each visit (see internal/pages/render.go).

import { api, field, handleError, select, switchInput, textInput, userCollections } from "../pocketbaseplus/lib.js";
import { applyCanvasKit, applyKitTraits, defineKitPageTrait, defineKitTraits, kitClasses, linkIds, setKitBlocks, toggleCanvasMode } from "./kit.js";
import { loadPages, pagePath, pageURL, shared, slugify } from "./pages.js";
import { applyPartsCSS, definePartType, loadParts, partOf, refreshPartTrait, saveAsPartModal, setPartBlocks, unlinkPart } from "./parts.js";
import { addBlockSearch } from "./blocksearch.js";
import { defineWidgetTypes, refreshChartTraits, refreshWidgets, setWidgetBlocks } from "./widgets.js";
import { addColumnsTrait, defineColumnsTrait } from "./tablecols.js";
import { clearDraft, historyModal, offerDraft, storeDraft } from "./history.js";
import { applyConditions, defineConditions } from "./conditions.js";
import { applyDynamic, defineFormula, dynamicValues, formulaModal, formulaSample } from "./formula.js";
import { accessLabel, addVisibleTrait, applyViewAs, defineMemberTypes, loadMembers, members, refreshMemberTraits, setMemberBlocks, viewerCanOpen, viewerOptions } from "./members.js";

const base = app.pb.buildURL("/_/extensions/pocketbaseplus-pages/");
const vendor = base + "vendor/grapesjs-0.23.6/";

let grapesLoading;
function loadGrapes() {
    if (window.grapesjs) {
        return Promise.resolve();
    }
    grapesLoading ||= new Promise((resolve, reject) => {
        document.head.appendChild(t.link({ rel: "stylesheet", href: vendor + "grapes.min.css" }));
        if (!document.querySelector(`link[href="${base}builder.css"]`)) {
            document.head.appendChild(t.link({ rel: "stylesheet", href: base + "builder.css" }));
        }
        const s = document.createElement("script");
        s.src = vendor + "grapes.min.js";
        s.onload = resolve;
        s.onerror = () => {
            grapesLoading = null;
            reject(new Error("Failed to load the page editor."));
        };
        document.head.appendChild(s);
    });
    return grapesLoading;
}

const defaultCSS = "body{margin:0;font-family:system-ui,-apple-system,'Segoe UI',Roboto,sans-serif;color:#222;line-height:1.5}";
const assetURL = (pageId, name) => `/p/_assets/${pageId}/${encodeURIComponent(name)}`;
const placeholderImage = "data:image/svg+xml," + encodeURIComponent(
    `<svg xmlns="http://www.w3.org/2000/svg" width="320" height="180" viewBox="0 0 320 180"><rect width="320" height="180" fill="#e9ecef"/><path d="M130 115l20-25 15 18 10-12 20 19z" fill="#adb5bd"/><circle cx="140" cy="72" r="9" fill="#adb5bd"/></svg>`,
);

// --- collections, fields and sample values ----------------------------------------------------

/** Visible fields of a collection, for a kind of element. */
function fieldsOf(col, kind) {
    if (!col) {
        return [];
    }
    return col.fields.filter((f) => {
        if (f.hidden || f.type == "password" || (col.type == "auth" && f.name == "tokenKey")) {
            return false;
        }
        switch (kind) {
            case "image":
                return (f.type == "file" && !f.protected) || f.type == "url" || f.type == "text";
            case "link":
                return f.type == "url" || f.type == "email" || (f.type == "file" && !f.protected);
            default:
                return f.type != "file";
        }
    });
}

/** The label fields of a collection (presentable, or a usual name field). */
function labelFields(col) {
    const visible = fieldsOf(col, "text");
    const presentable = visible.filter((f) => f.presentable);
    if (presentable.length) {
        return presentable;
    }
    const usual = ["name", "title", "label", "username", "email"].map((n) => visible.find((f) => f.name == n)).find(Boolean);
    return usual ? [usual] : [];
}

/**
 * The fields of a collection for a kind of element, then the fields of its
 * related records ("brand.name", labelled "brand → name"): [{ path, label, field }].
 * byName finds a collection by name or id.
 */
function bindableFields(col, kind, byName) {
    const own = fieldsOf(col, kind).map((f) => ({ path: f.name, label: `${f.name} (${f.type})`, field: f }));
    const related = [];
    for (const rel of fieldsOf(col, "text").filter((f) => f.type == "relation")) {
        const target = byName(rel.collectionId);
        if (!target) {
            continue;
        }
        for (const f of fieldsOf(target, kind)) {
            related.push({ path: `${rel.name}.${f.name}`, label: `${rel.name} → ${f.name} (${f.type})`, field: f });
        }
    }
    return [...own, ...related];
}

/** The field a path refers to, through the relations ("brand.name"). */
function fieldAt(col, path, byName) {
    const parts = String(path || "").split(".");
    let c = col;
    for (let i = 0; c && i < parts.length; i++) {
        const f = c.fields.find((x) => x.name == parts[i]);
        if (!f || i == parts.length - 1) {
            return f || null;
        }
        c = f.type == "relation" ? byName(f.collectionId) : null;
    }
    return null;
}

function sampleText(rec, f) {
    if (!rec || !f) {
        return f ? `{${f.name}}` : "{field}";
    }
    const v = rec[f.name];
    let out = "";
    switch (f.type) {
        case "date":
        case "autodate":
            out = v ? new Date(v.replace(" ", "T")).toLocaleDateString("en-US", { month: "short", day: "numeric", year: "numeric", timeZone: "UTC" }) : "";
            break;
        case "bool":
            out = v ? "Yes" : "No";
            break;
        case "editor":
            out = new DOMParser().parseFromString(v || "", "text/html").body.textContent.trim().slice(0, 300);
            break;
        case "relation":
            out = "";
            break;
        case "json":
        case "geoPoint":
            out = v == null ? "" : JSON.stringify(v);
            break;
        default:
            out = Array.isArray(v) ? v.join(", ") : String(v ?? "");
    }
    return out || `{${f.name}}`;
}

function sampleURL(col, rec, f) {
    if (!rec || !f) {
        return "";
    }
    const v = rec[f.name];
    if (f.type == "file") {
        const name = Array.isArray(v) ? v[0] : v;
        return name ? app.pb.files.getURL(rec, name) : "";
    }
    return typeof v == "string" && /^(https?:\/\/|\/)/.test(v) ? v : "";
}

// --- editor -----------------------------------------------------------------------------------

export function pageBuilder(route) {
    const id = route.params.id;
    const data = store({ page: null, isSaving: false, dirty: false, error: "", viewAs: "" });
    let editor = null;
    let collections = [];
    let forms = [];
    let theme = null; // site theme (pages_settings)
    let offlineMode = false;
    const samples = {};

    const byName = (name) => collections.find((c) => c.name == name || c.id == name);
    const recordCollection = () => byName(data.page?.record_collection);

    /** The first record of a collection, matching the list filter and sort when given. */
    function sample(col, filter = "", sort = "") {
        if (!col) {
            return Promise.resolve(null);
        }
        // placeholders ({{query.x}}) only have values on the published page
        if (filter.includes("{{")) {
            filter = "";
        }
        const key = [col.name, filter, sort].join("|");
        samples[key] ||= app.pb
            .collection(col.name)
            .getList(1, 1, { filter, sort: sort || "-@rowid", requestKey: null })
            .then((r) => r.items[0] || null)
            .catch(() => (filter || sort ? sample(col) : null));
        return samples[key];
    }

    async function load() {
        try {
            const [rec, cols, formList] = await Promise.all([
                app.pb.collection("pages").getOne(id),
                userCollections(),
                app.pb.collection("forms").getFullList({ fields: "slug,name", sort: "name", requestKey: null }).catch(() => []),
                loadPages(),
                loadGrapes(),
                app.pb.collection("pages_settings").getFirstListItem("", { requestKey: null }).then((t) => (theme = t)).catch(() => null),
                api("/api/gp/config").then((c) => (offlineMode = !!c.offline)).catch(() => null),
                loadParts(),
                loadMembers(),
            ]);
            collections = cols;
            forms = formList;
            data.page = { ...rec, kit: rec.kit == "basecoat", isPart: rec.kind == "part", access: rec.access || "public", roles: rec.roles || [] };
            app.store.title = rec.title;
        } catch (err) {
            handleError(err);
            data.error = err?.message || "Failed to load the page.";
            if (err?.status == 404) {
                window.location.hash = "#/pages";
            }
        }
    }

    // ---- component types bound to collections

    /** The closest list around a component. */
    function contextList(comp) {
        for (let p = comp.parent(); p; p = p.parent()) {
            if (p.getAttributes()["data-pb-list"] !== undefined) {
                return p;
            }
        }
        return null;
    }

    /** The collection the fields of a component refer to: the closest list, or the record of a record page. */
    function contextCollection(comp) {
        const list = contextList(comp);
        return list ? byName(list.getAttributes()["data-pb-list"]) : recordCollection();
    }

    /** The sample record shown by the fields of a component. */
    function contextSample(comp) {
        const list = contextList(comp);
        if (!list) {
            return sample(recordCollection(), data.page?.record_filter || "");
        }
        const a = list.getAttributes();
        return sample(byName(a["data-pb-list"]), a["data-pb-filter"] || "", a["data-pb-sort"] || "");
    }

    const kindOf = (comp) => (comp.is("pb-image") ? "image" : comp.is("pb-link") ? "link" : "text");

    function fieldOptions(comp, kind = kindOf(comp)) {
        const col = contextCollection(comp);
        const empty = col ? "- Select a field -" : "- Place it in a collection list -";
        return [{ id: "", label: empty }, ...bindableFields(col, kind, byName).map((b) => ({ id: b.path, label: b.label }))];
    }

    /**
     * The sample record and field of a path ("brand.logo": the sample's brand,
     * loaded by its id), for the canvas.
     */
    async function samplePath(col, rec, path) {
        const parts = String(path || "").split(".");
        for (let i = 0; col && rec && i < parts.length - 1; i++) {
            const rel = col.fields.find((x) => x.name == parts[i] && x.type == "relation");
            const target = rel && byName(rel.collectionId);
            const id = rel && [].concat(rec[rel.name] || [])[0];
            if (!target || !id) {
                return { col: target || null, rec: null, f: target?.fields.find((x) => x.name == parts.at(-1)) || null };
            }
            const key = "id|" + target.name + "|" + id;
            samples[key] ||= app.pb.collection(target.name).getOne(id, { requestKey: null }).catch(() => null);
            rec = await samples[key];
            col = target;
        }
        return { col, rec, f: col?.fields.find((x) => x.name == parts.at(-1)) || null };
    }

    // ---- pagination and search (internal/pages/paging.go)

    /** The top level lists of the page: [{ key, col, comp }] (a list's name in the address). */
    function pageLists() {
        if (!editor) {
            return [];
        }
        const out = [];
        for (const comp of editor.getWrapper().find("[data-pb-list]")) {
            if (comp.parents().some((p) => p.getAttributes()["data-pb-list"] !== undefined)) {
                continue; // nested: repeated with each record
            }
            const a = comp.getAttributes();
            const key = (a["data-pb-key"] || a["data-pb-list"] || "").replace(/[^\w-]/g, "");
            if (key && !out.some((l) => l.key == key)) {
                out.push({ key, col: byName(a["data-pb-list"]), comp });
            }
        }
        return out;
    }

    const listChoices = (none) => {
        const lists = pageLists();
        return [{ id: "", label: lists.length ? none : "- Add a collection list first -" }, ...lists.map((l) => ({ id: l.key, label: l.col && l.col.name != l.key ? `${l.key} (${l.col.name})` : l.key }))];
    };

    /** The fields a search box can search: texts, numbers, choices (and a related record's). */
    function searchableFields(col) {
        const types = ["text", "email", "url", "editor", "select", "number"];
        return bindableFields(col, "text", byName).filter((b) => types.includes(b.field.type) && b.path != "id" && !(col.type == "auth" && b.path == "email"));
    }

    function defaultSearchFields(col) {
        const all = searchableFields(col);
        const labels = labelFields(col).map((f) => f.name);
        const picked = all.filter((b) => labels.includes(b.path));
        for (const b of all) {
            if (picked.length >= 3) {
                break;
            }
            if (!picked.includes(b) && !b.path.includes(".") && b.field.type != "editor" && b.field.type != "url") {
                picked.push(b);
            }
        }
        return picked.map((b) => b.path);
    }

    /** Links new search boxes and pagers to a list of the page (the first one). */
    function linkSearch(root) {
        const lists = pageLists();
        for (const comp of [root, ...root.find("[data-pb-search], [data-pb-pager]")]) {
            const a = comp.getAttributes();
            if (a["data-pb-search"] === "" && lists.length) {
                comp.addAttributes({ "data-pb-search": lists[0].key, "data-pb-search-fields": defaultSearchFields(lists[0].col).join(",") });
            }
            if (a["data-pb-pager"] === "" && lists.length) {
                const list = lists.find((l) => l.comp.getAttributes()["data-pb-paginate"]) || lists[0];
                comp.addAttributes({ "data-pb-pager": list.key });
                if (!list.comp.getAttributes()["data-pb-paginate"]) {
                    list.comp.addAttributes({ "data-pb-paginate": "pages" });
                }
            }
        }
    }

    function defineSearchTraits(ed) {
        ed.Traits.addType("gpp-search-fields", {
            createInput({ component }) {
                const el = document.createElement("div");
                el.className = "gpp-search-fields";
                el.addEventListener("change", () => {
                    const picked = [...el.querySelectorAll("input:checked")].map((i) => i.value);
                    component.addAttributes({ "data-pb-search-fields": picked.join(",") });
                });
                render(el, component);
                return el;
            },
            onUpdate({ elInput, component }) {
                render(elInput, component);
            },
        });
        function render(el, comp) {
            const a = comp.getAttributes();
            const list = pageLists().find((l) => l.key == a["data-pb-search"]);
            const picked = new Set(String(a["data-pb-search-fields"] || "").split(",").filter(Boolean));
            if (!list?.col) {
                el.replaceChildren(Object.assign(document.createElement("span"), { className: "gpp-hint", textContent: "Choose the list it searches." }));
                return;
            }
            const rows = searchableFields(list.col).map((b) => {
                const label = document.createElement("label");
                const box = Object.assign(document.createElement("input"), { type: "checkbox", value: b.path, checked: picked.has(b.path) });
                const name = Object.assign(document.createElement("span"), { textContent: b.path.replace(".", " → ") });
                label.title = b.label;
                label.append(box, name, Object.assign(document.createElement("small"), { textContent: b.field.type }));
                return label;
            });
            if (!picked.size) {
                rows.unshift(Object.assign(document.createElement("span"), { className: "gpp-hint gpp-warn", textContent: "Tick the fields to search." }));
            }
            el.replaceChildren(...rows);
        }
    }

        const collectionOptions = () => [{ id: "", label: "- Select a collection -" }, ...collections.map((c) => ({ id: c.name, label: c.name }))];
    // charts and calendars (widgets.js)
    const widgetCtx = { pageId: id, byName: (name) => byName(name), collectionOptions: () => collectionOptions(), viewAs: () => data.viewAs };
    // formulas (formula.js)
    const formulaCtx = {
        pageId: id,
        collection: (comp) => contextCollection(comp),
        sampleId: async (comp) => (await contextSample(comp))?.id || "",
        sample: async (comp) => (await contextSample(comp)) || null,
        typedFields: (col) => bindableFields(col, "text", byName),
        typedUserFields: () => {
            const users = byName("users");
            return users ? [{ path: "id", label: "id", field: null }, ...bindableFields(users, "text", byName)] : [];
        },
        fields: (col) => bindableFields(col, "text", byName).map((b) => ({ path: b.path, label: b.label })),
        userFields: () => {
            const users = byName("users");
            return users ? [{ path: "id", label: "id" }, ...bindableFields(users, "text", byName).map((b) => ({ path: b.path, label: b.label }))] : [];
        },
        viewAs: () => data.viewAs || "",
        onChange: (comp) => {
            comp._pbKey = bindingKey(comp);
            applySamples(comp);
        },
    };
    const formOptions = () => [{ id: "", label: forms.length ? "- Select a form -" : "- No forms yet (Forms tab) -" }, ...forms.map((f) => ({ id: f.slug, label: f.name }))];

    function recordPageOptions(comp) {
        const col = contextCollection(comp);
        const pages = shared.pages.filter((p) => p.record_collection && (!col || p.record_collection == col.id));
        // the pages of the related records ("slug:brand")
        const related = [];
        for (const rel of fieldsOf(col, "text").filter((f) => f.type == "relation")) {
            for (const p of shared.pages.filter((p) => p.record_collection && p.record_collection == rel.collectionId)) {
                related.push({ id: `${p.slug}:${rel.name}`, label: `${p.title} (its ${rel.name})` });
            }
        }
        const any = pages.length + related.length;
        return [{ id: "", label: any ? "- Select a record page -" : "- No record page for this collection -" }, ...pages.map((p) => ({ id: p.slug, label: p.title })), ...related];
    }

    function defineTypes(ed) {
        const dc = ed.DomComponents;
        definePartType(ed);
        const has = (el, attr) => el?.nodeType == 1 && el.hasAttribute(attr);

        dc.addType("pb-list", {
            isComponent: (el) => has(el, "data-pb-list"),
            model: {
                defaults: {
                    name: "Collection list",
                    icon: '<i class="ri-database-2-line"></i>',
                    // only its item and empty state: anything else would be dropped by the server
                    droppable: "[data-pb-item], [data-pb-empty]",
                    traits: [
                        { type: "select", name: "data-pb-list", label: "Collection", options: collectionOptions() },
                        { type: "text", name: "data-pb-filter", label: "Filter", placeholder: "e.g. published = true" },
                        { type: "text", name: "data-pb-sort", label: "Sort", placeholder: "e.g. -created" },
                        { type: "number", name: "data-pb-limit", label: "Max records", min: 1, max: 100, placeholder: "12" },
                        {
                            type: "select",
                            name: "data-pb-paginate",
                            label: "Pagination",
                            options: [
                                { id: "", label: "None (the first records)" },
                                { id: "pages", label: "Pages (‹ 1 2 3 ›)" },
                                { id: "more", label: "\"Show more\" button" },
                            ],
                        },
                        { type: "text", name: "data-pb-key", label: "Address name", placeholder: "the collection name", attributes: { title: "The list's name in the page address (cards_page=2) when several lists of the page page or search" } },
                        {
                            type: "select",
                            name: "data-pb-access",
                            label: "Data access",
                            options: [
                                { id: "rules", label: "Visitor's API rules" },
                                { id: "", label: "Everyone (page gate)" },
                            ],
                        },
                    ],
                },
            },
        });
        dc.addType("pb-item", {
            isComponent: (el) => has(el, "data-pb-item"),
            model: { defaults: { name: "Item (repeated for each record)", draggable: "[data-pb-list]", copyable: false } },
        });
        dc.addType("pb-empty", {
            isComponent: (el) => has(el, "data-pb-empty"),
            model: { defaults: { name: "Shown when there are no records", draggable: "[data-pb-list]", copyable: false } },
        });
        dc.addType("pb-field", {
            isComponent: (el) => has(el, "data-pb-field") && el.tagName != "IMG" && el.tagName != "A",
            model: {
                defaults: {
                    name: "Field",
                    icon: '<i class="ri-input-field"></i>',
                    droppable: false,
                    traits: [{ type: "select", name: "data-pb-field", label: "Field", options: [] }],
                },
            },
        });
        dc.addType("pb-image", {
            extend: "image",
            isComponent: (el) => el?.tagName == "IMG" && has(el, "data-pb-field"),
            model: {
                defaults: {
                    name: "Field image",
                    traits: [{ type: "select", name: "data-pb-field", label: "Field", options: [] }, "alt"],
                },
            },
        });
        dc.addType("pb-link", {
            extend: "link",
            isComponent: (el) => el?.tagName == "A" && (has(el, "data-pb-link") || has(el, "data-pb-field") || has(el, "data-pb-href")),
            model: {
                defaults: {
                    name: "Record link",
                    traits: [
                        { type: "select", name: "data-pb-link", label: "Record page", options: [] },
                        { type: "select", name: "data-pb-href", label: "Or link field", options: [] },
                        "title",
                        { type: "select", name: "target", label: "Open in", options: [{ id: "", label: "Same tab" }, { id: "_blank", label: "New tab" }] },
                    ],
                },
            },
        });
        dc.addType("pb-search", {
            isComponent: (el) => el?.tagName == "FORM" && has(el, "data-pb-search"),
            model: {
                defaults: {
                    name: "Search box",
                    icon: '<i class="ri-search-line"></i>',
                    traits: [
                        { type: "select", name: "data-pb-search", label: "Searches", options: [] },
                        { type: "gpp-search-fields", name: "data-pb-search-fields", label: "Search in" },
                    ],
                },
            },
        });
        dc.addType("pb-pager", {
            isComponent: (el) => has(el, "data-pb-pager"),
            model: {
                defaults: {
                    name: "Pagination",
                    icon: '<i class="ri-pages-line"></i>',
                    droppable: false,
                    traits: [{ type: "select", name: "data-pb-pager", label: "Pages of", options: [] }],
                },
            },
        });
        dc.addType("pb-form", {
            isComponent: (el) => el?.tagName == "IFRAME" && has(el, "data-pb-form"),
            model: {
                defaults: {
                    tagName: "iframe",
                    name: "Form",
                    droppable: false,
                    attributes: { "data-pb-form": "", title: "Form", loading: "lazy" },
                    traits: [{ type: "select", name: "data-pb-form", label: "Form", options: formOptions() }],
                },
            },
        });
    }

    function defineBlocks(ed) {
        const bm = ed.BlockManager;
        const icon = (name) => `<i class="${name} gpp-block-icon"></i>`;
        const basic = { category: "Basic" };
        const dataCat = { category: "Data", open: true };
        const button = "display:inline-block;padding:12px 24px;background:#2563eb;color:#fff;border-radius:6px;text-decoration:none;font-weight:600";

        bm.add("section", { ...basic, label: "Section", media: icon("ri-layout-top-line"), content: `<section style="padding:56px 24px"><div style="max-width:1100px;margin:0 auto"><h2 style="margin:0 0 12px;font-size:32px">Section title</h2><p style="margin:0;font-size:18px;color:#555">Describe this section.</p></div></section>` });
        bm.add("columns-2", { ...basic, label: "2 columns", media: icon("ri-layout-column-line"), content: `<div style="display:flex;flex-wrap:wrap;gap:24px;padding:24px"><div style="flex:1 1 280px;min-height:80px"></div><div style="flex:1 1 280px;min-height:80px"></div></div>` });
        bm.add("columns-3", { ...basic, label: "3 columns", media: icon("ri-layout-3-line"), content: `<div style="display:flex;flex-wrap:wrap;gap:24px;padding:24px"><div style="flex:1 1 200px;min-height:80px"></div><div style="flex:1 1 200px;min-height:80px"></div><div style="flex:1 1 200px;min-height:80px"></div></div>` });
        bm.add("heading", { ...basic, label: "Heading", media: icon("ri-heading"), content: `<h2 style="font-size:28px;margin:0 0 12px">Heading</h2>` });
        bm.add("text", { ...basic, label: "Text", media: icon("ri-text"), content: `<p style="margin:0 0 12px">Write your text here.</p>` });
        bm.add("image", { ...basic, label: "Image", media: icon("ri-image-line"), activate: true, content: { type: "image", style: { "max-width": "100%", height: "auto" } } });
        bm.add("button", { ...basic, label: "Button", media: icon("ri-checkbox-blank-line"), content: `<a href="#" style="${button}">Button</a>` });
        bm.add("link", { ...basic, label: "Link", media: icon("ri-link"), content: { type: "link", content: "Link", attributes: { href: "#" } } });
        bm.add("divider", { ...basic, label: "Divider", media: icon("ri-separator"), content: `<hr style="border:0;border-top:1px solid #ddd;margin:24px 0">` });
        bm.add("video", { ...basic, label: "Video", media: icon("ri-video-line"), content: { type: "video", provider: "so", style: { width: "100%", height: "400px" } } }); // a file: YouTube/Vimeo in its settings

        bm.add("pb-list", {
            ...dataCat,
            label: "Collection list",
            media: icon("ri-database-2-line"),
            content: {
                type: "pb-list",
                attributes: { "data-pb-list": "", "data-pb-limit": "12" },
                style: { display: "grid", "grid-template-columns": "repeat(auto-fill,minmax(240px,1fr))", gap: "20px", padding: "24px" },
                components: [
                    {
                        type: "pb-item",
                        attributes: { "data-pb-item": "" },
                        style: { border: "1px solid #e5e7eb", "border-radius": "10px", overflow: "hidden", display: "flex", "flex-direction": "column" },
                        components: [
                            { type: "pb-image", attributes: { "data-pb-field": "" }, style: { width: "100%", height: "180px", "object-fit": "cover", display: "block" } },
                            { type: "pb-field", tagName: "h3", attributes: { "data-pb-field": "" }, style: { margin: "14px 16px 4px", "font-size": "18px" } },
                            { type: "pb-field", tagName: "p", attributes: { "data-pb-field": "" }, style: { margin: "0 16px 16px", color: "#555" } },
                        ],
                    },
                    { type: "pb-empty", attributes: { "data-pb-empty": "" }, style: { padding: "16px", color: "#777" }, components: [{ type: "text", content: "Nothing to show yet." }] },
                ],
            },
        });
        bm.add("pb-search", {
            ...dataCat,
            label: "Search box",
            media: icon("ri-search-line"),
            content: `<form data-pb-search="" data-pb-search-fields="" style="display:flex;gap:8px;max-width:560px;margin:16px auto;padding:0 24px"><input type="search" class="input" placeholder="Search…" aria-label="Search" style="flex:1 1 auto;min-width:0;padding:8px 12px;border:1px solid #d4d4d8;border-radius:6px;font:inherit"><button type="submit" class="btn" style="padding:8px 16px;border-radius:6px;font:inherit;font-weight:600;cursor:pointer">Search</button></form>`,
        });
        bm.add("pb-pager", {
            ...dataCat,
            label: "Pagination",
            media: icon("ri-pages-line"),
            content: `<nav data-pb-pager="" style="display:flex;flex-wrap:wrap;justify-content:center;gap:4px;margin:16px 0"><span class="btn" data-variant="ghost">‹ Previous</span><span class="btn" data-variant="outline">1</span><span class="btn" data-variant="ghost">2</span><span class="btn" data-variant="ghost">3</span><span class="btn" data-variant="ghost">Next ›</span></nav>`,
        });
        bm.add("pb-field", { ...dataCat, label: "Field", media: icon("ri-input-field"), content: { type: "pb-field", tagName: "p", attributes: { "data-pb-field": "" } } });
        bm.add("pb-formula", { ...dataCat, label: "Formula", media: icon("ri-functions"), content: { type: "pb-formula", tagName: "p", attributes: { "data-pb-formula": "" }, components: [{ type: "textnode", content: "ƒ formula" }] } });
        bm.add("pb-image", { ...dataCat, label: "Field image", media: icon("ri-image-2-line"), content: { type: "pb-image", attributes: { "data-pb-field": "" }, style: { "max-width": "100%", height: "auto" } } });
        bm.add("pb-link", { ...dataCat, label: "Record link", media: icon("ri-external-link-line"), content: { type: "pb-link", attributes: { "data-pb-link": "", href: "#" }, style: { display: "inline-block", margin: "0 16px 16px", "font-weight": "600", color: "#2563eb" }, components: [{ type: "textnode", content: "View details" }] } });
        bm.add("pb-form", { ...dataCat, label: "Form", media: icon("ri-survey-line"), content: { type: "pb-form", style: { width: "100%", height: "640px", border: "0" } } });
    }

    // ---- bindings: options, defaults and sample values in the canvas

    const dateFormatTrait = { type: "select", name: "data-pb-format", label: "Show", options: [{ id: "", label: "Date" }, { id: "datetime", label: "Date and time" }, { id: "time", label: "Time" }] };

    function refreshTraits(comp) {
        if (!comp?.getTrait) {
            return;
        }
        if (data.page?.kit) {
            applyKitTraits(comp, shared.pages.filter((p) => !p.record_collection && p.kind != "part").map((p) => ({ title: p.title, path: pagePath(p) })));
        }
        const col = contextCollection(comp);
        const tag = comp.get("tagName");
        // in a list or a record page, any image, link or text can show the record's data
        if (col && !comp.is("pb-list") && !comp.is("pb-item") && !comp.is("pb-empty") && !comp.is("pb-chart") && !comp.is("pb-calendar")) {
            // first: the record's data matters most in a repeated item
            const add = (name, label, at) => comp.getTrait(name) || comp.addTrait({ type: "select", name, label, options: [] }, { at });
            if (tag == "img") {
                add("data-pb-field", "Image field", 0);
            } else if (tag == "a") {
                add("data-pb-link", "Record page", 0);
                add("data-pb-href", "Or link field", 1);
            } else if (comp.is("text") || comp.is("pb-field")) {
                add("data-pb-field", "Show field", 0);
            }
        }
        // any text can be computed by a formula
        const textual = (comp.is("text") || comp.is("pb-field")) && tag != "img" && tag != "a";
        if (textual && !comp.getTrait("gpp-formula")) {
            comp.addTrait({ type: "gpp-formula", name: "gpp-formula", label: "Formula" }, { at: comp.getTrait("data-pb-field") ? 1 : 0 });
        }
        // any element can compute attributes and styles (image address, color...)
        if (comp !== editor.getWrapper() && !comp.is("pb-part")) {
            if (!comp.getTrait("gpp-conditions")) {
                comp.addTrait({ type: "gpp-conditions", name: "gpp-conditions", label: "Conditions" });
            }
            if (!comp.getTrait("gpp-dynamic")) {
                comp.addTrait({ type: "gpp-dynamic", name: "gpp-dynamic", label: "Dynamic values" });
            }
        }
        if (comp.is("pb-search")) {
            comp._pbSearch ??= comp.getAttributes()["data-pb-search"];
        }
        comp.getTrait("data-pb-search")?.set("options", listChoices("- Choose a list -"));
        comp.getTrait("data-pb-pager")?.set("options", listChoices("- Choose a list -"));
        const limitTrait = comp.is("pb-list") && comp.getTrait("data-pb-limit");
        if (limitTrait) {
            const paged = !!comp.getAttributes()["data-pb-paginate"];
            limitTrait.set("label", paged ? "Per page" : "Max records");
            comp.getTrait("data-pb-key")?.set("placeholder", comp.getAttributes()["data-pb-list"] || "the collection name");
        }
        const options = {
            "data-pb-field": () => fieldOptions(comp, tag == "img" ? "image" : "text"),
            "data-pb-href": () => fieldOptions(comp, "link"),
            "data-pb-link": () => recordPageOptions(comp),
        };
        for (const [name, opts] of Object.entries(options)) {
            comp.getTrait(name)?.set("options", opts());
        }
        // the signed-in visitor's fields, and their related records' ("team.name")
        const userTrait = comp.getTrait("data-pb-user");
        const users = byName("users");
        if (userTrait && users) {
            userTrait.set("options", [{ id: "", label: "- Select a field -" }, ...bindableFields(users, "text", byName).map((b) => ({ id: b.path, label: b.label }))]);
        }
        // date format: only for date fields
        const path = comp.getAttributes()["data-pb-field"];
        const f = path ? fieldAt(col, path, byName) : null;
        const isDate = f && (f.type == "date" || f.type == "autodate");
        if (isDate && !comp.getTrait("data-pb-format")) {
            comp.addTrait(dateFormatTrait);
        } else if (!isDate && comp.getTrait("data-pb-format")) {
            comp.removeTrait("data-pb-format");
        }
    }

    /** Binds the unbound fields of a component (and its children) to sensible fields. */
    function autofill(root) {
        const comps = [root, ...root.find("[data-pb-field], [data-pb-link]")];
        const used = new Map();
        for (const comp of comps) {
            const attrs = comp.getAttributes();
            const col = contextCollection(comp);
            if (!col) {
                continue;
            }
            if (comp.is("pb-link") && attrs["data-pb-link"] === "" && !attrs["data-pb-field"]) {
                const target = shared.pages.find((p) => p.record_collection == col.id);
                if (target) {
                    comp.addAttributes({ "data-pb-link": target.slug });
                }
                continue;
            }
            if (attrs["data-pb-field"] !== "" || comp.is("pb-link")) {
                continue;
            }
            const taken = used.get(col.name) || new Set();
            used.set(col.name, taken);
            let pick;
            if (comp.is("pb-image")) {
                pick = fieldsOf(col, "image").find((f) => f.type == "file") || fieldsOf(col, "image").find((f) => f.type == "url");
                if (!pick) {
                    // no image of its own: a related record's (e.g. the brand's logo)
                    const rel = bindableFields(col, "image", byName).find((b) => b.path.includes(".") && b.field.type == "file");
                    pick = rel && { name: rel.path };
                }
                if (!pick) {
                    comp.remove(); // no image field: drop the placeholder image
                    continue;
                }
            } else if (/^h[1-3]$/.test(comp.get("tagName")) || attrs["aria-current"] !== undefined) {
                // titles show the record name
                pick = labelFields(col)[0] || fieldsOf(col, "text").find((f) => f.name != "id");
            } else {
                const candidates = [...labelFields(col), ...fieldsOf(col, "text").filter((f) => !["id", "created", "updated"].includes(f.name))];
                pick = candidates.find((f) => !taken.has(f.name));
            }
            if (pick) {
                taken.add(pick.name);
                comp.addAttributes({ "data-pb-field": pick.name });
            }
        }
        // record tables: the column headers name the fields of the cells
        const lists = [root, ...root.find("[data-pb-list]")].filter((c) => c.get("tagName") == "tbody");
        for (const tbody of lists) {
            const col = byName(tbody.getAttributes()["data-pb-list"]);
            const row = tbody.find("[data-pb-item]")[0];
            const heads = tbody.parent()?.find("thead th") || [];
            row?.components().forEach((cell, i) => {
                const name = cell.find("[data-pb-field]")[0]?.getAttributes()["data-pb-field"];
                const f = col?.fields.find((x) => x.name == name);
                if (f && heads[i] && /^(Name|Field|\{.*\})$/.test(heads[i].getInnerHTML().trim())) {
                    heads[i].components(f.name[0].toUpperCase() + f.name.slice(1).replace(/_/g, " "));
                }
            });
        }
    }

    /** Shows sample values of the bound fields (the server shows the real ones). */
    async function applySamples(root) {
        const comps = [root, ...root.find("[data-pb-field], [data-pb-form], [data-pb-formula]")].filter((c) => c.getAttributes);
        const dynamic = [root, ...root.find("*")].filter((c) => c.getAttributes && (dynamicValues(c).length || c.getAttributes()["data-pb-conditions"] !== undefined || c.view?.el?._gppCondClasses));
        for (const comp of comps) {
            const attrs = comp.getAttributes();
            if (attrs["data-pb-formula"] !== undefined) {
                const rec = await contextSample(comp);
                const text = await formulaSample(id, attrs["data-pb-formula"], contextCollection(comp), rec?.id, data.viewAs || "");
                if (comp.getInnerHTML() != text.replace(/[<>&]/g, (c) => ({ "<": "&lt;", ">": "&gt;", "&": "&amp;" })[c])) {
                    comp.components([{ type: "textnode", content: text }]);
                }
                continue;
            }
            if (attrs["data-pb-form"] !== undefined) {
                const src = attrs["data-pb-form"] ? `/f/${encodeURIComponent(attrs["data-pb-form"])}/embed` : "about:blank";
                if (attrs.src != src) {
                    comp.addAttributes({ src });
                }
                continue;
            }
            const name = attrs["data-pb-field"];
            if (name === undefined || comp.is("pb-link")) {
                continue;
            }
            const { col, rec, f } = await samplePath(contextCollection(comp), await contextSample(comp), name);
            if (comp.get("tagName") == "img") {
                if (comp.getAttributes()["data-pb-attr-src"]) {
                    continue; // a computed address replaces the field (set since)
                }
                const url = sampleURL(col, rec, f) || placeholderImage;
                if (comp.get("src") != url) {
                    comp.set("src", url);
                }
            } else {
                const text = name ? sampleText(rec, f) : "{field}";
                if (comp.getInnerHTML() != text) {
                    comp.components([{ type: "textnode", content: text }]);
                }
            }
        }
        // after the fields: a computed image address replaces the field's
        for (const comp of dynamic) {
            const rec = await contextSample(comp);
            await applyDynamic(id, comp, contextCollection(comp), rec?.id, data.viewAs || "");
            await applyConditions(id, comp, contextCollection(comp), rec?.id, data.viewAs || "");
        }
    }

    const bindingKey = (comp) => {
        const a = comp.getAttributes();
        const dyn = dynamicValues(comp).map((v) => v.key + "=" + v.formula);
        return [a["data-pb-list"], a["data-pb-field"], a["data-pb-link"], a["data-pb-form"], a["data-pb-filter"], a["data-pb-sort"], a["data-pb-format"], a["data-pb-href"], a["data-pb-formula"], a["data-pb-conditions"], ...dyn].join("|");
    };

    function onAttributes(comp) {
        // a binding set back to "none" on an element that isn't a data block: no attribute
        const a = comp.getAttributes();
        const plain = !comp.is("pb-field") && !comp.is("pb-image") && !comp.is("pb-link");
        const unset = ["data-pb-field", "data-pb-href", "data-pb-link"].filter((n) => a[n] === "" && (plain || n == "data-pb-href"));
        if (unset.length) {
            comp.removeAttributes(unset);
            return;
        }
        // a search box set to another list: that list's usual fields
        if (comp.is("pb-search") && comp._pbSearch !== undefined && comp._pbSearch !== a["data-pb-search"]) {
            comp._pbSearch = a["data-pb-search"];
            const list = pageLists().find((l) => l.key == a["data-pb-search"]);
            comp.addAttributes({ "data-pb-search-fields": list?.col ? defaultSearchFields(list.col).join(",") : "" });
        }
        if (comp.is("pb-list") && comp._pbPaginate !== (a["data-pb-paginate"] || "")) {
            comp._pbPaginate = a["data-pb-paginate"] || "";
            refreshTraits(comp);
        }
        const key = bindingKey(comp);
        if (comp._pbKey === key) {
            return;
        }
        const listChanged = comp.is("pb-list") && (comp._pbKey ?? "").split("|")[0] != key.split("|")[0];
        comp._pbKey = key;
        if (listChanged) {
            autofill(comp);
        }
        applySamples(comp);
        refreshTraits(editor.getSelected());
    }

    // ---- mount, save, preview

    function mount(el) {
        const page = data.page;
        const project = page.project && page.project.pages ? page.project : null;
        const ed = grapesjs.init({
            container: el,
            height: "100%",
            width: "auto",
            telemetry: false,
            // the component types before the content is parsed (else it's plain elements)
            plugins: [
                (ed) => {
                    defineKitTraits(ed);
                    defineKitPageTrait(ed);
                    defineTypes(ed);
                    defineMemberTypes(ed);
                    defineWidgetTypes(ed, widgetCtx);
                    defineFormula(ed, formulaCtx);
                    defineConditions(ed, formulaCtx);
                    defineSearchTraits(ed);
                    defineColumnsTrait(ed, {
                        byName,
                        // own and related fields: { name: path, type }
                        fields: (col) => {
                            const all = [...bindableFields(col, "text", byName), ...bindableFields(col, "image", byName).filter((b) => b.field.type == "file")];
                            const seen = new Set();
                            return all.filter((b) => !seen.has(b.path) && seen.add(b.path)).map((b) => ({ name: b.path, type: b.field.type, label: b.label }));
                        },
                        applySamples: (comp) => applySamples(comp),
                        placeholderImage,
                    });
                },
            ],
            cssIcons: "", // no Font Awesome from a CDN (the dashboard CSP blocks it)
            storageManager: false,
            avoidInlineStyle: true,
            // saved project, or the starter HTML of a new page
            ...(project ? { projectData: project } : { components: page.html || "", style: page.css || (page.kit ? "" : defaultCSS) }),
            selectorManager: { componentFirst: true },
            canvas: { styles: [base + "canvas.css", base + "ui/gp-ui.css"], scripts: [base + "ui/gp-ui.js"] },
            deviceManager: {
                devices: [
                    { id: "desktop", name: "Desktop", width: "" },
                    { id: "tablet", name: "Tablet", width: "770px", widthMedia: "992px" },
                    { id: "mobile", name: "Phone", width: "375px", widthMedia: "480px" },
                ],
            },
            assetManager: {
                assets: (page.assets || []).map((n) => assetURL(page.id, n)),
                uploadFile: async (e) => {
                    const files = [...(e.dataTransfer?.files || e.target?.files || [])];
                    if (!files.length) {
                        return;
                    }
                    const before = new Set(data.page.assets || []);
                    const form = new FormData();
                    files.forEach((f) => form.append("assets+", f));
                    try {
                        const rec = await app.pb.collection("pages").update(id, form, { fields: "assets" });
                        data.page.assets = rec.assets;
                        ed.AssetManager.add(rec.assets.filter((n) => !before.has(n)).map((n) => assetURL(id, n)));
                    } catch (err) {
                        handleError(err);
                    }
                },
            },
            panels: {
                defaults: [
                    { id: "commands", buttons: [] },
                    {
                        id: "options",
                        buttons: [
                            { id: "sw-visibility", className: "ri-shape-line", command: "core:component-outline", context: "sw-visibility", active: true, attributes: { title: "Show outlines" } },
                            { id: "canvas-mode", className: "ri-contrast-2-line", command: (e) => toggleCanvasMode(e, data.page.kit, theme), togglable: false, attributes: { title: "Preview in light / dark mode (Basecoat pages)" } },
                            { id: "preview", className: "ri-eye-line", command: "preview", context: "preview", attributes: { title: "Hide the panels" } },
                            { id: "undo", className: "ri-arrow-go-back-line", command: "core:undo", attributes: { title: "Undo" } },
                            { id: "redo", className: "ri-arrow-go-forward-line", command: "core:redo", attributes: { title: "Redo" } },
                            { id: "export-template", className: "ri-code-s-slash-line", command: "export-template", attributes: { title: "View code" } },
                        ],
                    },
                    {
                        id: "views",
                        buttons: [
                            { id: "open-blocks", className: "ri-apps-2-line", command: "open-blocks", active: true, togglable: false, attributes: { title: "Blocks" } },
                            { id: "open-tm", className: "ri-settings-3-line", command: "open-tm", togglable: false, attributes: { title: "Settings of the selected element" } },
                            { id: "open-sm", className: "ri-paint-brush-line", command: "open-sm", togglable: false, attributes: { title: "Styles of the selected element" } },
                            { id: "open-layers", className: "ri-stack-line", command: "open-layers", togglable: false, attributes: { title: "Layers" } },
                        ],
                    },
                    {
                        id: "devices",
                        buttons: [
                            { id: "desktop", className: "ri-computer-line", command: (e) => e.setDevice("desktop"), active: true, togglable: false, attributes: { title: "Desktop" } },
                            { id: "tablet", className: "ri-tablet-line", command: (e) => e.setDevice("tablet"), togglable: false, attributes: { title: "Tablet" } },
                            { id: "mobile", className: "ri-smartphone-line", command: (e) => e.setDevice("mobile"), togglable: false, attributes: { title: "Phone" } },
                        ],
                    },
                ],
            },
        });
        editor = ed;
        defineBlocks(ed);
        setKitBlocks(ed, page.kit);
        setPartBlocks(ed, !page.isPart); // parts don't nest
        setMemberBlocks(ed, page.kit);
        setWidgetBlocks(ed);
        addBlockSearch(ed);
        // Basecoat classes: never shown as style tags (styling a button never restyles every .btn)
        kitClasses.forEach((name) => ed.Selectors.add({ name, private: true }));

        ed.on("load", () => {
            // placeholders saved as plain elements (before the parts type existed)
            ed.getWrapper()
                .find("[data-pb-part]")
                .filter((c) => !c.is("pb-part"))
                .forEach((c) => c.replaceWith({ type: "pb-part", attributes: { ...c.getAttributes() } }));
            // record links saved with their URL field as data-pb-field: data-pb-href
            ed.getWrapper()
                .find("a[data-pb-field]")
                .forEach((c) => {
                    const f = fieldAt(contextCollection(c), c.getAttributes()["data-pb-field"], byName);
                    if (f && ["url", "email", "file"].includes(f.type)) {
                        c.addAttributes({ "data-pb-href": c.getAttributes()["data-pb-field"] });
                        c.removeAttributes("data-pb-field");
                    }
                });
            // the first changes are the loading itself (the samples come later)
            refreshCanvas().finally(() =>
                setTimeout(() => {
                    data.dirty = false;
                    ed.clearDirtyCount(); // a late "update" of the loading isn't a change
                    ed.on("update", () => (data.dirty ||= ed.getDirtyCount() > 0));
                    offerDraft(id, data.page.updated, (project) => restoreProject({ project }));
                })
            );
        });
        ed.on("component:add", () => applyPartsCSS(ed));
        ed.on("component:selected", (comp) => {
            addVisibleTrait(comp);
            refreshMemberTraits(comp);
            refreshTraits(comp);
            addPartTools(comp);
            refreshChartTraits(comp, widgetCtx);
            addColumnsTrait(comp);
            if (comp.getTraits().length) {
                ed.runCommand("open-tm"); // show the bindings of data elements
                ed.Panels.getButton("views", "open-tm")?.set("active", true);
            }
        });
        ed.on("component:update:attributes", onAttributes);
        ed.on("block:drag:stop", (comp) => {
            if (comp) {
                // new lists show what the visitor may read (the collection's API rules)
                [comp, ...comp.find("[data-pb-list]")]
                    .filter((c) => c.getAttributes()["data-pb-list"] !== undefined && c.getAttributes()["data-pb-access"] === undefined)
                    .forEach((c) => c.addAttributes({ "data-pb-access": "rules" }));
                linkIds(comp); // unique ids for dialogs, tabs and menus
                autofill(comp);
                linkSearch(comp);
                applySamples(comp);
                ed.select(comp);
                if (comp.is("pb-formula") && !comp.getAttributes()["data-pb-formula"]) {
                    formulaModal(ed, comp, formulaCtx);
                }
            }
        });
        ed.on("canvas:drop", () => applySamples(ed.getWrapper()));
    }

    /** Applies the kit, the parts, the bindings and the samples to the canvas content. */
    function refreshCanvas() {
        const ed = editor;
        applyCanvasKit(ed, data.page.kit, theme);
        applyPartsCSS(ed);
        applyViewAs(ed, data.viewAs);
        autofill(ed.getWrapper());
        ed.getWrapper().find("[data-pb-list], [data-pb-field], [data-pb-link], [data-pb-form], [data-pb-formula]").forEach((c) => (c._pbKey = bindingKey(c)));
        return applySamples(ed.getWrapper()).catch(() => null);
    }

    /** Loads a version (or a local copy) in the editor: saved with the next save. */
    function restoreProject(v) {
        if (!editor) {
            return;
        }
        if (v.project?.pages) {
            editor.loadProjectData(v.project);
        } else {
            editor.setComponents(v.html || "");
            editor.setStyle(v.css || "");
        }
        if (v.kit !== undefined && (v.kit == "basecoat") != data.page.kit) {
            data.page.kit = v.kit == "basecoat";
            setKitBlocks(editor, data.page.kit);
            setMemberBlocks(editor, data.page.kit);
        }
        setTimeout(() => {
            refreshCanvas();
            data.dirty = true;
        }, 100);
    }

    // toolbar of the selected component: site part tools
    function addPartTools(comp) {
        if (!comp || comp === editor.getWrapper() || comp._gppTools) {
            return;
        }
        comp._gppTools = true;
        const tb = [...(comp.get("toolbar") || [])];
        if (comp.is("pb-part")) {
            refreshPartTrait(comp);
            tb.unshift(
                { attributes: { class: "ri-edit-line", title: "Edit this site part (every page using it changes)" }, command: () => editPart(comp) },
                { attributes: { class: "ri-link-unlink", title: "Unlink: an editable copy for this page only" }, command: (ed) => unlinkPart(ed, comp) },
            );
        } else if (!data.page.isPart) {
            tb.unshift({
                attributes: { class: "ri-bookmark-line", title: "Save as a site part (reuse it on other pages)" },
                command: (ed) =>
                    saveAsPartModal(ed, comp, {
                        kit: () => data.page.kit,
                        slugify,
                        takenSlugs: () => new Set(shared.pages.map((p) => p.slug)),
                        onCreated: () => {
                            data.dirty = true;
                            loadPages();
                        },
                    }),
            });
        }
        comp.set("toolbar", tb);
    }

    async function editPart(comp) {
        const p = partOf(comp);
        if (!p) {
            return;
        }
        if (data.dirty && !(await save())) {
            return;
        }
        window.location.hash = "#/pages/" + p.id;
    }

    async function duplicate() {
        if (data.dirty && !(await save())) {
            return;
        }
        try {
            const res = await api(`/api/pages/${id}/duplicate`, { method: "POST" });
            loadPages();
            app.toasts.success("Copied. You're now editing the copy (unpublished).");
            window.location.hash = "#/pages/" + res.id;
        } catch (err) {
            handleError(err);
        }
    }

    function openHistory() {
        historyModal({ id, isPart: data.page.isPart, restore: restoreProject });
    }

    async function save() {
        if (!editor || data.isSaving) {
            return;
        }
        data.isSaving = true;
        try {
            const p = data.page;
            const rec = await app.pb.collection("pages").update(id, {
                title: p.title,
                slug: p.slug,
                description: p.description,
                published: p.published && !p.isPart,
                home: p.home && !p.record_collection && !p.isPart,
                record_filter: p.record_filter,
                record_rules: !!p.record_rules && !!p.record_collection,
                kit: p.kit ? "basecoat" : "",
                access: p.isPart ? "public" : p.access,
                roles: p.access == "roles" && !p.isPart ? p.roles : [],
                html: editor.getHtml(),
                css: editor.getCss(),
                project: editor.getProjectData(),
            });
            data.page = { ...data.page, home: rec.home, updated: rec.updated };
            data.dirty = false;
            editor?.clearDirtyCount();
            clearDraft(id);
            app.store.title = rec.title;
            loadPages();
            app.toasts.success("Successfully saved the page.");
            warnRemote(rec.html + rec.css);
            warnGateLists();
            return true;
        } catch (err) {
            handleError(err);
            return false;
        } finally {
            data.isSaving = false;
        }
    }

    /** Lists showing data the collection's API rules keep private (Data access: page is the gate). */
    function warnGateLists() {
        const names = new Set(
            editor.getWrapper()
                .find("[data-pb-list]")
                .filter((c) => c.getAttributes()["data-pb-access"] != "rules")
                .map((c) => byName(c.getAttributes()["data-pb-list"]))
                .filter((col) => col && col.listRule !== "")
                .map((col) => col.name),
        );
        if (names.size) {
            app.toasts.info(`Lists of ${[...names].join(", ")} show records their API rules keep private, to everyone who can open the page ("Data access: page is the gate"). Choose "API rules of the visitor" to follow the rules.`);
        }
    }

    /** Offline mode: remote files (images, fonts, videos) won't load on the published page. */
    function warnRemote(src) {
        if (!offlineMode) {
            return;
        }
        // files the page loads (links to other sites are fine)
        const re = /(?:src|poster)=["'](https?:\/\/[^"']+)|url\(\s*["']?(https?:\/\/[^"')\s]+)|@import\s+(?:url\()?["']?(https?:\/\/[^"')\s;]+)/g;
        const remote = [...new Set([...src.matchAll(re)].map((m) => m[1] || m[2] || m[3]))];
        if (remote.length) {
            app.toasts.info(`Offline mode: this page loads files from the internet that won't show (${remote.slice(0, 3).join(", ")}${remote.length > 3 ? "…" : ""}). Upload them instead.`);
        }
    }

    async function preview() {
        const win = window.open("", "_blank"); // opened now: popups need the click
        if (data.dirty && !(await save())) {
            win?.close();
            return;
        }
        try {
            const as = data.viewAs ? "?as=" + encodeURIComponent(data.viewAs) : "";
            const res = await api(`/api/pages/${id}/preview${as}`, { method: "POST" });
            if (win) {
                win.opener = null;
                win.location = res.url;
            }
        } catch (err) {
            win?.close();
            handleError(err);
        }
    }

    function settingsModal() {
        const p = data.page;
        const draft = store({ title: p.title, slug: p.slug, description: p.description, published: p.published, home: p.home, record_filter: p.record_filter, record_rules: !!p.record_rules, kit: p.kit, access: p.access, roles: [...(p.roles || [])] });
        const col = recordCollection();

        function apply() {
            const kitChanged = draft.kit != data.page.kit;
            Object.assign(data.page, { ...draft, slug: slugify(draft.slug) });
            if (kitChanged && editor) {
                setKitBlocks(editor, draft.kit);
                setMemberBlocks(editor, draft.kit);
                applyCanvasKit(editor, draft.kit, theme);
            }
            data.dirty = true;
            app.modals.close(modal);
        }

        function remove() {
            app.modals.confirm(`Delete ${p.isPart ? "the site part" : "the page"} "${p.title}"? Its uploaded images and versions are deleted too.${p.isPart ? " The pages using it no longer show it." : ""}`, async () => {
                try {
                    await app.pb.collection("pages").delete(id);
                    data.dirty = false;
                    loadPages();
                    app.modals.close(modal);
                    window.location.hash = "#/pages";
                } catch (err) {
                    handleError(err);
                }
            });
        }

        const modal = t.div(
            { className: "modal popup" },
            t.header({ className: "modal-header" }, t.h5({ className: "m-auto" }, "Page settings")),
            t.form(
                { id: "gpPageSettings", className: "modal-content", onsubmit: (e) => (e.preventDefault(), apply()) },
                field({ label: "Title", hint: "The browser tab title (record pages add the record name)." }, textInput(draft, "title", { required: true })),
                field(
                    { label: "Slug", hint: () => "Address: " + pageURL(slugify(draft.slug), col ? "{record id}" : "") },
                    textInput(draft, "slug", { required: true, className: "txt-code" }),
                ),
                field({ label: "Description", hint: "Shown by search engines." }, t.textarea({ rows: 2, maxLength: 500, value: () => draft.description || "", oninput: (e) => (draft.description = e.target.value) })),
                col
                    ? t.div(
                        null,
                        field(
                            { label: `Records of ${col.name} shown`, hint: "Optional filter: other records get a 404 (e.g. published = true)." },
                            textInput(draft, "record_filter", { className: "txt-code", placeholder: "e.g. published = true" }),
                        ),
                        t.div(
                            { className: "m-b-sm" },
                            switchInput(draft, "record_rules", `Apply the view rule of ${col.name} for the visitor`),
                            t.div({ className: "field-help" }, "Like the API: a record the visitor can't view gets a 404 (guests are asked to sign in), and the fields their roles can't read are hidden."),
                        ),
                    )
                    : p.isPart
                        ? null
                        : t.div({ className: "m-b-sm" }, switchInput(draft, "home", "Home page (also served at /)")),
                t.div(
                    { className: "m-b-sm" },
                    switchInput(draft, "kit", "Basecoat UI kit (components, sections and the site theme)"),
                    t.div({ className: "field-help" }, "Its base styles reset headings, margins and images: turning it on can change the look of a page built without it."),
                ),
                p.isPart
                    ? null
                    : field(
                        { label: "Who can open the page", hint: "Guests opening a protected page are sent to the sign in page (Pages → Members & sign in). Hide elements by role with their \"Visible to\" setting." },
                        select({
                            required: true,
                            options: [
                                { value: "public", label: "Public: everyone" },
                                { value: "users", label: "Signed-in users" },
                                { value: "roles", label: "Some roles" },
                            ],
                            value: () => draft.access || "public",
                            onchange: (v) => (draft.access = v),
                        }),
                    ),
                () => (!p.isPart && draft.access == "roles"
                    ? t.div(
                        { rid: "roles", className: "gpp-role-checks m-b-sm" },
                        members.roles.length
                            ? members.roles.map((r) =>
                                t.div(
                                    { className: "field" },
                                    t.input({
                                        id: "gppRole" + r.id,
                                        type: "checkbox",
                                        checked: () => draft.roles.includes(r.id),
                                        onchange: (e) => (draft.roles = e.target.checked ? [...draft.roles, r.id] : draft.roles.filter((x) => x != r.id)),
                                    }),
                                    t.label({ htmlFor: "gppRole" + r.id }, r.name),
                                )
                            )
                            : t.p({ className: "txt-hint" }, "No roles yet: create them in Permissions → Roles & users."),
                    )
                    : null),
                p.isPart
                    ? t.p({ className: "txt-hint m-0" }, "A site part isn't published on its own: it's shown on the pages where it's placed (Site parts blocks).")
                    : switchInput(draft, "published", "Published"),
            ),
            t.footer(
                { className: "modal-footer" },
                t.button({ type: "button", className: "btn transparent danger", onclick: remove }, t.i({ className: "ri-delete-bin-7-line", ariaHidden: true }), t.span({ className: "txt" }, "Delete")),
                t.button(
                    { type: "button", className: "btn transparent secondary m-r-auto", onclick: () => (app.modals.close(modal), duplicate()), ariaDescription: app.attrs.tooltip("Saves, then makes an unpublished copy with its images") },
                    t.i({ className: "ri-file-copy-line", ariaHidden: true }),
                    t.span({ className: "txt" }, "Duplicate"),
                ),
                t.button({ type: "button", className: "btn transparent secondary", onclick: () => app.modals.close(modal) }, t.span({ className: "txt" }, "Cancel")),
                t.button({ type: "submit", "html-form": "gpPageSettings", className: "btn expanded", disabled: () => draft.access == "roles" && !draft.roles.length }, t.span({ className: "txt" }, "Apply")),
            ),
        );
        modal.onafterclose = () => modal.remove();
        document.body.appendChild(modal);
        app.modals.open(modal);
    }

    const onKeydown = (e) => {
        if ((e.ctrlKey || e.metaKey) && e.key == "s") {
            e.preventDefault();
            save();
        }
    };
    const keepDraft = () => {
        if (editor && data.dirty) {
            storeDraft(id, editor.getProjectData());
        }
    };
    let draftTimer = null;
    const onBeforeUnload = (e) => {
        if (data.dirty) {
            keepDraft();
            e.preventDefault();
        }
    };

    load();

    return t.div(
        { pbEvent: "gpPageBuilder", className: "page gp-page gpp-builder-page" },
        t.div(
            { className: "page-content gpp-builder" },
            t.header(
                { className: "page-header" },
                t.nav(
                    { className: "breadcrumbs" },
                    t.div({ className: "breadcrumb-item" }, t.a({ href: "#/pages" }, "Pages")),
                    t.div({ className: "breadcrumb-item" }, () => data.page?.title || "…"),
                ),
                t.div(
                    { className: "page-header-primary-btns" },
                    () => data.page?.published
                        ? t.a(
                            { rid: "open" + data.page.slug + data.page.home, href: data.page.record_collection ? null : pagePath(data.page), target: "_blank", rel: "noopener", className: "label sm success gpp-status", ariaDescription: app.attrs.tooltip(data.page.record_collection ? "Published at " + pageURL(data.page.slug, "{id}") : "Open the published page") },
                            "published",
                        )
                        : data.page
                            ? t.span({ rid: "draft", className: "label sm gpp-status" }, "draft")
                            : null,
                    () => (data.page?.isPart ? t.span({ rid: "part", className: "label sm warning gpp-status" }, "site part") : null),
                    () => (data.page && !data.page.isPart && data.page.access != "public"
                        ? t.span({ rid: "access" + data.page.access, className: "label sm gpp-status", ariaDescription: app.attrs.tooltip("Who can open the page: " + accessLabel(data.page)) }, t.i({ className: "ri-lock-line", ariaHidden: true }), data.page.access == "users" ? "members" : "roles")
                        : null),
                    // the dashboard select (native <select> popups ignore the dark mode)
                    t.div(
                        { className: "gpp-viewas", ariaDescription: app.attrs.tooltip("See the page as a visitor: elements hidden from them disappear, Preview opens it as them") },
                        t.i({ className: "ri-user-search-line", ariaHidden: true }),
                        select({
                            required: true,
                            name: "gppViewAs",
                            options: () => viewerOptions().map((o) => ({ value: o.value || "all", label: o.label })),
                            value: () => data.viewAs || "all",
                            onchange: (v) => {
                                data.viewAs = v == "all" ? "" : v;
                                applyViewAs(editor, data.viewAs);
                                refreshWidgets(editor);
                            },
                        }),
                    ),
                    t.button({ type: "button", className: "btn sm secondary transparent", disabled: () => !data.page, onclick: openHistory, ariaDescription: app.attrs.tooltip("Saved versions: preview or restore") }, t.i({ className: "ri-history-line", ariaHidden: true }), t.span({ className: "txt" }, "History")),
                    t.button({ type: "button", className: "btn sm secondary transparent", disabled: () => !data.page, onclick: settingsModal }, t.i({ className: "ri-settings-3-line", ariaHidden: true }), t.span({ className: "txt" }, "Settings")),
                    t.button({ type: "button", className: "btn sm secondary transparent", hidden: () => !!data.page?.isPart, disabled: () => !data.page, onclick: preview, ariaDescription: app.attrs.tooltip("Saves, then opens the page with your data (drafts too)") }, t.i({ className: "ri-eye-line", ariaHidden: true }), t.span({ className: "txt" }, "Preview")),
                    t.button(
                        { type: "button", className: () => `btn sm expanded ${data.isSaving ? "loading" : ""}`, disabled: () => !data.page || data.isSaving || !data.dirty, onclick: save },
                        t.span({ className: "txt" }, () => (data.dirty ? "Save changes" : "Saved")),
                    ),
                ),
            ),
            t.div(
                {
                    className: "gpp-editor-wrap",
                    onmount: () => {
                        window.addEventListener("keydown", onKeydown);
                        window.addEventListener("beforeunload", onBeforeUnload);
                        draftTimer = setInterval(keepDraft, 20000);
                    },
                    onunmount: () => {
                        window.removeEventListener("keydown", onKeydown);
                        window.removeEventListener("beforeunload", onBeforeUnload);
                        clearInterval(draftTimer);
                        keepDraft(); // left with unsaved changes: offered back next time
                        editor?.destroy();
                        editor = null;
                    },
                },
                () => {
                    if (data.error) {
                        return t.div({ rid: "error", className: "txt-center txt-hint p-base" }, data.error);
                    }
                    if (!data.page) {
                        return t.div({ rid: "loading", className: "txt-center p-base" }, t.span({ className: "loader" }));
                    }
                    return t.div(
                        { rid: "editor-" + data.page.id, className: "gpp-editor-col" },
                        () => (data.viewAs && !viewerCanOpen(data.page, data.viewAs)
                            ? t.div(
                                { rid: "denied", className: "gpp-viewas-banner" },
                                t.i({ className: "ri-lock-line", ariaHidden: true }),
                                data.viewAs == "public"
                                    ? " This visitor can't open the page: guests are sent to the sign in page."
                                    : " This visitor can't open the page (\"No access\"). Change who can open it in the Settings.",
                            )
                            : null),
                        t.div({ className: "gpp-editor", onmount: (el) => mount(el) }),
                    );
                },
            ),
        ),
    );
}
