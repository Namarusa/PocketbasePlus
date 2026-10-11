import { bare, checkbox, debounce, handleError, page } from "./lib.js";
import { permissionsSidebar } from "./permissions.js";

export function pageRoles() {
    app.store.title = "Roles & users";

    const data = store({
        roles: [],
        rolesLoaded: false,
        newName: "",
        newDescription: "",
        editingId: "",
        editName: "",
        editDescription: "",
        users: [],
        usersLoading: false,
        search: "",
        page: 1,
        totalPages: 1,
    });

    async function loadRoles() {
        try {
            data.roles = await app.pb.collection("roles").getFullList({ sort: "name", requestKey: "gpRoles" });
        } catch (err) {
            handleError(err);
        }
        data.rolesLoaded = true;
    }

    async function loadUsers() {
        data.usersLoading = true;
        try {
            const filter = data.search
                ? app.pb.filter("email ~ {:q} || name ~ {:q}", { q: data.search })
                : "";
            const res = await app.pb.collection("users").getList(data.page, 25, {
                filter,
                sort: "email",
                requestKey: "gpUsers",
            });
            data.users = res.items;
            data.totalPages = Math.max(1, res.totalPages);
        } catch (err) {
            handleError(err);
        }
        data.usersLoading = false;
    }

    async function createRole() {
        try {
            await app.pb.collection("roles").create({ name: data.newName, description: data.newDescription });
            data.newName = "";
            data.newDescription = "";
            app.toasts.success("Successfully created the role.");
            await loadRoles();
        } catch (err) {
            handleError(err);
        }
    }

    async function saveRole(role) {
        try {
            await app.pb.collection("roles").update(role.id, { name: data.editName, description: data.editDescription });
            data.editingId = "";
            await loadRoles();
        } catch (err) {
            handleError(err);
        }
    }

    function deleteRole(role) {
        app.modals.confirm(
            `Do you really want to delete the role "${role.name}"? Its permissions will be removed and the affected collections' API rules regenerated.`,
            async () => {
                try {
                    await app.pb.collection("roles").delete(role.id);
                    app.toasts.success("Successfully deleted the role.");
                    await Promise.all([loadRoles(), loadUsers()]);
                } catch (err) {
                    handleError(err);
                }
            },
        );
    }

    async function toggleRole(user, roleId, checked) {
        try {
            const updated = await app.pb.collection("users").update(user.id, { [checked ? "roles+" : "roles-"]: roleId }, {
                requestKey: null,
            });
            const i = data.users.findIndex((u) => u.id == user.id);
            if (i >= 0) {
                data.users[i].roles = updated.roles;
            }
        } catch (err) {
            handleError(err);
        }
    }

    const searchUsers = debounce(() => {
        data.page = 1;
        loadUsers();
    }, 250);

    loadRoles();
    loadUsers();

    return page(
        {
            pbEvent: "gpPageRoles",
            sidebar: permissionsSidebar(),
            breadcrumbs: ["Permissions", "Roles & users"],
        },
        // roles
        t.div({ className: "txt-lg m-b-sm" }, "Roles"),
        t.p({ className: "txt-hint m-t-0" }, "Users can have several roles; their permissions are combined."),
        t.div(
            { className: "gp-table-wrap" },
            t.table(
                null,
                t.thead(null, t.tr(null, t.th(null, "Name"), t.th(null, "Description"), t.th(null, ""))),
                t.tbody(null, () => {
                    if (!data.rolesLoaded) {
                        return t.tr({ rid: "loading" }, t.td({ colSpan: 3, className: "txt-center" }, t.span({ className: "loader" })));
                    }
                    if (!data.roles.length) {
                        return t.tr({ rid: "empty" }, t.td({ colSpan: 3, className: "txt-hint txt-center" }, "No roles yet."));
                    }
                    return data.roles.map((role) => {
                        if (data.editingId == role.id) {
                            return t.tr(
                                { rid: "edit_" + role.id },
                                t.td(null, bare(t.input({ type: "text", name: "name", value: data.editName, oninput: (e) => (data.editName = e.target.value) }))),
                                t.td(null, bare(t.input({ type: "text", name: "description", value: data.editDescription, oninput: (e) => (data.editDescription = e.target.value) }))),
                                t.td(
                                    { className: "txt-right txt-nowrap" },
                                    t.button({ type: "button", className: "btn sm transparent secondary", onclick: () => (data.editingId = "") }, "Cancel"),
                                    t.button({ type: "button", className: "btn sm", onclick: () => saveRole(role) }, "Save"),
                                ),
                            );
                        }
                        return t.tr(
                            { rid: role.id + role.updated },
                            t.td(null, t.strong(null, role.name)),
                            t.td({ className: "txt-hint" }, role.description),
                            t.td(
                                { className: "txt-right txt-nowrap" },
                                t.button(
                                    {
                                        type: "button",
                                        className: "btn sm circle transparent secondary",
                                        ariaLabel: app.attrs.tooltip("Edit"),
                                        onclick: () => {
                                            data.editName = role.name;
                                            data.editDescription = role.description;
                                            data.editingId = role.id;
                                        },
                                    },
                                    t.i({ className: "ri-pencil-line", ariaHidden: true }),
                                ),
                                t.button(
                                    {
                                        type: "button",
                                        className: "btn sm circle transparent danger",
                                        ariaLabel: app.attrs.tooltip("Delete"),
                                        onclick: () => deleteRole(role),
                                    },
                                    t.i({ className: "ri-delete-bin-7-line", ariaHidden: true }),
                                ),
                            ),
                        );
                    });
                }),
            ),
        ),
        t.form(
            {
                className: "flex gap-10 m-t-sm gp-new-role",
                onsubmit: (e) => {
                    e.preventDefault();
                    createRole();
                },
            },
            bare(t.input({
                type: "text",
                name: "name",
                required: true,
                placeholder: "New role name (e.g. editor)",
                value: () => data.newName,
                oninput: (e) => (data.newName = e.target.value),
            })),
            bare(t.input({
                type: "text",
                name: "description",
                placeholder: "Description (optional)",
                value: () => data.newDescription,
                oninput: (e) => (data.newDescription = e.target.value),
            })),
            t.button({ className: "btn" }, t.i({ className: "ri-add-line", ariaHidden: true }), t.span({ className: "txt" }, "Add role")),
        ),
        t.hr(),
        // users
        t.div(
            { className: "flex m-b-sm" },
            t.div({ className: "txt-lg m-r-auto" }, "Users"),
            bare(t.input({
                type: "search",
                placeholder: "Search email or name...",
                value: () => data.search,
                oninput: (e) => {
                    data.search = e.target.value;
                    searchUsers();
                },
            }), "gp-search"),
        ),
        t.div(
            { className: () => `gp-table-wrap ${data.usersLoading ? "faded" : ""}` },
            t.table(
                null,
                t.thead(
                    null,
                    t.tr(null, t.th(null, "User"), () => data.roles.map((r) => t.th({ className: "txt-center" }, r.name))),
                ),
                t.tbody(null, () => {
                    if (!data.users.length) {
                        return t.tr({ rid: "empty" }, t.td({ colSpan: data.roles.length + 1, className: "txt-hint txt-center" }, data.usersLoading ? "Loading..." : "No users found."));
                    }
                    return data.users.map((user) =>
                        t.tr(
                            null,
                            t.td(
                                null,
                                t.div(null, user.email),
                                user.name ? t.div({ className: "txt-hint txt-sm" }, user.name) : null,
                            ),
                            data.roles.map((role) =>
                                t.td(
                                    { className: "txt-center" },
                                    checkbox({
                                        id: `gp_role_${user.id}_${role.id}`,
                                        ariaLabel: `${role.name} for ${user.email}`,
                                        checked: () => !!data.users.find((u) => u.id == user.id)?.roles?.includes(role.id),
                                        onchange: (e) => toggleRole(user, role.id, e.target.checked),
                                    }),
                                )
                            ),
                        )
                    );
                }),
            ),
        ),
        t.div(
            { className: "flex gap-10 m-t-sm gp-pager", hidden: () => data.totalPages <= 1 },
            t.button(
                { type: "button", className: "btn sm secondary", disabled: () => data.page <= 1, onclick: () => (data.page--, loadUsers()) },
                "Previous",
            ),
            t.span({ className: "txt-hint" }, () => `Page ${data.page} of ${data.totalPages}`),
            t.button(
                { type: "button", className: "btn sm secondary", disabled: () => data.page >= data.totalPages, onclick: () => (data.page++, loadUsers()) },
                "Next",
            ),
        ),
    );
}
