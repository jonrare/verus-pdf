# VerusPDF

A free PDF editor. Merge, split, rotate, extract, encrypt, and edit PDFs — no subscription, no paywall.

**[Download](https://veruspdf.com)** · **[Report a Bug](https://github.com/jonrare/verus-pdf/issues)**

---

## Features

- **Edit text** — click and edit any text span in place
- **Merge & split** — combine PDFs or split by page count / bookmarks
- **Extract pages** — keep only what you need (ranges, individual pages, mixed)
- **Rotate pages** — fix sideways scans, any page or range
- **Encrypt & decrypt** — add/remove password protection (AES-256)
- **Extract text** — pull all selectable text to clipboard or file
- **Bookmarks** — add, remove, navigate
- **Optimize** — compress and deduplicate resources
- **Tabs** — work with multiple PDFs at once
- **6400% zoom** — from 1% to 6400% in 22 steps

## Tech Stack

- **Backend:** Go + [pdfcpu](https://github.com/pdfcpu/pdfcpu)
- **Frontend:** React + [pdf.js](https://mozilla.github.io/pdf.js/)
- **Framework:** [Wails v2](https://wails.io) (native desktop, no Electron)

## Building from Source

### Prerequisites

- [Go 1.24+](https://go.dev/dl/) (see `go.mod` for the exact toolchain)
- [Node.js 20+](https://nodejs.org/)
- [Wails CLI](https://wails.io/docs/gettingstarted/installation)

```bash
go install github.com/wailsapp/wails/v2/cmd/wails@latest
```

**Linux only** — GTK and WebKit dev libraries:
```bash
sudo apt-get install libgtk-3-dev libwebkit2gtk-4.1-dev pkg-config
```

### Build

```bash
# Install frontend dependencies
cd frontend && npm ci && cd ..

# Development (hot reload)
wails dev

# Production build
wails build           # add -tags webkit2_41 on Linux with WebKit 4.1
```

The binary is output to `build/bin/`.

> `main.go` embeds `frontend/dist`, which is gitignored. Build the frontend
> (`cd frontend && npm run build`) before running bare `go build` / `go test`,
> or just use `wails build`, which does it for you.

### Tests

```bash
go test ./...
```

The PDF parsing and editing code under `backend/` carries the bulk of the test
suite. See **[docs/pdf-spec.md](docs/pdf-spec.md)** for the specification each
module implements and how citations in the source map to it.

### CI

- `.github/workflows/ci.yml` — runs `go vet`, `go test`, and the frontend build
  on every push and pull request.
- `.github/workflows/build.yml` — packages Windows, macOS (universal binary),
  and Linux artifacts. Trigger it manually from the Actions tab.

## Architecture notes

**Documents are served, not shipped.** The viewer fetches the open PDF from the
asset server (`backend/viewer/fileserver.go`) rather than receiving it as base64
over the Wails bridge. The handler serves by opaque token, so filesystem paths
stay out of URLs and only files the app explicitly granted can be read. Range
requests are supported, so pdf.js renders the first page without waiting for the
whole file. The webview origin, header forwarding and range behaviour all differ
per platform — see **[docs/asset-server.md](docs/asset-server.md)**, which also
explains why the Linux build needs `-tags webkit2_41`.

**Edits go to per-session working files.** Each operation writes a fresh file in
a private `0700` directory, keeping a bounded window so undo has something to
return to. The directory is removed on shutdown — without that, decrypted copies
of protected documents would outlive the app.

## Not yet implemented

These services are bound to the frontend but currently return "not yet
implemented" errors: **annotations** (`backend/annotate`) and **OCR**
(`backend/ocr`, which would need the CGO-based go-fitz + gosseract), plus
`forms.LockForm`.

## License

[MIT](LICENSE)
