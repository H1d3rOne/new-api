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
import { useEffect, useRef, useState, type ReactNode } from 'react'
import { Braces, ChevronDown, Eye, Plus, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { cn } from '@/lib/utils'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from '@/components/ui/collapsible'
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
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { Textarea } from '@/components/ui/textarea'
import { createInterceptRule, updateInterceptRule } from '../api'
import type {
  InterceptRule,
  MessageContentMatch,
  MessageContentRewrite,
  TrafficLog,
} from '../types'
import {
  createTrafficBodyPreview,
  type TrafficBodyPreviewMode,
} from './traffic-body-preview'
import { TrafficBodySearch } from './traffic-body-search'
import { formatTrafficPayload } from './traffic-payload-format'

interface InterceptRuleDialogProps {
  rule: Partial<InterceptRule> | null
  sourceLog?: TrafficLog | null
  open: boolean
  initialTab?: 'basic' | 'request' | 'response' | 'script'
  onOpenChange: (open: boolean) => void
  onSaved: () => void
}

const SCRIPT_TEMPLATE = `async function onRequest(context, request) {
  if (request.body && request.headers["content-type"] && request.headers["content-type"].includes("application/json")) {
    try {
      var body = JSON.parse(request.body)
      if (body.client_metadata) {
        delete body.client_metadata
        request.body = JSON.stringify(body)
        request.headers["content-length"] = String(request.body.length)
      }
    } catch (e) {}
  }
  return request
}

async function onResponse(context, request, response) {
  return response
}`

const EMPTY_RULE: Partial<InterceptRule> = {
  name: '',
  description: '',
  priority: 0,
  enabled: true,
  match_limit: 0,
  match_count: 0,
  user_id: 0,
  username: '',
  path_pattern: '',
  method: '',
  model_pattern: '',
  request_content_match: '',
  request_message_matches: '[]',
  request_message_match_op: 'and',
  condition_expr: '',
  response_user_id: 0,
  response_username: '',
  response_path_pattern: '',
  response_method: '',
  response_model_pattern: '',
  response_content_match: '',
  response_tool_calls_match: '',
  response_match_op: 'and',
  response_condition_expr: '',
  request_match_enabled: false,
  response_match_enabled: false,
  intercept_request: true,
  intercept_response: false,
  script_enabled: false,
  block_enabled: false,
  block_status_code: 403,
  block_content_type: 'application/json',
  block_body: '',
  request_header_ops: '[]',
  request_body_rewrite: '',
  request_message_rewrites: '[]',
  request_url_rewrite: '',
  request_script: '',
  response_header_ops: '[]',
  response_body_rewrite: '',
  response_content_rewrite: '',
  response_tool_calls_rewrite: '',
  response_status_rewrite: '',
  response_url_rewrite: '',
  response_script: '',
  script: SCRIPT_TEMPLATE,
}

const ALL_METHODS_VALUE = '__all__'
const MATCH_OPS: Array<InterceptRule['request_message_match_op']> = [
  'and',
  'or',
]
const CONTENT_MATCH_OPS: Array<NonNullable<MessageContentMatch['content_op']>> =
  ['and', 'or']
const MESSAGE_CONTENT_ROLES = ['system', 'user', 'assistant', 'tool']
const MESSAGE_CONTENT_MODES: MessageContentRewrite['mode'][] = [
  'latest',
  'first',
  'all',
]
function exprString(value: string): string {
  return JSON.stringify(value ?? '')
}

function staticStringExpressionValue(value: string): string | null {
  const trimmed = value.trim()
  if (!trimmed) return ''
  try {
    const parsed = JSON.parse(trimmed) as unknown
    return typeof parsed === 'string' ? parsed : null
  } catch {
    return null
  }
}

function hasConfiguredJsonArray(value?: string): boolean {
  const trimmed = value?.trim() || ''
  return trimmed !== '' && trimmed !== '[]'
}

function hasRequestMatchFields(rule: Partial<InterceptRule>): boolean {
  return Boolean(
    rule.user_id ||
    rule.username?.trim() ||
    rule.path_pattern?.trim() ||
    rule.method?.trim() ||
    rule.model_pattern?.trim() ||
    rule.request_content_match?.trim() ||
    hasConfiguredJsonArray(rule.request_message_matches) ||
    rule.condition_expr?.trim()
  )
}

function hasResponseMatchFields(rule: Partial<InterceptRule>): boolean {
  return Boolean(
    rule.response_user_id ||
    rule.response_username?.trim() ||
    rule.response_path_pattern?.trim() ||
    rule.response_method?.trim() ||
    rule.response_model_pattern?.trim() ||
    rule.response_content_match?.trim() ||
    rule.response_tool_calls_match?.trim() ||
    rule.response_condition_expr?.trim()
  )
}

function createInitialRuleForm(
  rule: Partial<InterceptRule> | null
): Partial<InterceptRule> {
  const form = { ...EMPTY_RULE, ...(rule || {}) }
  if (rule && rule.request_match_enabled === undefined) {
    form.request_match_enabled = hasRequestMatchFields(form)
  }
  if (rule && rule.response_match_enabled === undefined) {
    form.response_match_enabled = hasResponseMatchFields(form)
  }
  if (!form.script?.trim()) {
    form.script = SCRIPT_TEMPLATE
  }
  return form
}

function parseMessageContentRewrites(value?: string): MessageContentRewrite[] {
  if (!value?.trim()) return []
  try {
    const parsed = JSON.parse(value) as unknown
    if (!Array.isArray(parsed)) return []
    return parsed
      .filter(
        (item): item is Partial<MessageContentRewrite> =>
          Boolean(item) && typeof item === 'object'
      )
      .map((item) => ({
        role: String(item.role || 'user'),
        mode: MESSAGE_CONTENT_MODES.includes(
          item.mode as MessageContentRewrite['mode']
        )
          ? (item.mode as MessageContentRewrite['mode'])
          : 'latest',
        index: typeof item.index === 'number' ? item.index : undefined,
        content: String(item.content || ''),
      }))
  } catch {
    return []
  }
}

function stringifyMessageContentRewrites(items: MessageContentRewrite[]) {
  return JSON.stringify(
    items
      .map((item) => ({
        role: item.role || 'user',
        mode: item.mode || 'latest',
        content: item.content || '',
      }))
      .filter((item) => item.role.trim())
  )
}

function parseMessageContentMatches(value?: string): MessageContentMatch[] {
  if (!value?.trim()) return []
  try {
    const parsed = JSON.parse(value) as unknown
    if (!Array.isArray(parsed)) return []
    return parsed
      .filter(
        (item): item is Partial<MessageContentMatch> =>
          Boolean(item) && typeof item === 'object'
      )
      .map((item) => ({
        role: String(item.role || 'user'),
        mode: MESSAGE_CONTENT_MODES.includes(
          item.mode as MessageContentMatch['mode']
        )
          ? (item.mode as MessageContentMatch['mode'])
          : 'latest',
        index: typeof item.index === 'number' ? item.index : undefined,
        content: String(item.content || ''),
        content_op: CONTENT_MATCH_OPS.includes(
          item.content_op as NonNullable<MessageContentMatch['content_op']>
        )
          ? (item.content_op as NonNullable<MessageContentMatch['content_op']>)
          : 'and',
      }))
  } catch {
    return []
  }
}

function stringifyMessageContentMatches(items: MessageContentMatch[]) {
  return JSON.stringify(
    items
      .map((item) => ({
        role: item.role || 'user',
        mode: item.mode || 'latest',
        content: item.content || '',
        content_op: item.content_op || 'and',
      }))
      .filter((item) => item.role.trim())
  )
}

function CollapsiblePayloadSection(props: {
  id: string
  label: string
  resetKey?: string | number | boolean
  defaultOpen?: boolean
  children: ReactNode
}) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(Boolean(props.defaultOpen))

  useEffect(() => {
    setOpen(Boolean(props.defaultOpen))
  }, [props.resetKey, props.defaultOpen])

  return (
    <Collapsible open={open} onOpenChange={setOpen}>
      <CollapsibleTrigger
        render={
          <button
            type='button'
            className='hover:bg-muted/40 border-border/60 flex w-full items-center justify-between rounded-md border px-3 py-2 text-left transition-colors'
            aria-expanded={open}
            aria-controls={`${props.id}_content`}
          />
        }
      >
        <span className='text-sm font-medium'>{props.label}</span>
        <span className='text-muted-foreground flex items-center gap-2 text-xs'>
          {open ? t('Collapse') : t('Expand')}
          <ChevronDown
            className={cn('size-4 transition-transform', open && 'rotate-180')}
            aria-hidden='true'
          />
        </span>
      </CollapsibleTrigger>
      <CollapsibleContent
        id={`${props.id}_content`}
        className='border-border/60 mt-2 rounded-md border p-3'
      >
        {props.children}
      </CollapsibleContent>
    </Collapsible>
  )
}

function PayloadRewriteTextarea(props: {
  id: string
  label: string
  value: string
  rows: number
  resetKey?: string | number | boolean
  disabled?: boolean
  readOnly?: boolean
  expressionValue?: boolean
  previewPhase?: 'request' | 'response'
  placeholder?: string
  collapsible?: boolean
  defaultOpen?: boolean
  onChange: (value: string) => void
}) {
  const { t } = useTranslation()
  const textareaRef = useRef<HTMLTextAreaElement | null>(null)
  const [formatOriginal, setFormatOriginal] = useState<string | null>(null)
  const [previewMode, setPreviewMode] = useState<TrafficBodyPreviewMode | null>(
    null
  )
  const [previewDraft, setPreviewDraft] = useState<string | null>(null)
  const [readOnlyValueOverride, setReadOnlyValueOverride] = useState<
    string | null
  >(null)
  const staticExpressionValue = props.expressionValue
    ? staticStringExpressionValue(props.value)
    : null
  const shouldStoreAsStaticExpression =
    Boolean(props.expressionValue) &&
    (staticExpressionValue !== null || props.value.trim() === '')
  const baseDisplayValue =
    props.expressionValue && staticExpressionValue !== null
      ? staticExpressionValue
      : props.value
  const displayValue = readOnlyValueOverride ?? baseDisplayValue
  const preview =
    props.previewPhase && previewMode
      ? createTrafficBodyPreview(displayValue, props.previewPhase, previewMode)
      : null
  const textValue = preview ? (previewDraft ?? preview.text) : displayValue
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
    setReadOnlyValueOverride(null)
  }, [props.resetKey])

  const commitValue = (value: string) => {
    if (props.readOnly) {
      setReadOnlyValueOverride(value)
      return
    }
    props.onChange(shouldStoreAsStaticExpression ? exprString(value) : value)
  }

  const handleFormat = () => {
    if (previewActive) return
    if (formatOriginal !== null) {
      commitValue(formatOriginal)
      setFormatOriginal(null)
      return
    }
    try {
      setFormatOriginal(displayValue)
      commitValue(formatTrafficPayload(displayValue))
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
      ? createTrafficBodyPreview(displayValue, props.previewPhase, mode)
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
    if (props.readOnly) return
    if (preview?.readOnly) return
    if (preview) {
      setPreviewDraft(value)
      const nextValue = preview.apply(value)
      if (nextValue !== null) {
        commitValue(nextValue)
      }
      return
    }
    commitValue(value)
  }

  const content = (
    <div className='space-y-2'>
      <div className='flex items-center justify-between gap-2'>
        {props.collapsible ? (
          <Label htmlFor={props.id} className='sr-only'>
            {props.label}
          </Label>
        ) : (
          <Label htmlFor={props.id}>{props.label}</Label>
        )}
        <div className='flex min-w-0 flex-wrap items-center justify-end gap-2'>
          <TrafficBodySearch
            value={textValue}
            textareaRef={textareaRef}
            disabled={props.disabled}
          />
          {props.previewPhase && (
            <>
              <Button
                type='button'
                variant='outline'
                size='sm'
                onClick={() => handlePreviewToggle('text')}
                disabled={props.disabled}
              >
                <Eye className='size-4' />
                {previewButtonLabel('text')}
              </Button>
              <Button
                type='button'
                variant='outline'
                size='sm'
                onClick={() => handlePreviewToggle('all')}
                disabled={props.disabled}
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
            disabled={props.disabled || previewActive}
          >
            <Braces className='size-4' />
            {formatOriginal !== null ? t('Cancel Format') : t('Format JSON')}
          </Button>
        </div>
      </div>
      <Textarea
        id={props.id}
        ref={textareaRef}
        value={textValue}
        onChange={(e) => handleChange(e.target.value)}
        placeholder={props.placeholder}
        rows={props.rows}
        disabled={props.disabled}
        readOnly={props.readOnly || preview?.readOnly}
        className='font-mono text-xs'
      />
    </div>
  )

  if (!props.collapsible) {
    return content
  }

  return (
    <CollapsiblePayloadSection
      id={props.id}
      label={props.label}
      resetKey={props.resetKey}
      defaultOpen={props.defaultOpen}
    >
      {content}
    </CollapsiblePayloadSection>
  )
}

export function InterceptRuleDialog(props: InterceptRuleDialogProps) {
  const { t } = useTranslation()
  const [form, setForm] = useState<Partial<InterceptRule>>(EMPTY_RULE)
  const [activeTab, setActiveTab] = useState<
    'basic' | 'request' | 'response' | 'script'
  >('basic')
  const [saving, setSaving] = useState(false)
  const [rawRequestBody, setRawRequestBody] = useState('')
  const [rawResponseBody, setRawResponseBody] = useState('')

  useEffect(() => {
    if (props.open) {
      setForm(createInitialRuleForm(props.rule))
      setActiveTab(props.initialTab || 'basic')
      setRawRequestBody(props.sourceLog?.request_body || '')
      setRawResponseBody(props.sourceLog?.response_body || '')
    }
  }, [props.open, props.rule, props.initialTab, props.sourceLog])

  const isEdit = Boolean(props.rule?.id)
  const formResetKey = `${props.open}-${props.rule?.id || 'new'}-${props.sourceLog?.id || 0}`
  const requestMatchEnabled = form.request_match_enabled ?? false
  const requestRewriteEnabled = form.intercept_request ?? false
  const responseMatchEnabled = form.response_match_enabled ?? false
  const responseRewriteEnabled = form.intercept_response ?? false
  const scriptEnabled = form.script_enabled ?? false
  const matchLimit = Number(form.match_limit || 0)
  const matchLimitUnlimited = matchLimit <= 0
  const requestMessageMatches = parseMessageContentMatches(
    form.request_message_matches
  )
  const requestMessageRewrites = parseMessageContentRewrites(
    form.request_message_rewrites
  )

  const updateField = <K extends keyof InterceptRule>(
    key: K,
    value: InterceptRule[K]
  ) => {
    setForm((prev) => ({ ...prev, [key]: value }))
  }

  const updateMatchField = (
    key: keyof InterceptRule,
    value: string | number
  ) => {
    setForm((prev) => ({ ...prev, [key]: value }))
  }

  const updateRequestMessageMatches = (items: MessageContentMatch[]) => {
    updateField(
      'request_message_matches',
      stringifyMessageContentMatches(items)
    )
  }

  const updateRequestMessageMatch = (
    index: number,
    patch: Partial<MessageContentMatch>
  ) => {
    updateRequestMessageMatches(
      requestMessageMatches.map((item, itemIndex) =>
        itemIndex === index ? { ...item, ...patch } : item
      )
    )
  }

  const addRequestMessageMatch = () => {
    updateRequestMessageMatches([
      ...requestMessageMatches,
      { role: 'user', mode: 'latest', content: '', content_op: 'and' },
    ])
  }

  const removeRequestMessageMatch = (index: number) => {
    updateRequestMessageMatches(
      requestMessageMatches.filter((_, itemIndex) => itemIndex !== index)
    )
  }

  const updateRequestMessageRewrites = (items: MessageContentRewrite[]) => {
    updateField(
      'request_message_rewrites',
      stringifyMessageContentRewrites(items)
    )
  }

  const updateRequestMessageRewrite = (
    index: number,
    patch: Partial<MessageContentRewrite>
  ) => {
    updateRequestMessageRewrites(
      requestMessageRewrites.map((item, itemIndex) =>
        itemIndex === index ? { ...item, ...patch } : item
      )
    )
  }

  const addRequestMessageRewrite = () => {
    updateRequestMessageRewrites([
      ...requestMessageRewrites,
      { role: 'user', mode: 'latest', content: '' },
    ])
  }

  const removeRequestMessageRewrite = (index: number) => {
    updateRequestMessageRewrites(
      requestMessageRewrites.filter((_, itemIndex) => itemIndex !== index)
    )
  }

  const handleSave = async () => {
    if (!form.name?.trim()) {
      toast.error(t('Name is required'))
      return
    }

    const payload = { ...form }
    delete payload.match_count
    payload.block_enabled = false
    payload.block_body = ''
    payload.request_header_ops = '[]'
    payload.response_header_ops = '[]'
    payload.condition_expr = ''
    payload.response_condition_expr = ''
    payload.request_content_match = ''
    payload.request_body_rewrite = ''
    payload.request_url_rewrite = ''
    payload.response_body_rewrite = ''
    payload.request_message_matches = stringifyMessageContentMatches(
      requestMessageMatches.filter((item) => item.content.trim())
    )

    setSaving(true)
    try {
      const result = isEdit
        ? await updateInterceptRule(props.rule!.id!, payload)
        : await createInterceptRule(payload)

      if (result.success) {
        toast.success(isEdit ? t('Rule updated') : t('Rule created'))
        props.onSaved()
        props.onOpenChange(false)
      } else {
        toast.error(result.message || t('Failed to save rule'))
      }
    } catch (err) {
      toast.error(t('Failed to save rule'))
    } finally {
      setSaving(false)
    }
  }

  const renderBaseMatchFields = (
    prefix: string,
    fields: {
      method: keyof InterceptRule
      pathPattern: keyof InterceptRule
      userId: keyof InterceptRule
      username: keyof InterceptRule
      modelPattern: keyof InterceptRule
    },
    disabled?: boolean
  ) => (
    <>
      <div className='grid gap-4 sm:grid-cols-2'>
        <div className='space-y-2'>
          <Label htmlFor={`${prefix}_method`}>{t('HTTP Method')}</Label>
          <Select
            value={String(form[fields.method] || '') || ALL_METHODS_VALUE}
            disabled={disabled}
            onValueChange={(v) =>
              updateMatchField(
                fields.method,
                !v || v === ALL_METHODS_VALUE ? '' : v
              )
            }
          >
            <SelectTrigger id={`${prefix}_method`}>
              <SelectValue placeholder={t('All methods')} />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value={ALL_METHODS_VALUE}>
                {t('All methods')}
              </SelectItem>
              <SelectItem value='GET'>GET</SelectItem>
              <SelectItem value='POST'>POST</SelectItem>
              <SelectItem value='PUT'>PUT</SelectItem>
              <SelectItem value='DELETE'>DELETE</SelectItem>
              <SelectItem value='PATCH'>PATCH</SelectItem>
            </SelectContent>
          </Select>
        </div>
        <div className='space-y-2'>
          <Label htmlFor={`${prefix}_path_pattern`}>{t('Path Pattern')}</Label>
          <Input
            id={`${prefix}_path_pattern`}
            value={String(form[fields.pathPattern] || '')}
            onChange={(e) =>
              updateMatchField(fields.pathPattern, e.target.value)
            }
            placeholder={t('Regex pattern, e.g. /v1/chat/.*')}
            disabled={disabled}
          />
        </div>
      </div>
      <div className='grid gap-4 sm:grid-cols-2'>
        <div className='space-y-2'>
          <Label htmlFor={`${prefix}_user_id`}>{t('User ID')}</Label>
          <Input
            id={`${prefix}_user_id`}
            type='number'
            value={
              Number(form[fields.userId] || 0)
                ? String(form[fields.userId])
                : ''
            }
            onChange={(e) =>
              updateMatchField(fields.userId, parseInt(e.target.value) || 0)
            }
            placeholder={t('All')}
            disabled={disabled}
          />
        </div>
        <div className='space-y-2'>
          <Label htmlFor={`${prefix}_username`}>{t('Username')}</Label>
          <Input
            id={`${prefix}_username`}
            value={String(form[fields.username] || '')}
            onChange={(e) => updateMatchField(fields.username, e.target.value)}
            placeholder={t('All')}
            disabled={disabled}
          />
        </div>
      </div>
      <div className='space-y-2'>
        <Label htmlFor={`${prefix}_model_pattern`}>{t('Model Pattern')}</Label>
        <Input
          id={`${prefix}_model_pattern`}
          value={String(form[fields.modelPattern] || '')}
          onChange={(e) =>
            updateMatchField(fields.modelPattern, e.target.value)
          }
          placeholder={t('Regex pattern, e.g. gpt-4.*')}
          disabled={disabled}
        />
      </div>
    </>
  )

  const renderRequestMatchFields = () => (
    <div className='space-y-4 rounded-md border p-3'>
      <div className='flex items-center gap-2'>
        <Checkbox
          id='request_match_enabled'
          checked={requestMatchEnabled}
          onCheckedChange={(checked) =>
            updateField('request_match_enabled', Boolean(checked))
          }
        />
        <Label htmlFor='request_match_enabled'>{t('Request Match')}</Label>
      </div>
      <div className={cn('space-y-4', !requestMatchEnabled && 'opacity-60')}>
        {renderBaseMatchFields(
          'request_match',
          {
            method: 'method',
            pathPattern: 'path_pattern',
            userId: 'user_id',
            username: 'username',
            modelPattern: 'model_pattern',
          },
          !requestMatchEnabled
        )}
        {renderRequestMessageMatchFields()}
      </div>
    </div>
  )

  const renderRequestMessageMatchFields = () => (
    <CollapsiblePayloadSection
      id='request_message_matches'
      label={t('Messages Content Match')}
      resetKey={formResetKey}
    >
      <div className='space-y-3'>
        <div className='grid gap-3 sm:grid-cols-[220px_1fr]'>
          <div className='space-y-2'>
            <Label htmlFor='request_message_match_op'>{t('Match Logic')}</Label>
            <Select
              value={form.request_message_match_op || 'and'}
              disabled={!requestMatchEnabled}
              onValueChange={(value) =>
                updateField(
                  'request_message_match_op',
                  (value || 'and') as InterceptRule['request_message_match_op']
                )
              }
            >
              <SelectTrigger id='request_message_match_op' className='w-full'>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {MATCH_OPS.map((op) => (
                  <SelectItem key={op} value={op}>
                    {op === 'and' ? t('Match All') : t('Match Any')}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
          <p className='text-muted-foreground self-end pb-2 text-xs'>
            {t('Controls the relationship between message content conditions.')}
          </p>
        </div>
        {requestMessageMatches.map((match, index) => (
          <div key={index} className='space-y-3 rounded-md border p-3'>
            <div className='grid gap-3 sm:grid-cols-[1fr_1fr_1fr_auto]'>
              <div className='space-y-2'>
                <Label htmlFor={`request_message_match_role_${index}`}>
                  {t('Role')}
                </Label>
                <Select
                  value={match.role || 'user'}
                  disabled={!requestMatchEnabled}
                  onValueChange={(value) =>
                    updateRequestMessageMatch(index, {
                      role: value || 'user',
                    })
                  }
                >
                  <SelectTrigger
                    id={`request_message_match_role_${index}`}
                    className='w-full'
                  >
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {MESSAGE_CONTENT_ROLES.map((role) => (
                      <SelectItem key={role} value={role}>
                        {role}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
              <div className='space-y-2'>
                <Label htmlFor={`request_message_match_mode_${index}`}>
                  {t('Mode')}
                </Label>
                <Select
                  value={match.mode || 'latest'}
                  disabled={!requestMatchEnabled}
                  onValueChange={(value) =>
                    updateRequestMessageMatch(index, {
                      mode: (value || 'latest') as MessageContentMatch['mode'],
                    })
                  }
                >
                  <SelectTrigger
                    id={`request_message_match_mode_${index}`}
                    className='w-full'
                  >
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value='latest'>{t('Latest')}</SelectItem>
                    <SelectItem value='first'>{t('First')}</SelectItem>
                    <SelectItem value='all'>{t('All')}</SelectItem>
                  </SelectContent>
                </Select>
              </div>
              <div className='space-y-2'>
                <Label htmlFor={`request_message_match_content_op_${index}`}>
                  {t('Keyword Logic')}
                </Label>
                <Select
                  value={match.content_op || 'and'}
                  disabled={!requestMatchEnabled}
                  onValueChange={(value) =>
                    updateRequestMessageMatch(index, {
                      content_op: (value || 'and') as NonNullable<
                        MessageContentMatch['content_op']
                      >,
                    })
                  }
                >
                  <SelectTrigger
                    id={`request_message_match_content_op_${index}`}
                    className='w-full'
                  >
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {CONTENT_MATCH_OPS.map((op) => (
                      <SelectItem key={op} value={op}>
                        {op === 'and' ? t('Match All') : t('Match Any')}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
              <div className='flex items-end'>
                <Button
                  type='button'
                  variant='ghost'
                  size='sm'
                  disabled={!requestMatchEnabled}
                  onClick={() => removeRequestMessageMatch(index)}
                >
                  <Trash2 className='text-destructive size-4' />
                </Button>
              </div>
            </div>
            <div className='space-y-2'>
              <Label htmlFor={`request_message_match_content_${index}`}>
                {t('Content')}
              </Label>
              <Textarea
                id={`request_message_match_content_${index}`}
                value={match.content || ''}
                onChange={(e) =>
                  updateRequestMessageMatch(index, {
                    content: e.target.value,
                  })
                }
                rows={4}
                className='font-mono text-xs'
                disabled={!requestMatchEnabled}
                placeholder={t('One keyword per line')}
              />
              <p className='text-muted-foreground text-xs'>
                {t(
                  'Multiple keyword lines are matched against the same selected role content.'
                )}
              </p>
            </div>
          </div>
        ))}
        <Button
          type='button'
          variant='outline'
          size='sm'
          disabled={!requestMatchEnabled}
          onClick={addRequestMessageMatch}
        >
          <Plus className='size-4' />
          {t('Add Role Match')}
        </Button>
      </div>
    </CollapsiblePayloadSection>
  )

  const renderRequestMessageRewriteFields = () => (
    <CollapsiblePayloadSection
      id='request_message_rewrites'
      label={t('Messages Content Rewrite')}
      resetKey={formResetKey}
    >
      <div className='space-y-3'>
        {requestMessageRewrites.map((rewrite, index) => (
          <div key={index} className='space-y-3 rounded-md border p-3'>
            <div className='grid gap-3 sm:grid-cols-[1fr_1fr_auto]'>
              <div className='space-y-2'>
                <Label htmlFor={`request_message_rewrite_role_${index}`}>
                  {t('Role')}
                </Label>
                <Select
                  value={rewrite.role || 'user'}
                  disabled={!requestRewriteEnabled}
                  onValueChange={(value) =>
                    updateRequestMessageRewrite(index, {
                      role: value || 'user',
                    })
                  }
                >
                  <SelectTrigger
                    id={`request_message_rewrite_role_${index}`}
                    className='w-full'
                  >
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {MESSAGE_CONTENT_ROLES.map((role) => (
                      <SelectItem key={role} value={role}>
                        {role}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
              <div className='space-y-2'>
                <Label htmlFor={`request_message_rewrite_mode_${index}`}>
                  {t('Mode')}
                </Label>
                <Select
                  value={rewrite.mode || 'latest'}
                  disabled={!requestRewriteEnabled}
                  onValueChange={(value) =>
                    updateRequestMessageRewrite(index, {
                      mode: (value ||
                        'latest') as MessageContentRewrite['mode'],
                    })
                  }
                >
                  <SelectTrigger
                    id={`request_message_rewrite_mode_${index}`}
                    className='w-full'
                  >
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value='latest'>{t('Latest')}</SelectItem>
                    <SelectItem value='first'>{t('First')}</SelectItem>
                    <SelectItem value='all'>{t('All')}</SelectItem>
                  </SelectContent>
                </Select>
              </div>
              <div className='flex items-end'>
                <Button
                  type='button'
                  variant='ghost'
                  size='sm'
                  disabled={!requestRewriteEnabled}
                  onClick={() => removeRequestMessageRewrite(index)}
                >
                  <Trash2 className='text-destructive size-4' />
                </Button>
              </div>
            </div>
            <div className='space-y-2'>
              <Label htmlFor={`request_message_rewrite_content_${index}`}>
                {t('Content')}
              </Label>
              <Textarea
                id={`request_message_rewrite_content_${index}`}
                value={rewrite.content || ''}
                onChange={(e) =>
                  updateRequestMessageRewrite(index, {
                    content: e.target.value,
                  })
                }
                rows={4}
                className='font-mono text-xs'
                disabled={!requestRewriteEnabled}
                placeholder={t('New content or expression')}
              />
            </div>
          </div>
        ))}
        <Button
          type='button'
          variant='outline'
          size='sm'
          disabled={!requestRewriteEnabled}
          onClick={addRequestMessageRewrite}
        >
          <Plus className='size-4' />
          {t('Add Role Rewrite')}
        </Button>
      </div>
    </CollapsiblePayloadSection>
  )

  const renderResponseMatchFields = () => (
    <div className='space-y-4 rounded-md border p-3'>
      <div className='flex items-center gap-2'>
        <Checkbox
          id='response_match_enabled'
          checked={responseMatchEnabled}
          onCheckedChange={(checked) =>
            updateField('response_match_enabled', Boolean(checked))
          }
        />
        <Label htmlFor='response_match_enabled'>{t('Response Match')}</Label>
      </div>
      <div className={cn('space-y-4', !responseMatchEnabled && 'opacity-60')}>
        {renderBaseMatchFields(
          'response_match',
          {
            method: 'response_method',
            pathPattern: 'response_path_pattern',
            userId: 'response_user_id',
            username: 'response_username',
            modelPattern: 'response_model_pattern',
          },
          !responseMatchEnabled
        )}
        <div className='grid gap-3 sm:grid-cols-[220px_1fr]'>
          <div className='space-y-2'>
            <Label htmlFor='response_match_op'>{t('Match Logic')}</Label>
            <Select
              value={form.response_match_op || 'and'}
              disabled={!responseMatchEnabled}
              onValueChange={(value) =>
                updateField(
                  'response_match_op',
                  (value || 'and') as InterceptRule['response_match_op']
                )
              }
            >
              <SelectTrigger id='response_match_op' className='w-full'>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {MATCH_OPS.map((op) => (
                  <SelectItem key={op} value={op}>
                    {op === 'and' ? t('Match All') : t('Match Any')}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
          <p className='text-muted-foreground self-end pb-2 text-xs'>
            {t(
              'Controls the relationship between response content and tool calls conditions.'
            )}
          </p>
        </div>
        <div className='space-y-2'>
          <Label htmlFor='response_content_match'>
            {t('Response Content Match')}
          </Label>
          <Textarea
            id='response_content_match'
            value={form.response_content_match || ''}
            onChange={(e) =>
              updateField('response_content_match', e.target.value)
            }
            placeholder={t('Matches assistant response content')}
            rows={3}
            className='font-mono text-sm'
            disabled={!responseMatchEnabled}
          />
        </div>
        <div className='space-y-2'>
          <Label htmlFor='response_tool_calls_match'>
            {t('Response Tool Calls Match')}
          </Label>
          <Textarea
            id='response_tool_calls_match'
            value={form.response_tool_calls_match || ''}
            onChange={(e) =>
              updateField('response_tool_calls_match', e.target.value)
            }
            placeholder={t('Matches tool_calls or function name/arguments')}
            rows={3}
            className='font-mono text-sm'
            disabled={!responseMatchEnabled}
          />
          <p className='text-muted-foreground text-xs'>
            {t(
              'Leave empty to ignore response content or tool_calls. Filled conditions use the selected match logic.'
            )}
          </p>
        </div>
      </div>
    </div>
  )

  return (
    <Dialog open={props.open} onOpenChange={props.onOpenChange}>
      <DialogContent
        className={cn(
          'flex max-h-[90vh] flex-col overflow-hidden',
          props.sourceLog ? 'sm:max-w-5xl' : 'sm:max-w-3xl'
        )}
      >
        <DialogHeader>
          <DialogTitle>
            {isEdit ? t('Edit Intercept Rule') : t('Create Intercept Rule')}
          </DialogTitle>
          <DialogDescription>
            {t('Configure request/response interception and rewriting rules.')}
          </DialogDescription>
        </DialogHeader>

        <div className='min-h-0 flex-1 overflow-y-auto pr-1'>
          <Tabs
            value={activeTab}
            onValueChange={(value) => setActiveTab(value as typeof activeTab)}
          >
            <TabsList>
              <TabsTrigger value='basic'>{t('Basic')}</TabsTrigger>
              <TabsTrigger value='request'>{t('Request Actions')}</TabsTrigger>
              <TabsTrigger value='response'>
                {t('Response Actions')}
              </TabsTrigger>
              <TabsTrigger value='script'>{t('Script')}</TabsTrigger>
            </TabsList>

            <TabsContent value='basic' className='space-y-4'>
              <div className='grid gap-4 sm:grid-cols-2'>
                <div className='space-y-2'>
                  <Label htmlFor='name'>{t('Name')}</Label>
                  <Input
                    id='name'
                    value={form.name || ''}
                    onChange={(e) => updateField('name', e.target.value)}
                    placeholder={t('Rule name')}
                  />
                </div>
                <div className='space-y-2'>
                  <Label htmlFor='priority'>{t('Priority')}</Label>
                  <Input
                    id='priority'
                    type='number'
                    value={form.priority ?? 0}
                    onChange={(e) =>
                      updateField('priority', parseInt(e.target.value) || 0)
                    }
                  />
                  <p className='text-muted-foreground text-xs'>
                    {t('Higher priority rules are evaluated first')}
                  </p>
                </div>
              </div>
              <div className='grid gap-4 sm:grid-cols-2'>
                <div className='space-y-2'>
                  <Label htmlFor='match_limit'>{t('Match Count')}</Label>
                  <Input
                    id='match_limit'
                    type='number'
                    min={1}
                    value={matchLimitUnlimited ? '' : String(matchLimit)}
                    onChange={(e) =>
                      updateField(
                        'match_limit',
                        Math.max(1, parseInt(e.target.value) || 1)
                      )
                    }
                    placeholder={t('Unlimited')}
                    disabled={matchLimitUnlimited}
                  />
                </div>
                <div className='flex items-end pb-2'>
                  <div className='flex h-8 items-center gap-2'>
                    <Checkbox
                      id='match_limit_unlimited'
                      checked={matchLimitUnlimited}
                      onCheckedChange={(checked) =>
                        updateField(
                          'match_limit',
                          Boolean(checked) ? 0 : Math.max(1, matchLimit || 1)
                        )
                      }
                    />
                    <Label htmlFor='match_limit_unlimited'>
                      {t('Unlimited')}
                    </Label>
                  </div>
                </div>
              </div>
              <div className='space-y-2'>
                <Label htmlFor='description'>{t('Description')}</Label>
                <Textarea
                  id='description'
                  value={form.description || ''}
                  onChange={(e) => updateField('description', e.target.value)}
                  placeholder={t('Optional description')}
                  rows={2}
                />
              </div>
            </TabsContent>

            <TabsContent value='request' className='space-y-4'>
              {renderRequestMatchFields()}
              <div className='space-y-4 rounded-md border p-3'>
                <div className='flex items-center gap-2'>
                  <Checkbox
                    id='intercept_request'
                    checked={requestRewriteEnabled}
                    onCheckedChange={(checked) =>
                      updateField('intercept_request', Boolean(checked))
                    }
                  />
                  <Label htmlFor='intercept_request'>
                    {t('Request Rewrite')}
                  </Label>
                </div>
                <div
                  className={cn(
                    'space-y-4',
                    !requestRewriteEnabled && 'opacity-60'
                  )}
                >
                  {props.sourceLog && (
                    <PayloadRewriteTextarea
                      id='raw_request_body'
                      label={t('Request Body View')}
                      value={rawRequestBody}
                      onChange={setRawRequestBody}
                      resetKey={formResetKey}
                      rows={12}
                      disabled={!requestRewriteEnabled}
                      readOnly
                      previewPhase='request'
                      collapsible
                    />
                  )}
                  {renderRequestMessageRewriteFields()}
                </div>
              </div>
            </TabsContent>

            <TabsContent value='response' className='space-y-4'>
              {renderResponseMatchFields()}
              <div className='space-y-4 rounded-md border p-3'>
                <div className='flex items-center gap-2'>
                  <Checkbox
                    id='intercept_response'
                    checked={responseRewriteEnabled}
                    onCheckedChange={(checked) =>
                      updateField('intercept_response', Boolean(checked))
                    }
                  />
                  <Label htmlFor='intercept_response'>
                    {t('Response Rewrite')}
                  </Label>
                </div>
                <div
                  className={cn(
                    'space-y-4',
                    !responseRewriteEnabled && 'opacity-60'
                  )}
                >
                  <div className='space-y-2'>
                    <Label htmlFor='response_url_rewrite'>
                      {t('Response URL Rewrite')}
                    </Label>
                    <Textarea
                      id='response_url_rewrite'
                      value={form.response_url_rewrite || ''}
                      onChange={(e) =>
                        updateField('response_url_rewrite', e.target.value)
                      }
                      placeholder={t(
                        'Expression that returns the new response URL'
                      )}
                      rows={2}
                      className='font-mono text-sm'
                      disabled={!responseRewriteEnabled}
                    />
                  </div>
                  <div className='space-y-2'>
                    <Label htmlFor='response_status_rewrite'>
                      {t('Response Status Rewrite')}
                    </Label>
                    <Textarea
                      id='response_status_rewrite'
                      value={form.response_status_rewrite || ''}
                      onChange={(e) =>
                        updateField('response_status_rewrite', e.target.value)
                      }
                      placeholder={t(
                        'Expression that returns the new status code'
                      )}
                      rows={2}
                      className='font-mono text-sm'
                      disabled={!responseRewriteEnabled}
                    />
                  </div>
                  {props.sourceLog && (
                    <PayloadRewriteTextarea
                      id='raw_response_body'
                      label={t('Response Body View')}
                      value={rawResponseBody}
                      onChange={setRawResponseBody}
                      resetKey={formResetKey}
                      rows={12}
                      disabled={!responseRewriteEnabled}
                      readOnly
                      previewPhase='response'
                      collapsible
                    />
                  )}
                  <PayloadRewriteTextarea
                    id='response_content_rewrite'
                    label={t('Response Content Rewrite')}
                    value={form.response_content_rewrite || ''}
                    onChange={(value) =>
                      updateField('response_content_rewrite', value)
                    }
                    resetKey={formResetKey}
                    rows={6}
                    disabled={!responseRewriteEnabled}
                    placeholder={t('New assistant response content')}
                    collapsible
                    defaultOpen={Boolean(form.response_content_rewrite)}
                  />
                  <PayloadRewriteTextarea
                    id='response_tool_calls_rewrite'
                    label={t('Response Tool Calls Rewrite')}
                    value={form.response_tool_calls_rewrite || ''}
                    onChange={(value) =>
                      updateField('response_tool_calls_rewrite', value)
                    }
                    resetKey={formResetKey}
                    rows={6}
                    disabled={!responseRewriteEnabled}
                    placeholder={t('JSON array of replacement tool_calls')}
                    collapsible
                    defaultOpen={Boolean(form.response_tool_calls_rewrite)}
                  />
                  {!props.sourceLog && Boolean(form.response_body_rewrite) && (
                    <PayloadRewriteTextarea
                      id='response_body_rewrite'
                      label={t('Legacy Response Body Rewrite')}
                      value={form.response_body_rewrite || ''}
                      onChange={(value) =>
                        updateField('response_body_rewrite', value)
                      }
                      resetKey={formResetKey}
                      rows={6}
                      disabled={!responseRewriteEnabled}
                      expressionValue
                      previewPhase='response'
                      placeholder={t(
                        'Expression: use "response" variable for the body string'
                      )}
                      collapsible
                    />
                  )}
                </div>
              </div>
            </TabsContent>

            <TabsContent value='script' className='space-y-4'>
              <div className='space-y-4 rounded-md border p-3'>
                <div className='flex items-center gap-2'>
                  <Checkbox
                    id='script_enabled'
                    checked={scriptEnabled}
                    onCheckedChange={(checked) => {
                      const enabled = Boolean(checked)
                      setForm((prev) => ({
                        ...prev,
                        script_enabled: enabled,
                        script: prev.script?.trim()
                          ? prev.script
                          : SCRIPT_TEMPLATE,
                      }))
                    }}
                  />
                  <Label htmlFor='script_enabled'>{t('Script Rewrite')}</Label>
                </div>
                <div className='space-y-2'>
                  <Label htmlFor='script'>{t('Script')}</Label>
                  <Textarea
                    id='script'
                    value={form.script || SCRIPT_TEMPLATE}
                    onChange={(e) => updateField('script', e.target.value)}
                    placeholder={SCRIPT_TEMPLATE}
                    rows={18}
                    className='font-mono text-sm'
                  />
                  <p className='text-muted-foreground text-xs'>
                    {t(
                      'Use JavaScript onRequest/onResponse hooks; script can mutate both request and response objects.'
                    )}
                  </p>
                </div>
              </div>
            </TabsContent>
          </Tabs>
        </div>

        <DialogFooter>
          <Button
            type='button'
            variant='outline'
            onClick={() => props.onOpenChange(false)}
          >
            {t('Cancel')}
          </Button>
          <Button type='button' onClick={handleSave} disabled={saving}>
            {saving ? t('Saving...') : isEdit ? t('Update') : t('Create')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
