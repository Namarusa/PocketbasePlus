// Settings → Secrets: created in the dashboard, used by an HTTP request,
// never shown back nor in the run log.
import http from "node:http";
import { api, assert, base, equal, login, until } from "../lib.mjs";

export default async function ({ page, shot }) {
    let received = "";
    const echo = http.createServer((req, res) => {
        received = req.headers.authorization || "";
        res.setHeader("Content-Type", "application/json");
        res.end(JSON.stringify({ echo: received }));
    });
    await new Promise((r) => echo.listen(0, "127.0.0.1", r));
    const echoURL = `http://127.0.0.1:${echo.address().port}/items`;
    try {
        await login(page);
        await page.evaluate(() => (location.hash = "#/settings/secrets"));
        await page.locator(".page-header-primary-btns button").first().click();
        await page.locator(".gp-secret-modal input[name=name]").pressSequentially("shop key");
        await page.locator(".gp-secret-modal input[name=value]").fill("live-key-0123456789");
        await page.locator(".gp-secret-modal button[type=submit]").click();
        await page.locator(".gp-secrets-table", { hasText: "SHOP_KEY" }).waitFor();
        await shot("page");

        const list = await api("/api/collections/secrets/records");
        assert(!JSON.stringify(list).includes("live-key"), "the API returns the value");
        equal(list.items.find((s) => s.name == "SHOP_KEY")?.hint, "…6789", "hint");

        const flow = await api("/api/collections/flows/records", {
            method: "POST",
            body: {
                name: "Call the shop", active: true, trigger_type: "manual", trigger: {},
                steps: [{ key: "call", type: "http_request", config: { method: "GET", url: echoURL, headers: { Authorization: "Bearer {{secret.SHOP_KEY}}" }, responseFormat: "auto" } }],
            },
        });
        await api("/api/flows/run/" + flow.id, { method: "POST", body: {} });
        const run = await until(async () => (await api(`/api/collections/flow_runs/records?filter=${encodeURIComponent(`flow='${flow.id}'`)}`)).items[0], { message: "no run" });
        equal(received, "Bearer live-key-0123456789", "the API received the key");
        assert(!JSON.stringify(run).includes("live-key"), "the run log shows the key");
        assert(JSON.stringify(run).includes("[hidden]"), "the run log hides the key");

        // a secret outside an HTTP request is refused
        const bad = await fetch(base + "/api/collections/flows/records", {
            method: "POST",
            headers: { "Content-Type": "application/json", Authorization: (await page.evaluate(() => app.pb.authStore.token)) },
            body: JSON.stringify({ name: "Leak", active: true, trigger_type: "manual", trigger: {}, steps: [{ key: "log", type: "log", config: { message: "{{secret.SHOP_KEY}}" } }] }),
        });
        equal(bad.status, 400, "a secret in a log step");
    } finally {
        echo.close();
    }
}
