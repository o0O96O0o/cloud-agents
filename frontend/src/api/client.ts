const BASE = import.meta.env.VITE_API_BASE ?? ''

export async function createTask(username: string): Promise<string> {
  const res = await fetch(`${BASE}/api/tasks`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ username }) })
  if (!res.ok) throw new Error('Failed to create task')
  const { id } = await res.json() as { id: string }
  return id
}

export async function sendMessage(taskId: string, prompt: string): Promise<Response> {
  return fetch(`${BASE}/api/tasks/${taskId}/messages`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ prompt }),
  })
}

export async function deleteTask(taskId: string): Promise<void> {
  await fetch(`${BASE}/api/tasks/${taskId}`, { method: 'DELETE' })
}
