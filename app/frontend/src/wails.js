// wails.js — Wails v2 Go bridge
// All calls go through window.go.<package>.<Struct>.<Method>
// which is the stable Wails binding layer.

function ready(check, timeout = 5000) {
  return new Promise((resolve, reject) => {
    if (check()) return resolve()
    const start = Date.now()
    const id = setInterval(() => {
      if (check()) { clearInterval(id); resolve() }
      else if (Date.now() - start > timeout) { clearInterval(id); reject(new Error('Wails bridge timeout')) }
    }, 20)
  })
}

// ── Dialogs (viewer service) ──────────────────────────────────────────────────

export async function OpenFileDialog() {
  await ready(() => window.go?.viewer?.Service?.OpenFileDialog)
  return await window.go.viewer.Service.OpenFileDialog() ?? ''
}

export async function OpenMultipleFilesDialog() {
  await ready(() => window.go?.viewer?.Service?.OpenMultipleFilesDialog)
  return await window.go.viewer.Service.OpenMultipleFilesDialog() ?? []
}

export async function OpenImageFilesDialog() {
  await ready(() => window.go?.viewer?.Service?.OpenImageFilesDialog)
  return await window.go.viewer.Service.OpenImageFilesDialog() ?? []
}

export async function OpenAnyFileDialog() {
  await ready(() => window.go?.viewer?.Service?.OpenAnyFileDialog)
  return await window.go.viewer.Service.OpenAnyFileDialog() ?? ''
}

export async function IsEncrypted(filePath) {
  await ready(() => window.go?.viewer?.Service?.IsEncrypted)
  return window.go.viewer.Service.IsEncrypted(filePath)
}

export async function EncryptionStatus(filePath) {
  await ready(() => window.go?.viewer?.Service?.EncryptionStatus)
  return window.go.viewer.Service.EncryptionStatus(filePath)
}

export async function CopyFile(src, dst) {
  await ready(() => window.go?.viewer?.Service?.CopyFile)
  return window.go.viewer.Service.CopyFile(src, dst)
}

export async function SaveFileDialog(title = 'Save As', defaultFilename = 'output.pdf') {
  await ready(() => window.go?.viewer?.Service?.SaveFileDialog)
  return await window.go.viewer.Service.SaveFileDialog(title, defaultFilename) ?? ''
}

export async function OpenDirectoryDialog(title = 'Select Folder') {
  await ready(() => window.go?.viewer?.Service?.OpenDirectoryDialog)
  return await window.go.viewer.Service.OpenDirectoryDialog(title) ?? ''
}

// ── Viewer ────────────────────────────────────────────────────────────────────

export async function OpenDocument(path) {
  await ready(() => window.go?.viewer?.Service?.OpenDocument)
  return window.go.viewer.Service.OpenDocument(path)
}

export async function SaveDocument(inputPath, outputPath) {
  await ready(() => window.go?.viewer?.Service?.SaveDocument)
  return window.go.viewer.Service.SaveDocument(inputPath, outputPath)
}

export async function ReadFileBytes(path) {
  await ready(() => window.go?.viewer?.Service?.ReadFileBytes)
  return window.go.viewer.Service.ReadFileBytes(path)
}

// ── Merge ─────────────────────────────────────────────────────────────────────
// MergeFiles(inputPaths []string, outputPath string) MergeResult
export async function MergeFiles(inputPaths, outputPath) {
  await ready(() => window.go?.merge?.Service?.MergeFiles)
  return window.go.merge.Service.MergeFiles(inputPaths, outputPath)
}

// SplitEveryNPages(inputPath string, n int, outputDir string) SplitResult
export async function SplitByBookmarks(inputPath, outputDir) {
  await ready(() => window.go?.merge?.Service?.SplitByBookmarks)
  return window.go.merge.Service.SplitByBookmarks(inputPath, outputDir)
}

export async function SplitEveryNPages(inputPath, n, outputDir) {
  await ready(() => window.go?.merge?.Service?.SplitEveryNPages)
  return window.go.merge.Service.SplitEveryNPages(inputPath, n, outputDir)
}

// ExtractPages(inputPath, outputPath, pageSelection string) MergeResult
export async function ExtractPages(inputPath, outputPath, pageSelection) {
  await ready(() => window.go?.merge?.Service?.ExtractPages)
  return window.go.merge.Service.ExtractPages(inputPath, outputPath, pageSelection)
}

// RotatePages(inputPath, outputPath string, degrees int, pageSelection string) MergeResult
export async function RotatePages(inputPath, outputPath, degrees, pageSelection) {
  await ready(() => window.go?.merge?.Service?.RotatePages)
  return window.go.merge.Service.RotatePages(inputPath, outputPath, degrees, pageSelection)
}

// ── Security ──────────────────────────────────────────────────────────────────
// Encrypt(inputPath, outputPath, ownerPassword, userPassword string, permissions PermissionLevel) Result
export async function Encrypt(inputPath, outputPath, ownerPassword, userPassword, permissions) {
  await ready(() => window.go?.security?.Service?.Encrypt)
  return window.go.security.Service.Encrypt(inputPath, outputPath, ownerPassword, userPassword, permissions)
}

// Decrypt(inputPath, outputPath, password string) Result
export async function Decrypt(inputPath, outputPath, password) {
  await ready(() => window.go?.security?.Service?.Decrypt)
  return window.go.security.Service.Decrypt(inputPath, outputPath, password)
}

// ChangePassword(inputPath, outputPath, currentOwnerPW, newOwnerPW string) Result
export async function ChangePassword(inputPath, outputPath, currentPw, newPw) {
  await ready(() => window.go?.security?.Service?.ChangePassword)
  return window.go.security.Service.ChangePassword(inputPath, outputPath, currentPw, newPw)
}

// ── Edit ──────────────────────────────────────────────────────────────────────
// AddTextStamp(inputPath, outputPath, text, descriptor, pageSelection string) Result
export async function AddTextStamp(inputPath, outputPath, text, descriptor, pageSelection) {
  await ready(() => window.go?.edit?.Service?.AddTextStamp)
  return window.go.edit.Service.AddTextStamp(inputPath, outputPath, text, descriptor, pageSelection)
}

// AddImageStamp(inputPath, outputPath, imagePath, descriptor, pageSelection string) Result
export async function AddImageStamp(inputPath, outputPath, imagePath, descriptor, pageSelection) {
  await ready(() => window.go?.edit?.Service?.AddImageStamp)
  return window.go.edit.Service.AddImageStamp(inputPath, outputPath, imagePath, descriptor, pageSelection)
}

// RemoveWatermarks(inputPath, outputPath, pageSelection string) Result
export async function RemoveWatermarks(inputPath, outputPath, pageSelection) {
  await ready(() => window.go?.edit?.Service?.RemoveWatermarks)
  return window.go.edit.Service.RemoveWatermarks(inputPath, outputPath, pageSelection)
}

// AddPageNumbers(inputPath, outputPath, descriptor string) Result
export async function AddPageNumbers(inputPath, outputPath, descriptor) {
  await ready(() => window.go?.edit?.Service?.AddPageNumbers)
  return window.go.edit.Service.AddPageNumbers(inputPath, outputPath, descriptor)
}

// AddWatermarkText(inputPath, outputPath, text string) Result
export async function AddWatermarkText(inputPath, outputPath, text) {
  await ready(() => window.go?.edit?.Service?.AddWatermarkText)
  return window.go.edit.Service.AddWatermarkText(inputPath, outputPath, text)
}

// ── Convert ───────────────────────────────────────────────────────────────────
// ImagesToPDF(imagePaths []string, outputPath string) Result
export async function ImagesToPDF(imagePaths, outputPath) {
  await ready(() => window.go?.convert?.Service?.ImagesToPDF)
  return window.go.convert.Service.ImagesToPDF(imagePaths, outputPath)
}

// PDFToText(inputPath string) (string, string)
export async function PDFToText(inputPath) {
  await ready(() => window.go?.convert?.Service?.PDFToText)
  return window.go.convert.Service.PDFToText(inputPath)
}

export async function WriteFromTemp(destPath) {
  await ready(() => window.go?.convert?.Service?.WriteFromTemp)
  return window.go.convert.Service.WriteFromTemp(destPath)
}

export async function SaveExtractedText(suggestedName) {
  await ready(() => window.go?.convert?.Service?.SaveExtractedText)
  return window.go.convert.Service.SaveExtractedText(suggestedName)
}

// ExtractImages(inputPath, outputDir string) Result
export async function ExtractImages(inputPath, outputDir) {
  await ready(() => window.go?.convert?.Service?.ExtractImages)
  return window.go.convert.Service.ExtractImages(inputPath, outputDir)
}

// WordToPDF(inputPath, outputDir string) Result
export async function WordToPDF(inputPath, outputDir) {
  await ready(() => window.go?.convert?.Service?.WordToPDF)
  return window.go.convert.Service.WordToPDF(inputPath, outputDir)
}

// ── Optimize ──────────────────────────────────────────────────────────────────
// Optimize(inputPath, outputPath string) OptimizeResult
export async function Optimize(inputPath, outputPath) {
  await ready(() => window.go?.optimize?.Service?.Optimize)
  return window.go.optimize.Service.Optimize(inputPath, outputPath)
}

// RemoveMetadata(inputPath, outputPath string) OptimizeResult
export async function RemoveMetadata(inputPath, outputPath) {
  await ready(() => window.go?.optimize?.Service?.RemoveMetadata)
  return window.go.optimize.Service.RemoveMetadata(inputPath, outputPath)
}

// Validate(inputPath string) (bool, string)
export async function Validate(inputPath) {
  await ready(() => window.go?.optimize?.Service?.Validate)
  return window.go.optimize.Service.Validate(inputPath)
}

// TempPath(baseName string) string
export async function TempPath(baseName) {
  await ready(() => window.go?.viewer?.Service?.TempPath)
  return window.go.viewer.Service.TempPath(baseName)
}

export async function TempPathB(baseName) {
  await ready(() => window.go?.viewer?.Service?.TempPathB)
  return window.go.viewer.Service.TempPathB(baseName)
}

// ExtractPageText(inputPath string, pageNum int) ([]TextSpan, error)
export async function ExtractPageText(inputPath, pageNum) {
  await ready(() => window.go?.edit?.Service?.ExtractPageText)
  return window.go.edit.Service.ExtractPageText(inputPath, pageNum)
}

// ReplaceSpanText(inputPath, outputPath string, pageNum, streamIndex, opStart, opEnd int, newText string) TextEditResult
export async function ReplaceSpanText(inputPath, outputPath, pageNum, streamIndex, opStart, opEnd, newText) {
  await ready(() => window.go?.edit?.Service?.ReplaceSpanText)
  return window.go.edit.Service.ReplaceSpanText(inputPath, outputPath, pageNum, streamIndex, opStart, opEnd, newText)
}

// DebugPageStream(inputPath string, pageNum int) (string, error)
export async function DebugPageStream(inputPath, pageNum) {
  await ready(() => window.go?.edit?.Service?.DebugPageStream)
  return window.go.edit.Service.DebugPageStream(inputPath, pageNum)
}

// ── Bookmarks ──────────────────────────────────────────────────────────────
export async function ListBookmarks(filePath) {
  await ready(() => window.go?.bookmarks?.Service?.ListBookmarks)
  return window.go.bookmarks.Service.ListBookmarks(filePath)
}
export async function AddBookmark(inputPath, outputPath, title, page) {
  await ready(() => window.go?.bookmarks?.Service?.AddBookmark)
  return window.go.bookmarks.Service.AddBookmark(inputPath, outputPath, title, page)
}
export async function RemoveBookmark(inputPath, outputPath, title, page) {
  await ready(() => window.go?.bookmarks?.Service?.RemoveBookmark)
  return window.go.bookmarks.Service.RemoveBookmark(inputPath, outputPath, title, page)
}
