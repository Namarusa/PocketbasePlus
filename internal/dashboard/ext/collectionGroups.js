// Custom groups in the native collections sidebar (like the "System" group).
//
// The sidebar is internal to the dashboard, so the groups are added next to
// its own sections: grouped collections are listed in their group and hidden
// from the default "Collections" section with a generated stylesheet. The
// groups are stored in the collection_groups collection (shared by superusers).

import { handleError, multiSelect } from "./lib.js";

const OPEN_KEY = "gpCollectionGroupsOpen";
const PINNED_KEY = "pbPinnedCollections"; // the dashboard pinned collections

const shared = store({
    groups: [],
    loaded: false,
    search: "",
    pinned: [],
    open: app.utils.getLocalHistory(OPEN_KEY, {}),
});

let loading = null;

export function loadGroups() {
    if (!loading) {
        loading = app.pb
            .collection("collection_groups")
            .getFullList({ sort: "order,name", requestKey: null })
            .then((list) => (shared.groups = list))
            .catch(handleError)
            .finally(() => {
                shared.loaded = true;
                loading = null;
            });
    }
    return loading;
}

const collectionsById = () => new Map(app.store.collections.map((c) => [c.id, c]));

function matchesSearch(c) {
    if (!shared.search) {
        return true;
    }
    const normalized = shared.search.replaceAll(" ", "").toLowerCase();
    return (c.name + c.id + c.type).toLowerCase().includes(normalized);
}

/** Collections of a group as shown in the sidebar (existing, not system, not pinned, matching the search). */
function visibleCollections(group, byId) {
    return (group.collections || [])
        .map((id) => byId.get(id))
        .filter((c) => c && !c.system && !shared.pinned.includes(c.id) && matchesSearch(c));
}

function groupedIds() {
    const ids = new Set();
    for (const g of shared.groups) {
        for (const id of g.collections || []) {
            ids.add(id);
        }
    }
    return ids;
}

/** Stylesheet hiding the grouped collections from the default section. */
function hiddenCss() {
    const ids = [...groupedIds()];
    if (!ids.length) {
        return "";
    }
    const rules = ids.map((id) => `.collections-sidebar .nav-group-regular-collections .nav-item[data-collection-id="${CSS.escape(id)}"]`);
    let css = `${rules.join(",\n")} { display: none !important; }`;

    // hide the default section when all its collections are grouped
    const grouped = new Set(ids);
    const remaining = app.store.collections.filter((c) => !c.system && !shared.pinned.includes(c.id) && matchesSearch(c) && !grouped.has(c.id));
    if (!remaining.length) {
        css += "\n.collections-sidebar .nav-group-regular-collections { display: none !important; }";
    }
    return css;
}

function collectionItem(c) {
    return t.button(
        {
            rid: c.id,
            type: "button",
            "html-data-collection-id": c.id,
            className: () => `nav-item responsive-close ${c.id == app.store.activeCollection?.id ? "active" : ""}`,
            title: c.name,
            onauxclick: (e) => {
                e.preventDefault();
                window.open(`#/collections?collection=${c.name}`, "_blank", "noreferrer,noopener");
            },
            onclick: (e) => {
                e.preventDefault();
                app.store.activeCollection = c.name;
            },
        },
        t.i({ className: app.collectionTypes?.[c.type]?.icon || app.utils.fallbackCollectionIcon, ariaHidden: true }),
        t.span({ className: "txt" }, c.name),
    );
}

function groupsSection() {
    return t.div(
        { className: "gp-collection-groups" },
        // the group elements are kept between renders (rid): their visibility
        // and items are reactive on their own (search, pins, collections changes)
        () =>
            shared.groups.map((g) => {
                const items = () => visibleCollections(g, collectionsById());
                return t.details(
                    {
                        rid: g.id + g.updated,
                        className: "nav-group gp-collection-group",
                        hidden: () => !items().length,
                        // always open while searching, like the native sections
                        open: () => !!shared.search || shared.open[g.id] !== false,
                        ontoggle: (e) => {
                            if (!shared.search) {
                                shared.open[g.id] = e.target.open;
                                app.utils.saveLocalHistory(OPEN_KEY, JSON.stringify(shared.open));
                            }
                        },
                    },
                    t.summary(null, g.name),
                    () => items().map(collectionItem),
                );
            }),
    );
}

/**
 * Adds the custom groups and the "Collection groups" button to the
 * collections sidebar element (called when it mounts).
 */
export function enhanceCollectionsSidebar(sidebar) {
    if (sidebar._gpGroups) {
        return;
    }
    sidebar._gpGroups = true;
    loadGroups();

    const readState = () => {
        shared.pinned = app.utils.getLocalHistory(PINNED_KEY, []);
        shared.search = sidebar.querySelector(".sidebar-search input")?.value || "";
    };
    readState();
    // the sidebar keeps its search and pins internally: follow its inputs
    sidebar.addEventListener("input", (e) => e.target.closest(".sidebar-search") && readState());
    // capture phase: the pin buttons stop the click propagation
    sidebar.addEventListener("click", (e) => {
        if (e.target.closest(".pin, .sidebar-search button")) {
            // the sidebar saves its pins in a (batched) watcher: read once it ran
            setTimeout(readState, 0);
            setTimeout(readState, 150);
        }
    }, true);

    const style = t.style({ id: "gpCollectionGroupsStyle" }, () => hiddenCss());
    document.head.appendChild(style);

    const section = groupsSection();
    const manageBtn = t.button(
        {
            type: "button",
            className: "btn sm circle transparent secondary link-faded gp-groups-btn",
            ariaDescription: app.attrs.tooltip("Collection groups", "left"),
            onclick: () => openGroupsModal(),
        },
        t.i({ className: "ri-folders-line", ariaHidden: true }),
    );

    // (re)insert the elements: the sidebar content can be re-rendered
    const place = () => {
        const nav = sidebar.querySelector("nav.collections-list");
        if (nav && section.parentNode !== nav) {
            nav.insertBefore(section, nav.querySelector(".nav-group-regular-collections"));
        }
        const addon = sidebar.querySelector(".sidebar-search .field.addon");
        if (addon && manageBtn.parentNode !== addon) {
            addon.insertBefore(manageBtn, addon.lastElementChild);
        }
    };
    place();
    const observer = new MutationObserver(place);
    observer.observe(sidebar, { childList: true, subtree: true });

    const cleanup = (e) => {
        if (e.detail === sidebar) {
            observer.disconnect();
            style.remove();
            document.removeEventListener("unmount:pageSidebar", cleanup);
        }
    };
    document.addEventListener("unmount:pageSidebar", cleanup);
}

// --- groups modal -----------------------------------------------------------------------

function openGroupsModal() {
    const data = store({
        groups: shared.groups.map((g) => ({ id: g.id, name: g.name, collections: [...(g.collections || [])] })),
        isSaving: false,
    });

    const groupable = () => app.store.collections.filter((c) => !c.system);

    // a collection belongs to one group: the others don't offer it
    const optionsFor = (group) => {
        const taken = new Set();
        for (const g of data.groups) {
            if (g !== group) {
                for (const id of g.collections) {
                    taken.add(id);
                }
            }
        }
        return groupable()
            .filter((c) => !taken.has(c.id))
            .map((c) => ({ value: c.id, label: c.name }));
    };

    function move(i, delta) {
        const j = i + delta;
        if (j < 0 || j >= data.groups.length) {
            return;
        }
        const list = [...data.groups];
        [list[i], list[j]] = [list[j], list[i]];
        data.groups = list;
    }

    async function save() {
        if (data.groups.some((g) => !g.name.trim())) {
            app.toasts.error("Every group needs a name.");
            return;
        }
        data.isSaving = true;
        try {
            const keep = new Set();
            for (const [i, g] of data.groups.entries()) {
                const body = { name: g.name.trim(), order: i, collections: g.collections };
                if (g.id) {
                    await app.pb.collection("collection_groups").update(g.id, body);
                    keep.add(g.id);
                } else {
                    const rec = await app.pb.collection("collection_groups").create(body);
                    keep.add(rec.id);
                }
            }
            for (const g of shared.groups) {
                if (!keep.has(g.id)) {
                    await app.pb.collection("collection_groups").delete(g.id);
                }
            }
            await loadGroups();
            app.toasts.success("Successfully saved the collection groups.");
            app.modals.close(modal);
        } catch (err) {
            handleError(err);
        } finally {
            data.isSaving = false;
        }
    }

    const modal = t.div(
        { className: "modal popup gp-groups-modal" },
        t.header({ className: "modal-header" }, t.h5({ className: "m-auto" }, "Collection groups")),
        t.form(
            {
                id: "gpGroupsForm",
                className: "modal-content",
                inert: () => data.isSaving,
                onsubmit: (e) => {
                    e.preventDefault();
                    save();
                },
            },
            t.p(
                { className: "txt-hint m-t-0" },
                "Group your collections in the sidebar, like the System group. The collections without a group stay in the Collections section.",
            ),
            () => {
                if (!data.groups.length) {
                    return t.div({ rid: "empty", className: "txt-hint txt-center p-sm" }, "No groups yet.");
                }
                return t.div(
                    // keyed by the groups (rows are added, removed and moved)
                    { rid: "list-" + data.groups.map((g, i) => (g.id || "new" + i)).join(), className: "gp-groups-list" },
                    data.groups.map((g, i) =>
                        t.div(
                            { className: "gp-group-row" },
                            t.div(
                                { className: "field gp-group-name" },
                                t.label(null, "Name"),
                                t.input({ type: "text", required: true, placeholder: "e.g. Sales", value: () => g.name, oninput: (e) => (g.name = e.target.value) }),
                            ),
                            t.div(
                                { className: "field gp-group-collections" },
                                t.label(null, "Collections"),
                                multiSelect({
                                    placeholder: "Select collections",
                                    options: () => optionsFor(g),
                                    value: () => g.collections,
                                    onchange: (v) => (g.collections = v),
                                }),
                            ),
                            t.div(
                                { className: "gp-group-actions" },
                                t.button({ type: "button", className: "btn sm circle transparent secondary", title: "Move up", disabled: i == 0, onclick: () => move(i, -1) }, t.i({ className: "ri-arrow-up-line", ariaHidden: true })),
                                t.button({ type: "button", className: "btn sm circle transparent secondary", title: "Move down", disabled: i == data.groups.length - 1, onclick: () => move(i, 1) }, t.i({ className: "ri-arrow-down-line", ariaHidden: true })),
                                t.button({ type: "button", className: "btn sm circle transparent danger", title: "Delete the group", onclick: () => (data.groups = data.groups.filter((x) => x !== g)) }, t.i({ className: "ri-delete-bin-7-line", ariaHidden: true })),
                            ),
                        )
                    ),
                );
            },
            t.button(
                { type: "button", className: "btn sm secondary m-t-sm", onclick: () => (data.groups = [...data.groups, { name: "", collections: [] }]) },
                t.i({ className: "ri-add-line", ariaHidden: true }),
                t.span({ className: "txt" }, "New group"),
            ),
        ),
        t.footer(
            { className: "modal-footer" },
            t.button({ type: "button", className: "btn transparent secondary m-r-auto", onclick: () => app.modals.close(modal) }, t.span({ className: "txt" }, "Cancel")),
            t.button(
                { type: "submit", "html-form": "gpGroupsForm", className: () => `btn expanded ${data.isSaving ? "loading" : ""}`, disabled: () => data.isSaving },
                t.span({ className: "txt" }, "Save"),
            ),
        ),
    );
    modal.onafterclose = () => modal.remove();
    document.body.appendChild(modal);
    app.modals.open(modal);
}
