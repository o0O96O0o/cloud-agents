import type { SessionEntry, UserEntry, AssistantEntry } from '@/types/session'
import type { Message, ToolUseBlock } from '@/types'

function userEntryToMessage(entry: UserEntry): Message {
  const content = entry.message.content
  let text = ''
  if (typeof content === 'string') {
    text = content
  } else if (Array.isArray(content)) {
    text = content
      .filter((b): b is { type: 'text'; text: string } => b.type === 'text')
      .map(b => b.text)
      .join('')
  }
  return { id: entry.uuid, role: 'user', text, status: 'done' }
}

function assistantEntryToMessage(entry: AssistantEntry): Message {
  const content = entry.message.content
  const text = content
    .filter((b): b is { type: 'text'; text: string } => b.type === 'text')
    .map(b => b.text)
    .join('')
  const toolUseBlocks: ToolUseBlock[] = content
    .filter((b): b is { type: 'tool_use'; id: string; name: string; input: unknown } => b.type === 'tool_use')
    .map(b => ({ id: b.id, name: b.name, input: b.input as Record<string, unknown> }))
  return { id: entry.uuid, role: 'assistant', text, status: 'done', toolUseBlocks }
}

/** Convert raw session entries from the history API into renderable Message objects. */
export function buildMessages(entries: SessionEntry[]): Message[] {
  return entries
    .filter((e): e is UserEntry | AssistantEntry => e.type === 'user' || e.type === 'assistant')
    .map(e => e.type === 'user'
      ? userEntryToMessage(e)
      : assistantEntryToMessage(e as AssistantEntry)
    )
}
