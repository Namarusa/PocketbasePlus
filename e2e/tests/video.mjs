// The Video block: dropped, a YouTube link (poster, then the player on click
// in the published page), its shape and title, and a video file of the Assets
// library (the browser's player).
import { api, assert, base, equal, login, until } from "../lib.mjs";

export default async function ({ page, shot }) {
    const problems = [];
    // the thumbnails of the video services: served here (no internet needed, no rate limits)
    const thumb = Buffer.from("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNkYPhfDwAChwGA60e6kgAAAABJRU5ErkJggg==", "base64");
    await page.context().route(/^https:\/\/(i\.ytimg\.com|www\.dailymotion\.com\/thumbnail)\//, (route) => route.fulfill({ status: 200, contentType: "image/png", body: thumb }));
    await login(page);
    // after the sign in (its retries when PocketBase limits the sign ins log errors)
    page.on("console", (m) => m.type() == "error" && problems.push(m.text()));
    const rec = await api("/api/collections/pages/records", {
        method: "POST",
        body: { title: "Watch", slug: "watch", published: true, html: `<section id="s" style="padding:24px;min-height:120px"><h1>Watch</h1></section>` },
    });
    await page.evaluate((id) => (location.hash = "#/pages/" + id), rec.id);
    const frame = page.frameLocator("iframe.gjs-frame");
    await frame.locator("#s").waitFor({ timeout: 20000 });

    // dropped from the blocks: an empty placeholder, its settings open
    await page.evaluate(async () => {
        const block = [...document.querySelectorAll(".gjs-block")].find((b) => b.textContent.trim() == "Video");
        const doc = document.querySelector("iframe.gjs-frame").contentDocument;
        const target = doc.querySelector("#s");
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
    const video = frame.locator("[data-pb-video]").first();
    await video.locator(".gpp-video-empty").waitFor();
    await page.locator(".gpp-video-settings").waitFor();

    // a YouTube link: its poster in the canvas
    await page.locator(".gpp-video-link").fill("https://www.youtube.com/watch?v=dQw4w9WgXcQ&t=42");
    await page.locator(".gpp-video-link").press("Enter");
    await video.locator("img.gp-video-poster[src*='i.ytimg.com/vi/dQw4w9WgXcQ']").waitFor();
    equal(await page.locator(".gpp-video-found").innerText(), "YouTube video.", "detected");
    await page.locator(".gpp-video-title").fill("Our launch");
    await page.locator(".gpp-video-title").press("Enter");
    await page.locator(".gpp-video-ratios button", { hasText: "21:9" }).click();
    await until(async () => (await video.getAttribute("data-gp-ratio")) == "21/9", { message: "the shape in the canvas" });
    const box = await video.boundingBox();
    assert(Math.abs(box.width / box.height - 21 / 9) < 0.05, "a 21:9 frame: " + JSON.stringify(box));
    await page.waitForTimeout(300);
    await shot("youtube");

    // a link the pages can't show: a warning
    await page.locator(".gpp-video-link").fill("https://example.com/clip");
    await page.locator(".gpp-video-link").press("Enter");
    await video.locator(".gpp-video-bad").waitFor();
    assert((await page.locator(".gpp-video-found").getAttribute("class")).includes("gpp-warn"), "the warning");
    await page.locator(".gpp-video-link").fill("https://youtu.be/dQw4w9WgXcQ?t=42");
    await page.locator(".gpp-video-link").press("Enter");
    await video.locator("img.gp-video-poster").waitFor();

    // saved: the placeholder only (no preview markup)
    await page.keyboard.press("Control+s");
    await page.locator(".gpp-builder", { hasText: "Saved" }).first().waitFor();
    const saved = await api("/api/collections/pages/records/" + rec.id);
    assert(/<div[^>]*data-pb-video="https:\/\/youtu\.be\/dQw4w9WgXcQ\?t=42"[^>]*><\/div>/.test(saved.html), "the placeholder: " + saved.html);
    assert(saved.html.includes('data-pb-video-ratio="21/9"') && saved.html.includes('data-pb-video-title="Our launch"') && !saved.html.includes("gp-video-play"), "the options: " + saved.html);

    // published: a poster linking to YouTube; a click puts the player in its place
    const pub = await page.context().newPage();
    await pub.route(/youtube|ytimg/, (route) => route.fulfill({ status: 200, contentType: "text/html", body: "<p>player</p>" }));
    await pub.goto(base + "/watch");
    const play = pub.locator("a.gp-video-play");
    equal(await play.getAttribute("href"), "https://www.youtube.com/watch?v=dQw4w9WgXcQ&t=42s", "the link without the script");
    equal(await play.getAttribute("aria-label"), "Play: Our launch", "the label");
    equal(await pub.locator("iframe").count(), 0, "nothing loaded before the click");
    await play.click();
    const player = pub.locator(".gp-video iframe");
    await player.waitFor();
    const src = await player.getAttribute("src");
    assert(src.startsWith("https://www.youtube-nocookie.com/embed/dQw4w9WgXcQ?") && src.includes("start=42") && src.includes("autoplay=1"), "the player: " + src);
    equal(await player.getAttribute("sandbox"), "allow-scripts allow-same-origin allow-popups allow-presentation", "sandboxed");
    equal(pub.url(), base + "/watch", "stayed on the page");
    await pub.close();

    // a video file of the Assets library: the browser's player
    await frame.locator("[data-pb-video]").first().click();
    await page.locator(".gpp-video-file").click();
    const mp4 = Buffer.concat([Buffer.from([0, 0, 0, 0x18]), Buffer.from("ftypmp42\0\0\0\0mp42isom"), Buffer.alloc(64)]);
    await page.locator(".gpm-picker input[type=file]").setInputFiles({ name: "clip.mp4", mimeType: "video/mp4", buffer: mp4 });
    await page.locator(".gpm-picker .gpm-card.selected", { hasText: "clip" }).waitFor();
    await page.locator(".gpm-picker .gpm-choose").click();
    await until(async () => /^\/media\/[a-z0-9]{15}\/clip\.mp4$/.test(await page.locator(".gpp-video-link").inputValue()), { message: "the file chosen" });
    equal(await page.locator(".gpp-video-found").innerText(), "A video file: the browser's player.", "a file");
    assert(await page.locator(".gpp-video-hosted").isHidden(), "no player choice for a file");
    await page.locator(".gpp-video-loop").check();
    await page.keyboard.press("Control+s");
    await until(async () => (await api("/api/collections/pages/records/" + rec.id)).html.includes('data-pb-video-loop="true"'), { message: "saved" });
    const html = await (await fetch(base + "/watch")).text();
    assert(/<video src="\/media\/[a-z0-9]{15}\/clip\.mp4" playsinline="" preload="metadata" title="Our launch" controls="" loop=""><\/video>/.test(html), "the file player: " + html.slice(html.indexOf("<body"), html.indexOf("<body") + 600));

    equal(problems.filter((p) => !/favicon/.test(p)), [], "browser errors");
}
