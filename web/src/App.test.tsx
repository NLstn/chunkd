import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import App from './App';
import { api, ApiError } from './api';

vi.mock('./api', async importOriginal => {
  const actual = await importOriginal<typeof import('./api')>();
  return { ...actual, api: { session: vi.fn(), setup: vi.fn(), login: vi.fn(), logout: vi.fn(), servers: vi.fn(), jobs: vi.fn() } };
});

beforeEach(() => { vi.resetAllMocks(); });
afterEach(cleanup);

describe('administrator workflow', () => {
  it('sets up one administrator and opens the authenticated empty dashboard', async () => {
    vi.mocked(api.session).mockResolvedValueOnce({ Authenticated: false, SetupRequired: true }).mockResolvedValue({ Authenticated: true, SetupRequired: false });
    vi.mocked(api.setup).mockResolvedValue(); vi.mocked(api.login).mockResolvedValue();
    vi.mocked(api.servers).mockResolvedValue({ value: [] }); vi.mocked(api.jobs).mockResolvedValue({ value: [] });
    render(<App />);
    const user = userEvent.setup();
    await user.type(await screen.findByLabelText(/Setup token/), 'test-token');
    await user.type(screen.getByLabelText('Username'), 'admin');
    await user.type(screen.getByLabelText(/Password/), 'a long test passphrase');
    await user.click(screen.getByRole('button', { name: 'Set up and sign in' }));
    expect(await screen.findByText('Your first world starts here.')).toBeInTheDocument();
    expect(api.setup).toHaveBeenCalledWith({ Username: 'admin', Password: 'a long test passphrase', SetupToken: 'test-token' });
    expect(api.login).toHaveBeenCalledWith({ Username: 'admin', Password: 'a long test passphrase' });
    expect(screen.queryByText('Users')).not.toBeInTheDocument();
    await user.click(screen.getByRole('button', { name: 'Sign out' }));
    expect(await screen.findByText('Sign in to Chunkd')).toBeInTheDocument();
    expect(api.logout).toHaveBeenCalledOnce();
  });

  it('shows login failures without leaving the sign-in form', async () => {
    vi.mocked(api.session).mockResolvedValue({ Authenticated: false, SetupRequired: false });
    vi.mocked(api.login).mockRejectedValue(new ApiError(401, 'Invalid credentials'));
    render(<App />);
    const user = userEvent.setup();
    await user.type(await screen.findByLabelText('Username'), 'admin');
    await user.type(screen.getByLabelText('Password'), 'wrong');
    await user.click(screen.getByRole('button', { name: 'Sign in' }));
    expect(await screen.findByRole('alert')).toHaveTextContent('Invalid credentials');
    expect(screen.getByText('Sign in to Chunkd')).toBeInTheDocument();
  });

  it('returns to sign-in when the session expires during data retrieval', async () => {
    vi.mocked(api.session).mockResolvedValue({ Authenticated: true, SetupRequired: false });
    vi.mocked(api.servers).mockRejectedValue(new ApiError(401, 'Sign in required'));
    vi.mocked(api.jobs).mockResolvedValue({ value: [] });
    render(<App />);
    expect(await screen.findByText('Sign in to Chunkd')).toBeInTheDocument();
    await waitFor(() => expect(screen.getByRole('alert')).toHaveTextContent('Sign in required'));
  });
});
