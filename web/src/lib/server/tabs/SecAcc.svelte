<script lang="ts">
  import type { Snippet } from 'svelte';
  import PixIcon from '../../ui/PixIcon.svelte';

  /** A section of the Security tab on a phone: a card whose header opens and closes it. */
  let {
    title,
    meta = '',
    tone = 'muted',
    open = $bindable(false),
    children,
  }: {
    title: string;
    /** The counter or status shown at the right of the header. */
    meta?: string;
    tone?: 'ok' | 'warn' | 'crit' | 'muted';
    open?: boolean;
    children: Snippet;
  } = $props();
</script>

<div class="card flush acc">
  <button class="ah" type="button" aria-expanded={open} onclick={() => (open = !open)}>
    <b class="at">{title}</b>
    {#if meta}<span class="meta {tone}">{meta}</span>{/if}
    <span class="chev" class:up={open}><PixIcon name="chev" size={20} /></span>
  </button>
  {#if open}
    <div class="accbody">
      {@render children()}
    </div>
  {/if}
</div>

<style>
  .acc {
    min-width: 0;
  }

  .ah {
    display: flex;
    align-items: center;
    gap: 10px;
    width: 100%;
    min-height: 52px;
    padding: 6px 8px 6px 12px;
    background: none;
    color: var(--ink);
    border: 0;
    font: inherit;
    text-align: left;
    cursor: pointer;
  }

  .at {
    flex: 1;
    min-width: 0;
    font-family: var(--font-display);
    font-size: 16px;
    font-weight: 400;
    overflow-wrap: anywhere;
  }

  .meta {
    flex: none;
    max-width: 45%;
    font-size: 12px;
    text-align: right;
    color: var(--muted);
  }

  .meta.ok {
    color: var(--good);
  }

  .meta.warn {
    color: var(--warn);
  }

  .meta.crit {
    color: var(--crit);
  }

  .chev {
    display: grid;
    place-items: center;
    flex: none;
    width: 36px;
    height: 36px;
  }

  .chev.up {
    transform: rotate(180deg);
  }

  .accbody {
    border-top: 2px dashed var(--seg-off);
  }

  /* The section inside is the old card: its frame and caption are replaced by the header above. */
  .accbody > :global(section.card) {
    padding: 12px;
    background: none;
    border: 0;
    box-shadow: none;
  }

  .accbody :global(.pc-list) {
    margin: 0 -12px;
    border-width: 2px 0 0;
    box-shadow: none;
  }

  .accbody :global(section > h3),
  .accbody :global(.f2h > h3) {
    display: none;
  }
</style>
