#!/bin/bash
#colors
export red='\033[0;31m'
export green='\033[0;32m'
export NC='\033[0m' # No Color

set -x 


# Function to display help message
show_help() {
  echo "Usage: $0 [--push] [TAG]"
  echo "Options:"
  echo "  --push   Optional. If set, will push to dockerhub dont forget to log into dockerhub first."
  echo "           docker login --username dockerhubusername --password token"
  echo "  TAG      Optional. The tag for image vX.X.X Defaults to 'latest'."
  echo "  --help   Show this help message."
  echo
  echo "If no TAG provided, will build with the tag 'latest'."
  echo "If only '--push' is provided, will build and push with the tag 'latest'."
}

# Default values
PUSH_COMMAND=false
TAG="latest"

# Check for --help or no parameters
if [[ "$#" -eq 0 ]] || [[ "$1" == "--help" ]]; then
  show_help
  sleep 5

  # If only --help is provided, we exit after showing the help message.
  [[ "$1" == "--help" ]] && exit 0
fi

# Parse arguments
for arg in "$@"; do
  case $arg in
    --push)
      PUSH_COMMAND=true
      shift # Remove --push from processing
      ;;
    --help) # Ignore --help if it's not the first argument
      shift
      ;;
    *)
      TAG=$arg
      shift # Remove the tag from processing
      ;;
  esac
done


# Always execute with TAG value
docker build --build-arg GH_PAT=$GH_PAT -t tarsalhq/bytefreezer-proxy:$TAG -f Dockerfile .

# If the --push flag is set, push to dockerhub
if [ "$PUSH_COMMAND" = true ]; then
    docker push tarsalhq/bytefreezer-proxy:$TAG
fi