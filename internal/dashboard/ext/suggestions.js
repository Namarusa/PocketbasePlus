// Panel suggestions: ready to use configurations derived from the field types
// of a collection (counts, sums of number fields, series on date fields,
// breakdowns by select/bool/relation fields...).

const MAX = 8;

// text fields that usually hold a few repeated values (good to group by)
const categoryNames = /^(status|state|type|kind|category|group|stage|priority|level|role|source|channel|country|city|region|plan|tier|gender|language|lang|currency)$/i;

function human(name) {
    return name.replace(/_/g, " ").replace(/([a-z])([A-Z])/g, "$1 $2").toLowerCase();
}

function title(s) {
    return s.charAt(0).toUpperCase() + s.slice(1);
}

function visible(fields) {
    return fields.filter((f) => !f.hidden && f.type != "password" && f.name != "tokenKey" && f.name != "id");
}

/** Fields of a collection sorted by role (numbers, dates, categories, labels). */
export function classifyFields(collection) {
    const fields = visible(collection?.fields || []);
    const dates = fields.filter((f) => f.type == "date" || f.type == "autodate");
    // "created" first, then "updated", then the custom date fields
    dates.sort((a, b) => rank(a) - rank(b));
    return {
        numbers: fields.filter((f) => f.type == "number"),
        dates,
        bools: fields.filter((f) => f.type == "bool"),
        categories: fields.filter((f) =>
            f.type == "select"
            || f.type == "bool"
            || (f.type == "relation" && (f.maxSelect || 1) <= 1)
            || (f.type == "text" && categoryNames.test(f.name))
        ),
        labels: fields.filter((f) => ["text", "email", "url", "select", "number", "relation"].includes(f.type)),
    };
}

function rank(f) {
    return f.name == "created" ? 0 : f.name == "updated" ? 2 : 1;
}

/** Default list columns: the first readable fields, then the main date. */
function listColumns(c) {
    const cols = c.labels.slice(0, 3).map((f) => f.name);
    if (c.dates[0] && cols.length < 4) {
        cols.push(c.dates[0].name);
    }
    return cols;
}

/**
 * Returns the suggestions for a panel type and collection:
 * [{ label, name, options, icon?, hint? }]
 */
export function panelSuggestions(type, collection, allCollections = []) {
    if (!collection) {
        return [];
    }
    const c = classifyFields(collection);
    const name = human(collection.name);
    const created = c.dates.find((f) => f.name == "created");
    const out = [];
    const add = (s) => out.length < MAX && out.push(s);

    switch (type) {
        case "metric":
            add({ label: `Number of ${name}`, name: `Total ${name}`, options: { fn: "count" }, icon: "ri-hashtag" });
            if (created) {
                add({ label: `New ${name} today`, name: `New ${name} today`, options: { fn: "count", filter: "created >= @todayStart" }, icon: "ri-calendar-line" });
                add({ label: `New ${name} this month`, name: `New ${name} this month`, options: { fn: "count", filter: "created >= @monthStart" }, icon: "ri-calendar-line" });
            }
            for (const f of c.numbers.slice(0, 2)) {
                add({ label: `Sum of ${human(f.name)}`, name: `Sum of ${human(f.name)}`, options: { fn: "sum", field: f.name, abbreviate: true }, icon: "ri-functions" });
                add({ label: `Average ${human(f.name)}`, name: `Average ${human(f.name)}`, options: { fn: "avg", field: f.name }, icon: "ri-functions" });
            }
            for (const f of c.bools.slice(0, 2)) {
                add({ label: `${title(name)} where ${human(f.name)}`, name: `${title(human(f.name))} ${name}`, options: { fn: "count", filter: `${f.name} = true` }, icon: "ri-checkbox-circle-line" });
            }
            break;

        case "meter":
            for (const f of c.numbers.slice(0, 3)) {
                add({
                    label: `Average ${human(f.name)}`,
                    name: `Average ${human(f.name)}`,
                    options: { fn: "avg", field: f.name, max: f.max || "" },
                    hint: f.max ? `Target: the field maximum (${f.max})` : "The target scales automatically, set one in Display",
                });
            }
            if (created) {
                add({ label: `${title(name)} this month vs a target`, name: `${title(name)} this month`, options: { fn: "count", filter: "created >= @monthStart", max: "" } });
            }
            for (const f of c.bools.slice(0, 1)) {
                add({ label: `${title(name)} where ${human(f.name)}`, name: `${title(human(f.name))} ${name}`, options: { fn: "count", filter: `${f.name} = true`, max: "" } });
            }
            break;

        case "time_series":
            for (const f of c.dates.slice(0, 2)) {
                const by = f.name == "created" ? "" : ` (by ${human(f.name)})`;
                add({ label: `${title(name)} per day${by}`, name: `${title(name)} per day`, options: { dateField: f.name, fn: "count", precision: "day", range: "30d", style: "area" }, icon: "ri-line-chart-line" });
                add({ label: `${title(name)} per month${by}`, name: `${title(name)} per month`, options: { dateField: f.name, fn: "count", precision: "month", range: "365d", style: "bar" }, icon: "ri-bar-chart-2-line" });
            }
            if (c.dates[0]) {
                for (const f of c.numbers.slice(0, 2)) {
                    add({ label: `${title(human(f.name))} per day`, name: `${title(human(f.name))} per day`, options: { dateField: c.dates[0].name, fn: "sum", field: f.name, precision: "day", range: "30d", style: "line" }, icon: "ri-line-chart-line" });
                }
            }
            break;

        case "bar":
        case "pie":
            for (const f of c.categories.slice(0, 4)) {
                const rel = f.type == "relation" ? allCollections.find((x) => x.id == f.collectionId) : null;
                const what = rel ? human(f.name) + ` (${rel.name})` : human(f.name);
                add({ label: `${title(name)} by ${what}`, name: `${title(name)} by ${human(f.name)}`, options: { groupField: f.name, fn: "count" } });
            }
            if (c.numbers[0]) {
                for (const f of c.categories.slice(0, 2)) {
                    add({ label: `${title(human(c.numbers[0].name))} by ${human(f.name)}`, name: `${title(human(c.numbers[0].name))} by ${human(f.name)}`, options: { groupField: f.name, fn: "sum", field: c.numbers[0].name } });
                }
            }
            break;

        case "list":
            if (created) {
                add({ label: `Latest ${name}`, name: `Latest ${name}`, options: { sort: "-created", fields: listColumns(c), limit: 10 }, icon: "ri-time-line" });
            }
            for (const f of c.numbers.slice(0, 2)) {
                add({ label: `Top ${name} by ${human(f.name)}`, name: `Top ${name} by ${human(f.name)}`, options: { sort: "-" + f.name, fields: [...new Set([...listColumns(c).slice(0, 2), f.name])], limit: 10 }, icon: "ri-sort-desc" });
            }
            for (const f of c.dates.filter((d) => d.name != "created").slice(0, 1)) {
                add({ label: `Upcoming ${name} by ${human(f.name)}`, name: `Upcoming ${name}`, options: { sort: f.name, filter: `${f.name} >= @now`, fields: [...new Set([...listColumns(c).slice(0, 2), f.name])], limit: 10 }, icon: "ri-calendar-line" });
            }
            break;
    }
    return out;
}
