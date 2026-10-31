// Pages: the PDF viewer block of the builder. The element is a placeholder
// (data-pb-pdf-* settings): the server writes the viewer, and the canvas
// shows it as the published page does (rendered by /api/pages/{id}/snippet,
// with the viewer script running in the canvas).

import { renderSnippet } from "./widgets.js";

// the source of the PDF, and its own setting
const sources = [
    { id: "file", label: "Uploaded file" },
    { id: "url", label: "Address" },
    { id: "field", label: "File field of the record" },
    { id: "document", label: "Document template" },
];

// the buttons of the toolbar (the thumbnails button comes with the thumbnails)
const tools = [
    ["pages", "Page numbers"],
    ["zoom", "Zoom"],
    ["search", "Search"],
    ["download", "Download"],
    ["print", "Print"],
    ["fullscreen", "Full screen"],
];

const isPDF = (name) => /\.pdf$/i.test(name || "");

/**
 * Defines the component type. ctx: { pageId, viewAs(), collection(comp),
 * pdfAssets(), uploadPDF(files), assetURL(name), documentTemplates() }.
 */
export function definePDFType(ed, ctx) {
    defineFileTrait(ed, ctx);
    defineToolsTrait(ed);
    // a link (or button) downloading a document of the record
    ed.DomComponents.addType("pb-document", {
        extend: "link",
        isComponent: (el) => el?.nodeType == 1 && el.hasAttribute("data-pb-document"),
        model: {
            defaults: {
                name: "Document link",
                icon: '<i class="ri-file-download-line"></i>',
                traits: [
                    { type: "select", name: "data-pb-document", label: "Template", options: [{ id: "", label: "Loading…" }] },
                    {
                        type: "select",
                        name: "data-pb-document-mode",
                        label: "On click",
                        options: [
                            { id: "", label: "Download the PDF" },
                            { id: "open", label: "Open it in a new tab" },
                        ],
                    },
                    "title",
                ],
            },
        },
    });
    ed.DomComponents.addType("pb-pdf", {
        isComponent: (el) => el?.nodeType == 1 && el.hasAttribute("data-pb-pdf"),
        model: {
            defaults: {
                name: "PDF viewer",
                icon: '<i class="ri-file-pdf-2-line"></i>',
                droppable: false,
                components: [],
                traits: [
                    { type: "select", name: "data-pb-pdf", label: "PDF", options: sources },
                    // the setting of the source comes here (refreshPDFTraits)
                    {
                        type: "select",
                        name: "data-pb-pdf-toolbar",
                        label: "Toolbar",
                        options: [
                            { id: "", label: "At the top" },
                            { id: "bottom", label: "At the bottom" },
                            { id: "none", label: "None" },
                        ],
                    },
                    { type: "gpp-pdf-tools", name: "data-pb-pdf-tools", label: "Buttons" },
                    {
                        type: "select",
                        name: "data-pb-pdf-sidebar",
                        label: "Page thumbnails",
                        options: [
                            { id: "", label: "None" },
                            { id: "closed", label: "Hidden (a button shows them)" },
                            { id: "open", label: "Shown" },
                        ],
                    },
                    {
                        type: "select",
                        name: "data-pb-pdf-layout",
                        label: "Pages",
                        options: [
                            { id: "", label: "All, scrolling" },
                            { id: "single", label: "One at a time" },
                        ],
                    },
                    {
                        type: "select",
                        name: "data-pb-pdf-zoom",
                        label: "Zoom",
                        options: [
                            { id: "", label: "Fit the width" },
                            { id: "page", label: "Fit the page" },
                            ...[50, 75, 100, 125, 150, 200].map((v) => ({ id: String(v), label: v + "%" })),
                        ],
                    },
                    { type: "number", name: "data-pb-pdf-page", label: "First page", min: 1, placeholder: "1" },
                    { type: "text", name: "data-pb-pdf-height", label: "Height", placeholder: "640 (px), or auto: all the pages" },
                    {
                        type: "select",
                        name: "data-pb-pdf-background",
                        label: "Behind pages",
                        options: [
                            { id: "", label: "Gray (theme)" },
                            { id: "none", label: "None" },
                            { id: "dark", label: "Dark" },
                        ],
                    },
                    {
                        type: "select",
                        name: "data-pb-pdf-frame",
                        label: "Page edges",
                        options: [
                            { id: "", label: "Shadow" },
                            { id: "border", label: "Border" },
                            { id: "none", label: "None" },
                        ],
                    },
                    {
                        type: "select",
                        name: "data-pb-pdf-text",
                        label: "Text",
                        options: [
                            { id: "", label: "Selectable" },
                            { id: "false", label: "Not selectable" },
                        ],
                    },
                    {
                        type: "select",
                        name: "data-pb-pdf-links",
                        label: "Links",
                        options: [
                            { id: "", label: "Clickable" },
                            { id: "false", label: "Not clickable" },
                        ],
                    },
                    { type: "text", name: "data-pb-pdf-name", label: "Download name", placeholder: "The file's name" },
                    { type: "text", name: "data-pb-pdf-label", label: "Label", placeholder: "PDF document (screen readers)" },
                    { type: "text", name: "data-pb-pdf-empty", label: "When empty", placeholder: "No document to show." },
                ],
            },
            init() {
                this.on("change:attributes", function () {
                    this.view?.render();
                });
                this.on("change:attributes:data-pb-pdf", () => {
                    clearSource(this);
                    refreshPDFTraits(this, ctx);
                });
            },
        },
        view: {
            onRender({ el, model }) {
                renderSnippet(ed, ctx, model, el);
            },
        },
    });
}

// a new source: the settings of the other sources go
function clearSource(comp) {
    const a = comp.getAttributes();
    const kind = a["data-pb-pdf"] || "file";
    const drop = [];
    if (kind != "field") {
        drop.push("data-pb-pdf-field");
    }
    if (kind != "document") {
        drop.push("data-pb-pdf-document");
    }
    const src = a["data-pb-pdf-src"] || "";
    if (kind == "field" || kind == "document" || (kind == "file" && !src.startsWith("/p/_assets/"))) {
        drop.push("data-pb-pdf-src");
    }
    const present = drop.filter((k) => k in a);
    if (present.length) {
        comp.removeAttributes(present);
    }
}

/** The setting of the source of a selected viewer (and its options). */
export async function refreshPDFTraits(comp, ctx) {
    if (comp?.is?.("pb-document")) {
        comp.getTrait("data-pb-document")?.set("options", await templateOptions(comp, ctx));
        return;
    }
    if (!comp?.is?.("pb-pdf")) {
        return;
    }
    const kind = comp.getAttributes()["data-pb-pdf"] || "file";
    const at = comp.getTraits().findIndex((t) => t.get("name") == "data-pb-pdf") + 1;
    let def;
    switch (kind) {
        case "url":
            def = { type: "text", name: "data-pb-pdf-src", label: "Address", placeholder: "https://… or /api/files/…, a formula: {{record.manual_url}}" };
            break;
        case "field": {
            const col = ctx.collection(comp);
            const files = (col?.fields || []).filter((f) => f.type == "file" && !f.hidden);
            def = {
                type: "select",
                name: "data-pb-pdf-field",
                label: "File field",
                options: col
                    ? [{ id: "", label: files.length ? "- Select a field -" : `- ${col.name} has no file field -` }, ...files.map((f) => ({ id: f.name, label: f.name + (f.protected ? " (protected: not shown)" : "") }))]
                    : [{ id: "", label: "- On a single item page or in a list -" }],
            };
            break;
        }
        case "document":
            def = { type: "select", name: "data-pb-pdf-document", label: "Template", options: [{ id: "", label: "Loading…" }] };
            break;
        default:
            def = { type: "gpp-pdf-file", name: "data-pb-pdf-src", label: "File" };
    }
    // the trait of the source: the one of this kind only
    for (const name of ["data-pb-pdf-src", "data-pb-pdf-field", "data-pb-pdf-document"]) {
        const t = comp.getTrait(name);
        if (t && (name != def.name || t.get("type") != def.type)) {
            comp.removeTrait(name);
        }
    }
    if (!comp.getTrait(def.name)) {
        comp.addTrait(def, { at });
    } else if (def.options) {
        comp.getTrait(def.name).set("options", def.options);
    }
    if (kind == "document") {
        comp.getTrait("data-pb-pdf-document")?.set("options", await templateOptions(comp, ctx));
    }
}

// the Documents templates, the ones about other records flagged
async function templateOptions(comp, ctx) {
    const list = await ctx.documentTemplates();
    const col = ctx.collection(comp);
    return list.length
        ? [
              { id: "", label: "- Select a template -" },
              ...list.map((t) => ({
                  id: t.id,
                  label: t.name + (t.collection ? (col && t.collection != col.name ? ` (about ${t.collection}: not this page's records)` : ` (${t.collection})`) : ""),
              })),
          ]
        : [{ id: "", label: "- No templates: make one in Documents -" }];
}

// "File": the PDFs uploaded to the page, and an upload button
function defineFileTrait(ed, ctx) {
    ed.Traits.addType("gpp-pdf-file", {
        createInput({ component }) {
            const el = document.createElement("div");
            el.className = "gpp-pdf-file";
            el.innerHTML =
                `<select aria-label="Uploaded PDF"></select>` +
                `<label class="gpp-pdf-upload"><input type="file" accept="application/pdf,.pdf" hidden><i class="ri-upload-2-line" aria-hidden="true"></i><span>Upload a PDF</span></label>` +
                `<p class="gpp-pdf-hint" hidden></p>`;
            const sel = el.querySelector("select");
            const hint = el.querySelector(".gpp-pdf-hint");
            const set = (name) => (name ? component.addAttributes({ "data-pb-pdf-src": ctx.assetURL(name) }) : component.removeAttributes("data-pb-pdf-src"));
            sel.addEventListener("change", () => set(sel.value));
            el.querySelector("input").addEventListener("change", async (e) => {
                const files = [...e.target.files].filter((f) => f.type == "application/pdf" || isPDF(f.name));
                e.target.value = "";
                if (!files.length) {
                    return;
                }
                hint.hidden = false;
                hint.textContent = "Uploading…";
                const added = await ctx.uploadPDF(files);
                hint.hidden = true;
                if (added[0]) {
                    set(added[0]);
                }
                this.onUpdate({ elInput: el, component });
            });
            return el;
        },
        onUpdate({ elInput, component }) {
            const src = component.getAttributes()["data-pb-pdf-src"] || "";
            const names = ctx.pdfAssets();
            const sel = elInput.querySelector("select");
            const current = names.find((n) => ctx.assetURL(n) == src) || "";
            sel.innerHTML = "";
            const add = (value, label) => {
                const o = document.createElement("option");
                o.value = value;
                o.textContent = label;
                sel.append(o);
            };
            add("", names.length ? "- Select a PDF -" : "- No PDF uploaded yet -");
            names.forEach((n) => add(n, n.replace(/_[a-z0-9]{10}(\.pdf)$/i, "$1")));
            sel.value = current;
        },
    });
}

// "Buttons": a check box for each button of the toolbar (data-pb-pdf-tools;
// none set: all of them)
function defineToolsTrait(ed) {
    ed.Traits.addType("gpp-pdf-tools", {
        createInput({ component }) {
            const el = document.createElement("div");
            el.className = "gpp-pdf-tools";
            el.innerHTML = tools.map(([id, label]) => `<label><input type="checkbox" value="${id}"><span>${label}</span></label>`).join("");
            el.addEventListener("change", () => {
                const on = [...el.querySelectorAll("input:checked")].map((i) => i.value);
                if (on.length == tools.length) {
                    component.removeAttributes("data-pb-pdf-tools");
                } else {
                    // the thumbnails button stays (it shows with the thumbnails only)
                    component.addAttributes({ "data-pb-pdf-tools": ["sidebar", ...on].join(",") });
                }
            });
            return el;
        },
        onUpdate({ elInput, component }) {
            const a = component.getAttributes();
            const set = "data-pb-pdf-tools" in a ? String(a["data-pb-pdf-tools"]).split(",") : tools.map(([id]) => id);
            elInput.querySelectorAll("input").forEach((i) => (i.checked = set.includes(i.value)));
            elInput.classList.toggle("is-off", a["data-pb-pdf-toolbar"] == "none");
        },
    });
}

export function setPDFBlocks(ed, kit) {
    const bm = ed.BlockManager;
    const media = (name) => `<i class="${name} gpp-block-icon"></i>`;
    bm.add("gpp-pdf", { category: "Basic", label: "PDF viewer", media: media("ri-file-pdf-2-line"), content: `<div data-pb-pdf="file" data-pb-pdf-sidebar="closed"></div>` });
    bm.add("gpp-pdf-field", { category: "Data", label: "Field PDF", media: media("ri-file-pdf-line"), content: `<div data-pb-pdf="field" data-pb-pdf-sidebar="closed"></div>` });
    bm.add("gpp-pdf-document", { category: "Data", label: "Document (PDF)", media: media("ri-file-paper-2-line"), content: `<div data-pb-pdf="document" data-pb-pdf-tools="sidebar,pages,zoom,download,print,fullscreen"></div>` });
    // the button follows the kit of the page (setPDFBlocks again when it changes)
    const btn = kit
        ? `class="btn"`
        : `style="display:inline-flex;align-items:center;gap:8px;padding:10px 20px;background:#2563eb;color:#fff;border-radius:6px;text-decoration:none;font-weight:600"`;
    bm.remove("gpp-document-link");
    bm.add("gpp-document-link", { category: "Data", label: "Download PDF", media: media("ri-file-download-line"), content: `<a data-pb-document="" href="#" ${btn}>Download PDF</a>` });
}
