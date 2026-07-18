import { describe, expect, it, vi } from 'vitest';
import type { BatchJobResponse, FileResult, Movie } from '$lib/api/types';
import { createRescrapeController } from './rescrape-controller';

function movie(overrides: Partial<Movie> = {}): Movie {
	return { id: 'ABC-001', title: 'Old title', description: 'Old description', ...overrides };
}

describe('rescrape controller candidate selection', () => {
	it('uses retained candidate metadata without loading scrapers or rescraping', async () => {
		const filePath = '/movies/ABC-001.mp4';
		const result: FileResult = {
			result_id: 'result-1',
			file_path: filePath,
			movie_id: 'ABC-001',
			status: 'completed',
			movie: movie(),
			started_at: '2026-07-18T00:00:00Z',
			is_multi_part: false,
			part_number: 0,
			part_suffix: '',
			has_conflict: true,
		};
		let job = {
			id: 'job-1',
			status: 'completed',
			results: { [filePath]: result },
		} as unknown as BatchJobResponse;
		const getScrapers = vi.fn().mockResolvedValue([]);
		const rescrapeBatchMovie = vi.fn();
		const selectBatchMovieCandidate = vi.fn().mockResolvedValue({
			movie: movie({ title: 'Selected title', description: 'Selected description' }),
			field_sources: { title: 'dmm', description: 'dmm' },
			candidates: result.candidates,
			has_conflict: false,
		});
		const success = vi.fn();
		const error = vi.fn();
		const rescrapingStates = new Map<string, boolean>();

		const controller = createRescrapeController({
			getJobId: () => 'job-1',
			getCurrentResult: () => result,
			getJob: () => job,
			setJob: (next) => (job = next),
			getEditedMovies: () => new Map<string, Movie>(),
			getAvailableScrapers: () => [],
			setAvailableScrapers: vi.fn(),
			getRescrapeResultId: () => '',
			setRescrapeResultId: vi.fn(),
			getSelectedScrapers: () => [],
			setSelectedScrapers: vi.fn(),
			getManualSearchMode: () => false,
			setManualSearchMode: vi.fn(),
			getManualSearchInput: () => '',
			setManualSearchInput: vi.fn(),
			setShowRescrapeModal: vi.fn(),
			getRescrapePreset: () => undefined,
			setRescrapePreset: vi.fn(),
			getRescrapeScalarStrategy: () => '',
			setRescrapeScalarStrategy: vi.fn(),
			getRescrapeArrayStrategy: () => '',
			setRescrapeArrayStrategy: vi.fn(),
			getRescrapeSelectedSections: () => [],
			setRescrapeSelectedSections: vi.fn(),
			getRescrapingStates: () => rescrapingStates,
			toastSuccess: success,
			toastError: error,
			api: { getScrapers, rescrapeBatchMovie, selectBatchMovieCandidate },
		});

		await controller.selectCandidateProvider('result-1', 'dmm');

		expect(selectBatchMovieCandidate).toHaveBeenCalledOnce();
		expect(selectBatchMovieCandidate).toHaveBeenCalledWith('job-1', 'result-1', 'dmm');
		expect(getScrapers).not.toHaveBeenCalled();
		expect(rescrapeBatchMovie).not.toHaveBeenCalled();
		expect(job.results[filePath].movie?.title).toBe('Selected title');
		expect(job.results[filePath].movie?.description).toBe('Selected description');
		expect(job.results[filePath].has_conflict).toBe(false);
		expect(rescrapingStates.size).toBe(0);
		expect(success).toHaveBeenCalledWith('Selected dmm title and description');
		expect(error).not.toHaveBeenCalled();
	});
});
