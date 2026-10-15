// The data of the browser tests: a small shop (brands, products), a page
// listing it with a search box, filters and pages, a secret and a webhook.

import { api } from "./lib.mjs";

export const products = [
    ["Chair", 40, "seat", "Acme"],
    ["Desk", 120, "table", "Zed"],
    ["Lamp", 12.5, "light", "Acme"],
    ["Shelf", 60, "table", "Acme"],
    ["Sofa", 300, "seat", "Zed"],
    ["Stool", 25, "seat", "Acme"],
    ["Table", 150, "table", "Zed"],
    ["Torch", 8, "light", "Zed"],
    ["Bench", 70, "seat", "Acme"],
];

export const pageHTML = `<body><main style="max-width:960px;margin:0 auto;padding:16px">
<h1>Shop</h1>
<form data-pb-search="products" data-pb-search-fields="name,brand.name"><input type="search" placeholder="Search…"><button type="submit">Search</button></form>
<form data-pb-filters="products" data-pb-filter-fields="kind,brand,price" data-pb-sort-options="-price:Most expensive,name:Name"></form>
<div id="grid" data-pb-list="products" data-pb-sort="name" data-pb-limit="4" data-pb-paginate="pages" data-pb-access="rules">
<article data-pb-item><h2 data-pb-field="name">x</h2><span class="price" data-pb-field="price"></span><span class="kind" data-pb-field="kind"></span>
<img alt="" data-pb-attr-src="https://example.test/{{lower record.name}}.png" data-pb-conditions='[{"action":"hide","when":[{"field":"record.kind","op":"eq","value":"table"}]}]'>
</article><p data-pb-empty>Nothing found</p></div>
</main></body>`;

export async function seed() {
    const brands = await api("/api/collections", {
        method: "POST",
        body: { name: "brands", type: "base", listRule: "", viewRule: "", fields: [{ name: "name", type: "text", presentable: true }] },
    });
    await api("/api/collections", {
        method: "POST",
        body: {
            name: "products",
            type: "base",
            listRule: "",
            viewRule: "",
            fields: [
                { name: "name", type: "text" },
                { name: "price", type: "number" },
                { name: "kind", type: "select", maxSelect: 1, values: ["light", "seat", "table"] },
                { name: "brand", type: "relation", collectionId: brands.id, maxSelect: 1 },
            ],
        },
    });
    const brandIds = {};
    for (const name of ["Acme", "Zed"]) {
        brandIds[name] = (await api("/api/collections/brands/records", { method: "POST", body: { name } })).id;
    }
    for (const [name, price, kind, brand] of products) {
        await api("/api/collections/products/records", { method: "POST", body: { name, price, kind, brand: brandIds[brand] } });
    }
    const page = await api("/api/collections/pages/records", {
        method: "POST",
        body: { title: "Shop", slug: "shop", published: true, html: pageHTML, css: "" },
    });
    const flow = await api("/api/collections/flows/records", {
        method: "POST",
        body: {
            name: "New order",
            active: true,
            trigger_type: "webhook",
            trigger: { method: "POST", wait: true, access: "public", slug: "e2e-order" },
            steps: [{ key: "reply", name: "Reply", type: "respond", config: { format: "json", status: "201", headers: {}, body: '{"order": "{{.trigger.body.item}}"}' } }],
        },
    });
    return { brandIds, pageId: page.id, webhookId: flow.id };
}
