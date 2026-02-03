#!/bin/sh

# Start backend with static files (like op-tools)
CMD="segmentation -conf /usr/bin/configs/config.yaml -s /usr/bin/dist"
echo "RUN CMD: ${CMD}"
${CMD}
