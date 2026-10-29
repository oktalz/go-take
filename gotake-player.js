/*!
 * gotake-player — embeddable player for gotake (.take) and asciicast (v2/v3) terminal recordings.
 *
 * Usage:
 *   <script src="gotake-player.js"></script>
 *   <div data-gotake-src="demo.take" data-autoplay data-loop></div>
 *
 * or programmatically:
 *   const p = await GoTakePlayer.create(element, 'demo.take', { autoplay: true, speed: 1.5 });
 *   p.on('ended', () => ...); p.seek(12.5); p.play();
 *
 * Terminal emulation is done by xterm.js. If `window.Terminal` is not present
 * it is loaded from a CDN (override with the `xtermBase` option).
 *
 * https://github.com/oktalz/go-take
 * Copyright (c) 2026 Zlatko Bratkovic
 * @license MIT
 */
(function (global) {
  'use strict';

  var XTERM_BASE = 'https://cdn.jsdelivr.net/npm/@xterm/xterm@5.5.0';

  var LOGO = [
    '              _        _',
    '   __ _  ___ | |_ __ _| | _____',
    '  / _` |/ _ \\| __/ _` | |/ / _ \\',
    ' | (_| | (_) | || (_| |   <  __/',
    '  \\__, |\\___/ \\__\\__,_|_|\\_\\___|',
    '  |___/'
  ].join('\r\n') + '\r\n';

  var CSS = [
    '.gotake{--gotake-bg:#121314;--gotake-fg:#d6d6d6;--gotake-accent:#e5a50a;--gotake-bar:rgba(18,19,20,.92);',
    'position:relative;display:block;max-width:100%;border-radius:8px;overflow:hidden;background:var(--gotake-bg);',
    'font:13px/1.2 system-ui,-apple-system,"Segoe UI",sans-serif;color:var(--gotake-fg);outline:none;user-select:none}',
    '.gotake:focus-visible{box-shadow:0 0 0 2px var(--gotake-accent)}',
    '.gotake-viewport{position:relative;overflow:hidden;padding:10px 12px}',
    '.gotake-scaler{transform-origin:0 0;width:max-content}',
    '.gotake .xterm-screen canvas.gotake-kitty{pointer-events:none}',
    '.gotake .xterm .xterm-viewport{overflow:hidden!important;background:transparent!important}',
    '.gotake .xterm-cursor-layer, .gotake .xterm-helpers{pointer-events:none}',
    '.gotake-overlay{position:absolute;inset:0 0 38px 0;display:flex;align-items:center;justify-content:center;cursor:pointer}',
    '.gotake-overlay[hidden]{display:none}',
    '.gotake-caption{position:absolute;left:0;right:0;bottom:18px;margin:0 auto;width:fit-content;max-width:88%;',
    'padding:.45em .8em;border-radius:6px;background:var(--gotake-caption-bg,#373839);color:var(--gotake-caption-fg,#fff);',
    'text-align:center;',
    'font:500 15px/1.35 system-ui,-apple-system,"Segoe UI",Cantarell,Ubuntu,sans-serif;white-space:pre-wrap;pointer-events:none}',
    '.gotake-caption[hidden]{display:none}',
    '.gotake-big{width:64px;height:64px;border-radius:50%;background:rgba(0,0,0,.55);display:flex;align-items:center;justify-content:center;',
    'transition:transform .15s,background .15s}',
    '.gotake-overlay:hover .gotake-big{transform:scale(1.08);background:rgba(0,0,0,.75)}',
    '.gotake-big svg{width:30px;height:30px;fill:#fff;margin-left:4px}',
    '.gotake-bar{display:flex;align-items:center;gap:8px;height:38px;padding:0 10px;background:var(--gotake-bar);border-top:1px solid rgba(255,255,255,.06)}',
    '.gotake-btn{all:unset;cursor:pointer;display:flex;align-items:center;justify-content:center;min-width:26px;height:26px;border-radius:4px;color:var(--gotake-fg);',
    'font:600 12px/1 ui-monospace,monospace}',
    '.gotake-btn:hover{background:rgba(255,255,255,.08)}',
    '.gotake-btn:focus-visible{box-shadow:0 0 0 2px var(--gotake-accent)}',
    '.gotake-btn svg{width:16px;height:16px;fill:currentColor;pointer-events:none}',
    '.gotake-time{font:12px/1 ui-monospace,monospace;opacity:.8;white-space:nowrap;font-variant-numeric:tabular-nums}',
    '.gotake-progress{position:relative;flex:1;height:22px;cursor:pointer;display:flex;align-items:center}',
    '.gotake-track{position:relative;width:100%;height:4px;border-radius:2px;background:rgba(255,255,255,.15)}',
    '.gotake-fill{position:absolute;left:0;top:0;bottom:0;border-radius:2px;background:var(--gotake-accent)}',
    '.gotake-knob{position:absolute;top:50%;width:12px;height:12px;margin:-6px 0 0 -6px;border-radius:50%;background:var(--gotake-accent);',
    'transform:scale(0);transition:transform .1s}',
    '.gotake-progress:hover .gotake-knob{transform:scale(1)}',
    '.gotake-marker{position:absolute;top:-2px;width:3px;height:8px;margin-left:-1px;background:#fff;opacity:.7;border-radius:1px}',
    '.gotake-tip{position:absolute;bottom:22px;transform:translateX(-50%);background:#000;color:#fff;padding:3px 6px;border-radius:4px;',
    'font:11px/1.2 ui-monospace,monospace;white-space:nowrap;pointer-events:none}',
    '.gotake-tip[hidden]{display:none}',
    '.gotake:fullscreen{display:flex;flex-direction:column;justify-content:center;border-radius:0}',
    '.gotake:fullscreen .gotake-viewport{flex:1;display:flex;align-items:center;justify-content:center}',
    '.gotake-contain{display:flex;flex-direction:column;height:100%}',
    '.gotake-contain .gotake-viewport{flex:1;min-height:0;display:flex;align-items:center;justify-content:center}',
    '.gotake-contain .gotake-bar{flex:none}',
    '.gotake-error{padding:16px;color:#ff7b72;font:13px ui-monospace,monospace}'
  ].join('');

  var ICONS = {
    play: '<svg viewBox="0 0 24 24"><path d="M7 4.5v15a1 1 0 0 0 1.5.86l12.5-7.5a1 1 0 0 0 0-1.72L8.5 3.64A1 1 0 0 0 7 4.5z"/></svg>',
    pause: '<svg viewBox="0 0 24 24"><path d="M6 4h4v16H6zM14 4h4v16h-4z"/></svg>',
    replay: '<svg viewBox="0 0 24 24"><path d="M12 5V1L7 6l5 5V7a6 6 0 1 1-6 6H4a8 8 0 1 0 8-8z"/></svg>',
    full: '<svg viewBox="0 0 24 24"><path d="M4 4h6v2H6v4H4zm10 0h6v6h-2V6h-4zM4 14h2v4h4v2H4zm14 0h2v6h-6v-2h4z"/></svg>'
  };

  var DEFAULT_THEME = {
    background: '#121314', foreground: '#d6d6d6', cursor: '#d6d6d6',
    black: '#1b1d1e', red: '#e0525c', green: '#7fb85e', yellow: '#e5a50a',
    blue: '#5b9bd5', magenta: '#c678dd', cyan: '#4fb6be', white: '#d6d6d6',
    brightBlack: '#5c6370', brightRed: '#ff6e76', brightGreen: '#a2d977', brightYellow: '#ffcb4b',
    brightBlue: '#7cb7ff', brightMagenta: '#dd99f0', brightCyan: '#6fdbe3', brightWhite: '#ffffff'
  };
  var PALETTE_KEYS = ['black', 'red', 'green', 'yellow', 'blue', 'magenta', 'cyan', 'white',
    'brightBlack', 'brightRed', 'brightGreen', 'brightYellow', 'brightBlue', 'brightMagenta', 'brightCyan', 'brightWhite'];

  // ---------------------------------------------------------------- parsing

  /**
   * Parse asciicast v2/v3 text, or normalize an already parsed object
   * ({header, events: [[delay, code, data], ...]}), into
   * {header, events: [{delay, code, data}]} with v3 semantics (relative delays).
   */
  function parseCast(src) {
    if (typeof src === 'object' && src !== null) {
      return {
        header: src.header || {},
        events: (src.events || []).map(function (e) {
          return Array.isArray(e) ? { delay: +e[0] || 0, code: e[1], data: e[2] == null ? '' : String(e[2]) } : e;
        })
      };
    }
    if (/^(\s*(#[^\n]*)?\n)*\s*gotake(\s|$)/.test(String(src))) return parseTake(String(src));
    var lines = String(src).split('\n');
    var header = null, version = 0, prev = 0, events = [];
    for (var i = 0; i < lines.length; i++) {
      var line = lines[i].trim();
      if (!line || line[0] === '#') continue;
      var obj;
      try { obj = JSON.parse(line); } catch (err) { throw new Error('line ' + (i + 1) + ': ' + err.message); }
      if (!header) {
        version = obj.version;
        if (version === 2) {
          header = {
            version: 3, term: { cols: obj.width, rows: obj.height, theme: obj.theme },
            idle_time_limit: obj.idle_time_limit, title: obj.title, command: obj.command, timestamp: obj.timestamp
          };
        } else if (version === 3) {
          header = obj;
        } else {
          throw new Error('unsupported asciicast version: ' + version);
        }
        continue;
      }
      var d = +obj[0] || 0;
      if (version === 2) { var abs = d; d = Math.max(0, abs - prev); prev = abs; }
      events.push({ delay: Math.max(0, d), code: obj[1], data: obj[2] == null ? '' : String(obj[2]) });
    }
    if (!header) throw new Error('empty recording');
    return { header: header, events: events };
  }

  var TAKE_CODES = { out: 'o', 'in': 'i', resize: 'r', marker: 'm', exit: 'x', caption: 'c' };
  var TAKE_ESC = { e: '\x1b', r: '\r', n: '\n', t: '\t', '\\': '\\', '"': '"' };

  /** Split a .take line into words: bare words and quoted strings, up to a # comment. */
  function takeWords(line) {
    var out = [], i = 0;
    while (i < line.length) {
      var c = line[i];
      if (c === ' ' || c === '\t' || c === '\r') { i++; continue; }
      if (c === '#') break;
      if (c === "'") { // raw string, no escapes
        var end = line.indexOf("'", i + 1);
        if (end < 0) throw new Error('unterminated string');
        out.push(line.slice(i + 1, end)); i = end + 1; continue;
      }
      if (c !== '"') {
        var j = i;
        while (j < line.length && !/[ \t\r]/.test(line[j])) j++;
        out.push(line.slice(i, j)); i = j; continue;
      }
      var s = '';
      for (i++; ; i++) {
        if (i >= line.length) throw new Error('unterminated string');
        c = line[i];
        if (c === '"') { i++; break; }
        if (c !== '\\') { s += c; continue; }
        c = line[++i];
        if (TAKE_ESC[c] != null) { s += TAKE_ESC[c]; continue; }
        var n = { x: 2, u: 4, U: 8 }[c], v = n && parseInt(line.substr(i + 1, n), 16);
        if (!n || isNaN(v)) throw new Error('bad escape \\' + c);
        s += c === 'x' && v >= 0x80 ? '�' : String.fromCodePoint(v);
        i += n;
      }
      out.push(s);
    }
    return out;
  }

  function takeDelay(s) {
    var m = /^([0-9.eE+]+)(ms|s)?$/.exec(s || ''), d = m ? parseFloat(m[1]) : NaN;
    if (isNaN(d) || d < 0) throw new Error('bad delay ' + JSON.stringify(s));
    return m[2] === 'ms' ? d / 1000 : d;
  }

  /** Parse gotake's .take format (see cast/take.go) into {header, events}. */
  function parseTake(src) {
    var lines = src.split('\n'), header = null, events = [], wait = 0;
    var push = function (e) { e.delay += wait; wait = 0; events.push(e); };
    for (var n = 0; n < lines.length; n++) {
      try {
        var w = takeWords(lines[n]), k = w[0], a = w.slice(1);
        if (!w.length) continue;
        if (!header) {
          if (k !== 'gotake' || a[0] !== '1') throw new Error('unsupported .take version');
          header = { version: 3, term: { cols: 80, rows: 24 } };
          continue;
        }
        switch (k) {
          case 'term':
            var sz = /^(\d+)x(\d+)$/.exec(a[0] || '');
            if (!sz) throw new Error('bad size');
            header.term.cols = +sz[1]; header.term.rows = +sz[2];
            if (a[1]) header.term.type = a[1];
            break;
          case 'theme': header.term.theme = { fg: a[0], bg: a[1], palette: a[2] }; break;
          case 'cell':
            var cell = /^(\d+)x(\d+)$/.exec(a[0] || '');
            if (!cell) throw new Error('bad cell size');
            header.term.cell_width = +cell[1]; header.term.cell_height = +cell[2];
            break;
          case 'title': header.title = a[0]; break;
          case 'command': header.command = a[0]; break;
          case 'timestamp': header.timestamp = +a[0]; break;
          case 'idle': header.idle_time_limit = takeDelay(a[0]); break;
          case 'term-version': case 'env': case 'tag': break;
          case 'wait': wait += takeDelay(a[0]); break;
          case 'event': push({ delay: takeDelay(a[0]), code: a[1], data: a[2] || '' }); break;
          case 'type':
            var chars = Array.from(a[1] || ''), every = 0.08, ds = [];
            if (a.length === 3 && a[2].indexOf('every=') === 0) every = takeDelay(a[2].slice(6));
            else if (a.length > 2) {
              ds = a.slice(2).map(takeDelay);
              every = ds.reduce(function (x, y) { return x + y; }, 0) / ds.length;
            }
            chars.forEach(function (ch, i) {
              push({ delay: i === 0 ? takeDelay(a[0]) : i <= ds.length ? ds[i - 1] : every, code: 'o', data: ch });
            });
            break;
          default:
            if (!TAKE_CODES[k]) throw new Error('unknown keyword ' + k);
            push({ delay: takeDelay(a[0]), code: TAKE_CODES[k], data: a[1] == null ? '' : a[1] });
        }
      } catch (err) {
        throw new Error('line ' + (n + 1) + ': ' + err.message);
      }
    }
    if (!header) throw new Error('empty recording');
    return { header: header, events: events };
  }

  function toXtermTheme(t) {
    if (!t) return null;
    // bg "transparent" (gotake's own value) leaves the page behind the terminal visible
    var bg = String(t.bg).toLowerCase() === 'transparent' ? '#00000000' : t.bg;
    var out = { background: bg, foreground: t.fg, cursor: t.fg };
    if (t.palette) {
      t.palette.split(':').forEach(function (c, i) {
        if (i < 16) out[PALETTE_KEYS[i]] = c;
        if (i < 8 && t.palette.split(':').length === 8) out[PALETTE_KEYS[i + 8]] = c;
      });
    }
    return out;
  }

  // captionColors returns caption [text, box] colors for an xterm theme, as the
  // exporter picks them: the background a fifth of the way to the text color,
  // and white or black text, whichever stands out. null for colors it cannot read.
  function captionColors(theme) {
    var rgb = function (c) {
      var m = /^#([0-9a-f]{2})([0-9a-f]{2})([0-9a-f]{2})$/i.exec(c || '');
      return m && [parseInt(m[1], 16), parseInt(m[2], 16), parseInt(m[3], 16)];
    };
    var bg = rgb(theme.background), fg = rgb(theme.foreground);
    if (!bg || !fg) return null;
    var box = bg.map(function (v, i) { return Math.floor((v * 4 + fg[i] + 2) / 5); });
    var light = 299 * box[0] + 587 * box[1] + 114 * box[2] > 128 * 1000;
    return [light ? '#000' : '#fff', 'rgb(' + box.join(',') + ')'];
  }

  function fmtTime(s) {
    s = Math.max(0, Math.floor(s));
    var h = Math.floor(s / 3600), m = Math.floor(s / 60) % 60, sec = s % 60;
    var mm = h ? String(m).padStart(2, '0') : String(m);
    return (h ? h + ':' : '') + mm + ':' + String(sec).padStart(2, '0');
  }

  var xtermPromise = null;
  function loadXterm(base) {
    if (global.Terminal) return Promise.resolve(global.Terminal);
    if (xtermPromise) return xtermPromise;
    xtermPromise = new Promise(function (resolve, reject) {
      var link = document.createElement('link');
      link.rel = 'stylesheet';
      link.href = base + '/css/xterm.css';
      document.head.appendChild(link);
      var s = document.createElement('script');
      s.src = base + '/lib/xterm.js';
      s.onload = function () { global.Terminal ? resolve(global.Terminal) : reject(new Error('xterm.js did not register window.Terminal')); };
      s.onerror = function () { xtermPromise = null; reject(new Error('failed to load ' + s.src)); };
      document.head.appendChild(s);
    });
    return xtermPromise;
  }

  var cssInjected = false;
  function injectCSS() {
    if (cssInjected) return;
    cssInjected = true;
    var st = document.createElement('style');
    st.textContent = CSS;
    document.head.appendChild(st);
  }

  function el(tag, cls, html) {
    var e = document.createElement(tag);
    if (cls) e.className = cls;
    if (html != null) e.innerHTML = html;
    return e;
  }

  // ---------------------------------------------------------------- kitty graphics

  // Kitty graphics protocol: https://sw.kovidgoyal.net/kitty/graphics-protocol/
  // (and Sixel and iTerm2 inline images, shown the same way as Kitty's direct placements)
  //
  // Images come in APC sequences (ESC _ G keys ; base64 ESC \) and are shown at
  // the cursor (direct placement) or through Unicode placeholders: U+10EEEE plus
  // combining marks for the row and column of the image piece, with the image id
  // as the foreground color. xterm.js knows neither, so, as gotake's exporter does,
  // graphics commands are taken out of the output and every image cell becomes one
  // character of Supplementary Private Use Area-B holding its row and column,
  // colored with the id of what it shows. xterm.js keeps, scrolls and clears those
  // cells like any text; a canvas over the terminal draws the image pieces on them.

  var KITTY_PLACEHOLDER = 0x10EEEE;
  var IMAGE_CELL = 0x100000;      // image cell: IMAGE_CELL + row * 256 + col
  var IMAGE_CELL_MAX = 254;       // largest row and column an image cell holds
  var ASSUMED_CELL = [8, 16];     // cell size in pixels when the recording does not say
  var DIRECT_IDS = 0xffffff;      // colors of direct placements count down from here
  var NUMBERED_IDS = 0x80000000;  // ids of images sent with a number only
  var KITTY_OSC = 5379;           // private OSC that runs a command where it was in the output
  var MAX_PENDING = 256 << 20;
  var DIACRITICS = [
    0x305, 0x30D, 0x30E, 0x310, 0x312, 0x33D, 0x33E, 0x33F, 0x346, 0x34A, 0x34B, 0x34C, 0x350, 0x351, 0x352, 0x357,
    0x35B, 0x363, 0x364, 0x365, 0x366, 0x367, 0x368, 0x369, 0x36A, 0x36B, 0x36C, 0x36D, 0x36E, 0x36F, 0x483, 0x484,
    0x485, 0x486, 0x487, 0x592, 0x593, 0x594, 0x595, 0x597, 0x598, 0x599, 0x59C, 0x59D, 0x59E, 0x59F, 0x5A0, 0x5A1,
    0x5A8, 0x5A9, 0x5AB, 0x5AC, 0x5AF, 0x5C4, 0x610, 0x611, 0x612, 0x613, 0x614, 0x615, 0x616, 0x617, 0x657, 0x658,
    0x659, 0x65A, 0x65B, 0x65D, 0x65E, 0x6D6, 0x6D7, 0x6D8, 0x6D9, 0x6DA, 0x6DB, 0x6DC, 0x6DF, 0x6E0, 0x6E1, 0x6E2,
    0x6E4, 0x6E7, 0x6E8, 0x6EB, 0x6EC, 0x730, 0x732, 0x733, 0x735, 0x736, 0x73A, 0x73D, 0x73F, 0x740, 0x741, 0x743,
    0x745, 0x747, 0x749, 0x74A, 0x7EB, 0x7EC, 0x7ED, 0x7EE, 0x7EF, 0x7F0, 0x7F1, 0x7F3, 0x816, 0x817, 0x818, 0x819,
    0x81B, 0x81C, 0x81D, 0x81E, 0x81F, 0x820, 0x821, 0x822, 0x823, 0x825, 0x826, 0x827, 0x829, 0x82A, 0x82B, 0x82C,
    0x82D, 0x951, 0x953, 0x954, 0xF82, 0xF83, 0xF86, 0xF87, 0x135D, 0x135E, 0x135F, 0x17DD, 0x193A, 0x1A17, 0x1A75,
    0x1A76, 0x1A77, 0x1A78, 0x1A79, 0x1A7A, 0x1A7B, 0x1A7C, 0x1B6B, 0x1B6D, 0x1B6E, 0x1B6F, 0x1B70, 0x1B71, 0x1B72,
    0x1B73, 0x1CD0, 0x1CD1, 0x1CD2, 0x1CDA, 0x1CDB, 0x1CE0, 0x1DC0, 0x1DC1, 0x1DC3, 0x1DC4, 0x1DC5, 0x1DC6, 0x1DC7,
    0x1DC8, 0x1DC9, 0x1DCB, 0x1DCC, 0x1DD1, 0x1DD2, 0x1DD3, 0x1DD4, 0x1DD5, 0x1DD6, 0x1DD7, 0x1DD8, 0x1DD9, 0x1DDA,
    0x1DDB, 0x1DDC, 0x1DDD, 0x1DDE, 0x1DDF, 0x1DE0, 0x1DE1, 0x1DE2, 0x1DE3, 0x1DE4, 0x1DE5, 0x1DE6, 0x1DFE, 0x20D0,
    0x20D1, 0x20D4, 0x20D5, 0x20D6, 0x20D7, 0x20DB, 0x20DC, 0x20E1, 0x20E7, 0x20E9, 0x20F0, 0x2CEF, 0x2CF0, 0x2CF1,
    0x2DE0, 0x2DE1, 0x2DE2, 0x2DE3, 0x2DE4, 0x2DE5, 0x2DE6, 0x2DE7, 0x2DE8, 0x2DE9, 0x2DEA, 0x2DEB, 0x2DEC, 0x2DED,
    0x2DEE, 0x2DEF, 0x2DF0, 0x2DF1, 0x2DF2, 0x2DF3, 0x2DF4, 0x2DF5, 0x2DF6, 0x2DF7, 0x2DF8, 0x2DF9, 0x2DFA, 0x2DFB,
    0x2DFC, 0x2DFD, 0x2DFE, 0x2DFF, 0xA66F, 0xA67C, 0xA67D, 0xA6F0, 0xA6F1, 0xA8E0, 0xA8E1, 0xA8E2, 0xA8E3, 0xA8E4,
    0xA8E5, 0xA8E6, 0xA8E7, 0xA8E8, 0xA8E9, 0xA8EA, 0xA8EB, 0xA8EC, 0xA8ED, 0xA8EE, 0xA8EF, 0xA8F0, 0xA8F1, 0xAAB0,
    0xAAB2, 0xAAB3, 0xAAB7, 0xAAB8, 0xAABE, 0xAABF, 0xAAC1, 0xFE20, 0xFE21, 0xFE22, 0xFE23, 0xFE24, 0xFE25, 0xFE26,
    0x10A0F, 0x10A38, 0x1D185, 0x1D186, 0x1D187, 0x1D188, 0x1D189, 0x1D1AA, 0x1D1AB, 0x1D1AC, 0x1D1AD, 0x1D242,
    0x1D243, 0x1D244
  ];
  var DIACRITIC_INDEX = {};
  DIACRITICS.forEach(function (r, i) { DIACRITIC_INDEX[r] = i; });

  function imageCell(row, col) { return String.fromCodePoint(IMAGE_CELL + row * 256 + col); }

  function parseKittyCmd(s) {
    var semi = s.indexOf(';');
    var head = semi < 0 ? s : s.slice(0, semi);
    var c = { keys: {}, payload: semi < 0 ? '' : s.slice(semi + 1) };
    head.split(',').forEach(function (kv) {
      var eq = kv.indexOf('=');
      if (eq === 1) c.keys[kv[0]] = kv.slice(2);
    });
    return c;
  }
  function kchar(c, k, def) { return c.keys[k] ? c.keys[k][0] : def; }
  function knum(c, k) { return parseInt(c.keys[k], 10) || 0; }

  function base64Bytes(s) {
    var bin = atob(s);
    var out = new Uint8Array(bin.length);
    for (var i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i);
    return out;
  }

  function inflate(bytes) {
    var stream = new Blob([bytes]).stream().pipeThrough(new DecompressionStream('deflate'));
    return new Response(stream).arrayBuffer().then(function (b) { return new Uint8Array(b); });
  }

  // decodeImage turns a transmission into something canvas can draw.
  function decodeImage(c, bytes) {
    var f = knum(c, 'f') || 32, w = knum(c, 's'), h = knum(c, 'v');
    var data = kchar(c, 'o', '') === 'z' ? inflate(bytes) : Promise.resolve(bytes);
    return data.then(function (d) {
      if (f === 100) return createImageBitmap(new Blob([d], { type: 'image/png' }));
      var bpp = f === 24 ? 3 : 4;
      if (w <= 0 || h <= 0 || d.length < w * h * bpp) throw new Error('short image data');
      var px = new Uint8ClampedArray(w * h * 4);
      for (var i = 0; i < w * h; i++) {
        px[i * 4] = d[i * bpp]; px[i * 4 + 1] = d[i * bpp + 1]; px[i * 4 + 2] = d[i * bpp + 2];
        px[i * 4 + 3] = bpp === 4 ? d[i * bpp + 3] : 255;
      }
      return createImageBitmap(new ImageData(px, w, h));
    });
  }

  // pngSize reads the size from a PNG header, so placements can be sized before it is decoded.
  function pngSize(b) {
    if (b.length < 24 || b[12] !== 0x49 || b[13] !== 0x48) return [0, 0]; // IHDR
    var be = function (i) { return ((b[i] << 24) | (b[i + 1] << 16) | (b[i + 2] << 8) | b[i + 3]) >>> 0; };
    return [be(16), be(20)];
  }

  /** Kitty graphics state of one terminal. cache keeps decoded images across rewinds. */
  function Kitty(cache, onChange) {
    this.cache = cache;
    this.onChange = onChange;
    this.seq = 0; // numbers commands run from the OSC handler; never reset
    this.cell = ASSUMED_CELL; // cell size in pixels, for images placed without a size in cells
    this.termSize = function () { return [80, 24]; }; // columns and rows, set by the player
    this.reset();
  }

  Kitty.prototype.reset = function () {
    this.images = {};   // id -> image
    this.numbers = {};  // image number (I) -> id
    this.virtual = {};  // image id -> Unicode placeholder placement
    this.direct = {};   // cell color -> direct placement
    this.deferred = {}; // seq -> command that needs the screen
    this.chunk = null;
    this.data = '';
    this.pending = '';
    this.nextDir = DIRECT_IDS;
    this.nextNum = NUMBERED_IDS;
    this.row = -1;
    this.col = 0;
    this.used = false;  // anything was placed since the reset
  };

  /** What an image cell with color id shows. */
  Kitty.prototype.placement = function (id) {
    return this.direct[id] || this.virtual[id];
  };

  /** Take graphics commands out of output, returning what xterm.js should get. */
  Kitty.prototype.filter = function (s) {
    s = this.pending + s;
    this.pending = '';
    var out = '';
    for (;;) {
      var next = nextGraphics(s);
      if (!next) break;
      out += this.text(s.slice(0, next.at));
      s = s.slice(next.at);
      if (next.kind === 'sixel') {
        var m = /^\x1bP([0-9;]*)(.?)/.exec(s);
        if (!m[2]) { this.hold(s); return out; } // not known yet
        if (m[2] !== 'q') { out += this.text(s.slice(0, 2)); s = s.slice(2); continue; } // another DCS
      }
      var intro = GFX_INTROS[next.kind], end = -1, len = 0;
      for (var i = intro.length; i < s.length; i++) {
        var ch = s[i];
        if (ch === '\x07' && next.kind === 'iterm') { end = i; len = i + 1; break; }
        if (ch !== '\x1b') continue;
        if (i + 1 < s.length) { end = s[i + 1] === '\\' ? i : -2; len = s[i + 1] === '\\' ? i + 2 : i; }
        break;
      }
      if (!len) { this.hold(s); return out; } // not ended yet
      if (end >= 0) out += this.graphics(next.kind, s.slice(intro.length, end));
      s = s.slice(len); // a command cut short by another escape is dropped
    }
    var n = heldBack(s);
    out += this.text(s.slice(0, s.length - n));
    this.pending = s.slice(s.length - n);
    return out;
  };

  Kitty.prototype.hold = function (s) {
    if (s.length <= MAX_PENDING) this.pending = s;
  };

  Kitty.prototype.graphics = function (kind, body) {
    if (kind === 'kitty') return this.command(body);
    if (kind === 'sixel') return this.sixel(body.slice(body.indexOf('q') + 1));
    return this.iterm(body);
  };

  // sixel shows a Sixel image at the cursor; the cursor goes below it.
  Kitty.prototype.sixel = function (data) {
    var px = decodeSixel(data);
    if (!px) return '';
    var img = { w: px.w, h: px.h, src: null }, onChange = this.onChange;
    createImageBitmap(new ImageData(px.rgba, px.w, px.h)).then(function (bmp) { img.src = bmp; onChange(); });
    return this.placeDirect({
      img: img, x: 0, y: 0, w: 0, h: 0, cols: Math.ceil(px.w / this.cell[0]), rows: Math.ceil(px.h / this.cell[1]),
      fit: 'top', cursor: 'below'
    });
  };

  // iterm shows an iTerm2 inline image (OSC 1337 File=) at the cursor.
  Kitty.prototype.iterm = function (body) {
    var colon = body.indexOf(':');
    if (colon < 0) return '';
    var args = {};
    body.slice(0, colon).split(';').forEach(function (kv) {
      var eq = kv.indexOf('=');
      if (eq > 0) args[kv.slice(0, eq)] = kv.slice(eq + 1);
    });
    if (args.inline !== '1') return ''; // a file download
    var bytes;
    try { bytes = base64Bytes(body.slice(colon + 1)); } catch (e) { return ''; }
    var dims = imageSize(bytes);
    if (!dims) return '';
    var w = dims[0], h = dims[1], cell = this.cell, ts = this.termSize();
    var size = function (v, total, c, own) {
      if (!v || v === 'auto') return [own, false];
      if (/px$/.test(v)) return [parseInt(v, 10) || 0, true];
      if (/%$/.test(v)) return [Math.floor(total * c * (parseInt(v, 10) || 0) / 100), true];
      return [(parseInt(v, 10) || 0) * c, true];
    };
    var pw = size(args.width, ts[0], cell[0], w), ph = size(args.height, ts[1], cell[1], h);
    var keep = args.preserveAspectRatio !== '0';
    if (keep && pw[1] && !ph[1]) ph[0] = Math.floor(pw[0] * h / Math.max(w, 1));
    else if (keep && ph[1] && !pw[1]) pw[0] = Math.floor(ph[0] * w / Math.max(h, 1));
    if (pw[0] <= 0 || ph[0] <= 0) return '';
    var img = { w: w, h: h, src: null }, onChange = this.onChange;
    createImageBitmap(new Blob([bytes])).then(function (bmp) { img.src = bmp; onChange(); }).catch(function () {});
    return this.placeDirect({
      img: img, x: 0, y: 0, w: 0, h: 0, cols: Math.min(Math.ceil(pw[0] / cell[0]), ts[0]),
      rows: Math.ceil(ph[0] / cell[1]), fit: keep ? 'top' : 'stretch', cursor: 'right'
    });
  };

  // placeDirect shows placement p at the cursor, as image cells colored with a new id.
  Kitty.prototype.placeDirect = function (p) {
    p.cols = Math.min(Math.max(p.cols, 1), IMAGE_CELL_MAX + 1);
    p.rows = Math.min(Math.max(p.rows, 1), IMAGE_CELL_MAX + 1);
    this.used = true;
    this.onChange();
    var color = this.nextDir--;
    this.direct[color] = p;
    return directCells(color, p);
  };

  // text rewrites Unicode placeholders to image cells.
  Kitty.prototype.text = function (s) {
    if (s.indexOf('\x1bc') >= 0) this.reset(); // a full reset deletes all images
    var ph = String.fromCodePoint(KITTY_PLACEHOLDER);
    var i = s.indexOf(ph);
    if (i < 0) return s;
    var out = '';
    while (i >= 0) {
      out += s.slice(0, i);
      s = s.slice(i + ph.length);
      var marks = [];
      while (marks.length < 3 && s.length) {
        var cp = s.codePointAt(0), n = DIACRITIC_INDEX[cp];
        if (n === undefined) break;
        marks.push(n);
        s = s.slice(cp > 0xffff ? 2 : 1);
      }
      // missing marks continue the placeholder to the left
      var row = this.row, col = this.col + 1;
      if (marks.length > 0 && marks[0] !== this.row) { row = marks[0]; col = 0; }
      if (marks.length > 1) col = marks[1];
      this.row = row; this.col = col;
      out += row < 0 || row > IMAGE_CELL_MAX || col > IMAGE_CELL_MAX ? ' ' : imageCell(row, col);
      i = s.indexOf(ph);
    }
    return out + s;
  };

  // command runs one graphics command, returning text that takes its place.
  Kitty.prototype.command = function (s) {
    var c = parseKittyCmd(s);
    if (this.chunk) { // continuation: only m (and q) count
      this.data += c.payload;
      if (kchar(c, 'm', '0') === '1') return '';
      c = this.chunk;
      c.payload = this.data;
      this.chunk = null;
      this.data = '';
    } else if (kchar(c, 'm', '0') === '1' && 'tTf'.indexOf(kchar(c, 'a', 't')) >= 0) {
      this.chunk = c;
      this.data = c.payload;
      return '';
    }
    var id;
    switch (kchar(c, 'a', 't')) {
      case 't':
        this.transmit(c);
        return '';
      case 'T':
        id = this.transmit(c);
        return id ? this.put(c, id) : '';
      case 'p':
        id = knum(c, 'i') || this.numbers[knum(c, 'I')];
        return id ? this.put(c, id) : '';
      case 'd':
        return this.delete(c);
      default: // queries and animation frames are not shown
        return '';
    }
  };

  // transmit stores an image, returning its id (0 when it cannot be shown).
  Kitty.prototype.transmit = function (c) {
    if (kchar(c, 't', 'd') !== 'd') return 0; // files and shared memory are not in the recording
    var key = [c.keys.f, c.keys.o, c.keys.s, c.keys.v, c.payload].join(';');
    var img = this.cache.get(key);
    if (!img) {
      var bytes;
      try { bytes = base64Bytes(c.payload); } catch (e) { return 0; }
      var raw = (knum(c, 'f') || 32) !== 100;
      var size = raw ? [knum(c, 's'), knum(c, 'v')] : kchar(c, 'o', '') === 'z' ? [0, 0] : pngSize(bytes);
      img = { w: size[0], h: size[1], src: null };
      var onChange = this.onChange;
      decodeImage(c, bytes).then(function (bmp) {
        img.src = bmp;
        img.w = bmp.width;
        img.h = bmp.height;
        onChange();
      }).catch(function () { img.failed = true; });
      this.cache.set(key, img);
    }
    var id = knum(c, 'i');
    if (!id) {
      id = this.nextNum++;
      if (knum(c, 'I')) this.numbers[knum(c, 'I')] = id;
    }
    this.images[id] = img;
    return id;
  };

  // put places image id: for Unicode placeholders (U=1) or as cells at the cursor.
  Kitty.prototype.put = function (c, id) {
    var img = this.images[id];
    if (!img) return '';
    var p = {
      img: img, x: knum(c, 'x'), y: knum(c, 'y'), w: knum(c, 'w'), h: knum(c, 'h'),
      cols: knum(c, 'c'), rows: knum(c, 'r'), imageID: id, id: knum(c, 'p'), z: knum(c, 'z'),
      fit: 'stretch'
    };
    var w = p.w || img.w - p.x, h = p.h || img.h - p.y;
    if (w > 0 && h > 0) {
      var cw = this.cell[0], ch = this.cell[1];
      if (p.cols && !p.rows) p.rows = Math.ceil(p.cols * cw * h / (w * ch));
      else if (p.rows && !p.cols) p.cols = Math.ceil(p.rows * ch * w / (h * cw));
      else if (!p.cols && !p.rows) {
        p.cols = Math.ceil(w / cw);
        p.rows = Math.ceil(h / ch);
      }
    }
    p.cols = Math.min(Math.max(p.cols, 1), IMAGE_CELL_MAX + 1);
    p.rows = Math.min(Math.max(p.rows, 1), IMAGE_CELL_MAX + 1);
    this.used = true;
    this.onChange();
    if (knum(c, 'U') === 1) {
      p.fit = 'center';
      this.virtual[id] = p;
      return '';
    }
    if (!knum(c, 'c') || !knum(c, 'r')) p.fit = 'top';
    p.cursor = knum(c, 'C') === 1 ? 'stay' : 'right';
    var color = this.nextDir--;
    this.direct[color] = p;
    return directCells(color, p);
  };

  // directCells returns the text that draws a direct placement's cells at the
  // cursor: first room is made (scrolling at the bottom), then each row is
  // written from the saved top left corner with autowrap off, so the image is
  // cut at the right edge. The cursor ends after the image's last column on its
  // last row (p.cursor 'right': Kitty, iTerm2), stays ('stay': Kitty C=1), or goes
  // to the line below it ('below': Sixel).
  function directCells(color, p) {
    var sgr = '\x1b[0;38;2;' + (color >> 16 & 255) + ';' + (color >> 8 & 255) + ';' + (color & 255) + 'm';
    var out = '';
    if (p.rows > 1) out += '\x1bD'.repeat(p.rows - 1) + '\x1b[' + (p.rows - 1) + 'A';
    out += '\x1b7\x1b[?7l';
    for (var r = 0; r < p.rows; r++) {
      out += '\x1b8' + (r ? '\x1b[' + r + 'B' : '') + sgr;
      for (var c = 0; c < p.cols; c++) out += imageCell(r, c);
    }
    out += '\x1b[?7h\x1b8';
    var down = p.rows > 1 ? '\x1b[' + (p.rows - 1) + 'B' : '';
    if (p.cursor === 'right') out += down + '\x1b[' + p.cols + 'C';
    else if (p.cursor === 'below') out += down + '\x1bD';
    return out;
  }

  var GFX_INTROS = { kitty: '\x1b_G', sixel: '\x1bP', iterm: '\x1b]1337;File=' };

  // nextGraphics returns where the next graphics command starts and its kind, or null.
  function nextGraphics(s) {
    var best = null;
    Object.keys(GFX_INTROS).forEach(function (kind) {
      var i = s.indexOf(GFX_INTROS[kind]);
      if (i >= 0 && (!best || i < best.at)) best = { at: i, kind: kind };
    });
    return best;
  }

  // heldBack returns how many characters at the end of s may start a graphics command.
  function heldBack(s) {
    var intros = Object.keys(GFX_INTROS).map(function (k) { return GFX_INTROS[k]; });
    for (var n = Math.min(s.length, GFX_INTROS.iterm.length - 1); n > 0; n--) {
      var tail = s.slice(s.length - n);
      if (intros.some(function (intro) { return intro.indexOf(tail) === 0; })) return n;
    }
    return 0;
  }

  // imageSize reads the size of a PNG, GIF or JPEG from its header, or null.
  function imageSize(b) {
    if (b[0] === 0x89 && b[1] === 0x50) { var s = pngSize(b); return s[0] ? s : null; }
    if (b[0] === 0x47 && b[1] === 0x49 && b[2] === 0x46) return [b[6] | b[7] << 8, b[8] | b[9] << 8];
    if (b[0] !== 0xff || b[1] !== 0xd8) return null;
    for (var i = 2; i + 9 < b.length;) { // JPEG: find a start of frame marker
      if (b[i] !== 0xff) return null;
      var marker = b[i + 1], len = b[i + 2] << 8 | b[i + 3];
      if (marker >= 0xc0 && marker <= 0xcf && marker !== 0xc4 && marker !== 0xc8 && marker !== 0xcc) {
        return [b[i + 7] << 8 | b[i + 8], b[i + 5] << 8 | b[i + 6]];
      }
      i += 2 + len;
    }
    return null;
  }

  var VT340 = [[0, 0, 0], [51, 51, 204], [204, 33, 33], [51, 204, 51], [204, 51, 204], [51, 204, 204], [204, 204, 51],
    [120, 120, 120], [69, 69, 69], [87, 87, 153], [153, 69, 69], [87, 153, 87], [153, 87, 153], [87, 153, 153],
    [153, 153, 87], [204, 204, 204]];
  var MAX_SIXEL = 8192;

  // sixelColor converts a color definition: Pu 2 is RGB in percent, 1 is HLS (hue 0 is blue).
  function sixelColor(pu, px, py, pz) {
    var pct = function (v) { return Math.floor(Math.min(Math.max(v, 0), 100) * 255 / 100); };
    if (pu === 2) return [pct(px), pct(py), pct(pz)];
    var h = ((px + 240) % 360) / 360, l = py / 100, s = pz / 100;
    var q = l < 0.5 ? l * (1 + s) : l + s - l * s, p = 2 * l - q;
    var hue = function (t) {
      t -= Math.floor(t);
      return t < 1 / 6 ? p + (q - p) * 6 * t : t < 0.5 ? q : t < 2 / 3 ? p + (q - p) * (2 / 3 - t) * 6 : p;
    };
    var to8 = function (v) { return Math.round(Math.min(Math.max(v, 0), 1) * 255); };
    return [to8(hue(h + 1 / 3)), to8(hue(h)), to8(hue(h - 1 / 3))];
  }

  // decodeSixel decodes Sixel data (after the q) to {w, h, rgba}, or null.
  // Pixels it does not set stay transparent. As export/inline.go does.
  function decodeSixel(data) {
    var palette = VT340.slice(), rows = [], x = 0, y = 0, cur = 0, declW = 0, declH = 0, maxW = 0, i = 0;
    var set = function (x, y, c) {
      if (x >= MAX_SIXEL || y >= MAX_SIXEL) return;
      while (rows.length <= y) rows.push([]);
      rows[y][x] = c;
      if (x + 1 > maxW) maxW = x + 1;
    };
    var sixel = function (bits) {
      for (var b = 0; b < 6; b++) if (bits & (1 << b)) set(x, y + b, palette[cur] || [0, 0, 0]);
    };
    var params = function () {
      var ps = [], m = /^[0-9;]*/.exec(data.slice(i))[0];
      i += m.length;
      if (m) m.split(';').forEach(function (v) { ps.push(Math.min(parseInt(v, 10) || 0, 1 << 20)); });
      return ps;
    };
    while (i < data.length) {
      var c = data[i++], code = c.charCodeAt(0), ps;
      if (c === '"') {
        ps = params();
        if (ps.length >= 4) { declW = Math.min(ps[2], MAX_SIXEL); declH = Math.min(ps[3], MAX_SIXEL); }
      } else if (c === '#') {
        ps = params();
        if (!ps.length || ps[0] > 255) continue;
        cur = ps[0];
        if (ps.length >= 5) palette[cur] = sixelColor(ps[1], ps[2], ps[3], ps[4]);
      } else if (c === '!') {
        ps = params();
        var rc = i < data.length ? data.charCodeAt(i) : 0;
        if (ps.length && rc >= 63 && rc <= 126) {
          i++;
          for (var k = 0; k < Math.min(ps[0], MAX_SIXEL); k++) { sixel(rc - 63); x++; }
        }
      } else if (c === '$') x = 0;
      else if (c === '-') { x = 0; y += 6; } else if (code >= 63 && code <= 126) { sixel(code - 63); x++; }
    }
    var w = Math.max(maxW, declW), h = Math.max(rows.length, declH);
    if (!w || !h) return null;
    var rgba = new Uint8ClampedArray(w * h * 4);
    rows.forEach(function (row, ry) {
      row.forEach(function (col, rx) {
        if (!col) return;
        var o = (ry * w + rx) * 4;
        rgba[o] = col[0]; rgba[o + 1] = col[1]; rgba[o + 2] = col[2]; rgba[o + 3] = 255;
      });
    });
    return { w: w, h: h, rgba: rgba };
  }

  // delete removes placements (lowercase d) and also the image data (uppercase d).
  // Deletes by position run later, from the OSC handler, when the screen is there.
  Kitty.prototype.delete = function (c) {
    var d = kchar(c, 'd', 'a');
    if ('cCpPqQxXyY'.indexOf(d) >= 0) {
      var seq = ++this.seq;
      this.deferred[seq] = c;
      return '\x1b]' + KITTY_OSC + ';' + seq + '\x07';
    }
    var lower = d.toLowerCase(), pid = knum(c, 'p'), self = this;
    var byImage = function (id) {
      return function (p) { return p.imageID === id && (!pid || p.id === pid); };
    };
    var match;
    switch (lower) {
      case 'a': match = function () { return true; }; break;
      case 'i': match = byImage(knum(c, 'i')); break;
      case 'n': match = byImage(this.numbers[knum(c, 'I')]); break;
      case 'r': match = function (p) { return p.imageID >= knum(c, 'x') && p.imageID <= knum(c, 'y'); }; break;
      case 'z': match = function (p) { return p.z === knum(c, 'z'); }; break;
      default: return '';
    }
    var removed = [];
    var sweep = function (m) {
      Object.keys(m).forEach(function (k) {
        if (match(m[k])) { removed.push(m[k]); delete m[k]; }
      });
    };
    sweep(this.direct);
    if (lower !== 'a') sweep(this.virtual); // deleting all visible placements leaves the virtual ones
    if (d !== lower) {
      removed.forEach(function (p) { delete self.images[p.imageID]; });
      if (d === 'I') delete this.images[knum(c, 'i')];
      if (d === 'N') delete this.images[this.numbers[knum(c, 'I')]];
    }
    this.onChange();
    return '';
  };

  /** Run a delete by position that the OSC with seq stands for, on xterm.js terminal term. */
  Kitty.prototype.runDeferred = function (seq, term) {
    var c = this.deferred[seq];
    if (!c) return;
    delete this.deferred[seq];
    var d = kchar(c, 'd', 'a'), lower = d.toLowerCase();
    var buf = term.buffer.active, cells = [];
    var x, y;
    if (lower === 'c') cells.push([buf.cursorX, buf.cursorY]);
    else if (lower === 'p' || lower === 'q') cells.push([knum(c, 'x') - 1, knum(c, 'y') - 1]);
    else if (lower === 'x') for (y = 0; y < term.rows; y++) cells.push([knum(c, 'x') - 1, y]);
    else if (lower === 'y') for (x = 0; x < term.cols; x++) cells.push([x, knum(c, 'y') - 1]);
    var cell = buf.getNullCell(), self = this;
    cells.forEach(function (xy) {
      if (xy[0] < 0 || xy[1] < 0 || xy[0] >= term.cols || xy[1] >= term.rows) return;
      var line = buf.getLine(buf.viewportY + xy[1]);
      if (!line || !line.getCell(xy[0], cell) || imageCellPos(cell.getChars()) === null) return;
      var color = cell.getFgColor(), p = self.direct[color];
      if (!p || (lower === 'q' && p.z !== knum(c, 'z'))) return;
      delete self.direct[color];
      if (d !== lower) delete self.images[p.imageID];
    });
    this.onChange();
  };

  // imageCellPos returns [row, col] of an image cell's characters, or null.
  function imageCellPos(chars) {
    if (!chars) return null;
    var v = chars.codePointAt(0) - IMAGE_CELL;
    if (v < 0 || v > IMAGE_CELL_MAX * 256 + IMAGE_CELL_MAX) return null;
    return [v >> 8, v & 255];
  }

  // fitRect places an image of w x h pixels in a box of bw x bh.
  function fitRect(fit, w, h, bw, bh) {
    if (fit === 'stretch') return [0, 0, bw, bh];
    var s = Math.min(bw / w, bh / h);
    var sw = w * s, sh = h * s;
    return fit === 'center' ? [(bw - sw) / 2, (bh - sh) / 2, sw, sh] : [0, 0, sw, sh];
  }

  // paletteColor returns the CSS color of xterm 256-color palette entry i.
  function paletteColor(theme, i) {
    if (i < 16) return theme[PALETTE_KEYS[i]] || DEFAULT_THEME[PALETTE_KEYS[i]];
    if (i >= 232) {
      var g = 8 + 10 * (i - 232);
      return 'rgb(' + g + ',' + g + ',' + g + ')';
    }
    var levels = [0, 95, 135, 175, 215, 255];
    i -= 16;
    return 'rgb(' + levels[Math.floor(i / 36)] + ',' + levels[Math.floor(i / 6) % 6] + ',' + levels[i % 6] + ')';
  }

  function cellBackground(cell, theme) {
    if (cell.isBgRGB()) return '#' + ('00000' + cell.getBgColor().toString(16)).slice(-6);
    if (cell.isBgPalette()) return paletteColor(theme, cell.getBgColor());
    return theme.background || DEFAULT_THEME.background;
  }

  /** Draw the image cells of xterm.js terminal term onto canvas, which covers its screen. */
  function drawKitty(kitty, term, canvas, width, height) {
    var dpr = global.devicePixelRatio || 1;
    var cw = Math.round(width * dpr), ch = Math.round(height * dpr);
    if (canvas.width !== cw || canvas.height !== ch) {
      canvas.width = cw;
      canvas.height = ch;
      canvas.style.width = width + 'px';
      canvas.style.height = height + 'px';
    }
    var ctx = canvas.getContext('2d');
    ctx.setTransform(1, 0, 0, 1, 0, 0);
    ctx.clearRect(0, 0, cw, ch);
    if (!kitty.used) return;
    ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
    var sx = width / term.cols, sy = height / term.rows;
    var buf = term.buffer.active, cell = buf.getNullCell(), theme = term.options.theme || DEFAULT_THEME;
    for (var y = 0; y < term.rows; y++) {
      var line = buf.getLine(buf.viewportY + y);
      if (!line) continue;
      for (var x = 0; x < term.cols; x++) {
        if (!line.getCell(x, cell)) continue;
        var pos = imageCellPos(cell.getChars());
        if (!pos) continue;
        // cover the character xterm.js drew, then the piece of the image
        var x0 = Math.round(x * sx), y0 = Math.round(y * sy);
        var x1 = Math.round((x + 1) * sx), y1 = Math.round((y + 1) * sy);
        ctx.fillStyle = cellBackground(cell, theme);
        ctx.fillRect(x0, y0, x1 - x0, y1 - y0);
        var p = (cell.isFgRGB() || cell.isFgPalette()) && kitty.placement(cell.getFgColor());
        if (!p || !p.img.src || pos[0] >= p.rows || pos[1] >= p.cols) continue;
        var img = p.img, w = p.w || img.w - p.x, h = p.h || img.h - p.y;
        if (w <= 0 || h <= 0) continue;
        var f = fitRect(p.fit, w, h, p.cols * sx, p.rows * sy);
        ctx.save();
        ctx.beginPath();
        ctx.rect(x0, y0, x1 - x0, y1 - y0);
        ctx.clip();
        ctx.drawImage(img.src, p.x, p.y, w, h, x * sx - pos[1] * sx + f[0], y * sy - pos[0] * sy + f[1], f[2], f[3]);
        ctx.restore();
      }
    }
  }

  // ---------------------------------------------------------------- player

  /**
   * Options:
   *   autoplay (false), loop (false), speed (1), idleTimeLimit (header value; 0 disables),
   *   startAt (0), stopAt (none: play to the end), poster (time to show before playing, default: none),
   *   where each time is seconds or the label of a marker. Playback pauses at stopAt (or
   *   loops back to startAt with loop); pressing play there continues past it.
   *   fontSize (15), fontFamily, lineHeight (1.15), theme (xterm theme object),
   *   fit ('width' | 'contain' | 'none', default 'width'; 'contain' scales up or down to fill
   *   the container, which needs a height), controls (true), xtermBase, title.
   */
  function Player(container, opts) {
    this.opts = Object.assign({
      autoplay: false, loop: false, speed: 1, idleTimeLimit: null, startAt: 0, stopAt: null, poster: null,
      fontSize: 15, fontFamily: 'ui-monospace,SFMono-Regular,Menlo,Consolas,"Liberation Mono",monospace',
      lineHeight: 1.15, theme: null, fit: 'width', controls: true, xtermBase: XTERM_BASE
    }, opts || {});
    this.container = container;
    this.listeners = {};
    this.events = [];      // [{t, code, data}] with absolute (idle-capped) times
    this.markers = [];     // [{t, label}]
    this.duration = 0;
    this.index = 0;        // next event to apply
    this.time = 0;         // current cast time
    this.playing = false;
    this.speed = this.opts.speed;
    this._raf = 0;
  }

  Player.prototype.on = function (name, fn) {
    (this.listeners[name] = this.listeners[name] || []).push(fn);
    return this;
  };
  Player.prototype._emit = function (name, arg) {
    (this.listeners[name] || []).forEach(function (fn) { fn(arg); });
  };

  Player.prototype._build = function (Terminal) {
    injectCSS();
    var self = this;
    var root = this.root = el('div', this.opts.fit === 'contain' ? 'gotake gotake-contain' : 'gotake');
    root.tabIndex = 0;
    var vp = this.viewport = el('div', 'gotake-viewport');
    var scaler = this.scaler = el('div', 'gotake-scaler');
    vp.appendChild(scaler);
    root.appendChild(vp);

    var caption = this.captionEl = el('div', 'gotake-caption');
    caption.hidden = true;
    vp.appendChild(caption);

    var overlay = this.overlay = el('div', 'gotake-overlay', '<div class="gotake-big">' + ICONS.play + '</div>');
    overlay.addEventListener('click', function () { self.play(); root.focus(); });
    root.appendChild(overlay);

    if (this.opts.controls) {
      var bar = el('div', 'gotake-bar');
      this.btnPlay = el('button', 'gotake-btn', ICONS.play);
      this.btnPlay.title = 'Play / pause (space)';
      this.btnPlay.addEventListener('click', function () { self.toggle(); });
      this.timeEl = el('span', 'gotake-time', '0:00 / 0:00');
      var prog = this.progress = el('div', 'gotake-progress');
      var track = el('div', 'gotake-track');
      this.fill = el('div', 'gotake-fill');
      this.knob = el('div', 'gotake-knob');
      this.tip = el('div', 'gotake-tip');
      this.tip.hidden = true;
      track.appendChild(this.fill);
      this.markerLayer = el('div');
      track.appendChild(this.markerLayer);
      track.appendChild(this.knob);
      prog.appendChild(track);
      prog.appendChild(this.tip);
      this.btnSpeed = el('button', 'gotake-btn', this.speed + 'x');
      this.btnSpeed.title = 'Playback speed';
      this.btnSpeed.addEventListener('click', function () {
        var speeds = [0.5, 1, 1.5, 2, 3, 5];
        var i = speeds.indexOf(self.speed);
        self.setSpeed(speeds[(i + 1) % speeds.length]);
      });
      var btnFull = el('button', 'gotake-btn', ICONS.full);
      btnFull.title = 'Fullscreen (f)';
      btnFull.addEventListener('click', function () { self.fullscreen(); });
      bar.appendChild(this.btnPlay);
      bar.appendChild(this.timeEl);
      bar.appendChild(prog);
      bar.appendChild(this.btnSpeed);
      bar.appendChild(btnFull);
      root.appendChild(bar);

      var seekFromEvent = function (ev) {
        var r = track.getBoundingClientRect();
        var f = Math.min(1, Math.max(0, (ev.clientX - r.left) / r.width));
        return f * self.duration;
      };
      var dragging = false;
      prog.addEventListener('pointerdown', function (ev) {
        dragging = true;
        prog.setPointerCapture(ev.pointerId);
        self.seek(seekFromEvent(ev));
      });
      prog.addEventListener('pointermove', function (ev) {
        var t = seekFromEvent(ev);
        var r = track.getBoundingClientRect();
        self.tip.hidden = false;
        self.tip.style.left = ((t / (self.duration || 1)) * r.width) + 'px';
        var label = '';
        self.markers.forEach(function (m) { if (Math.abs(m.t - t) < self.duration * 0.01 && m.label) label = ' ' + m.label; });
        self.tip.textContent = fmtTime(t) + label;
        if (dragging) self.seek(t);
      });
      prog.addEventListener('pointerup', function () { dragging = false; });
      prog.addEventListener('pointerleave', function () { self.tip.hidden = true; });
    } else {
      this.overlay.style.bottom = '0';
    }

    // capture phase: after a click the focus is in xterm's textarea, which stops keydown from bubbling
    root.addEventListener('keydown', function (ev) { self._key(ev); }, true);

    this.container.innerHTML = '';
    this.container.appendChild(root);

    this.term = new Terminal({
      cols: 80, rows: 24, disableStdin: true, cursorBlink: false, scrollback: 0,
      fontSize: this.opts.fontSize, fontFamily: this.opts.fontFamily, lineHeight: this.opts.lineHeight,
      allowTransparency: true, theme: DEFAULT_THEME, convertEol: false
    });
    this.term.open(scaler);
    this._unscaleMouse();
    this._kittyCache = this._kittyCache || new Map();
    this.kitty = new Kitty(this._kittyCache, function () { self._kittyRedraw(); });
    this.kitty.termSize = function () { return [self.term.cols, self.term.rows]; };
    this.term.parser.registerOscHandler(KITTY_OSC, function (seq) {
      self.kitty.runDeferred(+seq, self.term);
      return true;
    });
    this.term.onRender(function () { self._kittyRedraw(); });

    if (global.ResizeObserver) {
      this._ro = new ResizeObserver(function () { self._fit(); });
      this._ro.observe(root);
    }
  };

  // xterm.js maps the mouse to cells without knowing about the scaler's CSS
  // transform, so selecting text in a scaled player picked the wrong cells.
  // Mouse events are moved back into unscaled coordinates before xterm sees
  // them: mousedown on the terminal, and the mousemove/mouseup of a drag,
  // which xterm listens to on the document.
  Player.prototype._unscaleMouse = function () {
    var self = this, dragging = false;
    var fix = function (ev) {
      var s = self._scale || 1, screen = self.scaler.querySelector('.xterm-screen');
      if (s === 1 || !screen || ev.__gotakeUnscaled) return;
      var r = screen.getBoundingClientRect();
      Object.defineProperty(ev, 'clientX', { value: r.left + (ev.clientX - r.left) / s });
      Object.defineProperty(ev, 'clientY', { value: r.top + (ev.clientY - r.top) / s });
      ev.__gotakeUnscaled = true;
    };
    this.scaler.addEventListener('mousedown', function (ev) { dragging = ev.button === 0; fix(ev); }, true);
    // kept so dispose() can take the window listeners off again
    this._winListeners = {
      mousemove: function (ev) { if (dragging || self.scaler.contains(ev.target)) fix(ev); },
      mouseup: function (ev) { if (dragging) { fix(ev); dragging = false; } }
    };
    for (var name in this._winListeners) global.addEventListener(name, this._winListeners[name], true);
  };

  /** Text selected with the mouse in the terminal, or '' when nothing is selected. */
  Player.prototype.selectedText = function () {
    return this.term && this.term.hasSelection() ? this.term.getSelection() : '';
  };

  /** Copy the selected text to the clipboard. Resolves to the copied text ('' when none). */
  Player.prototype.copySelection = function () {
    var text = this.selectedText();
    if (!text || !navigator.clipboard) return Promise.resolve('');
    return navigator.clipboard.writeText(text).then(function () { return text; });
  };

  Player.prototype._key = function (ev) {
    var k = ev.key, handled = true;
    if ((ev.ctrlKey || ev.metaKey) && k.toLowerCase() === 'c') {
      if (this.selectedText()) { ev.preventDefault(); ev.stopPropagation(); this.copySelection(); }
      return;
    }
    if (ev.ctrlKey || ev.metaKey || ev.altKey) return;
    if (k === ' ' || k === 'k') this.toggle();
    else if (k === 'ArrowLeft') this.seek(this.time - 5);
    else if (k === 'ArrowRight') this.seek(this.time + 5);
    else if (k === '.') this.step();
    else if (k === 'f') this.fullscreen();
    else if (k === '[') this.prevMarker();
    else if (k === ']') this.nextMarker();
    else if (k === '<' || k === ',') this.setSpeed(Math.max(0.25, this.speed / 2));
    else if (k === '>') this.setSpeed(Math.min(16, this.speed * 2));
    else if (/^[0-9]$/.test(k)) this.seek(this.duration * (+k) / 10);
    else handled = false;
    if (handled) ev.preventDefault();
  };

  Player.prototype._fit = function () {
    if (!this.term) return;
    var screen = this.scaler.querySelector('.xterm-screen');
    if (!screen) return;
    var w = screen.offsetWidth, h = screen.offsetHeight;
    var avail = this.viewport.clientWidth - 24;
    var scale = 1;
    if (this.opts.fit === 'width' && w > 0 && avail > 0) scale = Math.min(1, avail / w);
    if (this.opts.fit === 'contain' || document.fullscreenElement === this.root) {
      var availH = this.viewport.clientHeight - 20;
      if (w > 0 && h > 0 && avail > 0 && availH > 0) scale = Math.min(avail / w, availH / h);
    }
    this._scale = scale;
    this.scaler.style.transform = scale === 1 ? '' : 'scale(' + scale + ')';
    this.scaler.style.height = (h * scale) + 'px';
    this.scaler.style.width = (w * scale) + 'px';
    this.scaler.style.overflow = 'visible';
  };

  /** Load a recording: URL, .take or asciicast text, or parsed {header, events}. */
  Player.prototype.load = function (source) {
    var self = this;
    var getText = typeof source === 'string' && !/^\s*\{|\n/.test(source)
      ? fetch(source).then(function (r) {
        if (!r.ok) throw new Error('fetch ' + source + ': ' + r.status);
        return r.text();
      })
      : Promise.resolve(source);
    return Promise.all([getText, loadXterm(this.opts.xtermBase)]).then(function (res) {
      if (!self.term) self._build(res[1]);
      self.setCast(parseCast(res[0]));
      return self;
    }).catch(function (err) {
      self.container.innerHTML = '';
      var e = el('div', 'gotake gotake-error');
      e.textContent = 'gotake-player: ' + err.message;
      self.container.appendChild(e);
      throw err;
    });
  };

  /** Replace the recording, keeping the playhead where possible. */
  Player.prototype.setCast = function (cast, keepTime) {
    var o = this.opts;
    this.cast = cast;
    var h = cast.header || {};
    var term = h.term || {};
    this.cols = term.cols || 80;
    this.rows = term.rows || 24;
    this.kitty.cell = term.cell_width > 0 && term.cell_height > 0 ? [term.cell_width, term.cell_height] : ASSUMED_CELL;
    var idle = o.idleTimeLimit != null ? o.idleTimeLimit : (h.idle_time_limit || 0);
    var t = 0, evs = [], markers = [];
    cast.events.forEach(function (e) {
      var d = e.delay;
      if (idle > 0 && d > idle) d = idle;
      t += d;
      evs.push({ t: t, code: e.code, data: e.data });
      if (e.code === 'm') markers.push({ t: t, label: e.data });
    });
    this.events = evs;
    this.markers = markers;
    this.duration = t;
    var theme = o.theme || toXtermTheme(term.theme) || DEFAULT_THEME;
    this.term.options.theme = Object.assign({}, DEFAULT_THEME, theme);
    if (theme.background) this.root.style.setProperty('--gotake-bg', theme.background);
    var cc = captionColors(this.term.options.theme);
    if (cc) {
      this.root.style.setProperty('--gotake-caption-bg', cc[1]);
      this.root.style.setProperty('--gotake-caption-fg', cc[0]);
    }
    this._renderMarkers();
    var start = keepTime ? Math.min(this.time, t) : (o.poster != null ? this.resolveTime(o.poster) : this.startTime());
    this.index = 0; // force a full re-render from the new events
    this.time = 0;
    this.seek(start);
    if (!keepTime && !o.autoplay) {
      if (this.overlay) this.overlay.hidden = false;
      if (o.poster == null) {
        this.term.write('\x1bc\x1b[?25l' + LOGO);
        this._logo = true;
      }
    }
    this._emit('ready', this);
    if (!keepTime && o.autoplay) this.play();
  };

  Player.prototype._renderMarkers = function () {
    if (!this.markerLayer) return;
    var self = this;
    this.markerLayer.innerHTML = '';
    this.markers.forEach(function (m) {
      var d = el('div', 'gotake-marker');
      d.style.left = (m.t / (self.duration || 1) * 100) + '%';
      d.title = m.label || fmtTime(m.t);
      self.markerLayer.appendChild(d);
    });
  };

  // Apply events [index, until) to the terminal in one write.
  Player.prototype._applyUntil = function (t) {
    var buf = '';
    var term = this.term;
    while (this.index < this.events.length && this.events[this.index].t <= t) {
      var e = this.events[this.index++];
      if (e.code === 'o') {
        buf += this.kitty.filter(e.data);
      } else if (e.code === 'r') {
        var m = /^(\d+)x(\d+)$/.exec(e.data);
        if (m) {
          if (buf) { term.write(buf); buf = ''; }
          term.resize(+m[1], +m[2]);
          this._fitSoon();
        }
      } else if (e.code === 'm') {
        this._emit('marker', { t: e.t, label: e.data });
      } else if (e.code === 'c') {
        this._caption(e.data);
      }
    }
    if (buf) term.write(buf);
  };

  // _caption shows text over the terminal (a caption event); empty hides it.
  Player.prototype._caption = function (text) {
    if (!this.captionEl) return;
    this.captionEl.textContent = text || '';
    this.captionEl.hidden = !text;
  };

  // _kittyRedraw draws Kitty graphics on the next frame.
  Player.prototype._kittyRedraw = function () {
    var self = this;
    if (this._kittyRaf) return;
    this._kittyRaf = requestAnimationFrame(function () {
      self._kittyRaf = 0;
      var screen = self.scaler.querySelector('.xterm-screen');
      if (!screen) return;
      var canvas = self._kittyCanvas;
      if (!self.kitty.used && !canvas) return;
      if (!canvas || canvas.parentNode !== screen) {
        canvas = self._kittyCanvas = document.createElement('canvas');
        canvas.className = 'gotake-kitty';
        screen.appendChild(canvas);
      }
      drawKitty(self.kitty, self.term, canvas, screen.offsetWidth, screen.offsetHeight);
    });
  };

  Player.prototype._fitSoon = function () {
    var self = this;
    requestAnimationFrame(function () { self._fit(); });
  };

  /** Jump to time t (seconds). */
  /** Time of t: seconds (a number or numeric string) or the label of a marker. */
  Player.prototype.resolveTime = function (t) {
    if (t == null || t === '') return null;
    if (typeof t === 'number' || /^\s*[\d.]+\s*$/.test(t)) return +t;
    var m = this.markers.find(function (m) { return m.label === t; }) ||
      this.markers.find(function (m) { return String(m.label).toLowerCase() === String(t).toLowerCase(); });
    if (!m) {
      console.warn('gotake-player: no marker ' + JSON.stringify(t));
      return null;
    }
    return m.t;
  };

  Player.prototype.startTime = function () { return this.resolveTime(this.opts.startAt) || 0; };

  // stopTime is where playback that starts at time from pauses: stopAt if it is still ahead.
  Player.prototype._stopTime = function (from) {
    var s = this.resolveTime(this.opts.stopAt);
    return s != null && s > from + 0.01 ? Math.min(s, this.duration) : this.duration;
  };

  Player.prototype.seek = function (t) {
    t = Math.min(Math.max(0, t), this.duration);
    if (t < this.time || this.index === 0 || t === 0 || this._logo) {
      // rewind: full reset in-band so it is ordered with pending writes
      this._logo = false;
      this.term.resize(this.cols, this.rows);
      this.term.write('\x1bc\x1b[?25l');
      this.kitty.reset();
      this._caption('');
      this.index = 0;
    }
    this.time = t;
    this._applyUntil(t);
    if (this.playing) {
      this._wallStart = performance.now();
      this._timeStart = t;
      this._end = this._stopTime(t);
    }
    if (this.overlay && t > 0) this.overlay.hidden = true;
    this._fitSoon();
    this._updateUI();
  };

  Player.prototype.play = function () {
    if (this.playing) return;
    if (this._logo || this.time >= this.duration) this.seek(this.startTime());
    this.playing = true;
    this.overlay.hidden = true;
    this._wallStart = performance.now();
    this._timeStart = this.time;
    this._end = this._stopTime(this.time);
    this._tick();
    this._updateUI();
    this._emit('play');
  };

  Player.prototype.pause = function () {
    if (!this.playing) return;
    this.playing = false;
    cancelAnimationFrame(this._raf);
    this._updateUI();
    this._emit('pause');
  };

  Player.prototype.toggle = function () { this.playing ? this.pause() : this.play(); };

  /** While paused: advance to the next output event. */
  Player.prototype.step = function () {
    if (this.playing) this.pause();
    var i = this.index;
    while (i < this.events.length && this.events[i].code !== 'o') i++;
    if (i < this.events.length) this.seek(this.events[i].t);
  };

  Player.prototype.nextMarker = function () {
    var t = this.time;
    var m = this.markers.find(function (m) { return m.t > t + 0.01; });
    this.seek(m ? m.t : this.duration);
  };

  Player.prototype.prevMarker = function () {
    var t = this.time, best = 0;
    this.markers.forEach(function (m) { if (m.t < t - 0.5) best = m.t; });
    this.seek(best);
  };

  Player.prototype.setSpeed = function (s) {
    if (this.playing) { this._timeStart = this.time; this._wallStart = performance.now(); }
    this.speed = s;
    if (this.btnSpeed) this.btnSpeed.textContent = s + 'x';
  };

  Player.prototype.fullscreen = function () {
    if (document.fullscreenElement) document.exitFullscreen();
    else if (this.root.requestFullscreen) this.root.requestFullscreen().then(this._fitSoon.bind(this));
  };

  Player.prototype._tick = function () {
    var self = this;
    this._raf = requestAnimationFrame(function () {
      if (!self.playing) return;
      var t = self._timeStart + (performance.now() - self._wallStart) / 1000 * self.speed;
      var end = self._end != null ? self._end : self.duration;
      if (t >= end) {
        self.time = end;
        self._applyUntil(end);
        self.playing = false;
        self._updateUI();
        // set before the event, so a handler that calls dispose() can still cancel it
        if (self.opts.loop) {
          self._loopTimer = setTimeout(function () {
            self._loopTimer = 0;
            self.seek(self.startTime());
            self.play();
          }, 1000);
        }
        self._emit(end < self.duration ? 'stop' : 'ended');
        return;
      }
      self.time = t;
      self._applyUntil(t);
      self._updateUI();
      self._tick();
    });
  };

  Player.prototype._updateUI = function () {
    this._emit('time', this.time);
    if (!this.btnPlay) return;
    var ended = !this.playing && this.time >= this.duration && this.duration > 0;
    // only swap the icon when it changes: replacing it every frame detaches the
    // element under the pointer between mousedown and mouseup, which drops clicks
    var icon = this.playing ? 'pause' : (ended ? 'replay' : 'play');
    if (this._icon !== icon) {
      this._icon = icon;
      this.btnPlay.innerHTML = ICONS[icon];
      this.btnPlay.setAttribute('aria-label', icon === 'pause' ? 'Pause' : 'Play');
    }
    var f = this.duration ? this.time / this.duration : 0;
    this.fill.style.width = (f * 100) + '%';
    this.knob.style.left = (f * 100) + '%';
    this.timeEl.textContent = fmtTime(this.time) + ' / ' + fmtTime(this.duration);
  };

  Player.prototype.dispose = function () {
    this.pause();
    clearTimeout(this._loopTimer);
    if (this._ro) this._ro.disconnect();
    for (var name in this._winListeners) global.removeEventListener(name, this._winListeners[name], true);
    this._winListeners = null;
    if (this.term) this.term.dispose();
    this.container.innerHTML = '';
  };

  Object.defineProperty(Player.prototype, 'currentTime', { get: function () { return this.time; } });

  // ---------------------------------------------------------------- API

  function create(container, source, opts) {
    if (typeof container === 'string') container = document.querySelector(container);
    var p = new Player(container, opts);
    return p.load(source);
  }

  function boolAttr(elm, name) {
    return elm.hasAttribute(name) && elm.getAttribute(name) !== 'false';
  }

  /** Mount a player on every element with a data-gotake-src attribute. */
  function mountAll(root) {
    var nodes = (root || document).querySelectorAll('[data-gotake-src]:not([data-gotake-mounted])');
    return Promise.all(Array.prototype.map.call(nodes, function (n) {
      n.setAttribute('data-gotake-mounted', '');
      var d = n.dataset;
      var opts = {
        autoplay: boolAttr(n, 'data-autoplay'),
        loop: boolAttr(n, 'data-loop'),
        controls: d.controls !== 'false'
      };
      if (d.speed) opts.speed = +d.speed;
      if (d.idleTimeLimit) opts.idleTimeLimit = +d.idleTimeLimit;
      if (d.startAt) opts.startAt = d.startAt;
      if (d.stopAt) opts.stopAt = d.stopAt;
      if (d.poster) opts.poster = d.poster;
      if (d.fontSize) opts.fontSize = +d.fontSize;
      if (d.fit) opts.fit = d.fit;
      return create(n, d.gotakeSrc, opts).catch(function (e) { console.error(e); });
    }));
  }

  var api = {
    create: create, mountAll: mountAll, parseCast: parseCast, Player: Player, formatTime: fmtTime,
    defaultTheme: DEFAULT_THEME, paletteKeys: PALETTE_KEYS
  };
  global.GoTakePlayer = api;

  if (typeof document !== 'undefined') {
    if (document.readyState === 'loading') document.addEventListener('DOMContentLoaded', function () { mountAll(); });
    else mountAll();
  }
})(typeof window !== 'undefined' ? window : this);
