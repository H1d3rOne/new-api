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
import type { ColumnDef } from '@tanstack/react-table'
import { Eye, MoreHorizontal, PencilLine, Repeat2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { formatTimestampToDate } from '@/lib/format'
import { Button } from '@/components/ui/button'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { DataTableColumnHeader } from '@/components/data-table'
import { StatusBadge } from '@/components/status-badge'
import type { TrafficLog } from '../types'
import { formatBytes, getStatusVariant } from './traffic-format'

interface UseTrafficColumnsParams {
  onView: (log: TrafficLog) => void
  onRewrite: (log: TrafficLog) => void
  onReplay: (log: TrafficLog) => void
}

export function useTrafficColumns(
  params: UseTrafficColumnsParams
): ColumnDef<TrafficLog>[] {
  const { t } = useTranslation()

  return [
    {
      accessorKey: 'created_at',
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title={t('Time')} />
      ),
      cell: ({ row }) => (
        <div className='flex flex-col gap-1'>
          <span className='font-mono text-xs tabular-nums'>
            {formatTimestampToDate(row.original.created_at)}
          </span>
          <StatusBadge
            label={row.original.method || 'HTTP'}
            variant='neutral'
            size='sm'
            copyable={false}
          />
        </div>
      ),
      meta: { label: t('Time') },
    },
    {
      accessorKey: 'status_code',
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title={t('Status')} />
      ),
      cell: ({ row }) => (
        <StatusBadge
          label={String(row.original.status_code || '-')}
          variant={getStatusVariant(row.original.status_code)}
          size='sm'
          copyable={false}
        />
      ),
      meta: { label: t('Status') },
    },
    {
      accessorKey: 'model_name',
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title={t('Model')} />
      ),
      cell: ({ row }) => (
        <div
          className='max-w-[220px] truncate font-medium'
          title={row.original.model_name}
        >
          {row.original.model_name || '-'}
        </div>
      ),
      meta: { label: t('Model') },
    },
    {
      accessorKey: 'username',
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title={t('User')} />
      ),
      cell: ({ row }) => (
        <div className='flex min-w-0 flex-col'>
          <span className='truncate font-medium'>
            {row.original.username || '-'}
          </span>
          <span className='text-muted-foreground font-mono text-xs'>
            #{row.original.user_id}
          </span>
        </div>
      ),
      meta: { label: t('User') },
    },
    {
      accessorKey: 'token_name',
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title={t('Token')} />
      ),
      cell: ({ row }) => (
        <div className='max-w-[160px] truncate' title={row.original.token_name}>
          {row.original.token_name || '-'}
        </div>
      ),
      meta: { label: t('Token') },
    },
    {
      accessorKey: 'channel',
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title={t('Channel')} />
      ),
      cell: ({ row }) => (
        <StatusBadge
          label={row.original.channel_name || `#${row.original.channel || '-'}`}
          autoColor={String(row.original.channel)}
          copyText={String(row.original.channel)}
          size='sm'
          showDot={false}
          className='max-w-[180px]'
        />
      ),
      meta: { label: t('Channel') },
    },
    {
      accessorKey: 'path',
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title={t('Path')} />
      ),
      cell: ({ row }) => (
        <div
          className='max-w-[260px] truncate font-mono text-xs'
          title={row.original.path}
        >
          {row.original.path || '-'}
        </div>
      ),
      meta: { label: t('Path') },
    },
    {
      accessorKey: 'duration_ms',
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title={t('Duration')} />
      ),
      cell: ({ row }) => (
        <span className='font-mono text-xs tabular-nums'>
          {row.original.duration_ms}ms
        </span>
      ),
      meta: { label: t('Duration') },
    },
    {
      id: 'body_size',
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title={t('Body Size')} />
      ),
      cell: ({ row }) => (
        <div className='flex flex-col gap-0.5 font-mono text-xs'>
          <span>
            {t('Request')}: {formatBytes(row.original.request_body_size)}
          </span>
          <span>
            {t('Response')}: {formatBytes(row.original.response_body_size)}
          </span>
        </div>
      ),
      meta: { label: t('Body Size') },
    },
    {
      id: 'actions',
      header: t('Actions'),
      cell: ({ row }) => (
        <DropdownMenu>
          <DropdownMenuTrigger
            render={
              <Button
                type='button'
                variant='ghost'
                size='icon-sm'
                aria-label={t('Actions')}
              />
            }
          >
            <MoreHorizontal className='size-4' />
          </DropdownMenuTrigger>
          <DropdownMenuContent align='end' className='w-40'>
            <DropdownMenuItem onClick={() => params.onView(row.original)}>
              <Eye className='size-4' />
              {t('View')}
            </DropdownMenuItem>
            <DropdownMenuItem onClick={() => params.onRewrite(row.original)}>
              <PencilLine className='size-4' />
              {t('Rewrite')}
            </DropdownMenuItem>
            <DropdownMenuItem onClick={() => params.onReplay(row.original)}>
              <Repeat2 className='size-4' />
              {t('Replay')}
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      ),
      meta: { label: t('Actions') },
    },
  ]
}
