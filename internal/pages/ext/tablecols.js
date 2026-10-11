// Pages: the "Columns" setting of record tables (a table whose body is a
// collection list). It lists the columns in order: move them, remove them, or
// add one from the fields of the collection. The header cell and the cell of
// the repeated row stay together, and the empty row spans every column.

/** The parts of the record table around a component, or null. */
export function recordTable(comp) {
    for (let c = comp; c; c = c.parent()) {
        if (c.get("tagName") == "table") {
            const body = c.find("tbody").find((b) => b.getAttributes()["data-pb-list"] !== undefined);
            const row = body?.find("[data-pb-item]")[0];
            if (!row) {
                return null;
            }
            return { table: c, body, row, head: c.find("thead tr")[0] || null, empty: body.find("[data-pb-empty]")[0] || null };
        }
    }
    return null;
}

const tableTags = new Set(["table", "thead", "tbody", "tr", "th", "td"]);
const esc = (s) => String(s).replace(/[<>&"]/g, (c) => ({ "<": "&lt;", ">": "&gt;", "&": "&amp;", '"': "&quot;" })[c]);
export const columnLabel = (name) => name[0].toUpperCase() + name.slice(1).replace(/[_.]/g, " ");

/**
 * Defines the setting. ctx: { byName(name), fields(col), applySamples(comp), placeholderImage }.
 */
export function defineColumnsTrait(ed, ctx) {
    ed.Traits.addType("gpp-columns", {
        createInput({ component }) {
            const el = document.createElement("div");
            el.className = "gpp-cols";
            el.addEventListener("click", (e) => {
                const b = e.target.closest("button[data-act]");
                const t = recordTable(component);
                if (!b || !t) {
                    return;
                }
                const i = +b.closest("[data-i]").dataset.i;
                const n = t.row.components().length;
                if (b.dataset.act == "up" && i > 0) {
                    move(t, i, i - 1);
                } else if (b.dataset.act == "down" && i < n - 1) {
                    move(t, i, i + 1);
                } else if (b.dataset.act == "del") {
                    for (const coll of [t.row.components(), t.head?.components()]) {
                        coll?.at(i)?.remove();
                    }
                }
                span(t);
                render(el, component);
            });
            el.addEventListener("change", (e) => {
                if (!e.target.matches(".gpp-cols-add")) {
                    return;
                }
                const t = recordTable(component);
                const col = t && ctx.byName(t.body.getAttributes()["data-pb-list"]);
                const f = col && ctx.fields(col).find((x) => x.name == e.target.value);
                if (f) {
                    add(t, f);
                    span(t);
                    ctx.applySamples(t.row);
                }
                render(el, component);
            });
            return el;
        },
        onUpdate({ elInput, component }) {
            render(elInput, component);
        },
    });

    function move(t, from, to) {
        for (const coll of [t.row.components(), t.head?.components()]) {
            const c = coll?.at(from);
            if (c) {
                coll.remove(c, { temporary: true });
                coll.add(c, { at: to });
            }
        }
    }

    function add(t, f) {
        // component definitions: cell markup alone isn't parsed outside a table
        const field = { "data-pb-field": f.name };
        const content =
            f.type == "file"
                ? { type: "pb-image", attributes: { ...field, src: ctx.placeholderImage, alt: "" }, style: { width: "40px", height: "40px", "object-fit": "cover", "border-radius": "6px", display: "block" } }
                : { type: "pb-field", tagName: "span", attributes: field, components: `{${f.name}}` };
        t.row.append({ type: "cell", tagName: "td", components: [content] });
        if (t.head) {
            // header cells with the attributes of the others (e.g. a class)
            const attrs = { ...(t.head.components().at(0)?.getAttributes() || {}) };
            delete attrs.id;
            t.head.append({ type: "cell", tagName: "th", attributes: attrs, components: columnLabel(f.name) });
        }
    }

    // the empty row spans every column
    function span(t) {
        const cell = t.empty?.components().at(0);
        if (cell) {
            cell.addAttributes({ colspan: String(Math.max(t.row.components().length, 1)) });
        }
    }

    function render(el, component) {
        const t = recordTable(component);
        const col = t && ctx.byName(t.body.getAttributes()["data-pb-list"]);
        if (!t) {
            el.innerHTML = "";
            return;
        }
        const heads = t.head ? t.head.components().models : [];
        const cells = t.row.components().models;
        const rows = cells.map((cell, i) => {
            const bound = cell.find("[data-pb-field]")[0]?.getAttributes()["data-pb-field"] || (cell.find("[data-pb-link]").length ? "link" : "");
            const title = heads[i]?.getEl()?.textContent.trim() || (bound ? columnLabel(bound) : `Column ${i + 1}`);
            return (
                `<div class="gpp-cols-row" data-i="${i}"><span class="gpp-cols-name" title="${esc(bound || "custom content")}">${esc(title)}<small>${esc(bound || "custom")}</small></span>` +
                `<button type="button" data-act="up" title="Move left" ${i == 0 ? "disabled" : ""}><i class="ri-arrow-left-s-line"></i></button>` +
                `<button type="button" data-act="down" title="Move right" ${i == cells.length - 1 ? "disabled" : ""}><i class="ri-arrow-right-s-line"></i></button>` +
                `<button type="button" data-act="del" title="Remove the column"><i class="ri-close-line"></i></button></div>`
            );
        });
        const used = new Set(cells.map((c) => c.find("[data-pb-field]")[0]?.getAttributes()["data-pb-field"]).filter(Boolean));
        const free = col ? ctx.fields(col).filter((f) => !used.has(f.name)) : [];
        el.innerHTML =
            `<div class="gpp-cols-title">Columns</div>` +
            (rows.join("") || `<small>No columns.</small>`) +
            (col
                ? `<select class="gpp-cols-add"><option value="">+ Add a column</option>${free.map((f) => `<option value="${esc(f.name)}">${esc(f.label || f.name + " (" + f.type + ")")}</option>`).join("")}</select>`
                : `<small>Choose the collection of the list (select the table body).</small>`);
    }
}

/** Adds the setting to the selected parts of a record table. */
export function addColumnsTrait(comp) {
    if (!comp || !tableTags.has(comp.get("tagName")) || comp.getTrait("gpp-columns") || !recordTable(comp)) {
        return;
    }
    comp.addTrait({ type: "gpp-columns", name: "gpp-columns", label: false, changeProp: true }); // full width
}
