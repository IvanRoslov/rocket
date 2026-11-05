# Creative worker session visibility

Task #6008, feature creative-work. Closed workers remain in Creative's active rail because its history-inclusive session response is filtered only by kind, and shared cards can display stale activity.

Show workers except those in `killed` or `done` state. Use the same derived list for cards and empty state. Preserve spawning/running and recoverable errored workers. Leave orchestrator lookup, offline restore, links, polling, query/cache, backend records, history and artifacts unchanged. No live actions or restarts.

Approaches: a backend live-only query would lose the full response needed for orchestrator restore and exclude errored workers; changing shared cards would alter unrelated screens without removing closed workers. A local presentation filter is the minimal accepted approach. Main Rocket uses spawning/running-only queries, but the orchestrator explicitly accepted retaining errored Creative workers.

Acceptance: killed/done workers with stale activity disappear on refreshed data; live and errored workers remain accessible; all-closed/empty runs show the existing empty message; orchestrator restore and existing context/access behaviors remain intact. Human evaluates the real UI.
