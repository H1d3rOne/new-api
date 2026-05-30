export type TrafficBodyPreviewMode = 'text' | 'all'

type PreviewPhase = 'request' | 'response'

export interface TrafficBodyPreview {
  text: string
  readOnly?: boolean
  apply: (text: string) => string | null
}

interface TextSegment {
  target: Record<string, unknown>
  key: string
}

interface EventStreamBlock {
  json?: Record<string, unknown>
  rawData: string
}

interface ToolCallSegment {
  target: Record<string, unknown>
  key: 'tool_calls' | 'function_call'
}

const TOOL_CALLS_LABEL = 'tool_calls:'

export function createTrafficBodyPreview(
  value: string,
  phase: PreviewPhase,
  mode: TrafficBodyPreviewMode = 'text'
): TrafficBodyPreview | null {
  return phase === 'request'
    ? createRequestBodyPreview(value, mode)
    : createResponseBodyPreview(value, mode)
}

export function collectTrafficResponseFunctionNames(value: string): string[] {
  const names = new Set<string>()
  if (/^\s*(event:|data:)/m.test(value)) {
    const streamBlocks = parseEventStreamBlocks(value)
    streamBlocks.forEach((block) => {
      if (!block.json) return
      collectResponseToolCallSegments(block.json)
        .flatMap((segment) => segmentToolCalls(segment))
        .forEach((toolCall) => addToolCallName(toolCall, names))
    })
    return Array.from(names)
  }

  const parsed = parseJsonObject(value)
  if (parsed) {
    collectResponseToolCallSegments(parsed)
      .flatMap((segment) => segmentToolCalls(segment))
      .forEach((toolCall) => addToolCallName(toolCall, names))
  }
  return Array.from(names)
}

export function createTrafficResponseContentRewrite(value: string): string {
  return createTrafficBodyPreview(value, 'response', 'text')?.text || ''
}

export function createTrafficResponseToolCallsRewrite(value: string): string {
  const toolCalls = collectTrafficResponseToolCalls(value)
  return toolCalls.length > 0 ? formatToolCallsPreview(toolCalls) : ''
}

export function collectTrafficResponseToolCalls(value: string): unknown[] {
  const toolCalls: unknown[] = []
  if (/^\s*(event:|data:)/m.test(value)) {
    parseEventStreamBlocks(value).forEach((block) => {
      if (!block.json) return
      collectResponseToolCallSegments(block.json)
        .flatMap((segment) => segmentToolCalls(segment))
        .forEach((toolCall) => toolCalls.push(toolCall))
    })
    return mergeToolCalls(toolCalls)
  }

  const parsed = parseJsonObject(value)
  if (parsed) {
    collectResponseToolCallSegments(parsed)
      .flatMap((segment) => segmentToolCalls(segment))
      .forEach((toolCall) => toolCalls.push(toolCall))
  }
  return mergeToolCalls(toolCalls)
}

function createRequestBodyPreview(
  value: string,
  mode: TrafficBodyPreviewMode
): TrafficBodyPreview | null {
  const parsed = parseJsonObject(value)
  if (!parsed) return null

  const contentPreview =
    previewStringField(parsed, 'input') ||
    previewStringField(parsed, 'prompt') ||
    previewLatestUserMessage(parsed) ||
    (Array.isArray(parsed.input)
      ? previewLatestMessageInArray(parsed, parsed.input)
      : null)

  if (mode === 'text') return contentPreview

  const toolCallSegments = collectRequestToolCallSegments(parsed)
  const toolCalls = toolCallSegments.flatMap((segment) =>
    segmentToolCalls(segment)
  )
  const text = buildPreviewText(contentPreview?.text || '', toolCalls)
  if (!text) return null
  return {
    text,
    apply: (nextText) => {
      const parts = splitPreviewText(nextText)
      if (contentPreview) {
        contentPreview.apply(parts.content)
      }
      if (parts.toolCalls !== null) {
        const nextToolCalls = parseToolCallsPreview(parts.toolCalls)
        if (nextToolCalls === null) return null
        applyToolCallSegments(toolCallSegments, nextToolCalls)
      }
      return JSON.stringify(parsed, null, 2)
    },
  }
}

function previewStringField(
  parsed: Record<string, unknown>,
  key: string
): TrafficBodyPreview | null {
  if (typeof parsed[key] !== 'string') return null
  return {
    text: parsed[key],
    apply: (text) => {
      parsed[key] = text
      return JSON.stringify(parsed, null, 2)
    },
  }
}

function previewLatestUserMessage(
  parsed: Record<string, unknown>
): TrafficBodyPreview | null {
  const messages = parsed.messages
  if (!Array.isArray(messages)) return null

  for (let index = messages.length - 1; index >= 0; index -= 1) {
    const message = messages[index]
    if (!isRecord(message) || message.role !== 'user') continue
    const contentPreview = previewMessageContent(message)
    if (!contentPreview) return null
    return {
      text: contentPreview.text,
      apply: (text) => {
        contentPreview.apply(text)
        return JSON.stringify(parsed, null, 2)
      },
    }
  }

  return null
}

function previewLatestMessageInArray(
  root: Record<string, unknown>,
  messages: unknown[]
): TrafficBodyPreview | null {
  for (let index = messages.length - 1; index >= 0; index -= 1) {
    const message = messages[index]
    if (!isRecord(message) || message.role !== 'user') continue
    const contentPreview = previewMessageContent(message)
    if (!contentPreview) return null
    return {
      text: contentPreview.text,
      apply: (text) => {
        contentPreview.apply(text)
        return JSON.stringify(root, null, 2)
      },
    }
  }
  return null
}

function previewMessageContent(message: Record<string, unknown>): {
  text: string
  apply: (text: string) => void
} | null {
  if (typeof message.content === 'string') {
    return {
      text: message.content,
      apply: (text) => {
        message.content = text
      },
    }
  }

  if (!Array.isArray(message.content)) return null
  const textParts = message.content.filter(
    (part): part is Record<string, unknown> =>
      isRecord(part) &&
      (typeof part.text === 'string' || typeof part.input_text === 'string')
  )
  if (textParts.length === 0) return null

  return {
    text: textParts
      .map((part) =>
        typeof part.text === 'string' ? part.text : String(part.input_text)
      )
      .join('\n'),
    apply: (text) => {
      textParts.forEach((part, index) => {
        const key = typeof part.text === 'string' ? 'text' : 'input_text'
        part[key] = index === 0 ? text : ''
      })
    },
  }
}

function createResponseBodyPreview(
  value: string,
  mode: TrafficBodyPreviewMode
): TrafficBodyPreview | null {
  const streamPreview = createEventStreamPreview(value, mode)
  if (streamPreview) return streamPreview

  const parsed = parseJsonObject(value)
  if (!parsed) return null
  const segments = collectResponseTextSegments(parsed)
  const toolCallSegments = collectResponseToolCallSegments(parsed)
  const text = responsePreviewText(segments, toolCallSegments, mode)
  if (!text) return null
  return {
    text,
    apply: (nextText) => {
      if (mode === 'all') {
        const parts = splitPreviewText(nextText)
        const nextToolCalls =
          parts.toolCalls === null
            ? undefined
            : parseToolCallsPreview(parts.toolCalls)
        if (nextToolCalls === null) return null
        applyResponsePreviewText(segments, parts.content)
        if (nextToolCalls)
          applyToolCallSegments(toolCallSegments, nextToolCalls)
      } else {
        applyResponsePreviewText(segments, nextText)
      }
      return JSON.stringify(parsed, null, 2)
    },
  }
}

function createEventStreamPreview(
  value: string,
  mode: TrafficBodyPreviewMode
): TrafficBodyPreview | null {
  if (!/^\s*(event:|data:)/m.test(value)) return null

  const blocks = parseEventStreamBlocks(value)
  const segments: Array<TextSegment & { block: EventStreamBlock }> = []
  const toolCallSegments: ToolCallSegment[] = []

  blocks.forEach((block) => {
    if (!block.json) return
    collectResponseTextSegments(block.json).forEach((segment) => {
      segments.push({ block, ...segment })
    })
    if (mode === 'all') {
      toolCallSegments.push(...collectResponseToolCallSegments(block.json))
    }
  })

  const contentText = segments
    .map((segment) => String(segment.target[segment.key]))
    .join('')
  const text =
    mode === 'all'
      ? buildPreviewText(
          contentText,
          toolCallSegments.flatMap((segment) => segmentToolCalls(segment))
        )
      : contentText
  if (!text) return null

  return {
    text,
    apply: (nextText) => {
      if (mode === 'all') {
        const parts = splitPreviewText(nextText)
        const nextToolCalls =
          parts.toolCalls === null
            ? undefined
            : parseToolCallsPreview(parts.toolCalls)
        if (nextToolCalls === null) return null
        applyResponsePreviewText(segments, parts.content)
        if (nextToolCalls)
          applyToolCallSegments(toolCallSegments, nextToolCalls)
      } else {
        applyResponsePreviewText(segments, nextText)
      }
      const body = blocks
        .map((block) => {
          if (!block.json) return `data: ${block.rawData}`
          return `data: ${JSON.stringify(block.json)}`
        })
        .join('\n\n')
      return `${body}\n\n`
    },
  }
}

function parseEventStreamBlocks(value: string): EventStreamBlock[] {
  return value
    .split(/\r?\n\r?\n/)
    .map((block) => block.trim())
    .filter(Boolean)
    .map((block) => {
      const data = eventBlockData(block)
      if (!data || data === '[DONE]') return { rawData: data || block }
      const parsed = parseJsonObject(data)
      return parsed ? { rawData: data, json: parsed } : { rawData: data }
    })
}

function eventBlockData(block: string): string {
  const lines = block.split(/\r?\n/)
  const dataLines: string[] = []
  let readingData = false

  lines.forEach((line) => {
    if (line.startsWith('data:')) {
      dataLines.push(line.slice(5).trimStart())
      readingData = true
      return
    }
    if (readingData && !/^[a-zA-Z-]+:/.test(line)) {
      dataLines.push(line)
    }
  })

  return dataLines.join('\n').trim()
}

function responsePreviewText(
  segments: TextSegment[],
  toolCallSegments: ToolCallSegment[],
  mode: TrafficBodyPreviewMode
): string {
  const contentText = segments
    .map((segment) => String(segment.target[segment.key]))
    .join('')
  if (mode === 'text') return contentText
  return buildPreviewText(
    contentText,
    toolCallSegments.flatMap((segment) => segmentToolCalls(segment))
  )
}

function collectResponseTextSegments(
  parsed: Record<string, unknown>
): TextSegment[] {
  const segments: TextSegment[] = []

  const choices = parsed.choices
  if (Array.isArray(choices)) {
    choices.forEach((choice) => {
      if (!isRecord(choice)) return
      collectTextField(choice.delta, 'content', segments)
      collectTextField(choice.message, 'content', segments)
      collectTextField(choice, 'text', segments)
    })
  }

  collectTextField(parsed, 'output_text', segments)
  collectTextField(parsed, 'text', segments)
  collectTextField(parsed, 'content', segments)

  return segments
}

function collectTextField(
  value: unknown,
  key: string,
  segments: TextSegment[]
) {
  if (!isRecord(value) || typeof value[key] !== 'string') return
  if (value[key] === '') return
  segments.push({ target: value, key })
}

function applyResponsePreviewText(segments: TextSegment[], text: string) {
  segments.forEach((segment) => {
    segment.target[segment.key] = ''
  })

  if (segments[0]) {
    segments[0].target[segments[0].key] = text
  }
}

function collectRequestToolCallSegments(
  parsed: Record<string, unknown>
): ToolCallSegment[] {
  const latestUserMessage =
    latestRoleMessage(parsed.messages, 'user') ||
    latestRoleMessage(parsed.input, 'user')
  return latestUserMessage ? collectToolCallSegments(latestUserMessage) : []
}

function collectResponseToolCallSegments(
  parsed: Record<string, unknown>
): ToolCallSegment[] {
  const segments: ToolCallSegment[] = []
  const choices = parsed.choices
  if (Array.isArray(choices)) {
    choices.forEach((choice) => {
      if (!isRecord(choice)) return
      segments.push(...collectToolCallSegments(choice.delta))
      segments.push(...collectToolCallSegments(choice.message))
      if (!choice.delta && !choice.message) {
        segments.push(...collectToolCallSegments(choice))
      }
    })
    return segments
  }
  return collectToolCallSegments(parsed)
}

function latestRoleMessage(
  input: unknown,
  role: string
): Record<string, unknown> | null {
  if (!Array.isArray(input)) return null
  for (let index = input.length - 1; index >= 0; index -= 1) {
    const message = input[index]
    if (!isRecord(message) || message.role !== role) continue
    return message
  }
  return null
}

function collectToolCallSegments(value: unknown): ToolCallSegment[] {
  const out: ToolCallSegment[] = []
  collectToolCallSegmentsInto(value, out)
  return out
}

function collectToolCallSegmentsInto(value: unknown, out: ToolCallSegment[]) {
  if (Array.isArray(value)) {
    value.forEach((item) => collectToolCallSegmentsInto(item, out))
    return
  }
  if (!isRecord(value)) return

  if (Array.isArray(value.tool_calls)) {
    out.push({ target: value, key: 'tool_calls' })
  }

  if (value.function_call) {
    out.push({ target: value, key: 'function_call' })
  }

  Object.entries(value).forEach(([key, child]) => {
    if (key === 'tool_calls' || key === 'function_call') return
    collectToolCallSegmentsInto(child, out)
  })
}

function addToolCallName(value: unknown, out: Set<string>) {
  if (!isRecord(value)) return
  if (typeof value.name === 'string' && value.name) {
    out.add(value.name)
  }
  if (isRecord(value.function) && typeof value.function.name === 'string') {
    out.add(value.function.name)
  }
}

function segmentToolCalls(segment: ToolCallSegment): unknown[] {
  const value = segment.target[segment.key]
  if (segment.key === 'tool_calls') {
    return Array.isArray(value) ? value : []
  }
  return value === undefined || value === null ? [] : [value]
}

function applyToolCallSegments(
  segments: ToolCallSegment[],
  toolCalls: unknown[]
) {
  segments.forEach((segment, index) => {
    if (segment.key === 'tool_calls') {
      segment.target.tool_calls = index === 0 ? toolCalls : []
      return
    }
    if (index === 0 && toolCalls.length > 0) {
      segment.target.function_call = toolCalls[0]
    } else {
      delete segment.target.function_call
    }
  })
}

function buildPreviewText(content: string, toolCalls: unknown[]): string {
  const parts: string[] = []
  if (content) parts.push(content)
  if (toolCalls.length > 0) {
    parts.push(`${TOOL_CALLS_LABEL}\n${formatToolCallsPreview(toolCalls)}`)
  }
  return parts.join('\n\n')
}

function splitPreviewText(value: string): {
  content: string
  toolCalls: string | null
} {
  const separator = `\n\n${TOOL_CALLS_LABEL}\n`
  const separatorIndex = value.indexOf(separator)
  if (separatorIndex >= 0) {
    return {
      content: value.slice(0, separatorIndex),
      toolCalls: value.slice(separatorIndex + separator.length),
    }
  }
  const trimmedStart = value.trimStart()
  if (trimmedStart.startsWith(`${TOOL_CALLS_LABEL}\n`)) {
    const offset = value.length - trimmedStart.length
    return {
      content: value.slice(0, offset),
      toolCalls: trimmedStart.slice(TOOL_CALLS_LABEL.length + 1),
    }
  }
  return { content: value, toolCalls: null }
}

function parseToolCallsPreview(value: string): unknown[] | null {
  const trimmed = value.trim()
  if (!trimmed) return []
  try {
    const parsed = JSON.parse(trimmed) as unknown
    return Array.isArray(parsed) ? parsed : [parsed]
  } catch {
    return null
  }
}

function formatToolCallsPreview(toolCalls: unknown[]): string {
  return JSON.stringify(mergeToolCalls(toolCalls), null, 2)
}

function mergeToolCalls(toolCalls: unknown[]): unknown[] {
  const out: unknown[] = []
  const keyed = new Map<string, Record<string, unknown>>()

  toolCalls.forEach((toolCall) => {
    if (!isRecord(toolCall)) {
      out.push(toolCall)
      return
    }
    const key = toolCallMergeKey(toolCall)
    const clone = cloneJsonRecord(toolCall)
    if (!key) {
      out.push(clone)
      return
    }
    const existing = keyed.get(key)
    if (!existing) {
      keyed.set(key, clone)
      out.push(clone)
      return
    }
    mergeToolCallRecord(existing, clone)
  })

  return out
}

function toolCallMergeKey(value: Record<string, unknown>): string {
  if (value.index !== undefined) return `index:${String(value.index)}`
  if (typeof value.id === 'string' && value.id) return `id:${value.id}`
  return ''
}

function mergeToolCallRecord(
  target: Record<string, unknown>,
  next: Record<string, unknown>
) {
  Object.entries(next).forEach(([key, value]) => {
    if (value === undefined || value === null || value === '') return
    if (key === 'function' && isRecord(target.function) && isRecord(value)) {
      mergeFunctionRecord(target.function, value)
      return
    }
    target[key] = value
  })
}

function mergeFunctionRecord(
  target: Record<string, unknown>,
  next: Record<string, unknown>
) {
  Object.entries(next).forEach(([key, value]) => {
    if (value === undefined || value === null || value === '') return
    if (
      key === 'arguments' &&
      typeof target.arguments === 'string' &&
      typeof value === 'string'
    ) {
      target.arguments += value
      return
    }
    target[key] = value
  })
}

function cloneJsonRecord(
  value: Record<string, unknown>
): Record<string, unknown> {
  try {
    const cloned = JSON.parse(JSON.stringify(value)) as unknown
    return isRecord(cloned) ? cloned : { value: cloned }
  } catch {
    return { ...value }
  }
}

function parseJsonObject(value: string): Record<string, unknown> | null {
  try {
    const parsed = JSON.parse(value)
    return isRecord(parsed) ? parsed : null
  } catch {
    return null
  }
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return Boolean(value) && typeof value === 'object' && !Array.isArray(value)
}
