import { api, handleError } from "./lib.js";

/**
 * Adds a "Run flow" dropdown in the header of the native record popup when
 * active manual flows are bound to the record's collection.
 */
export async function injectRunFlowButton(modalEl, collection, record) {
    let flows = [];
    try {
        flows = await app.pb.collection("flows").getFullList({
            filter: app.pb.filter("active = true && trigger_type = 'manual' && trigger.collection = {:c}", { c: collection.name }),
            fields: "id,name",
            sort: "name",
            requestKey: null,
        });
    } catch (err) {
        handleError(err);
        return;
    }
    const header = modalEl.querySelector(".modal-header .col-12.flex");
    if (!flows.length || !header || header.querySelector(".gp-run-flow")) {
        return;
    }

    const id = "gp_run_flow_" + record.id;
    const state = store({ running: "" });

    async function run(flow, btn) {
        btn.closest(".dropdown")?.hidePopover();
        state.running = flow.id;
        try {
            const res = await api("/api/flows/run/" + flow.id, {
                method: "POST",
                body: { collection: collection.name, recordIds: [record.id], wait: true },
            });
            const r = res.runs?.[0];
            if (r?.status == "success") {
                app.toasts.success(`Flow "${flow.name}" completed.`);
            } else {
                app.toasts.error(`Flow "${flow.name}" ${r?.status || "failed"}: ${r?.error || ""}`);
            }
        } catch (err) {
            handleError(err);
        }
        state.running = "";
    }

    const el = t.div(
        { className: "gp-run-flow" },
        t.button(
            {
                type: "button",
                className: () => `btn sm outline ${state.running ? "loading" : ""}`,
                disabled: () => !!state.running,
                "html-popovertarget": id,
            },
            t.i({ className: "ri-flow-chart", ariaHidden: true }),
            t.span({ className: "txt" }, "Run flow"),
        ),
        t.div(
            { id, className: "dropdown nowrap", popover: "auto" },
            flows.map((flow) =>
                t.button(
                    { type: "button", className: "dropdown-item", onclick: (e) => run(flow, e.currentTarget) },
                    t.i({ className: "ri-play-line", ariaHidden: true }),
                    t.span({ className: "txt" }, flow.name),
                )
            ),
        ),
    );

    const spacer = header.querySelector(".flex-fill");
    if (spacer) {
        spacer.after(el);
    } else {
        header.appendChild(el);
    }
}
