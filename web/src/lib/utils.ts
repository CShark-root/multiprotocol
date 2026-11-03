import { clsx, type ClassValue } from 'clsx'
import { twMerge } from 'tailwind-merge'

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs))
}

export const GiB = 1024 ** 3

export function formatBytes(bytes: number, locale: string): string {
  if (bytes <= 0) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  const i = Math.min(units.length - 1, Math.floor(Math.log(bytes) / Math.log(1024)))
  const value = bytes / 1024 ** i
  return `${value.toLocaleString(locale, { maximumFractionDigits: value < 10 ? 1 : 0 })} ${units[i]}`
}

export function formatDate(iso: string, locale: string): string {
  return new Date(iso).toLocaleDateString(locale, { day: 'numeric', month: 'short', year: 'numeric' })
}

export function formatDuration(seconds: number, locale: string): string {
  const days = Math.floor(seconds / 86400)
  const hours = Math.floor((seconds % 86400) / 3600)
  const minutes = Math.floor((seconds % 3600) / 60)
  const ru = locale.startsWith('ru')
  if (days > 0) return ru ? `${days} д ${hours} ч` : `${days}d ${hours}h`
  if (hours > 0) return ru ? `${hours} ч ${minutes} мин` : `${hours}h ${minutes}m`
  return ru ? `${minutes} мин` : `${minutes}m`
}

/** ISO timestamp -> "YYYY-MM-DD" in local time, for <input type="date">. */
export function toDateInput(iso: string | null): string {
  if (!iso) return ''
  const d = new Date(iso)
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`
}

/** "YYYY-MM-DD" -> ISO timestamp at the end of that local day. */
export function fromDateInput(value: string): string | null {
  if (!value) return null
  return new Date(`${value}T23:59:59`).toISOString()
}
