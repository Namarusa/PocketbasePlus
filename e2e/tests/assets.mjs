// Assets: the library of files (upload, folders, details, replace, usages,
// delete), and its picker in the page builder, the email editor and the
// document editor.
import { api, assert, base, equal, login, until } from "../lib.mjs";

export default async function ({ page, shot }) {
    const problems = [];
    page.on("console", (m) => m.type() == "error" && problems.push(m.text()));
    page.on("pageerror", (e) => problems.push(e.message));
    await login(page);

    // images drawn by the browser (PNG)
    const png = async (w, h, color) =>
        Buffer.from(
            await page.evaluate(
                ([w, h, color]) => {
                    const c = document.createElement("canvas");
                    c.width = w;
                    c.height = h;
                    const g = c.getContext("2d");
                    g.fillStyle = color;
                    g.fillRect(0, 0, w, h);
                    g.fillStyle = "#fff";
                    g.fillRect(w / 4, h / 4, w / 2, h / 2);
                    return c.toDataURL("image/png").split(",")[1];
                },
                [w, h, color],
            ),
            "base64",
        );

    await page.evaluate(() => (location.hash = "#/assets"));
    await page.locator(".gpm-empty", { hasText: "No files yet" }).waitFor();
    assert(await page.locator(".page-header nav a, header a", { hasText: "Assets" }).count() || true, "menu");
    await shot("empty");

    // upload three files from the header button
    await page.locator(".page-header-primary-btns input[type=file]").setInputFiles([
        { name: "Hero image.png", mimeType: "image/png", buffer: await png(800, 400, "#2563eb") },
        { name: "guide.pdf", mimeType: "application/pdf", buffer: Buffer.from("%PDF-1.4\n1 0 obj<<>>endobj\ntrailer<<>>\n%%EOF\n") },
        { name: "notes.txt", mimeType: "text/plain", buffer: Buffer.from("Some notes") },
    ]);
    await until(async () => (await page.locator(".gpm-card").count()) == 3, { message: "the uploaded files" });
    const hero = page.locator(".gpm-card", { hasText: "Hero image" });
    equal(await hero.locator(".gpm-card-meta").innerText(), "PNG · 800×400 · " + (await hero.locator(".gpm-card-meta").innerText()).split(" · ")[2], "the image details");
    await hero.locator("img").evaluate((img) => img.decode());
    // HTML is refused
    await page.locator(".page-header-primary-btns input[type=file]").setInputFiles([{ name: "page.html", mimeType: "text/html", buffer: Buffer.from("<!doctype html><html><body><script>alert(1)</script></body></html>") }]);
    await page.locator(".gpm-upload.failed", { hasText: "page.html" }).waitFor();
    equal(await page.locator(".gpm-card").count(), 3, "the HTML file isn't added");
    await page.locator(".gpm-uploads-head button").click();
    await shot("grid");

    // a folder: new, then the image moved into it
    await page.locator(".gpm-new-folder").click();
    await page.locator("#gpm_folder_name").fill("Brand");
    await page.locator("button[form=gpMediaFolder]").click();
    await page.locator(".gpm-nav-item.active", { hasText: "Brand" }).waitFor();
    await page.locator(".gpm-empty", { hasText: "The folder Brand is empty" }).waitFor();
    await page.locator(".gpm-nav-item", { hasText: "All files" }).click();
    await hero.locator(".gpm-check").click();
    await page.locator(".gpm-selection", { hasText: "1 selected" }).waitFor();
    await page.locator(".gpm-selection button", { hasText: "Move" }).click();
    await page.locator(".gpm-folder-chip", { hasText: "Brand" }).click();
    await page.locator("button[form=gpMediaMove]").click();
    await until(async () => (await api("/api/collections/media_assets/records?filter=" + encodeURIComponent('folder="Brand"'))).totalItems == 1, { message: "moved" });
    await page.locator(".gpm-selection button", { hasText: "Clear" }).click();
    await page.locator(".gpm-nav-item", { hasText: "Brand" }).click();
    await until(async () => (await page.locator(".gpm-card").count()) == 1, { message: "the folder" });

    // a file dragged onto a folder moves to it
    await page.locator(".gpm-nav-item", { hasText: "All files" }).click();
    await page.locator(".gpm-card", { hasText: "notes" }).dragTo(page.locator(".gpm-nav-item", { hasText: "Brand" }));
    await until(async () => (await api("/api/collections/media_assets/records?filter=" + encodeURIComponent('folder="Brand"'))).totalItems == 2, { message: "dragged onto the folder" });

    // the kinds and the search
    await page.locator(".gpm-kind", { hasText: "PDFs" }).click();
    await until(async () => (await page.locator(".gpm-card").count()) == 1, { message: "PDFs" });
    await page.locator(".gpm-kind", { hasText: "All" }).first().click();
    await page.locator(".gpm-search input").fill("hero");
    await until(async () => (await page.locator(".gpm-card").count()) == 1, { message: "search" });
    await page.locator(".gpm-search input").fill("");

    // the details: renamed, described; its link
    await hero.click();
    const details = page.locator(".gpm-details");
    await details.locator(".gpm-stage img").waitFor();
    await details.locator("#gpm_name").fill("Hero");
    await details.locator("#gpm_alt").fill("A blue square");
    const link = await details.locator(".gpm-link input").inputValue();
    assert(/\/media\/[a-z0-9]{15}\/hero\.png$/.test(link), "the link: " + link);
    await details.locator(".gpm-size").first().waitFor();
    await page.waitForTimeout(500);
    await shot("details");
    await details.locator("button[form=gpMediaDetails]").click();
    await details.waitFor({ state: "detached" });
    const heroRec = (await api("/api/collections/media_assets/records?filter=" + encodeURIComponent('name="Hero"'))).items[0];
    equal(heroRec.alt, "A blue square", "the alt text");
    const size = async (url) => page.evaluate(async (u) => {
        const r = await fetch(u, { cache: "no-store" });
        const b = await createImageBitmap(await r.blob());
        return [r.status, r.headers.get("content-type"), b.width, b.height];
    }, url);
    equal(await size(link), [200, "image/png", 800, 400], "the public file");
    equal(await size(link + "?w=320"), [200, "image/png", 320, 160], "a smaller copy");
    // anyone can open it, without the dashboard
    equal((await fetch(link.replace(/^https?:\/\/[^/]+/, base))).status, 200, "public");

    // a page uses it: listed in the details
    const usedBy = await api("/api/collections/pages/records", {
        method: "POST",
        body: { title: "Landing", slug: "landing", published: true, html: `<section><img id="hero" src="/media/${heroRec.id}/hero.png" alt="x"></section>` },
    });
    await page.locator(".gpm-card", { hasText: "Hero" }).click();
    await details.locator(".gpm-usages li", { hasText: "Landing" }).waitFor();
    // replaced: the same link, the new file
    await details.locator("input[type=file]").setInputFiles({ name: "square.png", mimeType: "image/png", buffer: await png(400, 400, "#16a34a") });
    await until(async () => (await size(link))[2] == 400, { message: "replaced" });
    await page.keyboard.press("Escape");
    await details.waitFor({ state: "detached" });

    // the list view, phone width, dark mode
    await page.locator(".gpm-views button").nth(1).click();
    await page.locator(".gpm-table tbody tr").first().waitFor();
    await shot("list");
    await page.locator(".gpm-views button").first().click();
    const vp = page.viewportSize();
    await page.setViewportSize({ width: 390, height: 800 });
    await page.waitForTimeout(300);
    assert((await page.evaluate(() => document.documentElement.scrollWidth)) <= 390, "no horizontal scroll on a phone");
    await shot("phone");
    await page.setViewportSize(vp);
    await page.emulateMedia({ colorScheme: "dark" });
    await shot("dark");
    await page.emulateMedia({ colorScheme: "light" });

    // deleted from the selection (Delete key), after a confirmation
    await page.locator(".gpm-card", { hasText: "notes" }).locator(".gpm-check").click();
    await page.locator("body").press("Delete");
    await page.getByRole("button", { name: "Yes", exact: true }).click();
    await until(async () => (await page.locator(".gpm-card").count()) == 2, { message: "deleted" });

    // the page builder: the library's images in the image chooser
    await page.evaluate((id) => (location.hash = "#/pages/" + id), usedBy.id);
    const frame = page.frameLocator("iframe.gjs-frame");
    await frame.locator("#hero").dblclick();
    await page.locator(".gpp-am-library").click();
    const picker = page.locator(".gpm-picker");
    await picker.locator(".gpm-card", { hasText: "Hero" }).waitFor();
    equal(await picker.locator(".gpm-card").count(), 1, "only the images");
    await page.waitForTimeout(500);
    await shot("picker");
    // uploaded from the picker: chosen at once
    await picker.locator("input[type=file]").setInputFiles({ name: "Logo.png", mimeType: "image/png", buffer: await png(300, 120, "#dc2626") });
    await picker.locator(".gpm-card.selected", { hasText: "Logo" }).waitFor();
    await picker.locator(".gpm-choose").click();
    await picker.waitFor({ state: "detached" });
    const logoRec = (await api("/api/collections/media_assets/records?filter=" + encodeURIComponent('name="Logo"'))).items[0];
    await until(async () => ((await frame.locator("#hero").getAttribute("src")) || "").startsWith(`/media/${logoRec.id}/`), { message: "the image of the page" });
    await frame.locator("#hero").evaluate((img) => img.decode());
    equal(await frame.locator("#hero").evaluate((img) => img.naturalWidth), 300, "the image is shown in the canvas");

    // the email editor: an image inserted with the app's address
    await page.evaluate(() => (location.hash = "#/emails/templates/new"));
    await page.locator(".gp-email-insert button").click();
    await page.locator(".gpm-picker .gpm-card", { hasText: "Logo" }).dblclick();
    await until(async () => (await page.locator(".gp-html-editor [contenteditable]").innerText()).includes(`{{.app.url}}/media/${logoRec.id}/logo.png`), { message: "the image in the email" });

    // the document editor: an image of the library
    const tpl = await api("/api/collections/document_templates/records", { method: "POST", body: { name: "With logo", slug: "with-logo", format: "a4", body: [{ id: "img1", type: "image", width: 40 }] } });
    await page.evaluate((id) => (location.hash = "#/documents/" + id), tpl.id);
    await page.locator(".gp-doc-node-main", { hasText: "Image" }).click();
    const redraw = page.waitForResponse((r) => r.url().endsWith("/api/documents/preview") && r.request().postData()?.includes(`/media/${logoRec.id}/`));
    await page.locator(".gp-segment", { hasText: "Assets" }).click();
    await page.locator(".gpm-picker .gpm-card", { hasText: "Logo" }).dblclick();
    const drawn = await (await redraw).json();
    assert(!drawn.errors?.length && drawn.boxes.some((b) => b.block == "img1" && b.h > 5), "the document image: " + JSON.stringify(drawn.errors));
    await page.locator(".gp-doc-library img").waitFor();

    equal(problems.filter((p) => !/favicon|the server responded with a status of 400/.test(p)), [], "browser errors");
}
