// Zoom presets modeled after Adobe Acrobat (22 steps, 1% → 6400%).
// Values are scale factors (1.0 = 100%).
export const ZOOM_LEVELS = [
  0.01, 0.0833, 0.125, 0.25, 0.3333, 0.5, 0.6667, 0.75,
  1.0, 1.25, 1.5, 2.0, 3.0, 4.0, 6.0, 8.0,
  12.0, 16.0, 24.0, 32.0, 48.0, 64.0,
]

export const ZOOM_MIN = ZOOM_LEVELS[0]
export const ZOOM_MAX = ZOOM_LEVELS[ZOOM_LEVELS.length - 1]

// Return the next zoom level up from the current value.
export function zoomIn(current) {
  for (const z of ZOOM_LEVELS) {
    if (z > current + 0.001) return z
  }
  return ZOOM_MAX
}

// Return the next zoom level down from the current value.
export function zoomOut(current) {
  for (let i = ZOOM_LEVELS.length - 1; i >= 0; i--) {
    if (ZOOM_LEVELS[i] < current - 0.001) return ZOOM_LEVELS[i]
  }
  return ZOOM_MIN
}

// Clamp an arbitrary zoom value to [ZOOM_MIN, ZOOM_MAX].
export function clampZoom(z) {
  return Math.max(ZOOM_MIN, Math.min(ZOOM_MAX, z))
}
