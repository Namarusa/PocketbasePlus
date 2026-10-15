// Helpers of the browser tests: the API as superuser, sign in to the
// dashboard, assertions.

export const base = process.env.E2E_BASE || "http://127.0.0.1:8190";
export const admin = { email: "e2e@example.com", password: "e2e-password-123" };

let token = "";

/** Calls the API as the superuser (JSON in and out). */
export async function api(path, { method = "GET", body, auth = true, headers = {} } = {}) {
    if (auth && !token) {
        const res = await fetch(base + "/api/collections/_superusers/auth-with-password", {
            method: "POST",
            headers: { "Content-Type": "application/json" },
            body: JSON.stringify({ identity: admin.email, password: admin.password }),
        });
        token = (await res.json()).token;
    }
    const res = await fetch(base + path, {
        method,
        headers: { "Content-Type": "application/json", ...(auth ? { Authorization: token } : {}), ...headers },
        body: body === undefined ? undefined : JSON.stringify(body),
    });
    const text = await res.text();
    let data = text;
    try {
        data = JSON.parse(text);
    } catch {}
    if (!res.ok && auth) {
        throw new Error(`${method} ${path}: ${res.status} ${text.slice(0, 300)}`);
    }
    return data;
}

/** Signs in to the dashboard. */
export async function login(page) {
    await page.goto(base + "/_/");
    await page.waitForSelector("input[type=email], input[name=identity]");
    await page.locator("input[type=email], input[name=identity]").first().fill(admin.email);
    await page.locator("input[type=password]").first().fill(admin.password);
    // PocketBase limits the sign ins per address: the tests sign in often, so try again
    for (let i = 0; ; i++) {
        await page.keyboard.press("Enter");
        try {
            await page.waitForFunction(() => !location.hash.includes("login"), null, { timeout: 4000 });
            return;
        } catch (err) {
            if (i >= 4) {
                throw err;
            }
            await page.locator("input[type=password]").first().click();
        }
    }
}

export function assert(ok, message) {
    if (!ok) {
        throw new Error(message);
    }
}

export function equal(got, want, message) {
    if (JSON.stringify(got) !== JSON.stringify(want)) {
        throw new Error(`${message}: got ${JSON.stringify(got)}, want ${JSON.stringify(want)}`);
    }
}

/** Waits until fn returns a truthy value (polling). */
export async function until(fn, { timeout = 10000, every = 200, message = "timeout" } = {}) {
    const end = Date.now() + timeout;
    for (;;) {
        const v = await fn();
        if (v) {
            return v;
        }
        if (Date.now() > end) {
            throw new Error(message);
        }
        await new Promise((r) => setTimeout(r, every));
    }
}
