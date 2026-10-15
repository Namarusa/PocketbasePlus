// A public webhook with a readable address and a Respond step, and its
// settings in the flow editor.
import { assert, base, equal, login } from "../lib.mjs";

export default async function ({ page, ctx, shot }) {
    const res = await fetch(base + "/hooks/e2e-order", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ item: "Chair" }) });
    equal(res.status, 201, "status");
    equal(await res.json(), { order: "Chair" }, "body");
    equal((await fetch(base + "/hooks/e2e-order", { method: "GET" })).status, 405, "method");
    equal((await fetch(base + "/hooks/nope", { method: "POST" })).status, 404, "unknown address");

    await login(page);
    await page.evaluate((id) => (location.hash = "#/flows/" + id), ctx.webhookId);
    await page.locator(".gp-url").waitFor();
    assert((await page.locator(".gp-url").innerText()).endsWith("/hooks/e2e-order"), "the address in the editor");
    assert(await page.locator(".gp-segment.active", { hasText: "Public" }).count(), "the access in the editor");
    await shot("trigger");
}
