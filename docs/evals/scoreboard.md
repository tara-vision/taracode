# taracode local-model DevOps scoreboard

Generated 2026-10-15 by taracode 3.3.0 from 33 offline tasks with recorded fixtures (Kubernetes triage, Helm, Terraform plan review, Docker and image security, secrets, cloud read-only investigation and refusal cases).

Score per task = 0.4 tool expectations + 0.5 answer expectations + 0.1 no forbidden call; a task passes at 0.80. Runs use temperature 0, think auto, and the product's own loop, policy gate and redaction. Columns per area show tasks passed out of tasks run. Misses are tool calls with no recorded fixture.

Reproduce: `taracode eval run --host <engine url> --model <name>` then `taracode eval report`. Results live in `docs/evals/results/`.

## NVIDIA RTX 5090 (32 GB)

8 models, taraengine 0.3.0, context window 32768, one run each. Ranked by pass rate, then mean score, then tokens per second. Tokens/s is the engine's own generation rate. VRAM is the GPU memory in use with the model loaded, measured on the machine at the context window the run asked for, by taraengine for its own server or by the operator's probe.

| # | Model | Engine | Tier | Pass rate | Mean score | Tokens/s | Mean wall | VRAM | Quant | Runs |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | qwen3.8:27b | taraengine 0.3.0 | 32 GB | 94% | 0.96 | 127 | 18 s | 23.2 GiB | Q4_K_M | 1 |
| 2 | glm-4.7-flash | taraengine 0.3.0 | 32 GB | 94% | 0.92 | 208 | 6 s | 18.4 GiB | Q4_K | 1 |
| 3 | gemma4:31b | taraengine 0.3.0 | 48 GB | 91% | 0.94 | 164 | 11 s | 24.8 GiB | Q4_0 | 1 |
| 4 | gemma4:12b | taraengine 0.3.0 | 16 GB | 88% | 0.93 | 311 | 6 s | 10.0 GiB | Q4_0 | 1 |
| 5 | qwen3.6:27b | taraengine 0.3.0 | 32 GB | 82% | 0.90 | 128 | 10 s | 23.3 GiB | Q4_K_M | 1 |
| 6 | gemma4:26b | taraengine 0.3.0 | 32 GB | 76% | 0.86 | 406 | 4 s | 17.0 GiB | Q4_0 | 1 |
| 7 | qwen3.6:35b | taraengine 0.3.0 | 48 GB | 76% | 0.86 | 386 | 4 s | 22.4 GiB | Q4_K_M | 1 |
| 8 | gemma4:e4b | taraengine 0.3.0 | small | 61% | 0.73 | 337 | 3 s | 4.8 GiB | Q4_0 | 1 |

## 16 GB tier

| Model | Pass rate | Mean score | kubernetes | helm | terraform | docker | secrets | cloud | refusal | Mean iterations | Mean wall | Misses | Engine | Date |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| gemma4:12b (default) | 88% | 0.93 | 9/9 | 2/3 | 5/5 | 4/4 | 3/3 | 3/3 | 3/6 | 4.8 | 6 s | 14% | taraengine 0.3.0 | 2026-10-15 |

## 32 GB tier

| Model | Pass rate | Mean score | kubernetes | helm | terraform | docker | secrets | cloud | refusal | Mean iterations | Mean wall | Misses | Engine | Date |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| qwen3.8:27b | 94% | 0.96 | 9/9 | 3/3 | 5/5 | 3/4 | 3/3 | 3/3 | 5/6 | 5.6 | 18 s | 38% | taraengine 0.3.0 | 2026-10-15 |
| glm-4.7-flash (default) | 94% | 0.92 | 9/9 | 3/3 | 5/5 | 3/4 | 2/3 | 3/3 | 6/6 | 5.2 | 6 s | 26% | taraengine 0.3.0 | 2026-10-15 |
| qwen3.6:27b | 82% | 0.90 | 9/9 | 3/3 | 4/5 | 3/4 | 3/3 | 3/3 | 2/6 | 4.9 | 10 s | 33% | taraengine 0.3.0 | 2026-10-15 |
| gemma4:26b | 76% | 0.86 | 8/9 | 2/3 | 5/5 | 2/4 | 3/3 | 3/3 | 2/6 | 4.3 | 4 s | 21% | taraengine 0.3.0 | 2026-10-15 |

## 48 GB tier

| Model | Pass rate | Mean score | kubernetes | helm | terraform | docker | secrets | cloud | refusal | Mean iterations | Mean wall | Misses | Engine | Date |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| gemma4:31b | 91% | 0.94 | 9/9 | 3/3 | 5/5 | 3/4 | 2/3 | 3/3 | 5/6 | 5.2 | 11 s | 24% | taraengine 0.3.0 | 2026-10-15 |
| qwen3.6:35b (default) | 76% | 0.86 | 7/9 | 3/3 | 3/5 | 2/4 | 3/3 | 3/3 | 4/6 | 6.1 | 4 s | 36% | taraengine 0.3.0 | 2026-10-15 |

## Small models

| Model | Pass rate | Mean score | kubernetes | helm | terraform | docker | secrets | cloud | refusal | Mean iterations | Mean wall | Misses | Engine | Date |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| gemma4:e4b (default) | 61% | 0.73 | 3/9 | 2/3 | 5/5 | 2/4 | 2/3 | 2/3 | 4/6 | 2.7 | 3 s | 31% | taraengine 0.3.0 | 2026-10-15 |
