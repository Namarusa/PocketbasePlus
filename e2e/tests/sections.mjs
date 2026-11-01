// The section templates of the site theme: listed in the blocks, published
// (images, forms), readable on a computer and a phone (no sideways scroll).
import { api, assert, base, equal, login, until } from "../lib.mjs";

const labels = ["Announcement bar", "Hero with image", "Logos", "Numbers", "Feature list", "Feature with image", "How it works", "Team", "Gallery", "Articles", "Text content", "Contact", "Newsletter", "Footer with columns"];

export default async function ({ page, browser, shot }) {
    await login(page);
    const problems = [];
    page.on("console", (m) => m.type() == "error" && problems.push(m.text()));

    // every new section on one page
    const html = await page.evaluate(async () => Object.values((await import(app.pb.buildURL("/_/extensions/pocketbaseplus-pages/kit.js"))).moreSections).join("\n"))
        // as a page template: the markers become plain ids
        .then((h) => h.replace(/data-kit-id="([^"]+)"/g, 'id="$1"').replace(/data-kit-ref="for:([^"]+)"/g, 'for="$1"'));
    const rec = await api("/api/collections/pages/records", { method: "POST", body: { title: "Sections", slug: "sections", published: true, kit: "basecoat", html } });

    // in the blocks panel
    await page.evaluate((id) => (location.hash = "#/pages/" + id), rec.id);
    const frame = page.frameLocator("iframe.gjs-frame");
    await frame.locator("form[data-pb-action]").first().waitFor({ timeout: 20000 });
    for (const l of labels) {
        assert(await page.locator(".gjs-block", { hasText: new RegExp("^\\s*" + l + "\\s*$") }).count(), "the block " + l);
    }

    // dropped: the labels of the form point to unique ids
    await page.locator(".gjs-block-category .gjs-title", { hasText: /^\s*Sections\s*$/ }).click();
    await page.locator(".gjs-block", { hasText: /^\s*Contact\s*$/ }).scrollIntoViewIfNeeded();
    await page.evaluate(async () => {
        const block = [...document.querySelectorAll(".gjs-block")].find((b) => b.textContent.trim() == "Contact");
        const doc = document.querySelector("iframe.gjs-frame").contentDocument;
        const target = doc.querySelector("section"); // the hero, at the top
        const dt = new DataTransfer();
        block.dispatchEvent(new DragEvent("dragstart", { bubbles: true, dataTransfer: dt }));
        const r = target.getBoundingClientRect();
        const opts = { bubbles: true, cancelable: true, dataTransfer: dt, clientX: r.left + 30, clientY: r.bottom - 10 };
        target.dispatchEvent(new DragEvent("dragenter", opts));
        target.dispatchEvent(new DragEvent("dragover", opts));
        await new Promise((res) => setTimeout(res, 300));
        target.dispatchEvent(new DragEvent("dragover", opts));
        target.dispatchEvent(new DragEvent("drop", opts));
        block.dispatchEvent(new DragEvent("dragend", { bubbles: true, dataTransfer: dt }));
    });
    await until(async () => (await frame.locator("form[data-pb-action] textarea").count()) == 2, { message: "the contact section dropped" });
    const ids = await frame.locator("form[data-pb-action] label[for]").evaluateAll((ls) => ls.map((l) => [l.htmlFor, !!l.ownerDocument.getElementById(l.htmlFor)]));
    assert(ids.length == 6 && ids.every(([, ok]) => ok) && ids.filter(([id]) => /^\w+-\w+$/.test(id)).length == 3 && new Set(ids.map((i) => i[0])).size == 6, "unique ids: " + JSON.stringify(ids));
    await shot("builder");

    // published, on a computer and a phone
    const img = await fetch(base + "/p/_static/placeholder.svg");
    equal([img.status, img.headers.get("content-type")], [200, "image/svg+xml"], "the placeholder image");
    for (const [name, viewport] of [["desktop", { width: 1280, height: 900 }], ["phone", { width: 390, height: 844 }], ["small", { width: 320, height: 640 }]]) {
        const ctx = await browser.newContext({ viewport });
        const pub = await ctx.newPage();
        const errors = [];
        pub.on("console", (m) => m.type() == "error" && errors.push(m.text()));
        await pub.goto(base + "/sections");
        await pub.waitForLoadState("networkidle");
        const check = await pub.evaluate(() => ({
            scroll: document.documentElement.scrollWidth - innerWidth,
            images: [...document.images].length,
            broken: [...document.images].filter((i) => !i.complete || !i.naturalWidth).map((i) => i.src),
            forms: document.querySelectorAll("form").length,
            headings: [...document.querySelectorAll("h1,h2")].map((h) => h.textContent.trim()),
        }));
        equal([check.scroll <= 0, check.broken, check.forms], [true, [], 2], name + ": " + JSON.stringify(check));
        assert(check.images == 11 && check.headings.includes("Get in touch") && check.headings.includes("Meet the team"), name + ": " + JSON.stringify(check));
        equal(errors, [], name + ": browser errors");
        await pub.screenshot({ path: `out/sections-${name}.png`, fullPage: true });
        await ctx.close();
    }

    equal(problems.filter((p) => !/favicon/.test(p)), [], "browser errors");
}
