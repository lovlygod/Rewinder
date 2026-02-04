<script lang="ts">
  import { onMount } from 'svelte';
  import type { ipcapi } from '../wailsjs/go/models';
  import { GetApps, GetTimeline, Restore, PauseTracking, ResumeTracking, ShowTimelineWindow, OverlayEnd, ExitApp, GetAutostart, SetAutostart } from '../wailsjs/go/main/App.js';
  import { EventsOn, WindowHide, WindowUnfullscreen, WindowSetAlwaysOnTop } from '../wailsjs/runtime/runtime.js';

  type View = 'timeline' | 'overlay' | 'settings';

  let view: View = 'timeline';
  let trackingState: 'active' | 'paused' | 'error' = 'active';

  let apps: ipcapi.AppSummary[] = [];
  let selectedAppID: string | null = null;
  let timeline: ipcapi.SnapshotMeta[] = [];
  let selectedSnapshotID: string | null = null;

  let overlaySnapshots: ipcapi.SnapshotMeta[] = [];
  let overlayIndex = 0;
  let overlayAppID: string | null = null;

  let autostartEnabled = false;

  type Toast = { id: string; kind: 'info' | 'success' | 'error'; text: string };
  let toasts: Toast[] = [];
  let lastSnapshotToast = 0;

  type SnapshotCreatedEvent = { appID: string; snapshot: ipcapi.SnapshotMeta; occurredAtUTC: number };
  type RestoreErrorEvent = { appID: string; snapshotID: string; error: string };
  type TrackingStateChangedEvent = { appID?: string | null; state: string; reason?: string; atUTC: number };

  function pushToast(kind: Toast['kind'], text: string) {
    const id = crypto.randomUUID();
    toasts = [...toasts, { id, kind, text }];
    if (toasts.length > 5) {
      toasts = toasts.slice(-5);
    }
    setTimeout(() => {
      toasts = toasts.filter(t => t.id !== id);
    }, 2000); 
  }

  async function refreshApps() {
    try {
      apps = await GetApps();
      if (!selectedAppID && apps.length) {
        selectedAppID = apps[0].appID;
      }
      if (selectedAppID) await refreshTimeline(selectedAppID);
    } catch (e: any) {
      pushToast('error', `GetApps failed: ${e?.message ?? e}`);
    }
  }

  async function refreshTimeline(appID: string) {
    try {
      timeline = await GetTimeline(appID);
      selectedSnapshotID = timeline[0]?.snapshotID ?? null;
    } catch (e: any) {
      pushToast('error', `GetTimeline failed: ${e?.message ?? e}`);
    }
  }

  function openOverlay(appID?: string) {
    overlayAppID = appID ?? (apps[0]?.appID ?? null);
    if (overlayAppID && overlayAppID !== selectedAppID) {
      GetTimeline(overlayAppID).then((snapshots) => {
        overlaySnapshots = snapshots;
        overlayIndex = 0;
        view = 'overlay';
      });
    } else {
      overlaySnapshots = overlayAppID ? (timeline.filter(t => t.appID === overlayAppID)) : [];
      overlayIndex = 0;
      view = 'overlay';
    }
  }

  async function doRestore(appID: string, snapshotID: string) {
    try {
      pushToast('info', 'Restoring…');
      await Restore(appID, snapshotID);
      pushToast('success', 'Restore completed');
    } catch (e: any) {
      pushToast('error', `Restore failed: ${e?.message ?? e}`);
    }
  }

  function closeOverlay() {
    view = 'timeline';
    void OverlayEnd();
    WindowSetAlwaysOnTop(false);
    WindowUnfullscreen();
    WindowHide();
  }

  function handleOverlayKey(e: KeyboardEvent) {
    if (view !== 'overlay') return;
    if (e.key === 'Escape') {
      e.preventDefault();
      closeOverlay();
      return;
    }
    if (e.key === 'ArrowLeft') {
      e.preventDefault();
      overlayIndex = Math.max(0, overlayIndex - 1);
      return;
    }
    if (e.key === 'ArrowRight') {
      e.preventDefault();
      overlayIndex = Math.min(overlaySnapshots.length - 1, overlayIndex + 1);
      return;
    }
    if (e.key === 'Enter') {
      e.preventDefault();
      const snap = overlaySnapshots[overlayIndex];
      if (overlayAppID && snap?.snapshotID) {
        void doRestore(overlayAppID, snap.snapshotID);
        closeOverlay();
      }
    }
  }

  async function toggleAutostart() {
    try {
      await SetAutostart(!autostartEnabled);
      autostartEnabled = !autostartEnabled;
      pushToast('success', autostartEnabled ? 'Autostart enabled' : 'Autostart disabled');
    } catch (e: any) {
      pushToast('error', `Failed to change autostart: ${e?.message ?? e}`);
    }
  }

  async function exitApplication() {
    try {
      await ExitApp();
    } catch (e: any) {
      pushToast('error', `Failed to exit: ${e?.message ?? e}`);
    }
  }

  async function loadSettings() {
    try {
      autostartEnabled = await GetAutostart();
    } catch (e: any) {
      pushToast('error', `Failed to load settings: ${e?.message ?? e}`);
    }
  }

  function handleOverlayWheel(e: WheelEvent) {
    if (view !== 'overlay') return;
    e.preventDefault();
    overlayIndex = Math.max(0, Math.min(overlaySnapshots.length - 1, overlayIndex + (e.deltaY > 0 ? 1 : -1)));
  }

  onMount(() => {
   const off1 = EventsOn('onSnapshotCreated', (payload: SnapshotCreatedEvent) => {
     const now = Date.now();
     if (now - lastSnapshotToast > 5000) {
       pushToast('info', `Snapshot: ${payload.appID.split(':')[0]}`);
       lastSnapshotToast = now;
     }
     if (selectedAppID === payload.appID) {
       timeline = [payload.snapshot, ...timeline];
       selectedSnapshotID = payload.snapshot.snapshotID;
       const appIndex = apps.findIndex(app => app.appID === payload.appID);
       if (appIndex !== -1) {
         apps[appIndex].snapshotCount += 1;
         apps = [...apps];
       }
       void refreshTimeline(payload.appID);
     } else {
       const appIndex = apps.findIndex(app => app.appID === payload.appID);
       if (appIndex !== -1) {
         apps[appIndex].snapshotCount += 1;
         apps = [...apps];
       }
       void refreshApps();
     }
   });
    const off3 = EventsOn('onRestoreError', (p: RestoreErrorEvent) => {
      pushToast('error', p.error);
    });
    const off4 = EventsOn('onTrackingStateChanged', (p: TrackingStateChangedEvent) => {
      trackingState = (p.state as any) ?? 'active';
    });
    const off5 = EventsOn('onOverlayOpenRequested', () => {
      void refreshApps().then(() => openOverlay());
    });
    const off6 = EventsOn('onShowTimelineRequested', () => {
      view = 'timeline';
      void refreshApps();
    });
    const off7 = EventsOn('onShowSettingsRequested', () => {
      view = 'settings';
      void loadSettings();
    });

    window.addEventListener('keydown', handleOverlayKey, { capture: true });
    window.addEventListener('wheel', handleOverlayWheel, { passive: false });
    void refreshApps();

    return () => {
      off1?.(); off3?.(); off4?.(); off5?.(); off6?.(); off7?.();
      window.removeEventListener('keydown', handleOverlayKey, { capture: true } as any);
      window.removeEventListener('wheel', handleOverlayWheel as any);
    };
  });
</script>

{#if view === 'overlay'}
  <div class="overlay" role="dialog" aria-label="Quick restore overlay">
    <div class="overlayCard">
      <div class="overlayHeader">
        <div class="title">Restore</div>
        <div class="hint">Scroll = time • ← → = step • Enter = restore • Esc = cancel</div>
      </div>

      <div class="axis">
        {#if overlaySnapshots.length === 0}
          <div class="empty">No snapshots yet</div>
        {:else}
          <div class="axisBar">
            {#each overlaySnapshots as s, i (s.snapshotID)}
              <button
                class="tick {i === overlayIndex ? 'active' : ''}"
                on:click={() => (overlayIndex = i)}
                aria-label={"Snapshot " + new Date(s.timestampUTC).toLocaleString()}
              />
            {/each}
          </div>
          <div class="preview">
            <div class="when">{new Date(overlaySnapshots[overlayIndex].timestampUTC).toLocaleString()}</div>
            <div class="meta">
              <span>{overlaySnapshots[overlayIndex].windowsCount} window diffs</span>
              <span>{overlaySnapshots[overlayIndex].filesAdded}+ / {overlaySnapshots[overlayIndex].filesRemoved}- files</span>
            </div>
          </div>
          <div class="actions">
            <button class="btn ghost" on:click={closeOverlay}>Cancel</button>
            <button
              class="btn primary"
              on:click={() => overlayAppID && doRestore(overlayAppID, overlaySnapshots[overlayIndex].snapshotID).then(closeOverlay)}
            >
              Restore
            </button>
          </div>
        {/if}
      </div>
    </div>
  </div>
{:else}
  <div class="appShell">
    <header class="topbar">
      <div class="brand">
        <div class="dot {trackingState}"></div>
        <div class="name">App Time Machine</div>
      </div>
      <div class="topActions">
        <button class="btn ghost" on:click={() => (view = 'timeline')}>Timeline</button>
        <button class="btn ghost" on:click={() => (view = 'settings')}>Settings</button>
        <button class="btn ghost" on:click={() => ShowTimelineWindow()}>Show Window</button>
      </div>
    </header>

    {#if view === 'timeline'}
      <div class="layout">
        <aside class="sidebar" aria-label="Applications list">
          <div class="sidebarHeader">
            <div class="h">Apps</div>
            <button class="btn small" on:click={refreshApps}>Refresh</button>
          </div>
          <div class="cards">
            {#each apps as a (a.appID)}
              <button
                class="card {selectedAppID === a.appID ? 'active' : ''}"
                on:click={() => { selectedAppID = a.appID; void refreshTimeline(a.appID); }}
              >
                <div class="cardTitle">{a.name}</div>
                <div class="cardMeta">
                  <span>{a.snapshotCount} snapshots</span>
                  <span>{new Date(a.lastActivityUTC).toLocaleString()}</span>
                </div>
              </button>
            {/each}
            {#if apps.length === 0}
              <div class="empty">No tracked apps yet. Use your apps and snapshots will appear.</div>
            {/if}
          </div>
        </aside>

        <main class="main">
          <div class="mainHeader">
            <div class="h">Timeline</div>
            <div class="mainActions">
              <button class="btn ghost" on:click={() => selectedAppID && PauseTracking(selectedAppID)}>Pause app</button>
              <button class="btn ghost" on:click={() => selectedAppID && ResumeTracking(selectedAppID)}>Resume app</button>
              <button class="btn ghost" on:click={() => PauseTracking(null)}>Pause all</button>
              <button class="btn ghost" on:click={() => ResumeTracking(null)}>Resume all</button>
            </div>
          </div>

          <div class="timeline">
            {#if !selectedAppID}
              <div class="empty">Select an app</div>
            {:else if timeline.length === 0}
              <div class="empty">No snapshots for this app yet</div>
            {:else}
              <div class="snapList" role="list">
                {#each timeline as s (s.snapshotID)}
                  <button
                    class="snap {selectedSnapshotID === s.snapshotID ? 'active' : ''}"
                    on:click={() => (selectedSnapshotID = s.snapshotID)}
                  >
                    <div class="snapWhen">{new Date(s.timestampUTC).toLocaleString()}</div>
                    <div class="snapMeta">
                      <span>{s.windowsCount} diffs</span>
                      <span>{s.filesAdded}+ / {s.filesRemoved}-</span>
                    </div>
                  </button>
                {/each}
              </div>
              <div class="bottomBar">
                <button class="btn ghost" on:click={() => selectedAppID && openOverlay(selectedAppID)}>Quick overlay</button>
                <button
                  class="btn primary"
                  disabled={!selectedAppID || !selectedSnapshotID}
                  on:click={() => selectedAppID && selectedSnapshotID && doRestore(selectedAppID, selectedSnapshotID)}
                >
                  Restore selected
                </button>
              </div>
            {/if}
          </div>
        </main>
      </div>
    {:else}
      <div class="settings">
        <div class="settingsCard">
          <div class="h">Settings</div>
          <div class="row">
            <div class="label">Tracking</div>
            <div class="value">{trackingState}</div>
          </div>
          <div class="row">
            <div class="label">Hotkey</div>
            <div class="value">Ctrl + Alt + Z</div>
          </div>
          <div class="row">
            <div class="label">Autostart</div>
            <div class="value">
              <button class="btn {autostartEnabled ? 'primary' : 'ghost'}" on:click={toggleAutostart}>
                {autostartEnabled ? 'Enabled' : 'Disabled'}
              </button>
            </div>
          </div>
          <div class="row">
            <div class="label">Privacy</div>
            <div class="value">Clipboard hash only (no raw content stored)</div>
          </div>
          <div class="row">
            <div class="label">Application</div>
            <div class="value">
              <button class="btn ghost" on:click={exitApplication}>Exit Completely</button>
            </div>
          </div>
        </div>
      </div>
    {/if}
  </div>
{/if}

<div class="toasts" aria-live="polite">
  {#each toasts as t (t.id)}
    <div class="toast {t.kind}">{t.text}</div>
  {/each}
</div>

<style>
  :global(html), :global(body) {
    height: 100%;
  }

  .appShell {
    height: 100vh;
    display: flex;
    flex-direction: column;
    color: rgba(255,255,255,0.92);
  }

  .topbar {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 14px 16px;
    border-bottom: 1px solid rgba(255,255,255,0.08);
    background: rgba(0,0,0,0.12);
  }

  .brand { display: flex; gap: 10px; align-items: center; }
  .name { font-weight: 700; letter-spacing: 0.3px; }
  .dot { width: 10px; height: 10px; border-radius: 999px; background: #4ade80; box-shadow: 0 0 0 3px rgba(74,222,128,0.15); }
  .dot.paused { background: #fbbf24; box-shadow: 0 0 0 3px rgba(251,191,36,0.15); }
  .dot.error { background: #fb7185; box-shadow: 0 0 0 3px rgba(251,113,133,0.15); }

  .topActions { display: flex; gap: 8px; }

  .layout {
    flex: 1;
    display: grid;
    grid-template-columns: 320px 1fr;
    min-height: 0;
  }

  .sidebar {
    border-right: 1px solid rgba(255,255,255,0.08);
    padding: 12px;
    min-height: 0;
    overflow: auto;
  }

  .sidebarHeader {
    display: flex;
    align-items: center;
    justify-content: space-between;
    margin-bottom: 10px;
  }

  .cards { display: flex; flex-direction: column; gap: 8px; }
  .card {
    text-align: left;
    padding: 10px 10px;
    border-radius: 10px;
    border: 1px solid rgba(255,255,255,0.10);
    background: rgba(0,0,0,0.12);
    cursor: pointer;
  }
  .card.active {
    border-color: rgba(99,102,241,0.8);
    background: rgba(99,102,241,0.12);
  }
  .cardTitle { font-weight: 700; }
  .cardMeta { display: flex; justify-content: space-between; margin-top: 6px; opacity: 0.75; font-size: 12px; }

  .main {
    min-height: 0;
    overflow: hidden;
    display: flex;
    flex-direction: column;
  }

  .mainHeader {
    padding: 12px 14px;
    display: flex;
    align-items: center;
    justify-content: space-between;
    border-bottom: 1px solid rgba(255,255,255,0.08);
  }
  .mainActions { display: flex; gap: 8px; flex-wrap: wrap; justify-content: flex-end; }
  .h { font-weight: 800; letter-spacing: 0.3px; }

  .timeline {
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
    padding: 12px;
  }

  .snapList {
    flex: 1;
    min-height: 0;
    overflow: auto;
    display: flex;
    flex-direction: column;
    gap: 8px;
  }
  .snap {
    text-align: left;
    padding: 10px;
    border-radius: 12px;
    border: 1px solid rgba(255,255,255,0.10);
    background: rgba(0,0,0,0.10);
    cursor: pointer;
  }
  .snap.active {
    border-color: rgba(59,130,246,0.85);
    background: rgba(59,130,246,0.12);
  }
  .snapWhen { font-weight: 700; }
  .snapMeta { opacity: 0.75; font-size: 12px; display: flex; gap: 12px; margin-top: 6px; }

  .bottomBar {
    display: flex;
    justify-content: space-between;
    gap: 10px;
    padding-top: 10px;
    border-top: 1px solid rgba(255,255,255,0.08);
  }

  .btn {
    border: 1px solid rgba(255,255,255,0.14);
    background: rgba(255,255,255,0.06);
    color: rgba(255,255,255,0.92);
    border-radius: 10px;
    padding: 8px 10px;
    cursor: pointer;
  }
  .btn.small { padding: 6px 8px; border-radius: 9px; font-size: 12px; }
  .btn.ghost { background: transparent; }
  .btn.primary {
    border-color: rgba(99,102,241,0.85);
    background: rgba(99,102,241,0.20);
  }
  .btn:disabled { opacity: 0.5; cursor: not-allowed; }

  .empty {
    opacity: 0.7;
    padding: 14px;
    border: 1px dashed rgba(255,255,255,0.18);
    border-radius: 12px;
  }

  .settings { padding: 20px; }
  .settingsCard {
    margin: 0 auto;
    max-width: 680px;
    border: 1px solid rgba(255,255,255,0.10);
    background: rgba(0,0,0,0.12);
    border-radius: 14px;
    padding: 14px;
    text-align: left;
  }
  .row { display: grid; grid-template-columns: 160px 1fr; gap: 12px; padding: 10px 0; border-top: 1px solid rgba(255,255,255,0.08); }
  .row:first-of-type { border-top: none; }
  .label { opacity: 0.75; }

  .overlay {
    position: fixed;
    inset: 0;
    display: grid;
    place-items: center;
    background: rgba(0,0,0,0.55);
    backdrop-filter: blur(6px);
  }
  .overlayCard {
    width: min(980px, calc(100vw - 32px));
    border-radius: 16px;
    border: 1px solid rgba(255,255,255,0.14);
    background: rgba(20,20,24,0.92);
    padding: 16px;
    text-align: left;
  }
  .overlayHeader { display: flex; justify-content: space-between; align-items: baseline; gap: 12px; }
  .title { font-weight: 900; font-size: 18px; }
  .hint { opacity: 0.7; font-size: 12px; }
  .axis { margin-top: 14px; }
  .axisBar {
    position: relative;
    display: flex;
    gap: 6px;
    padding: 10px;
    border-radius: 12px;
    border: 1px solid rgba(255,255,255,0.10);
    background: rgba(255,255,255,0.05);
    overflow: auto;
  }
  .tick {
    min-width: 14px;
    height: 14px;
    border-radius: 999px;
    border: 1px solid rgba(255,255,255,0.18);
    background: rgba(255,255,255,0.06);
    cursor: pointer;
  }
  .tick.active {
    border-color: rgba(59,130,246,0.9);
    background: rgba(59,130,246,0.35);
  }
  .preview { margin-top: 12px; display: flex; justify-content: space-between; align-items: baseline; }
  .when { font-weight: 800; }
  .meta { opacity: 0.75; font-size: 12px; display: flex; gap: 12px; }
  .actions { margin-top: 14px; display: flex; justify-content: flex-end; gap: 10px; }

  .toasts {
    position: fixed;
    top: 14px;
    right: 14px;
    display: flex;
    flex-direction: column;
    gap: 6px;
    z-index: 1000;
    max-height: 200px;
    overflow: hidden;
  }
  .toast {
    width: min(300px, calc(100vw - 28px));
    border-radius: 8px;
    border: 1px solid rgba(255,255,255,0.14);
    background: rgba(20,20,24,0.95);
    padding: 6px 10px;
    text-align: left;
    font-size: 12px;
    backdrop-filter: blur(8px);
  }
  .toast.info { border-color: rgba(255,255,255,0.14); }
  .toast.success { border-color: rgba(74,222,128,0.5); }
  .toast.error { border-color: rgba(251,113,133,0.6); }
</style>
