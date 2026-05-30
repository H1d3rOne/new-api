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
import { api } from '@/lib/api'
import type {
  ApiResponse,
  InterceptSettings,
  InterceptRule,
  InterceptRulesResponse,
  LiveInterceptDecisionPayload,
  LiveInterceptEvent,
  LiveInterceptSettings,
  TrafficReplayRequest,
  TrafficReplayResponse,
  TrafficLog,
  TrafficLogParams,
  TrafficLogsResponse,
} from './types'

function buildQueryParams(params: TrafficLogParams): URLSearchParams {
  const queryParams = new URLSearchParams()

  Object.entries(params).forEach(([key, value]) => {
    if (value !== undefined && value !== null && value !== '') {
      queryParams.append(key, String(value))
    }
  })

  return queryParams
}

export async function getTrafficLogs(
  params: TrafficLogParams = {}
): Promise<TrafficLogsResponse> {
  const queryParams = buildQueryParams(params)
  const res = await api.get(`/api/traffic/?${queryParams.toString()}`)
  return res.data
}

export async function getTrafficLog(
  id: number
): Promise<ApiResponse<TrafficLog>> {
  const res = await api.get(`/api/traffic/${id}`)
  return res.data
}

export async function replayTrafficLog(
  id: number,
  request: TrafficReplayRequest
): Promise<ApiResponse<TrafficReplayResponse>> {
  const res = await api.post(`/api/traffic/${id}/replay`, request)
  return res.data
}

// Intercept Rules API

export async function getInterceptSettings(): Promise<
  ApiResponse<InterceptSettings>
> {
  const res = await api.get('/api/intercept/settings')
  return res.data
}

export async function updateInterceptSettings(
  settings: InterceptSettings
): Promise<ApiResponse<InterceptSettings>> {
  const res = await api.put('/api/intercept/settings', settings)
  return res.data
}

export async function getLiveInterceptSettings(): Promise<
  ApiResponse<LiveInterceptSettings>
> {
  const res = await api.get('/api/intercept/live/settings')
  return res.data
}

export async function updateLiveInterceptSettings(
  settings: LiveInterceptSettings
): Promise<ApiResponse<LiveInterceptSettings>> {
  const res = await api.put('/api/intercept/live/settings', settings)
  return res.data
}

export async function getLiveInterceptEvents(): Promise<
  ApiResponse<LiveInterceptEvent[]>
> {
  const res = await api.get('/api/intercept/live/events')
  return res.data
}

export async function decideLiveInterceptEvent(
  id: string,
  payload: LiveInterceptDecisionPayload
): Promise<ApiResponse<void>> {
  const res = await api.post(
    `/api/intercept/live/events/${id}/decision`,
    payload
  )
  return res.data
}

export async function getInterceptRules(
  page = 1,
  pageSize = 20
): Promise<InterceptRulesResponse> {
  const res = await api.get(`/api/intercept/?p=${page}&page_size=${pageSize}`)
  return res.data
}

export async function getInterceptRule(
  id: number
): Promise<ApiResponse<InterceptRule>> {
  const res = await api.get(`/api/intercept/${id}`)
  return res.data
}

export async function createInterceptRule(
  rule: Partial<InterceptRule>
): Promise<ApiResponse<InterceptRule>> {
  const res = await api.post('/api/intercept/', rule)
  return res.data
}

export async function updateInterceptRule(
  id: number,
  rule: Partial<InterceptRule>
): Promise<ApiResponse<InterceptRule>> {
  const res = await api.put(`/api/intercept/${id}`, rule)
  return res.data
}

export async function deleteInterceptRule(
  id: number
): Promise<ApiResponse<void>> {
  const res = await api.delete(`/api/intercept/${id}`)
  return res.data
}
