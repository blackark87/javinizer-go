import { describe, expect, it } from 'vitest';
import type { ActressSyncResponse } from '$lib/api/types';
import { runActressSyncQueue } from './sync-runner';

function response(id: number, status: ActressSyncResponse['status'] = 'updated'): ActressSyncResponse {
	return {
		actress: { id, japanese_name: `Actress ${id}`, dmm_id: status === 'updated' ? 1000 + id : 0 },
		status,
		updated_fields: status === 'updated' ? ['dmm_id'] : [],
		messages: [`${status} ${id}`, `detail ${id}`],
		source: status === 'updated' ? 'sougouwiki' : undefined,
		source_query: status === 'updated' ? `Actress ${id}` : undefined
	};
}

describe('runActressSyncQueue', () => {
	it('never runs more than one request concurrently', async () => {
		let inFlight = 0;
		let maxInFlight = 0;
		const order: string[] = [];

		const summary = await runActressSyncQueue([1, 2, 3], async (id) => {
			inFlight += 1;
			maxInFlight = Math.max(maxInFlight, inFlight);
			order.push(`start-${id}`);
			await Promise.resolve();
			order.push(`end-${id}`);
			inFlight -= 1;
			return response(id);
		});

		expect(maxInFlight).toBe(1);
		expect(order).toEqual(['start-1', 'end-1', 'start-2', 'end-2', 'start-3', 'end-3']);
		expect(summary.updated).toBe(3);
	});

	it('continues after an item error and counts every outcome', async () => {
		const calls: number[] = [];
		const summary = await runActressSyncQueue([1, 2, 3, 4, 5], async (id) => {
			calls.push(id);
			if (id === 2) throw new Error('resolver unavailable');
			if (id === 3) return response(id, 'skipped');
			if (id === 4) return response(id, 'conflict');
			if (id === 5) return response(id, 'failed');
			return response(id);
		});

		expect(calls).toEqual([1, 2, 3, 4, 5]);
		expect(summary).toMatchObject({ processed: 5, updated: 1, skipped: 1, conflicts: 1, failed: 2 });
		expect(summary.details[0]).toMatchObject({
			id: 1,
			label: 'Actress 1',
			status: 'updated',
			messages: ['updated 1', 'detail 1'],
			source: 'sougouwiki',
			sourceQuery: 'Actress 1'
		});
		expect(summary.details[1]).toMatchObject({ id: 2, label: 'Actress #2', status: 'failed', messages: ['resolver unavailable'] });
	});

	it('stops before the next item and always runs final refresh callback', async () => {
		let stop = false;
		let finished = 0;
		const calls: number[] = [];
		const summary = await runActressSyncQueue([1, 2, 3], async (id) => {
			calls.push(id);
			stop = true;
			return response(id);
		}, {
			shouldStop: () => stop,
			onFinished: () => { finished += 1; }
		});

		expect(calls).toEqual([1]);
		expect(summary).toMatchObject({ processed: 1, total: 3, stopped: true });
		expect(finished).toBe(1);
	});
});
