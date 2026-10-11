import { formatDate, handleError, page, sidebar } from "./lib.js";

export const triggerTypes = {
    record: { label: "Record event", icon: "ri-database-2-line", hint: "When records are created, updated or deleted" },
    manual: { label: "Manual", icon: "ri-cursor-line", hint: "From a button on records, or run from here" },
    webhook: { label: "Webhook", icon: "ri-webhook-line", hint: "When an external service calls a secret URL" },
    schedule: { label: "Schedule", icon: "ri-calendar-schedule-line", hint: "On a cron schedule" },
    flow: { label: "Another flow", icon: "ri-git-branch-line", hint: "Called by a \"Run flow\" step" },
    action: { label: "Page action", icon: "ri-pages-line", hint: "From a button or form of the pages (sign in, actions...)" },
};

export function describeTrigger(f) {
    const cfg = f.trigger || {};
    switch (f.trigger_type) {
        case "record": {
            const timing = cfg.timing == "before" ? "before" : "after";
            return `${timing} ${(cfg.events || []).join("/")} in ${cfg.collection || "?"}`;
        }
        case "manual":
            return cfg.collection ? `Button on ${cfg.collection} records` : "Run manually";
        case "webhook":
            return `${cfg.method || "POST"} webhook${cfg.wait ? " (waits for the result)" : ""}`;
        case "schedule":
            return `Cron "${cfg.cron || ""}"`;
        case "action":
            return `Page action (${cfg.access == "users" ? "signed-in users" : cfg.access == "roles" ? "some roles" : "public"})`;
        default:
            return "Called by other flows";
    }
}

// shared between the sidebar and the pages
const shared = store({ flows: [], loaded: false });
let loading = null;

export function loadFlows() {
    if (!loading) {
        loading = app.pb
            .collection("flows")
            .getFullList({ sort: "name", fields: "id,name,active,trigger_type,trigger,updated", requestKey: null })
            .then((list) => (shared.flows = list))
            .catch(handleError)
            .finally(() => {
                shared.loaded = true;
                loading = null;
            });
    }
    return loading;
}

export function flowsSidebar() {
    if (!shared.loaded) {
        loadFlows();
    }
    return sidebar("gpFlowsSidebar", () => [
        {
            name: "Flows",
            empty: "No flows yet.",
            links: [
                { href: "#/flows/new", label: "New flow", icon: "ri-add-line", exact: true },
                ...shared.flows.map((f) => ({
                    href: "#/flows/" + f.id,
                    label: f.name,
                    icon: triggerTypes[f.trigger_type]?.icon,
                    after: t.span({ className: `gp-dot ${f.active ? "on" : ""}`, title: f.active ? "Active" : "Inactive" }),
                })),
            ],
        },
        // optional APIs module (internal/apisources)
        ...(globalThis.gpAPIs ? [{ name: "Integrations", links: [{ href: "#/apis", label: "APIs", icon: "ri-plug-line" }] }] : []),
    ]);
}

export function pageFlows() {
    app.store.title = "Flows";

    const data = store({ lastRuns: {} });

    async function loadLastRuns() {
        try {
            const runs = await app.pb.collection("flow_runs").getList(1, 200, {
                sort: "-created",
                fields: "id,flow,status,created",
                requestKey: "gpFlowLastRuns",
            });
            const last = {};
            for (const r of runs.items) {
                if (!last[r.flow]) {
                    last[r.flow] = r;
                }
            }
            data.lastRuns = last;
        } catch (err) {
            handleError(err);
        }
    }

    async function toggle(f) {
        try {
            await app.pb.collection("flows").update(f.id, { active: !f.active });
            loadFlows();
        } catch (err) {
            handleError(err);
        }
    }

    loadFlows();
    loadLastRuns();

    const statusClass = { success: "success", failed: "danger", rejected: "warning", running: "info" };

    return page(
        {
            pbEvent: "gpPageFlows",
            sidebar: flowsSidebar(),
            breadcrumbs: ["Flows"],
            buttons: t.a(
                { className: "btn expanded-lg", href: "#/flows/new" },
                t.i({ className: "ri-add-line", ariaHidden: true }),
                t.span({ className: "txt" }, "New flow"),
            ),
        },
        t.p(
            { className: "txt-hint m-t-0" },
            "Automate work with a trigger (record event, button, webhook, schedule) and a list of steps: conditions, loops, record changes, emails, HTTP requests, scripts...",
        ),
        t.div(
            { className: "gp-table-wrap" },
            t.table(
                null,
                t.thead(null, t.tr(null, t.th(null, "Name"), t.th(null, "Trigger"), t.th(null, "Last run"), t.th(null, "Status"))),
                t.tbody(null, () => {
                    if (!shared.loaded) {
                        return t.tr({ rid: "loading" }, t.td({ colSpan: 4, className: "txt-center" }, t.span({ className: "loader" })));
                    }
                    if (!shared.flows.length) {
                        return t.tr(
                            { rid: "empty" },
                            t.td({ colSpan: 4, className: "txt-center txt-hint" }, "No flows yet. ", t.a({ href: "#/flows/new" }, "Create one"), "."),
                        );
                    }
                    return shared.flows.map((f) =>
                        t.tr(
                            { rid: f.id + f.updated },
                            t.td(
                                null,
                                t.a(
                                    { href: "#/flows/" + f.id, className: "txt-bold flex gap-5" },
                                    t.i({ className: triggerTypes[f.trigger_type]?.icon, ariaHidden: true }),
                                    t.span(null, f.name),
                                ),
                            ),
                            t.td({ className: "txt-hint" }, describeTrigger(f)),
                            t.td(null, () => {
                                const run = data.lastRuns[f.id];
                                if (!run) {
                                    return t.span({ className: "txt-hint txt-sm" }, "Never");
                                }
                                return t.a(
                                    { href: `#/flows/${f.id}/runs`, className: "flex gap-5 txt-sm" },
                                    t.span({ className: `label sm ${statusClass[run.status] || ""}` }, run.status),
                                    t.span({ className: "txt-hint" }, formatDate(run.created)),
                                );
                            }),
                            t.td(
                                null,
                                t.button(
                                    {
                                        type: "button",
                                        className: `label sm gp-status-toggle ${f.active ? "success" : ""}`,
                                        ariaDescription: app.attrs.tooltip("Click to toggle"),
                                        onclick: () => toggle(f),
                                    },
                                    f.active ? "active" : "inactive",
                                ),
                            ),
                        )
                    );
                }),
            ),
        ),
    );
}
