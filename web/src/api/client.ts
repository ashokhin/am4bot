import { BASE_PATH } from '../basePath'
import type { ApiErrorBody } from './types'

/** Thrown by request() for any non-2xx response. */
export class ApiError extends Error {
  status: number

  constructor(status: number, message: string) {
    super(message)
    this.name = 'ApiError'
    this.status = status
  }
}

/**
 * Thin fetch wrapper for talking to apiserver's /api/* routes.
 *
 * `credentials: 'include'` is required on every call: the session lives in
 * an httpOnly cookie (see internal/api/server.go's setSessionCookie), and
 * without this the browser won't send it even to a same-origin request in
 * some configurations.
 */
async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
  const res = await fetch(BASE_PATH + path, {
    method,
    credentials: 'include',
    headers: body === undefined ? undefined : { 'Content-Type': 'application/json' },
    body: body === undefined ? undefined : JSON.stringify(body),
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

export const api = {
  get: <T>(path: string) => request<T>('GET', path),
  post: <T>(path: string, body?: unknown) => request<T>('POST', path, body ?? {}),
  put: <T>(path: string, body?: unknown) => request<T>('PUT', path, body ?? {}),
  patch: <T>(path: string, body?: unknown) => request<T>('PATCH', path, body ?? {}),
  delete: <T>(path: string) => request<T>('DELETE', path),
}
