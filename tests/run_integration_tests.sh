#!/bin/bash -eu

set -o pipefail

TOP="$(readlink -f "$(dirname "$0")"/../../..)"
"$TOP/build/soong/tests/androidmk_test.sh"
"$TOP/build/soong/tests/bootstrap_test.sh"
"$TOP/build/soong/tests/soong_test.sh"
"$TOP/build/soong/tests/java_partial_compile_test.sh"
