// Thin fetch wrapper for /api/v1. Every state-changing request needs the
// X-Requested-With header (CSRF rule from Task 13); GETs send it too so a
// single code path covers every call. 401 responses throw ApiError so
// AuthGate can redirect to /login.
import type { ApiErrorBody } from './types'

export const CSRF_HEADER = 'X-Requested-With'
export const CSRF_VALUE = 'styr'

export class ApiError extends Error {
  status: number
  code: string
  constructor(status: number, body?: ApiErrorBody) {
    super(body?.error?.message ?? `HTTP ${status}`)
    this.status = status
    this.code = body?.error?.code ?? 'http_error'
  }
}

async function parseError(res: Response): Promise<ApiError> {
  let body: ApiErrorBody | undefined
  try {
    body = (await res.json()) as ApiErrorBody
  } catch {
    body = undefined
  }
  return new ApiError(res.status, body)
}

export async function api<T>(path: string, init: RequestInit & { json?: unknown } = {}): Promise<T> {
  const { json, headers, ...rest } = init
  const method = rest.method ?? 'GET'
  const finalHeaders: Record<string, string> = {
    Accept: 'application/json',
    [CSRF_HEADER]: CSRF_VALUE,
    ...(headers as Record<string, string> | undefined),
  }
  let body = rest.body
  if (json !== undefined) {
    finalHeaders['Content-Type'] = 'application/json'
    body = JSON.stringify(json)
  }
  const res = await fetch(path, {
    ...rest,
    method,
    headers: finalHeaders,
    body,
    credentials: 'same-origin',
  })
  if (!res.ok) throw await parseError(res)
  if (res.status === 204) return undefined as T
  const contentType = res.headers.get('content-type') ?? ''
  if (contentType.includes('application/json')) return (await res.json()) as T
  return undefined as T
}
