#!/bin/bash

# Script to fix incorrectly named metadata files
# Changes .ndjson.gz.json files to .meta files

SPOOL_DIR="${1:-/var/spool/bytefreezer-proxy}"

if [ ! -d "$SPOOL_DIR" ]; then
    echo "Error: Spool directory $SPOOL_DIR does not exist"
    exit 1
fi

echo "Fixing metadata file extensions in $SPOOL_DIR..."

# Find all .ndjson.gz.json files and rename them to .meta
find "$SPOOL_DIR" -name "*.ndjson.gz.json" -type f | while read -r file; do
    # Extract the base name without the .ndjson.gz.json extension
    base_name=$(basename "$file" .ndjson.gz.json)
    dir_name=$(dirname "$file")
    new_name="$dir_name/$base_name.meta"

    echo "Renaming: $file -> $new_name"
    mv "$file" "$new_name"
done

echo "Done! All metadata files should now have .meta extension"