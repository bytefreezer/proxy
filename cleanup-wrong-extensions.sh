#!/bin/bash

# Script to fix incorrectly named metadata files
# Changes .raw.gz.json files to .meta files (supports any extension format)

SPOOL_DIR="${1:-/var/spool/bytefreezer-proxy}"

if [ ! -d "$SPOOL_DIR" ]; then
    echo "Error: Spool directory $SPOOL_DIR does not exist"
    exit 1
fi

echo "Fixing metadata file extensions in $SPOOL_DIR..."

# Find all .gz.json files and rename them to .meta (supports any extension)
find "$SPOOL_DIR" -name "*.gz.json" -type f | while read -r file; do
    # Extract the base name without the .gz.json extension
    base_name=$(basename "$file" .gz.json)
    dir_name=$(dirname "$file")
    new_name="$dir_name/$base_name.meta"

    echo "Renaming: $file -> $new_name"
    mv "$file" "$new_name"
done

echo "Done! All metadata files should now have .meta extension"