// Per-browser UI preferences. Storage can be unavailable (private mode,
// blocked site data), so every access is guarded and has a default.

export type Theme = 'light' | 'dark' | 'system'

function read(key: string): string | null {
  try {
    return localStorage.getItem(key)
  } catch {
    return null
  }
}

function write(key: string, value: string) {
  try {
    localStorage.setItem(key, value)
  } catch {
    /* preference just won't persist */
  }
}

export function getTheme(): Theme {
  const v = read('panel.theme')
  return v === 'light' || v === 'dark' ? v : 'system'
}

export function applyTheme(theme: Theme) {
  const dark = theme === 'dark' || (theme === 'system' && matchMedia('(prefers-color-scheme: dark)').matches)
  document.documentElement.classList.toggle('dark', dark)
}

export function setTheme(theme: Theme) {
  write('panel.theme', theme)
  applyTheme(theme)
}

export function getLanguage(): 'ru' | 'en' {
  const v = read('panel.lang')
  if (v === 'ru' || v === 'en') return v
  return navigator.language.toLowerCase().startsWith('ru') ? 'ru' : 'en'
}

export function setLanguage(lang: 'ru' | 'en') {
  write('panel.lang', lang)
}
