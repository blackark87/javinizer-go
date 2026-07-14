import type { ActressSyncJob, ActressSyncTask, ActressSyncTaskStatus } from '$lib/api/types';

export interface ActressSyncDetail {
	id: string;
	label: string;
	status: ActressSyncTaskStatus;
	stage: string;
	outcome?: string;
	messages: string[];
	updatedFields: string[];
	warning?: string;
	errorMessage?: string;
	movieID?: string;
}

export interface ActressSyncSummary {
	total: number;
	processed: number;
	updated: number;
	warnings: number;
	skipped: number;
	conflicts: number;
	failed: number;
	cancelled: number;
	stopped: boolean;
	details: ActressSyncDetail[];
	active: ActressSyncDetail[];
}

export function buildActressSyncSummary(job: ActressSyncJob, tasks: ActressSyncTask[]): ActressSyncSummary {
	const details = tasks.map((task): ActressSyncDetail => ({
		id: task.id,
		label: task.label || task.movie_id || `Task ${task.id}`,
		status: task.status,
		stage: task.stage,
		outcome: task.outcome,
		messages: task.messages ?? [],
		updatedFields: task.updated_fields ?? [],
		warning: task.warning,
		errorMessage: task.error_message,
		movieID: task.movie_id
	}));
	return {
		total: job.total_tasks,
		processed: job.completed,
		updated: job.updated,
		warnings: job.warnings,
		skipped: job.skipped,
		conflicts: job.conflicts,
		failed: job.failed,
		cancelled: job.cancelled,
		stopped: job.status === 'cancelled',
		details,
		active: details.filter((detail) => detail.status === 'running')
	};
}
