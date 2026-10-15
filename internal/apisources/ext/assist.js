// Helpers of the call editor that save typing: the variables a template uses
// ({{id}} or {{.vars.id}}), a pasted address split into its path and query
// parameters, and a cURL command read into a call.

import { loadFunctions } from "../pocketbaseplus/functions.js";

// the words of the templates (not variables)
const keywords = new Set([
    "if", "else", "end", "range", "with", "define", "template", "block", "break", "continue", "nil", "true", "false", "vars", "secret",
    "and", "call", "html", "index", "slice", "js", "len", "not", "or", "print", "printf", "println", "urlquery", "eq", "ne", "lt", "le", "gt", "ge",
]);
let functionNames = null;
loadFunctions()
    .then((c) => (functionNames = new Set(c.functions.map((f) => f.name))))
    .catch(() => {});

/** Whether a name can be written alone ({{id}}): not a function or a template word. */
export function isShortName(name) {
    return !keywords.has(name) && !(functionNames?.has(name) ?? false);
}

/** Whether the function names are loaded (until then, no variable is guessed). */
export const namesReady = () => functionNames != null;

/** The names of the variables a template uses: {{id}}, {{upper id}}, {{.vars.id}}. */
export function templateNames(src) {
    const out = new Set();
    for (const action of String(src || "").match(/\{\{[\s\S]*?\}\}/g) || []) {
        const code = action.slice(2, -2).replace(/"(?:[^"\\]|\\.)*"|`[^`]*`|'(?:[^'\\]|\\.)*'/g, '""').replace(/\/\*[\s\S]*?\*\//g, "");
        for (const m of code.matchAll(/(^|[^\w.$])(\.vars\.)?([A-Za-z_][A-Za-z0-9_]*)/g)) {
            const [, before, viaVars, name] = m;
            if (viaVars || (!/[.$]$/.test(before) && isShortName(name) && namesReady())) {
                out.add(name);
            }
        }
    }
    return out;
}

/** A variable name for a parameter or header name: "page-size" → page_size. */
export function varNameFor(key) {
    let name = String(key || "value").replace(/[^A-Za-z0-9_]/g, "_").replace(/^_+|_+$/g, "") || "value";
    if (!/^[A-Za-z_]/.test(name)) {
        name = "v_" + name;
    }
    return isShortName(name) ? name : name + "_value";
}

/**
 * A pasted address split for a call: the path (without the base address of
 * the API) and the query parameters. Returns null for a text that isn't an
 * address.
 */
export function splitURL(text, baseUrl = "") {
    const raw = String(text || "").trim();
    if (!/^https?:\/\/\S+$/i.test(raw)) {
        return null;
    }
    const hashless = raw.split("#")[0];
    const q = hashless.indexOf("?");
    let address = q >= 0 ? hashless.slice(0, q) : hashless;
    const query = [];
    if (q >= 0) {
        for (const [k, v] of new URLSearchParams(hashless.slice(q + 1))) {
            query.push([k, v]);
        }
    }
    const base = String(baseUrl || "").replace(/\/+$/, "");
    if (base && !base.includes("{{") && address.toLowerCase().startsWith(base.toLowerCase())) {
        address = address.slice(base.length).replace(/^\/+/, "");
        return { path: address, query, inBase: true };
    }
    return { path: address, query, inBase: !base };
}

// the words of a shell command line (quotes and backslash line breaks)
function shellWords(text) {
    const words = [];
    let cur = "";
    let quote = "";
    let has = false;
    const s = String(text).replace(/\\\r?\n/g, " ");
    for (let i = 0; i < s.length; i++) {
        const c = s[i];
        if (quote) {
            if (c == quote) {
                quote = "";
            } else if (c == "\\" && quote == '"' && i + 1 < s.length) {
                cur += s[++i];
            } else {
                cur += c;
            }
        } else if (c == "'" || c == '"') {
            quote = c;
            has = true;
        } else if (/\s/.test(c)) {
            if (cur || has) {
                words.push(cur);
            }
            cur = "";
            has = false;
        } else if (c == "\\" && i + 1 < s.length) {
            cur += s[++i];
        } else {
            cur += c;
        }
    }
    if (cur || has) {
        words.push(cur);
    }
    return words;
}

/**
 * A cURL command read into the parts of a call: { method, url, headers:
 * [[name, value]], body, bodyType (json | form | text | none), form:
 * [[name, value]], user, authorization } (user: a -u sign-in, authorization:
 * an Authorization header; both belong to the sign-in of the API settings).
 */
export function parseCurl(text) {
    const words = shellWords(text);
    if (!words.length || words[0] != "curl") {
        return null;
    }
    const out = { method: "", url: "", headers: [], body: "", bodyType: "none", form: [], user: "" };
    const data = [];
    for (let i = 1; i < words.length; i++) {
        const w = words[i];
        const next = () => words[++i] ?? "";
        if (w == "-X" || w == "--request") {
            out.method = next().toUpperCase();
        } else if (w == "-H" || w == "--header") {
            const h = next();
            const c = h.indexOf(":");
            if (c > 0) {
                out.headers.push([h.slice(0, c).trim(), h.slice(c + 1).trim()]);
            }
        } else if (["-d", "--data", "--data-raw", "--data-binary", "--data-ascii", "--data-urlencode"].includes(w)) {
            data.push(next());
        } else if (w == "-u" || w == "--user") {
            out.user = next();
        } else if (w == "--url") {
            out.url = next();
        } else if (w.startsWith("-")) {
            // flags without a value we care about (-s, -L, --compressed...); a few take one
            if (["-o", "--output", "-A", "--user-agent", "-e", "--referer", "-b", "--cookie", "-m", "--max-time", "--connect-timeout"].includes(w)) {
                i++;
            }
        } else if (!out.url) {
            out.url = w;
        }
    }
    if (data.length) {
        out.body = data.join("&");
        const type = out.headers.find(([k]) => k.toLowerCase() == "content-type")?.[1]?.toLowerCase() || "";
        if (type.includes("json") || /^\s*[{[]/.test(out.body)) {
            out.bodyType = "json";
            try {
                out.body = JSON.stringify(JSON.parse(out.body), null, 2);
            } catch {
                // kept as written
            }
        } else if (type.includes("x-www-form-urlencoded") || (!type && /^[^=\s&]+=/.test(out.body))) {
            out.bodyType = "form";
            out.form = [...new URLSearchParams(out.body)];
            out.body = "";
        } else {
            out.bodyType = type.includes("xml") || /^\s*</.test(out.body) ? "xml" : "text";
        }
        out.method ||= "POST";
    }
    out.method ||= "GET";
    // the content type and the sign-in are set by the call and the API (the
    // editor tells to move an Authorization header to the sign-in)
    out.authorization = out.headers.find(([k]) => k.toLowerCase() == "authorization")?.[1] || "";
    out.headers = out.headers.filter(([k]) => !["content-type", "authorization", "content-length"].includes(k.toLowerCase()));
    return out.url ? out : null;
}
