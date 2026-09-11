# Servo documentation

The public site targets end-users and has three pages: [Use Servo](content/_index.md),
[Caddy](content/caddy.md), and [Drivers](content/drivers.md). Caddy setup and driver
authoring are the only deep dives. Use the practical, conversational voice of the
project README and `local/` references, while checking behavior against current code.
`local/` is historical material and is not published.

Repository-only reference:

- [Development and tests](DEVELOPMENT.md)
- [Architecture](ARCHITECTURE.md)
- [Installer maintenance protocol](MAINTENANCE.md)
- [Releases](RELEASES.md)
- [Docs deployment](DEPLOYMENT.md)
- [Icebox](ICEBOX.md)

## Build the site

From the repository root, fetch the pinned Hugo executable and use the returned path:

```sh
hugo_bin=$(./scripts/vendor.sh hugo | sed -n 's/^hugo=//p')
cd docs
go mod download
go mod verify
"$hugo_bin" server --disableFastRender
```

For production, use `"$hugo_bin" --gc --minify --panicOnWarning` with `HUGO_ENV=production`
and `HUGO_ENVIRONMENT=production`. Output goes to ignored `docs/out/`. Remove old output
before checking a reorganization: Hugo does not automatically delete obsolete pages.
The CI checkout is clean. PR builds don't require the production deployment gate.

## Design and checks

Hugo/Hextra still supplies Markdown, code highlighting, copy buttons, search and the
pinned build. Local layouts provide a small three-page shell. The core script partial loads Hextra's code-copy controller and, on the homepage,
the local pixel-hover controller. It omits the unused sidebar/theme controllers. The design is always
dark; its pixel wordmark and signal drawing are SVG, with no remote fonts or image CDN.
Custom CSS lives in `assets/css/custom.css`. The logo reacts per pixel to pointer hover with color, scale and offset echoes.
The hit areas stay fixed to avoid hover flicker. Touch leaves the logo static;
reduced motion uses immediate color changes only.

`assets/json/search-data.json` includes the homepage operator guide as well as the
two regular pages. Keep internal notes outside `content/` so they cannot enter search.
The old public routes redirect to their corresponding sections through `static/_redirects`.

Validate the warning-fatal build, internal links and anchors, search (including homepage
sections), mobile/desktop layouts, keyboard navigation, reduced motion, and the 404 page.
Keep README and AGENTS links current when moving repository notes. Site changes don't
alter the application's dashboard themes or appearance settings.
