import { useAppStore } from '../stores/appStore'
import { FileText, Bookmark, MessageSquare } from 'lucide-react'
import { useState } from 'react'

export default function Sidebar() {
  const { document, currentPage, setCurrentPage } = useAppStore()
  const [tab, setTab] = useState('pages')

  if (!document) return null

  return (
    <div style={{
      width: 200,
      background: 'var(--bg-panel)',
      borderRight: '1px solid var(--border)',
      display: 'flex',
      flexDirection: 'column',
      flexShrink: 0,
    }}>
      <div style={{ display: 'flex', borderBottom: '1px solid var(--border)' }}>
        {[
          { id: 'pages', icon: FileText, label: 'Pages' },
          { id: 'bookmarks', icon: Bookmark, label: 'Bookmarks' },
          { id: 'annotations', icon: MessageSquare, label: 'Notes' },
        ].map(t => (
          <button
            key={t.id}
            onClick={() => setTab(t.id)}
            title={t.label}
            style={{
              flex: 1,
              padding: '8px 4px',
              borderRadius: 0,
              borderBottom: tab === t.id ? '2px solid var(--accent)' : '2px solid transparent',
              color: tab === t.id ? 'var(--accent)' : 'var(--text-muted)',
            }}
          >
            <t.icon size={14} />
          </button>
        ))}
      </div>

      <div style={{ flex: 1, overflow: 'auto', padding: 8 }}>
        {tab === 'pages' && Array.from({ length: document.pageCount }, (_, i) => i + 1).map(n => (
          <div
            key={n}
            onClick={() => setCurrentPage(n)}
            style={{
              padding: '6px 8px',
              borderRadius: 6,
              cursor: 'pointer',
              fontSize: 12,
              display: 'flex',
              alignItems: 'center',
              gap: 8,
              background: n === currentPage ? 'var(--bg-active)' : 'transparent',
              color: n === currentPage ? '#fff' : 'var(--text-muted)',
              marginBottom: 2,
            }}
          >
            <FileText size={12} />
            Page {n}
          </div>
        ))}
        {tab === 'bookmarks' && (
          <div style={{ fontSize: 11, color: 'var(--text-muted)', textAlign: 'center', marginTop: 24 }}>
            No bookmarks
          </div>
        )}
        {tab === 'annotations' && (
          <div style={{ fontSize: 11, color: 'var(--text-muted)', textAlign: 'center', marginTop: 24 }}>
            No annotations
          </div>
        )}
      </div>
    </div>
  )
}
