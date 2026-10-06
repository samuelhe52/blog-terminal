---
title: "Notes on Linear Attention"
description: "Rewriting softmax attention with a decomposable kernel turns quadratic cost into linear cost."
date: 2026-05-20
lang: "en"
translationSlug: "attention-notes"
author: "fixture"
---

> This is a sample article for testing the terminal reader. It follows [Transformers are RNNs](https://arxiv.org/abs/2006.16236), but every derivation and example here was written for the test suite.

We first review standard softmax attention, then show why replacing the similarity function with a decomposable kernel makes the whole computation grow linearly with sequence length.

## Softmax attention

Let $Q, K \in \mathbb{R}^{N \times d_k}$ be the query and key matrices and $V \in \mathbb{R}^{N \times d_v}$ the value matrix. Scaled dot-product attention is

$$
O = \operatorname{softmax}\left(\frac{QK^T}{\sqrt{d_k}}\right)V,
$$

which costs $O(N^2 d_k)$ for the scores and $O(N^2 d_v)$ for the weighted sum. Because the cost is quadratic in $N$, long contexts are expensive.

## Linear attention

With a feature map $\phi: \mathbb{R}^{d_k} \to \mathbb{R}^{r}$, define $\kappa(q_i, k_j) = \phi(q_i)^T \phi(k_j)$ and substitute:

$$
\begin{aligned}
o_i &= \frac{\sum_{j=1}^{N} \phi(q_i)^T \phi(k_j) v_j}{\sum_{j=1}^{N} \phi(q_i)^T \phi(k_j)} = \frac{\phi(q_i)^T \left(\sum_{j=1}^{N} \phi(k_j) v_j^T\right)}{\phi(q_i)^T \left(\sum_{j=1}^{N} \phi(k_j)\right)}, \\
&= \frac{\phi(q_i)^T S}{\phi(q_i)^T z}.
\end{aligned}
$$

<img src="/images/fixture/linear-attention.svg" alt="Linear attention: Q multiplied by the precomputed K^T V summary gives O." width="640" height="300" loading="lazy" />

| Method | Time | Memory | Note |
| --- | --- | --- | --- |
| Softmax attention | $O(N^2 d)$ | $O(N^2)$ | Needs the full score matrix |
| Linear attention | $O(N d^2)$ | $O(d^2)$ | State size is independent of length |

In a shell, the dollar in `echo $HOME` is not math, and neither is this:

```bash
echo "state size: ${STATE_DIM}x${STATE_DIM}, cost: $((STATE_DIM * STATE_DIM)) floats"
```

## The causal case

During generation only the prefix is visible, so the state updates recursively: $S_t = S_{t-1} + \phi(k_t) v_t^T$ and $z_t = z_{t-1} + \phi(k_t)$.
