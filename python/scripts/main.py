
import os
import runpy
import shutil
import sys
import tempfile
import zipfile

from pathlib import PurePath


sys.argv[0] = __loader__.archive

# Set sys.executable to None. The real executable is available as
# sys.argv[0], and too many things assume sys.executable is a regular Python
# binary, which isn't available. By setting it to None we get clear errors
# when people try to use it.
sys.executable = None

# Extract the shared libraries from the zip file into a temporary directory.
# This works around the limitations of dynamic linker.  Some Python libraries
# reference the .so files relatively and so extracting only the .so files
# does not work, so we extract the entire parent directory of the .so files to a
# tempdir and then add that to sys.path.
tempdir = None
with zipfile.ZipFile(__loader__.archive) as z:
  # any root so files or root directories that contain so files will be
  # extracted to the tempdir so the linker load them, this minimizes the
  # number of files that need to be extracted to a tempdir
  extract_paths = {}
  for member in z.infolist():
    if member.filename.endswith('.so'):
      extract_paths[PurePath(member.filename).parts[0]] = member.filename
  if extract_paths:
    tempdir = tempfile.mkdtemp()
    for member in z.infolist():
      if not PurePath(member.filename).parts[0] in extract_paths.keys():
        continue
      if member.is_dir():
        os.makedirs(os.path.join(tempdir, member.filename))
      else:
        z.extract(member, tempdir)
    sys.path.insert(0, tempdir)
try:
  runpy._run_module_as_main("ENTRY_POINT", alter_argv=False)
finally:
  if tempdir is not None:
    shutil.rmtree(tempdir)
