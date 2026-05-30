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
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Braces, Check, Eye, ShieldAlert, X } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'
import { Textarea } from '@/components/ui/textarea'
import { MultiSelect, type Option } from '@/components/multi-select'
import { searchUsers } from '@/features/users/api'
import {
  decideLiveInterceptEvent,
  getLiveInterceptEvents,
  getLiveInterceptSettings,
  updateLiveInterceptSettings,
} from '../api'
import type {
  LiveInterceptDecisionPayload,
  LiveInterceptEvent,
  LiveInterceptSettings,
} from '../types'
import { TrafficBodySearch } from './traffic-body-search'
import {
  createTrafficBodyPreview,
  type TrafficBodyPreviewMode,
} from './traffic-body-preview'
import { formatTrafficPayload } from './traffic-payload-format'

const DEFAULT_SETTINGS: LiveInterceptSettings = {
  enabled: false,
  user_ids: [],
  usernames: [],
  intercept_request: true,
  intercept_response: false,
  timeout_seconds: 60,
}

function headersText(headers: Record<string, string> | undefined): string {
  if (!headers || Object.keys(headers).length === 0) return '{}'
  return JSON.stringify(headers)
}

function parseHeadersText(text: string): Record<string, string> {
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

function eventBody(event: LiveInterceptEvent): string {
  return event.phase === 'response'
    ? event.response_body || ''
    : event.body || ''
}

function eventHeaders(event: LiveInterceptEvent): string {
  return event.phase === 'response'
    ? headersText(event.response_headers)
    : headersText(event.headers)
}

function EditablePayloadSection(props: {
  id: string
  title: string
  value: string
  rows: number
  previewPhase?: 'request' | 'response'
  emptyText?: string
  onChange: (value: string) => void
}) {
  const { t } = useTranslation()
  const textareaRef = useRef<HTMLTextAreaElement | null>(null)
  const [formatOriginal, setFormatOriginal] = useState<string | null>(null)
  const [previewMode, setPreviewMode] = useState<TrafficBodyPreviewMode | null>(
    null
  )
  const [previewDraft, setPreviewDraft] = useState<string | null>(null)
  const preview =
    props.previewPhase && previewMode
      ? createTrafficBodyPreview(props.value, props.previewPhase, previewMode)
      : null
  const displayValue = preview ? (previewDraft ?? preview.text) : props.value

  const previewActive = previewMode !== null

  const previewButtonLabel = (mode: TrafficBodyPreviewMode) =>
    previewMode === mode
      ? t('Full Body')
      : mode === 'text'
        ? t('Text Preview')
        : t('Full Preview')

  useEffect(() => {
    setFormatOriginal(null)
    setPreviewMode(null)
    setPreviewDraft(null)
  }, [props.id])

  const handleFormat = () => {
    if (previewActive) return
    if (formatOriginal !== null) {
      props.onChange(formatOriginal)
      setFormatOriginal(null)
      return
    }
    try {
      setFormatOriginal(props.value)
      props.onChange(formatTrafficPayload(props.value))
    } catch {
      toast.error(t('Invalid JSON'))
    }
  }

  const handlePreviewToggle = (mode: TrafficBodyPreviewMode) => {
    if (previewMode === mode) {
      setPreviewMode(null)
      setPreviewDraft(null)
      return
    }
    const nextPreview = props.previewPhase
      ? createTrafficBodyPreview(props.value, props.previewPhase, mode)
      : null
    if (!nextPreview) {
      toast.error(t('No preview content found'))
      return
    }
    setFormatOriginal(null)
    setPreviewMode(mode)
    setPreviewDraft(nextPreview.text)
  }

  const handleChange = (value: string) => {
    setFormatOriginal(null)
    if (preview?.readOnly) return
    if (preview) {
      setPreviewDraft(value)
      const nextValue = preview.apply(value)
      if (nextValue !== null) {
        props.onChange(nextValue)
      }
      return
    }
    props.onChange(value)
  }

  return (
    <div className='space-y-2'>
      <div className='flex items-center justify-between gap-2'>
        <Label htmlFor={props.id}>{props.title}</Label>
        <div className='flex min-w-0 flex-wrap items-center justify-end gap-2'>
          {props.previewPhase && (
            <TrafficBodySearch
              value={displayValue}
              textareaRef={textareaRef}
            />
          )}
          {props.previewPhase && (
            <>
              <Button
                type='button'
                variant='outline'
                size='sm'
                onClick={() => handlePreviewToggle('text')}
              >
                <Eye className='size-4' />
                {previewButtonLabel('text')}
              </Button>
              <Button
                type='button'
                variant='outline'
                size='sm'
                onClick={() => handlePreviewToggle('all')}
              >
                <Eye className='size-4' />
                {previewButtonLabel('all')}
              </Button>
            </>
          )}
          <Button
            type='button'
            variant='outline'
            size='sm'
            onClick={handleFormat}
            disabled={previewActive}
          >
            <Braces className='size-4' />
            {formatOriginal !== null ? t('Cancel Format') : t('Format JSON')}
          </Button>
        </div>
      </div>
      <Textarea
        id={props.id}
        ref={textareaRef}
        value={displayValue}
        onChange={(e) => handleChange(e.target.value)}
        placeholder={props.emptyText}
        rows={props.rows}
        readOnly={preview?.readOnly}
        className='font-mono text-xs'
      />
    </div>
  )
}

function LiveEventCard(props: {
  event: LiveInterceptEvent
  deciding: boolean
  onDecision: (id: string, payload: LiveInterceptDecisionPayload) => void
}) {
  const { t } = useTranslation()
  const event = props.event
  const phaseLabel = event.phase === 'response' ? t('Response') : t('Request')
  const [initialHeaders, setInitialHeaders] = useState(() =>
    eventHeaders(event)
  )
  const [initialBody, setInitialBody] = useState(() => eventBody(event))
  const [headersValue, setHeadersValue] = useState(() => eventHeaders(event))
  const [bodyValue, setBodyValue] = useState(() => eventBody(event))

  useEffect(() => {
    const nextHeaders = eventHeaders(event)
    const nextBody = eventBody(event)
    setInitialHeaders(nextHeaders)
    setInitialBody(nextBody)
    setHeadersValue(nextHeaders)
    setBodyValue(nextBody)
  }, [event.id])

  const handleAccept = () => {
    const headersModified = headersValue !== initialHeaders
    const bodyModified = bodyValue !== initialBody
    let headers: Record<string, string> | undefined
    if (headersModified) {
      try {
        headers = parseHeadersText(headersValue)
      } catch {
        toast.error(t('Request headers must be a JSON object'))
        return
      }
    }
    props.onDecision(event.id, {
      decision: 'accept',
      headers,
      body: bodyModified ? bodyValue : undefined,
      headers_modified: headersModified,
      body_modified: bodyModified,
    })
  }

  return (
    <div className='rounded-lg border'>
      <div className='flex flex-wrap items-center justify-between gap-3 border-b px-3 py-2'>
        <div className='min-w-0 space-y-1'>
          <div className='flex flex-wrap items-center gap-2'>
            <Badge
              variant={event.phase === 'response' ? 'secondary' : 'outline'}
            >
              {phaseLabel}
            </Badge>
            {event.status_code ? (
              <Badge variant='outline'>{event.status_code}</Badge>
            ) : null}
            <span className='truncate font-mono text-xs'>
              {event.method || 'HTTP'} {event.path || event.request_url || '-'}
            </span>
          </div>
          <div className='text-muted-foreground text-xs'>
            {event.username || t('User {{id}}', { id: event.user_id })}
            {event.user_id ? ` #${event.user_id}` : ''}
            {event.model_name ? ` · ${event.model_name}` : ''}
          </div>
        </div>
        <div className='flex shrink-0 items-center gap-2'>
          <Button
            type='button'
            size='sm'
            onClick={handleAccept}
            disabled={props.deciding}
          >
            <Check className='size-4' />
            {t('Accept')}
          </Button>
          <Button
            type='button'
            size='sm'
            variant='destructive'
            onClick={() => props.onDecision(event.id, { decision: 'block' })}
            disabled={props.deciding}
          >
            <X className='size-4' />
            {t('Block')}
          </Button>
        </div>
      </div>
      <div className='grid gap-3 p-3 lg:grid-cols-2'>
        <EditablePayloadSection
          id={`live-${event.id}-headers`}
          title={
            event.phase === 'response'
              ? t('Response Headers')
              : t('Request Headers')
          }
          value={headersValue}
          emptyText='{}'
          rows={8}
          onChange={setHeadersValue}
        />
        <EditablePayloadSection
          id={`live-${event.id}-body`}
          title={
            event.phase === 'response' ? t('Response Body') : t('Request Body')
          }
          value={bodyValue}
          emptyText={t('Empty body')}
          previewPhase={event.phase}
          rows={12}
          onChange={setBodyValue}
        />
      </div>
    </div>
  )
}

export function LiveInterceptPage() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [form, setForm] = useState<LiveInterceptSettings>(DEFAULT_SETTINGS)

  const settingsQuery = useQuery({
    queryKey: ['live-intercept-settings'],
    queryFn: async () => {
      const result = await getLiveInterceptSettings()
      if (!result.success || !result.data) {
        throw new Error(result.message || t('Failed to load intercept rules'))
      }
      return result.data
    },
  })

  useEffect(() => {
    if (settingsQuery.data) {
      setForm({
        ...DEFAULT_SETTINGS,
        ...settingsQuery.data,
      })
    }
  }, [settingsQuery.data])

  const usersQuery = useQuery({
    queryKey: ['live-intercept-users'],
    queryFn: async () => {
      const result = await searchUsers({ page_size: 100 })
      return result.data?.items || []
    },
  })

  const eventsQuery = useQuery({
    queryKey: ['live-intercept-events'],
    queryFn: async () => {
      const result = await getLiveInterceptEvents()
      if (!result.success || !result.data) return []
      return result.data
    },
    refetchInterval: form.enabled ? 1000 : false,
  })

  const userOptions: Option[] = useMemo(
    () =>
      (usersQuery.data || []).map((user) => ({
        value: String(user.id),
        label: `${user.username || t('User {{id}}', { id: user.id })} #${user.id}`,
      })),
    [t, usersQuery.data]
  )

  const saveMutation = useMutation({
    mutationFn: updateLiveInterceptSettings,
    onSuccess: (result) => {
      if (!result.success || !result.data) {
        toast.error(result.message || t('Failed to save settings'))
        return
      }
      setForm(result.data)
      queryClient.invalidateQueries({ queryKey: ['live-intercept-settings'] })
      toast.success(t('Settings saved'))
    },
    onError: () => toast.error(t('Failed to save settings')),
  })

  const decisionMutation = useMutation({
    mutationFn: (input: {
      id: string
      payload: LiveInterceptDecisionPayload
    }) => decideLiveInterceptEvent(input.id, input.payload),
    onSuccess: (result) => {
      if (!result.success) {
        toast.error(result.message || t('Failed to save settings'))
        return
      }
      queryClient.invalidateQueries({ queryKey: ['live-intercept-events'] })
    },
    onError: () => toast.error(t('Failed to save settings')),
  })

  const selectedUsers = form.user_ids.map(String)
  const updateSelectedUsers = (values: string[]) => {
    setForm((prev) => ({
      ...prev,
      user_ids: values
        .map((value) => Number(value))
        .filter((value) => value > 0),
      usernames: [],
    }))
  }

  const events = eventsQuery.data || []

  return (
    <div className='space-y-4'>
      <div className='rounded-lg border'>
        <div className='flex flex-wrap items-center justify-between gap-3 border-b px-4 py-3'>
          <div className='flex items-center gap-2'>
            <ShieldAlert className='size-4' />
            <div className='font-medium'>{t('Live Intercept')}</div>
          </div>
          <div className='flex items-center gap-2'>
            <Switch
              checked={form.enabled}
              onCheckedChange={(checked) =>
                setForm((prev) => ({ ...prev, enabled: Boolean(checked) }))
              }
            />
            <span className='text-sm'>
              {form.enabled ? t('Enabled') : t('Disabled')}
            </span>
          </div>
        </div>
        <div className='grid gap-4 p-4 lg:grid-cols-[minmax(0,1fr)_220px]'>
          <div className='space-y-2'>
            <Label>{t('User')}</Label>
            <MultiSelect
              options={userOptions}
              selected={selectedUsers}
              onChange={updateSelectedUsers}
              allowCreate
              placeholder={t('User ID')}
              maxVisibleChips={6}
            />
          </div>
          <div className='space-y-2'>
            <Label htmlFor='live-timeout'>{t('Timeout')}</Label>
            <Input
              id='live-timeout'
              type='number'
              value={form.timeout_seconds}
              onChange={(e) =>
                setForm((prev) => ({
                  ...prev,
                  timeout_seconds: parseInt(e.target.value) || 60,
                }))
              }
            />
          </div>
          <div className='flex flex-wrap gap-4 lg:col-span-2'>
            <div className='flex items-center gap-2'>
              <Checkbox
                id='live-request'
                checked={form.intercept_request}
                onCheckedChange={(checked) =>
                  setForm((prev) => ({
                    ...prev,
                    intercept_request: Boolean(checked),
                  }))
                }
              />
              <Label htmlFor='live-request'>{t('Intercept Request')}</Label>
            </div>
            <div className='flex items-center gap-2'>
              <Checkbox
                id='live-response'
                checked={form.intercept_response}
                onCheckedChange={(checked) =>
                  setForm((prev) => ({
                    ...prev,
                    intercept_response: Boolean(checked),
                  }))
                }
              />
              <Label htmlFor='live-response'>{t('Intercept Response')}</Label>
            </div>
          </div>
          <div className='lg:col-span-2'>
            <Button
              type='button'
              onClick={() => saveMutation.mutate(form)}
              disabled={saveMutation.isPending}
            >
              {saveMutation.isPending ? t('Saving...') : t('Save')}
            </Button>
          </div>
        </div>
      </div>

      <div className='space-y-3'>
        {events.length === 0 ? (
          <div className='text-muted-foreground rounded-lg border border-dashed p-4 text-sm'>
            {t('No pending live intercept events')}
          </div>
        ) : (
          events.map((event) => (
            <LiveEventCard
              key={event.id}
              event={event}
              deciding={decisionMutation.isPending}
              onDecision={(id, payload) =>
                decisionMutation.mutate({ id, payload })
              }
            />
          ))
        )}
      </div>
    </div>
  )
}
