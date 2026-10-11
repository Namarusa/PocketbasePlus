import { api, bare, debounce, emailFrame, emailsSidebar, formatDate, handleError, page, select } from "./lib.js";

const statuses = ["", "pending", "sent", "failed", "skipped", "cancelled"];
const statusClass = { sent: "success", failed: "danger", pending: "warning" };

export function pageLog() {
    app.store.title = "Email log";

    const data = store({
        isLoading: true,
        messages: [],
        status: "",
        search: "",
        page: 1,
        totalPages: 1,
        open: null,
    });

    async function load() {
        const parts = [];
        if (data.status) {
            parts.push(app.pb.filter("status = {:s}", { s: data.status }));
        }
        if (data.search) {
            parts.push(app.pb.filter("(to ~ {:q} || subject ~ {:q})", { q: data.search }));
        }
        try {
            const res = await app.pb.collection("email_messages").getList(data.page, 30, {
                sort: "-created",
                filter: parts.join(" && "),
                expand: "automation",
                fields: "*,html:excerpt(1)",
                requestKey: "gpLog",
            });
            data.messages = res.items;
            data.totalPages = Math.max(1, res.totalPages);
        } catch (err) {
            handleError(err);
        }
        data.isLoading = false;
    }

    const reload = debounce(load, 300);
    const search = debounce(() => {
        data.page = 1;
        load();
    }, 250);

    async function view(m) {
        try {
            data.open = await app.pb.collection("email_messages").getOne(m.id, { expand: "template,automation" });
            app.modals.open(modal);
        } catch (err) {
            handleError(err);
        }
    }

    async function act(m, action) {
        try {
            await api(`/api/emails/messages/${m.id}/${action}`, { method: "POST" });
            app.toasts.success(action == "retry" ? "The message was queued again." : "The message was cancelled.");
            app.modals.close(modal, true);
            load();
        } catch (err) {
            handleError(err);
        }
    }

    function actions(m) {
        const list = [];
        if (["failed", "cancelled", "skipped"].includes(m.status)) {
            list.push(t.button(
                { type: "button", className: "btn sm circle transparent secondary", ariaLabel: app.attrs.tooltip("Retry"), onclick: () => act(m, "retry") },
                t.i({ className: "ri-restart-line", ariaHidden: true }),
            ));
        }
        if (m.status == "pending") {
            list.push(t.button(
                { type: "button", className: "btn sm circle transparent danger", ariaLabel: app.attrs.tooltip("Cancel"), onclick: () => act(m, "cancel") },
                t.i({ className: "ri-close-circle-line", ariaHidden: true }),
            ));
        }
        return list;
    }

    const modal = t.div(
        { className: "modal popup lg" },
        t.header({ className: "modal-header" }, t.h5({ className: "m-auto txt-ellipsis" }, () => data.open?.subject || "Email")),
        t.div(
            { className: "modal-content" },
            () => {
                const m = data.open;
                if (!m) {
                    return null;
                }
                const row = (label, ...value) => t.tr(null, t.th({ className: "gp-dl-th" }, label), t.td(null, ...value));
                return [
                    t.table(
                        { className: "m-b-sm" },
                        t.tbody(
                            null,
                            row("To", m.to),
                            row("Status", t.span({ className: `label sm ${statusClass[m.status] || ""}` }, m.status), m.error ? t.span({ className: "txt-danger m-l-5" }, m.error) : ""),
                            row("Template", m.expand?.template?.name || "-"),
                            row("Source", m.expand?.automation?.name || m.source || "Manual", m.collection ? t.span({ className: "txt-hint m-l-5" }, `${m.collection}/${m.record_id}`) : ""),
                            row("Send at", formatDate(m.send_at)),
                            m.sent_at ? row("Sent at", formatDate(m.sent_at)) : null,
                            row("Attempts", String(m.attempts)),
                        ),
                    ),
                    m.html ? emailFrame(m.html) : t.pre({ className: "gp-text-preview" }, m.text),
                ];
            },
        ),
        t.footer(
            { className: "modal-footer" },
            t.button({ type: "button", className: "btn transparent secondary m-r-auto", onclick: () => app.modals.close(modal) }, t.span({ className: "txt" }, "Close")),
            () => {
                const m = data.open;
                if (!m) {
                    return null;
                }
                if (["failed", "cancelled", "skipped"].includes(m.status)) {
                    return t.button({ type: "button", className: "btn", onclick: () => act(m, "retry") }, t.i({ className: "ri-restart-line" }), t.span({ className: "txt" }, "Retry"));
                }
                if (m.status == "pending") {
                    return t.button({ type: "button", className: "btn danger", onclick: () => act(m, "cancel") }, t.span({ className: "txt" }, "Cancel sending"));
                }
            },
        ),
    );

    let unsubscribe;

    return page(
        {
            pbEvent: "gpPageLog",
            sidebar: emailsSidebar(),
            breadcrumbs: ["Emails", "Log"],
            wide: true,
            buttons: app.components.refreshButton({ className: "btn sm transparent secondary circle", onclick: load }),
        },
        t.div(
            {
                className: "flex gap-10 m-b-sm",
                onmount: async () => {
                    load();
                    // live updates while the worker processes the queue
                    try {
                        unsubscribe = await app.pb.collection("email_messages").subscribe("*", () => reload());
                    } catch (err) {
                        console.warn("email log realtime:", err);
                    }
                },
                onunmount: () => {
                    reload.cancel();
                    search.cancel();
                    unsubscribe?.();
                },
            },
            bare(select({
                required: true,
                options: statuses.map((s) => ({ value: s, label: s || "All statuses" })),
                value: () => data.status,
                onchange: (v) => {
                    data.status = v;
                    data.page = 1;
                    load();
                },
            }), "gp-status-filter"),
            bare(t.input({
                type: "search",
                placeholder: "Search recipient or subject...",
                value: () => data.search,
                oninput: (e) => {
                    data.search = e.target.value;
                    search();
                },
            }), "flex-fill"),
        ),
        t.p({ className: "txt-hint" }, "Every email goes through this queue: manual sends, automations and schedules."),
        t.div(
            { className: "gp-table-wrap" },
            t.table(
                null,
                t.thead(null, t.tr(null, t.th(null, "Status"), t.th(null, "To"), t.th(null, "Subject"), t.th(null, "Source"), t.th(null, "Date"), t.th(null, ""))),
                t.tbody(null, () => {
                    if (data.isLoading && !data.messages.length) {
                        return t.tr({ rid: "loading" }, t.td({ colSpan: 6, className: "txt-center" }, t.span({ className: "loader" })));
                    }
                    if (!data.messages.length) {
                        return t.tr({ rid: "empty" }, t.td({ colSpan: 6, className: "txt-center txt-hint" }, "No emails yet."));
                    }
                    return data.messages.map((m) =>
                        t.tr(
                            { rid: m.id + m.updated, className: "gp-clickable", onclick: (e) => !e.target.closest("button") && view(m) },
                            t.td(
                                { className: "txt-nowrap" },
                                t.span({ className: `label sm ${statusClass[m.status] || ""}` }, m.status),
                                m.attempts > 1 ? t.span({ className: "txt-hint txt-sm m-l-5" }, `x${m.attempts}`) : null,
                            ),
                            t.td(null, m.to),
                            t.td(null, t.div(null, m.subject), m.error ? t.div({ className: "txt-danger txt-sm" }, m.error) : null),
                            t.td({ className: "txt-hint txt-sm" }, m.expand?.automation?.name || m.source || "Manual"),
                            t.td({ className: "txt-hint txt-sm txt-nowrap" }, formatDate(m.status == "sent" ? m.sent_at : m.send_at)),
                            t.td({ className: "txt-right txt-nowrap" }, actions(m)),
                        )
                    );
                }),
            ),
        ),
        t.div(
            { className: "flex gap-10 m-t-sm gp-pager", hidden: () => data.totalPages <= 1 },
            t.button({ type: "button", className: "btn sm secondary", disabled: () => data.page <= 1, onclick: () => (data.page--, load()) }, "Previous"),
            t.span({ className: "txt-hint" }, () => `Page ${data.page} of ${data.totalPages}`),
            t.button({ type: "button", className: "btn sm secondary", disabled: () => data.page >= data.totalPages, onclick: () => (data.page++, load()) }, "Next"),
        ),
        modal,
    );
}
