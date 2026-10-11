import { api, checkbox, debounce, emailFrame, emailsSidebar, field, handleError, page, recipientPaths, recordLabel, select, switchInput, userCollections } from "./lib.js";

export function pageSend(route) {
    app.store.title = "Send email";

    const data = store({
        templates: [],
        collections: [],
        templateId: route.query?.template?.[0] || route.query?.template || "",
        collection: "",
        mode: "filter", // filter | selected
        filter: "",
        recipientField: "",
        staticRecipients: "",
        sendAt: "",
        dedupe: true,
        count: null, // dry run result
        isCounting: false,
        paths: [],
        records: [],
        total: 0,
        listError: "",
        selected: {},
        preview: null,
        isSending: false,
        get selectedIds() {
            return Object.keys(data.selected).filter((id) => data.selected[id]);
        },
        get targetCount() {
            if (!data.collection) {
                return 0;
            }
            return data.mode == "selected" ? data.selectedIds.length : data.total;
        },
        get canSend() {
            if (!data.templateId) {
                return false;
            }
            if (!data.collection) {
                return !!data.staticRecipients.trim();
            }
            return data.targetCount > 0 && (!!data.recipientField || !!data.staticRecipients.trim());
        },
    });

    async function init() {
        try {
            const [templates, collections] = await Promise.all([
                app.pb.collection("email_templates").getFullList({ sort: "name", fields: "id,name", requestKey: "gpSendTemplates" }),
                userCollections(),
            ]);
            data.templates = templates;
            data.collections = collections;
            if (!data.templateId && templates.length) {
                data.templateId = templates[0].id;
            }
        } catch (err) {
            handleError(err);
        }
    }

    const loadRecords = debounce(async () => {
        data.listError = "";
        if (!data.collection) {
            data.records = [];
            data.total = 0;
            return;
        }
        try {
            const res = await app.pb.collection(data.collection).getList(1, 50, { filter: data.filter, requestKey: "gpSendRecords" });
            data.records = res.items;
            data.total = res.totalItems;
        } catch (err) {
            if (!err.isAbort) {
                data.records = [];
                data.total = 0;
                data.listError = err.message;
            }
        }
    }, 300);

    const loadPreview = debounce(async () => {
        if (!data.templateId) {
            data.preview = null;
            return;
        }
        const recordId = data.mode == "selected" ? data.selectedIds[0] || "" : data.records[0]?.id || "";
        try {
            data.preview = await api("/api/emails/preview", {
                method: "POST",
                body: { templateId: data.templateId, collection: data.collection, recordId },
                requestKey: "gpSendPreview",
            });
        } catch (err) {
            handleError(err);
        }
    }, 200);

    function sendBody(dryRun) {
        return {
            templateId: data.templateId,
            collection: data.collection,
            recordIds: data.collection && data.mode == "selected" ? data.selectedIds : [],
            filter: data.mode == "filter" ? data.filter : "",
            recipientField: data.collection ? data.recipientField : "",
            staticRecipients: data.staticRecipients,
            sendAt: data.sendAt ? new Date(data.sendAt).toISOString() : "",
            dedupe: data.dedupe,
            dryRun,
        };
    }

    function describeCount(res) {
        const parts = [`${res.queued} email(s)`];
        if (res.duplicates) {
            parts.push(`${res.duplicates} duplicate address(es) skipped`);
        }
        if (res.unsubscribed) {
            parts.push(`${res.unsubscribed} unsubscribed skipped`);
        }
        if (res.noAddress) {
            parts.push(`${res.noAddress} record(s) without address`);
        }
        return parts.join(" · ");
    }

    async function countRecipients() {
        if (!data.canSend || data.isCounting) {
            return;
        }
        data.isCounting = true;
        try {
            data.count = await api("/api/emails/send", { method: "POST", body: sendBody(true), requestKey: "gpSendCount" });
        } catch (err) {
            data.count = null;
            handleError(err);
        }
        data.isCounting = false;
    }

    function send() {
        if (!data.canSend || data.isSending) {
            return;
        }
        const who = data.collection ? `${data.targetCount} record(s) of "${data.collection}"` : "the static recipients";
        app.modals.confirm(`Queue the emails for ${who}?`, async () => {
            data.isSending = true;
            try {
                const res = await api("/api/emails/send", { method: "POST", body: sendBody(false) });
                data.count = null;
                app.toasts.success(
                    `${describeCount(res).replace(" email(s)", " email(s) queued")}${data.sendAt ? " for " + new Date(data.sendAt).toLocaleString() : ""}.`,
                );
            } catch (err) {
                handleError(err);
            }
            data.isSending = false;
        });
    }

    const watchers = [];

    init();

    return page(
        {
            pbEvent: "gpPageSend",
            className: "gp-page-send",
            sidebar: emailsSidebar(),
            wide: true,
            breadcrumbs: ["Emails", "Send"],
        },
        t.div(
            {
                className: "grid",
                onmount: () => {
                    watchers.push(watch(() => data.collection, async (c) => {
                        data.selected = {};
                        data.paths = c ? await recipientPaths(c) : [];
                        data.recipientField = data.paths[0] || "";
                    }));
                    watchers.push(watch(() => [data.collection, data.filter], () => loadRecords()));
                    watchers.push(watch(() => [data.templateId, data.collection, data.mode, data.selectedIds[0], data.records[0]?.id], () => loadPreview()));
                    // the recipient count is stale once the targeting changes
                    watchers.push(watch(
                        () => [data.templateId, data.collection, data.mode, data.filter, data.recipientField, data.staticRecipients, data.dedupe, data.selectedIds.length],
                        () => (data.count = null),
                    ));
                },
                onunmount: () => {
                    loadRecords.cancel();
                    loadPreview.cancel();
                    watchers.forEach((w) => w.unwatch());
                },
            },
            t.div(
                { className: "col-lg-7" },
                t.div(
                    { className: "grid sm" },
                    t.div({ className: "col-lg-6" }, field({ label: "Template", name: "templateId" }, select({
                        id: "templateId",
                        required: true,
                        options: () => data.templates.map((tpl) => ({ value: tpl.id, label: tpl.name })),
                        value: () => data.templateId,
                        onchange: (v) => (data.templateId = v),
                    }))),
                    t.div({ className: "col-lg-6" }, field({ label: "Collection", name: "collection" }, select({
                        id: "collection",
                        placeholder: "None (static recipients only)",
                        options: () => data.collections.map((c) => ({ value: c.name, label: c.name })),
                        value: () => data.collection,
                        onchange: (v) => (data.collection = v),
                    }))),
                    t.div({ className: "col-lg-6", hidden: () => !data.collection }, field(
                        { label: "Recipient field", name: "recipientField", hint: "One email per record, sent to this address." },
                        t.input({
                            id: "recipientField",
                            type: "text",
                            className: "txt-code",
                            "html-list": "gpSendPaths",
                            placeholder: "email or owner.email",
                            value: () => data.recipientField,
                            oninput: (e) => (data.recipientField = e.target.value),
                        }),
                    )),
                    t.datalist({ id: "gpSendPaths" }, () => data.paths.map((p) => t.option({ value: p }))),
                    t.div(
                        { className: "col-lg-6", hidden: () => !data.collection },
                        t.label({ className: "gp-label" }, "Records"),
                        t.div(
                            { className: "flex gap-15 gp-events" },
                            [["filter", "All matching the filter"], ["selected", "Selected only"]].map(([value, label]) =>
                                t.div(
                                    { className: "field" },
                                    t.input({
                                        id: "mode_" + value,
                                        type: "radio",
                                        name: "mode",
                                        checked: () => data.mode == value,
                                        onchange: () => (data.mode = value),
                                    }),
                                    t.label({ htmlFor: "mode_" + value }, label),
                                )
                            ),
                        ),
                    ),
                    t.div({ className: "col-lg-12", hidden: () => !data.collection }, field(
                        { label: "Filter", name: "filter" },
                        t.input({
                            id: "filter",
                            type: "text",
                            className: "txt-code",
                            placeholder: "status = 'active' && newsletter = true",
                            value: () => data.filter,
                            oninput: (e) => (data.filter = e.target.value),
                        }),
                    ), t.div({ className: "txt-danger txt-sm", hidden: () => !data.listError }, () => data.listError)),
                    t.div({ className: "col-lg-6" }, field(
                        { label: () => (data.collection ? "Also send each email to" : "Recipients"), name: "staticRecipients" },
                        t.textarea({
                            id: "staticRecipients",
                            rows: 2,
                            placeholder: "a@example.com, b@example.com",
                            value: () => data.staticRecipients,
                            oninput: (e) => (data.staticRecipients = e.target.value),
                        }),
                    )),
                    t.div({ className: "col-lg-6" }, field(
                        { label: "Send at (optional)", name: "sendAt", hint: "Leave empty to send now." },
                        t.input({
                            id: "sendAt",
                            type: "datetime-local",
                            value: () => data.sendAt,
                            oninput: (e) => (data.sendAt = e.target.value),
                        }),
                    )),
                    t.div(
                        { className: "col-lg-12" },
                        switchInput(data, "dedupe", "One email per address (skip duplicates, e.g. customers with several orders)", { id: "gp_send_dedupe" }),
                    ),
                    t.div(
                        { className: "col-lg-12" },
                        t.div(
                            { className: "flex" },
                            t.span({ className: "txt-hint m-r-auto" }, () => {
                                if (data.count) {
                                    return "Recipients: " + describeCount(data.count) + (data.count.sample?.length ? ` (${data.count.sample.slice(0, 3).join(", ")}${data.count.queued > 3 ? "..." : ""})` : "");
                                }
                                if (!data.collection) {
                                    return "";
                                }
                                const more = data.mode == "filter" && data.total > 50 ? " (showing the first 50)" : "";
                                return `${data.targetCount} record(s) targeted${more}`;
                            }),
                            t.button(
                                {
                                    type: "button",
                                    className: () => `btn transparent secondary ${data.isCounting ? "loading" : ""}`,
                                    disabled: () => !data.canSend || data.isCounting,
                                    onclick: countRecipients,
                                },
                                t.i({ className: "ri-group-line", ariaHidden: true }),
                                t.span({ className: "txt" }, "Count recipients"),
                            ),
                            t.button(
                                {
                                    type: "button",
                                    className: () => `btn expanded-lg ${data.isSending ? "loading" : ""}`,
                                    disabled: () => !data.canSend || data.isSending,
                                    onclick: send,
                                },
                                t.i({ className: "ri-send-plane-line", ariaHidden: true }),
                                t.span({ className: "txt" }, "Send"),
                            ),
                        ),
                    ),
                    t.div(
                        { className: "col-lg-12", hidden: () => !data.collection },
                        t.div(
                            { className: "gp-table-wrap" },
                            t.table(
                                null,
                                t.thead(null, t.tr(
                                    null,
                                    t.th({ className: "gp-check-col" }, () =>
                                        data.mode == "selected"
                                            ? checkbox({
                                                id: "gp_send_all",
                                                ariaLabel: "Select all",
                                                checked: () => data.records.length > 0 && data.selectedIds.length == data.records.length,
                                                onchange: (e) => {
                                                    const next = {};
                                                    if (e.target.checked) {
                                                        data.records.forEach((r) => (next[r.id] = true));
                                                    }
                                                    data.selected = next;
                                                },
                                            })
                                            : null
                                    ),
                                    t.th(null, "Record"),
                                    t.th(null, "Id"),
                                )),
                                t.tbody(null, () => {
                                    if (!data.records.length) {
                                        return t.tr({ rid: "empty" }, t.td({ colSpan: 3, className: "txt-center txt-hint" }, "No matching records."));
                                    }
                                    return data.records.map((r) =>
                                        t.tr(
                                            { rid: r.id },
                                            t.td({ className: "gp-check-col" }, () =>
                                                data.mode == "selected"
                                                    ? checkbox({
                                                        id: "gp_send_" + r.id,
                                                        ariaLabel: "Select " + recordLabel(r),
                                                        checked: () => !!data.selected[r.id],
                                                        onchange: (e) => (data.selected[r.id] = e.target.checked),
                                                    })
                                                    : null
                                            ),
                                            t.td(null, recordLabel(r)),
                                            t.td(null, t.span({ className: "txt-code txt-hint txt-sm" }, r.id)),
                                        )
                                    );
                                }),
                            ),
                        ),
                    ),
                ),
            ),
            t.div(
                { className: "col-lg-5" },
                t.div(
                    { className: "gp-panel gp-preview" },
                    t.div({ className: "txt-bold m-b-sm" }, "Preview"),
                    t.div({ className: "alert danger gp-pre", hidden: () => !data.preview?.error }, () => data.preview?.error || ""),
                    t.div(
                        { className: "gp-preview-subject", hidden: () => !!data.preview?.error },
                        t.span({ className: "txt-hint" }, "Subject: "),
                        () => data.preview?.subject || "",
                    ),
                    t.div({ hidden: () => !!data.preview?.error }, emailFrame(() => data.preview?.html || "")),
                    t.div({ className: "field-help" }, "Rendered with the first targeted record."),
                ),
            ),
        ),
    );
}
