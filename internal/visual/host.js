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
let waiting = []; // {id, category, label, color, maxRung, since, halo, critter, look}, oldest first
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

// channels splits '#RRGGBB' into red, green and blue in 0..1.
function channels(hex) {
  const n = parseInt(hex.slice(1), 16);
  return [((n >> 16) & 255) / 255, ((n >> 8) & 255) / 255, (n & 255) / 255];
}

function rgb(hex, alpha) {
  const c = channels(hex);
  return $.NSColor.colorWithSRGBRedGreenBlueAlpha(c[0], c[1], c[2], alpha);
}

// cgColor is rgb as a CGColor for layers. Reading NSColor.CGColor through
// the JXA bridge kills osascript, so it builds the CGColor directly.
function cgColor(hex, alpha) {
  const c = channels(hex);
  return $.CGColorCreateSRGB(c[0], c[1], c[2], alpha);
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

// guarded wraps a callback that AppKit or a timer calls, so an exception is
// logged instead of escaping into the run loop (where it would stop a timer).
function guarded(fn) {
  return function () {
    try {
      return fn.apply(null, arguments);
    } catch (e) {
      console.log('cnw visual host: ' + e);
    }
  };
}

// timer schedules fn every (or once after) seconds in the common run-loop
// modes, so it keeps firing while the orb's menu is open.
function timer(seconds, repeats, fn) {
  const t = $.NSTimer.timerWithTimeIntervalRepeatsBlock(seconds, repeats, () => fn());
  $.NSRunLoop.mainRunLoop.addTimerForMode(t, $.NSRunLoopCommonModes);
  return t;
}

function after(seconds, fn) {
  timer(seconds, false, guarded(fn));
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
      if (w.clear_after_ms > 0 && now - w.since_ms > w.clear_after_ms) continue;
      out.push({ id: w.session_id, category: w.category, label: w.label || 'session', color: w.color,
        maxRung: w.max_rung, since: w.since_ms, halo: snap.halo_after_ms, critter: snap.critter_after_ms,
        look: snap.critter });
    }
  }
  const p = readJSON(DIR + '/visual-preview.json');
  if (p && now - p.started_ms < PREVIEW.end) {
    out.push({ id: '', category: p.category, label: p.label || 'preview', color: p.color,
      maxRung: p.max_rung, since: p.started_ms, halo: PREVIEW.halo, critter: PREVIEW.critter, look: p.critter });
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
  const reduce = reduceMotion();
  const key = top.color + '|' + text + '|' + reduce;
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
    noAnim(() => ring.setPosition($.NSMakePoint(0, 0)));
    if (!reduce) loop(ring, 'opacity', 1, 0.45, 0.9, 'breathe');
    halo = { box: box, ring: ring, label: label, key: key, overlay: null, left: null };
  }
  // The poll restarts the 60 Hz timer once the pointer moves again.
  if (followPointer() && !haloTimer) {
    haloTimer = timer(1 / 60, true, haloFrame);
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
  // The label sits right of the ring, or left of it where it would run off the display.
  const w = halo.label.bounds.size.width;
  const left = p.x + HALO_R + 8 + w > p.o.frame.w;
  noAnim(() => {
    if (moved) {
      halo.box.removeFromSuperlayer;
      p.o.root.addSublayer(halo.box);
      halo.label.setContentsScale(p.o.frame.scale);
      halo.overlay = p.o;
    }
    if (left !== halo.left) {
      halo.label.setPosition($.NSMakePoint((left ? -1 : 1) * (HALO_R + 8 + w / 2), -HALO_R));
      halo.left = left;
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
// The critters themselves are in critters.js, appended to this script.

let critter = null; // see critterBuild, plus key
let critterGone = 0; // when the leaving critter is off screen, ms

function critterKey(e) {
  return e.id + '|' + e.category + '|' + e.since;
}

let lastRandom = ''; // the previous random pick, never repeated

// critterId resolves the configured look; random (or anything unknown) picks
// one per appearance, never the same as the previous random pick.
function critterId(look) {
  if (CRITTERS[look]) return look;
  let ids = Object.keys(CRITTERS);
  if (ids.length > 1) ids = ids.filter((id) => id !== lastRandom);
  lastRandom = ids[Math.floor(Math.random() * ids.length)];
  return lastRandom;
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
  critter = critterBuild(critterId(e.look), e, o);
  critter.key = critterKey(e);
  critterArrive(critter, p ? p.x : o.frame.w / 2, reduceMotion());
}

// critterLeave sends the critter off and returns how long that takes, ms.
function critterLeave() {
  const c = critter;
  critter = null;
  return critterDepart(c, reduceMotion());
}

// ---- orb ----------------------------------------------------------------------

function registerMenuTarget() {
  ObjC.registerSubclass({
    name: 'CNWMenuTarget',
    protocols: ['NSMenuDelegate'],
    methods: {
      'menuNeedsUpdate:': { types: ['void', ['id']], implementation: guarded((menu) => fillMenu(menu)) },
      'jump:': {
        types: ['void', ['id']],
        implementation: guarded((item) => launch(['visual', 'jump', ObjC.unwrap(item.representedObject)])),
      },
      'quit:': { types: ['void', ['id']], implementation: guarded(() => quit()) },
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
  statusItem.button.setAttributedTitle(attributed(text, color, text.length, $.NSFont.menuBarFontOfSize(0)));
}

// attributed returns text in font with its first len characters in color.
// JXA doesn't bridge the NSAttributedString init methods, so it fills an
// empty string.
function attributed(text, color, len, font) {
  const s = $.NSMutableAttributedString.alloc.init;
  s.replaceCharactersInRangeWithString($.NSMakeRange(0, 0), $(text));
  s.addAttributesRange($.NSDictionary.dictionaryWithObjectForKey(font, $.NSFontAttributeName), $.NSMakeRange(0, s.length));
  s.addAttributesRange($.NSDictionary.dictionaryWithObjectForKey(color, $.NSForegroundColorAttributeName), $.NSMakeRange(0, len));
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
    it.setAttributedTitle(attributed(title, rgb(e.color, 1), 1, $.NSFont.menuFontOfSize(0)));
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
  timer(POLL_S, true, tick);
  app.run;
}
