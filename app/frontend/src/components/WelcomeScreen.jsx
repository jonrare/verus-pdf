import { FileText, FolderOpen } from 'lucide-react'
import { useAppStore } from '../stores/appStore'
import { OpenFileDialog, OpenDocument } from '../wails.js'

export default function WelcomeScreen() {
  const { recentFiles, openTab, addRecentFile } = useAppStore()

  const handleOpen = async () => {
    const path = await OpenFileDialog()
    if (!path) return
    const result = await OpenDocument(path)
    if (result?.error) { alert(result.error); return }
    openTab(result)
    addRecentFile(path)
  }

  const handleOpenRecent = async (path) => {
    const result = await OpenDocument(path)
    if (result?.error) { alert(result.error); return }
    openTab(result)
  }

  return (
    <div style={{
      flex: 1,
      display: 'flex',
      flexDirection: 'column',
      alignItems: 'center',
      justifyContent: 'center',
      gap: 32,
      background: 'var(--bg-base)',
    }}>
      <div style={{ display: 'flex', flexDirection: 'column', alignItems: 'center', gap: 12 }}>
        <div style={{
          width: 64, height: 64,
          background: 'var(--accent)',
          borderRadius: 16,
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'center',
        }}>
          <FileText size={36} color="#fff" />
        </div>
        <h1 style={{ fontSize: 28, fontWeight: 700, color: 'var(--text-primary)', letterSpacing: -0.5 }}>
          VerusPDF
        </h1>
        <p style={{ fontSize: 13, color: 'var(--text-muted)' }}>
          Your lightweight, open-source PDF suite
        </p>
      </div>

      <button className="primary" onClick={handleOpen} style={{ fontSize: 14, padding: '10px 24px', gap: 8 }}>
        <FolderOpen size={16} /> Open PDF
      </button>

      {recentFiles.length > 0 && (
        <div style={{ width: 420 }}>
          <div style={{ fontSize: 11, fontWeight: 600, textTransform: 'uppercase', letterSpacing: 0.5, marginBottom: 8, color: 'var(--text-muted)' }}>
            Recent Files
          </div>
          <div style={{ border: '1px solid var(--border)', borderRadius: 8, overflow: 'hidden' }}>
            {recentFiles.map((path, i) => (
              <div
                key={path}
                onClick={() => handleOpenRecent(path)}
                style={{
                  padding: '9px 14px',
                  cursor: 'pointer',
                  display: 'flex',
                  alignItems: 'center',
                  gap: 10,
                  borderTop: i > 0 ? '1px solid var(--border)' : undefined,
                  background: 'var(--bg-panel)',
                }}
                onMouseEnter={e => e.currentTarget.style.background = 'var(--bg-hover)'}
                onMouseLeave={e => e.currentTarget.style.background = 'var(--bg-panel)'}
              >
                <FileText size={14} style={{ color: 'var(--accent)', flexShrink: 0 }} />
                <span style={{ fontSize: 12, color: 'var(--text-primary)', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                  {path.split(/[\\/]/).pop()}
                </span>
                <span style={{ fontSize: 10, color: 'var(--text-muted)', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap', direction: 'rtl', flex: 1, textAlign: 'right' }}>
                  {path}
                </span>
              </div>
            ))}
          </div>
        </div>
      )}
    </div>
  )
}
