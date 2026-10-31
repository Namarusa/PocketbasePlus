// Documents: a template made from the Invoice starter, edited with a live
// PDF preview, the A5 format, generated and downloaded; a flow makes one and
// attaches it to an email.
import { api, assert, equal, login, until } from "../lib.mjs";

export default async function ({ page, shot, ctx }) {
    await login(page);
    await page.evaluate(() => (location.hash = "#/documents"));
    await page.locator(".page-header button", { hasText: "New template" }).click();

    const modal = page.locator(".gp-doc-new");
    await modal.locator(".gp-doc-starter", { hasText: "Invoice" }).click();
    await modal.locator("#doc_name").fill("Product sheet");
    await modal.locator(".field", { hasText: "About the records of" }).locator(".select").click();
    await page.locator(".dropdown .dropdown-item, .select .options .option", { hasText: "products" }).first().click();
    await shot("new");
    const created = page.waitForResponse((r) => r.url().includes("/api/collections/document_templates/records") && r.request().method() == "POST");
    await page.locator("button[form=gpNewDocument]").click();
    const tpl = await (await created).json();
    await page.waitForFunction((id) => location.hash == "#/documents/" + id, tpl.id);

    // the preview: the pages of the PDF made by the server, drawn in the editor
    const sheets = page.locator(".gp-doc-sheet canvas");
    await sheets.first().waitFor();
    const firstRaw = await page.evaluate(() => document.querySelector(".gp-doc-stage-bar a[target=_blank]")?.href);
    assert(firstRaw?.includes("/api/documents/preview/"), "preview url: " + firstRaw);
    const pdf = await page.evaluate(async (src) => {
        const r = await fetch(src);
        return { type: r.headers.get("content-type"), head: (await r.text()).slice(0, 5) };
    }, firstRaw);
    equal(pdf.type, "application/pdf", "preview type");
    equal(pdf.head, "%PDF-", "preview content");
    // something is drawn on the page
    const inked = await page.evaluate(() => {
        const c = document.querySelector(".gp-doc-sheet canvas");
        const px = c.getContext("2d").getImageData(0, 0, c.width, c.height).data;
        let dark = 0;
        for (let i = 0; i < px.length; i += 4) dark += px[i] < 128 ? 1 : 0;
        return dark;
    });
    assert(inked > 500, "the page is drawn: " + inked);
    await shot("editor");

    // a click on the title in the page selects it
    const hitId = await page.evaluate(() => [...document.querySelectorAll(".gp-doc-hit")].find((h) => h.dataset.label.startsWith("Invoice"))?.dataset.block);
    assert(hitId, "a box for the title");
    await page.locator(`.gp-doc-hit[data-block="${hitId}"]`).first().click();
    const title = page.locator("#b_text");
    await title.waitFor();
    assert(await page.locator(`.gp-doc-hit.selected[data-block="${hitId}"]`).count(), "the block is selected on the page");
    // the data picker opens whole, inside the window
    await page.locator(".gp-doc-formula", { has: title }).locator(".gp-doc-formula-btn").click();
    const picker = page.locator(".gp-doc-picker");
    await picker.waitFor();
    const pr = await picker.boundingBox();
    const vw = page.viewportSize().width;
    assert(pr.x >= 0 && pr.x + pr.width <= vw && pr.width >= 300, "the picker is visible: " + JSON.stringify(pr));
    const top = await page.evaluate(() => {
        const r = document.querySelector(".gp-doc-picker").getBoundingClientRect();
        return document.elementFromPoint(r.left + 20, r.top + 20)?.closest(".gp-doc-picker") != null;
    });
    assert(top, "the picker is on top");
    await shot("picker");
    await page.locator(".gp-doc-formula", { has: title }).locator(".gp-doc-formula-btn").click();
    await title.fill("Sheet {{record.name}}");
    await shot("block");
    const again = page.waitForResponse((r) => r.url().endsWith("/api/documents/preview") && r.request().method() == "POST");
    const res = await (await again).json();
    assert(res.url && res.pages >= 1 && res.boxes?.length, "preview after the edit: " + JSON.stringify(res).slice(0, 300));
    await until(async () => (await page.evaluate(() => document.querySelector(".gp-doc-stage-bar a[target=_blank]")?.href)) != firstRaw, { message: "the preview didn't change" });

    // a formula error shows on the block
    await title.fill("Sheet {{divide 1 0}}");
    await page.locator(".gp-doc-error", { hasText: "division" }).waitFor();
    await page.locator(".gp-doc-hit.has-error").first().waitFor();
    await title.fill("Sheet {{record.name}}");
    await page.locator(".gp-doc-error").waitFor({ state: "detached" });


    // drag a divider from the palette onto the title: it goes after it
    await page.locator(".gp-doc-back").click();
    const target = page.locator(`.gp-doc-hit[data-block="${hitId}"]`).first();
    // the preview with the title back (the failing one was empty)
    const tbox = await until(async () => {
        const b = await target.boundingBox().catch(() => null);
        return b && b.height > 30 ? b : null;
    }, { message: "the title isn't back on the page" });
    await page.locator(".gp-doc-palette-item", { hasText: "Divider" }).dragTo(target, { targetPosition: { x: 20, y: tbox.height - 3 } });
    await page.locator(".gp-doc-block-title", { hasText: "Divider" }).waitFor();
    // an image from an address: a file of this app
    await page.locator(".gp-doc-back").click();
    await page.locator(".gp-doc-palette-item", { hasText: "Image" }).click();
    await page.locator(".gp-segment", { hasText: "Address" }).click();
    const logo = await page.evaluate(async (id) => {
        // a 2×2 PNG uploaded to the template
        const png = Uint8Array.from(atob("iVBORw0KGgoAAAANSUhEUgAAAAIAAAACCAIAAAD91JpzAAAAFklEQVR4nGP8z8DAwMDAxMDAwMDAAAANHQEDasKb6QAAAABJRU5ErkJggg=="), (c) => c.charCodeAt(0));
        const fd = new FormData();
        fd.append("assets+", new File([png], "dot.png", { type: "image/png" }));
        const rec = await app.pb.collection("document_templates").update(id, fd);
        return `/api/files/document_templates/${id}/${rec.assets[0]}`;
    }, tpl.id);
    const withImage = page.waitForResponse(async (r) => r.url().endsWith("/api/documents/preview") && r.request().postData()?.includes("dot_") );
    await page.locator("#b_url").fill(logo);
    const imgRes = await (await withImage).json();
    assert(!imgRes.errors?.length && imgRes.boxes.some((b) => b.h > 1), "the image from its address: " + JSON.stringify(imgRes.errors));
    const imageId = await page.evaluate(() => document.querySelector(".gp-doc-hit.selected")?.dataset.block || "");
    await until(async () => (await page.locator(".gp-doc-hit.selected").count()) > 0, { message: "the image isn't on the page" });
    void imageId;
    await shot("image");
    await page.emulateMedia({ colorScheme: "dark" });
    await shot("image-dark");
    await page.emulateMedia({ colorScheme: "light" });
    const size = page.viewportSize();
    await page.setViewportSize({ width: 390, height: 800 });
    await shot("mobile-edit");
    await page.locator(".gp-doc-mobile-tabs button", { hasText: "Preview" }).click();
    await page.locator(".gp-doc-sheet canvas").first().waitFor();
    await page.waitForTimeout(400);
    await shot("mobile-preview");
    await page.locator(".gp-doc-mobile-tabs button", { hasText: "Edit" }).click();
    await page.setViewportSize(size);

    // the A5 format, then save
    await page.locator(".gp-doc-tabs button", { hasText: "Page" }).click();
    await page.locator(".gp-doc-format", { hasText: "A5" }).click();
    await page.waitForResponse((r) => r.url().endsWith("/api/documents/preview") && r.request().method() == "POST");
    await page.locator(".page-header button", { hasText: "Save" }).click();
    await until(async () => (await api(`/api/collections/document_templates/records/${tpl.id}`)).format == "a5", { message: "not saved" });
    const saved = await api(`/api/collections/document_templates/records/${tpl.id}`);
    assert(saved.body.every((b) => b.id), "the blocks have ids");
    const types = saved.body.map((b) => b.type);
    equal(types[types.findIndex((t) => t == "heading") + 1], "divider", "the divider after the title: " + types);
    assert(saved.body.some((b) => b.type == "image" && b.url == logo), "the image address is saved");
    equal(saved.collection, "products", "collection");

    // saved with a block open (Ctrl+S): its settings still edit the template
    await page.locator(".gp-doc-tabs button", { hasText: "Blocks" }).click();
    if (await page.locator(".gp-doc-back").isVisible()) {
        await page.locator(".gp-doc-back").click();
    }
    await page.locator(".gp-doc-node-main", { hasText: "Image" }).click();
    const savedAgain = page.waitForResponse((r) => r.url().includes(`/api/collections/document_templates/records/${tpl.id}`) && r.request().method() == "PATCH");
    await page.keyboard.press("Control+s");
    await savedAgain;
    const redraw = page.waitForResponse((r) => r.url().endsWith("/api/documents/preview") && r.request().postData()?.includes('"width":37'), { timeout: 5000 });
    await page.locator(".field", { hasText: "Width (mm)" }).locator("input").fill("37");
    await redraw;
    // column widths add up to 100
    await page.locator(".gp-doc-back").click();
    await page.locator(".gp-doc-palette-item", { hasText: "Columns" }).click();
    const widths = page.locator(".gp-doc-w input");
    await widths.nth(0).fill("80");
    await widths.nth(0).press("Tab");
    equal(await widths.nth(1).inputValue(), "20", "the other column");
    await widths.nth(1).fill("150");
    await widths.nth(1).press("Tab");
    equal([await widths.nth(0).inputValue(), await widths.nth(1).inputValue()].join(), "5,95", "a width over 100");
    await page.locator(".gp-segment", { hasText: "3" }).click();
    equal(await page.locator(".gp-doc-w input").evaluateAll((els) => els.reduce((a, e) => a + Number(e.value), 0)), 100, "3 columns");

    // generate it, download it from Generated
    await page.locator(".page-header button", { hasText: "Generate" }).click();
    const docs = await until(async () => {
        const list = (await api("/api/collections/documents/records")).items.filter((d) => d.template == tpl.id);
        return list.length ? list : null;
    }, { message: "no document" });
    equal(docs.length, 1, "generated");
    assert(docs[0].filename.endsWith(".pdf") && docs[0].pages >= 1, "document: " + JSON.stringify(docs[0]));
    await page.evaluate(() => (location.hash = "#/documents/generated"));
    const download = page.waitForEvent("download");
    await page.locator(".gp-doc-link", { hasText: docs[0].filename }).click();
    equal((await download).suggestedFilename(), docs[0].filename, "download");
    await shot("generated");

    // a flow: Generate document → Send email with the PDF attached
    const products = (await api("/api/collections/products/records?perPage=1")).items;
    const mailTpl = await api("/api/collections/email_templates/records", { method: "POST", body: { name: "Sheet", slug: "sheet", subject: "Your sheet", html: "<p>Attached.</p>" } });
    const flow = await api("/api/collections/flows/records", {
        method: "POST",
        body: {
            name: "Send the sheet", active: true, trigger_type: "manual", trigger: {},
            steps: [
                { key: "sheet", type: "generate_document", config: { template: tpl.id, record: products[0].id, values: {}, filename: "" } },
                { key: "mail", type: "send_email", config: { template: mailTpl.id, to: "buyer@example.com", variables: {}, attachments: ["{{steps.sheet}}"] } },
            ],
        },
    });
    await api("/api/flows/run/" + flow.id, { method: "POST", body: {} });
    const msg = await until(async () => (await api("/api/collections/email_messages/records?filter=" + encodeURIComponent("to='buyer@example.com'"))).items[0], { message: "no email" });
    equal(msg.attachments?.length, 1, "the email has the document");

    // the step in the flow editor
    await page.evaluate((id) => (location.hash = "#/flows/" + id), flow.id);
    await page.locator(".gp-step", { hasText: "Generate document" }).first().click().catch(() => {});
    await page.locator("text=Document template").first().waitFor();
    await shot("flow");
    // the email step attaches the document of the step before it
    const att = page.locator(".gp-att-option", { hasText: "Document of" }).first();
    if (!(await att.count())) {
        await page.locator(".gp-step-head, .gp-step", { hasText: "Send email" }).first().click();
    }
    await att.waitFor();
    assert(await att.locator("input").isChecked(), "the attachment is ticked");
    await att.scrollIntoViewIfNeeded();
    await shot("flow-email");
    void ctx;
}
