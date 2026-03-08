import { useState } from 'react'
import { useAppStore } from '../../stores/appStore'
import { useOperation } from '../../hooks/useOperation'
import { OpenMultipleFilesDialog, MergeFiles, SplitEveryNPages, SplitByBookmarks, ExtractPages, RotatePages, Optimize } from '../../wails.js'
import { Layers, Scissors, RotateCw, Copy, Zap } from 'lucide-react'

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

export default function PagesPanel() {
  const { document } = useAppStore()
  const { run, runToDir, docPath } = useOperation()

  const [mergeFiles, setMergeFiles] = useState([])
  const [splitN, setSplitN]         = useState(1)
  const [extractPages, setExtractPages] = useState('')
  const [rotateDeg, setRotateDeg]   = useState(90)
  const [rotatePages, setRotatePages] = useState('')

  const pickMergeFiles = async () => {
    const files = await OpenMultipleFilesDialog()
    if (files?.length) setMergeFiles(files)
  }

  if (!document) return null

  return (
    <div style={{ width: 260, background: 'var(--bg-panel)', borderRight: '1px solid var(--border)', display: 'flex', flexDirection: 'column', flexShrink: 0 }}>
      <div style={{ padding: '10px 14px', borderBottom: '1px solid var(--border)', fontSize: 12, fontWeight: 600 }}>Pages</div>
      <div style={{ flex: 1, overflow: 'auto', padding: 12 }}>

        <Section title="Merge" icon={Layers}>
          <div style={{ fontSize: 11, color: 'var(--text-muted)' }}>Append other PDFs to this one</div>
          <button onClick={pickMergeFiles} style={{ justifyContent: 'center' }}>
            {mergeFiles.length ? `${mergeFiles.length} file(s) selected` : 'Pick files to append…'}
          </button>
          {mergeFiles.length > 0 && (
            <div style={{ fontSize: 10, color: 'var(--text-muted)', maxHeight: 52, overflowY: 'auto' }}>
              {mergeFiles.map((f, i) => <div key={i}>{f.split(/[\\/]/).pop()}</div>)}
            </div>
          )}
          <button className="primary" disabled={!mergeFiles.length}
            onClick={() => run('Merge PDFs', (out) => MergeFiles([docPath, ...mergeFiles], out))}>
            Merge
          </button>
        </Section>

        <Section title="Split" icon={Scissors}>
          <div style={{ fontSize: 11, color: 'var(--text-muted)' }}>Break this PDF into separate files, one for every group of pages you choose.</div>
          <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
            <span style={{ fontSize: 11, color: 'var(--text-muted)' }}>Every</span>
            <input type="number" min={1} max={document.pageCount} value={splitN}
              onChange={e => setSplitN(Math.max(1, +e.target.value))} style={{ width: 52 }} />
            <span style={{ fontSize: 11, color: 'var(--text-muted)' }}>{splitN === 1 ? 'page' : 'pages'}</span>
          </div>
          <button className="primary"
            onClick={() => runToDir('Split PDF', (dir) => SplitEveryNPages(docPath, splitN, dir))}>
            Split → Pick Folder
          </button>
          <button
            onClick={() => runToDir('Split by Bookmarks', (dir) => SplitByBookmarks(docPath, dir))}>
            Split by Bookmarks → Pick Folder
          </button>
        </Section>

        <Section title="Extract Pages" icon={Copy}>
          <div style={{ fontSize: 11, color: 'var(--text-muted)' }}>Keep only selected pages</div>
          <input type="text" placeholder="e.g. 1-3, 5, 7-9  or  1, 2, 3"
            value={extractPages} onChange={e => setExtractPages(e.target.value)} />
          <button className="primary"
            onClick={() => run('Extract Pages', (out) => ExtractPages(docPath, out, extractPages.trim()))}>
            Extract
          </button>
        </Section>

        <Section title="Rotate" icon={RotateCw}>
          <input type="text" placeholder="Pages: e.g. 1-3, 5  (blank = all)"
            value={rotatePages} onChange={e => setRotatePages(e.target.value)} />
          <div style={{ display: 'flex', gap: 6 }}>
            {[90, 180, 270].map(d => (
              <button key={d} onClick={() => setRotateDeg(d)} style={{ flex: 1, justifyContent: 'center', background: rotateDeg === d ? 'var(--bg-active)' : undefined }}>
                {d}°
              </button>
            ))}
          </div>
          <button className="primary"
            onClick={() => run('Rotate Pages', (out) => RotatePages(docPath, out, rotateDeg, rotatePages.trim()))}>
            Rotate
          </button>
        </Section>

        <Section title="Optimize" icon={Zap}>
          <div style={{ fontSize: 11, color: 'var(--text-muted)' }}>Compress and deduplicate resources</div>
          <button className="primary" onClick={() => run('Optimize', (out) => Optimize(docPath, out))}>
            Optimize
          </button>
        </Section>

      </div>
    </div>
  )
}
