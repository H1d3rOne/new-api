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
import { useCallback, useEffect, useState } from 'react'
import { useIsFetching, useQueryClient } from '@tanstack/react-query'
import { getRouteApi, useNavigate } from '@tanstack/react-router'
import { type Table } from '@tanstack/react-table'
import { useTranslation } from 'react-i18next'
import { CompactDateTimeRangePicker } from '@/features/usage-logs/components/compact-date-time-range-picker'
import {
  LogsFilterField,
  LogsFilterInput,
  LogsFilterToolbar,
} from '@/features/usage-logs/components/logs-filter-toolbar'
import { getDefaultTimeRange } from '@/features/usage-logs/lib/utils'

const route = getRouteApi('/_authenticated/usage-logs/$section')

interface TrafficFilters {
  startTime?: Date
  endTime?: Date
  username?: string
  model?: string
  token?: string
  channel?: string
  group?: string
  statusCode?: string
  requestId?: string
  upstreamRequestId?: string
}

interface TrafficFilterToolbarProps<TData> {
  table: Table<TData>
}

export function TrafficFilterToolbar<TData>(
  props: TrafficFilterToolbarProps<TData>
) {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const searchParams = route.useSearch()
  const fetchingTrafficLogs = useIsFetching({ queryKey: ['traffic-logs'] })
  const [filters, setFilters] = useState<TrafficFilters>(() => {
    const range = getDefaultTimeRange()
    return { startTime: range.start, endTime: range.end }
  })

  useEffect(() => {
    const range = getDefaultTimeRange()
    setFilters({
      startTime: searchParams.startTime
        ? new Date(searchParams.startTime)
        : range.start,
      endTime: searchParams.endTime
        ? new Date(searchParams.endTime)
        : range.end,
      username: searchParams.username || undefined,
      model: searchParams.model || undefined,
      token: searchParams.token || undefined,
      channel: searchParams.channel || undefined,
      group: searchParams.group || undefined,
      statusCode: searchParams.statusCode
        ? String(searchParams.statusCode)
        : undefined,
      requestId: searchParams.requestId || undefined,
      upstreamRequestId: searchParams.upstreamRequestId || undefined,
    })
  }, [
    searchParams.startTime,
    searchParams.endTime,
    searchParams.username,
    searchParams.model,
    searchParams.token,
    searchParams.channel,
    searchParams.group,
    searchParams.statusCode,
    searchParams.requestId,
    searchParams.upstreamRequestId,
  ])

  const handleChange = useCallback(
    (field: keyof TrafficFilters, value: Date | string | undefined) => {
      setFilters((prev) => ({ ...prev, [field]: value }))
    },
    []
  )

  const buildSearch = useCallback(
    () => ({
      page: 1,
      startTime: filters.startTime?.getTime(),
      endTime: filters.endTime?.getTime(),
      username: filters.username || undefined,
      model: filters.model || undefined,
      token: filters.token || undefined,
      channel: filters.channel || undefined,
      group: filters.group || undefined,
      statusCode: filters.statusCode ? Number(filters.statusCode) : undefined,
      requestId: filters.requestId || undefined,
      upstreamRequestId: filters.upstreamRequestId || undefined,
    }),
    [filters]
  )

  const handleApply = useCallback(() => {
    navigate({
      to: '/usage-logs/$section',
      params: { section: 'traffic' },
      search: buildSearch(),
    })
    queryClient.invalidateQueries({ queryKey: ['traffic-logs'] })
  }, [buildSearch, navigate, queryClient])

  const handleReset = useCallback(() => {
    const range = getDefaultTimeRange()
    setFilters({ startTime: range.start, endTime: range.end })
    navigate({
      to: '/usage-logs/$section',
      params: { section: 'traffic' },
      search: {
        page: 1,
        startTime: range.start.getTime(),
        endTime: range.end.getTime(),
      },
    })
    queryClient.invalidateQueries({ queryKey: ['traffic-logs'] })
  }, [navigate, queryClient])

  const handleKeyDown = useCallback(
    (e: React.KeyboardEvent) => {
      if (e.key === 'Enter') handleApply()
    },
    [handleApply]
  )

  const dateRangeFilter = (
    <LogsFilterField wide>
      <CompactDateTimeRangePicker
        start={filters.startTime}
        end={filters.endTime}
        onChange={(range) => {
          handleChange('startTime', range.start)
          handleChange('endTime', range.end)
        }}
      />
    </LogsFilterField>
  )

  const modelFilter = (
    <LogsFilterField>
      <LogsFilterInput
        placeholder={t('Model')}
        value={filters.model || ''}
        onChange={(e) => handleChange('model', e.target.value)}
        onKeyDown={handleKeyDown}
      />
    </LogsFilterField>
  )

  const usernameFilter = (
    <LogsFilterField>
      <LogsFilterInput
        placeholder={t('Username')}
        value={filters.username || ''}
        onChange={(e) => handleChange('username', e.target.value)}
        onKeyDown={handleKeyDown}
      />
    </LogsFilterField>
  )

  const advancedFilters = (
    <>
      <LogsFilterField>
        <LogsFilterInput
          placeholder={t('Token')}
          value={filters.token || ''}
          onChange={(e) => handleChange('token', e.target.value)}
          onKeyDown={handleKeyDown}
        />
      </LogsFilterField>
      <LogsFilterField>
        <LogsFilterInput
          placeholder={t('Channel ID')}
          value={filters.channel || ''}
          onChange={(e) => handleChange('channel', e.target.value)}
          onKeyDown={handleKeyDown}
        />
      </LogsFilterField>
      <LogsFilterField>
        <LogsFilterInput
          placeholder={t('Status Code')}
          value={filters.statusCode || ''}
          onChange={(e) => handleChange('statusCode', e.target.value)}
          onKeyDown={handleKeyDown}
        />
      </LogsFilterField>
      <LogsFilterField>
        <LogsFilterInput
          placeholder={t('Group')}
          value={filters.group || ''}
          onChange={(e) => handleChange('group', e.target.value)}
          onKeyDown={handleKeyDown}
        />
      </LogsFilterField>
      <LogsFilterField>
        <LogsFilterInput
          placeholder={t('Request ID')}
          value={filters.requestId || ''}
          onChange={(e) => handleChange('requestId', e.target.value)}
          onKeyDown={handleKeyDown}
        />
      </LogsFilterField>
      <LogsFilterField>
        <LogsFilterInput
          placeholder={t('Upstream Request ID')}
          value={filters.upstreamRequestId || ''}
          onChange={(e) => handleChange('upstreamRequestId', e.target.value)}
          onKeyDown={handleKeyDown}
        />
      </LogsFilterField>
    </>
  )

  const advancedFilterCount = [
    filters.token,
    filters.channel,
    filters.statusCode,
    filters.group,
    filters.requestId,
    filters.upstreamRequestId,
  ].filter(Boolean).length
  const hasActiveFilters = Boolean(
    filters.username || filters.model || advancedFilterCount > 0
  )

  return (
    <LogsFilterToolbar
      table={props.table}
      primaryFilters={
        <>
          {dateRangeFilter}
          {modelFilter}
          {usernameFilter}
        </>
      }
      advancedFilters={advancedFilters}
      mobilePinnedFilters={dateRangeFilter}
      mobileFilters={
        <>
          {modelFilter}
          {usernameFilter}
          {advancedFilters}
        </>
      }
      mobileFilterCount={
        [filters.model, filters.username].filter(Boolean).length +
        advancedFilterCount
      }
      hasActiveFilters={hasActiveFilters}
      hasAdvancedActiveFilters={advancedFilterCount > 0}
      advancedFilterCount={advancedFilterCount}
      searchLoading={fetchingTrafficLogs > 0}
      onReset={handleReset}
      onSearch={handleApply}
    />
  )
}
