<script lang="ts">
  import PixIcon from '../../ui/PixIcon.svelte';
  import { tick, untrack } from 'svelte';
  import { get } from 'svelte/store';
  import { api, type DockerContainer, type DockerEditConfig } from '../../../api';
  import { locale, t, tr as tStore, type Vars } from '../../../i18n';
  import Modal from '../../Modal.svelte';
  import Select from '../../ui/Select.svelte';
  import { isPhone } from '../../ui/viewport';
  import { formatBytes } from '../format';
  import { errorText } from './docker-util';
  import {
    cloneDraft,
    diffSpec,
    envText,
    isSecretName,
    mountText,
    newEnv,
    newMount,
    newPort,
    tabOfPath,
    toDraft,
    toSpec,
    usedPorts,
    validateSpec,
    type Draft,
    type DiffWords,
    type EditTab,
  } from './docker-edit';

  let {
    serverId,
    container,
    config,
    others,
    onclose,
    onapply,
  }: {
    serverId: string;
    container: DockerContainer;
    config: DockerEditConfig;
    /** The other containers of the server (for the host ports that are taken). */
    others: DockerContainer[];
    onclose: () => void;
    /** The change was accepted; the token opens the `docker-edit` stream. */
    onapply: (token: string) => void;
  } = $props();

  const TABS: EditTab[] = ['main', 'ports', 'mounts', 'env', 'res'];
  const RESTART = ['no', 'always', 'unless-stopped', 'on-failure'] as const;

  // The dialog edits a copy of the settings it was opened with; later changes of the props do not reach it.
  const orig: Draft = untrack(() => toDraft(config));
  const compose = untrack(() => config.compose);
  const running = untrack(() => config.running);
  let draft = $state<Draft>(cloneDraft(orig));

  let tab = $state<EditTab>('main');
  let phase = $state<'edit' | 'review'>('edit');
  let confirmDiscard = $state(false);
  let applying = $state(false);
  let applyError = $state('');
  let reveal = $state<Record<number, boolean>>({});

  let nextBtn = $state<HTMLButtonElement | undefined>(undefined);
  let applyBtn = $state<HTMLButtonElement | undefined>(undefined);
  let keepBtn = $state<HTMLButtonElement | undefined>(undefined);

  const lang = $derived($locale);
  function T(key: string, vars?: Vars): string {
    return t(key, get(locale), vars);
  }

  const words = $derived<DiffWords>({
    asImage: $tStore('dockerEdit.words.asImage'),
    unlimited: $tStore('dockerEdit.words.unlimited'),
    mb: $tStore('dockerEdit.words.mb'),
    cores: $tStore('dockerEdit.words.cores'),
    readOnly: $tStore('dockerEdit.words.readOnly'),
    restart: { no: 'no', always: 'always', 'unless-stopped': 'unless-stopped', 'on-failure': 'on-failure' },
  });
  const diff = $derived(diffSpec(orig, draft, words));
  const taken = $derived(usedPorts(others));
  const errors = $derived.by(() => {
    return validateSpec(draft, { orig, compose: !!compose, others: taken, t: (key, vars) => t(key, lang, vars) });
  });
  const firstError = $derived(Object.values(errors)[0] ?? '');
  const count = $derived(diff.changes.length);
  const errorTabs = $derived(new Set(Object.keys(errors).map(tabOfPath)));
  const restartOptions = $derived(RESTART.map((value) => ({ value, label: $tStore(`dockerEdit.restart.${value}`) })));
  const protoOptions = [
    { value: 'tcp' as const, label: 'tcp' },
    { value: 'udp' as const, label: 'udp' },
  ];
  const mountTypes = $derived([
    { value: 'bind' as const, label: $tStore('dockerEdit.mount.bind') },
    { value: 'volume' as const, label: $tStore('dockerEdit.mount.volume') },
  ]);

  // Rows that exist in the original are drawn plain, new ones get an accent border.
  const origIds = new Set<number>([...orig.ports, ...orig.mounts, ...orig.env].map((r) => r._id));

  // Any edit takes back the "close without applying?" question.
  const signature = $derived(JSON.stringify(draft));
  $effect(() => {
    void signature;
    untrack(() => {
      confirmDiscard = false;
    });
  });

  function dirty(id: EditTab): boolean {
    return !!diff.tabs[id] || errorTabs.has(id);
  }

  function invalid(path: string): 'true' | undefined {
    return errors[path] ? 'true' : undefined;
  }

  function selectTab(id: EditTab): void {
    tab = id;
    confirmDiscard = false;
  }

  async function addRow(kind: 'ports' | 'mounts' | 'env'): Promise<void> {
    let row: { _id: number };
    if (kind === 'ports') draft.ports.push((row = newPort()));
    else if (kind === 'mounts') draft.mounts.push((row = newMount()));
    else draft.env.push((row = newEnv()));
    await tick();
    const first = kind === 'ports' ? 'host' : kind === 'mounts' ? 'src' : 'k';
    document.getElementById(`de-${kind}-${row._id}-${first}`)?.focus();
  }

  async function removeRow(kind: 'ports' | 'mounts' | 'env', id: number): Promise<void> {
    if (kind === 'ports') draft.ports = draft.ports.filter((r) => r._id !== id);
    else if (kind === 'mounts') draft.mounts = draft.mounts.filter((r) => r._id !== id);
    else draft.env = draft.env.filter((r) => r._id !== id);
    await tick();
    document.getElementById(`de-add-${kind}`)?.focus();
  }

  function tryClose(): void {
    if (applying) return;
    if (count > 0 && !confirmDiscard) {
      confirmDiscard = true;
      void tick().then(() => keepBtn?.focus());
      return;
    }
    onclose();
  }

  function onEscape(): void {
    if (applying) return;
    if (confirmDiscard) confirmDiscard = false;
    else tryClose();
  }

  async function goReview(): Promise<void> {
    if (count === 0 || firstError) return;
    phase = 'review';
    applyError = '';
    await tick();
    applyBtn?.focus();
  }

  async function goBack(): Promise<void> {
    phase = 'edit';
    applyError = '';
    await tick();
    document.getElementById(`de-tab-${tab}`)?.focus();
  }

  async function apply(): Promise<void> {
    if (applying || firstError) return;
    applying = true;
    applyError = '';
    try {
      const res = await api.dockerEdit(serverId, container.id, toSpec(draft));
      onapply(res.token);
    } catch (e) {
      applyError = errorText(e, T);
    } finally {
      applying = false;
    }
  }
</script>

{#snippet tag(recreate: boolean)}
  <span class="hl" class:re={recreate} class:lv={!recreate}>{$tStore(recreate ? 'dockerEdit.tag.recreate' : 'dockerEdit.tag.live')}</span>
{/snippet}

{#snippet info()}
  <div class="info">
    {#if confirmDiscard}
      <span class="err">{$tStore('dockerEdit.foot.discardText', { n: count })}</span>
    {:else if phase === 'edit'}
      <span class="mode">
        {#if count === 0}
          <span class="muted">{$tStore('dockerEdit.foot.none')}</span>
        {:else}
          <span class="tg acc">{$tStore('dockerEdit.foot.count', { n: count })}</span>
          {#if diff.recreate}
            <span class="tg warn">{$tStore('dockerEdit.foot.recreate')}</span>
          {:else}
            <span class="tg ok">{$tStore('dockerEdit.foot.live')}</span>
          {/if}
        {/if}
      </span>
      {#if firstError}<span class="err">{firstError}</span>{/if}
    {:else}
      <span class="mode">
        {#if diff.recreate}
          <span class="tg warn">{$tStore('dockerEdit.tag.recreate')}</span>
        {:else}
          <span class="tg ok">{$tStore('dockerEdit.foot.live')}</span>
        {/if}
        <span class="small muted">{$tStore('dockerEdit.review.audit')}</span>
      </span>
    {/if}
  </div>
{/snippet}

{#snippet applyErr()}
  {#if applyError}<p class="error" role="alert">{applyError}</p>{/if}
{/snippet}

<Modal
  title={phase === 'review' ? $tStore('dockerEdit.reviewTitle') : $tStore('dockerEdit.title', { name: container.name })}
  labelId="docker-edit-title"
  onclose={tryClose}
  onescape={onEscape}
  medium={!$isPhone}
  wide={$isPhone}
>
  <p class="sub small muted mono">{container.image}</p>

  {#if phase === 'edit'}
    <div class="tabs" role="group" aria-label={$tStore('dockerEdit.tabs.label')}>
      {#each TABS as id (id)}
        <button class="chip" type="button" id="de-tab-{id}" aria-pressed={tab === id} class:dirty={dirty(id)} onclick={() => selectTab(id)}>
          {$tStore(`dockerEdit.tabs.${id}`)}
          {#if dirty(id)}<i class="dot" aria-hidden="true"></i><span class="sr-only">({$tStore('dockerEdit.tabs.changed')})</span>{/if}
        </button>
      {/each}
    </div>

    <div class="pane">
      {#if compose && tab !== 'main' && tab !== 'res'}
        <p class="notice info" role="note">{$tStore('dockerEdit.notice.composeOther', { project: compose.project, file: compose.file })}</p>
      {:else if compose}
        <p class="notice info" role="note">{$tStore('dockerEdit.notice.composeMain', { project: compose.project })}</p>
      {/if}

      {#if tab === 'main'}
        <div class="grid2">
          <div class="field">
            <label class="label" for="de-name">{$tStore('dockerEdit.f.name')} {@render tag(false)}</label>
            <input class="input mono" class:bad={!!errors.name} id="de-name" type="text" autocomplete="off" spellcheck="false" disabled={!!compose} aria-invalid={invalid('name')} bind:value={draft.name} />
          </div>
          <div class="field">
            <label class="label" for="de-image">{$tStore('dockerEdit.f.image')} {@render tag(true)}</label>
            <input class="input mono" class:bad={!!errors.image} id="de-image" type="text" autocomplete="off" spellcheck="false" disabled={!!compose} placeholder={$tStore('dockerEdit.ph.image')} aria-invalid={invalid('image')} bind:value={draft.image} />
          </div>
        </div>
        <div class="grid2">
          <div class="field">
            <span class="label" id="de-restart-l">{$tStore('dockerEdit.f.restart')} {@render tag(false)}</span>
            <Select bind:value={draft.restart} options={restartOptions} ariaLabel={$tStore('dockerEdit.f.restart')} />
          </div>
          {#if draft.restart === 'on-failure'}
            <div class="field">
              <label class="label" for="de-retries">{$tStore('dockerEdit.f.retries')}</label>
              <input class="input mono" class:bad={!!errors.retries} id="de-retries" type="text" inputmode="numeric" autocomplete="off" aria-invalid={invalid('retries')} bind:value={draft.retries} />
              <span class="hint">{$tStore('dockerEdit.hint.retries')}</span>
            </div>
          {/if}
        </div>
        <div class="field">
          <label class="label" for="de-cmd">{$tStore('dockerEdit.f.cmd')} {@render tag(true)}</label>
          <input class="input mono" class:bad={!!errors.cmd} id="de-cmd" type="text" autocomplete="off" spellcheck="false" disabled={!!compose} placeholder={$tStore('dockerEdit.ph.cmd')} aria-invalid={invalid('cmd')} bind:value={draft.cmd} />
          <span class="hint">{$tStore('dockerEdit.hint.cmd')}</span>
        </div>
      {:else if tab === 'ports'}
        {#if draft.ports.length === 0}
          <p class="muted">{$tStore('dockerEdit.port.empty')}</p>
        {:else}
          <div class="rows">
            {#each draft.ports as p (p._id)}
              <div class="erow ports" class:new={!origIds.has(p._id)}>
                <div class="cell">
                  <label class="label" for="de-ports-{p._id}-ip">{$tStore('dockerEdit.port.ip')}</label>
                  <input class="input mono" class:bad={!!errors[`ports.${p._id}.ip`]} id="de-ports-{p._id}-ip" type="text" autocomplete="off" spellcheck="false" disabled={!!compose} aria-invalid={invalid(`ports.${p._id}.ip`)} bind:value={p.ip} />
                </div>
                <div class="cell">
                  <label class="label" for="de-ports-{p._id}-host">{$tStore('dockerEdit.port.host')}</label>
                  <input class="input mono" class:bad={!!errors[`ports.${p._id}.host`]} id="de-ports-{p._id}-host" type="text" inputmode="numeric" autocomplete="off" disabled={!!compose} aria-invalid={invalid(`ports.${p._id}.host`)} bind:value={p.host} />
                </div>
                <div class="cell">
                  <label class="label" for="de-ports-{p._id}-cont">{$tStore('dockerEdit.port.cont')}</label>
                  <input class="input mono" class:bad={!!errors[`ports.${p._id}.cont`]} id="de-ports-{p._id}-cont" type="text" inputmode="numeric" autocomplete="off" disabled={!!compose} aria-invalid={invalid(`ports.${p._id}.cont`)} bind:value={p.cont} />
                </div>
                <div class="cell">
                  <span class="label">{$tStore('dockerEdit.port.proto')}</span>
                  <Select bind:value={p.proto} options={protoOptions} ariaLabel={$tStore('dockerEdit.port.proto')} disabled={!!compose} />
                </div>
                <button class="rm" type="button" aria-label={$tStore('dockerEdit.port.remove')} disabled={!!compose} onclick={() => void removeRow('ports', p._id)}><PixIcon name="x" size={12} /></button>
              </div>
            {/each}
          </div>
        {/if}
        <div><button class="btn add" type="button" id="de-add-ports" disabled={!!compose} onclick={() => void addRow('ports')}>{$tStore('dockerEdit.port.add')}</button></div>
        <p class="hint">{$tStore('dockerEdit.port.hint')}</p>
      {:else if tab === 'mounts'}
        {#if draft.mounts.length === 0}
          <p class="muted">{$tStore('dockerEdit.mount.empty')}</p>
        {:else}
          <div class="rows">
            {#each draft.mounts as m (m._id)}
              <div class="erow mounts" class:new={!origIds.has(m._id)}>
                <div class="cell">
                  <span class="label">{$tStore('dockerEdit.mount.type')}</span>
                  <Select bind:value={m.type} options={mountTypes} ariaLabel={$tStore('dockerEdit.mount.type')} disabled={!!compose} />
                </div>
                <div class="cell wide">
                  <label class="label" for="de-mounts-{m._id}-src">{$tStore(m.type === 'bind' ? 'dockerEdit.mount.srcBind' : 'dockerEdit.mount.srcVolume')}</label>
                  <input class="input mono" class:bad={!!errors[`mounts.${m._id}.src`]} id="de-mounts-{m._id}-src" type="text" autocomplete="off" spellcheck="false" disabled={!!compose} aria-invalid={invalid(`mounts.${m._id}.src`)} bind:value={m.src} />
                </div>
                <div class="cell wide">
                  <label class="label" for="de-mounts-{m._id}-dst">{$tStore('dockerEdit.mount.dst')}</label>
                  <input class="input mono" class:bad={!!errors[`mounts.${m._id}.dst`]} id="de-mounts-{m._id}-dst" type="text" autocomplete="off" spellcheck="false" disabled={!!compose} aria-invalid={invalid(`mounts.${m._id}.dst`)} bind:value={m.dst} />
                </div>
                <div class="cell">
                  <span class="label">{$tStore('dockerEdit.mount.mode')}</span>
                  <label class="check chk"><input type="checkbox" disabled={!!compose} bind:checked={m.ro} /> <span>{$tStore('dockerEdit.mount.ro')}</span></label>
                </div>
                <button class="rm" type="button" aria-label={$tStore('dockerEdit.mount.remove')} disabled={!!compose} onclick={() => void removeRow('mounts', m._id)}><PixIcon name="x" size={12} /></button>
              </div>
            {/each}
          </div>
        {/if}
        <div><button class="btn add" type="button" id="de-add-mounts" disabled={!!compose} onclick={() => void addRow('mounts')}>{$tStore('dockerEdit.mount.add')}</button></div>
        <p class="hint">{$tStore('dockerEdit.mount.hint')}</p>
      {:else if tab === 'env'}
        {#if draft.env.length === 0}
          <p class="muted">{$tStore('dockerEdit.env.empty')}</p>
        {:else}
          <div class="rows">
            {#each draft.env as e (e._id)}
              {@const secret = isSecretName(e.k)}
              {@const shown = !!reveal[e._id]}
              <div class="erow env" class:new={!origIds.has(e._id)}>
                <div class="cell">
                  <label class="label" for="de-env-{e._id}-k">{$tStore('dockerEdit.env.k')}</label>
                  <input class="input mono" class:bad={!!errors[`env.${e._id}.k`]} id="de-env-{e._id}-k" type="text" autocomplete="off" spellcheck="false" disabled={!!compose} aria-invalid={invalid(`env.${e._id}.k`)} bind:value={e.k} />
                </div>
                <div class="cell">
                  <label class="label" for="de-env-{e._id}-v">{$tStore('dockerEdit.env.v')}</label>
                  <div class="vcell">
                    <input class="input mono" id="de-env-{e._id}-v" type={secret && !shown ? 'password' : 'text'} autocomplete="off" spellcheck="false" disabled={!!compose} bind:value={e.v} />
                    {#if secret}
                      <button class="eye" type="button" aria-pressed={shown} aria-label={$tStore(shown ? 'dockerEdit.env.hideLabel' : 'dockerEdit.env.showLabel')} onclick={() => (reveal[e._id] = !shown)}>
                        {$tStore(shown ? 'dockerEdit.env.hide' : 'dockerEdit.env.show')}
                      </button>
                    {/if}
                  </div>
                </div>
                <button class="rm" type="button" aria-label={$tStore('dockerEdit.env.remove')} disabled={!!compose} onclick={() => void removeRow('env', e._id)}><PixIcon name="x" size={12} /></button>
              </div>
            {/each}
          </div>
        {/if}
        <div><button class="btn add" type="button" id="de-add-env" disabled={!!compose} onclick={() => void addRow('env')}>{$tStore('dockerEdit.env.add')}</button></div>
        <p class="hint">{$tStore('dockerEdit.env.hint')}</p>
      {:else}
        <div class="grid2">
          <div class="field">
            <label class="label" for="de-mem">{$tStore('dockerEdit.f.mem')} {@render tag(false)}</label>
            <input class="input mono" class:bad={!!errors.mem} id="de-mem" type="text" inputmode="numeric" autocomplete="off" placeholder={$tStore('dockerEdit.ph.unlimited')} aria-invalid={invalid('mem')} bind:value={draft.mem} />
            <span class="hint">{running ? $tStore('dockerEdit.hint.mem', { used: formatBytes(container.mem_used) }) : $tStore('dockerEdit.hint.memIdle')}</span>
          </div>
          <div class="field">
            <label class="label" for="de-cpus">{$tStore('dockerEdit.f.cpus')} {@render tag(false)}</label>
            <input class="input mono" class:bad={!!errors.cpus} id="de-cpus" type="text" inputmode="decimal" autocomplete="off" placeholder={$tStore('dockerEdit.ph.unlimited')} aria-invalid={invalid('cpus')} bind:value={draft.cpus} />
            <span class="hint">{$tStore('dockerEdit.hint.cpus')}</span>
          </div>
        </div>
      {/if}
    </div>
  {:else}
    {#if diff.recreate && running}
      <p class="notice" role="note">{$tStore('dockerEdit.review.warn', { name: container.name })}</p>
    {/if}
    <ul class="diff" aria-label={$tStore('dockerEdit.diff.list')}>
      {#each diff.changes as ch, i (i)}
        <li>
          <span class="k">{$tStore(`dockerEdit.diff.${ch.field}`)}</span>
          <span class="v">
            <span class="sr-only">{$tStore(ch.kind === 'a' ? 'dockerEdit.diff.added' : ch.kind === 'd' ? 'dockerEdit.diff.removed' : 'dockerEdit.diff.changed')}:</span>
            {#if ch.kind === 'a'}
              <span class="a">+ {ch.after}</span>
            {:else if ch.kind === 'd'}
              <span class="d">{ch.before}</span>
            {:else}
              <span class="d">{ch.before}</span> <span aria-hidden="true">→</span> <span class="a">{ch.after}</span>
            {/if}
          </span>
        </li>
      {/each}
    </ul>
    <div class="steps">
      {#if diff.recreate}
        <span><b>1.</b> {$tStore('dockerEdit.review.step1')}</span>
        <span><b>2.</b> {$tStore('dockerEdit.review.step2', { name: container.name })}</span>
        <span><b>3.</b> {$tStore('dockerEdit.review.step3')}</span>
        <span><b>4.</b> {$tStore('dockerEdit.review.step4')}</span>
      {:else}
        <span>{$tStore('dockerEdit.review.stepsLive')}</span>
      {/if}
    </div>
  {/if}

  {#if $isPhone}
    {@render info()}
    {@render applyErr()}
  {/if}

  <div class="row end foot">
    {#if !$isPhone}{@render info()}{/if}
    {#if confirmDiscard}
      <button class="btn" type="button" onclick={onclose}>{$tStore('dockerEdit.foot.discard')}</button>
      <button class="btn primary" type="button" bind:this={keepBtn} onclick={() => (confirmDiscard = false)}>{$tStore('dockerEdit.foot.keep')}</button>
    {:else if phase === 'edit'}
      <button class="btn" type="button" onclick={tryClose}>{$tStore('common.cancel')}</button>
      <button class="btn primary" type="button" bind:this={nextBtn} disabled={count === 0 || !!firstError} onclick={() => void goReview()}>{$tStore('dockerEdit.foot.next')}</button>
    {:else}
      <button class="btn" type="button" disabled={applying} onclick={() => void goBack()}>{$tStore('dockerEdit.review.back')}</button>
      <button class="btn {diff.recreate ? 'danger' : 'primary'}" type="button" bind:this={applyBtn} disabled={applying} onclick={() => void apply()}>
        {applying ? $tStore('dockerEdit.review.busy') : $tStore(diff.recreate ? 'dockerEdit.review.recreateApply' : 'dockerEdit.review.apply')}
      </button>
    {/if}
  </div>
  {#if !$isPhone}{@render applyErr()}{/if}
</Modal>

<style>
  p {
    margin: 0;
  }

  .sub {
    overflow-wrap: anywhere;
  }

  .sr-only {
    position: absolute;
    width: 1px;
    height: 1px;
    overflow: hidden;
    clip: rect(0 0 0 0);
    white-space: nowrap;
  }

  .tabs {
    display: flex;
    gap: 7px;
    overflow-x: auto;
    padding-bottom: 2px;
  }

  .tabs .chip {
    display: inline-flex;
    align-items: center;
    gap: 7px;
    white-space: nowrap;
  }

  .dot {
    display: none;
    width: 7px;
    height: 7px;
    background: var(--accent);
  }

  .chip.dirty .dot {
    display: block;
  }

  .chip[aria-pressed='true'] .dot {
    background: var(--surface);
  }

  .pane {
    display: flex;
    flex-direction: column;
    gap: 11px;
    min-width: 0;
    min-height: 261px;
  }

  .notice {
    flex: none;
    display: block;
    margin: 0;
    padding: 7px 11px;
    border: 2px solid var(--warn);
    color: var(--warn);
  }

  .notice.info {
    flex: none; /* the plain .info rule below is a flex column for the review block */
    display: block;
    margin: 0;
    border-color: var(--accent);
    color: var(--accent);
  }

  .field {
    min-width: 0;
  }

  .label {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 7px;
    font-family: var(--font-display);
    letter-spacing: 0.06em;
    text-transform: uppercase;
  }

  .grid2 {
    display: grid;
    grid-template-columns: repeat(2, minmax(0, 1fr));
    gap: 11px;
    align-items: start;
  }

  .input:disabled {
    background: var(--seg-off);
    color: var(--muted);
    cursor: not-allowed;
  }

  .input.bad {
    border-color: var(--crit);
  }

  /* "on the spot" / "recreate" next to a field. */
  .hl {
    padding: 0 5px;
    border: 2px solid var(--muted);
    color: var(--muted);
    font-family: var(--font-body);
    font-size: 10px;
    line-height: 14px;
    letter-spacing: 0;
    text-transform: none;
    white-space: nowrap;
  }

  .hl.re {
    border-color: var(--warn);
    color: var(--warn);
  }

  .hl.lv {
    border-color: var(--good);
    color: var(--good);
  }

  .rows {
    display: flex;
    flex-direction: column;
    gap: 7px;
  }

  .erow {
    display: grid;
    gap: 7px;
    align-items: start;
    padding: 7px;
    border: 2px dashed var(--seg-off);
  }

  .erow.new {
    border-color: var(--accent);
  }

  .erow.ports {
    grid-template-columns: 1.1fr 0.8fr 0.8fr 0.7fr 28px;
  }

  .erow.mounts {
    grid-template-columns: 0.9fr 2fr 2fr 0.8fr 28px;
  }

  .erow.env {
    grid-template-columns: 1fr 1.6fr 28px;
  }

  .cell {
    display: flex;
    flex-direction: column;
    gap: 3px;
    min-width: 0;
  }

  .cell > .label {
    color: var(--muted);
    font-size: 11px;
  }

  .rm {
    align-self: end;
    width: 28px;
    height: 32px;
    padding: 0;
    background: var(--surface);
    border: 2px solid var(--line);
    border-radius: 0;
    cursor: pointer;
    font-family: var(--font-display);
    font-size: 16px;
    line-height: 1;
  }

  .rm:hover:not(:disabled) {
    background: var(--crit);
    border-color: var(--crit);
    color: var(--surface);
  }

  .rm:disabled {
    opacity: 0.35;
    cursor: not-allowed;
  }

  .rm:focus-visible,
  .eye:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 2px;
  }

  .vcell {
    display: flex;
    gap: 4px;
  }

  .eye {
    flex: none;
    width: 38px;
    padding: 0;
    background: var(--surface);
    border: 2px solid var(--line);
    border-radius: 0;
    cursor: pointer;
    font-size: 11px;
  }

  .chk {
    height: 32px;
    white-space: nowrap;
  }

  .add {
    height: 27px;
    padding-block: 0;
    font-size: 12px;
    display: inline-flex;
    align-items: center;
  }

  /* The review. */
  .diff {
    display: flex;
    flex-direction: column;
    margin: 0;
    padding: 0;
    border: 2px solid var(--line);
    list-style: none;
  }

  .diff li {
    display: flex;
    flex-wrap: wrap;
    align-items: baseline;
    gap: 3px 11px;
    padding: 5px 9px;
    border-bottom: 2px dashed var(--seg-off);
    overflow-wrap: anywhere;
  }

  .diff li:last-child {
    border-bottom: 0;
  }

  .diff .k {
    min-width: 99px;
    color: var(--muted);
  }

  .diff .a {
    color: var(--good);
  }

  .diff .d {
    color: var(--crit);
    text-decoration: line-through;
  }

  .steps {
    display: flex;
    flex-direction: column;
    gap: 3px;
    color: var(--muted);
  }

  .steps b {
    color: var(--ink);
    font-weight: 400;
  }

  /* The bar under the content. */
  .foot {
    align-items: center;
  }

  .info {
    display: flex;
    flex-direction: column;
    gap: 3px;
    flex: 1 1 225px;
    min-width: 0;
    margin-right: auto;
  }

  .mode {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 7px;
  }

  .tg {
    display: inline-block;
    padding: 0 6px;
    border: 2px solid var(--line);
    font-size: 11px;
    line-height: 16px;
    white-space: nowrap;
  }

  .tg.ok {
    border-color: var(--good);
    color: var(--good);
  }

  .tg.warn {
    border-color: var(--warn);
    color: var(--warn);
  }

  .tg.acc {
    border-color: var(--accent);
    color: var(--accent);
  }

  .err {
    color: var(--crit);
  }

  @media (max-width: 720px) {
    .grid2 {
      grid-template-columns: 1fr;
    }

    .pane {
      min-height: 0;
    }

    .erow.ports,
    .erow.mounts {
      grid-template-columns: 1fr 1fr;
    }

    .erow.env {
      grid-template-columns: 1fr;
    }

    .erow .rm {
      grid-column: 1 / -1;
      width: 100%;
      min-height: 44px;
    }

    .erow .cell.wide {
      grid-column: 1 / -1;
    }

    .eye {
      width: 52px;
      min-height: 44px;
    }

    .chk {
      min-height: 44px;
    }

    .add {
      height: 44px;
      font-size: 13px;
    }

    .info {
      flex: none;
      margin-right: 0;
    }
  }
</style>
