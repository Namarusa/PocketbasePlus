// PocketBasePlus pages: the PDF viewers. The server writes the options of a
// viewer (data-gp-pdf) and a link to the PDF; this script draws the PDF with
// PDF.js (loaded when a viewer comes near the screen) and the controls chosen
// in the builder: a toolbar and its buttons, a sidebar of page thumbnails,
// the zoom, all the pages or one at a time, text selection and links.
(() => {
    "use strict";

    const icons = {
        sidebar: '<rect width="18" height="18" x="3" y="3" rx="2"/><path d="M9 3v18"/>',
        up: '<path d="m18 15-6-6-6 6"/>',
        down: '<path d="m6 9 6 6 6-6"/>',
        left: '<path d="m15 18-6-6 6-6"/>',
        right: '<path d="m9 18 6-6-6-6"/>',
        minus: '<path d="M5 12h14"/>',
        plus: '<path d="M5 12h14"/><path d="M12 5v14"/>',
        search: '<circle cx="11" cy="11" r="8"/><path d="m21 21-4.3-4.3"/>',
        download: '<path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/><path d="m7 10 5 5 5-5"/><path d="M12 15V3"/>',
        print: '<path d="M6 9V2h12v7"/><path d="M6 18H4a2 2 0 0 1-2-2v-5a2 2 0 0 1 2-2h16a2 2 0 0 1 2 2v5a2 2 0 0 1-2 2h-2"/><rect width="12" height="8" x="6" y="14"/>',
        expand: '<path d="M8 3H5a2 2 0 0 0-2 2v3"/><path d="M21 8V5a2 2 0 0 0-2-2h-3"/><path d="M3 16v3a2 2 0 0 0 2 2h3"/><path d="M16 21h3a2 2 0 0 0 2-2v-3"/>',
        shrink: '<path d="M8 3v3a2 2 0 0 1-2 2H3"/><path d="M21 8h-3a2 2 0 0 1-2-2V3"/><path d="M3 16h3a2 2 0 0 1 2 2v3"/><path d="M16 21v-3a2 2 0 0 1 2-2h3"/>',
        close: '<path d="M18 6 6 18"/><path d="m6 6 12 12"/>',
        file: '<path d="M15 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V7Z"/><path d="M14 2v4a2 2 0 0 0 2 2h4"/>',
        lock: '<rect width="18" height="11" x="3" y="11" rx="2"/><path d="M7 11V7a5 5 0 0 1 10 0v4"/>',
    };
    const svg = (name) =>
        `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">${icons[name]}</svg>`;

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
    const button = (cls, label, icon) => {
        const b = el("button", "gp-pdf-btn " + cls, { type: "button", "aria-label": label, title: label });
        b.innerHTML = svg(icon);
        return b;
    };

    const CSS_UNITS = 96 / 72; // CSS pixels per PDF point: the 100% zoom
    const STEPS = [25, 33, 50, 67, 75, 80, 90, 100, 110, 125, 150, 175, 200, 250, 300, 400];
    const GAP = 12; // between the pages (px), matches the CSS
    const MAX_DRAWN = 14; // pages kept drawn (memory)

    // PDF.js, loaded once for all the viewers of the page
    let lib = null;
    function loadLib(base) {
        if (!lib) {
            lib = import(base + "pdf.min.mjs").then((m) => {
                m.GlobalWorkerOptions.workerSrc = base + "pdf.worker.min.mjs";
                return m;
            });
            lib.catch(() => (lib = null));
        }
        return lib;
    }

    // the text normalized for the search: lower case, no accents; map[i] is
    // the index in the original text of the normalized character i
    function normalize(text) {
        let out = "";
        const map = [];
        for (let i = 0; i < text.length; i++) {
            const n = text[i].normalize("NFD").replace(/\p{M}/gu, "").toLowerCase();
            for (let j = 0; j < n.length; j++) {
                out += n[j];
                map.push(i);
            }
        }
        map.push(text.length);
        return { text: out, map };
    }

    const safeLink = (u) => /^(https?:|mailto:)/i.test(u || "");

    // the file name of a Content-Disposition header
    function dispositionName(h) {
        if (!h) {
            return "";
        }
        const star = /filename\*\s*=\s*UTF-8''([^;]+)/i.exec(h);
        if (star) {
            try {
                return decodeURIComponent(star[1].trim());
            } catch {}
        }
        const plain = /filename\s*=\s*"?([^";]+)"?/i.exec(h);
        return plain ? plain[1].trim() : "";
    }

    const viewers = new Set();

    class Viewer {
        constructor(root) {
            this.root = root;
            let o = {};
            try {
                o = JSON.parse(root.getAttribute("data-gp-pdf") || "{}");
            } catch {}
            this.o = {
                src: o.src || "",
                lib: o.lib || "/p/_pdfjs/",
                toolbar: o.toolbar || "top",
                tools: Array.isArray(o.tools) ? o.tools : ["sidebar", "pages", "zoom", "search", "download", "print", "fullscreen"],
                sidebar: o.sidebar || "none",
                zoom: o.zoom || "width",
                layout: o.layout == "single" ? "single" : "scroll",
                page: Math.max(1, parseInt(o.page, 10) || 1),
                fit: !!o.fit,
                height: o.height || 0,
                background: o.background || "muted",
                frame: o.frame || "shadow",
                text: o.text !== false,
                links: o.links !== false,
                name: o.name || "",
                label: o.label || "PDF document",
                empty: o.empty || "No document to show.",
            };
            this.pages = [];
            this.current = 1;
            this.zoom = this.o.zoom; // "width" | "page" | a percent
            this.scale = 1;
            this.search = { q: "", hits: [], index: -1, token: 0 };
            this.texts = new Map(); // page -> {items, text, norm}
            this.drawn = [];
            this.destroyed = false;
            this.build();
            viewers.add(this);
        }

        has(tool) {
            return this.o.toolbar != "none" && this.o.tools.includes(tool);
        }

        // --- the frame --------------------------------------------------------------------

        build() {
            const r = this.root;
            const o = this.o;
            r.classList.add("gp-pdf", "gp-pdf--bg-" + o.background, "gp-pdf--frame-" + o.frame, "gp-pdf--" + o.layout);
            r.classList.toggle("gp-pdf--fit", o.fit);
            r.classList.toggle("gp-pdf--toolbar-bottom", o.toolbar == "bottom");
            if (o.height) {
                r.style.setProperty("--gp-pdf-h", o.height + "px");
            }
            if (!o.src) {
                return; // the server wrote the empty message
            }
            this.fallback = r.querySelector(".gp-pdf-fallback");
            r.textContent = "";

            this.body = el("div", "gp-pdf-body");
            this.scroller = el("div", "gp-pdf-scroller", { tabindex: "0", "aria-label": o.label });
            this.pagesEl = el("div", "gp-pdf-pages");
            this.scroller.append(this.pagesEl);
            if (o.sidebar != "none") {
                this.sidebar = el("div", "gp-pdf-sidebar", { role: "listbox", "aria-label": "Pages" });
                this.body.append(this.sidebar);
                this.setSidebar(o.sidebar == "open" && !this.narrow());
            }
            this.body.append(this.scroller);
            if (o.layout == "single") {
                this.prevArrow = button("gp-pdf-arrow gp-pdf-arrow--prev", "Previous page", "left");
                this.nextArrow = button("gp-pdf-arrow gp-pdf-arrow--next", "Next page", "right");
                this.prevArrow.addEventListener("click", () => this.goTo(this.current - 1));
                this.nextArrow.addEventListener("click", () => this.goTo(this.current + 1));
                this.body.append(this.prevArrow, this.nextArrow);
            }
            this.status = el("div", "gp-pdf-status", { role: "status", "aria-live": "polite" });
            this.body.append(this.status);
            if (o.toolbar != "none" && o.tools.length) {
                this.buildToolbar();
            }
            if (this.toolbar && o.toolbar == "bottom") {
                r.append(this.body, this.toolbar);
            } else if (this.toolbar) {
                r.append(this.toolbar, this.body);
            } else {
                r.append(this.body);
            }
            this.showStatus("loading", "Loading the PDF…");
            this.bind();
        }

        buildToolbar() {
            const tb = (this.toolbar = el("div", "gp-pdf-toolbar", { role: "toolbar", "aria-label": "PDF controls" }));
            const group = (cls) => {
                const g = el("div", "gp-pdf-group " + (cls || ""));
                tb.append(g);
                return g;
            };
            const start = group("gp-pdf-start");
            if (this.has("sidebar") && this.sidebar) {
                this.sideBtn = button("gp-pdf-side-btn", "Show the pages", "sidebar");
                this.sideBtn.setAttribute("aria-pressed", String(this.sidebarOpen));
                this.sideBtn.addEventListener("click", () => this.setSidebar(!this.sidebarOpen));
                start.append(this.sideBtn);
            }
            if (this.has("pages")) {
                const single = this.o.layout == "single";
                this.prevBtn = button("gp-pdf-prev", "Previous page", single ? "left" : "up");
                this.nextBtn = button("gp-pdf-next", "Next page", single ? "right" : "down");
                this.pageInput = el("input", "gp-pdf-page-input", { type: "text", inputmode: "numeric", "aria-label": "Page", value: "1", autocomplete: "off" });
                this.pageCount = el("span", "gp-pdf-page-count", { "aria-hidden": "true" });
                const label = el("label", "gp-pdf-pager");
                label.append(this.pageInput, this.pageCount);
                this.prevBtn.addEventListener("click", () => this.goTo(this.current - 1));
                this.nextBtn.addEventListener("click", () => this.goTo(this.current + 1));
                this.pageInput.addEventListener("focus", () => this.pageInput.select());
                this.pageInput.addEventListener("change", () => {
                    const n = parseInt(this.pageInput.value, 10);
                    if (n) {
                        this.goTo(n);
                    }
                    this.updatePage();
                });
                this.pageInput.addEventListener("keydown", (e) => e.key == "Enter" && this.pageInput.blur());
                start.append(this.prevBtn, label, this.nextBtn);
            }
            const middle = group("gp-pdf-middle");
            if (this.has("zoom")) {
                this.zoomOut = button("gp-pdf-zoom-out", "Zoom out", "minus");
                this.zoomIn = button("gp-pdf-zoom-in", "Zoom in", "plus");
                this.zoomSelect = el("select", "gp-pdf-zoom-select", { "aria-label": "Zoom" });
                this.zoomOut.addEventListener("click", () => this.stepZoom(-1));
                this.zoomIn.addEventListener("click", () => this.stepZoom(1));
                this.zoomSelect.addEventListener("change", () => this.setZoom(this.zoomSelect.value));
                middle.append(this.zoomOut, this.zoomSelect, this.zoomIn);
            }
            const end = group("gp-pdf-end");
            if (this.has("search")) {
                this.searchBtn = button("gp-pdf-search-btn", "Search", "search");
                this.searchBtn.addEventListener("click", () => this.toggleSearch());
                end.append(this.searchBtn);
                this.buildSearch();
            }
            if (this.has("download")) {
                const b = button("gp-pdf-download", "Download", "download");
                b.addEventListener("click", () => this.download());
                end.append(b);
            }
            if (this.has("print")) {
                const b = button("gp-pdf-print", "Print", "print");
                b.addEventListener("click", () => this.print());
                end.append(b);
            }
            if (this.has("fullscreen")) {
                this.fullBtn = button("gp-pdf-full", "Full screen", "expand");
                this.fullBtn.addEventListener("click", () => this.toggleFullscreen());
                end.append(this.fullBtn);
            }
            this.updateZoomSelect();
        }

        buildSearch() {
            const bar = (this.searchBar = el("div", "gp-pdf-searchbar", { hidden: true, role: "search" }));
            this.searchInput = el("input", "gp-pdf-search-input", { type: "text", enterkeyhint: "search", placeholder: "Search in the document", "aria-label": "Search in the document", autocomplete: "off" });
            this.searchCount = el("span", "gp-pdf-search-count", { "aria-live": "polite" });
            const prev = button("gp-pdf-search-prev", "Previous result", "up");
            const next = button("gp-pdf-search-next", "Next result", "down");
            const close = button("gp-pdf-search-close", "Close the search", "close");
            let timer = 0;
            this.searchInput.addEventListener("input", () => {
                clearTimeout(timer);
                timer = setTimeout(() => this.find(this.searchInput.value), 250);
            });
            this.searchInput.addEventListener("keydown", (e) => {
                if (e.key == "Enter") {
                    e.preventDefault();
                    clearTimeout(timer);
                    if (this.search.q != this.searchInput.value.trim()) {
                        this.find(this.searchInput.value);
                    } else {
                        this.nextHit(e.shiftKey ? -1 : 1);
                    }
                } else if (e.key == "Escape") {
                    e.stopPropagation();
                    this.toggleSearch(false);
                }
            });
            prev.addEventListener("click", () => this.nextHit(-1));
            next.addEventListener("click", () => this.nextHit(1));
            close.addEventListener("click", () => this.toggleSearch(false));
            bar.append(this.searchInput, this.searchCount, prev, next, close);
            this.toolbar.append(bar);
        }

        narrow() {
            return this.root.clientWidth > 0 && this.root.clientWidth < 640;
        }

        setSidebar(open) {
            this.sidebarOpen = !!open;
            this.root.classList.toggle("gp-pdf--sidebar-open", this.sidebarOpen);
            this.sideBtn?.setAttribute("aria-pressed", String(this.sidebarOpen));
            if (this.sidebarOpen) {
                this.sidebar?.querySelector('[aria-selected="true"]')?.scrollIntoView({ block: "nearest" });
            }
            // the pages get narrower or wider
            if (this.doc && (this.zoom == "width" || this.zoom == "page")) {
                requestAnimationFrame(() => this.applyZoom());
            }
        }

        showStatus(kind, message, extra) {
            const s = this.status;
            s.className = "gp-pdf-status gp-pdf-status--" + kind;
            s.textContent = "";
            if (!kind) {
                return;
            }
            if (kind == "loading") {
                s.append(el("span", "gp-pdf-spinner", { "aria-hidden": "true" }));
            } else {
                const i = el("span", "gp-pdf-status-icon");
                i.innerHTML = svg(kind == "password" ? "lock" : "file");
                s.append(i);
            }
            const p = el("p");
            p.textContent = message;
            s.append(p);
            if (extra) {
                s.append(extra);
            }
        }

        // --- events -----------------------------------------------------------------------

        bind() {
            this.scroller.addEventListener("scroll", () => this.onScroll(), { passive: true });
            if (this.o.fit) {
                this.onWindowScroll = () => this.onScroll();
                window.addEventListener("scroll", this.onWindowScroll, { passive: true });
            }
            // ctrl + wheel zooms the pages (not the page of the site)
            this.scroller.addEventListener(
                "wheel",
                (e) => {
                    if ((e.ctrlKey || e.metaKey) && this.doc) {
                        e.preventDefault();
                        this.stepZoom(e.deltaY < 0 ? 1 : -1);
                    }
                },
                { passive: false },
            );
            this.root.addEventListener("keydown", (e) => this.onKey(e));
            // one page at a time: swipe
            if (this.o.layout == "single") {
                let x0 = null;
                this.scroller.addEventListener("pointerdown", (e) => (x0 = e.pointerType == "mouse" ? null : e.clientX), { passive: true });
                this.scroller.addEventListener(
                    "pointerup",
                    (e) => {
                        if (x0 != null && this.scroller.scrollWidth <= this.scroller.clientWidth + 2 && Math.abs(e.clientX - x0) > 60) {
                            this.goTo(this.current + (e.clientX < x0 ? 1 : -1));
                        }
                        x0 = null;
                    },
                    { passive: true },
                );
            }
            this.resizer = new ResizeObserver(() => {
                if (this.doc && (this.zoom == "width" || this.zoom == "page") && this.root.clientWidth != this.lastWidth) {
                    this.applyZoom();
                }
                this.lastWidth = this.root.clientWidth;
            });
            this.resizer.observe(this.root);
            this.onFullscreen = () => {
                const on = document.fullscreenElement == this.root;
                this.fullBtn?.setAttribute("aria-label", on ? "Exit full screen" : "Full screen");
                this.fullBtn?.setAttribute("title", on ? "Exit full screen" : "Full screen");
                if (this.fullBtn) {
                    this.fullBtn.innerHTML = svg(on ? "shrink" : "expand");
                }
            };
            document.addEventListener("fullscreenchange", this.onFullscreen);
            // loaded when it comes near the screen
            this.starter = new IntersectionObserver(
                (entries) => {
                    if (entries.some((e) => e.isIntersecting)) {
                        this.starter.disconnect();
                        this.load();
                    }
                },
                { rootMargin: "400px" },
            );
            this.starter.observe(this.root);
        }

        onKey(e) {
            const t = e.target;
            const typing = t.matches?.("input, select, textarea");
            if ((e.ctrlKey || e.metaKey) && e.key == "f" && this.searchBar) {
                e.preventDefault();
                this.toggleSearch(true);
                return;
            }
            if (e.key == "Escape") {
                if (this.searchBar && !this.searchBar.hidden) {
                    this.toggleSearch(false);
                } else if (this.root.classList.contains("gp-pdf--expanded")) {
                    this.toggleFullscreen(false);
                }
                return;
            }
            if (typing || !this.doc) {
                return;
            }
            const single = this.o.layout == "single";
            if ((e.key == "+" || e.key == "=") && !e.ctrlKey && !e.metaKey) {
                e.preventDefault();
                this.stepZoom(1);
            } else if (e.key == "-" && !e.ctrlKey && !e.metaKey) {
                e.preventDefault();
                this.stepZoom(-1);
            } else if (e.key == "Home" || e.key == "End") {
                e.preventDefault();
                this.goTo(e.key == "Home" ? 1 : this.pages.length);
            } else if (single && (e.key == "ArrowRight" || e.key == "PageDown" || e.key == "ArrowLeft" || e.key == "PageUp")) {
                const horizontal = this.scroller.scrollWidth > this.scroller.clientWidth + 2;
                if (horizontal && e.key.startsWith("Arrow")) {
                    return;
                }
                e.preventDefault();
                this.goTo(this.current + (e.key == "ArrowRight" || e.key == "PageDown" ? 1 : -1));
            } else if (!single && (e.key == "PageDown" || e.key == "PageUp") && !this.o.fit) {
                e.preventDefault();
                this.goTo(this.current + (e.key == "PageDown" ? 1 : -1));
            }
        }

        // --- loading ----------------------------------------------------------------------

        async load(password) {
            let pdfjs;
            try {
                pdfjs = await loadLib(this.o.lib);
                if (!this.data) {
                    const res = await fetch(this.o.src, { credentials: "same-origin" });
                    if (!res.ok) {
                        throw Object.assign(new Error(), { status: res.status });
                    }
                    this.name = this.o.name || dispositionName(res.headers.get("Content-Disposition")) || decodeURIComponent((new URL(this.o.src, location.href).pathname.split("/").pop() || "document.pdf")).replace(/_[a-z0-9]{10}(\.pdf)$/i, "$1");
                    this.data = new Uint8Array(await res.arrayBuffer());
                }
                if (this.destroyed) {
                    return;
                }
                this.task = pdfjs.getDocument({
                    data: this.data.slice(), // PDF.js takes the buffer: kept for the download and a password
                    password,
                    isEvalSupported: false,
                    cMapUrl: this.o.lib + "cmaps/",
                    cMapPacked: true,
                    standardFontDataUrl: this.o.lib + "standard_fonts/",
                });
                this.doc = await this.task.promise;
            } catch (err) {
                if (this.destroyed) {
                    return;
                }
                this.fail(err, pdfjs);
                return;
            }
            if (this.destroyed) {
                this.doc.destroy();
                return;
            }
            this.pdfjs = pdfjs;
            await this.setup();
        }

        fail(err, pdfjs) {
            if (pdfjs && err?.name == "PasswordException") {
                const form = el("form", "gp-pdf-password");
                const input = el("input", "gp-pdf-password-input", { type: "password", "aria-label": "Password", placeholder: "Password", autocomplete: "off" });
                const ok = el("button", "gp-pdf-text-btn", { type: "submit" });
                ok.textContent = "Open";
                form.append(input, ok);
                form.addEventListener("submit", (e) => {
                    e.preventDefault();
                    this.showStatus("loading", "Opening…");
                    this.load(input.value);
                });
                this.showStatus("password", err.code == 2 ? "Wrong password, try again." : "This PDF is protected by a password.", form);
                setTimeout(() => input.focus(), 0);
                return;
            }
            let message = "The PDF couldn't be shown.";
            if (err?.status == 403) {
                message = "This link expired: reload the page to see the document.";
            } else if (err?.status == 404) {
                message = "The PDF wasn't found.";
            } else if (err?.status) {
                message = "The PDF couldn't be loaded (error " + err.status + ").";
            } else if (err instanceof TypeError) {
                message = "The PDF couldn't be loaded: its site may not allow it.";
            } else if (err?.name == "InvalidPDFException") {
                message = "This file isn't a valid PDF.";
            }
            const link = el("a", "gp-pdf-text-btn", { href: this.o.src, target: "_blank", rel: "noopener" });
            link.textContent = "Open the PDF";
            this.showStatus("error", message, err?.status == 404 ? null : link);
        }

        async setup() {
            const n = this.doc.numPages;
            const first = await this.doc.getPage(1);
            const base = first.getViewport({ scale: 1 });
            for (let i = 1; i <= n; i++) {
                const page = { n: i, w: base.width, h: base.height, el: el("div", "gp-pdf-page", { "data-page": i, role: "group", "aria-label": `Page ${i} of ${n}` }) };
                if (i == 1) {
                    page.page = first;
                }
                this.pages.push(page);
                this.pagesEl.append(page.el);
            }
            this.root.classList.toggle("gp-pdf--one-page", n == 1);
            if (this.pageCount) {
                this.pageCount.textContent = "/ " + n;
                this.pageInput.size = String(n).length;
            }
            this.current = Math.min(this.o.page, n);
            this.showStatus("");
            this.watchPages();
            this.applyZoom();
            if (this.current > 1) {
                this.goTo(this.current, true);
            }
            this.updatePage();
            this.buildThumbs();
            // the real size of every page (they may differ from the first one)
            for (let i = 2; i <= n && !this.destroyed; i++) {
                const p = this.pages[i - 1];
                try {
                    p.page = await this.doc.getPage(i);
                } catch {
                    continue;
                }
                const vp = p.page.getViewport({ scale: 1 });
                if (vp.width != p.w || vp.height != p.h) {
                    p.w = vp.width;
                    p.h = vp.height;
                    this.sizePage(p);
                    this.thumbs?.[i - 1] && this.sizeThumb(this.thumbs[i - 1], p);
                }
            }
        }

        // the pages are drawn when they come near the screen
        watchPages() {
            this.seen = new IntersectionObserver(
                (entries) => {
                    for (const e of entries) {
                        const p = this.pages[Number(e.target.dataset.page) - 1];
                        p.visible = e.isIntersecting;
                        if (e.isIntersecting) {
                            this.draw(p);
                        }
                    }
                },
                { root: this.o.fit ? null : this.scroller, rootMargin: "50% 0px" },
            );
            this.pages.forEach((p) => this.seen.observe(p.el));
        }

        // --- zoom -------------------------------------------------------------------------

        // the room for the pages
        room() {
            const s = getComputedStyle(this.scroller);
            const w = this.scroller.clientWidth - parseFloat(s.paddingLeft) - parseFloat(s.paddingRight);
            let h = this.scroller.clientHeight - parseFloat(s.paddingTop) - parseFloat(s.paddingBottom);
            if (this.o.fit) {
                h = window.innerHeight - (this.toolbar?.offsetHeight || 0) - 32;
            }
            return { w: Math.max(w, 80), h: Math.max(h, 80) };
        }

        scaleFor(zoom) {
            const first = this.pages[this.current - 1] || this.pages[0];
            const room = this.room();
            if (zoom == "width") {
                return room.w / first.w;
            }
            if (zoom == "page") {
                return Math.min(room.w / first.w, room.h / first.h);
            }
            return (Number(zoom) / 100) * CSS_UNITS;
        }

        setZoom(zoom) {
            this.zoom = zoom;
            this.applyZoom();
        }

        stepZoom(dir) {
            const pct = Math.round((this.scale / CSS_UNITS) * 100);
            const next = dir > 0 ? STEPS.find((s) => s > pct + 1) : [...STEPS].reverse().find((s) => s < pct - 1);
            if (next) {
                this.setZoom(String(next));
            }
        }

        applyZoom() {
            if (!this.pages.length) {
                return;
            }
            // keep the place being read
            const p = this.pages[this.current - 1];
            const before = this.offsetIn(p);
            this.scale = Math.max(0.1, Math.min(this.scaleFor(this.zoom), 10 * CSS_UNITS));
            this.pages.forEach((page) => this.sizePage(page));
            this.updateZoomSelect();
            if (this.o.layout == "single") {
                this.show(this.current);
            } else if (before != null && !this.o.fit) {
                this.scroller.scrollTop = p.el.offsetTop - GAP + before * p.el.offsetHeight;
            }
            for (const page of this.pages) {
                if (page.visible || (this.o.layout == "single" && page.n == this.current)) {
                    this.draw(page);
                }
            }
        }

        offsetIn(p) {
            if (!p || this.o.fit || this.o.layout == "single" || !p.el.offsetHeight) {
                return null;
            }
            return (this.scroller.scrollTop - p.el.offsetTop + GAP) / p.el.offsetHeight;
        }

        sizePage(p) {
            p.el.style.width = Math.floor(p.w * this.scale) + "px";
            p.el.style.height = Math.floor(p.h * this.scale) + "px";
            p.el.style.setProperty("--scale-factor", this.scale);
        }

        updateZoomSelect() {
            const s = this.zoomSelect;
            if (!s) {
                return;
            }
            const pct = Math.round((this.scale / CSS_UNITS) * 100);
            const options = [
                ["width", "Fit width"],
                ["page", "Fit page"],
                ...[50, 75, 100, 125, 150, 200, 300, 400].map((v) => [String(v), v + "%"]),
            ];
            const fixed = this.zoom == "width" || this.zoom == "page";
            if (!fixed && !options.some(([v]) => v == this.zoom)) {
                options.push([String(this.zoom), this.zoom + "%"]);
                options.sort((a, b) => (isNaN(a[0]) || isNaN(b[0]) ? 0 : a[0] - b[0]));
            }
            s.textContent = "";
            for (const [v, label] of options) {
                const opt = el("option", null, { value: v });
                opt.textContent = label;
                s.append(opt);
            }
            s.value = String(this.zoom);
            if (this.zoomOut) {
                this.zoomOut.disabled = pct <= STEPS[0];
                this.zoomIn.disabled = pct >= STEPS[STEPS.length - 1];
            }
        }

        // --- drawing ----------------------------------------------------------------------

        async draw(p) {
            const dpr = Math.min(window.devicePixelRatio || 1, 3);
            const key = this.scale.toFixed(4) + "|" + dpr;
            if (p.key == key || this.destroyed) {
                return;
            }
            if (p.busy) {
                p.again = true; // drawn again when the current drawing ends
                return;
            }
            p.busy = true;
            p.key = key;
            try {
                p.page ||= await this.doc.getPage(p.n);
                const vp = p.page.getViewport({ scale: this.scale });
                const canvas = el("canvas", "gp-pdf-canvas", { "aria-hidden": "true" });
                canvas.width = Math.floor(vp.width * dpr);
                canvas.height = Math.floor(vp.height * dpr);
                p.task = p.page.render({
                    canvasContext: canvas.getContext("2d", { alpha: false }),
                    viewport: vp,
                    transform: dpr != 1 ? [dpr, 0, 0, dpr, 0, 0] : null,
                    annotationMode: this.pdfjs.AnnotationMode.ENABLE,
                });
                await p.task.promise;
                if (this.destroyed) {
                    return;
                }
                const layers = [canvas];
                if (this.o.text) {
                    const text = el("div", "textLayer");
                    layers.push(text);
                    const tl = new this.pdfjs.TextLayer({ textContentSource: p.page.streamTextContent({ includeMarkedContent: true, disableNormalization: true }), container: text, viewport: vp });
                    await tl.render().catch(() => {});
                }
                p.marks = el("div", "gp-pdf-marks", { "aria-hidden": "true" });
                layers.push(p.marks);
                if (this.o.links) {
                    const links = await this.links(p, vp);
                    if (links) {
                        layers.push(links);
                    }
                }
                p.vp = vp;
                p.el.replaceChildren(...layers);
                p.el.classList.add("gp-pdf-page--drawn");
                this.drawMarks(p);
                this.remember(p);
            } catch (err) {
                if (err?.name != "RenderingCancelledException") {
                    p.key = null;
                }
            } finally {
                p.busy = false;
                p.task = null;
            }
            if (p.again) {
                p.again = false;
                this.draw(p);
            }
        }

        // the pages drawn last are kept, the others are cleared (memory)
        remember(p) {
            this.drawn = this.drawn.filter((x) => x != p);
            this.drawn.push(p);
            while (this.drawn.length > MAX_DRAWN) {
                const old = this.drawn.find((x) => !x.visible && x.n != this.current);
                if (!old) {
                    break;
                }
                this.drawn = this.drawn.filter((x) => x != old);
                old.el.replaceChildren();
                old.el.classList.remove("gp-pdf-page--drawn");
                old.key = null;
                old.marks = null;
            }
        }

        // the links of a page: addresses open in a new tab, the links inside
        // the document go to their page
        async links(p, vp) {
            let annots;
            try {
                annots = await p.page.getAnnotations({ intent: "display" });
            } catch {
                return null;
            }
            const layer = el("div", "gp-pdf-links");
            for (const a of annots) {
                if (a.subtype != "Link" || !a.rect) {
                    continue;
                }
                const [x1, y1, x2, y2] = vp.convertToViewportRectangle(a.rect);
                const link = el("a", "gp-pdf-link");
                Object.assign(link.style, { left: Math.min(x1, x2) + "px", top: Math.min(y1, y2) + "px", width: Math.abs(x2 - x1) + "px", height: Math.abs(y2 - y1) + "px" });
                const url = a.url || a.unsafeUrl;
                if (url && safeLink(url)) {
                    link.href = url;
                    link.target = "_blank";
                    link.rel = "noopener noreferrer";
                    link.title = url;
                } else if (a.dest) {
                    link.href = "#";
                    link.title = "Go to the page";
                    link.addEventListener("click", (e) => {
                        e.preventDefault();
                        this.goToDest(a.dest);
                    });
                } else {
                    continue;
                }
                layer.append(link);
            }
            return layer.childElementCount ? layer : null;
        }

        async goToDest(dest) {
            try {
                const explicit = typeof dest == "string" ? await this.doc.getDestination(dest) : dest;
                const ref = explicit?.[0];
                const index = typeof ref == "object" ? await this.doc.getPageIndex(ref) : Number(ref);
                if (Number.isInteger(index)) {
                    this.goTo(index + 1);
                }
            } catch {}
        }

        // --- moving -----------------------------------------------------------------------

        goTo(n, instant) {
            if (!this.pages.length) {
                return;
            }
            n = Math.max(1, Math.min(n, this.pages.length));
            if (this.o.layout == "single") {
                this.show(n);
            } else {
                const p = this.pages[n - 1];
                if (this.o.fit) {
                    const top = p.el.getBoundingClientRect().top + window.scrollY - (this.toolbar?.offsetHeight || 0) - 8;
                    window.scrollTo({ top, behavior: instant ? "instant" : "auto" });
                } else {
                    this.scroller.scrollTop = p.el.offsetTop - GAP;
                }
                this.current = n;
            }
            this.updatePage();
        }

        // one page at a time: show page n
        show(n) {
            this.current = n;
            this.pages.forEach((p) => p.el.classList.toggle("gp-pdf-page--shown", p.n == n));
            this.scroller.scrollTop = 0;
            this.draw(this.pages[n - 1]);
            // the next one is ready
            if (this.pages[n]) {
                this.draw(this.pages[n]);
            }
            this.updatePage();
        }

        onScroll() {
            if (this.scrolling || this.o.layout == "single" || !this.pages.length) {
                return;
            }
            this.scrolling = true;
            requestAnimationFrame(() => {
                this.scrolling = false;
                // the page at the top third of the view
                const top = this.o.fit ? (this.toolbar?.offsetHeight || 0) : this.scroller.getBoundingClientRect().top;
                const line = top + (this.o.fit ? window.innerHeight - top : this.scroller.clientHeight) / 3;
                let current = 1;
                for (const p of this.pages) {
                    if (p.el.getBoundingClientRect().top <= line) {
                        current = p.n;
                    } else {
                        break;
                    }
                }
                if (current != this.current) {
                    this.current = current;
                    this.updatePage();
                }
            });
        }

        updatePage() {
            const n = this.pages.length;
            if (this.pageInput && document.activeElement != this.pageInput) {
                this.pageInput.value = String(this.current);
            }
            for (const b of [this.prevBtn, this.prevArrow]) {
                if (b) {
                    b.disabled = this.current <= 1;
                }
            }
            for (const b of [this.nextBtn, this.nextArrow]) {
                if (b) {
                    b.disabled = this.current >= n;
                }
            }
            if (this.thumbs) {
                this.thumbs.forEach((t, i) => t.setAttribute("aria-selected", String(i + 1 == this.current)));
                if (this.sidebarOpen) {
                    this.thumbs[this.current - 1]?.scrollIntoView({ block: "nearest" });
                }
            }
        }

        // --- thumbnails -------------------------------------------------------------------

        buildThumbs() {
            if (!this.sidebar) {
                return;
            }
            this.thumbs = this.pages.map((p) => {
                const t = el("button", "gp-pdf-thumb", { type: "button", role: "option", "aria-label": "Page " + p.n, "data-page": p.n });
                const frame = el("span", "gp-pdf-thumb-frame");
                const num = el("span", "gp-pdf-thumb-num");
                num.textContent = String(p.n);
                t.append(frame, num);
                this.sizeThumb(t, p);
                t.addEventListener("click", () => {
                    this.goTo(p.n);
                    if (this.narrow()) {
                        this.setSidebar(false);
                    }
                });
                this.sidebar.append(t);
                return t;
            });
            this.thumbSeen = new IntersectionObserver(
                (entries) => {
                    for (const e of entries) {
                        if (e.isIntersecting) {
                            this.thumbSeen.unobserve(e.target);
                            this.drawThumb(e.target, this.pages[Number(e.target.dataset.page) - 1]);
                        }
                    }
                },
                { root: this.sidebar, rootMargin: "200px 0px" },
            );
            this.thumbs.forEach((t) => this.thumbSeen.observe(t));
            this.updatePage();
        }

        sizeThumb(t, p) {
            const frame = t.firstChild;
            frame.style.aspectRatio = p.w + " / " + p.h;
        }

        async drawThumb(t, p) {
            try {
                p.page ||= await this.doc.getPage(p.n);
                const width = 112 * Math.min(window.devicePixelRatio || 1, 2);
                const vp = p.page.getViewport({ scale: width / p.page.getViewport({ scale: 1 }).width });
                const canvas = el("canvas", null, { "aria-hidden": "true" });
                canvas.width = Math.floor(vp.width);
                canvas.height = Math.floor(vp.height);
                await p.page.render({ canvasContext: canvas.getContext("2d", { alpha: false }), viewport: vp }).promise;
                t.firstChild.replaceChildren(canvas);
            } catch {}
        }

        // --- search -----------------------------------------------------------------------

        toggleSearch(open = this.searchBar?.hidden) {
            if (!this.searchBar) {
                return;
            }
            this.searchBar.hidden = !open;
            this.root.classList.toggle("gp-pdf--searching", !!open);
            this.searchBtn?.setAttribute("aria-expanded", String(!!open));
            if (open) {
                this.searchInput.focus();
                this.searchInput.select();
            } else {
                this.find("");
                this.searchBtn?.focus();
            }
        }

        async pageText(n) {
            if (!this.texts.has(n)) {
                this.texts.set(
                    n,
                    (async () => {
                        const page = (this.pages[n - 1].page ||= await this.doc.getPage(n));
                        const content = await page.getTextContent();
                        // the items joined (a space at the ends of lines), and where each one starts
                        let text = "";
                        const items = [];
                        for (const it of content.items) {
                            if (typeof it.str != "string") {
                                continue;
                            }
                            items.push({ start: text.length, it });
                            text += it.str + (it.hasEOL ? " " : "");
                        }
                        return { text, items, norm: normalize(text) };
                    })(),
                );
            }
            return this.texts.get(n);
        }

        async find(q) {
            q = q.trim();
            const s = this.search;
            const token = ++s.token;
            s.q = q;
            s.hits = [];
            s.index = -1;
            this.pages.forEach((p) => this.drawMarks(p));
            if (!q || !this.doc) {
                this.searchCount.textContent = "";
                return;
            }
            const needle = normalize(q).text;
            this.searchCount.textContent = "…";
            for (const p of this.pages) {
                const t = await this.pageText(p.n).catch(() => null);
                if (token != s.token) {
                    return;
                }
                if (!t) {
                    continue;
                }
                let at = t.norm.text.indexOf(needle);
                let found = false;
                while (at >= 0) {
                    s.hits.push({ page: p.n, start: t.norm.map[at], end: t.norm.map[at + needle.length] });
                    found = true;
                    at = t.norm.text.indexOf(needle, at + Math.max(needle.length, 1));
                }
                if (found) {
                    // the first result shows as soon as it's found
                    if (s.index < 0) {
                        const first = s.hits.findIndex((h) => h.page >= this.current);
                        if (first >= 0) {
                            s.index = first;
                            this.showHit();
                        }
                    }
                    this.drawMarks(p);
                }
                this.countHits();
            }
            if (s.index < 0 && s.hits.length) {
                s.index = 0;
                this.showHit();
            }
            this.countHits(true);
        }

        countHits(done) {
            const s = this.search;
            if (!s.hits.length) {
                this.searchCount.textContent = done ? "No results" : "…";
            } else {
                this.searchCount.textContent = (s.index + 1 || 1) + " / " + s.hits.length + (done ? "" : "…");
            }
            this.root.classList.toggle("gp-pdf--no-hits", !!done && !s.hits.length && !!s.q);
        }

        nextHit(dir) {
            const s = this.search;
            if (!s.hits.length) {
                return;
            }
            s.index = (s.index + dir + s.hits.length) % s.hits.length;
            this.showHit();
            this.countHits(true);
        }

        showHit() {
            const s = this.search;
            const hit = s.hits[s.index];
            if (!hit) {
                return;
            }
            if (this.current != hit.page || this.o.layout == "single") {
                this.goTo(hit.page);
            }
            this.pages.forEach((p) => p.marks && this.drawMarks(p));
            // the result in the view, once drawn
            const reveal = (tries) => {
                const mark = this.pages[hit.page - 1].marks?.querySelector(".gp-pdf-mark--active");
                if (mark) {
                    mark.scrollIntoView({ block: "center", inline: "nearest" });
                } else if (tries > 0) {
                    setTimeout(() => reveal(tries - 1), 120);
                }
            };
            reveal(20);
        }

        // the rectangles of the results on a drawn page (the parts of each item)
        async drawMarks(p) {
            if (!p.marks) {
                return;
            }
            p.marks.textContent = "";
            const s = this.search;
            const hits = s.hits.filter((h) => h.page == p.n);
            if (!hits.length || !p.vp) {
                return;
            }
            const t = await this.pageText(p.n).catch(() => null);
            if (!t || !p.marks) {
                return;
            }
            const vp = p.vp;
            const active = s.hits[s.index];
            for (const h of hits) {
                for (const { start, it } of t.items) {
                    const end = start + it.str.length;
                    if (end <= h.start || start >= h.end || !it.str.length) {
                        continue;
                    }
                    const from = Math.max(h.start, start) - start;
                    const to = Math.min(h.end, end) - start;
                    const tx = this.pdfjs.Util.transform(vp.transform, it.transform);
                    const fontHeight = Math.hypot(tx[2], tx[3]);
                    const width = it.width * vp.scale;
                    const mark = el("div", "gp-pdf-mark" + (h == active ? " gp-pdf-mark--active" : ""));
                    Object.assign(mark.style, {
                        left: tx[4] + (width * from) / it.str.length + "px",
                        top: tx[5] - fontHeight + "px",
                        width: Math.max((width * (to - from)) / it.str.length, 2) + "px",
                        height: fontHeight * 1.15 + "px",
                    });
                    p.marks.append(mark);
                }
            }
        }

        // --- download, print, full screen -------------------------------------------------

        download() {
            const name = this.name || "document.pdf";
            if (!this.data) {
                window.open(this.o.src, "_blank", "noopener");
                return;
            }
            const url = URL.createObjectURL(new Blob([this.data], { type: "application/pdf" }));
            const a = el("a", null, { href: url, download: name });
            document.body.append(a);
            a.click();
            a.remove();
            setTimeout(() => URL.revokeObjectURL(url), 30000);
        }

        // the pages as images in a part of the page printed alone
        async print() {
            if (!this.doc || this.printing) {
                return;
            }
            this.printing = true;
            const box = el("div", "gp-pdf-print", { "aria-hidden": "true" });
            const urls = [];
            const n = this.pages.length;
            try {
                for (const p of this.pages) {
                    this.showStatus("loading", `Preparing the printing… ${p.n} / ${n}`);
                    p.page ||= await this.doc.getPage(p.n);
                    const vp = p.page.getViewport({ scale: 150 / 72 });
                    const canvas = el("canvas");
                    canvas.width = Math.floor(vp.width);
                    canvas.height = Math.floor(vp.height);
                    await p.page.render({ canvasContext: canvas.getContext("2d", { alpha: false }), viewport: vp, intent: "print", annotationMode: this.pdfjs.AnnotationMode.ENABLE }).promise;
                    const blob = await new Promise((ok) => canvas.toBlob(ok, "image/png"));
                    const url = URL.createObjectURL(blob);
                    urls.push(url);
                    const img = el("img", null, { src: url, alt: "" });
                    box.append(img);
                }
                await Promise.all([...box.querySelectorAll("img")].map((img) => img.decode().catch(() => {})));
                document.body.append(box);
                document.documentElement.classList.add("gp-pdf-printing");
                this.showStatus("");
                const done = () => {
                    window.removeEventListener("afterprint", done);
                    document.documentElement.classList.remove("gp-pdf-printing");
                    box.remove();
                    urls.forEach((u) => URL.revokeObjectURL(u));
                    this.printing = false;
                };
                window.addEventListener("afterprint", done);
                window.print();
                // browsers without afterprint (print() then returns when done)
                setTimeout(() => this.printing && !matchMedia("print").matches && done(), 1000);
            } catch {
                this.printing = false;
                box.remove();
                urls.forEach((u) => URL.revokeObjectURL(u));
                this.showStatus("");
            }
        }

        toggleFullscreen(on) {
            const r = this.root;
            const native = r.requestFullscreen && document.fullscreenEnabled;
            if (native) {
                if (document.fullscreenElement == r || on === false) {
                    document.fullscreenElement && document.exitFullscreen();
                } else {
                    r.requestFullscreen().catch(() => {});
                }
                return;
            }
            // phones without full screen elements: the viewer covers the screen
            const expanded = on ?? !r.classList.contains("gp-pdf--expanded");
            r.classList.toggle("gp-pdf--expanded", expanded);
            document.documentElement.classList.toggle("gp-pdf-noscroll", expanded);
            if (this.fullBtn) {
                this.fullBtn.innerHTML = svg(expanded ? "shrink" : "expand");
                this.fullBtn.setAttribute("aria-label", expanded ? "Exit full screen" : "Full screen");
                this.fullBtn.setAttribute("title", expanded ? "Exit full screen" : "Full screen");
            }
        }

        destroy() {
            this.destroyed = true;
            viewers.delete(this);
            this.starter?.disconnect();
            this.seen?.disconnect();
            this.thumbSeen?.disconnect();
            this.resizer?.disconnect();
            document.removeEventListener("fullscreenchange", this.onFullscreen);
            if (this.onWindowScroll) {
                window.removeEventListener("scroll", this.onWindowScroll);
            }
            this.pages.forEach((p) => p.task?.cancel());
            this.task?.destroy();
            this.doc = null;
        }
    }

    function init(root) {
        // the viewers removed from the page (the builder draws them again)
        for (const v of [...viewers]) {
            if (!v.root.isConnected) {
                v.destroy();
            }
        }
        root = root || document;
        const all = [...(root.matches?.("[data-gp-pdf]") ? [root] : []), ...root.querySelectorAll("[data-gp-pdf]")];
        for (const r of all) {
            if (!r.gpPdf) {
                r.gpPdf = new Viewer(r);
            }
        }
    }

    window.gpPDF = { init };
    if (document.readyState == "loading") {
        document.addEventListener("DOMContentLoaded", () => init());
    } else {
        init();
    }
})();
