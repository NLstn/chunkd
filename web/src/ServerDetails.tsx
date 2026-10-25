import { useEffect, useRef, useState } from 'react';
import type { FormEvent } from 'react';
import { api, ApiError } from './api';
import type { Server, ServerDetailsData, ServerSettings } from './api';

const message = (err: unknown) => err instanceof Error ? err.message : 'Unable to connect to this server.';
const metric = (value: number | null | undefined, suffix: string) => value == null ? 'Unavailable' : `${value.toFixed(1)}${suffix}`;
function uptime(seconds: number | null | undefined) {
  if (seconds == null) return 'Unavailable';
  const minutes = Math.floor(seconds / 60);
  return minutes >= 60 ? `${Math.floor(minutes / 60)}h ${minutes % 60}m` : `${minutes}m ${Math.floor(seconds % 60)}s`;
}

export default function ServerDetails({ id, tab }: { id: number; tab: string }) {
  const [server, setServer] = useState<Server | null>(null);
  const [details, setDetails] = useState<ServerDetailsData | null>(null);
  const [logs, setLogs] = useState('');
  const [error, setError] = useState('');
  const [actionError, setActionError] = useState('');
  const [missing, setMissing] = useState(false);
  const [busy, setBusy] = useState(false);
  const [notice, setNotice] = useState('');
  const [command, setCommand] = useState('');
  const [settings, setSettings] = useState<ServerSettings | null>(null);
  const [settingsError, setSettingsError] = useState('');
  const [settingsAttempt, setSettingsAttempt] = useState(0);
  const [follow, setFollow] = useState(true);
  const logView = useRef<HTMLPreElement>(null);

  useEffect(() => {
    let cancelled = false;
    let timer: ReturnType<typeof setTimeout>;
    let offset = 0;
    async function poll() {
      try {
        const [s, d] = await Promise.all([api.server(id), api.details(id, offset)]);
        if (cancelled) return;
        setServer(s); setDetails(d); setError(''); setMissing(false);
        setLogs(previous => (d.LogReset ? d.Logs : previous + d.Logs).slice(-65536));
        offset = d.LogOffset;
      } catch (err) {
        if (cancelled) return;
        setDetails(null); setError(message(err));
        if (err instanceof ApiError && err.status === 404) setMissing(true);
      } finally { if (!cancelled) timer = setTimeout(() => void poll(), 2500); }
    }
    void poll();
    return () => { cancelled = true; clearTimeout(timer); };
  }, [id]);

  useEffect(() => {
    if (follow && logView.current) logView.current.scrollTop = logView.current.scrollHeight;
  }, [logs, follow, tab]);

  useEffect(() => {
    if (tab !== 'settings') return;
    let cancelled = false;
    setSettingsError('');
    api.settings(id).then(value => { if (!cancelled) setSettings(value); }).catch(err => { if (!cancelled) setSettingsError(message(err)); });
    return () => { cancelled = true; };
  }, [id, tab, settingsAttempt]);

  async function control(action: 'Start' | 'Stop' | 'Restart' | 'RetryInstall') {
    setBusy(true); setNotice(''); setActionError('');
    try { await api.lifecycle(id, action); setNotice(`${action === 'RetryInstall' ? 'Installation retry' : action} queued.`); setServer(await api.server(id)); }
    catch (err) { setActionError(message(err)); }
    finally { setBusy(false); }
  }
  async function send(event: FormEvent) {
    event.preventDefault(); setBusy(true); setNotice(''); setActionError('');
    try { await api.command(id, command); setCommand(''); setNotice('Command sent. Responses appear in the console.'); }
    catch (err) { setActionError(message(err)); }
    finally { setBusy(false); }
  }
  async function save(event: FormEvent) {
    event.preventDefault(); if (!settings) return;
    setBusy(true); setNotice(''); setActionError(''); setSettingsError('');
    try { await api.saveSettings(id, settings); setNotice('Settings saved. Changes apply on the next server start.'); }
    catch (err) { setSettingsError(message(err)); }
    finally { setBusy(false); }
  }
  const editable = server && ['Stopped', 'Crashed'].includes(server.State);
  return <section className="dashboard server-details">
    <a className="back-link" href="#/">← All servers</a>
    {error && <p role="alert" className="error">{error}</p>}
    {actionError && <p role="alert" className="error">{actionError}</p>}
    {notice && <p role="status" className="notice">{notice}</p>}
    {missing ? <div className="card"><h1>Server not found</h1><p>This server may have been removed. Return to the server list.</p></div> : !server ? <p role="status">Loading server details…</p> : <>
      <div className="section-heading"><div><p className="eyebrow">SERVER / {server.Software.toUpperCase()} {server.Version}</p><h1>{server.Name}</h1><p>Port {server.Port} · {server.MemoryMB} MiB maximum memory</p></div><span className={`state state-${server.State.toLowerCase()}`}>{server.State}</span></div>
      <div className="actions lifecycle">
        {['Stopped', 'Crashed'].includes(server.State) && <button className="quiet" disabled={busy} onClick={() => void control('Start')}>Start server</button>}
        {['Running', 'Starting'].includes(server.State) && <button className="quiet" disabled={busy} onClick={() => void control('Stop')}>Stop server</button>}
        {server.State === 'Running' && <button className="quiet" disabled={busy} onClick={() => void control('Restart')}>Restart server</button>}
        {server.State === 'Failed' && <button className="quiet" disabled={busy} onClick={() => void control('RetryInstall')}>Retry installation</button>}
      </div>
      {server.LastError && <p className="error">{server.LastError}</p>}
      <nav className="detail-tabs" aria-label="Server pages">{['overview', 'console', 'settings'].map(page => <a key={page} href={`#/servers/${id}/${page}`} aria-current={tab === page ? 'page' : undefined}>{page === 'console' ? 'Console & logs' : page[0].toUpperCase() + page.slice(1)}</a>)}</nav>
      {tab === 'overview' && <>
        <div className="stats resource-stats">
          <div className="card"><span>CPU usage</span><strong>{metric(details?.CPUPercent, '%')}</strong><small>100% equals one CPU core. First sample may be unavailable.</small></div>
          <div className="card"><span>Memory usage</span><strong>{metric(details?.MemoryMB, ' MiB')}</strong><small>Resident process memory · {server.MemoryMB} MiB configured heap limit</small></div>
          <div className="card"><span>Uptime</span><strong>{uptime(details?.UptimeSeconds)}</strong><small>Since the current process started</small></div>
          <div className="card"><span>Players online</span><strong>{details?.Players ?? 'Unavailable'}</strong><small>Player reporting is not available yet.</small></div>
        </div>
        <div className="card"><h2>Server information</h2><dl className="server-info"><dt>Software</dt><dd>{server.Software} {server.Version}{server.Build > 0 ? ` · build ${server.Build}` : ''}</dd><dt>Java runtime</dt><dd>{server.JavaPath}</dd><dt>Required Java</dt><dd>{server.RequiredJava} or newer</dd><dt>Process ID</dt><dd>{server.PID || 'Not running'}</dd><dt>Game port</dt><dd>{server.Port}</dd></dl><p className="muted">Metrics refresh every 2.5 seconds and are available only for processes managed by this daemon.</p>{server.State === 'Running' && <p className="join-hint">Join with your host’s IP or hostname and port {server.Port}. Use Minecraft {server.Version}.</p>}</div>
      </>}
      {tab === 'console' && <div className="card">
        <div className="panel-heading"><h2>Console & logs</h2><label className="checkbox"><input type="checkbox" checked={follow} onChange={e => setFollow(e.target.checked)} />Follow output</label></div>
        <p className="muted">Latest 64 KiB of process output. Logs refresh every 2.5 seconds and remain available after the process exits, until the next start or daemon restart.</p>
        <pre className="console-output" ref={logView} tabIndex={0} aria-label="Server log output">{logs || 'No output captured yet. Start this server to see its logs.'}</pre>
        <form className="command-form" onSubmit={event => void send(event)}><label>Console command<input value={command} onChange={e => setCommand(e.target.value)} maxLength={1024} required disabled={busy || server.State !== 'Running'} placeholder="e.g. say Hello, world!" /></label><button className="quiet" disabled={busy || server.State !== 'Running' || !command.trim()}>Send command</button></form>
        {server.State !== 'Running' && <p className="muted">Commands are available while the server is running.</p>}
      </div>}
      {tab === 'settings' && <div className="card"><h2>Server settings</h2><p>Stop the server before editing. Saved changes apply on the next start.</p>
        {settingsError && <p role="alert" className="error">{settingsError} <button className="quiet" onClick={() => setSettingsAttempt(n => n + 1)}>Reload settings</button></p>}
        {!settings ? <p>{settingsError ? 'Settings are unavailable until installation completes.' : 'Loading settings…'}</p> : <form onSubmit={event => void save(event)}><fieldset className="settings-fields" disabled={busy || !editable}>
          <label>Server message (MOTD)<input value={settings.MOTD} maxLength={200} onChange={e => setSettings({ ...settings, MOTD: e.target.value })} /></label>
          <div className="form-grid">
            <label>Maximum players<input type="number" min={1} max={1000} required value={settings.MaxPlayers} onChange={e => setSettings({ ...settings, MaxPlayers: Number(e.target.value) })} /></label>
            <label>View distance (chunks)<input type="number" min={2} max={32} required value={settings.ViewDistance} onChange={e => setSettings({ ...settings, ViewDistance: Number(e.target.value) })} /></label>
            <label>Difficulty<select value={settings.Difficulty} onChange={e => setSettings({ ...settings, Difficulty: e.target.value })}>{['peaceful', 'easy', 'normal', 'hard'].map(v => <option key={v}>{v}</option>)}</select></label>
            <label>Game mode<select value={settings.GameMode} onChange={e => setSettings({ ...settings, GameMode: e.target.value })}>{['survival', 'creative', 'adventure', 'spectator'].map(v => <option key={v}>{v}</option>)}</select></label>
          </div>
          <label className="checkbox"><input type="checkbox" checked={settings.PVP} onChange={e => setSettings({ ...settings, PVP: e.target.checked })} />Allow player versus player combat</label>
          <label className="checkbox"><input type="checkbox" checked={settings.WhiteList} onChange={e => setSettings({ ...settings, WhiteList: e.target.checked })} />Require allowlist</label>
          <button className="quiet">{busy ? 'Saving…' : 'Save settings'}</button>
        </fieldset></form>}
      </div>}
    </>}
  </section>;
}
