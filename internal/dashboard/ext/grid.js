// Dashboard grid, shared by the superuser dashboard and the viewer.
// Panels have a position and a size in grid units (x, y, w, h). In edit mode
// they can be moved (drag the header) and resized (drag the corner); panels
// that end up overlapped are pushed down.

import { downloadPanelCSV, panelBody, panelTypes } from "./panels.js";

export const ROW_HEIGHT = 28; // px per grid row
export const GAP = 12; // px between cells
const NARROW = 700; // below this width panels are stacked

// panels are compared by key: reactive store items aren't identical between reads
const keyOf = (p) => p.id || p._key;

function overlaps(a, b) {
    return keyOf(a) != keyOf(b) && a.x < b.x + b.w && b.x < a.x + a.w && a.y < b.y + b.h && b.y < a.y + a.h;
}

/** Pushes down the panels overlapping `moved`, then the ones they overlap, and so on. */
export function resolveCollisions(panels, moved) {
    const queue = [moved];
    let guard = 0;
    while (queue.length && guard++ < 1000) {
        const current = queue.shift();
        for (const other of panels) {
            if (keyOf(other) != keyOf(moved) && overlaps(current, other)) {
                other.y = current.y + current.h;
                queue.push(other);
            }
        }
    }
}

/** First free position for a new panel of the given size (top to bottom, left to right). */
export function freePosition(panels, w, h, columns) {
    for (let y = 0; y < 1000; y++) {
        for (let x = 0; x + w <= columns; x++) {
            const candidate = { x, y, w, h };
            if (!panels.some((p) => overlaps(candidate, p))) {
                return { x, y };
            }
        }
    }
    return { x: 0, y: panels.reduce((max, p) => Math.max(max, p.y + p.h), 0) };
}

/** Fullscreen helpers (the Fullscreen API isn't available everywhere, e.g. iOS Safari). */
export function toggleFullscreen(el) {
    if (document.fullscreenElement) {
        return document.exitFullscreen();
    }
    return el.requestFullscreen?.();
}

/**
 * The grid.
 *
 * - panels(): the panels list (store objects, mutated in place while editing)
 * - state(panel): the query result store of a panel ({ data, error, loading })
 * - editing(): edit mode
 * - actions: { edit(panel), duplicate(panel), remove(panel), changed() } (edit mode)
 */
export function dashboardGrid({ panels, state, editing = () => false, columns = 24, actions = {}, emptyMessage = null }) {
    const ui = store({ narrow: false, active: "" });
    let gridEl, resizeObserver;

    function cellSize() {
        const width = gridEl.clientWidth;
        return { w: (width - GAP * (columns - 1)) / columns + GAP, h: ROW_HEIGHT + GAP };
    }

    // drag to move (mode "move") or resize (mode "resize")
    function startDrag(e, panel, mode) {
        if (!editing() || ui.narrow || e.button !== 0) {
            return;
        }
        e.preventDefault();
        const cell = cellSize();
        const start = { px: e.clientX, py: e.clientY, x: panel.x, y: panel.y, w: panel.w, h: panel.h };
        const type = panelTypes[panel.type] || { minW: 1, minH: 1 };
        ui.active = keyOf(panel);

        const move = (ev) => {
            const dx = Math.round((ev.clientX - start.px) / cell.w);
            const dy = Math.round((ev.clientY - start.py) / cell.h);
            if (mode == "move") {
                panel.x = Math.min(Math.max(0, start.x + dx), columns - panel.w);
                panel.y = Math.max(0, start.y + dy);
            } else {
                panel.w = Math.min(Math.max(type.minW || 1, start.w + dx), columns - panel.x);
                panel.h = Math.min(Math.max(type.minH || 1, start.h + dy), 200);
            }
        };
        const stop = () => {
            window.removeEventListener("pointermove", move);
            window.removeEventListener("pointerup", stop);
            window.removeEventListener("pointercancel", stop);
            ui.active = "";
            if (start.x != panel.x || start.y != panel.y || start.w != panel.w || start.h != panel.h) {
                resolveCollisions(panels(), panel);
                actions.changed?.();
            }
        };
        window.addEventListener("pointermove", move);
        window.addEventListener("pointerup", stop);
        window.addEventListener("pointercancel", stop);
    }

    // The current panel object of a key. Elements are reused across renders (rid)
    // while the panels list can be replaced (e.g. the draft copy of edit mode),
    // so elements never keep a panel object: they read it through this proxy.
    function livePanel(initial) {
        const key = keyOf(initial);
        const current = () => panels().find((p) => keyOf(p) == key) || initial;
        return {
            panel: new Proxy({}, {
                get: (_, prop) => current()[prop],
                set: (_, prop, value) => {
                    current()[prop] = value;
                    return true;
                },
            }),
            current,
        };
    }

    function panelEl(initial) {
        const key = keyOf(initial);
        const { panel, current } = livePanel(initial);
        return t.div(
            {
                rid: key,
                className: () => {
                    const classes = ["gp-panel", `gp-panel-${panel.type}`];
                    if (editing()) classes.push("editing");
                    if (ui.active == key) classes.push("dragging");
                    if (panel.show_header === false) classes.push("no-header");
                    return classes.join(" ");
                },
                style: () => ui.narrow
                    ? `grid-column: 1 / -1; grid-row: auto / span ${panel.h}`
                    : `grid-column: ${panel.x + 1} / span ${panel.w}; grid-row: ${panel.y + 1} / span ${panel.h}`,
            },
            t.header(
                {
                    // headerless panels keep their header as an overlay (actions on hover)
                    className: "gp-panel-header",
                    onpointerdown: (e) => {
                        if (!e.target.closest("button")) {
                            startDrag(e, panel, "move");
                        }
                    },
                },
                () => (panel.icon ? t.i({ rid: panel.icon, className: `${panel.icon} gp-panel-icon`, ariaHidden: true }) : null),
                t.span({ className: "gp-panel-name", title: () => panel.note || "" }, () => panel.name || panelTypes[panel.type]?.label || ""),
                () => (panel.note ? t.i({ rid: "note", className: "ri-information-line gp-panel-note", title: panel.note, ariaHidden: true }) : null),
                t.div(
                    { className: "gp-panel-actions" },
                    () => editing()
                        ? [
                            t.button({ rid: "edit", type: "button", className: "btn sm circle transparent secondary", title: "Edit", onclick: () => actions.edit?.(current()) }, t.i({ className: "ri-pencil-line", ariaHidden: true })),
                            t.button({ rid: "dup", type: "button", className: "btn sm circle transparent secondary", title: "Duplicate", onclick: () => actions.duplicate?.(current()) }, t.i({ className: "ri-file-copy-line", ariaHidden: true })),
                            t.button({ rid: "del", type: "button", className: "btn sm circle transparent danger", title: "Delete", onclick: () => actions.remove?.(current()) }, t.i({ className: "ri-delete-bin-7-line", ariaHidden: true })),
                        ]
                        : [
                            panel.type != "label"
                                ? t.button(
                                    { rid: "csv", type: "button", className: "btn sm circle transparent secondary", title: "Download CSV", onclick: () => downloadPanelCSV(current(), state(current()).data) },
                                    t.i({ className: "ri-file-download-line", ariaHidden: true }),
                                )
                                : null,
                            t.button(
                                { rid: "fs", type: "button", className: "btn sm circle transparent secondary gp-panel-fullscreen", title: "Fullscreen", onclick: (e) => toggleFullscreen(e.target.closest(".gp-panel")) },
                                t.i({ className: "ri-fullscreen-line", ariaHidden: true }),
                            ),
                        ],
                ),
            ),
            // the body is recreated only when the type changes (charts keep their instance otherwise)
            () => t.div({ rid: panel.type, className: "gp-panel-body" }, panelBody(panel, state(panel), { onDrill: actions.drill ? (panel, target) => !editing() && actions.drill(panel, target) : null })),
            () => editing() && !ui.narrow
                ? t.div({ rid: "resize", className: "gp-panel-resize", title: "Resize", onpointerdown: (e) => startDrag(e, panel, "resize") })
                : null,
        );
    }

    return t.div(
        {
            className: () => `gp-grid ${editing() ? "editing" : ""} ${ui.narrow ? "narrow" : ""}`,
            style: () => {
                const rows = panels().reduce((max, p) => Math.max(max, p.y + p.h), 0) + (editing() ? 4 : 0);
                return `--gpColumns: ${columns}; --gpRow: ${ROW_HEIGHT}px; --gpGap: ${GAP}px; --gpRows: ${Math.max(rows, 1)}`;
            },
            onmount: (el) => {
                gridEl = el;
                resizeObserver = new ResizeObserver(() => (ui.narrow = el.clientWidth < NARROW));
                resizeObserver.observe(el);
            },
            onunmount: () => resizeObserver?.disconnect(),
        },
        () => {
            const list = panels();
            if (!list.length) {
                return emptyMessage ? t.div({ rid: "empty", className: "gp-grid-empty" }, emptyMessage) : null;
            }
            // stacked layout: top to bottom, then left to right
            const ordered = ui.narrow ? [...list].sort((a, b) => a.y - b.y || a.x - b.x) : list;
            return ordered.map(panelEl);
        },
    );
}
