import { h } from 'vue';
import DefaultTheme from 'vitepress/theme';
import type { Theme } from 'vitepress';
import Landing from './Landing.vue';
import './custom.css';

// The home pages (layout: home) get the landing page in front of the default content.
export default {
  extends: DefaultTheme,
  Layout: () => h(DefaultTheme.Layout, null, { 'home-hero-before': () => h(Landing) }),
} satisfies Theme;
