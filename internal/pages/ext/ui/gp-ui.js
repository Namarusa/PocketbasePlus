// PocketBasePlus pages: charts and calendars (shadcn/ui look, Basecoat theme).
// The server draws the charts and writes the date fields: this script adds
// the chart tooltips and legend toggles, and turns the date fields into
// calendars and date pickers. Pages work without it.
(() => {
    "use strict";

    const el = (tag, cls, attrs) => {
        const e = document.createElement(tag);
        if (cls) {
            e.className = cls;
        }
        for (const [k, v] of Object.entries(attrs || {})) {
            if (v != null && v !== false) {
                e.setAttribute(k, v === true ? "" : v);
            }
        }
        return e;
    };
    const svgIcon = (d) => {
        const s = document.createElementNS("http://www.w3.org/2000/svg", "svg");
        s.setAttribute("viewBox", "0 0 24 24");
        s.setAttribute("fill", "none");
        s.setAttribute("stroke", "currentColor");
        s.setAttribute("stroke-width", "2");
        s.setAttribute("stroke-linecap", "round");
        s.setAttribute("stroke-linejoin", "round");
        s.setAttribute("aria-hidden", "true");
        s.innerHTML = d;
        return s;
    };
    const icons = {
        left: '<path d="m15 18-6-6 6-6"/>',
        right: '<path d="m9 18 6-6-6-6"/>',
        calendar: '<path d="M8 2v4"/><path d="M16 2v4"/><rect width="18" height="18" x="3" y="4" rx="2"/><path d="M3 10h18"/>',
    };

    // --- charts -----------------------------------------------------------------------------

    function compact(v) {
        const a = Math.abs(v);
        for (const [div, s] of [[1e9, "B"], [1e6, "M"], [1e3, "k"]]) {
            if (a >= div) {
                return Math.round((v / div) * 10) / 10 + s;
            }
        }
        return String(Math.round(v * 100) / 100);
    }

    function tooltipItems(tip, title, items) {
        const head = el("div", "chart-tooltip-title");
        head.textContent = title;
        const list = el("div", "chart-tooltip-items");
        for (const it of items) {
            const row = el("div", "chart-tooltip-item");
            const ind = el("span", "chart-tooltip-indicator");
            ind.style.setProperty("--chart-indicator-color", it.color);
            const label = el("span", "chart-tooltip-label");
            label.textContent = it.label;
            const value = el("span", "chart-tooltip-value");
            value.textContent = it.value;
            row.append(ind, label, value);
            list.append(row);
        }
        tip.replaceChildren(head, list);
    }

    function placeTip(fig, tip, e) {
        const r = fig.getBoundingClientRect();
        tip.hidden = false;
        let x = e.clientX - r.left + 14;
        const y = e.clientY - r.top + 14;
        if (x + tip.offsetWidth > r.width) {
            x = Math.max(0, e.clientX - r.left - tip.offsetWidth - 14);
        }
        tip.style.left = x + "px";
        tip.style.top = y + "px";
    }

    function arc(a0, a1, inner) {
        if (a1 - a0 >= 2 * Math.PI - 1e-9) {
            a1 = a0 + 2 * Math.PI - 1e-4;
        }
        const large = a1 - a0 > Math.PI ? 1 : 0;
        const p = (r, a) => `${r * Math.cos(a)} ${r * Math.sin(a)}`;
        return inner
            ? `M${p(1, a0)}A1 1 0 ${large} 1 ${p(1, a1)}L${p(inner, a1)}A${inner} ${inner} 0 ${large} 0 ${p(inner, a0)}Z`
            : `M0 0L${p(1, a0)}A1 1 0 ${large} 1 ${p(1, a1)}Z`;
    }

    function initChart(fig) {
        if (fig.gpChart) {
            return;
        }
        let d;
        try {
            d = JSON.parse(fig.getAttribute("data-gp-chart"));
        } catch {
            return;
        }
        if (!d || !d.series) {
            return;
        }
        fig.gpChart = true;
        // the tooltip shows the values: no native titles
        fig.querySelectorAll(".gp-chart-hits [title]").forEach((e) => e.removeAttribute("title"));
        fig.querySelectorAll(".gp-chart-slice title").forEach((t) => t.remove());
        const tip = el("div", "chart-tooltip gp-chart-tip", { role: "status" });
        tip.hidden = true;
        fig.append(tip);
        const off = new Set();

        if (d.type == "pie" || d.type == "donut") {
            const s = d.series[0];
            const slices = [...fig.querySelectorAll(".gp-chart-slice")];
            const center = fig.querySelector(".gp-chart-center strong");
            const draw = () => {
                const total = s.values.reduce((t, v, i) => t + (v > 0 && !off.has(i) ? v : 0), 0);
                let a = -Math.PI / 2;
                for (const p of slices) {
                    const i = +p.dataset.i;
                    const v = s.values[i];
                    p.classList.toggle("gp-off", off.has(i));
                    if (off.has(i) || !total) {
                        continue;
                    }
                    const b = a + (v / total) * 2 * Math.PI;
                    p.setAttribute("d", arc(a, b, d.type == "donut" ? 0.6 : 0));
                    a = b;
                }
                if (center) {
                    center.textContent = compact(total);
                }
            };
            fig.addEventListener("pointermove", (e) => {
                const p = e.target.closest(".gp-chart-slice");
                if (!p) {
                    tip.hidden = true;
                    return;
                }
                const i = +p.dataset.i;
                const total = s.values.reduce((t, v, j) => t + (v > 0 && !off.has(j) ? v : 0), 0);
                const pct = total ? Math.round((s.values[i] / total) * 1000) / 10 + "%" : "";
                tooltipItems(tip, d.labels[i], [{ color: s.colors[i], label: s.label, value: `${s.formatted[i]} (${pct})` }]);
                placeTip(fig, tip, e);
            });
            fig.addEventListener("pointerleave", () => (tip.hidden = true));
            fig.querySelectorAll(".chart-legend-item[data-i]").forEach((b) =>
                b.addEventListener("click", () => {
                    const i = +b.dataset.i;
                    off.has(i) ? off.delete(i) : off.add(i);
                    b.setAttribute("aria-pressed", String(!off.has(i)));
                    draw();
                }),
            );
            return;
        }

        const plot = fig.querySelector(".gp-chart-plot");
        if (!plot) {
            return;
        }
        const n = d.labels.length;
        const bar = d.type == "bar";
        const span = d.max - d.min || 1;
        const cursor = el("div", bar ? "gp-chart-cursor" : "gp-chart-crosshair");
        cursor.hidden = true;
        plot.prepend(cursor); // under the bars
        const dots = d.series.map((s) => {
            const dot = el("div", "gp-chart-dot");
            dot.style.background = s.color;
            dot.hidden = true;
            plot.append(dot);
            return dot;
        });
        const xOf = (i) => (bar ? ((i + 0.5) / n) * 100 : n == 1 ? 50 : (i / (n - 1)) * 100);
        const hide = () => {
            tip.hidden = cursor.hidden = true;
            dots.forEach((dot) => (dot.hidden = true));
        };
        plot.addEventListener("pointermove", (e) => {
            const r = plot.getBoundingClientRect();
            const rel = Math.min(Math.max((e.clientX - r.left) / r.width, 0), 0.9999);
            const i = bar ? Math.floor(rel * n) : n == 1 ? 0 : Math.round(rel * (n - 1));
            cursor.hidden = false;
            if (bar) {
                cursor.style.left = (i / n) * 100 + "%";
                cursor.style.width = 100 / n + "%";
            } else {
                cursor.style.left = xOf(i) + "%";
                let cum = 0;
                d.series.forEach((s, si) => {
                    cum += s.values[i];
                    const v = d.stacked ? cum : s.values[i];
                    dots[si].hidden = off.has(si);
                    dots[si].style.left = xOf(i) + "%";
                    dots[si].style.top = 100 - ((v - d.min) / span) * 100 + "%";
                });
            }
            const items = d.series
                .map((s, si) => ({ color: s.color, label: s.label, value: s.formatted[i], si }))
                .filter((it) => !off.has(it.si));
            tooltipItems(tip, d.labels[i], items);
            placeTip(fig, tip, e);
        });
        plot.addEventListener("pointerleave", hide);
        fig.querySelectorAll(".chart-legend-item[data-s]").forEach((b) =>
            b.addEventListener("click", () => {
                const si = +b.dataset.s;
                off.has(si) ? off.delete(si) : off.add(si);
                b.setAttribute("aria-pressed", String(!off.has(si)));
                fig.querySelectorAll(`[data-s="${si}"]:not(.chart-legend-item)`).forEach((e) => e.classList.toggle("gp-off", off.has(si)));
            }),
        );
    }

    // --- calendar ---------------------------------------------------------------------------

    const pad = (v) => String(v).padStart(2, "0");
    const iso = (d) => `${d.getUTCFullYear()}-${pad(d.getUTCMonth() + 1)}-${pad(d.getUTCDate())}`;
    const parse = (s) => {
        const m = /^(\d{4})-(\d{2})-(\d{2})/.exec(s || "");
        return m ? new Date(Date.UTC(+m[1], +m[2] - 1, +m[3])) : null;
    };
    const addDays = (d, n) => new Date(Date.UTC(d.getUTCFullYear(), d.getUTCMonth(), d.getUTCDate() + n));
    const monthStart = (d, n = 0) => new Date(Date.UTC(d.getUTCFullYear(), d.getUTCMonth() + n, 1));
    const todayISO = () => {
        const t = new Date();
        return `${t.getFullYear()}-${pad(t.getMonth() + 1)}-${pad(t.getDate())}`;
    };
    const lang = navigator.language || "en";
    const fmt = (o) => new Intl.DateTimeFormat(lang, { timeZone: "UTC", ...o });
    const monthTitle = fmt({ month: "long", year: "numeric" });
    const monthName = fmt({ month: "long" });
    const dayLabel = fmt({ dateStyle: "full" });
    const shortDate = fmt({ dateStyle: "medium" });
    const weekdayName = fmt({ weekday: "short" });

    function localeWeekStart() {
        try {
            const l = new Intl.Locale(lang);
            const info = (l.getWeekInfo && l.getWeekInfo()) || l.weekInfo;
            return info ? info.firstDay % 7 : 0;
        } catch {
            return 0;
        }
    }

    function initCalendar(root) {
        if (root.gpCal) {
            return;
        }
        let o;
        try {
            o = JSON.parse(root.getAttribute("data-gp-cal"));
        } catch {
            return;
        }
        const fallback = root.querySelector(".gp-cal-fallback");
        const inputs = fallback ? [...fallback.querySelectorAll("input")] : [];
        if (!inputs.length) {
            return;
        }
        root.gpCal = true;
        const mode = o.mode || "single";
        const months = o.months == 2 ? 2 : 1;
        const weekStart = o.weekStart ?? localeWeekStart();
        const disabledDays = new Set(o.disabled || []);
        const min = o.min == "today" ? todayISO() : o.min || "";
        const max = o.max == "today" ? todayISO() : o.max || "";
        root.dataset.mode = mode;

        // the selection, from the values of the inputs
        const st = { sel: [], from: "", to: "", time: "" };
        if (mode == "range") {
            st.from = parse(inputs[0].value) ? inputs[0].value.slice(0, 10) : "";
            st.to = inputs[1] && parse(inputs[1].value) ? inputs[1].value.slice(0, 10) : "";
        } else {
            st.sel = inputs[0].value.split(",").map((s) => s.trim()).filter((s) => parse(s)).map((s) => s.slice(0, 10));
            if (mode == "single") {
                st.sel = st.sel.slice(0, 1);
                const t = /T(\d{2}:\d{2})/.exec(inputs[0].value);
                st.time = t ? t[1] : "";
            }
        }
        const first = st.from || st.sel[0] || (min && todayISO() < min ? min : max && todayISO() > max ? max : todayISO());
        st.month = monthStart(parse(first));
        st.focus = first;

        // the inputs keep the names and send the values
        for (const input of inputs) {
            input.type = "hidden";
            input.removeAttribute("min");
            input.removeAttribute("max");
            input.removeAttribute("pattern");
        }
        const hasValue = () => (mode == "range" ? !!(st.from && st.to) : st.sel.length > 0);
        function sync() {
            const fire = (input) => {
                input.dispatchEvent(new Event("input", { bubbles: true }));
                input.dispatchEvent(new Event("change", { bubbles: true }));
            };
            if (mode == "range") {
                inputs[0].value = st.from;
                if (inputs[1]) {
                    inputs[1].value = st.to;
                    fire(inputs[1]);
                }
            } else if (mode == "multiple") {
                inputs[0].value = [...st.sel].sort().join(",");
            } else {
                inputs[0].value = st.sel[0] ? st.sel[0] + (o.time ? "T" + (st.time || "00:00") : "") : "";
            }
            fire(inputs[0]);
            if (hasValue()) {
                root.removeAttribute("aria-invalid");
            }
            updateTrigger();
        }

        const isDisabled = (s) => (min && s < min) || (max && s > max) || disabledDays.has(parse(s).getUTCDay());
        const selected = (s) => (mode == "range" ? s == st.from || s == st.to : st.sel.includes(s));

        const cal = el("div", "gp-cal");
        function render(focusSel) {
            const prev = el("button", "gp-cal-nav gp-cal-prev", { type: "button", "aria-label": "Go to the previous month" });
            prev.append(svgIcon(icons.left));
            const next = el("button", "gp-cal-nav gp-cal-next", { type: "button", "aria-label": "Go to the next month" });
            next.append(svgIcon(icons.right));
            prev.disabled = !!min && iso(addDays(st.month, -1)) < min;
            next.disabled = !!max && iso(monthStart(st.month, months)) > max;
            const wrap = el("div", "gp-cal-months");
            for (let m = 0; m < months; m++) {
                wrap.append(monthEl(monthStart(st.month, m), m));
            }
            const parts = [prev, next, wrap];
            if (o.time && mode == "single") {
                const row = el("div", "gp-cal-time");
                const id = "gpt" + Math.random().toString(36).slice(2, 8);
                const label = el("label", "", { for: id });
                label.textContent = "Time";
                const input = el("input", "", { type: "time", id, value: st.time || "" });
                input.addEventListener("change", () => {
                    st.time = input.value;
                    sync();
                });
                row.append(label, input);
                parts.push(row);
            }
            cal.replaceChildren(...parts);
            if (focusSel) {
                cal.querySelector(focusSel)?.focus();
            }
        }

        function caption(first, m) {
            const cap = el("div", "gp-cal-caption", { "aria-live": "polite" });
            if (o.captions == "dropdown" && m == 0) {
                const month = el("select", "gp-cal-select-month", { "aria-label": "Choose the month" });
                for (let i = 0; i < 12; i++) {
                    const opt = new Option(monthName.format(new Date(Date.UTC(2000, i, 1))), i);
                    opt.selected = i == first.getUTCMonth();
                    month.add(opt);
                }
                const year = el("select", "gp-cal-select-year", { "aria-label": "Choose the year" });
                const now = new Date().getFullYear();
                const y0 = min ? +min.slice(0, 4) : now - 100;
                const y1 = max ? +max.slice(0, 4) : now + 10;
                for (let y = Math.min(y0, first.getUTCFullYear()); y <= Math.max(y1, first.getUTCFullYear()); y++) {
                    const opt = new Option(y, y);
                    opt.selected = y == first.getUTCFullYear();
                    year.add(opt);
                }
                const change = (sel) => () => {
                    st.month = new Date(Date.UTC(+year.value, +month.value, 1));
                    render("." + sel);
                };
                month.addEventListener("change", change("gp-cal-select-month"));
                year.addEventListener("change", change("gp-cal-select-year"));
                cap.append(month, year);
            } else {
                cap.textContent = monthTitle.format(first);
            }
            return cap;
        }

        function monthEl(first, m) {
            const box = el("div", "gp-cal-month");
            const table = el("table", "gp-cal-grid", { role: "grid" });
            const head = el("tr");
            for (let i = 0; i < 7; i++) {
                const day = new Date(Date.UTC(2023, 0, 1 + ((weekStart + i) % 7))); // Jan 1 2023 is a Sunday
                const th = el("th", "", { scope: "col", "aria-label": fmt({ weekday: "long" }).format(day) });
                th.textContent = weekdayName.format(day).slice(0, 2);
                head.append(th);
            }
            const thead = el("thead");
            thead.append(head);
            const body = el("tbody");
            const today = todayISO();
            let d = addDays(first, -((first.getUTCDay() - weekStart + 7) % 7));
            const last = monthStart(first, 1);
            while (d < last) {
                const row = el("tr");
                for (let i = 0; i < 7; i++, d = addDays(d, 1)) {
                    const s = iso(d);
                    const td = el("td", "", { role: "gridcell" });
                    const outside = d.getUTCMonth() != first.getUTCMonth();
                    if (outside && months > 1) {
                        row.append(td); // the other month shows it
                        continue;
                    }
                    const b = el("button", "gp-cal-day", { type: "button", "data-date": s, "aria-label": dayLabel.format(d), tabindex: "-1" });
                    b.textContent = d.getUTCDate();
                    if (outside) {
                        b.classList.add("gp-outside");
                    }
                    if (s == today) {
                        b.classList.add("gp-today");
                        b.setAttribute("aria-current", "date");
                    }
                    if (isDisabled(s)) {
                        b.setAttribute("aria-disabled", "true"); // still focusable with the arrows
                    }
                    if (selected(s)) {
                        b.setAttribute("aria-selected", "true");
                    }
                    if (mode == "range" && st.from && s >= st.from && s <= (st.to || st.from)) {
                        td.classList.add("gp-in-range");
                        if (s == st.from) {
                            td.classList.add("gp-range-start");
                        }
                        if (s == (st.to || st.from)) {
                            td.classList.add("gp-range-end");
                        }
                    }
                    if (s == st.focus && !outside) {
                        b.tabIndex = 0;
                    }
                    td.append(b);
                    row.append(td);
                }
                body.append(row);
            }
            table.append(thead, body);
            if (!table.querySelector('[tabindex="0"]') && m == 0) {
                const firstDay = table.querySelector(".gp-cal-day:not(.gp-outside):not([aria-disabled])");
                if (firstDay) {
                    firstDay.tabIndex = 0;
                }
            }
            box.append(caption(first, m), table);
            return box;
        }

        // shows the month of a date
        function reveal(s) {
            const d = parse(s);
            if (d < st.month) {
                st.month = monthStart(d);
            } else if (d >= monthStart(st.month, months)) {
                st.month = monthStart(d, -(months - 1));
            }
        }

        function pick(s) {
            if (mode == "single") {
                st.sel = st.sel[0] == s && !o.required ? [] : [s];
            } else if (mode == "multiple") {
                st.sel = st.sel.includes(s) ? st.sel.filter((x) => x != s) : [...st.sel, s];
            } else if (!st.from || st.to) {
                st.from = s;
                st.to = "";
            } else if (s < st.from) {
                st.from = s;
            } else {
                st.to = s;
            }
            st.focus = s;
            reveal(s);
            sync();
            render(`[data-date="${s}"]`);
            if (popover && mode == "single" && !o.time && st.sel.length) {
                close(true);
            }
        }

        cal.addEventListener("click", (e) => {
            const b = e.target.closest(".gp-cal-day");
            if (b && !b.hasAttribute("aria-disabled")) {
                pick(b.dataset.date);
                return;
            }
            const nav = e.target.closest(".gp-cal-nav");
            if (nav && !nav.disabled) {
                const step = nav.classList.contains("gp-cal-prev") ? -1 : 1;
                st.month = monthStart(st.month, step);
                st.focus = iso(st.month);
                render(".gp-cal-" + (step < 0 ? "prev" : "next"));
            }
        });
        cal.addEventListener("keydown", (e) => {
            const b = e.target.closest(".gp-cal-day");
            if (!b) {
                return;
            }
            const d = parse(b.dataset.date);
            const dow = (d.getUTCDay() - weekStart + 7) % 7;
            const moves = {
                ArrowLeft: () => addDays(d, -1),
                ArrowRight: () => addDays(d, 1),
                ArrowUp: () => addDays(d, -7),
                ArrowDown: () => addDays(d, 7),
                Home: () => addDays(d, -dow),
                End: () => addDays(d, 6 - dow),
                PageUp: () => new Date(Date.UTC(d.getUTCFullYear() - (e.shiftKey ? 1 : 0), d.getUTCMonth() - (e.shiftKey ? 0 : 1), d.getUTCDate())),
                PageDown: () => new Date(Date.UTC(d.getUTCFullYear() + (e.shiftKey ? 1 : 0), d.getUTCMonth() + (e.shiftKey ? 0 : 1), d.getUTCDate())),
            };
            if (!moves[e.key]) {
                return;
            }
            e.preventDefault();
            let to = iso(moves[e.key]());
            if (min && to < min) {
                to = min;
            }
            if (max && to > max) {
                to = max;
            }
            st.focus = to;
            reveal(to);
            render(`[data-date="${to}"]`);
        });

        // date picker: a field opening the calendar in a popover
        let popover = null;
        let trigger = null;
        function label() {
            if (mode == "range") {
                if (!st.from) {
                    return "";
                }
                const a = parse(st.from);
                if (!st.to) {
                    return shortDate.format(a) + " –";
                }
                const b = parse(st.to);
                return shortDate.formatRange ? shortDate.formatRange(a, b) : `${shortDate.format(a)} – ${shortDate.format(b)}`;
            }
            if (mode == "multiple") {
                const list = [...st.sel].sort();
                return list.length > 2 ? `${list.length} dates` : list.map((s) => shortDate.format(parse(s))).join(", ");
            }
            if (!st.sel[0]) {
                return "";
            }
            return shortDate.format(parse(st.sel[0])) + (o.time && st.time ? ", " + st.time : "");
        }
        function updateTrigger() {
            if (!trigger) {
                return;
            }
            const text = label();
            trigger.querySelector("span").textContent = text || o.placeholder || "Pick a date";
            trigger.toggleAttribute("data-empty", !text);
        }
        const outside = (e) => {
            if (!root.contains(e.target)) {
                close(false);
            }
        };
        const escape = (e) => {
            if (e.key == "Escape") {
                e.preventDefault();
                close(true);
            }
        };
        function open() {
            popover.hidden = false;
            trigger.setAttribute("aria-expanded", "true");
            popover.classList.remove("gp-up", "gp-end");
            const t = trigger.getBoundingClientRect();
            const p = popover.getBoundingClientRect();
            if (t.bottom + p.height + 8 > innerHeight && t.top > p.height + 8) {
                popover.classList.add("gp-up");
            }
            if (t.left + p.width > document.documentElement.clientWidth) {
                popover.classList.add("gp-end");
            }
            (cal.querySelector('.gp-cal-day[tabindex="0"]') || cal.querySelector(".gp-cal-day"))?.focus();
            document.addEventListener("pointerdown", outside, true);
            document.addEventListener("keydown", escape, true);
        }
        function close(focus) {
            if (!popover || popover.hidden) {
                return;
            }
            popover.hidden = true;
            trigger.setAttribute("aria-expanded", "false");
            document.removeEventListener("pointerdown", outside, true);
            document.removeEventListener("keydown", escape, true);
            if (focus) {
                trigger.focus();
            }
        }

        if (o.picker) {
            const popId = "gpp" + Math.random().toString(36).slice(2, 8);
            trigger = el("button", "gp-dp-trigger", { type: "button", "aria-haspopup": "dialog", "aria-expanded": "false", "aria-controls": popId });
            trigger.append(svgIcon(icons.calendar), el("span"));
            popover = el("div", "gp-dp-pop", { id: popId, role: "dialog", "aria-label": o.placeholder || "Calendar" });
            popover.hidden = true;
            popover.append(cal);
            trigger.addEventListener("click", () => (popover.hidden ? open() : close(true)));
            root.append(trigger, popover);
        } else {
            root.append(cal);
        }
        root.classList.add("gp-cal-ready");
        render();
        updateTrigger();

        // required: the form isn't sent without a value
        const form = root.closest("form");
        if (o.required && form) {
            form.addEventListener(
                "submit",
                (e) => {
                    if (!hasValue()) {
                        e.preventDefault();
                        e.stopImmediatePropagation();
                        root.setAttribute("aria-invalid", "true");
                        (trigger || cal.querySelector('.gp-cal-day[tabindex="0"]'))?.focus();
                    }
                },
                true,
            );
        }
    }

    function init(root) {
        root = root || document;
        const all = (sel) => [...(root.matches && root.matches(sel) ? [root] : []), ...root.querySelectorAll(sel)];
        all("[data-gp-chart]").forEach(initChart);
        all("[data-gp-cal]").forEach(initCalendar);
    }

    window.gpUI = { init };
    if (document.readyState == "loading") {
        document.addEventListener("DOMContentLoaded", () => init());
    } else {
        init();
    }
})();
