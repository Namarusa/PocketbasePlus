// PocketBasePlus Pages module (optional, see internal/pages): visual page builder.
//
// PocketBase concatenates every extension's main.js into /_/extensions.js, so
// this one is wrapped in a block to keep its names to itself.
{
    // hide the pages collection from the collection pickers of the other pages
    globalThis.gpInternalCollections = [...(globalThis.gpInternalCollections || []), "pages", "pages_settings", "pages_versions"];

    const pagesBase = app.pb.buildURL("/_/extensions/pocketbaseplus-pages/");
    const loadPages = (file) => import(pagesBase + file);

    const links = app.store?.headerLinks;
    if (Array.isArray(links)) {
        const after = links.findIndex((l) => l.href == "#/forms");
        links.splice(after >= 0 ? after + 1 : links.length, 0, { href: "#/pages", icon: "ri-pages-line", label: "Pages" });
    } else {
        console.warn("[pocketbaseplus-pages] app.store.headerLinks not found, check internal/pages/ext/main.js after the PocketBase upgrade");
    }

    app.routes.superuserOnly("#/pages", async (route) => (await loadPages("pages.js")).pagePages(route));
    // before #/pages/{id}
    app.routes.superuserOnly("#/pages/theme", async (route) => (await loadPages("theme.js")).pageTheme(route));
    app.routes.superuserOnly("#/pages/members", async (route) => (await loadPages("members.js")).pageMembers(route));
    app.routes.superuserOnly("#/pages/{id}", async (route) => (await loadPages("builder.js")).pageBuilder(route));
}
