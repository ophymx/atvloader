#!/bin/sh
# Prints the APK version derived from git, as two lines:
#   versionName  e.g. 0.1.0, or 0.1.0-3-gabc1234 between tags, or 0.0.0-gabc1234
#   versionCode  major*1000000 + minor*1000 + patch of the nearest v* tag (0 if none)
# Android refuses downgrades, so versionCode must only ever grow: bump the tag.
set -e
desc=$(git describe --tags --match 'v*' --always --dirty 2>/dev/null || echo 0.0.0)
name=${desc#v}
case "$desc" in
  v*) ;;
  *) name="0.0.0-g$desc" ;;
esac
tag=$(git describe --tags --match 'v*' --abbrev=0 2>/dev/null || echo v0.0.0)
ver=${tag#v}; ver=${ver%%-*}
IFS=. read -r major minor patch <<EOT
$ver
EOT
code=$(( ${major:-0} * 1000000 + ${minor:-0} * 1000 + ${patch:-0} ))
echo "$name"
echo "$code"
