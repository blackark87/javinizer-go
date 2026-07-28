import { describe, expect, it } from 'vitest';
import type { FileResult } from '$lib/api/types';
import {
	canRetranslateBatchJob,
	countTranslationFailures,
	isTranslationFailure,
} from './translation-failure';

function result(overrides: Partial<FileResult> = {}): FileResult {
	return {
		result_id: 'result-1',
		file_path: '/media/IPX-535.mp4',
		movie_id: 'IPX-535',
		status: 'failed',
		movie: { id: 'IPX-535', title: '日本語題名' },
		started_at: new Date(0).toISOString(),
		is_multi_part: false,
		part_number: 0,
		part_suffix: '',
		...overrides,
	};
}

describe('isTranslationFailure', () => {
	it('recognizes recoverable deferred translation failures', () => {
		expect(
			isTranslationFailure(
				result({ error: 'translation stage failed: invalid title output' }),
			),
		).toBe(true);
	});

	it('does not classify translation checkpoint persistence failures', () => {
		expect(
			isTranslationFailure(
				result({ error: 'translation checkpoint persistence failed' }),
			),
		).toBe(false);
	});

	it('requires retained movie metadata', () => {
		expect(
			isTranslationFailure(
				result({
					error: 'translation stage failed: provider unavailable',
					movie: undefined,
				}),
			),
		).toBe(false);
	});
});

describe('countTranslationFailures', () => {
	it('counts only recoverable translation failures', () => {
		expect(
			countTranslationFailures({
				results: {
					'/media/IPX-535.mp4': result({
						error: 'translation stage failed: invalid title output',
					}),
					'/media/ABC-123.mp4': result({
						result_id: 'result-2',
						file_path: '/media/ABC-123.mp4',
						movie_id: 'ABC-123',
						error: 'scrape failed: no metadata',
					}),
				},
			}),
		).toBe(1);
	});
});

describe('canRetranslateBatchJob', () => {
	it('allows completed jobs with a recoverable translation failure', () => {
		expect(
			canRetranslateBatchJob({
				status: 'completed',
				completed: 0,
				results: {
					'/media/IPX-535.mp4': result({
						error: 'translation stage failed: provider unavailable',
					}),
				},
			}),
		).toBe(true);
	});

	it('hides retranslation after organization', () => {
		expect(
			canRetranslateBatchJob({
				status: 'organized',
				completed: 1,
				results: {
					'/media/IPX-535.mp4': result({
						status: 'completed',
						error: undefined,
					}),
				},
			}),
		).toBe(false);
	});
});
