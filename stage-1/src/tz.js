"use strict";
/**
 * IANA timezone wall-clock <-> instant conversion using only Node's built-in
 * Intl support (full ICU + tzdata ship in the Node runtime itself, no package
 * or system tzdata required). No third-party date library is used.
 */

const PROBE_MS = 6 * 3600 * 1000; // wider than any real DST shift, narrower than a day

const partsFormatterCache = new Map();

function formatterFor(tz) {
  let fmt = partsFormatterCache.get(tz);
  if (!fmt) {
    fmt = new Intl.DateTimeFormat("en-US", {
      timeZone: tz,
      year: "numeric",
      month: "2-digit",
      day: "2-digit",
      hour: "2-digit",
      minute: "2-digit",
      second: "2-digit",
      hour12: false,
    });
    partsFormatterCache.set(tz, fmt);
  }
  return fmt;
}

/** Wall-clock fields of `instantMs` as observed in `tz`. */
function localFieldsAt(instantMs, tz) {
  const parts = {};
  for (const p of formatterFor(tz).formatToParts(new Date(instantMs))) {
    parts[p.type] = p.value;
  }
  let hour = parseInt(parts.hour, 10);
  if (hour === 24) hour = 0;
  return {
    y: parseInt(parts.year, 10),
    mo: parseInt(parts.month, 10),
    d: parseInt(parts.day, 10),
    h: hour,
    mi: parseInt(parts.minute, 10),
    s: parseInt(parts.second, 10),
  };
}

function fieldsAsUtcMs(f) {
  return Date.UTC(f.y, f.mo - 1, f.d, f.h, f.mi, f.s || 0);
}

/** Offset in minutes such that local = instant + offset. */
function offsetMinutesAt(instantMs, tz) {
  const local = fieldsAsUtcMs(localFieldsAt(instantMs, tz));
  return Math.round((local - instantMs) / 60000);
}

/**
 * Resolve a local wall-clock time (y, mo, d, h, mi) in `tz` to a UTC instant.
 *
 * Returns { ok: true, ambiguous, utcMs } or { ok: false } when the local time
 * falls in a spring-forward gap and never occurred. On a fall-back repeated
 * hour, `ambiguous` is true and `utcMs` is the earlier (first) occurrence.
 */
function wallToUTC(y, mo, d, h, mi, tz) {
  const targetLocalMs = Date.UTC(y, mo - 1, d, h, mi, 0);
  const offA = offsetMinutesAt(targetLocalMs - PROBE_MS, tz);
  const offB = offsetMinutesAt(targetLocalMs + PROBE_MS, tz);
  const candA = targetLocalMs - offA * 60000;
  const candB = targetLocalMs - offB * 60000;

  const matches = (utcMs) => {
    const f = localFieldsAt(utcMs, tz);
    return f.y === y && f.mo === mo && f.d === d && f.h === h && f.mi === mi;
  };
  const mA = matches(candA);
  const mB = matches(candB);
  if (mA && mB) {
    if (candA === candB) return { ok: true, ambiguous: false, utcMs: candA };
    return { ok: true, ambiguous: true, utcMs: Math.min(candA, candB) };
  }
  if (mA) return { ok: true, ambiguous: false, utcMs: candA };
  if (mB) return { ok: true, ambiguous: false, utcMs: candB };
  return { ok: false };
}

function offsetString(instantMs, tz) {
  const mins = offsetMinutesAt(instantMs, tz);
  const sign = mins >= 0 ? "+" : "-";
  const abs = Math.abs(mins);
  const hh = String(Math.floor(abs / 60)).padStart(2, "0");
  const mm = String(abs % 60).padStart(2, "0");
  return `${sign}${hh}:${mm}`;
}

function pad2(n) {
  return String(n).padStart(2, "0");
}

/** RFC 3339 with explicit offset, in `tz`, e.g. 2026-09-24T19:00:00+02:00 */
function formatInZone(instantMs, tz) {
  const f = localFieldsAt(instantMs, tz);
  const off = offsetString(instantMs, tz);
  return `${f.y}-${pad2(f.mo)}-${pad2(f.d)}T${pad2(f.h)}:${pad2(f.mi)}:${pad2(f.s)}${off}`;
}

/** RFC 3339 in UTC with an explicit +00:00 offset (never `Z`). */
function formatUTC(instantMs) {
  const d = new Date(instantMs);
  return (
    `${d.getUTCFullYear()}-${pad2(d.getUTCMonth() + 1)}-${pad2(d.getUTCDate())}T` +
    `${pad2(d.getUTCHours())}:${pad2(d.getUTCMinutes())}:${pad2(d.getUTCSeconds())}+00:00`
  );
}

const LOCAL_RE = /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2})$/;

/** Parse a bare local `YYYY-MM-DDTHH:MM` string; null if malformed or not a real date/time. */
function parseLocal(str) {
  if (typeof str !== "string") return null;
  const m = LOCAL_RE.exec(str);
  if (!m) return null;
  const y = Number(m[1]);
  const mo = Number(m[2]);
  const d = Number(m[3]);
  const h = Number(m[4]);
  const mi = Number(m[5]);
  if (mo < 1 || mo > 12 || h > 23 || mi > 59) return null;
  const probe = new Date(Date.UTC(y, mo - 1, d));
  if (
    probe.getUTCFullYear() !== y ||
    probe.getUTCMonth() !== mo - 1 ||
    probe.getUTCDate() !== d
  ) {
    return null;
  }
  return { y, mo, d, h, mi };
}

const DATE_RE = /^(\d{4})-(\d{2})-(\d{2})$/;

/** Parse a bare calendar date `YYYY-MM-DD`; null if malformed or not a real date. */
function parseDate(str) {
  if (typeof str !== "string") return null;
  const m = DATE_RE.exec(str);
  if (!m) return null;
  const y = Number(m[1]);
  const mo = Number(m[2]);
  const d = Number(m[3]);
  if (mo < 1 || mo > 12) return null;
  const probe = new Date(Date.UTC(y, mo - 1, d));
  if (
    probe.getUTCFullYear() !== y ||
    probe.getUTCMonth() !== mo - 1 ||
    probe.getUTCDate() !== d
  ) {
    return null;
  }
  return { y, mo, d };
}

const WEEKDAYS = ["mon", "tue", "wed", "thu", "fri", "sat", "sun"];

/** mon..sun for a plain proleptic-Gregorian calendar date (no timezone involved). */
function weekdayOf(y, mo, d) {
  const jsDay = new Date(Date.UTC(y, mo - 1, d)).getUTCDay(); // 0=Sun..6=Sat
  return WEEKDAYS[(jsDay + 6) % 7];
}

function minutesOfHHMM(hhmm) {
  const m = /^(\d{2}):(\d{2})$/.exec(hhmm);
  if (!m) return null;
  const h = Number(m[1]);
  const mi = Number(m[2]);
  if (h > 23 || mi > 59) return null;
  return h * 60 + mi;
}

function hhmmOfMinutes(total) {
  const h = Math.floor(total / 60);
  const mi = total % 60;
  return `${pad2(h)}:${pad2(mi)}`;
}

module.exports = {
  wallToUTC,
  offsetString,
  formatInZone,
  formatUTC,
  parseLocal,
  parseDate,
  weekdayOf,
  minutesOfHHMM,
  hhmmOfMinutes,
  localFieldsAt,
};
