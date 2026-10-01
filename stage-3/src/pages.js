"use strict";

function escapeHtml(s) {
  return String(s)
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;")
    .replace(/'/g, "&#39;");
}

// Elements whose presence/absence is semantically meaningful (tests check
// `query_selector(...) is None` or `state="detached"`) are never pre-rendered
// as hidden placeholders here -- app.js inserts and removes them from a named
// `data-slot` anchor, so "absent" always means truly absent from the DOM.
function layout(page, title, body) {
  return `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8" />
<meta name="viewport" content="width=device-width, initial-scale=1" />
<title>${escapeHtml(title)} — Zum Anker Table</title>
<link rel="stylesheet" href="/app.css" />
</head>
<body data-page="${page}">
<a class="skip-link" href="#main">Skip to content</a>
<header class="site-header">
  <a class="brand" href="/">Zum&nbsp;Anker Table</a>
  <nav class="site-nav" aria-label="Primary">
    <a href="/">Search</a>
    <a href="/lookup">Find a reservation</a>
  </nav>
  <div class="auth-area" data-slot="auth-area"></div>
</header>
<main id="main">
${body}
</main>
<script src="/app.js" defer></script>
</body>
</html>
`;
}

function searchPage() {
  return layout("search", "Search availability", `
<section class="panel search-panel" aria-labelledby="search-heading">
  <h1 id="search-heading">Find a table</h1>
  <form data-testid="search-form" class="search-form">
    <div class="field">
      <label for="restaurant-select">Restaurant</label>
      <select id="restaurant-select" data-testid="restaurant-select" required></select>
    </div>
    <div class="field">
      <label for="date-input">Date</label>
      <input id="date-input" type="date" data-testid="date-input" required />
    </div>
    <div class="field">
      <label for="party-size-input">Party size</label>
      <input id="party-size-input" type="number" min="1" step="1" data-testid="party-size-input" value="2" required />
    </div>
    <button type="submit" data-testid="search-button" class="btn btn-primary">Search availability</button>
  </form>
  <div data-slot="search-auth-error"></div>
</section>

<section class="panel results-panel" aria-live="polite">
  <div data-slot="results"></div>
</section>

<div data-slot="booking"></div>

<div data-slot="confirmation"></div>
`);
}

function signupPage() {
  return layout("signup", "Sign up", `
<section class="panel auth-panel" aria-labelledby="signup-heading">
  <h1 id="signup-heading">Create your account</h1>
  <form data-testid="signup-form" class="auth-form">
    <div class="field">
      <label for="signup-email">Email</label>
      <input id="signup-email" type="email" data-testid="signup-email" required />
    </div>
    <div class="field">
      <label for="signup-password">Password</label>
      <input id="signup-password" type="password" data-testid="signup-password" minlength="8" required />
    </div>
    <div class="field">
      <label for="signup-display-name">Display name</label>
      <input id="signup-display-name" type="text" data-testid="signup-display-name" required />
    </div>
    <button type="submit" data-testid="signup-submit" class="btn btn-primary">Sign up</button>
    <div data-slot="auth-error"></div>
  </form>
</section>
`);
}

function loginPage() {
  return layout("login", "Sign in", `
<section class="panel auth-panel" aria-labelledby="login-heading">
  <h1 id="login-heading">Sign in</h1>
  <form data-testid="login-form" class="auth-form">
    <div class="field">
      <label for="login-email">Email</label>
      <input id="login-email" type="email" data-testid="login-email" required />
    </div>
    <div class="field">
      <label for="login-password">Password</label>
      <input id="login-password" type="password" data-testid="login-password" required />
    </div>
    <button type="submit" data-testid="login-submit" class="btn btn-primary">Sign in</button>
    <div data-slot="auth-error"></div>
  </form>
</section>
`);
}

function lookupPage() {
  return layout("lookup", "Find a reservation", `
<section class="panel lookup-panel" aria-labelledby="lookup-heading">
  <h1 id="lookup-heading">Find your reservation</h1>
  <form data-testid="lookup-form" class="lookup-form">
    <div class="field">
      <label for="lookup-reference-input">Confirmation reference</label>
      <input id="lookup-reference-input" type="text" data-testid="lookup-reference-input" required />
    </div>
    <button type="submit" data-testid="lookup-submit" class="btn btn-primary">Look up</button>
  </form>
  <div data-slot="reservation-error"></div>
  <div data-slot="reservation-detail"></div>
</section>
`);
}

module.exports = { searchPage, signupPage, loginPage, lookupPage };
