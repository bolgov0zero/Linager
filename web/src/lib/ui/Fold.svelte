<script lang="ts">
  import type { Snippet } from 'svelte';
  import PixIcon from './PixIcon.svelte';

  /**
   * A card whose header (the same uppercase caption as the other cards) opens and closes the body.
   * Closed by default. `meta` is a short counter or status at the right of the header.
   */
  let {
    title,
    meta = '',
    tone = 'muted',
    open = $bindable(false),
    children,
  }: {
    title: string;
    meta?: string;
    tone?: 'ok' | 'warn' | 'crit' | 'muted';
    open?: boolean;
    children: Snippet;
  } = $props();
</script>

<section class="card fold">
  <button class="fh" type="button" aria-expanded={open} onclick={() => (open = !open)}>
    <h3>{title}</h3>
    {#if meta}<span class="meta {tone}">{meta}</span>{/if}
    <span class="chev" class:up={open}><PixIcon name="chev" /></span>
  </button>
  {#if open}
    <div class="fbody">
      {@render children()}
    </div>
  {/if}
</section>

<style>
  .fold {
    display: flex;
    flex-direction: column;
    min-width: 0;
  }

  .fh {
    display: flex;
    align-items: center;
    gap: 9px;
    width: 100%;
    min-height: 32px;
    padding: 0;
    background: none;
    color: var(--ink);
    border: 0;
    font: inherit;
    text-align: left;
    cursor: pointer;
  }

  h3 {
    flex: 1;
    min-width: 0;
    margin: 0;
    font-size: 14px;
    font-weight: 400;
    color: var(--muted);
    text-transform: uppercase;
    letter-spacing: 0.04em;
  }

  .meta {
    flex: none;
    font-size: 11px;
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
    width: 25px;
    height: 25px;
  }

  .chev.up {
    transform: rotate(180deg);
  }

  .fbody {
    display: flex;
    flex-direction: column;
    gap: 11px;
    margin-top: 11px;
  }

  @media (max-width: 720px) {
    .fh {
      min-height: 44px;
    }

    h3 {
      font-size: 13px;
    }
  }
</style>
