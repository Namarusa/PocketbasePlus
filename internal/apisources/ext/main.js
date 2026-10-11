// PocketBasePlus APIs module (optional, see internal/apisources): outside APIs and
// their saved calls, used by the "API call" step of the flows.
//
// PocketBase concatenates every extension's main.js into /_/extensions.js, so
// this one is wrapped in a block to keep its names to itself.
{
    globalThis.gpInternalCollections = [...(globalThis.gpInternalCollections || []), "api_sources", "api_calls"];
    // the flow editor offers the "API call" step and the APIs link
    globalThis.gpAPIs = true;

    const apisBase = app.pb.buildURL("/_/extensions/pocketbaseplus-apis/");
    const loadAPIs = (file) => import(apisBase + file);

    app.routes.superuserOnly("#/apis", async (route) => (await loadAPIs("apis.js")).pageAPIs(route));
    app.routes.superuserOnly("#/apis/{id}", async (route) => (await loadAPIs("apis.js")).pageCall(route));
}
