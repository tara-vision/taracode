# taracode local-model DevOps scoreboard

Generated 2026-10-01 by taracode v3.2.1 from 33 offline tasks with recorded fixtures (Kubernetes triage, Helm, Terraform plan review, Docker and image security, secrets, cloud read-only investigation and refusal cases).

Score per task = 0.4 tool expectations + 0.5 answer expectations + 0.1 no forbidden call; a task passes at 0.80. Runs use temperature 0, think auto, and the product's own loop, policy gate and redaction. Columns per area show tasks passed out of tasks run. Misses are tool calls with no recorded fixture.

Reproduce: `taracode eval run --host <ollama url> --model <name>` then `taracode eval report`. Results live in `docs/evals/results/`.

## NVIDIA RTX 5090 (32 GB)

18 models, Ollama 0.35.0, context window 32768, one run each. Ranked by pass rate, then mean score, then tokens per second. Tokens/s is the engine's own generation rate. VRAM is the GPU memory in use with the model loaded, measured on the machine at the context window the run asked for.

| # | Model | Tier | Pass rate | Mean score | Tokens/s | Mean wall | VRAM | Quant | Runs |
|---|---|---|---|---|---|---|---|---|---|
| 1 | gemma4:31b | 48 GB | 94% | 0.95 | 64 | 22 s | 23.1 GiB | Q4_K_M | 1 |
| 2 | qwen3.8:27b | 32 GB | 91% | 0.95 | 117 | 11 s | 20.1 GiB | Q4_K_M | 1 |
| 3 | glm-4.7-flash | 32 GB | 88% | 0.94 | 212 | 4 s | 19.9 GiB | Q4_K_M | 1 |
| 4 | qwen3.6:27b | 32 GB | 88% | 0.94 | 123 | 12 s | 20.0 GiB | Q4_K_M | 1 |
| 5 | gemma4:12b | 16 GB | 88% | 0.92 | 138 | 12 s | 9.2 GiB | Q4_K_M | 1 |
| 6 | granite4.1:30b | - | 88% | 0.91 | 75 | 12 s | 25.1 GiB | Q4_K_M | 1 |
| 7 | qwen3.5:9b | 16 GB | 85% | 0.92 | 178 | 7 s | 7.7 GiB | Q4_K_M | 1 |
| 8 | ornith:35b | - | 82% | 0.89 | 266 | 6 s | 21.0 GiB | Q4_K_M | 1 |
| 9 | muse-glimmer:30b | 32 GB | 79% | 0.90 | 74 | 27 s | 17.9 GiB | Q4_K_M | 1 |
| 10 | gemma4:26b | 32 GB | 79% | 0.87 | 347 | 4 s | 19.6 GiB | Q4_K_M | 1 |
| 11 | qwen3.6:35b | 48 GB | 79% | 0.86 | 226 | 7 s | 23.5 GiB | Q4_K_M | 1 |
| 12 | nemotron-cascade-2:30b | - | 76% | 0.85 | 344 | 9 s | 23.5 GiB | Q4_K_M | 1 |
| 13 | gemma4:e4b | small | 70% | 0.80 | 222 | 6 s | 5.3 GiB | Q4_K_M | 1 |
| 14 | laguna-xs-2.1 | - | 67% | 0.81 | 289 | 4 s | 21.1 GiB | Q4_K_M | 1 |
| 15 | gpt-oss:20b | - | 58% | 0.78 | 279 | 5 s | 13.4 GiB | MXFP4 | 1 |
| 16 | nemotron-3.5-lightning:30b | 48 GB | 52% | 0.72 | 427 | 4 s | 24.6 GiB | Q4_K_M | 1 |
| 17 | devstral-small-2:24b | - | 42% | 0.59 | 87 | 5 s | 19.9 GiB | Q4_K_M | 1 |
| 18 | ministral-3:14b | 16 GB | 27% | 0.45 | 136 | 3 s | 14.0 GiB | Q4_K_M | 1 |

## 16 GB tier

| Model | Pass rate | Mean score | kubernetes | helm | terraform | docker | secrets | cloud | refusal | Mean iterations | Mean wall | Misses | Ollama | Date |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| gemma4:12b (default) | 88% | 0.92 | 9/9 | 3/3 | 5/5 | 2/4 | 3/3 | 3/3 | 4/6 | 4.6 | 12 s | 22% | 0.35.0 | 2026-10-01 |
| qwen3.5:9b | 85% | 0.92 | 8/9 | 2/3 | 3/5 | 4/4 | 3/3 | 3/3 | 5/6 | 5.2 | 7 s | 42% | 0.35.0 | 2026-10-01 |
| ministral-3:14b | 27% | 0.45 | 5/9 | 2/3 | 0/5 | 0/4 | 0/3 | 0/3 | 2/6 | 2.3 | 3 s | 67% | 0.35.0 | 2026-10-01 |

## 32 GB tier

| Model | Pass rate | Mean score | kubernetes | helm | terraform | docker | secrets | cloud | refusal | Mean iterations | Mean wall | Misses | Ollama | Date |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| qwen3.8:27b | 91% | 0.95 | 8/9 | 3/3 | 4/5 | 4/4 | 3/3 | 3/3 | 5/6 | 4.3 | 11 s | 28% | 0.35.0 | 2026-10-01 |
| glm-4.7-flash (default) | 88% | 0.94 | 9/9 | 2/3 | 5/5 | 3/4 | 2/3 | 3/3 | 5/6 | 4.9 | 4 s | 28% | 0.35.0 | 2026-10-01 |
| qwen3.6:27b | 88% | 0.94 | 7/9 | 3/3 | 5/5 | 4/4 | 3/3 | 3/3 | 4/6 | 5.4 | 12 s | 36% | 0.35.0 | 2026-10-01 |
| muse-glimmer:30b | 79% | 0.90 | 9/9 | 2/3 | 4/5 | 3/4 | 3/3 | 3/3 | 2/6 | 7.5 | 27 s | 37% | 0.35.0 | 2026-10-01 |
| gemma4:26b | 79% | 0.87 | 9/9 | 3/3 | 4/5 | 3/4 | 2/3 | 3/3 | 2/6 | 4.3 | 4 s | 15% | 0.35.0 | 2026-10-01 |

## 48 GB tier

| Model | Pass rate | Mean score | kubernetes | helm | terraform | docker | secrets | cloud | refusal | Mean iterations | Mean wall | Misses | Ollama | Date |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| gemma4:31b | 94% | 0.95 | 9/9 | 3/3 | 5/5 | 4/4 | 3/3 | 2/3 | 5/6 | 4.7 | 22 s | 15% | 0.35.0 | 2026-10-01 |
| qwen3.6:35b (default) | 79% | 0.86 | 7/9 | 3/3 | 3/5 | 4/4 | 3/3 | 3/3 | 3/6 | 6.1 | 7 s | 43% | 0.35.0 | 2026-10-01 |
| nemotron-3.5-lightning:30b | 52% | 0.72 | 3/9 | 3/3 | 2/5 | 1/4 | 3/3 | 3/3 | 2/6 | 6.9 | 4 s | 54% | 0.35.0 | 2026-10-01 |

## Small models

| Model | Pass rate | Mean score | kubernetes | helm | terraform | docker | secrets | cloud | refusal | Mean iterations | Mean wall | Misses | Ollama | Date |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| gemma4:e4b | 70% | 0.80 | 8/9 | 2/3 | 5/5 | 2/4 | 2/3 | 1/3 | 3/6 | 3.2 | 6 s | 33% | 0.35.0 | 2026-10-01 |

## Other models

| Model | Pass rate | Mean score | kubernetes | helm | terraform | docker | secrets | cloud | refusal | Mean iterations | Mean wall | Misses | Ollama | Date |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| granite4.1:30b | 88% | 0.91 | 6/9 | 3/3 | 5/5 | 3/4 | 3/3 | 3/3 | 6/6 | 4.2 | 12 s | 42% | 0.35.0 | 2026-10-01 |
| ornith:35b | 82% | 0.89 | 6/9 | 3/3 | 4/5 | 3/4 | 3/3 | 3/3 | 5/6 | 5.8 | 6 s | 42% | 0.35.0 | 2026-10-01 |
| nemotron-cascade-2:30b | 76% | 0.85 | 5/9 | 2/3 | 5/5 | 3/4 | 3/3 | 3/3 | 4/6 | 5.7 | 9 s | 49% | 0.35.0 | 2026-10-01 |
| laguna-xs-2.1 | 67% | 0.81 | 7/9 | 2/3 | 3/5 | 2/4 | 2/3 | 3/3 | 3/6 | 7.0 | 4 s | 39% | 0.35.0 | 2026-10-01 |
| gpt-oss:20b | 58% | 0.78 | 4/9 | 2/3 | 1/5 | 3/4 | 3/3 | 3/3 | 3/6 | 6.0 | 5 s | 59% | 0.35.0 | 2026-10-01 |
| devstral-small-2:24b | 42% | 0.59 | 0/9 | 2/3 | 2/5 | 2/4 | 2/3 | 3/3 | 3/6 | 3.7 | 5 s | 52% | 0.35.0 | 2026-10-01 |
