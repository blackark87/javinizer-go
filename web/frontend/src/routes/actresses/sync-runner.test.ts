import { describe, expect, it } from 'vitest';
import type { ActressSyncJob, ActressSyncTask } from '$lib/api/types';
import { buildActressSyncSummary } from './sync-runner';

describe('buildActressSyncSummary', () => {
	it('maps durable counters and exposes every concurrently running item', () => {
		const job = {
			id: 'job', status: 'running', scope: 'missing', total_tasks: 5, completed: 2,
			updated: 1, warnings: 1, skipped: 1, conflicts: 0, failed: 0, cancelled: 0,
			cancel_requested: false, created_at: 'now'
		} satisfies ActressSyncJob;
		const task = (id: string, status: ActressSyncTask['status'], stage: string): ActressSyncTask => ({
			id, job_id: 'job', kind: 'actress', label: id, dedupe_key: id, status, stage,
			messages: [`${id} detail`], updated_fields: [], attempts: 1, created_at: 'now'
		});
		const summary = buildActressSyncSummary(job, [
			task('done', 'completed', 'completed'), task('skipped', 'skipped', 'completed'),
			task('one', 'running', 'resolving'), task('two', 'running', 'translating'), task('queued', 'pending', 'queued')
		]);

		expect(summary).toMatchObject({ total: 5, processed: 2, updated: 1, warnings: 1, skipped: 1 });
		expect(summary.active.map((item) => [item.id, item.stage])).toEqual([
			['one', 'resolving'], ['two', 'translating']
		]);
		expect(summary.details.map((item) => item.id)).toEqual(['skipped']);
	});

	it('hides successful item logs and puts failures before other diagnostics', () => {
		const job = {
			id: 'job', status: 'completed', scope: 'selected', total_tasks: 4, completed: 4,
			updated: 1, warnings: 1, skipped: 1, conflicts: 0, failed: 1, cancelled: 0,
			cancel_requested: false, created_at: 'now'
		} satisfies ActressSyncJob;
		const base = {
			job_id: 'job', kind: 'actress' as const, dedupe_key: 'task', updated_fields: [], attempts: 1, created_at: 'now'
		};
		const summary = buildActressSyncSummary(job, [
			{ ...base, id: 'success', label: 'success', status: 'completed', stage: 'completed', outcome: 'updated', messages: ['Verified DMM ID 1 and canonical name from sougouwiki'] },
			{ ...base, id: 'skipped', label: 'skipped', status: 'skipped', stage: 'completed', messages: ['No linked movies are available'] },
			{ ...base, id: 'warning', label: 'warning', status: 'completed', stage: 'completed', outcome: 'updated_with_warning', warning: 'LLM timeout', messages: ['Updated NFO actress blocks: test.nfo'] },
			{ ...base, id: 'failed', label: 'failed', status: 'failed', stage: 'resolving', error_message: 'resolver timeout', messages: ['Resolving actress identity and thumbnail', 'sougouwiki: identity lookup failed: timeout'] }
		] satisfies ActressSyncTask[]);

		expect(summary.details.map((item) => item.id)).toEqual(['failed', 'warning', 'skipped']);
		expect(summary.details[0].messages).toEqual(['sougouwiki: identity lookup failed: timeout']);
		expect(summary.details[1].messages).toEqual([]);
	});

	it('marks a cancelled durable job as stopped', () => {
		const job = {
			id: 'job', status: 'cancelled', scope: 'selected', total_tasks: 1, completed: 1,
			updated: 0, warnings: 0, skipped: 0, conflicts: 0, failed: 0, cancelled: 1,
			cancel_requested: true, created_at: 'now'
		} satisfies ActressSyncJob;
		expect(buildActressSyncSummary(job, []).stopped).toBe(true);
	});
});
