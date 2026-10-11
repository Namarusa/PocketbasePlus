// Pages: the chart color palettes (shadcn/ui chart themes, served by
// /api/pages/chart-palettes from chartcolors.go) for the Theme page and the
// chart settings of the builder.

import { api } from "../pocketbaseplus/lib.js";

let cache = null;

/** The palettes: [{ name, light: [5], dark: [5] }]. */
export function loadPalettes() {
    cache ||= api("/api/pages/chart-palettes", { requestKey: null }).catch(() => {
        cache = null;
        return [];
    });
    return cache;
}

export const paletteLabel = (name) => (name == "multicolor" ? "Multicolor (shadcn classic)" : name[0].toUpperCase() + name.slice(1));

/** The light and dark colors of a setting (mirrors chartColors in chartcolors.go), or null. */
export function colorsOf(setting, palettes) {
    const v = String(setting || "").trim();
    const p = palettes.find((x) => x.name == v);
    if (p) {
        return { light: p.light, dark: p.dark };
    }
    if (v == "primary") {
        const c = [100, 75, 55, 40, 25].map((pct) => (pct == 100 ? "var(--primary)" : `color-mix(in oklab, var(--primary) ${pct}%, var(--background))`));
        return { light: c, dark: c };
    }
    if (v.startsWith("custom:")) {
        const list = v.slice(7).split(",").map((s) => s.trim());
        if (list.length && list.length <= 5 && list.every((c) => /^#([0-9a-f]{3}|[0-9a-f]{6})$/i.test(c))) {
            const c = [0, 1, 2, 3, 4].map((i) => list[i % list.length]);
            return { light: c, dark: c };
        }
    }
    return null;
}

/** The theme CSS of a setting (mirrors chartColorsCSS). */
export function paletteCSS(setting, palettes) {
    const c = colorsOf(setting, palettes);
    if (!c) {
        return "";
    }
    const vars = (list) => list.map((x, i) => `--chart-${i + 1}:${x};`).join("");
    return `:root{${vars(c.light)}}.dark{${vars(c.dark)}}`;
}

/** Five swatches of colors (HTML). */
export function swatches(colors) {
    return `<span class="gpp-swatches">${colors.map((c) => `<i style="background:${c.replace(/[<>"]/g, "")}"></i>`).join("")}</span>`;
}
