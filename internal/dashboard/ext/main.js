// PocketBasePlus dashboard extension entry point.
//
// PocketBase concatenates this file into /_/extensions.js and runs it before
// the dashboard router starts, so routes and header links must be registered here.
// The pages are ES modules served from /_/extensions/pocketbaseplus/ and loaded on demand.

const extBase = app.pb.buildURL("/_/extensions/pocketbaseplus/");
const load = (file) => import(extBase + file);

document.head.appendChild(t.link({ rel: "stylesheet", href: extBase + "style.css" }));

// The UI extensions API is undocumented upstream: check the internals this file
// relies on so a PocketBase upgrade degrades with a console warning, not a broken dashboard.
const gpWarn = (what) => console.warn(`[pocketbaseplus] ${what} not found, check internal/dashboard/ext/main.js after the PocketBase upgrade`);

// top navigation: right after "Collections" (at the end if it moved)
const gpHeaderLinks = app.store?.headerLinks;
if (!Array.isArray(gpHeaderLinks)) {
    gpWarn("app.store.headerLinks");
}
const collectionsIndex = gpHeaderLinks?.findIndex((l) => l.href == "#/collections") ?? -1;
gpHeaderLinks?.splice(
    collectionsIndex >= 0 ? collectionsIndex + 1 : gpHeaderLinks.length,
    0,
    {
        href: "#/dashboards",
        icon: "ri-dashboard-line",
        label: "Dashboards",
    },
    {
        href: "#/permissions",
        icon: "ri-shield-user-line",
        label: "Permissions",
    },
    {
        href: "#/emails",
        icon: "ri-mail-send-line",
        label: "Emails",
    },
    {
        href: "#/flows",
        icon: "ri-flow-chart",
        label: "Flows",
    },
    {
        href: "#/forms",
        icon: "ri-survey-line",
        label: "Forms",
    },
    {
        href: "#/activity",
        icon: "ri-history-line",
        label: "Activity",
    },
);

// Settings → AI (a group of the native settings sidebar, after "System")
const gpSettingsGroups = app.store?.settingsNavGroups;
if (gpSettingsGroups && typeof gpSettingsGroups == "object") {
    const aiLinks = [
        { href: "#/settings/ai", icon: "ri-plug-line", label: "AI providers" },
        { href: "#/settings/ai/models", icon: "ri-sparkling-2-line", label: "AI models" },
        { href: "#/settings/ai/assistant", icon: "ri-chat-smile-3-line", label: "AI assistant" },
    ];
    const groups = {};
    for (const [name, links] of Object.entries(gpSettingsGroups)) {
        groups[name] = links;
        if (name == "System") {
            groups.AI = aiLinks;
        }
    }
    groups.AI ||= aiLinks;
    app.store.settingsNavGroups = groups;
} else {
    gpWarn("app.store.settingsNavGroups (AI settings link)");
}
app.routes.superuserOnly("#/settings/ai", async (route) => (await load("aiSettings.js")).pageAIProviders(route));
app.routes.superuserOnly("#/settings/ai/models", async (route) => (await load("aiSettings.js")).pageAIModels(route));
app.routes.superuserOnly("#/settings/ai/assistant", async (route) => (await load("aiSettings.js")).pageAIAssistant(route));

// dashboards
app.routes.superuserOnly("#/dashboards", async (route) => (await load("dashboards.js")).pageDashboards(route));
app.routes.superuserOnly("#/dashboards/{id}", async (route) => (await load("dashboards.js")).pageDashboard(route));

// forms
app.routes.superuserOnly("#/forms", async (route) => (await load("forms.js")).pageForms(route));
app.routes.superuserOnly("#/forms/{id}", async (route) => (await load("forms.js")).pageForm(route));

// activity
app.routes.superuserOnly("#/activity", async (route) => (await load("activity.js")).pageActivity(route));

// permissions
app.routes.superuserOnly("#/permissions", async (route) => (await load("permissions.js")).pagePermissions(route));
app.routes.superuserOnly("#/permissions/roles", async (route) => (await load("roles.js")).pageRoles(route));
app.routes.superuserOnly(
    "#/permissions/collections/{collection}",
    async (route) => (await load("permissions.js")).pagePermissions(route),
);

// emails
app.routes.superuserOnly("#/emails", async (route) => (await load("templates.js")).pageTemplates(route));
app.routes.superuserOnly("#/emails/templates", async (route) => (await load("templates.js")).pageTemplates(route));
app.routes.superuserOnly(
    "#/emails/templates/{id}",
    async (route) => (await load("templateEditor.js")).pageTemplateEditor(route),
);
app.routes.superuserOnly("#/emails/automations", async (route) => (await load("automations.js")).pageAutomations(route));
app.routes.superuserOnly("#/emails/send", async (route) => (await load("send.js")).pageSend(route));
app.routes.superuserOnly("#/emails/log", async (route) => (await load("log.js")).pageLog(route));

// flows
app.routes.superuserOnly("#/flows", async (route) => (await load("flows.js")).pageFlows(route));
app.routes.superuserOnly("#/flows/{id}", async (route) => (await load("flowEditor.js")).pageFlowEditor(route));
app.routes.superuserOnly("#/flows/{id}/runs", async (route) => (await load("flowEditor.js")).pageFlowEditor(route));

// "Run flow" button in the native record popup, for manual flows bound to its collection.
// The popup doesn't expose its record, so remember what openRecordUpsert was called with.
let lastOpenedRecord = null;
const openRecordUpsert = app.modals?.openRecordUpsert;
if (typeof openRecordUpsert == "function") {
    app.modals.openRecordUpsert = function(collection, record, ...rest) {
        lastOpenedRecord = { collection, record };
        return openRecordUpsert.call(this, collection, record, ...rest);
    };
    document.addEventListener("mount:recordUpsertModal", async (e) => {
        const ctx = lastOpenedRecord;
        if (ctx?.record?.id && ctx.collection?.name && e.detail) {
            try {
                (await load("activity.js")).injectHistoryButton(e.detail, ctx.collection, ctx.record);
                (await load("recordFlows.js")).injectRunFlowButton(e.detail, ctx.collection, ctx.record);
            } catch (err) {
                console.warn("[pocketbaseplus] failed to add the record popup buttons", err);
            }
        }
    });
} else {
    gpWarn("app.modals.openRecordUpsert (Run flow button disabled)");
}

// custom groups in the collections sidebar (next to the native "System" group)
document.addEventListener("mount:pageSidebar", async (e) => {
    if (e.detail?.classList?.contains("collections-sidebar")) {
        try {
            (await load("collectionGroups.js")).enhanceCollectionsSidebar(e.detail);
        } catch (err) {
            console.warn("[pocketbaseplus] failed to add the collection groups", err);
        }
    }
});

// notifications bell in the app header
document.addEventListener("mount:appHeader", async (e) => {
    try {
        (await load("activity.js")).mountNotifications(e.detail);
    } catch (err) {
        console.warn("[pocketbaseplus] failed to add the notifications", err);
    }
});

// AI assistant button + chat drawer in the app header
document.addEventListener("mount:appHeader", async (e) => {
    try {
        (await load("assistant.js")).mountAssistant(e.detail);
    } catch (err) {
        console.warn("[pocketbaseplus] failed to add the assistant", err);
    }
});

// collection page tools: saved views, layouts, export/import (header) and bulk edit + layouts (records list)
document.addEventListener("mount:pageHeaderSecondaryBtns", async (e) => {
    try {
        (await load("records.js")).mountToolbar(e.detail);
    } catch (err) {
        console.warn("[pocketbaseplus] failed to add the collection tools", err);
    }
});
document.addEventListener("mount:recordsList", async (e) => {
    // only the main list of the collections page (not the relation pickers)
    if (!e.detail?.closest(".page-content")) {
        return;
    }
    try {
        const m = await load("records.js");
        m.mountBulkEdit(e.detail);
        m.mountLayouts(e.detail);
    } catch (err) {
        console.warn("[pocketbaseplus] failed to add the records tools", err);
    }
});
