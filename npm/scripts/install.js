#!/usr/bin/env node

'use strict';

const packageJSON = require('../../package.json');
const { install } = require('../lib/install');

if (process.env.PROCYON_CLI_SKIP_DOWNLOAD === '1') {
  console.log('procyon-cli: platform binary download skipped');
  process.exit(0);
}

install(packageJSON.version)
  .then(({ filename }) => {
    console.log(`procyon-cli: installed ${filename}`);
  })
  .catch((error) => {
    console.error(`procyon-cli: installation failed: ${error.message}`);
    process.exit(1);
  });
