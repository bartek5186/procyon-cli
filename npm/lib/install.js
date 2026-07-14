'use strict';

const crypto = require('node:crypto');
const fs = require('node:fs/promises');
const https = require('node:https');
const path = require('node:path');
const { assetName, binaryPath, releaseURL } = require('./release');

const maxRedirects = 8;

function download(url, redirects = 0) {
  return new Promise((resolve, reject) => {
    const request = https.get(url, {
      headers: {
        Accept: 'application/octet-stream',
        'User-Agent': 'procyon-cli-npm-installer'
      }
    }, (response) => {
      const status = response.statusCode || 0;
      if (status >= 300 && status < 400 && response.headers.location) {
        response.resume();
        if (redirects >= maxRedirects) {
          reject(new Error(`Too many redirects while downloading ${url}`));
          return;
        }
        resolve(download(new URL(response.headers.location, url).toString(), redirects + 1));
        return;
      }
      if (status !== 200) {
        response.resume();
        reject(new Error(`Download failed with HTTP ${status}: ${url}`));
        return;
      }
      const chunks = [];
      response.on('data', (chunk) => chunks.push(chunk));
      response.on('end', () => resolve(Buffer.concat(chunks)));
      response.on('error', reject);
    });
    request.on('error', reject);
    request.setTimeout(60_000, () => {
      request.destroy(new Error(`Download timed out: ${url}`));
    });
  });
}

function expectedChecksum(checksumFile, filename) {
  for (const line of checksumFile.toString('utf8').split(/\r?\n/)) {
    const match = line.trim().match(/^([a-fA-F0-9]{64})\s+\*?(.+)$/);
    if (match && match[2] === filename) {
      return match[1].toLowerCase();
    }
  }
  throw new Error(`SHA-256 checksum for ${filename} is missing from SHA256SUMS`);
}

function verifyChecksum(contents, expected, filename) {
  const actual = crypto.createHash('sha256').update(contents).digest('hex');
  if (actual !== expected) {
    throw new Error(`SHA-256 mismatch for ${filename}: expected ${expected}, received ${actual}`);
  }
}

async function install(version, options = {}) {
  const fetch = options.download || download;
  const filename = assetName(version, options.platform, options.architecture);
  const checksums = await fetch(releaseURL(version, 'SHA256SUMS'));
  const expected = expectedChecksum(checksums, filename);
  const contents = await fetch(releaseURL(version, filename));
  verifyChecksum(contents, expected, filename);

  const destination = options.destination || binaryPath(options.platform);
  const temporary = `${destination}.${process.pid}.tmp`;
  await fs.mkdir(path.dirname(destination), { recursive: true });
  try {
    await fs.writeFile(temporary, contents, { mode: 0o755 });
    await fs.rename(temporary, destination);
  } catch (error) {
    await fs.rm(temporary, { force: true });
    throw error;
  }
  if ((options.platform || process.platform) !== 'win32') {
    await fs.chmod(destination, 0o755);
  }
  return { destination, filename };
}

module.exports = {
  download,
  expectedChecksum,
  install,
  verifyChecksum
};
