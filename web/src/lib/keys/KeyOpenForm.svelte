<script lang="ts">
  import { tr, t } from '../../i18n';
  import { openFile } from './store';
  import { FILE_EXT, VaultError } from './vault';

  /** The fields that open the vault: the file and the passphrase. Used by the window "Keys" and by the small window of a form. */
  let { onopened }: { onopened?: () => void } = $props();

  let file = $state<File | null>(null);
  let pass = $state('');
  let busy = $state(false);
  let error = $state('');
  let input = $state<HTMLInputElement | null>(null);

  async function submit(event: SubmitEvent): Promise<void> {
    event.preventDefault();
    if (!file || !pass || busy) return;
    busy = true;
    error = '';
    try {
      await openFile(file, pass);
      pass = '';
      file = null;
      onopened?.();
    } catch (e) {
      error = e instanceof VaultError ? t(`keys.err.${e.code}`) : t('common.genericError');
    } finally {
      busy = false;
    }
  }
</script>

<form class="stack" onsubmit={submit}>
  <div class="field">
    <span class="label">{$tr('keys.file')}</span>
    <div class="row fileline">
      <span class="fn" class:none={!file}>{file?.name ?? $tr('keys.file.none')}</span>
      <button class="btn" type="button" onclick={() => input?.click()}>{$tr('keys.file.pick')}</button>
      <input
        bind:this={input}
        type="file"
        accept={FILE_EXT}
        hidden
        onchange={() => {
          file = input?.files?.[0] ?? null;
          error = '';
        }} />
    </div>
  </div>
  <label class="field">
    <span class="label">{$tr('keys.pass')}</span>
    <input class="input" type="password" bind:value={pass} autocomplete="current-password" oninput={() => (error = '')} />
  </label>
  {#if error}<p class="error" role="alert">{error}</p>{/if}
  <button class="btn primary" type="submit" disabled={!file || !pass || busy}>{busy ? $tr('keys.open.busy') : $tr('keys.open.go')}</button>
</form>

<style>
  p {
    margin: 0;
  }

  .fileline {
    flex-wrap: nowrap;
  }

  .fn {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    padding: 6px 9px;
    border: 2px dashed var(--seg-off);
    font-size: 13px;
  }

  .fn.none {
    color: var(--muted);
  }
</style>
