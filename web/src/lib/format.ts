import { errorMessage } from './load'

export function formatBytes(bytes?: number) {
  if (!bytes) return '0 B'
  const units = ['B', 'KiB', 'MiB', 'GiB', 'TiB']
  let n = bytes
  let i = 0
  while (n >= 1024 && i < units.length - 1) {
    n /= 1024
    i++
  }
  return `${n.toFixed(i ? 1 : 0)} ${units[i]}`
}

export function csv(text: string) {
  return text
    .split(',')
    .map((s) => s.trim())
    .filter(Boolean)
}

export async function act(fn: () => Promise<void>) {
  try {
    await fn()
  } catch (err) {
    alert(errorMessage(err))
  }
}
