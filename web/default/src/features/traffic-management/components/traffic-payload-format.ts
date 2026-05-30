export function formatTrafficPayload(value: string): string {
  const raw = value || ''
  const trimmed = raw.trim()
  if (!trimmed) return raw
  try {
    return JSON.stringify(JSON.parse(trimmed), null, 2)
  } catch {
    return formatEventStream(raw)
  }
}

function formatEventStream(value: string): string {
  const lines = value.split(/\r?\n/)
  let changed = false
  const formatted = lines.map((line) => {
    if (!line.startsWith('data:')) return line
    const data = line.slice(5).trimStart()
    if (!data || data === '[DONE]') return line
    try {
      const json = JSON.stringify(JSON.parse(data), null, 2)
      changed = true
      return `data: ${json}`
    } catch {
      return line
    }
  })
  if (!changed) {
    throw new Error('payload is not JSON or formatted event-stream JSON')
  }
  return formatted.join('\n')
}
