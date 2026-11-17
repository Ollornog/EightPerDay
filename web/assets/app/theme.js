// SPDX-License-Identifier: MIT
// Runs before the first paint, so nothing flashes: light or dark, kept in this browser under the key the design system's
// theme switch uses (localStorage.themeMode). Without a choice, the system decides.
(() => {
  let mode = null;
  try { mode = localStorage.getItem("themeMode"); } catch { /* private window */ }
  const dark = mode === "dark" || (mode !== "light" && window.matchMedia("(prefers-color-scheme: dark)").matches);
  document.documentElement.classList.toggle("dark", dark);
})();
