import { useState, useEffect, useCallback } from 'react'
import { useAppStore } from '../../stores/appStore'
import { useOperation } from '../../hooks/useOperation'
import { ListBookmarks, AddBookmark, RemoveBookmark } from '../../wails.js'
import { Bookmark, BookmarkPlus, Trash2, ChevronRight, ChevronDown } from 'lucide-react'

function BookmarkItem({ bm, depth = 0, currentPage, onNavigate, onDelete }) {
  const [open, setOpen] = useState(depth === 0)
  const [hovered, setHovered] = useState(false)
  const hasKids = bm.kids && bm.kids.length > 0
  const isActive = bm.page === currentPage

  const bg = isActive
    ? 'rgba(99,179,237,0.12)'
    : hovered
      ? 'var(--bg-toolbar)'
      : 'transparent'

  return (
    <div>
      <div
        style={{
          display: 'flex',
          alignItems: 'center',
          gap: 4,
          paddingLeft: 8 + depth * 14,
          paddingRight: 6,
          paddingTop: 4,
          paddingBottom: 4,
          borderRadius: 5,
          cursor: 'pointer',
          background: bg,
          color: isActive ? 'var(--accent)' : 'var(--text-primary)',
        }}
        onMouseEnter={() => setHovered(true)}
        onMouseLeave={() => setHovered(false)}
      >
        {/* expand/collapse toggle */}
        <span
          style={{ width: 14, flexShrink: 0, color: 'var(--text-muted)', lineHeight: 0 }}
          onClick={e => { e.stopPropagation(); if (hasKids) setOpen(o => !o) }}
        >
          {hasKids
            ? (open ? <ChevronDown size={11} /> : <ChevronRight size={11} />)
            : <span style={{ display: 'inline-block', width: 11 }} />}
        </span>

        {/* label */}
        <span
          onClick={() => onNavigate(bm.page)}
          style={{
            flex: 1,
            fontSize: 12,
            fontWeight: bm.bold ? 600 : 400,
            fontStyle: bm.italic ? 'italic' : 'normal',
            overflow: 'hidden',
            textOverflow: 'ellipsis',
            whiteSpace: 'nowrap',
            userSelect: 'none',
          }}
          title={bm.title}
        >
          {bm.title}
        </span>

        {/* page badge */}
        <span style={{ fontSize: 10, color: 'var(--text-muted)', flexShrink: 0 }}>
          {bm.page}
        </span>

        {/* delete — only for top-level for now */}
        {depth === 0 && (
          <button
            onClick={e => { e.stopPropagation(); onDelete(bm) }}
            title="Remove bookmark"
            style={{ padding: 2, marginLeft: 2, borderRadius: 3, lineHeight: 0, color: 'var(--text-muted)', opacity: 0.5 }}
            onMouseEnter={e => e.currentTarget.style.opacity = '1'}
            onMouseLeave={e => e.currentTarget.style.opacity = '0.5'}
          >
            <Trash2 size={10} />
          </button>
        )}
      </div>

      {hasKids && open && (
        <div>
          {bm.kids.map((kid, i) => (
            <BookmarkItem
              key={i}
              bm={kid}
              depth={depth + 1}
              currentPage={currentPage}
              onNavigate={onNavigate}
              onDelete={onDelete}
            />
          ))}
        </div>
      )}
    </div>
  )
}

export default function BookmarksPanel() {
  const { document, currentPage, setCurrentPage, startOperation, finishOperation, failOperation } = useAppStore()
  const { docPath } = useOperation()

  const [bookmarks, setBookmarks] = useState([])
  const [loading,   setLoading]   = useState(false)
  const [newTitle,  setNewTitle]  = useState('')
  const [addOpen,   setAddOpen]   = useState(false)

  const reload = useCallback(async (path) => {
    if (!path) return
    setLoading(true)
    try {
      const result = await ListBookmarks(path)
      // ListBookmarks returns either [] or a bookmark array (never an error object)
      setBookmarks(Array.isArray(result) ? result : [])
    } catch (e) {
      console.warn('ListBookmarks error:', e)
      setBookmarks([])
    } finally {
      setLoading(false)
    }
  }, [])

  // Reload when doc path changes (e.g. new file opened)
  useEffect(() => { reload(docPath) }, [docPath, reload])

  const handleNavigate = (page) => setCurrentPage(page)


  const handleAdd = async () => {
    if (!docPath) return
    const title = newTitle.trim() || `Page ${currentPage}`
    const { TempPath, TempPathB, OpenDocument } = await import('../../wails.js')
    const sourceName = docPath.split(/[\/]/).pop()
    const slot = useAppStore.getState().document?.tempSlot
    const writeToB = slot !== 'b'
    const tempPath = writeToB ? await TempPathB(sourceName) : await TempPath(sourceName)
    startOperation('Add Bookmark')
    try {
      const result = await AddBookmark(docPath, tempPath, title, currentPage)
      if (result?.error) { failOperation(result.error); return }
      if (result?.debug) console.log('[AddBookmark]', result.debug)
      const doc = await OpenDocument(tempPath)
      if (doc?.error) { failOperation(doc.error); return }
      // Save the page before setDocument resets it to 1
      const savedPage = useAppStore.getState().currentPage
      useAppStore.getState().setDocument({
        ...doc,
        path: tempPath,
        originalPath: useAppStore.getState().document?.originalPath ?? docPath,
        tempSlot: writeToB ? 'b' : 'a',
      })
      useAppStore.getState().setCurrentPage(savedPage)
      finishOperation('Bookmark added')
      setNewTitle('')
      setAddOpen(false)
      await reload(tempPath)
    } catch (e) {
      failOperation(e.message ?? String(e))
    }
  }

  const handleDelete = async (bm) => {
    if (!docPath) return
    const { TempPath, TempPathB, OpenDocument } = await import('../../wails.js')
    const sourceName = docPath.split(/[\/]/).pop()
    const slot = useAppStore.getState().document?.tempSlot
    const writeToB = slot !== 'b'
    const tempPath = writeToB ? await TempPathB(sourceName) : await TempPath(sourceName)
    startOperation('Remove Bookmark')
    try {
      const result = await RemoveBookmark(docPath, tempPath, bm.title, bm.page)
      if (result?.error) { failOperation(result.error); return }
      const doc = await OpenDocument(tempPath)
      if (doc?.error) { failOperation(doc.error); return }
      const savedPage = useAppStore.getState().currentPage
      useAppStore.getState().setDocument({
        ...doc,
        path: tempPath,
        originalPath: useAppStore.getState().document?.originalPath ?? docPath,
        tempSlot: writeToB ? 'b' : 'a',
      })
      useAppStore.getState().setCurrentPage(savedPage)
      finishOperation('Bookmark removed')
      await reload(tempPath)
    } catch (e) {
      failOperation(e.message ?? String(e))
    }
  }

  if (!document) return null

  return (
    <div style={{
      width: 240,
      background: 'var(--bg-panel)',
      borderRight: '1px solid var(--border)',
      display: 'flex',
      flexDirection: 'column',
      flexShrink: 0,
    }}>
      {/* header */}
      <div style={{
        padding: '8px 12px',
        borderBottom: '1px solid var(--border)',
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'space-between',
        flexShrink: 0,
      }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: 7, fontSize: 12, fontWeight: 600 }}>
          <Bookmark size={13} style={{ color: 'var(--accent)' }} />
          Bookmarks
        </div>
        <button
          onClick={() => setAddOpen(o => !o)}
          title="Add bookmark for current page"
          style={{
            padding: 4,
            borderRadius: 5,
            lineHeight: 0,
            color: addOpen ? 'var(--accent)' : 'var(--text-muted)',
            background: addOpen ? 'rgba(99,179,237,0.12)' : 'transparent',
          }}
          onMouseEnter={e => { if (!addOpen) e.currentTarget.style.color = 'var(--text-primary)' }}
          onMouseLeave={e => { if (!addOpen) e.currentTarget.style.color = 'var(--text-muted)' }}
        >
          <BookmarkPlus size={14} />
        </button>
      </div>

      {/* add form */}
      {addOpen && (
        <div style={{
          padding: '8px 10px',
          borderBottom: '1px solid var(--border)',
          display: 'flex',
          gap: 6,
          background: 'var(--bg-toolbar)',
          flexShrink: 0,
        }}>
          <input
            autoFocus
            value={newTitle}
            onChange={e => setNewTitle(e.target.value)}
            onKeyDown={e => { if (e.key === 'Enter') handleAdd(); if (e.key === 'Escape') setAddOpen(false) }}
            placeholder={`Page ${currentPage}`}
            style={{
              flex: 1,
              background: 'var(--bg-base)',
              border: '1px solid var(--border)',
              color: 'var(--text-primary)',
              borderRadius: 5,
              padding: '4px 7px',
              fontSize: 12,
            }}
          />
          <button className="primary" onClick={handleAdd} style={{ padding: '4px 10px', fontSize: 11 }}>
            Add
          </button>
        </div>
      )}

      {/* bookmark tree */}
      <div style={{ flex: 1, overflow: 'auto', padding: '4px 4px' }}>
        {loading ? (
          <div style={{ padding: 16, fontSize: 11, color: 'var(--text-muted)', textAlign: 'center' }}>
            Loading…
          </div>
        ) : bookmarks.length === 0 ? (
          <div style={{ padding: 16, fontSize: 11, color: 'var(--text-muted)', textAlign: 'center', lineHeight: 1.5 }}>
            No bookmarks yet.<br />
            Click <BookmarkPlus size={11} style={{ display: 'inline', verticalAlign: 'middle' }} /> to add one for the current page.
          </div>
        ) : (
          bookmarks.map((bm, i) => (
            <BookmarkItem
              key={i}
              bm={bm}
              currentPage={currentPage}
              onNavigate={handleNavigate}
              onDelete={handleDelete}
            />
          ))
        )}
      </div>
    </div>
  )
}
