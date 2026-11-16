<script lang="ts">
  import type { FwSet } from '../../../api';
  import { tr as trs } from '../../../i18n';

  let {
    value = $bindable(''),
    lists,
    placeholder,
    ariaLabel,
    cls = '',
    oninput,
  }: {
    value: string;
    lists: FwSet[];
    placeholder?: string;
    ariaLabel: string;
    /** Classes of the input itself. */
    cls?: string;
    oninput?: () => void;
  } = $props();

  const uid = `src-${Math.random().toString(36).slice(2, 9)}`;
  let open = $state(false);
  let active = $state(-1);
  let input = $state<HTMLInputElement | undefined>(undefined);

  // The list is offered while nothing but a list name is typed: an address or a network needs no help.
  const q = $derived(value.trim().replace(/^@/, '').toLowerCase());
  const shown = $derived(/^[a-z0-9_]*$/.test(q) ? lists.filter((s) => s.name.includes(q)) : []);
  const visible = $derived(open && shown.length > 0);

  function pick(name: string): void {
    value = '@' + name;
    open = false;
    active = -1;
    oninput?.();
    input?.focus();
  }

  function onkeydown(e: KeyboardEvent): void {
    if (e.key === 'Escape' && open) {
      e.stopPropagation(); // do not close a dialog around the field
      open = false;
      return;
    }
    if (!shown.length) return;
    if (e.key === 'ArrowDown') {
      e.preventDefault();
      open = true;
      active = (active + 1) % shown.length;
    } else if (e.key === 'ArrowUp') {
      e.preventDefault();
      open = true;
      active = (active - 1 + shown.length) % shown.length;
    } else if (e.key === 'Enter' && visible && active >= 0) {
      e.preventDefault();
      pick(shown[active].name);
    }
  }
</script>

<span class="src">
  <input
    bind:this={input}
    class={cls}
    type="text"
    autocomplete="off"
    spellcheck="false"
    role="combobox"
    aria-expanded={visible}
    aria-controls={uid}
    aria-autocomplete="list"
    {placeholder}
    aria-label={ariaLabel}
    bind:value
    oninput={() => {
      open = true;
      active = -1;
      oninput?.();
    }}
    onfocus={() => (open = true)}
    onblur={() => setTimeout(() => (open = false), 120)}
    {onkeydown}
  />
  {#if visible}
    <ul class="pop" id={uid} role="listbox" aria-label={$trs('fw.sets.listLabel')}>
      {#each shown as s, i (s.name)}
        <li role="option" aria-selected={i === active}>
          <button class="opt" class:on={i === active} type="button" tabindex="-1" onmousedown={(e) => e.preventDefault()} onclick={() => pick(s.name)}>
            <span class="mono nm">@{s.name}</span>
            <span class="small muted">{s.family === 'ipv6' ? 'IPv6' : 'IPv4'} · {s.elements.length}</span>
          </button>
        </li>
      {/each}
    </ul>
  {/if}
</span>

<style>
  .src {
    position: relative;
    display: inline-flex;
    min-width: 0;
  }

  .src > input {
    width: 100%;
    min-width: 0;
  }

  .pop {
    position: absolute;
    z-index: 30;
    top: calc(100% + 2px);
    left: 0;
    min-width: 100%;
    max-height: 220px;
    overflow: auto;
    margin: 0;
    padding: 0;
    list-style: none;
    border: 2px solid var(--line);
    background: var(--surface);
    box-shadow: 3px 3px 0 var(--shade);
  }

  .pop li + li {
    border-top: 2px dashed var(--seg-off);
  }

  .opt {
    display: flex;
    justify-content: space-between;
    align-items: baseline;
    gap: 12px;
    width: 100%;
    padding: 7px 10px;
    border: 0;
    background: none;
    color: inherit;
    font: inherit;
    font-size: 13px;
    text-align: left;
    cursor: pointer;
    white-space: nowrap;
  }

  .opt:hover,
  .opt.on {
    background: var(--ink);
    color: var(--surface);
  }

  .opt:hover .muted,
  .opt.on .muted {
    color: inherit;
    opacity: 0.8;
  }

  @media (max-width: 720px) {
    .src {
      display: flex;
      width: 100%;
    }

    .opt {
      min-height: 44px;
      align-items: center;
    }
  }
</style>
