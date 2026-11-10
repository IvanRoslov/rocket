// Desktop acceptance against the accepted Conversation feed HTTP contract.
// Load runFeedBrowserCheck(page) in a Playwright environment while Vite serves
// this worktree at http://127.0.0.1:5177. All /v1 requests are intercepted;
// this check never contacts or changes a running Rocket daemon.
export async function runFeedBrowserCheck(page) {
  const { mkdir } = await import('node:fs/promises');
  const screenshots = new URL('../../docs/screenshots/creative-feed/', import.meta.url).pathname;
  await mkdir(screenshots, { recursive: true });
  await page.unroute('**/v1/**');
  await page.setViewportSize({ width: 1600, height: 1100 });
  const now = 1794340000;
  const work = { description: 'Build a short atmospheric film.', expected_artifacts: 'Storyboard and scene images', status: 'in_progress', progress: 60, progress_note: 'Opening revised', review_revision: 0, artifact_version_ids: [] };
  const items = [5, 6].map((id, i) => ({ ...work, id, run_id: 7, stage_id: 4, position: i, name: i ? 'Finale' : 'Opening scene' }));
  const run = { id: 7, project_id: 'film', pipeline_id: 1, pipeline_name: 'Short film', name: 'After the rain', brief: 'A quiet story about an unexpected reunion.', status: 'in_progress', orchestrator_session_id: 'creative-7', allowed_profiles: [], stages: [
    { ...work, id: 4, run_id: 7, position: 0, name: 'Scenes', repeated: true, approval: 'item', items },
    { ...work, id: 8, run_id: 7, position: 1, name: 'Edit', repeated: false, approval: 'stage', items: [] },
  ] };
  const message = (id, body, { stage = 4, item = 0, author = 'orchestrator', versions = [] } = {}) => ({ id, run_id: 7, stage_id: stage, item_id: item, kind: 'message', author, author_session_id: author === 'human' ? '' : 'creative-7', body, artifact_version_ids: versions, created_at: now + id * 60, card: null });
  const proposal = (id, body, card) => ({ ...message(id, body, { item: 5, versions: [201] }), kind: 'proposal', card: { status: 'pending', decision: '', decided_at: 0, replaces_entry_id: 0, replaced_by_entry_id: 0, ...card } });
  let entries = [
    message(105, 'I revised the opening with a warmer palette. **Version 2** keeps the framing you liked.', { item: 5, versions: [201] }),
    message(104, 'Keep the framing, but make the light warmer.', { item: 5, author: 'human', versions: [201] }),
    proposal(103, '### Two opening images\nGenerate **2 images** with the configured image tool / Example provider / Model A.\n\nInputs: Storyboard version 2 below, uploaded to the provider only.\n\nCalls: opening-wide, opening-detail (2 calls). **Estimate: $0.20**, based on $0.10 per image. Wait for Go.', { replaces_entry_id: 102 }),
    proposal(102, 'Four opening images — original estimate $0.40. Superseded by the two-image proposal.', { status: 'replaced', replaced_by_entry_id: 103 }),
    message(101, 'The scene outline is ready. Shall we keep the rain in the opening?', { item: 5 }),
  ];
  const markers = new Map();
  const writes = [], reads = [], requests = [], failures = [], consoleErrors = [];
  let archiveAnswered = false;
  page.on('pageerror', error => consoleErrors.push(error.message));
  page.on('console', message => { if (message.type() === 'error') consoleErrors.push(message.text()); });
  const json = (route, body, status = 200) => route.fulfill({ status, contentType: 'application/json', body: JSON.stringify(body) });
  await page.route('**/v1/**', async route => {
    const request = route.request(), url = new URL(request.url()), path = url.pathname, method = request.method();
    requests.push(`${method} ${path}${url.search}`);
    if (path === '/v1/projects') return json(route, [{ id: 'film', name: 'Film studio' }]);
    if (path === '/v1/creative/pipelines') return json(route, []);
    if (path === '/v1/model-profiles') return json(route, { profiles: [] });
    if (path === '/v1/creative/runs') return json(route, [run]);
    if (path === '/v1/creative/runs/7') return json(route, run);
    if (path.endsWith('/sessions')) return json(route, [{ id: 'creative-7', tmux_name: 'film-director', kind: 'orchestrator', state: 'running', agent: 'codex', model: 'configured-model', activity: 'ready' }]);
    if (path.endsWith('/artifacts') && !path.endsWith('/artifact-versions')) return json(route, []);
    if (path === '/v1/creative/artifact-versions/201') return json(route, { id: 201, artifact_id: 30, version_number: 2, filename: 'storyboard.txt', mime: 'text/plain', size: 100, content_url: '/mock/storyboard.txt', download_url: '/mock/storyboard.txt' });
    if (path === '/v1/creative/artifacts/30') return json(route, { artifact: { id: 30, name: 'Storyboard', run_id: 7, stage_id: 4, item_id: 5, kind: 'result', current_version_id: 201 }, versions: [] });
    if (path.endsWith('/questions')) return json(route, Number(url.searchParams.get('stage_id')) === 4 && !url.searchParams.has('item_id') ? [{ id: 21, ordinal: 1, local_ref: 'creative/Q21', title: 'Keep the rain?', body: 'An older question from before the conversation feed.', status: archiveAnswered ? 'resolved' : 'open', resolution: archiveAnswered ? 'answered' : '', options: ['Keep it', 'Remove it'], your_turn: true, asked_by: 'creative-7', asked_at: now, artifact_version_ids: [], creative_context: { run_id: 7, stage_id: 4 }, messages: [] }] : []);
    if (path === '/v1/questions/21/answer' || path === '/v1/questions/21/reply') { writes.push({ path, body: request.postDataJSON() }); archiveAnswered = path.endsWith('/answer'); return json(route, { id: 21, status: archiveAnswered ? 'resolved' : 'open' }); }
    const feedBase = '/v1/creative/runs/7/feed';
    if (path === `${feedBase}/unread`) {
      const count = stage => entries.filter(e => e.stage_id === stage && e.author === 'orchestrator' && e.id > (markers.get(stage) ?? 0)).length;
      const stages = [4, 8].map(stage_id => ({ stage_id, unread: count(stage_id) }));
      return json(route, { run_id: 7, total: count(0) + stages.reduce((n, s) => n + s.unread, 0), run_feed: count(0), stages });
    }
    if (path === `${feedBase}/read`) {
      const body = request.postDataJSON(), stage = body.stage_id ?? 0;
      if (body.through_entry_id !== 0 && !entries.some(e => e.id === body.through_entry_id && e.stage_id === stage)) { failures.push('Invalid read marker'); return json(route, { error: { code: 'invalid_scope', message: 'Wrong read scope' } }, 400); }
      reads.push(body); markers.set(stage, Math.max(markers.get(stage) ?? 0, body.through_entry_id));
      return json(route, { stage_id: stage, through_entry_id: markers.get(stage) });
    }
    const card = path.match(/\/feed\/cards\/(\d+)(\/decision)?$/);
    if (card) {
      const entry = entries.find(e => e.id === Number(card[1]));
      if (!entry) return json(route, { error: { code: 'not_found', message: 'Card not found' } }, 404);
      if (card[2]) {
        if (entry.card.status !== 'pending') return json(route, { error: { code: 'creative_card_conflict', message: 'Read the current proposal' }, entry }, 409);
        const body = request.postDataJSON(); writes.push({ path, body });
        entry.card = { ...entry.card, status: 'decided', decision: body.decision, decided_at: now + 10000 };
      }
      return json(route, entry);
    }
    if (path === feedBase) {
      if (method === 'POST') {
        const body = request.postDataJSON(); writes.push({ path, body });
        const created = message(Math.max(...entries.map(e => e.id)) + 1, body.body, { stage: body.stage_id ?? 0, item: body.item_id ?? 0, author: 'human', versions: body.artifact_version_ids ?? [] });
        entries.push(created); return json(route, created, 201);
      }
      const stage = Number(url.searchParams.get('stage_id') ?? 0), item = Number(url.searchParams.get('item_id') ?? 0);
      return json(route, entries.filter(e => e.stage_id === stage && (!item || e.item_id === item)).sort((a, b) => b.id - a.id));
    }
    failures.push(`${method} ${path}`);
    return json(route, { error: { code: 'unexpected_mock_request', message: `${method} ${path}` } }, 500);
  });
  await page.goto('http://127.0.0.1:5177/creative/?project=film&run=7&stage=4&item=5');
  await page.getByRole('article', { name: 'Message 105' }).waitFor();
  await page.getByRole('link', { name: 'Storyboard · version 2' }).first().waitFor();
  await page.screenshot({ path: `${screenshots}01-stage-conversation.png`, fullPage: true });
  await page.getByLabel('Message', { exact: true }).fill('Keep this framing and the warmer light.');
  await page.getByRole('button', { name: 'Send message', exact: true }).click();
  await page.getByText('Keep this framing and the warmer light.', { exact: true }).waitFor();
  const sent = writes.find(w => w.path === '/v1/creative/runs/7/feed');
  if (!sent || sent.body.stage_id !== 4 || sent.body.item_id !== 5) throw new Error('Message lost selected item scope');
  const pending = page.getByRole('article', { name: 'Proposal 103' });
  await pending.scrollIntoViewIfNeeded();
  await page.screenshot({ path: `${screenshots}02-proposal-before-go.png`, fullPage: true });
  await pending.getByRole('button', { name: 'Go', exact: true }).click();
  await pending.getByText('Decision: Go').waitFor();
  await page.screenshot({ path: `${screenshots}03-proposal-after-go.png`, fullPage: true });
  const replaced = page.getByRole('article', { name: 'Proposal 102' });
  await replaced.getByRole('link', { name: 'Replaced → proposal #103' }).click();
  await page.waitForFunction(() => document.activeElement?.id === 'feed-entry-103');
  if (await replaced.getByRole('button').count()) throw new Error('Replaced card has actions');
  await page.screenshot({ path: `${screenshots}04-replaced-card.png`, fullPage: true });
  // The run remains open while a new orchestrator entry arrives in another stage.
  entries.push(message(110, 'The edit is ready for your notes.', { stage: 8 }));
  await page.locator('.stages > li').nth(1).getByLabel('1 unread messages').waitFor();
  await page.screenshot({ path: `${screenshots}05-unread-appears.png`, fullPage: true });
  await page.getByRole('button', { name: /2\. Edit/ }).click();
  await page.getByRole('article', { name: 'Message 110' }).waitFor();
  await page.waitForFunction(() => !document.querySelector('.process .feed-unread'));
  await page.screenshot({ path: `${screenshots}06-unread-clears.png`, fullPage: true });
  await page.getByRole('button', { name: /1\. Scenes/ }).click();
  await page.getByText('Old questions', { exact: true }).click();
  await page.getByRole('button', { name: 'Keep it', exact: true }).waitFor();
  await page.locator('.results').evaluate(element => { element.scrollTop = element.scrollHeight; });
  await page.screenshot({ path: `${screenshots}07-old-questions.png`, fullPage: true });
  await page.getByRole('button', { name: 'Keep it', exact: true }).click();
  await page.getByRole('button', { name: 'Keep it', exact: true }).waitFor({ state: 'hidden' });
  if (await page.getByRole('button', { name: 'Open question', exact: true }).count()) throw new Error('Question creation remains');
  if (!writes.some(w => w.path.endsWith('/cards/103/decision') && w.body.decision === 'go')) throw new Error('Missing Go decision');
  if (!reads.some(r => r.stage_id === 8 && r.through_entry_id === 110)) throw new Error('Missing edit read marker');
  if (failures.length || consoleErrors.length) throw new Error(JSON.stringify({ failures, consoleErrors }));
  return { screenshots, writes, reads, requestCount: requests.length, errors: consoleErrors };
}
