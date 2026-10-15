import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { addPasskey, passkeysApi } from '../api/passkeys'
import { useAuth } from '../auth/AuthContext'
import { Button } from './ui/button'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from './ui/dialog'
import { Label } from './ui/label'
import { Switch } from './ui/switch'

/**
 * Shown right after a successful login when User.show_passkey_prompt is
 * true - see internal/api's recordSuccessfulLogin doc comment for exactly
 * when that is (no passkey yet, prompt not dismissed, due by login count).
 * "Add" starts the registration ceremony on the spot; "Skip" (optionally
 * with "don't show again") just records the choice server-side.
 */
export function PasskeyPromptModal() {
  const { t } = useTranslation()
  const { user, clearPasskeyPrompt } = useAuth()
  const [dontShowAgain, setDontShowAgain] = useState(false)
  const [busy, setBusy] = useState(false)

  const open = Boolean(user?.show_passkey_prompt)

  async function handleAdd() {
    setBusy(true)

    try {
      await addPasskey(navigator.userAgent)
      clearPasskeyPrompt()
      toast.success(t('settings.saved'))
    } catch {
      toast.error(t('passkeys.addError'))
    } finally {
      setBusy(false)
    }
  }

  async function handleSkip() {
    setBusy(true)

    try {
      await passkeysApi.skipPrompt({ dont_show_again: dontShowAgain })
    } catch {
      // Best-effort - worst case the prompt just resurfaces next login.
    } finally {
      clearPasskeyPrompt()
      setBusy(false)
    }
  }

  return (
    <Dialog open={open}>
      <DialogContent showCloseButton={false} onEscapeKeyDown={(e) => e.preventDefault()} onInteractOutside={(e) => e.preventDefault()}>
        <DialogHeader>
          <DialogTitle>{t('passkeys.promptTitle')}</DialogTitle>
          <DialogDescription>{t('passkeys.promptBody')}</DialogDescription>
        </DialogHeader>
        <div className="flex items-center gap-2">
          <Switch id="dont-show-again" checked={dontShowAgain} onCheckedChange={setDontShowAgain} disabled={busy} />
          <Label htmlFor="dont-show-again">{t('passkeys.dontShowAgain')}</Label>
        </div>
        <DialogFooter>
          <Button type="button" variant="outline" disabled={busy} onClick={() => void handleSkip()}>
            {t('passkeys.skip')}
          </Button>
          <Button type="button" disabled={busy} onClick={() => void handleAdd()}>
            {t('passkeys.add')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
