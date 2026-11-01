// The icon picker of the kit buttons: the popular icons, a search over every
// Lucide icon (names and tags), the chosen icon saved inline and published.
import { api, assert, base, equal, login, until } from "../lib.mjs";

export default async function ({ page, shot }) {
    const problems = [];
    await login(page);
    // after the sign in (its retries when PocketBase limits the sign ins log errors)
    page.on("console", (m) => m.type() == "error" && problems.push(m.text()));
    const rec = await api("/api/collections/pages/records", {
        method: "POST",
        body: { title: "Icons", slug: "icon-picker", published: true, kit: "basecoat", html: `<section style="padding:24px"><a id="b" class="btn" href="/cart">Cart</a></section>` },
    });
    await page.evaluate((id) => (location.hash = "#/pages/" + id), rec.id);
    const frame = page.frameLocator("iframe.gjs-frame");
    await frame.locator("#b").click();
    const picker = page.locator(".gpp-icon-picker");
    await picker.waitFor();
    await picker.locator(".gpp-icon-current").click();

    // the popular icons first, then all of them once loaded
    await picker.locator(".gpp-icon-group", { hasText: /All icons \(\d{4}\)/ }).waitFor();
    const count = await picker.locator(".gpp-icon-grid button").count();
    assert(count > 1500, "every icon listed: " + count);
    await shot("all");

    // a search by a tag ("trolley" is a tag of shopping-cart), then by a name
    await picker.locator(".gpp-icon-search").fill("trolley");
    await until(async () => (await picker.locator(".gpp-icon-grid button").evaluateAll((bs) => bs.map((b) => b.dataset.value).join())) == "baggage-claim,shopping-cart,shopping-cart-plus,shopping-cart-minus", { message: "found by a tag", timeout: 3000 });
    await picker.locator(".gpp-icon-search").fill("truck");
    await until(async () => (await picker.locator(".gpp-icon-grid button").first().getAttribute("data-value")) == "truck", { message: "found by its name" });
    await picker.locator(".gpp-icon-search").fill("zzqx");
    await picker.locator(".gpp-icon-note", { hasText: "No icon" }).waitFor();
    await shot("search");

    // Enter picks the best match: an icon outside the popular ones
    await picker.locator(".gpp-icon-search").fill("shopping basket");
    await until(async () => (await picker.locator(".gpp-icon-grid button").first().getAttribute("data-value")) == "shopping-basket", { message: "the basket" });
    await picker.locator(".gpp-icon-search").press("Enter");
    await frame.locator("#b svg[data-icon='shopping-basket'] path").first().waitFor();
    equal((await picker.locator(".gpp-icon-current").innerText()).trim(), "Shopping basket", "the picker shows it");
    assert(await picker.locator(".gpp-icon-panel").isHidden(), "closed after the choice");

    // saved inline, published as is
    await page.keyboard.press("Control+s");
    await page.locator(".gpp-builder", { hasText: "Saved" }).first().waitFor();
    await until(async () => (await api("/api/collections/pages/records/" + rec.id)).html.includes('data-icon="shopping-basket"'), { message: "saved" });
    const html = await (await fetch(base + "/icon-picker")).text();
    assert(/<svg[^>]*data-icon="shopping-basket"[^>]*>(<path[^>]*>|<\/path>)+/.test(html), "published: " + html.slice(html.indexOf('id="b"') - 20, html.indexOf('id="b"') + 600));

    // reopened: the icon of the whole set is shown again
    await page.reload();
    await frame.locator("#b").click();
    await until(async () => (await page.locator(".gpp-icon-current svg path").count()) > 0, { message: "the icon after a reload" });

    equal(problems.filter((p) => !/favicon/.test(p)), [], "browser errors");
}
