import { useState } from 'react'
import { useAppStore } from '../../stores/appStore'
import { useOperation } from '../../hooks/useOperation'
import { AddWatermarkText, AddTextStamp, AddPageNumbers, RemoveWatermarks, RemoveMetadata, DebugPageStream } from '../../wails.js'
import { Droplets, Type, Hash, Trash2, FileX } from 'lucide-react'

function Section({ title, icon: Icon, children }) {
  return (
    <div style={{ marginBottom: 16, borderRadius: 8, border: '1px solid var(--border)', overflow: 'hidden' }}>
      <div style={{ padding: '8px 12px', background: 'var(--bg-toolbar)', display: 'flex', alignItems: 'center', gap: 7, fontSize: 11, fontWeight: 600, color: 'var(--text-muted)', textTransform: 'uppercase', letterSpacing: 0.7 }}>
        <Icon size={12} /> {title}
      </div>
      <div style={{ padding: 12, display: 'flex', flexDirection: 'column', gap: 8 }}>
        {children}
      </div>
    </div>
  )
}

function Field({ label, children }) {
  return (
    <div>
      <div style={{ fontSize: 11, color: 'var(--text-muted)', marginBottom: 3 }}>{label}</div>
      {children}
    </div>
  )
}

export default function EditPanel() {
  const { document, currentPage } = useAppStore()
  const { run, docPath } = useOperation()

  const [wmText, setWmText]               = useState('DRAFT')
  const [stampText, setStampText]         = useState('')
  const [stampSize, setStampSize]         = useState(24)
  const [stampColor, setStampColor]       = useState('#FF0000')
  const [stampOpacity, setStampOpacity]   = useState(0.8)
  const [stampPos, setStampPos]           = useState('c')
  const [stampRotation, setStampRotation] = useState(0)
  const [stampPages, setStampPages]       = useState('')
  const [pnPos, setPnPos]                 = useState('bc')
  const [pnSize, setPnSize]               = useState(10)
  const [removePages, setRemovePages]     = useState('')
  const [debugStream, setDebugStream]     = useState(null)

  if (!document) return null

  const buildStampDescriptor = () => {
    const colorHex = stampColor.replace('#', '')
    return `font:Helvetica, points:${stampSize}, color:#${colorHex}, rotation:${stampRotation}, opacity:${stampOpacity}, position:${stampPos}`
  }

  const handleDebug = async () => {
    try {
      const result = await DebugPageStream(document.path, currentPage)
      setDebugStream(result)
    } catch (e) {
      setDebugStream('Error: ' + e.message)
    }
  }

  return (
    <div style={{ width: 260, background: 'var(--bg-panel)', borderRight: '1px solid var(--border)', display: 'flex', flexDirection: 'column', flexShrink: 0 }}>
      <div style={{ padding: '10px 14px', borderBottom: '1px solid var(--border)', fontSize: 12, fontWeight: 600 }}>Edit</div>
      <div style={{ flex: 1, overflow: 'auto', padding: 12 }}>

        <Section title="Background Watermark" icon={Droplets}>
          <Field label="Text">
            <input type="text" value={wmText} onChange={e => setWmText(e.target.value)} style={{ width: '100%' }} />
          </Field>
          <div style={{ fontSize: 10, color: 'var(--text-muted)' }}>Diagonal · grey · semi-transparent · all pages</div>
          <button className="primary" disabled={!wmText.trim()}
            onClick={() => run('Add Watermark', (out) => AddWatermarkText(docPath, out, wmText))}>
            Add Watermark
          </button>
        </Section>

        <Section title="Text Stamp" icon={Type}>
          <Field label="Text">
            <input type="text" value={stampText} onChange={e => setStampText(e.target.value)} style={{ width: '100%' }} placeholder="Stamp text" />
          </Field>
          <div style={{ display: 'flex', gap: 8 }}>
            <Field label="Size">
              <input type="number" min={6} max={120} value={stampSize} onChange={e => setStampSize(+e.target.value)} style={{ width: 56 }} />
            </Field>
            <Field label="Color">
              <input type="color" value={stampColor} onChange={e => setStampColor(e.target.value)}
                style={{ width: 44, height: 28, padding: 2, background: 'var(--bg-base)', border: '1px solid var(--border)', borderRadius: 5 }} />
            </Field>
            <Field label="Opacity">
              <input type="number" min={0.1} max={1} step={0.1} value={stampOpacity} onChange={e => setStampOpacity(+e.target.value)} style={{ width: 52 }} />
            </Field>
          </div>
          <div style={{ display: 'flex', gap: 8 }}>
            <Field label="Position">
              <select value={stampPos} onChange={e => setStampPos(e.target.value)}
                style={{ background: 'var(--bg-base)', border: '1px solid var(--border)', color: 'var(--text-primary)', borderRadius: 5, padding: '4px 6px', fontSize: 12 }}>
                <option value="c">Center</option>
                <option value="tl">Top Left</option><option value="tc">Top Center</option><option value="tr">Top Right</option>
                <option value="bl">Bottom Left</option><option value="bc">Bottom Center</option><option value="br">Bottom Right</option>
              </select>
            </Field>
            <Field label="Rotation">
              <input type="number" min={-180} max={180} value={stampRotation} onChange={e => setStampRotation(+e.target.value)} style={{ width: 56 }} />
            </Field>
          </div>
          <Field label="Pages (blank = all, e.g. 1-3,5)">
            <input type="text" value={stampPages} onChange={e => setStampPages(e.target.value)} placeholder="all" style={{ width: '100%' }} />
          </Field>
          <button className="primary" disabled={!stampText.trim()}
            onClick={() => run('Add Stamp', (out) => AddTextStamp(docPath, out, stampText, buildStampDescriptor(), stampPages.trim()))}>
            Add Stamp
          </button>
        </Section>

        <Section title="Page Numbers" icon={Hash}>
          <div style={{ display: 'flex', gap: 8 }}>
            <Field label="Position">
              <select value={pnPos} onChange={e => setPnPos(e.target.value)}
                style={{ background: 'var(--bg-base)', border: '1px solid var(--border)', color: 'var(--text-primary)', borderRadius: 5, padding: '4px 6px', fontSize: 12 }}>
                <option value="bc">Bottom Center</option><option value="bl">Bottom Left</option><option value="br">Bottom Right</option>
                <option value="tc">Top Center</option><option value="tl">Top Left</option><option value="tr">Top Right</option>
              </select>
            </Field>
            <Field label="Size">
              <input type="number" min={6} max={24} value={pnSize} onChange={e => setPnSize(+e.target.value)} style={{ width: 52 }} />
            </Field>
          </div>
          <button className="primary"
            onClick={() => run('Add Page Numbers', (out) => AddPageNumbers(docPath, out, `font:Helvetica, points:${pnSize}, color:#555555, position:${pnPos}, offset:0 10`))}>
            Add Page Numbers
          </button>
        </Section>

        <Section title="Remove" icon={Trash2}>
          <Field label="Remove watermarks — pages (blank = all)">
            <input type="text" value={removePages} onChange={e => setRemovePages(e.target.value)} placeholder="all" style={{ width: '100%' }} />
          </Field>
          <button onClick={() => run('Remove Watermarks', (out) => RemoveWatermarks(docPath, out, removePages.trim()))}>
            Remove Watermarks
          </button>
          <button onClick={() => run('Strip Metadata', (out) => RemoveMetadata(docPath, out))}>
            <FileX size={13} /> Strip Metadata
          </button>
        </Section>

        <button onClick={handleDebug} style={{ width: '100%', padding: '6px 0', fontSize: 11, color: 'var(--text-muted)', border: '1px solid var(--border)', borderRadius: 6, marginBottom: 8 }}>
          Debug: Dump page stream
        </button>

      </div>

      {debugStream && (
        <div style={{ position: 'fixed', inset: 0, zIndex: 1000, background: 'rgba(0,0,0,0.7)', display: 'flex', alignItems: 'center', justifyContent: 'center' }}
             onClick={() => setDebugStream(null)}>
          <div onClick={e => e.stopPropagation()} style={{ background: 'var(--bg-panel)', border: '1px solid var(--border)', borderRadius: 10, width: '80vw', maxHeight: '80vh', display: 'flex', flexDirection: 'column', overflow: 'hidden' }}>
            <div style={{ padding: '10px 14px', borderBottom: '1px solid var(--border)', display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
              <span style={{ fontSize: 12, fontWeight: 600 }}>Content Stream — Page {currentPage}</span>
              <button onClick={() => setDebugStream(null)} style={{ fontSize: 11 }}>Close</button>
            </div>
            <pre style={{ margin: 0, padding: 14, overflowY: 'auto', fontSize: 11, fontFamily: 'monospace', color: 'var(--text-primary)', whiteSpace: 'pre-wrap', wordBreak: 'break-all' }}>
              {debugStream}
            </pre>
          </div>
        </div>
      )}
    </div>
  )
}
