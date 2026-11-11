// Desktop acceptance against the accepted Conversation feed HTTP contract.
// Load captureChatWindow(page, phase = 'before') in a Playwright environment while Vite serves
// this worktree at http://127.0.0.1:5177. All /v1 requests are intercepted;
// this check never contacts or changes a running Rocket daemon.
export async function captureChatWindow(page, phase = 'before') {
  const { mkdir } = await import('node:fs/promises');
  const screenshots = new URL('../../docs/screenshots/creative-feed/', import.meta.url).pathname;
  await mkdir(screenshots, { recursive: true });
  await page.unroute('**/v1/**');
  await page.setViewportSize({ width: 1600, height: 1100 });
  const now = Math.floor(Date.now() / 1000) - 9000;
  const versions = Array.from({ length: 12 }, (_, i) => 201 + i);
  const work = { description: 'Build a short atmospheric film.', expected_artifacts: 'Storyboard and scene images', status: 'in_progress', progress: 60, progress_note: 'Opening revised', review_revision: 0, artifact_version_ids: [] };
  const items = [5, 6].map((id, i) => ({ ...work, id, run_id: 7, stage_id: 4, position: i, name: i ? 'Finale' : 'Opening scene' }));
  const run = { id: 7, project_id: 'film', pipeline_id: 1, pipeline_name: 'Short film', name: 'After the rain', brief: 'A quiet story about an unexpected reunion.', status: 'in_progress', orchestrator_session_id: 'creative-7', allowed_profiles: [], stages: [
    { ...work, id: 4, run_id: 7, position: 0, name: 'Scenes', repeated: true, approval: 'item', items },
    { ...work, id: 8, run_id: 7, position: 1, name: 'Edit', repeated: false, approval: 'stage', items: [] },
  ] };
  const message = (id, body, { stage = 4, item = 0, author = 'orchestrator', versions = [] } = {}) => ({ id, run_id: 7, stage_id: stage, item_id: item, kind: 'message', author, author_session_id: author === 'human' ? '' : 'creative-7', body, artifact_version_ids: versions, created_at: now + id * 60, card: null });
  const proposal = (id, body, card) => ({ ...message(id, body, { item: 5, versions }), kind: 'proposal', card: { status: 'pending', decision: '', decided_at: 0, replaces_entry_id: 0, replaced_by_entry_id: 0, ...card } });
  let entries = [
    message(105, 'I revised the opening with a warmer palette. **Version 2** keeps the framing you liked.', { item: 5, versions }),
    message(104, 'Keep the framing, but make the light warmer.', { item: 5, author: 'human', versions }),
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
    if (path.endsWith('/artifacts') && !path.endsWith('/artifact-versions')) return json(route, url.searchParams.get('item_id') !== '5' ? [] : versions.map((id, i) => ({ id: 30 + i, name: `Opening image ${i + 1}`, run_id: 7, stage_id: 4, item_id: 5, kind: 'result', current_version_id: id })));
    const versionMatch = path.match(/artifact-versions\/(\d+)$/);
    if (versionMatch) { const id = Number(versionMatch[1]); return json(route, { id, artifact_id: 30 + id - 201, version_number: id - 200, filename: 'opening.svg', mime: 'image/svg+xml', size: 100, content_url: '/mock/opening.svg', download_url: '/mock/opening.svg' }); }
    const artifactMatch = path.match(/artifacts\/(\d+)$/);
    if (artifactMatch) { const id = Number(artifactMatch[1]); return json(route, { artifact: { id, name: `Opening image ${id - 29}`, run_id: 7, stage_id: 4, item_id: 5, kind: 'result', current_version_id: 201 + id - 30 }, versions: [{ id: 201 + id - 30, artifact_id: id, version_number: id - 29, filename: 'opening.svg', mime: 'image/svg+xml', size: 100, content_url: '/mock/opening.svg', download_url: '/mock/opening.svg' }] }); }
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
  await page.route('**/mock/opening.svg', route => route.fulfill({ contentType: 'image/svg+xml', body: '<svg xmlns="http://www.w3.org/2000/svg" width="400" height="220"><rect width="400" height="220" fill="#caa47f"/><circle cx="310" cy="55" r="35" fill="#f8dfac"/><path d="M0 220V140L85 85L175 165L260 110L400 200V220" fill="#425959"/></svg>' }));
  entries.push(message(99, 'The run brief is ready. We will build the story scene by scene.', { stage: 0 }));
  for (const colorScheme of ['dark', 'light']) {
    await page.emulateMedia({ colorScheme });
    for (const narrow of [false, true]) {
      await page.setViewportSize(narrow ? { width: 390, height: 844 } : { width: 1600, height: 1100 });
      for (const scope of ['stage', 'item', 'run']) {
        await page.goto(`http://127.0.0.1:5177/creative/?project=film&run=7${scope === 'run' ? '' : '&stage=4'}${scope === 'item' ? '&item=5' : ''}`);
        await page.getByRole('article', { name: scope === 'run' ? 'Message 99' : 'Message 105', exact: true }).waitFor();
        // A dark OS (or a stale host attribute) must not darken chat alone.
        for (const theme of [null, 'dark', 'light']) {
          await page.evaluate(theme => {
            if (theme === null) document.documentElement.removeAttribute('data-theme');
            else document.documentElement.setAttribute('data-theme', theme);
          }, theme);
          const colors = await page.evaluate(() => {
            const background = selector => getComputedStyle(document.querySelector(selector)).backgroundColor;
            const surface = document.createElement('div');
            surface.style.background = 'var(--surface-2)';
            document.body.append(surface);
            const pageHistorySurface = getComputedStyle(surface).backgroundColor;
            surface.remove();
            return {
              page: background('.topbar'),
              discussion: background('.discussion'),
              composer: background('.composer'),
              history: background('.chat-history'),
              pageHistorySurface,
            };
          });
          if (colors.page !== 'rgb(255, 255, 255)' || colors.pageHistorySurface !== 'rgb(240, 240, 238)' ||
              colors.discussion !== colors.page || colors.composer !== colors.page || colors.history !== colors.pageHistorySurface) {
            throw new Error(`Chat must match the light dashboard (${colorScheme} OS, theme=${theme}, ${scope}, narrow=${narrow}): ${JSON.stringify(colors)}`);
          }
        }
        await page.waitForTimeout(500);
        if (phase === 'after' && narrow) await page.locator('.chat-window').scrollIntoViewIfNeeded();
        if (colorScheme === 'light') await page.screenshot({ path: `${screenshots}${phase}-${scope}-light${narrow ? '-narrow' : ''}.png`, fullPage: phase === 'before' });
      }
    }
  }
  if (phase === 'after') {
    await page.setViewportSize({ width: 1600, height: 1100 });
    await page.goto('http://127.0.0.1:5177/creative/?project=film&run=7&stage=4&item=5');
    await page.getByRole('article', { name: 'Message 105', exact: true }).waitFor();
    const history = page.getByRole('region', { name: 'Chat history' });
    await page.getByRole('textbox', { name: 'Message', exact: true }).fill('A draft that survives polling');
    const anchor = page.getByRole('article', { name: 'Message 104', exact: true });
    await history.evaluate(el => {
      const anchor = el.querySelector('#feed-entry-104');
      el.scrollTop += anchor.getBoundingClientRect().top - el.getBoundingClientRect().top - 10;
      el.dispatchEvent(new Event('scroll'));
    });
    const anchorTop = (await anchor.boundingBox()).y;
    await page.locator('#feed-entry-101').evaluate(el => { el.style.paddingBottom = '160px'; });
    await page.waitForTimeout(150);
    if (Math.abs((await anchor.boundingBox()).y - anchorTop) > 2) throw new Error('Late attachment layout shifted visible message');
    await page.locator('#feed-entry-101').evaluate(el => { el.style.paddingBottom = ''; });
    await page.waitForTimeout(100);
    await history.evaluate(el => { el.scrollTop = 60; el.dispatchEvent(new Event('scroll')); });
    const top = await history.evaluate(el => el.scrollTop);
    entries.push(message(110, 'A new message while you read history.', { item: 5 }));
    await page.getByRole('button', { name: '1 new ↓', exact: true }).waitFor();
    if (Math.abs(await history.evaluate(el => el.scrollTop) - top) > 1) throw new Error('Polling shifted history');
    if (await page.getByRole('textbox', { name: 'Message', exact: true }).inputValue() !== 'A draft that survives polling') throw new Error('Polling lost draft');
    await page.screenshot({ path: `${screenshots}after-new-message-pill.png` });
    await page.getByRole('button', { name: '1 new ↓', exact: true }).click();
    entries.push(message(111, 'Another message at the bottom.', { item: 5 }));
    await page.getByRole('article', { name: 'Message 111', exact: true }).waitFor();
    await page.waitForTimeout(100);
    if (await history.evaluate(el => el.scrollHeight - el.scrollTop - el.clientHeight) > 2) throw new Error('Did not follow bottom');
    await page.getByRole('button', { name: /Awaiting decision:/ }).click();
    await page.getByRole('button', { name: 'Go', exact: true }).waitFor();
    await page.screenshot({ path: `${screenshots}after-pending-proposal.png` });
    await page.getByRole('textbox', { name: 'Message', exact: true }).press('Control+Enter');
    await page.waitForFunction(() => document.querySelector('textarea')?.value === '');
    const sent = writes.find(w => w.path === '/v1/creative/runs/7/feed');
    if (sent?.body.artifact_version_ids.length !== 12 || sent.body.item_id !== 5) throw new Error('Keyboard send lost target versions');
    const composer = await page.locator('.composer').boundingBox();
    if (composer.y + composer.height > 1100) throw new Error('Composer below viewport');
  }
  if (failures.length || consoleErrors.length) throw new Error(JSON.stringify({ failures, consoleErrors }));
  return { screenshots, errors: consoleErrors };
}
