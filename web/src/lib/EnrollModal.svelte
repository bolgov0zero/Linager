<script lang="ts">
  import { onMount } from 'svelte';
  import { api, ApiError, isUnauthorized, type Enrollment } from '../api';
  import { t, tr } from '../i18n';
  import Modal from './Modal.svelte';
  import { showToast } from './toast';

  let {
    onclose,
    onchanged,
    onauthlost,
  }: {
    onclose: () => void;
    onchanged: () => void;
    onauthlost: () => void;
  } = $props();

  const POLL_MS = 3000;
  const KEY_LENGTH = 9; // XXXX-XXXX

  let items = $state<Enrollment[]>([]);
  let loaded = $state(false);
  let keys = $state<Record<string, string>>({});
  let errors = $state<Record<string, string>>({});
  let busy = $state<Record<string, boolean>>({});

  /** Uppercase, strip anything but A-Z/0-9, keep 8 chars and insert the dash after 4. */
  function formatKey(raw: string): string {
    const clean = raw.toUpperCase().replace(/[^A-Z0-9]/g, '').slice(0, 8);
    return clean.length > 4 ? `${clean.slice(0, 4)}-${clean.slice(4)}` : clean;
  }

  function onKeyInput(id: string, event: Event): void {
    const input = event.currentTarget as HTMLInputElement;
    const formatted = formatKey(input.value);
    input.value = formatted;
    keys[id] = formatted;
  }

  async function poll(): Promise<void> {
    try {
      items = await api.enrollments();
    } catch (e) {
      if (isUnauthorized(e)) onauthlost();
    } finally {
      loaded = true;
    }
  }

  async function approve(item: Enrollment): Promise<void> {
    const key = keys[item.id] ?? '';
    if (key.length !== KEY_LENGTH || busy[item.id]) return;
    busy[item.id] = true;
    errors[item.id] = '';
    try {
      await api.approveEnrollment(item.id, key);
      showToast(t('enroll.approved'));
      onchanged();
      await poll();
    } catch (e) {
      if (isUnauthorized(e)) {
        onauthlost();
        return;
      }
      if (e instanceof ApiError && e.status === 422) {
        errors[item.id] = t('enroll.invalidKey');
      } else if (e instanceof ApiError && e.status === 410) {
        showToast(t('enroll.tooManyAttempts'));
        await poll();
      } else {
        errors[item.id] = t('common.genericError');
      }
    } finally {
      busy[item.id] = false;
    }
  }

  async function reject(item: Enrollment): Promise<void> {
    if (busy[item.id]) return;
    busy[item.id] = true;
    errors[item.id] = '';
    try {
      await api.rejectEnrollment(item.id);
      showToast(t('enroll.rejected'));
      await poll();
    } catch (e) {
      if (isUnauthorized(e)) {
        onauthlost();
        return;
      }
      errors[item.id] = t('common.genericError');
    } finally {
      busy[item.id] = false;
    }
  }

  onMount(() => {
    void poll();
    const timer = setInterval(() => void poll(), POLL_MS);
    return () => clearInterval(timer);
  });
</script>

<Modal title={$tr('enroll.title')} labelId="enroll-title" {onclose}>
  <h3 class="section-title">{$tr('enroll.pending')}</h3>

  {#if !loaded}
    <p class="muted">{$tr('common.loading')}</p>
  {:else if items.length === 0}
    <p class="muted">{$tr('enroll.empty')}</p>
  {:else}
    <ul class="stack items">
      {#each items as item (item.id)}
        <li class="card item">
          <div class="item-head">
            <strong class="mono">{item.hostname}</strong>
            <span class="muted mono">{item.address}</span>
          </div>

          <dl class="meta">
            <dt>{$tr('enroll.fingerprint')}</dt>
            <dd class="mono">{item.key_fingerprint}</dd>
            <dt>{$tr('enroll.verifyCode')}</dt>
            <dd class="mono verify">{item.verify_code}</dd>
          </dl>
          <p class="hint">{$tr('enroll.verifyHint')}</p>

          <label class="field">
            <span class="label">{$tr('enroll.keyLabel')}</span>
            <input
              class="input mono"
              type="text"
              autocomplete="off"
              autocapitalize="characters"
              spellcheck="false"
              maxlength={KEY_LENGTH}
              placeholder={$tr('enroll.keyPlaceholder')}
              value={keys[item.id] ?? ''}
              oninput={(event) => onKeyInput(item.id, event)}
            />
          </label>

          {#if errors[item.id]}
            <p class="error" role="alert">{errors[item.id]}</p>
          {/if}

          <div class="row end">
            <button
              class="btn"
              type="button"
              disabled={busy[item.id]}
              onclick={() => void reject(item)}>{$tr('enroll.reject')}</button>
            <button
              class="btn primary"
              type="button"
              disabled={busy[item.id] || (keys[item.id] ?? '').length !== KEY_LENGTH}
              onclick={() => void approve(item)}>{$tr('enroll.approve')}</button>
          </div>
        </li>
      {/each}
    </ul>
  {/if}
</Modal>

<style>
  .section-title {
    margin: 0;
    font-size: 16px;
  }

  .items {
    margin: 0;
    padding: 0;
    list-style: none;
    gap: 16px;
  }

  .item {
    box-shadow: 3px 3px 0 var(--shade);
    padding: 12px;
    display: flex;
    flex-direction: column;
    gap: 10px;
  }

  .item-head {
    display: flex;
    flex-wrap: wrap;
    align-items: baseline;
    justify-content: space-between;
    gap: 4px 12px;
  }

  .meta {
    display: grid;
    grid-template-columns: auto 1fr;
    gap: 4px 12px;
    margin: 0;
  }

  .meta dt {
    color: var(--muted);
  }

  .meta dd {
    margin: 0;
    overflow-wrap: anywhere;
  }

  .verify {
    font-size: 18px;
    letter-spacing: 0.15em;
  }
</style>
