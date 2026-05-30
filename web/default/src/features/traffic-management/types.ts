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

export interface TrafficLog {
  id: number
  created_at: number
  user_id: number
  username: string
  token_id: number
  token_name: string
  model_name: string
  channel: number
  channel_name?: string
  group: string
  ip: string
  request_id?: string
  upstream_request_id?: string
  method: string
  path: string
  request_url: string
  status_code: number
  is_stream: boolean
  request_content_type: string
  response_content_type: string
  request_headers: string
  response_headers: string
  request_body: string
  response_body: string
  request_body_size: number
  response_body_size: number
  request_body_truncated: boolean
  response_body_truncated: boolean
  request_body_truncated_bytes: number
  response_body_truncated_bytes: number
  duration_ms: number
  user_agent: string
}

export interface TrafficLogParams {
  p?: number
  page_size?: number
  user_id?: number
  username?: string
  token_name?: string
  model_name?: string
  start_timestamp?: number
  end_timestamp?: number
  channel?: number
  group?: string
  status_code?: number
  request_id?: string
  upstream_request_id?: string
}

export interface ApiResponse<T = unknown> {
  success: boolean
  message?: string
  data?: T
}

export interface TrafficLogsResponse {
  success: boolean
  message?: string
  data?: {
    items: TrafficLog[]
    total: number
    page: number
    page_size: number
  }
}

export interface HeaderOp {
  op: 'set' | 'remove'
  key: string
  value?: string
}

export interface MessageContentRewrite {
  role: string
  mode: 'latest' | 'first' | 'all' | 'index'
  index?: number
  content: string
}

export interface MessageContentMatch {
  role: string
  mode: 'latest' | 'first' | 'all' | 'index'
  index?: number
  content: string
  content_op?: 'and' | 'or'
}

export interface InterceptRule {
  id: number
  name: string
  description: string
  priority: number
  enabled: boolean
  match_limit: number
  match_count: number
  user_id: number
  username: string

  // Request matching conditions
  path_pattern: string
  method: string
  model_pattern: string
  request_content_match: string
  request_message_matches: string
  request_message_match_op: 'and' | 'or'
  condition_expr: string

  // Response matching conditions
  response_user_id: number
  response_username: string
  response_path_pattern: string
  response_method: string
  response_model_pattern: string
  response_content_match: string
  response_tool_calls_match: string
  response_match_op: 'and' | 'or'
  response_condition_expr: string

  // Intercept phases
  request_match_enabled: boolean
  response_match_enabled: boolean
  intercept_request: boolean
  intercept_response: boolean
  script_enabled: boolean

  // Request actions
  block_enabled: boolean
  block_status_code: number
  block_content_type: string
  block_body: string
  request_header_ops: string // JSON string of HeaderOp[]
  request_body_rewrite: string
  request_message_rewrites: string // JSON string of MessageContentRewrite[]
  request_url_rewrite: string
  request_script: string

  // Response actions
  response_header_ops: string // JSON string of HeaderOp[]
  response_body_rewrite: string
  response_content_rewrite: string
  response_tool_calls_rewrite: string
  response_status_rewrite: string
  response_url_rewrite: string
  response_script: string
  script: string

  created_at: number
  updated_at: number
}

export interface InterceptSettings {
  enabled: boolean
}

export interface LiveInterceptSettings {
  enabled: boolean
  user_ids: number[]
  usernames: string[]
  intercept_request: boolean
  intercept_response: boolean
  timeout_seconds: number
}

export interface LiveInterceptEvent {
  id: string
  phase: 'request' | 'response'
  created_at: number
  user_id: number
  username: string
  method: string
  path: string
  request_url: string
  model_name: string
  content_type: string
  headers: Record<string, string>
  body: string
  status_code: number
  response_headers: Record<string, string>
  response_body: string
  response_content_type: string
}

export interface LiveInterceptDecisionPayload {
  decision: 'accept' | 'block'
  headers?: Record<string, string>
  body?: string
  headers_modified?: boolean
  body_modified?: boolean
}

export interface TrafficReplayRequest {
  method: string
  request_url: string
  headers: Record<string, string>
  body: string
  content_type: string
}

export interface TrafficReplayResponse {
  status_code: number
  content_type: string
  headers: Record<string, string>
  body: string
  body_size: number
  body_truncated: boolean
  truncated_bytes: number
  duration_ms: number
}

export interface InterceptRulesResponse {
  success: boolean
  message?: string
  data?: {
    items: InterceptRule[]
    total: number
    page: number
    page_size: number
  }
}
