import { useState } from 'react';
import type { FormEvent } from 'react';
import { api } from './api';
import type { Server, Job, JavaRuntime, MinecraftVersion } from './api';

export default function ServerPanel({ servers, jobs, loaded, changed }: { servers: Server[]; jobs: Job[]; loaded: boolean; changed: () => Promise<void> }) {
  const [creating, setCreating] = useState(false);
  const [runtimes, setRuntimes] = useState<JavaRuntime[]>([]);
  const [versions, setVersions] = useState<MinecraftVersion[]>([]);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [notice, setNotice] = useState('');

  async function openCreate() {
    setCreating(true); setBusy(true); setError('');
    try {
      const [r, v] = await Promise.all([api.runtimes(), api.versions()]);
      setRuntimes(r); setVersions(v);
    } catch (err) { setError(err instanceof Error ? err.message : 'Unable to load available runtimes and versions.'); }
    finally { setBusy(false); }
  }

  async function create(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const data = new FormData(event.currentTarget);
    setBusy(true); setError(''); setNotice('');
    try {
      await api.createServer({ Name: String(data.get('name')), Software: String(data.get('software')), Version: String(data.get('version')), JavaPath: String(data.get('java')), MemoryMB: Number(data.get('memory')), Port: Number(data.get('port')), EULAAccepted: data.get('eula') === 'on' });
      setCreating(false); setNotice('Installation queued. Its progress will appear in recent jobs.');
      await changed();
    } catch (err) { setError(err instanceof Error ? err.message : 'Unable to create server.'); }
    finally { setBusy(false); }
  }

  async function control(id: number, action: 'Start' | 'Stop' | 'Restart' | 'RetryInstall') {
    setBusy(true); setError(''); setNotice('');
    try {
      await api.lifecycle(id, action);
      setNotice(`${action === 'RetryInstall' ? 'Installation retry' : action} queued.`);
      await changed();
    } catch (err) { setError(err instanceof Error ? err.message : 'Operation failed.'); }
    finally { setBusy(false); }
  }

  return <div className="card server-card">
    <div className="panel-heading"><h2>Your servers</h2><button className="quiet" disabled={busy} onClick={() => void openCreate()}>Create server</button></div>
    {error && <p className="error" role="alert">{error}</p>}
    {notice && <p role="status">{notice}</p>}
    {creating && <form className="create-form" onSubmit={event => void create(event)}>
      <h3>Create a Minecraft server</h3>
      {busy && <p role="status">Please wait…</p>}
      {!busy && runtimes.length === 0 && <p className="error">No Java runtime found. Install Java 21 or newer on the host, or configure CHUNKD_JAVA_PATHS, then reopen this form. Minecraft 26.1+ requires Java 25.</p>}
      <fieldset disabled={busy}>
        <div className="form-grid">
          <label>Server name<input name="name" required maxLength={100} placeholder="My survival world" /></label>
          <label>Server software<select name="software" defaultValue="vanilla"><option value="vanilla">Vanilla</option><option value="paper">Paper (stable builds only)</option></select></label>
          <label>Minecraft version<select name="version" required key={versions[0]?.ID}>{versions.map(v => <option key={v.ID}>{v.ID}</option>)}</select></label>
          <label>Java runtime<select name="java" required key={runtimes[0]?.Path}>{runtimes.map(r => <option value={r.Path} key={r.Path}>Java {r.Version} — {r.Path}</option>)}</select></label>
          <label>Maximum memory (MiB)<input name="memory" type="number" min={512} max={65536} step={512} defaultValue={2048} required /></label>
          <label>Game port<input name="port" type="number" min={1024} max={65535} defaultValue={25565} required /></label>
        </div>
        <p className="muted">Release versions from 1.20.5 onward. The selected Java runtime is checked before installation and each launch. Ports and memory must also be available on this host.</p>
        <label className="checkbox"><input name="eula" type="checkbox" required /> <span>I have read and accept the <a href="https://www.minecraft.net/eula" target="_blank" rel="noreferrer">Minecraft EULA</a> for this server.</span></label>
        <div className="actions"><button type="submit" className="primary" disabled={busy || runtimes.length === 0 || versions.length === 0}>Install server</button><button type="button" className="quiet" onClick={() => setCreating(false)}>Cancel</button></div>
      </fieldset>
    </form>}
    {!loaded ? <p role="status">Loading server data…</p> : servers.length === 0 ? <div className="empty"><span className="empty-block" aria-hidden="true">▧</span><h3>Your first world starts here.</h3><p>There are no Minecraft servers on this host yet.</p><p className="muted">Create a Vanilla or Paper server to get started.</p></div> : <ul className="server-list">{servers.map(s => {
      const pending = jobs.some(j => j.ServerID === s.ID && (j.State === 'Queued' || j.State === 'Running'));
      return <li key={s.ID} className="server-row">
        <div className="panel-heading"><h3>{s.Name}</h3><span className={`state state-${s.State.toLowerCase()}`}>{s.State}</span></div>
        <p>{s.Software} {s.Version}{s.Build > 0 ? ` · build ${s.Build}` : ''} · {s.MemoryMB} MiB · port <strong>{s.Port}</strong></p>
        {s.State === 'Running' && <p className="join-hint">Join with your host’s IP or hostname and port {s.Port}. Use the same Minecraft version.</p>}
        {s.LastError && <p className="error">{s.LastError}</p>}
        <div className="actions">
          {['Stopped', 'Crashed'].includes(s.State) && <button className="quiet" disabled={busy || pending} onClick={() => void control(s.ID, 'Start')}>Start {s.Name}</button>}
          {['Running', 'Starting'].includes(s.State) && <button className="quiet" disabled={busy || pending} onClick={() => void control(s.ID, 'Stop')}>Stop {s.Name}</button>}
          {s.State === 'Running' && <button className="quiet" disabled={busy || pending} onClick={() => void control(s.ID, 'Restart')}>Restart {s.Name}</button>}
          {s.State === 'Failed' && <button className="quiet" disabled={busy || pending} onClick={() => void control(s.ID, 'RetryInstall')}>Retry installation</button>}
          {pending && <span className="muted">Operation in progress…</span>}
        </div>
      </li>;
    })}</ul>}
  </div>;
}
