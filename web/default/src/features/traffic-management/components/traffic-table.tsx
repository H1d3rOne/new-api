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
import { useEffect, useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { getRouteApi } from '@tanstack/react-router'
import {
  flexRender,
  getCoreRowModel,
  getFacetedRowModel,
  getFacetedUniqueValues,
  getFilteredRowModel,
  getPaginationRowModel,
  useReactTable,
  type ColumnDef,
} from '@tanstack/react-table'
import { useMediaQuery } from '@/hooks'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { cn } from '@/lib/utils'
import { useTableUrlState } from '@/hooks/use-table-url-state'
import { TableCell, TableRow } from '@/components/ui/table'
import { DataTablePage } from '@/components/data-table'
import { getTrafficLog, getTrafficLogs } from '../api'
import type { InterceptRule, TrafficLog, TrafficLogParams } from '../types'
import { InterceptRuleDialog } from './intercept-rule-dialog'
import {
  collectTrafficResponseFunctionNames,
  createTrafficResponseContentRewrite,
  createTrafficResponseToolCallsRewrite,
  createTrafficBodyPreview,
} from './traffic-body-preview'
import { useTrafficColumns } from './traffic-columns'
import { TrafficDetailDialog } from './traffic-detail-dialog'
import { TrafficFilterToolbar } from './traffic-filter-toolbar'
import { TrafficReplayDialog } from './traffic-replay-dialog'

const route = getRouteApi('/_authenticated/usage-logs/$section')

const DEFAULT_TRAFFIC_DATA = {
  items: [] as TrafficLog[],
  total: 0,
  page: 1,
  page_size: 20,
}

function timestampToSeconds(timestamp?: number): number | undefined {
  return timestamp ? Math.floor(timestamp / 1000) : undefined
}

function escapeRegExp(value: string): string {
  return value.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
}

function canUseLoggedBody(log: TrafficLog, phase: 'request' | 'response') {
  const body =
    phase === 'request' ? log.request_body || '' : log.response_body || ''
  const truncated =
    phase === 'request'
      ? log.request_body_truncated
      : log.response_body_truncated
  return body !== '' && !truncated && !body.startsWith('[base64]')
}

function canSafelyMatchLoggedRequest(log: TrafficLog) {
  return (log.request_body_size || 0) === 0 || canUseLoggedBody(log, 'request')
}

function trafficBodyPreviewText(
  log: TrafficLog,
  phase: 'request' | 'response',
  mode: 'text' | 'all'
) {
  if (!canUseLoggedBody(log, phase)) return ''
  return createTrafficBodyPreview(
    phase === 'request' ? log.request_body : log.response_body,
    phase,
    mode
  )?.text.trim()
}

function buildTrafficRewriteRule(
  log: TrafficLog,
  ruleName: string,
  description: string
): Partial<InterceptRule> {
  const safeRequestMatch = canSafelyMatchLoggedRequest(log)
  const requestContent = trafficBodyPreviewText(log, 'request', 'text')
  const responseContent = trafficBodyPreviewText(log, 'response', 'text')
  const responseFunctionNames = canUseLoggedBody(log, 'response')
    ? collectTrafficResponseFunctionNames(log.response_body)
    : []
  const responseContentRewrite =
    safeRequestMatch && canUseLoggedBody(log, 'response')
      ? createTrafficResponseContentRewrite(log.response_body)
      : ''
  const responseToolCallsRewrite =
    safeRequestMatch && canUseLoggedBody(log, 'response')
      ? createTrafficResponseToolCallsRewrite(log.response_body)
      : ''
  const hasResponseMatch =
    Boolean(responseContent) || responseFunctionNames.length > 0
  return {
    name: ruleName,
    description,
    priority: 0,
    enabled: safeRequestMatch,
    match_limit: 0,
    match_count: 0,
    user_id: log.user_id || 0,
    username: log.username || '',
    path_pattern: log.path ? `^${escapeRegExp(log.path)}$` : '',
    method: log.method || '',
    model_pattern: log.model_name ? `^${escapeRegExp(log.model_name)}$` : '',
    request_content_match: '',
    request_message_matches: requestContent
      ? JSON.stringify([
          {
            role: 'user',
            mode: 'latest',
            content: requestContent,
            content_op: 'and',
          },
        ])
      : '[]',
    request_message_match_op: 'and',
    condition_expr: '',
    response_user_id: 0,
    response_username: '',
    response_path_pattern: '',
    response_method: '',
    response_model_pattern: log.model_name
      ? `^${escapeRegExp(log.model_name)}$`
      : '',
    response_content_match: responseContent || '',
    response_tool_calls_match: responseFunctionNames[0] || '',
    response_match_op: 'and',
    response_condition_expr: '',
    request_match_enabled: safeRequestMatch,
    response_match_enabled: safeRequestMatch && hasResponseMatch,
    intercept_request: true,
    intercept_response: safeRequestMatch,
    script_enabled: false,
    block_enabled: false,
    block_status_code: log.status_code || 200,
    block_content_type: log.response_content_type || 'application/json',
    block_body: '',
    request_header_ops: '[]',
    request_body_rewrite: '',
    request_message_rewrites: requestContent
      ? JSON.stringify([
          { role: 'user', mode: 'latest', content: requestContent },
        ])
      : '[]',
    request_url_rewrite: '',
    request_script: '',
    response_header_ops: '[]',
    response_body_rewrite: '',
    response_content_rewrite: responseContentRewrite,
    response_tool_calls_rewrite: responseToolCallsRewrite,
    response_status_rewrite:
      safeRequestMatch && log.status_code ? String(log.status_code) : '',
    response_url_rewrite: '',
    response_script: '',
    script: '',
  }
}

function buildTrafficParams(config: {
  page: number
  pageSize: number
  searchParams: Record<string, unknown>
}): TrafficLogParams {
  const searchParams = config.searchParams
  return {
    p: config.page,
    page_size: config.pageSize,
    username: searchParams.username ? String(searchParams.username) : undefined,
    token_name: searchParams.token ? String(searchParams.token) : undefined,
    model_name: searchParams.model ? String(searchParams.model) : undefined,
    channel: searchParams.channel
      ? Number(searchParams.channel) || 0
      : undefined,
    group: searchParams.group ? String(searchParams.group) : undefined,
    status_code: searchParams.statusCode
      ? Number(searchParams.statusCode) || 0
      : undefined,
    request_id: searchParams.requestId
      ? String(searchParams.requestId)
      : undefined,
    upstream_request_id: searchParams.upstreamRequestId
      ? String(searchParams.upstreamRequestId)
      : undefined,
    start_timestamp: timestampToSeconds(searchParams.startTime as number),
    end_timestamp: timestampToSeconds(searchParams.endTime as number),
  }
}

interface TrafficTableProps {
  onRewriteRuleSaved?: () => void
}

export function TrafficTable(props: TrafficTableProps) {
  const { t } = useTranslation()
  const isMobile = useMediaQuery('(max-width: 640px)')
  const searchParams = route.useSearch()
  const queryClient = useQueryClient()
  const [selectedLog, setSelectedLog] = useState<TrafficLog | null>(null)
  const [detailOpen, setDetailOpen] = useState(false)
  const [rewriteRule, setRewriteRule] = useState<Partial<InterceptRule> | null>(
    null
  )
  const [rewriteSourceLog, setRewriteSourceLog] = useState<TrafficLog | null>(
    null
  )
  const [rewriteOpen, setRewriteOpen] = useState(false)
  const [replayLog, setReplayLog] = useState<TrafficLog | null>(null)
  const [replayOpen, setReplayOpen] = useState(false)

  const {
    pagination,
    onPaginationChange,
    ensurePageInRange,
    columnFilters,
    onColumnFiltersChange,
  } = useTableUrlState({
    search: searchParams,
    navigate: route.useNavigate(),
    pagination: { defaultPage: 1, defaultPageSize: isMobile ? 20 : 100 },
    globalFilter: { enabled: false },
    columnFilters: [],
  })

  const { data, isLoading, isFetching } = useQuery({
    queryKey: [
      'traffic-logs',
      pagination.pageIndex + 1,
      pagination.pageSize,
      searchParams,
    ],
    queryFn: async () => {
      const result = await getTrafficLogs(
        buildTrafficParams({
          page: pagination.pageIndex + 1,
          pageSize: pagination.pageSize,
          searchParams,
        })
      )
      if (!result.success) {
        toast.error(result.message || t('Failed to load traffic logs'))
        return DEFAULT_TRAFFIC_DATA
      }
      return result.data || DEFAULT_TRAFFIC_DATA
    },
    placeholderData: (previousData) => previousData,
  })

  const loadFullLog = async (log: TrafficLog) => {
    const result = await getTrafficLog(log.id)
    if (!result.success || !result.data) {
      throw new Error(result.message || t('Failed to load traffic logs'))
    }
    return result.data
  }

  const columns = useTrafficColumns({
    onView: async (log) => {
      try {
        setSelectedLog(await loadFullLog(log))
        setDetailOpen(true)
      } catch (err) {
        toast.error(
          err instanceof Error ? err.message : t('Failed to load traffic logs')
        )
      }
    },
    onRewrite: async (log) => {
      try {
        const fullLog = await loadFullLog(log)
        setRewriteSourceLog(fullLog)
        setRewriteRule(
          buildTrafficRewriteRule(
            fullLog,
            t('Rewrite traffic log #{{id}}', { id: fullLog.id }),
            t('Generated from traffic log #{{id}}', { id: fullLog.id })
          )
        )
        setRewriteOpen(true)
      } catch (err) {
        toast.error(
          err instanceof Error ? err.message : t('Failed to load traffic logs')
        )
      }
    },
    onReplay: async (log) => {
      if (!log.token_id) {
        toast.error(t('Traffic log has no replayable token'))
        return
      }
      try {
        setReplayLog(await loadFullLog(log))
        setReplayOpen(true)
      } catch (err) {
        toast.error(
          err instanceof Error ? err.message : t('Failed to load traffic logs')
        )
      }
    },
  })
  const logs = data?.items || []
  const isLoadingData = isLoading || (isFetching && !data)

  const table = useReactTable({
    data: logs,
    columns,
    state: {
      pagination,
      columnFilters,
    },
    enableRowSelection: false,
    onPaginationChange,
    onColumnFiltersChange,
    getCoreRowModel: getCoreRowModel(),
    getFilteredRowModel: getFilteredRowModel(),
    getPaginationRowModel: getPaginationRowModel(),
    getFacetedRowModel: getFacetedRowModel(),
    getFacetedUniqueValues: getFacetedUniqueValues(),
    manualPagination: true,
    manualFiltering: true,
    pageCount: Math.ceil((data?.total || 0) / pagination.pageSize),
  })

  const pageCount = table.getPageCount()
  useEffect(() => {
    ensurePageInRange(pageCount)
  }, [pageCount, ensurePageInRange])

  return (
    <>
      <DataTablePage
        table={table}
        columns={columns as ColumnDef<TrafficLog, unknown>[]}
        isLoading={isLoadingData}
        isFetching={isFetching}
        emptyTitle={t('No Traffic Logs Found')}
        emptyDescription={t(
          'No captured relay traffic is available for the selected filters.'
        )}
        skeletonKeyPrefix='traffic-log-skeleton'
        tableClassName={cn(
          'overflow-x-auto',
          '[&_[data-slot=table]]:text-[13px] [&_[data-slot=table]_td]:text-[13px] [&_[data-slot=table]_td_*]:text-[13px] [&_[data-slot=table]_th]:text-[13px] [&_[data-slot=table]_th_*]:text-[13px]'
        )}
        tableHeaderClassName='bg-muted/30 sticky top-0 z-10'
        toolbar={<TrafficFilterToolbar table={table} />}
        renderRow={(row) => (
          <TableRow key={row.id} className='transition-colors'>
            {row.getVisibleCells().map((cell) => (
              <TableCell key={cell.id} className='py-2'>
                {flexRender(cell.column.columnDef.cell, cell.getContext())}
              </TableCell>
            ))}
          </TableRow>
        )}
      />

      <TrafficDetailDialog
        log={selectedLog}
        open={detailOpen}
        onOpenChange={setDetailOpen}
      />

      <InterceptRuleDialog
        rule={rewriteRule}
        sourceLog={rewriteSourceLog}
        open={rewriteOpen}
        initialTab='request'
        onOpenChange={(open) => {
          setRewriteOpen(open)
          if (!open) {
            setRewriteSourceLog(null)
          }
        }}
        onSaved={() => {
          queryClient.invalidateQueries({ queryKey: ['intercept-rules'] })
          props.onRewriteRuleSaved?.()
        }}
      />

      <TrafficReplayDialog
        log={replayLog}
        open={replayOpen}
        onOpenChange={setReplayOpen}
      />
    </>
  )
}
