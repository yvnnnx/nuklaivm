#!/usr/bin/env bash
# Copyright (C) 2024, AllianceBlock. All rights reserved.
# See the file LICENSE for licensing terms.

set -e

# Set the CGO flags to use the portable version of BLST
#
# We use "export" here instead of just setting a bash variable because we need
# to pass this flag to all child processes spawned by the shell.
export CGO_CFLAGS="-O -D__BLST_PORTABLE__" CGO_ENABLED=1

# to run E2E tests (terminates cluster afterwards)
# MODE=test ./scripts/run.sh
if ! [[ "$0" =~ scripts/run.sh ]]; then
  echo "must be run from repository root"
  exit 255
fi

VERSION=v1.10.18
MAX_UINT64=18446744073709551615
MODE=${MODE:-run}
AGO_LOGLEVEL=${AGO_LOGLEVEL:-info}
LOGLEVEL=${LOGLEVEL:-info}
STATESYNC_DELAY=${STATESYNC_DELAY:-0}
MIN_BLOCK_GAP=${MIN_BLOCK_GAP:-100}
STORE_TXS=${STORE_TXS:-false}
UNLIMITED_USAGE=${UNLIMITED_USAGE:-false}
INITIAL_OWNER_ADDRESS=${INITIAL_OWNER_ADDRESS:-nuklai1qrzvk4zlwj9zsacqgtufx7zvapd3quufqpxk5rsdd4633m4wz2fdjss0gwx}
EMISSION_ADDRESS=${EMISSION_ADDRESS:-nuklai1qqmzlnnredketlj3cu20v56nt5ken6thchra7nylwcrmz77td654w2jmpt9}
if [[ ${MODE} != "run" ]]; then
  LOGLEVEL=debug
  STATESYNC_DELAY=100000000 # 100ms
  MIN_BLOCK_GAP=250 #ms
  STORE_TXS=true
  UNLIMITED_USAGE=true
fi

WINDOW_TARGET_UNITS="40000000,450000,450000,450000,450000"
MAX_BLOCK_UNITS="1800000,15000,15000,2500,15000"
if ${UNLIMITED_USAGE}; then
  WINDOW_TARGET_UNITS="${MAX_UINT64},${MAX_UINT64},${MAX_UINT64},${MAX_UINT64},${MAX_UINT64}"
  # If we don't limit the block size, AvalancheGo will reject the block.
  MAX_BLOCK_UNITS="1800000,${MAX_UINT64},${MAX_UINT64},${MAX_UINT64},${MAX_UINT64}"
fi

echo "Running with:"
echo AGO_LOGLEVEL: "${AGO_LOGLEVEL}"
echo LOGLEVEL: "${LOGLEVEL}"
echo VERSION: ${VERSION}
echo MODE: "${MODE}"
echo LOG LEVEL: "${LOGLEVEL}"
echo STATESYNC_DELAY \(ns\): "${STATESYNC_DELAY}"
echo MIN_BLOCK_GAP \(ms\): "${MIN_BLOCK_GAP}"
echo STORE_TXS: "${STORE_TXS}"
echo WINDOW_TARGET_UNITS: ${WINDOW_TARGET_UNITS}
echo MAX_BLOCK_UNITS: ${MAX_BLOCK_UNITS}
echo INITIAL_OWNER_ADDRESS: "${INITIAL_OWNER_ADDRESS}"
echo EMISSION_ADDRESS: "${EMISSION_ADDRESS}"

############################
# build avalanchego
# https://github.com/ava-labs/avalanchego/releases
# Set TMPDIR to the first command line argument if provided, otherwise default to /tmp/nuklaivm
# Default working directory
TMPDIR="/tmp/nuklaivm"

# Initialize an array for additional options
OPTIONS=()

# Parse arguments
while [[ $# -gt 0 ]]; do
  key="$1"
  case $key in
    --ginkgo.*)
      OPTIONS+=("$1")
      shift # past argument
      ;;
    *)
      TMPDIR="$1"
      shift # past argument
      ;;
  esac
done

echo "working directory: $TMPDIR"

AVALANCHEGO_PATH=${TMPDIR}/avalanchego-${VERSION}/avalanchego
AVALANCHEGO_PLUGIN_DIR=${TMPDIR}/avalanchego-${VERSION}/plugins

if [ ! -f "$AVALANCHEGO_PATH" ]; then
  echo "building avalanchego"
  CWD=$(pwd)

  # Clear old folders
  rm -rf "${TMPDIR}"/avalanchego-${VERSION}
  mkdir -p "${TMPDIR}"/avalanchego-${VERSION}
  rm -rf "${TMPDIR}"/avalanchego-src
  mkdir -p "${TMPDIR}"/avalanchego-src

  # Download src
  cd "${TMPDIR}"/avalanchego-src
  git clone https://github.com/ava-labs/avalanchego.git
  cd avalanchego
  git checkout ${VERSION}

  # Build avalanchego
  ./scripts/build.sh
  mv build/avalanchego "${TMPDIR}"/avalanchego-${VERSION}

  cd "${CWD}"
else
  echo "using previously built avalanchego"
fi

############################

############################
echo "building nuklaivm"

# delete previous (if exists)
rm -f "${TMPDIR}"/avalanchego-${VERSION}/plugins/qeX5BUxbiwUhSePncmz1C7RdH6njYYv6dNZhJrdeXRKMnTpKt

# rebuild with latest code
go build \
-o "${TMPDIR}"/avalanchego-${VERSION}/plugins/qeX5BUxbiwUhSePncmz1C7RdH6njYYv6dNZhJrdeXRKMnTpKt \
./cmd/nuklaivm

echo "building nuklai-cli"
go build -v -o "${TMPDIR}"/nuklai-cli ./cmd/nuklai-cli

# log everything in the avalanchego directory
find "${TMPDIR}"/avalanchego-${VERSION}

############################

############################

# Always create allocations (linter doesn't like tab)
#
# Make sure to replace this address with your own address
# if you are starting your own devnet (otherwise anyone can access
# funds using the included demo.pk)
# Initial balance: 853 million NAI
echo "creating allocations file"
cat <<EOF > "${TMPDIR}"/allocations.json
[
  {"address":"${INITIAL_OWNER_ADDRESS}", "balance":853000000000000000}
]
EOF
echo "creating emission balancer file"
# maxSupply: 10 billion NAI
cat <<EOF > "${TMPDIR}"/emission-balancer.json
{
  "maxSupply":  10000000000000000000,
  "emissionAddress":"${EMISSION_ADDRESS}"
}
EOF

GENESIS_PATH=$2
if [[ -z "${GENESIS_PATH}" ]]; then
  echo "creating VM genesis file with allocations"
  rm -f "${TMPDIR}"/nuklaivm.genesis
  "${TMPDIR}"/nuklai-cli genesis generate "${TMPDIR}"/allocations.json "${TMPDIR}"/emission-balancer.json \
  --window-target-units ${WINDOW_TARGET_UNITS} \
  --max-block-units ${MAX_BLOCK_UNITS} \
  --min-block-gap "${MIN_BLOCK_GAP}" \
  --genesis-file "${TMPDIR}"/nuklaivm.genesis
else
  echo "copying custom genesis file"
  rm -f "${TMPDIR}"/nuklaivm.genesis
  cp "${GENESIS_PATH}" "${TMPDIR}"/nuklaivm.genesis
fi

############################

############################

echo "creating vm config"
rm -f "${TMPDIR}"/nuklaivm.config
rm -rf "${TMPDIR}"/nuklaivm-e2e-profiles
cat <<EOF > "${TMPDIR}"/nuklaivm.config
{
  "mempoolSize": 10000000,
  "mempoolSponsorSize": 10000000,
  "mempoolExemptSponsors":["${INITIAL_OWNER_ADDRESS}", "${EMISSION_ADDRESS}"],
  "authVerificationCores": 2,
  "rootGenerationCores": 2,
  "transactionExecutionCores": 2,
  "verifyAuth":true,
  "storeTransactions": ${STORE_TXS},
  "streamingBacklogSize": 10000000,
  "logLevel": "${LOGLEVEL}",
  "continuousProfilerDir":"${TMPDIR}/nuklaivm-e2e-profiles/*",
  "stateSyncServerDelay": ${STATESYNC_DELAY}
}
EOF
mkdir -p "${TMPDIR}"/nuklaivm-e2e-profiles

############################

############################

echo "creating subnet config"
rm -f "${TMPDIR}"/nuklaivm.subnet
cat <<EOF > "${TMPDIR}"/nuklaivm.subnet
{
  "proposerMinBlockDelay": 0,
  "proposerNumHistoricalBlocks": 50000
}
EOF

############################

############################
echo "building e2e.test"
# to install the ginkgo binary (required for test build and run)
go install -v github.com/onsi/ginkgo/v2/ginkgo@v2.16.0

# alert the user if they do not have $GOPATH properly configured
if ! command -v ginkgo &> /dev/null
then
    echo -e "\033[0;31myour golang environment is misconfigued...please ensure the golang bin folder is in your PATH\033[0m"
    echo -e "\033[0;31myou can set this for the current terminal session by running \"export PATH=\$PATH:\$(go env GOPATH)/bin\"\033[0m"
    exit
fi

ACK_GINKGO_RC=true ginkgo build ./tests/e2e
./tests/e2e/e2e.test --help

#################################
# download avalanche-network-runner
# https://github.com/ava-labs/avalanche-network-runner
ANR_REPO_PATH=github.com/ava-labs/avalanche-network-runner
ANR_VERSION=90aa9ae77845665b7638404a2a5e6a4dcce6d489
# version set
go install -v ${ANR_REPO_PATH}@${ANR_VERSION}

#################################
# run "avalanche-network-runner" server
GOPATH=$(go env GOPATH)
if [[ -z ${GOBIN+x} ]]; then
  # no gobin set
  BIN=${GOPATH}/bin/avalanche-network-runner
else
  # gobin set
  BIN=${GOBIN}/avalanche-network-runner
fi

killall avalanche-network-runner || true

echo "launch avalanche-network-runner in the background"
$BIN server \
--log-level verbo \
--port=":12352" \
--grpc-gateway-port=":12353" &

############################
# By default, it runs all e2e test cases!
# Use "--ginkgo.skip" to skip tests.
# Use "--ginkgo.focus" to select tests.

KEEPALIVE=false
function cleanup() {
  if [[ ${KEEPALIVE} = true ]]; then
    echo "avalanche-network-runner is running in the background..."
    echo ""
    echo "use the following command to terminate:"
    echo ""
    echo "./scripts/stop.sh;"
    echo ""
    exit
  fi

  echo "avalanche-network-runner shutting down..."
  ./scripts/stop.sh;
}
trap cleanup EXIT

echo "running e2e tests"
./tests/e2e/e2e.test \
--ginkgo.v \
"${OPTIONS[@]}" \
--network-runner-log-level verbo \
--avalanchego-log-level "${AGO_LOGLEVEL}" \
--network-runner-grpc-endpoint="0.0.0.0:12352" \
--network-runner-grpc-gateway-endpoint="0.0.0.0:12353" \
--avalanchego-path="${AVALANCHEGO_PATH}" \
--avalanchego-plugin-dir="${AVALANCHEGO_PLUGIN_DIR}" \
--vm-genesis-path="${TMPDIR}"/nuklaivm.genesis \
--vm-config-path="${TMPDIR}"/nuklaivm.config \
--subnet-config-path="${TMPDIR}"/nuklaivm.subnet \
--output-path="${TMPDIR}"/avalanchego-${VERSION}/output.yaml \
--mode="${MODE}"

############################
if [[ ${MODE} == "run" ]]; then
  echo "cluster is ready!"
  # We made it past initialization and should avoid shutting down the network
  KEEPALIVE=true
fi
