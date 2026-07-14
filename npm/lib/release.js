'use strict';

const path = require('node:path');

const repository = 'bartek5186/procyon-cli';

const platforms = {
  linux: 'linux',
  darwin: 'darwin',
  win32: 'windows'
};

const architectures = {
  x64: 'amd64',
  arm64: 'arm64'
};

function releaseTarget(platform = process.platform, architecture = process.arch) {
  const os = platforms[platform];
  const arch = architectures[architecture];
  if (!os || !arch) {
    throw new Error(
      `Unsupported platform: ${platform}/${architecture}. ` +
      'Supported systems are Linux, macOS and Windows on x64 or arm64.'
    );
  }
  return { os, arch };
}

function assetName(version, platform = process.platform, architecture = process.arch) {
  const { os, arch } = releaseTarget(platform, architecture);
  const extension = os === 'windows' ? '.exe' : '';
  return `procyon-cli_${version}_${os}_${arch}${extension}`;
}

function releaseURL(version, filename) {
  return `https://github.com/${repository}/releases/download/v${version}/${filename}`;
}

function binaryPath(platform = process.platform) {
  const extension = platform === 'win32' ? '.exe' : '';
  return path.join(__dirname, '..', 'vendor', `procyon-cli${extension}`);
}

module.exports = {
  assetName,
  binaryPath,
  releaseTarget,
  releaseURL
};
