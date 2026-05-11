import { useEffect, useRef, useState } from 'react'
import { ScrollArea } from '@/components/ui/scroll-area'
import { ChatMessage } from '@/components/ChatMessage'
import { ChatInput } from '@/components/ChatInput'
import { StatusBadge } from '@/components/StatusBadge'
import { useChat } from '@/hooks/useChat'

export function ChatPage() {
  const [username, setUsername] = useState<string | null>(null)
  const [inputValue, setInputValue] = useState('')
  const { messages, sandboxState, sending, sendMessage, approvePermission, answerQuestion } = useChat(username ?? '')
  const bottomRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    bottomRef.current?.scrollIntoView({ behavior: 'smooth' })
  }, [messages])

  if (!username) {
    return (
      <div className="flex flex-col items-center justify-center h-[100dvh]">
        <div className="flex flex-col gap-3 w-full max-w-sm px-4">
          <h1 className="text-lg font-semibold text-center">Welcome to Lucas</h1>
          <p className="text-sm text-neutral-500 text-center">What's your name?</p>
          <input
            className="border border-neutral-200 rounded-md px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-neutral-300"
            placeholder="Username"
            value={inputValue}
            onChange={e => setInputValue(e.target.value)}
            onKeyDown={e => { if (e.key === 'Enter' && inputValue.trim()) setUsername(inputValue.trim()) }}
            autoFocus
          />
          <button
            className="bg-neutral-900 text-white rounded-md px-4 py-2 text-sm font-medium disabled:opacity-50"
            disabled={!inputValue.trim()}
            onClick={() => setUsername(inputValue.trim())}
          >
            Continue
          </button>
        </div>
      </div>
    )
  }

  return (
    <div className="flex flex-col h-[100dvh] max-w-3xl mx-auto">
      <header className="flex items-center justify-between px-4 py-3 border-b border-neutral-200">
        <span className="font-semibold text-sm">Lucas</span>
        <StatusBadge state={sandboxState} />
      </header>

      <ScrollArea className="flex-1">
        <div className="p-4 space-y-4">
          {messages.length === 0 ? (
            <div className="flex items-center justify-center h-full min-h-[60dvh]">
              <p className="text-neutral-400 text-sm">What can I help you with?</p>
            </div>
          ) : (
            messages.map(msg => (
                <ChatMessage
                  key={msg.id}
                  message={msg}
                  onApprovePermission={msg.status === 'requesting' ? approvePermission : undefined}
                  onAnswerQuestion={msg.status === 'asking' ? answerQuestion : undefined}
                />
              ))
          )}
          <div ref={bottomRef} />
        </div>
      </ScrollArea>

      <ChatInput onSend={sendMessage} disabled={sending} />
    </div>
  )
}
