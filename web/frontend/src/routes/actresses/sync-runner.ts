import type { ActressSyncResponse, ActressSyncStatus } from '$lib/api/types';

export interface ActressSyncDetail {
	id: number;
	status: ActressSyncStatus | 'failed';
	message: string;
	updatedFields: string[];
	conflictActressId?: number;
}

export interface ActressSyncSummary {
	total: number;
	processed: number;
	updated: number;
	skipped: number;
	conflicts: number;
	failed: number;
	stopped: boolean;
	details: ActressSyncDetail[];
}

export interface ActressSyncRunnerOptions {
	shouldStop?: () => boolean;
	onItemStart?: (id: number, summary: ActressSyncSummary) => void;
	onProgress?: (summary: ActressSyncSummary, detail: ActressSyncDetail) => void;
	onFinished?: (summary: ActressSyncSummary) => void | Promise<void>;
}

function snapshot(summary: ActressSyncSummary): ActressSyncSummary {
	return { ...summary, details: [...summary.details] };
}

function responseDetail(id: number, response: ActressSyncResponse): ActressSyncDetail {
	return {
		id,
		status: response.status,
		message: response.messages.join(' ') || `Actress #${id}: ${response.status}`,
		updatedFields: [...response.updated_fields],
		conflictActressId: response.conflict_actress_id
	};
}

// Deliberately uses a for-of await loop: there is never more than one actress
// sync request in flight. A stop request is observed after the current item.
export async function runActressSyncQueue(
	ids: number[],
	syncOne: (id: number) => Promise<ActressSyncResponse>,
	options: ActressSyncRunnerOptions = {}
): Promise<ActressSyncSummary> {
	const summary: ActressSyncSummary = {
		total: ids.length,
		processed: 0,
		updated: 0,
		skipped: 0,
		conflicts: 0,
		failed: 0,
		stopped: false,
		details: []
	};

	try {
		for (const id of ids) {
			if (options.shouldStop?.()) {
				summary.stopped = true;
				break;
			}
			options.onItemStart?.(id, snapshot(summary));

			let detail: ActressSyncDetail;
			try {
				const response = await syncOne(id);
				detail = responseDetail(id, response);
				if (response.status === 'updated') summary.updated += 1;
				else if (response.status === 'skipped') summary.skipped += 1;
				else summary.conflicts += 1;
			} catch (error) {
				summary.failed += 1;
				detail = {
					id,
					status: 'failed',
					message: error instanceof Error ? error.message : String(error),
					updatedFields: []
				};
			}

			summary.processed += 1;
			summary.details.push(detail);
			options.onProgress?.(snapshot(summary), detail);
		}
		if (summary.processed < summary.total) summary.stopped = true;
		return snapshot(summary);
	} finally {
		await options.onFinished?.(snapshot(summary));
	}
}
