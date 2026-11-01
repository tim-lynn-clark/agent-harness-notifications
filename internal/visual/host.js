// cnw visual host (visual attention spec §5, §6.3). Started by `cnw visual host`:
//   osascript -l JavaScript visual-host-<hash>.js STATE_DIR CNW_PATH
// Every 250 ms it reads visual.json and visual-preview.json from STATE_DIR and
// draws the waiting sessions. It writes nothing but the heartbeat.
'use strict';
ObjC.import('Cocoa');
ObjC.import('QuartzCore');

const POLL_S = 0.25;
const HEARTBEAT_MS = 5000;
const EDGE = 28; // aurora depth, points
const STRIPS = 7; // aurora strips per edge, fading inward
const PREVIEW = { halo: 3000, critter: 6000, end: 12000 };
// CanJoinAllSpaces | Stationary | IgnoresCycle | FullScreenAuxiliary
const BEHAVIOR = (1 << 0) | (1 << 4) | (1 << 6) | (1 << 8);
const STATUS_LEVEL = 25; // NSStatusWindowLevel
const SHARING_NONE = 0; // NSWindowSharingNone

let DIR = '';
let CNW = '';
let waiting = []; // {id, category, label, color, maxRung, since, halo, critter}, oldest first
let overlays = []; // {frame:{x,y,w,h,scale}, window, root, aurora}
let screensKey = '';
let auroraKey = null;
let orbKey = null;
let lastBeat = 0;
let statusItem = null;
let menuTarget = null;

// ---- helpers ----------------------------------------------------------------

function readJSON(path) {
  const s = $.NSString.stringWithContentsOfFileEncodingError($(path), $.NSUTF8StringEncoding, null);
  if (s.isNil()) return null;
  try {
    return JSON.parse(ObjC.unwrap(s));
  } catch (e) {
    return null;
  }
}

function writeText(path, text) {
  $(text).writeToFileAtomicallyEncodingError($(path), true, $.NSUTF8StringEncoding, null);
}

function rgb(hex, alpha) {
  const n = parseInt(hex.slice(1), 16);
  return $.NSColor.colorWithSRGBRedGreenBlueAlpha(((n >> 16) & 255) / 255, ((n >> 8) & 255) / 255, (n & 255) / 255, alpha);
}

// cgColor is rgb as a CGColor for layers. Reading NSColor.CGColor through
// the JXA bridge kills osascript, so it builds the CGColor directly.
function cgColor(hex, alpha) {
  const n = parseInt(hex.slice(1), 16);
  return $.CGColorCreateSRGB(((n >> 16) & 255) / 255, ((n >> 8) & 255) / 255, (n & 255) / 255, alpha);
}

function num(v) {
  return $.NSNumber.numberWithDouble(v);
}

function noAnim(fn) {
  $.CATransaction.begin;
  $.CATransaction.setDisableActions(true);
  fn();
  $.CATransaction.commit;
}

function after(seconds, fn) {
  $.NSTimer.scheduledTimerWithTimeIntervalRepeatsBlock(seconds, false, () => fn());
}

function reduceMotion() {
  return $.NSWorkspace.sharedWorkspace.accessibilityDisplayShouldReduceMotion;
}

function fade(l, from, to, dur) {
  const a = $.CABasicAnimation.animationWithKeyPath($('opacity'));
  a.setFromValue(num(from));
  a.setToValue(num(to));
  a.setDuration(dur);
  noAnim(() => l.setOpacity(to));
  l.addAnimationForKey(a, $('fade'));
}

function fadeIn(l) {
  fade(l, 0, 1, 0.3);
}

function fadeOut(l) {
  fade(l, 1, 0, 0.3);
  after(0.35, () => l.removeFromSuperlayer);
}

// loop animates path between from and to and back, forever or for repeatFor seconds.
function loop(l, path, from, to, dur, key, repeatFor) {
  const a = $.CABasicAnimation.animationWithKeyPath($(path));
  a.setFromValue(num(from));
  a.setToValue(num(to));
  a.setDuration(dur);
  a.setAutoreverses(true);
  if (repeatFor) a.setRepeatDuration(repeatFor);
  else a.setRepeatCount(1e9);
  l.addAnimationForKey(a, $(key));
}

// ---- model ------------------------------------------------------------------

function currentWaiting(now) {
  const out = [];
  const snap = readJSON(DIR + '/visual.json');
  if (snap && snap.version === 1) {
    for (const w of snap.waiting || []) {
      if (now - w.since_ms > snap.stale_after_ms) continue;
      out.push({ id: w.session_id, category: w.category, label: w.label || 'session', color: w.color,
        maxRung: w.max_rung, since: w.since_ms, halo: snap.halo_after_ms, critter: snap.critter_after_ms });
    }
  }
  const p = readJSON(DIR + '/visual-preview.json');
  if (p && now - p.started_ms < PREVIEW.end) {
    out.push({ id: '', category: p.category, label: p.label || 'preview', color: p.color,
      maxRung: p.max_rung, since: p.started_ms, halo: PREVIEW.halo, critter: PREVIEW.critter });
  }
  out.sort((a, b) => a.since - b.since);
  return out;
}

function rung(e, now) {
  const age = now - e.since;
  return Math.min(age >= e.critter ? 3 : age >= e.halo ? 2 : 1, e.maxRung);
}

// ---- overlay windows ----------------------------------------------------------

function screenFrames() {
  const screens = $.NSScreen.screens;
  const out = [];
  for (let i = 0; i < screens.count; i++) {
    const s = screens.objectAtIndex(i);
    const f = s.frame;
    out.push({ x: f.origin.x, y: f.origin.y, w: f.size.width, h: f.size.height, scale: s.backingScaleFactor });
  }
  return out;
}

function makeOverlay(f) {
  const win = $.NSWindow.alloc.initWithContentRectStyleMaskBackingDefer($.NSMakeRect(f.x, f.y, f.w, f.h), 0, 2, false);
  win.setOpaque(false);
  win.setBackgroundColor($.NSColor.clearColor);
  win.setHasShadow(false);
  win.setIgnoresMouseEvents(true);
  win.setLevel(STATUS_LEVEL);
  win.setCollectionBehavior(BEHAVIOR);
  win.setSharingType(SHARING_NONE);
  win.setReleasedWhenClosed(false);
  win.contentView.setWantsLayer(true);
  win.orderFrontRegardless;
  return { frame: f, window: win, root: win.contentView.layer, aurora: null };
}

function syncScreens() {
  const frames = screenFrames();
  const key = JSON.stringify(frames);
  if (key === screensKey) return;
  screensKey = key;
  for (const o of overlays) o.window.orderOut(null);
  overlays = frames.map(makeOverlay);
  dropHalo();
  critter = null;
  critterGone = 0;
  auroraKey = null;
}

// ---- rung 1: aurora -----------------------------------------------------------

function renderAurora() {
  const reduce = reduceMotion();
  const key = waiting.map((e) => e.color).join(',') + (reduce ? ':still' : '');
  if (key === auroraKey) return;
  auroraKey = key;
  for (const o of overlays) {
    if (o.aurora) fadeOut(o.aurora);
    o.aurora = waiting.length ? buildAurora(o, waiting.map((e) => e.color), reduce) : null;
  }
}

// buildAurora divides each edge into one segment per session (oldest first:
// top and bottom left to right, sides top to bottom), each a stack of strips
// fading inward.
function buildAurora(o, colors, reduce) {
  const W = o.frame.w;
  const H = o.frame.h;
  const t = EDGE / STRIPS;
  const n = colors.length;
  const box = $.CALayer.layer; // fades in and out
  box.setFrame($.NSMakeRect(0, 0, W, H));
  const glow = $.CALayer.layer; // breathes
  glow.setFrame($.NSMakeRect(0, 0, W, H));
  box.addSublayer(glow);
  for (let s = 0; s < n; s++) {
    for (let i = 0; i < STRIPS; i++) {
      const color = cgColor(colors[s], 0.6 * Math.pow(1 - i / STRIPS, 2));
      const segW = W / n;
      const segH = H / n;
      const rects = [
        [s * segW, H - (i + 1) * t, segW, t],
        [s * segW, i * t, segW, t],
        [i * t, H - (s + 1) * segH, t, segH],
        [W - (i + 1) * t, H - (s + 1) * segH, t, segH],
      ];
      for (const r of rects) {
        const l = $.CALayer.layer;
        l.setFrame($.NSMakeRect(r[0], r[1], r[2], r[3]));
        l.setBackgroundColor(color);
        glow.addSublayer(l);
      }
    }
  }
  if (!reduce) loop(glow, 'opacity', 1, 0.35, 1.2, 'breathe');
  o.root.addSublayer(box);
  fadeIn(box);
  return box;
}

// ---- rung 2: halo -------------------------------------------------------------

const HALO_R = 22;
const HALO_IDLE_MS = 500; // stop the 60 Hz timer after the pointer rests this long
let halo = null; // {box, ring, label, key, overlay}
let haloTimer = null;
let lastPointer = '';
let lastMove = 0;

function pointerOverlay() {
  const m = $.NSEvent.mouseLocation;
  for (const o of overlays) {
    const f = o.frame;
    if (m.x >= f.x && m.x < f.x + f.w && m.y >= f.y && m.y < f.y + f.h) return { o: o, x: m.x - f.x, y: m.y - f.y };
  }
  return null;
}

// textLayer takes CGColors (see cgColor) for fg and bg.
function textLayer(text, size, fg, bg) {
  const l = $.CATextLayer.layer;
  l.setString($(text));
  l.setFontSize(size);
  l.setForegroundColor(fg);
  l.setBackgroundColor(bg);
  l.setCornerRadius(6);
  l.setAlignmentMode($('center'));
  l.setContentsScale($.NSScreen.mainScreen.backingScaleFactor);
  const sz = l.preferredFrameSize;
  l.setBounds($.NSMakeRect(0, 0, sz.width + 14, sz.height + 4));
  return l;
}

function renderHalo(now) {
  const top = waiting.find((e) => rung(e, now) >= 2);
  if (!top) {
    dropHalo();
    return;
  }
  const others = waiting.length - 1;
  const text = top.label + ' · ' + top.category.replace(/_/g, ' ') + (others > 0 ? ' +' + others : '');
  const key = top.color + '|' + text;
  if (!halo || halo.key !== key) {
    dropHalo();
    const box = $.CALayer.layer;
    const ring = $.CALayer.layer;
    ring.setBounds($.NSMakeRect(0, 0, HALO_R * 2, HALO_R * 2));
    ring.setCornerRadius(HALO_R);
    ring.setBorderWidth(3);
    ring.setBorderColor(cgColor(top.color, 0.9));
    ring.setBackgroundColor(cgColor(top.color, 0.12));
    box.addSublayer(ring);
    const label = textLayer(text, 13, cgColor('#FFFFFF', 1), cgColor(top.color, 0.85));
    box.addSublayer(label);
    // The box has zero size; ring and label sit at fixed offsets so each frame moves only the box.
    noAnim(() => {
      ring.setPosition($.NSMakePoint(0, 0));
      label.setPosition($.NSMakePoint(HALO_R + 8 + label.bounds.size.width / 2, -HALO_R));
    });
    if (!reduceMotion()) loop(ring, 'opacity', 1, 0.45, 0.9, 'breathe');
    halo = { box: box, ring: ring, label: label, key: key, overlay: null };
  }
  // The poll restarts the 60 Hz timer once the pointer moves again.
  if (followPointer() && !haloTimer) {
    haloTimer = $.NSTimer.scheduledTimerWithTimeIntervalRepeatsBlock(1 / 60, true, () => haloFrame());
  }
}

// haloFrame runs from the repeating timer; an exception escaping it would stop the timer.
function haloFrame() {
  try {
    if (!followPointer() && Date.now() - lastMove >= HALO_IDLE_MS && haloTimer) {
      haloTimer.invalidate;
      haloTimer = null;
    }
  } catch (e) {
    console.log('cnw visual host: ' + e);
  }
}

// followPointer moves the halo to the pointer and reports whether the pointer moved.
function followPointer() {
  if (!halo) return false;
  const p = pointerOverlay();
  if (!p) return false;
  const key = Math.round(p.x) + ',' + Math.round(p.y) + ',' + overlays.indexOf(p.o);
  if (key === lastPointer) return false; // pointer still: skip the frame
  lastPointer = key;
  lastMove = Date.now();
  const moved = halo.overlay !== p.o;
  noAnim(() => {
    if (moved) {
      halo.box.removeFromSuperlayer;
      p.o.root.addSublayer(halo.box);
      halo.overlay = p.o;
    }
    halo.box.setPosition($.NSMakePoint(p.x, p.y));
  });
  if (moved) fadeIn(halo.box);
  return true;
}

function dropHalo() {
  if (haloTimer) {
    haloTimer.invalidate;
    haloTimer = null;
  }
  if (halo) {
    if (halo.overlay) fadeOut(halo.box);
    halo = null;
  }
  lastPointer = '';
}

// ---- rung 3: critter ----------------------------------------------------------

const GLYPH = {
  question: '?', done_question: '?', plan_approval: '✎', permission: '!', mcp_input: '⇄',
  done: '✓', done_background: '✓', error: '✕', usage_limit: '⏳', idle_reminder: '…',
};
let critter = null; // {key, overlay, box, body, feet, sign}
let critterGone = 0; // when the leaving critter is off screen, ms

// part adds a rounded rectangle to parent with its bottom centre at (x, y); color is a CGColor.
function part(parent, x, y, w, h, radius, color) {
  const l = $.CALayer.layer;
  l.setAnchorPoint($.NSMakePoint(0.5, 0));
  l.setBounds($.NSMakeRect(0, 0, w, h));
  l.setPosition($.NSMakePoint(x, y));
  l.setCornerRadius(radius);
  l.setBackgroundColor(color);
  parent.addSublayer(l);
  return l;
}

function blink(eye) {
  const a = $.CAKeyframeAnimation.animationWithKeyPath($('transform.scale.y'));
  a.setValues($([num(1), num(1), num(0.1), num(1)]));
  a.setKeyTimes($([num(0), num(0.92), num(0.96), num(1)]));
  a.setDuration(4);
  a.setRepeatCount(1e9);
  eye.addAnimationForKey(a, $('blink'));
}

function moveTo(l, fromX, toX, dur) {
  const a = $.CABasicAnimation.animationWithKeyPath($('position.x'));
  a.setFromValue(num(fromX));
  a.setToValue(num(toX));
  a.setDuration(dur);
  noAnim(() => l.setPosition($.NSMakePoint(toX, 0)));
  l.addAnimationForKey(a, $('walk'));
}

function walk(c, fromX, toX, dur) {
  moveTo(c.box, fromX, toX, dur);
  loop(c.body, 'transform.scale.y', 1, 0.9, 0.18, 'squash', dur);
  loop(c.feet[0], 'position.y', 0, 6, 0.18, 'step', dur);
  loop(c.feet[1], 'position.y', 6, 0, 0.18, 'step', dur);
}

function critterKey(e) {
  return e.id + '|' + e.category + '|' + e.since;
}

function renderCritter(now) {
  const top = waiting.find((e) => rung(e, now) >= 3);
  if (critter && (!top || critterKey(top) !== critter.key)) critterGone = now + critterLeave();
  if (top && !critter && now >= critterGone) critterEnter(top);
}

function critterEnter(e) {
  const p = pointerOverlay();
  const o = p ? p.o : overlays[0];
  if (!o) return;
  const reduce = reduceMotion();
  const W = o.frame.w;
  const box = $.CALayer.layer;
  box.setAnchorPoint($.NSMakePoint(0.5, 0));
  box.setBounds($.NSMakeRect(0, 0, 120, 160));
  const dark = cgColor('#262626', 1);
  const feet = [part(box, 46, 0, 18, 8, 4, dark), part(box, 74, 0, 18, 8, 4, dark)];
  const body = part(box, 60, 6, 64, 56, 28, cgColor(e.color, 1));
  for (const x of [20, 44]) {
    const eye = part(body, x, 26, 14, 16, 7, cgColor('#FFFFFF', 1));
    part(eye, 7, 3, 6, 7, 3, cgColor('#000000', 1));
    if (!reduce) blink(eye);
  }
  const sign = $.CALayer.layer;
  sign.setAnchorPoint($.NSMakePoint(0.5, 0));
  sign.setBounds($.NSMakeRect(0, 0, 4, 100));
  sign.setPosition($.NSMakePoint(100, 30));
  part(sign, 2, 0, 4, 70, 2, cgColor('#734D26', 1));
  const board = textLayer((GLYPH[e.category] || '!') + '  ' + e.label, 13, cgColor('#000000', 1), cgColor('#FFFFFF', 1));
  board.setBorderWidth(2);
  board.setBorderColor(cgColor(e.color, 1));
  board.setAnchorPoint($.NSMakePoint(0.5, 0));
  board.setPosition($.NSMakePoint(2, 66));
  sign.addSublayer(board);
  box.addSublayer(sign);
  o.root.addSublayer(box);
  critter = { key: critterKey(e), overlay: o, box: box, body: body, feet: feet, sign: sign };
  const stopX = W - 220;
  if (reduce) {
    noAnim(() => box.setPosition($.NSMakePoint(stopX, 0)));
    fadeIn(box);
  } else {
    walk(critter, W + 80, stopX, 3);
    after(3, () => loop(sign, 'transform.rotation.z', -0.05, 0.05, 1.6, 'sway'));
  }
}

// critterLeave sends the critter off and returns how long that takes, ms.
function critterLeave() {
  const c = critter;
  critter = null;
  if (reduceMotion()) {
    fadeOut(c.box);
    return 350;
  }
  c.sign.removeAnimationForKey($('sway'));
  loop(c.sign, 'transform.rotation.z', -0.35, 0.35, 0.2, 'wave', 1.2);
  after(1.2, () => {
    walk(c, c.box.position.x, c.overlay.frame.w + 120, 2.5);
    after(2.6, () => c.box.removeFromSuperlayer);
  });
  return 3800;
}

// ---- orb ----------------------------------------------------------------------

function registerMenuTarget() {
  ObjC.registerSubclass({
    name: 'CNWMenuTarget',
    protocols: ['NSMenuDelegate'],
    methods: {
      'menuNeedsUpdate:': { types: ['void', ['id']], implementation: (menu) => fillMenu(menu) },
      'jump:': { types: ['void', ['id']], implementation: (item) => launch(['visual', 'jump', ObjC.unwrap(item.representedObject)]) },
      'quit:': { types: ['void', ['id']], implementation: () => quit() },
    },
  });
  menuTarget = $.CNWMenuTarget.alloc.init;
}

function makeOrb() {
  statusItem = $.NSStatusBar.systemStatusBar.statusItemWithLength(-1); // NSVariableStatusItemLength
  const menu = $.NSMenu.alloc.init;
  menu.setAutoenablesItems(false);
  menu.setDelegate(menuTarget);
  statusItem.setMenu(menu);
}

function renderOrb() {
  const key = waiting.length ? waiting[0].color + ':' + waiting.length : '';
  if (key === orbKey) return;
  orbKey = key;
  const text = waiting.length === 0 ? '○' : waiting.length === 1 ? '●' : '● ' + waiting.length;
  const color = waiting.length ? rgb(waiting[0].color, 1) : $.NSColor.tertiaryLabelColor;
  statusItem.button.setAttributedTitle(attributed(text, color, text.length));
}

// attributed returns text with its first len characters in color. JXA doesn't
// bridge the NSAttributedString init methods, so it fills an empty string.
function attributed(text, color, len) {
  const s = $.NSMutableAttributedString.alloc.init;
  s.replaceCharactersInRangeWithString($.NSMakeRange(0, 0), $(text));
  const attrs = $.NSDictionary.dictionaryWithObjectForKey(color, $.NSForegroundColorAttributeName);
  s.addAttributesRange(attrs, $.NSMakeRange(0, len));
  return s;
}

function age(ms) {
  const s = Math.max(0, Math.floor(ms / 1000));
  return s < 60 ? s + 's' : s < 3600 ? Math.floor(s / 60) + 'm' : Math.floor(s / 3600) + 'h';
}

function menuItem(title, action) {
  const it = $.NSMenuItem.alloc.initWithTitleActionKeyEquivalent($(title), action, $(''));
  if (action) it.setTarget(menuTarget);
  return it;
}

function fillMenu(menu) {
  menu.removeAllItems;
  const now = Date.now();
  if (!waiting.length) {
    const it = menuItem('Nothing is waiting', null);
    it.setEnabled(false);
    menu.addItem(it);
  }
  for (const e of waiting) {
    const title = '● ' + e.label + ' — ' + e.category.replace(/_/g, ' ') + ' · ' + age(now - e.since);
    const it = menuItem(title, e.id ? 'jump:' : null);
    it.setAttributedTitle(attributed(title, rgb(e.color, 1), 1));
    if (e.id) it.setRepresentedObject($(e.id));
    else it.setEnabled(false);
    menu.addItem(it);
  }
  menu.addItem($.NSMenuItem.separatorItem);
  menu.addItem(menuItem('Quit visuals', 'quit:'));
}

function launch(args) {
  $.NSTask.launchedTaskWithLaunchPathArguments($(CNW), $(args));
}

function quit() {
  for (const o of overlays) o.window.orderOut(null);
  $.NSApplication.sharedApplication.terminate(null); // exit 0: launchd won't restart
}

// ---- loop ---------------------------------------------------------------------

function tick() {
  try {
    const now = Date.now();
    if (now - lastBeat >= HEARTBEAT_MS) {
      writeText(DIR + '/visual-host.alive', String(now));
      lastBeat = now;
    }
    waiting = currentWaiting(now);
    syncScreens();
    renderAurora();
    renderHalo(now);
    renderCritter(now);
    renderOrb();
  } catch (e) {
    console.log('cnw visual host: ' + e);
  }
}

function run(argv) {
  DIR = argv[0];
  CNW = argv[1];
  const app = $.NSApplication.sharedApplication;
  app.setActivationPolicy(1); // NSApplicationActivationPolicyAccessory
  registerMenuTarget();
  makeOrb();
  tick();
  $.NSTimer.scheduledTimerWithTimeIntervalRepeatsBlock(POLL_S, true, () => tick());
  app.run;
}
