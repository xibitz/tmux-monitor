export type User = { id: string; username: string; isAdmin: boolean };
export type Identity = { user: User; csrfToken: string };
export type HostOptions = { allowedSSHHosts: string[] };
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
  stale: boolean;
};
export type SSHMetadata = {
  configured: boolean;
  port?: number;
  fingerprint?: string;
  configuredAt?: string;
};
export type Inventory = {
  hostId: string;
  socketId: string;
  transport: string;
  state: string;
  statusReason: string;
  observedAt: string;
  stale: boolean;
  epoch: string;
  capabilities: {
    inventory: boolean;
    ptyAttach: boolean;
    controlMode: boolean;
    independentPane: boolean;
    tmuxVersion: string;
    serverRunning: boolean;
  };
  sessions: {
    id: string;
    name: string;
    revision: string;
    renameRevision: string;
    windows: {
      id: string;
      name: string;
      index: number;
      active: boolean;
      revision: string;
      panes: {
        id: string;
        active: boolean;
        width: number;
        height: number;
        command: string;
        revision: string;
      }[];
    }[];
  }[];
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
  operationKey?: string,
): Promise<T> {
  const headers: Record<string, string> = {};
  if (body !== undefined) headers["Content-Type"] = "application/json";
  if (method !== "GET") headers["X-CSRF-Token"] = csrfToken;
  if (operationKey) headers["Idempotency-Key"] = operationKey;
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
