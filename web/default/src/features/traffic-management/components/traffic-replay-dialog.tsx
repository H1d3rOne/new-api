/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { useEffect, useMemo, useRef, useState } from 'react'
import { Braces, SendHorizontal } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { Textarea } from '@/components/ui/textarea'
import { CopyButton } from '@/components/copy-button'
import { StatusBadge } from '@/components/status-badge'
import { replayTrafficLog } from '../api'
import type { TrafficLog, TrafficReplayResponse } from '../types'
import { TrafficBodySearch } from './traffic-body-search'
import { formatBytes, getStatusVariant } from './traffic-format'
import { formatTrafficPayload } from './traffic-payload-format'

interface TrafficReplayDialogProps {
  log: TrafficLog | null
  open: boolean
  onOpenChange: (open: boolean) => void
}

function parseHeaderText(text: string): Record<string, string> {
  const trimmed = text.trim()
  if (!trimmed) return {}
  const parsed = JSON.parse(trimmed) as Record<string, unknown>
  if (!parsed || Array.isArray(parsed) || typeof parsed !== 'object') {
    throw new Error('headers must be an object')
  }
  const headers: Record<string, string> = {}
  Object.entries(parsed).forEach(([key, value]) => {
    if (value === undefined || value === null) return
    headers[key] = String(value)
  })
  return headers
}

function prettyHeaders(headers?: Record<string, string>): string {
  if (!headers || Object.keys(headers).length === 0) return '{}'
  return JSON.stringify(headers, null, 2)
}

function headerContentType(headers: Record<string, string>): string {
  for (const [key, value] of Object.entries(headers)) {
    if (key.toLowerCase() === 'content-type') return value
  }
  return ''
}

function ReplayResponsePanel(props: {
  response: TrafficReplayResponse | null
}) {
  const { t } = useTranslation()
  const bodyRef = useRef<HTMLTextAreaElement | null>(null)
  const response = props.response
  const [formattedBody, setFormattedBody] = useState<string | null>(null)
  const body = formattedBody ?? response?.body ?? ''
  const headers = useMemo(
    () => prettyHeaders(response?.headers),
    [response?.headers]
  )

  useEffect(() => {
    setFormattedBody(null)
  }, [response])

  const handleFormatBody = () => {
    if (formattedBody !== null) {
      setFormattedBody(null)
      return
    }
    try {
      setFormattedBody(formatTrafficPayload(response?.body || ''))
    } catch {
      toast.error(t('Invalid JSON'))
    }
  }

  if (!response) {
    return (
      <div className='text-muted-foreground rounded-lg border border-dashed p-4 text-sm'>
        {t('Replay response will appear here after sending.')}
      </div>
    )
  }

  const sizeMeta = `${formatBytes(response.body_size)}${
    response.body_truncated
      ? ` · ${t('truncated {{bytes}}', {
          bytes: formatBytes(response.truncated_bytes),
        })}`
      : ''
  }`

  return (
    <div className='space-y-3'>
      <div className='flex flex-wrap items-center gap-2'>
        <StatusBadge
          label={String(response.status_code)}
          variant={getStatusVariant(response.status_code)}
          copyable={false}
        />
        <Badge variant='outline'>{response.duration_ms}ms</Badge>
        <Badge variant='outline'>{sizeMeta}</Badge>
        {response.content_type && (
          <Badge variant='secondary' className='max-w-[260px] truncate'>
            {response.content_type}
          </Badge>
        )}
      </div>
      <Tabs defaultValue='body'>
        <TabsList>
          <TabsTrigger value='body'>{t('Response Body')}</TabsTrigger>
          <TabsTrigger value='headers'>{t('Response Headers')}</TabsTrigger>
        </TabsList>
        <TabsContent value='body' className='space-y-2'>
          <div className='flex flex-wrap items-center justify-end gap-2'>
            <TrafficBodySearch value={body} textareaRef={bodyRef} />
            <Button
              type='button'
              variant='outline'
              size='sm'
              onClick={handleFormatBody}
            >
              <Braces className='size-4' />
              {formattedBody !== null ? t('Cancel Format') : t('Format JSON')}
            </Button>
            <CopyButton value={body} size='icon' tooltip={t('Copy')} />
          </div>
          <Textarea
            ref={bodyRef}
            value={body}
            readOnly
            placeholder={t('Empty body')}
            className='bg-background text-foreground max-h-[320px] min-h-[220px] resize-y overflow-auto font-mono text-xs leading-relaxed'
          />
        </TabsContent>
        <TabsContent value='headers' className='space-y-2'>
          <div className='flex justify-end'>
            <CopyButton value={headers} size='icon' tooltip={t('Copy')} />
          </div>
          <pre className='bg-background text-foreground max-h-[320px] min-h-[220px] overflow-auto rounded-lg border p-3 font-mono text-xs leading-relaxed break-words whitespace-pre-wrap'>
            {headers}
          </pre>
        </TabsContent>
      </Tabs>
    </div>
  )
}

export function TrafficReplayDialog(props: TrafficReplayDialogProps) {
  const { t } = useTranslation()
  const bodyRef = useRef<HTMLTextAreaElement | null>(null)
  const [method, setMethod] = useState('POST')
  const [requestURL, setRequestURL] = useState('')
  const [headersText, setHeadersText] = useState('{}')
  const [body, setBody] = useState('')
  const [sending, setSending] = useState(false)
  const [response, setResponse] = useState<TrafficReplayResponse | null>(null)
  const [headersFormatOriginal, setHeadersFormatOriginal] = useState<
    string | null
  >(null)
  const [bodyFormatOriginal, setBodyFormatOriginal] = useState<string | null>(
    null
  )

  const formatEditableJson = (
    value: string,
    onChange: (value: string) => void,
    formatOriginal: string | null,
    setFormatOriginal: (value: string | null) => void
  ) => {
    if (formatOriginal !== null) {
      onChange(formatOriginal)
      setFormatOriginal(null)
      return
    }
    try {
      setFormatOriginal(value)
      onChange(formatTrafficPayload(value))
    } catch {
      toast.error(t('Invalid JSON'))
    }
  }

  useEffect(() => {
    if (!props.open || !props.log) return
    setMethod(props.log.method || 'POST')
    setRequestURL(props.log.request_url || props.log.path || '')
    setHeadersText(props.log.request_headers || '{}')
    setBody(props.log.request_body || '')
    setResponse(null)
    setHeadersFormatOriginal(null)
    setBodyFormatOriginal(null)
  }, [props.open, props.log])

  const handleReplay = async () => {
    if (!props.log) return
    let headers: Record<string, string>
    try {
      headers = parseHeaderText(headersText)
    } catch {
      toast.error(t('Request headers must be a JSON object'))
      return
    }

    setSending(true)
    try {
      const result = await replayTrafficLog(props.log.id, {
        method,
        request_url: requestURL,
        headers,
        body,
        content_type:
          headerContentType(headers) || props.log.request_content_type || '',
      })
      if (!result.success || !result.data) {
        toast.error(result.message || t('Failed to replay request'))
        return
      }
      setResponse(result.data)
      toast.success(t('Request replayed'))
    } catch {
      toast.error(t('Failed to replay request'))
    } finally {
      setSending(false)
    }
  }

  if (!props.log) return null

  return (
    <Dialog open={props.open} onOpenChange={props.onOpenChange}>
      <DialogContent className='flex max-h-[90vh] flex-col overflow-hidden sm:max-w-5xl'>
        <DialogHeader>
          <DialogTitle>{t('Replay Request')}</DialogTitle>
          <DialogDescription className='truncate font-mono'>
            {method} {requestURL || props.log.path || '-'}
          </DialogDescription>
        </DialogHeader>

        <div className='min-h-0 flex-1 overflow-y-auto pr-1'>
          <div className='grid gap-4 lg:grid-cols-[minmax(0,1fr)_minmax(0,1fr)]'>
            <div className='space-y-4'>
              <div className='grid gap-3 sm:grid-cols-[120px_minmax(0,1fr)]'>
                <div className='space-y-2'>
                  <Label htmlFor='replay-method'>{t('HTTP Method')}</Label>
                  <Input
                    id='replay-method'
                    value={method}
                    onChange={(e) => setMethod(e.target.value.toUpperCase())}
                  />
                </div>
                <div className='space-y-2'>
                  <Label htmlFor='replay-url'>{t('Request URL')}</Label>
                  <Input
                    id='replay-url'
                    value={requestURL}
                    onChange={(e) => setRequestURL(e.target.value)}
                    className='font-mono'
                  />
                </div>
              </div>
              <div className='space-y-2'>
                <div className='flex items-center justify-between gap-2'>
                  <Label htmlFor='replay-headers'>{t('Request Headers')}</Label>
                  <Button
                    type='button'
                    variant='outline'
                    size='sm'
                    onClick={() =>
                      formatEditableJson(
                        headersText,
                        setHeadersText,
                        headersFormatOriginal,
                        setHeadersFormatOriginal
                      )
                    }
                  >
                    <Braces className='size-4' />
                    {headersFormatOriginal !== null
                      ? t('Cancel Format')
                      : t('Format JSON')}
                  </Button>
                </div>
                <Textarea
                  id='replay-headers'
                  value={headersText}
                  onChange={(e) => {
                    setHeadersFormatOriginal(null)
                    setHeadersText(e.target.value)
                  }}
                  rows={7}
                  className='font-mono text-xs'
                />
              </div>
              <div className='space-y-2'>
                <div className='flex items-center justify-between gap-2'>
                  <Label htmlFor='replay-body'>{t('Request Body')}</Label>
                  <div className='flex min-w-0 flex-wrap items-center justify-end gap-2'>
                    <TrafficBodySearch value={body} textareaRef={bodyRef} />
                    <Button
                      type='button'
                      variant='outline'
                      size='sm'
                      onClick={() =>
                        formatEditableJson(
                          body,
                          setBody,
                          bodyFormatOriginal,
                          setBodyFormatOriginal
                        )
                      }
                    >
                      <Braces className='size-4' />
                      {bodyFormatOriginal !== null
                        ? t('Cancel Format')
                        : t('Format JSON')}
                    </Button>
                  </div>
                </div>
                <Textarea
                  id='replay-body'
                  ref={bodyRef}
                  value={body}
                  onChange={(e) => {
                    setBodyFormatOriginal(null)
                    setBody(e.target.value)
                  }}
                  rows={12}
                  className='font-mono text-xs'
                />
              </div>
            </div>
            <ReplayResponsePanel response={response} />
          </div>
        </div>

        <DialogFooter>
          <Button
            type='button'
            variant='outline'
            onClick={() => props.onOpenChange(false)}
          >
            {t('Cancel')}
          </Button>
          <Button type='button' onClick={handleReplay} disabled={sending}>
            <SendHorizontal className='size-4' />
            {sending ? t('Replaying...') : t('Replay')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
