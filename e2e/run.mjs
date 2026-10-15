// Runs the browser tests: builds the binary into a temporary folder, starts
// it on an empty data folder (from that folder: no migration files are
// written in the project), seeds the data, runs each file of tests/ and
// stops the server.
//
//   cd e2e && npm install && npx playwright install chromium && node run.mjs [name...]
//
// E2E_PORT (8190), E2E_KEEP=1 keeps the server and its data after the run,
// E2E_CHROMIUM: a Chromium (or headless shell) to use instead of Playwright's.
// Screenshots: e2e/out/.

import { spawn, execFileSync } from "node:child_process";
import { mkdtempSync, mkdirSync, readdirSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { chromium } from "playwright";

const here = dirname(fileURLToPath(import.meta.url));
const port = process.env.E2E_PORT || "8190";
process.env.E2E_BASE = `http://127.0.0.1:${port}`;
const { admin, base } = await import("./lib.mjs");
const { seed } = await import("./seed.mjs");

const work = mkdtempSync(join(tmpdir(), "pbp-e2e-"));
const bin = join(work, process.platform == "win32" ? "pocketbaseplus.exe" : "pocketbaseplus");
const data = join(work, "pb_data");
const out = join(here, "out");
mkdirSync(out, { recursive: true });

console.log("building…");
execFileSync("go", ["build", "-o", bin, "."], { cwd: join(here, ".."), stdio: "inherit" });
execFileSync(bin, ["superuser", "upsert", admin.email, admin.password, "--dir", data], { cwd: work, stdio: "ignore" });

const server = spawn(bin, ["serve", "--http", `127.0.0.1:${port}`, "--dir", data], {
    cwd: work,
    env: { ...process.env, POCKETBASEPLUS_FLOWS_ALLOW_PRIVATE_HTTP: "1" },
    stdio: ["ignore", "ignore", "inherit"],
});
const stop = () => {
    server.kill();
    if (!process.env.E2E_KEEP) {
        rmSync(work, { recursive: true, force: true });
    }
};
process.on("SIGINT", () => (stop(), process.exit(130)));

let failed = 0;
try {
    for (let i = 0; ; i++) {
        try {
            if ((await fetch(base + "/api/health")).ok) {
                break;
            }
        } catch {}
        if (i > 100) {
            throw new Error("the server didn't start");
        }
        await new Promise((r) => setTimeout(r, 100));
    }
    console.log("seeding…");
    const ctx = await seed();

    const only = process.argv.slice(2);
    const files = readdirSync(join(here, "tests")).filter((f) => f.endsWith(".mjs") && (!only.length || only.some((o) => f.includes(o)))).sort();
    const browser = await chromium.launch({ executablePath: process.env.E2E_CHROMIUM || undefined });
    for (const file of files) {
        const test = await import(join(here, "tests", file));
        const started = Date.now();
        const context = await browser.newContext({ viewport: { width: 1280, height: 860 } });
        const page = await context.newPage();
        const errors = [];
        page.on("pageerror", (e) => errors.push(e.message));
        try {
            await test.default({ page, context, browser, ctx, shot: (name) => page.screenshot({ path: join(out, `${file.replace(".mjs", "")}-${name}.png`) }) });
            if (errors.length) {
                throw new Error("page errors: " + errors.join(" | "));
            }
            console.log(`✓ ${file} (${Date.now() - started} ms)`);
        } catch (err) {
            failed++;
            console.log(`✗ ${file}: ${err.message}` + (errors.length ? `\n  page errors: ${errors.join(" | ")}` : ""));
            await page.screenshot({ path: join(out, `${file.replace(".mjs", "")}-failure.png`) }).catch(() => {});
        }
        await context.close();
    }
    await browser.close();
} catch (err) {
    failed++;
    console.log("✗", err.message);
} finally {
    stop();
}
console.log(failed ? `${failed} failed` : "all passed");
process.exit(failed ? 1 : 0);
