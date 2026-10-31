// The editor of a document template: the outline of the blocks (header,
// body, footer), the settings of the selected block (formulas with a data
// and function picker), the page and data settings, and the live PDF preview
// rendered by the server.

import { allCollections, api, debounce, field, handleError, select, userCollections } from "../pocketbaseplus/lib.js";
import { functionList, functionSnippet, insertAt, actionAt } from "../pocketbaseplus/functions.js";
import { formatLabels } from "./documents.js";

const blockTypes = {
    heading: { label: "Heading", icon: "ri-heading", hint: "A title" },
    text: { label: "Text", icon: "ri-text", hint: "A paragraph: **bold**, *italic*, [links](https://…)" },
    fields: { label: "Fields", icon: "ri-list-unordered", hint: "Label and value rows: Invoice no., Date…" },
    columns: { label: "Columns", icon: "ri-layout-column-line", hint: "2 to 4 columns side by side" },
    table: { label: "Table", icon: "ri-table-line", hint: "One row per item of a list" },
    image: { label: "Image", icon: "ri-image-line", hint: "A logo, a signature, a photo of the record" },
    divider: { label: "Divider", icon: "ri-separator", hint: "A line" },
    spacer: { label: "Space", icon: "ri-space", hint: "Empty space" },
    page_break: { label: "Page break", icon: "ri-page-separator", hint: "Next blocks on a new page" },
};

const parts = [
    ["header", "Header", "On top of every page"],
    ["body", "Body", "The content"],
    ["footer", "Footer", "At the bottom of every page: {{page}} / {{pages}}"],
];

const ZOOM_KEY = "gpDocZoom";

// PDF.js, shared with the PDF viewers of the pages (internal/pdfview: the
// same version)
const PDFJS = "/api/documents/pdfjs/4.10.38/";

const randomId = () => Math.random().toString(36).slice(2, 10);

const minWidth = 5; // percent

// fitWidths scales the widths (percent) of columns or table cells so they add
// up to 100; the ones without a width get an even share
function fitWidths(list) {
    if (!list.length) {
        return;
    }
    const set = list.filter((c) => Number(c.width) > 0);
    const even = set.length ? set.reduce((a, c) => a + Number(c.width), 0) / set.length : 100 / list.length;
    const cur = list.map((c) => (Number(c.width) > 0 ? Number(c.width) : even));
    const sum = cur.reduce((a, x) => a + x, 0);
    roundTo100(list, cur.map((x) => (x / sum) * 100), -1);
}

// balanceWidths keeps the width just typed in list[i] and shares the rest of
// the 100% among the others, by their current widths
function balanceWidths(list, i) {
    const n = list.length;
    if (n < 2) {
        list.forEach((c) => (c.width = 100));
        return;
    }
    const v = Math.min(Math.max(Math.round(Number(list[i].width) || 0), minWidth), 100 - minWidth * (n - 1));
    const others = list.map((c, j) => (j == i ? 0 : Math.max(Number(list[j].width) || 0, 0)));
    const sum = others.reduce((a, x) => a + x, 0);
    const shares = others.map((x, j) => (j == i ? v : sum > 0 ? (x / sum) * (100 - v) : (100 - v) / (n - 1)));
    roundTo100(list, shares, i);
}

// roundTo100 sets whole widths of at least minWidth that add up to 100,
// leaving list[keep] as it is
function roundTo100(list, shares, keep) {
    const w = shares.map((x) => Math.max(minWidth, Math.round(x)));
    let diff = 100 - w.reduce((a, x) => a + x, 0);
    while (diff != 0) {
        // the widest (or, to grow, the narrowest) of the others takes the difference
        let j = -1;
        w.forEach((x, k) => {
            if (k != keep && (diff > 0 ? x > minWidth - 1 : x > minWidth) && (j < 0 || (diff < 0 ? x > w[j] : x < w[j]))) {
                j = k;
            }
        });
        if (j < 0) {
            break;
        }
        const step = diff > 0 ? diff : Math.max(diff, minWidth - w[j]);
        w[j] += step;
        diff -= step;
    }
    list.forEach((c, k) => (c.width = w[k]));
}

const widthsTotal = (list) => list.reduce((a, c) => a + (Number(c.width) || 0), 0);

function newBlock(type, lists) {
    const b = { id: randomId(), type };
    switch (type) {
        case "heading":
            return { ...b, level: 2, text: "Title" };
        case "text":
            return { ...b, text: "Text" };
        case "fields":
            return { ...b, rows: [{ label: "Reference", value: "{{record.id}}" }] };
        case "columns":
            return { ...b, columns: [{ width: 50, blocks: [] }, { width: 50, blocks: [] }] };
        case "table":
            return {
                ...b,
                items: lists[0] ? `{{lists.${lists[0].name}}}` : "",
                zebra: true,
                cells: [
                    { header: "Item", value: "{{row}}", width: 70, align: "" },
                    { header: "Amount", value: "", width: 30, align: "right" },
                ],
            };
        case "image":
            return { ...b, width: 40 };
        case "spacer":
            return { ...b, height: 6 };
    }
    return b;
}

export function pageEditor(route) {
    const id = route.params.id;
    app.store.title = "Document template";

    const data = store({
        isLoading: true,
        isSaving: false,
        tpl: null, // the record (assets, name...)
        form: null, // the edited fields
        original: "",
        selected: "", // block id
        hover: "", // block id under the pointer (outline or pages)
        dragging: false,
        tab: "blocks", // blocks | page | data
        zoom: app.utils.getLocalHistory(ZOOM_KEY, "fit"), // "fit" or a scale
        collections: [],
        all: [], // every collection (schemas)
        samples: [], // records of the collection
        preview: { raw: "", pages: 0, errors: [], error: "", loading: false, filename: "", boxes: [], pageWidth: 210, pageHeight: 297 },
        mobileView: "edit",
    });

    // --- loading and saving ------------------------------------------------------------------

    const fields = ["name", "slug", "folder", "format", "orientation", "width_mm", "height_mm", "margins", "font_size", "accent_color", "collection", "sample_record", "lists", "header", "body", "footer", "filename"];

    async function load() {
        try {
            const [rec, cols, all] = await Promise.all([
                app.pb.collection("document_templates").getOne(id, { requestKey: "gpDocTemplate" }),
                userCollections(),
                allCollections(),
            ]);
            data.collections = cols.map((c) => c.name);
            data.all = all;
            data.tpl = rec;
            const form = {};
            for (const f of fields) {
                form[f] = rec[f];
            }
            form.margins = { top: 18, right: 18, bottom: 18, left: 18, ...(rec.margins || {}) };
            for (const p of ["header", "body", "footer", "lists"]) {
                form[p] = Array.isArray(rec[p]) ? rec[p] : [];
            }
            form.font_size ||= 10;
            form.accent_color ||= "#2563eb";
            form.format ||= "a4";
            form.orientation ||= "portrait";
            data.form = form;
            data.original = JSON.stringify(form);
            app.store.title = rec.name;
            loadSamples();
        } catch (err) {
            handleError(err);
        }
        data.isLoading = false;
    }

    async function loadSamples() {
        data.samples = [];
        const col = data.form?.collection;
        if (!col) {
            return;
        }
        try {
            const res = await app.pb.collection(col).getList(1, 30, { sort: "-@rowid", requestKey: "gpDocSamples" });
            data.samples = res.items;
        } catch {
            try {
                data.samples = (await app.pb.collection(col).getList(1, 30, { requestKey: "gpDocSamples2" })).items;
            } catch {}
        }
    }

    const dirty = () => data.form && JSON.stringify(data.form) != data.original;

    async function save() {
        if (data.isSaving) {
            return;
        }
        data.isSaving = true;
        try {
            const rec = await app.pb.collection("document_templates").update(id, data.form);
            // the blocks are kept as they are: the open settings and the
            // outline hold them, new copies would leave them editing stale
            // ones. Only blocks without an id (the server gives them one)
            // are taken back.
            const noId = (list) => (list || []).some((b) => !b.id || (b.columns || []).some((c) => noId(c.blocks)));
            for (const p of ["header", "body", "footer"]) {
                if (noId(data.form[p])) {
                    data.form[p] = rec[p] || [];
                }
            }
            data.tpl = rec;
            data.original = JSON.stringify(data.form);
            app.store.title = rec.name;
            app.toasts.success("Saved the template.");
        } catch (err) {
            handleError(err);
        }
        data.isSaving = false;
    }

    async function generate() {
        if (dirty()) {
            await save();
        }
        try {
            const doc = await api(`/api/documents/${id}/generate`, { method: "POST", body: { recordId: data.form.sample_record || data.preview.recordId || "" } });
            app.toasts.success(`Made ${doc.filename}: find it in Generated.`);
        } catch (err) {
            handleError(err);
        }
    }

    // --- preview ---------------------------------------------------------------------------------

    function specOf() {
        const f = data.form;
        return {
            format: f.format,
            orientation: f.orientation,
            width_mm: Number(f.width_mm) || 0,
            height_mm: Number(f.height_mm) || 0,
            margins: f.margins,
            font_size: Number(f.font_size) || 10,
            accent_color: f.accent_color,
            header: f.header,
            body: f.body,
            footer: f.footer,
            filename: f.filename,
            lists: f.lists,
        };
    }

    const refresh = debounce(async () => {
        if (!data.form) {
            return;
        }
        data.preview.loading = true;
        try {
            const res = await api("/api/documents/preview", {
                method: "POST",
                body: { id, name: data.form.slug, spec: specOf(), record: data.form.collection, recordId: data.form.sample_record },
                requestKey: "gpDocPreview",
            });
            if (res.error) {
                data.preview.error = res.error;
            } else {
                data.preview = {
                    raw: app.pb.buildURL(res.url),
                    pages: res.pages,
                    errors: res.errors || [],
                    error: "",
                    loading: false,
                    filename: res.filename,
                    recordId: res.recordId,
                    boxes: res.boxes || [],
                    pageWidth: res.pageWidth || 210,
                    pageHeight: res.pageHeight || 297,
                };
            }
        } catch (err) {
            if (!err.isAbort) {
                data.preview.error = err.message;
            }
        }
        data.preview.loading = false;
    }, 600);

    // --- blocks ------------------------------------------------------------------------------------

    // where a block is: its list (owner[prop]) and position
    function locate(blockId) {
        let found = null;
        const walk = (owner, prop) => {
            (owner[prop] || []).forEach((b, i) => {
                if (found) {
                    return;
                }
                if (b.id == blockId) {
                    found = { owner, prop, index: i, block: b };
                    return;
                }
                (b.columns || []).forEach((c) => walk(c, "blocks"));
            });
        };
        for (const [p] of parts) {
            walk(data.form, p);
        }
        return found;
    }

    const selectedBlock = () => (data.selected ? locate(data.selected)?.block : null);

    function select_(blockId) {
        data.selected = blockId;
        data.tab = "blocks";
        data.mobileView = "edit";
    }

    // the list a new block goes to: after the selected one, or at the end of the body
    function addBlock(type) {
        const sel = data.selected && locate(data.selected);
        place({ type }, sel ? { id: sel.block.id, where: "after" } : { owner: data.form, prop: "body" });
    }

    const inColumn = (owner) => owner != data.form;

    // place moves a block ({id}) or adds a new one ({type}) before or after a
    // block ({id, where}), or into a list ({owner, prop, index}: a part or a column).
    function place(src, target) {
        let into = target.owner;
        if (target.id) {
            const t = locate(target.id);
            if (!t) {
                return;
            }
            into = t.owner;
        }
        const moving = src.id ? locate(src.id) : null;
        if (src.id && (!moving || src.id == target.id)) {
            return;
        }
        const type = moving ? moving.block.type : src.type;
        if (type == "columns" && inColumn(into)) {
            app.toasts.info("Columns can't go inside columns.");
            return;
        }
        // not into itself
        if (moving?.block.columns?.some((c) => c === into || JSON.stringify(c).includes(`"id":"${target.id}"`))) {
            return;
        }
        let block;
        if (moving) {
            block = moving.block;
            moving.owner[moving.prop] = moving.owner[moving.prop].filter((b) => b.id != src.id);
        } else {
            block = newBlock(src.type, data.form.lists);
        }
        if (target.id) {
            const t = locate(target.id);
            const list = [...t.owner[t.prop]];
            list.splice(t.index + (target.where == "after" ? 1 : 0), 0, block);
            t.owner[t.prop] = list;
        } else {
            const list = [...(target.owner[target.prop] || [])];
            list.splice(target.index ?? list.length, 0, block);
            target.owner[target.prop] = list;
        }
        select_(block.id);
    }

    function move(blockId, delta) {
        const at = locate(blockId);
        if (!at) {
            return;
        }
        const list = [...at.owner[at.prop]];
        const to = at.index + delta;
        if (to < 0 || to >= list.length) {
            return;
        }
        [list[at.index], list[to]] = [list[to], list[at.index]];
        at.owner[at.prop] = list;
    }

    function duplicate(blockId) {
        const at = locate(blockId);
        if (!at) {
            return;
        }
        const copy = JSON.parse(JSON.stringify(at.block));
        const renew = (b) => {
            b.id = randomId();
            (b.columns || []).forEach((c) => c.blocks.forEach(renew));
        };
        renew(copy);
        const list = [...at.owner[at.prop]];
        list.splice(at.index + 1, 0, copy);
        at.owner[at.prop] = list;
        data.selected = copy.id;
    }

    function removeBlock(blockId) {
        const at = locate(blockId);
        if (!at) {
            return;
        }
        at.owner[at.prop] = at.owner[at.prop].filter((b) => b.id != blockId);
        if (data.selected == blockId) {
            data.selected = "";
        }
    }

    const errorsOf = (blockId) => data.preview.errors.filter((e) => e.block == blockId);

    function blockLabel(b) {
        const def = blockTypes[b.type] || { label: b.type };
        const text = (b.text || "").replace(/\s+/g, " ").trim();
        if ((b.type == "heading" || b.type == "text") && text) {
            return text.length > 40 ? text.slice(0, 40) + "…" : text;
        }
        if (b.type == "table" && b.items) {
            return "Table · " + b.items.replace(/[{}]/g, "").trim();
        }
        if (b.type == "image") {
            return "Image · " + (b.asset || (b.field ? "record." + b.field : b.url ? b.url : "none"));
        }
        if (b.type == "columns") {
            return `${(b.columns || []).length} columns`;
        }
        return def.label;
    }

    // --- dragging: the outline rows, the blocks of the preview and the palette ----------------

    let dragged = null; // {id} or {type}

    function dragStart(e, src) {
        dragged = src;
        e.dataTransfer.effectAllowed = src.type ? "copy" : "move";
        e.dataTransfer.setData("text/plain", src.id || src.type);
        data.dragging = true;
    }

    function dragEnd() {
        dragged = null;
        data.dragging = false;
        clearDropMarks();
    }

    function clearDropMarks() {
        document.querySelectorAll(".gp-drop-before, .gp-drop-after, .gp-drop-into").forEach((el) => el.classList.remove("gp-drop-before", "gp-drop-after", "gp-drop-into"));
    }

    // a block as a drop target: before or after it, by the half the pointer is on
    function dropOnBlock(blockId) {
        const where = (e) => {
            const r = e.currentTarget.getBoundingClientRect();
            return e.clientY < r.top + r.height / 2 ? "before" : "after";
        };
        return {
            ondragover: (e) => {
                if (!dragged || dragged.id == blockId) {
                    return;
                }
                e.preventDefault();
                e.stopPropagation();
                clearDropMarks();
                e.currentTarget.classList.add("gp-drop-" + where(e));
            },
            ondragleave: (e) => e.currentTarget.classList.remove("gp-drop-before", "gp-drop-after"),
            ondrop: (e) => {
                if (!dragged) {
                    return;
                }
                e.preventDefault();
                e.stopPropagation();
                const src = dragged;
                const w = where(e);
                dragEnd();
                place(src, { id: blockId, where: w });
            },
        };
    }

    // a list as a drop target (a part or a column), at index
    function dropInto(owner, prop, index = 0) {
        return {
            ondragover: (e) => {
                if (!dragged) {
                    return;
                }
                e.preventDefault();
                e.stopPropagation();
                clearDropMarks();
                e.currentTarget.classList.add("gp-drop-into");
            },
            ondragleave: (e) => e.currentTarget.classList.remove("gp-drop-into"),
            ondrop: (e) => {
                if (!dragged) {
                    return;
                }
                e.preventDefault();
                e.stopPropagation();
                const src = dragged;
                dragEnd();
                place(src, { owner, prop, index });
            },
        };
    }

    // --- the outline ---------------------------------------------------------------------------

    function palette() {
        return t.div(
            { className: "gp-doc-palette" },
            Object.entries(blockTypes).map(([type, def]) =>
                t.button(
                    {
                        type: "button",
                        className: "gp-doc-palette-item",
                        draggable: true,
                        ondragstart: (e) => dragStart(e, { type }),
                        ondragend: dragEnd,
                        onclick: () => addBlock(type),
                        ariaDescription: app.attrs.tooltip(def.hint + ". Click to add it after the selected block, or drag it to its place."),
                    },
                    t.i({ className: def.icon, ariaHidden: true }),
                    t.span({ className: "txt" }, def.label),
                )
            ),
        );
    }

    let menuCount = 0;
    function addMenu(owner, prop, label = "Add") {
        const menuId = "gpDocAdd" + ++menuCount;
        const close = (e) => e.currentTarget.closest(".dropdown")?.hidePopover();
        return t.div(
            { className: "gp-doc-add" },
            t.button(
                { type: "button", className: "btn xs secondary transparent", "html-popovertarget": menuId },
                t.i({ className: "ri-add-line", ariaHidden: true }),
                t.span({ className: "txt" }, label),
            ),
            t.div(
                { id: menuId, popover: "auto", className: "dropdown gp-doc-add-menu" },
                Object.entries(blockTypes)
                    .filter(([type]) => !(inColumn(owner) && type == "columns"))
                    .map(([type, def]) =>
                        t.button(
                            { type: "button", className: "dropdown-item", onclick: (e) => (close(e), place({ type }, { owner, prop })) },
                            t.i({ className: def.icon, ariaHidden: true }),
                            t.span({ className: "txt" }, t.strong(null, def.label), t.small({ className: "txt-hint" }, def.hint)),
                        )
                    ),
            ),
        );
    }

    function outlineRow(b, depth) {
        const def = blockTypes[b.type] || { icon: "ri-question-line" };
        const rows = [
            t.div(
                {
                    rid: b.id,
                    className: () => `gp-doc-node ${data.selected == b.id ? "active" : ""} ${data.hover == b.id ? "hover" : ""} ${errorsOf(b.id).length ? "has-error" : ""}`,
                    style: `--depth:${depth}`,
                    draggable: true,
                    ondragstart: (e) => dragStart(e, { id: b.id }),
                    ondragend: dragEnd,
                    onmouseenter: () => (data.hover = b.id),
                    onmouseleave: () => data.hover == b.id && (data.hover = ""),
                    ...dropOnBlock(b.id),
                },
                t.i({ className: "ri-draggable gp-doc-grip", ariaHidden: true }),
                t.button(
                    { type: "button", className: "gp-doc-node-main", onclick: () => select_(b.id) },
                    t.i({ className: def.icon, ariaHidden: true }),
                    t.span({ className: "txt-ellipsis" }, () => blockLabel(b)),
                    b.show_if ? t.i({ className: "ri-eye-off-line txt-hint", title: "Shown when a condition is met" }) : null,
                    () => (errorsOf(b.id).length ? t.i({ className: "ri-error-warning-fill gp-doc-err-icon", title: errorsOf(b.id)[0].error }) : null),
                ),
            ),
        ];
        if (b.type == "columns") {
            (b.columns || []).forEach((c, i) => {
                rows.push(t.div({ rid: b.id + "c" + i, className: "gp-doc-col-label", style: `--depth:${depth + 1}`, ...dropInto(c, "blocks", 0) }, `Column ${i + 1}`));
                (c.blocks || []).forEach((cb) => rows.push(...outlineRow(cb, depth + 2)));
                rows.push(t.div({ rid: b.id + "a" + i + (c.blocks || []).length, style: `--depth:${depth + 2}`, className: "gp-doc-indent", ...dropInto(c, "blocks", (c.blocks || []).length) }, addMenu(c, "blocks", "Add to column " + (i + 1))));
            });
        }
        return rows;
    }

    function outline() {
        return t.div(
            { className: "gp-doc-outline" },
            t.div({ className: "gp-doc-panel-title" }, "Add a block"),
            palette(),
            parts.map(([p, label, hint]) =>
                t.div(
                    { className: "gp-doc-part" },
                    t.div({ className: "gp-doc-part-head", ...dropInto(data.form, p, 0) }, t.span({ className: "txt-bold" }, label), t.small({ className: "txt-hint" }, hint)),
                    () => {
                        const list = data.form[p] || [];
                        return t.div(
                            { rid: p + JSON.stringify(list.map((b) => [b.id, (b.columns || []).map((c) => (c.blocks || []).map((x) => x.id))])) },
                            list.length
                                ? list.flatMap((b) => outlineRow(b, 0))
                                : t.div({ className: "txt-hint txt-sm gp-doc-empty", ...dropInto(data.form, p, 0) }, p == "body" ? "No blocks yet: add one above." : "Nothing: drop a block here."),
                        );
                    },
                    t.div({ ...dropInto(data.form, p, Infinity) }, addMenu(data.form, p)),
                )
            ),
        );
    }

    // --- formula inputs with the data picker ------------------------------------------------------

    // the data a formula can use: the record, the lists, the values, the page; in a table, the row
    function dataGroups(inTable) {
        const groups = [];
        const col = data.all.find((c) => c.name == data.form.collection);
        if (col) {
            const items = [{ path: "record", label: "the record (its name)" }];
            for (const f of col.fields) {
                if (f.hidden || f.type == "password" || f.name == "tokenKey") {
                    continue;
                }
                items.push({ path: "record." + f.name, label: f.type });
                if (f.type == "relation") {
                    const rel = data.all.find((c) => c.id == f.collectionId);
                    for (const rf of rel?.fields || []) {
                        if (!rf.hidden && rf.type != "password" && rf.name != "id" && rf.name != "tokenKey") {
                            items.push({ path: `record.${f.name}.${rf.name}`, label: `${rel.name} · ${rf.type}` });
                        }
                    }
                }
            }
            groups.push({ name: `Record (${col.name})`, items });
        }
        if (inTable) {
            const listName = /lists\.([A-Za-z0-9_]+)/.exec(inTable.items || "")?.[1];
            const list = data.form.lists.find((l) => l.name == listName);
            const lcol = list && data.all.find((c) => c.name == list.collection);
            const items = [{ path: "row", label: "the item (its name)" }, { path: "index", label: "position (1, 2, …)" }];
            for (const f of lcol?.fields || []) {
                if (!f.hidden && f.type != "password" && f.name != "tokenKey") {
                    items.push({ path: "row." + f.name, label: f.type });
                }
            }
            groups.push({ name: "Row of the table", items });
        }
        if (data.form.lists.length) {
            groups.push({ name: "Lists", items: data.form.lists.map((l) => ({ path: "lists." + l.name, label: `${l.collection} (list)` })) });
        }
        groups.push({
            name: "Document",
            items: [
                { path: "values.name", label: "a value given by the flow step" },
                { path: "app.name", label: "application name" },
                { path: "app.url", label: "application URL" },
                { path: "page", label: "page number (header, footer)" },
                { path: "pages", label: "page count (header, footer)" },
                { path: "document.name", label: "template slug" },
            ],
        });
        return groups;
    }

    function formulaInput(obj, key, { multiline = false, rows = 3, placeholder = "", mono = false, table = null, id = "" } = {}) {
        const state = store({ open: false, tab: "data", search: "", category: "" });
        const attrs = {
            id,
            className: mono ? "txt-code" : "",
            placeholder,
            value: () => obj[key] ?? "",
            oninput: (e) => (obj[key] = e.target.value),
        };
        const input = multiline ? t.textarea({ ...attrs, rows }) : t.input({ ...attrs, type: "text" });
        function insertPath(path) {
            const inAction = actionAt(input.value, input.selectionStart ?? input.value.length) != null;
            insertAt(input, inAction ? path : `{{${path}}}`);
            state.open = false;
        }
        return t.div(
            { className: () => `gp-doc-formula ${multiline ? "multiline" : ""} ${state.open ? "open" : ""}` },
            input,
            t.button(
                {
                    type: "button",
                    className: "btn xs circle transparent secondary gp-doc-formula-btn",
                    ariaLabel: app.attrs.tooltip("Insert data or a function"),
                    ariaExpanded: () => state.open,
                    onmousedown: (e) => e.preventDefault(),
                    onclick: () => (state.open = !state.open),
                },
                t.i({ className: "ri-braces-line", ariaHidden: true }),
            ),
            () =>
                state.open
                    ? t.div(
                        {
                            rid: "picker",
                            className: "gp-doc-picker",
                            // fixed on the screen: the side panel would clip it
                            onmount: (el) => {
                                const place = () => placePicker(el, input);
                                place();
                                el._place = place;
                                window.addEventListener("scroll", place, true);
                                window.addEventListener("resize", place);
                            },
                            onunmount: (el) => {
                                window.removeEventListener("scroll", el._place, true);
                                window.removeEventListener("resize", el._place);
                            },
                        },
                        t.div(
                            { className: "gp-doc-picker-tabs" },
                            ["data", "functions"].map((k) =>
                                t.button(
                                    { type: "button", className: () => `btn xs ${state.tab == k ? "" : "secondary transparent"}`, onmousedown: (e) => e.preventDefault(), onclick: () => (state.tab = k) },
                                    k == "data" ? "Data" : "Functions",
                                )
                            ),
                            t.input({ type: "search", className: "gp-doc-picker-search", placeholder: "Search…", value: () => state.search, oninput: (e) => (state.search = e.target.value) }),
                        ),
                        () =>
                            state.tab == "functions"
                                ? t.div(
                                    { rid: "fns", className: "gp-doc-picker-list" },
                                    functionList(state, (f) => {
                                        const { text, sel } = functionSnippet(f, input.value, input.selectionStart ?? input.value.length);
                                        insertAt(input, text, sel);
                                        state.open = false;
                                    }),
                                )
                                : t.div(
                                    { rid: "data" + state.search, className: "gp-doc-picker-list" },
                                    dataGroups(table).map((g) => {
                                        const q = state.search.toLowerCase();
                                        const items = g.items.filter((it) => !q || it.path.toLowerCase().includes(q) || it.label.toLowerCase().includes(q));
                                        return items.length
                                            ? t.div(
                                                null,
                                                t.div({ className: "gp-doc-picker-group" }, g.name),
                                                items.map((it) =>
                                                    t.button(
                                                        { type: "button", className: "gp-doc-picker-item", onmousedown: (e) => e.preventDefault(), onclick: () => insertPath(it.path) },
                                                        t.code(null, it.path),
                                                        t.small({ className: "txt-hint" }, it.label),
                                                    )
                                                ),
                                            )
                                            : null;
                                    }),
                                ),
                    )
                    : null,
        );
    }

    // placePicker puts the picker under its input (above it when there is no
    // room below), aligned on its left edge, inside the window
    function placePicker(el, input) {
        const r = input.getBoundingClientRect();
        const w = Math.min(380, window.innerWidth - 16);
        el.style.width = w + "px";
        el.style.left = Math.max(8, Math.min(r.left, window.innerWidth - w - 8)) + "px";
        const below = window.innerHeight - r.bottom - 12;
        if (below >= Math.min(340, r.top - 12) || below >= 240) {
            el.style.top = r.bottom + 4 + "px";
            el.style.bottom = "";
            el.style.maxHeight = Math.min(340, below) + "px";
        } else {
            el.style.top = "";
            el.style.bottom = window.innerHeight - r.top + 4 + "px";
            el.style.maxHeight = Math.min(340, r.top - 12) + "px";
        }
    }

    function segmented(obj, key, options, name) {
        return t.div(
            { className: "gp-segmented" },
            options.map(([value, icon, label]) =>
                t.label(
                    { className: () => `gp-segment ${(obj[key] || "") == value ? "active" : ""}`, title: label },
                    t.input({ type: "radio", name, checked: () => (obj[key] || "") == value, onchange: () => (obj[key] = value) }),
                    icon ? t.i({ className: icon, ariaHidden: true }) : null,
                    t.span({ className: icon ? "txt gp-doc-seg-label" : "txt" }, label),
                )
            ),
        );
    }

    // a label above a control that isn't an input (the field styles would draw it as a checkbox)
    const labelled = (label, el) => t.div({ className: "gp-doc-labelled" }, t.div({ className: "gp-label" }, label), el);

    const aligns = [["", "ri-align-left", "Left"], ["center", "ri-align-center", "Center"], ["right", "ri-align-right", "Right"]];

    // the help under widths in %, with a fix when they don't add up to 100 (older templates)
    function widthsHelp(list, help) {
        return () => {
            const l = list() || [];
            const total = widthsTotal(l);
            if (!l.length || total == 100) {
                return t.div({ rid: "wh", className: "field-help" }, help);
            }
            return t.div(
                { rid: "wh" + total, className: "field-help gp-doc-widths-off" },
                t.span({ className: "txt-danger" }, `They add up to ${total}%: the page scales them to fit. `),
                t.button({ type: "button", className: "link-primary txt-bold", onclick: () => fitWidths(l) }, "Make them 100%"),
            );
        };
    }

    function numberInput(obj, key, { min = 0, max = 999, step = 1, id = "", placeholder = "", onchange = null } = {}) {
        return t.input({
            id,
            type: "number",
            min,
            max,
            step,
            placeholder,
            value: () => obj[key] ?? "",
            oninput: (e) => (obj[key] = e.target.value === "" ? undefined : Number(e.target.value)),
            onchange: onchange ? (e) => (onchange(), (e.target.value = obj[key] ?? "")) : null,
        });
    }

    function colorInput(obj, key, fallback) {
        return t.div(
            { className: "gp-doc-color" },
            t.input({ type: "color", ariaLabel: "Color", value: () => obj[key] || fallback, oninput: (e) => (obj[key] = e.target.value) }),
            t.input({ type: "text", className: "txt-code", maxLength: 7, placeholder: fallback, value: () => obj[key] || "", oninput: (e) => (obj[key] = e.target.value) }),
            t.button({ type: "button", className: "btn xs transparent secondary", hidden: () => !obj[key], onclick: () => (obj[key] = "") }, "Default"),
        );
    }

    // rows of label/value formulas (fields, totals)
    function rowsEditor(b, prop, addLabel) {
        return t.div(
            { className: "gp-doc-rows" },
            () =>
                (b[prop] || []).map((r, i) =>
                    t.div(
                        { rid: prop + i + (b[prop] || []).length, className: "gp-doc-row" },
                        formulaInput(r, "label", { placeholder: "Label" }),
                        formulaInput(r, "value", { placeholder: "{{record.number}}" }),
                        t.button(
                            { type: "button", className: "btn sm circle transparent danger", ariaLabel: app.attrs.tooltip("Remove"), onclick: () => (b[prop] = b[prop].filter((_, j) => j != i)) },
                            t.i({ className: "ri-close-line", ariaHidden: true }),
                        ),
                    )
                ),
            t.button(
                { type: "button", className: "btn sm secondary transparent", onclick: () => (b[prop] = [...(b[prop] || []), { label: "", value: "" }]) },
                t.i({ className: "ri-add-line", ariaHidden: true }),
                t.span({ className: "txt" }, addLabel),
            ),
        );
    }

    function blockSettings(b) {
        const out = [
            () => {
                const errs = errorsOf(b.id);
                return errs.length ? t.div({ rid: "errs" + errs.length, className: "alert danger m-b-sm gp-pre" }, errs.map((e) => e.error).join("\n")) : t.div({ rid: "noerr" });
            },
        ];
        switch (b.type) {
            case "heading":
                out.push(
                    field({ label: "Text", name: "b_text" }, formulaInput(b, "text", { id: "b_text", placeholder: "Invoice {{record.number}}" })),
                    t.div(
                        { className: "grid sm" },
                        t.div({ className: "col-6" }, labelled("Level", segmented(b, "level", [[1, "", "Title"], [2, "", "Section"], [3, "", "Small"]], "lvl" + b.id))),
                        t.div({ className: "col-6" }, labelled("Align", segmented(b, "align", aligns, "al" + b.id))),
                    ),
                    labelled("Color", colorInput(b, "color", b.type == "heading" && (b.level || 1) == 1 ? data.form.accent_color : "#1f2328")),
                );
                break;
            case "text":
                out.push(
                    field(
                        { label: "Text", name: "b_text", hint: "**bold**, *italic*, [a link](https://…); a new line starts a new line. Formulas: {{record.name}}, {{formatDate \"DD/MM/YYYY\" record.created}}." },
                        formulaInput(b, "text", { id: "b_text", multiline: true, rows: 5 }),
                    ),
                    t.div(
                        { className: "grid sm" },
                        t.div({ className: "col-6" }, labelled("Align", segmented(b, "align", aligns, "al" + b.id))),
                        t.div({ className: "col-6" }, field({ label: "Size (pt)" }, numberInput(b, "size", { min: 5, max: 72, placeholder: String(data.form.font_size || 10) }))),
                    ),
                    t.div(
                        { className: "flex gap-10 m-b-sm" },
                        ...[["bold", "Bold"], ["italic", "Italic"], ["mono", "Monospace"]].map(([k, label]) =>
                            t.label({ className: "gp-doc-check" }, t.input({ type: "checkbox", checked: () => !!b[k], onchange: (e) => (b[k] = e.target.checked) }), label)
                        ),
                    ),
                    labelled("Color", colorInput(b, "color", b.type == "heading" && (b.level || 1) == 1 ? data.form.accent_color : "#1f2328")),
                );
                break;
            case "fields":
                out.push(
                    t.label({ className: "gp-label" }, "Rows"),
                    rowsEditor(b, "rows", "Add row"),
                    field({ label: "Label width (%)" }, numberInput(b, "label_width", { min: 10, max: 80, placeholder: "35" })),
                );
                break;
            case "columns":
                out.push(
                    labelled(
                        "Columns",
                        segmented(
                            { get n() { return String((b.columns || []).length); }, set n(v) {
                                const n = Number(v);
                                const cols = [...(b.columns || [])];
                                while (cols.length < n) {
                                    cols.push({ width: 0, blocks: [] });
                                }
                                if (cols.length > n) {
                                    // the blocks of the removed columns go to the last kept one
                                    const moved = cols.slice(n).flatMap((c) => c.blocks || []);
                                    cols.length = n;
                                    cols[n - 1] = { ...cols[n - 1], blocks: [...(cols[n - 1].blocks || []), ...moved] };
                                }
                                const even = cols.map((c) => ({ ...c, width: 0 }));
                                fitWidths(even);
                                b.columns = even;
                            } },
                            "n",
                            [["2", "", "2"], ["3", "", "3"], ["4", "", "4"]],
                            "cols" + b.id,
                        ),
                    ),
                    t.label({ className: "gp-label" }, "Widths (%)"),
                    () =>
                        t.div(
                            { rid: "w" + (b.columns || []).length, className: "flex gap-5" },
                            (b.columns || []).map((c, i) => t.div({ className: "field gp-bare gp-doc-w" }, numberInput(c, "width", { min: minWidth, max: 100 - minWidth, id: `cw${b.id}${i}`, onchange: () => balanceWidths(b.columns, i) }))),
                        ),
                    widthsHelp(() => b.columns, "Changing a width adjusts the others: they always add up to 100%."),
                    t.div({ className: "field-help" }, "Add blocks to each column in the Blocks list, or drag them there."),
                );
                break;
            case "table":
                out.push(
                    field(
                        { label: "Rows", name: "b_items", hint: "A list: one of the Data lists ({{lists.items}}), a list field of the record, or a value of the flow ({{values.lines}})." },
                        t.div(
                            { className: "gp-doc-items" },
                            () =>
                                data.form.lists.length
                                    ? t.div(
                                        { rid: "pick" + data.form.lists.length },
                                        select({
                                            placeholder: "- Choose a list -",
                                            options: () => [...data.form.lists.map((l) => ({ value: `{{lists.${l.name}}}`, label: `${l.name} (${l.collection})` })), ...(b.items && !data.form.lists.some((l) => b.items == `{{lists.${l.name}}}`) ? [{ value: b.items, label: b.items }] : [])],
                                            value: () => b.items,
                                            onchange: (v) => (b.items = v),
                                        }),
                                    )
                                    : t.div({ rid: "nolists", className: "txt-hint txt-sm" }, "No lists yet: add one in the Data tab (e.g. the lines of the order)."),
                            formulaInput(b, "items", { id: "b_items", mono: true, placeholder: "{{lists.items}}" }),
                        ),
                    ),
                    t.label({ className: "gp-label" }, "Columns"),
                    t.div({ className: "field-help m-b-5" }, "{{row.name}} is a field of the item, {{index}} its position."),
                    () =>
                        t.div(
                            { rid: "cells" + (b.cells || []).length, className: "gp-doc-cells" },
                            (b.cells || []).map((c, i) =>
                                t.div(
                                    { className: "gp-doc-cell" },
                                    t.div({ className: "field gp-bare" }, t.input({ type: "text", placeholder: "Header", value: () => c.header || "", oninput: (e) => (c.header = e.target.value) })),
                                    formulaInput(c, "value", { placeholder: "{{row.name}}", table: b }),
                                    t.div({ className: "field gp-bare gp-doc-w" }, numberInput(c, "width", { min: minWidth, max: 100, placeholder: "%", onchange: () => balanceWidths(b.cells, i) })),
                                    segmented(c, "align", aligns, `ca${b.id}${i}`),
                                    t.div(
                                        { className: "gp-doc-cell-actions" },
                                        t.button({ type: "button", className: "btn xs circle transparent secondary", ariaLabel: app.attrs.tooltip("Left"), disabled: i == 0, onclick: () => { const l = [...b.cells]; [l[i - 1], l[i]] = [l[i], l[i - 1]]; b.cells = l; } }, t.i({ className: "ri-arrow-left-s-line", ariaHidden: true })),
                                        t.button({ type: "button", className: "btn xs circle transparent danger", ariaLabel: app.attrs.tooltip("Remove"), onclick: () => { const l = b.cells.filter((_, j) => j != i); fitWidths(l); b.cells = l; } }, t.i({ className: "ri-close-line", ariaHidden: true })),
                                    ),
                                )
                            ),
                            t.button(
                                { type: "button", className: "btn sm secondary transparent", onclick: () => { const l = [...(b.cells || []), { header: "", value: "", width: Math.round(100 / ((b.cells || []).length + 1)), align: "" }]; balanceWidths(l, l.length - 1); b.cells = l; } },
                                t.i({ className: "ri-add-line", ariaHidden: true }),
                                t.span({ className: "txt" }, "Add column"),
                            ),
                        ),
                    t.label({ className: "gp-label m-t-sm" }, "Totals (under the table)"),
                    widthsHelp(() => b.cells, "Widths in %: changing one adjusts the others, they always add up to 100%."),
                    rowsEditor(b, "totals", "Add total"),
                    t.div(
                        { className: "flex gap-10 m-t-sm m-b-sm" },
                        t.label({ className: "gp-doc-check" }, t.input({ type: "checkbox", checked: () => !!b.zebra, onchange: (e) => (b.zebra = e.target.checked) }), "Striped rows"),
                        t.label({ className: "gp-doc-check" }, t.input({ type: "checkbox", checked: () => !!b.hide_header, onchange: (e) => (b.hide_header = e.target.checked) }), "No header"),
                    ),
                    field({ label: "When the list is empty" }, formulaInput(b, "empty", { placeholder: "No items." })),
                );
                break;
            case "image": {
                const fileFields = () => (data.all.find((c) => c.name == data.form.collection)?.fields || []).filter((f) => f.type == "file").map((f) => f.name);
                const source = store({ v: b.field ? "field" : b.url ? "url" : "asset" });
                const setSource = (v) => {
                    source.v = v;
                    b.asset = v == "asset" ? b.asset : "";
                    b.field = v == "field" ? b.field || fileFields()[0] || "" : "";
                    b.url = v == "url" ? b.url : "";
                };
                out.push(
                    labelled(
                        "Image",
                        segmented({ get v() { return source.v; }, set v(x) { setSource(x); } }, "v", [["asset", "", "Uploaded"], ["field", "", "Record file"], ["url", "", "Address"]], "src" + b.id),
                    ),
                    () => {
                        if (source.v == "field") {
                            return t.div(
                                { rid: "field" },
                                fileFields().length
                                    ? field({ label: "File field of the record", hint: "Its first file." }, select({ placeholder: "- Choose -", options: () => fileFields().map((f) => ({ value: f, label: f })), value: () => b.field, onchange: (v) => (b.field = v) }))
                                    : t.div({ className: "txt-hint txt-sm m-b-sm" }, data.form.collection ? `${data.form.collection} has no file field: use Address for an image link.` : "Choose the collection of the records in Data first."),
                            );
                        }
                        if (source.v == "url") {
                            return t.div(
                                { rid: "url" },
                                field(
                                    {
                                        label: "Image address",
                                        name: "b_url",
                                        hint: "A formula giving an https:// address, e.g. {{record.image}}/high.webp, or a file of this app (/api/files/…). PNG, JPEG, GIF or WebP, 10 MB at most.",
                                    },
                                    formulaInput(b, "url", { id: "b_url", mono: true, placeholder: "https://…/{{record.photo}}" }),
                                ),
                            );
                        }
                        const assets = data.tpl?.assets || [];
                        return t.div(
                            { rid: "asset" + assets.join() },
                            t.div(
                                { className: "gp-doc-assets m-b-sm" },
                                assets.map((a) =>
                                    t.button(
                                        { type: "button", className: () => `gp-doc-asset ${b.asset == a ? "active" : ""}`, title: a, onclick: () => (b.asset = a) },
                                        t.img({ src: app.pb.files.getURL(data.tpl, a, { thumb: "100x100" }), alt: a }),
                                    )
                                ),
                                uploadTile(async (files) => {
                                    const added = await uploadAssets(files);
                                    if (added[0]) {
                                        b.asset = added[0];
                                    }
                                }),
                            ),
                        );
                    },
                    t.div(
                        { className: "grid sm" },
                        t.div({ className: "col-6" }, field({ label: "Width (mm)" }, numberInput(b, "width", { min: 5, max: 400 }))),
                        t.div({ className: "col-6" }, labelled("Align", segmented(b, "align", aligns, "al" + b.id))),
                    ),
                );
                break;
            }
            case "spacer":
                out.push(field({ label: "Height (mm)" }, numberInput(b, "height", { min: 1, max: 200 })));
                break;
        }
        out.push(
            t.details(
                { className: "gp-doc-showif m-t-sm", open: () => !!b.show_if },
                t.summary(null, "Show only when…"),
                field(
                    { name: "b_show", hint: "A formula: the block is left out when it gives nothing, false or 0. E.g. {{record.paid}}, {{gt record.discount 0}}." },
                    formulaInput(b, "show_if", { id: "b_show", mono: true, placeholder: "{{record.paid}}" }),
                ),
            ),
        );
        return out;
    }

    // --- page and data settings ------------------------------------------------------------------

    function pageSettings() {
        const f = data.form;
        return [
            t.div(
                { className: "grid sm" },
                t.div({ className: "col-lg-6" }, field({ label: "Name", name: "name" }, t.input({ id: "name", type: "text", value: () => f.name, oninput: (e) => (f.name = e.target.value) }))),
                t.div({ className: "col-lg-6" }, field({ label: "Folder", name: "folder" }, t.input({ id: "folder", type: "text", placeholder: "No folder", value: () => f.folder || "", oninput: (e) => (f.folder = e.target.value) }))),
            ),
            t.label({ className: "gp-label" }, "Page"),
            t.div(
                { className: "gp-doc-formats" },
                Object.entries(formatLabels).map(([k, label]) =>
                    t.button(
                        { type: "button", className: () => `gp-doc-format ${f.format == k ? "active" : ""}`, ariaPressed: () => f.format == k, onclick: () => (f.format = k) },
                        t.span({ className: `gp-doc-format-shape ${f.orientation == "landscape" ? "landscape" : ""}`, ariaHidden: true }),
                        t.strong(null, label),
                        t.small({ className: "txt-hint" }, { a4: "210 × 297", a5: "148 × 210", a3: "297 × 420", letter: "216 × 279", legal: "216 × 356", custom: "your size" }[k]),
                    )
                ),
            ),
            t.div(
                { className: "grid sm m-t-sm" },
                t.div({ className: "col-lg-6" }, labelled("Orientation", segmented(f, "orientation", [["portrait", "ri-file-line", "Portrait"], ["landscape", "ri-file-line gp-rot", "Landscape"]], "orient"))),
                () =>
                    f.format == "custom"
                        ? t.div(
                            { rid: "custom", className: "col-lg-6 flex gap-5" },
                            field({ label: "Width (mm)" }, numberInput(f, "width_mm", { min: 50, max: 1200 })),
                            field({ label: "Height (mm)" }, numberInput(f, "height_mm", { min: 50, max: 1200 })),
                        )
                        : t.div({ rid: "std", className: "col-lg-6" }),
            ),
            t.label({ className: "gp-label" }, "Margins (mm)"),
            t.div(
                { className: "gp-doc-margins" },
                [["top", "Top"], ["right", "Right"], ["bottom", "Bottom"], ["left", "Left"]].map(([k, label]) =>
                    field({ label }, numberInput(f.margins, k, { min: 0, max: 80 })),
                ),
            ),
            t.div(
                { className: "grid sm" },
                t.div({ className: "col-lg-6" }, field({ label: "Text size (pt)" }, numberInput(f, "font_size", { min: 6, max: 24, step: 0.5 }))),
                t.div({ className: "col-lg-6" }, labelled("Accent color (titles, table headers, links)", colorInput(f, "accent_color", "#2563eb"))),
            ),
            field(
                { label: "File name", name: "filename", hint: "A formula; .pdf is added. E.g. invoice-{{record.number}}." },
                formulaInput(f, "filename", { id: "filename", mono: true, placeholder: () => `${f.slug}-{{record.id}}` }),
            ),
        ];
    }

    // uploadAssets adds images to the template; it returns their names
    async function uploadAssets(files) {
        const fd = new FormData();
        for (const file of files) {
            fd.append("assets+", file);
        }
        const before = data.tpl?.assets || [];
        try {
            data.tpl = await app.pb.collection("document_templates").update(id, fd);
            app.toasts.success("Uploaded.");
            return (data.tpl.assets || []).filter((a) => !before.includes(a));
        } catch (err) {
            handleError(err);
        }
        return [];
    }

    function uploadTile(onfiles) {
        return t.label(
            { className: "gp-doc-upload" },
            t.input({ type: "file", accept: "image/png,image/jpeg,image/gif,image/webp", multiple: true, onchange: (e) => (onfiles([...e.target.files]), (e.target.value = "")) }),
            t.i({ className: "ri-upload-2-line", ariaHidden: true }),
            t.span(null, "Upload"),
        );
    }

    async function removeAsset(name) {
        try {
            data.tpl = await app.pb.collection("document_templates").update(id, { "assets-": [name] });
        } catch (err) {
            handleError(err);
        }
    }

    function dataSettings() {
        const f = data.form;
        return [
            field(
                { label: "About the records of", hint: "The document is made for one of its records: {{record.…}}. The flows give the record." },
                select({
                    placeholder: "- No collection -",
                    options: () => data.collections.map((c) => ({ value: c, label: c })),
                    value: () => f.collection,
                    onchange: (v) => {
                        f.collection = v;
                        f.sample_record = "";
                        loadSamples();
                    },
                }),
            ),
            t.label({ className: "gp-label m-t-sm" }, "Lists"),
            t.div({ className: "field-help m-b-5" }, "Records shown in tables, e.g. items: the order_items where order = {{record.id}}."),
            () =>
                t.div(
                    { rid: "lists" + f.lists.length, className: "gp-doc-lists" },
                    f.lists.map((l, i) =>
                        t.div(
                            { className: "gp-doc-list" },
                            t.div(
                                { className: "grid sm" },
                                t.div({ className: "col-6" }, field({ label: "Name" }, t.input({ type: "text", className: "txt-code", placeholder: "items", value: () => l.name, oninput: (e) => (l.name = e.target.value) }))),
                                t.div({ className: "col-6" }, field({ label: "Collection" }, select({ placeholder: "- Choose -", options: () => data.collections.map((c) => ({ value: c, label: c })), value: () => l.collection, onchange: (v) => (l.collection = v) }))),
                                t.div(
                                    { className: "col-12" },
                                    field(
                                        { label: "Filter", hint: "PocketBase filter; {{record.x}} and {{values.x}} are given as safe parameters." },
                                        t.input({ type: "text", className: "txt-code", placeholder: "order = {{record.id}}", value: () => l.filter || "", oninput: (e) => (l.filter = e.target.value) }),
                                    ),
                                ),
                                t.div({ className: "col-6" }, field({ label: "Sort" }, t.input({ type: "text", className: "txt-code", placeholder: "created", value: () => l.sort || "", oninput: (e) => (l.sort = e.target.value) }))),
                                t.div({ className: "col-6" }, field({ label: "Limit" }, numberInput(l, "limit", { min: 1, max: 5000, placeholder: "5000" }))),
                            ),
                            t.button({ type: "button", className: "btn xs transparent danger", onclick: () => (f.lists = f.lists.filter((_, j) => j != i)) }, t.i({ className: "ri-delete-bin-7-line", ariaHidden: true }), t.span({ className: "txt" }, "Remove the list")),
                        )
                    ),
                    t.button(
                        {
                            type: "button",
                            className: "btn sm secondary transparent",
                            onclick: () => {
                                const n = f.lists.length ? `list${f.lists.length + 1}` : "items";
                                f.lists = [...f.lists, { name: n, collection: "", filter: f.collection ? "" : "", sort: "", limit: 0 }];
                            },
                        },
                        t.i({ className: "ri-add-line", ariaHidden: true }),
                        t.span({ className: "txt" }, "Add list"),
                    ),
                ),
            t.label({ className: "gp-label m-t-sm" }, "Images of the template"),
            () =>
                t.div(
                    { rid: "assets" + (data.tpl?.assets || []).join(), className: "gp-doc-assets" },
                    (data.tpl?.assets || []).map((a) =>
                        t.div(
                            { className: "gp-doc-asset" },
                            t.img({ src: app.pb.files.getURL(data.tpl, a, { thumb: "100x100" }), alt: a }),
                            t.button({ type: "button", className: "btn xs circle danger gp-doc-asset-del", ariaLabel: app.attrs.tooltip("Remove"), onclick: () => removeAsset(a) }, t.i({ className: "ri-close-line", ariaHidden: true })),
                        )
                    ),
                    uploadTile(uploadAssets),
                ),
        ];
    }

    function recordName(r) {
        const col = data.all.find((c) => c.name == data.form.collection);
        const presentable = col?.fields.find((f) => f.presentable)?.name;
        const name = (presentable && r[presentable]) || r.name || r.title || r.email || r.number || "";
        return name ? `${name} (${r.id})` : r.id;
    }

    // --- layout: the panel (blocks, page, data) and the stage (the pages) -------------------------

    function blockPanel(b) {
        const def = blockTypes[b.type] || { label: b.type, icon: "" };
        const iconBtn = (icon, label, onclick, cls = "secondary") =>
            t.button({ type: "button", className: `btn xs circle transparent ${cls}`, ariaLabel: app.attrs.tooltip(label), onclick }, t.i({ className: icon, ariaHidden: true }));
        return t.div(
            { rid: "block" + b.id, className: "gp-doc-block-panel" },
            t.div(
                { className: "gp-doc-settings-head" },
                t.button({ type: "button", className: "btn xs transparent secondary gp-doc-back", onclick: () => (data.selected = "") }, t.i({ className: "ri-arrow-left-line", ariaHidden: true }), t.span({ className: "txt" }, "Blocks")),
                t.div(
                    { className: "gp-doc-block-actions" },
                    iconBtn("ri-arrow-up-line", "Move up (Alt+↑)", () => move(b.id, -1)),
                    iconBtn("ri-arrow-down-line", "Move down (Alt+↓)", () => move(b.id, 1)),
                    iconBtn("ri-file-copy-line", "Duplicate", () => duplicate(b.id)),
                    iconBtn("ri-delete-bin-7-line", "Delete", () => removeBlock(b.id), "danger"),
                ),
            ),
            t.div({ className: "gp-doc-block-title" }, t.i({ className: def.icon, ariaHidden: true }), t.strong(null, def.label), t.span({ className: "txt-hint txt-ellipsis" }, () => blockLabel(b) != def.label ? blockLabel(b) : "")),
            blockSettings(b),
        );
    }

    function panel() {
        const tab = (k, label) => t.button({ type: "button", className: () => `gp-tab-btn ${data.tab == k ? "active" : ""}`, onclick: () => (data.tab = k) }, label);
        return t.div(
            { className: "gp-doc-panel" },
            t.div({ className: "gp-doc-tabs" }, tab("blocks", "Blocks"), tab("page", "Page"), tab("data", "Data")),
            t.div({ className: "gp-doc-panel-body" }, () => {
                if (data.tab == "page") {
                    return t.div({ rid: "page" }, pageSettings());
                }
                if (data.tab == "data") {
                    return t.div({ rid: "data" }, dataSettings());
                }
                const b = selectedBlock();
                return b ? blockPanel(b) : t.div({ rid: "outline" }, outline());
            }),
        );
    }

    // --- the stage: the pages drawn by PDF.js, the blocks clickable over them ------------------

    const zooms = [0.5, 0.75, 1, 1.25, 1.5, 2];
    const mmToPx = 96 / 25.4;
    let pdfjs = null;
    async function loadPdfjs() {
        if (!pdfjs) {
            pdfjs = await import(PDFJS + "pdf.min.mjs");
            pdfjs.GlobalWorkerOptions.workerSrc = PDFJS + "pdf.worker.min.mjs";
        }
        return pdfjs;
    }

    let sheetsEl = null;
    let loaded = null; // {url, doc}: the PDF of the preview drawn last
    let drawn = { url: "", width: 0 };
    let drawToken = 0;

    const sheetWidth = () => {
        const p = data.preview;
        if (data.zoom == "fit") {
            const room = (sheetsEl?.clientWidth || 800) - 48;
            return Math.max(240, Math.min(room, 1100));
        }
        return Math.round((p.pageWidth || 210) * mmToPx * data.zoom);
    };

    async function loadDocument(url) {
        const lib = await loadPdfjs();
        const res = await fetch(url);
        if (!res.ok) {
            throw new Error("The preview expired: change something to make it again.");
        }
        return lib.getDocument({ data: new Uint8Array(await res.arrayBuffer()), isEvalSupported: false }).promise;
    }

    // draw draws the pages in new sheets, then swaps them in (no flicker)
    async function draw() {
        if (!sheetsEl || !data.preview.raw) {
            return;
        }
        const token = ++drawToken;
        const p = data.preview;
        try {
            // a draw started later wins, even when its PDF comes first
            if (loaded?.url != p.raw) {
                const doc = await loadDocument(p.raw);
                if (token != drawToken) {
                    doc.destroy();
                    return;
                }
                loaded?.doc.destroy();
                loaded = { url: p.raw, doc };
            }
            const pdfDoc = loaded.doc;
            const width = sheetWidth();
            const dpr = Math.min(window.devicePixelRatio || 1, 2);
            const sheets = [];
            for (let i = 1; i <= pdfDoc.numPages; i++) {
                const pg = await pdfDoc.getPage(i);
                const base = pg.getViewport({ scale: 1 });
                const vp = pg.getViewport({ scale: (width / base.width) * dpr });
                const canvas = document.createElement("canvas");
                canvas.width = Math.round(vp.width);
                canvas.height = Math.round(vp.height);
                await pg.render({ canvasContext: canvas.getContext("2d"), viewport: vp }).promise;
                if (token != drawToken) {
                    return;
                }
                sheets.push(sheet(i, canvas, width, (width * base.height) / base.width));
            }
            sheetsEl.replaceChildren(...sheets);
            drawn = { url: p.raw, width };
            markHits();
        } catch (err) {
            if (token == drawToken) {
                data.preview.error = err.message || String(err);
            }
        }
    }

    function sheet(n, canvas, width, height) {
        const p = data.preview;
        const el = document.createElement("div");
        el.className = "gp-doc-sheet";
        el.style.width = width + "px";
        el.style.height = height + "px";
        el.dataset.page = n;
        el.append(canvas);
        // the bigger boxes first: the blocks of the columns are over their columns
        const boxes = (p.boxes || []).filter((b) => b.page == n).sort((a, b) => b.w * b.h - a.w * a.h);
        for (const box of boxes) {
            const at = locate(box.block);
            if (!at) {
                continue;
            }
            const hit = document.createElement("div");
            hit.className = "gp-doc-hit";
            hit.dataset.block = box.block;
            hit.dataset.label = blockLabel(at.block);
            hit.draggable = true;
            Object.assign(hit.style, {
                left: (box.x / p.pageWidth) * 100 + "%",
                top: (box.y / p.pageHeight) * 100 + "%",
                width: (box.w / p.pageWidth) * 100 + "%",
                height: (box.h / p.pageHeight) * 100 + "%",
            });
            hit.addEventListener("click", (e) => {
                e.stopPropagation();
                select_(box.block);
            });
            hit.addEventListener("mouseenter", () => (data.hover = box.block));
            hit.addEventListener("mouseleave", () => data.hover == box.block && (data.hover = ""));
            hit.addEventListener("dragstart", (e) => {
                e.stopPropagation();
                dragStart(e, { id: box.block });
            });
            hit.addEventListener("dragend", dragEnd);
            for (const [ev, fn] of Object.entries(dropOnBlock(box.block))) {
                hit.addEventListener(ev.slice(2), fn);
            }
            el.append(hit);
        }
        const label = document.createElement("div");
        label.className = "gp-doc-sheet-no";
        label.textContent = `${n} / ${p.pages}`;
        el.append(label);
        return el;
    }

    // the selected, hovered and failing blocks on the pages
    function markHits() {
        sheetsEl?.querySelectorAll(".gp-doc-hit").forEach((h) => {
            const id = h.dataset.block;
            h.classList.toggle("selected", id == data.selected);
            h.classList.toggle("hover", id == data.hover);
            h.classList.toggle("has-error", data.preview.errors.some((e) => e.block == id));
        });
    }

    // the selected block in view (chosen in the outline)
    function revealSelected() {
        const hit = data.selected && sheetsEl?.querySelector(`.gp-doc-hit[data-block="${CSS.escape(data.selected)}"]`);
        if (!hit) {
            return;
        }
        const r = hit.getBoundingClientRect();
        const box = sheetsEl.getBoundingClientRect();
        if (r.bottom < box.top + 20 || r.top > box.bottom - 20) {
            hit.scrollIntoView({ block: "center", behavior: "smooth" });
        }
    }

    function setZoom(z) {
        data.zoom = z;
        app.utils.saveLocalHistory(ZOOM_KEY, JSON.stringify(z));
        draw();
    }

    function zoomBy(delta) {
        const current = data.zoom == "fit" ? sheetWidth() / ((data.preview.pageWidth || 210) * mmToPx) : data.zoom;
        const next = delta > 0 ? zooms.find((z) => z > current + 0.01) : [...zooms].reverse().find((z) => z < current - 0.01);
        if (next) {
            setZoom(next);
        }
    }

    function errorList() {
        const errs = data.preview.errors;
        return t.div(
            { rid: "errs" + JSON.stringify(errs), className: "gp-doc-errors" },
            errs.slice(0, 8).map((e) => {
                const isList = String(e.block).startsWith("list:");
                const at = !isList && locate(e.block);
                return t.button(
                    { type: "button", className: "gp-doc-error", onclick: () => (isList ? (data.tab = "data") : select_(e.block)) },
                    t.i({ className: "ri-error-warning-line", ariaHidden: true }),
                    t.span(null, (at ? blockLabel(at.block) + ": " : isList ? `List ${e.block.slice(5)}: ` : "") + e.error),
                );
            }),
        );
    }

    function stage() {
        return t.div(
            { className: "gp-doc-stage" },
            t.div(
                { className: "gp-doc-stage-bar" },
                () =>
                    data.form.collection
                        ? t.div(
                            { rid: "sample" + data.form.collection + data.samples.length, className: "gp-doc-sample", ariaDescription: app.attrs.tooltip("The record the preview is made with") },
                            t.i({ className: "ri-database-2-line", ariaHidden: true }),
                            select({
                                placeholder: "The latest record",
                                options: () => data.samples.map((r) => ({ value: r.id, label: recordName(r) })),
                                value: () => data.form.sample_record,
                                onchange: (v) => (data.form.sample_record = v),
                            }),
                        )
                        : t.span({ rid: "nosample", className: "txt-hint txt-sm" }, "No record: choose a collection in Data"),
                () => (data.preview.loading ? t.span({ rid: "ld", className: "loader sm" }) : t.span({ rid: "nold" })),
                t.div(
                    { className: "gp-doc-zoom m-l-auto" },
                    t.button({ type: "button", className: "btn xs circle transparent secondary", ariaLabel: app.attrs.tooltip("Zoom out"), onclick: () => zoomBy(-1) }, t.i({ className: "ri-zoom-out-line", ariaHidden: true })),
                    t.button(
                        { type: "button", className: () => `btn xs transparent ${data.zoom == "fit" ? "" : "secondary"}`, ariaDescription: app.attrs.tooltip("Fit the width"), onclick: () => setZoom("fit") },
                        () => (data.zoom == "fit" ? "Fit" : Math.round(data.zoom * 100) + "%"),
                    ),
                    t.button({ type: "button", className: "btn xs circle transparent secondary", ariaLabel: app.attrs.tooltip("Zoom in"), onclick: () => zoomBy(1) }, t.i({ className: "ri-zoom-in-line", ariaHidden: true })),
                ),
                () => t.span({ rid: "info" + data.preview.pages, className: "txt-hint txt-sm gp-doc-pages-info" }, data.preview.pages ? `${data.preview.pages} page${data.preview.pages > 1 ? "s" : ""}` : ""),
                () =>
                    data.preview.raw
                        ? t.a(
                            { rid: "open" + data.preview.raw, className: "btn xs circle transparent secondary", href: data.preview.raw, target: "_blank", rel: "noopener", ariaLabel: app.attrs.tooltip("Open the PDF: " + data.preview.filename) },
                            t.i({ className: "ri-external-link-line", ariaHidden: true }),
                        )
                        : t.span({ rid: "noopen" }),
            ),
            () => (data.preview.error ? t.div({ rid: "err", className: "alert danger gp-doc-stage-alert gp-pre" }, data.preview.error) : t.div({ rid: "noerr" })),
            () => (data.preview.errors.length ? errorList() : t.div({ rid: "noerrs" })),
            t.div(
                {
                    className: () => `gp-doc-sheets ${data.dragging ? "dragging" : ""}`,
                    onmount: (el) => {
                        sheetsEl = el;
                        const ro = new ResizeObserver(() => {
                            if (data.zoom == "fit" && Math.abs(sheetWidth() - drawn.width) > 8) {
                                redraw();
                            }
                        });
                        ro.observe(el);
                        el._ro = ro;
                    },
                    onunmount: (el) => {
                        el._ro?.disconnect();
                        sheetsEl = null;
                    },
                    onclick: (e) => {
                        if (e.target == e.currentTarget) {
                            data.selected = "";
                        }
                    },
                },
                t.div({ className: "gp-doc-sheets-empty txt-hint" }, "Making the preview…"),
            ),
        );
    }

    const redraw = debounce(draw, 150);

    function onKeydown(e) {
        if ((e.ctrlKey || e.metaKey) && e.key == "s") {
            e.preventDefault();
            save();
            return;
        }
        const typing = /^(INPUT|TEXTAREA|SELECT)$/.test(document.activeElement?.tagName) || document.activeElement?.isContentEditable;
        if (e.altKey && (e.key == "ArrowUp" || e.key == "ArrowDown") && data.selected) {
            e.preventDefault();
            move(data.selected, e.key == "ArrowUp" ? -1 : 1);
        } else if (e.key == "Escape" && data.selected && !typing && !document.querySelector(":popover-open")) {
            data.selected = "";
        }
    }

    function onBeforeUnload(e) {
        if (dirty()) {
            e.preventDefault();
        }
    }

    const watchers = [];

    load();

    return t.div(
        { pbEvent: "gpPageDocumentEditor", className: "page gp-page gp-doc-page" },
        t.div(
            { className: "page-content gp-doc-builder" },
            t.header(
                { className: "page-header" },
                t.nav(
                    { className: "breadcrumbs" },
                    t.div({ className: "breadcrumb-item" }, t.a({ href: "#/documents" }, "Documents")),
                    t.div({ className: "breadcrumb-item" }, () => data.form?.name || "…"),
                ),
                t.div(
                    { className: "page-header-primary-btns" },
                    t.a({ href: "#/documents/generated", className: "btn sm secondary transparent gp-doc-gen-link" }, t.i({ className: "ri-history-line", ariaHidden: true }), t.span({ className: "txt" }, "Generated")),
                    t.button(
                        { type: "button", className: "btn sm outline", disabled: () => data.isLoading, onclick: generate, ariaDescription: app.attrs.tooltip("Makes the document for the preview record and keeps it in Generated") },
                        t.i({ className: "ri-file-pdf-2-line", ariaHidden: true }),
                        t.span({ className: "txt" }, "Generate"),
                    ),
                    t.button(
                        { type: "button", className: () => `btn sm expanded ${data.isSaving ? "loading" : ""}`, disabled: () => data.isSaving || !dirty(), onclick: save },
                        t.span({ className: "txt" }, () => (dirty() ? "Save" : "Saved")),
                    ),
                ),
            ),
            () => {
                if (data.isLoading || !data.form) {
                    return t.div({ rid: "loading", className: "block txt-center p-base" }, t.span({ className: "loader lg" }));
                }
                return t.div(
                    {
                        rid: "editor",
                        className: () => `gp-doc-editor view-${data.mobileView}`,
                        onmount: () => {
                            window.addEventListener("keydown", onKeydown);
                            window.addEventListener("beforeunload", onBeforeUnload);
                            watchers.push(watch(() => JSON.stringify([specOf(), data.form.collection, data.form.sample_record, data.tpl?.assets]), () => refresh()));
                            watchers.push(watch(() => data.preview.raw, () => draw()));
                            watchers.push(watch(() => [data.selected, data.hover, data.preview.errors.length].join("|"), () => markHits()));
                            watchers.push(watch(() => data.selected, () => setTimeout(revealSelected, 0)));
                            refresh();
                        },
                        onunmount: () => {
                            window.removeEventListener("keydown", onKeydown);
                            window.removeEventListener("beforeunload", onBeforeUnload);
                            refresh.cancel();
                            redraw.cancel();
                            watchers.forEach((w) => w.unwatch());
                            drawToken++;
                            loaded?.doc.destroy();
                            loaded = null;
                        },
                    },
                    t.div(
                        { className: "gp-doc-mobile-tabs" },
                        ["edit", "preview"].map((v) =>
                            t.button({ type: "button", className: () => `btn sm ${data.mobileView == v ? "" : "secondary transparent"}`, onclick: () => ((data.mobileView = v), v == "preview" && redraw()) }, v == "edit" ? "Edit" : "Preview")
                        ),
                    ),
                    panel(),
                    stage(),
                );
            },
        ),
    );
}
