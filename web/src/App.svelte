<script lang="ts">
  import { locale, setLocale, t, type Locale } from './i18n';

  type Theme = 'light' | 'dark';

  function initialTheme(): Theme {
    const attr = document.documentElement.dataset.theme;
    if (attr === 'light' || attr === 'dark') return attr;
    return window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light';
  }

  let theme = $state<Theme>(initialTheme());

  // Reactive translator: re-derives whenever the locale store changes.
  const tr = $derived.by(() => {
    const current = $locale;
    return (key: string) => t(key, current);
  });

  function toggleTheme(): void {
    theme = theme === 'dark' ? 'light' : 'dark';
    document.documentElement.dataset.theme = theme;
  }

  function pickLocale(next: Locale): void {
    setLocale(next);
  }
</script>

<header class="top">
  <div class="brand">
    <h1>{tr('app.title')}</h1>
    <span class="status">{tr('app.status')}</span>
  </div>
  <div class="controls">
    <button class="btn" type="button" onclick={toggleTheme}>
      {theme === 'dark' ? tr('theme.light') : tr('theme.dark')}
    </button>
    <div class="seg" role="group" aria-label="language">
      <button
        class="chip"
        type="button"
        aria-pressed={$locale === 'ru'}
        onclick={() => pickLocale('ru')}>{tr('lang.ru')}</button>
      <button
        class="chip"
        type="button"
        aria-pressed={$locale === 'en'}
        onclick={() => pickLocale('en')}>{tr('lang.en')}</button>
    </div>
  </div>
</header>

<main>
  <section class="placeholder">
    <p>{tr('page.placeholder')}</p>
  </section>
</main>

<style>
  .top {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    padding: 12px 16px;
    background: var(--surface);
    border-bottom: 2px solid var(--line);
    box-shadow: 3px 3px 0 var(--shade);
  }

  .brand {
    display: flex;
    align-items: baseline;
    gap: 12px;
  }

  .brand h1 {
    margin: 0;
    font-size: 26px;
    letter-spacing: 0.02em;
    line-height: 1;
  }

  .status {
    color: var(--muted);
  }

  .controls {
    display: flex;
    align-items: center;
    gap: 8px;
  }

  .btn,
  .chip {
    background: var(--surface);
    border: 2px solid var(--line);
    border-radius: 0;
    padding: 6px 12px;
    cursor: pointer;
    font-family: var(--font-display);
    font-size: 14px;
    letter-spacing: 0.02em;
    box-shadow: 3px 3px 0 var(--shade);
  }

  .chip[aria-pressed='true'] {
    background: var(--ink);
    color: var(--surface);
  }

  .seg {
    display: flex;
    gap: 6px;
  }

  main {
    padding: 24px 16px;
  }

  .placeholder {
    max-width: 720px;
    padding: 24px;
    background: var(--surface);
    border: 2px solid var(--line);
    box-shadow: 4px 4px 0 var(--shade);
  }

  .placeholder p {
    margin: 0;
    color: var(--muted);
  }
</style>
