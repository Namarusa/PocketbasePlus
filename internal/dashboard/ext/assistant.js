// AI assistant: a header button opening a chat drawer. The model reads the
// app with tools and proposes changes as cards that the superuser applies.

import { api, emailFrame, handleError } from "./lib.js";

const ui = store({
    open: false,
    status: null, // {ready, model, error}
    conv: null, // {id, title, messages, actions}
    history: [],
    historyOpen: false,
    busy: false,
    busyAction: "",
    input: "",
});

let drawerEl = null;
let bodyEl = null;
let inputEl = null;

const suggestions = [
    "Create a dashboard of the orders of the last 30 days",
    "Which products sold the most this month?",
    "Prepare an email campaign for the customers who ordered in the last 3 days",
    "What can you do?",
];

export function mountAssistant(headerEl) {
    if (headerEl._gpAssistant) {
        return;
    }
    headerEl._gpAssistant = true;
    const btn = t.button(
        {
            type: "button",
            className: () => `header-link gp-assistant-btn ${ui.open ? "active" : ""}`,
            ariaLabel: app.attrs.tooltip("AI assistant"),
            ariaExpanded: () => (ui.open ? "true" : "false"),
            onclick: toggle,
        },
        t.i({ className: "ri-sparkling-2-line", ariaHidden: true }),
        t.span({ className: "txt" }, "Assistant"),
    );
    const place = () => {
        const anchor = headerEl.querySelector(".gp-bell") || headerEl.querySelector(".header-link.logged-user");
        if (anchor && btn.nextElementSibling !== anchor) {
            anchor.before(btn);
        }
    };
    place();
    new MutationObserver(place).observe(headerEl, { childList: true });

    if (!drawerEl) {
        drawerEl = drawer();
        document.body.appendChild(drawerEl);
        document.addEventListener("keydown", (e) => {
            if (e.key == "Escape" && ui.open && !document.querySelector(".modal[data-modal-state=open]")) {
                ui.open = false;
            }
        });
    }
}

function toggle() {
    ui.open = !ui.open;
    if (ui.open) {
        loadStatus();
        loadHistory();
        setTimeout(() => inputEl?.focus(), 50);
    }
}

async function loadStatus() {
    try {
        ui.status = await api("/api/assistant/status", { requestKey: null });
    } catch (err) {
        ui.status = { ready: false, error: err.message };
    }
}

async function loadHistory() {
    try {
        const res = await app.pb.collection("ai_conversations").getList(1, 20, {
            filter: app.pb.filter("owner = {:id}", { id: app.store.superuser?.id || "" }),
            sort: "-updated",
            fields: "id,title,updated",
            requestKey: null,
        });
        ui.history = res.items;
    } catch (err) {
        handleError(err);
    }
}

async function openConversation(id) {
    ui.historyOpen = false;
    try {
        const c = await app.pb.collection("ai_conversations").getOne(id, { requestKey: null });
        ui.conv = { id: c.id, title: c.title, messages: c.messages || [], actions: c.actions || [] };
        scrollDown();
    } catch (err) {
        handleError(err);
    }
}

function newChat() {
    ui.conv = null;
    ui.historyOpen = false;
    ui.input = "";
    setTimeout(() => inputEl?.focus(), 0);
}

function scrollDown() {
    setTimeout(() => bodyEl && (bodyEl.scrollTop = bodyEl.scrollHeight), 30);
}

// the page of the superuser, as context for the model
function pageContext() {
    const hash = window.location.hash || "";
    const query = new URLSearchParams(hash.split("?")[1] || "");
    const id = (re) => re.exec(hash)?.[1] || "";
    return {
        path: hash.split("?")[0],
        collection: query.get("collection") || "",
        record: query.get("record") || "",
        dashboard: id(/^#\/dashboards\/([a-z0-9]{15})/),
        flow: id(/^#\/flows\/([a-z0-9]{15})/),
        page: id(/^#\/pages\/([a-z0-9]{15})/),
        template: id(/^#\/emails\/templates\/([a-z0-9]{15})/),
    };
}

async function send(text) {
    const message = (text ?? ui.input).trim();
    if (!message || ui.busy) {
        return;
    }
    ui.input = "";
    const conv = ui.conv || { id: "", title: "", messages: [], actions: [] };
    // shown right away, replaced by the server copy
    ui.conv = { ...conv, messages: [...conv.messages, { role: "user", content: message }] };
    ui.busy = true;
    scrollDown();
    try {
        const res = await api("/api/assistant/chat", {
            method: "POST",
            body: { conversation: conv.id, message, page: pageContext() },
            requestKey: null,
        });
        ui.conv = res;
        if (res.error) {
            app.toasts.error(res.error);
        }
        loadHistory();
    } catch (err) {
        handleError(err);
        ui.conv = conv.messages.length ? conv : null;
        ui.input = message;
    }
    ui.busy = false;
    scrollDown();
    setTimeout(() => inputEl?.focus(), 0);
}

async function runAction(act, verb) {
    const doIt = async () => {
        ui.busyAction = act.id;
        try {
            const res = await api(`/api/assistant/conversations/${ui.conv.id}/actions/${act.id}/${verb}`, { method: "POST", requestKey: null });
            ui.conv = res;
            if (res.error) {
                app.toasts.error(res.error);
            } else if (verb == "apply") {
                app.toasts.success("Done: " + act.title);
            }
        } catch (err) {
            handleError(err);
        }
        ui.busyAction = "";
    };
    if (verb == "apply" && act.type == "campaign") {
        app.modals.confirm(`Send “${act.details?.subject || act.title}” to ${act.details?.recipients ?? "the"} recipient(s)?`, doIt);
    } else if (verb == "apply" && act.type == "flow_run") {
        app.modals.confirm(`Run the flow now? Its steps really run (records are written, emails sent...).`, doIt);
    } else {
        doIt();
    }
}

// --- rendering --------------------------------------------------------------------

function drawer() {
    return t.aside(
        { className: () => `gp-assistant ${ui.open ? "open" : ""}`, ariaLabel: "AI assistant", inert: () => !ui.open },
        t.header(
            { className: "gp-assistant-head" },
            t.i({ className: "ri-sparkling-2-line gp-assistant-logo", ariaHidden: true }),
            t.div(
                { className: "gp-assistant-title" },
                t.strong(null, () => ui.conv?.title || "Assistant"),
                t.small({ className: "txt-hint" }, () => (ui.status?.ready ? ui.status.model : "")),
            ),
            t.button(
                { type: "button", className: "btn sm circle transparent secondary", ariaLabel: app.attrs.tooltip("Conversations"), onclick: () => (ui.historyOpen = !ui.historyOpen) },
                t.i({ className: "ri-history-line", ariaHidden: true }),
            ),
            t.button(
                { type: "button", className: "btn sm circle transparent secondary", ariaLabel: app.attrs.tooltip("New conversation"), onclick: newChat },
                t.i({ className: "ri-chat-new-line", ariaHidden: true }),
            ),
            t.button(
                { type: "button", className: "btn sm circle transparent secondary", ariaLabel: app.attrs.tooltip("Close"), onclick: () => (ui.open = false) },
                t.i({ className: "ri-close-line", ariaHidden: true }),
            ),
        ),
        () => {
            if (!ui.historyOpen) {
                return null;
            }
            return t.div(
                { rid: "history-" + ui.history.map((h) => h.id + h.updated).join(), className: "gp-assistant-history" },
                ui.history.length
                    ? ui.history.map((h) =>
                        t.button(
                            { type: "button", className: () => `gp-assistant-history-item ${ui.conv?.id == h.id ? "active" : ""}`, onclick: () => openConversation(h.id) },
                            t.span({ className: "txt" }, h.title || "Untitled"),
                            t.small({ className: "txt-hint" }, new Date(h.updated.replace(" ", "T")).toLocaleDateString()),
                        )
                    )
                    : t.div({ className: "txt-hint p-sm" }, "No conversations yet."),
            );
        },
        t.div(
            { className: "gp-assistant-body", onmount: (el) => (bodyEl = el) },
            () => renderBody(),
        ),
        t.form(
            {
                className: "gp-assistant-composer",
                onsubmit: (e) => {
                    e.preventDefault();
                    send();
                },
            },
            t.textarea({
                rows: 2,
                ariaLabel: "Message",
                placeholder: "Ask about your data, or what to prepare… (Enter to send)",
                value: () => ui.input,
                disabled: () => ui.busy || ui.status?.ready === false,
                onmount: (el) => (inputEl = el),
                oninput: (e) => (ui.input = e.target.value),
                onkeydown: (e) => {
                    if (e.key == "Enter" && !e.shiftKey && !e.isComposing) {
                        e.preventDefault();
                        send();
                    }
                },
            }),
            t.button(
                {
                    type: "submit",
                    className: () => `btn circle gp-assistant-send ${ui.busy ? "loading" : ""}`,
                    ariaLabel: app.attrs.tooltip("Send"),
                    disabled: () => ui.busy || !ui.input.trim(),
                },
                t.i({ className: "ri-arrow-up-line", ariaHidden: true }),
            ),
        ),
    );
}

function renderBody() {
    if (ui.status && !ui.status.ready) {
        return t.div(
            { rid: "notready", className: "gp-assistant-empty" },
            t.div({ className: "alert warning" }, ui.status.error || "The assistant isn't configured."),
            t.a({ href: "#/settings/ai/assistant", className: "btn sm", onclick: () => (ui.open = false) }, t.span({ className: "txt" }, "Set up the assistant")),
        );
    }
    const c = ui.conv;
    if (!c || !c.messages.length) {
        return t.div(
            { rid: "welcome", className: "gp-assistant-empty" },
            t.i({ className: "ri-sparkling-2-line gp-assistant-big", ariaHidden: true }),
            t.p(null, "Ask questions about your data, or ask me to prepare dashboards, email templates and campaigns. Nothing changes until you click ", t.strong(null, "Apply"), "."),
            t.div(
                { className: "gp-assistant-suggestions" },
                suggestions.map((s) => t.button({ type: "button", className: "gp-assistant-chip", onclick: () => send(s) }, s)),
            ),
        );
    }
    const sig = [c.id, c.messages.length, c.actions.map((a) => a.id + a.status).join(), ui.busy ? 1 : 0, ui.busyAction].join(":");
    return t.div({ rid: "conv-" + sig, className: "gp-assistant-msgs" }, ...renderMessages(c), ui.busy ? thinking() : null);
}

function thinking() {
    return t.div({ className: "gp-amsg assistant" }, t.div({ className: "gp-assistant-thinking" }, t.span({ className: "loader sm" }), t.span({ className: "txt-hint" }, "Working…")));
}

function renderMessages(c) {
    const results = {};
    for (const m of c.messages) {
        if (m.role == "tool") {
            results[m.tool_call_id] = m.content;
        }
    }
    const out = [];
    for (const m of c.messages) {
        if (m.role == "user") {
            out.push(t.div({ className: "gp-amsg user" }, t.div({ className: "gp-amsg-bubble" }, m.content)));
        } else if (m.role == "assistant") {
            if (m.tool_calls?.length) {
                out.push(t.div({ className: "gp-asteps" }, m.tool_calls.map((call) => toolStep(call, results[call.id], c))));
            }
            if (m.content) {
                out.push(t.div({ className: "gp-amsg assistant" }, t.div({ className: "gp-amsg-bubble gp-md", innerHTML: markdown(m.content) })));
            }
        }
    }
    return out;
}

function parse(json) {
    try {
        return JSON.parse(json);
    } catch {
        return null;
    }
}

const stepIcons = {
    describe_collection: "ri-database-2-line",
    query_records: "ri-table-line",
    aggregate: "ri-functions",
    relative_date: "ri-calendar-line",
    list_resources: "ri-list-check",
    preview_email: "ri-mail-line",
    propose_dashboard: "ri-dashboard-line",
    propose_email_template: "ri-mail-add-line",
    propose_campaign: "ri-send-plane-line",
    propose_record: "ri-add-box-line",
    describe_functions: "ri-functions",
    get_flow: "ri-flow-chart",
    flow_runs: "ri-history-line",
    propose_flow: "ri-flow-chart",
    propose_flow_run: "ri-play-circle-line",
    get_page: "ri-pages-line",
    propose_page: "ri-pages-line",
    propose_page_change: "ri-pages-line",
};

function stepLabel(name, args) {
    switch (name) {
        case "describe_collection":
            return `Read the fields of ${args.collection}`;
        case "query_records":
            return `Read ${args.collection} records`;
        case "aggregate":
            return `Computed the ${args.fn || "count"} of ${args.collection}${args.group_by ? " by " + args.group_by : ""}`;
        case "relative_date":
            return "Computed a date";
        case "list_resources":
            return `Listed the ${String(args.kind || "").replace("_", " ")}`;
        case "preview_email":
            return "Previewed an email";
        case "propose_dashboard":
            return "Prepared a dashboard";
        case "propose_email_template":
            return "Prepared an email template";
        case "propose_campaign":
            return "Prepared an email campaign";
        case "propose_record":
            return `Prepared a ${args.collection} record`;
        case "describe_functions":
            return "Read the functions";
        case "get_flow":
            return `Read the flow ${args.flow_id || ""}`;
        case "flow_runs":
            return args.run_id ? "Read a run log" : "Read the flow runs";
        case "propose_flow":
            return args.flow_id ? "Prepared changes to a flow" : "Prepared a flow";
        case "propose_flow_run":
            return "Prepared a test run";
        case "get_page":
            return `Read the page ${args.page || ""}`;
        case "propose_page":
            return "Prepared a page";
        case "propose_page_change":
            return "Prepared changes to a page";
    }
    return name;
}

function toolStep(call, resultJSON, c) {
    const args = parse(call.function?.arguments) || {};
    const result = parse(resultJSON);
    const failed = !!result?.error;
    const action = result?.proposed ? c.actions.find((a) => a.id == result.proposed) : null;
    return t.div(
        { className: "gp-astep-wrap" },
        t.details(
            { className: `gp-astep ${failed ? "failed" : ""}` },
            t.summary(
                null,
                t.i({ className: failed ? "ri-error-warning-line" : stepIcons[call.function?.name] || "ri-tools-line", ariaHidden: true }),
                t.span({ className: "txt" }, stepLabel(call.function?.name, args)),
                failed ? t.span({ className: "txt-danger" }, " · retried") : null,
            ),
            t.pre({ className: "gp-astep-raw" }, JSON.stringify(args, null, 2) + "\n→ " + (resultJSON || "").slice(0, 3000)),
        ),
        action ? actionCard(action) : null,
    );
}

const actionIcons = {
    dashboard: "ri-dashboard-line",
    email_template: "ri-mail-add-line",
    campaign: "ri-send-plane-line",
    record: "ri-add-box-line",
    flow: "ri-flow-chart",
    flow_run: "ri-play-circle-line",
    page: "ri-pages-line",
};
const statusLabels = { pending: "Waiting for you", applied: "Applied", discarded: "Discarded", failed: "Failed" };
const statusClass = { pending: "info", applied: "success", discarded: "", failed: "danger" };

function actionCard(act) {
    const d = act.details || {};
    const busy = ui.busyAction == act.id;
    let body = null;
    switch (act.type) {
        case "dashboard":
            body = t.ul(
                { className: "gp-action-list" },
                (d.panels || []).map((p) => t.li(null, t.strong(null, p.name), t.span({ className: "txt-hint" }, ` · ${p.type}${p.preview ? " · " + p.preview : ""}`))),
            );
            break;
        case "email_template":
        case "campaign":
            body = t.div(
                null,
                d.warning ? t.div({ className: "alert warning m-b-5" }, d.warning) : null,
                d.sample?.length ? t.div({ className: "txt-sm m-b-5" }, t.span({ className: "txt-hint" }, "Recipients: "), d.sample.slice(0, 5).join(", ") + (d.recipients > 5 ? `… (${d.recipients})` : "")) : null,
                t.div({ className: "txt-sm m-b-5" }, t.span({ className: "txt-hint" }, "Subject: "), d.subject || ""),
                emailFrame(d.html || "", "gp-action-frame"),
            );
            break;
        case "flow":
            body = t.div(
                null,
                t.div({ className: "txt-sm m-b-5" }, t.span({ className: "txt-hint" }, "Trigger: "), d.trigger || ""),
                t.pre({ className: "gp-action-outline" }, (d.outline || []).join("\n") || "(no steps)"),
            );
            break;
        case "page":
            body = t.div(
                null,
                t.div({ className: "txt-sm m-b-5" }, t.span({ className: "txt-hint" }, "Address: "), d.address || ""),
                d.preview ? emailFrame(d.preview, "gp-action-frame gp-action-page") : d.preview_note ? t.div({ className: "txt-hint txt-sm" }, d.preview_note) : null,
                act.status == "applied" && act.result?.url ? t.a({ href: act.result.url, target: "_blank", rel: "noopener", className: "txt-sm" }, "Open the page ↗") : null,
            );
            break;
        case "flow_run":
            body = t.div(
                null,
                t.div({ className: "txt-sm txt-hint" }, "Trigger:"),
                t.pre({ className: "gp-action-outline" }, d.trigger || "{}"),
                act.status == "applied" && act.result
                    ? t.div(
                          { className: "txt-sm m-t-5" },
                          t.span({ className: `label sm ${act.result.status == "success" ? "success" : "danger"}` }, act.result.status),
                          act.result.error ? t.span({ className: "txt-hint" }, " " + act.result.error) : null,
                      )
                    : null,
            );
            break;
        case "record":
            body = t.table(
                { className: "gp-action-table" },
                t.tbody(null, Object.entries(d.data || {}).map(([k, v]) => t.tr(null, t.th(null, k), t.td(null, typeof v == "object" ? JSON.stringify(v) : String(v))))),
            );
            break;
    }
    return t.div(
        { className: `gp-action ${act.status}` },
        t.div(
            { className: "gp-action-head" },
            t.i({ className: actionIcons[act.type] || "ri-magic-line", ariaHidden: true }),
            t.strong({ className: "m-r-auto" }, act.title),
            t.span({ className: `label sm ${statusClass[act.status] || ""}` }, statusLabels[act.status] || act.status),
        ),
        t.div({ className: "gp-action-summary" }, act.summary),
        body,
        act.error ? t.div({ className: "alert danger m-t-5" }, act.error) : null,
        t.div(
            { className: "gp-action-buttons" },
            act.status == "pending" || act.status == "failed"
                ? [
                    t.button(
                        { type: "button", className: `btn sm ${busy ? "loading" : ""}`, disabled: busy, onclick: () => runAction(act, "apply") },
                        t.i({ className: "ri-check-line", ariaHidden: true }),
                        t.span({ className: "txt" }, act.status == "failed" ? "Retry" : act.type == "campaign" ? "Send" : act.type == "flow_run" ? "Run" : "Apply"),
                    ),
                    t.button({ type: "button", className: "btn sm transparent secondary", disabled: busy, onclick: () => runAction(act, "discard") }, t.span({ className: "txt" }, "Discard")),
                ]
                : null,
            act.status == "applied" && act.result?.link
                ? t.a({ href: act.result.link, className: "btn sm outline" }, t.span({ className: "txt" }, act.result.label || "Open"), t.i({ className: "ri-arrow-right-line", ariaHidden: true }))
                : null,
        ),
    );
}

// --- markdown (subset, from escaped text) ---------------------------------------------

function escapeHTML(s) {
    return String(s).replace(/[&<>"']/g, (ch) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[ch]);
}

function inline(s) {
    // s is escaped: only the tags added here exist
    const codes = [];
    s = s.replace(/`([^`]+)`/g, (_, code) => {
        codes.push(code);
        return `\u0000${codes.length - 1}\u0000`;
    });
    s = s.replace(/\[([^\]]+)\]\(([^)\s]+)\)/g, (m, text, href) => {
        const raw = href.replace(/&amp;/g, "&");
        if (!/^(#\/|https?:\/\/)/.test(raw)) {
            return text;
        }
        const external = !raw.startsWith("#/");
        return `<a href="${escapeHTML(raw)}"${external ? ' target="_blank" rel="noopener noreferrer"' : ""}>${text}</a>`;
    });
    s = s.replace(/\*\*([^*]+)\*\*/g, "<strong>$1</strong>");
    s = s.replace(/(^|[\s(])\*([^*\s][^*]*?)\*(?=[\s).,!?:;]|$)/g, "$1<em>$2</em>");
    return s.replace(/\u0000(\d+)\u0000/g, (_, i) => `<code>${codes[i]}</code>`);
}

export function markdown(src) {
    const lines = escapeHTML(src || "").split("\n");
    const out = [];
    let i = 0;
    while (i < lines.length) {
        const line = lines[i];
        if (/^```/.test(line)) {
            const code = [];
            i++;
            while (i < lines.length && !/^```/.test(lines[i])) {
                code.push(lines[i++]);
            }
            i++;
            out.push(`<pre><code>${code.join("\n")}</code></pre>`);
            continue;
        }
        if (/^\s*\|.*\|\s*$/.test(line)) {
            const rows = [];
            while (i < lines.length && /^\s*\|.*\|\s*$/.test(lines[i])) {
                rows.push(lines[i++]);
            }
            const cells = (r) => r.trim().replace(/^\||\|$/g, "").split("|").map((c) => inline(c.trim()));
            const body = rows.filter((r) => !/^\s*\|[\s:|-]+\|\s*$/.test(r));
            const [head, ...rest] = body;
            out.push(
                `<div class="gp-md-table"><table><thead><tr>${cells(head).map((c) => `<th>${c}</th>`).join("")}</tr></thead><tbody>` +
                    rest.map((r) => `<tr>${cells(r).map((c) => `<td>${c}</td>`).join("")}</tr>`).join("") +
                    "</tbody></table></div>",
            );
            continue;
        }
        const heading = /^(#{1,4})\s+(.*)$/.exec(line);
        if (heading) {
            out.push(`<p class="gp-md-h">${inline(heading[2])}</p>`);
            i++;
            continue;
        }
        if (/^\s*([-*]|\d+\.)\s+/.test(line)) {
            const ordered = /^\s*\d+\./.test(line);
            const items = [];
            while (i < lines.length && /^\s*([-*]|\d+\.)\s+/.test(lines[i])) {
                items.push(inline(lines[i++].replace(/^\s*([-*]|\d+\.)\s+/, "")));
            }
            const tag = ordered ? "ol" : "ul";
            out.push(`<${tag}>${items.map((it) => `<li>${it}</li>`).join("")}</${tag}>`);
            continue;
        }
        if (!line.trim()) {
            i++;
            continue;
        }
        const para = [];
        while (i < lines.length && lines[i].trim() && !/^(```|#{1,4}\s|\s*([-*]|\d+\.)\s|\s*\|)/.test(lines[i])) {
            para.push(inline(lines[i++]));
        }
        out.push(`<p>${para.join("<br>")}</p>`);
    }
    return out.join("");
}
