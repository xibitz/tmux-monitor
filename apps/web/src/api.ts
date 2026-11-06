export type User = { id: string; username: string; isAdmin: boolean };
export type Identity = { user: User; csrfToken: string };
export type HostInput = {
  label: string;
  transport: "ssh" | "agent";
  tailnetAddress: string;
  osUser: string;
  socketName: string;
  environment: string;
};
export type Host = HostInput & {
  id: string;
  state: string;
  statusReason: string;
  observedAt: string | null;
  createdAt: string;
  updatedAt: string;
};
export type AuditEvent = {
  id: number;
  actor: string;
  action: string;
  hostId?: string;
  result: string;
  requestId: string;
  at: string;
};
export class APIError extends Error {
  constructor(
    public status: number,
    message: string,
  ) {
    super(message);
  }
}

// Session and CSRF state stay in memory. Never cache API responses or credentials.
let csrfToken = "";
let unauthorized: (() => void) | undefined;
export function configureSession(
  identity?: Identity,
  onUnauthorized?: () => void,
) {
  csrfToken = identity?.csrfToken ?? "";
  unauthorized = onUnauthorized;
}
export async function api<T>(
  path: string,
  method = "GET",
  body?: unknown,
): Promise<T> {
  const headers: Record<string, string> = {};
  if (body !== undefined) headers["Content-Type"] = "application/json";
  if (method !== "GET") headers["X-CSRF-Token"] = csrfToken;
  let response: Response;
  try {
    response = await fetch(`/api/v1${path}`, {
      method,
      headers,
      credentials: "same-origin",
      cache: "no-store",
      body: body === undefined ? undefined : JSON.stringify(body),
      signal: AbortSignal.timeout(15_000),
    });
  } catch {
    throw new APIError(
      0,
      "Hub tidak dapat dijangkau. Periksa koneksi Anda, lalu coba lagi.",
    );
  }
  if (response.status === 204) return undefined as T;
  const data = await response.json().catch(() => null);
  if (!response.ok) {
    if (response.status === 401 && path !== "/auth/login") unauthorized?.();
    throw new APIError(
      response.status,
      data?.error?.message ?? "Permintaan gagal. Coba lagi.",
    );
  }
  return data as T;
}
