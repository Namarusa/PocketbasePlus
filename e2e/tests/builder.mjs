// The page builder: the data blocks, and a Filters block linking itself to
// the page's list.
import { assert, base, equal, login } from "../lib.mjs";

export default async function ({ page, ctx, shot }) {
    await login(page);
    await page.evaluate((id) => (location.hash = "#/pages/" + id), ctx.pageId);
    const frame = page.frameLocator("iframe.gjs-frame");
    await frame.locator("[data-pb-list]").first().waitFor({ timeout: 20000 });

    for (const label of ["Search box", "Filters", "Pagination"]) {
        assert(await page.locator(".gjs-block", { hasText: label }).count(), `the block ${label} is missing`);
    }
    // the filters of the page: a preview of the controls and their settings
    await frame.locator("form[data-pb-filters]").click({ position: { x: 4, y: 4 } });
    const traits = page.locator(".gjs-trt-trait");
    await traits.first().waitFor();
    const labels = await page.locator(".gjs-trt-trait .gjs-label").allInnerTexts();
    for (const l of ["Filters", "Fields", "Sort choices"]) {
        assert(labels.some((x) => x.includes(l)), `the setting ${l} is missing: ${labels}`);
    }
    equal(await frame.locator("form[data-pb-filters] .pb-filter-label").allInnerTexts(), ["Kind", "Brand", "Price", "Sort by"], "preview of the filters");

    // drop a new Filters block: it links itself to the list
    await page.evaluate(async () => {
        const block = [...document.querySelectorAll(".gjs-block")].find((b) => b.textContent.includes("Filters"));
        const doc = document.querySelector("iframe.gjs-frame").contentDocument;
        const target = doc.querySelector("[data-pb-list]").parentElement;
        const dt = new DataTransfer();
        block.dispatchEvent(new DragEvent("dragstart", { bubbles: true, dataTransfer: dt }));
        const r = target.getBoundingClientRect();
        const opts = { bubbles: true, cancelable: true, dataTransfer: dt, clientX: r.left + 20, clientY: r.top + 5 };
        target.dispatchEvent(new DragEvent("dragenter", opts));
        target.dispatchEvent(new DragEvent("dragover", opts));
        await new Promise((res) => setTimeout(res, 300));
        target.dispatchEvent(new DragEvent("dragover", opts));
        target.dispatchEvent(new DragEvent("drop", opts));
        block.dispatchEvent(new DragEvent("dragend", { bubbles: true, dataTransfer: dt }));
    });
    await page.waitForTimeout(1000);
    const forms = frame.locator("form[data-pb-filters]");
    equal(await forms.count(), 2, "dropped");
    const dropped = forms.first();
    equal(await dropped.getAttribute("data-pb-filters"), "products", "linked to the list");
    equal(await dropped.getAttribute("data-pb-filter-fields"), "kind,brand", "default fields: choices and relations");
    await shot("filters");
}
