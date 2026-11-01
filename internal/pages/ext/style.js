// Pages builder: the style panel (layout with flex and grid, the placement of
// an element in its parent, position, size, image fit, text, effects), the
// breakpoints (with a min-width "Large screen") and the page's own CSS.
//
// GrapesJS keeps any CSS; it only shows the properties listed here.

const lengths = ["px", "%", "em", "rem", "vw", "vh"];
const sizeWords = ["auto", "none", "fit-content", "min-content", "max-content", "initial", "inherit"];

const num = (property, label, extra = {}) => ({ property, label, type: "number", units: lengths, fixedValues: sizeWords, ...extra });
const select = (property, label, options, extra = {}) => ({ property, label, type: "select", options: options.map(([id, label]) => ({ id, label })), ...extra });
// icon buttons: [value, title, icon] (no icon: the title is the label)
const radio = (property, label, options, extra = {}) => ({
    property,
    label,
    type: "radio",
    options: options.map(([id, title, icon]) => (icon ? { id, title, className: icon } : { id, label: title, title })),
    ...extra,
});
// a free value with buttons for the usual ones: [value, label]
const presets = (property, label, list, extra = {}) => ({ property, label, type: "gpp-presets", presets: list, full: true, ...extra });

// --- when a property makes sense ----------------------------------------------------------------

const styleOf = (el) => (el ? el.ownerDocument.defaultView.getComputedStyle(el) : null);
/** The display of the selected element: its value in the panel, else what the canvas computes. */
function displayOf(property, component) {
    const v = property.em?.get("StyleManager")?.getProperty("layout", "display")?.getValue();
    return v || styleOf(component?.getEl?.())?.display || "";
}
const parentDisplay = (component) => styleOf(component?.parent?.()?.getEl?.())?.display || "";
/** The selected element (the panel's target is often its CSS rule). */
const elementOf = ({ component, target, property }) => component || (target?.getEl ? target : null) || property?.em?.getSelected?.() || null;
const isFlex = (d) => d == "flex" || d == "inline-flex";
const isGrid = (d) => d == "grid" || d == "inline-grid";
const self = (test) => (o) => test(displayOf(o.property, elementOf(o)));
const inParent = (test) => (o) => test(parentDisplay(elementOf(o)));
const positioned = ({ property }) => {
    const v = property.em?.get("StyleManager")?.getProperty("position", "position")?.getValue();
    return !!v && v != "static";
};
const media = ["img", "video", "iframe", "picture", "object", "embed", "svg", "canvas"];
const isMedia = (o) => media.includes(String(elementOf(o)?.get?.("tagName") || "").toLowerCase());

// --- the sectors -----------------------------------------------------------------------------------

const align = [
    ["flex-start", "Start", "ri-align-item-left-line"],
    ["center", "Center", "ri-align-item-horizontal-center-line"],
    ["flex-end", "End", "ri-align-item-right-line"],
    ["space-between", "Space between", "ri-align-justify"],
    ["space-around", "Space around", "ri-space"],
    ["space-evenly", "Space evenly", "ri-more-line"],
];
const cross = [
    ["stretch", "Stretch", "ri-expand-height-line"],
    ["flex-start", "Top / start", "ri-align-item-top-line"],
    ["center", "Middle", "ri-align-item-vertical-center-line"],
    ["flex-end", "Bottom / end", "ri-align-item-bottom-line"],
    ["baseline", "Text baseline", "ri-text"],
];
const gridAlign = [
    ["stretch", "Stretch", "ri-expand-width-line"],
    ["start", "Start", "ri-align-item-left-line"],
    ["center", "Center", "ri-align-item-horizontal-center-line"],
    ["end", "End", "ri-align-item-right-line"],
];

export const sectors = [
    {
        id: "layout",
        name: "Layout",
        open: true,
        properties: [
            select("display", "Display", [
                ["block", "Block"],
                ["flex", "Flex (a row or a column)"],
                ["grid", "Grid (columns and rows)"],
                ["inline", "Inline"],
                ["inline-block", "Inline block"],
                ["inline-flex", "Inline flex"],
                ["inline-grid", "Inline grid"],
                ["contents", "Contents only"],
                ["none", "Hidden"],
            ], { full: true }),
            radio("flex-direction", "Direction", [
                ["row", "Row", "ri-arrow-right-line"],
                ["column", "Column", "ri-arrow-down-line"],
                ["row-reverse", "Row, reversed", "ri-arrow-left-line"],
                ["column-reverse", "Column, reversed", "ri-arrow-up-line"],
            ], { isVisible: self(isFlex) }),
            radio("flex-wrap", "Wrap", [["nowrap", "No"], ["wrap", "Yes"], ["wrap-reverse", "Reverse"]], { isVisible: self(isFlex) }),
            presets("grid-template-columns", "Columns", [
                ["1fr 1fr", "2"],
                ["repeat(3, 1fr)", "3"],
                ["repeat(4, 1fr)", "4"],
                ["1fr 2fr", "1 : 2"],
                ["2fr 1fr", "2 : 1"],
                ["1fr 3fr", "Sidebar"],
                ["repeat(auto-fill, minmax(240px, 1fr))", "Auto cards"],
            ], { placeholder: "e.g. 1fr 1fr", isVisible: self(isGrid) }),
            presets("grid-template-rows", "Rows", [
                ["auto 1fr auto", "Header, body, footer"],
                ["repeat(2, auto)", "2"],
                ["repeat(3, auto)", "3"],
            ], { placeholder: "auto (as many as needed)", isVisible: self(isGrid) }),
            radio("grid-auto-flow", "Fill order", [["row", "Rows"], ["column", "Columns"], ["row dense", "Fill gaps"]], { isVisible: self(isGrid) }),
            num("gap", "Gap", { full: true, isVisible: self((d) => isFlex(d) || isGrid(d)) }),
            num("row-gap", "Row gap", { isVisible: self((d) => isFlex(d) || isGrid(d)) }),
            num("column-gap", "Column gap", { isVisible: self((d) => isFlex(d) || isGrid(d)) }),
            radio("justify-content", "Main axis", align, { full: true, isVisible: self((d) => isFlex(d) || isGrid(d)) }),
            radio("align-items", "Cross axis", cross, { full: true, isVisible: self((d) => isFlex(d) || isGrid(d)) }),
            radio("justify-items", "Items in their cells", gridAlign, { full: true, isVisible: self(isGrid) }),
            select("align-content", "Lines", [
                ["normal", "Normal"],
                ["flex-start", "Start"],
                ["center", "Center"],
                ["flex-end", "End"],
                ["space-between", "Space between"],
                ["space-around", "Space around"],
                ["stretch", "Stretch"],
            ], { isVisible: self((d) => isFlex(d) || isGrid(d)) }),
        ],
    },
    {
        id: "item",
        name: "In its parent",
        open: true,
        properties: [
            presets("grid-column", "Column span", [["span 2", "2"], ["span 3", "3"], ["span 4", "4"], ["1 / -1", "Full row"]], { placeholder: "e.g. span 2", isVisible: inParent(isGrid) }),
            presets("grid-row", "Row span", [["span 2", "2"], ["span 3", "3"]], { placeholder: "e.g. span 2", isVisible: inParent(isGrid) }),
            num("flex-grow", "Grow", { units: [], fixedValues: [], min: 0, isVisible: inParent(isFlex) }),
            num("flex-shrink", "Shrink", { units: [], fixedValues: [], min: 0, isVisible: inParent(isFlex) }),
            num("flex-basis", "Base size", { full: true, isVisible: inParent(isFlex) }),
            radio("align-self", "Align itself", [["auto", "Auto"], ...cross], { full: true, isVisible: inParent((d) => isFlex(d) || isGrid(d)) }),
            radio("justify-self", "Justify itself", [["auto", "Auto"], ...gridAlign], { full: true, isVisible: inParent(isGrid) }),
            num("order", "Order", { units: [], fixedValues: [], isVisible: inParent((d) => isFlex(d) || isGrid(d)) }),
        ],
    },
    {
        id: "position",
        name: "Position",
        properties: [
            select("position", "Position", [
                ["static", "Normal"],
                ["relative", "Relative (moved from its place)"],
                ["absolute", "Absolute (in the positioned parent)"],
                ["fixed", "Fixed (in the window)"],
                ["sticky", "Sticky (stays on scroll)"],
            ], { full: true }),
            num("top", "Top", { isVisible: positioned }),
            num("right", "Right", { isVisible: positioned }),
            num("bottom", "Bottom", { isVisible: positioned }),
            num("left", "Left", { isVisible: positioned }),
            num("z-index", "Layer (z-index)", { units: [], fixedValues: ["auto"] }),
            radio("float", "Float", [["none", "None"], ["left", "Left", "ri-align-left"], ["right", "Right", "ri-align-right"]]),
        ],
    },
    {
        id: "size",
        name: "Size",
        properties: [
            num("width", "Width"),
            num("height", "Height"),
            num("min-width", "Min width"),
            num("min-height", "Min height"),
            num("max-width", "Max width"),
            num("max-height", "Max height"),
            presets("aspect-ratio", "Aspect ratio", [["1 / 1", "1:1"], ["4 / 3", "4:3"], ["3 / 2", "3:2"], ["16 / 9", "16:9"], ["21 / 9", "21:9"], ["9 / 16", "9:16"], ["3 / 4", "3:4"]], { placeholder: "auto" }),
            select("overflow", "Overflow", [["visible", "Visible"], ["hidden", "Hidden"], ["clip", "Clipped"], ["auto", "Scroll when needed"], ["scroll", "Scroll"]]),
            radio("box-sizing", "Size includes", [["border-box", "Padding"], ["content-box", "Content only"]]),
        ],
    },
    { id: "spacing", name: "Spacing", properties: ["margin", "padding"] },
    {
        id: "media",
        name: "Image & video",
        open: true,
        properties: [
            select("object-fit", "Fit", [
                ["fill", "Stretch to the box"],
                ["cover", "Cover (crop to fill)"],
                ["contain", "Contain (show it all)"],
                ["none", "Original size"],
                ["scale-down", "Shrink only"],
            ], { full: true, isVisible: isMedia }),
            presets("object-position", "Focus point", [
                ["center", "Center"],
                ["top", "Top"],
                ["bottom", "Bottom"],
                ["left", "Left"],
                ["right", "Right"],
                ["top left", "Top left"],
                ["top right", "Top right"],
            ], { placeholder: "e.g. 50% 30%", isVisible: isMedia }),
            radio("image-rendering", "Scaling", [["auto", "Smooth"], ["pixelated", "Pixels"]], { full: true, isVisible: isMedia }),
        ],
    },
    {
        id: "text",
        name: "Text",
        properties: [
            "font-family",
            "font-size",
            "font-weight",
            radio("font-style", "Style", [["normal", "Normal", "ri-text"], ["italic", "Italic", "ri-italic"]]),
            "line-height",
            "letter-spacing",
            "color",
            radio("text-align", "Align", [
                ["left", "Left", "ri-align-left"],
                ["center", "Center", "ri-align-center"],
                ["right", "Right", "ri-align-right"],
                ["justify", "Justify", "ri-align-justify"],
            ], { full: true }),
            radio("text-transform", "Case", [["none", "Aa"], ["uppercase", "AA"], ["lowercase", "aa"], ["capitalize", "Ab"]], { full: true }),
            radio("text-decoration", "Line", [
                ["none", "None", "ri-text"],
                ["underline", "Underline", "ri-underline"],
                ["line-through", "Strikethrough", "ri-strikethrough"],
                ["overline", "Overline", "ri-separator"],
            ], { full: true }),
            select("white-space", "Line breaks", [["normal", "Normal"], ["nowrap", "Never break"], ["pre-line", "Keep line breaks"], ["pre-wrap", "Keep spaces and breaks"]]),
            select("text-wrap", "Wrapping", [["wrap", "Normal"], ["balance", "Balanced (titles)"], ["pretty", "No lone last word"]]),
            "text-shadow",
        ],
    },
    {
        id: "background",
        name: "Background & border",
        properties: ["background-color", "background", "border", "border-radius", "box-shadow"],
    },
    {
        id: "effects",
        name: "Effects",
        properties: [
            "opacity",
            presets("filter", "Filter", [
                ["grayscale(1)", "Grayscale"],
                ["sepia(0.6)", "Sepia"],
                ["blur(4px)", "Blur"],
                ["brightness(0.8)", "Darker"],
                ["brightness(1.2)", "Lighter"],
                ["contrast(1.25)", "Contrast"],
                ["saturate(1.5)", "Vivid"],
            ], { placeholder: "none" }),
            presets("backdrop-filter", "Behind (glass)", [["blur(8px)", "Blur"], ["blur(16px) saturate(1.6)", "Frosted"]], { placeholder: "none" }),
            select("mix-blend-mode", "Blend", [["normal", "Normal"], ["multiply", "Multiply"], ["screen", "Screen"], ["overlay", "Overlay"], ["darken", "Darken"], ["lighten", "Lighten"]]),
            "cursor",
            "transition",
            "transform",
        ],
    },
];

/** The "free value plus usual values" control (gpp-presets). */
function definePresets(ed) {
    ed.StyleManager.addType("gpp-presets", {
        create({ props, change }) {
            const el = document.createElement("div");
            el.className = "gpp-presets";
            const input = Object.assign(document.createElement("input"), { type: "text", placeholder: props.placeholder || "", spellcheck: false });
            input.addEventListener("change", () => change({ value: input.value.trim() }));
            const chips = document.createElement("div");
            chips.className = "gpp-presets-chips";
            for (const [value, label] of props.presets || []) {
                const b = Object.assign(document.createElement("button"), { type: "button", textContent: label, title: value });
                b.dataset.value = value;
                // the chosen one again: removed
                b.addEventListener("click", () => change({ value: input.value.trim() == value ? "" : value }));
                chips.append(b);
            }
            el.append(input, chips);
            return el;
        },
        emit({ updateStyle }, { value }) {
            updateStyle(value);
        },
        update({ value, el }) {
            const input = el.querySelector("input");
            if (input && document.activeElement !== input) {
                input.value = value || "";
            }
            el.querySelectorAll("button").forEach((b) => b.classList.toggle("active", b.dataset.value == (value || "").trim()));
        },
    });
}

// --- breakpoints ------------------------------------------------------------------------------------

// The canvas orders the device rules as listed: the max-width ones from the
// widest, the min-width one never overlaps them.
export const devices = [
    { id: "desktop", name: "Desktop", width: "" },
    { id: "large", name: "Large screen", width: "1440px", widthMedia: "1440px", gpMinWidth: true },
    { id: "laptop", name: "Small laptop", width: "1180px", widthMedia: "1280px" },
    { id: "tablet", name: "Tablet", width: "770px", widthMedia: "992px" },
    { id: "mobile", name: "Phone", width: "375px", widthMedia: "480px" },
];

export const deviceButtons = [
    { id: "large", className: "ri-tv-2-line", title: "Large screen (1440px and wider)" },
    { id: "desktop", className: "ri-computer-line", title: "Desktop (every screen, unless styled below)", active: true },
    { id: "laptop", className: "ri-macbook-line", title: "Small laptop (1280px and narrower)" },
    { id: "tablet", className: "ri-tablet-line", title: "Tablet (992px and narrower)" },
    { id: "mobile", className: "ri-smartphone-line", title: "Phone (480px and narrower)" },
].map((b) => ({ id: b.id, className: b.className, command: (e) => e.setDevice(b.id), active: !!b.active, togglable: false, attributes: { title: b.title } }));

/** Min-width media for the "Large screen" device (GrapesJS writes max-width only). */
function allowMinWidth(ed) {
    const em = ed.getModel();
    const current = em.getCurrentMedia.bind(em);
    em.getCurrentMedia = function () {
        const d = this.getDeviceModel?.();
        if (d?.get("gpMinWidth") && d.get("widthMedia") && !this.getConfig().devicePreviewMode) {
            return `(min-width: ${d.get("widthMedia")})`;
        }
        return current();
    };
    // the canvas groups the rules by the width of their media: read min-width too
    ed.Css.render();
    const View = ed.Css.rulesView?.constructor;
    ed.Css.rulesView?.remove();
    if (View?.prototype.getMediaWidth) {
        View.prototype.getMediaWidth = function (text) {
            return text && text.replace(/^\((?:min|max)-width:\s*/, "").replace(")", "");
        };
    }
}

/** A device wider than the canvas (Large screen on a laptop): zoomed out to fit, at its real width. */
function fitWideDevices(ed) {
    const fit = () => {
        const canvas = ed.Canvas.getElement?.();
        const box = canvas?.clientWidth || 0;
        const device = ed.Devices.getSelected?.();
        const width = parseFloat(device?.get("width")) || 0;
        if (width && box && width + 40 > box) {
            // as tall as the canvas once zoomed out
            const height = Math.round(((canvas.clientHeight - 40) * (width + 40)) / box);
            ed.Canvas.getFrames().forEach((f) => f.set("height", height + "px"));
            ed.Canvas.fitViewport({ gap: 20, ignoreHeight: true });
        } else if (ed.Canvas.getZoom() != 100) {
            ed.Canvas.getFrames().forEach((f) => f.set("height", device?.get("height") || ""));
            ed.Canvas.setZoom(100);
            ed.Canvas.setCoords(0, 0);
        }
    };
    const later = () => setTimeout(fit, 400); // after the frame's resize animation
    ed.on("device:select", later);
    window.addEventListener("resize", later);
    ed.on("destroy", () => window.removeEventListener("resize", later));
}

// --- the page's own CSS -------------------------------------------------------------------------------

/** The page's own CSS (kept as written, after the CSS of the builder). */
export const customCSS = (ed) => ed?.gppCustomCSS || "";

export function setCustomCSS(ed, css) {
    ed.gppCustomCSS = String(css || "");
    applyCustomCSS(ed);
}

/** The CSS saved with the page: the builder's, then the page's own. */
export function pageCSS(ed) {
    const own = customCSS(ed).trim();
    return ed.getCss() + (own ? "\n/* custom CSS */\n" + own + "\n" : "");
}

function applyCustomCSS(ed) {
    const doc = ed.Canvas?.getDocument?.();
    if (!doc?.body) {
        return;
    }
    let el = doc.getElementById("gpp-custom-css");
    if (!el) {
        el = doc.createElement("style");
        el.id = "gpp-custom-css";
    }
    doc.body.appendChild(el); // last: over the builder's rules
    el.textContent = customCSS(ed);
}

export function customCSSModal(ed, onChange) {
    const data = store({ css: customCSS(ed) });
    function apply() {
        if (data.css != customCSS(ed)) {
            setCustomCSS(ed, data.css);
            onChange?.();
        }
        app.modals.close(modal);
    }
    const modal = t.div(
        { className: "modal popup lg gpp-css-modal" },
        t.header({ className: "modal-header" }, t.h5({ className: "m-auto" }, "Custom CSS of the page")),
        t.form(
            { id: "gppCustomCSS", className: "modal-content", onsubmit: (e) => (e.preventDefault(), apply()) },
            t.p(
                { className: "txt-hint m-t-0" },
                "Anything the style panel doesn't offer: classes, ",
                t.code(null, ":hover"),
                ", ",
                t.code(null, "@media"),
                ", ",
                t.code(null, "@keyframes"),
                "… It comes after the builder's styles, and is saved with the page. Select an element and look at its id or classes in the Layers to target it.",
            ),
            t.textarea({
                className: "gpp-css-input",
                spellcheck: false,
                autofocus: true,
                placeholder: ".hero h1 {\n  font-size: clamp(2rem, 6vw, 4rem);\n}\n\n@media (min-width: 1440px) {\n  .hero { padding: 120px 0; }\n}",
                value: () => data.css,
                oninput: (e) => (data.css = e.target.value),
                onkeydown: (e) => {
                    if (e.key == "Tab" && !e.shiftKey) {
                        e.preventDefault();
                        e.target.setRangeText("  ", e.target.selectionStart, e.target.selectionEnd, "end");
                        data.css = e.target.value;
                    } else if (e.key == "Enter" && (e.ctrlKey || e.metaKey)) {
                        e.preventDefault();
                        apply();
                    }
                },
            }),
        ),
        t.footer(
            { className: "modal-footer" },
            t.button({ type: "button", className: "btn transparent secondary m-r-auto", onclick: () => app.modals.close(modal) }, t.span({ className: "txt" }, "Cancel")),
            t.button({ type: "submit", "html-form": "gppCustomCSS", className: "btn expanded" }, t.span({ className: "txt" }, "Apply")),
        ),
    );
    modal.onafterclose = () => modal.remove();
    document.body.appendChild(modal);
    app.modals.open(modal);
}

/** The style panel, the breakpoints and the custom CSS, in a GrapesJS plugin. */
export function defineStyles(ed) {
    definePresets(ed);
    allowMinWidth(ed);
    // the panel drawn after a selection shows every property: apply their visibility again
    const sync = () =>
        setTimeout(() =>
            ed.StyleManager.getSectors().forEach((sector) => {
                sector.getProperties().forEach((p) => p.trigger("change:visible", p, p.get("visible")));
                sector.trigger("change:visible", sector, sector.get("visible"));
            }),
        );
    ed.on("command:run:open-sm run:open-sm", sync);
    fitWideDevices(ed);
    ed.on("canvas:frame:load", () => applyCustomCSS(ed));
    ed.on("load", () => applyCustomCSS(ed));
}
