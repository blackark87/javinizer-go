import type { BatchJobResponse, FileResult } from '$lib/api/types';

export function isTranslationFailure(
	result: Pick<FileResult, 'status' | 'error' | 'movie'>,
): boolean {
	if (result.status.toLowerCase() !== 'failed' || !result.movie) {
		return false;
	}
	const message = result.error?.trim().toLowerCase() ?? '';
	return (
		message.startsWith('translation stage failed:') ||
		message.startsWith('translation failed:') ||
		message.startsWith('translation failed for ')
	);
}

export function countTranslationFailures(
	job: Pick<BatchJobResponse, 'results'>,
): number {
	return Object.values(job.results ?? {}).filter(isTranslationFailure).length;
}

export function canRetranslateBatchJob(
	job: Pick<BatchJobResponse, 'status' | 'completed' | 'results'>,
): boolean {
	return (
		job.status.toLowerCase() === 'completed' &&
		(job.completed > 0 || countTranslationFailures(job) > 0)
	);
}

export function isBatchRetranslationRunning(
	job: Pick<BatchJobResponse, 'retranslation'>,
): boolean {
	return job.retranslation?.status === 'running';
}
