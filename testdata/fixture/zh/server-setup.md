---
title: "搭建双节点推理服务"
description: "环境变量、镜像检查，以及双机部署时的常见问题。"
date: 2026-06-30
lang: "zh-CN"
translationSlug: "server-setup"
author: "fixture"
---

> 下面的完整流程比较长。如果只关心排错，可以直接跳到[常见问题与解决办法](/zh/posts/server-setup/#常见问题与解决办法)。

## 环境

```bash
export WORKSPACE=/home/YOUR_USER/inference-setup
export MODEL_PATH="$WORKSPACE/models/example-model"
```

`WORKER_A_IP` 和 `WORKER_B_IP` 必须是两台服务器之间可以直接互访的内网地址，否则路由器无法把请求分发到另一台机器上。

## 常见问题与解决办法

| 现象 | 原因 | 解决办法 |
| --- | --- | --- |
| 任务在智能体启动前就失败 | 网络不稳定导致镜像拉取超时 | 提前拉取所有镜像并校验架构 |
