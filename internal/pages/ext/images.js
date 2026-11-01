// Pages builder: the settings of an image (the file, its address, the text
// for screen readers, a hover title, a link around it, how it loads). Its
// look (fit, focus point, ratio, corners, filters) is in the style panel.

const isImage = (comp) => String(comp?.get?.("tagName") || "").toLowerCase() == "img";
/** The link wrapping only this image (made by the "Link" setting, or by hand). */
const linkOf = (comp) => {
    const p = comp.parent();
    return p && String(p.get("tagName")).toLowerCase() == "a" && p.components().length == 1 ? p : null;
};

function el(tag, props = {}, ...children) {
    const e = Object.assign(document.createElement(tag), props);
    e.append(...children);
    return e;
}

export function defineImageTraits(ed) {
    // the hints of the text settings (GrapesJS's own: "eg. Text here")
    ed.I18n.addMessages({
        en: {
            traitManager: {
                traits: {
                    attributes: {
                        alt: { placeholder: "What the image shows (empty: decorative)" },
                        title: { placeholder: "Shown on hover (optional)" },
                    },
                },
            },
        },
    });
    // the file: a preview, the chooser (uploads, the Assets library), its address
    ed.Traits.addType("gpp-image", {
        createInput({ component }) {
            const thumb = el("img", { className: "gpp-image-thumb", alt: "" });
            const choose = el("button", { type: "button", className: "gpp-image-choose" }, el("i", { className: "ri-image-edit-line" }), el("span", { textContent: "Change image" }));
            choose.addEventListener("click", () => openChooser(ed, component));
            const url = el("input", { type: "text", className: "gpp-image-url", placeholder: "https://… or /media/…", spellcheck: false });
            url.addEventListener("change", () => {
                const v = url.value.trim();
                if (v) {
                    component.set({ src: v });
                }
            });
            const note = el("small", { className: "gpp-hint gpp-image-note" });
            const root = el("div", { className: "gpp-image" }, el("div", { className: "gpp-image-row" }, thumb, choose), el("label", { className: "gpp-image-label", textContent: "Address" }), url, note);
            // a new file (chooser, double click, the library): the preview follows
            this.listenTo(component, "change:src change:attributes", () => this.onUpdate({ elInput: root, component }));
            return root;
        },
        onUpdate({ elInput, component }) {
            const a = component.getAttributes();
            const bound = a["data-pb-field"] !== undefined || a["data-pb-attr-src"] !== undefined;
            const src = component.get("src") || a.src || "";
            elInput.querySelector(".gpp-image-thumb").src = src;
            const url = elInput.querySelector(".gpp-image-url");
            if (document.activeElement !== url) {
                url.value = src.startsWith("data:") ? "" : src;
            }
            // filled from the record at each visit
            for (const n of elInput.querySelectorAll(".gpp-image-choose, .gpp-image-url, .gpp-image-label")) {
                n.hidden = bound;
            }
            elInput.querySelector(".gpp-image-note").textContent = bound ? "Filled from the record at each visit (the preview shows a sample)." : "";
        },
    });

    // a link around the image
    ed.Traits.addType("gpp-image-link", {
        createInput({ component }) {
            const href = el("input", { type: "text", className: "gpp-image-href", placeholder: "https://… or /page", spellcheck: false });
            const box = el("input", { type: "checkbox", className: "gpp-image-blank" });
            href.addEventListener("change", () => setLink(ed, component, href.value.trim(), box.checked));
            box.addEventListener("change", () => setLink(ed, component, href.value.trim(), box.checked));
            return el("div", { className: "gpp-image-link" }, el("span", { className: "gpp-image-label", textContent: "Link" }), href, el("label", { className: "gpp-check" }, box, el("span", { textContent: "Open in a new tab" })));
        },
        onUpdate({ elInput, component }) {
            const link = linkOf(component);
            const href = elInput.querySelector(".gpp-image-href");
            if (document.activeElement !== href) {
                href.value = link?.getAttributes().href || "";
            }
            elInput.querySelector(".gpp-image-blank").checked = link?.getAttributes().target == "_blank";
        },
    });

    // how it loads: normal, lazy (below the first screen), first (the main image at the top)
    ed.Traits.addType("gpp-image-loading", {
        createInput({ component }) {
            const s = el("select", { className: "gpp-image-loading" });
            for (const [v, label] of [["", "Normal"], ["lazy", "Lazy (below the first screen)"], ["high", "First (the main image at the top)"]]) {
                s.append(el("option", { value: v, textContent: label }));
            }
            s.addEventListener("change", () => {
                component.removeAttributes(["loading", "fetchpriority"]);
                if (s.value == "lazy") {
                    component.addAttributes({ loading: "lazy" });
                } else if (s.value == "high") {
                    component.addAttributes({ fetchpriority: "high" });
                }
            });
            return s;
        },
        onUpdate({ elInput, component }) {
            const a = component.getAttributes();
            elInput.value = a.loading == "lazy" ? "lazy" : a.fetchpriority == "high" ? "high" : "";
        },
    });
}

/** The image chooser of GrapesJS (uploads, and the Assets library button) for this image. */
function openChooser(ed, comp) {
    const am = ed.AssetManager;
    am.open({
        target: comp,
        types: ["image"],
        accept: "image/*",
        select(asset, complete) {
            comp.set({ src: asset.getSrc() });
            complete && am.close();
        },
    });
}

/** Wraps the image in a link, changes it, or removes it (an empty address). */
function setLink(ed, comp, href, blank) {
    const link = linkOf(comp);
    if (!href) {
        if (link) {
            comp.move(link.parent(), { at: link.index() });
            link.remove();
            ed.select(comp);
        }
        return;
    }
    const attrs = { href, ...(blank ? { target: "_blank", rel: "noopener" } : {}) };
    if (link) {
        link.removeAttributes(["target", "rel"]);
        link.addAttributes(attrs);
        return;
    }
    const parent = comp.parent();
    if (!parent) {
        return;
    }
    const [a] = parent.append({ type: "link", tagName: "a", attributes: attrs, components: [], editable: false }, { at: comp.index() });
    comp.move(a, { at: 0 });
    ed.select(comp);
}

/** The image settings of a selected image (added once). */
export function addImageTraits(comp) {
    if (!isImage(comp) || comp.getTrait("gpp-image")) {
        return;
    }
    comp.addTrait({ type: "gpp-image", name: "gpp-image", label: false, changeProp: true }, { at: 0 });
    if (!comp.getTrait("alt")) {
        comp.addTrait({ name: "alt", label: "Alt text" });
    } else {
        comp.getTrait("alt").set({ label: "Alt text" });
    }
    comp.addTrait([
        { name: "title", label: "Hover text" },
        { type: "gpp-image-link", name: "gpp-image-link", label: false, changeProp: true }, // full width
        { type: "gpp-image-loading", name: "gpp-image-loading", label: "Loading", changeProp: true },
    ]);
}
