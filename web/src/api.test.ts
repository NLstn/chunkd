import { afterEach, expect, it, vi } from 'vitest';
import { api } from './api';

afterEach(() => vi.unstubAllGlobals());

it('reads the go-odata function envelope and uses same-origin cookie credentials', async () => {
  const fetch = vi.fn().mockResolvedValue(new Response(JSON.stringify({ value: { Authenticated: true, SetupRequired: false } }), { status: 200 }));
  vi.stubGlobal('fetch', fetch);
  expect(await api.session()).toEqual({ Authenticated: true, SetupRequired: false });
  expect(fetch).toHaveBeenCalledWith('/odata/GetSession()', expect.objectContaining({ credentials: 'same-origin', method: 'GET' }));
});

it('uses an OData action with the CSRF guard header for login', async () => {
  const fetch = vi.fn().mockResolvedValue(new Response(null, { status: 204 }));
  vi.stubGlobal('fetch', fetch);
  await api.login({ Username: 'admin', Password: 'secret' });
  expect(fetch).toHaveBeenCalledWith('/odata/Login', expect.objectContaining({ method: 'POST', body: JSON.stringify({ Username: 'admin', Password: 'secret' }), headers: expect.objectContaining({ 'X-Chunkd-Request': '1' }) }));
});

it('preserves structured OData error messages', async () => {
  vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response(JSON.stringify({ error: { code: 'Unauthorized', message: 'Invalid credentials' } }), { status: 401 })));
  await expect(api.login({ Username: 'admin', Password: 'wrong' })).rejects.toMatchObject({ status: 401, message: 'Invalid credentials' });
});
