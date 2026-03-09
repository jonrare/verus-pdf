import { useAppStore } from '../stores/appStore'
import { TempPath, TempPathB, OpenDocument, OpenDirectoryDialog } from '../wails.js'
import { friendlyError } from '../friendlyError'

export function useOperation() {
  const { document, startOperation, finishOperation, failOperation, setDocument, pushUndo } = useAppStore()
  const docPath = document?.path

  async function run(title, fn, onSuccess) {
    if (!docPath) return
    const originalPath = document.originalPath ?? docPath
    const sourceName   = originalPath.split(/[\\/]/).pop()

    // Alternate between two temp slots so input and output are never the same file.
    // First op (no slot set): write to slot B. Next: back to A. And so on.
    const writeToB = document.tempSlot !== 'b'
    const tempPath = writeToB ? await TempPathB(sourceName) : await TempPath(sourceName)
    const nextSlot = writeToB ? 'b' : 'a'

    pushUndo()
    startOperation(title)
    try {
      const result = await fn(tempPath)
      const errMsg  = Array.isArray(result) ? result[1] : result?.error
      if (errMsg) { failOperation(friendlyError(errMsg)); return }

      const doc = await OpenDocument(tempPath)
      if (doc?.error) { failOperation(friendlyError(doc.error)); return }

      setDocument({ ...doc, path: tempPath, originalPath, tempSlot: nextSlot })
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
