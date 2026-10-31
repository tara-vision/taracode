# taracode local-model DevOps scoreboard

Generated 2026-10-31 by taracode 3.5.0 from 33 offline tasks, corpus v2, with recorded fixtures (Kubernetes triage, Helm, Terraform plan review, Docker and image security, secrets, cloud read-only investigation and refusal cases).

Score per task = 0.4 tool expectations + 0.5 answer expectations + 0.1 no forbidden call; a task passes at 0.80. Runs use temperature 0, think auto, and the product's own loop, policy gate and redaction. Columns per area show tasks passed out of tasks run. Misses are tool calls with no recorded fixture.

Reproduce: `taracode eval run --host <engine url> --model <name>` then `taracode eval report`. Results live in `docs/evals/results/`.

## Apple M4 Max (48 GB)

8 models, taraengine 0.5.0, context window 32768, one run each. Ranked by pass rate, then mean score, then tokens per second. Tokens/s is the engine's own generation rate. VRAM is the engine's own estimate (~), not a measurement: run on taraengine with an NVIDIA GPU, or with --gpu-probe, to measure it.

| # | Model | Engine | Tier | Pass rate | Mean score | Tokens/s | Mean wall | VRAM | Quant | Runs |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | glm-4.7-flash | taraengine 0.5.0 | 32 GB | 94% | 0.92 | 49 | 26 s | ~17.8 GiB | Q4_K | 1 |
| 2 | gemma4:31b | taraengine 0.5.0 | 48 GB | 91% | 0.94 | 42 | 48 s | ~21.6 GiB | Q4_0 | 1 |
| 3 | qwen3.8:27b | taraengine 0.5.0 | 32 GB | 91% | 0.93 | 39 | 69 s | ~22.0 GiB | Q4_K_M | 1 |
| 4 | gemma4:12b | taraengine 0.5.0 | 16 GB | 88% | 0.93 | 87 | 19 s | ~8.6 GiB | Q4_0 | 1 |
| 5 | qwen3.6:35b | taraengine 0.5.0 | 48 GB | 85% | 0.91 | 124 | 11 s | ~21.2 GiB | Q4_K_M | 1 |
| 6 | qwen3.6:27b | taraengine 0.5.0 | 32 GB | 85% | 0.90 | 39 | 41 s | ~21.7 GiB | Q4_K_M | 1 |
| 7 | gemma4:26b | taraengine 0.5.0 | 32 GB | 76% | 0.85 | 131 | 13 s | ~15.7 GiB | Q4_0 | 1 |
| 8 | gemma4:e4b | taraengine 0.5.0 | small | 42% | 0.63 | 124 | 8 s | ~5.5 GiB | Q4_0 | 1 |

## NVIDIA RTX 5090 (32 GB)

9 models, taraengine 0.5.0, context window 32768, one run each. Ranked by pass rate, then mean score, then tokens per second. Tokens/s is the engine's own generation rate. VRAM is the GPU memory in use with the model loaded, measured on the machine at the context window the run asked for, by taraengine for its own server or by the operator's probe.

| # | Model | Engine | Tier | Pass rate | Mean score | Tokens/s | Mean wall | VRAM | Quant | Runs |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | qwen3.8:27b | taraengine 0.5.0 | 32 GB | 100% | 0.98 | 129 | 19 s | 21.4 GiB | Q4_K_M | 1 |
| 2 | glm-4.7-flash | taraengine 0.5.0 | 32 GB | 97% | 0.97 | 222 | 5 s | 18.4 GiB | Q4_K | 1 |
| 3 | qwen3.6:35b | taraengine 0.5.0 | 48 GB | 94% | 0.95 | 395 | 4 s | 21.7 GiB | Q4_K_M | 1 |
| 4 | nemotron-3.5-bolt:74b [1] | taraengine 0.5.0 | 48 GB | 91% | 0.95 | 328 | 9 s | 24.9 GiB | IQ2_XXS | 1 |
| 5 | gemma4:31b | taraengine 0.5.0 | 48 GB | 91% | 0.93 | 175 | 11 s | 22.4 GiB | Q4_0 | 1 |
| 6 | qwen3.6:27b | taraengine 0.5.0 | 32 GB | 88% | 0.92 | 129 | 9 s | 21.5 GiB | Q4_K_M | 1 |
| 7 | gemma4:26b | taraengine 0.5.0 | 32 GB | 79% | 0.89 | 431 | 4 s | 16.5 GiB | Q4_0 | 1 |
| 8 | gemma4:12b | taraengine 0.5.0 | 16 GB | 79% | 0.89 | 329 | 6 s | 9.0 GiB | Q4_0 | 1 |
| 9 | gemma4:e4b | taraengine 0.5.0 | small | 52% | 0.66 | 410 | 2 s | 4.8 GiB | Q4_0 | 1 |

[1] outside the catalog: docs/evals/overrides/nemotron-3.5-bolt-74b.taraengine-0.5.0.registry.yaml

## 16 GB tier

| Model | Machine | Pass rate | Mean score | kubernetes | helm | terraform | docker | secrets | cloud | refusal | Mean iterations | Mean wall | Misses | Engine | Date |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| gemma4:12b (default) | Apple M4 Max (48 GB) | 88% | 0.93 | 9/9 | 3/3 | 4/5 | 4/4 | 3/3 | 3/3 | 3/6 | 5.0 | 19 s | 12% | taraengine 0.5.0 | 2026-10-31 |
| gemma4:12b (default) | NVIDIA RTX 5090 (32 GB) | 79% | 0.89 | 9/9 | 1/3 | 3/5 | 4/4 | 3/3 | 3/3 | 3/6 | 5.0 | 6 s | 17% | taraengine 0.5.0 | 2026-10-31 |

## 32 GB tier

| Model | Machine | Pass rate | Mean score | kubernetes | helm | terraform | docker | secrets | cloud | refusal | Mean iterations | Mean wall | Misses | Engine | Date |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| qwen3.8:27b | NVIDIA RTX 5090 (32 GB) | 100% | 0.98 | 9/9 | 3/3 | 5/5 | 4/4 | 3/3 | 3/3 | 6/6 | 5.8 | 19 s | 35% | taraengine 0.5.0 | 2026-10-31 |
| glm-4.7-flash (default) | NVIDIA RTX 5090 (32 GB) | 97% | 0.97 | 9/9 | 3/3 | 5/5 | 3/4 | 3/3 | 3/3 | 6/6 | 5.2 | 5 s | 26% | taraengine 0.5.0 | 2026-10-31 |
| glm-4.7-flash (default) | Apple M4 Max (48 GB) | 94% | 0.92 | 8/9 | 3/3 | 5/5 | 4/4 | 2/3 | 3/3 | 6/6 | 5.4 | 26 s | 33% | taraengine 0.5.0 | 2026-10-31 |
| qwen3.8:27b | Apple M4 Max (48 GB) | 91% | 0.93 | 9/9 | 3/3 | 4/5 | 4/4 | 3/3 | 3/3 | 4/6 | 5.6 | 69 s | 37% | taraengine 0.5.0 | 2026-10-31 |
| qwen3.6:27b | NVIDIA RTX 5090 (32 GB) | 88% | 0.92 | 9/9 | 3/3 | 3/5 | 4/4 | 3/3 | 3/3 | 4/6 | 4.6 | 9 s | 22% | taraengine 0.5.0 | 2026-10-31 |
| qwen3.6:27b | Apple M4 Max (48 GB) | 85% | 0.90 | 9/9 | 3/3 | 3/5 | 4/4 | 3/3 | 3/3 | 3/6 | 5.2 | 41 s | 32% | taraengine 0.5.0 | 2026-10-31 |
| gemma4:26b | NVIDIA RTX 5090 (32 GB) | 79% | 0.89 | 8/9 | 2/3 | 5/5 | 3/4 | 3/3 | 3/3 | 2/6 | 3.9 | 4 s | 6% | taraengine 0.5.0 | 2026-10-31 |
| gemma4:26b | Apple M4 Max (48 GB) | 76% | 0.85 | 8/9 | 2/3 | 4/5 | 3/4 | 3/3 | 3/3 | 2/6 | 4.7 | 13 s | 20% | taraengine 0.5.0 | 2026-10-31 |

## 48 GB tier

| Model | Machine | Pass rate | Mean score | kubernetes | helm | terraform | docker | secrets | cloud | refusal | Mean iterations | Mean wall | Misses | Engine | Date |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| qwen3.6:35b | NVIDIA RTX 5090 (32 GB) | 94% | 0.95 | 7/9 | 3/3 | 5/5 | 4/4 | 3/3 | 3/3 | 6/6 | 5.6 | 4 s | 27% | taraengine 0.5.0 | 2026-10-31 |
| nemotron-3.5-bolt:74b [1] | NVIDIA RTX 5090 (32 GB) | 91% | 0.95 | 9/9 | 3/3 | 4/5 | 3/4 | 3/3 | 2/3 | 6/6 | 6.2 | 9 s | 38% | taraengine 0.5.0 | 2026-10-31 |
| gemma4:31b (default) | Apple M4 Max (48 GB) | 91% | 0.94 | 9/9 | 3/3 | 5/5 | 3/4 | 2/3 | 3/3 | 5/6 | 5.3 | 48 s | 20% | taraengine 0.5.0 | 2026-10-31 |
| gemma4:31b (default) | NVIDIA RTX 5090 (32 GB) | 91% | 0.93 | 9/9 | 3/3 | 5/5 | 3/4 | 2/3 | 3/3 | 5/6 | 5.2 | 11 s | 20% | taraengine 0.5.0 | 2026-10-31 |
| qwen3.6:35b | Apple M4 Max (48 GB) | 85% | 0.91 | 6/9 | 3/3 | 5/5 | 2/4 | 3/3 | 3/3 | 6/6 | 5.4 | 11 s | 30% | taraengine 0.5.0 | 2026-10-31 |

[1] outside the catalog: docs/evals/overrides/nemotron-3.5-bolt-74b.taraengine-0.5.0.registry.yaml

## Small models

| Model | Machine | Pass rate | Mean score | kubernetes | helm | terraform | docker | secrets | cloud | refusal | Mean iterations | Mean wall | Misses | Engine | Date |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| gemma4:e4b (default) | NVIDIA RTX 5090 (32 GB) | 52% | 0.66 | 2/9 | 1/3 | 4/5 | 3/4 | 2/3 | 2/3 | 3/6 | 2.5 | 2 s | 24% | taraengine 0.5.0 | 2026-10-31 |
| gemma4:e4b (default) | Apple M4 Max (48 GB) | 42% | 0.63 | 1/9 | 1/3 | 5/5 | 2/4 | 1/3 | 1/3 | 3/6 | 2.4 | 8 s | 24% | taraengine 0.5.0 | 2026-10-31 |
