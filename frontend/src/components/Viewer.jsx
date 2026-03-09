import { useEffect, useRef, useState, useCallback, useMemo } from 'react'
import * as pdfjsLib from 'pdfjs-dist'
import workerUrl from 'pdfjs-dist/build/pdf.worker.min.mjs?url'
import { useAppStore } from '../stores/appStore'
import { useOperation } from '../hooks/useOperation'
import { ReadFileBytes, RotatePages, ExtractPageText, ReplaceSpanText, EditMergedSpans, GetFormFields, FillFormFields } from '../wails.js'
import { zoomIn, zoomOut, clampZoom } from '../zoomLevels'

pdfjsLib.GlobalWorkerOptions.workerSrc = workerUrl

const DRAG_THRESHOLD = 4

// Merge raw spans into display-only spans for overlay boxes.
// This does NOT change opStart/opEnd — merged spans are never sent to the editor.
function mergeSpansForDisplay(rawSpans) {
  if (!rawSpans || rawSpans.length === 0) return []

  const merged = []
  let cur = { ...rawSpans[0], subSpans: [rawSpans[0]] }

  for (let i = 1; i < rawSpans.length; i++) {
    const next = rawSpans[i]

    const sameLine = Math.abs(cur.y - next.y) < cur.fontSize * 0.4
    const sameFont = cur.fontName === next.fontName
    const sameSize = Math.abs(cur.fontSize - next.fontSize) < 1.0
    const sameRot  = Math.abs((cur.rotation ?? 0) - (next.rotation ?? 0)) < 1.0

    const curEndX = (cur.width && cur.width > 0)
      ? cur.x + cur.width
      : cur.x + (cur.text?.length ?? 0) * cur.fontSize * 0.5
    const gap = next.x - curEndX
    const maxGap = cur.fontSize * 2.0
    const minGap = -cur.fontSize * 0.5
    const closeEnough = gap < maxGap && gap > minGap
    const wideRange = sameLine && next.x > cur.x && (next.x - cur.x) < cur.fontSize * 40

    if (sameFont && sameSize && sameRot && sameLine && (closeEnough || wideRange)) {
      const needsSpace = gap > cur.fontSize * 0.15
      cur.text = needsSpace ? cur.text + ' ' + next.text : cur.text + next.text
      cur.subSpans.push(next)
      if (next.width && next.width > 0) {
        cur.width = (next.x + next.width) - cur.x
      } else if (cur.width > 0) {
        const nextEndX = next.x + (next.text?.length ?? 0) * next.fontSize * 0.5
        cur.width = nextEndX - cur.x
      }
    } else {
      merged.push(cur)
      cur = { ...next, subSpans: [next] }
    }
  }
  merged.push(cur)
  return merged
}

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
  const [spans, setSpans]           = useState([])       // raw unmerged spans (safe for editing)
  const [spansLoading, setSpansLoading] = useState(false)
  const [editTarget, setEditTarget] = useState(null)     // { span, canvasX, canvasY, inputValue }
  const [editError, setEditError]   = useState(null)
  const inputValueRef = useRef('')   // always-current input value (avoids closure staleness)

  // Form fields state (loaded automatically when PDF has AcroForm)
  const [formFields, setFormFields] = useState([])     // all fields across all pages
  const [formValues, setFormValues] = useState({})      // field name → current value (includes unsaved keystrokes)
  const [formSaving, setFormSaving] = useState(false)
  const formSavingRef = useRef(false)  // synchronous guard — React state is async
  const savedFormValues = useRef({})   // values last written to PDF — used to detect real changes

  // Merged spans for display overlays only — NOT for editing.
  // Raw spans preserve correct opStart/opEnd for safe stream editing.
  const displaySpans = useMemo(() => mergeSpansForDisplay(spans), [spans])

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
    const vp = pageViewportRef.current
    if (!container || !vp) return
    offset.current = {
      x: Math.max(0, (container.clientWidth  - vp.width  * zoom) / 2),
      y: Math.max(0, (container.clientHeight - vp.height * zoom) / 2),
    }
    forceRender(n => n + 1)
  }, [zoom])

  // Zoom anchored to the center of the visible viewport
  const zoomToCenter = useCallback((newZoom) => {
    const container = containerRef.current
    if (!container) { setZoom(newZoom); return }
    const oldZoom = zoomRef.current
    const scale = newZoom / oldZoom
    const cx = container.clientWidth  / 2
    const cy = container.clientHeight / 2
    offset.current = {
      x: cx - (cx - offset.current.x) * scale,
      y: cy - (cy - offset.current.y) * scale,
    }
    setZoom(newZoom)
    forceRender(n => n + 1)
  }, [setZoom])

  // Recentre canvas when the container is resized (e.g. panel opening/closing)
  useEffect(() => {
    const container = containerRef.current
    if (!container) return
    const ro = new ResizeObserver(() => {
      const vp = pageViewportRef.current
      if (!vp) return
      const cw = container.clientWidth
      const ch = container.clientHeight
      const pw = vp.width  * zoomRef.current
      const ph = vp.height * zoomRef.current
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

        // Cap backing store to browser canvas limit
        const MAX_DIM = 16384
        let renderScale = zoom * dpr
        const testVp = page.getViewport({ scale: renderScale })
        if (testVp.width > MAX_DIM || testVp.height > MAX_DIM) {
          renderScale *= Math.min(MAX_DIM / testVp.width, MAX_DIM / testVp.height)
        }

        const viewport = page.getViewport({ scale: renderScale })
        const canvas   = canvasRef.current
        if (!canvas) return
        canvas.width  = viewport.width
        canvas.height = viewport.height

        const vp1 = page.getViewport({ scale: 1 })
        // CSS size MUST be pageW*zoom × pageH*zoom to match form field coordinate math.
        // Don't use viewport/dpr which breaks when renderScale is capped.
        canvas.style.width  = `${vp1.width * zoom}px`
        canvas.style.height = `${vp1.height * zoom}px`
        pageViewportRef.current = vp1
        pageRotationRef.current = vp1.rotation ?? 0

        const task = page.render({
          canvasContext: canvas.getContext('2d'),
          viewport,
          annotationMode: 0,
        })
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

  // ── Load form fields when document opens ──────────────────────────────────
  useEffect(() => {
    if (!document?.path) { setFormFields([]); setFormValues({}); return }
    let cancelled = false

    async function loadFields() {
      try {
        const fields = await GetFormFields(document.path)
        if (cancelled) return
        if (fields && fields.length > 0) {
          setFormFields(fields)
          const vals = {}
          for (const f of fields) {
            vals[f.name] = f.value ?? ''
          }
          setFormValues(vals)
          savedFormValues.current = { ...vals }
        } else {
          setFormFields([])
          setFormValues({})
          savedFormValues.current = {}
        }
      } catch (e) {
        // Not an error if PDF has no forms — just means no AcroForm
        if (!cancelled) { setFormFields([]); setFormValues({}) }
      }
    }
    loadFields()
    return () => { cancelled = true }
  }, [document?.path])

  // Fields for the current page
  const pageFormFields = useMemo(
    () => formFields.filter(f => f.pageNum === currentPage),
    [formFields, currentPage]
  )

  // Save a single form field value
  const saveFormField = useCallback(async (fieldName, newValue) => {
    if (!docPath || formSavingRef.current) return

    // Skip save if value hasn't changed from what's already in the PDF
    if ((savedFormValues.current[fieldName] ?? '') === (newValue ?? '')) return

    formSavingRef.current = true
    setFormSaving(true)

    const updatedValues = { ...formValues, [fieldName]: newValue }
    setFormValues(updatedValues)

    try {
      await run('Fill form field', async (out) => {
        // Save ALL current values together so no fields get lost
        const result = await FillFormFields(docPath, out, updatedValues)
        if (result?.error) throw new Error(result.error)
        return result
      })
      // Reload fields from the new file
      const freshPath = useAppStore.getState().document?.path
      if (freshPath) {
        try {
          const fields = await GetFormFields(freshPath)
          if (fields) {
            setFormFields(fields)
            // Merge: keep our local values, overlay with what the PDF now says
            const vals = { ...updatedValues }
            for (const f of fields) {
              if (f.value) vals[f.name] = f.value
            }
            setFormValues(vals)
            savedFormValues.current = { ...vals }
          }
        } catch (e) { /* ignore reload errors */ }
      }
    } catch (e) {
      console.error('Form save error:', e)
    } finally {
      formSavingRef.current = false
      setFormSaving(false)
    }
  }, [docPath, formValues, run])

  // Focus the edit input when it appears
  useEffect(() => {
    if (editTarget) setTimeout(() => editInputRef.current?.focus(), 30)
  }, [editTarget])

  // ── Span hit-testing ─────────────────────────────────────────────────────
  // For rotated spans we rotate the query point into the span's local frame
  // (un-rotate around the span's origin) before doing a simple AABB check.
  const hitTestSpan = useCallback((pdfX, pdfY, s, pad = 4) => {
    // Use backend-computed width if available, otherwise estimate
    let spanW
    if (s.width && s.width > 0) {
      spanW = s.width
    } else {
      const cwRatio = s.fontSize >= 20 ? 0.56 : s.fontSize >= 12
        ? 0.42 + (s.fontSize - 12) * (0.56 - 0.42) / 8 : 0.42
      spanW = s.text.length * s.fontSize * cwRatio
    }
    const ascent  = s.fontSize * 0.85
    const descent = s.fontSize * 0.15
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

    // Hit-test against merged display spans — these have the full text
    const hit = displaySpans.find(s => hitTestSpan(pdfX, pdfY, s))

    if (!hit) {
      setEditTarget(null)
      return
    }

    const { x: canX, y: canY } = pdfToCanvas(hit.x, hit.y)
    inputValueRef.current = hit.text
    setEditTarget({
      span:       hit,
      left:       canX - 3,
      top:        canY - hit.fontSize * 0.85 * zoom - 1,
      inputValue: hit.text,
    })
    setEditError(null)
  }, [isEditMode, displaySpans, canvasToPdf, pdfToCanvas, zoom])

  const commitEdit = useCallback(async () => {
    if (!editTarget) return
    const { span } = editTarget
    // Read from ref to avoid stale closure — the ref is updated on every keystroke
    const inputValue = inputValueRef.current
    if (inputValue === span.text) { setEditTarget(null); return }

    setEditTarget(t => ({ ...t, saving: true }))
    try {
      await run('Edit text', async (out) => {
        let result

        // Always use block-level rewriting for correct add/delete behavior
        const rawSpans = span.subSpans ?? [span]
        const subSpanInfos = rawSpans.map(s => ({
          streamIndex: s.streamIndex,
          opStart:     s.opStart,
          opEnd:       s.opEnd,
          text:        s.text,
          fontName:    s.fontName,
          blockStart:  s.blockStart,
          blockEnd:    s.blockEnd,
          tfSize:      s.tfSize,
          tmA:         s.tmA,
          tmB:         s.tmB,
          tmC:         s.tmC,
          tmD:         s.tmD,
          tmE:         s.tmE,
          tmF:         s.tmF,
        }))
        result = await EditMergedSpans(
          docPath, out, span.pageNum,
          subSpanInfos, span.text, inputValue
        )

        if (result.error) throw new Error(result.error)
        if (result.truncated) setEditError(`Text truncated to: "${result.actualText}"`)
        else if (result.padded) setEditError(null)
        else setEditError(null)
        return result
      })
      // Re-extract spans from the NEW document path (run() changed it via setDocument)
      const freshPath = useAppStore.getState().document?.path
      if (freshPath) {
        const fresh = await ExtractPageText(freshPath, currentPage)
        setSpans(fresh ?? [])
      }
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
    // Don't start drag if clicking on a form field input
    const tag = e.target.tagName
    if (tag === 'INPUT' || tag === 'SELECT' || tag === 'TEXTAREA') return

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
      const overSpan = displaySpans.some(s => hitTestSpan(pdfX, pdfY, s))
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
  }, [isEditMode, displaySpans, canvasToPdf])

  const onMouseMove = useCallback((e) => {
    // Hover detection for cursor — always runs
    if (isEditMode && canvasRef.current) {
      const rect = canvasRef.current.getBoundingClientRect()
      const cx = e.clientX - rect.left
      const cy = e.clientY - rect.top
      const { x: pdfX, y: pdfY } = canvasToPdf(cx, cy)
      const over = displaySpans.some(s => hitTestSpan(pdfX, pdfY, s))
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
  }, [isEditMode, displaySpans, canvasToPdf])

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
    { label: 'Zoom in',   action: () => { zoomToCenter(zoomIn(zoom)); closeMenu() } },
    { label: 'Zoom out',  action: () => { zoomToCenter(zoomOut(zoom)); closeMenu() } },
    { label: 'Fit to page', action: () => {
      closeMenu()
      const c = containerRef.current
      const vp = pageViewportRef.current
      if (!c || !vp) return
      zoomToCenter(clampZoom(Math.min((c.clientWidth - 48) / vp.width, (c.clientHeight - 48) / vp.height)))
    }},
    { label: 'Actual size', action: () => { zoomToCenter(1); closeMenu() } },
    null,
    { label: 'Rotate 90° clockwise', action: rotatePage },
  ]

  // In edit mode show span highlight boxes — uses merged displaySpans for clean visuals
  const spanOverlays = isEditMode && displaySpans.map((s, i) => {
    const { x, y } = pdfToCanvas(s.x, s.y)
    const PAD_X  = 3
    const PAD_Y  = 1

    // Width: use backend width or estimate from chars
    let w
    if (s.width && s.width > 0) {
      const { x: xEnd } = pdfToCanvas(s.x + s.width, s.y)
      w = Math.abs(xEnd - x) + PAD_X * 2
    } else {
      const cwRatio = s.fontSize >= 20
        ? 0.56
        : s.fontSize >= 12
          ? 0.42 + (s.fontSize - 12) * (0.56 - 0.42) / 8
          : 0.42
      const charW = s.fontSize * cwRatio * zoom
      w = s.text.length * charW + PAD_X * 2
    }
    // Clamp to canvas bounds
    const canvasW = canvasRef.current?.offsetWidth ?? ((pageViewportRef.current?.width ?? 612) * zoom)
    if (x + w > canvasW) w = Math.max(20, canvasW - x + PAD_X)

    // Height: tight around the glyph body (reduced from 1.05+0.35 to 0.85+0.15)
    const ascent  = s.fontSize * 0.85 * zoom
    const descent = s.fontSize * 0.15 * zoom
    const h       = ascent + descent + PAD_Y * 2
    // Total rotation = page rotation (CW, from viewport) + text rotation (CCW, from Tm)
    // CSS positive = CW, PDF atan2 positive = CCW, so signs differ.
    const pageRot = pageRotationRef.current  // 0 | 90 | 180 | 270 (CW degrees)
    const textRot = s.rotation ?? 0          // CCW degrees from Tm matrix
    const totalRot = pageRot - textRot       // net CW rotation for CSS
    return (
      <div key={i} style={{
        position:        'absolute',
        left:            x - PAD_X,
        top:             y - ascent - PAD_Y,
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

      <div style={{
        position: 'absolute',
        left: offset.current.x,
        top: offset.current.y,
        width:  pageViewportRef.current ? pageViewportRef.current.width  * zoom : undefined,
        height: pageViewportRef.current ? pageViewportRef.current.height * zoom : undefined,
      }}>
        <canvas ref={canvasRef} style={{ boxShadow: '0 4px 24px rgba(0,0,0,0.5)', borderRadius: 2, display: 'block' }} />

        {/* Span highlight boxes — inside canvas wrapper for correct positioning */}
        {spanOverlays}

        {/* Form field overlays — pdfToCanvas handles rotation, wrapper has explicit dimensions */}
        {pageFormFields.map((field, i) => {
          const bl = pdfToCanvas(field.x, field.y)
          const tr = pdfToCanvas(field.x + field.width, field.y + field.height)
          const left = Math.min(bl.x, tr.x)
          const top  = Math.min(bl.y, tr.y)
          const w    = Math.abs(tr.x - bl.x)
          const h    = Math.abs(tr.y - bl.y)

          if (w < 2 || h < 2) return null

          // Font size: use DA value scaled by zoom if available, else auto-fit
          const fontSize = field.fontSize > 0
            ? field.fontSize * zoom
            : Math.max(6, h * 0.75)

          const fieldStyle = {
            position: 'absolute',
            left,
            top,
            width:  w,
            height: h,
            zIndex: 150,
            boxSizing: 'border-box',
            overflow: 'hidden',
            display: 'flex',
            alignItems: 'center',
          }

          const baseInputStyle = {
            border: '1px solid rgba(59,130,246,0.35)',
            borderRadius: 1,
            background: '#e8f0fe',
            color: '#111',
            fontSize,
            boxSizing: 'border-box',
            outline: 'none',
            margin: 0,
            fontFamily: 'Helvetica, Arial, sans-serif',
          }

          if (field.readOnly) {
            return (
              <div key={field.id || i} style={fieldStyle}>
                <div style={{
                  ...baseInputStyle, width: '100%', height: '100%',
                  background: '#eef2f7',
                  border: '1px solid rgba(150,150,150,0.15)',
                  color: '#555', display: 'flex', alignItems: 'center',
                  padding: '0 2px', overflow: 'hidden', whiteSpace: 'nowrap',
                }}>
                  {formValues[field.name] ?? field.value ?? ''}
                </div>
              </div>
            )
          }

          if (field.type === 'checkbox') {
            const onVal = field.onValue || 'Yes'
            const curVal = formValues[field.name] ?? field.value ?? 'Off'
            const checked = curVal !== 'Off' && curVal !== '' && curVal !== 'No'
            return (
              <div key={field.id || i} style={{
                ...fieldStyle,
                background: checked ? '#3b82f6' : '#e8f0fe',
                display: 'flex',
                alignItems: 'center',
                justifyContent: 'center',
                cursor: 'pointer',
                borderRadius: 1,
              }}
                onClick={() => saveFormField(field.name, checked ? 'Off' : onVal)}
              >
                {checked && (
                  <svg width={w * 0.7} height={h * 0.7} viewBox="0 0 16 16" fill="none">
                    <path d="M3 8.5L6.5 12L13 4" stroke="#fff" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round"/>
                  </svg>
                )}
              </div>
            )
          }

          if (field.type === 'dropdown' || field.type === 'listbox') {
            return (
              <div key={field.id || i} style={fieldStyle}>
                <select
                  value={formValues[field.name] ?? field.value ?? ''}
                  onChange={e => saveFormField(field.name, e.target.value)}
                  style={{ ...baseInputStyle, width: '100%', height: '100%', padding: '0 2px', cursor: 'pointer' }}
                >
                  <option value="">—</option>
                  {(field.options ?? []).map((opt, oi) => (
                    <option key={oi} value={opt}>{opt}</option>
                  ))}
                </select>
              </div>
            )
          }

          // §12.7.4.3 Comb fields: divide into MaxLen equal-width cells
          if (field.comb && field.maxLen > 0) {
            const cellW = w / field.maxLen
            const val = formValues[field.name] ?? ''
            const combFontSize = Math.max(6, Math.min(h * 0.75, cellW * 0.85))
            return (
              <div key={field.id || i} style={{ ...fieldStyle, display: 'flex' }}>
                {Array.from({ length: field.maxLen }, (_, ci) => (
                  <input
                    key={ci}
                    type="text"
                    maxLength={1}
                    value={val[ci] ?? ''}
                    onChange={e => {
                      const ch = e.target.value.slice(-1)
                      const chars = val.split('')
                      while (chars.length < field.maxLen) chars.push('')
                      chars[ci] = ch
                      const newVal = chars.join('').replace(/\s+$/, '')
                      setFormValues(v => ({ ...v, [field.name]: newVal }))
                      if (ch && ci < field.maxLen - 1) {
                        const next = e.target.parentElement?.children[ci + 1]
                        if (next) next.focus()
                      }
                    }}
                    onKeyDown={e => {
                      if (e.key === 'Backspace' && !val[ci] && ci > 0) {
                        e.target.parentElement?.children[ci - 1]?.focus()
                      }
                      if (e.key === 'ArrowLeft' && ci > 0) {
                        e.target.parentElement?.children[ci - 1]?.focus()
                      }
                      if (e.key === 'ArrowRight' && ci < field.maxLen - 1) {
                        e.target.parentElement?.children[ci + 1]?.focus()
                      }
                      if (e.key === 'Tab' && !e.shiftKey && ci < field.maxLen - 1) {
                        e.preventDefault()
                        e.target.parentElement?.children[ci + 1]?.focus()
                      }
                    }}
                    onBlur={(ev) => {
                      const parent = ev.target.parentElement
                      setTimeout(() => {
                        if (!parent?.contains(window.document.activeElement)) {
                          saveFormField(field.name, formValues[field.name] ?? '')
                        }
                      }, 50)
                    }}
                    style={{
                      ...baseInputStyle,
                      width: cellW,
                      height: '100%',
                      fontSize: combFontSize,
                      textAlign: 'center',
                      padding: 0,
                      borderLeft: ci === 0 ? baseInputStyle.border : 'none',
                      borderRadius: ci === 0 ? '1px 0 0 1px' : ci === field.maxLen - 1 ? '0 1px 1px 0' : 0,
                    }}
                  />
                ))}
              </div>
            )
          }

          // §12.7.4.3 Multiline text field (bit 13)
          if (field.multiline) {
            const maxLines = Math.max(1, Math.round(h / (fontSize * 1.3)))
            return (
              <div key={field.id || i} style={fieldStyle}>
                <textarea
                  value={formValues[field.name] ?? ''}
                  onChange={e => setFormValues(v => ({ ...v, [field.name]: e.target.value }))}
                  onBlur={e => saveFormField(field.name, e.target.value)}
                  onKeyDown={e => {
                    if (e.key === 'Enter') {
                      const val = formValues[field.name] ?? ''
                      const lineCount = (val.match(/\n/g) || []).length + 1
                      if (lineCount >= maxLines) {
                        e.preventDefault()
                        e.target.blur()
                      }
                    }
                  }}
                  rows={maxLines}
                  style={{
                    ...baseInputStyle,
                    width: '100%',
                    height: '100%',
                    padding: '2px 3px',
                    lineHeight: (fontSize * 1.3) + 'px',
                    resize: 'none',
                    overflow: 'hidden',
                  }}
                  onFocus={e => {
                    e.target.style.border = '2px solid rgba(59,130,246,0.8)'
                    e.target.style.background = 'rgba(255,255,255,0.95)'
                    e.target.style.boxShadow = '0 0 0 2px rgba(59,130,246,0.2)'
                  }}
                  onBlurCapture={e => {
                    e.target.style.border = baseInputStyle.border
                    e.target.style.background = baseInputStyle.background
                    e.target.style.boxShadow = 'none'
                  }}
                />
              </div>
            )
          }

          // Regular text field
          return (
            <div key={field.id || i} style={fieldStyle}>
              <input
                type="text"
                value={formValues[field.name] ?? ''}
                maxLength={field.maxLen > 0 ? field.maxLen : undefined}
                onChange={e => setFormValues(v => ({ ...v, [field.name]: e.target.value }))}
                onBlur={e => saveFormField(field.name, e.target.value)}
                onKeyDown={e => { if (e.key === 'Enter') { e.preventDefault(); e.target.blur() } }}
                style={{
                  ...baseInputStyle,
                  width: '100%',
                  height: '100%',
                  padding: '0 2px',
                }}
                onFocus={e => {
                  e.target.style.border = '2px solid rgba(59,130,246,0.8)'
                  e.target.style.background = 'rgba(255,255,255,0.95)'
                  e.target.style.boxShadow = '0 0 0 2px rgba(59,130,246,0.2)'
                }}
                onBlurCapture={e => {
                  e.target.style.border = baseInputStyle.border
                  e.target.style.background = baseInputStyle.background
                  e.target.style.boxShadow = 'none'
                }}
              />
            </div>
          )
        })}

        {/* Inline text editor overlay — inside canvas wrapper for correct positioning */}
        {editTarget && (
          <div style={{
            position: 'absolute',
            left: editTarget.left,
            top:  editTarget.top,
            zIndex: 200,
          }}>
            <input
              ref={editInputRef}
              type="text"
              value={editTarget.inputValue}
              onChange={e => { inputValueRef.current = e.target.value; setEditTarget(t => ({ ...t, inputValue: e.target.value })) }}
              onKeyDown={e => {
                if (e.key === 'Enter')  { e.preventDefault(); commitEdit() }
                if (e.key === 'Escape') { e.preventDefault(); cancelEdit() }
              }}
              onBlur={commitEdit}
              disabled={editTarget.saving}
              style={{
                background:  '#fff',
                border:      '2px solid var(--accent)',
                borderRadius: 3,
                padding:     '2px 6px',
                fontSize:    editTarget.span.fontSize * zoom,
                fontFamily:  'Helvetica, Arial, sans-serif',
                color:       '#111',
                outline:     'none',
                minWidth:    40,
                width:       Math.max(
                  (editTarget.span.width && editTarget.span.width > 0)
                    ? editTarget.span.width * zoom + 20
                    : editTarget.span.text.length * editTarget.span.fontSize * 0.6 * zoom + 20,
                  60
                ),
              }}
            />
            <div style={{ fontSize: 10, color: '#aaa', marginTop: 2, whiteSpace: 'nowrap' }}>
              Enter to save · Esc to cancel
            </div>
          </div>
        )}
      </div>

      {/* Edit mode status */}
      {isEditMode && spansLoading && (
        <div style={{ position: 'absolute', bottom: 8, left: '50%', transform: 'translateX(-50%)', background: 'rgba(0,0,0,0.7)', color: '#aaa', fontSize: 11, padding: '3px 10px', borderRadius: 4 }}>
          Reading text…
        </div>
      )}
      {isEditMode && !spansLoading && spans.length === 0 && !loading && (
        <div style={{
          position: 'absolute', bottom: 12, left: '50%', transform: 'translateX(-50%)',
          background: 'rgba(0,0,0,0.85)', color: '#ccc', fontSize: 12,
          padding: '10px 16px', borderRadius: 8, maxWidth: 380, textAlign: 'center',
          lineHeight: 1.5, boxShadow: '0 2px 12px rgba(0,0,0,0.3)',
        }}>
          <div style={{ fontWeight: 600, color: '#e4e4e8', marginBottom: 4 }}>No editable text on this page</div>
          <div style={{ fontSize: 11 }}>
            This PDF may contain images or graphics instead of selectable text.
            If you can see text but can't select it, the text was likely converted to shapes when the file was created.
          </div>
        </div>
      )}
      {editError && (
        <div style={{ position: 'absolute', bottom: 8, left: '50%', transform: 'translateX(-50%)', background: 'rgba(239,68,68,0.9)', color: '#fff', fontSize: 11, padding: '3px 10px', borderRadius: 4 }}>
          {editError}
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
