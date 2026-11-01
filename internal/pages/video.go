package pages

import (
	"html"
	"net/url"
	"path"
	"regexp"
	"strconv"
	"strings"

	"github.com/Namarusa/PocketbasePlus/internal/offline"
	xhtml "golang.org/x/net/html"
)

// Videos: a placeholder (data-pb-video) with the link of a hosted video
// (YouTube, Vimeo, Loom, Dailymotion, Wistia, Bunny Stream) or of a video
// file, drawn by the server: a poster with a play button that loads the
// player on click (the pages script gp-video.js; without it, the link opens
// the video's page), the player itself, or a <video> for the files.
const (
	attrVideo         = "data-pb-video"          // the link (a formula: {{record.video}})
	attrVideoRatio    = "data-pb-video-ratio"    // 16/9 (default) | 4/3 | 1/1 | 9/16 | 21/9 | auto (files)
	attrVideoPoster   = "data-pb-video-poster"   // an image shown before playing (YouTube: its thumbnail by default)
	attrVideoLoad     = "data-pb-video-load"     // click (default): the player loads on click | page: with the page
	attrVideoAutoplay = "data-pb-video-autoplay" // "true": starts muted with the page
	attrVideoLoop     = "data-pb-video-loop"     // "true"
	attrVideoControls = "data-pb-video-controls" // "false": hides the controls
	attrVideoStart    = "data-pb-video-start"    // the start: seconds or m:ss
	attrVideoTitle    = "data-pb-video-title"    // for screen readers and the poster
)

// videoRatios are the shapes of the frame (data-gp-ratio, gp-video.css).
var videoRatios = map[string]bool{"16/9": true, "4/3": true, "1/1": true, "9/16": true, "21/9": true, "auto": true}

// videoFrameSandbox matches the other embeds (sanitizeAttrs).
const videoFrameSandbox = "allow-scripts allow-same-origin allow-popups allow-presentation"
const videoFrameAllow = "accelerometer; autoplay; clipboard-write; encrypted-media; fullscreen; gyroscope; picture-in-picture; web-share"

// videoOptions are the settings of a video.
type videoOptions struct {
	autoplay, loop, hideControls bool
	start                        int // seconds
}

// hostedVideo is a video of a hosting service.
type hostedVideo struct {
	provider string // its name
	id       string
	embed    string // the player
	watch    string // its page (the link without the script)
	thumb    string // a poster (YouTube, Dailymotion)
}

var videoFileExt = map[string]bool{".mp4": true, ".webm": true, ".ogv": true, ".ogg": true, ".mov": true, ".m4v": true}

var (
	youtubeID  = regexp.MustCompile(`^[A-Za-z0-9_-]{6,20}$`)
	vimeoID    = regexp.MustCompile(`^\d{3,12}$`)
	vimeoHash  = regexp.MustCompile(`^[0-9a-f]{6,20}$`)
	plainID    = regexp.MustCompile(`^[A-Za-z0-9_-]{3,64}$`)
	bunnyLib   = regexp.MustCompile(`^\d{1,10}$`)
	bunnyVideo = regexp.MustCompile(`^[0-9a-f-]{36}$`)
	startTime  = regexp.MustCompile(`^(?:(\d+)h)?(?:(\d+)m)?(?:(\d+)s?)?$`)
)

// parseStart reads a start time: 90, 1:30, 1:02:03, 1m30s (YouTube's t=).
func parseStart(v string) int {
	v = strings.TrimSpace(strings.ToLower(v))
	if v == "" {
		return 0
	}
	if strings.Contains(v, ":") {
		total := 0
		for _, part := range strings.Split(v, ":") {
			n, err := strconv.Atoi(part)
			if err != nil || n < 0 {
				return 0
			}
			total = total*60 + n
		}
		return total
	}
	m := startTime.FindStringSubmatch(v)
	if m == nil {
		return 0
	}
	h, _ := strconv.Atoi(m[1])
	mi, _ := strconv.Atoi(m[2])
	s, _ := strconv.Atoi(m[3])
	return h*3600 + mi*60 + s
}

// parseHostedVideo recognizes the link of a hosted video (nil: not one).
// The start of the link (YouTube's t=, Vimeo's #t=) is used when the
// settings don't give one.
func parseHostedVideo(link string, o videoOptions) *hostedVideo {
	u, err := url.Parse(strings.TrimSpace(link))
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") {
		return nil
	}
	host := strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
	host = strings.TrimPrefix(host, "m.")
	parts := strings.FieldsFunc(u.Path, func(r rune) bool { return r == '/' })
	q := u.Query()
	at := func(i int) string {
		if i < len(parts) {
			return parts[i]
		}
		return ""
	}
	if o.start == 0 {
		o.start = parseStart(q.Get("t"))
		if o.start == 0 {
			o.start = parseStart(q.Get("start"))
		}
		if o.start == 0 && strings.HasPrefix(u.Fragment, "t=") {
			o.start = parseStart(strings.TrimPrefix(u.Fragment, "t="))
		}
	}
	params := url.Values{}
	set := func(cond bool, kv ...string) {
		if cond {
			for i := 0; i+1 < len(kv); i += 2 {
				params.Set(kv[i], kv[i+1])
			}
		}
	}
	with := func(base string) string {
		if len(params) == 0 {
			return base
		}
		return base + "?" + params.Encode()
	}

	switch {
	case host == "youtube.com" || host == "youtu.be" || host == "youtube-nocookie.com" || host == "music.youtube.com":
		id := ""
		switch {
		case host == "youtu.be":
			id = at(0)
		case at(0) == "watch":
			id = q.Get("v")
		case at(0) == "embed" || at(0) == "shorts" || at(0) == "live" || at(0) == "v":
			id = at(1)
		}
		if !youtubeID.MatchString(id) {
			return nil
		}
		params.Set("rel", "0")
		params.Set("playsinline", "1")
		set(o.start > 0, "start", strconv.Itoa(o.start))
		set(o.autoplay, "autoplay", "1", "mute", "1")
		set(o.loop, "loop", "1", "playlist", id)
		set(o.hideControls, "controls", "0")
		watch := "https://www.youtube.com/watch?v=" + id
		if o.start > 0 {
			watch += "&t=" + strconv.Itoa(o.start) + "s"
		}
		return &hostedVideo{provider: "YouTube", id: id, embed: with("https://www.youtube-nocookie.com/embed/" + id), watch: watch, thumb: "https://i.ytimg.com/vi/" + id + "/hqdefault.jpg"}

	case host == "vimeo.com" || host == "player.vimeo.com":
		id, hash := "", q.Get("h")
		for i, p := range parts {
			if vimeoID.MatchString(p) {
				id = p
				if h := at(i + 1); hash == "" && vimeoHash.MatchString(h) {
					hash = h // unlisted: vimeo.com/123/abcdef
				}
				break
			}
		}
		if id == "" {
			return nil
		}
		params.Set("dnt", "1")
		set(hash != "" && vimeoHash.MatchString(hash), "h", hash)
		set(o.autoplay, "autoplay", "1", "muted", "1")
		set(o.loop, "loop", "1")
		set(o.hideControls, "controls", "0")
		embed := with("https://player.vimeo.com/video/" + id)
		watch := "https://vimeo.com/" + id
		if hash != "" && vimeoHash.MatchString(hash) {
			watch += "/" + hash
		}
		if o.start > 0 {
			embed += "#t=" + strconv.Itoa(o.start) + "s"
			watch += "#t=" + strconv.Itoa(o.start) + "s"
		}
		return &hostedVideo{provider: "Vimeo", id: id, embed: embed, watch: watch}

	case host == "loom.com":
		id := ""
		if at(0) == "share" || at(0) == "embed" {
			id = at(1)
		}
		if !plainID.MatchString(id) {
			return nil
		}
		params.Set("hide_owner", "true")
		params.Set("hide_share", "true")
		set(o.autoplay, "autoplay", "1", "muted", "1")
		set(o.start > 0, "t", strconv.Itoa(o.start))
		return &hostedVideo{provider: "Loom", id: id, embed: with("https://www.loom.com/embed/" + id), watch: "https://www.loom.com/share/" + id}

	case host == "dailymotion.com" || host == "dai.ly" || host == "geo.dailymotion.com":
		id := ""
		switch {
		case host == "dai.ly":
			id = at(0)
		case at(0) == "video":
			id = at(1)
		case at(0) == "embed" && at(1) == "video":
			id = at(2)
		case host == "geo.dailymotion.com":
			id = q.Get("video")
		}
		id, _, _ = strings.Cut(id, "_") // video/x8abc12_the-title
		if !plainID.MatchString(id) {
			return nil
		}
		set(o.autoplay, "autoplay", "1", "mute", "1")
		set(o.loop, "loop", "1")
		set(o.hideControls, "controls", "0")
		set(o.start > 0, "start", strconv.Itoa(o.start))
		return &hostedVideo{provider: "Dailymotion", id: id, embed: with("https://www.dailymotion.com/embed/video/" + id), watch: "https://www.dailymotion.com/video/" + id,
			thumb: "https://www.dailymotion.com/thumbnail/video/" + id}

	case strings.HasSuffix(host, "wistia.com") || strings.HasSuffix(host, "wistia.net"):
		id := ""
		for i, p := range parts {
			if (p == "medias" || p == "iframe") && plainID.MatchString(at(i+1)) {
				id = at(i + 1)
				break
			}
		}
		if id == "" {
			id = q.Get("wvideo")
		}
		if !plainID.MatchString(id) {
			return nil
		}
		set(o.autoplay, "autoPlay", "true", "muted", "true")
		set(o.loop, "endVideoBehavior", "loop")
		set(o.hideControls, "controlsVisibleOnLoad", "false")
		set(o.start > 0, "time", strconv.Itoa(o.start))
		return &hostedVideo{provider: "Wistia", id: id, embed: with("https://fast.wistia.net/embed/iframe/" + id), watch: u.String()}

	case host == "iframe.mediadelivery.net" || host == "player.mediadelivery.net":
		if (at(0) != "play" && at(0) != "embed") || !bunnyLib.MatchString(at(1)) || !bunnyVideo.MatchString(at(2)) {
			return nil
		}
		set(o.autoplay, "autoplay", "true", "muted", "true")
		set(o.loop, "loop", "true")
		set(o.start > 0, "t", strconv.Itoa(o.start))
		base := "https://iframe.mediadelivery.net/embed/" + at(1) + "/" + at(2)
		return &hostedVideo{provider: "Bunny Stream", id: at(2), embed: with(base), watch: "https://iframe.mediadelivery.net/play/" + at(1) + "/" + at(2)}
	}
	return nil
}

// isVideoFile reports whether a link is a video file (by its extension):
// the Assets library, the files of the records, an https address.
func isVideoFile(link string) bool {
	u, err := url.Parse(link)
	if err != nil {
		return false
	}
	return videoFileExt[strings.ToLower(path.Ext(u.Path))]
}

func boolAttr(n *xhtml.Node, key string) bool {
	return strings.EqualFold(getAttr(n, key), "true") || getAttr(n, key) == "1"
}

// video draws a video placeholder.
func (r *renderer) video(n *xhtml.Node, s scope) {
	link := strings.TrimSpace(getAttr(n, attrVideo))
	if strings.Contains(link, "{{") {
		out, err := r.evalFormula(s, link)
		if err != nil {
			clearChildren(n)
			r.fail(n, "Video: the link formula: "+err.Error())
			return
		}
		link = strings.TrimSpace(out)
	}
	o := videoOptions{
		autoplay:     boolAttr(n, attrVideoAutoplay),
		loop:         boolAttr(n, attrVideoLoop),
		hideControls: strings.EqualFold(getAttr(n, attrVideoControls), "false"),
		start:        parseStart(getAttr(n, attrVideoStart)),
	}
	title := strings.TrimSpace(getAttr(n, attrVideoTitle))
	if title == "" {
		title = "Video"
	}
	ratio := strings.ReplaceAll(getAttr(n, attrVideoRatio), " ", "")
	if !videoRatios[ratio] {
		ratio = "16/9"
	}
	poster := strings.TrimSpace(getAttr(n, attrVideoPoster))
	if poster != "" && (!safeURL(poster, true) || strings.HasPrefix(poster, "//") || (offline.Enabled() && strings.Contains(poster, "://"))) {
		poster = ""
	}

	clearChildren(n)
	addClass(n, "gp-video")
	setAttr(n, "data-gp-ratio", ratio)
	r.videoCSS = true
	if link == "" {
		if !strings.Contains(getAttr(n, attrVideo), "{{") { // a record without a video: nothing
			r.fail(n, "Video: paste the link of the video in its settings")
		}
		return
	}

	// a file: the browser's player
	if isVideoFile(link) || strings.HasPrefix(link, "/media/") {
		u, err := url.Parse(link)
		if err != nil || !safeURL(link, false) || strings.HasPrefix(link, "//") || (u.Scheme != "" && u.Scheme != "https" && u.Scheme != "http") {
			r.fail(n, "Video: "+link+" isn't an http(s) address")
			return
		}
		if u.Scheme != "" && offline.Enabled() {
			r.fail(n, "Video: the offline mode is on, the pages can't show "+u.Host)
			return
		}
		src := link
		if o.start > 0 {
			src += "#t=" + strconv.Itoa(o.start)
		}
		var b strings.Builder
		b.WriteString(`<video src="` + html.EscapeString(src) + `" playsinline preload="metadata" title="` + html.EscapeString(title) + `"`)
		if !o.hideControls {
			b.WriteString(" controls")
		}
		if o.autoplay {
			b.WriteString(" autoplay muted")
		}
		if o.loop {
			b.WriteString(" loop")
		}
		if poster != "" {
			b.WriteString(` poster="` + html.EscapeString(poster) + `"`)
		}
		b.WriteString(`></video>`)
		appendHTML(n, b.String())
		return
	}

	v := parseHostedVideo(link, o)
	if v == nil {
		shown := "the link"
		if l := strings.ToLower(link); strings.HasPrefix(l, "https://") || strings.HasPrefix(l, "http://") {
			shown = link
		}
		r.fail(n, "Video: "+shown+" isn't a YouTube, Vimeo, Loom, Dailymotion, Wistia or Bunny Stream link, nor a video file (.mp4, .webm)")
		return
	}
	if offline.Enabled() {
		r.fail(n, "Video: the offline mode is on, the pages can't show "+v.provider+" videos")
		return
	}
	// with the page: the player itself (autoplay needs it)
	if o.autoplay || getAttr(n, attrVideoLoad) == "page" {
		appendHTML(n, `<iframe src="`+html.EscapeString(v.embed)+`" title="`+html.EscapeString(title)+`" allow="`+videoFrameAllow+`" allowfullscreen loading="lazy" referrerpolicy="strict-origin-when-cross-origin" sandbox="`+videoFrameSandbox+`"></iframe>`)
		return
	}
	// on click: a poster, the player loaded by gp-video.js (it starts at once)
	play, frag, _ := strings.Cut(v.embed, "#")
	sep := "?"
	if strings.Contains(play, "?") {
		sep = "&"
	}
	switch v.provider {
	case "Wistia":
		play += sep + "autoPlay=true"
	case "Bunny Stream":
		play += sep + "autoplay=true"
	default:
		play += sep + "autoplay=1"
	}
	if frag != "" {
		play += "#" + frag
	}
	if poster == "" {
		poster = v.thumb
	}
	var b strings.Builder
	b.WriteString(`<a class="gp-video-play" href="` + html.EscapeString(v.watch) + `" target="_blank" rel="noopener" data-gp-video-src="` + html.EscapeString(play) + `" data-gp-video-title="` + html.EscapeString(title) + `" aria-label="Play: ` + html.EscapeString(title) + `">`)
	if poster != "" {
		b.WriteString(`<img class="gp-video-poster" src="` + html.EscapeString(poster) + `" alt="" loading="lazy" referrerpolicy="no-referrer">`)
	}
	b.WriteString(`<span class="gp-video-icon" aria-hidden="true"></span>`)
	if t := getAttr(n, attrVideoTitle); strings.TrimSpace(t) != "" {
		b.WriteString(`<span class="gp-video-label">` + html.EscapeString(strings.TrimSpace(t)) + `</span>`)
	} else if poster == "" {
		b.WriteString(`<span class="gp-video-label">` + html.EscapeString(v.provider) + `</span>`)
	}
	b.WriteString(`</a>`)
	appendHTML(n, b.String())
	r.videoScript = true
}
