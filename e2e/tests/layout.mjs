// The page builder's style panel: grid layout, the placement in the parent,
// the Large screen breakpoint (min-width), the image settings and styles, and
// the page's own CSS (kept, shown in the canvas, published).
import { api, assert, base, equal, login, until } from "../lib.mjs";

const pixel =
    "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAIAAAACCAYAAABytg0kAAAAFklEQVR4nGP8z8DwnwEIGBmQAQMDAwA0AQP/ZK7eWwAAAABJRU5ErkJggg==";

export default async function ({ page, shot }) {
    const problems = [];
    page.on("console", (m) => m.type() == "error" && problems.push(m.text()));
    await login(page);
    const rec = await api("/api/collections/pages/records", {
        method: "POST",
        body: {
            title: "Layout",
            slug: "layout",
            published: true,
            html: `<section id="grid" style="padding:16px"><div id="a">A</div><div id="b">B</div><div id="c">C</div></section><p><img id="pic" src="${pixel}" style="width:200px;height:120px"></p>`,
        },
    });
    await page.evaluate((id) => (location.hash = "#/pages/" + id), rec.id);
    const frame = page.frameLocator("iframe.gjs-frame");
    await frame.locator("#grid").waitFor({ timeout: 20000 });
    const computed = (sel, prop) => frame.locator(sel).evaluate((el, p) => getComputedStyle(el).getPropertyValue(p), prop);
    const tracks = async (sel) => (await computed(sel, "grid-template-columns")).trim().split(/\s+/).length;
    const prop = (name) => page.locator(`.gjs-sm-property__${name}`);
    // a device: the canvas resizes (and zooms out for the large screen)
    const device = async (icon) => {
        await page.locator(".gjs-pn-btn." + icon).click();
        await page.waitForTimeout(900);
    };
    // a selection shows its settings: back to the styles
    const select = async (sel, opts) => {
        await frame.locator(sel).click(opts);
        await page.locator(".gjs-pn-btn.ri-paint-brush-line").click();
    };

    // the section: a grid of 3 columns with a gap
    await select("#grid", { position: { x: 2, y: 2 } });
    const sectors = await page.locator(".gjs-sm-sector-title").allInnerTexts();
    for (const s of ["Layout", "Position", "Size", "Spacing", "Text", "Background & border", "Effects"]) {
        assert(sectors.some((x) => x.includes(s)), `the sector ${s}: ${sectors}`);
    }
    assert(!(await prop("object-fit").isVisible()), "no image settings for a section");
    assert(!(await prop("grid-template-columns").isVisible()), "no grid settings for a block");
    await prop("display").locator("select").selectOption("grid");
    await prop("grid-template-columns").waitFor({ state: "visible" });
    await prop("grid-template-columns").locator("button", { hasText: /^3$/ }).click();
    await until(async () => (await tracks("#grid")) == 3, { message: "3 columns" });
    await prop("gap").locator("input").first().fill("24");
    await prop("gap").locator("input").first().press("Enter");
    await until(async () => (await computed("#grid", "column-gap")) == "24px", { message: "the gap" });
    assert(await prop("justify-content").isVisible(), "the alignment of a grid");
    await shot("grid");

    // an item: spans the full row
    await select("#c");
    await prop("grid-column").waitFor({ state: "visible" });
    await prop("grid-column").locator("button", { hasText: "Full row" }).click();
    await until(async () => (await computed("#c", "grid-column-start")) == "1" && (await computed("#c", "grid-column-end")) == "-1", { message: "the full row" });
    assert(!(await prop("flex-grow").isVisible()), "no flex settings in a grid");

    // Large screen: 4 columns from 1440px, the desktop keeps 3
    await device("ri-tv-2-line");
    await select("#grid", { position: { x: 2, y: 2 } });
    await prop("grid-template-columns").locator("button", { hasText: /^4$/ }).click();
    await until(async () => (await tracks("#grid")) == 4, { message: "4 columns on a large screen" });
    await shot("large");
    await device("ri-computer-line");
    await until(async () => (await tracks("#grid")) == 3, { message: "3 columns on a desktop" });
    // Phone: 1 column
    await device("ri-smartphone-line");
    await select("#grid", { position: { x: 2, y: 2 } });
    await prop("grid-template-columns").locator("input").fill("1fr");
    await prop("grid-template-columns").locator("input").press("Enter");
    await until(async () => (await tracks("#grid")) == 1, { message: "1 column on a phone" });
    await device("ri-computer-line");

    // the image: its settings and its fit
    await frame.locator("#pic").click();
    await page.locator(".gpp-image").waitFor();
    await page.locator(".gjs-trt-trait", { hasText: "Alt text" }).locator("input").fill("A test image");
    await page.locator(".gjs-trt-trait", { hasText: "Alt text" }).locator("input").press("Enter");
    await page.locator(".gpp-image-href").fill("/about");
    await page.locator(".gpp-image-href").press("Enter");
    await frame.locator("a[href='/about'] > #pic").waitFor();
    await page.locator(".gpp-image-blank").check();
    await page.locator(".gpp-image-loading").selectOption("lazy");
    await shot("image-settings");
    await page.locator(".gjs-pn-btn.ri-paint-brush-line").click();
    await prop("object-fit").waitFor({ state: "visible" });
    await prop("object-fit").locator("select").selectOption("cover");
    await prop("aspect-ratio").locator("button", { hasText: "16:9" }).click();
    await until(async () => (await computed("#pic", "object-fit")) == "cover", { message: "the fit" });
    await shot("image-styles");

    // the page's own CSS: shown in the canvas
    await page.locator(".gjs-pn-btn.ri-css3-line").click();
    await page.locator(".gpp-css-input").fill("#grid > div { border-top: 7px solid rgb(255, 0, 0); }\n@keyframes spin { to { transform: rotate(1turn) } }");
    await page.waitForTimeout(400);
    await shot("custom-css");
    await page.locator("button[form=gppCustomCSS]").click();
    await until(async () => (await computed("#a", "border-top-width")) == "7px", { message: "the custom CSS in the canvas" });

    // saved: the CSS of the builder, then the custom CSS
    await page.keyboard.press("Control+s");
    await page.locator(".gpp-builder", { hasText: "Saved" }).first().waitFor();
    const saved = await api("/api/collections/pages/records/" + rec.id);
    assert(/@media \(min-width: 1440px\)\{#grid\{grid-template-columns:repeat\(4, 1fr\);?\}\}/.test(saved.css), "the large screen rule: " + saved.css);
    assert(saved.css.indexOf("min-width: 1440px") < saved.css.indexOf("max-width: 480px"), "the media order");
    assert(saved.css.trimEnd().endsWith("@keyframes spin { to { transform: rotate(1turn) } }"), "the custom CSS last");
    equal(saved.project.gppCustomCSS.startsWith("#grid > div"), true, "kept as written");
    assert(/<a href="\/about" target="_blank" rel="noopener"><img[^>]*alt="A test image"/.test(saved.html) && saved.html.includes('loading="lazy"'), "the image: " + saved.html);

    // published: the same rules
    const html = await (await fetch(base + "/layout")).text();
    assert(html.includes("min-width: 1440px") && html.includes("border-top: 7px solid"), "the published CSS");
    const pub = await page.context().newPage();
    for (const [width, want] of [[1600, 4], [1200, 3], [400, 1]]) {
        await pub.setViewportSize({ width, height: 800 });
        await pub.goto(base + "/layout");
        equal(await pub.locator("#grid").evaluate((el) => getComputedStyle(el).gridTemplateColumns.split(" ").length), want, `columns at ${width}px`);
    }
    await pub.close();

    // opened again: the custom CSS is still there
    await page.reload();
    await frame.locator("#grid").waitFor({ timeout: 20000 });
    await until(async () => (await computed("#a", "border-top-width")) == "7px", { message: "the custom CSS after a reload" });
    await page.locator(".gjs-pn-btn.ri-css3-line").click();
    assert((await page.locator(".gpp-css-input").inputValue()).startsWith("#grid > div"), "the custom CSS in its window");

    equal(problems.filter((p) => !/favicon/.test(p)), [], "browser errors");
}
