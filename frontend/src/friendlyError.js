// friendlyError.js — translate raw Go/OS error strings into plain English

const patterns = [
  [/being used by another process/i, 'The file is being used by another program.'],
  [/permission denied/i, 'Permission denied — the file may be read-only or protected.'],
  [/no such file or directory/i, 'The file could not be found. It may have been moved or deleted.'],
  [/is empty|0 bytes/i, 'The file is empty and cannot be opened.'],
  [/not a valid PDF|not a PDF|malformed/i, 'This does not appear to be a valid PDF file.'],
  [/not a directory/i, 'The path is not a valid folder.'],
  [/file exists/i, 'A file with that name already exists.'],
  [/disk full|no space left/i, 'Not enough disk space to save the file.'],
  [/access is denied/i, 'Access denied — you may not have permission to write here.'],
  [/network.*unreachable|network.*error/i, 'A network error occurred. Check your connection.'],
  [/cannot create output/i, 'The file could not be saved.'],
  [/cannot open source/i, 'The source file could not be opened.'],
  [/copy failed/i, 'The file could not be copied.'],
]

export function friendlyError(raw) {
  if (!raw) return 'An unknown error occurred.'
  const msg = typeof raw === 'string' ? raw : raw.message ?? String(raw)
  for (const [re, friendly] of patterns) {
    if (re.test(msg)) return friendly
  }
  // Strip file paths (C:\..., /tmp/...) and Go prefixes for anything unmatched
  return msg
    .replace(/cannot create output:\s*/i, '')
    .replace(/open\s+\S+:\s*/i, '')
    .replace(/[A-Z]:\\[^\s:]+/g, '')
    .replace(/\/tmp\/[^\s:]+/g, '')
    .trim() || 'An unexpected error occurred.'
}
