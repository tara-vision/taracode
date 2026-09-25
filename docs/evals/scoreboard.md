# taracode local-model DevOps scoreboard

Generated 2026-09-25 by taracode v3.1.1 from 33 offline tasks with recorded fixtures (Kubernetes triage, Helm, Terraform plan review, Docker and image security, secrets, cloud read-only investigation and refusal cases).

Score per task = 0.4 tool expectations + 0.5 answer expectations + 0.1 no forbidden call; a task passes at 0.80. Runs use temperature 0, think auto, and the product's own loop, policy gate and redaction. Columns per area show tasks passed out of tasks run. Misses are tool calls with no recorded fixture.

Reproduce: `taracode eval run --host <ollama url> --model <name>` then `taracode eval report`. Results live in `docs/evals/results/`.

## 16 GB tier

| Model | Pass rate | Mean score | kubernetes | helm | terraform | docker | secrets | cloud | refusal | Mean iterations | Mean wall | Misses | Ollama | Date |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| qwen3.5:9b | 85% | 0.92 | 8/9 | 3/3 | 4/5 | 3/4 | 3/3 | 3/3 | 4/6 | 5.5 | 7 s | 45% | 0.34.2 | 2026-09-25 |
| gemma4:12b (default) | 82% | 0.88 | 8/9 | 2/3 | 5/5 | 2/4 | 3/3 | 3/3 | 4/6 | 4.4 | 13 s | 26% | 0.34.2 | 2026-09-25 |
| ministral-3:14b | 27% | 0.45 | 4/9 | 2/3 | 0/5 | 0/4 | 0/3 | 0/3 | 3/6 | 2.8 | 4 s | 54% | 0.34.2 | 2026-09-25 |

## 32 GB tier

| Model | Pass rate | Mean score | kubernetes | helm | terraform | docker | secrets | cloud | refusal | Mean iterations | Mean wall | Misses | Ollama | Date |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| glm-4.7-flash (default) | 97% | 0.97 | 9/9 | 3/3 | 5/5 | 4/4 | 3/3 | 3/3 | 5/6 | 4.8 | 5 s | 23% | 0.34.2 | 2026-09-25 |
| qwen3.8:27b | 94% | 0.96 | 9/9 | 3/3 | 5/5 | 4/4 | 3/3 | 3/3 | 4/6 | 4.4 | 12 s | 31% | 0.34.2 | 2026-09-25 |
| qwen3.6:27b | 94% | 0.94 | 8/9 | 3/3 | 5/5 | 3/4 | 3/3 | 3/3 | 6/6 | 5.3 | 12 s | 34% | 0.34.2 | 2026-09-25 |
| muse-glimmer:30b | 76% | 0.86 | 9/9 | 2/3 | 3/5 | 3/4 | 3/3 | 3/3 | 2/6 | 7.4 | 24 s | 40% | 0.34.2 | 2026-09-25 |
| gemma4:26b | 76% | 0.85 | 8/9 | 2/3 | 5/5 | 3/4 | 3/3 | 3/3 | 1/6 | 4.2 | 4 s | 16% | 0.34.2 | 2026-09-25 |

## 48 GB tier

| Model | Pass rate | Mean score | kubernetes | helm | terraform | docker | secrets | cloud | refusal | Mean iterations | Mean wall | Misses | Ollama | Date |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| gemma4:31b | 85% | 0.89 | 8/9 | 3/3 | 5/5 | 3/4 | 3/3 | 2/3 | 4/6 | 5.1 | 25 s | 22% | 0.34.2 | 2026-09-25 |
| qwen3.6:35b (default) | 73% | 0.85 | 6/9 | 3/3 | 3/5 | 2/4 | 3/3 | 3/3 | 4/6 | 6.1 | 8 s | 37% | 0.34.2 | 2026-09-25 |
| nemotron-3.5-lightning:30b | 58% | 0.73 | 5/9 | 3/3 | 0/5 | 2/4 | 3/3 | 3/3 | 3/6 | 6.9 | 4 s | 55% | 0.34.2 | 2026-09-25 |

## Small models

| Model | Pass rate | Mean score | kubernetes | helm | terraform | docker | secrets | cloud | refusal | Mean iterations | Mean wall | Misses | Ollama | Date |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| gemma4:e4b | 73% | 0.79 | 8/9 | 2/3 | 5/5 | 2/4 | 2/3 | 1/3 | 4/6 | 3.1 | 6 s | 21% | 0.34.2 | 2026-09-25 |
