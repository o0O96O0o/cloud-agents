import { useEffect, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { ArrowLeft } from 'lucide-react'
import { getUserSettings, updateUserSettings } from '@/api/client'
import { Button } from '@/components/ui/button'
import { Textarea } from '@/components/ui/textarea'
import { Input } from '@/components/ui/input'

export function SettingsPage() {
  const navigate = useNavigate()
  const [sshKeyValue, setSshKeyValue] = useState('')
  const [hasSSHKey, setHasSSHKey] = useState(false)
  const [anthropicKeyValue, setAnthropicKeyValue] = useState('')
  const [hasAnthropicKey, setHasAnthropicKey] = useState(false)
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState<'ssh' | 'anthropic' | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [success, setSuccess] = useState<string | null>(null)

  useEffect(() => {
    getUserSettings()
      .then(s => {
        setHasSSHKey(s.has_ssh_key)
        setHasAnthropicKey(s.has_anthropic_key)
      })
      .catch(() => setError('Failed to load settings.'))
      .finally(() => setLoading(false))
  }, [])

  const handleSaveSSH = async () => {
    if (!sshKeyValue.trim()) return
    setSaving('ssh')
    setError(null)
    setSuccess(null)
    try {
      await updateUserSettings({ ssh_private_key: sshKeyValue.trim() + '\n' })
      setHasSSHKey(true)
      setSshKeyValue('')
      setSuccess('SSH key saved.')
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Failed to save key')
    } finally {
      setSaving(null)
    }
  }

  const handleClearSSH = async () => {
    setSaving('ssh')
    setError(null)
    setSuccess(null)
    try {
      await updateUserSettings({ ssh_private_key: '' })
      setHasSSHKey(false)
      setSshKeyValue('')
      setSuccess('SSH key removed.')
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Failed to clear key')
    } finally {
      setSaving(null)
    }
  }

  const handleSaveAnthropicKey = async () => {
    if (!anthropicKeyValue.trim()) return
    setSaving('anthropic')
    setError(null)
    setSuccess(null)
    try {
      await updateUserSettings({ anthropic_api_key: anthropicKeyValue.trim() })
      setHasAnthropicKey(true)
      setAnthropicKeyValue('')
      setSuccess('Anthropic API key saved.')
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Failed to save key')
    } finally {
      setSaving(null)
    }
  }

  const handleClearAnthropicKey = async () => {
    setSaving('anthropic')
    setError(null)
    setSuccess(null)
    try {
      await updateUserSettings({ anthropic_api_key: '' })
      setHasAnthropicKey(false)
      setAnthropicKeyValue('')
      setSuccess('Anthropic API key removed.')
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Failed to clear key')
    } finally {
      setSaving(null)
    }
  }

  return (
    <div className="min-h-screen bg-white">
      <header className="flex items-center gap-3 px-6 py-4 border-b border-neutral-200">
        <button
          onClick={() => navigate('/')}
          className="p-1.5 rounded hover:bg-neutral-100 text-neutral-500 hover:text-neutral-700 transition-colors"
          title="Back"
        >
          <ArrowLeft size={16} />
        </button>
        <span className="font-semibold text-sm">Settings</span>
      </header>

      <div className="max-w-xl mx-auto px-6 py-8 space-y-8">
        <div>
          <div className="flex items-center justify-between mb-1">
            <h2 className="text-sm font-medium text-neutral-800">Anthropic API Key</h2>
            {!loading && (
              <span className={`text-xs px-2 py-0.5 rounded-full font-medium ${hasAnthropicKey ? 'bg-green-100 text-green-700' : 'bg-neutral-100 text-neutral-500'}`}>
                {hasAnthropicKey ? 'Key configured' : 'Using server default'}
              </span>
            )}
          </div>
          <p className="text-xs text-neutral-500 mb-3">
            Your personal Anthropic API key. When set, it overrides the server-wide key for all your tasks. The key is encrypted at rest and never returned by the server.
          </p>
          <Input
            className="font-mono text-xs"
            type="password"
            placeholder="sk-ant-..."
            value={anthropicKeyValue}
            onChange={e => setAnthropicKeyValue(e.target.value)}
          />
          <div className="flex gap-2 mt-3">
            {hasAnthropicKey && (
              <Button variant="outline" size="sm" onClick={handleClearAnthropicKey} disabled={saving !== null}>
                Clear
              </Button>
            )}
            <Button size="sm" onClick={handleSaveAnthropicKey} disabled={saving !== null || !anthropicKeyValue.trim()}>
              Save
            </Button>
          </div>
        </div>

        <div className="border-t border-neutral-100 pt-8">
          <div className="flex items-center justify-between mb-1">
            <h2 className="text-sm font-medium text-neutral-800">SSH Private Key</h2>
            {!loading && (
              <span className={`text-xs px-2 py-0.5 rounded-full font-medium ${hasSSHKey ? 'bg-green-100 text-green-700' : 'bg-neutral-100 text-neutral-500'}`}>
                {hasSSHKey ? 'Key configured' : 'No key'}
              </span>
            )}
          </div>
          <p className="text-xs text-neutral-500 mb-3">
            Used to clone private git repositories. The key is encrypted at rest and never returned by the server.
          </p>
          <Textarea
            className="font-mono text-xs h-40 resize-none"
            placeholder="-----BEGIN OPENSSH PRIVATE KEY-----"
            value={sshKeyValue}
            onChange={e => setSshKeyValue(e.target.value)}
          />
          <div className="flex gap-2 mt-3">
            {hasSSHKey && (
              <Button variant="outline" size="sm" onClick={handleClearSSH} disabled={saving !== null}>
                Clear
              </Button>
            )}
            <Button size="sm" onClick={handleSaveSSH} disabled={saving !== null || !sshKeyValue.trim()}>
              Save
            </Button>
          </div>
        </div>

        {error && <p className="text-xs text-red-600">{error}</p>}
        {success && <p className="text-xs text-green-600">{success}</p>}
      </div>
    </div>
  )
}
