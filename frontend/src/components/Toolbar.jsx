import { useAppStore } from '../stores/appStore'
import { OpenFileDialog, OpenDocument, SaveFileDialog, SaveDocument } from '../wails.js'
import { zoomIn, zoomOut, ZOOM_LEVELS } from '../zoomLevels'
import {
  FolderOpen, Save, ChevronLeft, ChevronRight,
  ZoomIn, ZoomOut, Type, Shield, Layers,
  PanelLeft, Maximize2, RefreshCw, ArrowRightLeft
} from 'lucide-react'

export default function Toolbar() {
  const {
    document, currentPage, zoom, toolbarMode,
    setCurrentPage, setZoom, setToolbarMode,
    setSidebarOpen, sidebarOpen,
    openTab, setDocument, addRecentFile,
  } = useAppStore()

  const totalPages = document?.pageCount || 0

  const handleOpen = async () => {
    const path = await OpenFileDialog()
    if (!path) return
    const result = await OpenDocument(path)
    if (result?.error) {
      if (result.error.startsWith('encrypted:')) {
        alert('🔒 This PDF is password-protected.\n\nOpen it, then use the Security panel → Remove Protection to decrypt it before editing.')
      } else {
        alert(result.error)
      }
      return
    }
    openTab(result)
    addRecentFile(path)
  }

  const handleSave = async () => {
    if (!document) return
    const originalPath = document.originalPath ?? document.path
    if (document.path === originalPath) { return handleSaveAs() }  // nothing to overwrite yet
    const result = await SaveDocument(document.path, originalPath)
    if (result?.error) { alert(result.error); return }
    const doc = await OpenDocument(originalPath)
    if (!doc?.error) { setDocument({ ...doc, path: originalPath, originalPath, tempSlot: document.tempSlot }) }
  }

  const handleSaveAs = async () => {
    if (!document) return
    const baseName   = (document.originalPath ?? document.path).split(/[\\/]/).pop()
    const outputPath = await SaveFileDialog('Save As', baseName)
    if (!outputPath) return
    const result = await SaveDocument(document.path, outputPath)
    if (result?.error) { alert(result.error); return }
    // Reload from saved location and update the tab to reflect the new canonical path
    const doc = await OpenDocument(outputPath)
    if (!doc?.error) { setDocument({ ...doc, originalPath: outputPath }); addRecentFile(outputPath) }
  }

  const handleReload = async () => {
    if (!document) return
    const reloadPath = document.originalPath ?? document.path
    const result = await OpenDocument(reloadPath)
    if (result?.error) { alert(result.error); return }
    setDocument({ ...result, originalPath: reloadPath })
  }

  const modes = [
    { id: 'view',     label: 'View',     icon: Maximize2 },
    { id: 'pages',    label: 'Pages',    icon: Layers },
    { id: 'edit',     label: 'Edit',     icon: Type },
    { id: 'security', label: 'Security', icon: Shield },
    { id: 'convert',  label: 'Convert',  icon: ArrowRightLeft },
  ]

  return (
    <div style={{
      background: 'var(--bg-toolbar)',
      borderBottom: '1px solid var(--border)',
      display: 'flex',
      alignItems: 'center',
      gap: 2,
      padding: '4px 8px',
      flexShrink: 0,
    }}>
      <button onClick={handleOpen} title="Open PDF">
        <FolderOpen size={15} /> Open
      </button>
      {document && (
        <>
          <button onClick={handleSave} title="Save (Ctrl+S)">
            <Save size={14} /> Save
          </button>
          <button onClick={handleSaveAs} title="Save As (Ctrl+Shift+S)">
            <Save size={14} /> Save As
          </button>
          <button onClick={handleReload} title="Reload from disk">
            <RefreshCw size={13} />
          </button>
        </>
      )}
      <div className="divider" />

      {modes.map(m => (
        <button
          key={m.id}
          onClick={() => setToolbarMode(m.id)}
          style={{
            background: toolbarMode === m.id ? 'var(--bg-active)' : undefined,
            color:      toolbarMode === m.id ? '#fff' : undefined,
          }}
        >
          <m.icon size={14} /> {m.label}
        </button>
      ))}

      <div style={{ flex: 1 }} />

      {document && (
        <>
          <button onClick={() => setCurrentPage(Math.max(1, currentPage - 1))} disabled={currentPage <= 1}>
            <ChevronLeft size={14} />
          </button>
          <span style={{ color: 'var(--text-muted)', fontSize: 12, whiteSpace: 'nowrap' }}>
            <input
              type="number"
              min={1} max={totalPages}
              value={currentPage}
              onChange={e => setCurrentPage(Math.max(1, Math.min(totalPages, +e.target.value)))}
              style={{ width: 40, textAlign: 'center', padding: '3px 4px' }}
            />
            {' '}/ {totalPages}
          </span>
          <button onClick={() => setCurrentPage(Math.min(totalPages, currentPage + 1))} disabled={currentPage >= totalPages}>
            <ChevronRight size={14} />
          </button>
          <div className="divider" />
          <button onClick={() => setZoom(zoomOut(zoom))} title="Zoom out"><ZoomOut size={14} /></button>
          <select
            value=""
            onChange={e => { if (e.target.value) setZoom(+e.target.value) }}
            title="Zoom level"
            style={{ fontSize: 12, width: 62, textAlign: 'center', color: 'var(--text-muted)', background: 'var(--bg-base)', border: '1px solid var(--border)', borderRadius: 4, padding: '2px 0' }}
          >
            <option value="" disabled hidden>{Math.round(zoom * 100)}%</option>
            {ZOOM_LEVELS.map(z => (
              <option key={z} value={z}>{Math.round(z * 100)}%</option>
            ))}
          </select>
          <button onClick={() => setZoom(zoomIn(zoom))} title="Zoom in"><ZoomIn size={14} /></button>
          <div className="divider" />
          <button onClick={() => setSidebarOpen(!sidebarOpen)} title="Toggle sidebar">
            <PanelLeft size={14} />
          </button>
        </>
      )}
    </div>
  )
}
