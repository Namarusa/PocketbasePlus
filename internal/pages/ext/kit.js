// Pages: the Basecoat UI kit of the builder (shadcn/ui in plain HTML/CSS, MIT,
// embedded with its styles, script and the Geist fonts: nothing is loaded from
// the internet). Blocks, variant settings, page templates and the canvas theme.

export const kitVersion = "1.0.2";
export const styles = ["vega", "nova", "maia", "lyra", "mira", "luma", "sera", "rhea"];
export const styleURL = (style) => `/p/_static/basecoat-${kitVersion}-${styles.includes(style) ? style : "vega"}.css`;

// Lucide icons (ISC, see vendor/lucide-1.48.0), inline: no icon font or CDN
const paths = {
    check: '<path d="M20 6 9 17l-5-5"/>',
    x: '<path d="M18 6 6 18"/><path d="m6 6 12 12"/>',
    chevron: '<path d="m6 9 6 6 6-6"/>',
    arrow: '<path d="M5 12h14"/><path d="m12 5 7 7-7 7"/>',
    star: '<path d="M11.525 2.295a.53.53 0 0 1 .95 0l2.31 4.679a2.123 2.123 0 0 0 1.595 1.16l5.166.756a.53.53 0 0 1 .294.904l-3.736 3.638a2.123 2.123 0 0 0-.611 1.878l.882 5.14a.53.53 0 0 1-.771.56l-4.618-2.428a2.122 2.122 0 0 0-1.973 0L6.396 21.01a.53.53 0 0 1-.77-.56l.881-5.139a2.122 2.122 0 0 0-.611-1.879L2.16 9.795a.53.53 0 0 1 .294-.906l5.165-.755a2.122 2.122 0 0 0 1.597-1.16z"/>',
    zap: '<path d="M15.914 4a1.5 1.5 0 00-2.474-1.561l-9 9A1.5 1.5 0 005.5 14h4.002a.5.5 0 01.471.666L8.086 20a1.5 1.5 0 002.475 1.56l9-9A1.5 1.5 0 0018.5 10h-3.997a.5.5 0 01-.472-.667z"/>',
    shield: '<path d="M20 13c0 5-3.5 7.5-7.66 8.95a1 1 0 0 1-.67-.01C7.5 20.5 4 18 4 13V6a1 1 0 0 1 1-1c2 0 4.5-1.2 6.24-2.72a1.17 1.17 0 0 1 1.52 0C14.51 3.81 17 5 19 5a1 1 0 0 1 1 1z"/><path d="m9 12 2 2 4-4"/>',
    sparkles: '<path d="M11.017 2.814a1 1 0 0 1 1.966 0l1.051 5.558a2 2 0 0 0 1.594 1.594l5.558 1.051a1 1 0 0 1 0 1.966l-5.558 1.051a2 2 0 0 0-1.594 1.594l-1.051 5.558a1 1 0 0 1-1.966 0l-1.051-5.558a2 2 0 0 0-1.594-1.594l-5.558-1.051a1 1 0 0 1 0-1.966l5.558-1.051a2 2 0 0 0 1.594-1.594z"/><path d="M20 2v4"/><path d="M22 4h-4"/><circle cx="4" cy="20" r="2"/>',
    info: '<circle cx="12" cy="12" r="10"/><path d="M12 16v-4"/><path d="M12 8h.01"/>',
    alert: '<circle cx="12" cy="12" r="10"/><line x1="12" x2="12" y1="8" y2="12"/><line x1="12" x2="12.01" y1="16" y2="16"/>',
    quote: '<path d="M16 3a2 2 0 0 0-2 2v6a2 2 0 0 0 2 2 1 1 0 0 1 1 1v1a2 2 0 0 1-2 2 1 1 0 0 0-1 1v2a1 1 0 0 0 1 1 6 6 0 0 0 6-6V5a2 2 0 0 0-2-2z"/><path d="M5 3a2 2 0 0 0-2 2v6a2 2 0 0 0 2 2 1 1 0 0 1 1 1v1a2 2 0 0 1-2 2 1 1 0 0 0-1 1v2a1 1 0 0 0 1 1 6 6 0 0 0 6-6V5a2 2 0 0 0-2-2z"/>',
    rocket: '<path d="M12 15v5s3.03-.55 4-2c1.08-1.62 0-5 0-5"/><path d="M4.5 16.5c-1.5 1.26-2 5-2 5s3.74-.5 5-2c.71-.84.7-2.13-.09-2.91a2.18 2.18 0 0 0-2.91-.09"/><path d="M9 12a22 22 0 0 1 2-3.95A12.88 12.88 0 0 1 22 2c0 2.72-.78 7.5-6 11a22.4 22.4 0 0 1-4 2z"/><path d="M9 12H4s.55-3.03 2-4c1.62-1.08 5 .05 5 .05"/>',
    lock: '<rect width="18" height="11" x="3" y="11" rx="2" ry="2"/><path d="M7 11V7a5 5 0 0 1 10 0v4"/>',
    "arrow-right": '<path d="M5 12h14"/><path d="m12 5 7 7-7 7"/>',
    "arrow-left": '<path d="m12 19-7-7 7-7"/><path d="M19 12H5"/>',
    "chevron-right": '<path d="m9 18 6-6-6-6"/>',
    "chevron-down": '<path d="m6 9 6 6 6-6"/>',
    plus: '<path d="M5 12h14"/><path d="M12 5v14"/>',
    download: '<path d="M12 15V3"/><path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/><path d="m7 10 5 5 5-5"/>',
    upload: '<path d="M12 3v12"/><path d="m17 8-5-5-5 5"/><path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/>',
    "external-link": '<path d="M15 3h6v6"/><path d="M10 14 21 3"/><path d="M18 13v6a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V8a2 2 0 0 1 2-2h6"/>',
    mail: '<path d="m22 7-8.991 5.727a2 2 0 0 1-2.009 0L2 7"/><rect x="2" y="4" width="20" height="16" rx="2"/>',
    phone: '<path d="M13.832 16.568a1 1 0 0 0 1.213-.303l.355-.465A2 2 0 0 1 17 15h3a2 2 0 0 1 2 2v3a2 2 0 0 1-2 2A18 18 0 0 1 2 4a2 2 0 0 1 2-2h3a2 2 0 0 1 2 2v3a2 2 0 0 1-.8 1.6l-.468.351a1 1 0 0 0-.292 1.233 14 14 0 0 0 6.392 6.384"/>',
    search: '<path d="m21 21-4.34-4.34"/><circle cx="11" cy="11" r="8"/>',
    settings: '<path d="M9.671 4.136a2.34 2.34 0 0 1 4.659 0 2.34 2.34 0 0 0 3.319 1.915 2.34 2.34 0 0 1 2.33 4.033 2.34 2.34 0 0 0 0 3.831 2.34 2.34 0 0 1-2.33 4.033 2.34 2.34 0 0 0-3.319 1.915 2.34 2.34 0 0 1-4.659 0 2.34 2.34 0 0 0-3.32-1.915 2.34 2.34 0 0 1-2.33-4.033 2.34 2.34 0 0 0 0-3.831A2.34 2.34 0 0 1 6.35 6.051a2.34 2.34 0 0 0 3.319-1.915"/><circle cx="12" cy="12" r="3"/>',
    user: '<path d="M19 21v-2a4 4 0 0 0-4-4H9a4 4 0 0 0-4 4v2"/><circle cx="12" cy="7" r="4"/>',
    heart: '<path d="M2 9.5a5.5 5.5 0 0 1 9.591-3.676.56.56 0 0 0 .818 0A5.49 5.49 0 0 1 22 9.5c0 2.29-1.5 4-3 5.5l-5.492 5.313a2 2 0 0 1-3 .019L5 15c-1.5-1.5-3-3.2-3-5.5"/>',
    "trash-2": '<path d="M10 11v6"/><path d="M14 11v6"/><path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6"/><path d="M3 6h18"/><path d="M8 6V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2"/>',
    pencil: '<path d="M21.174 6.812a1 1 0 0 0-3.986-3.987L3.842 16.174a2 2 0 0 0-.5.83l-1.321 4.352a.5.5 0 0 0 .623.622l4.353-1.32a2 2 0 0 0 .83-.497z"/><path d="m15 5 4 4"/>',
    send: '<path d="M14.536 21.686a.5.5 0 0 0 .937-.024l6.5-19a.496.496 0 0 0-.635-.635l-19 6.5a.5.5 0 0 0-.024.937l7.93 3.18a2 2 0 0 1 1.112 1.11z"/><path d="m21.854 2.147-10.94 10.939"/>',
    "shopping-cart": '<path d="m2.05 2.05 1.099-.028a1 1 0 0 1 1.008.815l2.69 14.347A1 1 0 0 0 7.83 18H18"/><path d="M4.563 5h16.435a1 1 0 0 1 .981 1.204l-1.026 6.226A2 2 0 0 1 18.962 14H6.25"/><circle cx="18" cy="20" r="2"/><circle cx="8" cy="20" r="2"/>',
    calendar: '<path d="M8 2v3"/><path d="M16 2v3"/><rect x="3" y="3" width="18" height="18" rx="2"/><path d="M3 9h18"/>',
    "map-pin": '<path d="M20 10c0 4.993-5.539 10.193-7.399 11.799a1 1 0 0 1-1.202 0C9.539 20.193 4 14.993 4 10a8 8 0 0 1 16 0"/><circle cx="12" cy="10" r="3"/>',
    house: '<path d="M15 21v-8a1 1 0 0 0-1-1h-4a1 1 0 0 0-1 1v8"/><path d="M3 10a2 2 0 0 1 .709-1.528l7-6a2 2 0 0 1 2.582 0l7 6A2 2 0 0 1 21 10v9a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z"/>',
    menu: '<path d="M4 5h16"/><path d="M4 12h16"/><path d="M4 19h16"/>',
    "log-in": '<path d="m10 17 5-5-5-5"/><path d="M15 12H3"/><path d="M15 3h4a2 2 0 0 1 2 2v14a2 2 0 0 1-2 2h-4"/>',
    "log-out": '<path d="m16 17 5-5-5-5"/><path d="M21 12H9"/><path d="M9 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h4"/>',
    play: '<path d="M5 5a2 2 0 0 1 3.008-1.728l11.997 6.998a2 2 0 0 1 .003 3.458l-12 7A2 2 0 0 1 5 19z"/>',
    sun: '<circle cx="12" cy="12" r="4"/><path d="M12 2v2"/><path d="M12 20v2"/><path d="m4.93 4.93 1.41 1.41"/><path d="m17.66 17.66 1.41 1.41"/><path d="M2 12h2"/><path d="M20 12h2"/><path d="m6.34 17.66-1.41 1.41"/><path d="m19.07 4.93-1.41 1.41"/>',
    moon: '<path d="M20.985 12.486a9 9 0 1 1-9.473-9.472c.405-.022.617.46.402.803a6 6 0 0 0 8.268 8.268c.344-.215.825-.004.803.401"/>',
    "panel-left": '<rect width="18" height="18" x="3" y="3" rx="2"/><path d="M9 3v18"/>',
    copy: '<rect width="14" height="14" x="8" y="8" rx="2" ry="2"/><path d="M4 16c-1.1 0-2-.9-2-2V4c0-1.1.9-2 2-2h10c1.1 0 2 .9 2 2"/>',
    bell: '<path d="M10.268 21a2 2 0 0 0 3.464 0"/><path d="M3.262 15.326A1 1 0 0 0 4 17h16a1 1 0 0 0 .74-1.673C19.41 13.956 18 12.499 18 8A6 6 0 0 0 6 8c0 4.499-1.411 5.956-2.738 7.326"/>',
};
const svg = (name, size, attrs) =>
    `<svg xmlns="http://www.w3.org/2000/svg" width="${size}" height="${size}" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true" ${attrs}>${paths[name]}</svg>`;
/** An inline icon (data-icon: the icon setting of buttons and badges finds it). */
export const icon = (name, size = 16) => svg(name, size, `data-icon="${name}"`);

// the icons offered in the settings of buttons and badges
const iconChoices = Object.keys(paths).filter((n) => !["arrow", "chevron", "alert", "quote"].includes(n)).sort();

// --- variants: settings of the components (attributes, removed when "Default") ----------------

const opt = (values) => values.map((v) => (Array.isArray(v) ? { id: v[0], label: v[1] } : { id: v, label: v[0].toUpperCase() + v.slice(1) }));
const tooltipTraits = [
    { type: "kit-attr", name: "data-tooltip", label: "Tooltip" },
    { type: "kit-attr", name: "data-side", label: "Tooltip side", options: opt([["", "Top"], "bottom", "left", "right"]) },
];
const iconTraits = [
    { type: "kit-icon", name: "gpp-icon", changeProp: true, label: "Icon", options: [{ id: "", label: "None" }, ...iconChoices.map((n) => ({ id: n, label: n[0].toUpperCase() + n.slice(1).replace(/-\d*/g, " ").trim() }))] },
    { type: "kit-icon-pos", name: "gpp-icon-pos", changeProp: true, label: "Icon position", options: opt([["before", "Before the text"], ["after", "After the text"]]) },
];
const variantTraits = {
    btn: [
        { type: "kit-attr", name: "data-variant", label: "Variant", options: opt([["", "Primary"], "secondary", "outline", "ghost", "link", "destructive"]) },
        {
            type: "kit-attr",
            name: "data-size",
            label: "Size",
            options: opt([["xs", "Extra small"], ["sm", "Small"], ["", "Default"], ["lg", "Large"], ["icon-xs", "Icon only, extra small"], ["icon-sm", "Icon only, small"], ["icon", "Icon only"], ["icon-lg", "Icon only, large"]]),
        },
        ...iconTraits,
        ...tooltipTraits,
    ],
    badge: [{ type: "kit-attr", name: "data-variant", label: "Variant", options: opt([["", "Primary"], "secondary", "outline", "destructive", "ghost", "link"]) }, ...iconTraits, ...tooltipTraits],
    sidebar: [
        { type: "kit-attr", name: "data-side", label: "Side", options: opt([["", "Left"], "right"]) },
        { type: "kit-attr", name: "data-initial-open", label: "On computers", options: opt([["", "Open"], ["false", "Closed (opens with the toggle)"]]) },
        { type: "kit-attr", name: "data-initial-mobile-open", label: "On phones", options: opt([["", "Closed (opens with the toggle)"], ["true", "Open"]]) },
    ],
    alert: [{ type: "kit-attr", name: "data-variant", label: "Variant", options: opt([["", "Default"], "destructive"]) }],
    avatar: [{ type: "kit-attr", name: "data-size", label: "Size", options: opt([["sm", "Small"], ["", "Default"], ["lg", "Large"]]) }],
    card: [{ type: "kit-attr", name: "data-size", label: "Size", options: opt([["", "Default"], ["sm", "Small"]]) }],
    item: [
        { type: "kit-attr", name: "data-variant", label: "Variant", options: opt([["", "Default"], "outline", "muted"]) },
        { type: "kit-attr", name: "data-size", label: "Size", options: opt([["", "Default"], ["sm", "Small"], ["xs", "Extra small"]]) },
    ],
};

/** The Basecoat classes (private selectors: styling a component never rewrites every .btn). */
export const kitClasses = [
    "btn", "badge", "alert", "card", "card-title", "card-description", "card-action", "avatar", "avatar-group", "table",
    "table-container", "accordion", "breadcrumb", "kbd", "progress", "skeleton", "empty", "item", "item-group", "tabs",
    "dropdown-menu", "popover", "dialog", "alert-dialog", "field", "label", "input", "textarea", "button-group",
    "sidebar", "scrollbar", "icon-sun", "icon-moon",
];

/** Registers the variant setting type (an attribute removed when empty). */
export function defineKitTraits(ed) {
    ed.Traits.addType("kit-attr", {
        createInput({ trait }) {
            const options = trait.get("options");
            if (options?.length) {
                const el = document.createElement("select");
                for (const o of options) {
                    el.add(new Option(o.label, o.id));
                }
                return el;
            }
            const el = document.createElement("input");
            el.type = "text";
            return el;
        },
        onEvent({ elInput, component, trait }) {
            const v = elInput.value.trim();
            const name = trait.getName();
            if (v) {
                component.addAttributes({ [name]: v });
            } else {
                component.removeAttributes(name);
            }
        },
        onUpdate({ elInput, component, trait }) {
            elInput.value = component.getAttributes()[trait.getName()] || "";
        },
    });
    const select = (trait) => {
        const el = document.createElement("select");
        for (const o of trait.get("options") || []) {
            el.add(new Option(o.label, o.id));
        }
        return el;
    };
    // icon picker: the icons shown in a grid (a select can't show them)
    const iconLabel = (trait, v) => (trait.get("options") || []).find((o) => o.id == v)?.label || "None";
    const showIcon = (el, trait, v) => {
        el.dataset.value = v;
        el.querySelector(".gpp-icon-current").innerHTML = (v && paths[v] ? svg(v, 16, "") : "") + `<span>${iconLabel(trait, v)}</span><i class="ri-arrow-down-s-line"></i>`;
        for (const b of el.querySelectorAll(".gpp-icon-grid button")) {
            b.classList.toggle("active", b.dataset.value == v);
        }
    };
    ed.Traits.addType("kit-icon", {
        createInput({ trait }) {
            const el = document.createElement("div");
            el.className = "gpp-icon-picker";
            const current = document.createElement("button");
            current.type = "button";
            current.className = "gpp-icon-current";
            const grid = document.createElement("div");
            grid.className = "gpp-icon-grid";
            grid.hidden = true;
            current.onclick = () => (grid.hidden = !grid.hidden);
            for (const o of trait.get("options") || []) {
                const b = document.createElement("button");
                b.type = "button";
                b.title = o.label;
                b.dataset.value = o.id;
                b.innerHTML = o.id ? svg(o.id, 18, "") : '<i class="ri-forbid-line"></i>';
                b.onclick = () => {
                    el.dataset.value = o.id;
                    grid.hidden = true;
                    el.dispatchEvent(new Event("change", { bubbles: true }));
                };
                grid.append(b);
            }
            el.append(current, grid);
            return el;
        },
        onEvent({ elInput, component, trait }) {
            setIcon(component, elInput.dataset.value || "", iconAfter(component));
            showIcon(elInput, trait, elInput.dataset.value || "");
        },
        onUpdate({ elInput, component, trait }) {
            showIcon(elInput, trait, iconChild(component)?.getAttributes()["data-icon"] || "");
        },
    });
    ed.Traits.addType("kit-icon-pos", {
        createInput: ({ trait }) => select(trait),
        onEvent({ elInput, component }) {
            const c = iconChild(component);
            if (c) {
                setIcon(component, c.getAttributes()["data-icon"], elInput.value == "after");
            }
        },
        onUpdate({ elInput, component }) {
            elInput.value = iconAfter(component) ? "after" : "before";
        },
    });
}

// icon of a button or badge: an svg child marked data-icon, first or last
const iconChild = (comp) => comp.components().find((c) => c.getAttributes()["data-icon"]);
function iconAfter(comp) {
    const kids = comp.components();
    const c = iconChild(comp);
    return !!c && kids.length > 1 && kids.indexOf(c) == kids.length - 1;
}
function setIcon(comp, name, after) {
    iconChild(comp)?.remove();
    if (name) {
        comp.components().add(icon(name), { at: after ? comp.components().length : 0 });
    }
}

/** Adds the variant settings of the Basecoat classes of a component (pages: [{ title, path }]). */
export function applyKitTraits(comp, pages = []) {
    if (!comp?.getClasses) {
        return;
    }
    const classes = comp.getClasses();
    for (const [cls, traits] of Object.entries(variantTraits)) {
        if (classes.includes(cls)) {
            for (const tr of traits) {
                if (!comp.getTrait(tr.name)) {
                    comp.addTrait(tr);
                }
            }
        }
    }
    // links: pick a page instead of typing its address
    if (comp.is("link") && !comp.getAttributes()["data-pb-link"] && !comp.getTrait("gpp-page")) {
        comp.addTrait({
            type: "kit-page",
            name: "gpp-page",
            changeProp: true, // not an attribute of the link
            label: "Page",
            options: [{ id: "", label: "- Other address -" }, ...pages.map((p) => ({ id: p.path, label: p.title }))],
        }, { at: 0 });
    }
}

/** A select setting the href of links to one of the pages. */
export function defineKitPageTrait(ed) {
    ed.Traits.addType("kit-page", {
        createInput({ trait }) {
            const el = document.createElement("select");
            for (const o of trait.get("options") || []) {
                el.add(new Option(o.label, o.id));
            }
            return el;
        },
        onEvent({ elInput, component }) {
            if (elInput.value) {
                component.addAttributes({ href: elInput.value });
            }
        },
        onUpdate({ elInput, component }) {
            // links saved before the pages moved to the root (/p/slug)
            const href = (component.getAttributes().href || "").replace(/^\/p\/(?!_)/, "/");
            elInput.value = [...elInput.options].some((o) => o.value == href) ? href : "";
        },
    });
}

// --- ids of the interactive components ---------------------------------------------------------

/**
 * Gives the dropped components unique ids: targets are marked data-kit-id="name",
 * references data-kit-ref="attr:name attr:name" (dialogs, tabs, menus).
 */
export function linkIds(root) {
    const uid = Math.random().toString(36).slice(2, 7);
    const all = [root, ...root.find("[data-kit-id], [data-kit-ref]")];
    for (const c of all) {
        const a = c.getAttributes();
        if (a["data-kit-id"]) {
            c.setId(`${a["data-kit-id"]}-${uid}`);
            c.removeAttributes("data-kit-id");
        }
    }
    for (const c of all) {
        const ref = c.getAttributes()["data-kit-ref"];
        if (ref) {
            const attrs = {};
            for (const pair of ref.split(/\s+/)) {
                const [attr, name] = pair.split(":");
                if (attr && name) {
                    attrs[attr] = `${name}-${uid}`;
                }
            }
            c.addAttributes(attrs);
            c.removeAttributes("data-kit-ref");
        }
    }
}

// --- blocks -----------------------------------------------------------------------------------

const muted = "color:var(--muted-foreground)";
const container = "max-width:1120px;margin:0 auto;padding:0 24px";
const placeholderImage = "data:image/svg+xml," + encodeURIComponent(
    `<svg xmlns="http://www.w3.org/2000/svg" width="640" height="360" viewBox="0 0 640 360"><rect width="640" height="360" fill="#e9ecef"/><path d="M260 230l40-50 30 36 20-24 40 38z" fill="#adb5bd"/><circle cx="280" cy="144" r="18" fill="#adb5bd"/></svg>`,
);

/** Switches the light and dark mode on the published page (the choice is remembered). */
const themeToggle = `<button type="button" class="btn" data-variant="ghost" data-size="icon" data-theme-toggle="" aria-label="Switch light / dark mode">${svg("sun", 16, 'class="icon-sun"')}${svg("moon", 16, 'class="icon-moon"')}</button>`;

const buttonHTML = (label, variant = "", extra = "") => `<a href="#" class="btn"${variant ? ` data-variant="${variant}"` : ""}${extra}>${label}</a>`;

const featureCard = (ic, title, text) => `<div class="card" style="padding:24px;gap:12px">
<div style="width:40px;height:40px;border-radius:var(--radius);background:var(--muted);display:flex;align-items:center;justify-content:center">${icon(ic, 20)}</div>
<h3 style="margin:0;font-size:18px;font-weight:600">${title}</h3><p style="margin:0;${muted}">${text}</p></div>`;

const priceCard = (name, price, items, popular) => `<div class="card" style="padding:24px;gap:16px${popular ? ";border-color:var(--primary);box-shadow:0 0 0 1px var(--primary)" : ""}">
<header style="gap:6px"><h3 style="margin:0;font-size:18px;font-weight:600">${name}${popular ? ' <span class="badge" style="margin-left:6px">Popular</span>' : ""}</h3>
<p style="margin:0"><span style="font-size:36px;font-weight:700">${price}</span><span style="${muted}"> / month</span></p></header>
<ul style="list-style:none;margin:0;padding:0;display:flex;flex-direction:column;gap:8px">${items.map((i) => `<li style="display:flex;gap:8px;align-items:center">${icon("check")}<span>${i}</span></li>`).join("")}</ul>
<footer>${buttonHTML("Choose " + name, popular ? "" : "outline", ' style="width:100%"')}</footer></div>`;

const testimonial = (text, name, role, initials) => `<figure class="card" style="padding:24px;gap:16px;margin:0">
<div style="${muted}">${icon("quote", 20)}</div><blockquote style="margin:0;font-size:16px">${text}</blockquote>
<figcaption style="display:flex;align-items:center;gap:12px"><span class="avatar"><span>${initials}</span></span>
<span><strong style="display:block">${name}</strong><span style="${muted};font-size:14px">${role}</span></span></figcaption></figure>`;

const faqItem = (q, a, open = false) => `<details${open ? " open" : ""}><summary>${q}${icon("chevron")}</summary><section><p>${a}</p></section></details>`;

export const sections = {
    navbar: `<header style="border-bottom:1px solid var(--border)"><div style="${container};display:flex;align-items:center;justify-content:space-between;gap:16px;padding-top:14px;padding-bottom:14px;flex-wrap:wrap">
<a href="/" style="font-weight:700;font-size:18px;color:inherit;text-decoration:none;display:flex;align-items:center;gap:8px">${icon("sparkles", 20)}Brand</a>
<nav style="display:flex;gap:4px;flex-wrap:wrap;align-items:center"><a href="/" class="btn" data-variant="ghost">Home</a><a href="#" class="btn" data-variant="ghost">Products</a><a href="#" class="btn" data-variant="ghost">About</a><a href="#" class="btn">Contact</a>${themeToggle}</nav></div></header>`,
    hero: `<section style="padding:96px 0 80px;text-align:center"><div style="${container};display:flex;flex-direction:column;align-items:center;gap:20px">
<span class="badge" data-variant="secondary">${icon("sparkles", 12)} New: just launched</span>
<h1 style="margin:0;font-size:52px;line-height:1.05;font-weight:700;letter-spacing:-0.03em;max-width:780px">Build something people love</h1>
<p style="margin:0;font-size:19px;${muted};max-width:620px">A short sentence that explains what you offer and why it matters to the people reading this page.</p>
<div style="display:flex;gap:10px;flex-wrap:wrap;justify-content:center">${buttonHTML("Get started " + icon("arrow"), "", ' data-size="lg"')}${buttonHTML("Learn more", "outline", ' data-size="lg"')}</div></div></section>`,
    features: `<section style="padding:72px 0"><div style="${container}">
<div style="text-align:center;margin-bottom:40px"><h2 style="margin:0 0 10px;font-size:34px;font-weight:700;letter-spacing:-0.02em">Everything you need</h2><p style="margin:0;${muted};font-size:17px">Three reasons to choose us.</p></div>
<div style="display:grid;grid-template-columns:repeat(auto-fit,minmax(260px,1fr));gap:20px">${featureCard("zap", "Fast", "Pages are rendered by the server and load instantly.")}${featureCard("shield", "Secure", "Only the fields you choose are ever shown.")}${featureCard("rocket", "Simple", "Drag blocks, bind them to your data, publish.")}</div></div></section>`,
    pricing: `<section style="padding:72px 0"><div style="${container}">
<div style="text-align:center;margin-bottom:40px"><h2 style="margin:0 0 10px;font-size:34px;font-weight:700;letter-spacing:-0.02em">Simple pricing</h2><p style="margin:0;${muted};font-size:17px">Pick the plan that fits you.</p></div>
<div style="display:grid;grid-template-columns:repeat(auto-fit,minmax(260px,1fr));gap:20px;align-items:start">${priceCard("Starter", "$0", ["1 project", "Community support", "Basic analytics"])}${priceCard("Pro", "$19", ["Unlimited projects", "Priority support", "Advanced analytics"], true)}${priceCard("Team", "$49", ["Everything in Pro", "5 team members", "Shared workspaces"])}</div></div></section>`,
    testimonials: `<section style="padding:72px 0;background:var(--muted)"><div style="${container}">
<h2 style="margin:0 0 32px;text-align:center;font-size:34px;font-weight:700;letter-spacing:-0.02em">What people say</h2>
<div style="display:grid;grid-template-columns:repeat(auto-fit,minmax(260px,1fr));gap:20px">${testimonial("It changed the way we work. Setting it up took an afternoon.", "Alex Martin", "Founder, Acme", "AM")}${testimonial("Clear, fast and pleasant to use. Our customers noticed.", "Sam Lee", "Designer", "SL")}${testimonial("The best decision we made this year.", "Jo Rivera", "Operations", "JR")}</div></div></section>`,
    faq: `<section style="padding:72px 0"><div style="max-width:760px;margin:0 auto;padding:0 24px">
<h2 style="margin:0 0 24px;text-align:center;font-size:34px;font-weight:700;letter-spacing:-0.02em">Frequently asked questions</h2>
<section class="accordion">${faqItem("How does it work?", "Explain it in one or two sentences.", true)}${faqItem("Can I cancel anytime?", "Yes. Tell people what happens next.")}${faqItem("Do you offer support?", "Describe how people can reach you.")}${faqItem("Is my data safe?", "Explain how you protect it.")}</section></div></section>`,
    cta: `<section style="padding:72px 0"><div style="${container}"><div class="card" style="padding:48px 32px;text-align:center;align-items:center;gap:16px;background:var(--primary);color:var(--primary-foreground)">
<h2 style="margin:0;font-size:32px;font-weight:700;letter-spacing:-0.02em">Ready to get started?</h2><p style="margin:0;opacity:.85;font-size:17px">Join the people already using it.</p>
${buttonHTML("Start now " + icon("arrow"), "secondary", ' data-size="lg"')}</div></div></section>`,
    footer: `<footer style="border-top:1px solid var(--border);padding:32px 0"><div style="${container};display:flex;justify-content:space-between;gap:16px;flex-wrap:wrap;align-items:center;${muted};font-size:14px">
<span>© 2026 Brand. All rights reserved.</span><nav style="display:flex;gap:16px"><a href="#" style="color:inherit">Privacy</a><a href="#" style="color:inherit">Terms</a><a href="#" style="color:inherit">Contact</a></nav></div></footer>`,
};

const sidebarLink = (ic, label, href = "#") => `<li><a href="${href}">${icon(ic)}<span>${label}</span></a></li>`;

const sidebarAside = `<aside class="sidebar" data-kit-id="sidebar"><nav aria-label="Sidebar navigation">
<header><a href="/" style="display:flex;align-items:center;gap:8px;padding:8px;font-weight:600;color:inherit;text-decoration:none">${icon("sparkles", 20)}<span>Brand</span></a></header>
<section class="scrollbar">
<div role="group" data-kit-ref="aria-labelledby:nav-main"><h3 data-kit-id="nav-main">Menu</h3><ul>${sidebarLink("house", "Home", "/")}${sidebarLink("search", "Browse")}${sidebarLink("calendar", "Events")}${sidebarLink("mail", "Contact")}</ul></div>
<div role="group" data-kit-ref="aria-labelledby:nav-more"><h3 data-kit-id="nav-more">More</h3><ul>${sidebarLink("info", "About")}${sidebarLink("settings", "Settings")}</ul></div>
</section>
<footer><a href="#" class="btn" data-variant="outline">${icon("log-in")} Sign in</a></footer></nav></aside>`;

/** The content beside a sidebar; its toggle opens the sidebar of the page (or the one with the ref's id). */
const sidebarMain = (title, content, toggle = ' data-kit-ref="data-sidebar-toggle:sidebar"') => `<main style="min-height:100vh;display:flex;flex-direction:column">
<header style="position:sticky;top:0;z-index:10;display:flex;align-items:center;gap:8px;padding:10px 16px;border-bottom:1px solid var(--border);background:var(--background)">
<button type="button" class="btn" data-variant="ghost" data-size="icon"${toggle} aria-label="Show / hide the menu">${icon("panel-left")}</button>
<span style="font-weight:600">${title}</span><span style="flex:1"></span>${themeToggle}</header>
<div style="padding:24px;display:flex;flex-direction:column;gap:24px;flex:1">${content}</div></main>`;

/** A sidebar beside the main content (Basecoat styles the element just after the sidebar). */
const sidebarLayout = (title, content) => `<div style="min-height:100vh">${sidebarAside}${sidebarMain(title, content)}</div>`;

const sidebarContent = `<div><h1 style="margin:0 0 6px;font-size:30px;font-weight:700;letter-spacing:-0.02em">Welcome back</h1><p style="margin:0;${muted}">Here is what happened since your last visit.</p></div>
<div style="display:grid;grid-template-columns:repeat(auto-fit,minmax(220px,1fr));gap:16px">${[["Visitors", "1,284", "+12% this month"], ["Orders", "86", "+4 today"], ["Revenue", "$4,120", "+8% this month"]]
    .map(([k, v, d]) => `<div class="card" style="padding:20px;gap:6px"><p style="margin:0;${muted};font-size:14px">${k}</p><p style="margin:0;font-size:28px;font-weight:700">${v}</p><p style="margin:0;${muted};font-size:13px">${d}</p></div>`)
    .join("")}</div>`;

const components = {
    button: buttonHTML("Button"),
    "button-icon": buttonHTML(`${icon("download")} Download`),
    "button-icon-after": buttonHTML(`Continue ${icon("arrow-right")}`, "secondary"),
    "button-icon-only": `<button type="button" class="btn" data-variant="outline" data-size="icon" aria-label="Settings">${icon("settings")}</button>`,
    "button-variants": `<div style="display:flex;gap:8px;flex-wrap:wrap;align-items:center">${buttonHTML("Primary")}${buttonHTML("Secondary", "secondary")}${buttonHTML("Outline", "outline")}${buttonHTML("Ghost", "ghost")}${buttonHTML("Link", "link")}${buttonHTML(`${icon("trash-2")} Delete`, "destructive")}</div>`,
    "button-group": `<div class="button-group" role="group" aria-label="Actions">${buttonHTML(`${icon("arrow-left")} Previous`, "outline")}${buttonHTML("Today", "outline")}${buttonHTML(`Next ${icon("arrow-right")}`, "outline")}</div>`,
    "theme-toggle": themeToggle,
    badge: `<span class="badge">Badge</span>`,
    alert: `<div class="alert">${icon("info")}<h2>Heads up!</h2><section>You can add components to your page with the blocks panel.</section></div>`,
    "alert-destructive": `<div class="alert" data-variant="destructive">${icon("alert")}<h2>Something went wrong</h2><section>Describe the problem and how to fix it.</section></div>`,
    card: `<div class="card" style="max-width:380px"><header><h2>Card title</h2><p>Card description.</p></header><section><p style="margin:0">The content of the card.</p></section><footer style="gap:8px">${buttonHTML("Action")}${buttonHTML("Cancel", "outline")}</footer></div>`,
    avatar: `<span class="avatar"><span>AB</span></span>`,
    table: `<div class="table-container"><table class="table"><thead><tr><th>Name</th><th>Status</th><th style="text-align:right">Amount</th></tr></thead><tbody><tr><td>Invoice #001</td><td><span class="badge" data-variant="secondary">Paid</span></td><td style="text-align:right">$250.00</td></tr><tr><td>Invoice #002</td><td><span class="badge" data-variant="outline">Pending</span></td><td style="text-align:right">$150.00</td></tr></tbody></table></div>`,
    accordion: `<section class="accordion">${faqItem("First question", "The answer.", true)}${faqItem("Second question", "The answer.")}${faqItem("Third question", "The answer.")}</section>`,
    breadcrumb: `<nav class="breadcrumb" aria-label="Breadcrumb"><ol><li><a href="/">Home</a></li><li aria-hidden="true">/</li><li><a href="#">Section</a></li><li aria-hidden="true">/</li><li><span aria-current="page">Page</span></li></ol></nav>`,
    separator: `<hr style="border:0;border-top:1px solid var(--border);margin:24px 0">`,
    kbd: `<kbd class="kbd">⌘ K</kbd>`,
    progress: `<div class="progress" role="progressbar" aria-valuenow="60" aria-valuemin="0" aria-valuemax="100" style="height:8px;border-radius:999px;background:var(--muted)"><span style="width:60%;background:var(--primary)"></span></div>`,
    skeleton: `<div style="display:flex;gap:12px;align-items:center"><div class="skeleton" style="width:48px;height:48px;border-radius:999px"></div><div style="display:flex;flex-direction:column;gap:8px"><div class="skeleton" style="width:220px;height:14px"></div><div class="skeleton" style="width:160px;height:14px"></div></div></div>`,
    empty: `<div class="empty"><header><figure>${icon("sparkles", 24)}</figure><h3>Nothing here yet</h3><p>Explain what will appear here.</p></header><section>${buttonHTML("Get started")}</section></div>`,
    items: `<div class="item-group">${["First item", "Second item"].map((x) => `<div class="item" data-variant="outline"><section><h3>${x}</h3><p>A short description.</p></section><aside>${buttonHTML("Open", "outline", ' data-size="sm"')}</aside></div>`).join("")}</div>`,
    tooltip: buttonHTML("Hover me", "outline", ' data-tooltip="A helpful hint"'),
    tabs: `<div class="tabs" style="max-width:520px"><nav role="tablist" aria-orientation="horizontal">
<button type="button" role="tab" data-kit-id="tab1" data-kit-ref="aria-controls:panel1" aria-selected="true" tabindex="0">Account</button>
<button type="button" role="tab" data-kit-id="tab2" data-kit-ref="aria-controls:panel2" aria-selected="false" tabindex="-1">Password</button>
<button type="button" role="tab" data-kit-id="tab3" data-kit-ref="aria-controls:panel3" aria-selected="false" tabindex="-1">Billing</button></nav>
<div role="tabpanel" data-kit-id="panel1" data-kit-ref="aria-labelledby:tab1" tabindex="-1" aria-selected="true"><div class="card" style="padding:20px"><p style="margin:0">Content of the first tab.</p></div></div>
<div role="tabpanel" data-kit-id="panel2" data-kit-ref="aria-labelledby:tab2" tabindex="-1" aria-selected="false" hidden><div class="card" style="padding:20px"><p style="margin:0">Content of the second tab.</p></div></div>
<div role="tabpanel" data-kit-id="panel3" data-kit-ref="aria-labelledby:tab3" tabindex="-1" aria-selected="false" hidden><div class="card" style="padding:20px"><p style="margin:0">Content of the third tab.</p></div></div></div>`,
    dropdown: `<div class="dropdown-menu"><button type="button" class="btn" data-variant="outline" data-kit-id="menu-trigger" data-kit-ref="aria-controls:menu" aria-haspopup="menu" aria-expanded="false">Options ${icon("chevron")}</button>
<div data-popover aria-hidden="true" data-kit-id="menu-popover"><div role="menu" data-kit-id="menu" data-kit-ref="aria-labelledby:menu-trigger">
<div role="menuitem">Profile</div><div role="menuitem">Settings</div><hr role="separator"><div role="menuitem">Sign out</div></div></div></div>`,
    popover: `<div class="popover"><button type="button" class="btn" data-variant="outline" data-kit-id="pop-trigger" data-kit-ref="aria-controls:pop" aria-expanded="false">Open popover</button>
<div data-popover aria-hidden="true" data-kit-id="pop" style="padding:16px;width:280px"><h4 style="margin:0 0 6px;font-weight:600">Popover title</h4><p style="margin:0;font-size:14px;${muted}">Some content in a popover.</p></div></div>`,
    // dialogs open with the browser's invoker commands: no script needed
    dialog: `<div><button type="button" class="btn" data-variant="outline" command="show-modal" data-kit-ref="commandfor:dialog">Open dialog</button>
<dialog class="dialog" closedby="any" data-kit-id="dialog"><div><header><h2>Dialog title</h2><p>A short description of the dialog.</p></header><section><p>The content of the dialog.</p></section>
<footer>${buttonHTML("Continue")}<button type="button" class="btn" data-variant="outline" command="close" data-kit-ref="commandfor:dialog">Cancel</button></footer>
<button type="button" class="btn" data-variant="ghost" data-size="icon-sm" aria-label="Close dialog" command="close" data-kit-ref="commandfor:dialog">${icon("x")}</button></div></dialog></div>`,
};

// data blocks: Basecoat markup bound to collections (see builder.js: pb-list, pb-field...)
const dataBlocks = {
    "record-cards": `<div data-pb-list="" data-pb-limit="12" style="display:grid;grid-template-columns:repeat(auto-fill,minmax(260px,1fr));gap:20px;padding:24px">
<article class="card" data-pb-item style="overflow:hidden;padding-top:0">
<img data-pb-field="" src="${placeholderImage}" alt="" style="width:100%;height:180px;object-fit:cover;display:block">
<header><h2 data-pb-field="">{field}</h2><p data-pb-field="">{field}</p></header>
<footer><a href="#" class="btn" data-variant="outline" data-pb-link="">View details</a></footer></article>
<div class="empty" data-pb-empty><header><h3>Nothing to show yet</h3><p>Records will appear here.</p></header></div></div>`,
    "record-table": `<div class="table-container"><table class="table"><thead><tr><th>Name</th><th>Field</th><th>Field</th></tr></thead>
<tbody data-pb-list="" data-pb-limit="20"><tr data-pb-item><td><span data-pb-field="">{field}</span></td><td><span data-pb-field="">{field}</span></td><td><span data-pb-field="">{field}</span></td></tr>
<tr data-pb-empty><td colspan="3" style="text-align:center;${muted}">No records yet.</td></tr></tbody></table></div>`,
    "record-accordion": `<section class="accordion" data-pb-list="" data-pb-limit="20"><details data-pb-item><summary><span data-pb-field="">{field}</span>${icon("chevron")}</summary><section><p data-pb-field="">{field}</p></section></details>
<div class="empty" data-pb-empty><header><h3>No questions yet</h3></header></div></section>`,
    "record-items": `<div class="item-group" data-pb-list="" data-pb-limit="12"><div class="item" data-variant="outline" data-pb-item><figure><img data-pb-field="" src="${placeholderImage}" alt="" style="width:48px;height:48px;object-fit:cover;border-radius:var(--radius)"></figure>
<section><h3 data-pb-field="">{field}</h3><p data-pb-field="">{field}</p></section><aside><a href="#" class="btn" data-variant="outline" data-size="sm" data-pb-link="">Open</a></aside></div>
<div class="empty" data-pb-empty><header><h3>Nothing to show yet</h3></header></div></div>`,
    "record-avatar": `<span class="avatar" data-size="lg"><img data-pb-field="" src="${placeholderImage}" alt=""></span>`,
};

const blockIcon = (name) => `<i class="${name} gpp-block-icon"></i>`;
const blockList = [
    ["Components", "kit-button", "Button", "ri-checkbox-blank-line", components.button],
    ["Components", "kit-button-icon", "Button + icon", "ri-download-2-line", components["button-icon"]],
    ["Components", "kit-button-icon-after", "Button + icon after", "ri-arrow-right-line", components["button-icon-after"]],
    ["Components", "kit-button-icon-only", "Icon button", "ri-settings-3-line", components["button-icon-only"]],
    ["Components", "kit-button-variants", "Button variants", "ri-layout-row-line", components["button-variants"]],
    ["Components", "kit-button-group", "Button group", "ri-layout-column-line", components["button-group"]],
    ["Components", "kit-theme-toggle", "Light / dark toggle", "ri-contrast-2-line", components["theme-toggle"]],
    ["Components", "kit-badge", "Badge", "ri-price-tag-3-line", components.badge],
    ["Components", "kit-alert", "Alert", "ri-information-line", components.alert],
    ["Components", "kit-alert-destructive", "Alert (error)", "ri-error-warning-line", components["alert-destructive"]],
    ["Components", "kit-card", "Card", "ri-bank-card-line", components.card],
    ["Components", "kit-avatar", "Avatar", "ri-user-3-line", components.avatar],
    ["Components", "kit-table", "Table", "ri-table-line", components.table],
    ["Components", "kit-accordion", "Accordion", "ri-list-check-3", components.accordion],
    ["Components", "kit-tabs", "Tabs", "ri-folders-line", components.tabs],
    ["Components", "kit-dialog", "Dialog", "ri-window-line", components.dialog],
    ["Components", "kit-dropdown", "Dropdown menu", "ri-menu-line", components.dropdown],
    ["Components", "kit-popover", "Popover", "ri-chat-1-line", components.popover],
    ["Components", "kit-tooltip", "Tooltip", "ri-question-line", components.tooltip],
    ["Components", "kit-breadcrumb", "Breadcrumb", "ri-arrow-right-s-line", components.breadcrumb],
    ["Components", "kit-items", "Item list", "ri-list-unordered", components.items],
    ["Components", "kit-empty", "Empty state", "ri-inbox-line", components.empty],
    ["Components", "kit-progress", "Progress", "ri-loader-2-line", components.progress],
    ["Components", "kit-skeleton", "Skeleton", "ri-layout-masonry-line", components.skeleton],
    ["Components", "kit-kbd", "Keyboard key", "ri-keyboard-line", components.kbd],
    ["Components", "kit-separator", "Separator", "ri-separator", components.separator],
    ["Sections", "kit-navbar", "Navbar", "ri-layout-top-2-line", sections.navbar],
    ["Sections", "kit-hero", "Hero", "ri-layout-top-line", sections.hero],
    ["Sections", "kit-features", "Features", "ri-layout-grid-line", sections.features],
    ["Sections", "kit-pricing", "Pricing", "ri-price-tag-line", sections.pricing],
    ["Sections", "kit-testimonials", "Testimonials", "ri-chat-quote-line", sections.testimonials],
    ["Sections", "kit-faq", "FAQ", "ri-question-answer-line", sections.faq],
    ["Sections", "kit-cta", "Call to action", "ri-megaphone-line", sections.cta],
    ["Sections", "kit-footer", "Footer", "ri-layout-bottom-line", sections.footer],
    ["Sections", "kit-sidebar", "Sidebar layout", "ri-layout-left-line", sidebarLayout("Dashboard", sidebarContent)],
    ["Data", "kit-record-cards", "Record cards", "ri-gallery-view-2", dataBlocks["record-cards"]],
    ["Data", "kit-record-table", "Record table", "ri-table-2", dataBlocks["record-table"]],
    ["Data", "kit-record-accordion", "Record FAQ", "ri-list-check-2", dataBlocks["record-accordion"]],
    ["Data", "kit-record-items", "Record items", "ri-list-view", dataBlocks["record-items"]],
    ["Data", "kit-record-avatar", "Avatar field", "ri-user-smile-line", dataBlocks["record-avatar"]],
];

/** Adds or removes the Basecoat blocks. */
export function setKitBlocks(ed, enabled) {
    const bm = ed.BlockManager;
    for (const [category, id, label, ic, content] of blockList) {
        if (!enabled) {
            bm.remove(id);
        } else if (!bm.get(id)) {
            bm.add(id, { category: { id: category, label: category, open: category != "Sections" }, label, media: blockIcon(ic), content });
        }
    }
}

// --- page templates -----------------------------------------------------------------------------

const catalog = `<section style="padding:56px 0 8px"><div style="${container}"><h1 style="margin:0 0 8px;font-size:40px;font-weight:700;letter-spacing:-0.02em">Our catalog</h1>
<p style="margin:0;${muted};font-size:17px">Pick a collection in the settings of the list below.</p></div></section>`;

const recordDetail = `<section style="padding:56px 0"><div style="max-width:880px;margin:0 auto;padding:0 24px;display:flex;flex-direction:column;gap:20px">
<nav class="breadcrumb" aria-label="Breadcrumb"><ol><li><a href="/">Home</a></li><li aria-hidden="true">/</li><li><span aria-current="page" data-pb-field="">{field}</span></li></ol></nav>
<h1 data-pb-field="" style="margin:0;font-size:40px;font-weight:700;letter-spacing:-0.02em">{field}</h1>
<div class="card" style="padding:24px"><p data-pb-field="" style="margin:0">{field}</p></div>
<div>${buttonHTML("Contact us")}</div></div></section>`;

/** Template HTML: the data-kit-id/ref markers as plain ids (one copy per page). */
const fixedIds = (html) =>
    html
        .replace(/data-kit-id="([^"]+)"/g, 'id="$1"')
        .replace(/data-kit-ref="([^"]+)"/g, (_, ref) => ref.split(/\s+/).map((p) => p.split(":")).map(([a, n]) => `${a}="${n}"`).join(" "));

const header = (o) => (o.header ? part(o.header) : sections.navbar);
const footer = (o) => (o.footer ? part(o.footer) : sections.footer);

/** The placeholder of a site part. */
const part = (slug) => `<div data-pb-part="${slug}"></div>`;

/**
 * New page templates ("html" is null for the classic blank page). html(o):
 * o.header, o.footer, o.sidebar are the slugs of the site parts to use instead
 * of new copies of these sections (when the site has them).
 */
export const templates = [
    { id: "blank", label: "Blank (Basecoat UI)", kind: "page", kit: true, html: () => `<section style="padding:64px 24px;text-align:center"><h1 style="margin:0 0 12px;font-size:40px;font-weight:700">Untitled</h1><p style="margin:0;${muted}">Drag blocks from the right panel.</p></section>` },
    { id: "landing", label: "Landing page", kind: "page", kit: true, parts: ["header", "footer"], html: (o = {}) => header(o) + sections.hero + sections.features + sections.testimonials + sections.faq + sections.cta + footer(o) },
    { id: "catalog", label: "Catalog (record cards)", kind: "page", kit: true, parts: ["header", "footer"], html: (o = {}) => header(o) + catalog + `<section><div style="${container}">${dataBlocks["record-cards"]}</div></section>` + footer(o) },
    {
        id: "sidebar",
        label: "App with sidebar",
        kind: "page",
        kit: true,
        parts: ["sidebar"],
        html: (o = {}) => (o.sidebar ? part(o.sidebar) + fixedIds(sidebarMain("Dashboard", sidebarContent, ' data-sidebar-toggle=""')) : fixedIds(sidebarLayout("Dashboard", sidebarContent))),
    },
    { id: "detail", label: "Record detail", kind: "record", kit: true, parts: ["header", "footer"], html: (o = {}) => header(o) + recordDetail + footer(o) },
    // site parts
    { id: "part-header", label: "Header (navbar)", kind: "part", kit: true, html: () => sections.navbar },
    { id: "part-footer", label: "Footer", kind: "part", kit: true, html: () => sections.footer },
    { id: "part-sidebar", label: "Sidebar (menu beside the content)", kind: "part", kit: true, html: () => fixedIds(sidebarAside) },
    { id: "part-blank", label: "Blank (Basecoat UI)", kind: "part", kit: true, html: () => `<section style="padding:24px"><p style="margin:0">Drag blocks here.</p></section>` },
    { id: "classic", label: "Blank without kit (classic)", kind: "any", kit: false, html: null },
];

// --- canvas -----------------------------------------------------------------------------------

let canvasMode = ""; // "light" or "dark": chosen with the preview button of the editor

/** Whether the canvas shows the dark mode (the site mode, auto: the system setting). */
function canvasDark(theme) {
    const mode = canvasMode || theme?.mode;
    return mode == "dark" || (mode == "auto" && matchMedia("(prefers-color-scheme: dark)").matches);
}

/** Switches the canvas between the light and dark modes (the published page follows the site theme). */
export function toggleCanvasMode(ed, enabled, theme) {
    canvasMode = canvasDark(theme) ? "light" : "dark";
    applyCanvasKit(ed, enabled, theme);
}

/** Shows the page with the kit styles and the site theme in the editor canvas. */
export function applyCanvasKit(ed, enabled, theme) {
    const doc = ed.Canvas.getDocument();
    if (!doc) {
        return;
    }
    const set = (id, href) => {
        let link = doc.getElementById(id);
        if (!href) {
            link?.remove();
            return;
        }
        if (!link) {
            link = doc.createElement("link");
            link.rel = "stylesheet";
            link.id = id;
            doc.head.prepend(link); // before the page styles: they win
        }
        if (link.getAttribute("href") != href) {
            link.setAttribute("href", href);
        }
    };
    // inserted in reverse: prepend puts the kit first, then the theme
    set("gpp-kit-theme", enabled ? `/p/_theme.css?v=${encodeURIComponent(theme?.updated || "")}` : "");
    set("gpp-kit-style", enabled ? styleURL(theme?.style) : "");
    doc.documentElement.classList.toggle("dark", !!(enabled && canvasDark(theme)));
}
