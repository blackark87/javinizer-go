import type { ActressSyncResponse, ActressSyncStatus } from '$lib/api/types';

export interface ActressSyncDetail {
	id: number;
	label: string;
	status: ActressSyncStatus;
	messages: string[];
	updatedFields: string[];
	source?: string;
	sourceQuery?: string;
	dmmId?: number;
	thumbURL?: string;
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
	getLabel?: (id: number) => string;
	onItemStart?: (id: number, summary: ActressSyncSummary) => void;
	onProgress?: (summary: ActressSyncSummary, detail: ActressSyncDetail) => void;
	onFinished?: (summary: ActressSyncSummary) => void | Promise<void>;
}

function snapshot(summary: ActressSyncSummary): ActressSyncSummary {
	return { ...summary, details: [...summary.details] };
}

function responseActressLabel(id: number, response: ActressSyncResponse, fallback: string): string {
	const actress = response.actress;
	const englishName = [actress.first_name, actress.last_name].filter(Boolean).join(' ').trim();
	const names = [actress.japanese_name?.trim(), englishName].filter((name, index, all): name is string =>
		Boolean(name) && all.indexOf(name) === index
	);
	return names.length > 0 ? names.join(' / ') : fallback || `Actress #${id}`;
}

function responseDetail(id: number, response: ActressSyncResponse, fallbackLabel: string): ActressSyncDetail {
	return {
		id,
		label: responseActressLabel(id, response, fallbackLabel),
		status: response.status,
		messages: response.messages.length > 0 ? [...response.messages] : [`No detail was returned for ${response.status}`],
		updatedFields: [...response.updated_fields],
		source: response.source,
		sourceQuery: response.source_query,
		dmmId: response.actress.dmm_id,
		thumbURL: response.actress.thumb_url,
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
			const fallbackLabel = options.getLabel?.(id) || `Actress #${id}`;
			try {
				const response = await syncOne(id);
				detail = responseDetail(id, response, fallbackLabel);
				if (response.status === 'updated') summary.updated += 1;
				else if (response.status === 'skipped') summary.skipped += 1;
				else if (response.status === 'conflict') summary.conflicts += 1;
				else summary.failed += 1;
			} catch (error) {
				summary.failed += 1;
				detail = {
					id,
					label: fallbackLabel,
					status: 'failed',
					messages: [error instanceof Error ? error.message : String(error)],
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
