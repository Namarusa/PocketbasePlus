import { api, emailsSidebar, field, handleError, page, recipientPaths, select, userCollections } from "./lib.js";

const triggers = [
    { value: "record_event", label: "When a record changes", icon: "ri-flashlight-line" },
    { value: "delayed", label: "Some time after a record changes", icon: "ri-timer-line" },
    { value: "schedule", label: "On a schedule", icon: "ri-calendar-schedule-line" },
];

const cronPresets = [
    { value: "0 * * * *", label: "Every hour" },
    { value: "0 9 * * *", label: "Every day at 9:00" },
    { value: "0 9 * * 1", label: "Every Monday at 9:00" },
    { value: "0 9 1 * *", label: "First day of the month at 9:00" },
];

function emptyForm(templateId = "") {
    return {
        id: "",
        name: "",
        active: true,
        template: templateId,
        trigger: "record_event",
        collection: "",
        events: ["create"],
        filter: "",
        cron: "0 9 * * 1",
        delay_minutes: 60,
        recheck_filter: true,
        recipient_field: "",
        static_recipients: "",
    };
}

function describe(a) {
    const where = a.collection ? ` in ${a.collection}` : "";
    const filter = a.filter ? ` where ${a.filter}` : "";
    const events = (a.events || []).join("/");
    switch (a.trigger) {
        case "record_event":
            return `On ${events}${where}${filter}`;
        case "delayed":
            return `${a.delay_minutes} min after ${events}${where}${filter}${a.recheck_filter ? " (rechecked)" : ""}`;
        default:
            return `Cron "${a.cron}"${where}${filter}`;
    }
}

export function pageAutomations() {
    app.store.title = "Email automations";

    const data = store({
        isLoading: true,
        automations: [],
        templates: [],
        collections: [],
        form: emptyForm(),
        paths: [],
        isSaving: false,
    });

    async function load() {
        data.isLoading = true;
        try {
            const [automations, templates, collections] = await Promise.all([
                app.pb.collection("email_automations").getFullList({ sort: "name", requestKey: "gpAutomations" }),
                app.pb.collection("email_templates").getFullList({ sort: "name", fields: "id,name", requestKey: "gpAutoTemplates" }),
                userCollections(),
            ]);
            data.automations = automations;
            data.templates = templates;
            data.collections = collections;
        } catch (err) {
            handleError(err);
        }
        data.isLoading = false;
    }

    const templateName = (id) => data.templates.find((t) => t.id == id)?.name || "-";

    async function save() {
        if (data.isSaving) {
            return;
        }
        data.isSaving = true;
        const { id, ...body } = JSON.parse(JSON.stringify(data.form));
        try {
            if (id) {
                await app.pb.collection("email_automations").update(id, body);
            } else {
                await app.pb.collection("email_automations").create(body);
            }
            app.toasts.success("Successfully saved the automation.");
            app.modals.close(modal, true);
            load();
        } catch (err) {
            handleError(err);
        }
        data.isSaving = false;
    }

    async function toggle(a) {
        try {
            await app.pb.collection("email_automations").update(a.id, { active: !a.active });
            load();
        } catch (err) {
            handleError(err);
        }
    }

    async function runNow(a) {
        try {
            const res = await api(`/api/emails/automations/${a.id}/run`, { method: "POST" });
            app.toasts.success(`${res.queued} email(s) queued.`);
        } catch (err) {
            handleError(err);
        }
    }

    function remove(a) {
        app.modals.confirm(`Do you really want to delete the automation "${a.name}"?`, async () => {
            try {
                await app.pb.collection("email_automations").delete(a.id);
                app.toasts.success("Successfully deleted the automation.");
                load();
            } catch (err) {
                handleError(err);
            }
        });
    }

    function openForm(a) {
        app.store.errors = {};
        data.form = a
            ? {
                ...emptyForm(),
                ...JSON.parse(JSON.stringify(a)),
                cron: a.cron || "0 9 * * 1",
                delay_minutes: a.delay_minutes || 60,
            }
            : emptyForm(data.templates[0]?.id);
        app.modals.open(modal);
    }

    function toggleEvent(ev, checked) {
        data.form.events = checked ? [...data.form.events, ev] : data.form.events.filter((x) => x != ev);
    }

    const modal = t.div(
        {
            className: "modal popup lg gp-automation-modal",
            onmount: () => {
                modal._w = watch(() => data.form.collection, async (c) => {
                    data.paths = c ? await recipientPaths(c) : [];
                });
            },
            onunmount: () => modal._w?.unwatch(),
        },
        t.header({ className: "modal-header" }, t.h5({ className: "m-auto" }, () => (data.form.id ? "Edit automation" : "New automation"))),
        t.form(
            {
                id: "gpAutomationForm",
                className: "modal-content",
                inert: () => data.isSaving,
                onsubmit: (e) => {
                    e.preventDefault();
                    save();
                },
            },
            t.div(
                { className: "grid" },
                t.div({ className: "col-lg-6" }, field({ label: "Name", name: "name" }, t.input({
                    id: "name",
                    name: "name",
                    type: "text",
                    required: true,
                    value: () => data.form.name,
                    oninput: (e) => (data.form.name = e.target.value),
                }))),
                t.div({ className: "col-lg-6" }, field({ label: "Template", name: "template" }, select({
                    id: "template",
                    name: "template",
                    required: true,
                    options: () => data.templates.map((tpl) => ({ value: tpl.id, label: tpl.name })),
                    value: () => data.form.template,
                    onchange: (v) => (data.form.template = v),
                }))),
                t.div(
                    { className: "col-lg-12" },
                    t.label({ className: "gp-label" }, "Trigger"),
                    t.div(
                        { className: "gp-segmented" },
                        triggers.map((tr) =>
                            t.label(
                                { className: () => `gp-segment ${data.form.trigger == tr.value ? "active" : ""}` },
                                t.input({
                                    type: "radio",
                                    name: "trigger",
                                    value: tr.value,
                                    checked: () => data.form.trigger == tr.value,
                                    onchange: () => (data.form.trigger = tr.value),
                                }),
                                t.i({ className: tr.icon, ariaHidden: true }),
                                t.span({ className: "txt" }, tr.label),
                            )
                        ),
                    ),
                ),
                t.div({ className: "col-lg-6" }, field(
                    {
                        label: "Collection",
                        name: "collection",
                        hint: () => (data.form.trigger == "schedule" ? "Optional for schedules." : ""),
                    },
                    select({
                        id: "collection",
                        name: "collection",
                        required: () => data.form.trigger != "schedule",
                        placeholder: () => (data.form.trigger == "schedule" ? "None (static recipients only)" : "- Select -"),
                        options: () => data.collections.map((c) => ({ value: c.name, label: c.name })),
                        value: () => data.form.collection,
                        onchange: (v) => (data.form.collection = v),
                    }),
                )),
                t.div(
                    { className: "col-lg-6", hidden: () => data.form.trigger == "schedule" },
                    t.label({ className: "gp-label" }, "Events"),
                    t.div(
                        { className: "flex gap-15 gp-events", "html-data-field": "events" },
                        ["create", "update", "delete"].map((ev) =>
                            t.div(
                                { className: "field" },
                                t.input({
                                    id: "ev_" + ev,
                                    name: "events",
                                    type: "checkbox",
                                    checked: () => data.form.events.includes(ev),
                                    onchange: (e) => toggleEvent(ev, e.target.checked),
                                }),
                                t.label({ htmlFor: "ev_" + ev }, ev),
                            )
                        ),
                    ),
                ),
                t.div(
                    { className: "col-lg-6", hidden: () => data.form.trigger != "schedule" },
                    field(
                        { label: "Schedule (cron, UTC)", name: "cron" },
                        t.div(
                            { className: "flex gap-5" },
                            t.input({
                                id: "cron",
                                name: "cron",
                                type: "text",
                                className: "txt-code",
                                value: () => data.form.cron,
                                oninput: (e) => (data.form.cron = e.target.value),
                            }),
                            select({
                                className: "gp-presets",
                                placeholder: "Presets...",
                                options: cronPresets,
                                value: () => "", // an action, not a value: always shows the placeholder
                                onchange: (v) => (data.form.cron = v),
                            }),
                        ),
                    ),
                ),
                t.div(
                    { className: "col-lg-12", hidden: () => !data.form.collection },
                    field(
                        { label: "Filter (optional PocketBase filter)", name: "filter" },
                        t.input({
                            id: "filter",
                            name: "filter",
                            type: "text",
                            className: "txt-code",
                            placeholder: "status = 'paid' && total > 100",
                            value: () => data.form.filter,
                            oninput: (e) => (data.form.filter = e.target.value),
                        }),
                    ),
                ),
                t.div(
                    { className: "col-lg-6", hidden: () => data.form.trigger != "delayed" },
                    field(
                        { label: "Delay (minutes)", name: "delay_minutes", hint: "1440 = 1 day, 10080 = 1 week." },
                        t.input({
                            id: "delay_minutes",
                            name: "delay_minutes",
                            type: "number",
                            min: 1,
                            step: 1,
                            value: () => data.form.delay_minutes,
                            oninput: (e) => (data.form.delay_minutes = parseInt(e.target.value, 10) || 0),
                        }),
                    ),
                ),
                t.div(
                    { className: "col-lg-6", hidden: () => data.form.trigger != "delayed" },
                    t.div(
                        { className: "field gp-recheck" },
                        t.input({
                            id: "recheck_filter",
                            name: "recheck_filter",
                            type: "checkbox",
                            className: "switch",
                            checked: () => data.form.recheck_filter,
                            onchange: (e) => (data.form.recheck_filter = e.target.checked),
                        }),
                        t.label({ htmlFor: "recheck_filter" }, t.span({ className: "txt" }, "Recheck the filter at send time")),
                    ),
                    t.div(
                        { className: "field-help" },
                        `Skip the email if the record was deleted or no longer matches (e.g. "3 days after signup if still unverified"). The email is re-rendered with fresh data.`,
                    ),
                ),
                t.div({ className: "col-lg-6" }, field(
                    {
                        label: "Recipient field",
                        name: "recipient_field",
                        hint: () =>
                            data.form.trigger == "schedule"
                                ? "Set it to send one email per matching record. Leave it empty to send one digest (matches in .records) to the static recipients."
                                : "The record field holding the address, or relation.email.",
                    },
                    t.input({
                        id: "recipient_field",
                        name: "recipient_field",
                        type: "text",
                        className: "txt-code",
                        "html-list": "gpRecipientPaths",
                        disabled: () => !data.form.collection,
                        placeholder: () => (data.form.collection ? "email or owner.email" : "Select a collection first"),
                        value: () => data.form.recipient_field,
                        oninput: (e) => (data.form.recipient_field = e.target.value),
                    }),
                )),
                t.datalist({ id: "gpRecipientPaths" }, () => data.paths.map((p) => t.option({ value: p }))),
                t.div({ className: "col-lg-6" }, field(
                    { label: "Static recipients", name: "static_recipients", hint: "Comma or newline separated." },
                    t.textarea({
                        id: "static_recipients",
                        name: "static_recipients",
                        rows: 2,
                        placeholder: "team@example.com, boss@example.com",
                        value: () => data.form.static_recipients,
                        oninput: (e) => (data.form.static_recipients = e.target.value),
                    }),
                )),
                t.div({ className: "col-lg-12" }, t.div(
                    { className: "field" },
                    t.input({
                        id: "active",
                        name: "active",
                        type: "checkbox",
                        className: "switch",
                        checked: () => data.form.active,
                        onchange: (e) => (data.form.active = e.target.checked),
                    }),
                    t.label({ htmlFor: "active" }, t.span({ className: "txt" }, "Active")),
                )),
            ),
        ),
        t.footer(
            { className: "modal-footer" },
            t.button({ type: "button", className: "btn transparent secondary m-r-auto", onclick: () => app.modals.close(modal) }, t.span({ className: "txt" }, "Close")),
            t.button(
                { type: "submit", "html-form": "gpAutomationForm", className: () => `btn expanded ${data.isSaving ? "loading" : ""}`, disabled: () => data.isSaving },
                t.span({ className: "txt" }, () => (data.form.id ? "Save changes" : "Create")),
            ),
        ),
    );
    load();

    return page(
        {
            pbEvent: "gpPageAutomations",
            sidebar: emailsSidebar(),
            breadcrumbs: ["Emails", "Automations"],
            buttons: t.button(
                { type: "button", className: "btn expanded-lg", disabled: () => !data.templates.length, onclick: () => openForm() },
                t.i({ className: "ri-add-line", ariaHidden: true }),
                t.span({ className: "txt" }, "New automation"),
            ),
            className: "gp-page-automations",
        },
        t.p({ className: "txt-hint m-t-0" }, "Send templates automatically from any collection, without per-collection code."),
        t.div(
            { className: "alert info m-b-sm", hidden: () => data.isLoading || data.templates.length },
            "Create an ",
            t.a({ href: "#/emails/templates/new" }, "email template"),
            " first.",
        ),
        t.div(
            { className: "gp-table-wrap" },
            t.table(
                null,
                t.thead(null, t.tr(null, t.th(null, "Name"), t.th(null, "When"), t.th(null, "Template"), t.th(null, "To"), t.th(null, "Status"), t.th(null, ""))),
                t.tbody(null, () => {
                    if (data.isLoading && !data.automations.length) {
                        return t.tr({ rid: "loading" }, t.td({ colSpan: 6, className: "txt-center" }, t.span({ className: "loader" })));
                    }
                    if (!data.automations.length) {
                        return t.tr({ rid: "empty" }, t.td({ colSpan: 6, className: "txt-center txt-hint" }, "No automations yet."));
                    }
                    return data.automations.map((a) =>
                        t.tr(
                            { rid: a.id + a.updated },
                            t.td(null, t.strong(null, a.name)),
                            t.td({ className: "txt-hint" }, describe(a)),
                            t.td(null, templateName(a.template)),
                            t.td({ className: "txt-hint" }, [a.recipient_field && `field ${a.recipient_field}`, a.static_recipients].filter(Boolean).join(" + ")),
                            t.td(
                                null,
                                t.button(
                                    {
                                        type: "button",
                                        className: `label sm gp-status-toggle ${a.active ? "success" : ""}`,
                                        ariaDescription: app.attrs.tooltip("Click to toggle"),
                                        onclick: () => toggle(a),
                                    },
                                    a.active ? "active" : "paused",
                                ),
                            ),
                            t.td(
                                { className: "txt-right txt-nowrap" },
                                a.trigger == "schedule"
                                    ? t.button(
                                        { type: "button", className: "btn sm circle transparent secondary", ariaLabel: app.attrs.tooltip("Run now"), onclick: () => runNow(a) },
                                        t.i({ className: "ri-play-large-line", ariaHidden: true }),
                                    )
                                    : null,
                                t.button(
                                    { type: "button", className: "btn sm circle transparent secondary", ariaLabel: app.attrs.tooltip("Edit"), onclick: () => openForm(a) },
                                    t.i({ className: "ri-pencil-line", ariaHidden: true }),
                                ),
                                t.button(
                                    { type: "button", className: "btn sm circle transparent danger", ariaLabel: app.attrs.tooltip("Delete"), onclick: () => remove(a) },
                                    t.i({ className: "ri-delete-bin-7-line", ariaHidden: true }),
                                ),
                            ),
                        )
                    );
                }),
            ),
        ),
        modal,
    );
}
