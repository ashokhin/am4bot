import { startRegistration } from '@simplewebauthn/browser'
import type {
  AuthenticationResponseJSON,
  PublicKeyCredentialCreationOptionsJSON,
  PublicKeyCredentialRequestOptionsJSON,
  RegistrationResponseJSON,
} from '@simplewebauthn/browser'

import { BASE_PATH } from '../basePath'
import { api, ApiError } from './client'
import type { ApiErrorBody, Passkey, SkipPasskeyPromptRequest, User } from './types'

// passkeysApi wraps the WebAuthn registration/login ceremonies on top of
// internal/api/passkey_handlers.go's routes. The finish calls below don't
// go through api.post() like the rest of the client - they send the
// browser's own attestation/assertion response straight through as the
// body (what @simplewebauthn/browser's startRegistration/startAuthentication
// return), with no extra wrapping, which is exactly what the Go handlers
// parse directly from *http.Request.
async function postRaw<T>(path: string, body: unknown): Promise<T> {
  const res = await fetch(BASE_PATH + path, {
    method: 'POST',
    credentials: 'include',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  })

  if (res.status === 204) {
    return undefined as T
  }

  const text = await res.text()
  const data = text ? JSON.parse(text) : undefined

  if (!res.ok) {
    const message = (data as ApiErrorBody | undefined)?.error ?? res.statusText
    throw new ApiError(res.status, message)
  }

  return data as T
}

export interface CredentialCreation {
  publicKey: PublicKeyCredentialCreationOptionsJSON
}

export interface PasskeyLoginBeginResponse {
  flow_id: string
  assertion: { publicKey: PublicKeyCredentialRequestOptionsJSON }
}

export const passkeysApi = {
  list: () => api.get<Passkey[]>('/api/me/passkeys'),
  delete: (id: number) => api.delete<void>(`/api/me/passkeys/${id}`),
  skipPrompt: (req: SkipPasskeyPromptRequest) => api.post<void>('/api/me/passkey-prompt/skip', req),

  registerBegin: () => api.post<CredentialCreation>('/api/me/passkeys/register/begin'),
  registerFinish: (name: string, response: RegistrationResponseJSON) =>
    postRaw<void>(`/api/me/passkeys/register/finish?name=${encodeURIComponent(name)}`, response),

  loginBegin: () => api.post<PasskeyLoginBeginResponse>('/api/auth/passkey/login/begin'),
  loginFinish: (flowID: string, response: AuthenticationResponseJSON) =>
    postRaw<User>(`/api/auth/passkey/login/finish?flow_id=${encodeURIComponent(flowID)}`, response),
}

// addPasskey runs the whole registration ceremony (begin, the browser's
// own navigator.credentials.create() prompt via startRegistration, then
// finish) - shared by the post-login "add a passkey?" prompt and
// Settings' own "add a passkey" button, so the sequence can't drift
// between the two call sites.
export async function addPasskey(name: string): Promise<void> {
  const creation = await passkeysApi.registerBegin()
  const response = await startRegistration({ optionsJSON: creation.publicKey })
  await passkeysApi.registerFinish(name, response)
}
