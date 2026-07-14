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

const verboseSuccessPrefixes = [
	'resolving actress identity',
	'resolving the complete sougouwiki cast',
	'applying dmm hepburn',
	'refreshing affected movie',
	'transliterating actress names',
	'mapping ',
	'sougouwiki: checking linked movie',
	'reused canonical actress',
	'canonicalized actress',
	'verified dmm id',
	'profile thumbnail resolved',
	'replaced the unknown mapping',
	'updated nfo actress blocks',
	'no safe existing nfo was found'
];

function isDiagnosticMessage(message: string): boolean {
	const normalized = message.trim().toLowerCase();
	if (!normalized) return false;
	if (verboseSuccessPrefixes.some((prefix) => normalized.startsWith(prefix))) return false;
	if (normalized.includes(': matched dmm id ')) return false;
	if (normalized.includes(': existing dmm id matched the canonical actress')) return false;
	if (normalized.includes(': exact actress match found')) return false;
	if (normalized.includes(': unique remaining actress matched')) return false;
	return true;
}

function diagnosticPriority(detail: ActressSyncDetail): number {
	if (detail.status === 'failed' || detail.errorMessage) return 0;
	if (detail.status === 'conflict') return 1;
	if (detail.warning) return 2;
	if (detail.status === 'skipped') return 3;
	if (detail.status === 'cancelled') return 4;
	return 5;
}

export function buildActressSyncSummary(job: ActressSyncJob, tasks: ActressSyncTask[]): ActressSyncSummary {
	const allDetails = tasks.map((task): ActressSyncDetail => ({
		id: task.id,
		label: task.label || task.movie_id || `Task ${task.id}`,
		status: task.status,
		stage: task.stage,
		outcome: task.outcome,
		messages: (task.messages ?? []).filter(isDiagnosticMessage),
		updatedFields: task.updated_fields ?? [],
		warning: task.warning,
		errorMessage: task.error_message,
		movieID: task.movie_id
	}));
	const active = allDetails.filter((detail) => detail.status === 'running');
	const details = allDetails
		.filter((detail) =>
			detail.status === 'failed' || detail.status === 'conflict' || detail.status === 'skipped' ||
			detail.status === 'cancelled' || Boolean(detail.warning) || Boolean(detail.errorMessage)
		)
		.sort((left, right) => diagnosticPriority(left) - diagnosticPriority(right));
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
		active
	};
}
