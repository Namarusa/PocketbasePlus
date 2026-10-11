// Pages: site parts. A part (header, footer, sidebar...) is a page of kind
// "part" placed on other pages by a placeholder (<div data-pb-part="slug">):
// the server puts its HTML there, so editing the part changes every page
// using it. In the editor, the placeholder shows the part (not editable there).

import { field, handleError } from "../pocketbaseplus/lib.js";

export const parts = { list: [] };

export async function loadParts() {
    try {
        parts.list = await app.pb.collection("pages").getFullList({ filter: 'kind = "part"', sort: "title", fields: "id,title,slug,html,css,kit", requestKey: null });
    } catch (err) {
        handleError(err);
    }
    return parts.list;
}

const bySlug = (slug) => parts.list.find((p) => p.slug == slug);

/** The inner HTML of a part's body, without scripts, event handlers or nested parts. */
export function partBody(html) {
    const doc = new DOMParser().parseFromString(html || "", "text/html");
    doc.body.querySelectorAll("script, [data-pb-part]").forEach((el) => el.remove());
    for (const el of doc.body.querySelectorAll("*")) {
        for (const a of [...el.attributes]) {
            if (a.name.startsWith("on")) {
                el.removeAttribute(a.name);
            }
        }
    }
    return doc.body.innerHTML;
}

const partOptions = () => [{ id: "", label: "- Select a site part -" }, ...parts.list.map((p) => ({ id: p.slug, label: p.title }))];

/** The placeholder component of the parts. */
export function definePartType(ed) {
    ed.DomComponents.addType("pb-part", {
        isComponent: (el) => el?.nodeType == 1 && el.hasAttribute("data-pb-part"),
        model: {
            defaults: {
                name: "Site part",
                icon: '<i class="ri-layout-top-2-line"></i>',
                droppable: false,
                editable: false,
                components: [],
                traits: [{ type: "select", name: "data-pb-part", label: "Site part", options: [] }],
            },
            init() {
                this.on("change:attributes:data-pb-part", () => {
                    this.view?.render();
                    applyPartsCSS(ed);
                });
            },
        },
        view: {
            onRender({ el, model }) {
                const p = bySlug(model.getAttributes()["data-pb-part"]);
                el.innerHTML = p ? partBody(p.html) : '<p style="margin:0;padding:12px;font:13px system-ui,sans-serif">Choose a site part in the settings.</p>';
                el.setAttribute("data-gpp-label", "Site part: " + (p?.title || "none"));
            },
        },
    });
}

/** Refreshes the options of the part setting of a placeholder. */
export function refreshPartTrait(comp) {
    comp?.getTrait?.("data-pb-part")?.set("options", partOptions());
}

/** The CSS of the parts on the page, in the canvas. */
export function applyPartsCSS(ed) {
    const doc = ed.Canvas.getDocument();
    if (!doc) {
        return;
    }
    const used = new Set(ed.getWrapper().find("[data-pb-part]").map((c) => c.getAttributes()["data-pb-part"]));
    const css = [...used].map((s) => bySlug(s)?.css || "").join("\n");
    let style = doc.getElementById("gpp-parts");
    if (!style) {
        style = doc.createElement("style");
        style.id = "gpp-parts";
        doc.head.appendChild(style);
    }
    if (style.textContent != css) {
        style.textContent = css;
    }
}

/** The "Site parts" blocks (none in a part: parts don't nest). */
export function setPartBlocks(ed, enabled) {
    const bm = ed.BlockManager;
    bm.getAll()
        .filter((b) => b.get("id").startsWith("gpp-part-"))
        .forEach((b) => bm.remove(b.get("id")));
    if (!enabled) {
        return;
    }
    for (const p of parts.list) {
        bm.add("gpp-part-" + p.id, {
            category: { id: "Site parts", label: "Site parts", open: true },
            label: p.title,
            media: '<i class="ri-layout-top-2-line gpp-block-icon"></i>',
            content: { type: "pb-part", attributes: { "data-pb-part": p.slug } },
        });
    }
}

/** Replaces a placeholder with an editable copy of its part. */
export function unlinkPart(ed, comp) {
    const p = bySlug(comp.getAttributes()["data-pb-part"]);
    if (!p) {
        comp.remove();
        return;
    }
    if (p.css) {
        ed.Css.addRules(p.css);
    }
    const added = comp.replaceWith(partBody(p.html));
    applyPartsCSS(ed);
    if (added?.[0]) {
        ed.select(added[0]);
    }
}

export const partOf = (comp) => bySlug(comp.getAttributes()["data-pb-part"]);

/** A slug not used by any page or part. */
function freeSlug(base, taken) {
    let s = base;
    for (let n = 2; taken.has(s); n++) {
        s = `${base}-${n}`;
    }
    return s;
}

/**
 * Makes a selected component a site part: saved as a new part and replaced by
 * its placeholder. ctx: { kit, slugify, takenSlugs, onCreated }.
 */
export function saveAsPartModal(ed, comp, ctx) {
    const data = store({ title: "", isSaving: false });

    async function create() {
        data.isSaving = true;
        try {
            const slug = freeSlug(ctx.slugify(data.title), ctx.takenSlugs());
            const rec = await app.pb.collection("pages").create({
                title: data.title,
                slug,
                kind: "part",
                published: false,
                kit: ctx.kit() ? "basecoat" : "",
                html: `<body>${comp.toHTML()}</body>`,
                css: ed.getCss({ component: comp, keepUnusedStyles: false }) || "",
            });
            parts.list = [...parts.list, { id: rec.id, title: rec.title, slug: rec.slug, html: rec.html, css: rec.css, kit: rec.kit }];
            const [placeholder] = comp.replaceWith({ type: "pb-part", attributes: { "data-pb-part": rec.slug } });
            applyPartsCSS(ed);
            setPartBlocks(ed, true);
            ctx.onCreated?.();
            placeholder && ed.select(placeholder);
            app.modals.close(modal);
            app.toasts.success(`Saved as the site part "${rec.title}". Drop it on other pages from the Site parts blocks.`);
        } catch (err) {
            handleError(err);
        } finally {
            data.isSaving = false;
        }
    }

    const modal = t.div(
        { className: "modal popup sm" },
        t.header({ className: "modal-header" }, t.h5({ className: "m-auto" }, "Save as a site part")),
        t.form(
            { id: "gpSavePart", className: "modal-content", onsubmit: (e) => (e.preventDefault(), create()) },
            t.p({ className: "txt-hint m-t-0" }, "The selection becomes a part you can place on any page (Site parts blocks). Editing the part later changes every page using it."),
            field({ label: "Name" }, t.input({ type: "text", required: true, autofocus: true, placeholder: "e.g. Header", value: () => data.title, oninput: (e) => (data.title = e.target.value) })),
        ),
        t.footer(
            { className: "modal-footer" },
            t.button({ type: "button", className: "btn transparent secondary m-r-auto", onclick: () => app.modals.close(modal) }, t.span({ className: "txt" }, "Cancel")),
            t.button({ type: "submit", "html-form": "gpSavePart", className: () => `btn expanded ${data.isSaving ? "loading" : ""}`, disabled: () => data.isSaving || !data.title.trim() }, t.span({ className: "txt" }, "Save part")),
        ),
    );
    modal.onafterclose = () => modal.remove();
    document.body.appendChild(modal);
    app.modals.open(modal);
}
