"use strict";
// Client-side Plex PIN login (popup + poll), same approach as plex-photo / Overseerr.
// The browser asks plex.tv for a PIN, opens the Plex auth page in a popup, polls
// until the PIN yields an authToken, then posts that token to our backend.
// No server-side callback URL is required (works on localhost, LAN IP, proxy…).
(function () {
  const PIN_URL = "https://plex.tv/api/v2/pins?strong=true";
  const AUTH_URL = "https://app.plex.tv/auth/#!?";
  const CLIENT_ID_KEY = "yr-plex-client-id";

  function clientId() {
    let id = localStorage.getItem(CLIENT_ID_KEY);
    if (!id) {
      id =
        typeof crypto !== "undefined" && crypto.randomUUID
          ? crypto.randomUUID()
          : "yr-" + Math.random().toString(36).slice(2) + Date.now().toString(36);
      localStorage.setItem(CLIENT_ID_KEY, id);
    }
    return id;
  }

  function headers(product) {
    return {
      Accept: "application/json",
      "X-Plex-Product": product,
      "X-Plex-Client-Identifier": clientId()
    };
  }

  function encode(params) {
    return Object.keys(params)
      .map(function (k) {
        return [k, params[k]].map(encodeURIComponent).join("=");
      })
      .join("&");
  }

  async function getProduct() {
    try {
      const res = await fetch("/api/auth/plex/config");
      if (res.ok) {
        const data = await res.json();
        if (data && data.product) return data.product;
      }
    } catch (_) {}
    return "Yomi Reader";
  }

  async function getPin(product) {
    const res = await fetch(PIN_URL, { method: "POST", headers: headers(product) });
    if (!res.ok) throw new Error("Could not start Plex sign-in (" + res.status + ").");
    const data = await res.json();
    return { id: data.id, code: data.code };
  }

  // Soft-cancel on popup.closed only after a grace period — COOP can make
  // popup.closed unreliable after navigating to app.plex.tv.
  function pollToken(pin, product, popup) {
    const POLL_INTERVAL_MS = 1000;
    const TIMEOUT_MS = 5 * 60 * 1000;
    const deadline = Date.now() + TIMEOUT_MS;
    let popupClosedAt = 0;

    return new Promise(function (resolve, reject) {
      const tick = async function () {
        try {
          const url =
            "https://plex.tv/api/v2/pins/" +
            pin.id +
            "?" +
            encode({ code: pin.code });
          const res = await fetch(url, { headers: headers(product) });
          const data = await res.json();
          if (data && data.authToken) {
            resolve(data.authToken);
            return;
          }

          if (popup && popup.closed) {
            if (!popupClosedAt) popupClosedAt = Date.now();
            if (Date.now() - popupClosedAt > 3000) {
              reject(new Error("Sign-in was cancelled."));
              return;
            }
          } else {
            popupClosedAt = 0;
          }

          if (Date.now() > deadline) {
            reject(new Error("Sign-in timed out. Please try again."));
            return;
          }
          setTimeout(tick, POLL_INTERVAL_MS);
        } catch (e) {
          if (Date.now() > deadline) {
            reject(e);
            return;
          }
          setTimeout(tick, POLL_INTERVAL_MS);
        }
      };
      tick();
    });
  }

  function centeredPopup(w, h) {
    const left = window.screenX + Math.max(0, (window.outerWidth - w) / 2);
    const top = window.screenY + Math.max(0, (window.outerHeight - h) / 2);
    return window.open(
      "about:blank",
      "plex-auth",
      "scrollbars=yes,width=" + w + ",height=" + h + ",top=" + top + ",left=" + left
    );
  }

  async function login(setStatus) {
    const popup = centeredPopup(600, 700);
    try {
      if (!popup) {
        // Popup blocked: fall back to server redirect flow (has forwardUrl).
        window.location.href = "/auth/login";
        return;
      }
      const product = await getProduct();
      const pin = await getPin(product);
      const params = {
        clientID: clientId(),
        code: pin.code,
        "context[device][product]": product,
        "context[device][deviceName]": product
      };
      popup.location.href = AUTH_URL + encode(params);

      if (setStatus) setStatus("Waiting for Plex sign-in\u2026");
      const token = await pollToken(pin, product, popup);
      if (popup && !popup.closed) popup.close();

      const res = await fetch("/api/auth/plex/exchange", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        credentials: "same-origin",
        body: JSON.stringify({ token: token })
      });
      let data = {};
      try {
        data = await res.json();
      } catch (_) {}
      if (!res.ok) throw new Error(data.error || "Sign-in failed.");
    } catch (e) {
      if (popup && !popup.closed) popup.close();
      throw e;
    }
  }

  // Bind a SPA-rendered button (DOMContentLoaded alone is too early for hash views).
  function bind(btn, labelEl, errEl) {
    if (!btn || btn.dataset.plexBound === "1") return;
    btn.dataset.plexBound = "1";
    const baseLabel = labelEl ? labelEl.textContent : "Sign in with Plex";

    function setStatus(text) {
      if (labelEl && text) labelEl.textContent = text;
    }
    function showError(msg) {
      if (!errEl) return;
      errEl.textContent = msg || "";
      errEl.hidden = !msg;
    }

    btn.addEventListener("click", async function () {
      btn.disabled = true;
      showError("");
      setStatus("Connecting\u2026");
      try {
        let provider = "plex";
        try {
          const res = await fetch("/api/auth/info", { credentials: "same-origin" });
          if (res.ok) provider = (await res.json()).provider || "plex";
        } catch (_) {}
        if (provider === "mock") {
          window.location.href = "/auth/login";
          return;
        }
        await login(setStatus);
        window.location.hash = "#/";
        window.location.reload();
      } catch (e) {
        showError(e.message || "Sign-in failed.");
        if (labelEl) labelEl.textContent = baseLabel;
        btn.disabled = false;
      }
    });
  }

  window.YomiPlex = { login: login, bind: bind };
})();
