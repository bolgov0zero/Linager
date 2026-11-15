<script lang="ts">
  import { onMount } from 'svelte';
  import Select from './ui/Select.svelte';
  import { api, type AuditItem } from '../api';
  import { tr } from '../i18n';
  import AuditTable from './audit/AuditTable.svelte';
  import { ACTION_GROUPS, RESULT_OPTIONS } from './audit/actions';

  let { onBack, embedded = false }: { onBack?: () => void; embedded?: boolean } = $props();

  const PAGE_SIZE = 100;
  const REFRESH_MS = 15_000;
  const TEXT_DEBOUNCE_MS = 400;

  // Filter controls (what the user sees) and the applied text filters (what requests use).
  let result = $state('');
  let group = $state('');
  let search = $state('');
  let actor = $state('');
  let appliedSearch = $state('');
  let appliedActor = $state('');

  let items = $state<AuditItem[]>([]);
  let nextBefore = $state(0);
  // Number of pages loaded into `items` since the last reset; auto-refresh only runs while this is 1.
  let pages = $state(0);
  let loading = $state(false);
  let loaded = $state(false);
  let failed = $state(false);

  // Only the newest request may update state, so stale responses are ignored.
  let requestSeq = 0;
  let textTimer: ReturnType<typeof setTimeout> | undefined;

  async function load(reset: boolean): Promise<void> {
    if (!reset && nextBefore <= 0) return;
    const id = ++requestSeq;
    loading = true;
    try {
      const page = await api.audit({
        before: reset ? 0 : nextBefore,
        limit: PAGE_SIZE,
        action: group,
        actor: appliedActor,
        result,
        q: appliedSearch,
      });
      if (id !== requestSeq) return;
      items = reset ? page.items : [...items, ...page.items];
      nextBefore = page.next_before;
      pages = reset ? 1 : pages + 1;
      failed = false;
    } catch {
      if (id === requestSeq) failed = true;
    } finally {
      if (id === requestSeq) {
        loading = false;
        loaded = true;
      }
    }
  }

  function onTextInput(): void {
    clearTimeout(textTimer);
    textTimer = setTimeout(() => {
      appliedSearch = search.trim();
      appliedActor = actor.trim();
      void load(true);
    }, TEXT_DEBOUNCE_MS);
  }

  function tick(): void {
    // Refresh page 1 only, and only if nothing is in flight. Loaded older rows are never dropped.
    if (pages !== 1 || loading) return;
    void load(true);
  }

  onMount(() => {
    void load(true);
    const timer = setInterval(tick, REFRESH_MS);
    return () => {
      clearInterval(timer);
      clearTimeout(textTimer);
      requestSeq++;
    };
  });
</script>

<div class="audit stack">
  <div class="head row">
    {#if !embedded && onBack}
      <button class="btn" type="button" onclick={onBack}>{$tr('audit.back')}</button>
    {/if}
    {#if embedded}<h3 class="emb">{$tr('audit.title')}</h3>{:else}<h1>{$tr('audit.title')}</h1>{/if}
  </div>

  <section class="card stack">
    <div class="filters row">
      <label class="field">
        <span class="label small">{$tr('audit.filter.result')}</span>
        <Select
          value={result}
          options={RESULT_OPTIONS.map((o) => ({ value: o.value, label: $tr(o.key) }))}
          onchange={(v) => {
            result = v;
            void load(true);
          }}
        />
      </label>

      <label class="field">
        <span class="label small">{$tr('audit.filter.group')}</span>
        <Select
          value={group}
          options={ACTION_GROUPS.map((o) => ({ value: o.value, label: $tr(o.key) }))}
          onchange={(v) => {
            group = v;
            void load(true);
          }}
        />
      </label>

      <label class="field grow">
        <span class="label small">{$tr('audit.filter.search')}</span>
        <input
          class="input"
          type="search"
          value={search}
          placeholder={$tr('audit.filter.searchPlaceholder')}
          oninput={(e) => {
            search = e.currentTarget.value;
            onTextInput();
          }}
        />
      </label>

      <label class="field">
        <span class="label small">{$tr('audit.filter.actor')}</span>
        <input
          class="input"
          type="text"
          value={actor}
          placeholder={$tr('audit.filter.actorPlaceholder')}
          oninput={(e) => {
            actor = e.currentTarget.value;
            onTextInput();
          }}
        />
      </label>

      <button class="btn" type="button" disabled={loading} onclick={() => load(true)}>
        {$tr('audit.refresh')}
      </button>
    </div>

    {#if failed}
      <p class="error">{$tr('audit.loadFailed')}</p>
    {/if}

    {#if !loaded}
      <p class="note muted">{$tr('audit.loading')}</p>
    {:else if items.length === 0}
      <p class="note muted">{failed ? '' : $tr('audit.empty')}</p>
    {:else}
      <AuditTable {items} />
    {/if}

    {#if nextBefore > 0}
      <div class="row end">
        <button class="btn" type="button" disabled={loading} onclick={() => load(false)}>
          {$tr('audit.more')}
        </button>
      </div>
    {/if}
  </section>
</div>

<style>
  .audit {
    min-width: 0;
  }

  .head h1 {
    margin: 0;
    font-variation-settings: "ELSH" 40;
    font-size: 20px;
  }

  .filters {
    align-items: flex-end;
  }

  .field {
    flex: 1 1 150px;
    min-width: 0;
  }

  .field.grow {
    flex-basis: 220px;
  }

  .filters > .btn {
    margin-bottom: 0;
  }

  .note {
    margin: 0;
    padding: 24px 0;
    text-align: center;
  }

  .small {
    font-size: 12px;
  }

  .muted {
    color: var(--muted);
  }
  .emb {
    margin: 0;
  }
</style>
