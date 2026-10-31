// PDF viewers in the pages: an uploaded PDF with every control, one page at
// a time without a toolbar, as tall as its pages; a Documents template drawn
// for the record of a page; the block and its settings in the builder.
import { api, assert, base, equal, login, until } from "../lib.mjs";

export default async function ({ page, shot }) {
    const problems = [];
    page.on("console", (m) => m.type() == "error" && problems.push(m.text()));
    page.on("pageerror", (e) => problems.push(e.message));
    await login(page);

    // a PDF of 3 pages (made by the Documents preview), uploaded to a new page
    const sample = await api("/api/documents/preview", {
        method: "POST",
        body: {
            name: "guide",
            spec: {
                format: "a5",
                body: [
                    { id: "a", type: "heading", level: 1, text: "Alpha chapter" },
                    { id: "l", type: "text", text: "See [the site](https://example.com/docs) for more." },
                    { id: "p1", type: "page_break" },
                    { id: "b", type: "heading", level: 1, text: "Bravo chapter" },
                    { id: "p2", type: "page_break" },
                    { id: "c", type: "heading", level: 1, text: "Charlie chapter" },
                ],
            },
        },
    });
    equal(sample.pages, 3, "the sample PDF");
    const guide = await page.evaluate(async (url) => {
        const blob = await (await fetch(url)).blob();
        const fd = new FormData();
        fd.append("title", "Guide");
        fd.append("slug", "guide");
        fd.append("published", "true");
        fd.append("assets", new File([blob], "guide.pdf", { type: "application/pdf" }));
        const rec = await app.pb.collection("pages").create(fd);
        return { id: rec.id, src: `/p/_assets/${rec.id}/${rec.assets[0]}` };
    }, sample.url);
    const html =
        `<main style="max-width:860px;margin:0 auto;padding:16px">` +
        `<div id="full" data-pb-pdf="file" data-pb-pdf-src="${guide.src}" data-pb-pdf-sidebar="closed" data-pb-pdf-name="Guide"></div>` +
        `<div id="single" style="margin-top:24px" data-pb-pdf="file" data-pb-pdf-src="${guide.src}" data-pb-pdf-toolbar="none" data-pb-pdf-layout="single"` +
        ` data-pb-pdf-zoom="page" data-pb-pdf-height="420" data-pb-pdf-background="none" data-pb-pdf-frame="border" data-pb-pdf-text="false"></div>` +
        `<div id="tall" style="margin-top:24px" data-pb-pdf="file" data-pb-pdf-src="${guide.src}" data-pb-pdf-height="auto" data-pb-pdf-tools="pages" data-pb-pdf-zoom="50" data-pb-pdf-background="dark"></div>` +
        `<div id="none" style="margin-top:24px" data-pb-pdf="url" data-pb-pdf-empty="Nothing here"></div>` +
        `</main>`;
    await api(`/api/collections/pages/records/${guide.id}`, { method: "PATCH", body: { html } });

    await page.goto(base + "/guide");
    const full = page.locator("#full");
    await full.locator(".gp-pdf-page--drawn canvas").first().waitFor();
    equal(await full.locator(".gp-pdf-page").count(), 3, "pages");
    equal((await full.locator(".gp-pdf-page-count").innerText()).trim(), "/ 3", "page count");
    assert((await full.locator(".textLayer").first().innerText()).includes("Alpha chapter"), "the text can be selected");
    // the link of the PDF (one area per word)
    assert((await full.locator('.gp-pdf-link[href="https://example.com/docs"]').count()) >= 1, "the link of the PDF");
    const inked = await full.locator(".gp-pdf-page--drawn canvas").first().evaluate((c) => {
        const px = c.getContext("2d").getImageData(0, 0, c.width, c.height).data;
        let dark = 0;
        for (let i = 0; i < px.length; i += 16) dark += px[i] < 128 ? 1 : 0;
        return dark;
    });
    assert(inked > 50, "the page is drawn: " + inked);
    await shot("viewer");

    // pages
    await full.locator(".gp-pdf-next").click();
    await until(async () => (await full.locator(".gp-pdf-page-input").inputValue()) == "2", { message: "next page" });
    await full.locator(".gp-pdf-page-input").fill("3");
    await full.locator(".gp-pdf-page-input").press("Enter");
    await until(async () => (await full.locator(".gp-pdf-page-input").inputValue()) == "3", { message: "page typed" });
    equal(await full.locator(".gp-pdf-next").isDisabled(), true, "no page after the last one");

    // zoom
    const width = async () => (await full.locator(".gp-pdf-page").first().boundingBox()).width;
    const fitted = await width();
    await full.locator(".gp-pdf-zoom-select").selectOption("200");
    await until(async () => (await width()) > fitted * 1.2, { message: "zoom 200%" });
    await full.locator(".gp-pdf-zoom-out").click();
    await until(async () => (await full.locator(".gp-pdf-zoom-select").inputValue()) == "175", { message: "zoom out" });
    await full.locator(".gp-pdf-zoom-select").selectOption("width");
    await until(async () => Math.abs((await width()) - fitted) < 2, { message: "fit the width again" });

    // search
    await full.locator(".gp-pdf-search-btn").click();
    await full.locator(".gp-pdf-search-input").fill("bravo");
    await until(async () => (await full.locator(".gp-pdf-search-count").innerText()) == "1 / 1", { message: "search results" });
    await full.locator('.gp-pdf-page[data-page="2"] .gp-pdf-mark--active').waitFor();
    equal(await full.locator(".gp-pdf-page-input").inputValue(), "2", "the page of the result");
    await full.locator(".gp-pdf-search-input").fill("chapter");
    await until(async () => (await full.locator(".gp-pdf-search-count").innerText()).endsWith("/ 3"), { message: "3 results" });
    await full.locator(".gp-pdf-search-input").press("Enter");
    const bar = await full.locator(".gp-pdf-toolbar").boundingBox();
    const input = await full.locator(".gp-pdf-search-input").boundingBox();
    assert(input.y + input.height <= bar.y + bar.height, "the search bar is in the toolbar");
    await shot("search");
    await full.locator(".gp-pdf-search-input").fill("zzz");
    await until(async () => (await full.locator(".gp-pdf-search-count").innerText()) == "No results", { message: "no results" });
    await full.locator(".gp-pdf-search-input").press("Escape");
    equal(await full.locator(".gp-pdf-searchbar").isHidden(), true, "search closed");

    // thumbnails
    await full.locator(".gp-pdf-side-btn").click();
    await full.locator(".gp-pdf-thumb canvas").first().waitFor();
    equal(await full.locator(".gp-pdf-thumb").count(), 3, "thumbnails");
    await full.locator('.gp-pdf-thumb[data-page="1"]').click();
    await until(async () => (await full.locator(".gp-pdf-page-input").inputValue()) == "1", { message: "thumbnail" });
    equal(await full.locator('.gp-pdf-thumb[data-page="1"]').getAttribute("aria-selected"), "true", "current thumbnail");
    await shot("thumbnails");

    // download: the PDF, under its name
    const [download] = await Promise.all([page.waitForEvent("download"), full.locator(".gp-pdf-download").click()]);
    equal(download.suggestedFilename(), "Guide.pdf", "downloaded name");

    // full screen
    await full.locator(".gp-pdf-full").click();
    await until(async () => page.evaluate(() => document.fullscreenElement?.id == "full" || document.querySelector("#full").classList.contains("gp-pdf--expanded")), { message: "full screen" });
    await page.keyboard.press("Escape");
    await page.evaluate(() => document.fullscreenElement && document.exitFullscreen());

    // one page at a time, no toolbar
    const single = page.locator("#single");
    await single.scrollIntoViewIfNeeded();
    await single.locator(".gp-pdf-page--drawn canvas").first().waitFor();
    equal(await single.locator(".gp-pdf-toolbar").count(), 0, "no toolbar");
    equal(await single.locator(".textLayer").count(), 0, "no text layer");
    equal(await single.locator(".gp-pdf-page--shown").getAttribute("data-page"), "1", "first page");
    await single.locator(".gp-pdf-body").hover();
    await single.locator(".gp-pdf-arrow--next").click();
    await until(async () => (await single.locator(".gp-pdf-page--shown").getAttribute("data-page")) == "2", { message: "next arrow" });
    await single.locator(".gp-pdf-scroller").focus();
    await page.keyboard.press("ArrowRight");
    await until(async () => (await single.locator(".gp-pdf-page--shown").getAttribute("data-page")) == "3", { message: "arrow key" });
    await single.locator(".gp-pdf-page--shown.gp-pdf-page--drawn").waitFor();
    const sb = await single.boundingBox();
    assert(Math.abs(sb.height - 420) < 2, "the height: " + sb.height);
    const shown = await single.locator(".gp-pdf-page--shown").boundingBox();
    assert(shown.height <= 420 && shown.height > 300, "fit the page: " + shown.height);
    await shot("single");

    // as tall as its pages
    const tall = page.locator("#tall");
    await tall.scrollIntoViewIfNeeded();
    await tall.locator(".gp-pdf-page--drawn canvas").first().waitFor();
    const tb = await tall.boundingBox();
    const pages = await tall.locator(".gp-pdf-page").evaluateAll((els) => els.reduce((h, e) => h + e.getBoundingClientRect().height, 0));
    assert(tb.height > pages, `as tall as the pages: ${tb.height} for ${pages}`);
    equal(await tall.locator(".gp-pdf-btn").count(), 2, "only the page buttons");
    equal(await page.locator("#none .gp-pdf-empty").innerText(), "Nothing here", "nothing to show");
    await tall.locator(".gp-pdf-page--drawn").nth(2).waitFor();
    await shot("tall");

    // a phone
    const size = page.viewportSize();
    await page.setViewportSize({ width: 390, height: 800 });
    await page.evaluate(() => scrollTo(0, 0));
    await page.waitForTimeout(400);
    await shot("phone");
    const tbar = await full.locator(".gp-pdf-toolbar").boundingBox();
    assert(tbar.width <= 390 && (await page.evaluate(() => document.documentElement.scrollWidth)) <= 390, "no horizontal scroll on a phone");
    await page.setViewportSize(size);

    // the UI kit in dark mode: the viewer takes the theme
    const settings = (await api("/api/collections/pages_settings/records?perPage=1")).items[0];
    await api(`/api/collections/pages_settings/records/${settings.id}`, { method: "PATCH", body: { mode: "dark" } });
    await api(`/api/collections/pages/records/${guide.id}`, { method: "PATCH", body: { kit: "basecoat" } });
    await page.goto(base + "/guide");
    await full.locator(".gp-pdf-page--drawn canvas").first().waitFor();
    const colors = await full.locator(".gp-pdf-toolbar").evaluate((e) => [getComputedStyle(e).backgroundColor, getComputedStyle(e).color]);
    assert(colors[0] != "rgb(255, 255, 255)" && colors[1] != "rgb(10, 10, 10)", "dark toolbar: " + colors);
    await full.locator(".gp-pdf-side-btn").click();
    await full.locator(".gp-pdf-search-btn").click();
    await full.locator(".gp-pdf-search-input").fill("alpha");
    await until(async () => (await full.locator(".gp-pdf-search-count").innerText()) == "1 / 1", { message: "search (dark)" });
    await shot("dark");
    await api(`/api/collections/pages_settings/records/${settings.id}`, { method: "PATCH", body: { mode: settings.mode } });

    // a Documents template drawn for the record of a single item page
    const products = await api("/api/collections/products");
    const lamp = (await api("/api/collections/products/records?perPage=1&sort=name")).items[0];
    const tpl = await api("/api/collections/document_templates/records", {
        method: "POST",
        body: { name: "Sheet", slug: "sheet", format: "a5", collection: "products", body: [
            { id: "h", type: "heading", level: 1, text: "Sheet of {{record.name}}" },
            { id: "q", type: "code", text: "https://example.com/products/{{record.id}}", width: 35 },
            { id: "b", type: "code", code: "code128", text: "{{record.id}}", caption: true },
        ], filename: "sheet {{record.name}}" },
    });
    await api("/api/collections/pages/records", {
        method: "POST",
        body: { title: "Product sheet", slug: "product-sheet", published: true, record_collection: products.id, html: `<div id="doc" data-pb-pdf="document" data-pb-pdf-document="${tpl.id}"></div><p><a id="dl" data-pb-document="sheet" href="#">Download the sheet</a></p>` },
    });
    await page.goto(`${base}/product-sheet/${lamp.id}`);
    const doc = page.locator("#doc");
    await doc.locator(".gp-pdf-page--drawn .textLayer").first().waitFor();
    assert((await doc.locator(".textLayer").first().innerText()).includes("Sheet of " + lamp.name), "the document of the record");
    const [docDownload] = await Promise.all([page.waitForEvent("download"), doc.locator(".gp-pdf-download").click()]);
    equal(docDownload.suggestedFilename(), `sheet ${lamp.name}.pdf`, "the document's name");
    await shot("document-codes");
    // the Download PDF link: the same document, downloaded
    const [linkDownload] = await Promise.all([page.waitForEvent("download"), page.locator("#dl").click()]);
    equal(linkDownload.suggestedFilename(), `sheet ${lamp.name}.pdf`, "the document link's file");
    equal(problems.filter((p) => !/favicon/.test(p)), [], "browser errors");
    // a link that was changed doesn't open
    const forged = await page.evaluate((id) => {
        const o = JSON.parse(document.querySelector("#doc").getAttribute("data-gp-pdf"));
        return fetch(o.src.replace(id, "x" + id.slice(1))).then((r) => r.status);
    }, lamp.id);
    equal(forged, 403, "a changed link");


    // the builder: the viewer drawn in the canvas, its settings
    await page.goto(base + "/_/");
    await page.evaluate((id) => (location.hash = "#/pages/" + id), guide.id);
    const frame = page.frameLocator("iframe.gjs-frame");
    await frame.locator("#full .gp-pdf-page--drawn canvas").first().waitFor({ timeout: 20000 });
    for (const label of ["PDF viewer", "Field PDF", "Document (PDF)", "Download PDF"]) {
        assert(await page.locator(".gjs-block", { hasText: label }).count(), `the block ${label} is missing`);
    }
    await frame.locator("#full").click({ position: { x: 300, y: 300 } });
    await page.locator(".gjs-trt-trait").first().waitFor();
    const labels = await page.locator(".gjs-trt-trait .gjs-label").allInnerTexts();
    for (const l of ["PDF", "File", "Toolbar", "Buttons", "Page thumbnails", "Pages", "Zoom", "Height"]) {
        assert(labels.some((x) => x.trim() == l), `the setting ${l} is missing: ${labels}`);
    }
    equal(await page.locator(".gpp-pdf-file select").inputValue(), guide.src.split("/").pop(), "the uploaded PDF chosen");
    // no toolbar: the canvas shows it at once
    await shot("builder-settings");
    await page.locator(".gjs-trt-trait", { hasText: "Toolbar" }).locator("select").selectOption("none");
    await until(async () => (await frame.locator("#full .gp-pdf-page--drawn").count()) > 0 && (await frame.locator("#full .gp-pdf-toolbar").count()) == 0, { message: "no toolbar in the canvas" });
    // the buttons: unchecked ones go
    await page.locator(".gjs-trt-trait", { hasText: "Toolbar" }).locator("select").selectOption("");
    await page.locator(".gpp-pdf-tools label", { hasText: "Print" }).click();
    await until(async () => (await frame.locator("#full .gp-pdf-toolbar").count()) == 1 && (await frame.locator("#full .gp-pdf-print").count()) == 0, { message: "no print button" });
    // another source: the address
    await page.locator(".gjs-trt-trait", { hasText: "PDF" }).first().locator("select").selectOption("url");
    await page.locator(".gjs-trt-trait--text").filter({ has: page.locator(".gjs-label", { hasText: /^\s*Address\s*$/ }) }).waitFor();
    equal(await page.locator(".gpp-pdf-file").count(), 0, "the file setting goes");
    await shot("builder");
}
