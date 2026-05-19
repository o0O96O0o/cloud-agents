import { useEffect, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { getRuntimeConfig, loginWithPassword, register, type RuntimeConfig } from '@/api/client'
import { setToken } from '@/lib/auth'

const BASE = import.meta.env.VITE_API_BASE ?? ''

type Mode = 'login' | 'register'

export function LoginPage() {
  const [config, setConfig] = useState<RuntimeConfig | null>(null)
  const [mode, setMode] = useState<Mode>('login')
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [email, setEmail] = useState('')
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(false)
  const navigate = useNavigate()

  useEffect(() => {
    getRuntimeConfig()
      .then(setConfig)
      .catch(() => setConfig({ loginMode: 'none', passwordLogin: false, allowRegister: false }))
  }, [])

  const switchMode = (m: Mode) => {
    setMode(m)
    setUsername('')
    setPassword('')
    setEmail('')
    setError('')
  }

  const handleSubmit = async () => {
    if (!username.trim() || !password) return
    setLoading(true)
    setError('')
    try {
      const token = mode === 'register'
        ? await register(username.trim(), password, email.trim() || undefined)
        : await loginWithPassword(username.trim(), password)
      setToken(token)
      navigate('/', { replace: true })
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Something went wrong')
    } finally {
      setLoading(false)
    }
  }

  return (
    <div className="flex flex-col items-center justify-center h-[100dvh]">
      <div className="flex flex-col gap-3 w-full max-w-sm px-4">
        <h1 className="text-lg font-semibold text-center">Welcome to Cloud Managed Agents</h1>

        {config?.loginMode.includes('sso') && (
          <>
            <p className="text-sm text-neutral-500 text-center">Sign in to continue</p>
            <button
              className="bg-neutral-900 text-white rounded-md px-4 py-2 text-sm font-medium hover:bg-neutral-700 transition-colors"
              onClick={() => { window.location.href = `${BASE}/api/auth/sso/login` }}
            >
              Login with SSO
            </button>
          </>
        )}

        {config?.loginMode.includes('oidc') && (
          <button
            className="bg-neutral-900 text-white rounded-md px-4 py-2 text-sm font-medium hover:bg-neutral-700 transition-colors"
            onClick={() => { window.location.href = `${BASE}/api/auth/oidc/login` }}
          >
            Login with OIDC
          </button>
        )}

        {config?.passwordLogin && (
          <>
            {config.allowRegister && (
              <div className="flex rounded-md border border-neutral-200 overflow-hidden text-sm">
                <button
                  className={`flex-1 py-1.5 font-medium transition-colors ${mode === 'login' ? 'bg-neutral-900 text-white' : 'text-neutral-500 hover:bg-neutral-50'}`}
                  onClick={() => switchMode('login')}
                >
                  Sign in
                </button>
                <button
                  className={`flex-1 py-1.5 font-medium transition-colors ${mode === 'register' ? 'bg-neutral-900 text-white' : 'text-neutral-500 hover:bg-neutral-50'}`}
                  onClick={() => switchMode('register')}
                >
                  Register
                </button>
              </div>
            )}

            <input
              className="border border-neutral-200 rounded-md px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-neutral-300"
              placeholder="Username"
              value={username}
              onChange={e => setUsername(e.target.value)}
              onKeyDown={e => { if (e.key === 'Enter') handleSubmit() }}
              autoFocus
            />

            {mode === 'register' && (
              <input
                className="border border-neutral-200 rounded-md px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-neutral-300"
                placeholder="Email (optional)"
                type="email"
                value={email}
                onChange={e => setEmail(e.target.value)}
                onKeyDown={e => { if (e.key === 'Enter') handleSubmit() }}
              />
            )}

            <input
              className="border border-neutral-200 rounded-md px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-neutral-300"
              placeholder="Password"
              type="password"
              value={password}
              onChange={e => setPassword(e.target.value)}
              onKeyDown={e => { if (e.key === 'Enter') handleSubmit() }}
            />

            {error && <p className="text-xs text-red-500 text-center">{error}</p>}

            <button
              className="bg-neutral-900 text-white rounded-md px-4 py-2 text-sm font-medium disabled:opacity-50"
              disabled={!username.trim() || !password || loading}
              onClick={handleSubmit}
            >
              {loading ? 'Please wait…' : mode === 'register' ? 'Create account' : 'Sign in'}
            </button>
          </>
        )}

        {!config && (
          <p className="text-sm text-neutral-400 text-center">Loading…</p>
        )}
      </div>
    </div>
  )
}
