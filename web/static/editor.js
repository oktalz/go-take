/* gotake editor — talks to the Go server (`gotake edit`) which applies all edits. */
(function () {
  'use strict';

  var $ = function (id) { return document.getElementById(id); };
  var ROW = 24;

  var S = null;            // server state
  var times = [];          // absolute time of each event
  var keys = [];           // keystroke flag per event
  var sel = null;          // {a: anchor, b: focus} event indices (inclusive)
  var cur = -1;            // index of the last event shown by the preview
  var player = null;
  var stopAt = null;       // stop preview at this time (play selection)
  var view = { start: 0, end: 1 }; // timeline viewport in seconds
  var viewFit = true;
  var found = { events: [], set: new Set(), matches: 0 }; // find results

  // ------------------------------------------------------------ helpers

  function api(path, body) {
    var opts = body === undefined ? {} : { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) };
    return fetch(path, opts).then(function (r) {
      return r.json().then(function (j) {
        if (!r.ok) throw new Error(j.error || r.statusText);
        return j;
      });
    });
  }

  function num(id) { return parseFloat($(id).value); }

  function fmt(t) {
    var m = Math.floor(t / 60), s = t - m * 60;
    return m + ':' + (s < 10 ? '0' : '') + s.toFixed(3);
  }

  function fmtDelay(d) { return d.toFixed(d >= 10 ? 2 : 3); }

  var htmlEsc = { '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;' };
  function escHTML(s) { return s.replace(/[&<>"]/g, function (c) { return htmlEsc[c]; }); }

  // Render event data with visible control characters.
  function showData(s) {
    if (s.length > 400) s = s.slice(0, 400) + '…';
    var out = '';
    for (var i = 0; i < s.length; i++) {
      var c = s[i], code = s.charCodeAt(i);
      if (c === '\x1b') out += '<span class="esc">␛</span>';
      else if (c === '\r') out += '<span class="esc">\\r</span>';
      else if (c === '\n') out += '<span class="esc">\\n</span>';
      else if (c === '\t') out += '<span class="esc">\\t</span>';
      else if (code < 32 || code === 127) out += '<span class="esc">^' + String.fromCharCode(code ^ 64) + '</span>';
      else out += htmlEsc[c] || c;
    }
    return out;
  }

  // Escape data for editing in a text field; unescape() reverses it.
  function escapeData(s) {
    return s.replace(/[\\\x00-\x1f\x7f]/g, function (c) {
      switch (c) {
        case '\\': return '\\\\';
        case '\r': return '\\r';
        case '\n': return '\\n';
        case '\t': return '\\t';
        case '\x1b': return '\\e';
      }
      return '\\x' + c.charCodeAt(0).toString(16).padStart(2, '0');
    });
  }

  function unescapeData(s) {
    s = s.replace(/\r?\n/g, '\r\n'); // literal line breaks become terminal line breaks
    return s.replace(/\\(u[0-9a-fA-F]{4}|x[0-9a-fA-F]{2}|.)/g, function (m, e) {
      switch (e[0]) {
        case 'n': return '\n';
        case 'r': return '\r';
        case 't': return '\t';
        case 'e': return '\x1b';
        case '\\': return '\\';
        case 'u': case 'x': return e.length > 1 ? String.fromCharCode(parseInt(e.slice(1), 16)) : m;
      }
      return m;
    });
  }

  var ansiRe = /\x1b(\[[0-?]*[ -/]*[@-~]|\][^\x07\x1b]*(\x07|\x1b\\)|[@-Z\\-_])/g;
  function isKeystroke(e) {
    if (e[1] === 'i') return true;
    if (e[1] !== 'o' || e[2].length > 64) return false;
    var v = e[2].replace(ansiRe, '');
    if (/[\r\n]/.test(v)) return false;
    var n = Array.from(v).length;
    return n >= 1 && n <= 2;
  }

  function range() {
    if (!sel) return null;
    return [Math.min(sel.a, sel.b), Math.max(sel.a, sel.b) + 1];
  }

  // time span covered by events [from, to): from the start of the first event's delay
  function span(from, to) {
    if (from >= to) return [0, 0];
    return [times[from] - S.events[from][0], times[to - 1]];
  }

  function indexAtTime(t) {
    // last event with time <= t
    var lo = 0, hi = times.length - 1, ans = -1;
    while (lo <= hi) {
      var mid = (lo + hi) >> 1;
      if (times[mid] <= t + 1e-9) { ans = mid; lo = mid + 1; } else hi = mid - 1;
    }
    return ans;
  }

  function flash(text, isErr) {
    var m = $('msg');
    if (!text) { m.hidden = true; return; }
    m.hidden = false;
    m.textContent = text;
    m.className = 'msg' + (isErr ? ' err' : '');
    clearTimeout(flash.t);
    flash.t = setTimeout(function () { m.hidden = true; }, isErr ? 6000 : 2500);
  }

  // ------------------------------------------------------------ state

  function setState(st, keepSel) {
    S = st;
    times = new Array(S.events.length);
    keys = new Array(S.events.length);
    var t = 0;
    for (var i = 0; i < S.events.length; i++) {
      t += S.events[i][0];
      times[i] = t;
      keys[i] = isKeystroke(S.events[i]);
    }
    var n = S.events.length;
    if (!keepSel || !n) sel = null;
    else if (sel) sel = { a: Math.min(sel.a, n - 1), b: Math.min(sel.b, n - 1) };
    if (viewFit) view = { start: 0, end: Math.max(S.duration, 1) };

    $('path').textContent = S.path;
    $('dirty').hidden = !S.dirty;
    $('btn-save').disabled = !S.dirty;
    $('btn-save').title = S.dirty ? 'Save (Ctrl+S)' : 'No changes to save';
    $('btn-undo').disabled = !S.can_undo;
    $('btn-redo').disabled = !S.can_redo;
    $('stats').textContent = S.events.length + ' events · ' + fmt(S.duration) + ' · ' + S.header.term.cols + '×' + S.header.term.rows;
    document.title = (S.dirty ? '● ' : '') + S.path.split('/').pop() + ' — gotake editor';
    fillHeader();
    if (S.message) flash(S.message);
    scheduleFind();

    var castObj = { header: S.header, events: S.events };
    if (!player) {
      player = new GoTakePlayer.Player($('preview'), { idleTimeLimit: 0, fit: 'width', fontSize: 14 });
      player.on('time', onPlayerTime);
      player.load(castObj).catch(function (e) { flash(e.message, true); });
    } else if (player.term) {
      player.setCast(GoTakePlayer.parseCast(castObj), true);
    }
    renderAll();
  }

  function fillHeader() {
    var h = S.header;
    var set = function (id, v) { if (document.activeElement !== $(id)) $(id).value = v; };
    set('h-title', h.title || '');
    set('h-cols', h.term.cols);
    set('h-rows', h.term.rows);
    set('h-idle', h.idle_time_limit || '');
    fillTheme();
  }

  // ------------------------------------------------------------ theme
  //
  // The theme lives in the header as {fg, bg, palette: "c0:c1:...:c15"}; bg may be
  // "transparent". The pickers show the recording's colors with defaults filled in.

  var THEME_NAMES = ['black', 'red', 'green', 'yellow', 'blue', 'magenta', 'cyan', 'white'];
  var THEME_PRESETS = {
    'gotake (default)': null,
    Dracula: ['#f8f8f2', '#282a36', '#21222c:#ff5555:#50fa7b:#f1fa8c:#bd93f9:#ff79c6:#8be9fd:#f8f8f2:' +
      '#6272a4:#ff6e6e:#69ff94:#ffffa5:#d6acff:#ff92df:#a4ffff:#ffffff'],
    'Solarized dark': ['#839496', '#002b36', '#073642:#dc322f:#859900:#b58900:#268bd2:#d33682:#2aa198:#eee8d5:' +
      '#586e75:#cb4b16:#859900:#b58900:#268bd2:#6c71c4:#2aa198:#fdf6e3'],
    Light: ['#24292f', '#ffffff', '#24292f:#cf222e:#116329:#9a6700:#0969da:#8250df:#1b7c83:#6e7781:' +
      '#57606a:#a40e26:#1a7f37:#7d4e00:#218bff:#a475f9:#3192aa:#8c959f']
  };

  // hex6 returns c as #rrggbb (what <input type=color> needs), or fallback.
  function hex6(c, fallback) {
    var s = String(c || '').trim().toLowerCase();
    if (/^#[0-9a-f]{6}$/.test(s)) return s;
    if (/^#[0-9a-f]{3}$/.test(s)) return '#' + s[1] + s[1] + s[2] + s[2] + s[3] + s[3];
    return fallback;
  }

  // currentTheme returns {fg, bg, transparent, palette: [16]} for the recording.
  function currentTheme() {
    var d = GoTakePlayer.defaultTheme;
    var t = (S.header.term && S.header.term.theme) || {};
    var pal = GoTakePlayer.paletteKeys.map(function (k) { return d[k]; });
    var given = t.palette ? t.palette.split(':') : [];
    given.forEach(function (c, i) {
      if (i < 16) pal[i] = hex6(c, pal[i]);
      if (given.length === 8) pal[i + 8] = hex6(c, pal[i + 8]); // 8-color palettes reuse them for bright
    });
    var transparent = String(t.bg).toLowerCase() === 'transparent';
    return {
      fg: hex6(t.fg, d.foreground), bg: transparent ? d.background : hex6(t.bg, d.background),
      transparent: transparent, palette: pal
    };
  }

  function themeFromInputs() {
    return {
      fg: $('th-fg').value, bg: $('th-bg').value, transparent: $('th-transparent').checked,
      palette: Array.prototype.map.call($('th-palette').querySelectorAll('input'), function (i) { return i.value; })
    };
  }

  function fillTheme() {
    var t = currentTheme();
    $('th-fg').value = t.fg;
    $('th-bg').value = t.bg;
    $('th-bg').disabled = t.transparent;
    $('th-transparent').checked = t.transparent;
    $('th-palette').querySelectorAll('input').forEach(function (inp, i) { inp.value = t.palette[i]; });
    $('th-reset').disabled = !(S.header.term && S.header.term.theme);
  }

  // previewTheme recolors the preview right away, without changing the recording.
  function previewTheme(t) {
    if (!player || !player.term) return;
    var bg = t.transparent ? '#00000000' : t.bg;
    var x = { background: bg, foreground: t.fg, cursor: t.fg };
    GoTakePlayer.paletteKeys.forEach(function (k, i) { x[k] = t.palette[i]; });
    player.term.options.theme = x;
    player.root.style.setProperty('--gotake-bg', bg);
  }

  // setTheme stores t in the header (null removes it, back to the default) as one undo step.
  function setTheme(t) {
    var h = JSON.parse(JSON.stringify(S.header));
    if (t) h.term.theme = { fg: t.fg, bg: t.transparent ? 'transparent' : t.bg, palette: t.palette.join(':') };
    else delete h.term.theme;
    run({ op: 'header', header: h });
  }

  function initTheme() {
    var pal = $('th-palette');
    for (var i = 0; i < 16; i++) {
      var inp = document.createElement('input');
      inp.type = 'color';
      inp.title = (i < 8 ? '' : 'bright ') + THEME_NAMES[i % 8];
      inp.setAttribute('aria-label', inp.title);
      pal.appendChild(inp);
    }
    var pickers = [$('th-fg'), $('th-bg')].concat(Array.prototype.slice.call(pal.querySelectorAll('input')));
    pickers.forEach(function (inp) {
      inp.addEventListener('input', function () { previewTheme(themeFromInputs()); });
      inp.addEventListener('change', function () { setTheme(themeFromInputs()); });
    });
    $('th-transparent').addEventListener('change', function () { setTheme(themeFromInputs()); });
    $('th-reset').onclick = function () { setTheme(null); };

    var sel = $('th-preset');
    sel.innerHTML = '<option value="">Choose…</option>' + Object.keys(THEME_PRESETS).map(function (n) {
      return '<option>' + n + '</option>';
    }).join('');
    sel.onchange = function () {
      var p = THEME_PRESETS[sel.value];
      if (sel.value === '') return;
      sel.value = '';
      if (!p) { setTheme(null); return; }
      setTheme({ fg: p[0], bg: p[1], transparent: $('th-transparent').checked, palette: p[2].split(':') });
    };
  }

  // ------------------------------------------------------------ operations

  function run(req, after) {
    var before = S.events.length;
    return api('api/op', req).then(function (st) {
      setState(st, true);
      if (after) after(S.events.length - before);
      renderAll();
    }).catch(function (e) { flash(e.message, true); });
  }

  function needSel() {
    var r = range();
    if (!r) flash('Select one or more events first (click rows or drag on the timeline).', true);
    return r;
  }

  function selOrAll() { return range() || [0, S.events.length]; }

  function selectRange(from, to) {
    if (to <= from || !S.events.length) { sel = null; return; }
    sel = { a: from, b: Math.min(to, S.events.length) - 1 };
  }

  var ops = {
    delete: function () {
      var r = needSel(); if (!r) return;
      run({ op: 'delete', from: r[0], to: r[1] }, function () {
        sel = r[0] < S.events.length ? { a: r[0], b: r[0] } : null;
      });
    },
    duplicate: function () {
      var r = needSel(); if (!r) return;
      run({ op: 'duplicate', from: r[0], to: r[1] }, function (n) { selectRange(r[1], r[1] + n); });
    },
    merge: function () {
      var r = needSel(); if (!r) return;
      run({ op: 'merge', from: r[0], to: r[1] }, function (n) { selectRange(r[0], r[1] + n); });
    },
    split: function () {
      var r = needSel(); if (!r) return;
      run({ op: 'split', from: r[0], to: r[1] }, function (n) { selectRange(r[0], r[1] + n); });
    },
    stretch: function () { var r = selOrAll(); run({ op: 'stretch', from: r[0], to: r[1], factor: num('in-factor') }); },
    stretch_to: function () { var r = selOrAll(); run({ op: 'stretch_to', from: r[0], to: r[1], seconds: num('in-length') }); },
    set_delay: function () { var r = selOrAll(); run({ op: 'set_delay', from: r[0], to: r[1], seconds: num('in-delay') }); },
    cap_idle: function () { var r = selOrAll(); run({ op: 'cap_idle', from: r[0], to: r[1], seconds: num('in-cap') }); },
    pause: function () {
      var r = needSel(); if (!r) return;
      run({ op: 'pause', at: r[0], seconds: num('in-pause') });
    },
    typing: function () {
      var r = selOrAll();
      run({ op: 'typing', from: r[0], to: r[1], seconds: num('in-type-delay'), jitter: num('in-type-jitter') });
    },
    inject: function () {
      var cmd = $('in-command').value, out = $('in-output').value;
      if (!cmd && !out) { flash('Enter a command and/or output to inject.', true); return; }
      var r = range();
      var at = r ? r[1] : S.events.length;
      run({
        op: 'inject', at: at, inject: {
          prompt: unescapeData($('in-prompt').value), command: cmd, output: out,
          after: unescapeData($('in-after').value),
          pre_delay: 0.5, typing_delay: num('in-inj-delay'), jitter: num('in-inj-delay') * 0.35,
          enter_delay: 0.4, output_delay: 0.15, line_delay: num('in-inj-line'),
          record_input: $('in-inj-input').checked, seed: Date.now() % 100000
        }
      }, function (n) { selectRange(at, at + n); scrollToSel(); });
    },
    insert_text: function () {
      var text = unescapeData($('in-text').value);
      if (!text) { flash('Enter text to insert.', true); return; }
      var r = range(), at = r ? r[1] : S.events.length;
      run({ op: 'insert', at: at, event: [num('in-text-delay'), 'o', text] }, function (n) { selectRange(at, at + n); });
    },
    marker: function () {
      var r = range(), at = r ? r[0] : Math.max(0, cur + 1);
      run({ op: 'marker', at: at, label: $('in-marker').value }, function () { selectRange(at, at + 1); });
    },
    replace: function () {
      var o = findOptions();
      if (!o.find) { flash('Enter the text to find.', true); $('in-find').focus(); return; }
      if (!found.matches) { flash('No matches to replace.', true); return; }
      var r = findRange();
      run({ op: 'replace', from: r[0], to: r[1], replace: o });
    },
    header: function () {
      var h = JSON.parse(JSON.stringify(S.header));
      h.title = $('h-title').value || undefined;
      h.term.cols = parseInt($('h-cols').value, 10);
      h.term.rows = parseInt($('h-rows').value, 10);
      h.idle_time_limit = parseFloat($('h-idle').value) || undefined;
      run({ op: 'header', header: h });
    }
  };

  function updateEvent(i, ev) { return run({ op: 'update', at: i, event: ev }); }

  // ------------------------------------------------------------ list

  var list = $('list'), spacer = $('list-spacer');

  function renderList() {
    var n = S.events.length;
    spacer.style.height = (n * ROW) + 'px';
    var first = Math.max(0, Math.floor(list.scrollTop / ROW) - 8);
    var last = Math.min(n, first + Math.ceil(list.clientHeight / ROW) + 16);
    var r = range() || [-1, -1];
    var html = '';
    for (var i = first; i < last; i++) {
      var e = S.events[i];
      var cls = 'row' + (i >= r[0] && i < r[1] ? ' sel' : '') + (i === cur ? ' cur' : '') + (found.set.has(i) ? ' match' : '');
      var d = e[0];
      var dcls = d >= 1 ? ' long' : d >= 0.3 ? ' mid' : '';
      var ccls = e[1] === 'o' && keys[i] ? 'key' : e[1];
      html += '<div class="' + cls + '" data-i="' + i + '" style="top:' + (i * ROW) + 'px">' +
        '<span class="c-idx">' + i + '</span>' +
        '<span class="c-time">' + fmt(times[i]) + '</span>' +
        '<span class="c-delay' + dcls + '" title="double-click to edit">' + fmtDelay(d) + '</span>' +
        '<span class="c-code"><span class="code ' + ccls + '">' + escHTML(e[1]) + '</span></span>' +
        '<span class="c-data">' + showData(e[2]) + '</span></div>';
    }
    // keep the spacer, replace rows
    list.innerHTML = '';
    list.appendChild(spacer);
    list.insertAdjacentHTML('beforeend', html);
  }

  list.addEventListener('scroll', function () { requestAnimationFrame(renderList); });

  // Selecting rows: click, Shift+click (or Ctrl/Cmd+click) to extend, or drag.
  // Dragging past the top or bottom edge scrolls the list.
  var listDrag = null;

  function rowAtY(clientY) {
    var r = list.getBoundingClientRect();
    var i = Math.floor((clientY - r.top + list.scrollTop) / ROW);
    return Math.max(0, Math.min(S.events.length - 1, i));
  }

  list.addEventListener('mousedown', function (ev) {
    var row = ev.target.closest('.row');
    if (!row || ev.target.tagName === 'INPUT' || ev.button !== 0) return; // right-click: the menu handles selection
    ev.preventDefault(); // no native text selection while selecting rows
    list.focus({ preventScroll: true });
    var i = +row.dataset.i;
    var extend = (ev.shiftKey || ev.ctrlKey || ev.metaKey) && sel;
    if (extend) sel.b = i;
    else sel = { a: i, b: i };
    if (!extend && player && player.term && !player.playing) player.seek(times[i]);
    listDrag = { y: ev.clientY, timer: setInterval(dragScroll, 30) };
    renderAll();
  });

  function dragTo(clientY) {
    var i = rowAtY(clientY);
    if (sel && i !== sel.b) {
      sel.b = i;
      renderAll();
    }
  }

  // keep scrolling while the pointer rests above or below the list
  function dragScroll() {
    if (!listDrag) return;
    var r = list.getBoundingClientRect(), y = listDrag.y, step = 0;
    if (y < r.top + ROW) step = -Math.ceil((r.top + ROW - y) / 4);
    else if (y > r.bottom - ROW) step = Math.ceil((y - r.bottom + ROW) / 4);
    if (step) {
      list.scrollTop += Math.max(-ROW * 3, Math.min(ROW * 3, step));
      dragTo(y);
    }
  }

  document.addEventListener('mousemove', function (ev) {
    if (!listDrag) return;
    listDrag.y = ev.clientY;
    dragTo(ev.clientY);
  });
  document.addEventListener('mouseup', function () {
    if (!listDrag) return;
    clearInterval(listDrag.timer);
    listDrag = null;
  });

  list.addEventListener('dblclick', function (ev) {
    var cell = ev.target.closest('.c-delay');
    var row = ev.target.closest('.row');
    if (!row) return;
    var i = +row.dataset.i;
    if (!cell) {
      $('event-panel').open = true;
      var ta = $('ev-data');
      if (ta) ta.focus();
      return;
    }
    var input = document.createElement('input');
    input.type = 'number';
    input.step = '0.01';
    input.min = '0';
    input.value = S.events[i][0].toFixed(3);
    cell.textContent = '';
    cell.appendChild(input);
    input.focus();
    input.select();
    var done = false;
    var commit = function (save) {
      if (done) return;
      done = true;
      var v = parseFloat(input.value);
      if (save && !isNaN(v) && v >= 0 && Math.abs(v - S.events[i][0]) > 1e-7) {
        var e = S.events[i].slice();
        e[0] = v;
        updateEvent(i, e);
      } else renderList();
    };
    input.addEventListener('keydown', function (e) {
      e.stopPropagation();
      if (e.key === 'Enter') commit(true);
      else if (e.key === 'Escape') commit(false);
    });
    input.addEventListener('blur', function () { commit(true); });
  });

  function scrollToSel() {
    if (!sel) return;
    var top = sel.b * ROW;
    if (top < list.scrollTop) list.scrollTop = top - ROW * 2;
    else if (top + ROW > list.scrollTop + list.clientHeight) list.scrollTop = top - list.clientHeight + ROW * 3;
  }

  // ------------------------------------------------------------ timeline

  var canvas = $('timeline');
  var ctx = canvas.getContext('2d');
  var css = getComputedStyle(document.documentElement);
  var color = function (name) { return css.getPropertyValue(name).trim(); };
  var C = {
    o: color('--k-o'), key: color('--k-key'), i: color('--k-i'), m: color('--k-m'), r: color('--k-r'),
    x: color('--danger'), sel: color('--sel'), selLine: color('--sel-line'), accent: color('--accent'),
    muted: color('--muted'), line: color('--line')
  };

  function tx(t, W) { return (t - view.start) / (view.end - view.start) * W; }
  function xt(x, W) { return view.start + x / W * (view.end - view.start); }

  function niceStep(sec, px, W) {
    var steps = [0.01, 0.02, 0.05, 0.1, 0.2, 0.5, 1, 2, 5, 10, 15, 30, 60, 120, 300, 600, 1800, 3600];
    for (var i = 0; i < steps.length; i++) if (steps[i] / sec * W >= px) return steps[i];
    return 7200;
  }

  function renderTimeline() {
    var dpr = window.devicePixelRatio || 1;
    var W = canvas.clientWidth, H = canvas.clientHeight;
    if (canvas.width !== W * dpr || canvas.height !== H * dpr) {
      canvas.width = W * dpr;
      canvas.height = H * dpr;
    }
    ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
    ctx.clearRect(0, 0, W, H);
    var axisH = 16, top = 6, bottom = H - axisH;

    // selection band
    var r = range();
    if (r) {
      var sp = span(r[0], r[1]);
      var x0 = tx(sp[0], W), x1 = Math.max(tx(sp[1], W), x0 + 2);
      ctx.fillStyle = C.sel;
      ctx.fillRect(x0, 0, x1 - x0, bottom);
      ctx.fillStyle = C.selLine;
      ctx.fillRect(x0, 0, 1, bottom);
      ctx.fillRect(x1 - 1, 0, 1, bottom);
    }

    // events
    var first = Math.max(0, indexAtTime(view.start));
    for (var i = first; i < S.events.length; i++) {
      var t = times[i];
      if (t > view.end) break;
      var e = S.events[i], x = Math.round(tx(t, W)) + 0.5;
      var kind = e[1] === 'o' && keys[i] ? 'key' : e[1];
      ctx.fillStyle = C[kind] || C.o;
      if (e[1] === 'm') {
        ctx.fillRect(x - 0.5, 0, 1.5, bottom);
        if (e[2]) {
          ctx.font = '10px ui-monospace,monospace';
          ctx.fillText(e[2], x + 3, 10);
        }
        continue;
      }
      var h;
      if (e[1] === 'o' && !keys[i]) h = Math.min(1, Math.log(e[2].length + 1) / Math.log(2000)) * (bottom - top - 14) + 6;
      else if (e[1] === 'r' || e[1] === 'x') h = bottom - top;
      else h = 14;
      ctx.fillRect(x - 0.5, bottom - h, 1.5, h);
    }

    // axis
    ctx.fillStyle = C.line;
    ctx.fillRect(0, bottom, W, 1);
    var step = niceStep(view.end - view.start, 70, W);
    ctx.fillStyle = C.muted;
    ctx.font = '10px ui-monospace,monospace';
    for (var s = Math.ceil(view.start / step) * step; s <= view.end; s += step) {
      var ax = tx(s, W);
      ctx.fillRect(ax, bottom, 1, 4);
      var label = step < 1 ? s.toFixed(step < 0.1 ? 2 : 1) + 's' : (s >= 60 ? Math.floor(s / 60) + ':' + String(Math.round(s % 60)).padStart(2, '0') : Math.round(s) + 's');
      ctx.fillText(label, ax + 3, H - 3);
    }

    // playhead
    if (player && player.term) {
      var px = tx(player.time, W);
      ctx.fillStyle = C.accent;
      ctx.fillRect(px - 1, 0, 2, bottom);
      ctx.beginPath();
      ctx.moveTo(px - 5, 0); ctx.lineTo(px + 5, 0); ctx.lineTo(px, 6); ctx.fill();
    }
  }

  var drag = null;
  canvas.addEventListener('pointerdown', function (ev) {
    if (ev.button !== 0) return;
    canvas.setPointerCapture(ev.pointerId);
    drag = { x: ev.offsetX, moved: false };
  });
  canvas.addEventListener('pointermove', function (ev) {
    if (!drag) return;
    if (Math.abs(ev.offsetX - drag.x) > 3) drag.moved = true;
    if (!drag.moved) return;
    var W = canvas.clientWidth;
    var t0 = xt(Math.min(drag.x, ev.offsetX), W), t1 = xt(Math.max(drag.x, ev.offsetX), W);
    var from = indexAtTime(t0) + 1, to = indexAtTime(t1) + 1;
    if (to > from) { sel = { a: from, b: to - 1 }; scrollToSel(); }
    else sel = null;
    renderAll();
  });
  canvas.addEventListener('pointerup', function (ev) {
    if (drag && !drag.moved && player && player.term) {
      var t = xt(ev.offsetX, canvas.clientWidth);
      player.seek(t);
      var i = indexAtTime(t);
      if (i >= 0 && !ev.shiftKey) { sel = { a: i, b: i }; scrollToSel(); }
      renderAll();
    }
    drag = null;
  });
  canvas.addEventListener('wheel', function (ev) {
    ev.preventDefault();
    var W = canvas.clientWidth, len = view.end - view.start, dur = Math.max(S.duration, 1);
    if (ev.ctrlKey || ev.metaKey) {
      var t = xt(ev.offsetX, W);
      var f = Math.exp(ev.deltaY * 0.002);
      var nlen = Math.min(dur, Math.max(0.05, len * f));
      var ratio = (t - view.start) / len;
      view.start = t - ratio * nlen;
      view.end = view.start + nlen;
    } else {
      var d = (ev.deltaX || ev.deltaY) / W * len;
      view.start += d; view.end += d;
    }
    if (view.start < 0) { view.end -= view.start; view.start = 0; }
    if (view.end > dur) { view.start -= view.end - dur; view.end = dur; view.start = Math.max(0, view.start); }
    viewFit = view.start <= 0 && view.end >= dur;
    renderTimeline();
  }, { passive: false });

  $('btn-zoom-all').onclick = function () { viewFit = true; view = { start: 0, end: Math.max(S.duration, 1) }; renderTimeline(); };
  $('btn-zoom-sel').onclick = function () {
    var r = range(); if (!r) return;
    var sp = span(r[0], r[1]), pad = Math.max(0.2, (sp[1] - sp[0]) * 0.1);
    view = { start: Math.max(0, sp[0] - pad), end: Math.min(Math.max(S.duration, 1), sp[1] + pad) };
    viewFit = false;
    renderTimeline();
  };
  window.addEventListener('resize', renderTimeline);

  // ------------------------------------------------------------ side panel

  function renderSummary() {
    var r = range(), el = $('sel-summary');
    if (!r) {
      el.innerHTML = 'Nothing selected <span class="muted">— tools apply to the whole recording</span>';
      return;
    }
    var sp = span(r[0], r[1]), k = 0;
    for (var i = r[0]; i < r[1]; i++) if (keys[i]) k++;
    el.innerHTML = '#' + r[0] + (r[1] - r[0] > 1 ? '–#' + (r[1] - 1) : '') + ' · ' + (r[1] - r[0]) + ' event' + (r[1] - r[0] > 1 ? 's' : '') +
      '<br><span class="muted">' + fmt(sp[0]) + ' → ' + fmt(sp[1]) + ' · ' + (sp[1] - sp[0]).toFixed(3) + 's' +
      (k ? ' · ' + k + ' keystrokes' : '') + '</span>';
  }

  var editorFor = -1;
  function renderEventEditor() {
    var r = range(), box = $('event-editor');
    if (!r || r[1] - r[0] !== 1) {
      editorFor = -1;
      box.className = 'muted';
      box.textContent = 'Select a single event to edit it.';
      return;
    }
    var i = r[0], e = S.events[i];
    var sig = i + ':' + JSON.stringify(e);
    if (editorFor === sig) return;
    editorFor = sig;
    box.className = '';
    box.innerHTML =
      '<div class="field"><label>Delay (s)</label><input id="ev-delay" type="number" step="0.01" min="0"></div>' +
      '<div class="field"><label>Type</label><select id="ev-code">' +
      '<option value="o">o — output</option><option value="i">i — input</option><option value="m">m — marker</option>' +
      '<option value="r">r — resize</option><option value="x">x — exit</option></select></div>' +
      '<label class="block">Data <span class="muted">(\\r \\n \\e \\xHH \\uHHHH)</span></label>' +
      '<textarea id="ev-data" rows="5" spellcheck="false"></textarea>' +
      '<div class="field"><span style="flex:1"></span><button id="ev-apply">Apply</button></div>';
    $('ev-delay').value = e[0].toFixed(3);
    $('ev-code').value = e[1];
    $('ev-data').value = escapeData(e[2]);
    $('ev-apply').onclick = function () {
      updateEvent(i, [parseFloat($('ev-delay').value) || 0, $('ev-code').value, unescapeData($('ev-data').value)]);
    };
  }

  function renderAll() {
    renderList();
    renderTimeline();
    renderSummary();
    renderEventEditor();
  }

  // ------------------------------------------------------------ find & replace

  function findOptions() {
    var regex = $('in-find-regex').checked, f = $('in-find').value;
    return {
      find: regex ? f : unescapeData(f),
      replace: unescapeData($('in-replace').value),
      regex: regex,
      ignore_case: $('in-find-case').checked,
      ignore_ansi: $('in-find-ansi').checked
    };
  }

  function findRange() {
    var r = range();
    return $('in-find-sel').checked && r ? r : [0, S.events.length];
  }

  function setFound(events, matches) {
    found = { events: events, set: new Set(events), matches: matches };
    var el = $('find-count');
    var has = $('in-find').value !== '';
    el.className = has && !matches ? 'none' : 'muted';
    el.textContent = !has ? '—' : matches
      ? matches + ' match' + (matches > 1 ? 'es' : '') + ' · ' + events.length + ' event' + (events.length > 1 ? 's' : '')
      : 'no matches';
    renderList();
  }

  var findSeq = 0;
  function runFind() {
    if (!S) return;
    var o = findOptions();
    if (!o.find) { setFound([], 0); return; }
    var r = findRange(), seq = ++findSeq;
    api('api/find', { from: r[0], to: r[1], replace: o }).then(function (res) {
      if (seq === findSeq) setFound(res.events, res.matches);
    }).catch(function (e) {
      if (seq !== findSeq) return;
      setFound([], 0);
      $('find-count').textContent = e.message;
    });
  }

  function scheduleFind() {
    clearTimeout(scheduleFind.t);
    scheduleFind.t = setTimeout(runFind, 150);
  }

  // jump to the next (dir 1) or previous (dir -1) event with a match
  function gotoMatch(dir) {
    var ev = found.events;
    if (!ev.length) return;
    var from = sel ? sel.b : (dir > 0 ? -1 : S.events.length);
    var next = null;
    if (dir > 0) next = ev.find(function (i) { return i > from; });
    else for (var k = ev.length - 1; k >= 0; k--) if (ev[k] < from) { next = ev[k]; break; }
    if (next == null) next = dir > 0 ? ev[0] : ev[ev.length - 1]; // wrap around
    sel = { a: next, b: next };
    if (player && player.term && !player.playing) player.seek(times[next]);
    scrollToSel();
    renderAll();
  }

  ['in-find', 'in-find-regex', 'in-find-case', 'in-find-ansi', 'in-find-sel'].forEach(function (id) {
    $(id).addEventListener(id === 'in-find' ? 'input' : 'change', scheduleFind);
  });
  $('in-find').addEventListener('keydown', function (ev) {
    if (ev.key === 'Enter') { ev.preventDefault(); gotoMatch(ev.shiftKey ? -1 : 1); }
  });
  $('in-replace').addEventListener('keydown', function (ev) {
    if (ev.key === 'Enter') { ev.preventDefault(); ops.replace(); }
  });
  $('btn-find-next').onclick = function () { gotoMatch(1); };
  $('btn-find-prev').onclick = function () { gotoMatch(-1); };

  // ------------------------------------------------------------ preview sync

  // Move the "current event" marker by toggling classes. Rebuilding the rows
  // during playback would swap elements under the pointer and drop clicks.
  function setCurrentRow(i) {
    var old = list.querySelector('.row.cur');
    if (old) old.classList.remove('cur');
    cur = i;
    var row = list.querySelector('.row[data-i="' + i + '"]');
    if (row) row.classList.add('cur');
  }

  var rafPending = false;
  function onPlayerTime(t) {
    if (stopAt != null && t >= stopAt - 1e-6) {
      stopAt = null;
      player.pause();
    }
    var i = indexAtTime(t);
    if (rafPending) return;
    rafPending = true;
    requestAnimationFrame(function () {
      rafPending = false;
      if (i !== cur) {
        setCurrentRow(i);
        if (player.playing) {
          var top = cur * ROW;
          if (top < list.scrollTop || top > list.scrollTop + list.clientHeight - ROW) list.scrollTop = top - list.clientHeight / 2;
        }
      }
      renderTimeline();
    });
  }

  $('btn-play-sel').onclick = function () {
    var r = needSel(); if (!r || !player) return;
    var sp = span(r[0], r[1]);
    player.pause();
    player.seek(Math.max(0, sp[0] - 0.001));
    stopAt = sp[1];
    player.play();
  };

  // ------------------------------------------------------------ context menus
  //
  // Item shapes:
  //   {label, kbd, action, disabled, danger, checked}   a command
  //   {label, items: [...]}                              a submenu
  //   {label, input: {placeholder, value, submit(v)}}    an inline value entry
  //   {sep: true}                                        a separator

  var menus = []; // open menu elements, index = nesting level

  function closeMenus(level) {
    level = level || 0;
    while (menus.length > level) menus.pop().remove();
  }

  function menuButtons(m) {
    return Array.prototype.filter.call(m.querySelectorAll(':scope > .ctx-item'), function (b) { return !b.disabled; });
  }

  function openMenu(items, x, y, level, flipX) {
    level = level || 0;
    closeMenus(level);
    var m = document.createElement('div');
    m.className = 'ctx';
    m.setAttribute('role', 'menu');
    items.forEach(function (it) {
      if (!it) return;
      if (it.sep) {
        m.insertAdjacentHTML('beforeend', '<div class="ctx-sep" role="separator"></div>');
        return;
      }
      if (it.input) {
        m.appendChild(menuInput(it));
        return;
      }
      var b = document.createElement('button');
      b.type = 'button';
      b.className = 'ctx-item' + (it.danger ? ' danger' : '');
      b.setAttribute('role', 'menuitem');
      b.disabled = !!it.disabled;
      b.innerHTML = '<span class="ctx-check"></span><span class="ctx-label"></span><span class="ctx-kbd"></span>';
      b.children[0].textContent = it.checked ? '✓' : '';
      b.children[1].textContent = it.label;
      b.children[2].textContent = it.items ? '›' : (it.kbd || '');
      var openSub = function (focusFirst) {
        var r = b.getBoundingClientRect();
        var sub = openMenu(it.items, r.right - 3, r.top - 5, level + 1, r.left + 3);
        b.classList.add('open');
        if (focusFirst) { var f = menuButtons(sub)[0] || sub.querySelector('input'); if (f) f.focus(); }
      };
      b._openSub = it.items ? openSub : null;
      b.addEventListener('mouseenter', function () {
        b.focus({ preventScroll: true });
        m.querySelectorAll('.ctx-item.open').forEach(function (o) { if (o !== b) o.classList.remove('open'); });
        if (it.items) openSub(false);
        else closeMenus(level + 1);
      });
      b.addEventListener('click', function () {
        if (it.items) { openSub(true); return; }
        closeMenus(0);
        it.action();
      });
      m.appendChild(b);
    });
    document.body.appendChild(m);
    var w = m.offsetWidth, h = m.offsetHeight;
    if (x + w > innerWidth - 4) x = flipX != null ? flipX - w : innerWidth - w - 4;
    if (y + h > innerHeight - 4) y = innerHeight - h - 4;
    m.style.left = Math.max(4, x) + 'px';
    m.style.top = Math.max(4, y) + 'px';
    menus.push(m);
    return m;
  }

  // An inline text field inside a menu; Enter submits, Esc closes.
  function menuInput(it) {
    var row = document.createElement('form');
    row.className = 'ctx-input';
    row.innerHTML = '<span class="ctx-check"></span><label></label><input type="text" spellcheck="false"><button type="submit">↵</button>';
    row.children[1].textContent = it.label;
    var input = row.children[2];
    input.placeholder = it.input.placeholder || '';
    input.value = it.input.value || '';
    row.addEventListener('mouseenter', function () {
      var level = menus.indexOf(row.parentNode);
      if (level >= 0) closeMenus(level + 1);
    });
    row.addEventListener('submit', function (ev) {
      ev.preventDefault();
      var v = input.value;
      closeMenus(0);
      it.input.submit(v);
    });
    return row;
  }

  function moveFocus(m, dir) {
    var bs = menuButtons(m);
    if (!bs.length) return;
    var i = bs.indexOf(document.activeElement);
    bs[(i + dir + bs.length) % bs.length].focus();
  }

  // Keyboard handling while a menu is open (capture phase, before editor shortcuts).
  document.addEventListener('keydown', function (ev) {
    if (!menus.length) return;
    var m = menus[menus.length - 1];
    var inInput = ev.target.tagName === 'INPUT';
    var k = ev.key, handled = true;
    if (k === 'Escape') closeMenus(menus.length > 1 && !inInput ? menus.length - 1 : 0);
    else if (inInput) handled = false;
    else if (k === 'ArrowDown') moveFocus(m, 1);
    else if (k === 'ArrowUp') moveFocus(m, -1);
    else if (k === 'Home') { var hb = menuButtons(m); if (hb.length) hb[0].focus(); }
    else if (k === 'End') { var eb = menuButtons(m); if (eb.length) eb[eb.length - 1].focus(); }
    else if (k === 'ArrowRight') { if (document.activeElement._openSub) document.activeElement._openSub(true); }
    else if (k === 'ArrowLeft') {
      if (menus.length > 1) {
        closeMenus(menus.length - 1);
        var opener = menus[menus.length - 1].querySelector('.ctx-item.open');
        if (opener) { opener.classList.remove('open'); opener.focus(); }
      }
    } else if (k === 'Tab') moveFocus(m, ev.shiftKey ? -1 : 1);
    else if (k !== 'Enter' && k !== ' ') {
      // type-to-jump: first item starting with the key
      var hit = menuButtons(m).find(function (b) { return b.children[1].textContent.toLowerCase().startsWith(k.toLowerCase()); });
      if (hit) hit.focus(); else handled = false;
    } else handled = false; // Enter / Space click the focused button natively
    if (handled) { ev.preventDefault(); ev.stopPropagation(); }
  }, true);

  document.addEventListener('mousedown', function (ev) {
    if (menus.length && !ev.target.closest('.ctx')) closeMenus(0);
  }, true);
  window.addEventListener('blur', function () { closeMenus(0); });
  window.addEventListener('resize', function () { closeMenus(0); });
  list.addEventListener('scroll', function () { closeMenus(0); });

  // ---------------------------------------------------- clipboard

  var clip = null; // events copied inside this editor

  function eventsToText(evs) {
    return evs.map(function (e) { return JSON.stringify(e); }).join('\n');
  }

  function parseEvents(text) {
    var lines = text.split('\n').filter(function (l) { return l.trim(); });
    if (!lines.length) return null;
    try {
      var evs = lines.map(function (l) { return JSON.parse(l); });
      var ok = evs.every(function (e) { return Array.isArray(e) && e.length === 3 && typeof e[0] === 'number' && typeof e[1] === 'string'; });
      return ok ? evs : null;
    } catch (e) { return null; }
  }

  function visibleText(evs) {
    return evs.filter(function (e) { return e[1] === 'o'; })
      .map(function (e) { return e[2]; }).join('')
      .replace(ansiRe, '').replace(/\r\n/g, '\n').replace(/\r/g, '');
  }

  function writeClipboard(text) {
    if (navigator.clipboard) navigator.clipboard.writeText(text).catch(function () {});
  }

  function copySel(cut) {
    var r = needSel(); if (!r) return;
    clip = S.events.slice(r[0], r[1]);
    writeClipboard(eventsToText(clip));
    if (cut) run({ op: 'delete', from: r[0], to: r[1] }, function () { sel = r[0] < S.events.length ? { a: r[0], b: r[0] } : null; });
    else flash('copied ' + clip.length + ' event' + (clip.length > 1 ? 's' : ''));
  }

  function copyVisible() {
    var r = needSel(); if (!r) return;
    writeClipboard(visibleText(S.events.slice(r[0], r[1])));
    flash('copied text');
  }

  // Paste events: the system clipboard if it holds asciicast event lines
  // (e.g. copied from another file), otherwise what was copied here,
  // otherwise plain text as one output event.
  function paste(at) {
    var insert = function (evs) {
      run({ op: 'insert', at: at, events: evs }, function (n) { selectRange(at, at + n); scrollToSel(); });
    };
    var fallback = function (text) {
      if (clip) insert(clip);
      else if (text) insert([[0.1, 'o', text.replace(/\r?\n/g, '\r\n')]]);
      else flash('Clipboard is empty.', true);
    };
    if (!navigator.clipboard || !navigator.clipboard.readText) { fallback(''); return; }
    navigator.clipboard.readText().then(function (text) {
      var evs = parseEvents(text);
      if (evs) insert(evs); else fallback(text);
    }, function () { fallback(''); });
  }

  // ---------------------------------------------------- menu actions

  function opOn(r, op, extra) { run(Object.assign({ op: op, from: r[0], to: r[1] }, extra || {})); }

  function playFrom(t, stop) {
    if (!player || !player.term) return;
    player.pause();
    player.seek(Math.max(0, t - 0.001));
    stopAt = stop == null ? null : stop;
    player.play();
  }

  function openPanel(panelId, focusId) {
    var p = $(panelId);
    p.open = true;
    p.scrollIntoView({ block: 'nearest', behavior: 'smooth' });
    if (focusId) setTimeout(function () { $(focusId).focus(); $(focusId).select && $(focusId).select(); }, 50);
  }

  function seconds(v) {
    var n = parseFloat(String(v).replace(',', '.'));
    if (isNaN(n) || n < 0) { flash('Enter a number of seconds.', true); return null; }
    return n;
  }

  function pauseItems(at) {
    return [0.5, 1, 2, 3].map(function (s) {
      return { label: s + ' s', action: function () { run({ op: 'pause', at: at, seconds: s }); } };
    }).concat([{ sep: true }, {
      label: 'Custom', input: { placeholder: 'seconds', submit: function (v) { var s = seconds(v); if (s != null) run({ op: 'pause', at: at, seconds: s }); } }
    }]);
  }

  function markerItem(at) {
    return {
      label: 'Marker', input: {
        placeholder: 'label, Enter to add',
        submit: function (v) { run({ op: 'marker', at: at, label: v }, function () { selectRange(at, at + 1); }); }
      }
    };
  }

  function rowMenu(i) {
    var r = range();
    if (!r || i < r[0] || i >= r[1]) { sel = { a: i, b: i }; renderAll(); r = range(); }
    var n = r[1] - r[0], multi = n > 1, e = S.events[r[0]];
    var sp = span(r[0], r[1]);
    var text = visibleText(S.events.slice(r[0], r[1])).trim();
    var findText = text.split('\n')[0].slice(0, 60);
    var what = multi ? n + ' events' : 'event';
    return [
      { label: 'Play from here', action: function () { playFrom(sp[0]); } },
      { label: multi ? 'Play selection' : 'Play this event', action: function () { playFrom(sp[0], sp[1]); } },
      { sep: true },
      { label: 'Edit event…', disabled: multi, kbd: 'dbl-click', action: function () { openPanel('event-panel', 'ev-data'); } },
      {
        label: 'Set delay', items: [0, 0.03, 0.08, 0.17, 0.25, 0.5, 1, 2].map(function (d) {
          return { label: d + ' s', checked: !multi && Math.abs(e[0] - d) < 5e-4, action: function () { opOn(r, 'set_delay', { seconds: d }); } };
        }).concat([{ sep: true }, {
          label: 'Custom', input: { placeholder: 'seconds', value: multi ? '' : e[0].toFixed(3), submit: function (v) { var s = seconds(v); if (s != null) opOn(r, 'set_delay', { seconds: s }); } }
        }])
      },
      {
        label: 'Timing', items: [
          { label: 'Insert pause before', items: pauseItems(r[0]) },
          { sep: true },
          { label: 'Speed up ×2', action: function () { opOn(r, 'stretch', { factor: 0.5 }); } },
          { label: 'Speed up ×4', action: function () { opOn(r, 'stretch', { factor: 0.25 }); } },
          { label: 'Slow down ×2', action: function () { opOn(r, 'stretch', { factor: 2 }); } },
          {
            label: 'Set length', input: {
              placeholder: 'seconds', value: (sp[1] - sp[0]).toFixed(2),
              submit: function (v) { var s = seconds(v); if (s != null) opOn(r, 'stretch_to', { seconds: s }); }
            }
          },
          { sep: true },
          { label: 'Cap pauses at 0.5 s', action: function () { opOn(r, 'cap_idle', { seconds: 0.5 }); } },
          { label: 'Cap pauses at 1 s', action: function () { opOn(r, 'cap_idle', { seconds: 1 }); } },
          { label: 'Even out typing', action: function () { opOn(r, 'typing', { seconds: num('in-type-delay'), jitter: num('in-type-jitter') }); } }
        ]
      },
      { sep: true },
      { label: 'Cut', kbd: 'Ctrl+X', action: function () { copySel(true); } },
      { label: 'Copy', kbd: 'Ctrl+C', action: function () { copySel(false); } },
      { label: 'Copy visible text', disabled: !text, action: copyVisible },
      { label: 'Paste after', kbd: 'Ctrl+V', action: function () { paste(r[1]); } },
      { label: 'Paste before', action: function () { paste(r[0]); } },
      { label: 'Duplicate', kbd: 'Ctrl+D', action: ops.duplicate },
      { sep: true },
      {
        label: 'Insert', items: insertItems({ before: r[0], after: r[1] })
      },
      {
        label: 'Transform', items: [
          { label: 'Merge output', disabled: !multi, action: ops.merge },
          { label: 'Split lines', action: ops.split }
        ]
      },
      {
        label: findText ? 'Find “' + (findText.length > 28 ? findText.slice(0, 28) + '…' : findText) + '”' : 'Find…',
        kbd: 'Ctrl+F',
        action: function () {
          $('in-find').value = findText;
          $('in-find-regex').checked = false;
          openPanel('find-panel', 'in-find');
          scheduleFind();
        }
      },
      { label: 'Export ' + (multi ? 'selection' : 'event') + ' as', items: exportItems(true, ['anim', 'html', 'text', 'cast']) },
      { sep: true },
      { label: 'Delete ' + what, kbd: 'Del', danger: true, action: ops.delete }
    ];
  }

  function timelineMenu(t) {
    var at = indexAtTime(t) + 1;
    var dur = Math.max(S.duration, 1);
    var zoom = function (f) {
      var len = Math.min(dur, Math.max(0.05, (view.end - view.start) * f));
      view.start = Math.max(0, t - len / 2);
      view.end = Math.min(dur, view.start + len);
      view.start = Math.max(0, view.end - len);
      viewFit = view.start <= 0 && view.end >= dur;
      renderTimeline();
    };
    var ph = player && player.term ? player.time : 0;
    return [
      { label: 'Seek here', kbd: fmt(t), action: function () { player.seek(t); renderTimeline(); } },
      { label: 'Play from here', action: function () { playFrom(t); } },
      {
        label: 'Select playhead → here', disabled: Math.abs(ph - t) < 1e-3, action: function () {
          var a = Math.min(ph, t), b = Math.max(ph, t);
          var from = indexAtTime(a) + 1, to = indexAtTime(b) + 1;
          if (to > from) { sel = { a: from, b: to - 1 }; scrollToSel(); renderAll(); }
        }
      },
      { sep: true },
      { label: 'Insert here', items: insertItems({ at: at }) },
      { label: 'Insert pause here', items: pauseItems(at) },
      markerItem(at),
      { label: 'Paste here', action: function () { paste(at); } },
      { sep: true },
      { label: 'Zoom in', action: function () { zoom(0.5); } },
      { label: 'Zoom out', action: function () { zoom(2); } },
      { label: 'Zoom to selection', disabled: !range(), action: function () { $('btn-zoom-sel').click(); } },
      { label: 'Fit', action: function () { $('btn-zoom-all').click(); } }
    ];
  }

  function previewMenu() {
    var r = range();
    var selected = player.selectedText(); // taken now: playback may clear it before the menu item is clicked
    var screenText = function () {
      var b = player.term.buffer.active, out = [];
      for (var i = 0; i < player.term.rows; i++) {
        var l = b.getLine(b.viewportY + i);
        out.push(l ? l.translateToString(true) : '');
      }
      return out.join('\n').replace(/\n+$/, '');
    };
    return [
      { label: player.playing ? 'Pause' : 'Play', kbd: 'Space', action: function () { stopAt = null; player.toggle(); } },
      { label: 'Play selection', disabled: !r, action: function () { $('btn-play-sel').click(); } },
      { label: 'Restart', action: function () { playFrom(0); } },
      {
        label: 'Speed', items: [0.5, 1, 1.5, 2, 3, 5].map(function (s) {
          return { label: s + '×', checked: player.speed === s, action: function () { player.setSpeed(s); } };
        })
      },
      { sep: true },
      {
        label: 'Select event at playhead', disabled: cur < 0, action: function () {
          sel = { a: cur, b: cur }; scrollToSel(); renderAll();
        }
      },
      { label: 'Insert at playhead', items: insertItems({ at: Math.max(0, cur + 1) }) },
      { sep: true },
      {
        label: 'Copy selected text', kbd: 'Ctrl+C', disabled: !selected, action: function () {
          writeClipboard(selected); flash('copied selected text');
        }
      },
      { label: 'Copy screen text', action: function () { writeClipboard(screenText()); flash('copied screen text'); } },
      { label: 'Save frame as', items: exportItems(false, ['still']) },
      { label: 'Fullscreen', kbd: 'f', action: function () { player.fullscreen(); } }
    ];
  }

  function generalMenu() {
    var r = range();
    return [
      { label: 'Save', kbd: 'Ctrl+S', disabled: !S.dirty, action: save },
      { label: 'Undo', kbd: 'Ctrl+Z', disabled: !S.can_undo, action: undo },
      { label: 'Redo', kbd: 'Ctrl+Shift+Z', disabled: !S.can_redo, action: redo },
      { sep: true },
      { label: 'Select all', kbd: 'Ctrl+A', disabled: !S.events.length, action: function () { sel = { a: 0, b: S.events.length - 1 }; renderAll(); } },
      { label: 'Clear selection', kbd: 'Esc', disabled: !r, action: function () { sel = null; renderAll(); } },
      { label: 'Find & replace', kbd: 'Ctrl+F', action: function () { openPanel('find-panel', 'in-find'); } },
      { label: 'Paste at end', action: function () { paste(S.events.length); } },
      { label: 'Remove trailing exit', action: function () { run({ op: 'trim_exit' }); } },
      { sep: true },
      { label: 'Export', items: exportItems(false) },
      { label: 'Download recording', action: function () { $('btn-download').click(); } }
    ];
  }

  // ---------------------------------------------------- export

  // kind decides which options apply: anim (gif, svg), still (one frame), html, text, cast
  var FORMATS = [
    { name: 'gif', label: 'Animated GIF', kind: 'anim' },
    { name: 'svg', label: 'Animated SVG', kind: 'anim' },
    { name: 'html', label: 'HTML page (self-contained)', kind: 'html' },
    { name: 'png', label: 'PNG image', kind: 'still' },
    { name: 'svg-frame', label: 'SVG image', kind: 'still' },
    { name: 'txt', label: 'Text transcript', kind: 'text' },
    { name: 'screen', label: 'Screen text', kind: 'still' },
    { name: 'take', label: 'gotake recording', kind: 'cast' },
    { name: 'cast', label: 'asciicast v3', kind: 'cast' },
    { name: 'v2', label: 'asciicast v2', kind: 'cast' }
  ];
  var formatOf = function (name) { return FORMATS.find(function (f) { return f.name === name; }); };

  $('ex-format').innerHTML = FORMATS.map(function (f) {
    return '<option value="' + f.name + '">' + escHTML(f.label + (f.kind === 'still' ? ' (frame)' : '')) + '</option>';
  }).join('');

  // show only the options that apply to the chosen format
  function syncExportPanel() {
    var k = formatOf($('ex-format').value).kind;
    var show = { 'ex-anim': k === 'anim', 'ex-img': k === 'anim', 'ex-still': k === 'still' && $('ex-format').value !== 'screen', 'ex-html': k === 'html', 'ex-still-note': k === 'still' };
    $('export-panel').querySelectorAll('.ex-anim, .ex-img, .ex-still, .ex-html, .ex-still-note').forEach(function (el) {
      el.hidden = !Array.prototype.some.call(el.classList, function (c) { return show[c]; });
    });
  }
  $('ex-format').addEventListener('change', syncExportPanel);
  syncExportPanel();

  // exportAs downloads the recording in a format; selection: true exports only the selected events
  function exportAs(name, selection) {
    var f = formatOf(name), q = new URLSearchParams({ format: name });
    var r = range();
    if (selection) {
      if (!r) { flash('Select events to export, or export the whole recording.', true); return; }
      q.set('from', r[0]);
      q.set('to', r[1]);
    }
    if (f.kind === 'still') q.set('at', player && player.term ? player.time : -1);
    if (f.kind === 'anim') {
      q.set('speed', $('ex-speed').value || 1);
      if ($('ex-idle').value) q.set('idle', $('ex-idle').value);
      q.set('fps', $('ex-fps').value || 30);
    }
    if (f.kind === 'anim' || f.kind === 'still') {
      q.set('font_size', $('ex-font').value || 16);
      q.set('window', $('ex-window').checked ? 1 : 0);
      q.set('window_style', $('ex-window-style').value);
      q.set('cursor', $('ex-cursor').checked ? 1 : 0);
    }
    if (f.kind === 'html') {
      q.set('title', S.header.title || '');
      q.set('autoplay', $('ex-autoplay').checked ? 1 : 0);
      q.set('loop', $('ex-loop').checked ? 1 : 0);
    }
    flash('Exporting ' + f.label + '…');
    fetch('api/export?' + q).then(function (res) {
      if (!res.ok) return res.json().then(function (j) { throw new Error(j.error || res.statusText); });
      var m = /filename="([^"]+)"/.exec(res.headers.get('Content-Disposition') || '');
      return res.blob().then(function (b) { return { blob: b, name: m ? m[1] : 'recording' }; });
    }).then(function (d) {
      var url = URL.createObjectURL(d.blob);
      var a = document.createElement('a');
      a.href = url;
      a.download = d.name;
      document.body.appendChild(a);
      a.click();
      a.remove();
      setTimeout(function () { URL.revokeObjectURL(url); }, 10000);
      var kb = d.blob.size / 1024;
      flash('Exported ' + d.name + ' (' + (kb >= 1024 ? (kb / 1024).toFixed(1) + ' MB' : kb.toFixed(1) + ' KB') + ')');
    }).catch(function (e) { flash('Export failed: ' + e.message, true); });
  }

  function exportItems(selection, kinds) {
    return FORMATS.filter(function (f) { return !kinds || kinds.indexOf(f.kind) >= 0; }).map(function (f) {
      return { label: f.label + (f.kind === 'still' ? ' at playhead' : ''), action: function () { exportAs(f.name, selection); } };
    });
  }

  function openExportMenu() {
    var b = $('btn-export').getBoundingClientRect();
    var useSel = $('ex-range').value === 'sel' && !!range();
    var items = exportItems(useSel).concat([
      { sep: true },
      {
        label: 'Selection only', checked: useSel, disabled: !range(), action: function () {
          $('ex-range').value = useSel ? 'all' : 'sel';
          openExportMenu(); // keep the menu open after toggling
        }
      },
      { label: 'Options…', action: function () { openPanel('export-panel', 'ex-format'); } }
    ]);
    openMenu(items, b.right - 230, b.bottom + 4, 0);
  }

  $('btn-export').addEventListener('click', function (ev) {
    ev.stopPropagation();
    if (menus.length) closeMenus(0); else openExportMenu();
  });
  $('ex-go').onclick = function () { exportAs($('ex-format').value, $('ex-range').value === 'sel'); };

  // ---------------------------------------------------- insert window
  //
  // A floating, draggable window for inserting a command, output, a marker or
  // a pause at a fixed position. It opens where the user right-clicked.

  var menuPos = { x: 200, y: 200 }; // where the last context menu opened
  var insertWin = null;

  var PROMPT_END = /[$#%>❯»]\s?$/;

  // rawLastLine returns the raw (escape codes included) text after the last
  // line break in all output before event index at: what the cursor's line shows.
  function rawLastLine(at) {
    var tail = '';
    for (var i = at - 1; i >= 0; i--) {
      var e = S.events[i];
      if (e[1] !== 'o') continue;
      var nl = Math.max(e[2].lastIndexOf('\n'), e[2].lastIndexOf('\r'));
      if (nl >= 0) return e[2].slice(nl + 1) + tail;
      tail = e[2] + tail;
    }
    return tail;
  }

  // promptBefore returns the most recent raw prompt line printed before at.
  function promptBefore(at) {
    var text = '';
    for (var i = Math.max(0, at - 400); i < at; i++) if (S.events[i][1] === 'o') text += S.events[i][2];
    var lines = text.split(/\r?\n/);
    for (var k = lines.length - 1; k >= 0; k--) {
      var raw = lines[k].slice(lines[k].lastIndexOf('\r') + 1);
      var vis = raw.replace(ansiRe, '');
      var m = /^(.{1,80}?[$#%>❯»] )/.exec(vis);
      if (!m) continue;
      // cut the raw line right after the visible prompt end, keeping its escape codes
      var want = m[1].length, seen = 0, j = 0;
      while (j < raw.length && seen < want) {
        var esc = raw.slice(j).match(/^\x1b(\[[0-?]*[ -/]*[@-~]|\][^\x07\x1b]*(\x07|\x1b\\)|[@-Z\\-_])/);
        if (esc) { j += esc[0].length; continue; }
        j++; seen++;
      }
      var rest = raw.slice(j).match(/^(\x1b\[[0-?]*[ -/]*[@-~])*/); // trailing color resets
      return raw.slice(0, j) + (rest ? rest[0] : '');
    }
    return '';
  }

  function closeInsertWindow() {
    if (insertWin) { insertWin.remove(); insertWin = null; }
  }

  // openInsertWindow opens the window on a tab ('command', 'output', 'marker', 'pause').
  // pos: {before, after} event indices for a selection, or {at} for a single point.
  function openInsertWindow(tab, pos, x, y) {
    closeMenus(0);
    closeInsertWindow();
    var hasSel = pos.before != null;
    var w = document.createElement('div');
    w.className = 'fw';
    w.setAttribute('role', 'dialog');
    w.setAttribute('aria-label', 'Insert');
    w.innerHTML =
      '<div class="fw-head"><span class="fw-title">Insert</span><span class="fw-where"></span>' +
      '<button type="button" class="fw-close" title="Close (Esc)" aria-label="Close">×</button></div>' +
      '<div class="fw-tabs" role="tablist">' +
      ['command', 'output', 'marker', 'pause'].map(function (t) {
        return '<button type="button" role="tab" data-tab="' + t + '">' + t[0].toUpperCase() + t.slice(1) + '</button>';
      }).join('') + '</div>' +
      '<form class="fw-body">' +
      '<section data-pane="command">' +
      '  <div class="field"><label>Prompt</label><input name="prompt" type="text" spellcheck="false"></div>' +
      '  <div class="field"><label>Command</label><input name="command" type="text" spellcheck="false" placeholder="ls -la"></div>' +
      '  <label class="block">Output</label><textarea name="cmdOutput" rows="5" spellcheck="false" placeholder="command output…"></textarea>' +
      '  <div class="field"><label>Then print</label><input name="after" type="text" spellcheck="false" placeholder="next prompt"></div>' +
      '  <p class="note fw-hint"></p>' +
      '  <div class="fw-row"><label>Key delay <input name="keyDelay" type="number" step="0.01" min="0" value="0.08"></label>' +
      '  <label>Line delay <input name="lineDelay" type="number" step="0.01" min="0" value="0.02"></label></div>' +
      '  <label class="fw-check"><input name="recordInput" type="checkbox"> also record keystrokes as input events</label>' +
      '</section>' +
      '<section data-pane="output">' +
      '  <label class="block">Text <span class="muted">(\\e[31m colors, \\r\\n; line breaks become \\r\\n)</span></label>' +
      '  <textarea name="text" rows="6" spellcheck="false" placeholder="Hello, world"></textarea>' +
      '  <div class="field"><label>Delay before (s)</label><input name="textDelay" type="number" step="0.05" min="0" value="0.1"></div>' +
      '  <label class="fw-check"><input name="perLine" type="checkbox"> one event per line</label>' +
      '</section>' +
      '<section data-pane="marker">' +
      '  <div class="field"><label>Label</label><input name="label" type="text" placeholder="chapter name"></div>' +
      '  <p class="note">Markers show as ticks on the player\'s progress bar; [ and ] jump between them.</p>' +
      '</section>' +
      '<section data-pane="pause">' +
      '  <div class="field"><label>Seconds</label><input name="seconds" type="number" step="0.1" min="0" value="1"></div>' +
      '  <div class="fw-chips">' + [0.5, 1, 2, 3, 5].map(function (s) { return '<button type="button" data-sec="' + s + '">' + s + ' s</button>'; }).join('') + '</div>' +
      '</section>' +
      '<div class="fw-foot">' +
      (hasSel ? '<div class="fw-seg" role="radiogroup"><button type="button" data-side="before">Before</button><button type="button" data-side="after">After</button></div>' : '') +
      '<span class="fw-info muted"></span>' +
      '<button type="submit" class="primary">Insert</button></div>' +
      '</form>';
    document.body.appendChild(w);
    insertWin = w;

    var form = w.querySelector('form');
    // a command or output goes before the selection by default, a marker or pause after
    // it; once Before or After is clicked, switching tabs keeps that choice
    var TAB_SIDE = { command: 'before', output: 'before', marker: 'after', pause: 'after' };
    var side = TAB_SIDE[tab] || 'after', sideChosen = false;
    var at = function () { return hasSel ? (side === 'before' ? pos.before : pos.after) : pos.at; };
    var field = function (n) { return form.elements[n]; };

    // header text: where the insert goes
    var where = function () {
      var i = at();
      var t = i > 0 ? times[i - 1] : 0;
      w.querySelector('.fw-where').textContent = (i >= S.events.length ? 'at the end' : (i === 0 ? 'at the start' : 'after #' + (i - 1))) + ' · ' + fmt(t);
    };

    // prompt detection for the command tab
    var detect = function () {
      var i = at();
      var line = rawLastLine(i);
      var hint = w.querySelector('.fw-hint');
      if (PROMPT_END.test(line.replace(ansiRe, ''))) {
        field('prompt').value = '';
        field('after').value = escapeData(line);
        hint.textContent = 'The screen already shows a prompt here, so the command is typed at it and the same prompt is printed after the output.';
      } else {
        var p = promptBefore(i);
        field('prompt').value = escapeData(p || '$ ');
        field('after').value = '';
        hint.textContent = p ? 'Prompt copied from earlier in the recording.' : 'No prompt found before this point; using "$ ".';
      }
    };

    var estimate = function () {
      var tabName = w.dataset.tab, info = '';
      if (tabName === 'command') {
        var n = field('command').value.length, lines = field('cmdOutput').value ? field('cmdOutput').value.split('\n').length : 0;
        var secs = 0.5 + n * (parseFloat(field('keyDelay').value) || 0) + 0.4 + 0.15 + lines * (parseFloat(field('lineDelay').value) || 0);
        info = n || lines ? '≈ ' + secs.toFixed(1) + ' s' : '';
      } else if (tabName === 'pause') {
        info = '+' + (parseFloat(field('seconds').value) || 0) + ' s';
      }
      w.querySelector('.fw-info').textContent = info;
    };

    var showTab = function (t) {
      w.dataset.tab = t;
      w.querySelectorAll('[data-tab]').forEach(function (b) { b.setAttribute('aria-selected', b.dataset.tab === t); });
      w.querySelectorAll('[data-pane]').forEach(function (p) { p.hidden = p.dataset.pane !== t; });
      if (hasSel && !sideChosen && TAB_SIDE[t] !== side) setSide(TAB_SIDE[t]);
      estimate();
      var first = { command: 'command', output: 'text', marker: 'label', pause: 'seconds' }[t];
      setTimeout(function () { field(first).focus(); field(first).select && field(first).select(); }, 0);
    };

    var setSide = function (s) {
      side = s;
      w.querySelectorAll('[data-side]').forEach(function (b) { b.setAttribute('aria-checked', b.dataset.side === s); });
      where();
      detect();
    };

    w.querySelectorAll('[data-tab]').forEach(function (b) { b.onclick = function () { showTab(b.dataset.tab); }; });
    w.querySelectorAll('[data-side]').forEach(function (b) { b.onclick = function () { sideChosen = true; setSide(b.dataset.side); }; });
    w.querySelectorAll('[data-sec]').forEach(function (b) { b.onclick = function () { field('seconds').value = b.dataset.sec; estimate(); }; });
    w.querySelector('.fw-close').onclick = closeInsertWindow;
    form.addEventListener('input', estimate);

    form.addEventListener('submit', function (ev) {
      ev.preventDefault();
      var i = at(), t = w.dataset.tab, req;
      if (t === 'command') {
        if (!field('command').value && !field('cmdOutput').value) { flash('Enter a command and/or its output.', true); return; }
        var kd = parseFloat(field('keyDelay').value) || 0.08;
        req = {
          op: 'inject', at: i, inject: {
            prompt: unescapeData(field('prompt').value), command: field('command').value,
            output: unescapeData(field('cmdOutput').value), after: unescapeData(field('after').value),
            pre_delay: 0.5, typing_delay: kd, jitter: kd * 0.35, enter_delay: 0.4, output_delay: 0.15,
            line_delay: parseFloat(field('lineDelay').value) || 0, record_input: field('recordInput').checked,
            seed: Date.now() % 100000
          }
        };
      } else if (t === 'output') {
        var text = unescapeData(field('text').value);
        if (!text) { flash('Enter text to insert.', true); return; }
        var d = parseFloat(field('textDelay').value) || 0;
        var parts = field('perLine').checked ? text.split(/(?<=\n)/) : [text];
        req = { op: 'insert', at: i, events: parts.map(function (p, k) { return [k === 0 ? d : 0.05, 'o', p]; }) };
      } else if (t === 'marker') {
        req = { op: 'marker', at: i, label: field('label').value };
      } else {
        var s = parseFloat(field('seconds').value);
        if (isNaN(s)) { flash('Enter the pause length.', true); return; }
        req = { op: 'pause', at: i, seconds: s };
      }
      closeInsertWindow();
      run(req, function (n) {
        if (n > 0) { selectRange(i, i + n); scrollToSel(); }
        else if (i < S.events.length) { sel = { a: i, b: i }; scrollToSel(); }
      });
    });

    // keyboard: Esc closes, Ctrl+Enter inserts (also from the textareas)
    w.addEventListener('keydown', function (ev) {
      if (ev.key === 'Escape') { ev.stopPropagation(); closeInsertWindow(); }
      else if (ev.key === 'Enter' && (ev.ctrlKey || ev.metaKey)) { ev.preventDefault(); form.requestSubmit(); }
      ev.stopPropagation(); // keep editor shortcuts (Delete, Space…) out of the window
    });

    // drag by the header
    var head = w.querySelector('.fw-head');
    head.addEventListener('pointerdown', function (ev) {
      if (ev.target.closest('button')) return;
      var r = w.getBoundingClientRect(), dx = ev.clientX - r.left, dy = ev.clientY - r.top;
      head.setPointerCapture(ev.pointerId);
      var move = function (e) { place(e.clientX - dx, e.clientY - dy); };
      head.addEventListener('pointermove', move);
      head.addEventListener('pointerup', function up() {
        head.removeEventListener('pointermove', move);
        head.removeEventListener('pointerup', up);
      });
    });
    var place = function (px, py) {
      w.style.left = Math.min(Math.max(4, px), innerWidth - w.offsetWidth - 4) + 'px';
      w.style.top = Math.min(Math.max(4, py), innerHeight - 40) + 'px';
    };

    setSide(side);
    showTab(tab);
    // open next to the click, kept fully on screen
    place(Math.min(x + 12, innerWidth - w.offsetWidth - 8), Math.min(y - 20, innerHeight - w.offsetHeight - 8));
  }

  // CLEAR_SCREEN is what the clear command prints: cursor home, erase the screen and the scrollback.
  var CLEAR_SCREEN = '\x1b[H\x1b[2J\x1b[3J';

  function insertClear(i) {
    run({ op: 'insert', at: i, events: [[0.1, 'o', CLEAR_SCREEN]] }, function (n) { selectRange(i, i + n); scrollToSel(); });
  }

  function insertItems(pos) {
    var open = function (tab) { return function () { openInsertWindow(tab, pos, menuPos.x, menuPos.y); }; };
    var clear = pos.before != null
      ? [{ label: 'Clear screen before', action: function () { insertClear(pos.before); } },
        { label: 'Clear screen after', action: function () { insertClear(pos.after); } }]
      : [{ label: 'Clear screen', action: function () { insertClear(pos.at); } }];
    return [
      { label: 'Command…', kbd: 'Ctrl+I', action: open('command') },
      { label: 'Output text…', action: open('output') },
      { label: 'Marker…', action: open('marker') },
      { label: 'Pause…', action: open('pause') }
    ].concat({ sep: true }, clear);
  }

  // Ctrl+I: insert window for the selection, next to its first row
  function insertAtSelection() {
    var r = range();
    var pos = r ? { before: r[0], after: r[1] } : { at: S.events.length };
    var row = r && list.querySelector('.row[data-i="' + (r[1] - 1) + '"]');
    var b = row ? row.getBoundingClientRect() : { left: innerWidth / 3, bottom: innerHeight / 3 };
    openInsertWindow('command', pos, b.left + 160, b.bottom);
  }

  // Route right-clicks to the menu for what is under the pointer.
  // Text fields keep the browser menu, so copy/paste in them works as usual.
  document.addEventListener('contextmenu', function (ev) {
    var t = ev.target;
    if (!S || t.closest('input, textarea, select, .ctx, .fw')) return;
    menuPos = { x: ev.clientX, y: ev.clientY };
    var items;
    var row = t.closest('.list .row');
    if (row) items = rowMenu(+row.dataset.i);
    else if (t === canvas) items = timelineMenu(xt(ev.offsetX, canvas.clientWidth));
    else if (t.closest('#preview') && player && player.term) items = previewMenu();
    else items = generalMenu();
    ev.preventDefault();
    openMenu(items, ev.clientX, ev.clientY, 0);
  });

  // Context-menu key / Shift+F10 opens the menu for the selection.
  function keyboardMenu() {
    var r = range();
    if (!r) { openMenu(generalMenu(), innerWidth / 3, innerHeight / 3, 0); }
    else {
      scrollToSel();
      var row = list.querySelector('.row[data-i="' + r[0] + '"]');
      var b = row ? row.getBoundingClientRect() : list.getBoundingClientRect();
      menuPos = { x: b.left + 120, y: b.bottom };
      openMenu(rowMenu(r[0]), menuPos.x, menuPos.y, 0);
    }
    var first = menuButtons(menus[0])[0];
    if (first) first.focus();
  }

  // ------------------------------------------------------------ wiring

  document.querySelectorAll('[data-op]').forEach(function (b) {
    b.addEventListener('click', function () { ops[b.dataset.op](); });
  });

  function undo() { api('api/undo', {}).then(function (s) { setState(s, true); }); }
  function redo() { api('api/redo', {}).then(function (s) { setState(s, true); }); }
  function save() { api('api/save', {}).then(function (s) { setState(s, true); }).catch(function (e) { flash(e.message, true); }); }
  $('btn-undo').onclick = undo;
  $('btn-redo').onclick = redo;
  $('btn-save').onclick = save;
  initTheme();

  document.addEventListener('keydown', function (ev) {
    var tag = ev.target.tagName;
    var inField = tag === 'INPUT' || tag === 'TEXTAREA' || tag === 'SELECT';
    var mod = ev.ctrlKey || ev.metaKey;
    if (mod && ev.key.toLowerCase() === 's') { ev.preventDefault(); save(); return; }
    if (mod && ev.key.toLowerCase() === 'f') {
      ev.preventDefault();
      $('find-panel').open = true;
      $('in-find').focus();
      $('in-find').select();
      return;
    }
    if (inField || ev.target.closest('.gotake, .ctx')) return;
    var n = S ? S.events.length : 0;
    var k = ev.key;
    if (mod && k.toLowerCase() === 'z') { ev.preventDefault(); ev.shiftKey ? redo() : undo(); }
    else if (mod && k.toLowerCase() === 'y') { ev.preventDefault(); redo(); }
    else if (mod && k.toLowerCase() === 'a') { ev.preventDefault(); if (n) sel = { a: 0, b: n - 1 }; renderAll(); }
    else if (mod && k.toLowerCase() === 'd') { ev.preventDefault(); ops.duplicate(); }
    else if (mod && k.toLowerCase() === 'i') { ev.preventDefault(); insertAtSelection(); }
    else if (mod && k.toLowerCase() === 'c' && !String(getSelection())) { ev.preventDefault(); copySel(false); }
    else if (mod && k.toLowerCase() === 'x') { ev.preventDefault(); copySel(true); }
    else if (mod && k.toLowerCase() === 'v') { ev.preventDefault(); var pr = range(); paste(pr ? pr[1] : S.events.length); }
    else if (k === 'ContextMenu' || (ev.shiftKey && k === 'F10')) { ev.preventDefault(); keyboardMenu(); }
    else if (k === 'Delete' || k === 'Backspace') { ev.preventDefault(); ops.delete(); }
    else if (k === 'Escape') { sel = null; renderAll(); }
    else if (k === ' ') { ev.preventDefault(); stopAt = null; if (player) player.toggle(); }
    else if ((k === 'ArrowDown' || k === 'ArrowUp' || k === 'PageDown' || k === 'PageUp' || k === 'Home' || k === 'End') && n) {
      ev.preventDefault();
      var page = Math.max(1, Math.floor(list.clientHeight / ROW) - 1);
      var d = { ArrowDown: 1, ArrowUp: -1, PageDown: page, PageUp: -page, Home: -n, End: n }[k];
      if (!sel) sel = { a: 0, b: 0 };
      else {
        var b = Math.min(n - 1, Math.max(0, sel.b + d));
        sel = ev.shiftKey ? { a: sel.a, b: b } : { a: b, b: b };
      }
      if (player && player.term && !player.playing) player.seek(times[sel.b]);
      scrollToSel();
      renderAll();
    }
  });

  window.addEventListener('beforeunload', function (ev) {
    if (S && S.dirty) { ev.preventDefault(); ev.returnValue = ''; }
  });

  api('api/state').then(function (s) { setState(s); }).catch(function (e) { flash(e.message, true); });
})();
