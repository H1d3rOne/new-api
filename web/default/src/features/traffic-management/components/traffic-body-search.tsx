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
import {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  type RefObject,
} from 'react'
import { ChevronDown, ChevronUp, Search, X } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'

interface BodySearchMatch {
  start: number
  end: number
}

interface TrafficBodySearchProps {
  value: string
  textareaRef: RefObject<HTMLTextAreaElement | null>
  disabled?: boolean
  className?: string
}

function findMatches(value: string, query: string): BodySearchMatch[] {
  const needle = query.trim().toLowerCase()
  if (!needle) return []

  const haystack = value.toLowerCase()
  const matches: BodySearchMatch[] = []
  let cursor = 0
  while (cursor < haystack.length) {
    const index = haystack.indexOf(needle, cursor)
    if (index < 0) break
    matches.push({ start: index, end: index + needle.length })
    cursor = index + Math.max(needle.length, 1)
  }
  return matches
}

export function TrafficBodySearch(props: TrafficBodySearchProps) {
  const { t } = useTranslation()
  const composingRef = useRef(false)
  const [query, setQuery] = useState('')
  const [searchQuery, setSearchQuery] = useState('')
  const [activeIndex, setActiveIndex] = useState(-1)
  const matches = useMemo(
    () => findMatches(props.value, searchQuery),
    [props.value, searchQuery]
  )

  const selectMatch = useCallback(
    (match: BodySearchMatch) => {
      if (props.disabled) return
      const textarea = props.textareaRef.current
      if (!textarea) return

      requestAnimationFrame(() => {
        textarea.focus({ preventScroll: true })
        textarea.setSelectionRange(match.start, match.end)

        const beforeMatch = props.value.slice(0, match.start)
        const lineIndex = beforeMatch.split(/\r?\n/).length - 1
        const totalLines = Math.max(props.value.split(/\r?\n/).length, 1)
        const maxScrollTop = Math.max(
          textarea.scrollHeight - textarea.clientHeight,
          0
        )
        const lineRatio = totalLines > 1 ? lineIndex / (totalLines - 1) : 0
        textarea.scrollTop = Math.max(
          0,
          maxScrollTop * lineRatio - textarea.clientHeight / 3
        )
      })
    },
    [props.disabled, props.textareaRef, props.value]
  )

  useEffect(() => {
    setActiveIndex((current) => {
      if (matches.length === 0) return -1
      if (current < 0) return 0
      return Math.min(current, matches.length - 1)
    })
  }, [matches.length])

  const searchValue = (value: string) => {
    setSearchQuery(value)
    const nextMatches = findMatches(props.value, value)
    setActiveIndex(nextMatches.length > 0 ? 0 : -1)
    if (nextMatches[0]) {
      selectMatch(nextMatches[0])
    }
  }

  const handleQueryChange = (value: string) => {
    setQuery(value)
    if (!composingRef.current) {
      searchValue(value)
    }
  }

  const navigate = (direction: 1 | -1) => {
    if (matches.length === 0) return
    const safeCurrent = activeIndex >= 0 ? activeIndex : 0
    const nextIndex = (safeCurrent + direction + matches.length) % matches.length
    setActiveIndex(nextIndex)
    selectMatch(matches[nextIndex])
  }

  const clearSearch = () => {
    setQuery('')
    setSearchQuery('')
    setActiveIndex(-1)
    props.textareaRef.current?.setSelectionRange(0, 0)
  }

  const counter =
    searchQuery.trim() === ''
      ? ''
      : matches.length > 0
        ? `${activeIndex + 1}/${matches.length}`
        : '0/0'

  return (
    <div
      className={cn('flex min-w-0 flex-wrap items-center gap-1', props.className)}
    >
      <div className='relative min-w-[180px] flex-1 sm:max-w-[260px]'>
        <Search className='text-muted-foreground pointer-events-none absolute top-1/2 left-2 size-3.5 -translate-y-1/2' />
        <Input
          value={query}
          onChange={(event) => handleQueryChange(event.target.value)}
          onKeyDown={(event) => {
            if (composingRef.current) return
            if (event.key === 'ArrowUp') {
              event.preventDefault()
              navigate(-1)
            }
            if (event.key === 'ArrowDown') {
              event.preventDefault()
              navigate(1)
            }
          }}
          onCompositionStart={() => {
            composingRef.current = true
          }}
          onCompositionEnd={(event) => {
            composingRef.current = false
            setQuery(event.currentTarget.value)
            searchValue(event.currentTarget.value)
          }}
          disabled={props.disabled}
          placeholder={t('Search')}
          aria-label={t('Search')}
          className='h-7 pr-7 pl-7 font-mono text-xs'
        />
        {query && (
          <button
            type='button'
            className='text-muted-foreground hover:text-foreground absolute top-1/2 right-1.5 -translate-y-1/2'
            onClick={clearSearch}
            aria-label={t('Clear')}
            disabled={props.disabled}
          >
            <X className='size-3.5' />
          </button>
        )}
      </div>
      <span className='text-muted-foreground w-11 text-center font-mono text-xs tabular-nums'>
        {counter}
      </span>
      <Button
        type='button'
        variant='ghost'
        size='icon-sm'
        onClick={() => navigate(-1)}
        disabled={props.disabled || matches.length === 0}
        aria-label={t('Previous')}
        title={t('Previous')}
      >
        <ChevronUp className='size-4' />
      </Button>
      <Button
        type='button'
        variant='ghost'
        size='icon-sm'
        onClick={() => navigate(1)}
        disabled={props.disabled || matches.length === 0}
        aria-label={t('Next')}
        title={t('Next')}
      >
        <ChevronDown className='size-4' />
      </Button>
    </div>
  )
}
