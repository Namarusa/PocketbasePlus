// Pages: the saved versions of a page (History) and the local copy of the
// unsaved changes (restored after a crash or a closed tab).

import { api, handleError } from "../pocketbaseplus/lib.js";

const when = (d) => new Date(String(d).replace(" ", "T")).toLocaleString();

/**
 * Lists the versions of a page: preview one (new tab) or load it in the
 * editor (saved only when the page is saved). ctx: { id, isPart, restore(version) }.
 */
export function historyModal(ctx) {
    const data = store({ versions: null });
    app.pb
        .collection("pages_versions")
        .getList(1, 30, { filter: app.pb.filter("page = {:id}", { id: ctx.id }), sort: "-created", fields: "id,created,by,title", requestKey: null })
        .then((r) => (data.versions = r.items))
        .catch((err) => {
            handleError(err);
            data.versions = [];
        });

    async function preview(v) {
        const win = window.open("", "_blank"); // opened now: popups need the click
        try {
            const res = await api(`/api/pages/${ctx.id}/preview?version=${encodeURIComponent(v.id)}`, { method: "POST" });
            if (win) {
                win.opener = null;
                win.location = res.url;
            }
        } catch (err) {
            win?.close();
            handleError(err);
        }
    }

    async function restore(v) {
        try {
            const full = await app.pb.collection("pages_versions").getOne(v.id, { requestKey: null });
            ctx.restore(full);
            app.modals.close(modal);
            app.toasts.success(`Loaded the version of ${when(v.created)}. Save to apply it.`);
        } catch (err) {
            handleError(err);
        }
    }

    const modal = t.div(
        { className: "modal popup" },
        t.header({ className: "modal-header" }, t.h5({ className: "m-auto" }, "History")),
        t.div(
            { className: "modal-content" },
            t.p({ className: "txt-hint m-t-0" }, "Each save that changes the design keeps a version (the last 30). Restoring loads it in the editor: nothing changes on the site until you save."),
            () => {
                if (!data.versions) {
                    return t.div({ rid: "loading", className: "txt-center" }, t.span({ className: "loader" }));
                }
                if (!data.versions.length) {
                    return t.p({ rid: "empty", className: "txt-hint txt-center" }, "No saved versions yet.");
                }
                return t.div(
                    { rid: "list", className: "gpp-history" },
                    ...data.versions.map((v, i) =>
                        t.div(
                            { className: "gpp-history-row" },
                            t.div(
                                { className: "gpp-history-info" },
                                t.strong(null, when(v.created)),
                                i == 0 ? t.span({ className: "label sm success" }, "current") : null,
                                t.div({ className: "txt-hint txt-sm" }, v.by || "", v.title ? ` · ${v.title}` : ""),
                            ),
                            ctx.isPart
                                ? null
                                : t.button({ type: "button", className: "btn sm transparent secondary", onclick: () => preview(v) }, t.i({ className: "ri-eye-line", ariaHidden: true }), t.span({ className: "txt" }, "Preview")),
                            i == 0
                                ? null
                                : t.button({ type: "button", className: "btn sm secondary", onclick: () => restore(v) }, t.i({ className: "ri-history-line", ariaHidden: true }), t.span({ className: "txt" }, "Restore")),
                        )
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

// --- local copy of the unsaved changes ---------------------------------------------------------

const draftKey = (id) => "gpp-draft-" + id;

export function storeDraft(id, project) {
    try {
        localStorage.setItem(draftKey(id), JSON.stringify({ at: Date.now(), project }));
    } catch {
        // full or blocked storage: no local copy
    }
}

export function clearDraft(id) {
    try {
        localStorage.removeItem(draftKey(id));
    } catch {
        // blocked storage
    }
}

/** Offers to restore a local copy newer than the saved page. */
export function offerDraft(id, updated, restore) {
    let saved = null;
    try {
        saved = JSON.parse(localStorage.getItem(draftKey(id)) || "null");
    } catch {
        return;
    }
    if (!saved?.project || saved.at <= Date.parse(String(updated).replace(" ", "T"))) {
        if (saved) {
            clearDraft(id);
        }
        return;
    }
    app.modals.confirm(
        `This page has unsaved changes from ${new Date(saved.at).toLocaleString()} (the tab was closed or the browser stopped). Restore them?`,
        () => restore(saved.project),
        () => clearDraft(id),
        { yesButton: "Restore", noButton: "Discard" },
    );
}
