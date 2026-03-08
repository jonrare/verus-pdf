import { useState } from 'react'
import { useAppStore } from '../../stores/appStore'
import { useOperation } from '../../hooks/useOperation'
import { PDFToText, ExtractImages } from '../../wails.js'
import { FileText, Download } from 'lucide-react'

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

export default function ConvertPanel() {
  const { document } = useAppStore()
  const { runToDir, docPath } = useOperation()
  const { startOperation, finishOperation, failOperation } = useAppStore()

  const [textResult, setTextResult] = useState('')
  const [hasText, setHasText]       = useState(false)
  const [showText, setShowText]     = useState(false)
  const [saveError, setSaveError]   = useState('')

  const handlePDFToText = async () => {
    if (!docPath) return
    startOperation('Extract Text')
    try {
      const result = await PDFToText(docPath)
      if (result?.error) { failOperation(result.error); return }
      finishOperation('Text extracted')
      setTextResult(result?.text || '(No selectable text found in this PDF)')
      setHasText(true)
      setShowText(true)
    } catch (e) {
      failOperation(e.message ?? String(e))
    }
  }

  const handleSave = async () => {
    setSaveError('')
    try {
      const name = (document?.path ?? 'document').split(/[/\\]/).pop().replace(/\.pdf$/i, '') + '.txt'
      const handle = await window.showSaveFilePicker({
        suggestedName: name,
        types: [{ description: 'Text Files', accept: { 'text/plain': ['.txt'] } }]
      })
      const writable = await handle.createWritable()
      // Strip any null bytes that may have leaked from CID font decoding
      await writable.write(textResult.replace(/\0/g, ''))
      await writable.close()
    } catch (e) {
      if (e.name !== 'AbortError') setSaveError(String(e))
    }
  }

  const handleCopy = () => {
    const ta = window.document.createElement('textarea')
    ta.value = textResult
    ta.style.position = 'fixed'
    ta.style.opacity = '0'
    window.document.body.appendChild(ta)
    ta.select()
    window.document.execCommand('copy')
    window.document.body.removeChild(ta)
  }

  if (!document) return null

  return (
    <div style={{ width: 260, background: 'var(--bg-panel)', borderRight: '1px solid var(--border)', display: 'flex', flexDirection: 'column', flexShrink: 0 }}>
      <div style={{ padding: '10px 14px', borderBottom: '1px solid var(--border)', fontSize: 12, fontWeight: 600 }}>Convert</div>
      <div style={{ flex: 1, overflow: 'auto', padding: 12 }}>

        <Section title="PDF → Text" icon={FileText}>
          <div style={{ fontSize: 11, color: 'var(--text-muted)' }}>Extract all selectable text from this PDF</div>
          <button className="primary" onClick={handlePDFToText}>Extract Text</button>
        </Section>

        <Section title="Extract Embedded Images" icon={Download}>
          <div style={{ fontSize: 11, color: 'var(--text-muted)' }}>Save all embedded images to a folder</div>
          <button className="primary"
            onClick={() => runToDir('Extract Images', (dir) => ExtractImages(docPath, dir))}>
            Extract Images → Pick Folder
          </button>
        </Section>

      </div>

      {showText && (
        <div style={{ position: 'fixed', inset: 0, background: 'rgba(0,0,0,0.7)', display: 'flex', alignItems: 'center', justifyContent: 'center', zIndex: 200 }}>
          <div style={{ background: 'var(--bg-panel)', border: '1px solid var(--border)', borderRadius: 12, width: 640, maxHeight: '80vh', display: 'flex', flexDirection: 'column', boxShadow: 'var(--shadow)' }}>
            <div style={{ padding: '14px 20px', borderBottom: '1px solid var(--border)', display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
              <span style={{ fontWeight: 600 }}>Extracted Text</span>
              <button onClick={() => setShowText(false)}>Close</button>
            </div>
            <div style={{ flex: 1, overflow: 'auto', padding: 20 }}>
              <pre style={{ fontSize: 12, color: 'var(--text-primary)', whiteSpace: 'pre-wrap', fontFamily: 'monospace', lineHeight: 1.6 }}>
                {textResult}
              </pre>
            </div>
            <div style={{ padding: '12px 20px', borderTop: '1px solid var(--border)', display: 'flex', flexDirection: 'column', gap: 8 }}>
              {saveError && <div style={{ fontSize: 11, color: 'red' }}>{saveError}</div>}
              <div style={{ display: 'flex', gap: 8 }}>
                <button className="primary" disabled={!hasText} onClick={handleCopy}>Copy to Clipboard</button>
                <button className="primary" disabled={!hasText} onClick={handleSave}>Save as .txt</button>
                <button onClick={() => setShowText(false)}>Close</button>
              </div>
            </div>
          </div>
        </div>
      )}
    </div>
  )
}
