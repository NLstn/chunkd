import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { cleanup, render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import ServerPanel from './ServerPanel';
import { api } from './api';
import type { Job, Server } from './api';

vi.mock('./api', () => ({ api: { runtimes: vi.fn(), versions: vi.fn(), createServer: vi.fn(), lifecycle: vi.fn() } }));
beforeEach(() => vi.resetAllMocks());
afterEach(cleanup);

const server: Server = { ID: 1, Name: 'Survival', State: 'Stopped', CreatedAt: '', Software: 'vanilla', Version: '1.21.11', Build: 0, JavaPath: '/usr/bin/java', RequiredJava: 21, MemoryMB: 2048, Port: 25565, LastError: '', PID: 0 };
const job: Job = { ID: 1, Kind: 'Install', State: 'Queued', CreatedAt: '', ServerID: 1, Message: '' };

it('requires explicit EULA consent and queues a configured server installation', async () => {
  vi.mocked(api.runtimes).mockResolvedValue([{ Path: '/usr/bin/java', Version: '25.0.1', Major: 25 }]);
  vi.mocked(api.versions).mockResolvedValue([{ ID: '1.21.11' }]);
  vi.mocked(api.createServer).mockResolvedValue(job);
  const changed = vi.fn().mockResolvedValue(undefined);
  render(<ServerPanel servers={[]} jobs={[]} loaded changed={changed} />);
  const user = userEvent.setup();
  await user.click(screen.getByRole('button', { name: 'Create server' }));
  await screen.findByRole('option', { name: '1.21.11' });
  await user.type(screen.getByLabelText('Server name'), 'Survival');
  await user.click(screen.getByRole('button', { name: 'Install server' }));
  expect(api.createServer).not.toHaveBeenCalled();
  await user.click(screen.getByRole('checkbox'));
  await user.click(screen.getByRole('button', { name: 'Install server' }));
  expect(await screen.findByText(/Installation queued/)).toBeInTheDocument();
  expect(api.createServer).toHaveBeenCalledWith({ Name: 'Survival', Software: 'vanilla', Version: '1.21.11', JavaPath: '/usr/bin/java', MemoryMB: 2048, Port: 25565, EULAAccepted: true });
  expect(changed).toHaveBeenCalledOnce();
});

it('queues lifecycle actions and disables controls for a pending operation', async () => {
  vi.mocked(api.lifecycle).mockResolvedValue({ ...job, Kind: 'Start' });
  const changed = vi.fn().mockResolvedValue(undefined);
  const { rerender } = render(<ServerPanel servers={[server]} jobs={[]} loaded changed={changed} />);
  const user = userEvent.setup();
  await user.click(screen.getByRole('button', { name: 'Start Survival' }));
  expect(await screen.findByText('Start queued.')).toBeInTheDocument();
  expect(api.lifecycle).toHaveBeenCalledWith(1, 'Start');
  rerender(<ServerPanel servers={[server]} jobs={[job]} loaded changed={changed} />);
  expect(screen.getByRole('button', { name: 'Start Survival' })).toBeDisabled();
});

it('shows configuration guidance when no runtime is installed', async () => {
  vi.mocked(api.runtimes).mockResolvedValue([]);
  vi.mocked(api.versions).mockResolvedValue([{ ID: '1.21.11' }]);
  render(<ServerPanel servers={[]} jobs={[]} loaded changed={vi.fn()} />);
  await userEvent.setup().click(screen.getByRole('button', { name: 'Create server' }));
  expect(await screen.findByText(/No Java runtime found/)).toBeInTheDocument();
  expect(screen.getByRole('button', { name: 'Install server' })).toBeDisabled();
});
