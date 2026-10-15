// "Which records" of a collection list: rules instead of a typed filter,
// an order picked, a value from the address (dynamic filter).
import { api, assert, base, equal, login } from "../lib.mjs";

export default async function ({ page, browser, ctx, shot }) {
    await login(page);

    // the filter text <-> the rules (the module of the builder)
    const round = await page.evaluate(async () => {
        const m = await import(app.pb.buildURL("/_/extensions/pocketbaseplus-pages/records.js"));
        const col = await app.pb.collections.getOne("products");
        const brands = await app.pb.collections.getOne("brands");
        const byName = (n) => [col, brands].find((c) => c.name == n || c.id == n);
        const fields = m.filterFields(col, byName);
        const fieldOf = (p) => fields.find((x) => x.value == p)?.field;
        const out = {};
        for (const src of ["kind = 'seat' && price >= 10", "brand = {{query.brand}} || name ~ 'ch'", "brand.name = 'Acme'", "kind = ''", "price > @now"]) {
            const p = m.parseFilter(src, fieldOf);
            out[src] = p ? m.buildFilter(p, fieldOf) : null;
        }
        out.mixed = m.parseFilter("(kind = 'seat' || kind = 'table') && price > 1", fieldOf);
        out.sentence = m.filterSentence("kind = 'seat' && brand = {{auth.id}}", col, byName);
        out.sort = m.buildSort(m.parseSort("-price, name"));
        return out;
    });
    for (const src of ["kind = 'seat' && price >= 10", "brand = {{query.brand}} || name ~ 'ch'", "brand.name = 'Acme'", "kind = ''"]) {
        equal(round[src], src, "round trip");
    }
    equal(round.mixed, null, "and mixed with or stays text");
    equal(round.sentence, "kind is “seat” and brand is the signed-in visitor", "sentence");
    equal(round.sort, "-price,name", "sort");

    // the editor of the shop's list
    await page.evaluate((id) => (location.hash = "#/pages/" + id), ctx.pageId);
    const frame = page.frameLocator("iframe.gjs-frame");
    await frame.locator("#grid h2").first().click();
    for (let i = 0; i < 2; i++) {
        await page.locator(".gjs-toolbar-item").nth(1).click(); // select the parent
        await page.waitForTimeout(300);
    }
    const trait = page.locator(".gjs-trt-trait", { hasText: "Filter" }).first();
    await trait.getByRole("button", { name: /Filter the records|Edit the filter/ }).click();
    const modal = page.locator(".gpp-records-modal");
    await modal.getByRole("button", { name: "Add a rule" }).click();
    // the rule: brand is the address's ?brand
    const row = modal.locator(".gpp-rec-row").first();
    await row.locator(".gpp-rec-field .select").click();
    await page.locator(".dropdown-item:visible", { hasText: /^brand$/ }).first().click();
    await row.locator(".gpp-rec-source .select").click();
    await page.locator(".dropdown-item:visible", { hasText: /^the address$/ }).first().click();
    await modal.locator(".gpp-rec-row").first().locator(".gpp-rec-input input").fill("brand");
    // the list's order (name A–Z): the price, highest first
    const sortRow = modal.locator(".gpp-rec-row.sort").first();
    await sortRow.locator(".gpp-rec-field .select").click();
    await page.locator(".dropdown-item:visible", { hasText: /^price$/ }).first().click();
    await modal.locator(".gpp-rec-row.sort").first().locator(".gpp-rec-op .select").click();
    await page.locator(".dropdown-item:visible", { hasText: "highest first" }).first().click();
    await page.waitForTimeout(500);
    await shot("modal");
    assert((await modal.locator(".gpp-rec-count").innerText()).includes("depend on the visitor or the address"), "the count of a dynamic filter");
    await modal.getByRole("button", { name: "Apply" }).click();
    const list = frame.locator("#grid");
    equal([await list.getAttribute("data-pb-filter"), await list.getAttribute("data-pb-sort")], ["brand = {{query.brand}}", "-price"], "the list's filter and order");
    assert((await trait.innerText()).includes("brand is ?brand of the address"), "the filter in words: " + (await trait.innerText()));
    await shot("trait");
    await page.getByRole("button", { name: "Save changes" }).click();
    await page.waitForTimeout(1500);

    // the published page follows the address
    const guest = await browser.newPage();
    await guest.goto(base + "/shop?brand=" + ctx.brandIds.Zed);
    equal(await guest.locator("#grid h2").allInnerTexts(), ["Sofa", "Table", "Desk", "Torch"], "Zed's products, most expensive first");
    await guest.close();
    // back to the seeded list for the other tests
    await api(`/api/collections/pages/records/${ctx.pageId}`, { method: "PATCH", body: { html: (await import("../seed.mjs")).pageHTML, project: null } });
}
