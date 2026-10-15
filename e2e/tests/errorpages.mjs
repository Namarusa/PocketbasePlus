// Error pages: the default ones have the site theme; a 404 page made with
// "New page" replaces it once in use.
import { api, assert, base, equal, login } from "../lib.mjs";

export default async function ({ page, browser, shot }) {
    const guest = await browser.newPage();
    let res = await guest.goto(base + "/no-such-page");
    equal(res.status(), 404, "status");
    assert((await guest.content()).includes("/p/_theme.css"), "the default page has the site theme");
    equal((await guest.locator("h1").innerText()).trim(), "Page not found", "default title");
    await guest.screenshot({ path: new URL("../out/errorpages-default.png", import.meta.url).pathname });

    await login(page);
    await page.evaluate(() => (location.hash = "#/pages"));
    await page.getByRole("button", { name: "New page" }).click();
    const modal = page.locator(".gpp-new-page");
    await modal.locator(".gpp-types .gp-ai-card").nth(4).click();
    equal(await modal.locator("input[type=text]").first().inputValue(), "Page not found", "title prefilled");
    await shot("modal");
    await page.locator("button[form=gpNewPage]").click();
    await page.waitForFunction(() => /#\/pages\/\w+/.test(location.hash));
    await page.frameLocator("iframe.gjs-frame").locator("h1").first().waitFor({ timeout: 20000 });
    await page.waitForTimeout(1000);
    await shot("builder");
    await page.getByRole("button", { name: "Save changes" }).click();
    await page.waitForTimeout(1000);

    const created = (await api("/api/collections/pages/records?filter=" + encodeURIComponent("kind='error'"))).items[0];
    equal([created?.error_code, created?.slug, created?.published], ["404", "error-404", false], "the error page");
    // a draft: still the default page; in use: the page
    res = await guest.goto(base + "/no-such-page");
    assert(!(await guest.locator("header, nav").count()), "the default page (no header)");
    await api(`/api/collections/pages/records/${created.id}`, { method: "PATCH", body: { published: true } });
    res = await guest.goto(base + "/no-such-page");
    equal(res.status(), 404, "still a 404");
    assert(await guest.locator("nav").count(), "the page's header");
    equal((await guest.locator("main h1").innerText()).trim(), "Page not found", "its title");
    equal((await guest.goto(base + "/error-404")).status(), 404, "not served at its address");
    // one page per error
    const dup = await fetch(base + "/api/collections/pages/records", {
        method: "POST",
        headers: { "Content-Type": "application/json", Authorization: await page.evaluate(() => app.pb.authStore.token) },
        body: JSON.stringify({ title: "Other", slug: "other-404", kind: "error", error_code: "404", html: "<p>x</p>" }),
    });
    equal(dup.status, 400, "a second 404 page");
    await guest.close();
}
