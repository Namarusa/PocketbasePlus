// "New page": a collection page with its single item page, ready to use
// (search, filters, pages, cards linked to the items), and the theme always on.
import { api, assert, base, equal, login } from "../lib.mjs";

export default async function ({ page, browser, shot }) {
    await login(page);
    await page.evaluate(() => (location.hash = "#/pages"));
    await page.getByRole("button", { name: "New page" }).click();
    const modal = page.locator(".gpp-new-page");
    await modal.locator("input[type=text]").first().fill("Catalog");
    await modal.locator(".gpp-types .gp-ai-card").nth(1).click();
    await modal.locator(".field", { hasText: "Collection" }).locator(".select").click();
    await page.locator(".dropdown .dropdown-item, .select .options .option", { hasText: "products" }).first().click();
    await modal.locator(".gpp-layouts .gp-ai-card", { hasText: "Cards" }).waitFor();
    assert(await modal.getByText("Also create its single item page").count(), "the single item page option");
    assert(!(await modal.getByText(/without kit|Basecoat/i).count()), "no styling choice");
    await shot("modal");
    await page.locator("button[form=gpNewPage]").click();
    await page.waitForFunction(() => /#\/pages\/\w+/.test(location.hash));
    const frame = page.frameLocator("iframe.gjs-frame");
    await frame.locator("[data-pb-list=products] h2[data-pb-field=name]").first().waitFor({ timeout: 20000 });
    await page.waitForTimeout(1500);
    await shot("builder");
    // the settings: the type, no kit switch
    await page.getByRole("button", { name: "Settings" }).click();
    const settings = page.locator(".modal", { hasText: "Page settings" });
    assert((await settings.locator(".gpp-page-type").innerText()).includes("Collection page · products"), "type in the settings");
    assert(!(await settings.getByText("Basecoat UI kit").count()), "no kit switch");
    await settings.getByRole("button", { name: "Cancel" }).click();
    await page.getByRole("button", { name: "Save changes" }).click();
    await page.waitForTimeout(1500);

    // publish both pages, then visit as a guest
    const pages = (await api("/api/collections/pages/records?perPage=100")).items;
    const catalog = pages.find((p) => p.slug == "catalog");
    const item = pages.find((p) => p.slug == "catalog-item");
    equal([catalog?.kind, catalog?.kit, item?.kit, item?.record_collection == catalog?.list_collection], ["collection", "basecoat", "basecoat", true], "created pages");
    for (const p of [catalog, item]) {
        await api(`/api/collections/pages/records/${p.id}`, { method: "PATCH", body: { published: true } });
    }
    const guest = await browser.newPage();
    await guest.goto(base + "/catalog");
    equal(await guest.locator("article h2").count(), 9, "cards (12 per page: one page)");
    assert(await guest.locator("form[role=search] input[type=search]").count(), "search box");
    assert(await guest.locator("form.pb-filters select").count(), "filters");
    await guest.screenshot({ path: new URL("../out/newpage-published.png", import.meta.url).pathname, fullPage: true });
    const href = await guest.locator("article a[href*='/catalog-item/']").first().getAttribute("href");
    assert(href, "cards link to the single item page");
    await guest.goto(base + href);
    assert((await guest.locator("h1").first().innerText()).length > 0, "the item page shows the record");
    await guest.close();

    // an old page without the theme: one switch
    const classic = await api("/api/collections/pages/records", { method: "POST", body: { title: "Old", slug: "old", html: "<p>Old</p>", kit: "" } });
    await page.evaluate((id) => (location.hash = "#/pages/" + id), classic.id);
    await page.frameLocator("iframe.gjs-frame").locator("p").first().waitFor({ timeout: 20000 });
    await page.getByRole("button", { name: "Settings" }).click();
    await page.getByRole("button", { name: "Use the site theme" }).click();
    await page.locator(".modal", { hasText: "Page settings" }).getByRole("button", { name: "Apply" }).click();
    await page.getByRole("button", { name: "Save changes" }).click();
    await page.waitForTimeout(1500);
    equal((await api(`/api/collections/pages/records/${classic.id}`)).kit, "basecoat", "converted");
}
