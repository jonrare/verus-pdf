# VerusPDF

A free PDF editor. Merge, split, rotate, extract, encrypt, and edit PDFs — no subscription, no paywall.

**[Download](https://veruspdf.com)** · **[Report a Bug](https://github.com/YOUR_USERNAME/verus-pdf/issues)**

---

## Features

- **Edit text** — click and edit any text span in place
- **Merge & split** — combine PDFs or split by page count / bookmarks
- **Extract pages** — keep only what you need (ranges, individual pages, mixed)
- **Rotate pages** — fix sideways scans, any page or range
- **Encrypt & decrypt** — add/remove password protection (AES-128, AES-256)
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

- [Go 1.21+](https://go.dev/dl/)
- [Node.js 20+](https://nodejs.org/)
- [Wails CLI](https://wails.io/docs/gettingstarted/installation)

```bash
go install github.com/wailsapp/wails/v2/cmd/wails@latest
```

**Linux only** — GTK and WebKit dev libraries:
```bash
sudo apt-get install libgtk-3-dev libwebkit2gtk-4.0-dev pkg-config
```

### Build

```bash
# Install frontend dependencies
cd frontend && npm ci && cd ..

# Development (hot reload)
wails dev

# Production build
wails build
```

The binary is output to `build/bin/`.

### CI Build

The repo includes a GitHub Actions workflow (`.github/workflows/build.yml`) that builds Windows, macOS (universal binary), and Linux artifacts. Trigger it manually from the Actions tab.

## License

MIT
