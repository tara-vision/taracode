# taracode local-model DevOps scoreboard

Generated 2026-10-20 by taracode 3.3.2 from 33 offline tasks with recorded fixtures (Kubernetes triage, Helm, Terraform plan review, Docker and image security, secrets, cloud read-only investigation and refusal cases).

Score per task = 0.4 tool expectations + 0.5 answer expectations + 0.1 no forbidden call; a task passes at 0.80. Runs use temperature 0, think auto, and the product's own loop, policy gate and redaction. Columns per area show tasks passed out of tasks run. Misses are tool calls with no recorded fixture.

Reproduce: `taracode eval run --host <engine url> --model <name>` then `taracode eval report`. Results live in `docs/evals/results/`.

## Apple M4 Max (48 GB)

8 models, taraengine 0.3.1, context window 32768, one run each. Ranked by pass rate, then mean score, then tokens per second. Tokens/s is the engine's own generation rate. VRAM is the engine's own estimate (~), not a measurement: run on taraengine with an NVIDIA GPU, or with --gpu-probe, to measure it.

| # | Model | Engine | Tier | Pass rate | Mean score | Tokens/s | Mean wall | VRAM | Quant | Runs |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | qwen3.8:27b | taraengine 0.3.1 | 32 GB | 94% | 0.95 | 39 | 72 s | ~23.4 GiB | Q4_K_M | 1 |
| 2 | glm-4.7-flash | taraengine 0.3.1 | 32 GB | 91% | 0.92 | 48 | 28 s | ~17.8 GiB | Q4_K | 1 |
| 3 | gemma4:31b | taraengine 0.3.1 | 48 GB | 88% | 0.93 | 42 | 46 s | ~23.9 GiB | Q4_0 | 1 |
| 4 | gemma4:12b | taraengine 0.3.1 | 16 GB | 88% | 0.92 | 83 | 23 s | ~9.6 GiB | Q4_0 | 1 |
| 5 | qwen3.6:35b | taraengine 0.3.1 | 48 GB | 88% | 0.91 | 124 | 11 s | ~22.0 GiB | Q4_K_M | 1 |
| 6 | qwen3.6:27b | taraengine 0.3.1 | 32 GB | 85% | 0.92 | 39 | 39 s | ~23.6 GiB | Q4_K_M | 1 |
| 7 | gemma4:26b | taraengine 0.3.1 | 32 GB | 76% | 0.85 | 133 | 11 s | ~16.3 GiB | Q4_0 | 1 |
| 8 | gemma4:e4b | taraengine 0.3.1 | small | 61% | 0.77 | 113 | 10 s | ~5.6 GiB | Q4_0 | 1 |

## NVIDIA RTX 5090 (32 GB)

9 models, taraengine 0.4.0, context window 32768, one run each. Ranked by pass rate, then mean score, then tokens per second. Tokens/s is the engine's own generation rate. VRAM is the GPU memory in use with the model loaded, measured on the machine at the context window the run asked for, by taraengine for its own server or by the operator's probe.

| # | Model | Engine | Tier | Pass rate | Mean score | Tokens/s | Mean wall | VRAM | Quant | Runs |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | qwen3.8:27b | taraengine 0.4.0 | 32 GB | 97% | 0.97 | 128 | 17 s | 23.2 GiB | Q4_K_M | 1 |
| 2 | qwen3.6:27b | taraengine 0.4.0 | 32 GB | 94% | 0.94 | 129 | 10 s | 23.3 GiB | Q4_K_M | 1 |
| 3 | gemma4:31b | taraengine 0.4.0 | 48 GB | 88% | 0.92 | 169 | 11 s | 24.8 GiB | Q4_0 | 1 |
| 4 | glm-4.7-flash | taraengine 0.4.0 | 32 GB | 88% | 0.90 | 218 | 5 s | 18.4 GiB | Q4_K | 1 |
| 5 | qwen3.6:35b | taraengine 0.4.0 | 48 GB | 82% | 0.90 | 397 | 4 s | 22.4 GiB | Q4_K_M | 1 |
| 6 | gemma4:12b | taraengine 0.4.0 | 16 GB | 82% | 0.89 | 318 | 6 s | 10.0 GiB | Q4_0 | 1 |
| 7 | nemotron-3.5-bolt:74b | taraengine 0.4.0 | 48 GB | 79% | 0.88 | 331 | 9 s | 28.3 GiB | IQ2_XXS | 1 |
| 8 | gemma4:26b | taraengine 0.4.0 | 32 GB | 73% | 0.84 | 425 | 5 s | 17.0 GiB | Q4_0 | 1 |
| 9 | gemma4:e4b | taraengine 0.4.0 | small | 61% | 0.76 | 409 | 2 s | 4.8 GiB | Q4_0 | 1 |

## 16 GB tier

| Model | Machine | Pass rate | Mean score | kubernetes | helm | terraform | docker | secrets | cloud | refusal | Mean iterations | Mean wall | Misses | Engine | Date |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| gemma4:12b (default) | Apple M4 Max (48 GB) | 88% | 0.92 | 9/9 | 2/3 | 4/5 | 4/4 | 3/3 | 3/3 | 4/6 | 5.4 | 23 s | 21% | taraengine 0.3.1 | 2026-10-17 |
| gemma4:12b (default) | NVIDIA RTX 5090 (32 GB) | 82% | 0.89 | 9/9 | 2/3 | 2/5 | 4/4 | 3/3 | 3/3 | 4/6 | 5.1 | 6 s | 17% | taraengine 0.4.0 | 2026-10-20 |

## 32 GB tier

| Model | Machine | Pass rate | Mean score | kubernetes | helm | terraform | docker | secrets | cloud | refusal | Mean iterations | Mean wall | Misses | Engine | Date |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| qwen3.8:27b | NVIDIA RTX 5090 (32 GB) | 97% | 0.97 | 9/9 | 3/3 | 5/5 | 4/4 | 3/3 | 3/3 | 5/6 | 5.6 | 17 s | 40% | taraengine 0.4.0 | 2026-10-20 |
| qwen3.8:27b | Apple M4 Max (48 GB) | 94% | 0.95 | 9/9 | 3/3 | 4/5 | 4/4 | 3/3 | 3/3 | 5/6 | 5.7 | 72 s | 40% | taraengine 0.3.1 | 2026-10-17 |
| qwen3.6:27b | NVIDIA RTX 5090 (32 GB) | 94% | 0.94 | 9/9 | 3/3 | 4/5 | 4/4 | 3/3 | 3/3 | 5/6 | 5.4 | 10 s | 26% | taraengine 0.4.0 | 2026-10-20 |
| qwen3.6:27b | Apple M4 Max (48 GB) | 85% | 0.92 | 9/9 | 3/3 | 3/5 | 4/4 | 3/3 | 3/3 | 3/6 | 4.9 | 39 s | 30% | taraengine 0.3.1 | 2026-10-17 |
| glm-4.7-flash (default) | Apple M4 Max (48 GB) | 91% | 0.92 | 9/9 | 3/3 | 5/5 | 3/4 | 2/3 | 3/3 | 5/6 | 5.4 | 28 s | 35% | taraengine 0.3.1 | 2026-10-17 |
| glm-4.7-flash (default) | NVIDIA RTX 5090 (32 GB) | 88% | 0.90 | 8/9 | 3/3 | 5/5 | 2/4 | 2/3 | 3/3 | 6/6 | 5.1 | 5 s | 27% | taraengine 0.4.0 | 2026-10-20 |
| gemma4:26b | Apple M4 Max (48 GB) | 76% | 0.85 | 8/9 | 2/3 | 5/5 | 2/4 | 3/3 | 3/3 | 2/6 | 4.3 | 11 s | 24% | taraengine 0.3.1 | 2026-10-17 |
| gemma4:26b | NVIDIA RTX 5090 (32 GB) | 73% | 0.84 | 7/9 | 2/3 | 5/5 | 2/4 | 3/3 | 3/3 | 2/6 | 4.6 | 5 s | 25% | taraengine 0.4.0 | 2026-10-20 |

## 48 GB tier

| Model | Machine | Pass rate | Mean score | kubernetes | helm | terraform | docker | secrets | cloud | refusal | Mean iterations | Mean wall | Misses | Engine | Date |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| gemma4:31b (default) | Apple M4 Max (48 GB) | 88% | 0.93 | 9/9 | 3/3 | 5/5 | 3/4 | 2/3 | 3/3 | 4/6 | 5.1 | 46 s | 25% | taraengine 0.3.1 | 2026-10-17 |
| gemma4:31b (default) | NVIDIA RTX 5090 (32 GB) | 88% | 0.92 | 9/9 | 3/3 | 4/5 | 3/4 | 3/3 | 3/3 | 4/6 | 5.3 | 11 s | 27% | taraengine 0.4.0 | 2026-10-20 |
| qwen3.6:35b | Apple M4 Max (48 GB) | 88% | 0.91 | 7/9 | 3/3 | 4/5 | 3/4 | 3/3 | 3/3 | 6/6 | 5.6 | 11 s | 35% | taraengine 0.3.1 | 2026-10-17 |
| qwen3.6:35b | NVIDIA RTX 5090 (32 GB) | 82% | 0.90 | 7/9 | 3/3 | 3/5 | 2/4 | 3/3 | 3/3 | 6/6 | 5.8 | 4 s | 34% | taraengine 0.4.0 | 2026-10-20 |
| nemotron-3.5-bolt:74b | NVIDIA RTX 5090 (32 GB) | 79% | 0.88 | 9/9 | 3/3 | 2/5 | 2/4 | 3/3 | 3/3 | 4/6 | 6.6 | 9 s | 54% | taraengine 0.4.0 | 2026-10-20 |

## Small models

| Model | Machine | Pass rate | Mean score | kubernetes | helm | terraform | docker | secrets | cloud | refusal | Mean iterations | Mean wall | Misses | Engine | Date |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| gemma4:e4b (default) | Apple M4 Max (48 GB) | 61% | 0.77 | 5/9 | 2/3 | 5/5 | 2/4 | 2/3 | 2/3 | 2/6 | 2.6 | 10 s | 26% | taraengine 0.3.1 | 2026-10-17 |
| gemma4:e4b (default) | NVIDIA RTX 5090 (32 GB) | 61% | 0.76 | 4/9 | 2/3 | 5/5 | 2/4 | 1/3 | 3/3 | 3/6 | 2.7 | 2 s | 19% | taraengine 0.4.0 | 2026-10-20 |
