export interface SessionInfo { Authenticated: boolean; SetupRequired: boolean }
export interface Server { ID: number; Name: string; State: string; CreatedAt: string; Software: string; Version: string; Build: number; JavaPath: string; RequiredJava: number; MemoryMB: number; Port: number; LastError: string; PID: number }
export interface Job { ID: number; Kind: string; State: string; CreatedAt: string; ServerID: number; Message: string }
export interface JavaRuntime { Path: string; Version: string; Major: number }
export interface MinecraftVersion { ID: string }
export interface CreateServerInput { Name: string; Software: string; Version: string; JavaPath: string; MemoryMB: number; Port: number; EULAAccepted: boolean }
export interface ODataCollection<T> { value: T[]; '@odata.nextLink'?: string; '@odata.count'?: number }
export interface Credentials { Username: string; Password: string }

export class ApiError extends Error {
  constructor(public status: number, message: string) { super(message); }
}

async function request<T>(path: string, body?: unknown): Promise<T> {
  const response = await fetch(`/odata/${path}`, {
    method: body === undefined ? 'GET' : 'POST',
    credentials: 'same-origin',
    headers: { Accept: 'application/json', ...(body === undefined ? {} : { 'Content-Type': 'application/json', 'X-Chunkd-Request': '1' }) },
    ...(body === undefined ? {} : { body: JSON.stringify(body) }),
  });
  if (!response.ok) {
    const error = await response.json().catch(() => null) as { error?: { message?: string } } | null;
    throw new ApiError(response.status, error?.error?.message ?? `Request failed (${response.status})`);
  }
  if (response.status === 204) return undefined as T;
  return response.json() as Promise<T>;
}

export const api = {
  session: async () => (await request<{ value: SessionInfo }>('GetSession()')).value,
  setup: (credentials: Credentials & { SetupToken: string }) => request<void>('Setup', credentials),
  login: (credentials: Credentials) => request<void>('Login', credentials),
  logout: () => request<void>('Logout', {}),
  servers: () => request<ODataCollection<Server>>('Servers?$orderby=ID&$top=100'),
  jobs: () => request<ODataCollection<Job>>('Jobs?$orderby=ID%20desc&$top=10'),
  runtimes: async () => (await request<{ value: JavaRuntime[] }>('GetJavaRuntimes()')).value,
  versions: async () => (await request<{ value: MinecraftVersion[] }>('GetMinecraftVersions()')).value,
  createServer: (input: CreateServerInput) => request<Job>('CreateServer', input),
  lifecycle: (id: number, action: 'Start' | 'Stop' | 'Restart' | 'RetryInstall') => request<Job>(`Servers(${id})/Chunkd.${action}`, {}),
};
