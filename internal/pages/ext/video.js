// Pages builder: the Video block. The page keeps a placeholder
// (data-pb-video="<link>" and its options); the server draws the player
// (internal/pages/video.go). The canvas shows a preview of the poster.

const esc = (v) => String(v ?? "").replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[c]);

const ratios = [
    ["16/9", "16:9"],
    ["4/3", "4:3"],
    ["1/1", "1:1"],
    ["9/16", "9:16"],
    ["21/9", "21:9"],
    ["auto", "Auto"],
];

const fileExt = /\.(mp4|webm|ogv|ogg|mov|m4v)$/i;
const isFile = (link) => {
    try {
        const u = new URL(link, location.origin);
        return fileExt.test(u.pathname) || u.pathname.startsWith("/media/");
    } catch {
        return false;
    }
};

/** The service of a link and what the canvas can show (the server checks it again). */
export function videoInfo(link) {
    link = String(link || "").trim();
    if (!link) {
        return { kind: "empty" };
    }
    if (link.includes("{{")) {
        return { kind: "formula" };
    }
    if (isFile(link)) {
        return { kind: "file", provider: "Video file" };
    }
    let u;
    try {
        u = new URL(link);
    } catch {
        return { kind: "bad" };
    }
    if (!/^https?:$/.test(u.protocol)) {
        return { kind: "bad" };
    }
    const host = u.hostname.toLowerCase().replace(/^(www|m)\./, "");
    const parts = u.pathname.split("/").filter(Boolean);
    if (["youtube.com", "youtu.be", "youtube-nocookie.com", "music.youtube.com"].includes(host)) {
        const id = host == "youtu.be" ? parts[0] : parts[0] == "watch" ? u.searchParams.get("v") : ["embed", "shorts", "live", "v"].includes(parts[0]) ? parts[1] : "";
        return /^[\w-]{6,20}$/.test(id || "") ? { kind: "hosted", provider: "YouTube", thumb: `https://i.ytimg.com/vi/${id}/hqdefault.jpg` } : { kind: "bad" };
    }
    if (host == "vimeo.com" || host == "player.vimeo.com") {
        return parts.some((p) => /^\d{3,12}$/.test(p)) ? { kind: "hosted", provider: "Vimeo" } : { kind: "bad" };
    }
    if (host == "loom.com") {
        return ["share", "embed"].includes(parts[0]) && parts[1] ? { kind: "hosted", provider: "Loom" } : { kind: "bad" };
    }
    if (["dailymotion.com", "dai.ly", "geo.dailymotion.com"].includes(host)) {
        let id = host == "dai.ly" ? parts[0] : parts[0] == "video" ? parts[1] : parts[0] == "embed" ? parts[2] : u.searchParams.get("video");
        id = (id || "").split("_")[0];
        return /^[\w-]{3,64}$/.test(id) ? { kind: "hosted", provider: "Dailymotion", thumb: `https://www.dailymotion.com/thumbnail/video/${id}` } : { kind: "bad" };
    }
    if (/(^|\.)wistia\.(com|net)$/.test(host)) {
        return { kind: "hosted", provider: "Wistia" };
    }
    if (host == "iframe.mediadelivery.net" || host == "player.mediadelivery.net") {
        return { kind: "hosted", provider: "Bunny Stream" };
    }
    return { kind: "bad" };
}

/** The canvas preview of a video placeholder. */
function previewHTML(a) {
    const info = videoInfo(a["data-pb-video"]);
    const title = (a["data-pb-video-title"] || "").trim();
    const poster = a["data-pb-video-poster"] || info.thumb || "";
    const label = (text) => `<span class="gp-video-label">${esc(text)}</span>`;
    switch (info.kind) {
        case "empty":
            return `<div class="gpp-video-empty"><i class="ri-video-add-line"></i><span>Video: paste a YouTube, Vimeo, Loom… link<br>or choose a file in its settings</span></div>`;
        case "bad":
            return `<div class="gpp-video-empty gpp-video-bad"><i class="ri-error-warning-line"></i><span>This link isn't a video the pages can show.<br>Check it in the settings.</span></div>`;
        case "formula":
            return `<div class="gp-video-play"><span class="gp-video-icon"></span>${label(title || "The video of the record " + a["data-pb-video"])}</div>`;
        case "file":
            return poster
                ? `<div class="gp-video-play"><img class="gp-video-poster" src="${esc(poster)}" alt=""><span class="gp-video-icon"></span>${title ? label(title) : ""}</div>`
                : `<video src="${esc(a["data-pb-video"])}" preload="metadata" muted></video>`;
        default:
            return `<div class="gp-video-play">${poster ? `<img class="gp-video-poster" src="${esc(poster)}" alt="">` : ""}<span class="gp-video-icon"></span>${title || !poster ? label(title || info.provider) : ""}</div>`;
    }
}

function el(tag, props = {}, ...children) {
    const e = Object.assign(document.createElement(tag), props);
    e.append(...children);
    return e;
}

export function defineVideo(ed) {
    ed.DomComponents.addType("pb-video", {
        isComponent: (node) => node?.nodeType == 1 && node.hasAttribute("data-pb-video"),
        model: {
            defaults: {
                name: "Video",
                tagName: "div",
                icon: '<i class="ri-video-line"></i>',
                droppable: false,
                components: [],
                attributes: { "data-pb-video": "" },
                traits: [{ type: "gpp-video", name: "gpp-video", label: false, changeProp: true }],
            },
        },
        view: {
            init() {
                this.listenTo(this.model, "change:attributes change:classes", this.drawPreview);
            },
            onRender() {
                this.drawPreview();
            },
            drawPreview() {
                const a = this.model.getAttributes();
                this.el.classList.add("gp-video");
                this.el.dataset.gpRatio = ["16/9", "4/3", "1/1", "9/16", "21/9", "auto"].includes(a["data-pb-video-ratio"]) ? a["data-pb-video-ratio"] : "16/9";
                this.el.innerHTML = previewHTML(a);
            },
        },
    });

    // the settings: the link (or a file), the shape, the poster, how it plays
    ed.Traits.addType("gpp-video", {
        createInput({ component }) {
            const set = (attrs) => {
                const add = {};
                const remove = [];
                for (const [k, v] of Object.entries(attrs)) {
                    if (v === "" || v === false || v == null) {
                        remove.push(k);
                    } else {
                        add[k] = v === true ? "true" : String(v);
                    }
                }
                remove.length && component.removeAttributes(remove);
                Object.keys(add).length && component.addAttributes(add);
            };
            const text = (cls, placeholder) => el("input", { type: "text", className: cls, placeholder, spellcheck: false });
            const check = (cls, label) => el("label", { className: "gpp-check" }, el("input", { type: "checkbox", className: cls }), el("span", { textContent: label }));
            const button = (cls, icon, label) => el("button", { type: "button", className: "gpp-video-btn " + cls }, el("i", { className: icon }), el("span", { textContent: label }));

            const link = text("gpp-video-link", "https://youtu.be/… or {{record.video}}");
            link.addEventListener("change", () => set({ "data-pb-video": link.value.trim() }));
            const fromAssets = button("gpp-video-file", "ri-gallery-line", "Video file from the Assets");
            fromAssets.hidden = !globalThis.gpMedia;
            fromAssets.addEventListener("click", async () => {
                const [a] = await globalThis.gpMedia.pick({ kind: "video", title: "Choose a video" });
                if (a) {
                    set({ "data-pb-video": a.url, "data-pb-video-title": component.getAttributes()["data-pb-video-title"] || a.alt || "" });
                }
            });
            const found = el("small", { className: "gpp-hint gpp-video-found" });

            const shapes = el("div", { className: "gpp-presets-chips gpp-video-ratios" });
            for (const [v, label] of ratios) {
                const b = el("button", { type: "button", textContent: label });
                b.dataset.value = v;
                b.addEventListener("click", () => set({ "data-pb-video-ratio": v == "16/9" ? "" : v }));
                shapes.append(b);
            }

            const poster = text("gpp-video-poster", "Poster image (YouTube: its thumbnail)");
            poster.addEventListener("change", () => set({ "data-pb-video-poster": poster.value.trim() }));
            const posterPick = button("gpp-video-poster-pick", "ri-image-line", "Poster from the Assets");
            posterPick.hidden = !globalThis.gpMedia;
            posterPick.addEventListener("click", async () => {
                const [a] = await globalThis.gpMedia.pick({ kind: "image", title: "Choose a poster" });
                if (a) {
                    set({ "data-pb-video-poster": a.url });
                }
            });

            const load = el("select", { className: "gpp-video-load" });
            load.append(el("option", { value: "", textContent: "On click (faster, nothing loaded before)" }), el("option", { value: "page", textContent: "With the page" }));
            load.addEventListener("change", () => set({ "data-pb-video-load": load.value }));

            const autoplay = check("gpp-video-autoplay", "Autoplay (muted)");
            const loop = check("gpp-video-loop", "Loop");
            const controls = check("gpp-video-controls", "Hide the controls");
            autoplay.firstChild.addEventListener("change", (e) => set({ "data-pb-video-autoplay": e.target.checked }));
            loop.firstChild.addEventListener("change", (e) => set({ "data-pb-video-loop": e.target.checked }));
            controls.firstChild.addEventListener("change", (e) => set({ "data-pb-video-controls": e.target.checked ? "false" : "" }));

            const start = text("gpp-video-start", "0:00");
            start.addEventListener("change", () => set({ "data-pb-video-start": start.value.trim() }));
            const title = text("gpp-video-title", "What the video is (screen readers, the poster)");
            title.addEventListener("change", () => set({ "data-pb-video-title": title.value.trim() }));

            const lbl = (t) => el("span", { className: "gpp-image-label", textContent: t });
            const root = el(
                "div",
                { className: "gpp-video-settings" },
                lbl("Video link"),
                link,
                found,
                fromAssets,
                lbl("Shape"),
                shapes,
                lbl("Title"),
                title,
                el("div", { className: "gpp-video-hosted" }, lbl("Player"), load),
                el("div", { className: "gpp-video-checks" }, autoplay, loop, controls),
                el("div", { className: "gpp-video-row" }, lbl("Start at"), start),
                lbl("Poster"),
                poster,
                posterPick,
            );
            this.listenTo(component, "change:attributes", () => this.onUpdate({ elInput: root, component }));
            return root;
        },
        onUpdate({ elInput, component }) {
            const a = component.getAttributes();
            const val = (sel, v) => {
                const i = elInput.querySelector(sel);
                if (document.activeElement !== i) {
                    i.value = v || "";
                }
            };
            val(".gpp-video-link", a["data-pb-video"]);
            val(".gpp-video-poster", a["data-pb-video-poster"]);
            val(".gpp-video-start", a["data-pb-video-start"]);
            val(".gpp-video-title", a["data-pb-video-title"]);
            elInput.querySelector(".gpp-video-load").value = a["data-pb-video-load"] == "page" ? "page" : "";
            elInput.querySelector(".gpp-video-autoplay").checked = a["data-pb-video-autoplay"] == "true";
            elInput.querySelector(".gpp-video-loop").checked = a["data-pb-video-loop"] == "true";
            elInput.querySelector(".gpp-video-controls").checked = a["data-pb-video-controls"] == "false";
            const ratio = a["data-pb-video-ratio"] || "16/9";
            elInput.querySelectorAll(".gpp-video-ratios button").forEach((b) => b.classList.toggle("active", b.dataset.value == ratio));

            const info = videoInfo(a["data-pb-video"]);
            const found = elInput.querySelector(".gpp-video-found");
            found.classList.toggle("gpp-warn", info.kind == "bad");
            found.textContent = {
                empty: "YouTube, Vimeo, Loom, Dailymotion, Wistia, Bunny Stream, or a video file.",
                bad: "Not a video link the pages can show.",
                formula: "Filled from the record at each visit.",
                file: "A video file: the browser's player.",
                hosted: info.provider + " video.",
            }[info.kind];
            // autoplay plays with the page; a file has no "on click" poster
            elInput.querySelector(".gpp-video-hosted").hidden = info.kind == "file" || a["data-pb-video-autoplay"] == "true";
        },
    });
}
