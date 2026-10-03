/* Tablekeeper — screens for /, /signup, /login and /lookup.
   Hand-written ES2019, no framework, no bundler, no network beyond this service.
   One document serves all four routes; the screen is chosen from location.pathname.
   See DESIGN.md for the visual system and the per-screen layouts this implements. */

(function () {
  'use strict';

  /* =============================================================== helpers */

  function h(tag, attrs, kids) {
    var n = document.createElement(tag), k, v, list, i, c;
    if (attrs) {
      for (k in attrs) {
        if (!Object.prototype.hasOwnProperty.call(attrs, k)) continue;
        v = attrs[k];
        if (v === null || v === undefined || v === false) continue;
        if (k === 'text') { n.textContent = v; }
        else if (k === 'html') { n.innerHTML = v; }          /* own static markup only */
        else if (k === 'class') { n.className = v; }
        else { n.setAttribute(k, v === true ? '' : String(v)); }
      }
    }
    if (kids !== null && kids !== undefined) {
      list = Array.isArray(kids) ? kids : [kids];
      for (i = 0; i < list.length; i++) {
        c = list[i];
        if (c === null || c === undefined || c === false) continue;
        n.appendChild(typeof c === 'string' ? document.createTextNode(c) : c);
      }
    }
    return n;
  }

  function q(sel, root) { return (root || document).querySelector(sel); }
  function qa(sel, root) { return Array.prototype.slice.call((root || document).querySelectorAll(sel)); }
  function clear(node) { while (node && node.firstChild) node.removeChild(node.firstChild); }
  function show(node, on) { if (node) { if (on) node.removeAttribute('hidden'); else node.setAttribute('hidden', ''); } }

  var SVG_ATTRS = 'viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.75" ' +
                  'stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"';

  /* Six line glyphs on a shared 24x24 grid, drawn from plain primitives in a Tabler-consistent
     geometry. Original work, not copies of Tabler's paths — see DESIGN.md §2.7 and ICONS.md. */
  var ICON = {
    alert: '<svg ' + SVG_ATTRS + '><circle cx="12" cy="12" r="9"/><path d="M12 7.5v5.5"/><path d="M12 16.5h.01"/></svg>',
    check: '<svg ' + SVG_ATTRS + '><path d="M5 12.5 9.5 17 19 7.5"/></svg>',
    checkCircle: '<svg ' + SVG_ATTRS + '><circle cx="12" cy="12" r="9"/><path d="M8.5 12.5 11 15l4.5-5"/></svg>',
    crossCircle: '<svg ' + SVG_ATTRS + '><circle cx="12" cy="12" r="9"/><path d="M9 9l6 6M15 9l-6 6"/></svg>',
    calendar: '<svg ' + SVG_ATTRS + '><rect x="4" y="5.5" width="16" height="14" rx="2"/><path d="M4 10h16M9 3.5v4M15 3.5v4"/></svg>',
    search: '<svg ' + SVG_ATTRS + '><circle cx="10.5" cy="10.5" r="6"/><path d="M15 15l4.5 4.5"/></svg>'
  };

  var WEEKDAY_LONG = ['Sunday', 'Monday', 'Tuesday', 'Wednesday', 'Thursday', 'Friday', 'Saturday'];
  var WEEKDAY_SHORT = ['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat'];
  var WEEKDAY_CODE = ['sun', 'mon', 'tue', 'wed', 'thu', 'fri', 'sat'];
  var WEEK_ORDER = ['mon', 'tue', 'wed', 'thu', 'fri', 'sat', 'sun'];
  var MONTH_LONG = ['January', 'February', 'March', 'April', 'May', 'June',
                    'July', 'August', 'September', 'October', 'November', 'December'];
  var MONTH_SHORT = ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun',
                     'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec'];

  /* YYYY-MM-DD is treated as a plain calendar date: built field-by-field so it never shifts
     by a timezone the way new Date("2026-10-10") would. */
  function ymdParts(ymd) {
    var m = /^(\d{4})-(\d{2})-(\d{2})$/.exec(String(ymd || ''));
    if (!m) return null;
    return { y: +m[1], m: +m[2], d: +m[3] };
  }
  function ymdWeekdayIndex(ymd) {
    var p = ymdParts(ymd);
    if (!p) return -1;
    return new Date(p.y, p.m - 1, p.d).getDay();
  }
  function fmtDateLong(ymd) {               /* "Saturday 10 October" */
    var p = ymdParts(ymd);
    if (!p) return String(ymd || '');
    return WEEKDAY_LONG[ymdWeekdayIndex(ymd)] + ' ' + p.d + ' ' + MONTH_LONG[p.m - 1];
  }
  function fmtDateShort(ymd) {              /* "Sat 10 Oct 2026" */
    var p = ymdParts(ymd);
    if (!p) return String(ymd || '');
    return WEEKDAY_SHORT[ymdWeekdayIndex(ymd)] + ' ' + p.d + ' ' + MONTH_SHORT[p.m - 1] + ' ' + p.y;
  }
  function todayYMD() {
    var d = new Date(), m = d.getMonth() + 1, day = d.getDate();
    return d.getFullYear() + '-' + (m < 10 ? '0' + m : m) + '-' + (day < 10 ? '0' + day : day);
  }
  function hhmmOf(startsAtLocal) {          /* "2026-10-10T19:00" -> "19:00" */
    var s = String(startsAtLocal || ''), i = s.indexOf('T');
    return i < 0 ? s : s.slice(i + 1, i + 6);
  }
  function dateOf(startsAtLocal) {
    var s = String(startsAtLocal || ''), i = s.indexOf('T');
    return i < 0 ? s : s.slice(0, i);
  }
  function plural(n, one, many) { return n === 1 ? one : many; }
  function guestsText(n) { return n + ' ' + plural(n, 'guest', 'guests'); }
  function minutesText(mins) {
    if (!mins) return '';
    if (mins % 60 === 0) { var hrs = mins / 60; return hrs + ' ' + plural(hrs, 'hour', 'hours'); }
    return mins + ' minutes';
  }

  /* A table whose label is bare digits reads as an identifier on its own, so it gets the word
     "Table" in front ("2" -> "Table 2", per the brief's own example). A label that is already
     a name is shown exactly as the restaurant wrote it ("Window 1", "Booth"). Nothing here
     invents or hardcodes a name — every label comes from the API. */
  function tableName(label) {
    var s = String(label === null || label === undefined ? '' : label).trim();
    if (s === '') return 'Table';
    return /^\d+$/.test(s) ? 'Table ' + s : s;
  }
  function tablesPhrase(labels) { return labels.join(' + '); }

  function newIdempotencyKey() {
    try {
      if (window.crypto && window.crypto.randomUUID) return window.crypto.randomUUID();
      if (window.crypto && window.crypto.getRandomValues) {
        var a = new Uint8Array(16), out = '', i;
        window.crypto.getRandomValues(a);
        for (i = 0; i < a.length; i++) out += (a[i] + 0x100).toString(16).slice(1);
        return out;
      }
    } catch (e) { /* fall through */ }
    return 'k' + Date.now() + '-' + Math.random().toString(36).slice(2, 12);
  }

  /* =============================================================== theme */

  var themeBtn = q('[data-theme-toggle]');

  function applyTheme(t, remember) {
    document.documentElement.setAttribute('data-theme', t);
    if (remember) { try { localStorage.setItem('tk-theme', t); } catch (e) { /* private mode */ } }
    var dark = t === 'dark';
    if (themeBtn) {
      themeBtn.setAttribute('aria-pressed', dark ? 'true' : 'false');
      show(q('[data-icon-moon]', themeBtn), !dark);
      show(q('[data-icon-sun]', themeBtn), dark);
      var lab = q('[data-theme-label]', themeBtn);
      if (lab) lab.textContent = dark ? 'Switch to light theme' : 'Switch to dark theme';
    }
  }

  function currentTheme() { return document.documentElement.getAttribute('data-theme') === 'dark' ? 'dark' : 'light'; }

  applyTheme(currentTheme(), false);
  if (themeBtn) {
    themeBtn.addEventListener('click', function () {
      applyTheme(currentTheme() === 'dark' ? 'light' : 'dark', true);
    });
  }
  /* Follow the system preference until the person chooses for themselves. */
  try {
    var mq = window.matchMedia('(prefers-color-scheme: dark)');
    var onSys = function (e) {
      var stored = null;
      try { stored = localStorage.getItem('tk-theme'); } catch (err) { /* ignore */ }
      if (stored !== 'light' && stored !== 'dark') applyTheme(e.matches ? 'dark' : 'light', false);
    };
    if (mq.addEventListener) mq.addEventListener('change', onSys);
    else if (mq.addListener) mq.addListener(onSys);
  } catch (e) { /* ignore */ }

  /* =============================================================== session */

  var SESSION_KEY = 'tk-session';
  var session = null;

  function loadSession() {
    try {
      var raw = localStorage.getItem(SESSION_KEY);
      if (!raw) return null;
      var s = JSON.parse(raw);
      return (s && typeof s.token === 'string' && s.token) ? s : null;
    } catch (e) { return null; }
  }
  function saveSession(s) {
    session = s;
    try {
      if (s) localStorage.setItem(SESSION_KEY, JSON.stringify(s));
      else localStorage.removeItem(SESSION_KEY);
    } catch (e) { /* private mode: the session simply does not outlive the page */ }
  }
  session = loadSession();

  /* =============================================================== api */

  var LOST_RESPONSE_MS = 6000;

  /* Resolves to {ok, status, data} for anything the server answered, and rejects with
     {lost:true} only when no answer arrived at all. The caller distinguishes a refusal
     (answered) from an uncertain outcome (not answered) on exactly that boundary. */
  function api(method, path, opts) {
    opts = opts || {};
    var init = { method: method, headers: {}, cache: 'no-store' };
    if (opts.body !== undefined) {
      init.headers['Content-Type'] = 'application/json';
      init.body = JSON.stringify(opts.body);
    }
    if (opts.token) init.headers['Authorization'] = 'Bearer ' + opts.token;
    if (opts.idempotencyKey) init.headers['Idempotency-Key'] = opts.idempotencyKey;

    var ctl = null, timer = null;
    if (opts.timeoutMs && typeof AbortController === 'function') {
      ctl = new AbortController();
      init.signal = ctl.signal;
      timer = setTimeout(function () { try { ctl.abort(); } catch (e) { /* ignore */ } }, opts.timeoutMs);
    }

    return fetch(path, init).then(function (res) {
      if (timer) clearTimeout(timer);
      return res.text().then(function (txt) {
        var data = null;
        if (txt) { try { data = JSON.parse(txt); } catch (e) { data = null; } }
        if (res.ok && data === null && txt) {
          /* answered, but not with something we can read: treat as no answer */
          var bad = new Error('unreadable response');
          bad.lost = true;
          throw bad;
        }
        return { ok: res.ok, status: res.status, data: data };
      }, function () {
        var cut = new Error('response body did not arrive');
        cut.lost = true;
        throw cut;
      });
    }, function (err) {
      if (timer) clearTimeout(timer);
      var lost = new Error('no response');
      lost.lost = true;
      lost.cause = err;
      throw lost;
    });
  }

  function errCode(r) { return (r && r.data && r.data.error && r.data.error.code) || ''; }
  function errMessage(r) { return (r && r.data && r.data.error && r.data.error.message) || ''; }

  /* =============================================================== notices */

  function notice(kind, testid, title, bodyNodes, extra) {
    var cls = kind === 'danger' ? 'notice notice-danger'
            : kind === 'warning' ? 'notice notice-warning'
            : 'notice notice-auth';
    var glyph = kind === 'danger' ? ICON.crossCircle : ICON.alert;
    var body = [h('p', { class: 'n-title', text: title })];
    (bodyNodes || []).forEach(function (n) { body.push(n); });
    if (extra) body.push(extra);
    return h('div', { class: cls, 'data-testid': testid, role: 'status', 'aria-live': 'polite' }, [
      h('span', { html: glyph }),
      h('div', { class: 'n-body' }, body)
    ]);
  }

  /* =============================================================== top bar */

  var barAuth = q('[data-bar-auth]');
  var barSlot = q('[data-bar-auth-slot]');

  function renderTopBar() {
    if (!barSlot) return;
    clear(barSlot);
    if (session) {
      barSlot.appendChild(h('span', { class: 'bar-who' }, [
        'Signed in as ',
        h('strong', { 'data-testid': 'current-user', text: session.display_name || session.email || 'you' })
      ]));
      var out = h('button', { class: 'btn btn-secondary', type: 'button', 'data-testid': 'logout-button', text: 'Sign out' });
      out.addEventListener('click', signOut);
      barSlot.appendChild(out);
    } else {
      barSlot.appendChild(h('a', { class: 'btn btn-secondary', href: signInHref(), text: 'Sign in' }));
      barSlot.appendChild(h('a', { class: 'btn btn-primary', href: signUpHref(), text: 'Create account' }));
    }
    qa('.nav a').forEach(function (a) {
      if (a.getAttribute('data-nav') === route) a.setAttribute('aria-current', 'page');
      else a.removeAttribute('aria-current');
    });
  }

  function signOut() {
    saveSession(null);
    renderTopBar();
    if (route === '/') { home.onSignedOut(); }
    else if (route === '/lookup') { lookup.reset(); }
  }

  /* =============================================================== routing */

  var path = window.location.pathname.replace(/\/+$/, '') || '/';
  var route = (path === '/signup' || path === '/login' || path === '/lookup') ? path : '/';
  var params = new URLSearchParams(window.location.search);

  /* The chosen slot travels through sign-in as plain query parameters and nothing else, so a
     person who clicks an available time while signed out comes back to that same time. */
  function slotQuery() {
    if (!home.state.search) return '';
    var p = new URLSearchParams();
    p.set('r', home.state.search.restaurantId);
    p.set('d', home.state.search.date);
    p.set('p', String(home.state.search.partySize));
    if (home.state.sel) {
      p.set('s', home.state.sel.startsAtLocal);
      p.set('t', home.state.sel.tableIds.join('+'));
    }
    return p.toString();
  }
  function signInHref() {
    var qs = route === '/' ? slotQuery() : '';
    return '/login' + (qs ? '?next=' + encodeURIComponent('/?' + qs) : '');
  }
  function signUpHref() {
    var qs = route === '/' ? slotQuery() : '';
    return '/signup' + (qs ? '?next=' + encodeURIComponent('/?' + qs) : '');
  }

  /* =============================================================== home */

  var home = (function () {
    var screen = q('[data-screen="home"]');
    var form = q('[data-search-form]', screen);
    var selRestaurant = q('[data-testid="restaurant-select"]', screen);
    var inDate = q('[data-testid="date-input"]', screen);
    var inParty = q('[data-testid="party-size-input"]', screen);
    var btnSearch = q('[data-testid="search-button"]', screen);
    var hours = q('[data-hours]', screen);
    var hoursZone = q('[data-hours-zone]', screen);
    var hoursDays = q('[data-hours-days]', screen);
    var work = q('[data-work]', screen);
    var firstRun = q('[data-first-run]', screen);
    var homeTop = q('[data-home-top]', screen);
    var resultsTitle = q('[data-results-title]', screen);
    var resultsSub = q('[data-results-sub]', screen);
    var legend = q('[data-legend]', screen);
    var resultsBody = q('[data-results-body]', screen);
    var reviewBody = q('[data-review-body]', screen);

    var state = {
      restaurants: [],
      rest: null,          /* restaurant detail the applied results describe */
      search: null,        /* {restaurantId, date, partySize} the applied results describe */
      slots: null,
      sel: null,
      loading: false,
      notice: null,        /* {kind:'error'|'uncertain', title, lines[]} */
      authPrompt: false,
      confirmed: null,     /* the reservation response to display */
      confirmedBody: null, /* the request body that produced it */
      submitting: false,
      restoring: null      /* a slot to re-select once results land */
    };

    /* ---- freshness: the newest search wins, whenever it happens to arrive -------------
       Every search takes the next sequence number. A response is applied only if its number
       is still the newest one issued. A slow earlier search therefore cannot restore its own
       grid, labels or form over a later one, regardless of arrival order. */
    var searchSeq = 0;

    /* ---- the pending booking identity -------------------------------------------------
       One idempotency key per (restaurant, tables, time, party size). The key survives an
       uncertain outcome so the retry is the same request; it is replaced the moment any field
       changes, which is what makes the next submission a new booking. */
    var pending = null;    /* {key, bodyStr} */

    function canonBody(b) {
      return JSON.stringify([b.restaurant_id, b.table_ids.slice(), b.starts_at_local, b.party_size]);
    }
    function keyFor(body) {
      var s = canonBody(body);
      if (!pending || pending.bodyStr !== s) pending = { key: newIdempotencyKey(), bodyStr: s };
      return pending.key;
    }

    /* ---- restaurant + capacity lookup ------------------------------------------------- */

    function tableById(rest, id) {
      var i;
      if (!rest || !rest.tables) return null;
      for (i = 0; i < rest.tables.length; i++) if (rest.tables[i].id === id) return rest.tables[i];
      return null;
    }
    function labelsFor(rest, ids) {
      return ids.map(function (id) {
        var t = tableById(rest, id);
        return t ? tableName(t.label) : id;
      });
    }
    function capacityFor(rest, ids) {
      var total = 0;
      ids.forEach(function (id) { var t = tableById(rest, id); if (t) total += t.capacity; });
      return total;
    }
    function validPairs(rest) {
      if (!rest || !rest.combinable) return [];
      return rest.combinable.filter(function (p) {
        return p && p.length === 2 && tableById(rest, p[0]) && tableById(rest, p[1]);
      });
    }

    /* ---- opening hours strip ---------------------------------------------------------- */

    function renderHours(rest, dateYMD) {
      if (!rest) { show(hours, false); return; }
      hoursZone.textContent = 'Times in ' + rest.timezone;
      clear(hoursDays);
      var todayCode = dateYMD ? WEEKDAY_CODE[ymdWeekdayIndex(dateYMD)] : null;
      WEEK_ORDER.forEach(function (code) {
        var spans = (rest.opening_hours || []).filter(function (o) {
          return String(o.weekday || '').toLowerCase() === code;
        }).map(function (o) { return o.opens + '–' + o.closes; });
        var txt = spans.length ? spans.join(', ') : 'Closed';
        var isSearched = code === todayCode;
        hoursDays.appendChild(h('li', { class: 'hday', 'data-today': isSearched ? 'true' : 'false' }, [
          h('span', { class: 'd' }, [
            WEEKDAY_SHORT[WEEKDAY_CODE.indexOf(code)],
            /* the highlight is a fill; say so in words too, so it is never colour alone */
            isSearched ? h('span', { class: 'sr-only', text: ' — the night you searched' }) : null
          ]),
          h('span', { class: 'h', text: txt })
        ]));
      });
      show(hours, true);
    }

    /* ---- the availability grid -------------------------------------------------------- */

    function rowsFor(rest) {
      var rows = [];
      (rest.tables || []).forEach(function (t) {
        rows.push({
          ids: [t.id],
          testPart: t.id,
          name: tableName(t.label),
          capacity: t.capacity,
          capText: 'Up to ' + guestsText(t.capacity)
        });
      });
      validPairs(rest).forEach(function (p) {
        var a = tableById(rest, p[0]), b = tableById(rest, p[1]);
        rows.push({
          ids: [p[0], p[1]],
          testPart: p[0] + '+' + p[1],
          name: tableName(a.label) + ' + ' + tableName(b.label),
          capacity: a.capacity + b.capacity,
          capText: 'Combined, up to ' + guestsText(a.capacity + b.capacity)
        });
      });
      return rows;
    }

    function slotOffers(slot, ids) {
      var i, o;
      if (ids.length === 1) {
        return (slot.available_table_ids || []).indexOf(ids[0]) !== -1;
      }
      for (i = 0; i < (slot.available_options || []).length; i++) {
        o = slot.available_options[i];
        if (o && o.table_ids && o.table_ids.length === 2 &&
            o.table_ids[0] === ids[0] && o.table_ids[1] === ids[1]) return true;
      }
      return false;
    }

    function isSelected(row, hhmm) {
      var s = state.sel;
      return !!s && hhmm === s.hhmm && s.tableIds.length === row.ids.length &&
             s.tableIds.every(function (id, i) { return id === row.ids[i]; });
    }

    function renderGrid() {
      var rest = state.rest, slots = state.slots, rows = rowsFor(rest);
      var times = slots.map(function (s) { return hhmmOf(s.starts_at_local); });
      var anyFree = false;

      var headCells = [h('th', { scope: 'col', class: 'corner', text: 'Table' })];
      times.forEach(function (t) { headCells.push(h('th', { scope: 'col', text: t })); });

      var body = rows.map(function (row) {
        var cells = [h('th', { scope: 'row' }, [
          h('span', { class: 't-name', text: row.name }),
          h('span', { class: 't-cap', text: row.capText })
        ])];
        slots.forEach(function (slot, i) {
          var free = slotOffers(slot, row.ids);
          var hhmm = times[i];
          var chosen = isSelected(row, hhmm);
          if (free) anyFree = true;
          var testid = 'slot-' + row.testPart + '-' + hhmm;
          var context = ', ' + row.name + ', ' + row.capText.toLowerCase();
          var cell;
          if (free) {
            var kids = [];
            if (chosen) kids.push(h('span', { class: 'tick', html: ICON.check }));
            kids.push(h('span', { class: 't', text: hhmm }));
            cell = h('button', {
              type: 'button', class: 'cell',
              'data-testid': testid, 'data-available': 'true',
              'aria-pressed': chosen ? 'true' : 'false',
              'aria-label': hhmm + context + ' — ' + (chosen ? 'your choice' : 'available')
            }, kids);
            cell.addEventListener('click', function () { onCellClick(row, slot); });
          } else if (chosen) {
            /* no longer free, because the person holding it is this person: their own booking,
               refreshed in after it was confirmed. Reads as theirs, not as somebody else's. */
            cell = h('span', {
              class: 'cell cell-yours',
              'data-testid': testid, 'data-available': 'false'
            }, [
              h('span', { class: 'tick', html: ICON.check }),
              h('span', { class: 't', text: hhmm }),
              h('span', { class: 'sr-only', text: context + ' — your table, held' })
            ]);
          } else {
            /* A taken slot is not an actionable control, so it is not a button and carries no
               aria-disabled: clicking it genuinely does nothing, and nothing has to block to
               make that true. Its state is real text, not only the hatch and the strike. */
            cell = h('span', {
              class: 'cell',
              'data-testid': testid, 'data-available': 'false'
            }, [
              h('span', { class: 't', text: hhmm }),
              h('span', { class: 'sr-only', text: context + ' — already taken' })
            ]);
          }
          cells.push(h('td', null, cell));
        });
        return h('tr', null, cells);
      });

      var table = h('table', { class: 'grid', 'data-testid': 'availability-grid' }, [
        h('caption', { class: 'sr-only', text:
          'Times for ' + rest.name + ' on ' + fmtDateLong(state.search.date) +
          ', ' + guestsText(state.search.partySize) + '. Times are in ' + rest.timezone + '.' },
        null),
        h('thead', null, h('tr', null, headCells)),
        h('tbody', null, body)
      ]);

      var out = [];
      if (!anyFree) {
        out.push(notice('warning', null, 'Every table is taken that night.', [
          h('p', { text: 'Each time below is already held by someone else. Try another night, or a ' +
                         'smaller party if some of you can come another time.' })
        ]));
      }
      out.push(h('div', { class: 'grid-scroll', tabindex: '0', role: 'region',
                          'aria-label': 'Availability grid, scrolls sideways' }, table));
      return out;
    }

    function renderInvitation() {
      return h('div', { class: 'idle-note' }, [
        h('p', { html: ICON.search }),
        h('p', { text: 'Pick a restaurant, a night and how many of you there are, then press Find tables.' })
      ]);
    }

    function renderSkeleton() {
      var rows = [], i;
      for (i = 0; i < 5; i++) {
        rows.push(h('div', { class: 'sk-row' }, [
          h('div', { class: 'sk sk-label' }),
          h('div', { class: 'sk sk-cells' })
        ]));
      }
      return h('div', { class: 'sk-rows', role: 'status', 'aria-live': 'polite' }, [
        h('span', { class: 'sr-only', text: 'Looking for tables…' })
      ].concat(rows));
    }

    function renderNoSlots() {
      var rest = state.rest, date = state.search.date;
      var code = WEEKDAY_CODE[ymdWeekdayIndex(date)];
      var openThatDay = (rest.opening_hours || []).some(function (o) {
        return String(o.weekday || '').toLowerCase() === code;
      });
      var title = openThatDay ? 'No times left that night.'
                              : rest.name + ' is closed on ' + fmtDateLong(date) + '.';
      var line = openThatDay
        ? 'The kitchen is open but there is no sitting that still finishes before closing. Try an earlier night.'
        : 'Pick one of the open days above — they are listed beside the search.';
      return h('div', { class: 'panel-empty', 'data-testid': 'no-slots' }, [
        h('img', { src: '/static/assets/illustrations/undraw_no-data_ig65.svg',
                   alt: 'An empty board with nothing pinned to it', width: '200', height: '160' }),
        h('h2', { text: title }),
        h('p', { text: line })
      ]);
    }

    /* ---- the review panel ------------------------------------------------------------- */

    function holdNote(rest) {
      var bits = [];
      if (rest.reservation_duration_minutes) {
        bits.push('The table is held for ' + minutesText(rest.reservation_duration_minutes) + '.');
      }
      if (rest.cancellation_cutoff_minutes) {
        bits.push('You can change or cancel from Reservation lookup until ' +
                  minutesText(rest.cancellation_cutoff_minutes) + ' before.');
      }
      return bits.join(' ');
    }

    function bookingBody() {
      return {
        restaurant_id: state.search.restaurantId,
        table_ids: state.sel.tableIds.slice(),
        starts_at_local: state.sel.startsAtLocal,
        party_size: bookingPartySize()
      };
    }

    var bookingPartyOverride = null;
    function bookingPartySize() {
      if (bookingPartyOverride !== null && bookingPartyOverride >= 1) return bookingPartyOverride;
      return state.search.partySize;
    }

    function renderReview() {
      clear(reviewBody);

      if (!state.search || !state.sel) {
        reviewBody.appendChild(h('div', { class: 'idle-note' }, [
          h('p', { text: 'Select an available time to review your reservation here.' })
        ]));
        if (!state.search) {
          reviewBody.appendChild(h('p', { class: 'hold-note', text:
            'Nothing is booked until you press Confirm booking.' }));
        }
        return;
      }

      var rest = state.rest, sel = state.sel;
      var labels = labelsFor(rest, sel.tableIds);

      /* success block first, per the brief: illustration, reference in large type, next steps */
      if (state.confirmed) {
        var c = state.confirmed;
        var cLabels = labelsFor(rest, c.table_ids || []);
        reviewBody.appendChild(h('div', { class: 'confirmed rise', 'data-testid': 'confirmation' }, [
          h('div', { class: 'confirmed-art' }, h('img', {
            src: '/static/assets/illustrations/undraw_confirmation_31jc.svg',
            alt: '', width: '180', height: '150' })),
          h('p', { class: 'ok' }, [h('span', { html: ICON.checkCircle }), 'Your table is held']),
          h('p', { class: 'eyebrow', text: 'Your reference' }),
          h('p', { class: 'ref', 'data-testid': 'confirmation-reference', text: c.reference }),
          /* One visible line carries the restaurant, every table label and the local start time.
             confirmation-tables wraps the table labels inside it, so both hooks are real,
             visible text and nothing is said twice. */
          h('p', { class: 'confirmed-details', 'data-testid': 'confirmation-details' }, [
            rest.name + ' · ',
            h('strong', { 'data-testid': 'confirmation-tables', text: tablesPhrase(cLabels) }),
            ' · ' + fmtDateLong(dateOf(c.starts_at_local)) + ' at ' + hhmmOf(c.starts_at_local) +
            ' · ' + guestsText(c.party_size)
          ]),
          h('p', { class: 'hold-note', text: 'Times are in ' + rest.timezone + '.' }),
          h('div', { class: 'next-up' }, [
            h('p', { text: 'Keep the reference. Reservation lookup will show this booking, and let ' +
                           'you change the time or cancel it.' }),
            h('p', { text: 'There is nothing to print and nothing to pay now.' })
          ])
        ]));
      }

      /* the booking form stays on screen after success, per the spec */
      var formWrap = h('div', state.confirmed ? { class: 'after-rule' } : null, null);
      if (state.confirmed) {
        formWrap.appendChild(h('p', { class: 'eyebrow', text: 'Booking details' }));
      }

      var partyInput = h('input', {
        class: 'input', id: 'booking-party', 'data-testid': 'booking-party-size',
        type: 'number', min: '1', max: '99', step: '1', inputmode: 'numeric',
        value: String(bookingPartySize())
      });
      /* Typing in the party field must NOT re-render the panel: that would replace the input
         under the caret and throw focus away mid-edit. Only the two notes depend on the value,
         so they are updated in place. The booking identity is recomputed at submit time, which
         is what makes a changed field a new booking request. */
      var updateNotes = null;
      partyInput.addEventListener('input', function () {
        var v = parseInt(partyInput.value, 10);
        bookingPartyOverride = isNaN(v) ? null : v;
        if (updateNotes) updateNotes();
      });

      var dl = h('dl', { class: 'kv', 'data-testid': 'booking-summary' }, [
        h('div', { class: 'kv-row' }, [h('dt', { text: 'Restaurant' }), h('dd', { text: rest.name })]),
        h('div', { class: 'kv-row' }, [h('dt', { text: 'Date' }),
          h('dd', { text: fmtDateShort(dateOf(sel.startsAtLocal)) })]),
        h('div', { class: 'kv-row' }, [h('dt', { text: 'Time' }),
          h('dd', { text: sel.hhmm + ' ' + rest.timezone })]),
        h('div', { class: 'kv-row' }, [h('dt', { text: labels.length > 1 ? 'Tables' : 'Table' }),
          h('dd', { text: tablesPhrase(labels) })]),
        h('div', { class: 'kv-row' }, [
          h('dt', null, h('label', { for: 'booking-party', text: 'Party size' })),
          h('dd', null, h('div', { class: 'party-cell' }, [partyInput, h('span', { text: 'guests' })]))
        ])
      ]);

      var formKids = [dl];

      if (state.notice) {
        formKids.push(notice(
          state.notice.kind === 'uncertain' ? 'warning' : 'danger',
          state.notice.kind === 'uncertain' ? 'booking-uncertain' : 'booking-error',
          state.notice.title,
          state.notice.lines.map(function (t) { return h('p', { text: t }); })
        ));
      }

      if (!session) {
        /* the signed-out reference screenshot: auth-error beside the review panel, the chosen
           slot named in the copy so it is visibly kept, Sign in and Create account right here,
           and no Confirm button */
        formKids.push(notice('auth', 'auth-error', 'Sign in to hold this table.', [
          h('p', { text: 'Your choice, ' + tablesPhrase(labels) + ' at ' + sel.hhmm +
                         ', is kept while you sign in.' })
        ], h('div', { class: 'n-actions' }, [
          h('a', { class: 'btn btn-primary btn-block', href: signInHref(), text: 'Sign in' }),
          h('a', { class: 'btn btn-secondary btn-block', href: signUpHref(), text: 'Create account' })
        ])));
      } else {
        var capNote = h('p', { class: 'hold-note', role: 'status' });
        var footNote = h('p', { class: 'hold-note' });
        updateNotes = function () {
          var tooBig = bookingPartySize() > sel.capacity;
          capNote.textContent = tooBig
            ? tablesPhrase(labels) + ' seats up to ' + guestsText(sel.capacity) +
              '. Lower the party size, or pick a bigger table above.'
            : '';
          show(capNote, tooBig);
          footNote.textContent = !state.confirmed
            ? holdNote(rest)
            : (sameAsConfirmed()
                ? 'Pressing Confirm booking again will not book a second table.'
                : 'You have changed the details, so Confirm booking will make a second, ' +
                  'separate booking. The one above stays held.');
        };

        var submit = h('button', {
          class: 'btn btn-primary btn-block', type: 'button', 'data-testid': 'booking-submit',
          text: state.submitting ? 'Holding your table…' : 'Confirm booking',
          /* aria-busy, not aria-disabled: a second press while one is in flight is ignored by
             submitBooking's own guard, and the button must never block an automated click */
          'aria-busy': state.submitting ? 'true' : null
        });
        submit.addEventListener('click', submitBooking);

        formKids.push(capNote);
        formKids.push(submit);
        formKids.push(footNote);
        updateNotes();
      }

      formWrap.appendChild(h('div', { 'data-testid': 'booking-form' }, formKids));
      reviewBody.appendChild(formWrap);
    }

    function sameAsConfirmed() {
      if (!state.confirmedBody || !state.sel) return false;
      return canonBody(state.confirmedBody) === canonBody(bookingBody());
    }

    /* ---- actions ---------------------------------------------------------------------- */

    function onCellClick(row, slot) {
      state.sel = {
        tableIds: row.ids.slice(),
        startsAtLocal: slot.starts_at_local,
        hhmm: hhmmOf(slot.starts_at_local),
        capacity: row.capacity
      };
      bookingPartyOverride = null;
      state.notice = null;
      paintResults();
      renderReview();
    }

    function submitBooking() {
      if (state.submitting || !session || !state.sel) return;
      var body = bookingBody();
      var key = keyFor(body);
      state.submitting = true;
      renderReview();

      api('POST', '/reservations', {
        body: body, token: session.token, idempotencyKey: key, timeoutMs: LOST_RESPONSE_MS
      }).then(function (r) {
        state.submitting = false;
        if (r.ok && r.data && r.data.reference) {
          /* a definite success — including the replay of a booking whose response was lost */
          state.notice = null;
          state.confirmed = r.data;
          state.confirmedBody = body;
          renderReview();
          /* The slot just taken is no longer free, so the grid beside it would be lying. Refresh
             it once (not a poll), keeping the selection, the inputs and the pending booking
             identity intact so an unchanged resubmit still replays rather than rebooks. */
          runSearch({ keepSelection: true, keepNotice: true, silent: true });
          return;
        }
        if (r.status === 401) {
          saveSession(null);
          renderTopBar();
          state.notice = null;
          renderReview();
          return;
        }
        /* the server answered and refused: a definite rejection, so booking-error */
        var code = errCode(r);
        state.notice = { kind: 'error', title: refusalTitle(code), lines: refusalLines(code, errMessage(r)) };
        renderReview();
        if (code === 'table_unavailable') {
          /* refresh availability but keep the selection, the inputs and this error in place */
          runSearch({ keepSelection: true, keepNotice: true, silent: true });
        }
      }, function (err) {
        state.submitting = false;
        if (!err || !err.lost) {
          state.notice = { kind: 'error', title: 'We could not send that.',
                           lines: ['Something went wrong before the request left the page. Press Confirm booking to try again.'] };
          renderReview();
          return;
        }
        /* no answer arrived. The booking may or may not have committed, so: uncertain, never
           an error, never a confirmation. The key and body are untouched, so pressing Confirm
           booking again is the same request and the server will tell us what really happened. */
        state.notice = {
          kind: 'uncertain',
          title: 'We did not hear back.',
          lines: [
            'Your table may or may not be held — we cannot tell from here, and we will not ' +
            'guess. Nothing has been booked twice.',
            'Press Confirm booking again. We will ask about this exact booking, not a new one, ' +
            'and show you the answer.'
          ]
        };
        renderReview();
      });
    }

    function refusalTitle(code) {
      if (code === 'table_unavailable') return 'That table has just gone.';
      if (code === 'party_exceeds_capacity') return 'That table is too small for your party.';
      if (code === 'combination_not_allowed') return 'Those tables cannot be put together.';
      if (code === 'outside_opening_hours') return 'The restaurant is closed then.';
      if (code === 'not_on_slot_grid') return 'That is not one of the sitting times.';
      if (code === 'invalid_local_time') return 'That time does not exist that night.';
      if (code === 'validation_failed') return 'Something in the booking does not add up.';
      return 'We could not hold that table.';
    }
    function refusalLines(code, message) {
      if (code === 'table_unavailable') {
        return ['Someone else took it a moment ago. We have refreshed the times below — your ' +
                'details are still here, just pick another one.'];
      }
      if (code === 'party_exceeds_capacity') {
        return ['Lower the party size, or choose a bigger table or a combined pair above.'];
      }
      if (code === 'combination_not_allowed') {
        return ['The restaurant only joins certain pairs of tables. Pick one of the combined rows above.'];
      }
      return [message || 'Nothing has been booked. Change a detail above and try again.'];
    }

    /* ---- painting --------------------------------------------------------------------- */

    function paintResults() {
      clear(resultsBody);
      if (state.loading) {
        show(legend, false);
        resultsBody.appendChild(renderSkeleton());
        return;
      }
      if (!state.search || !state.slots) {
        show(legend, false);
        resultsTitle.textContent = 'Choose a time';
        resultsSub.textContent = '';
        resultsBody.appendChild(renderInvitation());
        return;
      }
      resultsTitle.textContent = fmtDateLong(state.search.date);
      resultsSub.textContent = state.rest.name + ' · ' + guestsText(state.search.partySize) +
                               ' · pick a time to hold a table';
      if (!state.slots.length) {
        show(legend, false);
        resultsBody.appendChild(renderNoSlots());
        return;
      }
      show(legend, true);
      renderGrid().forEach(function (n) { resultsBody.appendChild(n); });
    }

    /* ---- search ----------------------------------------------------------------------- */

    function readForm() {
      var party = parseInt(inParty.value, 10);
      return {
        restaurantId: selRestaurant.value,
        date: inDate.value,
        partySize: isNaN(party) || party < 1 ? 1 : party
      };
    }

    function runSearch(opts) {
      opts = opts || {};
      var want = opts.params || readForm();
      if (!want.restaurantId || !ymdParts(want.date)) return;

      var seq = ++searchSeq;
      if (!opts.silent) {
        state.loading = true;
        paintResults();
      }

      /* The restaurant detail and the availability belong to the same search: the grid, the
         table labels, the timezone and the opening hours must all describe the same request. */
      Promise.all([
        api('GET', '/restaurants/' + encodeURIComponent(want.restaurantId)),
        api('GET', '/availability?restaurant_id=' + encodeURIComponent(want.restaurantId) +
                   '&date=' + encodeURIComponent(want.date) +
                   '&party_size=' + encodeURIComponent(String(want.partySize)))
      ]).then(function (rs) {
        if (seq !== searchSeq) return;          /* a newer search has been issued: discard */
        var rest = rs[0], avail = rs[1];
        state.loading = false;

        if (!rest.ok || !rest.data) {
          state.rest = null; state.slots = null; state.search = null;
          clear(resultsBody);
          resultsBody.appendChild(notice('danger', null, 'We could not load that restaurant.',
            [h('p', { text: 'Pick a restaurant from the list and press Find tables again.' })]));
          renderReview();
          return;
        }
        if (!avail.ok || !avail.data) {
          state.slots = null;
          state.rest = rest.data;
          state.search = want;
          renderHours(rest.data, want.date);
          clear(resultsBody);
          resultsBody.appendChild(notice('danger', null, 'We could not load the times.',
            [h('p', { text: errMessage(avail) || 'Press Find tables to try again.' })]));
          renderReview();
          return;
        }

        state.rest = rest.data;
        state.search = want;
        state.slots = avail.data.slots || [];
        if (!opts.keepSelection) { state.sel = null; bookingPartyOverride = null; }
        if (!opts.keepNotice) state.notice = null;

        renderHours(rest.data, want.date);
        show(work, true);

        if (state.restoring) {
          var wanted = state.restoring;
          state.restoring = null;
          restoreSlot(wanted);
        }
        paintResults();
        renderReview();
      }, function () {
        if (seq !== searchSeq) return;
        state.loading = false;
        if (opts.silent) return;
        clear(resultsBody);
        resultsBody.appendChild(notice('warning', null, 'We could not reach the restaurant list.',
          [h('p', { text: 'Nothing has changed. Press Find tables to try again.' })]));
      });
    }

    function restoreSlot(wanted) {
      var rows = rowsFor(state.rest), i, j, row, slot;
      for (i = 0; i < rows.length; i++) {
        row = rows[i];
        if (row.ids.join('+') !== wanted.tableIds.join('+')) continue;
        for (j = 0; j < state.slots.length; j++) {
          slot = state.slots[j];
          if (slot.starts_at_local !== wanted.startsAtLocal) continue;
          if (!slotOffers(slot, row.ids)) return;     /* gone while they were signing in */
          state.sel = {
            tableIds: row.ids.slice(), startsAtLocal: slot.starts_at_local,
            hhmm: hhmmOf(slot.starts_at_local), capacity: row.capacity
          };
          return;
        }
      }
    }

    /* ---- boot ------------------------------------------------------------------------- */

    function loadRestaurants() {
      return api('GET', '/restaurants').then(function (r) {
        if (!r.ok || !r.data) { state.restaurants = []; return; }
        state.restaurants = r.data.restaurants || [];
      }, function () { state.restaurants = []; });
    }

    function fillRestaurantSelect(preferId) {
      clear(selRestaurant);
      state.restaurants.forEach(function (rr) {
        selRestaurant.appendChild(h('option', { value: rr.id, text: rr.name }));
      });
      if (preferId) {
        var found = state.restaurants.some(function (rr) { return rr.id === preferId; });
        if (found) selRestaurant.value = preferId;
      }
    }

    function start() {
      show(screen, true);

      var wantR = params.get('r'), wantD = params.get('d'), wantP = params.get('p');
      var wantS = params.get('s'), wantT = params.get('t');

      inDate.value = ymdParts(wantD) ? wantD : todayYMD();
      if (wantP && parseInt(wantP, 10) >= 1) inParty.value = String(parseInt(wantP, 10));

      form.addEventListener('submit', function (e) {
        e.preventDefault();
        state.confirmed = null;
        state.confirmedBody = null;
        pending = null;
        runSearch({});
      });

      loadRestaurants().then(function () {
        if (!state.restaurants.length) {
          /* first run: nothing is set up yet. The hero stays, because it is what explains the
             product; the search and results go, because there is nothing to search. Name the
             file that has the instructions. Never invent a restaurant. */
          show(homeTop, true);
          show(form, false);
          show(work, false);
          show(firstRun, true);
          return;
        }
        show(firstRun, false);
        fillRestaurantSelect(wantR);
        show(work, true);
        paintResults();
        renderReview();

        if (wantS && wantT) {
          state.restoring = { startsAtLocal: wantS, tableIds: wantT.split('+').filter(Boolean) };
          runSearch({});
        }
      });
    }

    return {
      state: state,
      start: start,
      onSignedOut: function () { renderReview(); }
    };
  })();

  /* =============================================================== login / signup */

  function nextHref() {
    var n = params.get('next');
    if (!n) return '/';
    /* only ever follow a path on this origin */
    return (n.charAt(0) === '/' && n.charAt(1) !== '/') ? n : '/';
  }

  function keepNextOnLinks() {
    var n = params.get('next');
    if (!n) return;
    qa('[data-keep-next]').forEach(function (a) {
      a.setAttribute('href', a.getAttribute('href') + '?next=' + encodeURIComponent(n));
    });
  }

  function authScreen(kind) {
    var screen = q('[data-screen="' + kind + '"]');
    show(screen, true);
    keepNextOnLinks();

    var form = q(kind === 'login' ? '[data-login-form]' : '[data-signup-form]', screen);
    var errBox = q(kind === 'login' ? '[data-login-error]' : '[data-signup-error]', screen);
    var submit = q('[data-testid="' + kind + '-submit"]', screen);
    var busy = false;

    function setError(title, lines) {
      clear(errBox);
      if (!title) return;                      /* auth-error exists only when there is one */
      errBox.appendChild(notice('danger', 'auth-error', title,
        (lines || []).map(function (t) { return h('p', { text: t }); })));
    }

    form.addEventListener('submit', function (e) {
      e.preventDefault();
      if (busy) return;
      setError(null);

      var email = q('[data-testid="' + kind + '-email"]', screen).value.trim();
      var password = q('[data-testid="' + kind + '-password"]', screen).value;
      var displayName = kind === 'signup'
        ? q('[data-testid="signup-display-name"]', screen).value.trim() : null;

      if (!email) { setError('We need your email address.', ['Enter the email you booked with.']); return; }
      if (!password) { setError('We need your password.', ['Enter your password to continue.']); return; }
      if (kind === 'signup' && !displayName) {
        setError('We need a name for the booking.', ['The restaurant shows this name on the night.']);
        return;
      }
      if (kind === 'signup' && password.length < 8) {
        setError('That password is too short.', ['Use at least 8 characters.']);
        return;
      }

      busy = true;
      submit.setAttribute('aria-busy', 'true');   /* never aria-disabled: see the grid cells */
      submit.textContent = kind === 'login' ? 'Signing you in…' : 'Creating your account…';

      var body = kind === 'login'
        ? { email: email, password: password }
        : { email: email, password: password, display_name: displayName };

      api('POST', kind === 'login' ? '/auth/login' : '/auth/signup', { body: body })
        .then(function (r) {
          if (r.ok && r.data && r.data.token) {
            saveSession({
              token: r.data.token,
              user_id: r.data.user_id,
              display_name: r.data.display_name,
              email: email
            });
            window.location.href = nextHref();
            return;
          }
          busy = false;
          submit.removeAttribute('aria-busy');
          submit.textContent = kind === 'login' ? 'Sign in' : 'Create account';
          var code = errCode(r);
          if (kind === 'login') {
            setError('That email and password do not match.',
              ['Check them and try again. If you have not booked with us before, create an account.']);
          } else if (code === 'email_taken') {
            setError('That email already has an account.', ['Sign in instead, and your chosen time is kept.']);
          } else {
            setError('We could not create that account.',
              [errMessage(r) || 'Check the three fields above and try again.']);
          }
        }, function () {
          busy = false;
          submit.removeAttribute('aria-busy');
          submit.textContent = kind === 'login' ? 'Sign in' : 'Create account';
          setError('We did not hear back.', ['Nothing has changed. Press the button to try again.']);
        });
    });
  }

  /* =============================================================== lookup */

  var lookup = (function () {
    var screen = q('[data-screen="lookup"]');
    var form, input, submit, body;
    var current = null;     /* {resv, rest} */
    var busy = false;

    function setBody(nodes) {
      clear(body);
      (Array.isArray(nodes) ? nodes : [nodes]).forEach(function (n) { if (n) body.appendChild(n); });
    }

    function errorCard(title, lines) {
      return h('div', { class: 'card detail-card' },
        notice('danger', 'reservation-error', title,
          (lines || []).map(function (t) { return h('p', { text: t }); })));
    }

    function render() {
      if (!current) return;
      var resv = current.resv, rest = current.rest;
      var labels = (resv.table_ids || []).map(function (id) {
        var i, t;
        for (i = 0; i < ((rest && rest.tables) || []).length; i++) {
          t = rest.tables[i];
          if (t.id === id) return tableName(t.label);
        }
        return id;
      });
      var cancelled = resv.status === 'cancelled';

      var kids = [
        h('div', { class: 'detail-top' }, [
          h('div', null, [
            h('p', { class: 'eyebrow', text: 'Booking reference' }),
            h('p', { class: 'detail-ref', text: resv.reference })
          ]),
          h('span', { class: 'pill', 'data-status': resv.status }, [
            h('span', { html: cancelled ? ICON.crossCircle : ICON.checkCircle }),
            h('span', { class: 's', 'data-testid': 'reservation-status', text: resv.status })
          ])
        ]),
        h('dl', { class: 'kv' }, [
          h('div', { class: 'kv-row' }, [h('dt', { text: 'Restaurant' }),
            h('dd', { text: (rest && rest.name) || resv.restaurant_id })]),
          h('div', { class: 'kv-row' }, [h('dt', { text: 'Date' }),
            h('dd', { text: fmtDateShort(dateOf(resv.starts_at_local)) })]),
          h('div', { class: 'kv-row' }, [h('dt', { text: 'Time' }),
            h('dd', { text: hhmmOf(resv.starts_at_local) + (rest ? ' ' + rest.timezone : '') })]),
          h('div', { class: 'kv-row' }, [h('dt', { text: labels.length > 1 ? 'Tables' : 'Table' }),
            h('dd', { 'data-testid': 'reservation-tables', text: tablesPhrase(labels) })]),
          h('div', { class: 'kv-row' }, [h('dt', { text: 'Party' }),
            h('dd', { text: guestsText(resv.party_size) })])
        ])
      ];

      if (cancelled) {
        kids.push(h('p', { class: 'hold-note', text:
          'This booking is cancelled and the table has been let go. Find a table to book another night.' }));
        kids.push(h('a', { class: 'btn btn-secondary', href: '/', text: 'Find a table' }));
      } else {
        kids.push(h('p', { class: 'hold-note', text:
          'Changed your mind? Cancelling frees the table' +
          (labels.length > 1 ? 's' : '') + ' straight away.' }));
        /* the destructive action is the quieter of the two, and second */
        var cancel = h('button', { class: 'btn btn-quiet-danger', type: 'button',
                                   'data-testid': 'reservation-cancel-button', text: 'Cancel reservation' });
        cancel.addEventListener('click', doCancel);
        kids.push(h('div', { class: 'detail-actions' }, [
          h('a', { class: 'btn btn-secondary', href:
            '/?r=' + encodeURIComponent(resv.restaurant_id) +
            '&d=' + encodeURIComponent(dateOf(resv.starts_at_local)) +
            '&p=' + encodeURIComponent(String(resv.party_size)),
            text: 'Change the time' }),
          cancel
        ]));
      }

      var nodes = [h('div', { class: 'card detail-card', 'data-testid': 'reservation-detail' }, kids)];
      if (current.notice) nodes.push(current.notice);
      setBody(nodes);
    }

    function doCancel() {
      if (!current || busy || !session) return;
      busy = true;
      api('POST', '/reservations/' + encodeURIComponent(current.resv.reference) + '/cancel',
          { token: session.token, timeoutMs: LOST_RESPONSE_MS })
        .then(function (r) {
          busy = false;
          if (r.ok && r.data) { current.resv = r.data; current.notice = null; render(); return; }
          var code = errCode(r);
          var title = code === 'cutoff_passed' ? 'It is too late to cancel this one.'
                    : code === 'reservation_cancelled' ? 'That booking is already cancelled.'
                    : 'We could not cancel that booking.';
          var line = code === 'cutoff_passed'
            ? 'The restaurant stops cancellations shortly before the sitting. Please call them instead.'
            : (errMessage(r) || 'Nothing has changed. Try again in a moment.');
          current.notice = notice('danger', 'reservation-error', title, [h('p', { text: line })]);
          render();
        }, function () {
          busy = false;
          current.notice = notice('warning', null, 'We did not hear back.',
            [h('p', { text: 'We cannot tell whether the cancellation went through. ' +
                            'Press Find reservation above to see where it stands.' })]);
          render();
        });
    }

    function doLookup(ref) {
      current = null;
      setBody(h('div', { class: 'card detail-card' }, [
        h('div', { class: 'sk', style: 'height:28px;width:60%' }),
        h('div', { class: 'sk', style: 'height:88px' })
      ]));

      api('GET', '/reservations/' + encodeURIComponent(ref), { token: session ? session.token : null })
        .then(function (r) {
          if (r.ok && r.data && r.data.reference) {
            var resv = r.data;
            api('GET', '/restaurants/' + encodeURIComponent(resv.restaurant_id)).then(function (rr) {
              current = { resv: resv, rest: (rr.ok && rr.data) ? rr.data : null, notice: null };
              render();
            }, function () {
              current = { resv: resv, rest: null, notice: null };
              render();
            });
            return;
          }
          if (!session) {
            setBody(errorCard('We could not find that reservation.', [
              'Bookings are only visible to the account that made them. Sign in with that account and look it up again.'
            ]));
            return;
          }
          setBody(errorCard('We could not find that reservation.', [
            'Check the reference against your confirmation — it is case sensitive. If you booked ' +
            'with a different account, sign in with that one and try again.'
          ]));
        }, function () {
          setBody(h('div', { class: 'card detail-card' },
            notice('warning', null, 'We did not hear back.',
              [h('p', { text: 'Nothing has changed. Press Find reservation to try again.' })])));
        });
    }

    function start() {
      show(screen, true);
      form = q('[data-lookup-form]', screen);
      input = q('[data-testid="lookup-reference-input"]', screen);
      submit = q('[data-testid="lookup-submit"]', screen);
      body = q('[data-lookup-body]', screen);

      form.addEventListener('submit', function (e) {
        e.preventDefault();
        var ref = input.value.trim();
        if (!ref) {
          setBody(errorCard('We need a reference.',
            ['It is the short code on your confirmation.']));
          return;
        }
        doLookup(ref);
      });

      var pre = params.get('ref');
      if (pre) { input.value = pre; doLookup(pre); }
    }

    return { start: start, reset: function () { current = null; if (body) clear(body); } };
  })();

  /* =============================================================== boot */

  renderTopBar();

  if (route === '/') home.start();
  else if (route === '/login') authScreen('login');
  else if (route === '/signup') authScreen('signup');
  else if (route === '/lookup') lookup.start();

})();
