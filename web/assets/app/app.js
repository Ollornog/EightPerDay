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
          const answer = await fetch("/api/parse?line=" + encodeURIComponent(line), { credentials: "same-origin" });
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

  // ---- The list of every week: it opens at the newest week, at the bottom, and the weeks before load while
  // scrolling up. The page's own link at the top does the same without JavaScript.
  if (document.querySelector("[data-endless]")) {
    if (!location.hash) window.scrollTo({ top: document.documentElement.scrollHeight, behavior: "instant" });
    let loading = false;
    const observer = new IntersectionObserver(async (seen) => {
      const link = document.querySelector("[data-load-older]");
      if (loading || !link || !seen.some((entry) => entry.isIntersecting)) return;
      loading = true;
      try {
        const response = await fetch(link.dataset.part, { credentials: "same-origin" });
        if (!response.ok) return;
        const part = new DOMParser().parseFromString(await response.text(), "text/html").querySelector("[data-weeks-part]");
        if (!part) return;
        const height = document.documentElement.scrollHeight;
        observer.unobserve(link);
        link.replaceWith(...part.childNodes);
        // What was on the screen stays there: the page grew above it.
        window.scrollBy({ top: document.documentElement.scrollHeight - height, behavior: "instant" });
        const next = document.querySelector("[data-load-older]");
        if (next) observer.observe(next);
      } finally {
        loading = false;
      }
    }, { rootMargin: "300px 0px 0px 0px" });
    const first = document.querySelector("[data-load-older]");
    if (first) observer.observe(first);
  }

  // ---- Installable on a phone: the service worker keeps the assets of this build.
  if ("serviceWorker" in navigator && window.isSecureContext) {
    navigator.serviceWorker.register("/sw.js").catch(() => { /* a browser that refuses still works */ });
  }
})();
