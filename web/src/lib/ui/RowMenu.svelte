<script lang="ts">
  import ActionMenu from './ActionMenu.svelte';
  import { isPhone } from './viewport';

  /** The row actions menu: the usual ActionMenu on a desktop, a 44 px "⋯" button on a phone (the list is a sheet there). */
  let {
    title = '',
    label,
    ...rest
  }: {
    label: string;
    /** The row's name: the sheet on a phone is titled "label: title". */
    title?: string;
    items: { key: string; label: string; disabled?: boolean; onselect: () => void }[];
    disabled?: boolean;
  } = $props();
</script>

<span class="rm"><ActionMenu label={title && $isPhone ? `${label}: ${title}` : label} {...rest} /></span>

<style>
  .rm {
    display: inline-flex;
  }

  @media (max-width: 720px) {
    .rm :global(.am) {
      width: 44px;
      min-width: 44px;
      padding: 0;
      gap: 0;
      font-size: 0;
    }

    .rm :global(.am .arrow) {
      display: none;
    }

    .rm :global(.am)::after {
      content: '';
      width: 18px;
      height: 18px;
      background: currentColor;
      -webkit-mask: url("data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 8 8' shape-rendering='crispEdges'%3E%3Cpath d='M0 2h2v2H0zM3 2h2v2H3zM6 2h2v2H6z'/%3E%3C/svg%3E") center / contain no-repeat;
      mask: url("data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 8 8' shape-rendering='crispEdges'%3E%3Cpath d='M0 2h2v2H0zM3 2h2v2H3zM6 2h2v2H6z'/%3E%3C/svg%3E") center / contain no-repeat;
    }
  }
</style>
