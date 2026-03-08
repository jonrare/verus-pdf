import { useEffect, useRef, useState, useCallback } from 'react'
import * as pdfjsLib from 'pdfjs-dist'
import workerUrl from 'pdfjs-dist/build/pdf.worker.min.mjs?url'
import { useAppStore } from '../stores/appStore'
import { useOperation } from '../hooks/useOperation'
import { ReadFileBytes, RotatePages, ExtractPageText, ReplaceSpanText } from '../wails.js'
import { zoomIn, zoomOut, clampZoom } from '../zoomLevels'

pdfjsLib.GlobalWorkerOptions.workerSrc = workerUrl

const DRAG_THRESHOLD = 4

// PDF coordinate system: origin at bottom-left, Y increases upward.
// Canvas coordinate system: origin at top-left, Y increases downward.
// Coordinate conversion via pdf.js viewport (handles page rotation)

export default function Viewer({ isEditMode = false }) {
  const { document, currentPage, zoom, setZoom } = useAppStore()
  const { run, docPath } = useOperation()

  const canvasRef    = useRef(null)
  const containerRef = useRef(null)
  const pdfRef       = useRef(null)
  const renderTask   = useRef(null)
  const menuRef      = useRef(null)
  const editInputRef = useRef(null)

  const [error, setError]       = useState(null)
  const [loading, setLoading]   = useState(false)
  const [menu, setMenu]         = useState(null)
  const [menuPos, setMenuPos]   = useState(null)
  const [isDragging, setIsDragging] = useState(false)
  const [overSpan,   setOverSpan]   = useState(false)

  // Text editing state
  const [spans, setSpans]           = useState([])       // extracted TextSpans for current page
  const [spansLoading, setSpansLoading] = useState(false)
  const [editTarget, setEditTarget] = useState(null)     // { span, canvasX, canvasY, inputValue }
  const [editError, setEditError]   = useState(null)

  // Free-floating canvas position
  const offset    = useRef({ x: 0, y: 0 })
  const dragState = useRef(null)
  const zoomRef   = useRef(zoom)
  const [, forceRender] = useState(0)
  const lastCentredPage = useRef(null)
  // Scale-1 viewport — used for coordinate conversion (handles page rotation)
  const pageViewportRef  = useRef(null)
  const pageRotationRef  = useRef(0)  // 0 | 90 | 180 | 270


  useEffect(() => { zoomRef.current = zoom }, [zoom])

  const centreCanvas = useCallback((page) => {
    if (lastCentredPage.current === page) return
    lastCentredPage.current = page
    const container = containerRef.current
    const canvas    = canvasRef.current
    if (!container || !canvas) return
    offset.current = {
      x: Math.max(0, (container.clientWidth  - canvas.offsetWidth)  / 2),
      y: Math.max(0, (container.clientHeight - canvas.offsetHeight) / 2),
    }
    forceRender(n => n + 1)
  }, [])

  // Recentre canvas when the container is resized (e.g. panel opening/closing)
  useEffect(() => {
    const container = containerRef.current
    if (!container) return
    const ro = new ResizeObserver(() => {
      // Only recentre if canvas is currently centred (hasn't been panned).
      // We detect this by checking if the canvas is roughly centred.
      const canvas = canvasRef.current
      if (!canvas) return
      const cw = container.clientWidth
      const ch = container.clientHeight
      const pw = canvas.offsetWidth
      const ph = canvas.offsetHeight
      // If canvas fits within container, recentre it
      if (pw <= cw && ph <= ch) {
        offset.current = {
          x: Math.max(0, (cw - pw) / 2),
          y: Math.max(0, (ch - ph) / 2),
        }
        forceRender(n => n + 1)
      }
    })
    ro.observe(container)
    return () => ro.disconnect()
  }, [])

  // ── Load PDF ──────────────────────────────────────────────────────────────
  useEffect(() => {
    if (!document?.path) return
    let cancelled = false
    setLoading(true); setError(null)

    async function load() {
      try {
        const b64    = await ReadFileBytes(document.path)
        if (cancelled) return
        const binary = atob(b64)
        const bytes  = new Uint8Array(binary.length)
        for (let i = 0; i < binary.length; i++) bytes[i] = binary.charCodeAt(i)
        const pdf = await pdfjsLib.getDocument({ data: bytes }).promise
        if (cancelled) { pdf.destroy(); return }
        pdfRef.current = pdf
        setLoading(false)
      } catch (e) {
        if (!cancelled) { setError(`Failed to load PDF: ${e.message}`); setLoading(false) }
      }
    }
    load()
    return () => { cancelled = true }
  }, [document?.path])

  // ── Render page ───────────────────────────────────────────────────────────
  useEffect(() => {
    if (!pdfRef.current || !canvasRef.current || loading) return
    let cancelled = false

    async function render() {
      try {
        if (renderTask.current) { renderTask.current.cancel(); renderTask.current = null }
        const page     = await pdfRef.current.getPage(currentPage)
        if (cancelled) return
        const dpr      = window.devicePixelRatio || 1
        const viewport = page.getViewport({ scale: zoom * dpr })
        const canvas   = canvasRef.current
        if (!canvas) return
        canvas.width  = viewport.width
        canvas.height = viewport.height
        canvas.style.width  = `${viewport.width  / dpr}px`
        canvas.style.height = `${viewport.height / dpr}px`
        // Store scale:1 viewport — convertToViewportPoint handles rotation
        const vp1 = page.getViewport({ scale: 1 })
        pageViewportRef.current = vp1
        pageRotationRef.current = vp1.rotation ?? 0
        const task = page.render({ canvasContext: canvas.getContext('2d'), viewport })
        renderTask.current = task
        await task.promise
        renderTask.current = null
        centreCanvas(currentPage)
      } catch (e) {
        if (!cancelled && e?.name !== 'RenderingCancelledException')
          setError(`Render error: ${e.message}`)
      }
    }
    render()
    return () => { cancelled = true }
  }, [pdfRef.current, currentPage, zoom, loading])

  // ── Extract text spans when entering edit mode ────────────────────────────
  useEffect(() => {
    if (!isEditMode || !document?.path) { setSpans([]); return }
    let cancelled = false
    setSpansLoading(true)

    async function extract() {
      try {
        const result = await ExtractPageText(document.path, currentPage)
        if (!cancelled) { setSpans(result ?? []); setEditError(null) }
      } catch (e) {
        if (!cancelled) {
          setSpans([])
          const msg = e?.message ?? String(e)
          // Surface encrypted-file error prominently
          if (msg.includes('encrypted') || msg.includes('password') || msg.includes('corrupt')) {
            setEditError('🔒 This PDF is encrypted. Use Security → Remove Protection first, then re-open to edit text.')
          }
        }
      } finally {
        if (!cancelled) setSpansLoading(false)
      }
    }
    extract()
    return () => { cancelled = true }
  }, [isEditMode, document?.path, currentPage])

  // Focus the edit input when it appears
  useEffect(() => {
    if (editTarget) setTimeout(() => editInputRef.current?.focus(), 30)
  }, [editTarget])

  // ── Span hit-testing ─────────────────────────────────────────────────────
  // For rotated spans we rotate the query point into the span's local frame
  // (un-rotate around the span's origin) before doing a simple AABB check.
  const hitTestSpan = useCallback((pdfX, pdfY, s, pad = 4) => {
    const cwRatio = s.fontSize >= 20 ? 0.56 : s.fontSize >= 12
      ? 0.42 + (s.fontSize - 12) * (0.56 - 0.42) / 8 : 0.42
    const spanW   = s.text.length * s.fontSize * cwRatio
    const ascent  = s.fontSize * 1.05
    const descent = s.fontSize * 0.35
    const rot     = (s.rotation ?? 0) * Math.PI / 180  // CCW radians
    // Translate so span origin is at (0,0), then un-rotate
    const dx = pdfX - s.x
    const dy = pdfY - s.y
    const lx =  dx * Math.cos(rot) + dy * Math.sin(rot)
    const ly = -dx * Math.sin(rot) + dy * Math.cos(rot)
    return lx >= -pad && lx <= spanW + pad &&
           ly >= -descent - pad && ly <= ascent + pad
  }, [])

  // ── Coordinate conversion ─────────────────────────────────────────────────
  // PDF content-stream coords → canvas CSS pixels.
  // viewport.convertToViewportPoint handles page rotation automatically.
  const pdfToCanvas = useCallback((pdfX, pdfY) => {
    const vp = pageViewportRef.current
    if (!vp) return { x: pdfX * zoom, y: pdfY * zoom }
    const [vx, vy] = vp.convertToViewportPoint(pdfX, pdfY)
    return { x: vx * zoom, y: vy * zoom }
  }, [zoom])

  // Canvas CSS pixels → PDF content-stream coords.
  // Inverts the viewport transform matrix manually (works in all pdfjs versions).
  const canvasToPdf = useCallback((cx, cy) => {
    const vp = pageViewportRef.current
    if (!vp) return { x: cx / zoom, y: cy / zoom }
    // vp.transform = [a, b, c, d, e, f]  maps PDF → viewport
    // We need the inverse to map viewport → PDF
    const [a, b, c, d, e, f] = vp.transform
    const det = a * d - b * c
    if (Math.abs(det) < 1e-10) return { x: cx / zoom, y: cy / zoom }
    const vx = cx / zoom - e
    const vy = cy / zoom - f
    return { x: ( d * vx - c * vy) / det,
             y: (-b * vx + a * vy) / det }
  }, [zoom])

  // ── Click handling ────────────────────────────────────────────────────────
  const handleCanvasClick = useCallback((e) => {
    if (!isEditMode || dragState.current?.moved) return

    const canvas = canvasRef.current
    if (!canvas) return
    const rect = canvas.getBoundingClientRect()
    const cx   = e.clientX - rect.left
    const cy   = e.clientY - rect.top

    const { x: pdfX, y: pdfY } = canvasToPdf(cx, cy)

    // Hit-test using same metrics as the overlay boxes
    const hit = spans.find(s => hitTestSpan(pdfX, pdfY, s))

    if (!hit) {
      setEditTarget(null)
      return
    }

    const { x: canX, y: canY } = pdfToCanvas(hit.x, hit.y)
    setEditTarget({
      span:       hit,
      left:       offset.current.x + canX - 3,
      top:        offset.current.y + canY - hit.fontSize * 1.05 * zoom - 3,
      inputValue: hit.text,
    })
    setEditError(null)
  }, [isEditMode, spans, canvasToPdf, pdfToCanvas, zoom])

  const commitEdit = useCallback(async () => {
    if (!editTarget) return
    const { span, inputValue } = editTarget
    if (inputValue === span.text) { setEditTarget(null); return }

    setEditTarget(t => ({ ...t, saving: true }))
    try {
      await run('Edit text', async (out) => {
        const result = await ReplaceSpanText(
          docPath, out,
          span.pageNum, span.streamIndex, span.opStart, span.opEnd,
          inputValue
        )
        if (result.error) throw new Error(result.error)
        if (result.truncated) setEditError(`Text truncated to: "${result.actualText}"`)
        else if (result.padded) setEditError(null)
        else setEditError(null)
        return result
      })
      // Re-extract spans after edit so offsets stay accurate
      const fresh = await ExtractPageText(docPath, currentPage)
      setSpans(fresh ?? [])
    } catch (e) {
      setEditError(e.message)
    } finally {
      setEditTarget(null)
    }
  }, [editTarget, run, docPath, currentPage])

  const cancelEdit = useCallback(() => { setEditTarget(null); setEditError(null) }, [])

  // ── Scroll wheel — zoom, pan (CAD-style) ────────────────────────────────
  // Plain scroll    → zoom toward cursor
  // Shift + scroll  → pan up / down
  // Ctrl + scroll   → pan left / right
  // Middle-button drag → pan (see onMouseDown)
  const handleWheel = useCallback((e) => {
    if (!document) return
    e.preventDefault()
    const container = containerRef.current
    if (!container) return

    const PAN_STEP = 60  // px per scroll tick

    if (e.shiftKey) {
      // Pan vertically
      offset.current = { ...offset.current, y: offset.current.y - (e.deltaY > 0 ? PAN_STEP : -PAN_STEP) }
      forceRender(n => n + 1)
      return
    }

    if (e.ctrlKey) {
      // Pan horizontally
      offset.current = { ...offset.current, x: offset.current.x - (e.deltaY > 0 ? PAN_STEP : -PAN_STEP) }
      forceRender(n => n + 1)
      return
    }

    // Plain scroll → zoom toward cursor
    const rect    = container.getBoundingClientRect()
    const cursorX = e.clientX - rect.left
    const cursorY = e.clientY - rect.top
    const oldZoom = zoomRef.current
    const newZoom = e.deltaY > 0 ? zoomOut(oldZoom) : zoomIn(oldZoom)
    const scale   = newZoom / oldZoom
    offset.current = {
      x: cursorX - (cursorX - offset.current.x) * scale,
      y: cursorY - (cursorY - offset.current.y) * scale,
    }
    forceRender(n => n + 1)
    setZoom(newZoom)
  }, [document, setZoom])

  useEffect(() => {
    const el = containerRef.current
    if (!el) return
    el.addEventListener('wheel', handleWheel, { passive: false })
    return () => el.removeEventListener('wheel', handleWheel)
  }, [handleWheel])

  // ── Drag-to-pan ───────────────────────────────────────────────────────────
  // Track when mousedown was suppressed because it landed on a span.
  // onMouseUp checks this to still fire the click handler.
  const downOnSpan = useRef(false)

  const onMouseDown = useCallback((e) => {
    // Middle mouse button always pans regardless of mode
    if (e.button === 1) {
      e.preventDefault()
      dragState.current = {
        startX: e.clientX, startY: e.clientY,
        ox: offset.current.x, oy: offset.current.y,
        moved: false, middle: true,
      }
      return
    }
    if (e.button !== 0) return

    // In edit mode, check if the click is over a span — if so, suppress drag
    if (isEditMode && canvasRef.current) {
      const rect = canvasRef.current.getBoundingClientRect()
      const cx   = e.clientX - rect.left
      const cy   = e.clientY - rect.top
      const { x: pdfX, y: pdfY } = canvasToPdf(cx, cy)
      const overSpan = spans.some(s => hitTestSpan(pdfX, pdfY, s))
      if (overSpan) {
        downOnSpan.current = true
        dragState.current  = null
        return
      }
    }

    downOnSpan.current = false
    dragState.current = {
      startX: e.clientX, startY: e.clientY,
      ox: offset.current.x, oy: offset.current.y,
      moved: false,
    }
  }, [isEditMode, spans, canvasToPdf])

  const onMouseMove = useCallback((e) => {
    // Hover detection for cursor — always runs
    if (isEditMode && canvasRef.current) {
      const rect = canvasRef.current.getBoundingClientRect()
      const cx = e.clientX - rect.left
      const cy = e.clientY - rect.top
      const { x: pdfX, y: pdfY } = canvasToPdf(cx, cy)
      const over = spans.some(s => hitTestSpan(pdfX, pdfY, s))
      setOverSpan(over)
    } else {
      setOverSpan(false)
    }
    // Drag pan
    if (!dragState.current) return
    const dx = e.clientX - dragState.current.startX
    const dy = e.clientY - dragState.current.startY
    if (!dragState.current.moved && Math.hypot(dx, dy) < DRAG_THRESHOLD) return
    if (!dragState.current.moved) { dragState.current.moved = true; setIsDragging(true) }
    e.preventDefault()
    offset.current = { x: dragState.current.ox + dx, y: dragState.current.oy + dy }
    forceRender(n => n + 1)
  }, [isEditMode, spans, canvasToPdf])

  const onMouseUp = useCallback((e) => {
    const wasMiddle = dragState.current?.middle
    if (!wasMiddle && (downOnSpan.current || (dragState.current && !dragState.current.moved))) {
      handleCanvasClick(e)
    }
    downOnSpan.current = false
    dragState.current  = null
    setIsDragging(false)
  }, [handleCanvasClick])

  // ── Context menu ──────────────────────────────────────────────────────────
  const handleContextMenu = useCallback((e) => {
    if (!document) return
    e.preventDefault()
    setMenu({ x: e.clientX, y: e.clientY }); setMenuPos(null)
  }, [document])

  useEffect(() => {
    if (!menu || !menuRef.current) return
    const { width, height } = menuRef.current.getBoundingClientRect()
    setMenuPos({
      x: Math.min(menu.x, window.innerWidth  - width  - 8),
      y: Math.min(menu.y, window.innerHeight - height - 8),
    })
  }, [menu])

  const closeMenu = useCallback(() => { setMenu(null); setMenuPos(null) }, [])

  const rotatePage = useCallback(() => {
    closeMenu()
    run('Rotate 90°', (out) => RotatePages(docPath, out, 90, String(currentPage)))
  }, [closeMenu, run, docPath, currentPage])

  if (!document) return null

  const menuItems = [
    { label: 'Zoom in',   action: () => { setZoom(zoomIn(zoom)); closeMenu() } },
    { label: 'Zoom out',  action: () => { setZoom(zoomOut(zoom)); closeMenu() } },
    { label: 'Fit to page', action: () => {
      closeMenu()
      const c = containerRef.current; const p = document?.pages?.[currentPage - 1]
      if (!c || !p) return
      setZoom(Math.min((c.clientWidth - 48) / p.width, (c.clientHeight - 48) / p.height))
    }},
    { label: 'Actual size', action: () => { setZoom(1); closeMenu() } },
    null,
    { label: 'Rotate 90° clockwise', action: rotatePage },
  ]

  // In edit mode show span highlight boxes so the user knows what's clickable
  const spanOverlays = isEditMode && spans.map((s, i) => {
    const { x, y } = pdfToCanvas(s.x, s.y)
    // Character width varies by font size: large display fonts ~0.58em,
    // small body text ~0.42em. Interpolate between them, then cap at page width.
    const PAD_X  = 3
    const PAD_Y  = 3
    const cwRatio = s.fontSize >= 20
      ? 0.56
      : s.fontSize >= 12
        ? 0.42 + (s.fontSize - 12) * (0.56 - 0.42) / 8
        : 0.42
    const charW  = s.fontSize * cwRatio * zoom
    const pageW  = (pageViewportRef.current?.width ?? 612) * zoom
    const rawW   = s.text.length * charW + PAD_X * 2
    const w      = Math.min(rawW, pageW - s.x * zoom)
    const ascent  = s.fontSize * 1.05 * zoom  // above baseline
    const descent = s.fontSize * 0.35 * zoom  // below baseline
    const h       = ascent + descent + PAD_Y * 2
    // Total rotation = page rotation (CW, from viewport) + text rotation (CCW, from Tm)
    // CSS positive = CW, PDF atan2 positive = CCW, so signs differ.
    const pageRot = pageRotationRef.current  // 0 | 90 | 180 | 270 (CW degrees)
    const textRot = s.rotation ?? 0          // CCW degrees from Tm matrix
    const totalRot = pageRot - textRot       // net CW rotation for CSS
    return (
      <div key={i} style={{
        position:        'absolute',
        left:            offset.current.x + x - PAD_X,
        top:             offset.current.y + y - ascent - PAD_Y,
        width:           w,
        height:          h,
        border:          '1.5px solid rgba(99,179,237,0.7)',
        background:      'rgba(99,179,237,0.08)',
        borderRadius:    3,
        cursor:          'text',
        pointerEvents:   'none',
        boxSizing:       'border-box',
        transformOrigin: `${PAD_X}px ${ascent + PAD_Y}px`,
        transform:       totalRot !== 0 ? `rotate(${totalRot}deg)` : undefined,
      }} />
    )
  })

  return (
    <div
      ref={containerRef}
      onMouseDown={onMouseDown}
      onMouseMove={onMouseMove}
      onMouseUp={onMouseUp}
      onMouseLeave={() => { dragState.current = null; setIsDragging(false); setOverSpan(false) }}
      onContextMenu={handleContextMenu}
      style={{
        flex: 1, overflow: 'hidden', background: '#222', position: 'relative',
        cursor: isDragging ? 'grabbing' : (overSpan ? 'text' : 'grab'),
        userSelect: 'none',
      }}
    >
      {loading && <div style={{ position: 'absolute', top: '50%', left: '50%', transform: 'translate(-50%,-50%)', color: '#aaa', fontSize: 13 }}>Loading PDF…</div>}
      {error   && <div style={{ position: 'absolute', top: '50%', left: '50%', transform: 'translate(-50%,-50%)', color: '#f87171', fontSize: 13, maxWidth: 400, textAlign: 'center' }}>{error}</div>}

      <div style={{ position: 'absolute', left: offset.current.x, top: offset.current.y, lineHeight: 0 }}>
        <canvas ref={canvasRef} style={{ boxShadow: '0 4px 24px rgba(0,0,0,0.5)', borderRadius: 2, display: 'block' }} />
      </div>

      {/* Span highlight boxes */}
      {spanOverlays}

      {/* Edit mode status */}
      {isEditMode && spansLoading && (
        <div style={{ position: 'absolute', bottom: 8, left: '50%', transform: 'translateX(-50%)', background: 'rgba(0,0,0,0.7)', color: '#aaa', fontSize: 11, padding: '3px 10px', borderRadius: 4 }}>
          Reading text…
        </div>
      )}
      {isEditMode && !spansLoading && spans.length === 0 && !loading && (
        <div style={{ position: 'absolute', bottom: 8, left: '50%', transform: 'translateX(-50%)', background: 'rgba(0,0,0,0.7)', color: '#aaa', fontSize: 11, padding: '3px 10px', borderRadius: 4 }}>
          No editable text found on this page
        </div>
      )}
      {editError && (
        <div style={{ position: 'absolute', bottom: 8, left: '50%', transform: 'translateX(-50%)', background: 'rgba(239,68,68,0.9)', color: '#fff', fontSize: 11, padding: '3px 10px', borderRadius: 4 }}>
          {editError}
        </div>
      )}

      {/* Inline text editor overlay */}
      {editTarget && (
        <div style={{
          position: 'absolute',
          left: editTarget.left,
          top:  editTarget.top,
          zIndex: 200,
        }}>
          <input
            ref={editInputRef}
            value={editTarget.inputValue}
            onChange={e => setEditTarget(t => ({ ...t, inputValue: e.target.value }))}
            onKeyDown={e => {
              if (e.key === 'Enter')  { e.preventDefault(); commitEdit() }
              if (e.key === 'Escape') cancelEdit()
            }}
            onBlur={commitEdit}
            disabled={editTarget.saving}
            style={{
              fontSize:    editTarget.span.fontSize * zoom,
              fontFamily:  'Helvetica, Arial, sans-serif',
              padding:     '0 2px',
              border:      '1px solid var(--accent)',
              borderRadius: 2,
              background:  'rgba(255,255,248,0.95)',
              color:       '#111',
              outline:     'none',
              minWidth:    40,
              width:       Math.max(editTarget.span.text.length * editTarget.span.fontSize * 0.6 * zoom + 20, 60),
            }}
          />
          <div style={{ fontSize: 10, color: '#aaa', marginTop: 2, whiteSpace: 'nowrap' }}>
            Enter to save · Esc to cancel
          </div>
        </div>
      )}

      {menu && (
        <>
          <div style={{ position: 'fixed', inset: 0, zIndex: 499 }} onMouseDown={closeMenu} />
          <div ref={menuRef} style={{
            position: 'fixed', top: (menuPos ?? menu).y, left: (menuPos ?? menu).x,
            zIndex: 500, opacity: menuPos ? 1 : 0,
            background: 'var(--bg-panel)', border: '1px solid var(--border)',
            borderRadius: 8, padding: 4, minWidth: 170, boxShadow: 'var(--shadow)',
          }}>
            {menuItems.map((item, i) =>
              item === null
                ? <div key={i} style={{ height: 1, background: 'var(--border)', margin: '3px 6px' }} />
                : <button key={i} onMouseDown={e => e.stopPropagation()} onClick={item.action}
                    style={{ display: 'flex', width: '100%', padding: '6px 10px', borderRadius: 5, fontSize: 12, justifyContent: 'flex-start' }}>
                    {item.label}
                  </button>
            )}
          </div>
        </>
      )}
    </div>
  )
}
