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

  var CSS = [
    '.gotake{--gotake-bg:#121314;--gotake-fg:#d6d6d6;--gotake-accent:#e5a50a;--gotake-bar:rgba(18,19,20,.92);',
    'position:relative;display:block;max-width:100%;border-radius:8px;overflow:hidden;background:var(--gotake-bg);',
    'font:13px/1.2 system-ui,-apple-system,"Segoe UI",sans-serif;color:var(--gotake-fg);outline:none;user-select:none}',
    '.gotake:focus-visible{box-shadow:0 0 0 2px var(--gotake-accent)}',
    '.gotake-viewport{position:relative;overflow:hidden;padding:10px 12px}',
    '.gotake-scaler{transform-origin:0 0;width:max-content}',
    '.gotake .xterm .xterm-viewport{overflow:hidden!important;background:transparent!important}',
    '.gotake .xterm-cursor-layer, .gotake .xterm-helpers{pointer-events:none}',
    '.gotake-overlay{position:absolute;inset:0 0 38px 0;display:flex;align-items:center;justify-content:center;cursor:pointer}',
    '.gotake-overlay[hidden]{display:none}',
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

  var TAKE_CODES = { out: 'o', 'in': 'i', resize: 'r', marker: 'm', exit: 'x' };
  var TAKE_ESC = { e: '\x1b', r: '\r', n: '\n', t: '\t', '\\': '\\', '"': '"' };

  /** Split a .take line into words: bare words and quoted strings, up to a # comment. */
  function takeWords(line) {
    var out = [], i = 0;
    while (i < line.length) {
      var c = line[i];
      if (c === ' ' || c === '\t' || c === '\r') { i++; continue; }
      if (c === '#') break;
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

  // ---------------------------------------------------------------- player

  /**
   * Options:
   *   autoplay (false), loop (false), speed (1), idleTimeLimit (header value; 0 disables),
   *   startAt (0), stopAt (none: play to the end), poster (time to show before playing, default: none),
   *   where each time is seconds or the label of a marker. Playback pauses at stopAt (or
   *   loops back to startAt with loop); pressing play there continues past it.
   *   fontSize (15), fontFamily, lineHeight (1.15), theme (xterm theme object),
   *   fit ('width' | 'none', default 'width'), controls (true), xtermBase, title.
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
    var root = this.root = el('div', 'gotake');
    root.tabIndex = 0;
    var vp = this.viewport = el('div', 'gotake-viewport');
    var scaler = this.scaler = el('div', 'gotake-scaler');
    vp.appendChild(scaler);
    root.appendChild(vp);

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
    global.addEventListener('mousemove', function (ev) {
      if (dragging || self.scaler.contains(ev.target)) fix(ev);
    }, true);
    global.addEventListener('mouseup', function (ev) {
      if (dragging) { fix(ev); dragging = false; }
    }, true);
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
    if (document.fullscreenElement === this.root) {
      var availH = this.viewport.clientHeight - 20;
      scale = Math.min(avail / w, availH / h);
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
    this._renderMarkers();
    var start = keepTime ? Math.min(this.time, t) : (o.poster != null ? this.resolveTime(o.poster) : this.startTime());
    this.index = 0; // force a full re-render from the new events
    this.time = 0;
    this.seek(start);
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
        buf += e.data;
      } else if (e.code === 'r') {
        var m = /^(\d+)x(\d+)$/.exec(e.data);
        if (m) {
          if (buf) { term.write(buf); buf = ''; }
          term.resize(+m[1], +m[2]);
          this._fitSoon();
        }
      } else if (e.code === 'm') {
        this._emit('marker', { t: e.t, label: e.data });
      }
    }
    if (buf) term.write(buf);
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
    if (t < this.time || this.index === 0 || t === 0) {
      // rewind: full reset in-band so it is ordered with pending writes
      this.term.resize(this.cols, this.rows);
      this.term.write('\x1bc\x1b[?25l');
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
    if (this.time >= this.duration) this.seek(this.startTime());
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
        self._emit(end < self.duration ? 'stop' : 'ended');
        if (self.opts.loop) {
          setTimeout(function () { self.seek(self.startTime()); self.play(); }, 1000);
        }
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
    if (this._ro) this._ro.disconnect();
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
