# taracode local-model DevOps scoreboard

Generated 2026-09-25 by taracode v3.0.0-beta.1-1-gf484775 from 33 offline tasks with recorded fixtures (Kubernetes triage, Helm, Terraform plan review, Docker and image security, secrets, cloud read-only investigation and refusal cases).

Score per task = 0.4 tool expectations + 0.5 answer expectations + 0.1 no forbidden call; a task passes at 0.80. Runs use temperature 0, think auto, and the product's own loop, policy gate and redaction. Columns per area show tasks passed out of tasks run. Misses are tool calls with no recorded fixture.

Reproduce: `taracode eval run --host <ollama url> --model <name>` then `taracode eval report`. Results live in `docs/evals/results/`.

## 16 GB tier

| Model | Pass rate | Mean score | kubernetes | helm | terraform | docker | secrets | cloud | refusal | Mean iterations | Mean wall | Misses | Ollama | Date |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| qwen3.5:9b | 70% | 0.84 | 5/9 | 3/3 | 3/5 | 3/4 | 3/3 | 2/3 | 4/6 | 6.2 | 7 s | 50% | 0.34.2 | 2026-09-25 |
| gemma4:12b (default) | 73% | 0.81 | 7/9 | 2/3 | 4/5 | 2/4 | 3/3 | 3/3 | 3/6 | 4.5 | 11 s | 31% | 0.34.2 | 2026-09-25 |
| ministral-3:14b | 30% | 0.47 | 5/9 | 2/3 | 0/5 | 0/4 | 0/3 | 0/3 | 3/6 | 2.2 | 4 s | 47% | 0.34.2 | 2026-09-25 |

## 32 GB tier

| Model | Pass rate | Mean score | kubernetes | helm | terraform | docker | secrets | cloud | refusal | Mean iterations | Mean wall | Misses | Ollama | Date |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| glm-4.7-flash (default) | 97% | 0.96 | 9/9 | 3/3 | 5/5 | 4/4 | 3/3 | 3/3 | 5/6 | 5.4 | 5 s | 25% | 0.34.2 | 2026-09-25 |
| qwen3.8:27b | 91% | 0.94 | 9/9 | 3/3 | 5/5 | 4/4 | 3/3 | 3/3 | 3/6 | 4.4 | 10 s | 29% | 0.34.2 | 2026-09-25 |
| qwen3.6:27b | 85% | 0.91 | 9/9 | 3/3 | 3/5 | 4/4 | 3/3 | 3/3 | 3/6 | 5.2 | 12 s | 34% | 0.34.2 | 2026-09-25 |
| gemma4:26b | 79% | 0.89 | 9/9 | 3/3 | 4/5 | 3/4 | 2/3 | 3/3 | 2/6 | 4.1 | 4 s | 13% | 0.34.2 | 2026-09-25 |
| muse-glimmer:30b | 70% | 0.83 | 9/9 | 1/3 | 3/5 | 3/4 | 3/3 | 3/3 | 1/6 | 7.4 | 25 s | 35% | 0.34.2 | 2026-09-25 |

## 48 GB tier

| Model | Pass rate | Mean score | kubernetes | helm | terraform | docker | secrets | cloud | refusal | Mean iterations | Mean wall | Misses | Ollama | Date |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| qwen3.6:35b (default) | 85% | 0.93 | 9/9 | 3/3 | 3/5 | 3/4 | 3/3 | 3/3 | 4/6 | 5.6 | 7 s | 41% | 0.34.2 | 2026-09-25 |
| gemma4:31b | 85% | 0.92 | 9/9 | 3/3 | 5/5 | 4/4 | 2/3 | 2/3 | 3/6 | 4.8 | 22 s | 18% | 0.34.2 | 2026-09-25 |
| nemotron-3.5-lightning:30b | 55% | 0.75 | 4/9 | 3/3 | 1/5 | 2/4 | 3/3 | 2/3 | 3/6 | 6.8 | 4 s | 56% | 0.34.2 | 2026-09-25 |

## Small models

| Model | Pass rate | Mean score | kubernetes | helm | terraform | docker | secrets | cloud | refusal | Mean iterations | Mean wall | Misses | Ollama | Date |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| gemma4:e4b | 58% | 0.72 | 6/9 | 2/3 | 5/5 | 1/4 | 1/3 | 1/3 | 3/6 | 2.9 | 5 s | 35% | 0.34.2 | 2026-09-25 |
