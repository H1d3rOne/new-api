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
import { Edit, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { Switch } from '@/components/ui/switch'
import { DataTableColumnHeader } from '@/components/data-table'
import { StatusBadge } from '@/components/status-badge'
import type { InterceptRule } from '../types'

interface UseInterceptRuleColumnsParams {
  onEdit: (rule: InterceptRule) => void
  onDelete: (rule: InterceptRule) => void
  onToggleEnabled: (rule: InterceptRule, enabled: boolean) => void
  isTogglingEnabled?: (rule: InterceptRule) => boolean
}

export function useInterceptRuleColumns(
  params: UseInterceptRuleColumnsParams
): ColumnDef<InterceptRule>[] {
  const { t } = useTranslation()

  return [
    {
      accessorKey: 'name',
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title={t('Name')} />
      ),
      cell: ({ row }) => (
        <div className='max-w-[200px] truncate font-medium'>
          {row.original.name}
        </div>
      ),
      meta: { label: t('Name') },
    },
    {
      accessorKey: 'enabled',
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title={t('Status')} />
      ),
      cell: ({ row }) => (
        <StatusBadge
          label={row.original.enabled ? t('Enabled') : t('Disabled')}
          variant={row.original.enabled ? 'success' : 'neutral'}
          size='sm'
          copyable={false}
        />
      ),
      meta: { label: t('Status') },
    },
    {
      accessorKey: 'priority',
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title={t('Priority')} />
      ),
      cell: ({ row }) => (
        <span className='font-mono text-xs'>{row.original.priority}</span>
      ),
      meta: { label: t('Priority') },
    },
    {
      accessorKey: 'username',
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title={t('User')} />
      ),
      cell: ({ row }) => {
        const username = row.original.username
        const userId = row.original.user_id
        return (
          <div className='max-w-[160px] truncate text-xs'>
            {username || userId ? (
              <>
                {username || t('User {{id}}', { id: userId })}
                {userId ? (
                  <span className='text-muted-foreground ml-1 font-mono'>
                    #{userId}
                  </span>
                ) : null}
              </>
            ) : (
              t('All')
            )}
          </div>
        )
      },
      meta: { label: t('User') },
    },
    {
      accessorKey: 'method',
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title={t('Method')} />
      ),
      cell: ({ row }) => (
        <span className='font-mono text-xs'>
          {row.original.method || t('All')}
        </span>
      ),
      meta: { label: t('Method') },
    },
    {
      accessorKey: 'path_pattern',
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title={t('Path Pattern')} />
      ),
      cell: ({ row }) => (
        <div
          className='max-w-[200px] truncate font-mono text-xs'
          title={row.original.path_pattern}
        >
          {row.original.path_pattern || '*'}
        </div>
      ),
      meta: { label: t('Path Pattern') },
    },
    {
      id: 'phases',
      header: t('Phases'),
      cell: ({ row }) => (
        <div className='flex gap-1'>
          {row.original.intercept_request && (
            <StatusBadge
              label={t('Request')}
              variant='warning'
              size='sm'
              copyable={false}
            />
          )}
          {row.original.intercept_response && (
            <StatusBadge
              label={t('Response')}
              variant='info'
              size='sm'
              copyable={false}
            />
          )}
          {row.original.block_enabled && (
            <StatusBadge
              label={t('Block')}
              variant='danger'
              size='sm'
              copyable={false}
            />
          )}
        </div>
      ),
      meta: { label: t('Phases') },
    },
    {
      id: 'actions',
      header: t('Actions'),
      cell: ({ row }) => (
        <div className='flex items-center gap-2'>
          <Switch
            size='sm'
            checked={row.original.enabled}
            disabled={params.isTogglingEnabled?.(row.original)}
            aria-label={t('Enabled')}
            onCheckedChange={(checked) =>
              params.onToggleEnabled(row.original, Boolean(checked))
            }
          />
          <Button
            type='button'
            variant='ghost'
            size='sm'
            onClick={() => params.onEdit(row.original)}
          >
            <Edit className='size-4' />
          </Button>
          <Button
            type='button'
            variant='ghost'
            size='sm'
            onClick={() => params.onDelete(row.original)}
          >
            <Trash2 className='text-destructive size-4' />
          </Button>
        </div>
      ),
      meta: { label: t('Actions') },
    },
  ]
}
