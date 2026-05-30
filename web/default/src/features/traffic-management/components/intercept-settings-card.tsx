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
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { ShieldCheck, ShieldOff } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import {
  Card,
  CardAction,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import { Switch } from '@/components/ui/switch'
import { StatusBadge } from '@/components/status-badge'
import { getInterceptSettings, updateInterceptSettings } from '../api'

export function InterceptSettingsCard() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()

  const { data, isFetching } = useQuery({
    queryKey: ['intercept-settings'],
    queryFn: async () => {
      const result = await getInterceptSettings()
      if (!result.success || !result.data) {
        throw new Error(result.message || t('Failed to load intercept settings'))
      }
      return result.data
    },
  })

  const mutation = useMutation({
    mutationFn: updateInterceptSettings,
    onSuccess: (result) => {
      if (!result.success) {
        toast.error(result.message || t('Failed to update intercept settings'))
        return
      }
      toast.success(t('Intercept settings updated'))
      queryClient.invalidateQueries({ queryKey: ['intercept-settings'] })
      queryClient.invalidateQueries({ queryKey: ['intercept-rules'] })
    },
    onError: () => {
      toast.error(t('Failed to update intercept settings'))
    },
  })

  const enabled = Boolean(data?.enabled)
  const busy = isFetching || mutation.isPending

  return (
    <Card className='border-primary/10 bg-primary/5'>
      <CardHeader>
        <CardTitle className='flex items-center gap-2'>
          {enabled ? (
            <ShieldCheck className='text-success size-4' />
          ) : (
            <ShieldOff className='text-muted-foreground size-4' />
          )}
          {t('Traffic Interception')}
          <StatusBadge
            label={enabled ? t('Enabled') : t('Disabled')}
            variant={enabled ? 'success' : 'neutral'}
            size='sm'
            copyable={false}
          />
        </CardTitle>
        <CardDescription>
          {t(
            'Enable interception to apply request and response rewrite rules during model relay.'
          )}
        </CardDescription>
        <CardAction>
          <Switch
            checked={enabled}
            disabled={busy}
            aria-label={t('Traffic Interception')}
            onCheckedChange={(checked) =>
              mutation.mutate({ enabled: Boolean(checked) })
            }
          />
        </CardAction>
      </CardHeader>
      <CardContent>
        <div className='text-muted-foreground text-xs leading-relaxed'>
          {t(
            'When disabled, rules remain saved but are not evaluated. When enabled, matching rules can block requests, rewrite upstream requests, and rewrite non-streaming responses.'
          )}
        </div>
      </CardContent>
    </Card>
  )
}
