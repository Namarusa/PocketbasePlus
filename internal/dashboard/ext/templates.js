import { emailsSidebar, formatDate, handleError, page } from "./lib.js";

export function pageTemplates() {
    app.store.title = "Email templates";

    const data = store({ templates: [], isLoading: true });

    async function load() {
        data.isLoading = true;
        try {
            data.templates = await app.pb.collection("email_templates").getFullList({
                sort: "name",
                fields: "id,name,slug,subject,updated",
                requestKey: "gpTemplates",
            });
        } catch (err) {
            handleError(err);
        }
        data.isLoading = false;
    }

    async function duplicate(tpl) {
        try {
            const full = await app.pb.collection("email_templates").getOne(tpl.id);
            delete full.id;
            await app.pb.collection("email_templates").create({
                ...full,
                name: full.name + " (copy)",
                slug: `${full.slug}-copy-${Date.now().toString(36)}`,
            });
            app.toasts.success("Successfully duplicated the template.");
            load();
        } catch (err) {
            handleError(err);
        }
    }

    function remove(tpl) {
        app.modals.confirm(
            `Do you really want to delete "${tpl.name}"? The automations using it will be deleted too.`,
            async () => {
                try {
                    await app.pb.collection("email_templates").delete(tpl.id);
                    app.toasts.success("Successfully deleted the template.");
                    load();
                } catch (err) {
                    handleError(err);
                }
            },
        );
    }

    load();

    return page(
        {
            pbEvent: "gpPageTemplates",
            sidebar: emailsSidebar(),
            breadcrumbs: ["Emails", "Templates"],
            buttons: t.a(
                { className: "btn expanded-lg", href: "#/emails/templates/new" },
                t.i({ className: "ri-add-line", ariaHidden: true }),
                t.span({ className: "txt" }, "New template"),
            ),
        },
        t.p(
            { className: "txt-hint m-t-0" },
            "HTML email templates with variables like ",
            t.code(null, "{{.record.name}}"),
            ". Use them for manual sends and automations.",
        ),
        t.div(
            { className: "gp-table-wrap" },
            t.table(
                null,
                t.thead(
                    null,
                    t.tr(null, t.th(null, "Name"), t.th(null, "Slug"), t.th(null, "Subject"), t.th(null, "Updated"), t.th(null, "")),
                ),
                t.tbody(null, () => {
                    if (data.isLoading && !data.templates.length) {
                        return t.tr({ rid: "loading" }, t.td({ colSpan: 5, className: "txt-center" }, t.span({ className: "loader" })));
                    }
                    if (!data.templates.length) {
                        return t.tr(
                            { rid: "empty" },
                            t.td(
                                { colSpan: 5, className: "txt-center txt-hint" },
                                "No templates yet. ",
                                t.a({ href: "#/emails/templates/new" }, "Create one"),
                                ".",
                            ),
                        );
                    }
                    return data.templates.map((tpl) =>
                        t.tr(
                            { rid: tpl.id + tpl.updated },
                            t.td(null, t.a({ href: "#/emails/templates/" + tpl.id, className: "txt-bold" }, tpl.name)),
                            t.td(null, t.span({ className: "label sm" }, tpl.slug)),
                            t.td({ className: "txt-hint" }, tpl.subject),
                            t.td({ className: "txt-hint txt-sm txt-nowrap" }, formatDate(tpl.updated)),
                            t.td(
                                { className: "txt-right txt-nowrap" },
                                t.a(
                                    {
                                        className: "btn sm circle transparent secondary",
                                        href: "#/emails/send?template=" + tpl.id,
                                        ariaLabel: app.attrs.tooltip("Send"),
                                    },
                                    t.i({ className: "ri-send-plane-line", ariaHidden: true }),
                                ),
                                t.button(
                                    {
                                        type: "button",
                                        className: "btn sm circle transparent secondary",
                                        ariaLabel: app.attrs.tooltip("Duplicate"),
                                        onclick: () => duplicate(tpl),
                                    },
                                    t.i({ className: "ri-file-copy-line", ariaHidden: true }),
                                ),
                                t.button(
                                    {
                                        type: "button",
                                        className: "btn sm circle transparent danger",
                                        ariaLabel: app.attrs.tooltip("Delete"),
                                        onclick: () => remove(tpl),
                                    },
                                    t.i({ className: "ri-delete-bin-7-line", ariaHidden: true }),
                                ),
                            ),
                        )
                    );
                }),
            ),
        ),
    );
}
