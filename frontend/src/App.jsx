import { useEffect } from 'react'
import { useAppStore } from './stores/appStore'
import { friendlyError } from './friendlyError'
import Titlebar from './components/Titlebar'
import Toolbar from './components/Toolbar'
import TabBar from './components/TabBar'
import Sidebar from './components/Sidebar'
import Viewer from './components/Viewer'
import WelcomeScreen from './components/WelcomeScreen'
import StatusBar, { OperationModal } from './components/StatusBar'
import PagesPanel from './components/panels/PagesPanel'
import SecurityPanel from './components/panels/SecurityPanel'
import EditPanel from './components/panels/EditPanel'
import ConvertPanel from './components/panels/ConvertPanel'
import BookmarksPanel from './components/panels/BookmarksPanel'

const TOOL_PANELS = {
  view:     BookmarksPanel,
  pages:    PagesPanel,
  security: SecurityPanel,
  edit:     EditPanel,
  convert:  ConvertPanel,
}

export default function App() {
  const document    = useAppStore(s => s.document)
  const activeTabId = useAppStore(s => s.activeTabId)
  const sidebarOpen = useAppStore(s => s.sidebarOpen)
  const toolbarMode = useAppStore(s => s.toolbarMode)

  const { undo, setDocument } = useAppStore(s => ({ undo: s.undo, setDocument: s.setDocument }))

  useEffect(() => {
    const handler = async (e) => {
      const mod = e.ctrlKey || e.metaKey

      // Ctrl+Z — undo
      if (mod && e.key === 'z' && !e.shiftKey) {
        e.preventDefault()
        const snapshot = undo()
        if (!snapshot) return
        const { OpenDocument } = await import('./wails.js')
        const doc = await OpenDocument(snapshot.path)
        if (!doc?.error) setDocument({ ...doc, path: snapshot.path, tempSlot: snapshot.tempSlot })
        return
      }

      // Ctrl+S — save (overwrite original); Ctrl+Shift+S — save as
      if (mod && e.key === 's') {
        e.preventDefault()
        const { document: doc, setDocument: sd, finishOperation, failOperation } = useAppStore.getState()
        if (!doc) return
        const { SaveFileDialog, SaveDocument, OpenDocument } = await import('./wails.js')
        const originalPath = doc.originalPath ?? doc.path

        let destPath
        if (e.shiftKey) {
          // Shift+Ctrl+S → always prompt for new location
          const baseName = originalPath.split(/[\/\\]/).pop()
          destPath = await SaveFileDialog('Save As', baseName)
          if (!destPath) return
        } else {
          // Ctrl+S → save directly to original path
          destPath = originalPath
          // If no edits have been made (working file IS the original), nothing to do
          if (doc.path === originalPath) {
            finishOperation('Saved')
            return
          }
        }

        try {
          const result = await SaveDocument(doc.path, destPath)
          if (result?.error) {
            failOperation(`Save failed: ${friendlyError(result.error)}`)
            return
          }

          // After save, reload from the saved path so doc.path === doc.originalPath (clean state)
          const reloaded = await OpenDocument(destPath)
          if (!reloaded?.error) {
            sd({ ...reloaded, path: destPath, originalPath: destPath, tempSlot: doc.tempSlot })
            finishOperation('Saved')
          }
        } catch (err) {
          failOperation(`Save failed: ${friendlyError(err)}`)
        }
        return
      }

      // Arrow keys — page navigation (skip if focus is in an input/textarea)
      const tag = window.document.activeElement?.tagName
      if (tag === 'INPUT' || tag === 'TEXTAREA') return

      const { document: doc, currentPage, setCurrentPage } = useAppStore.getState()
      if (!doc) return

      if (e.key === 'ArrowRight' || e.key === 'ArrowDown') {
        e.preventDefault()
        if (currentPage < doc.pageCount) setCurrentPage(currentPage + 1)
      } else if (e.key === 'ArrowLeft' || e.key === 'ArrowUp') {
        e.preventDefault()
        if (currentPage > 1) setCurrentPage(currentPage - 1)
      }
    }
    window.addEventListener('keydown', handler)
    return () => window.removeEventListener('keydown', handler)
  }, [undo, setDocument])

  const Panel       = document ? TOOL_PANELS[toolbarMode] : null
  const showSidebar = document && sidebarOpen && !Panel

  return (
    <div style={{ display: 'flex', flexDirection: 'column', height: '100vh', overflow: 'hidden' }}>
      <Titlebar />
      <Toolbar />
      <TabBar />
      <div style={{ display: 'flex', flex: 1, overflow: 'hidden' }}>
        {Panel && <Panel />}
        {showSidebar && <Sidebar />}
        <main style={{ flex: 1, overflow: 'hidden', display: 'flex', flexDirection: 'column' }}>
          {/* key={activeTabId} forces Viewer to fully remount on tab switch,
              resetting all PDF.js state cleanly */}
          {document
            ? <Viewer key={activeTabId} isEditMode={toolbarMode === 'edit'} />
            : <WelcomeScreen />
          }
        </main>
      </div>
      <StatusBar />
    </div>
  )
}
