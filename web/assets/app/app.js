// SPDX-License-Identifier: MIT
// The behavior of EightPerDay's pages: dialogs, the question before something is removed, the theme, and the passkey
// steps. No inline code anywhere (the CSP forbids it); everything hangs on data attributes.
(() => {
  "use strict";

  // ---- Dialogs: [data-open-dialog="id"] opens one, [data-close-dialog] or a click on the backdrop closes it.
  document.addEventListener("click", (event) => {
    const opener = event.target.closest("[data-open-dialog]");
    if (opener) {
      const dialog = document.getElementById(opener.dataset.openDialog);
      if (dialog && typeof dialog.showModal === "function") {
        event.preventDefault();
        dialog.showModal();
      }
      return;
    }
    const closer = event.target.closest("[data-close-dialog]");
    if (closer) {
      closer.closest("dialog")?.close();
      return;
    }
    if (event.target instanceof HTMLDialogElement) event.target.close();
  });

  // ---- The question before a form removes something: a dialog of the page, never the browser's own question box.
  let confirmed = null;
  document.addEventListener("submit", (event) => {
    const form = event.target;
    if (!(form instanceof HTMLFormElement) || !form.dataset.confirm) return;
    if (confirmed === form) {
      confirmed = null;
      return;
    }
    const dialog = document.getElementById("confirm-dialog");
    if (!dialog) return;
    event.preventDefault();
    dialog.querySelector("[data-confirm-text]").textContent = form.dataset.confirm;
    const ok = dialog.querySelector("[data-confirm-ok]");
    const onOK = () => {
      dialog.close();
      confirmed = form;
      form.requestSubmit(event.submitter || undefined);
    };
    ok.addEventListener("click", onOK, { once: true });
    dialog.addEventListener("close", () => ok.removeEventListener("click", onOK), { once: true });
    dialog.showModal();
  }, true);

  // ---- Theme: light and dark, kept in this browser (theme.js applies it before the first paint).
  document.addEventListener("click", (event) => {
    if (!event.target.closest("[data-theme-toggle]")) return;
    const dark = !document.documentElement.classList.contains("dark");
    document.documentElement.classList.toggle("dark", dark);
    try { localStorage.setItem("themeMode", dark ? "dark" : "light"); } catch { /* private window */ }
  });

  // ---- A field whose text is meant to be copied, such as a one-time link.
  document.addEventListener("focusin", (event) => {
    if (event.target.matches?.("[data-select-on-focus]")) event.target.select();
  });

  // ---- Passkeys: the browser's WebAuthn with the options the server sends, as JSON both ways.
  const b64url = {
    decode(text) {
      const plain = text.replace(/-/g, "+").replace(/_/g, "/");
      const padded = plain + "===".slice((plain.length + 3) % 4);
      return Uint8Array.from(atob(padded), (c) => c.charCodeAt(0)).buffer;
    },
    encode(buffer) {
      let text = "";
      for (const byte of new Uint8Array(buffer)) text += String.fromCharCode(byte);
      return btoa(text).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
    },
  };

  function creationOptions(json) {
    if (PublicKeyCredential.parseCreationOptionsFromJSON) return PublicKeyCredential.parseCreationOptionsFromJSON(json);
    const options = { ...json, challenge: b64url.decode(json.challenge), user: { ...json.user, id: b64url.decode(json.user.id) } };
    options.excludeCredentials = (json.excludeCredentials || []).map((c) => ({ ...c, id: b64url.decode(c.id) }));
    return options;
  }

  function requestOptions(json) {
    if (PublicKeyCredential.parseRequestOptionsFromJSON) return PublicKeyCredential.parseRequestOptionsFromJSON(json);
    const options = { ...json, challenge: b64url.decode(json.challenge) };
    options.allowCredentials = (json.allowCredentials || []).map((c) => ({ ...c, id: b64url.decode(c.id) }));
    return options;
  }

  function credentialJSON(credential) {
    if (typeof credential.toJSON === "function") return credential.toJSON();
    const r = credential.response;
    const response = { clientDataJSON: b64url.encode(r.clientDataJSON) };
    if (r.attestationObject) {
      response.attestationObject = b64url.encode(r.attestationObject);
      if (r.getTransports) response.transports = r.getTransports();
    } else {
      response.authenticatorData = b64url.encode(r.authenticatorData);
      response.signature = b64url.encode(r.signature);
      if (r.userHandle) response.userHandle = b64url.encode(r.userHandle);
    }
    return {
      id: credential.id, rawId: b64url.encode(credential.rawId), type: credential.type, response,
      authenticatorAttachment: credential.authenticatorAttachment || undefined,
      clientExtensionResults: credential.getClientExtensionResults ? credential.getClientExtensionResults() : {},
    };
  }

  async function post(url, body) {
    const answer = await fetch(url, {
      method: "POST", credentials: "same-origin",
      headers: { "Content-Type": "application/json" },
      body: body === undefined ? "{}" : JSON.stringify(body),
    });
    let data = {};
    try { data = await answer.json(); } catch { /* not JSON */ }
    if (!answer.ok) throw new Error(data.error || answer.statusText);
    return data;
  }

  function showError(box, error) {
    const field = box.querySelector("[data-passkey-error]");
    if (!field) return;
    // The kind of failure stays on the box, for whoever has to find out why.
    box.dataset.failure = (error && error.name) || "Error";
    // A person who closes the browser's dialog has not failed; say nothing then.
    if (error && error.name === "NotAllowedError") {
      field.hidden = true;
      return;
    }
    field.textContent = (error && error.message) || String(error);
    field.hidden = false;
  }

  async function register(beginURL, beginBody, finishURL) {
    const options = await post(beginURL, beginBody);
    const credential = await navigator.credentials.create({ publicKey: creationOptions(options.publicKey) });
    return post(finishURL, credentialJSON(credential));
  }

  function busy(button, on) {
    button.disabled = on;
    button.setAttribute("aria-busy", on ? "true" : "false");
  }

  // Sign in with a passkey: no name needed, the passkey says whose it is.
  document.addEventListener("click", async (event) => {
    const button = event.target.closest("[data-passkey-login-start]");
    if (!button) return;
    const box = button.closest("[data-passkey-login]");
    busy(button, true);
    try {
      const options = await post("/login/passkey/begin");
      const credential = await navigator.credentials.get({ publicKey: requestOptions(options.publicKey) });
      const done = await post("/login/passkey/finish?next=" + encodeURIComponent(box.dataset.next || "/"), credentialJSON(credential));
      window.location.assign(done.next);
    } catch (error) {
      showError(box, error);
    } finally {
      busy(button, false);
    }
  });

  // Add a passkey to a signed-in account.
  document.addEventListener("click", async (event) => {
    const button = event.target.closest("[data-passkey-add-start]");
    if (!button) return;
    const box = button.closest("[data-passkey-add]");
    const name = box.querySelector("input[name=name]").value;
    busy(button, true);
    try {
      const done = await register("/account/passkeys/begin", undefined, "/account/passkeys/finish?name=" + encodeURIComponent(name));
      window.location.assign(done.next);
    } catch (error) {
      showError(box, error);
    } finally {
      busy(button, false);
    }
  });

  // The page of a one-time link: the password fields only matter when a password is chosen, and a passkey is made here.
  const setup = document.querySelector("form[data-setup]");
  if (setup) {
    const fields = setup.querySelector("[data-password-fields]");
    const sync = () => {
      const method = setup.querySelector("input[name=method]:checked")?.value;
      const password = method !== "passkey";
      fields.hidden = !password;
      for (const input of fields.querySelectorAll("input")) input.required = password;
    };
    setup.addEventListener("change", sync);
    sync();
    setup.addEventListener("submit", async (event) => {
      if (setup.querySelector("input[name=method]:checked")?.value !== "passkey") return;
      event.preventDefault();
      const button = setup.querySelector("button[type=submit]");
      const value = (name) => setup.querySelector(`input[name=${name}]`)?.value || "";
      busy(button, true);
      try {
        const done = await register("/setup/passkey/begin",
          { token: setup.dataset.token, username: value("username"), display_name: value("display_name") },
          "/setup/passkey/finish?name=" + encodeURIComponent(navigator.platform || ""));
        window.location.assign(done.next);
      } catch (error) {
        showError(setup, error);
      } finally {
        busy(button, false);
      }
    });
  }

  // ---- A form that shows something else as soon as a choice changes, such as the employer of the week.
  document.addEventListener("change", (event) => {
    const form = event.target.closest?.("form[data-autosubmit]");
    if (form) form.requestSubmit();
  });

  // ---- The free-text line says what it understood while it is typed. The server parses; this only shows it.
  for (const input of document.querySelectorAll("input[data-line]")) {
    const preview = input.form.querySelector("[data-line-preview]");
    let timer = null;
    let asked = 0;
    input.addEventListener("input", () => {
      clearTimeout(timer);
      timer = setTimeout(async () => {
        const line = input.value.trim();
        const mine = ++asked;
        if (!line) {
          preview.textContent = "";
          return;
        }
        try {
          const answer = await fetch(document.body.dataset.base + "/api/parse?line=" + encodeURIComponent(line), { credentials: "same-origin" });
          const data = await answer.json();
          if (mine === asked) preview.textContent = data.error || data.text || "";
        } catch { /* offline: the line is checked when it is sent */ }
      }, 250);
    });
  }

  // ---- The stopwatch runs on the server; the page only counts along.
  const elapsed = document.querySelector("[data-timer-elapsed]");
  if (elapsed) {
    const started = Number(elapsed.dataset.started) * 1000;
    const tick = () => {
      const minutes = Math.max(0, Math.floor((Date.now() - started) / 60000));
      elapsed.textContent = Math.floor(minutes / 60) + ":" + String(minutes % 60).padStart(2, "0");
    };
    tick();
    setInterval(tick, 15000);
  }

  // ---- A page to print, such as the weekly report: the browser's print, which also saves a PDF.
  document.addEventListener("click", (event) => {
    if (event.target.closest("[data-print]")) window.print();
  });

  // ---- The sheet (sheet.go, templates/sheet.html): the start page in its three views. One keyboard for all of them:
  // a field shows its hours and gives back what was typed while it has the focus; Enter or ↓ goes to the next line
  // (the next day in the table and the journal, the next row in the grid), Shift+Enter or ↑ back, ← and → at the edge
  // of a field to the field beside it, Esc takes back what was typed. Each field saves itself when it is left
  // (POST /sheet/cell); the answer is the weeks from the one changed on, rendered by the server, and only the parts
  // marked data-slot are taken over, so a field being typed into is never touched. Without JavaScript each week is a
  // form with a save button.
  const sheet = document.querySelector("[data-sheet]");
  if (sheet) setUpSheet(sheet);

  function setUpSheet(sheet) {
    const base = document.body.dataset.base || "";
    const scroller = sheet.querySelector("[data-sheet-scroller]");
    const errorLine = sheet.querySelector("[data-sheet-error]");
    const params = new URLSearchParams(location.search);
    // Nobody chose a view and the screen is narrow: the journal fits a phone.
    if (sheet.hasAttribute("data-auto-view") && sheet.dataset.view !== "journal" && !params.has("view") &&
        window.matchMedia("(max-width: 640px)").matches) {
      const target = new URL(location.href);
      target.searchParams.set("view", "journal");
      location.replace(target);
      return;
    }

    // What the page needs without JavaScript goes away: the save buttons and reason fields of the weeks.
    const scriptOnly = () => {
      for (const el of sheet.querySelectorAll("[data-save-week], [data-week-reason]")) el.hidden = true;
    };
    // The week's row sticks right below the head of the table: its offset is the height of the head.
    const stick = () => {
      const head = sheet.querySelector("[data-sheet-table] thead");
      if (!head) return;
      const top = head.getBoundingClientRect().height + "px";
      for (const cell of sheet.querySelectorAll("[data-sheet-table] tr[data-week-row] > *")) cell.style.top = top;
    };
    scriptOnly();
    stick();
    const head = sheet.querySelector("[data-sheet-table] thead");
    if (head && "ResizeObserver" in window) new ResizeObserver(stick).observe(head);

    const isText = (input) => input.dataset.col === "note" || input.dataset.col === "line";
    // A field without the focus shows its value as the server wrote it (the value attribute); with the focus what was
    // typed (data-raw), unless the server refused it, which then stays to be fixed.
    const enter = (input) => {
      if (input.getAttribute("aria-invalid") !== "true" && !isText(input)) input.value = input.dataset.raw || "";
      setTimeout(() => { if (document.activeElement === input) input.select(); }, 0);
    };

    // Saving, one field after the other, so the answers arrive in the order of the changes.
    let queue = Promise.resolve();
    let pending = 0;
    const busy = (delta) => {
      pending += delta;
      sheet.toggleAttribute("data-saving", pending > 0);
    };
    const save = (input, value, reason) => {
      busy(1);
      queue = queue.then(() => send(input, value, reason)).finally(() => busy(-1));
    };
    async function send(input, value, reason) {
      const body = new URLSearchParams({ date: input.dataset.date, col: input.dataset.col, value,
        view: sheet.dataset.view, to: sheet.dataset.last });
      if (reason) body.set("reason", reason);
      let data = {};
      try {
        const answer = await fetch(base + "/sheet/cell", { method: "POST", credentials: "same-origin", body });
        data = await answer.json();
      } catch {
        data = { error: errorLine.dataset.offline || "offline" };
      }
      if (data.reason) {
        askReason(input, value);
        return;
      }
      if (data.error) {
        input.setAttribute("aria-invalid", "true");
        input.title = data.error;
        errorLine.textContent = (input.getAttribute("aria-label") || "") + ": " + data.error;
        errorLine.hidden = false;
        return;
      }
      input.removeAttribute("aria-invalid");
      input.removeAttribute("title");
      errorLine.hidden = true;
      if (data.html) merge(data.html);
    }

    // A change to an approved week asks for its reason, then goes as a change request.
    const reasonDialog = document.getElementById("reason-dialog");
    let asking = null;
    function askReason(input, value) {
      asking = { input, value };
      const text = reasonDialog.querySelector("[data-reason-text]");
      text.value = "";
      reasonDialog.showModal();
      text.focus();
    }
    reasonDialog?.querySelector("[data-reason-ok]")?.addEventListener("click", () => {
      const reason = reasonDialog.querySelector("[data-reason-text]").value.trim();
      if (!reason || !asking) return;
      const { input, value } = asking;
      asking = null;
      reasonDialog.close();
      save(input, value, reason);
    });
    reasonDialog?.querySelector("[data-reason-text]")?.addEventListener("keydown", (event) => {
      if (event.key === "Enter") {
        event.preventDefault();
        reasonDialog.querySelector("[data-reason-ok]").click();
      }
    });
    reasonDialog?.addEventListener("close", () => {
      if (!asking) return;
      // Cancelled: the field shows the approved value again.
      asking.input.value = asking.input.defaultValue;
      asking = null;
    });

    // The answer of the server: every part marked data-slot replaces the one of the page with the same name.
    function merge(html, withOlder) {
      const part = new DOMParser().parseFromString(html, "text/html").querySelector("[data-sheet-part]");
      if (!part) return;
      for (const fresh of part.querySelectorAll("[data-slot]")) {
        const slot = fresh.dataset.slot;
        if (slot === "older" && !withOlder) continue;
        const old = sheet.querySelector('[data-slot="' + CSS.escape(slot) + '"]');
        if (!old) continue;
        if (old.tagName !== fresh.tagName) {
          if (document.activeElement !== old) old.replaceWith(fresh);
          continue;
        }
        if (old.tagName === "INPUT") {
          old.defaultValue = fresh.defaultValue;
          old.dataset.raw = fresh.dataset.raw || "";
          old.placeholder = fresh.placeholder;
          if (document.activeElement !== old && old.getAttribute("aria-invalid") !== "true") old.value = old.defaultValue;
          const was = old.nextElementSibling;
          if (was && was.type === "hidden") was.value = fresh.defaultValue;
          continue;
        }
        old.replaceChildren(...fresh.childNodes);
      }
      scriptOnly();
      stick();
    }

    // ---- Moving with the keyboard.
    function focusIn(input) {
      input.focus({ preventScroll: true });
      const r = input.getBoundingClientRect();
      const s = scroller.getBoundingClientRect();
      const sticky = [...sheet.querySelectorAll("thead th:first-child, [data-week-row] > :first-child")]
        .map((el) => el.getBoundingClientRect())
        .filter((b) => b.top <= s.top + 2 && b.bottom > s.top)
        .reduce((most, b) => Math.max(most, b.bottom - s.top), 0);
      if (r.bottom > s.bottom - 8) scroller.scrollTop += r.bottom - s.bottom + 48;
      else if (r.top < s.top + sticky + 4) scroller.scrollTop -= s.top + sticky + 4 - r.top + 8;
    }
    // move leads to the field beside or below; where there is none, the field is left all the same, which saves it.
    function move(input, rows, columns) {
      const line = input.closest("[data-line]");
      let next = null;
      if (line && columns) {
        const fields = [...line.querySelectorAll("input[data-cell]")];
        next = fields[fields.indexOf(input) + columns];
      } else if (line) {
        const lines = [...sheet.querySelectorAll("[data-line]")];
        for (let i = lines.indexOf(line) + rows; !next && i >= 0 && i < lines.length; i += rows) {
          next = lines[i].querySelector('input[data-cell][data-x="' + CSS.escape(input.dataset.x) + '"]');
        }
      }
      if (next) focusIn(next);
      else if (!columns) input.blur();
    }
    scroller.addEventListener("focusin", (event) => {
      if (event.target.matches?.("input[data-cell]")) enter(event.target);
    });
    scroller.addEventListener("focusout", (event) => {
      const input = event.target;
      if (!input.matches?.("input[data-cell]")) return;
      const value = input.value.trim();
      if (value === (input.dataset.raw || "").trim()) {
        if (input.getAttribute("aria-invalid") === "true") {
          input.removeAttribute("aria-invalid");
          input.removeAttribute("title");
          errorLine.hidden = true;
        }
        input.value = input.defaultValue;
        return;
      }
      save(input, value, "");
    });
    scroller.addEventListener("keydown", (event) => {
      const input = event.target;
      if (!input.matches?.("input[data-cell]")) return;
      const whole = input.selectionStart === 0 && input.selectionEnd === input.value.length;
      const text = input.dataset.x === "line";
      if (event.key === "Enter" || event.key === "ArrowDown" || event.key === "ArrowUp") {
        event.preventDefault();
        move(input, event.key === "ArrowUp" || (event.key === "Enter" && event.shiftKey) ? -1 : 1, 0);
      } else if (!text && event.key === "ArrowLeft" && (whole || input.selectionEnd === 0)) {
        event.preventDefault();
        move(input, 0, -1);
      } else if (!text && event.key === "ArrowRight" && (whole || input.selectionStart === input.value.length)) {
        event.preventDefault();
        move(input, 0, 1);
      } else if (event.key === "Escape") {
        input.value = input.dataset.raw || "";
        input.removeAttribute("aria-invalid");
        input.select();
      }
    });

    // ---- Today: the page opens there, and the button leads back to it.
    function toToday(focus) {
      const grid = sheet.dataset.view === "grid";
      const target = (!grid && sheet.querySelector("#today")) || sheet.querySelector("[data-week][data-current]");
      if (!target) return;
      const top = target.getBoundingClientRect().top - scroller.getBoundingClientRect().top + scroller.scrollTop;
      scroller.scrollTop = Math.max(0, top - (grid ? 8 : scroller.clientHeight * 0.4));
      if (focus) {
        const field = sheet.querySelector('[data-focus]') || target.querySelector("input[data-cell]") ||
          sheet.querySelector('input[data-cell][data-date="' + sheet.dataset.today + '"]');
        field?.focus({ preventScroll: true });
      }
    }
    sheet.querySelector("[data-go-today]")?.addEventListener("click", (event) => {
      event.preventDefault();
      toToday(true);
    });

    // ---- Older weeks load while scrolling up; what was on the screen stays where it was.
    let loading = false;
    const observer = new IntersectionObserver(async (seen) => {
      const link = sheet.querySelector("[data-load-older]");
      if (loading || !link || !seen.some((entry) => entry.isIntersecting)) return;
      loading = true;
      try {
        const answer = await fetch(link.dataset.part, { credentials: "same-origin" });
        if (!answer.ok) return;
        const part = new DOMParser().parseFromString(await answer.text(), "text/html").querySelector("[data-sheet-part]");
        const weeks = part?.querySelector("[data-weeks]");
        if (!weeks) return;
        const height = scroller.scrollHeight;
        const top = scroller.scrollTop;
        observer.unobserve(link);
        const added = [...weeks.children];
        sheet.querySelector("[data-sentinel]").after(...added);
        merge(part.outerHTML, true);
        if (added.length) sheet.dataset.first = added[0].dataset.week;
        const back = document.querySelector('#sheet-prefs input[name="back"]');
        if (back) back.value = "/?from=" + sheet.dataset.first;
        scroller.scrollTop = top + (scroller.scrollHeight - height);
        const next = sheet.querySelector("[data-load-older]");
        if (next) observer.observe(next);
      } finally {
        loading = false;
      }
    }, { root: scroller, rootMargin: "300px 0px 0px 0px" });

    // ---- The preferences: opening an employer's projects reloads the page where it was.
    document.getElementById("sheet-prefs")?.addEventListener("submit", () => {
      try { sessionStorage.setItem("epd-sheet-scroll", String(scroller.scrollTop)); } catch { /* private window */ }
    });

    let kept = null;
    try {
      kept = sessionStorage.getItem("epd-sheet-scroll");
      sessionStorage.removeItem("epd-sheet-scroll");
    } catch { /* private window */ }
    if (kept !== null) scroller.scrollTop = Number(kept);
    else if (location.hash.startsWith("#week-")) document.getElementById(location.hash.slice(1))?.scrollIntoView({ block: "start" });
    else toToday(false);
    if (document.activeElement?.matches?.("input[data-cell]")) enter(document.activeElement);
    const first = sheet.querySelector("[data-load-older]");
    if (first) observer.observe(first);
  }

  // ---- Installable on a phone: the service worker keeps the assets of this build.
  // Not inside another application: the worker's scope would be the application's, not ours.
  if ("serviceWorker" in navigator && window.isSecureContext && !document.documentElement.hasAttribute("data-embedded")) {
    navigator.serviceWorker.register("/sw.js").catch(() => { /* a browser that refuses still works */ });
  }
})();
