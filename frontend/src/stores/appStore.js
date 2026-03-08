import { create } from 'zustand'

let statusTimer = null
let tabCounter  = 0

function newTabId() { return `tab_${++tabCounter}` }

// Sync the top-level document/currentPage/zoom mirrors from a tab object.
// This means all existing consumers (panels, hooks, toolbar) keep working
// without changes — they just read the mirrors as before.
function mirrors(tab) {
  return {
    document:    tab?.document    ?? null,
    currentPage: tab?.currentPage ?? 1,
    zoom:        tab?.zoom        ?? 1.0,
  }
}

export const useAppStore = create((set, get) => ({
  // ── Tabs ──────────────────────────────────────────────────────────────────
  tabs:        [{ id: 'tab_0', document: null, currentPage: 1, zoom: 1.0, undoStack: [] }],
  activeTabId: 'tab_0',

  // ── Mirrors of active tab (read by all existing consumers) ────────────────
  document:    null,
  currentPage: 1,
  zoom:        1.0,

  // ── Global UI state ───────────────────────────────────────────────────────
  recentFiles:   [],
  sidebarOpen:   true,
  toolbarMode:   'view',
  activeTool:    null,
  statusMessage: null,

  // ── Tab actions ───────────────────────────────────────────────────────────
  newTab: () => {
    const id  = newTabId()
    const tab = { id, document: null, currentPage: 1, zoom: 1.0, undoStack: [] }
    const tabs = [...get().tabs, tab]
    set({ tabs, activeTabId: id, ...mirrors(tab) })
  },

  openTab: (doc) => {
    const originalPath = doc.originalPath ?? doc.path

    // If this file is already open, just switch to it
    const existing = get().tabs.find(t => t.document?.originalPath === originalPath)
    if (existing) {
      set({ activeTabId: existing.id, ...mirrors(existing) })
      return
    }

    const { tabs, activeTabId } = get()
    const activeTab = tabs.find(t => t.id === activeTabId)

    // Fill the active tab if it's blank, otherwise open a new one
    if (activeTab && !activeTab.document) {
      const updated = { ...activeTab, document: { ...doc, originalPath }, currentPage: 1, zoom: 1.0 }
      const newTabs = tabs.map(t => t.id === activeTabId ? updated : t)
      set({ tabs: newTabs, ...mirrors(updated) })
      return
    }

    const id  = newTabId()
    const tab = { id, document: { ...doc, originalPath }, currentPage: 1, zoom: 1.0, tempSlot: undefined, undoStack: [] }
    const newTabs = [...tabs, tab]
    set({ tabs: newTabs, activeTabId: id, ...mirrors(tab) })
  },

  closeTab: (id) => {
    const { tabs, activeTabId } = get()

    // If this is the last tab, clear it rather than removing it
    if (tabs.length === 1) {
      const blank = { ...tabs[0], document: null, currentPage: 1, zoom: 1.0, tempSlot: undefined }
      set({ tabs: [blank], activeTabId: blank.id, ...mirrors(blank) })
      return
    }

    const idx     = tabs.findIndex(t => t.id === id)
    const newTabs = tabs.filter(t => t.id !== id)

    let newActiveId = activeTabId
    if (activeTabId === id) {
      const next = newTabs[idx] ?? newTabs[idx - 1] ?? null
      newActiveId = next?.id ?? null
    }

    const activeTab = newTabs.find(t => t.id === newActiveId) ?? null
    set({ tabs: newTabs, activeTabId: newActiveId, ...mirrors(activeTab) })
  },

  setActiveTab: (id) => {
    const tab = get().tabs.find(t => t.id === id)
    if (!tab) return
    set({ activeTabId: id, ...mirrors(tab) })
  },

  // ── Per-tab document/page/zoom setters (update tab + mirrors) ─────────────
  setDocument: (doc) => {
    const { tabs, activeTabId } = get()
    const originalPath = doc.originalPath ?? doc.path
    const updated      = { ...doc, originalPath }
    const newTabs = tabs.map(t =>
      t.id === activeTabId ? { ...t, document: updated, currentPage: 1, tempSlot: doc.tempSlot } : t
    )
    const activeTab = newTabs.find(t => t.id === activeTabId)
    set({ tabs: newTabs, ...mirrors(activeTab) })
  },

  setCurrentPage: (page) => {
    const { tabs, activeTabId } = get()
    const newTabs = tabs.map(t => t.id === activeTabId ? { ...t, currentPage: page } : t)
    set({ tabs: newTabs, currentPage: page })
  },

  setZoom: (z) => {
    const clamped = Math.max(0.01, Math.min(64, z))
    const { tabs, activeTabId } = get()
    const newTabs = tabs.map(t => t.id === activeTabId ? { ...t, zoom: clamped } : t)
    set({ tabs: newTabs, zoom: clamped })
  },

  // Push current document state onto the undo stack before an operation
  pushUndo: () => {
    const { tabs, activeTabId } = get()
    const activeTab = tabs.find(t => t.id === activeTabId)
    if (!activeTab?.document) return
    const snapshot = { path: activeTab.document.path, tempSlot: activeTab.document.tempSlot }
    const undoStack = [...(activeTab.undoStack ?? []), snapshot].slice(-20) // max 20 levels
    const newTabs = tabs.map(t => t.id === activeTabId ? { ...t, undoStack } : t)
    set({ tabs: newTabs })
  },

  undo: () => {
    const { tabs, activeTabId } = get()
    const activeTab = tabs.find(t => t.id === activeTabId)
    if (!activeTab?.undoStack?.length) return null
    const stack    = [...activeTab.undoStack]
    const snapshot = stack.pop()
    const newTabs  = tabs.map(t => t.id === activeTabId ? { ...t, undoStack: stack } : t)
    set({ tabs: newTabs })
    return snapshot // caller reloads the doc from snapshot.path
  },

  // ── Global UI actions ─────────────────────────────────────────────────────
  setSidebarOpen:  (open) => set({ sidebarOpen: open }),
  setToolbarMode:  (mode) => set({ toolbarMode: mode, activeTool: null }),
  setActiveTool:   (tool) => set({ activeTool: tool }),

  addRecentFile: (path) => {
    const updated = [path, ...get().recentFiles.filter(f => f !== path)].slice(0, 10)
    set({ recentFiles: updated })
  },

  // ── Status ────────────────────────────────────────────────────────────────
  startOperation: (title) => {
    if (statusTimer) clearTimeout(statusTimer)
    set({ statusMessage: { message: title + '…', type: 'working' } })
  },
  finishOperation: (message = 'Done') => {
    if (statusTimer) clearTimeout(statusTimer)
    set({ statusMessage: { message, type: 'done' } })
    statusTimer = setTimeout(() => set({ statusMessage: null }), 3000)
  },
  failOperation: (message) => {
    if (statusTimer) clearTimeout(statusTimer)
    set({ statusMessage: { message, type: 'error' } })
  },
}))
