# Serving documents through the Wails asset server

The viewer fetches the open PDF over HTTP from the Wails asset server rather
than receiving it as base64 across the bridge. This document records how that
actually works on each platform, because the behaviour differs in ways that are
invisible until you run the app on all three.

Everything below was read out of Wails v2.11.0 and `pdfjs-dist` 4.10.38 as
vendored in this repo, not from documentation.

## How a request reaches our handler

`assetserver.Options.Handler` is **not** a router — it is a fallback. From
`pkg/assetserver/assethandler.go`:

- A `GET` is first looked up in `Options.Assets`. Only when that returns
  `os.ErrNotExist` is `Handler` called.
- Every non-`GET` request goes straight to `Handler`.
- If `Handler` is nil, a missing asset is a bare 404.

So `/_file/<token>` reaches us simply because no such file is embedded. Nothing
needs registering.

One trap worth knowing: `AssetServer.ServeHTTP` intercepts paths ending in `/`
or `/index.html` and runs them through an HTML-injection recorder
(`isRuntimeInjectionMatch`). Our URLs end in a hex token, so they take the plain
pass-through branch. **Never give the handler a URL path ending in a slash.**

Requests are dispatched to a worker pool and the response is streamed back
through a per-platform `ResponseWriter`, so writing a large body does not block
the UI thread.

## Platform matrix

| | Windows | macOS | Linux (`webkit2_41`) | Linux (legacy 4.0) |
|---|---|---|---|---|
| Webview | WebView2 | WKWebView | WebKitGTK 4.1 | WebKitGTK 4.0 |
| Origin | `http://wails.localhost/` | `wails://wails/` | `wails://wails/` | `wails://wails/` |
| Request headers forwarded | yes | yes | yes | **no — faked** |
| Status codes other than 200 | yes | yes | yes | **no — request fails** |
| Range requests reach us | yes | yes | yes | no |
| pdf.js transport | fetch | XHR | XHR | XHR |

Sources: `internal/frontend/desktop/{windows,darwin,linux}/frontend.go` for the
origins; `pkg/assetserver/webview/request_*.go` for header forwarding;
`pkg/assetserver/webview/webkit2_36+.go` versus `webkit2_legacy.go` for the
Linux split.

### The Linux build tag matters

Without a `webkit2_36`/`webkit2_40`/`webkit2_41` build tag, Wails compiles
`webkit2_legacy.go`, in which:

```go
func webkit_uri_scheme_request_get_http_headers(_ *C.WebKitURISchemeRequest) http.Header {
	// Fake some basic default headers ...
}

func webkit_uri_scheme_request_finish(..., code int, ...) error {
	if code != http.StatusOK {
		return fmt.Errorf("StatusCodes not supported: %d - %s", ...)
	}
```

Real request headers are discarded and **any** status other than 200 fails the
request outright. This is why the build uses `-tags webkit2_41` (see
`.github/workflows/build.yml` and the README).

The legacy path still *works* — no `Range` header ever arrives, so
`http.ServeContent` always answers 200 with the whole file — but nothing
streams. What it cannot tolerate is a 304, which is why the handler strips
conditional request headers before calling `ServeContent`.

## Why pdf.js uses XHR on macOS and Linux

`getDocument` chooses its transport in `pdf.mjs`:

```js
NetworkStream = isValidFetchUrl(url) ? PDFFetchStream : PDFNetworkStream;
```

and `isValidFetchUrl` returns true only for `http:` and `https:`. The macOS and
Linux webviews serve from `wails://`, so pdf.js falls back to
`PDFNetworkStream`, which is XHR-based.

That is fine: `PDFNetworkStream` issues range requests too
(`xhr.setRequestHeader("Range", ...)`), as does `PDFFetchStream`
(`headers.append("Range", ...)`). Both paths stream.

Note that `isValidFetchUrl` is called **without a base URL**, so `new URL(url)`
throws on a relative path and the check returns false. Passing a relative URL
would therefore force XHR even on Windows. `Viewer.jsx` resolves the URL against
`document.baseURI` before handing it to pdf.js so the fetch transport is
available where the origin allows it.

## Range degradation is safe

When a range request is answered with a full 200 body — the legacy Linux case —
pdf.js handles it explicitly rather than corrupting the document:

```js
const ok_response_on_range_request =
  xhrStatus === OK_RESPONSE && pendingRequest.expectedStatus === PARTIAL_CONTENT_RESPONSE;
...
} else if (chunk) {
  pendingRequest.onDone({ begin: 0, chunk });
}
```

A 200 to a range request is accepted as the whole file from offset 0. So
advertising `Accept-Ranges: bytes` — which `http.ServeContent` does
unconditionally — is safe even where ranges cannot actually be honoured.

## Access control

The handler serves by opaque token, never by path:

- `FileURL(path)` resolves the path, checks it is a regular file, and registers
  `sha256(abs)[:16]` → path.
- The handler looks the token up. A token that was never granted is a 404.

Filesystem paths never appear in URLs, and the handler cannot be induced to read
an arbitrary file — path traversal has nothing to traverse. Grants are revoked
when a working file is pruned and when the session is cleaned up.

`ExpectedWebViewHost` is set on macOS only (to `wails`), and requests with a
different `Host` are rejected there before reaching any handler.

## What still needs a real run

The Go side is covered by `backend/viewer/fileserver_test.go` — routing, token
rejection, range support, cache headers, revocation. What tests cannot cover is
the webview integration itself:

1. That the relative-to-absolute URL resolution lands on the right origin on
   each platform.
2. That WebKit permits XHR from a `wails://wails/` document to a `wails://`
   URL. Same-origin requests to a custom scheme should be allowed — Wails
   registers `wails` as an ordinary custom scheme
   (`webkit_web_context_register_uri_scheme`) without marking it no-access —
   but this is the one assumption not verified by reading code alone.
3. That a 206 from a `WKURLSchemeHandler` is surfaced correctly to XHR on
   macOS.

If (2) or (3) turn out to be wrong, the symptom is a failed or hanging document
load, and the fallback is to pass `disableRange: true` to `getDocument`, which
forces a single full-file request.
