// cnw visual host, rung 3: the desk critters (visual attention spec §5.2).
// HostScript appends this file to host.js; host.js's dispatcher calls
// critterBuild, critterArrive and critterDepart. Each critter is drawn in its
// mockup's units (a 260 x 250 box, y down) and scaled by CRITTER_K.

const CRITTER_K = 1.1; // mockup units to points
const CRITTER_MARGIN = 24; // the figure and sign stay this far inside the display
const SIGN_MAX_W = 420;
const GLYPH = {
  question: '?', done_question: '?', plan_approval: '✎', permission: '!', mcp_input: '⇄',
  done: '✓', done_background: '✓', error: '✕', usage_limit: '⏳', idle_reminder: '…',
};

// ---- colours ------------------------------------------------------------------

function mix(hex, to, t) {
  const a = channels(hex);
  const b = channels(to);
  return '#' + a.map((v, i) => Math.round((v + (b[i] - v) * t) * 255).toString(16).padStart(2, '0')).join('');
}

function shade(hex, f) {
  return mix(hex, '#000000', 1 - f);
}

function tint(hex, t) {
  return mix(hex, '#ffffff', t);
}

// paint takes '#RRGGBB' or ['#RRGGBB', alpha].
function paint(v) {
  return Array.isArray(v) ? cgColor(v[0], v[1]) : cgColor(v, 1);
}

// ---- drawing ------------------------------------------------------------------

// svgPath builds a CGPath from SVG path data (M L H V Q C Z, absolute or
// relative), mapping (x, y) to ((x - ox) k, (y - oy) k).
function svgPath(d, ox, oy) {
  const p = $.CGPathCreateMutable();
  const tok = d.match(/[A-Za-z]|-?\d*\.?\d+/g);
  const X = (v) => (v - ox) * CRITTER_K;
  const Y = (v) => (v - oy) * CRITTER_K;
  let i = 0;
  let cmd = '';
  let x = 0;
  let y = 0;
  let sx = 0;
  let sy = 0;
  const n = () => parseFloat(tok[i++]);
  while (i < tok.length) {
    if (/[A-Za-z]/.test(tok[i])) cmd = tok[i++];
    const rel = cmd !== cmd.toUpperCase();
    const bx = rel ? x : 0;
    const by = rel ? y : 0;
    switch (cmd.toUpperCase()) {
      case 'M':
        x = bx + n();
        y = by + n();
        $.CGPathMoveToPoint(p, null, X(x), Y(y));
        sx = x;
        sy = y;
        cmd = rel ? 'l' : 'L';
        break;
      case 'L':
        x = bx + n();
        y = by + n();
        $.CGPathAddLineToPoint(p, null, X(x), Y(y));
        break;
      case 'H':
        x = bx + n();
        $.CGPathAddLineToPoint(p, null, X(x), Y(y));
        break;
      case 'V':
        y = by + n();
        $.CGPathAddLineToPoint(p, null, X(x), Y(y));
        break;
      case 'Q': {
        const x1 = bx + n();
        const y1 = by + n();
        x = bx + n();
        y = by + n();
        $.CGPathAddQuadCurveToPoint(p, null, X(x1), Y(y1), X(x), Y(y));
        break;
      }
      case 'C': {
        const x1 = bx + n();
        const y1 = by + n();
        const x2 = bx + n();
        const y2 = by + n();
        x = bx + n();
        y = by + n();
        $.CGPathAddCurveToPoint(p, null, X(x1), Y(y1), X(x2), Y(y2), X(x), Y(y));
        break;
      }
      case 'Z':
        $.CGPathCloseSubpath(p);
        x = sx;
        y = sy;
        if (i < tok.length && !/[A-Za-z]/.test(tok[i])) return p; // malformed: stop
        break;
      default:
        return p;
    }
  }
  return p;
}

// pen draws into layer in mockup units, with (ox, oy) at the layer's origin.
// Style: {fill, stroke, w, cap, join, alpha, glow: [hex, radius, opacity]};
// colours as paint takes them.
function pen(layer, ox, oy, scale) {
  const k = CRITTER_K;
  const shape = (path, st) => {
    const l = $.CAShapeLayer.layer;
    l.setPath(path);
    l.setFillColor(st.fill ? paint(st.fill) : null);
    if (st.stroke) {
      l.setStrokeColor(paint(st.stroke));
      l.setLineWidth(st.w * k);
      l.setLineCap($(st.cap || 'round'));
      l.setLineJoin($(st.join || 'round'));
    }
    if (st.alpha !== undefined) l.setOpacity(st.alpha);
    if (st.glow) {
      l.setShadowColor(cgColor(st.glow[0], 1));
      l.setShadowRadius(st.glow[1]);
      l.setShadowOpacity(st.glow[2]);
      l.setShadowOffset($.NSMakeSize(0, 0));
      if (st.fill) l.setShadowPath(path);
    }
    layer.addSublayer(l);
    return l;
  };
  const rect = (x, y, w, h) => $.NSMakeRect((x - ox) * k, (y - oy) * k, w * k, h * k);
  return {
    layer: layer,
    scale: scale,
    // at returns a pen on a new zero-size layer at (x, y): transforms on it pivot there.
    at: (x, y) => {
      const l = $.CALayer.layer;
      l.setBounds($.NSMakeRect(0, 0, 0, 0));
      l.setPosition($.NSMakePoint((x - ox) * k, (y - oy) * k));
      layer.addSublayer(l);
      return pen(l, x, y, scale);
    },
    ellipse: (cx, cy, rx, ry, st) => shape($.CGPathCreateWithEllipseInRect(rect(cx - rx, cy - ry, 2 * rx, 2 * ry), null), st),
    circle: (cx, cy, r, st) => shape($.CGPathCreateWithEllipseInRect(rect(cx - r, cy - r, 2 * r, 2 * r), null), st),
    rect: (x, y, w, h, r, st) => {
      const rr = Math.min(r, w / 2, h / 2) * k;
      return shape($.CGPathCreateWithRoundedRect(rect(x, y, w, h), rr, rr, null), st);
    },
    path: (d, st) => shape(svgPath(d, ox, oy), st),
    // text centres str on (cx, cy); size is in points.
    text: (str, cx, cy, size, hex, weight) => {
      const l = textLine(str, size, weight, hex, SIGN_MAX_W, scale);
      l.setPosition($.NSMakePoint((cx - ox) * k, (cy - oy) * k));
      layer.addSublayer(l);
      return l;
    },
  };
}

// textLine is one line of text, truncated with … to fit maxW points.
// CATextLayer measures wrongly once its font is set, so it uses an
// attributed string; its own truncation drew nothing, so this shortens the
// string itself.
function textLine(str, size, weight, hex, maxW, scale) {
  const l = $.CATextLayer.layer;
  const font = $.NSFont.systemFontOfSizeWeight(size, weight);
  const color = rgb(hex, 1);
  const set = (s) => {
    l.setString(attributed(s, color, s.length, font));
    return l.preferredFrameSize.width + 2;
  };
  if (set(str) > maxW) {
    let lo = 0;
    let hi = str.length;
    while (lo < hi) {
      const mid = Math.ceil((lo + hi) / 2);
      if (set(str.slice(0, mid).trimEnd() + '…') <= maxW) lo = mid;
      else hi = mid - 1;
    }
    set(str.slice(0, lo).trimEnd() + '…');
  }
  l.setAlignmentMode($('center'));
  l.setContentsScale(scale);
  const sz = l.preferredFrameSize;
  l.setBounds($.NSMakeRect(0, 0, Math.ceil(sz.width) + 2, Math.ceil(sz.height)));
  return l;
}

function category(e) {
  return e.category.replace(/_/g, ' ');
}

function glyphTitle(e) {
  return (GLYPH[e.category] || '!') + ' ' + category(e);
}

// sign draws a board on a stick (or, with hang, a board whose top edge is at
// top) pivoting at (x, pivot), and returns {layer, tilt, cx, w}: its centre
// and width in points.
function sign(p, e, o) {
  const piv = p.at(o.x, o.pivot);
  if (!o.hang) piv.rect(o.x - o.stickW / 2, o.top, o.stickW, o.bottom - o.top, o.stickW / 2, { fill: o.stick });
  const pad = 16;
  const inner = SIGN_MAX_W - 2 * pad;
  const title = textLine(o.title, 20, 0.3, o.titleColor, inner, p.scale);
  const sub = textLine(e.label, 15, 0.23, o.subColor || '#333333', inner, p.scale);
  const w = Math.max(title.bounds.size.width, sub.bounds.size.width, o.minW * CRITTER_K) + 2 * pad;
  const h = title.bounds.size.height + sub.bounds.size.height + 14;
  const board = $.CALayer.layer;
  board.setBounds($.NSMakeRect(0, 0, w, h));
  board.setBackgroundColor(paint(o.fill));
  board.setBorderColor(cgColor(e.color, 1));
  board.setBorderWidth(3);
  board.setCornerRadius(o.radius === 'pill' ? h / 2 : o.radius * CRITTER_K);
  const topY = (o.top - o.pivot) * CRITTER_K;
  board.setPosition($.NSMakePoint(0, o.hang ? topY + h / 2 : topY - h / 2 + 4));
  title.setPosition($.NSMakePoint(w / 2, 7 + title.bounds.size.height / 2));
  sub.setPosition($.NSMakePoint(w / 2, h - 7 - sub.bounds.size.height / 2));
  board.addSublayer(title);
  board.addSublayer(sub);
  piv.layer.addSublayer(board);
  const tilt = o.tilt || 0;
  if (tilt) piv.layer.setTransform($.CATransform3DMakeRotation(tilt, 0, 0, 1));
  // reach: how far the board's far edge is from the pivot, for the swing allowance.
  return { layer: piv.layer, tilt: tilt, cx: o.x * CRITTER_K, w: w, reach: Math.abs(topY) + h };
}

// ---- motion helpers -------------------------------------------------------------

function keyframes(l, path, values, times, dur, key, repeatFor) {
  const a = $.CAKeyframeAnimation.animationWithKeyPath($(path));
  a.setValues($(values.map(num)));
  a.setKeyTimes($(times.map(num)));
  a.setDuration(dur);
  if (repeatFor) a.setRepeatDuration(repeatFor);
  else a.setRepeatCount(1e9);
  l.addAnimationForKey(a, $(key));
}

// blinkEvery squashes eye shut briefly once every period seconds; shut is the
// closed scale.
function blinkEvery(eye, period, shut) {
  keyframes(eye, 'transform.scale.y', [1, 1, shut, 1], [0, 0.9, 0.95, 1], period, 'blink');
}

function bob(l, rise, half) {
  loop(l, 'transform.translation.y', 0, -rise * CRITTER_K, half, 'bob');
}

function spin(l, period, dir, key, repeatFor) {
  const a = $.CABasicAnimation.animationWithKeyPath($('transform.rotation.z'));
  a.setFromValue(num(0));
  a.setToValue(num(dir * 2 * Math.PI));
  a.setDuration(period);
  if (repeatFor) a.setRepeatDuration(repeatFor);
  else a.setRepeatCount(1e9);
  l.addAnimationForKey(a, $(key));
}

function sway(s, amp, half) {
  loop(s.layer, 'transform.rotation.z', s.tilt - amp, s.tilt + amp, half, 'sway');
}

// step alternates two feet up and down for dur seconds.
function step(feet, lift, half, dur) {
  loop(feet[0], 'transform.translation.y', 0, -lift * CRITTER_K, half, 'step', dur);
  loop(feet[1], 'transform.translation.y', -lift * CRITTER_K, 0, half, 'step', dur);
}

// slide moves the critter's box along keyPath (position.x or position.y).
function slide(l, keyPath, from, to, dur, timing) {
  const a = $.CABasicAnimation.animationWithKeyPath($(keyPath));
  a.setFromValue(num(from));
  a.setToValue(num(to));
  a.setDuration(dur);
  a.setTimingFunction($.CAMediaTimingFunction.functionWithName($(timing || 'easeInEaseOut')));
  noAnim(() => {
    const p = l.position;
    l.setPosition(keyPath === 'position.x' ? $.NSMakePoint(to, p.y) : $.NSMakePoint(p.x, to));
  });
  l.addAnimationForKey(a, $(keyPath));
}

function travelTime(dist) {
  return Math.min(4.5, Math.max(1.4, Math.abs(dist) / 650));
}

// ---- the critters ---------------------------------------------------------------
// Each builder draws into p (the figure's pen) and returns its parts: sign,
// always() (runs from entry), gait(dur, dir) (while travelling), arrive(dir)
// (once, on stopping) and idle() (while waiting). Reduce Motion skips all four.

function buildBlob(p, e) {
  const c = e.color;
  const dark = shade(c, 0.81);
  p.ellipse(110, 240, 58, 7, { fill: ['#000000', 0.45] });
  const all = p.at(0, 0);
  const sg = sign(all, e, { x: 150, top: 80, bottom: 175, pivot: 160, stick: '#8a5a2b', stickW: 5, fill: '#ffffff',
    radius: 9, tilt: 0.07, minW: 128, title: glyphTitle(e), titleColor: dark });
  const body = all.at(110, 236);
  body.path('M70 120 Q 72 104 80 98', { stroke: dark, w: 4 });
  const bulb = body.circle(80, 96, 13, { fill: tint(c, 0.6), glow: [c, 12, 1] });
  body.circle(80, 96, 5, { fill: '#ffffff' });
  body.ellipse(110, 180, 62, 56, { fill: shade(c, 0.8) });
  body.ellipse(106, 175, 57, 51, { fill: c });
  body.ellipse(98, 160, 38, 30, { fill: [tint(c, 0.45), 0.55] });
  body.ellipse(88, 150, 20, 11, { fill: ['#ffffff', 0.35] });
  const eyes = [92, 130].map((x) => {
    const g = body.at(x, 176);
    g.circle(x, 176, 15, { fill: '#ffffff' });
    g.circle(x + 4, 179, 8, { fill: '#1d1d1f' });
    g.circle(x + 7, 175, 3, { fill: '#ffffff' });
    return g.layer;
  });
  body.ellipse(78, 198, 9, 5, { fill: ['#ff7a8a', 0.55] });
  body.ellipse(146, 198, 9, 5, { fill: ['#ff7a8a', 0.55] });
  body.path('M104 202 Q 112 210 120 202', { stroke: '#1d1d1f', w: 3 });
  body.path('M166 180 Q 178 170 150 150', { stroke: dark, w: 7 });
  const feet = [88, 132].map((x) => {
    const f = all.at(x, 234);
    f.ellipse(x, 234, 16, 7, { fill: shade(c, 0.62) });
    return f.layer;
  });
  return {
    sign: sg,
    always: () => {
      loop(bulb, 'opacity', 1, 0.35, 0.6, 'pulse');
      for (const eye of eyes) blinkEvery(eye, 4, 0.1);
    },
    gait: (dur) => {
      loop(body.layer, 'transform.scale.y', 1, 0.9, 0.18, 'squash', dur);
      step(feet, 6, 0.18, dur);
    },
    idle: () => {
      bob(all.layer, 6, 0.8);
      sway(sg, 0.05, 1.2);
    },
  };
}

function buildGhost(p, e) {
  const c = e.color;
  const hem = 'M55 150 Q 55 88 105 88 Q 155 88 155 150 L 155 208 Q 145 222 135 208 Q 125 194 115 208 ' +
    'Q 105 222 95 208 Q 85 194 75 208 Q 65 222 55 208 Z';
  const shadow = p.at(105, 242);
  shadow.ellipse(105, 242, 40, 5, { fill: ['#000000', 0.35] });
  const all = p.at(0, 0);
  all.circle(105, 150, 72, { fill: [c, 0.1] });
  all.circle(105, 150, 56, { fill: [c, 0.12] });
  // The body fades from cream to a tint of the colour: a gradient masked to the hem shape.
  all.path(hem, { fill: tint(c, 0.62), glow: [c, 16, 0.9] });
  const k = CRITTER_K;
  const fadeL = $.CAGradientLayer.layer;
  fadeL.setFrame($.NSMakeRect(55 * k, 88 * k, 100 * k, 134 * k));
  fadeL.setColors($([cgColor('#fff8ea', 1), cgColor(tint(c, 0.62), 1)]));
  const mask = $.CAShapeLayer.layer;
  mask.setPath(svgPath(hem, 55, 88));
  fadeL.setMask(mask);
  all.layer.addSublayer(fadeL);
  all.path(hem, { stroke: c, w: 3 });
  const eyes = [88, 122].map((x) => {
    const g = all.at(x, 140);
    g.ellipse(x, 140, 9, 13, { fill: '#1d1d1f' });
    g.circle(x + 3, 135, 3, { fill: '#ffffff' });
    return g.layer;
  });
  all.ellipse(105, 168, 7, 9, { fill: '#1d1d1f' });
  all.path('M152 156 Q 170 150 176 128', { stroke: c, w: 6 });
  const sg = sign(all, e, { x: 182, top: 72, bottom: 134, pivot: 134, stick: [c, 0.8], stickW: 4, fill: '#fff8ea',
    radius: 'pill', tilt: -0.09, minW: 124, title: 'psst… ' + category(e), titleColor: shade(c, 0.81), subColor: '#555555' });
  return {
    sign: sg,
    always: () => {
      bob(all.layer, 14, 1.1);
      loop(shadow.layer, 'transform.scale.x', 1, 0.75, 1.1, 'pulse');
      for (const eye of eyes) blinkEvery(eye, 4.4, 0.1);
    },
    gait: () => {},
    idle: () => sway(sg, 0.03, 1.6),
  };
}

function buildCat(p, e) {
  const c = e.color;
  const deep = shade(c, 0.92);
  const cream = tint(c, 0.62);
  p.ellipse(100, 242, 60, 6, { fill: ['#000000', 0.45] });
  const all = p.at(0, 0);
  const tail = all.at(150, 226);
  tail.path('M150 226 Q 196 222 186 180 Q 180 160 196 150', { stroke: deep, w: 12 });
  const body = all.at(100, 240);
  body.ellipse(100, 196, 54, 44, { fill: c });
  body.ellipse(100, 206, 30, 26, { fill: cream });
  body.path('M62 108 L 70 72 L 92 98 Z M138 108 L 130 72 L 108 98 Z', { fill: deep });
  body.path('M68 102 L 72 84 L 84 98 Z M132 102 L 128 84 L 116 98 Z', { fill: '#ffb3a7' });
  body.circle(100, 128, 42, { fill: c });
  body.path('M70 108 L 78 116 M 84 100 L 86 112', { stroke: shade(c, 0.81), w: 3 });
  const eyes = [84, 116].map((x) => {
    const g = body.at(x, 128);
    g.ellipse(x, 128, 8, 11, { fill: '#1d1d1f' });
    g.circle(x + 2, 124, 3, { fill: '#ffffff' });
    return g.layer;
  });
  body.path('M96 142 L 104 142 L 100 147 Z', { fill: '#ff7a8a' });
  body.path('M100 147 Q 94 154 88 150 M100 147 Q 106 154 112 150', { stroke: '#1d1d1f', w: 2 });
  body.path('M60 140 L 36 136 M60 146 L 38 150 M140 140 L 164 136 M140 146 L 162 150', { stroke: '#ffffff', w: 1.5, alpha: 0.8 });
  const paws = [78, 122].map((x) => {
    const f = all.at(x, 236);
    f.ellipse(x, 236, 14, 7, { fill: cream });
    return f.layer;
  });
  const sg = sign(all, e, { x: 196, top: 70, bottom: 140, pivot: 140, stick: '#8a5a2b', stickW: 4, fill: '#ffffff',
    radius: 6, minW: 112, title: glyphTitle(e), titleColor: shade(c, 0.81) });
  all.path('M140 196 Q 170 170 192 146', { stroke: c, w: 10 });
  return {
    sign: sg,
    always: () => {
      for (const eye of eyes) blinkEvery(eye, 3.5, 0.09);
    },
    gait: (dur) => {
      step(paws, 5, 0.22, dur);
      loop(body.layer, 'transform.translation.y', 0, -3 * CRITTER_K, 0.22, 'pad', dur);
    },
    // Sitting down: the body settles and squashes a little.
    arrive: () => keyframes(body.layer, 'transform.scale.y', [1, 0.93, 1.02, 1], [0, 0.4, 0.75, 1], 0.6, 'sit', 0.6),
    idle: () => {
      keyframes(tail.layer, 'transform.rotation.z', [0, -0.17, 0], [0, 0.5, 1], 1.8, 'swish');
      keyframes(sg.layer, 'transform.rotation.z', [0, -0.1, 0, 0.1, 0], [0, 0.25, 0.5, 0.75, 1], 3, 'sway');
    },
  };
}

function buildDeskbot(p, e) {
  const c = e.color;
  const steel = '#9aa0ab';
  p.ellipse(105, 243, 36, 5, { fill: ['#000000', 0.45] });
  const lean = p.at(105, 239);
  const all = lean.at(105, 239);
  const wheel = all.at(105, 222);
  wheel.circle(105, 222, 17, { fill: '#3a3f48', stroke: '#5c636f', w: 3 });
  wheel.path('M105 209 V 235 M92 222 H 118', { stroke: '#5c636f', w: 2.5 });
  wheel.circle(105, 222, 5, { fill: steel });
  all.rect(74, 160, 62, 54, 26, { fill: '#dfe3e9', stroke: '#b5bbc5', w: 2 });
  all.rect(78, 162, 54, 28, 14, { fill: ['#ffffff', 0.9] });
  const heart = all.circle(105, 186, 9, { fill: c, glow: [c, 8, 0.9] });
  all.rect(99, 146, 12, 16, 4, { fill: steel });
  all.rect(52, 78, 106, 74, 34, { fill: '#dfe3e9', stroke: '#b5bbc5', w: 2 });
  all.rect(56, 80, 98, 40, 30, { fill: ['#ffffff', 0.9] });
  all.rect(64, 90, 82, 50, 24, { fill: '#14161a' });
  const eyes = [88, 122].map((x) => {
    const g = all.at(x, 115);
    g.ellipse(x, 115, 11, 14, { fill: [c, 0.7], glow: [c, 9, 1] });
    g.ellipse(x, 115, 6.5, 9, { fill: tint(c, 0.6) });
    return g.layer;
  });
  all.path('M105 78 L 112 60', { stroke: steel, w: 3 });
  all.circle(113, 57, 5, { fill: c });
  all.path('M136 178 Q 160 168 170 140', { stroke: '#cfd4dc', w: 9 });
  const sg = sign(all, e, { x: 178, top: 78, bottom: 142, pivot: 144, stick: steel, stickW: 4, fill: '#ffffff',
    radius: 12, tilt: 0.09, minW: 120, title: glyphTitle(e), titleColor: shade(c, 0.81) });
  return {
    sign: sg,
    always: () => {
      for (const eye of eyes) blinkEvery(eye, 3.2, 0.14);
      loop(heart, 'opacity', 1, 0.45, 0.65, 'pulse');
    },
    gait: (dur, dir) => {
      spin(wheel.layer, 0.6, dir, 'roll', dur);
      loop(lean.layer, 'transform.rotation.z', 0.05 * dir, 0.07 * dir, 0.3, 'tilt', dur);
    },
    // Leaning forward as it brakes, then settling back.
    arrive: (dir) => keyframes(lean.layer, 'transform.rotation.z', [0.06 * dir, 0.16 * dir, -0.04 * dir, 0], [0, 0.35, 0.7, 1], 0.8, 'brake', 0.8),
    idle: () => {
      bob(all.layer, 4, 0.7);
      sway(sg, 0.045, 1.1);
    },
  };
}

function buildTintoy(p, e) {
  const c = e.color;
  const red = '#c94f3d';
  const rim = '#8f2f22';
  const cream = '#f4d9a8';
  p.ellipse(105, 243, 50, 6, { fill: ['#000000', 0.45] });
  const all = p.at(105, 240);
  const key = all.at(150, 150);
  key.rect(146, 134, 8, 32, 3, { fill: '#b9802a' });
  key.rect(136, 144, 28, 12, 6, { fill: '#d9a441' });
  all.rect(62, 70, 86, 64, 8, { fill: red, stroke: rim, w: 3 });
  for (const [x, y] of [[68, 76], [142, 76], [68, 128], [142, 128]]) all.circle(x, y, 2.5, { fill: cream });
  for (const x of [88, 122]) {
    all.circle(x, 96, 12, { fill: cream, stroke: rim, w: 3 });
    all.circle(x, 97, 5, { fill: '#1d1d1f' });
  }
  all.rect(84, 114, 42, 12, 3, { fill: '#2b2b2b' });
  all.path('M90 114 V126 M98 114 V126 M106 114 V126 M114 114 V126 M122 114 V126', { stroke: '#888888', w: 2, cap: 'butt' });
  all.rect(70, 138, 70, 74, 8, { fill: red, stroke: rim, w: 3 });
  all.circle(105, 170, 20, { fill: cream, stroke: c, w: 3, glow: [c, 10, 0.9] });
  all.text(GLYPH[e.category] || '!', 105, 171, 22, shade(c, 0.81), 0.6);
  const needle = all.at(105, 170);
  needle.path('M105 170 L 117 158', { stroke: '#1d1d1f', w: 2 });
  const legs = [78, 112].map((x) => {
    const g = all.at(x + 10, 238);
    g.rect(x, 212, 20, 26, 4, { fill: '#3a3a3a' });
    return g.layer;
  });
  // Its stick is longer than the mockup's so the wider sign clears the head.
  const sg = sign(all, e, { x: 196, top: 58, bottom: 164, pivot: 164, stick: '#6b4a22', stickW: 4, fill: cream,
    radius: 4, minW: 116, title: category(e).toUpperCase() + (GLYPH[e.category] || '!'), titleColor: rim });
  // The accordion arm reaches the stick (the mockup's stops short of it).
  all.path('M140 160 l8 -6 l8 6 l8 -6 l8 6 l8 -6 l8 6 l8 -6', { stroke: '#9aa0ab', w: 5 });
  const rock = (amp, half, repeatFor) => loop(all.layer, 'transform.rotation.z', -amp, amp, half, 'rock', repeatFor);
  return {
    sign: sg,
    always: () => {
      spin(key.layer, 2, 1, 'wind');
      keyframes(needle.layer, 'transform.rotation.z', [0, 0.7, 0.45, 0.7, 0], [0, 0.3, 0.5, 0.65, 1], 1, 'twitch');
    },
    gait: (dur) => {
      rock(0.07, 0.16, dur);
      step(legs, 5, 0.16, dur);
    },
    idle: () => rock(0.035, 0.3),
  };
}

function buildDrone(p, e) {
  const c = e.color;
  p.ellipse(130, 243 + DRONE_LIFT / CRITTER_K, 34, 4, { fill: ['#000000', 0.3] });
  const all = p.at(0, 0);
  const rotors = [80, 180].map((x) => {
    const g = all.at(x, 66);
    g.ellipse(x, 66, 30, 6, { stroke: '#9aa0ab', w: 2, alpha: 0.7 });
    return g.layer;
  });
  all.path('M80 66 L 106 84 M180 66 L 154 84', { stroke: '#5c636f', w: 5 });
  const sg = sign(all, e, { x: 130, top: 168, pivot: 132, hang: true, fill: '#ffffff', radius: 8, minW: 128,
    title: glyphTitle(e), titleColor: shade(c, 0.81) });
  // The cables swing with the sign.
  pen(sg.layer, 130, 132, p.scale).path('M112 132 L 92 168 M148 132 L 168 168', { stroke: '#9aa0ab', w: 2 });
  all.circle(130, 100, 34, { fill: '#2b2f36', stroke: '#5c636f', w: 3 });
  const iris = all.at(130, 100);
  iris.circle(130, 100, 18, { fill: shade(c, 0.5), glow: [c, 10, 0.9] });
  iris.circle(130, 100, 15, { fill: c });
  iris.circle(130, 100, 10, { fill: tint(c, 0.45) });
  iris.circle(130, 100, 4.5, { fill: '#ffffff' });
  all.circle(124, 94, 4, { fill: ['#ffffff', 0.9] });
  const tally = all.circle(104, 78, 3, { fill: '#f04444' });
  return {
    sign: sg,
    always: () => {
      loop(rotors[0], 'transform.scale.x', 1, 0.73, 0.075, 'blur');
      loop(rotors[1], 'transform.scale.x', 0.73, 1, 0.075, 'blur');
      loop(tally, 'opacity', 1, 0, 0.5, 'tally');
      loop(iris.layer, 'transform.scale', 1, 0.78, 1.2, 'breathe');
      bob(all.layer, 10, 0.9);
    },
    gait: () => {},
    idle: () => loop(sg.layer, 'transform.rotation.z', -0.05, 0.05, 0.9, 'sway'),
  };
}

const DRONE_LIFT = 83; // the drone's sign hangs about 120 pt above the bottom edge
const WAVE = 0.35; // the goodbye wave's swing either side of the sign's tilt, radians

// CRITTERS: per id, the builder, how it moves, the body's centre and the
// figure's left and right edges without the sign (mockup units), how high it
// rests above the bottom edge (points) and, for the tin toy, its rock (radians).
const CRITTERS = {
  blob: { build: buildBlob, move: 'ground', bodyX: 110, minX: 48, maxX: 178, lift: 0 },
  ghost: { build: buildGhost, move: 'float', bodyX: 105, minX: 33, maxX: 180, lift: 0 },
  cat: { build: buildCat, move: 'ground', bodyX: 100, minX: 36, maxX: 202, lift: 0 },
  deskbot: { build: buildDeskbot, move: 'ground', bodyX: 105, minX: 52, maxX: 172, lift: 0 },
  tintoy: { build: buildTintoy, move: 'ground', bodyX: 105, minX: 55, maxX: 200, lift: 0, rock: 0.07 },
  drone: { build: buildDrone, move: 'fly', bodyX: 130, minX: 50, maxX: 210, lift: DRONE_LIFT },
};

// ---- the dispatcher's entry points ------------------------------------------------

// critterBuild draws critter id for e on overlay o, off screen until
// critterArrive. Its box is a zero-size layer at the body's bottom centre.
function critterBuild(id, e, o) {
  const def = CRITTERS[id];
  const k = CRITTER_K;
  const box = $.CALayer.layer;
  box.setBounds($.NSMakeRect(0, 0, 0, 0));
  const fig = $.CALayer.layer;
  fig.setGeometryFlipped(true);
  fig.setAnchorPoint($.NSMakePoint(0, 0));
  fig.setBounds($.NSMakeRect(0, 0, 260 * k, 250 * k));
  fig.setPosition($.NSMakePoint(-def.bodyX * k, 0));
  box.addSublayer(fig);
  const parts = def.build(pen(fig, 0, 0, o.frame.scale), e);
  const s = parts.sign;
  // The sign swings sideways by up to reach × sin(tilt + wave) (the wave is the
  // widest swing; every idle sway is smaller), plus the tin toy's whole-body rock.
  const swing = s.reach * Math.sin(Math.abs(s.tilt) + WAVE) + 270 * Math.sin(def.rock || 0);
  const left = Math.max((def.bodyX - def.minX) * k, def.bodyX * k - (s.cx - s.w / 2) + swing);
  const right = Math.max((def.maxX - def.bodyX) * k, s.cx + s.w / 2 - def.bodyX * k + swing);
  return { overlay: o, box: box, def: def, parts: parts, left: left, right: right, gone: false };
}

// critterArrive adds c to its overlay and brings it to rest below pointer x px.
function critterArrive(c, px, reduce) {
  const W = c.overlay.frame.w;
  const x = Math.min(Math.max(px, CRITTER_MARGIN + c.left), W - CRITTER_MARGIN - c.right);
  const lift = c.def.lift;
  const fromLeft = px < W / 2;
  c.overlay.root.addSublayer(c.box);
  if (reduce) {
    noAnim(() => c.box.setPosition($.NSMakePoint(x, lift)));
    fadeIn(c.box);
    return;
  }
  c.parts.always();
  const done = (dur, dir) =>
    after(dur, () => {
      if (c.gone) return;
      if (c.parts.arrive) c.parts.arrive(dir);
      c.parts.idle();
    });
  if (c.def.move === 'float') {
    // Rises from the bottom edge near the side it comes from, then drifts over.
    const sx = fromLeft ? CRITTER_MARGIN + c.left : W - CRITTER_MARGIN - c.right;
    const rise = 1.4;
    const drift = Math.abs(x - sx) < 4 ? 0 : travelTime(x - sx);
    noAnim(() => c.box.setPosition($.NSMakePoint(sx, -280 * CRITTER_K)));
    slide(c.box, 'position.y', -280 * CRITTER_K, lift, rise, 'easeOut');
    if (drift) after(rise, () => c.gone || slide(c.box, 'position.x', sx, x, drift));
    done(rise + drift, x >= sx ? 1 : -1);
    return;
  }
  const sx = fromLeft ? -c.right - 30 : W + c.left + 30;
  const dir = x >= sx ? 1 : -1;
  const dur = travelTime(x - sx);
  noAnim(() => c.box.setPosition($.NSMakePoint(sx, lift)));
  if (c.def.move === 'fly') {
    // Comes in higher and descends to its hover height.
    slide(c.box, 'position.x', sx, x, dur);
    slide(c.box, 'position.y', lift + 140, lift, dur, 'easeOut');
  } else {
    slide(c.box, 'position.x', sx, x, dur, 'easeOut');
  }
  c.parts.gait(dur, dir);
  done(dur, dir);
}

// critterDepart waves c goodbye and sends it off toward the nearer edge,
// returning how long that takes, ms.
function critterDepart(c, reduce) {
  c.gone = true;
  if (reduce) {
    fadeOut(c.box);
    return 350;
  }
  const s = c.parts.sign;
  s.layer.removeAnimationForKey($('sway'));
  loop(s.layer, 'transform.rotation.z', s.tilt - WAVE, s.tilt + WAVE, 0.2, 'wave', 1.2);
  const W = c.overlay.frame.w;
  const pres = c.box.presentationLayer;
  const from = pres.isNil() ? c.box.position : pres.position;
  // Freeze any unfinished entry where it is, so it waves in place.
  c.box.removeAnimationForKey($('position.x'));
  c.box.removeAnimationForKey($('position.y'));
  noAnim(() => c.box.setPosition($.NSMakePoint(from.x, from.y)));
  const toRight = from.x >= W / 2;
  const end = toRight ? W + c.left + 40 : -c.right - 40;
  const dur = travelTime(end - from.x);
  after(1.2, () => {
    slide(c.box, 'position.x', from.x, end, dur, 'easeIn');
    if (c.def.move === 'float') {
      slide(c.box, 'position.y', from.y, from.y + 320, dur, 'easeIn');
      fade(c.box, 1, 0, dur);
    } else if (c.def.move === 'fly') {
      slide(c.box, 'position.y', from.y, from.y + 260, dur, 'easeIn');
    }
    c.parts.gait(dur, toRight ? 1 : -1);
    after(dur + 0.1, () => c.box.removeFromSuperlayer);
  });
  return Math.round((1.2 + dur + 0.2) * 1000);
}
