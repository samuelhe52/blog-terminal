---
title: "Setting Up a Two-Node Inference Server"
description: "Environment variables, image checks, and the pitfalls of a two-machine serving setup."
date: 2026-06-30
lang: "en"
translationSlug: "server-setup"
author: "fixture"
---

> The full procedure below is long. You can skip to [Pitfalls and fixes](/en/posts/server-setup/#pitfalls-and-fixes) if you only want the troubleshooting notes.

## Environment

Set these on both machines:

```bash
export WORKSPACE=/home/YOUR_USER/inference-setup
export MODEL_PATH="$WORKSPACE/models/example-model"
export WORKER_A_IP=10.0.0.11
export WORKER_B_IP=10.0.0.12
```

`WORKER_A_IP` and `WORKER_B_IP` must be private addresses that the two servers can use to reach each other directly.

## Check the image

```bash
docker pull "$SERVING_IMAGE"
docker image inspect "$SERVING_IMAGE" \
  --format 'id={{.Id}} architecture={{.Architecture}} created={{.Created}} quoted argument with spaces'
```

1. Pull the image on both machines.
2. Compare the image IDs:
   - they must match exactly;
   - otherwise re-pull with `--platform linux/amd64`.
3. Start the workers.

## Pitfalls and fixes

| Symptom | Cause | Fix |
| --- | --- | --- |
| Tasks fail during setup before the agent starts | Image pulls time out on an unstable network | Pre-pull every image and verify its architecture before starting the run |
| Router returns 502 | A worker is still loading weights | Wait for both health checks |

```python
def ready(workers):
    return all(w.health() == "ok" for w in workers)  # a deliberately long comment that will not fit in narrow terminals
```
