// Dashboard panels rendering, shared by the superuser dashboard (/_/) and the
// read-only viewer of role users (/dashboards). It only relies on the Shablon
// globals (t, store, watch) and ECharts, never on the dashboard `app` object.

export const panelTypes = {
    metric: {
        label: "Metric",
        icon: "ri-hashtag",
        description: "One aggregated value: a count, a sum, an average...",
        w: 6, h: 4, minW: 3, minH: 2,
    },
    time_series: {
        label: "Time series",
        icon: "ri-line-chart-line",
        description: "Values over time, as a line, an area or bars.",
        w: 12, h: 8, minW: 6, minH: 4,
    },
    bar: {
        label: "Bar chart",
        icon: "ri-bar-chart-2-line",
        description: "Values grouped by a field, as bars.",
        w: 12, h: 8, minW: 4, minH: 4,
    },
    pie: {
        label: "Pie / donut",
        icon: "ri-pie-chart-2-line",
        description: "The distribution of the values of a field.",
        w: 8, h: 8, minW: 4, minH: 4,
    },
    meter: {
        label: "Meter",
        icon: "ri-dashboard-3-line",
        description: "A value compared to a target, as a gauge.",
        w: 6, h: 6, minW: 3, minH: 3,
    },
    list: {
        label: "List",
        icon: "ri-list-unordered",
        description: "The latest or top records of a collection.",
        w: 8, h: 8, minW: 4, minH: 3,
    },
    label: {
        label: "Label",
        icon: "ri-text",
        description: "A heading or a short text.",
        w: 12, h: 2, minW: 2, minH: 1,
    },
};

export const aggregateFunctions = {
    count: "Count",
    count_distinct: "Count distinct",
    sum: "Sum",
    avg: "Average",
    min: "Minimum",
    max: "Maximum",
};

// --- formatting -----------------------------------------------------------------

/** Formats a number with the panel options (decimals, compact notation, prefix/suffix). */
export function formatNumber(value, options = {}) {
    if (value === null || value === undefined || value === "") {
        return "-";
    }
    if (typeof value != "number") {
        return formatValue(value);
    }
    const decimals = options.decimals === "" || options.decimals == null ? undefined : Number(options.decimals);
    const formatted = new Intl.NumberFormat(undefined, {
        notation: options.abbreviate ? "compact" : "standard",
        maximumFractionDigits: decimals ?? (Math.abs(value) < 10 ? 2 : Number.isInteger(value) ? 0 : 1),
        minimumFractionDigits: decimals ?? 0,
    }).format(value);
    return `${options.prefix || ""}${formatted}${options.suffix || ""}`;
}

const dateRegex = /^\d{4}-\d{2}-\d{2}[ T]\d{2}:\d{2}/;

/** Readable record value (dates in the local timezone, lists joined). */
export function formatValue(value) {
    if (value === null || value === undefined || value === "") {
        return "";
    }
    if (Array.isArray(value)) {
        return value.map(formatValue).join(", ");
    }
    if (typeof value == "boolean") {
        return value ? "Yes" : "No";
    }
    if (typeof value == "number") {
        return formatNumber(value);
    }
    if (typeof value == "object") {
        return JSON.stringify(value);
    }
    if (dateRegex.test(value)) {
        const d = new Date(value.replace(" ", "T"));
        if (!isNaN(d)) {
            return d.toLocaleString(undefined, { dateStyle: "medium", timeStyle: "short" });
        }
    }
    return String(value);
}

/** Readable time series bucket (UTC keys from the API). */
export function formatBucket(key, precision) {
    const parts = String(key).split(/[- :]/).map(Number);
    const d = new Date(Date.UTC(parts[0], (parts[1] || 1) - 1, parts[2] || 1, parts[3] || 0));
    if (isNaN(d)) {
        return key;
    }
    const opts = { timeZone: "UTC" };
    switch (precision) {
        case "hour":
            return d.toLocaleString(undefined, { ...opts, month: "short", day: "numeric", hour: "2-digit", minute: "2-digit" });
        case "week":
            return "Week of " + d.toLocaleDateString(undefined, { ...opts, month: "short", day: "numeric" });
        case "month":
            return d.toLocaleDateString(undefined, { ...opts, month: "short", year: "numeric" });
        case "year":
            return String(parts[0]);
    }
    return d.toLocaleDateString(undefined, { ...opts, month: "short", day: "numeric" });
}

// --- theme ------------------------------------------------------------------------

let colorCanvas;

/** Normalizes any CSS color (oklch, color(), var...) to a format ECharts understands. */
function normalizeColor(color, fallback = "#888888") {
    if (!color) {
        return fallback;
    }
    colorCanvas ||= document.createElement("canvas").getContext("2d");
    colorCanvas.fillStyle = fallback;
    colorCanvas.fillStyle = color;
    return colorCanvas.fillStyle;
}

function cssVar(name, fallback) {
    return normalizeColor(getComputedStyle(document.documentElement).getPropertyValue(name).trim(), fallback);
}

// theme colors follow the light/dark scheme of the dashboard
const themeColors = new Set(["primary", "info", "success", "warning", "danger", "accent"]);

/** Resolves a panel color option (a theme color name or any CSS color). */
export function resolveColor(color, fallbackVar = "--accentColor") {
    if (themeColors.has(color)) {
        return cssVar(`--${color}Color`, cssVar(fallbackVar));
    }
    return color ? normalizeColor(color, cssVar(fallbackVar)) : cssVar(fallbackVar);
}

export const colorChoices = [
    { value: "", label: "Default" },
    { value: "accent", label: "Accent" },
    { value: "info", label: "Blue" },
    { value: "success", label: "Green" },
    { value: "warning", label: "Orange" },
    { value: "danger", label: "Red" },
    { value: "primary", label: "Contrast" },
];

function theme() {
    return {
        text: cssVar("--surfaceTxtColor", "#1a1a1a"),
        hint: cssVar("--surfaceTxtHintColor", "#666666"),
        border: cssVar("--surfaceAlt3Color", "#dddddd"),
        surface: cssVar("--surfaceColor", "#ffffff"),
        tooltip: cssVar("--tooltipSurfaceColor", "#222222"),
        tooltipText: cssVar("--tooltipTxtColor", "#ffffff"),
        font: getComputedStyle(document.documentElement).getPropertyValue("--baseFontFamily").trim() || "sans-serif",
        // the accent (the dashboard header blue) first, like the default series color
        // distinct hues first (the info blue is close to the accent: it comes last)
        palette: ["--accentColor", "--successColor", "--warningColor", "--dangerColor"]
            .map((v) => cssVar(v))
            .concat(["#8b5cf6", "#0d9488", "#d4669b", "#8a8f2e", "#b5651d", "#5b7083"])
            .concat([cssVar("--infoColor"), cssVar("--primaryColor")]),
    };
}

// --- charts -----------------------------------------------------------------------

/**
 * Chart container: creates the ECharts instance on mount and redraws it on
 * option, data, size (resize, fullscreen) and color scheme changes.
 */
function chart(buildOptions, onclick = null) {
    let instance, resizeObserver, themeObserver, watcher;
    const draw = () => {
        if (!instance) {
            return;
        }
        instance.setOption(buildOptions(theme()), { notMerge: true, lazyUpdate: true });
    };
    return t.div({
        className: "gp-chart",
        onmount: (el) => {
            if (!window.echarts) {
                el.textContent = "Charts are unavailable.";
                return;
            }
            instance = window.echarts.init(el, null, { renderer: "canvas" });
            if (onclick) {
                instance.on("click", (params) => onclick(params));
                el.classList.add("gp-chart-clickable");
            }
            watcher = watch(() => draw());
            resizeObserver = new ResizeObserver(() => instance?.resize());
            resizeObserver.observe(el);
            themeObserver = new MutationObserver(() => draw());
            themeObserver.observe(document.documentElement, { attributes: true, attributeFilter: ["data-color-scheme"] });
        },
        onunmount: () => {
            watcher?.unwatch();
            resizeObserver?.disconnect();
            themeObserver?.disconnect();
            instance?.dispose();
            instance = null;
        },
    });
}

function baseChartOptions(th) {
    return {
        animationDuration: 400,
        textStyle: { fontFamily: th.font, color: th.text },
        tooltip: {
            trigger: "axis",
            backgroundColor: th.tooltip,
            borderWidth: 0,
            textStyle: { color: th.tooltipText, fontFamily: th.font, fontSize: 12 },
            confine: true,
        },
        grid: { left: 8, right: 16, top: 16, bottom: 8, containLabel: true },
    };
}

function axisStyle(th) {
    return {
        axisLine: { lineStyle: { color: th.border } },
        axisTick: { show: false },
        axisLabel: { color: th.hint, fontSize: 11, hideOverlap: true },
        splitLine: { lineStyle: { color: th.border, type: "dashed" } },
    };
}

function timeSeriesChart(panel, state, opts) {
    return chart((th) => {
        const o = panel.options || {};
        const data = state.data || {};
        const style = o.style || "line";
        const fmt = (v) => formatNumber(v, o);
        // a single series, or one per value of the split field
        const series = data.series || (data.points ? [{ key: "", label: panel.name || "Value", points: data.points }] : []);
        const xs = series[0]?.points?.map((p) => p.x) || [];
        const split = !!data.series;
        const base = baseChartOptions(th);
        return {
            ...base,
            color: split ? th.palette : [resolveColor(panel.color)],
            legend: split ? { type: "scroll", top: 0, textStyle: { color: th.text, fontSize: 11 }, pageTextStyle: { color: th.hint }, icon: "circle", itemWidth: 8, itemHeight: 8 } : undefined,
            grid: { ...base.grid, top: split ? 34 : 16 },
            tooltip: { ...base.tooltip, valueFormatter: fmt },
            xAxis: {
                type: "category",
                data: xs.map((x) => formatBucket(x, data.precision)),
                boundaryGap: style == "bar",
                ...axisStyle(th),
                // keep the first/last labels inside the chart
                axisLabel: { ...axisStyle(th).axisLabel, alignMinLabel: "left", alignMaxLabel: "right" },
                splitLine: { show: false },
            },
            yAxis: { type: "value", ...axisStyle(th), axisLabel: { ...axisStyle(th).axisLabel, formatter: (v) => formatNumber(v, { ...o, abbreviate: true, prefix: "", suffix: "" }) }, min: o.yMin === "" || o.yMin == null ? undefined : Number(o.yMin) },
            series: series.map((s) => ({
                type: style == "bar" ? "bar" : "line",
                name: s.label,
                data: s.points.map((p) => p.y),
                stack: split && (o.stacked || style == "area") ? "total" : undefined,
                smooth: o.curve == "smooth",
                step: o.curve == "step" ? "middle" : false,
                showSymbol: !split && xs.length <= 40,
                symbolSize: 5,
                areaStyle: style == "area" ? { opacity: split ? 0.35 : 0.18 } : undefined,
                barMaxWidth: 32,
                itemStyle: { borderRadius: style == "bar" && !(split && o.stacked) ? [3, 3, 0, 0] : 0 },
                emphasis: { focus: split ? "series" : "none" },
            })),
        };
    }, opts.onDrill ? (p) => {
        const s = (state.data?.series || [])[p.seriesIndex];
        const x = (state.data?.series?.[0]?.points || state.data?.points || [])[p.dataIndex]?.x;
        if (x) {
            opts.onDrill({ bucket: x, precision: state.data?.precision, seriesKey: s?.key });
        }
    } : null);
}

function barChart(panel, state, opts) {
    return chart((th) => {
        const o = panel.options || {};
        const data = state.data || {};
        const horizontal = !!o.horizontal;
        const split = !!data.series;
        const cats = split ? data.categories || [] : (data.groups || []).map((g) => ({ key: g.key, label: g.label }));
        const series = split ? data.series : [{ key: "", label: panel.name || "Value", values: (data.groups || []).map((g) => g.value) }];
        const labels = cats.map((c) => c.label);
        const fmt = (v) => formatNumber(v, o);
        const ordered = (list) => (horizontal ? [...list].reverse() : list);
        const category = { type: "category", data: ordered(labels), ...axisStyle(th), splitLine: { show: false }, axisLabel: { ...axisStyle(th).axisLabel, width: 120, overflow: "truncate", interval: 0, rotate: !horizontal && labels.length > 6 ? 35 : 0 } };
        const value = { type: "value", ...axisStyle(th), axisLabel: { ...axisStyle(th).axisLabel, formatter: (v) => formatNumber(v, { ...o, abbreviate: true, prefix: "", suffix: "" }) } };
        const base = baseChartOptions(th);
        return {
            ...base,
            color: split ? th.palette : [resolveColor(panel.color)],
            legend: split ? { type: "scroll", top: 0, textStyle: { color: th.text, fontSize: 11 }, pageTextStyle: { color: th.hint }, icon: "circle", itemWidth: 8, itemHeight: 8 } : undefined,
            grid: { ...base.grid, top: split ? 34 : 16 },
            tooltip: { ...base.tooltip, valueFormatter: fmt, axisPointer: { type: "shadow" } },
            xAxis: horizontal ? value : category,
            yAxis: horizontal ? category : value,
            series: series.map((s) => ({
                type: "bar",
                name: s.label,
                data: ordered(s.values),
                stack: split && o.stacked ? "total" : undefined,
                barMaxWidth: 40,
                itemStyle: { borderRadius: split && o.stacked ? 0 : horizontal ? [0, 3, 3, 0] : [3, 3, 0, 0] },
                label: o.showValues && !split ? { show: true, position: horizontal ? "right" : "top", color: th.hint, fontSize: 11, formatter: (p) => fmt(p.value) } : undefined,
                emphasis: { focus: split ? "series" : "none" },
            })),
        };
    }, opts.onDrill ? (p) => {
        const data = state.data || {};
        const cats = data.series ? data.categories || [] : data.groups || [];
        const index = panel.options?.horizontal ? cats.length - 1 - p.dataIndex : p.dataIndex;
        const cat = cats[index];
        if (cat) {
            opts.onDrill({ key: cat.key, seriesKey: data.series?.[p.seriesIndex]?.key });
        }
    } : null);
}

function pieChart(panel, state, opts) {
    return chart((th) => {
        const o = panel.options || {};
        const groups = state.data?.groups || [];
        const legend = o.legend !== false;
        return {
            ...baseChartOptions(th),
            color: th.palette,
            tooltip: { ...baseChartOptions(th).tooltip, trigger: "item", valueFormatter: (v) => formatNumber(v, o) },
            legend: legend ? { type: "scroll", orient: "vertical", right: 0, top: "middle", textStyle: { color: th.text, fontSize: 12 }, pageTextStyle: { color: th.hint }, icon: "circle", itemWidth: 9, itemHeight: 9 } : undefined,
            series: [{
                type: "pie",
                radius: o.donut ? ["48%", "75%"] : ["0%", "75%"],
                center: [legend ? "38%" : "50%", "50%"],
                avoidLabelOverlap: true,
                itemStyle: { borderColor: th.surface, borderWidth: 2, borderRadius: o.donut ? 4 : 0 },
                label: o.labels ? { show: true, color: th.text, fontSize: 11, formatter: "{b}: {d}%" } : { show: false },
                labelLine: { show: !!o.labels },
                data: groups.map((g) => ({ name: g.label, value: g.value })),
            }],
        };
    }, opts.onDrill ? (p) => {
        const g = (state.data?.groups || [])[p.dataIndex];
        if (g) {
            opts.onDrill({ key: g.key });
        }
    } : null);
}

function meterChart(panel, state) {
    return chart((th) => {
        const o = panel.options || {};
        const value = typeof state.data?.value == "number" ? state.data.value : 0;
        const max = Number(o.max) > 0 ? Number(o.max) : niceCeil(value);
        const color = resolveColor(panel.color);
        return {
            ...baseChartOptions(th),
            series: [{
                type: "gauge",
                startAngle: 210,
                endAngle: -30,
                min: 0,
                max,
                radius: "92%",
                center: ["50%", "58%"],
                progress: { show: true, roundCap: true, width: 12, itemStyle: { color } },
                axisLine: { roundCap: true, lineStyle: { width: 12, color: [[1, th.border]] } },
                axisTick: { show: false },
                splitLine: { show: false },
                axisLabel: { show: false },
                pointer: { show: false },
                anchor: { show: false },
                title: { show: true, offsetCenter: [0, "38%"], color: th.hint, fontSize: 12 },
                detail: {
                    valueAnimation: true,
                    offsetCenter: [0, "0%"],
                    fontSize: 24,
                    fontWeight: 600,
                    color: th.text,
                    formatter: () => formatNumber(state.data?.value ?? null, o),
                },
                data: [{ value: Math.min(value, max), name: `of ${formatNumber(max, { ...o, prefix: o.prefix, suffix: o.suffix })}` }],
            }],
        };
    });
}

/** A round target above the value, for meters without one (e.g. 511 -> 600). */
function niceCeil(value) {
    if (!(value > 0)) {
        return 100;
    }
    const magnitude = 10 ** Math.floor(Math.log10(value));
    for (const step of [1, 1.2, 1.5, 2, 2.5, 3, 4, 5, 6, 8, 10]) {
        if (step * magnitude > value) {
            return step * magnitude;
        }
    }
    return 10 * magnitude;
}

// --- other panels -------------------------------------------------------------------

const conditionOps = {
    ">": (a, b) => a > b,
    ">=": (a, b) => a >= b,
    "<": (a, b) => a < b,
    "<=": (a, b) => a <= b,
    "=": (a, b) => a == b,
    "!=": (a, b) => a != b,
};

/** Color of a metric value: the first matching condition, else the panel color. */
export function metricColor(panel, value) {
    for (const c of panel.options?.conditions || []) {
        const fn = conditionOps[c.op];
        if (fn && c.value !== "" && c.value != null && typeof value == "number" && fn(value, Number(c.value))) {
            return resolveColor(c.color, "--surfaceTxtColor");
        }
    }
    return panel.color ? resolveColor(panel.color) : "";
}

function metric(panel, state) {
    return t.div(
        { className: () => `gp-metric align-${panel.options?.align || "center"}` },
        t.div(
            {
                className: "gp-metric-value",
                style: () => {
                    const c = metricColor(panel, state.data?.value);
                    return c ? `color: ${c}` : "";
                },
            },
            () => formatNumber(state.data?.value ?? null, panel.options),
        ),
    );
}

function list(panel, state) {
    return t.div(
        { className: "gp-list-panel" },
        () => {
            const fields = state.data?.fields || [];
            const items = state.data?.items || [];
            if (!items.length) {
                return t.div({ rid: "empty", className: "gp-panel-empty" }, "No records.");
            }
            return t.table(
                // keyed by content so that refreshed data replaces the rows
                { rid: "table-" + JSON.stringify([fields, items]).length + "-" + items.map((i) => i.id).join(), className: "gp-list-table" },
                panel.options?.headers === false
                    ? null
                    : t.thead(null, t.tr(null, fields.map((f) => t.th(null, f.name)))),
                t.tbody(
                    null,
                    items.map((item) => t.tr(null, fields.map((f) => t.td({ title: formatValue(item[f.name]) }, formatValue(item[f.name]))))),
                ),
            );
        },
    );
}

function label(panel) {
    return t.div(
        {
            className: () => `gp-label-panel size-${panel.options?.size || "md"} align-${panel.options?.align || "left"}`,
            style: () => (panel.color ? `color: ${resolveColor(panel.color)}` : ""),
        },
        () => panel.options?.text || "",
    );
}

const renderers = {
    metric,
    meter: meterChart,
    time_series: timeSeriesChart,
    bar: barChart,
    pie: pieChart,
    list,
    label,
};

/**
 * Panel body. `state` is a store with the panel query result: { data, error, loading }.
 * Labels have no data; the other panels show a loader, their error or "no data".
 */
export function panelBody(panel, state, opts = {}) {
    const render = renderers[panel.type];
    if (!render) {
        return t.div({ className: "gp-panel-empty" }, `Unknown panel type "${panel.type}".`);
    }
    if (panel.type == "label") {
        return render(panel, state, opts);
    }
    // drill-down: clicking a chart element calls opts.onDrill(panel, target)
    const renderOpts = { onDrill: opts.onDrill ? (target) => opts.onDrill(panel, target) : null };
    const body = render(panel, state, renderOpts);
    return t.div(
        { className: "gp-panel-content" },
        body,
        t.div({ className: "gp-panel-state", hidden: () => !state.loading || !!state.data }, t.span({ className: "loader sm" })),
        t.div(
            { className: "gp-panel-state gp-panel-error", hidden: () => !state.error },
            t.i({ className: "ri-error-warning-line", ariaHidden: true }),
            t.span(null, () => state.error || ""),
        ),
        t.div(
            { className: "gp-panel-state gp-panel-empty", hidden: () => !!state.error || state.loading || !isEmptyData(panel.type, state.data) },
            "No data",
        ),
    );
}

function isEmptyData(type, data) {
    if (!data) {
        return true;
    }
    switch (type) {
        case "time_series":
            return !(data.points?.length || data.series?.some((s) => s.points?.some((p) => p.y)));
        case "bar":
            return !(data.groups?.length || data.categories?.length);
        case "pie":
            return !data.groups?.length;
        case "list":
            return false; // the list shows its own empty message
    }
    return false;
}

/** Loads ECharts once (the viewer page already includes it). */
let echartsLoading;
export function loadECharts(base) {
    if (window.echarts) {
        return Promise.resolve();
    }
    echartsLoading ||= new Promise((resolve, reject) => {
        const s = document.createElement("script");
        s.src = base + "vendor/echarts.min.js";
        s.onload = resolve;
        s.onerror = () => {
            echartsLoading = null;
            reject(new Error("Failed to load the charts library."));
        };
        document.head.appendChild(s);
    });
    return echartsLoading;
}

// --- CSV export of a panel data ------------------------------------------------------------

function csvCell(v) {
    if (v === null || v === undefined) {
        return "";
    }
    const s = typeof v == "object" ? JSON.stringify(v) : String(v);
    return /[",\n;]/.test(s) ? `"${s.replace(/"/g, '""')}"` : s;
}

/** The data of a panel as CSV rows ([] when there is nothing to export). */
export function panelRows(panel, data) {
    if (!data) {
        return [];
    }
    switch (panel.type) {
        case "metric":
        case "meter":
            return [["value"], [data.value]];
        case "time_series":
            if (data.series) {
                const xs = data.series[0]?.points?.map((p) => p.x) || [];
                return [["date", ...data.series.map((s) => s.label)], ...xs.map((x, i) => [x, ...data.series.map((s) => s.points[i]?.y)])];
            }
            return [["date", "value"], ...(data.points || []).map((p) => [p.x, p.y])];
        case "bar":
            if (data.series) {
                return [["group", ...data.series.map((s) => s.label)], ...(data.categories || []).map((c, i) => [c.label, ...data.series.map((s) => s.values[i])])];
            }
            return [["group", "value"], ...(data.groups || []).map((g) => [g.label, g.value])];
        case "pie":
            return [["group", "value"], ...(data.groups || []).map((g) => [g.label, g.value])];
        case "list": {
            const fields = (data.fields || []).map((f) => f.name);
            return [["id", ...fields], ...(data.items || []).map((item) => [item.id, ...fields.map((f) => item[f])])];
        }
    }
    return [];
}

/** Downloads the data of a panel as a CSV file. */
export function downloadPanelCSV(panel, data) {
    const rows = panelRows(panel, data);
    if (!rows.length) {
        return false;
    }
    const csv = "\ufeff" + rows.map((r) => r.map(csvCell).join(",")).join("\n") + "\n";
    const a = document.createElement("a");
    a.href = URL.createObjectURL(new Blob([csv], { type: "text/csv;charset=utf-8" }));
    a.download = (panel.name || panel.type).replace(/[^\w\- ]+/g, "").trim().replace(/\s+/g, "-").toLowerCase() + ".csv";
    a.click();
    setTimeout(() => URL.revokeObjectURL(a.href), 10_000);
    return true;
}

