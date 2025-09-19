#!/bin/bash

# Cleanup script for orphaned metadata files in bytefreezer-proxy spooling
# These are metadata files that reference queue files that have been moved to DLQ

SPOOL_DIR="/tmp/bytefreezer-proxy"

if [ ! -d "$SPOOL_DIR" ]; then
    echo "Spooling directory $SPOOL_DIR does not exist"
    exit 0
fi

echo "Checking for orphaned metadata files in $SPOOL_DIR..."

orphaned_count=0
fixed_count=0

# Find all .meta files in meta directories
find "$SPOOL_DIR" -path "*/meta/*.meta" -type f | while read -r meta_file; do
    echo "Checking: $meta_file"

    # Read the filename from the metadata file
    filename=$(grep -o '"filename":"[^"]*"' "$meta_file" | sed 's/"filename":"//' | sed 's/"//')

    if [ -n "$filename" ]; then
        # Check if the referenced file exists
        if [ ! -f "$filename" ]; then
            echo "  ✗ Referenced file missing: $filename"

            # Check if there's a corresponding file in DLQ
            dir_path=$(dirname "$meta_file")
            tenant_dataset_path=$(dirname "$dir_path")
            dlq_path="$tenant_dataset_path/dlq"
            base_filename=$(basename "$filename")
            dlq_file="$dlq_path/$base_filename"

            if [ -f "$dlq_file" ]; then
                echo "  → Found in DLQ: $dlq_file"
                echo "  → Removing orphaned metadata: $meta_file"
                rm "$meta_file"
                ((fixed_count++))
            else
                echo "  → File not found in DLQ either, removing orphaned metadata"
                rm "$meta_file"
                ((orphaned_count++))
            fi
        else
            echo "  ✓ Referenced file exists: $filename"
        fi
    else
        echo "  ⚠ Could not extract filename from metadata"
    fi
done

echo ""
echo "Cleanup complete:"
echo "  - Removed $fixed_count orphaned metadata files (files moved to DLQ)"
echo "  - Removed $orphaned_count truly orphaned metadata files (files completely missing)"