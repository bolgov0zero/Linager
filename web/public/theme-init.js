// Sets the theme before the page is painted: the one the user picked (kept in this browser), dark by default.
// The system's light or dark setting is not followed. A separate file because the CSP forbids inline scripts.
(function () {
  var theme = 'dark';
  try {
    var saved = localStorage.getItem('linager.theme');
    if (saved === 'light' || saved === 'dark') theme = saved;
  } catch (e) {
    // storage is blocked: the default stays
  }
  document.documentElement.dataset.theme = theme;
  var meta = document.querySelector('meta[name="theme-color"]');
  if (meta) meta.setAttribute('content', theme === 'dark' ? '#0f1218' : '#e8ebe5');
})();
