// A published page: dynamic values, conditions, search, filters, sort and
// pages, as a visitor (no JavaScript needed on the page).
import { assert, base, equal } from "../lib.mjs";

export default async function ({ page, browser, shot }) {
    const names = () => page.locator("#grid h2").allInnerTexts();
    await page.goto(base + "/shop");
    equal(await names(), ["Bench", "Chair", "Desk", "Lamp"], "first page");
    equal((await page.locator(".pb-pager-info").innerText()).trim(), "1–4 of 9", "pager");

    // dynamic image address; hidden for the tables (condition)
    equal(await page.locator("#grid article", { hasText: "Chair" }).locator("img").getAttribute("src"), "https://example.test/chair.png", "dynamic src");
    equal(await page.locator("#grid article", { hasText: "Desk" }).locator("img").count(), 0, "a table's image is removed");
    await shot("list");

    // search: every word in the name or the brand
    await page.locator("form[role=search] input[type=search]").fill("zed s");
    await page.locator("form[role=search] input[type=search]").press("Enter");
    await page.waitForLoadState("load");
    equal(await names(), ["Desk", "Sofa"], "search 'zed s' (every word in the name or the brand)");

    // filters: a choice and a sort, kept while paging
    await page.goto(base + "/shop");
    await page.locator(".pb-filters select[name='f.kind']").selectOption("seat");
    await page.locator(".pb-filters select[name=sort]").selectOption("-price");
    await page.locator(".pb-filters button[type=submit]").click();
    await page.waitForLoadState("load");
    equal(await names(), ["Sofa", "Bench", "Chair", "Stool"], "seats, most expensive first");
    assert(page.url().includes("f.kind=seat") && page.url().includes("sort=-price"), "the address holds the filters: " + page.url());
    equal(await page.locator(".pb-filter-chip").allInnerTexts(), ["Kind: seat ✕"], "chips");
    await shot("filters");

    await page.locator(".pb-filters select[name='f.kind']").selectOption("");
    await page.locator(".pb-filters input[name='f.price_min']").fill("50");
    await page.locator(".pb-filters button[type=submit]").click();
    await page.waitForLoadState("load");
    equal((await page.locator(".pb-pager-info").innerText()).trim(), "1–4 of 5", "price ≥ 50");
    await page.locator("a[rel=next]").click();
    await page.waitForLoadState("load");
    assert(page.url().includes("page=2") && page.url().includes("f.price_min=50"), "next page keeps the filters: " + page.url());
    equal(await names(), ["Shelf"], "second page (still the most expensive first)");

    // Clear
    await page.locator(".pb-filters a", { hasText: "Clear" }).click();
    await page.waitForLoadState("load");
    equal((await page.locator(".pb-pager-info").innerText()).trim(), "1–4 of 9", "cleared");

    // phones: a compact pager
    const phone = await browser.newPage({ viewport: { width: 390, height: 844 } });
    await phone.goto(base + "/shop?page=2");
    assert(await phone.locator(".pb-pager-current").isVisible(), "the current page shows on phones");
    assert(!(await phone.locator(".pb-pager-num").first().isVisible()), "the page numbers hide on phones");
    equal((await phone.locator(".pb-pager-current").innerText()).trim(), "2 / 3", "current page");
    await phone.screenshot({ path: new URL("../out/pages-phone.png", import.meta.url).pathname, fullPage: true });
    await phone.close();
}
