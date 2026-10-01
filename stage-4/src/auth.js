"use strict";

const crypto = require("crypto");

const SCRYPT_KEYLEN = 64;

function hashPassword(password) {
  const salt = crypto.randomBytes(16).toString("hex");
  const hash = crypto.scryptSync(password, salt, SCRYPT_KEYLEN).toString("hex");
  return `${salt}:${hash}`;
}

function verifyPassword(password, stored) {
  if (typeof stored !== "string" || !stored.includes(":")) return false;
  const [salt, hash] = stored.split(":");
  const candidate = crypto.scryptSync(password, salt, SCRYPT_KEYLEN);
  const expected = Buffer.from(hash, "hex");
  if (candidate.length !== expected.length) return false;
  return crypto.timingSafeEqual(candidate, expected);
}

function newToken() {
  return crypto.randomBytes(24).toString("hex");
}

function newId(prefix) {
  return `${prefix}_${crypto.randomBytes(9).toString("hex")}`;
}

const REF_ALPHABET = "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789";

function newReference(existing) {
  for (let attempt = 0; attempt < 100; attempt++) {
    let ref = "";
    const bytes = crypto.randomBytes(8);
    for (let i = 0; i < 8; i++) ref += REF_ALPHABET[bytes[i] % REF_ALPHABET.length];
    if (!existing.has(ref)) return ref;
  }
  throw new Error("could not allocate a unique reference");
}

module.exports = { hashPassword, verifyPassword, newToken, newId, newReference };
