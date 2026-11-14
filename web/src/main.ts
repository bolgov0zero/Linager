import './app.css';
import { mount } from 'svelte';
import App from './App.svelte';

// iOS Safari ignores user-scalable=no for pinch gestures; cancelling them keeps the admin panel at 100%.
for (const type of ['gesturestart', 'gesturechange', 'gestureend']) {
  document.addEventListener(type, (event) => event.preventDefault(), { passive: false });
}

const app = mount(App, { target: document.getElementById('app')! });

export default app;
