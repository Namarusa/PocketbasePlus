// The PocketBasePlus release in the dashboard footer, next to PocketBase's.
import { assert, login } from "../lib.mjs";

export default async function ({ page, shot }) {
    await login(page);
    const link = page.locator("a", { hasText: /^PocketBasePlus / });
    await link.first().waitFor({ timeout: 10000 });
    const text = (await link.first().innerText()).trim();
    assert(/^PocketBasePlus (v\d+\.\d+\.\d+|dev)/.test(text), "the version link: " + text);
    assert(await page.locator("a", { hasText: /^PocketBase v0\./ }).count(), "PocketBase's own version is kept");
    await shot("footer");
}
