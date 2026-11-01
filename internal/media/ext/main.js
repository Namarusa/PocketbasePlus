// PocketBasePlus Assets module (optional, see internal/media): a library of
// files shared by the pages, documents and emails.
//
// PocketBase concatenates every extension's main.js into /_/extensions.js, so
// this one is wrapped in a block to keep its names to itself.
{
    globalThis.gpInternalCollections = [...(globalThis.gpInternalCollections || []), "media_assets"];

    const mediaBase = app.pb.buildURL("/_/extensions/pocketbaseplus-media/");
    const loadMedia = () => import(mediaBase + "media.js");

    // what the other modules use (none of it without the module):
    //   gpMedia.pick({ kind: "image", multiple: false, title }) → [asset] or [] (cancelled)
    //   gpMedia.list({ kind }) → the assets (the newest first)
    //   asset: { id, name, alt, kind, mime, size, width, height, url (/media/…), href (absolute) }
    globalThis.gpMedia = {
        pick: async (opts) => (await loadMedia()).pick(opts),
        list: async (opts) => (await loadMedia()).listAssets(opts),
    };

    const links = app.store?.headerLinks;
    if (Array.isArray(links)) {
        const after = links.findIndex((l) => l.href == "#/pages");
        links.splice(after >= 0 ? after + 1 : links.length, 0, { href: "#/assets", icon: "ri-gallery-line", label: "Assets" });
    } else {
        console.warn("[pocketbaseplus-media] app.store.headerLinks not found, check internal/media/ext/main.js after the PocketBase upgrade");
    }

    app.routes.superuserOnly("#/assets", async (route) => (await loadMedia()).pageAssets(route));
}
