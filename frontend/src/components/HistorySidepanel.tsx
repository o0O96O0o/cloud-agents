import { PenLine } from 'lucide-react'
import { ScrollArea } from '@/components/ui/scroll-area'
import { cn } from '@/lib/utils'
import type { TaskSummary } from '@/api/client'

interface Props {
  tasks: TaskSummary[]
  activeTaskId: string | null
  onSelectTask: (id: string) => void
  onNewChat: () => void
}

function formatDate(dateStr: string): string {
  const date = new Date(dateStr)
  const now = new Date()
  if (date.toDateString() === now.toDateString()) {
    return date.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
  }
  const yesterday = new Date(now)
  yesterday.setDate(now.getDate() - 1)
  if (date.toDateString() === yesterday.toDateString()) {
    return 'Yesterday'
  }
  return date.toLocaleDateString([], { month: 'short', day: 'numeric' })
}

export function HistorySidepanel({ tasks, activeTaskId, onSelectTask, onNewChat }: Props) {
  return (
    <div className="w-60 flex-shrink-0 flex flex-col border-r border-neutral-200 h-full bg-neutral-50">
      <div className="px-3 py-3 flex items-center justify-between border-b border-neutral-200">
        <span className="text-xs font-semibold text-neutral-500 uppercase tracking-wide">History</span>
        <button
          onClick={onNewChat}
          className="p-1.5 rounded hover:bg-neutral-200 text-neutral-500 hover:text-neutral-700 transition-colors"
          title="New chat"
        >
          <PenLine size={14} />
        </button>
      </div>

      <ScrollArea className="flex-1">
        <div className="py-1">
          {tasks.map(task => (
            <button
              key={task.id}
              onClick={() => onSelectTask(task.id)}
              className={cn(
                'w-full text-left px-3 py-2.5 hover:bg-neutral-100 transition-colors',
                activeTaskId === task.id && 'bg-neutral-100'
              )}
            >
              <div className={cn(
                'truncate text-sm text-neutral-700',
                activeTaskId === task.id && 'font-medium text-neutral-900'
              )}>
                {task.title || 'Untitled'}
              </div>
              <div className="text-xs text-neutral-400 mt-0.5">
                {formatDate(task.updated_at)}
              </div>
            </button>
          ))}

          {tasks.length === 0 && (
            <p className="px-3 py-4 text-xs text-neutral-400">No previous sessions</p>
          )}
        </div>
      </ScrollArea>
    </div>
  )
}
