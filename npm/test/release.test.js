'use strict';

const assert = require('node:assert/strict');
const crypto = require('node:crypto');
const fs = require('node:fs/promises');
const os = require('node:os');
const path = require('node:path');
const test = require('node:test');
const { assetName, releaseTarget, releaseURL } = require('../lib/release');
const { expectedChecksum, install, verifyChecksum } = require('../lib/install');

test('maps Node platforms and architectures to GitHub release assets', () => {
  assert.deepEqual(releaseTarget('linux', 'x64'), { os: 'linux', arch: 'amd64' });
  assert.deepEqual(releaseTarget('darwin', 'arm64'), { os: 'darwin', arch: 'arm64' });
  assert.equal(assetName('0.3.4', 'win32', 'x64'), 'procyon-cli_0.3.4_windows_amd64.exe');
  assert.equal(
    releaseURL('0.3.4', 'SHA256SUMS'),
    'https://github.com/bartek5186/procyon-cli/releases/download/v0.3.4/SHA256SUMS'
  );
});

test('rejects unsupported targets with an actionable message', () => {
  assert.throws(() => releaseTarget('freebsd', 'x64'), /Unsupported platform: freebsd\/x64/);
  assert.throws(() => releaseTarget('linux', 'ia32'), /Unsupported platform: linux\/ia32/);
});

test('extracts and verifies the matching checksum', () => {
  const contents = Buffer.from('procyon');
  const checksum = crypto.createHash('sha256').update(contents).digest('hex');
  const manifest = Buffer.from(`${checksum}  procyon-cli_0.3.4_linux_amd64\n`);
  assert.equal(expectedChecksum(manifest, 'procyon-cli_0.3.4_linux_amd64'), checksum);
  assert.doesNotThrow(() => verifyChecksum(contents, checksum, 'binary'));
  assert.throws(() => verifyChecksum(contents, '0'.repeat(64), 'binary'), /SHA-256 mismatch/);
});

test('installs a verified binary at the requested destination', async (t) => {
  const directory = await fs.mkdtemp(path.join(os.tmpdir(), 'procyon-cli-npm-'));
  t.after(() => fs.rm(directory, { recursive: true, force: true }));
  const destination = path.join(directory, 'procyon-cli');
  const contents = Buffer.from('#!/bin/sh\necho procyon\n');
  const checksum = crypto.createHash('sha256').update(contents).digest('hex');
  const filename = 'procyon-cli_0.3.4_linux_amd64';
  const download = async (url) => url.endsWith('/SHA256SUMS')
    ? Buffer.from(`${checksum}  ${filename}\n`)
    : contents;

  const result = await install('0.3.4', {
    platform: 'linux',
    architecture: 'x64',
    destination,
    download
  });
  assert.equal(result.filename, filename);
  assert.deepEqual(await fs.readFile(destination), contents);
  assert.equal((await fs.stat(destination)).mode & 0o111, 0o111);
});
