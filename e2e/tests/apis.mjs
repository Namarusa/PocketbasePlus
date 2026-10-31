// Flows → APIs: the variable menu of the call fields (the { } button, or
// "{{" typed) lists the call's variables, the API's and the secrets, and
// inserts {{name}} at the caret; the API's variables are listed with the
// call's.
import http from "node:http";
import { api, assert, equal, login, until } from "../lib.mjs";

// waits for an editable variable row with that name; returns its position
async function varRow(page, name) {
    const handle = await page.waitForFunction(
        (n) => {
            const rows = [...document.querySelectorAll(".gp-api-var-row:not(.gp-api-var-ro):not(.gp-api-head)")];
            const i = rows.findIndex((r) => r.querySelector("input")?.value == n);
            return i >= 0 ? i + 1 : 0;
        },
        name,
        { timeout: 10000 },
    );
    return page.locator(".gp-api-var-row:not(.gp-api-var-ro):not(.gp-api-head)").nth((await handle.jsonValue()) - 1);
}

export default async function ({ page, shot }) {
    let received = "";
    const echo = http.createServer((req, res) => {
        received = req.url;
        res.setHeader("Content-Type", "application/json");
        res.end(JSON.stringify({ path: req.url, items: [{ name: "Pikachu" }] }));
    });
    await new Promise((r) => echo.listen(0, "127.0.0.1", r));
    const host = `127.0.0.1:${echo.address().port}`;
    try {
        const source = await api("/api/collections/api_sources/records", {
            method: "POST",
            body: { name: "Cards", base_url: "http://{{host}}/v2", auth: "none", variables: [{ name: "host", default: host }, { name: "title", default: "Mr" }] },
        });
        await login(page);
        await page.evaluate((id) => (location.hash = "#/apis/new?api=" + id), source.id);
        const path = page.locator("input[name=path]");
        await path.waitFor();

        // "{{" typed: the menu lists the API's variables; a new one is declared
        await path.click();
        await path.pressSequentially("cards/{{");
        const menu = page.locator(".gp-varmenu");
        await menu.locator(".gp-varmenu-group", { hasText: "From the API" }).waitFor();
        await menu.locator(".gp-varmenu-item", { hasText: "host" }).waitFor();
        await shot("menu");
        await path.pressSequentially("id");
        await menu.locator(".gp-varmenu-item", { hasText: "New variable" }).click();
        equal(await path.inputValue(), "cards/{{id}}", "typed and picked");
        await varRow(page, "id");

        // the { } button of a query value inserts at its caret, the short form
        await page.locator("button", { hasText: "Add parameter" }).click();
        const row = page.locator(".gp-kv-row").last();
        await row.locator("input").first().fill("server");
        const value = row.locator("input.gp-api-tpl");
        await value.fill("on ");
        await row.locator(".gp-varfield-btn").click();
        await menu.locator(".gp-varmenu-item", { hasText: "host" }).click();
        equal(await value.inputValue(), "on {{host}}", "inserted with the button");

        // a reserved name (a function) is written with .vars; keyboard pick
        await value.press("End");
        await value.pressSequentially(" {{tit");
        await value.press("Enter");
        equal(await value.inputValue(), "on {{host}} {{.vars.title}}", "reserved name");

        // the API's variables are listed with the call's, used or not
        const apivars = page.locator(".gp-api-apivars");
        await apivars.locator(".gp-api-var-ro", { hasText: "host" }).locator(".label", { hasText: /^used$/ }).waitFor();
        await (await varRow(page, "id")).locator("input").nth(1).fill("25");
        await page.locator(".gp-api-preview code", { hasText: `http://${host}/v2/cards/25?server=on` }).waitFor();
        await shot("call");

        // the call works
        await page.locator("button", { hasText: "Test the call" }).click();
        await until(() => received.startsWith("/v2/cards/25?"), { message: "request: " + received });
        equal(new URL("http://x" + received).searchParams.get("server"), `on ${host} Mr`, "query");

        await page.locator(".page-header button", { hasText: "Save" }).click();
        await page.waitForFunction(() => /#\/apis\/[a-z0-9]{15}$/.test(location.hash));
        const saved = (await api("/api/collections/api_calls/records")).items.find((c) => c.source == source.id);
        equal(saved.path, "cards/{{id}}", "saved path");
        equal(saved.query.server, "on {{host}} {{.vars.title}}", "saved query");
        assert(!saved.variables.some((v) => v.name == "host"), "the API's variable isn't copied to the call");

        // the API settings: the same menu
        await page.locator(".gp-api-apivars button", { hasText: "API settings" }).click();
        const base = page.locator("input#base_url");
        await base.waitFor();
        await base.press("End");
        await base.pressSequentially("/{{ver");
        await page.locator(".gp-varmenu-item", { hasText: "New variable" }).click();
        equal(await base.inputValue(), "http://{{host}}/v2/{{ver}}", "API base");
        await varRow(page, "ver");
        // "title" is used by the call: not struck out in the API settings
        await page.waitForFunction(() => {
            const row = [...document.querySelectorAll(".gp-api-var-row")].find((r) => r.querySelector("input")?.value == "title");
            return row && !row.classList.contains("unused");
        });
        await shot("api-settings");

        // phone width, dark: the menu fits
        await page.keyboard.press("Escape");
        await page.locator("button", { hasText: "Cancel" }).last().click();
        await page.setViewportSize({ width: 390, height: 800 });
        await page.emulateMedia({ colorScheme: "dark" });
        await path.click();
        await path.press("End");
        await path.pressSequentially("/{{");
        await menu.waitFor();
        const box = await menu.boundingBox();
        assert(box.x >= 0 && box.x + box.width <= 390, "the menu fits the phone width: " + JSON.stringify(box));
        await shot("menu-phone");
    } finally {
        echo.close();
    }
}
