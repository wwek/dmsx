import { ApiError } from './client'

function fileNameFromDisposition(header: string | null, fallback: string): string {
  if (!header) return fallback
  const star = /filename\*=(?:UTF-8''|utf-8'')([^;]+)/i.exec(header)
  if (star) {
    try {
      return decodeURIComponent(star[1].trim().replace(/^"|"$/g, ''))
    } catch {
      return star[1].trim()
    }
  }
  const plain = /filename="?([^";]+)"?/i.exec(header)
  return plain ? plain[1] : fallback
}

/** Authenticated download that surfaces API errors instead of saving JSON bodies. */
export async function downloadAuth(path: string, fallbackName: string): Promise<void> {
  const response = await fetch(path)
  if (!response.ok) {
    let message = response.statusText
    try {
      const body = await response.json()
      if (body?.error) message = String(body.error)
    } catch {
      /* ignore */
    }
    throw new ApiError(response.status, message)
  }
  const blob = await response.blob()
  const name = fileNameFromDisposition(response.headers.get('Content-Disposition'), fallbackName)
  const url = URL.createObjectURL(blob)
  const anchor = document.createElement('a')
  anchor.href = url
  anchor.download = name
  anchor.click()
  URL.revokeObjectURL(url)
}

/** Open a text API response in a new tab without leaving the console. */
export async function openTextAuth(path: string): Promise<void> {
  const response = await fetch(path)
  if (!response.ok) {
    let message = response.statusText
    try {
      const body = await response.json()
      if (body?.error) message = String(body.error)
    } catch {
      /* ignore */
    }
    throw new ApiError(response.status, message)
  }
  const text = await response.text()
  const blob = new Blob([text], { type: 'text/plain; charset=utf-8' })
  const url = URL.createObjectURL(blob)
  window.open(url, '_blank', 'noopener,noreferrer')
  setTimeout(() => URL.revokeObjectURL(url), 60_000)
}
