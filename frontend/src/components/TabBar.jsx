import { useAppStore } from '../stores/appStore'
import { X, FileText } from 'lucide-react'

export default function TabBar() {
  const { tabs, activeTabId, setActiveTab, closeTab, newTab } = useAppStore()

  if (tabs.length === 0) return null

  return (
    <div style={{
      display: 'flex',
      alignItems: 'flex-end',
      background: 'var(--bg-base)',
      borderBottom: '1px solid var(--border)',
      flexShrink: 0,
      height: 34,
    }}>
      {/* Scrollable tab list */}
      <div style={{
        display: 'flex',
        alignItems: 'flex-end',
        flex: 1,
        overflowX: 'auto',
        overflowY: 'hidden',
        scrollbarWidth: 'none',
        paddingLeft: 4,
        gap: 2,
        height: '100%',
      }}>
      {tabs.map(tab => {
        const isActive  = tab.id === activeTabId
        const label   = tab.document
                          ? (tab.document.originalPath ?? tab.document.path).split(/[\/]/).pop()
                          : 'New Tab'
        const isDirty = tab.document && tab.document.path !== tab.document.originalPath

        return (
          <div
            key={tab.id}
            onClick={() => setActiveTab(tab.id)}
            style={{
              display: 'flex',
              alignItems: 'center',
              gap: 6,
              padding: '0 10px 0 10px',
              height: 30,
              borderRadius: '6px 6px 0 0',
              cursor: 'pointer',
              flexShrink: 0,
              maxWidth: 200,
              background: isActive ? 'var(--bg-panel)' : 'transparent',
              border: isActive ? '1px solid var(--border)' : '1px solid transparent',
              borderBottom: isActive ? '1px solid var(--bg-panel)' : '1px solid transparent',
              marginBottom: isActive ? -1 : 0,
              position: 'relative',
            }}
          >
            <FileText size={11} style={{ color: 'var(--accent)', flexShrink: 0 }} />
            <span style={{
              fontSize: 11,
              color: isActive ? 'var(--text-primary)' : 'var(--text-muted)',
              overflow: 'hidden',
              textOverflow: 'ellipsis',
              whiteSpace: 'nowrap',
              flex: 1,
              userSelect: 'none',
            }}>
              {isDirty ? '● ' : ''}{label}
            </span>
            <button
              onClick={e => { e.stopPropagation(); closeTab(tab.id) }}
              style={{
                padding: 2,
                borderRadius: 3,
                flexShrink: 0,
                color: 'var(--text-muted)',
                opacity: 0.6,
                lineHeight: 0,
              }}
              onMouseEnter={e => e.currentTarget.style.opacity = '1'}
              onMouseLeave={e => e.currentTarget.style.opacity = '0.6'}
            >
              <X size={10} />
            </button>
          </div>
        )
      })}
      {/* New tab button — sits right after last tab in the scroll flow */}
      <button
        onClick={newTab}
        title="New tab"
        style={{
          flexShrink: 0,
          width: 28,
          height: 26,
          alignSelf: 'flex-end',
          marginBottom: 2,
          fontSize: 18,
          color: 'var(--text-muted)',
          lineHeight: 1,
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'center',
          borderRadius: 4,
        }}
        onMouseEnter={e => e.currentTarget.style.color = 'var(--text-primary)'}
        onMouseLeave={e => e.currentTarget.style.color = 'var(--text-muted)'}
      >
        +
      </button>
      </div>{/* end scrollable tab list */}
    </div>
  )
}
