#!/usr/bin/env node

'use strict';

const fs = require('node:fs');
const path = require('node:path');
const packageJSON = require('../../package.json');

const root = path.join(__dirname, '..', '..');
const buildInfo = fs.readFileSync(path.join(root, 'internal', 'buildinfo', 'version.go'), 'utf8');
const match = buildInfo.match(/CLIVersion\s*=\s*"([^"]+)"/);
if (!match) {
  throw new Error('CLIVersion was not found in internal/buildinfo/version.go');
}
if (match[1] !== packageJSON.version) {
  throw new Error(`CLI version ${match[1]} does not match npm version ${packageJSON.version}`);
}

const tag = process.argv[2] || process.env.GITHUB_REF_NAME;
if (tag && tag !== `v${packageJSON.version}`) {
  throw new Error(`Git tag ${tag} does not match npm version v${packageJSON.version}`);
}

console.log(`Release versions match: v${packageJSON.version}`);
