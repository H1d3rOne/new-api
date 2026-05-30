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
import { Braces, ChevronDown, ChevronRight, Eye } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { cn } from '@/lib/utils'
import { Button } from '@/components/ui/button'
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from '@/components/ui/collapsible'
import { Textarea } from '@/components/ui/textarea'
import { CopyButton } from '@/components/copy-button'
import { TrafficBodySearch } from './traffic-body-search'
import {
  createTrafficBodyPreview,
  type TrafficBodyPreviewMode,
} from './traffic-body-preview'
import { formatTrafficPayload } from './traffic-payload-format'

interface TrafficRawSectionProps {
  title: string
  value: string
  defaultOpen?: boolean
  emptyText?: string
  meta?: ReactNode
  className?: string
  contentClassName?: string
  formattable?: boolean
  previewPhase?: 'request' | 'response'
  searchable?: boolean
}

export function TrafficRawSection(props: TrafficRawSectionProps) {
  const { t } = useTranslation()
  const textareaRef = useRef<HTMLTextAreaElement | null>(null)
  const [open, setOpen] = useState(props.defaultOpen ?? true)
  const [formattedValue, setFormattedValue] = useState<string | null>(null)
  const [previewMode, setPreviewMode] = useState<TrafficBodyPreviewMode | null>(
    null
  )
  const preview =
    props.previewPhase && previewMode
      ? createTrafficBodyPreview(props.value, props.previewPhase, previewMode)
      : null
  const rawDisplayValue = preview
    ? preview.text
    : (formattedValue ?? props.value)
  const displayValue = rawDisplayValue || props.emptyText || ''
  const previewActive = previewMode !== null

  const previewLabel = (mode: TrafficBodyPreviewMode) =>
    previewMode === mode
      ? t('Full Body')
      : mode === 'text'
        ? t('Text Preview')
        : t('Full Preview')

  useEffect(() => {
    setFormattedValue(null)
    setPreviewMode(null)
  }, [props.previewPhase, props.value])

  const handleFormat = () => {
    if (previewActive) return
    if (formattedValue !== null) {
      setFormattedValue(null)
      return
    }
    try {
      setFormattedValue(formatTrafficPayload(props.value))
      setOpen(true)
    } catch {
      toast.error(t('Invalid JSON'))
    }
  }

  const handlePreviewToggle = (mode: TrafficBodyPreviewMode) => {
    if (previewMode === mode) {
      setPreviewMode(null)
      return
    }
    if (
      !props.previewPhase ||
      !createTrafficBodyPreview(props.value, props.previewPhase, mode)
    ) {
      toast.error(t('No preview content found'))
      return
    }
    setFormattedValue(null)
    setPreviewMode(mode)
    setOpen(true)
  }

  return (
    <Collapsible open={open} onOpenChange={setOpen}>
      <div className={cn('rounded-lg border', props.className)}>
        <div className='flex min-w-0 items-center justify-between gap-2 px-3 py-2'>
          <div className='flex min-w-0 items-center gap-2'>
            <CollapsibleTrigger
              render={
                <Button
                  type='button'
                  variant='ghost'
                  size='icon-sm'
                  className='shrink-0'
                  aria-label={props.title}
                />
              }
            >
              {open ? (
                <ChevronDown className='size-4' />
              ) : (
                <ChevronRight className='size-4' />
              )}
            </CollapsibleTrigger>
            <div className='min-w-0'>
              <div className='truncate text-sm font-medium'>{props.title}</div>
              {props.meta && (
                <div className='text-muted-foreground mt-1 flex min-w-0 flex-wrap items-center gap-1.5 text-xs'>
                  {props.meta}
                </div>
              )}
            </div>
          </div>
          <div className='flex shrink-0 items-center gap-1'>
            {props.previewPhase && (
              <>
                <Button
                  type='button'
                  variant='ghost'
                  size='icon-sm'
                  onClick={() => handlePreviewToggle('text')}
                  aria-label={previewLabel('text')}
                  title={previewLabel('text')}
                >
                  <Eye className='size-4' />
                </Button>
                <Button
                  type='button'
                  variant='ghost'
                  size='icon-sm'
                  onClick={() => handlePreviewToggle('all')}
                  aria-label={previewLabel('all')}
                  title={previewLabel('all')}
                >
                  <Eye className='size-4' />
                </Button>
              </>
            )}
            {props.formattable && (
              <Button
                type='button'
                variant='ghost'
                size='icon-sm'
                onClick={handleFormat}
                disabled={previewActive}
                aria-label={
                  formattedValue !== null
                    ? t('Cancel Format')
                    : t('Format JSON')
                }
              >
                <Braces className='size-4' />
              </Button>
            )}
            <CopyButton value={displayValue} size='icon' tooltip={t('Copy')} />
          </div>
        </div>
        <CollapsibleContent>
          {props.searchable ? (
            <div className='space-y-2 border-t p-3'>
              <TrafficBodySearch
                value={displayValue}
                textareaRef={textareaRef}
              />
              <Textarea
                ref={textareaRef}
                aria-label={props.title}
                value={displayValue}
                readOnly
                className={cn(
                  'bg-background text-foreground max-h-[320px] min-h-[140px] resize-y overflow-auto font-mono text-xs leading-relaxed',
                  props.contentClassName
                )}
              />
            </div>
          ) : (
            <pre
              aria-label={props.title}
              tabIndex={0}
              className={cn(
                'bg-background text-foreground max-h-[320px] min-h-[140px] overflow-auto border-t p-3 font-mono text-xs leading-relaxed break-words whitespace-pre-wrap',
                props.contentClassName
              )}
            >
              {displayValue}
            </pre>
          )}
        </CollapsibleContent>
      </div>
    </Collapsible>
  )
}
