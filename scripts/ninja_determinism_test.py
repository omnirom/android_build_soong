#!/usr/bin/env python3

import asyncio
import argparse
import dataclasses
import hashlib
import os
import re
import socket
import subprocess
import sys
import zipfile

from typing import List

def get_top() -> str:
  path = '.'
  while not os.path.isfile(os.path.join(path, 'build/soong/soong_ui.bash')):
    if os.path.abspath(path) == '/':
      sys.exit('Could not find android source tree root.')
    path = os.path.join(path, '..')
  return os.path.abspath(path)


_PRODUCT_REGEX = re.compile(r'([a-zA-Z_][a-zA-Z0-9_]*)(?:(?:-([a-zA-Z_][a-zA-Z0-9_]*))?-(user|userdebug|eng))?')


@dataclasses.dataclass(frozen=True)
class Product:
  """Represents a TARGET_PRODUCT and TARGET_BUILD_VARIANT."""
  product: str
  release: str
  variant: str

  def __post_init__(self):
    if not _PRODUCT_REGEX.match(str(self)):
      raise ValueError(f'Invalid product name: {self}')

  def __str__(self):
    return self.product + '-' + self.release + '-' + self.variant


async def run_make_nothing(product: Product, out_dir: str) -> bool:
  """Runs a build and returns if it succeeded or not."""
  with open(os.path.join(out_dir, 'build.log'), 'wb') as f:
    result = await asyncio.create_subprocess_exec(
        'prebuilts/build-tools/linux-x86/bin/nsjail',
        '-q',
        '--cwd',
        os.getcwd(),
        '-e',
        '-B',
        '/',
        '-B',
        f'{os.path.abspath(out_dir)}:{os.path.join(os.getcwd(), "out")}',
        '--time_limit',
        '0',
        '--skip_setsid',
        '--keep_caps',
        '--disable_clone_newcgroup',
        '--disable_clone_newnet',
        '--rlimit_as',
        'soft',
        '--rlimit_core',
        'soft',
        '--rlimit_cpu',
        'soft',
        '--rlimit_fsize',
        'soft',
        '--rlimit_nofile',
        'soft',
        '--proc_rw',
        '--hostname',
        socket.gethostname(),
        '--',
        'build/soong/soong_ui.bash',
        '--make-mode',
        f'TARGET_PRODUCT={product.product}',
        f'TARGET_RELEASE={product.release}',
        f'TARGET_BUILD_VARIANT={product.variant}',
        '--skip-ninja',
        'nothing', stdout=f, stderr=subprocess.STDOUT)
    return await result.wait() == 0

SUBNINJA_OR_INCLUDE_REGEX = re.compile(rb'\n(?:include|subninja) ')

def find_subninjas_and_includes(contents) -> List[str]:
  results = []
  def get_path_from_directive(i):
    j = contents.find(b'\n', i)
    if j < 0:
      path_bytes = contents[i:]
    else:
      path_bytes = contents[i:j]
    path_bytes = path_bytes.removesuffix(b'\r')
    path = path_bytes.decode()
    if '$' in path:
      sys.exit('includes/subninjas with variables are unsupported: '+path)
    return path

  if contents.startswith(b"include "):
    results.append(get_path_from_directive(len(b"include ")))
  elif contents.startswith(b"subninja "):
    results.append(get_path_from_directive(len(b"subninja ")))

  for match in SUBNINJA_OR_INCLUDE_REGEX.finditer(contents):
    results.append(get_path_from_directive(match.end()))

  return results


def transitively_included_ninja_files(out_dir: str, ninja_file: str, seen):
  with open(ninja_file, 'rb') as f:
    contents = f.read()

  results = [ninja_file]
  seen[ninja_file] = True
  sub_files = find_subninjas_and_includes(contents)
  for sub_file in sub_files:
    sub_file = os.path.join(out_dir, sub_file.removeprefix('out/'))
    if sub_file not in seen:
      results.extend(transitively_included_ninja_files(out_dir, sub_file, seen))

  return results


def hash_ninja_file(out_dir: str, ninja_file: str, hasher):
  with open(ninja_file, 'rb') as f:
    contents = f.read()

  sub_files = find_subninjas_and_includes(contents)

  hasher.update(contents)

  for sub_file in sub_files:
    hash_ninja_file(out_dir, os.path.join(out_dir, sub_file.removeprefix('out/')), hasher)


def hash_files(files: List[str]) -> str:
  hasher = hashlib.md5()
  for file in files:
    with open(file, 'rb') as f:
      hasher.update(f.read())
  return hasher.hexdigest()


def dist_ninja_files(out_dir: str, zip_name: str, ninja_files: List[str]):
  dist_dir = os.getenv('DIST_DIR', os.path.join(os.getenv('OUT_DIR', 'out'), 'dist'))
  os.makedirs(dist_dir, exist_ok=True)

  with open(os.path.join(dist_dir, zip_name), 'wb') as f:
    with zipfile.ZipFile(f, mode='w') as zf:
      for ninja_file in ninja_files:
        zf.write(ninja_file, arcname=os.path.basename(out_dir)+'/out/' + os.path.relpath(ninja_file, out_dir))


async def main():
    parser = argparse.ArgumentParser()
    args = parser.parse_args()

    os.chdir(get_top())
    subprocess.check_call(['touch', 'build/soong/Android.bp'])

    product = Product(
      'aosp_cf_x86_64_phone',
      'trunk_staging',
      'userdebug',
    )
    os.environ['TARGET_PRODUCT'] = 'aosp_cf_x86_64_phone'
    os.environ['TARGET_RELEASE'] = 'trunk_staging'
    os.environ['TARGET_BUILD_VARIANT'] = 'userdebug'

    out_dir1 = os.path.join(os.getenv('OUT_DIR', 'out'), 'determinism_test_out1')
    out_dir2 = os.path.join(os.getenv('OUT_DIR', 'out'), 'determinism_test_out2')

    os.makedirs(out_dir1, exist_ok=True)
    os.makedirs(out_dir2, exist_ok=True)

    success1, success2 = await asyncio.gather(
      run_make_nothing(product, out_dir1),
      run_make_nothing(product, out_dir2))

    if not success1:
      with open(os.path.join(out_dir1, 'build.log'), 'r') as f:
        print(f.read(), file=sys.stderr)
      sys.exit('build failed')
    if not success2:
      with open(os.path.join(out_dir2, 'build.log'), 'r') as f:
        print(f.read(), file=sys.stderr)
      sys.exit('build failed')

    ninja_files1 = transitively_included_ninja_files(out_dir1, os.path.join(out_dir1, f'combined-{product.product}.ninja'), {})
    ninja_files2 = transitively_included_ninja_files(out_dir2, os.path.join(out_dir2, f'combined-{product.product}.ninja'), {})

    dist_ninja_files(out_dir1, 'determinism_test_files_1.zip', ninja_files1)
    dist_ninja_files(out_dir2, 'determinism_test_files_2.zip', ninja_files2)

    hash1 = hash_files(ninja_files1)
    hash2 = hash_files(ninja_files2)

    if hash1 != hash2:
        sys.exit("ninja files were not deterministic! See disted determinism_test_files_1/2.zip")

    print("Success, ninja files were deterministic")


if __name__ == "__main__":
    asyncio.run(main())


