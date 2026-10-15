// Emails → Templates: folders (grouped, collapsed, moved, renamed) and the
// account emails (designed from PocketBase's, previewed with a sample link,
// hidden from the senders).
import { api, assert, equal, login, until } from "../lib.mjs";

export default async function ({ page, shot }) {
    for (const [name, folder] of [["Order shipped", "Orders"], ["Order paid", "Orders"], ["Weekly news", ""]]) {
        await api("/api/collections/email_templates/records", {
            method: "POST",
            body: { name, slug: name.toLowerCase().replace(/ /g, "-"), subject: name, html: "<p>{{.record.name}}</p>", folder },
        });
    }
    await login(page);
    await page.evaluate(() => (location.hash = "#/emails/templates"));
    const table = page.locator(".gp-templates-table");
    await table.locator(".gp-folder-row", { hasText: "Orders" }).waitFor();
    await table.locator(".gp-folder-row", { hasText: "Account emails" }).waitFor();
    await table.locator("tr", { hasText: "Password reset" }).waitFor();
    await shot("list");

    // collapse a folder (remembered), search opens it
    await table.locator(".gp-folder-row", { hasText: "Orders" }).locator(".gp-folder-toggle").click();
    await table.locator("tr", { hasText: "Order shipped" }).waitFor({ state: "detached" });
    await page.locator(".gp-templates-search input").fill("shipped");
    await table.locator("tr", { hasText: "Order shipped" }).waitFor();
    assert(!(await table.locator("tr", { hasText: "Weekly news" }).count()), "the search filters");
    await page.locator(".gp-templates-search input").fill("");

    // move a template to a new folder
    const weekly = table.locator("tr", { hasText: "Weekly news" });
    await weekly.locator("button[aria-label='Move to a folder']").click();
    await page.locator("input#folder:visible").fill("Marketing");
    await page.locator("button[form=gpMoveTemplate]").click();
    await table.locator(".gp-folder-row", { hasText: "Marketing" }).waitFor();

    // rename a folder
    const orders = table.locator(".gp-folder-row", { hasText: "Orders" });
    await orders.hover();
    await orders.locator(".gp-folder-edit").click();
    await page.locator("input#folder_name:visible").fill("Shop");
    await page.locator("button[form=gpRenameFolder]").click();
    await table.locator(".gp-folder-row", { hasText: "Shop" }).waitFor();
    const shop = await api("/api/collections/email_templates/records?filter=" + encodeURIComponent("folder='Shop'"));
    equal(shop.items.length, 2, "renamed folder");

    // design the password reset email, from PocketBase's
    await table.locator("tr", { hasText: "Password reset" }).locator("button", { hasText: "Design it" }).click();
    await page.locator("button[form=gpCustomizeAccount]").click();
    await page.waitForFunction(() => /#\/emails\/templates\/[a-z0-9]{15}$/.test(location.hash));
    await page.locator(".gp-panel", { hasText: "Account email: Password reset" }).waitFor();
    const frame = page.frameLocator(".gp-email-frame");
    await frame.locator("a", { hasText: "Reset password" }).waitFor();
    const href = await frame.locator("a", { hasText: "Reset password" }).getAttribute("href");
    assert(href.includes("/_/#/auth/confirm-password-reset/sample-token"), "preview link: " + href);
    assert(!(await page.locator("#folder").isVisible()), "no folder for account emails");
    await page.locator(".gp-chip", { hasText: ".link" }).waitFor();
    await shot("account-editor");

    const tpl = await until(async () => (await api("/api/collections/email_templates/records?filter=" + encodeURIComponent("account_email='password_reset'"))).items[0], { message: "no account template" });
    equal(tpl.account_collection, "", "all collections");
    assert(tpl.html.includes("{{.link}}") && tpl.subject.includes("{{.app.name}}"), "converted placeholders: " + tpl.subject);

    // the senders don't offer it; the list shows it under its email
    await page.evaluate(() => (location.hash = "#/emails/templates"));
    await table.locator(".gp-sub-row", { hasText: "all auth collections" }).waitFor();
    // another collection can still get its own
    await table.locator("tr", { hasText: "Password reset" }).locator("button", { hasText: "Another collection" }).waitFor();
    await shot("list-account");
    // the verification email for every collection, the superusers included
    await table.locator("tr", { hasText: "Verification" }).locator("button", { hasText: "Design it" }).click();
    await page.locator("#account_collection").locator("xpath=ancestor-or-self::*[contains(@class,'select')][1]").click();
    await page.locator(".dropdown .dropdown-item, .select .options .option", { hasText: "superusers included" }).first().click();
    await page.locator("button[form=gpCustomizeAccount]").click();
    await page.locator(".gp-panel", { hasText: "Account email: Verification" }).waitFor();
    const verification = await until(async () => (await api("/api/collections/email_templates/records?filter=" + encodeURIComponent("account_email='verification'"))).items[0], { message: "no verification template" });
    equal(verification.account_collection, "*", "superusers included");
    await page.evaluate(() => (location.hash = "#/emails/templates"));
    await table.locator(".gp-sub-row", { hasText: "all, superusers included" }).waitFor();

    await page.evaluate(() => (location.hash = "#/emails/send"));
    await page.locator(".page-content").waitFor();
    const send = await page.evaluate(async () =>
        (await app.pb.collection("email_templates").getFullList({ filter: "account_email = ''", fields: "name" })).map((x) => x.name),
    );
    assert(!send.includes(tpl.name), "account templates in the senders");
}
