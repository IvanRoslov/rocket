// Isolated browser acceptance. Intercept every /v1 request: never contact a live daemon.
export async function runFinalAcceptanceCheck(page) {
  const work = { id: 7, project_id: 'film', name: 'After the rain', brief: 'A quiet short film.', pipeline_id: 1, pipeline_name: 'Film', status: 'completed', closure_state: 'open', closure_error: '', accepted_at: 0, orchestrator_session_id: 'orch', stages: [{ id: 11, run_id: 7, position: 0, name: 'Final edit', description: 'Finished edit', expected_artifacts: 'Film', repeated: false, approval: 'stage', status: 'completed', progress: 100, progress_note: '', review_revision: 1, artifact_version_ids: [201], items: [] }] };
  let attempts = 0;
  const errors = [];
  page.on('pageerror', error => errors.push(error.message));
  const json = (route, value, status = 200) => route.fulfill({ status, contentType: 'application/json', body: JSON.stringify(value) });
  await page.route('**/v1/**', route => {
    const req = route.request(), url = new URL(req.url()), path = url.pathname;
    if (path === '/v1/projects') return json(route, [{ id: 'film', name: 'Film studio' }]);
    if (path === '/v1/creative/pipelines') return json(route, []);
    if (path === '/v1/model-profiles') return json(route, { profiles: [] });
    if (path === '/v1/creative/runs') return json(route, [work]);
    if (path === '/v1/creative/runs/7') return json(route, work);
    if (path === '/v1/creative/runs/7/accept') {
      attempts++;
      if (attempts === 1) { work.closure_state = 'closing'; work.closure_error = 'Injected worker stop failure'; return json(route, { error: { code: 'internal_error', message: work.closure_error } }, 500); }
      work.closure_state = 'accepted'; work.accepted_at = 1794410000; work.closure_error = ''; return json(route, work);
    }
    if (path.endsWith('/sessions')) return json(route, [{ id: 'orch', tmux_name: 'film-orch', kind: 'orchestrator', agent: 'codex', state: work.closure_state === 'accepted' ? 'killed' : 'running' }]);
    if (path.endsWith('/feed/unread')) return json(route, { run_id: 7, total: 0, run_feed: 0, stages: [] });
    if (path.endsWith('/feed/read')) return json(route, req.postDataJSON());
    if (path.endsWith('/feed')) return json(route, [{ id: 1, run_id: 7, stage_id: 11, item_id: 0, kind: 'message', author: 'orchestrator', author_session_id: 'orch', body: 'The final film is published and retained.', artifact_version_ids: [], created_at: 1794410000, card: null }]);
    if (path.endsWith('/questions')) return json(route, []);
    if (path.endsWith('/artifacts')) return json(route, [{ id: 30, run_id: 7, stage_id: 11, name: 'Final film notes', kind: 'result', current_version_id: 201 }]);
    if (path === '/v1/creative/artifacts/30') return json(route, { artifact: { id: 30, run_id: 7, stage_id: 11, name: 'Final film notes', kind: 'result', current_version_id: 201 }, versions: [{ id: 201, artifact_id: 30, version_number: 1, filename: 'final.txt', mime: 'text/plain', size: 12, content_url: '/v1/creative/artifact-versions/201/content', download_url: '/v1/creative/artifact-versions/201/download' }] });
    if (path.endsWith('/content') || path.endsWith('/download')) return route.fulfill({ status: 200, contentType: 'text/plain', body: 'Retained film' });
    throw new Error(`Unexpected isolated request: ${req.method()} ${path}`);
  });
  await page.setViewportSize({ width: 1440, height: 1000 });
  await page.goto('http://127.0.0.1:15574/creative/?project=film&run=7&stage=11');
  await page.getByRole('button', { name: 'Accept all work', exact: true }).click();
  await page.getByRole('heading', { name: 'Closure incomplete' }).waitFor();
  if (await page.getByText('Work accepted · Permanent archive', { exact: true }).count()) throw new Error('False success after stop failure');
  await page.getByRole('button', { name: 'Retry closure' }).click();
  await page.getByText('Work accepted · Permanent archive', { exact: true }).waitFor();
  if (!page.url().includes('stage=11')) throw new Error('Acceptance lost selection');
  if (await page.getByRole('textbox', { name: 'Message', exact: true }).count()) throw new Error('Archive has composer');
  await page.reload();
  await page.getByText('Work accepted · Permanent archive', { exact: true }).waitFor();
  await page.getByRole('tab', { name: 'Artifacts' }).click();
  await page.getByRole('link', { name: 'Download final.txt' }).waitFor();
  if (await page.getByRole('button', { name: 'Upload source', exact: true }).count()) throw new Error('Archive has upload action');
  await page.screenshot({ path: 'docs/screenshots/creative-final-acceptance-desktop.png', fullPage: true });
  await page.getByRole('button', { name: 'Completed works', exact: true }).click();
  await page.getByRole('heading', { name: 'Completed works', exact: true }).waitFor();
  await page.getByRole('button', { name: /After the rain/ }).click();
  await page.getByText('Work accepted · Permanent archive', { exact: true }).waitFor();
  await page.setViewportSize({ width: 390, height: 844 });
  if (await page.evaluate(() => document.documentElement.scrollWidth > innerWidth)) throw new Error('Archive navigation overflows mobile viewport');
  await page.screenshot({ path: 'docs/screenshots/creative-final-acceptance-mobile.png', fullPage: true });
  if (errors.length) throw new Error(errors.join('\n'));
  return { attempts, state: work.closure_state, retainedSelection: page.url(), errors };
}
