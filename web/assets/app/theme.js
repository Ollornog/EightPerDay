// SPDX-License-Identifier: MIT
// Runs before the first paint, so nothing flashes: light or dark, kept in this browser under the key the design system's
// theme switch uses (localStorage.themeMode). Without a choice, the system decides. Inside another application the key
// is the application's (data-theme-key on <html>), and the page follows when the application changes it.
(() => {
  const root = document.documentElement;
  const key = root.dataset.themeKey || "themeMode";
  const scheme = window.matchMedia("(prefers-color-scheme: dark)");
  const apply = () => {
    let mode = null;
    try { mode = localStorage.getItem(key); } catch { /* private window */ }
    root.classList.toggle("dark", mode === "dark" || (mode !== "light" && scheme.matches));
  };
  apply();
  scheme.addEventListener("change", apply);
  window.addEventListener("storage", (event) => { if (event.key === key) apply(); });
})();
