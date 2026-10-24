import { useEffect, useState } from 'react';
import type { FormEvent } from 'react';
import { api, ApiError } from './api';
import type { SessionInfo, Server, Job } from './api';

export default function App() {
  const [session, setSession] = useState<SessionInfo | null>(null);
  const [servers, setServers] = useState<Server[]>([]);
  const [jobs, setJobs] = useState<Job[]>([]);
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);
  const [loaded, setLoaded] = useState(false);
  const [notice, setNotice] = useState('');

  async function refresh() {
    setError('');
    setLoaded(false);
    try {
      const next = await api.session();
      setSession(next);
      if (next.Authenticated) {
        const [s, j] = await Promise.all([api.servers(), api.jobs()]);
        setServers(s.value); setJobs(j.value);
      } else { setServers([]); setJobs([]); }
      setLoaded(true);
    } catch (err) {
      if (err instanceof ApiError && err.status === 401) {
        setSession({ Authenticated: false, SetupRequired: false });
        setServers([]); setJobs([]); setLoaded(true);
      }
      setError(err instanceof Error ? err.message : 'Unable to connect to Chunkd.');
    }
  }

  useEffect(() => { void refresh(); }, []);

  async function signIn(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const form = event.currentTarget;
    const data = new FormData(form);
    const credentials = { Username: String(data.get('username')), Password: String(data.get('password')) };
    setBusy(true); setError(''); setNotice('');
    try {
      if (session?.SetupRequired) {
        await api.setup({ ...credentials, SetupToken: String(data.get('token')).trim() });
        // Keep setup success visible even if the following login fails.
        setSession({ Authenticated: false, SetupRequired: false });
        setNotice('Administrator configured.');
      }
      await api.login(credentials);
      form.reset();
      await refresh();
    } catch (err) { setError(err instanceof Error ? err.message : 'Sign in failed.'); }
    finally { setBusy(false); }
  }

  async function signOut() {
    setBusy(true); setError('');
    try {
      await api.logout(); setSession({ Authenticated: false, SetupRequired: false });
      setServers([]); setJobs([]); setNotice('');
    } catch (err) { setError(err instanceof Error ? err.message : 'Sign out failed.'); }
    finally { setBusy(false); }
  }

  return <div className="app">
    <header className="topbar">
      <a className="brand" href="/" aria-label="Chunkd home"><span className="block-mark" aria-hidden="true">▦</span> chunkd<span className="version">M1</span></a>
      <span className="host-label"><span className="dot" /> Your host. Your world.</span>
      {session?.Authenticated && <button className="quiet" disabled={busy} onClick={() => void signOut()}>Sign out</button>}
    </header>
    <main>
      {error && <div className="error" role="alert">{error} <button className="quiet" onClick={() => void refresh()}>Retry</button></div>}
      {notice && <p role="status">{notice}</p>}
      {!session ? <section className="intro"><p className="eyebrow">CHUNKD / CONTROL PANEL</p><h1>Welcome to your world.</h1><p>{error ? 'Check that the backend is running, then retry.' : 'Connecting to your host…'}</p></section> : !session.Authenticated ?
        <section className="login-layout">
          <div className="intro"><p className="eyebrow">MINECRAFT, ON YOUR TERMS</p><h1>A home for<br />your next world.</h1><p>Bring your servers together on one host.<br />Start with your local administrator login.</p><div className="terrain" aria-hidden="true"><i /><i /><i /><i /><i /><i /></div></div>
          <form className="card login" onSubmit={event => void signIn(event)}>
            <p className="eyebrow">{session.SetupRequired ? 'FIRST RUN' : 'WELCOME BACK'}</p>
            <h2>{session.SetupRequired ? 'Set up your administrator' : 'Sign in to Chunkd'}</h2>
            <p>{session.SetupRequired ? 'One administrator for this host.' : 'Use your local administrator credentials.'}</p>
            {session.SetupRequired && <label>Setup token<input name="token" required autoComplete="off" placeholder="From data/setup-token" /><small>Read the setup-token file in your configured data directory.</small></label>}
            <label>Username<input name="username" required maxLength={64} autoComplete="username" autoFocus /></label>
            <label>Password<input name="password" type="password" required minLength={session.SetupRequired ? 12 : undefined} maxLength={72} autoComplete={session.SetupRequired ? 'new-password' : 'current-password'} />{session.SetupRequired && <small>Use 12–72 bytes. Longer passphrases are encouraged.</small>}</label>
            <button className="primary" disabled={busy}>{busy ? 'Please wait…' : session.SetupRequired ? 'Set up and sign in' : 'Sign in'}</button>
          </form>
        </section> : <section className="dashboard">
          <div className="section-heading"><div><p className="eyebrow">YOUR CONTROL PANEL</p><h1>Overview</h1><p>A little space. Endless possibilities.</p></div><button className="quiet" disabled={busy} onClick={() => void refresh()}>Refresh</button></div>
          <div className="stats"><div className="card"><span>Servers in view</span><strong>{loaded ? servers.length : '—'}</strong></div><div className="card"><span>Recent jobs</span><strong>{loaded ? jobs.length : '—'}</strong></div><div className="card"><span>Workspace</span><strong className="status">Foundation</strong></div></div>
          <div className="card server-card"><h2>Your servers</h2>{!loaded ? <p role="status">Loading server data…</p> : servers.length === 0 ? <div className="empty"><span className="empty-block" aria-hidden="true">▧</span><h3>Your first world starts here.</h3><p>There are no Minecraft servers on this host yet.</p><p className="muted">Server creation and native Java controls arrive in M2.</p></div> : <ul className="items">{servers.map(s => <li key={s.ID}><strong>{s.Name}</strong><span>{s.State}</span></li>)}</ul>}</div>
          <div className="card"><h2>Recent jobs</h2>{!loaded ? <p>Loading jobs…</p> : jobs.length === 0 ? <p className="muted">No activity yet. Future server operations will appear here.</p> : <ul className="items">{jobs.map(j => <li key={j.ID}><strong>{j.Kind}</strong><span>{j.State}</span></li>)}</ul>}</div>
        </section>}
    </main>
    <footer>CHUNKD <span>Single host · Minecraft Java Edition</span></footer>
  </div>;
}
