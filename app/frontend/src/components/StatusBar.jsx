import { useAppStore } from '../stores/appStore'
import { Loader2 } from 'lucide-react'

export default function StatusBar() {
  const { document, currentPage, zoom, statusMessage } = useAppStore()

  const msgColor = statusMessage?.type === 'error'   ? 'var(--danger)'
                 : statusMessage?.type === 'done'    ? 'var(--success)'
                 : 'var(--text-muted)'

  return (
    <div style={{
      height: 24,
      background: 'var(--bg-toolbar)',
      borderTop: '1px solid var(--border)',
      display: 'flex',
      alignItems: 'center',
      padding: '0 12px',
      gap: 16,
      fontSize: 11,
      color: 'var(--text-muted)',
      flexShrink: 0,
    }}>
      {document ? (
        <>
          <span>{(document.originalPath ?? document.path).split(/[\\/]/).pop()}</span>
          <span>•</span>
          <span>Page {currentPage} of {document.pageCount}</span>
          <span>•</span>
          <span>{Math.round(zoom * 100)}%</span>
          {document.title && <><span>•</span><span>{document.title}</span></>}
        </>
      ) : (
        <span>Ready</span>
      )}

      <div style={{ flex: 1 }} />

      {statusMessage && (
        <span style={{ display: 'flex', alignItems: 'center', gap: 5, color: msgColor, transition: 'color 0.2s' }}>
          {statusMessage.type === 'working' && <Loader2 size={11} style={{ animation: 'spin 1s linear infinite' }} />}
          {statusMessage.message}
        </span>
      )}
    </div>
  )
}

// Keep OperationModal export so existing imports don't break — it's now a no-op
export function OperationModal() { return null }
