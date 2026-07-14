#!/usr/bin/env node

'use strict';

const { spawnSync } = require('node:child_process');
const { binaryPath } = require('../lib/release');

const executable = binaryPath();
const result = spawnSync(executable, process.argv.slice(2), {
  stdio: 'inherit',
  env: {
    ...process.env,
    PROCYON_CLI_INSTALL_METHOD: 'npm'
  }
});

if (result.error) {
  const hint = result.error.code === 'ENOENT'
    ? 'The platform binary is missing. Reinstall with `npm install --global procyon-cli`.'
    : result.error.message;
  console.error(`procyon-cli: ${hint}`);
  process.exit(1);
}

if (result.signal) {
  process.kill(process.pid, result.signal);
}
process.exit(result.status ?? 1);
