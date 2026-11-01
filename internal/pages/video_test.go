package pages

import (
	"strings"
	"testing"

	"github.com/Namarusa/PocketbasePlus/internal/testutil"
)

func TestParseHostedVideo(t *testing.T) {
	cases := []struct {
		link, provider, embed, watch string
		o                            videoOptions
	}{
		{"https://www.youtube.com/watch?v=dQw4w9WgXcQ", "YouTube", "https://www.youtube-nocookie.com/embed/dQw4w9WgXcQ?playsinline=1&rel=0", "https://www.youtube.com/watch?v=dQw4w9WgXcQ", videoOptions{}},
		{"https://youtu.be/dQw4w9WgXcQ?t=90", "YouTube", "https://www.youtube-nocookie.com/embed/dQw4w9WgXcQ?playsinline=1&rel=0&start=90", "https://www.youtube.com/watch?v=dQw4w9WgXcQ&t=90s", videoOptions{}},
		{"https://youtube.com/shorts/dQw4w9WgXcQ", "YouTube", "https://www.youtube-nocookie.com/embed/dQw4w9WgXcQ?autoplay=1&controls=0&loop=1&mute=1&playlist=dQw4w9WgXcQ&playsinline=1&rel=0", "", videoOptions{autoplay: true, loop: true, hideControls: true}},
		{"https://m.youtube.com/watch?v=dQw4w9WgXcQ&t=1m5s", "YouTube", "https://www.youtube-nocookie.com/embed/dQw4w9WgXcQ?playsinline=1&rel=0&start=65", "", videoOptions{}},
		{"https://vimeo.com/76979871", "Vimeo", "https://player.vimeo.com/video/76979871?dnt=1", "https://vimeo.com/76979871", videoOptions{}},
		{"https://vimeo.com/76979871/abcdef1234", "Vimeo", "https://player.vimeo.com/video/76979871?dnt=1&h=abcdef1234#t=30s", "https://vimeo.com/76979871/abcdef1234#t=30s", videoOptions{start: 30}},
		{"https://player.vimeo.com/video/76979871?h=abcdef1234", "Vimeo", "https://player.vimeo.com/video/76979871?autoplay=1&dnt=1&h=abcdef1234&muted=1", "", videoOptions{autoplay: true}},
		{"https://www.loom.com/share/0281766fa2d04bb788eaf19e65135184", "Loom", "https://www.loom.com/embed/0281766fa2d04bb788eaf19e65135184?hide_owner=true&hide_share=true", "", videoOptions{}},
		{"https://www.dailymotion.com/video/x8abc12_some-title", "Dailymotion", "https://www.dailymotion.com/embed/video/x8abc12", "", videoOptions{}},
		{"https://dai.ly/x8abc12", "Dailymotion", "https://www.dailymotion.com/embed/video/x8abc12?start=10", "", videoOptions{start: 10}},
		{"https://acme.wistia.com/medias/abc123xyz", "Wistia", "https://fast.wistia.net/embed/iframe/abc123xyz", "", videoOptions{}},
		{"https://iframe.mediadelivery.net/play/12345/0f8b2c1e-1d2a-4b5c-9d8e-7f6a5b4c3d2e", "Bunny Stream", "https://iframe.mediadelivery.net/embed/12345/0f8b2c1e-1d2a-4b5c-9d8e-7f6a5b4c3d2e?loop=true", "", videoOptions{loop: true}},
	}
	for _, c := range cases {
		v := parseHostedVideo(c.link, c.o)
		if v == nil {
			t.Errorf("%s: not recognized", c.link)
			continue
		}
		if v.provider != c.provider || v.embed != c.embed || (c.watch != "" && v.watch != c.watch) {
			t.Errorf("%s:\n got %s %s %s\nwant %s %s %s", c.link, v.provider, v.embed, v.watch, c.provider, c.embed, c.watch)
		}
	}
	for _, bad := range []string{
		"", "javascript:alert(1)", "https://example.com/watch?v=dQw4w9WgXcQ", "https://www.youtube.com/watch?v=<script>",
		"https://vimeo.com/channels/staff", "ftp://youtu.be/dQw4w9WgXcQ", "https://www.youtube.com/watch?v=a\"b",
		"https://iframe.mediadelivery.net/play/12345/not-a-guid",
	} {
		if v := parseHostedVideo(bad, videoOptions{}); v != nil {
			t.Errorf("%q recognized as %+v", bad, v)
		}
	}
	for in, want := range map[string]int{"90": 90, "1:30": 90, "1:02:03": 3723, "1m30s": 90, "2h": 7200, "45s": 45, "x": 0, "-5": 0, "": 0} {
		if got := parseStart(in); got != want {
			t.Errorf("parseStart(%q) = %d, want %d", in, got, want)
		}
	}
}

func TestVideoBlock(t *testing.T) {
	f := setup(t)
	view := func(src string, s scope) (string, needs) {
		t.Helper()
		out, n, err := render(t.Context(), f.app, pageView{HTML: src, Scope: s}, nil, true)
		if err != nil {
			t.Fatal(err)
		}
		return out, n
	}
	has := func(out string, want ...string) {
		t.Helper()
		for _, w := range want {
			if !strings.Contains(out, w) {
				t.Errorf("missing %q in\n%.3000s", w, out)
			}
		}
	}
	hasNot := func(out string, bad ...string) {
		t.Helper()
		for _, w := range bad {
			if strings.Contains(out, w) {
				t.Errorf("unexpected %q in\n%.3000s", w, out)
			}
		}
	}

	// on click (default): a poster linking to the video, the script, no player yet
	out, n := view(`<div id="v" class="mine" data-pb-video="https://youtu.be/dQw4w9WgXcQ" data-pb-video-title="Our launch"><p>old</p></div>`, scope{})
	if !n.script {
		t.Error("the click needs the script")
	}
	has(out, `id="v"`, `class="mine gp-video"`, `data-gp-ratio="16/9"`, `/p/_static/gp-video-`,
		`class="gp-video-play"`, `href="https://www.youtube.com/watch?v=dQw4w9WgXcQ" rel="noopener" target="_blank"`,
		`data-gp-video-src="https://www.youtube-nocookie.com/embed/dQw4w9WgXcQ?playsinline=1&amp;rel=0&amp;autoplay=1"`,
		`aria-label="Play: Our launch"`, `src="https://i.ytimg.com/vi/dQw4w9WgXcQ/hqdefault.jpg"`, `<span class="gp-video-label">Our launch</span>`)
	hasNot(out, "<iframe", "data-pb-", "old")

	// with the page, a ratio, a poster: the player, sandboxed
	out, _ = view(`<div data-pb-video="https://vimeo.com/76979871" data-pb-video-load="page" data-pb-video-ratio="9/16" data-pb-video-start="1:00"></div>`, scope{})
	has(out, `data-gp-ratio="9/16"`, `<iframe src="https://player.vimeo.com/video/76979871?dnt=1#t=60s"`, `allowfullscreen`, `loading="lazy"`,
		`sandbox="allow-scripts allow-same-origin allow-popups allow-presentation"`, `title="Video"`)
	hasNot(out, "gp-video-play", `<script src="/p/_static/gp-video-`) // no script

	// autoplay: muted, with the page
	out, _ = view(`<div data-pb-video="https://www.youtube.com/watch?v=dQw4w9WgXcQ" data-pb-video-autoplay="true" data-pb-video-loop="true" data-pb-video-controls="false"></div>`, scope{})
	has(out, `autoplay=1`, `mute=1`, `loop=1`, `playlist=dQw4w9WgXcQ`, `controls=0`, "<iframe")

	// a file of the Assets library: the browser's player
	out, n = view(`<div data-pb-video="/media/abcdefghijklmno/clip.mp4" data-pb-video-poster="/media/bbbbbbbbbbbbbbb/cover.jpg" data-pb-video-loop="true" data-pb-video-ratio="auto"></div>`, scope{})
	has(out, `<video src="/media/abcdefghijklmno/clip.mp4" playsinline="" preload="metadata" title="Video" controls="" loop="" poster="/media/bbbbbbbbbbbbbbb/cover.jpg"></video>`, `data-gp-ratio="auto"`)
	if n.script {
		t.Error("a file needs no script")
	}

	// unsafe or unknown: an error in the previews, nothing else
	for _, src := range []string{
		`<div data-pb-video="javascript:alert(1)"></div>`,
		`<div data-pb-video="https://example.com/page"></div>`,
		`<div data-pb-video="https://youtu.be/dQw4w9WgXcQ" data-pb-video-poster="javascript:alert(1)" data-pb-video-ratio="1/0;color:red"></div>`,
	} {
		out, _ := view(src, scope{})
		hasNot(out, "javascript:", "1/0", "color:red")
	}
	out, _ = view(`<div data-pb-video="https://example.com/page"></div>`, scope{})
	has(out, "isn&#39;t a YouTube, Vimeo")

	// a formula: the video of the record; nothing for a record without one
	f.chair.Set("site", "https://vimeo.com/76979871")
	if err := f.app.Save(f.chair); err != nil {
		t.Fatal(err)
	}
	out, _ = view(`<div data-pb-video="{{record.site}}"></div>`, scope{col: f.products, rec: f.chair})
	has(out, `href="https://vimeo.com/76979871"`, `<span class="gp-video-label">Vimeo</span>`)
	desk := testutil.Create(t, f.app, "products", map[string]any{"name": "Desk"})
	out, _ = view(`<div data-pb-video="{{record.site}}"></div>`, scope{col: f.products, rec: desk})
	hasNot(out, "gp-video-play", "⚠")
}
