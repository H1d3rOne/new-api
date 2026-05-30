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
import {
  flexRender,
  getCoreRowModel,
  getPaginationRowModel,
  useReactTable,
  type ColumnDef,
  type PaginationState,
} from '@tanstack/react-table'
import { useMediaQuery } from '@/hooks'
import { Plus } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import { Button } from '@/components/ui/button'
import { TableCell, TableRow } from '@/components/ui/table'
import { DataTablePage } from '@/components/data-table'
import {
  deleteInterceptRule,
  getInterceptRules,
  updateInterceptRule,
} from '../api'
import type { InterceptRule } from '../types'
import { useInterceptRuleColumns } from './intercept-rule-columns'
import { InterceptRuleDialog } from './intercept-rule-dialog'
import { InterceptSettingsCard } from './intercept-settings-card'

export function InterceptRulesPage() {
  const { t } = useTranslation()
  const isMobile = useMediaQuery('(max-width: 640px)')
  const queryClient = useQueryClient()
  const [selectedRule, setSelectedRule] = useState<InterceptRule | null>(null)
  const [dialogOpen, setDialogOpen] = useState(false)
  const [deleteTarget, setDeleteTarget] = useState<InterceptRule | null>(null)
  const [deleting, setDeleting] = useState(false)
  const [togglingRuleIds, setTogglingRuleIds] = useState<Set<number>>(
    () => new Set()
  )
  const [pagination, setPagination] = useState<PaginationState>({
    pageIndex: 0,
    pageSize: isMobile ? 20 : 100,
  })

  const { data, isLoading, isFetching } = useQuery({
    queryKey: [
      'intercept-rules',
      pagination.pageIndex + 1,
      pagination.pageSize,
    ],
    queryFn: async () => {
      const result = await getInterceptRules(
        pagination.pageIndex + 1,
        pagination.pageSize
      )
      if (!result.success) {
        toast.error(result.message || t('Failed to load intercept rules'))
        return { items: [], total: 0, page: 1, page_size: 20 }
      }
      return result.data || { items: [], total: 0, page: 1, page_size: 20 }
    },
    placeholderData: (previousData) => previousData,
  })

  const rules = data?.items || []
  const isLoadingData = isLoading || (isFetching && !data)

  const updateCachedRuleEnabled = (
    ruleId: number,
    enabled: boolean,
    matchCount = 0
  ) => {
    queryClient.setQueriesData<{ items: InterceptRule[] }>(
      { queryKey: ['intercept-rules'] },
      (old) => {
        if (!old?.items) return old
        return {
          ...old,
          items: old.items.map((item) =>
            item.id === ruleId
              ? { ...item, enabled, match_count: matchCount }
              : item
          ),
        }
      }
    )
  }

  const handleToggleEnabled = async (rule: InterceptRule, enabled: boolean) => {
    if (togglingRuleIds.has(rule.id)) return
    setTogglingRuleIds((prev) => new Set(prev).add(rule.id))
    updateCachedRuleEnabled(rule.id, enabled)
    try {
      const result = await updateInterceptRule(rule.id, { enabled })
      if (result.success) {
        toast.success(t('Rule updated'))
        queryClient.invalidateQueries({ queryKey: ['intercept-rules'] })
      } else {
        updateCachedRuleEnabled(rule.id, rule.enabled, rule.match_count)
        toast.error(result.message || t('Failed to save rule'))
      }
    } catch {
      updateCachedRuleEnabled(rule.id, rule.enabled, rule.match_count)
      toast.error(t('Failed to save rule'))
    } finally {
      setTogglingRuleIds((prev) => {
        const next = new Set(prev)
        next.delete(rule.id)
        return next
      })
    }
  }

  const columns = useInterceptRuleColumns({
    onEdit: (rule) => {
      setSelectedRule(rule)
      setDialogOpen(true)
    },
    onDelete: (rule) => {
      setDeleteTarget(rule)
    },
    onToggleEnabled: handleToggleEnabled,
    isTogglingEnabled: (rule) => togglingRuleIds.has(rule.id),
  })

  const table = useReactTable({
    data: rules,
    columns,
    state: { pagination },
    enableRowSelection: false,
    onPaginationChange: setPagination,
    getCoreRowModel: getCoreRowModel(),
    getPaginationRowModel: getPaginationRowModel(),
    manualPagination: true,
    pageCount: Math.ceil((data?.total || 0) / pagination.pageSize),
  })

  const pageCount = table.getPageCount()
  useEffect(() => {
    if (pageCount > 0 && pagination.pageIndex + 1 > pageCount) {
      setPagination((prev) => ({ ...prev, pageIndex: 0 }))
    }
  }, [pageCount, pagination.pageIndex])

  const handleCreate = () => {
    setSelectedRule(null)
    setDialogOpen(true)
  }

  const handleSaved = () => {
    queryClient.invalidateQueries({ queryKey: ['intercept-rules'] })
  }

  const handleDelete = async () => {
    if (!deleteTarget) return
    setDeleting(true)
    try {
      const result = await deleteInterceptRule(deleteTarget.id)
      if (result.success) {
        toast.success(t('Rule deleted'))
        queryClient.invalidateQueries({ queryKey: ['intercept-rules'] })
      } else {
        toast.error(result.message || t('Failed to delete rule'))
      }
    } catch {
      toast.error(t('Failed to delete rule'))
    } finally {
      setDeleting(false)
      setDeleteTarget(null)
    }
  }

  return (
    <>
      <div className='mb-4 space-y-4'>
        <InterceptSettingsCard />
        <div className='flex items-center justify-end'>
          <Button type='button' onClick={handleCreate}>
            <Plus className='mr-2 size-4' />
            {t('Create Rule')}
          </Button>
        </div>
      </div>

      <DataTablePage
        table={table}
        columns={columns as ColumnDef<InterceptRule, unknown>[]}
        isLoading={isLoadingData}
        isFetching={isFetching}
        emptyTitle={t('No Intercept Rules')}
        emptyDescription={t(
          'Create rules to intercept and rewrite requests and responses.'
        )}
        skeletonKeyPrefix='intercept-rule-skeleton'
        tableClassName='overflow-x-auto'
        tableHeaderClassName='bg-muted/30 sticky top-0 z-10'
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

      <InterceptRuleDialog
        rule={selectedRule}
        open={dialogOpen}
        onOpenChange={setDialogOpen}
        onSaved={handleSaved}
      />

      <AlertDialog
        open={Boolean(deleteTarget)}
        onOpenChange={() => setDeleteTarget(null)}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t('Delete Rule')}</AlertDialogTitle>
            <AlertDialogDescription>
              {t('Are you sure you want to delete rule "{{name}}"?', {
                name: deleteTarget?.name,
              })}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t('Cancel')}</AlertDialogCancel>
            <AlertDialogAction onClick={handleDelete} disabled={deleting}>
              {deleting ? t('Deleting...') : t('Delete')}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  )
}
