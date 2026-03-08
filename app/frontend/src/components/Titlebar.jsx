import { useAppStore } from '../stores/appStore'

export default function Titlebar() {
  const document = useAppStore(s => s.document)
  const name = document ? ( document.originalPath ?? document.path ).split(/[\/]/).pop() : 'VerusPDF'

  return (
    <div
      style={{
        height: 36,
        background: 'var(--bg-toolbar)',
        borderBottom: '1px solid var(--border)',
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        fontSize: 12,
        fontWeight: 600,
        color: 'var(--text-muted)',
        flexShrink: 0,
        userSelect: 'none',
        cursor: 'default',
        '--wails-draggable': 'drag',
      }}
    >
      {name}
    </div>
  )
}
