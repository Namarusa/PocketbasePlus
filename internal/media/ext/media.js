// Assets: the library of files (Assets menu), and the picker the pages,
// documents and emails open to choose a file of it (gpMedia.pick).
//
// A file has a stable public address, /media/{id}/{name}: replacing the file
// keeps it, renaming it too (the name is only there to read well).

import { formatDate, handleError, select } from "../pocketbaseplus/lib.js";

const COLLECTION = "media_assets";
const FIELDS = "id,collectionId,collectionName,name,folder,alt,kind,mime,size,width,height,file,created,updated";
const MAX_BYTES = 100 * 1024 * 1024;
const VIEW_KEY = "gpMediaView";
const SORT_KEY = "gpMediaSort";

const base = app.pb.buildURL("/_/extensions/pocketbaseplus-media/");
if (!document.querySelector(`link[href="${base}media.css"]`)) {
    document.head.appendChild(t.link({ rel: "stylesheet", href: base + "media.css" }));
}

export const kinds = [
    { id: "image", label: "Images", one: "Image", icon: "ri-image-line" },
    { id: "video", label: "Videos", one: "Video", icon: "ri-film-line" },
    { id: "audio", label: "Audio", one: "Audio", icon: "ri-music-2-line" },
    { id: "pdf", label: "PDFs", one: "PDF", icon: "ri-file-pdf-2-line" },
    { id: "font", label: "Fonts", one: "Font", icon: "ri-font-size" },
    { id: "document", label: "Documents", one: "Document", icon: "ri-file-text-line" },
];
const kindOf = (id) => kinds.find((k) => k.id == id) || kinds[kinds.length - 1];
// the file chooser of an upload, by kind
const accepts = { image: "image/*", video: "video/*", audio: "audio/*", pdf: "application/pdf,.pdf", font: ".woff,.woff2,.ttf,.otf", document: "" };

const sorts = [
    { value: "-created", label: "Newest first" },
    { value: "created", label: "Oldest first" },
    { value: "name", label: "Name" },
    { value: "-size", label: "Largest first" },
];

// --- addresses ----------------------------------------------------------------------------------

const accents = { à: "a", á: "a", â: "a", ä: "a", ã: "a", å: "a", ç: "c", è: "e", é: "e", ê: "e", ë: "e", ì: "i", í: "i", î: "i", ï: "i", ñ: "n", ò: "o", ó: "o", ô: "o", ö: "o", õ: "o", ù: "u", ú: "u", û: "u", ü: "u", ý: "y", ÿ: "y", œ: "oe", æ: "ae", ß: "ss" };

/** The name in the address of a file (as the server writes it: logo-dark.png). */
export function linkName(name, file) {
    let s = String(name || "")
        .toLowerCase()
        .replace(/[àáâäãåçèéêëìíîïñòóôöõùúûüýÿœæß]/g, (c) => accents[c])
        .replace(/[^a-z0-9]+/g, "-")
        .replace(/^-+|-+$/g, "");
    if (s.length > 80) {
        s = s.slice(0, 80).replace(/-+$/, "");
    }
    const ext = (String(file || "").match(/\.[^./]+$/) || [""])[0].toLowerCase();
    return (s || "file") + ext;
}

/** The public address of a file of the library (relative). */
export const assetURL = (a) => `/media/${a.id}/${encodeURIComponent(linkName(a.name, a.file))}`;
const absolute = (url) => window.location.origin + url;

const resizable = (a) => ["image/png", "image/jpeg", "image/gif", "image/webp"].includes(a.mime);

/** What the other modules get of a file. */
export function publicAsset(a) {
    const url = assetURL(a);
    return { id: a.id, name: a.name, alt: a.alt || "", kind: a.kind, mime: a.mime, size: a.size, width: a.width, height: a.height, url, href: absolute(url) };
}

function thumbURL(a) {
    if (resizable(a)) {
        return app.pb.files.getURL(a, a.file, { thumb: "320x320f" });
    }
    return assetURL(a);
}

export function fileSize(n) {
    n = Number(n) || 0;
    if (n < 1024) {
        return n + " B";
    }
    if (n < 1024 * 1024) {
        return Math.round(n / 1024) + " KB";
    }
    return (n / 1024 / 1024).toFixed(n < 10 * 1024 * 1024 ? 1 : 0) + " MB";
}

const extension = (a) => ((a.file || "").match(/\.([^./]+)$/) || ["", ""])[1].toUpperCase();

function metaLine(a) {
    const parts = [extension(a) || kindOf(a.kind).one];
    if (a.width && a.height) {
        parts.push(`${a.width}×${a.height}`);
    }
    parts.push(fileSize(a.size));
    return parts.join(" · ");
}

async function copy(text, what = "Link") {
    try {
        await navigator.clipboard.writeText(text);
        app.toasts.success(`${what} copied.`);
    } catch {
        app.toasts.error("The clipboard isn't available: select the text and copy it.");
    }
}

// fonts: shown with their own letters
const fontsLoaded = new Set();
function fontFamily(a) {
    const family = "gpm-" + a.id + "-" + String(a.updated || "").replace(/\W/g, "");
    if (!fontsLoaded.has(family) && "FontFace" in window) {
        fontsLoaded.add(family);
        new FontFace(family, `url(${assetURL(a)})`)
            .load()
            .then((f) => document.fonts.add(f))
            .catch(() => {});
    }
    return `"${family}", var(--baseFontFamily)`;
}

/** A preview of a file: the image, the first frame of a video, the letters of a font, or its icon. */
function preview(a, large = false) {
    switch (a.kind) {
        case "image":
            return t.img({ src: large ? assetURL(a) : thumbURL(a), alt: a.alt || "", loading: "lazy", draggable: false });
        case "video":
            return large
                ? t.video({ src: assetURL(a), controls: true, preload: "metadata" })
                : [t.video({ src: assetURL(a) + "#t=0.1", muted: true, preload: "metadata", ariaHidden: true, tabIndex: -1 }), t.i({ className: "ri-play-fill gpm-play", ariaHidden: true })];
        case "audio":
            return large
                ? t.div({ className: "gpm-preview-audio" }, t.i({ className: "ri-music-2-line", ariaHidden: true }), t.audio({ src: assetURL(a), controls: true, preload: "metadata" }))
                : t.i({ className: "ri-music-2-line gpm-icon", ariaHidden: true });
        case "pdf":
            return large ? t.iframe({ src: assetURL(a), title: a.name }) : t.i({ className: "ri-file-pdf-2-line gpm-icon", ariaHidden: true });
        case "font":
            return t.div(
                { className: "gpm-font", style: `font-family:${fontFamily(a)}` },
                large ? [t.div({ className: "gpm-font-big" }, "Aa Bb Cc"), t.div(null, "The quick brown fox jumps over the lazy dog."), t.div(null, "0123456789 & ! ? @ €")] : "Aa",
            );
        default:
            return t.div({ className: "gpm-doc-icon" }, t.i({ className: "ri-file-text-line gpm-icon", ariaHidden: true }), t.span(null, extension(a) || "FILE"));
    }
}

// a short key of a content: Shablon keeps an element of the same rid with its
// old children, so a region's rid changes with what it shows
function sig(parts) {
    let h = 5381;
    const str = parts.join("\u0001");
    for (let i = 0; i < str.length; i++) {
        h = ((h << 5) + h + str.charCodeAt(i)) | 0;
    }
    return (h >>> 0).toString(36) + "-" + parts.length;
}

// --- data ---------------------------------------------------------------------------------------

async function loadAll() {
    return app.pb.collection(COLLECTION).getFullList({ sort: "-created", fields: FIELDS, requestKey: null });
}

/** The files of the library (gpMedia.list). */
export async function listAssets({ kind = "" } = {}) {
    const items = await loadAll();
    return items.filter((a) => !kind || a.kind == kind).map(publicAsset);
}

function sortItems(list, sort) {
    const out = [...list];
    switch (sort) {
        case "created":
            return out.sort((a, b) => a.created.localeCompare(b.created));
        case "name":
            return out.sort((a, b) => a.name.localeCompare(b.name, undefined, { numeric: true }));
        case "-size":
            return out.sort((a, b) => (b.size || 0) - (a.size || 0));
    }
    return out.sort((a, b) => b.created.localeCompare(a.created));
}

const folderList = (items, extra = []) => [...new Set([...items.map((a) => a.folder).filter(Boolean), ...extra])].sort((a, b) => a.localeCompare(b));

function matches(a, q) {
    q = q.trim().toLowerCase();
    return !q || [a.name, a.alt, a.folder, a.file, extension(a)].some((v) => (v || "").toLowerCase().includes(q));
}

// --- uploads ------------------------------------------------------------------------------------

// the files being sent (shown by the page and the picker)
const uploads = store({ items: [] });

function errorMessage(body, status) {
    const fieldErr = body?.data && Object.values(body.data)[0]?.message;
    return fieldErr || body?.message || `The upload failed (${status || "network error"}).`;
}

/** Sends a file (a new one, or the new file of id); progress(0..1). */
function send(file, { id = "", folder = "" } = {}, progress = () => {}) {
    return new Promise((resolve, reject) => {
        const fd = new FormData();
        fd.append("file", file);
        if (!id) {
            fd.append("folder", folder || "");
        }
        const xhr = new XMLHttpRequest();
        xhr.open(id ? "PATCH" : "POST", app.pb.buildURL(`/api/collections/${COLLECTION}/records${id ? "/" + id : ""}?fields=${FIELDS}`));
        xhr.setRequestHeader("Authorization", app.pb.authStore.token);
        xhr.upload.onprogress = (e) => e.lengthComputable && progress(e.loaded / e.total);
        xhr.onload = () => {
            let body = null;
            try {
                body = JSON.parse(xhr.responseText);
            } catch {}
            xhr.status < 300 ? resolve(body) : reject(new Error(errorMessage(body, xhr.status)));
        };
        xhr.onerror = () => reject(new Error(errorMessage(null)));
        xhr.send(fd);
    });
}

/** Uploads files to a folder (3 at a time); returns the new records. */
export async function uploadFiles(files, folder = "") {
    const list = [...files].filter((f) => f && f.size !== undefined);
    if (!list.length) {
        return [];
    }
    const jobs = list.map((f) => ({ key: Math.random().toString(36).slice(2), name: f.name, file: f, progress: 0, error: "", done: false }));
    uploads.items = [...uploads.items.filter((u) => !u.done || u.error), ...jobs];
    const items = () => uploads.items;
    const update = (key, patch) => (uploads.items = items().map((u) => (u.key == key ? { ...u, ...patch } : u)));
    const created = [];
    let next = 0;
    async function worker() {
        while (next < jobs.length) {
            const job = jobs[next++];
            if (job.file.size > MAX_BYTES) {
                update(job.key, { error: `Larger than ${MAX_BYTES / 1024 / 1024} MB.`, done: true });
                continue;
            }
            try {
                const rec = await send(job.file, { folder }, (p) => update(job.key, { progress: p }));
                created.push(rec);
                update(job.key, { progress: 1, done: true });
            } catch (err) {
                update(job.key, { error: err.message, done: true });
            }
        }
    }
    await Promise.all([worker(), worker(), worker()]);
    const failed = items().filter((u) => jobs.some((j) => j.key == u.key) && u.error).length;
    if (created.length) {
        app.toasts.success(created.length == 1 ? `Uploaded ${created[0].name}.` : `Uploaded ${created.length} files.`);
    }
    // the finished ones go after a moment (the failed ones stay until closed)
    setTimeout(() => (uploads.items = items().filter((u) => !u.done || u.error)), failed ? 0 : 1500);
    return created;
}

function uploadsPanel() {
    return () => {
        const list = uploads.items;
        if (!list.length) {
            return t.div({ rid: "noup", hidden: true });
        }
        const running = list.filter((u) => !u.done).length;
        return t.div(
            { rid: "uploads" + sig(list.map((u) => u.key + u.done + u.error + Math.round(u.progress * 20))), className: "gpm-uploads", role: "status", ariaLive: "polite" },
            t.div(
                { className: "gpm-uploads-head" },
                t.strong(null, running ? `Uploading ${running} file${running > 1 ? "s" : ""}…` : "Uploads"),
                running
                    ? null
                    : t.button(
                        { type: "button", className: "btn xs circle transparent secondary", ariaLabel: app.attrs.tooltip("Close"), onclick: () => (uploads.items = []) },
                        t.i({ className: "ri-close-line", ariaHidden: true }),
                    ),
            ),
            list.map((u) =>
                t.div(
                    { rid: u.key + u.done + u.error + Math.round(u.progress * 20), className: `gpm-upload ${u.error ? "failed" : u.done ? "done" : ""}` },
                    t.div(
                        { className: "gpm-upload-name" },
                        t.i({ className: u.error ? "ri-error-warning-line" : u.done ? "ri-check-line" : "ri-upload-2-line", ariaHidden: true }),
                        t.span({ title: u.name }, u.name),
                    ),
                    u.error ? t.div({ className: "gpm-upload-error" }, u.error) : t.div({ className: "gpm-progress" }, t.div({ style: `width:${Math.round(u.progress * 100)}%` })),
                )
            ),
        );
    };
}

// --- shared pieces ------------------------------------------------------------------------------

function uploadButton({ label = "Upload", accept = "", className = "btn expanded-lg", onfiles }) {
    return t.label(
        { className, tabIndex: 0, onkeydown: (e) => (e.key == "Enter" || e.key == " ") && (e.preventDefault(), e.currentTarget.querySelector("input").click()) },
        t.input({ type: "file", multiple: true, accept, hidden: true, onchange: (e) => (onfiles([...e.target.files]), (e.target.value = "")) }),
        t.i({ className: "ri-upload-2-line", ariaHidden: true }),
        t.span({ className: "txt" }, label),
    );
}

/** The folder of files: chips of the existing folders and a free text (a new one). */
function folderField(state, key, folders) {
    return t.div(
        null,
        t.div(
            { className: "field" },
            t.label({ htmlFor: "gpm_folder" }, "Folder"),
            t.input({ type: "text", id: "gpm_folder", maxLength: 100, placeholder: "No folder", value: () => state[key], oninput: (e) => (state[key] = e.target.value) }),
        ),
        t.div({ className: "field-help m-b-sm" }, "A new name makes the folder. Empty: no folder."),
        folders.length
            ? t.div(
                { className: "gpm-folder-chips" },
                folders.map((f) =>
                    t.button(
                        { type: "button", className: () => `label gpm-folder-chip ${state[key] == f ? "active" : ""}`, onclick: () => (state[key] = f) },
                        t.i({ className: "ri-folder-line", ariaHidden: true }),
                        f,
                    )
                ),
            )
            : null,
    );
}

function openModal(modal, onclose) {
    modal.onafterclose = () => {
        modal.remove();
        onclose?.();
    };
    document.body.appendChild(modal);
    app.modals.open(modal);
}

async function usagesOf(id) {
    try {
        return await app.pb.send(`/api/media/usages/${id}`, { requestKey: null });
    } catch (err) {
        handleError(err);
        return [];
    }
}

/** Where a record using a file is edited. */
function usageLink(u) {
    switch (u.collection) {
        case "pages":
            return "#/pages/" + u.id;
        case "pages_settings":
            return "#/pages";
        case "email_templates":
            return "#/emails/templates/" + u.id;
        case "document_templates":
            return "#/documents/" + u.id;
        case "flows":
            return "#/flows/" + u.id;
        case "forms":
            return "#/forms/" + u.id;
    }
    return `#/collections?collection=${encodeURIComponent(u.collection)}&record=${encodeURIComponent(u.id)}`;
}

const usageKind = (u) => ({ pages: "Page", pages_settings: "Site settings", email_templates: "Email template", document_templates: "Document template", flows: "Flow", forms: "Form" })[u.collection] || u.collection;

function usagesText(list) {
    const names = list.slice(0, 5).map((u) => `${usageKind(u)} “${u.label}”`);
    return names.join(", ") + (list.length > 5 ? ` and ${list.length - 5} more` : "");
}

// --- the details window -------------------------------------------------------------------------

function detailsModal(a, ctx) {
    const state = store({ name: a.name, alt: a.alt || "", folder: a.folder || "", isSaving: false, usages: null, replacing: false, progress: 0, current: a });
    usagesOf(a.id).then((list) => (state.usages = list));
    const cur = () => state.current;
    const dirty = () => state.name.trim() != cur().name || state.alt.trim() != (cur().alt || "") || state.folder.trim() != (cur().folder || "");

    async function save() {
        if (!state.name.trim()) {
            app.toasts.error("The file needs a name.");
            return;
        }
        state.isSaving = true;
        try {
            const rec = await app.pb.collection(COLLECTION).update(a.id, { name: state.name.trim(), alt: state.alt.trim(), folder: state.folder.trim() }, { fields: FIELDS });
            ctx.changed(rec);
            app.modals.close(modal);
        } catch (err) {
            handleError(err);
        }
        state.isSaving = false;
    }

    async function replace(files) {
        const f = files[0];
        if (!f) {
            return;
        }
        if (f.size > MAX_BYTES) {
            app.toasts.error(`The file is larger than ${MAX_BYTES / 1024 / 1024} MB.`);
            return;
        }
        state.replacing = true;
        state.progress = 0;
        try {
            const rec = await send(f, { id: a.id }, (p) => (state.progress = p));
            state.current = rec;
            ctx.changed(rec);
            app.toasts.success("Replaced the file: its links show the new one.");
        } catch (err) {
            app.toasts.error(err.message);
        }
        state.replacing = false;
    }

    function remove() {
        const used = state.usages || [];
        const msg = used.length
            ? `“${cur().name}” is used by ${usagesText(used)}. Delete it anyway? Their links to it will be broken.`
            : `Do you really want to delete “${cur().name}”? Its links stop working.`;
        app.modals.confirm(msg, async () => {
            try {
                await app.pb.collection(COLLECTION).delete(a.id);
                ctx.removed([a.id]);
                app.modals.close(modal);
                app.toasts.success("Deleted the file.");
            } catch (err) {
                handleError(err);
            }
        });
    }

    const sizes = () => (resizable(cur()) ? [320, 640, 1280].filter((w) => w < cur().width) : []);

    const modal = t.div(
        { className: "modal popup lg gpm-details" },
        t.header({ className: "modal-header" }, t.h5({ className: "m-auto gpm-details-title" }, () => cur().name)),
        t.form(
            { id: "gpMediaDetails", className: "modal-content", onsubmit: (e) => (e.preventDefault(), save()) },
            t.div(
                { className: "gpm-details-grid" },
                t.div(
                    { className: "gpm-details-preview" },
                    () => t.div({ rid: "pv" + cur().id + cur().updated, className: `gpm-stage gpm-stage-${cur().kind}` }, preview(cur(), true)),
                    () =>
                        state.replacing
                            ? t.div({ rid: "rp", className: "gpm-progress m-t-sm" }, t.div({ style: () => `width:${Math.round(state.progress * 100)}%` }))
                            : t.div({ rid: "norp", hidden: true }),
                    t.dl(
                        { className: "gpm-info" },
                        () => [
                            t.dt({ rid: "t1" }, "Type"),
                            t.dd({ rid: "t2" + cur().mime }, `${kindOf(cur().kind).one} · ${cur().mime || "?"}`),
                            t.dt({ rid: "s1" }, "Size"),
                            t.dd({ rid: "s2" + cur().size }, fileSize(cur().size) + (cur().width ? ` · ${cur().width} × ${cur().height} px` : "")),
                            t.dt({ rid: "u1" }, "Uploaded"),
                            t.dd({ rid: "u2" + cur().created }, formatDate(cur().created)),
                            t.dt({ rid: "m1" }, "Changed"),
                            t.dd({ rid: "m2" + cur().updated }, formatDate(cur().updated)),
                        ],
                    ),
                ),
                t.div(
                    { className: "gpm-details-form" },
                    t.div(
                        { className: "field" },
                        t.label({ htmlFor: "gpm_name" }, "Name"),
                        t.input({ type: "text", id: "gpm_name", required: true, maxLength: 200, value: () => state.name, oninput: (e) => (state.name = e.target.value) }),
                    ),
                    cur().kind == "image" || cur().kind == "video"
                        ? [
                            t.div(
                                { className: "field" },
                                t.label({ htmlFor: "gpm_alt" }, "Description (alt text)"),
                                t.textarea({ id: "gpm_alt", rows: 2, maxLength: 500, placeholder: "What the image shows, for screen readers and when it can't load", value: () => state.alt, oninput: (e) => (state.alt = e.target.value) }),
                            ),
                            t.div({ className: "field-help m-b-sm" }, "The pages and emails use it when they show the image."),
                        ]
                        : null,
                    folderField(state, "folder", ctx.folders()),
                    t.label({ className: "gp-label m-t-sm" }, "Link"),
                    t.div(
                        { className: "gpm-link" },
                        t.input({ type: "text", readOnly: true, ariaLabel: "Link", value: () => absolute(assetURL({ ...cur(), name: state.name.trim() || cur().name })), onfocus: (e) => e.target.select() }),
                        t.button(
                            { type: "button", className: "btn sm secondary", onclick: () => copy(absolute(assetURL({ ...cur(), name: state.name.trim() || cur().name }))) },
                            t.i({ className: "ri-file-copy-line", ariaHidden: true }),
                            t.span({ className: "txt" }, "Copy"),
                        ),
                    ),
                    t.div({ className: "field-help m-b-sm" }, "Anyone with the link can open the file. It stays the same when you rename or replace the file."),
                    () =>
                        sizes().length
                            ? t.div(
                                { rid: "sizes" + sizes().join(), className: "gpm-sizes m-b-sm" },
                                t.span({ className: "txt-hint txt-sm" }, "Smaller copies:"),
                                sizes().map((w) =>
                                    t.button({ type: "button", className: "label gpm-size", title: "Copy the link", onclick: () => copy(absolute(assetURL(cur()) + "?w=" + w), `${w} px link`) }, `${w} px`)
                                ),
                            )
                            : t.div({ rid: "nosizes", hidden: true }),
                    t.label({ className: "gp-label m-t-sm" }, "Used in"),
                    () => {
                        if (state.usages == null) {
                            return t.div({ rid: "uload", className: "txt-hint txt-sm" }, t.span({ className: "loader sm" }));
                        }
                        if (!state.usages.length) {
                            return t.div({ rid: "unone", className: "txt-hint txt-sm" }, "Nothing links to this file yet.");
                        }
                        return t.ul(
                            { rid: "ulist" + state.usages.length, className: "gpm-usages" },
                            state.usages.map((u) =>
                                t.li(
                                    null,
                                    t.span({ className: "label sm" }, usageKind(u)),
                                    " ",
                                    t.a({ href: usageLink(u), onclick: () => app.modals.close(modal) }, u.label),
                                    u.field && !["pages", "email_templates", "document_templates"].includes(u.collection) ? t.span({ className: "txt-hint txt-sm" }, ` · ${u.field}`) : null,
                                )
                            ),
                        );
                    },
                ),
            ),
        ),
        t.footer(
            { className: "modal-footer gpm-details-footer" },
            t.button({ type: "button", className: "btn transparent danger", onclick: remove }, t.i({ className: "ri-delete-bin-7-line", ariaHidden: true }), t.span({ className: "txt" }, "Delete")),
            t.span({ className: "m-r-auto" }),
            t.a(
                { className: "btn transparent secondary", href: () => assetURL(cur()) + "?download=1", download: "" },
                t.i({ className: "ri-download-2-line", ariaHidden: true }),
                t.span({ className: "txt" }, "Download"),
            ),
            uploadButton({ label: "Replace file", className: () => `btn secondary ${state.replacing ? "loading" : ""}`, accept: accepts[a.kind] || "", onfiles: replace }),
            t.button(
                { type: "submit", "html-form": "gpMediaDetails", className: () => `btn expanded ${state.isSaving ? "loading" : ""}`, disabled: () => state.isSaving || !dirty() },
                t.span({ className: "txt" }, "Save"),
            ),
        ),
    );
    openModal(modal);
}

// --- the library page ---------------------------------------------------------------------------

// a text field has the keyboard (not a check box)
const isTyping = (el) => !!el && ((el.tagName == "INPUT" && !["checkbox", "radio", "button", "submit", "file"].includes(el.type)) || el.tagName == "TEXTAREA" || el.isContentEditable);

export function pageAssets() {
    app.store.title = "Assets";
    const data = store({
        items: [],
        isLoading: true,
        folder: null, // null: every file, "": the files without folder
        kind: "",
        search: "",
        sort: app.utils.getLocalHistory(SORT_KEY, "-created"),
        view: app.utils.getLocalHistory(VIEW_KEY, "grid"),
        selected: [],
        extraFolders: [], // new folders, until a file goes in
        dropping: false,
        dropFolder: "\u0000", // the folder under the dragged files (sidebar)
        lastClicked: "",
    });

    async function load() {
        data.isLoading = true;
        try {
            data.items = await loadAll();
        } catch (err) {
            handleError(err);
        }
        data.isLoading = false;
    }

    const folders = () => folderList(data.items, data.extraFolders);
    const visible = () =>
        sortItems(
            data.items.filter((a) => (data.folder == null || (a.folder || "") == data.folder) && (!data.kind || a.kind == data.kind) && matches(a, data.search)),
            data.sort,
        );
    const isSel = (a) => data.selected.includes(a.id);
    const selectedItems = () => data.items.filter((a) => data.selected.includes(a.id));

    const ctx = {
        folders,
        changed(rec) {
            data.items = data.items.map((a) => (a.id == rec.id ? rec : a));
        },
        removed(ids) {
            data.items = data.items.filter((a) => !ids.includes(a.id));
            data.selected = data.selected.filter((id) => !ids.includes(id));
        },
    };

    async function upload(files, folder = data.folder || "") {
        const created = await uploadFiles(files, folder);
        if (created.length) {
            data.items = [...created, ...data.items.filter((a) => !created.some((c) => c.id == a.id))];
            data.extraFolders = data.extraFolders.filter((f) => f != folder);
        }
    }

    function toggle(a, range = false) {
        if (range && data.lastClicked) {
            const list = visible();
            const i = list.findIndex((x) => x.id == data.lastClicked);
            const j = list.findIndex((x) => x.id == a.id);
            if (i >= 0 && j >= 0) {
                const ids = list.slice(Math.min(i, j), Math.max(i, j) + 1).map((x) => x.id);
                data.selected = [...new Set([...data.selected, ...ids])];
                data.lastClicked = a.id;
                return;
            }
        }
        data.selected = isSel(a) ? data.selected.filter((id) => id != a.id) : [...data.selected, a.id];
        data.lastClicked = a.id;
    }

    async function move(list, folder) {
        folder = folder.trim();
        const todo = list.filter((a) => (a.folder || "") != folder);
        try {
            const done = await Promise.all(todo.map((a) => app.pb.collection(COLLECTION).update(a.id, { folder }, { fields: FIELDS, requestKey: null })));
            done.forEach((rec) => ctx.changed(rec));
            data.extraFolders = data.extraFolders.filter((f) => f != folder);
            if (done.length) {
                app.toasts.success(done.length == 1 ? `Moved ${done[0].name} ${folder ? "to " + folder : "out of its folder"}.` : `Moved ${done.length} files ${folder ? "to " + folder : "out of their folder"}.`);
            }
        } catch (err) {
            handleError(err);
            load();
        }
    }

    function moveModal(list) {
        const state = store({ folder: list.length == 1 ? list[0].folder || "" : data.folder || "", isSaving: false });
        const modal = t.div(
            { className: "modal popup sm" },
            t.header({ className: "modal-header" }, t.h5({ className: "m-auto" }, list.length == 1 ? `Move “${list[0].name}”` : `Move ${list.length} files`)),
            t.form(
                {
                    id: "gpMediaMove",
                    className: "modal-content",
                    onsubmit: async (e) => {
                        e.preventDefault();
                        state.isSaving = true;
                        await move(list, state.folder);
                        state.isSaving = false;
                        app.modals.close(modal);
                    },
                },
                folderField(state, "folder", folders()),
            ),
            t.footer(
                { className: "modal-footer" },
                t.button({ type: "button", className: "btn transparent secondary m-r-auto", onclick: () => app.modals.close(modal) }, t.span({ className: "txt" }, "Cancel")),
                t.button({ type: "submit", "html-form": "gpMediaMove", className: () => `btn expanded ${state.isSaving ? "loading" : ""}`, disabled: () => state.isSaving }, t.span({ className: "txt" }, "Move")),
            ),
        );
        openModal(modal);
    }

    function folderModal(current = "") {
        const state = store({ name: current, isSaving: false });
        const modal = t.div(
            { className: "modal popup sm" },
            t.header({ className: "modal-header" }, t.h5({ className: "m-auto" }, current ? "Rename the folder" : "New folder")),
            t.form(
                {
                    id: "gpMediaFolder",
                    className: "modal-content",
                    onsubmit: async (e) => {
                        e.preventDefault();
                        const name = state.name.trim().replace(/\s+/g, " ");
                        if (!current) {
                            if (name) {
                                if (!folders().includes(name)) {
                                    data.extraFolders = [...data.extraFolders, name];
                                }
                                data.folder = name;
                            }
                            app.modals.close(modal);
                            return;
                        }
                        state.isSaving = true;
                        await move(data.items.filter((a) => a.folder == current), name);
                        data.extraFolders = data.extraFolders.filter((f) => f != current);
                        if (data.folder == current) {
                            data.folder = name || null;
                        }
                        state.isSaving = false;
                        app.modals.close(modal);
                    },
                },
                t.div(
                    { className: "field" },
                    t.label({ htmlFor: "gpm_folder_name" }, "Name"),
                    t.input({ type: "text", id: "gpm_folder_name", maxLength: 100, required: !current, autofocus: true, value: () => state.name, oninput: (e) => (state.name = e.target.value) }),
                ),
                t.div({ className: "field-help" }, current ? "Empty: its files leave the folder (they aren't deleted)." : "Upload files into it, or drag files onto it in the list of folders."),
            ),
            t.footer(
                { className: "modal-footer" },
                t.button({ type: "button", className: "btn transparent secondary m-r-auto", onclick: () => app.modals.close(modal) }, t.span({ className: "txt" }, "Cancel")),
                t.button({ type: "submit", "html-form": "gpMediaFolder", className: () => `btn expanded ${state.isSaving ? "loading" : ""}`, disabled: () => state.isSaving }, t.span({ className: "txt" }, current ? "Rename" : "Create")),
            ),
        );
        openModal(modal);
    }

    async function removeSelected() {
        const list = selectedItems();
        if (!list.length) {
            return;
        }
        const usages = (await Promise.all(list.map((a) => usagesOf(a.id)))).flat();
        const what = list.length == 1 ? `“${list[0].name}”` : `these ${list.length} files`;
        const msg = usages.length
            ? `${list.length == 1 ? "It is" : "Some of them are"} used by ${usagesText(usages)}. Delete ${what} anyway? ${list.length == 1 ? "Its link" : "Their links"} will be broken.`
            : `Do you really want to delete ${what}? ${list.length == 1 ? "Its link stops" : "Their links stop"} working.`;
        app.modals.confirm(msg, async () => {
            const ok = [];
            for (const a of list) {
                try {
                    await app.pb.collection(COLLECTION).delete(a.id, { requestKey: null });
                    ok.push(a.id);
                } catch (err) {
                    handleError(err);
                }
            }
            ctx.removed(ok);
            if (ok.length) {
                app.toasts.success(ok.length == 1 ? "Deleted the file." : `Deleted ${ok.length} files.`);
            }
        });
    }

    // --- drag and drop: files from the computer (upload), cards onto a folder (move)

    const DRAG_TYPE = "application/x-gp-media";
    const hasFiles = (e) => [...(e.dataTransfer?.types || [])].includes("Files");
    const hasCards = (e) => [...(e.dataTransfer?.types || [])].includes(DRAG_TYPE);
    let dragDepth = 0;

    const dropzone = {
        ondragenter: (e) => {
            if (hasFiles(e)) {
                e.preventDefault();
                dragDepth++;
                data.dropping = true;
            }
        },
        ondragover: (e) => {
            if (hasFiles(e)) {
                e.preventDefault();
                e.dataTransfer.dropEffect = "copy";
            }
        },
        ondragleave: () => {
            dragDepth = Math.max(0, dragDepth - 1);
            if (!dragDepth) {
                data.dropping = false;
            }
        },
        ondrop: (e) => {
            dragDepth = 0;
            data.dropping = false;
            if (hasFiles(e)) {
                e.preventDefault();
                upload([...e.dataTransfer.files]);
            }
        },
    };

    function folderDrop(folder) {
        return {
            ondragover: (e) => {
                if (hasCards(e) || hasFiles(e)) {
                    e.preventDefault();
                    e.stopPropagation();
                    e.dataTransfer.dropEffect = hasCards(e) ? "move" : "copy";
                    data.dropFolder = folder;
                }
            },
            ondragleave: () => {
                if (data.dropFolder == folder) {
                    data.dropFolder = "\u0000";
                }
            },
            ondrop: (e) => {
                data.dropFolder = "\u0000";
                dragDepth = 0;
                data.dropping = false;
                if (hasCards(e)) {
                    e.preventDefault();
                    e.stopPropagation();
                    const ids = JSON.parse(e.dataTransfer.getData(DRAG_TYPE) || "[]");
                    move(data.items.filter((a) => ids.includes(a.id)), folder);
                } else if (hasFiles(e)) {
                    e.preventDefault();
                    e.stopPropagation();
                    upload([...e.dataTransfer.files], folder);
                }
            },
        };
    }

    function dragCard(e, a) {
        const ids = isSel(a) ? data.selected : [a.id];
        e.dataTransfer.setData(DRAG_TYPE, JSON.stringify(ids));
        e.dataTransfer.setData("text/uri-list", absolute(assetURL(a)));
        e.dataTransfer.effectAllowed = "copyMove";
    }

    // pasted images (a screenshot) go to the current folder
    function onPaste(e) {
        const el = document.activeElement;
        if (isTyping(el) || document.querySelector(".modal[data-modal-state=open]")) {
            return;
        }
        const files = [...(e.clipboardData?.files || [])];
        if (files.length) {
            e.preventDefault();
            upload(files.map((f, i) => (f.name && f.name != "image.png" ? f : new File([f], `pasted-${new Date().toISOString().slice(0, 19).replace(/[T:]/g, "-")}${i ? "-" + i : ""}.${(f.type.split("/")[1] || "png").replace("jpeg", "jpg")}`, { type: f.type }))));
        }
    }

    function onKeydown(e) {
        const el = document.activeElement;
        if (isTyping(el) || document.querySelector(".modal[data-modal-state=open]")) {
            return;
        }
        if (e.key == "Escape" && data.selected.length) {
            data.selected = [];
        } else if (e.key == "Delete" && data.selected.length) {
            removeSelected();
        } else if ((e.ctrlKey || e.metaKey) && e.key == "a" && visible().length) {
            e.preventDefault();
            data.selected = visible().map((a) => a.id);
        }
    }

    // --- the parts of the page

    const navItem = ({ key, label, icon, count, active, onclick, folder, edit }) =>
        t.div(
            {
                rid: `nav${key}${count}${active}`,
                className: () => `gpm-nav-row ${folder !== undefined && data.dropFolder == folder ? "drop" : ""}`,
                ...(folder !== undefined ? folderDrop(folder) : {}),
            },
            t.button(
                { type: "button", className: `nav-item gpm-nav-item ${active ? "active" : ""}`, ariaCurrent: active ? "true" : null, onclick },
                t.i({ className: icon, ariaHidden: true }),
                t.span({ className: "txt" }, label),
                t.span({ className: "gpm-count" }, String(count)),
            ),
            edit
                ? t.button({ type: "button", className: "btn xs circle transparent secondary gpm-nav-edit", ariaLabel: app.attrs.tooltip("Rename the folder"), onclick: edit }, t.i({ className: "ri-pencil-line", ariaHidden: true }))
                : null,
        );

    const sidebar = app.components.pageSidebar(
        { pbEvent: "gpMediaSidebar", className: "gp-sidebar gpm-sidebar" },
        t.nav({ className: "sidebar-content scrollable" }, () => {
            const count = (f) => data.items.filter((a) => (a.folder || "") == f).length;
            const loose = count("");
            const key = sig([String(data.folder), data.items.length, ...folders().map((f) => f + count(f))]);
            return [
                t.details(
                    { rid: "lib" + key, className: "nav-group", open: true },
                    t.summary({ tabIndex: -1, onfocusout: () => false, onclick: () => false, onkeyup: () => false }, "Library"),
                    navItem({ key: "all", label: "All files", icon: "ri-gallery-line", count: data.items.length, active: data.folder == null, onclick: () => (data.folder = null) }),
                    folders().length
                        ? navItem({ key: "loose", label: "No folder", icon: "ri-file-list-line", count: loose, active: data.folder === "", folder: "", onclick: () => (data.folder = "") })
                        : null,
                ),
                t.details(
                    { rid: "folders" + key, className: "nav-group", open: true },
                    t.summary({ tabIndex: -1, onfocusout: () => false, onclick: () => false, onkeyup: () => false }, "Folders"),
                    folders().map((f) =>
                        navItem({ key: "f" + f, label: f, icon: data.folder == f ? "ri-folder-open-line" : "ri-folder-line", count: count(f), active: data.folder == f, folder: f, onclick: () => (data.folder = f), edit: () => folderModal(f) })
                    ),
                    t.button({ type: "button", className: "nav-item gpm-nav-item gpm-new-folder", onclick: () => folderModal() }, t.i({ className: "ri-folder-add-line", ariaHidden: true }), t.span({ className: "txt" }, "New folder")),
                ),
            ];
        }),
    );

    const toolbar = t.div(
        { className: "gpm-toolbar" },
        t.div(
            { className: "field gp-bare gpm-search" },
            t.input({ type: "search", placeholder: "Search files…", ariaLabel: "Search files", value: () => data.search, oninput: (e) => (data.search = e.target.value) }),
        ),
        // the folders on phones (no sidebar)
        t.div(
            { className: "gpm-folder-select" },
            () =>
                t.div(
                    { rid: "fs" + folders().join("|") },
                    select({
                        required: true,
                        ariaLabel: "Folder",
                        options: () => [{ value: "\u0001", label: "All files" }, ...(folders().length ? [{ value: "", label: "No folder" }] : []), ...folders().map((f) => ({ value: f, label: f }))],
                        value: () => (data.folder == null ? "\u0001" : data.folder),
                        onchange: (v) => (data.folder = v == "\u0001" ? null : v),
                    }),
                ),
        ),
        t.div(
            { className: "gpm-kinds", role: "group", ariaLabel: "Kind of files" },
            [{ id: "", label: "All" }, ...kinds].map((k) =>
                t.button(
                    { type: "button", className: () => `label gpm-kind ${data.kind == k.id ? "active" : ""}`, ariaPressed: () => data.kind == k.id, onclick: () => (data.kind = k.id) },
                    k.icon ? t.i({ className: k.icon, ariaHidden: true }) : null,
                    k.label,
                )
            ),
        ),
        t.div(
            { className: "gpm-toolbar-end" },
            t.div(
                { className: "gpm-sort" },
                select({
                    required: true,
                    ariaLabel: "Sort",
                    options: sorts,
                    value: () => data.sort,
                    onchange: (v) => {
                        data.sort = v || "-created";
                        app.utils.saveLocalHistory(SORT_KEY, data.sort);
                    },
                }),
            ),
            t.div(
                { className: "gpm-views", role: "group", ariaLabel: "View" },
                [["grid", "ri-layout-grid-line", "Grid"], ["list", "ri-list-check", "List"]].map(([v, icon, label]) =>
                    t.button(
                        {
                            type: "button",
                            className: () => `btn sm circle ${data.view == v ? "secondary" : "transparent secondary"}`,
                            ariaPressed: () => data.view == v,
                            ariaLabel: app.attrs.tooltip(label),
                            onclick: () => {
                                data.view = v;
                                app.utils.saveLocalHistory(VIEW_KEY, v);
                            },
                        },
                        t.i({ className: icon, ariaHidden: true }),
                    )
                ),
            ),
        ),
    );

    const selectionBar = () =>
        data.selected.length
            ? t.div(
                { rid: "sel", className: "gpm-selection" },
                t.strong(null, () => `${data.selected.length} selected`),
                t.button({ type: "button", className: "btn sm secondary", onclick: () => moveModal(selectedItems()) }, t.i({ className: "ri-folder-transfer-line", ariaHidden: true }), t.span({ className: "txt" }, "Move")),
                t.button({ type: "button", className: "btn sm transparent danger", onclick: removeSelected }, t.i({ className: "ri-delete-bin-7-line", ariaHidden: true }), t.span({ className: "txt" }, "Delete")),
                t.span({ className: "m-r-auto" }),
                t.button({ type: "button", className: "btn sm transparent secondary", onclick: () => (data.selected = visible().map((a) => a.id)) }, t.span({ className: "txt" }, "Select all")),
                t.button({ type: "button", className: "btn sm transparent secondary", onclick: () => (data.selected = []) }, t.span({ className: "txt" }, "Clear")),
            )
            : t.div({ rid: "nosel", hidden: true });

    function card(a) {
        return t.div(
            {
                rid: a.id + a.updated,
                className: () => `gpm-card ${isSel(a) ? "selected" : ""}`,
                draggable: true,
                tabIndex: 0,
                role: "button",
                ariaLabel: `${a.name}, ${metaLine(a)}`,
                title: a.name,
                ondragstart: (e) => dragCard(e, a),
                onclick: (e) => (e.shiftKey || e.ctrlKey || e.metaKey || data.selected.length ? toggle(a, e.shiftKey) : detailsModal(a, ctx)),
                onkeydown: (e) => {
                    if (e.target != e.currentTarget) {
                        return;
                    }
                    if (e.key == "Enter") {
                        e.preventDefault();
                        detailsModal(a, ctx);
                    } else if (e.key == " ") {
                        e.preventDefault();
                        toggle(a, e.shiftKey);
                    }
                },
            },
            t.label(
                { className: "gpm-check", title: "Select", onclick: (e) => e.stopPropagation() },
                t.input({ type: "checkbox", ariaLabel: `Select ${a.name}`, checked: () => isSel(a), onclick: (e) => (e.stopPropagation(), toggle(a, e.shiftKey), (e.target.checked = isSel(a))) }),
            ),
            t.div({ className: `gpm-thumb gpm-thumb-${a.kind}` }, preview(a)),
            t.div({ className: "gpm-card-name" }, a.name),
            t.div({ className: "gpm-card-meta" }, metaLine(a)),
        );
    }

    function row(a) {
        return t.tr(
            {
                rid: a.id + a.updated,
                className: () => `gpm-row ${isSel(a) ? "selected" : ""}`,
                draggable: true,
                ondragstart: (e) => dragCard(e, a),
            },
            t.td(
                { className: "gpm-row-check" },
                t.input({ type: "checkbox", ariaLabel: `Select ${a.name}`, checked: () => isSel(a), onclick: (e) => (toggle(a, e.shiftKey), (e.target.checked = isSel(a))) }),
            ),
            t.td(
                null,
                t.button(
                    { type: "button", className: "gpm-row-name", onclick: () => detailsModal(a, ctx) },
                    t.span({ className: `gpm-row-thumb gpm-thumb-${a.kind}` }, preview(a)),
                    t.span({ className: "txt-bold" }, a.name),
                ),
            ),
            t.td({ className: "txt-hint" }, a.folder || "—"),
            t.td({ className: "txt-hint txt-nowrap" }, extension(a) || kindOf(a.kind).one),
            t.td({ className: "txt-hint txt-nowrap" }, a.width ? `${a.width} × ${a.height}` : "—"),
            t.td({ className: "txt-hint txt-nowrap" }, fileSize(a.size)),
            t.td({ className: "txt-hint txt-sm txt-nowrap" }, formatDate(a.created)),
            t.td(
                { className: "txt-right txt-nowrap" },
                t.button({ type: "button", className: "btn sm circle transparent secondary", ariaLabel: app.attrs.tooltip("Copy the link"), onclick: () => copy(absolute(assetURL(a))) }, t.i({ className: "ri-link", ariaHidden: true })),
            ),
        );
    }

    function emptyState() {
        if (data.items.length && (data.search || data.kind)) {
            return t.div(
                { rid: "nomatch", className: "gpm-empty" },
                t.i({ className: "ri-search-line", ariaHidden: true }),
                t.p(null, "No file matches."),
                t.button({ type: "button", className: "btn sm secondary", onclick: () => ((data.search = ""), (data.kind = "")) }, t.span({ className: "txt" }, "Clear the filters")),
            );
        }
        return t.div(
            { rid: "empty" + data.folder, className: "gpm-empty" },
            t.i({ className: "ri-upload-cloud-2-line", ariaHidden: true }),
            t.p({ className: "txt-bold" }, data.folder ? `The folder ${data.folder} is empty` : "No files yet"),
            t.p({ className: "txt-hint" }, "Drop files here, paste an image, or upload them: images, videos, audio, PDFs, fonts and documents, 100 MB each."),
            uploadButton({ className: "btn", onfiles: (f) => upload(f) }),
        );
    }

    const content = () => {
        if (data.isLoading && !data.items.length) {
            return t.div({ rid: "loading", className: "gpm-empty" }, t.span({ className: "loader" }));
        }
        const list = visible();
        if (!list.length) {
            return emptyState();
        }
        if (data.view == "list") {
            return t.div(
                { rid: "list" + sig(list.map((a) => a.id + a.updated)), className: "gp-table-wrap" },
                t.table(
                    { className: "gpm-table" },
                    t.thead(
                        null,
                        t.tr(
                            null,
                            t.th({ className: "gpm-row-check" }, t.input({ type: "checkbox", ariaLabel: "Select all", checked: () => list.every(isSel), onclick: (e) => (data.selected = e.target.checked ? list.map((a) => a.id) : []) })),
                            t.th(null, "Name"),
                            t.th(null, "Folder"),
                            t.th(null, "Type"),
                            t.th(null, "Size (px)"),
                            t.th(null, "Weight"),
                            t.th(null, "Uploaded"),
                            t.th(null, ""),
                        ),
                    ),
                    t.tbody(null, list.map(row)),
                ),
            );
        }
        return t.div({ rid: "grid" + sig(list.map((a) => a.id + a.updated)), className: "gpm-grid" }, list.map(card));
    };

    load();

    return t.div(
        {
            pbEvent: "gpPageAssets",
            className: "page gp-page gpm-page",
            onmount: () => {
                document.addEventListener("paste", onPaste);
                document.addEventListener("keydown", onKeydown);
            },
            onunmount: () => {
                document.removeEventListener("paste", onPaste);
                document.removeEventListener("keydown", onKeydown);
            },
        },
        sidebar,
        t.div(
            { className: "page-content full-height", ...dropzone },
            t.header(
                { className: "page-header" },
                t.nav(
                    { className: "breadcrumbs" },
                    t.div({ className: "breadcrumb-item" }, "Assets"),
                    () => t.div({ rid: "bc" + data.folder, className: "breadcrumb-item" }, data.folder == null ? "All files" : data.folder || "No folder"),
                ),
                t.div(
                    { className: "page-header-primary-btns" },
                    uploadButton({ onfiles: (f) => upload(f) }),
                ),
            ),
            t.div(
                { className: "wrapper lg m-b-base" },
                toolbar,
                selectionBar,
                content,
                () => (data.dropping ? t.div({ rid: "drop", className: "gpm-dropzone" }, t.i({ className: "ri-upload-cloud-2-line", ariaHidden: true }), t.span(null, `Drop to upload to ${data.folder ? "“" + data.folder + "”" : "the library"}`)) : t.div({ rid: "nodrop", hidden: true })),
            ),
            t.footer({ className: "page-footer" }, app.components.credits()),
        ),
        uploadsPanel(),
    );
}

// --- the picker ---------------------------------------------------------------------------------

/**
 * Opens the library to choose files: kind limits them ("image", "pdf"…),
 * multiple lets several be chosen. Resolves with the chosen files (publicAsset),
 * or [] when closed.
 */
export function pick({ kind = "", multiple = false, title = "" } = {}) {
    return new Promise((resolve) => {
        let chosen = [];
        const state = store({ items: [], isLoading: true, folder: null, search: "", selected: [], dropping: false });

        loadAll()
            .then((items) => (state.items = items.filter((a) => !kind || a.kind == kind)))
            .catch(handleError)
            .finally(() => (state.isLoading = false));

        const folders = () => folderList(state.items);
        const visible = () => state.items.filter((a) => (state.folder == null || (a.folder || "") == state.folder) && matches(a, state.search));
        const isSel = (a) => state.selected.includes(a.id);

        function choose(list = state.items.filter(isSel)) {
            if (!list.length) {
                return;
            }
            chosen = list.map(publicAsset);
            app.modals.close(modal);
        }

        function toggle(a) {
            if (!multiple) {
                state.selected = [a.id];
                return;
            }
            state.selected = isSel(a) ? state.selected.filter((id) => id != a.id) : [...state.selected, a.id];
        }

        async function upload(files) {
            const created = (await uploadFiles(files, state.folder || "")).filter((a) => !kind || a.kind == kind);
            const refused = files.length - created.length;
            if (created.length) {
                state.items = [...created, ...state.items];
                state.selected = multiple ? [...state.selected, ...created.map((a) => a.id)] : [created[0].id];
            }
            if (kind && refused > 0 && created.length < files.length) {
                app.toasts.info(`Only the ${kindOf(kind).label.toLowerCase()} can be chosen here.`);
            }
        }

        const label = kind ? kindOf(kind).label.toLowerCase() : "files";
        const modal = t.div(
            {
                className: "modal popup lg gpm-picker",
                ondragover: (e) => [...(e.dataTransfer?.types || [])].includes("Files") && (e.preventDefault(), (state.dropping = true)),
                ondragleave: (e) => e.target == e.currentTarget && (state.dropping = false),
                ondrop: (e) => {
                    state.dropping = false;
                    if (e.dataTransfer?.files?.length) {
                        e.preventDefault();
                        upload([...e.dataTransfer.files]);
                    }
                },
            },
            t.header({ className: "modal-header" }, t.h5({ className: "m-auto" }, title || (kind ? `Choose ${multiple ? "" : "an "}${multiple ? label : kindOf(kind).one.toLowerCase()} from the Assets` : "Choose from the Assets"))),
            t.div(
                { className: "modal-content" },
                t.div(
                    { className: "gpm-toolbar gpm-picker-toolbar" },
                    t.div(
                        { className: "field gp-bare gpm-search" },
                        t.input({ type: "search", placeholder: `Search ${label}…`, ariaLabel: "Search", autofocus: true, value: () => state.search, oninput: (e) => (state.search = e.target.value) }),
                    ),
                    () =>
                        t.div(
                            { rid: "pf" + folders().join("|"), className: "gpm-picker-folder" },
                            select({
                                required: true,
                                ariaLabel: "Folder",
                                options: () => [{ value: "\u0001", label: "All folders" }, ...(folders().length ? [{ value: "", label: "No folder" }] : []), ...folders().map((f) => ({ value: f, label: f }))],
                                value: () => (state.folder == null ? "\u0001" : state.folder),
                                onchange: (v) => (state.folder = v == "\u0001" ? null : v),
                            }),
                        ),
                    uploadButton({ className: "btn sm secondary", accept: accepts[kind] || "", onfiles: upload }),
                ),
                () => {
                    if (state.isLoading) {
                        return t.div({ rid: "ld", className: "gpm-empty" }, t.span({ className: "loader" }));
                    }
                    const list = visible();
                    if (!list.length) {
                        return t.div(
                            { rid: "none" + state.items.length, className: "gpm-empty" },
                            t.i({ className: "ri-upload-cloud-2-line", ariaHidden: true }),
                            t.p(null, state.items.length ? "Nothing matches." : `No ${label} in the library yet: upload some, or drop them here.`),
                        );
                    }
                    return t.div(
                        { rid: "pg" + sig(list.map((a) => a.id + a.updated)), className: "gpm-grid gpm-picker-grid", role: "listbox", ariaMultiSelectable: multiple ? "true" : "false" },
                        list.map((a) =>
                            t.div(
                                {
                                    rid: a.id + a.updated,
                                    className: () => `gpm-card ${isSel(a) ? "selected" : ""}`,
                                    role: "option",
                                    tabIndex: 0,
                                    ariaSelected: () => (isSel(a) ? "true" : "false"),
                                    title: a.name,
                                    onclick: () => toggle(a),
                                    ondblclick: () => choose([a]),
                                    onkeydown: (e) => {
                                        if (e.key == "Enter") {
                                            e.preventDefault();
                                            choose(multiple && state.selected.length ? undefined : [a]);
                                        } else if (e.key == " ") {
                                            e.preventDefault();
                                            toggle(a);
                                        }
                                    },
                                },
                                t.div({ className: `gpm-thumb gpm-thumb-${a.kind}` }, preview(a)),
                                t.div({ className: "gpm-card-name" }, a.name),
                                t.div({ className: "gpm-card-meta" }, metaLine(a)),
                            )
                        ),
                    );
                },
                () => (state.dropping ? t.div({ rid: "pd", className: "gpm-dropzone" }, t.i({ className: "ri-upload-cloud-2-line", ariaHidden: true }), t.span(null, "Drop to upload")) : t.div({ rid: "npd", hidden: true })),
            ),
            t.footer(
                { className: "modal-footer" },
                t.a({ href: "#/assets", className: "btn transparent secondary gpm-open-library", target: "_blank" }, t.i({ className: "ri-external-link-line", ariaHidden: true }), t.span({ className: "txt" }, "Open the library")),
                t.span({ className: "m-r-auto" }),
                t.button({ type: "button", className: "btn transparent secondary", onclick: () => app.modals.close(modal) }, t.span({ className: "txt" }, "Cancel")),
                t.button(
                    { type: "button", className: "btn expanded gpm-choose", disabled: () => !state.selected.length, onclick: () => choose() },
                    t.span({ className: "txt" }, () => (multiple && state.selected.length > 1 ? `Choose ${state.selected.length}` : "Choose")),
                ),
            ),
            uploadsPanel(),
        );
        openModal(modal, () => resolve(chosen));
    });
}
