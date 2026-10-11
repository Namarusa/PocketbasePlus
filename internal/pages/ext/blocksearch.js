// Pages: a search box above the builder's blocks. Matches the block names and
// their category (every word of the query), opens the categories while
// searching and hides the empty ones.

/** Adds the search box to the blocks panel of the editor. */
export function addBlockSearch(ed) {
    let query = "";
    let box, input, empty, observer;

    const filter = (list) => {
        const words = query.toLowerCase().split(/\s+/).filter(Boolean);
        list.classList.toggle("gpp-searching", words.length > 0);
        let total = 0;
        const match = (blocks, category) => {
            let shown = 0;
            blocks.forEach((b) => {
                const text = (b.textContent + " " + (b.title || "") + " " + category).toLowerCase();
                b.hidden = !words.every((w) => text.includes(w));
                shown += b.hidden ? 0 : 1;
            });
            return shown;
        };
        list.querySelectorAll(".gjs-block-category").forEach((cat) => {
            const shown = match(cat.querySelectorAll(".gjs-block"), cat.querySelector(".gjs-title")?.textContent || "");
            cat.hidden = !shown;
            total += shown;
        });
        // blocks without a category
        total += match([...list.querySelectorAll(".gjs-block")].filter((b) => !b.closest(".gjs-block-category")), "");
        empty.hidden = !words.length || total > 0;
    };

    const mount = () => {
        const list = ed.getContainer()?.querySelector(".gjs-blocks-cs");
        if (!list || list.previousElementSibling === box) {
            return;
        }
        if (!box) {
            box = document.createElement("div");
            box.className = "gpp-block-search";
            const icon = document.createElement("i");
            icon.className = "ri-search-line";
            icon.ariaHidden = "true";
            input = document.createElement("input");
            input.type = "search";
            input.placeholder = "Search blocks";
            input.ariaLabel = "Search blocks";
            input.autocomplete = "off";
            input.spellcheck = false;
            input.addEventListener("input", () => {
                query = input.value;
                filter(list);
            });
            input.addEventListener("keydown", (e) => {
                e.stopPropagation(); // not the editor's shortcuts (delete, undo...)
                if (e.key == "Escape" && input.value) {
                    input.value = query = "";
                    filter(list);
                }
            });
            empty = document.createElement("p");
            empty.className = "gpp-block-search-empty";
            empty.textContent = "No blocks match.";
            empty.hidden = true;
            box.append(icon, input);
        }
        list.before(box);
        list.after(empty);
        // blocks change with the kit, the parts and the members: filter them again
        observer?.disconnect();
        observer = new MutationObserver(() => query && filter(list));
        observer.observe(list, { childList: true, subtree: true });
        filter(list);
    };

    let loaded = false;
    ed.on("load", () => {
        loaded = true;
        mount();
    });
    ed.on("command:run:open-blocks", () => {
        mount();
        if (loaded) {
            setTimeout(() => input?.focus({ preventScroll: true })); // opened by the Blocks button
        }
    });
}
