import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { cleanup, render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import ServerDetails from './ServerDetails';
import { api, ApiError } from './api';

vi.mock('./api', async importOriginal => ({ ...await importOriginal<typeof import('./api')>(), api: { server: vi.fn(), details: vi.fn(), settings: vi.fn(), command: vi.fn(), saveSettings: vi.fn(), lifecycle: vi.fn() } }));
const server = { ID: 1, Name: 'Survival', State: 'Stopped', CreatedAt: '', Software: 'vanilla', Version: '1.21.11', Build: 0, JavaPath: '/usr/bin/java', RequiredJava: 21, MemoryMB: 2048, Port: 25565, LastError: '', PID: 0 };
const settings = { MOTD: 'My world', MaxPlayers: 20, Difficulty: 'easy', GameMode: 'survival', ViewDistance: 10, PVP: true, WhiteList: false };
beforeEach(() => {
 vi.resetAllMocks();
 vi.mocked(api.server).mockResolvedValue(server);
 vi.mocked(api.details).mockResolvedValue({ CPUPercent: null, MemoryMB: null, UptimeSeconds: null, Players: null, Logs: '', LogOffset: 0, LogReset: false });
 vi.mocked(api.settings).mockResolvedValue(settings);
});
afterEach(cleanup);

it('shows unavailable metrics instead of invented zeroes for stopped servers', async () => {
 render(<ServerDetails id={1} tab="overview" />);
 expect(await screen.findByRole('heading', { name: 'Survival' })).toBeInTheDocument();
 expect(screen.getAllByText('Unavailable')).toHaveLength(4);
 expect(screen.getByRole('link', { name: /All servers/ })).toHaveAttribute('href', '#/');
});
it('sends console commands for the selected running server', async () => {
 vi.mocked(api.server).mockResolvedValue({ ...server, State: 'Running', PID: 123 });
 vi.mocked(api.command).mockResolvedValue();
 render(<ServerDetails id={1} tab="console" />);
 const input = await screen.findByLabelText('Console command');
 await userEvent.type(input, 'say hello');
 await userEvent.click(screen.getByRole('button', { name: 'Send command' }));
 expect(api.command).toHaveBeenCalledWith(1, 'say hello');
 expect(await screen.findByText(/Command sent/)).toBeInTheDocument();
 expect(input).toHaveValue('');
});
it('saves validated settings and explains when changes apply', async () => {
 vi.mocked(api.saveSettings).mockResolvedValue();
 render(<ServerDetails id={1} tab="settings" />);
 const motd = await screen.findByLabelText('Server message (MOTD)');
 await userEvent.clear(motd); await userEvent.type(motd, 'New world');
 await userEvent.click(screen.getByRole('button', { name: 'Save settings' }));
 expect(api.saveSettings).toHaveBeenCalledWith(1, { ...settings, MOTD: 'New world' });
 expect(await screen.findByText(/Settings saved/)).toBeInTheDocument();
});
it('prevents settings edits while running', async () => {
 vi.mocked(api.server).mockResolvedValue({ ...server, State: 'Running' });
 render(<ServerDetails id={1} tab="settings" />);
 expect(await screen.findByLabelText('Server message (MOTD)')).toBeDisabled();
 expect(screen.getByRole('button', { name: 'Save settings' })).toBeDisabled();
});
it('handles a missing server', async () => {
 vi.mocked(api.server).mockRejectedValue(new ApiError(404, 'Server not found'));
 render(<ServerDetails id={100} tab="overview" />);
 expect(await screen.findByRole('heading', { name: 'Server not found' })).toBeInTheDocument();
});
