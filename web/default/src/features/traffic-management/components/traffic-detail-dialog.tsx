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
import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { ChevronDown, ChevronRight } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { formatTimestampToDate } from '@/lib/format'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from '@/components/ui/collapsible'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { getTrafficLog } from '../api'
import type { TrafficLog } from '../types'
import { formatBytes } from './traffic-format'
import { TrafficRawSection } from './traffic-raw-section'

interface TrafficDetailDialogProps {
  log: TrafficLog | null
  open: boolean
  onOpenChange: (open: boolean) => void
}

function DetailItem(props: { label: string; value?: string | number }) {
  return (
    <div className='bg-muted/20 min-w-0 rounded-lg border px-3 py-2'>
      <div className='text-muted-foreground text-xs'>{props.label}</div>
      <div className='mt-1 truncate font-mono text-xs'>
        {props.value || '-'}
      </div>
    </div>
  )
}

function BodyMeta(props: {
  contentType: string
  size: number
  truncated: boolean
  truncatedBytes: number
}) {
  const { t } = useTranslation()
  const meta = `${formatBytes(props.size)}${props.truncated ? ` · ${t('truncated {{bytes}}', { bytes: formatBytes(props.truncatedBytes) })}` : ''}`

  return (
    <>
      <Badge variant='outline'>{meta}</Badge>
      {props.contentType && (
        <Badge variant='secondary' className='max-w-[260px] truncate'>
          {props.contentType}
        </Badge>
      )}
    </>
  )
}

function OverviewSection(props: { log: TrafficLog }) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  const log = props.log

  return (
    <Collapsible open={open} onOpenChange={setOpen}>
      <div className='rounded-lg border'>
        <div className='flex min-w-0 items-center justify-between gap-2 px-3 py-2'>
          <div className='flex min-w-0 items-center gap-2'>
            <CollapsibleTrigger
              render={
                <Button
                  type='button'
                  variant='ghost'
                  size='icon-sm'
                  className='shrink-0'
                  aria-label={t('Overview')}
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
              <div className='truncate text-sm font-medium'>
                {t('Overview')}
              </div>
              <div className='text-muted-foreground mt-1 flex min-w-0 flex-wrap items-center gap-1.5 text-xs'>
                <Badge variant='outline'>{log.method || 'HTTP'}</Badge>
                <Badge variant='outline'>{log.status_code || '-'}</Badge>
                <Badge variant='outline'>{log.duration_ms}ms</Badge>
              </div>
            </div>
          </div>
        </div>
        <CollapsibleContent>
          <div className='grid gap-2 border-t p-3 sm:grid-cols-2 lg:grid-cols-4'>
            <DetailItem
              label={t('Time')}
              value={formatTimestampToDate(log.created_at)}
            />
            <DetailItem label={t('Status')} value={log.status_code} />
            <DetailItem label={t('Duration')} value={`${log.duration_ms}ms`} />
            <DetailItem label={t('Model')} value={log.model_name} />
            <DetailItem
              label={t('User')}
              value={`${log.username || '-'} #${log.user_id}`}
            />
            <DetailItem
              label={t('Token')}
              value={log.token_name || `#${log.token_id}`}
            />
            <DetailItem
              label={t('Channel')}
              value={
                log.channel_name
                  ? `${log.channel_name} #${log.channel}`
                  : `#${log.channel}`
              }
            />
            <DetailItem label={t('Group')} value={log.group} />
            <DetailItem label={t('Request ID')} value={log.request_id} />
            <DetailItem
              label={t('Upstream Request ID')}
              value={log.upstream_request_id}
            />
            <DetailItem label={t('IP')} value={log.ip} />
            <DetailItem label={t('User Agent')} value={log.user_agent} />
          </div>
        </CollapsibleContent>
      </div>
    </Collapsible>
  )
}

export function TrafficDetailDialog(props: TrafficDetailDialogProps) {
  const { t } = useTranslation()
  const log = props.log

  const { data: detailLog, isFetching } = useQuery({
    queryKey: ['traffic-log', log?.id],
    queryFn: async () => {
      if (!log?.id) return null
      const result = await getTrafficLog(log.id)
      if (!result.success || !result.data) {
        throw new Error(result.message || t('Failed to load traffic logs'))
      }
      return result.data
    },
    enabled: props.open && Boolean(log?.id),
    placeholderData: log,
  })

  if (!log) {
    return null
  }

  const currentLog = detailLog || log
  const title = `${currentLog.method || 'HTTP'} ${currentLog.path || '-'}`

  return (
    <Dialog open={props.open} onOpenChange={props.onOpenChange}>
      <DialogContent className='flex max-h-[90vh] flex-col overflow-hidden sm:max-w-5xl'>
        <DialogHeader>
          <DialogTitle className='flex items-center gap-2'>
            {t('Traffic Details')}
            {isFetching && <Badge variant='secondary'>{t('Loading...')}</Badge>}
          </DialogTitle>
          <DialogDescription className='truncate font-mono'>
            {title}
          </DialogDescription>
        </DialogHeader>

        <div className='min-h-0 flex-1 space-y-4 overflow-y-auto pr-1'>
          <OverviewSection log={currentLog} />

          <Tabs defaultValue='request'>
            <TabsList>
              <TabsTrigger value='request'>{t('Request')}</TabsTrigger>
              <TabsTrigger value='response'>{t('Response')}</TabsTrigger>
            </TabsList>
            <TabsContent value='request' className='space-y-3'>
              <TrafficRawSection
                title={t('Request Headers')}
                value={currentLog.request_headers || '{}'}
                emptyText='{}'
                defaultOpen
                formattable
              />
              <TrafficRawSection
                title={t('Request Body')}
                value={currentLog.request_body}
                emptyText={t('Empty body')}
                defaultOpen
                formattable
                previewPhase='request'
                searchable
                meta={
                  <BodyMeta
                    contentType={currentLog.request_content_type}
                    size={currentLog.request_body_size}
                    truncated={currentLog.request_body_truncated}
                    truncatedBytes={currentLog.request_body_truncated_bytes}
                  />
                }
              />
            </TabsContent>
            <TabsContent value='response' className='space-y-3'>
              <TrafficRawSection
                title={t('Response Headers')}
                value={currentLog.response_headers || '{}'}
                emptyText='{}'
                defaultOpen
                formattable
              />
              <TrafficRawSection
                title={t('Response Body')}
                value={currentLog.response_body}
                emptyText={t('Empty body')}
                defaultOpen
                formattable
                previewPhase='response'
                searchable
                meta={
                  <BodyMeta
                    contentType={currentLog.response_content_type}
                    size={currentLog.response_body_size}
                    truncated={currentLog.response_body_truncated}
                    truncatedBytes={currentLog.response_body_truncated_bytes}
                  />
                }
              />
            </TabsContent>
          </Tabs>
        </div>
      </DialogContent>
    </Dialog>
  )
}
