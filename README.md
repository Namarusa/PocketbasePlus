# PocketBasePlus

An extended [PocketBase](https://github.com/pocketbase/pocketbase) (used as a Go framework) with features added **inside the native PocketBase dashboard** (`/_/`):

- **Dashboards**: panels of metrics, charts and lists built from any collection (like Directus Insights), with a
  drag and resize grid, fullscreen, auto refresh, suggestions by field type, and read-only sharing with roles.
- **Permissions**: roles and a roles × actions matrix (None / Own / All), compiled into native PocketBase API rules.
- **Emails**: HTML templates with live preview, manual sends, and automations (record events, delays, cron
  schedules) for any collection, without per-collection code.
- **Flows**: a general automation engine (like Directus Flows / Budibase automations) with one trigger, a list of
  steps, branches and loops, and a dynamic content picker on every input.
- **AI**: providers and models in Settings → AI (TypeSafe Jev "System One" models; "Standard" models from OpenRouter,
  Anthropic, OpenAI, Google Gemini, DeepSeek, Kimi and GLM), used
  by the **AI decision** and **AI generate** flow steps, and an **AI assistant** chat that reads your data and prepares
  dashboards, email templates and campaigns (applied by you).
- **Pages** (optional module): a drag and drop page builder whose blocks show your collections (lists, fields, record
  pages, forms), published on your site and rendered by the server. See [Pages](#pages), and how to remove it.

They appear as **Dashboards**, **Permissions**, **Emails** and **Flows** tabs in the dashboard's top navigation. They're built with PocketBase's
UI extensions API and its own UI toolkit, so the dashboard isn't forked and stays upgradable.

## Why extend PocketBase

[PocketBase](https://pocketbase.io) ([source](https://github.com/pocketbase/pocketbase), MIT, by Gani Georgiev and
contributors) is an open source backend in one file: an embedded SQLite database with realtime subscriptions, auth,
file storage, API rules and an admin dashboard. It stays deliberately small, and leaves dashboards, permissions by role,
emails, automations, AI or a page builder to the apps built on it. PocketBasePlus adds them for the teams that want an
internal tool (in the spirit of Directus, Budibase or Appsmith) and keep PocketBase's strengths:

- **One binary, no services**: still a single executable with SQLite, the new features embedded, nothing to install,
  and it [works offline](#offline-deployment) (no CDN).
- **Extended, not forked**: PocketBase is used [as a Go framework](https://pocketbase.io/docs/go-overview/), its
  documented way to be extended (hooks, routes, migrations), and the new pages use the dashboard's UI extensions API.
  PocketBase's code and dashboard are unchanged, so it can be upgraded by bumping its version in `go.mod`.
- **PocketBase's own building blocks**: the roles compile into native API rules, the page visitors sign in with
  PocketBase auth, the settings and data are regular collections, and the REST API, SDKs and realtime keep working
  for your other apps.
- **Removable modules**: the most experimental features (Pages, saved APIs) are self-contained packages, removed by
  deleting one line of `main.go` (see [Removing the Pages module](#removing-the-pages-module)).

PocketBasePlus is an independent project, not affiliated with or endorsed by PocketBase. PocketBase is still in active
development (before v1.0), and so is this project: back up `pb_data` before upgrading.

## Run

Download the binary of your system from the [releases](../../releases) (Linux 64-bit and ARM, including Raspberry Pi,
Windows, macOS Intel and Apple Silicon; no dependencies), then:

```sh
./pocketbaseplus serve     # http://127.0.0.1:8090/_/ (data in ./pb_data next to the binary)
```

On macOS, the binaries aren't signed: allow it once with `xattr -d com.apple.quarantine ./pocketbaseplus` (or right-click →
Open). On Linux and macOS, `chmod +x pocketbaseplus` if needed.

With Docker: `docker run -d -p 8090:8090 -v pocketbaseplus_data:/pb_data ghcr.io/namarusa/pocketbaseplus`, see
[Docker](#docker).

From the sources (Go 1.27+, no C compiler needed):

```sh
make dev     # http://127.0.0.1:8090/_/ (migrations apply automatically)
make build   # ./pocketbaseplus: single binary, everything embedded
make build-windows   # pocketbaseplus.exe: the same for Windows 64-bit
make release VERSION=v0.1.0   # dist/: Linux (amd64, arm64, armv7), Windows, macOS archives + SHA256SUMS
make test    # Go tests
```

First run: open the installer link printed in the console (or run `./pocketbaseplus superuser upsert EMAIL PASS`), then set
up SMTP in Settings → Mail settings.

## Docker

One image, `ghcr.io/namarusa/pocketbaseplus`, for **linux/amd64**, **linux/arm64** and **linux/arm/v7**: Docker picks the
right one on Linux servers, Docker Desktop (Windows, macOS Intel and Apple Silicon), Synology, QNAP, Unraid, TrueNAS
SCALE, Raspberry Pi (32 and 64-bit), Portainer, Coolify, CapRover or Kubernetes. Tags: `latest`, `0.1.0`, `0.1`.

```sh
docker run -d --name pocketbaseplus -p 8090:8090 \
  -v pocketbaseplus_data:/pb_data \
  -e POCKETBASEPLUS_ADMIN_EMAIL=admin@example.com \
  -e POCKETBASEPLUS_ADMIN_PASSWORD='a-long-password' \
  ghcr.io/namarusa/pocketbaseplus:latest
```

then open `http://localhost:8090/_/`. With Compose: `docker compose up -d` with the
[docker-compose.yml](docker-compose.yml) of the repository (`docker build -t pocketbaseplus .` builds the image from
the sources).

- **Data**: everything (database, uploads, backups) is in the `/pb_data` volume: back it up, and keep it when you
  update the image (`docker compose pull && docker compose up -d`).
- **Folders of the host** (bind mounts, NAS shares): the app runs as uid 65532, not root. Either give it the folder
  (`sudo chown -R 65532:65532 ./pb_data`) or run the container as the folder's owner (`--user 1000:1000`, or `user:`
  in Compose; on a Synology, the uid/gid of your user, shown by `id` over SSH).
- **Health**: the image has a health check (`pocketbaseplus health`), shown as *healthy* by Docker, Portainer and the
  NAS app managers.
- **HTTPS**: put the container behind your reverse proxy (Caddy, Traefik, Nginx Proxy Manager, the NAS one) and set
  Settings → Application → *User IP proxy headers* (see [Security](#security)).
- **Commands**: `docker exec pocketbaseplus pocketbaseplus --dir /pb_data superuser upsert EMAIL PASS` (or
  `pages uninstall`, `migrate`...).

### Environment variables

All are optional, and work the same way without Docker (`POCKETBASEPLUS_HTTP` is then the default of `serve --http`).

| Variable | Default | Purpose |
| --- | --- | --- |
| `POCKETBASEPLUS_ADMIN_EMAIL`, `POCKETBASEPLUS_ADMIN_PASSWORD` | | Creates the first superuser when there is none yet (no installer link needed). An existing account is never changed: remove them after the first start. |
| `POCKETBASEPLUS_ENCRYPTION_KEY` | | 32 characters (e.g. `openssl rand -hex 16`) encrypting the stored settings (SMTP password, S3 and OAuth2 secrets). Keep it safe: without it, the app can't start on that data anymore. Existing settings are encrypted at their next save. |
| `POCKETBASEPLUS_HTTP` | `0.0.0.0:8090` in the image, `127.0.0.1:8090` otherwise | Address and port the server listens on. |
| `TZ` | `UTC` | Time zone of the server (logs, dates without a zone). Time zones are embedded in the binary. |
| `POCKETBASEPLUS_FLOWS_ALLOW_PRIVATE_HTTP` | off | `1`: flows and API calls may reach private addresses (services of your network, other containers). |
| `POCKETBASEPLUS_OFFLINE` | off | `1`: published pages load nothing from the internet (see [Offline deployment](#offline-deployment)). |
| `POCKETBASEPLUS_MAP_TILES` | OpenStreetMap | A tile server of your network for the Map layout. |
| `POCKETBASEPLUS_REVISIONS_RETENTION_DAYS` | `180` | How long the record history is kept. |
| `POCKETBASEPLUS_UNSUBSCRIBE_SECRET` | derived from the app's secret | Fixed key of the email unsubscribe links. |

Everything else (SMTP, S3, backups, AI providers, OAuth2) is set in the dashboard and stored in the database.

## Security

- **HTTPS**: serve the app over HTTPS, or passwords, tokens and the page sessions cross the network in clear text.
  With a domain name, `./pocketbaseplus serve yourdomain.com` gets a Let's Encrypt certificate; on a LAN, put a reverse proxy
  (e.g. Caddy with its internal CA) in front and set Settings → Application → *User IP proxy headers* so the app
  trusts its `X-Forwarded-*` headers. The page session cookies are then `Secure` with the `__Host-` prefix (other
  services of the same host can't set them).
- **Rate limiting** of PocketBase is turned on (migration `1761400000_rate_limits.go`, PocketBase's default rules:
  sign in, sign up and every API call per client). The page sign in also stops after 10 failures per IP and 20 per
  account in 10 minutes.
- **Roles**: only superusers can change the roles of a user (sign up and self updates with roles are refused, OAuth2
  sign up too), so the default rules of the users collection can't be used to gain permissions.
- **Run logs** never store passwords, tokens, API keys or cookies (values of fields named like `password`, `secret`,
  `token`, `api_key`, `authorization`...): they show `[hidden]`.
- **Page actions** never send the flow output back (only the status, message and next page). The record of an action
  button is signed for its flow and expires after 7 days. An action sends at most 5 emails, and a visitor 10 per hour.
- **Values in flow HTTP requests and API calls** are escaped for their place: path segments and query values of the
  URL, string content or JSON values of a JSON body (`{{json ...}}` and other helpers are inserted as written), XML
  text, form fields. Header values can't hold line breaks.
- **API secrets** (tokens, passwords, API keys of [Flows → APIs](#apis)) are never returned by the API, even to
  superusers (only their last 4 characters), never logged, and removed from error messages. A page action makes at
  most 30 HTTP requests or API calls per visitor in 10 minutes.
- **Rich text of the records** shown on pages keeps formatting only (allow list): no forms, buttons, styles sheets,
  frames, media players, or positioning styles; user links get `rel="nofollow ugc"`.
- **Draft pages**: their images are only served to the page builder and to their preview links.
- **Files**: on Linux and macOS, the database, backups and uploads are only readable by the account running the
  server.

## Layout

```
internal/rbac/        roles matrix → API rules compiler, hooks, /api/rbac/* routes
internal/mail/        templates rendering, queue + worker, automations, schedules, /api/emails/* routes
internal/flows/       flows engine: triggers, steps, sandboxed JS, run logs, /api/flows/* routes
internal/ai/          AI providers (TypeSafe, OpenRouter), decisions (yes/no, choice, score), generation, tool calling
internal/assistant/   AI assistant: chat, tools reading the app, proposed changes applied by the user, /api/assistant/*
internal/dashboards/  panel queries (aggregates, groups, time series, lists), /api/dashboards/* routes, /dashboards viewer
internal/activity/    revisions of every record change (author, diff, revert), comments, notifications
internal/actor/       "who made the change" carried through contexts (users, superusers, flows)
internal/records/     export (CSV/JSON/XLSX), import with dry run, bulk edit, saved views
internal/forms/       public forms: /f/{slug} page, /f/{slug}/embed iframe, /api/forms/* (schema, submit)
internal/publicpage/  HTML shell and CSP of the public pages (dashboards viewer, forms)
internal/session/     visitors signed in on the pages (HttpOnly cookie), flash messages, /api/session/logout
internal/pages/       optional Pages module, self-contained: its migration, page routes, server rendering, and its own
                      dashboard extension "pocketbaseplus-pages" (ext/, with GrapesJS 0.23.6 BSD-3, Basecoat UI 1.0.2 MIT,
                      Geist 1.7.2 fonts OFL, Lucide 1.48.0 icons ISC: licences in ext/vendor/)
internal/offline/     offline deployment settings (POCKETBASEPLUS_OFFLINE, POCKETBASEPLUS_MAP_TILES)
internal/deploy/      container settings from the environment (listen address, encryption key, first superuser) and the health command
internal/formula/     the functions of the templates, flows and page formulas, with their catalog (/api/gp/functions)
internal/labels/      readable names of records (relation values)
internal/internalcols/ the module collections, which never trigger automations or get generated rules
internal/dashboard/   dashboard extension (registered as core.UIExtension "pocketbaseplus")
  ext/main.js         entry: header tabs + routes (runs before the dashboard router starts)
  ext/*.js            pages, loaded on demand as ES modules from /_/extensions/pocketbaseplus/
  ext/style.css       styles on top of the dashboard CSS variables
  ext/vendor/         Apache ECharts 6 (Apache-2.0, see ECHARTS-LICENSE) and Leaflet 1.9 (BSD-2, see
                      leaflet/LEAFLET-LICENSE), served locally
migrations/           collections: roles, users.roles, permissions, rbac_collections, email_*, flows, flow_runs,
                      dashboards, dashboard_panels, collection_groups, revisions, comments, notifications,
                      saved_views, forms, ai_providers, ai_models, ai_settings, ai_conversations,
                      email_unsubscribes (and email_templates.marketing)
                      (all flagged as system collections)
```

The extension is plain JavaScript using the dashboard globals (`t`, `store`, `watch` from
[Shablon](https://github.com/ganigeorgiev/shablon), plus `app.routes`, `app.store`, `app.components`, `app.modals`,
`app.toasts`, `app.pb`). There's no build step; restart the server after editing it, since the files are embedded.

> PocketBase marks its UI extensions API as undocumented until a stable release, so a PocketBase upgrade may need
> small adjustments in `internal/dashboard/ext/`. Check the dashboard after upgrading.

## Permissions

- `roles` collection; `users.roles` is a multi relation (permissions are the union of a user's roles).
- Permissions → *collection* saves rows in `permissions` (role, collection, action, scope) and `rbac_collections`
  (managed flag, owner field). Hooks compile them into the collection's list/view/create/update/delete rules:
  - All: `@request.auth.roles.id ?= "ROLE"`
  - Own: `(@request.auth.roles.id ?= "ROLE" && owner = @request.auth.id)` (create checks `@request.body.owner`;
    update also refuses changing the owner to someone else)
  - No grant: `null` (superusers only)
  - Custom: `(@request.auth.roles.id ?= "ROLE" && (FILTER))`, any PocketBase filter (`status = "published"`,
    `@request.auth.*`, `@collection.*`...), validated on save.
- Each granted cell has options (the sliders button):
  - **Readable fields** (list/view): the other fields are hidden from the responses (lists, views, expands,
    realtime) and can't be used in filters, sorts or expands.
  - **Writable fields** (create/update): changing another field is refused with a 403 (sending an unchanged value
    back is accepted).
  - **Presets** (create): default values of the fields not sent (`@request.auth.id` and `@now` are resolved). They're
    applied after the create rule check, so an "Own" create scope still needs the owner in the request.
  - Field limits apply to managed collections and never to superusers. With several roles, a user gets the union of
    their grants.
- Only **managed** collections are rewritten. Their rules edited in the collection's API Rules tab are overwritten on
  the next change or restart.

## Emails

- Templates: subject/text use Go `text/template`; the HTML body uses `html/template` (values are escaped).
  Data: `.record`, `.records` (digests), `.recipient.email`, `.app.name`, `.app.url`, and every
  [function](#functions) (the **Functions** button lists them).
- Automations:
  - **When a record changes**: create/update/delete on any collection, with an optional filter.
  - **Some time after**: the same, delayed, optionally rechecking the filter and re-rendering at send time.
  - **On a schedule**: cron (UTC). With a recipient field it sends one email per matching record; without one it
    sends a single digest of `.records` to the static recipients.
- Recipients: a record field (`email`) or one relation hop (`owner.email`), plus static addresses.
- **Marketing emails** (template option): an unsubscribe link is added at the end (or place `{{.unsubscribe_url}}`
  yourself), with the `List-Unsubscribe` and one-click headers (RFC 8058). The link opens `/email/unsubscribe` (a
  confirmation page, so that link scanners don't unsubscribe anybody) and adds the address to `email_unsubscribes`
  (Emails → Unsubscribes). Marketing emails are never sent to these addresses, even when queued before. Set the
  Application URL in the settings (the links need it). Transactional templates aren't affected. The links are signed
  with a key derived from the superusers token secret, or `POCKETBASEPLUS_UNSUBSCRIBE_SECRET`.
- **Send**: "One email per address" skips duplicate addresses (e.g. customers with several orders), and **Count
  recipients** shows how many emails would be queued (duplicates, unsubscribed and records without address skipped).
- `email_messages` is the queue and the log. Messages are sent right away and by a worker every minute, with up to
  3 attempts. You can retry or cancel from Emails → Log.
- The preview iframe is sandboxed and follows the dashboard CSP, so remote images in emails don't show in the
  preview (they do in the real email).

## Flows

A flow has **one trigger** and a **list of steps**. Every step input accepts dynamic content (the ⚡ button):
`{{.trigger.record.title}}`, `{{.steps.<key>.<field>}}`, `{{.item}}` inside loops, `{{.last}}`, `{{.now}}`.
An input that holds exactly one variable keeps its type (a list stays a list), and so does a single function giving
a list or an object (`{{where "stock" 0 .steps.api.body}}` can be the items of a *For each*); anything else is
rendered as text. The ⚡ picker has a **Functions** tab with all the [functions](#functions): a picked function is
inserted with its first argument selected, and a value picked next replaces it.

- **Triggers:**
  - **Record event**: create/update/delete on a collection.
    - *After save* runs in the background, with an optional filter.
    - *Before save* runs inside the save (15s max) and can change the record ("Update triggering record") or
      refuse it ("Stop / reject").
  - **Manual**: a "Run flow" button in the record popup of the selected collection, or Test run.
  - **Webhook**: `POST|GET /api/flows/webhook/{id}?token=...` (or an `X-Flow-Token` header; the token is
    removed from the logged request URL). With "wait" it
    responds with the last step output.
  - **Schedule**: cron (UTC).
  - **Another flow**: called by a "Run another flow" step.
  - **Page action**: a button or form of the [pages](#members-sign-in-and-page-actions) (`POST /api/flows/action/{id}`,
    same-site requests only). Who can run it: *Public*, *Signed-in users* or some *Roles*. The flow gets the form
    fields (`trigger.form.<name>`), the signed-in visitor (`trigger.user`, null for guests), the record of the button
    in a list or on a record page (`trigger.record`, signed by the server so visitors can't swap it), the page
    (`trigger.page`) and `?next` (`trigger.next`). It runs while the visitor waits (30 s max).
- **Steps:**
  - condition (all/any rules → *If yes* / *If no* branches)
  - for each (loop)
  - delay (≤10 min)
  - stop / reject
  - run another flow
  - get / create / update / delete records
  - update triggering record
  - send email: pick a template, and its `{{...}}` variables are listed to be mapped
  - API call: a saved call of one of your [APIs](#apis)
  - HTTP request: any address, typed in the step (method, query parameters, headers, body, response format,
    retries)
  - log
  - set variables
  - run script
  - AI decision and AI generate (see [AI](#ai))
  - *Page action flows only*: **Sign in** (email or username and password of an auth collection, `users` by default;
    superusers can't), **Sign out**, **Redirect to page** (a page of the site, with an optional message)
- **Run script** uses a goja JS sandbox: it only sees `data` (trigger, steps, last, item, index) and
  `console.log`, with no app, file or network access, a 5 s limit, a call depth limit, and it's stopped when the
  server heap grows by more than 256 MB while it runs. Return the step output.
- **Responses** of HTTP requests and API calls are read as JSON, XML or raw text: *Automatic* follows the
  `Content-Type` (then the content), or force one format. The step output is `status`, `ok`, `format` (json, xml or
  text), `headers`, `body` (the parsed data, or the text) and `raw` (the text, up to 1 MB; 5 MB are read at most).
  HTTP request steps saved before the formats existed keep reading JSON or text.
- **XML** becomes data that the steps read like JSON: `<rss version="2.0"><channel><title>News</title><item>…</item>
  <item>…</item></channel></rss>` is `{"rss": {"@version": "2.0", "channel": {"title": "News", "item": [{…}, {…}]}}}`.
  Attributes are `@name`, the text of an element with attributes or children is `#text`, repeated elements are a
  list (a single one isn't: *For each* accepts one object as a list of one, and the `list` function always gives a
  list), namespace prefixes are dropped. External entities are never loaded. Names that aren't template
  names are read with `index`: `{{index .steps.feed.body.rss "@version"}}`; the ⚡ picker inserts it for you.
- **Retries** (0 to 3) repeat a request after a network error, a 429 or a 5xx answer (0.5 s, 1 s, 2 s, or the
  `Retry-After` delay up to 5 s), within the timeout. POST and PATCH are only retried when the call allows it.
- **HTTP request** can't reach loopback, private, link-local or cloud metadata addresses, since URLs can come from
  trigger data. Set `POCKETBASEPLUS_FLOWS_ALLOW_PRIVATE_HTTP=1` to allow them (e.g. services on the same network).
- Record data in flows has the same fields as email templates: passwords and hidden fields are left out (except the
  email of auth records). Collections of the modules (roles, permissions, emails, flows) never trigger flows.
- Values inserted into "Get records" filters are bound as parameters, so trigger data can't inject filter syntax.
- Every run is logged in `flow_runs` with each step's input and output (Flows → *flow* → Runs). Chains of
  flows triggering each other stop after 5 levels.

### APIs

*Flows → APIs* (optional module, `internal/apisources`) keeps the outside APIs the flows use, like the data sources
of Budibase or the API calls of FlutterFlow:

- **An API** has a base address (`https://api.example.com/v1`), a sign-in (*Bearer token*, *Basic auth*, *API key in a
  header* or *in the query string*), shared headers and a timeout. The secret is entered once and stays on the
  server.
- **Variables of an API** are shared by its calls (like the API groups of FlutterFlow) and can be used anywhere in
  its base address (`https://{{.vars.host}}/{{.vars.version}}`), shared headers and sign-in (username, key name,
  secret: `{{.vars.token}}` for a token that a flow gets at run time). The values of the variables used by the sign-in
  are hidden in the run logs. A call can't declare a variable with the name of one of its API's.
- **A call** adds a method and a path (`customers/{{.vars.id}}`, or a full address when the API has none), query
  parameters, headers and a body (JSON, form, XML or text) that use its **variables** and those of its API (`{{.vars.name}}`, required or not; a query parameter whose variable is
  empty is left out). It chooses the response format, the retries, and its **outputs**: named values of the response
  (`items.0.id`, `rss.channel.title`).
- **Test the call** sends it as edited, with the test values of its variables, and shows the status, the time, the
  parsed fields (a tree: click a field to add it as an output), the raw text, the headers and the request sent
  (without the secret). The last test response is kept to list the response fields in the flow editor.
- **Values in the address** are escaped as one path segment (`a/../b` can't change the path) or query value; a host
  with a port (`192.168.1.10:8080`) is kept as is. A variable at the very start of the address is the whole base
  address, inserted as written.
- **The API call step** picks a call and maps flow values to its variables and those of its API. Its output has `data.<output>` for each
  output, plus the whole response (`body`, `raw`, `status`, `ok`, `headers`). A 4xx/5xx answer fails the step unless
  "Fail the step on error status" is off.
- A call used by a flow can't be deleted, nor an API with calls. Flows are checked when saved: the call must exist
  and only its variables can be set.

To remove the module: back up `pb_data`, run `./pocketbaseplus apis uninstall --yes` (deletes the APIs and their calls),
remove `apisources.Register(app)` from `main.go` and delete `internal/apisources/`. Flows with API call steps then
fail with "the APIs module isn't installed".

## Functions

The email templates, the flow inputs and the page [formulas](#pages) share one library of functions
(`internal/formula`), in the Go template syntax: `{{name argument1 argument2}}`, nested with parentheses
(`{{currency "EUR" (multiply .record.price .record.qty)}}`) or as a pipeline, the value last
(`{{.record.price | round 2}}`). Every picker lists them with their syntax, a description and an example with its
result (`GET /api/gp/functions`, superusers).

- **Text**: `upper`, `lower`, `capitalize`, `title`, `titleCase`, `trim`, `truncate`, `replace`, `contains`,
  `startsWith`, `endsWith`, `split`, `join`, `concat`, `substr`, `padLeft`, `padRight`, `repeat`, `slug`, `initials`,
  `stripHtml`, `wordCount`, `length`, `urlEncode`
- **Number**: `round`, `floor`, `ceil`, `abs`, `add`, `subtract`, `multiply`, `divide`, `modulo`, `power`, `sqrt`,
  `min`, `max`, `percent`, `formatNumber` (1,234,567.89), `currency` (€1,234.50), `fileSize`, `toNumber`
- **Date**: `now`, `today`, `formatDate`, `timeAgo` (3 days ago, in 2 hours), `toDate`, `parseDate`, `fromUnix`,
  `toUTC`, `addMinutes`, `addHours`,
  `addDays`, `addMonths`, `addYears`, `diffDays`, `diffHours`, `diffMinutes`, `duration` (2h 5m), `startOfDay`,
  `endOfDay`, `startOfWeek`, `startOfMonth`, `endOfMonth`, `startOfYear`, `year`, `month`, `monthName`, `day`,
  `weekday`, `hour`, `minute`, `isPast`, `isFuture`, `isToday`, `isBefore`, `isAfter`, `tz`, `unix`, `date`
- **List**: `list`, `count`, `first`, `last`, `nth`, `take`, `sum`, `avg`, `sort`, `sortBy`, `reverse`, `unique`,
  `pluck`, `where`, `range`
- **Logic**: `if`, `ifElse`, `eq`, `ne`, `gt`, `ge`, `lt`, `le`, `and`, `or`, `not`, `isEmpty`, `default`, `coalesce`
- **Other**: `get`, `json`, `toString`, `printf`

Notes:
- `formatDate` takes tokens: `YYYY YY MMMM MMM MM M DD D dddd ddd HH H hh h mm ss A`, with `[text]` kept as is
  (`{{formatDate "dddd DD/MM/YYYY [at] HH:mm" .record.created}}`), or `date`, `datetime`, `time`, `iso`. Dates are
  in UTC; `tz` moves one to a time zone first (`{{formatDate "HH:mm" (tz "Europe/Paris" .record.start)}}`; the zones
  are built in, offline too). Month and day names, `timeAgo` and number formats are in English for now.
- **Dates for the date fields.** PocketBase stores dates in UTC as `2026-10-18 09:30:00.000Z` and reads only that
  format and ISO 8601 (`2026-10-18T11:30:00+02:00`). The date functions give dates in that format (a date moved to
  a zone with `tz` keeps its offset, and is stored back in UTC), so a flow can store `{{addDays 7 .trigger.record.created}}`.
  Dates typed in another format are converted with `parseDate`: `{{parseDate "DD/MM/YYYY HH:mm" .trigger.form.date
  "Europe/Paris"}}` reads 25/12/2026 14:30 as a Paris time and gives `2026-12-25 13:30:00.000Z`. Also `toDate`
  (standard formats), `fromUnix` (Unix seconds or milliseconds), `toUTC` and `formatDate "storage"`. The flow steps
  that write records refuse a date value PocketBase can't read (it would store it empty, silently) and say to use
  `parseDate`.
- Comparisons (`eq`, `gt`, ...) compare numbers as numbers whatever their type (`gt .record.price 10` works with a
  number field), dates as dates, the rest as text.
- A value of the wrong type gives an empty result rather than an error (`round 2 "abc"` is empty); real errors
  (`divide 1 0`, an unknown time zone or function) are shown in the previews and the run logs. A result is limited to
  256 KB.

## AI

**Settings → AI** (a group of the native settings sidebar):

- **AI providers**: an account with its API key.
  - **TypeSafe** serves **System One** models such as Jev ([docs](https://docs.typesafe.ai)). They answer typed questions
    with calibrated probabilities, in well under a second.
  - **Standard** models (general LLMs):

    | Provider | API | Answers in JSON |
    |---|---|---|
    | OpenRouter | OpenAI compatible, many vendors | strict JSON schema (only routed to providers supporting it) |
    | Anthropic | Messages API (official Go SDK) | structured outputs; thinking blocks are kept in tool loops |
    | OpenAI | OpenAI | strict JSON schema |
    | Google Gemini | OpenAI compatible endpoint, AI Studio key | JSON schema |
    | Kimi (Moonshot) | OpenAI compatible | JSON schema |
    | DeepSeek | OpenAI compatible | JSON mode, the schema is written in the instructions |
    | GLM (Z.ai) | OpenAI compatible | JSON mode, the schema is written in the instructions |

    Set a custom endpoint for proxies or regional accounts (Kimi China `https://api.moonshot.cn/v1`, GLM China
    `https://open.bigmodel.cn/api/paas/v4`). Temperature is only sent when a step sets one (current Claude, GPT and
    Kimi models reject non default values). The reasoning text of DeepSeek, Kimi and GLM is passed back in the
    assistant tool loops.
  - The key is stored in `ai_providers` and is never returned by the API, even to superusers (the list shows its last 4
    characters). Leave the key empty when editing to keep it. **Test** checks the key and lists the models.
- **AI models**: a type (System One or Standard), a provider of that type and the provider's model id (`jev-latest`,
  `openai/gpt-5-mini`...). The model id field suggests the provider's models. **Test** asks three sample questions and
  shows the probabilities. Models used by a flow and providers with models can't be deleted.

**AI decision** flow step: one call evaluates the **data** (named values, by default the fields of the trigger record,
or one expression) against a list of **questions**:

- **Yes / No**: the probability of yes. Optional meanings of yes and no, and the "yes from" threshold (0.5).
- **Choice**: one option, from a fixed list (key and description), the values of a select field (with optional
  descriptions), **records of a collection** (filter, sort, up to 255; the answer is the record id, its name is the
  label, chosen fields describe each record) or a dynamic list (`{{.steps.records.items}}` with key/name/description paths).
- **Score**: a level among 2 to 10 ordered levels (lowest first): a fixed list, the values of a select field, or a
  dynamic list.
- Every question text, description and context value accepts dynamic content. **Context values** send the question as
  structured instructions (`{"question": ..., "candidate": {...}}`) so it can refer to data by name.

Output, per question key (`{{.steps.ai.team.choice}}`):

| Type | Fields |
|---|---|
| Yes / No | `yes`, `value` (probability), `confidence`, `confident` |
| Choice | `choice` (option key or record id), `label`, `probabilities`, `confidence`, `confident` |
| Score | `label`, `level` (0 = lowest), `score` (e.g. 1.4), `probabilities` (by level name), `confidence`, `confident` |

Plus `confident` (every answer reached its "Confident from" value), `model` (the version that answered), `provider`,
`calibrated` and `usage`. Confidence uses TypeSafe's formulas for both providers. With **Branches** on, the step runs
"If confident" or "Otherwise" (e.g. a human review); the branches can use the answers.

- Standard models answer through a strict JSON schema with the probabilities they estimate themselves
  (`calibrated: false`), at temperature 0, routed only to providers enforcing the schema; an invalid answer is retried
  once. "Explain the answers" adds a `reason` per question.
- Rate limits and overloads (429, 529, 502-504) are retried with backoff. Default timeout 30 s (capped by the 15 s
  of "before save" flows). Long texts of the data are cut at "Max characters per text" (2000 by default).
- The run log stores the data and questions sent (never the API key). The record data goes to the provider: hidden
  fields are already left out of flow data.
- Jev's known limits ([jaggedness](https://docs.typesafe.ai/model-jaggedness/jev-1.13.md)): keep math, counting and date
  comparisons in Conditions, write literal questions, and expect text from public forms to try to steer the answers.

### AI generate step

A Standard model writes a **text** (`{{.steps.write.text}}`) or **named fields** (text, number, yes/no, list of texts)
through a strict JSON schema (`{{.steps.write.subject}}`), from instructions (system) and a prompt with dynamic content.
Optional temperature and max tokens. System One models can't generate text.

### AI assistant

**Settings → AI assistant**: enable it, pick a Standard model supporting tool calling, and write **business notes**
(definitions the schema doesn't tell: what a paid order is, where the customers are, the tone of emails...). The
**Assistant** button of the header opens a chat drawer on every page.

- **Context** given to the model at every step: the guide of the app (filter syntax, panel options, template syntax),
  the date, the business notes, the schema of your collections (fields, select values, relations, record counts),
  the [functions](#functions), the flows guide (triggers, step types and their settings), the current page
  (collection, record, dashboard, flow, page or email template) and the status of its proposals (with the result of
  test runs). Records are never dumped in the
  prompt: the model reads them with tools.
- **Read tools**: describe a collection (fields, back-relations, 3 recent records), query records (max 50, long texts
  cut, hidden fields excluded), aggregate (count/sum/avg/min/max, grouped or over time, with the dashboards engine),
  relative dates, the lists of dashboards, email templates, flows, AI models, pages, forms and saved API calls, email
  previews, the functions (syntax and examples), a flow (trigger, steps, last runs; the webhook token stays hidden) and
  its runs (the latest, or one run's step logs, to explain a failure). Internal collections are out of reach.
- **Proposals**: `propose_dashboard` (panels checked against the data, laid out automatically), `propose_email_template`,
  `propose_campaign` (marketing template + recipients counted with a dry run: one email per address, unsubscribed
  skipped, preview with the first recipient), `propose_record` (e.g. a promo code), `propose_flow` (a new flow, or
  changes to one: validated like the flow editor's save, shown as an outline of its steps; new flows are inactive
  unless you asked otherwise, and a flow changed since the proposal is refused) and `propose_flow_run` (a test run with
  a sample trigger or record: **Run** asks for a confirmation, since the steps really run; the result comes back to the
  assistant, which can then read the run log). They show as cards: **nothing is
  created or sent until you click Apply** (Send for campaigns, with a confirmation). Changes are attributed to you
  "(AI assistant)" in Activity.
- Example: "create a dashboard showing the last 30 days orders" → a dashboard card with previews of each panel →
  Apply. "Email the customers who ordered product X over the past 3 days a XXXXX15 code" → the assistant computes the
  date, queries the orders, proposes the promo code record (if you store codes) and the campaign to the matching
  customers → Apply, then Send.
- Example: on a flow's page, "why did this flow fail yesterday?" → the assistant reads the failed runs and the step
  logs, explains the error and proposes the fixed flow → Apply → it proposes a test run.
- **Pages** (with the Pages module, which adds these tools itself: removing the module removes them): `get_page` (the
  settings, an outline of the elements with a ref, their bindings and text, and the HTML when small or of one
  element), `propose_page` (a new page, record page or site part; a draft unless you asked to publish) and
  `propose_page_change` (settings, edits of elements by ref: replace, before, after, append, prepend, remove, or the
  whole HTML, and CSS). Proposals are checked and rendered by the server: binding errors (unknown collection, invalid
  filter or formula) go back to the assistant to fix, and the card shows the rendered page with sample records.
  Scripts and event handlers are removed. Applying keeps a version before and after the change (History in the
  builder), and a page changed since the proposal is refused.
- Not covered yet: forms, permissions, email automations, changing or deleting records, the collections' structure. The assistant says so and explains how to do it in the dashboard.
- Conversations are private to each superuser (`ai_conversations`). Up to 14 model calls per message, 3 minutes.

## Dashboards

- **Dashboards → New dashboard**, then **Edit panels**: add panels, drag them by their header, resize them by their
  bottom right corner (panels pushed out of the way move down), then **Save**. **Cancel** discards the changes.
- **Panels**: metric (count, count distinct, sum, average, min, max, with prefix/suffix, decimals, compact numbers
  and conditional colors), time series (by hour/day/week/month/year over a range, as a line, area or bars), bar
  chart and pie/donut (grouped by a field), meter (a value against a target, automatic when empty), list (chosen
  columns, sort, limit) and label (text). Every data panel accepts a PocketBase filter (`status = "paid"`,
  `created >= @monthStart`...).
- **Suggestions**: once a collection is selected, the panel editor proposes ready to use panels from its field types
  (counts, sums and averages of number fields, series on date fields, breakdowns by select/bool/relation fields,
  latest or top records). Field pickers only list the fields compatible with the chosen function. The preview is live.
- Relation groups and list columns show the related record names (presentable fields, or name/title/email).
  Multiple select/relation values count once per value. Dates are grouped in UTC.
- **Fullscreen**: the whole dashboard (toolbar button) or a single panel (button in the panel header).
  **Auto refresh** is set per dashboard (Settings) and pauses while editing or when the tab is hidden.
- **Sharing**: select roles in the dashboard settings. Users with one of these roles sign in at **`/dashboards`** and
  see the dashboard read-only. Their data goes through the collections' **list rules** (an "Own" scope only counts
  their records), hidden fields aren't available, and collections without a list rule show an access error.
  Superusers can open the viewer with their dashboard session.
- **Variables** (dashboard settings): inputs shown above the panels, for superusers and role viewers. Types: date
  range (presets, or a custom from/to), select (options, or the values of a select field), text and number. Panel
  filters use them as `{{$key}}`, e.g. `({{$status}} = "" || status = {{$status}})` (empty = all) or
  `created >= {{$period.from}} && created < {{$period.to}}`; the panel editor inserts these snippets. Values are
  bound as parameters (they can't change the filter syntax) and the collections' list rules still apply. A time series
  can use a date range variable as its range. The chosen values are remembered per browser.
- **Split series** (time series and bar charts): one series per value of a field (single value fields), the biggest
  ones (5 by default, up to 12) and the others summed in "Other" (counts and sums), optionally stacked.
- **Download CSV**: the button in a panel header exports the data shown by the panel.
- **Drill-down** (superusers): clicking a bar, a slice or a time bucket opens the collection with the matching
  records (the panel filter, the variables and the clicked group).
- Charts use [Apache ECharts](https://echarts.apache.org) (Apache-2.0, embedded): ApexCharts was not used since its
  licence is no longer MIT from v5.2.

## Collections sidebar

- The PocketBasePlus collections (roles, permissions, email_*, flows, flow_runs, dashboards, dashboard_panels,
  collection_groups) are **system collections**: they're listed in the sidebar **System** group, and PocketBase
  prevents renaming or deleting them and changing their API rules (records stay editable).
- **Custom groups**: the folders button next to the sidebar search opens **Collection groups**: create groups, pick
  their collections (one group per collection), order them. Groups show as sidebar sections like **System**
  (collapsible, remembered per browser), grouped collections leave the default **Collections** section, search
  filters them, and pinned collections stay in **Pinned**. Groups are stored in `collection_groups` and shared by
  all superusers.

## Activity

- **Revisions**: every create, update and delete of your collections' records is recorded with its author
  (superuser, user, flow or system), the date, the full record state and the changed fields (`{field: [old, new]}`).
  Updates that change nothing are skipped. Revisions are written in the same transaction as the change.
  History starts when the module is installed (older records have no "created" revision).
- **History** button in the record popup: a timeline of the record's versions with field diffs, **Revert** to a
  version, or **Restore** a deleted record (same id). Files are kept as they are. Reverts are recorded too.
- **Comments** (same popup, Comments tab): superusers comment on records and `@mention` each other (autocomplete).
  Only the author can edit or delete a comment.
- **Notifications**: the bell in the header shows mentions and flow notifications (new **Send notification** flow
  step, to all superusers or chosen emails), live, with unread counts.
- **Activity** tab: all changes, filterable by collection, action, author type and text.
- Retention: revisions older than 180 days are deleted every night. Set `POCKETBASEPLUS_REVISIONS_RETENTION_DAYS`
  (`0` keeps them forever).

## Collection page tools

Next to the collection settings button of a collection page:

- **Views**: save the current filter, sort, visible columns and layout as a view (personal or shared with the other
  superusers) and reopen it in one click. Only the owner can edit or delete a view.
- **Layout**: Table (native), **Cards** (title, image and a few fields), **Kanban** (columns from a select field,
  drag a card to change its value), **Calendar** (month grid on a date field, UTC) and **Map** (markers from a geo
  point field, OpenStreetMap tiles). The layout and its fields (Layout settings) are remembered per collection and
  browser. Layouts show up to 300 records (1000 on the map) of the current filter; clicking one opens the record.
- **Export**: the filtered records (current filter, sort and visible columns) as CSV (UTF-8 with BOM), Excel XLSX
  or JSON. Multiple values are joined with `;`.
- **Import**: CSV (`,` or `;`), XLSX (first sheet) or JSON (array of objects, or the records API `{items}`). Columns
  are matched to fields by name (editable), values are converted (numbers with `.` or `,`, yes/no, `;` separated
  multiple values, `lat, lon` geo points). Modes: create, or update the records matching `id` or a field and create
  the others. **Dry run** validates every row; the import runs in one transaction, so nothing is saved if a row is
  invalid (up to 10 000 rows, 20 MB). Files can't be imported.
- **Bulk edit**: select records in the table, then **Edit** in the selection bar: set fields on all of them, in one
  transaction (the record hooks run, so revisions and flows see the changes).

## Forms

Forms tab: public forms built from the fields of a collection. Each answer creates a record of that collection, so
flows, email automations and revisions see it (the revisions show "Form: <name>" as the author).

- **Fields**: pick and order the fields to show (text, email, url, number, yes/no, select, date, long text, file,
  relation), with a label, placeholder, help text, required flag and full/half width. Relations need "List its
  records publicly" (their names become the options). The editor warns about the collection's required fields that
  are neither shown nor fixed.
- **Fixed values**: set on every answer and never shown: a value or `@now`, `@query.<param>` (from the page URL, e.g.
  `/f/contact?src=newsletter`), `@request.auth.id` (forms requiring a login). Fields that aren't in the form are
  ignored, whatever is sent.
- **Publish**: a page at `/f/{slug}` and an iframe embed at `/f/{slug}/embed` (the embed code resizes the iframe to
  the form). Only the allowed sites can embed the form (`Content-Security-Policy: frame-ancestors`) or submit it from
  their pages (Origin check); the full page can't be embedded. Drafts can only be previewed by superusers.
- The editor preview follows the unsaved changes live (submitting is enabled once saved).
- The form is the access gate: the collection's API rules can stay closed. Optional login (users collection, a
  sign-in step is shown). Spam protection: signed submit tokens (valid 2 h, refreshed automatically), a hidden
  honeypot field and a limit of answers per visitor and hour.
- Texts (title, description, submit button, success message or redirect URL) and look (light/dark/automatic theme,
  accent color). Auth collections aren't supported (use their sign-up).

## Pages

An optional module: the **Pages** tab builds web pages with a drag and drop editor ([GrapesJS](https://grapesjs.com),
embedded) and publishes them on your site. Blocks in the **Data** group show your collections:

- **Collection list**: the records of a collection (filter, sort, max records). Design one card (the *item*) and it's
  repeated for each record; the *empty state* shows when there are none. Picking the collection fills the card with
  its name, image and another field.
- **Field** / **Field image**: a field of the current record (the card's record, or the record of a record page).
  Dates can show the date, date and time, or time; rich text keeps its formatting; relations show the related names.
- **Record link**: links each card to a **record page**, a page showing one record per address (`/{slug}/{id}`),
  created with "New page" → Record page. Its optional filter (e.g. `published = true`) gives a 404 for other records.
- **Record table** (UI kit): select the table (or any of its cells) for its **Columns** setting: move a column left
  or right, remove it, or add one from the collection's fields or its related records' fields (the header and the cell stay together). Each cell's
  field can also be changed in its own settings, and the headers are editable text.
- **Any element of a card** (or of a record page) can show the record's data from its settings, not only the Field
  blocks: a text or heading (*Show field*), an image (*Image field*), a link or button (*Record page*, or *Or link
  field*: a URL, email or file field becomes the link's target while its text stays, e.g. a "Website" button).
- **Related records**: every field setting also lists the fields of the related records, as `brand → name` (the
  binding is `brand.name`; up to two relations, e.g. `order.customer.email`). Several related records show their
  values separated by commas (an image or a link: the first one). A record link can open a related record's page
  (*Brand (its brand)*). Filters take related values too: `country = {{record.brand.country}}`, and the signed-in
  visitor's fields can follow their relations (`team.name`). The related records follow the same rules as the
  record's own fields: hidden fields are never shown, and with the visitor's API rules a related record is only
  shown if they can view it (with the field permissions of their roles). They're loaded once per list, not per card.
- **Formula**: a text computed from the record, the visitor and the address with the [functions](#functions), e.g.
  `{{currency "EUR" (multiply record.price record.qty)}}` or `Added {{timeAgo record.created}} by
  {{default "a guest" auth.name}}`. Drop the **Formula** block, or select any text and use its **Formula** setting:
  the editor lists the record's fields (and its related records'), the visitor's fields (`auth.…`, empty for guests),
  the address parameters (`query.…`) and the functions, and previews the result on the sample record as you type.
  The canvas shows the computed sample. Formulas see what the field bindings see: hidden fields aren't there, and
  with the visitor's API rules only the related records they can view. A relation is its related record
  (`record.brand.name`), or a list for multiple relations (`{{join ", " (pluck "name" record.tags)}}`); alone it
  prints the related names. The result is text (HTML is escaped). A formula error shows in the preview only; the
  published page shows nothing there.
- **Form**: embeds one of your [forms](#forms).
- Plus sections, columns, headings, text, images (uploaded to the page), buttons, links, dividers and videos, with
  the styles panel and desktop/tablet/phone views. The canvas shows sample records; **Preview** opens the saved page
  with your data (drafts too, for an hour).

Pages are published at `/{slug}` (record pages at `/{slug}/{id}`), and one page can be the **home page**, served at
`/` (its slug redirects there). They're rendered by the server: fast, indexed by search engines, no JavaScript needed.

- The app's own addresses always win: `/api`, `/_` (dashboard), `/f` (forms), `/dashboards`, `/email` and `/p` can't
  be page slugs (saving one is refused).
- Older `/p/{slug}` addresses redirect to `/{slug}` (links, flows and bookmarks keep working), and the links of saved
  pages are shown with the new address. The pages' files (images, theme, UI kit) stay under `/p/_…`.

### Members, sign in and page actions

Like Budibase's screen roles and button actions, built on the app's roles (Permissions → Roles & users) and flows:

- **Who can open a page** (page Settings): *Public* (everyone), *Signed-in users*, or *Some roles*. Guests opening a
  protected page are sent to the **sign in page** and come back after signing in; signed-in users without the role
  see "No access" (with a button to switch account). Protected pages have a lock in the Pages lists.
- **Pages → Members & sign in**: choose the sign in page, or **Create the sign in page**. It makes a "Sign in" flow
  (Page action trigger, public: *Sign in* step, then *Redirect to page* back to `?next` or the home page with
  "Welcome back!") and a published page with a login form for guests and "signed in as… / Sign out" for users. Edit
  the flow to add checks (a condition + *Stop / reject* shows its message on the page), notifications...
- Sign in is PocketBase's own auth: the same users and password check (the collection's password auth and auth rule
  apply; MFA isn't supported), and the session is a regular PocketBase auth token of the user (`NewAuthToken`),
  checked on every page request with `FindAuthRecordByToken` (it expires with the collection's token duration and
  when the password changes). Only its transport differs: the published pages run no scripts and browsers never send
  an `Authorization` header when they open a page, so the token travels in an HttpOnly, SameSite=Lax cookie. The
  cookie is only read by the pages and the page actions, never by the REST API (no CSRF exposure). Wrong passwords:
  "Invalid email or password.", 10 failures per IP per 10 minutes.
- **The API rules and roles apply to the page data**, evaluated by PocketBase's rule engine with the visitor as
  `@request.auth` (so the rules generated by the permission matrix, e.g. `@request.auth.roles.id ?= "…"`, work):
  - collection lists with **Data access: API rules of the visitor** (the default for new lists) show the records
    the collection's list rule allows (none for a superusers-only collection), and hide the fields the visitor's
    roles can't read; relations only name the records they can view. *Page is the gate* (existing lists) shows the
    list to everyone who can open the page, as before;
  - record pages with **Apply the view rule** (the default for new record pages): a record the visitor can't view
    gets a 404 (guests are asked to sign in);
  - in Page action flows, the get / create / update / delete record steps **check the API rules of the visitor** by
    default (list, create, update, delete rules, plus the field permissions and presets of their roles); refused,
    the visitor sees "You don't have access to this record.". Turn it off on a step for full rights, like other flows.
- **Members & actions** blocks: *Login form*, *Action button*, *Action form* (inputs: their name is
  `trigger.form.<name>`), *Sign out button* (no flow needed), *Account* (signed in as… / sign in link), *Signed-in
  user field*, *Action messages* (where "Saved!", "Invalid email or password." show; at the top of the page when the
  page has none), *Box for guests*, *Box for signed-in users*. Pick the flow of an action in its settings.
- **Visible to** (settings of every element): everyone, guests only, signed-in users or some roles. Hidden elements
  aren't sent to the visitor. Collection lists can filter on the visitor: `owner = {{auth.id}}` (empty for guests).
- **View as** (builder header): see the page as a public visitor, a signed-in user or a role: elements they don't see
  disappear from the canvas, a banner says when they can't open the page, and **Preview** opens it as them (with the
  first user having that role).
- The pages still run no script: actions are plain forms posted to the server, which answers with the next page.
  Forms posted by other sites are refused.

### History, recovery, duplicate

- **History** (builder header): each save that changes the design keeps a version (the last 30 per page, with who
  saved it, in the `pages_versions` collection). **Preview** opens a version with your data (signed link, an hour);
  **Restore** loads it in the editor, and nothing changes on the site until you save.
- **Unsaved changes** are kept in the browser while you edit (every 20 seconds, and when the tab closes or you leave
  the page). Opening the page again offers to restore them.
- **Duplicate** (page Settings): an unpublished copy with its own copies of the uploaded images.

### Site parts

A **site part** (header, footer, sidebar...) is built once and placed on many pages: editing it changes every page
using it.

- Create one with **New page → Site part** (Header, Footer, Sidebar or Blank templates), or select any section in a
  page and click the bookmark button of its toolbar (**Save as a site part**): it becomes a part and the page shows
  it in place.
- Place a part with the **Site parts** blocks. In the editor it shows as placed (orange outline); its toolbar has
  **Edit** (opens the part, after saving the page) and **Unlink** (an editable copy for this page only).
- New pages from the Landing, Catalog, App with sidebar and Record detail templates offer to use the site's parts
  (matched by name: header/navbar, footer, sidebar) instead of new copies.
- On the published page the part's HTML and CSS replace the placeholder; its collection lists and fields show the
  data of the page (e.g. a record page). Parts are never published on their own and don't nest. They're listed under
  **Site parts** in the Pages sidebar and have their own History.

### Charts

The **Charts** blocks (bar, line, area, pie, donut) show the data of a collection, computed like the
[dashboards](#dashboards) panels (same functions, grouping, time intervals and periods, split series):

- **Settings**: collection, value (count, count of distinct values, sum, average, min, max of a field), *Group by* a
  field (one bar, point or slice per value) or *Over time* (a date field, with an interval and a period), *Series by*
  a field (one series per value, *Stacked* for bars and areas), a filter (`{{record.x}}`, `{{query.x}}` and
  `{{auth.x}}` work as in lists), the max number of bars or slices, the order, the name of the values and the height.
- **Data access**: the visitor's API rules (default: a role only counts the records it can read) or the page as the
  gate, like the collection lists.
- **Colors**: the charts use the theme's chart colors, chosen in **Pages → Theme → Chart colors**: the style's
  default (blue), the chart themes of shadcn/ui (Neutral, Stone, Zinc, Mauve, Olive, Mist, Taupe, Amber, Blue, Cyan,
  Emerald, Fuchsia, Green, Indigo, Lime, Orange, Pink, Purple, Red, Rose, Sky, Teal, Violet, Yellow), shadcn/ui's
  classic *Multicolor* palette (with its dark mode colors), shades of the primary color, or 5 custom colors. The
  theme preview shows a sample chart. A chart can also have its own colors (its *Colors* setting).
- They're drawn by the server (HTML and SVG, in the shadcn/ui style, with the Basecoat theme's `--chart-1..5`
  colors), so they show without JavaScript, fill any width and follow the light / dark mode. A small embedded script
  (a few KB) adds the tooltips that follow the pointer and the legend that hides series; without it the values are in
  the titles. The canvas of the builder shows the real chart (as the "View as" visitor).

### Calendars and date pickers

The **Dates** blocks are form fields like shadcn/ui's Calendar and Date Picker, drawn with the Basecoat theme:

- **Date picker**, **Date range picker**, **Date and time picker** (a field opening the calendar in a popover), and
  inline **Calendar** blocks.
- **Settings**: one date, a range or several dates; the field name (sent to the page action as
  `trigger.form.<name>`; a range sends `<name>_start` and `<name>_end`, several dates are comma separated, a time is
  `YYYY-MM-DDTHH:MM`); required; with a time; one or two months; month and year lists (for distant dates such as a
  birth date); earliest and latest dates (`today` works); unavailable days (weekends, Sundays, weekdays); the first day
  of the week (the visitor's locale by default); the placeholder.
- Keyboard: arrows, Home / End, Page Up / Down (with Shift: a year), Escape closes the popover. Month and day names
  follow the visitor's language.
- Without JavaScript the browser's date fields are shown with the same names, so the forms still work. Values sent in
  the query (a search form using GET) are shown again.

### Basecoat UI kit

New pages use [Basecoat UI](https://basecoatui.com) (shadcn/ui components in plain HTML/CSS), embedded in the app:

- **Components**: button (plain, with an icon before or after, icon only, all variants, button group), light / dark
  toggle, badge, alert, card, avatar, table, accordion, tabs, dialog, dropdown menu, popover, tooltip, breadcrumb,
  item list, empty state, progress, skeleton, keyboard key, separator. Select one to set its variant and size (and a
  tooltip) in its settings; buttons and badges also get an icon (about 40 Lucide icons, inline) placed before or after
  the text; links can point to one of your pages.
- **Sections**: navbar (with a light / dark toggle), hero, features, pricing, testimonials, FAQ, call to action,
  footer, sidebar layout (Basecoat sidebar beside the content: side, open or closed on computers and phones in its
  settings, a toggle button in the top bar, the link of the current page highlighted).
- **Data**: record cards, record table, record FAQ, record items and avatar field, bound to a collection like the
  collection list.
- **Templates** in "New page": Blank, Landing page, Catalog (record cards), App with sidebar, Record detail (record
  pages), or a classic page without the kit.
- **Pages → Theme**: the site theme of these pages: style (Vega, Nova, Maia, Lyra, Mira, Luma, Sera, Rhea), light,
  dark or auto (the visitor's system setting, followed live), font (Geist, embedded, or the system font), primary color, corner radius, and an optional pasted shadcn/ui
  theme (CSS variables), with a live preview in light and dark. With a light / dark toggle on a page, the visitor's
  choice wins over the site mode and is remembered on every page (browser storage). The editor's contrast button
  previews the canvas in the other mode.
- A small inline script in the head of these pages applies the mode before the page shows and runs the theme and
  sidebar toggles (allowed by its hash in the CSP). Tabs, dropdown menus, popovers and the sidebar use Basecoat's
  script: only the pages containing them load it (embedded, `script-src 'self'`; scripts in the page HTML are still
  removed). Dialogs open with the browser's invoker commands
  (`command="show-modal"`, current browsers), without any script. In the editor, hidden tabs, menus and dialogs are
  shown so they can be edited.
- Pages built before the kit keep their look (its base styles reset headings, margins and images); the kit can be
  turned on or off in each page's settings.

### Offline deployment

The app is one binary that loads nothing from the internet: the dashboard, the editor, Basecoat, the fonts and the
icons are embedded. Some features reach outside only when you configure them: AI providers (or a model server on your
network through a custom endpoint, e.g. Ollama or LM Studio), SMTP, flow HTTP steps (`POCKETBASEPLUS_FLOWS_ALLOW_PRIVATE_HTTP=1`
for services on your network), and map tiles (OpenStreetMap by default).

- `POCKETBASEPLUS_OFFLINE=1`: published pages may only load the app's files (images, fonts and embeds from other sites are
  blocked, and the builder warns when a page uses some), and the Map layout shows its markers on a plain grid.
- `POCKETBASEPLUS_MAP_TILES=http://10.0.0.5:8080/tile/{z}/{x}/{y}.png`: a tile server on your network for the Map layout
  (allowed in the dashboard CSP). PocketBase's own location field still uses OpenStreetMap.

- **Access**: like forms, the page is the gate (and its "Who can open the page" setting, see
  [members](#members-sign-in-and-page-actions)). Visitors see the records your lists select and only the fields you
  placed; they can't change the filters (a filter can use `{{query.x}}`, a value of the page address, bound as a
  parameter, and `{{record.x}}`, a field of the current record, e.g. `brand = {{record.id}}` for the products of a
  brand page). Hidden fields, passwords, protected files and the emails of auth records (unless public) are never
  shown. The collections' API rules don't apply, so check the filters of the lists of private data.
- **Safety**: pages run no scripts (CSP `script-src 'none'`), except the kit's mode script (allowed by its hash) and
  Basecoat's embedded script on the pages with tabs, menus, popovers or a sidebar; scripts, event handlers and `javascript:` links are removed from the HTML, third-party
  embeds are sandboxed, and uploaded images are served sandboxed.

### Removing the Pages module

1. Optional, to delete the pages and site parts, their images, versions and the theme: back up `pb_data`, then run
   `./pocketbaseplus pages uninstall --yes`.
2. Remove the `pages.Register(app)` line (and the import) from `main.go`, then rebuild. The tab, the page
   routes and the embedded editor are gone. You can also delete `internal/pages/`.

The visitor sessions (`internal/session`) and the Page action flows stay: they don't depend on the module.

Without step 1 the `pages`, `pages_settings` and `pages_versions` collections stay in the database (unused) and come back to life if the
module is added again.

## Credits and license

PocketBasePlus is released under the [MIT license](LICENSE). It is built on:

- [PocketBase](https://github.com/pocketbase/pocketbase) (MIT): the backend, database, auth and dashboard.
- Embedded in the binary, with their licenses next to them: [GrapesJS](https://github.com/GrapesJS/grapesjs)
  (BSD-3-Clause, the page builder), [Basecoat](https://github.com/hunvreus/basecoat) (MIT, the pages' UI kit),
  [Lucide](https://github.com/lucide-icons/lucide) (ISC, icons), [Geist](https://github.com/vercel/geist-font)
  (SIL OFL 1.1, fonts), [Apache ECharts](https://github.com/apache/echarts) (Apache-2.0, charts) and
  [Leaflet](https://github.com/Leaflet/Leaflet) (BSD-2-Clause, maps).
- Go modules listed in `go.mod` (among them goja, excelize, cobra and the Anthropic Go SDK), each under its own license.
