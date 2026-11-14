/* Yomi Reader — vanilla SPA. No build step, no framework. */
(function () {
  'use strict';

  var t = window.t;

  /* ================================================================
   * Utilities
   * ================================================================ */
  var $ = function (sel, root) { return (root || document).querySelector(sel); };

  function h(tag, attrs) {
    var el = document.createElement(tag);
    if (attrs) {
      for (var k in attrs) {
        var v = attrs[k];
        if (v == null || v === false) continue;
        if (k === 'class') el.className = v;
        else if (k === 'html') el.innerHTML = v; // trusted static strings only
        else if (k === 'style' && typeof v === 'object') Object.assign(el.style, v);
        else if (k.slice(0, 2) === 'on') el.addEventListener(k.slice(2), v);
        else if (k === 'value' || k === 'checked' || k === 'selected' || k === 'disabled' || k === 'hidden') el[k] = v;
        else el.setAttribute(k, v === true ? '' : v);
      }
    }
    for (var i = 2; i < arguments.length; i++) add(el, arguments[i]);
    return el;
  }
  function add(el, kid) {
    if (kid == null || kid === false) return;
    if (Array.isArray(kid)) { kid.forEach(function (k) { add(el, k); }); return; }
    el.appendChild(kid.nodeType ? kid : document.createTextNode(String(kid)));
  }
  function clear(el) { while (el.firstChild) el.removeChild(el.firstChild); return el; }

  var ICONS = {
    back: '<path d="M15 5l-7 7 7 7"/>',
    left: '<path d="M15 5l-7 7 7 7"/>',
    right: '<path d="M9 5l7 7-7 7"/>',
    heart: '<path d="M12 20.5s-7.5-4.6-9.2-9.3C1.6 7.700 4 4.500 7.200 4.500c1.900 0 3.700 1 4.800 2.700 1.100-1.700 2.900-2.700 4.800-2.700 3.200 0 5.600 3.200 4.400 6.700-1.700 4.700-9.200 9.300-9.200 9.300z"/>',
    play: '<path d="M7 4.500v15l12-7.500z"/>',
    out: '<path d="M10 4H5v16h5M15 8l4 4-4 4M19 12H9"/>',
    shield: '<path d="M12 3l8 3v6c0 4.500-3.200 8-8 9-4.800-1-8-4.500-8-9V6z"/>',
    full: '<path d="M4 9V4h5M20 9V4h-5M4 15v5h5M20 15v5h-5"/>',
    x: '<path d="M6 6l12 12M18 6L6 18"/>',
    folder: '<path d="M3 6.500A1.500 1.500 0 014.500 5H9l2 2h8.500A1.500 1.500 0 0121 8.500v9a1.500 1.500 0 01-1.500 1.500h-15A1.500 1.500 0 013 17.500z"/>',
    refresh: '<path d="M20 11a8 8 0 10-2.300 5.700M20 4v7h-7"/>',
    check: '<path d="M5 12.500l4.500 4.500L19 7.500"/>',
    sort: '<path d="M7 4v16M7 20l-3-3M7 20l3-3M17 20V4M17 4l-3 3M17 4l3 3"/>',
    search: '<circle cx="11" cy="11" r="6.500"/><path d="M16 16l4.500 4.500"/>',
    plus: '<path d="M12 5v14M5 12h14"/>',
    trash: '<path d="M5 7h14M10 7V4h4v3M7 7l1 13h8l1-13"/>',
    edit: '<path d="M4 20h4L19 9l-4-4L4 16zM13.500 6.500l4 4"/>',
    up: '<path d="M5 15l7-7 7 7"/>',
    down: '<path d="M5 9l7 7 7-7"/>',
    chevron: '<path d="M6 9l6 6 6-6"/>'
  };
  function icon(name, extra) {
    return h('span', {
      class: 'ic' + (extra ? ' ' + extra : ''), 'aria-hidden': 'true',
      html: '<svg viewBox="0 0 24 24" width="22" height="22" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">' + (ICONS[name] || '') + '</svg>'
    });
  }
  function iconBtn(name, label, onclick, cls) {
    return h('button', { class: 'ibtn' + (cls ? ' ' + cls : ''), type: 'button', 'aria-label': label, title: label, onclick: onclick }, icon(name));
  }

  function debounce(fn, ms) {
    var id;
    var d = function () { var a = arguments, self = this; clearTimeout(id); id = setTimeout(function () { fn.apply(self, a); }, ms); };
    d.cancel = function () { clearTimeout(id); };
    d.flush = function () { clearTimeout(id); fn(); };
    return d;
  }
  function clamp(v, a, b) { return Math.max(a, Math.min(b, v)); }
  function enc(s) { return encodeURIComponent(s); }

  /** Localized label for a chapter edition token (official / unofficial / …). */
  function editionLabel(ed) {
    if (!ed) return '';
    var key = 'edition_' + String(ed).toLowerCase();
    var s = t(key);
    if (s !== key) return s;
    return String(ed).charAt(0).toUpperCase() + String(ed).slice(1).toLowerCase();
  }

  /**
   * Label for a chapter copy in source/edition switchers.
   * Same source + edition → just the edition; otherwise "Source · Edition".
   */
  function copyLabel(c, peer) {
    if (!c) return '';
    var ed = editionLabel(c.edition);
    if (peer && c.sourceName === peer.sourceName && ed) return ed;
    if (ed) return (c.sourceName || '') + ' · ' + ed;
    return c.sourceName || ed || '';
  }
  function fmtDate(s) {
    if (!s) return '—';
    var d = new Date(s);
    if (isNaN(d)) return '—';
    return d.toLocaleString(S.lang === 'fr' ? 'fr-FR' : 'en-GB', { dateStyle: 'medium', timeStyle: 'short' });
  }
  function validDate(s) {
    if (!s) return null;
    var d = new Date(s);
    return isNaN(d) || d.getFullYear() < 1971 ? null : d;
  }
  function fmtRel(s) {
    var d = validDate(s);
    if (!d) return '';
    var sec = Math.round((Date.now() - d.getTime()) / 1000);
    if (sec < 45) return t('rel_just_now');
    if (sec < 3600) return t('rel_minutes', { n: Math.max(1, Math.floor(sec / 60)) });
    if (sec < 86400) return t('rel_hours', { n: Math.floor(sec / 3600) });
    if (sec < 86400 * 14) return t('rel_days', { n: Math.floor(sec / 86400) });
    return d.toLocaleDateString(S.lang === 'fr' ? 'fr-FR' : 'en-GB', { dateStyle: 'medium' });
  }
  // Short chapter date: "8 Nov" / "8 nov.", with year when not current.
  function fmtShortDate(s) {
    var d = validDate(s);
    if (!d) return '';
    var opts = { day: 'numeric', month: 'short' };
    if (d.getFullYear() !== new Date().getFullYear()) opts.year = 'numeric';
    return d.toLocaleDateString(S.lang === 'fr' ? 'fr-FR' : 'en-GB', opts);
  }
  function chapterWhen(c) {
    return fmtShortDate(c.releasedAt || c.scannedAt);
  }
  function fmtNum(n) {
    return (Math.round(n * 100) / 100).toString();
  }

  /* ================================================================
   * State + API
   * ================================================================ */
  var S = {
    info: null, me: null, prefs: {}, authed: false,
    lang: window.getLang(),
    token: 0, cleanup: null, pendingSave: null,
    chCache: {}, meta: null, metaMode: null,
    ageMode: 'safe', // 'safe' | 'adult' when the user can see X
    forceRoute: false
  };

  function canSeeAdult() {
    return !!(S.me && (S.me.isAdmin || S.me.maxAgeRating === 'X'));
  }
  function catalogAgeMode() {
    if (!canSeeAdult()) return '';
    return S.ageMode === 'adult' ? 'adult' : 'safe';
  }
  function setAgeMode(mode) {
    var next = mode === 'adult' ? 'adult' : 'safe';
    if (S.ageMode === next) return;
    S.ageMode = next;
    S.meta = null;
    S.metaMode = null;
  }
  function withAgeMode(path) {
    var mode = catalogAgeMode();
    if (!mode) return path;
    return path + (path.indexOf('?') >= 0 ? '&' : '?') + 'ageMode=' + enc(mode);
  }

  async function api(path, opts) {
    opts = opts || {};
    var headers = Object.assign({}, opts.headers || {});
    var init = { credentials: 'same-origin', method: opts.method || 'GET', headers: headers };
    if (opts.json !== undefined) { headers['Content-Type'] = 'application/json'; init.body = JSON.stringify(opts.json); }
    if (opts.keepalive) init.keepalive = true;
    var r = await fetch(path, init);
    if (r.status === 401 && !opts.noRedirect) {
      S.authed = false;
      if (S.info && S.info.provider === 'mock') location.href = '/auth/login';
      else if (location.hash !== '#/login') location.hash = '#/login';
      throw new Error('unauthorized');
    }
    var ct = r.headers.get('content-type') || '';
    var body = ct.indexOf('json') >= 0 ? await r.json() : await r.text();
    if (!r.ok) throw new Error((body && body.error) || (typeof body === 'string' && body) || r.statusText);
    return body;
  }

  function toast(msg, kind) {
    var el = h('div', { class: 'toast' + (kind ? ' ' + kind : ''), role: 'status' }, msg);
    $('#toasts').appendChild(el);
    setTimeout(function () { el.classList.add('out'); setTimeout(function () { el.remove(); }, 300); }, kind === 'err' ? 4500 : 2200);
  }
  function errToast(e) { if (e && e.message !== 'unauthorized') toast(e.message || String(e), 'err'); }

  async function savePrefs(patch) {
    Object.assign(S.prefs, patch);
    try { await api('/api/preferences', { method: 'PUT', json: S.prefs }); } catch (e) { /* best effort */ }
  }

  /* ================================================================
   * Router
   * ================================================================ */
  function parseHash() {
    var hs = location.hash.replace(/^#/, '') || '/';
    var qi = hs.indexOf('?');
    var path = qi >= 0 ? hs.slice(0, qi) : hs;
    return {
      parts: path.split('/').filter(Boolean).map(decodeURIComponent),
      query: new URLSearchParams(qi >= 0 ? hs.slice(qi + 1) : '')
    };
  }

  var ROUTES = { '': viewHome, adult: viewHome, login: viewLogin, library: viewLibrary, series: viewSeries, read: viewReader, settings: viewSettings };

  async function route() {
    var rt = parseHash();
    var name = rt.parts[0] || '';
    if (name === 'admin') {
      var qi = location.hash.indexOf('?');
      location.hash = '#/settings' + (qi >= 0 ? location.hash.slice(qi) : '');
      return;
    }
    var view = $('#view');
    var softSettings = !S.forceRoute && name === 'settings' && S.authed && !!view.querySelector('.settings-page');
    S.forceRoute = false;

    var my = ++S.token;
    if (S.cleanup) { try { S.cleanup(); } catch (e) { console.error(e); } S.cleanup = null; }

    document.body.classList.toggle('is-reader', name === 'read');
    document.body.classList.toggle('is-login', name === 'login');
    if (!softSettings) {
      clear(view);
      view.className = 'view v-' + (name || 'home');
      window.scrollTo(0, 0);
    }

    if (!S.authed && name !== 'login') { location.hash = '#/login'; return; }
    if (S.authed && name === 'login') { location.hash = '#/'; return; }
    if (name === 'adult' && !canSeeAdult()) { location.hash = '#/'; return; }
    if (name === '' || name === 'adult') setAgeMode(name === 'adult' ? 'adult' : 'safe');

    renderHeader(name);
    var fn = ROUTES[name];
    if (!fn) { location.hash = '#/'; return; }

    var ctx = {
      token: my,
      alive: function () { return my === S.token; },
      onCleanup: function (f) {
        var prev = S.cleanup;
        S.cleanup = function () { if (prev) prev(); f(); };
      },
      soft: softSettings
    };
    if (!softSettings) {
      view.animate([{ opacity: 0, transform: 'translateY(8px)' }, { opacity: 1, transform: 'none' }], { duration: 240, easing: 'cubic-bezier(.2,.7,.2,1)' });
    }
    try {
      await fn(view, rt, ctx);
    } catch (e) {
      if (!ctx.alive() || (e && e.message === 'unauthorized')) return;
      console.error(e);
      clear(view).appendChild(errorBlock(e, route));
    }
  }

  function errorBlock(e, retry) {
    return h('div', { class: 'page narrow empty' },
      h('h2', { class: 'display' }, t('error_title')),
      h('p', { class: 'muted' }, (e && e.message) || String(e)),
      retry ? h('button', { class: 'btn', type: 'button', onclick: retry }, t('retry')) : null);
  }
  function spinner() { return h('div', { class: 'spin', role: 'status', 'aria-label': t('loading') }); }

  /* ================================================================
   * Header
   * ================================================================ */
  function renderHeader(active) {
    var hdr = $('#hdr');
    if (!S.authed || active === 'login' || active === 'read') { hdr.hidden = true; return; }
    hdr.hidden = false;
    clear(hdr);
    var nav = h('nav', { class: 'nav', 'aria-label': 'Main' },
      navLink('#/', t('nav_home'), active === ''),
      canSeeAdult() ? navLink('#/adult', t('nav_adult'), active === 'adult') : null,
      navLink('#/library', t('nav_library'), active === 'library'));
    hdr.append(
      h('a', { class: 'brand', href: '#/', 'aria-label': 'Yomi' }, 'Yomi', h('i', { class: 'dot' })),
      nav,
      h('div', { class: 'hdr-right' }, accountMenu(active)));
  }
  function navLink(href, label, on) {
    return h('a', { href: href, class: on ? 'on' : '', 'aria-current': on ? 'page' : null }, label);
  }
  function userAvatar(cls) {
    var name = (S.me && S.me.username) || '?';
    return h('span', { class: 'avatar' + (cls ? ' ' + cls : ''), 'aria-hidden': 'true' }, initial(name));
  }
  function accountMenu(active) {
    var me = S.me || {};
    var open = false;
    var menu = h('div', {
      class: 'account-menu',
      role: 'menu',
      hidden: true
    },
      h('div', { class: 'account-head' },
        userAvatar('lg'),
        h('strong', { class: 'account-name' }, me.username || ''),
        me.email ? h('span', { class: 'account-sub' }, me.email) : null,
        me.isAdmin ? h('span', { class: 'badge' }, t('admin_badge')) : null),
      h('div', { class: 'account-links' },
        h('a', {
          class: 'account-item' + (active === 'settings' ? ' on' : ''),
          href: '#/settings',
          role: 'menuitem',
          onclick: function () { setOpen(false); }
        }, t('nav_settings'))),
      h('div', { class: 'account-foot' },
        h('button', {
          class: 'account-item',
          type: 'button',
          role: 'menuitem',
          onclick: function () { setOpen(false); logout(); }
        }, t('logout'))));
    var btn = h('button', {
      class: 'who' + (active === 'settings' ? ' on' : ''),
      type: 'button',
      'aria-haspopup': 'menu',
      'aria-expanded': 'false',
      'aria-label': t('account_menu'),
      title: me.username || t('account_menu'),
      onclick: function (e) { e.stopPropagation(); setOpen(!open); }
    }, userAvatar(), icon('chevron', 'xs'));
    var wrap = h('div', { class: 'account' }, btn, menu);

    function detach() {
      document.removeEventListener('click', onDoc);
      document.removeEventListener('keydown', onKey);
    }
    function onDoc(e) {
      if (!wrap.isConnected) { detach(); return; }
      if (!wrap.contains(e.target)) setOpen(false);
    }
    function onKey(e) {
      if (!wrap.isConnected) { detach(); return; }
      if (e.key === 'Escape') setOpen(false);
    }
    function setOpen(v) {
      if (open === v) return;
      open = v;
      menu.hidden = !open;
      btn.setAttribute('aria-expanded', open ? 'true' : 'false');
      wrap.classList.toggle('open', open);
      if (open) {
        setTimeout(function () {
          document.addEventListener('click', onDoc);
          document.addEventListener('keydown', onKey);
        }, 0);
      } else {
        detach();
      }
    }
    return wrap;
  }
  function langToggle() {
    return h('div', { class: 'lang', role: 'group', 'aria-label': t('language') },
      ['en', 'fr'].map(function (l) {
        return h('button', {
          type: 'button', class: S.lang === l ? 'on' : '', 'aria-pressed': S.lang === l ? 'true' : 'false',
          onclick: function () { setLanguage(l); }
        }, l.toUpperCase());
      }));
  }
  function setLanguage(l) {
    if (l === S.lang) return;
    S.lang = l;
    window.setLang(l);
    if (S.authed) savePrefs({ lang: l });
    S.forceRoute = true;
    route();
  }
  async function logout() {
    try { await api('/api/auth/logout', { method: 'POST', noRedirect: true }); } catch (e) { /* ignore */ }
    S.authed = false; S.me = null;
    if (S.info && S.info.provider === 'mock') { sessionStorage.setItem('yr_logged_out', '1'); }
    location.hash = '#/login';
  }

  /* ================================================================
   * Shared components
   * ================================================================ */
  function initial(title) { return (title || '?').trim().charAt(0).toUpperCase() || '?'; }

  function coverSrc(s) {
    if (!s || !s.coverUrl) return '';
    var v = s.updatedAt || '';
    return s.coverUrl + (s.coverUrl.indexOf('?') >= 0 ? '&' : '?') + 'v=' + encodeURIComponent(v);
  }

  function tile(s, o) {
    o = o || {};
    var cover = h('div', { class: 'cover' }, h('span', { class: 'ph', 'aria-hidden': 'true' }, initial(s.title)));
    if (s.hasCover !== false && s.coverUrl) {
      var img = h('img', { src: coverSrc(s), alt: '', loading: 'lazy', decoding: 'async', draggable: 'false' });
      img.addEventListener('load', function () { img.classList.add('ok'); });
      img.addEventListener('error', function () { img.remove(); });
      cover.appendChild(img);
    }
    if (s.favorite && !o.noFav) cover.appendChild(h('span', { class: 'fav-mark', 'aria-hidden': 'true' }, icon('heart')));
    if (o.progress != null) cover.appendChild(h('span', { class: 'bar' }, h('i', { style: { width: Math.round(clamp(o.progress, 0, 1) * 100) + '%' } })));
    var subText = o.sub != null ? o.sub : (s.chapterCount != null ? t('n_chapters', { n: s.chapterCount }, s.chapterCount) : null);
    var showAge = (s.ageRating === 'M' || s.ageRating === 'X') && S.ageMode !== 'adult';
    var sub = null;
    if (subText != null || showAge) {
      sub = h('span', { class: 'ts' });
      if (subText != null) sub.appendChild(document.createTextNode(subText));
      if (showAge) {
        if (subText != null) sub.appendChild(document.createTextNode(' · '));
        sub.appendChild(h('span', { class: 'age-tag', title: t('age_rating') }, s.ageRating));
      }
    }
    return h('a', { class: 'tile', href: o.href || ('#/series/' + enc(s.id)), title: s.title },
      cover,
      h('span', { class: 'tt' }, s.title),
      sub);
  }

  function shelf(title, items, builder, seeAllHref) {
    var track = h('div', { class: 'track' }, items.map(builder));
    var step = function (dir) { track.scrollBy({ left: dir * track.clientWidth * 0.85, behavior: 'smooth' }); };
    return h('section', { class: 'shelf' },
      h('div', { class: 'shelf-head' },
        h('h2', { class: 'display' }, title),
        h('div', { class: 'shelf-ctl' },
          seeAllHref ? h('a', { class: 'link', href: seeAllHref }, t('see_all')) : null,
          iconBtn('left', t('scroll_left'), function () { step(-1); }, 'sm hide-touch'),
          iconBtn('right', t('scroll_right'), function () { step(1); }, 'sm hide-touch'))),
      track);
  }

  function emptyState(title, text, actions) {
    return h('div', { class: 'empty' },
      h('div', { class: 'empty-mark', 'aria-hidden': 'true' }, 'よ'),
      h('h2', { class: 'display' }, title),
      text ? h('p', { class: 'muted' }, text) : null,
      actions ? h('div', { class: 'row center' }, actions) : null);
  }

  function field(label, input, hint) {
    return h('label', { class: 'field' }, h('span', { class: 'lbl' }, label), input, hint ? h('small', { class: 'muted' }, hint) : null);
  }

  /* ================================================================
   * Login
   * ================================================================ */
  function viewLogin(root) {
    var info = S.info || {};
    var err = h('p', { class: 'form-err', role: 'alert', hidden: true });
    var box = h('div', { class: 'login' },
      h('div', { class: 'login-top' }, langToggle()),
      h('div', { class: 'login-brand' },
        h('h1', { class: 'logo' }, 'Yomi', h('i', { class: 'dot' })),
        h('p', { class: 'tag' }, t('tagline'))));

    if (info.provider === 'local') {
      var user = h('input', { type: 'text', name: 'username', autocomplete: 'username', required: true, autocapitalize: 'none', spellcheck: 'false' });
      var pass = h('input', { type: 'password', name: 'password', autocomplete: 'current-password', required: true });
      var btn = h('button', { class: 'btn primary wide', type: 'submit' }, t('sign_in'));
      var form = h('form', {
        class: 'login-form',
        onsubmit: async function (e) {
          e.preventDefault();
          btn.disabled = true; err.hidden = true;
          try {
            await api('/api/auth/login', { method: 'POST', json: { username: user.value.trim(), password: pass.value }, noRedirect: true });
            await bootSession();
            location.hash = '#/';
          } catch (ex) {
            err.textContent = ex.message; err.hidden = false; btn.disabled = false; pass.select();
          }
        }
      }, field(t('username'), user), field(t('password'), pass), err, btn);
      box.appendChild(form);
      setTimeout(function () { user.focus(); }, 60);
    } else if (info.provider === 'plex') {
      err.id = 'login-error';
      var plexLabel = h('span', { id: 'plex-login-label' }, t('sign_in_plex'));
      var plexBtn = h('button', { id: 'plex-login-btn', class: 'btn primary wide', type: 'button' }, plexLabel);
      box.appendChild(h('div', { class: 'login-form' }, plexBtn, err));
      if (window.YomiPlex && typeof window.YomiPlex.bind === 'function') {
        window.YomiPlex.bind(plexBtn, plexLabel, err);
      } else {
        plexBtn.addEventListener('click', function () { location.href = '/auth/login'; });
      }
    } else {
      box.appendChild(h('div', { class: 'login-form' },
        h('a', { class: 'btn primary wide', href: '/auth/login' }, t('sign_in_sso'))));
    }
    root.appendChild(box);
  }

  /* ================================================================
   * Home
   * ================================================================ */
  async function viewHome(root, rt, ctx) {
    var adult = rt.parts[0] === 'adult';
    root.appendChild(spinner());
    var d = await api(withAgeMode('/api/home'));
    if (!ctx.alive()) return;
    clear(root);
    var page = h('div', { class: 'page' });
    var cont = d.continue || [], favs = d.favorites || [], added = d.recentlyAdded || [];
    var updated = d.recentlyUpdated || [], random = d.random || [];
    if (!cont.length && !favs.length && !added.length && !updated.length && !random.length) {
      page.appendChild(emptyState(
        adult ? t('empty_adult_title') : t('empty_lib_title'),
        adult ? t('empty_adult_text') : t('empty_lib_text'),
        !adult && S.me && S.me.isAdmin ? h('a', { class: 'btn primary', href: '#/settings?tab=libraries' }, t('add_library')) : null));
    } else {
      if (cont.length) page.appendChild(shelf(t('continue_reading'), cont, function (c) {
        var pc = c.series.chapterCount > 0 ? c.chapterNumber / c.series.chapterCount : null;
        var n = c.chapterNumber >= 0 ? t('ch_short', { n: fmtNum(c.chapterNumber) }) : c.chapterTitle;
        return tile(c.series, {
          href: '#/read/' + enc(c.chapterId), sub: n + (c.pageIndex ? ' · ' + t('p_short', { n: c.pageIndex + 1 }) : ''),
          progress: pc, noFav: true
        });
      }));
      if (favs.length) page.appendChild(shelf(t('favorites'), favs, function (s) { return tile(s, { noFav: true }); }, '#/library?fav=1'));
      if (added.length) page.appendChild(shelf(t('recently_added'), added, function (s) { return tile(s); }, '#/library?sort=added&order=desc'));
      if (updated.length) page.appendChild(shelf(t('recently_updated'), updated, function (s) { return tile(s); }, '#/library?sort=updated&order=desc'));
      if (random.length) page.appendChild(shelf(t('random'), random, function (s) { return tile(s); }, '#/library?sort=random'));
    }
    root.appendChild(page);
  }

  /* ================================================================
   * Browse (full catalog — Home/Adult age split does not apply here)
   * ================================================================ */
  async function viewLibrary(root, rt, ctx) {
    var qp = rt.query;
    var f = {
      q: qp.get('q') || '', tags: (qp.get('tag') || '').split(',').filter(Boolean),
      source: qp.get('source') || '', age: qp.get('age') || '',
      sort: qp.get('sort') || 'title', order: qp.get('order') || (qp.get('sort') === 'added' || qp.get('sort') === 'updated' ? 'desc' : 'asc'),
      fav: qp.get('fav') === '1'
    };
    if (f.age === 'X' && !canSeeAdult()) f.age = '';
    var PAGE = 60, offset = 0, total = null, loading = false, seq = 0, done = false;

    var page = h('div', { class: 'page' });
    var title = h('h1', { class: 'display page-title' }, t('nav_library'), ' ', h('span', { class: 'count' }));
    var search = h('input', { type: 'search', class: 'search', placeholder: t('search_ph'), 'aria-label': t('search_ph'), value: f.q, autocomplete: 'off', enterkeyhint: 'search' });
    var sortSel = h('select', { 'aria-label': t('sort') },
      ['title', 'added', 'updated', 'chapters', 'year', 'random'].map(function (s) { return h('option', { value: s, selected: f.sort === s }, t('sort_' + s)); }));
    var orderBtn = h('button', { type: 'button', class: 'ibtn', 'aria-label': t('order'), title: t('order'), onclick: function () { f.order = f.order === 'asc' ? 'desc' : 'asc'; paintOrder(); reload(); } }, icon('sort'));
    var srcSel = h('select', { 'aria-label': t('source') }, h('option', { value: '' }, t('all_sources')));
    var ageOpts = canSeeAdult() ? ['E', 'T', 'M', 'X'] : ['E', 'T', 'M'];
    var ageSel = h('select', { 'aria-label': t('age_limit') },
      h('option', { value: '' }, t('all_ages')),
      ageOpts.map(function (a) { return h('option', { value: a, selected: f.age === a }, t('age_up_to', { a: a })); }));
    var favBtn = h('button', { type: 'button', class: 'chip', 'aria-pressed': f.fav ? 'true' : 'false', onclick: function () { f.fav = !f.fav; favBtn.setAttribute('aria-pressed', f.fav ? 'true' : 'false'); reload(); } }, icon('heart', 'xs'), t('favorites'));
    var tagsBox = h('div', { class: 'chips', role: 'group', 'aria-label': t('tags') });
    var grid = h('div', { class: 'grid' });
    var more = h('div', { class: 'more' });
    var empty = h('div', { hidden: true });

    function paintOrder() { orderBtn.classList.toggle('desc', f.order === 'desc'); orderBtn.setAttribute('aria-pressed', f.order === 'desc' ? 'true' : 'false'); }
    paintOrder();

    page.append(
      h('div', { class: 'page-head' }, title),
      h('div', { class: 'toolbar' },
        h('div', { class: 'search-wrap' }, icon('search'), search),
        h('div', { class: 'tb-sel' }, sortSel, orderBtn, srcSel, ageSel, favBtn)),
      tagsBox, empty, grid, more);
    root.appendChild(page);

    function syncUrl() {
      var p = new URLSearchParams();
      if (f.q) p.set('q', f.q);
      if (f.tags.length) p.set('tag', f.tags.join(','));
      if (f.source) p.set('source', f.source);
      if (f.age) p.set('age', f.age);
      if (f.fav) p.set('fav', '1');
      if (f.sort !== 'title') p.set('sort', f.sort);
      if (f.order !== (f.sort === 'added' || f.sort === 'updated' ? 'desc' : 'asc')) p.set('order', f.order);
      var s = p.toString();
      history.replaceState(null, '', '#/library' + (s ? '?' + s : ''));
    }

    var io = new IntersectionObserver(function (es) { if (es[0].isIntersecting) load(); }, { rootMargin: '900px 0px' });
    ctx.onCleanup(function () { io.disconnect(); });

    function hasFilters() { return f.q || f.tags.length || f.source || f.age || f.fav; }

    function reload() { syncUrl(); load(true); }

    async function load(reset) {
      if (reset) { offset = 0; total = null; done = false; seq++; loading = false; clear(grid); empty.hidden = true; }
      if (loading || done) return;
      var my = seq;
      loading = true;
      clear(more).appendChild(spinner());
      try {
        var p = new URLSearchParams({ offset: offset, limit: PAGE, sort: f.sort, order: f.order });
        if (f.q) p.set('q', f.q);
        f.tags.forEach(function (x) { p.append('tag', x); });
        if (f.source) p.set('source', f.source);
        if (f.age) p.set('age', f.age);
        if (f.fav) p.set('favorites', '1');
        // Browse lists the full catalog the viewer may see (no Home/Adult ageMode).
        var d = await api('/api/series?' + p);
        if (!ctx.alive() || my !== seq) return;
        var items = d.items || [];
        total = d.total;
        var frag = document.createDocumentFragment();
        items.forEach(function (s) { frag.appendChild(tile(s)); });
        grid.appendChild(frag);
        offset += items.length;
        done = items.length === 0 || offset >= total;
        title.querySelector('.count').textContent = total != null ? '· ' + total : '';
        clear(more);
        if (total === 0) showEmpty();
        else if (!done) {
          more.appendChild(h('button', { class: 'btn', type: 'button', onclick: function () { load(); } }, t('load_more')));
          io.disconnect(); io.observe(more);
        }
      } catch (e) {
        if (my === seq) { clear(more).appendChild(h('button', { class: 'btn', type: 'button', onclick: function () { load(); } }, t('retry'))); errToast(e); }
      } finally {
        if (my === seq) loading = false;
      }
    }

    function showEmpty() {
      clear(empty); empty.hidden = false;
      if (hasFilters()) {
        empty.appendChild(emptyState(t('no_results'), t('no_results_text'),
          h('button', { class: 'btn', type: 'button', onclick: function () { f.q = ''; f.tags = []; f.source = ''; f.age = ''; f.fav = false; search.value = ''; srcSel.value = ''; ageSel.value = ''; favBtn.setAttribute('aria-pressed', 'false'); paintTags(); reload(); } }, t('clear_filters'))));
      } else {
        empty.appendChild(emptyState(
          t('empty_lib_title'),
          t('empty_lib_text'),
          S.me && S.me.isAdmin ? h('a', { class: 'btn primary', href: '#/settings?tab=libraries' }, t('add_library')) : null));
      }
    }

    var tagsAll = [], tagsExpanded = false;
    function paintTags() {
      clear(tagsBox);
      if (!tagsAll.length) { tagsBox.hidden = true; return; }
      tagsBox.hidden = false;
      var sel = f.tags.map(function (n) { return tagsAll.find(function (x) { return x.name === n; }) || { name: n, count: 0 }; });
      var rest = tagsAll.filter(function (x) { return f.tags.indexOf(x.name) < 0; });
      var shown = tagsExpanded ? rest : rest.slice(0, 24);
      sel.concat(shown).forEach(function (x) {
        var on = f.tags.indexOf(x.name) >= 0;
        tagsBox.appendChild(h('button', {
          type: 'button', class: 'chip' + (on ? ' on' : ''), 'aria-pressed': on ? 'true' : 'false',
          onclick: function () {
            if (on) f.tags = f.tags.filter(function (n) { return n !== x.name; }); else f.tags.push(x.name);
            paintTags(); reload();
          }
        }, x.name, x.count ? h('em', null, x.count) : null));
      });
      if (rest.length > 24) tagsBox.appendChild(h('button', { type: 'button', class: 'chip ghost', onclick: function () { tagsExpanded = !tagsExpanded; paintTags(); } }, tagsExpanded ? t('fewer') : t('more_tags', { n: rest.length - 24 })));
    }

    search.addEventListener('input', debounce(function () { f.q = search.value.trim(); reload(); }, 300));
    sortSel.addEventListener('change', function () {
      f.sort = sortSel.value;
      f.order = (f.sort === 'added' || f.sort === 'updated') ? 'desc' : 'asc';
      paintOrder(); reload();
    });
    srcSel.addEventListener('change', function () { f.source = srcSel.value; reload(); });
    ageSel.addEventListener('change', function () { f.age = ageSel.value; reload(); });

    // meta (tags + sources) reflects the full catalog for Browse
    load(true);
    try {
      if (!S.meta || S.metaMode !== 'all') {
        var m = await Promise.all([api('/api/tags'), api('/api/sources')]);
        S.meta = { tags: m[0] || [], sources: m[1] || [] };
        S.metaMode = 'all';
      }
      if (!ctx.alive()) return;
      tagsAll = S.meta.tags.slice().sort(function (a, b) { return b.count - a.count || a.name.localeCompare(b.name); });
      var seen = {};
      S.meta.sources.forEach(function (s) {
        if (seen[s.name]) return; seen[s.name] = 1;
        srcSel.appendChild(h('option', { value: s.name, selected: f.source === s.name }, s.name + ' (' + s.seriesCount + ')'));
      });
      srcSel.value = f.source;
      paintTags();
    } catch (e) { errToast(e); }
  }

  /* ================================================================
   * Series detail
   * ================================================================ */
  async function viewSeries(root, rt, ctx) {
    var id = rt.parts[1];
    root.appendChild(spinner());
    var res = await Promise.all([api('/api/series/' + enc(id)), api('/api/series/' + enc(id) + '/chapters')]);
    if (!ctx.alive()) return;
    var d = res[0], chapters = res[1] || [];
    S.chCache[id] = chapters;
    clear(root);

    var asc = false;
    var favBtn, resumeBox = h('div', { class: 'cta' }), list = h('div', { class: 'chlist' });

    function paintFav() {
      favBtn.classList.toggle('on', d.favorite);
      favBtn.setAttribute('aria-pressed', d.favorite ? 'true' : 'false');
      favBtn.setAttribute('aria-label', d.favorite ? t('unfavorite') : t('favorite'));
      favBtn.title = favBtn.getAttribute('aria-label');
      clear(favBtn).append(icon('heart'), h('span', null, d.favorite ? t('favorited') : t('favorite')));
    }
    favBtn = h('button', {
      type: 'button', class: 'btn lg fav', onclick: async function () {
        var want = !d.favorite;
        d.favorite = want; paintFav();
        try { await api('/api/favorites/' + enc(id), { method: want ? 'PUT' : 'DELETE' }); }
        catch (e) { d.favorite = !want; paintFav(); errToast(e); }
      }
    });

    function paintCta() {
      clear(resumeBox);
      var rs = d.resume, first = d.firstChapterId;
      if (!chapters.length) { resumeBox.appendChild(h('span', { class: 'muted' }, t('no_chapters'))); return; }
      if (rs && !rs.finished) {
        var lbl = rs.chapterNumber >= 0 ? t('ch_short', { n: fmtNum(rs.chapterNumber) }) : rs.chapterTitle;
        resumeBox.appendChild(h('a', { class: 'btn primary lg', href: '#/read/' + enc(rs.chapterId) }, icon('play'), t('continue') + ' · ' + lbl + (rs.pageIndex ? ' · ' + t('p_short', { n: rs.pageIndex + 1 }) : '')));
      } else if (rs && rs.finished) {
        resumeBox.appendChild(h('a', { class: 'btn primary lg', href: '#/read/' + enc(first) + '?p=0' }, icon('refresh'), t('read_again')));
      } else {
        resumeBox.appendChild(h('a', { class: 'btn primary lg', href: '#/read/' + enc(first) }, icon('play'), t('start_reading')));
      }
      if (d.progress) resumeBox.appendChild(h('button', {
        class: 'btn lg', type: 'button', onclick: async function () {
          try { await api('/api/progress/' + enc(id), { method: 'DELETE' }); d.progress = null; d.resume = null; paintCta(); paintList(); toast(t('marked_unread')); } catch (e) { errToast(e); }
        }
      }, t('mark_unread')));
    }

    function paintList() {
      clear(list);
      var curNum = d.progress ? d.progress.chapterNumber : null;
      var curId = d.resume && !d.resume.finished ? d.resume.chapterId : null;
      var rows = asc ? chapters : chapters.slice().reverse();
      var frag = document.createDocumentFragment();
      rows.forEach(function (c) {
        var isRead = d.progress && c.number >= 0 && (c.number < curNum || (c.number === curNum && d.progress.completed));
        var isCur = curId && (c.id === curId || (c.alternatives || []).some(function (a) { return a.id === curId; }));
        var alts = c.alternatives || [];
        var when = chapterWhen(c);
        frag.appendChild(h('div', { class: 'chrow' + (isRead ? ' read' : '') + (isCur ? ' cur' : '') },
          h('a', { class: 'chmain', href: '#/read/' + enc(c.id) },
            h('span', { class: 'chn' }, c.number >= 0 ? fmtNum(c.number) : '—'),
            h('span', { class: 'cht' }, c.title),
            when ? h('span', { class: 'chd', title: c.releasedAt || c.scannedAt || '' }, when) : h('span', { class: 'chd' }),
            h('span', { class: 'chm' }, copyLabel(c) + (c.pageCount ? ' · ' + t('n_pages', { n: c.pageCount }) : '') + (c.format === 'pdf' ? ' · PDF' : '')),
            isRead ? h('span', { class: 'chk', 'aria-label': t('read') }, icon('check', 'xs')) : null),
          alts.length ? h('div', { class: 'alts' }, h('span', { class: 'muted' }, t('also_in')),
            alts.map(function (a) {
              return h('a', { class: 'alt', href: '#/read/' + enc(a.id) },
                copyLabel(a, c), a.pageCount ? ' · ' + a.pageCount : '');
            })) : null));
      });
      list.appendChild(frag);
    }

    var coverBust = coverSrc(d);
    var reloadTok = rt.query.get('_');
    if (coverBust && reloadTok) coverBust += (coverBust.indexOf('?') >= 0 ? '&' : '?') + 'r=' + encodeURIComponent(reloadTok);
    var coverImg = d.hasCover && d.coverUrl ? h('img', { src: coverBust, alt: '', decoding: 'async' }) : null;
    if (coverImg) coverImg.addEventListener('error', function () { coverImg.remove(); });
    var meta = [];
    if (d.status) meta.push(d.status);
    if (d.year) meta.push(String(d.year));
    meta.push(t('n_chapters', { n: d.chapterCount }, d.chapterCount));
    var when = fmtRel(d.updatedAt);
    if (when) meta.push(t('updated', { when: when }));

    var coverWrap = h('div', { class: 'bigcover' },
      h('span', { class: 'ph', 'aria-hidden': 'true' }, initial(d.title)),
      coverImg);
    if (S.me && S.me.isAdmin) {
      coverWrap.appendChild(h('button', {
        type: 'button', class: 'cover-edit', 'aria-label': t('change_cover'), title: t('change_cover'),
        onclick: function () { editDialog(d, chapters, { tab: 'cover' }); }
      }, icon('edit', 'xs'), h('span', null, t('change_cover'))));
    }

    var adminBtns = S.me && S.me.isAdmin ? h('button', {
      class: 'btn lg', type: 'button',
      onclick: function () { editDialog(d, chapters); }
    }, icon('edit', 'xs'), t('edit')) : null;

    var info = h('div', { class: 'info' },
      h('h1', { class: 'display series-title' }, d.title),
      d.authors ? h('p', { class: 'authors' }, d.authors) : null,
      h('p', { class: 'meta' }, h('span', { class: 'age-pill age-' + d.ageRating, title: t('age_rating') }, d.ageRating), meta.join(' · ')),
      d.genres && d.genres.length ? h('div', { class: 'chips' }, d.genres.map(function (g) {
        return h('a', { class: 'chip', href: '#/library?tag=' + enc(g) }, g);
      })) : null,
      d.summary ? h('p', { class: 'summary' }, d.summary) : null,
      resumeBox,
      h('div', { class: 'row' }, favBtn, adminBtns),
      d.sourceDetails && d.sourceDetails.length ? h('div', { class: 'sources' },
        h('h3', { class: 'lbl' }, t('sources')),
        h('ol', null, d.sourceDetails.map(function (s, i) {
          return h('li', { class: i === 0 ? 'best' : '' }, s.name, h('em', null, t('n_chapters', { n: s.chapterCount }, s.chapterCount)));
        }))) : null);

    var page = h('div', { class: 'page series' },
      h('div', { class: 'hero' },
        coverImg ? h('div', { class: 'hero-bg', style: { backgroundImage: 'url("' + coverBust + '")' }, 'aria-hidden': 'true' }) : null,
        h('div', { class: 'hero-in' }, coverWrap, info)),
      h('section', { class: 'chapters' },
        h('div', { class: 'shelf-head' },
          h('h2', { class: 'display' }, t('chapters'), ' ', h('span', { class: 'count' }, chapters.length)),
          h('button', { class: 'btn sm', type: 'button', onclick: function (e) { asc = !asc; e.currentTarget.lastChild.textContent = asc ? t('oldest_first') : t('newest_first'); paintList(); } }, icon('sort', 'xs'), h('span', null, t('newest_first')))),
        list));
    root.appendChild(page);
    paintFav(); paintCta(); paintList();
    document.title = d.title + ' · Yomi';
    ctx.onCleanup(function () { document.title = 'Yomi'; });
  }

  function splitList(s) {
    return String(s || '').split(/[,;]+/).map(function (x) { return x.trim(); }).filter(Boolean);
  }

  function editDialog(series, chapters, opts) {
    opts = opts || {};
    var details = (series.sourceDetails || []).slice();
    var tabs = ['metadata', 'cover', 'sources'];
    var tab = opts.tab && tabs.indexOf(opts.tab) >= 0 ? opts.tab : 'metadata';

    var dlg = h('dialog', { class: 'dlg edit-dlg', 'aria-label': t('edit') });
    var panel = h('div', { class: 'edit-panel' });
    var foot = h('div', { class: 'dlg-foot' });
    var err = h('p', { class: 'form-err', hidden: true });
    var nav = h('nav', { class: 'edit-nav', 'aria-label': t('edit') });

    async function reloadSeries() {
      S.meta = null;
      S.chCache = {};
      var next = '#/series/' + enc(series.id) + '?_=' + Date.now();
      if (location.hash === next) route();
      else location.hash = next;
    }

    function setTab(next) {
      tab = next;
      Array.prototype.forEach.call(nav.children, function (b) {
        var on = b.getAttribute('data-tab') === tab;
        b.classList.toggle('on', on);
        if (on) b.setAttribute('aria-current', 'page');
        else b.removeAttribute('aria-current');
      });
      paintPanel();
    }

    tabs.forEach(function (k) {
      nav.appendChild(h('button', {
        type: 'button', class: k === tab ? 'on' : '', 'data-tab': k,
        'aria-current': k === tab ? 'page' : null,
        onclick: function () { setTab(k); }
      }, t('edit_tab_' + k)));
    });

    /* ---- metadata ---- */
    var titleIn = h('input', { type: 'text', required: true, value: series.title || '', autocomplete: 'off' });
    var authorsIn = h('input', { type: 'text', value: series.authors || '', autocomplete: 'off' });
    var statusKnown = ['Ongoing', 'Completed', 'Hiatus', 'Cancelled'];
    var statusCur = series.status || '';
    var statusIn = h('select', null,
      h('option', { value: '', selected: !statusCur }, t('status_unknown')),
      statusKnown.map(function (s) { return h('option', { value: s, selected: statusCur === s }, s); }));
    if (statusCur && statusKnown.indexOf(statusCur) < 0) {
      statusIn.appendChild(h('option', { value: statusCur, selected: true }, statusCur));
    }
    var yearIn = h('input', { type: 'number', min: 0, max: 9999, step: 1, value: series.year || '' });
    var genresIn = h('input', { type: 'text', value: (series.genres || []).join(', '), autocomplete: 'off',
      placeholder: t('genres_ph') });
    var tagsIn = h('input', { type: 'text', value: (series.tags || []).join(', '), autocomplete: 'off',
      placeholder: t('tags_ph') });
    var summaryIn = h('textarea', { rows: 7, spellcheck: 'true' }, series.summary || '');
    var ageIn = h('select', null,
      h('option', { value: 'auto' }, t('age_auto') + (series.ageRating ? ' (' + series.ageRating + ')' : '')),
      ['E', 'T', 'M', 'X'].map(function (a) {
        return h('option', { value: a }, a + ' — ' + t('age_desc_' + a));
      }));
    ageIn.value = series.ageLocked ? (series.ageRating || 'T') : 'auto';

    async function saveMeta() {
      err.hidden = true;
      var body = {
        title: titleIn.value.trim(),
        authors: authorsIn.value.trim(),
        status: statusIn.value,
        year: yearIn.value === '' ? 0 : parseInt(yearIn.value, 10) || 0,
        genres: splitList(genresIn.value),
        tags: splitList(tagsIn.value),
        summary: summaryIn.value,
        ageRating: ageIn.value,
        metaLocked: true
      };
      if (!body.title) { err.textContent = t('title_required'); err.hidden = false; return; }
      try {
        await api('/api/admin/series/' + enc(series.id), { method: 'PUT', json: body });
        toast(t('saved'));
        dlg.close();
        await reloadSeries();
      } catch (ex) { err.textContent = ex.message; err.hidden = false; }
    }

    /* ---- cover ---- */
    var coverStatus = h('p', { class: 'muted cover-hint' }, t('cover_pick_hint'));
    var coverBusy = false;
    async function applyCover(body, isForm) {
      if (coverBusy) return;
      coverBusy = true;
      coverStatus.textContent = t('cover_uploading');
      try {
        if (isForm) {
          var r = await fetch('/api/admin/series/' + enc(series.id) + '/cover', { method: 'PUT', credentials: 'same-origin', body: body });
          var ct = r.headers.get('content-type') || '';
          var res = ct.indexOf('json') >= 0 ? await r.json() : await r.text();
          if (!r.ok) throw new Error((res && res.error) || r.statusText);
        } else {
          await api('/api/admin/series/' + enc(series.id) + '/cover', { method: 'PUT', json: body });
        }
        toast(t('cover_saved'));
        dlg.close();
        await reloadSeries();
      } catch (e) { errToast(e); coverStatus.textContent = t('cover_pick_hint'); coverBusy = false; }
    }
    var fileIn = h('input', { type: 'file', accept: 'image/jpeg,image/png,image/webp,image/gif,.jpg,.jpeg,.png,.webp,.gif', hidden: true });
    fileIn.addEventListener('change', function () {
      if (!fileIn.files || !fileIn.files[0]) return;
      var fd = new FormData();
      fd.append('file', fileIn.files[0]);
      applyCover(fd, true);
    });
    var coverGrid = h('div', { class: 'cover-grid' });
    (chapters || []).slice(0, 24).forEach(function (c) {
      var label = c.number >= 0 ? t('ch_short', { n: fmtNum(c.number) }) : (c.title || '—');
      var thumb = h('button', {
        type: 'button', class: 'cover-cand', title: label,
        onclick: function () {
          if (thumb.classList.contains('broken') || thumb.disabled) {
            toast(t('cover_chapter_bad'), 'err');
            return;
          }
          applyCover({ chapterId: c.id });
        }
      },
        h('img', { src: '/api/chapters/' + enc(c.id) + '/page/0', alt: '', loading: 'lazy', decoding: 'async' }),
        h('span', null, label));
      thumb.querySelector('img').addEventListener('error', function () {
        thumb.classList.add('broken');
        thumb.disabled = true;
        thumb.title = t('cover_chapter_bad');
      });
      coverGrid.appendChild(thumb);
    });

    /* ---- sources ---- */
    function orderedSources() {
      var custom = (series.sourcePriority || []).slice();
      var byName = {};
      details.forEach(function (s) { byName[s.name.toLowerCase()] = s; });
      var out = [];
      var seen = {};
      custom.forEach(function (name) {
        var key = name.toLowerCase();
        var hit = byName[key] || details.find(function (s) {
          return s.name.toLowerCase().indexOf(key) === 0 || key.indexOf(s.name.toLowerCase().split(' (')[0]) === 0;
        });
        if (hit && !seen[hit.id]) { seen[hit.id] = 1; out.push(hit); }
      });
      details.forEach(function (s) { if (!seen[s.id]) out.push(s); });
      return out;
    }
    var multiSrc = details.length > 1;
    var srcOrder = orderedSources();
    var srcList = h('ol', { class: 'src-prio-list' });
    var srcHint = h('p', { class: 'muted' }, !details.length ? t('no_sources')
      : !multiSrc ? t('series_source_single_hint')
      : (series.sourcePriority && series.sourcePriority.length)
        ? t('series_source_priority_hint')
        : t('series_source_priority_global'));

    function paintSrcList() {
      clear(srcList);
      srcOrder.forEach(function (s, i) {
        var acts = null;
        if (multiSrc) {
          var up = h('button', {
            type: 'button', class: 'btn sm icon-only', disabled: i === 0, title: t('move_up'),
            'aria-label': t('move_up'),
            onclick: function () {
              if (i === 0) return;
              var tmp = srcOrder[i - 1]; srcOrder[i - 1] = srcOrder[i]; srcOrder[i] = tmp;
              paintSrcList();
            }
          }, icon('up', 'xs'));
          var down = h('button', {
            type: 'button', class: 'btn sm icon-only', disabled: i === srcOrder.length - 1, title: t('move_down'),
            'aria-label': t('move_down'),
            onclick: function () {
              if (i >= srcOrder.length - 1) return;
              var tmp = srcOrder[i + 1]; srcOrder[i + 1] = srcOrder[i]; srcOrder[i] = tmp;
              paintSrcList();
            }
          }, icon('down', 'xs'));
          acts = h('span', { class: 'src-prio-acts' }, up, down);
        }
        srcList.appendChild(h('li', { class: multiSrc && i === 0 ? 'best' : '' },
          multiSrc ? h('span', { class: 'src-prio-rank' }, String(i + 1)) : null,
          h('span', { class: 'src-prio-name' }, s.name,
            h('em', null, t('n_chapters', { n: s.chapterCount }, s.chapterCount))),
          acts));
      });
    }
    paintSrcList();

    async function saveSources(clearCustom) {
      err.hidden = true;
      var body = { sourcePriority: clearCustom ? [] : srcOrder.map(function (s) { return s.name; }) };
      try {
        await api('/api/admin/series/' + enc(series.id), { method: 'PUT', json: body });
        toast(t('saved'));
        dlg.close();
        await reloadSeries();
      } catch (ex) { err.textContent = ex.message; err.hidden = false; }
    }

    function fill(el) {
      clear(el);
      for (var i = 1; i < arguments.length; i++) add(el, arguments[i]);
    }

    function paintPanel() {
      err.hidden = true;
      if (tab === 'cover') {
        fill(panel,
          coverStatus,
          h('div', { class: 'row cover-actions' },
            h('button', { class: 'btn', type: 'button', onclick: function () { fileIn.click(); } }, icon('folder', 'xs'), t('cover_upload')),
            h('button', { class: 'btn', type: 'button', onclick: function () { applyCover({ auto: true }); } }, icon('refresh', 'xs'), t('cover_auto'))),
          fileIn,
          (chapters || []).length ? h('h4', { class: 'lbl' }, t('cover_from_chapter')) : null,
          coverGrid);
        fill(foot, h('button', { class: 'btn', type: 'button', onclick: function () { dlg.close(); } }, t('cancel')));
        return;
      }
      if (tab === 'sources') {
        fill(panel,
          h('h4', { class: 'lbl' }, multiSrc ? t('source_priority') : t('sources')),
          srcHint,
          details.length ? srcList : null,
          err);
        if (!multiSrc) {
          fill(foot, h('button', { class: 'btn', type: 'button', onclick: function () { dlg.close(); } }, t('close')));
          return;
        }
        fill(foot,
          (series.sourcePriority && series.sourcePriority.length) ? h('button', {
            class: 'btn meta-unlock-btn', type: 'button',
            onclick: function () { saveSources(true); }
          }, t('source_priority_reset')) : null,
          h('button', { class: 'btn', type: 'button', onclick: function () { dlg.close(); } }, t('cancel')),
          h('button', { class: 'btn primary', type: 'button', onclick: function () { saveSources(false); } }, t('save')));
        return;
      }
      fill(panel,
        field(t('series_title'), titleIn),
        field(t('authors'), authorsIn),
        h('div', { class: 'meta-grid' },
          field(t('status'), statusIn),
          field(t('year'), yearIn),
          field(t('age_rating'), ageIn)),
        field(t('genres'), genresIn, t('list_hint')),
        field(t('tags'), tagsIn, t('list_hint')),
        field(t('summary'), summaryIn),
        series.metaLocked ? h('p', { class: 'muted meta-unlock-hint' }, t('meta_locked_hint')) : null,
        err);
      fill(foot,
        series.metaLocked ? h('button', {
          class: 'btn meta-unlock-btn', type: 'button',
          onclick: async function () {
            try {
              await api('/api/admin/series/' + enc(series.id), { method: 'PUT', json: { metaLocked: false } });
              toast(t('meta_unlocked'));
              dlg.close();
              await reloadSeries();
            } catch (ex) { err.textContent = ex.message; err.hidden = false; }
          }
        }, t('meta_unlock')) : null,
        h('button', { class: 'btn', type: 'button', onclick: function () { dlg.close(); } }, t('cancel')),
        h('button', { class: 'btn primary', type: 'button', onclick: saveMeta }, t('save')));
      if (tab === 'metadata') titleIn.focus();
    }

    fill(dlg,
      h('div', { class: 'dlg-head' }, h('h3', { class: 'display' }, t('edit')), iconBtn('x', t('close'), function () { dlg.close(); })),
      h('div', { class: 'edit-layout' }, nav, panel),
      foot);
    dlg.addEventListener('close', function () { dlg.remove(); });
    document.body.appendChild(dlg);
    dlg.showModal();
    paintPanel();
  }

  /* ================================================================
   * Reader
   * ================================================================ */
  var MODES = ['webtoon', 'ltr', 'rtl', 'vertical'];
  /* Modes for which we've already shown the tap-zone preview this SPA session. */
  var tapZonesShown = Object.create(null);

  async function viewReader(root, rt, ctx) {
    var chapterId = rt.parts[1];
    root.appendChild(h('div', { class: 'rd-load' }, spinner()));
    if (S.pendingSave) { try { await S.pendingSave; } catch (e) { /* ignore */ } }
    var ch = await api('/api/chapters/' + enc(chapterId));
    if (!ctx.alive()) return;
    var seriesId = ch.seriesId;
    var isPdf = ch.format === 'pdf';

    var chaptersP = S.chCache[seriesId] ? Promise.resolve(S.chCache[seriesId]) : api('/api/series/' + enc(seriesId) + '/chapters').then(function (l) { S.chCache[seriesId] = l; return l; }).catch(function () { return []; });
    var res = await Promise.all([
      isPdf ? Promise.resolve([{ index: 0, name: 'pdf' }]) : api('/api/chapters/' + enc(chapterId) + '/pages'),
      chaptersP,
      api('/api/progress/' + enc(seriesId)).catch(function () { return null; }),
      api('/api/series/' + enc(seriesId)).catch(function () { return null; })
    ]);
    if (!ctx.alive()) return;
    var pages = res[0] || [], chapters = res[1] || [], prog = res[2], ser = res[3];
    var n = pages.length;

    /* ---- reader state ---- */
    var mode = (ser && ser.readerMode) || 'webtoon';
    if (MODES.indexOf(mode) < 0) mode = 'webtoon';
    var idx = 0, ratio = 0, completed = false;
    var userMoved = false, chromeOn = true, hideTimer = 0, destroyed = false;
    var restoreUntil = 0, restoreIdx = 0, restoreRatio = 0;
    var els = [], io = null, lastTop = 0, ticking = false, prefetched = {}, keep = [];
    /* Continuous webtoon: preload/append next chapter below a break "fake page". */
    var wtCol = null, wtBreak = null, wtBreakHint = null;
    var nextFetch = null, nextBundle = null, nextCol = null, nextEls = [];
    var nextIo = null, adopting = false, breakArmed = false;
    var stage = null, stageImg = null;
    var zonesEl = null, zonesTimer = 0, zonesFadeTimer = 0;
    /* pinch-zoom / pan — when zoomed, tap zones & page turns stay off until reset */
    var zoomEl = null, zScale = 1, zX = 0, zY = 0;
    var ZOOM_MAX = 4, ZOOM_EPS = 1.02;
    var pinch = null, pan = null, lastTap = 0, lastTapX = 0, lastTapY = 0;
    var vpMeta = document.querySelector('meta[name="viewport"]');
    var vpPrev = vpMeta ? vpMeta.getAttribute('content') : '';
    if (vpMeta && !isPdf) {
      vpMeta.setAttribute('content', 'width=device-width, initial-scale=1, maximum-scale=1, user-scalable=no, viewport-fit=cover');
    }

    // initial position
    var q = rt.query.get('p');
    if (q === 'last') { idx = Math.max(0, n - 1); ratio = 0; }
    else if (q != null && !isNaN(parseInt(q, 10))) { idx = clamp(parseInt(q, 10), 0, Math.max(0, n - 1)); }
    else {
      var rs = prog && prog.resume;
      if (rs && rs.chapterId === chapterId && !rs.finished) { idx = clamp(rs.pageIndex || 0, 0, Math.max(0, n - 1)); ratio = clamp(rs.scrollRatio || 0, 0, 1); }
    }

    var pageUrl = function (i) { return '/api/chapters/' + enc(chapterId) + '/page/' + i; };
    var chLabel = ch.number >= 0 ? t('ch_short', { n: fmtNum(ch.number) }) : '';
    var chTitle = ch.title && !/^(ch(apter)?\.?\s*#?\s*\d|#\s*\d)/i.test(ch.title) ? ch.title : '';

    /* ---- DOM ---- */
    var body = h('div', { class: 'rd-body', tabindex: '-1' });
    var pill = h('div', { class: 'rd-pill', 'aria-hidden': 'true' });
    var slider = h('input', { type: 'range', class: 'rd-slider', min: 1, max: Math.max(1, n), value: idx + 1, 'aria-label': t('page') });
    var posLbl = h('span', { class: 'rd-pos' });
    var prevBtn = iconBtn('left', t('prev_chapter'), function () { goChapter(ch.prev, true); });
    var nextBtn = iconBtn('right', t('next_chapter'), function () { goNext(); });
    if (!ch.prev) prevBtn.disabled = true;

    var chSel = h('select', { class: 'rd-sel ch', 'aria-label': t('chapter') });
    var found = false;
    chapters.forEach(function (c) {
      var on = c.id === chapterId || (c.alternatives || []).some(function (a) { return a.id === chapterId; });
      if (on) found = true;
      chSel.appendChild(h('option', { value: on ? chapterId : c.id, selected: on }, (c.number >= 0 ? t('ch_short', { n: fmtNum(c.number) }) + ' — ' : '') + c.title));
    });
    if (!found) chSel.insertBefore(h('option', { value: chapterId, selected: true }, chLabel || ch.title), chSel.firstChild);
    /* Chapter changes inside the reader replace history so browser Back leaves
       the reader (usually to the series), instead of stepping chapter-by-chapter. */
    function readReplace(hash) { location.replace(hash); }

    chSel.addEventListener('change', function () { chSel.blur(); readReplace('#/read/' + enc(chSel.value)); });

    var srcSel = null;
    if (ch.alternatives && ch.alternatives.length) {
      srcSel = h('select', { class: 'rd-sel src', 'aria-label': t('source') },
        h('option', { value: chapterId, selected: true }, copyLabel(ch)),
        ch.alternatives.map(function (a) { return h('option', { value: a.id }, copyLabel(a, ch)); }));
      srcSel.addEventListener('change', function () { srcSel.blur(); readReplace('#/read/' + enc(srcSel.value) + '?p=0'); });
    }

    var modeSel = h('select', { class: 'rd-sel mode', 'aria-label': t('reader_mode') },
      MODES.map(function (m) { return h('option', { value: m, selected: m === mode }, t('mode_' + m)); }));
    modeSel.addEventListener('change', function () { modeSel.blur(); setMode(modeSel.value); });

    // Favorites are series-only (toggle on the series page / Home shelf), not per-chapter.
    var top = h('div', { class: 'rd-top' },
      iconBtn('back', t('back_to_series'), backToSeries),
      h('div', { class: 'rd-title' },
        h('strong', null, ch.seriesTitle || ''),
        h('span', null, [chLabel, chTitle].filter(Boolean).join(' · ') || ch.title)),
      h('div', { class: 'rd-tools' }, chSel, srcSel, isPdf ? null : modeSel,
        iconBtn('full', t('fullscreen'), toggleFullscreen, 'hide-small')));

    var bot = h('div', { class: 'rd-bot' }, prevBtn,
      isPdf ? h('span', { class: 'rd-pos grow' }, 'PDF') : [slider, posLbl], nextBtn);

    var rd = h('div', { class: 'rd' + (isPdf ? ' pdf' : '') }, top, body, bot, pill);
    clear(root).appendChild(rd);

    function backToSeries() { readReplace('#/series/' + enc(seriesId)); }
    function toggleFullscreen() {
      if (document.fullscreenElement) document.exitFullscreen();
      else if (document.documentElement.requestFullscreen) document.documentElement.requestFullscreen().catch(function () { });
    }

    /* ---- chrome (top/bottom bars) ---- */
    function cancelHide() { clearTimeout(hideTimer); hideTimer = 0; }
    function setChrome(on) {
      if (isPdf) on = true;
      chromeOn = on;
      rd.classList.toggle('chrome-off', !on);
      cancelHide();
    }
    function pokeChrome(ms) {
      setChrome(true);
      if (ms && !isPdf) hideTimer = setTimeout(function () { hideTimer = 0; setChrome(false); }, ms);
    }
    top.addEventListener('focusin', cancelHide);
    bot.addEventListener('focusin', cancelHide);
    rd.addEventListener('mousemove', function (e) {
      if (isPdf) return;
      var y = e.clientY, hgt = window.innerHeight;
      if (y < 70 || y > hgt - 90) { if (!chromeOn) setChrome(true); cancelHide(); }
      else if (chromeOn && !hideTimer) hideTimer = setTimeout(function () { hideTimer = 0; setChrome(false); }, 2500);
    }, { passive: true });

    /* ---- progress ---- */
    function payload(done) {
      return { chapterId: chapterId, pageIndex: idx, scrollRatio: clamp(ratio, 0, 1), completed: !!(done !== undefined ? done : completed) };
    }
    function save(done) {
      if (destroyed && done === undefined) return;
      S.pendingSave = api('/api/progress/' + enc(seriesId), { method: 'PUT', json: payload(done), keepalive: true, noRedirect: true }).catch(function () { });
      return S.pendingSave;
    }
    var saveSoon = debounce(function () { save(); }, 1500);
    function flush() { saveSoon.cancel(); return save(); }
    function onHide() { if (document.visibilityState === 'hidden') flush(); }
    document.addEventListener('visibilitychange', onHide);
    window.addEventListener('pagehide', flush);

    /* ---- shared position UI ---- */
    function updateUi() {
      var label = (idx + 1) + ' / ' + n;
      posLbl.textContent = label;
      pill.textContent = label;
      slider.value = idx + 1;
      if (n - idx <= 3) prefetchNext();
    }
    function setIdx(i, r) {
      i = clamp(i, 0, Math.max(0, n - 1));
      idx = i; ratio = r || 0;
      if (mode !== 'webtoon') completed = idx >= n - 1;
      updateUi();
      saveSoon();
    }

    function canContinueNext() {
      var nx = ch.next;
      if (!nx || isPdf) return false;
      var meta = chapters.find(function (c) { return c.id === nx.id; });
      if (meta && meta.format === 'pdf') return false;
      return true;
    }
    function chapterHeading(linkOrCh) {
      if (!linkOrCh) return '';
      var num = linkOrCh.number >= 0 ? t('ch_short', { n: fmtNum(linkOrCh.number) }) : '';
      var title = linkOrCh.title && !/^(ch(apter)?\.?\s*#?\s*\d|#\s*\d)/i.test(linkOrCh.title) ? linkOrCh.title : '';
      return [num, title].filter(Boolean).join(' — ') || linkOrCh.title || '';
    }
    function setBreakHint(kind, msg) {
      if (!wtBreakHint) return;
      clear(wtBreakHint);
      wtBreakHint.className = 'wt-break-hint muted' + (kind === 'busy' ? ' busy' : '');
      if (kind === 'busy') {
        add(wtBreakHint, spinner());
        add(wtBreakHint, document.createTextNode(msg || t('loading_next_chapter')));
      } else {
        add(wtBreakHint, document.createTextNode(msg || t('scroll_for_next')));
      }
    }
    function prefetchNext() {
      var nx = ch.next;
      if (!nx || prefetched[nx.id]) return;
      var meta = chapters.find(function (c) { return c.id === nx.id; });
      if (isPdf || !meta || meta.format === 'pdf') return;
      prefetched[nx.id] = 1;
      var im = new Image(); im.src = '/api/chapters/' + enc(nx.id) + '/page/0'; keep.push(im);
      if (mode === 'webtoon' && canContinueNext()) ensureNextChapter();
    }
    function ensureNextChapter() {
      if (!canContinueNext()) return Promise.resolve(null);
      if (nextBundle) return Promise.resolve(nextBundle);
      if (nextFetch) return nextFetch;
      var nxId = ch.next.id;
      setBreakHint('busy', t('loading_next_chapter'));
      nextFetch = Promise.all([
        api('/api/chapters/' + enc(nxId)),
        api('/api/chapters/' + enc(nxId) + '/pages')
      ]).then(function (pair) {
        if (destroyed) return null;
        var detail = pair[0], pgs = pair[1] || [];
        if (!detail || detail.format === 'pdf') {
          setBreakHint('text', t('scroll_for_next'));
          return null;
        }
        if (!pgs.length) {
          setBreakHint('text', t('no_pages'));
          return null;
        }
        nextBundle = { detail: detail, pages: pgs };
        var i, im;
        for (i = 0; i < Math.min(6, pgs.length); i++) {
          im = new Image();
          im.decoding = 'async';
          im.src = '/api/chapters/' + enc(nxId) + '/page/' + i;
          keep.push(im);
        }
        setBreakHint('text', t('scroll_for_next'));
        if (breakArmed && !nextCol) appendNextPages(nextBundle);
        return nextBundle;
      }).catch(function () {
        nextFetch = null;
        if (!destroyed) setBreakHint('text', t('next_chapter_failed'));
        return null;
      });
      return nextFetch;
    }
    function mountNextPage(i, eager) {
      var pg = nextEls[i];
      if (!pg || !nextBundle) return;
      var img = pg._img;
      var nxId = nextBundle.detail.id;
      if (!img) {
        img = new Image();
        img.decoding = 'async';
        img.alt = t('page_n', { n: i + 1 });
        img.draggable = false;
        img.loading = eager ? 'eager' : 'lazy';
        img.onload = function () {
          if (img.naturalWidth) pg.style.aspectRatio = img.naturalWidth + ' / ' + img.naturalHeight;
          pg.classList.add('ok'); pg.classList.remove('err');
        };
        img.onerror = function () { pg.classList.add('err'); };
        img.src = '/api/chapters/' + enc(nxId) + '/page/' + i;
        pg.appendChild(img);
        pg._img = img;
      } else if (eager && img.loading === 'lazy') img.loading = 'eager';
    }
    function appendNextPages(bundle) {
      if (!wtCol || nextCol || !bundle || !bundle.pages.length || adopting) return;
      nextCol = h('div', { class: 'wt-next' });
      nextEls = [];
      var i, nn = bundle.pages.length;
      for (i = 0; i < nn; i++) {
        var pg = h('div', { class: 'wt-pg', 'data-next-i': i });
        var rb = h('button', { class: 'retry', type: 'button', onclick: (function (k) {
          return function (e) {
            e.stopPropagation();
            var p = nextEls[k];
            if (p && p._img) { p._img.onload = p._img.onerror = null; p._img.remove(); p._img = null; p.classList.remove('ok', 'err'); }
            mountNextPage(k, true);
          };
        })(i) }, t('retry_page'));
        pg.appendChild(rb);
        nextEls.push(pg);
        nextCol.appendChild(pg);
      }
      wtCol.appendChild(nextCol);
      for (i = 0; i < Math.min(10, nn); i++) mountNextPage(i, i < 6);
      if (nextIo) nextIo.disconnect();
      nextIo = new IntersectionObserver(function (es) {
        es.forEach(function (e) {
          if (!e.isIntersecting) return;
          var i = +e.target.getAttribute('data-next-i');
          if (i === 0) adoptNextChapter();
          else if (i > 0 && i < nextEls.length) {
            var lo = Math.max(0, i - 2), hi = Math.min(nextEls.length - 1, i + 6), j;
            for (j = lo; j <= hi; j++) mountNextPage(j, j <= i + 3);
          }
        });
      }, { root: body, rootMargin: '-45% 0px -45% 0px' });
      nextEls.forEach(function (p) { nextIo.observe(p); });
    }
    function syncChapterChrome() {
      chLabel = ch.number >= 0 ? t('ch_short', { n: fmtNum(ch.number) }) : '';
      chTitle = ch.title && !/^(ch(apter)?\.?\s*#?\s*\d|#\s*\d)/i.test(ch.title) ? ch.title : '';
      var titleBox = top.querySelector('.rd-title');
      if (titleBox) {
        clear(titleBox);
        add(titleBox, h('strong', null, ch.seriesTitle || ''));
        add(titleBox, h('span', null, [chLabel, chTitle].filter(Boolean).join(' · ') || ch.title));
      }
      prevBtn.disabled = !ch.prev;
      var i, opt, foundSel = false;
      for (i = 0; i < chSel.options.length; i++) {
        opt = chSel.options[i];
        opt.selected = opt.value === chapterId;
        if (opt.selected) foundSel = true;
      }
      if (!foundSel) {
        chSel.insertBefore(h('option', { value: chapterId, selected: true }, chapterHeading(ch) || ch.title), chSel.firstChild);
      }
      if (srcSel && srcSel.parentNode) {
        srcSel.remove();
        srcSel = null;
      }
      if (ch.alternatives && ch.alternatives.length) {
        srcSel = h('select', { class: 'rd-sel src', 'aria-label': t('source') },
          h('option', { value: chapterId, selected: true }, copyLabel(ch)),
          ch.alternatives.map(function (a) { return h('option', { value: a.id }, copyLabel(a, ch)); }));
        srcSel.addEventListener('change', function () { srcSel.blur(); readReplace('#/read/' + enc(srcSel.value) + '?p=0'); });
        var tools = top.querySelector('.rd-tools');
        if (tools) tools.insertBefore(srcSel, modeSel);
      }
      slider.max = Math.max(1, n);
      slider.value = idx + 1;
      document.title = (ch.seriesTitle || 'Yomi') + ' ' + (chLabel || '') + ' · Yomi';
      updateUi();
    }
    function placeChapterEnd() {
      if (!wtCol) return;
      if (wtBreak && wtBreak.parentNode) wtBreak.remove();
      wtBreak = null;
      wtBreakHint = null;
      breakArmed = false;
      if (!ch.next) {
        wtCol.appendChild(h('div', { class: 'wt-end' },
          h('p', { class: 'muted' }, t('end_of_series')),
          h('div', { class: 'row center' },
            h('button', { class: 'btn', type: 'button', onclick: backToSeries }, t('back_to_series')))));
        return;
      }
      if (!canContinueNext()) {
        wtCol.appendChild(h('div', { class: 'wt-end' },
          h('p', { class: 'muted' }, t('end_of_chapter')),
          h('div', { class: 'row center' },
            h('button', { class: 'btn primary lg', type: 'button', onclick: goNext }, t('next_chapter'), icon('right')),
            h('button', { class: 'btn', type: 'button', onclick: backToSeries }, t('back_to_series')))));
        return;
      }
      wtBreakHint = h('p', { class: 'wt-break-hint muted' }, t('scroll_for_next'));
      wtBreak = h('div', { class: 'wt-break', 'data-break': '1' },
        h('div', { class: 'wt-break-inner' },
          h('p', { class: 'wt-break-kicker' }, t('end_of_chapter')),
          h('h2', { class: 'wt-break-title display' }, chapterHeading(ch.next)),
          wtBreakHint,
          h('div', { class: 'row center' },
            h('button', { class: 'btn primary', type: 'button', onclick: goNext }, t('next_chapter'), icon('right')),
            h('button', { class: 'btn', type: 'button', onclick: backToSeries }, t('back_to_series')))));
      wtCol.appendChild(wtBreak);
    }
    function adoptNextChapter() {
      if (adopting || destroyed || !nextBundle || !nextCol || !wtCol) return;
      adopting = true;
      resetZoom();
      var bundle = nextBundle;
      var nx = bundle.detail;
      var newPages = bundle.pages;
      var newEls = nextEls.slice();
      var anchorTop = nextCol.offsetTop;
      var keepScroll = body.scrollTop;

      completed = true;
      flush();

      if (io) { io.disconnect(); io = null; }
      if (nextIo) { nextIo.disconnect(); nextIo = null; }
      winLo = winHi = -1;

      while (wtCol.firstChild && wtCol.firstChild !== nextCol) wtCol.removeChild(wtCol.firstChild);
      while (nextCol.firstChild) wtCol.appendChild(nextCol.firstChild);
      nextCol.remove();
      nextCol = null;
      nextEls = [];
      nextBundle = null;
      nextFetch = null;
      wtBreak = null;
      wtBreakHint = null;
      breakArmed = false;

      body.scrollTop = Math.max(0, keepScroll - anchorTop);

      chapterId = nx.id;
      ch = nx;
      pages = newPages;
      n = pages.length;
      pageUrl = function (i) { return '/api/chapters/' + enc(chapterId) + '/page/' + i; };
      els = newEls;
      for (var i = 0; i < els.length; i++) {
        els[i].removeAttribute('data-next-i');
        els[i].setAttribute('data-i', String(i));
      }
      idx = 0;
      if (els[0]) ratio = clamp((body.scrollTop - els[0].offsetTop) / Math.max(1, els[0].offsetHeight), 0, 1);
      else ratio = 0;
      /* If we landed past page 0, pick the page nearest the viewport center. */
      if (els.length) {
        var mid = body.scrollTop + body.clientHeight * 0.5, best = 0, bestDist = Infinity, pi, pEl, cy, dist;
        for (pi = 0; pi < els.length; pi++) {
          pEl = els[pi];
          cy = pEl.offsetTop + pEl.offsetHeight * 0.5;
          dist = Math.abs(cy - mid);
          if (dist < bestDist) { bestDist = dist; best = pi; }
        }
        idx = best;
        ratio = clamp((body.scrollTop - els[idx].offsetTop) / Math.max(1, els[idx].offsetHeight), 0, 1);
      }
      completed = false;
      prefetched = {};
      adopting = false;

      io = new IntersectionObserver(function (es) {
        es.forEach(function (e) {
          if (!e.isIntersecting) return;
          var i = +e.target.getAttribute('data-i');
          if (i !== idx) { idx = i; updateUi(); syncWindow(); saveSoon(); }
        });
      }, { root: body, rootMargin: '-50% 0px -50% 0px' });
      els.forEach(function (p) { io.observe(p); });
      placeChapterEnd();
      setZoomTarget(wtCol);
      syncWindow();
      syncChapterChrome();
      saveSoon();

      /* Replace the URL in place — do not push history, or Back would walk
         chapters instead of leaving the reader. No hashchange → no remount. */
      var wantHash = '#/read/' + enc(chapterId);
      if (location.hash.split('?')[0] !== wantHash) history.replaceState(null, '', wantHash);
    }

    /* ---- chapter navigation ---- */
    function goChapter(link, last) {
      if (!link) { toast(last ? t('first_chapter') : t('last_chapter')); return; }
      readReplace('#/read/' + enc(link.id) + (last ? '?p=last' : ''));
    }
    function goNext() {
      if (!ch.next) {
        completed = true; flush();
        toast(t('last_chapter'));
        return;
      }
      completed = true;
      flush();
      readReplace('#/read/' + enc(ch.next.id) + '?p=0');
    }

    /* ================= WEBTOON ================= */
    function pageEl(i) { return els[i]; }

    function mountPage(i, eager) {
      var pg = els[i];
      if (!pg) return;
      var img = pg._img;
      if (!img) {
        img = new Image();
        img.decoding = 'async';
        img.alt = t('page_n', { n: i + 1 });
        img.draggable = false;
        img.loading = eager ? 'eager' : 'lazy';
        img.onload = function () {
          if (img.naturalWidth) pg.style.aspectRatio = img.naturalWidth + ' / ' + img.naturalHeight;
          pg.classList.add('ok'); pg.classList.remove('err');
          if (restoreUntil && !userMoved && performance.now() < restoreUntil) scrollToPage(restoreIdx, restoreRatio);
        };
        img.onerror = function () { pg.classList.add('err'); };
        img.src = pageUrl(i);
        pg.appendChild(img);
        pg._img = img;
      } else if (eager && img.loading === 'lazy') img.loading = 'eager';
    }
    function unmountPage(i) {
      var pg = els[i];
      if (pg && pg._img) { pg._img.onload = pg._img.onerror = null; pg._img.remove(); pg._img = null; pg.classList.remove('ok', 'err'); }
    }
    var winLo = -1, winHi = -1;
    function syncWindow() {
      var lo = Math.max(0, idx - 15), hi = Math.min(n - 1, idx + 15), i;
      for (i = winLo; i >= 0 && i <= winHi; i++) if (i < lo || i > hi) unmountPage(i);
      for (i = lo; i <= hi; i++) mountPage(i, i >= idx - 2 && i <= idx + 5);
      winLo = lo; winHi = hi;
    }
    function scrollToPage(i, r) {
      var pg = els[i];
      if (!pg) return;
      body.scrollTop = pg.offsetTop + (r || 0) * pg.offsetHeight;
    }
    function buildWebtoon() {
      body.className = 'rd-body wt';
      nextFetch = null; nextBundle = null; nextCol = null; nextEls = [];
      adopting = false; breakArmed = false;
      if (nextIo) { nextIo.disconnect(); nextIo = null; }
      wtCol = h('div', { class: 'wt-col' });
      els = [];
      for (var i = 0; i < n; i++) {
        var pg = h('div', { class: 'wt-pg', 'data-i': i });
        var rb = h('button', { class: 'retry', type: 'button', onclick: (function (k) { return function (e) { e.stopPropagation(); unmountPage(k); mountPage(k, true); }; })(i) }, t('retry_page'));
        pg.appendChild(rb);
        els.push(pg); wtCol.appendChild(pg);
      }
      placeChapterEnd();
      body.appendChild(wtCol);
      setZoomTarget(wtCol);

      io = new IntersectionObserver(function (es) {
        es.forEach(function (e) {
          if (!e.isIntersecting) return;
          var i = +e.target.getAttribute('data-i');
          if (i !== idx) { idx = i; updateUi(); syncWindow(); saveSoon(); }
        });
      }, { root: body, rootMargin: '-50% 0px -50% 0px' });
      els.forEach(function (p) { io.observe(p); });

      syncWindow();
      restoreIdx = idx; restoreRatio = ratio; restoreUntil = performance.now() + 3000; userMoved = false;
      scrollToPage(idx, ratio);
      lastTop = body.scrollTop;
      updateUi();
      body.addEventListener('scroll', onWtScroll, { passive: true });
      ['wheel', 'touchstart', 'pointerdown'].forEach(function (ev) { body.addEventListener(ev, markMoved, { passive: true }); });
    }
    function markMoved() { userMoved = true; }
    function onWtScroll() {
      if (ticking) return;
      ticking = true;
      requestAnimationFrame(function () {
        ticking = false;
        if (destroyed || mode !== 'webtoon') return;
        var top = body.scrollTop, d = top - lastTop;
        if (userMoved) {
          if (d > 14 && chromeOn) setChrome(false);
          else if (d < -14 && !chromeOn) setChrome(true);
        }
        if (Math.abs(d) > 14) lastTop = top;
        var pg = els[idx];
        if (pg) ratio = clamp((top - pg.offsetTop) / Math.max(1, pg.offsetHeight), 0, 1);
        /* Complete once the last page of *this* chapter is mostly past — not the
           scroll container end (next chapter may already be appended below). */
        var pastCurrent = false;
        if (n && els[n - 1]) {
          var lastPg = els[n - 1];
          pastCurrent = top + body.clientHeight >= lastPg.offsetTop + lastPg.offsetHeight * 0.85;
        }
        if (pastCurrent !== completed && (userMoved || pastCurrent)) { completed = pastCurrent; saveSoon(); }
        if (pastCurrent && !chromeOn && userMoved && !nextCol) setChrome(true);

        if (wtBreak && canContinueNext() && !adopting) {
          var br = wtBreak.getBoundingClientRect();
          var bb = body.getBoundingClientRect();
          if (br.top < bb.bottom + bb.height * 2) {
            breakArmed = true;
            ensureNextChapter();
          }
          if (nextBundle && !nextCol && br.top < bb.bottom + 40) appendNextPages(nextBundle);
        }
      });
    }
    function wtStep(frac) { body.scrollBy({ top: frac * body.clientHeight, behavior: 'smooth' }); userMoved = true; }

    /* ================= ZOOM / PAN ================= */
    function isZoomed() { return zScale > ZOOM_EPS; }
    function setZoomTarget(el) {
      if (zoomEl && zoomEl !== el) {
        zoomEl.style.transform = '';
        zoomEl.classList.remove('rd-zoom-target');
      }
      zoomEl = el || null;
      if (zoomEl) zoomEl.classList.add('rd-zoom-target');
      applyZoom();
    }
    function applyZoom() {
      if (zoomEl) {
        if (zScale <= 1.001 && Math.abs(zX) < 0.5 && Math.abs(zY) < 0.5) {
          zoomEl.style.transform = '';
        } else {
          zoomEl.style.transform = 'translate(' + zX + 'px,' + zY + 'px) scale(' + zScale + ')';
        }
      }
      rd.classList.toggle('zoomed', isZoomed());
      body.classList.toggle('zoomed', isZoomed());
    }
    /* Untransformed top-left of zoomEl relative to the reader body (layout, not painted). */
    function zoomOffset() {
      var x = 0, y = 0, el = zoomEl;
      while (el && el !== body) {
        x += el.offsetLeft;
        y += el.offsetTop;
        el = el.offsetParent;
      }
      return { x: x, y: y };
    }
    function clampPan() {
      if (!zoomEl || zScale <= 1) { zX = 0; zY = 0; return; }
      var bw = body.clientWidth, bh = body.clientHeight;
      var w = zoomEl.offsetWidth * zScale, h = zoomEl.offsetHeight * zScale;
      var o = zoomOffset();
      if (w <= bw) zX = -o.x + (bw - w) / 2;
      else zX = clamp(zX, -o.x + bw - w, -o.x);
      if (h <= bh) zY = -o.y + (bh - h) / 2;
      else zY = clamp(zY, -o.y + bh - h, -o.y);
    }
    function resetZoom() {
      zScale = 1; zX = 0; zY = 0;
      pinch = pan = null;
      applyZoom();
    }
    function zoomAt(clientX, clientY, nextScale) {
      if (!zoomEl) return;
      var br = body.getBoundingClientRect();
      var o = zoomOffset();
      var baseLeft = br.left + o.x, baseTop = br.top + o.y;
      var prev = zScale || 1;
      nextScale = clamp(nextScale, 1, ZOOM_MAX);
      zScale = nextScale;
      zX = clientX - baseLeft - (clientX - baseLeft - zX) * (nextScale / prev);
      zY = clientY - baseTop - (clientY - baseTop - zY) * (nextScale / prev);
      if (zScale <= 1.001) { zScale = 1; zX = 0; zY = 0; }
      else clampPan();
      applyZoom();
    }
    function onZoomTouchStart(e) {
      if (isPdf || destroyed || !zoomEl) return;
      if (e.touches.length === 2) {
        e.preventDefault();
        pan = null;
        var a = e.touches[0], b = e.touches[1];
        var br = body.getBoundingClientRect();
        var o = zoomOffset();
        if (stage) stage.scrollTop = 0;
        pinch = {
          dist: Math.hypot(a.clientX - b.clientX, a.clientY - b.clientY) || 1,
          scale: zScale,
          mx: (a.clientX + b.clientX) / 2,
          my: (a.clientY + b.clientY) / 2,
          x: zX,
          y: zY,
          baseLeft: br.left + o.x,
          baseTop: br.top + o.y
        };
        swallowClick = true;
        return;
      }
      if (e.touches.length === 1 && isZoomed()) {
        e.preventDefault();
        var t = e.touches[0];
        pan = { x: t.clientX, y: t.clientY, ox: zX, oy: zY, moved: false };
        swallowClick = true;
      }
    }
    function onZoomTouchMove(e) {
      if (!zoomEl) return;
      if (pinch && e.touches.length >= 2) {
        e.preventDefault();
        var a = e.touches[0], b = e.touches[1];
        var dist = Math.hypot(a.clientX - b.clientX, a.clientY - b.clientY) || 1;
        var mx = (a.clientX + b.clientX) / 2, my = (a.clientY + b.clientY) / 2;
        var next = clamp(pinch.scale * (dist / pinch.dist), 1, ZOOM_MAX);
        zScale = next;
        zX = mx - pinch.baseLeft - (pinch.mx - pinch.baseLeft - pinch.x) * (next / pinch.scale);
        zY = my - pinch.baseTop - (pinch.my - pinch.baseTop - pinch.y) * (next / pinch.scale);
        if (zScale <= 1.001) { zScale = 1; zX = 0; zY = 0; }
        else clampPan();
        applyZoom();
        return;
      }
      if (pan && e.touches.length === 1 && isZoomed()) {
        e.preventDefault();
        var t = e.touches[0];
        var dx = t.clientX - pan.x, dy = t.clientY - pan.y;
        if (Math.abs(dx) > 2 || Math.abs(dy) > 2) pan.moved = true;
        zX = pan.ox + dx;
        zY = pan.oy + dy;
        clampPan();
        applyZoom();
      }
    }
    function onZoomTouchEnd(e) {
      var endedPinch = false, endedPan = false;
      if (pinch && e.touches.length < 2) {
        pinch = null;
        endedPinch = true;
        if (zScale <= ZOOM_EPS) resetZoom();
        else { clampPan(); applyZoom(); }
        setTimeout(function () { swallowClick = false; }, 280);
      }
      if (pan && e.touches.length === 0) {
        endedPan = !!pan.moved;
        pan = null;
        if (endedPan) {
          swallowClick = true;
          setTimeout(function () { swallowClick = false; }, 280);
        } else {
          setTimeout(function () { swallowClick = false; }, 280);
        }
      }
      /* double-tap → reset zoom & re-enable nav zones */
      if (!endedPinch && !endedPan && e.changedTouches && e.changedTouches.length === 1 && e.touches.length === 0) {
        var t = e.changedTouches[0];
        var now = Date.now();
        if (now - lastTap < 320 && Math.hypot(t.clientX - lastTapX, t.clientY - lastTapY) < 36) {
          lastTap = 0;
          if (isZoomed()) {
            resetZoom();
            swallowClick = true;
            setTimeout(function () { swallowClick = false; }, 350);
          }
        } else {
          lastTap = now; lastTapX = t.clientX; lastTapY = t.clientY;
        }
      }
    }
    function onZoomWheel(e) {
      if (isPdf || destroyed || !zoomEl) return;
      if (e.ctrlKey) {
        e.preventDefault();
        zoomAt(e.clientX, e.clientY, zScale * (e.deltaY < 0 ? 1.12 : 1 / 1.12));
        return;
      }
      if (!isZoomed()) return;
      e.preventDefault();
      zX -= e.deltaX;
      zY -= e.deltaY;
      clampPan();
      applyZoom();
    }
    function onZoomPointerDown(e) {
      if (isPdf || destroyed || !zoomEl || e.pointerType === 'touch') return;
      if (!isZoomed() || e.button !== 0) return;
      pan = { x: e.clientX, y: e.clientY, ox: zX, oy: zY, moved: false, ptr: e.pointerId };
      try { body.setPointerCapture(e.pointerId); } catch (err) { /* ignore */ }
    }
    function onZoomPointerMove(e) {
      if (!pan || pan.ptr !== e.pointerId || e.pointerType === 'touch') return;
      var dx = e.clientX - pan.x, dy = e.clientY - pan.y;
      if (Math.abs(dx) > 2 || Math.abs(dy) > 2) pan.moved = true;
      zX = pan.ox + dx;
      zY = pan.oy + dy;
      clampPan();
      applyZoom();
    }
    function onZoomPointerUp(e) {
      if (!pan || pan.ptr !== e.pointerId) return;
      var wasPan = pan.moved;
      pan = null;
      if (wasPan) {
        swallowClick = true;
        setTimeout(function () { swallowClick = false; }, 280);
      }
    }
    function onZoomDblClick(e) {
      if (isPdf || destroyed || !zoomEl) return;
      if (isZoomed()) {
        e.preventDefault();
        resetZoom();
        swallowClick = true;
        setTimeout(function () { swallowClick = false; }, 350);
      }
    }
    body.addEventListener('touchstart', onZoomTouchStart, { passive: false });
    body.addEventListener('touchmove', onZoomTouchMove, { passive: false });
    body.addEventListener('touchend', onZoomTouchEnd, { passive: true });
    body.addEventListener('touchcancel', onZoomTouchEnd, { passive: true });
    body.addEventListener('wheel', onZoomWheel, { passive: false });
    body.addEventListener('pointerdown', onZoomPointerDown);
    body.addEventListener('pointermove', onZoomPointerMove);
    body.addEventListener('pointerup', onZoomPointerUp);
    body.addEventListener('pointercancel', onZoomPointerUp);
    body.addEventListener('dblclick', onZoomDblClick);

    /* ================= PAGED ================= */
    var preCache = {};
    function preload(i) {
      if (i < 0 || i >= n || preCache[i]) return;
      var im = new Image(); im.decoding = 'async'; im.src = pageUrl(i); preCache[i] = im;
    }
    function buildPaged() {
      body.className = 'rd-body pgd pgd-' + mode;
      stage = h('div', { class: 'pg-stage' });
      var zoomWrap = h('div', { class: 'pg-zoom' });
      stageImg = h('img', { class: 'pg-img', alt: '', draggable: false });
      stageImg.addEventListener('load', function () { stage.classList.remove('busy'); });
      stageImg.addEventListener('error', function () { stage.classList.remove('busy'); toast(t('page_failed'), 'err'); });
      zoomWrap.appendChild(stageImg);
      stage.appendChild(zoomWrap);
      body.appendChild(stage);
      setZoomTarget(zoomWrap);
      showPage(idx);
      body.addEventListener('touchstart', onTouchStart, { passive: true });
      body.addEventListener('touchend', onTouchEnd, { passive: true });
    }
    var tsx = 0, tsy = 0, tst = 0;
    function onTouchStart(e) {
      if (isZoomed() || e.touches.length > 1) return;
      var p = e.changedTouches[0]; tsx = p.clientX; tsy = p.clientY; tst = Date.now();
    }
    function onTouchEnd(e) {
      if (mode === 'vertical' || isZoomed()) return;
      var p = e.changedTouches[0], dx = p.clientX - tsx, dy = p.clientY - tsy;
      if (Date.now() - tst < 600 && Math.abs(dx) > 55 && Math.abs(dx) > Math.abs(dy) * 1.5) {
        swallowClick = true; setTimeout(function () { swallowClick = false; }, 350);
        var forward = mode === 'rtl' ? dx > 0 : dx < 0;
        if (forward) nextPage(); else prevPage();
      }
    }
    function showPage(i) {
      resetZoom();
      setIdx(i, 0);
      var next = pageUrl(idx);
      if (stageImg.getAttribute('src') !== next) {
        var cached = preCache[idx] && preCache[idx].complete;
        if (!cached) stage.classList.add('busy');
        stageImg.src = next;
      }
      stage.scrollTop = 0;
      for (var k = 1; k <= 4; k++) preload(idx + k);
      preload(idx - 1);
    }
    function nextPage() { userMoved = true; if (chromeOn) setChrome(false); if (idx >= n - 1) goNext(); else showPage(idx + 1); }
    function prevPage() { userMoved = true; if (chromeOn) setChrome(false); if (idx <= 0) goChapter(ch.prev, true); else showPage(idx - 1); }
    function vStep(dir) {
      userMoved = true;
      var max = stage.scrollHeight - stage.clientHeight;
      if (dir > 0 && stage.scrollTop < max - 6) stage.scrollBy({ top: stage.clientHeight * 0.85, behavior: 'smooth' });
      else if (dir < 0 && stage.scrollTop > 6) stage.scrollBy({ top: -stage.clientHeight * 0.85, behavior: 'smooth' });
      else dir > 0 ? nextPage() : prevPage();
    }

    /* ================= PDF ================= */
    function buildPdf() {
      body.className = 'rd-body pdfv';
      body.appendChild(h('iframe', { class: 'pdf-frame', src: '/api/chapters/' + enc(chapterId) + '/file', title: ch.title }));
      idx = 0; ratio = 0; completed = false;
    }

    /* ================= interaction ================= */
    var swallowClick = false;
    body.addEventListener('click', function (e) {
      if (swallowClick || isPdf || e.target.closest('.wt-end') || e.target.closest('.wt-break') || e.target.closest('.retry')) return;
      if (isZoomed()) return; /* nav zones off while zoomed — double-tap resets */
      if (mode === 'webtoon') { setChrome(!chromeOn); userMoved = true; return; }
      var r = body.getBoundingClientRect();
      var x = (e.clientX - r.left) / r.width, y = (e.clientY - r.top) / r.height;
      if (mode === 'vertical') {
        if (y < 0.3) vStep(-1); else if (y > 0.7) vStep(1); else setChrome(!chromeOn);
        return;
      }
      if (x > 0.3 && x < 0.7) { setChrome(!chromeOn); return; }
      var right = x >= 0.7;
      var forward = mode === 'rtl' ? !right : right;
      forward ? nextPage() : prevPage();
    });

    var sliderRaf = 0;
    slider.addEventListener('input', function () {
      var v = parseInt(slider.value, 10) - 1;
      posLbl.textContent = (v + 1) + ' / ' + n;
      pill.textContent = posLbl.textContent;
      cancelAnimationFrame(sliderRaf);
      sliderRaf = requestAnimationFrame(function () { jumpTo(v); });
    });
    function jumpTo(i) {
      i = clamp(i, 0, Math.max(0, n - 1));
      userMoved = true;
      resetZoom();
      if (mode === 'webtoon') { idx = i; ratio = 0; syncWindow(); scrollToPage(i, 0); updateUi(); saveSoon(); }
      else showPage(i);
    }

    function clearTapZones() {
      clearTimeout(zonesTimer); zonesTimer = 0;
      clearTimeout(zonesFadeTimer); zonesFadeTimer = 0;
      if (zonesEl && zonesEl.parentNode) zonesEl.remove();
      zonesEl = null;
    }
    /* Brief Previous / Menu / Next map. Once per mode per session; always when forced (mode change). */
    function showTapZones(force) {
      if (destroyed || isPdf || n === 0) return;
      if (!force && tapZonesShown[mode]) return;
      tapZonesShown[mode] = true;
      clearTapZones();
      var layout = mode === 'vertical' ? 'vt' : mode === 'webtoon' ? 'wt' : 'hz';
      var prevL = t('zone_prev'), nextL = t('zone_next'), menuL = t('zone_menu');
      var kids;
      if (layout === 'wt') {
        kids = [h('div', { class: 'rd-zone menu' }, h('span', null, menuL))];
      } else if (layout === 'vt') {
        kids = [
          h('div', { class: 'rd-zone prev' }, h('span', null, prevL)),
          h('div', { class: 'rd-zone menu' }, h('span', null, menuL)),
          h('div', { class: 'rd-zone next' }, h('span', null, nextL))
        ];
      } else {
        var leftNext = mode === 'rtl';
        kids = [
          h('div', { class: 'rd-zone ' + (leftNext ? 'next' : 'prev') }, h('span', null, leftNext ? nextL : prevL)),
          h('div', { class: 'rd-zone menu' }, h('span', null, menuL)),
          h('div', { class: 'rd-zone ' + (leftNext ? 'prev' : 'next') }, h('span', null, leftNext ? prevL : nextL))
        ];
      }
      zonesEl = h('div', { class: 'rd-zones ' + layout, 'aria-hidden': 'true' }, kids);
      rd.appendChild(zonesEl);
      requestAnimationFrame(function () {
        if (!zonesEl || destroyed) return;
        zonesEl.classList.add('on');
      });
      zonesTimer = setTimeout(function () {
        zonesTimer = 0;
        if (!zonesEl) return;
        zonesEl.classList.remove('on');
        var el = zonesEl;
        zonesFadeTimer = setTimeout(function () {
          zonesFadeTimer = 0;
          if (el && el.parentNode) el.remove();
          if (zonesEl === el) zonesEl = null;
        }, 400);
      }, 1400);
    }

    function setMode(m) {
      if (m === mode || isPdf) return;
      var keepIdx = idx, keepRatio = ratio;
      mode = m; modeSel.value = m;
      api('/api/series/' + enc(seriesId) + '/reader-mode', { method: 'PUT', json: { readerMode: m } }).catch(function () { /* best effort */ });
      teardownBody();
      idx = keepIdx; ratio = mode === 'webtoon' ? keepRatio : 0;
      rd.setAttribute('data-mode', mode);
      slider.style.direction = mode === 'rtl' ? 'rtl' : 'ltr';
      if (mode === 'webtoon') { completed = false; buildWebtoon(); } else { completed = idx >= n - 1; buildPaged(); }
      body.focus({ preventScroll: true });
      toast(t('mode_' + mode));
      showTapZones(true);
    }
    function teardownBody() {
      if (io) { io.disconnect(); io = null; }
      if (nextIo) { nextIo.disconnect(); nextIo = null; }
      winLo = winHi = -1;
      els = []; stage = stageImg = null;
      wtCol = null; wtBreak = null; wtBreakHint = null;
      nextFetch = null; nextBundle = null; nextCol = null; nextEls = [];
      adopting = false; breakArmed = false;
      resetZoom();
      setZoomTarget(null);
      body.removeEventListener('scroll', onWtScroll);
      ['wheel', 'touchstart', 'pointerdown'].forEach(function (ev) { body.removeEventListener(ev, markMoved); });
      body.removeEventListener('touchstart', onTouchStart);
      body.removeEventListener('touchend', onTouchEnd);
      clear(body);
      body.scrollTop = 0;
    }

    function onKey(e) {
      if (e.ctrlKey || e.metaKey || e.altKey) return;
      var tg = e.target, tn = tg && tg.tagName;
      if (tn === 'INPUT' && tg.type !== 'range') return;
      if (tn === 'TEXTAREA') return;
      var k = e.key, handled = true;
      if (k === 'Escape') {
        if (document.fullscreenElement) return;
        if (isZoomed()) { resetZoom(); return; }
        backToSeries();
      } else if (k === 'f' || k === 'F') toggleFav();
      else if (k === 'm' || k === 'M') { if (!isPdf) setMode(MODES[(MODES.indexOf(mode) + 1) % MODES.length]); }
      else if (k === 'c') setChrome(!chromeOn);
      else if (k === '[') goChapter(ch.prev, true);
      else if (k === ']') goNext();
      else if (isPdf) handled = false;
      else if (mode === 'webtoon') {
        if (k === 'j' || k === ' ' && !e.shiftKey) wtStep(k === 'j' ? 0.5 : 0.85);
        else if (k === 'k' || (k === ' ' && e.shiftKey)) wtStep(k === 'k' ? -0.5 : -0.85);
        else if (k === 'PageDown') wtStep(0.9);
        else if (k === 'PageUp') wtStep(-0.9);
        else if (k === 'ArrowDown') { body.scrollBy({ top: 90 }); userMoved = true; }
        else if (k === 'ArrowUp') { body.scrollBy({ top: -90 }); userMoved = true; }
        else if (k === 'ArrowRight') jumpTo(idx + 1);
        else if (k === 'ArrowLeft') jumpTo(idx - 1);
        else if (k === 'Home') jumpTo(0);
        else if (k === 'End') { body.scrollTo({ top: body.scrollHeight }); userMoved = true; }
        else handled = false;
      } else {
        var fwd = k === 'ArrowRight' ? mode !== 'rtl' : k === 'ArrowLeft' ? mode === 'rtl' : null;
        if (mode === 'vertical' && (k === 'ArrowDown' || k === 'j' || k === ' ' && !e.shiftKey || k === 'PageDown')) vStep(1);
        else if (mode === 'vertical' && (k === 'ArrowUp' || k === 'k' || (k === ' ' && e.shiftKey) || k === 'PageUp')) vStep(-1);
        else if (fwd === true) nextPage();
        else if (fwd === false) prevPage();
        else if (k === 'ArrowDown' || k === 'j' || k === 'PageDown' || (k === ' ' && !e.shiftKey)) nextPage();
        else if (k === 'ArrowUp' || k === 'k' || k === 'PageUp' || (k === ' ' && e.shiftKey)) prevPage();
        else if (k === 'Home') showPage(0);
        else if (k === 'End') showPage(n - 1);
        else handled = false;
      }
      if (handled) e.preventDefault();
    }
    document.addEventListener('keydown', onKey);

    /* ---- boot reader ---- */
    rd.setAttribute('data-mode', mode);
    slider.style.direction = mode === 'rtl' ? 'rtl' : 'ltr';
    if (n === 0) {
      body.appendChild(emptyState(t('no_pages'), null, h('button', { class: 'btn', type: 'button', onclick: backToSeries }, t('back_to_series'))));
    } else if (isPdf) buildPdf();
    else if (mode === 'webtoon') { completed = false; buildWebtoon(); }
    else { completed = idx >= n - 1; buildPaged(); }
    updateUi();
    body.focus({ preventScroll: true });
    if (!isPdf) pokeChrome(2800);
    if (!isPdf && n > 0) showTapZones(false);
    saveSoon();
    document.title = (ch.seriesTitle || 'Yomi') + ' ' + (chLabel || '') + ' · Yomi';

    ctx.onCleanup(function () {
      destroyed = true;
      clearTimeout(hideTimer); cancelAnimationFrame(sliderRaf);
      clearTapZones();
      resetZoom();
      setZoomTarget(null);
      body.removeEventListener('touchstart', onZoomTouchStart);
      body.removeEventListener('touchmove', onZoomTouchMove);
      body.removeEventListener('touchend', onZoomTouchEnd);
      body.removeEventListener('touchcancel', onZoomTouchEnd);
      body.removeEventListener('wheel', onZoomWheel);
      body.removeEventListener('pointerdown', onZoomPointerDown);
      body.removeEventListener('pointermove', onZoomPointerMove);
      body.removeEventListener('pointerup', onZoomPointerUp);
      body.removeEventListener('pointercancel', onZoomPointerUp);
      body.removeEventListener('dblclick', onZoomDblClick);
      if (vpMeta && vpPrev != null) vpMeta.setAttribute('content', vpPrev);
      document.removeEventListener('keydown', onKey);
      document.removeEventListener('visibilitychange', onHide);
      window.removeEventListener('pagehide', flush);
      if (io) io.disconnect();
      saveSoon.cancel();
      // final flush (kept in S.pendingSave so the next reader waits for it)
      save(completed);
      if (document.fullscreenElement) document.exitFullscreen().catch(function () { });
      document.title = 'Yomi';
    });
  }

  /* ================================================================
   * Settings panel (role-based sidebar)
   * ================================================================ */
  async function viewSettings(root, rt, ctx) {
    if (!S.me) { location.hash = '#/'; return; }
    var isAdmin = !!S.me.isAdmin;
    var tabs = isAdmin ? ['libraries', 'users', 'settings', 'jobs'] : ['settings'];
    var tab = rt.query.get('tab') || 'settings';
    if (tabs.indexOf(tab) < 0) { location.hash = '#/settings?tab=settings'; return; }

    var page = ctx.soft ? root.querySelector('.settings-page') : null;
    var content, nav;
    if (page) {
      content = page.querySelector('.settings-body');
      nav = page.querySelector('.settings-nav');
      Array.prototype.forEach.call(nav.children, function (a) {
        var on = a.getAttribute('data-tab') === tab;
        a.classList.toggle('on', on);
        if (on) a.setAttribute('aria-current', 'page');
        else a.removeAttribute('aria-current');
      });
      clear(content);
    } else {
      content = h('div', { class: 'settings-body admin-body' });
      page = h('div', { class: 'page settings-page' },
        h('h1', { class: 'display page-title' }, t('nav_settings')),
        h('div', { class: 'settings-layout' },
          h('nav', { class: 'settings-nav', 'aria-label': t('nav_settings') }, tabs.map(function (k) {
            return h('a', {
              href: '#/settings?tab=' + k,
              'data-tab': k,
              class: tab === k ? 'on' : '',
              'aria-current': tab === k ? 'page' : null
            }, t('tab_' + k));
          })),
          content));
      root.appendChild(page);
    }

    var timers = [];
    ctx.onCleanup(function () { timers.forEach(clearInterval); });
    var poll = function (fn, ms) { var id = setInterval(function () { if (ctx.alive()) fn(); }, ms); timers.push(id); return id; };

    content.appendChild(spinner());
    if (tab === 'libraries') await adminLibraries(content, ctx, poll);
    else if (tab === 'users') await adminUsers(content, ctx);
    else if (tab === 'jobs') await adminJobs(content, ctx, poll);
    else await userSettings(content, ctx);
  }

  /* ---- user + optional server settings ---- */
  async function userSettings(box, ctx) {
    var ver = (S.info && S.info.version) || 'dev';
    clear(box).appendChild(h('div', { class: 'stack settings-block' },
      h('h3', { class: 'display' }, t('settings_prefs')),
      h('div', { class: 'field' }, h('span', { class: 'lbl' }, t('language')), langToggle()),
      h('div', { class: 'row' },
        h('span', { class: 'muted app-version', title: t('version') }, t('version') + ' ' + ver))));
    if (!S.me.isAdmin) return;

    var serverBox = h('div', { class: 'settings-block' });
    box.appendChild(h('h3', { class: 'display' }, t('settings_server')));
    box.appendChild(serverBox);
    await adminSettings(serverBox, ctx);
  }

  /* ---- libraries ---- */
  async function adminLibraries(box, ctx, poll) {
    var libs = await api('/api/admin/libraries');
    if (!ctx.alive()) return;
    var listEl = h('div', { class: 'stack' });
    var rows = {};

    function scanText(st) {
      if (!st) return '';
      if (st.running) return t('scan_' + (st.phase || 'scanning')) + (st.seriesTotal ? ' ' + st.seriesDone + '/' + st.seriesTotal : '');
      if (st.phase === 'error') return t('scan_error') + (st.error ? ': ' + st.error : '');
      if (st.phase === 'done') return t('scan_done', { a: st.seriesAdded, c: st.chaptersAdded });
      return '';
    }
    function paintRow(l) {
      var r = rows[l.id]; if (!r) return;
      var st = l.scan, running = st && st.running;
      r.status.textContent = scanText(st);
      r.status.classList.toggle('err', !!(st && st.phase === 'error'));
      r.bar.hidden = !running;
      r.bar.firstChild.style.width = running && st.seriesTotal ? Math.round(st.seriesDone / st.seriesTotal * 100) + '%' : (running ? '8%' : '0');
      r.bar.classList.toggle('indet', !!running && !st.seriesTotal);
      r.scanBtn.disabled = !!running;
      r.delBtn.disabled = !!running;
      r.meta.textContent = t('n_series', { n: l.seriesCount }, l.seriesCount) + ' · ' + t('last_scan') + ': ' + fmtDate(l.lastScan);
    }
    async function refresh() {
      try {
        libs = await api('/api/admin/libraries');
        if (!ctx.alive()) return;
        var ids = libs.map(function (l) { return l.id; }).join();
        if (ids !== Object.keys(rows).join()) paintAll(); else libs.forEach(paintRow);
      } catch (e) { /* ignore poll errors */ }
    }
    function paintAll() {
      clear(listEl); rows = {};
      if (!libs.length) listEl.appendChild(h('p', { class: 'muted' }, t('no_libraries')));
      libs.forEach(function (l) {
        var r = {
          status: h('span', { class: 'scan-status' }),
          bar: h('div', { class: 'pbar', hidden: true }, h('i')),
          meta: h('span', { class: 'muted' }),
          scanBtn: h('button', { class: 'btn sm', type: 'button', onclick: async function () {
            try { await api('/api/admin/libraries/' + enc(l.id) + '/scan', { method: 'POST' }); toast(t('scan_started')); refresh(); } catch (e) { errToast(e); }
          } }, icon('refresh', 'xs'), t('scan')),
          delBtn: h('button', { class: 'btn sm danger', type: 'button', onclick: async function () {
            if (!confirm(t('confirm_delete_library', { name: l.name }))) return;
            try { await api('/api/admin/libraries/' + enc(l.id), { method: 'DELETE' }); S.meta = null; toast(t('deleted')); refresh(); } catch (e) { errToast(e); }
          } }, icon('trash', 'xs'), t('delete'))
        };
        rows[l.id] = r;
        listEl.appendChild(h('div', { class: 'row-card' },
          h('div', { class: 'rc-main' },
            h('strong', null, l.name),
            h('code', null, l.rootPath),
            r.meta, r.status, r.bar),
          h('div', { class: 'rc-act' }, r.scanBtn,
            h('button', { class: 'btn sm', type: 'button', onclick: async function () {
              var name = prompt(t('library_name'), l.name);
              if (!name || name === l.name) return;
              try { await api('/api/admin/libraries/' + enc(l.id), { method: 'PUT', json: { name: name } }); refresh(); } catch (e) { errToast(e); }
            } }, icon('edit', 'xs'), t('rename')),
            r.delBtn)));
        paintRow(l);
      });
    }

    var nameIn = h('input', { type: 'text', required: true, placeholder: t('library_name'), 'aria-label': t('library_name') });
    var pathIn = h('input', { type: 'text', required: true, placeholder: t('library_path_ph'), 'aria-label': t('library_path'), spellcheck: 'false' });
    var addForm = h('form', {
      class: 'addform',
      onsubmit: async function (e) {
        e.preventDefault();
        try {
          await api('/api/admin/libraries', { method: 'POST', json: { name: nameIn.value.trim(), rootPath: pathIn.value.trim() } });
          nameIn.value = ''; pathIn.value = ''; S.meta = null; toast(t('library_added')); refresh();
        } catch (ex) { errToast(ex); }
      }
    },
      h('h3', { class: 'display' }, t('add_library')),
      h('div', { class: 'form-grid' }, field(t('library_name'), nameIn),
        field(t('library_path'), h('div', { class: 'inline' }, pathIn, h('button', { class: 'btn', type: 'button', onclick: function () { browseDialog(pathIn.value.trim(), function (p) { pathIn.value = p; if (!nameIn.value) nameIn.value = p.replace(/[\\/]+$/, '').split(/[\\/]/).pop() || p; }); } }, icon('folder', 'xs'), t('browse'))))),
      h('button', { class: 'btn primary', type: 'submit' }, icon('plus', 'xs'), t('add_library')));

    clear(box).append(listEl, addForm);
    paintAll();
    poll(refresh, 1800);
  }

  function browseDialog(start, onPick) {
    var dlg = h('dialog', { class: 'dlg', 'aria-label': t('browse') });
    var pathEl = h('code', { class: 'bpath' });
    var listEl = h('div', { class: 'blist' });
    var useBtn = h('button', { class: 'btn primary', type: 'button', disabled: true }, t('use_folder'));
    var cur = '';
    async function go(p) {
      clear(listEl).appendChild(spinner());
      try {
        var d = await api('/api/admin/browse?path=' + enc(p || ''));
        cur = d.path || '';
        pathEl.textContent = cur || t('drives');
        useBtn.disabled = !cur;
        clear(listEl);
        if (d.parent !== null && d.parent !== undefined) listEl.appendChild(h('button', { class: 'bitem up', type: 'button', onclick: function () { go(d.parent); } }, icon('up', 'xs'), '..'));
        (d.entries || []).forEach(function (en) {
          listEl.appendChild(h('button', { class: 'bitem', type: 'button', onclick: function () { go(en.path); } }, icon('folder', 'xs'), en.name));
        });
        if (d.looksLikeLibrary) listEl.insertBefore(h('div', { class: 'hint' }, t('looks_like_library')), listEl.firstChild);
        if (!(d.entries || []).length) listEl.appendChild(h('p', { class: 'muted pad' }, t('no_subfolders')));
      } catch (e) { clear(listEl).appendChild(h('p', { class: 'form-err' }, e.message)); if (p) go(''); }
    }
    useBtn.addEventListener('click', function () { onPick(cur); dlg.close(); });
    dlg.append(
      h('div', { class: 'dlg-head' }, h('h3', { class: 'display' }, t('browse')), iconBtn('x', t('close'), function () { dlg.close(); })),
      pathEl, listEl,
      h('div', { class: 'dlg-foot' }, h('button', { class: 'btn', type: 'button', onclick: function () { dlg.close(); } }, t('cancel')), useBtn));
    dlg.addEventListener('close', function () { dlg.remove(); });
    document.body.appendChild(dlg);
    dlg.showModal();
    go(start || '');
  }

  /* ---- users ---- */
  async function adminUsers(box, ctx) {
    var res = await Promise.all([api('/api/admin/users'), api('/api/admin/libraries')]);
    if (!ctx.alive()) return;
    var users = res[0] || [], libs = res[1] || [];

    function libNames(u) {
      if (!u.libraries || !u.libraries.length) return t('all_libraries');
      return u.libraries.map(function (id) { var l = libs.find(function (x) { return x.id === id; }); return l ? l.name : id; }).join(', ');
    }
    function paint() {
      clear(box);
      box.appendChild(h('div', { class: 'row between' }, h('h3', { class: 'display' }, t('tab_users')),
        h('button', { class: 'btn primary', type: 'button', onclick: function () { userDialog(null); } }, icon('plus', 'xs'), t('add_user'))));
      var tb = h('div', { class: 'stack' });
      users.forEach(function (u) {
        tb.appendChild(h('div', { class: 'row-card' },
          h('div', { class: 'rc-main' },
            h('strong', null, u.username, u.isAdmin ? h('span', { class: 'badge' }, t('admin_badge')) : null),
            h('span', { class: 'muted' }, (u.email ? u.email + ' · ' : '') + t('max_age') + ': ' + u.maxAgeRating),
            h('span', { class: 'muted' }, t('libraries') + ': ' + libNames(u)),
            h('span', { class: 'muted' }, t('last_seen') + ': ' + fmtDate(u.lastSeen))),
          h('div', { class: 'rc-act' },
            h('button', { class: 'btn sm', type: 'button', onclick: function () { userDialog(u); } }, icon('edit', 'xs'), t('edit')),
            h('button', { class: 'btn sm danger', type: 'button', disabled: S.me && S.me.username.toLowerCase() === u.username.toLowerCase(), onclick: async function () {
              if (!confirm(t('confirm_delete_user', { name: u.username }))) return;
              try { await api('/api/admin/users/' + enc(u.username), { method: 'DELETE' }); users = users.filter(function (x) { return x !== u; }); paint(); toast(t('deleted')); } catch (e) { errToast(e); }
            } }, icon('trash', 'xs'), t('delete')))));
      });
      box.appendChild(tb);
    }
    function userDialog(u) {
      var isNew = !u;
      var dlg = h('dialog', { class: 'dlg' });
      var un = h('input', { type: 'text', required: true, value: u ? u.username : '', disabled: !isNew, autocomplete: 'off' });
      var pw = h('input', { type: 'password', autocomplete: 'new-password', required: isNew, minlength: 4, placeholder: isNew ? '' : t('leave_blank') });
      var em = h('input', { type: 'email', value: u ? u.email || '' : '' });
      var adm = h('input', { type: 'checkbox', checked: u ? u.isAdmin : false });
      var age = h('select', null, ['E', 'T', 'M', 'X'].map(function (a) { return h('option', { value: a, selected: (u ? u.maxAgeRating : 'T') === a }, a + ' — ' + t('age_desc_' + a)); }));
      var libChecks = libs.map(function (l) {
        return { l: l, cb: h('input', { type: 'checkbox', checked: !!(u && u.libraries && u.libraries.indexOf(l.id) >= 0) }) };
      });
      var err = h('p', { class: 'form-err', hidden: true });
      var form = h('form', {
        onsubmit: async function (e) {
          e.preventDefault();
          var body = { email: em.value.trim(), isAdmin: adm.checked, maxAgeRating: age.value, libraries: libChecks.filter(function (x) { return x.cb.checked; }).map(function (x) { return x.l.id; }) };
          if (pw.value) body.password = pw.value;
          try {
            var saved;
            if (isNew) { body.username = un.value.trim(); saved = await api('/api/admin/users', { method: 'POST', json: body }); users.push(saved); }
            else { saved = await api('/api/admin/users/' + enc(u.username), { method: 'PUT', json: body }); users[users.indexOf(u)] = saved; }
            dlg.close(); paint(); toast(t('saved'));
          } catch (ex) { err.textContent = ex.message; err.hidden = false; }
        }
      },
        h('div', { class: 'dlg-head' }, h('h3', { class: 'display' }, isNew ? t('add_user') : t('edit_user') + ' · ' + u.username), iconBtn('x', t('close'), function () { dlg.close(); })),
        h('div', { class: 'dlg-body' },
          field(t('username'), un), field(t('password'), pw), field(t('email'), em),
          h('label', { class: 'check' }, adm, t('is_admin')),
          field(t('max_age'), age),
          libs.length ? h('fieldset', { class: 'field' }, h('legend', { class: 'lbl' }, t('libraries')), h('small', { class: 'muted' }, t('libraries_hint')),
            libChecks.map(function (x) { return h('label', { class: 'check' }, x.cb, x.l.name); })) : null,
          err),
        h('div', { class: 'dlg-foot' }, h('button', { class: 'btn', type: 'button', onclick: function () { dlg.close(); } }, t('cancel')), h('button', { class: 'btn primary', type: 'submit' }, t('save'))));
      dlg.appendChild(form);
      dlg.addEventListener('close', function () { dlg.remove(); });
      document.body.appendChild(dlg);
      dlg.showModal();
    }
    paint();
  }

  /* ---- settings ---- */
  async function adminSettings(box, ctx) {
    var s = await api('/api/admin/settings');
    if (!ctx.alive()) return;
    var prio = h('textarea', { rows: 4, spellcheck: 'false', 'aria-label': t('source_priority') }, (s.source_priority || '').split(',').map(function (x) { return x.trim(); }).filter(Boolean).join(', '));
    var hours = h('input', { type: 'number', min: 0, max: 8760, value: s.auto_scan_hours || 0 });
    var rm = h('select', null, ['auto', 'webtoon', 'ltr', 'rtl', 'vertical'].map(function (m) { return h('option', { value: m, selected: s.default_reader_mode === m }, m === 'auto' ? t('mode_auto') : t('mode_' + m)); }));
    var refreshOpts = [
      { v: 30, k: 'random_refresh_30m' },
      { v: 60, k: 'random_refresh_1h' },
      { v: 360, k: 'random_refresh_6h' },
      { v: 720, k: 'random_refresh_12h' },
      { v: 1440, k: 'random_refresh_24h' }
    ];
    var curRefresh = refreshOpts.some(function (o) { return o.v === s.random_refresh_minutes; }) ? s.random_refresh_minutes : 360;
    var refresh = h('select', null, refreshOpts.map(function (o) {
      return h('option', { value: String(o.v), selected: curRefresh === o.v }, t(o.k));
    }));
    var chipsBox = h('div', { class: 'chips' });

    function list() { return prio.value.split(/[,\n]/).map(function (x) { return x.trim(); }).filter(Boolean); }
    function paintChips() {
      clear(chipsBox);
      var cur = list().map(function (x) { return x.toLowerCase(); });
      var seen = {};
      (s.sources || []).forEach(function (so) {
        if (seen[so.name]) return; seen[so.name] = 1;
        var on = cur.indexOf(so.name.toLowerCase()) >= 0;
        chipsBox.appendChild(h('button', { type: 'button', class: 'chip' + (on ? ' on' : ''), 'aria-pressed': on ? 'true' : 'false', onclick: function () {
          var l = list();
          if (on) l = l.filter(function (x) { return x.toLowerCase() !== so.name.toLowerCase(); }); else l.push(so.name);
          prio.value = l.join(', '); paintChips();
        } }, so.name, h('em', null, so.seriesCount)));
      });
    }
    prio.addEventListener('input', paintChips);
    paintChips();

    var form = h('form', {
      class: 'stack',
      onsubmit: async function (e) {
        e.preventDefault();
        try {
          await api('/api/admin/settings', { method: 'PUT', json: {
            source_priority: list().join(','),
            auto_scan_hours: parseInt(hours.value, 10) || 0,
            default_reader_mode: rm.value,
            random_refresh_minutes: parseInt(refresh.value, 10)
          } });
          S.chCache = {};
          toast(t('saved'));
        } catch (ex) { errToast(ex); }
      }
    },
      field(t('source_priority'), prio, t('source_priority_hint')),
      h('div', { class: 'field' }, h('span', { class: 'lbl' }, t('known_sources')), chipsBox),
      field(t('auto_scan_hours'), hours, t('auto_scan_hint')),
      field(t('default_reader_mode'), rm, t('default_reader_mode_hint')),
      field(t('random_refresh'), refresh, t('random_refresh_hint')),
      h('div', { class: 'row' }, h('button', { class: 'btn primary', type: 'submit' }, t('save'))));
    clear(box).appendChild(form);
  }

  /* ---- jobs ---- */
  async function adminJobs(box, ctx, poll) {
    var table = h('div', { class: 'stack' });
    async function load() {
      var jobs = await api('/api/admin/jobs');
      if (!ctx.alive()) return;
      clear(table);
      if (!jobs.length) { table.appendChild(h('p', { class: 'muted' }, t('no_jobs'))); return; }
      jobs.forEach(function (j) {
        var pct = j.total ? Math.round(j.done / j.total * 100) : (j.status === 'done' ? 100 : 0);
        table.appendChild(h('div', { class: 'row-card job ' + j.status },
          h('div', { class: 'rc-main' },
            h('strong', null, j.label || j.kind, h('span', { class: 'badge st-' + j.status }, t('job_' + j.status))),
            h('span', { class: 'muted' }, fmtDate(j.startedAt) + (j.finishedAt ? ' → ' + fmtDate(j.finishedAt) : '')),
            j.message ? h('span', { class: 'muted' }, j.message) : null,
            j.status === 'running' ? h('div', { class: 'pbar' }, h('i', { style: { width: pct + '%' } })) : null),
          j.total ? h('span', { class: 'muted nowrap' }, j.done + ' / ' + j.total) : null));
      });
    }
    clear(box).append(h('div', { class: 'row between' }, h('h3', { class: 'display' }, t('tab_jobs')), h('button', { class: 'btn sm', type: 'button', onclick: function () { load().catch(errToast); } }, icon('refresh', 'xs'), t('refresh'))), table);
    await load();
    poll(function () { load().catch(function () { }); }, 3000);
  }

  /* ================================================================
   * Boot
   * ================================================================ */
  async function bootSession() {
    var res = await Promise.all([api('/api/me', { noRedirect: true }), api('/api/preferences', { noRedirect: true }).catch(function () { return {}; })]);
    S.me = res[0]; S.prefs = res[1] || {}; S.authed = true; S.meta = null;
    if (!localStorage.getItem('yr_lang') && S.prefs.lang && window.I18N[S.prefs.lang]) { S.lang = S.prefs.lang; window.setLang(S.lang); }
  }

  async function boot() {
    document.documentElement.lang = S.lang;
    try {
      S.info = await api('/api/auth/info', { noRedirect: true });
    } catch (e) {
      S.info = { provider: 'local', authenticated: false };
    }
    if (S.info.authenticated) {
      try { await bootSession(); } catch (e) { S.authed = false; }
    }
    if (!S.authed && S.info.provider === 'mock' && !sessionStorage.getItem('yr_logged_out')) {
      var last = +sessionStorage.getItem('yr_sso_try') || 0;
      if (Date.now() - last > 15000) { sessionStorage.setItem('yr_sso_try', String(Date.now())); location.href = '/auth/login'; return; }
    }
    if (S.authed) sessionStorage.removeItem('yr_logged_out');
    if (!location.hash || location.hash === '#') location.hash = S.authed ? '#/' : '#/login';
    window.addEventListener('hashchange', route);
    window.addEventListener('yr-lang', function () { document.documentElement.lang = S.lang; });
    route();
    registerServiceWorker();
  }

  function registerServiceWorker() {
    if (!('serviceWorker' in navigator)) return;
    var go = function () {
      navigator.serviceWorker.register('/sw.js').catch(function (err) {
        console.warn('service worker registration failed', err);
      });
    };
    if (document.readyState === 'complete') go();
    else window.addEventListener('load', go);
  }

  boot();
})();
