#!/bin/bash -eu

set -o pipefail

export BUILD_JAVA_LIBRARY="true"

# This test checks partial_compile features
source "$(dirname "$0")/lib.sh"
source "$(dirname "$0")/java_partial_compile_setup.sh"
source "$(dirname "$0")/compare_jars.sh"

function test_add_new_file {
  setup

  partial_compile_setup out/soong/.intermediates/soong-test/java/integration/impl-library/android_common/javac/impl-library.jar
  run_soong_for_java_lib
  run_ninja impl-library

  # add a new file
  create_example_impl3_file
  run_ninja impl-library
  # copy out jar
  mkdir -p out/test_add_new_file/partial_compile && cp out/soong/.intermediates/soong-test/java/integration/impl-library/android_common/javac/impl-library.jar out/test_add_new_file/partial_compile/impl-library.jar

## Now run with full_compile setup
  full_compile_setup out/soong/.intermediates/soong-test/java/integration/impl-library/android_common/javac/impl-library.jar
  run_soong_for_java_lib
  run_ninja impl-library

  # add a new file
  create_example_impl3_file
  run_ninja impl-library
  # copy out jar
  mkdir -p out/test_add_new_file/full_compile && cp out/soong/.intermediates/soong-test/java/integration/impl-library/android_common/javac/impl-library.jar out/test_add_new_file/full_compile/impl-library.jar

  # compare the two jar's for equality
  assert_jars_equal out/test_add_new_file/full_compile/impl-library.jar out/test_add_new_file/partial_compile/impl-library.jar || \
    fail "Failed: full-compile and partial-compile outputs do not match"

  echo "test_add_new_file test passed"

  remove_base_dir
}

function test_move_file {
  setup

  partial_compile_setup out/soong/.intermediates/soong-test/java/integration/impl-library/android_common/javac/impl-library.jar
  run_soong_for_java_lib

  # create a new directory
  mkdir -p soong-test/java/integration/impllib/newimpl
  # rename soong-test/java/integration/impllib/ExampleImpl1.java to soong-test/java/integration/impllib/newimpl/ExampleImpl1.java
  mv soong-test/java/integration/impllib/ExampleImpl1.java soong-test/java/integration/impllib/newimpl/ExampleImpl1.java
  run_ninja impl-library
  # copy out jar
  mkdir -p out/test_move_file/partial_compile && cp out/soong/.intermediates/soong-test/java/integration/impl-library/android_common/javac/impl-library.jar out/test_move_file/partial_compile/impl-library.jar

## Now run with full_compile setup
  full_compile_setup out/soong/.intermediates/soong-test/java/integration/impl-library/android_common/javac/impl-library.jar
  run_soong_for_java_lib

  # create a new directory
  mkdir -p soong-test/java/integration/impllib/newimpl
  # rename soong-test/java/integration/impllib/ExampleImpl1.java to soong-test/java/integration/impllib/newimpl/ExampleImpl1.java
  mv soong-test/java/integration/impllib/ExampleImpl1.java soong-test/java/integration/impllib/newimpl/ExampleImpl1.java
  run_ninja impl-library
  # copy out jar
  mkdir -p out/test_move_file/full_compile && cp out/soong/.intermediates/soong-test/java/integration/impl-library/android_common/javac/impl-library.jar out/test_move_file/full_compile/impl-library.jar

  # compare the two jar's for equality
  assert_jars_equal out/test_move_file/full_compile/impl-library.jar out/test_move_file/partial_compile/impl-library.jar || \
    fail "Failed: full-compile and partial-compile outputs do not match"

  echo "test_move_file test passed"

  remove_base_dir
}

function test_remove_file {
  setup

  partial_compile_setup out/soong/.intermediates/soong-test/java/integration/impl-library/android_common/javac/impl-library.jar true
  run_soong_for_java_lib
  run_ninja impl-library

  # remove soong-test/java/integration/impllib/ExampleImpl3.java
  rm -rf soong-test/java/integration/impllib/ExampleImpl3.java
  run_ninja impl-library
  # copy out jar
  mkdir -p out/test_remove_file/partial_compile && cp out/soong/.intermediates/soong-test/java/integration/impl-library/android_common/javac/impl-library.jar out/test_remove_file/partial_compile/impl-library.jar

## Now run with full_compile setup
  full_compile_setup out/soong/.intermediates/soong-test/java/integration/impl-library/android_common/javac/impl-library.jar true
  run_soong_for_java_lib
  run_ninja impl-library

  # remove soong-test/java/integration/impllib/ExampleImpl3.java
  rm -rf soong-test/java/integration/impllib/ExampleImpl3.java
  run_ninja impl-library
  # copy out jar
  mkdir -p out/test_remove_file/full_compile && cp out/soong/.intermediates/soong-test/java/integration/impl-library/android_common/javac/impl-library.jar out/test_remove_file/full_compile/impl-library.jar

  # compare the two jar's for equality
  assert_jars_equal out/test_remove_file/full_compile/impl-library.jar out/test_remove_file/partial_compile/impl-library.jar || \
    fail "Failed: full-compile and partial-compile outputs do not match"

  echo "test_remove_file test passed"

  remove_base_dir
}

function test_bp_modification {
  setup

  partial_compile_setup out/soong/.intermediates/soong-test/java/integration/impl-library/android_common/javac/impl-library.jar true
  run_soong_for_java_lib
  run_ninja impl-library

  #modify the bp file
  cat > soong-test/java/integration/Android.bp <<'EOF'
java_library {
    name: "impl-library",
    srcs: [
        "impllib/**/*.java",
    ],
    libs: ["provider-library-new"],
    sdk_version: "35",
}

java_library {
    name: "provider-library-new",
    srcs: ["provider/**/*.java"],
    sdk_version: "35",
}
EOF

  run_ninja impl-library
  # copy out jar
  mkdir -p out/test_bp_modification/partial_compile && cp out/soong/.intermediates/soong-test/java/integration/impl-library/android_common/javac/impl-library.jar out/test_bp_modification/partial_compile/impl-library.jar

## Now run with full_compile setup
  full_compile_setup out/soong/.intermediates/soong-test/java/integration/impl-library/android_common/javac/impl-library.jar true
  run_soong_for_java_lib
  run_ninja impl-library

  #modify the bp file
  cat > soong-test/java/integration/Android.bp <<'EOF'
java_library {
    name: "impl-library",
    srcs: [
        "impllib/**/*.java",
    ],
    libs: ["provider-library-new"],
    sdk_version: "35",
}

java_library {
    name: "provider-library-new",
    srcs: ["provider/**/*.java"],
    sdk_version: "35",
}
EOF

  run_ninja impl-library
  # copy out jar
  mkdir -p out/test_bp_modification/full_compile && cp out/soong/.intermediates/soong-test/java/integration/impl-library/android_common/javac/impl-library.jar out/test_bp_modification/full_compile/impl-library.jar

  # compare the two jar's for equality
  assert_jars_equal out/test_bp_modification/full_compile/impl-library.jar out/test_bp_modification/partial_compile/impl-library.jar || \
    fail "Failed: full-compile and partial-compile outputs do not match"

  echo "test_bp_modification test passed"

  remove_base_dir
}

function test_incorrect_file_modification {
  setup
  readonly ERROR_LOG=${MOCK_TOP}/out/error.log
  readonly ERROR_RULE="FAILED: //soong-test/java/integration:impl-library javac-inc"
  readonly ERROR_MESSAGE="soong-test/java/integration/impllib/ExampleImpl3.java:15"

  partial_compile_setup out/soong/.intermediates/soong-test/java/integration/impl-library/android_common/javac/impl-library.jar true
  run_soong_for_java_lib
  run_ninja impl-library

  #modify a java file incorrectly, inc-javac should fail
  modify_example_impl1_file

  run_ninja impl-library && \
    fail "impl-library built with incorrect java compilation"

  if grep -q "${ERROR_RULE}" "${ERROR_LOG}" && grep -q "${ERROR_MESSAGE}" "${ERROR_LOG}" ; then
    echo Error rule and error message found in logs >/dev/null
  else
    fail "Did not find javac failure error AND file error message in error.log"
  fi

  echo "test_incorrect_file_modification test passed"
  remove_base_dir
}

function test_incorrect_cross_module_file_modification {
  setup
  local readonly ERROR_LOG=${MOCK_TOP}/out/error.log
  local readonly ERROR_RULE="FAILED: //soong-test/java/integration:impl-library javac-inc"
  local readonly ERROR_MESSAGE="soong-test/java/integration/impllib/ExampleImpl1.java:19"

  partial_compile_setup out/soong/.intermediates/soong-test/java/integration/impl-library/android_common/javac/impl-library.jar true
  run_soong_for_java_lib
  run_ninja impl-library

  #modify a cross-module java file incorrectly, inc-javac should fail
  modify_provider_file

  run_ninja impl-library && \
    fail "impl-library built with incorrect java compilation"

  if grep -q "${ERROR_RULE}" "${ERROR_LOG}" && grep -q "${ERROR_MESSAGE}" "${ERROR_LOG}" ; then
    echo Error rule and error message found in logs >/dev/null
  else
    fail "Did not find javac failure error AND file error message in error.log"
  fi

  echo "test_incorrect_cross_module_file_modification test passed"
  remove_base_dir
}

function test_incorrect_api_generating_ap_modification {
  setup
  local readonly ERROR_LOG=${MOCK_TOP}/out/error.log
  local readonly ERROR_RULE="FAILED: //soong-test/java/integration:impl-library javac-inc"
  local readonly ERROR_MESSAGE="soong-test/java/integration/impllib/ExampleImpl4.java:9"

  partial_compile_setup_with_ap out/soong/.intermediates/soong-test/java/integration/impl-library/android_common/javac/impl-library.jar
  run_soong_for_java_lib
  run_ninja impl-library

  #modify the API surface generated by AP, which is being used in impl-library
  modify_annotation_api

  run_ninja impl-library && \
    fail "impl-library built with incorrect java compilation"

  if grep -q "${ERROR_RULE}" "${ERROR_LOG}" && grep -q "${ERROR_MESSAGE}" "${ERROR_LOG}" ; then
    echo Error rule and error message found in logs >/dev/null
  else
    fail "Did not find javac failure error AND file error message in error.log"
  fi

  echo "test_incorrect_api_generating_ap_modification test passed"
  remove_base_dir
}

scan_and_run_tests