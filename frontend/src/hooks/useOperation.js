import { useAppStore } from '../stores/appStore'
import { NewWorkingPath, OpenDocument, OpenDirectoryDialog } from '../wails.js'
import { friendlyError } from '../friendlyError'

export function useOperation() {
  const { document, startOperation, finishOperation, failOperation, setDocument, pushUndo } = useAppStore()
  const docPath = document?.path

  async function run(title, fn, onSuccess) {
    if (!docPath) return
    const originalPath = document.originalPath ?? docPath
    const sourceName   = originalPath.split(/[\\/]/).pop()

    // A fresh working file per operation: input and output are never the same
    // path, and each undo snapshot keeps pointing at content nothing overwrites.
    const tempPath = await NewWorkingPath(sourceName)

    pushUndo()
    startOperation(title)
    try {
      const result = await fn(tempPath)
      const errMsg  = Array.isArray(result) ? result[1] : result?.error
      if (errMsg) { failOperation(friendlyError(errMsg)); return }

      const doc = await OpenDocument(tempPath)
      if (doc?.error) { failOperation(friendlyError(doc.error)); return }

      setDocument({ ...doc, path: tempPath, originalPath })
      finishOperation(`${title} applied — use Save As to keep`)
      if (onSuccess) await onSuccess(tempPath)
    } catch (e) {
      failOperation(friendlyError(e))
    }
  }

  async function runToDir(title, fn) {
    const dir = await OpenDirectoryDialog('Select Output Folder')
    if (!dir) return
    pushUndo()
    startOperation(title)
    try {
      const result = await fn(dir)
      const errMsg  = Array.isArray(result) ? result[1] : result?.error
      if (errMsg) { failOperation(friendlyError(errMsg)); return }
      const count = result?.files?.length
      finishOperation(count ? `${count} file(s) saved to folder` : 'Done')
    } catch (e) {
      failOperation(friendlyError(e))
    }
  }

  return { run, runToDir, docPath }
}
