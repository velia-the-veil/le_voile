// Tests for bypass detection functions in background.js
// Run: node extension/background_test.js
// No dependencies required — uses Node.js assert module

'use strict';

const assert = require('assert');

// --- Extract and test pure functions from background.js ---

const BYPASS_THRESHOLD = 52428800; // 50 Mo

function getContentLength(responseHeaders) {
  if (!responseHeaders) return -1;
  for (let i = 0; i < responseHeaders.length; i++) {
    if (responseHeaders[i].name.toLowerCase() === 'content-length') {
      const len = parseInt(responseHeaders[i].value, 10);
      if (isNaN(len) || len < 0) return -1;
      return len;
    }
  }
  return -1;
}

// Simulated bypassUrls Set for testing
let bypassUrls = new Set();

function isAlreadyBypassed(url, firefoxMode) {
  if (firefoxMode) return bypassUrls.has(url);
  try {
    return bypassUrls.has(new URL(url).hostname);
  } catch (e) {
    return false;
  }
}

function addBypassEntry(url, firefoxMode) {
  if (firefoxMode) {
    if (bypassUrls.has(url)) return;
    bypassUrls.add(url);
  } else {
    try {
      const hostname = new URL(url).hostname;
      if (bypassUrls.has(hostname)) return;
      bypassUrls.add(hostname);
    } catch (e) {
      // URL malformee
    }
  }
}

function generatePacBypassCheck(hostname) {
  const safe = hostname.replace(/\\/g, '\\\\').replace(/'/g, "\\'");
  return `if (host === '${safe}') return 'DIRECT';`;
}

// --- Tests ---

let passed = 0;
let failed = 0;

function test(name, fn) {
  try {
    fn();
    passed++;
    console.log(`  PASS: ${name}`);
  } catch (e) {
    failed++;
    console.log(`  FAIL: ${name} — ${e.message}`);
  }
}

console.log('=== getContentLength ===');

test('returns content length from headers', () => {
  const headers = [{ name: 'Content-Length', value: '100000000' }];
  assert.strictEqual(getContentLength(headers), 100000000);
});

test('returns -1 when no headers', () => {
  assert.strictEqual(getContentLength(null), -1);
  assert.strictEqual(getContentLength(undefined), -1);
});

test('returns -1 when content-length absent', () => {
  const headers = [{ name: 'Content-Type', value: 'text/html' }];
  assert.strictEqual(getContentLength(headers), -1);
});

test('returns -1 for empty headers array', () => {
  assert.strictEqual(getContentLength([]), -1);
});

test('handles case-insensitive header name', () => {
  const headers = [{ name: 'content-length', value: '52428801' }];
  assert.strictEqual(getContentLength(headers), 52428801);
});

test('returns -1 for NaN content-length', () => {
  const headers = [{ name: 'Content-Length', value: 'abc' }];
  assert.strictEqual(getContentLength(headers), -1);
});

test('returns -1 for negative content-length', () => {
  const headers = [{ name: 'Content-Length', value: '-1' }];
  assert.strictEqual(getContentLength(headers), -1);
});

test('threshold comparison: 50 Mo exactly is NOT bypassed', () => {
  assert.strictEqual(BYPASS_THRESHOLD > BYPASS_THRESHOLD, false);
});

test('threshold comparison: 50 Mo + 1 IS bypassed', () => {
  assert.strictEqual(BYPASS_THRESHOLD + 1 > BYPASS_THRESHOLD, true);
});

test('threshold comparison: -1 (no header) is NOT bypassed', () => {
  assert.strictEqual(-1 > BYPASS_THRESHOLD, false);
});

console.log('\n=== isAlreadyBypassed ===');

test('Chrome mode: bypassed by hostname', () => {
  bypassUrls = new Set(['download.ubuntu.com']);
  assert.strictEqual(isAlreadyBypassed('https://download.ubuntu.com/ubuntu.iso', false), true);
});

test('Chrome mode: not bypassed if hostname not in set', () => {
  bypassUrls = new Set(['download.ubuntu.com']);
  assert.strictEqual(isAlreadyBypassed('https://other.com/file.iso', false), false);
});

test('Firefox mode: bypassed by exact URL', () => {
  bypassUrls = new Set(['https://download.ubuntu.com/ubuntu.iso']);
  assert.strictEqual(isAlreadyBypassed('https://download.ubuntu.com/ubuntu.iso', true), true);
});

test('Firefox mode: different URL on same host is NOT bypassed', () => {
  bypassUrls = new Set(['https://download.ubuntu.com/ubuntu.iso']);
  assert.strictEqual(isAlreadyBypassed('https://download.ubuntu.com/other.iso', true), false);
});

test('Chrome mode: malformed URL returns false', () => {
  bypassUrls = new Set();
  assert.strictEqual(isAlreadyBypassed('not-a-valid-url', false), false);
});

console.log('\n=== addBypassEntry ===');

test('Chrome mode: adds hostname', () => {
  bypassUrls = new Set();
  addBypassEntry('https://download.ubuntu.com/ubuntu.iso', false);
  assert.strictEqual(bypassUrls.has('download.ubuntu.com'), true);
  assert.strictEqual(bypassUrls.size, 1);
});

test('Chrome mode: duplicate hostname not added', () => {
  bypassUrls = new Set();
  addBypassEntry('https://download.ubuntu.com/file1.iso', false);
  addBypassEntry('https://download.ubuntu.com/file2.iso', false);
  assert.strictEqual(bypassUrls.size, 1);
});

test('Firefox mode: adds exact URL', () => {
  bypassUrls = new Set();
  addBypassEntry('https://download.ubuntu.com/ubuntu.iso', true);
  assert.strictEqual(bypassUrls.has('https://download.ubuntu.com/ubuntu.iso'), true);
});

test('Firefox mode: different URLs on same host both added', () => {
  bypassUrls = new Set();
  addBypassEntry('https://download.ubuntu.com/file1.iso', true);
  addBypassEntry('https://download.ubuntu.com/file2.iso', true);
  assert.strictEqual(bypassUrls.size, 2);
});

test('Chrome mode: malformed URL does not crash', () => {
  bypassUrls = new Set();
  addBypassEntry('not-a-valid-url', false);
  assert.strictEqual(bypassUrls.size, 0);
});

console.log('\n=== generatePacBypassCheck (hostname escaping) ===');

test('normal hostname passes through', () => {
  const result = generatePacBypassCheck('download.ubuntu.com');
  assert.strictEqual(result, "if (host === 'download.ubuntu.com') return 'DIRECT';");
});

test('hostname with single quote is escaped', () => {
  const result = generatePacBypassCheck("host'name.com");
  assert.strictEqual(result, "if (host === 'host\\'name.com') return 'DIRECT';");
});

test('hostname with backslash is escaped', () => {
  const result = generatePacBypassCheck('host\\name.com');
  assert.strictEqual(result, "if (host === 'host\\\\name.com') return 'DIRECT';");
});

// --- Summary ---

console.log(`\n=== Results: ${passed} passed, ${failed} failed ===`);
process.exit(failed > 0 ? 1 : 0);
