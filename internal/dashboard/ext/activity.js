// Activity: record history (revisions with diffs and revert), record
// comments with @mentions, the activity page and the notifications bell.

import { api, formatDate, handleError, page, select, sidebar, userCollections } from "./lib.js";

const actionInfo = {
    create: { label: "Created", icon: "ri-add-circle-line", className: "success" },
    update: { label: "Updated", icon: "ri-edit-line", className: "info" },
    delete: { label: "Deleted", icon: "ri-delete-bin-7-line", className: "danger" },
    revert: { label: "Reverted", icon: "ri-arrow-go-back-line", className: "warning" },
};

const actorIcons = {
    superuser: "ri-shield-user-line",
    user: "ri-user-3-line",
    flow: "ri-flow-chart",
    system: "ri-settings-3-line",
};

function timeAgo(value) {
    const d = new Date(String(value).replace(" ", "T"));
    if (isNaN(d)) {
        return "";
    }
    const s = Math.round((Date.now() - d.getTime()) / 1000);
    if (s < 60) return "just now";
    if (s < 3600) return `${Math.round(s / 60)} min ago`;
    if (s < 86400) return `${Math.round(s / 3600)} h ago`;
    if (s < 86400 * 30) return `${Math.round(s / 86400)} d ago`;
    return d.toLocaleDateString();
}

/** Readable field value for the diffs. */
function displayValue(v) {
    if (v === null || v === undefined || v === "") {
        return t.span({ className: "txt-hint" }, "empty");
    }
    if (typeof v == "object") {
        return t.code({ className: "gp-diff-json" }, JSON.stringify(v));
    }
    if (typeof v == "boolean") {
        return v ? "true" : "false";
    }
    const s = String(v);
    // editor fields hold HTML: show it as text
    return s.length > 300 ? s.slice(0, 300) + "…" : s;
}

let superusersCache = null;
function superusers() {
    superusersCache ||= app.pb.collection("_superusers")
        .getFullList({ fields: "id,email", sort: "email", requestKey: null })
        .catch((err) => {
            superusersCache = null;
            throw err;
        });
    return superusersCache;
}

// --- record popup: History & comments ---------------------------------------------------

/** Adds the "History" button to the header of the native record popup. */
export function injectHistoryButton(modalEl, collection, record) {
    const header = modalEl.querySelector(".modal-header .col-12.flex");
    if (!header || header.querySelector(".gp-history-btn")) {
        return;
    }
    const btn = t.button(
        {
            type: "button",
            className: "btn sm outline gp-history-btn",
            onclick: () => openHistory(collection, record, modalEl),
        },
        t.i({ className: "ri-history-line", ariaHidden: true }),
        t.span({ className: "txt" }, "History"),
    );
    const spacer = header.querySelector(".flex-fill");
    if (spacer) {
        spacer.after(btn);
    } else {
        header.appendChild(btn);
    }
}

function openHistory(collection, record, upsertModal) {
    const data = store({
        tab: "history",
        revisions: [],
        comments: [],
        loading: true,
        open: {},
        text: "",
        editing: "",
        editText: "",
        superusers: [],
        mention: null, // { query, start }
        isSending: false,
    });
    const me = app.store.superuser?.id;

    async function loadRevisions() {
        data.loading = true;
        try {
            const res = await app.pb.collection("revisions").getList(1, 100, {
                filter: app.pb.filter("collection = {:c} && record_id = {:r}", { c: collection.id, r: record.id }),
                sort: "-created",
                requestKey: "gpRevisions" + record.id,
            });
            data.revisions = res.items;
            if (res.items[0]) {
                data.open[res.items[0].id] = true;
            }
        } catch (err) {
            handleError(err);
        } finally {
            data.loading = false;
        }
    }

    async function loadComments() {
        try {
            data.comments = await app.pb.collection("comments").getFullList({
                filter: app.pb.filter("collection = {:c} && record_id = {:r}", { c: collection.name, r: record.id }),
                sort: "created",
                requestKey: "gpComments" + record.id,
            });
        } catch (err) {
            handleError(err);
        }
    }

    function revert(rev) {
        const label = rev.action == "delete" ? "restore the record as it was before its deletion" : "revert the record to this version";
        app.modals.confirm(`Do you really want to ${label}? The current values will be replaced (files are kept).`, async () => {
            try {
                const res = await api(`/api/activity/revisions/${rev.id}/revert`, { method: "POST" });
                app.toasts.success(res.skippedFiles?.length ? "Reverted (files were kept as they are)." : "Successfully reverted the record.");
                app.modals.close(modal);
                // reopen the record popup with the reverted values
                if (upsertModal?.isConnected) {
                    app.modals.close(upsertModal, true);
                }
                app.modals.openRecordUpsert(collection, res.record);
            } catch (err) {
                handleError(err);
            }
        });
    }

    // mentions: "@email" of a superuser in the text
    function mentionsOf(text) {
        return data.superusers.filter((s) => text.includes("@" + s.email)).map((s) => s.id);
    }

    function onCommentInput(e) {
        data.text = e.target.value;
        const before = e.target.value.slice(0, e.target.selectionStart);
        const m = before.match(/(^|\s)@([^\s@]*)$/);
        data.mention = m ? { query: m[2].toLowerCase(), start: before.length - m[2].length - 1 } : null;
    }

    function insertMention(textarea, su) {
        const start = data.mention.start;
        const end = start + 1 + data.mention.query.length;
        data.text = data.text.slice(0, start) + "@" + su.email + " " + data.text.slice(end);
        data.mention = null;
        textarea.value = data.text;
        textarea.focus();
    }

    async function send() {
        if (!data.text.trim()) {
            return;
        }
        data.isSending = true;
        try {
            await app.pb.collection("comments").create({
                collection: collection.name,
                record_id: record.id,
                text: data.text.trim(),
                mentions: mentionsOf(data.text),
            });
            data.text = "";
            await loadComments();
        } catch (err) {
            handleError(err);
        } finally {
            data.isSending = false;
        }
    }

    async function saveEdit(c) {
        try {
            await app.pb.collection("comments").update(c.id, { text: data.editText, mentions: mentionsOf(data.editText) });
            data.editing = "";
            await loadComments();
        } catch (err) {
            handleError(err);
        }
    }

    function removeComment(c) {
        app.modals.confirm("Delete this comment?", async () => {
            try {
                await app.pb.collection("comments").delete(c.id);
                await loadComments();
            } catch (err) {
                handleError(err);
            }
        });
    }

    // highlights the @mentions of a comment
    function commentText(text) {
        const parts = text.split(/(@[^\s@]+@[^\s@]+)/g);
        return parts.map((p) => (p.startsWith("@") && p.includes("@", 1) ? t.strong({ className: "gp-mention" }, p) : p));
    }

    loadRevisions();
    loadComments();
    superusers().then((list) => (data.superusers = list)).catch(handleError);

    let textareaEl;
    const modal = t.div(
        { className: "modal popup gp-history-modal" },
        t.header(
            { className: "modal-header" },
            t.h5({ className: "m-auto" }, `${collection.name} / ${record.id}`),
        ),
        t.div(
            { className: "modal-content" },
            t.div(
                { className: "tabs-header gp-history-tabs" },
                t.button({ type: "button", className: () => `tab-item ${data.tab == "history" ? "active" : ""}`, onclick: () => (data.tab = "history") }, t.i({ className: "ri-history-line", ariaHidden: true }), t.span({ className: "txt" }, "History")),
                t.button(
                    { type: "button", className: () => `tab-item ${data.tab == "comments" ? "active" : ""}`, onclick: () => (data.tab = "comments") },
                    t.i({ className: "ri-chat-3-line", ariaHidden: true }),
                    t.span({ className: "txt" }, () => `Comments${data.comments.length ? ` (${data.comments.length})` : ""}`),
                ),
            ),
            () => {
                if (data.tab == "history") {
                    if (data.loading && !data.revisions.length) {
                        return t.div({ rid: "loading", className: "txt-center p-base" }, t.span({ className: "loader" }));
                    }
                    if (!data.revisions.length) {
                        return t.p({ rid: "empty", className: "txt-hint txt-center" }, "No history yet. Changes are recorded from now on.");
                    }
                    return t.div(
                        { rid: "revisions-" + data.revisions.map((r) => r.id).join(), className: "gp-timeline" },
                        data.revisions.map((rev, i) => {
                            const info = actionInfo[rev.action] || actionInfo.update;
                            const delta = rev.delta || {};
                            const fields = Object.keys(delta);
                            return t.div(
                                { className: "gp-timeline-item" },
                                t.span({ className: `gp-timeline-icon ${info.className}` }, t.i({ className: info.icon, ariaHidden: true })),
                                t.div(
                                    { className: "gp-timeline-content" },
                                    t.div(
                                        { className: "gp-timeline-head" },
                                        t.strong(null, info.label),
                                        t.span({ className: "txt-hint" }, "by"),
                                        t.span({ className: "gp-actor" }, t.i({ className: actorIcons[rev.actor_type] || actorIcons.system, ariaHidden: true }), rev.actor_label || rev.actor_type),
                                        t.span({ className: "txt-hint txt-sm", title: formatDate(rev.created) }, timeAgo(rev.created)),
                                        t.div(
                                            { className: "gp-timeline-actions" },
                                            fields.length
                                                ? t.button(
                                                    { type: "button", className: "btn sm transparent secondary", onclick: () => (data.open[rev.id] = !data.open[rev.id]) },
                                                    t.span({ className: "txt" }, () => `${fields.length} field${fields.length > 1 ? "s" : ""}`),
                                                    t.i({ className: () => (data.open[rev.id] ? "ri-arrow-up-s-line" : "ri-arrow-down-s-line"), ariaHidden: true }),
                                                )
                                                : null,
                                            i > 0 || rev.action == "delete"
                                                ? t.button(
                                                    { type: "button", className: "btn sm transparent secondary", title: rev.action == "delete" ? "Restore the record" : "Revert to this version", onclick: () => revert(rev) },
                                                    t.i({ className: "ri-arrow-go-back-line", ariaHidden: true }),
                                                    t.span({ className: "txt" }, rev.action == "delete" ? "Restore" : "Revert"),
                                                )
                                                : t.span({ className: "label sm" }, "Current"),
                                        ),
                                    ),
                                    () =>
                                        data.open[rev.id] && fields.length
                                            ? t.table(
                                                { rid: "diff", className: "gp-diff" },
                                                t.thead(null, t.tr(null, t.th(null, "Field"), t.th(null, "Before"), t.th(null, "After"))),
                                                t.tbody(
                                                    null,
                                                    fields.map((f) =>
                                                        t.tr(
                                                            null,
                                                            t.td({ className: "txt-bold" }, f),
                                                            t.td({ className: "gp-diff-old" }, displayValue(delta[f]?.[0])),
                                                            t.td({ className: "gp-diff-new" }, displayValue(delta[f]?.[1])),
                                                        )
                                                    ),
                                                ),
                                            )
                                            : null,
                                ),
                            );
                        }),
                    );
                }

                return t.div(
                    { rid: "comments", className: "gp-comments" },
                    () =>
                        data.comments.length
                            ? data.comments.map((c) =>
                                t.div(
                                    { rid: c.id + c.updated, className: "gp-comment" },
                                    t.div(
                                        { className: "gp-comment-head" },
                                        t.strong(null, c.author_label || "?"),
                                        t.span({ className: "txt-hint txt-sm", title: formatDate(c.created) }, timeAgo(c.created) + (c.updated != c.created ? " (edited)" : "")),
                                        c.author_id == me
                                            ? t.div(
                                                { className: "gp-comment-actions" },
                                                t.button({ type: "button", className: "btn sm circle transparent secondary", title: "Edit", onclick: () => ((data.editing = c.id), (data.editText = c.text)) }, t.i({ className: "ri-pencil-line", ariaHidden: true })),
                                                t.button({ type: "button", className: "btn sm circle transparent danger", title: "Delete", onclick: () => removeComment(c) }, t.i({ className: "ri-delete-bin-7-line", ariaHidden: true })),
                                            )
                                            : null,
                                    ),
                                    () =>
                                        data.editing == c.id
                                            ? t.div(
                                                { rid: "edit", className: "gp-comment-edit" },
                                                t.div({ className: "field" }, t.textarea({ rows: 3, value: () => data.editText, oninput: (e) => (data.editText = e.target.value) })),
                                                t.div(
                                                    { className: "flex gap-5" },
                                                    t.button({ type: "button", className: "btn sm transparent secondary", onclick: () => (data.editing = "") }, t.span({ className: "txt" }, "Cancel")),
                                                    t.button({ type: "button", className: "btn sm", onclick: () => saveEdit(c) }, t.span({ className: "txt" }, "Save")),
                                                ),
                                            )
                                            : t.div({ rid: "text", className: "gp-comment-text" }, commentText(c.text)),
                                )
                            )
                            : t.p({ rid: "empty", className: "txt-hint txt-center" }, "No comments yet."),
                    t.div(
                        { className: "gp-comment-form" },
                        t.div(
                            { className: "field" },
                            t.textarea({
                                rows: 3,
                                placeholder: "Write a comment… Use @ to mention a superuser.",
                                value: () => data.text,
                                oninput: onCommentInput,
                                onkeydown: (e) => {
                                    if (e.key == "Enter" && (e.ctrlKey || e.metaKey)) {
                                        e.preventDefault();
                                        send();
                                    }
                                    if (e.key == "Escape" && data.mention) {
                                        e.stopPropagation();
                                        data.mention = null;
                                    }
                                },
                                onmount: (el) => (textareaEl = el),
                            }),
                        ),
                        () => {
                            if (!data.mention) {
                                return null;
                            }
                            const list = data.superusers.filter((s) => s.id != me && s.email.toLowerCase().includes(data.mention.query)).slice(0, 6);
                            if (!list.length) {
                                return null;
                            }
                            return t.div(
                                // keyed by the query: an element kept by rid wouldn't refresh its items
                                { rid: "mentions-" + data.mention.query, className: "gp-mention-list" },
                                list.map((s) => t.button({ type: "button", className: "dropdown-item", onclick: () => insertMention(textareaEl, s) }, t.i({ className: "ri-at-line", ariaHidden: true }), t.span({ className: "txt" }, s.email))),
                            );
                        },
                        t.div(
                            { className: "flex gap-10" },
                            t.span({ className: "txt-hint txt-sm m-r-auto" }, "Ctrl + Enter to send"),
                            t.button(
                                { type: "button", className: () => `btn sm ${data.isSending ? "loading" : ""}`, disabled: () => data.isSending || !data.text.trim(), onclick: send },
                                t.i({ className: "ri-send-plane-line", ariaHidden: true }),
                                t.span({ className: "txt" }, "Comment"),
                            ),
                        ),
                    ),
                );
            },
        ),
        t.footer(
            { className: "modal-footer" },
            t.button({ type: "button", className: "btn transparent secondary m-l-auto", onclick: () => app.modals.close(modal) }, t.span({ className: "txt" }, "Close")),
        ),
    );
    modal.onafterclose = () => modal.remove();
    document.body.appendChild(modal);
    app.modals.open(modal);
}

// --- activity page ---------------------------------------------------------------------

export function pageActivity() {
    app.store.title = "Activity";
    const data = store({
        items: [],
        page: 1,
        hasMore: false,
        loading: false,
        collection: "",
        action: "",
        actorType: "",
        search: "",
        collections: [],
    });

    userCollections().then((list) => (data.collections = list)).catch(handleError);

    async function load(reset = true) {
        if (reset) {
            data.page = 1;
        }
        data.loading = true;
        const filters = [];
        const params = {};
        if (data.collection) {
            filters.push("collection_name = {:c}");
            params.c = data.collection;
        }
        if (data.action) {
            filters.push("action = {:a}");
            params.a = data.action;
        }
        if (data.actorType) {
            filters.push("actor_type = {:t}");
            params.t = data.actorType;
        }
        if (data.search.trim()) {
            filters.push("(actor_label ~ {:s} || record_id ~ {:s})");
            params.s = data.search.trim();
        }
        try {
            const res = await app.pb.collection("revisions").getList(data.page, 50, {
                filter: filters.length ? app.pb.filter(filters.join(" && "), params) : "",
                sort: "-created",
                fields: "id,collection_name,record_id,action,actor_type,actor_label,delta,created",
                requestKey: "gpActivity",
            });
            data.items = reset ? res.items : [...data.items, ...res.items];
            data.hasMore = res.page < res.totalPages;
        } catch (err) {
            handleError(err);
        } finally {
            data.loading = false;
        }
    }
    load();

    const filterSelect = (key, placeholder, options) =>
        t.div({ className: "field gp-bare gp-activity-filter" }, select({
            placeholder,
            options,
            value: () => data[key],
            onchange: (v) => {
                data[key] = v;
                load();
            },
        }));

    let searchTimer;
    return page(
        {
            pbEvent: "gpPageActivity",
            className: "gp-page-activity",
            sidebar: sidebar("gpActivitySidebar", [{ name: "Activity", links: [{ href: "#/activity", label: "All changes", icon: "ri-history-line", exact: true }] }]),
            breadcrumbs: ["Activity"],
            wide: true,
        },
        t.p({ className: "txt-hint m-t-0" }, "Every change of your collections' records: who, when and what changed. Open a record and use History to compare or revert versions."),
        t.div(
            { className: "gp-activity-filters" },
            filterSelect("collection", "All collections", () => data.collections.map((c) => ({ value: c.name, label: c.name }))),
            filterSelect("action", "All actions", Object.entries(actionInfo).map(([value, i]) => ({ value, label: i.label }))),
            filterSelect("actorType", "All authors", [
                { value: "superuser", label: "Superusers" },
                { value: "user", label: "Users" },
                { value: "flow", label: "Flows" },
                { value: "system", label: "System" },
            ]),
            t.div(
                { className: "field gp-bare gp-activity-search" },
                t.input({
                    type: "search",
                    placeholder: "Search author or record id...",
                    value: () => data.search,
                    oninput: (e) => {
                        data.search = e.target.value;
                        clearTimeout(searchTimer);
                        searchTimer = setTimeout(load, 300);
                    },
                }),
            ),
        ),
        t.div(
            { className: "gp-table-wrap" },
            t.table(
                null,
                t.thead(null, t.tr(null, t.th(null, "When"), t.th(null, "Action"), t.th(null, "Record"), t.th(null, "Author"), t.th(null, "Changed fields"))),
                t.tbody(null, () => {
                    if (data.loading && !data.items.length) {
                        return t.tr({ rid: "loading" }, t.td({ colSpan: 5, className: "txt-center" }, t.span({ className: "loader" })));
                    }
                    if (!data.items.length) {
                        return t.tr({ rid: "empty" }, t.td({ colSpan: 5, className: "txt-center txt-hint" }, "No changes recorded yet."));
                    }
                    return data.items.map((r) => {
                        const info = actionInfo[r.action] || actionInfo.update;
                        const fields = Object.keys(r.delta || {});
                        return t.tr(
                            { rid: r.id },
                            t.td({ className: "txt-nowrap", title: formatDate(r.created) }, timeAgo(r.created)),
                            t.td(null, t.span({ className: `label sm ${info.className}` }, info.label)),
                            t.td(
                                null,
                                r.action == "delete"
                                    ? t.span(null, `${r.collection_name} / `, t.span({ className: "txt-hint" }, r.record_id))
                                    : t.a({ href: `#/collections?collection=${encodeURIComponent(r.collection_name)}&record=${encodeURIComponent(r.record_id)}` }, `${r.collection_name} / ${r.record_id}`),
                            ),
                            t.td(null, t.span({ className: "gp-actor" }, t.i({ className: actorIcons[r.actor_type] || actorIcons.system, ariaHidden: true }), r.actor_label || r.actor_type)),
                            t.td({ className: "txt-hint txt-sm gp-activity-fields" }, fields.join(", ")),
                        );
                    });
                }),
            ),
        ),
        t.div(
            { className: "txt-center m-t-sm", hidden: () => !data.hasMore },
            t.button(
                { type: "button", className: () => `btn sm secondary ${data.loading ? "loading" : ""}`, onclick: () => (data.page++, load(false)) },
                t.span({ className: "txt" }, "Load more"),
            ),
        ),
    );
}

// --- notifications bell ------------------------------------------------------------------

const bell = store({ items: [], unread: 0, loaded: false });
let unsubscribe = null;

async function loadNotifications() {
    const me = app.store.superuser?.id;
    if (!me) {
        return;
    }
    try {
        const res = await app.pb.collection("notifications").getList(1, 30, {
            filter: app.pb.filter("recipient = {:me}", { me }),
            sort: "-created",
            requestKey: "gpNotifications",
        });
        bell.items = res.items;
        bell.unread = (await app.pb.collection("notifications").getList(1, 1, {
            filter: app.pb.filter("recipient = {:me} && read = false", { me }),
            fields: "id",
            requestKey: "gpNotificationsUnread",
        })).totalItems;
        bell.loaded = true;
    } catch (err) {
        if (!err?.isAbort) {
            console.warn("[pocketbaseplus] failed to load the notifications", err);
        }
    }
}

async function markAllRead() {
    const unread = bell.items.filter((n) => !n.read);
    try {
        await Promise.all(unread.map((n) => app.pb.collection("notifications").update(n.id, { read: true }, { requestKey: null })));
    } catch (err) {
        handleError(err);
    }
    loadNotifications();
}

async function openNotification(n) {
    if (!n.read) {
        app.pb.collection("notifications").update(n.id, { read: true }, { requestKey: null }).then(loadNotifications).catch(() => {});
    }
    if (n.link) {
        window.location.hash = n.link.replace(/^#/, "");
    }
}

function bellButton() {
    const id = "gpNotificationsDropdown";
    return t.div(
        { className: "gp-bell" },
        t.button(
            {
                type: "button",
                className: "header-link gp-bell-btn",
                ariaLabel: "Notifications",
                "html-popovertarget": id,
            },
            t.i({ className: () => (bell.unread ? "ri-notification-3-fill" : "ri-notification-3-line"), ariaHidden: true }),
            () => (bell.unread ? t.span({ rid: "badge", className: "gp-bell-badge" }, bell.unread > 99 ? "99+" : String(bell.unread)) : null),
        ),
        t.div(
            { id, popover: "auto", className: "dropdown gp-bell-dropdown" },
            t.div(
                { className: "gp-bell-head" },
                t.strong(null, "Notifications"),
                t.button({ type: "button", className: "btn sm transparent secondary", hidden: () => !bell.unread, onclick: markAllRead }, t.span({ className: "txt" }, "Mark all read")),
            ),
            () => {
                if (!bell.items.length) {
                    return t.div({ rid: "empty", className: "txt-hint txt-center p-sm" }, "No notifications.");
                }
                return t.div(
                    { rid: "list-" + bell.items.map((n) => n.id + (n.read ? 1 : 0)).join(), className: "gp-bell-list" },
                    bell.items.map((n) =>
                        t.button(
                            {
                                type: "button",
                                className: `dropdown-item gp-notification ${n.read ? "" : "unread"}`,
                                onclick: (e) => {
                                    e.currentTarget.closest(".dropdown")?.hidePopover();
                                    openNotification(n);
                                },
                            },
                            t.i({ className: n.kind == "mention" ? "ri-at-line" : n.kind == "flow" ? "ri-flow-chart" : "ri-information-line", ariaHidden: true }),
                            t.div(
                                { className: "gp-notification-content" },
                                t.strong(null, n.subject),
                                n.message ? t.span({ className: "txt-hint txt-sm" }, n.message) : null,
                                t.span({ className: "txt-hint gp-txt-xs" }, timeAgo(n.created)),
                            ),
                        )
                    ),
                );
            },
        ),
    );
}

/** Adds the notifications bell to the app header (called when it mounts). */
export function mountNotifications(headerEl) {
    if (headerEl._gpBell) {
        return;
    }
    headerEl._gpBell = true;
    const el = bellButton();
    const place = () => {
        const anchor = headerEl.querySelector(".header-link.logged-user");
        if (anchor && el.nextElementSibling !== anchor) {
            anchor.before(el);
        }
    };
    place();
    new MutationObserver(place).observe(headerEl, { childList: true });

    loadNotifications();
    if (!unsubscribe) {
        // live updates (new notifications, read from another tab)
        app.pb.collection("notifications")
            .subscribe("*", (e) => {
                if (e.record?.recipient == app.store.superuser?.id) {
                    if (e.action == "create") {
                        app.toasts.info(e.record.subject);
                    }
                    loadNotifications();
                }
            })
            .then((fn) => (unsubscribe = fn))
            .catch((err) => console.warn("[pocketbaseplus] notifications realtime unavailable", err));
    }
}
