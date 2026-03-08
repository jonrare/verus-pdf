import { useState, useEffect } from 'react'
import { useAppStore } from '../../stores/appStore'
import { useOperation } from '../../hooks/useOperation'
import { Encrypt, Decrypt, ChangePassword, EncryptionStatus, TempPathB, SaveFileDialog, CopyFile } from '../../wails.js'
import { Lock, Unlock, Key } from 'lucide-react'

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

const inputStyle = { width: '100%', background: 'var(--bg-base)', border: '1px solid var(--border)', color: 'var(--text-primary)', borderRadius: 5, padding: '5px 8px', fontSize: 12 }

export default function SecurityPanel() {
  const { document, startOperation, finishOperation, failOperation } = useAppStore()
  const { docPath } = useOperation()

  const [encStatus, setEncStatus] = useState({ encrypted: false, hasUserPW: false })
  const [ownerPw,   setOwnerPw]   = useState('')
  const [userPw,    setUserPw]    = useState('')
  const [perms,     setPerms]     = useState('all')
  const [decPw,     setDecPw]     = useState('')
  const [curPw,     setCurPw]     = useState('')
  const [newPw,     setNewPw]     = useState('')

  useEffect(() => {
    if (!docPath) { setEncStatus({ encrypted: false, hasUserPW: false }); return }
    EncryptionStatus(docPath)
      .then(s => setEncStatus(s ?? { encrypted: false, hasUserPW: false }))
      .catch(() => setEncStatus({ encrypted: false, hasUserPW: false }))
  }, [docPath])

  if (!document) return null

  const { encrypted, hasUserPW } = encStatus

  const handleEncrypt = async () => {
    if (!ownerPw || !docPath) return
    const sourceName = docPath.split(/[/\\]/).pop()
    startOperation('Encrypt')
    try {
      const tempPath = await TempPathB(sourceName)
      const result   = await Encrypt(docPath, tempPath, ownerPw, userPw, perms)
      if (result?.error) { failOperation(result.error); return }
      const dest = await SaveFileDialog('Save Encrypted PDF', sourceName)
      if (dest) {
        await CopyFile(tempPath, dest)
        finishOperation('Encrypted and saved — reopen the saved file to continue editing')
      } else {
        finishOperation('Encrypted — use Save As to keep the protected file')
      }
    } catch (e) {
      failOperation(e.message ?? String(e))
    }
  }

  return (
    <div style={{ width: 260, background: 'var(--bg-panel)', borderRight: '1px solid var(--border)', display: 'flex', flexDirection: 'column', flexShrink: 0 }}>
      <div style={{ padding: '10px 14px', borderBottom: '1px solid var(--border)', fontSize: 12, fontWeight: 600 }}>Security</div>
      <div style={{ flex: 1, overflow: 'auto', padding: 12 }}>

        {encrypted && (
          <div style={{ padding: '8px 12px', marginBottom: 12, background: 'rgba(99,179,237,0.1)', border: '1px solid rgba(99,179,237,0.3)', borderRadius: 8, fontSize: 11, color: 'var(--text-muted)' }}>
            🔒 {hasUserPW ? 'Password-protected (user + owner password)' : 'Owner password only — file opens without a password'}
          </div>
        )}

        {!encrypted && (
          <Section title="Encrypt" icon={Lock}>
            <Field label="Owner password (required)">
              <input type="password" value={ownerPw} onChange={e => setOwnerPw(e.target.value)} style={inputStyle} placeholder="Full-access password" />
            </Field>
            <Field label="User password (to open — optional)">
              <input type="password" value={userPw} onChange={e => setUserPw(e.target.value)} style={inputStyle} placeholder="Leave blank = open but restricted" />
            </Field>
            <Field label="Permissions for user">
              <select value={perms} onChange={e => setPerms(e.target.value)}
                style={{ ...inputStyle, padding: '4px 8px' }}>
                <option value="all">Full access</option>
                <option value="print">Print only</option>
                <option value="none">No permissions</option>
              </select>
            </Field>
            <div style={{ fontSize: 10, color: 'var(--text-muted)' }}>
              You'll be prompted to save. Reopen the saved file to continue editing.
            </div>
            <button className="primary" disabled={!ownerPw} onClick={handleEncrypt}>
              Encrypt & Save As…
            </button>
          </Section>
        )}

        {encrypted && (
          <Section title="Remove Protection" icon={Unlock}>
            <Field label={hasUserPW ? 'Owner or user password' : 'Owner password'}>
              <input type="password" value={decPw} onChange={e => setDecPw(e.target.value)} style={inputStyle} placeholder="Enter password" />
            </Field>
            <button className="primary" disabled={!decPw}
              onClick={() => run('Decrypt', (out) => Decrypt(docPath, out, decPw))}>
              Remove Protection
            </button>
          </Section>
        )}

        {encrypted && (
          <Section title="Change Password" icon={Key}>
            <Field label="Current owner password">
              <input type="password" value={curPw} onChange={e => setCurPw(e.target.value)} style={inputStyle} />
            </Field>
            <Field label="New owner password">
              <input type="password" value={newPw} onChange={e => setNewPw(e.target.value)} style={inputStyle} />
            </Field>
            <button className="primary" disabled={!curPw || !newPw}
              onClick={async () => {
                if (!docPath) return
                const { TempPath, SaveFileDialog: SFD, CopyFile: CF } = await import('../../wails.js')
                const sourceName = docPath.split(/[/\\]/).pop()
                startOperation('Change Password')
                try {
                  const tempPath = await TempPath(sourceName)
                  const result   = await ChangePassword(docPath, tempPath, curPw, newPw)
                  if (result?.error) { failOperation(result.error); return }
                  const dest = await SFD('Save File With New Password', sourceName)
                  if (dest) {
                    await CF(tempPath, dest)
                    finishOperation('Password changed — file saved')
                  } else {
                    finishOperation('Password changed')
                  }
                } catch (e) { failOperation(e.message ?? String(e)) }
              }}>
              Change Password
            </button>
          </Section>
        )}

      </div>
    </div>
  )
}
