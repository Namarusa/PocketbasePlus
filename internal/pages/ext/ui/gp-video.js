// PocketBasePlus pages: videos loaded on click (internal/pages/video.go).
// The poster is a link to the video's page; a click puts the player in its
// place instead, started at once. Nothing is loaded from the video service
// before that click.
(() => {
    const sandbox = "allow-scripts allow-same-origin allow-popups allow-presentation";
    const allow = "accelerometer; autoplay; clipboard-write; encrypted-media; fullscreen; gyroscope; picture-in-picture; web-share";
    document.addEventListener("click", (e) => {
        const link = e.target instanceof Element ? e.target.closest("a.gp-video-play[data-gp-video-src]") : null;
        if (!link || e.ctrlKey || e.metaKey || e.shiftKey || e.button !== 0) {
            return; // a new tab: the video's page
        }
        const src = link.getAttribute("data-gp-video-src") || "";
        if (!src.startsWith("https://")) {
            return;
        }
        e.preventDefault();
        const frame = document.createElement("iframe");
        frame.src = src;
        frame.title = link.getAttribute("data-gp-video-title") || "Video";
        frame.allow = allow;
        frame.allowFullscreen = true;
        frame.referrerPolicy = "strict-origin-when-cross-origin";
        frame.setAttribute("sandbox", sandbox);
        link.replaceWith(frame);
        frame.focus();
    });
})();
