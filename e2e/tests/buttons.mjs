// Buttons: their text in the settings (Text), next to the flow of an action
// button and the address of a sign out button; an icon of the kit stays.
import { api, assert, base, equal, login, until } from "../lib.mjs";

export default async function ({ page, shot }) {
    await login(page);
    const problems = [];
    page.on("console", (m) => m.type() == "error" && problems.push(m.text()));
    const rec = await api("/api/collections/pages/records", {
        method: "POST",
        body: {
            title: "Buttons",
            slug: "buttons",
            published: true,
            html: `<section style="padding:24px"><button id="run" type="submit" data-pb-action="">Run action</button> <button id="out" type="submit" data-pb-logout="/">Sign out</button><button id="ic" type="button"><svg data-icon="check" width="16" height="16" viewBox="0 0 24 24"><path d="M20 6 9 17l-5-5"/></svg>OK</button><form id="f" data-pb-action="" style="padding:16px"><input name="message"><button id="send" type="submit">Send</button></form></section>`,
        },
    });
    await page.evaluate((id) => (location.hash = "#/pages/" + id), rec.id);
    const frame = page.frameLocator("iframe.gjs-frame");
    const edit = async (sel, text, other) => {
        await frame.locator(sel).click();
        const input = page.locator(".gjs-trt-trait", { hasText: "Text" }).locator("input");
        equal(await input.inputValue(), await frame.locator(sel).innerText(), sel + ": its text in the setting");
        if (other) {
            await page.locator(".gjs-trt-trait", { hasText: other }).waitFor();
        }
        await input.fill(text);
        await input.press("Enter");
        await until(async () => (await frame.locator(sel).innerText()) == text, { message: sel + " shows " + text });
    };

    await edit("#run", "Book now", "Flow");
    await edit("#out", "Log out", "Then open");
    await edit("#send", "Send <the> message");
    await edit("#ic", "Done");
    equal(await frame.locator("#ic").evaluate((b) => [...b.childNodes].map((n) => n.nodeName).join()), "svg,#text", "the icon kept, before the text");
    await shot("edited");

    // the form keeps its flow setting, without a text
    await frame.locator("#f").click({ position: { x: 2, y: 2 } });
    await page.locator(".gjs-trt-trait", { hasText: "Flow" }).waitFor();
    equal(await page.locator(".gjs-trt-trait", { hasText: "Text" }).count(), 0, "no text for a form");

    await page.keyboard.press("Control+s");
    await page.locator(".gpp-builder", { hasText: "Saved" }).first().waitFor();
    const html = (await api("/api/collections/pages/records/" + rec.id)).html;
    for (const want of [/<button[^>]*id="run"[^>]*>Book now<\/button>/, /<button[^>]*id="out"[^>]*>Log out<\/button>/, /<button[^>]*id="send"[^>]*>Send &lt;the&gt; message<\/button>/]) {
        assert(want.test(html), want + " in " + html);
    }
    assert(/<button[^>]*id="ic"[^>]*><svg[^>]*data-icon="check"[^>]*>.*<\/svg>Done<\/button>/.test(html), "the icon button: " + html);
    assert(html.includes('data-pb-action=""') && html.includes('data-pb-logout="/"'), "the settings kept: " + html);
    const pub = await (await fetch(base + "/buttons")).text();
    assert(pub.includes("Book now") && pub.includes("Log out") && pub.includes("Send &lt;the&gt; message"), "published");

    equal(problems.filter((p) => !/favicon/.test(p)), [], "browser errors");
}
