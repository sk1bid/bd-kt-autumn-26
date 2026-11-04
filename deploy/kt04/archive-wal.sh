#!/bin/sh
set -eu

source_file=$1
destination="/wal-archive/$2"

if [ -e "$destination" ]; then
    cmp -s "$source_file" "$destination"
    exit
fi

temporary=$(mktemp /wal-archive/.kt04-wal.XXXXXX)
trap 'rm -f "$temporary"' EXIT
trap 'exit 1' HUP INT TERM
cp "$source_file" "$temporary"
sync -f "$temporary"
mv "$temporary" "$destination"
sync -f /wal-archive
