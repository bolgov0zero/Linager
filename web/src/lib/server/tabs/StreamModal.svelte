<script lang="ts">
  import { onMount } from 'svelte';
  import { openStream, type StreamKind } from '../../../api';
  import { tr } from '../../../i18n';
  import Modal from '../../Modal.svelte';
  import { isPhone } from '../../ui/viewport';
  import { attachStream, createBatcher, LINE_LIMIT, type StreamLine, type StreamState } from './docker-util';

  let {
    title,
    serverId,
    kind,
    params = {},
    onend,
    onclose,
  }: {
    title: string;
    serverId: string;
    kind: StreamKind;
    params?: Record<string, string | number>;
    /** Called once when the stream is over; `failed` is true after an error. */
    onend?: (failed: boolean) => void;
    onclose: () => void;
  } = $props();

  let lines = $state<StreamLine[]>([]);
  let phase = $state<StreamState>('connecting');
  let box = $state<HTMLDivElement | undefined>(undefined);

  onMount(() => {
    const batch = createBatcher((next) => {
      lines.push(...next);
      if (lines.length > LINE_LIMIT) lines.splice(0, lines.length - LINE_LIMIT);
    });
    const detach = attachStream(openStream(serverId, kind, params), {
      open: () => {
        phase = 'live';
      },
      lines: (next) => batch.push(next),
      end: (failed) => {
        batch.flushNow();
        phase = failed ? 'failed' : 'ended';
        onend?.(failed);
      },
    });
    return () => {
      detach();
      batch.cancel();
    };
  });

  $effect(() => {
    void lines.length;
    if (box) box.scrollTop = box.scrollHeight;
  });
</script>

<div class="wide">
  <Modal {title} labelId="stream-modal-title" {onclose} wide={$isPhone}>
    <p class="small muted">{$tr(`docker.stream.${phase}`)}</p>
    <div class="logpanel mono" bind:this={box} role="log">
      {#each lines as line, i (i)}
        <div class="ln {line.tone}">{line.text}</div>
      {:else}
        <div class="ln info">{$tr('docker.pull.waiting')}</div>
      {/each}
    </div>
    <div class="row end">
      <button class="btn" type="button" onclick={onclose}>{$tr('common.close')}</button>
    </div>
  </Modal>
</div>

<style>
  .wide :global(.dialog) {
    max-width: 810px;
  }

  .logpanel {
    height: min(60vh, 414px);
    overflow: auto;
    padding: 9px;
    border: 2px solid var(--line);
    background: #0c0f14;
    color: #d8dee9;
    font-size: 11px;
    line-height: 1.4;
  }

  .ln {
    white-space: pre-wrap;
    overflow-wrap: anywhere;
  }

  .ln.info {
    color: #8a93a4;
  }

  .ln.error {
    color: #ff8a80;
  }

  /* Phones: the output fills the sheet. */
  @media (max-width: 720px) {
    .wide :global(.dialog) {
      max-width: none;
    }

    .logpanel {
      flex: 1 1 auto;
      height: auto;
      min-height: 50dvh;
    }
  }
</style>
