// Pure presentation and curve rules, shared by QML and hardware-free tests.

var tempMin = 30
var tempMax = 100
var fanMin = 0
var fanMax = 100

var profiles = [
  { value: "quiet", label: "Quiet", icon: "\u{F032A}" },
  { value: "balanced", label: "Balanced", icon: "\u{F029A}" },
  { value: "blast", label: "Blast", icon: "\u{F04C5}" },
  { value: "custom", label: "Custom", icon: "\u{F062E}" }
]

var presets = {
  quiet: [{ t: 50, f: 20 }, { t: 75, f: 45 }, { t: 90, f: 85 }],
  balanced: [{ t: 45, f: 25 }, { t: 68, f: 55 }, { t: 85, f: 90 }],
  blast: [{ t: 35, f: 55 }, { t: 55, f: 85 }, { t: 70, f: 100 }]
}

function clone(points) {
  return points.map(function(p) { return { t: p.t, f: p.f } })
}

function clamp(value, low, high) {
  return Math.max(low, Math.min(high, value))
}

function profileLabel(value) {
  for (var i = 0; i < profiles.length; i++)
    if (profiles[i].value === value) return profiles[i].label
  return "Custom"
}

// Keep points ordered and at least `gap` degrees apart so the curve stays a function.
function constrain(points, index, t, f, gap) {
  var g = gap || 5
  var low = index > 0 ? points[index - 1].t + g : tempMin
  var high = index < points.length - 1 ? points[index + 1].t - g : tempMax
  var next = clone(points)
  next[index] = { t: Math.round(clamp(t, low, high)), f: Math.round(clamp(f, fanMin, fanMax)) }
  return next
}

// Fritsch-Carlson tangents: a smooth curve that never overshoots its points,
// so fan speed never drops while temperature rises.
function tangents(points) {
  var n = points.length
  var d = [], m = []
  for (var i = 0; i < n - 1; i++)
    d.push((points[i + 1].f - points[i].f) / Math.max(1e-6, points[i + 1].t - points[i].t))
  for (var j = 0; j < n; j++) {
    if (j === 0) m.push(n > 1 ? d[0] : 0)
    else if (j === n - 1) m.push(d[n - 2])
    else if (d[j - 1] * d[j] <= 0) m.push(0)
    else m.push((d[j - 1] + d[j]) / 2)
  }
  for (var k = 0; k < n - 1; k++) {
    if (d[k] === 0) { m[k] = 0; m[k + 1] = 0; continue }
    var a = m[k] / d[k], b = m[k + 1] / d[k], s = a * a + b * b
    if (s > 9) {
      var tau = 3 / Math.sqrt(s)
      m[k] = tau * a * d[k]
      m[k + 1] = tau * b * d[k]
    }
  }
  return m
}

// Cubic Bezier segments between consecutive points: [{ p0, c1, c2, p1 }].
function segments(points) {
  var m = tangents(points)
  var out = []
  for (var i = 0; i < points.length - 1; i++) {
    var a = points[i], b = points[i + 1], h = (b.t - a.t) / 3
    out.push({
      p0: a,
      c1: { t: a.t + h, f: a.f + m[i] * h },
      c2: { t: b.t - h, f: b.f - m[i + 1] * h },
      p1: b
    })
  }
  return out
}

// Fan percent for a temperature: flat before the first and after the last point.
function evaluate(points, temp) {
  if (!points || points.length === 0) return 0
  if (temp <= points[0].t) return points[0].f
  var last = points[points.length - 1]
  if (temp >= last.t) return last.f
  var m = tangents(points)
  for (var i = 0; i < points.length - 1; i++) {
    var a = points[i], b = points[i + 1]
    if (temp > b.t) continue
    var h = b.t - a.t, s = (temp - a.t) / h
    var s2 = s * s, s3 = s2 * s
    var v = (2 * s3 - 3 * s2 + 1) * a.f + (s3 - 2 * s2 + s) * h * m[i]
      + (-2 * s3 + 3 * s2) * b.f + (s3 - s2) * h * m[i + 1]
    return clamp(v, fanMin, fanMax)
  }
  return last.f
}

function formatRpm(rpm) {
  var n = Math.max(0, Math.round(Number(rpm) || 0))
  return n.toString().replace(/\B(?=(\d{3})+(?!\d))/g, ",")
}
